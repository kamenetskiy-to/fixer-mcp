"""Resume and transcript discovery helpers for the Fixer wire launcher."""

from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime, timezone
import json
import re
from pathlib import Path
from typing import Any, Callable, Sequence

from client_wires.backends import SUPPORTED_BACKENDS, normalize_backend_name
from client_wires import fixer_wire_db
from client_wires import fixer_wire_selectors


@dataclass(frozen=True)
class ResumeSessionSummary:
    session_id: str
    created: datetime
    updated: datetime
    preview: str
    provider: str = "codex"
    log_path: Path | None = None


@dataclass(frozen=True)
class FixerResumeSelection:
    provider: str
    session_id: str

    @property
    def selector_value(self) -> str:
        return format_fixer_resume_selection(self.provider, self.session_id)


def summary_provider(summary: Any) -> str:
    return normalize_backend_name(
        str(
            getattr(
                summary,
                "provider",
                getattr(summary, "backend", getattr(summary, "cli_backend", "codex")),
            )
            or "codex"
        )
    )


def format_fixer_resume_selection(provider: str, session_id: str) -> str:
    normalized = normalize_backend_name(provider)
    clean_session_id = str(session_id).strip()
    if normalized == "codex":
        return clean_session_id
    return f"{normalized}:{clean_session_id}"


def parse_fixer_resume_selection(value: str) -> FixerResumeSelection:
    selected = str(value).strip()
    if ":" in selected:
        maybe_provider, maybe_session_id = selected.split(":", 1)
        normalized = normalize_backend_name(maybe_provider)
        if normalized in SUPPORTED_BACKENDS and maybe_session_id.strip():
            return FixerResumeSelection(normalized, maybe_session_id.strip())
    return FixerResumeSelection("codex", selected)


def wrap_resume_summary(summary: Any, provider: str, *, log_path: Path | None = None) -> ResumeSessionSummary:
    return ResumeSessionSummary(
        session_id=str(getattr(summary, "session_id")),
        created=getattr(summary, "created"),
        updated=getattr(summary, "updated"),
        preview=str(getattr(summary, "preview", "") or ""),
        provider=normalize_backend_name(provider),
        log_path=log_path,
    )


def latest_codex_session_id_for_cwd(cwd: Path) -> str | None:
    try:
        from client_wires.codex_compat.sessions import _load_session_summaries
    except Exception:
        return None

    history_path = Path.home() / ".codex" / "history.jsonl"
    try:
        summaries = _load_session_summaries(history_path, limit=1, cwd_filter=cwd)
    except Exception:
        return None
    if not summaries:
        return None
    return summaries[0].session_id


def prompt_resume_session_id(
    session_id: int,
    backend: str,
    *,
    backend_descriptor: Callable[[str], Any],
) -> str | None:
    descriptor = backend_descriptor(backend)
    while True:
        raw = input(
            f"No stored {descriptor.label} session id for session {session_id}. "
            "Enter session id to resume (q cancel): "
        ).strip()
        if raw.lower() in {"q", "quit", "exit"}:
            return None
        if raw:
            return raw
        print("Session id is required to resume non-pending sessions.")


def netrunner_session_marker(session_id: int) -> str:
    return f"Preselected session ID from fixer wire: `{session_id}`."


def first_marker_line(
    log_path: Path,
    marker: str,
    *,
    max_lines: int = 240,
) -> int | None:
    if not marker:
        return None
    try:
        with log_path.open("r", encoding="utf-8") as fh:
            for index, raw_line in enumerate(fh):
                if marker in raw_line:
                    return index
                if index >= max_lines:
                    break
    except OSError:
        return None
    return None


def first_any_marker_line(
    log_path: Path,
    markers: Sequence[str],
    *,
    max_lines: int = 240,
) -> int | None:
    lines = [
        line
        for marker in markers
        if (line := first_marker_line(log_path, marker, max_lines=max_lines)) is not None
    ]
    return min(lines) if lines else None


def session_log_has_markers(log_path: Path, markers: Sequence[str], *, max_lines: int = 240) -> bool:
    try:
        with log_path.open("r", encoding="utf-8") as fh:
            required = {marker for marker in markers if marker}
            for index, raw_line in enumerate(fh):
                matched = {marker for marker in required if marker in raw_line}
                required.difference_update(matched)
                if not required:
                    return True
                if index >= max_lines:
                    break
    except OSError:
        return False
    return False


def session_log_has_any_marker(log_path: Path, markers: Sequence[str], *, max_lines: int = 240) -> bool:
    return first_any_marker_line(log_path, markers, max_lines=max_lines) is not None


