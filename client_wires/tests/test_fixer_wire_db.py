from __future__ import annotations

import os
import sqlite3
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from client_wires import fixer_wire
from client_wires import fixer_wire_db


class FixerWireDbExtractionTests(unittest.TestCase):
    def test_storage_helpers_delegate_to_db_module_while_facade_symbols_remain(self) -> None:
        conn = object()
        with patch.object(fixer_wire_db, "_ensure_wire_schema") as delegated:
            fixer_wire._ensure_wire_schema(conn)  # type: ignore[arg-type]

        delegated.assert_called_once_with(conn)
        self.assertIs(fixer_wire.SessionRow, fixer_wire_db.SessionRow)
        self.assertIs(fixer_wire.SessionLaunchSelection, fixer_wire_db.SessionLaunchSelection)
        self.assertIs(fixer_wire.RegistryMcpMetadata, fixer_wire_db.RegistryMcpMetadata)

    def test_storage_wrappers_preserve_patchable_fixer_wire_dependencies(self) -> None:
        conn = object()
        with patch.object(fixer_wire_db, "_persist_session_mcp_names") as delegated:
            fixer_wire._persist_session_mcp_names(conn, 7, ["sqlite"])  # type: ignore[arg-type]

        delegated.assert_called_once()
        self.assertIs(delegated.call_args.kwargs["normalize_names"], fixer_wire._normalize_names)
        self.assertIs(delegated.call_args.kwargs["sync_registry_names"], fixer_wire._sync_registry_names)


class ResolveFixerDbPathTests(unittest.TestCase):
    def test_prefers_repo_local_db_over_cwd_db(self) -> None:
        with tempfile.TemporaryDirectory() as repo_tmp, tempfile.TemporaryDirectory() as cwd_tmp:
            repo_root = Path(repo_tmp)
            cwd = Path(cwd_tmp)
            repo_db = repo_root / "fixer_mcp" / "fixer.db"
            cwd_db = cwd / "fixer_mcp" / "fixer.db"
            repo_db.parent.mkdir(parents=True, exist_ok=True)
            cwd_db.parent.mkdir(parents=True, exist_ok=True)
            repo_db.touch()
            cwd_db.touch()

            with (
                patch.object(fixer_wire, "_repo_root", return_value=repo_root),
                patch.dict(os.environ, {fixer_wire.FIXER_DB_PATH_ENV: ""}, clear=False),
            ):
                resolved = fixer_wire._resolve_fixer_db_path(cwd)

        self.assertEqual(resolved, repo_db.resolve())

    def test_ignores_fixer_genui_db_when_legacy_fixer_db_exists(self) -> None:
        with tempfile.TemporaryDirectory() as repo_tmp, tempfile.TemporaryDirectory() as cwd_tmp:
            repo_root = Path(repo_tmp)
            cwd = Path(cwd_tmp)
            repo_db = repo_root / "fixer_mcp" / "fixer.db"
            rogue_genui_db = repo_root / "fixer_mcp" / "fixer_genui.db"
            repo_db.parent.mkdir(parents=True, exist_ok=True)
            repo_db.touch()
            rogue_genui_db.touch()

            with (
                patch.object(fixer_wire, "_repo_root", return_value=repo_root),
                patch.dict(os.environ, {fixer_wire.FIXER_DB_PATH_ENV: ""}, clear=False),
            ):
                resolved = fixer_wire._resolve_fixer_db_path(cwd)

        self.assertEqual(resolved, repo_db.resolve())

    def test_uses_env_override_first(self) -> None:
        with tempfile.TemporaryDirectory() as repo_tmp, tempfile.TemporaryDirectory() as cwd_tmp:
            repo_root = Path(repo_tmp)
            cwd = Path(cwd_tmp)
            repo_db = repo_root / "fixer_mcp" / "fixer.db"
            env_db = repo_root / "custom" / "wire.db"
            repo_db.parent.mkdir(parents=True, exist_ok=True)
            env_db.parent.mkdir(parents=True, exist_ok=True)
            repo_db.touch()
            env_db.touch()

            with (
                patch.object(fixer_wire, "_repo_root", return_value=repo_root),
                patch.dict(os.environ, {fixer_wire.FIXER_DB_PATH_ENV: str(env_db)}, clear=False),
            ):
                resolved = fixer_wire._resolve_fixer_db_path(cwd)

        self.assertEqual(resolved, env_db.resolve())

    def test_relative_env_override_is_resolved_from_repo_root(self) -> None:
        with tempfile.TemporaryDirectory() as repo_tmp, tempfile.TemporaryDirectory() as cwd_tmp:
            repo_root = Path(repo_tmp)
            cwd = Path(cwd_tmp)
            env_db = repo_root / "relative" / "fixer.db"
            env_db.parent.mkdir(parents=True, exist_ok=True)
            env_db.touch()

            with (
                patch.object(fixer_wire, "_repo_root", return_value=repo_root),
                patch.dict(os.environ, {fixer_wire.FIXER_DB_PATH_ENV: "relative/fixer.db"}, clear=False),
            ):
                resolved = fixer_wire._resolve_fixer_db_path(cwd)

        self.assertEqual(resolved, env_db.resolve())

    def test_falls_back_to_cwd_when_repo_db_missing(self) -> None:
        with tempfile.TemporaryDirectory() as repo_tmp, tempfile.TemporaryDirectory() as cwd_tmp:
            repo_root = Path(repo_tmp)
            cwd = Path(cwd_tmp)
            cwd_db = cwd / "fixer.db"
            cwd_db.touch()

            with (
                patch.object(fixer_wire, "_repo_root", return_value=repo_root),
                patch.dict(os.environ, {fixer_wire.FIXER_DB_PATH_ENV: ""}, clear=False),
            ):
                resolved = fixer_wire._resolve_fixer_db_path(cwd)

        self.assertEqual(resolved, cwd_db.resolve())

    def test_error_lists_checked_candidates(self) -> None:
        with tempfile.TemporaryDirectory() as repo_tmp, tempfile.TemporaryDirectory() as cwd_tmp:
            repo_root = Path(repo_tmp)
            cwd = Path(cwd_tmp)
            with (
                patch.object(fixer_wire, "_repo_root", return_value=repo_root),
                patch.dict(os.environ, {fixer_wire.FIXER_DB_PATH_ENV: ""}, clear=False),
            ):
                with self.assertRaises(RuntimeError) as ctx:
                    fixer_wire._resolve_fixer_db_path(cwd)

        message = str(ctx.exception)
        self.assertIn(f"Could not locate {fixer_wire.PRIMARY_FIXER_DB_FILENAME}.", message)
        self.assertIn(str((repo_root / "fixer_mcp" / "fixer.db").resolve()), message)
        self.assertIn(str((cwd / "fixer.db").resolve()), message)
        self.assertIn(fixer_wire.FIXER_DB_PATH_ENV, message)


