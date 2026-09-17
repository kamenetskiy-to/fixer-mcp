---
name: run-netrunner-wave
description: "Use this skill when a project Fixer should dispatch multiple bounded Netrunner sessions in parallel through Fixer MCP Git worktrees, wait for review-ready workers, review each result serially, and clean up wave worktrees."
---

# Run Netrunner Wave

Use this skill for every Fixer-managed Netrunner launch. A wave may contain one worker, multiple independent workers, or dependency-gated workers whose scopes overlap only through an explicit DAG.

## Preconditions

- You are authenticated as project `fixer`.
- The current MCP tool list exposes `create_netrunner_wave`, `get_netrunner_wave`, `launch_netrunner_wave`, `wait_for_netrunner_wave`, and `cleanup_netrunner_wave`; restart the Fixer/MCP session if they are missing.
- The registered project root must be a Git repository. If it is not a Git repository, initialize it first (`git init && git add . && git commit -m "Initial commit"`). Absence of a Git repository is NEVER a reason to refuse a wave.
- There is no active orchestration freeze or stale epoch blocker.
- The wave has at least one pending session.
- Each session has a narrow, disjoint `declared_write_scope`.
- No session owns broad scope such as `.`, whole repo, shared app root, shared migrations, or the same test/dev-server state unless the Fixer has an explicit dependency DAG and a concrete isolation reason.

## Review Policy

Every wave has a `review_policy`:

- `manual` is the default. No reviewer Netrunner is created; the Fixer personally reviews every worker result.
- A one-worker wave (monowave) must always use `manual`.
- `automatic` adds an independent reviewer Netrunner after all workers are terminal. Use it only when the Architect explicitly requests automatic review or for a genuinely large parallel wave where the additional review materially helps.
- The Fixer still owns the final review and integration decision under both policies. `manual` does not disable review.
- When `automatic` is selected, the Fixer may set `review_backend`, `review_model`, and `review_reasoning`. MCP-bound defaults are `codex`, `gpt-5.6-luna`, and `high`. OpenCode Go DeepSeek V4 Pro is forbidden.
- CommandCode/OpenCode routes remain available as explicit alternatives; they are not the default worker route.

## Attempt And Instruction Semantics

The provider receives the task text when its attempt is launched. `update_task`
appends durable session metadata; it does not hot-patch a running provider
process. If the acceptance criteria or instructions change, explicitly stop and
requeue/relaunch the attempt (or use the governed repair path) before claiming
that the worker received the change.

Worker terminality is separate from reviewer terminality. Under `automatic`,
the runtime creates one linked reviewer only after all scheduled implementation
workers are terminal and failure reconciliation passes. The reviewer is an
independent session, not another implementation worker; a reviewer launch
failure is persisted for Fixer follow-up and never implies acceptance.

## Dirty Base Dispatch

Do not ask the Architect merely because the base worktree is dirty. Treat dirt
as dispatch work, not as a reason to stop.

If `create_netrunner_wave` refuses with a dirty-base error, immediately classify
the repo state and continue:

1. Inspect `git status --porcelain` and separate tracked changes from untracked
   local files.
2. Leave untracked local-only files alone by default. Do not stage secrets,
   `.env` files, local credentials, build outputs, or generated scratch files
   just to satisfy wave creation.
3. If tracked changes are accepted prior Fixer/Netrunner work, close them using
   the project's normal Git discipline: commit, integrate, stash, or remove
   generated artifacts. The target is a clean tracked root.
4. If tracked changes are unrelated but must be preserved, use a reversible
   named stash or the project's established preservation path. Record what was
   preserved in the handoff/report. Do not revert unrelated user work.
5. If a pending session depends on local secrets or local-only files that will
   not exist in isolated wave worktrees, do not launch it autonomously. Report
   the constraint or ask the Architect for an explicitly manual operator run.
6. If a session is `in_progress` but has no active worker process, recover it:
   set it back to `pending` when safe, or fork/create a replacement session.
   Do not let a zombie session block the whole wave.
