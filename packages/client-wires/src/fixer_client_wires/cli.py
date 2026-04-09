from __future__ import annotations

import argparse
import json
import sys
from dataclasses import asdict
from pathlib import Path

from .executor import execute_launch_plan
from .backends import available_backend_descriptors
from .bootstrap import bootstrap_runtime_import_path, wire_info_lines
from .launcher import available_roles, build_fresh_launch_plan, build_resume_plan, render_launch_plan

FIXER_LAUNCH_NEW = "__fixer_launch_new__"
FIXER_LAUNCH_RESUME = "__fixer_launch_resume__"


def _role_choice_lines() -> list[str]:
    roles = available_roles()
    lines = ["Select role:"]
    for index, role in enumerate(roles, start=1):
        lines.append(f"{index}. {role.name} - {role.summary}")
    return lines


def _prompt_for_role_selection() -> str | None:
    if not sys.stdin.isatty():
        return None
    roles = available_roles()
    print("\n".join(_role_choice_lines()))
    while True:
        try:
            raw_choice = input("Enter role number or name: ").strip()
        except EOFError:
            return None
        if not raw_choice:
            print("Choose fixer, netrunner, or overseer.")
            continue
        normalized = raw_choice.lower()
        for role in roles:
            if normalized == role.name:
                return role.name
        if raw_choice.isdigit():
            index = int(raw_choice)
            if 1 <= index <= len(roles):
                return roles[index - 1].name
        print("Choose fixer, netrunner, or overseer.")


def _prompt_select(
    *,
    title: str,
    options: list[tuple[str, str]],
    empty_message: str,
) -> str | None:
    if not sys.stdin.isatty():
        return None
    lines = [title]
    for index, (_value, label) in enumerate(options, start=1):
        lines.append(f"{index}. {label}")
    print("\n".join(lines))
    while True:
        try:
            raw_choice = input("Enter number or name: ").strip()
        except EOFError:
            return None
        if not raw_choice:
            print(empty_message)
            continue
        normalized = raw_choice.lower()
        for value, label in options:
            if normalized == value.lower() or normalized == label.lower():
                return value
        if raw_choice.isdigit():
            index = int(raw_choice)
            if 1 <= index <= len(options):
                return options[index - 1][0]
        print(empty_message)


def _prompt_for_fixer_launch_action() -> str | None:
    return _prompt_select(
        title="Fixer launch mode:",
        options=[
            (FIXER_LAUNCH_NEW, "Start new Fixer"),
            (FIXER_LAUNCH_RESUME, "Resume existing Fixer"),
        ],
        empty_message="Choose Start new Fixer or Resume existing Fixer.",
    )


def _prompt_for_backend_selection(preferred_backend: str) -> str | None:
    descriptors = available_backend_descriptors()
    options = [(descriptor.name, f"{descriptor.label} - {descriptor.description}") for descriptor in descriptors]
    return _prompt_select(
        title="Select backend:",
        options=options,
        empty_message=f"Choose one of: {', '.join(descriptor.name for descriptor in descriptors)}.",
    ) or preferred_backend


def _prompt_for_model_selection(backend: str, preferred_model: str | None) -> str | None:
    descriptor = next(item for item in available_backend_descriptors() if item.name == backend)
    options = [(model, model) for model in descriptor.model_options]
    return _prompt_select(
        title=f"Select model for {backend}:",
        options=options,
        empty_message="Choose one of the listed models.",
    ) or preferred_model


def _prompt_for_reasoning_selection(backend: str, preferred_reasoning: str | None) -> str | None:
    descriptor = next(item for item in available_backend_descriptors() if item.name == backend)
    options = [(reasoning, reasoning) for reasoning in descriptor.reasoning_options]
    return _prompt_select(
        title=f"Select reasoning for {backend}:",
        options=options,
        empty_message="Choose one of the listed reasoning values.",
    ) or preferred_reasoning


