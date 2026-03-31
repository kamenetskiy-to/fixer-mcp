# Client Wires

Canonical client launch wires for Fixer MCP live in this directory.

These wires are the bridge between durable orchestration state in `fixer_mcp` and actual worker execution in Codex-backed sessions. They are what turns stored sessions, attached docs, MCP selections, and review rules into real Fixer, Netrunner, and Overseer runs.

## Common Path

For a new operator, the usual first commands are:

```bash
python3 client_wires/fixer_wire.py --wire-info
python3 client_wires/fixer_wire.py --role fixer
```

If `mcp_servers/codex_pro_app` is not in the default sibling location, set `MCP_SERVERS_ROOT` first.

## Why These Wires Matter

This directory is more than a launch convenience layer. It is where the repo converts the control-plane model into disciplined execution:
- role-aware startup for Fixer, Netrunner, and Overseer
- session-aware resume flows
- deterministic MCP assignment injection
- explicit autonomous worker dispatch and wake-up behavior
- backend-aware launch metadata for `codex` and `droid`

## Flow Map

- `explicit Fixer MCP flow`: this is the canonical explicit path for MCP-sensitive or durable Fixer-managed Netrunners. For live Fixer threads, prefer `fixer_mcp.launch_and_wait_netrunner`, `launch_explicit_netrunner` plus `wait_for_netrunner_session`, or `launch_explicit_netrunner` plus `wait_for_netrunner_sessions` when multiple safe sidecar workers are already running. The durable/background variant is still implemented by `client_wires/fixer_autonomous.py`.
- `manual separate-terminal`: use `$manual-resolution` when the Architect wants to launch or resume the Netrunner personally in another terminal. The old `$start-netrunner` skill is no longer the canonical entrypoint.
- `review and closure`: use `$manual-acceptance` when a completed session needs Fixer review, acceptance, rejection, or lifecycle closure. Despite the legacy name, this review phase is shared across manual, autonomous, and native worker launches.

## Model Policy

- Default model: `gpt-5.4`
- Default reasoning effort: `medium`
- Use `gpt-5.4-mini` only for trivial, tightly bounded, low-risk chores
- Escalate to `high` or `xhigh` only for unusually difficult debugging, architecture, or ambiguous investigation

## Fixer Wire

- Entrypoint: `client_wires/fixer_wire.py`
- Purpose: launch role flows for `fixer`, `netrunner`, and `overseer`.
- Runtime source: delegates into `codex_pro_app` from `mcp_servers`, but canonical wire ownership and bootstrap logic are here.
- Repo-local MCP additions for this project should live in the root `mcp_config.json` so the base `codex_pro_app` launcher path discovers them automatically; `client_wires/fixer_wire.py` also overlays optional root `webMCP.toml` entries after that base discovery pass for wire-specific additions.
- Manual Netrunner launch now injects `$manual-resolution` with a preselected session and MCP set, then stops after the initialization checklist unless the Architect explicitly pre-approved immediate execution.
- Netrunner UX:
  - Uses keyboard-friendly interactive selectors (arrow keys + enter).
  - Enforces two dialogs in sequence (session picker, then MCP checklist).
  - Session picker shows only `in_progress` by default; `+` toggles archived statuses.
  - MCP checklist hides `fixer_mcp` (forced always-on) and persists manual overrides back to `fixer.db`.
- MVP scaffold UX:
  - `fixer --scaffold-mvp <project_slug>` creates a fresh Serverpod + Flutter + `llm_pipeline` starter from any cwd.
  - Optional `--scaffold-target-dir <parent_dir>` chooses where the new project root is created.
  - The scaffold runs before Codex bootstrap or project registration checks, so it also works for brand-new projects.
  - Plain `fixer` now exposes `MVP Scaffold` in the first menu, then asks for project name, target directory, and `dry-run` vs real create mode.
- Autonomous Netrunner dispatch:
  - `python3 client_wires/fixer_autopilot.py --cwd <project_root> --max-parallel <n>` polls Fixer-backed pending sessions and launches Netrunners automatically.
  - It reuses existing session MCP assignments and attached-doc context instead of inventing its own dispatch state.
  - Retry behavior is bounded and process-driven, borrowing the core idea from Symphony's unattended dispatch loop.