7. Rebuild the wave candidate list from sessions that are still independent,
   pending, and wave-safe.
8. If any wave-safe sessions remain, create and launch the wave, including a one-worker wave when only one remains.
9. Stop only when no safe wave dispatch remains, and report the exact
    remaining blocker.

Never fall back to serial autonomous execution. Do not collapse the whole batch
because one slice needs local secrets, local files, or manual handling.

## When Not To Use

Do not autonomously launch the slice when:

- the implementation needs cross-file architectural decisions in one shared area
- workers may edit the same files or tests
- any worker needs to refactor shared contracts used by the others
- the project root is not a Git repo root
- that slice depends on local secrets or local-only files that should not be
  copied into isolated wave worktrees
- the task needs auto-merge, patch application, or acceptance automation

Re-slice it into a dependency DAG or request an explicitly manual operator session. Do not call a provider CLI or the retired serial launcher.

## Flow

1. Slice the work into independent tasks with explicit ownership, acceptance criteria, tests, and forbidden areas.
2. Create each worker session with `create_task`, using disjoint `declared_write_scope`.
3. Attach only relevant docs with `set_session_attached_docs`.
4. Assign only required MCP servers with `set_session_mcp_servers`.
5. If old candidate sessions are stale, zombie `in_progress`, secret-dependent,
   or no longer wave-safe, recover or exclude them before wave creation.
6. Create the wave with `create_netrunner_wave(session_ids=[...], review_policy="manual")`. The field may be omitted because `manual` is the system default. A monowave must not use `automatic`. Use `automatic` only under the **Review Policy** rules and pass reviewer model overrides when needed.
7. If wave creation reports dirty base, follow **Dirty Base Dispatch** and retry
   with the safe candidate subset.
8. Launch it with `launch_netrunner_wave(wave_id=...)`. Use the top-level backend/model/reasoning as defaults and `worker_configs=[{session_id, backend?, model?, reasoning?}, ...]` for per-worker overrides in one mixed-model wave.
9. After a successful launch, immediately report the launch to the Architect before the first `wait_for_netrunner_wave` call, unless the Architect explicitly requested launch-and-wait without an intermediate report. Include:
   - the execution/dependency sequence: parallel groups and any DAG ordering;
   - each Netrunner's responsibility;
   - the resolved backend, model, and reasoning for each Netrunner;
   - an estimated wait/execution time for each Netrunner;
   - an estimated wait/execution time for the wave as a whole;
   - the wave id, session ids, and each worker's initial `launched`, `running`, or dependency-pending status.
   Use persisted launch configuration and returned initial statuses, label estimates as approximate when needed, and make this report the first response content required by the active-wave status rule. This launch report does not replace later active-wave reconciliation or the final-response wait requirement.
10. Wait with `wait_for_netrunner_wave(wave_id, return_when="first_review_ready")`. Under `manual`, this never starts a reviewer Netrunner. Under explicitly selected `automatic`, all implementation workers becoming terminal may start the configured independent reviewer, but does not make that reviewer terminal or accepted.
11. The Fixer reviews every returned worker serially under both policies and,
    for `automatic`, separately verifies the linked reviewer session/process:
   - read the session report and proposals
   - inspect changed paths and the captured patch artifact
   - inspect the worker worktree when needed
   - verify the worker reported a commit SHA, a clean worktree, scope compliance, and required tests
   - verify the automatic reviewer is terminal and its report is available before using it as review evidence
   - reject for rework if task changes are uncommitted, the worktree is dirty, or changed paths exceed `declared_write_scope`
   - approve or reject doc proposals by Fixer judgment
   - complete the session or append precise rework
12. Continue waiting until all implementation workers are terminal; use
    `return_when="all_terminal"` for the final worker aggregate, but do not
    confuse that result with reviewer or acceptance completion.
