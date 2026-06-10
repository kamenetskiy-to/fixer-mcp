"""Prompt and MCP guidance builders for the Fixer wire launcher."""

from __future__ import annotations

import textwrap
from pathlib import Path
from typing import Callable, Sequence

from client_wires.backends import normalize_backend_name
from client_wires import fixer_wire_db

RegistryMcpMetadata = fixer_wire_db.RegistryMcpMetadata

WEB_STACK_GUIDANCE_MCP_NAMES = {
    "playwright",
    "playwright-mcp",
    "playwright_mcp",
    "chrome-devtools",
    "chrome-devtools-mcp",
    "chrome_devtools_mcp",
    "eslint",
    "eslint-mcp",
    "eslint_mcp",
    "mcp-language-server",
    "mcp_language_server",
}
STANDARD_WEB_STACK_GUIDANCE = (
    "Next.js (App Router)",
    "React + react-dom",
    "TypeScript strict",
    "Tailwind CSS + daisyUI",
    "Framer Motion",
    "react-responsive",
    "eslint + eslint-config-next",
)
NETRUNNER_KIND_MANUAL = "manual"
NETRUNNER_KIND_ACCEPTANCE = "acceptance"
NETRUNNER_MANUAL_SKILL_NAME = "run-manual-netrunner"
NETRUNNER_ACCEPTANCE_SKILL_NAME = "run-manual-acceptance-netrunner"


def _normalize_names(values: Sequence[str]) -> list[str]:
    seen: set[str] = set()
    names: list[str] = []
    for raw in values:
        for part in raw.split(","):
            name = part.strip()
            if not name or name in seen:
                continue
            seen.add(name)
            names.append(name)
    names.sort()
    return names


def _build_netrunner_prompt(
    session_id: int,
    mcp_names: Sequence[str],
    mcp_how_to: dict[str, str],
    *,
    netrunner_kind: str = NETRUNNER_KIND_MANUAL,
    default_how_to: Callable[[str], str] | None = None,
    standard_web_stack_guidance_block: Callable[[Sequence[str]], str] | None = None,
) -> str:
    default_how_to = default_how_to or _build_default_how_to
    standard_web_stack_guidance_block = (
        standard_web_stack_guidance_block or _build_standard_web_stack_guidance_block
    )
    skill_name = (
        NETRUNNER_ACCEPTANCE_SKILL_NAME
        if netrunner_kind == NETRUNNER_KIND_ACCEPTANCE
        else NETRUNNER_MANUAL_SKILL_NAME
    )
    mcp_text = ", ".join(mcp_names) if mcp_names else "none"
    how_to_lines: list[str] = []
    for name in mcp_names:
        guidance = mcp_how_to.get(name, default_how_to(name))
        how_to_lines.append(f"- {name}: {guidance}")
    standard_web_stack_text = standard_web_stack_guidance_block(mcp_names)
    prompt_lines = [
        f"Activate skill `${skill_name}` immediately.",
        "Use its Netrunner separate-terminal mode for this launch.",
        "Execute only its initialization checklist first, then stop and report status.",
        "",
        f"Preselected session ID from fixer wire: `{session_id}`.",
        f"Assigned MCP selection from fixer wire: {mcp_text}.",
        "Attached MCP how-to guidance:",
        *(how_to_lines or ["- none"]),
        "After checkout, call `fixer_mcp.log_netrunner_progress` with `log_type=\"started\"`; use only `started`, `progress`, `blocked`, `workaround`, or `completed`.",
    ]
    if standard_web_stack_text:
        prompt_lines.extend(["", *standard_web_stack_text.splitlines()])
    prompt_lines.append("Use this session ID for checkout unless Architect explicitly overrides.")
    return "\n".join(prompt_lines)


def _build_droid_netrunner_prompt(
    session_id: int,
    mcp_names: Sequence[str],
    *,
    netrunner_kind: str = NETRUNNER_KIND_MANUAL,
) -> str:
    skill_name = (
        NETRUNNER_ACCEPTANCE_SKILL_NAME
        if netrunner_kind == NETRUNNER_KIND_ACCEPTANCE
        else NETRUNNER_MANUAL_SKILL_NAME
    )
    mcp_text = ", ".join(mcp_names) if mcp_names else "none"
    return "\n".join(
        [
            f"Activate skill `${skill_name}` immediately.",
            "Use Netrunner separate-terminal mode.",
            f"Run the initialization checklist for session `{session_id}`, then report status.",
            f"Assigned MCPs: {mcp_text}.",
            "Droid MCP note: use exact Fixer MCP tool ids `fixer_mcp___assume_role`, `fixer_mcp___checkout_task`, `fixer_mcp___log_netrunner_progress`, `fixer_mcp___get_attached_project_docs`, and `fixer_mcp___get_session_mcp_servers`.",
            "After checkout, call `fixer_mcp___log_netrunner_progress` with `log_type=\"started\"`; use only `started`, `progress`, `blocked`, `workaround`, or `completed`.",
            "Do not stop with an MCP-not-mounted report while Droid shows the assigned MCPs are connected.",
        ]
    )


