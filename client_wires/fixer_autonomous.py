#!/usr/bin/env python3
"""Helpers for serial autonomous Fixer/Netrunner execution."""

from __future__ import annotations

import argparse
import json
import os
import re
import sqlite3
import subprocess
import sys
import time
from pathlib import Path
from types import SimpleNamespace
from typing import Any

if __package__ in (None, ""):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from client_wires.bootstrap import bootstrap_codex_pro_import_path
from client_wires import fixer_wire

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


def _load_or_initialize_launch_state(cwd: Path, fixer_session_id: str | None) -> dict[str, Any]:
    path = _state_path(cwd)
    if path.is_file():
        payload = _load_state(cwd)
    else:
        payload = {
            "mode": "serial_autonomous_resolution",
            "workflow_type": "ghost_run",
            "workflow_label": "Ghost Run",
            "project_cwd": str(cwd),
            "fixer_codex_session_id": _resolve_fixer_session_id(cwd, fixer_session_id),
            "active_netrunner_session_id": None,
            "active_netrunner_session_ids": [],
            "updated_at_epoch": int(time.time()),
        }
        _save_state(cwd, payload)

    if not str(payload.get("fixer_codex_session_id", "")).strip():
        payload["fixer_codex_session_id"] = _resolve_fixer_session_id(cwd, fixer_session_id)
        payload["updated_at_epoch"] = int(time.time())
        _save_state(cwd, payload)

    _set_active_netrunner_session_ids(payload, _normalize_active_netrunner_session_ids(payload))

    return payload


def _latest_fixer_session_id(cwd: Path) -> str:
    summaries = fixer_wire._load_fixer_resume_summaries(cwd, limit=1)
    if not summaries:
        raise RuntimeError(
            f"No fixer Codex session was found for cwd: {cwd}. "
            "Open a Fixer session first, then rerun register-fixer."
        )
    return str(summaries[0].session_id)


def _fixer_session_id_from_env() -> str | None:
    for env_name in ("CODEX_THREAD_ID", "CODEX_SESSION_ID"):
        value = os.environ.get(env_name, "").strip()
        if value:
            return value
    return None


def _resolve_fixer_session_id(cwd: Path, fixer_session_id: str | None) -> str:
    explicit = (fixer_session_id or "").strip()
    if explicit:
        return explicit

    env_session_id = _fixer_session_id_from_env()
    if env_session_id:
        return env_session_id

    return _latest_fixer_session_id(cwd)


def register_fixer_session(cwd: Path, fixer_session_id: str | None) -> Path:
    resolved_session_id = _resolve_fixer_session_id(cwd, fixer_session_id)
    payload = {
        "mode": "serial_autonomous_resolution",
        "workflow_type": "ghost_run",
        "workflow_label": "Ghost Run",
        "project_cwd": str(cwd),
        "fixer_codex_session_id": resolved_session_id,
        "active_netrunner_session_id": None,
        "active_netrunner_session_ids": [],
        "updated_at_epoch": int(time.time()),
    }
    path = _save_state(cwd, payload)
    print(f"[fixer-autonomous] registered fixer session {resolved_session_id} in {path}")
    return path


def _build_common_codex_env(adapter: Any, llm_selection: Any) -> dict[str, str]:
    from codex_pro_app.main import _load_llm_env, _merge_env_with_os

    llm_env = _load_llm_env()
    env = _merge_env_with_os(llm_env)
    adapter.prepare_env(env, llm_selection)
    return env


def _build_exec_prefix(cwd: Path) -> tuple[list[str], dict[str, str], Any]:
    bootstrap_codex_pro_import_path()
    available_servers, config_env_vars, adapter, ensure_sqlite_scaffold = fixer_wire._load_available_servers(cwd)
    from codex_pro_app.main import LLMSelection

    llm_selection = LLMSelection(
        display_model=fixer_wire.FIXER_WIRE_MODEL,
        detail=fixer_wire.FIXER_WIRE_REASONING_EFFORT,
        provider_slug="openai",
        model=fixer_wire.FIXER_WIRE_MODEL,
        reasoning_effort=fixer_wire.FIXER_WIRE_REASONING_EFFORT,
        requires_provider_override=False,
    )
    env = _build_common_codex_env(adapter, llm_selection)
    prefix = [
        adapter.command,
        "--model",
        fixer_wire.FIXER_WIRE_MODEL,
        "--dangerously-bypass-approvals-and-sandbox",
    ]
    return prefix, env, (available_servers, config_env_vars, adapter, ensure_sqlite_scaffold)


