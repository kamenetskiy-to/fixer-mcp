from __future__ import annotations

import os
from pathlib import Path
from typing import List

SUPPORTED_EXTENSIONS = {".md", ".txt"}
SKIP_DIR_NAMES = {"node_modules", ".git", ".hg", ".svn", ".venv", "venv", "__pycache__"}


def find_textual_files(root: Path, max_depth: int | None = None) -> List[Path]:
    results: List[Path] = []
    root = root.resolve()
    for dirpath, dirnames, filenames in os.walk(root):
        current = Path(dirpath)
        rel_parts = current.relative_to(root).parts if current != root else ()
        depth = len(rel_parts)
        if current.name in SKIP_DIR_NAMES:
            dirnames[:] = []
            continue
        dirnames[:] = [name for name in dirnames if name not in SKIP_DIR_NAMES]
        if max_depth is not None and depth > max_depth:
            dirnames[:] = []
            continue
        for name in filenames:
            path = current / name
            if path.suffix.lower() in SUPPORTED_EXTENSIONS:
                results.append(path)
    results.sort()
    return results
