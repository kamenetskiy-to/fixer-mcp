"""State helpers for autonomous Fixer/Netrunner control-plane flows."""

from __future__ import annotations

import json
import time
from pathlib import Path
from typing import Any, Callable

AUTONOMOUS_STATE_RELATIVE_PATH = Path(".codex") / "autonomous_resolution.json"


def _state_path(cwd: Path) -> Path:
    return cwd / AUTONOMOUS_STATE_RELATIVE_PATH


def _load_state(cwd: Path) -> dict[str, Any]:
    path = _state_path(cwd)
    if not path.is_file():
        raise RuntimeError(
            f"Autonomous state file not found: {path}. "
            "Register the Fixer session first."
        )
    return json.loads(path.read_text(encoding="utf-8"))


def _save_state(cwd: Path, payload: dict[str, Any]) -> Path:
    path = _state_path(cwd)
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(payload, indent=2) + "\n", encoding="utf-8")
    return path


def _normalize_active_netrunner_session_ids(state: dict[str, Any]) -> list[int]:
    raw_ids = state.get("active_netrunner_session_ids")
    if isinstance(raw_ids, list):
        values = raw_ids
    else:
        values = []

    legacy_value = state.get("active_netrunner_session_id")
    if legacy_value not in (None, "", 0):
        values = [*values, legacy_value]

    normalized: list[int] = []
    seen: set[int] = set()
    for raw in values:
        try:
            session_id = int(raw)
        except (TypeError, ValueError):
            continue
        if session_id <= 0 or session_id in seen:
            continue
        seen.add(session_id)
        normalized.append(session_id)
    return normalized


def _set_active_netrunner_session_ids(state: dict[str, Any], session_ids: list[int]) -> dict[str, Any]:
    state["active_netrunner_session_ids"] = session_ids
    state["active_netrunner_session_id"] = session_ids[-1] if session_ids else None
    return state


def _load_or_initialize_launch_state(
    cwd: Path,
    fixer_session_id: str | None,
    *,
    resolve_fixer_session_id_fn: Callable[[Path, str | None], str],
    allow_missing_fixer_session: bool = False,
) -> dict[str, Any]:
    def resolve_launch_fixer_session_id() -> str:
        try:
            return resolve_fixer_session_id_fn(cwd, fixer_session_id)
        except RuntimeError:
            if allow_missing_fixer_session:
                return ""
            raise

    path = _state_path(cwd)
    if path.is_file():
        payload = _load_state(cwd)
    else:
        payload = {
            "mode": "serial_autonomous_resolution",
            "workflow_type": "ghost_run",
            "workflow_label": "Ghost Run",
            "project_cwd": str(cwd),
            "fixer_codex_session_id": resolve_launch_fixer_session_id(),
            "active_netrunner_session_id": None,
            "active_netrunner_session_ids": [],
            "updated_at_epoch": int(time.time()),
        }
        _save_state(cwd, payload)

    if not str(payload.get("fixer_codex_session_id", "")).strip():
        payload["fixer_codex_session_id"] = resolve_launch_fixer_session_id()
        payload["updated_at_epoch"] = int(time.time())
        _save_state(cwd, payload)

    _set_active_netrunner_session_ids(payload, _normalize_active_netrunner_session_ids(payload))

    return payload


def _current_state_fixer_session_id(cwd: Path) -> str:
    state = _load_state(cwd)
    session_id = str(state.get("fixer_codex_session_id", "")).strip()
    if not session_id:
        raise RuntimeError(
            f"Autonomous state file for {cwd} does not contain fixer_codex_session_id."
        )
    return session_id


def _clear_stale_active_netrunner_if_safe(
    cwd: Path,
    state: dict[str, Any],
    session_by_id: dict[int, Any],
    requested_session_id: int,
) -> dict[str, Any]:
    active_session_ids = _normalize_active_netrunner_session_ids(state)
    retained_session_ids: list[int] = []
    for active_session_id in active_session_ids:
        if active_session_id == requested_session_id:
            retained_session_ids.append(active_session_id)
            continue
        active_session = session_by_id.get(active_session_id)
        if active_session is not None and active_session.status in {"pending", "in_progress"}:
            retained_session_ids.append(active_session_id)

    _set_active_netrunner_session_ids(state, retained_session_ids)
    state["updated_at_epoch"] = int(time.time())
    _save_state(cwd, state)
    return state


def _format_runtime_error(exc: RuntimeError) -> str:
    messages = [str(exc)]
    cause: BaseException | None = exc.__cause__
    if cause is None and not exc.__suppress_context__:
        cause = exc.__context__
    seen: set[int] = {id(exc)}
    while cause is not None and id(cause) not in seen:
        seen.add(id(cause))
        messages.append(f"Caused by: {type(cause).__name__}: {cause}")
        next_cause = cause.__cause__
        if next_cause is None and not cause.__suppress_context__:
            next_cause = cause.__context__
        cause = next_cause
    return "\n".join(messages)