def _build_fixer_exec_command(cwd: Path, prompt: str) -> tuple[list[str], dict[str, str]]:
    prefix, env, available_bundle = _build_exec_prefix(cwd)
    available_servers, config_env_vars, adapter, ensure_sqlite_scaffold = available_bundle
    selected_servers = {}
    if fixer_wire.FORCED_MCP_SERVER in available_servers:
        selected_servers[fixer_wire.FORCED_MCP_SERVER] = available_servers[fixer_wire.FORCED_MCP_SERVER]
    selected_config_paths: dict[str, Path] = {}
    if "sqlite" in selected_servers:
        sqlite_config = ensure_sqlite_scaffold(cwd)
        if sqlite_config is not None:
            selected_config_paths["sqlite"] = sqlite_config
    for server_name, config_path in selected_config_paths.items():
        env_var = config_env_vars.get(server_name)
        if env_var:
            env[env_var] = str(config_path)
    command = [
        *prefix,
        *adapter.build_mcp_flags(selected_servers, available_servers),
        "exec",
        "--skip-git-repo-check",
        prompt,
    ]
    return command, env


def _build_fixer_resume_command(cwd: Path, fixer_session_id: str, prompt: str) -> tuple[list[str], dict[str, str]]:
    prefix, env, available_bundle = _build_exec_prefix(cwd)
    available_servers, config_env_vars, adapter, ensure_sqlite_scaffold = available_bundle
    selected_servers = {}
    if fixer_wire.FORCED_MCP_SERVER in available_servers:
        selected_servers[fixer_wire.FORCED_MCP_SERVER] = available_servers[fixer_wire.FORCED_MCP_SERVER]
    selected_config_paths: dict[str, Path] = {}
    if "sqlite" in selected_servers:
        sqlite_config = ensure_sqlite_scaffold(cwd)
        if sqlite_config is not None:
            selected_config_paths["sqlite"] = sqlite_config
    for server_name, config_path in selected_config_paths.items():
        env_var = config_env_vars.get(server_name)
        if env_var:
            env[env_var] = str(config_path)
    command = [
        *prefix,
        *adapter.build_mcp_flags(selected_servers, available_servers),
        "exec",
        "resume",
        "--skip-git-repo-check",
        fixer_session_id,
        prompt,
    ]
    return command, env


def _current_state_fixer_session_id(cwd: Path) -> str:
    state = _load_state(cwd)
    session_id = str(state.get("fixer_codex_session_id", "")).strip()
    if not session_id:
        raise RuntimeError(
            f"Autonomous state file for {cwd} does not contain fixer_codex_session_id."
        )
    return session_id


def _wait_for_new_codex_session_id(cwd: Path, before: str | None, timeout_sec: float = 8.0) -> str | None:
    deadline = time.time() + timeout_sec
    while time.time() < deadline:
        latest = fixer_wire._latest_codex_session_id_for_cwd(cwd)
        if latest and latest != before:
            return latest
        time.sleep(0.5)
    return None


def _headless_netrunner_log_path(cwd: Path, session_id: int, backend: str) -> Path:
    log_dir = cwd / ".codex" / "headless_netrunner_logs"
    log_dir.mkdir(parents=True, exist_ok=True)
    timestamp = int(time.time())
    return log_dir / f"session-{session_id}-{backend}-{timestamp}.log"


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


def _wait_for_new_droid_session_id(log_path: Path, timeout_sec: float = 8.0) -> str | None:
    deadline = time.time() + timeout_sec
    while time.time() < deadline:
        try:
            with log_path.open("r", encoding="utf-8") as fh:
                for raw_line in fh:
                    session_id = _extract_droid_session_id_from_line(raw_line)
                    if session_id:
                        return session_id
        except OSError:
            return None
        time.sleep(0.5)
    return None


