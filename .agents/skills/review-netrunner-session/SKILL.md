---
name: review-netrunner-session
description: "Use this skill when a Fixer must review a completed Netrunner session, inspect work and tests, approve or reject doc proposals, then complete the session or send precise rework."
---

# Review Netrunner Session

Use this skill as the Fixer review and acceptance flow for any Netrunner
session returned by `complete_task` (normally status `review`).

It applies to manual, autonomous, and Fixer MCP-native worker launches.

## Review Flow

1. Authenticate as `fixer`.
2. Load current internal Fixer MCP docs.
3. Read the target session with `get_session` and confirm its structured report,
   declared scope, and current task text.
4. Read append-only worker history with `view_netrunner_logs` when available.
5. Read pending doc proposals with `review_doc_proposals`.
6. Validate the actual work, not only the worker report or logs.
7. For implementation sessions, verify:
   - required code changes were made
   - relevant automated tests were added, updated, or removed
   - older broken tests in scope were fixed when they blocked delivery
8. For an automatic wave reviewer, do not treat implementation workers being
   terminal as reviewer terminality. Inspect the linked reviewer session and
   process; a reviewer launch failure is persisted for Fixer follow-up and is
   not an acceptance signal.
9. Treat every doc proposal as a hypothesis requiring Fixer judgment.
10. If valid, approve the relevant proposals and set the reviewed session to
    `completed`; `complete_task` itself only moves a worker to `review`.
11. If incomplete, reject or leave proposals pending as appropriate, append
    precise rework instructions, and move the session back to `pending` or
    `in_progress`. A task update changes durable metadata and is not a live
    hot-patch to an already-running provider process; relaunch or rework
    explicitly when the worker must receive new instructions.

## Wave Acceptance And Repair

For a wave with the governed phase contract, implementation review is not the
end of the lifecycle:

1. Reconcile all scheduled workers and the failure policy before advancing.
2. A first eligible single-worker/minority failure enters `repair_required`;
   use `authorize_netrunner_wave_repair` for the one selected failed worker.
   A strict failure majority pauses the wave. After the one repair is consumed,
   another minority or tie requires manual repair; do not invent an untracked
   serial repair worker.
3. Once all implementation workers are terminal, the failure policy is
   `passed`, and the implementation reviewer is completed and approved, call
   `transition_netrunner_wave_phase(target_phase="acceptance", ...)` with a
   distinct project-scoped pending acceptance session.
4. Review the acceptance session as its own governed step. Only after it is
   completed and reviewed may the Fixer transition the wave to `completed`.
   Recursive-capable waves also require an exact committed `handoff_sha`.
   Completion closes the wave gate and releases its scope leases.

Current runtime blocker: `transition_netrunner_wave_phase` requires a completed
implementation reviewer session even when the wave uses `manual` review, but
manual policy intentionally creates no reviewer Netrunner. Therefore manual
waves (including monowaves, which must remain manual) cannot currently enter
this acceptance transition through the runtime contract. Record that blocker;
do not invent a reviewer session or claim that Fixer-only review satisfies the
handler until the runtime contract is changed.

## Acceptance Cleanup

After accepting a Netrunner, make the repository state explicit.

For implementation work, actively drive toward a clean tracked worktree: commit,
integrate, stash, or remove generated artifacts according to the project's
established Git policy. Treat lingering tracked dirt after acceptance as
unfinished review work unless it is deliberately preserved and documented.

Do not revert unrelated user changes. If unrelated pre-existing dirt remains, preserve it and document it in the handoff or report.

When accepted work is preparing future parallel waves, a clean tracked worktree
is required before wave creation. If the accepted worker leaves follow-up work,
close it serially or create a replacement session before trying to launch the
wave.

## Acceptance Standards

- No blind approval.
- No code-only acceptance when tests were required.
- No canon merge without explicit review; Netrunner logs are evidence, not canonical docs.
- Prefer concrete rejection reasons over vague dissatisfaction.
