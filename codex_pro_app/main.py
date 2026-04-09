from __future__ import annotations

import fnmatch
import json
import os
import shutil
import subprocess
import sys
import textwrap
from abc import ABC, abstractmethod
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Dict, Iterable, List, Optional, Tuple

try:
    import tomllib  # Python 3.11+
except ModuleNotFoundError:  # pragma: no cover
    import tomli as tomllib  # type: ignore

# ---------------------------------------------------------------------------
# Proxy env helpers (mirrors client_wires/launch_env.py)
# ---------------------------------------------------------------------------
_PROXY_ALIAS_GROUPS: Tuple[Tuple[str, ...], ...] = (
    ("ALL_PROXY", "all_proxy"),
    ("HTTP_PROXY", "http_proxy"),
    ("HTTPS_PROXY", "https_proxy"),
    ("NO_PROXY", "no_proxy"),
)

_PROXY_STATE_REL_PATH = Path(".codex") / "runtime_proxy_env.json"


def _capture_proxy_env(environ: Optional[Dict[str, str]] = None) -> Dict[str, str]:
    source = environ or {}
    payload: Dict[str, str] = {}
    for aliases in _PROXY_ALIAS_GROUPS:
        value = ""
        for name in aliases:
            candidate = str(source.get(name, "")).strip()
            if candidate:
                value = candidate
                break
        if not value:
            continue
        for name in aliases:
            payload[name] = value
    return payload


def _load_proxy_state(cwd: Path) -> Dict[str, str]:
    path = cwd / _PROXY_STATE_REL_PATH
    if not path.is_file():
        return {}
    payload = json.loads(path.read_text(encoding="utf-8"))
    if isinstance(payload, dict):
        stored = payload.get("proxy_env", payload)
        if isinstance(stored, dict):
            return _capture_proxy_env({str(k): str(v) for k, v in stored.items()})
    return {}


def _save_proxy_state(cwd: Path, proxy_env: Dict[str, str]) -> None:
    path = cwd / _PROXY_STATE_REL_PATH
    path.parent.mkdir(parents=True, exist_ok=True)
    normalized = _capture_proxy_env(proxy_env)
    import time as _time
    path.write_text(
        json.dumps({"proxy_env": normalized, "updated_at_epoch": int(_time.time())}, indent=2, sort_keys=True) + "\n",
        encoding="utf-8",
    )


def _resolve_proxy_env(cwd: Path) -> Dict[str, str]:
    current = _capture_proxy_env(dict(os.environ))
    if current:
        _save_proxy_state(cwd, current)
        return current
    return _load_proxy_state(cwd)


def _apply_proxy_env(target: Dict[str, str], proxy_env: Dict[str, str]) -> None:
    for key, value in _capture_proxy_env(proxy_env).items():
        target[key] = value


from .config_loader import (
    ConfigError,
    attach_preprompts_from_command_paths,
    discover_project_mcp_servers,
    discover_self_mcp_servers,
    fetch_mcp_servers,
    get_config_path,
    load_config,
    merge_mcp_servers,
)
from .file_search import find_textual_files
from .prompts import (
    YOLO_PROMPT,
    build_documentation_prompt,
    build_file_prompt,
    build_mcp_prompt,
    compose_prompt,
    load_global_prompts,
    read_global_prompt,
)
from .ui import multi_select, multi_select_items, multi_select_tree, Option, single_select_items


YOLO_KEY = "YOLO mode"
SANDBOX_FLAG_VALUE = "dangerous_sandbox"
AUTO_APPROVE_FLAG_VALUE = "auto_approve"
NO_MCP_PREPROMPTS_FLAG_VALUE = "no_mcp_preprompts"

PRESET_NONE = "custom"
PRESET_FAST_RESEARCH = "fast_research"
PRESET_CREATE_PROJECT_MCP = "create_project_mcp"
PRESET_SIMPLE_CHAT = "simple_chat"
PRESET_CODING = "coding"
PRESET_FIXER = "fixer"
PRESET_NETRUNNER = "netrunner"
PRESET_OVERSEER = "overseer"

CODING_PRESET_ALLOWED_SERVERS = {
    "postgres",
    "clickhouse",
    "sqlite",
    "google_search",
    "tavily",
    "figma",
    "global_image_assets",
    "firebase_mcp",
    "dart_flutter",
    "laravel_mcp_companion",
    "gopls",
    "serverpod",
    "nodejs_docs",
    "shadcn",
    "telegram_notify",
}

ORCHESTRATOR_BRIEF_CANDIDATES = (
    Path("project_book/mcp_orchestrator_brief.md"),
    Path("mcp_orchestrator_brief.md"),
)

SERVER_DISPLAY_NAMES = {
    "postgres": "Postgres DB",
    "clickhouse": "ClickHouse DB",
    "sqlite": "SQLite DB",
    "exa": "Exa Semantic Search",
    "tavily": "Tavily Search",
    "librex": "LibreX Search",
    "searCrawl": "searCrawl Search",
    "deep_research": "Deep Research",
    "dart_flutter": "Dart & Flutter Docs",
    "gopls": "Go Docs (gopls)",
    "serverpod": "Serverpod Docs Q&A",
    "plane": "Plane Project Tools",
    "google_docs": "Google Docs",
    "google_sheets": "Google Sheets",
    "pandas_mcp": "Pandas Data Tools",
    "dart_project_tools": "Dart Project Tools",
    "telegram_notify": "Telegram Notifications",
    "laravel_mcp_companion": "Laravel Docs (MCP Companion)",
    "nodejs_docs": "Node.js API Docs",
    "shadcn": "shadcn Registry Components",
    "framer": "Framer MCP",
    "figma": "Figma MCP",
    "schemacrawler": "SchemaCrawler AI",
    "firebase_mcp": "Firebase MCP",
    "global_image_assets": "Global Image Assets (MCP)",
}

CONFIG_PATTERNS = {
    "postgres": "*postgresMCP*.toml",
    "clickhouse": "*clickhouseMCP*.toml",
    "sqlite": "*sqliteMCP*.toml",
    "firebase_mcp": "*firebaseMCP*.toml",
    "figma": "*figmaMCP*.toml",
}

CONFIG_ENV_VARS = {
    "postgres": "POSTGRES_MCP_CONFIG_PATH",
    "clickhouse": "CLICKHOUSE_MCP_CONFIG_PATH",
    "sqlite": "SQLITE_MCP_CONFIG_PATH",
    "firebase_mcp": "FIREBASE_MCP_CONFIG_PATH",
}

DEFAULT_SQLITE_DB_NAME = "db/dev.sqlite3"

MODEL_REASONING_OPTIONS: Dict[str, List[Tuple[str, str, str]]] = {
    "gpt-5.4": [
        ("Minimal", "minimal", "Fastest responses with little reasoning"),
        ("Low", "low", "Balances speed with some reasoning; useful for straightforward queries and short explanations"),
        ("Medium", "medium", "Provides a solid balance of reasoning depth and latency for general-purpose tasks"),
        ("High", "high", "Maximizes reasoning depth for complex or ambiguous problems"),
    ],
    "gpt-5.3-codex": [
        ("Minimal", "minimal", "Fastest responses with little reasoning"),
        ("Low", "low", "Balances speed with some reasoning; useful for straightforward queries and short explanations"),
        ("Medium", "medium", "Provides a solid balance of reasoning depth and latency for general-purpose tasks"),
        ("High", "high", "Maximizes reasoning depth for complex or ambiguous problems"),
    ],
    "gpt-5.3-codex-spark": [
        ("Low", "low", "Fastest responses with limited reasoning"),
        ("Medium", "medium", "Dynamically adjusts reasoning based on the task"),
        ("High", "high", "Maximizes reasoning depth for complex or ambiguous problems"),
    ],
    "gpt-5.2": [
        ("Minimal", "minimal", "Fastest responses with little reasoning"),
        ("Low", "low", "Balances speed with some reasoning; useful for straightforward queries and short explanations"),
        ("Medium", "medium", "Provides a solid balance of reasoning depth and latency for general-purpose tasks"),
        ("High", "high", "Maximizes reasoning depth for complex or ambiguous problems"),
    ],
}

MODEL_DEFAULT_EFFORT = {
    "gpt-5.4": "high",
    "gpt-5.3-codex": "medium",
    "gpt-5.3-codex-spark": "medium",
    "gpt-5.2": "medium",
}

MODEL_DISPLAY_ORDER = ["gpt-5.4", "gpt-5.3-codex", "gpt-5.3-codex-spark", "gpt-5.2"]
DEFAULT_MODEL = "gpt-5.4"
DEFAULT_REASONING = MODEL_DEFAULT_EFFORT[DEFAULT_MODEL]


def _reasoning_label(model: str, effort: str) -> str:
    for label, key, _ in MODEL_REASONING_OPTIONS.get(model, []):
        if key == effort:
            return label
    return effort.title()


def _toml_literal(value: object) -> str:
    if value is None:
        return '""'
    if isinstance(value, bool):
        return "true" if value else "false"
    if isinstance(value, (int, float)):
        return str(value)
    if isinstance(value, str):
        escaped = value.replace("\\", "\\\\").replace('"', '\\"')
        return f'"{escaped}"'
    if isinstance(value, dict):
        parts = [f'{k}={_toml_literal(v)}' for k, v in value.items()]
        return "{" + ", ".join(parts) + "}"
    if isinstance(value, (list, tuple)):
        parts = [_toml_literal(v) for v in value]
        return "[" + ", ".join(parts) + "]"
    return _toml_literal(str(value))


def _toml_override(key: str, value: object) -> str:
    return f"{key}={_toml_literal(value)}"


def _dynamic_mcp_overrides(name: str, config: Dict[str, object]) -> List[str]:
    if config.get("_source") not in {"self_mcp", "project_mcp"}:
        return []

    overrides: List[str] = []
    for field in ("command", "args", "env", "transport", "cwd", "startup_timeout_sec", "timeout", "tool_timeout_sec"):
        if field in config:
            overrides.extend(["-c", _toml_override(f"mcp_servers.{name}.{field}", config[field])])
    return overrides


