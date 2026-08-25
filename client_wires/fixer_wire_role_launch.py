"""Role launch workflow implementations for the Fixer wire launcher."""

from __future__ import annotations

from contextlib import closing
from dataclasses import dataclass
from pathlib import Path
import sqlite3
import subprocess
import sys
from typing import Any, Callable, Sequence

from client_wires.backends import is_codex_backend, normalize_backend_name
from client_wires import fixer_wire_prompts
from client_wires import fixer_wire_resume
from client_wires import fixer_wire_selectors


def _redacted_command_for_display(command: Sequence[str]) -> list[str]:
    displayed: list[str] = []
    for argument in command:
        if ".env={" in argument:
            displayed.append(argument.split(".env={", 1)[0] + ".env=<redacted>")
        else:
            displayed.append(argument)
    return displayed

@dataclass(frozen=True)
class RoleLaunchCallbacks:
    normalize_names: Callable[[Sequence[str]], list[str]]
    select_fresh_launch_selection: Callable[..., Any]
    load_available_servers: Callable[..., tuple[dict[str, dict[str, object]], dict[str, str], Any, Any]]
    resolve_fixer_db_path: Callable[[Path], Path]
    bind_fixer_db_path_to_server_env: Callable[..., dict[str, dict[str, object]]]
    bind_locked_role_to_server_env: Callable[..., dict[str, dict[str, object]]]
    bind_launcher_telegram_env_to_server_env: Callable[[dict[str, dict[str, object]]], dict[str, dict[str, object]]]
    append_droid_mcp_tool_guidance: Callable[..., str]
    append_codex_apps_gate: Callable[..., list[str]]
    build_backend_launch_env: Callable[..., dict[str, str]]
    assert_project_is_registered: Callable[[Path], None]
    select_fixer_launch_action_interactive: Callable[..., str]
    resolve_latest_fixer_resume_session_id: Callable[[Path], str | None]
    load_fixer_resume_summaries: Callable[[Path], list[Any]]
    select_fixer_resume_session_interactive: Callable[..., str]
    build_fixer_prompt: Callable[[], str]
    launch_fresh_role_session: Callable[..., int]
    resolve_unattached_fixer_cwd: Callable[[], Path]
    ensure_wire_schema: Callable[[sqlite3.Connection], None]
    ensure_unattached_fixer_project: Callable[..., int]
    build_unattached_fixer_prompt: Callable[[Path], str]
    select_overseer_launch_action_interactive: Callable[..., str]
    select_role_preset_server_names: Callable[..., list[str]]
    load_overseer_resume_summaries: Callable[[Path], list[Any]]
    select_overseer_resume_session_interactive: Callable[..., str]
    build_overseer_prompt: Callable[[], str]
    launch_unattached_fixer: Callable[..., int]
    forced_mcp_server: str
    figma_console_mcp_name: str
    fixer_launch_new: str
    fixer_launch_resume: str
    overseer_launch_new: str
    fixer_wire_model: str
    fixer_wire_reasoning_effort: str


def select_role_preset_server_names(
    available_servers: dict[str, dict[str, object]],
    *,
    cwd: Path,
    role: str | None = None,
    callbacks: RoleLaunchCallbacks,
) -> list[str]:
    selected_names = [
        name
        for name, config in available_servers.items()
        if config.get("_source") in {"project_mcp", "self_mcp"}
    ]
    if role in {"fixer", "overseer"}:
        selected_names = [name for name in selected_names if name != "react-native-guide"]
    if role == "overseer":
        selected_names = [name for name in selected_names if name != callbacks.figma_console_mcp_name]
    if not selected_names and "sqlite" in available_servers and (cwd / "sqliteMCP.toml").is_file():
        selected_names.append("sqlite")
    if callbacks.forced_mcp_server in available_servers:
        selected_names.append(callbacks.forced_mcp_server)
    return callbacks.normalize_names(selected_names)


def _selected_servers_for_names(
    available_servers: dict[str, dict[str, object]],
    selected_mcp_names: Sequence[str],
    *,
    callbacks: RoleLaunchCallbacks,
) -> dict[str, dict[str, object]]:
    normalized_names = callbacks.normalize_names(selected_mcp_names)
    selected_servers = {
        name: dict(available_servers[name]) for name in normalized_names if name in available_servers
    }
    missing_names = [name for name in normalized_names if name not in available_servers]
    if missing_names:
        missing_text = ", ".join(missing_names)
        raise RuntimeError(f"Selected MCP servers are unavailable in current launcher context: {missing_text}")
    return selected_servers


