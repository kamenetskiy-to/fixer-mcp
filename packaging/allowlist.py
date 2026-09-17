"""Allowlist and dangerous file detection for Fixer MCP release packaging."""

import fnmatch
import os
import re
from typing import Callable, Iterable, List, Optional, Tuple


class DangerousFileError(Exception):
    """Raised when a dangerous, secret, database, log, or worktree file is encountered."""
    pass


# Explicit dangerous patterns to reject from any release payload
DANGEROUS_PATTERNS = [
    # SQLite / Database state, WAL, SHM
    "*.db",
    "*.db-*",
    "*.db-wal",
    "*.db-shm",
    "*.sqlite",
    "*.sqlite3",
    "fixer.db*",
    "*.snapshot*",
    # Credentials, secrets, environment files
    ".env",
    ".env.*",
    "*.env",
    "*.key",
    "*.pem",
    "*.crt",
    "*.cert",
    "*.p12",
    "*.pfx",
    "*secret*",
    "*token*",
    "*credential*",
    # Logs
    "*.log",
    "*.log.*",
    "fixer_mcp.log*",
    # Provider materialization, worktrees, VCS, local private dirs
    ".git",
    ".git*",
    ".codex*",
    ".factory*",
    ".commandcode*",
    "*worktree*",
    "recovered_tools*",
    "__pycache__*",
    "*.pyc",
    "*.pyo",
    "*.pyd",
    ".DS_Store",
    # Local developer overrides and backups
    "*.local",
    "*.override*",
    "private_*",
    "*.backup",
    "*.orig",
]


def is_dangerous_name_or_path(path: str) -> bool:
    """Check if a filename or path matches any dangerous pattern."""
    normalized = path.replace("\\", "/")
    parts = normalized.strip("/").split("/")
    basename = os.path.basename(normalized)

    for pattern in DANGEROUS_PATTERNS:
        # Check basename
        if fnmatch.fnmatch(basename.lower(), pattern.lower()):
            return True
        # Check any directory segment
        for part in parts:
            if fnmatch.fnmatch(part.lower(), pattern.lower()):
                return True
        # Check entire relative path
        if fnmatch.fnmatch(normalized.lower(), pattern.lower()):
            return True

    # Check for personal home paths in relative archive paths
    if re.search(r"(/Users/[^/\s]+|/home/[^/\s]+|/root/[^/\s]+)", normalized):
        return True

    return False


def assert_not_dangerous(path: str) -> None:
    """Raise DangerousFileError if the path matches a dangerous pattern."""
    if is_dangerous_name_or_path(path):
        raise DangerousFileError(f"Rejected dangerous file/path in release packaging: {path}")


# Allowed files and extensions for fixer_mcp component
FIXER_MCP_ALLOWED_NAMES = {
    "fixer_mcp",
    "mcp_config.json",
    "README.md",
    "LICENSE",
}

# Allowed root files and directory trees for client_wires
CLIENT_WIRES_ALLOWED_EXTENSIONS = {
    ".py",
    ".json",
    ".cjs",
    ".js",
    ".html",
    ".exp",
    ".md",
    ".txt",
}

CLIENT_WIRES_ALLOWED_DIRS = {
    "backends",
    "backends/data",
    "codex_compat",
    "edge_mcp_extension",
}


def is_allowed_fixer_mcp_file(rel_path: str) -> bool:
    """Check if a file is allowed inside payload/fixer_mcp."""
    normalized = rel_path.replace("\\", "/").strip("/")
    if "/" in normalized:
        return False
    return normalized in FIXER_MCP_ALLOWED_NAMES


def is_allowed_client_wire_file(rel_path: str) -> bool:
    """Check if a file from client_wires source is permitted in the release payload."""
    normalized = rel_path.replace("\\", "/").strip("/")
    if is_dangerous_name_or_path(normalized):
        return False

    parts = normalized.split("/")
    filename = parts[-1]
    ext = os.path.splitext(filename)[1].lower()

    # Reject tests directory from launch payload
    if parts[0] == "tests":
        return False

    if ext not in CLIENT_WIRES_ALLOWED_EXTENSIONS:
        return False

    if len(parts) == 1:
        # Root client_wire file
        return True

    dir_part = "/".join(parts[:-1])
    for allowed_dir in CLIENT_WIRES_ALLOWED_DIRS:
        if dir_part == allowed_dir or dir_part.startswith(allowed_dir + "/"):
            return True

    return False


def is_allowed_skill_file(rel_path: str) -> bool:
    """Check if a skill file is allowed in payload/.agents/skills."""
    normalized = rel_path.replace("\\", "/").strip("/")
    if is_dangerous_name_or_path(normalized):
        return False

    parts = normalized.split("/")
    if len(parts) < 2:
        return False

    # First segment must be skill name (non-hidden)
    if parts[0].startswith("."):
        return False

    ext = os.path.splitext(parts[-1])[1].lower()
    allowed_skill_exts = {".md", ".json", ".yaml", ".yml", ".sh", ".py", ".txt", ".prompt"}
    return ext in allowed_skill_exts or parts[-1] in {"SKILL.md", "instructions.md"}


def is_allowed_installer_file(rel_path: str) -> bool:
    """Check if a file from installer/ is permitted in the release payload."""
    normalized = rel_path.replace("\\", "/").strip("/")
    if is_dangerous_name_or_path(normalized):
        return False
    parts = normalized.split("/")
    if any(p.startswith(".") for p in parts):
        return False
    if parts[0] == "__pycache__":
        return False
    ext = os.path.splitext(parts[-1])[1].lower()
    return ext == ".py"


def is_allowed_packaging_file(rel_path: str) -> bool:
    """Check if a file from packaging/ is permitted in the release payload."""
    normalized = rel_path.replace("\\", "/").strip("/")
    if is_dangerous_name_or_path(normalized):
        return False
    parts = normalized.split("/")
    if any(p.startswith(".") for p in parts):
        return False
    if parts[0] == "__pycache__":
        return False
    ext = os.path.splitext(parts[-1])[1].lower()
    return ext == ".py"


def is_allowed_bin_file(rel_path: str) -> bool:
    """Check if a file from bin/ is permitted in the release payload."""
    normalized = rel_path.replace("\\", "/").strip("/")
    if is_dangerous_name_or_path(normalized):
        return False
    return normalized == "fixer"



def collect_allowed_files(
    src_dir: str,
    allow_predicate: Callable[[str], bool],
) -> List[Tuple[str, str]]:
    """
    Recursively collect allowed files from src_dir.
    Returns a sorted list of (absolute_src_path, relative_path).
    Raises DangerousFileError if any dangerous file is attempted.
    """
    collected: List[Tuple[str, str]] = []
    if not os.path.exists(src_dir):
        return collected

    for root, dirs, files in os.walk(src_dir):
        # Prune dangerous directories in-place
        dirs[:] = [d for d in dirs if not is_dangerous_name_or_path(d)]
        for f in files:
            abs_path = os.path.join(root, f)
            rel_path = os.path.relpath(abs_path, src_dir).replace("\\", "/")
            if is_dangerous_name_or_path(rel_path):
                # If it's a dangerous file matching allowlist, that's a security violation
                if allow_predicate(rel_path):
                    raise DangerousFileError(f"Security error: dangerous file allowed: {rel_path}")
                continue
            if allow_predicate(rel_path):
                assert_not_dangerous(rel_path)
                collected.append((abs_path, rel_path))

    collected.sort(key=lambda item: item[1])
    return collected
