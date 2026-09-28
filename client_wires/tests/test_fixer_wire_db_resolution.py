from __future__ import annotations

import contextlib
import io
import os
import sqlite3
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from client_wires import fixer_wire_db


def _make_db(path: Path, *, initialized: bool = True, marker: str = "canon") -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    conn = sqlite3.connect(path)
    if initialized:
        conn.execute("CREATE TABLE project (id INTEGER PRIMARY KEY, name TEXT, cwd TEXT)")
        conn.execute("INSERT INTO project (name, cwd) VALUES (?, '/tmp/canon')", (marker,))
    conn.commit()
    conn.close()


def _read_marker(path: Path) -> str | None:
    conn = sqlite3.connect(path)
    try:
        row = conn.execute("SELECT name FROM project LIMIT 1").fetchone()
        return None if row is None else str(row[0])
    finally:
        conn.close()


def _clear_env(home: Path, *, state_dir: Path | None = None, xdg_state: Path | None = None):
    env = {"HOME": str(home)}
    if state_dir is not None:
        env["FIXER_STATE_DIR"] = str(state_dir)
    if xdg_state is not None:
        env["XDG_STATE_HOME"] = str(xdg_state)
    # clear=True also drops FIXER_DB_PATH so the launcher env of the test host
    # never short-circuits the resolver and leaks a real database into the test.
    return patch.dict(os.environ, env, clear=True)


def _resolve(cwd: Path, repo_root: Path) -> tuple[Path, str]:
    stderr = io.StringIO()
    with contextlib.redirect_stderr(stderr):
        resolved = fixer_wire_db._resolve_fixer_db_path(cwd, repo_root=repo_root)
    return resolved, stderr.getvalue()


