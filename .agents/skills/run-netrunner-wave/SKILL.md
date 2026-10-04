---
name: run-netrunner-wave
description: "Use this skill when a project Fixer should dispatch multiple bounded Netrunner sessions in parallel through Fixer MCP Git worktrees, wait for review-ready workers, review each result serially, and clean up wave worktrees."
---

# Run Netrunner Wave

Use this skill for every Fixer-managed Netrunner launch. A wave may contain one worker, multiple independent workers, or dependency-gated workers sequenced through an explicit DAG. Use a dependency DAG when worker outputs must be merged in order.

## Preconditions

- You are authenticated as project `fixer`.
- The current MCP tool list exposes `create_netrunner_wave`, `get_netrunner_wave`, `launch_netrunner_wave`, `wait_for_netrunner_wave`, and `cleanup_netrunner_wave`; restart the Fixer/MCP session if they are missing.
- The registered project root must be a Git repository. If it is not a Git repository, initialize it first (`git init && git add . && git commit -m "Initial commit"`). Absence of a Git repository is NEVER a reason to refuse a wave.
- There is no active orchestration freeze or stale epoch blocker.
- The wave has at least one pending session.

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
2. Create each worker session with `create_task`.
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
   - verify the worker reported a commit SHA, a clean worktree, and required tests
   - verify the automatic reviewer is terminal and its report is available before using it as review evidence
   - reject for rework if task changes are uncommitted or the worktree is dirty
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
    `transition_netrunner_wave_phase(target_phase="acceptance", review_approved=true)`
    only after all implementation workers are terminal and the failure policy
    is `passed`. On `manual` waves the Fixer's attestation is the review: no
    reviewer Netrunner and no acceptance session are required (an optional
    `acceptance_session_id` is validated when supplied — pending and distinct
    from workers/reviewer). On `automatic` waves the implementation reviewer
    must be completed and a distinct pending acceptance session supplied;
    review that acceptance session separately. Then transition to `completed`
    with `review_approved=true` (a supplied acceptance session must be
    completed first; recursive-capable waves also require an exact committed
    `handoff_sha`).
15. Clean up only after review/acceptance decisions are made. Start
    conservative, then call `cleanup_netrunner_wave(remove_worktrees=true)`
    when it is safe.

### Acceptance And Completion Contract (manual review)

Manual waves close on the Fixer's review attestation — there is no dead-end and
nothing to fabricate. `transition_netrunner_wave_phase` accepts
`target_phase="acceptance"` with `review_approved=true` and without a reviewer
Netrunner or acceptance session. The canonical order for a reviewed manual
wave:

1. all implementation workers terminal, failure policy `passed`;
2. `transition_netrunner_wave_phase(target_phase="acceptance",
   review_approved=true)`;
3. close reviewed worker sessions with `set_session_status(status="completed")`
   — allowed from the acceptance phase on (the wave keeps session ownership
   during implementation and refuses with an actionable message);
4. `transition_netrunner_wave_phase(target_phase="completed",
   review_approved=true)` (`handoff_sha` for recursive-capable waves; a
   supplied acceptance session must be completed first).

Automatic waves keep the stricter contract: a completed implementation
reviewer and a pending acceptance session are required when entering
acceptance.

## Rejected Review: Rework Or Closed-Rejected

A rejected review never needs a fake PASS (feedback 95). Two governed roads:

1. **Rework**: `set_session_status(status="pending", reason="rejected: …")` on
   the worker session (from `review`). It increments `rework_count` and
   requeues the worker as `retry_wait`, so the next `wait_for_netrunner_wave`
   relaunches the same worktree with the appended instructions. Append the
   precise rework notes with `update_task` first.
2. **Closed-rejected**: `transition_netrunner_wave_phase(target_phase=
   "completed", review_approved=true, review_outcome="rejected")` closes the
   wave from any phase once all workers are terminal: nothing is attested as
   passed and the verdict is labelled on the wave. No write-path reservation
   machinery is involved.

Everything else on a wave-linked session stays wave-owned while the wave runs.

## System1 First-Stage Review (system1-trial-0.1)

`system1_check` is REQUIRED on new waves: `create_netrunner_wave` and
`launch_netrunner_wave` fail closed with an actionable error when a new wave
would run without it. Waves created before this layer (no persisted packet)
stay on the manual review path. Full contract and default prompts:
`docs/plans/system1-review-contract.md`.

Packet (attached per wave):

```json
{
  "criteria_prompt": "c1 (weight 0.6, hard): ...\nc2 (weight 0.4, soft): ...",
  "hard_ids": ["c1"],
  "threshold": 0.75,
  "max_checks": 3,
  "contract_version": "system1-trial-0.1"
}
```

Write one criterion per line (the JSON `\n` above denotes a newline).
The reader is a one-shot `cmd` call to `xiaomi/mimo-v2.6-flash`; only its final
factual overview is passed to `typesafe/jev` as typed `noul` questions. Both
inputs travel on stdin, not argv. Jev is not a chat judge; weights, threshold
and hard gates are computed locally by the wave engine.

Decision rule (0.75/0.5): a content check passes iff `overall_probability >= 0.75`
AND every hard criterion in `hard_ids` has `probability >= 0.5`. A valid hard
criterion below 0.5 forces a content fail. Missing/invalid answers, malformed
JSON, reader/CLI/API errors and oversized requests are infrastructure failures,
never a content verdict or a pass.

Per worker, at most `max_checks` System1 checks run (default 3, clamped 1..3):

- **Pass**: record `system1_passed`; the worker stays `review_ready` for the
  normal manual acceptance close. System1 never auto-completes the wave.
- **Fail with budget remaining (checks 1–2)**: the SAME worker continues — no
  forked session. The check results and remaining gaps are appended to the
  task (`update_task`), `set_session_status(review -> pending)` requeues the
  worker to `retry_wait` with a cleared `worker_process_id`, and the wait loop
  relaunches it in its recorded worktree.
- **Third failed content check**: no requeue. The worker is marked
  `system1_escalated`; `next_action="fixer_second_stage_review"` asks the Fixer
  to accept or reject through the existing manual wave close.
- **Stronger but different**: if strict criteria fail but the independent
  `stronger_but_different` probability reaches the threshold, immediately
  escalate to the Fixer instead of requeuing the worker onto the specification.
  This is not a PASS; the Fixer decides whether the alternative is better.
- **Infrastructure failure**: append an `infra_failed` diagnostic row/artifact;
  do not consume the content-check budget, move the session or requeue the
  implementation. The worker stays `review_ready`. At most three such attempts
  run before escalation to Fixer; never invent synthetic content probabilities.

Each check stores its packet and verdict as a wave artifact and a short row
readable via `get_system1_reviews`.

System1 is deliberately independent of worker ownership and admission
bookkeeping — do not remove or weaken the System1 layer in unrelated changes.

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
- Treat timeout, stale epoch, frozen orchestration, or missing process as review blockers.
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
