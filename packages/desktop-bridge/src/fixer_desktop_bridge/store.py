from __future__ import annotations

import json
import sqlite3
from collections import Counter
from pathlib import Path
from typing import Any


def parse_json_list(raw: str | None) -> list[str]:
    if not raw:
        return []
    try:
        value = json.loads(raw)
    except json.JSONDecodeError:
        return []
    if not isinstance(value, list):
        return []
    return [str(item) for item in value]


def resolve_default_db_path(start: Path | None = None) -> Path:
    origin = (start or Path.cwd()).resolve()
    for base in (origin, *origin.parents):
        for candidate in (base / "fixer_mcp" / "fixer.db", base / "fixer.db"):
            if candidate.is_file():
                return candidate
    raise FileNotFoundError(
        "could not resolve Fixer SQLite path; pass --db-path or set the working directory near fixer_mcp/fixer.db"
    )


class FixerDesktopStore:
    def __init__(self, db_path: Path) -> None:
        self.db_path = db_path.resolve()

    def _connect(self) -> sqlite3.Connection:
        connection = sqlite3.connect(self.db_path)
        connection.row_factory = sqlite3.Row
        return connection

    def health(self) -> dict[str, Any]:
        return {"ok": True, "db_path": str(self.db_path)}

    def list_projects(self) -> list[dict[str, Any]]:
        with self._connect() as connection:
            projects = connection.execute(
                "SELECT id, name, cwd FROM project ORDER BY name COLLATE NOCASE"
            ).fetchall()
            return [self._project_summary(connection, project["id"], project) for project in projects]

    def get_project_dashboard(self, project_id: int) -> dict[str, Any]:
        with self._connect() as connection:
            project = connection.execute(
                "SELECT id, name, cwd FROM project WHERE id = ?",
                (project_id,),
            ).fetchone()
            if project is None:
                raise KeyError(f"unknown project id {project_id}")

            dashboard = self._project_summary(connection, project_id, project)
            sessions = connection.execute(
                """
                SELECT
                    s.id,
                    s.project_id,
                    s.task_description,
                    s.status,
                    s.report,
                    s.declared_write_scope,
                    s.rework_count,
                    s.forced_stop_count,
                    s.cli_backend,
                    s.cli_model,
                    s.cli_reasoning,
                    ext.external_session_id,
                    ext.updated_at AS external_updated_at,
                    wp.status AS worker_status,
                    wp.pid AS worker_pid,
                    wp.updated_at AS worker_updated_at
                FROM session AS s
                LEFT JOIN session_external_link AS ext
                    ON ext.id = (
                        SELECT ext2.id
                        FROM session_external_link AS ext2
                        WHERE ext2.session_id = s.id
                        ORDER BY ext2.updated_at DESC, ext2.id DESC
                        LIMIT 1
                    )
                LEFT JOIN worker_process AS wp
                    ON wp.id = (
                        SELECT wp2.id
                        FROM worker_process AS wp2
                        WHERE wp2.session_id = s.id
                        ORDER BY wp2.updated_at DESC, wp2.id DESC
                        LIMIT 1
                    )
                WHERE s.project_id = ?
                ORDER BY
                    CASE s.status
                        WHEN 'in_progress' THEN 0
                        WHEN 'review' THEN 1
                        WHEN 'pending' THEN 2
                        ELSE 3
                    END,
                    s.id DESC
                """,
                (project_id,),
            ).fetchall()

            session_ids = [int(session["id"]) for session in sessions]
            attached_counts = self._count_by_session(
                connection,
                "netrunner_attached_doc",
                session_ids,
                "project_doc_id",
            )
            proposal_counts = self._count_by_session(connection, "doc_proposal", session_ids, "id")
            pending_proposal_counts = self._count_by_session(
                connection,
                "doc_proposal",
                session_ids,
                "id",
                extra_where="status = 'pending'",
            )
            mcp_counts = self._count_by_session(
                connection,
                "session_mcp_server",
                session_ids,
                "mcp_server_id",
            )

            dashboard["run_status"] = self._run_status(connection, project_id)
            dashboard["sessions"] = [
                {
                    "id": int(session["id"]),
                    "project_id": int(session["project_id"]),
                    "status": session["status"],
                    "task_title": self._task_title(session["task_description"]),
                    "task_description": session["task_description"],
                    "report_excerpt": self._report_excerpt(session["report"]),
                    "write_scope": parse_json_list(session["declared_write_scope"]),
                    "cli_backend": session["cli_backend"],
                    "cli_model": session["cli_model"],
                    "cli_reasoning": session["cli_reasoning"],
                    "external_session_id": session["external_session_id"] or "",
                    "external_updated_at": session["external_updated_at"] or "",
                    "worker_status": session["worker_status"] or "",
                    "worker_pid": session["worker_pid"],
                    "worker_updated_at": session["worker_updated_at"] or "",
                    "attached_doc_count": attached_counts.get(int(session["id"]), 0),
                    "proposal_count": proposal_counts.get(int(session["id"]), 0),
                    "pending_proposal_count": pending_proposal_counts.get(int(session["id"]), 0),
                    "mcp_server_count": mcp_counts.get(int(session["id"]), 0),
                    "rework_count": int(session["rework_count"]),
                    "forced_stop_count": int(session["forced_stop_count"]),
                }
                for session in sessions
            ]
            return dashboard

    def get_session_detail(self, session_id: int) -> dict[str, Any]:
        with self._connect() as connection:
            session = connection.execute(
                """
                SELECT
                    s.id,
                    s.project_id,
                    s.task_description,
                    s.status,
                    s.report,
                    s.declared_write_scope,
                    s.parallel_wave_id,
                    s.repair_source_session_id,
                    s.rework_count,
                    s.forced_stop_count,
                    s.cli_backend,
                    s.cli_model,
                    s.cli_reasoning,
                    p.name AS project_name,
                    p.cwd AS project_cwd
                FROM session AS s
                JOIN project AS p ON p.id = s.project_id
                WHERE s.id = ?
                """,
                (session_id,),
            ).fetchone()
            if session is None:
                raise KeyError(f"unknown session id {session_id}")

            external_links = connection.execute(
                """
                SELECT backend, external_session_id, updated_at
                FROM session_external_link
                WHERE session_id = ?
                ORDER BY updated_at DESC, id DESC
                """,
                (session_id,),
            ).fetchall()
            attached_docs = connection.execute(
                """
                SELECT d.id, d.title, d.doc_type
                FROM netrunner_attached_doc AS attached
                JOIN project_doc AS d ON d.id = attached.project_doc_id
                WHERE attached.session_id = ?
                ORDER BY d.id
                """,
                (session_id,),
            ).fetchall()
            mcp_servers = connection.execute(
                """
                SELECT server.name, server.category, server.short_description, server.how_to
                FROM session_mcp_server AS selected
                JOIN mcp_server AS server ON server.id = selected.mcp_server_id
                WHERE selected.session_id = ?
                ORDER BY server.name COLLATE NOCASE
                """,
                (session_id,),
            ).fetchall()
            proposals = connection.execute(
                """
                SELECT id, status, proposed_doc_type, target_project_doc_id
                FROM doc_proposal
                WHERE session_id = ?
                ORDER BY
                    CASE status
                        WHEN 'pending' THEN 0
                        ELSE 1
                    END,
                    id DESC
                """,
                (session_id,),
            ).fetchall()
            worker_processes = connection.execute(
                """
                SELECT pid, status, stop_reason, started_at, updated_at, stopped_at
                FROM worker_process
                WHERE session_id = ?
                ORDER BY updated_at DESC, id DESC
                """,
                (session_id,),
            ).fetchall()

            return {
                "id": int(session["id"]),
                "project": {
                    "id": int(session["project_id"]),
                    "name": session["project_name"],
                    "cwd": session["project_cwd"],
                },
                "status": session["status"],
                "task_title": self._task_title(session["task_description"]),
                "task_description": session["task_description"],
                "report": session["report"] or "",
                "write_scope": parse_json_list(session["declared_write_scope"]),
                "parallel_wave_id": session["parallel_wave_id"] or "",
                "repair_source_session_id": session["repair_source_session_id"],
                "rework_count": int(session["rework_count"]),
                "forced_stop_count": int(session["forced_stop_count"]),
                "cli_backend": session["cli_backend"],
                "cli_model": session["cli_model"],
                "cli_reasoning": session["cli_reasoning"],
                "external_links": [
                    {
                        "backend": link["backend"],
                        "external_session_id": link["external_session_id"],
                        "updated_at": link["updated_at"],
                    }
                    for link in external_links
                ],
                "attached_docs": [
                    {
                        "id": int(doc["id"]),
                        "title": doc["title"],
                        "doc_type": doc["doc_type"],
                    }
                    for doc in attached_docs
                ],
                "mcp_servers": [
                    {
                        "name": server["name"],
                        "category": server["category"] or "",
                        "short_description": server["short_description"] or "",
                        "how_to": server["how_to"] or "",
                    }
                    for server in mcp_servers
                ],
                "doc_proposals": [
                    {
                        "id": int(proposal["id"]),
                        "status": proposal["status"],
                        "proposed_doc_type": proposal["proposed_doc_type"] or "",
                        "target_project_doc_id": proposal["target_project_doc_id"],
                    }
                    for proposal in proposals
                ],
                "worker_processes": [
                    {
                        "pid": int(process["pid"]),
                        "status": process["status"],
                        "stop_reason": process["stop_reason"] or "",
                        "started_at": process["started_at"],
                        "updated_at": process["updated_at"],
                        "stopped_at": process["stopped_at"] or "",
                    }
                    for process in worker_processes
                ],
                "run_status": self._run_status(connection, int(session["project_id"])),
            }

    def _count_by_session(
        self,
        connection: sqlite3.Connection,
        table_name: str,
        session_ids: list[int],
        count_column: str,
        *,
        extra_where: str = "",
    ) -> dict[int, int]:
        if not session_ids:
            return {}
        placeholders = ",".join("?" for _ in session_ids)
        query = (
            f"SELECT session_id, COUNT({count_column}) AS count "
            f"FROM {table_name} WHERE session_id IN ({placeholders})"
        )
        if extra_where:
            query += f" AND {extra_where}"
        query += " GROUP BY session_id"
        rows = connection.execute(query, session_ids).fetchall()
        return {int(row["session_id"]): int(row["count"]) for row in rows}

    def _project_summary(
        self,
        connection: sqlite3.Connection,
        project_id: int,
        project: sqlite3.Row,
    ) -> dict[str, Any]:
        session_rows = connection.execute(
            "SELECT status FROM session WHERE project_id = ?",
            (project_id,),
        ).fetchall()
        session_counts = Counter(str(row["status"]) for row in session_rows)
        pending_proposals = connection.execute(
            "SELECT COUNT(*) FROM doc_proposal WHERE project_id = ? AND status = 'pending'",
            (project_id,),
        ).fetchone()[0]
        active_workers = connection.execute(
            "SELECT COUNT(*) FROM worker_process WHERE project_id = ? AND status = 'running'",
            (project_id,),
        ).fetchone()[0]
        latest_session = connection.execute(
            """
            SELECT id, status, cli_backend, cli_model, cli_reasoning
            FROM session
            WHERE project_id = ?
            ORDER BY id DESC
            LIMIT 1
            """,
            (project_id,),
        ).fetchone()
        latest_run = self._run_status(connection, project_id)
        return {
            "id": int(project["id"]),
            "name": project["name"],
            "cwd": project["cwd"],
            "session_counts": dict(session_counts),
            "pending_doc_proposals": int(pending_proposals),
            "active_worker_count": int(active_workers),
            "latest_run_status": latest_run,
            "latest_session": {
                "id": int(latest_session["id"]),
                "status": latest_session["status"],
                "cli_backend": latest_session["cli_backend"],
                "cli_model": latest_session["cli_model"],
                "cli_reasoning": latest_session["cli_reasoning"],
            }
            if latest_session
            else None,
        }

    def _run_status(self, connection: sqlite3.Connection, project_id: int) -> dict[str, Any] | None:
        row = connection.execute(
            """
            SELECT
                state,
                summary,
                focus,
                blocker,
                evidence,
                updated_at,
                orchestration_frozen
            FROM autonomous_run_status
            WHERE project_id = ?
            ORDER BY updated_at DESC, id DESC
            LIMIT 1
            """,
            (project_id,),
        ).fetchone()
        if row is None:
            return None
        return {
            "state": row["state"],
            "summary": row["summary"],
            "focus": row["focus"] or "",
            "blocker": row["blocker"] or "",
            "evidence": row["evidence"] or "",
            "updated_at": row["updated_at"],
            "orchestration_frozen": bool(row["orchestration_frozen"]),
        }

    @staticmethod
    def _task_title(task_description: str) -> str:
        for line in task_description.splitlines():
            stripped = line.strip()
            if stripped:
                return stripped[:96]
        return "Untitled session"

    @staticmethod
    def _report_excerpt(report: str | None) -> str:
        if not report:
            return ""
        collapsed = " ".join(report.split())
        if len(collapsed) <= 180:
            return collapsed
        return collapsed[:177] + "..."
