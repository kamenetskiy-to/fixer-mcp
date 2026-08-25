#!/usr/bin/env node
'use strict';

const childProcess = require('child_process');
const fs = require('fs');
const Module = require('module');
const os = require('os');
const path = require('path');

const OFFICIAL_EXTENSION_ID = 'mmlmfjhmonkocbjadbfplnigmagldckm';
const ALL_TABS_EXTENSION_ID = 'ffmbpfogmjdlbhnepmmhgahagoemdjkm';
const EDGE_EXECUTABLE = '/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge';
const LOG_PATH = process.env.CODEX_PRO_EDGE_LOG ||
  '/home/operator/Desktop/projects/mcp_servers/logs/edge_mcp.log';
const TAB_PROBE_TIMEOUT_MS = positiveIntegerEnv('CODEX_PRO_EDGE_TAB_PROBE_TIMEOUT_MS', 3000);
const NEW_TAB_PROBE_TIMEOUT_MS = positiveIntegerEnv('CODEX_PRO_EDGE_NEW_TAB_PROBE_TIMEOUT_MS', 10000);
const TAB_HEADER_TIMEOUT_MS = positiveIntegerEnv('CODEX_PRO_EDGE_TAB_HEADER_TIMEOUT_MS', 2000);
const SNAPSHOT_TIMEOUT_MS = positiveIntegerEnv('CODEX_PRO_EDGE_SNAPSHOT_TIMEOUT_MS', 5000);
const TRACE_ENABLED = /^(1|true|yes)$/i.test(process.env.CODEX_PRO_EDGE_TRACE || '');
const BACKGROUND_MODE = !/^(1|true|yes)$/i.test(process.env.CODEX_PRO_EDGE_ALLOW_FOREGROUND || '');
const EDGE_BUNDLE_ID = 'com.microsoft.edgemac';
const focusGuardProcesses = new Set();
const unavailableHeaderTabs = new WeakSet();
const INSTANCE_LOCK_PATH = process.env.CODEX_PRO_EDGE_LOCK ||
  path.join(os.tmpdir(), 'codex-pro-edge-mcp.lock');
const INSTANCE_LOCK_TOKEN = `${process.pid}-${process.hrtime.bigint()}`;
let ownsInstanceLock = false;
let shuttingDown = false;

function positiveIntegerEnv(name, fallback) {
  const parsed = Number.parseInt(process.env[name] || '', 10);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : fallback;
}

function log(message) {
  try {
    fs.mkdirSync(path.dirname(LOG_PATH), { recursive: true });
    fs.appendFileSync(LOG_PATH, `${new Date().toISOString()} ${message}\n`);
  } catch {
    // MCP stdio must remain clean even if diagnostics cannot be written.
  }
}

function processIsAlive(pid) {
  if (!Number.isInteger(pid) || pid <= 0)
    return false;
  try {
    process.kill(pid, 0);
    return true;
  } catch (error) {
    return error?.code === 'EPERM';
  }
}

function processIsEdgeMcp(pid) {
  const result = childProcess.spawnSync(
    '/bin/ps',
    ['-p', String(pid), '-o', 'command='],
    { encoding: 'utf8', timeout: 2000 },
  );
  return result.status === 0 && result.stdout.includes(path.basename(__filename));
}

function sleepSync(milliseconds) {
  const blocker = new Int32Array(new SharedArrayBuffer(4));
  Atomics.wait(blocker, 0, 0, milliseconds);
}

function readInstanceLock() {
  try {
    return JSON.parse(fs.readFileSync(INSTANCE_LOCK_PATH, 'utf8'));
  } catch {
    return undefined;
  }
}

function removeInstanceLockIfOwned() {
  if (!ownsInstanceLock)
    return;
  const record = readInstanceLock();
  if (record?.token === INSTANCE_LOCK_TOKEN) {
    try {
      fs.unlinkSync(INSTANCE_LOCK_PATH);
    } catch {
      // A replacement may already have removed the old lock.
    }
  }
  ownsInstanceLock = false;
}

