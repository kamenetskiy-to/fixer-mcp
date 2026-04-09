# client-wires

This package now carries the staged public launcher contract for the GitHub-ready Fixer MCP track.

It is still additive and compatibility-aware, but it now exposes a real package boundary:

- repo-local bootstrap and config resolution
- backend descriptors for `codex`, `droid`, and `claude`
- role-aware launch-plan building for `fixer`, `netrunner`, and `overseer`
- package entrypoints, including the repo-native `fixer` console script, that can both preview and execute launcher behavior without importing the legacy `client_wires` tree

## Runtime contract

Resolution order:

1. `FIXER_CLIENT_WIRES_RUNTIME_ROOT`
2. staged package-local runtime in `packages/client-wires/runtime/`
3. compatibility override `MCP_SERVERS_ROOT`
4. compatibility fallback sibling checkout `../mcp_servers`

That order makes the packaged repo-local runtime the default story while preserving older operator overrides during migration.

If an installed wheel does not have repo-side `runtime/` files next to it, the package now materializes its bundled staged runtime into `FIXER_CLIENT_WIRES_CACHE_ROOT` or `~/.cache/fixer-client-wires/`.

Background Codex launches are expected to keep a project-local proxy snapshot in `.codex/runtime_proxy_env.json` so later daemon/GUI-triggered launches can reuse the last known `ALL_PROXY` / `HTTP_PROXY` / `HTTPS_PROXY` / `NO_PROXY` values instead of depending on shell inheritance.

## Config contract

Resolution order:

1. `FIXER_CLIENT_WIRES_CONFIG_PATH`
2. `FIXER_CLIENT_WIRES_CONFIG_ROOT/mcp-config.json`
3. package-local `packages/client-wires/config/mcp-config.json`
4. package-local `packages/client-wires/examples/mcp-config.example.json`
5. repo-level `examples/mcp-config.example.json`
6. compatibility-only repo-root `mcp_config.json`

That order replaces implicit root-file discovery with a deterministic public contract while keeping the legacy root filename available only as a migration fallback.

If neither repo-local config nor compatibility fallbacks exist, the package now falls back to its bundled staged config/example assets instead of silently resolving to nothing.

## Skills contract

The package now also carries a small staged skill bundle for the GitHub-ready track, including:

- `autonomous-resolution-one-task`
- `start-fixer`
- `start-overseer`
- `start-netrunner`
- `manual-resolution`
- `session-completion`

Only two execution flows are currently supported in the GitHub-ready track:

- `manual-resolution`
- `autonomous-resolution-one-task`

Parallel Netrunner orchestration and autonomous fixer resume chains are intentionally disabled in this staged package surface.

`wire-info` and launch-plan rendering report the resolved staged skills root so non-Codex backends can be debugged against a concrete packaged path instead of an empty skill surface.

## Quick start

```bash
python3 -m pip install -e ./packages/client-wires
fixer --wire-info
fixer
python3 -m fixer_client_wires wire-info
fixer-client-wires list-backends
fixer-client-wires plan-launch --role fixer --backend codex --mcp-server fixer_mcp
fixer-client-wires launch --role fixer --backend codex --mcp-server fixer_mcp
fixer-client-wires resume --role netrunner --backend codex --session-id ext-session-123 --mcp-server fixer_mcp
```

Commands:

- `fixer`: repo-native direct entrypoint that starts with an interactive role selector for `fixer`, `netrunner`, or `overseer`
- `python3 -m fixer_client_wires wire-info`: print resolved runtime and config details
- `fixer-client-wires list-roles`: show the packaged launch roles
- `fixer-client-wires list-backends`: show the staged backend adapter surface
- `fixer-client-wires plan-launch ...`: preview the staged launch command and backend notes using only package-local code
- `fixer-client-wires launch ...`: execute a fresh launch and materialize the required backend-specific MCP wiring
- `fixer-client-wires plan-resume ...`: preview a headless resume command that keeps backend/model metadata sticky to the stored session
- `fixer-client-wires resume ...`: execute the staged resume command with the same sticky session metadata contract

Pass `--role` to the direct `fixer` entrypoint when you need a non-interactive bypass for scripting or focused dry runs.

The packaged execution path now also handles the missing repo-native bootstrap work:

- auto-builds `packages/fixer-mcp-server/fixer_mcp` when the configured `fixer_mcp` binary is missing or stale
- binds `fixer_mcp` to a wrapper-managed state directory, defaulting to `~/.local/state/fixer-client-wires/`
- keeps the SQLite database at `~/.local/state/fixer-client-wires/fixer.db` unless `FIXER_CLIENT_WIRES_STATE_ROOT` overrides it
- materializes backend-specific MCP config for Droid (`.factory/mcp.json`) and Claude (`.mcp.json`)
- injects Codex MCP config overrides inline so Ubuntu operators do not need to hand-edit shell aliases or private config files before first use

## Package layout

- `src/fixer_client_wires/bootstrap.py`: runtime resolution and import-path bootstrap
- `src/fixer_client_wires/cli.py`: package CLI and module entrypoint
- `src/fixer_client_wires/launcher.py`: staged role and backend launch-plan boundary
- `src/fixer_client_wires/backends/`: staged backend descriptors and command builders
- `src/fixer_client_wires/staged/`: bundled runtime, config, example, and skill assets for installed-package fallback
- `config/mcp-config.json`: package-local starter config for the GitHub-ready track
- `examples/mcp-config.example.json`: package-local example config for first boot and docs
- `runtime/fixer_runtime/`: staged local runtime placeholder for the public track
- `pyproject.toml`: installable packaging metadata for the new distribution path
- direct `fixer` entrypoint now runs a phased interactive flow: role selection first, then Fixer `Start new` vs `Resume existing`, then backend/model/reasoning prompts for fresh launches
