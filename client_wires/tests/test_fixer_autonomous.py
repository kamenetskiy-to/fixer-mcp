from __future__ import annotations

import json
import os
import sqlite3
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from client_wires import fixer_autonomous, fixer_wire, launch_env


class _TrackingConnection:
    def __init__(self, inner: sqlite3.Connection) -> None:
        self._inner = inner
        self.closed = False

    def close(self) -> None:
        self.closed = True
        self._inner.close()

    def __getattr__(self, name: str) -> object:
        return getattr(self._inner, name)

    def __enter__(self) -> "_TrackingConnection":
        return self

    def __exit__(self, exc_type: object, exc: object, tb: object) -> None:
        self.close()


class _FakeBackendAdapter:
    command = "codex"

    @staticmethod
    def build_mcp_flags(selected_servers: dict[str, object], _available: dict[str, object]) -> list[str]:
        return [f"--mcp={','.join(sorted(selected_servers.keys()))}"] if selected_servers else []

    @staticmethod
    def build_headless_command(
        *,
        model: str,
        reasoning: str,
        selected: dict[str, object],
        available: dict[str, object],
        prompt: str,
    ) -> list[str]:
        del model, reasoning
        return ["codex", *_FakeBackendAdapter.build_mcp_flags(selected, available), prompt]

    @staticmethod
    def ensure_runtime_files(
        _cwd: Path,
        _selection: object,
        _selected: dict[str, object],
        _available: dict[str, object],
    ) -> None:
        return None

    @staticmethod
    def prepare_env(_env: dict[str, str], _selection: object) -> None:
        return None


def _fake_available_servers() -> tuple[dict[str, dict[str, object]], dict[str, str], _FakeBackendAdapter, object]:
    return (
        {fixer_wire.FORCED_MCP_SERVER: {"command": "fixer_mcp"}},
        {},
        _FakeBackendAdapter(),
        lambda _cwd: None,
    )


def _seed_launch_db(db_path: Path, session_rows: list[fixer_wire.SessionRow]) -> None:
    conn = sqlite3.connect(db_path)
    try:
        conn.executescript(
            """
            CREATE TABLE session (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                project_id INTEGER NOT NULL,
                task_description TEXT NOT NULL,
                status TEXT NOT NULL,
                cli_backend TEXT NOT NULL DEFAULT 'codex',
                cli_model TEXT NOT NULL DEFAULT '',
                cli_reasoning TEXT NOT NULL DEFAULT ''
            );
            CREATE TABLE session_codex_link (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                session_id INTEGER NOT NULL UNIQUE,
                codex_session_id TEXT NOT NULL,
                updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
            );
            CREATE TABLE session_external_link (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                session_id INTEGER NOT NULL,
                backend TEXT NOT NULL,
                external_session_id TEXT NOT NULL,
                updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
            );
            """
        )
        for row in session_rows:
            conn.execute(
                """
                INSERT INTO session (id, project_id, task_description, status, cli_backend, cli_model, cli_reasoning)
                VALUES (?, 1, ?, ?, ?, ?, ?)
                """,
                (
                    row.global_session_id,
                    row.task_description,
                    row.status,
                    row.cli_backend,
                    row.cli_model,
                    row.cli_reasoning,
                ),
            )
            if row.codex_session_id:
                conn.execute(
                    """
                    INSERT INTO session_codex_link (session_id, codex_session_id)
                    VALUES (?, ?)
                    """,
                    (row.global_session_id, row.codex_session_id),
                )
                conn.execute(
                    """
                    INSERT INTO session_external_link (session_id, backend, external_session_id)
                    VALUES (?, 'codex', ?)
                    """,
                    (row.global_session_id, row.codex_session_id),
                )
        conn.commit()
    finally:
        conn.close()


