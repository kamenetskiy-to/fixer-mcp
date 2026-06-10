"""Codex session history lookup helpers."""

from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime, timezone
import json
from pathlib import Path
from typing import Dict, List, Optional


@dataclass
class SessionSummary:
    session_id: str
    created: datetime
    updated: datetime
    preview: str
    cwd: Optional[Path] = None


def session_log_root() -> Path:
    return Path.home() / ".codex" / "sessions"


def find_session_log(session_id: str, *, created: Optional[datetime], updated: Optional[datetime]) -> Optional[Path]:
    root = session_log_root()
    candidate_dirs: List[Path] = []
    for dt in (updated, created):
        if not dt:
            continue
        utc_dt = dt.astimezone(timezone.utc)
        candidate_dirs.append(root / f"{utc_dt.year:04d}" / f"{utc_dt.month:02d}" / f"{utc_dt.day:02d}")
    candidate_dirs.append(root)

    for directory in candidate_dirs:
        if not directory.exists():
            continue
        try:
            return next(directory.rglob(f"*{session_id}.jsonl"))
        except StopIteration:
            continue
    return None


def session_cwd_from_log(log_path: Path) -> Optional[Path]:
    try:
        with log_path.open("r", encoding="utf-8") as fh:
            for raw_line in fh:
                try:
                    entry = json.loads(raw_line)
                except json.JSONDecodeError:
                    continue
                if entry.get("type") != "session_meta":
                    continue
                payload = entry.get("payload") or {}
                cwd_value = payload.get("cwd")
                if not cwd_value:
                    return None
                return Path(cwd_value)
    except OSError:
        return None
    return None


def load_session_summaries(history_path: Path, *, limit: int = 30, cwd_filter: Optional[Path] = None) -> List[SessionSummary]:
    if not history_path.is_file():
        return []
    sessions: Dict[str, SessionSummary] = {}
    try:
        with history_path.open("r", encoding="utf-8") as fh:
            for raw_line in fh:
                line = raw_line.strip()
                if not line:
                    continue
                try:
                    entry = json.loads(line)
                except json.JSONDecodeError:
                    continue
                session_id = entry.get("session_id")
                ts = entry.get("ts")
                if not session_id or ts is None:
                    continue
                try:
                    timestamp = datetime.fromtimestamp(ts, timezone.utc)
                except (OverflowError, OSError):
                    continue
                text = (entry.get("text") or "").strip()
                summary = sessions.get(session_id)
                if summary is None:
                    sessions[session_id] = SessionSummary(
                        session_id=session_id,
                        created=timestamp,
                        updated=timestamp,
                        preview=text,
                    )
                else:
                    if timestamp < summary.created:
                        summary.created = timestamp
                        if text:
                            summary.preview = text
                    if timestamp > summary.updated:
                        summary.updated = timestamp
                    if not summary.preview and text:
                        summary.preview = text
    except OSError:
        return []
    sorted_sessions = sorted(sessions.values(), key=lambda s: s.updated, reverse=True)[:limit]

    if cwd_filter is None:
        return sorted_sessions

    normalized_cwd = cwd_filter.resolve()
    filtered: List[SessionSummary] = []
    for summary in sorted_sessions:
        log_path = find_session_log(summary.session_id, created=summary.created, updated=summary.updated)
        if not log_path:
            continue
        session_cwd = session_cwd_from_log(log_path)
        if not session_cwd:
            continue
        try:
            if session_cwd.resolve() != normalized_cwd:
                continue
        except OSError:
            continue
        summary.cwd = session_cwd
        filtered.append(summary)
    return filtered


_session_log_root = session_log_root
_find_session_log = find_session_log
_session_cwd_from_log = session_cwd_from_log
_load_session_summaries = load_session_summaries

