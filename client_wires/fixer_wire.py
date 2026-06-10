#!/usr/bin/env python3
"""Canonical Fixer MCP wire entrypoint for fixer/netrunner/overseer launch."""

from __future__ import annotations

import argparse
from contextlib import closing
import os
import shutil
import sqlite3
import subprocess
import sys
import time
from pathlib import Path
from typing import Any, Callable, Sequence

if __package__ in (None, ""):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from client_wires.bootstrap import bootstrap_codex_pro_import_path, wire_info_lines
from client_wires.backends import (
    DEFAULT_BACKEND,
    available_backend_descriptors,
    get_backend_adapter,
    normalize_backend_name,
)
from client_wires import launch_env
from client_wires import fixer_wire_db
from client_wires import fixer_wire_launch_support
from client_wires import fixer_wire_mcp
from client_wires import fixer_wire_netrunner_launch
from client_wires import fixer_wire_prompts
from client_wires import fixer_wire_resume
from client_wires import fixer_wire_role_launch
from client_wires import fixer_wire_selectors
from client_wires.mvp_scaffold import run_scaffold_cli

ROLE_CHOICES = fixer_wire_mcp.ROLE_CHOICES
SCAFFOLD_MVP_ACTION = "__scaffold_mvp__"
UNATTACHED_FIXER_ACTION = "__unattached_fixer__"
FORCED_MCP_SERVER = fixer_wire_mcp.FORCED_MCP_SERVER
HIDDEN_MCP_SERVERS = fixer_wire_mcp.HIDDEN_MCP_SERVERS
NONDEFAULT_ROLE_AUTO_MCP_SERVERS = {"react-native-guide"}
MCP_CATEGORY_ORDER = ("DB", "Web-search", "Design", "Productivity", "Coding", "Other")
MCP_FALLBACK_CATEGORY = "Other"
FIGMA_CONSOLE_MCP_NAME = fixer_wire_mcp.FIGMA_CONSOLE_MCP_NAME
FIGMA_CONSOLE_MCP_FALLBACK_CATEGORY = fixer_wire_mcp.FIGMA_CONSOLE_MCP_FALLBACK_CATEGORY
FIGMA_CONSOLE_MCP_FALLBACK_HOW_TO = fixer_wire_mcp.FIGMA_CONSOLE_MCP_FALLBACK_HOW_TO
FIGMA_CONSOLE_MCP_TOKEN_ENV_NAMES = fixer_wire_mcp.FIGMA_CONSOLE_MCP_TOKEN_ENV_NAMES
RESEARCH_QUERY_MCP_NAME = fixer_wire_mcp.RESEARCH_QUERY_MCP_NAME
PHILOLOGISTS_PROJECT_MARKER = fixer_wire_mcp.PHILOLOGISTS_PROJECT_MARKER
ALWAYS_VISIBLE_MCP_NAMES = {FIGMA_CONSOLE_MCP_NAME}
WEB_STACK_GUIDANCE_MCP_NAMES = fixer_wire_prompts.WEB_STACK_GUIDANCE_MCP_NAMES
STANDARD_WEB_STACK_GUIDANCE = fixer_wire_prompts.STANDARD_WEB_STACK_GUIDANCE
RECENTLY_ACTIVE_STATUSES = {"in_progress"}
ARCHIVED_STATUSES = {"review", "completed"}
TOGGLE_ARCHIVED_VALUE = "__toggle_archived__"
FIXER_LAUNCH_NEW = "__fixer_launch_new__"
FIXER_LAUNCH_RESUME = "__fixer_launch_resume__"
OVERSEER_LAUNCH_NEW = "__overseer_launch_new__"
OVERSEER_LAUNCH_RESUME = "__overseer_launch_resume__"
COMPUTER_USE_MCP_NAME = "computer-use"
NETRUNNER_KIND_MANUAL = fixer_wire_prompts.NETRUNNER_KIND_MANUAL
NETRUNNER_KIND_ACCEPTANCE = fixer_wire_prompts.NETRUNNER_KIND_ACCEPTANCE
FIXER_SKILL_MARKER = "Activate skill `$init-fixer` immediately."
NETRUNNER_MANUAL_SKILL_NAME = fixer_wire_prompts.NETRUNNER_MANUAL_SKILL_NAME
NETRUNNER_ACCEPTANCE_SKILL_NAME = fixer_wire_prompts.NETRUNNER_ACCEPTANCE_SKILL_NAME
NETRUNNER_SKILL_MARKER = f"Activate skill `${NETRUNNER_MANUAL_SKILL_NAME}` immediately."
NETRUNNER_ACCEPTANCE_SKILL_MARKER = f"Activate skill `${NETRUNNER_ACCEPTANCE_SKILL_NAME}` immediately."
OVERSEER_SKILL_MARKER = "Activate skill `$init-overseer` immediately."
FIXER_SKILL_MARKERS = (
    FIXER_SKILL_MARKER,
    "Activate skill `$start-fixer` immediately.",
)
NETRUNNER_SKILL_MARKERS = (
    NETRUNNER_SKILL_MARKER,
    NETRUNNER_ACCEPTANCE_SKILL_MARKER,
    "Activate skill `$start-netrunner` immediately.",
)
OVERSEER_SKILL_MARKERS = (
    OVERSEER_SKILL_MARKER,
    "Activate skill `$start-overseer` immediately.",
)
FIXER_DB_PATH_ENV = fixer_wire_mcp.FIXER_DB_PATH_ENV
FIXER_MCP_DEFAULT_ROLE_ENV = fixer_wire_mcp.FIXER_MCP_DEFAULT_ROLE_ENV
FIXER_MCP_DEFAULT_CWD_ENV = fixer_wire_mcp.FIXER_MCP_DEFAULT_CWD_ENV
FIXER_MCP_LOCKED_ROLE_ENV = fixer_wire_mcp.FIXER_MCP_LOCKED_ROLE_ENV
FIXER_UNATTACHED_CWD_ENV = "FIXER_UNATTACHED_CWD"
UNATTACHED_FIXER_PROJECT_NAME = "Unattached Fixer"
FIXER_MCP_TELEGRAM_ENV_NAMES = fixer_wire_mcp.FIXER_MCP_TELEGRAM_ENV_NAMES
FIXER_MCP_BINARY_ENV = fixer_wire_mcp.FIXER_MCP_BINARY_ENV
PRIMARY_FIXER_DB_FILENAME = "fixer.db"
WEB_MCP_CONFIG_FILENAME = fixer_wire_mcp.WEB_MCP_CONFIG_FILENAME
FIXER_MCP_AUTOBUILD_SKIP_ENV = fixer_wire_mcp.FIXER_MCP_AUTOBUILD_SKIP_ENV
FIXER_WIRE_MODEL = "gpt-5.5"
FIXER_WIRE_REASONING_EFFORT = "high"
FORCED_FIXER_MCP_TIMEOUT_FLOOR_SEC = fixer_wire_mcp.FORCED_FIXER_MCP_TIMEOUT_FLOOR_SEC
FORCED_FIXER_MCP_TIMEOUT_FLOOR_MS = fixer_wire_mcp.FORCED_FIXER_MCP_TIMEOUT_FLOOR_MS
_FIXER_MCP_BUILD_CHECKED = fixer_wire_mcp._FIXER_MCP_BUILD_CHECKED

