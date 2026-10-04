# AGY Claude Opus 5.5 Live Launch Smoke — 2026-10-04

Governed live smoke authorized by the Architect. Purpose: prove that the new
`antigravity` (agy) route with Claude Opus 5.5 / high reasoning reaches a
working Netrunner that can authenticate, check out its project-bound session,
log progress, and complete normal Fixer MCP execution.

Scope: this file is the only intended repository change. No product code,
refactors, subagents, schedules, or nested provider calls were used.

## Execution envelope

| Field | Value | Source |
| --- | --- | --- |
| Session ID | `713` | runtime prompt (preselected compatibility session) |
| Project ID | `2` | `get_session` / `assume_role` response |
| Wave / worker | wave `846`, worker `1344` | runtime prompt |
| Branch | `fixer/wave-846/session-713` | runtime prompt, `git status --branch` |
| Worker cwd | `.codex/netrunner_worktrees/wave-846/session-713` | runtime prompt |
| Base commit | `b623294` (equal to `main` at launch) | `git log --oneline -n 3` |

## Requested model metadata

These values describe what was *requested / configured*, not what the provider
actually executed internally.

| Field | Value | Source |
| --- | --- | --- |
| `cli_backend` | `antigravity` | `fixer_mcp.get_session(713)` |
| `cli_model` | `Claude Opus 5.5` | `fixer_mcp.get_session(713)` |
| `cli_reasoning` | `high` | `fixer_mcp.get_session(713)` |
| Client model selection | `Claude Opus 5.5 (High)` | Antigravity client setting surfaced to the agent at launch |

## Provider-internal inference identity

Not independently verified. The agent has no read-only, non-secret channel that
exposes the upstream inference model ID actually served for this conversation,
and env/auth files and process argv were intentionally not inspected (task
constraint). No provider-internal identity is claimed here; only the requested
metadata above and the CLI inventory below are evidenced.

## CLI model inventory (`agy models`, read-only)

`agy --version` → `1.2.16`. `agy models` exited `0` and listed:

```text
gemini-3.8-flash-high      Gemini 3.8 Flash (High)
gemini-3.8-flash-medium    Gemini 3.8 Flash (Medium)
gemini-3.8-flash-low       Gemini 3.8 Flash (Low)
gemini-3.7-flash-high      Gemini 3.7 Flash (High)
gemini-3.7-flash-medium    Gemini 3.7 Flash (Medium)
gemini-3.7-flash-low       Gemini 3.7 Flash (Low)
gemini-3.6-flash-high      Gemini 3.6 Flash (High)
gemini-3.6-flash-medium    Gemini 3.6 Flash (Medium)
gemini-3.6-flash-low       Gemini 3.6 Flash (Low)
gemini-3.1-pro-high        Gemini 3.1 Pro (High)
gemini-3.1-pro-low         Gemini 3.1 Pro (Low)
claude-opus-5-5-low        Claude Opus 5.5 (Low)
claude-opus-5-5-medium     Claude Opus 5.5 (Medium)
claude-opus-5-5-high       Claude Opus 5.5 (High)
claude-sonnet-5-5-low      Claude Sonnet 5.5 (Low)
claude-sonnet-5-5-medium   Claude Sonnet 5.5 (Medium)
claude-sonnet-5-5-high     Claude Sonnet 5.5 (High)
gpt-oss-120b-medium        GPT-OSS 120B (Medium)
```

The requested route (`Claude Opus 5.5` + `high`) corresponds to the inventory
identifier `claude-opus-5-5-high` / label `Claude Opus 5.5 (High)`.

## Fixer MCP sequence (native MCP, no wrapper)

Native MCP calls worked directly; the `/tmp/fixer_netrunner_call.py` fallback
was not needed and not used.

1. `assume_role(role="netrunner", cwd=<worker cwd>)` → `success`,
   "Authenticated as Netrunner for Project 2".
2. `checkout_task(session_id=713)` → `success`.
3. `log_netrunner_progress(log_type="started", log_text=...)` → `success`,
   `log_id=8771`, `session_id=713`.
4. `get_session(713)` → status `in_progress`, backend/model/reasoning as above.
5. `get_attached_project_docs(713)` → no attached docs.
6. Progress log, `propose_doc_update`, commit, `completed` log, and
   `complete_task` follow after this file is written (recorded in the session
   report).

## Git diff check

- Pre-existing uncommitted modifications to 8 files under `.agents/skills/`
  were present in the worktree at launch (before any action by this worker).
  They are not part of this smoke and were neither edited nor committed by it.
- This worker's change set is exactly
  `docs/validation/agy-opus-5.5-live-smoke-2026-10-04.md`, committed alone
  (`git diff --cached --name-only` checked before commit).

## Verdict

The agy route with requested `Claude Opus 5.5` / `high` launched a working
Netrunner that authenticated, checked out session 713, logged progress, and
executed normal MCP calls successfully. The provider-internal inference model
identity remains unverified by design.
