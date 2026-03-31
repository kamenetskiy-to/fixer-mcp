# Contributing

This file is for maintainers and contributors working on the GitHub-ready Fixer MCP track.

## Current State

The repository is still in a staged migration from a legacy private workspace into a first-class public repo layout.

The important rule is:
- `github_repo/` is the canonical source for the new public track
- the old workspace outside it is legacy reference material

## Release Flow

Repo-native release work is driven from:

```bash
python3 scripts/release_public_repo.py --version <version> --dry-run --json
python3 scripts/release_public_repo.py --version <version>
```

That helper:
- runs repo Python tests
- runs Go tests for `packages/fixer-mcp-server`
- builds Python package artifacts
- builds the staged Go binary
- assembles a canonical publication snapshot
- writes release metadata into `dist/releases/<version>/`

## Staged Packages

- `packages/fixer-mcp-server`
  The staged Go control plane package.
- `packages/client-wires`
  The staged public launcher package.
- `packages/compat-bridge`
  Compatibility helpers for operators with older launcher habits.

## Validation

Common checks:

```bash
python3 -m unittest discover -s tests
cd packages/fixer-mcp-server && go test ./...
```

## Migration Context

The public track exists to remove private-workspace assumptions from the default user story while preserving backward compatibility during transition.

Useful deep-dive docs:
- [docs/architecture.md](docs/architecture.md)
- [docs/migration-plan.md](docs/migration-plan.md)
- [docs/implementation-slices.md](docs/implementation-slices.md)
- [docs/compatibility.md](docs/compatibility.md)
