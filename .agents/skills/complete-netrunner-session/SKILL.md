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
- `blockers`

The runtime requires at least one entry in each of `files_changed`,
`commands_run`, and `checks_run`. It also requires at least one prior
`propose_doc_update` for the session, even when the implementation has no
canonical documentation impact. `commit_sha`, `git_status`, and `scope_check`
are worker-policy evidence, not fields currently validated or persisted by
`complete_task`; include their evidence in `checks_run` when useful.

Minimal valid shape:

```json
{
  "files_changed": ["path/to/file"],
  "commands_run": ["command you ran"],
  "checks_run": ["check result"],
  "blockers": []
}
```

## Closeout Procedure

1. Submit and confirm at least one `propose_doc_update` call has succeeded,
   regardless of whether the change has canonical documentation impact.
2. Run `git status --porcelain` and inspect every tracked and untracked changed path.
3. Confirm every changed path is inside the session's `declared_write_scope`. Resolve any scope drift before completion.
4. Commit all task changes on the worker branch. Do not merge, rebase, or push.
5. Run `git status --porcelain` again. It must be empty before `complete_task`.
6. Record the resulting `git rev-parse HEAD` value for the report/handoff.
7. Build one finished JSON report before the first `complete_task` call. Put
   the commit, clean-tree, and scope evidence in `checks_run`; these are
   workflow requirements even though the MCP parser does not have dedicated
   fields for them.
8. Fill `files_changed` with repo-relative paths actually changed.
9. Fill `commands_run` with concrete commands executed.
10. Fill `checks_run` with verification outcomes.
11. Fill `blockers` with remaining blockers, or `[]`.
12. Optionally include `residual_risks` and `cleanup_claims` when they add signal.
13. Call `complete_task` once with the finished JSON. Successful completion
    moves the session to `review`; it does not mean that the Fixer accepted it.
14. If the runtime prompt requires it, call `wake_fixer_autonomous` after successful completion.

## Constraints

- Do not probe the schema by submitting partial reports.
- Do not call Fixer-only review tools.
- Do not claim checks that were not run.
- Do not call `complete_task` with a dirty worktree or changed paths outside `declared_write_scope`.
