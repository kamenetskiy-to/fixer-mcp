---
name: review-netrunner-session
description: "Use this skill when a Fixer must review a completed Netrunner session, inspect work and tests, approve or reject doc proposals, then complete the session or send precise rework."
---

# Review Netrunner Session

Use this skill as the Fixer review and acceptance flow for any completed Netrunner session.

It applies to manual, autonomous, and Fixer MCP-native worker launches.

## Review Flow

1. Authenticate as `fixer`.
2. Load current internal Fixer MCP docs.
3. Read the target session with `get_session`.
4. Read append-only worker history with `view_netrunner_logs` when available.
5. Read pending doc proposals with `review_doc_proposals`.
6. Validate the actual work, not only the worker report or logs.
7. For implementation sessions, verify:
   - required code changes were made
   - relevant automated tests were added, updated, or removed
   - older broken tests in scope were fixed when they blocked delivery
8. Treat every doc proposal as a hypothesis requiring Fixer judgment.
9. If valid, approve the relevant proposals and set the session to `completed`.
10. If incomplete, reject or leave proposals pending as appropriate, append precise rework instructions, and move the session back to `pending` or `in_progress`.

## Acceptance Cleanup

After accepting a Netrunner, make the repository state explicit.

For implementation work, actively drive toward a clean tracked worktree: commit,
integrate, stash, or remove generated artifacts according to the project's
established Git policy. Treat lingering tracked dirt after acceptance as
unfinished review work unless it is deliberately preserved and documented.

Do not revert unrelated user changes. If unrelated pre-existing dirt remains, preserve it and document it in the handoff or report.

When accepted work is preparing future parallel waves, a clean tracked worktree
is required before wave creation. If the accepted worker leaves follow-up work,
close it serially or create a replacement session before trying to launch the
wave.

## Acceptance Standards

- No blind approval.
- No code-only acceptance when tests were required.
- No canon merge without explicit review; Netrunner logs are evidence, not canonical docs.
- Prefer concrete rejection reasons over vague dissatisfaction.
