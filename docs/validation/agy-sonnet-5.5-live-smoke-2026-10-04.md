# AGY Claude Sonnet 5.5 Live Launch Smoke — 2026-10-04

Second model of the governed agy smoke (Opus 5.5 evidence:
`agy-opus-5.5-live-smoke-2026-10-04.md`). This file is the only intended change.
No subagents, nested provider calls, product edits, or env/auth/argv inspection.

## Envelope and requested metadata

| Field | Value | Source |
| --- | --- | --- |
| Session / project | `714` / `2` | runtime prompt, `assume_role` |
| Wave / worker / branch | `847` / `1345` / `fixer/wave-847/session-714` | runtime prompt |
| `cli_backend` | `antigravity` | `get_session(714)` |
| `cli_model` | `Claude Sonnet 5.5` | `get_session(714)` |
| `cli_reasoning` | `high` | `get_session(714)` |
| Client selection | `Claude Sonnet 5.5 (High)` | client setting shown at launch |

## CLI evidence

- `agy --version` → `1.2.16`.
- `agy models` lists `claude-sonnet-5-5-high    Claude Sonnet 5.5 (High)`,
  matching the requested model + effort.

## Native Fixer MCP sequence (no wrapper needed)

1. `assume_role(netrunner, cwd)` → success (Project 2).
2. `checkout_task(714)` → success.
3. `log_netrunner_progress(started)` → success, `log_id=8775`.
4. `get_session(714)` → `in_progress`, metadata above; `get_attached_project_docs(714)` → none.

## Git diff check

Pre-existing tracked dirt before any action: ` D .agents/skills/figma-frontend-works/.DS_Store`.
It was preserved (not restored, not committed). The commit contains only this file.

## Verdict

Requested metadata and CLI selection are proven. Hidden provider inference
identity is **not** claimed.