function acquireSingleInstanceLock() {
  const deadline = Date.now() + 10000;
  fs.mkdirSync(path.dirname(INSTANCE_LOCK_PATH), { recursive: true });

  while (Date.now() < deadline) {
    try {
      const descriptor = fs.openSync(INSTANCE_LOCK_PATH, 'wx', 0o600);
      fs.writeFileSync(descriptor, JSON.stringify({
        pid: process.pid,
        parentPid: process.ppid,
        token: INSTANCE_LOCK_TOKEN,
        startedAt: new Date().toISOString(),
      }));
      fs.closeSync(descriptor);
      ownsInstanceLock = true;
      log(`singleton acquired lock=${INSTANCE_LOCK_PATH}`);
      return;
    } catch (error) {
      if (error?.code !== 'EEXIST')
        fail(`could not acquire singleton lock: ${error.message}`);
    }

    const existing = readInstanceLock();
    const existingPid = Number(existing?.pid);
    const sameLauncher = processIsAlive(existingPid) && processIsEdgeMcp(existingPid);
    if (!sameLauncher) {
      try {
        fs.unlinkSync(INSTANCE_LOCK_PATH);
      } catch {
        // Another contender may already have replaced the stale lock.
      }
      continue;
    }

    log(`singleton takeover previous_pid=${existingPid} previous_parent_pid=${existing.parentPid ?? 'unknown'}`);
    try {
      process.kill(existingPid, 'SIGTERM');
    } catch {
      // The previous instance may have exited after the liveness check.
    }
    const gracefulDeadline = Date.now() + 3000;
    while (processIsAlive(existingPid) && Date.now() < gracefulDeadline)
      sleepSync(50);
    if (processIsAlive(existingPid)) {
      log(`singleton forcing previous_pid=${existingPid}`);
      try {
        process.kill(existingPid, 'SIGKILL');
      } catch {
        // The previous instance may have exited between checks.
      }
      const forcedDeadline = Date.now() + 2000;
      while (processIsAlive(existingPid) && Date.now() < forcedDeadline)
        sleepSync(50);
    }

    const current = readInstanceLock();
    if (current?.token === existing?.token) {
      try {
        fs.unlinkSync(INSTANCE_LOCK_PATH);
      } catch {
        // Another contender may already have cleaned the old lock.
      }
    }
  }

  fail(`could not take singleton ownership within 10000ms: ${INSTANCE_LOCK_PATH}`);
}

function stopForSignal(signal) {
  if (shuttingDown)
    return;
  shuttingDown = true;
  log(`stopping signal=${signal}`);
  for (const guard of focusGuardProcesses)
    guard.kill('SIGTERM');
  removeInstanceLockIfOwned();
  process.exit(0);
}

function frontmostBundleIdentifier() {
  if (process.platform !== 'darwin')
    return undefined;
  const result = childProcess.spawnSync(
    '/usr/bin/osascript',
    ['-e', 'tell application "System Events" to get bundle identifier of first application process whose frontmost is true'],
    { encoding: 'utf8', timeout: 2000 },
  );
  if (result.status !== 0)
    return undefined;
  return result.stdout.trim() || undefined;
}

