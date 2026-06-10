"""MCP discovery and materialization helpers for the Fixer wire launcher."""

from __future__ import annotations

import json
import os
import shutil
import subprocess
import sys
from pathlib import Path
from typing import Any, Callable, Sequence

try:
    import tomllib  # Python 3.11+
except ModuleNotFoundError:  # pragma: no cover
    import tomli as tomllib  # type: ignore

ROLE_CHOICES = ("fixer", "netrunner", "overseer")
FORCED_MCP_SERVER = "fixer_mcp"
HIDDEN_MCP_SERVERS = {FORCED_MCP_SERVER}
FIGMA_CONSOLE_MCP_NAME = "figma-console-mcp"
FIGMA_CONSOLE_MCP_FALLBACK_CATEGORY = "Design"
FIGMA_CONSOLE_MCP_FALLBACK_HOW_TO = (
    "Use for Figma design-system extraction, creation, and debugging workflows across "
    "components, variables, and layout iteration."
)
FIGMA_CONSOLE_MCP_TOKEN_ENV_NAMES = ("FIGMA_ACCESS_TOKEN", "FIGMA_TOKEN", "FIGMA_API_KEY")
RESEARCH_QUERY_MCP_NAME = "research_query_mcp"
PHILOLOGISTS_PROJECT_MARKER = "philologists"
WEB_MCP_CONFIG_FILENAME = "webMCP.toml"
FIXER_DB_PATH_ENV = "FIXER_DB_PATH"
FIXER_MCP_DEFAULT_ROLE_ENV = "FIXER_MCP_DEFAULT_ROLE"
FIXER_MCP_DEFAULT_CWD_ENV = "FIXER_MCP_DEFAULT_CWD"
FIXER_MCP_LOCKED_ROLE_ENV = "FIXER_MCP_LOCKED_ROLE"
FIXER_MCP_TELEGRAM_ENV_NAMES = (
    "FIXER_MCP_TELEGRAM_BOT_TOKEN",
    "FIXER_MCP_TELEGRAM_CHAT_ID",
    "FIXER_MCP_TELEGRAM_API_BASE_URL",
)
FIXER_MCP_BINARY_ENV = "FIXER_MCP_BINARY"
FIXER_MCP_AUTOBUILD_SKIP_ENV = "FIXER_WIRE_SKIP_FIXER_MCP_AUTOBUILD"
FORCED_FIXER_MCP_TIMEOUT_FLOOR_SEC = 21_600
FORCED_FIXER_MCP_TIMEOUT_FLOOR_MS = FORCED_FIXER_MCP_TIMEOUT_FLOOR_SEC * 1000
_FIXER_MCP_BUILD_CHECKED: set[Path] = set()


def _bind_fixer_db_path_to_server_env(
    selected_servers: dict[str, dict[str, object]],
    *,
    db_path: Path,
) -> dict[str, dict[str, object]]:
    fixer_spec = selected_servers.get(FORCED_MCP_SERVER)
    if fixer_spec is None:
        return selected_servers

    updated_servers = dict(selected_servers)
    updated_spec = dict(fixer_spec)
    existing_env = updated_spec.get("env")
    merged_env = dict(existing_env) if isinstance(existing_env, dict) else {}
    merged_env[FIXER_DB_PATH_ENV] = str(db_path)
    updated_spec["env"] = merged_env
    updated_servers[FORCED_MCP_SERVER] = updated_spec
    return updated_servers


def _bind_locked_role_to_server_env(
    selected_servers: dict[str, dict[str, object]],
    *,
    role: str,
) -> dict[str, dict[str, object]]:
    fixer_spec = selected_servers.get(FORCED_MCP_SERVER)
    if fixer_spec is None:
        return selected_servers

    normalized_role = role.strip().lower()
    if normalized_role not in ROLE_CHOICES:
        raise RuntimeError(f"Invalid {FIXER_MCP_LOCKED_ROLE_ENV}: {role!r}")

    updated_servers = dict(selected_servers)
    updated_spec = dict(fixer_spec)
    existing_env = updated_spec.get("env")
    merged_env = dict(existing_env) if isinstance(existing_env, dict) else {}
    merged_env[FIXER_MCP_LOCKED_ROLE_ENV] = normalized_role
    updated_spec["env"] = merged_env
    updated_servers[FORCED_MCP_SERVER] = updated_spec
    return updated_servers


