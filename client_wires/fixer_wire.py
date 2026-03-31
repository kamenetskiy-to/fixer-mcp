#!/usr/bin/env python3
"""Canonical Fixer MCP wire entrypoint for fixer/netrunner/overseer launch."""

from __future__ import annotations

import argparse
from contextlib import closing
import json
import os
import re
import shutil
import sqlite3
import subprocess
import sys
import textwrap
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Sequence

try:
    import tomllib  # Python 3.11+
except ModuleNotFoundError:  # pragma: no cover
    import tomli as tomllib  # type: ignore

if __package__ in (None, ""):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from client_wires.bootstrap import bootstrap_codex_pro_import_path, wire_info_lines
from client_wires.backends import (
    DEFAULT_BACKEND,
    available_backend_descriptors,
    get_backend_adapter,
    normalize_backend_name,
)
from client_wires.mvp_scaffold import run_scaffold_cli

ROLE_CHOICES = ("fixer", "netrunner", "overseer")
SCAFFOLD_MVP_ACTION = "__scaffold_mvp__"
FORCED_MCP_SERVER = "fixer_mcp"
HIDDEN_MCP_SERVERS = {FORCED_MCP_SERVER}
MCP_CATEGORY_ORDER = ("DB", "Web-search", "Design", "Productivity", "Coding", "Other")
MCP_FALLBACK_CATEGORY = "Other"
FIGMA_CONSOLE_MCP_NAME = "figma-console-mcp"
FIGMA_CONSOLE_MCP_FALLBACK_CATEGORY = "Design"
FIGMA_CONSOLE_MCP_FALLBACK_HOW_TO = (
    "Use for Figma design-system extraction, creation, and debugging workflows across "
    "components, variables, and layout iteration."
)
RESEARCH_QUERY_MCP_NAME = "research_query_mcp"
PHILOLOGISTS_PROJECT_MARKER = "philologists"
ALWAYS_VISIBLE_MCP_NAMES = {FIGMA_CONSOLE_MCP_NAME}
WEB_STACK_GUIDANCE_MCP_NAMES = {
    "playwright",
    "playwright-mcp",
    "playwright_mcp",
    "chrome-devtools",
    "chrome-devtools-mcp",
    "chrome_devtools_mcp",
    "eslint",
    "eslint-mcp",
    "eslint_mcp",
    "mcp-language-server",
    "mcp_language_server",
}
STANDARD_WEB_STACK_GUIDANCE = (
    "Next.js (App Router)",
    "React + react-dom",
    "TypeScript strict",
    "Tailwind CSS + daisyUI",
    "Framer Motion",
    "react-responsive",
    "eslint + eslint-config-next",
)
RECENTLY_ACTIVE_STATUSES = {"in_progress"}
ARCHIVED_STATUSES = {"review", "completed"}
TOGGLE_ARCHIVED_VALUE = "__toggle_archived__"
FIXER_LAUNCH_NEW = "__fixer_launch_new__"
FIXER_LAUNCH_RESUME = "__fixer_launch_resume__"
FIXER_SKILL_MARKER = "Activate skill `$start-fixer` immediately."
NETRUNNER_SKILL_MARKER = "Activate skill `$manual-resolution` immediately."
LEGACY_NETRUNNER_SKILL_MARKERS = (
    "Activate skill `$start-netrunner` immediately.",
    "Activate skill `$start-netrunner-autonomous` immediately.",
)
OVERSEER_SKILL_MARKER = "Activate skill `$start-overseer` immediately."
FIXER_DB_PATH_ENV = "FIXER_DB_PATH"
PRIMARY_FIXER_DB_FILENAME = "fixer.db"
WEB_MCP_CONFIG_FILENAME = "webMCP.toml"
FIXER_MCP_AUTOBUILD_SKIP_ENV = "FIXER_WIRE_SKIP_FIXER_MCP_AUTOBUILD"
FIXER_WIRE_MODEL = "gpt-5.4"
FIXER_WIRE_REASONING_EFFORT = "medium"
FORCED_FIXER_MCP_TIMEOUT_FLOOR_SEC = 21_600
FORCED_FIXER_MCP_TIMEOUT_FLOOR_MS = FORCED_FIXER_MCP_TIMEOUT_FLOOR_SEC * 1000
_FIXER_MCP_BUILD_CHECKED: set[Path] = set()


def _parse_wire_args(argv: Sequence[str]) -> tuple[argparse.Namespace, list[str]]:
    parser = argparse.ArgumentParser(add_help=False)
    parser.add_argument("--role", choices=ROLE_CHOICES)
    parser.add_argument("--wire-info", action="store_true")
    parser.add_argument("--netrunner-session-id", type=int)
    parser.add_argument("--netrunner-backend")
    parser.add_argument("--netrunner-model")
    parser.add_argument("--netrunner-reasoning")
    parser.add_argument("--scaffold-mvp")
    parser.add_argument("--scaffold-target-dir")
    parser.add_argument(
        "--netrunner-mcp",
        action="append",
        default=[],
        help="Repeat or pass comma-separated MCP server names for netrunner overrides.",
    )
    parser.add_argument("--dry-run", action="store_true")
    return parser.parse_known_args(list(argv))


def _normalize_names(values: Sequence[str]) -> list[str]:
    seen: set[str] = set()
    names: list[str] = []
    for raw in values:
        for part in raw.split(","):
            name = part.strip()
            if not name or name in seen:
                continue
            seen.add(name)
            names.append(name)
    names.sort()
    return names


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


def _repo_root() -> Path:
    return Path(__file__).resolve().parents[1]


def _resolve_fixer_db_path(cwd: Path) -> Path:
    repo_root = _repo_root()
    candidates: list[Path] = []

    from_env = os.environ.get(FIXER_DB_PATH_ENV)
    if from_env:
        env_path = Path(from_env).expanduser()
        if not env_path.is_absolute():
            env_path = repo_root / env_path
        candidates.append(env_path)

    # Old self_orchestration wires are intentionally isolated from fixer_genui.db.
    # They only operate on the legacy fixer.db store unless explicitly overridden.
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
        raise RuntimeError(
            f"No project entry found in {PRIMARY_FIXER_DB_FILENAME} for cwd: "
            f"{cwd_text}\n"
            "Project onboarding is Fixer-only. Authenticate as fixer and call:\n"
            f"  register_project(cwd='{cwd_text}', name='{cwd.name or 'project'}')\n"
            "Then relaunch overseer/netrunner for this cwd."
        )
    return int(row[0])


def _assert_project_is_registered(cwd: Path) -> None:
    db_path = _resolve_fixer_db_path(cwd)
    conn = sqlite3.connect(db_path)
    try:
        _ensure_wire_schema(conn)
        _resolve_project_id(conn, cwd)
    finally:
        conn.close()


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


