from __future__ import annotations

import json
from dataclasses import dataclass
from pathlib import Path
from typing import Mapping, Sequence

from .backends import DEFAULT_BACKEND, BackendDescriptor, available_backend_descriptors, get_backend_adapter
from .bootstrap import (
    RuntimeResolution,
    bootstrap_runtime_import_path,
    resolve_config_path,
    resolve_package_root,
    resolve_repo_root,
)


@dataclass(frozen=True)
class RoleDescriptor:
    name: str
    summary: str
    prompt_stub: str


@dataclass(frozen=True)
class LaunchPlan:
    role: RoleDescriptor
    backend: BackendDescriptor
    command: tuple[str, ...]
    runtime_resolution: RuntimeResolution
    config_path: Path
    config_source: str
    config_kind: str
    selected_mcp_servers: tuple[str, ...]
    prompt: str
    notes: tuple[str, ...]


ROLE_DESCRIPTORS: dict[str, RoleDescriptor] = {
    "fixer": RoleDescriptor(
        name="fixer",
        summary="Orchestrator role for dispatch, review, and canon updates.",
        prompt_stub="Review the current project state, dispatch work, and maintain canon.",
    ),
    "netrunner": RoleDescriptor(
        name="netrunner",
        summary="Implementation worker role for code, tests, and completion reports.",
        prompt_stub="Implement the assigned task, update tests, and submit the completion report.",
    ),
    "overseer": RoleDescriptor(
        name="overseer",
        summary="High-level coordinator role for workspace analysis and worker selection.",
        prompt_stub="Inspect the workspace, answer high-level questions, and decide which worker to use.",
    ),
}


def available_roles() -> list[RoleDescriptor]:
    return list(ROLE_DESCRIPTORS.values())


def get_role_descriptor(name: str) -> RoleDescriptor:
    normalized = name.strip().lower()
    try:
        return ROLE_DESCRIPTORS[normalized]
    except KeyError as exc:
        supported = ", ".join(sorted(ROLE_DESCRIPTORS))
        raise RuntimeError(f"Unsupported launch role {name!r}. Supported roles: {supported}") from exc


def load_mcp_config(config_path: Path) -> dict[str, Mapping[str, object]]:
    payload = json.loads(config_path.read_text(encoding="utf-8"))
    servers = payload.get("mcpServers", {})
    if not isinstance(servers, dict):
        raise RuntimeError(f"{config_path} does not contain an object-valued mcpServers map")
    normalized: dict[str, Mapping[str, object]] = {}
    for name, value in servers.items():
        if isinstance(value, dict):
            normalized[str(name)] = value
        else:
            normalized[str(name)] = {}
    return normalized


def build_launch_plan(
    *,
    role: str,
    backend: str | None = None,
    model: str | None = None,
    reasoning: str | None = None,
    prompt: str | None = None,
    mcp_servers: Sequence[str] | None = None,
    repo_root: Path | None = None,
    package_root: Path | None = None,
    environ: Mapping[str, str] | None = None,
) -> LaunchPlan:
    selected_role = get_role_descriptor(role)
    selected_backend = get_backend_adapter(backend or DEFAULT_BACKEND)
    resolved_repo_root = (repo_root or resolve_repo_root()).resolve()
    resolved_package_root = (package_root or resolve_package_root()).resolve()
    runtime_resolution = bootstrap_runtime_import_path(
        repo_root=resolved_repo_root,
        package_root=resolved_package_root,
        environ=environ,
    )
    config_resolution = resolve_config_path(
        repo_root=resolved_repo_root,
        package_root=resolved_package_root,
        environ=environ,
    )
    available_servers = load_mcp_config(config_resolution.path)
    chosen_names = tuple(sorted(set(mcp_servers or available_servers.keys())))
    selected_servers = {name: available_servers.get(name, {}) for name in chosen_names}
    resolved_prompt = (prompt or "").strip() or selected_role.prompt_stub
    resolved_model = selected_backend.normalize_model(model)
    resolved_reasoning = selected_backend.normalize_reasoning(reasoning)
    notes = [
        f"role summary: {selected_role.summary}",
        f"runtime source: {runtime_resolution.source}",
        f"config source: {config_resolution.source}",
    ]
    notes.extend(selected_backend.runtime_side_effects(selected_mcp_servers=selected_servers))
    return LaunchPlan(
        role=selected_role,
        backend=selected_backend.descriptor,
        command=tuple(
            selected_backend.build_headless_command(
                model=resolved_model,
                reasoning=resolved_reasoning,
                prompt=resolved_prompt,
                selected_mcp_servers=selected_servers,
            )
        ),
        runtime_resolution=runtime_resolution,
        config_path=config_resolution.path,
        config_source=config_resolution.source,
        config_kind=config_resolution.kind,
        selected_mcp_servers=chosen_names,
        prompt=resolved_prompt,
        notes=tuple(notes),
    )


def render_launch_plan(plan: LaunchPlan) -> list[str]:
    lines = [
        "Fixer client-wires launch plan:",
        f"- role: {plan.role.name}",
        f"- backend: {plan.backend.name}",
        f"- runtime root: {plan.runtime_resolution.root}",
        f"- runtime source: {plan.runtime_resolution.source}",
        f"- config path: {plan.config_path}",
        f"- config source: {plan.config_source}",
        f"- config kind: {plan.config_kind}",
        f"- selected MCP servers: {', '.join(plan.selected_mcp_servers) if plan.selected_mcp_servers else '(none)'}",
        f"- command: {' '.join(plan.command)}",
    ]
    lines.extend(f"- note: {note}" for note in plan.notes)
    return lines


__all__ = [
    "LaunchPlan",
    "RoleDescriptor",
    "ROLE_DESCRIPTORS",
    "available_backend_descriptors",
    "available_roles",
    "build_launch_plan",
    "get_role_descriptor",
    "render_launch_plan",
]
