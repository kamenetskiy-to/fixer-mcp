"""Launch-support helper implementations for the Fixer wire launcher."""

from __future__ import annotations

from dataclasses import dataclass
import re
from typing import Any, Callable, Sequence

from client_wires.backends import normalize_backend_name
from client_wires import fixer_wire_db

SessionRow = fixer_wire_db.SessionRow
SessionLaunchSelection = fixer_wire_db.SessionLaunchSelection


@dataclass(frozen=True)
class LaunchSelectionCallbacks:
    select_backend_interactive: Callable[..., str]
    select_model_interactive: Callable[..., str]
    select_reasoning_interactive: Callable[..., str]
    backend_descriptor: Callable[[str], Any]
    normalize_backend_model: Callable[[Any, str | None], str]
    normalize_backend_reasoning: Callable[[Any, str | None], str]


def _ensure_passthrough_dangerous_sandbox(passthrough_args: Sequence[str]) -> list[str]:
    args = list(passthrough_args)
    if "--sandbox" in args:
        return args
    return [*args, "--sandbox", "danger-full-access"]


def _is_codex_adapter(adapter: Any) -> bool:
    return normalize_backend_name(getattr(adapter, "name", "")) == "codex"


def _maybe_configure_playwright_runtime_mode(
    adapter: Any,
    selected_servers: dict[str, dict[str, object]],
    available_servers: dict[str, dict[str, object]],
    *,
    interactive: bool,
) -> str | None:
    if not _is_codex_adapter(adapter):
        return None
    if "playwright" not in selected_servers:
        return None

    try:
        from client_wires.codex_compat.runtime import _maybe_configure_playwright_runtime
    except ImportError:
        return None

    if not callable(_maybe_configure_playwright_runtime):
        return None
    return _maybe_configure_playwright_runtime(
        selected_servers,
        available_servers,
        interactive=interactive,
    )


def _is_computer_use_config_override(value: str) -> bool:
    normalized = re.sub(r"[\s\"']", "", value.strip().lower())
    if "computer-use" not in normalized and "computer_use" not in normalized and "computeruse" not in normalized:
        return False
    return (
        normalized.startswith("mcp_servers.")
        or normalized.startswith("mcpservers.")
    ) and (
        ".enabled=false" in normalized
        or ".enabled=true" in normalized
        or ".disabled=true" in normalized
        or ".disabled=false" in normalized
    )


def _strip_computer_use_overrides(args: Sequence[str]) -> list[str]:
    stripped: list[str] = []
    iterator = iter(range(len(args)))
    skip_indexes: set[int] = set()

    for index in iterator:
        if index in skip_indexes:
            continue
        arg = str(args[index])
        if arg in {"-c", "--config"} and index + 1 < len(args):
            next_arg = str(args[index + 1])
            if _is_computer_use_config_override(next_arg):
                skip_indexes.add(index + 1)
                continue
        if arg == "--enable" and index + 1 < len(args):
            next_arg = re.sub(r"[\s\"']", "", str(args[index + 1]).strip().lower())
            if next_arg in {"computer_use", "computer-use", "computeruse"}:
                skip_indexes.add(index + 1)
                continue
        if arg.startswith("--enable="):
            _, _, value = arg.partition("=")
            normalized = re.sub(r"[\s\"']", "", value.strip().lower())
            if normalized in {"computer_use", "computer-use", "computeruse"}:
                continue
        if arg.startswith("-c=") or arg.startswith("--config="):
            _, _, value = arg.partition("=")
            if _is_computer_use_config_override(value):
                continue
        stripped.append(arg)
    return stripped


def _append_codex_apps_gate(codex_args: list[str], adapter: Any, *, allow_computer_use: bool) -> list[str]:
    if not _is_codex_adapter(adapter):
        return codex_args
    if allow_computer_use:
        return [*_strip_computer_use_overrides(codex_args), "--disable", "apps", "--enable", "computer_use"]
    return [*_strip_computer_use_overrides(codex_args), "--disable", "apps"]


