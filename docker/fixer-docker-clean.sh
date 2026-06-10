#!/usr/bin/env bash
set -euo pipefail

IMAGE_NAME="${DOCKER_IMAGE:-fixer-mcp-smoke:local}"
CONTAINER_PREFIX="${DOCKER_CONTAINER_PREFIX:-fixer-mcp-smoke}"
VOLUME_PREFIX="${DOCKER_VOLUME_PREFIX:-fixer-mcp-smoke}"

containers="$(docker ps -aq --filter "name=${CONTAINER_PREFIX}" 2>/dev/null || true)"
if [[ -n "${containers}" ]]; then
  docker rm -f ${containers}
fi

volumes="$(docker volume ls -q --filter "name=${VOLUME_PREFIX}" 2>/dev/null || true)"
if [[ -n "${volumes}" ]]; then
  docker volume rm -f ${volumes}
fi

images="$(docker image ls -q --filter "label=fixer-mcp.smoke=true" 2>/dev/null || true)"
if [[ -n "${images}" ]]; then
  docker image rm -f ${images}
fi

docker image rm -f "${IMAGE_NAME}" >/dev/null 2>&1 || true
rm -rf .run/docker-smoke