@dataclass
class SessionSummary:
    session_id: str
    created: datetime
    updated: datetime
    preview: str
    cwd: Optional[Path] = None


@dataclass
class DocumentationBundle:
    name: str
    root: Path
    readme: Optional[Path]
    contents: Optional[Path]


@dataclass
class LLMSelection:
    display_model: str
    detail: str
    provider_slug: str
    model: str
    reasoning_effort: Optional[str]
    requires_provider_override: bool


@dataclass
class ExecutionPreferences:
    dangerous_sandbox: bool
    auto_approve: bool


@dataclass(frozen=True)
class ExternalModelOption:
    key: str
    menu_label: str
    provider: str
    model: str
    description: str
    required_env_vars: Tuple[str, ...]


LLM_ENV_PATH = Path.home() / ".codex" / "llm.env"


class CLIAdapter(ABC):
    """Adapter describing how to invoke the underlying Codex CLI."""

    name: str
    command: str
    supports_mcp: bool
    supports_prompt: bool
    supports_llm_selection: bool
    supports_resume: bool

    def __init__(
        self,
        name: str,
        command: str,
        *,
        supports_mcp: bool = True,
        supports_prompt: bool = True,
        supports_llm_selection: bool = True,
        supports_resume: bool = True,
    ) -> None:
        self.name = name
        self.command = command
        self.supports_mcp = supports_mcp
        self.supports_prompt = supports_prompt
        self.supports_llm_selection = supports_llm_selection
        self.supports_resume = supports_resume

    @abstractmethod
    def build_llm_args(self, selection: LLMSelection) -> List[str]:  # pragma: no cover - interactive wrapper
        """Translate LLM selection into CLI args."""

    def build_execution_args(self, prefs: ExecutionPreferences) -> List[str]:  # pragma: no cover - interactive wrapper
        return []

    def build_mcp_flags(
        self,
        selected: Dict[str, Dict[str, object]],
        available: Dict[str, Dict[str, object]],
    ) -> List[str]:  # pragma: no cover - interactive wrapper
        if not self.supports_mcp:
            return []
        overrides: List[str] = []
        for name, cfg in available.items():
            enabled = "true" if name in selected else "false"
            overrides.extend(["-c", f"mcp_servers.{name}.enabled={enabled}"])
            overrides.extend(_dynamic_mcp_overrides(name, cfg))
        return overrides

    def prepare_env(self, env: Dict[str, str], selection: LLMSelection) -> None:  # pragma: no cover - interactive wrapper
        """Allow adapters to mutate environment variables before launch."""
        return

    def build_prompt_args(self, prompt: str) -> List[str]:
        if not prompt:
            return []
        return [prompt]


class CodexCLIAdapter(CLIAdapter):
    def __init__(self) -> None:
        super().__init__("codex", "codex", supports_mcp=True)

    def build_llm_args(self, selection: LLMSelection) -> List[str]:
        args = ["--model", selection.model]
        if selection.reasoning_effort:
            args.extend(["-c", f'model_reasoning_effort="{selection.reasoning_effort}"'])
        if selection.model == "gpt-5.3-codex-spark":
            args.extend([
                "-c", "model_context_window=127000",
                "-c", "model_auto_compact_token_limit=112000",
            ])
        if selection.requires_provider_override:
            args.extend(["-c", f'model_provider="{selection.provider_slug}"'])
        return args

    def build_execution_args(self, prefs: ExecutionPreferences) -> List[str]:
        args: List[str] = []
        if prefs.dangerous_sandbox:
            args.extend(["--sandbox", "danger-full-access"])
        if prefs.auto_approve:
            args.extend(["--ask-for-approval", "never"])
        return args

    def build_prompt_args(self, prompt: str) -> List[str]:
        if not prompt:
            return []
        return ["--", prompt]


class OpenCodexCLIAdapter(CLIAdapter):
    def __init__(self) -> None:
        super().__init__("open-codex", "open-codex", supports_mcp=False)

    def build_llm_args(self, selection: LLMSelection) -> List[str]:
        provider = selection.provider_slug or "openai"
        args = ["--provider", provider, "--model", selection.model]
        return args

    def build_execution_args(self, prefs: ExecutionPreferences) -> List[str]:
        args: List[str] = []
        if prefs.dangerous_sandbox and prefs.auto_approve:
            args.append("--dangerously-auto-approve-everything")
        elif prefs.auto_approve:
            args.append("--full-auto")
        elif prefs.dangerous_sandbox:
            args.append("--dangerously-auto-approve-everything")
        return args

    def build_prompt_args(self, prompt: str) -> List[str]:
        if not prompt:
            return []
        return ["--", prompt]

    def prepare_env(self, env: Dict[str, str], selection: LLMSelection) -> None:
        if selection.provider_slug == "gemini" and not env.get("GOOGLE_GENERATIVE_AI_API_KEY"):
            gemini_key = env.get("GEMINI_API_KEY")
            if gemini_key:
                env["GOOGLE_GENERATIVE_AI_API_KEY"] = gemini_key


CODEX_CLI_ADAPTER = CodexCLIAdapter()
OPEN_CODEX_CLI_ADAPTER = OpenCodexCLIAdapter()
class CrushCLIAdapter(CLIAdapter):
    def __init__(self) -> None:
        super().__init__(
            "crush",
            "crush",
            supports_mcp=True,
            supports_prompt=False,
            supports_llm_selection=False,
            supports_resume=False,
        )
        self._config_path = Path.home() / ".config" / "crush" / "crush.json"

    def build_llm_args(self, selection: LLMSelection) -> List[str]:
        return []

    def build_execution_args(self, prefs: ExecutionPreferences) -> List[str]:
        args: List[str] = []
        if prefs.auto_approve:
            args.append("-y")
        return args

    def build_mcp_flags(
        self,
        selected: Dict[str, Dict[str, object]],
        available: Dict[str, Dict[str, object]],
    ) -> List[str]:
        self._sync_mcp_config(selected, available)
        return []

    def _sync_mcp_config(
        self,
        selected: Dict[str, Dict[str, object]],
        available: Dict[str, Dict[str, object]],
    ) -> None:
        if not available:
            return
        config_dir = self._config_path.parent
        try:
            config_dir.mkdir(parents=True, exist_ok=True)
        except OSError as exc:
            print(f"Не удалось подготовить каталог {config_dir}: {exc}")
            return
        data: Dict[str, object] = {}
        if self._config_path.exists():
            try:
                with self._config_path.open("r", encoding="utf-8") as fh:
                    data = json.load(fh)
            except (OSError, json.JSONDecodeError) as exc:
                print(f"Не удалось прочитать {self._config_path}: {exc}. Будет создан новый файл.")
                data = {}
        mcp_section = data.get("mcp")
        if not isinstance(mcp_section, dict):
            mcp_section = {}
        enabled_names = set(selected.keys())
        for name in sorted(available):
            server = available[name]
            if not isinstance(server, dict):
                continue
            entry: Dict[str, object] = dict(mcp_section.get(name) or {})
            transport = str(server.get("transport") or "stdio").lower()
            if transport not in ("stdio", "http", "sse"):
                transport = "stdio"
            entry["type"] = transport
            if transport == "stdio":
                command = server.get("command")
                if not isinstance(command, str):
                    continue
                entry["command"] = command
                args = server.get("args")
                if isinstance(args, list):
                    entry["args"] = [str(arg) for arg in args]
                else:
                    entry["args"] = []
            else:
                url = server.get("url")
                if isinstance(url, str):
                    entry["url"] = url
            timeout = server.get("timeout") or server.get("startup_timeout_sec")
            if isinstance(timeout, int):
                entry["timeout"] = timeout
            env_values = server.get("env")
            if isinstance(env_values, dict) and env_values:
                entry["env"] = {str(key): str(value) for key, value in env_values.items()}
            entry["disabled"] = name not in enabled_names
            mcp_section[name] = entry
        data["mcp"] = mcp_section
        data.setdefault("$schema", "https://charm.land/crush.json")
        try:
            with self._config_path.open("w", encoding="utf-8") as fh:
                json.dump(data, fh, ensure_ascii=False, indent=2)
                fh.write("\n")
        except OSError as exc:
            print(f"Не удалось записать {self._config_path}: {exc}")


CRUSH_CLI_ADAPTER = CrushCLIAdapter()