def _prefer_fixed_model_for_role_presets(
    codex_main: Any,
    *,
    fixer_wire_model: str,
    fixer_wire_reasoning_effort: str,
) -> None:
    order_raw = getattr(codex_main, "MODEL_DISPLAY_ORDER", [])
    if not isinstance(order_raw, list):
        return
    order = [str(item) for item in order_raw if str(item).strip()]
    preferred = [fixer_wire_model, *[item for item in order if item != fixer_wire_model]]
    setattr(codex_main, "MODEL_DISPLAY_ORDER", preferred)
    default_effort = getattr(codex_main, "MODEL_DEFAULT_EFFORT", None)
    if isinstance(default_effort, dict):
        default_effort[fixer_wire_model] = fixer_wire_reasoning_effort


def _resolve_netrunner_launch_selection(
    selected_session: SessionRow,
    *,
    preset_backend: str | None,
    preset_model: str | None,
    preset_reasoning: str | None,
    dry_run: bool,
    Option: Any,
    single_select_items: Any,
    callbacks: LaunchSelectionCallbacks,
) -> SessionLaunchSelection:
    started = bool(selected_session.external_session_id.strip())
    preferred_backend = normalize_backend_name(selected_session.cli_backend)

    if preset_backend:
        backend = normalize_backend_name(preset_backend)
    elif started or dry_run:
        backend = preferred_backend
    else:
        backend = callbacks.select_backend_interactive(preferred_backend, Option, single_select_items)

    descriptor = callbacks.backend_descriptor(backend)

    if preset_model and preset_model.strip():
        model = preset_model.strip()
    elif selected_session.cli_model.strip():
        model = selected_session.cli_model.strip()
    elif started or dry_run:
        model = descriptor.default_model
    else:
        model = callbacks.select_model_interactive(backend, descriptor.default_model, Option, single_select_items)

    if preset_reasoning and preset_reasoning.strip():
        reasoning = preset_reasoning.strip()
    elif selected_session.cli_reasoning.strip():
        reasoning = selected_session.cli_reasoning.strip()
    elif started or dry_run:
        reasoning = descriptor.default_reasoning
    else:
        reasoning = callbacks.select_reasoning_interactive(backend, descriptor.default_reasoning, Option, single_select_items)

    return SessionLaunchSelection(
        backend=backend,
        model=callbacks.normalize_backend_model(descriptor, model),
        reasoning=callbacks.normalize_backend_reasoning(descriptor, reasoning),
    )


def _select_fresh_launch_selection(
    *,
    preset_backend: str | None,
    preset_model: str | None,
    preset_reasoning: str | None,
    Option: Any,
    single_select_items: Any,
    callbacks: LaunchSelectionCallbacks,
) -> SessionLaunchSelection:
    preferred_backend = normalize_backend_name(preset_backend)
    if preset_backend:
        backend = preferred_backend
    else:
        backend = callbacks.select_backend_interactive(preferred_backend, Option, single_select_items)
    descriptor = callbacks.backend_descriptor(backend)
    if not descriptor.fresh_launch_supported:
        raise RuntimeError(
            f"Backend {backend!r} is surfaced in the launcher catalog, but fresh-launch execution is not implemented yet."
        )

    if preset_model and preset_model.strip():
        model = preset_model.strip()
    else:
        model = callbacks.select_model_interactive(backend, descriptor.default_model, Option, single_select_items)

    if preset_reasoning and preset_reasoning.strip():
        reasoning = preset_reasoning.strip()
    else:
        reasoning = callbacks.select_reasoning_interactive(backend, descriptor.default_reasoning, Option, single_select_items)

    return SessionLaunchSelection(
        backend=backend,
        model=callbacks.normalize_backend_model(descriptor, model),
        reasoning=callbacks.normalize_backend_reasoning(descriptor, reasoning),
    )