globalThis.__codexProEdgeTrace = message => {
  if (TRACE_ENABLED)
    log(`relay ${message}`);
};
globalThis.__codexProEdgeTabProbeTimeoutMs = TAB_PROBE_TIMEOUT_MS;
globalThis.__codexProEdgeNewTabProbeTimeoutMs = NEW_TAB_PROBE_TIMEOUT_MS;
globalThis.__codexProEdgeSnapshotTimeoutMs = SNAPSHOT_TIMEOUT_MS;
globalThis.__codexProEdgeTabHeaderSnapshot = async (tab, index) => {
  const fallback = () => ({
    title: '[unresponsive tab]',
    url: tab?.page?.url?.() || '',
    current: Boolean(tab?.isCurrentTab?.()),
    crashed: Boolean(tab?.crashed),
    console: { total: 0, errors: 0, warnings: 0 },
    changed: true,
  });
  if (unavailableHeaderTabs.has(tab))
    return fallback();
  try {
    return await globalThis.__codexProEdgeWithTimeout(
      tab.headerSnapshot(),
      TAB_HEADER_TIMEOUT_MS,
      `tab header ${index}`,
    );
  } catch (error) {
    unavailableHeaderTabs.add(tab);
    globalThis.__codexProEdgeTrace?.(`using header fallback index=${index} error=${error.message}`);
    return fallback();
  }
};
globalThis.__codexProEdgeBackgroundMode = BACKGROUND_MODE;
globalThis.__codexProEdgeCaptureFrontmostApp = () => undefined;
globalThis.__codexProEdgeRestoreFrontmostApp = async bundleIdentifier => {
  if (!BACKGROUND_MODE || !bundleIdentifier || process.platform !== 'darwin')
    return;
  if (frontmostBundleIdentifier() !== EDGE_BUNDLE_ID || bundleIdentifier === EDGE_BUNDLE_ID)
    return;
  const script = [
    'on run argv',
    'tell application "System Events"',
    'set targetBundle to item 1 of argv',
    'set frontmost of first application process whose bundle identifier is targetBundle to true',
    'end tell',
    'end run',
  ].join('\n');
  await new Promise(resolve => {
    childProcess.execFile(
      '/usr/bin/osascript',
      ['-e', script, bundleIdentifier],
      { timeout: 3000 },
      () => resolve(),
    );
  });
};
globalThis.__codexProEdgeStartFocusGuard = () => undefined;
globalThis.__codexProEdgeStopFocusGuard = async () => {};
globalThis.__codexProEdgeWithTimeout = (promise, timeoutMs, label) => new Promise((resolve, reject) => {
  const timer = setTimeout(() => reject(new Error(`${label} timed out after ${timeoutMs}ms`)), timeoutMs);
  promise.then(
    value => {
      clearTimeout(timer);
      resolve(value);
    },
    error => {
      clearTimeout(timer);
      reject(error);
    },
  );
});

function fail(message) {
  log(`startup failed: ${message}`);
  process.stderr.write(`Edge MCP startup failed: ${message}\n`);
  process.exit(1);
}

function cachedPlaywrightMcpCli() {
  const override = process.env.CODEX_PRO_PLAYWRIGHT_MCP_CLI;
  if (override) {
    try {
      return { cliPath: fs.realpathSync(override), source: 'environment override' };
    } catch (error) {
      fail(`CODEX_PRO_PLAYWRIGHT_MCP_CLI is invalid: ${error.message}`);
    }
  }

  const npxCache = path.join(os.homedir(), '.npm', '_npx');
  let cacheEntries = [];
  try {
    cacheEntries = fs.readdirSync(npxCache, { withFileTypes: true });
  } catch {
    return undefined;
  }

  const candidates = [];
  for (const entry of cacheEntries) {
    if (!entry.isDirectory())
      continue;
    const packageDir = path.join(npxCache, entry.name, 'node_modules', '@playwright', 'mcp');
    const candidate = path.join(packageDir, 'cli.js');
    const packageJson = path.join(packageDir, 'package.json');
    try {
      const packageMetadata = JSON.parse(fs.readFileSync(packageJson, 'utf8'));
      const version = String(packageMetadata.version || '')
        .split(/[.-]/)
        .map(part => Number.parseInt(part, 10) || 0);
      candidates.push({
        cliPath: fs.realpathSync(candidate),
        version,
        modifiedMs: fs.statSync(packageJson).mtimeMs,
      });
    } catch {
      // Ignore incomplete or unrelated npx cache entries.
    }
  }

  candidates.sort((left, right) => {
    const length = Math.max(left.version.length, right.version.length);
    for (let index = 0; index < length; index++) {
      const difference = (right.version[index] || 0) - (left.version[index] || 0);
      if (difference)
        return difference;
    }
    return right.modifiedMs - left.modifiedMs;
  });
  return candidates[0] && { cliPath: candidates[0].cliPath, source: 'npx cache' };
}

