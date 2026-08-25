# Client Wires

Canonical client launch wires for Fixer MCP live in this directory.

These wires are the bridge between durable orchestration state in `fixer_mcp` and actual worker execution in Codex-backed sessions. They are what turns stored sessions, attached docs, MCP selections, and review rules into real Fixer, Netrunner, and Overseer runs.

## Common Path

For a new operator, the usual first commands are:

```bash
python3 client_wires/fixer_wire.py --wire-info
python3 client_wires/fixer_wire.py --role fixer
```

No sibling `mcp_servers/codex_pro_app` checkout is required; the launcher uses
the repo-vendored `client_wires.codex_compat` package.

## Why These Wires Matter

This directory is more than a launch convenience layer. It is where the repo converts the control-plane model into disciplined execution:
- role-aware startup for Fixer, Netrunner, and Overseer
- session-aware resume flows
- deterministic MCP assignment injection
- explicit autonomous worker dispatch and wake-up behavior
- backend-aware launch metadata for `codex` and `droid`

## Flow Map

- `Fixer-managed worker flow`: every autonomous Netrunner is created, launched, awaited, and cleaned up through Fixer MCP wave tools, including one-worker waves.
- `project Hands`: the `Руки (Project Hands)` role opens the current project's one
  durable mailbox. The terminal provider process is disposable and reloads
  history from Fixer MCP instead of becoming a permanent session.
- `compatibility execution`: waves, the Hands dispatcher, and explicit governed
  acceptance may launch a preselected session envelope; the operator does not
  create or browse ad-hoc sessions.
- `review and closure`: use `$review-netrunner-session` when a completed session needs Fixer review, acceptance, rejection, or lifecycle closure.

## Netrunner Worker Model Policy

Backend/model/reasoning for Netrunner workers is owned by the
`netrunner-backend-models` skill. Read it before recommending or launching
workers; it contains the current quota gate, temporary provider overrides, and
model-specific reasoning constraints.

## Fixer Wire

- Entrypoint: `client_wires/fixer_wire.py`
- Purpose: launch role flows for `fixer`, `netrunner`, and `overseer`.
- Runtime source: uses the vendored `client_wires.codex_compat` launcher helpers.
- Repo-local MCP additions for this project should live in the root `mcp_config.json` so the base launcher path discovers them automatically; `client_wires/fixer_wire.py` also overlays optional root `webMCP.toml` entries after that base discovery pass for wire-specific additions.
- Project Hands UX:
  - `--role netrunner` without a session ID opens the current project's one
    permanent `Руки` channel.
  - The lane selector displays `codex`/`claude`/`kimi`/`antigravity` as
    available lanes of the same actor.
  - Kimi Code in Project Hands is interactive-only. Never add `-p`, `--print`,
    native prompt submission, or a headless smoke path; use the same manual
    Kimi TUI flow as `fixer -> fixer`.
  - Each open starts a fresh disposable provider client, then reads mailbox and
    history through Fixer MCP. Provider transcript resume is not channel
    identity.
  - No task/session picker, task-scoped MCP checklist, model picker, or reasoning
    picker is part of the public Hands path.
  - Repository-write instructions remain governed and await explicit Fixer
    review.
- MVP scaffold UX:
  - `fixer --scaffold-mvp <project_slug>` creates a fresh Serverpod + Flutter + `llm_pipeline` starter from any cwd.
  - Optional `--scaffold-target-dir <parent_dir>` chooses where the new project root is created.
  - The scaffold runs before Codex bootstrap or project registration checks, so it also works for brand-new projects.
  - Plain `fixer` now exposes `MVP Scaffold` in the first menu, then asks for project name, target directory, and `dry-run` vs real create mode.
