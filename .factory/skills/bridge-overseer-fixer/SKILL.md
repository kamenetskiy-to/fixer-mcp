---
name: bridge-overseer-fixer
description: "Use this skill when a project Fixer is launched or resumed through the durable Overseer/Fixer chat bridge and must read bridge messages, update run state, route work, and answer through the bridge."
---

# Bridge Overseer Fixer

Use this skill only as a project-bound Fixer invoked by the global Overseer.

## Required Flow

1. Authenticate as `fixer` if needed.
2. Read `get_overseer_fixer_messages`.
3. Read `get_overseer_fixer_run_state` and `get_project_handoff` when useful.
4. Do not ask the human Architect questions inside this Fixer thread.
5. If the Overseer request is unclear or blocked, answer with `append_overseer_fixer_message` using `sender_role='fixer'`.
6. Keep `set_overseer_fixer_run_state` current while work is active and when it completes or blocks.
7. Route clear work through canonical Fixer flows:
   - `$run-netrunner-wave` for every bounded implementation task, including a one-worker wave
   - `$review-netrunner-session` for completed-session review
   - never launch or resume Project Hands; the Architect exclusively opens Hands
     through the fixer TUI or Fixer Studio
   - use Hands tools only to inspect state/history, submit instructions, cancel
     stale work, or perform governed review
   - `$hands-netrunner` applies only inside an Architect-opened Hands client
     or an explicitly preselected compatibility envelope, never as bridge dispatch
8. Send the compact result through `append_overseer_fixer_message`.

## Constraints

- Use the durable chat bridge for Overseer-facing communication.
- Do not write project code directly unless the active task is Fixer skill or documentation maintenance.