- Serial autonomous Fixer loop:
  - `python3 client_wires/fixer_autonomous.py register-fixer --cwd <project_root>` stores the active Fixer resume thread for later autonomous wake-ups.
  - `register-fixer` resolves the Fixer Codex session in this order: explicit `--fixer-session-id`, then `CODEX_THREAD_ID` / `CODEX_SESSION_ID` from the current shell, then history-based auto-detection.
  - `fixer_mcp.launch_explicit_netrunner` starts the same explicit worker path from inside Fixer MCP.
  - `fixer_mcp.wait_for_netrunner_session` polls Fixer MCP session/proposal state and returns structured review-ready completion metadata.
  - `fixer_mcp.wait_for_netrunner_sessions` waits across an explicit project-scoped session list or the current project's active explicit-launch candidates, then returns the first review-ready or terminal winner.
  - `fixer_mcp.launch_and_wait_netrunner` composes both so an Architect-visible Fixer thread can stay in-place through MCP-sensitive worker completion.
  - The repo-managed wire now forces the attached `fixer_mcp` server timeout floor to `21600s`, matching the long explicit wait window and avoiding the old accidental `600s` client cutoff on `launch_and_wait_netrunner`.
  - If some external client ignores MCP server timeout settings, use `launch_explicit_netrunner` plus `wait_for_netrunner_session` as the safe fallback instead of assuming one long blocking call will survive.
  - Bounded parallel sidecar launches are allowed only when task ownership or write scopes are clearly non-overlapping, investigation is not duplicated on the same unresolved thread, and the Fixer remains review authority for every worker result.
  - Launch without immediate wait is valid for that sidecar pattern; use `wait_for_netrunner_sessions` to re-enter review as soon as one worker finishes.
  - If multiple workers qualify on the same poll, the deterministic winner is the lowest project-scoped session id in the considered set.
  - `python3 client_wires/fixer_autonomous.py launch-netrunner --cwd <project_root> --session-id <local_session_id>` launches one headless-durable Netrunner using the project's assigned MCP/doc context and the implementation-session rule that code changes ship with relevant automated test additions/updates/removals.
  - Routine operator Telegram updates now go through `fixer_mcp.send_operator_telegram_notification` with project-bound context; `telegram_notify` is no longer part of the normal Fixer/Netrunner MCP surface for this workflow.
  - Older broken tests in scope are part of the worker obligation when they block the change; Ghost Run must not degrade into code-only delivery.
  - When a final tester worker reports bugs, the autonomous review handoff is expected to spawn repair Netrunner sessions from those findings before Ghost Run concludes.
  - `fixer_mcp.wake_fixer_autonomous` is the project-scoped wake-up tool for autonomous Netrunners; it resumes the registered Fixer thread headlessly into `$manual-acceptance`, then the Fixer continues the serial autonomous loop.
- Fixer UX:
  - Shows a role-specific pre-screen: `Start new Fixer` or `Resume existing Fixer`.
  - Resume picker lists prior Fixer Codex sessions with `started` and `updated` timestamps.
  - No MCP selection UI is shown for Fixer; `fixer_mcp` remains forced-attached.
  - Fixer launch uses `--sandbox danger-full-access` to preserve codex-pro style cross-cwd filesystem behavior.
- Launch is blocked for unknown project cwd with explicit Fixer onboarding instructions (`register_project`), avoiding any direct DB insert fallback.
  - Unknown cwd guidance is Fixer-only (`register_project`) and must not fall back to direct DB writes.
- Overseer UX:
  - Overseer launch also defaults to `--sandbox danger-full-access` unless an explicit sandbox flag is passed through.
- Session resume:
  - Tracks `codex` session IDs in `session_codex_link`.
  - Selecting an archived session (`review`/`completed`) auto-resumes `codex` by stored session ID.
- Session lifecycle closure: Fixer/Overseer now finalize reviewed work via `set_session_status` (typically `review` -> `completed`), with optional rollback to `pending`/`in_progress` when rework is needed.
- Netrunner supports non-interactive verification flags:
  - `--netrunner-session-id <id>`
  - `--netrunner-mcp <name[,name...]>` (repeatable)
  - `--dry-run` (prints resolved launch command without running Codex)

## Compatibility Bridge

Legacy alias target remains supported:

- `python3 ../mcp_servers/fixer.py`

That legacy script delegates into this repo-local entrypoint, so execution passes through `client_wires/fixer_wire.py`.

## Canonical Docs

- Runtime modes: `project_book/clean_docs/30_ops/runtime_modes_native_vs_headless.md`
- Native prompt helpers: `project_book/clean_docs/30_ops/native_netrunner_prompt_helpers.md`
- Native Telegram operator notifications: `project_book/clean_docs/30_ops/native_telegram_operator_notifications.md`
- Project handoff storage and tools: `project_book/clean_docs/20_architecture/project_handoff_storage.md`

## Quick Checks

- `python3 client_wires/fixer_wire.py --wire-info`
- `python3 ../mcp_servers/fixer.py --wire-info`
- `python3 client_wires/fixer_wire.py --role fixer --help`
- `python3 client_wires/fixer_wire.py --scaffold-mvp sample_app --dry-run`
- `python3 client_wires/fixer_autopilot.py --cwd /path/to/project --once --dry-run`
- `python3 client_wires/fixer_autonomous.py register-fixer --cwd /path/to/project --fixer-session-id <codex_session_id>`
- `python3 client_wires/fixer_autonomous.py launch-netrunner --cwd /path/to/project --session-id 3`
