---
name: run-one-netrunner-task
description: "Use this skill when a Fixer should dispatch exactly one bounded Netrunner task through Fixer MCP, wait for that worker in the same Fixer thread, then review and accept or reject the result."
---

# Run One Netrunner Task

Use this skill for one bounded Fixer-managed implementation slice.

## Flow

1. Authenticate as `fixer` and load project canon through Fixer MCP docs.
2. Snapshot and understand any pre-existing dirty worktree state before launch. Serial runs may start from dirty repositories when project context requires it.
3. Define one concrete Netrunner task with scope, acceptance criteria, attached docs, MCP assignments, and automated-test obligations.
4. Create the task with `create_task`.
5. Attach docs with `set_session_attached_docs`.
6. Assign MCP servers with `set_session_mcp_servers` when needed.
7. Launch and wait with `launch_and_wait_netrunner`.
8. Review the completed session immediately using `$review-netrunner-session` standards.
9. Accept and close it, or reject it with precise rework instructions and one replacement path.
10. After accepting implementation work, make the resulting repo state explicit
    and drive accepted changes to a clean tracked state using the project's
    normal Git policy. This is especially important before any future
    `$run-netrunner-wave` attempt.

## Dirty Repo Discipline

- Do not use serial dirty-state tolerance as permission to leave accepted work
  half-integrated.
- Preserve unrelated user changes; do not revert them.
- If a serial task needs local secrets or local-only files, keep it serial and
  do not promote that slice into a future wave.
- If a serial launch is interrupted and the session has no active worker
  process, recover the session state before planning later waves.

Worker model policy is owned by the role init skills (`$init-fixer`, `$init-unattached-fixer`).

## Droid Backend Launches

Use Droid only when the Architect explicitly asks for a Droid worker, when the
task is about Droid behavior, or when project policy requires Droid. Otherwise
keep the default Codex launch path.

When launching one Netrunner under Droid, call:

```text
launch_and_wait_netrunner(
  session_id=<project-scoped session id>,
  backend="droid",
  model="kimi-k2.6",
  reasoning="high"
)
```

Rules:

- Default to `model="kimi-k2.6"`.
- Use `model="glm-5.1"` only when GLM is explicitly requested or the task is
  a GLM comparison/debug slice.
- Pass only the public Droid aliases `kimi-k2.6` or `glm-5.1`.
- Do not pass Factory/Droid internal ids such as
  `custom:Kimi-K2.6-[Kimi]-0` or `custom:GLM-5.1-[Z.AI]-0`; those are adapter
  internals and must only appear
  in the Droid command/settings materialization layer.
- If a legacy `custom:` id is already stored on a pending session, relaunch with
  the public alias so the launch config normalizes back to the public value.
- Do not change backend/model/reasoning after a session has an external Droid
  session id; fork or create a replacement task instead.
- If a Droid worker stays `pending` with an empty log or never reaches
  checkout, stop the stale worker process and create/fork a replacement rather
  than repeatedly reusing the same hung process.
- If a Droid session reaches `review` without a structured final report or doc
  proposal, treat it as malformed worker completion and send rework or fork a
  repair session; do not accept it as review-ready work.
- Droid workers have the Z.AI Vision MCP server available by default for image
  analysis, OCR, technical diagrams, UI diff checks, and video analysis. For
  vision work, put images or videos in the local workspace and refer to them by
  path/name in the prompt. Direct image paste may not trigger MCP in non-Claude
  clients.
- Droid workers also have the Z.AI Web Search MCP server `web-search-prime`
  available by default. Use its `webSearchPrime` tool deliberately for current
  web lookup when assigned task-specific search MCPs are not the better fit;
  Z.AI plan quotas apply.

## Constraints

- One Netrunner only; parallel work belongs to `$run-netrunner-wave`.
- Use `wait_for_netrunner_session` only for the same launched worker.
