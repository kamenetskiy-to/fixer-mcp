#!/usr/bin/env python3
"""Helpers for serial autonomous Fixer/Netrunner execution."""

from __future__ import annotations

import argparse
import json
import os
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
from client_wires import (
    fixer_autonomous_commands,
    fixer_autonomous_prompts,
    fixer_autonomous_state,
    fixer_autonomous_transcripts,
    fixer_autonomous_wave,
    fixer_wire,
)

AUTONOMOUS_STATE_RELATIVE_PATH = fixer_autonomous_state.AUTONOMOUS_STATE_RELATIVE_PATH
WAVE_BRANCH_PATTERN = fixer_autonomous_wave.WAVE_BRANCH_PATTERN
EXTERNAL_SESSION_ID_DETECTOR_TIMEOUT_SEC = float(
    os.environ.get("FIXER_EXTERNAL_SESSION_ID_DETECTOR_TIMEOUT_SEC", "180")
)
_WaveNetrunnerLaunchPlan = fixer_autonomous_wave._WaveNetrunnerLaunchPlan


def _state_path(cwd: Path) -> Path:
    return fixer_autonomous_state._state_path(cwd)


def _load_state(cwd: Path) -> dict[str, Any]:
    return fixer_autonomous_state._load_state(cwd)


def _save_state(cwd: Path, payload: dict[str, Any]) -> Path:
    return fixer_autonomous_state._save_state(cwd, payload)


def _normalize_active_netrunner_session_ids(state: dict[str, Any]) -> list[int]:
    return fixer_autonomous_state._normalize_active_netrunner_session_ids(state)


def _set_active_netrunner_session_ids(state: dict[str, Any], session_ids: list[int]) -> dict[str, Any]:
    return fixer_autonomous_state._set_active_netrunner_session_ids(state, session_ids)


def _load_or_initialize_launch_state(
    cwd: Path,
    fixer_session_id: str | None,
    *,
    allow_missing_fixer_session: bool = False,
) -> dict[str, Any]:
    return fixer_autonomous_state._load_or_initialize_launch_state(
        cwd,
        fixer_session_id,
        resolve_fixer_session_id_fn=_resolve_fixer_session_id,
        allow_missing_fixer_session=allow_missing_fixer_session,
    )


def _latest_fixer_session_id(cwd: Path) -> str:
    bootstrap_codex_pro_import_path()
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


def _resolve_overseer_fixer_session_id(cwd: Path, fixer_session_id: str | None) -> str:
    explicit = (fixer_session_id or "").strip()
    if explicit:
        return explicit
    return _latest_fixer_session_id(cwd)


def _format_runtime_error(exc: RuntimeError) -> str:
    return fixer_autonomous_state._format_runtime_error(exc)


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


def _build_common_codex_env(adapter: Any, llm_selection: Any, cwd: Path) -> dict[str, str]:
    return fixer_autonomous_commands._build_common_codex_env(adapter, llm_selection, cwd)


def _build_exec_prefix(cwd: Path) -> tuple[list[str], dict[str, str], Any]:
    return fixer_autonomous_commands._build_exec_prefix(
        cwd,
        bootstrap_codex_pro_import_path_fn=bootstrap_codex_pro_import_path,
        build_common_codex_env_fn=_build_common_codex_env,
    )


def _build_fixer_exec_command(cwd: Path, prompt: str) -> tuple[list[str], dict[str, str]]:
    return fixer_autonomous_commands._build_fixer_exec_command(
        cwd,
        prompt,
        build_exec_prefix_fn=_build_exec_prefix,
    )


def _build_fixer_resume_command(cwd: Path, fixer_session_id: str, prompt: str) -> tuple[list[str], dict[str, str]]:
    return fixer_autonomous_commands._build_fixer_resume_command(
        cwd,
        fixer_session_id,
        prompt,
        build_exec_prefix_fn=_build_exec_prefix,
    )


def _current_state_fixer_session_id(cwd: Path) -> str:
    return fixer_autonomous_state._current_state_fixer_session_id(cwd)


