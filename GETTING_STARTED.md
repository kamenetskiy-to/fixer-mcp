# Getting Started With Fixer MCP

This guide is for the common case: you want to run the published Fixer MCP repo locally, start the control plane, and launch Fixer-managed work without reverse-engineering the private workspace it came from.

## What You Need

- Go `1.25.4+`
- Python `3.11+`
- a checkout of `mcp_servers` that contains `codex_pro_app`
- optional: Flutter if you want the desktop dashboard in `fixer_mcp/dashboard_app`

By default the wire layer looks for:

```text
../mcp_servers/codex_pro_app
```

If your layout is different, set:

```bash
export MCP_SERVERS_ROOT=/absolute/path/to/mcp_servers
```

## Fastest Startup Path

From the repo root:

```bash
cd fixer_mcp && go build -o fixer_mcp .
python3 client_wires/fixer_wire.py --role fixer
```

If you want to confirm dependency resolution first:

```bash
python3 client_wires/fixer_wire.py --wire-info
```

## What Those Commands Do

1. `go build -o fixer_mcp .`
Builds the local MCP server binary used by the orchestration layer.

2. `python3 client_wires/fixer_wire.py --role fixer`
Starts the canonical launcher for the Fixer role. From there you can create or resume orchestration work using the repo's normal role-aware flow.

## Optional Telegram Notifications

Inside [`fixer_mcp`](fixer_mcp):

```bash
cp .env.example .env
```

Then set:
- `FIXER_MCP_TELEGRAM_BOT_TOKEN`
- `FIXER_MCP_TELEGRAM_CHAT_ID`
- `FIXER_MCP_TELEGRAM_API_BASE_URL` only if you need a non-default API base

## Common Commands

- Inspect wire bootstrap:

```bash
python3 client_wires/fixer_wire.py --wire-info
```

- Show Fixer launcher help:

```bash
python3 client_wires/fixer_wire.py --role fixer --help
```

- Register the active Fixer thread for autonomous wake-ups:

```bash
python3 client_wires/fixer_autonomous.py register-fixer --cwd /path/to/project --fixer-session-id <codex_session_id>
```

- Launch one durable Netrunner from the autonomous wire:

```bash
python3 client_wires/fixer_autonomous.py launch-netrunner --cwd /path/to/project --session-id 3
```

## Validation

```bash
cd fixer_mcp && go test ./...
python3 -m pytest tests/test_prepare_public_repo.py client_wires/tests/test_fixer_wire.py client_wires/tests/test_fixer_autonomous.py client_wires/tests/test_fixer_autopilot.py
```

## Where To Read Next

- Root overview: [README.md](README.md)
- Go control plane details: [fixer_mcp/README.md](fixer_mcp/README.md)
- Launcher and workflow details: [client_wires/README.md](client_wires/README.md)
