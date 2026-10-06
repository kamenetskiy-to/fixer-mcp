"""SQLite/storage helpers for the Fixer wire launcher."""

from __future__ import annotations

import hashlib
import os
import shutil
import sqlite3
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Callable, Sequence

from client_wires.backends import (
    DEFAULT_BACKEND,
    available_backend_descriptors,
    is_codex_backend,
    normalize_backend_name,
)
from client_wires.backends.antigravity_adapter import (
    normalize_antigravity_model_alias,
    normalize_antigravity_reasoning_alias,
)
from client_wires.backends.droid_adapter import normalize_droid_model_alias

FIXER_DB_PATH_ENV = "FIXER_DB_PATH"
PRIMARY_FIXER_DB_FILENAME = "fixer.db"
UNATTACHED_FIXER_PROJECT_NAME = "Unattached Fixer"
FIGMA_CONSOLE_MCP_NAME = "figma-console-mcp"
FIGMA_CONSOLE_MCP_FALLBACK_CATEGORY = "Design"
FIGMA_CONSOLE_MCP_FALLBACK_HOW_TO = (
    "Use for Figma design-system extraction, creation, and debugging workflows across "
    "components, variables, and layout iteration."
)
WIRE_DB_BUSY_TIMEOUT_MS = 30_000


@dataclass(frozen=True)
class SessionRow:
    session_id: int
    global_session_id: int
    task_description: str
    status: str
    cli_backend: str = DEFAULT_BACKEND
    cli_model: str = ""
    cli_reasoning: str = ""
    external_session_id: str = ""
    codex_session_id: str = ""

    def __post_init__(self) -> None:
        normalized_backend = normalize_backend_name(self.cli_backend)
        object.__setattr__(self, "cli_backend", normalized_backend)
        resolved_external_session_id = self.external_session_id.strip() or self.codex_session_id.strip()
        object.__setattr__(self, "external_session_id", resolved_external_session_id)
        legacy_codex_session_id = self.codex_session_id.strip()
        if not legacy_codex_session_id and is_codex_backend(normalized_backend):
            legacy_codex_session_id = resolved_external_session_id
        object.__setattr__(self, "codex_session_id", legacy_codex_session_id)


@dataclass(frozen=True)
class SessionLaunchSelection:
    backend: str
    model: str
    reasoning: str


@dataclass(frozen=True)
class RegistryMcpMetadata:
    is_default: bool
    category: str
    how_to: str


def _dedupe_link_table(conn: sqlite3.Connection, table_name: str, partition_by: Sequence[str]) -> None:
    partition_sql = ", ".join(partition_by)
    try:
        conn.execute(
            f"""
            DELETE FROM {table_name}
            WHERE id IN (
                SELECT id
                FROM (
                    SELECT
                        id,
                        ROW_NUMBER() OVER (
                            PARTITION BY {partition_sql}
                            ORDER BY COALESCE(updated_at, '') DESC, id DESC
                        ) AS row_rank
                    FROM {table_name}
                )
                WHERE row_rank > 1
            )
            """
        )
    except sqlite3.OperationalError:
        grouped_columns = ", ".join(partition_by)
        conn.execute(
            f"""
            DELETE FROM {table_name}
            WHERE id NOT IN (
                SELECT MAX(id)
                FROM {table_name}
                GROUP BY {grouped_columns}
            )
            """
        )


