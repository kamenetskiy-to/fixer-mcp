import { spawn } from "node:child_process";
import { createServer } from "node:http";
import { randomUUID } from "node:crypto";
import readline from "node:readline";
import { URL, fileURLToPath } from "node:url";
import path from "node:path";
import fs from "node:fs";

import {
  ALWAYS_ON_MCP_SERVER,
  allowlistKey,
  ensureAlwaysOnAllowlist,
  mcpServerConfigsFromBody,
  normalizeAllowlist,
  normalizeMcpServerConfigs,
  stableStringify,
} from "./mcp_config.mjs";
import { resolveMcpProfilesForRequest } from "./mcp_profiles.mjs";
import { readThreadTranscript } from "./thread_transcript.mjs";
import { summarizeTurnEvent, summarizeTurnStatus } from "./turn_events.mjs";

const PORT = Number(process.env.CODEX_BRIDGE_PORT ?? "14242");
const CODEX_BIN = process.env.CODEX_BIN ?? "codex";
const CODEX_MCP_ALLOWLIST = (process.env.CODEX_MCP_ALLOWLIST ?? "").trim();
const DEFAULT_CODEX_MODEL = (process.env.CODEX_DEFAULT_MODEL ?? "gpt-5.3-codex").trim();
const DEFAULT_REASONING_EFFORT = (
  process.env.CODEX_DEFAULT_REASONING_EFFORT ?? "high"
).trim();
const DISABLE_CODEX_SQLITE = (process.env.CODEX_BRIDGE_DISABLE_CODEX_SQLITE ?? "1") !== "0";
// Codex may emit very noisy logs like:
//   "ERROR codex_core::rollout::list: state db missing rollout path for thread ..."
// which are not actionable for codex-hub users and can flood local logs.
// Silence that target by default; opt out with `CODEX_BRIDGE_SILENCE_CODEX_ROLLOUT_LIST_LOGS=0`.
const SILENCE_CODEX_ROLLOUT_LIST_LOGS =
  (process.env.CODEX_BRIDGE_SILENCE_CODEX_ROLLOUT_LIST_LOGS ?? "1") !== "0";
const DEFAULT_CODEX_CWD =
  process.env.CODEX_APP_CWD?.trim() ||
  path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const SELF_MCP_DIR = path.join(DEFAULT_CODEX_CWD, "self_mcp_servers");
const STREAM_RETENTION_MS = 90_000;
const MAX_RETAINED_STREAM_EVENTS = 500;

const SANDBOX_MODES = new Set(["read-only", "workspace-write", "danger-full-access"]);
const DEFAULT_SANDBOX_MODE = "danger-full-access";

function readJsonBody(req) {
  return new Promise((resolve, reject) => {
    let data = "";
    req.setEncoding("utf8");
    req.on("data", (chunk) => (data += chunk));
    req.on("end", () => {
      if (!data) return resolve({});
      try {
        resolve(JSON.parse(data));
      } catch (e) {
        reject(e);
      }
    });
    req.on("error", reject);
  });
}

function sendJson(res, status, obj) {
  const body = JSON.stringify(obj);
  res.writeHead(status, {
    "content-type": "application/json; charset=utf-8",
    "content-length": Buffer.byteLength(body),
  });
  res.end(body);
}

function logLine(level, message, extra) {
  const ts = new Date().toISOString();
  if (extra !== undefined) {
    // eslint-disable-next-line no-console
    console[level](`[${ts}] ${message} ${JSON.stringify(extra)}`);
  } else {
    // eslint-disable-next-line no-console
    console[level](`[${ts}] ${message}`);
  }
}

function notFound(res) {
  sendJson(res, 404, { error: "not_found" });
}

function badRequest(res, message) {
  sendJson(res, 400, { error: "bad_request", message });
}

function threadMessagesResponse(threadId) {
  const transcript = readThreadTranscript(threadId);
  return {
    ...transcript,
    sendSupported: true,
    sendEndpoint: "/turn/start",
    streamEndpointTemplate: "/turn/stream/{streamId}",
    turnStatusEndpointTemplate: "/turn/status/{streamId}",
  };
}

function normalizeSandboxMode(raw) {
  if (typeof raw !== "string") return DEFAULT_SANDBOX_MODE;
  const v = raw.trim();
  if (!v) return DEFAULT_SANDBOX_MODE;
  if (!SANDBOX_MODES.has(v)) return null;
  return v;
}

function normalizeModel(raw) {
  if (typeof raw !== "string") return null;
  const model = raw.trim();
  return model.length > 0 ? model : null;
}

const REASONING_EFFORTS = new Set(["none", "minimal", "low", "medium", "high", "xhigh"]);

function normalizeReasoningEffort(raw) {
  if (typeof raw !== "string") return null;
  const normalized = raw.trim().toLowerCase();
  if (!normalized) return null;
  if (
    normalized === "extra high" ||
    normalized === "extra-high" ||
    normalized === "extra_high"
  ) {
    return "xhigh";
  }
  if (!REASONING_EFFORTS.has(normalized)) return null;
  return normalized;
}

const NORMALIZED_DEFAULT_REASONING_EFFORT =
  normalizeReasoningEffort(DEFAULT_REASONING_EFFORT) ?? "high";

function sandboxPolicyForMode(mode, cwd) {
  if (mode === "read-only") return { type: "readOnly" };
  if (mode === "danger-full-access") return { type: "dangerFullAccess" };
  // workspace-write
  const root = typeof cwd === "string" && cwd.trim() ? cwd.trim() : DEFAULT_CODEX_CWD;
  return { type: "workspaceWrite", writableRoots: [root] };
}

class CodexAppServerClient {
  #proc;
  #rl;
  #errRl;
  #nextId = 1;
  #pending = new Map(); // id -> {resolve,reject,method}
  #subscribers = new Set(); // fn(msg)
  #configuredMcpServerNames = [];

