"""Resume and transcript discovery helpers for the Fixer wire launcher."""

from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime, timezone
import hashlib
import json
import os
import re
import sqlite3
import subprocess
from pathlib import Path
from typing import Any, Callable, Sequence

from client_wires.backends import SUPPORTED_BACKENDS, available_backend_descriptors, is_codex_backend, normalize_backend_name
from client_wires.backends.codex_adapter import codex_default_model_for_family, codex_model_family_for_model, codex_model_family_label
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
    cwd: Path | None = None
    model: str = ""
    reasoning: str = ""
    origin: str = "provider_history"
    subscription_provider: str = ""


@dataclass(frozen=True)
class FixerResumeSelection:
    provider: str
    session_id: str
    subscription_provider: str = ""

    @property
    def selector_value(self) -> str:
        return format_fixer_resume_selection(self.provider, self.session_id, self.subscription_provider)


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


def summary_subscription_provider(summary: Any) -> str:
    explicit = str(getattr(summary, "subscription_provider", "") or "").strip().lower()
    if explicit:
        return explicit
    backend = summary_provider(summary)
    model = str(getattr(summary, "model", "") or "").strip()
    if backend in {"codex", "commandcode"}:
        return codex_model_family_for_model(model) if model else ("commandcode" if backend == "commandcode" else "openai")
    return backend


def format_fixer_resume_selection(provider: str, session_id: str, subscription_provider: str = "") -> str:
    normalized = normalize_backend_name(provider)
    clean_session_id = str(session_id).strip()
    if is_codex_backend(normalized):
        subscription = subscription_provider.strip().lower()
        if subscription and subscription != "openai":
            return f"codex/{subscription}:{clean_session_id}"
        return clean_session_id
    return f"{normalized}:{clean_session_id}"


def parse_fixer_resume_selection(value: str) -> FixerResumeSelection:
    selected = str(value).strip()
    if ":" in selected:
        maybe_provider, maybe_session_id = selected.split(":", 1)
        if maybe_provider.startswith("codex/") and maybe_session_id.strip():
            return FixerResumeSelection("codex", maybe_session_id.strip(), maybe_provider.split("/", 1)[1])
        normalized = normalize_backend_name(maybe_provider)
        if normalized in SUPPORTED_BACKENDS and maybe_session_id.strip():
            return FixerResumeSelection(normalized, maybe_session_id.strip(), normalized if normalized != "codex" else "openai")
    return FixerResumeSelection("codex", selected, "openai")


def wrap_resume_summary(summary: Any, provider: str, *, log_path: Path | None = None) -> ResumeSessionSummary:
    return ResumeSessionSummary(
        session_id=str(getattr(summary, "session_id")),
        created=getattr(summary, "created"),
        updated=getattr(summary, "updated"),
        preview=str(getattr(summary, "preview", "") or ""),
        provider=normalize_backend_name(provider),
        log_path=log_path,
        cwd=getattr(summary, "cwd", None),
        model=str(getattr(summary, "model", "") or ""),
        reasoning=str(getattr(summary, "reasoning", "") or ""),
        origin=str(getattr(summary, "origin", "provider_history") or "provider_history"),
        subscription_provider=summary_subscription_provider(summary),
    )


def _backend_defaults(provider: str) -> tuple[str, str]:
    normalized = normalize_backend_name(provider)
    for descriptor in available_backend_descriptors():
        if descriptor.name == normalized:
            return descriptor.default_model, descriptor.default_reasoning
    return "", ""


