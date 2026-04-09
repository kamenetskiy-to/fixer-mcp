# Fixer Desktop Bridge

Thin local HTTP bridge for the first `fixer-desktop` app slice.

This package projects the existing Fixer MCP SQLite state into app-friendly,
read-oriented JSON payloads. It does not create a second orchestration store.

## What it exposes

- `GET /health`
- `GET /api/projects`
- `GET /api/projects/<project-id>/dashboard`
- `GET /api/sessions/<session-id>`

## Local run

From the repo root:

```bash
python3 -m pip install -e ./github_repo/packages/desktop-bridge
fixer-desktop-bridge serve --db-path ./fixer_mcp/fixer.db --host 127.0.0.1 --port 8765
```

If `--db-path` is omitted, the bridge tries to discover `fixer_mcp/fixer.db`
or `fixer.db` by walking up from the current working directory.