  async start() {
    // NOTE: We disable Codex' sqlite feature by default in codex-bridge because in some setups it
    // can produce noisy `state db missing rollout path` logs due to index mismatches. The old
    // non-sqlite listing continues to work without log spam. Set
    // `CODEX_BRIDGE_DISABLE_CODEX_SQLITE=0` to opt back in.
    const args = [];
    if (DISABLE_CODEX_SQLITE) args.push("-c", "features.sqlite=false");
    args.push("app-server");

    const env = { ...process.env };
    if (SILENCE_CODEX_ROLLOUT_LIST_LOGS) {
      const extra = "codex_core::rollout::list=off";
      const current = typeof env.RUST_LOG === "string" ? env.RUST_LOG.trim() : "";
      env.RUST_LOG = current ? `${current},${extra}` : extra;
    }

    this.#proc = spawn(CODEX_BIN, args, {
      stdio: ["pipe", "pipe", "pipe"],
      env,
      cwd: DEFAULT_CODEX_CWD,
    });

    if (!this.#proc.stdin || !this.#proc.stdout || !this.#proc.stderr) {
      throw new Error("Failed to spawn codex app-server (missing stdio)");
    }

    this.#errRl = readline.createInterface({ input: this.#proc.stderr, crlfDelay: Infinity });
    this.#errRl.on("line", (line) => {
      if (
        SILENCE_CODEX_ROLLOUT_LIST_LOGS &&
        typeof line === "string" &&
        line.includes("codex_core::rollout::list: state db missing rollout path for thread")
      ) {
        return;
      }
      process.stderr.write(`${line}\n`);
    });

    this.#proc.on("exit", (code, signal) => {
      const err = new Error(`codex app-server exited (code=${code}, signal=${signal})`);
      for (const { reject } of this.#pending.values()) reject(err);
      this.#pending.clear();
      for (const fn of this.#subscribers) {
        try {
          fn({ type: "bridge/error", error: err.message });
        } catch {
          // ignore
        }
      }
    });

    this.#rl = readline.createInterface({ input: this.#proc.stdout, crlfDelay: Infinity });
    this.#rl.on("line", (line) => {
      let msg;
      try {
        msg = JSON.parse(line);
      } catch {
        return;
      }

      if (msg && typeof msg.id !== "undefined") {
        const entry = this.#pending.get(msg.id);
        if (!entry) return;
        this.#pending.delete(msg.id);
        if (msg.error) {
          logLine("error", "[codex] response error", {
            id: msg.id,
            method: entry.method,
            error: msg.error,
          });
          entry.reject(new Error(msg.error.message ?? "app-server error"));
        } else {
          entry.resolve(msg.result);
        }
        return;
      }

      if (msg && typeof msg.method === "string") {
        for (const fn of this.#subscribers) {
          try {
            fn(msg);
          } catch {
            // ignore subscriber errors
          }
        }
      }
    });

    await this.request("initialize", {
      clientInfo: { name: "codex_hub_poc", title: "Codex Hub PoC", version: "0.0.1" },
    });
    this.notify("initialized", {});
  }

  mcpServerNames = [];

  get configuredMcpServerNames() {
    return this.#configuredMcpServerNames;
  }

  async refreshConfiguredMcpServerNames(cwd) {
    const result = await this.request("config/read", { includeLayers: false, cwd });
    const cfg = result?.config ?? result?.Config ?? null;
    const mcpServers =
      (cfg && (cfg.mcpServers ?? cfg.mcp_servers ?? cfg.mcp)) ||
      (result?.mcpServers ?? result?.mcp_servers) ||
      null;

    const names = mcpServers && typeof mcpServers === "object" ? Object.keys(mcpServers) : [];
    names.sort();
    this.#configuredMcpServerNames = names;
    return names;
  }

  async listAllMcpServerNames() {
    const names = [];
    let cursor = null;
    while (true) {
      const result = await this.request("mcpServerStatus/list", { cursor, limit: 200 });
      const data = Array.isArray(result?.data) ? result.data : [];
      for (const item of data) {
        if (item && typeof item.name === "string") names.push(item.name);
      }
      cursor = result?.nextCursor ?? null;
      if (!cursor) break;
    }
    names.sort();
    return names;
  }

  subscribe(fn) {
    this.#subscribers.add(fn);
    return () => this.#subscribers.delete(fn);
  }

  notify(method, params) {
    const payload = JSON.stringify({ method, params });
    this.#proc.stdin.write(`${payload}\n`);
  }

  request(method, params) {
    const id = this.#nextId++;
    const payload = JSON.stringify({ method, id, params });
    return new Promise((resolve, reject) => {
      this.#pending.set(id, { resolve, reject, method });
      this.#proc.stdin.write(`${payload}\n`);
    });
  }
}

const codex = new CodexAppServerClient();
const codexState = {
  ready: false,
  startError: null,
};

const codexReady = codex
  .start()
  .then(async () => {
    codexState.ready = true;
    try {
      await codex.refreshConfiguredMcpServerNames(DEFAULT_CODEX_CWD);
    } catch (e) {
      logLine("error", "[config] failed to read config", { message: String(e?.message ?? e) });
    }
  })
  .catch((e) => {
    codexState.startError = e;
    throw e;
  });

// streamId -> { res, threadId, turnId, buffer, done, unsubscribe }
const streams = new Map();
const loadedThreads = new Set();
const threadMcpConfigKey = new Map(); // threadId -> stringified allowlist

function sseStart(res) {
  res.writeHead(200, {
    "content-type": "text/event-stream; charset=utf-8",
    "cache-control": "no-cache, no-transform",
    connection: "keep-alive",
  });
  res.write(`:ok\n\n`);
}

function sseSend(res, obj) {
  res.write(`data: ${JSON.stringify(obj)}\n\n`);
}

function sseEnd(res) {
  try {
    res.end();
  } catch {
    // ignore
  }
}

function rememberStreamEvent(entry, msg) {
  const event = summarizeTurnEvent({
    streamId: entry.streamId,
    threadId: entry.threadId,
    turnId: entry.turnId,
    sequence: entry.nextSequence++,
    receivedAt: new Date().toISOString(),
    msg,
  });
  entry.events.push(event);
  if (entry.events.length > MAX_RETAINED_STREAM_EVENTS) {
    entry.events.splice(0, entry.events.length - MAX_RETAINED_STREAM_EVENTS);
  }
  if (entry.res) sseSend(entry.res, event);
  else entry.buffer.push(event);
  return event;
}

function finishStream(entry) {
  if (entry.done) return;
  entry.done = true;
  entry.completedAt = new Date().toISOString();
  if (entry.res) {
    sseEnd(entry.res);
    entry.res = null;
  }
  if (entry.unsubscribe) {
    entry.unsubscribe();
    entry.unsubscribe = null;
  }
  const timer = setTimeout(() => {
    const current = streams.get(entry.streamId);
    if (current === entry && current.done) streams.delete(entry.streamId);
  }, STREAM_RETENTION_MS);
  timer.unref?.();
}

function streamStatusResponse(streamId) {
  const entry = streams.get(streamId);
  if (!entry) {
    return {
      streamId,
      threadId: "",
      turnId: "",
      done: true,
      eventCount: 0,
      startedAt: "",
      completedAt: "",
      assistantText: "",
      progressText:
        "No retained live turn state exists for this stream. It may have expired or never started.",
      events: [],
      expired: true,
    };
  }
  return summarizeTurnStatus(entry);
}

function extractThreadId(msg) {
  const p = msg?.params;
  if (!p) return null;
  if (typeof p.threadId === "string") return p.threadId;
  if (typeof p.thread_id === "string") return p.thread_id;
  if (p.thread && typeof p.thread.id === "string") return p.thread.id;
  if (p.turn && typeof p.turn.threadId === "string") return p.turn.threadId;
  if (p.turn && typeof p.turn.thread_id === "string") return p.turn.thread_id;
  if (p.turn && p.turn.thread && typeof p.turn.thread.id === "string") return p.turn.thread.id;
  return null;
}

