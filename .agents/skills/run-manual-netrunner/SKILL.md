---
name: run-manual-netrunner
description: "Run the manual separate-terminal Netrunner flow for Fixer MCP: either prepare a deterministic handoff from an orchestrator, or execute a preselected Netrunner session with docs, tests, and explicitly gated finalization."
---

# Run Manual Netrunner

Use this skill for the separate-terminal Netrunner path. It is valid when a child worker needs explicitly mounted MCP tools or when the Architect asks for a manual worker terminal.

## Orchestrator Handoff Mode

Use this mode only as Fixer or Overseer when preparing a manual Netrunner launch.

1. Authenticate in the current role.
2. Load internal Fixer MCP docs through project-doc tools.
3. Create the Netrunner session deterministically.
4. Attach docs and assign MCP servers.
5. Verify attachments and MCP assignments.
6. Report the session ID, attached docs, assigned MCPs, execution approval state, and recommended model policy.

## Netrunner Execution Mode

Use this mode when you are the separate-terminal Netrunner.

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
11. Stop after implementation/checks and report status unless finalization has been explicitly requested.

## Progress Logs

Use `log_netrunner_progress` for append-only session history:

- `started`: work began after checkout.
- `progress`: normal milestone or useful review context.
- `blocked`: cannot proceed without external help or state change.
- `workaround`: blocker found, but you are trying a workaround.
- `completed`: implementation/checks/finalization finished.

Do not put history logs into `propose_doc_update`; proposals are for canonical project docs only.

## Finalization Gate

Do not submit a doc proposal, call `complete_task`, or move the session to review
just because an implementation pass or intermediate fix is done.

Finalize only when the Architect or runtime prompt explicitly asks to finish,
submit, finalize, complete, send to review, or otherwise close the Netrunner
session. When finalization is explicitly requested:

1. Submit a Fixer MCP doc-impact proposal with `propose_doc_update` when there is real doc impact.
2. Use `$complete-netrunner-session` to build the final report and call `complete_task`.
3. Call `log_netrunner_progress` with `log_type="completed"` after final checks and before or immediately after `complete_task`.
4. If the runtime prompt requires it, call `wake_fixer_autonomous` after `complete_task` with the completed session ID and a concise handoff summary.

## Operator Updates

For routine out-of-band status updates, use `fixer_mcp.send_operator_telegram_notification`. Do not depend on a separate `telegram_notify` MCP server for normal Fixer flows.

## Constraints

- In locked Netrunner mode, do not try to use Fixer review, task creation, or doc-admin tools.
- Current-thread Fixer-managed execution belongs to `$run-one-netrunner-task`.
