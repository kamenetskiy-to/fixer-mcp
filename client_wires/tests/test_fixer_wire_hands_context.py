from __future__ import annotations

import sqlite3
import subprocess
import tempfile
import unittest
from pathlib import Path

from client_wires import fixer_wire_db
from client_wires import fixer_wire_hands_context


def _make_conn(db_path: Path) -> sqlite3.Connection:
    conn = sqlite3.connect(db_path)
    conn.execute("CREATE TABLE project (id INTEGER PRIMARY KEY, name TEXT, cwd TEXT)")
    conn.execute("INSERT INTO project (id, name, cwd) VALUES (1, 'proj', '/tmp/proj')")
    fixer_wire_db._ensure_wire_schema(conn)
    return conn


class HandsWorktreeTests(unittest.TestCase):
    def test_create_hands_worktree_builds_clean_tree_on_new_branch(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            repo = Path(tmp) / "repo"
            repo.mkdir()
            subprocess.run(["git", "init"], cwd=repo, check=True, capture_output=True)
            subprocess.run(["git", "config", "user.email", "t@t"], cwd=repo, check=True)
            subprocess.run(["git", "config", "user.name", "t"], cwd=repo, check=True)
            (repo / "file.txt").write_text("base", encoding="utf-8")
            subprocess.run(["git", "add", "."], cwd=repo, check=True)
            subprocess.run(["git", "commit", "-m", "init"], cwd=repo, check=True, capture_output=True)
            (repo / "dirty.txt").write_text("uncommitted", encoding="utf-8")

            worktree_path, branch_name = fixer_wire_hands_context.create_hands_worktree(repo)

            self.assertTrue((worktree_path / "file.txt").is_file())
            self.assertFalse((worktree_path / "dirty.txt").exists())
            self.assertTrue(branch_name.startswith("hands/"))
            current = subprocess.run(
                ["git", "branch", "--show-current"],
                cwd=worktree_path,
                check=True,
                capture_output=True,
                text=True,
            ).stdout.strip()
            self.assertEqual(current, branch_name)


class HandsDocsMaterializationTests(unittest.TestCase):
    def test_materialize_hands_docs_writes_index_and_files(self) -> None:
        docs = [
            fixer_wire_hands_context.HandsDocEntry(
                doc_id=1, title="Overview", content="root body", level=0,
                slug="overview", path="overview", status="current",
            ),
            fixer_wire_hands_context.HandsDocEntry(
                doc_id=2, title="Child Doc", content="child body", level=1,
                slug="child", path="overview/child", status="draft",
            ),
        ]
        with tempfile.TemporaryDirectory() as tmp:
            worktree = Path(tmp)
            written = fixer_wire_hands_context.materialize_hands_docs(worktree, docs)
            index = (worktree / ".hands/project_docs/_index.md").read_text(encoding="utf-8")
            first = (worktree / written[0]).read_text(encoding="utf-8")

        self.assertEqual(len(written), 2)
        self.assertIn("Overview", index)
        self.assertIn("  - [Child Doc]", index)
        self.assertIn("root body", first)
        self.assertIn("doc_id: 1", first)

    def test_load_project_doc_tree_computes_local_ids(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            conn = _make_conn(Path(tmp) / "fixer.db")
            try:
                conn.execute(
                    """
                    CREATE TABLE project_doc (
                        id INTEGER PRIMARY KEY AUTOINCREMENT,
                        project_id INTEGER NOT NULL,
                        title TEXT NOT NULL,
                        content TEXT NOT NULL,
                        doc_type TEXT,
                        parent_doc_id INTEGER,
                        level INTEGER,
                        slug TEXT,
                        path TEXT,
                        status TEXT
                    )
                    """
                )
                conn.execute(
                    "INSERT INTO project_doc (project_id, title, content, level, slug, path, status) "
                    "VALUES (1, 'A', 'a-body', 0, 'a', 'a', 'current')"
                )
                conn.execute(
                    "INSERT INTO project_doc (project_id, title, content, parent_doc_id, level, slug, path, status) "
                    "VALUES (1, 'B', 'b-body', 1, 1, 'b', 'a/b', 'stale')"
                )
                entries = fixer_wire_hands_context.load_project_doc_tree(conn, 1)
            finally:
                conn.close()

        self.assertEqual([entry.doc_id for entry in entries], [1, 2])
        self.assertEqual(entries[0].title, "A")
        self.assertEqual(entries[0].parent_id, 0)
        self.assertEqual(entries[1].content, "b-body")
        self.assertEqual(entries[1].level, 1)
        self.assertEqual(entries[1].parent_id, 1)

    def test_load_tree_uses_russian_display_title_but_materializes_english_canon(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            conn = _make_conn(Path(tmp) / "fixer.db")
            try:
                conn.executescript(
                    """
                    CREATE TABLE project_doc (
                        id INTEGER PRIMARY KEY AUTOINCREMENT,
                        project_id INTEGER NOT NULL,
                        title TEXT NOT NULL,
                        content TEXT NOT NULL,
                        doc_type TEXT,
                        parent_doc_id INTEGER,
                        level INTEGER,
                        slug TEXT,
                        path TEXT,
                        status TEXT
                    );
                    CREATE TABLE project_doc_language_policy (
                        project_id INTEGER PRIMARY KEY,
                        language_code TEXT NOT NULL
                    );
                    CREATE TABLE project_doc_title_localization (
                        project_doc_id INTEGER NOT NULL,
                        language_code TEXT NOT NULL,
                        localized_title TEXT NOT NULL,
                        UNIQUE(project_doc_id, language_code)
                    );
                    INSERT INTO project_doc_language_policy VALUES (1, 'ru');
                    INSERT INTO project_doc (project_id, title, content, level, slug, path, status)
                    VALUES (1, 'Architecture', 'English canonical body.', 0, 'architecture', 'architecture', 'current');
                    INSERT INTO project_doc_title_localization VALUES (1, 'ru', 'Архитектура');
                    """
                )
                entries = fixer_wire_hands_context.load_project_doc_tree(conn, 1)
            finally:
                conn.close()

            worktree = Path(tmp) / "materialized"
            written = fixer_wire_hands_context.materialize_hands_docs(worktree, entries)
            content = (worktree / written[0]).read_text(encoding="utf-8")

        self.assertEqual(entries[0].title, "Architecture")
        self.assertEqual(entries[0].display_title, "Архитектура")
        self.assertIn("# Architecture", content)
        self.assertNotIn("# Архитектура", content)


if __name__ == "__main__":
    unittest.main()