class ResolveProjectIdTests(unittest.TestCase):
    def _make_project_conn(self) -> sqlite3.Connection:
        conn = sqlite3.connect(":memory:")
        conn.execute(
            """
            CREATE TABLE project (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                name TEXT NOT NULL,
                cwd TEXT UNIQUE NOT NULL
            );
            """
        )
        return conn

    def test_unknown_project_can_be_registered_with_prompted_name(self) -> None:
        conn = self._make_project_conn()
        try:
            missing_cwd = Path("/tmp/unknown-project-root")
            project_id = fixer_wire._ensure_project_registered(
                conn,
                missing_cwd,
                name_reader=lambda _prompt: "Prompted Project",
            )
            row = conn.execute("SELECT id, name, cwd FROM project").fetchone()
        finally:
            conn.close()

        self.assertEqual(project_id, 1)
        self.assertEqual(row, (1, "Prompted Project", str(missing_cwd.resolve())))

    def test_blank_project_name_uses_default_cwd_name(self) -> None:
        conn = self._make_project_conn()
        try:
            missing_cwd = Path("/tmp/default-name-project")
            project_id = fixer_wire._ensure_project_registered(
                conn,
                missing_cwd,
                name_reader=lambda _prompt: "   ",
            )
            row = conn.execute("SELECT id, name, cwd FROM project").fetchone()
        finally:
            conn.close()

        self.assertEqual(project_id, 1)
        self.assertEqual(row, (1, "default-name-project", str(missing_cwd.resolve())))

    def test_existing_nested_project_is_not_duplicated(self) -> None:
        conn = self._make_project_conn()
        try:
            parent_cwd = str(Path("/tmp/existing-project").resolve())
            conn.execute(
                "INSERT INTO project (id, name, cwd) VALUES (7, 'Existing Project', ?)",
                (parent_cwd,),
            )

            def fail_if_prompted(_prompt: str) -> str:
                self.fail("existing project should not prompt for onboarding")

            project_id = fixer_wire._ensure_project_registered(
                conn,
                Path("/tmp/existing-project/nested"),
                name_reader=fail_if_prompted,
            )
            rows = conn.execute("SELECT id, name, cwd FROM project").fetchall()
        finally:
            conn.close()

        self.assertEqual(project_id, 7)
        self.assertEqual(rows, [(7, "Existing Project", parent_cwd)])

    def test_project_onboarding_cancel_raises_clear_runtime_error(self) -> None:
        conn = self._make_project_conn()
        try:
            missing_cwd = Path("/tmp/cancelled-project")
            with self.assertRaises(RuntimeError) as ctx:
                fixer_wire._ensure_project_registered(
                    conn,
                    missing_cwd,
                    name_reader=lambda _prompt: "q",
                )
        finally:
            conn.close()

        message = str(ctx.exception)
        self.assertIn("Project onboarding cancelled", message)
        self.assertIn(str(missing_cwd.resolve()), message)

    def test_unattached_fixer_project_creation_is_idempotent_and_noninteractive(self) -> None:
        conn = self._make_project_conn()
        try:
            fixer_wire._ensure_wire_schema(conn)
            conn.execute(
                """
                CREATE TABLE mcp_server (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    name TEXT NOT NULL UNIQUE,
                    is_default INTEGER NOT NULL DEFAULT 0
                );
                """
            )
            conn.execute("CREATE TABLE session (id INTEGER PRIMARY KEY AUTOINCREMENT, project_id INTEGER)")
            conn.execute(
                """
                CREATE TABLE session_mcp_server (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    session_id INTEGER NOT NULL,
                    mcp_server_id INTEGER NOT NULL
                );
                """
            )
            conn.execute("INSERT INTO mcp_server (name, is_default) VALUES ('fixer_mcp', 1)")
            with tempfile.TemporaryDirectory() as tmp:
                scratch_cwd = Path(tmp) / "scratch"
                first_id, first_cwd = fixer_wire._ensure_unattached_fixer_project(conn, scratch_cwd=scratch_cwd)
                second_id, second_cwd = fixer_wire._ensure_unattached_fixer_project(conn, scratch_cwd=scratch_cwd)
                rows = conn.execute("SELECT id, name, cwd FROM project ORDER BY id").fetchall()
                bindings = conn.execute("SELECT COUNT(*) FROM project_mcp_server WHERE project_id = ?", (first_id,)).fetchone()
                self.assertTrue(first_cwd.is_dir())
        finally:
            conn.close()

        self.assertEqual(first_id, second_id)
        self.assertEqual(first_cwd, second_cwd)
        self.assertEqual(rows, [(1, fixer_wire.UNATTACHED_FIXER_PROJECT_NAME, str(first_cwd))])
        self.assertEqual(bindings[0], 1)

    def test_default_unattached_fixer_cwd_is_unique_run_directory(self) -> None:
        with tempfile.TemporaryDirectory() as home:
            with patch.dict(os.environ, {"HOME": home}, clear=True):
                first_cwd = fixer_wire._resolve_unattached_fixer_cwd()
                second_cwd = fixer_wire._resolve_unattached_fixer_cwd()

        base_cwd = (Path(home) / ".codex" / "fixer_unattached" / "runs").resolve()
        self.assertNotEqual(first_cwd, second_cwd)
        self.assertEqual(first_cwd.parent, base_cwd)
        self.assertEqual(second_cwd.parent, base_cwd)

    def test_unattached_fixer_cwd_env_remains_exact_override(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            scratch_cwd = Path(tmp) / "manual-scratch"
            with patch.dict(os.environ, {fixer_wire.FIXER_UNATTACHED_CWD_ENV: str(scratch_cwd)}, clear=True):
                resolved = fixer_wire._resolve_unattached_fixer_cwd()

        self.assertEqual(resolved, scratch_cwd.resolve())


class ProjectScopedMcpBindingTests(unittest.TestCase):
    def test_load_project_allowed_mcp_names_isolated_per_project(self) -> None:
        conn = sqlite3.connect(":memory:")
        try:
            conn.executescript(
                """
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
                    name TEXT NOT NULL UNIQUE,
                    is_default INTEGER NOT NULL DEFAULT 0
                );
                CREATE TABLE session_mcp_server (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    session_id INTEGER NOT NULL,
                    mcp_server_id INTEGER NOT NULL
                );
                CREATE TABLE project_mcp_server (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    project_id INTEGER NOT NULL,
                    mcp_server_id INTEGER NOT NULL,
                    UNIQUE(project_id, mcp_server_id)
                );
                INSERT INTO project (id, name, cwd) VALUES
                    (1, 'Philologists Paradise', '/tmp/philologists'),
                    (2, 'Other', '/tmp/other');
                INSERT INTO mcp_server (id, name, is_default) VALUES
                    (1, 'sqlite', 1),
                    (2, 'research_query_mcp', 0);
                INSERT INTO project_mcp_server (project_id, mcp_server_id) VALUES
                    (1, 1),
                    (1, 2),
                    (2, 1);
                """
            )

            philologists_allowed = fixer_wire._load_project_allowed_mcp_names(conn, 1)
            other_allowed = fixer_wire._load_project_allowed_mcp_names(conn, 2)
        finally:
            conn.close()

        self.assertIn("research_query_mcp", philologists_allowed)
        self.assertNotIn("research_query_mcp", other_allowed)

    def test_allowed_runtime_and_preselected_mcp_names(self) -> None:
        assigned = ["sqlite", "research_query_mcp", "fixer_mcp"]
        allowed_project_a = ["sqlite", "research_query_mcp"]
        allowed_project_b = ["sqlite"]
        available = {"sqlite": {}, "research_query_mcp": {}, "gopls": {}}

        project_a_allowed_runtime = fixer_wire._allowed_runtime_mcp_names(allowed_project_a, available)
        project_b_allowed_runtime = fixer_wire._allowed_runtime_mcp_names(allowed_project_b, available)
        project_a_preselected = fixer_wire._assigned_preselected_mcp_names(assigned, project_a_allowed_runtime)
        project_b_preselected = fixer_wire._assigned_preselected_mcp_names(assigned, project_b_allowed_runtime)

        self.assertEqual(project_a_allowed_runtime, ["research_query_mcp", "sqlite"])
        self.assertEqual(project_b_allowed_runtime, ["sqlite"])
        self.assertEqual(project_a_preselected, ["research_query_mcp", "sqlite"])
        self.assertEqual(project_b_preselected, ["sqlite"])

    def test_allowed_runtime_excludes_react_native_guide_by_default(self) -> None:
        allowed = ["sqlite", "react-native-guide"]
        available = {"sqlite": {}, "react-native-guide": {}}

        allowed_runtime = fixer_wire._allowed_runtime_mcp_names(allowed, available)

        self.assertEqual(allowed_runtime, ["sqlite"])

    def test_assigned_allowed_includes_unavailable_for_visibility(self) -> None:
        assigned = ["sqlite", "research_query_mcp"]
        allowed = ["sqlite", "research_query_mcp"]
        available = {"sqlite": {}}

        assigned_allowed = fixer_wire._assigned_allowed_mcp_names(assigned, allowed)
        allowed_runtime = fixer_wire._allowed_runtime_mcp_names(allowed, available)
        assigned_unavailable = sorted(set(assigned_allowed) - set(allowed_runtime))
        picker_pool = sorted(set(allowed_runtime).union(assigned_unavailable))

        self.assertEqual(assigned_allowed, ["research_query_mcp", "sqlite"])
        self.assertEqual(allowed_runtime, ["sqlite"])
        self.assertEqual(assigned_unavailable, ["research_query_mcp"])
        self.assertEqual(picker_pool, ["research_query_mcp", "sqlite"])


class SessionCodexLinkPersistenceTests(unittest.TestCase):
    def test_ensure_wire_schema_migrates_legacy_codex_links_into_external_link_table(self) -> None:
        conn = sqlite3.connect(":memory:")
        try:
            conn.executescript(
                """
                CREATE TABLE session (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    project_id INTEGER NOT NULL,
                    task_description TEXT NOT NULL,
                    status TEXT NOT NULL
                );
                CREATE TABLE session_codex_link (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    session_id INTEGER NOT NULL,
                    codex_session_id TEXT NOT NULL,
                    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
                );
                INSERT INTO session (id, project_id, task_description, status)
                VALUES (4, 1, 'Legacy session', 'review');
                INSERT INTO session_codex_link (session_id, codex_session_id)
                VALUES (4, 'legacy-codex-4');
                """
            )

            fixer_wire._ensure_wire_schema(conn)
            rows = conn.execute(
                """
                SELECT backend, external_session_id
                FROM session_external_link
                WHERE session_id = 4
                """
            ).fetchall()
        finally:
            conn.close()

        self.assertEqual(rows, [("codex", "legacy-codex-4")])

    def test_load_session_rows_prefers_latest_codex_link_for_legacy_duplicates(self) -> None:
        conn = sqlite3.connect(":memory:")
        try:
            conn.executescript(
                """
                CREATE TABLE session (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    project_id INTEGER NOT NULL,
                    task_description TEXT NOT NULL,
                    status TEXT NOT NULL
                );
                CREATE TABLE session_codex_link (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    session_id INTEGER NOT NULL,
                    codex_session_id TEXT NOT NULL,
                    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
                );
                INSERT INTO session (id, project_id, task_description, status)
                VALUES (7, 2, 'Repair resume flow', 'review');
                INSERT INTO session_codex_link (session_id, codex_session_id, updated_at)
                VALUES
                    (7, 'resume-old', '2026-03-01 09:00:00'),
                    (7, 'resume-new', '2026-03-01 10:00:00');
                """
            )

            fixer_wire._ensure_wire_schema(conn)
            rows = fixer_wire._load_session_rows(conn, 2)
        finally:
            conn.close()

        self.assertEqual(len(rows), 1)
        self.assertEqual(rows[0].codex_session_id, "resume-new")

    def test_load_session_external_id_falls_back_to_legacy_codex_link(self) -> None:
        conn = sqlite3.connect(":memory:")
        try:
            conn.executescript(
                """
                CREATE TABLE session (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    project_id INTEGER NOT NULL,
                    task_description TEXT NOT NULL,
                    status TEXT NOT NULL
                );
                CREATE TABLE session_codex_link (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    session_id INTEGER NOT NULL,
                    codex_session_id TEXT NOT NULL,
                    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
                );
                INSERT INTO session (id, project_id, task_description, status)
                VALUES (11, 2, 'Fallback session', 'review');
                INSERT INTO session_codex_link (session_id, codex_session_id)
                VALUES (11, 'legacy-fallback-11');
                """
            )

            fixer_wire._ensure_wire_schema(conn)
            external_session_id = fixer_wire._load_session_external_id(conn, 11, "codex")
        finally:
            conn.close()

        self.assertEqual(external_session_id, "legacy-fallback-11")

    def test_ensure_wire_schema_deduplicates_links_and_enforces_unique_session_mapping(self) -> None:
        conn = sqlite3.connect(":memory:")
        try:
            conn.executescript(
                """
                CREATE TABLE session_codex_link (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    session_id INTEGER NOT NULL,
                    codex_session_id TEXT NOT NULL,
                    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
                );
                INSERT INTO session_codex_link (session_id, codex_session_id, updated_at)
                VALUES
                    (9, 'resume-old', '2026-03-01 09:00:00'),
                    (9, 'resume-new', '2026-03-01 10:00:00');
                """
            )

            fixer_wire._ensure_wire_schema(conn)
            fixer_wire._save_session_codex_id(conn, 9, "resume-stable")
            rows = conn.execute(
                """
                SELECT codex_session_id
                FROM session_codex_link
                WHERE session_id = 9
                ORDER BY id
                """
            ).fetchall()
            indexes = conn.execute("PRAGMA index_list('session_codex_link')").fetchall()
        finally:
            conn.close()

        self.assertEqual(rows, [("resume-stable",)])
        self.assertTrue(any(index[2] for index in indexes))


if __name__ == "__main__":
    unittest.main()
