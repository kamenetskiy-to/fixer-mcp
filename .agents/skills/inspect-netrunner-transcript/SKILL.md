---
name: inspect-netrunner-transcript
description: Use when a Fixer needs to recover or audit a confusing, broken, dead, or interrupted Netrunner by locating its local Codex/Droid JSONL transcript path through Fixer MCP without loading the transcript into context.
---

# Inspect Netrunner Transcript

Use this when a Netrunner state is unclear: broken thread, dead worker, interrupted connection, missing completion report, suspicious MCP/tool behavior, or a need to audit what the worker actually saw/did.

## Workflow

1. Call `fixer_mcp.get_netrunner_transcript_path`.
   - Fixer: pass the project-scoped `session_id`.
   - Overseer: pass both `project_id` and the project-scoped `session_id`.
2. Read the returned metadata: `backend`, `global_session_id`, `external_session_id`, `transcript_path`, `exists`, `readable`, `file_size_bytes`, `modified_at`, and `search_diagnostics`.
3. Inspect locally by path only. Do not ask MCP to return the full JSONL.

Useful local commands:

```bash
tail -n 120 '<transcript_path>'
rg -n 'complete_task|propose_doc_update|ERROR|panic|MCP|tool' '<transcript_path>'
jq -c 'select(.type == "response_item" or .type == "message")' '<transcript_path>' | tail -n 80
```

## Notes

- Codex and Droid path details are hidden by the MCP tool. Treat `transcript_path` as authoritative when present.
- If `exists` or `readable` is false, use `search_diagnostics` to decide whether the worker never launched, external id metadata is missing, or the local transcript store is unavailable.
- Quote only the small lines needed for evidence. Summarize the rest.
