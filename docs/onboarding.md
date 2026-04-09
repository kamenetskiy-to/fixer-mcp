# Onboarding

## What This Repo Contains

`github_repo/` is the GitHub-ready Fixer MCP track. It stages the server, launcher, and compatibility bridge in one repo so a new operator can start here instead of reverse-engineering the older private workspace.

Role model:

- Fixer: orchestrates tasks, reviews worker output, and updates canon
- Netrunner: implements code, tests, and task reports
- Overseer: inspects a workspace and decides what worker or orchestrator to launch

## Quick Start

### Server-only path

Use this when you want the control plane running first and do not need the packaged launcher yet.

```bash
cd packages/fixer-mcp-server
make test
make build
make run
```

The server keeps its local runtime state inside the package working directory. That avoids leaking `fixer.db` or logs into the rest of the repo.

### Full operator path

Use this when you want the staged launcher contract from the public repo itself.

```bash
python3 -m pip install -e ./packages/client-wires
cd packages/fixer-mcp-server && make build
cd ../..
fixer --wire-info
fixer
```

This onboarding path is repo-native:

- `fixer` now begins with a first-step role selector for `fixer`, `netrunner`, or `overseer`
- no sibling `../mcp_servers` checkout is required for first boot
- package-local config in `packages/client-wires/config/mcp-config.json` is the default starting point
- `fixer` now comes from the repo-owned `packages/client-wires` install instead of the compatibility bridge
- packaged launches keep SQLite state in `~/.local/state/fixer-client-wires/` unless overridden
- legacy env vars and repo-root config files remain compatibility-only fallbacks
- install `packages/compat-bridge` only if you still need the older `fixer_compat_bridge` flag vocabulary

## Config And Runtime Overrides

Supported advanced overrides:

- `FIXER_CLIENT_WIRES_RUNTIME_ROOT`
- `FIXER_CLIENT_WIRES_CONFIG_PATH`
- `FIXER_CLIENT_WIRES_CONFIG_ROOT`

Compatibility-only fallbacks:

- `MCP_SERVERS_ROOT`
- repo-root `mcp_config.json`
- sibling `../mcp_servers`

Use the compatibility fallbacks only when migrating an older operator workflow.

## Recommended Validation

Before pushing changes or cutting a release from this repo:

```bash
python3 -m unittest discover -s tests
cd packages/fixer-mcp-server && go test ./...
```

For the repo-native publication flow, see [release.md](release.md).