EXTERNAL_MODEL_OPTIONS: List[ExternalModelOption] = [
    ExternalModelOption(
        key="minimax/minimax-m2.5",
        menu_label="MiniMax M2.5",
        provider="openrouter",
        model="minimax/minimax-m2.5",
        description="OpenRouter",
        required_env_vars=("OPENROUTER_API_KEY",),
    ),
    ExternalModelOption(
        key="z-ai/glm-5",
        menu_label="Z-AI GLM-5",
        provider="openrouter",
        model="z-ai/glm-5",
        description="OpenRouter",
        required_env_vars=("OPENROUTER_API_KEY",),
    ),
    ExternalModelOption(
        key="google/gemini-3.1-flash-lite-preview",
        menu_label="Gemini 3.1 Flash Lite Preview",
        provider="openrouter",
        model="google/gemini-3.1-flash-lite-preview",
        description="OpenRouter",
        required_env_vars=("OPENROUTER_API_KEY",),
    ),
    ExternalModelOption(
        key="google/gemini-3.1-pro-preview-customtools",
        menu_label="Gemini 3.1 Pro Preview (CustomTools)",
        provider="openrouter",
        model="google/gemini-3.1-pro-preview-customtools",
        description="OpenRouter",
        required_env_vars=("OPENROUTER_API_KEY",),
    ),
    ExternalModelOption(
        key="qwen/qwen3-max-thinking",
        menu_label="Qwen3 Max Thinking",
        provider="openrouter",
        model="qwen/qwen3-max-thinking",
        description="OpenRouter",
        required_env_vars=("OPENROUTER_API_KEY",),
    ),
    ExternalModelOption(
        key="qwen/qwen3-coder-next",
        menu_label="Qwen3 Coder Next",
        provider="openrouter",
        model="qwen/qwen3-coder-next",
        description="OpenRouter",
        required_env_vars=("OPENROUTER_API_KEY",),
    ),
    ExternalModelOption(
        key="qwen/qwen3.5-flash-02-23",
        menu_label="Qwen3.5 Flash (02-23)",
        provider="openrouter",
        model="qwen/qwen3.5-flash-02-23",
        description="OpenRouter",
        required_env_vars=("OPENROUTER_API_KEY",),
    ),
    ExternalModelOption(
        key="qwen/qwen3.6-plus:free",
        menu_label="Qwen3.6 Plus (free)",
        provider="openrouter",
        model="qwen/qwen3.6-plus:free",
        description="OpenRouter",
        required_env_vars=("OPENROUTER_API_KEY",),
    ),
    ExternalModelOption(
        key="stepfun/step-3.5-flash:free",
        menu_label="StepFun Step-3.5 Flash (free)",
        provider="openrouter",
        model="stepfun/step-3.5-flash:free",
        description="OpenRouter",
        required_env_vars=("OPENROUTER_API_KEY",),
    ),
    ExternalModelOption(
        key="moonshotai/kimi-k2.5",
        menu_label="Kimi K2.5",
        provider="openrouter",
        model="moonshotai/kimi-k2.5",
        description="OpenRouter",
        required_env_vars=("OPENROUTER_API_KEY",),
    ),
    ExternalModelOption(
        key="openai/gpt-oss-120b:free",
        menu_label="OpenAI GPT-OSS 120B (free)",
        provider="openrouter",
        model="openai/gpt-oss-120b:free",
        description="OpenRouter",
        required_env_vars=("OPENROUTER_API_KEY",),
    ),
]
EXTERNAL_MODEL_MAP = {option.key: option for option in EXTERNAL_MODEL_OPTIONS}

def _flag_items() -> List[Option]:
    # Grouped: Flags vs Options (display text, not raw flags)
    items: List[Option] = []
    items.append(Option("Flags", is_header=True))
    items.append(Option("Dangerous sandbox (full local access)", SANDBOX_FLAG_VALUE))
    items.append(Option("No approval prompts", AUTO_APPROVE_FLAG_VALUE))
    items.append(Option("Options", is_header=True))
    items.append(Option(YOLO_KEY, YOLO_KEY))
    items.append(Option("No MCP preprompts", NO_MCP_PREPROMPTS_FLAG_VALUE))
    return items


def _current_model_settings(config: Dict[str, object]) -> Tuple[str, str]:
    configured_model = config.get("model")
    current_model = configured_model if isinstance(configured_model, str) else DEFAULT_MODEL

    if current_model not in MODEL_REASONING_OPTIONS:
        current_model = DEFAULT_MODEL

    configured_effort = config.get("model_reasoning_effort")
    if isinstance(configured_effort, str):
        current_effort = configured_effort.lower()
    elif configured_effort is not None:
        current_effort = str(configured_effort).lower()
    else:
        current_effort = MODEL_DEFAULT_EFFORT.get(current_model, DEFAULT_REASONING)

    valid_efforts = {effort for _, effort, _ in MODEL_REASONING_OPTIONS[current_model]}
    if current_effort not in valid_efforts:
        current_effort = MODEL_DEFAULT_EFFORT.get(current_model, DEFAULT_REASONING)

    return current_model, current_effort


def _relative_to_cwd(path: Path, cwd: Path) -> str:
    try:
        return path.relative_to(cwd).as_posix()
    except ValueError:
        return path.as_posix()


def _discover_sqlite_files(root: Path, limit: int = 20) -> List[Path]:
    exts = {".sqlite", ".sqlite3", ".db"}
    ignore_dirs = {
        ".git",
        ".idea",
        ".vscode",
        "node_modules",
        "dist",
        "build",
        ".venv",
        ".env",
        ".tox",
        "__pycache__",
        ".mypy_cache",
        ".pytest_cache",
    }
    found: List[Path] = []
    for dirpath, dirnames, filenames in os.walk(root):
        dirnames[:] = [
            name
            for name in dirnames
            if name not in ignore_dirs and not name.startswith(".git")
        ]
        for filename in filenames:
            if len(found) >= limit:
                return found
            suffix = Path(filename).suffix.lower()
            if suffix in exts:
                found.append(Path(dirpath) / filename)
                if len(found) >= limit:
                    return found
    return found


def _ensure_sqlite_scaffold(cwd: Path) -> Optional[Path]:
    config_path = cwd / "sqliteMCP.toml"
    if config_path.exists():
        return config_path

    def _finalize_with_path(db_path: Path, *, created_db: bool) -> Path:
        config_value = _relative_to_cwd(db_path, cwd)
        content = textwrap.dedent(
            f"""\
            # Auto-generated by codex-pro (SQLite MCP)
            [sqlite]
            db_path = "{config_value}"
            create_if_missing = false

            [mcp]
            extra_args = []
            env = {{}}
            """
        ).strip() + "\n"
        try:
            config_path.write_text(content, encoding="utf-8")
        except OSError as exc:
            print(f"Не удалось записать sqliteMCP.toml: {exc}.")
            return config_path
        if created_db:
            print(f"Созданы {config_path} и база {db_path}")
        else:
            print(f"Создан {config_path}; используется существующая база {db_path}")
        return config_path

    candidates = _discover_sqlite_files(cwd, limit=20)
    if candidates:
        options: List[Option] = [Option("Найденные SQLite файлы", is_header=True)]
        for path in candidates:
            label = _relative_to_cwd(path, cwd)
            options.append(Option(label, path))
        options.append(Option("Создать новую SQLite базу", "create_new"))
        selection = single_select_items(
            options,
            title="Выберите существующую SQLite базу или создайте новую",
        )
        if selection is None:
            print("Отмена настройки SQLite MCP.")
            return None
        if isinstance(selection, Path):
            return _finalize_with_path(selection.resolve(), created_db=False)

    print(
        "\n[SQLite MCP] В корне проекта не найден sqliteMCP.toml — нужно создать базу и конфиг.\n"
        "Введите имя файла для SQLite (относительно корня) или абсолютный путь.\n"
        "Нажмите Enter, чтобы использовать значение по умолчанию."
    )
    default_choice = DEFAULT_SQLITE_DB_NAME
    while True:
        try:
            raw = input(f"Имя файла SQLite [{default_choice}]: ").strip()
        except KeyboardInterrupt:
            print("\nОтмена настройки SQLite MCP.")
            return None
        if not raw:
            raw = default_choice
        expanded = Path(os.path.expandvars(os.path.expanduser(raw)))
        if expanded.is_absolute():
            db_path = expanded
        else:
            db_path = (cwd / expanded).resolve()
        try:
            db_path.parent.mkdir(parents=True, exist_ok=True)
            db_path.touch(exist_ok=True)
        except OSError as exc:
            print(f"Не удалось создать файл базы {db_path}: {exc}. Попробуйте другой путь.")
            continue

        return _finalize_with_path(db_path, created_db=True)


def _select_model_reasoning(config: Dict[str, object]) -> Tuple[str, str]:
    current_model, current_effort = _current_model_settings(config)

    model_entries: Dict[str, List[Tuple[str, str, str]]] = {
        key: list(options) for key, options in MODEL_REASONING_OPTIONS.items()
    }
    menu_models: List[str] = list(MODEL_DISPLAY_ORDER)

    if current_model not in model_entries:
        label = current_effort.title()
        description = "Use configured defaults from config.toml"
        model_entries[current_model] = [(label, current_effort, description)]
        menu_models.append(current_model)
    elif current_model not in menu_models:
        menu_models.append(current_model)

    valid_efforts = {effort for _, effort, _ in model_entries[current_model]}
    if current_effort not in valid_efforts:
        label = current_effort.title()
        description = "Use configured defaults from config.toml"
        model_entries[current_model].append((label, current_effort, description))
        valid_efforts.add(current_effort)

    items: List[Option] = []
    seen_pairs: set[Tuple[str, str]] = set()
    for model in menu_models:
        options = model_entries.get(model, [])
        if not options:
            continue
        items.append(Option(model, is_header=True))
        default_effort = MODEL_DEFAULT_EFFORT.get(model)
        for label, effort, description in options:
            suffix_parts: List[str] = []
            if default_effort and effort == default_effort:
                suffix_parts.append("default")
            if model == current_model and effort == current_effort:
                suffix_parts.append("current")
            display_label = label
            if suffix_parts:
                display_label = f"{label} ({', '.join(suffix_parts)})"
            text = f"{display_label} — {description}"
            items.append(Option(text, (model, effort)))
            seen_pairs.add((model, effort))

    preselected = (current_model, current_effort)
    if preselected not in seen_pairs:
        preselected = (DEFAULT_MODEL, DEFAULT_REASONING)

    choice = single_select_items(
        items,
        title="Select Codex model & reasoning level (enter confirm, q cancel)",
        preselected_value=preselected,
    )
    if choice is None:
        print("Cancelled.")
        sys.exit(130)
    selected_model, selected_effort = choice
    return selected_model, selected_effort


def _load_llm_env() -> Dict[str, str]:
    data: Dict[str, str] = {}
    if not LLM_ENV_PATH.exists():
        return data
    try:
        for line in LLM_ENV_PATH.read_text(encoding="utf-8").splitlines():
            stripped = line.strip()
            if not stripped or stripped.startswith("#"):
                continue
            if "=" not in stripped:
                continue
            key, value = stripped.split("=", 1)
            data[key.strip()] = value.strip().strip('"').strip("'")
    except OSError as exc:
        print(f"Не удалось прочитать {LLM_ENV_PATH}: {exc}")
    return data


