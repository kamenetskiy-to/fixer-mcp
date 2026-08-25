from __future__ import annotations

from typing import Any

from .base import DEFAULT_BACKEND, BackendAdapter, BackendDescriptor, is_codex_backend, normalize_backend_name
from .antigravity_adapter import AntigravityBackendAdapter
from .catalog import is_backend_available, load_backend_entry, set_backend_availability
from .claude_adapter import ClaudeCodeBackendAdapter
from .codex_adapter import CodexBackendAdapter
from .commandcode_adapter import CommandCodeBackendAdapter
from .droid_adapter import DroidBackendAdapter
from .grok_adapter import GrokBackendAdapter
from .junie_adapter import JunieBackendAdapter
from .kimi_code_adapter import KimiCodeBackendAdapter


SUPPORTED_BACKENDS = ("codex", "commandcode", "droid", "claude", "antigravity", "junie", "kimi-code", "grok")


def available_backend_descriptors() -> list[BackendDescriptor]:
    """Every structurally supported backend, regardless of current subscription/auth status.

    Kept name/behavior unchanged for existing callers that need to resolve
    any backend by name (resume flows, label lookups, launch config). For
    "what should the operator be offered to pick right now", use
    `subscribed_backend_descriptors()` instead.
    """
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
                available=bool(entry.get("available", True)),
            )
        )
    return descriptors


def subscribed_backend_descriptors() -> list[BackendDescriptor]:
    """Backends the Architect currently has a working subscription/auth for.

    This is what interactive NEW-launch pickers should show — it filters out
    backends flagged unavailable in backend-catalog.json (quota exhausted,
    auth broken, etc.) so the operator doesn't pick a dead end. Falls back to
    the full list if every backend is somehow flagged unavailable, so the
    picker never goes empty. Do not use this for resume flows or name-based
    descriptor lookups — those need every structurally supported backend.
    """
    descriptors = [descriptor for descriptor in available_backend_descriptors() if descriptor.available]
    return descriptors or available_backend_descriptors()


def get_backend_adapter(name: str | None, *, codex_adapter: Any) -> BackendAdapter:
    normalized = normalize_backend_name(name)
    if normalized == "codex":
        return CodexBackendAdapter(codex_adapter)
    if normalized == "commandcode":
        return CommandCodeBackendAdapter()
    if normalized == "droid":
        return DroidBackendAdapter()
    if normalized == "claude":
        return ClaudeCodeBackendAdapter()
    if normalized == "antigravity":
        return AntigravityBackendAdapter()
    if normalized == "junie":
        return JunieBackendAdapter()
    if normalized == "kimi-code":
        return KimiCodeBackendAdapter()
    if normalized == "grok":
        return GrokBackendAdapter()
    supported = ", ".join(SUPPORTED_BACKENDS)
    raise RuntimeError(f"Unsupported CLI backend {name!r}. Supported backends: {supported}")


__all__ = [
    "DEFAULT_BACKEND",
    "SUPPORTED_BACKENDS",
    "BackendAdapter",
    "BackendDescriptor",
    "available_backend_descriptors",
    "get_backend_adapter",
    "is_backend_available",
    "is_codex_backend",
    "normalize_backend_name",
    "set_backend_availability",
    "subscribed_backend_descriptors",
]
