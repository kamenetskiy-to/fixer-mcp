import assert from "node:assert/strict";
import test from "node:test";

import {
  ALWAYS_ON_MCP_SERVER,
  allowlistKey,
  ensureAlwaysOnAllowlist,
  mcpServerConfigsFromBody,
  normalizeAllowlist,
} from "../mcp_config.mjs";

test("normalizes allowlists and preserves the always-on Fixer authority server", () => {
  assert.deepEqual(normalizeAllowlist([" sqlite ", "dart_flutter", "", "sqlite"]), [
    "dart_flutter",
    "sqlite",
  ]);

  assert.deepEqual(ensureAlwaysOnAllowlist(["sqlite"]), [
    ALWAYS_ON_MCP_SERVER,
    "sqlite",
  ]);
});

test("normalizes bridge MCP server config bodies into stable keys", () => {
  const body = {
    mcpServerConfigs: {
      sqlite: {
        enabledTools: ["query", " ", 42],
        env: { SQLITE_DB_PATH: "/tmp/fixer.db", EMPTY: "" },
      },
      ignored: null,
    },
  };

  assert.deepEqual(mcpServerConfigsFromBody(body), {
    sqlite: {
      enabled_tools: ["query"],
      env: { SQLITE_DB_PATH: "/tmp/fixer.db" },
    },
  });

  assert.equal(
    allowlistKey(["sqlite"], body.mcpServerConfigs),
    '{"allowlist":["sqlite"],"mcpServerConfigs":{"sqlite":{"enabled_tools":["query"],"env":{"SQLITE_DB_PATH":"/tmp/fixer.db"}}}}',
  );
});
