#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="${ROOT_DIR:-/workspace/self_orchestration}"
OUT_DIR="${OUT_DIR:-/bootstrap-out}"
DB_PATH="${OUT_DIR}/fixer-bootstrap-e2e.db"
BUILD_DIR="${BUILD_DIR:-/tmp/fixer_mcp_bootstrap_e2e}"
FIXER_BINARY="${ROOT_DIR}/fixer_mcp/fixer_mcp"

mkdir -p "${OUT_DIR}" "${BUILD_DIR}"
cd "${ROOT_DIR}"

echo "[bootstrap-e2e] toolchain"
go version
python3 --version
node --version
npm --version
codex --version

test -r /root/.codex/auth.json
echo "[bootstrap-e2e] Codex auth mount detected at /root/.codex/auth.json"
python3 -c "import client_wires.codex_compat"
echo "[bootstrap-e2e] vendored codex_compat detected in client_wires"

echo "[bootstrap-e2e] Go tests and binary build"
(
  cd fixer_mcp
  go test ./...
  go build -o "${FIXER_BINARY}" .
)

echo "[bootstrap-e2e] non-LLM stdio preflight"
FIXER_SMOKE_BINARY="${FIXER_BINARY}" python3 tests/fixer_mcp_stdio_smoke.py

echo "[bootstrap-e2e] real Codex-backed bootstrap scenario"
FIXER_BOOTSTRAP_E2E_DB_PATH="${DB_PATH}" \
FIXER_BOOTSTRAP_E2E_BINARY="${FIXER_BINARY}" \
python3 docker/fixer_bootstrap_e2e.py
