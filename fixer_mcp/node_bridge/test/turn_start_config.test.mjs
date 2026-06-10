import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));

test("turn/start only applies MCP allowlist when explicitly requested", () => {
  const source = fs.readFileSync(
    path.resolve(__dirname, "../index.mjs"),
    "utf8",
  );

  const turnStartIndex = source.indexOf('if (req.method === "POST" && url.pathname === "/turn/start") {');
  assert.notEqual(turnStartIndex, -1);

  const snippet = source.slice(turnStartIndex, turnStartIndex + 1600);
  assert.match(snippet, /const allowlist = allowlistFromBodyOrEnv\(body\);/);
  assert.doesNotMatch(snippet, /const allowlist = ensureAlwaysOnAllowlist\(allowlistFromBodyOrEnv\(body\)\);/);
});

test("turn/start preserves historical model and effort unless explicitly requested", () => {
  const source = fs.readFileSync(
    path.resolve(__dirname, "../index.mjs"),
    "utf8",
  );

  const turnStartIndex = source.indexOf('if (req.method === "POST" && url.pathname === "/turn/start") {');
  assert.notEqual(turnStartIndex, -1);

  const snippet = source.slice(turnStartIndex, turnStartIndex + 4600);
  assert.match(snippet, /const model = normalizeModel\(body\.model\);/);
  assert.doesNotMatch(snippet, /normalizeModel\(body\.model\) \?\? DEFAULT_CODEX_MODEL/);
  assert.doesNotMatch(snippet, /NORMALIZED_DEFAULT_REASONING_EFFORT/);
  assert.match(snippet, /\.\.\.\(model \? \{ model \} : \{\}\),/);
});