function resolvePlaywrightMcpCli() {
  const cached = cachedPlaywrightMcpCli();
  if (cached)
    return cached;

  const resolution = childProcess.spawnSync(
    'npm',
    ['exec', '--yes', '--package=@playwright/mcp@latest', '--', 'sh', '-c', 'command -v playwright-mcp'],
    { encoding: 'utf8', timeout: 60000 },
  );
  if (resolution.error)
    fail(`could not resolve @playwright/mcp: ${resolution.error.message}`);
  if (resolution.status !== 0)
    fail(`could not resolve @playwright/mcp (exit ${resolution.status}): ${resolution.stderr.trim()}`);

  try {
    return { cliPath: fs.realpathSync(resolution.stdout.trim()), source: 'npm fallback' };
  } catch (error) {
    fail(`resolved Playwright MCP entrypoint is invalid: ${error.message}`);
  }
}

if (!fs.existsSync(EDGE_EXECUTABLE))
  fail(`Microsoft Edge executable was not found at ${EDGE_EXECUTABLE}`);

const { cliPath, source: cliResolutionSource } = resolvePlaywrightMcpCli();

const originalJavaScriptLoader = Module._extensions['.js'];
let patchedBundle = false;
Module._extensions['.js'] = function loadJavaScript(module, filename) {
  if (!filename.endsWith(path.join('playwright-core', 'lib', 'coreBundle.js')))
    return originalJavaScriptLoader(module, filename);

  const source = fs.readFileSync(filename, 'utf8');
  if (!source.includes(OFFICIAL_EXTENSION_ID))
    fail('the installed Playwright core bundle no longer contains the expected extension ID');
  let patched = source.replaceAll(OFFICIAL_EXTENSION_ID, ALL_TABS_EXTENSION_ID);
  const sendMarker = 'const id = ++this._lastId;';
  const responseMarker = 'if (object.id && this._callbacks.has(object.id)) {';
  const playwrightMarker = 'const { id, sessionId, method, params: params2 } = message;';
  const tabProbeMarker = 'const targetInfo = result2?.targetInfo;';
  const backgroundLaunchMarker = '(0, import_child_process6.spawn)(executablePath, args, {';
  const newTabMarker = 'const tab2 = await this._sendToExtension("chrome.tabs.create", [{ url: url3 }]);';
  const downloadBehaviorMarker = 'case "Browser.setDownloadBehavior": {';
  const connectPageMarker = 'this._openConnectPageInBrowser(clientName);';
  const relayReadyMarker = 'await this._handler.ready();';
  const toolCallMarker = 'async callTool(name, rawArguments = {}, signal) {';
  const toolFinallyMarker = 'context2.setRunningTool(void 0);';
  const attachTabMarker = 'async _attachTab(tabId) {';
  const createTargetAttachMarker = 'const tabSession = await this._attachTab(tab2.id);';
  const tabHeaderListMarker = 'const tabHeaders = await Promise.all(context2.tabs().map((tab2) => tab2.headerSnapshot()));';
  const responseTabHeaderMarker = 'const tabHeaders = await Promise.all(this._context.tabs().map((tab2) => tab2.headerSnapshot()));';
  const responseSnapshotMarker = 'const tabSnapshot = this._context.currentTab() ? await this._context.currentTabOrDie().captureSnapshot(this._includeSnapshotRoot, this._includeSnapshotDepth, this._includeSnapshotBoxes, this._clientWorkspace) : void 0;';
  const ariaSnapshotMarker = 'const ariaSnapshot = root ? await root.ariaSnapshot({ mode: "ai", depth, boxes }) : await this.page.ariaSnapshot({ mode: "ai", depth, boxes });';
  for (const marker of [
    sendMarker,
    responseMarker,
    playwrightMarker,
    tabProbeMarker,
    backgroundLaunchMarker,
    newTabMarker,
    downloadBehaviorMarker,
    connectPageMarker,
    relayReadyMarker,
    toolCallMarker,
    toolFinallyMarker,
    attachTabMarker,
    createTargetAttachMarker,
    tabHeaderListMarker,
    responseTabHeaderMarker,
    responseSnapshotMarker,
    ariaSnapshotMarker,
  ]) {
    if (!patched.includes(marker))
      fail(`the installed Playwright core bundle no longer contains patch marker: ${marker}`);
  }
  patched = patched
    .replace(
      sendMarker,
      `${sendMarker}\n        globalThis.__codexProEdgeTrace?.(\`extension send id=\${id} method=\${method}\`);`,
    )
    .replace(
      responseMarker,
      `${responseMarker}\n          globalThis.__codexProEdgeTrace?.(\`extension response id=\${object.id} error=\${Boolean(object.error)}\`);`,
    )
    .replace(
      playwrightMarker,
      `${playwrightMarker}\n        globalThis.__codexProEdgeTrace?.(\`playwright request id=\${id} session=\${sessionId ?? 'root'} method=\${method}\`);`,
    )
    .replace(
      tabProbeMarker,
      `${tabProbeMarker}
        try {
          await globalThis.__codexProEdgeWithTimeout(
            this._sendToExtension("chrome.debugger.sendCommand", [
              { tabId },
              "Page.getFrameTree"
            ]),
            probeTimeoutMs,
            "tab " + tabId + " Page.getFrameTree probe"
          );
        } catch (error) {
          globalThis.__codexProEdgeTrace?.("skipping unresponsive tabId=" + tabId + " error=" + error.message);
          this._knownTabs.delete(tabId);
          await globalThis.__codexProEdgeWithTimeout(
            this._sendToExtension("chrome.debugger.detach", [{ tabId }]),
            1000,
            "tab " + tabId + " debugger detach"
          ).catch(() => {});
          throw error;
        }`,
    )
    .replace(
      backgroundLaunchMarker,
      `const launchThroughBroker = globalThis.__codexProEdgeBackgroundMode && process.platform === "darwin";
        const brokerPayload = JSON.stringify({ relayUrl: mcpRelayEndpoint, protocolVersion: this._protocolVersion });
        (0, import_child_process6.spawn)(
          launchThroughBroker ? "/usr/bin/curl" : executablePath,
          launchThroughBroker ? ["--silent", "--show-error", "--fail", "--max-time", "3", "--header", "content-type: application/json", "--data-binary", brokerPayload, "http://127.0.0.1:55712/connect"] : args,
          {`,
    )
    .replace(
      newTabMarker,
      'const tab2 = await this._sendToExtension("chrome.tabs.create", [{ url: url3, active: !globalThis.__codexProEdgeBackgroundMode }]);',
    )
    .replace(
      downloadBehaviorMarker,
      `case "Page.bringToFront": {
            if (globalThis.__codexProEdgeBackgroundMode) {
              globalThis.__codexProEdgeTrace?.("suppressed Page.bringToFront");
              return {};
            }
            break;
          }
          ${downloadBehaviorMarker}`,
    )
    .replace(
      connectPageMarker,
      `const previousFrontmostApp = globalThis.__codexProEdgeCaptureFrontmostApp?.();
        const focusGuard = globalThis.__codexProEdgeStartFocusGuard?.(previousFrontmostApp);
        ${connectPageMarker}`,
    )
    .replace(
      relayReadyMarker,
      `${relayReadyMarker}
        await globalThis.__codexProEdgeStopFocusGuard?.(focusGuard, previousFrontmostApp);`,
    )
    .replace(
      toolCallMarker,
      `${toolCallMarker}
        const previousFrontmostApp = globalThis.__codexProEdgeCaptureFrontmostApp?.();
        const focusGuard = globalThis.__codexProEdgeStartFocusGuard?.(previousFrontmostApp);`,
    )
    .replace(
      toolFinallyMarker,
      `${toolFinallyMarker}
          await globalThis.__codexProEdgeStopFocusGuard?.(focusGuard, previousFrontmostApp);`,
    )
    .replace(
      attachTabMarker,
      'async _attachTab(tabId, probeTimeoutMs = globalThis.__codexProEdgeTabProbeTimeoutMs) {',
    )
    .replace(
      createTargetAttachMarker,
      'const tabSession = await this._attachTab(tab2.id, globalThis.__codexProEdgeNewTabProbeTimeoutMs);',
    )
    .replace(
      tabHeaderListMarker,
      'const tabHeaders = await Promise.all(context2.tabs().map((tab2, index) => globalThis.__codexProEdgeTabHeaderSnapshot(tab2, index)));',
    )
    .replace(
      responseTabHeaderMarker,
      'const tabHeaders = await Promise.all(this._context.tabs().map((tab2, index) => globalThis.__codexProEdgeTabHeaderSnapshot(tab2, index)));',
    )
    .replace(
      responseSnapshotMarker,
      `let tabSnapshot;
        if (this._includeSnapshot !== "none" && this._context.currentTab()) {
          try {
            tabSnapshot = await this._context.currentTabOrDie().captureSnapshot(this._includeSnapshotRoot, this._includeSnapshotDepth, this._includeSnapshotBoxes, this._clientWorkspace);
          } catch (error) {
            if (this._includeSnapshot === "explicit")
              throw error;
            globalThis.__codexProEdgeTrace?.("omitting post-tool snapshot error=" + error.message);
          }
        }`,
    )
    .replace(
      ariaSnapshotMarker,
      'const ariaSnapshot = root ? await root.ariaSnapshot({ mode: "ai", depth, boxes, timeout: globalThis.__codexProEdgeSnapshotTimeoutMs }) : await this.page.ariaSnapshot({ mode: "ai", depth, boxes, timeout: globalThis.__codexProEdgeSnapshotTimeoutMs });',
    );
  patchedBundle = true;
  module._compile(patched, filename);
};