def _wait_for_new_external_session_id(
    backend: str,
    cwd: Path,
    before: str | None,
    log_path: Path,
    timeout_sec: float = 8.0,
) -> str | None:
    normalized_backend = fixer_wire.normalize_backend_name(backend)
    if normalized_backend == "codex":
        return _wait_for_new_codex_session_id(cwd, before, timeout_sec=timeout_sec)
    if normalized_backend == "droid":
        return _wait_for_new_droid_session_id(log_path, timeout_sec=timeout_sec)
    return None


def _implementation_test_discipline_lines() -> list[str]:
    return [
        "Implementation-session execution discipline:",
        "- write the required code changes",
        "- create, update, or remove the relevant automated tests for those changes",
        "- fix older broken tests in scope when they block the task instead of shipping code-only work",
    ]


def _build_autonomous_netrunner_prompt(
    session_id: int,
    mcp_names: list[str],
    fixer_session_id: str,
    mcp_how_to: dict[str, str],
) -> str:
    mcp_text = ", ".join(mcp_names) if mcp_names else "none"
    how_to_lines: list[str] = []
    for name in mcp_names:
        guidance = mcp_how_to.get(name, fixer_wire._build_default_how_to(name))
        how_to_lines.append(f"- {name}: {guidance}")
    lines = [
        "Activate skill `$manual-resolution` immediately.",
        "Use its Netrunner separate-terminal mode for this headless durable worker.",
        "",
        f"Preselected session ID from fixer autonomous flow: `{session_id}`.",
        f"Assigned MCP selection from fixer autonomous flow: {mcp_text}.",
        f"Autonomous fixer Codex session ID: `{fixer_session_id}`.",
        "Attached MCP how-to guidance:",
        *(how_to_lines or ["- none"]),
        "Architect pre-approved execution for this autonomous run. Do not wait for a manual 'Go'.",
        *_implementation_test_discipline_lines(),
        "If the operator needs an out-of-band status update, use `fixer_mcp.send_operator_telegram_notification`; do not rely on a separate `telegram_notify` MCP for routine Fixer flows.",
        "When the work is finished and you have submitted your mandatory doc proposal and completion report, call the fixer_mcp tool `wake_fixer_autonomous` with the completed session id and a concise handoff summary.",
        "Use this session ID for checkout unless Architect explicitly overrides.",
    ]
    return "\n".join(lines)


def _build_autonomous_fixer_resume_prompt(completed_session_id: int, summary: str) -> str:
    return "\n".join(
        [
            "Activate skill `$manual-acceptance` immediately.",
            f"Target completed session ID: `{completed_session_id}`.",
            f"Netrunner handoff summary: {summary or '(none)'}",
            "In this GenUI sandbox, the project Overseer owns the Ghost Run serial delivery loop.",
            "This is a serial autonomous-resolution run. Review the completed session, decide whether it is accepted or needs rework, then either launch exactly one next pending Netrunner session or conclude the run.",
            "If the completed session is the final tester worker and it reported bugs, create repair Netrunner sessions from those findings instead of concluding Ghost Run.",
            "For implementation sessions, verify the worker changed the relevant automated tests and fixed older broken tests in scope when needed.",
            "Reject code-only implementation deliveries that skipped required automated test work.",
        ]
    )


