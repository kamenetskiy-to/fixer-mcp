from __future__ import annotations

import io
import sqlite3
import sys
import tempfile
import unittest
from contextlib import redirect_stdout
from pathlib import Path
from unittest.mock import patch

from client_wires import fixer_autopilot, fixer_wire


class _FakeProcess:
    def __init__(self, return_code: int | None = None) -> None:
        self._return_code = return_code

    def poll(self) -> int | None:
        return self._return_code


def _seed_autopilot_db(db_path: Path, project_cwd: Path) -> None:
    conn = sqlite3.connect(db_path)
    try:
        fixer_wire._ensure_wire_schema(conn)
        conn.executescript(
            f"""
            CREATE TABLE project (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                name TEXT NOT NULL,
                cwd TEXT UNIQUE NOT NULL
            );
            CREATE TABLE session (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                project_id INTEGER NOT NULL,
                task_description TEXT NOT NULL,
                status TEXT NOT NULL
            );
            CREATE TABLE mcp_server (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                name TEXT NOT NULL UNIQUE
            );
            CREATE TABLE session_mcp_server (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                session_id INTEGER NOT NULL,
                mcp_server_id INTEGER NOT NULL
            );
            CREATE TABLE netrunner_attached_doc (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                session_id INTEGER NOT NULL,
                project_doc_id INTEGER NOT NULL
            );
            INSERT INTO project (id, name, cwd) VALUES (1, 'Project', '{project_cwd.resolve()}');
            INSERT INTO session (id, project_id, task_description, status) VALUES
                (1, 1, '## Goal\nFirst pending session', 'pending'),
                (2, 1, '## Goal\nNo docs yet', 'pending'),
                (3, 1, '## Goal\nAlready running', 'in_progress');
            INSERT INTO mcp_server (id, name) VALUES
                (1, 'fixer_mcp'),
                (2, 'tavily');
            INSERT INTO session_mcp_server (session_id, mcp_server_id) VALUES
                (1, 1),
                (1, 2),
                (2, 1);
            INSERT INTO netrunner_attached_doc (session_id, project_doc_id) VALUES
                (1, 11);
            """
        )
    finally:
        conn.close()


class FixerAutopilotTests(unittest.TestCase):
    def test_load_dispatchable_sessions_only_returns_pending(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            db_path = Path(tmp) / "fixer.db"
            project_cwd = Path(tmp) / "project"
            project_cwd.mkdir()
            _seed_autopilot_db(db_path, project_cwd)
            conn = sqlite3.connect(db_path)
            try:
                sessions = fixer_autopilot.load_dispatchable_sessions(conn, 1)
            finally:
                conn.close()

        self.assertEqual([session.local_session_id for session in sessions], [1, 2])
        self.assertEqual(sessions[0].mcp_names, ("fixer_mcp", "tavily"))
        self.assertEqual(sessions[0].attached_doc_count, 1)
        self.assertEqual(sessions[1].attached_doc_count, 0)

    def test_build_netrunner_command_includes_preselected_mcp(self) -> None:
        session = fixer_autopilot.DispatchableSession(
            local_session_id=4,
            global_session_id=9,
            task_description="Task",
            status="pending",
            mcp_names=("fixer_mcp", "tavily"),
            attached_doc_count=1,
        )

        command = fixer_autopilot.build_netrunner_command(
            Path("/repo"),
            session,
        )

        self.assertEqual(command[:5], [sys.executable, "/repo/client_wires/fixer_wire.py", "--role", "netrunner", "--netrunner-session-id"])
        self.assertIn("4", command)
        self.assertIn("--netrunner-mcp", command)
        self.assertIn("fixer_mcp,tavily", command)

    def test_dispatch_pending_sessions_skips_missing_docs_and_launches_eligible(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            db_path = Path(tmp) / "fixer.db"
            project_cwd = Path(tmp) / "project"
            project_cwd.mkdir()
            _seed_autopilot_db(db_path, project_cwd)
            launched_commands: list[list[str]] = []
            output = io.StringIO()

            def fake_launcher(command: list[str], cwd: Path) -> _FakeProcess:
                launched_commands.append(command)
                self.assertEqual(cwd, project_cwd)
                return _FakeProcess(return_code=None)

            with (
                patch.object(fixer_wire, "_resolve_fixer_db_path", return_value=db_path),
                patch.object(fixer_wire, "_repo_root", return_value=Path("/repo")),
                redirect_stdout(output),
            ):
                active_runs: dict[int, fixer_autopilot.ActiveRun] = {}
                retry_entries: dict[int, fixer_autopilot.RetryEntry] = {}
                launched = fixer_autopilot.dispatch_pending_sessions(
                    project_cwd,
                    max_parallel=2,
                    active_runs=active_runs,
                    retry_entries=retry_entries,
                    dry_run=False,
                    launcher=fake_launcher,
                )

        self.assertEqual(launched, 1)
        self.assertEqual(len(launched_commands), 1)
        self.assertIn("skip session 2: no attached docs", output.getvalue())
        self.assertIn(1, active_runs)

    def test_reap_finished_runs_creates_retry_entry_for_failures(self) -> None:
        session = fixer_autopilot.DispatchableSession(
            local_session_id=3,
            global_session_id=7,
            task_description="Retry me",
            status="pending",
            mcp_names=("fixer_mcp",),
            attached_doc_count=1,
        )
        active_runs = {
            7: fixer_autopilot.ActiveRun(
                session=session,
                process=_FakeProcess(return_code=9),
                attempt=1,
                command=["cmd"],
            )
        }
        retry_entries: dict[int, fixer_autopilot.RetryEntry] = {}

        fixer_autopilot.reap_finished_runs(
            active_runs,
            retry_entries,
            max_retry_delay_sec=300.0,
            now_monotonic=100.0,
        )

        self.assertEqual(active_runs, {})
        self.assertIn(7, retry_entries)
        self.assertEqual(retry_entries[7].attempt, 2)
        self.assertGreater(retry_entries[7].due_at_monotonic, 100.0)


if __name__ == "__main__":
    unittest.main()
