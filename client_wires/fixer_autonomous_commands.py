"""Command and environment builders for autonomous Fixer launches."""

from __future__ import annotations

from pathlib import Path
from typing import Any, Callable

from client_wires import fixer_wire


def _build_common_codex_env(adapter: Any, llm_selection: Any, cwd: Path) -> dict[str, str]:
    from client_wires.codex_compat.llm import _load_llm_env, _merge_env_with_os

    return fixer_wire._build_backend_launch_env(
        adapter,
        llm_selection,
        cwd=cwd,
        load_llm_env=_load_llm_env,
        merge_env_with_os=_merge_env_with_os,
    )


def _build_exec_prefix(
    cwd: Path,
    *,
    bootstrap_codex_pro_import_path_fn: Callable[[], object],
    build_common_codex_env_fn: Callable[[Any, Any, Path], dict[str, str]],
) -> tuple[list[str], dict[str, str], Any]:
    bootstrap_codex_pro_import_path_fn()
    available_servers, config_env_vars, adapter, ensure_sqlite_scaffold = fixer_wire._load_available_servers(cwd)
    from client_wires.codex_compat.llm import LLMSelection

    llm_selection = LLMSelection(
        display_model=fixer_wire.FIXER_WIRE_MODEL,
        detail=fixer_wire.FIXER_WIRE_REASONING_EFFORT,
        provider_slug="openai",
        model=fixer_wire.FIXER_WIRE_MODEL,
        reasoning_effort=fixer_wire.FIXER_WIRE_REASONING_EFFORT,
        requires_provider_override=False,
    )
    env = build_common_codex_env_fn(adapter, llm_selection, cwd)
    prefix = [
        adapter.command,
        "--model",
        fixer_wire.FIXER_WIRE_MODEL,
        "--dangerously-bypass-approvals-and-sandbox",
    ]
    return prefix, env, (available_servers, config_env_vars, adapter, ensure_sqlite_scaffold)


def _select_forced_fixer_server(
    cwd: Path,
    env: dict[str, str],
    available_bundle: Any,
) -> tuple[dict[str, dict[str, object]], Any]:
    available_servers, config_env_vars, adapter, ensure_sqlite_scaffold = available_bundle
    fixer_wire._ensure_forced_fixer_server_resolved(available_servers)
    selected_servers: dict[str, dict[str, object]] = {}
    if fixer_wire.FORCED_MCP_SERVER in available_servers:
        selected_servers[fixer_wire.FORCED_MCP_SERVER] = available_servers[fixer_wire.FORCED_MCP_SERVER]
    if fixer_wire.FORCED_MCP_SERVER in selected_servers:
        selected_servers = fixer_wire._bind_fixer_db_path_to_server_env(
            selected_servers,
            db_path=fixer_wire._resolve_fixer_db_path(cwd),
        )
        selected_servers = fixer_wire._bind_locked_role_to_server_env(selected_servers, role="fixer")
        selected_servers = fixer_wire._bind_launcher_telegram_env_to_server_env(selected_servers)
    selected_config_paths: dict[str, Path] = {}
    if "sqlite" in selected_servers:
        sqlite_config = ensure_sqlite_scaffold(cwd)
        if sqlite_config is not None:
            selected_config_paths["sqlite"] = sqlite_config
    for server_name, config_path in selected_config_paths.items():
        env_var = config_env_vars.get(server_name)
        if env_var:
            env[env_var] = str(config_path)
    return selected_servers, adapter


def _build_fixer_exec_command(
    cwd: Path,
    prompt: str,
    *,
    build_exec_prefix_fn: Callable[[Path], tuple[list[str], dict[str, str], Any]],
) -> tuple[list[str], dict[str, str]]:
    prefix, env, available_bundle = build_exec_prefix_fn(cwd)
    available_servers = available_bundle[0]
    selected_servers, adapter = _select_forced_fixer_server(cwd, env, available_bundle)
    command = [
        *prefix,
        *adapter.build_mcp_flags(selected_servers, available_servers),
        "exec",
        "--skip-git-repo-check",
        prompt,
    ]
    return command, env


def _build_fixer_resume_command(
    cwd: Path,
    fixer_session_id: str,
    prompt: str,
    *,
    build_exec_prefix_fn: Callable[[Path], tuple[list[str], dict[str, str], Any]],
) -> tuple[list[str], dict[str, str]]:
    prefix, env, available_bundle = build_exec_prefix_fn(cwd)
    available_servers = available_bundle[0]
    selected_servers, adapter = _select_forced_fixer_server(cwd, env, available_bundle)
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
