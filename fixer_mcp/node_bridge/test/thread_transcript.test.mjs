import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import test from "node:test";

import {
  extractMessageText,
  findCodexSessionLogPath,
  readThreadTranscript,
} from "../thread_transcript.mjs";

test("extracts text from Codex message content arrays", () => {
  assert.equal(
    extractMessageText([
      { type: "input_text", text: "hello" },
      { type: "output_text", text: "world" },
    ]),
    "hello\nworld",
  );
});

test("reads user and assistant messages from a Codex rollout log", () => {
  const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "thread-transcript-"));
  const sessionsDir = path.join(tempDir, "sessions", "2026", "04", "28");
  fs.mkdirSync(sessionsDir, { recursive: true });
  const threadId = "019test-thread";
  const filePath = path.join(sessionsDir, `rollout-2026-04-28T12-00-00-${threadId}.jsonl`);
  fs.writeFileSync(
    filePath,
    [
      JSON.stringify({
        timestamp: "2026-04-28T12:00:00Z",
        type: "session_meta",
        payload: { id: threadId, cwd: "/tmp/project", timestamp: "2026-04-28T12:00:00Z" },
      }),
      JSON.stringify({
        timestamp: "2026-04-28T12:00:01Z",
        type: "response_item",
        payload: {
          type: "message",
          role: "user",
          content: [{ type: "input_text", text: "Build the thing" }],
        },
      }),
      JSON.stringify({
        timestamp: "2026-04-28T12:00:02Z",
        type: "response_item",
        payload: {
          type: "message",
          role: "assistant",
          content: [{ type: "output_text", text: "On it." }],
        },
      }),
    ].join("\n"),
  );

  const previousSessionsDir = process.env.CODEX_SESSIONS_DIR;
  process.env.CODEX_SESSIONS_DIR = path.join(tempDir, "sessions");
  try {
    assert.equal(findCodexSessionLogPath(threadId), filePath);
    const transcript = readThreadTranscript(threadId);
    assert.equal(transcript.transcriptAvailable, true);
    assert.equal(transcript.availability, "codex_jsonl");
    assert.equal(transcript.messages.length, 2);
    assert.equal(transcript.messages[0].role, "user");
    assert.equal(transcript.messages[1].text, "On it.");
  } finally {
    if (previousSessionsDir == null) delete process.env.CODEX_SESSIONS_DIR;
    else process.env.CODEX_SESSIONS_DIR = previousSessionsDir;
    fs.rmSync(tempDir, { recursive: true, force: true });
  }
});

test("exposes the latest turn context model and reasoning", () => {
  const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "thread-transcript-context-"));
  const sessionsDir = path.join(tempDir, "sessions", "2026", "04", "28");
  fs.mkdirSync(sessionsDir, { recursive: true });
  const threadId = "019context-thread";
  const filePath = path.join(sessionsDir, `rollout-2026-04-28T14-00-00-${threadId}.jsonl`);
  fs.writeFileSync(
    filePath,
    [
      JSON.stringify({
        timestamp: "2026-04-28T14:00:00Z",
        type: "session_meta",
        payload: { id: threadId, cwd: "/tmp/project", timestamp: "2026-04-28T14:00:00Z" },
      }),
      JSON.stringify({
        timestamp: "2026-04-28T14:00:01Z",
        type: "turn_context",
        payload: { model: "gpt-5.5", effort: "high" },
      }),
      JSON.stringify({
        timestamp: "2026-04-28T14:00:02Z",
        type: "response_item",
        payload: {
          type: "message",
          role: "assistant",
          content: [{ type: "output_text", text: "Done." }],
        },
      }),
    ].join("\n"),
  );

  const previousSessionsDir = process.env.CODEX_SESSIONS_DIR;
  process.env.CODEX_SESSIONS_DIR = path.join(tempDir, "sessions");
  try {
    const transcript = readThreadTranscript(threadId);
    assert.equal(transcript.model, "gpt-5.5");
    assert.equal(transcript.reasoning, "high");
    assert.equal(transcript.messages.length, 1);
  } finally {
    if (previousSessionsDir == null) delete process.env.CODEX_SESSIONS_DIR;
    else process.env.CODEX_SESSIONS_DIR = previousSessionsDir;
    fs.rmSync(tempDir, { recursive: true, force: true });
  }
});