def _merge_env_with_os(loaded: Dict[str, str]) -> Dict[str, str]:
    env = os.environ.copy()
    env.update(loaded)
    return env


def _env_available(key: str, loaded: Dict[str, str]) -> bool:
    if key == "GEMINI_API_KEY":
        if loaded.get("GEMINI_API_KEY") or os.environ.get("GEMINI_API_KEY"):
            return True
        return bool(loaded.get("GOOGLE_GENERATIVE_AI_API_KEY") or os.environ.get("GOOGLE_GENERATIVE_AI_API_KEY"))
    return bool(loaded.get(key) or os.environ.get(key))


def _display_env_name(key: str) -> str:
    if key == "GEMINI_API_KEY":
        return "GEMINI_API_KEY (или GOOGLE_GENERATIVE_AI_API_KEY)"
    return key


def _select_llm_mode(config: Dict[str, object]) -> Tuple[LLMSelection, Dict[str, str]]:
    loaded_env = _load_llm_env()
    default_model, default_effort = _current_model_settings(config)
    codex_label = f"Codex (OpenAI) — {default_model} / {_reasoning_label(default_model, default_effort)}"

    items: List[Option] = [
        Option("LLM providers", is_header=True),
        Option(codex_label, ("codex", None)),
    ]
    for option in EXTERNAL_MODEL_OPTIONS:
        label = f"{option.menu_label} ({option.description})"
        items.append(Option(label, ("external", option.key)))

    choice = single_select_items(
        items,
        title="Select LLM provider/model (enter confirm, q cancel)",
        preselected_value=("codex", None),
    )
    if choice is None:
        print("Cancelled.")
        sys.exit(130)

    provider_key, identifier = choice
    if provider_key == "codex":
        selected_model, selected_effort = _select_model_reasoning(config)
        detail = _reasoning_label(selected_model, selected_effort)
        return (
            LLMSelection(
                display_model=selected_model,
                detail=detail,
                provider_slug="openai",
                model=selected_model,
                reasoning_effort=selected_effort,
                requires_provider_override=False,
            ),
            loaded_env,
        )

    external = EXTERNAL_MODEL_MAP.get(identifier)
    if not external:
        print(f"Unknown external model {identifier}")
        sys.exit(1)
    missing = [key for key in external.required_env_vars if not _env_available(key, loaded_env)]
    if missing:
        printable = ", ".join(_display_env_name(key) for key in missing)
        print(
            f"Для использования модели {external.menu_label} нужны переменные {printable} "
            f"в {LLM_ENV_PATH}. Заполните файл и повторите."
        )
        sys.exit(1)
    detail = external.description
    return (
        LLMSelection(
            display_model=external.menu_label,
            detail=detail,
            provider_slug=external.provider,
            model=external.model,
            reasoning_effort=None,
            requires_provider_override=True,
        ),
        loaded_env,
    )


def _select_execution_preferences() -> Tuple[ExecutionPreferences, bool, bool]:
    items = _flag_items()
    preselected_values = [SANDBOX_FLAG_VALUE, AUTO_APPROVE_FLAG_VALUE]
    chosen_values = multi_select_items(
        items,
        title="Toggle Codex options (space toggle, enter confirm, a toggle all, q cancel)",
        preselected_values=preselected_values,
    )
    if chosen_values is None:
        print("Cancelled.")
        sys.exit(130)
    prefs = ExecutionPreferences(
        dangerous_sandbox=SANDBOX_FLAG_VALUE in chosen_values,
        auto_approve=AUTO_APPROVE_FLAG_VALUE in chosen_values,
    )
    return prefs, YOLO_KEY in chosen_values, NO_MCP_PREPROMPTS_FLAG_VALUE in chosen_values


def _select_preset() -> str:
    items = [
        Option("Custom flow (manual setup)", PRESET_NONE),
        Option("Быстрое исследование — danger sandbox + no approval; Google Search + Telegram", PRESET_FAST_RESEARCH),
        Option("Create Project MCP — danger sandbox + no approval; Dart/Flutter + Telegram; preset global prompt", PRESET_CREATE_PROJECT_MCP),
        Option("Простой чат — danger sandbox + no approval; без MCP", PRESET_SIMPLE_CHAT),
        Option("Кодинг — ограниченный список MCP, фильтр project_book файлов, без глобальных промптов", PRESET_CODING),
        Option("Fixer (Orchestrator) — no sandbox, append fixer skill", PRESET_FIXER),
        Option("Netrunner (Solo) — danger sandbox, append netrunner skill", PRESET_NETRUNNER),
        Option("Overseer (Global) — no sandbox, append overseer skill", PRESET_OVERSEER),
    ]
    choice = single_select_items(
        items,
        title="Select preset (enter confirm, q cancel)",
        preselected_value=PRESET_NONE,
    )
    if choice is None:
        print("Cancelled.")
        sys.exit(130)
    return choice


def _select_role_preset() -> str:
    items = [
        Option("Overseer (Global)", PRESET_OVERSEER),
        Option("Fixer (Orchestrator)", PRESET_FIXER),
        Option("Netrunner (Solo)", PRESET_NETRUNNER),
    ]
    choice = single_select_items(
        items,
        title="Select role (enter confirm, q cancel)",
        preselected_value=PRESET_OVERSEER,
    )
    if choice is None:
        print("Cancelled.")
        sys.exit(130)
    return choice


def _find_global_prompt_by_stem(global_dir: Path, stems: Iterable[str]) -> Optional[Path]:
    prompts = load_global_prompts(global_dir)
    lookup = {name.lower(): path for name, path in prompts}
    for stem in stems:
        candidate = lookup.get(stem.lower())
        if candidate:
            return candidate
    return None


def _select_target_mcp_name(self_mcp_names: List[str]) -> Optional[str]:
    options: List[Option] = [Option("Создать новый MCP-сервер (ввести имя)", "__new__")]
    if self_mcp_names:
        options.append(Option("Использовать существующий self MCP", is_header=True))
        for name in self_mcp_names:
            options.append(Option(name, name))
    choice = single_select_items(
        options,
        title="Выберите целевой MCP-сервер (или создайте новый)",
        preselected_value="__new__",
    )
    if choice is None:
        return None
    if choice == "__new__":
        try:
            entered = input("Введите имя MCP-сервера (a-z, 0-9, -, _): ").strip()
        except KeyboardInterrupt:
            return None
        if not entered:
            return None
        sanitized = "".join(ch for ch in entered if ch.isalnum() or ch in ("-", "_"))
        return sanitized or None
    if isinstance(choice, str):
        return choice
    return None


def _locate_orchestrator_brief(cwd: Path, global_prompts_dir: Optional[Path]) -> Optional[Path]:
    for candidate in ORCHESTRATOR_BRIEF_CANDIDATES:
        path = (cwd / candidate).resolve()
        if path.is_file():
            return path
    if global_prompts_dir:
        global_candidate = global_prompts_dir / "mcp_orchestrator_brief.md"
        if global_candidate.is_file():
            return global_candidate
    return None


def _copy_orchestrator_brief(src: Path, target_dir: Path) -> Optional[Path]:
    try:
        target_dir.mkdir(parents=True, exist_ok=True)
    except OSError as exc:
        print(f"Не удалось создать директорию {target_dir}: {exc}")
        return None
    dest = target_dir / src.name
    try:
        # Always copy (overwrite) to keep template up to date.
        shutil.copy2(src, dest)
        return dest
    except OSError as exc:
        print(f"Не удалось скопировать {src} -> {dest}: {exc}")
        return None


HUMAN_RUNBOOK_NOTE = textwrap.dedent(
    """\
    Human runbook requirement:
    - Все MCP-тулы должны быть доступны и человеку через Makefile (один таргет = один tool) и/или локальный CLI-раннер.
    - Не добавляйте новый tool без соответствующего таргета/CLI-команды для ручного запуска и тестирования.
    - Keep parity: реализация одна, интерфейсы два — MCP (для AI) и Makefile/CLI (для человека)."""
).strip()


def _parse_pinned_mcp_names_from_env() -> List[str]:
    raw = os.environ.get("CODEX_PRO_PINNED_MCP") or os.environ.get("CODEX_PRO_PINNED_MCPS")
    if not raw:
        return []
    result: List[str] = []
    for chunk in raw.split(","):
        name = chunk.strip()
        if name:
            result.append(name)
    return result


def _load_project_priority_mcp_names(cwd: Path) -> List[str]:
    """Read MCP server priority from local MCPs.toml active profile (if present)."""
    path = cwd / "MCPs.toml"
    if not path.is_file():
        return []
    try:
        with path.open("rb") as fh:
            data = tomllib.load(fh)
    except Exception:
        return []

    active_profile = data.get("active_profile")
    profiles = data.get("profiles")
    if not isinstance(active_profile, str) or not isinstance(profiles, dict):
        return []
    profile = profiles.get(active_profile)
    if not isinstance(profile, dict):
        return []
    servers = profile.get("servers")
    if not isinstance(servers, dict):
        return []
    # Keep declaration order from TOML.
    return [str(name) for name in servers.keys()]


def _unique_preserve_order(names: Iterable[str]) -> List[str]:
    seen: set[str] = set()
    ordered: List[str] = []
    for name in names:
        if name in seen:
            continue
        seen.add(name)
        ordered.append(name)
    return ordered