def _clear_stale_active_netrunner_if_safe(
    cwd: Path,
    state: dict[str, Any],
    session_by_id: dict[int, fixer_wire.SessionRow],
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


def launch_netrunner(
    cwd: Path,
    local_session_id: int,
    fixer_session_id: str | None = None,
    backend: str | None = None,
    model: str | None = None,
    reasoning: str | None = None,
) -> str | None:
    bootstrap_codex_pro_import_path()
    state = _load_or_initialize_launch_state(cwd, fixer_session_id)

    db_path = fixer_wire._resolve_fixer_db_path(cwd)
    conn = sqlite3.connect(db_path)
    try:
        fixer_wire._ensure_wire_schema(conn)
        project_id = fixer_wire._resolve_project_id(conn, cwd)
        sessions = fixer_wire._load_session_rows(conn, project_id)
        session_by_id = {row.session_id: row for row in sessions}
        state = _clear_stale_active_netrunner_if_safe(cwd, state, session_by_id, local_session_id)
        selected_session = session_by_id.get(local_session_id)
        if selected_session is None:
            raise RuntimeError(f"Session {local_session_id} is not available for {cwd}.")

        assigned_names = fixer_wire._load_assigned_mcp_names(conn, selected_session.global_session_id)
        registry_meta = fixer_wire._load_registry_mcp_metadata(conn)
        resolved_fixer_session_id = (fixer_session_id or "").strip() or _current_state_fixer_session_id(cwd)
        resolved_backend = fixer_wire.normalize_backend_name(backend or selected_session.cli_backend)
        descriptor = fixer_wire._backend_descriptor(resolved_backend)
        launch_selection = fixer_wire.SessionLaunchSelection(
            backend=resolved_backend,
            model=(model or selected_session.cli_model or descriptor.default_model).strip() or descriptor.default_model,
            reasoning=(reasoning or selected_session.cli_reasoning or descriptor.default_reasoning).strip() or descriptor.default_reasoning,
        )
        launch_selection = fixer_wire._persist_session_launch_selection(conn, selected_session, launch_selection)
        selected_session = fixer_wire.SessionRow(
            session_id=selected_session.session_id,
            global_session_id=selected_session.global_session_id,
            task_description=selected_session.task_description,
            status=selected_session.status,
            cli_backend=launch_selection.backend,
            cli_model=launch_selection.model,
            cli_reasoning=launch_selection.reasoning,
            external_session_id=fixer_wire._load_session_external_id(
                conn,
                selected_session.global_session_id,
                launch_selection.backend,
            ),
        )
    finally:
        conn.close()

    available_servers, config_env_vars, adapter, ensure_sqlite_scaffold = fixer_wire._load_available_servers(
        cwd,
        backend=launch_selection.backend,
    )
    selected_mcp_names = fixer_wire._normalize_names(assigned_names)
    if fixer_wire.FORCED_MCP_SERVER in available_servers:
        selected_mcp_names = fixer_wire._normalize_names([*selected_mcp_names, fixer_wire.FORCED_MCP_SERVER])
    selected_servers = {name: dict(available_servers[name]) for name in selected_mcp_names if name in available_servers}

    selected_config_paths: dict[str, Path] = {}
    if "sqlite" in selected_servers:
        sqlite_config = ensure_sqlite_scaffold(cwd)
        if sqlite_config is not None:
            selected_config_paths["sqlite"] = sqlite_config

    for server_name, config_path in selected_config_paths.items():
        env_var = config_env_vars.get(server_name)
        if not env_var:
            continue
        server_spec = dict(selected_servers.get(server_name, {}))
        server_env = server_spec.get("env", {})
        merged_server_env = dict(server_env) if isinstance(server_env, dict) else {}
        merged_server_env[env_var] = str(config_path)
        server_spec["env"] = merged_server_env
        selected_servers[server_name] = server_spec

    adapter.ensure_runtime_files(cwd, selected_servers, available_servers)

    prompt = _build_autonomous_netrunner_prompt(
        selected_session.session_id,
        selected_mcp_names,
        resolved_fixer_session_id,
        fixer_wire._build_mcp_how_to_map(selected_mcp_names, registry_meta),
    )
    llm_selection = SimpleNamespace(
        display_model=launch_selection.model,
        detail=launch_selection.reasoning,
        provider_slug="openai",
        model=launch_selection.model,
        reasoning_effort=launch_selection.reasoning,
        requires_provider_override=False,
    )
    env = _build_common_codex_env(adapter, llm_selection)
    for server_name, config_path in selected_config_paths.items():
        env_var = config_env_vars.get(server_name)
        if env_var:
            env[env_var] = str(config_path)

    before = (
        fixer_wire._latest_codex_session_id_for_cwd(cwd)
        if launch_selection.backend == "codex"
        else None
    )
    log_path = _headless_netrunner_log_path(cwd, local_session_id, launch_selection.backend)
    command = adapter.build_headless_command(
        model=launch_selection.model,
        reasoning=launch_selection.reasoning,
        selected=selected_servers,
        available=available_servers,
        prompt=prompt,
    )
    log_handle = log_path.open("a", encoding="utf-8")
    process = subprocess.Popen(
        command,
        cwd=str(cwd),
        env=env,
        stdout=log_handle,
        stderr=log_handle,
        start_new_session=True,
        text=True,
    )
    log_handle.close()
    if process.poll() is not None and process.returncode not in (0, None):
        raise RuntimeError(f"Failed to launch headless netrunner for session {local_session_id}.")

    new_session_id = _wait_for_new_external_session_id(
        launch_selection.backend,
        cwd,
        before,
        log_path,
    )
    if new_session_id:
        conn = sqlite3.connect(db_path)
        try:
            fixer_wire._ensure_wire_schema(conn)
            fixer_wire._save_session_external_id(
                conn,
                selected_session.global_session_id,
                launch_selection.backend,
                new_session_id,
            )
        finally:
            conn.close()

    active_session_ids = _normalize_active_netrunner_session_ids(state)
    if local_session_id not in active_session_ids:
        active_session_ids.append(local_session_id)
    _set_active_netrunner_session_ids(state, active_session_ids)
    state["last_launched_netrunner_session_id"] = new_session_id
    state["last_launched_netrunner_backend"] = launch_selection.backend
    state["updated_at_epoch"] = int(time.time())
    _save_state(cwd, state)

    print(
        f"[fixer-autonomous] launched netrunner session {local_session_id} "
        f"(backend={launch_selection.backend} external_session_id={new_session_id or 'pending-detect'})"
    )
    return new_session_id


def resume_fixer(cwd: Path, completed_session_id: int, summary: str) -> None:
    state = _load_state(cwd)
    fixer_session_id = str(state.get("fixer_codex_session_id", "")).strip()
    if not fixer_session_id:
        raise RuntimeError(
            f"Autonomous state file for {cwd} does not contain fixer_codex_session_id."
        )

    active_session_ids = [
        session_id
        for session_id in _normalize_active_netrunner_session_ids(state)
        if session_id != completed_session_id
    ]
    _set_active_netrunner_session_ids(state, active_session_ids)
    state["last_completed_netrunner_session_id"] = completed_session_id
    state["last_handoff_summary"] = summary
    state["updated_at_epoch"] = int(time.time())
    _save_state(cwd, state)

    prompt = _build_autonomous_fixer_resume_prompt(completed_session_id, summary)
    command, env = _build_fixer_resume_command(cwd, fixer_session_id, prompt)
    subprocess.Popen(
        command,
        cwd=str(cwd),
        env=env,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
        start_new_session=True,
        text=True,
    )
    print(
        f"[fixer-autonomous] resumed fixer session {fixer_session_id} "
        f"for completed session {completed_session_id}"
    )


def _parse_args(argv: list[str]) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Serial autonomous Fixer helpers.")
    subparsers = parser.add_subparsers(dest="command", required=True)

    register_parser = subparsers.add_parser("register-fixer")
    register_parser.add_argument("--cwd", required=True)
    register_parser.add_argument("--fixer-session-id")

    launch_parser = subparsers.add_parser("launch-netrunner")
    launch_parser.add_argument("--cwd", required=True)
    launch_parser.add_argument("--session-id", type=int, required=True)
    launch_parser.add_argument("--fixer-session-id")
    launch_parser.add_argument("--backend")
    launch_parser.add_argument("--model")
    launch_parser.add_argument("--reasoning")

    resume_parser = subparsers.add_parser("resume-fixer")
    resume_parser.add_argument("--cwd", required=True)
    resume_parser.add_argument("--completed-session-id", type=int, required=True)
    resume_parser.add_argument("--summary", default="")

    state_parser = subparsers.add_parser("show-state")
    state_parser.add_argument("--cwd", required=True)

    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = _parse_args(sys.argv[1:] if argv is None else argv)
    cwd = Path(args.cwd).expanduser().resolve()
    try:
        if args.command == "register-fixer":
            register_fixer_session(cwd, getattr(args, "fixer_session_id", None))
            return 0
        if args.command == "launch-netrunner":
            launch_netrunner(
                cwd,
                args.session_id,
                getattr(args, "fixer_session_id", None),
                getattr(args, "backend", None),
                getattr(args, "model", None),
                getattr(args, "reasoning", None),
            )
            return 0
        if args.command == "resume-fixer":
            resume_fixer(cwd, args.completed_session_id, args.summary)
            return 0
        if args.command == "show-state":
            print(json.dumps(_load_state(cwd), indent=2))
            return 0
    except RuntimeError as exc:
        print(f"[fixer-autonomous] {exc}", file=sys.stderr)
        return 2
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
