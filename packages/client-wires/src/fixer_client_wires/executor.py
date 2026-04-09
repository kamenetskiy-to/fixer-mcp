from __future__ import annotations

import json
import os
import shutil
import subprocess
from pathlib import Path
from typing import Callable, Mapping

from .backends.base import normalize_mcp_server_payload
from .bootstrap import resolve_repo_root, resolve_staged_skills_root
from .launcher import LaunchPlan, load_mcp_config

STATE_ROOT_ENV = "FIXER_CLIENT_WIRES_STATE_ROOT"
FIXER_DB_PATH_ENV = "FIXER_DB_PATH"
FIXER_SERVER_NAME = "fixer_mcp"
FIXER_SERVER_TIMEOUT_FLOOR_SEC = 21_600
FIXER_SERVER_TIMEOUT_FLOOR_MS = FIXER_SERVER_TIMEOUT_FLOOR_SEC * 1000
FIXER_SERVER_AUTOBUILD_SKIP_ENV = "FIXER_CLIENT_WIRES_SKIP_FIXER_MCP_AUTOBUILD"
FIXER_CLIENT_WIRES_SKIP_FIXER_MCP_AUTOBUILD_ENV = FIXER_SERVER_AUTOBUILD_SKIP_ENV


def resolve_state_root(environ: Mapping[str, str] | None = None) -> Path:
    env = environ or os.environ
    override = env.get(STATE_ROOT_ENV, "").strip()
    if override:
        return Path(override).expanduser().resolve()
    return (Path.home() / ".local" / "state" / "fixer-client-wires").resolve()


def _toml_literal(value: object) -> str:
    if value is None:
        return '""'
    if isinstance(value, bool):
        return "true" if value else "false"
    if isinstance(value, (int, float)):
        return str(value)
    if isinstance(value, str):
        escaped = value.replace("\\", "\\\\").replace('"', '\\"')
        return f'"{escaped}"'
    if isinstance(value, dict):
        return "{" + ", ".join(f"{key}={_toml_literal(raw)}" for key, raw in value.items()) + "}"
    if isinstance(value, (list, tuple)):
        return "[" + ", ".join(_toml_literal(raw) for raw in value) + "]"
    return _toml_literal(str(value))


def _latest_mtime(paths: list[Path]) -> float:
    mtimes = [path.stat().st_mtime for path in paths if path.exists()]
    return max(mtimes) if mtimes else 0.0


def _ensure_fixer_mcp_binary(repo_root: Path, *, environ: Mapping[str, str] | None = None) -> Path:
    env = environ or os.environ
    module_dir = (repo_root / "packages" / "fixer-mcp-server").resolve()
    binary_path = module_dir / "fixer_mcp"
    if env.get(FIXER_SERVER_AUTOBUILD_SKIP_ENV, "").strip() == "1":
        return binary_path

    source_candidates = [*module_dir.rglob("*.go"), module_dir / "go.mod", module_dir / "go.sum"]
    latest_source_mtime = _latest_mtime(source_candidates)
    binary_mtime = binary_path.stat().st_mtime if binary_path.exists() else 0.0
    if binary_mtime >= latest_source_mtime:
        return binary_path

    subprocess.run(
        ["go", "build", "-o", str(binary_path), "."],
        cwd=str(module_dir),
        check=True,
    )
    return binary_path


def _resolve_relative_path(raw_value: str, *, base_dir: Path) -> str:
    candidate = Path(raw_value).expanduser()
    if candidate.is_absolute():
        return str(candidate.resolve())
    return str((base_dir / candidate).resolve())


def prepare_selected_servers(
    plan: LaunchPlan,
    *,
    state_root: Path | None = None,
    environ: Mapping[str, str] | None = None,
) -> dict[str, dict[str, object]]:
    repo_root = resolve_repo_root()
    resolved_state_root = (state_root or resolve_state_root(environ)).resolve()
    resolved_state_root.mkdir(parents=True, exist_ok=True)
    available = load_mcp_config(plan.config_path)
    prepared: dict[str, dict[str, object]] = {}

    for name in plan.selected_mcp_servers:
        spec = dict(available.get(name, {}))
        command = spec.get("command")
        if isinstance(command, str) and command.strip() and "url" not in spec:
            spec["command"] = _resolve_relative_path(command.strip(), base_dir=plan.config_path.parent)

        cwd_value = spec.get("cwd")
        if isinstance(cwd_value, str) and cwd_value.strip():
            spec["cwd"] = _resolve_relative_path(cwd_value.strip(), base_dir=plan.config_path.parent)

        if name == FIXER_SERVER_NAME:
            binary_path = _ensure_fixer_mcp_binary(repo_root, environ=environ)
            spec["command"] = str(binary_path)
            spec["cwd"] = str(resolved_state_root)
            env_block = spec.get("env")
            merged_env = dict(env_block) if isinstance(env_block, dict) else {}
            merged_env[FIXER_DB_PATH_ENV] = str((resolved_state_root / "fixer.db").resolve())
            spec["env"] = merged_env
            if not isinstance(spec.get("startup_timeout_sec"), int) or int(spec["startup_timeout_sec"]) < 30:
                spec["startup_timeout_sec"] = 30
            if not isinstance(spec.get("timeout"), int) or int(spec["timeout"]) < FIXER_SERVER_TIMEOUT_FLOOR_SEC:
                spec["timeout"] = FIXER_SERVER_TIMEOUT_FLOOR_SEC
            if not isinstance(spec.get("tool_timeout_sec"), int) or int(spec["tool_timeout_sec"]) < FIXER_SERVER_TIMEOUT_FLOOR_SEC:
                spec["tool_timeout_sec"] = FIXER_SERVER_TIMEOUT_FLOOR_SEC
            if not isinstance(spec.get("per_tool_timeout_ms"), int) or int(spec["per_tool_timeout_ms"]) < FIXER_SERVER_TIMEOUT_FLOOR_MS:
                spec["per_tool_timeout_ms"] = FIXER_SERVER_TIMEOUT_FLOOR_MS

        prepared[name] = spec

    return prepared


