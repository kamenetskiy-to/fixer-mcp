"""Descriptor schema (format=1) and validation for Fixer MCP release packages."""

import datetime
import json
import re
from typing import Any, Dict, Optional


class DescriptorValidationError(Exception):
    """Raised when release descriptor validation fails."""
    pass


REQUIRED_DESCRIPTOR_FIELDS = [
    "format",
    "version",
    "source_revision",
    "platform",
    "payload_url",
    "payload_path",
    "sha256",
    "payload_sha256",
    "schema_compatibility_class",
    "changelog",
]


def create_release_descriptor(
    version: str,
    source_revision: str,
    platform: str,
    payload_filename: str,
    sha256_hex: str,
    schema_compatibility_class: str = "project-workroom-v1",
    changelog: Optional[str] = None,
    changelog_url: Optional[str] = None,
    min_os: str = "macOS 12.0",
    created_at: Optional[str] = None,
) -> Dict[str, Any]:
    """Create a format=1 machine-readable release descriptor."""
    if created_at is None:
        created_at = datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")

    if changelog is None:
        changelog = f"Fixer MCP release {version} for {platform}"

    # Normalize payload path to filename only (prevent personal paths)
    clean_payload_name = payload_filename.replace("\\", "/").split("/")[-1]

    descriptor: Dict[str, Any] = {
        "format": 1,
        "version": version.strip(),
        "source_revision": source_revision.strip(),
        "platform": platform.strip(),
        "payload_url": clean_payload_name,
        "payload_path": clean_payload_name,
        "sha256": sha256_hex.strip().lower(),
        "payload_sha256": sha256_hex.strip().lower(),
        "schema_compatibility_class": schema_compatibility_class.strip(),
        "changelog": changelog.strip(),
        "changelog_url": changelog_url or "",
        "min_os": min_os,
        "created_at": created_at,
    }

    validate_release_descriptor(descriptor)
    return descriptor


def validate_release_descriptor(descriptor: Dict[str, Any]) -> None:
    """
    Validate a format=1 release descriptor.
    Raises DescriptorValidationError if invalid.
    """
    if not isinstance(descriptor, dict):
        raise DescriptorValidationError("Descriptor must be a JSON object")

    # Check format=1
    fmt = descriptor.get("format")
    if fmt != 1:
        raise DescriptorValidationError(f"Expected descriptor format=1, got {fmt!r}")

    # Check required fields
    for field in REQUIRED_DESCRIPTOR_FIELDS:
        if field not in descriptor:
            raise DescriptorValidationError(f"Missing required descriptor field: {field!r}")
        val = descriptor[field]
        if field != "format" and (not isinstance(val, str) or not val.strip()):
            raise DescriptorValidationError(f"Field {field!r} must be a non-empty string, got {val!r}")

    # Validate sha256
    sha256 = descriptor.get("sha256", "")
    if not re.match(r"^[a-f0-9]{64}$", sha256):
        raise DescriptorValidationError(f"Invalid SHA-256 format: {sha256!r}")
    if descriptor.get("payload_sha256") != sha256:
        raise DescriptorValidationError("Field 'payload_sha256' must match 'sha256'")

    # Validate platform
    platform = descriptor.get("platform", "")
    if not platform.startswith("darwin_"):
        raise DescriptorValidationError(f"Target platform must be macOS (darwin_*), got {platform!r}")

    # Check for personal paths or secrets leaked in any string value
    personal_path_regex = re.compile(r"(/Users/[^/\s]+|/home/[^/\s]+|/private/var|/var/folders)", re.IGNORECASE)
    for k, v in descriptor.items():
        if isinstance(v, str) and personal_path_regex.search(v):
            raise DescriptorValidationError(f"Personal path detected in descriptor field {k!r}: {v!r}")


def serialize_descriptor(descriptor: Dict[str, Any], indent: int = 2) -> str:
    """Serialize descriptor to deterministically formatted JSON string."""
    validate_release_descriptor(descriptor)
    return json.dumps(descriptor, indent=indent, sort_keys=True) + "\n"
