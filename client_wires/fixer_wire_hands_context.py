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
    display_title: str = ""


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
    try:
        language_row = conn.execute(
            "SELECT language_code FROM project_doc_language_policy WHERE project_id = ?",
            (project_id,),
        ).fetchone()
    except sqlite3.OperationalError:
        language_row = None
    language = str(language_row[0]).strip().lower() if language_row else "en"
    localized = language.split("-", 1)[0] != "en"
    localization_join = (
        "LEFT JOIN project_doc_title_localization l "
        "ON l.project_doc_id = d.id AND l.language_code = ?"
        if localized
        else ""
    )
    display_title = "COALESCE(NULLIF(TRIM(l.localized_title), ''), '')" if localized else "d.title"
    params: tuple[object, ...] = (language, project_id) if localized else (project_id,)
    rows = conn.execute(
        f"""
        SELECT
            (
                SELECT COUNT(*)
                FROM project_doc d2
                WHERE d2.project_id = d.project_id AND d2.id <= d.id
            ) AS local_doc_id,
            d.title,
            {display_title},
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
        {localization_join}
        WHERE d.project_id = ?
        ORDER BY d.id
        """,
        params,
    ).fetchall()
    if localized:
        missing = [f"{int(row[0])} {str(row[1])!r}" for row in rows if not str(row[2]).strip()]
        if missing:
            raise RuntimeError(
                f"Project documentation localization is incomplete for language {language!r}; "
                f"missing localized titles: {', '.join(missing[:10])}"
            )
    return [
        HandsDocEntry(
            doc_id=int(row[0]),
            title=str(row[1]),
            display_title=str(row[2]),
            content=str(row[3]),
            level=int(row[4]),
            slug=str(row[5]),
            path=str(row[6]),
            status=str(row[7]),
            parent_id=int(row[8]),
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