def _select_mcp_servers(
    available_servers: Dict[str, Dict[str, object]],
    *,
    cwd: Path,
    allowed_servers: Optional[Iterable[str]] = None,
) -> Dict[str, Dict[str, object]]:
    def _env_file_has_any_key(path: Path, keys: set[str]) -> bool:
        try:
            text = path.read_text(encoding="utf-8")
        except FileNotFoundError:
            return False
        except OSError:
            return False
        for raw in text.splitlines():
            line = raw.strip()
            if not line or line.startswith("#"):
                continue
            if line.lower().startswith("export "):
                line = line[7:].lstrip()
            if "=" not in line:
                continue
            key = line.split("=", 1)[0].strip()
            if key in keys:
                return True
        return False

    # Filter out temporary removals
    allowed_set: Optional[set[str]] = set(allowed_servers) if allowed_servers is not None else None
    servers = {
        k: v
        for k, v in available_servers.items()
        if k != "dart_project_tools"
        and (
            allowed_set is None
            or k in allowed_set
            or v.get("_source") == "self_mcp"
        )
    }

    # Determine DB availability by presence of per-project config files in cwd
    has_pg = (cwd / "postgresMCP.toml").is_file()
    has_ch = (cwd / "clickhouseMCP.toml").is_file()
    has_framer_env = (cwd / ".env").is_file() or (cwd / ".env.local").is_file()
    has_figma_toml = (cwd / "figmaMCP.toml").is_file()
    figma_keys = {"FIGMA_API_KEY", "FIGMA_PERSONAL_ACCESS_TOKEN", "FIGMA_MCP_API_KEY"}
    figma_global_env = Path(os.environ.get("FIGMA_MCP_GLOBAL_ENV", "~/.codex/figma.env")).expanduser()
    has_figma_env = (
        has_framer_env
        or any(os.environ.get(key) for key in figma_keys)
        or _env_file_has_any_key(figma_global_env, figma_keys)
    )

    # Build grouped item list
    items: List[Option] = []
    used: set[str] = set()

    def _append_group(header: str, names: Iterable[str], *, disabled_map: Optional[Dict[str, bool]] = None) -> None:
        names = [name for name in names if name in servers and name not in used]
        if not names:
            return
        items.append(Option(header, is_header=True))
        for name in names:
            disabled = False
            if disabled_map and name in disabled_map:
                disabled = disabled_map[name]
            label = SERVER_DISPLAY_NAMES.get(name, name.replace("_", " ").title())
            items.append(Option(label, name, disabled=disabled))
            used.add(name)

    # Highest-priority section: project-specific servers and explicit pins.
    pinned_names = _unique_preserve_order(
        [
            *_parse_pinned_mcp_names_from_env(),
            *[name for name, cfg in servers.items() if cfg.get("_source") == "project_mcp"],
            *_load_project_priority_mcp_names(cwd),
        ]
    )
    _append_group("Project Priority", pinned_names)

    # Project MCP section first
    project_names = sorted(name for name, cfg in servers.items() if cfg.get("_source") == "project_mcp")
    _append_group("Project MCP Servers", project_names)

    self_names = [name for name, cfg in servers.items() if cfg.get("_source") == "self_mcp"]
    _append_group("Self MCP", self_names)

    # DB group with availability checks
    db_disabled = {
        "postgres": not has_pg,
        "schemacrawler": False,
        "clickhouse": not has_ch,
    }
    _append_group("DB", ("postgres", "schemacrawler", "clickhouse", "sqlite"), disabled_map=db_disabled)

    # Web-search group
    _append_group("Web-search", ("searCrawl", "google_search", "librex", "deep_research", "tavily", "exa"))
    # Figma requires a per-project figmaMCP.toml so we always know which file to target.
    design_disabled = {"framer": not has_framer_env, "figma": (not has_figma_toml) or (not has_figma_env)}
    _append_group("Design Tools", ("global_image_assets", "framer", "figma"), disabled_map=design_disabled)

    _append_group("Productivity", ("google_docs", "google_sheets", "firebase_mcp"))

    # Coding Documentation group
    _append_group(
        "Coding Documentation",
        ("dart_flutter", "gopls", "serverpod", "nodejs_docs", "laravel_mcp_companion", "shadcn"),
    )

    # Notification tools
    _append_group("Notifications", ("telegram_notify",))

    # General group (remaining servers)
    remaining = [name for name in servers if name not in used]
    if remaining:
        _append_group("General", remaining)

    preselected = []
    if "telegram_notify" in servers:
        preselected.append("telegram_notify")

    chosen = multi_select_items(
        items,
        title="Select MCP servers (space toggle, enter confirm, a toggle all, q cancel)",
        preselected_values=preselected,
    )
    if chosen is None:
        print("Cancelled.")
        sys.exit(130)
    return {name: servers[name] for name in chosen if name in servers}


def _filter_available_servers(
    available_servers: Dict[str, Dict[str, object]],
    desired: Iterable[str],
) -> Dict[str, Dict[str, object]]:
    return {name: available_servers[name] for name in desired if name in available_servers}


def _select_role_preset_servers(
    available_servers: Dict[str, Dict[str, object]],
    *,
    cwd: Path,
) -> Dict[str, Dict[str, object]]:
    selected: Dict[str, Dict[str, object]] = {}
    for name, cfg in available_servers.items():
        if cfg.get("_source") in {"project_mcp", "self_mcp"}:
            selected[name] = cfg

    if not selected and "sqlite" in available_servers and (cwd / "sqliteMCP.toml").is_file():
        selected["sqlite"] = available_servers["sqlite"]

    return selected

_WALK_IGNORE_DIRS = {
    ".git",
    ".idea",
    ".vscode",
    "node_modules",
    "dist",
    "build",
    ".venv",
    ".env",
    ".tox",
    "__pycache__",
    ".mypy_cache",
    ".pytest_cache",
}


def _sorted_candidates(paths: Iterable[Path], *, root: Path) -> List[Path]:
    unique = {p for p in paths if p.is_file()}
    return sorted(
        unique,
        key=lambda p: (len(p.relative_to(root).parts), str(p.relative_to(root))),
    )


def _find_config_candidates(root: Path, pattern: str, *, limit: int = 250) -> List[Path]:
    found: List[Path] = []
    for dirpath, dirnames, filenames in os.walk(root):
        dirnames[:] = [
            name
            for name in dirnames
            if name not in _WALK_IGNORE_DIRS and not name.startswith(".git")
        ]
        for filename in filenames:
            if len(found) >= limit:
                return _sorted_candidates(found, root=root)
            if fnmatch.fnmatch(filename, pattern):
                candidate = Path(dirpath) / filename
                if candidate.is_file():
                    found.append(candidate)
    return _sorted_candidates(found, root=root)


def _toml_load(path: Path) -> Optional[Dict[str, object]]:
    try:
        with path.open("rb") as fh:
            data = tomllib.load(fh)
        return data if isinstance(data, dict) else None
    except Exception:
        return None


def _mapping_has_sqlite_path(data: Dict[str, object]) -> bool:
    for key in ("db_path", "path", "file", "database"):
        value = data.get(key)
        if isinstance(value, str) and value.strip():
            return True
    return False


def _looks_like_sqlite_mcp_config(doc: Dict[str, object]) -> bool:
    sqlite_section = doc.get("sqlite")
    if isinstance(sqlite_section, dict) and _mapping_has_sqlite_path(sqlite_section):
        return True
    database_section = doc.get("database")
    if isinstance(database_section, dict) and _mapping_has_sqlite_path(database_section):
        return True
    return False


def _find_sqlite_config_candidates(root: Path, *, limit: int = 200) -> List[Path]:
    candidates: List[Path] = []
    for dirpath, dirnames, filenames in os.walk(root):
        dirnames[:] = [
            name
            for name in dirnames
            if name not in _WALK_IGNORE_DIRS and not name.startswith(".git")
        ]
        parts = Path(dirpath).parts
        in_mcp_configs_dir = False
        for idx in range(len(parts) - 1):
            if parts[idx] == "mcp" and parts[idx + 1] == "configs":
                in_mcp_configs_dir = True
                break

        for filename in filenames:
            if len(candidates) >= limit:
                return _sorted_candidates(candidates, root=root)
            if not filename.lower().endswith(".toml"):
                continue
            if "sqlite" not in filename.lower() and not in_mcp_configs_dir:
                continue
            path = Path(dirpath) / filename
            doc = _toml_load(path)
            if not doc:
                continue
            if _looks_like_sqlite_mcp_config(doc):
                candidates.append(path)

    return _sorted_candidates(candidates, root=root)


def _find_config_candidates_for_server(server_key: str, *, cwd: Path) -> List[Path]:
    if server_key == "sqlite":
        return _find_sqlite_config_candidates(cwd)
    pattern = CONFIG_PATTERNS.get(server_key)
    if not pattern:
        return []
    return _find_config_candidates(cwd, pattern)


def _select_config_for_server(server_key: str, *, cwd: Path) -> Optional[Path]:
    candidates = _find_config_candidates_for_server(server_key, cwd=cwd)
    if not candidates:
        return None

    auto_value = "__auto__"
    options: List[Option] = [Option("Auto-detect nearest config (default)", auto_value)]
    for path in candidates:
        try:
            rel = path.relative_to(cwd)
            label = str(rel)
        except ValueError:
            label = str(path)
        if server_key == "sqlite":
            doc = _toml_load(path)
            meta = doc.get("metadata") if isinstance(doc, dict) else None
            name = meta.get("name") if isinstance(meta, dict) else None
            if isinstance(name, str) and name.strip():
                label = f"{label} — {name.strip()}"
        options.append(Option(label, path))

    title = f"Select {SERVER_DISPLAY_NAMES.get(server_key, server_key)} config"
    selection = single_select_items(options, title=title, preselected_value=auto_value)
    if selection is None:
        print("Cancelled.")
        sys.exit(130)
    if selection == auto_value:
        return candidates[0]
    if isinstance(selection, Path):
        return selection
    try:
        return Path(selection)  # type: ignore[arg-type]
    except (TypeError, ValueError):
        return None


def _collect_config_overrides(selected_servers: Dict[str, Dict[str, object]], *, cwd: Path) -> Dict[str, Path]:
    overrides: Dict[str, Path] = {}
    for server_key in ("postgres", "clickhouse", "sqlite", "firebase_mcp"):
        if server_key not in selected_servers:
            continue
        chosen = _select_config_for_server(server_key, cwd=cwd)
        if chosen:
            overrides[server_key] = chosen
    return overrides



