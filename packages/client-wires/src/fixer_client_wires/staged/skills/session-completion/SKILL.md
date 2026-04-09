# Session Completion

Use this skill when a Netrunner is about to call `fixer_mcp.complete_task`.

Checklist:
- submit at least one `propose_doc_update` before completion
- prepare one structured JSON `final_report` before the first `complete_task` call
- include these required top-level keys: `files_changed`, `commands_run`, `checks_run`, `blockers`
- use `[]` for `blockers` when nothing remains blocked
- do not probe the schema by sending partial reports repeatedly
