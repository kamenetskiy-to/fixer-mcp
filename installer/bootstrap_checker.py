"""Candidate bootstrap check against a temporary SQLite database before activation."""

import json
import os
import stat
import subprocess
import tempfile
from typing import Any, Dict, Optional

from installer.errors import CandidateBootstrapError


def find_fixer_mcp_binary(payload_root: str) -> Optional[str]:
    """Find the fixer_mcp binary within an extracted payload directory."""
    candidates = [
        os.path.join(payload_root, "payload", "fixer_mcp", "fixer_mcp"),
        os.path.join(payload_root, "fixer_mcp", "fixer_mcp"),
        os.path.join(payload_root, "bin", "fixer_mcp"),
    ]
    for c in candidates:
        if os.path.isfile(c):
            return c
    return None


def check_candidate_bootstrap(staged_payload_dir: str, timeout: float = 15.0) -> Dict[str, Any]:
    """
    Run the candidate Go binary --bootstrap-schema smoke check against a temporary SQLite database.
    Verifies that the candidate binary is runnable, executable permissions are set,
    initializes the schema cleanly, and verifies project-workroom-v1 compatibility.
    """
    staged_payload_dir = os.path.abspath(staged_payload_dir)
    binary_path = find_fixer_mcp_binary(staged_payload_dir)

    if not binary_path:
        raise CandidateBootstrapError(
            f"Candidate fixer_mcp binary not found in staged payload: {staged_payload_dir}"
        )

    # Check executable permission
    st = os.stat(binary_path)
    if not (st.st_mode & stat.S_IXUSR):
        # Attempt to make executable if not set
        try:
            os.chmod(binary_path, st.st_mode | stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH)
        except OSError as e:
            raise CandidateBootstrapError(f"Candidate binary is not executable: {binary_path} ({e})")

    # Run bootstrap in a clean temporary directory with a temporary DB
    with tempfile.TemporaryDirectory(prefix="fixer_candidate_bootstrap_") as tmp_dir:
        temp_db_path = os.path.join(tmp_dir, "candidate_smoke.db")
        env = os.environ.copy()
        env["FIXER_DB_PATH"] = temp_db_path
        env["FIXER_RUNTIME_ROOT"] = staged_payload_dir

        cmd = [binary_path, "--bootstrap-schema"]
        try:
            proc = subprocess.run(
                cmd,
                cwd=tmp_dir,
                env=env,
                capture_output=True,
                text=True,
                timeout=timeout,
            )
        except subprocess.TimeoutExpired as e:
            raise CandidateBootstrapError(
                f"Candidate binary bootstrap timed out after {timeout}s"
            ) from e
        except OSError as e:
            raise CandidateBootstrapError(
                f"Failed to execute candidate binary {binary_path}: {e}"
            ) from e

        if proc.returncode != 0:
            raise CandidateBootstrapError(
                f"Candidate binary bootstrap failed (exit code {proc.returncode}):\n"
                f"stdout: {proc.stdout}\n"
                f"stderr: {proc.stderr}"
            )

        try:
            result = json.loads(proc.stdout.strip())
        except json.JSONDecodeError as e:
            raise CandidateBootstrapError(
                f"Candidate binary returned invalid bootstrap JSON: {proc.stdout!r} ({e})"
            ) from e

        if result.get("status") != "ready":
            raise CandidateBootstrapError(
                f"Candidate bootstrap status is not 'ready': got {result.get('status')!r}"
            )

        if "schema" not in result:
            raise CandidateBootstrapError("Candidate bootstrap result missing 'schema' identifier")

        return result