def _bind_role_server_env(
    selected_servers: dict[str, dict[str, object]],
    *,
    cwd: Path,
    role: str,
    callbacks: RoleLaunchCallbacks,
) -> dict[str, dict[str, object]]:
    if callbacks.forced_mcp_server not in selected_servers:
        return selected_servers
    selected_servers = callbacks.bind_fixer_db_path_to_server_env(
        selected_servers,
        db_path=callbacks.resolve_fixer_db_path(cwd),
    )
    selected_servers = callbacks.bind_locked_role_to_server_env(
        selected_servers,
        role=role,
        project_cwd=cwd,
    )
    return callbacks.bind_launcher_telegram_env_to_server_env(selected_servers)


def _selected_sqlite_config_paths(
    selected_servers: dict[str, dict[str, object]],
    *,
    cwd: Path,
    ensure_sqlite_scaffold: Any,
) -> dict[str, Path]:
    selected_config_paths: dict[str, Path] = {}
    if "sqlite" in selected_servers:
        sqlite_config = ensure_sqlite_scaffold(cwd)
        if sqlite_config is None:
            raise RuntimeError("Unable to scaffold sqliteMCP.toml for selected sqlite server.")
        selected_config_paths["sqlite"] = sqlite_config
    return selected_config_paths


def _adapter_resume_command(adapter: Any, option_args: Sequence[str], external_session_id: str) -> list[str]:
    build_resume_command = getattr(adapter, "build_resume_command", None)
    if callable(build_resume_command):
        return list(build_resume_command(option_args, external_session_id))
    if is_codex_backend(getattr(adapter, "name", "")):
        return [adapter.command, "resume", *list(option_args), external_session_id]
    return [adapter.command, "resume", *list(option_args), external_session_id]


def _role_interactive_execution_args(adapter: Any, execution_prefs: Any, *, role: str) -> list[str]:
    backend = normalize_backend_name(getattr(adapter, "name", getattr(adapter, "command", "")))
    if role in {"fixer", "netrunner"} and is_codex_backend(backend):
        # Project-bound Fixer and Hands/Netrunner clients must stay local so
        # this launcher's per-process MCP selection is applied. A remote Codex
        # TUI would inherit the managed app-server configuration and can fail
        # when that app-server is not running.
        return list(adapter.build_execution_args(execution_prefs))
    return list(adapter.build_interactive_execution_args(execution_prefs))


def _apply_selected_config_paths(
    env: dict[str, str],
    *,
    selected_config_paths: dict[str, Path],
    config_env_vars: dict[str, str],
) -> None:
    for server_name, config_path in selected_config_paths.items():
        env_var = config_env_vars.get(server_name)
        if env_var:
            env[env_var] = str(config_path)


def _forced_fixer_mcp_names(
    available_servers: dict[str, dict[str, object]],
    *,
    callbacks: RoleLaunchCallbacks,
) -> list[str]:
    if callbacks.forced_mcp_server in available_servers:
        return [callbacks.forced_mcp_server]
    print(
        f"[warning] {callbacks.forced_mcp_server} not found in launcher MCP set; continuing without forced attach",
        file=sys.stderr,
    )
    return []


