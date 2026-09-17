#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

# Run assembler using system python3 or specified python
PYTHON_BIN="${PYTHON:-python3}"

exec "${PYTHON_BIN}" "${SCRIPT_DIR}/assemble_release.py" --repo-root "${REPO_ROOT}" "$@"
