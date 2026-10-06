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

Worker policy requires at least one honest entry in each of `files_changed`,
`commands_run`, and `checks_run`. Do not submit a filler `propose_doc_update`:
proposals are required only when the work changes canonical documentation.
For artifact-only verification or rework, explicitly state that there is no
canonical documentation impact. `commit_sha` and `git_status` are worker-policy
evidence; record the commit and clean-tree evidence in `checks_run`.

Optional `cleanup_claims` must be an object, never an array or a free-form claim
of success:

```json
{"removed_paths": ["path/removed"], "expected_present_paths": ["path/present"]}
```

Omit it when there are no filesystem claims. Only list paths whose state you
actually checked. Legacy text reports remain historical evidence, not verified
cleanup; malformed structured claims must not be called verified.

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

1. Assess canonical documentation impact. If there is an actual change, submit
   and confirm an appropriately targeted `propose_doc_update`; otherwise record
   no documentation impact and do not manufacture a proposal. New-document
   creation and existing-document updates must have explicit intent/target.
2. Run `git status --porcelain` and inspect every tracked and untracked changed path.
3. Commit all task changes on the worker branch. Do not merge, rebase, or push.
4. Run `git status --porcelain` again. It must be empty before `complete_task`.
5. Record the resulting `git rev-parse HEAD` value for the report/handoff.
6. Build one finished JSON report before the first `complete_task` call. Put
   the commit and clean-tree evidence in `checks_run`; these are
   workflow requirements even though the MCP parser does not have dedicated
   fields for them.
7. Fill `files_changed` with repo-relative paths actually changed.
8. Fill `commands_run` with concrete commands executed.
9. Fill `checks_run` with verification outcomes.
10. Fill `blockers` with remaining blockers, or `[]`.
11. Optionally include `residual_risks` and `cleanup_claims` when they add signal.
12. Call `complete_task` once with the finished JSON. Successful completion
    moves the session to `review`; it does not mean that the Fixer accepted it.
13. If the runtime prompt requires it, call `wake_fixer_autonomous` after successful completion.

## Constraints

- Do not probe the schema by submitting partial reports.
- Do not call Fixer-only review tools.
- Do not claim checks that were not run.
- Do not call `complete_task` with a dirty worktree.
