from __future__ import annotations

from pathlib import Path


def resolve_repo_root() -> Path:
    return Path(__file__).resolve().parents[1]


def bootstrap_codex_pro_import_path() -> Path:
    """Deprecated no-op: Codex compatibility code is vendored in client_wires.codex_compat."""
    return resolve_repo_root()


def wire_info_lines(_compat_root: Path) -> list[str]:
    repo_root = resolve_repo_root()
    return [
        "Fixer wire bootstrap resolved:",
        f"- self_orchestration: {repo_root}",
        "- codex compatibility: client_wires.codex_compat (vendored)",
        f"- canonical entrypoint: {repo_root / 'client_wires' / 'fixer_wire.py'}",
    ]
