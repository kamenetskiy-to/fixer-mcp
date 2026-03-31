# ТЗ: Мультибэкенд для Fixer MCP

**Дата:** 2026-03-31
**Статус:** Draft
**Автор:** Factory Droid

---

## 1. Контекст и цель

Fixer MCP — Go-сервер MCP (stdio, `modelcontextprotocol/go-sdk v1.3.1`), реализующий оркестрацию AI-сессий поверх Codex CLI. Архитектура включает ролевую модель (fixer / netrunner / overseer), управление проектами, документооборот с proposal-ревью, автономный запуск netrunner-агентов, fork/repair логику и Telegram-нотификации.

**Цель:** Добавить поддержку множества CLI-бэкендов (codex, droid, и потенциально других в будущем), позволив:
- Выбирать бэкенд при запуске сессии
- Выбирать модель внутри бэкенда
- Сохранять привязку сессии к бэкенду (запрет свитча после старта)
- Продолжать (resume) сессию на том же бэкенде

---

## 2. Текущее состояние

### 2.1 Python-сторона (client_wires)

Launch-логика уже использует adapter pattern:

```python
# fixer_wire.py::_load_available_servers()
return available_servers, CONFIG_ENV_VARS, CODEX_CLI_ADAPTER, _ensure_sqlite_scaffold
```

Интерфейс адаптера:

| Метод | Назначение |
|-------|-----------|
| `adapter.command` | Имя CLI-команды (`"codex"`) |
| `adapter.build_llm_args(llm_selection)` | Аргументы модели |
| `adapter.build_execution_args(execution_prefs)` | Аргументы выполнения (sandbox, auto-approve) |
| `adapter.build_mcp_flags(selected, available)` | Флаги MCP-серверов |
| `adapter.build_prompt_args(prompt)` | Формат промпта |
| `adapter.prepare_env(env, llm_selection)` | Подготовка окружения |

Проблема: `CODEX_CLI_ADAPTER` захардкожен из `codex_pro_app.main`.

### 2.2 Go-сторона (fixer_mcp/main.go)

`launchExplicitNetrunnerWithMetadata()` (строка ~5044):
- Вызывает `resolveExplicitLauncherScript()` → `client_wires/fixer_autonomous.py`
- Запускает `python3 fixer_autonomous.py launch-netrunner --cwd ... --session-id ...`
- Ждёт появления `codex_session_id` в таблице `session_codex_link`
- Все waitFor/waitSnapshot функции содержат поле `codexSessionID`

### 2.3 DB Schema (fixer.db)

Ключевые таблицы:

| Таблица | Проблема |
|---------|----------|
| `session` | Нет поля для привязки к бэкенду |
| `session_codex_link(session_id, codex_session_id)` | Захардкожена на codex |

---

## 3. Архитектура решения

```
┌─────────────────────────────────────────────────────────┐
│                    Fixer MCP Server                      │
│                                                         │
│  LaunchExplicitNetrunner                                │
│       │                                                 │
│       ├─ backend=codex → fixer_autonomous.py            │
│       │                      CODX_ADAPTER → codex ...   │
│       │                                                 │
│       ├─ backend=droid → fixer_autonomous.py            │
│       │                      DROID_ADAPTER → droid exec │
│       │                                                 │
│       └─ backend=<future>                               │
│                                                         │
│  DB: session.backend = "codex" | "droid" | ...          │
│      session_external_link(session_id, backend, ext_id) │
└─────────────────────────────────────────────────────────┘
```

### 3.1 Абстракция CLI-бэкенда

Каждый бэкенд определяется:

```python
class CliBackend(Protocol):
    name: str                           # "codex", "droid"
    command: str                        # "codex", "droid"
    
    def build_launch_args(self, config: LaunchConfig) -> list[str]: ...
    def build_resume_args(self, config: ResumeConfig, external_id: str) -> list[str]: ...
    def build_mcp_flags(self, selected: dict, available: dict) -> list[str]: ...
    def prepare_env(self, env: dict, config: LaunchConfig) -> dict: ...
    def external_id_poll_query(self, session_id: int) -> str:
        # SQL-column для polling readiness
        ...
```

### 3.2 Поддерживаемые бэкенды

