"""LLM selection models, environment loading, and Codex CLI adapter."""

from __future__ import annotations

import os
from abc import ABC, abstractmethod
from dataclasses import dataclass
from pathlib import Path
from typing import Dict, List, Optional, Tuple


CONFIG_ENV_VARS = {
    "postgres": "POSTGRES_MCP_CONFIG_PATH",
    "clickhouse": "CLICKHOUSE_MCP_CONFIG_PATH",
    "sqlite": "SQLITE_MCP_CONFIG_PATH",
    "firebase_mcp": "FIREBASE_MCP_CONFIG_PATH",
}

MODEL_REASONING_OPTIONS: Dict[str, List[Tuple[str, str, str]]] = {
    "gpt-5.5": [
        ("Minimal", "minimal", "Fastest responses with little reasoning"),
        ("Low", "low", "Balances speed with some reasoning; useful for straightforward queries and short explanations"),
        ("Medium", "medium", "Provides a solid balance of reasoning depth and latency for general-purpose tasks"),
        ("High", "high", "Maximizes reasoning depth for complex or ambiguous problems"),
    ],
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
    "gpt-5.5": "high",
    "gpt-5.4": "high",
    "gpt-5.3-codex": "medium",
    "gpt-5.3-codex-spark": "medium",
    "gpt-5.2": "medium",
}

DEFAULT_MODEL = "gpt-5.5"
DEFAULT_REASONING = MODEL_DEFAULT_EFFORT[DEFAULT_MODEL]
LLM_ENV_PATH = Path.home() / ".codex" / "llm.env"


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


def reasoning_label(model: str, effort: str) -> str:
    for label, key, _ in MODEL_REASONING_OPTIONS.get(model, []):
        if key == effort:
            return label
    return effort.title()


def load_llm_env() -> Dict[str, str]:
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


def merge_env_with_os(loaded: Dict[str, str]) -> Dict[str, str]:
    env = os.environ.copy()
    env.update(loaded)
    return env


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


def dynamic_mcp_overrides(name: str, config: Dict[str, object]) -> List[str]:
    if config.get("_source") not in {"self_mcp", "project_mcp", "preset_mcp"}:
        return []

    overrides: List[str] = []
    for field in ("command", "args", "env", "transport", "cwd", "startup_timeout_sec", "timeout", "tool_timeout_sec"):
        if field in config:
            overrides.extend(["-c", _toml_override(f"mcp_servers.{name}.{field}", config[field])])
    return overrides


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
            overrides.extend(dynamic_mcp_overrides(name, cfg))
        return overrides

    def prepare_env(self, env: Dict[str, str], selection: LLMSelection) -> None:  # pragma: no cover
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
        if selection.model == "gpt-5.5":
            args.extend([
                "-c", "model_context_window=800000",
                "-c", "model_auto_compact_token_limit=736000",
            ])
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


CODEX_CLI_ADAPTER = CodexCLIAdapter()

_reasoning_label = reasoning_label
_load_llm_env = load_llm_env
_merge_env_with_os = merge_env_with_os
_dynamic_mcp_overrides = dynamic_mcp_overrides

