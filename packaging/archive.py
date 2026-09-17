"""Archive creation, safe extraction, and integrity verification for Fixer MCP releases."""

import gzip
import hashlib
import io
import os
import shutil
import stat
import tarfile
import tempfile
from typing import Any, Dict, List, Optional, Set

from packaging.allowlist import (
    DangerousFileError,
    assert_not_dangerous,
    is_dangerous_name_or_path,
)
from packaging.descriptor import validate_release_descriptor


class ArchiveSecurityError(Exception):
    """Base exception for archive security violations."""
    pass


class ArchiveTraversalError(ArchiveSecurityError):
    """Raised when an archive member attempts directory traversal."""
    pass


class ArchiveSymlinkEscapeError(ArchiveSecurityError):
    """Raised when an archive symlink attempts to escape the extraction root."""
    pass


def calculate_sha256(filepath: str) -> str:
    """Calculate the SHA-256 hex digest of a file."""
    hasher = hashlib.sha256()
    with open(filepath, "rb") as f:
        for chunk in iter(lambda: f.read(65536), b""):
            hasher.update(chunk)
    return hasher.hexdigest()


def create_deterministic_tar_gz(
    source_root: str,
    output_tar_gz_path: str,
    root_prefix: str = "payload",
) -> str:
    """
    Create a byte-deterministic tar.gz archive from source_root directory.
    Files are stored under root_prefix/ in the archive.
    Returns SHA-256 hex string of the created archive.
    """
    source_root = os.path.abspath(source_root)
    output_tar_gz_path = os.path.abspath(output_tar_gz_path)
    os.makedirs(os.path.dirname(output_tar_gz_path), exist_ok=True)

    # Collect all items to add
    entries: List[Tuple[str, str]] = []
    for root, dirs, files in os.walk(source_root):
        dirs.sort()
        files.sort()
        rel_dir = os.path.relpath(root, source_root)
        if rel_dir != ".":
            entries.append((root, os.path.join(root_prefix, rel_dir).replace("\\", "/")))
        for file in files:
            abs_file = os.path.join(root, file)
            rel_file = os.path.relpath(abs_file, source_root)
            arc_name = os.path.join(root_prefix, rel_file).replace("\\", "/")
            assert_not_dangerous(arc_name)
            entries.append((abs_file, arc_name))

    entries.sort(key=lambda item: item[1])

    # First write uncompressed tar in memory or deterministic stream
    tar_buffer = io.BytesIO()
    with tarfile.open(fileobj=tar_buffer, mode="w") as tar:
        for abs_path, arc_name in entries:
            tarinfo = tar.gettarinfo(abs_path, arcname=arc_name)
            # Reset non-deterministic attributes
            tarinfo.uid = 0
            tarinfo.gid = 0
            tarinfo.uname = ""
            tarinfo.gname = ""
            tarinfo.mtime = 0

            # Normalize permissions
            if tarinfo.isdir():
                tarinfo.mode = 0o755
            elif tarinfo.isreg():
                # Preserve executable bit if set in source
                if os.stat(abs_path).st_mode & 0o111:
                    tarinfo.mode = 0o755
                else:
                    tarinfo.mode = 0o644
            elif tarinfo.issym():
                # Check symlink destination does not escape
                if is_dangerous_name_or_path(tarinfo.linkname):
                    raise ArchiveSecurityError(f"Dangerous symlink target: {tarinfo.linkname}")

            if tarinfo.isreg():
                with open(abs_path, "rb") as f:
                    tar.addfile(tarinfo, f)
            else:
                tar.addfile(tarinfo)

    tar_bytes = tar_buffer.getvalue()

    # Deterministic gzip compression (mtime=0)
    with open(output_tar_gz_path, "wb") as f_out:
        with gzip.GzipFile(filename="", mode="wb", fileobj=f_out, mtime=0.0) as gz_out:
            gz_out.write(tar_bytes)

    return calculate_sha256(output_tar_gz_path)