def session_log_has_fixer_marker(
    log_path: Path,
    *,
    fixer_skill_markers: Sequence[str],
    max_lines: int = 240,
) -> bool:
    return session_log_has_any_marker(log_path, fixer_skill_markers, max_lines=max_lines)


def session_log_is_fixer_session(
    log_path: Path,
    *,
    fixer_skill_markers: Sequence[str],
    netrunner_skill_markers: Sequence[str],
    overseer_skill_markers: Sequence[str],
    max_lines: int = 240,
) -> bool:
    fixer_line = first_any_marker_line(log_path, fixer_skill_markers, max_lines=max_lines)
    if fixer_line is None:
        return False
    competing_lines = [
        line
        for line in (
            first_any_marker_line(log_path, netrunner_skill_markers, max_lines=max_lines),
            first_any_marker_line(log_path, overseer_skill_markers, max_lines=max_lines),
        )
        if line is not None
    ]
    if not competing_lines:
        return True
    return fixer_line <= min(competing_lines)


def session_log_is_overseer_session(
    log_path: Path,
    *,
    fixer_skill_markers: Sequence[str],
    netrunner_skill_markers: Sequence[str],
    overseer_skill_markers: Sequence[str],
    max_lines: int = 240,
) -> bool:
    overseer_line = first_any_marker_line(log_path, overseer_skill_markers, max_lines=max_lines)
    if overseer_line is None:
        return False
    competing_lines = [
        line
        for line in (
            first_any_marker_line(log_path, fixer_skill_markers, max_lines=max_lines),
            first_any_marker_line(log_path, netrunner_skill_markers, max_lines=max_lines),
        )
        if line is not None
    ]
    if not competing_lines:
        return True
    return overseer_line <= min(competing_lines)


def session_log_has_netrunner_marker(
    log_path: Path,
    session_id: int | None = None,
    *,
    netrunner_skill_markers: Sequence[str],
    max_lines: int = 240,
) -> bool:
    if not session_log_has_any_marker(log_path, netrunner_skill_markers, max_lines=max_lines):
        return False
    if session_id is None:
        return True
    return session_log_has_markers(log_path, [netrunner_session_marker(session_id)], max_lines=max_lines)


def load_cwd_session_summaries(cwd: Path, *, limit: int, minimum_scan_limit: int = 80) -> tuple[Any, list[Any]]:
    try:
        from client_wires.codex_compat.sessions import _find_session_log, _load_session_summaries
    except Exception as err:
        raise RuntimeError("Unable to load Codex history helpers for resume flow.") from err

    history_path = Path.home() / ".codex" / "history.jsonl"
    scan_limit = max(limit * 4, minimum_scan_limit)
    summaries = _load_session_summaries(history_path, limit=scan_limit, cwd_filter=cwd)
    return _find_session_log, summaries


def _project_store_slug(cwd: Path) -> str:
    return re.sub(r"[^A-Za-z0-9]+", "-", str(cwd.resolve()))


def _datetime_from_value(value: object, *, fallback: datetime) -> datetime:
    if isinstance(value, datetime):
        if value.tzinfo is None:
            return value.replace(tzinfo=timezone.utc)
        return value
    if isinstance(value, (int, float)) and not isinstance(value, bool):
        seconds = float(value) / 1000 if value > 10_000_000_000 else float(value)
        return datetime.fromtimestamp(seconds, tz=timezone.utc)
    if isinstance(value, str) and value.strip():
        raw = value.strip()
        try:
            if raw.endswith("Z"):
                raw = f"{raw[:-1]}+00:00"
            parsed = datetime.fromisoformat(raw)
            if parsed.tzinfo is None:
                return parsed.replace(tzinfo=timezone.utc)
            return parsed
        except ValueError:
            return fallback
    return fallback


def _file_time(path: Path) -> datetime:
    try:
        return datetime.fromtimestamp(path.stat().st_mtime, tz=timezone.utc)
    except OSError:
        return datetime.now(timezone.utc)


def _iter_jsonl_records(path: Path, *, max_lines: int = 400) -> list[dict[str, Any]]:
    records: list[dict[str, Any]] = []
    try:
        with path.open("r", encoding="utf-8") as fh:
            for index, raw_line in enumerate(fh):
                if index >= max_lines:
                    break
                try:
                    record = json.loads(raw_line)
                except json.JSONDecodeError:
                    continue
                if isinstance(record, dict):
                    records.append(record)
    except OSError:
        return []
    return records


