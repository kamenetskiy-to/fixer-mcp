---
name: refresh-project-overview
description: "Use this skill when a project-bound Fixer needs to refresh the compact Fixer MCP project overview used by Overseer global project visibility and routing."
---

# Refresh Project Overview

Use this skill only as a project-bound Fixer.

## Refresh Flow

1. Ensure you are authenticated as `fixer` for the current project.
2. Read internal project canon:
   - prefer `check_current_project_docs`
   - use `get_project_docs` only when full content is needed
3. Write an English overview of 1000 characters or less.
4. Include durable facts useful to Overseer routing: purpose, architecture, active operational shape.
5. Exclude transient chat state, speculation, and task noise.
6. Store it with `set_project_overview`.
7. If available and appropriate, call `set_project_activity` with `activity='active'`.

## Output

Report that the overview was refreshed and mention whether the project was marked active.