SessionRow = fixer_wire_db.SessionRow
SessionLaunchSelection = fixer_wire_db.SessionLaunchSelection
RegistryMcpMetadata = fixer_wire_db.RegistryMcpMetadata


def _parse_wire_args(argv: Sequence[str]) -> tuple[argparse.Namespace, list[str]]:
    parser = argparse.ArgumentParser(add_help=False)
    parser.add_argument("--role", choices=ROLE_CHOICES)
    parser.add_argument("--wire-info", action="store_true")
    parser.add_argument("--fixer-resume-latest", action="store_true")
    parser.add_argument("--fixer-session-id")
    parser.add_argument("--netrunner-session-id", type=int)
    parser.add_argument("--netrunner-backend")
    parser.add_argument("--netrunner-model")
    parser.add_argument("--netrunner-reasoning")
    parser.add_argument("--scaffold-mvp", "--scaffold", dest="scaffold_mvp")
    parser.add_argument("--scaffold-target-dir")
    parser.add_argument(
        "--netrunner-mcp",
        action="append",
        default=[],
        help="Repeat or pass comma-separated MCP server names for netrunner overrides.",
    )
    parser.add_argument("--dry-run", action="store_true")
    return parser.parse_known_args(list(argv))


def _normalize_names(values: Sequence[str]) -> list[str]:
    seen: set[str] = set()
    names: list[str] = []
    for raw in values:
        for part in raw.split(","):
            name = part.strip()
            if not name or name in seen:
                continue
            seen.add(name)
            names.append(name)
    names.sort()
    return names


def _dedupe_link_table(conn: sqlite3.Connection, table_name: str, partition_by: Sequence[str]) -> None:
    fixer_wire_db._dedupe_link_table(conn, table_name, partition_by)


def _ensure_wire_schema(conn: sqlite3.Connection) -> None:
    fixer_wire_db._ensure_wire_schema(conn)


def _repo_root() -> Path:
    return Path(__file__).resolve().parents[1]


def _build_backend_launch_env(
    adapter: Any,
    llm_selection: Any,
    *,
    cwd: Path | None = None,
    load_llm_env: Any | None = None,
    merge_env_with_os: Any | None = None,
) -> dict[str, str]:
    normalized_backend = normalize_backend_name(getattr(adapter, "name", ""))
    if normalized_backend == "codex":
        if load_llm_env is None or merge_env_with_os is None:
            raise RuntimeError("Codex launches require LLM env helpers.")
        env = merge_env_with_os(load_llm_env())
    else:
        env = dict(os.environ)
    adapter.prepare_env(env, llm_selection)
    if cwd is not None:
        env.setdefault(FIXER_DB_PATH_ENV, str(_resolve_fixer_db_path(cwd)))
    launch_env.clear_proxy_env(env)
    return env


def _bind_fixer_db_path_to_server_env(
    selected_servers: dict[str, dict[str, object]],
    *,
    db_path: Path,
) -> dict[str, dict[str, object]]:
    return fixer_wire_mcp._bind_fixer_db_path_to_server_env(selected_servers, db_path=db_path)


def _bind_locked_role_to_server_env(
    selected_servers: dict[str, dict[str, object]],
    *,
    role: str,
) -> dict[str, dict[str, object]]:
    return fixer_wire_mcp._bind_locked_role_to_server_env(selected_servers, role=role)


def _bind_netrunner_stateless_auth_to_server_env(
	selected_servers: dict[str, dict[str, object]],
	*,
	project_cwd: Path,
) -> dict[str, dict[str, object]]:
    return fixer_wire_mcp._bind_netrunner_stateless_auth_to_server_env(
        selected_servers,
        project_cwd=project_cwd,
    )


def _bind_launcher_telegram_env_to_server_env(
    selected_servers: dict[str, dict[str, object]],
) -> dict[str, dict[str, object]]:
    return fixer_wire_mcp._bind_launcher_telegram_env_to_server_env(selected_servers, environ=os.environ)


def _resolve_fixer_db_path(cwd: Path) -> Path:
    return fixer_wire_db._resolve_fixer_db_path(cwd, repo_root=_repo_root())


def _default_project_name(cwd: Path) -> str:
    return fixer_wire_db._default_project_name(cwd)


def _resolve_project_id(conn: sqlite3.Connection, cwd: Path) -> int:
    return fixer_wire_db._resolve_project_id(conn, cwd)


def _read_onboarding_project_name(
    cwd: Path,
    *,
    name_reader: Callable[[str], str] | None = None,
) -> str:
    return fixer_wire_db._read_onboarding_project_name(cwd, name_reader=name_reader)


def _ensure_project_registered(
    conn: sqlite3.Connection,
    cwd: Path,
    *,
    name_reader: Callable[[str], str] | None = None,
) -> int:
    return fixer_wire_db._ensure_project_registered(
        conn,
        cwd,
        name_reader=name_reader,
        resolve_project_id=_resolve_project_id,
        read_onboarding_project_name=_read_onboarding_project_name,
    )


def _resolve_unattached_fixer_cwd() -> Path:
    raw_cwd = os.environ.get(FIXER_UNATTACHED_CWD_ENV, "").strip()
    if raw_cwd:
        cwd = Path(raw_cwd).expanduser()
    else:
        base_cwd = Path.home() / ".codex" / "fixer_unattached"
        run_id = f"{time.strftime('%Y%m%d-%H%M%S')}-{os.getpid()}-{time.time_ns()}"
        cwd = base_cwd / "runs" / run_id
    if not cwd.is_absolute():
        cwd = Path.cwd() / cwd
    return cwd.resolve()