def launch_fresh_role_session(
    role: str,
    prompt: str,
    passthrough_args: Sequence[str],
    *,
    launch_cwd: Path | None = None,
    selected_mcp_names: Sequence[str],
    dry_run: bool,
    preset_backend: str | None,
    preset_model: str | None,
    preset_reasoning: str | None,
    dangerous_sandbox: bool,
    Option: Any,
    single_select_items: Any,
    callbacks: RoleLaunchCallbacks,
    launch_label: str | None = None,
    resume_external_session_id: str | None = None,
) -> int:
    from client_wires.codex_compat.llm import (
        ExecutionPreferences,
        LLMSelection,
        _load_llm_env,
        _merge_env_with_os,
        _reasoning_label,
    )

    cwd = (launch_cwd or Path.cwd()).resolve()
    launch_selection = callbacks.select_fresh_launch_selection(
        preset_backend=preset_backend,
        preset_model=preset_model,
        preset_reasoning=preset_reasoning,
        Option=Option,
        single_select_items=single_select_items,
    )
    available_servers, config_env_vars, adapter, ensure_sqlite_scaffold = callbacks.load_available_servers(
        cwd,
        backend=launch_selection.backend,
    )
    selected_servers = _selected_servers_for_names(
        available_servers,
        selected_mcp_names,
        callbacks=callbacks,
    )
    selected_servers = _bind_role_server_env(selected_servers, cwd=cwd, role=role, callbacks=callbacks)
    selected_config_paths = _selected_sqlite_config_paths(
        selected_servers,
        cwd=cwd,
        ensure_sqlite_scaffold=ensure_sqlite_scaffold,
    )

    llm_selection = LLMSelection(
        display_model=launch_selection.model,
        detail=_reasoning_label(launch_selection.model, launch_selection.reasoning),
        provider_slug="openai",
        model=launch_selection.model,
        reasoning_effort=launch_selection.reasoning,
        requires_provider_override=False,
    )
    execution_prefs = ExecutionPreferences(dangerous_sandbox=dangerous_sandbox, auto_approve=True)
    codex_args: list[str] = []
    if not resume_external_session_id:
        codex_args.extend(adapter.build_llm_args(llm_selection))
    codex_args.extend(_role_interactive_execution_args(adapter, execution_prefs, role=role))
    codex_args = callbacks.append_codex_apps_gate(codex_args, adapter, allow_computer_use=False)
    codex_args.extend(list(passthrough_args))
    option_args = [*codex_args, *adapter.build_mcp_flags(selected_servers, available_servers)]
    command = (
        adapter.build_resume_command(option_args, resume_external_session_id.strip())
        if resume_external_session_id
        else [adapter.command, *option_args]
    )
    launch_prompt = "" if resume_external_session_id else callbacks.append_droid_mcp_tool_guidance(
        fixer_wire_prompts.materialize_fixer_provider_prompt(prompt, launch_selection.backend)
        if role == "fixer"
        else prompt,
        backend=launch_selection.backend,
        mcp_names=selected_mcp_names,
    )
    if launch_prompt:
        # Kimi Code must stay interactive in every Project Hands launch. Never
        # add -p/--print here: the Architect enters the prompt in the Kimi TUI,
        # exactly like fixer -> fixer.
        command.extend(adapter.build_prompt_args(launch_prompt))

    env = callbacks.build_backend_launch_env(
        adapter,
        llm_selection,
        cwd=cwd,
        load_llm_env=_load_llm_env,
        merge_env_with_os=_merge_env_with_os,
    )
    _apply_selected_config_paths(
        env,
        selected_config_paths=selected_config_paths,
        config_env_vars=config_env_vars,
    )

    display_label = launch_label or f"{role} session"
    print(f"[fixer-wire] {'resuming' if resume_external_session_id else 'starting new'} {display_label}")
    print(f"[fixer-wire] {display_label} backend: {launch_selection.backend}")
    print(f"[fixer-wire] {display_label} model: {launch_selection.model}")
    print(f"[fixer-wire] {display_label} reasoning: {launch_selection.reasoning}")
    print(
        f"[fixer-wire] {display_label} MCP selection: "
        f"{', '.join(sorted(selected_servers)) if selected_servers else 'none'}"
    )
    print("[fixer-wire] command:", _redacted_command_for_display(command))
    if (
        launch_selection.backend in {"kimi-code", "commandcode"}
        and launch_prompt
        and not dry_run
    ):
        print("[fixer-wire] kimi shell mode cannot auto-submit; paste the following into the Kimi TUI:")
        print(launch_prompt)
    if dry_run:
        return 0

    adapter.ensure_runtime_files(cwd, llm_selection, selected_servers, available_servers)
    return subprocess.call(command, env=env, cwd=str(cwd))


