---
name: init-unattached-fixer
description: "Initialize an Unattached Fixer launched through Fixer MCP: authenticate against the internal scratch workspace, load durable ad-hoc context, mark the synthetic project active, and route research/automation work through normal Netrunner tasks."
---

# Init Unattached Fixer

Use this skill when the launcher starts `Unattached Fixer`.

This is still a normal locked `fixer` role internally, but it is bound to an internal scratch workspace instead of the operator's current product repository.

## Initialization

1. Read the scratch workspace path from the launch prompt.
2. Authenticate with `fixer_mcp.assume_role`:
   - `role`: `fixer`
   - `cwd`: absolute scratch workspace path
   - do not provide a token when the MCP server is role-locked by the launcher
3. If auth returns `invalid token`, stop and report a launcher/runtime bug. Do not search local files for secrets.
4. Read any `role_preprompt` returned by auth and treat it as session-local behavior.
5. Call `get_project_handoff`.
6. Call `set_project_activity` with `activity='active'` if available.
7. Load scratch-project canon with `check_current_project_docs` or `get_project_docs`.
8. Report successful initialization, scratch workspace path, whether a handoff exists, and wait for the Architect's ad-hoc task.

## Routing

- Use `$run-netrunner-wave` for every bounded research or automation worker, including a one-worker wave.
- Assign task-specific MCP servers before launching workers.
- Keep outputs under the scratch workspace unless the Architect names another destination.
- Use `$review-netrunner-session` for completed-session review and closure.
- Use `$save-fixer-handoff` before stopping with meaningful ad-hoc state to preserve.

## Constraints

- Delegate implementation, research, and automation execution to Netrunners.
- When you discover a clear Fixer MCP runtime/tooling bug, immediately call `submit_fixer_mcp_feedback` with a concise repro and impact, even if you can continue through a workaround.
- Prepare a clean Git base when workers are needed, then launch them only through waves. Never substitute a serial autonomous launcher.

## Worker Model Policy

Backend/model/reasoning for Netrunner workers is owned by the `netrunner-backend-models` skill. Read it before launching workers; it contains the current quota gate, temporary provider overrides, and model-specific reasoning constraints.
