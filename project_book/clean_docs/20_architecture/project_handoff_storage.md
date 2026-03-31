# Project Handoff Storage

## Purpose

Fixer MCP keeps one concise project handoff record per project in the control plane so a newly started Fixer can recover the latest operational state without scraping old chat history.

## Storage Model

- Table: `project_handoff`
- Scope: one row per `project_id`
- Fields:
  - `project_id`
  - `content`
  - `updated_at`

This is intentionally minimal. It stores only the latest handoff snapshot, not a workflow log.

## MCP Tools

- `set_project_handoff`
  - Fixer writes for the bound project
  - Overseer may write for any project by specifying `project_id`
- `get_project_handoff`
  - Fixer reads the bound project
  - Overseer may read any project by specifying `project_id`
- `clear_project_handoff`
  - Fixer clears the bound project
  - Overseer may clear any project by specifying `project_id`

## RBAC

- Fixer: read/write/clear for the current project
- Overseer: read/write/clear cross-project when `project_id` is explicit
- Netrunner: no normal access

## Content Guidelines

A good handoff is short and operational:

- current objective or active run mode
- current blocker or risk, if any
- next recommended action
- key files, tools, or sessions to inspect first
- any validation or restart note that matters immediately