def _ensure_wire_schema(conn: sqlite3.Connection) -> None:
    # Schema repair is called by every launcher entrypoint. Give concurrent
    # Fixer MCP writers a reasonable window to finish instead of failing on a
    # transient lock, and always finish our own migration transaction before
    # returning. In particular, callers may prompt for onboarding immediately
    # afterwards; keeping this transaction open across input() can pin the
    # whole launcher database indefinitely.
    conn.execute(f"PRAGMA busy_timeout = {WIRE_DB_BUSY_TIMEOUT_MS}")
    conn.executescript(
        """
        CREATE TABLE IF NOT EXISTS session_external_link (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            session_id INTEGER NOT NULL,
            backend TEXT NOT NULL,
            external_session_id TEXT NOT NULL,
            updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
            FOREIGN KEY(session_id) REFERENCES session(id) ON DELETE CASCADE ON UPDATE NO ACTION
        );
        CREATE TABLE IF NOT EXISTS session_codex_link (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            session_id INTEGER NOT NULL UNIQUE,
            codex_session_id TEXT NOT NULL,
            updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
            FOREIGN KEY(session_id) REFERENCES session(id) ON DELETE CASCADE ON UPDATE NO ACTION
        );
        CREATE TABLE IF NOT EXISTS project_mcp_server (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            project_id INTEGER NOT NULL,
            mcp_server_id INTEGER NOT NULL,
            created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
            UNIQUE(project_id, mcp_server_id),
            FOREIGN KEY(project_id) REFERENCES project(id) ON DELETE CASCADE ON UPDATE NO ACTION,
            FOREIGN KEY(mcp_server_id) REFERENCES mcp_server(id) ON DELETE CASCADE ON UPDATE NO ACTION
        );
        CREATE TABLE IF NOT EXISTS fixer_resume_session_alias (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            project_id INTEGER NOT NULL,
            codex_session_id TEXT NOT NULL,
            note TEXT NOT NULL DEFAULT '',
            created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
            UNIQUE(project_id, codex_session_id),
            FOREIGN KEY(project_id) REFERENCES project(id) ON DELETE CASCADE ON UPDATE NO ACTION
        );
        """
    )
    has_session_table = conn.execute(
        "SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = 'session'"
    ).fetchone() is not None

    if has_session_table:
        try:
            conn.execute("ALTER TABLE session ADD COLUMN cli_backend TEXT NOT NULL DEFAULT 'codex'")
        except sqlite3.OperationalError:
            pass
        try:
            conn.execute("ALTER TABLE session ADD COLUMN cli_model TEXT NOT NULL DEFAULT 'gpt-5.6-luna'")
        except sqlite3.OperationalError:
            pass
        try:
            conn.execute("ALTER TABLE session ADD COLUMN cli_reasoning TEXT NOT NULL DEFAULT 'high'")
        except sqlite3.OperationalError:
            pass

        conn.execute("UPDATE session SET cli_backend = 'codex' WHERE COALESCE(TRIM(cli_backend), '') = ''")
        conn.execute("UPDATE session SET cli_model = '' WHERE cli_model IS NULL")
        conn.execute("UPDATE session SET cli_reasoning = '' WHERE cli_reasoning IS NULL")

    _dedupe_link_table(conn, "session_codex_link", ("session_id",))
    _dedupe_link_table(conn, "session_external_link", ("session_id", "backend"))

    if has_session_table:
        conn.execute(
            """
            INSERT INTO session_external_link (session_id, backend, external_session_id, updated_at)
            SELECT legacy.session_id, 'codex', legacy.codex_session_id, COALESCE(legacy.updated_at, CURRENT_TIMESTAMP)
            FROM session_codex_link AS legacy
            LEFT JOIN session_external_link AS external_link
                ON external_link.session_id = legacy.session_id
               AND external_link.backend = 'codex'
            WHERE external_link.id IS NULL
            """
        )
    conn.execute(
        """
        CREATE UNIQUE INDEX IF NOT EXISTS idx_session_codex_link_session_id
        ON session_codex_link(session_id)
        """
    )
    conn.execute(
        """
        CREATE UNIQUE INDEX IF NOT EXISTS idx_session_external_link_session_backend
        ON session_external_link(session_id, backend)
        """
    )
    conn.execute(
        """
        CREATE INDEX IF NOT EXISTS idx_session_external_link_backend
        ON session_external_link(backend)
        """
    )
    conn.execute(
        """
        CREATE INDEX IF NOT EXISTS idx_fixer_resume_session_alias_project_id
        ON fixer_resume_session_alias(project_id)
        """
    )
    conn.commit()


MANAGED_STATE_SUBDIR = "fixer-client-wires"


def _host_canonical_state_db_candidates() -> list[Path]:
    """Host canonical Fixer state database paths, highest priority first.

    Mirrors the Go console (``control_plane/internal/mcpclient.dbPath``) and the
    installer's state layout: an explicit ``FIXER_STATE_DIR`` wins over
    ``$XDG_STATE_HOME``, which wins over the private directory under the user's
    home. The managed runtime root is deliberately not part of this list: its
    ``fixer_mcp/fixer.db`` shadowed the true canonical state on real machines.
    """
    candidates: list[Path] = []
    state_dir = os.environ.get("FIXER_STATE_DIR")
    if state_dir and state_dir.strip():
        candidates.append(Path(state_dir.strip()).expanduser() / PRIMARY_FIXER_DB_FILENAME)
    xdg_state = os.environ.get("XDG_STATE_HOME")
    if xdg_state and xdg_state.strip():
        candidates.append(
            Path(xdg_state.strip()).expanduser() / MANAGED_STATE_SUBDIR / PRIMARY_FIXER_DB_FILENAME
        )
    candidates.append(
        Path(os.path.expanduser("~"))
        / ".local"
        / "state"
        / MANAGED_STATE_SUBDIR
        / PRIMARY_FIXER_DB_FILENAME
    )

    seen: set[Path] = set()
    deduped: list[Path] = []
    for candidate in candidates:
        resolved = candidate.resolve()
        if resolved in seen:
            continue
        seen.add(resolved)
        deduped.append(resolved)
    return deduped


def _find_stray_bare_db(cwd: Path, *, repo_root: Path) -> Path | None:
    """A bare ``fixer.db`` next to the checkout or cwd is a stray, never canonical.

    These files used to be silent resolution candidates; a stray project-root
    database then captured the launcher and showed a completely empty project
    world while the real state lived elsewhere. Return the first stray so it can
    be migrated once or reported, never selected directly.
    """
    candidates = [
        (repo_root / PRIMARY_FIXER_DB_FILENAME).resolve(),
        (cwd / PRIMARY_FIXER_DB_FILENAME).resolve(),
    ]
    seen: set[Path] = set()
    for candidate in candidates:
        if candidate in seen:
            continue
        seen.add(candidate)
        if candidate.is_file():
            return candidate
    return None