- Wave-only autonomous Fixer loop:
  - `python3 client_wires/fixer_autonomous.py register-fixer --cwd <project_root>` stores the active Fixer resume thread for later autonomous wake-ups.
  - `register-fixer` resolves the Fixer Codex session in this order: explicit `--fixer-session-id`, then `CODEX_THREAD_ID` / `CODEX_SESSION_ID` from the current shell, then history-based auto-detection.
  - `create_netrunner_wave`, `launch_netrunner_wave`, `wait_for_netrunner_wave`, and `cleanup_netrunner_wave` are the only autonomous worker lifecycle.
  - `wait_for_netrunner_wave.timeout_seconds` defaults to 300 seconds; every explicit value must be between 300 and 21600 seconds inclusive. Use 300 seconds for ordinary polling, raise it up to 21600 for an expected very heavy or long-running Netrunner, and never exceed 21600.
  - `launch_netrunner_wave.worker_configs` assigns per-session backend/model/reasoning overrides while top-level launch fields remain defaults.
  - Codex Fixer launches mount a second `fixer_netrunner_gate` MCP namespace whose server-level surface contains only `wait_for_netrunner_wave` and `wait_for_netrunner_waves`, keeping long blocking waits direct-only. Launch tools stay on the primary authenticated `fixer_mcp` surface.
  - The gate process auto-authenticates only when it is simultaneously locked to `fixer`, bound to a registered project CWD, explicitly opted into auto-auth, and started with the `netrunner_gate` tool profile. Ordinary Fixer MCP processes retain normal `assume_role` authentication.
  - The repo-managed wire forces the attached `fixer_mcp` server timeout floor to `21600s`, matching long wave waits.
  - Claude Code launch materialization writes `.mcp.json` per-server `timeout` in milliseconds, using `per_tool_timeout_ms` first and otherwise converting the wire's second-based timeout fields.
  - Serial launch-and-wait tools and the `fixer_autonomous.py launch-netrunner` CLI command are retired and must not be revived or replaced by provider CLI shell-outs.
  - Fresh role launches set `FIXER_MCP_LOCKED_ROLE` on the forced `fixer_mcp` server: `overseer` for Overseer, `fixer` for Fixer, and `netrunner` for Netrunner.
  - Internal `launch-wave-worker` and `launch-wave-reviewer` commands are wave-bound implementation details; the reviewer command validates the exact `parallel-wave-review:<wave_id>` session marker.
  - Routine operator Telegram updates now go through `fixer_mcp.send_operator_telegram_notification` with project-bound context; `telegram_notify` is no longer part of the normal Fixer/Netrunner MCP surface for this workflow.
  - Older broken tests in scope are part of the worker obligation when they block the change; Ghost Run must not degrade into code-only delivery.
  - When a final tester worker reports bugs, the autonomous review handoff is expected to spawn repair Netrunner sessions from those findings before Ghost Run concludes.
  - Wave workers do not wake the Fixer individually; `wait_for_netrunner_wave` owns completion and reviewer progression.
- Fixer UX:
  - Shows a role-specific pre-screen: `Start new Fixer` or `Resume existing Fixer`.
  - Resume picker lists prior Fixer Codex sessions with `started` and `updated` timestamps.
  - No MCP selection UI is shown for Fixer; `fixer_mcp` remains forced-attached.
  - Fixer launch uses `--sandbox danger-full-access` to preserve codex-pro style cross-cwd filesystem behavior.
- Fresh Fixer/Overseer launches ensure the current cwd is registered in the launcher DB before role startup.
  - Unknown cwd onboarding happens through the launcher path, so normal role startup does not require a manual Overseer `register_project` call first.
- Overseer UX:
  - Overseer launch also defaults to `--sandbox danger-full-access` unless an explicit sandbox flag is passed through.
- Compatibility execution resume:
  - Tracks `codex` session IDs in `session_codex_link`.
  - Resumes only an explicitly supplied internal compatibility envelope; it
    never represents the permanent Hands mailbox.
- Session lifecycle closure: Fixer/Overseer now finalize reviewed work via `set_session_status` (typically `review` -> `completed`), with optional rollback to `pending`/`in_progress` when rework is needed.
- Compatibility execution supports non-interactive verification flags:
  - `--netrunner-session-id <id>`
  - `--netrunner-mcp <name[,name...]>` (repeatable)
  - `--netrunner-acceptance` (requires `--netrunner-session-id`)
  - `--dry-run` (prints resolved launch command without running Codex)

## Compatibility Bridge

Legacy alias target remains supported:

- `python3 ../mcp_servers/fixer.py`

That legacy script delegates into this repo-local entrypoint, so execution passes through `client_wires/fixer_wire.py`.

## Canonical Docs

- Runtime modes: `docs/reference/30_ops/runtime_modes_native_vs_headless.md` in the public export
- Native prompt helpers: `docs/reference/30_ops/native_netrunner_prompt_helpers.md` in the public export
- Native Telegram operator notifications: `docs/reference/30_ops/native_telegram_operator_notifications.md` in the public export
- Project handoff storage and tools: `docs/reference/20_architecture/project_handoff_storage.md` in the public export

## Quick Checks

- `python3 client_wires/fixer_wire.py --wire-info`
- `python3 ../mcp_servers/fixer.py --wire-info`
- `python3 client_wires/fixer_wire.py --role fixer --help`
- `python3 client_wires/fixer_wire.py --scaffold-mvp sample_app --dry-run`
- `python3 client_wires/fixer_autonomous.py register-fixer --cwd /path/to/project --fixer-session-id <codex_session_id>`
