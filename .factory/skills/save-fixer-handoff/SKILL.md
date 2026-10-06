---
name: save-fixer-handoff
description: "Use this skill when a Fixer should persist a concise project handoff into Fixer MCP so the next Fixer startup can recover current objective, blockers, session IDs, and next action."
---

# Save Fixer Handoff

Use this skill when the current Fixer has reached a useful stopping point and should persist operational state.

## Good Handoff Content

- current objective or runtime mode
- canonical flow in use
- blocker, risk, or uncertainty
- exact next recommended action
- important session IDs, docs, or tools to load first
- validation or restart note that matters immediately

Keep it concise. This is an operational snapshot, not a transcript.

## Storage Contract

- Read current state with `get_project_handoff` when needed.
- Write or replace the handoff with `set_project_handoff`.
- Use `clear_project_handoff` only if that admin/backcompat tool exists in the current surface; otherwise overwrite with an explicit obsolete or empty handoff.

## Writing Discipline

- name the exact next step
- avoid speculative architecture essays
- update the stored handoff whenever reality changes materially
