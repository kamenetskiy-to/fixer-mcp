from __future__ import annotations

import os
import sqlite3
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from client_wires import fixer_wire_db


def _make_db(path: Path, *, sessions: int = 0, marker: str = "canon") -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    conn = sqlite3.connect(path)
    try:
        conn.execute("CREATE TABLE project (id INTEGER PRIMARY KEY, name TEXT, cwd TEXT)")
        conn.execute("INSERT INTO project (name, cwd) VALUES (?, '/tmp/canon')", (marker,))
        conn.execute("CREATE TABLE session (id INTEGER PRIMARY KEY, project_id INTEGER)")
        for _ in range(sessions):
            conn.execute("INSERT INTO session (project_id) VALUES (1)")
        conn.commit()
    finally:
        conn.close()


class DescribeDbAuthorityTests(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = Path(tempfile.mkdtemp(prefix="fixer_db_auth_"))
        self.home = self.tmp / "home"
        self.state_dir = self.tmp / "state"
        self.repo_root = self.tmp / "repo"
        self.cwd = self.repo_root / "work"
        self.home.mkdir()
        self.state_dir.mkdir()
        self.repo_root.mkdir()
        self.cwd.mkdir()

    def tearDown(self) -> None:
        import shutil

        shutil.rmtree(self.tmp, ignore_errors=True)

    def _env(self, **extra: str):
        env = {"HOME": str(self.home), "FIXER_STATE_DIR": str(self.state_dir)}
        env.update(extra)
        return patch.dict(os.environ, env, clear=True)

    def test_sanctioned_temp_override_is_respected_and_labelled(self) -> None:
        override = self.tmp / "probe" / "probe.db"
        _make_db(override)
        with self._env(FIXER_DB_PATH=str(override)):
            report = fixer_wire_db.describe_db_authority(self.cwd, repo_root=self.repo_root)
        self.assertEqual(report["effective_source"], "explicit_override")
        self.assertTrue(report["sanctioned_override"])
        self.assertEqual(report["effective_db"], str(override.resolve()))
        self.assertFalse(report["accidental_split"])
        self.assertTrue(report["preserved"])

    def test_host_canonical_is_the_default_authority(self) -> None:
        canonical = self.state_dir / "fixer.db"
        _make_db(canonical)
        with self._env():
            report = fixer_wire_db.describe_db_authority(self.cwd, repo_root=self.repo_root)
        self.assertEqual(report["effective_source"], "host_canonical")
        self.assertFalse(report["sanctioned_override"])
        self.assertEqual(report["effective_db"], str(canonical.resolve()))
        self.assertEqual(report["host_canonical_db"], str(canonical.resolve()))

    def test_path_aliases_normalize_to_one_absolute_path(self) -> None:
        override = self.tmp / "probe" / "probe.db"
        _make_db(override)
        alias = self.tmp / "probe" / "sub" / ".." / "probe.db"
        with self._env(FIXER_DB_PATH=str(alias)):
            report = fixer_wire_db.describe_db_authority(self.cwd, repo_root=self.repo_root)
        self.assertEqual(report["effective_db"], str(override.resolve()))

    def test_accidental_per_cwd_split_is_detected_and_both_databases_survive(self) -> None:
        canonical = self.state_dir / "fixer.db"
        _make_db(canonical, sessions=3, marker="canon")
        stray = self.cwd / "fixer.db"
        _make_db(stray, sessions=0, marker="stray")
        with self._env():
            report = fixer_wire_db.describe_db_authority(self.cwd, repo_root=self.repo_root)
        self.assertTrue(report["accidental_split"])
        self.assertEqual(report["stray_db"], str(stray.resolve()))
        self.assertTrue(any("data divergence in session" in line for line in report["data_divergence"]))
        self.assertIn("stray", report["fingerprints"])
        self.assertIn("effective", report["fingerprints"])
        self.assertTrue(report["preserved"])
        # Read-only diagnostics: both databases are preserved verbatim.
        self.assertTrue(stray.is_file())
        self.assertTrue(canonical.is_file())
        conn = sqlite3.connect(stray)
        try:
            marker = conn.execute("SELECT name FROM project LIMIT 1").fetchone()[0]
        finally:
            conn.close()
        self.assertEqual(marker, "stray")

    def test_diagnostics_never_migrate_or_write(self) -> None:
        # No canonical database exists yet: the resolver would migrate the
        # stray, but the diagnostics must not.
        stray = self.cwd / "fixer.db"
        _make_db(stray, marker="stray")
        with self._env():
            report = fixer_wire_db.describe_db_authority(self.cwd, repo_root=self.repo_root)
        self.assertTrue(stray.is_file())
        self.assertEqual(report["stray_db"], str(stray.resolve()))
        self.assertTrue(report["accidental_split"])

    def test_fingerprint_schema_divergence_is_reported(self) -> None:
        left = self.tmp / "left.db"
        right = self.tmp / "right.db"
        _make_db(left, sessions=1)
        right.parent.mkdir(parents=True, exist_ok=True)
        conn = sqlite3.connect(right)
        try:
            conn.execute("CREATE TABLE project (id INTEGER PRIMARY KEY, other TEXT)")
            conn.commit()
        finally:
            conn.close()
        left_fp = fixer_wire_db._db_fingerprint(left)
        right_fp = fixer_wire_db._db_fingerprint(right)
        lines = fixer_wire_db._fingerprint_divergence("left", left_fp, "right", right_fp)
        self.assertTrue(any(line.startswith("schema divergence") for line in lines))


if __name__ == "__main__":
    unittest.main()
