from __future__ import annotations

from dataclasses import dataclass
from typing import Mapping

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
    supports_headless: bool = True


class BackendAdapter:
    descriptor: BackendDescriptor

    @property
    def name(self) -> str:
        return self.descriptor.name

    def normalize_model(self, model: str | None) -> str:
        return (model or "").strip() or self.descriptor.default_model

    def normalize_reasoning(self, reasoning: str | None) -> str:
        return (reasoning or "").strip() or self.descriptor.default_reasoning

    def build_headless_command(
        self,
        *,
        model: str,
        reasoning: str,
        prompt: str,
        selected_mcp_servers: Mapping[str, Mapping[str, object]],
    ) -> list[str]:
        raise NotImplementedError

    def runtime_side_effects(
        self,
        *,
        selected_mcp_servers: Mapping[str, Mapping[str, object]],
    ) -> list[str]:
        del selected_mcp_servers
        return []


def normalize_backend_name(raw: str | None) -> str:
    normalized = (raw or "").strip().lower()
    return normalized or DEFAULT_BACKEND