def launch_fixer(
    passthrough_args: Sequence[str],
    *,
    launch_cwd: Path | None = None,
    dry_run: bool,
    preset_resume_latest: bool,
    preset_resume_session_id: str | None,
    Option: Any,
    single_select_items: Any,
    callbacks: RoleLaunchCallbacks,
) -> int:
    from client_wires.codex_compat.llm import (
        ExecutionPreferences,
        LLMSelection,
        _load_llm_env,
        _merge_env_with_os,
        _reasoning_label,
    )

    cwd = (launch_cwd or Path.cwd()).resolve()
    callbacks.assert_project_is_registered(cwd)
    resume_provider = "codex"
    resume_session_id: str | None = None
    if preset_resume_session_id:
        resume_selection = fixer_wire_resume.parse_fixer_resume_selection(str(preset_resume_session_id))
        resume_provider = resume_selection.provider
        resume_session_id = resume_selection.session_id
        if not resume_session_id:
            raise RuntimeError("Explicit fixer session id must be non-empty.")
    elif preset_resume_latest:
        resume_selection = fixer_wire_resume.parse_fixer_resume_selection(
            callbacks.resolve_latest_fixer_resume_session_id(cwd)
        )
        resume_provider = resume_selection.provider
        resume_session_id = resume_selection.session_id
    else:
        launch_mode = callbacks.select_fixer_launch_action_interactive(Option, single_select_items)
        if launch_mode == fixer_wire_selectors.UNATTACHED_FIXER_ACTION:
            return callbacks.launch_unattached_fixer(passthrough_args, dry_run=dry_run, Option=Option, single_select_items=single_select_items)
        if launch_mode == callbacks.fixer_launch_resume:
            fixer_summaries = callbacks.load_fixer_resume_summaries(cwd)
            raw_selection = callbacks.select_fixer_resume_session_interactive(
                fixer_summaries,
                Option,
                single_select_items,
            )
            resume_selection = fixer_wire_resume.parse_fixer_resume_selection(raw_selection)
            resume_provider = resume_selection.provider
            resume_session_id = resume_selection.session_id
        else:
            available_servers, _config_env_vars, _adapter, _ensure_sqlite_scaffold = callbacks.load_available_servers(cwd)
            selected_mcp_names = _forced_fixer_mcp_names(available_servers, callbacks=callbacks)
            return callbacks.launch_fresh_role_session(
                "fixer",
                callbacks.build_fixer_prompt(),
                passthrough_args,
                launch_cwd=cwd,
                selected_mcp_names=selected_mcp_names,
                dry_run=dry_run,
                preset_backend=None,
                preset_model=None,
                preset_reasoning=None,
                dangerous_sandbox=True,
                Option=Option,
                single_select_items=single_select_items,
            )

    available_servers, config_env_vars, adapter, ensure_sqlite_scaffold = callbacks.load_available_servers(
        cwd,
        backend=resume_provider,
    )
    selected_mcp_names = _forced_fixer_mcp_names(available_servers, callbacks=callbacks)
    selected_servers = {name: available_servers[name] for name in selected_mcp_names}
    selected_servers = _bind_role_server_env(selected_servers, cwd=cwd, role="fixer", callbacks=callbacks)
    selected_config_paths = _selected_sqlite_config_paths(
        selected_servers,
        cwd=cwd,
        ensure_sqlite_scaffold=ensure_sqlite_scaffold,
    )

    model = str(getattr(adapter, "default_model", callbacks.fixer_wire_model))
    effort = str(getattr(adapter, "default_reasoning", callbacks.fixer_wire_reasoning_effort))
    llm_selection = LLMSelection(
        display_model=model,
        detail=_reasoning_label(model, effort),
        provider_slug="openai",
        model=model,
        reasoning_effort=effort,
        requires_provider_override=False,
    )
    execution_prefs = ExecutionPreferences(dangerous_sandbox=True, auto_approve=True)

    codex_args: list[str] = []
    if not resume_session_id:
        codex_args.extend(adapter.build_llm_args(llm_selection))
    codex_args.extend(_role_interactive_execution_args(adapter, execution_prefs, role="fixer"))
    codex_args = callbacks.append_codex_apps_gate(codex_args, adapter, allow_computer_use=False)
    codex_args.extend(list(passthrough_args))

    option_args = [*codex_args, *adapter.build_mcp_flags(selected_servers, available_servers)]
    if resume_session_id:
        codex_cmd = _adapter_resume_command(adapter, option_args, resume_session_id)
    else:
        codex_cmd = [adapter.command, *option_args]
        prompt = callbacks.build_fixer_prompt()
        if prompt:
            codex_cmd.extend(adapter.build_prompt_args(prompt))

    env = callbacks.build_backend_launch_env(
        adapter,
        llm_selection,
        cwd=cwd,
        load_llm_env=_load_llm_env,
        merge_env_with_os=_merge_env_with_os,
    )
    _apply_selected_config_paths(
        env,
        selected_config_paths=selected_config_paths,
        config_env_vars=config_env_vars,
    )

    if resume_session_id:
        print(f"[fixer-wire] resuming fixer {resume_provider} session id: {resume_session_id}")
    else:
        print(f"[fixer-wire] starting new fixer {resume_provider} session")
    print(f"[fixer-wire] fixer MCP selection: {', '.join(selected_mcp_names) if selected_mcp_names else 'none'}")
    print("[fixer-wire] command:", _redacted_command_for_display(codex_cmd))
    if dry_run:
        return 0

    adapter.ensure_runtime_files(cwd, llm_selection, selected_servers, available_servers)
    return subprocess.call(codex_cmd, env=env, cwd=str(cwd))


