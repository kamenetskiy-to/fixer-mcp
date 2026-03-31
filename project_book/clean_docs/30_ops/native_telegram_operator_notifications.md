# Native Telegram Operator Notifications

## Goal

Send routine operator-facing Telegram updates directly from `fixer_mcp` without depending on a separate `telegram_notify` MCP server.

## Runtime Configuration

- `FIXER_MCP_TELEGRAM_BOT_TOKEN`: Telegram bot token used by `fixer_mcp`
- `FIXER_MCP_TELEGRAM_CHAT_ID`: target operator chat or channel id
- `FIXER_MCP_TELEGRAM_API_BASE_URL`: optional override for tests or proxies; defaults to `https://api.telegram.org`

If the required env vars are missing, `send_operator_telegram_notification` fails fast with a deterministic config error.

## Tool Contract

Tool: `send_operator_telegram_notification`

Required fields:

- `source`: кто или что отправляет уведомление
- `status`: короткий статус на русском

Optional fields:

- `summary`: однострочная сводка
- `session_id`: контекст сессии; для Fixer/Netrunner это project-scoped id
- `run_state`: контекст прогона (`running`, `blocked`, `awaiting_review`, `completed`, ...)
- `details`: компактная дополнительная строка

Project identity is always resolved from Fixer MCP context:

- Fixer/Netrunner use the currently bound project
- Overseer must pass `project_id`

## Rendered Message Shape

Messages stay plain-text and compact:

```text
Fixer MCP: уведомление оператору
Проект: Fixer MCP (#2)
Источник: Нетрaннер / headless
Статус: Блокер
Сессия: 5
Прогон: blocked
Сводка: Не удалось завершить preflight
Детали: Figma Bridge не отвечает после reconnect.
```

Only non-empty optional fields are rendered.

## Policy

- Use this native path for routine Architect/operator notifications inside Fixer workflows.
- Do not treat `telegram_notify` as part of the normal curated/default MCP surface for Fixer/Netrunner runs.
- Keep messages concise and operational; this tool is for status signaling, not long reports.
