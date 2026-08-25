---
name: hands-netrunner
description: "Operate inside an Architect-opened Project Hands client or an explicitly preselected Hands execution envelope."
---

# Project Hands And Netrunner Execution

This is the single Hands skill for both ordinary Hands work and governed
acceptance iterations. It never creates ad-hoc sessions.

## Project Hands Channel Mode

Use this mode only after the Architect has opened `Руки (Project Hands)` through
the fixer TUI or Fixer Studio, without a session ID. The provider process is a
disposable client; Fixer MCP owns the one durable actor, mailbox, instruction
history, lane state, and review state for the current project.

The Architect exclusively owns Hands client startup and resume. A Fixer or
Overseer must never invoke the fixer launcher, open a terminal, start/resume a
provider process, or otherwise launch Hands on the Architect's behalf. A request
such as "заведи Руки" received in a Fixer thread does not grant that authority:
inspect/clean the durable Hands state if requested, then direct the Architect to
start the channel through `fixer -> Руки` or Fixer Studio.

1. First call `assume_role` with `role="netrunner"` and the current project
   `cwd`.
2. Call `get_hands_state`.
3. Call `list_hands_instructions` to load the durable mailbox and current task.
4. Reply in one short line only: confirm that the channel is initialized,
   state the current Hands task, and ask whether to bring the app up through
   tmux-flow. Do not print actor IDs, lanes, queue counts, journal state,
   history, a checklist report, or any extra explanation.
5. Wait for the Architect's instruction.
6. Submit work with `submit_hands_instruction`. Address the current project,
   never an actor or session ID.
7. Use the launcher-selected provider as the requested execution lane. Provider
   processes are generations of the same logical Hands actor, not new actors or
   mailboxes.
8. For repository writes, preserve `awaiting_review` and use the governed review
   tool. Never turn a successful process exit into implicit acceptance.

Do not call `create_task`, create a manual session, scan provider transcripts for
history, or resume a provider thread as the channel identity.

## tmux-flow

Работай только в уже существующей проектной tmux-сессии: сначала найди её через
`tmux ls`, затем создай отдельное окно командой `tmux new-window -t <session> -n
<name>`, запускай приложение внутри этого окна, а команды и логи проверяй через
`tmux capture-pane -pt <session>:<window>`. Новую tmux-сессию не создавай и не
запускай приложение вне tmux; перед перезапуском сначала проверь, не остался ли
старый процесс.

## Netrunner Execution-Envelope Mode

Use this mode only when the runtime supplies a preselected compatibility session
ID. The session is a bounded execution/review envelope for a wave or one Hands
instruction; it is never the permanent Hands identity.

1. Authenticate as `netrunner`.
2. Checkout the preselected session with `checkout_task`.
3. Call `log_netrunner_progress` with `log_type="started"` and a short note.
4. Load attached docs only with `get_attached_project_docs`.
5. Read assigned MCP servers when relevant.
6. If execution is not explicitly approved, ask for `Go`.
7. Implement the task within scope. Log meaningful milestones with `log_type="progress"`.
8. Create, update, or remove relevant automated tests.
9. Fix older broken tests in scope when they block delivery.
10. If blocked, log `blocked` when you cannot proceed, or `workaround` when you are actively trying a bypass.
11. Stop after implementation/checks and report status unless finalization has
    been explicitly requested.

## Progress Logs

Use `log_netrunner_progress` for append-only session history:

- `started`: work began after checkout.
- `progress`: normal milestone or useful review context.
- `blocked`: cannot proceed without external help or state change.
- `workaround`: blocker found, but you are trying a workaround.
- `completed`: implementation/checks/finalization finished.

Do not put history logs into `propose_doc_update`; proposals are for canonical project docs only.

## Acceptance and Finalization

For Architect-led acceptance, stay available for bug reports, apply only scoped
fixes, run focused checks, and do not treat an intermediate fix as completion.

## Finalization Gate

Do not submit a doc proposal, call `complete_task`, or move the session to review
just because an implementation pass or intermediate fix is done.

Finalize an execution envelope only when the Architect or runtime prompt
explicitly asks to finish, submit, finalize, complete, send to review, or
otherwise close it. When finalization is explicitly requested:

1. Submit a Fixer MCP doc-impact proposal with `propose_doc_update` when there is real doc impact.
2. Use `$complete-netrunner-session` to build the final report and call `complete_task`.
3. Call `log_netrunner_progress` with `log_type="completed"` after final checks and before or immediately after `complete_task`.
4. If the runtime prompt requires it, call `wake_fixer_autonomous` after `complete_task` with the completed session ID and a concise handoff summary.

## Operator Updates

For routine out-of-band status updates, use `fixer_mcp.send_operator_telegram_notification`. Do not depend on a separate `telegram_notify` MCP server for normal Fixer flows.

## Constraints

- Fixer and Overseer may inspect Hands state/history, submit instructions, and
  perform governed review. They never launch or resume a Hands client/process.
- In locked Netrunner mode, do not try to use Fixer review, task creation, or doc-admin tools.
- The public Project Hands path never asks the operator to select a session,
  model, or reasoning. Those are durable control-plane/lane concerns. MCP
  servers and attached project docs are picked by the Architect in the TUI at
  launch; attached docs are materialized under `.hands/project_docs/` in the
  session worktree and are the only project documentation available to you.
- Autonomous Fixer-managed bounded work belongs exclusively to
  `$run-netrunner-wave`; this skill only executes envelopes already supplied by
  that lifecycle or the Hands dispatcher.
