from __future__ import annotations

from .base import DEFAULT_BACKEND, BackendAdapter, BackendDescriptor, normalize_backend_name
from .codex import CodexBackendAdapter
from .droid import DroidBackendAdapter

_BACKENDS: dict[str, BackendAdapter] = {
    "codex": CodexBackendAdapter(),
    "droid": DroidBackendAdapter(),
}


def available_backend_descriptors() -> list[BackendDescriptor]:
    return [adapter.descriptor for adapter in _BACKENDS.values()]


def get_backend_adapter(name: str | None) -> BackendAdapter:
    normalized = normalize_backend_name(name)
    try:
        return _BACKENDS[normalized]
    except KeyError as exc:
        supported = ", ".join(sorted(_BACKENDS))
        raise RuntimeError(f"Unsupported CLI backend {name!r}. Supported backends: {supported}") from exc


__all__ = [
    "DEFAULT_BACKEND",
    "BackendAdapter",
    "BackendDescriptor",
    "available_backend_descriptors",
    "get_backend_adapter",
    "normalize_backend_name",
]
