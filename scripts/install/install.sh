#!/usr/bin/env bash
set -euo pipefail

# Bootstrap shell script for Fixer MCP managed installation
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PYTHON="${PYTHON:-python3}"

exec "$PYTHON" "$SCRIPT_DIR/install.py" "$@"