def _bind_netrunner_stateless_auth_to_server_env(
    selected_servers: dict[str, dict[str, object]],
    *,
    project_cwd: Path,
) -> dict[str, dict[str, object]]:
    fixer_spec = selected_servers.get(FORCED_MCP_SERVER)
    if fixer_spec is None:
        return selected_servers

    updated_servers = dict(selected_servers)
    updated_spec = dict(fixer_spec)
    existing_env = updated_spec.get("env")
    merged_env = dict(existing_env) if isinstance(existing_env, dict) else {}
    merged_env[FIXER_MCP_DEFAULT_ROLE_ENV] = "netrunner"
    merged_env[FIXER_MCP_DEFAULT_CWD_ENV] = str(project_cwd.resolve())
    updated_spec["env"] = merged_env
    updated_servers[FORCED_MCP_SERVER] = updated_spec
    return updated_servers


def _bind_launcher_telegram_env_to_server_env(
    selected_servers: dict[str, dict[str, object]],
    *,
    environ: dict[str, str] | os._Environ[str] = os.environ,
) -> dict[str, dict[str, object]]:
    fixer_spec = selected_servers.get(FORCED_MCP_SERVER)
    if fixer_spec is None:
        return selected_servers

    updated_servers = dict(selected_servers)
    updated_spec = dict(fixer_spec)
    existing_env = updated_spec.get("env")
    merged_env = dict(existing_env) if isinstance(existing_env, dict) else {}
    dotenv_values = _load_fixer_mcp_dotenv(_resolve_fixer_mcp_server_root(fixer_spec) / ".env")
    for env_name in FIXER_MCP_TELEGRAM_ENV_NAMES:
        env_value = environ.get(env_name, "").strip()
        if not env_value:
            env_value = dotenv_values.get(env_name, "").strip()
        if env_value:
            merged_env[env_name] = env_value
    updated_spec["env"] = merged_env
    updated_servers[FORCED_MCP_SERVER] = updated_spec
    return updated_servers


def _resolve_fixer_mcp_server_root(fixer_spec: dict[str, object]) -> Path:
    cwd = fixer_spec.get("cwd")
    if isinstance(cwd, str) and cwd.strip():
        return Path(cwd).expanduser().resolve()

    command = fixer_spec.get("command")
    if isinstance(command, str) and command.strip():
        return Path(command).expanduser().resolve().parent

    return Path(__file__).resolve().parents[1] / "fixer_mcp"


def _load_fixer_mcp_dotenv(env_path: Path) -> dict[str, str]:
    try:
        lines = env_path.read_text(encoding="utf-8").splitlines()
    except OSError:
        return {}

    values: dict[str, str] = {}
    for raw_line in lines:
        line = raw_line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        name, raw_value = line.split("=", 1)
        name = name.strip()
        if name not in FIXER_MCP_TELEGRAM_ENV_NAMES:
            continue
        value = raw_value.strip()
        if len(value) >= 2 and value[0] == value[-1] and value[0] in {"'", '"'}:
            value = value[1:-1]
        values[name] = value
    return values


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
        parts = [f"{k}={_toml_literal(v)}" for k, v in value.items()]
        return "{" + ", ".join(parts) + "}"
    if isinstance(value, (list, tuple)):
        parts = [_toml_literal(v) for v in value]
        return "[" + ", ".join(parts) + "]"
    return _toml_literal(str(value))


def _load_forced_fixer_spec(
    *,
    repo_root: Callable[[], Path],
    maybe_rebuild_fixer_mcp_binary: Callable[[Path], None],
    environ: dict[str, str] | os._Environ[str] = os.environ,
) -> dict[str, object]:
    root = repo_root()
    config_path = root / "fixer_mcp" / "mcp_config.json"
    if not config_path.is_file():
        raise RuntimeError(f"Missing fixer MCP config: {config_path}")

    parsed = json.loads(config_path.read_text(encoding="utf-8"))
    raw_spec = parsed.get("mcpServers", {}).get(FORCED_MCP_SERVER)
    if not isinstance(raw_spec, dict):
        raise RuntimeError(f"Missing '{FORCED_MCP_SERVER}' entry in {config_path}")

    spec = dict(raw_spec)
    spec = _resolve_forced_fixer_command(
        spec,
        config_path=config_path,
        repo_root=root,
        maybe_rebuild_fixer_mcp_binary=maybe_rebuild_fixer_mcp_binary,
        environ=environ,
    )
    spec.setdefault("args", [])
    spec.setdefault("env", {})
    spec.setdefault("transport", "stdio")
    spec.setdefault("cwd", str((root / "fixer_mcp").resolve()))
    spec.setdefault("startup_timeout_sec", 30)
    return _with_forced_fixer_timeout_floor(spec)


