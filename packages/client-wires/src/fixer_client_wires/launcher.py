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
    resolve_staged_skills_root,
)


@dataclass(frozen=True)
class RoleDescriptor:
    name: str
    summary: str
    prompt_stub: str


@dataclass(frozen=True)
class LaunchPlan:
    mode: str
    role: RoleDescriptor
    backend: BackendDescriptor
    command: tuple[str, ...]
    runtime_resolution: RuntimeResolution
    config_path: Path
    config_source: str
    config_kind: str
    selected_mcp_servers: tuple[str, ...]
    external_session_id: str | None
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
    return build_fresh_launch_plan(
        role=role,
        backend=backend,
        model=model,
        reasoning=reasoning,
        prompt=prompt,
        mcp_servers=mcp_servers,
        repo_root=repo_root,
        package_root=package_root,
        environ=environ,
    )


def _resolve_plan_context(
    *,
    role: str,
    backend: str | None,
    prompt: str | None,
    mcp_servers: Sequence[str] | None,
    repo_root: Path | None,
    package_root: Path | None,
    environ: Mapping[str, str] | None,
) -> tuple[RoleDescriptor, object, RuntimeResolution, Path, str, str, dict[str, Mapping[str, object]], tuple[str, ...], str]:
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
    skills_resolution = resolve_staged_skills_root(environ=environ)
    available_servers = load_mcp_config(config_resolution.path)
    chosen_names = tuple(sorted(set(mcp_servers or available_servers.keys())))
    selected_servers = {name: available_servers.get(name, {}) for name in chosen_names}
    resolved_prompt = (prompt or "").strip() or selected_role.prompt_stub
    return (
        selected_role,
        selected_backend,
        runtime_resolution,
        config_resolution.path,
        config_resolution.source,
        config_resolution.kind,
        skills_resolution,
        selected_servers,
        chosen_names,
        resolved_prompt,
    )


def build_fresh_launch_plan(
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
    (
        selected_role,
        selected_backend,
        runtime_resolution,
        config_path,
        config_source,
        config_kind,
        skills_resolution,
        selected_servers,
        chosen_names,
        resolved_prompt,
    ) = _resolve_plan_context(
        role=role,
        backend=backend,
        prompt=prompt,
        mcp_servers=mcp_servers,
        repo_root=repo_root,
        package_root=package_root,
        environ=environ,
    )
    if not selected_backend.descriptor.fresh_launch_supported:
        raise RuntimeError(
            f"Backend {selected_backend.name!r} is listed in the public catalog but does not support staged headless fresh launches."
        )
    resolved_model = selected_backend.normalize_model(model)
    resolved_reasoning = selected_backend.normalize_reasoning(reasoning)
    notes = [
        f"role summary: {selected_role.summary}",
        f"runtime source: {runtime_resolution.source}",
        f"config source: {config_source}",
        f"staged skills source: {skills_resolution.source}",
        f"staged skills root: {skills_resolution.root}",
        "fresh launches may choose backend, model, and reasoning on this surface",
    ]
    notes.extend(selected_backend.runtime_side_effects(mode="fresh launch", selected_mcp_servers=selected_servers))
    return LaunchPlan(
        mode="fresh",
        role=selected_role,
        backend=selected_backend.descriptor,
        command=tuple(
            selected_backend.build_fresh_command(
                model=resolved_model,
                reasoning=resolved_reasoning,
                prompt=resolved_prompt,
                selected_mcp_servers=selected_servers,
            )
        ),
        runtime_resolution=runtime_resolution,
        config_path=config_path,
        config_source=config_source,
        config_kind=config_kind,
        selected_mcp_servers=chosen_names,
        external_session_id=None,
        prompt=resolved_prompt,
        notes=tuple(notes),
    )


def build_resume_plan(
    *,
    role: str,
    backend: str,
    external_session_id: str,
    prompt: str | None = None,
    mcp_servers: Sequence[str] | None = None,
    repo_root: Path | None = None,
    package_root: Path | None = None,
    environ: Mapping[str, str] | None = None,
) -> LaunchPlan:
    (
        selected_role,
        selected_backend,
        runtime_resolution,
        config_path,
        config_source,
        config_kind,
        skills_resolution,
        selected_servers,
        chosen_names,
        resolved_prompt,
    ) = _resolve_plan_context(
        role=role,
        backend=backend,
        prompt=prompt,
        mcp_servers=mcp_servers,
        repo_root=repo_root,
        package_root=package_root,
        environ=environ,
    )
    if not external_session_id.strip():
        raise RuntimeError("Resume planning requires a non-empty external session id.")
    if not selected_backend.descriptor.resume_supported:
        raise RuntimeError(
            f"Backend {selected_backend.name!r} is listed in the public catalog but does not support staged headless resume planning."
        )
    notes = [
        f"role summary: {selected_role.summary}",
        f"runtime source: {runtime_resolution.source}",
        f"config source: {config_source}",
        f"staged skills source: {skills_resolution.source}",
        f"staged skills root: {skills_resolution.root}",
        "resume keeps backend, model, and reasoning sticky to stored session metadata",
    ]
    notes.extend(selected_backend.runtime_side_effects(mode="resume", selected_mcp_servers=selected_servers))
    return LaunchPlan(
        mode="resume",
        role=selected_role,
        backend=selected_backend.descriptor,
        command=tuple(
            selected_backend.build_resume_command(
                external_session_id=external_session_id,
                prompt=resolved_prompt,
                selected_mcp_servers=selected_servers,
            )
        ),
        runtime_resolution=runtime_resolution,
        config_path=config_path,
        config_source=config_source,
        config_kind=config_kind,
        selected_mcp_servers=chosen_names,
        external_session_id=external_session_id.strip(),
        prompt=resolved_prompt,
        notes=tuple(notes),
    )


def render_launch_plan(plan: LaunchPlan) -> list[str]:
    lines = [
        f"Fixer client-wires {plan.mode} plan:",
        f"- role: {plan.role.name}",
        f"- backend: {plan.backend.name}",
        f"- runtime root: {plan.runtime_resolution.root}",
        f"- runtime source: {plan.runtime_resolution.source}",
        f"- config path: {plan.config_path}",
        f"- config source: {plan.config_source}",
        f"- config kind: {plan.config_kind}",
        f"- selected MCP servers: {', '.join(plan.selected_mcp_servers) if plan.selected_mcp_servers else '(none)'}",
        f"- external session id: {plan.external_session_id or '(fresh launch)'}",
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
    "build_fresh_launch_plan",
    "build_launch_plan",
    "build_resume_plan",
    "get_role_descriptor",
    "render_launch_plan",
]
