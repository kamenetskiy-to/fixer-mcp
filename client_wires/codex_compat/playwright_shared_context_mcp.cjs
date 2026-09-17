#!/usr/bin/env node
"use strict";

const fs = require("fs");
const Module = require("module");
const path = require("path");

const BACKGROUND_MODE = !/^(1|true|yes)$/i.test(
  process.env.CODEX_PRO_PLAYWRIGHT_ALLOW_FOREGROUND || "",
);
globalThis.__codexProPlaywrightBackgroundMode = BACKGROUND_MODE;

// The Playwright MCP bundle normally brings the selected page to the front
// and creates new Chromium targets in the foreground.  That is especially
// disruptive on macOS when Chrome lives in another Space.  Patch only this
// agent-owned shared-context process; the npm cache and user Chrome remain
// untouched.  Foreground behavior can still be enabled explicitly for
// debugging with CODEX_PRO_PLAYWRIGHT_ALLOW_FOREGROUND=1.
const originalJavaScriptLoader = Module._extensions[".js"];
let patchedBundle = false;
Module._extensions[".js"] = function loadJavaScript(module, filename) {
  if (!BACKGROUND_MODE || !filename.endsWith(path.join("playwright-core", "lib", "coreBundle.js")))
    return originalJavaScriptLoader(module, filename);

  let source = fs.readFileSync(filename, "utf8");
  const replacements = [
    [
      'await this._mainFrameSession._client.send("Page.bringToFront");',
      "return;",
    ],
    [
      'await this._session.send("Page.bringToFront", {});',
      "return;",
    ],
    [
      'const { targetId } = await this._browser._session.send("Target.createTarget", { url: "about:blank", browserContextId: this._browserContextId });',
      'const { targetId } = await this._browser._session.send("Target.createTarget", { url: "about:blank", browserContextId: this._browserContextId, background: true });',
    ],
    [
      "await locator2.click(options);",
      "await (globalThis.__codexProPlaywrightBackgroundMode ? locator2.dispatchEvent(\"click\") : locator2.click(options));",
    ],
    [
      "await locator2.dblclick(options);",
      "await (globalThis.__codexProPlaywrightBackgroundMode ? locator2.dispatchEvent(\"dblclick\") : locator2.dblclick(options));",
    ],
    [
      "const resolved = await locator2.normalize();",
      "const resolved = await locator2.normalize();",
    ],
    [
      "let locator2 = this.page.locator(`aria-ref=${param.target}`);",
      "let locator2 = this.page.locator(`aria-ref=${param.target}`);",
    ],
    [
      "return { locator: locator2, resolved: resolved.toString(), selector: locatorSelector(resolved) };",
      "return { locator: globalThis.__codexProPlaywrightBackgroundMode ? resolved : locator2, resolved: resolved.toString(), selector: locatorSelector(resolved) };",
    ],
    [
      "await tab2.page.bringToFront();",
      "if (!globalThis.__codexProPlaywrightBackgroundMode) await tab2.page.bringToFront();",
    ],
  ];
  for (const [marker, replacement] of replacements) {
    if (!source.includes(marker))
      throw new Error(`Playwright MCP background patch marker not found: ${marker}`);
    source = source.replaceAll(marker, replacement);
  }
  patchedBundle = true;
  return module._compile(source, filename);
};

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
  log(`background_mode=${BACKGROUND_MODE}`);
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

if (BACKGROUND_MODE && !patchedBundle)
  throw new Error("Playwright MCP did not load its core bundle for background patching");