def _wait_for_new_codex_session_id(cwd: Path, before: str | None, timeout_sec: float = 8.0) -> str | None:
    return fixer_autonomous_transcripts._wait_for_new_codex_session_id(
        cwd,
        before,
        timeout_sec=timeout_sec,
        latest_codex_session_id_for_cwd_fn=fixer_wire._latest_codex_session_id_for_cwd,
        find_new_codex_session_id_from_transcript_store_fn=_find_new_codex_session_id_from_transcript_store,
    )


def _headless_netrunner_log_path(cwd: Path, session_id: int, backend: str) -> Path:
    log_dir = cwd / ".codex" / "headless_netrunner_logs"
    log_dir.mkdir(parents=True, exist_ok=True)
    timestamp = int(time.time())
    return log_dir / f"session-{session_id}-{backend}-{timestamp}.log"


def _write_worker_metadata(
    metadata_path: Path,
    *,
    worker_pid: int,
    headless_log_path: Path,
    backend: str,
    session_id: int,
    wave_id: int | None = None,
    wave_worker_id: int | None = None,
    project_cwd: Path | None = None,
    worker_cwd: Path | None = None,
    branch_name: str | None = None,
) -> None:
    fixer_autonomous_wave._write_worker_metadata(
        metadata_path,
        worker_pid=worker_pid,
        headless_log_path=headless_log_path,
        backend=backend,
        session_id=session_id,
        wave_id=wave_id,
        wave_worker_id=wave_worker_id,
        project_cwd=project_cwd,
        worker_cwd=worker_cwd,
        branch_name=branch_name,
    )


def _persist_detected_external_session_id(
    db_path: Path,
    *,
    global_session_id: int,
    backend: str,
    external_session_id: str | None,
) -> str | None:
    resolved_external_session_id = (external_session_id or "").strip()
    if not resolved_external_session_id:
        return None
    conn = sqlite3.connect(db_path)
    try:
        fixer_wire._ensure_wire_schema(conn)
        fixer_wire._save_session_external_id(
            conn,
            global_session_id,
            backend,
            resolved_external_session_id,
        )
    finally:
        conn.close()
    return resolved_external_session_id


def _positive_wave_int(name: str, value: int) -> int:
    return fixer_autonomous_wave._positive_wave_int(name, value)


def _wave_branch_name(wave_id: int, local_session_id: int) -> str:
    return fixer_autonomous_wave._wave_branch_name(wave_id, local_session_id)


def _validate_wave_branch_name(branch_name: str) -> str:
    return fixer_autonomous_wave._validate_wave_branch_name(branch_name)


def _wave_worktree_path(
    project_cwd: Path,
    worktree_root: Path,
    wave_id: int,
    local_session_id: int,
) -> Path:
    return fixer_autonomous_wave._wave_worktree_path(project_cwd, worktree_root, wave_id, local_session_id)


def _wave_worker_artifact_dir(project_cwd: Path, wave_id: int, local_session_id: int) -> Path:
    return fixer_autonomous_wave._wave_worker_artifact_dir(project_cwd, wave_id, local_session_id)


def _wave_worker_metadata_path(project_cwd: Path, wave_id: int, local_session_id: int) -> Path:
    return fixer_autonomous_wave._wave_worker_metadata_path(project_cwd, wave_id, local_session_id)


def _build_git_worktree_list_command(project_cwd: Path) -> list[str]:
    return fixer_autonomous_wave._build_git_worktree_list_command(project_cwd)


def _build_git_branch_exists_command(project_cwd: Path, branch_name: str) -> list[str]:
    return fixer_autonomous_wave._build_git_branch_exists_command(project_cwd, branch_name)


def _build_git_worktree_add_command(
    project_cwd: Path,
    *,
    worktree_path: Path,
    branch_name: str,
    base_sha: str,
) -> list[str]:
    return fixer_autonomous_wave._build_git_worktree_add_command(
        project_cwd,
        worktree_path=worktree_path,
        branch_name=branch_name,
        base_sha=base_sha,
    )


def _validate_specific_worktree_path(project_cwd: Path, worktree_path: Path) -> Path:
    return fixer_autonomous_wave._validate_specific_worktree_path(project_cwd, worktree_path)


