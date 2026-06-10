"""Manual Netrunner launch workflow implementation for the Fixer wire launcher."""

from __future__ import annotations

from contextlib import closing
from dataclasses import dataclass
from pathlib import Path
import sqlite3
import subprocess
import sys
from typing import Any, Callable, Sequence

from client_wires.backends import normalize_backend_name
from client_wires.fixer_wire_db import SessionLaunchSelection, SessionRow


@dataclass(frozen=True)
class NetrunnerLaunchCallbacks:
    resolve_fixer_db_path: Callable[[Path], Path]
    ensure_wire_schema: Callable[[sqlite3.Connection], None]
    ensure_project_registered: Callable[[sqlite3.Connection, Path], int]
    load_session_rows: Callable[[sqlite3.Connection, int], list[SessionRow]]
    select_session_interactive: Callable[..., SessionRow]
    select_manual_netrunner_kind_interactive: Callable[..., str]
    resolve_netrunner_launch_selection: Callable[..., SessionLaunchSelection]
    load_available_servers: Callable[..., tuple[dict[str, dict[str, object]], dict[str, str], Any, Any]]
    sync_registry_names: Callable[[sqlite3.Connection, Sequence[str]], None]
    load_registry_mcp_metadata: Callable[..., dict[str, Any]]
    load_assigned_mcp_names: Callable[[sqlite3.Connection, int], list[str]]
    load_project_allowed_mcp_names: Callable[[sqlite3.Connection, int], list[str]]
    allowed_runtime_mcp_names: Callable[[Sequence[str], dict[str, dict[str, object]]], list[str]]
    assigned_allowed_mcp_names: Callable[[Sequence[str], Sequence[str]], list[str]]
    assigned_preselected_mcp_names: Callable[[Sequence[str], Sequence[str]], list[str]]
    select_mcp_interactive: Callable[..., list[str]]
    normalize_names: Callable[[Sequence[str]], list[str]]
    persist_session_mcp_names: Callable[[sqlite3.Connection, int, Sequence[str]], None]
    persist_session_launch_selection: Callable[[sqlite3.Connection, SessionRow, SessionLaunchSelection], SessionLaunchSelection]
    load_session_external_id: Callable[[sqlite3.Connection, int, str], str]
    save_session_external_id: Callable[[sqlite3.Connection, int, str, str], None]
    backend_descriptor: Callable[[str], Any]
    resolve_netrunner_resume_session_id: Callable[..., str]
    maybe_configure_playwright_runtime_mode: Callable[..., str | None]
    bind_fixer_db_path_to_server_env: Callable[..., dict[str, dict[str, object]]]
    bind_netrunner_stateless_auth_to_server_env: Callable[..., dict[str, dict[str, object]]]
    bind_locked_role_to_server_env: Callable[..., dict[str, dict[str, object]]]
    bind_launcher_telegram_env_to_server_env: Callable[[dict[str, dict[str, object]]], dict[str, dict[str, object]]]
    build_droid_netrunner_prompt: Callable[..., str]
    build_netrunner_prompt: Callable[..., str]
    build_mcp_how_to_map: Callable[..., dict[str, str]]
    build_backend_launch_env: Callable[..., dict[str, str]]
    append_codex_apps_gate: Callable[..., list[str]]
    latest_matching_netrunner_codex_session_id: Callable[[Path, int], str | None]
    latest_codex_session_id_for_cwd: Callable[[Path], str | None]
    prompt_resume_session_id: Callable[[int, str], str | None]
    netrunner_kind_manual: str
    computer_use_mcp_name: str
    nondefault_role_auto_mcp_servers: set[str]
    forced_mcp_server: str


