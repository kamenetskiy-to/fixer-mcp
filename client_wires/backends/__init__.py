from __future__ import annotations

from typing import Any

from .base import DEFAULT_BACKEND, BackendAdapter, BackendDescriptor, normalize_backend_name
from .catalog import load_backend_entry
from .claude_adapter import ClaudeCodeBackendAdapter
from .codex_adapter import CodexBackendAdapter
from .droid_adapter import DroidBackendAdapter


SUPPORTED_BACKENDS = ("codex", "droid", "claude")


def available_backend_descriptors() -> list[BackendDescriptor]:
    descriptors: list[BackendDescriptor] = []
    for name in SUPPORTED_BACKENDS:
        entry = load_backend_entry(name)
        descriptors.append(
            BackendDescriptor(
                name=name,
                label=str(entry["label"]),
                description=str(entry["description"]),
                default_model=str(entry["default_model"]),
                default_reasoning=str(entry["default_reasoning"]),
                model_options=tuple(str(value) for value in entry["model_options"]),
                reasoning_options=tuple(str(value) for value in entry["reasoning_options"]),
                fresh_launch_supported=bool(entry.get("fresh_launch_supported", True)),
                resume_supported=bool(entry.get("resume_supported", True)),
            )
        )
    return descriptors


def get_backend_adapter(name: str | None, *, codex_adapter: Any) -> BackendAdapter:
    normalized = normalize_backend_name(name)
    if normalized == "codex":
        return CodexBackendAdapter(codex_adapter)
    if normalized == "droid":
        return DroidBackendAdapter()
    if normalized == "claude":
        return ClaudeCodeBackendAdapter()
    supported = ", ".join(SUPPORTED_BACKENDS)
    raise RuntimeError(f"Unsupported CLI backend {name!r}. Supported backends: {supported}")


__all__ = [
    "DEFAULT_BACKEND",
    "SUPPORTED_BACKENDS",
    "BackendAdapter",
    "BackendDescriptor",
    "available_backend_descriptors",
    "get_backend_adapter",
    "normalize_backend_name",
]
