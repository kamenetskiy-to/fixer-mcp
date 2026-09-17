---
name: export-project-doc-bundle
description: Package a minimal, selected set of canonical project documentation into a portable ZIP for a handoff or focused research request.
---

# Export Project Documentation Bundle

Use this skill when a Fixer needs to hand off selected canonical project
documentation to another project, Fixer, Netrunner, or external collaborator.

1. Call `check_current_project_docs` and use its compact project-scoped IDs,
   paths, levels, summaries, and statuses.
2. Select the smallest coherent set: include directly relevant documents,
   parent contracts when they define required vocabulary or invariants, and
   narrow level-3 contracts when they materially constrain the request.
   Exclude stale, archived, history, and unrelated context unless requested.
3. If summaries are insufficient, call `get_project_docs` and inspect only the
   candidate documents.
4. Record the selected compact `project_doc_ids` and the rationale, including
   intentionally omitted context.
5. Call `export_project_doc_bundle` with exactly those IDs. Relative `path`
   values resolve under the bound project root; the tool creates a ZIP with
   `manifest.json` and the selected Markdown files.
6. Report the final archive path, exported IDs/count, and omissions.

This is an exact-selection export. Do not call `export_project_context_package`
for a selective request, and do not claim that the resulting archive imports
itself into another project.