def launch_netrunner(
    passthrough_args: Sequence[str],
    *,
    preset_session_id: int | None,
    preset_backend: str | None = None,
    preset_model: str | None = None,
    preset_reasoning: str | None = None,
    preset_mcp_names: Sequence[str],
    dry_run: bool,
    Option: Any,
    single_select_items: Any,
    multi_select_items: Any,
    callbacks: NetrunnerLaunchCallbacks,
) -> int:
    from client_wires.codex_compat.llm import (
        ExecutionPreferences,
        LLMSelection,
        _load_llm_env,
        _merge_env_with_os,
        _reasoning_label,
    )

    cwd = Path.cwd()
    db_path = callbacks.resolve_fixer_db_path(cwd)
    # Keep launcher DB access in short-lived windows so interactive selection
    # never leaves SQLite pinned open while the operator is thinking.
    with closing(sqlite3.connect(db_path)) as conn:
        callbacks.ensure_wire_schema(conn)
        project_id = callbacks.ensure_project_registered(conn, cwd)
        sessions = callbacks.load_session_rows(conn, project_id)
    if not sessions:
        raise RuntimeError("No sessions found for current project.")

    session_by_id = {row.session_id: row for row in sessions}
    selected_via_interactive_picker = preset_session_id is None
    if preset_session_id is not None:
        if preset_session_id not in session_by_id:
            raise RuntimeError(f"Session {preset_session_id} is not available for the current project.")
        selected_session = session_by_id[preset_session_id]
    else:
        selected_session = callbacks.select_session_interactive(sessions, Option, single_select_items)

    netrunner_kind = callbacks.netrunner_kind_manual
    if selected_via_interactive_picker and selected_session.status == "pending" and not dry_run:
        netrunner_kind = callbacks.select_manual_netrunner_kind_interactive(Option, single_select_items)

    launch_selection = callbacks.resolve_netrunner_launch_selection(
        selected_session,
        preset_backend=preset_backend,
        preset_model=preset_model,
        preset_reasoning=preset_reasoning,
        dry_run=dry_run,
        Option=Option,
        single_select_items=single_select_items,
    )

    available_servers, config_env_vars, adapter, ensure_sqlite_scaffold = callbacks.load_available_servers(
        cwd,
        backend=launch_selection.backend,
    )
    with closing(sqlite3.connect(db_path)) as conn:
        callbacks.ensure_wire_schema(conn)
        callbacks.sync_registry_names(conn, list(available_servers.keys()))
        registry_meta = callbacks.load_registry_mcp_metadata(conn)
        assigned_names = callbacks.load_assigned_mcp_names(conn, selected_session.global_session_id)
        allowed_names = callbacks.load_project_allowed_mcp_names(conn, project_id)
    allowed_runtime_names = callbacks.allowed_runtime_mcp_names(allowed_names, available_servers)
    allowed_runtime_names = [name for name in allowed_runtime_names if name != callbacks.computer_use_mcp_name]
    assigned_allowed_names = callbacks.assigned_allowed_mcp_names(assigned_names, allowed_names)
    assigned_allowed_names = [name for name in assigned_allowed_names if name != callbacks.computer_use_mcp_name]
    for name in callbacks.nondefault_role_auto_mcp_servers:
        if name in assigned_allowed_names and name in available_servers:
            allowed_runtime_names = sorted(set(allowed_runtime_names).union({name}))
    preselected_names = callbacks.assigned_preselected_mcp_names(assigned_names, allowed_runtime_names)
    assigned_unavailable_names = sorted(set(assigned_allowed_names) - set(allowed_runtime_names))
    picker_pool_names = sorted(set(allowed_runtime_names).union(assigned_unavailable_names))

    if assigned_unavailable_names:
        print(
            "[warning] Assigned MCP server(s) unavailable in current runtime config: "
            + ", ".join(assigned_unavailable_names),
            file=sys.stderr,
        )

    if preset_mcp_names:
        selected_mcp_names = [
            name for name in callbacks.normalize_names(preset_mcp_names) if name != callbacks.computer_use_mcp_name
        ]
        invalid = sorted(
            name
            for name in selected_mcp_names
            if name not in allowed_runtime_names and name != callbacks.forced_mcp_server
        )
        if invalid:
            invalid_text = ", ".join(invalid)
            raise RuntimeError(
                "Preset MCP selection must be project-allowed and runtime-available. "
                f"Invalid: {invalid_text}"
            )
    elif dry_run:
        selected_mcp_names = sorted(set(preselected_names))
    else:
        selected_mcp_names = callbacks.select_mcp_interactive(
            picker_pool_names,
            preselected_names,
            registry_meta,
            available_servers,
            Option,
            multi_select_items,
            show_all_registry_names=True,
        )
        selected_mcp_names = [name for name in selected_mcp_names if name != callbacks.computer_use_mcp_name]

    if callbacks.forced_mcp_server in available_servers:
        selected_mcp_names = callbacks.normalize_names([*selected_mcp_names, callbacks.forced_mcp_server])
    else:
        print(
            f"[warning] {callbacks.forced_mcp_server} not found in launcher MCP set; continuing without forced attach",
            file=sys.stderr,
        )

    unknown = [name for name in selected_mcp_names if name not in available_servers]
    if unknown:
        unknown_text = ", ".join(sorted(unknown))
        raise RuntimeError(f"Selected MCP servers are unavailable in current launcher context: {unknown_text}")

    if not dry_run:
        with closing(sqlite3.connect(db_path)) as conn:
            callbacks.ensure_wire_schema(conn)
            callbacks.persist_session_mcp_names(conn, selected_session.global_session_id, selected_mcp_names)
            launch_selection = callbacks.persist_session_launch_selection(conn, selected_session, launch_selection)
            current_external_session_id = callbacks.load_session_external_id(
                conn,
                selected_session.global_session_id,
                launch_selection.backend,
            )
    else:
        launch_selection = SessionLaunchSelection(
            backend=normalize_backend_name(launch_selection.backend),
            model=launch_selection.model.strip(),
            reasoning=launch_selection.reasoning.strip(),
        )
        current_external_session_id = (
            selected_session.external_session_id
            if normalize_backend_name(selected_session.cli_backend) == launch_selection.backend
            else ""
        )

    selected_session = SessionRow(
        session_id=selected_session.session_id,
        global_session_id=selected_session.global_session_id,
        task_description=selected_session.task_description,
        status=selected_session.status,
        cli_backend=launch_selection.backend,
        cli_model=launch_selection.model,
        cli_reasoning=launch_selection.reasoning,
        external_session_id=current_external_session_id,
    )

    selected_servers = {name: dict(available_servers[name]) for name in selected_mcp_names}
    callbacks.maybe_configure_playwright_runtime_mode(
        adapter,
        selected_servers,
        available_servers,
        interactive=not dry_run,
    )
    if callbacks.forced_mcp_server in selected_servers:
        selected_servers = callbacks.bind_fixer_db_path_to_server_env(selected_servers, db_path=db_path)
        selected_servers = callbacks.bind_netrunner_stateless_auth_to_server_env(selected_servers, project_cwd=cwd)
        selected_servers = callbacks.bind_locked_role_to_server_env(selected_servers, role="netrunner")
        selected_servers = callbacks.bind_launcher_telegram_env_to_server_env(selected_servers)

    selected_config_paths: dict[str, Path] = {}
    if "sqlite" in selected_servers:
        sqlite_config = ensure_sqlite_scaffold(cwd)
        if sqlite_config is None:
            raise RuntimeError("Unable to scaffold sqliteMCP.toml for selected sqlite server.")
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

    model = launch_selection.model
    effort = launch_selection.reasoning
    llm_selection = LLMSelection(
        display_model=model,
        detail=_reasoning_label(model, effort),
        provider_slug="openai",
        model=model,
        reasoning_effort=effort,
        requires_provider_override=False,
    )
    if not dry_run:
        adapter.ensure_runtime_files(cwd, llm_selection, selected_servers, available_servers)
    execution_prefs = ExecutionPreferences(dangerous_sandbox=True, auto_approve=True)

    codex_args: list[str] = []
    codex_args.extend(adapter.build_llm_args(llm_selection))
    codex_args.extend(adapter.build_execution_args(execution_prefs))
    codex_args.extend(list(passthrough_args))
    codex_args = callbacks.append_codex_apps_gate(codex_args, adapter, allow_computer_use=False)

    option_args = [*codex_args, *adapter.build_mcp_flags(selected_servers, available_servers)]
    resume_external_session_id: str | None = None
    if selected_session.status != "pending":
        descriptor = callbacks.backend_descriptor(launch_selection.backend)
        if not descriptor.resume_supported:
            raise RuntimeError(
                f"Backend {launch_selection.backend!r} keeps resume metadata sticky by design, but live resume execution is not implemented yet."
            )
        resume_external_session_id = callbacks.resolve_netrunner_resume_session_id(
            cwd,
            selected_session,
            Option,
            single_select_items,
        )
        with closing(sqlite3.connect(db_path)) as conn:
            callbacks.ensure_wire_schema(conn)
            callbacks.save_session_external_id(
                conn,
                selected_session.global_session_id,
                launch_selection.backend,
                resume_external_session_id,
            )

    if selected_session.status == "pending":
        descriptor = callbacks.backend_descriptor(launch_selection.backend)
        if not descriptor.fresh_launch_supported:
            raise RuntimeError(
                f"Backend {launch_selection.backend!r} is surfaced in the launcher catalog, but fresh-launch execution is not implemented yet."
            )

    if resume_external_session_id:
        codex_cmd = adapter.build_resume_command(option_args, resume_external_session_id)
    else:
        codex_cmd = [adapter.command, *option_args]
        if normalize_backend_name(launch_selection.backend) == "droid":
            prompt = callbacks.build_droid_netrunner_prompt(
                selected_session.session_id,
                selected_mcp_names,
                netrunner_kind=netrunner_kind,
            )
        else:
            prompt = callbacks.build_netrunner_prompt(
                selected_session.session_id,
                selected_mcp_names,
                callbacks.build_mcp_how_to_map(selected_mcp_names, registry_meta),
                netrunner_kind=netrunner_kind,
            )
        if prompt:
            codex_cmd.extend(adapter.build_prompt_args(prompt))

    env = callbacks.build_backend_launch_env(
        adapter,
        llm_selection,
        cwd=cwd,
        load_llm_env=_load_llm_env,
        merge_env_with_os=_merge_env_with_os,
    )
    for server_name, config_path in selected_config_paths.items():
        env_var = config_env_vars.get(server_name)
        if env_var:
            env[env_var] = str(config_path)

    print(f"[fixer-wire] netrunner session: {selected_session.session_id}")
    print(f"[fixer-wire] netrunner backend: {launch_selection.backend}")
    print(f"[fixer-wire] netrunner model: {launch_selection.model}")
    print(f"[fixer-wire] netrunner reasoning: {launch_selection.reasoning}")
    print(f"[fixer-wire] netrunner manual mode: {netrunner_kind}")
    if resume_external_session_id:
        print(f"[fixer-wire] resuming {launch_selection.backend} session id: {resume_external_session_id}")
    print(f"[fixer-wire] netrunner MCP selection: {', '.join(selected_mcp_names) if selected_mcp_names else 'none'}")
    print("[fixer-wire] command:", codex_cmd)
    if dry_run:
        return 0
    before_matches: set[str] = set()
    if launch_selection.backend == "codex" and not resume_external_session_id:
        try:
            before_match = callbacks.latest_matching_netrunner_codex_session_id(cwd, selected_session.session_id)
            if before_match:
                before_matches.add(before_match)
        except RuntimeError:
            before_matches = set()
    before = callbacks.latest_codex_session_id_for_cwd(cwd) if launch_selection.backend == "codex" else None
    result = subprocess.call(codex_cmd, env=env, cwd=str(cwd))
    linked_session_id: str | None = None
    if resume_external_session_id:
        linked_session_id = resume_external_session_id
    else:
        if launch_selection.backend == "codex":
            try:
                after_match = callbacks.latest_matching_netrunner_codex_session_id(cwd, selected_session.session_id)
            except RuntimeError:
                after_match = None
            if after_match and after_match not in before_matches:
                linked_session_id = after_match
            elif after_match:
                linked_session_id = after_match
            else:
                after = callbacks.latest_codex_session_id_for_cwd(cwd)
                if after and after != before:
                    linked_session_id = after
        elif result == 0:
            linked_session_id = callbacks.prompt_resume_session_id(selected_session.session_id, launch_selection.backend)
    if linked_session_id:
        with closing(sqlite3.connect(db_path)) as conn:
            callbacks.ensure_wire_schema(conn)
            callbacks.save_session_external_id(
                conn,
                selected_session.global_session_id,
                launch_selection.backend,
                linked_session_id,
            )
    return result