def _ensure_unattached_fixer_project(
    conn: sqlite3.Connection,
    *,
    scratch_cwd: Path | None = None,
) -> tuple[int, Path]:
    return fixer_wire_db._ensure_unattached_fixer_project(
        conn,
        scratch_cwd=scratch_cwd,
        resolve_unattached_fixer_cwd=_resolve_unattached_fixer_cwd,
        bootstrap_project_mcp_bindings=_bootstrap_project_mcp_bindings,
    )


def _assert_project_is_registered(cwd: Path) -> None:
    fixer_wire_db._assert_project_is_registered(
        cwd,
        resolve_fixer_db_path=_resolve_fixer_db_path,
        ensure_wire_schema=_ensure_wire_schema,
        ensure_project_registered=_ensure_project_registered,
    )


def _toml_literal(value: object) -> str:
    return fixer_wire_mcp._toml_literal(value)


def _load_forced_fixer_spec() -> dict[str, object]:
    return fixer_wire_mcp._load_forced_fixer_spec(
        repo_root=_repo_root,
        maybe_rebuild_fixer_mcp_binary=_maybe_rebuild_fixer_mcp_binary,
        environ=os.environ,
    )


def _with_forced_fixer_timeout_floor(spec: dict[str, object]) -> dict[str, object]:
    return fixer_wire_mcp._with_forced_fixer_timeout_floor(spec)


def _latest_mtime(paths: Sequence[Path]) -> float:
    return fixer_wire_mcp._latest_mtime(paths)


def _maybe_rebuild_fixer_mcp_binary(command_path: Path) -> None:
    fixer_wire_mcp._maybe_rebuild_fixer_mcp_binary(
        command_path,
        repo_root=_repo_root,
        build_checked=_FIXER_MCP_BUILD_CHECKED,
        environ=os.environ,
        subprocess_run=subprocess.run,
        stderr=sys.stderr,
    )


def _build_forced_fixer_override_args() -> list[str]:
    return fixer_wire_mcp._build_forced_fixer_override_args(_load_forced_fixer_spec())


def _inject_forced_fixer_server(available_servers: dict[str, dict[str, object]]) -> dict[str, dict[str, object]]:
    return fixer_wire_mcp._inject_forced_fixer_server(
        available_servers,
        forced_fixer_spec=_load_forced_fixer_spec(),
    )


def _describe_forced_fixer_resolution() -> str:
    return fixer_wire_mcp._describe_forced_fixer_resolution(repo_root=_repo_root, environ=os.environ)


def _ensure_forced_fixer_server_resolved(servers: dict[str, dict[str, object]]) -> None:
    fixer_wire_mcp._ensure_forced_fixer_server_resolved(
        servers,
        repo_root=_repo_root,
        environ=os.environ,
    )


def _parse_simple_env_file(path: Path, keys: Sequence[str]) -> dict[str, str]:
    return fixer_wire_mcp._parse_simple_env_file(path, keys)


def _figma_console_env_file_candidates(cwd: Path) -> list[Path]:
    return fixer_wire_mcp._figma_console_env_file_candidates(cwd, environ=os.environ)


def _load_figma_console_credentials(cwd: Path) -> dict[str, str]:
    return fixer_wire_mcp._load_figma_console_credentials(cwd, environ=os.environ)


def _inject_figma_console_server(
    available_servers: dict[str, dict[str, object]],
    cwd: Path,
) -> dict[str, dict[str, object]]:
    return fixer_wire_mcp._inject_figma_console_server(available_servers, cwd, environ=os.environ)


def _inject_research_query_server(
    available_servers: dict[str, dict[str, object]],
    cwd: Path,
) -> dict[str, dict[str, object]]:
    return fixer_wire_mcp._inject_research_query_server(available_servers, cwd, which=shutil.which)


def _registry_metadata_with_fallback(
    name: str,
    metadata: RegistryMcpMetadata | None,
) -> RegistryMcpMetadata | None:
    return fixer_wire_db._registry_metadata_with_fallback(name, metadata)


def _load_session_rows(conn: sqlite3.Connection, project_id: int) -> list[SessionRow]:
    return fixer_wire_db._load_session_rows(conn, project_id)


def _strip_md_prefix(text: str) -> str:
    return fixer_wire_selectors._strip_md_prefix(text)


def _session_title(task_description: str, *, limit: int = 110) -> str:
    return fixer_wire_selectors._session_title(task_description, limit=limit)


def _load_registry_mcp_names(conn: sqlite3.Connection) -> list[str]:
    return fixer_wire_db._load_registry_mcp_names(
        conn,
        load_registry_mcp_metadata=_load_registry_mcp_metadata,
    )


def _load_registry_mcp_metadata(conn: sqlite3.Connection) -> dict[str, RegistryMcpMetadata]:
    return fixer_wire_db._load_registry_mcp_metadata(conn)


def _load_assigned_mcp_names(conn: sqlite3.Connection, session_id: int) -> list[str]:
    return fixer_wire_db._load_assigned_mcp_names(conn, session_id)


def _bootstrap_project_mcp_bindings(conn: sqlite3.Connection, project_id: int) -> None:
    fixer_wire_db._bootstrap_project_mcp_bindings(conn, project_id)


def _load_project_allowed_mcp_names(conn: sqlite3.Connection, project_id: int) -> list[str]:
    return fixer_wire_db._load_project_allowed_mcp_names(
        conn,
        project_id,
        bootstrap_project_mcp_bindings=_bootstrap_project_mcp_bindings,
        load_registry_mcp_names=_load_registry_mcp_names,
    )


def _allowed_runtime_mcp_names(
    allowed_names: Sequence[str],
    available_servers: dict[str, dict[str, object]],
) -> list[str]:
    available_names = set(available_servers.keys())
    allowed_runtime = set(allowed_names).intersection(available_names)
    allowed_runtime -= HIDDEN_MCP_SERVERS
    allowed_runtime -= NONDEFAULT_ROLE_AUTO_MCP_SERVERS
    return sorted(allowed_runtime)


def _assigned_preselected_mcp_names(
    assigned_names: Sequence[str],
    allowed_runtime_names: Sequence[str],
) -> list[str]:
    allowed_runtime_set = set(allowed_runtime_names)
    preselected = set(assigned_names).intersection(allowed_runtime_set)
    preselected -= HIDDEN_MCP_SERVERS
    return sorted(preselected)


