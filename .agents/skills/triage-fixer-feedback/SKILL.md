---
name: triage-fixer-feedback
description: "Triage the current cross-project Fixer MCP feedback backlog and prepare a source-attributed fix map for the Architect; exclude legacy and already-fixed reports."
---

# Triage Fixer Feedback

Use this skill when the Architect asks what Fixer MCP feedback is still actionable and how to split the fixes. This is a read-only triage and planning workflow. Do not implement fixes, create a Netrunner wave, merge code, or create backlog items unless the Architect separately asks for that next step.

## Required initialization

1. Authenticate as the project-bound `fixer` with `assume_role` if the role is
   not already authenticated. For the Fixer MCP project, use the absolute
   `self_orchestration` root.
2. Read the current project handoff and compact overview, mark the project
   active, and load the relevant canonical project docs.
3. Confirm that the bound project is the Fixer MCP project Fixer. Only that
   project may perform the global feedback search. If global access is not
   available, report the limitation instead of silently searching only the
   bound project's feedback.

## Collect the complete feedback set

Call `list_fixer_mcp_feedback` without `project_id` and paginate until
`has_more` is false. Do not use a date cutoff, inactive-project filter, or
"latest N" shortcut: old-looking feedback can remain current and recent
feedback can already be fixed.

Resolve every `project_id` to the registry's canonical project name and cwd.
Prefer an MCP project-registry tool when the authenticated surface provides
one; otherwise use the permitted read-only SQLite `project` table. Never infer
the project name from the feedback prose alone.

For every report retain:

- feedback id;
- source project name and project id;
- exact `created_at` date/time;
- feedback type;
- the concise problem statement and relevant reproduction evidence.

## Decide whether a report is current

The feedback table has no authoritative resolution field. Therefore neither
the report's age, nor the source project's inactivity, proves that a report is
legacy or open.

Classify each normalized issue using evidence:

- `closed`: the reported behavior is demonstrably fixed in the current
  integrated code/config/skill set, with a matching commit, test, accepted
  session, or verified runtime behavior;
- `actionable`: the reported behavior is still reproducible or the current
  implementation still visibly contains the defect, and there is no accepted
  fix that supersedes it;
- `unverified`: the source project or relevant runtime cannot be inspected,
  or the evidence is insufficient to decide. Do not put unverified issues into
  the actionable fix map.

For shared Fixer MCP defects, inspect the current `self_orchestration` source,
tests, integrated `main`, and relevant MCP tool schemas. For project-specific
defects, inspect the registered source project's current Git state and its
later Fixer sessions/commits when accessible. A historical branch or an
unmerged worker commit is not a closed fix.

Handle corrections before classification:

- a correction that withdraws a report removes it from the actionable set;
- a correction that narrows or amends a report supersedes the original text;
- duplicate reports become one issue, but all supporting source reports remain
  listed with their project and date.

Never claim "already fixed" merely because a similarly named change exists.
Record the concrete evidence used for a closed decision. Never claim
"current" merely because no resolution metadata exists.

## Build the Architect report

Report only the actionable fix map in the main section. Keep closed and
unverified material out of the implementation list; at most provide aggregate
counts and a one-line note about why they were excluded.

Use this compact row shape for every actionable issue:

| Problem | Source feedback | Current evidence | Proposed fix / wave slice | Priority |
|---|---|---|---|---|
| concise normalized problem | `#ID — Project Name (project_id) — YYYY-MM-DD` for **every** duplicate/source report | direct code, test, commit, or repro evidence | bounded implementation responsibility and dependency | P0/P1/P2 |

The source column is mandatory. If several projects reported the same issue,
list every project and every feedback date, not only the newest report. Never
write a row such as "other Fixers" or a bare feedback id.

After the table, give a short wave proposal:

1. independent parallel slices with narrow, disjoint write scopes;
2. dependency-gated or sequential slices where files/contracts overlap;
3. acceptance checks for each slice;
4. unresolved risks and any slice that needs a stronger planning worker.

Do not include legacy fixes in this proposal. Do not launch workers from this
skill. If the Architect later authorizes implementation, route that separate
step through `$run-netrunner-wave` and the current `$netrunner-backend-models`
policy.

## Safety and audit discipline

- Use Fixer MCP first for feedback, handoff, docs, sessions, and waves.
- Use filesystem/Git/SQLite only as read-only evidence gathering.
- Do not mutate another project's database, source, docs, backlog, or runtime.
- Redact credentials, tokens, and sensitive command-line values from findings.
- Keep the final report short: counts, actionable rows with source attribution,
  then the proposed wave breakdown.
