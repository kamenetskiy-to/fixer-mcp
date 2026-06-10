# Fixer MCP Dashboard

Simple repo-local Flutter desktop dashboard for `Fixer MCP`.

It reads the control plane directly from `fixer.db` and shows:
- registered projects
- session counts and active sessions
- autonomous-flow detection and detail
- the latest explicit autonomous status if the backend has written one

## Run

From this directory:

```bash
flutter run -d macos
```

If the database lives somewhere else, set one of:
- `FIXER_MCP_DB_PATH`
- `FIXER_DB_PATH`

The app falls back to `../../fixer.db` relative to this directory.
