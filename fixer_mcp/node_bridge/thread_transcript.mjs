import os from "node:os";
import path from "node:path";
import fs from "node:fs";

const DEFAULT_CODEX_SESSIONS_DIR = path.join(os.homedir(), ".codex", "sessions");
const MESSAGE_ROLES = new Set(["user", "assistant"]);
const DEFAULT_SESSION_SCAN_LIMIT = 240;

function sessionsDir() {
  const raw = process.env.CODEX_SESSIONS_DIR?.trim();
  return raw || DEFAULT_CODEX_SESSIONS_DIR;
}

function asTextPart(part) {
  if (!part || typeof part !== "object") return "";
  if (typeof part.text === "string") return part.text;
  if (typeof part.content === "string") return part.content;
  return "";
}

export function extractMessageText(content) {
  if (typeof content === "string") return content.trim();
  if (!Array.isArray(content)) return "";
  return content.map(asTextPart).filter(Boolean).join("\n").trim();
}

function walkJsonlFiles(root, out) {
  let entries;
  try {
    entries = fs.readdirSync(root, { withFileTypes: true });
  } catch {
    return;
  }

  entries.sort((left, right) => right.name.localeCompare(left.name));
  for (const entry of entries) {
    const p = path.join(root, entry.name);
    if (entry.isDirectory()) {
      walkJsonlFiles(p, out);
    } else if (entry.isFile() && entry.name.endsWith(".jsonl")) {
      out.push(p);
    }
  }
}

export function findCodexSessionLogPath(threadId, { maxFiles = DEFAULT_SESSION_SCAN_LIMIT } = {}) {
  const normalized = String(threadId ?? "").trim();
  if (!normalized) return null;

  const root = sessionsDir();
  const candidates = [];
  walkJsonlFiles(root, candidates);

  const rankedCandidates = candidates
    .map((candidate) => ({
      path: candidate,
      direct: path.basename(candidate).includes(normalized),
      mtimeMs: safeMtimeMs(candidate),
    }))
    .sort((left, right) => right.mtimeMs - left.mtimeMs);

  const toInspect = [
    ...rankedCandidates.filter((candidate) => candidate.direct),
    ...rankedCandidates.filter((candidate) => !candidate.direct).slice(0, maxFiles),
  ];
  const seenPaths = new Set();
  const matches = toInspect
    .filter((candidate) => {
      if (seenPaths.has(candidate.path)) return false;
      seenPaths.add(candidate.path);
      return true;
    })
    .map((candidate) => {
      const meta = inspectSessionLogIdentity(candidate.path);
      if (!candidate.direct && meta.threadId !== normalized) return null;
      return {
        path: candidate.path,
        lastActivityAt: meta.lastActivityAt,
        mtimeMs: candidate.mtimeMs,
      };
    })
    .filter(Boolean);

  matches.sort((left, right) => {
    const byActivity = right.lastActivityAt.localeCompare(left.lastActivityAt);
    if (byActivity !== 0) return byActivity;
    return right.mtimeMs - left.mtimeMs;
  });
  return matches[0]?.path ?? null;
}

function inspectSessionLogIdentity(filePath) {
  let threadId = "";
  let lastActivityAt = "";
  try {
    const fd = fs.openSync(filePath, "r");
    try {
      const buffer = Buffer.alloc(1024 * 1024);
      const bytesRead = fs.readSync(fd, buffer, 0, buffer.length, 0);
      const lines = buffer.subarray(0, bytesRead).toString("utf8").split(/\r?\n/);
      for (const line of lines) {
        if (!line.trim()) continue;
        let envelope;
        try {
          envelope = JSON.parse(line);
        } catch {
          continue;
        }
        if (typeof envelope?.timestamp === "string") lastActivityAt = envelope.timestamp;
        if (envelope?.type === "session_meta" && typeof envelope?.payload?.id === "string") {
          threadId = envelope.payload.id;
        }
      }
    } finally {
      fs.closeSync(fd);
    }
  } catch {
    // ignore unreadable logs
  }
  return { threadId, lastActivityAt };
}

function safeMtimeMs(filePath) {
  try {
    return fs.statSync(filePath).mtimeMs;
  } catch {
    return 0;
  }
}