def launch_new_fixer_chat(
    *,
    launch_cwd: Path,
    backend: str,
    model: str,
    reasoning: str,
    dry_run: bool,
    passthrough_args: Sequence[str] = (),
    Option: Any,
    single_select_items: Any,
    callbacks: RoleLaunchCallbacks,
) -> int:
    """Launch a dashboard-created Fixer through the canonical role path.

    Keeping this as a thin entrypoint makes the app flow inherit the same
    role-locked Fixer MCP environment, init-fixer prompt, backend adapter, and
    runtime materialization as the manual ``fixer`` alias.
    """

    cwd = launch_cwd.resolve()
    selected_backend = normalize_backend_name(backend)
    selected_model = str(model).strip()
    selected_reasoning = str(reasoning).strip()
    if not selected_model:
        raise ValueError("Fixer chat model must be non-empty.")
    if not selected_reasoning:
        raise ValueError("Fixer chat reasoning must be non-empty.")

    callbacks.assert_project_is_registered(cwd)
    available_servers, _config_env_vars, _adapter, _ensure_sqlite_scaffold = callbacks.load_available_servers(
        cwd,
        backend=selected_backend,
    )
    selected_mcp_names = _forced_fixer_mcp_names(available_servers, callbacks=callbacks)
    return callbacks.launch_fresh_role_session(
        "fixer",
        callbacks.build_fixer_prompt(),
        passthrough_args,
        launch_cwd=cwd,
        selected_mcp_names=selected_mcp_names,
        dry_run=dry_run,
        preset_backend=selected_backend,
        preset_model=selected_model,
        preset_reasoning=selected_reasoning,
        dangerous_sandbox=True,
        Option=Option,
        single_select_items=single_select_items,
    )


def launch_unattached_fixer(
    passthrough_args: Sequence[str],
    *,
    dry_run: bool,
    Option: Any,
    single_select_items: Any,
    callbacks: RoleLaunchCallbacks,
) -> int:
    scratch_cwd = callbacks.resolve_unattached_fixer_cwd()
    db_path = callbacks.resolve_fixer_db_path(scratch_cwd)
    with closing(sqlite3.connect(db_path)) as conn:
        callbacks.ensure_wire_schema(conn)
        callbacks.ensure_unattached_fixer_project(conn, scratch_cwd=scratch_cwd)

    available_servers, _config_env_vars, _adapter, _ensure_sqlite_scaffold = callbacks.load_available_servers(scratch_cwd)
    selected_mcp_names = _forced_fixer_mcp_names(available_servers, callbacks=callbacks)

    return callbacks.launch_fresh_role_session(
        "fixer",
        callbacks.build_unattached_fixer_prompt(scratch_cwd),
        passthrough_args,
        launch_cwd=scratch_cwd,
        selected_mcp_names=selected_mcp_names,
        dry_run=dry_run,
        preset_backend=None,
        preset_model=None,
        preset_reasoning=None,
        dangerous_sandbox=True,
        Option=Option,
        single_select_items=single_select_items,
    )