def _walk_strings(value: object) -> list[str]:
    if isinstance(value, str):
        return [value]
    if isinstance(value, list):
        found: list[str] = []
        for item in value:
            found.extend(_walk_strings(item))
        return found
    if isinstance(value, dict):
        found = []
        for item in value.values():
            found.extend(_walk_strings(item))
        return found
    return []


_SKIPPED_PREVIEW_PREFIXES = (
    "<system-reminder>",
    "<command-name>",
    "<local-command-caveat>",
    "<task-notification>",
    "Caveat:",
)


def _content_text_strings(content: object) -> list[str]:
    if isinstance(content, str):
        return [content]
    if not isinstance(content, list):
        return []

    found: list[str] = []
    for item in content:
        if isinstance(item, str):
            found.append(item)
            continue
        if not isinstance(item, dict):
            continue
        item_type = str(item.get("type", "") or "").casefold()
        if item_type in {"text", "input_text"} and isinstance(item.get("text"), str):
            found.append(str(item["text"]))
        nested_content = item.get("content")
        if nested_content is not None:
            found.extend(_content_text_strings(nested_content))
    return found


def _message_preview_texts(message: object) -> list[str]:
    if isinstance(message, dict):
        return _content_text_strings(message.get("content"))
    if isinstance(message, str):
        return [message]
    return []


def _is_informative_preview_text(text: str) -> bool:
    return bool(text) and not text.startswith(_SKIPPED_PREVIEW_PREFIXES)


def _preview_from_records(records: Sequence[dict[str, Any]], *, fallback: str) -> str:
    for record in records:
        record_type = str(record.get("type", "")).casefold()
        message = record.get("message")
        role = ""
        if isinstance(message, dict):
            role = str(message.get("role", "")).casefold()
        if record_type not in {"user", "message"} and role != "user":
            continue
        for text in _message_preview_texts(message):
            clean = " ".join(text.split())
            if _is_informative_preview_text(clean):
                return clean
    return fallback


def _summary_times_from_records(
    records: Sequence[dict[str, Any]],
    *,
    fallback: datetime,
) -> tuple[datetime, datetime]:
    times: list[datetime] = []
    for record in records:
        for key in ("timestamp", "createdAt", "updatedAt", "startTime", "lastUpdated", "timestampMs"):
            if key in record:
                times.append(_datetime_from_value(record[key], fallback=fallback))
    if not times:
        return fallback, fallback
    return min(times), max(times)


def _load_claude_fixer_resume_summaries(
    cwd: Path,
    *,
    limit: int,
    session_is_fixer: Callable[[Path], bool],
) -> list[ResumeSessionSummary]:
    project_dir = Path.home() / ".claude" / "projects" / _project_store_slug(cwd)
    if not project_dir.is_dir():
        return []

    summaries: list[ResumeSessionSummary] = []
    for log_path in sorted(project_dir.glob("*.jsonl"), key=lambda item: _file_time(item), reverse=True):
        if not session_is_fixer(log_path):
            continue
        records = _iter_jsonl_records(log_path)
        fallback = _file_time(log_path)
        session_id = ""
        for record in records:
            session_id = str(record.get("sessionId", "") or "").strip()
            if session_id:
                break
        session_id = session_id or log_path.stem
        created, updated = _summary_times_from_records(records, fallback=fallback)
        summaries.append(
            ResumeSessionSummary(
                provider="claude",
                session_id=session_id,
                created=created,
                updated=updated,
                preview=_preview_from_records(records, fallback=log_path.stem),
                log_path=log_path,
            )
        )
        if len(summaries) >= limit:
            break
    return summaries


def _load_droid_fixer_resume_summaries(
    cwd: Path,
    *,
    limit: int,
    session_is_fixer: Callable[[Path], bool],
) -> list[ResumeSessionSummary]:
    project_dir = Path.home() / ".factory" / "sessions" / _project_store_slug(cwd)
    if not project_dir.is_dir():
        return []

    cwd_text = str(cwd.resolve())
    summaries: list[ResumeSessionSummary] = []
    for log_path in sorted(project_dir.glob("*.jsonl"), key=lambda item: _file_time(item), reverse=True):
        records = _iter_jsonl_records(log_path)
        if records:
            recorded_cwd = str(records[0].get("cwd", "") or "")
            if recorded_cwd and Path(recorded_cwd).resolve() != cwd.resolve():
                continue
        elif cwd_text not in str(log_path):
            continue
        if not session_is_fixer(log_path):
            continue
        fallback = _file_time(log_path)
        first_record = records[0] if records else {}
        session_id = str(first_record.get("id", "") or first_record.get("sessionId", "") or log_path.stem)
        created, updated = _summary_times_from_records(records, fallback=fallback)
        summaries.append(
            ResumeSessionSummary(
                provider="droid",
                session_id=session_id,
                created=created,
                updated=updated,
                preview=str(first_record.get("sessionTitle", "") or first_record.get("title", "") or log_path.stem),
                log_path=log_path,
            )
        )
        if len(summaries) >= limit:
            break
    return summaries


