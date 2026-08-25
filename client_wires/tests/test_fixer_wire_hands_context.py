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


class HandsLaunchContextStorageTests(unittest.TestCase):
    def test_save_list_and_external_id_roundtrip(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            conn = _make_conn(Path(tmp) / "fixer.db")
            try:
                fixer_wire_db._save_hands_launch_context(
                    conn,
                    1,
                    worktree_path="/tmp/proj/.codex/hands_worktrees/1",
                    branch_name="hands/1",
                    provider="codex",
                    model="gpt-5.6-sol",
                    reasoning="high",
                    mcp_names=["sqlite", "fixer_mcp"],
                    doc_ids=[1, 3],
                )
                fixer_wire_db._save_hands_launch_context(
                    conn,
                    1,
                    worktree_path="/tmp/proj/.codex/hands_worktrees/2",
                    branch_name="hands/2",
                    provider="claude",
                    model="kimi/k3",
                    reasoning="high",
                    mcp_names=[],
                    doc_ids=[],
                )
                fixer_wire_db._save_hands_launch_external_id(
                    conn, 1, "/tmp/proj/.codex/hands_worktrees/1", "ext-123"
                )
                contexts = fixer_wire_db._list_hands_launch_contexts(conn, 1)
            finally:
                conn.close()

        self.assertEqual(len(contexts), 2)
        first = next(c for c in contexts if c.worktree_path.endswith("/1"))
        self.assertEqual(first.external_session_id, "ext-123")
        self.assertEqual(first.mcp_names, ("sqlite", "fixer_mcp"))
        self.assertEqual(first.doc_ids, (1, 3))
        self.assertEqual(first.provider, "codex")
        second = next(c for c in contexts if c.worktree_path.endswith("/2"))
        self.assertEqual(second.external_session_id, "")

    def test_save_upserts_on_same_worktree(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            conn = _make_conn(Path(tmp) / "fixer.db")
            try:
                for doc_ids in ([1], [2, 4]):
                    fixer_wire_db._save_hands_launch_context(
                        conn,
                        1,
                        worktree_path="/tmp/proj/.codex/hands_worktrees/1",
                        branch_name="hands/1",
                        provider="codex",
                        model="m",
                        reasoning="high",
                        mcp_names=[],
                        doc_ids=doc_ids,
                    )
                contexts = fixer_wire_db._list_hands_launch_contexts(conn, 1)
            finally:
                conn.close()

        self.assertEqual(len(contexts), 1)
        self.assertEqual(contexts[0].doc_ids, (2, 4))


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


if __name__ == "__main__":
    unittest.main()