function extractTurnId(msg) {
  const p = msg?.params;
  if (!p) return null;
  if (typeof p.turnId === "string") return p.turnId;
  if (typeof p.turn_id === "string") return p.turn_id;
  if (p.turn && typeof p.turn.id === "string") return p.turn.id;
  return null;
}

function hasActiveTurnIdCollision(entry) {
  for (const other of streams.values()) {
    if (other === entry) continue;
    if (other.done) continue;
    if (other.turnId !== entry.turnId) continue;
    if (other.threadId === entry.threadId) continue;
    return true;
  }
  return false;
}

function parseAllowlist(raw) {
  if (!raw) return null;
  const parts = raw
    .split(",")
    .map((s) => s.trim())
    .filter(Boolean);
  return parts.length ? parts : null;
}

function allowlistFromBodyOrEnv(body) {
  if (body && Array.isArray(body.mcpServers)) {
    // Explicit per-request allowlist. Empty array => disable all.
    return body.mcpServers
      .filter((x) => typeof x === "string")
      .map((x) => x.trim())
      .filter((x) => x.length > 0);
  }
  return parseAllowlist(CODEX_MCP_ALLOWLIST);
}

function normalizeMcpProfile(raw) {
  if (typeof raw !== "string") return null;
  const value = raw.trim();
  return value ? value : null;
}

function knownMcpServerNames(extraNames = []) {
  const discovered = Array.isArray(codex.mcpServerNames) ? codex.mcpServerNames : [];
  const configured = Array.isArray(codex.configuredMcpServerNames)
    ? codex.configuredMcpServerNames
    : [];
  const self = listSelfMcpServers();
  const extras = Array.isArray(extraNames) ? extraNames : [];
  const out = [...new Set([...configured, ...discovered, ...self, ...extras])];
  out.sort();
  return out;
}

function sanitizeMcpProfileResolution(resolution, onlyNames = null) {
  const include = onlyNames ? new Set(onlyNames) : null;
  const servers = (resolution.servers ?? [])
    .filter((s) => !include || include.has(s.name))
    .map((s) => ({
      name: s.name,
      profile: s.profile ?? null,
      selectable: Boolean(s.selectable),
      configured: Boolean(s.configured),
      requiresCredentials: Boolean(s.requiresCredentials),
      hasProfileEntry: Boolean(s.hasProfileEntry),
      reason: s.reason ?? null,
      missingVars: Array.isArray(s.missingVars) ? s.missingVars : [],
      configPath: s.configPath ?? null,
    }));
  return {
    profile: resolution.profile ?? null,
    profileSource: resolution.profileSource ?? null,
    profileError: resolution.profileError ?? null,
    configPath: resolution.configPath ?? null,
    availableProfiles: Array.isArray(resolution.availableProfiles)
      ? resolution.availableProfiles
      : [],
    blockedSelected: Array.isArray(resolution.blockedSelected)
      ? resolution.blockedSelected.map((b) => ({
          name: b.name,
          reason: b.reason ?? null,
          missingVars: Array.isArray(b.missingVars) ? b.missingVars : [],
        }))
      : [],
    servers,
  };
}

function summarizeBlockedServers(blockedSelected) {
  if (!Array.isArray(blockedSelected) || blockedSelected.length === 0) return null;
  return blockedSelected
    .map((entry) => {
      const reason = typeof entry?.reason === "string" && entry.reason.trim()
        ? entry.reason.trim()
        : "unavailable";
      return `${entry?.name ?? "unknown"}: ${reason}`;
    })
    .join("; ");
}

function resolveMcpProfileContext({
  cwd,
  allowlist,
  mcpServerConfigs,
  requestedProfile,
}) {
  const known = knownMcpServerNames(allowlist);
  return resolveMcpProfilesForRequest({
    cwd,
    requestedProfile,
    knownServerNames: known,
    allowlist,
    mcpServerConfigs,
  });
}

const PERSONALITIES = new Set(["friendly", "pragmatic", "none"]);

function normalizePromptProfileFromBody(body) {
  const baseRaw = body?.baseInstructions ?? body?.base_instructions ?? null;
  const devRaw =
    body?.developerInstructions ?? body?.developer_instructions ?? null;
  const personalityRaw = body?.personality ?? null;
  const metaRaw = body?.agentPromptProfile ?? body?.agent_prompt_profile ?? null;

  const baseInstructions =
    typeof baseRaw === "string" && baseRaw.trim() ? baseRaw : null;
  const developerInstructions =
    typeof devRaw === "string" && devRaw.trim() ? devRaw : null;

  let personality = null;
  if (typeof personalityRaw === "string") {
    const normalized = personalityRaw.trim().toLowerCase();
    if (PERSONALITIES.has(normalized)) personality = normalized;
  }

  let metadata = null;
  if (metaRaw && typeof metaRaw === "object" && !Array.isArray(metaRaw)) {
    metadata = {};
    if (typeof metaRaw.role === "string" && metaRaw.role.trim()) {
      metadata.role = metaRaw.role.trim();
    }
    if (typeof metaRaw.projectId === "number") metadata.projectId = metaRaw.projectId;
    if (typeof metaRaw.sessionId === "number") metadata.sessionId = metaRaw.sessionId;
    if (typeof metaRaw.fingerprint === "string" && metaRaw.fingerprint.trim()) {
      metadata.fingerprint = metaRaw.fingerprint.trim();
    }
    if (Array.isArray(metaRaw.sourceScopes)) {
      metadata.sourceScopes = metaRaw.sourceScopes
        .filter((x) => typeof x === "string")
        .map((x) => x.trim())
        .filter(Boolean);
    }
    if (Array.isArray(metaRaw.sourceProfileIds)) {
      metadata.sourceProfileIds = metaRaw.sourceProfileIds.filter((x) => Number.isInteger(x));
    }
    if (typeof metaRaw.featureFlagEnabled === "boolean") {
      metadata.featureFlagEnabled = metaRaw.featureFlagEnabled;
    }
    if (typeof metaRaw.willInject === "boolean") {
      metadata.willInject = metaRaw.willInject;
    }
    if (Object.keys(metadata).length === 0) metadata = null;
  }

  const hasOverrides = Boolean(baseInstructions || developerInstructions || personality);
  const overrides = hasOverrides
    ? {
        ...(baseInstructions ? { baseInstructions } : {}),
        ...(developerInstructions ? { developerInstructions } : {}),
        ...(personality ? { personality } : {}),
      }
    : null;

  return { overrides, metadata };
}

function mcpAndPromptConfigKey(allowlist, mcpServerConfigs, promptProfileOverrides) {
  const mcpKey = allowlistKey(allowlist, mcpServerConfigs);
  if (mcpKey == null && !promptProfileOverrides) return null;
  return stableStringify({
    mcpKey,
    promptProfile: promptProfileOverrides ?? {},
  });
}