def _resolve_forced_fixer_command(
    spec: dict[str, object],
    *,
    config_path: Path,
    repo_root: Path,
    maybe_rebuild_fixer_mcp_binary: Callable[[Path], None],
    environ: dict[str, str] | os._Environ[str] = os.environ,
) -> dict[str, object]:
    merged = dict(spec)
    repo_fixer_dir = repo_root / "fixer_mcp"
    repo_binary = (repo_fixer_dir / "fixer_mcp").resolve()
    override = environ.get(FIXER_MCP_BINARY_ENV, "").strip()
    if override:
        override_path = _resolve_forced_fixer_command_path(
            override,
            base_dir=repo_root,
        )
        maybe_rebuild_fixer_mcp_binary(override_path)
        merged["command"] = str(override_path)
        merged["cwd"] = str(override_path.parent)
        return merged

    configured_command = merged.get("command")
    if isinstance(configured_command, str) and configured_command.strip():
        configured_path = _resolve_forced_fixer_command_path(
            configured_command,
            base_dir=config_path.parent,
        )
        maybe_rebuild_fixer_mcp_binary(configured_path)
        if configured_path.exists():
            merged["command"] = str(configured_path)
            return merged

    maybe_rebuild_fixer_mcp_binary(repo_binary)
    merged["command"] = str(repo_binary)
    merged["cwd"] = str(repo_fixer_dir.resolve())
    return merged


def _resolve_forced_fixer_command_path(command: str, *, base_dir: Path) -> Path:
    command_path = Path(command).expanduser()
    if command_path.is_absolute():
        return command_path.resolve()
    return (base_dir / command_path).resolve()


def _describe_forced_fixer_resolution(
    *,
    repo_root: Callable[[], Path],
    environ: dict[str, str] | os._Environ[str] = os.environ,
) -> str:
    root = repo_root()
    config_path = root / "fixer_mcp" / "mcp_config.json"
    checked_paths: list[str] = []
    override = environ.get(FIXER_MCP_BINARY_ENV, "").strip()
    if override:
        checked_paths.append(str(_resolve_forced_fixer_command_path(override, base_dir=root)))
    else:
        try:
            parsed = json.loads(config_path.read_text(encoding="utf-8"))
            raw_spec = parsed.get("mcpServers", {}).get(FORCED_MCP_SERVER)
            configured_command = raw_spec.get("command") if isinstance(raw_spec, dict) else None
        except (OSError, json.JSONDecodeError):
            configured_command = None
        if isinstance(configured_command, str) and configured_command.strip():
            checked_paths.append(
                str(_resolve_forced_fixer_command_path(configured_command, base_dir=config_path.parent))
            )
        checked_paths.append(str((root / "fixer_mcp" / "fixer_mcp").resolve()))

    unique_paths = list(dict.fromkeys(checked_paths))
    path_text = ", ".join(unique_paths) if unique_paths else "(none)"
    override_text = f"{FIXER_MCP_BINARY_ENV}={override!r}" if override else f"{FIXER_MCP_BINARY_ENV} is unset"
    return f"Checked paths: {path_text}. Env override: {override_text}."


