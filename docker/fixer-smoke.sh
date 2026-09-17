#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="${ROOT_DIR:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
BUILD_DIR="${BUILD_DIR:-/tmp/fixer_mcp_smoke}"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
SMOKE_BINARY="${INSTALL_DIR}/fixer_mcp"

# The deterministic smoke owns its temporary role/database state. Do not let a
# caller's locked worker environment change Go tests or the stdio scenario.
unset FIXER_MCP_LOCKED_ROLE FIXER_DB_PATH FIXER_MCP_DEFAULT_ROLE
unset FIXER_MCP_DEFAULT_CWD FIXER_MCP_AUTO_AUTH FIXER_MCP_TOOL_PROFILE

cd "${ROOT_DIR}"

echo "[smoke] toolchain"
go version
python3 --version
node --version
npm --version

if [[ "${FIXER_SMOKE_REQUIRE_AUTH:-0}" == "1" ]]; then
  test -r /root/.codex/auth.json
  echo "[smoke] auth mount detected at /root/.codex/auth.json"
fi

mkdir -p "${BUILD_DIR}" "${INSTALL_DIR}"

echo "[smoke] go tests"
(
  cd fixer_mcp
  go test ./...
  go build -o "${BUILD_DIR}/fixer_mcp" .
)

echo "[smoke] install fixer_mcp binary"
install -m 0755 "${BUILD_DIR}/fixer_mcp" "${SMOKE_BINARY}"
"${SMOKE_BINARY}" --help >/dev/null

echo "[smoke] focused client_wires non-interactive tests"
python3 -m unittest \
  client_wires.tests.test_fixer_wire_db.ResolveFixerDbPathTests \
  client_wires.tests.test_fixer_wire_db.ResolveProjectIdTests \
  client_wires.tests.test_fixer_wire_mcp.ForcedFixerSpecTests

echo "[smoke] node bridge tests"
(
  cd fixer_mcp/node_bridge
  npm test
)

echo "[smoke] clean MCP stdio flow"
FIXER_SMOKE_BINARY="${SMOKE_BINARY}" python3 tests/fixer_mcp_stdio_smoke.py

echo "[smoke] passed"
