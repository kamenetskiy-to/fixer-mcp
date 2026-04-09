from __future__ import annotations

import json
import os
import sys
from pathlib import Path
from typing import Dict, Any

try:
    import tomllib  # Python 3.11+
except ModuleNotFoundError:  # pragma: no cover
    import tomli as tomllib  # type: ignore


DEFAULT_CONFIG_PATH = Path.home() / ".codex" / "config.toml"


class ConfigError(RuntimeError):
    """Raised when the Codex config cannot be loaded or parsed."""


def get_config_path() -> Path:
    override = os.environ.get("CODEX_CONFIG_PATH")
    return Path(override).expanduser() if override else DEFAULT_CONFIG_PATH


def load_config(path: Path) -> Dict[str, object]:
    if not path.exists():
        raise ConfigError(f"Config file not found at {path}")
    try:
        with path.open("rb") as fh:
            return tomllib.load(fh)
    except Exception as exc:  # pragma: no cover
        raise ConfigError(f"Failed to parse {path}: {exc}") from exc


def fetch_mcp_servers(config: Dict[str, object]) -> Dict[str, Dict[str, object]]:
    servers = config.get("mcp_servers", {})
    if not isinstance(servers, dict):
        raise ConfigError("Expected [mcp_servers] to map names to server configs")
    return servers  # type: ignore[return-value]


def discover_self_mcp_servers(cwd: Path) -> tuple[Dict[str, Dict[str, object]], list[Path]]:
    """Load MCP server definitions from self_mcp_servers/*/mcp.json relative to cwd.

    Returns (servers, missing) where `missing` lists server dirs without mcp.json.
    """
    root = cwd / "self_mcp_servers"
    if not root.is_dir():
        return {}, []

    discovered: Dict[str, Dict[str, object]] = {}
    missing: list[Path] = []
    for entry in sorted(root.iterdir()):
        if not entry.is_dir():
            continue
        config_path = entry / "mcp.json"
        if not config_path.is_file():
            missing.append(entry)
            continue
        try:
            data: Dict[str, Any] = json.loads(config_path.read_text(encoding="utf-8"))
        except Exception as exc:  # pragma: no cover - defensive
            print(f"Warning: failed to parse {config_path}: {exc}", file=sys.stderr)
            continue

        name = str(data.get("name") or entry.name)
        command = data.get("command")
        if not command:
            print(f"Warning: skipping {config_path} (missing command)", file=sys.stderr)
            continue

        raw_args = data.get("args", [])
        args = raw_args if isinstance(raw_args, list) else [raw_args]
        env = data.get("env") if isinstance(data.get("env"), dict) else {}
        transport = data.get("transport") or "stdio"
        cwd_value = data.get("cwd")
        resolved_cwd = entry / cwd_value if cwd_value else entry

        server_cfg: Dict[str, Any] = {
            "command": command,
            "args": args,
            "env": env,
            "transport": transport,
            "cwd": str(resolved_cwd.resolve()),
            "enabled": False,
            "_source": "self_mcp",
        }
        startup_timeout = data.get("startup_timeout_sec")
        if startup_timeout is None:
            startup_timeout = 30
        server_cfg["startup_timeout_sec"] = startup_timeout
        server_cfg["tool_timeout_sec"] = data.get("tool_timeout_sec", 600)
        if "timeout" in data:
            server_cfg["timeout"] = data["timeout"]
        else:
            server_cfg["timeout"] = 600  # allow long-running tools (e.g., run_live_analysis)
        # Host-level per-tool timeout (ms) for Codex client; prevents 60s tool-call cutoff.
        server_cfg["per_tool_timeout_ms"] = data.get("per_tool_timeout_ms", 600_000)

        preprompt_path = entry / "preprompt.md"
        if preprompt_path.is_file():
            server_cfg["_preprompt_path"] = str(preprompt_path.resolve())

        if name in discovered:
            print(f"Warning: duplicate self MCP server name '{name}' at {config_path}", file=sys.stderr)
            continue
        discovered[name] = server_cfg

    return discovered, missing