def _select_files(root: Path) -> List[Path]:
    candidates = find_textual_files(root)
    if not candidates:
        return []
    path_map = {str(path.relative_to(root)): path for path in candidates}
    options = list(path_map.keys())
    selection = multi_select_tree(
        options,
        title="Select .md/.txt files to preload (space toggle, enter confirm, a toggle all, q cancel)",
    )
    if selection is None:
        print("Cancelled.")
        sys.exit(130)
    selected_paths = [path_map[sel] for sel in selection]
    return selected_paths


def _select_project_book_files(root: Path) -> List[Path]:
    project_book = root / "project_book"
    if not project_book.is_dir():
        return []

    candidates = [path for path in find_textual_files(project_book) if project_book in path.parents or path == project_book]
    if not candidates:
        return []

    rel_map = {str(path.relative_to(root)): path for path in candidates}
    preselected: List[str] = []

    # Prefer agents_onboarding.md, fallback to agent_onboarding.md
    onboarding_candidates = [
        project_book / "agents_onboarding.md",
        project_book / "agent_onboarding.md",
    ]
    for onboarding in onboarding_candidates:
        if onboarding.is_file():
            preselected.append(str(onboarding.relative_to(root)))
            break

    force_expand_dirs: List[str] = []
    reverse_sort_dirs = set()
    session_logs = project_book / "session_logs"
    if session_logs.is_dir():
        subdirs = [p for p in session_logs.iterdir() if p.is_dir()]
        if subdirs:
            date_like = [p for p in subdirs if p.name[:4].isdigit()]
            target_candidates = date_like or subdirs
            latest = sorted(target_candidates, key=lambda p: p.name)[-1]
            force_expand_dirs.append(str(session_logs.relative_to(root)))
            force_expand_dirs.append(str(latest.relative_to(root)))
        reverse_sort_dirs.add(str(session_logs.relative_to(root)))

    selection = multi_select_tree(
        rel_map.keys(),
        title="Select project_book files to preload",
        preselected=preselected,
        force_expand_dirs=force_expand_dirs if force_expand_dirs else None,
        reverse_sort_dirs=reverse_sort_dirs if reverse_sort_dirs else None,
    )
    if selection is None:
        print("Cancelled.")
        sys.exit(130)
    return [rel_map[s] for s in selection]


def _merge_unique_paths(*groups: Iterable[Path]) -> List[Path]:
    seen: set[Path] = set()
    merged: List[Path] = []
    for group in groups:
        for path in group:
            resolved = path.resolve()
            if resolved in seen:
                continue
            seen.add(resolved)
            merged.append(path)
    return merged


def _read_markdown_lines(path: Path) -> List[str]:
    try:
        return path.read_text(encoding="utf-8").splitlines()
    except OSError:
        return []


def _consume_front_matter(lines: List[str]) -> Tuple[int, Optional[str]]:
    if not lines or lines[0].strip() != "---":
        return 0, None
    idx = 1
    title_value: Optional[str] = None
    while idx < len(lines):
        current = lines[idx].strip()
        if current == "---":
            return idx + 1, title_value
        if current.lower().startswith("title:"):
            candidate = current.split(":", 1)[1].strip().strip('"').strip("'")
            if candidate:
                title_value = candidate
        idx += 1
    return len(lines), title_value


def _extract_markdown_title(path: Path, fallback: str) -> str:
    lines = _read_markdown_lines(path)
    start_idx, fm_title = _consume_front_matter(lines)
    if fm_title:
        return fm_title
    for line in lines[start_idx:]:
        stripped = line.strip()
        if not stripped:
            continue
        if stripped.startswith("#"):
            stripped = stripped.lstrip("#").strip()
        return stripped
    return fallback


def _extract_markdown_summary(path: Path) -> str:
    lines = _read_markdown_lines(path)
    start_idx, _ = _consume_front_matter(lines)
    for line in lines[start_idx:]:
        stripped = line.strip()
        if not stripped:
            continue
        if stripped.startswith("#"):
            continue
        return stripped
    return ""


def _doc_bundle_roots(cwd: Path) -> List[Path]:
    """Return all directories that may contain coding documentation bundles."""
    roots: List[Path] = []
    seen: set[Path] = set()

    def _maybe_add(path: Path) -> None:
        expanded = path.expanduser()
        resolved = expanded.resolve()
        if resolved in seen or not resolved.is_dir():
            return
        seen.add(resolved)
        roots.append(resolved)

    _maybe_add(cwd / "coding_scraped_docs")
    repo_root = Path(__file__).resolve().parent.parent
    _maybe_add(repo_root / "coding_scraped_docs")
    return roots


def _select_doc_bundles(cwd: Path) -> List[DocumentationBundle]:
    roots = _doc_bundle_roots(cwd)
    if not roots:
        return []

    bundles: List[Tuple[str, Path, Optional[Path], Optional[Path], str, str]] = []
    seen_dirs: set[Path] = set()
    for root in roots:
        for entry in sorted(root.iterdir(), key=lambda p: p.name.lower()):
            if not entry.is_dir():
                continue
            resolved_entry = entry.resolve()
            if resolved_entry in seen_dirs:
                continue
            readme = entry / "README.md"
            contents = entry / "CONTENTS.md"
            if not readme.exists() and not contents.exists():
                continue
            title = (
                _extract_markdown_title(readme, entry.name.replace("_", " ").title())
                if readme.exists()
                else entry.name.replace("_", " ").title()
            )
            summary_source = contents if contents.exists() else readme
            summary = _extract_markdown_summary(summary_source) if summary_source.exists() else ""
            rel_display = _relative_to_cwd(entry, cwd)
            bundles.append(
                (
                    title,
                    resolved_entry,
                    readme if readme.exists() else None,
                    contents if contents.exists() else None,
                    rel_display,
                    summary,
                )
            )
            seen_dirs.add(resolved_entry)

    if not bundles:
        return []

    items: List[Option] = [Option("Coding Documentation Bundles", is_header=True)]
    for title, path, readme, contents, rel, summary in bundles:
        snippet = summary.replace("\n", " ").strip()
        label = title
        if snippet:
            snippet = textwrap.shorten(snippet, width=70, placeholder="…")
            label = f"{title} — {snippet}"
        label = f"{label} [{rel}]"
        items.append(Option(label, (title, path, readme, contents)))

    selection = multi_select_items(
        items,
        title="Select coding documentation bundles to expose (optional; q cancel)",
    )
    if selection is None:
        print("Cancelled.")
        sys.exit(130)

    chosen: List[DocumentationBundle] = []
    for value in selection:
        if isinstance(value, tuple) and len(value) == 4:
            title, path, readme, contents = value
            bundle = DocumentationBundle(
                name=title,
                root=path if isinstance(path, Path) else Path(path),
                readme=readme if isinstance(readme, Path) else (Path(readme) if readme else None),
                contents=contents if isinstance(contents, Path) else (Path(contents) if contents else None),
            )
            chosen.append(bundle)
    return chosen


def _format_session_label(summary: SessionSummary) -> str:
    created_local = summary.created.astimezone()
    updated_local = summary.updated.astimezone()
    preview = summary.preview.replace("\n", " ").strip()
    if not preview:
        preview = "(no content recorded)"
    preview = textwrap.shorten(preview, width=80, placeholder="…")
    return (
        f"Created {created_local:%Y-%m-%d %H:%M} | "
        f"Updated {updated_local:%H:%M} | "
        f"{preview}"
    )


def _session_log_root() -> Path:
    return Path.home() / ".codex" / "sessions"


def _find_session_log(session_id: str, *, created: Optional[datetime], updated: Optional[datetime]) -> Optional[Path]:
    root = _session_log_root()
    candidate_dirs: List[Path] = []
    for dt in (updated, created):
        if not dt:
            continue
        utc_dt = dt.astimezone(timezone.utc)
        candidate_dirs.append(root / f"{utc_dt.year:04d}" / f"{utc_dt.month:02d}" / f"{utc_dt.day:02d}")
    candidate_dirs.append(root)

    for directory in candidate_dirs:
        if not directory.exists():
            continue
        try:
            return next(directory.rglob(f"*{session_id}.jsonl"))
        except StopIteration:
            continue
    return None


def _session_cwd_from_log(log_path: Path) -> Optional[Path]:
    try:
        with log_path.open("r", encoding="utf-8") as fh:
            for raw_line in fh:
                try:
                    entry = json.loads(raw_line)
                except json.JSONDecodeError:
                    continue
                if entry.get("type") != "session_meta":
                    continue
                payload = entry.get("payload") or {}
                cwd_value = payload.get("cwd")
                if not cwd_value:
                    return None
                return Path(cwd_value)
    except OSError:
        return None
    return None


def _load_session_summaries(history_path: Path, *, limit: int = 30, cwd_filter: Optional[Path] = None) -> List[SessionSummary]:
    if not history_path.is_file():
        return []
    sessions: Dict[str, SessionSummary] = {}
    try:
        with history_path.open("r", encoding="utf-8") as fh:
            for raw_line in fh:
                line = raw_line.strip()
                if not line:
                    continue
                try:
                    entry = json.loads(line)
                except json.JSONDecodeError:
                    continue
                session_id = entry.get("session_id")
                ts = entry.get("ts")
                if not session_id or ts is None:
                    continue
                try:
                    timestamp = datetime.fromtimestamp(ts, timezone.utc)
                except (OverflowError, OSError):
                    continue
                text = (entry.get("text") or "").strip()
                summary = sessions.get(session_id)
                if summary is None:
                    sessions[session_id] = SessionSummary(
                        session_id=session_id,
                        created=timestamp,
                        updated=timestamp,
                        preview=text,
                    )
                else:
                    if timestamp < summary.created:
                        summary.created = timestamp
                        if text:
                            summary.preview = text
                    if timestamp > summary.updated:
                        summary.updated = timestamp
                    if not summary.preview and text:
                        summary.preview = text
    except OSError:
        return []
    sorted_sessions = sorted(sessions.values(), key=lambda s: s.updated, reverse=True)[:limit]

    if cwd_filter is None:
        return sorted_sessions

    normalized_cwd = cwd_filter.resolve()
    filtered: List[SessionSummary] = []
    for summary in sorted_sessions:
        log_path = _find_session_log(summary.session_id, created=summary.created, updated=summary.updated)
        if not log_path:
            continue
        session_cwd = _session_cwd_from_log(log_path)
        if not session_cwd:
            continue
        try:
            if session_cwd.resolve() != normalized_cwd:
                continue
        except OSError:
            continue
        summary.cwd = session_cwd
        filtered.append(summary)
    return filtered