class ResolveFixerDbPathTest(unittest.TestCase):
    def test_env_override_is_authoritative_and_never_migrates(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            cwd = root / "project"
            explicit = root / "explicit" / "fixer.db"
            stray = cwd / "fixer.db"
            _make_db(stray)

            with _clear_env(home):
                with patch.dict(os.environ, {"FIXER_DB_PATH": str(explicit)}):
                    resolved, stderr = _resolve(cwd, root)

            self.assertEqual(resolved, explicit.resolve())
            self.assertTrue(stray.is_file())
            self.assertNotIn("migrated", stderr)

    def test_relative_env_override_resolves_from_repo_root(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            cwd = root / "project"

            with _clear_env(home):
                with patch.dict(os.environ, {"FIXER_DB_PATH": "relative/fixer.db"}):
                    resolved, _stderr = _resolve(cwd, root)

            self.assertEqual(resolved, (root / "relative" / "fixer.db").resolve())

    def test_checkout_project_db_wins_over_state_and_strays(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            cwd = root / "project"
            project_db = root / "fixer_mcp" / "fixer.db"
            state_db = home / ".local" / "state" / "fixer-client-wires" / "fixer.db"
            root_stray = root / "fixer.db"
            cwd_stray = cwd / "fixer.db"
            _make_db(project_db, marker="project")
            _make_db(state_db, marker="state")
            _make_db(root_stray, marker="root-stray")
            _make_db(cwd_stray, marker="cwd-stray")

            with _clear_env(home):
                resolved, stderr = _resolve(cwd, root)

            self.assertEqual(resolved, project_db.resolve())
            # Neither stray is touched; the checkout database is the truth.
            self.assertTrue(root_stray.is_file())
            self.assertTrue(cwd_stray.is_file())
            self.assertEqual(stderr, "")

    def test_stray_cwd_db_does_not_shadow_host_canonical_db(self) -> None:
        """Regression for the 2026-09-25 incident.

        A stray ``<cwd>/fixer.db`` used to be a silent candidate and captured
        the launcher, showing an empty project world while the canonical state
        lived in the host state directory.
        """

        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            cwd = root / "project"
            canonical = home / ".local" / "state" / "fixer-client-wires" / "fixer.db"
            stray = cwd / "fixer.db"
            _make_db(stray, marker="stray")
            _make_db(canonical, marker="canonical")

            with _clear_env(home):
                resolved, stderr = _resolve(cwd, root)

            self.assertEqual(resolved, canonical.resolve())
            self.assertTrue(stray.is_file(), "stray must be left untouched")
            self.assertEqual(_read_marker(stray), "stray")
            self.assertIn("ignored", stderr)
            self.assertIn(f"FIXER_DB_PATH={stray.resolve()}", stderr)

    def test_stray_root_db_ignored_when_canonical_exists(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            cwd = root / "project"
            canonical = home / ".local" / "state" / "fixer-client-wires" / "fixer.db"
            stray = root / "fixer.db"
            _make_db(stray, marker="stray")
            _make_db(canonical, marker="canonical")

            with _clear_env(home):
                resolved, stderr = _resolve(cwd, root)

            self.assertEqual(resolved, canonical.resolve())
            self.assertTrue(stray.is_file())
            self.assertIn(f"FIXER_DB_PATH={stray.resolve()}", stderr)

    def test_stray_migrates_once_when_no_canonical_state_exists(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            cwd = root / "project"
            stray = cwd / "fixer.db"
            target = home / ".local" / "state" / "fixer-client-wires" / "fixer.db"
            _make_db(stray, marker="operator-data")

            with _clear_env(home):
                first, first_stderr = _resolve(cwd, root)
                self.assertFalse(stray.exists(), "stray must be moved away")
                self.assertTrue(target.is_file())
                self.assertEqual(_read_marker(target), "operator-data")
                self.assertIn("migrated", first_stderr)
                self.assertIn(str(target.resolve()), first_stderr)

                second, second_stderr = _resolve(cwd, root)

            self.assertEqual(first, target.resolve())
            self.assertEqual(second, target.resolve())
            self.assertEqual(_read_marker(target), "operator-data")
            self.assertNotIn("migrated", second_stderr)

    def test_stray_migration_creates_parent_with_mode_0700(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            cwd = root / "project"
            stray = root / "fixer.db"
            target = home / ".local" / "state" / "fixer-client-wires" / "fixer.db"
            _make_db(stray, marker="root-data")

            with _clear_env(home):
                resolved, stderr = _resolve(cwd, root)

            self.assertEqual(resolved, target.resolve())
            self.assertEqual(_read_marker(target), "root-data")
            self.assertEqual(target.parent.stat().st_mode & 0o777, 0o700)
            self.assertIn("migrated", stderr)

    def test_fresh_host_resolves_to_preferred_state_path(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            cwd = root / "project"
            target = home / ".local" / "state" / "fixer-client-wires" / "fixer.db"

            with _clear_env(home):
                resolved, stderr = _resolve(cwd, root)

            self.assertEqual(resolved, target.resolve())
            self.assertTrue(target.parent.is_dir())
            self.assertEqual(target.parent.stat().st_mode & 0o777, 0o700)
            self.assertEqual(stderr, "")

    def test_state_dir_beats_xdg_and_home(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            cwd = root / "project"
            state_db = root / "explicit-state" / "fixer.db"
            xdg_db = root / "xdg" / "fixer-client-wires" / "fixer.db"
            home_db = home / ".local" / "state" / "fixer-client-wires" / "fixer.db"
            _make_db(state_db, marker="state-dir")
            _make_db(xdg_db, marker="xdg")
            _make_db(home_db, marker="home")

            with _clear_env(home, state_dir=root / "explicit-state", xdg_state=root / "xdg"):
                resolved, _stderr = _resolve(cwd, root)

            self.assertEqual(resolved, state_db.resolve())

    def test_xdg_beats_home_when_state_dir_missing(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            cwd = root / "project"
            xdg_db = root / "xdg" / "fixer-client-wires" / "fixer.db"
            home_db = home / ".local" / "state" / "fixer-client-wires" / "fixer.db"
            _make_db(xdg_db, marker="xdg")
            _make_db(home_db, marker="home")

            with _clear_env(home, state_dir=root / "explicit-state", xdg_state=root / "xdg"):
                resolved, _stderr = _resolve(cwd, root)

            self.assertEqual(resolved, xdg_db.resolve())

    def test_home_state_used_when_state_dir_and_xdg_missing(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            cwd = root / "project"
            home_db = home / ".local" / "state" / "fixer-client-wires" / "fixer.db"
            _make_db(home_db, marker="home")

            with _clear_env(home, state_dir=root / "explicit-state", xdg_state=root / "xdg"):
                resolved, _stderr = _resolve(cwd, root)

            self.assertEqual(resolved, home_db.resolve())

    def test_preferred_state_path_priority_when_none_exist(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            cwd = root / "project"

            with _clear_env(home, state_dir=root / "explicit-state", xdg_state=root / "xdg"):
                resolved, _stderr = _resolve(cwd, root)
            self.assertEqual(resolved, (root / "explicit-state" / "fixer.db").resolve())

            with _clear_env(home, xdg_state=root / "xdg"):
                resolved, _stderr = _resolve(cwd, root)
            self.assertEqual(resolved, (root / "xdg" / "fixer-client-wires" / "fixer.db").resolve())

            with _clear_env(home):
                resolved, _stderr = _resolve(cwd, root)
            self.assertEqual(
                resolved,
                (home / ".local" / "state" / "fixer-client-wires" / "fixer.db").resolve(),
            )

    def test_cwd_local_fixer_mcp_db_is_not_a_candidate(self) -> None:
        """Only the launcher repo's ``fixer_mcp/fixer.db`` is canonical."""

        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            cwd = root / "project"
            cwd_db = cwd / "fixer_mcp" / "fixer.db"
            canonical = home / ".local" / "state" / "fixer-client-wires" / "fixer.db"
            _make_db(cwd_db, marker="cwd-mcp")
            _make_db(canonical, marker="canonical")

            with _clear_env(home):
                resolved, _stderr = _resolve(cwd, root)

            self.assertEqual(resolved, canonical.resolve())

    def test_managed_runtime_root_is_not_consulted(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            cwd = root / "project"
            canonical = home / ".local" / "state" / "fixer-client-wires" / "fixer.db"
            _make_db(canonical, marker="canonical")

            def _explode(*_args, **_kwargs):
                raise AssertionError("managed runtime root must not be a resolution candidate")

            with _clear_env(home):
                with patch("installer.paths.resolve_db_path", side_effect=_explode):
                    resolved, _stderr = _resolve(cwd, root)

            self.assertEqual(resolved, canonical.resolve())


if __name__ == "__main__":
    unittest.main()
