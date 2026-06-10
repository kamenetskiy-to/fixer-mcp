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

1. Confirm at least one `propose_doc_update` call has already succeeded.
2. Build one finished JSON report before the first `complete_task` call.
3. Fill `files_changed` with repo-relative paths or absolute global skill paths actually changed.
4. Fill `commands_run` with concrete commands executed.
5. Fill `checks_run` with verification outcomes.
6. Fill `blockers` with remaining blockers, or `[]`.
7. Optionally include `residual_risks` and `cleanup_claims` when they add signal.
8. Call `complete_task` once with the finished JSON.
9. If the runtime prompt requires it, call `wake_fixer_autonomous` after successful completion.

## Constraints

- Do not probe the schema by submitting partial reports.
- Do not call Fixer-only review tools.
- Do not claim checks that were not run.