process.on('exit', code => {
  for (const guard of focusGuardProcesses)
    guard.kill('SIGTERM');
  removeInstanceLockIfOwned();
  log(`exit code=${code}`);
});
process.once('SIGTERM', () => stopForSignal('SIGTERM'));
process.once('SIGINT', () => stopForSignal('SIGINT'));
process.on('uncaughtException', error => {
  fail(error?.stack || error?.message || String(error));
});

acquireSingleInstanceLock();

const forwardedArgs = process.argv.slice(2);
if (!forwardedArgs.includes('--timeout-action'))
  forwardedArgs.push('--timeout-action', '10000');
if (!forwardedArgs.includes('--snapshot-mode'))
  forwardedArgs.push('--snapshot-mode', 'none');

process.argv = [
  process.execPath,
  cliPath,
  '--browser',
  'msedge',
  '--extension',
  '--executable-path',
  EDGE_EXECUTABLE,
  ...forwardedArgs,
];

log(`starting extension=${ALL_TABS_EXTENSION_ID} cli=${cliPath} cli_source=${cliResolutionSource} tab_probe_timeout_ms=${TAB_PROBE_TIMEOUT_MS} new_tab_probe_timeout_ms=${NEW_TAB_PROBE_TIMEOUT_MS} tab_header_timeout_ms=${TAB_HEADER_TIMEOUT_MS} snapshot_timeout_ms=${SNAPSHOT_TIMEOUT_MS} background_mode=${BACKGROUND_MODE}`);
require(cliPath);
if (!patchedBundle)
  fail('Playwright MCP did not load its core bundle');