def _ensure_state_parent(path: Path) -> None:
    parent = path.parent
    parent.mkdir(parents=True, exist_ok=True)
    try:
        os.chmod(parent, 0o700)
    except OSError:
        pass


def _migrate_stray_bare_db(target: Path, *, cwd: Path, repo_root: Path) -> None:
    stray = _find_stray_bare_db(cwd, repo_root=repo_root)
    if stray is None:
        return
    _ensure_state_parent(target)
    try:
        os.replace(stray, target)
    except OSError:
        # Cross-device (or otherwise non-atomic) move: copy then drop the stray
        # so the operator's data survives exactly once.
        shutil.copy2(stray, target)
        stray.unlink()
    print(f"[fixer-wire] migrated stray {stray} -> {target}", file=sys.stderr)


def _warn_ignored_stray(target: Path, *, cwd: Path, repo_root: Path) -> None:
    stray = _find_stray_bare_db(cwd, repo_root=repo_root)
    if stray is None:
        return
    print(
        f"[fixer-wire] stray {stray} ignored (canonical {target} already exists). "
        f"Adopt it explicitly with {FIXER_DB_PATH_ENV}={stray}",
        file=sys.stderr,
    )


def _resolve_fixer_db_path(cwd: Path, *, repo_root: Path) -> Path:
    from_env = os.environ.get(FIXER_DB_PATH_ENV)
    if from_env and from_env.strip():
        env_path = Path(from_env.strip()).expanduser()
        if not env_path.is_absolute():
            env_path = repo_root / env_path
        # An explicit path is authoritative even before the database exists and
        # is never migrated or overridden by a stray. The Go schema bootstrap
        # creates the file.
        return env_path.resolve()

    # Rule 2: the source-checkout project database is canonical for that
    # checkout and beats any host state copy.
    project_db = (repo_root / "fixer_mcp" / PRIMARY_FIXER_DB_FILENAME).resolve()
    if project_db.is_file():
        return project_db

    # Rule 3: the host canonical state database. Bare fixer.db files at the
    # repo root or cwd must never shadow it. One is migrated once when no
    # canonical state exists yet, otherwise it is left untouched and reported
    # for explicit adoption via FIXER_DB_PATH.
    state_candidates = _host_canonical_state_db_candidates()
    if not state_candidates:
        raise RuntimeError(
            f"Could not locate {PRIMARY_FIXER_DB_FILENAME}. Set {FIXER_DB_PATH_ENV} to override."
        )
    for candidate in state_candidates:
        if candidate.is_file():
            _warn_ignored_stray(candidate, cwd=cwd, repo_root=repo_root)
            return candidate

    preferred = state_candidates[0]
    _ensure_state_parent(preferred)
    _migrate_stray_bare_db(preferred, cwd=cwd, repo_root=repo_root)
    return preferred


def _resolve_fixer_db_path_readonly(cwd: Path, *, repo_root: Path) -> tuple[Path, str]:
    """Side-effect-free mirror of :func:`_resolve_fixer_db_path`.

    Diagnostics must never migrate a stray or touch either database: this
    returns the path the launcher WOULD pick together with its authority
    source, leaving both databases exactly as they are.
    """
    from_env = os.environ.get(FIXER_DB_PATH_ENV)
    if from_env and from_env.strip():
        env_path = Path(from_env.strip()).expanduser()
        if not env_path.is_absolute():
            env_path = repo_root / env_path
        return env_path.resolve(), "explicit_override"
    project_db = (repo_root / "fixer_mcp" / PRIMARY_FIXER_DB_FILENAME).resolve()
    if project_db.is_file():
        return project_db, "checkout_project"
    for candidate in _host_canonical_state_db_candidates():
        if candidate.is_file():
            return candidate, "host_canonical"
    return _host_canonical_state_db_candidates()[0], "host_canonical"


def _db_fingerprint(path: Path) -> dict[str, Any]:
    """Read-only content fingerprint of one database (never writes to it)."""
    resolved = path.expanduser().resolve()
    fingerprint: dict[str, Any] = {"path": str(resolved), "exists": resolved.is_file()}
    if not resolved.is_file():
        return fingerprint
    fingerprint["size_bytes"] = resolved.stat().st_size
    schema_parts: list[str] = []
    counts: dict[str, int | None] = {}
    try:
        conn = sqlite3.connect(f"file:{resolved}?mode=ro", uri=True, timeout=WIRE_DB_BUSY_TIMEOUT_MS / 1000)
        try:
            for name, ddl in conn.execute(
                "SELECT COALESCE(name, ''), COALESCE(sql, '') FROM sqlite_master ORDER BY type, name"
            ):
                schema_parts.append(f"{name}\0{ddl or ''}")
            for table in ("project", "session", "backlog_item", "hands_instruction"):
                try:
                    counts[table] = conn.execute(f"SELECT COUNT(*) FROM {table}").fetchone()[0]
                except sqlite3.Error:
                    counts[table] = None
        finally:
            conn.close()
    except sqlite3.Error as exc:
        fingerprint["read_error"] = f"{type(exc).__name__}: {exc}"
        return fingerprint
    fingerprint["schema_sha256"] = hashlib.sha256("\n".join(schema_parts).encode("utf-8")).hexdigest()[:16]
    fingerprint["counts"] = counts
    return fingerprint


