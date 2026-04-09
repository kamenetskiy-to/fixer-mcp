# Agent Onboarding

This repo ships a repo-native `fixer` launcher for the GitHub-ready track inside `github_repo/`.

## Quick Setup

From the `github_repo/` directory:

```bash
python3 -m pip install -e ./packages/client-wires
python3 -m pip install -e ./packages/compat-bridge
cd packages/fixer-mcp-server && make build
cd ../..
fixer --wire-info
fixer
```

What that does:

- installs the packaged launcher surfaces
- exposes the `fixer` wrapper command
- builds `packages/fixer-mcp-server/fixer_mcp` once up front
- launches Fixer with the packaged MCP wiring instead of only printing a launch plan

## Runtime State

The public wrapper keeps durable state under `~/.local/state/fixer-client-wires/` by default.

Important files:

- SQLite DB: `~/.local/state/fixer-client-wires/fixer.db`
- server log: `~/.local/state/fixer-client-wires/fixer_mcp.log`

Override the state location if needed:

```bash
export FIXER_CLIENT_WIRES_STATE_ROOT="$HOME/.fixer-state"
```

The wrapper also passes `FIXER_DB_PATH` into the packaged `fixer_mcp` server so the SQLite path stays stable even when you launch `fixer` from another directory.

## Day-To-Day Use

- `fixer`: launch Fixer with the default packaged backend (`codex`)
- `fixer --dry-run`: print the resolved launch plan instead of executing it
- `fixer --backend droid`: launch through the Droid backend
- `fixer-compat-bridge --role netrunner --backend codex --resume-session-id <id>`: resume a stored worker session through the packaged wrapper

## Repo-Side Verification

Run these before publishing or re-testing Ubuntu onboarding:

```bash
python3 -m unittest discover -s tests
cd packages/fixer-mcp-server && go test ./...
```

## Notes

- Work only inside `github_repo/` for the new track.
- The packaged launcher auto-builds `packages/fixer-mcp-server/fixer_mcp` if the binary is missing or stale unless `FIXER_CLIENT_WIRES_SKIP_FIXER_MCP_AUTOBUILD=1`.
- If Ubuntu is missing `go`, install Go first or build `fixer_mcp` manually before running `fixer`.
