---
name: fixer-repo-cleanup
description: "Prepare a Fixer project for new work when the Architect explicitly asks to clean stale Hands instructions, sessions, worktrees, documentation metadata, and the cleanup runbook."
---

# Fixer Repo Cleanup

Use this skill only when the Architect explicitly asks to clean the Fixer
project space before new Fixer work. The goal is a clean, predictable control
plane; do not use it for product-code cleanup or broad repository refactors.

Run the cleanup in this order:

1. Hands → idle
2. pending sessions → terminal
3. review sessions → completed
4. terminal wave worktrees → cleaned
5. project documentation → checked and corrected
6. cleanup runbook → recorded

## Hands

Use:

- `get_hands_state` to inspect `derived_state` and `queued_count`;
- `list_hands_instructions` to inventory instruction states;
- `cancel_hands_instruction(instruction_id, idempotency_key, reason)` for
  stale instructions in `queued`, `waiting_for_lease`, `starting`, or
  `running`.

Do not touch terminal instructions (`cancelled`, `completed`, `failed`,
or `abandoned`). Preserve the intent of cancelled work by moving its useful
scope into backlog when appropriate. Use a stable idempotency key such as
`fixer-clean-<date>-<sequence>`.

## Sessions

Inventory with `list_project_sessions` (or the project-specific pending-task
tool when available). The valid cleanup path for stale sessions is:

`pending → in_progress → review → completed`

There is no direct pending-to-completed transition. Set a concrete reason such
as `stale cleanup` on every transition. Move stale `in_progress` sessions
to `completed` only when their work is no longer live and the cleanup reason
is recorded.

For old `review` sessions, use `set_session_status(..., completed,
"stale cleanup")`. This closes finalized history; it does not delete it.

Never silently reject or erase an active Architect intention. If the intention
still matters, create or update a backlog item before closing the stale entity.

## Worktrees

Discover wave state with `list_netrunner_waves` and `get_netrunner_wave`.
Clean terminal waves only through:

`cleanup_netrunner_wave(wave_id, remove_worktrees=true, prune=true)`

If a terminal failed or review-ready wave refuses cleanup because its worktree
contains modified or untracked files, retry with `force=true` after verifying
that the wave is terminal and no live work depends on it.

Do not run manual `git worktree remove` for Fixer-managed wave worktrees.
Do not touch `.codex/hands_worktrees/*` through this skill; Hands cleanup has
its own governed path.

After MCP cleanup, inspect empty wrapper directories under
`.codex/netrunner_worktrees` and remove only confirmed empty leftovers. Keep
the project root and active Hands worktrees.

## Documentation

Use `check_current_project_docs` or `get_project_docs` as the source of
truth, then use `update_project_doc`, `add_project_doc`, or
`delete_project_doc` only for verified corrections.

Check for:

- duplicated content under unrelated document titles;
- missing `slug` or `path`;
- filesystem paths accidentally stored as document paths;
- a flat level-0 tree where the real parent structure is known.

Do not invent missing document content. If the source content cannot be
recovered from code or history, record the defect in backlog instead of filling
it with a placeholder. Safe metadata-only repairs may be applied after
verifying the target document and its intended parent.

## Runbook

Record the cleanup in a concise project-local runbook or canonical project
document. Include:

- cleanup timestamp and project;
- Hands result;
- session transitions and reasons;
- cleaned wave IDs and any forced cleanup;
- documentation defects found or corrected;
- backlog items created for unresolved intent or missing content.

## Completion checklist

- `get_hands_state` reports `derived_state=idle` and `queued_count=0`;
- no stale `pending`, `in_progress`, or `review` sessions remain;
- terminal wave worktrees are cleaned through the MCP cleanup tool;
- Hands worktrees were not touched by this flow;
- documentation defects are corrected or recorded in backlog;
- the cleanup runbook is saved.