def validate_archive_members(tar: tarfile.TarFile, dest_dir: str) -> None:
    """
    Inspect all archive members for directory traversal, symlink escapes,
    dangerous filenames, and safe paths.
    """
    dest_dir_abs = os.path.abspath(dest_dir)

    for member in tar.getmembers():
        name = member.name.replace("\\", "/")

        # Check for dangerous patterns
        if is_dangerous_name_or_path(name):
            raise DangerousFileError(f"Archive contains prohibited file: {name}")

        # Check for path traversal in member name
        if name.startswith("/") or name.startswith("\\"):
            raise ArchiveTraversalError(f"Absolute path in archive: {name}")

        parts = name.strip("/").split("/")
        if ".." in parts:
            raise ArchiveTraversalError(f"Path traversal ('..') detected in member: {name}")

        # Destination path check
        dest_path = os.path.normpath(os.path.join(dest_dir_abs, name))
        if not (dest_path == dest_dir_abs or dest_path.startswith(dest_dir_abs + os.sep)):
            raise ArchiveTraversalError(f"Member resolves outside destination directory: {name}")

        # Symlink / Hardlink escape check
        if member.issym() or member.islnk():
            target = member.linkname.replace("\\", "/")
            if target.startswith("/") or target.startswith("\\"):
                raise ArchiveSymlinkEscapeError(f"Symlink with absolute target rejected: {name} -> {target}")

            link_parts = target.strip("/").split("/")
            if ".." in link_parts:
                # Check if resolved link target escapes dest_dir_abs
                member_dir = os.path.dirname(dest_path)
                resolved_target = os.path.normpath(os.path.join(member_dir, target))
                if not (resolved_target == dest_dir_abs or resolved_target.startswith(dest_dir_abs + os.sep)):
                    raise ArchiveSymlinkEscapeError(
                        f"Symlink target escapes extraction root: {name} -> {target}"
                    )


def safe_extract(archive_path: str, dest_dir: str) -> List[str]:
    """
    Safely extract a release archive into dest_dir, rejecting traversal,
    symlink escapes, and dangerous files.
    Returns list of extracted relative paths.
    """
    archive_path = os.path.abspath(archive_path)
    dest_dir_abs = os.path.abspath(dest_dir)
    os.makedirs(dest_dir_abs, exist_ok=True)

    extracted_members: List[str] = []

    with tarfile.open(archive_path, "r:gz") as tar:
        # Validate all members before extracting anything
        validate_archive_members(tar, dest_dir_abs)

        for member in tar.getmembers():
            tar.extract(member, path=dest_dir_abs)
            extracted_members.append(member.name)

    return extracted_members


def verify_payload_archive(
    archive_path: str,
    descriptor: Dict[str, Any],
    verify_extraction: bool = True,
    temp_dir: Optional[str] = None,
) -> Dict[str, Any]:
    """
    Verify release payload archive matches descriptor, contains expected
    sibling layout, satisfies all security checks, and is executable.
    """
    archive_path = os.path.abspath(archive_path)
    if not os.path.isfile(archive_path):
        raise FileNotFoundError(f"Archive not found: {archive_path}")

    # Validate descriptor schema
    validate_release_descriptor(descriptor)

    # Validate SHA-256 match
    actual_sha256 = calculate_sha256(archive_path)
    expected_sha256 = descriptor["sha256"]
    if actual_sha256 != expected_sha256:
        raise ValueError(
            f"SHA-256 mismatch for {archive_path}:\n"
            f"  Expected: {expected_sha256}\n"
            f"  Actual:   {actual_sha256}"
        )

    # Inspect archive contents
    with tarfile.open(archive_path, "r:gz") as tar:
        validate_archive_members(tar, dest_dir=os.getcwd())
        member_names = {m.name.replace("\\", "/") for m in tar.getmembers()}

    # Check required payload layout
    required_files = [
        "payload/fixer_mcp/fixer_mcp",
        "payload/client_wires/fixer_wire.py",
        "payload/installer/cli.py",
        "payload/bin/fixer",
    ]
    for req in required_files:
        if req not in member_names:
            raise ValueError(f"Required release payload file missing from archive: {req}")

    # Check that skills directory exists with skills
    skill_members = [m for m in member_names if m.startswith("payload/.agents/skills/")]
    if not skill_members:
        raise ValueError("No agent skills found in payload/.agents/skills/")

    # If verification includes extraction
    if verify_extraction:
        cleanup_temp = temp_dir is None
        extract_dir = temp_dir or tempfile.mkdtemp(prefix="fixer_verify_")
        try:
            safe_extract(archive_path, extract_dir)
            # Verify executable permissions on fixer_mcp
            binary_path = os.path.join(extract_dir, "payload", "fixer_mcp", "fixer_mcp")
            if not os.path.isfile(binary_path):
                raise FileNotFoundError(f"Extracted binary not found at: {binary_path}")
            st = os.stat(binary_path)
            if not (st.st_mode & stat.S_IXUSR):
                raise PermissionError(f"Extracted binary is not executable: {binary_path}")

            bin_fixer_path = os.path.join(extract_dir, "payload", "bin", "fixer")
            if os.path.isfile(bin_fixer_path):
                st_b = os.stat(bin_fixer_path)
                if not (st_b.st_mode & stat.S_IXUSR):
                    raise PermissionError(f"Extracted bin/fixer is not executable: {bin_fixer_path}")
        finally:
            if cleanup_temp and os.path.exists(extract_dir):
                shutil.rmtree(extract_dir, ignore_errors=True)

    return {
        "status": "verified",
        "archive_path": archive_path,
        "sha256": actual_sha256,
        "member_count": len(member_names),
        "descriptor_version": descriptor["version"],
        "platform": descriptor["platform"],
    }
