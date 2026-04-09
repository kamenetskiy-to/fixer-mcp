from __future__ import annotations

import argparse
import sys
from pathlib import Path


def _resolve_repo_root() -> Path:
    return Path(__file__).resolve().parents[4]


def _bootstrap_client_wires_import() -> None:
    client_wires_src = _resolve_repo_root() / "packages" / "client-wires" / "src"
    client_wires_src_str = str(client_wires_src)
    if client_wires_src_str not in sys.path:
        sys.path.insert(0, client_wires_src_str)


def _build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="Compatibility bridge for legacy Fixer MCP launcher invocations."
    )
    parser.add_argument(
        "--wire-info",
        action="store_true",
        help="Render the staged runtime and config resolution using the old flag shape.",
    )
    parser.add_argument(
        "--role",
        choices=("fixer", "netrunner", "overseer"),
        help="Legacy role selector. Delegates to the staged plan-launch command.",
    )
    parser.add_argument("--backend", default="codex")
    parser.add_argument(
        "--resume-session-id",
        help="Legacy-style external session id for resume preview. Delegates to plan-resume when provided.",
    )
    parser.add_argument("--model")
    parser.add_argument("--reasoning")
    parser.add_argument("--prompt")
    parser.add_argument("--mcp-server", action="append", dest="mcp_servers", default=[])
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Render the delegated launch preview instead of executing it.",
    )
    parser.add_argument(
        "--json",
        action="store_true",
        help="Emit delegated plan-launch output as JSON when used with --role.",
    )
    return parser


def _render_missing_role_message(parser: argparse.ArgumentParser) -> int:
    parser.print_help()
    print(
        "\nCompatibility note: interactive legacy launching is not staged in github_repo yet. "
        "Use --wire-info or --role to exercise the GitHub-ready launcher boundary.",
        file=sys.stderr,
    )
    return 2


def main(argv: list[str] | None = None) -> int:
    parser = _build_parser()
    args = parser.parse_args(argv)
    invoked_as = Path(sys.argv[0]).name

    _bootstrap_client_wires_import()
    from fixer_client_wires import cli as client_cli

    if args.wire_info:
        return client_cli.main(["wire-info"])

    role = args.role
    if role is None and invoked_as == "fixer":
        role = "fixer"

    if not role:
        return _render_missing_role_message(parser)

    render_only = args.dry_run or args.json
    if args.resume_session_id:
        delegated_args = ["plan-resume" if render_only else "resume", "--role", role, "--backend", args.backend, "--session-id", args.resume_session_id]
    else:
        delegated_args = ["plan-launch" if render_only else "launch", "--role", role, "--backend", args.backend]
        if args.model:
            delegated_args.extend(["--model", args.model])
        if args.reasoning:
            delegated_args.extend(["--reasoning", args.reasoning])
    if args.prompt:
        delegated_args.extend(["--prompt", args.prompt])
    for server in args.mcp_servers:
        delegated_args.extend(["--mcp-server", server])
    if args.json:
        delegated_args.append("--json")
    return client_cli.main(delegated_args)