def _fingerprint_divergence(
    left_name: str,
    left: dict[str, Any],
    right_name: str,
    right: dict[str, Any],
) -> list[str]:
    lines: list[str] = []
    if left.get("schema_sha256") != right.get("schema_sha256"):
        lines.append(
            f"schema divergence: {left_name}={left.get('schema_sha256')} {right_name}={right.get('schema_sha256')}"
        )
    left_counts = left.get("counts") or {}
    right_counts = right.get("counts") or {}
    for table in ("project", "session", "backlog_item", "hands_instruction"):
        if left_counts.get(table) != right_counts.get(table):
            lines.append(
                f"data divergence in {table}: {left_name}={left_counts.get(table)} {right_name}={right_counts.get(table)}"
            )
    return lines


def describe_db_authority(cwd: Path, *, repo_root: Path) -> dict[str, Any]:
    """Explicit DB authority plus accidental-split diagnostics.

    The host canonical state database is the default authority; an intentional
    ``FIXER_DB_PATH`` override (tests/dev/probes) is sanctioned and respected.
    A bare ``fixer.db`` next to the cwd or checkout while another database is
    live is an accidental per-cwd split: both databases are preserved verbatim
    and reported with separate fingerprints and data/canon divergence. Nothing
    is merged, no IDs are rewritten, and no checkout history is deleted.
    """
    effective, source = _resolve_fixer_db_path_readonly(cwd, repo_root=repo_root)
    stray = _find_stray_bare_db(cwd, repo_root=repo_root)
    host_canonical = next(
        (candidate for candidate in _host_canonical_state_db_candidates() if candidate.is_file()),
        None,
    )
    fingerprints: dict[str, dict[str, Any]] = {"effective": _db_fingerprint(effective)}
    divergence: list[str] = []
    accidental_split = False
    if stray is not None and stray != effective:
        stray_fingerprint = _db_fingerprint(stray)
        fingerprints["stray"] = stray_fingerprint
        accidental_split = True
        divergence.extend(
            _fingerprint_divergence("effective", fingerprints["effective"], "stray", stray_fingerprint)
        )
    if host_canonical is not None and host_canonical != effective and host_canonical != stray:
        canonical_fingerprint = _db_fingerprint(host_canonical)
        fingerprints["host_canonical"] = canonical_fingerprint
        divergence.extend(
            _fingerprint_divergence(
                "effective", fingerprints["effective"], "host_canonical", canonical_fingerprint
            )
        )
    return {
        "effective_db": str(effective),
        "effective_source": source,
        "sanctioned_override": source == "explicit_override",
        "host_canonical_db": str(host_canonical) if host_canonical is not None else "",
        "stray_db": str(stray) if stray is not None else "",
        "accidental_split": accidental_split,
        "fingerprints": fingerprints,
        "data_divergence": divergence,
        "preserved": True,
        "note": "both databases preserved verbatim; no merge, no ID rewrites, no history deletion",
    }


def _default_project_name(cwd: Path) -> str:
    return cwd.name or "project"


def _resolve_project_id(conn: sqlite3.Connection, cwd: Path) -> int:
    cwd_text = str(cwd.resolve())
    row = conn.execute(
        """
        SELECT id
        FROM project
        WHERE cwd = ? OR ? LIKE cwd || '/%'
        ORDER BY LENGTH(cwd) DESC
        LIMIT 1
        """,
        (cwd_text, cwd_text),
    ).fetchone()
    if row is None:
        raise RuntimeError(f"No project entry found in {PRIMARY_FIXER_DB_FILENAME} for cwd: {cwd_text}")
    return int(row[0])


def _read_onboarding_project_name(
    cwd: Path,
    *,
    name_reader: Callable[[str], str] | None = None,
) -> str:
    default_name = _default_project_name(cwd)
    prompt = f"[fixer-wire] Register project name for {cwd.resolve()} [{default_name}]: "
    try:
        raw_name = (name_reader or input)(prompt).strip()
    except (EOFError, KeyboardInterrupt) as err:
        raise RuntimeError(f"Project onboarding cancelled for cwd: {cwd.resolve()}") from err
    if raw_name.lower() in {"q", "quit", "exit", "cancel"}:
        raise RuntimeError(f"Project onboarding cancelled for cwd: {cwd.resolve()}")
    return raw_name or default_name


def _ensure_project_registered(
    conn: sqlite3.Connection,
    cwd: Path,
    *,
    name_reader: Callable[[str], str] | None = None,
    resolve_project_id: Callable[[sqlite3.Connection, Path], int] = _resolve_project_id,
    read_onboarding_project_name: Callable[..., str] = _read_onboarding_project_name,
) -> int:
    try:
        return resolve_project_id(conn, cwd)
    except RuntimeError:
        pass

    cwd_text = str(cwd.resolve())
    project_name = read_onboarding_project_name(cwd, name_reader=name_reader)
    try:
        cursor = conn.execute(
            "INSERT INTO project (name, cwd) VALUES (?, ?)",
            (project_name, cwd_text),
        )
        conn.commit()
    except sqlite3.IntegrityError:
        try:
            return resolve_project_id(conn, cwd)
        except RuntimeError as lookup_err:
            raise RuntimeError(f"Project onboarding failed for cwd: {cwd_text}") from lookup_err
    project_id = int(cursor.lastrowid)
    print(f"[fixer-wire] registered project '{project_name}' for cwd: {cwd_text}")
    return project_id


