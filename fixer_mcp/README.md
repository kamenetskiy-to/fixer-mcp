# Fixer MCP

`fixer_mcp` is the Go control-plane server behind the Fixer / Netrunner / Overseer workflow.

This is the durable part of the system. It stores the orchestration state that lighter agent wrappers usually keep only in terminal history: sessions, project canon, doc proposals, handoffs, MCP assignment, launch metadata, and review lifecycle state.

## What The Server Owns

- project and session orchestration backed by SQLite
- explicit Netrunner launch and wait primitives
- project-scoped docs, handoff, and MCP assignment storage
- native operator notification hooks, including Telegram
- optional desktop inspection UI in [`dashboard_app`](dashboard_app)

## Why This Layer Exists

Fixer MCP is intentionally not just a launcher. The MCP server form factor gives the workflow one durable control plane that multiple clients and launch surfaces can talk to.

That matters when you need:
- resumable work after a terminal or model session disappears
- review authority before a worker result is accepted
- deliberate tool assignment per project or task
- a canonical project memory attached to the work itself
- explicit launch and wait behavior for long-running workers

## Layout

- `main.go`: MCP server entrypoint and tool surface
- `cmd/project_doc_hard_replace`: one-shot project-doc replacement/import helper
- `dashboard_app`: local desktop dashboard for inspecting `fixer.db`
- `mcp_config.json`: local MCP registration for the server binary

## Prerequisites

- Go `1.25.4` or newer
- SQLite available through the bundled Go driver
- Python `3.11+` if you want to use the sibling `client_wires` launch tooling
- Optional: Flutter if you want to run the dashboard

## Build

From this directory:

```bash
go build -o fixer_mcp .
```

## Quick Start

Run the server from this directory:

```bash
./fixer_mcp
```

It uses `fixer.db` in the current working directory. In the common Fixer MCP workflow, this binary is paired with the sibling Python launch wires in [`../client_wires`](../client_wires), which start and resume Fixer-managed sessions against this server.

## Run

```bash
./fixer_mcp
```

Optional Telegram variables:

```bash
cp .env.example .env
```

Then set:
- `FIXER_MCP_TELEGRAM_BOT_TOKEN`
- `FIXER_MCP_TELEGRAM_CHAT_ID`
- `FIXER_MCP_TELEGRAM_API_BASE_URL` only if you need a non-default API base

## Tests

```bash
go test ./...
```

## Why Use This Instead Of A Thin Wrapper

If all you need is a few prompts and a way to spawn sub-agents, this server is probably more structure than you want.

If you need durable orchestration state, governed delegation, project canon, resumable workers, and a reviewable lifecycle, this is the layer that makes those workflows reliable rather than aspirational.

## Dashboard

From [`dashboard_app`](dashboard_app):

```bash
flutter run -d macos
```

If the DB is not at `../fixer.db`, set `FIXER_MCP_DB_PATH` or `FIXER_DB_PATH`.