def _assigned_allowed_mcp_names(
    assigned_names: Sequence[str],
    allowed_names: Sequence[str],
) -> list[str]:
    allowed_set = set(allowed_names)
    assigned_allowed = set(assigned_names).intersection(allowed_set)
    assigned_allowed -= HIDDEN_MCP_SERVERS
    return sorted(assigned_allowed)


def _eligible_session_mcp_names(
    assigned_names: Sequence[str],
    allowed_names: Sequence[str],
    available_servers: dict[str, dict[str, object]],
) -> list[str]:
    # Backward-compatible alias used in tests/older call sites.
    available_names = set(available_servers.keys())
    eligible = set(assigned_names).intersection(allowed_names).intersection(available_names)
    eligible -= HIDDEN_MCP_SERVERS
    return sorted(eligible)


def _load_project_web_mcp_servers(cwd: Path) -> dict[str, dict[str, object]]:
    return fixer_wire_mcp._load_project_web_mcp_servers(cwd, stderr=sys.stderr)


def _overlay_project_mcp_servers(
    base: dict[str, dict[str, object]],
    overrides: dict[str, dict[str, object]],
) -> dict[str, dict[str, object]]:
    return fixer_wire_mcp._overlay_project_mcp_servers(base, overrides)


def _select_role_interactive(Option: Any, single_select_items: Any) -> str:
    return fixer_wire_selectors._select_role_interactive(Option, single_select_items)


def _prompt_scaffold_value(prompt: str, *, default: str | None = None) -> str:
    return fixer_wire_selectors._prompt_scaffold_value(prompt, default=default)


def _select_scaffold_execution_mode_interactive(Option: Any, single_select_items: Any) -> bool:
    return fixer_wire_selectors._select_scaffold_execution_mode_interactive(Option, single_select_items)


def _launch_scaffold_interactive(Option: Any, single_select_items: Any) -> int:
    project_name = _prompt_scaffold_value("MVP project name or slug")
    target_dir = _prompt_scaffold_value("Target parent directory", default=str(Path.cwd()))
    dry_run = _select_scaffold_execution_mode_interactive(Option, single_select_items)
    return run_scaffold_cli(project_name, target_dir=target_dir, dry_run=dry_run)


def _select_fixer_launch_action_interactive(Option: Any, single_select_items: Any) -> str:
    return fixer_wire_selectors._select_fixer_launch_action_interactive(Option, single_select_items)


def _select_overseer_launch_action_interactive(Option: Any, single_select_items: Any) -> str:
    return fixer_wire_selectors._select_overseer_launch_action_interactive(Option, single_select_items)


def _select_manual_netrunner_kind_interactive(Option: Any, single_select_items: Any) -> str:
    return fixer_wire_selectors._select_manual_netrunner_kind_interactive(Option, single_select_items)


def _select_session_interactive(
    session_rows: Sequence[SessionRow],
    Option: Any,
    single_select_items: Any,
) -> SessionRow:
    return fixer_wire_selectors._select_session_interactive(
        session_rows,
        Option,
        single_select_items,
        session_title=_session_title,
    )


def _select_mcp_interactive(
    registry_names: Sequence[str],
    assigned_names: Sequence[str],
    registry_meta: dict[str, RegistryMcpMetadata],
    available_servers: dict[str, dict[str, object]],
    Option: Any,
    multi_select_items: Any,
    *,
    show_all_registry_names: bool = False,
) -> list[str]:
    return fixer_wire_selectors._select_mcp_interactive(
        registry_names,
        assigned_names,
        registry_meta,
        available_servers,
        Option,
        multi_select_items,
        show_all_registry_names=show_all_registry_names,
        registry_metadata_with_fallback=_registry_metadata_with_fallback,
    )


def _sync_registry_names(conn: sqlite3.Connection, names: Sequence[str]) -> None:
    fixer_wire_db._sync_registry_names(conn, names, normalize_names=_normalize_names)


def _persist_session_mcp_names(conn: sqlite3.Connection, session_id: int, names: Sequence[str]) -> None:
    fixer_wire_db._persist_session_mcp_names(
        conn,
        session_id,
        names,
        normalize_names=_normalize_names,
        sync_registry_names=_sync_registry_names,
    )


def _backend_descriptor(backend_name: str) -> Any:
    return fixer_wire_db._backend_descriptor(backend_name)


def _normalize_backend_model(descriptor: Any, model: str | None) -> str:
    return fixer_wire_db._normalize_backend_model(descriptor, model)


def _normalize_backend_reasoning(descriptor: Any, reasoning: str | None) -> str:
    return fixer_wire_db._normalize_backend_reasoning(descriptor, reasoning)


def _load_session_external_id(conn: sqlite3.Connection, session_id: int, backend: str) -> str:
    return fixer_wire_db._load_session_external_id(conn, session_id, backend)


def _save_session_external_id(conn: sqlite3.Connection, session_id: int, backend: str, external_session_id: str) -> None:
    fixer_wire_db._save_session_external_id(conn, session_id, backend, external_session_id)


def _save_session_codex_id(conn: sqlite3.Connection, session_id: int, codex_session_id: str) -> None:
    _save_session_external_id(conn, session_id, "codex", codex_session_id)


def _persist_session_launch_selection(
    conn: sqlite3.Connection,
    session_row: SessionRow,
    selection: SessionLaunchSelection,
) -> SessionLaunchSelection:
    return fixer_wire_db._persist_session_launch_selection(
        conn,
        session_row,
        selection,
        backend_descriptor=_backend_descriptor,
        normalize_backend_model=_normalize_backend_model,
        normalize_backend_reasoning=_normalize_backend_reasoning,
    )


def _latest_codex_session_id_for_cwd(cwd: Path) -> str | None:
    return fixer_wire_resume.latest_codex_session_id_for_cwd(cwd)


def _prompt_resume_session_id(session_id: int, backend: str) -> str | None:
    return fixer_wire_resume.prompt_resume_session_id(
        session_id,
        backend,
        backend_descriptor=_backend_descriptor,
    )


def _netrunner_session_marker(session_id: int) -> str:
    return fixer_wire_resume.netrunner_session_marker(session_id)


def _first_marker_line(
    log_path: Path,
    marker: str,
    *,
    max_lines: int = 240,
) -> int | None:
    return fixer_wire_resume.first_marker_line(log_path, marker, max_lines=max_lines)


def _first_any_marker_line(
    log_path: Path,
    markers: Sequence[str],
    *,
    max_lines: int = 240,
) -> int | None:
    return fixer_wire_resume.first_any_marker_line(log_path, markers, max_lines=max_lines)


