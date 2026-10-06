"""External session discovery and provider-neutral Netrunner transcripts."""

from __future__ import annotations

import json
import os
import re
import time
from dataclasses import asdict, dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Callable


@dataclass(frozen=True)
class ProviderThreadCapability:
    """Dashboard-facing capabilities for one provider-backed thread."""

    backend: str
    transcript_availability: str
    continuation_supported: bool
    continuation_mode: str
    unsupported_reason: str = ""


@dataclass(frozen=True)
class ProviderThreadMessage:
    """Small, provider-neutral message representation used by the dashboard."""

    id: str
    role: str
    text: str
    created_at: str = ""
    source: str = "provider_transcript"


_PROVIDER_THREAD_CAPABILITIES = {
    "codex": ProviderThreadCapability("codex", "jsonl", True, "headless_resume"),
    "commandcode": ProviderThreadCapability("commandcode", "jsonl", True, "headless_resume"),
    "droid": ProviderThreadCapability("droid", "jsonl", True, "headless_resume"),
    "pi": ProviderThreadCapability("pi", "jsonl", True, "headless_resume"),
    "claude": ProviderThreadCapability(
        "claude",
        "metadata_only",
        False,
        "unsupported",
        "Claude resume needs a scoped MCP runtime config that the dashboard does not yet materialize.",
    ),
    "kimi-code": ProviderThreadCapability(
        "kimi-code",
        "metadata_only",
        False,
        "unsupported",
        "Kimi resume needs its project-scoped MCP config file; direct dashboard continuation is not wired yet.",
    ),
    "antigravity": ProviderThreadCapability(
        "antigravity",
        "metadata_only",
        False,
        "unsupported",
        "Antigravity exposes conversation metadata, but the dashboard cannot read or continue the conversation safely yet.",
    ),
    "junie": ProviderThreadCapability(
        "junie",
        "metadata_only",
        False,
        "unsupported",
        "Junie session metadata can be retained, but dashboard continuation is not implemented yet.",
    ),
    "grok": ProviderThreadCapability(
        "grok",
        "metadata_only",
        False,
        "unsupported",
        "Grok session metadata can be retained, but dashboard continuation is not implemented yet.",
    ),
}


def _normalize_thread_backend(backend: str) -> str:
    normalized = backend.strip().lower().replace("_", "-")
    aliases = {
        "agy": "antigravity",
        "claude-code": "claude",
        "kimi": "kimi-code",
        "kimi-cli": "kimi-code",
        "factory": "droid",
    }
    return aliases.get(normalized, normalized)


def _provider_thread_capability(backend: str) -> ProviderThreadCapability:
    """Return an explicit capability result; never silently default to Codex."""

    normalized = _normalize_thread_backend(backend)
    if not normalized:
        return ProviderThreadCapability(
            "",
            "unavailable",
            False,
            "awaiting_backend",
            "This manual Netrunner has not selected or launched a backend yet.",
        )
    capability = _PROVIDER_THREAD_CAPABILITIES.get(normalized)
    if capability is not None:
        return capability
    return ProviderThreadCapability(
        normalized,
        "metadata_only",
        False,
        "unsupported",
        f"Provider {normalized!r} has no dashboard transcript or continuation adapter.",
    )


def _provider_thread_capability_payload(backend: str) -> dict[str, object]:
    return asdict(_provider_thread_capability(backend))


def _message_text(value: object) -> str:
    if isinstance(value, str):
        return value.strip()
    if isinstance(value, list):
        parts = [_message_text(item) for item in value]
        return "\n".join(part for part in parts if part).strip()
    if not isinstance(value, dict):
        return ""
    for key in ("text", "message", "content", "output_text", "input_text", "value"):
        if key in value:
            text = _message_text(value[key])
            if text:
                return text
    return ""


def _normalize_message_role(value: object, record_type: str) -> str:
    role = str(value or "").strip().lower()
    if role in {"assistant", "agent", "model"}:
        return "assistant"
    if role in {"user", "human"}:
        return "user"
    lowered_type = record_type.lower()
    if "assistant" in lowered_type or "agent" in lowered_type:
        return "assistant"
    if "user" in lowered_type or "human" in lowered_type:
        return "user"
    return ""