def _load_forced_fixer_spec() -> dict[str, object]:
    config_path = _repo_root() / "fixer_mcp" / "mcp_config.json"
    if not config_path.is_file():
        raise RuntimeError(f"Missing fixer MCP config: {config_path}")

    parsed = json.loads(config_path.read_text(encoding="utf-8"))
    raw_spec = parsed.get("mcpServers", {}).get(FORCED_MCP_SERVER)
    if not isinstance(raw_spec, dict):
        raise RuntimeError(f"Missing '{FORCED_MCP_SERVER}' entry in {config_path}")

    spec = dict(raw_spec)
    command = spec.get("command")
    if isinstance(command, str) and command and not command.startswith("/"):
        spec["command"] = str((config_path.parent / command).resolve())
    command = spec.get("command")
    if isinstance(command, str) and command.strip():
        _maybe_rebuild_fixer_mcp_binary(Path(command.strip()))
    spec.setdefault("args", [])
    spec.setdefault("env", {})
    spec.setdefault("transport", "stdio")
    spec.setdefault("cwd", str((_repo_root() / "fixer_mcp").resolve()))
    spec.setdefault("startup_timeout_sec", 30)
    return _with_forced_fixer_timeout_floor(spec)


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


def _maybe_rebuild_fixer_mcp_binary(command_path: Path) -> None:
    if os.environ.get(FIXER_MCP_AUTOBUILD_SKIP_ENV, "").strip() == "1":
        return

    repo_root = _repo_root()
    module_dir = repo_root / "fixer_mcp"
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
        # Do not attempt to auto-build external/custom fixer_mcp commands.
        return

    if binary_path in _FIXER_MCP_BUILD_CHECKED:
        return

    source_candidates = [*module_dir.rglob("*.go"), module_dir / "go.mod", module_dir / "go.sum"]
    latest_source_mtime = _latest_mtime(source_candidates)
    binary_mtime = binary_path.stat().st_mtime if binary_path.exists() else 0.0
    if binary_mtime >= latest_source_mtime:
        _FIXER_MCP_BUILD_CHECKED.add(binary_path)
        return

    print(
        "[fixer-wire] detected stale fixer_mcp binary; rebuilding before launch...",
        file=sys.stderr,
    )
    try:
        subprocess.run(
            ["go", "build", "-o", str(binary_path), "."],
            cwd=str(module_dir),
            check=True,
        )
    except (OSError, subprocess.CalledProcessError) as err:
        raise RuntimeError(
            f"Failed to rebuild fixer_mcp binary at {binary_path}: {err}. "
            f"Set {FIXER_MCP_AUTOBUILD_SKIP_ENV}=1 to disable auto-build."
        ) from err

    _FIXER_MCP_BUILD_CHECKED.add(binary_path)


def _build_forced_fixer_override_args() -> list[str]:
    spec = _load_forced_fixer_spec()
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


def _inject_forced_fixer_server(available_servers: dict[str, dict[str, object]]) -> dict[str, dict[str, object]]:
    spec = _load_forced_fixer_spec()
    merged = dict(available_servers)
    current = dict(merged.get(FORCED_MCP_SERVER, {}))
    current.update(spec)
    current["_source"] = "project_mcp"
    merged[FORCED_MCP_SERVER] = current
    return merged


