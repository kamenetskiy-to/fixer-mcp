# Compatibility

## Non-Negotiable Boundary

Everything outside `github_repo/` remains untouched during the migration. The legacy workspace is reference material and continues to serve existing operators.

## Compatibility Rules

- preserve current legacy commands until the new repo can replace them cleanly
- preserve old environment variable overrides during the migration window
- treat current export tooling as temporary compatibility infrastructure, not the desired long-term publication model
- keep session and orchestration concepts stable across both worlds

## Compatibility Surfaces To Preserve

- legacy launcher entrypoints that route into Fixer / Netrunner / Overseer workflows
- environment overrides such as `MCP_SERVERS_ROOT` where operators already depend on them
- current explicit-worker and autonomous execution semantics
- operator expectations around project docs, session assignment, and review authority

## Staged Bridge In `github_repo/`

Slice 5 adds a real repo-local compatibility package at `packages/compat-bridge`.

Current bridge behavior:

- `python3 -m fixer_compat_bridge --wire-info` maps the old flag shape onto the staged `fixer_client_wires wire-info` command
- `python3 -m fixer_compat_bridge --role <fixer|netrunner|overseer>` maps legacy role selection onto `fixer-client-wires plan-launch`
- `MCP_SERVERS_ROOT` remains a supported compatibility override because the delegated `client-wires` bootstrap still honors it
- repo-root `mcp_config.json` remains a fallback migration surface, but package-local config is the preferred default

This keeps the old invocation vocabulary usable while forcing execution through the new package boundary inside `github_repo/`.

## Incremental Operator Move

1. Install `packages/client-wires`.
2. Install `packages/compat-bridge`.
3. Validate runtime/config resolution with `python3 -m fixer_compat_bridge --wire-info`.
4. Preview role launches with `fixer-compat-bridge --role fixer` or `--role netrunner`.
5. Move to direct `fixer-client-wires` commands once the staged contract is familiar.

## Compatibility Surfaces To Change

- the default onboarding path must stop requiring sibling workspace layout knowledge
- the public repo must stop presenting export mechanics as part of the normal product story
- repo-native examples and package-local config should replace private root-file assumptions
