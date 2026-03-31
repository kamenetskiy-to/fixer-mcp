# Fixer MCP

Fixer MCP is a local orchestration control plane for the Fixer / Netrunner / Overseer workflow.

This public export contains:
- `fixer_mcp/`: the Go MCP server and migration helpers
- `client_wires/`: the Python launch wires used to start and resume Fixer-managed sessions
- `project_book/clean_docs/`: the small canon subset referenced by the wires
- `fixer_mcp/dashboard_app/`: an optional Flutter desktop dashboard for `fixer.db`

## What This Repo Depends On

The wire layer delegates to `codex_pro_app`, which is not vendored here. By default the wires look for a sibling checkout:

```text
../mcp_servers/codex_pro_app
```

If your layout is different, set `MCP_SERVERS_ROOT` to the directory that contains `codex_pro_app`.

## Prerequisites

- Go `1.25.4+`
- Python `3.11+`
- Optional: Flutter for `fixer_mcp/dashboard_app`

## Quick Start

1. Build the MCP server:

```bash
cd fixer_mcp
go build -o fixer_mcp .
```

2. Optional Telegram setup:

```bash
cp .env.example .env
```

Fill in:
- `FIXER_MCP_TELEGRAM_BOT_TOKEN`
- `FIXER_MCP_TELEGRAM_CHAT_ID`

3. Point the wire layer at your `mcp_servers` checkout if needed:

```bash
export MCP_SERVERS_ROOT=/absolute/path/to/mcp_servers
```

4. Inspect the wire bootstrap:

```bash
python3 client_wires/fixer_wire.py --wire-info
```

## Validation

Core checks used for this export:

```bash
cd fixer_mcp && go test ./...
python3 -m pytest tests/test_prepare_public_repo.py client_wires/tests/test_fixer_wire.py client_wires/tests/test_fixer_autonomous.py client_wires/tests/test_fixer_autopilot.py
python3 scripts/prepare_public_repo.py --output-dir dist/fixer-mcp-public
```

## Publication Shape

This repo is a focused export from a larger private workspace. The export is built deterministically by `scripts/prepare_public_repo.py`, which copies only the directories needed to run Fixer MCP and strips local databases, logs, binaries, build products, and machine-specific configuration.
