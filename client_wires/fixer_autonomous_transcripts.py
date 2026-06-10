"""External Codex/Droid session discovery for autonomous launches."""

from __future__ import annotations

import json
import re
import time
from pathlib import Path
from typing import Callable


def _extract_droid_session_id_from_payload(payload: object) -> str | None:
    if isinstance(payload, dict):
        for key in ("external_session_id", "externalSessionId", "session_id", "sessionId"):
            value = payload.get(key)
            if value not in (None, ""):
                return str(value).strip() or None
        session_payload = payload.get("session")
        if session_payload is not None:
            nested = _extract_droid_session_id_from_payload(session_payload)
            if nested:
                return nested
        return None
    if isinstance(payload, list):
        for item in payload:
            nested = _extract_droid_session_id_from_payload(item)
            if nested:
                return nested
    return None


def _extract_droid_session_id_from_line(raw_line: str) -> str | None:
    line = raw_line.strip()
    if not line:
        return None

    try:
        payload = json.loads(line)
    except json.JSONDecodeError:
        match = re.search(
            r'(?:external_session_id|externalSessionId|session_id|sessionId)["=: ]+["\']?([A-Za-z0-9._:-]+)',
            line,
        )
        if match:
            return match.group(1).strip()
        return None

    return _extract_droid_session_id_from_payload(payload)


def _droid_factory_sessions_root() -> Path:
    return Path.home() / ".factory" / "sessions"


def _codex_sessions_root() -> Path:
    return Path.home() / ".codex" / "sessions"


def _extract_droid_record_type(payload: object) -> str:
    if not isinstance(payload, dict):
        return ""
    for key in ("type", "event", "event_type", "record_type"):
        value = payload.get(key)
        if value not in (None, ""):
            return str(value).strip()
    return ""


def _extract_droid_cwd_from_payload(payload: object) -> str | None:
    if not isinstance(payload, dict):
        return None
    for key in ("cwd", "current_working_directory", "workingDirectory", "working_directory"):
        value = payload.get(key)
        if value not in (None, ""):
            return str(value).strip() or None
    session_payload = payload.get("session")
    if isinstance(session_payload, dict):
        nested = _extract_droid_cwd_from_payload(session_payload)
        if nested:
            return nested
    return None


def _extract_codex_session_id_from_payload(payload: object) -> str | None:
    if not isinstance(payload, dict):
        return None
    nested_payload = payload.get("payload")
    if isinstance(nested_payload, dict):
        nested = _extract_codex_session_id_from_payload(nested_payload)
        if nested:
            return nested
    for key in ("id", "session_id", "sessionId", "external_session_id", "externalSessionId"):
        value = payload.get(key)
        if value not in (None, ""):
            return str(value).strip() or None
    return None


def _extract_codex_cwd_from_payload(payload: object) -> str | None:
    if not isinstance(payload, dict):
        return None
    nested_payload = payload.get("payload")
    if isinstance(nested_payload, dict):
        nested = _extract_codex_cwd_from_payload(nested_payload)
        if nested:
            return nested
    for key in ("cwd", "current_working_directory", "workingDirectory", "working_directory"):
        value = payload.get(key)
        if value not in (None, ""):
            return str(value).strip() or None
    return None


def _candidate_droid_transcript_paths(
    sessions_root: Path,
    *,
    launch_started_at: float | None,
) -> list[Path]:
    if not sessions_root.is_dir():
        return []

    cutoff = (launch_started_at - 1.0) if launch_started_at is not None else None
    candidates: list[tuple[float, Path]] = []
    for path in sessions_root.rglob("*.jsonl"):
        try:
            stat = path.stat()
        except OSError:
            continue
        if cutoff is not None and stat.st_mtime < cutoff:
            continue
        candidates.append((stat.st_mtime, path))
    return [path for _mtime, path in sorted(candidates, reverse=True)]


def _candidate_codex_transcript_paths(
    sessions_root: Path,
    *,
    launch_started_at: float | None,
) -> list[Path]:
    if not sessions_root.is_dir():
        return []

    cutoff = (launch_started_at - 1.0) if launch_started_at is not None else None
    candidates: list[tuple[float, Path]] = []
    for path in sessions_root.rglob("*.jsonl"):
        try:
            stat = path.stat()
        except OSError:
            continue
        if cutoff is not None and stat.st_mtime < cutoff:
            continue
        candidates.append((stat.st_mtime, path))
    return [path for _mtime, path in sorted(candidates, reverse=True)]


def _codex_session_id_from_transcript(path: Path, cwd: Path) -> str | None:
    expected_cwd = str(cwd.resolve())
    try:
        with path.open("r", encoding="utf-8") as fh:
            for raw_line in fh:
                try:
                    payload = json.loads(raw_line)
                except json.JSONDecodeError:
                    continue

                if _extract_droid_record_type(payload) != "session_meta":
                    continue

                payload_cwd = _extract_codex_cwd_from_payload(payload)
                if not payload_cwd:
                    continue
                try:
                    resolved_payload_cwd = str(Path(payload_cwd).resolve())
                except OSError:
                    resolved_payload_cwd = payload_cwd
                if resolved_payload_cwd != expected_cwd:
                    continue

                session_id = _extract_codex_session_id_from_payload(payload)
                if session_id:
                    return session_id
                match = re.search(r"([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})", path.name)
                if match:
                    return match.group(1)
    except OSError:
        return None
    return None