def _ensure_unattached_fixer_project(
    conn: sqlite3.Connection,
    *,
    scratch_cwd: Path | None = None,
    resolve_unattached_fixer_cwd: Callable[[], Path],
    bootstrap_project_mcp_bindings: Callable[[sqlite3.Connection, int], None],
) -> tuple[int, Path]:
    cwd = (scratch_cwd or resolve_unattached_fixer_cwd()).expanduser().resolve()
    cwd.mkdir(parents=True, exist_ok=True)
    cwd_text = str(cwd)

    row = conn.execute("SELECT id FROM project WHERE cwd = ? LIMIT 1", (cwd_text,)).fetchone()
    if row is None:
        cursor = conn.execute(
            "INSERT INTO project (name, cwd) VALUES (?, ?)",
            (UNATTACHED_FIXER_PROJECT_NAME, cwd_text),
        )
        conn.commit()
        project_id = int(cursor.lastrowid)
        print(f"[fixer-wire] registered internal '{UNATTACHED_FIXER_PROJECT_NAME}' project for cwd: {cwd_text}")
    else:
        project_id = int(row[0])

    try:
        bootstrap_project_mcp_bindings(conn, project_id)
    except sqlite3.OperationalError:
        pass
    return project_id, cwd


def _assert_project_is_registered(
    cwd: Path,
    *,
    resolve_fixer_db_path: Callable[[Path], Path],
    ensure_wire_schema: Callable[[sqlite3.Connection], None],
    ensure_project_registered: Callable[[sqlite3.Connection, Path], int],
) -> None:
    db_path = resolve_fixer_db_path(cwd)
    conn = sqlite3.connect(db_path)
    try:
        ensure_wire_schema(conn)
        ensure_project_registered(conn, cwd)
    finally:
        conn.close()


def _load_session_rows(conn: sqlite3.Connection, project_id: int) -> list[SessionRow]:
    rows = conn.execute(
        """
        SELECT
            s.id,
            (
                SELECT COUNT(*)
                FROM session s2
                WHERE s2.project_id = s.project_id AND s2.id <= s.id
            ) AS local_session_id,
            s.task_description,
            s.status,
            COALESCE(
                (
                    SELECT sel.external_session_id
                    FROM session_external_link sel
                    WHERE sel.session_id = s.id
                      AND sel.backend = COALESCE(NULLIF(TRIM(s.cli_backend), ''), 'codex')
                    ORDER BY COALESCE(sel.updated_at, '') DESC, sel.id DESC
                    LIMIT 1
                ),
                CASE
                    WHEN COALESCE(NULLIF(TRIM(s.cli_backend), ''), 'codex') = 'codex'
                    THEN (
                        SELECT sc.codex_session_id
                        FROM session_codex_link sc
                        WHERE sc.session_id = s.id
                        ORDER BY COALESCE(sc.updated_at, '') DESC, sc.id DESC
                        LIMIT 1
                    )
                    ELSE ''
                END,
                ''
            ),
            COALESCE(NULLIF(TRIM(s.cli_backend), ''), 'codex'),
            COALESCE(s.cli_model, ''),
            COALESCE(s.cli_reasoning, '')
        FROM session s
        WHERE s.project_id = ?
        ORDER BY
          CASE s.status
            WHEN 'in_progress' THEN 0
            WHEN 'pending' THEN 1
            WHEN 'review' THEN 2
            WHEN 'completed' THEN 3
            ELSE 3
          END,
          s.id ASC
        """,
        (project_id,),
    ).fetchall()
    return [
        SessionRow(
            session_id=int(row[1]),
            global_session_id=int(row[0]),
            task_description=str(row[2]),
            status=str(row[3]),
            cli_backend=str(row[5]),
            cli_model=str(row[6]),
            cli_reasoning=str(row[7]),
            external_session_id=str(row[4]),
            codex_session_id=str(row[4]) if is_codex_backend(str(row[5])) else "",
        )
        for row in rows
    ]


def _registry_metadata_with_fallback(
    name: str,
    metadata: RegistryMcpMetadata | None,
) -> RegistryMcpMetadata | None:
    if name != FIGMA_CONSOLE_MCP_NAME:
        return metadata
    if metadata is None:
        return RegistryMcpMetadata(
            is_default=False,
            category=FIGMA_CONSOLE_MCP_FALLBACK_CATEGORY,
            how_to=FIGMA_CONSOLE_MCP_FALLBACK_HOW_TO,
        )
    category = metadata.category.strip() or FIGMA_CONSOLE_MCP_FALLBACK_CATEGORY
    how_to = metadata.how_to.strip() or FIGMA_CONSOLE_MCP_FALLBACK_HOW_TO
    if category == metadata.category and how_to == metadata.how_to:
        return metadata
    return RegistryMcpMetadata(
        is_default=metadata.is_default,
        category=category,
        how_to=how_to,
    )


