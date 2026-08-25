import os from "node:os";
import path from "node:path";
import fs from "node:fs";

const DEFAULT_CODEX_SESSIONS_DIR = path.join(os.homedir(), ".codex", "sessions");
const MESSAGE_ROLES = new Set(["user", "assistant"]);
const DEFAULT_SESSION_SCAN_LIMIT = 240;

function sessionsDir() {
  const raw =
    process.env.CODEX_SESSIONS_DIR?.trim() ||
    process.env.FIXER_CODEX_SESSION_ROOT?.trim();
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
  let model = "";
  let reasoning = "";

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

    if (envelope?.type === "turn_context" && envelope.payload) {
      if (typeof envelope.payload.model === "string") model = envelope.payload.model.trim();
      if (typeof envelope.payload.effort === "string") {
        reasoning = envelope.payload.effort.trim();
      }
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
    model,
    reasoning,
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

const DEFAULT_ANTIGRAVITY_ROOT = path.join(os.homedir(), ".gemini", "antigravity-cli");

export function findAntigravitySessionLogPath(threadId) {
  const normalized = String(threadId ?? "").trim();
  if (!normalized) return null;
  const agyRoot = process.env.ANTIGRAVITY_ROOT?.trim() || DEFAULT_ANTIGRAVITY_ROOT;
  const transcriptPath = path.join(
    agyRoot,
    "brain",
    normalized,
    ".system_generated",
    "logs",
    "transcript.jsonl",
  );
  if (fs.existsSync(transcriptPath)) return transcriptPath;
  return null;
}

function extractAntigravityUserText(content) {
  if (typeof content !== "string") return "";
  let text = content.trim();
  const reqMatch = text.match(/<USER_REQUEST>([\s\S]*?)<\/USER_REQUEST>/);
  if (reqMatch) {
    text = reqMatch[1].trim();
  }
  return text;
}

export function parseAntigravitySessionLog(filePath, threadId, { limit = 120 } = {}) {
  const messages = [];
  let cwd = "";
  let startedAt = "";
  let lastActivityAt = "";
  let model = "Gemini 3.7 Flash";
  let reasoning = "medium";

  const agyRoot = process.env.ANTIGRAVITY_ROOT?.trim() || DEFAULT_ANTIGRAVITY_ROOT;
  try {
    const historyPath = path.join(agyRoot, "history.jsonl");
    if (fs.existsSync(historyPath)) {
      const historyRaw = fs.readFileSync(historyPath, "utf8");
      for (const line of historyRaw.split(/\r?\n/)) {
        if (!line.trim()) continue;
        try {
          const rec = JSON.parse(line);
          if (rec.conversationId === threadId) {
            if (rec.workspace) cwd = rec.workspace;
            if (typeof rec.timestamp === "number" && rec.timestamp > 0) {
              startedAt = new Date(rec.timestamp).toISOString();
              lastActivityAt = startedAt;
            } else if (typeof rec.timestamp === "string") {
              startedAt = rec.timestamp;
              lastActivityAt = rec.timestamp;
            }
            if (rec.model) model = rec.model;
            if (rec.reasoning) reasoning = rec.reasoning;
            break;
          }
        } catch {}
      }
    }
  } catch {}

  const raw = fs.readFileSync(filePath, "utf8");
  const lines = raw.split(/\r?\n/);

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i].trim();
    if (!line) continue;
    let step;
    try {
      step = JSON.parse(line);
    } catch {
      continue;
    }

    const timestamp = typeof step.created_at === "string" ? step.created_at : "";
    if (timestamp) lastActivityAt = timestamp;
    if (!startedAt && timestamp) startedAt = timestamp;

    if (step.type === "USER_INPUT" || step.source === "USER_EXPLICIT") {
      const text = extractAntigravityUserText(step.content);
      if (!text) continue;
      const meta = messageMeta("user", text);
      messages.push({
        id: `${path.basename(filePath)}:${i}`,
        role: "user",
        text,
        kind: meta.kind,
        summary: meta.summary,
        collapsed: meta.collapsed,
        createdAt: timestamp,
        source: "antigravity_jsonl",
      });
      continue;
    }

    if (step.type === "PLANNER_RESPONSE") {
      if (Array.isArray(step.tool_calls) && step.tool_calls.length > 0) {
        for (const tc of step.tool_calls) {
          const name = tc.name || "tool";
          const args = typeof tc.args === "object" ? JSON.stringify(tc.args) : String(tc.args || "");
          const label = `Called ${name}(${compactToolArguments(args)})`;
          const text = args ? `${label}\n\nArguments:\n${formatToolArguments(args)}` : label;
          messages.push({
            id: `${path.basename(filePath)}:${i}:${name}`,
            role: "tool",
            text,
            kind: "tool_call",
            summary: label,
            collapsed: true,
            createdAt: timestamp,
            source: "antigravity_jsonl",
          });
        }
      }
      if (typeof step.content === "string" && step.content.trim()) {
        const text = step.content.trim();
        const meta = messageMeta("assistant", text);
        messages.push({
          id: `${path.basename(filePath)}:${i}`,
          role: "assistant",
          text,
          kind: meta.kind,
          summary: meta.summary,
          collapsed: meta.collapsed,
          createdAt: timestamp,
          source: "antigravity_jsonl",
        });
      }
      continue;
    }

    if (step.source === "MODEL" && step.type !== "PLANNER_RESPONSE") {
      const content = typeof step.content === "string" ? step.content.trim() : "";
      if (content) {
        messages.push({
          id: `${path.basename(filePath)}:${i}`,
          role: "tool",
          text: content,
          kind: "tool_result",
          summary: `${step.type} result`,
          collapsed: true,
          createdAt: timestamp,
          source: "antigravity_jsonl",
        });
      }
      continue;
    }
  }

  return {
    threadId,
    cwd,
    startedAt,
    lastActivityAt,
    model,
    reasoning,
    messages: limit > 0 && messages.length > limit ? messages.slice(messages.length - limit) : messages,
  };
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

  const agyPath = findAntigravitySessionLogPath(normalized);
  if (agyPath) {
    const parsed = parseAntigravitySessionLog(agyPath, normalized, options);
    return {
      threadId: parsed.threadId || normalized,
      transcriptAvailable: parsed.messages.length > 0,
      availability: parsed.messages.length > 0 ? "antigravity_jsonl" : "metadata_only",
      unsupportedReason:
        parsed.messages.length > 0
          ? ""
          : "The Antigravity transcript exists, but no messages were extractable.",
      sessionLogPath: agyPath,
      cwd: parsed.cwd,
      startedAt: parsed.startedAt,
      lastActivityAt: parsed.lastActivityAt,
      model: parsed.model,
      reasoning: parsed.reasoning,
      messages: parsed.messages,
    };
  }

  const filePath = findCodexSessionLogPath(normalized, options);
  if (!filePath) {
    return {
      threadId: normalized,
      transcriptAvailable: false,
      availability: "not_found",
      unsupportedReason: "No local Codex or Antigravity JSONL rollout log was found for this thread.",
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
    model: parsed.model,
    reasoning: parsed.reasoning,
    messages: parsed.messages,
  };
}