def _ensure_forced_fixer_server_resolved(
    servers: dict[str, dict[str, object]],
    *,
    repo_root: Callable[[], Path],
    environ: dict[str, str] | os._Environ[str] = os.environ,
) -> None:
    fixer_spec = servers.get(FORCED_MCP_SERVER)
    details = _describe_forced_fixer_resolution(repo_root=repo_root, environ=environ)
    if fixer_spec is None:
        raise RuntimeError(
            f"Forced '{FORCED_MCP_SERVER}' MCP server is unavailable; refusing to launch without "
            f"the Fixer control plane. {details}"
        )

    command = fixer_spec.get("command")
    if not isinstance(command, str) or not command.strip():
        raise RuntimeError(
            f"Forced '{FORCED_MCP_SERVER}' MCP server has no command; refusing to launch without "
            f"the Fixer control plane. {details}"
        )

    command_path = Path(command.strip()).expanduser()
    is_path_like = command_path.is_absolute() or any(separator in command.strip() for separator in ("/", "\\"))
    if not is_path_like:
        return

    if not command_path.is_absolute():
        cwd = fixer_spec.get("cwd")
        base_dir = Path(cwd).expanduser() if isinstance(cwd, str) and cwd.strip() else Path.cwd()
        command_path = base_dir / command_path
    command_path = command_path.resolve()
    if not command_path.is_file() or not os.access(command_path, os.X_OK):
        raise RuntimeError(
            f"Forced '{FORCED_MCP_SERVER}' MCP server command is not an executable file: {command_path}. "
            f"{details} Set {FIXER_MCP_BINARY_ENV} to an executable fixer_mcp binary or build "
            f"{repo_root() / 'fixer_mcp'}."
        )


def _with_forced_fixer_timeout_floor(spec: dict[str, object]) -> dict[str, object]:
    merged = dict(spec)

    timeout_value = merged.get("timeout")
    if not isinstance(timeout_value, int) or timeout_value < FORCED_FIXER_MCP_TIMEOUT_FLOOR_SEC:
        merged["timeout"] = FORCED_FIXER_MCP_TIMEOUT_FLOOR_SEC

    tool_timeout_value = merged.get("tool_timeout_sec")
    if not isinstance(tool_timeout_value, int) or tool_timeout_value < FORCED_FIXER_MCP_TIMEOUT_FLOOR_SEC:
        merged["tool_timeout_sec"] = FORCED_FIXER_MCP_TIMEOUT_FLOOR_SEC

    per_tool_timeout_value = merged.get("per_tool_timeout_ms")
    if not isinstance(per_tool_timeout_value, int) or per_tool_timeout_value < FORCED_FIXER_MCP_TIMEOUT_FLOOR_MS:
        merged["per_tool_timeout_ms"] = FORCED_FIXER_MCP_TIMEOUT_FLOOR_MS

    return merged


def _latest_mtime(paths: Sequence[Path]) -> float:
    mtimes = [path.stat().st_mtime for path in paths if path.exists()]
    return max(mtimes) if mtimes else 0.0


def _maybe_rebuild_fixer_mcp_binary(
    command_path: Path,
    *,
    repo_root: Callable[[], Path],
    build_checked: set[Path] = _FIXER_MCP_BUILD_CHECKED,
    environ: dict[str, str] | os._Environ[str] = os.environ,
    subprocess_run: Callable[..., object] = subprocess.run,
    stderr: Any = sys.stderr,
) -> None:
    if environ.get(FIXER_MCP_AUTOBUILD_SKIP_ENV, "").strip() == "1":
        return

    module_dir = repo_root() / "fixer_mcp"
    if not module_dir.is_dir():
        return

    binary_path = command_path.expanduser()
    if not binary_path.is_absolute():
        binary_path = (module_dir / binary_path).resolve()
    else:
        binary_path = binary_path.resolve()

    try:
        binary_path.relative_to(module_dir.resolve())
    except ValueError:
        return

    if binary_path in build_checked:
        return

    source_candidates = [*module_dir.rglob("*.go"), module_dir / "go.mod", module_dir / "go.sum"]
    latest_source_mtime = _latest_mtime(source_candidates)
    binary_mtime = binary_path.stat().st_mtime if binary_path.exists() else 0.0
    if binary_mtime >= latest_source_mtime:
        build_checked.add(binary_path)
        return

    print(
        "[fixer-wire] detected stale fixer_mcp binary; rebuilding before launch...",
        file=stderr,
    )
    try:
        subprocess_run(
            ["go", "build", "-o", str(binary_path), "."],
            cwd=str(module_dir),
            check=True,
        )
    except (OSError, subprocess.CalledProcessError) as err:
        raise RuntimeError(
            f"Failed to rebuild fixer_mcp binary at {binary_path}: {err}. "
            f"Set {FIXER_MCP_AUTOBUILD_SKIP_ENV}=1 to disable auto-build."
        ) from err

    build_checked.add(binary_path)


