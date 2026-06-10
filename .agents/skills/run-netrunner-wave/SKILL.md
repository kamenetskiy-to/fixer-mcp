---
name: run-netrunner-wave
description: "Use this skill when a project Fixer should dispatch multiple bounded Netrunner sessions in parallel through Fixer MCP Git worktrees, wait for review-ready workers, review each result serially, and clean up wave worktrees."
---

# Run Netrunner Wave

Use this skill only when a Fixer needs a parallel wave of independent Netrunner tasks.

Parallelism is for isolated work, not for making one tangled task faster. If scopes overlap, use `$run-one-netrunner-task`.

## Preconditions

- You are authenticated as project `fixer`.
- The current MCP tool list exposes `create_netrunner_wave`, `get_netrunner_wave`, `launch_netrunner_wave`, `wait_for_netrunner_wave`, and `cleanup_netrunner_wave`; restart the Fixer/MCP session if they are missing.
- The registered project root is a clean Git repository root. This is a hard precondition for wave creation.
- There is no active orchestration freeze or stale epoch blocker.
- The wave has at least two pending sessions.
- Each session has a narrow, disjoint `declared_write_scope`.
- No session owns broad scope such as `.`, whole repo, shared app root, shared migrations, or the same test/dev-server state unless the Fixer has a concrete isolation reason.

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
   not exist in isolated wave worktrees, remove that session from the wave plan
   and run it serially later.
6. If a session is `in_progress` but has no active worker process, recover it:
   set it back to `pending` when safe, or fork/create a replacement session.
   Do not let a zombie session block the whole wave.
7. Rebuild the wave candidate list from sessions that are still independent,
   pending, and wave-safe.
8. If at least two wave-safe sessions remain, create and launch the wave.
9. If exactly one wave-safe session remains, run it with `$run-one-netrunner-task`.
10. Stop only when no safe serial or wave dispatch remains, and report the exact
    remaining blocker.

Fallback to serial flow for unsafe slices; do not collapse the whole batch just
because one slice needs local secrets, local files, or serial handling.

## When Not To Use

Use one serial Netrunner for a specific slice when:

- the implementation needs cross-file architectural decisions in one shared area
- workers may edit the same files or tests
- any worker needs to refactor shared contracts used by the others
- the project root is not a Git repo root
- that slice depends on local secrets or local-only files that should not be
  copied into isolated wave worktrees
- the task needs auto-merge, patch application, or acceptance automation

## Flow

1. Slice the work into independent tasks with explicit ownership, acceptance criteria, tests, and forbidden areas.
2. Create each worker session with `create_task`, using disjoint `declared_write_scope`.
3. Attach only relevant docs with `set_session_attached_docs`.
4. Assign only required MCP servers with `set_session_mcp_servers`.
5. If old candidate sessions are stale, zombie `in_progress`, secret-dependent,
   or no longer wave-safe, recover or exclude them before wave creation.
6. Create the wave with `create_netrunner_wave(session_ids=[...])`.
7. If wave creation reports dirty base, follow **Dirty Base Dispatch** and retry
   with the safe candidate subset.
8. Launch it with `launch_netrunner_wave(wave_id=...)`, passing backend/model/reasoning only when needed.
9. Wait with `wait_for_netrunner_wave(wave_id, return_when="first_review_ready")`.
10. Review every returned worker serially:
   - read the session report and proposals
   - inspect changed paths and the captured patch artifact
   - inspect the worker worktree when needed
   - verify scope boundaries and required tests
   - approve or reject doc proposals by Fixer judgment
   - complete the session or append precise rework
11. Continue waiting until all workers are terminal; use `return_when="all_terminal"` when you need the final aggregate state.
12. Clean up only after review decisions are made. Start conservative, then call `cleanup_netrunner_wave(remove_worktrees=true)` when it is safe.

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
malformed-completion handling, and hang recovery follow the same Droid rules as
`$run-one-netrunner-task`; that skill owns the canonical Droid launch rules.

## Safety Rules

- The Fixer remains the serialized reviewer and integration authority.
- Do not auto-merge, auto-apply worker patches, or let workers merge their own work. Review wave worker results serially.
- Do not stage, copy, or expose local secrets to make a wave work. Move those
  slices to serial execution.
- Do not let one unsafe slice block safe independent slices.
- Netrunners must not remove worktrees, rebase, merge, change wave state, or edit another worker's branch.
- Treat timeout, stale epoch, frozen orchestration, missing process, or scope drift as review blockers.
- If the wave produces conflicting results, stop parallel handling and resolve serially.

## Reporting

Report at least:

- wave id
- worker session ids
- worker statuses
- changed paths and patch artifact paths
- tests/builds verified
- proposals approved/rejected
- cleanup status
- residual risks or blockers

Update the project handoff after any significant wave.
