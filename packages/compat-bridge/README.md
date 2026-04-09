# compat-bridge

`compat-bridge` is the staged migration package for operators who still think in terms of the old launcher entrypoints.

It does not modify the legacy workspace. Instead, it adds a repo-local wrapper that accepts a small compatibility subset of the old `fixer_wire.py` flags and delegates into the staged `client-wires` package.

## Supported compatibility surface

- `python3 -m fixer_compat_bridge --wire-info`
- `python3 -m fixer_compat_bridge --role fixer`
- `python3 -m fixer_compat_bridge --role netrunner --backend codex --mcp-server fixer_mcp`
- `fixer`
- legacy runtime override via `MCP_SERVERS_ROOT`
- legacy config fallback via repo-root `mcp_config.json`

Execution contract:

- `fixer` defaults to `--role fixer` and performs a real packaged launch
- `fixer-compat-bridge --role ...` also performs a real launch by default
- add `--dry-run` or `--json` when you want the old preview-only behavior

## Quick start

```bash
python3 -m pip install -e ./packages/client-wires
python3 -m pip install -e ./packages/compat-bridge
python3 -m fixer_compat_bridge --wire-info
fixer
```

## Package layout

- `src/fixer_compat_bridge/cli.py`: legacy flag parsing and delegation into `fixer_client_wires.cli`
- `src/fixer_compat_bridge/__main__.py`: module entrypoint
- `pyproject.toml`: packaging metadata and `fixer-compat-bridge` / `fixer` console scripts
