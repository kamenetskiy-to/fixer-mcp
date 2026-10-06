"""System diagnostics and health checks for Fixer MCP installations."""

import hashlib
import os
import platform
import shutil
import sqlite3
import subprocess
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


def _db_identity(path: str) -> dict:
    """Read-only identity of one database: absolute path, fingerprint and era.

    The schema era distinguishes the stale pre-1.0.11 (1.0.9-era) schema from
    the current 1.0.11+ schema so stale-schema failures never masquerade as
    current-version failures. Never writes to the inspected database.
    """
    resolved = os.path.abspath(path)
    identity = {
        "path": resolved,
        "exists": os.path.isfile(resolved),
        "schema_era": "unknown",
        "schema_sha256": "",
        "counts": {},
    }
    if not identity["exists"]:
        identity["schema_era"] = "missing"
        return identity
    identity["size_bytes"] = os.path.getsize(resolved)
    schema_parts = []
    has_binary_state = False
    has_binary_state_identity_columns = False
    try:
        conn = sqlite3.connect(f"file:{resolved}?mode=ro", uri=True, timeout=1.0)
        try:
            rows = conn.execute(
                "SELECT COALESCE(name, ''), COALESCE(sql, '') FROM sqlite_master ORDER BY type, name"
            ).fetchall()
            for name, ddl in rows:
                schema_parts.append(f"{name}\0{ddl or ''}")
                if name == "mcp_binary_state":
                    has_binary_state = True
                    if "running_build_id" in (ddl or "") and "running_process_identity" in (ddl or ""):
                        has_binary_state_identity_columns = True
            for table in ("project", "session", "mcp_binary_state"):
                try:
                    identity["counts"][table] = conn.execute(f"SELECT COUNT(*) FROM {table}").fetchone()[0]
                except Exception:  # noqa: BLE001 - diagnostics must never crash
                    identity["counts"][table] = None
        finally:
            conn.close()
    except Exception as exc:  # noqa: BLE001 - diagnostics must never crash
        identity["read_error"] = f"{type(exc).__name__}: {exc}"
        return identity
    identity["schema_sha256"] = hashlib.sha256("\n".join(schema_parts).encode("utf-8")).hexdigest()[:16]
    if not schema_parts:
        identity["schema_era"] = "empty"
    elif has_binary_state_identity_columns:
        identity["schema_era"] = "current-1.0.11"
    elif has_binary_state:
        identity["schema_era"] = "stale-1.0.9"
    else:
        # Schema exists but the runtime bookkeeping table is not bootstrapped
        # yet (a fresh launcher-created database). That is the normal
        # pre-bootstrap state, not a stale 1.0.9-era release schema.
        identity["schema_era"] = "pre-bootstrap"
    return identity


def _runtime_binary_sha(path: str) -> str:
    """Immutable content identity of the runtime binary, when it exists."""
    if not os.path.isfile(path):
        return ""
    digest = hashlib.sha256()
    try:
        with open(path, "rb") as handle:
            for chunk in iter(lambda: handle.read(1024 * 1024), b""):
                digest.update(chunk)
    except OSError:
        return ""
    return digest.hexdigest()[:16]


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
            f"DB Authority:       {d.get('db_authority')}",
            f"DB Identity:        schema era {d.get('db_schema_era')} (schema {d.get('db_schema_sha256', 'n/a')}, fp {d.get('db_data_fingerprint', 'n/a')})",
            f"Runtime Binary ID:  {d.get('runtime_binary_sha') or 'n/a'}",
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