test("reads legacy user_message and assistant_message rollout envelopes", () => {
  const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "thread-transcript-legacy-"));
  const sessionsDir = path.join(tempDir, "sessions", "2026", "04", "28");
  fs.mkdirSync(sessionsDir, { recursive: true });
  const threadId = "019legacy-thread";
  const filePath = path.join(sessionsDir, `rollout-2026-04-28T13-00-00-${threadId}.jsonl`);
  fs.writeFileSync(
    filePath,
    [
      JSON.stringify({
        timestamp: "2026-04-28T13:00:00Z",
        type: "session_meta",
        payload: { id: threadId, cwd: "/tmp/project", timestamp: "2026-04-28T13:00:00Z" },
      }),
      JSON.stringify({
        timestamp: "2026-04-28T13:00:01Z",
        type: "user_message",
        payload: { text: "Activate skill $init-fixer immediately." },
      }),
      JSON.stringify({
        timestamp: "2026-04-28T13:00:02Z",
        type: "assistant_message",
        payload: { text: "Fixer is reading the project docs." },
      }),
    ].join("\n"),
  );

  const previousSessionsDir = process.env.CODEX_SESSIONS_DIR;
  process.env.CODEX_SESSIONS_DIR = path.join(tempDir, "sessions");
  try {
    const transcript = readThreadTranscript(threadId);
    assert.equal(transcript.transcriptAvailable, true);
    assert.equal(transcript.messages.length, 2);
    assert.equal(transcript.messages[0].role, "user");
    assert.equal(transcript.messages[1].text, "Fixer is reading the project docs.");
  } finally {
    if (previousSessionsDir == null) delete process.env.CODEX_SESSIONS_DIR;
    else process.env.CODEX_SESSIONS_DIR = previousSessionsDir;
    fs.rmSync(tempDir, { recursive: true, force: true });
  }
});

test("finds the latest rollout by session_meta id when filenames differ after resume", () => {
  const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "thread-transcript-resume-"));
  const sessionsDir = path.join(tempDir, "sessions", "2026", "04", "29");
  fs.mkdirSync(sessionsDir, { recursive: true });
  const threadId = "019resumed-thread";
  const oldPath = path.join(sessionsDir, `rollout-2026-04-29T09-00-00-${threadId}.jsonl`);
  const newPath = path.join(sessionsDir, "rollout-2026-04-29T10-00-00-019different-turn-id.jsonl");
  fs.writeFileSync(
    oldPath,
    [
      JSON.stringify({
        timestamp: "2026-04-29T09:00:00Z",
        type: "session_meta",
        payload: { id: threadId, cwd: "/tmp/project", timestamp: "2026-04-29T09:00:00Z" },
      }),
      JSON.stringify({ timestamp: "2026-04-29T09:00:01Z", type: "user_message", payload: { text: "old" } }),
    ].join("\n"),
  );
  fs.writeFileSync(
    newPath,
    [
      JSON.stringify({
        timestamp: "2026-04-29T10:00:00Z",
        type: "session_meta",
        payload: { id: threadId, cwd: "/tmp/project", timestamp: "2026-04-29T10:00:00Z" },
      }),
      JSON.stringify({ timestamp: "2026-04-29T10:00:01Z", type: "user_message", payload: { text: "new" } }),
    ].join("\n"),
  );

  const previousSessionsDir = process.env.CODEX_SESSIONS_DIR;
  process.env.CODEX_SESSIONS_DIR = path.join(tempDir, "sessions");
  try {
    assert.equal(findCodexSessionLogPath(threadId), newPath);
    const transcript = readThreadTranscript(threadId);
    assert.equal(transcript.messages[0].text, "new");
  } finally {
    if (previousSessionsDir == null) delete process.env.CODEX_SESSIONS_DIR;
    else process.env.CODEX_SESSIONS_DIR = previousSessionsDir;
    fs.rmSync(tempDir, { recursive: true, force: true });
  }
});