def launch_overseer(
    passthrough_args: Sequence[str],
    *,
    launch_cwd: Path | None = None,
    preset_backend: str | None = None,
    preset_model: str | None = None,
    preset_reasoning: str | None = None,
    preset_resume_session_id: str | None = None,
    dry_run: bool,
    Option: Any,
    single_select_items: Any,
    callbacks: RoleLaunchCallbacks,
) -> int:
    from client_wires.codex_compat.llm import (
        ExecutionPreferences,
        LLMSelection,
        _load_llm_env,
        _merge_env_with_os,
        _reasoning_label,
    )

    cwd = (launch_cwd or Path.cwd()).resolve()
    if launch_cwd is None:
        callbacks.assert_project_is_registered(cwd)
    elif not cwd.is_dir():
        raise RuntimeError(f"Overseer launch cwd is not an existing directory: {cwd}")

    launch_mode = (
        "preset_resume"
        if preset_resume_session_id
        else callbacks.select_overseer_launch_action_interactive(Option, single_select_items)
    )
    if launch_mode == callbacks.overseer_launch_new:
        available_servers, _config_env_vars, _adapter, _ensure_sqlite_scaffold = callbacks.load_available_servers(cwd)
        selected_mcp_names = callbacks.select_role_preset_server_names(
            available_servers,
            cwd=cwd,
            role="overseer",
        )
        return callbacks.launch_fresh_role_session(
            "overseer",
            callbacks.build_overseer_prompt(),
            passthrough_args,
            launch_cwd=cwd,
            selected_mcp_names=selected_mcp_names,
            dry_run=dry_run,
            preset_backend=preset_backend,
            preset_model=preset_model,
            preset_reasoning=preset_reasoning,
            dangerous_sandbox=True,
            Option=Option,
            single_select_items=single_select_items,
        )

    overseer_summaries = callbacks.load_overseer_resume_summaries(cwd)
    raw_selection = preset_resume_session_id or callbacks.select_overseer_resume_session_interactive(
        overseer_summaries,
        Option,
        single_select_items,
    )
    resume_selection = fixer_wire_resume.parse_fixer_resume_selection(raw_selection)
    if resume_selection.provider == "codex":
        matching_summary = next(
            (
                summary
                for summary in overseer_summaries
                if str(summary.session_id) == resume_selection.session_id
            ),
            None,
        )
        if matching_summary is not None:
            resume_selection = fixer_wire_resume.FixerResumeSelection(
                provider=fixer_wire_resume.summary_provider(matching_summary),
                session_id=resume_selection.session_id,
            )
    resume_provider = resume_selection.provider
    resume_session_id = resume_selection.session_id
    available_servers, config_env_vars, adapter, ensure_sqlite_scaffold = callbacks.load_available_servers(
        cwd,
        backend=resume_provider,
    )
    selected_mcp_names = callbacks.select_role_preset_server_names(
        available_servers,
        cwd=cwd,
        role="overseer",
    )
    selected_servers = _selected_servers_for_names(
        available_servers,
        selected_mcp_names,
        callbacks=callbacks,
    )
    selected_servers = _bind_role_server_env(selected_servers, cwd=cwd, role="overseer", callbacks=callbacks)
    selected_config_paths = _selected_sqlite_config_paths(
        selected_servers,
        cwd=cwd,
        ensure_sqlite_scaffold=ensure_sqlite_scaffold,
    )

    model = str(getattr(adapter, "default_model", callbacks.fixer_wire_model))
    effort = str(getattr(adapter, "default_reasoning", callbacks.fixer_wire_reasoning_effort))
    llm_selection = LLMSelection(
        display_model=model,
        detail=_reasoning_label(model, effort),
        provider_slug="openai",
        model=model,
        reasoning_effort=effort,
        requires_provider_override=False,
    )
    execution_prefs = ExecutionPreferences(dangerous_sandbox=True, auto_approve=True)

    codex_args: list[str] = []
    codex_args.extend(adapter.build_interactive_execution_args(execution_prefs))
    codex_args.extend(list(passthrough_args))
    codex_args = callbacks.append_codex_apps_gate(codex_args, adapter, allow_computer_use=False)
    option_args = [*codex_args, *adapter.build_mcp_flags(selected_servers, available_servers)]
    command = _adapter_resume_command(adapter, option_args, resume_session_id)

    env = callbacks.build_backend_launch_env(
        adapter,
        llm_selection,
        cwd=cwd,
        load_llm_env=_load_llm_env,
        merge_env_with_os=_merge_env_with_os,
    )
    _apply_selected_config_paths(
        env,
        selected_config_paths=selected_config_paths,
        config_env_vars=config_env_vars,
    )

    print(f"[fixer-wire] resuming overseer {resume_provider} session id: {resume_session_id}")
    print(f"[fixer-wire] overseer MCP selection: {', '.join(sorted(selected_servers)) if selected_servers else 'none'}")
    print("[fixer-wire] command:", _redacted_command_for_display(command))
    if dry_run:
        return 0

    adapter.ensure_runtime_files(cwd, llm_selection, selected_servers, available_servers)
    return subprocess.call(command, env=env, cwd=str(cwd))
