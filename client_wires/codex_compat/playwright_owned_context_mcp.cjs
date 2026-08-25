#!/usr/bin/env node
"use strict";

const fs = require("fs");
const { createConnection } = require("@playwright/mcp");
const { StdioServerTransport } = require("@modelcontextprotocol/sdk/server/stdio.js");
const { chromium } = require("playwright");

function errorDetail(error) {
  if (error && typeof error.stack === "string") return error.stack;
  return String(error);
}

function log(message) {
  const line = `${new Date().toISOString()} pid=${process.pid} ${message}\n`;
  const path = process.env.CODEX_PRO_PLAYWRIGHT_LOG;
  if (path) {
    try { fs.appendFileSync(path, line, { encoding: "utf8" }); } catch (_) { /* best-effort diagnostics */ }
  }
}

function parseArgs(argv) {
  const result = { endpoint: "", viewport: undefined };
  for (let index = 0; index < argv.length; index += 1) {
    const key = argv[index];
    if (key === "--cdp-endpoint") result.endpoint = argv[++index] || "";
    else if (key === "--viewport-size") result.viewport = parseViewport(argv[++index] || "");
    else throw new Error("Unsupported owned-context MCP argument");
  }
  if (!/^http:\/\/127\.0\.0\.1:\d+$/.test(result.endpoint)) {
    throw new Error("Owned-context MCP requires an explicit loopback CDP endpoint");
  }
  return result;
}

function parseViewport(value) {
  const match = /^(\d{2,5})x(\d{2,5})$/.exec(value);
  if (!match) throw new Error("Viewport must use WIDTHxHEIGHT");
  return { width: Number(match[1]), height: Number(match[2]) };
}

async function main() {
  const args = parseArgs(process.argv.slice(2));
  log(`owned-context connecting endpoint=${args.endpoint}`);
  const browser = await chromium.connectOverCDP(args.endpoint);
  const persistentContext = browser.contexts()[0];
  if (!persistentContext) throw new Error("Attached Chrome has no persistent default context");

  // Seed an isolated, agent-owned context from the on-disk profile. Official
  // Playwright MCP receives only this context, so existing or later user tabs,
  // pages, workers, and downloads are outside its inventory and authority.
  const seed = await persistentContext.storageState({ indexedDB: true });
  const ownedContext = await browser.newContext({
    storageState: seed,
    serviceWorkers: "block",
    acceptDownloads: false,
    ...(args.viewport ? { viewport: args.viewport } : {}),
  });

  const connection = await createConnection(
    {
      browser: { browserName: "chromium", isolated: false },
      sharedBrowserContext: true,
    },
    async () => ownedContext,
  );
  const transport = new StdioServerTransport();

  let stopping = false;
  const shutdown = async (exitCode = 0, reason = "requested") => {
    if (stopping) return;
    stopping = true;
    log(`owned-context stopping reason=${reason} exit_code=${exitCode}`);
    try {
      await connection.close().catch(() => undefined);
      const state = await ownedContext.storageState({ indexedDB: true }).catch(() => null);
      if (state) {
        // Merge cookies so concurrent user changes are never cleared. This is
        // enough for normal login/session continuity and never navigates an
        // existing user page. The persistent Chrome owns disk flushing.
        await persistentContext.addCookies(state.cookies).catch(() => undefined);
      }
      await ownedContext.close().catch(() => undefined);
      // connectOverCDP Browser.close disconnects this client connection; it
      // does not terminate the pre-existing Chrome process.
      await browser.close().catch(() => undefined);
    } finally {
      process.exit(exitCode);
    }
  };

  browser.once("disconnected", () => { void shutdown(1, "browser-disconnected"); });
  process.once("SIGINT", () => { void shutdown(130, "SIGINT"); });
  process.once("SIGTERM", () => { void shutdown(0, "SIGTERM"); });
  process.stdin.once("end", () => { void shutdown(0, "stdin-end"); });
  process.stdin.once("close", () => { void shutdown(0, "stdin-close"); });
  await connection.connect(transport);
  log(`owned-context ready endpoint=${args.endpoint}`);
}

main().catch((error) => {
  const detail = errorDetail(error);
  log(`owned-context failed\n${detail}`);
  process.stderr.write(`playwright owned-context MCP failed: ${detail}\n`);
  process.exit(1);
});