def _build_default_how_to(server_name: str) -> str:
    return f"Use {server_name} for domain-specific tools in this task; inspect tool descriptions before execution."


def _build_standard_web_stack_guidance_block(mcp_names: Sequence[str]) -> str:
    selected = {name.strip() for name in mcp_names}
    if not selected.intersection(WEB_STACK_GUIDANCE_MCP_NAMES):
        return ""
    lines = ["Standard web stack guidance:"]
    lines.extend(f"- {item}" for item in STANDARD_WEB_STACK_GUIDANCE)
    return "\n".join(lines)


def _build_droid_mcp_tool_guidance_block(
    mcp_names: Sequence[str],
    *,
    normalize_names: Callable[[Sequence[str]], list[str]] | None = None,
) -> str:
    normalize_names = normalize_names or _normalize_names
    selected = normalize_names(mcp_names)
    server_text = ", ".join(selected) if selected else "none"
    lines = [
        "Droid MCP tool guidance:",
        f"- Selected MCP servers for this Droid launch: {server_text}.",
        "- Use exact Fixer MCP tool ids in the form `fixer_mcp___<tool>`, for example `fixer_mcp___assume_role`.",
        "- Do not stop with an MCP-not-mounted report while Droid shows the selected MCP servers are connected.",
    ]
    return "\n".join(lines)


def _append_droid_mcp_tool_guidance(
    prompt: str,
    *,
    backend: str,
    mcp_names: Sequence[str],
    backend_normalizer: Callable[[str], str] | None = None,
    droid_mcp_tool_guidance_block: Callable[[Sequence[str]], str] | None = None,
) -> str:
    backend_normalizer = backend_normalizer or normalize_backend_name
    droid_mcp_tool_guidance_block = (
        droid_mcp_tool_guidance_block or _build_droid_mcp_tool_guidance_block
    )
    if backend_normalizer(backend) != "droid":
        return prompt
    guidance = droid_mcp_tool_guidance_block(mcp_names)
    if not prompt.strip():
        return guidance
    return "\n\n".join([prompt.rstrip(), guidance])


def _build_mcp_how_to_map(
    mcp_names: Sequence[str],
    registry_meta: dict[str, RegistryMcpMetadata],
    *,
    registry_metadata_with_fallback: Callable[
        [str, RegistryMcpMetadata | None], RegistryMcpMetadata | None
    ]
    | None = None,
    default_how_to: Callable[[str], str] | None = None,
) -> dict[str, str]:
    registry_metadata_with_fallback = (
        registry_metadata_with_fallback or fixer_wire_db._registry_metadata_with_fallback
    )
    default_how_to = default_how_to or _build_default_how_to
    how_to_by_name: dict[str, str] = {}
    for name in mcp_names:
        metadata = registry_metadata_with_fallback(name, registry_meta.get(name))
        how_to = (metadata.how_to if metadata else "").strip()
        if not how_to:
            how_to = default_how_to(name)
        how_to_by_name[name] = how_to
    return how_to_by_name


def _build_fixer_prompt() -> str:
    return textwrap.dedent(
        """
        Activate skill `$init-fixer` immediately.
        Fixer is the project-scoped orchestrator role in the current Fixer MCP system.
        Execute only its initialization checklist first, then stop and report status.
        """
    ).strip()


def _build_unattached_fixer_prompt(scratch_cwd: Path) -> str:
    return textwrap.dedent(
        f"""
        Activate skill `$init-unattached-fixer` immediately.
        This is Unattached Fixer mode.
        You are bound to an internal scratch workspace, not to the operator's current product repository.
        Scratch workspace: `{scratch_cwd.resolve()}`.
        Use project docs, handoff, and autonomous run status for durable ad-hoc context.
        You may create normal Netrunner tasks for research and automation work.
        Assign task-specific MCP servers through the existing project/session MCP allowlist tools before launching Netrunners.
        Put outputs under the scratch workspace unless the Architect explicitly names another destination.
        Execute only the `$init-unattached-fixer` initialization checklist first, then stop and report status.
        """
    ).strip()


def _build_overseer_prompt() -> str:
    return textwrap.dedent(
        """
        Activate skill `$init-overseer` immediately.
        Overseer is the global analysis and routing role in the current Fixer MCP system.
        Execute only its initialization checklist first, then stop and report status.
        """
    ).strip()