def _load_registry_mcp_names(
    conn: sqlite3.Connection,
    *,
    load_registry_mcp_metadata: Callable[[sqlite3.Connection], dict[str, RegistryMcpMetadata]],
) -> list[str]:
    return sorted(load_registry_mcp_metadata(conn).keys())


def _load_registry_mcp_metadata(conn: sqlite3.Connection) -> dict[str, RegistryMcpMetadata]:
    try:
        rows = conn.execute(
            """
            SELECT name, COALESCE(is_default, 0), COALESCE(category, ''), COALESCE(how_to, '')
            FROM mcp_server
            ORDER BY name
            """
        ).fetchall()
        return {
            str(name): RegistryMcpMetadata(
                is_default=bool(is_default),
                category=str(category),
                how_to=str(how_to),
            )
            for name, is_default, category, how_to in rows
        }
    except sqlite3.OperationalError:
        rows = conn.execute("SELECT name FROM mcp_server ORDER BY name").fetchall()
        return {
            str(row[0]): RegistryMcpMetadata(
                is_default=False,
                category="",
                how_to="",
            )
            for row in rows
        }


def _load_assigned_mcp_names(conn: sqlite3.Connection, session_id: int) -> list[str]:
    rows = conn.execute(
        """
        SELECT s.name
        FROM session_mcp_server sms
        INNER JOIN mcp_server s ON s.id = sms.mcp_server_id
        WHERE sms.session_id = ?
        ORDER BY s.name
        """,
        (session_id,),
    ).fetchall()
    return [str(row[0]) for row in rows]


def _bootstrap_project_mcp_bindings(conn: sqlite3.Connection, project_id: int) -> None:
    row = conn.execute(
        "SELECT COUNT(*) FROM project_mcp_server WHERE project_id = ?",
        (project_id,),
    ).fetchone()
    existing_count = int(row[0]) if row else 0
    if existing_count > 0:
        return

    default_ids = conn.execute(
        "SELECT id FROM mcp_server WHERE COALESCE(is_default, 0) = 1 ORDER BY id"
    ).fetchall()
    assigned_ids = conn.execute(
        """
        SELECT DISTINCT sms.mcp_server_id
        FROM session_mcp_server sms
        INNER JOIN session s ON s.id = sms.session_id
        WHERE s.project_id = ?
        ORDER BY sms.mcp_server_id
        """,
        (project_id,),
    ).fetchall()

    server_ids = {int(row[0]) for row in [*default_ids, *assigned_ids]}
    if not server_ids:
        return

    with conn:
        for server_id in sorted(server_ids):
            conn.execute(
                "INSERT OR IGNORE INTO project_mcp_server (project_id, mcp_server_id) VALUES (?, ?)",
                (project_id, server_id),
            )


def _load_project_allowed_mcp_names(
    conn: sqlite3.Connection,
    project_id: int,
    *,
    bootstrap_project_mcp_bindings: Callable[[sqlite3.Connection, int], None],
    load_registry_mcp_names: Callable[[sqlite3.Connection], list[str]],
) -> list[str]:
    try:
        bootstrap_project_mcp_bindings(conn, project_id)
        rows = conn.execute(
            """
            SELECT s.name
            FROM project_mcp_server pms
            INNER JOIN mcp_server s ON s.id = pms.mcp_server_id
            WHERE pms.project_id = ?
            ORDER BY s.name
            """,
            (project_id,),
        ).fetchall()
    except sqlite3.OperationalError:
        return load_registry_mcp_names(conn)
    return [str(row[0]) for row in rows]


def _load_hands_mcp_names(conn: sqlite3.Connection, project_id: int) -> list[str]:
    try:
        rows = conn.execute(
            """
            SELECT s.name
            FROM project_hands_mcp_server phms
            INNER JOIN mcp_server s ON s.id = phms.mcp_server_id
            WHERE phms.project_id = ?
            ORDER BY s.name
            """,
            (project_id,),
        ).fetchall()
    except sqlite3.OperationalError:
        return []
    return [str(row[0]) for row in rows]


def _load_hands_doc_ids(conn: sqlite3.Connection, project_id: int) -> list[int]:
    try:
        rows = conn.execute(
            """
            SELECT (
                SELECT COUNT(*)
                FROM project_doc d2
                WHERE d2.project_id = d.project_id AND d2.id <= d.id
            )
            FROM project_hands_doc phd
            INNER JOIN project_doc d ON d.id = phd.project_doc_id
            WHERE phd.project_id = ?
            ORDER BY d.id
            """,
            (project_id,),
        ).fetchall()
    except sqlite3.OperationalError:
        return []
    return [int(row[0]) for row in rows]


def _sync_registry_names(
    conn: sqlite3.Connection,
    names: Sequence[str],
    *,
    normalize_names: Callable[[Sequence[str]], list[str]],
) -> None:
    normalized = normalize_names(list(names))
    if not normalized:
        return
    with conn:
        for name in normalized:
            conn.execute("INSERT OR IGNORE INTO mcp_server (name, auto_attach) VALUES (?, 0)", (name,))


