# fixer-mcp-server

`fixer-mcp-server` is the staged Go control-plane package for the GitHub-ready Fixer MCP track.

This package is intentionally incremental. It brings the current server module, migration helpers, and validation path into `github_repo/` without changing the legacy workspace outside this directory.

## What Is Staged Here

- `main.go`: MCP server entrypoint and tool surface
- `cmd/project_doc_hard_replace`: one-shot project-doc replacement/import helper
- `migrate.go` and `migrate_docs.go`: legacy migration helpers kept for compatibility
- `examples/mcp-config.example.json`: package-local MCP registration example without private absolute paths
- `Makefile`: package-local build and validation commands
- `.gitignore`: local-state and build-output exclusions for the package directory

## Compatibility Position

- The Go module name remains `fixer_mcp` for now to avoid an unnecessary compatibility break during the packaging slice.
- Legacy sources outside `github_repo/` remain the reference implementation during the migration window.
- This staged package is the first public package boundary, not the final publication layout.

## Local State Boundary

The server writes `fixer.db` and `fixer_mcp.log` in its current working directory. Keep local runtime state inside this package directory or a wrapper-managed working directory rather than committing it into the repo.

Ignored local artifacts include:

- `fixer.db`
- `fixer.db-shm`
- `fixer.db-wal`
- `fixer_mcp.log`
- `migration_backups/`
- `migration_reports/`
- built binaries such as `fixer_mcp`

## Prerequisites

- Go `1.25.4` or newer
- SQLite through the bundled Go driver

## Validation

```bash
make test
```

Equivalent direct command:

```bash
go test ./...
```

## Build

```bash
make build
```

## Run

Build first, then run from this directory so the local state stays package-scoped:

```bash
make run
```

## MCP Registration Example

See [`examples/mcp-config.example.json`](examples/mcp-config.example.json) for a public-repo-safe starting point. It uses a placeholder package path instead of a private absolute path.
