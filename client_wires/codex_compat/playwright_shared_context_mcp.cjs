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
  const result = { endpoint: "" };
  for (let index = 0; index < argv.length; index += 1) {
    const key = argv[index];
    if (key === "--cdp-endpoint") result.endpoint = argv[++index] || "";
    else if (key === "--viewport-size") index += 1; // Native visible windows own their viewport.
    else throw new Error("Unsupported shared-context MCP argument");
  }
  if (!/^http:\/\/127\.0\.0\.1:\d+$/.test(result.endpoint)) {
    throw new Error("Shared-context MCP requires an explicit loopback CDP endpoint");
  }
  return result;
}

async function main() {
  const args = parseArgs(process.argv.slice(2));
  log(`shared-context connecting endpoint=${args.endpoint}`);
  // External persistent browsers may reject Playwright's default download/
  // artifact setup for the default context (notably after an in-place Chrome
  // update). MCP only needs the already-running context, so do not mutate
  // browser-wide defaults while attaching.
  const browser = await chromium.connectOverCDP(args.endpoint, { noDefaults: true });
  const persistentContext = browser.contexts()[0];
  if (!persistentContext) throw new Error("Attached browser has no persistent default context");

  const connection = await createConnection(
    {
      browser: { browserName: "chromium", isolated: false },
      sharedBrowserContext: true,
    },
    async () => persistentContext,
  );
  const transport = new StdioServerTransport();

  let stopping = false;
  const shutdown = async (exitCode = 0, reason = "requested") => {
    if (stopping) return;
    stopping = true;
    log(`shared-context stopping reason=${reason} exit_code=${exitCode}`);
    try {
      await connection.close().catch(() => undefined);
      // On a CDP attachment Browser.close disconnects this Playwright client;
      // it does not terminate the pre-existing Chrome/Edge process.
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
  log(`shared-context ready endpoint=${args.endpoint}`);
}

main().catch((error) => {
  const detail = errorDetail(error);
  log(`shared-context failed\n${detail}`);
  process.stderr.write(`playwright shared-context MCP failed: ${detail}\n`);
  process.exit(1);
});