def _provider_message_from_payload(
    payload: object,
    *,
    line_number: int,
    backend: str,
) -> ProviderThreadMessage | None:
    if not isinstance(payload, dict):
        return None
    nested = payload.get("payload")
    record = nested if isinstance(nested, dict) else payload
    record_type = str(record.get("type") or payload.get("type") or "").strip()
    role = _normalize_message_role(record.get("role"), record_type)
    text = ""
    nested_message = record.get("message")
    if not role and isinstance(nested_message, dict):
        # Pi JSONL nests the conversation message under "message".
        role = _normalize_message_role(nested_message.get("role"), record_type)
        text = _message_text(nested_message.get("content")) or _message_text(nested_message)
    if not role:
        return None
    if not text:
        text = (
            _message_text(record.get("content"))
            or _message_text(record.get("message"))
            or _message_text(record.get("text"))
        )
    if not text:
        return None
    created_at = str(payload.get("timestamp") or record.get("timestamp") or "").strip()
    message_id = str(
        record.get("id") or payload.get("id") or f"{backend}-{line_number}"
    ).strip()
    return ProviderThreadMessage(
        id=message_id,
        role=role,
        text=text,
        created_at=created_at,
    )


def _load_provider_thread_messages(
    backend: str,
    transcript_path: Path,
    *,
    limit: int = 400,
) -> list[ProviderThreadMessage]:
    """Read supported JSONL transcripts without leaking raw tool/event records."""

    capability = _provider_thread_capability(backend)
    if capability.transcript_availability != "jsonl" or limit <= 0:
        return []
    messages: list[ProviderThreadMessage] = []
    try:
        with transcript_path.open("r", encoding="utf-8") as fh:
            for line_number, raw_line in enumerate(fh, start=1):
                try:
                    payload = json.loads(raw_line)
                except json.JSONDecodeError:
                    continue
                message = _provider_message_from_payload(
                    payload,
                    line_number=line_number,
                    backend=capability.backend,
                )
                if message is None:
                    continue
                if (
                    messages
                    and messages[-1].role == message.role
                    and messages[-1].text == message.text
                ):
                    continue
                messages.append(message)
    except OSError:
        return []
    return messages[-limit:]


def _provider_thread_message_payloads(
    backend: str,
    transcript_path: Path,
    *,
    limit: int = 400,
) -> list[dict[str, str]]:
    return [
        asdict(message)
        for message in _load_provider_thread_messages(
            backend,
            transcript_path,
            limit=limit,
        )
    ]


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


def _commandcode_sessions_root() -> Path:
    return Path(os.environ.get("COMMANDCODE_HOME", str(Path.home() / ".commandcode"))) / "projects"


def _antigravity_cli_log_root() -> Path:
    return Path.home() / ".gemini" / "antigravity-cli" / "log"


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


def _commandcode_session_id_from_transcript(path: Path, cwd: Path) -> str | None:
    expected_cwd = str(cwd.resolve())
    try:
        with path.open("r", encoding="utf-8") as fh:
            for raw_line in fh:
                try:
                    payload = json.loads(raw_line)
                except json.JSONDecodeError:
                    continue
                payload_cwd = _extract_codex_cwd_from_payload(payload)
                if not payload_cwd:
                    continue
                try:
                    payload_cwd = str(Path(payload_cwd).resolve())
                except OSError:
                    pass
                if payload_cwd != expected_cwd:
                    continue
                return _extract_codex_session_id_from_payload(payload) or path.stem
    except OSError:
        return None
    return None


def _find_new_commandcode_session_id_from_transcript_store(
    cwd: Path,
    *,
    launch_started_at: float | None,
    sessions_root: Path | None = None,
    commandcode_sessions_root_fn: Callable[[], Path] = _commandcode_sessions_root,
) -> str | None:
    root = sessions_root if sessions_root is not None else commandcode_sessions_root_fn()
    for path in _candidate_codex_transcript_paths(root, launch_started_at=launch_started_at):
        session_id = _commandcode_session_id_from_transcript(path, cwd)
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