function normalizeTurnInput(body) {
  const input = body?.input;
  if (!Array.isArray(input)) return null;

  const out = [];
  for (const item of input) {
    if (!item || typeof item !== "object" || Array.isArray(item)) return null;
    const type = typeof item.type === "string" ? item.type.trim() : "";
    if (!type) return null;

    if (type === "text") {
      const text = typeof item.text === "string" ? item.text : null;
      if (text == null) return null;
      out.push({ type: "text", text });
      continue;
    }

    if (type === "skill") {
      const name = typeof item.name === "string" ? item.name.trim() : "";
      const path = typeof item.path === "string" ? item.path.trim() : "";
      if (!name || !path) return null;
      out.push({ type: "skill", name, path });
      continue;
    }

    // Unknown input item type.
    return null;
  }
  return out.length > 0 ? out : null;
}

function tryDescribeSkillFile(p) {
  try {
    if (!fs.existsSync(p)) return { exists: false };
    const stat = fs.statSync(p);
    if (!stat.isFile()) return { exists: true, isFile: false };
    // Try a tiny read to catch obvious permission/path issues early (without logging the full content).
    let headBytes = 0;
    try {
      const fd = fs.openSync(p, "r");
      try {
        const buf = Buffer.alloc(64);
        headBytes = fs.readSync(fd, buf, 0, buf.length, 0);
      } finally {
        fs.closeSync(fd);
      }
    } catch (e) {
      return {
        exists: true,
        isFile: true,
        size: stat.size,
        readable: false,
        readError: String(e?.message ?? e),
      };
    }
    return { exists: true, isFile: true, size: stat.size, readable: true, headBytes };
  } catch (e) {
    return { exists: false, statError: String(e?.message ?? e) };
  }
}

function listSelfMcpServers() {
  try {
    if (!fs.existsSync(SELF_MCP_DIR)) return [];
    const entries = fs.readdirSync(SELF_MCP_DIR, { withFileTypes: true });
    const names = [];
    for (const ent of entries) {
      if (!ent.isDirectory()) continue;
      const mcpJsonPath = path.join(SELF_MCP_DIR, ent.name, "mcp.json");
      if (!fs.existsSync(mcpJsonPath)) continue;
      try {
        const raw = fs.readFileSync(mcpJsonPath, "utf8");
        const parsed = JSON.parse(raw);
        const name = typeof parsed?.name === "string" ? parsed.name.trim() : "";
        if (name) names.push(name);
      } catch {
        // ignore invalid mcp.json
      }
    }
    names.sort();
    return names;
  } catch {
    return [];
  }
}

function loadSelfMcpServerDefinition(name) {
  const normalized = String(name ?? "").trim();
  if (!normalized) return null;
  const mcpJsonPath = path.join(SELF_MCP_DIR, normalized, "mcp.json");
  if (!fs.existsSync(mcpJsonPath)) return null;

  try {
    const raw = fs.readFileSync(mcpJsonPath, "utf8");
    const parsed = JSON.parse(raw);
    if (!parsed || typeof parsed !== "object") return null;
    if (typeof parsed.command !== "string" || !parsed.command.trim()) return null;

    const def = {};
    if (typeof parsed.transport === "string") def.transport = parsed.transport;
    const cmdRaw = parsed.command.trim();
    const cmdNeedsResolve = (cmdRaw.includes("/") || cmdRaw.startsWith(".")) && !path.isAbsolute(cmdRaw);
    def.command = cmdNeedsResolve ? path.resolve(path.dirname(mcpJsonPath), cmdRaw) : cmdRaw;
    if (Array.isArray(parsed.args)) def.args = parsed.args;
    if (parsed.env && typeof parsed.env === "object") def.env = parsed.env;
    if (typeof parsed.startup_timeout_sec === "number") {
      def.startup_timeout_sec = parsed.startup_timeout_sec;
    }
    if (typeof parsed.tool_timeout_sec === "number") def.tool_timeout_sec = parsed.tool_timeout_sec;
    if (typeof parsed.per_tool_timeout_ms === "number") {
      def.per_tool_timeout_ms = parsed.per_tool_timeout_ms;
    }

    if (typeof parsed.cwd === "string" && parsed.cwd.trim()) {
      const cwdRaw = parsed.cwd.trim();
      def.cwd = path.isAbsolute(cwdRaw) ? cwdRaw : path.resolve(path.dirname(mcpJsonPath), cwdRaw);
    }

    return def;
  } catch {
    return null;
  }
}

function buildMcpConfigOverridesAllowlist(allowlist, mcpServerConfigs) {
  if (allowlist == null) return null;
  const discovered = Array.isArray(codex.mcpServerNames) ? codex.mcpServerNames : [];
  const configured = Array.isArray(codex.configuredMcpServerNames)
    ? codex.configuredMcpServerNames
    : [];
  const known = [...new Set([...configured, ...discovered])].sort();

  const allow = new Set(normalizeAllowlist(allowlist) ?? []);
  const normalizedConfigs = normalizeMcpServerConfigs(mcpServerConfigs);
  const mcp_servers = {};

  for (const name of known) {
    mcp_servers[name] = { enabled: allow.has(name) };
  }

  // If allowlist includes a self MCP server that codex app-server didn't discover/configure, define it explicitly.
  for (const name of allow) {
    if (mcp_servers[name]) continue;
    const def = loadSelfMcpServerDefinition(name);
    if (def) mcp_servers[name] = { ...def, enabled: true };
    else mcp_servers[name] = { enabled: true };
  }

  if (normalizedConfigs) {
    for (const [name, cfg] of Object.entries(normalizedConfigs)) {
      if (name === ALWAYS_ON_MCP_SERVER) {
        mcp_servers[name] = { ...(mcp_servers[name] ?? {}), ...cfg, enabled: true };
      } else {
        mcp_servers[name] = { ...(mcp_servers[name] ?? {}), ...cfg };
      }
    }
  }

  return { mcp_servers };
}

function buildMcpConfigOverridesAdditive(names) {
  const normalized = normalizeAllowlist(names);
  if (!normalized || normalized.length === 0) return null;
  const mcp_servers = {};
  for (const name of normalized) {
    const def = loadSelfMcpServerDefinition(name);
    if (def) mcp_servers[name] = { ...def, enabled: true };
  }
  return Object.keys(mcp_servers).length ? { mcp_servers } : null;
}

async function ensureCodexReady(res) {
  try {
    await codexReady;
    return true;
  } catch (e) {
    logLine("error", "[codex] not ready", { message: String(e?.message ?? e) });
    sendJson(res, 503, { error: "codex_unavailable", message: String(e?.message ?? e) });
    return false;
  }
}