| Бэкенд | Команда | Запуск | Resume | MCP-конфиг |
|--------|---------|--------|--------|------------|
| codex | `codex` | `codex [args] [prompt]` | `codex resume <session-id>` | `codex.mcpServers` |
| droid | `droid` | `droid exec --auto high [args] [prompt]` | `droid exec --session-id <id> --auto high [prompt]` | `.factory/settings.json` / `mcp.json` |

---

## 4. Изменения по слоям

### 4.1 База данных

#### 4.1.1 Новая колонка в `session`

```sql
ALTER TABLE session ADD COLUMN cli_backend TEXT NOT NULL DEFAULT 'codex';
-- CHECK (cli_backend IN ('codex', 'droid'))
```

#### 4.1.2 Переименование / генерализация session_codex_link

```sql
-- Вариант A: переименовать
ALTER TABLE session_codex_link RENAME TO session_external_link;
ALTER TABLE session_external_link ADD COLUMN backend TEXT NOT NULL DEFAULT 'codex';
ALTER TABLE session_external_link RENAME COLUMN codex_session_id TO external_session_id;
-- Уникальность: (session_id, backend)

-- Вариант B (мягче): оставить codex_link, добавить session_external_link параллельно
CREATE TABLE session_external_link (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id INTEGER NOT NULL,
    backend TEXT NOT NULL,
    external_session_id TEXT NOT NULL,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(session_id) REFERENCES session(id) ON DELETE CASCADE,
    UNIQUE(session_id, backend)
);
```

**Рекомендация:** Вариант B — безопаснее, codex_link оставить для обратной совместимости, новый `session_external_link` для универсальности. Go-код читает из `session_external_link` с fallback на `session_codex_link WHERE backend = 'codex'`.

#### 4.1.3 Миграция

```sql
-- Перенести существующие codex-ссылки в новую таблицу
INSERT INTO session_external_link(session_id, backend, external_session_id)
SELECT session_id, 'codex', codex_session_id FROM session_codex_link;

-- Проставить backend для существующих сессий
UPDATE session SET cli_backend = 'codex' WHERE cli_backend = 'codex' OR cli_backend = '';
```

### 4.2 Python (client_wires/fixer_wire.py и fixer_autonomous.py)

#### 4.2.1 Registry адаптеров

```python
# client_wires/backends/
# ├── __init__.py
# ├── codex_adapter.py    # обёртка над существующим CODEX_CLI_ADAPTER
# └── droid_adapter.py    # новая реализация
```

`_load_available_servers()` → `_load_available_servers_with_backend(cwd, backend="codex")`:

```python
BACKEND_ADAPTERS = {
    "codex": CodexCliAdapter,
    "droid": DroidCliAdapter,
}

def _load_available_servers(cwd: Path, backend: str = "codex") -> tuple[...]:
    adapter_cls = BACKEND_ADAPTERS[backend]
    adapter = adapter_cls(...)
    return available_servers, config_env_vars, adapter, ensure_sqlite_scaffold
```

#### 4.2.2 DroidCliAdapter

```python
class DroidCliAdapter:
    name = "droid"
    command = "droid"

    def build_launch_args(self, config: LaunchConfig) -> list[str]:
        args = ["exec", "--auto", "high"]  # netrunner = полная автономия
        args.extend(["-m", config.model])  # например, "gpt-5.3-codex"
        if config.reasoning:
            args.extend(["-r", config.reasoning])  # off|low|medium|high
        return args

    def build_resume_args(self, config: ResumeConfig, external_id: str) -> list[str]:
        args = ["exec", "--session-id", external_id, "--auto", "high"]
        if config.model:
            args.extend(["-m", config.model])
        return args

    def build_mcp_flags(self, selected: dict, available: dict) -> list[str]:
        # Droid MCP привязывается через .factory/settings.json или mcp_config.json
        # На уровне exec --cwd уже будет нужный проект с нужным mcp_config
        return []  # MCP handled via cwd + local config

    def prepare_env(self, env: dict, config: LaunchConfig) -> dict:
        env["FACTORY_API_KEY"] = config.factory_api_key  # из .env или config
        return env

    def external_id_poll_query(self):
        # Для droid session_id — это то, что фиксится при launch
        return "external_session_id"
```

#### 4.2.3 UI выбора бэкенда

При `launch_fixer_new` и `launch_netrunner`:

```
Выбери бэкенд:
  1) codex (default)
  2) droid

Выбери модель:
  [список зависит от бэкенда]
  codex: gpt-4.5, gpt-o3, gpt-o4-mini
  droid: gpt-5.3-codex, claude-sonnet-4.5, glm-5, ...
```

Выбор сохраняется в `session.cli_backend` в `fixer.db`.

#### 4.2.4 fixer_autonomous.py

`launch-netrunner` subcommand принимает новый флаг:

```bash
fixer_autonomous.py launch-netrunner \
    --cwd /path \
    --session-id 5 \
    --backend droid \           # NEW, default codex
    --model gpt-5.3-codex \     # NEW
    --reasoning medium          # NEW
```

### 4.3 Go MCP Server (main.go)

#### 4.3.1 LaunchExplicitNetrunner

Input: добавить `Backend` и `Model`:

```go
type LaunchExplicitNetrunnerInput struct {
    SessionId              int    `json:"session_id"`
    FixerSessionId         string `json:"fixer_session_id,omitempty"`
    WriteScopeOverrideReason string `json:"write_scope_override_reason,omitempty"`
    SessionReuseOverrideReason string `json:"session_reuse_override_reason,omitempty"`
    Backend                string `json:"backend,omitempty"`        // "codex" | "droid"
    Model                  string `json:"model,omitempty"`          // модель
    Reasoning              string `json:"reasoning,omitempty"`      // off|low|medium|high
}
```

При запуске передавать `--backend` в launcher script:

```go
if input.Backend != "" {
    commandArgs = append(commandArgs, "--backend", input.Backend)
}
if input.Model != "" {
    commandArgs = append(commandArgs, "--model", input.Model)
}
```

#### 4.3.2 External ID polling

`fetchSessionCodexSessionID()` → `fetchSessionExternalID(sessionID, backend)`:

```go
func fetchSessionExternalID(sessionID int, backend string) (string, error) {
    var externalID string
    err := db.QueryRow(
        `SELECT external_session_id
         FROM session_external_link
         WHERE session_id = ? AND backend = ?`,
        sessionID, backend,
    ).Scan(&externalID)
    // fallback на session_codex_link если backend="codex"
    ...
}
```

#### 4.3.3 Блокировка свитча бэкенда

В `checkout_task` и повторных `launch_explicit_netrunner`:

```go
// Если сессия уже имеет cli_backend != "", и попытка запустить на другом
if sessionState.CliBackend != "" && sessionState.CliBackend != input.Backend {
    return nil, fmt.Errorf(
        "session %d is bound to backend %q, cannot switch to %q",
        sessionID, sessionState.CliBackend, input.Backend,
    )
}
```

### 4.4 JSON API / Output

#### 4.4.1 LaunchExplicitNetrunnerOutput

```go
type ExplicitNetrunnerLaunchMetadata struct {
    SessionId              int      `json:"session_id"`
    ProjectCwd             string   `json:"project_cwd"`
    LauncherScript         string   `json:"launcher_script"`
    Backend                string   `json:"backend"`                 // NEW
    ExternalSessionId      string   `json:"external_session_id"`     // NEW (было CodexSessionId)
    Model                  string   `json:"model,omitempty"`         // NEW
    SpawnedBackground      bool     `json:"spawned_background"`
    OrchestrationEpoch     int      `json:"orchestration_epoch"`
    DeclaredWriteScope     []string `json:"declared_write_scope"`
    ParallelWaveID         string   `json:"parallel_wave_id"`
    ConcurrentLaunch       bool     `json:"concurrent_launch"`
    WriteScopeOverrideUsed bool     `json:"write_scope_override_used"`
}
```

`CodexSessionId` во всех output-структурах заменить на `ExternalSessionId`. Старое поле оставить как deprecated alias.

---

## 5. MCP Discovery для Droid

### 5.1 Проблема

Сейчас `discover_project_mcp_servers()` / `discover_self_mcp_servers()` читают `codex_pro_app`-специфичные конфигурационные файлы. Droid использует другие форматы.

### 5.2 Решение

Для Droid MCP-серверы привязываются к проекту через `.factory/settings.json`:

```json
{
    "mcpServers": {
        "fixer_mcp": {
            "command": "/path/to/fixer_mcp",
            "args": []
        }
    }
}
```

При запуске netrunner с `--cwd <project>` Droid автоматически читает `.factory/settings.json` из этой директории. Поэтому:

- **Для codex:** launcher script собирает MCP из `mcp_config.json` и передаёт через флаги
- **Для droid:** MCP резолвится автоматически через `--cwd`. Launcher только убеждается, что `.factory/settings.json` содержит нужный `fixer_mcp` binding

**Задача:** В `DroidCliAdapter.build_mcp_flags()` добавить опциональную генерацию/валидацию `.factory/settings.json` с правильным bind на `fixer_mcp`.

---

## 6. План работ

### Фаза 1: Инфраструктура (1-2 дня)

- [ ] **DB:** Добавить `session.cli_backend` column
- [ ] **DB:** Создать `session_external_link` таблицу
- [ ] **DB:** Миграция данных из `session_codex_link`
- [ ] **Python:** Создать `client_wires/backends/` пакет
- [ ] **Python:** Рефакторинг `CODEX_CLI_ADAPTER` → `CodexCliAdapter` с полным интерфейсом
- [ ] **Python:** Реализация `DroidCliAdapter`

### Фаза 2: UI и выбор (1-2 дня)

- [ ] **Python:** UI выбора бэкенда и модели в `launch_fixer_new`
- [ ] **Python:** UI выбора бэкенда и модели в `launch_netrunner`
- [ ] **Python:** Флаг `--backend` и `--model` для `fixer_autonomous.py launch-netrunner`
- [ ] **Python:** Запись `cli_backend` в DB при создании сессии

### Фаза 3: Go MCP Server (2-3 дня)

- [ ] **Go:** Обновить `LaunchExplicitNetrunnerInput` + передача `--backend`, `--model`
- [ ] **Go:** `fetchSessionExternalID(sessionID, backend)` с fallback на codex_link
- [ ] **Go:** Блокировка свитча бэкенда в `checkout_task` и `launch_explicit_netrunner`
- [ ] **Go:** Обновить все структуры: `CodexSessionId` → `ExternalSessionId`
- [ ] **Go:** Обновить `WaitForNetrunnerSession(s)` — poll с правильным backend
- [ ] **Go:** Обновить `LaunchAndWaitNetrunner`

### Фаза 4: Droid MCP Discovery (1-2 дня)

- [ ] **Python:** `DroidCliAdapter` — валидация `.factory/settings.json`
- [ ] **Python/Python:** Авто-генерация `fixer_mcp` binding при необходимости
- [ ] **Integration test:** Запуск netrunner через droid, проверка что fixer_mcp доступен

### Фаза 5: Тесты и полировка (2-3 дня)

- [ ] Обновить `test_fixer_wire.py` — тесты нового adapter
- [ ] Обновить `test_fixer_autonomous.py` — тесты launch с `--backend droid`
- [ ] Обновить `main_test.go` — тесты Go MCP с обоими бэкендами
- [ ] Ручное тестирование: launch+wait для codex и droid
- [ ] Resume-сценарий для droid: `droid exec --session-id`

---

## 7. Риски и mitigation

| Риск | Вероятность | Влияние | Mitigation |
|------|------------|---------|------------|
| Droid MCP config (.factory/settings.json) не совпадает с expected форматом | Средняя | Средняя | Делать валидацию + autogeneration |
| Session continuity для droid работает иначе (stream-jsonrpc vs codex resume) | Средняя | Средняя | Изучить `droid exec --session-id` поведение заранее |
| Разные timeout-модели между бэкендами | Низкая | Низкая | Backend-specific timeout конфиг |
| Обратная совместимость — старые сессии без cli_backend | Низкая | Низкая | DEFAULT 'codex', migration script |

---

## 8. Заметки

### Droid Exec ключевые флаги

| Флаг | Значение |
|------|----------|
| `--auto high` | Полная автономия (netrunner) |
| `-m <model>` | Модель (gpt-5.3-codex, claude-sonnet-4.5, glm-5...) |
| `-r <level>` | Reasoning (off, low, medium, high) |
| `--session-id <id>` | Resume сессии |
| `--cwd <path>` | Рабочая директория |
| `--output-format json` | Структурированный вывод |

### Официальные примеры Droid

- [droid-chat](https://github.com/Factory-AI/examples/tree/main/droid-chat) — SSE + exec
- [Droid Exec docs](https://docs.factory.ai/cli/droid-exec/overview.md) — stream-jsonrpc для multi-turn