_ANTIGRAVITY_CONVERSATION_ID_RE = re.compile(
    r"\b(?:conversation=|Created conversation |Streaming conversation |Resuming conversation )"
    r"([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})\b"
)


def _extract_antigravity_conversation_id_from_line(raw_line: str) -> str | None:
    match = _ANTIGRAVITY_CONVERSATION_ID_RE.search(raw_line)
    if match:
        return match.group(1).strip()
    return None


def _candidate_antigravity_cli_log_paths(
    log_root: Path,
    *,
    launch_started_at: float | None,
) -> list[Path]:
    if not log_root.is_dir():
        return []

    cutoff = (launch_started_at - 1.0) if launch_started_at is not None else None
    candidates: list[tuple[float, Path]] = []
    for path in log_root.glob("cli-*.log"):
        try:
            stat = path.stat()
        except OSError:
            continue
        if cutoff is not None and stat.st_mtime < cutoff:
            continue
        candidates.append((stat.st_mtime, path))
    return [path for _mtime, path in sorted(candidates, reverse=True)]


def _antigravity_conversation_id_from_cli_log(path: Path, cwd: Path) -> str | None:
    expected_cwd = str(cwd.resolve())
    matched_workspace = False
    conversation_id: str | None = None
    try:
        with path.open("r", encoding="utf-8") as fh:
            for raw_line in fh:
                if "workspaceDirs=[" in raw_line and expected_cwd in raw_line:
                    matched_workspace = True
                    if conversation_id:
                        return conversation_id

                extracted_id = _extract_antigravity_conversation_id_from_line(raw_line)
                if extracted_id:
                    conversation_id = extracted_id
                    if matched_workspace:
                        return conversation_id
    except OSError:
        return None
    return conversation_id if matched_workspace else None


def _find_new_antigravity_conversation_id_from_cli_logs(
    cwd: Path,
    *,
    launch_started_at: float | None,
    log_root: Path | None = None,
    antigravity_cli_log_root_fn: Callable[[], Path] = _antigravity_cli_log_root,
) -> str | None:
    root = log_root if log_root is not None else antigravity_cli_log_root_fn()
    for path in _candidate_antigravity_cli_log_paths(root, launch_started_at=launch_started_at):
        conversation_id = _antigravity_conversation_id_from_cli_log(path, cwd)
        if conversation_id:
            return conversation_id
    return None


def _wait_for_new_antigravity_conversation_id(
    cwd: Path,
    *,
    launch_started_at: float | None = None,
    timeout_sec: float = 8.0,
    find_new_antigravity_conversation_id_from_cli_logs_fn: Callable[..., str | None] = (
        _find_new_antigravity_conversation_id_from_cli_logs
    ),
) -> str | None:
    deadline = time.time() + timeout_sec
    while time.time() < deadline:
        conversation_id = find_new_antigravity_conversation_id_from_cli_logs_fn(
            cwd,
            launch_started_at=launch_started_at,
        )
        if conversation_id:
            return conversation_id
        time.sleep(0.5)
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


def _wait_for_new_commandcode_session_id(
    cwd: Path,
    before: str | None,
    *,
    launch_started_at: float | None = None,
    timeout_sec: float = 8.0,
) -> str | None:
    del before
    deadline = time.time() + timeout_sec
    while time.time() < deadline:
        session_id = _find_new_commandcode_session_id_from_transcript_store(
            cwd,
            launch_started_at=launch_started_at,
        )
        if session_id:
            return session_id
        time.sleep(0.5)
    return None


def _pi_sessions_root() -> Path:
    """Pi agent session store: ``~/.pi/agent/sessions`` (mirrors pi_adapter)."""
    return Path.home() / ".pi" / "agent" / "sessions"


