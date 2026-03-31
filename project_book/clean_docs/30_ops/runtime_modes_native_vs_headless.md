# Runtime Modes: Explicit Fixer MCP Flow vs Manual Separate-Terminal Flow

## Explicit Fixer MCP Flow

Use this as the canonical explicit execution model when a Fixer-managed Netrunner needs deterministic project-scoped MCP mounting or durable launch/wait behavior.

Characteristics:

- one Fixer MCP review authority at a time
- launch through `fixer_mcp.launch_and_wait_netrunner`, `launch_explicit_netrunner` + `wait_for_netrunner_session`, or `launch_explicit_netrunner` + `wait_for_netrunner_sessions`
- durable/background continuation is still implemented by `client_wires/fixer_autonomous.py`
- still uses the same Fixer MCP sessions, attached docs, MCP assignments, and review authority
- live Fixer threads can stay in-place and wait for review-ready completion
- the repo-managed Fixer wire raises the forced `fixer_mcp` timeout floor to `21600s` so `launch_and_wait_netrunner` is not cut off by a stale `600s` client-side default
- if an external client does not honor MCP server timeout settings, prefer `launch_explicit_netrunner` plus `wait_for_netrunner_session` over one long blocking call
- routine operator Telegram updates go through `fixer_mcp.send_operator_telegram_notification`
- serial execution remains the default, but bounded parallel sidecar launches are allowed when write scopes are clearly disjoint, investigation is not duplicated on the same unresolved thread, and the Fixer reviews every worker result before closure
- `fixer_mcp.wait_for_netrunner_sessions` snapshots either an explicit project-scoped session list or the current project's active explicit-launch candidates, then returns the first review-ready or terminal winner
- if multiple candidates qualify on the same poll, the deterministic winner is the lowest project-scoped session id in the considered set
- launch without immediate wait is valid for sidecar work under those constraints
- bounded non-MCP-sensitive work can still use native in-thread Codex workers when the Architect prefers that lighter path

Primary skill mapping:

- `$autonomous-resolution` for long serial delivery
- `$autonomous-resolution-one-task` for one bounded task now
- `$manual-acceptance` for review/closure

## Manual Separate-Terminal Flow

Use this only when the Architect explicitly wants to launch or resume the Netrunner in another terminal.

Characteristics:

- `$manual-resolution` is the separate-terminal version of the same explicit-flow choice
- the launched Netrunner follows `$manual-resolution` with execution already pre-approved
- review still ends in `$manual-acceptance`, which is the shared Fixer review skill despite the legacy name

## Model Policy

- default model: `gpt-5.4`
- default reasoning effort: `medium`
- use `gpt-5.4-mini` only for trivial, low-risk chores
- escalate to `high` or `xhigh` only for exceptional difficulty