def _session_log_has_markers(log_path: Path, markers: Sequence[str], *, max_lines: int = 240) -> bool:
    return fixer_wire_resume.session_log_has_markers(log_path, markers, max_lines=max_lines)


def _session_log_has_any_marker(log_path: Path, markers: Sequence[str], *, max_lines: int = 240) -> bool:
    return fixer_wire_resume.session_log_has_any_marker(log_path, markers, max_lines=max_lines)


def _session_log_has_fixer_marker(log_path: Path, *, max_lines: int = 240) -> bool:
    return fixer_wire_resume.session_log_has_fixer_marker(
        log_path,
        fixer_skill_markers=FIXER_SKILL_MARKERS,
        max_lines=max_lines,
    )


def _session_log_is_fixer_session(log_path: Path, *, max_lines: int = 240) -> bool:
    return fixer_wire_resume.session_log_is_fixer_session(
        log_path,
        fixer_skill_markers=FIXER_SKILL_MARKERS,
        netrunner_skill_markers=NETRUNNER_SKILL_MARKERS,
        overseer_skill_markers=OVERSEER_SKILL_MARKERS,
        max_lines=max_lines,
    )


def _session_log_is_overseer_session(log_path: Path, *, max_lines: int = 240) -> bool:
    return fixer_wire_resume.session_log_is_overseer_session(
        log_path,
        fixer_skill_markers=FIXER_SKILL_MARKERS,
        netrunner_skill_markers=NETRUNNER_SKILL_MARKERS,
        overseer_skill_markers=OVERSEER_SKILL_MARKERS,
        max_lines=max_lines,
    )


def _session_log_has_netrunner_marker(
    log_path: Path,
    session_id: int | None = None,
    *,
    max_lines: int = 240,
) -> bool:
    return fixer_wire_resume.session_log_has_netrunner_marker(
        log_path,
        session_id,
        netrunner_skill_markers=NETRUNNER_SKILL_MARKERS,
        max_lines=max_lines,
    )


def _load_cwd_session_summaries(cwd: Path, *, limit: int, minimum_scan_limit: int = 80) -> tuple[Any, list[Any]]:
    return fixer_wire_resume.load_cwd_session_summaries(cwd, limit=limit, minimum_scan_limit=minimum_scan_limit)


def _load_fixer_resume_summaries(cwd: Path, *, limit: int = 40) -> list[Any]:
    return fixer_wire_resume.load_fixer_resume_summaries(
        cwd,
        limit=limit,
        load_cwd_summaries=_load_cwd_session_summaries,
        load_alias_session_ids=_load_fixer_resume_alias_session_ids,
        session_is_fixer=_session_log_is_fixer_session,
    )


def _load_overseer_resume_summaries(cwd: Path, *, limit: int = 40) -> list[Any]:
    return fixer_wire_resume.load_overseer_resume_summaries(
        cwd,
        limit=limit,
        load_cwd_summaries=_load_cwd_session_summaries,
        session_is_overseer=_session_log_is_overseer_session,
    )


def _load_fixer_resume_alias_session_ids(cwd: Path) -> set[str]:
    return fixer_wire_resume.load_fixer_resume_alias_session_ids(
        cwd,
        resolve_fixer_db_path=_resolve_fixer_db_path,
        ensure_wire_schema=_ensure_wire_schema,
        resolve_project_id=_resolve_project_id,
    )


def _load_netrunner_resume_summaries(cwd: Path, session_id: int, *, limit: int = 20) -> list[Any]:
    return fixer_wire_resume.load_netrunner_resume_summaries(
        cwd,
        session_id,
        limit=limit,
        load_cwd_summaries=_load_cwd_session_summaries,
        log_has_netrunner_marker=_session_log_has_netrunner_marker,
    )


def _select_fixer_resume_session_interactive(
    summaries: Sequence[Any],
    Option: Any,
    single_select_items: Any,
) -> str:
    return fixer_wire_selectors._select_fixer_resume_session_interactive(summaries, Option, single_select_items)


def _select_overseer_resume_session_interactive(
    summaries: Sequence[Any],
    Option: Any,
    single_select_items: Any,
) -> str:
    return fixer_wire_selectors._select_overseer_resume_session_interactive(summaries, Option, single_select_items)


def _resolve_latest_fixer_resume_session_id(cwd: Path) -> str:
    return fixer_wire_resume.resolve_latest_fixer_resume_session_id(
        cwd,
        load_fixer_resume_summaries=_load_fixer_resume_summaries,
    )


