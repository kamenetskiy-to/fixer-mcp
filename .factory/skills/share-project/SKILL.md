---
name: share-project
description: "Use this skill when a Fixer must move a project to another machine: export the project context (overview, handoff, doc tree, backlog, session index) into a portable package on the source machine and install it on the target machine with the Fixer MCP context-package tools."
---

# Share Project

Use this skill to move a Fixer MCP project's context between machines. The
portable package carries canonical control-plane state only; it is not a
product export and not a git migration.

## What The Package Carries

- manifest: schema_version, exported_at, project name, project root path
- project overview and project handoff content
- all project docs with title, content, doc_type, level, slug, path, status,
  and parent linkage via parent slug/path (never raw doc ids)
- backlog items (title, description, priority, status)
- a lightweight session index (session_id, status, task excerpt) for
  orientation only — it is NOT imported

## What The Package Does NOT Carry

- the git repo itself, branches, or git history — move the repo separately
  (clone, scp, rsync, USB)
- netrunner transcripts and progress logs — history stays behind per the
  documentation/history split canon
- secrets: the package is built only from control-plane DB state; never add
  .env or credential files to it
- local untracked files; machine-local paths are valid only on the source

## Export Side (source machine)

1. Authenticate as fixer bound to the source project (`assume_role`).
2. Optionally refresh canonical state first: `save-fixer-handoff`,
   `refresh-project-overview`.
3. Call `export_project_context_package`. Without a `path` argument the file
   lands at `artifacts/context_packages/<project-slug>-<timestamp>.json`
   under the project root; the final path is returned in the response.
4. Sanity-check the response: docs_count/backlog_count look right,
   has_overview/has_handoff match expectations.
5. Move the package file to the target machine (scp, USB, synced folder —
   operator's choice). Move the git repo separately.

## Import Side (target machine)

1. Clone/copy the repo to its new location first.
2. Register/open the project root so the Fixer binds to the right project
   (overseer `register_project`, or launch the Fixer with the project cwd),
   then authenticate as fixer.
3. Call `import_project_context_package` with the package `path`.
   - If the target project already has docs or a handoff, the import is
     refused; pass `force: true` only when you really intend to merge or
     overwrite (overview and handoff are replaced, docs are added with
     uniquified slug/path, backlog items are deduped by title).
4. Verify the response counts: docs_created, backlog_inserted,
   backlog_skipped, overview_set, handoff_set.
5. Spot-check with `get_project_docs` (tree levels and parent linkage) and
   `get_project_handoff`.
6. Continue with `init-fixer` as usual.

## Safety Notes

- schema_version mismatch is refused; do not hand-edit packages across
  versions.
- The package is a point-in-time snapshot. Export again if the source project
  moved on since the last export.
