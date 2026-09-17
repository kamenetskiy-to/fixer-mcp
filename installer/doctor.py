"""System diagnostics and health checks for Fixer MCP installations."""

import os
import platform
import shutil
import sqlite3
import sys
from typing import Any, Dict, List, Optional

from installer.metadata import InstallMetadata, is_development_checkout
from installer.paths import (
    current_link_path,
    resolve_cache_dir,
    resolve_config_dir,
    resolve_db_path,
    resolve_managed_root,
    resolve_state_dir,
    resolve_user_bin_dir,
)
from installer.shim import detect_shadowing


class DoctorReport:
    def __init__(self, data: Dict[str, Any]):
        self.data = data

    @property
    def is_healthy(self) -> bool:
        return len(self.data.get("issues", [])) == 0

    def to_dict(self) -> Dict[str, Any]:
        return self.data

    def format_text(self) -> str:
        d = self.data
        lines = [
            "=== Fixer MCP Doctor Diagnostic ===",
            f"Install Mode:       {d.get('mode')}",
            f"Installed Version:  {d.get('version')} (commit {d.get('source_revision', 'n/a')})",
            f"Platform:           {d.get('platform')}",
            f"Managed Root:       {d.get('managed_root')}",
            f"Executable Shim:    {d.get('shim_path')} (in PATH: {d.get('shim_in_path')})",
            f"Runtime Binary:     {d.get('runtime_binary')} (exists: {d.get('runtime_binary_exists')})",
            f"Python Runtime:     {d.get('python_path')} ({d.get('python_version')})",
            f"State Directory:    {d.get('state_dir')} (exists: {d.get('state_dir_exists')})",
            f"Database Path:      {d.get('db_path')} (exists: {d.get('db_exists')}, size: {d.get('db_size_bytes', 0)} bytes)",
            f"Config Directory:   {d.get('config_dir')}",
            f"Cache Directory:    {d.get('cache_dir')}",
            f"Backend Readiness:  {d.get('backend_readiness')}",
        ]

        shadowing = d.get("shadowing", [])
        if shadowing:
            lines.append("\n[!] Shadowing Warnings:")
            for s in shadowing:
                lines.append(f"  - {s.get('detail')}")
                lines.append(f"    Repair: {s.get('repair')}")

        issues = d.get("issues", [])
        if issues:
            lines.append("\n[!] Issues Detected:")
            for issue in issues:
                lines.append(f"  - {issue}")
        else:
            lines.append("\n[OK] Fixer MCP environment is ready.")

        return "\n".join(lines)


def run_doctor(
    managed_root: Optional[str] = None,
    state_dir: Optional[str] = None,
    config_dir: Optional[str] = None,
    cache_dir: Optional[str] = None,
    user_bin_dir: Optional[str] = None,
    db_path: Optional[str] = None,
) -> DoctorReport:
    """Run comprehensive diagnostic checks on Fixer MCP environment."""
    m_root = resolve_managed_root(managed_root)
    s_dir = resolve_state_dir(state_dir)
    c_dir = resolve_config_dir(config_dir)
    ca_dir = resolve_cache_dir(cache_dir)
    b_dir = resolve_user_bin_dir(user_bin_dir)
    d_path = resolve_db_path(s_dir, db_path)

    issues: List[str] = []

    # Check mode and metadata
    is_dev = is_development_checkout(m_root)
    meta = InstallMetadata.load(m_root)
    mode = "development" if is_dev else (meta.mode if meta else "uninitialized")
    version = meta.version if meta else ("dev" if is_dev else "unknown")
    revision = meta.source_revision[:8] if (meta and meta.source_revision) else "unknown"

    # Check runtime binary
    current_link = current_link_path(m_root)
    candidate_binary = os.path.join(current_link, "fixer_mcp", "fixer_mcp")
    if not os.path.isfile(candidate_binary):
        candidate_binary = os.path.join(current_link, "payload", "fixer_mcp", "fixer_mcp")
    if is_dev and not os.path.isfile(candidate_binary):
        candidate_binary = os.path.join(m_root, "fixer_mcp")

    runtime_binary_exists = os.path.isfile(candidate_binary)
    if not runtime_binary_exists and not is_dev:
        issues.append(f"Runtime binary not found at {candidate_binary}")

    # Check shim
    shim_path = os.path.join(b_dir, "fixer")
    resolved_fixer = shutil.which("fixer")
    shim_in_path = resolved_fixer is not None and os.path.abspath(resolved_fixer) == os.path.abspath(shim_path)

    shadowing = detect_shadowing(shim_path)

    # Check DB
    db_exists = os.path.isfile(d_path)
    db_size = os.path.getsize(d_path) if db_exists else 0
    backend_ready = "ready"
    if db_exists:
        try:
            conn = sqlite3.connect(d_path, timeout=1.0)
            with conn:
                cur = conn.cursor()
                cur.execute("SELECT count(*) FROM sqlite_master WHERE type='table'")
                _ = cur.fetchone()[0]
            conn.close()
        except Exception as e:
            backend_ready = f"database error ({e})"
            issues.append(f"Database error at {d_path}: {e}")
    else:
        backend_ready = "uninitialized (will initialize on first launch)"

    data: Dict[str, Any] = {
        "mode": mode,
        "version": version,
        "source_revision": revision,
        "platform": meta.platform if meta else platform.platform(),
        "managed_root": m_root,
        "shim_path": shim_path,
        "shim_in_path": shim_in_path,
        "runtime_binary": candidate_binary,
        "runtime_binary_exists": runtime_binary_exists,
        "python_path": sys.executable,
        "python_version": sys.version.split()[0],
        "state_dir": s_dir,
        "state_dir_exists": os.path.isdir(s_dir),
        "db_path": d_path,
        "db_exists": db_exists,
        "db_size_bytes": db_size,
        "config_dir": c_dir,
        "cache_dir": ca_dir,
        "backend_readiness": backend_ready,
        "shadowing": shadowing,
        "issues": issues,
    }

    return DoctorReport(data)
