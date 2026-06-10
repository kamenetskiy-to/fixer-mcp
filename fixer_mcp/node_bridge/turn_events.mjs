import { extractMessageText } from "./thread_transcript.mjs";

const TEXT_DELTA_KEYS = new Set([
  "delta",
  "textDelta",
  "text_delta",
  "contentDelta",
  "content_delta",
]);

export function summarizeTurnEvent({ streamId, threadId, turnId, sequence, receivedAt, msg }) {
  const method = typeof msg?.method === "string" ? msg.method : "event";
  const textDelta = extractTurnTextDelta(msg);
  return {
    streamId,
    threadId,
    turnId,
    sequence,
    receivedAt,
    method,
    phase: phaseForMethod(method, textDelta),
    textDelta,
    msg,
  };
}

export function summarizeTurnStatus(entry) {
  const events = Array.isArray(entry?.events) ? entry.events : [];
  const assistantText = collectAssistantText(events);
  const errorText = [...events].reverse().map((event) => extractTurnErrorText(event.msg)).find(Boolean);
  const recentMethods = events
    .slice(-4)
    .map((event) => event.method)
    .filter(Boolean);
  const progressText =
    assistantText ||
    errorText ||
    (recentMethods.length > 0
      ? recentMethods.join(" -> ")
      : entry?.done
        ? "Turn completed."
        : "Turn started; waiting for Codex events.");

  return {
    streamId: entry?.streamId ?? "",
    threadId: entry?.threadId ?? "",
    turnId: entry?.turnId ?? "",
    done: Boolean(entry?.done),
    eventCount: events.length,
    startedAt: entry?.startedAt ?? "",
    completedAt: entry?.completedAt ?? "",
    assistantText,
    progressText,
    events,
  };
}

function collectAssistantText(events) {
  let text = "";
  for (const event of events) {
    const delta = typeof event?.textDelta === "string" ? event.textDelta : "";
    if (!delta) continue;
    if (isUserMessageItem(event.msg)) continue;
    if (isCompletedMessageItem(event.msg) && text.endsWith(delta)) continue;
    text += delta;
  }
  return text;
}

function extractTurnErrorText(msg) {
  const direct = msg?.params?.error?.message;
  if (typeof direct === "string" && direct.trim()) return direct.trim();
  const turnError = msg?.params?.turn?.error?.message;
  if (typeof turnError === "string" && turnError.trim()) return turnError.trim();
  return "";
}

export function extractTurnTextDelta(msg) {
  const params = msg?.params;
  if (!params || typeof params !== "object") return "";

  const direct = directTextDelta(params);
  if (direct) return direct;

  const item = params.item ?? params.responseItem ?? params.response_item ?? params.message;
  if (isUserMessageObject(item)) return "";
  const itemText = extractMessageObjectText(item);
  if (itemText) return itemText;

  const turn = params.turn;
  if (turn && typeof turn === "object") {
    const turnText = directTextDelta(turn) || extractMessageObjectText(turn.message);
    if (turnText) return turnText;
  }

  return "";
}

function isUserMessageItem(msg) {
  const params = msg?.params;
  if (!params || typeof params !== "object") return false;
  return isUserMessageObject(params.item ?? params.responseItem ?? params.response_item ?? params.message);
}

function isUserMessageObject(item) {
  if (!item || typeof item !== "object") return false;
  return item.type === "userMessage" || item.role === "user";
}

function isCompletedMessageItem(msg) {
  const method = typeof msg?.method === "string" ? msg.method : "";
  if (method !== "item/completed" && method !== "response/item/completed") return false;
  const params = msg?.params;
  if (!params || typeof params !== "object") return false;
  const item = params.item ?? params.responseItem ?? params.response_item ?? params.message;
  return item && typeof item === "object";
}

function directTextDelta(obj) {
  if (!obj || typeof obj !== "object") return "";
  for (const key of TEXT_DELTA_KEYS) {
    const value = obj[key];
    if (typeof value === "string" && value.length > 0) return value;
  }
  return "";
}

function extractMessageObjectText(obj) {
  if (!obj || typeof obj !== "object") return "";
  if (typeof obj.text === "string") return obj.text;
  if (typeof obj.content === "string") return obj.content;
  if (Array.isArray(obj.content)) return extractMessageText(obj.content);
  return "";
}

function phaseForMethod(method, textDelta) {
  if (textDelta) return "assistant_delta";
  if (method === "turn/started") return "started";
  if (method === "turn/completed") return "completed";
  if (method.toLowerCase().includes("error")) return "error";
  return "progress";
}