def _persist_session_mcp_names(
    conn: sqlite3.Connection,
    session_id: int,
    names: Sequence[str],
    *,
    normalize_names: Callable[[Sequence[str]], list[str]],
    sync_registry_names: Callable[[sqlite3.Connection, Sequence[str]], None],
) -> None:
    normalized = normalize_names(list(names))
    sync_registry_names(conn, normalized)

    with conn:
        conn.execute("DELETE FROM session_mcp_server WHERE session_id = ?", (session_id,))
        for name in normalized:
            row = conn.execute("SELECT id FROM mcp_server WHERE name = ?", (name,)).fetchone()
            if row is None:
                continue
            conn.execute(
                "INSERT OR IGNORE INTO session_mcp_server (session_id, mcp_server_id) VALUES (?, ?)",
                (session_id, int(row[0])),
            )


def _backend_descriptor(backend_name: str) -> Any:
    normalized = normalize_backend_name(backend_name)
    for descriptor in available_backend_descriptors():
        if descriptor.name == normalized:
            return descriptor
    raise RuntimeError(f"Unsupported CLI backend {backend_name!r}.")


def _normalize_backend_model(descriptor: Any, model: str | None) -> str:
    candidate = (model or "").strip() or descriptor.default_model
    if descriptor.name == "droid":
        candidate = normalize_droid_model_alias(candidate)
    if descriptor.name == "antigravity":
        candidate = normalize_antigravity_model_alias(candidate)
    if candidate not in descriptor.model_options:
        if descriptor.name == "antigravity" and "claude" in candidate.lower() and "4.6" in candidate:
            raise RuntimeError(
                f"Unsupported model {candidate!r} (requested {model!r}) for backend {descriptor.name!r}: "
                "retired Claude 4.6 (Thinking) pick. Select a current Claude 5.5 variant "
                "(Claude Opus 5.5 / Claude Sonnet 5.5 with reasoning low, medium or high)."
            )
        supported = ", ".join(descriptor.model_options)
        raise RuntimeError(
            f"Unsupported model {candidate!r} for backend {descriptor.name!r}. Supported models: {supported}"
        )
    return candidate


def _normalize_backend_reasoning(descriptor: Any, reasoning: str | None, model: str | None = None) -> str:
    candidate = (reasoning or "").strip() or descriptor.default_reasoning
    if descriptor.name == "droid" and candidate in {"", "none"}:
        candidate = "high"
    if descriptor.name == "antigravity":
        candidate = candidate.lower()
    if descriptor.name == "pi":
        # Pi's reasoning surface is per model (`thinkingLevelMap`), not per backend.
        # A stored or selected level the model does not declare makes the adapter
        # guard raise at launch time ("does not support reasoning 'low'"), which is
        # the right failure for a genuinely wrong request but a confusing one for a
        # stale saved selection. Correct it to the model's default and say so.
        from client_wires.backends.pi_adapter import pi_supported_thinking_levels

        supported = pi_supported_thinking_levels(model or "")
        if supported:
            if candidate in supported:
                return candidate
            preferred = (
                descriptor.default_reasoning
                if descriptor.default_reasoning in supported
                else supported[-1]
            )
            print(
                f"[fixer-wire] Pi model {model!r} does not support reasoning {candidate!r}; "
                f"using {preferred!r}. Supported: {', '.join(supported)}.",
                file=sys.stderr,
            )
            return preferred
    if candidate not in descriptor.reasoning_options:
        supported = ", ".join(descriptor.reasoning_options)
        raise RuntimeError(
            f"Unsupported reasoning {candidate!r} for backend {descriptor.name!r}. Supported reasoning values: {supported}"
        )
    return candidate


def _load_session_external_id(conn: sqlite3.Connection, session_id: int, backend: str) -> str:
    normalized_backend = normalize_backend_name(backend)
    row = conn.execute(
        """
        SELECT external_session_id
        FROM session_external_link
        WHERE session_id = ? AND backend = ?
        ORDER BY COALESCE(updated_at, '') DESC, id DESC
        LIMIT 1
        """,
        (session_id, normalized_backend),
    ).fetchone()
    if row and str(row[0]).strip():
        return str(row[0]).strip()
    if not is_codex_backend(normalized_backend):
        return ""
    row = conn.execute(
        """
        SELECT codex_session_id
        FROM session_codex_link
        WHERE session_id = ?
        ORDER BY COALESCE(updated_at, '') DESC, id DESC
        LIMIT 1
        """,
        (session_id,),
    ).fetchone()
    if row is None:
        return ""
    return str(row[0]).strip()


