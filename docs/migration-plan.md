# Migration Plan

## Phase 1: Canon And Scaffold

Deliver this initial buildout:

- architecture note
- migration plan
- compatibility note
- implementation slices
- placeholder package directories
- sample config

Exit criteria:

- `github_repo/` stands as a coherent source-of-truth plan
- follow-on sessions have a concrete package map and risk register

## Phase 2: Package Boundary Extraction

Move publishable code into the new package layout inside `github_repo/`:

- stage `fixer_mcp` into `packages/fixer-mcp-server`
- stage `client_wires` into `packages/client-wires`
- introduce packaging metadata and package-local config loading

Exit criteria:

- the new repo can run core validation without importing from the legacy workspace
- package-local READMEs document supported entrypoints

## Phase 3: Compatibility Bridge

Implement transition helpers:

- map legacy launcher commands into the new repo layout
- preserve old env var names and legacy invocation patterns
- add migration notes for operators who still use the private workspace

Exit criteria:

- a legacy operator can understand how to move incrementally
- the new repo does not require private layout archaeology

Current staged result:

- `packages/compat-bridge` now provides a real Python package with a `fixer-compat-bridge` console script
- the bridge accepts legacy-style `--wire-info` and `--role` flags, then delegates into the staged `client-wires` package
- compatibility env/config surfaces stay additive because the bridge relies on the package-local bootstrap contract rather than editing the legacy workspace

## Phase 4: Public Onboarding

Replace export-era onboarding with product-first docs:

- quick start for server-only path
- packaged launcher path for full operator workflow
- examples for optional MCP integrations
- repo-native onboarding document for the public checkout

Exit criteria:

- the default quick start contains no sibling checkout requirement
- product docs do not expose private workspace internals as the normal story

Current staged result:

- `docs/onboarding.md` now provides the public quick start without sibling-workspace archaeology
- `docs/release.md` now defines publication from `github_repo/` itself
- `scripts/release_public_repo.py` now stages a release plan, validates the repo, builds package artifacts through the packages' declared PEP 517 backends, and writes a release manifest under `dist/releases/<version>/`

## Phase 5: Export Retirement

Reduce or remove reliance on `scripts/prepare_public_repo.py` once parity is reached.

Exit criteria:

- GitHub publication comes from `github_repo/` directly
- legacy export tooling is either retired or explicitly demoted to temporary compatibility support