def _build_git_worktree_remove_command(
    project_cwd: Path,
    worktree_path: Path,
    *,
    force: bool = False,
) -> list[str]:
    return fixer_autonomous_wave._build_git_worktree_remove_command(
        project_cwd,
        worktree_path,
        force=force,
    )


def _build_git_worktree_prune_command(project_cwd: Path, *, dry_run: bool = True) -> list[str]:
    return fixer_autonomous_wave._build_git_worktree_prune_command(project_cwd, dry_run=dry_run)


def _extract_droid_session_id_from_payload(payload: object) -> str | None:
    return fixer_autonomous_transcripts._extract_droid_session_id_from_payload(payload)


def _extract_droid_session_id_from_line(raw_line: str) -> str | None:
    return fixer_autonomous_transcripts._extract_droid_session_id_from_line(raw_line)


def _droid_factory_sessions_root() -> Path:
    return fixer_autonomous_transcripts._droid_factory_sessions_root()


def _codex_sessions_root() -> Path:
    return fixer_autonomous_transcripts._codex_sessions_root()


def _extract_droid_record_type(payload: object) -> str:
    return fixer_autonomous_transcripts._extract_droid_record_type(payload)


def _extract_droid_cwd_from_payload(payload: object) -> str | None:
    return fixer_autonomous_transcripts._extract_droid_cwd_from_payload(payload)


def _extract_codex_session_id_from_payload(payload: object) -> str | None:
    return fixer_autonomous_transcripts._extract_codex_session_id_from_payload(payload)


def _extract_codex_cwd_from_payload(payload: object) -> str | None:
    return fixer_autonomous_transcripts._extract_codex_cwd_from_payload(payload)


def _candidate_droid_transcript_paths(
    sessions_root: Path,
    *,
    launch_started_at: float | None,
) -> list[Path]:
    return fixer_autonomous_transcripts._candidate_droid_transcript_paths(
        sessions_root,
        launch_started_at=launch_started_at,
    )


def _candidate_codex_transcript_paths(
    sessions_root: Path,
    *,
    launch_started_at: float | None,
) -> list[Path]:
    return fixer_autonomous_transcripts._candidate_codex_transcript_paths(
        sessions_root,
        launch_started_at=launch_started_at,
    )


def _codex_session_id_from_transcript(path: Path, cwd: Path) -> str | None:
    return fixer_autonomous_transcripts._codex_session_id_from_transcript(path, cwd)


def _find_new_codex_session_id_from_transcript_store(
    cwd: Path,
    *,
    launch_started_at: float | None,
    sessions_root: Path | None = None,
) -> str | None:
    return fixer_autonomous_transcripts._find_new_codex_session_id_from_transcript_store(
        cwd,
        launch_started_at=launch_started_at,
        sessions_root=sessions_root,
        codex_sessions_root_fn=_codex_sessions_root,
    )


def _droid_session_id_from_transcript(path: Path, cwd: Path) -> str | None:
    return fixer_autonomous_transcripts._droid_session_id_from_transcript(path, cwd)


def _find_new_droid_session_id_from_factory_store(
    cwd: Path,
    *,
    launch_started_at: float | None,
    sessions_root: Path | None = None,
) -> str | None:
    return fixer_autonomous_transcripts._find_new_droid_session_id_from_factory_store(
        cwd,
        launch_started_at=launch_started_at,
        sessions_root=sessions_root,
        droid_factory_sessions_root_fn=_droid_factory_sessions_root,
    )


def _wait_for_new_droid_session_id(
    log_path: Path,
    cwd: Path,
    *,
    launch_started_at: float | None = None,
    timeout_sec: float = 8.0,
) -> str | None:
    return fixer_autonomous_transcripts._wait_for_new_droid_session_id(
        log_path,
        cwd,
        launch_started_at=launch_started_at,
        timeout_sec=timeout_sec,
        find_new_droid_session_id_from_factory_store_fn=_find_new_droid_session_id_from_factory_store,
        extract_droid_session_id_from_line_fn=_extract_droid_session_id_from_line,
    )