export function parseCodexSessionLog(filePath, { limit = 120 } = {}) {
  const messages = [];
  const toolCalls = new Map();
  let threadId = "";
  let cwd = "";
  let startedAt = "";
  let lastActivityAt = "";

  const raw = fs.readFileSync(filePath, "utf8");
  const lines = raw.split(/\r?\n/);

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i].trim();
    if (!line) continue;

    let envelope;
    try {
      envelope = JSON.parse(line);
    } catch {
      continue;
    }

    if (typeof envelope?.timestamp === "string") lastActivityAt = envelope.timestamp;

    if (envelope?.type === "session_meta" && envelope.payload) {
      if (typeof envelope.payload.id === "string") threadId = envelope.payload.id;
      if (typeof envelope.payload.cwd === "string") cwd = envelope.payload.cwd;
      if (typeof envelope.payload.timestamp === "string") startedAt = envelope.payload.timestamp;
      continue;
    }

    if (envelope?.type === "user_message" || envelope?.type === "assistant_message") {
      const role = envelope.type === "user_message" ? "user" : "assistant";
      const text = extractLegacyEnvelopeText(envelope.payload);
      if (!text) continue;
      const meta = messageMeta(role, text);
      messages.push({
        id: `${path.basename(filePath)}:${i}`,
        role,
        text,
        kind: meta.kind,
        summary: meta.summary,
        collapsed: meta.collapsed,
        createdAt: typeof envelope.timestamp === "string" ? envelope.timestamp : "",
        source: "codex_jsonl",
      });
      continue;
    }

    if (envelope?.type !== "response_item") continue;
    const payload = envelope.payload;
    if (!payload) continue;

    if (payload.type === "function_call") {
      const toolMessage = toolCallMessage(filePath, i, envelope, payload);
      toolCalls.set(payload.call_id, toolMessage);
      messages.push(toolMessage);
      continue;
    }

    if (payload.type === "function_call_output") {
      const callId = typeof payload.call_id === "string" ? payload.call_id : "";
      const output = typeof payload.output === "string" ? payload.output : "";
      const existing = toolCalls.get(callId);
      if (existing) {
        existing.text = `${existing.text}\n\nOutput:\n${output}`.trim();
      } else if (output) {
        messages.push({
          id: `${path.basename(filePath)}:${i}`,
          role: "tool",
          text: output,
          kind: "tool_result",
          summary: "Tool result",
          collapsed: true,
          createdAt: typeof envelope.timestamp === "string" ? envelope.timestamp : "",
          source: "codex_jsonl",
        });
      }
      continue;
    }

    if (payload.type !== "message") continue;
    if (!MESSAGE_ROLES.has(payload.role)) continue;

    const text = extractMessageText(payload.content);
    if (!text) continue;
    const meta = messageMeta(payload.role, text);
    messages.push({
      id: `${path.basename(filePath)}:${i}`,
      role: payload.role,
      text,
      kind: meta.kind,
      summary: meta.summary,
      collapsed: meta.collapsed,
      createdAt: typeof envelope.timestamp === "string" ? envelope.timestamp : "",
      source: "codex_jsonl",
    });
  }

  return {
    threadId,
    cwd,
    startedAt,
    lastActivityAt,
    messages: limit > 0 && messages.length > limit ? messages.slice(messages.length - limit) : messages,
  };
}

function messageMeta(role, text) {
  if (role === "user" && text.startsWith("# AGENTS.md instructions for ")) {
    return {
      kind: "internal_context",
      summary: "Internal context: AGENTS.md and environment",
      collapsed: true,
    };
  }
  if (role === "user" && text.startsWith("<skill>\n")) {
    const match = text.match(/<name>([^<]+)<\/name>/);
    const skillName = match?.[1]?.trim();
    return {
      kind: "internal_context",
      summary: skillName ? `Internal skill context: ${skillName}` : "Internal skill context",
      collapsed: true,
    };
  }
  return { kind: "message", summary: "", collapsed: false };
}

function toolCallMessage(filePath, lineIndex, envelope, payload) {
  const label = toolCallLabel(payload);
  const args = typeof payload.arguments === "string" ? payload.arguments : "";
  const text = args ? `${label}\n\nArguments:\n${formatToolArguments(args)}` : label;
  return {
    id: `${path.basename(filePath)}:${lineIndex}`,
    role: "tool",
    text,
    kind: "tool_call",
    summary: label,
    collapsed: true,
    createdAt: typeof envelope.timestamp === "string" ? envelope.timestamp : "",
    source: "codex_jsonl",
  };
}

function toolCallLabel(payload) {
  const name = typeof payload.name === "string" ? payload.name.trim() : "tool";
  const namespace = typeof payload.namespace === "string" ? payload.namespace.trim() : "";
  const prefix = namespace ? `${normalizeToolNamespace(namespace)}.` : "";
  const args = typeof payload.arguments === "string" ? payload.arguments : "";
  return `Called ${prefix}${name}(${compactToolArguments(args)})`;
}

function normalizeToolNamespace(namespace) {
  if (namespace.startsWith("mcp__") && namespace.endsWith("__")) {
    return namespace.slice("mcp__".length, -"__".length);
  }
  return namespace;
}

function compactToolArguments(args) {
  const trimmed = args.trim();
  if (!trimmed || trimmed === "{}") return "{}";
  try {
    const parsed = JSON.parse(trimmed);
    const compact = JSON.stringify(parsed);
    if (compact === "{}") return "{}";
    return compact.length > 180 ? `${compact.slice(0, 177)}...` : compact;
  } catch {
    return trimmed.length > 180 ? `${trimmed.slice(0, 177)}...` : trimmed;
  }
}

function formatToolArguments(args) {
  try {
    return JSON.stringify(JSON.parse(args), null, 2);
  } catch {
    return args;
  }
}

function extractLegacyEnvelopeText(payload) {
  if (!payload || typeof payload !== "object") return "";
  if (typeof payload.text === "string") return payload.text.trim();
  if (typeof payload.message === "string") return payload.message.trim();
  return extractMessageText(payload.content);
}

export function readThreadTranscript(threadId, options = {}) {
  const normalized = String(threadId ?? "").trim();
  if (!normalized) {
    return {
      threadId: "",
      transcriptAvailable: false,
      availability: "unsupported",
      unsupportedReason: "threadId is required",
      messages: [],
    };
  }

  const filePath = findCodexSessionLogPath(normalized, options);
  if (!filePath) {
    return {
      threadId: normalized,
      transcriptAvailable: false,
      availability: "not_found",
      unsupportedReason: "No local Codex JSONL rollout log was found for this thread.",
      messages: [],
    };
  }

  const parsed = parseCodexSessionLog(filePath, options);
  return {
    threadId: parsed.threadId || normalized,
    transcriptAvailable: parsed.messages.length > 0,
    availability: parsed.messages.length > 0 ? "codex_jsonl" : "metadata_only",
    unsupportedReason:
      parsed.messages.length > 0
        ? ""
        : "The Codex log exists, but no user or assistant message records were extractable.",
    sessionLogPath: filePath,
    cwd: parsed.cwd,
    startedAt: parsed.startedAt,
    lastActivityAt: parsed.lastActivityAt,
    messages: parsed.messages,
  };
}