def _select_resume_session(history_path: Path, *, cwd: Optional[Path] = None) -> Optional[SessionSummary]:
    summaries = _load_session_summaries(history_path, cwd_filter=cwd)
    if not summaries:
        return None

    summary_map = {summary.session_id: summary for summary in summaries}
    options: List[Option] = [Option("Recent sessions", is_header=True)]
    for summary in summaries:
        options.append(Option(_format_session_label(summary), summary.session_id))

    chosen = multi_select_items(
        options,
        title="Select a session to resume (optional; leave blank for a new session)",
    )
    if chosen is None:
        print("Cancelled.")
        sys.exit(130)
    if not chosen:
        return None

    chosen_id: Optional[str] = None
    for value in chosen:
        if isinstance(value, str):
            chosen_id = value
            break
    if not chosen_id:
        return None
    return summary_map.get(chosen_id)


def _build_prompt_parts(
    *,
    yolo_enabled: bool,
    file_paths: List[Path],
    global_prompts: List[Path],
    doc_bundles: List[DocumentationBundle],
    selected_servers: Dict[str, Dict[str, object]],
    prompts_dir: Path,
    cwd: Path,
    include_mcp_prompts: bool = True,
) -> List[str]:
    parts: List[str] = []
    if yolo_enabled:
        parts.append(YOLO_PROMPT)
    for path in global_prompts:
        parts.append(read_global_prompt(path))
    for bundle in doc_bundles:
        parts.append(build_documentation_prompt(bundle.name, bundle.root))
    for path in file_paths:
        parts.append(build_file_prompt(path, cwd))
    if include_mcp_prompts:
        for server, cfg in selected_servers.items():
            prompt = build_mcp_prompt(prompts_dir, server)
            if prompt:
                parts.append(prompt)
            preprompt_path = cfg.get("_preprompt_path")
            if isinstance(preprompt_path, str):
                try:
                    preprompt_text = Path(preprompt_path).read_text(encoding="utf-8").strip()
                    if preprompt_text:
                        parts.append(preprompt_text)
                except OSError:
                    pass
            if server == "smysl_mcp":
                parts.append(
                    "Для smysl_mcp не указывай timeoutSeconds/timeout в tool-calls; фиксированный таймаут 600 секунд настроен на стороне клиента."
                )
    return parts


def run_plain(argv: List[str], adapter: CLIAdapter = CODEX_CLI_ADAPTER) -> int:
    cwd = Path.cwd()

    config_path = get_config_path()
    try:
        config = load_config(config_path)
        available_servers = fetch_mcp_servers(config)
        local_servers, missing_local = discover_self_mcp_servers(cwd)
        project_servers = discover_project_mcp_servers(cwd)
        if missing_local:
            for path in missing_local:
                print(
                    f"[warning] self_mcp_servers entry without mcp.json: {path.relative_to(cwd)} (skipped)",
                    file=sys.stderr,
                )
        available_servers = merge_mcp_servers(available_servers, local_servers)
        available_servers = merge_mcp_servers(available_servers, project_servers)
        attach_preprompts_from_command_paths(available_servers)
    except ConfigError as err:
        print(err, file=sys.stderr)
        return 1

    model, effort = _current_model_settings(config)
    if model not in MODEL_REASONING_OPTIONS and model not in MODEL_DISPLAY_ORDER:
        model = DEFAULT_MODEL
        effort = MODEL_DEFAULT_EFFORT.get(model, DEFAULT_REASONING)
    if model in MODEL_REASONING_OPTIONS:
        valid_efforts = {value for _, value, _ in MODEL_REASONING_OPTIONS[model]}
        if effort not in valid_efforts:
            effort = MODEL_DEFAULT_EFFORT.get(model, DEFAULT_REASONING)
    llm_selection = LLMSelection(
        display_model=model,
        detail=_reasoning_label(model, effort),
        provider_slug="openai",
        model=model,
        reasoning_effort=effort,
        requires_provider_override=False,
    )
    codex_args: List[str] = []
    codex_args.extend(adapter.build_llm_args(llm_selection))
    codex_args.extend(adapter.build_execution_args(ExecutionPreferences(dangerous_sandbox=True, auto_approve=True)))
    codex_args.extend(argv)

    selected_servers: Dict[str, Dict[str, object]] = {}
    if "telegram_notify" in available_servers:
        selected_servers["telegram_notify"] = available_servers["telegram_notify"]

    option_args = [*codex_args, *adapter.build_mcp_flags(selected_servers, available_servers)]
    codex_cmd = [adapter.command, *option_args]

    print("Launching Codex (plain mode)")
    print(f"Working directory: {cwd}")
    print(f"Model: {model} — {_reasoning_label(model, effort)}")
    print("MCP enable map:", {name: (name in selected_servers) for name in available_servers})
    print("Command:", codex_cmd)

    env = os.environ.copy()
    adapter.prepare_env(env, llm_selection)
    _apply_proxy_env(env, _resolve_proxy_env(cwd))
    return subprocess.call(codex_cmd, env=env)