def _wait_for_new_external_session_id(
    backend: str,
    cwd: Path,
    before: str | None,
    log_path: Path,
    launch_started_at: float | None = None,
    timeout_sec: float = 8.0,
) -> str | None:
    return fixer_autonomous_transcripts._wait_for_new_external_session_id(
        backend,
        cwd,
        before,
        log_path,
        launch_started_at=launch_started_at,
        timeout_sec=timeout_sec,
        normalize_backend_name_fn=fixer_wire.normalize_backend_name,
        wait_for_new_codex_session_id_fn=_wait_for_new_codex_session_id,
        wait_for_new_droid_session_id_fn=_wait_for_new_droid_session_id,
    )


def _implementation_test_discipline_lines() -> list[str]:
    return fixer_autonomous_prompts._implementation_test_discipline_lines()


def _build_autonomous_netrunner_prompt(
    session_id: int,
    mcp_names: list[str],
    fixer_session_id: str,
    mcp_how_to: dict[str, str],
    *,
    suppress_autonomous_wake: bool = False,
) -> str:
    return fixer_autonomous_prompts._build_autonomous_netrunner_prompt(
        session_id,
        mcp_names,
        fixer_session_id,
        mcp_how_to,
        default_how_to_fn=fixer_wire._build_default_how_to,
        suppress_autonomous_wake=suppress_autonomous_wake,
    )


def _build_wave_netrunner_prompt(
    *,
    session_id: int,
    mcp_names: list[str],
    fixer_session_id: str,
    mcp_how_to: dict[str, str],
    wave_id: int,
    wave_worker_id: int,
    branch_name: str,
    worker_cwd: Path,
    declared_write_scope: list[str],
) -> str:
    return fixer_autonomous_prompts._build_wave_netrunner_prompt(
        session_id=session_id,
        mcp_names=mcp_names,
        fixer_session_id=fixer_session_id,
        mcp_how_to=mcp_how_to,
        wave_id=wave_id,
        wave_worker_id=wave_worker_id,
        branch_name=branch_name,
        worker_cwd=worker_cwd,
        declared_write_scope=declared_write_scope,
        positive_wave_int_fn=_positive_wave_int,
        validate_wave_branch_name_fn=_validate_wave_branch_name,
        default_how_to_fn=fixer_wire._build_default_how_to,
    )


def _build_wave_netrunner_launch_plan(
    *,
    project_cwd: Path,
    worker_cwd: Path,
    local_session_id: int,
    wave_id: int,
    wave_worker_id: int,
    declared_write_scope: list[str],
    fixer_session_id: str,
    assigned_mcp_names: list[str],
    mcp_how_to: dict[str, str],
    launch_selection: fixer_wire.SessionLaunchSelection,
    available_servers: dict[str, dict[str, object]],
    config_env_vars: dict[str, str],
    adapter: Any,
    ensure_sqlite_scaffold: Any,
    db_path: Path | None = None,
    branch_name: str | None = None,
) -> _WaveNetrunnerLaunchPlan:
    return fixer_autonomous_wave._build_wave_netrunner_launch_plan(
        project_cwd=project_cwd,
        worker_cwd=worker_cwd,
        local_session_id=local_session_id,
        wave_id=wave_id,
        wave_worker_id=wave_worker_id,
        declared_write_scope=declared_write_scope,
        fixer_session_id=fixer_session_id,
        assigned_mcp_names=assigned_mcp_names,
        mcp_how_to=mcp_how_to,
        launch_selection=launch_selection,
        available_servers=available_servers,
        config_env_vars=config_env_vars,
        adapter=adapter,
        ensure_sqlite_scaffold=ensure_sqlite_scaffold,
        db_path=db_path,
        branch_name=branch_name,
        build_common_codex_env_fn=_build_common_codex_env,
        build_wave_netrunner_prompt_fn=_build_wave_netrunner_prompt,
    )


def _build_autonomous_fixer_resume_prompt(completed_session_id: int, summary: str) -> str:
    return fixer_autonomous_prompts._build_autonomous_fixer_resume_prompt(
        completed_session_id,
        summary,
    )


def _build_overseer_directed_fixer_prompt() -> str:
    return fixer_autonomous_prompts._build_overseer_directed_fixer_prompt()


