---
name: complete-netrunner-session
description: "Use this skill when a Netrunner is about to call Fixer MCP complete_task and needs the required structured final_report schema, including files changed, commands run, checks run, and blockers."
---

# Complete Netrunner Session

Use this skill immediately before calling `fixer_mcp.complete_task`.

## Required Final Report Shape

`complete_task` requires a non-empty JSON object with these top-level keys:

- `files_changed`
- `commands_run`
- `checks_run`
- `commit_sha`
- `git_status`
- `scope_check`
- `blockers`

Minimal valid shape:

```json
{
  "files_changed": ["path/to/file"],
  "commands_run": ["command you ran"],
  "checks_run": ["check result"],
  "commit_sha": "<worker branch HEAD SHA>",
  "git_status": "clean",
  "scope_check": "all changed paths are within declared_write_scope",
  "blockers": []
}
```

## Closeout Procedure

1. Confirm at least one `propose_doc_update` call has already succeeded.
2. Run `git status --porcelain` and inspect every tracked and untracked changed path.
3. Confirm every changed path is inside the session's `declared_write_scope`. Resolve any scope drift before completion.
4. Commit all task changes on the worker branch. Do not merge, rebase, or push.
5. Run `git status --porcelain` again. It must be empty before `complete_task`.
6. Record the resulting `git rev-parse HEAD` as `commit_sha`.
7. Build one finished JSON report before the first `complete_task` call.
8. Fill `files_changed` with repo-relative paths actually changed.
9. Fill `commands_run` with concrete commands executed.
10. Fill `checks_run` with verification outcomes.
11. Set `git_status` to `clean` and state the verified scope result in `scope_check`.
12. Fill `blockers` with remaining blockers, or `[]`.
13. Optionally include `residual_risks` and `cleanup_claims` when they add signal.
14. Call `complete_task` once with the finished JSON.
15. If the runtime prompt requires it, call `wake_fixer_autonomous` after successful completion.

## Constraints

- Do not probe the schema by submitting partial reports.
- Do not call Fixer-only review tools.
- Do not claim checks that were not run.
- Do not call `complete_task` with a dirty worktree or changed paths outside `declared_write_scope`.