def _summary_with_metadata(
    *,
    provider: str,
    session_id: str,
    created: datetime,
    updated: datetime,
    preview: str,
    log_path: Path | None,
    cwd: Path,
    model: str = "",
    reasoning: str = "",
    origin: str,
    subscription_provider: str = "",
) -> ResumeSessionSummary:
    default_model, default_reasoning = _backend_defaults(provider)
    return ResumeSessionSummary(
        provider=normalize_backend_name(provider),
        session_id=session_id,
        created=created,
        updated=updated,
        preview=preview,
        log_path=log_path,
        cwd=cwd.resolve(),
        model=model.strip() or default_model,
        reasoning=reasoning.strip() or default_reasoning,
        origin=origin,
        subscription_provider=(subscription_provider.strip().lower() or (
            codex_model_family_for_model(model)
            if normalize_backend_name(provider) in {"codex", "commandcode"}
            else normalize_backend_name(provider)
        )),
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
    return f"Preselected compatibility session ID from fixer wire: `{session_id}`."


def _legacy_netrunner_session_marker(session_id: int) -> str:
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
    user_texts = _session_user_message_texts(log_path, max_lines=max_lines)
    if user_texts:
        fixer_line = _first_marker_position(user_texts, fixer_skill_markers)
        competing_lines = [
            position
            for position in (
                _first_marker_position(user_texts, netrunner_skill_markers),
                _first_marker_position(user_texts, overseer_skill_markers),
            )
            if position is not None
        ]
    else:
        fixer_line = first_any_marker_line(log_path, fixer_skill_markers, max_lines=max_lines)
        competing_lines = [
            line
            for line in (
                first_any_marker_line(log_path, netrunner_skill_markers, max_lines=max_lines),
                first_any_marker_line(log_path, overseer_skill_markers, max_lines=max_lines),
            )
            if line is not None
        ]
    if fixer_line is None:
        return False
    if not competing_lines:
        return True
    return fixer_line <= min(competing_lines)


def _session_user_message_texts(log_path: Path, *, max_lines: int) -> list[str]:
    texts: list[str] = []
    try:
        with log_path.open("r", encoding="utf-8") as handle:
            for index, raw_line in enumerate(handle):
                if index >= max_lines:
                    break
                try:
                    record = json.loads(raw_line)
                except json.JSONDecodeError:
                    continue
                payload = record.get("payload") if isinstance(record, dict) else None
                if not isinstance(payload, dict) or payload.get("type") != "message":
                    continue
                if payload.get("role") != "user":
                    continue
                content = payload.get("content", [])
                if isinstance(content, str):
                    texts.append(content)
                elif isinstance(content, list):
                    texts.extend(
                        str(item.get("text", ""))
                        for item in content
                        if isinstance(item, dict) and item.get("text")
                    )
    except OSError:
        return []
    return texts


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
    return any(
        session_log_has_markers(log_path, [marker], max_lines=max_lines)
        for marker in (
            netrunner_session_marker(session_id),
            _legacy_netrunner_session_marker(session_id),
        )
    )


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


def _file_birth_time(path: Path) -> datetime:
    try:
        stat_result = path.stat()
    except OSError:
        return datetime.now(timezone.utc)
    created = getattr(stat_result, "st_birthtime", None)
    if isinstance(created, (int, float)):
        return datetime.fromtimestamp(created, tz=timezone.utc)
    return datetime.fromtimestamp(stat_result.st_ctime, tz=timezone.utc)


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


def _first_string_for_keys(value: object, keys: Sequence[str]) -> str:
    if isinstance(value, dict):
        for key in keys:
            candidate = value.get(key)
            if isinstance(candidate, str) and candidate.strip():
                return candidate.strip()
        for nested in value.values():
            candidate = _first_string_for_keys(nested, keys)
            if candidate:
                return candidate
    elif isinstance(value, list):
        for nested in value:
            candidate = _first_string_for_keys(nested, keys)
            if candidate:
                return candidate
    return ""


def _provider_metadata_from_records(records: Sequence[dict[str, Any]]) -> tuple[str, str]:
    model = ""
    reasoning = ""
    for record in records:
        if not model:
            model = _first_string_for_keys(record, ("model", "modelId", "model_id"))
        if not reasoning:
            reasoning = _first_string_for_keys(
                record,
                ("reasoning", "reasoningEffort", "reasoning_effort", "effort"),
            )
        if model and reasoning:
            break
    return model, reasoning


def _subscription_provider_from_records(records: Sequence[dict[str, Any]]) -> str:
    for record in records:
        candidate = _first_string_for_keys(record, ("model_provider", "modelProvider", "provider")).lower()
        if candidate in {"openai", "opencode-go", "commandcode"}:
            return candidate
    return ""


def _canonical_subscription_model(model: str, subscription_provider: str) -> str:
    clean = model.strip()
    if not clean or subscription_provider == "openai" or clean.startswith(f"{subscription_provider}/"):
        return clean
    return f"{subscription_provider}/{clean}"


def _codex_summary_with_metadata(summary: Any, log_path: Path, cwd: Path) -> ResumeSessionSummary:
    records = _iter_jsonl_records(log_path)
    model, reasoning = _provider_metadata_from_records(records)
    subscription_provider = _subscription_provider_from_records(records) or codex_model_family_for_model(model)
    model = _canonical_subscription_model(model, subscription_provider)
    return _summary_with_metadata(
        provider="codex",
        session_id=str(summary.session_id),
        created=summary.created,
        updated=summary.updated,
        preview=str(getattr(summary, "preview", "") or ""),
        log_path=log_path,
        cwd=cwd,
        model=model,
        reasoning=reasoning,
        origin="codex_session_log",
        subscription_provider=subscription_provider,
    )


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


_ANTIGRAVITY_CONVERSATION_FALLBACK_PREVIEW = "(antigravity conversation)"


def _antigravity_skill_marker_variants(skill_name: str) -> tuple[str, ...]:
    return (
        f"/{skill_name}",
        f"Activate skill ${skill_name} immediately.",
        f"Use the `{skill_name}` skill immediately.",
        f"Use the {skill_name} skill immediately.",
    )


_ANTIGRAVITY_FIXER_MARKERS = tuple(
    marker
    for skill_name in ("init-fixer", "start-fixer")
    for marker in _antigravity_skill_marker_variants(skill_name)
)
_ANTIGRAVITY_NETRUNNER_MARKERS = tuple(
    marker
    for skill_name in ("hands-netrunner", "start-netrunner")
    for marker in _antigravity_skill_marker_variants(skill_name)
)
_ANTIGRAVITY_OVERSEER_MARKERS = tuple(
    marker
    for skill_name in ("init-overseer", "start-overseer")
    for marker in _antigravity_skill_marker_variants(skill_name)
)
_ANTIGRAVITY_PREVIEW_MARKER_SNIPPETS = (
    "skill immediately",
    "MCP selection",
    "Preselected compatibility session ID",
    "Preselected session ID",
    "Autonomous fixer Codex session ID",
)
_PRINTABLE_BYTES_RE = re.compile(rb"[\t\r\n -~]{4,}")


_ANTIGRAVITY_HANDS_MARKERS = (
    "Use its Project Hands Channel Mode for the current project.",
    "Project Hands Channel Mode",
    "disposable client of the one permanent project actor `Руки`",
    "disposable client of the one permanent project actor 'Руки'",
)


def _antigravity_store_root() -> Path:
    return Path.home() / ".gemini" / "antigravity-cli"


def _read_antigravity_last_conversation_id(store_root: Path, cwd: Path) -> str | None:
    mapping_path = store_root / "cache" / "last_conversations.json"
    try:
        raw_mapping = json.loads(mapping_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return None
    if not isinstance(raw_mapping, dict):
        return None

    cwd_text = str(cwd.resolve())
    for raw_cwd, raw_conversation_id in raw_mapping.items():
        if not isinstance(raw_cwd, str) or not isinstance(raw_conversation_id, str):
            continue
        try:
            if Path(raw_cwd).resolve() != cwd.resolve():
                continue
        except OSError:
            if raw_cwd != cwd_text:
                continue
        conversation_id = raw_conversation_id.strip()
        return conversation_id or None
    return None


def _iter_antigravity_conversation_ids_for_cwd(store_root: Path, cwd: Path) -> list[str]:
    seen: set[str] = set()
    conversation_ids: list[str] = []
    latest_id = _read_antigravity_last_conversation_id(store_root, cwd)
    if latest_id:
        seen.add(latest_id)
        conversation_ids.append(latest_id)

    cwd_text = str(cwd.resolve())
    for record in _iter_jsonl_records(store_root / "history.jsonl", max_lines=5000):
        if str(record.get("workspace", "") or "") != cwd_text:
            continue
        conversation_id = str(record.get("conversationId", "") or "").strip()
        if not conversation_id or conversation_id in seen:
            continue
        seen.add(conversation_id)
        conversation_ids.append(conversation_id)
    return conversation_ids


def _antigravity_conversation_file(store_root: Path, conversation_id: str) -> Path | None:
    transcript_path = (
        store_root / "brain" / conversation_id / ".system_generated" / "logs" / "transcript.jsonl"
    )
    if transcript_path.is_file():
        return transcript_path
    conversation_dir = store_root / "conversations"
    candidates = [
        conversation_dir / f"{conversation_id}.db",
        conversation_dir / f"{conversation_id}.pb",
    ]
    existing = [candidate for candidate in candidates if candidate.is_file()]
    if not existing:
        return None
    return max(existing, key=lambda item: _file_time(item))


def _printable_strings_from_binary_file(path: Path, *, max_bytes: int = 64_000_000) -> list[str]:
    try:
        data = path.read_bytes()[:max_bytes]
    except OSError:
        return []
    strings: list[str] = []
    for match in _PRINTABLE_BYTES_RE.finditer(data):
        clean = " ".join(match.group(0).decode("utf-8", errors="ignore").split())
        if clean:
            strings.append(clean)
    return strings


def _antigravity_role_texts(path: Path) -> list[str]:
    """Read role markers from conversation steps, not embedded global skill metadata."""
    if path.suffix.lower() == ".jsonl":
        texts: list[str] = []
        for record in _iter_jsonl_records(path, max_lines=10):
            content = str(record.get("content", "") or "")
            if content:
                texts.append(content)
        if texts:
            return texts
    if path.suffix.lower() == ".db":
        try:
            with sqlite3.connect(f"file:{path}?mode=ro", uri=True) as conn:
                rows = conn.execute(
                    "SELECT step_payload, metadata FROM steps ORDER BY idx LIMIT 3"
                ).fetchall()
            texts: list[str] = []
            for payload, metadata in rows:
                for blob in (payload, metadata):
                    if blob:
                        texts.extend(_printable_strings_from_binary_file_bytes(blob))
            if texts:
                return texts
        except (OSError, sqlite3.Error):
            pass
    return _printable_strings_from_binary_file(path)


def _printable_strings_from_binary_file_bytes(data: bytes) -> list[str]:
    strings: list[str] = []
    for match in _PRINTABLE_BYTES_RE.finditer(data):
        clean = " ".join(match.group(0).decode("utf-8", errors="ignore").split())
        if clean:
            strings.append(clean)
    return strings


def _first_marker_position(texts: Sequence[str], markers: Sequence[str]) -> int | None:
    joined = "\n".join(texts)
    positions = [position for marker in markers if (position := joined.find(marker)) >= 0]
    return min(positions) if positions else None


def _antigravity_conversation_has_role(texts: Sequence[str], role: str) -> bool | None:
    hands_pos = _first_marker_position(texts, _ANTIGRAVITY_HANDS_MARKERS)
    if hands_pos is not None:
        return role == "hands"
    if role == "hands":
        return False

    marker_sets = {
        "fixer": _ANTIGRAVITY_FIXER_MARKERS,
        "netrunner": _ANTIGRAVITY_NETRUNNER_MARKERS,
        "overseer": _ANTIGRAVITY_OVERSEER_MARKERS,
    }
    if role not in marker_sets:
        return None
    selected_markers = marker_sets[role]
    selected_position = _first_marker_position(texts, selected_markers)
    competing_positions = [
        position
        for competing_role, markers in marker_sets.items()
        if competing_role != role
        for position in (_first_marker_position(texts, markers),)
        if position is not None
    ]
    if selected_position is None:
        if competing_positions:
            return False
        return None
    if not competing_positions:
        return True
    return selected_position <= min(competing_positions)


def _antigravity_conversation_is_fixer(texts: Sequence[str]) -> bool | None:
    return _antigravity_conversation_has_role(texts, "fixer")


def _antigravity_preview_from_history(store_root: Path, cwd: Path, conversation_id: str) -> str | None:
    history_path = store_root / "history.jsonl"
    cwd_text = str(cwd.resolve())
    for record in _iter_jsonl_records(history_path, max_lines=5000):
        if str(record.get("conversationId", "") or "") != conversation_id:
            continue
        if str(record.get("workspace", "") or "") != cwd_text:
            continue
        display = " ".join(str(record.get("display", "") or "").split())
        if _is_informative_preview_text(display):
            return display
    return None


def _antigravity_preview_from_strings(texts: Sequence[str], *, fallback: str) -> str:
    for text in texts:
        clean = " ".join(text.split())
        if not _is_informative_preview_text(clean):
            continue
        if any(marker in clean for marker in _ANTIGRAVITY_PREVIEW_MARKER_SNIPPETS):
            continue
        if clean.startswith(("SQLite format", "CREATE TABLE", "CREATE INDEX")):
            continue
        if len(clean) > 240:
            continue
        return clean
    return fallback


def _load_antigravity_fixer_resume_summaries(
    cwd: Path,
    *,
    limit: int,
    store_root: Path | None = None,
) -> list[ResumeSessionSummary]:
    if limit <= 0:
        return []

    resolved_store_root = store_root or _antigravity_store_root()
    conversation_ids = _iter_antigravity_conversation_ids_for_cwd(resolved_store_root, cwd)
    if not conversation_ids:
        return []

    fixer_summaries: list[ResumeSessionSummary] = []
    for conversation_id in conversation_ids:
        conversation_file = _antigravity_conversation_file(resolved_store_root, conversation_id)
        if conversation_file is None:
            continue

        texts = _antigravity_role_texts(conversation_file)
        role_is_fixer = _antigravity_conversation_is_fixer(texts)
        if role_is_fixer is not True:
            continue

        created = _file_birth_time(conversation_file)
        updated = _file_time(conversation_file)
        preview = (
            _antigravity_preview_from_history(resolved_store_root, cwd, conversation_id)
            or _antigravity_preview_from_strings(texts, fallback=_ANTIGRAVITY_CONVERSATION_FALLBACK_PREVIEW)
        )
        summary = ResumeSessionSummary(
            provider="antigravity",
            session_id=conversation_id,
            created=created,
            updated=updated,
            preview=preview,
            log_path=conversation_file,
        )
        fixer_summaries.append(summary)

    fixer_summaries.sort(key=lambda summary: summary.updated, reverse=True)
    return fixer_summaries[:limit]


def _load_antigravity_overseer_resume_summaries(
    cwd: Path,
    *,
    limit: int,
    store_root: Path | None = None,
) -> list[ResumeSessionSummary]:
    if limit <= 0:
        return []

    resolved_store_root = store_root or _antigravity_store_root()
    summaries: list[ResumeSessionSummary] = []
    for conversation_id in _iter_antigravity_conversation_ids_for_cwd(resolved_store_root, cwd):
        conversation_file = _antigravity_conversation_file(resolved_store_root, conversation_id)
        if conversation_file is None:
            continue
        texts = _antigravity_role_texts(conversation_file)
        if _antigravity_conversation_has_role(texts, "overseer") is not True:
            continue
        summaries.append(
            _summary_with_metadata(
                provider="antigravity",
                session_id=conversation_id,
                created=_file_birth_time(conversation_file),
                updated=_file_time(conversation_file),
                preview=(
                    _antigravity_preview_from_history(resolved_store_root, cwd, conversation_id)
                    or _antigravity_preview_from_strings(
                        texts,
                        fallback=_ANTIGRAVITY_CONVERSATION_FALLBACK_PREVIEW,
                    )
                ),
                log_path=conversation_file,
                cwd=cwd,
                origin="antigravity_conversation_store",
            )
        )
    summaries.sort(key=lambda summary: summary.updated, reverse=True)
    return summaries[:limit]


def _commandcode_project_store_slug(cwd: Path) -> str:
    return re.sub(r"[^A-Za-z0-9]+", "-", str(cwd.resolve())).strip("-").lower()


def _load_commandcode_fixer_resume_summaries(
    cwd: Path,
    *,
    limit: int,
    session_is_fixer: Callable[[Path], bool],
    store_root: Path | None = None,
) -> list[ResumeSessionSummary]:
    project_dir = (store_root or (Path.home() / ".commandcode" / "projects")) / _commandcode_project_store_slug(cwd)
    if not project_dir.is_dir():
        return []

    summaries: list[ResumeSessionSummary] = []
    for log_path in sorted(project_dir.glob("*.jsonl"), key=_file_time, reverse=True):
        if log_path.name.endswith(".checkpoints.jsonl") or not session_is_fixer(log_path):
            continue
        records = _iter_jsonl_records(log_path)
        header = records[0] if records else {}
        session_id = str(header.get("id", "") or header.get("sessionId", "") or log_path.stem)
        created, updated = _summary_times_from_records(records, fallback=_file_time(log_path))
        model, reasoning = _provider_metadata_from_records(records)
        subscription_provider = "commandcode"
        model = _canonical_subscription_model(model, subscription_provider)
        summaries.append(
            _summary_with_metadata(
                provider="commandcode",
                session_id=session_id,
                created=created,
                updated=updated,
                preview=_preview_from_records(records, fallback=log_path.stem),
                log_path=log_path,
                cwd=cwd,
                model=model,
                reasoning=reasoning,
                origin="commandcode_session_store",
                subscription_provider=subscription_provider,
            )
        )
        if len(summaries) >= limit:
            break
    return summaries


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


def _load_claude_overseer_resume_summaries(
    cwd: Path,
    *,
    limit: int,
    session_is_overseer: Callable[[Path], bool],
    store_root: Path | None = None,
) -> list[ResumeSessionSummary]:
    project_dir = (store_root or (Path.home() / ".claude" / "projects")) / _project_store_slug(cwd)
    if not project_dir.is_dir():
        return []

    summaries: list[ResumeSessionSummary] = []
    for log_path in sorted(project_dir.glob("*.jsonl"), key=_file_time, reverse=True):
        if not session_is_overseer(log_path):
            continue
        records = _iter_jsonl_records(log_path)
        fallback = _file_time(log_path)
        session_id = next(
            (
                str(record.get("sessionId", "") or "").strip()
                for record in records
                if str(record.get("sessionId", "") or "").strip()
            ),
            log_path.stem,
        )
        created, updated = _summary_times_from_records(records, fallback=fallback)
        model, reasoning = _provider_metadata_from_records(records)
        summaries.append(
            _summary_with_metadata(
                provider="claude",
                session_id=session_id,
                created=created,
                updated=updated,
                preview=_preview_from_records(records, fallback=log_path.stem),
                log_path=log_path,
                cwd=cwd,
                model=model,
                reasoning=reasoning,
                origin="claude_project_log",
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


def _load_droid_overseer_resume_summaries(
    cwd: Path,
    *,
    limit: int,
    session_is_overseer: Callable[[Path], bool],
    store_root: Path | None = None,
) -> list[ResumeSessionSummary]:
    project_dir = (store_root or (Path.home() / ".factory" / "sessions")) / _project_store_slug(cwd)
    if not project_dir.is_dir():
        return []

    summaries: list[ResumeSessionSummary] = []
    for log_path in sorted(project_dir.glob("*.jsonl"), key=_file_time, reverse=True):
        records = _iter_jsonl_records(log_path)
        if records:
            recorded_cwd = str(records[0].get("cwd", "") or "")
            if recorded_cwd and Path(recorded_cwd).resolve() != cwd.resolve():
                continue
        if not session_is_overseer(log_path):
            continue
        fallback = _file_time(log_path)
        first_record = records[0] if records else {}
        session_id = str(first_record.get("id", "") or first_record.get("sessionId", "") or log_path.stem)
        created, updated = _summary_times_from_records(records, fallback=fallback)
        model, reasoning = _provider_metadata_from_records(records)
        summaries.append(
            _summary_with_metadata(
                provider="droid",
                session_id=session_id,
                created=created,
                updated=updated,
                preview=str(first_record.get("sessionTitle", "") or first_record.get("title", "") or log_path.stem),
                log_path=log_path,
                cwd=cwd,
                model=model,
                reasoning=reasoning,
                origin="droid_session_log",
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


def _load_junie_overseer_resume_summaries(
    cwd: Path,
    *,
    limit: int,
    session_is_overseer: Callable[[Path], bool],
    index_path: Path | None = None,
) -> list[ResumeSessionSummary]:
    resolved_index_path = index_path or (Path.home() / ".junie" / "sessions" / "index.jsonl")
    if not resolved_index_path.is_file():
        return []

    summaries: list[ResumeSessionSummary] = []
    fallback = _file_time(resolved_index_path)
    for record in _iter_jsonl_records(resolved_index_path, max_lines=1000):
        if str(record.get("projectDir", "") or "") != str(cwd.resolve()):
            continue
        session_id = str(record.get("sessionId", "") or "").strip()
        if not session_id:
            continue
        session_dir = resolved_index_path.parent / session_id
        marker_path = session_dir / "state.json"
        if not marker_path.is_file():
            marker_path = session_dir / "events.jsonl"
        if not marker_path.is_file() or not session_is_overseer(marker_path):
            continue
        created = _datetime_from_value(record.get("createdAt"), fallback=fallback)
        updated = _datetime_from_value(record.get("updatedAt"), fallback=created)
        model, reasoning = _provider_metadata_from_records([record])
        summaries.append(
            _summary_with_metadata(
                provider="junie",
                session_id=session_id,
                created=created,
                updated=updated,
                preview=str(record.get("taskName", "") or session_id),
                log_path=marker_path,
                cwd=cwd,
                model=model,
                reasoning=reasoning,
                origin="junie_session_index",
            )
        )
        if len(summaries) >= limit:
            break
    return summaries


def _kimi_workdir_hash(cwd: Path) -> str:
    return hashlib.md5(str(cwd.resolve()).encode("utf-8"), usedforsecurity=False).hexdigest()


def _kimi_store_root() -> Path:
    configured = os.environ.get("KIMI_SHARE_DIR", "").strip()
    return Path(configured).expanduser() if configured else Path.home() / ".kimi"


def _iter_kimi_session_dirs(
    cwd: Path,
    *,
    store_root: Path | None = None,
) -> list[tuple[str, Path, Path]]:
    roots: list[tuple[str, Path]] = [("kimi-code", store_root or _kimi_store_root())]

    found: list[tuple[str, Path, Path]] = []
    for provider, root in roots:
        sessions_root = root / "sessions"
        workdir_roots = [sessions_root / _kimi_workdir_hash(cwd)]

        for workdir_root in workdir_roots:
            if not workdir_root.is_dir():
                continue
            for session_dir in sorted(workdir_root.iterdir(), key=_file_time, reverse=True):
                context_path = session_dir / "context.jsonl"
                if session_dir.is_dir() and context_path.is_file():
                    found.append((provider, session_dir, context_path))
    return found


def _load_kimi_role_resume_summaries(
    cwd: Path,
    *,
    limit: int,
    session_is_role: Callable[[Path], bool],
    store_root: Path | None = None,
) -> list[ResumeSessionSummary]:
    if limit <= 0:
        return []

    summaries: list[ResumeSessionSummary] = []
    for provider, session_dir, context_path in _iter_kimi_session_dirs(
        cwd,
        store_root=store_root,
    ):
        if not session_is_role(context_path):
            continue
        records = _iter_jsonl_records(context_path)
        fallback = _file_time(context_path)
        created, updated = _summary_times_from_records(records, fallback=fallback)
        model, reasoning = _provider_metadata_from_records(records)
        state_path = session_dir / "state.json"
        state: dict[str, Any] = {}
        try:
            raw_state = json.loads(state_path.read_text(encoding="utf-8"))
            if isinstance(raw_state, dict):
                state = raw_state
        except (OSError, json.JSONDecodeError):
            pass
        summaries.append(
            _summary_with_metadata(
                provider=provider,
                session_id=session_dir.name,
                created=created,
                updated=updated,
                preview=str(state.get("title", "") or _preview_from_records(records, fallback=session_dir.name)),
                log_path=context_path,
                cwd=cwd,
                model=model,
                reasoning=reasoning,
                origin="kimi_session_store",
            )
        )
        if len(summaries) >= limit:
            break
    return summaries


def _load_kimi_overseer_resume_summaries(
    cwd: Path,
    *,
    limit: int,
    session_is_overseer: Callable[[Path], bool],
    store_root: Path | None = None,
) -> list[ResumeSessionSummary]:
    return _load_kimi_role_resume_summaries(
        cwd,
        limit=limit,
        session_is_role=session_is_overseer,
        store_root=store_root,
    )


def _load_kimi_fixer_resume_summaries(
    cwd: Path,
    *,
    limit: int,
    session_is_fixer: Callable[[Path], bool],
    store_root: Path | None = None,
) -> list[ResumeSessionSummary]:
    return _load_kimi_role_resume_summaries(
        cwd,
        limit=limit,
        session_is_role=session_is_fixer,
        store_root=store_root,
    )


# Every role bootstrap prompt this launcher submits starts with this phrase.
_ROLE_BOOTSTRAP_MARKERS = ("Activate skill $",)

# Pi keeps one agent store directory per resolved cwd under
# `<agent dir>/sessions/`. The directory name is Pi's own safe path from
# `getDefaultSessionDirPath()`: drop a single leading slash, replace every `/`,
# `\` and `:` with `-`, wrap the result in `--`. Session files inside are JSONL
# named `<created iso>_<session id>.jsonl`, and the first record is the session
# header that repeats the id and the cwd.
PI_SESSION_STORE_RELATIVE_ROOT = Path(".pi") / "agent" / "sessions"


def _pi_project_session_dir(cwd: Path, *, store_root: Path | None = None) -> Path:
    resolved = str(cwd.resolve())
    trimmed = resolved[1:] if resolved[:1] in {"/", "\\"} else resolved
    safe_path = f"--{re.sub(r'[/\\:]', '-', trimmed)}--"
    return (store_root or (Path.home() / PI_SESSION_STORE_RELATIVE_ROOT)) / safe_path


def _pi_session_id(log_path: Path, records: Sequence[dict[str, Any]]) -> str:
    for record in records:
        if str(record.get("type", "")).casefold() == "session":
            session_id = str(record.get("id", "") or "").strip()
            if session_id:
                return session_id
    return log_path.stem.split("_", 1)[-1].strip() or log_path.stem


def _pi_metadata_from_records(records: Sequence[dict[str, Any]]) -> tuple[str, str]:
    # Pi writes the model in a `model_change` record and the reasoning level in
    # a `thinking_level_change` record; neither uses the generic key names the
    # other providers' metadata reader looks for.
    model = ""
    reasoning = ""
    for record in records:
        if not model:
            model = _first_string_for_keys(record, ("modelId", "model_id"))
        if not reasoning:
            reasoning = _first_string_for_keys(record, ("thinkingLevel",))
        if model and reasoning:
            break
    return model, reasoning


def _pi_preview_from_records(records: Sequence[dict[str, Any]], *, fallback: str) -> str:
    # Every provider store starts with the same launcher bootstrap prompt. Pi
    # keeps it verbatim, so the raw first user message would make every Fixer
    # entry in the picker look identical.
    for record in records:
        message = record.get("message")
        if not isinstance(message, dict):
            continue
        for text in _message_preview_texts(message):
            clean = " ".join(str(text).split())
            if not _is_informative_preview_text(clean):
                continue
            if any(marker in clean for marker in _ROLE_BOOTSTRAP_MARKERS):
                continue
            return clean
    return _preview_from_records(records, fallback=fallback)


def _load_pi_fixer_resume_summaries(
    cwd: Path,
    *,
    limit: int,
    session_is_fixer: Callable[[Path], bool],
    store_root: Path | None = None,
) -> list[ResumeSessionSummary]:
    if limit <= 0:
        return []

    project_dir = _pi_project_session_dir(cwd, store_root=store_root)
    if not project_dir.is_dir():
        return []

    summaries: list[ResumeSessionSummary] = []
    for log_path in sorted(project_dir.glob("*.jsonl"), key=_file_time, reverse=True):
        if not session_is_fixer(log_path):
            continue
        records = _iter_jsonl_records(log_path)
        header = records[0] if records else {}
        recorded_cwd = str(header.get("cwd", "") or "")
        if recorded_cwd and Path(recorded_cwd).resolve() != cwd.resolve():
            continue
        fallback = _file_time(log_path)
        created, updated = _summary_times_from_records(records, fallback=fallback)
        # Pi appends to a full-transcript JSONL, so the record scan above stops
        # well before the last turn of a long session. The file mtime is the
        # honest last-activity time and the picker sorts on it.
        updated = max(updated, fallback)
        model, reasoning = _pi_metadata_from_records(records)
        summaries.append(
            _summary_with_metadata(
                provider="pi",
                session_id=_pi_session_id(log_path, records),
                created=created,
                updated=updated,
                preview=_pi_preview_from_records(records, fallback=log_path.stem),
                log_path=log_path,
                cwd=cwd,
                model=model,
                reasoning=reasoning,
                origin="pi_agent_session_store",
                subscription_provider=_subscription_provider_from_records(records),
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
                if log_path := find_session_log(summary.session_id, created=summary.created, updated=summary.updated):
                    fixer_summaries.append(_codex_summary_with_metadata(summary, log_path, cwd))
                else:
                    fixer_summaries.append(wrap_resume_summary(summary, "codex"))
                if len(fixer_summaries) >= limit:
                    break
                continue
            log_path = find_session_log(summary.session_id, created=summary.created, updated=summary.updated)
            if not log_path:
                continue
            if not session_is_fixer(log_path):
                continue
            fixer_summaries.append(_codex_summary_with_metadata(summary, log_path, cwd))
            if len(fixer_summaries) >= limit:
                break

    provider_loaders = (
        _load_commandcode_fixer_resume_summaries,
        _load_claude_fixer_resume_summaries,
        _load_droid_fixer_resume_summaries,
        _load_junie_fixer_resume_summaries,
        _load_antigravity_fixer_resume_summaries,
        _load_kimi_fixer_resume_summaries,
        _load_pi_fixer_resume_summaries,
    )
    for provider_loader in provider_loaders:
        remaining = limit
        if provider_loader is _load_antigravity_fixer_resume_summaries:
            fixer_summaries.extend(provider_loader(cwd, limit=remaining))
        else:
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


def codex_session_log_has_active_writer(log_path: Path | str | None) -> bool:
    if not log_path:
        return False
    try:
        result = subprocess.run(
            ["lsof", "-t", str(log_path)],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
            check=False,
        )
    except OSError:
        return False
    return result.returncode == 0


def load_overseer_resume_summaries(
    cwd: Path,
    *,
    limit: int = 40,
    load_cwd_summaries: Callable[..., tuple[Any, list[Any]]] = load_cwd_session_summaries,
    session_is_overseer: Callable[[Path], bool],
) -> list[Any]:
    overseer_summaries: list[ResumeSessionSummary] = []
    codex_error: RuntimeError | None = None
    try:
        find_session_log, summaries = load_cwd_summaries(cwd, limit=limit)
    except RuntimeError as err:
        codex_error = RuntimeError("Unable to load Codex history helpers for Overseer resume flow.")
    else:
        for summary in summaries:
            log_path = find_session_log(summary.session_id, created=summary.created, updated=summary.updated)
            if not log_path or not session_is_overseer(log_path):
                continue
            overseer_summaries.append(_codex_summary_with_metadata(summary, log_path, cwd))
            if len(overseer_summaries) >= limit:
                break

    provider_loaders: tuple[Callable[..., list[ResumeSessionSummary]], ...] = (
        _load_claude_overseer_resume_summaries,
        _load_droid_overseer_resume_summaries,
        _load_junie_overseer_resume_summaries,
        _load_antigravity_overseer_resume_summaries,
        _load_kimi_overseer_resume_summaries,
    )
    for provider_loader in provider_loaders:
        remaining = max(limit - len(overseer_summaries), 0)
        if remaining <= 0:
            break
        if provider_loader is _load_antigravity_overseer_resume_summaries:
            overseer_summaries.extend(provider_loader(cwd, limit=remaining))
        else:
            overseer_summaries.extend(
                provider_loader(
                    cwd,
                    limit=remaining,
                    session_is_overseer=session_is_overseer,
                )
            )

    if not overseer_summaries and codex_error is not None:
        raise codex_error
    overseer_summaries.sort(key=lambda summary: summary.updated, reverse=True)
    return overseer_summaries[:limit]


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
    summaries = load_fixer_resume_summaries(cwd, limit=8)
    if not summaries:
        raise RuntimeError("No existing Fixer sessions were found for this project cwd.")
    active_codex_count = 0
    for summary in summaries:
        provider = summary_provider(summary)
        if is_codex_backend(provider) and codex_session_log_has_active_writer(getattr(summary, "log_path", None)):
            active_codex_count += 1
            continue
        return format_fixer_resume_selection(
            provider,
            str(summary.session_id),
            summary_subscription_provider(summary),
        )
    if active_codex_count:
        raise RuntimeError(
            "All discovered Codex Fixer sessions are currently active. "
            "Close the existing Codex writer or pass --fixer-session-id for a different session."
        )
    raise RuntimeError("No existing Fixer sessions were found for this project cwd.")


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
    if not is_codex_backend(backend):
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