test("collapses injected context and includes tool calls in transcripts", () => {
  const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "thread-transcript-tools-"));
  const sessionsDir = path.join(tempDir, "sessions", "2026", "04", "29");
  fs.mkdirSync(sessionsDir, { recursive: true });
  const threadId = "019tool-thread";
  const filePath = path.join(sessionsDir, `rollout-2026-04-29T12-00-00-${threadId}.jsonl`);
  fs.writeFileSync(
    filePath,
    [
      JSON.stringify({
        timestamp: "2026-04-29T12:00:00Z",
        type: "session_meta",
        payload: { id: threadId, cwd: "/tmp/project", timestamp: "2026-04-29T12:00:00Z" },
      }),
      JSON.stringify({
        timestamp: "2026-04-29T12:00:01Z",
        type: "response_item",
        payload: {
          type: "message",
          role: "user",
          content: [{ type: "input_text", text: "# AGENTS.md instructions for /tmp/project\n\n<INSTRUCTIONS />" }],
        },
      }),
      JSON.stringify({
        timestamp: "2026-04-29T12:00:02Z",
        type: "response_item",
        payload: {
          type: "message",
          role: "user",
          content: [{ type: "input_text", text: "<skill>\n<name>init-fixer</name>\n</skill>" }],
        },
      }),
      JSON.stringify({
        timestamp: "2026-04-29T12:00:03Z",
        type: "response_item",
        payload: {
          type: "function_call",
          name: "get_project_handoff",
          namespace: "mcp__fixer_mcp__",
          arguments: "{}",
          call_id: "call-1",
        },
      }),
      JSON.stringify({
        timestamp: "2026-04-29T12:00:04Z",
        type: "response_item",
        payload: {
          type: "function_call_output",
          call_id: "call-1",
          output: "{\"handoff\":null}",
        },
      }),
      JSON.stringify({
        timestamp: "2026-04-29T12:00:05Z",
        type: "response_item",
        payload: {
          type: "message",
          role: "assistant",
          content: [{ type: "output_text", text: "Fixer initialized.\n\nStanding by." }],
        },
      }),
    ].join("\n"),
  );

  const previousSessionsDir = process.env.CODEX_SESSIONS_DIR;
  process.env.CODEX_SESSIONS_DIR = path.join(tempDir, "sessions");
  try {
    const transcript = readThreadTranscript(threadId);
    assert.equal(transcript.messages.length, 4);
    assert.equal(transcript.messages[0].kind, "internal_context");
    assert.equal(transcript.messages[0].collapsed, true);
    assert.equal(transcript.messages[1].summary, "Internal skill context: init-fixer");
    assert.equal(transcript.messages[2].role, "tool");
    assert.equal(transcript.messages[2].summary, "Called fixer_mcp.get_project_handoff({})");
    assert.match(transcript.messages[2].text, /Output:\n\{"handoff":null\}/);
    assert.equal(transcript.messages[3].collapsed, false);
  } finally {
    if (previousSessionsDir == null) delete process.env.CODEX_SESSIONS_DIR;
    else process.env.CODEX_SESSIONS_DIR = previousSessionsDir;
    fs.rmSync(tempDir, { recursive: true, force: true });
  }
});

test("reads user and assistant messages from Antigravity transcript.jsonl", () => {
  const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "thread-transcript-agy-"));
  const threadId = "d56fbe38-42ad-4a7f-8c65-8b6523e2515f";
  const transcriptDir = path.join(tempDir, "brain", threadId, ".system_generated", "logs");
  fs.mkdirSync(transcriptDir, { recursive: true });
  fs.writeFileSync(
    path.join(tempDir, "history.jsonl"),
    JSON.stringify({
      conversationId: threadId,
      workspace: "/home/operator/Desktop/projects/self_orchestration",
      display: "Руки Antigravity",
      model: "Gemini 3.7 Flash",
      timestamp: 1786671108888,
    }) + "\n",
  );
  fs.writeFileSync(
    path.join(transcriptDir, "transcript.jsonl"),
    [
      JSON.stringify({
        step_index: 0,
        source: "USER_EXPLICIT",
        type: "USER_INPUT",
        created_at: "2026-08-14T01:25:41Z",
        content: "<USER_REQUEST>\n/hands-netrunner\nUse its Project Hands Channel Mode for the current project.\n</USER_REQUEST>",
      }),
      JSON.stringify({
        step_index: 1,
        source: "MODEL",
        type: "PLANNER_RESPONSE",
        created_at: "2026-08-14T01:25:42Z",
        tool_calls: [{ name: "get_hands_state", args: {} }],
        content: "Channel initialized.",
      }),
    ].join("\n"),
  );

  const previousAgyRoot = process.env.ANTIGRAVITY_ROOT;
  process.env.ANTIGRAVITY_ROOT = tempDir;
  try {
    const transcript = readThreadTranscript(threadId);
    assert.equal(transcript.transcriptAvailable, true);
    assert.equal(transcript.availability, "antigravity_jsonl");
    assert.equal(transcript.model, "Gemini 3.7 Flash");
    assert.equal(transcript.messages.length, 3); // 1 user, 1 tool_call, 1 assistant
    assert.equal(transcript.messages[0].role, "user");
    assert.match(transcript.messages[0].text, /\/hands-netrunner/);
    assert.equal(transcript.messages[1].role, "tool");
    assert.equal(transcript.messages[2].role, "assistant");
    assert.equal(transcript.messages[2].text, "Channel initialized.");
  } finally {
    if (previousAgyRoot == null) delete process.env.ANTIGRAVITY_ROOT;
    else process.env.ANTIGRAVITY_ROOT = previousAgyRoot;
    fs.rmSync(tempDir, { recursive: true, force: true });
  }
});