async function refreshMcpDiscovery() {
  try {
    await codexReady;
    await codex.refreshConfiguredMcpServerNames(DEFAULT_CODEX_CWD);
  } catch {
    // ignore
  }

  try {
    codex.mcpServerNames = await codex.listAllMcpServerNames();
    logLine("log", "[mcp] discovered servers", { count: codex.mcpServerNames.length });
  } catch (e) {
    codex.mcpServerNames = [];
    logLine("error", "[mcp] failed to list servers", { message: String(e?.message ?? e) });
  }
}

async function syncLoadedThreadsFromServer() {
  try {
    await codexReady;
    const result = await codex.request("thread/loaded/list", {});
    const ids = Array.isArray(result?.data) ? result.data : [];
    for (const id of ids) {
      if (typeof id === "string" && id) loadedThreads.add(id);
    }
    return ids.length;
  } catch {
    return 0;
  }
}

async function applyThreadMcpConfig({
  threadId,
  cwd,
  allowlist,
  mcpServerConfigs,
  promptProfileOverrides,
}) {
  const key = mcpAndPromptConfigKey(allowlist, mcpServerConfigs, promptProfileOverrides);

  const config = buildMcpConfigOverridesAllowlist(allowlist, mcpServerConfigs);
  const isLoaded = loadedThreads.has(threadId);
  const prevKey = threadMcpConfigKey.get(threadId) ?? null;
  const needsResume = !isLoaded || key !== prevKey;
  if (needsResume) {
    const params = { threadId };
    if (config) params.config = config;
    if (promptProfileOverrides) {
      Object.assign(params, promptProfileOverrides);
    }
    await codex.request("thread/resume", params);
    loadedThreads.add(threadId);
    if (key != null) threadMcpConfigKey.set(threadId, key);
    else threadMcpConfigKey.delete(threadId);
  }

  return { applied: needsResume, loaded: loadedThreads.has(threadId), key };
}

function isMissingRolloutError(error) {
  const normalized = String(error?.message ?? error).toLowerCase();
  return (
    normalized.includes("no rollout found for thread id") ||
    normalized.includes("state db missing rollout path for thread")
  );
}

function isThreadNotFoundError(error) {
  const normalized = String(error?.message ?? error).toLowerCase();
  return (
    normalized.includes("thread not found:") ||
    normalized.includes("thread not found")
  );
}

function attachStream(streamId, res) {
  const entry = streams.get(streamId);
  if (!entry) {
    badRequest(res, "unknown streamId");
    return;
  }
  if (entry.res) {
    badRequest(res, "stream already attached");
    return;
  }

  sseStart(res);
  entry.res = res;

  for (const buffered of entry.buffer) {
    sseSend(res, buffered);
  }
  entry.buffer.length = 0;

  if (entry.done) {
    sseEnd(res);
    entry.res = null;
    return;
  }

  res.on("close", () => {
    const e = streams.get(streamId);
    if (!e) return;
    e.res = null;
    if (e.done) return;
    if (e.unsubscribe) e.unsubscribe();
    streams.delete(streamId);
  });
}