def _pi_project_dir_name(cwd: Path) -> str:
    """Mirror pi's session directory encoding: strip one leading separator,
    replace '/', '\\' and ':' with '-', wrap in '--...--'."""
    cleaned = os.path.normpath(str(cwd))
    trimmed = cleaned[1:] if cleaned[:1] in ("/", "\\") else cleaned
    safe = trimmed.replace("/", "-").replace("\\", "-").replace(":", "-")
    return f"--{safe}--"


def _pi_filename_session_id(name: str) -> str | None:
    """Session identity encoded in a Pi transcript filename: pi writes
    ``<timestamp>_<id>.jsonl``; a bare ``<id>.jsonl`` stem is the identity.
    Substring matches are never accepted as identities."""
    if not name.endswith(".jsonl"):
        return None
    stem = name[: -len(".jsonl")]
    if not stem:
        return None
    if "_" in stem:
        candidate = stem.rsplit("_", 1)[1]
        return candidate or None
    return stem or None


def _pi_session_header_from_transcript(path: Path) -> dict[str, str] | None:
    """Read the leading ``{"type":"session","id":...,"cwd":...,"timestamp":...}``
    record of a Pi JSONL transcript. The original file is only read, never
    modified."""
    try:
        with path.open("r", encoding="utf-8") as fh:
            for index, raw_line in enumerate(fh):
                if index >= 8:
                    break
                try:
                    payload = json.loads(raw_line)
                except json.JSONDecodeError:
                    continue
                if not isinstance(payload, dict):
                    continue
                if str(payload.get("type") or "").strip() != "session":
                    continue
                header: dict[str, str] = {}
                for key in ("id", "session_id", "sessionId"):
                    value = payload.get(key)
                    if value not in (None, ""):
                        header["id"] = str(value).strip()
                        break
                for key in ("cwd", "current_working_directory", "workingDirectory"):
                    value = payload.get(key)
                    if value not in (None, ""):
                        header["cwd"] = str(value).strip()
                        break
                for key in ("timestamp", "created_at", "createdAt"):
                    value = payload.get(key)
                    if value not in (None, ""):
                        header["timestamp"] = str(value).strip()
                        break
                return header
    except OSError:
        return None
    return None


def _pi_transcript_proven_identity(path: Path, expected_cwd: Path) -> str | None:
    """Return the proven session id of one Pi transcript file, or None.

    Provenance requires agreement between the filename identity and the
    session header identity plus a matching header cwd. A header that
    contradicts the filename (and is not an embedded id) is contradictory
    provenance and yields None — never a guessed binding.
    """
    header = _pi_session_header_from_transcript(path) or {}
    header_id = header.get("id") or None
    filename_id = _pi_filename_session_id(path.name)
    if header_id and filename_id and header_id != filename_id:
        if header_id not in filename_id and filename_id not in header_id:
            return None
    identity = header_id or filename_id
    if not identity:
        return None
    header_cwd = header.get("cwd") or ""
    if not header_cwd:
        return None
    try:
        same_cwd = Path(header_cwd).resolve() == Path(expected_cwd).resolve()
    except OSError:
        same_cwd = header_cwd == str(expected_cwd)
    if not same_cwd:
        return None
    return identity


def _find_new_pi_session_id_from_transcript_store(
    cwd: Path,
    *,
    launch_started_at: float | None,
    sessions_root: Path | None = None,
    pi_sessions_root_fn: Callable[[], Path] = _pi_sessions_root,
) -> str | None:
    """Detect the Pi session JSONL a launch just produced (fresh run or a
    resumed attempt appending/updating its file) with proven id/cwd identity.
    No arbitrary newest file is ever returned."""
    root = sessions_root if sessions_root is not None else pi_sessions_root_fn()
    if not root.is_dir():
        return None
    cutoff = (launch_started_at - 1.0) if launch_started_at is not None else None
    candidates: list[tuple[float, Path]] = []
    for path in root.rglob("*.jsonl"):
        try:
            stat = path.stat()
        except OSError:
            continue
        if cutoff is not None and stat.st_mtime < cutoff:
            continue
        candidates.append((stat.st_mtime, path))
    for _mtime, path in sorted(candidates, reverse=True):
        identity = _pi_transcript_proven_identity(path, cwd)
        if identity:
            return identity
    return None


