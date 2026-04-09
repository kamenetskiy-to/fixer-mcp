from __future__ import annotations

import os
import shutil
from abc import ABC, abstractmethod
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Mapping, Sequence

DEFAULT_BACKEND = "codex"


@dataclass(frozen=True)
class BackendDescriptor:
    name: str
    label: str
    description: str
    default_model: str
    default_reasoning: str
    model_options: tuple[str, ...]
    reasoning_options: tuple[str, ...]
    fresh_launch_supported: bool = True
    resume_supported: bool = True


def normalize_backend_name(raw: str | None) -> str:
    normalized = (raw or "").strip().lower()
    if not normalized:
        return DEFAULT_BACKEND
    return normalized


def normalize_mcp_server_for_factory(source: Mapping[str, object]) -> dict[str, object]:
    if "url" in source:
        payload: dict[str, object] = {
            "type": "http",
            "url": source["url"],
            "disabled": bool(source.get("disabled", False)),
        }
        headers = source.get("headers")
        if isinstance(headers, dict) and headers:
            payload["headers"] = dict(headers)
        return payload

    payload = {
        "type": "stdio",
        "command": str(source.get("command", "")).strip(),
        "disabled": bool(source.get("disabled", False)),
    }
    args = source.get("args")
    if isinstance(args, (list, tuple)):
        payload["args"] = [str(item) for item in args]
    env = source.get("env")
    if isinstance(env, dict) and env:
        payload["env"] = {str(key): str(value) for key, value in env.items()}
    return payload


def materialize_factory_skills(cwd: Path, skill_names: Sequence[str]) -> None:
    skill_root = cwd / ".factory" / "skills"
    skill_root.mkdir(parents=True, exist_ok=True)
    codex_home = os.environ.get("CODEX_HOME", "").strip()
    candidate_roots = [
        Path(codex_home).expanduser() / "skills" if codex_home else None,
        Path.home() / ".codex" / "skills",
        cwd / ".codex" / "skills",
    ]
    for skill_name in skill_names:
        normalized_name = skill_name.strip()
        if not normalized_name:
            continue
        source_dir: Path | None = None
        for root in candidate_roots:
            if root is None:
                continue
            candidate = root / normalized_name
            if (candidate / "SKILL.md").is_file():
                source_dir = candidate
                break
        if source_dir is None:
            continue
        destination = skill_root / normalized_name
        shutil.rmtree(destination, ignore_errors=True)
        shutil.copytree(source_dir, destination)


class BackendAdapter(ABC):
    descriptor: BackendDescriptor
    command: str
    supports_resume: bool

    @property
    def name(self) -> str:
        return self.descriptor.name

    @property
    def default_model(self) -> str:
        return self.descriptor.default_model

    @property
    def default_reasoning(self) -> str:
        return self.descriptor.default_reasoning

    @property
    def model_options(self) -> tuple[str, ...]:
        return self.descriptor.model_options

    @property
    def reasoning_options(self) -> tuple[str, ...]:
        return self.descriptor.reasoning_options

    def normalize_model(self, model: str | None) -> str:
        candidate = (model or "").strip() or self.default_model
        if candidate not in self.model_options:
            supported = ", ".join(self.model_options)
            raise RuntimeError(
                f"Unsupported model {candidate!r} for backend {self.name!r}. Supported models: {supported}"
            )
        return candidate

    def normalize_reasoning(self, reasoning: str | None) -> str:
        candidate = (reasoning or "").strip() or self.default_reasoning
        if candidate not in self.reasoning_options:
            supported = ", ".join(self.reasoning_options)
            raise RuntimeError(
                f"Unsupported reasoning {candidate!r} for backend {self.name!r}. Supported reasoning values: {supported}"
            )
        return candidate

    @abstractmethod
    def build_llm_args(self, selection: Any) -> list[str]:
        """Translate LLM selection into CLI args for interactive launches."""

    def build_execution_args(self, prefs: Any) -> list[str]:
        return []

    def build_interactive_execution_args(self, prefs: Any) -> list[str]:
        return self.build_execution_args(prefs)

    def build_mcp_flags(
        self,
        selected: Mapping[str, Mapping[str, object]],
        available: Mapping[str, Mapping[str, object]],
    ) -> list[str]:
        return []

    def build_prompt_args(self, prompt: str) -> list[str]:
        trimmed = prompt.strip()
        if not trimmed:
            return []
        return [trimmed]

    def prepare_env(self, env: dict[str, str], selection: Any) -> None:
        del env, selection
        return

    def ensure_runtime_files(
        self,
        cwd: Path,
        selection: Any,
        selected: Mapping[str, Mapping[str, object]],
        available: Mapping[str, Mapping[str, object]],
    ) -> None:
        del cwd, selection, selected, available
        return

    @abstractmethod
    def build_resume_command(self, option_args: Sequence[str], external_session_id: str) -> list[str]:
        """Build the backend-specific resume command."""

    @abstractmethod
    def build_headless_command(
        self,
        *,
        model: str,
        reasoning: str,
        selected: Mapping[str, Mapping[str, object]],
        available: Mapping[str, Mapping[str, object]],
        prompt: str,
    ) -> list[str]:
        """Build the backend-specific detached/headless command."""