def _prompt_for_resume_session_id(role: str, backend: str) -> str | None:
    if not sys.stdin.isatty():
        return None
    while True:
        try:
            raw_value = input(f"Enter existing {backend} session id to resume for role {role}: ").strip()
        except EOFError:
            return None
        if raw_value:
            return raw_value
        print("Session id is required for resume.")


def _build_direct_entry_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="Repo-native Fixer launcher entrypoint for the GitHub-ready Fixer MCP track."
    )
    parser.add_argument(
        "--wire-info",
        action="store_true",
        help="Render the staged runtime and config resolution for the direct entrypoint.",
    )
    parser.add_argument(
        "--role",
        choices=sorted(role.name for role in available_roles()),
        help="Launch role. Defaults to fixer when invoked via the fixer console script.",
    )
    parser.add_argument("--backend", default="codex")
    parser.add_argument(
        "--resume-session-id",
        help="External session id for resume execution or preview.",
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
        help="Emit delegated plan output as JSON for dry-run entrypoint usage.",
    )
    return parser


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


def _render_missing_role_message(parser: argparse.ArgumentParser) -> int:
    parser.print_help()
    print()
    print("\n".join(_role_choice_lines()), file=sys.stderr)
    print(
        "\nDirect entry note: run `fixer` interactively to choose a role first, pass --role to bypass the selector, "
        "or use the explicit subcommands.",
        file=sys.stderr,
    )
    return 2


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


def _run_subcommand(argv: list[str]) -> int:
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


def _run_direct_entry(argv: list[str], *, invoked_as: str) -> int:
    parser = _build_direct_entry_parser()
    args = parser.parse_args(argv)

    if args.wire_info:
        return _run_subcommand(["wire-info"])

    role = args.role
    if role is None and invoked_as == "fixer":
        role = _prompt_for_role_selection()
    if not role:
        return _render_missing_role_message(parser)

    backend = args.backend
    model = args.model
    reasoning = args.reasoning
    resume_session_id = args.resume_session_id

    if sys.stdin.isatty():
        if role == "fixer" and not resume_session_id:
            launch_mode = _prompt_for_fixer_launch_action()
            if not launch_mode:
                return 130
            if launch_mode == FIXER_LAUNCH_RESUME:
                resume_session_id = _prompt_for_resume_session_id(role, backend)
                if not resume_session_id:
                    return 130

        if not resume_session_id:
            backend = _prompt_for_backend_selection(backend)
            if not backend:
                return 130
            if not model:
                model = _prompt_for_model_selection(backend, model)
                if not model:
                    return 130
            if not reasoning:
                reasoning = _prompt_for_reasoning_selection(backend, reasoning)
                if not reasoning:
                    return 130

    render_only = args.dry_run or args.json
    if resume_session_id:
        delegated_args = [
            "plan-resume" if render_only else "resume",
            "--role",
            role,
            "--backend",
            backend,
            "--session-id",
            resume_session_id,
        ]
    else:
        delegated_args = ["plan-launch" if render_only else "launch", "--role", role, "--backend", backend]
        if model:
            delegated_args.extend(["--model", model])
        if reasoning:
            delegated_args.extend(["--reasoning", reasoning])
    if args.prompt:
        delegated_args.extend(["--prompt", args.prompt])
    for server in args.mcp_servers:
        delegated_args.extend(["--mcp-server", server])
    if args.json:
        delegated_args.append("--json")
    return _run_subcommand(delegated_args)


def main(argv: list[str] | None = None) -> int:
    raw_argv = list(argv) if argv is not None else sys.argv[1:]
    invoked_as = Path(sys.argv[0]).name
    direct_entry_mode = (invoked_as == "fixer" and (not raw_argv or raw_argv[0].startswith("--"))) or (
        raw_argv and raw_argv[0].startswith("--")
    )
    if direct_entry_mode:
        return _run_direct_entry(raw_argv, invoked_as=invoked_as)
    return _run_subcommand(raw_argv)
