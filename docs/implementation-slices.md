# Implementation Slices

## Slice 1: Bootstrap Decoupling

Scope:

- remove required dependency on sibling `../mcp_servers`
- define packaged runtime resolution for `client-wires`

Acceptance criteria:

- a new checkout can run the packaged launcher path without private sibling directories
- advanced overrides still work via environment or explicit config

Risks:

- hidden imports from the shared launcher runtime

## Slice 2: Config Surface Cleanup

Scope:

- replace implicit root-file discovery with documented config paths
- add example config for the public repo

Acceptance criteria:

- config lookup order is documented and deterministic
- public examples cover first boot without private files
- package-local starter config exists for the GitHub-ready path

Risks:

- autonomous and explicit-worker flows may rely on root-relative config side effects

## Slice 3: Server Packaging

Scope:

- package the Go control plane as a publishable unit
- define what remains optional, including dashboard assets

Current staging status:

- Go control-plane source is staged under `packages/fixer-mcp-server`
- package-local validation commands live in `packages/fixer-mcp-server/Makefile`
- package-local MCP registration example lives in `packages/fixer-mcp-server/examples/`
- local runtime state is excluded via `packages/fixer-mcp-server/.gitignore`

Acceptance criteria:

- server package has its own README and validation commands
- required runtime artifacts are separated from local state

Risks:

- optional dashboard support may pull in large or platform-specific baggage

## Slice 4: Launcher Packaging

Scope:

- package the Python wire layer
- formalize backend and MCP adapter boundaries

Current staging status:

- `packages/client-wires` now ships an installable CLI surface through `fixer-client-wires` and `python -m fixer_client_wires`
- staged backend adapters exist for `codex` and `droid`
- staged role descriptors exist for `fixer`, `netrunner`, and `overseer`
- a packaged `plan-launch` command resolves runtime/config and previews headless launch commands without importing the legacy launcher tree

Acceptance criteria:

- launcher package documents supported roles and entrypoints
- launcher tests run without importing the legacy workspace tree

Risks:

- backend adapters may still encode project-specific assumptions

## Slice 5: Compatibility Bridge

Scope:

- add wrappers for old commands, env vars, and migration guidance

Acceptance criteria:

- old operators get an explicit migration path
- backward compatibility is tested rather than implied

Risks:

- too much compatibility logic can freeze the new design in legacy constraints

## Slice 6: Public Docs And Release Flow

Scope:

- replace export-derived public docs with repo-native docs
- define the release/publication path from `github_repo/`

Current staging status:

- `docs/onboarding.md` provides a public first-boot path for server-only and full operator workflows
- `docs/release.md` defines the staged publication flow from `github_repo/`
- `scripts/release_public_repo.py` builds a repo-native release plan, validates the repo, assembles a canonical `assembly/github_repo/` payload, and emits assembly/release manifests in `dist/releases/<version>/`

Acceptance criteria:

- public docs are product-facing
- release instructions no longer depend on the private workspace export script as the canonical path
- the staged repo can generate an inspectable assembled publication tree from `github_repo/` alone

Risks:

- documentation can drift if package boundaries are not finalized first
