from __future__ import annotations

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


def normalize_backend_name(raw: str | None) -> str:
    normalized = (raw or "").strip().lower()
    if not normalized:
        return DEFAULT_BACKEND
    return normalized


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

    @abstractmethod
    def build_llm_args(self, selection: Any) -> list[str]:
        """Translate LLM selection into CLI args for interactive launches."""

    def build_execution_args(self, prefs: Any) -> list[str]:
        return []

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
        selected: Mapping[str, Mapping[str, object]],
        available: Mapping[str, Mapping[str, object]],
    ) -> None:
        del cwd, selected, available
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