def _launcher_resolved_db_path(release_root: str) -> tuple[str, str]:
    """Ask the launcher's own resolver which database it would use, read-only.

    The probe must never mutate state: the launcher's real resolver can migrate
    a stray ``fixer.db`` and create the state directory (the 2026-10-04
    "unexpected migration" incident class). The probe therefore prefers the
    payload's side-effect-free resolver mirror, falls back to the legacy
    resolver with its known mutating helpers neutralized, and refuses outright
    (exit 42) if the payload still attempts a mutation.

    Returns `(path, error)`; `error` is non-empty when the resolver raised, which
    is exactly the "Could not locate fixer.db" failure the operator would see.
    """
    script = """
import sys
sys.path.insert(0, sys.argv[1])
from pathlib import Path
from client_wires import fixer_wire_db as db

cwd = Path(sys.argv[2])
root = Path(sys.argv[3])
readonly = getattr(db, "_resolve_fixer_db_path_readonly", None)
if readonly is not None:
    print(readonly(cwd, repo_root=root)[0])
    raise SystemExit(0)

# Legacy payload without the read-only mirror: suppress its known mutating
# helpers (stray migration, state-directory creation) so the decision logic
# still runs verbatim while state stays untouched.
db._migrate_stray_bare_db = lambda *args, **kwargs: None
db._ensure_state_parent = lambda *args, **kwargs: None


class _MutationBlocked(RuntimeError):
    pass


def _guard(event, args):
    if event in {
        "os.rename", "os.remove", "os.rmdir", "os.mkdir", "os.makedirs",
        "os.chmod", "os.truncate", "shutil.copyfile", "shutil.copy2",
    }:
        raise _MutationBlocked(event)


sys.addaudithook(_guard)
try:
    print(db._resolve_fixer_db_path(cwd, repo_root=root))
except _MutationBlocked:
    raise SystemExit(42)
"""
    try:
        proc = subprocess.run(
            [sys.executable, "-c", script, release_root, os.path.expanduser("~"), release_root],
            cwd=os.path.expanduser("~"),
            capture_output=True,
            text=True,
            timeout=30,
        )
    except Exception as exc:  # noqa: BLE001 - diagnostics must never crash
        return "", f"{type(exc).__name__}: {exc}"
    if proc.returncode == 42:
        return "", (
            "probe refused a state-mutating resolver step (diagnostics never mutate state; "
            "payload predates side-effect-free probing)"
        )
    if proc.returncode != 0:
        message = (proc.stderr or proc.stdout or "").strip().splitlines()
        tail = message[-1] if message else f"exit code {proc.returncode}"
        stderr_text = proc.stderr or ""
        if "ModuleNotFoundError" in stderr_text or "ImportError" in stderr_text:
            # Not a real release tree (for example a synthetic test payload):
            # there is nothing to compare, and this is not an operator problem.
            return "", ""
        return "", tail
    return proc.stdout.strip(), ""


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

    # The launcher resolves its database independently of the installer's paths.
    # They disagreed once (release 0.3.0: doctor said the state directory, the
    # launcher searched only repository-relative paths and refused to start on a
    # machine without a checkout), so verify they agree here rather than relying
    # on the operator to find out.
    launcher_db, launcher_db_error = "", ""
    if not is_dev and os.path.isdir(current_link):
        launcher_db, launcher_db_error = _launcher_resolved_db_path(current_link)
    if launcher_db_error and "Could not locate" in launcher_db_error:
        # Fresh install: no database exists yet. The report already records
        # that as `uninitialized (will initialize on first launch)`, so it must
        # not be reported as an installation issue and make `fixer doctor` fail.
        launcher_db, launcher_db_error = "", ""
    if launcher_db_error:
        issues.append(f"Launcher database resolution failed: {launcher_db_error}")
    elif launcher_db and os.path.realpath(launcher_db) != os.path.realpath(d_path):
        # Two databases diverged (data/canon split). Report both identities and
        # preserve both files; never merge, never rewrite IDs. Comparison is on
        # realpath so a sanctioned override reached through a path alias (macOS
        # /tmp -> /private/tmp, symlinked state dirs) is never a false split.
        launcher_identity = _db_identity(launcher_db)
        doctor_identity_for_divergence = _db_identity(d_path)
        issues.append(
            "Launcher would use a different database than this report (both preserved, no merge): "
            f"launcher={launcher_db} (schema {launcher_identity.get('schema_sha256')}, "
            f"counts {launcher_identity.get('counts')}) "
            f"doctor={d_path} (schema {doctor_identity_for_divergence.get('schema_sha256')}, "
            f"counts {doctor_identity_for_divergence.get('counts')})"
        )

    # Check shim
    shim_path = os.path.join(b_dir, "fixer")
    resolved_fixer = shutil.which("fixer")
    shim_in_path = resolved_fixer is not None and os.path.abspath(resolved_fixer) == os.path.abspath(shim_path)

    shadowing = detect_shadowing(shim_path)

    # DB identity: absolute path, authority (sanctioned override vs state
    # canonical) and schema era (stale 1.0.9 vs current 1.0.11).
    db_identity = _db_identity(d_path)
    override_requested = bool(db_path) or bool(os.environ.get("FIXER_DB_PATH", "").strip())
    db_authority = "explicit_override (sanctioned)" if override_requested else "host_canonical (default)"
    if db_identity.get("schema_era") == "stale-1.0.9":
        issues.append(
            f"Database schema at {db_identity['path']} is stale 1.0.9-era; the current 1.0.11+ "
            "runtime expects the post-1.0.11 identity columns. Reconnect a freshly installed "
            "current binary so migrations run; do not edit rows manually."
        )
    if db_identity.get("read_error"):
        issues.append(f"Database identity probe failed at {db_identity['path']}: {db_identity['read_error']}")

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
        "db_authority": db_authority,
        "db_schema_era": db_identity.get("schema_era"),
        "db_schema_sha256": db_identity.get("schema_sha256", ""),
        "db_data_fingerprint": ",".join(
            f"{table}:{count}" for table, count in sorted((db_identity.get("counts") or {}).items())
        ),
        "db_identity": db_identity,
        "runtime_binary_sha": _runtime_binary_sha(candidate_binary),
        "config_dir": c_dir,
        "cache_dir": ca_dir,
        "backend_readiness": backend_ready,
        "shadowing": shadowing,
        "issues": issues,
    }

    return DoctorReport(data)
