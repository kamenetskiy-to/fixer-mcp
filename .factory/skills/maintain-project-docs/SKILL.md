---
name: maintain-project-docs
description: "Bootstrap or repair Fixer MCP project documentation into a current canonical 0..3-level tree. Use when project docs are missing, chaotic, stale, mixed with session history, or need migration to the canonical documentation/history split."
---

# Maintain Project Docs

Use this skill when a Fixer or documentation worker needs to create, audit, or repair project documentation for Fixer MCP.

## Core Rule

Keep history separate from canon.

- Netrunner logs are append-only evidence about how work unfolded.
- Project docs are the current canonical truth of the project.
- Doc proposals update canonical docs, not historical logs.
- Repo-local Markdown documentation is temporary evidence unless it is an intentional product artifact such as a public README.
- Internal source of truth belongs in Fixer MCP project docs; verified local-doc facts should be moved into project docs, and stale local docs should be deleted.

## Workflow

1. Read current project docs and recent attached docs.
2. Inspect local documentation-like Markdown files such as `docs/**/*.md`, local READMEs, migration plans, audits, research, and implementation notes.
3. Treat every old project-doc and local-doc claim as a hypothesis until verified against current code, tests, tools, skills, runtime behavior, or another live source.
4. Inspect Netrunner logs only as evidence for review and reconstruction.
5. Classify existing docs as: current canon, historical note, research, implementation summary, product/public, audit, or temporary note.
6. Choose a small tree:
   - level 0: project or major-area overview;
   - level 1: stable major domains such as product, backend/control plane, runtime/workers, frontend/operator UI, skills/workflow, ops/validation, data/storage, or integrations;
   - level 2: concrete subsystem or workflow;
   - level 3: narrow verified contracts for concrete tools, schemas, UI flows, modules, or operational invariants.
7. Preserve current truth; rewrite old session summaries into durable rules only when they are still valid.
8. Create or update canonical project docs through Fixer MCP project-doc tools or Netrunner doc proposals, depending on role.
9. Delete historical/audit/research/session local docs after verified durable facts have been moved into canon.

## Tree Rules

- Level 0 has no parent.
- Level 1..3 requires a parent.
- Child level should be parent level + 1.
- Use stable, human-readable `slug` and `path`.
- Avoid depth when a flatter document is easier to maintain.
- Level 3 is for narrow, stable, verified contracts. Do not create level 3 from unverified implementation notes just to preserve old material.
- Treat terminal launchers, CLI menus, alias scripts, desktop consoles, and operator command surfaces as frontend/operator UI when they are the user's actual entry point.

## Output Shape

When proposing a documentation repair, include:

- the intended root/section tree;
- which old docs remain canonical;
- which old docs are historical only;
- which docs should be merged, archived, or rewritten;
- any Netrunner logs used as evidence.

Keep the result practical and short enough for the next Fixer to act on.
