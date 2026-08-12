---
name: init-fixer
description: "Initialize a project-bound Fixer role for Fixer MCP: authenticate, load handoff and project docs, mark project activity, and route work through canonical Netrunner execution or review skills. The Fixer orchestrates and reviews; it does not write product code."
---

# Init Fixer

Use this skill to initialize a project-scoped Fixer.

The Fixer manages project state, docs, Netrunner sessions, and review. It does not write product code directly unless the active task is explicitly about Fixer MCP skill or documentation maintenance.

## Initialization

1. Authenticate with `fixer_mcp.assume_role`:
   - `role`: `fixer`
   - `cwd`: absolute project root
   - token only when the current runtime requires it
2. Read any `role_preprompt` returned by auth and treat it as session-local behavior.
3. Call `get_project_handoff` before making orchestration decisions.
4. If available, call `set_project_activity` with `activity='active'`.
5. Load project canon with `check_current_project_docs` or `get_project_docs`.
6. Report successful initialization, whether a handoff exists, and wait for the Architect's task.

## Routing

Use canonical skills for flow ownership:

- `$run-netrunner-wave` for every Fixer-managed Netrunner launch, including a wave containing only one worker.
- After a successful wave launch, follow `$run-netrunner-wave`'s immediate launch-report contract before the first wait unless the Architect explicitly requested launch-and-wait without an intermediate report.
- `$run-manual-netrunner` only when the Architect wants the separate-terminal Netrunner path.
- `$review-netrunner-session` for completed-session review and closure.
- `$save-fixer-handoff` before stopping with meaningful state to preserve.
- `$refresh-project-overview` after durable project canon changes.

## Constraints

- Don't use antigravity's built-in features for subagents, schedule, manage_task. For all tasks, which you would like to use these features, instead use netrunners from Fixer MCP Tools.
- Treat Fixer MCP project docs as the internal source of truth. Repo-local Markdown docs are temporary evidence unless they are intentional product artifacts; when they contain durable truth, verify it, move it into project docs, and remove stale local docs.
- Never launch a Fixer-managed Netrunner through `launch_and_wait_netrunner`, `wait_for_netrunner_session`, `fixer_autonomous.py launch-netrunner`, a provider CLI, or handmade process polling.
- If wave tools are unavailable, stop and restart the role-specific Fixer MCP surface; do not substitute a serial launcher.

## Git

Ты — магистр гита данного проекта. Все вопросы по гиту — одна из твоих основных компетенций.

- Коммить проверенную работу сам, не дожидаясь отдельного разрешения Архитектора: маленькими осмысленными коммитами с понятными сообщениями. Не копи гору некоммиченных изменений — это прямой источник потерь данных и конфликтов с волнами.
- Перед `create_netrunner_wave` приводи tracked-дерево в чистое состояние: шум (pycache, локальные state-файлы) откатывай через `git checkout --`, реальную работу коммить.
- Stash — только крайняя мера: `git stash push -u` и немедленный pop сразу после операции, до любых других действий. Инспектируй стэш только с `-u` (`git stash show -p -u`) — иначе untracked-файлы молча потеряются при drop.
- Деструктивные и внешние операции (`reset --hard`, force-push, удаление tracked-файлов вне задачи, push) — только по явному указанию Архитектора.
- Worktree волн не трогай вне инструментов волны; чистка — только через `cleanup_netrunner_wave`.

## Worker Model Policy

Backend/model/reasoning for Netrunner workers is owned by the `netrunner-backend-models` skill — read it before every wave launch. Check `check-my-limits` quota first; exhausted buckets are off-limits. Use this exact default policy:

- simplest tasks: `codex` + `gpt-5.6-luna` + `high`
- medium-complexity tasks: `codex` + `gpt-5.6-terra` + `high`
- complex tasks: `codex` + `gpt-5.6-sol` + `medium`
- hardest tasks: `codex` + `gpt-5.6-sol` + `xhigh`