def _build_forced_fixer_override_args(spec: dict[str, object]) -> list[str]:
    overrides = [f"mcp_servers.{FORCED_MCP_SERVER}.enabled=true"]
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
    ):
        if field in spec:
            overrides.append(f"mcp_servers.{FORCED_MCP_SERVER}.{field}={_toml_literal(spec[field])}")

    args: list[str] = []
    for override in overrides:
        args.extend(["-c", override])
    return args


def _inject_forced_fixer_server(
    available_servers: dict[str, dict[str, object]],
    *,
    forced_fixer_spec: dict[str, object],
) -> dict[str, dict[str, object]]:
    merged = dict(available_servers)
    current = dict(merged.get(FORCED_MCP_SERVER, {}))
    current.update(forced_fixer_spec)
    current["_source"] = "project_mcp"
    merged[FORCED_MCP_SERVER] = current
    return merged


def _parse_simple_env_file(path: Path, keys: Sequence[str]) -> dict[str, str]:
    key_set = set(keys)
    if not path.is_file():
        return {}

    values: dict[str, str] = {}
    try:
        lines = path.read_text(encoding="utf-8").splitlines()
    except OSError:
        return {}

    for raw_line in lines:
        line = raw_line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        name, raw_value = line.split("=", 1)
        name = name.strip()
        if name not in key_set:
            continue
        value = raw_value.strip()
        if len(value) >= 2 and value[0] == value[-1] and value[0] in {"'", '"'}:
            value = value[1:-1]
        if value:
            values[name] = value
    return values


def _figma_console_env_file_candidates(
    cwd: Path,
    *,
    environ: dict[str, str] | os._Environ[str] = os.environ,
) -> list[Path]:
    global_env = Path(environ.get("FIGMA_MCP_GLOBAL_ENV", "~/.codex/figma.env")).expanduser()
    return [global_env, cwd / ".env"]


def _load_figma_console_credentials(
    cwd: Path,
    *,
    environ: dict[str, str] | os._Environ[str] = os.environ,
) -> dict[str, str]:
    env: dict[str, str] = {}
    for name in FIGMA_CONSOLE_MCP_TOKEN_ENV_NAMES:
        value = environ.get(name, "").strip()
        if value:
            env[name] = value

    for path in _figma_console_env_file_candidates(cwd, environ=environ):
        for name, value in _parse_simple_env_file(path, FIGMA_CONSOLE_MCP_TOKEN_ENV_NAMES).items():
            env.setdefault(name, value)

    if "FIGMA_ACCESS_TOKEN" not in env:
        access_token_alias = env.get("FIGMA_TOKEN") or env.get("FIGMA_API_KEY")
        if access_token_alias:
            env["FIGMA_ACCESS_TOKEN"] = access_token_alias
    return env


def _inject_figma_console_server(
    available_servers: dict[str, dict[str, object]],
    cwd: Path,
    *,
    environ: dict[str, str] | os._Environ[str] = os.environ,
) -> dict[str, dict[str, object]]:
    merged = dict(available_servers)
    fallback_spec: dict[str, object] = {
        "command": "npx",
        "args": ["-y", "figma-console-mcp@latest"],
        "env": {"ENABLE_MCP_APPS": "true"},
        "transport": "stdio",
        "cwd": str(cwd.resolve()),
        "startup_timeout_sec": 120,
        "timeout": 600,
        "tool_timeout_sec": 600,
    }

    current = dict(merged.get(FIGMA_CONSOLE_MCP_NAME, {}))
    for key in ("command", "args", "transport", "cwd", "startup_timeout_sec", "timeout", "tool_timeout_sec"):
        current.setdefault(key, fallback_spec[key])

    existing_env = current.get("env")
    env_map = dict(existing_env) if isinstance(existing_env, dict) else {}
    for env_key, env_value in fallback_spec["env"].items():
        env_map.setdefault(env_key, env_value)
    env_map.update(_load_figma_console_credentials(cwd, environ=environ))
    current["env"] = env_map

    current.setdefault("_source", "project_mcp")
    merged[FIGMA_CONSOLE_MCP_NAME] = current
    return merged


