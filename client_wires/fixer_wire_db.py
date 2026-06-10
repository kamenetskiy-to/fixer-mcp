"""SQLite/storage helpers for the Fixer wire launcher."""

from __future__ import annotations

import os
import sqlite3
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Callable, Sequence

from client_wires.backends import (
    DEFAULT_BACKEND,
    available_backend_descriptors,
    normalize_backend_name,
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
        if not legacy_codex_session_id and normalized_backend == "codex":
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
            conn.execute("ALTER TABLE session ADD COLUMN cli_model TEXT NOT NULL DEFAULT ''")
        except sqlite3.OperationalError:
            pass
        try:
            conn.execute("ALTER TABLE session ADD COLUMN cli_reasoning TEXT NOT NULL DEFAULT ''")
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


def _resolve_fixer_db_path(cwd: Path, *, repo_root: Path) -> Path:
    candidates: list[Path] = []

    from_env = os.environ.get(FIXER_DB_PATH_ENV)
    if from_env:
        env_path = Path(from_env).expanduser()
        if not env_path.is_absolute():
            env_path = repo_root / env_path
        candidates.append(env_path)

    candidates.extend(
        [
            repo_root / "fixer_mcp" / PRIMARY_FIXER_DB_FILENAME,
            repo_root / PRIMARY_FIXER_DB_FILENAME,
            cwd / "fixer_mcp" / PRIMARY_FIXER_DB_FILENAME,
            cwd / PRIMARY_FIXER_DB_FILENAME,
        ]
    )

    checked: list[Path] = []
    seen: set[Path] = set()
    for candidate in candidates:
        resolved = candidate.resolve()
        if resolved in seen:
            continue
        seen.add(resolved)
        checked.append(resolved)
        if resolved.is_file():
            return resolved

    checked_text = ", ".join(str(path) for path in checked)
    raise RuntimeError(
        f"Could not locate {PRIMARY_FIXER_DB_FILENAME}. Checked: {checked_text}. "
        f"Set {FIXER_DB_PATH_ENV} to override."
    )


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
            codex_session_id=str(row[4]) if str(row[5]) == "codex" else "",
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
    if candidate not in descriptor.model_options:
        supported = ", ".join(descriptor.model_options)
        raise RuntimeError(
            f"Unsupported model {candidate!r} for backend {descriptor.name!r}. Supported models: {supported}"
        )
    return candidate


def _normalize_backend_reasoning(descriptor: Any, reasoning: str | None) -> str:
    candidate = (reasoning or "").strip() or descriptor.default_reasoning
    if descriptor.name == "droid" and candidate in {"", "none"}:
        candidate = "high"
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
    if normalized_backend != "codex":
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
        if normalized_backend == "codex":
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
    resolved_reasoning = normalize_backend_reasoning(descriptor, selection.reasoning)
    stored_backend = normalize_backend_name(session_row.cli_backend)
    started = bool(session_row.external_session_id.strip())
    stored_model = ""
    stored_reasoning = ""
    if started:
        stored_model_raw = session_row.cli_model.strip()
        stored_model = normalize_backend_model(descriptor, stored_model_raw) if stored_model_raw else ""
        stored_reasoning_raw = session_row.cli_reasoning.strip()
        stored_reasoning = normalize_backend_reasoning(descriptor, stored_reasoning_raw) if stored_reasoning_raw else ""

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

    conn = sqlite3.connect(db_path)
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
    except RuntimeError:
        return set()
    finally:
        conn.close()

    return {str(row[0]).strip() for row in rows if str(row[0]).strip()}