def _pi_transcript_attempt_evidence(
    cwd: Path,
    session_id: str,
    *,
    sessions_root: Path | None = None,
    pi_sessions_root_fn: Callable[[], Path] = _pi_sessions_root,
) -> list[dict[str, object]]:
    """Durable evidence records for every transcript file provably bound to
    one Pi session id (fresh and resumed/continuation attempts), ordered
    chronologically. Records carry id/cwd/time provenance so a continuation
    history is unambiguously linked to the current worker head."""
    resolved_id = (session_id or "").strip()
    if not resolved_id:
        return []
    root = sessions_root if sessions_root is not None else pi_sessions_root_fn()
    if not root.is_dir():
        return []
    records: list[dict[str, object]] = []
    for path in sorted(root.rglob("*.jsonl")):
        header = _pi_session_header_from_transcript(path) or {}
        header_id = header.get("id") or ""
        filename_id = _pi_filename_session_id(path.name) or ""
        if resolved_id not in (header_id, filename_id):
            continue
        if header_id and filename_id and header_id != filename_id:
            if header_id not in filename_id and filename_id not in header_id:
                continue
        try:
            stat = path.stat()
        except OSError:
            continue
        records.append(
            {
                "path": str(path),
                "bytes": stat.st_size,
                "mtime_epoch": stat.st_mtime,
                "session_id": resolved_id,
                "session_cwd": header.get("cwd") or "",
                "session_timestamp": header.get("timestamp") or "",
                "header_identity": header_id,
                "filename_identity": filename_id,
            }
        )
    records.sort(key=lambda record: (str(record.get("session_timestamp") or ""), float(record.get("mtime_epoch") or 0.0)))
    return records


def _pi_timestamp_epoch(timestamp: str) -> float | None:
    """Parse a Pi session-header timestamp into epoch seconds, or None."""
    trimmed = (timestamp or "").strip()
    if not trimmed:
        return None
    try:
        parsed = datetime.fromisoformat(trimmed.replace("Z", "+00:00"))
    except ValueError:
        return None
    if parsed.tzinfo is None:
        parsed = parsed.replace(tzinfo=timezone.utc)
    return parsed.timestamp()


def _pi_transcript_files_proven_for_cwd(
    cwd: Path,
    *,
    since_epoch: float | None = None,
    sessions_root: Path | None = None,
    pi_sessions_root_fn: Callable[[], Path] = _pi_sessions_root,
) -> list[dict[str, object]]:
    """Durable evidence records for every Pi transcript provably bound to one
    worker cwd at or after a launch bound (used when the external session id
    could not be detected at launch). Only files with proven id/cwd identity
    are returned — a missing detection never falls back to an arbitrary newest
    file, and a contradictory header is never bound."""
    resolved_cwd = Path(cwd)
    root = sessions_root if sessions_root is not None else pi_sessions_root_fn()
    if not root.is_dir():
        return []
    records: list[dict[str, object]] = []
    for path in sorted(root.rglob("*.jsonl")):
        identity = _pi_transcript_proven_identity(path, resolved_cwd)
        if not identity:
            continue
        header = _pi_session_header_from_transcript(path) or {}
        try:
            stat = path.stat()
        except OSError:
            continue
        timestamp = str(header.get("timestamp") or "")
        if since_epoch is not None:
            started = _pi_timestamp_epoch(timestamp)
            if started is None:
                started = stat.st_mtime
            if started < float(since_epoch) - 1.0:
                continue
        records.append(
            {
                "path": str(path),
                "bytes": stat.st_size,
                "mtime_epoch": stat.st_mtime,
                "session_id": identity,
                "session_cwd": header.get("cwd") or "",
                "session_timestamp": timestamp,
                "header_identity": header.get("id") or "",
                "filename_identity": _pi_filename_session_id(path.name) or "",
            }
        )
    records.sort(key=lambda record: (str(record.get("session_timestamp") or ""), float(record.get("mtime_epoch") or 0.0)))
    return records