def _inject_research_query_server(
    available_servers: dict[str, dict[str, object]],
    cwd: Path,
    *,
    which: Callable[[str], str | None] = shutil.which,
) -> dict[str, dict[str, object]]:
    merged = dict(available_servers)
    if RESEARCH_QUERY_MCP_NAME in merged:
        return merged

    resolved_cwd = cwd.resolve()
    if PHILOLOGISTS_PROJECT_MARKER not in str(resolved_cwd).lower():
        return merged

    llm_pipeline_dir = resolved_cwd / "philologists_paradise" / "llm_pipeline"
    server_entrypoint = llm_pipeline_dir / "cmd" / "research_query_mcp" / "main.go"
    if not server_entrypoint.is_file():
        return merged
    if which("go") is None:
        return merged

    merged[RESEARCH_QUERY_MCP_NAME] = {
        "command": "go",
        "args": ["run", "./cmd/research_query_mcp", "--transport", "stdio"],
        "env": {},
        "transport": "stdio",
        "cwd": str(llm_pipeline_dir.resolve()),
        "enabled": False,
        "_source": "project_mcp",
        "startup_timeout_sec": 120,
        "timeout": 600,
        "tool_timeout_sec": 600,
    }
    return merged


def _load_project_web_mcp_servers(
    cwd: Path,
    *,
    stderr: Any = sys.stderr,
) -> dict[str, dict[str, object]]:
    config_path = cwd / WEB_MCP_CONFIG_FILENAME
    if not config_path.is_file():
        return {}

    try:
        with config_path.open("rb") as fh:
            data = tomllib.load(fh)
    except Exception as exc:  # pragma: no cover - defensive
        print(f"[warning] failed to parse {config_path}: {exc}", file=stderr)
        return {}

    servers_block = data.get("mcp_servers")
    if not isinstance(servers_block, dict):
        servers_block = data.get("mcpServers")
    if not isinstance(servers_block, dict):
        print(
            f"[warning] {config_path} does not define [mcp_servers] (or [mcpServers]); skipping",
            file=stderr,
        )
        return {}

    discovered: dict[str, dict[str, object]] = {}
    for name, raw_cfg in sorted(servers_block.items(), key=lambda item: str(item[0]).lower()):
        if not isinstance(raw_cfg, dict):
            continue
        command = raw_cfg.get("command")
        if not isinstance(command, str) or not command.strip():
            print(
                f"[warning] skipping server '{name}' in {config_path} (missing command)",
                file=stderr,
            )
            continue

        raw_args = raw_cfg.get("args", [])
        args = raw_args if isinstance(raw_args, list) else [raw_args]
        env = raw_cfg.get("env") if isinstance(raw_cfg.get("env"), dict) else {}
        transport = raw_cfg.get("transport") or "stdio"
        cwd_value = raw_cfg.get("cwd")
        if isinstance(cwd_value, str) and cwd_value.strip():
            raw_server_cwd = Path(cwd_value).expanduser()
            resolved_cwd = raw_server_cwd if raw_server_cwd.is_absolute() else (config_path.parent / raw_server_cwd)
        else:
            resolved_cwd = config_path.parent

        discovered[str(name)] = {
            "command": command,
            "args": args,
            "env": env,
            "transport": transport,
            "cwd": str(resolved_cwd.resolve()),
            "enabled": False,
            "_source": "project_mcp",
            "_config_path": str(config_path.resolve()),
            "startup_timeout_sec": raw_cfg.get("startup_timeout_sec", 30),
            "tool_timeout_sec": raw_cfg.get("tool_timeout_sec", 600),
            "timeout": raw_cfg.get("timeout", 600),
            "per_tool_timeout_ms": raw_cfg.get("per_tool_timeout_ms", 600_000),
        }

    return discovered


def _overlay_project_mcp_servers(
    base: dict[str, dict[str, object]],
    overrides: dict[str, dict[str, object]],
) -> dict[str, dict[str, object]]:
    merged = dict(base)
    for name, cfg in overrides.items():
        current = dict(merged.get(name, {}))
        current.update(cfg)
        merged[name] = current
    return merged
