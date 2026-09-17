#!/usr/bin/env python3
"""CLI tool to verify an assembled Fixer MCP release package."""

import argparse
import json
import os
import sys

# Ensure repository root is on sys.path
REPO_ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
if REPO_ROOT not in sys.path:
    sys.path.insert(0, REPO_ROOT)

from packaging.archive import safe_extract, verify_payload_archive
from packaging.descriptor import validate_release_descriptor


def main():
    parser = argparse.ArgumentParser(description="Verify Fixer MCP release payload archive and descriptor.")
    parser.add_argument(
        "--archive",
        required=True,
        help="Path to release .tar.gz archive",
    )
    parser.add_argument(
        "--descriptor",
        default=None,
        help="Path to format=1 descriptor JSON file (default: sibling release.json)",
    )
    parser.add_argument(
        "--extract-dir",
        default=None,
        help="Optional directory to extract payload into for verification",
    )

    args = parser.parse_args()

    archive_path = os.path.abspath(args.archive)
    if not os.path.isfile(archive_path):
        sys.stderr.write(f"Archive file not found: {archive_path}\n")
        sys.exit(1)

    descriptor_path = args.descriptor
    if descriptor_path is None:
        sibling_desc = os.path.join(os.path.dirname(archive_path), "release.json")
        if os.path.isfile(sibling_desc):
            descriptor_path = sibling_desc
        else:
            sys.stderr.write("No descriptor provided and sibling release.json not found\n")
            sys.exit(1)

    with open(descriptor_path, "r", encoding="utf-8") as f:
        descriptor = json.load(f)

    try:
        result = verify_payload_archive(
            archive_path=archive_path,
            descriptor=descriptor,
            verify_extraction=True,
            temp_dir=args.extract_dir,
        )
        print(json.dumps(result, indent=2))
    except Exception as e:
        sys.stderr.write(f"Verification failed: {e}\n")
        sys.exit(1)


if __name__ == "__main__":
    main()
