# client-wires

This package now carries the staged public launcher contract for the GitHub-ready Fixer MCP track.

It is still additive and compatibility-aware, but it now exposes a real package boundary:

- repo-local bootstrap and config resolution
- backend descriptors for `codex` and `droid`
- role-aware launch-plan building for `fixer`, `netrunner`, and `overseer`
- package entrypoints that inspect and preview launcher behavior without importing the legacy `client_wires` tree

## Runtime contract

Resolution order:

1. `FIXER_CLIENT_WIRES_RUNTIME_ROOT`
2. staged package-local runtime in `packages/client-wires/runtime/`
3. compatibility override `MCP_SERVERS_ROOT`
4. compatibility fallback sibling checkout `../mcp_servers`

That order makes the packaged repo-local runtime the default story while preserving older operator overrides during migration.

## Config contract

Resolution order:

1. `FIXER_CLIENT_WIRES_CONFIG_PATH`
2. `FIXER_CLIENT_WIRES_CONFIG_ROOT/mcp-config.json`
3. package-local `packages/client-wires/config/mcp-config.json`
4. package-local `packages/client-wires/examples/mcp-config.example.json`
5. repo-level `examples/mcp-config.example.json`
6. compatibility-only repo-root `mcp_config.json`

That order replaces implicit root-file discovery with a deterministic public contract while keeping the legacy root filename available only as a migration fallback.

## Quick start

```bash
python3 -m pip install -e ./packages/client-wires
python3 -m fixer_client_wires wire-info
fixer-client-wires list-backends
fixer-client-wires plan-launch --role netrunner --backend codex --mcp-server fixer_mcp
```

Commands:

- `python3 -m fixer_client_wires wire-info`: print resolved runtime and config details
- `fixer-client-wires list-roles`: show the packaged launch roles
- `fixer-client-wires list-backends`: show the staged backend adapter surface
- `fixer-client-wires plan-launch ...`: preview a headless launch command and backend notes using only package-local code

The `plan-launch` command resolves runtime/config through the public package contract and loads MCP server names from the selected config file. For `codex` it renders explicit `--mcp=` flags; for `droid` it reports the staged `.factory/settings.json` side effect that the full runtime would materialize.

## Package layout

- `src/fixer_client_wires/bootstrap.py`: runtime resolution and import-path bootstrap
- `src/fixer_client_wires/cli.py`: package CLI and module entrypoint
- `src/fixer_client_wires/launcher.py`: staged role and backend launch-plan boundary
- `src/fixer_client_wires/backends/`: staged backend descriptors and command builders
- `config/mcp-config.json`: package-local starter config for the GitHub-ready track
- `examples/mcp-config.example.json`: package-local example config for first boot and docs
- `runtime/fixer_runtime/`: staged local runtime placeholder for the public track
- `pyproject.toml`: installable packaging metadata for the new distribution path
