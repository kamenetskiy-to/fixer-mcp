from __future__ import annotations

import contextlib
import io
import json
import os
import sqlite3
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
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
    def test_find_new_codex_session_id_from_transcript_store_matches_session_meta_cwd(self) -> None:
        with tempfile.TemporaryDirectory() as tmp, tempfile.TemporaryDirectory() as codex_tmp:
            cwd = Path(tmp)
            sessions_root = Path(codex_tmp)
            transcript = (
                sessions_root
                / "2026"
                / "05"
                / "23"
                / "rollout-2026-05-23T12-00-00-codex-session-123.jsonl"
            )
            transcript.parent.mkdir(parents=True, exist_ok=True)
            transcript.write_text(
                json.dumps(
                    {
                        "type": "session_meta",
                        "payload": {
                            "id": "codex-session-123",
                            "cwd": str(cwd),
                        },
                    }
                )
                + "\n",
                encoding="utf-8",
            )

            session_id = fixer_autonomous._find_new_codex_session_id_from_transcript_store(
                cwd,
                launch_started_at=0,
                sessions_root=sessions_root,
            )

        self.assertEqual(session_id, "codex-session-123")

    def test_find_new_droid_session_id_from_factory_store_matches_session_start_cwd(self) -> None:
        with tempfile.TemporaryDirectory() as tmp, tempfile.TemporaryDirectory() as factory_tmp:
            cwd = Path(tmp)
            sessions_root = Path(factory_tmp)
            transcript = sessions_root / "-tmp-project" / "droid-session-123.jsonl"
            transcript.parent.mkdir(parents=True, exist_ok=True)
            transcript.write_text(
                json.dumps(
                    {
                        "type": "session_start",
                        "cwd": str(cwd),
                    }
                )
                + "\n",
                encoding="utf-8",
            )

            session_id = fixer_autonomous._find_new_droid_session_id_from_factory_store(
                cwd,
                launch_started_at=0,
                sessions_root=sessions_root,
            )

        self.assertEqual(session_id, "droid-session-123")

    def test_find_new_droid_session_id_ignores_old_or_wrong_cwd_transcripts(self) -> None:
        with tempfile.TemporaryDirectory() as tmp, tempfile.TemporaryDirectory() as factory_tmp:
            cwd = Path(tmp)
            sessions_root = Path(factory_tmp)
            wrong = sessions_root / "wrong" / "wrong-session.jsonl"
            old = sessions_root / "old" / "old-session.jsonl"
            wrong.parent.mkdir(parents=True, exist_ok=True)
            old.parent.mkdir(parents=True, exist_ok=True)
            wrong.write_text(
                json.dumps({"type": "session_start", "cwd": "/tmp/other-project"}) + "\n",
                encoding="utf-8",
            )
            old.write_text(
                json.dumps({"type": "session_start", "cwd": str(cwd)}) + "\n",
                encoding="utf-8",
            )
            os.utime(old, (10, 10))

            session_id = fixer_autonomous._find_new_droid_session_id_from_factory_store(
                cwd,
                launch_started_at=100,
                sessions_root=sessions_root,
            )

        self.assertIsNone(session_id)

    def test_find_new_droid_session_id_prefers_payload_session_id(self) -> None:
        with tempfile.TemporaryDirectory() as tmp, tempfile.TemporaryDirectory() as factory_tmp:
            cwd = Path(tmp)
            sessions_root = Path(factory_tmp)
            transcript = sessions_root / "project" / "filename-session.jsonl"
            transcript.parent.mkdir(parents=True, exist_ok=True)
            transcript.write_text(
                json.dumps(
                    {
                        "type": "session_start",
                        "cwd": str(cwd),
                        "session_id": "payload-session-456",
                    }
                )
                + "\n",
                encoding="utf-8",
            )

            session_id = fixer_autonomous._find_new_droid_session_id_from_factory_store(
                cwd,
                launch_started_at=0,
                sessions_root=sessions_root,
            )

        self.assertEqual(session_id, "payload-session-456")

    def test_wait_for_new_droid_session_id_uses_factory_transcript_when_headless_log_empty(self) -> None:
        with tempfile.TemporaryDirectory() as tmp, tempfile.TemporaryDirectory() as factory_tmp:
            cwd = Path(tmp)
            log_path = cwd / "headless.log"
            log_path.write_text("", encoding="utf-8")
            sessions_root = Path(factory_tmp)
            transcript = sessions_root / "project" / "droid-before-checkout.jsonl"
            transcript.parent.mkdir(parents=True, exist_ok=True)
            transcript.write_text(
                json.dumps({"type": "session_start", "cwd": str(cwd)}) + "\n",
                encoding="utf-8",
            )

            with patch.object(fixer_autonomous, "_droid_factory_sessions_root", return_value=sessions_root):
                session_id = fixer_autonomous._wait_for_new_droid_session_id(
                    log_path,
                    cwd,
                    launch_started_at=0,
                    timeout_sec=0.1,
                )

        self.assertEqual(session_id, "droid-before-checkout")

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

    def test_latest_fixer_session_id_bootstraps_before_loading_resume_summaries(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            cwd = Path(tmp)
            calls: list[str] = []

            def fake_load_summaries(_cwd: Path, *, limit: int) -> list[SimpleNamespace]:
                calls.append("load")
                self.assertEqual(calls, ["bootstrap", "load"])
                self.assertEqual(limit, 1)
                return [SimpleNamespace(session_id="latest-fixer-session")]

            with (
                patch.object(
                    fixer_autonomous,
                    "bootstrap_codex_pro_import_path",
                    side_effect=lambda: calls.append("bootstrap"),
                ),
                patch.object(fixer_wire, "_load_fixer_resume_summaries", side_effect=fake_load_summaries),
            ):
                session_id = fixer_autonomous._latest_fixer_session_id(cwd)

        self.assertEqual(session_id, "latest-fixer-session")
        self.assertEqual(calls, ["bootstrap", "load"])

    def test_main_runtime_error_stderr_includes_underlying_cause(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            stderr = io.StringIO()

            def fail_register(_cwd: Path, _session_id: str | None) -> None:
                try:
                    raise ModuleNotFoundError("No module named 'client_wires.codex_compat'")
                except ModuleNotFoundError as err:
                    raise RuntimeError("Unable to load Codex history helpers for Fixer resume flow.") from err

            with (
                patch.object(fixer_autonomous, "register_fixer_session", side_effect=fail_register),
                contextlib.redirect_stderr(stderr),
            ):
                code = fixer_autonomous.main(["register-fixer", "--cwd", tmp])

        self.assertEqual(code, 2)
        self.assertIn("Unable to load Codex history helpers for Fixer resume flow.", stderr.getvalue())
        self.assertIn("Caused by: ModuleNotFoundError: No module named 'client_wires.codex_compat'", stderr.getvalue())

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

        self.assertIn("Activate skill `$run-manual-netrunner` immediately.", prompt)
        self.assertIn("headless durable worker", prompt)
        self.assertIn("Preselected session ID from fixer autonomous flow: `7`.", prompt)
        self.assertIn("Autonomous fixer Codex session ID: `fixer-session-123`.", prompt)
        self.assertIn("Do not wait for a manual 'Go'.", prompt)
        self.assertIn("create, update, or remove the relevant automated tests", prompt)
        self.assertIn("fix older broken tests in scope", prompt)
        self.assertIn("send_operator_telegram_notification", prompt)
        self.assertIn("wake_fixer_autonomous", prompt)

    def test_build_autonomous_netrunner_prompt_suppresses_wake_for_explicit_wait(self) -> None:
        prompt = fixer_autonomous._build_autonomous_netrunner_prompt(
            7,
            ["fixer_mcp"],
            "fixer-session-123",
            {"fixer_mcp": "Use for project tools."},
            suppress_autonomous_wake=True,
        )

        self.assertIn("Do not call fixer_mcp.wake_fixer_autonomous", prompt)
        self.assertIn("waiting Fixer's `wait_for_netrunner_session` polling is the completion signal", prompt)
        self.assertNotIn("call the fixer_mcp tool `wake_fixer_autonomous`", prompt)

    def test_build_autonomous_netrunner_prompt_omits_empty_fixer_session_id(self) -> None:
        prompt = fixer_autonomous._build_autonomous_netrunner_prompt(
            7,
            ["fixer_mcp"],
            "",
            {"fixer_mcp": "Use for project tools."},
            suppress_autonomous_wake=True,
        )

        self.assertNotIn("Autonomous fixer Codex session ID", prompt)
        self.assertIn("Preselected session ID from fixer autonomous flow: `7`.", prompt)
        self.assertIn("Do not call fixer_mcp.wake_fixer_autonomous", prompt)

    def test_wave_naming_helpers_use_deterministic_branch_and_worktree_paths(self) -> None:
        project_cwd = Path("/tmp/project")

        branch_name = fixer_autonomous._wave_branch_name(12, 34)
        worktree_path = fixer_autonomous._wave_worktree_path(
            project_cwd,
            Path(".codex/netrunner_worktrees"),
            12,
            34,
        )
        metadata_path = fixer_autonomous._wave_worker_metadata_path(project_cwd, 12, 34)

        self.assertEqual(branch_name, "fixer/wave-12/session-34")
        self.assertEqual(
            worktree_path,
            project_cwd.resolve() / ".codex" / "netrunner_worktrees" / "wave-12" / "session-34",
        )
        self.assertEqual(
            metadata_path,
            project_cwd.resolve()
            / ".codex"
            / "netrunner_wave_artifacts"
            / "wave-12"
            / "session-34"
            / "worker_metadata.json",
        )
        self.assertEqual(
            fixer_autonomous._validate_wave_branch_name(branch_name),
            branch_name,
        )
        with self.assertRaises(RuntimeError):
            fixer_autonomous._wave_branch_name(0, 34)
        with self.assertRaises(RuntimeError):
            fixer_autonomous._validate_wave_branch_name("main")

    def test_wave_git_command_builders_are_explicit_and_safe_by_default(self) -> None:
        project_cwd = Path("/tmp/project")
        branch_name = "fixer/wave-3/session-9"
        worktree_path = project_cwd / ".codex" / "netrunner_worktrees" / "wave-3" / "session-9"

        add_command = fixer_autonomous._build_git_worktree_add_command(
            project_cwd,
            worktree_path=worktree_path,
            branch_name=branch_name,
            base_sha="abc123",
        )
        remove_command = fixer_autonomous._build_git_worktree_remove_command(project_cwd, worktree_path)
        forced_remove_command = fixer_autonomous._build_git_worktree_remove_command(
            project_cwd,
            worktree_path,
            force=True,
        )

        self.assertEqual(
            add_command,
            [
                "git",
                "-C",
                str(project_cwd),
                "worktree",
                "add",
                "-b",
                branch_name,
                str(worktree_path),
                "abc123",
            ],
        )
        self.assertEqual(
            fixer_autonomous._build_git_branch_exists_command(project_cwd, branch_name),
            ["git", "-C", str(project_cwd), "show-ref", "--verify", f"refs/heads/{branch_name}"],
        )
        self.assertEqual(
            fixer_autonomous._build_git_worktree_list_command(project_cwd),
            ["git", "-C", str(project_cwd), "worktree", "list", "--porcelain"],
        )
        self.assertEqual(
            remove_command,
            ["git", "-C", str(project_cwd), "worktree", "remove", str(worktree_path)],
        )
        self.assertEqual(
            forced_remove_command,
            ["git", "-C", str(project_cwd), "worktree", "remove", "--force", str(worktree_path)],
        )
        self.assertEqual(
            fixer_autonomous._build_git_worktree_prune_command(project_cwd),
            ["git", "-C", str(project_cwd), "worktree", "prune", "--dry-run"],
        )
        with self.assertRaises(RuntimeError):
            fixer_autonomous._build_git_worktree_remove_command(project_cwd, project_cwd)

    def test_build_wave_netrunner_prompt_includes_guardrails_and_omits_worker_wake(self) -> None:
        prompt = fixer_autonomous._build_wave_netrunner_prompt(
            session_id=7,
            mcp_names=["eslint", "fixer_mcp"],
            fixer_session_id="fixer-session-123",
            mcp_how_to={
                "eslint": "Use for lint loops.",
                "fixer_mcp": "Use for project tools.",
            },
            wave_id=3,
            wave_worker_id=44,
            branch_name="fixer/wave-3/session-7",
            worker_cwd=Path("/tmp/project/.codex/netrunner_worktrees/wave-3/session-7"),
            declared_write_scope=["client_wires/fixer_autonomous.py"],
        )

        self.assertIn("wave_id: `3`", prompt)
        self.assertIn("wave_worker_id: `44`", prompt)
        self.assertIn("branch_name: `fixer/wave-3/session-7`", prompt)
        self.assertIn("assigned declared_write_scope: client_wires/fixer_autonomous.py", prompt)
        self.assertIn("Operate only inside the assigned declared_write_scope.", prompt)
        self.assertIn("Do not merge, rebase, remove worktrees, or alter wave state.", prompt)
        self.assertIn("Report changed files in the completion report.", prompt)
        self.assertIn("Do not call fixer_mcp.wake_fixer_autonomous", prompt)
        self.assertIn("submit the mandatory doc proposal and completion report", prompt)

    def test_build_wave_netrunner_launch_plan_uses_worker_cwd_and_project_auth(self) -> None:
        with tempfile.TemporaryDirectory() as project_tmp, tempfile.TemporaryDirectory() as worker_tmp:
            project_cwd = Path(project_tmp)
            worker_cwd = Path(worker_tmp)
            db_path = project_cwd / "fixer.db"
            db_path.touch()
            ensure_calls: list[Path] = []

            def fake_ensure_runtime_files(
                cwd: Path,
                _selection: object,
                _selected: dict[str, object],
                _available: dict[str, object],
            ) -> None:
                ensure_calls.append(cwd)

            available_servers = {
                "eslint": {"command": "eslint-mcp"},
                fixer_wire.FORCED_MCP_SERVER: {"command": "fixer_mcp"},
            }
            adapter = _FakeBackendAdapter()
            launch_selection = fixer_wire.SessionLaunchSelection(
                backend="codex",
                model="gpt-5.5",
                reasoning="xhigh",
            )

            with (
                patch.object(_FakeBackendAdapter, "ensure_runtime_files", side_effect=fake_ensure_runtime_files),
                patch.object(
                    fixer_autonomous,
                    "_build_common_codex_env",
                    return_value={"BASE_ENV": "1", fixer_wire.FIXER_DB_PATH_ENV: "wrong-db"},
                ),
            ):
                plan = fixer_autonomous._build_wave_netrunner_launch_plan(
                    project_cwd=project_cwd,
                    worker_cwd=worker_cwd,
                    local_session_id=7,
                    wave_id=3,
                    wave_worker_id=44,
                    declared_write_scope=["client_wires/fixer_autonomous.py"],
                    fixer_session_id="fixer-session-123",
                    assigned_mcp_names=["eslint"],
                    mcp_how_to={
                        "eslint": "Use for lint loops.",
                        fixer_wire.FORCED_MCP_SERVER: "Use for project tools.",
                    },
                    launch_selection=launch_selection,
                    available_servers=available_servers,
                    config_env_vars={},
                    adapter=adapter,
                    ensure_sqlite_scaffold=lambda _cwd: None,
                    db_path=db_path,
                )

        self.assertEqual(plan.popen_cwd, worker_cwd.resolve())
        self.assertEqual(ensure_calls, [worker_cwd.resolve()])
        self.assertEqual(plan.env["BASE_ENV"], "1")
        self.assertEqual(plan.env[fixer_wire.FIXER_DB_PATH_ENV], str(db_path.resolve()))
        self.assertEqual(plan.command[0], "codex")
        self.assertIn("--mcp=eslint,fixer_mcp", plan.command)
        self.assertEqual(plan.command[-1], plan.prompt)
        self.assertIn("Do not call fixer_mcp.wake_fixer_autonomous", plan.prompt)
        self.assertEqual(plan.metadata["wave_id"], 3)
        self.assertEqual(plan.metadata["wave_worker_id"], 44)
        self.assertEqual(plan.metadata["branch_name"], "fixer/wave-3/session-7")
        server_env = plan.selected_servers[fixer_wire.FORCED_MCP_SERVER]["env"]
        self.assertEqual(server_env[fixer_wire.FIXER_DB_PATH_ENV], str(db_path.resolve()))
        self.assertEqual(server_env[fixer_wire.FIXER_MCP_DEFAULT_CWD_ENV], str(project_cwd.resolve()))
        self.assertEqual(server_env[fixer_wire.FIXER_MCP_DEFAULT_ROLE_ENV], "netrunner")
        self.assertEqual(server_env[fixer_wire.FIXER_MCP_LOCKED_ROLE_ENV], "netrunner")

    def test_write_worker_metadata_can_include_wave_context(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            metadata_path = Path(tmp) / "worker_metadata.json"
            fixer_autonomous._write_worker_metadata(
                metadata_path,
                worker_pid=12345,
                headless_log_path=Path(tmp) / "worker.log",
                backend="codex",
                session_id=7,
                wave_id=3,
                wave_worker_id=44,
                project_cwd=Path("/tmp/project"),
                worker_cwd=Path("/tmp/project/.codex/netrunner_worktrees/wave-3/session-7"),
                branch_name="fixer/wave-3/session-7",
            )
            payload = json.loads(metadata_path.read_text(encoding="utf-8"))

        self.assertEqual(payload["worker_pid"], 12345)
        self.assertEqual(payload["wave_id"], 3)
        self.assertEqual(payload["wave_worker_id"], 44)
        self.assertEqual(payload["project_cwd"], "/tmp/project")
        self.assertEqual(payload["branch_name"], "fixer/wave-3/session-7")

    def test_build_overseer_directed_fixer_prompt_uses_chat_and_no_human_clarification(self) -> None:
        prompt = fixer_autonomous._build_overseer_directed_fixer_prompt()

        self.assertIn("project Overseer", prompt)
        self.assertIn("fixer_mcp.get_overseer_fixer_messages", prompt)
        self.assertIn("fixer_mcp.append_overseer_fixer_message", prompt)
        self.assertIn("fixer_mcp.set_overseer_fixer_run_state", prompt)
        self.assertIn("Do not ask the human Architect", prompt)

    @unittest.skip("public export excludes private runtime state")
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
                patch.dict("sys.modules", {"client_wires.codex_compat.llm": fake_module}),
            ):
                env = fixer_autonomous._build_common_codex_env(adapter, object(), Path(tmp))

        self.assertEqual(env["HOME"], "/tmp/home")
        self.assertEqual(env["OPENAI_API_KEY"], "host-openai")
        self.assertEqual(env["PREPARED"], "1")

    def test_build_autonomous_fixer_resume_prompt_requires_test_review(self) -> None:
        prompt = fixer_autonomous._build_autonomous_fixer_resume_prompt(
            9,
            "Implemented feature and ran tests.",
        )

        self.assertIn("Activate skill `$review-netrunner-session` immediately.", prompt)
        self.assertIn("Target completed session ID: `9`.", prompt)
        self.assertNotIn("GenUI", prompt)
        self.assertNotIn("Ghost Run", prompt)
        self.assertNotIn("launch exactly one next pending Netrunner session", prompt)
        self.assertIn("Review only the named completed session", prompt)
        self.assertIn("avoid launching unrelated pending sessions", prompt)
        self.assertIn("verify the worker changed the relevant automated tests", prompt)
        self.assertIn("Reject code-only implementation deliveries", prompt)

    def test_build_fixer_resume_command_locks_forced_fixer_mcp_role(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            cwd = Path(tmp)
            db_path = cwd / "fixer.db"
            db_path.touch()
            captured: dict[str, object] = {}

            def fake_build_mcp_flags(selected_servers: dict[str, object], _available: dict[str, object]) -> list[str]:
                captured["selected"] = selected_servers
                return ["--mcp=fixer_mcp"]

            with (
                patch.object(
                    fixer_autonomous,
                    "_build_exec_prefix",
                    return_value=(
                        ["codex"],
                        {},
                        (
                            {fixer_wire.FORCED_MCP_SERVER: {"command": "fixer_mcp"}},
                            {},
                            _FakeBackendAdapter(),
                            lambda _cwd: None,
                        ),
                    ),
                ),
                patch.object(fixer_wire, "_resolve_fixer_db_path", return_value=db_path),
                patch.object(_FakeBackendAdapter, "build_mcp_flags", side_effect=fake_build_mcp_flags),
            ):
                command, env = fixer_autonomous._build_fixer_resume_command(
                    cwd,
                    "fixer-session-123",
                    "prompt",
                )

        self.assertEqual(command[:4], ["codex", "--mcp=fixer_mcp", "exec", "resume"])
        self.assertEqual(env, {})
        selected = captured["selected"]
        server_env = selected[fixer_wire.FORCED_MCP_SERVER]["env"]
        self.assertEqual(server_env[fixer_wire.FIXER_DB_PATH_ENV], str(db_path))
        self.assertEqual(server_env[fixer_wire.FIXER_MCP_LOCKED_ROLE_ENV], "fixer")

    def test_build_fixer_resume_command_fails_loudly_when_forced_server_is_missing(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            cwd = Path(tmp)

            with patch.object(
                fixer_autonomous,
                "_build_exec_prefix",
                return_value=(["codex"], {}, ({}, {}, _FakeBackendAdapter(), lambda _cwd: None)),
            ):
                with self.assertRaisesRegex(RuntimeError, "refusing to launch without the Fixer control plane"):
                    fixer_autonomous._build_fixer_resume_command(cwd, "fixer-session-123", "prompt")

    def test_build_fixer_resume_command_fails_loudly_when_forced_command_path_is_missing(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            cwd = Path(tmp)
            missing_binary = cwd / "missing" / "fixer_mcp"

            with patch.object(
                fixer_autonomous,
                "_build_exec_prefix",
                return_value=(
                    ["codex"],
                    {},
                    (
                        {fixer_wire.FORCED_MCP_SERVER: {"command": str(missing_binary)}},
                        {},
                        _FakeBackendAdapter(),
                        lambda _cwd: None,
                    ),
                ),
            ):
                with self.assertRaisesRegex(RuntimeError, fixer_wire.FIXER_MCP_BINARY_ENV) as raised:
                    fixer_autonomous._build_fixer_resume_command(cwd, "fixer-session-123", "prompt")

        self.assertIn(str(missing_binary), str(raised.exception))

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

    def test_launch_netrunner_persists_early_droid_external_session_id(self) -> None:
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
            saved_external_ids: list[tuple[int, str, str]] = []

            class _FakeProcess:
                returncode = None
                pid = 12345

                @staticmethod
                def poll() -> int | None:
                    return None

            session_row = fixer_wire.SessionRow(
                session_id=6,
                global_session_id=60,
                task_description="Droid task",
                status="pending",
                cli_backend="droid",
                cli_model="kimi-k2.6",
                cli_reasoning="high",
            )
            _seed_launch_db(db_path, [session_row])

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
                patch.object(
                    fixer_autonomous,
                    "_wait_for_new_external_session_id",
                    return_value="droid-session-early",
                ) as wait_mock,
                patch.object(
                    fixer_wire,
                    "_save_session_external_id",
                    side_effect=lambda _conn, session_id, backend, external_session_id: saved_external_ids.append(
                        (session_id, backend, external_session_id)
                    ),
                ),
                patch("client_wires.fixer_autonomous.subprocess.Popen", return_value=_FakeProcess()),
            ):
                new_session_id = fixer_autonomous.launch_netrunner(cwd, 6)

            payload = json.loads((cwd / ".codex" / "autonomous_resolution.json").read_text(encoding="utf-8"))

        self.assertEqual(new_session_id, "droid-session-early")
        self.assertEqual(saved_external_ids, [(60, "droid", "droid-session-early")])
        self.assertEqual(payload["active_netrunner_session_ids"], [6])
        self.assertEqual(payload["last_launched_netrunner_session_id"], "droid-session-early")
        self.assertEqual(wait_mock.call_args.kwargs["timeout_sec"], fixer_autonomous.EXTERNAL_SESSION_ID_DETECTOR_TIMEOUT_SEC)

    def test_launch_netrunner_locks_forced_fixer_mcp_role(self) -> None:
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
            captured: dict[str, object] = {}

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

            def fake_build_headless_command(
                *,
                model: str,
                reasoning: str,
                selected: dict[str, object],
                available: dict[str, object],
                prompt: str,
            ) -> list[str]:
                del model, reasoning, available
                captured["selected"] = selected
                return ["codex", prompt]

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
                patch.object(fixer_wire, "_save_session_external_id", return_value=None),
                patch.object(fixer_autonomous, "_wait_for_new_external_session_id", return_value="new-session"),
                patch.object(_FakeBackendAdapter, "build_headless_command", side_effect=fake_build_headless_command),
                patch("client_wires.fixer_autonomous.subprocess.Popen", return_value=_FakeProcess()),
            ):
                new_session_id = fixer_autonomous.launch_netrunner(cwd, 6)

        self.assertEqual(new_session_id, "new-session")
        selected = captured["selected"]
        server_env = selected[fixer_wire.FORCED_MCP_SERVER]["env"]
        self.assertEqual(server_env[fixer_wire.FIXER_DB_PATH_ENV], str(db_path))
        self.assertEqual(server_env[fixer_wire.FIXER_MCP_DEFAULT_CWD_ENV], str(cwd.resolve()))
        self.assertEqual(server_env[fixer_wire.FIXER_MCP_DEFAULT_ROLE_ENV], "netrunner")
        self.assertEqual(server_env[fixer_wire.FIXER_MCP_LOCKED_ROLE_ENV], "netrunner")

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

    def test_launch_netrunner_suppressed_wake_allows_missing_fixer_state(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            cwd = Path(tmp)
            db_path = cwd / "fixer.db"
            db_path.touch()

            launched: dict[str, object] = {}

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
                patch.object(fixer_wire, "_save_session_external_id", return_value=None),
                patch.object(fixer_autonomous, "_wait_for_new_external_session_id", return_value="new-session"),
                patch.object(fixer_autonomous, "_latest_fixer_session_id", side_effect=RuntimeError("missing fixer")),
                patch("client_wires.fixer_autonomous.subprocess.Popen", side_effect=fake_popen),
            ):
                new_session_id = fixer_autonomous.launch_netrunner(
                    cwd,
                    1,
                    suppress_autonomous_wake=True,
                )

            payload = json.loads((cwd / ".codex" / "autonomous_resolution.json").read_text(encoding="utf-8"))

        self.assertEqual(new_session_id, "new-session")
        self.assertEqual(payload["fixer_codex_session_id"], "")
        self.assertIn("--mcp=fixer_mcp", launched["command"])
        self.assertNotIn("Autonomous fixer Codex session ID", launched["command"][-1])
        self.assertIn("Do not call fixer_mcp.wake_fixer_autonomous", launched["command"][-1])

    def test_launch_netrunner_without_suppressed_wake_still_requires_fixer_state(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            cwd = Path(tmp)

            with (
                patch.dict(os.environ, {"CODEX_THREAD_ID": "", "CODEX_SESSION_ID": ""}, clear=False),
                patch.object(fixer_autonomous, "bootstrap_codex_pro_import_path", return_value=None),
                patch.object(fixer_autonomous, "_latest_fixer_session_id", side_effect=RuntimeError("missing fixer")),
            ):
                with self.assertRaisesRegex(RuntimeError, "missing fixer"):
                    fixer_autonomous.launch_netrunner(cwd, 1)

    def test_launch_netrunner_saves_droid_external_id_from_factory_store(self) -> None:
        with tempfile.TemporaryDirectory() as tmp, tempfile.TemporaryDirectory() as factory_tmp:
            cwd = Path(tmp)
            sessions_root = Path(factory_tmp)
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
            saved_ids: list[tuple[int, str, str]] = []

            class _FakeProcess:
                returncode = None
                pid = 12345

                @staticmethod
                def poll() -> int | None:
                    return None

            session_row = fixer_wire.SessionRow(
                session_id=6,
                global_session_id=60,
                task_description="Task",
                status="pending",
                cli_backend="droid",
                cli_model="custom:GLM-5.1-[Z.AI]-0",
                cli_reasoning="medium",
            )
            _seed_launch_db(db_path, [session_row])

            def fake_popen(command: list[str], **kwargs: object) -> _FakeProcess:
                launched["command"] = command
                launched["kwargs"] = kwargs
                transcript = sessions_root / "-tmp-project" / "droid-native-session.jsonl"
                transcript.parent.mkdir(parents=True, exist_ok=True)
                transcript.write_text(
                    json.dumps({"type": "session_start", "cwd": str(cwd)}) + "\n",
                    encoding="utf-8",
                )
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
                patch.object(fixer_autonomous, "_droid_factory_sessions_root", return_value=sessions_root),
                patch.object(
                    fixer_wire,
                    "_save_session_external_id",
                    side_effect=lambda _conn, session_id, backend, external_session_id: saved_ids.append(
                        (session_id, backend, external_session_id)
                    ),
                ),
                patch("client_wires.fixer_autonomous.subprocess.Popen", side_effect=fake_popen),
            ):
                new_session_id = fixer_autonomous.launch_netrunner(cwd, 6)

            payload = json.loads((cwd / ".codex" / "autonomous_resolution.json").read_text(encoding="utf-8"))

        self.assertEqual(new_session_id, "droid-native-session")
        self.assertEqual(saved_ids, [(60, "droid", "droid-native-session")])
        self.assertEqual(payload["last_launched_netrunner_session_id"], "droid-native-session")
        self.assertEqual(payload["last_launched_netrunner_backend"], "droid")
        self.assertIn("Preselected session ID from fixer autonomous flow: `6`.", launched["command"][-1])

    def test_launch_netrunner_allows_explicit_droid_model_override_for_pending_session(self) -> None:
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

            class _FakeProcess:
                returncode = None
                pid = 12345

                @staticmethod
                def poll() -> int | None:
                    return None

            session_row = fixer_wire.SessionRow(
                session_id=6,
                global_session_id=60,
                task_description="Task",
                status="pending",
                cli_backend="droid",
                cli_model="glm-5.1",
                cli_reasoning="none",
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
                patch.object(fixer_autonomous, "_wait_for_new_external_session_id", return_value=None),
                patch("client_wires.fixer_autonomous.subprocess.Popen", side_effect=fake_popen),
            ):
                new_session_id = fixer_autonomous.launch_netrunner(
                    cwd,
                    6,
                    backend="droid",
                    model="custom:GLM-5.1-[Z.AI]-0",
                    reasoning="high",
                )

            conn = sqlite3.connect(db_path)
            try:
                stored = conn.execute(
                    "SELECT cli_backend, cli_model, cli_reasoning FROM session WHERE id = 60"
                ).fetchone()
            finally:
                conn.close()

        self.assertIsNone(new_session_id)
        self.assertEqual(stored, ("droid", "glm-5.1", "high"))
        self.assertIn("Preselected session ID from fixer autonomous flow: `6`.", launched["command"][-1])

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

    def test_launch_overseer_fixer_ignores_env_and_resumes_latest_fixer_session(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            cwd = Path(tmp)
            launched: dict[str, object] = {}

            def fake_popen(command: list[str], **kwargs: object) -> None:
                launched["command"] = command
                launched["kwargs"] = kwargs
                return None

            with (
                patch.dict(
                    os.environ,
                    {
                        "CODEX_THREAD_ID": "overseer-thread-from-env",
                        "CODEX_SESSION_ID": "overseer-session-from-env",
                    },
                    clear=False,
                ),
                patch.object(fixer_autonomous, "_latest_fixer_session_id", return_value="latest-fixer-session"),
                patch.object(
                    fixer_autonomous,
                    "_build_fixer_resume_command",
                    return_value=(["codex", "exec", "resume", "latest-fixer-session", "prompt"], {"ENV": "1"}),
                ) as build_command,
                patch("client_wires.fixer_autonomous.subprocess.Popen", side_effect=fake_popen),
            ):
                session_id = fixer_autonomous.launch_overseer_fixer(cwd)

        self.assertEqual(session_id, "latest-fixer-session")
        build_command.assert_called_once()
        args = build_command.call_args.args
        self.assertEqual(args[0], cwd)
        self.assertEqual(args[1], "latest-fixer-session")
        self.assertIn("fixer_mcp.get_overseer_fixer_messages", args[2])
        self.assertEqual(launched["command"], ["codex", "exec", "resume", "latest-fixer-session", "prompt"])

    def test_launch_overseer_fixer_explicit_session_id_wins_over_env_and_latest(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            cwd = Path(tmp)
            launched: dict[str, object] = {}

            def fake_popen(command: list[str], **kwargs: object) -> None:
                launched["command"] = command
                launched["kwargs"] = kwargs
                return None

            with (
                patch.dict(
                    os.environ,
                    {
                        "CODEX_THREAD_ID": "overseer-thread-from-env",
                        "CODEX_SESSION_ID": "overseer-session-from-env",
                    },
                    clear=False,
                ),
                patch.object(fixer_autonomous, "_latest_fixer_session_id", return_value="latest-fixer-session") as latest,
                patch.object(
                    fixer_autonomous,
                    "_build_fixer_resume_command",
                    return_value=(["codex", "exec", "resume", "explicit-fixer-session", "prompt"], {"ENV": "1"}),
                ) as build_command,
                patch("client_wires.fixer_autonomous.subprocess.Popen", side_effect=fake_popen),
            ):
                session_id = fixer_autonomous.launch_overseer_fixer(cwd, "explicit-fixer-session")

        self.assertEqual(session_id, "explicit-fixer-session")
        latest.assert_not_called()
        build_command.assert_called_once()
        args = build_command.call_args.args
        self.assertEqual(args[0], cwd)
        self.assertEqual(args[1], "explicit-fixer-session")
        self.assertIn("fixer_mcp.get_overseer_fixer_messages", args[2])
        self.assertEqual(launched["command"], ["codex", "exec", "resume", "explicit-fixer-session", "prompt"])


if __name__ == "__main__":
    unittest.main()
