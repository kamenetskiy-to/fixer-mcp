from __future__ import annotations

from typing import Any

from .base import DEFAULT_BACKEND, BackendAdapter, BackendDescriptor, normalize_backend_name
from .codex_adapter import CodexBackendAdapter
from .droid_adapter import DroidBackendAdapter


SUPPORTED_BACKENDS = ("codex", "droid")


def available_backend_descriptors() -> list[BackendDescriptor]:
    return [CodexBackendAdapter.descriptor, DroidBackendAdapter.descriptor]


def get_backend_adapter(name: str | None, *, codex_adapter: Any) -> BackendAdapter:
    normalized = normalize_backend_name(name)
    if normalized == "codex":
        return CodexBackendAdapter(codex_adapter)
    if normalized == "droid":
        return DroidBackendAdapter()
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