def run(
    argv: List[str],
    adapter: CLIAdapter = CODEX_CLI_ADAPTER,
    *,
    preset_override: Optional[str] = None,
) -> int:
    cwd = Path.cwd()
    base_prompts_dir = Path(__file__).resolve().parent.parent / "codex_prompts"
    global_prompts_dir = base_prompts_dir / "global"

    config_path = get_config_path()
    try:
        config = load_config(config_path)
        available_servers = fetch_mcp_servers(config)
        local_servers, missing_local = discover_self_mcp_servers(cwd)
        project_servers = discover_project_mcp_servers(cwd)
        if missing_local:
            for path in missing_local:
                try:
                    rel = path.relative_to(cwd)
                except ValueError:
                    rel = path
                print(
                    f"[warning] self_mcp_servers entry without mcp.json: {rel} (skipped)",
                    file=sys.stderr,
                )
        available_servers = merge_mcp_servers(available_servers, local_servers)
        available_servers = merge_mcp_servers(available_servers, project_servers)
        attach_preprompts_from_command_paths(available_servers)
    except ConfigError as err:
        print(err, file=sys.stderr)
        return 1

    preset_choice = preset_override if preset_override is not None else _select_preset()
    self_mcp_names = [name for name, cfg in available_servers.items() if cfg.get("_source") == "self_mcp"]
    # Hard-limit smysl_mcp timeouts to avoid 60s tool-call cutoff.
    if "smysl_mcp" in available_servers:
        cfg = available_servers["smysl_mcp"]
        cfg["timeout"] = 600
        cfg["startup_timeout_sec"] = cfg.get("startup_timeout_sec") or 30
    use_custom_flow = preset_choice == PRESET_NONE
    target_mcp_name: Optional[str] = None
    target_mcp_dir: Optional[Path] = None
    orchestrator_brief_src: Optional[Path] = None
    orchestrator_brief_dst: Optional[Path] = None

    if preset_choice == PRESET_CREATE_PROJECT_MCP:
        target_mcp_name = _select_target_mcp_name(self_mcp_names)
        if not target_mcp_name:
            print("Не указано имя MCP-сервера — переключаемся на Custom flow.")
            preset_choice = PRESET_NONE
            use_custom_flow = True
        else:
            target_mcp_dir = cwd / "self_mcp_servers" / target_mcp_name
            try:
                target_mcp_dir.mkdir(parents=True, exist_ok=True)
            except OSError as exc:
                print(f"Не удалось создать {target_mcp_dir}: {exc}. Переключаемся на Custom flow.")
                preset_choice = PRESET_NONE
                use_custom_flow = True
            else:
                orchestrator_brief_src = _locate_orchestrator_brief(cwd, global_prompts_dir)
                if orchestrator_brief_src:
                    orchestrator_brief_dst = _copy_orchestrator_brief(orchestrator_brief_src, target_mcp_dir)

    if adapter.supports_llm_selection:
        if use_custom_flow:
            llm_selection, llm_env = _select_llm_mode(config)
        else:
            llm_env = _load_llm_env()
            default_model = DEFAULT_MODEL
            default_effort = MODEL_DEFAULT_EFFORT.get(default_model, DEFAULT_REASONING)
            llm_selection = LLMSelection(
                display_model=default_model,
                detail=_reasoning_label(default_model, default_effort),
                provider_slug="openai",
                model=default_model,
                reasoning_effort=default_effort,
                requires_provider_override=False,
            )
    else:
        llm_env = _load_llm_env()
        llm_selection = LLMSelection(
            display_model="Crush",
            detail="выберите модель внутри Crush",
            provider_slug="",
            model="",
            reasoning_effort=None,
            requires_provider_override=False,
        )
    reasoning_label = llm_selection.detail

    if use_custom_flow:
        execution_prefs, yolo_enabled, disable_mcp_preprompts = _select_execution_preferences()
    else:
        dangerous_sandbox = True
        if preset_choice in (PRESET_FIXER, PRESET_OVERSEER):
            dangerous_sandbox = False
        execution_prefs = ExecutionPreferences(dangerous_sandbox=dangerous_sandbox, auto_approve=True)
        yolo_enabled = False
        disable_mcp_preprompts = False

    codex_args: List[str] = []
    codex_args.extend(adapter.build_llm_args(llm_selection))
    codex_args.extend(adapter.build_execution_args(execution_prefs))
    codex_args.extend(argv)

    selected_servers: Dict[str, Dict[str, object]] = {}
    selected_config_paths: Dict[str, Path] = {}
    selected_doc_bundles: List[DocumentationBundle] = []
    selected_files: List[Path] = []
    selected_global_paths: List[Path] = []
    doc_context_files: List[Path] = []

    if adapter.supports_mcp:
        if preset_choice == PRESET_FAST_RESEARCH:
            selected_servers = _filter_available_servers(available_servers, ("google_search", "telegram_notify"))
        elif preset_choice == PRESET_CREATE_PROJECT_MCP:
            selected_servers = _filter_available_servers(available_servers, ("dart_flutter", "telegram_notify"))
            for name in self_mcp_names:
                if name in available_servers:
                    selected_servers[name] = available_servers[name]
        elif preset_choice == PRESET_SIMPLE_CHAT:
            selected_servers = {}
        elif preset_choice == PRESET_CODING:
            selected_servers = _select_mcp_servers(
                available_servers,
                cwd=cwd,
                allowed_servers=CODING_PRESET_ALLOWED_SERVERS,
            )
            selected_config_paths = _collect_config_overrides(selected_servers, cwd=cwd)
            if "sqlite" in selected_servers and "sqlite" not in selected_config_paths:
                sqlite_config_path = _ensure_sqlite_scaffold(cwd)
                if sqlite_config_path is None:
                    selected_servers.pop("sqlite", None)
                else:
                    selected_config_paths["sqlite"] = sqlite_config_path
        elif preset_choice in (PRESET_FIXER, PRESET_NETRUNNER, PRESET_OVERSEER):
            selected_servers = _select_role_preset_servers(available_servers, cwd=cwd)
            selected_config_paths = _collect_config_overrides(selected_servers, cwd=cwd)
            if "sqlite" in selected_servers and "sqlite" not in selected_config_paths:
                sqlite_config_path = _ensure_sqlite_scaffold(cwd)
                if sqlite_config_path is None:
                    selected_servers.pop("sqlite", None)
                else:
                    selected_config_paths["sqlite"] = sqlite_config_path
        else:
            selected_servers = _select_mcp_servers(available_servers, cwd=cwd)
            selected_config_paths = _collect_config_overrides(selected_servers, cwd=cwd)
            if "sqlite" in selected_servers and "sqlite" not in selected_config_paths:
                sqlite_config_path = _ensure_sqlite_scaffold(cwd)
                if sqlite_config_path is None:
                    selected_servers.pop("sqlite", None)
                else:
                    selected_config_paths["sqlite"] = sqlite_config_path
    else:
        if available_servers:
            print("[open-codex] MCP конфигурация будет пропущена — CLI не поддерживает MCP-серверы.")

    if preset_choice == PRESET_CODING:
        selected_files = _select_project_book_files(cwd)
    elif use_custom_flow:
        selected_doc_bundles = _select_doc_bundles(cwd)
        selected_files = _select_files(cwd)
        for bundle in selected_doc_bundles:
            for extra in (bundle.readme, bundle.contents):
                if extra and extra.exists():
                    doc_context_files.append(extra)
        selected_files = _merge_unique_paths(doc_context_files, selected_files)
    # Presets fast paths skip file/doc selection

    extra_notes: List[str] = []
    if preset_choice == PRESET_CREATE_PROJECT_MCP:
        preset_prompt = _find_global_prompt_by_stem(
            global_prompts_dir,
            ("create project mcp server dart", "create project mcp server"),
        )
        if preset_prompt:
            selected_global_paths = [preset_prompt]
        # Always append human runbook note for project MCP creation
        extra_notes.append(HUMAN_RUNBOOK_NOTE)
    elif preset_choice in (PRESET_FIXER, PRESET_NETRUNNER, PRESET_OVERSEER):
        role_map = {
            PRESET_FIXER: "start-fixer",
            PRESET_NETRUNNER: "start-netrunner",
            PRESET_OVERSEER: "start-overseer",
        }
        skill_name = role_map[preset_choice]
        extra_notes.append(
            textwrap.dedent(
                f"""\
                Activate skill `${skill_name}` immediately.
                Execute only its initialization checklist first, then stop and report status.
                """
            ).strip()
        )
    elif use_custom_flow:
        global_candidates = load_global_prompts(global_prompts_dir)
        if global_candidates:
            # Rename specific global prompts for display
            rename_map = {
                "configure universal clickhouse mcp": "ClickHouse MCP initialization",
                "configure universal postgres mcp": "Postgres MCP initialization",
                "setup mcp server": "Create new MCP server",
                "project orchestrator": "Project Orchestrator",
                "neuro assistant": "Neuro Assistant",
                "framer mcp": "Framer MCP bridge",
                "documentation cleanup orchestrator": "Documentation Cleanup Orchestrator",
                "coding docs snapshot helper": "Coding Docs Snapshot Helper",
            }
            names = [rename_map.get(name, name) for name, _ in global_candidates]
            paths = {rename_map.get(name, name): path for name, path in global_candidates}
            selection = multi_select(
                names,
                title="Select global prompts to include (space toggle, enter confirm, a toggle all, q cancel)",
            )
            if selection is None:
                print("Cancelled.")
                sys.exit(130)
            selected_global_paths = [paths[name] for name in selection]

    history_path = Path.home() / ".codex" / "history.jsonl"
    resume_summary: Optional[SessionSummary] = None
    if adapter.supports_resume and use_custom_flow:
        resume_summary = _select_resume_session(history_path, cwd=cwd)

    # If we are resuming a session, we usually want to avoid re-injecting MCP preprompts
    # that are likely already present in the session context.
    should_include_mcp_prompts = (not disable_mcp_preprompts) and (resume_summary is None)

    prompt_parts = _build_prompt_parts(
        yolo_enabled=yolo_enabled,
        file_paths=selected_files,
        global_prompts=selected_global_paths,
        doc_bundles=selected_doc_bundles,
        selected_servers=selected_servers,
        prompts_dir=base_prompts_dir,
        cwd=cwd,
        include_mcp_prompts=should_include_mcp_prompts,
    )
    if extra_notes:
        prompt_parts.extend(extra_notes)

    if preset_choice == PRESET_CREATE_PROJECT_MCP and target_mcp_name and target_mcp_dir:
        tz_target = orchestrator_brief_dst or orchestrator_brief_src
        if tz_target:
            try:
                rel_tz = tz_target.relative_to(cwd)
                tz_display = str(rel_tz)
            except ValueError:
                tz_display = str(tz_target)
            tz_note = (
                f"Оркестратор оставил ТЗ: {tz_display}. "
                "Прочитай перед изменениями и следуй требованиям."
            )
        else:
            tz_note = (
                "ТЗ от Оркестратора не найдено (ожидается mcp_orchestrator_brief.md "
                "в корне или project_book). Остановись и запроси файл у Оркестратора."
            )
        prompt_parts.append(
            textwrap.dedent(
                f"""\
                Целевой MCP-сервер: `{target_mcp_name}`.
                Рабочая директория: `{target_mcp_dir}`.
                {tz_note}
                Если ТЗ отсутствует — не начинай разработку, запроси ТЗ у Оркестратора.
                """
            ).strip()
        )

    final_prompt = compose_prompt(prompt_parts)

    option_args = [*codex_args, *adapter.build_mcp_flags(selected_servers, available_servers)]
    if resume_summary:
        codex_cmd = [adapter.command, "resume", *option_args, resume_summary.session_id]
    else:
        codex_cmd = [adapter.command, *option_args]

    enable_map = {name: (name in selected_servers) for name in available_servers}
    if resume_summary:
        created_local = resume_summary.created.astimezone().strftime("%Y-%m-%d %H:%M")
        updated_local = resume_summary.updated.astimezone().strftime("%Y-%m-%d %H:%M")
        print(
            f"Resuming Codex session {resume_summary.session_id} "
            f"(created {created_local}, updated {updated_local})"
        )
    else:
        print("Launching Codex with MCP servers:", ", ".join(selected_servers.keys()) or "none")
    print(f"Model: {llm_selection.display_model} — {reasoning_label}")
    print("MCP enable map:", enable_map)
    if selected_config_paths:
        printable = {server: str(path) for server, path in selected_config_paths.items()}
        print("MCP config overrides:", printable)
    if selected_doc_bundles:
        doc_map = {bundle.name: str(bundle.root) for bundle in selected_doc_bundles}
        print("Coding documentation bundles:", doc_map)
    print("Command:", codex_cmd)

    if final_prompt:
        if adapter.supports_prompt:
            codex_cmd.extend(adapter.build_prompt_args(final_prompt))
        else:
            _notify_external_prompt(final_prompt)

    env = _merge_env_with_os(llm_env)
    adapter.prepare_env(env, llm_selection)
    _apply_proxy_env(env, _resolve_proxy_env(cwd))
    for server_key, path in selected_config_paths.items():
        env_var = CONFIG_ENV_VARS.get(server_key)
        if not env_var:
            continue
        env[env_var] = str(path)

    return subprocess.call(codex_cmd, env=env)


def run_fixer(argv: List[str], adapter: CLIAdapter = CODEX_CLI_ADAPTER) -> int:
    role_preset = _select_role_preset()
    return run(argv, adapter=adapter, preset_override=role_preset)


def _notify_external_prompt(prompt: str) -> None:
    copied = _copy_to_clipboard(prompt)
    if copied:
        print("Crush не принимает стартовый промпт через аргументы. Текст скопирован в буфер обмена — вставьте его вручную после запуска (Cmd+V).\n")
    else:
        print("Crush не принимает стартовый промпт через аргументы. Скопируйте текст ниже и вставьте вручную после запуска:\n")
        print(prompt)


def _copy_to_clipboard(text: str) -> bool:
    pbcopy = shutil.which("pbcopy")
    if not pbcopy:
        return False
    try:
        proc = subprocess.run([pbcopy], input=text.encode("utf-8"), check=True)
    except (subprocess.CalledProcessError, OSError):
        return False
    return proc.returncode == 0