def build_codex_override_args(selected_servers: Mapping[str, Mapping[str, object]]) -> list[str]:
    overrides: list[str] = []
    for name, spec in sorted(selected_servers.items()):
        overrides.append(f"mcp_servers.{name}.enabled=true")
        for field in (
            "command",
            "args",
            "env",
            "transport",
            "cwd",
            "startup_timeout_sec",
            "timeout",
            "tool_timeout_sec",
            "per_tool_timeout_ms",
            "url",
            "headers",
        ):
            if field in spec:
                overrides.append(f"mcp_servers.{name}.{field}={_toml_literal(spec[field])}")

    args: list[str] = []
    for override in overrides:
        args.extend(["-c", override])
    return args


def _inject_codex_override_args(command: tuple[str, ...], overrides: list[str]) -> list[str]:
    rendered = list(command)
    if not overrides:
        return rendered
    try:
        exec_index = rendered.index("exec")
    except ValueError:
        return [*rendered, *overrides]
    return [*rendered[:exec_index], *overrides, *rendered[exec_index:]]


def _materialize_factory_skills(skills_root: Path, launch_cwd: Path) -> None:
    destination_root = launch_cwd / ".factory" / "skills"
    destination_root.mkdir(parents=True, exist_ok=True)
    for source_dir in skills_root.iterdir():
        if not source_dir.is_dir() or not (source_dir / "SKILL.md").is_file():
            continue
        destination = destination_root / source_dir.name
        shutil.rmtree(destination, ignore_errors=True)
        shutil.copytree(source_dir, destination)


def _materialize_droid_files(
    launch_cwd: Path,
    *,
    selected_servers: Mapping[str, Mapping[str, object]],
    skills_root: Path,
) -> None:
    factory_root = launch_cwd / ".factory"
    factory_root.mkdir(parents=True, exist_ok=True)
    mcp_path = factory_root / "mcp.json"
    payload = {
        "mcpServers": {
            name: normalize_mcp_server_payload(config)
            for name, config in sorted(selected_servers.items())
        }
    }
    mcp_path.write_text(json.dumps(payload, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    _materialize_factory_skills(skills_root, launch_cwd)


def _materialize_claude_files(
    launch_cwd: Path,
    *,
    selected_servers: Mapping[str, Mapping[str, object]],
) -> None:
    payload = {
        "mcpServers": {
            name: {
                key: value
                for key, value in config.items()
                if key in {
                    "command",
                    "args",
                    "env",
                    "transport",
                    "cwd",
                    "startup_timeout_sec",
                    "timeout",
                    "tool_timeout_sec",
                    "per_tool_timeout_ms",
                    "url",
                    "headers",
                }
            }
            for name, config in sorted(selected_servers.items())
        }
    }
    (launch_cwd / ".mcp.json").write_text(json.dumps(payload, indent=2, sort_keys=True) + "\n", encoding="utf-8")


def execute_launch_plan(
    plan: LaunchPlan,
    *,
    launch_cwd: Path | None = None,
    environ: Mapping[str, str] | None = None,
    runner: Callable[..., int] | None = None,
) -> int:
    resolved_launch_cwd = (launch_cwd or Path.cwd()).resolve()
    resolved_state_root = resolve_state_root(environ)
    selected_servers = prepare_selected_servers(plan, state_root=resolved_state_root, environ=environ)

    command = list(plan.command)
    skills_root = resolve_staged_skills_root(environ=environ).root
    if plan.backend.name == "codex":
        command = _inject_codex_override_args(plan.command, build_codex_override_args(selected_servers))
    elif plan.backend.name == "droid":
        _materialize_droid_files(resolved_launch_cwd, selected_servers=selected_servers, skills_root=skills_root)
    elif plan.backend.name == "claude":
        _materialize_claude_files(resolved_launch_cwd, selected_servers=selected_servers)

    env = dict(os.environ)
    env.setdefault(FIXER_DB_PATH_ENV, str((resolved_state_root / "fixer.db").resolve()))
    if environ is not None:
        env.update({str(key): str(value) for key, value in environ.items()})
    invoke = runner or subprocess.call
    return int(invoke(command, cwd=str(resolved_launch_cwd), env=env))