class FixerAutonomousTests(unittest.TestCase):
    def test_register_fixer_session_writes_state_file(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            cwd = Path(tmp)
            state_path = fixer_autonomous.register_fixer_session(cwd, "fixer-session-123")
            payload = json.loads(state_path.read_text(encoding="utf-8"))

        self.assertEqual(state_path, cwd / ".codex" / "autonomous_resolution.json")
        self.assertEqual(payload["fixer_codex_session_id"], "fixer-session-123")
        self.assertEqual(payload["mode"], "serial_autonomous_resolution")
        self.assertEqual(payload["workflow_type"], "ghost_run")
        self.assertEqual(payload["workflow_label"], "Ghost Run")
        self.assertEqual(payload["project_cwd"], str(cwd))

    def test_register_fixer_session_falls_back_to_codex_thread_id_env(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            cwd = Path(tmp)
            with (
                patch.dict(os.environ, {"CODEX_THREAD_ID": "thread-from-env"}, clear=False),
                patch.object(
                    fixer_autonomous,
                    "_latest_fixer_session_id",
                    side_effect=RuntimeError("history helpers unavailable"),
                ),
            ):
                state_path = fixer_autonomous.register_fixer_session(cwd, None)
                payload = json.loads(state_path.read_text(encoding="utf-8"))

        self.assertEqual(payload["fixer_codex_session_id"], "thread-from-env")

    def test_build_autonomous_netrunner_prompt_contains_no_go_and_wake_step(self) -> None:
        prompt = fixer_autonomous._build_autonomous_netrunner_prompt(
            7,
            ["fixer_mcp", "tavily"],
            "fixer-session-123",
            {
                "fixer_mcp": "Use for project tools.",
                "tavily": "Use for web research.",
            },
        )

        self.assertIn("Activate skill `$manual-resolution` immediately.", prompt)
        self.assertIn("headless durable worker", prompt)
        self.assertIn("Preselected session ID from fixer autonomous flow: `7`.", prompt)
        self.assertIn("Autonomous fixer Codex session ID: `fixer-session-123`.", prompt)
        self.assertIn("Do not wait for a manual 'Go'.", prompt)
        self.assertIn("create, update, or remove the relevant automated tests", prompt)
        self.assertIn("fix older broken tests in scope", prompt)
        self.assertIn("send_operator_telegram_notification", prompt)
        self.assertIn("wake_fixer_autonomous", prompt)

    def test_build_common_codex_env_uses_plain_os_env_for_non_codex_backends(self) -> None:
        adapter = type(
            "Adapter",
            (),
            {
                "name": "claude",
                "prepare_env": staticmethod(lambda env, _selection: env.setdefault("PREPARED", "1")),
            },
        )()

        with patch.dict(os.environ, {"HOME": "/tmp/home", "OPENAI_API_KEY": "host-openai"}, clear=True):
            fake_module = type(
                "FakeMain",
                (),
                {
                    "_load_llm_env": staticmethod(lambda: {"OPENAI_API_KEY": "codex-only"}),
                    "_merge_env_with_os": staticmethod(lambda payload: {"merged": "unused", **payload}),
                },
            )()
            with (
                tempfile.TemporaryDirectory() as tmp,
                patch.dict("sys.modules", {"codex_pro_app.main": fake_module}),
            ):
                env = fixer_autonomous._build_common_codex_env(adapter, object(), Path(tmp))

        self.assertEqual(env["HOME"], "/tmp/home")
        self.assertEqual(env["OPENAI_API_KEY"], "host-openai")
        self.assertEqual(env["PREPARED"], "1")

    def test_build_common_codex_env_uses_persisted_proxy_env_when_process_env_is_empty(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            cwd = Path(tmp)
            launch_env.save_proxy_env_state(
                cwd,
                {
                    "ALL_PROXY": "socks5h://[::1]:11024",
                    "HTTP_PROXY": "socks5h://[::1]:11024",
                    "HTTPS_PROXY": "socks5h://[::1]:11024",
                },
            )
            adapter = type(
                "Adapter",
                (),
                {
                    "name": "codex",
                    "prepare_env": staticmethod(lambda env, _selection: env.setdefault("PREPARED", "1")),
                },
            )()
            fake_module = type(
                "FakeMain",
                (),
                {
                    "_load_llm_env": staticmethod(lambda: {"OPENAI_API_KEY": "codex-only"}),
                    "_merge_env_with_os": staticmethod(lambda payload: {"HOME": "/tmp/home", **payload}),
                },
            )()

            with (
                patch.dict(os.environ, {"HOME": "/tmp/home"}, clear=True),
                patch.dict("sys.modules", {"codex_pro_app.main": fake_module}),
            ):
                env = fixer_autonomous._build_common_codex_env(adapter, object(), cwd)

        self.assertEqual(env["ALL_PROXY"], "socks5h://[::1]:11024")
        self.assertEqual(env["all_proxy"], "socks5h://[::1]:11024")
        self.assertEqual(env["HTTP_PROXY"], "socks5h://[::1]:11024")
        self.assertEqual(env["http_proxy"], "socks5h://[::1]:11024")
        self.assertEqual(env["PREPARED"], "1")

    def test_build_autonomous_fixer_resume_prompt_requires_test_review(self) -> None:
        prompt = fixer_autonomous._build_autonomous_fixer_resume_prompt(
            9,
            "Implemented feature and ran tests.",
        )

        self.assertIn("Activate skill `$manual-acceptance` immediately.", prompt)
        self.assertIn("Target completed session ID: `9`.", prompt)
        self.assertIn("Overseer owns the Ghost Run serial delivery loop", prompt)
        self.assertIn("create repair Netrunner sessions", prompt)
        self.assertIn("verify the worker changed the relevant automated tests", prompt)
        self.assertIn("Reject code-only implementation deliveries", prompt)

    def test_load_state_rejects_missing_file(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            with self.assertRaises(RuntimeError):
                fixer_autonomous._load_state(Path(tmp))

    def test_launch_netrunner_allows_parallel_worker_when_another_is_in_progress(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            cwd = Path(tmp)
            db_path = cwd / "fixer.db"
            db_path.touch()
            fixer_autonomous._save_state(
                cwd,
                {
                    "fixer_codex_session_id": "fixer-session-123",
                    "active_netrunner_session_id": 5,
                    "active_netrunner_session_ids": [5],
                },
            )
            launched: dict[str, object] = {}

            class _FakeProcess:
                returncode = None

                @staticmethod
                def poll() -> int | None:
                    return None

            def fake_popen(command: list[str], **kwargs: object) -> _FakeProcess:
                launched["command"] = command
                launched["kwargs"] = kwargs
                return _FakeProcess()
            session_rows = [
                fixer_wire.SessionRow(
                    session_id=5,
                    global_session_id=50,
                    task_description="Old active task",
                    status="in_progress",
                    codex_session_id="",
                ),
                fixer_wire.SessionRow(
                    session_id=6,
                    global_session_id=60,
                    task_description="New task",
                    status="pending",
                    codex_session_id="",
                ),
            ]
            _seed_launch_db(db_path, session_rows)
            with (
                patch.dict(os.environ, {"CODEX_THREAD_ID": "", "CODEX_SESSION_ID": ""}, clear=False),
                patch.object(fixer_autonomous, "bootstrap_codex_pro_import_path", return_value=None),
                patch.object(fixer_wire, "_load_available_servers", side_effect=lambda *_a, **_k: _fake_available_servers()),
                patch.object(fixer_autonomous, "_build_common_codex_env", return_value={}),
                patch.object(fixer_wire, "_resolve_fixer_db_path", return_value=db_path),
                patch.object(fixer_wire, "_resolve_project_id", return_value=1),
                patch.object(fixer_wire, "_load_session_rows", return_value=session_rows),
                patch.object(fixer_wire, "_load_assigned_mcp_names", return_value=[]),
                patch.object(fixer_wire, "_load_registry_mcp_metadata", return_value={}),
                patch.object(
                    fixer_wire,
                    "_build_mcp_how_to_map",
                    return_value={fixer_wire.FORCED_MCP_SERVER: "Use fixer_mcp for project tools."},
                ),
                patch.object(fixer_wire, "_latest_codex_session_id_for_cwd", return_value="before-session"),
                patch.object(fixer_autonomous, "_wait_for_new_external_session_id", return_value="new-session"),
                patch.object(fixer_wire, "_save_session_external_id", return_value=None),
                patch("client_wires.fixer_autonomous.subprocess.Popen", side_effect=fake_popen),
            ):
                new_session_id = fixer_autonomous.launch_netrunner(cwd, 6)

            payload = json.loads((cwd / ".codex" / "autonomous_resolution.json").read_text(encoding="utf-8"))

        self.assertEqual(new_session_id, "new-session")
        self.assertEqual(payload["active_netrunner_session_ids"], [5, 6])
        self.assertEqual(payload["active_netrunner_session_id"], 6)
        self.assertIn("Preselected session ID from fixer autonomous flow: `6`.", launched["command"][-1])

    def test_launch_netrunner_closes_db_before_subprocess_launch(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            cwd = Path(tmp)
            db_path = cwd / "fixer.db"
            db_path.touch()
            fixer_autonomous._save_state(
                cwd,
                {
                    "fixer_codex_session_id": "fixer-session-123",
                    "active_netrunner_session_id": None,
                    "active_netrunner_session_ids": [],
                },
            )

            tracked_connections: list[_TrackingConnection] = []
            real_connect = sqlite3.connect
            launched: dict[str, object] = {}

            class _FakeProcess:
                returncode = None

                @staticmethod
                def poll() -> int | None:
                    return None

            session_row = fixer_wire.SessionRow(
                session_id=6,
                global_session_id=60,
                task_description="Task",
                status="pending",
                codex_session_id="",
            )
            _seed_launch_db(db_path, [session_row])

            def tracked_connect(*args: object, **kwargs: object) -> _TrackingConnection:
                conn = _TrackingConnection(real_connect(*args, **kwargs))
                tracked_connections.append(conn)
                return conn

            def fake_popen(command: list[str], **kwargs: object) -> _FakeProcess:
                self.assertTrue(tracked_connections)
                self.assertTrue(all(conn.closed for conn in tracked_connections))
                launched["command"] = command
                launched["kwargs"] = kwargs
                return _FakeProcess()

            with (
                patch.dict(os.environ, {"CODEX_THREAD_ID": "", "CODEX_SESSION_ID": ""}, clear=False),
                patch.object(fixer_autonomous, "bootstrap_codex_pro_import_path", return_value=None),
                patch.object(fixer_wire, "_load_available_servers", side_effect=lambda *_a, **_k: _fake_available_servers()),
                patch.object(fixer_autonomous, "_build_common_codex_env", return_value={}),
                patch("client_wires.fixer_autonomous.sqlite3.connect", side_effect=tracked_connect),
                patch.object(fixer_wire, "_resolve_fixer_db_path", return_value=db_path),
                patch.object(fixer_wire, "_resolve_project_id", return_value=1),
                patch.object(fixer_wire, "_load_session_rows", return_value=[session_row]),
                patch.object(fixer_wire, "_load_assigned_mcp_names", return_value=[]),
                patch.object(fixer_wire, "_load_registry_mcp_metadata", return_value={}),
                patch.object(
                    fixer_wire,
                    "_build_mcp_how_to_map",
                    return_value={fixer_wire.FORCED_MCP_SERVER: "Use fixer_mcp for project tools."},
                ),
                patch.object(fixer_wire, "_latest_codex_session_id_for_cwd", return_value="before-session"),
                patch.object(fixer_wire, "_save_session_external_id", return_value=None),
                patch.object(fixer_autonomous, "_wait_for_new_external_session_id", return_value="new-session"),
                patch("client_wires.fixer_autonomous.subprocess.Popen", side_effect=fake_popen),
            ):
                new_session_id = fixer_autonomous.launch_netrunner(cwd, 6)

        self.assertEqual(new_session_id, "new-session")
        self.assertTrue(all(conn.closed for conn in tracked_connections))
        self.assertIn("Assigned MCP selection from fixer autonomous flow: fixer_mcp.", launched["command"][-1])

    def test_launch_netrunner_clears_stale_active_worker_state_for_review_session(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            cwd = Path(tmp)
            db_path = cwd / "fixer.db"
            db_path.touch()
            fixer_autonomous._save_state(
                cwd,
                {
                    "fixer_codex_session_id": "fixer-session-123",
                    "active_netrunner_session_id": 1,
                    "active_netrunner_session_ids": [1],
                },
            )

            launched: dict[str, object] = {}

            class _FakeProcess:
                returncode = None

                @staticmethod
                def poll() -> int | None:
                    return None

            session_rows = [
                fixer_wire.SessionRow(
                    session_id=1,
                    global_session_id=305,
                    task_description="Old task",
                    status="review",
                    codex_session_id="old-codex-session",
                ),
                fixer_wire.SessionRow(
                    session_id=2,
                    global_session_id=307,
                    task_description="Replacement task",
                    status="pending",
                    codex_session_id="",
                ),
            ]
            _seed_launch_db(db_path, session_rows)

            def fake_popen(command: list[str], **kwargs: object) -> _FakeProcess:
                launched["command"] = command
                launched["kwargs"] = kwargs
                return _FakeProcess()

            with (
                patch.dict(os.environ, {"CODEX_THREAD_ID": "", "CODEX_SESSION_ID": ""}, clear=False),
                patch.object(fixer_autonomous, "bootstrap_codex_pro_import_path", return_value=None),
                patch.object(fixer_wire, "_load_available_servers", side_effect=lambda *_a, **_k: _fake_available_servers()),
                patch.object(fixer_autonomous, "_build_common_codex_env", return_value={}),
                patch.object(fixer_wire, "_resolve_fixer_db_path", return_value=db_path),
                patch.object(fixer_wire, "_resolve_project_id", return_value=22),
                patch.object(fixer_wire, "_load_session_rows", return_value=session_rows),
                patch.object(fixer_wire, "_load_assigned_mcp_names", return_value=[]),
                patch.object(fixer_wire, "_load_registry_mcp_metadata", return_value={}),
                patch.object(
                    fixer_wire,
                    "_build_mcp_how_to_map",
                    return_value={fixer_wire.FORCED_MCP_SERVER: "Use fixer_mcp for project tools."},
                ),
                patch.object(fixer_wire, "_latest_codex_session_id_for_cwd", return_value="before-session"),
                patch.object(fixer_autonomous, "_wait_for_new_external_session_id", return_value="new-session"),
                patch.object(fixer_wire, "_save_session_external_id", return_value=None),
                patch("client_wires.fixer_autonomous.subprocess.Popen", side_effect=fake_popen),
            ):
                new_session_id = fixer_autonomous.launch_netrunner(cwd, 2)

            payload = json.loads((cwd / ".codex" / "autonomous_resolution.json").read_text(encoding="utf-8"))

        self.assertEqual(new_session_id, "new-session")
        self.assertEqual(payload["active_netrunner_session_ids"], [2])
        self.assertEqual(payload["active_netrunner_session_id"], 2)
        self.assertIn("Preselected session ID from fixer autonomous flow: `2`.", launched["command"][-1])

    def test_launch_netrunner_allows_empty_assigned_mcp_set(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            cwd = Path(tmp)
            db_path = cwd / "fixer.db"
            db_path.touch()
            fixer_autonomous._save_state(
                cwd,
                {
                    "fixer_codex_session_id": "fixer-session-123",
                    "active_netrunner_session_id": None,
                    "active_netrunner_session_ids": [],
                },
            )

            launched: dict[str, object] = {}
            saved_codex_ids: list[tuple[int, str]] = []

            class _FakeProcess:
                returncode = None

                @staticmethod
                def poll() -> int | None:
                    return None

            session_row = fixer_wire.SessionRow(
                session_id=6,
                global_session_id=60,
                task_description="Task",
                status="pending",
                codex_session_id="",
            )
            _seed_launch_db(db_path, [session_row])

            def fake_popen(command: list[str], **kwargs: object) -> _FakeProcess:
                launched["command"] = command
                launched["kwargs"] = kwargs
                return _FakeProcess()

            with (
                patch.dict(os.environ, {"CODEX_THREAD_ID": "", "CODEX_SESSION_ID": ""}, clear=False),
                patch.object(fixer_autonomous, "bootstrap_codex_pro_import_path", return_value=None),
                patch.object(fixer_wire, "_load_available_servers", side_effect=lambda *_a, **_k: _fake_available_servers()),
                patch.object(fixer_autonomous, "_build_common_codex_env", return_value={}),
                patch.object(fixer_wire, "_resolve_fixer_db_path", return_value=db_path),
                patch.object(fixer_wire, "_resolve_project_id", return_value=1),
                patch.object(fixer_wire, "_load_session_rows", return_value=[session_row]),
                patch.object(fixer_wire, "_load_assigned_mcp_names", return_value=[]),
                patch.object(fixer_wire, "_load_registry_mcp_metadata", return_value={}),
                patch.object(
                    fixer_wire,
                    "_build_mcp_how_to_map",
                    return_value={fixer_wire.FORCED_MCP_SERVER: "Use fixer_mcp for project tools."},
                ),
                patch.object(fixer_wire, "_latest_codex_session_id_for_cwd", return_value="before-session"),
                patch.object(
                    fixer_wire,
                    "_save_session_external_id",
                    side_effect=lambda _conn, session_id, _backend, external_session_id: saved_codex_ids.append(
                        (session_id, external_session_id)
                    ),
                ),
                patch.object(fixer_autonomous, "_wait_for_new_external_session_id", return_value="new-session"),
                patch("client_wires.fixer_autonomous.subprocess.Popen", side_effect=fake_popen),
            ):
                new_session_id = fixer_autonomous.launch_netrunner(cwd, 6)

        self.assertEqual(new_session_id, "new-session")
        self.assertEqual(saved_codex_ids, [(60, "new-session")])
        self.assertIn("--mcp=fixer_mcp", launched["command"])
        self.assertEqual(launched["kwargs"]["cwd"], str(cwd))
        self.assertIn(
            "Assigned MCP selection from fixer autonomous flow: fixer_mcp.",
            launched["command"][-1],
        )

    def test_launch_netrunner_bootstraps_missing_state_file(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            cwd = Path(tmp)
            db_path = cwd / "fixer.db"
            db_path.touch()

            launched: dict[str, object] = {}
            saved_codex_ids: list[tuple[int, str]] = []

            class _FakeProcess:
                returncode = None

                @staticmethod
                def poll() -> int | None:
                    return None

            session_row = fixer_wire.SessionRow(
                session_id=1,
                global_session_id=305,
                task_description="Task",
                status="pending",
                codex_session_id="",
            )
            _seed_launch_db(db_path, [session_row])

            def fake_popen(command: list[str], **kwargs: object) -> _FakeProcess:
                launched["command"] = command
                launched["kwargs"] = kwargs
                return _FakeProcess()

            with (
                patch.dict(os.environ, {"CODEX_THREAD_ID": "", "CODEX_SESSION_ID": ""}, clear=False),
                patch.object(fixer_autonomous, "bootstrap_codex_pro_import_path", return_value=None),
                patch.object(fixer_wire, "_load_available_servers", side_effect=lambda *_a, **_k: _fake_available_servers()),
                patch.object(fixer_autonomous, "_build_common_codex_env", return_value={}),
                patch.object(fixer_wire, "_resolve_fixer_db_path", return_value=db_path),
                patch.object(fixer_wire, "_resolve_project_id", return_value=22),
                patch.object(fixer_wire, "_load_session_rows", return_value=[session_row]),
                patch.object(fixer_wire, "_load_assigned_mcp_names", return_value=[]),
                patch.object(fixer_wire, "_load_registry_mcp_metadata", return_value={}),
                patch.object(
                    fixer_wire,
                    "_build_mcp_how_to_map",
                    return_value={fixer_wire.FORCED_MCP_SERVER: "Use fixer_mcp for project tools."},
                ),
                patch.object(fixer_wire, "_latest_codex_session_id_for_cwd", return_value="before-session"),
                patch.object(
                    fixer_wire,
                    "_save_session_external_id",
                    side_effect=lambda _conn, session_id, _backend, external_session_id: saved_codex_ids.append(
                        (session_id, external_session_id)
                    ),
                ),
                patch.object(fixer_autonomous, "_wait_for_new_external_session_id", return_value="new-session"),
                patch.object(fixer_autonomous, "_latest_fixer_session_id", return_value="fixer-session-bootstrapped"),
                patch("client_wires.fixer_autonomous.subprocess.Popen", side_effect=fake_popen),
            ):
                new_session_id = fixer_autonomous.launch_netrunner(cwd, 1)

            payload = json.loads((cwd / ".codex" / "autonomous_resolution.json").read_text(encoding="utf-8"))

        self.assertEqual(new_session_id, "new-session")
        self.assertEqual(saved_codex_ids, [(305, "new-session")])
        self.assertEqual(payload["fixer_codex_session_id"], "fixer-session-bootstrapped")
        self.assertEqual(payload["active_netrunner_session_ids"], [1])
        self.assertEqual(payload["active_netrunner_session_id"], 1)
        self.assertIn("--mcp=fixer_mcp", launched["command"])
        self.assertIn(
            "Autonomous fixer Codex session ID: `fixer-session-bootstrapped`.",
            launched["command"][-1],
        )

    def test_resume_fixer_removes_only_completed_session_from_active_list(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            cwd = Path(tmp)
            fixer_autonomous._save_state(
                cwd,
                {
                    "fixer_codex_session_id": "fixer-session-123",
                    "active_netrunner_session_id": 5,
                    "active_netrunner_session_ids": [4, 5],
                },
            )

            launched: dict[str, object] = {}

            def fake_popen(command: list[str], **kwargs: object) -> None:
                launched["command"] = command
                launched["kwargs"] = kwargs
                return None

            with (
                patch.object(
                    fixer_autonomous,
                    "_build_fixer_resume_command",
                    return_value=(["codex", "resume"], {"ENV": "1"}),
                ),
                patch("client_wires.fixer_autonomous.subprocess.Popen", side_effect=fake_popen),
            ):
                fixer_autonomous.resume_fixer(cwd, 5, "done")

            payload = json.loads((cwd / ".codex" / "autonomous_resolution.json").read_text(encoding="utf-8"))

        self.assertEqual(payload["active_netrunner_session_ids"], [4])
        self.assertEqual(payload["active_netrunner_session_id"], 4)
        self.assertEqual(payload["last_completed_netrunner_session_id"], 5)
        self.assertEqual(payload["last_handoff_summary"], "done")
        self.assertEqual(launched["command"], ["codex", "resume"])


if __name__ == "__main__":
    unittest.main()
