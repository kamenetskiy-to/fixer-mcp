from __future__ import annotations

import os
import sqlite3
import tempfile
import types
import unittest
from pathlib import Path
from unittest.mock import patch

from client_wires import fixer_wire_db


def _make_db(path: Path, *, initialized: bool) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    conn = sqlite3.connect(path)
    if initialized:
        conn.execute("CREATE TABLE project (id INTEGER PRIMARY KEY, name TEXT, cwd TEXT)")
        conn.execute("INSERT INTO project (name, cwd) VALUES ('canon', '/tmp/canon')")
    conn.commit()
    conn.close()


def _patch_managed_db(managed_db: Path):
    """Point the installer's managed database resolution at a fixed path."""

    real_import = fixer_wire_db.importlib.import_module

    def fake_import(name, *args, **kwargs):
        if name == "installer.paths":
            return types.SimpleNamespace(resolve_db_path=lambda: str(managed_db))
        return real_import(name, *args, **kwargs)

    return patch.object(fixer_wire_db.importlib, "import_module", fake_import)


class ResolveFixerDbPathTest(unittest.TestCase):
    def test_env_override_is_authoritative(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            explicit = root / "explicit" / "fixer.db"
            with patch.dict(os.environ, {"FIXER_DB_PATH": str(explicit)}):
                resolved = fixer_wire_db._resolve_fixer_db_path(root / "project", repo_root=root)
            self.assertEqual(resolved, explicit.resolve())

    def test_initialized_checkout_db_beats_empty_managed_db(self) -> None:
        """Regression: an empty managed state database must not shadow the real one.

        The out-of-band managed install on a machine that also holds a source
        checkout left an empty state database behind; the launcher picked it and
        died with "no such table: project" although the canonical checkout
        database existed.
        """

        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            checkout_db = root / "fixer_mcp" / "fixer.db"
            managed_db = root / "state" / "fixer.db"
            _make_db(checkout_db, initialized=True)
            _make_db(managed_db, initialized=False)

            env = {k: v for k, v in os.environ.items() if k != "FIXER_DB_PATH"}
            with patch.dict(os.environ, env, clear=True), _patch_managed_db(managed_db):
                resolved = fixer_wire_db._resolve_fixer_db_path(root / "fixer_mcp", repo_root=root)

            self.assertEqual(resolved, checkout_db.resolve())

    def test_empty_managed_db_is_used_for_a_fresh_install(self) -> None:
        """A payload-only install has no checkout database: the empty state db wins."""

        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            managed_db = root / "state" / "fixer.db"
            _make_db(managed_db, initialized=False)

            env = {k: v for k, v in os.environ.items() if k != "FIXER_DB_PATH"}
            with patch.dict(os.environ, env, clear=True), _patch_managed_db(managed_db):
                resolved = fixer_wire_db._resolve_fixer_db_path(root / "project", repo_root=root)

            self.assertEqual(resolved, managed_db.resolve())

    def test_missing_everywhere_reports_every_candidate(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            managed_db = root / "state" / "fixer.db"
            env = {k: v for k, v in os.environ.items() if k != "FIXER_DB_PATH"}
            with patch.dict(os.environ, env, clear=True), _patch_managed_db(managed_db):
                with self.assertRaises(RuntimeError) as raised:
                    fixer_wire_db._resolve_fixer_db_path(root / "project", repo_root=root)
            message = str(raised.exception)
            self.assertIn("Could not locate fixer.db", message)
            self.assertIn("FIXER_DB_PATH", message)


if __name__ == "__main__":
    unittest.main()