def _clear_stale_active_netrunner_if_safe(
    cwd: Path,
    state: dict[str, Any],
    session_by_id: dict[int, fixer_wire.SessionRow],
    requested_session_id: int,
) -> dict[str, Any]:
    return fixer_autonomous_state._clear_stale_active_netrunner_if_safe(
        cwd,
        state,
        session_by_id,
        requested_session_id,
    )


def launch_netrunner(
    cwd: Path,
    local_session_id: int,
    fixer_session_id: str | None = None,
    backend: str | None = None,
    model: str | None = None,
    reasoning: str | None = None,
    headless_log_path: Path | None = None,
    worker_metadata_path: Path | None = None,
    suppress_autonomous_wake: bool = False,
) -> str | None:
    bootstrap_codex_pro_import_path()
    state = _load_or_initialize_launch_state(
        cwd,
        fixer_session_id,
        allow_missing_fixer_session=suppress_autonomous_wake,
    )

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
        resolved_fixer_session_id = (fixer_session_id or "").strip()
        if not resolved_fixer_session_id:
            state_fixer_session_id = str(state.get("fixer_codex_session_id", "")).strip()
            resolved_fixer_session_id = (
                state_fixer_session_id
                if suppress_autonomous_wake
                else state_fixer_session_id or _current_state_fixer_session_id(cwd)
            )
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
    fixer_wire._ensure_forced_fixer_server_resolved(available_servers)
    selected_mcp_names = fixer_wire._normalize_names(assigned_names)
    if fixer_wire.FORCED_MCP_SERVER in available_servers:
        selected_mcp_names = fixer_wire._normalize_names([*selected_mcp_names, fixer_wire.FORCED_MCP_SERVER])
    selected_servers = {name: dict(available_servers[name]) for name in selected_mcp_names if name in available_servers}
    if fixer_wire.FORCED_MCP_SERVER in selected_servers:
        selected_servers = fixer_wire._bind_fixer_db_path_to_server_env(selected_servers, db_path=db_path)
        selected_servers = fixer_wire._bind_netrunner_stateless_auth_to_server_env(selected_servers, project_cwd=cwd)
        selected_servers = fixer_wire._bind_locked_role_to_server_env(selected_servers, role="netrunner")
        selected_servers = fixer_wire._bind_launcher_telegram_env_to_server_env(selected_servers)

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

    llm_selection = SimpleNamespace(
        display_model=launch_selection.model,
        detail=launch_selection.reasoning,
        provider_slug="openai",
        model=launch_selection.model,
        reasoning_effort=launch_selection.reasoning,
        requires_provider_override=False,
    )
    fixer_wire._maybe_configure_playwright_runtime_mode(
        adapter,
        selected_servers,
        available_servers,
        interactive=False,
    )
    adapter.ensure_runtime_files(cwd, llm_selection, selected_servers, available_servers)

    prompt = _build_autonomous_netrunner_prompt(
        selected_session.session_id,
        selected_mcp_names,
        resolved_fixer_session_id,
        fixer_wire._build_mcp_how_to_map(selected_mcp_names, registry_meta),
        suppress_autonomous_wake=suppress_autonomous_wake,
    )
    prompt = fixer_wire._append_droid_mcp_tool_guidance(
        prompt,
        backend=launch_selection.backend,
        mcp_names=selected_mcp_names,
    )
    env = _build_common_codex_env(adapter, llm_selection, cwd)
    for server_name, config_path in selected_config_paths.items():
        env_var = config_env_vars.get(server_name)
        if env_var:
            env[env_var] = str(config_path)

    before = (
        fixer_wire._latest_codex_session_id_for_cwd(cwd)
        if launch_selection.backend == "codex"
        else None
    )
    log_path = (
        headless_log_path
        if headless_log_path is not None
        else _headless_netrunner_log_path(cwd, local_session_id, launch_selection.backend)
    )
    log_path.parent.mkdir(parents=True, exist_ok=True)
    command = adapter.build_headless_command(
        model=launch_selection.model,
        reasoning=launch_selection.reasoning,
        selected=selected_servers,
        available=available_servers,
        prompt=prompt,
    )
    launch_started_at = time.time()
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
    if worker_metadata_path is not None:
        _write_worker_metadata(
            worker_metadata_path,
            worker_pid=process.pid,
            headless_log_path=log_path,
            backend=launch_selection.backend,
            session_id=local_session_id,
        )

    active_session_ids = _normalize_active_netrunner_session_ids(state)
    if local_session_id not in active_session_ids:
        active_session_ids.append(local_session_id)
    _set_active_netrunner_session_ids(state, active_session_ids)
    state["last_launched_netrunner_session_id"] = None
    state["last_launched_netrunner_backend"] = launch_selection.backend
    state["updated_at_epoch"] = int(time.time())
    _save_state(cwd, state)

    new_session_id = _wait_for_new_external_session_id(
        launch_selection.backend,
        cwd,
        before,
        log_path,
        launch_started_at=launch_started_at,
        timeout_sec=EXTERNAL_SESSION_ID_DETECTOR_TIMEOUT_SEC,
    )
    new_session_id = _persist_detected_external_session_id(
        db_path,
        global_session_id=selected_session.global_session_id,
        backend=launch_selection.backend,
        external_session_id=new_session_id,
    )

    state["last_launched_netrunner_session_id"] = new_session_id
    state["updated_at_epoch"] = int(time.time())
    _save_state(cwd, state)

    print(
        f"[fixer-autonomous] launched netrunner session {local_session_id} "
        f"(backend={launch_selection.backend} external_session_id={new_session_id or 'pending-detect'})"
    )
    return new_session_id