def _save_session_external_id(conn: sqlite3.Connection, session_id: int, backend: str, external_session_id: str) -> None:
    normalized_backend = normalize_backend_name(backend)
    resolved_external_session_id = external_session_id.strip()
    if not resolved_external_session_id:
        return
    with conn:
        conn.execute(
            """
            INSERT INTO session_external_link (session_id, backend, external_session_id, updated_at)
            VALUES (?, ?, ?, CURRENT_TIMESTAMP)
            ON CONFLICT(session_id, backend) DO UPDATE SET
                external_session_id = excluded.external_session_id,
                updated_at = CURRENT_TIMESTAMP
            """,
            (session_id, normalized_backend, resolved_external_session_id),
        )
        if is_codex_backend(normalized_backend):
            conn.execute(
                """
                INSERT INTO session_codex_link (session_id, codex_session_id, updated_at)
                VALUES (?, ?, CURRENT_TIMESTAMP)
                ON CONFLICT(session_id) DO UPDATE SET
                    codex_session_id = excluded.codex_session_id,
                    updated_at = CURRENT_TIMESTAMP
                """,
                (session_id, resolved_external_session_id),
            )


def _save_session_codex_id(conn: sqlite3.Connection, session_id: int, codex_session_id: str) -> None:
    _save_session_external_id(conn, session_id, "codex", codex_session_id)


def _persist_session_launch_selection(
    conn: sqlite3.Connection,
    session_row: SessionRow,
    selection: SessionLaunchSelection,
    *,
    backend_descriptor: Callable[[str], Any] = _backend_descriptor,
    normalize_backend_model: Callable[[Any, str | None], str] = _normalize_backend_model,
    normalize_backend_reasoning: Callable[[Any, str | None], str] = _normalize_backend_reasoning,
) -> SessionLaunchSelection:
    normalized_backend = normalize_backend_name(selection.backend)
    descriptor = backend_descriptor(normalized_backend)
    resolved_model = normalize_backend_model(descriptor, selection.model)
    resolved_reasoning = normalize_backend_reasoning(
        descriptor,
        normalize_antigravity_reasoning_alias(selection.model, selection.reasoning)
        if descriptor.name == "antigravity"
        else selection.reasoning,
        resolved_model,
    )
    stored_backend = normalize_backend_name(session_row.cli_backend)
    started = bool(session_row.external_session_id.strip())
    stored_model = ""
    stored_reasoning = ""
    if started:
        stored_model_raw = session_row.cli_model.strip()
        stored_model = normalize_backend_model(descriptor, stored_model_raw) if stored_model_raw else ""
        stored_reasoning_raw = session_row.cli_reasoning.strip()
        if stored_reasoning_raw:
            stored_reasoning_input = (
                normalize_antigravity_reasoning_alias(stored_model_raw, stored_reasoning_raw)
                if descriptor.name == "antigravity"
                else stored_reasoning_raw
            )
            stored_reasoning = normalize_backend_reasoning(descriptor, stored_reasoning_input, stored_model)

    if started and stored_backend != normalized_backend:
        raise RuntimeError(
            f"Session {session_row.session_id} is bound to backend {stored_backend!r}; "
            f"cannot switch to {normalized_backend!r} after launch."
        )
    if started and stored_model and resolved_model and stored_model != resolved_model:
        raise RuntimeError(
            f"Session {session_row.session_id} is bound to model {stored_model!r}; "
            f"cannot switch to {resolved_model!r} after launch."
        )
    if started and stored_reasoning and resolved_reasoning and stored_reasoning != resolved_reasoning:
        raise RuntimeError(
            f"Session {session_row.session_id} is bound to reasoning {stored_reasoning!r}; "
            f"cannot switch to {resolved_reasoning!r} after launch."
        )

    final_backend = stored_backend if started else normalized_backend
    final_model = stored_model if started and stored_model else resolved_model
    final_reasoning = stored_reasoning if started and stored_reasoning else resolved_reasoning

    conn.execute(
        """
        UPDATE session
        SET cli_backend = ?, cli_model = ?, cli_reasoning = ?
        WHERE id = ?
        """,
        (final_backend, final_model, final_reasoning, session_row.global_session_id),
    )
    commit = getattr(conn, "commit", None)
    if callable(commit):
        commit()

    return SessionLaunchSelection(
        backend=final_backend,
        model=final_model,
        reasoning=final_reasoning,
    )


def _load_fixer_resume_alias_session_ids(
    cwd: Path,
    *,
    resolve_fixer_db_path: Callable[[Path], Path],
    ensure_wire_schema: Callable[[sqlite3.Connection], None],
    resolve_project_id: Callable[[sqlite3.Connection, Path], int],
) -> set[str]:
    try:
        db_path = resolve_fixer_db_path(cwd)
    except RuntimeError:
        return set()

    try:
        conn = sqlite3.connect(db_path)
    except sqlite3.Error:
        return set()
    try:
        ensure_wire_schema(conn)
        project_id = resolve_project_id(conn, cwd)
        rows = conn.execute(
            """
            SELECT codex_session_id
            FROM fixer_resume_session_alias
            WHERE project_id = ?
            """,
            (project_id,),
        ).fetchall()
    except (RuntimeError, sqlite3.Error):
        # Resume listing is best-effort: a missing or partially provisioned
        # database (fresh host, no `project` table yet) must read as "no
        # aliases", never abort the whole resume flow.
        return set()
    finally:
        conn.close()

    return {str(row[0]).strip() for row in rows if str(row[0]).strip()}