13. After implementation review passes, reconcile governed repair before
    advancing. A first eligible single-worker/minority failure becomes
    `repair_required`; the Fixer authorizes the selected worker once with
    `authorize_netrunner_wave_repair`. A strict majority pauses the wave, and
    a later minority/tie after the repair is consumed becomes
    `manual_repair_required`.
14. For the explicit phase contract, call
    `transition_netrunner_wave_phase(target_phase="acceptance", ...)` only
    after all implementation workers are terminal, failure policy is `passed`,
    the implementation reviewer is completed and approved, and a distinct
    project-scoped pending acceptance session is supplied. Review that
    acceptance session separately, then transition to `completed` only after
    it is completed and reviewed; recursive-capable waves also require an
    exact committed `handoff_sha`.
15. Clean up only after review/acceptance decisions are made. Start
    conservative, then call `cleanup_netrunner_wave(remove_worktrees=true)`
    when it is safe.

### Acceptance Transition Runtime Blocker

The current `transition_netrunner_wave_phase` handler requires a completed
implementation reviewer session, while `manual` review intentionally creates
no reviewer Netrunner. Consequently manual waves—including mandatory-manual
monowaves—cannot use the acceptance transition as currently implemented. Keep
the blocker visible and do not fabricate a reviewer or claim that a Fixer-only
review satisfies the handler; this needs a runtime contract change before
manual-wave acceptance can be enabled.

## Droid Backend Launches

Use Droid waves only when the Architect explicitly asks for Droid workers, when
the wave is testing/fixing Droid behavior, or when project policy requires
Droid. Otherwise prefer the default Codex backend for wave work.

When launching a Droid wave, call `launch_netrunner_wave` with the public Droid
model alias:

```text
launch_netrunner_wave(
  wave_id=<wave id>,
  backend="droid",
  model="kimi-k2.6",
  reasoning="high"
)
```

Model aliases, vision/web-search MCP availability, external-session stickiness,
malformed-completion handling, and hang recovery follow the provider adapter canon.

## Safety Rules

- The Fixer remains the serialized reviewer and integration authority.
- Do not auto-merge, auto-apply worker patches, or let workers merge their own work. Review wave worker results serially.
- Do not stage, copy, or expose local secrets to make a wave work. Move those
  slices to an explicitly manual operator session or report them blocked.
- Do not let one unsafe slice block safe independent slices.
- Netrunners must not remove worktrees, rebase, merge, change wave state, or edit another worker's branch.
- Netrunners must commit all task changes on their own worker branch before `complete_task`; they must not merge or push.
- Treat timeout, stale epoch, frozen orchestration, missing process, or scope drift as review blockers.
- If the wave produces conflicting results, use the durable failure-policy and
  governed repair path above, or stop and report the conflict; do not launch an
  untracked serial autonomous worker.

## Reporting

**CRITICAL RULE FOR ACTIVE WAVES:** During active conversation with the Architect, the Fixer MUST report the status of every active wave at the beginning of each normal user-facing answer, launch report, review report, handoff, or final answer until all waves are closed and no wave-reports are pending. Do not repeat the full active-wave status block in every intermediate progress update while the Fixer is still working inside a single long turn; for those updates, mention only material changes or blockers.

**CRITICAL RULE BEFORE ANY FINAL ANSWER:** While any wave in the project is active (created/running/review_ready, workers not all terminal), the Fixer MUST NOT send a final/closing message to the Architect without first calling `wait_for_netrunner_wave` on every active wave in that same turn. Every call must use `timeout_seconds` of at least 600 seconds. Use 600 seconds for ordinary polling; raise it as needed up to 21600 seconds for an expected very heavy or long-running Netrunner, and never exceed 21600. The call reconciles stale worker/wave status even when it returns early. The final message must reflect the wave state returned by that call, not stale assumptions.

Report at least:

- wave id
- review policy and, for `automatic`, reviewer backend/model/reasoning
- worker session ids
- worker statuses
- changed paths and patch artifact paths
- tests/builds verified
- proposals approved/rejected
- cleanup status
- residual risks or blockers

Update the project handoff after any significant wave.
