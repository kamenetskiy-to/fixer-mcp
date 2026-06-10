"""Wave worker planning helpers for autonomous Netrunner launches."""

from __future__ import annotations

import json
import re
import time
from dataclasses import dataclass
from pathlib import Path
from types import SimpleNamespace
from typing import Any, Callable

from client_wires import fixer_wire

WAVE_BRANCH_PATTERN = re.compile(r"^fixer/wave-[1-9][0-9]*/session-[1-9][0-9]*$")


@dataclass(frozen=True)
class _WaveNetrunnerLaunchPlan:
    command: list[str]
    env: dict[str, str]
    popen_cwd: Path
    prompt: str
    selected_mcp_names: list[str]
    selected_servers: dict[str, dict[str, object]]
    selected_config_paths: dict[str, Path]
    metadata: dict[str, int | str]


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
    metadata_path.parent.mkdir(parents=True, exist_ok=True)
    payload = {
        "worker_pid": int(worker_pid),
        "headless_log_path": str(headless_log_path),
        "backend": backend,
        "session_id": int(session_id),
        "written_at_epoch": int(time.time()),
    }
    if wave_id is not None:
        payload["wave_id"] = int(wave_id)
    if wave_worker_id is not None:
        payload["wave_worker_id"] = int(wave_worker_id)
    if project_cwd is not None:
        payload["project_cwd"] = str(project_cwd)
    if worker_cwd is not None:
        payload["worker_cwd"] = str(worker_cwd)
    if branch_name:
        payload["branch_name"] = branch_name
    metadata_path.write_text(json.dumps(payload, indent=2) + "\n", encoding="utf-8")


def _positive_wave_int(name: str, value: int) -> int:
    if isinstance(value, bool):
        raise RuntimeError(f"{name} must be a positive integer.")
    try:
        normalized = int(value)
    except (TypeError, ValueError) as exc:
        raise RuntimeError(f"{name} must be a positive integer.") from exc
    if normalized <= 0:
        raise RuntimeError(f"{name} must be a positive integer.")
    return normalized


def _wave_branch_name(wave_id: int, local_session_id: int) -> str:
    normalized_wave_id = _positive_wave_int("wave_id", wave_id)
    normalized_session_id = _positive_wave_int("local_session_id", local_session_id)
    return f"fixer/wave-{normalized_wave_id}/session-{normalized_session_id}"


def _validate_wave_branch_name(branch_name: str) -> str:
    candidate = branch_name.strip()
    if not WAVE_BRANCH_PATTERN.fullmatch(candidate):
        raise RuntimeError(
            "Wave branch names must match fixer/wave-<wave_id>/session-<local_session_id>."
        )
    return candidate


def _wave_worktree_path(
    project_cwd: Path,
    worktree_root: Path,
    wave_id: int,
    local_session_id: int,
) -> Path:
    normalized_wave_id = _positive_wave_int("wave_id", wave_id)
    normalized_session_id = _positive_wave_int("local_session_id", local_session_id)
    root = worktree_root.expanduser()
    if not root.is_absolute():
        root = project_cwd.expanduser().resolve() / root
    return root / f"wave-{normalized_wave_id}" / f"session-{normalized_session_id}"


def _wave_worker_artifact_dir(project_cwd: Path, wave_id: int, local_session_id: int) -> Path:
    normalized_wave_id = _positive_wave_int("wave_id", wave_id)
    normalized_session_id = _positive_wave_int("local_session_id", local_session_id)
    return (
        project_cwd.expanduser().resolve()
        / ".codex"
        / "netrunner_wave_artifacts"
        / f"wave-{normalized_wave_id}"
        / f"session-{normalized_session_id}"
    )


def _wave_worker_metadata_path(project_cwd: Path, wave_id: int, local_session_id: int) -> Path:
    return _wave_worker_artifact_dir(project_cwd, wave_id, local_session_id) / "worker_metadata.json"


def _build_git_worktree_list_command(project_cwd: Path) -> list[str]:
    return ["git", "-C", str(project_cwd), "worktree", "list", "--porcelain"]


def _build_git_branch_exists_command(project_cwd: Path, branch_name: str) -> list[str]:
    return [
        "git",
        "-C",
        str(project_cwd),
        "show-ref",
        "--verify",
        f"refs/heads/{_validate_wave_branch_name(branch_name)}",
    ]


def _build_git_worktree_add_command(
    project_cwd: Path,
    *,
    worktree_path: Path,
    branch_name: str,
    base_sha: str,
) -> list[str]:
    resolved_base_sha = base_sha.strip()
    if not resolved_base_sha:
        raise RuntimeError("base_sha is required for git worktree add.")
    return [
        "git",
        "-C",
        str(project_cwd),
        "worktree",
        "add",
        "-b",
        _validate_wave_branch_name(branch_name),
        str(worktree_path),
        resolved_base_sha,
    ]


def _validate_specific_worktree_path(project_cwd: Path, worktree_path: Path) -> Path:
    candidate = worktree_path.expanduser()
    resolved_candidate = candidate.resolve()
    resolved_project = project_cwd.expanduser().resolve()
    if resolved_candidate == resolved_project:
        raise RuntimeError("Refusing to treat the canonical project cwd as a removable wave worktree.")
    if resolved_candidate == Path(resolved_candidate.anchor):
        raise RuntimeError("Refusing to build a worktree cleanup command for a filesystem root.")
    return candidate


