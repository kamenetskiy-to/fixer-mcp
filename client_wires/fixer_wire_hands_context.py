"""Project Hands launch context: clean worktree, attached docs, resume registry."""

from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime
from pathlib import Path
import re
import sqlite3
import subprocess


HANDS_WORKTREE_ROOT = ".codex/hands_worktrees"
HANDS_DOCS_DIR = ".hands/project_docs"


@dataclass(frozen=True)
class HandsDocEntry:
    doc_id: int
    title: str
    content: str
    level: int
    slug: str
    path: str
    status: str
    parent_id: int = 0


def _slugify_filename(text: str, *, fallback: str) -> str:
    slug = re.sub(r"[^A-Za-z0-9._-]+", "-", text.strip()).strip("-.")
    return slug or fallback


def create_hands_worktree(project_cwd: Path) -> tuple[Path, str]:
    """Create a clean git worktree for a new Project Hands client.

    Mirrors the Netrunner wave mechanics (dedicated worktree + branch from the
    current HEAD) but lives under the Hands-specific root so wave tooling never
    confuses these with wave worker trees.
    """
    root = project_cwd / HANDS_WORKTREE_ROOT
    stamp = datetime.now().strftime("%Y%m%d-%H%M%S")
    worktree_path = root / stamp
    branch_name = f"hands/{stamp}"
    suffix = 1
    while worktree_path.exists():
        suffix += 1
        worktree_path = root / f"{stamp}-{suffix}"
        branch_name = f"hands/{stamp}-{suffix}"
    root.mkdir(parents=True, exist_ok=True)
    result = subprocess.run(
        ["git", "worktree", "add", str(worktree_path), "-b", branch_name, "HEAD"],
        cwd=str(project_cwd),
        capture_output=True,
        text=True,
    )
    if result.returncode != 0:
        detail = (result.stderr or result.stdout or "").strip()
        raise RuntimeError(f"Failed to create Project Hands worktree: {detail}")
    return worktree_path, branch_name


def load_project_doc_tree(conn: sqlite3.Connection, project_id: int) -> list[HandsDocEntry]:
    rows = conn.execute(
        """
        SELECT
            (
                SELECT COUNT(*)
                FROM project_doc d2
                WHERE d2.project_id = d.project_id AND d2.id <= d.id
            ) AS local_doc_id,
            d.title,
            d.content,
            COALESCE(d.level, 0),
            COALESCE(d.slug, ''),
            COALESCE(d.path, ''),
            COALESCE(d.status, 'current'),
            CASE
                WHEN d.parent_doc_id IS NULL THEN 0
                ELSE (
                    SELECT COUNT(*)
                    FROM project_doc p2
                    WHERE p2.project_id = d.project_id AND p2.id <= d.parent_doc_id
                )
            END AS local_parent_id
        FROM project_doc d
        WHERE d.project_id = ?
        ORDER BY d.id
        """,
        (project_id,),
    ).fetchall()
    return [
        HandsDocEntry(
            doc_id=int(row[0]),
            title=str(row[1]),
            content=str(row[2]),
            level=int(row[3]),
            slug=str(row[4]),
            path=str(row[5]),
            status=str(row[6]),
            parent_id=int(row[7]),
        )
        for row in rows
    ]


def materialize_hands_docs(worktree_path: Path, docs: list[HandsDocEntry]) -> list[str]:
    """Write the selected docs into the worktree; returns relative file paths."""
    docs_root = worktree_path / HANDS_DOCS_DIR
    docs_root.mkdir(parents=True, exist_ok=True)
    written: list[str] = []
    index_lines = ["# Attached project documentation", ""]
    for entry in docs:
        filename = f"{entry.doc_id:02d}-{_slugify_filename(entry.slug or entry.title, fallback='doc')}.md"
        target = docs_root / filename
        target.write_text(
            f"# {entry.title}\n\n"
            f"- doc_id: {entry.doc_id}\n"
            f"- path: {entry.path or '-'}\n"
            f"- status: {entry.status}\n\n"
            f"{entry.content}\n",
            encoding="utf-8",
        )
        relative = f"{HANDS_DOCS_DIR}/{filename}"
        written.append(relative)
        indent = "  " * entry.level
        index_lines.append(f"{indent}- [{entry.title}]({filename}) — {entry.status}")
    index_lines.append("")
    (docs_root / "_index.md").write_text("\n".join(index_lines), encoding="utf-8")
    return written
