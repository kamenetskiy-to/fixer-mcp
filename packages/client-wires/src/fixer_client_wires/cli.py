from __future__ import annotations

import argparse
import json
import sys
from dataclasses import asdict

from .executor import execute_launch_plan
from .backends import available_backend_descriptors
from .bootstrap import bootstrap_runtime_import_path, wire_info_lines
from .launcher import available_roles, build_fresh_launch_plan, build_resume_plan, render_launch_plan


def _build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="Public client-wires CLI for the GitHub-ready Fixer MCP track.")
    subparsers = parser.add_subparsers(dest="command")

    subparsers.add_parser("wire-info", help="Print resolved runtime and config details.")
    subparsers.add_parser("list-roles", help="List supported launcher roles.")
    subparsers.add_parser("list-backends", help="List supported launcher backends.")

    plan_parser = subparsers.add_parser(
        "plan-launch",
        help="Build a staged headless launch plan without importing the legacy workspace.",
    )
    plan_parser.add_argument("--role", required=True, choices=sorted(role.name for role in available_roles()))
    plan_parser.add_argument("--backend", default="codex")
    plan_parser.add_argument("--model")
    plan_parser.add_argument("--reasoning")
    plan_parser.add_argument("--prompt")
    plan_parser.add_argument("--mcp-server", action="append", dest="mcp_servers", default=[])
    plan_parser.add_argument("--json", action="store_true", help="Emit the launch plan as JSON.")

    launch_parser = subparsers.add_parser(
        "launch",
        help="Execute a fresh packaged launch using the staged runtime and config contract.",
    )
    launch_parser.add_argument("--role", required=True, choices=sorted(role.name for role in available_roles()))
    launch_parser.add_argument("--backend", default="codex")
    launch_parser.add_argument("--model")
    launch_parser.add_argument("--reasoning")
    launch_parser.add_argument("--prompt")
    launch_parser.add_argument("--mcp-server", action="append", dest="mcp_servers", default=[])

    resume_parser = subparsers.add_parser(
        "plan-resume",
        help="Build a staged headless resume plan using stored backend metadata and an external session id.",
    )
    resume_parser.add_argument("--role", required=True, choices=sorted(role.name for role in available_roles()))
    resume_parser.add_argument("--backend", required=True)
    resume_parser.add_argument("--session-id", required=True, dest="external_session_id")
    resume_parser.add_argument("--prompt")
    resume_parser.add_argument("--mcp-server", action="append", dest="mcp_servers", default=[])
    resume_parser.add_argument("--json", action="store_true", help="Emit the resume plan as JSON.")

    resume_exec_parser = subparsers.add_parser(
        "resume",
        help="Execute a packaged resume launch using stored backend metadata and an external session id.",
    )
    resume_exec_parser.add_argument("--role", required=True, choices=sorted(role.name for role in available_roles()))
    resume_exec_parser.add_argument("--backend", required=True)
    resume_exec_parser.add_argument("--session-id", required=True, dest="external_session_id")
    resume_exec_parser.add_argument("--prompt")
    resume_exec_parser.add_argument("--mcp-server", action="append", dest="mcp_servers", default=[])

    return parser


def _print_roles() -> int:
    print("Supported roles:")
    for role in available_roles():
        print(f"- {role.name}: {role.summary}")
    return 0


def _print_backends() -> int:
    print("Supported backends:")
    for backend in available_backend_descriptors():
        modes = []
        modes.append("fresh" if backend.fresh_launch_supported else "fresh planned")
        modes.append("resume" if backend.resume_supported else "resume planned")
        print(f"- {backend.name}: {backend.description} [{', '.join(modes)}]")
    return 0


def _print_plan(args: argparse.Namespace) -> int:
    plan = build_fresh_launch_plan(
        role=args.role,
        backend=args.backend,
        model=args.model,
        reasoning=args.reasoning,
        prompt=args.prompt,
        mcp_servers=args.mcp_servers,
    )
    if args.json:
        payload = {
            "mode": plan.mode,
            "role": asdict(plan.role),
            "backend": asdict(plan.backend),
            "command": list(plan.command),
            "runtime_root": str(plan.runtime_resolution.root),
            "runtime_package": plan.runtime_resolution.package_name,
            "runtime_source": plan.runtime_resolution.source,
            "config_path": str(plan.config_path),
            "config_source": plan.config_source,
            "config_kind": plan.config_kind,
            "selected_mcp_servers": list(plan.selected_mcp_servers),
            "prompt": plan.prompt,
            "notes": list(plan.notes),
        }
        print(json.dumps(payload, indent=2, sort_keys=True))
        return 0

    print("\n".join(render_launch_plan(plan)))
    return 0


def _print_resume_plan(args: argparse.Namespace) -> int:
    plan = build_resume_plan(
        role=args.role,
        backend=args.backend,
        external_session_id=args.external_session_id,
        prompt=args.prompt,
        mcp_servers=args.mcp_servers,
    )
    if args.json:
        payload = {
            "mode": plan.mode,
            "role": asdict(plan.role),
            "backend": asdict(plan.backend),
            "command": list(plan.command),
            "runtime_root": str(plan.runtime_resolution.root),
            "runtime_package": plan.runtime_resolution.package_name,
            "runtime_source": plan.runtime_resolution.source,
            "config_path": str(plan.config_path),
            "config_source": plan.config_source,
            "config_kind": plan.config_kind,
            "selected_mcp_servers": list(plan.selected_mcp_servers),
            "external_session_id": plan.external_session_id,
            "prompt": plan.prompt,
            "notes": list(plan.notes),
        }
        print(json.dumps(payload, indent=2, sort_keys=True))
        return 0

    print("\n".join(render_launch_plan(plan)))
    return 0


def _execute_fresh_launch(args: argparse.Namespace) -> int:
    plan = build_fresh_launch_plan(
        role=args.role,
        backend=args.backend,
        model=args.model,
        reasoning=args.reasoning,
        prompt=args.prompt,
        mcp_servers=args.mcp_servers,
    )
    return execute_launch_plan(plan)


def _execute_resume_launch(args: argparse.Namespace) -> int:
    plan = build_resume_plan(
        role=args.role,
        backend=args.backend,
        external_session_id=args.external_session_id,
        prompt=args.prompt,
        mcp_servers=args.mcp_servers,
    )
    return execute_launch_plan(plan)


def main(argv: list[str] | None = None) -> int:
    parser = _build_parser()
    args = parser.parse_args(argv)

    try:
        if args.command in (None, "wire-info"):
            resolution = bootstrap_runtime_import_path()
            print("\n".join(wire_info_lines(resolution)))
            return 0
        if args.command == "list-roles":
            return _print_roles()
        if args.command == "list-backends":
            return _print_backends()
        if args.command == "plan-launch":
            return _print_plan(args)
        if args.command == "launch":
            return _execute_fresh_launch(args)
        if args.command == "plan-resume":
            return _print_resume_plan(args)
        if args.command == "resume":
            return _execute_resume_launch(args)
    except RuntimeError as exc:
        print(str(exc), file=sys.stderr)
        return 2

    parser.error(f"unsupported command: {args.command}")
    return 2