def _build_git_worktree_remove_command(
    project_cwd: Path,
    worktree_path: Path,
    *,
    force: bool = False,
) -> list[str]:
    safe_worktree_path = _validate_specific_worktree_path(project_cwd, worktree_path)
    command = ["git", "-C", str(project_cwd), "worktree", "remove"]
    if force:
        command.append("--force")
    command.append(str(safe_worktree_path))
    return command


def _build_git_worktree_prune_command(project_cwd: Path, *, dry_run: bool = True) -> list[str]:
    command = ["git", "-C", str(project_cwd), "worktree", "prune"]
    if dry_run:
        command.append("--dry-run")
    return command


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
    build_common_codex_env_fn: Callable[[Any, Any, Path], dict[str, str]],
    build_wave_netrunner_prompt_fn: Callable[..., str],
) -> _WaveNetrunnerLaunchPlan:
    normalized_session_id = _positive_wave_int("local_session_id", local_session_id)
    normalized_wave_id = _positive_wave_int("wave_id", wave_id)
    normalized_wave_worker_id = _positive_wave_int("wave_worker_id", wave_worker_id)
    resolved_project_cwd = project_cwd.expanduser().resolve()
    resolved_worker_cwd = worker_cwd.expanduser().resolve()
    resolved_db_path = (
        db_path.expanduser().resolve()
        if db_path is not None
        else fixer_wire._resolve_fixer_db_path(resolved_project_cwd)
    )
    resolved_branch_name = _validate_wave_branch_name(
        branch_name or _wave_branch_name(normalized_wave_id, normalized_session_id)
    )

    selected_mcp_names = fixer_wire._normalize_names(assigned_mcp_names)
    if fixer_wire.FORCED_MCP_SERVER in available_servers:
        selected_mcp_names = fixer_wire._normalize_names([*selected_mcp_names, fixer_wire.FORCED_MCP_SERVER])
    selected_servers = {
        name: dict(available_servers[name])
        for name in selected_mcp_names
        if name in available_servers
    }
    if fixer_wire.FORCED_MCP_SERVER in selected_servers:
        selected_servers = fixer_wire._bind_fixer_db_path_to_server_env(
            selected_servers,
            db_path=resolved_db_path,
        )
        selected_servers = fixer_wire._bind_netrunner_stateless_auth_to_server_env(
            selected_servers,
            project_cwd=resolved_project_cwd,
        )
        selected_servers = fixer_wire._bind_locked_role_to_server_env(selected_servers, role="netrunner")
        selected_servers = fixer_wire._bind_launcher_telegram_env_to_server_env(selected_servers)

    selected_config_paths: dict[str, Path] = {}
    if "sqlite" in selected_servers:
        sqlite_config = ensure_sqlite_scaffold(resolved_project_cwd)
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
    adapter.ensure_runtime_files(resolved_worker_cwd, llm_selection, selected_servers, available_servers)
    prompt = build_wave_netrunner_prompt_fn(
        session_id=normalized_session_id,
        mcp_names=selected_mcp_names,
        fixer_session_id=fixer_session_id,
        mcp_how_to=mcp_how_to,
        wave_id=normalized_wave_id,
        wave_worker_id=normalized_wave_worker_id,
        branch_name=resolved_branch_name,
        worker_cwd=resolved_worker_cwd,
        declared_write_scope=declared_write_scope,
    )
    prompt = fixer_wire._append_droid_mcp_tool_guidance(
        prompt,
        backend=launch_selection.backend,
        mcp_names=selected_mcp_names,
    )
    env = build_common_codex_env_fn(adapter, llm_selection, resolved_project_cwd)
    env[fixer_wire.FIXER_DB_PATH_ENV] = str(resolved_db_path)
    for server_name, config_path in selected_config_paths.items():
        env_var = config_env_vars.get(server_name)
        if env_var:
            env[env_var] = str(config_path)

    command = adapter.build_headless_command(
        model=launch_selection.model,
        reasoning=launch_selection.reasoning,
        selected=selected_servers,
        available=available_servers,
        prompt=prompt,
    )
    return _WaveNetrunnerLaunchPlan(
        command=command,
        env=env,
        popen_cwd=resolved_worker_cwd,
        prompt=prompt,
        selected_mcp_names=selected_mcp_names,
        selected_servers=selected_servers,
        selected_config_paths=selected_config_paths,
        metadata={
            "backend": launch_selection.backend,
            "model": launch_selection.model,
            "reasoning": launch_selection.reasoning,
            "session_id": normalized_session_id,
            "wave_id": normalized_wave_id,
            "wave_worker_id": normalized_wave_worker_id,
            "project_cwd": str(resolved_project_cwd),
            "worker_cwd": str(resolved_worker_cwd),
            "branch_name": resolved_branch_name,
        },
    )


def _wave_headless_netrunner_log_path(
    project_cwd: Path,
    wave_id: int,
    local_session_id: int,
    backend: str,
) -> Path:
    artifact_dir = _wave_worker_artifact_dir(project_cwd, wave_id, local_session_id)
    artifact_dir.mkdir(parents=True, exist_ok=True)
    timestamp = int(time.time())
    return artifact_dir / f"headless-{backend}-{timestamp}.log"