def _inject_figma_console_server(
    available_servers: dict[str, dict[str, object]],
    cwd: Path,
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
    current["env"] = env_map

    current.setdefault("_source", "project_mcp")
    merged[FIGMA_CONSOLE_MCP_NAME] = current
    return merged


def _inject_research_query_server(
    available_servers: dict[str, dict[str, object]],
    cwd: Path,
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
    if shutil.which("go") is None:
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


def _strip_md_prefix(text: str) -> str:
    return re.sub(r"^[#>*\-\s\d\.\)\(]+", "", text).strip()


def _session_title(task_description: str, *, limit: int = 110) -> str:
    stripped = task_description.strip()
    if not stripped:
        return "(empty task)"

    for line in stripped.splitlines():
        candidate = _strip_md_prefix(line)
        if not candidate:
            continue
        if candidate.lower() in {"goal", "цель"}:
            continue
        return textwrap.shorten(candidate, width=limit, placeholder="…")

    first_line = stripped.splitlines()[0]
    return textwrap.shorten(_strip_md_prefix(first_line) or first_line, width=limit, placeholder="…")


def _load_registry_mcp_names(conn: sqlite3.Connection) -> list[str]:
    return sorted(_load_registry_mcp_metadata(conn).keys())


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
        # Backward-compatible fallback for DBs that predate category/how_to/default metadata.
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


def _load_project_allowed_mcp_names(conn: sqlite3.Connection, project_id: int) -> list[str]:
    try:
        _bootstrap_project_mcp_bindings(conn, project_id)
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
        # Backward-compatible fallback while wiring catches up.
        return _load_registry_mcp_names(conn)
    return [str(row[0]) for row in rows]


def _allowed_runtime_mcp_names(
    allowed_names: Sequence[str],
    available_servers: dict[str, dict[str, object]],
) -> list[str]:
    available_names = set(available_servers.keys())
    allowed_runtime = set(allowed_names).intersection(available_names)
    allowed_runtime -= HIDDEN_MCP_SERVERS
    return sorted(allowed_runtime)


def _assigned_preselected_mcp_names(
    assigned_names: Sequence[str],
    allowed_runtime_names: Sequence[str],
) -> list[str]:
    allowed_runtime_set = set(allowed_runtime_names)
    preselected = set(assigned_names).intersection(allowed_runtime_set)
    preselected -= HIDDEN_MCP_SERVERS
    return sorted(preselected)


def _assigned_allowed_mcp_names(
    assigned_names: Sequence[str],
    allowed_names: Sequence[str],
) -> list[str]:
    allowed_set = set(allowed_names)
    assigned_allowed = set(assigned_names).intersection(allowed_set)
    assigned_allowed -= HIDDEN_MCP_SERVERS
    return sorted(assigned_allowed)


def _eligible_session_mcp_names(
    assigned_names: Sequence[str],
    allowed_names: Sequence[str],
    available_servers: dict[str, dict[str, object]],
) -> list[str]:
    # Backward-compatible alias used in tests/older call sites.
    available_names = set(available_servers.keys())
    eligible = set(assigned_names).intersection(allowed_names).intersection(available_names)
    eligible -= HIDDEN_MCP_SERVERS
    return sorted(eligible)


def _load_project_web_mcp_servers(cwd: Path) -> dict[str, dict[str, object]]:
    config_path = cwd / WEB_MCP_CONFIG_FILENAME
    if not config_path.is_file():
        return {}

    try:
        with config_path.open("rb") as fh:
            data = tomllib.load(fh)
    except Exception as exc:  # pragma: no cover - defensive
        print(f"[warning] failed to parse {config_path}: {exc}", file=sys.stderr)
        return {}

    servers_block = data.get("mcp_servers")
    if not isinstance(servers_block, dict):
        servers_block = data.get("mcpServers")
    if not isinstance(servers_block, dict):
        print(
            f"[warning] {config_path} does not define [mcp_servers] (or [mcpServers]); skipping",
            file=sys.stderr,
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
                file=sys.stderr,
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


def _select_role_interactive(Option: Any, single_select_items: Any) -> str:
    options = [
        (SCAFFOLD_MVP_ACTION, "MVP Scaffold"),
        ("fixer", "Fixer (Global)"),
        ("overseer", "Overseer (Project)"),
        ("netrunner", "Netrunner (Worker)"),
    ]
    choice = single_select_items(
        [Option(label, value) for value, label in options],
        title="Select mode (enter confirm, q cancel)",
        preselected_value=SCAFFOLD_MVP_ACTION,
    )
    if choice is None:
        print("Cancelled.")
        raise SystemExit(130)
    return str(choice)


def _prompt_scaffold_value(prompt: str, *, default: str | None = None) -> str:
    while True:
        suffix = f" [{default}]" if default else ""
        raw = input(f"{prompt}{suffix}: ").strip()
        if raw.lower() in {"q", "quit", "exit"}:
            print("Cancelled.")
            raise SystemExit(130)
        if raw:
            return raw
        if default is not None:
            return default
        print("Value is required.")


def _select_scaffold_execution_mode_interactive(Option: Any, single_select_items: Any) -> bool:
    options = [
        Option("MVP scaffold mode", is_header=True),
        Option("Dry run only", "dry_run"),
        Option("Create scaffold", "create"),
    ]
    selected = single_select_items(
        options,
        title="Select scaffold mode (enter confirm, q cancel)",
        preselected_value="dry_run",
    )
    if selected is None:
        print("Cancelled.")
        raise SystemExit(130)
    selected_text = str(selected)
    if selected_text == "dry_run":
        return True
    if selected_text == "create":
        return False
    raise RuntimeError(f"Unexpected scaffold mode: {selected_text}")


def _launch_scaffold_interactive(Option: Any, single_select_items: Any) -> int:
    project_name = _prompt_scaffold_value("MVP project name or slug")
    target_dir = _prompt_scaffold_value("Target parent directory", default=str(Path.cwd()))
    dry_run = _select_scaffold_execution_mode_interactive(Option, single_select_items)
    return run_scaffold_cli(project_name, target_dir=target_dir, dry_run=dry_run)


def _select_fixer_launch_action_interactive(Option: Any, single_select_items: Any) -> str:
    options = [
        Option("Fixer global launch", is_header=True),
        Option("Start new Fixer", FIXER_LAUNCH_NEW),
        Option("Resume existing Fixer", FIXER_LAUNCH_RESUME),
    ]
    selected = single_select_items(
        options,
        title="Fixer global session mode (enter confirm, q cancel)",
        preselected_value=FIXER_LAUNCH_NEW,
    )
    if selected is None:
        print("Cancelled.")
        raise SystemExit(130)
    selected_text = str(selected)
    if selected_text not in {FIXER_LAUNCH_NEW, FIXER_LAUNCH_RESUME}:
        raise RuntimeError(f"Unexpected Fixer launch mode: {selected_text}")
    return selected_text


def _select_session_interactive(
    session_rows: Sequence[SessionRow],
    Option: Any,
    single_select_items: Any,
) -> SessionRow:
    by_id = {row.session_id: row for row in session_rows}
    show_archived = not any(row.status in {"pending", "in_progress"} for row in session_rows)
    while True:
        if show_archived:
            visible = list(session_rows)
        else:
            visible = [row for row in session_rows if row.status in RECENTLY_ACTIVE_STATUSES]
            if not visible:
                visible = [row for row in session_rows if row.status in {"pending", "in_progress"}]

        if not visible:
            if not show_archived:
                show_archived = True
                continue
            raise RuntimeError("No sessions available for selection.")

        options = [Option("Netrunner sessions", is_header=True)]
        preselected: int | None = None
        for row in visible:
            external_suffix = ""
            if row.external_session_id:
                external_suffix = f" | {row.cli_backend}={row.external_session_id}"
            label = f"[{row.session_id}] {row.status:<11} | {_session_title(row.task_description)}{external_suffix}"
            options.append(Option(label, row.session_id))
            if preselected is None and row.status == "in_progress":
                preselected = row.session_id

        toggle_label = "[+] Show archived statuses" if not show_archived else "[-] Hide archived statuses"
        options.append(Option(toggle_label, TOGGLE_ARCHIVED_VALUE))
        selected = single_select_items(
            options,
            title="Select netrunner session (enter confirm, q cancel)",
            preselected_value=preselected if preselected is not None else visible[0].session_id,
        )
        if selected is None:
            print("Cancelled.")
            raise SystemExit(130)
        if selected == TOGGLE_ARCHIVED_VALUE:
            show_archived = not show_archived
            continue

        session_id = int(selected)
        row = by_id.get(session_id)
        if row is None:
            raise RuntimeError(f"Selected session {session_id} is unavailable.")
        return row


def _select_mcp_interactive(
    registry_names: Sequence[str],
    assigned_names: Sequence[str],
    registry_meta: dict[str, RegistryMcpMetadata],
    available_servers: dict[str, dict[str, object]],
    Option: Any,
    multi_select_items: Any,
    *,
    show_all_registry_names: bool = False,
) -> list[str]:
    always_visible_names = ALWAYS_VISIBLE_MCP_NAMES.intersection(set(registry_names))
    if show_all_registry_names:
        names = sorted((set(registry_names) | set(assigned_names) | always_visible_names) - HIDDEN_MCP_SERVERS)
    else:
        default_names = {name for name in registry_names if registry_meta.get(name) and registry_meta[name].is_default}
        all_candidate_names = {*registry_names, *assigned_names}
        if default_names:
            names = sorted((default_names | set(assigned_names) | always_visible_names) - HIDDEN_MCP_SERVERS)
        else:
            names = sorted(all_candidate_names - HIDDEN_MCP_SERVERS)
    if not names:
        return []

    unavailable = {name for name in names if name not in available_servers}
    options = [Option("Session MCP defaults", is_header=True)]

    category_buckets: dict[str, list[str]] = {}
    for name in names:
        meta = _registry_metadata_with_fallback(name, registry_meta.get(name))
        category = (meta.category if meta else "").strip() or MCP_FALLBACK_CATEGORY
        category_buckets.setdefault(category, []).append(name)

    def _category_sort_key(category: str) -> tuple[int, str]:
        try:
            return MCP_CATEGORY_ORDER.index(category), category.lower()
        except ValueError:
            return len(MCP_CATEGORY_ORDER), category.lower()

    for category in sorted(category_buckets.keys(), key=_category_sort_key):
        options.append(Option(category, is_header=True))
        for name in sorted(category_buckets[category]):
            meta = _registry_metadata_with_fallback(name, registry_meta.get(name))
            label = name
            if meta and meta.is_default:
                label = f"{label} [default]"
            options.append(Option(label, name, disabled=name in unavailable))

    selected = multi_select_items(
        options,
        title="Select MCP servers (space toggle, enter confirm, a toggle all, q cancel)",
        preselected_values=[name for name in assigned_names if name in names and name not in unavailable],
    )
    if selected is None:
        print("Cancelled.")
        raise SystemExit(130)
    return [str(name) for name in selected if isinstance(name, str)]


def _sync_registry_names(conn: sqlite3.Connection, names: Sequence[str]) -> None:
    normalized = _normalize_names(list(names))
    if not normalized:
        return
    with conn:
        for name in normalized:
            conn.execute("INSERT OR IGNORE INTO mcp_server (name, auto_attach) VALUES (?, 0)", (name,))


def _persist_session_mcp_names(conn: sqlite3.Connection, session_id: int, names: Sequence[str]) -> None:
    normalized = _normalize_names(list(names))
    _sync_registry_names(conn, normalized)

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
) -> SessionLaunchSelection:
    normalized_backend = normalize_backend_name(selection.backend)
    resolved_model = selection.model.strip()
    resolved_reasoning = selection.reasoning.strip()
    stored_backend = normalize_backend_name(session_row.cli_backend)
    stored_model = session_row.cli_model.strip()
    stored_reasoning = session_row.cli_reasoning.strip()
    started = bool(session_row.external_session_id.strip())

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
    final_model = stored_model or resolved_model
    final_reasoning = stored_reasoning or resolved_reasoning

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


def _latest_codex_session_id_for_cwd(cwd: Path) -> str | None:
    try:
        from codex_pro_app.main import _load_session_summaries
    except Exception:
        return None

    history_path = Path.home() / ".codex" / "history.jsonl"
    try:
        summaries = _load_session_summaries(history_path, limit=1, cwd_filter=cwd)
    except Exception:
        return None
    if not summaries:
        return None
    return summaries[0].session_id


def _prompt_resume_session_id(session_id: int, backend: str) -> str | None:
    descriptor = _backend_descriptor(backend)
    while True:
        raw = input(
            f"No stored {descriptor.label} session id for session {session_id}. "
            "Enter session id to resume (q cancel): "
        ).strip()
        if raw.lower() in {"q", "quit", "exit"}:
            return None
        if raw:
            return raw
        print("Session id is required to resume non-pending sessions.")


def _netrunner_session_marker(session_id: int) -> str:
    return f"Preselected session ID from fixer wire: `{session_id}`."


def _first_marker_line(
    log_path: Path,
    marker: str,
    *,
    max_lines: int = 240,
) -> int | None:
    if not marker:
        return None
    try:
        with log_path.open("r", encoding="utf-8") as fh:
            for index, raw_line in enumerate(fh):
                if marker in raw_line:
                    return index
                if index >= max_lines:
                    break
    except OSError:
        return None
    return None


def _session_log_has_markers(log_path: Path, markers: Sequence[str], *, max_lines: int = 240) -> bool:
    try:
        with log_path.open("r", encoding="utf-8") as fh:
            required = {marker for marker in markers if marker}
            for index, raw_line in enumerate(fh):
                matched = {marker for marker in required if marker in raw_line}
                required.difference_update(matched)
                if not required:
                    return True
                if index >= max_lines:
                    break
    except OSError:
        return False
    return False


def _session_log_has_fixer_marker(log_path: Path, *, max_lines: int = 240) -> bool:
    return _session_log_has_markers(log_path, [FIXER_SKILL_MARKER], max_lines=max_lines)


def _session_log_is_fixer_session(log_path: Path, *, max_lines: int = 240) -> bool:
    fixer_line = _first_marker_line(log_path, FIXER_SKILL_MARKER, max_lines=max_lines)
    if fixer_line is None:
        return False
    competing_lines = [
        line
        for line in (
            _first_marker_line(log_path, NETRUNNER_SKILL_MARKER, max_lines=max_lines),
            *[
                _first_marker_line(log_path, marker, max_lines=max_lines)
                for marker in LEGACY_NETRUNNER_SKILL_MARKERS
            ],
            _first_marker_line(log_path, OVERSEER_SKILL_MARKER, max_lines=max_lines),
        )
        if line is not None
    ]
    if not competing_lines:
        return True
    return fixer_line <= min(competing_lines)


def _session_log_has_netrunner_marker(
    log_path: Path,
    session_id: int | None = None,
    *,
    max_lines: int = 240,
) -> bool:
    skill_markers = [NETRUNNER_SKILL_MARKER, *LEGACY_NETRUNNER_SKILL_MARKERS]
    if not any(
        _session_log_has_markers(log_path, [marker], max_lines=max_lines)
        for marker in skill_markers
    ):
        return False
    if session_id is None:
        return True
    return _session_log_has_markers(log_path, [_netrunner_session_marker(session_id)], max_lines=max_lines)


def _load_cwd_session_summaries(cwd: Path, *, limit: int, minimum_scan_limit: int = 80) -> tuple[Any, list[Any]]:
    try:
        from codex_pro_app.main import _find_session_log, _load_session_summaries
    except Exception as err:
        raise RuntimeError("Unable to load Codex history helpers for resume flow.") from err

    history_path = Path.home() / ".codex" / "history.jsonl"
    scan_limit = max(limit * 4, minimum_scan_limit)
    summaries = _load_session_summaries(history_path, limit=scan_limit, cwd_filter=cwd)
    return _find_session_log, summaries


def _load_fixer_resume_summaries(cwd: Path, *, limit: int = 40) -> list[Any]:
    try:
        find_session_log, summaries = _load_cwd_session_summaries(cwd, limit=limit)
    except RuntimeError as err:
        raise RuntimeError("Unable to load Codex history helpers for Fixer resume flow.") from err
    explicit_session_ids = _load_fixer_resume_alias_session_ids(cwd)
    fixer_summaries: list[Any] = []
    for summary in summaries:
        if str(summary.session_id) in explicit_session_ids:
            fixer_summaries.append(summary)
            if len(fixer_summaries) >= limit:
                break
            continue
        log_path = find_session_log(summary.session_id, created=summary.created, updated=summary.updated)
        if not log_path:
            continue
        if not _session_log_is_fixer_session(log_path):
            continue
        fixer_summaries.append(summary)
        if len(fixer_summaries) >= limit:
            break
    return fixer_summaries


def _load_fixer_resume_alias_session_ids(cwd: Path) -> set[str]:
    try:
        db_path = _resolve_fixer_db_path(cwd)
    except RuntimeError:
        return set()

    conn = sqlite3.connect(db_path)
    try:
        _ensure_wire_schema(conn)
        project_id = _resolve_project_id(conn, cwd)
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


def _load_netrunner_resume_summaries(cwd: Path, session_id: int, *, limit: int = 20) -> list[Any]:
    try:
        find_session_log, summaries = _load_cwd_session_summaries(cwd, limit=limit, minimum_scan_limit=120)
    except RuntimeError as err:
        raise RuntimeError("Unable to load Codex history helpers for Netrunner resume flow.") from err

    netrunner_summaries: list[Any] = []
    for summary in summaries:
        log_path = find_session_log(summary.session_id, created=summary.created, updated=summary.updated)
        if not log_path:
            continue
        if not _session_log_has_netrunner_marker(log_path, session_id):
            continue
        netrunner_summaries.append(summary)
        if len(netrunner_summaries) >= limit:
            break
    return netrunner_summaries


def _select_fixer_resume_session_interactive(
    summaries: Sequence[Any],
    Option: Any,
    single_select_items: Any,
) -> str:
    if not summaries:
        raise RuntimeError("No existing Fixer sessions were found for this project cwd.")

    options = [Option("Fixer sessions", is_header=True)]
    for summary in summaries:
        created_local = summary.created.astimezone().strftime("%Y-%m-%d %H:%M")
        updated_local = summary.updated.astimezone().strftime("%Y-%m-%d %H:%M")
        preview = textwrap.shorten(
            _strip_md_prefix(getattr(summary, "preview", "") or "(no preview)"),
            width=66,
            placeholder="…",
        )
        label = f"[{summary.session_id}] started {created_local} | updated {updated_local} | {preview}"
        options.append(Option(label, summary.session_id))

    selected = single_select_items(
        options,
        title="Select Fixer session to resume (enter confirm, q cancel)",
        preselected_value=summaries[0].session_id,
    )
    if selected is None:
        print("Cancelled.")
        raise SystemExit(130)

    selected_text = str(selected)
    if not any(str(summary.session_id) == selected_text for summary in summaries):
        raise RuntimeError(f"Selected Codex session '{selected_text}' is unavailable.")
    return selected_text


def _select_netrunner_resume_session_interactive(
    summaries: Sequence[Any],
    session_id: int,
    Option: Any,
    single_select_items: Any,
    *,
    preferred_session_id: str | None = None,
) -> str:
    if not summaries:
        raise RuntimeError(f"No matching Codex sessions were found for netrunner session {session_id}.")

    options = [Option("Matching Codex sessions", is_header=True)]
    available_ids = {str(summary.session_id) for summary in summaries}
    for summary in summaries:
        created_local = summary.created.astimezone().strftime("%Y-%m-%d %H:%M")
        updated_local = summary.updated.astimezone().strftime("%Y-%m-%d %H:%M")
        preview = textwrap.shorten(
            _strip_md_prefix(getattr(summary, "preview", "") or "(no preview)"),
            width=66,
            placeholder="…",
        )
        label = f"[{summary.session_id}] started {created_local} | updated {updated_local} | {preview}"
        options.append(Option(label, summary.session_id))

    selected = single_select_items(
        options,
        title=f"Select Codex session to resume for netrunner session {session_id} (enter confirm, q cancel)",
        preselected_value=preferred_session_id if preferred_session_id in available_ids else summaries[0].session_id,
    )
    if selected is None:
        print("Cancelled.")
        raise SystemExit(130)

    selected_text = str(selected)
    if selected_text not in available_ids:
        raise RuntimeError(f"Selected Codex session '{selected_text}' is unavailable.")
    return selected_text


def _resolve_netrunner_resume_session_id(
    cwd: Path,
    selected_session: SessionRow,
    Option: Any,
    single_select_items: Any,
) -> str:
    backend = normalize_backend_name(selected_session.cli_backend)
    stored_session_id = selected_session.external_session_id.strip()
    if backend != "codex":
        if stored_session_id:
            return stored_session_id
        manual_session_id = _prompt_resume_session_id(selected_session.session_id, backend)
        if manual_session_id:
            return manual_session_id
        raise RuntimeError(
            f"Session {selected_session.session_id} is not pending and no stored {backend} session id was found to resume."
        )

    matching_summaries = _load_netrunner_resume_summaries(cwd, selected_session.session_id)
    available_ids = [str(summary.session_id) for summary in matching_summaries]

    if stored_session_id and stored_session_id in available_ids:
        return stored_session_id
    if len(matching_summaries) == 1:
        return str(matching_summaries[0].session_id)
    if matching_summaries:
        preferred = stored_session_id if stored_session_id in available_ids else None
        return _select_netrunner_resume_session_interactive(
            matching_summaries,
            selected_session.session_id,
            Option,
            single_select_items,
            preferred_session_id=preferred,
        )

    manual_session_id = _prompt_resume_session_id(selected_session.session_id, backend)
    if manual_session_id:
        return manual_session_id
    raise RuntimeError(
        f"Session {selected_session.session_id} is not pending and no matching Codex session was found to resume."
    )


def _latest_matching_netrunner_codex_session_id(cwd: Path, session_id: int) -> str | None:
    summaries = _load_netrunner_resume_summaries(cwd, session_id, limit=8)
    if not summaries:
        return None
    return str(summaries[0].session_id)


def _load_available_servers(cwd: Path, *, backend: str = DEFAULT_BACKEND) -> tuple[dict[str, dict[str, object]], dict[str, str], Any, Any]:
    from codex_pro_app.config_loader import (
        ConfigError,
        attach_preprompts_from_command_paths,
        discover_project_mcp_servers,
        discover_self_mcp_servers,
        fetch_mcp_servers,
        get_config_path,
        load_config,
        merge_mcp_servers,
    )
    from codex_pro_app.main import (
        CODEX_CLI_ADAPTER,
        CONFIG_ENV_VARS,
        _ensure_sqlite_scaffold,
    )

    try:
        config = load_config(get_config_path())
        available_servers = fetch_mcp_servers(config)
        local_servers, missing_local = discover_self_mcp_servers(cwd)
        project_servers = discover_project_mcp_servers(cwd)
        web_mcp_servers = _load_project_web_mcp_servers(cwd)
        if missing_local:
            for path in missing_local:
                try:
                    rel = path.relative_to(cwd)
                except ValueError:
                    rel = path
                print(
                    f"[warning] self_mcp_servers entry without mcp.json: {rel} (skipped)",
                    file=sys.stderr,
                )
        available_servers = merge_mcp_servers(available_servers, local_servers)
        available_servers = merge_mcp_servers(available_servers, project_servers)
        if web_mcp_servers:
            available_servers = _overlay_project_mcp_servers(available_servers, web_mcp_servers)
        available_servers = _inject_research_query_server(available_servers, cwd)
        available_servers = _inject_figma_console_server(available_servers, cwd)
        available_servers = _inject_forced_fixer_server(available_servers)
        attach_preprompts_from_command_paths(available_servers)
    except ConfigError as err:
        raise RuntimeError(str(err)) from err

    return available_servers, CONFIG_ENV_VARS, get_backend_adapter(backend, codex_adapter=CODEX_CLI_ADAPTER), _ensure_sqlite_scaffold


def _select_backend_interactive(
    preferred_backend: str,
    Option: Any,
    single_select_items: Any,
) -> str:
    descriptors = available_backend_descriptors()
    options = [Option("CLI backends", is_header=True)]
    for descriptor in descriptors:
        label = descriptor.label
        if descriptor.name == DEFAULT_BACKEND:
            label = f"{label} [default]"
        options.append(Option(f"{label} | {descriptor.description}", descriptor.name))

    selected = single_select_items(
        options,
        title="Select CLI backend (enter confirm, q cancel)",
        preselected_value=normalize_backend_name(preferred_backend),
    )
    if selected is None:
        print("Cancelled.")
        raise SystemExit(130)
    return normalize_backend_name(str(selected))


def _select_model_interactive(
    backend: str,
    preferred_model: str,
    Option: Any,
    single_select_items: Any,
) -> str:
    descriptor = _backend_descriptor(backend)
    options = [Option(f"{descriptor.label} models", is_header=True)]
    for model in descriptor.model_options:
        label = model
        if model == descriptor.default_model:
            label = f"{label} [default]"
        options.append(Option(label, model))

    selected = single_select_items(
        options,
        title=f"Select {descriptor.label} model (enter confirm, q cancel)",
        preselected_value=preferred_model.strip() or descriptor.default_model,
    )
    if selected is None:
        print("Cancelled.")
        raise SystemExit(130)
    return str(selected).strip()


def _select_reasoning_interactive(
    backend: str,
    preferred_reasoning: str,
    Option: Any,
    single_select_items: Any,
) -> str:
    descriptor = _backend_descriptor(backend)
    options = [Option(f"{descriptor.label} reasoning", is_header=True)]
    for reasoning in descriptor.reasoning_options:
        label = reasoning
        if reasoning == descriptor.default_reasoning:
            label = f"{label} [default]"
        options.append(Option(label, reasoning))

    selected = single_select_items(
        options,
        title=f"Select {descriptor.label} reasoning (enter confirm, q cancel)",
        preselected_value=preferred_reasoning.strip() or descriptor.default_reasoning,
    )
    if selected is None:
        print("Cancelled.")
        raise SystemExit(130)
    return str(selected).strip()


def _build_netrunner_prompt(
    session_id: int,
    mcp_names: Sequence[str],
    mcp_how_to: dict[str, str],
) -> str:
    mcp_text = ", ".join(mcp_names) if mcp_names else "none"
    how_to_lines: list[str] = []
    for name in mcp_names:
        guidance = mcp_how_to.get(name, _build_default_how_to(name))
        how_to_lines.append(f"- {name}: {guidance}")
    standard_web_stack_text = _build_standard_web_stack_guidance_block(mcp_names)
    prompt_lines = [
        "Activate skill `$manual-resolution` immediately.",
        "Use its Netrunner separate-terminal mode for this launch.",
        "Execute only its initialization checklist first, then stop and report status.",
        "",
        f"Preselected session ID from fixer wire: `{session_id}`.",
        f"Assigned MCP selection from fixer wire: {mcp_text}.",
        "Attached MCP how-to guidance:",
        *(how_to_lines or ["- none"]),
    ]
    if standard_web_stack_text:
        prompt_lines.extend(["", *standard_web_stack_text.splitlines()])
    prompt_lines.append("Use this session ID for checkout unless Architect explicitly overrides.")
    return "\n".join(prompt_lines)


def _build_default_how_to(server_name: str) -> str:
    return f"Use {server_name} for domain-specific tools in this task; inspect tool descriptions before execution."


def _build_standard_web_stack_guidance_block(mcp_names: Sequence[str]) -> str:
    selected = {name.strip() for name in mcp_names}
    if not selected.intersection(WEB_STACK_GUIDANCE_MCP_NAMES):
        return ""
    lines = ["Standard web stack guidance:"]
    lines.extend(f"- {item}" for item in STANDARD_WEB_STACK_GUIDANCE)
    return "\n".join(lines)


def _build_mcp_how_to_map(
    mcp_names: Sequence[str],
    registry_meta: dict[str, RegistryMcpMetadata],
) -> dict[str, str]:
    how_to_by_name: dict[str, str] = {}
    for name in mcp_names:
        metadata = _registry_metadata_with_fallback(name, registry_meta.get(name))
        how_to = (metadata.how_to if metadata else "").strip()
        if not how_to:
            how_to = _build_default_how_to(name)
        how_to_by_name[name] = how_to
    return how_to_by_name


def _build_fixer_prompt() -> str:
    return textwrap.dedent(
        """
        Activate skill `$start-fixer` immediately.
        Fixer is the project-scoped orchestrator role in the current Fixer MCP system.
        Execute only its initialization checklist first, then stop and report status.
        """
    ).strip()


def _ensure_passthrough_dangerous_sandbox(passthrough_args: Sequence[str]) -> list[str]:
    args = list(passthrough_args)
    if "--sandbox" in args:
        return args
    return [*args, "--sandbox", "danger-full-access"]


def _prefer_fixed_model_for_role_presets(codex_main: Any) -> None:
    order_raw = getattr(codex_main, "MODEL_DISPLAY_ORDER", [])
    if not isinstance(order_raw, list):
        return
    order = [str(item) for item in order_raw if str(item).strip()]
    preferred = [FIXER_WIRE_MODEL, *[item for item in order if item != FIXER_WIRE_MODEL]]
    setattr(codex_main, "MODEL_DISPLAY_ORDER", preferred)
    default_effort = getattr(codex_main, "MODEL_DEFAULT_EFFORT", None)
    if isinstance(default_effort, dict):
        default_effort[FIXER_WIRE_MODEL] = FIXER_WIRE_REASONING_EFFORT


def _resolve_netrunner_launch_selection(
    selected_session: SessionRow,
    *,
    preset_backend: str | None,
    preset_model: str | None,
    preset_reasoning: str | None,
    dry_run: bool,
    Option: Any,
    single_select_items: Any,
) -> SessionLaunchSelection:
    started = bool(selected_session.external_session_id.strip())
    preferred_backend = normalize_backend_name(selected_session.cli_backend)

    if preset_backend:
        backend = normalize_backend_name(preset_backend)
    elif started or dry_run:
        backend = preferred_backend
    else:
        backend = _select_backend_interactive(preferred_backend, Option, single_select_items)

    descriptor = _backend_descriptor(backend)

    if preset_model and preset_model.strip():
        model = preset_model.strip()
    elif selected_session.cli_model.strip():
        model = selected_session.cli_model.strip()
    elif started or dry_run:
        model = descriptor.default_model
    else:
        model = _select_model_interactive(backend, descriptor.default_model, Option, single_select_items)

    if preset_reasoning and preset_reasoning.strip():
        reasoning = preset_reasoning.strip()
    elif selected_session.cli_reasoning.strip():
        reasoning = selected_session.cli_reasoning.strip()
    elif started or dry_run:
        reasoning = descriptor.default_reasoning
    else:
        reasoning = _select_reasoning_interactive(backend, descriptor.default_reasoning, Option, single_select_items)

    return SessionLaunchSelection(
        backend=backend,
        model=model,
        reasoning=reasoning,
    )


def _launch_fixer(
    passthrough_args: Sequence[str],
    *,
    dry_run: bool,
    Option: Any,
    single_select_items: Any,
) -> int:
    from codex_pro_app.main import (
        ExecutionPreferences,
        LLMSelection,
        _load_llm_env,
        _merge_env_with_os,
        _reasoning_label,
    )

    cwd = Path.cwd()
    _assert_project_is_registered(cwd)
    launch_mode = _select_fixer_launch_action_interactive(Option, single_select_items)
    resume_codex_session_id: str | None = None
    if launch_mode == FIXER_LAUNCH_RESUME:
        fixer_summaries = _load_fixer_resume_summaries(cwd)
        resume_codex_session_id = _select_fixer_resume_session_interactive(
            fixer_summaries,
            Option,
            single_select_items,
        )

    available_servers, config_env_vars, adapter, ensure_sqlite_scaffold = _load_available_servers(cwd)
    selected_mcp_names: list[str] = []
    if FORCED_MCP_SERVER in available_servers:
        selected_mcp_names = [FORCED_MCP_SERVER]
    else:
        print(
            f"[warning] {FORCED_MCP_SERVER} not found in launcher MCP set; continuing without forced attach",
            file=sys.stderr,
        )
    selected_servers = {name: available_servers[name] for name in selected_mcp_names}

    selected_config_paths: dict[str, Path] = {}
    if "sqlite" in selected_servers:
        sqlite_config = ensure_sqlite_scaffold(cwd)
        if sqlite_config is None:
            raise RuntimeError("Unable to scaffold sqliteMCP.toml for selected sqlite server.")
        selected_config_paths["sqlite"] = sqlite_config

    model = FIXER_WIRE_MODEL
    effort = FIXER_WIRE_REASONING_EFFORT
    llm_selection = LLMSelection(
        display_model=model,
        detail=_reasoning_label(model, effort),
        provider_slug="openai",
        model=model,
        reasoning_effort=effort,
        requires_provider_override=False,
    )
    execution_prefs = ExecutionPreferences(dangerous_sandbox=True, auto_approve=True)

    codex_args: list[str] = []
    codex_args.extend(adapter.build_llm_args(llm_selection))
    codex_args.extend(adapter.build_execution_args(execution_prefs))
    codex_args.extend(list(passthrough_args))

    option_args = [*codex_args, *adapter.build_mcp_flags(selected_servers, available_servers)]
    if resume_codex_session_id:
        codex_cmd = [adapter.command, "resume", *option_args, resume_codex_session_id]
    else:
        codex_cmd = [adapter.command, *option_args]
        prompt = _build_fixer_prompt()
        if prompt:
            codex_cmd.extend(adapter.build_prompt_args(prompt))

    llm_env = _load_llm_env()
    env = _merge_env_with_os(llm_env)
    adapter.prepare_env(env, llm_selection)
    for server_name, config_path in selected_config_paths.items():
        env_var = config_env_vars.get(server_name)
        if env_var:
            env[env_var] = str(config_path)

    if resume_codex_session_id:
        print(f"[fixer-wire] resuming fixer codex session id: {resume_codex_session_id}")
    else:
        print("[fixer-wire] starting new fixer codex session")
    print(f"[fixer-wire] fixer MCP selection: {', '.join(selected_mcp_names) if selected_mcp_names else 'none'}")
    print("[fixer-wire] command:", codex_cmd)
    if dry_run:
        return 0

    return subprocess.call(codex_cmd, env=env)


def _launch_netrunner(
    passthrough_args: Sequence[str],
    *,
    preset_session_id: int | None,
    preset_backend: str | None = None,
    preset_model: str | None = None,
    preset_reasoning: str | None = None,
    preset_mcp_names: Sequence[str],
    dry_run: bool,
    Option: Any,
    single_select_items: Any,
    multi_select_items: Any,
) -> int:
    from codex_pro_app.main import (
        ExecutionPreferences,
        LLMSelection,
        _load_llm_env,
        _merge_env_with_os,
        _reasoning_label,
    )

    cwd = Path.cwd()
    db_path = _resolve_fixer_db_path(cwd)
    # Keep launcher DB access in short-lived windows so interactive selection
    # never leaves SQLite pinned open while the operator is thinking.
    with closing(sqlite3.connect(db_path)) as conn:
        _ensure_wire_schema(conn)
        project_id = _resolve_project_id(conn, cwd)
        sessions = _load_session_rows(conn, project_id)
    if not sessions:
        raise RuntimeError("No sessions found for current project.")

    session_by_id = {row.session_id: row for row in sessions}
    if preset_session_id is not None:
        if preset_session_id not in session_by_id:
            raise RuntimeError(f"Session {preset_session_id} is not available for the current project.")
        selected_session = session_by_id[preset_session_id]
    else:
        selected_session = _select_session_interactive(sessions, Option, single_select_items)

    launch_selection = _resolve_netrunner_launch_selection(
        selected_session,
        preset_backend=preset_backend,
        preset_model=preset_model,
        preset_reasoning=preset_reasoning,
        dry_run=dry_run,
        Option=Option,
        single_select_items=single_select_items,
    )

    available_servers, config_env_vars, adapter, ensure_sqlite_scaffold = _load_available_servers(
        cwd,
        backend=launch_selection.backend,
    )
    with closing(sqlite3.connect(db_path)) as conn:
        _ensure_wire_schema(conn)
        _sync_registry_names(conn, list(available_servers.keys()))
        registry_meta = _load_registry_mcp_metadata(conn)
        assigned_names = _load_assigned_mcp_names(conn, selected_session.global_session_id)
        allowed_names = _load_project_allowed_mcp_names(conn, project_id)
    allowed_runtime_names = _allowed_runtime_mcp_names(allowed_names, available_servers)
    assigned_allowed_names = _assigned_allowed_mcp_names(assigned_names, allowed_names)
    preselected_names = _assigned_preselected_mcp_names(assigned_names, allowed_runtime_names)
    assigned_unavailable_names = sorted(set(assigned_allowed_names) - set(allowed_runtime_names))
    picker_pool_names = sorted(set(allowed_runtime_names).union(assigned_unavailable_names))

    if assigned_unavailable_names:
        print(
            "[warning] Assigned MCP server(s) unavailable in current runtime config: "
            + ", ".join(assigned_unavailable_names),
            file=sys.stderr,
        )

    if preset_mcp_names:
        selected_mcp_names = _normalize_names(preset_mcp_names)
        invalid = sorted(
            name
            for name in selected_mcp_names
            if name not in allowed_runtime_names and name != FORCED_MCP_SERVER
        )
        if invalid:
            invalid_text = ", ".join(invalid)
            raise RuntimeError(
                "Preset MCP selection must be project-allowed and runtime-available. "
                f"Invalid: {invalid_text}"
            )
    elif dry_run:
        selected_mcp_names = sorted(set(preselected_names))
    else:
        selected_mcp_names = _select_mcp_interactive(
            picker_pool_names,
            preselected_names,
            registry_meta,
            available_servers,
            Option,
            multi_select_items,
            show_all_registry_names=True,
        )

    if FORCED_MCP_SERVER in available_servers:
        selected_mcp_names = _normalize_names([*selected_mcp_names, FORCED_MCP_SERVER])
    else:
        print(
            f"[warning] {FORCED_MCP_SERVER} not found in launcher MCP set; continuing without forced attach",
            file=sys.stderr,
        )

    unknown = [name for name in selected_mcp_names if name not in available_servers]
    if unknown:
        unknown_text = ", ".join(sorted(unknown))
        raise RuntimeError(f"Selected MCP servers are unavailable in current launcher context: {unknown_text}")

    if not dry_run:
        with closing(sqlite3.connect(db_path)) as conn:
            _ensure_wire_schema(conn)
            _persist_session_mcp_names(conn, selected_session.global_session_id, selected_mcp_names)
            launch_selection = _persist_session_launch_selection(conn, selected_session, launch_selection)
            current_external_session_id = _load_session_external_id(
                conn,
                selected_session.global_session_id,
                launch_selection.backend,
            )
    else:
        launch_selection = SessionLaunchSelection(
            backend=normalize_backend_name(launch_selection.backend),
            model=launch_selection.model.strip(),
            reasoning=launch_selection.reasoning.strip(),
        )
        current_external_session_id = (
            selected_session.external_session_id
            if normalize_backend_name(selected_session.cli_backend) == launch_selection.backend
            else ""
        )

    selected_session = SessionRow(
        session_id=selected_session.session_id,
        global_session_id=selected_session.global_session_id,
        task_description=selected_session.task_description,
        status=selected_session.status,
        cli_backend=launch_selection.backend,
        cli_model=launch_selection.model,
        cli_reasoning=launch_selection.reasoning,
        external_session_id=current_external_session_id,
    )

    selected_servers = {name: dict(available_servers[name]) for name in selected_mcp_names}

    selected_config_paths: dict[str, Path] = {}
    if "sqlite" in selected_servers:
        sqlite_config = ensure_sqlite_scaffold(cwd)
        if sqlite_config is None:
            raise RuntimeError("Unable to scaffold sqliteMCP.toml for selected sqlite server.")
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

    if not dry_run:
        adapter.ensure_runtime_files(cwd, selected_servers, available_servers)

    model = launch_selection.model
    effort = launch_selection.reasoning
    llm_selection = LLMSelection(
        display_model=model,
        detail=_reasoning_label(model, effort),
        provider_slug="openai",
        model=model,
        reasoning_effort=effort,
        requires_provider_override=False,
    )
    execution_prefs = ExecutionPreferences(dangerous_sandbox=True, auto_approve=True)

    codex_args: list[str] = []
    codex_args.extend(adapter.build_llm_args(llm_selection))
    codex_args.extend(adapter.build_execution_args(execution_prefs))
    codex_args.extend(list(passthrough_args))

    option_args = [*codex_args, *adapter.build_mcp_flags(selected_servers, available_servers)]
    resume_external_session_id: str | None = None
    if selected_session.status != "pending":
        resume_external_session_id = _resolve_netrunner_resume_session_id(
            cwd,
            selected_session,
            Option,
            single_select_items,
        )
        with closing(sqlite3.connect(db_path)) as conn:
            _ensure_wire_schema(conn)
            _save_session_external_id(
                conn,
                selected_session.global_session_id,
                launch_selection.backend,
                resume_external_session_id,
            )

    if resume_external_session_id:
        codex_cmd = adapter.build_resume_command(option_args, resume_external_session_id)
    else:
        codex_cmd = [adapter.command, *option_args]
        prompt = _build_netrunner_prompt(
            selected_session.session_id,
            selected_mcp_names,
            _build_mcp_how_to_map(selected_mcp_names, registry_meta),
        )
        if prompt:
            codex_cmd.extend(adapter.build_prompt_args(prompt))

    llm_env = _load_llm_env()
    env = _merge_env_with_os(llm_env)
    adapter.prepare_env(env, llm_selection)
    for server_name, config_path in selected_config_paths.items():
        env_var = config_env_vars.get(server_name)
        if env_var:
            env[env_var] = str(config_path)

    print(f"[fixer-wire] netrunner session: {selected_session.session_id}")
    print(f"[fixer-wire] netrunner backend: {launch_selection.backend}")
    print(f"[fixer-wire] netrunner model: {launch_selection.model}")
    print(f"[fixer-wire] netrunner reasoning: {launch_selection.reasoning}")
    if resume_external_session_id:
        print(f"[fixer-wire] resuming {launch_selection.backend} session id: {resume_external_session_id}")
    print(f"[fixer-wire] netrunner MCP selection: {', '.join(selected_mcp_names) if selected_mcp_names else 'none'}")
    print("[fixer-wire] command:", codex_cmd)
    if dry_run:
        return 0
    before_matches: set[str] = set()
    if launch_selection.backend == "codex" and not resume_external_session_id:
        try:
            before_match = _latest_matching_netrunner_codex_session_id(cwd, selected_session.session_id)
            if before_match:
                before_matches.add(before_match)
        except RuntimeError:
            before_matches = set()
    before = _latest_codex_session_id_for_cwd(cwd) if launch_selection.backend == "codex" else None
    result = subprocess.call(codex_cmd, env=env)
    linked_session_id: str | None = None
    if resume_external_session_id:
        linked_session_id = resume_external_session_id
    else:
        if launch_selection.backend == "codex":
            try:
                after_match = _latest_matching_netrunner_codex_session_id(cwd, selected_session.session_id)
            except RuntimeError:
                after_match = None
            if after_match and after_match not in before_matches:
                linked_session_id = after_match
            elif after_match:
                linked_session_id = after_match
            else:
                after = _latest_codex_session_id_for_cwd(cwd)
                if after and after != before:
                    linked_session_id = after
        elif result == 0:
            linked_session_id = _prompt_resume_session_id(selected_session.session_id, launch_selection.backend)
    if linked_session_id:
        with closing(sqlite3.connect(db_path)) as conn:
            _ensure_wire_schema(conn)
            _save_session_external_id(
                conn,
                selected_session.global_session_id,
                launch_selection.backend,
                linked_session_id,
            )
    return result


def main(argv: Sequence[str] | None = None) -> int:
    raw_args = list(sys.argv[1:] if argv is None else argv)
    wire_args, passthrough_args = _parse_wire_args(raw_args)

    if wire_args.scaffold_mvp:
        if wire_args.role:
            print("[fixer-wire] `--scaffold-mvp` cannot be combined with `--role`.", file=sys.stderr)
            return 2
        if passthrough_args:
            extra = " ".join(passthrough_args)
            print(
                f"[fixer-wire] Scaffold mode does not accept passthrough Codex args. Unexpected: {extra}",
                file=sys.stderr,
            )
            return 2
        return run_scaffold_cli(
            wire_args.scaffold_mvp,
            target_dir=wire_args.scaffold_target_dir,
            dry_run=wire_args.dry_run,
        )

    try:
        mcp_root = bootstrap_codex_pro_import_path()
    except RuntimeError as exc:
        print(f"[fixer-wire] {exc}", file=sys.stderr)
        return 2

    if wire_args.wire_info:
        for line in wire_info_lines(mcp_root):
            print(line)
        if not passthrough_args:
            return 0

    import codex_pro_app.main as codex_main
    from codex_pro_app.ui import Option, multi_select_items, single_select_items

    role = wire_args.role or _select_role_interactive(Option, single_select_items)
    if role == SCAFFOLD_MVP_ACTION:
        try:
            return _launch_scaffold_interactive(Option, single_select_items)
        except RuntimeError as exc:
            print(f"[fixer-wire] {exc}", file=sys.stderr)
            return 2

    if role == "netrunner":
        try:
            return _launch_netrunner(
                passthrough_args,
                preset_session_id=wire_args.netrunner_session_id,
                preset_backend=wire_args.netrunner_backend,
                preset_model=wire_args.netrunner_model,
                preset_reasoning=wire_args.netrunner_reasoning,
                preset_mcp_names=wire_args.netrunner_mcp,
                dry_run=wire_args.dry_run,
                Option=Option,
                single_select_items=single_select_items,
                multi_select_items=multi_select_items,
            )
        except RuntimeError as exc:
            print(f"[fixer-wire] {exc}", file=sys.stderr)
            return 2

    if role == "fixer":
        try:
            return _launch_fixer(
                passthrough_args,
                dry_run=wire_args.dry_run,
                Option=Option,
                single_select_items=single_select_items,
            )
        except RuntimeError as exc:
            print(f"[fixer-wire] {exc}", file=sys.stderr)
            return 2

    forced_args = []
    if not any("mcp_servers.fixer_mcp" in arg for arg in passthrough_args):
        forced_args = _build_forced_fixer_override_args()
    if forced_args:
        print("[fixer-wire] forcing fixer_mcp attachment for role launch")

    if role == "overseer":
        passthrough_args = _ensure_passthrough_dangerous_sandbox(passthrough_args)
        _prefer_fixed_model_for_role_presets(codex_main)

    return codex_main.run([*passthrough_args, *forced_args], preset_override=role)


if __name__ == "__main__":
    raise SystemExit(main())