def _wave_headless_netrunner_log_path(
    project_cwd: Path,
    wave_id: int,
    local_session_id: int,
    backend: str,
) -> Path:
    return fixer_autonomous_wave._wave_headless_netrunner_log_path(
        project_cwd,
        wave_id,
        local_session_id,
        backend,
    )


def launch_wave_netrunner_worker(
    project_cwd: Path,
    worker_cwd: Path,
    local_session_id: int,
    wave_id: int,
    wave_worker_id: int,
    declared_write_scope: list[str],
    fixer_session_id: str | None = None,
    backend: str | None = None,
    model: str | None = None,
    reasoning: str | None = None,
    branch_name: str | None = None,
    headless_log_path: Path | None = None,
    worker_metadata_path: Path | None = None,
) -> str | None:
    bootstrap_codex_pro_import_path()
    resolved_project_cwd = project_cwd.expanduser().resolve()
    resolved_worker_cwd = worker_cwd.expanduser().resolve()
    normalized_session_id = _positive_wave_int("local_session_id", local_session_id)
    normalized_wave_id = _positive_wave_int("wave_id", wave_id)
    normalized_wave_worker_id = _positive_wave_int("wave_worker_id", wave_worker_id)

    db_path = fixer_wire._resolve_fixer_db_path(resolved_project_cwd)
    conn = sqlite3.connect(db_path)
    try:
        fixer_wire._ensure_wire_schema(conn)
        project_id = fixer_wire._resolve_project_id(conn, resolved_project_cwd)
        sessions = fixer_wire._load_session_rows(conn, project_id)
        session_by_id = {row.session_id: row for row in sessions}
        selected_session = session_by_id.get(normalized_session_id)
        if selected_session is None:
            raise RuntimeError(f"Session {normalized_session_id} is not available for {resolved_project_cwd}.")

        assigned_names = fixer_wire._load_assigned_mcp_names(conn, selected_session.global_session_id)
        registry_meta = fixer_wire._load_registry_mcp_metadata(conn)
        resolved_fixer_session_id = (fixer_session_id or "").strip() or _current_state_fixer_session_id(
            resolved_project_cwd
        )
        resolved_backend = fixer_wire.normalize_backend_name(backend or selected_session.cli_backend)
        descriptor = fixer_wire._backend_descriptor(resolved_backend)
        launch_selection = fixer_wire.SessionLaunchSelection(
            backend=resolved_backend,
            model=(model or selected_session.cli_model or descriptor.default_model).strip()
            or descriptor.default_model,
            reasoning=(reasoning or selected_session.cli_reasoning or descriptor.default_reasoning).strip()
            or descriptor.default_reasoning,
        )
        launch_selection = fixer_wire._persist_session_launch_selection(conn, selected_session, launch_selection)
        global_session_id = selected_session.global_session_id
    finally:
        conn.close()

    available_servers, config_env_vars, adapter, ensure_sqlite_scaffold = fixer_wire._load_available_servers(
        resolved_project_cwd,
        backend=launch_selection.backend,
    )
    plan = _build_wave_netrunner_launch_plan(
        project_cwd=resolved_project_cwd,
        worker_cwd=resolved_worker_cwd,
        local_session_id=normalized_session_id,
        wave_id=normalized_wave_id,
        wave_worker_id=normalized_wave_worker_id,
        declared_write_scope=declared_write_scope,
        fixer_session_id=resolved_fixer_session_id,
        assigned_mcp_names=assigned_names,
        mcp_how_to=fixer_wire._build_mcp_how_to_map(assigned_names, registry_meta),
        launch_selection=launch_selection,
        available_servers=available_servers,
        config_env_vars=config_env_vars,
        adapter=adapter,
        ensure_sqlite_scaffold=ensure_sqlite_scaffold,
        db_path=db_path,
        branch_name=branch_name,
    )

    before = (
        fixer_wire._latest_codex_session_id_for_cwd(resolved_worker_cwd)
        if launch_selection.backend == "codex"
        else None
    )
    log_path = (
        headless_log_path
        if headless_log_path is not None
        else _wave_headless_netrunner_log_path(resolved_project_cwd, normalized_wave_id, normalized_session_id, launch_selection.backend)
    )
    metadata_path = (
        worker_metadata_path
        if worker_metadata_path is not None
        else _wave_worker_metadata_path(resolved_project_cwd, normalized_wave_id, normalized_session_id)
    )
    log_path.parent.mkdir(parents=True, exist_ok=True)
    launch_started_at = time.time()
    log_handle = log_path.open("a", encoding="utf-8")
    process = subprocess.Popen(
        plan.command,
        cwd=str(plan.popen_cwd),
        env=plan.env,
        stdout=log_handle,
        stderr=log_handle,
        start_new_session=True,
        text=True,
    )
    log_handle.close()
    if process.poll() is not None and process.returncode not in (0, None):
        raise RuntimeError(f"Failed to launch wave netrunner for session {normalized_session_id}.")

    _write_worker_metadata(
        metadata_path,
        worker_pid=process.pid,
        headless_log_path=log_path,
        backend=launch_selection.backend,
        session_id=normalized_session_id,
        wave_id=normalized_wave_id,
        wave_worker_id=normalized_wave_worker_id,
        project_cwd=resolved_project_cwd,
        worker_cwd=resolved_worker_cwd,
        branch_name=str(plan.metadata["branch_name"]),
    )

    new_session_id = _wait_for_new_external_session_id(
        launch_selection.backend,
        resolved_worker_cwd,
        before,
        log_path,
        launch_started_at=launch_started_at,
        timeout_sec=EXTERNAL_SESSION_ID_DETECTOR_TIMEOUT_SEC,
    )
    new_session_id = _persist_detected_external_session_id(
        db_path,
        global_session_id=global_session_id,
        backend=launch_selection.backend,
        external_session_id=new_session_id,
    )

    print(
        f"[fixer-autonomous] launched wave netrunner session {normalized_session_id} "
        f"(wave={normalized_wave_id} worker={normalized_wave_worker_id} "
        f"backend={launch_selection.backend} external_session_id={new_session_id or 'pending-detect'})"
    )
    return new_session_id


