# Fixer MCP

Fixer MCP is a durable orchestration control plane for multi-agent coding work.

It is built for teams that need more than prompt files, ad-hoc agent spawning, or a thin wrapper around a CLI. The system keeps project-scoped state for sessions, handoffs, canon docs, MCP assignment, launch metadata, and review outcomes so work can be delegated, resumed, audited, and governed instead of disappearing into chat history.

## Why This Exists

Fixer MCP solves the failure mode where multi-agent work feels fast for 20 minutes and chaotic for the next 2 days.

Without a control plane, teams usually lose one or more of these:
- durable task state across restarts and handoffs
- a canonical project memory that workers can attach directly
- explicit review authority before work is accepted
- governed delegation with scoped tool access
- a reliable way to resume the right worker with the right context

Fixer MCP treats those as first-class runtime objects instead of social conventions.

## Who It Is For

- engineers running serious local or MCP-aware agent workflows
- teams that want role separation between planning, execution, and review
- operators who need resumability, task history, and structured handoffs
- projects where tool access should be assigned deliberately per task

## Why It Is More Than A Prompt Pack

Fixer MCP is not just a collection of prompts or a launcher script. The Go MCP server owns durable orchestration state, while the Python wires turn that state into actual Fixer, Netrunner, and Overseer runs.

That split matters because it gives you:
- durable SQLite-backed sessions, docs, proposals, and handoffs
- explicit launch and wait primitives for long-running workers
- role-aware workflows with review authority
- project-scoped MCP assignment instead of global tool chaos
- backend-aware session metadata for resume and audit flows

## Why The MCP Server Form Factor Matters

Packaging the control plane as an MCP server makes it strategically stronger than a repo that only ships prompts or shell wrappers:
- any MCP-capable client can talk to the orchestration layer
- tool access, launch flows, and project canon live behind one durable interface
- the system can coordinate external workers without collapsing into one terminal history
- the same orchestration layer can be reused across interactive, autonomous, and review-driven runs

## Quick Start

If your `mcp_servers` checkout lives next to this repo, the common path is two commands:

```bash
cd fixer_mcp && go build -o fixer_mcp .
python3 client_wires/fixer_wire.py --role fixer
```

If your layout is different, point the wire layer at the directory that contains `codex_pro_app`:

```bash
export MCP_SERVERS_ROOT=/absolute/path/to/mcp_servers
```

Then verify bootstrap resolution:

```bash
python3 client_wires/fixer_wire.py --wire-info
```

For the fuller setup path, see [GETTING_STARTED.md](GETTING_STARTED.md).

## Why Teams Pick This Over Lighter Orchestration

Projects like `oh-my-claudecode` are useful when you mainly want lighter-weight multi-agent coordination. Fixer MCP is aimed at a different problem boundary: durable, reviewable, resumable delivery with explicit project canon and governed tool access.

Choose Fixer MCP when you care about:
- durable orchestration state instead of chat-only coordination
- formal role separation between Fixer, Netrunner, and Overseer
- project docs, handoffs, and proposals attached to the work itself
- per-session MCP assignment and launch metadata
- review authority before work is considered done

## Repo Shape

- `fixer_mcp/`: Go MCP server, migrations, and optional desktop dashboard
- `client_wires/`: canonical launch wires for Fixer-managed execution
- `project_book/clean_docs/`: public canon subset the wires and workflows reference
- `fixer_mcp/dashboard_app/`: optional Flutter desktop dashboard for `fixer.db`

## Validation

Core checks used for this export:

```bash
cd fixer_mcp && go test ./...
python3 -m pytest tests/test_prepare_public_repo.py client_wires/tests/test_fixer_wire.py client_wires/tests/test_fixer_autonomous.py client_wires/tests/test_fixer_autopilot.py
python3 scripts/prepare_public_repo.py --output-dir dist/fixer-mcp-public
```

## Export Note

This repository is published from a larger private workspace, but that is implementation detail, not the product story. The export is built deterministically by `scripts/prepare_public_repo.py`, which copies the runtime-relevant directories and strips local databases, logs, binaries, build products, and machine-specific configuration.
