import assert from "node:assert/strict";
import test from "node:test";

import {
  extractTurnTextDelta,
  summarizeTurnEvent,
  summarizeTurnStatus,
} from "../turn_events.mjs";

test("extracts text deltas from common Codex turn notification shapes", () => {
  assert.equal(
    extractTurnTextDelta({ method: "turn/delta", params: { delta: "Hello" } }),
    "Hello",
  );
  assert.equal(
    extractTurnTextDelta({
      method: "response/item",
      params: {
        item: {
          content: [{ type: "output_text", text: " from Codex" }],
        },
      },
    }),
    "from Codex",
  );
});

test("summarizes retained stream events into a pollable turn status", () => {
  const entry = {
    streamId: "stream-1",
    threadId: "thread-1",
    turnId: "turn-1",
    done: false,
    startedAt: "2026-04-28T12:00:00.000Z",
    completedAt: "",
    events: [
      summarizeTurnEvent({
        streamId: "stream-1",
        threadId: "thread-1",
        turnId: "turn-1",
        sequence: 1,
        receivedAt: "2026-04-28T12:00:01.000Z",
        msg: { method: "turn/started", params: { turnId: "turn-1" } },
      }),
      summarizeTurnEvent({
        streamId: "stream-1",
        threadId: "thread-1",
        turnId: "turn-1",
        sequence: 2,
        receivedAt: "2026-04-28T12:00:02.000Z",
        msg: { method: "turn/delta", params: { text_delta: "Working" } },
      }),
    ],
  };

  const status = summarizeTurnStatus(entry);
  assert.equal(status.eventCount, 2);
  assert.equal(status.assistantText, "Working");
  assert.equal(status.progressText, "Working");
  assert.equal(status.events[0].phase, "started");
  assert.equal(status.events[1].phase, "assistant_delta");
});

test("surfaces turn error messages in progress text", () => {
  const entry = {
    streamId: "stream-1",
    threadId: "thread-1",
    turnId: "turn-1",
    done: true,
    startedAt: "2026-04-28T12:00:00.000Z",
    completedAt: "2026-04-28T12:00:03.000Z",
    events: [
      summarizeTurnEvent({
        streamId: "stream-1",
        threadId: "thread-1",
        turnId: "turn-1",
        sequence: 1,
        receivedAt: "2026-04-28T12:00:01.000Z",
        msg: { method: "item/started", params: { turnId: "turn-1" } },
      }),
      summarizeTurnEvent({
        streamId: "stream-1",
        threadId: "thread-1",
        turnId: "turn-1",
        sequence: 2,
        receivedAt: "2026-04-28T12:00:02.000Z",
        msg: {
          method: "error",
          params: {
            turnId: "turn-1",
            error: { message: "Error running remote compact task" },
          },
        },
      }),
    ],
  };

  const status = summarizeTurnStatus(entry);
  assert.equal(status.progressText, "Error running remote compact task");
});

test("does not include user message items in assistant text", () => {
  const entry = {
    streamId: "stream-1",
    threadId: "thread-1",
    turnId: "turn-1",
    done: true,
    startedAt: "2026-04-28T12:00:00.000Z",
    completedAt: "2026-04-28T12:00:02.000Z",
    events: [
      summarizeTurnEvent({
        streamId: "stream-1",
        threadId: "thread-1",
        turnId: "turn-1",
        sequence: 1,
        receivedAt: "2026-04-28T12:00:01.000Z",
        msg: {
          method: "item/started",
          params: {
            item: { type: "userMessage", text: "please reply with OK" },
          },
        },
      }),
      summarizeTurnEvent({
        streamId: "stream-1",
        threadId: "thread-1",
        turnId: "turn-1",
        sequence: 2,
        receivedAt: "2026-04-28T12:00:02.000Z",
        msg: { method: "turn/delta", params: { delta: "OK" } },
      }),
    ],
  };

  const status = summarizeTurnStatus(entry);
  assert.equal(status.assistantText, "OK");
});

test("does not duplicate a completed agent message after streaming the same text", () => {
  const entry = {
    streamId: "stream-1",
    threadId: "thread-1",
    turnId: "turn-1",
    done: true,
    startedAt: "2026-04-28T12:00:00.000Z",
    completedAt: "2026-04-28T12:00:02.000Z",
    events: [
      summarizeTurnEvent({
        streamId: "stream-1",
        threadId: "thread-1",
        turnId: "turn-1",
        sequence: 1,
        receivedAt: "2026-04-28T12:00:01.000Z",
        msg: { method: "turn/delta", params: { delta: "OK" } },
      }),
      summarizeTurnEvent({
        streamId: "stream-1",
        threadId: "thread-1",
        turnId: "turn-1",
        sequence: 2,
        receivedAt: "2026-04-28T12:00:02.000Z",
        msg: {
          method: "item/completed",
          params: {
            item: { type: "agentMessage", text: "OK" },
          },
        },
      }),
    ],
  };

  const status = summarizeTurnStatus(entry);
  assert.equal(status.assistantText, "OK");
});
