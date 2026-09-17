#!/usr/bin/env python3
"""CLI tool to assemble macOS release packages for Fixer MCP."""

import argparse
import json
import os
import sys

# Ensure repository root is on sys.path
REPO_ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
if REPO_ROOT not in sys.path:
    sys.path.insert(0, REPO_ROOT)

from packaging.assembler import ReleaseAssembler


def main():
    parser = argparse.ArgumentParser(description="Assemble macOS release package for Fixer MCP.")
    parser.add_argument(
        "--repo-root",
        default=REPO_ROOT,
        help="Path to Fixer MCP repository root",
    )
    parser.add_argument(
        "--client-wires",
        default=None,
        help="Path to client_wires launcher directory",
    )
    parser.add_argument(
        "--go-module-dir",
        default=None,
        help="Path to the Go module directory containing main.go (default: <repo-root> or <repo-root>/fixer_mcp)",
    )
    parser.add_argument(
        "--out-dir",
        default="dist",
        help="Directory to place output release archive and descriptor",
    )
    parser.add_argument(
        "--version",
        default="0.1.0",
        help="Release version string (e.g. 0.1.0)",
    )
    parser.add_argument(
        "--platform",
        default=None,
        help="Target platform (default: detected macOS platform darwin_arm64/darwin_amd64)",
    )
    parser.add_argument(
        "--changelog",
        default=None,
        help="Optional changelog description",
    )

    args = parser.parse_args()

    assembler = ReleaseAssembler(
        repo_root=args.repo_root,
        client_wires_src=args.client_wires,
        out_dir=args.out_dir,
        version=args.version,
        platform_id=args.platform,
        changelog=args.changelog,
        go_module_dir=args.go_module_dir,
    )

    try:
        result = assembler.assemble()
        print(json.dumps(result, indent=2))
    except Exception as e:
        sys.stderr.write(f"Error during release assembly: {e}\n")
        sys.exit(1)


if __name__ == "__main__":
    main()