def discover_project_mcp_servers(cwd: Path) -> Dict[str, Dict[str, object]]:
    """Load MCP server definitions from local mcp_config.json files.

    Supported shapes:
    - {"mcpServers": {...}}
    - {"mcp_servers": {...}}

    Search scope:
    - <cwd>/mcp_config.json
    - <cwd>/*/mcp_config.json
    """
    ignore_dir_prefixes = (".",)
    ignore_dirs = {"node_modules", "__pycache__", ".venv", ".git"}
    candidates: list[Path] = []

    root_config = cwd / "mcp_config.json"
    if root_config.is_file():
        candidates.append(root_config)

    try:
        entries = sorted(cwd.iterdir(), key=lambda p: p.name.lower())
    except OSError:
        entries = []
    for entry in entries:
        if not entry.is_dir():
            continue
        if entry.name in ignore_dirs or entry.name.startswith(ignore_dir_prefixes):
            continue
        config_path = entry / "mcp_config.json"
        if config_path.is_file():
            candidates.append(config_path)

    discovered: Dict[str, Dict[str, object]] = {}
    for config_path in candidates:
        try:
            data: Dict[str, Any] = json.loads(config_path.read_text(encoding="utf-8"))
        except Exception as exc:  # pragma: no cover - defensive
            print(f"Warning: failed to parse {config_path}: {exc}", file=sys.stderr)
            continue

        servers_block = data.get("mcpServers")
        if not isinstance(servers_block, dict):
            servers_block = data.get("mcp_servers")
        if not isinstance(servers_block, dict):
            continue

        for name, raw_cfg in servers_block.items():
            if not isinstance(raw_cfg, dict):
                continue
            command = raw_cfg.get("command")
            if not isinstance(command, str) or not command.strip():
                print(
                    f"Warning: skipping server '{name}' in {config_path} (missing command)",
                    file=sys.stderr,
                )
                continue

            raw_args = raw_cfg.get("args", [])
            args = raw_args if isinstance(raw_args, list) else [raw_args]
            env = raw_cfg.get("env") if isinstance(raw_cfg.get("env"), dict) else {}
            transport = raw_cfg.get("transport") or "stdio"
            cwd_value = raw_cfg.get("cwd")
            if isinstance(cwd_value, str) and cwd_value.strip():
                raw_cwd = Path(cwd_value).expanduser()
                resolved_cwd = raw_cwd if raw_cwd.is_absolute() else (config_path.parent / raw_cwd)
            else:
                resolved_cwd = config_path.parent

            server_cfg: Dict[str, Any] = {
                "command": command,
                "args": args,
                "env": env,
                "transport": transport,
                "cwd": str(resolved_cwd.resolve()),
                "enabled": False,
                "_source": "project_mcp",
                "_config_path": str(config_path.resolve()),
            }
            server_cfg["startup_timeout_sec"] = raw_cfg.get("startup_timeout_sec", 30)
            server_cfg["tool_timeout_sec"] = raw_cfg.get("tool_timeout_sec", 600)
            if "timeout" in raw_cfg:
                server_cfg["timeout"] = raw_cfg["timeout"]
            else:
                server_cfg["timeout"] = 600
            server_cfg["per_tool_timeout_ms"] = raw_cfg.get("per_tool_timeout_ms", 600_000)

            if name in discovered:
                print(
                    f"Warning: duplicate project MCP server name '{name}' at {config_path} (skipped)",
                    file=sys.stderr,
                )
                continue
            discovered[name] = server_cfg

    return discovered


def merge_mcp_servers(
    base: Dict[str, Dict[str, object]],
    additions: Dict[str, Dict[str, object]],
) -> Dict[str, Dict[str, object]]:
    merged = dict(base)
    for name, cfg in additions.items():
        existing = merged.get(name)
        if existing:
            # Prefer self MCP definitions when they shadow a global entry (keeps local _source/preprompt info).
            if cfg.get("_source") == "self_mcp":
                merged_cfg = dict(existing)
                merged_cfg.update(cfg)
                merged[name] = merged_cfg
            continue
        merged[name] = cfg
    return merged


def attach_preprompts_from_command_paths(servers: Dict[str, Dict[str, object]]) -> None:
    """Attach `_preprompt_path` for servers whose `command` points at a local wrapper dir.

    This supports global `~/.codex/config.toml` entries that launch a local wrapper script,
    without requiring `self_mcp_servers/*/preprompt.md`.
    """
    for cfg in servers.values():
        if not isinstance(cfg, dict):
            continue
        if cfg.get("_preprompt_path"):
            continue
        command = cfg.get("command")
        if not isinstance(command, str) or not command.strip():
            continue

        command_path = Path(command).expanduser()
        if not command_path.is_file():
            continue

        wrapper_dir = command_path.resolve().parent
        candidate = wrapper_dir / "preprompt.md"
        if candidate.is_file():
            cfg["_preprompt_path"] = str(candidate.resolve())
