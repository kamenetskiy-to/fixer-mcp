# Explicit Netrunner Prompt Helpers

## Goal

Keep explicit Fixer MCP Netrunner launches stable by passing only the minimum context required to execute one Fixer MCP session correctly.

## Required Prompt Inputs

- project `cwd`
- preselected Fixer MCP session ID
- attached project docs only
- assigned MCP server names plus tiny how-to notes when useful
- execution approval state
- model and reasoning policy
- test/doc-proposal/completion obligations

## Minimal Worker Template

```text
Use the Fixer-selected explicit flow (`$autonomous-resolution`, `$autonomous-resolution-one-task`, or `$manual-resolution`) as already chosen by the Fixer.

You are a Netrunner working in `<cwd>`.
Preselected Fixer MCP session ID: `<session_id>`.
Load attached docs only.
Assigned MCP servers: `<mcp_names>`.
Execution is `<approved|not approved>`.

Model policy:
- default `gpt-5.4`
- default reasoning `medium`
- escalate only with concrete reason

Delivery contract:
- implement the scoped task
- update relevant automated tests
- fix older broken tests in scope when they block delivery
- for routine operator Telegram updates, use `fixer_mcp.send_operator_telegram_notification`
- submit at least one doc proposal
- complete the Fixer MCP session with a concise final report
```

## Tiny Helper Notes

- Do not pass unbounded architecture context when attached docs already cover the task.
- If execution is not pre-approved, stop after initialization and ask for `Go`.
- Do not treat `telegram_notify` as part of the normal Fixer/Netrunner MCP baseline for this ecosystem.
- For durable headless workers, keep the same session/doc/MCP payload but use the wire launcher under the explicit Fixer MCP flow.
