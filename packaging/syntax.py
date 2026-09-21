"""Python syntax verification gate for Fixer MCP release packaging.

Enforces that every Python file in a release payload is compatible with the
minimum supported Python floor (Python 3.9) before packaging.
"""

import ast
import os
from typing import List, Optional, Tuple


class PythonSyntaxFloorError(Exception):
    """Raised when a Python file contains syntax unsupported by the minimum Python floor."""
    pass


DEFAULT_MIN_PYTHON_VERSION: Tuple[int, int] = (3, 9)


class FStringBackslashChecker(ast.NodeVisitor):
    """Detect backslashes inside f-string expressions (SyntaxError before Python 3.12)."""

    def __init__(self, rel_path: str):
        self.rel_path = rel_path
        self.errors: List[str] = []

    def visit_FormattedValue(self, node: ast.FormattedValue) -> None:
        try:
            expr_str = ast.unparse(node.value)
            if "\\" in expr_str:
                self.errors.append(
                    f"{self.rel_path}:{node.lineno}: backslash inside f-string expression "
                    f"(SyntaxError before Python 3.12): {expr_str}"
                )
        except Exception:
            pass
        self.generic_visit(node)


def verify_file_python_syntax(
    file_path: str,
    rel_name: Optional[str] = None,
    min_version: Tuple[int, int] = DEFAULT_MIN_PYTHON_VERSION,
) -> None:
    """
    Verify a single Python file for syntax compatibility with min_version.
    Uses ast.parse with feature_version=min_version, and verifies no 3.12+
    f-string backslash expressions exist when min_version < (3, 12).
    Raises PythonSyntaxFloorError if parsing or compatibility checks fail.
    """
    rel = rel_name or os.path.basename(file_path)
    try:
        with open(file_path, "r", encoding="utf-8") as f:
            source = f.read()
    except OSError as e:
        raise PythonSyntaxFloorError(f"Failed to read file {rel}: {e}") from e

    try:
        tree = ast.parse(source, filename=rel, feature_version=min_version)
    except SyntaxError as e:
        raise PythonSyntaxFloorError(
            f"SyntaxError in {rel}:{e.lineno}:{e.offset or 0} "
            f"for minimum Python floor {min_version[0]}.{min_version[1]}: {e.msg}"
        ) from e

    if min_version < (3, 12):
        checker = FStringBackslashChecker(rel)
        checker.visit(tree)
        if checker.errors:
            raise PythonSyntaxFloorError("\n".join(checker.errors))


def verify_python_syntax_floor(
    root_dir: str,
    min_version: Tuple[int, int] = DEFAULT_MIN_PYTHON_VERSION,
) -> List[str]:
    """
    Verify all .py files under root_dir can parse with the minimum supported Python version.
    Returns sorted list of verified relative file paths.
    Raises PythonSyntaxFloorError if any file fails to parse.
    """
    root_dir = os.path.abspath(root_dir)
    verified_files: List[str] = []

    for root, dirs, files in os.walk(root_dir):
        # Prune hidden or cache directories
        dirs[:] = [d for d in dirs if not d.startswith(".") and d != "__pycache__"]
        for f in files:
            if not f.endswith(".py"):
                continue
            abs_path = os.path.join(root, f)
            rel_path = os.path.relpath(abs_path, root_dir).replace("\\", "/")
            verify_file_python_syntax(abs_path, rel_name=rel_path, min_version=min_version)
            verified_files.append(rel_path)

    verified_files.sort()
    return verified_files