const server = createServer(async (req, res) => {
  try {
    const url = new URL(req.url ?? "/", `http://${req.headers.host ?? "localhost"}`);

    if (req.method === "GET" && url.pathname === "/health") {
      return sendJson(res, 200, {
        ok: true,
        codex: {
          ready: codexState.ready,
          startError: codexState.startError ? String(codexState.startError?.message ?? codexState.startError) : null,
        },
        loadedThreads: loadedThreads.size,
        capabilities: {
          threadMessages: {
            read: true,
            readSources: ["codex_jsonl"],
            send: true,
            sendEndpoint: "/turn/start",
            streamEndpointTemplate: "/turn/stream/{streamId}",
            turnStatusEndpointTemplate: "/turn/status/{streamId}",
          },
        },
      });
    }

    if (req.method === "GET" && url.pathname.startsWith("/threads/") && url.pathname.endsWith("/messages")) {
      const threadId = decodeURIComponent(
        url.pathname.slice("/threads/".length, -"/messages".length),
      ).trim();
      if (!threadId) return badRequest(res, "threadId is required");
      return sendJson(res, 200, threadMessagesResponse(threadId));
    }

    if (req.method === "GET" && url.pathname === "/thread/messages") {
      const threadId =
        url.searchParams.get("threadId") ||
        url.searchParams.get("thread_id") ||
        url.searchParams.get("sessionId") ||
        url.searchParams.get("session_id") ||
        url.searchParams.get("codexSessionId") ||
        url.searchParams.get("codex_session_id") ||
        "";
      if (!threadId.trim()) return badRequest(res, "threadId is required");
      return sendJson(res, 200, threadMessagesResponse(threadId));
    }

    if (req.method === "POST" && url.pathname === "/thread/messages/read") {
      const body = await readJsonBody(req);
      const threadId =
        body.threadId ??
        body.thread_id ??
        body.sessionId ??
        body.session_id ??
        body.codexSessionId ??
        body.codex_session_id ??
        "";
      if (typeof threadId !== "string" || !threadId.trim()) {
        return badRequest(res, "threadId is required");
      }
      return sendJson(res, 200, threadMessagesResponse(threadId));
    }

    if (req.method === "GET" && url.pathname === "/mcp/servers") {
      if (!(await ensureCodexReady(res))) return;
      const requestedCwd = url.searchParams.get("cwd");
      const cwd =
        typeof requestedCwd === "string" && requestedCwd.trim()
          ? requestedCwd.trim()
          : DEFAULT_CODEX_CWD;
      const requestedProfile = normalizeMcpProfile(url.searchParams.get("profile"));
      const all = knownMcpServerNames();
      const resolution = resolveMcpProfileContext({
        cwd,
        allowlist: all,
        mcpServerConfigs: null,
        requestedProfile,
      });
      const details = sanitizeMcpProfileResolution(resolution, all);
      return sendJson(res, 200, {
        servers: all,
        ...details,
      });
    }

    if (req.method === "POST" && url.pathname === "/mcp/options") {
      if (!(await ensureCodexReady(res))) return;
      const body = await readJsonBody(req);
      const cwd = typeof body.cwd === "string" ? body.cwd : DEFAULT_CODEX_CWD;
      const requestedProfile = normalizeMcpProfile(body.mcpProfile ?? body.mcp_profile);
      const requestedServers = Array.isArray(body.mcpServers)
        ? body.mcpServers
            .filter((x) => typeof x === "string")
            .map((x) => x.trim())
            .filter(Boolean)
        : null;
      const all = knownMcpServerNames(requestedServers ?? []);
      const allowlist = requestedServers ?? all;
      const resolution = resolveMcpProfileContext({
        cwd,
        allowlist,
        mcpServerConfigs: mcpServerConfigsFromBody(body),
        requestedProfile,
      });
      return sendJson(res, 200, {
        ...sanitizeMcpProfileResolution(resolution, allowlist),
      });
    }

    if (req.method === "POST" && url.pathname === "/skills/list") {
      if (!(await ensureCodexReady(res))) return;
      const body = await readJsonBody(req);
      const cwdsRaw = body && Array.isArray(body.cwds) ? body.cwds : null;
      const cwds =
        cwdsRaw
          ?.filter((x) => typeof x === "string")
          .map((x) => x.trim())
          .filter(Boolean) ?? [];
      const forceReload = body && typeof body.forceReload === "boolean" ? body.forceReload : false;
      const result = await codex.request("skills/list", { cwds, forceReload });
      return sendJson(res, 200, result ?? {});
    }

	    if (req.method === "POST" && url.pathname === "/thread/start") {
	      if (!(await ensureCodexReady(res))) return;
	      const body = await readJsonBody(req);
	      const cwd = typeof body.cwd === "string" ? body.cwd : DEFAULT_CODEX_CWD;
	      const model = normalizeModel(body.model) ?? DEFAULT_CODEX_MODEL;
	      const sandboxMode = normalizeSandboxMode(body.sandboxMode ?? body.sandbox);
	      if (sandboxMode == null) return badRequest(res, "invalid sandboxMode");
	      const requestedProfile = normalizeMcpProfile(body.mcpProfile ?? body.mcp_profile);
	      const baseMcpServerConfigs = mcpServerConfigsFromBody(body);
	      const allowlist = ensureAlwaysOnAllowlist(allowlistFromBodyOrEnv(body));
	      const mcpResolution = resolveMcpProfileContext({
	        cwd,
	        allowlist,
	        mcpServerConfigs: baseMcpServerConfigs,
	        requestedProfile,
	      });
	      if (mcpResolution.profileError) return badRequest(res, mcpResolution.profileError);
	      if (mcpResolution.blockedSelected.length > 0) {
	        return sendJson(res, 400, {
	          error: "mcp_config_invalid",
	          message: summarizeBlockedServers(mcpResolution.blockedSelected),
	          ...sanitizeMcpProfileResolution(mcpResolution, allowlist),
	        });
	      }
	      const mcpServerConfigs = normalizeMcpServerConfigs(mcpResolution.mergedMcpServerConfigs);
	      const promptProfile = normalizePromptProfileFromBody(body);
	      const config = buildMcpConfigOverridesAllowlist(allowlist, mcpServerConfigs);
	      if (allowlist != null) logLine("log", "[mcp] thread/start allowlist", { allowlist });

      const result = await codex.request("thread/start", {
        model,
        cwd,
        approvalPolicy: "never",
        sandbox: sandboxMode,
        config,
        ...(promptProfile.overrides ?? {}),
      });
      const threadId = result?.thread?.id;
      if (threadId) {
        loadedThreads.add(threadId);
        const key = mcpAndPromptConfigKey(
          allowlist,
	          mcpServerConfigs,
          promptProfile.overrides,
        );
        if (key != null) threadMcpConfigKey.set(threadId, key);
      }
      return sendJson(res, 200, {
        threadId: result?.thread?.id,
        mcpProfile: mcpResolution.profile ?? null,
        mcpProfileSource: mcpResolution.profileSource ?? null,
        ...(promptProfile.metadata ? { agentPromptProfile: promptProfile.metadata } : {}),
      });
    }

	    if (req.method === "POST" && url.pathname === "/thread/mcp/apply") {
	      if (!(await ensureCodexReady(res))) return;
	      const body = await readJsonBody(req);
	      const threadId = typeof body.threadId === "string" ? body.threadId.trim() : "";
	      if (!threadId) return badRequest(res, "threadId is required");
	      const cwd = typeof body.cwd === "string" ? body.cwd : DEFAULT_CODEX_CWD;
	      const requestedProfile = normalizeMcpProfile(body.mcpProfile ?? body.mcp_profile);
	      const baseMcpServerConfigs = mcpServerConfigsFromBody(body);
	      const allowlist = ensureAlwaysOnAllowlist(allowlistFromBodyOrEnv(body));
	      const mcpResolution = resolveMcpProfileContext({
	        cwd,
	        allowlist,
	        mcpServerConfigs: baseMcpServerConfigs,
	        requestedProfile,
	      });
	      if (mcpResolution.profileError) return badRequest(res, mcpResolution.profileError);
	      if (mcpResolution.blockedSelected.length > 0) {
	        return sendJson(res, 400, {
	          error: "mcp_config_invalid",
	          message: summarizeBlockedServers(mcpResolution.blockedSelected),
	          ...sanitizeMcpProfileResolution(mcpResolution, allowlist),
	        });
	      }
	      const mcpServerConfigs = normalizeMcpServerConfigs(mcpResolution.mergedMcpServerConfigs);
	      const promptProfile = normalizePromptProfileFromBody(body);

	      const result = await applyThreadMcpConfig({
	        threadId,
	        cwd,
	        allowlist,
	        mcpServerConfigs,
	        promptProfileOverrides: promptProfile.overrides,
	      });
	      return sendJson(res, 200, {
	        ok: true,
	        ...result,
	        mcpProfile: mcpResolution.profile ?? null,
	        mcpProfileSource: mcpResolution.profileSource ?? null,
	        ...(promptProfile.metadata ? { agentPromptProfile: promptProfile.metadata } : {}),
	      });
	    }

	    if (req.method === "POST" && url.pathname === "/turn/start") {
	      if (!(await ensureCodexReady(res))) return;
	      const body = await readJsonBody(req);
	      const threadId = typeof body.threadId === "string" ? body.threadId : null;
	      const prompt = typeof body.prompt === "string" ? body.prompt : null;
	      const input = normalizeTurnInput(body);
	      const cwd = typeof body.cwd === "string" ? body.cwd : DEFAULT_CODEX_CWD;
	      const model = normalizeModel(body.model);
	      const effort =
	        normalizeReasoningEffort(body.effort) ??
	        normalizeReasoningEffort(body.reasoningEffort);
	      const sandboxMode = normalizeSandboxMode(body.sandboxMode ?? body.sandbox);
	      if (sandboxMode == null) return badRequest(res, "invalid sandboxMode");
	      const requestedProfile = normalizeMcpProfile(body.mcpProfile ?? body.mcp_profile);
	      const baseMcpServerConfigs = mcpServerConfigsFromBody(body);
	      // Historical chat turns should resume with their existing thread config
	      // unless the caller explicitly asks for MCP overrides on this send.
	      const allowlist = allowlistFromBodyOrEnv(body);
	      const mcpResolution = resolveMcpProfileContext({
	        cwd,
	        allowlist,
	        mcpServerConfigs: baseMcpServerConfigs,
	        requestedProfile,
	      });
	      if (mcpResolution.profileError) return badRequest(res, mcpResolution.profileError);
	      if (mcpResolution.blockedSelected.length > 0) {
	        return sendJson(res, 400, {
	          error: "mcp_config_invalid",
	          message: summarizeBlockedServers(mcpResolution.blockedSelected),
	          ...sanitizeMcpProfileResolution(mcpResolution, allowlist),
	        });
	      }
	      const mcpServerConfigs = normalizeMcpServerConfigs(mcpResolution.mergedMcpServerConfigs);
	      const promptProfile = normalizePromptProfileFromBody(body);
	      if (allowlist != null) logLine("log", "[mcp] turn/start allowlist", { allowlist });

      if (!threadId) return badRequest(res, "threadId is required");
      if (!input && !prompt) return badRequest(res, "prompt or input is required");

      if (input) {
        for (const item of input) {
          if (item?.type !== "skill") continue;
          const desc = tryDescribeSkillFile(item.path);
          logLine("log", "[skill] input item", {
            threadId,
            name: item.name,
            path: item.path,
            ...desc,
          });
        }
      }

	      try {
	        await applyThreadMcpConfig({
	          threadId,
	          cwd,
	          allowlist,
	          mcpServerConfigs,
	          promptProfileOverrides: promptProfile.overrides,
	        });
	      } catch (e) {
	        if (isMissingRolloutError(e)) {
	          loadedThreads.delete(threadId);
	          threadMcpConfigKey.delete(threadId);
	          const message = String(e?.message ?? e);
	          logLine("warn", "[turn/start] missing rollout during thread resume", {
	            threadId,
	            message,
	          });
	          return sendJson(res, 409, {
	            error: "missing_thread_rollout",
	            threadId,
	            message,
	          });
	        }
	        throw e;
	      }

      const streamId = randomUUID();

      // Collect any early events that arrive before we know the turn id.
      const preEvents = [];
      const unsubPre = codex.subscribe((msg) => {
        const tid = extractThreadId(msg);
        if (tid && tid === threadId) preEvents.push(msg);
      });

	      const result = await codex.request("turn/start", {
	        threadId,
	        input: input ?? [{ type: "text", text: prompt }],
	        cwd,
	        approvalPolicy: "never",
	        sandboxPolicy: sandboxPolicyForMode(sandboxMode, cwd),
	        ...(model ? { model } : {}),
	        ...(effort ? { effort } : {}),
	      });

      unsubPre();

      const turnId = result?.turn?.id;
      if (!turnId) return sendJson(res, 500, { error: "missing_turn_id" });

      const entry = {
        streamId,
        res: null,
        threadId,
        turnId,
        buffer: [],
        events: [],
        done: false,
        startedAt: new Date().toISOString(),
        completedAt: "",
        nextSequence: 1,
        unsubscribe: null,
      };
      streams.set(streamId, entry);

      for (const msg of preEvents) {
        const msgTurnId = extractTurnId(msg);
        if (msgTurnId && msgTurnId === turnId) {
          rememberStreamEvent(entry, msg);
        }
      }

	      const unsubscribe = codex.subscribe((msg) => {
	        const entry = streams.get(streamId);
	        if (!entry) return;

	        const msgThreadId = extractThreadId(msg);
	        const msgTurnId = extractTurnId(msg);
	        if (msgThreadId) {
	          if (msgThreadId !== entry.threadId) return;
	          if (msgTurnId && msgTurnId !== entry.turnId) return;
	        } else {
	          // Some events may omit `threadId`; only route by turn id when this
	          // turn id is unambiguous across active streams.
	          if (!msgTurnId || msgTurnId !== entry.turnId) return;
	          if (hasActiveTurnIdCollision(entry)) return;
	        }

	        rememberStreamEvent(entry, msg);

        if (msg.method === "turn/completed") {
          const completedTurnId = extractTurnId(msg);
          if (!completedTurnId || completedTurnId === entry.turnId) {
            finishStream(entry);
          }
        }
      });

      entry.unsubscribe = unsubscribe;
      if (entry.done && entry.unsubscribe) {
        entry.unsubscribe();
        entry.unsubscribe = null;
      }

	      return sendJson(res, 200, {
	        streamId,
	        threadId,
	        turnId,
	        streamEndpoint: `/turn/stream/${streamId}`,
	        turnStatusEndpoint: `/turn/status/${streamId}`,
	        mcpProfile: mcpResolution.profile ?? null,
	        mcpProfileSource: mcpResolution.profileSource ?? null,
	        ...(promptProfile.metadata ? { agentPromptProfile: promptProfile.metadata } : {}),
	      });
	    }

	    if (req.method === "POST" && url.pathname === "/thread/compact/start") {
	      if (!(await ensureCodexReady(res))) return;
	      const body = await readJsonBody(req);
	      const threadId = typeof body.threadId === "string" ? body.threadId.trim() : "";
	      if (!threadId) return badRequest(res, "threadId is required");

	      const streamId = randomUUID();

	      // Collect any early events that arrive before we know the compaction turn id.
	      const preEvents = [];
	      const unsubPre = codex.subscribe((msg) => {
	        const tid = extractThreadId(msg);
	        if (tid && tid === threadId) preEvents.push(msg);
	      });

	      try {
	        // Compact can be triggered long after thread creation. Ensure the thread
	        // is resumed in current runtime after app-server/backend restarts.
	        await applyThreadMcpConfig({
	          threadId,
	          cwd: DEFAULT_CODEX_CWD,
	          allowlist: null,
	          mcpServerConfigs: null,
	          promptProfileOverrides: null,
	        });
	      } catch (e) {
	        if (isMissingRolloutError(e) || isThreadNotFoundError(e)) {
	          loadedThreads.delete(threadId);
	          threadMcpConfigKey.delete(threadId);
	          const message = String(e?.message ?? e);
	          logLine("warn", "[thread/compact/start] missing rollout during thread resume", {
	            threadId,
	            message,
	          });
	          unsubPre();
	          return sendJson(res, 409, {
	            error: "missing_thread_rollout",
	            threadId,
	            message,
	          });
	        }
	        unsubPre();
	        throw e;
	      }

	      try {
	        await codex.request("thread/compact/start", { threadId });
	      } catch (e) {
	        // `loadedThreads` can become stale if app-server state resets while
	        // bridge remains up. Force one explicit resume + retry in that case.
	        if (isThreadNotFoundError(e)) {
	          loadedThreads.delete(threadId);
	          threadMcpConfigKey.delete(threadId);
	          try {
	            await codex.request("thread/resume", { threadId });
	            loadedThreads.add(threadId);
	            await codex.request("thread/compact/start", { threadId });
	          } catch (retryError) {
	            if (isMissingRolloutError(retryError) || isThreadNotFoundError(retryError)) {
	              const message = String(retryError?.message ?? retryError);
	              logLine("warn", "[thread/compact/start] missing rollout during retry", {
	                threadId,
	                message,
	              });
	              unsubPre();
	              return sendJson(res, 409, {
	                error: "missing_thread_rollout",
	                threadId,
	                message,
	              });
	            }
	            unsubPre();
	            throw retryError;
	          }
	        } else if (isMissingRolloutError(e)) {
	          loadedThreads.delete(threadId);
	          threadMcpConfigKey.delete(threadId);
	          const message = String(e?.message ?? e);
	          logLine("warn", "[thread/compact/start] missing rollout during compact", {
	            threadId,
	            message,
	          });
	          unsubPre();
	          return sendJson(res, 409, {
	            error: "missing_thread_rollout",
	            threadId,
	            message,
	          });
	        } else {
	          unsubPre();
	          throw e;
	        }
	      }

	      // Compaction runs as a regular turn internally, but turn id is not part of
	      // the request response. Discover it from early notifications for this thread.
	      let turnId = null;
	      const discoverStartedAt = Date.now();
	      while (!turnId && Date.now() - discoverStartedAt < 12000) {
	        // Preferred signal: explicit turn/started.
	        for (let i = 0; i < preEvents.length; i++) {
	          const candidate = preEvents[i];
	          if (candidate?.method !== "turn/started") continue;
	          const candidateTurnId = extractTurnId(candidate);
	          if (candidateTurnId) {
	            turnId = candidateTurnId;
	            break;
	          }
	        }
	        // Fallback signal: any event tied to this thread with turn id.
	        if (!turnId) {
	          for (let i = 0; i < preEvents.length; i++) {
	            const candidate = preEvents[i];
	            const candidateTurnId = extractTurnId(candidate);
	            if (candidateTurnId) {
	              turnId = candidateTurnId;
	              break;
	            }
	          }
	        }
	        if (turnId) break;
	        await new Promise((resolve) => setTimeout(resolve, 20));
	      }

	      unsubPre();

	      if (!turnId) {
	        const observedMethods = [...new Set(preEvents.map((e) => e?.method).filter(Boolean))];
	        logLine("warn", "[thread/compact/start] missing turn id", {
	          threadId,
	          observedEvents: preEvents.length,
	          observedMethods,
	        });
	        return sendJson(res, 500, {
	          error: "missing_turn_id",
	          message: "thread/compact/start did not emit turn id in time",
	        });
	      }

	      const entry = {
	        streamId,
	        res: null,
	        threadId,
	        turnId,
	        buffer: [],
	        events: [],
	        done: false,
	        startedAt: new Date().toISOString(),
	        completedAt: "",
	        nextSequence: 1,
	        unsubscribe: null,
	      };
	      streams.set(streamId, entry);

	      for (const msg of preEvents) {
	        const msgTurnId = extractTurnId(msg);
	        if (msgTurnId && msgTurnId === turnId) {
	          rememberStreamEvent(entry, msg);
	        }
	      }

	      const unsubscribe = codex.subscribe((msg) => {
	        const entry = streams.get(streamId);
	        if (!entry) return;

	        const msgThreadId = extractThreadId(msg);
	        const msgTurnId = extractTurnId(msg);
	        if (msgThreadId) {
	          if (msgThreadId !== entry.threadId) return;
	          if (msgTurnId && msgTurnId !== entry.turnId) return;
	        } else {
	          // Some events may omit `threadId`; only route by turn id when this
	          // turn id is unambiguous across active streams.
	          if (!msgTurnId || msgTurnId !== entry.turnId) return;
	          if (hasActiveTurnIdCollision(entry)) return;
	        }

	        rememberStreamEvent(entry, msg);

	        if (msg.method === "turn/completed") {
	          const completedTurnId = extractTurnId(msg);
	          if (!completedTurnId || completedTurnId === entry.turnId) {
	            finishStream(entry);
	          }
	        }
	      });

	      entry.unsubscribe = unsubscribe;
	      if (entry.done && entry.unsubscribe) {
	        entry.unsubscribe();
	        entry.unsubscribe = null;
	      }

	      return sendJson(res, 200, {
	        streamId,
	        threadId,
	        turnId,
	        streamEndpoint: `/turn/stream/${streamId}`,
	        turnStatusEndpoint: `/turn/status/${streamId}`,
	      });
	    }

	    if (req.method === "POST" && url.pathname === "/model/list") {
	      if (!(await ensureCodexReady(res))) return;
	      const body = await readJsonBody(req);
	      const limit = typeof body.limit === "number" ? body.limit : 100;
	      const cursor = typeof body.cursor === "string" ? body.cursor : null;
	      const result = await codex.request("model/list", { limit, cursor });
	      return sendJson(res, 200, result ?? {});
	    }

    if (req.method === "POST" && url.pathname === "/account/rate-limits/read") {
      if (!(await ensureCodexReady(res))) return;
      const result = await codex.request("account/rateLimits/read", {});
      return sendJson(res, 200, result ?? {});
    }

    if (req.method === "GET" && url.pathname.startsWith("/turn/status/")) {
      const streamId = url.pathname.slice("/turn/status/".length).trim();
      if (!streamId) return badRequest(res, "streamId is required");
      return sendJson(res, 200, streamStatusResponse(streamId));
    }

    if (req.method === "GET" && url.pathname === "/turn/status") {
      const streamId = (url.searchParams.get("streamId") || url.searchParams.get("stream_id") || "").trim();
      if (!streamId) return badRequest(res, "streamId is required");
      return sendJson(res, 200, streamStatusResponse(streamId));
    }

    if (req.method === "GET" && url.pathname.startsWith("/turn/stream/")) {
      const streamId = url.pathname.slice("/turn/stream/".length);
      if (!streamId) return badRequest(res, "streamId is required");
      if (!streams.has(streamId)) return badRequest(res, "unknown streamId");
      attachStream(streamId, res);
      return;
    }

    if (req.method === "POST" && url.pathname === "/turn/interrupt") {
      if (!(await ensureCodexReady(res))) return;
      const body = await readJsonBody(req);
      const threadId = typeof body.threadId === "string" ? body.threadId.trim() : "";
      const turnId = typeof body.turnId === "string" ? body.turnId.trim() : "";
      if (!threadId) return badRequest(res, "threadId is required");
      if (!turnId) return badRequest(res, "turnId is required");

      // See Codex App Server JSON-RPC docs: `turn/interrupt`.
      await codex.request("turn/interrupt", { threadId, turnId });
      return sendJson(res, 200, { ok: true });
    }

    return notFound(res);
  } catch (e) {
    logLine("error", "[http] handler error", { message: String(e?.message ?? e) });
    return sendJson(res, 500, { error: "internal_error", message: String(e?.message ?? e) });
  }
});

server.listen(PORT, "127.0.0.1", () => {
  logLine("log", `codex-bridge listening on http://127.0.0.1:${PORT}`);
});

server.on("request", (req, res) => {
  const startedAt = Date.now();
  const requestId = randomUUID().slice(0, 8);
  res.on("finish", () => {
    const ms = Date.now() - startedAt;
    logLine(
      "log",
      `[http] ${requestId} ${req.method} ${req.url} -> ${res.statusCode} (${ms}ms)`,
    );
  });
});
