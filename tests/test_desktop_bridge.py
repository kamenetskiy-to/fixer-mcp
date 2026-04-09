from __future__ import annotations

import json
import sqlite3
import tempfile
import threading
import time
import unittest
from pathlib import Path
from urllib.request import ProxyHandler, build_opener
import sys

REPO_ROOT = Path(__file__).resolve().parents[1]
BRIDGE_SRC = REPO_ROOT / "packages" / "desktop-bridge" / "src"
if str(BRIDGE_SRC) not in sys.path:
    sys.path.insert(0, str(BRIDGE_SRC))

from fixer_desktop_bridge.app import serve
from fixer_desktop_bridge.store import FixerDesktopStore, resolve_default_db_path


SCHEMA = """
CREATE TABLE project (id INTEGER PRIMARY KEY, name TEXT NOT NULL, cwd TEXT NOT NULL);
CREATE TABLE session (
  id INTEGER PRIMARY KEY,
  project_id INTEGER,
  task_description TEXT NOT NULL,
  status TEXT NOT NULL,
  report TEXT,
  declared_write_scope TEXT NOT NULL DEFAULT '["."]',
  parallel_wave_id TEXT NOT NULL DEFAULT '',
  repair_source_session_id INTEGER,
  rework_count INTEGER NOT NULL DEFAULT 0,
  forced_stop_count INTEGER NOT NULL DEFAULT 0,
  cli_backend TEXT NOT NULL DEFAULT 'codex',
  cli_model TEXT NOT NULL DEFAULT '',
  cli_reasoning TEXT NOT NULL DEFAULT ''
);
CREATE TABLE project_doc (
  id INTEGER PRIMARY KEY,
  project_id INTEGER,
  title TEXT NOT NULL,
  content TEXT NOT NULL,
  doc_type TEXT DEFAULT 'documentation'
);
CREATE TABLE doc_proposal (
  id INTEGER PRIMARY KEY,
  project_id INTEGER,
  session_id INTEGER,
  status TEXT NOT NULL,
  proposed_content TEXT NOT NULL,
  proposed_doc_type TEXT DEFAULT 'documentation',
  target_project_doc_id INTEGER
);
CREATE TABLE mcp_server (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  short_description TEXT,
  long_description TEXT,
  auto_attach INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  is_default INTEGER NOT NULL DEFAULT 0,
  category TEXT,
  how_to TEXT
);
CREATE TABLE session_mcp_server (
  id INTEGER PRIMARY KEY,
  session_id INTEGER NOT NULL,
  mcp_server_id INTEGER NOT NULL,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE netrunner_attached_doc (
  id INTEGER PRIMARY KEY,
  session_id INTEGER NOT NULL,
  project_doc_id INTEGER NOT NULL,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE autonomous_run_status (
  id INTEGER PRIMARY KEY,
  project_id INTEGER NOT NULL,
  session_id INTEGER,
  state TEXT NOT NULL,
  summary TEXT NOT NULL,
  focus TEXT,
  blocker TEXT,
  evidence TEXT,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  orchestration_epoch INTEGER NOT NULL DEFAULT 0,
  orchestration_frozen INTEGER NOT NULL DEFAULT 0,
  notifications_enabled_for_active_run INTEGER NOT NULL DEFAULT 1
);
CREATE TABLE worker_process (
  id INTEGER PRIMARY KEY,
  project_id INTEGER NOT NULL,
  session_id INTEGER NOT NULL,
  pid INTEGER NOT NULL,
  launch_epoch INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'running',
  stop_reason TEXT,
  started_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  stopped_at TEXT
);
CREATE TABLE session_external_link (
  id INTEGER PRIMARY KEY,
  session_id INTEGER NOT NULL,
  backend TEXT NOT NULL,
  external_session_id TEXT NOT NULL,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
"""


def build_fixture_db(db_path: Path) -> None:
    connection = sqlite3.connect(db_path)
    try:
        connection.executescript(SCHEMA)
        connection.executescript(
            """
            INSERT INTO project (id, name, cwd)
            VALUES (2, 'Fixer MCP', '/tmp/self_orchestration');

            INSERT INTO session (
              id, project_id, task_description, status, report, declared_write_scope,
              rework_count, forced_stop_count, cli_backend, cli_model, cli_reasoning
            )
            VALUES
              (91, 2, 'Review the launcher package', 'review', 'Reviewed and ready.', '["github_repo/tests"]', 1, 0, 'codex', 'gpt-5.4', 'medium'),
              (92, 2, 'Build the first desktop slice', 'in_progress', '', '["github_repo/apps","github_repo/packages"]', 0, 0, 'codex', 'gpt-5.4', 'high');

            INSERT INTO project_doc (id, project_id, title, content, doc_type)
            VALUES
              (11, 2, 'Migration Plan', 'doc', 'architecture'),
              (12, 2, 'Runtime Modes', 'doc', 'documentation');

            INSERT INTO netrunner_attached_doc (session_id, project_doc_id)
            VALUES (92, 11), (92, 12);

            INSERT INTO mcp_server (id, name, short_description, category, how_to)
            VALUES
              (1, 'fixer_mcp', 'Fixer orchestration tools', 'Control', 'Use for project-bound operations.'),
              (2, 'sqlite', 'SQLite access', 'DB', 'Use for schema checks.'),
              (3, 'gopls', 'Go tooling', 'Coding', 'Use for semantic Go tooling.');

            INSERT INTO session_mcp_server (session_id, mcp_server_id)
            VALUES (92, 1), (92, 2), (92, 3);

            INSERT INTO doc_proposal (id, project_id, session_id, status, proposed_content, proposed_doc_type, target_project_doc_id)
            VALUES
              (7, 2, 92, 'pending', 'desktop bridge contract', 'architecture', 11),
              (8, 2, 91, 'approved', 'launcher cleanup', 'documentation', 12);

            INSERT INTO autonomous_run_status (project_id, session_id, state, summary, focus, blocker, evidence, updated_at, orchestration_frozen)
            VALUES
              (2, 92, 'running', 'Desktop slice in progress', 'desktop bridge + app shell', '', 'active worker', '2026-04-04 10:00:00', 0);

            INSERT INTO worker_process (project_id, session_id, pid, status, updated_at)
            VALUES
              (2, 92, 44123, 'running', '2026-04-04 10:00:01'),
              (2, 91, 44001, 'exited', '2026-04-04 09:00:00');

            INSERT INTO session_external_link (session_id, backend, external_session_id, updated_at)
            VALUES (92, 'codex', 'session-xyz', '2026-04-04 10:00:02');
            """
        )
        connection.commit()
    finally:
        connection.close()


class FixerDesktopStoreTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temp_dir = tempfile.TemporaryDirectory()
        self.db_path = Path(self.temp_dir.name) / "fixer.db"
        build_fixture_db(self.db_path)
        self.store = FixerDesktopStore(self.db_path)

    def tearDown(self) -> None:
        self.temp_dir.cleanup()

    def test_list_projects_returns_summary_counts(self) -> None:
        projects = self.store.list_projects()
        self.assertEqual(len(projects), 1)
        project = projects[0]
        self.assertEqual(project["name"], "Fixer MCP")
        self.assertEqual(project["session_counts"]["review"], 1)
        self.assertEqual(project["session_counts"]["in_progress"], 1)
        self.assertEqual(project["pending_doc_proposals"], 1)
        self.assertEqual(project["active_worker_count"], 1)

    def test_get_project_dashboard_includes_sessions_and_run_status(self) -> None:
        dashboard = self.store.get_project_dashboard(2)
        self.assertEqual(dashboard["id"], 2)
        self.assertEqual(dashboard["run_status"]["state"], "running")
        self.assertEqual(len(dashboard["sessions"]), 2)
        session = dashboard["sessions"][0]
        self.assertEqual(session["id"], 92)
        self.assertEqual(session["attached_doc_count"], 2)
        self.assertEqual(session["mcp_server_count"], 3)
        self.assertEqual(session["pending_proposal_count"], 1)
        self.assertEqual(session["worker_status"], "running")

    def test_get_session_detail_returns_workspace_payload(self) -> None:
        detail = self.store.get_session_detail(92)
        self.assertEqual(detail["project"]["name"], "Fixer MCP")
        self.assertEqual(detail["cli_backend"], "codex")
        self.assertEqual(detail["write_scope"], ["github_repo/apps", "github_repo/packages"])
        self.assertEqual(len(detail["attached_docs"]), 2)
        self.assertEqual(len(detail["mcp_servers"]), 3)
        self.assertEqual(detail["external_links"][0]["external_session_id"], "session-xyz")
        self.assertEqual(detail["worker_processes"][0]["pid"], 44123)

    def test_resolve_default_db_path_prefers_fixer_mcp_db(self) -> None:
        repo_root = Path(self.temp_dir.name) / "repo"
        nested = repo_root / "github_repo" / "packages" / "desktop-bridge"
        fixer_dir = repo_root / "fixer_mcp"
        nested.mkdir(parents=True)
        fixer_dir.mkdir(parents=True)
        target = fixer_dir / "fixer.db"
        target.write_text("", encoding="utf-8")
        self.assertEqual(resolve_default_db_path(nested), target.resolve())


class FixerDesktopBridgeHttpTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temp_dir = tempfile.TemporaryDirectory()
        self.db_path = Path(self.temp_dir.name) / "fixer.db"
        build_fixture_db(self.db_path)
        self.store = FixerDesktopStore(self.db_path)
        self.host = "127.0.0.1"
        self.port = 8876
        self.thread = threading.Thread(
            target=serve,
            kwargs={"host": self.host, "port": self.port, "store": self.store},
            daemon=True,
        )
        self.thread.start()
        time.sleep(0.2)
        self.opener = build_opener(ProxyHandler({}))

    def tearDown(self) -> None:
        self.temp_dir.cleanup()

    def test_http_routes_return_json_payloads(self) -> None:
        with self.opener.open(f"http://{self.host}:{self.port}/api/projects") as response:
            payload = json.loads(response.read().decode("utf-8"))
        self.assertEqual(payload["projects"][0]["id"], 2)

        with self.opener.open(
            f"http://{self.host}:{self.port}/api/projects/2/dashboard"
        ) as response:
            dashboard = json.loads(response.read().decode("utf-8"))
        self.assertEqual(dashboard["sessions"][0]["id"], 92)

        with self.opener.open(f"http://{self.host}:{self.port}/api/sessions/92") as response:
            session = json.loads(response.read().decode("utf-8"))
        self.assertEqual(session["mcp_servers"][0]["name"], "fixer_mcp")


if __name__ == "__main__":
    unittest.main()