def launch_overseer_fixer(cwd: Path, fixer_session_id: str | None = None) -> str:
    resolved_session_id = _resolve_overseer_fixer_session_id(cwd, fixer_session_id)
    prompt = _build_overseer_directed_fixer_prompt()
    command, env = _build_fixer_resume_command(cwd, resolved_session_id, prompt)
    subprocess.Popen(
        command,
        cwd=str(cwd),
        env=env,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
        start_new_session=True,
        text=True,
    )
    print(f"[fixer-autonomous] launched overseer-directed fixer session {resolved_session_id}")
    return resolved_session_id


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
    launch_parser.add_argument("--headless-log-path")
    launch_parser.add_argument("--worker-metadata-path")
    launch_parser.add_argument("--suppress-autonomous-wake", action="store_true")

    wave_worker_parser = subparsers.add_parser("launch-wave-worker")
    wave_worker_parser.add_argument("--project-cwd", required=True)
    wave_worker_parser.add_argument("--worker-cwd", required=True)
    wave_worker_parser.add_argument("--session-id", type=int, required=True)
    wave_worker_parser.add_argument("--wave-id", type=int, required=True)
    wave_worker_parser.add_argument("--wave-worker-id", type=int, required=True)
    wave_worker_parser.add_argument("--declared-write-scope", action="append", default=[])
    wave_worker_parser.add_argument("--fixer-session-id")
    wave_worker_parser.add_argument("--backend")
    wave_worker_parser.add_argument("--model")
    wave_worker_parser.add_argument("--reasoning")
    wave_worker_parser.add_argument("--branch-name")
    wave_worker_parser.add_argument("--headless-log-path")
    wave_worker_parser.add_argument("--worker-metadata-path")

    resume_parser = subparsers.add_parser("resume-fixer")
    resume_parser.add_argument("--cwd", required=True)
    resume_parser.add_argument("--completed-session-id", type=int, required=True)
    resume_parser.add_argument("--summary", default="")

    overseer_parser = subparsers.add_parser("launch-overseer-fixer")
    overseer_parser.add_argument("--cwd", required=True)
    overseer_parser.add_argument("--fixer-session-id")

    state_parser = subparsers.add_parser("show-state")
    state_parser.add_argument("--cwd", required=True)

    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = _parse_args(sys.argv[1:] if argv is None else argv)
    cwd_arg = getattr(args, "cwd", None)
    cwd = Path(cwd_arg).expanduser().resolve() if cwd_arg else None
    try:
        if args.command == "register-fixer":
            assert cwd is not None
            register_fixer_session(cwd, getattr(args, "fixer_session_id", None))
            return 0
        if args.command == "launch-netrunner":
            assert cwd is not None
            launch_netrunner(
                cwd,
                args.session_id,
                getattr(args, "fixer_session_id", None),
                getattr(args, "backend", None),
                getattr(args, "model", None),
                getattr(args, "reasoning", None),
                Path(args.headless_log_path).expanduser().resolve() if getattr(args, "headless_log_path", None) else None,
                Path(args.worker_metadata_path).expanduser().resolve() if getattr(args, "worker_metadata_path", None) else None,
                bool(getattr(args, "suppress_autonomous_wake", False)),
            )
            return 0
        if args.command == "launch-wave-worker":
            launch_wave_netrunner_worker(
                Path(args.project_cwd).expanduser().resolve(),
                Path(args.worker_cwd).expanduser().resolve(),
                args.session_id,
                args.wave_id,
                args.wave_worker_id,
                list(getattr(args, "declared_write_scope", []) or []),
                getattr(args, "fixer_session_id", None),
                getattr(args, "backend", None),
                getattr(args, "model", None),
                getattr(args, "reasoning", None),
                getattr(args, "branch_name", None),
                Path(args.headless_log_path).expanduser().resolve() if getattr(args, "headless_log_path", None) else None,
                Path(args.worker_metadata_path).expanduser().resolve()
                if getattr(args, "worker_metadata_path", None)
                else None,
            )
            return 0
        if args.command == "resume-fixer":
            assert cwd is not None
            resume_fixer(cwd, args.completed_session_id, args.summary)
            return 0
        if args.command == "launch-overseer-fixer":
            assert cwd is not None
            launch_overseer_fixer(cwd, getattr(args, "fixer_session_id", None))
            return 0
        if args.command == "show-state":
            assert cwd is not None
            print(json.dumps(_load_state(cwd), indent=2))
            return 0
    except RuntimeError as exc:
        print(f"[fixer-autonomous] {_format_runtime_error(exc)}", file=sys.stderr)
        return 2
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
