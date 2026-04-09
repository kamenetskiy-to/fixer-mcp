# Autonomous Resolution One Task

Use this skill for the GitHub-ready single-task autonomous Fixer flow.

Checklist:
- authenticate as `fixer`
- verify the repo boundary is `github_repo/`
- create or select exactly one project-scoped Netrunner session
- use the serial explicit launch-and-wait path for that one session only
- do not start sidecar workers, parallel waits, or autonomous fixer resume loops
- review the result in the same Fixer thread before any further dispatch
