#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
IMAGE_NAME="${DOCKER_BOOTSTRAP_E2E_IMAGE:-fixer-mcp-bootstrap-e2e:local}"
CONTAINER_PREFIX="${DOCKER_BOOTSTRAP_E2E_CONTAINER_PREFIX:-fixer-mcp-bootstrap-e2e}"
AUTH_PATH="${DOCKER_BOOTSTRAP_E2E_AUTH_PATH:-${HOME}/.codex/auth.json}"
LOG_ROOT="${DOCKER_BOOTSTRAP_E2E_LOG_ROOT:-${ROOT_DIR}/.run/docker-bootstrap-e2e}"
TIMESTAMP="$(date -u +%Y%m%dT%H%M%SZ)"
RUN_DIR="${LOG_ROOT}/${TIMESTAMP}"

if [[ ! -r "${AUTH_PATH}" ]]; then
  echo "Missing readable Codex auth file: ${AUTH_PATH}" >&2
  echo "Expected a read-only bind mount source for /root/.codex/auth.json." >&2
  exit 1
fi

mkdir -p "${RUN_DIR}"

echo "[bootstrap-e2e] logs: ${RUN_DIR}"
echo "[bootstrap-e2e] building ${IMAGE_NAME}"
docker build \
  -f "${ROOT_DIR}/docker/fixer-bootstrap-e2e.Dockerfile" \
  -t "${IMAGE_NAME}" \
  "${ROOT_DIR}" \
  2>&1 | tee "${RUN_DIR}/docker-build.log"

echo "[bootstrap-e2e] running container with read-only Codex auth mount"
docker run --rm \
  --name "${CONTAINER_PREFIX}-${TIMESTAMP}" \
  --mount type=bind,source="${AUTH_PATH}",target=/root/.codex/auth.json,readonly \
  --mount type=bind,source="${RUN_DIR}",target=/bootstrap-out \
  -e BOOTSTRAP_E2E_MODEL="${BOOTSTRAP_E2E_MODEL:-gpt-5.5}" \
  -e BOOTSTRAP_E2E_REASONING="${BOOTSTRAP_E2E_REASONING:-high}" \
  -e BOOTSTRAP_E2E_TIMEOUT_SECONDS="${BOOTSTRAP_E2E_TIMEOUT_SECONDS:-3600}" \
  -e FIXER_EXTERNAL_SESSION_ID_DETECTOR_TIMEOUT_SEC="${FIXER_EXTERNAL_SESSION_ID_DETECTOR_TIMEOUT_SEC:-180}" \
  "${IMAGE_NAME}" \
  2>&1 | tee "${RUN_DIR}/container.log"

echo "[bootstrap-e2e] complete: ${RUN_DIR}"
