"""Descriptor and payload fetching, verification, and checksum validation."""

import hashlib
import json
import os
import shutil
import sys
import tarfile
import urllib.error
import urllib.parse
import urllib.request
from typing import Any, Dict, Optional

# Ensure packaging package is discoverable
_cur_dir = os.path.dirname(os.path.abspath(__file__))
_repo_root = os.path.abspath(os.path.join(_cur_dir, ".."))
if _repo_root not in sys.path:
    sys.path.insert(0, _repo_root)

from packaging.archive import calculate_sha256, validate_archive_members
from packaging.descriptor import DescriptorValidationError, validate_release_descriptor
from installer.errors import DescriptorFetchError, PayloadFetchError, VerificationError


def fetch_descriptor(source: str, timeout: float = 2.0) -> Dict[str, Any]:
    """
    Fetch and validate a format=1 release descriptor from local file or HTTP(S) URL.
    Enforces the specified timeout for network requests.
    """
    source = source.strip()
    parsed = urllib.parse.urlparse(source)

    try:
        if parsed.scheme in ("http", "https"):
            req = urllib.request.Request(
                source,
                headers={"User-Agent": "FixerMCP-Installer/1.0", "Accept": "application/json"},
            )
            with urllib.request.urlopen(req, timeout=timeout) as resp:
                content = resp.read().decode("utf-8")
        elif parsed.scheme == "file":
            local_path = urllib.parse.unquote(parsed.path)
            with open(local_path, "r", encoding="utf-8") as f:
                content = f.read()
        else:
            # Assume local filesystem path
            local_path = os.path.abspath(os.path.expanduser(source))
            if not os.path.isfile(local_path):
                raise DescriptorFetchError(f"Local descriptor file not found: {local_path}")
            with open(local_path, "r", encoding="utf-8") as f:
                content = f.read()

        data = json.loads(content)
        validate_release_descriptor(data)
        return data

    except (urllib.error.URLError, TimeoutError, OSError) as e:
        raise DescriptorFetchError(f"Failed to fetch release descriptor from {source}: {e}") from e
    except json.JSONDecodeError as e:
        raise DescriptorFetchError(f"Invalid JSON in release descriptor from {source}: {e}") from e
    except DescriptorValidationError as e:
        raise VerificationError(f"Descriptor validation failed: {e}") from e


def fetch_payload(
    payload_source: str,
    dest_path: str,
    expected_sha256: str,
    base_descriptor_source: Optional[str] = None,
    timeout: float = 10.0,
) -> str:
    """
    Download or copy release payload tar.gz archive to dest_path.
    Verifies SHA-256 digest during download and cleans up on mismatch or error.
    """
    dest_path = os.path.abspath(dest_path)
    os.makedirs(os.path.dirname(dest_path), exist_ok=True)
    partial_path = f"{dest_path}.partial.{os.getpid()}"

    # Resolve relative payload source against base_descriptor_source if needed
    source = payload_source.strip()
    parsed = urllib.parse.urlparse(source)

    if not parsed.scheme:
        if base_descriptor_source:
            base_parsed = urllib.parse.urlparse(base_descriptor_source)
            if base_parsed.scheme in ("http", "https"):
                source = urllib.parse.urljoin(base_descriptor_source, source)
                parsed = urllib.parse.urlparse(source)
            elif base_parsed.scheme == "file":
                base_dir = os.path.dirname(urllib.parse.unquote(base_parsed.path))
                source = os.path.join(base_dir, source)
            else:
                base_dir = os.path.dirname(os.path.abspath(base_descriptor_source))
                source = os.path.join(base_dir, source)
        else:
            source = os.path.abspath(os.path.expanduser(source))

    expected_sha256 = expected_sha256.strip().lower()
    hasher = hashlib.sha256()

    try:
        if parsed.scheme in ("http", "https"):
            req = urllib.request.Request(
                source,
                headers={"User-Agent": "FixerMCP-Installer/1.0"},
            )
            with urllib.request.urlopen(req, timeout=timeout) as resp, open(partial_path, "wb") as f_out:
                while True:
                    chunk = resp.read(65536)
                    if not chunk:
                        break
                    hasher.update(chunk)
                    f_out.write(chunk)
        else:
            # Local file or file:// scheme
            if parsed.scheme == "file":
                src_file = urllib.parse.unquote(parsed.path)
            else:
                src_file = os.path.abspath(os.path.expanduser(source))

            if not os.path.isfile(src_file):
                raise PayloadFetchError(f"Payload archive file not found: {src_file}")

            with open(src_file, "rb") as f_in, open(partial_path, "wb") as f_out:
                while True:
                    chunk = f_in.read(65536)
                    if not chunk:
                        break
                    hasher.update(chunk)
                    f_out.write(chunk)

        actual_sha256 = hasher.hexdigest().lower()
        if actual_sha256 != expected_sha256:
            raise VerificationError(
                f"Payload SHA-256 mismatch for {source}:\n"
                f"  Expected: {expected_sha256}\n"
                f"  Actual:   {actual_sha256}"
            )

        # Inspect archive members before accepting
        with tarfile.open(partial_path, "r:gz") as tar:
            validate_archive_members(tar, dest_dir=os.path.dirname(dest_path))

        # Atomic rename into final location
        os.replace(partial_path, dest_path)
        return dest_path

    except Exception:
        # Clean up partial download on failure
        if os.path.exists(partial_path):
            try:
                os.unlink(partial_path)
            except OSError:
                pass
        raise
