---
name: run-manual-acceptance-netrunner
description: "Run a separate-terminal Netrunner for hands-on post-autonomous acceptance, iterative fixes, runtime validation, and explicitly gated final submission."
---

# Run Manual Acceptance Netrunner

Use this skill for a separate-terminal Netrunner that stays available while the
Architect manually accepts a long Fixer-managed implementation. The operator may
click through a local or production app, report bugs, and expect iterative fixes
without every intermediate fix being treated as final completion.

## Initialization

1. Authenticate as `netrunner`.
2. Checkout the preselected session with `checkout_task`.
3. Call `log_netrunner_progress` with `log_type="started"` and a short note.
4. Load attached docs only with `get_attached_project_docs`.
5. Read assigned MCP servers when relevant.
6. If execution is not explicitly approved, ask for or confirm approval before changing code or touching runtime environments.

## App And Runtime Setup

1. Inspect the task and attached docs to determine whether the acceptance target is Flutter, web frontend, backend stack, or production/remote validation.
2. Prefer project-defined reproducible commands such as Makefile targets, scripts, package scripts, or documented commands over ad-hoc starts.
3. For Flutter/manual app work, use `dart_flutter` MCP when assigned and available. Keep hot restart or hot reload ready, preferably through project make targets when documented.
4. For web/manual frontend work, use Playwright MCP or Chrome DevTools MCP in a headed/browser-observable session when assigned and available. Start backend/frontend processes only when needed, and report URLs and process commands.
5. For backend acceptance, start only the services needed for the reported flow and run focused checks against them.
6. For production or remote validation, avoid destructive actions and state clearly which environment is being touched.

## Acceptance Loop

1. Remain available for Architect bug reports and iterative fixes.
2. Implement bugfixes in scope.
3. Run focused checks that match the changed behavior.
4. Keep local app or browser state usable for continued manual acceptance when practical.
5. Log useful milestones with `log_type="progress"`.
6. If blocked, log `blocked` when you cannot proceed, or `workaround` when you are actively trying a bypass.
7. Give concise status updates and ask only when a choice blocks progress.
8. Do not treat intermediate fixes as final completion.

## Progress Logs

Progress-log discipline and allowed `log_type` values are the same as in
`$run-manual-netrunner`; that skill owns the canonical logging rules.

## Finalization Gate

Do not submit a doc proposal, call `complete_task`, or move the session to review
until the Architect or runtime prompt explicitly asks to finish, submit,
finalize, complete, send to review, or otherwise close the Netrunner session.

When finalization is explicitly requested:

1. Create a Fixer MCP doc-impact proposal with `propose_doc_update` when there is real doc impact.
2. Use `$complete-netrunner-session` to build the final report and call `complete_task`.
3. Call `log_netrunner_progress` with `log_type="completed"` after final checks and before or immediately after `complete_task`.
4. If the runtime prompt requires it, call `wake_fixer_autonomous` after `complete_task` with the completed session ID and a concise handoff summary.

## Operator Updates

Operator update rules are the same as in `$run-manual-netrunner`.

## Constraints

- Stay in the acceptance loop until explicit finalization is requested.
- Current-thread Fixer-managed execution belongs to `$run-one-netrunner-task`.