def _wait_for_new_pi_session_id(
    cwd: Path,
    before: str | None,
    *,
    launch_started_at: float | None = None,
    timeout_sec: float = 8.0,
    find_new_pi_session_id_from_transcript_store_fn: Callable[..., str | None] = _find_new_pi_session_id_from_transcript_store,
    pi_transcript_attempt_evidence_fn: Callable[..., list[dict[str, object]]] = _pi_transcript_attempt_evidence,
) -> str | None:
    """Wait for the Pi session identity of a launch.

    Fresh runs are detected from the new session JSONL (proven id/cwd). A
    resumed run that has not appended yet falls back to the already-known id
    ``before`` — but only when real transcript files with proven identity
    exist for it, so a missing file never silently becomes a bound session.
    """
    deadline = time.time() + timeout_sec
    while time.time() < deadline:
        session_id = find_new_pi_session_id_from_transcript_store_fn(
            cwd,
            launch_started_at=launch_started_at,
        )
        if session_id:
            return session_id
        resumed_id = (before or "").strip()
        if resumed_id and pi_transcript_attempt_evidence_fn(cwd, resumed_id):
            return resumed_id
        time.sleep(0.5)
    return None


def attempt_manifest_path(cwd: Path, local_session_id: int, backend: str) -> Path:
    """Durable append-only attempt manifest location (no DB schema needed):
    one JSONL file per worker session and backend."""
    return Path(cwd) / ".codex" / "netrunner_attempt_manifests" / f"session-{int(local_session_id)}-{backend}.jsonl"


def append_attempt_manifest_record(path: Path, record: dict[str, object]) -> dict[str, object]:
    """Append one attempt record to the manifest. The manifest is append-only:
    attempt numbers derive from the existing lines, existing history is never
    rewritten, and each record carries the proven identity/continuation
    evidence for that attempt."""
    path.parent.mkdir(parents=True, exist_ok=True)
    attempt_number = 1
    try:
        with path.open("r", encoding="utf-8") as fh:
            for line in fh:
                if line.strip():
                    attempt_number += 1
    except OSError:
        pass
    payload = dict(record)
    payload["attempt_number"] = attempt_number
    with path.open("a", encoding="utf-8") as fh:
        fh.write(json.dumps(payload, ensure_ascii=False, sort_keys=True) + "\n")
    return payload


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
    wait_for_new_commandcode_session_id_fn: Callable[..., str | None],
    wait_for_new_droid_session_id_fn: Callable[..., str | None],
    wait_for_new_antigravity_conversation_id_fn: Callable[..., str | None],
    wait_for_new_pi_session_id_fn: Callable[..., str | None] = _wait_for_new_pi_session_id,
) -> str | None:
    normalized_backend = normalize_backend_name_fn(backend)
    if normalized_backend in {"codex", "commandcode"}:
        if normalized_backend == "commandcode":
            return wait_for_new_commandcode_session_id_fn(
                cwd,
                before,
                launch_started_at=launch_started_at,
                timeout_sec=timeout_sec,
            )
        return wait_for_new_codex_session_id_fn(cwd, before, timeout_sec=timeout_sec)
    if normalized_backend == "droid":
        return wait_for_new_droid_session_id_fn(
            log_path,
            cwd,
            launch_started_at=launch_started_at,
            timeout_sec=timeout_sec,
        )
    if normalized_backend == "antigravity":
        return wait_for_new_antigravity_conversation_id_fn(
            cwd,
            launch_started_at=launch_started_at,
            timeout_sec=timeout_sec,
        )
    if normalized_backend == "pi":
        return wait_for_new_pi_session_id_fn(
            cwd,
            before,
            launch_started_at=launch_started_at,
            timeout_sec=timeout_sec,
        )
    return None
