from __future__ import annotations

import os
import sys
from pathlib import Path
from typing import Iterable, Optional

MCP_SERVERS_ROOT_ENV = "MCP_SERVERS_ROOT"


def resolve_repo_root() -> Path:
    return Path(__file__).resolve().parents[1]


def _candidate_mcp_roots(repo_root: Path) -> Iterable[Path]:
    from_env = os.environ.get(MCP_SERVERS_ROOT_ENV)
    if from_env:
        yield Path(from_env).expanduser()
    yield repo_root
    yield repo_root.parent / "mcp_servers"


def _is_valid_mcp_servers_root(path: Path) -> bool:
    return (path / "codex_pro_app" / "__init__.py").is_file()


def resolve_mcp_servers_root(repo_root: Optional[Path] = None) -> Path:
    root = repo_root or resolve_repo_root()
    candidates = [candidate.resolve() for candidate in _candidate_mcp_roots(root)]
    for candidate in candidates:
        if _is_valid_mcp_servers_root(candidate):
            return candidate

    checked = ", ".join(str(candidate) for candidate in candidates)
    raise RuntimeError(
        "Could not locate mcp_servers root with codex_pro_app package. "
        f"Checked: {checked}. Set {MCP_SERVERS_ROOT_ENV} to override."
    )


def bootstrap_codex_pro_import_path() -> Path:
    mcp_root = resolve_mcp_servers_root()
    mcp_root_str = str(mcp_root)
    if mcp_root_str not in sys.path:
        sys.path.insert(0, mcp_root_str)
    return mcp_root


def wire_info_lines(mcp_root: Path) -> list[str]:
    repo_root = resolve_repo_root()
    return [
        "Fixer wire bootstrap resolved:",
        f"- self_orchestration: {repo_root}",
        f"- mcp_servers: {mcp_root}",
        f"- canonical entrypoint: {repo_root / 'client_wires' / 'fixer_wire.py'}",
    ]