def _load_junie_fixer_resume_summaries(
    cwd: Path,
    *,
    limit: int,
    session_is_fixer: Callable[[Path], bool],
) -> list[ResumeSessionSummary]:
    index_path = Path.home() / ".junie" / "sessions" / "index.jsonl"
    if not index_path.is_file():
        return []

    summaries: list[ResumeSessionSummary] = []
    cwd_text = str(cwd.resolve())
    fallback = _file_time(index_path)
    for record in _iter_jsonl_records(index_path, max_lines=1000):
        if str(record.get("projectDir", "") or "") != cwd_text:
            continue
        session_id = str(record.get("sessionId", "") or "").strip()
        if not session_id:
            continue
        session_dir = index_path.parent / session_id
        marker_path = session_dir / "state.json"
        if not marker_path.is_file():
            marker_path = session_dir / "events.jsonl"
        if not marker_path.is_file() or not session_is_fixer(marker_path):
            continue
        created = _datetime_from_value(record.get("createdAt"), fallback=fallback)
        updated = _datetime_from_value(record.get("updatedAt"), fallback=created)
        summaries.append(
            ResumeSessionSummary(
                provider="junie",
                session_id=session_id,
                created=created,
                updated=updated,
                preview=str(record.get("taskName", "") or session_id),
                log_path=marker_path,
            )
        )
        if len(summaries) >= limit:
            break
    return summaries


def load_fixer_resume_alias_session_ids(
    cwd: Path,
    *,
    resolve_fixer_db_path: Callable[[Path], Path],
    ensure_wire_schema: Callable[[Any], None],
    resolve_project_id: Callable[[Any, Path], int | None],
) -> set[str]:
    return fixer_wire_db._load_fixer_resume_alias_session_ids(
        cwd,
        resolve_fixer_db_path=resolve_fixer_db_path,
        ensure_wire_schema=ensure_wire_schema,
        resolve_project_id=resolve_project_id,
    )


def load_fixer_resume_summaries(
    cwd: Path,
    *,
    limit: int = 40,
    load_cwd_summaries: Callable[..., tuple[Any, list[Any]]] = load_cwd_session_summaries,
    load_alias_session_ids: Callable[[Path], set[str]],
    session_is_fixer: Callable[[Path], bool],
) -> list[Any]:
    fixer_summaries: list[Any] = []
    codex_error: RuntimeError | None = None
    try:
        find_session_log, summaries = load_cwd_summaries(cwd, limit=limit)
    except RuntimeError as err:
        codex_error = RuntimeError("Unable to load Codex history helpers for Fixer resume flow.")
    else:
        explicit_session_ids = load_alias_session_ids(cwd)
        for summary in summaries:
            if str(summary.session_id) in explicit_session_ids:
                fixer_summaries.append(wrap_resume_summary(summary, "codex"))
                if len(fixer_summaries) >= limit:
                    break
                continue
            log_path = find_session_log(summary.session_id, created=summary.created, updated=summary.updated)
            if not log_path:
                continue
            if not session_is_fixer(log_path):
                continue
            fixer_summaries.append(wrap_resume_summary(summary, "codex", log_path=log_path))
            if len(fixer_summaries) >= limit:
                break

    provider_loaders = (
        _load_claude_fixer_resume_summaries,
        _load_droid_fixer_resume_summaries,
        _load_junie_fixer_resume_summaries,
    )
    for provider_loader in provider_loaders:
        remaining = max(limit - len(fixer_summaries), 0)
        if remaining <= 0:
            break
        fixer_summaries.extend(
            provider_loader(
                cwd,
                limit=remaining,
                session_is_fixer=session_is_fixer,
            )
        )

    if not fixer_summaries and codex_error is not None:
        raise codex_error

    fixer_summaries.sort(key=lambda summary: getattr(summary, "updated"), reverse=True)
    return fixer_summaries[:limit]