def _select_netrunner_resume_session_interactive(
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


def _resolve_netrunner_resume_session_id(
    cwd: Path,
    selected_session: SessionRow,
    Option: Any,
    single_select_items: Any,
) -> str:
    return fixer_wire_resume.resolve_netrunner_resume_session_id(
        cwd,
        selected_session,
        Option,
        single_select_items,
        prompt_resume_session_id=_prompt_resume_session_id,
        load_netrunner_resume_summaries=_load_netrunner_resume_summaries,
        select_netrunner_resume_session_interactive=_select_netrunner_resume_session_interactive,
    )


def _latest_matching_netrunner_codex_session_id(cwd: Path, session_id: int) -> str | None:
    return fixer_wire_resume.latest_matching_netrunner_codex_session_id(
        cwd,
        session_id,
        load_netrunner_resume_summaries=_load_netrunner_resume_summaries,
    )


def _load_available_servers(cwd: Path, *, backend: str = DEFAULT_BACKEND) -> tuple[dict[str, dict[str, object]], dict[str, str], Any, Any]:
    from client_wires.codex_compat.config import (
        ConfigError,
        attach_preprompts_from_command_paths,
        discover_project_mcp_servers,
        discover_self_mcp_servers,
        fetch_mcp_servers,
        get_config_path,
        load_config,
        merge_mcp_servers,
    )
    from client_wires.codex_compat.llm import (
        CODEX_CLI_ADAPTER,
        CONFIG_ENV_VARS,
    )
    from client_wires.codex_compat.runtime import (
        _ensure_sqlite_scaffold,
    )

    try:
        config = load_config(get_config_path())
        available_servers = fetch_mcp_servers(config)
        local_servers, missing_local = discover_self_mcp_servers(cwd)
        project_servers = discover_project_mcp_servers(cwd)
        web_mcp_servers = _load_project_web_mcp_servers(cwd)
        if missing_local:
            for path in missing_local:
                try:
                    rel = path.relative_to(cwd)
                except ValueError:
                    rel = path
                print(
                    f"[warning] self_mcp_servers entry without mcp.json: {rel} (skipped)",
                    file=sys.stderr,
                )
        available_servers = merge_mcp_servers(available_servers, local_servers)
        available_servers = merge_mcp_servers(available_servers, project_servers)
        if web_mcp_servers:
            available_servers = _overlay_project_mcp_servers(available_servers, web_mcp_servers)
        available_servers = _inject_research_query_server(available_servers, cwd)
        available_servers = _inject_figma_console_server(available_servers, cwd)
        available_servers = _inject_forced_fixer_server(available_servers)
        attach_preprompts_from_command_paths(available_servers)
    except ConfigError as err:
        raise RuntimeError(str(err)) from err

    return available_servers, CONFIG_ENV_VARS, get_backend_adapter(backend, codex_adapter=CODEX_CLI_ADAPTER), _ensure_sqlite_scaffold


def _select_backend_interactive(
    preferred_backend: str,
    Option: Any,
    single_select_items: Any,
) -> str:
    return fixer_wire_selectors._select_backend_interactive(preferred_backend, Option, single_select_items)


def _select_model_interactive(
    backend: str,
    preferred_model: str,
    Option: Any,
    single_select_items: Any,
) -> str:
    return fixer_wire_selectors._select_model_interactive(
        backend,
        preferred_model,
        Option,
        single_select_items,
        backend_descriptor=_backend_descriptor,
    )


def _select_reasoning_interactive(
    backend: str,
    preferred_reasoning: str,
    Option: Any,
    single_select_items: Any,
) -> str:
    return fixer_wire_selectors._select_reasoning_interactive(
        backend,
        preferred_reasoning,
        Option,
        single_select_items,
        backend_descriptor=_backend_descriptor,
    )


def _build_netrunner_prompt(
    session_id: int,
    mcp_names: Sequence[str],
    mcp_how_to: dict[str, str],
    *,
    netrunner_kind: str = NETRUNNER_KIND_MANUAL,
) -> str:
    return fixer_wire_prompts._build_netrunner_prompt(
        session_id,
        mcp_names,
        mcp_how_to,
        netrunner_kind=netrunner_kind,
        default_how_to=_build_default_how_to,
        standard_web_stack_guidance_block=_build_standard_web_stack_guidance_block,
    )


def _build_droid_netrunner_prompt(
    session_id: int,
    mcp_names: Sequence[str],
    *,
    netrunner_kind: str = NETRUNNER_KIND_MANUAL,
) -> str:
    return fixer_wire_prompts._build_droid_netrunner_prompt(
        session_id,
        mcp_names,
        netrunner_kind=netrunner_kind,
    )


def _build_default_how_to(server_name: str) -> str:
    return fixer_wire_prompts._build_default_how_to(server_name)


def _build_standard_web_stack_guidance_block(mcp_names: Sequence[str]) -> str:
    return fixer_wire_prompts._build_standard_web_stack_guidance_block(mcp_names)


def _build_droid_mcp_tool_guidance_block(mcp_names: Sequence[str]) -> str:
    return fixer_wire_prompts._build_droid_mcp_tool_guidance_block(
        mcp_names,
        normalize_names=_normalize_names,
    )


def _append_droid_mcp_tool_guidance(
    prompt: str,
    *,
    backend: str,
    mcp_names: Sequence[str],
) -> str:
    return fixer_wire_prompts._append_droid_mcp_tool_guidance(
        prompt,
        backend=backend,
        mcp_names=mcp_names,
        backend_normalizer=normalize_backend_name,
        droid_mcp_tool_guidance_block=_build_droid_mcp_tool_guidance_block,
    )


def _build_mcp_how_to_map(
    mcp_names: Sequence[str],
    registry_meta: dict[str, RegistryMcpMetadata],
) -> dict[str, str]:
    return fixer_wire_prompts._build_mcp_how_to_map(
        mcp_names,
        registry_meta,
        registry_metadata_with_fallback=_registry_metadata_with_fallback,
        default_how_to=_build_default_how_to,
    )


def _build_fixer_prompt() -> str:
    return fixer_wire_prompts._build_fixer_prompt()


def _build_unattached_fixer_prompt(scratch_cwd: Path) -> str:
    return fixer_wire_prompts._build_unattached_fixer_prompt(scratch_cwd)


def _build_overseer_prompt() -> str:
    return fixer_wire_prompts._build_overseer_prompt()


def _launch_selection_callbacks() -> fixer_wire_launch_support.LaunchSelectionCallbacks:
    return fixer_wire_launch_support.LaunchSelectionCallbacks(
        select_backend_interactive=_select_backend_interactive,
        select_model_interactive=_select_model_interactive,
        select_reasoning_interactive=_select_reasoning_interactive,
        backend_descriptor=_backend_descriptor,
        normalize_backend_model=_normalize_backend_model,
        normalize_backend_reasoning=_normalize_backend_reasoning,
    )


def _ensure_passthrough_dangerous_sandbox(passthrough_args: Sequence[str]) -> list[str]:
    return fixer_wire_launch_support._ensure_passthrough_dangerous_sandbox(passthrough_args)


def _is_codex_adapter(adapter: Any) -> bool:
    return fixer_wire_launch_support._is_codex_adapter(adapter)


def _maybe_configure_playwright_runtime_mode(
    adapter: Any,
    selected_servers: dict[str, dict[str, object]],
    available_servers: dict[str, dict[str, object]],
    *,
    interactive: bool,
) -> str | None:
    return fixer_wire_launch_support._maybe_configure_playwright_runtime_mode(
        adapter,
        selected_servers,
        available_servers,
        interactive=interactive,
    )


def _is_computer_use_config_override(value: str) -> bool:
    return fixer_wire_launch_support._is_computer_use_config_override(value)


def _strip_computer_use_overrides(args: Sequence[str]) -> list[str]:
    return fixer_wire_launch_support._strip_computer_use_overrides(args)


def _append_codex_apps_gate(codex_args: list[str], adapter: Any, *, allow_computer_use: bool) -> list[str]:
    return fixer_wire_launch_support._append_codex_apps_gate(
        codex_args,
        adapter,
        allow_computer_use=allow_computer_use,
    )


def _prefer_fixed_model_for_role_presets(codex_main: Any) -> None:
    fixer_wire_launch_support._prefer_fixed_model_for_role_presets(
        codex_main,
        fixer_wire_model=FIXER_WIRE_MODEL,
        fixer_wire_reasoning_effort=FIXER_WIRE_REASONING_EFFORT,
    )


def _resolve_netrunner_launch_selection(
    selected_session: SessionRow,
    *,
    preset_backend: str | None,
    preset_model: str | None,
    preset_reasoning: str | None,
    dry_run: bool,
    Option: Any,
    single_select_items: Any,
) -> SessionLaunchSelection:
    return fixer_wire_launch_support._resolve_netrunner_launch_selection(
        selected_session,
        preset_backend=preset_backend,
        preset_model=preset_model,
        preset_reasoning=preset_reasoning,
        dry_run=dry_run,
        Option=Option,
        single_select_items=single_select_items,
        callbacks=_launch_selection_callbacks(),
    )


def _select_fresh_launch_selection(
    *,
    preset_backend: str | None,
    preset_model: str | None,
    preset_reasoning: str | None,
    Option: Any,
    single_select_items: Any,
) -> SessionLaunchSelection:
    return fixer_wire_launch_support._select_fresh_launch_selection(
        preset_backend=preset_backend,
        preset_model=preset_model,
        preset_reasoning=preset_reasoning,
        Option=Option,
        single_select_items=single_select_items,
        callbacks=_launch_selection_callbacks(),
    )


def _role_launch_callbacks() -> fixer_wire_role_launch.RoleLaunchCallbacks:
    return fixer_wire_role_launch.RoleLaunchCallbacks(
        normalize_names=_normalize_names,
        select_fresh_launch_selection=_select_fresh_launch_selection,
        load_available_servers=_load_available_servers,
        resolve_fixer_db_path=_resolve_fixer_db_path,
        bind_fixer_db_path_to_server_env=_bind_fixer_db_path_to_server_env,
        bind_locked_role_to_server_env=_bind_locked_role_to_server_env,
        bind_launcher_telegram_env_to_server_env=_bind_launcher_telegram_env_to_server_env,
        append_droid_mcp_tool_guidance=_append_droid_mcp_tool_guidance,
        append_codex_apps_gate=_append_codex_apps_gate,
        build_backend_launch_env=_build_backend_launch_env,
        assert_project_is_registered=_assert_project_is_registered,
        select_fixer_launch_action_interactive=_select_fixer_launch_action_interactive,
        resolve_latest_fixer_resume_session_id=_resolve_latest_fixer_resume_session_id,
        load_fixer_resume_summaries=_load_fixer_resume_summaries,
        select_fixer_resume_session_interactive=_select_fixer_resume_session_interactive,
        build_fixer_prompt=_build_fixer_prompt,
        launch_fresh_role_session=_launch_fresh_role_session,
        resolve_unattached_fixer_cwd=_resolve_unattached_fixer_cwd,
        ensure_wire_schema=_ensure_wire_schema,
        ensure_unattached_fixer_project=_ensure_unattached_fixer_project,
        build_unattached_fixer_prompt=_build_unattached_fixer_prompt,
        select_overseer_launch_action_interactive=_select_overseer_launch_action_interactive,
        select_role_preset_server_names=_select_role_preset_server_names,
        load_overseer_resume_summaries=_load_overseer_resume_summaries,
        select_overseer_resume_session_interactive=_select_overseer_resume_session_interactive,
        build_overseer_prompt=_build_overseer_prompt,
        forced_mcp_server=FORCED_MCP_SERVER,
        figma_console_mcp_name=FIGMA_CONSOLE_MCP_NAME,
        fixer_launch_new=FIXER_LAUNCH_NEW,
        fixer_launch_resume=FIXER_LAUNCH_RESUME,
        overseer_launch_new=OVERSEER_LAUNCH_NEW,
        fixer_wire_model=FIXER_WIRE_MODEL,
        fixer_wire_reasoning_effort=FIXER_WIRE_REASONING_EFFORT,
    )


def _select_role_preset_server_names(
    available_servers: dict[str, dict[str, object]],
    *,
    cwd: Path,
    role: str | None = None,
) -> list[str]:
    return fixer_wire_role_launch.select_role_preset_server_names(
        available_servers,
        cwd=cwd,
        role=role,
        callbacks=_role_launch_callbacks(),
    )


def _launch_fresh_role_session(
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
) -> int:
    return fixer_wire_role_launch.launch_fresh_role_session(
        role,
        prompt,
        passthrough_args,
        launch_cwd=launch_cwd,
        selected_mcp_names=selected_mcp_names,
        dry_run=dry_run,
        preset_backend=preset_backend,
        preset_model=preset_model,
        preset_reasoning=preset_reasoning,
        dangerous_sandbox=dangerous_sandbox,
        Option=Option,
        single_select_items=single_select_items,
        callbacks=_role_launch_callbacks(),
    )


def _launch_fixer(
    passthrough_args: Sequence[str],
    *,
    launch_cwd: Path | None = None,
    dry_run: bool,
    preset_resume_latest: bool,
    preset_resume_session_id: str | None,
    Option: Any,
    single_select_items: Any,
) -> int:
    return fixer_wire_role_launch.launch_fixer(
        passthrough_args,
        launch_cwd=launch_cwd,
        dry_run=dry_run,
        preset_resume_latest=preset_resume_latest,
        preset_resume_session_id=preset_resume_session_id,
        Option=Option,
        single_select_items=single_select_items,
        callbacks=_role_launch_callbacks(),
    )


def _launch_unattached_fixer(
    passthrough_args: Sequence[str],
    *,
    dry_run: bool,
    Option: Any,
    single_select_items: Any,
) -> int:
    return fixer_wire_role_launch.launch_unattached_fixer(
        passthrough_args,
        dry_run=dry_run,
        Option=Option,
        single_select_items=single_select_items,
        callbacks=_role_launch_callbacks(),
    )


def _netrunner_launch_callbacks() -> fixer_wire_netrunner_launch.NetrunnerLaunchCallbacks:
    return fixer_wire_netrunner_launch.NetrunnerLaunchCallbacks(
        resolve_fixer_db_path=_resolve_fixer_db_path,
        ensure_wire_schema=_ensure_wire_schema,
        ensure_project_registered=_ensure_project_registered,
        load_session_rows=_load_session_rows,
        select_session_interactive=_select_session_interactive,
        select_manual_netrunner_kind_interactive=_select_manual_netrunner_kind_interactive,
        resolve_netrunner_launch_selection=_resolve_netrunner_launch_selection,
        load_available_servers=_load_available_servers,
        sync_registry_names=_sync_registry_names,
        load_registry_mcp_metadata=_load_registry_mcp_metadata,
        load_assigned_mcp_names=_load_assigned_mcp_names,
        load_project_allowed_mcp_names=_load_project_allowed_mcp_names,
        allowed_runtime_mcp_names=_allowed_runtime_mcp_names,
        assigned_allowed_mcp_names=_assigned_allowed_mcp_names,
        assigned_preselected_mcp_names=_assigned_preselected_mcp_names,
        select_mcp_interactive=_select_mcp_interactive,
        normalize_names=_normalize_names,
        persist_session_mcp_names=_persist_session_mcp_names,
        persist_session_launch_selection=_persist_session_launch_selection,
        load_session_external_id=_load_session_external_id,
        save_session_external_id=_save_session_external_id,
        backend_descriptor=_backend_descriptor,
        resolve_netrunner_resume_session_id=_resolve_netrunner_resume_session_id,
        maybe_configure_playwright_runtime_mode=_maybe_configure_playwright_runtime_mode,
        bind_fixer_db_path_to_server_env=_bind_fixer_db_path_to_server_env,
        bind_netrunner_stateless_auth_to_server_env=_bind_netrunner_stateless_auth_to_server_env,
        bind_locked_role_to_server_env=_bind_locked_role_to_server_env,
        bind_launcher_telegram_env_to_server_env=_bind_launcher_telegram_env_to_server_env,
        build_droid_netrunner_prompt=_build_droid_netrunner_prompt,
        build_netrunner_prompt=_build_netrunner_prompt,
        build_mcp_how_to_map=_build_mcp_how_to_map,
        build_backend_launch_env=_build_backend_launch_env,
        append_codex_apps_gate=_append_codex_apps_gate,
        latest_matching_netrunner_codex_session_id=_latest_matching_netrunner_codex_session_id,
        latest_codex_session_id_for_cwd=_latest_codex_session_id_for_cwd,
        prompt_resume_session_id=_prompt_resume_session_id,
        netrunner_kind_manual=NETRUNNER_KIND_MANUAL,
        computer_use_mcp_name=COMPUTER_USE_MCP_NAME,
        nondefault_role_auto_mcp_servers=NONDEFAULT_ROLE_AUTO_MCP_SERVERS,
        forced_mcp_server=FORCED_MCP_SERVER,
    )


def _launch_netrunner(
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
) -> int:
    return fixer_wire_netrunner_launch.launch_netrunner(
        passthrough_args,
        preset_session_id=preset_session_id,
        preset_backend=preset_backend,
        preset_model=preset_model,
        preset_reasoning=preset_reasoning,
        preset_mcp_names=preset_mcp_names,
        dry_run=dry_run,
        Option=Option,
        single_select_items=single_select_items,
        multi_select_items=multi_select_items,
        callbacks=_netrunner_launch_callbacks(),
    )


def _launch_overseer(
	passthrough_args: Sequence[str],
	*,
	dry_run: bool,
	Option: Any,
	single_select_items: Any,
) -> int:
    return fixer_wire_role_launch.launch_overseer(
        passthrough_args,
        dry_run=dry_run,
        Option=Option,
        single_select_items=single_select_items,
        callbacks=_role_launch_callbacks(),
    )


def main(argv: Sequence[str] | None = None) -> int:
    raw_args = list(sys.argv[1:] if argv is None else argv)
    wire_args, passthrough_args = _parse_wire_args(raw_args)

    if wire_args.scaffold_mvp:
        if wire_args.role:
            print("[fixer-wire] `--scaffold-mvp` cannot be combined with `--role`.", file=sys.stderr)
            return 2
        if passthrough_args:
            extra = " ".join(passthrough_args)
            print(
                f"[fixer-wire] Scaffold mode does not accept passthrough Codex args. Unexpected: {extra}",
                file=sys.stderr,
            )
            return 2
        return run_scaffold_cli(
            wire_args.scaffold_mvp,
            target_dir=wire_args.scaffold_target_dir,
            dry_run=wire_args.dry_run,
        )

    mcp_root = bootstrap_codex_pro_import_path()

    if wire_args.wire_info:
        for line in wire_info_lines(mcp_root):
            print(line)
        if not passthrough_args:
            return 0

    from client_wires.codex_compat.ui import Option, multi_select_items, single_select_items

    role = wire_args.role or _select_role_interactive(Option, single_select_items)
    if role == SCAFFOLD_MVP_ACTION:
        try:
            return _launch_scaffold_interactive(Option, single_select_items)
        except RuntimeError as exc:
            print(f"[fixer-wire] {exc}", file=sys.stderr)
            return 2

    if role == UNATTACHED_FIXER_ACTION:
        try:
            return _launch_unattached_fixer(
                passthrough_args,
                dry_run=wire_args.dry_run,
                Option=Option,
                single_select_items=single_select_items,
            )
        except RuntimeError as exc:
            print(f"[fixer-wire] {exc}", file=sys.stderr)
            return 2

    if role == "netrunner":
        try:
            return _launch_netrunner(
                passthrough_args,
                preset_session_id=wire_args.netrunner_session_id,
                preset_backend=wire_args.netrunner_backend,
                preset_model=wire_args.netrunner_model,
                preset_reasoning=wire_args.netrunner_reasoning,
                preset_mcp_names=wire_args.netrunner_mcp,
                dry_run=wire_args.dry_run,
                Option=Option,
                single_select_items=single_select_items,
                multi_select_items=multi_select_items,
            )
        except RuntimeError as exc:
            print(f"[fixer-wire] {exc}", file=sys.stderr)
            return 2

    if role == "fixer":
        try:
            if wire_args.fixer_resume_latest and wire_args.fixer_session_id:
                raise RuntimeError("Use only one of --fixer-resume-latest or --fixer-session-id.")
            return _launch_fixer(
                passthrough_args,
                dry_run=wire_args.dry_run,
                preset_resume_latest=wire_args.fixer_resume_latest,
                preset_resume_session_id=wire_args.fixer_session_id,
                Option=Option,
                single_select_items=single_select_items,
            )
        except RuntimeError as exc:
            print(f"[fixer-wire] {exc}", file=sys.stderr)
            return 2

    if role == "overseer":
        try:
            return _launch_overseer(
                passthrough_args,
                dry_run=wire_args.dry_run,
                Option=Option,
                single_select_items=single_select_items,
            )
        except RuntimeError as exc:
            print(f"[fixer-wire] {exc}", file=sys.stderr)
            return 2

    print(f"[fixer-wire] unsupported role: {role}", file=sys.stderr)
    return 2


if __name__ == "__main__":
    raise SystemExit(main())
