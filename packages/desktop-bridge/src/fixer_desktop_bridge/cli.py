from __future__ import annotations

import argparse
import json
from pathlib import Path

from .app import serve
from .store import FixerDesktopStore, resolve_default_db_path


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="Run the Fixer desktop bridge.")
    subparsers = parser.add_subparsers(dest="command", required=True)

    serve_parser = subparsers.add_parser("serve", help="Start the local bridge HTTP server.")
    serve_parser.add_argument("--db-path", type=Path, default=None, help="Path to Fixer SQLite state.")
    serve_parser.add_argument("--host", default="127.0.0.1")
    serve_parser.add_argument("--port", type=int, default=8765)

    snapshot_parser = subparsers.add_parser(
        "snapshot",
        help="Print a project dashboard or session detail payload as JSON.",
    )
    snapshot_parser.add_argument("--db-path", type=Path, default=None, help="Path to Fixer SQLite state.")
    snapshot_group = snapshot_parser.add_mutually_exclusive_group(required=True)
    snapshot_group.add_argument("--project-id", type=int)
    snapshot_group.add_argument("--session-id", type=int)
    return parser


def main(argv: list[str] | None = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)
    db_path = args.db_path or resolve_default_db_path()
    store = FixerDesktopStore(db_path)

    if args.command == "serve":
        serve(host=args.host, port=args.port, store=store)
        return 0
    if args.command == "snapshot":
        if args.project_id is not None:
            payload = store.get_project_dashboard(args.project_id)
        else:
            payload = store.get_session_detail(args.session_id)
        print(json.dumps(payload, indent=2, sort_keys=True))
        return 0

    parser.error(f"unsupported command: {args.command}")
    return 2