def load_overseer_resume_summaries(
    cwd: Path,
    *,
    limit: int = 40,
    load_cwd_summaries: Callable[..., tuple[Any, list[Any]]] = load_cwd_session_summaries,
    session_is_overseer: Callable[[Path], bool],
) -> list[Any]:
    try:
        find_session_log, summaries = load_cwd_summaries(cwd, limit=limit)
    except RuntimeError as err:
        raise RuntimeError("Unable to load Codex history helpers for Overseer resume flow.") from err
    overseer_summaries: list[Any] = []
    for summary in summaries:
        log_path = find_session_log(summary.session_id, created=summary.created, updated=summary.updated)
        if not log_path:
            continue
        if not session_is_overseer(log_path):
            continue
        overseer_summaries.append(summary)
        if len(overseer_summaries) >= limit:
            break
    return overseer_summaries


def load_netrunner_resume_summaries(
    cwd: Path,
    session_id: int,
    *,
    limit: int = 20,
    load_cwd_summaries: Callable[..., tuple[Any, list[Any]]] = load_cwd_session_summaries,
    log_has_netrunner_marker: Callable[[Path, int], bool],
) -> list[Any]:
    try:
        find_session_log, summaries = load_cwd_summaries(cwd, limit=limit, minimum_scan_limit=120)
    except RuntimeError as err:
        raise RuntimeError("Unable to load Codex history helpers for Netrunner resume flow.") from err

    netrunner_summaries: list[Any] = []
    for summary in summaries:
        log_path = find_session_log(summary.session_id, created=summary.created, updated=summary.updated)
        if not log_path:
            continue
        if not log_has_netrunner_marker(log_path, session_id):
            continue
        netrunner_summaries.append(summary)
        if len(netrunner_summaries) >= limit:
            break
    return netrunner_summaries


def resolve_latest_fixer_resume_session_id(
    cwd: Path,
    *,
    load_fixer_resume_summaries: Callable[..., list[Any]],
) -> str:
    summaries = load_fixer_resume_summaries(cwd, limit=1)
    if not summaries:
        raise RuntimeError("No existing Fixer sessions were found for this project cwd.")
    return format_fixer_resume_selection(summary_provider(summaries[0]), str(summaries[0].session_id))


def select_netrunner_resume_session_interactive(
    summaries: Sequence[Any],
    session_id: int,
    Option: Any,
    single_select_items: Any,
    *,
    preferred_session_id: str | None = None,
) -> str:
    return fixer_wire_selectors._select_netrunner_resume_session_interactive(
        summaries,
        session_id,
        Option,
        single_select_items,
        preferred_session_id=preferred_session_id,
    )


def resolve_netrunner_resume_session_id(
    cwd: Path,
    selected_session: Any,
    Option: Any,
    single_select_items: Any,
    *,
    prompt_resume_session_id: Callable[[int, str], str | None],
    load_netrunner_resume_summaries: Callable[[Path, int], list[Any]],
    select_netrunner_resume_session_interactive: Callable[..., str],
) -> str:
    backend = normalize_backend_name(selected_session.cli_backend)
    stored_session_id = selected_session.external_session_id.strip()
    if backend != "codex":
        if stored_session_id:
            return stored_session_id
        manual_session_id = prompt_resume_session_id(selected_session.session_id, backend)
        if manual_session_id:
            return manual_session_id
        raise RuntimeError(
            f"Session {selected_session.session_id} is not pending and no stored {backend} session id was found to resume."
        )

    matching_summaries = load_netrunner_resume_summaries(cwd, selected_session.session_id)
    available_ids = [str(summary.session_id) for summary in matching_summaries]

    if stored_session_id and stored_session_id in available_ids:
        return stored_session_id
    if len(matching_summaries) == 1:
        return str(matching_summaries[0].session_id)
    if matching_summaries:
        preferred = stored_session_id if stored_session_id in available_ids else None
        return select_netrunner_resume_session_interactive(
            matching_summaries,
            selected_session.session_id,
            Option,
            single_select_items,
            preferred_session_id=preferred,
        )

    manual_session_id = prompt_resume_session_id(selected_session.session_id, backend)
    if manual_session_id:
        return manual_session_id
    raise RuntimeError(
        f"Session {selected_session.session_id} is not pending and no matching Codex session was found to resume."
    )


def latest_matching_netrunner_codex_session_id(
    cwd: Path,
    session_id: int,
    *,
    load_netrunner_resume_summaries: Callable[..., list[Any]],
) -> str | None:
    summaries = load_netrunner_resume_summaries(cwd, session_id, limit=8)
    if not summaries:
        return None
    return str(summaries[0].session_id)