def _find_new_codex_session_id_from_transcript_store(
    cwd: Path,
    *,
    launch_started_at: float | None,
    sessions_root: Path | None = None,
    codex_sessions_root_fn: Callable[[], Path] = _codex_sessions_root,
) -> str | None:
    root = sessions_root if sessions_root is not None else codex_sessions_root_fn()
    for path in _candidate_codex_transcript_paths(root, launch_started_at=launch_started_at):
        session_id = _codex_session_id_from_transcript(path, cwd)
        if session_id:
            return session_id
    return None


def _droid_session_id_from_transcript(path: Path, cwd: Path) -> str | None:
    expected_cwd = str(cwd.resolve())
    try:
        with path.open("r", encoding="utf-8") as fh:
            for raw_line in fh:
                try:
                    payload = json.loads(raw_line)
                except json.JSONDecodeError:
                    continue

                record_type = _extract_droid_record_type(payload)
                if record_type and record_type != "session_start":
                    continue

                payload_cwd = _extract_droid_cwd_from_payload(payload)
                if not payload_cwd:
                    continue
                try:
                    resolved_payload_cwd = str(Path(payload_cwd).resolve())
                except OSError:
                    resolved_payload_cwd = payload_cwd
                if resolved_payload_cwd != expected_cwd:
                    continue

                return _extract_droid_session_id_from_payload(payload) or path.stem
    except OSError:
        return None
    return None


def _find_new_droid_session_id_from_factory_store(
    cwd: Path,
    *,
    launch_started_at: float | None,
    sessions_root: Path | None = None,
    droid_factory_sessions_root_fn: Callable[[], Path] = _droid_factory_sessions_root,
) -> str | None:
    root = sessions_root if sessions_root is not None else droid_factory_sessions_root_fn()
    for path in _candidate_droid_transcript_paths(root, launch_started_at=launch_started_at):
        session_id = _droid_session_id_from_transcript(path, cwd)
        if session_id:
            return session_id
    return None


def _wait_for_new_droid_session_id(
    log_path: Path,
    cwd: Path,
    *,
    launch_started_at: float | None = None,
    timeout_sec: float = 8.0,
    find_new_droid_session_id_from_factory_store_fn: Callable[..., str | None] = _find_new_droid_session_id_from_factory_store,
    extract_droid_session_id_from_line_fn: Callable[[str], str | None] = _extract_droid_session_id_from_line,
) -> str | None:
    deadline = time.time() + timeout_sec
    while time.time() < deadline:
        session_id = find_new_droid_session_id_from_factory_store_fn(
            cwd,
            launch_started_at=launch_started_at,
        )
        if session_id:
            return session_id
        try:
            with log_path.open("r", encoding="utf-8") as fh:
                for raw_line in fh:
                    session_id = extract_droid_session_id_from_line_fn(raw_line)
                    if session_id:
                        return session_id
        except OSError:
            return None
        time.sleep(0.5)
    return None


def _wait_for_new_codex_session_id(
    cwd: Path,
    before: str | None,
    *,
    timeout_sec: float = 8.0,
    latest_codex_session_id_for_cwd_fn: Callable[[Path], str | None],
    find_new_codex_session_id_from_transcript_store_fn: Callable[..., str | None] = _find_new_codex_session_id_from_transcript_store,
) -> str | None:
    deadline = time.time() + timeout_sec
    while time.time() < deadline:
        latest = latest_codex_session_id_for_cwd_fn(cwd)
        if latest and latest != before:
            return latest
        latest = find_new_codex_session_id_from_transcript_store_fn(
            cwd,
            launch_started_at=deadline - timeout_sec,
        )
        if latest and latest != before:
            return latest
        time.sleep(0.5)
    return None


def _wait_for_new_external_session_id(
    backend: str,
    cwd: Path,
    before: str | None,
    log_path: Path,
    *,
    launch_started_at: float | None = None,
    timeout_sec: float = 8.0,
    normalize_backend_name_fn: Callable[[str], str],
    wait_for_new_codex_session_id_fn: Callable[..., str | None],
    wait_for_new_droid_session_id_fn: Callable[..., str | None],
) -> str | None:
    normalized_backend = normalize_backend_name_fn(backend)
    if normalized_backend == "codex":
        return wait_for_new_codex_session_id_fn(cwd, before, timeout_sec=timeout_sec)
    if normalized_backend == "droid":
        return wait_for_new_droid_session_id_fn(
            log_path,
            cwd,
            launch_started_at=launch_started_at,
            timeout_sec=timeout_sec,
        )
    return None
