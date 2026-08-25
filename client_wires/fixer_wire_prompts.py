"""Prompt and MCP guidance builders for the Fixer wire launcher.

Bootstrap design (2026-06-14): prompts are MINIMAL, exactly like the codex flow.
We do NOT inject MCP tool-naming guidance or "list your tools first" instructions
of any kind — the MCP server is mounted and the agent calls its tools directly,
the same way it does under codex. Adding tool-access prose only confused droid
(it started listing connectors instead of calling the mounted tools). Skill
scoping and MCP-server scoping are handled by the launch/adapter layer, not by
prompt instructions.
"""

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
    "edge",
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
NETRUNNER_KIND_HANDS_GENERATION = "hands_generation"
# Compatibility alias for callers that still import the historical symbol.
NETRUNNER_KIND_MANUAL = NETRUNNER_KIND_HANDS_GENERATION
NETRUNNER_KIND_ACCEPTANCE = "acceptance"
NETRUNNER_MANUAL_SKILL_NAME = "hands-netrunner"
NETRUNNER_ACCEPTANCE_SKILL_NAME = "hands-netrunner"

FIXER_PROVIDER_RULE_MARKER = "[FIXER_PROVIDER_ORCHESTRATION_RULE]"

_FIXER_PROVIDER_ORCHESTRATION_RULES = {
    "codex": (
        "CRITICAL: Do not use Codex's built-in agent-thread controls "
        "(`/agent`, `/subagents`) or its task/cloud/scheduled-task orchestration. "
        "For all work that would use those features, use Fixer MCP Netrunner waves."
    ),
    "kimi-code": (
        "CRITICAL: Do not use Kimi Code's built-in `Agent` tool or background "
        "`Shell` tasks with `run_in_background=true`. For all work that would use "
        "those features, use Fixer MCP Netrunner waves."
    ),
    "claude": (
        "CRITICAL: Do not use Claude Code's built-in `Agent` tool, `claude agents`, "
        "or `--bg` background sessions. For all work that would use those features, "
        "use Fixer MCP Netrunner waves."
    ),
    "antigravity": (
        "CRITICAL: Don't use Antigravity's built-in `subagents`, `schedule`, or "
        "`manage_task` features. For all work that would use those features, use "
        "Fixer MCP Netrunner waves."
    ),
    "grok": (
        "CRITICAL: Do not use Grok Build's built-in subagent spawning (`--agents`, "
        "the Agent tool) or background worktrees for delegation. For all work that "
        "would use those features, use Fixer MCP Netrunner waves."
    ),
}


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
        f"Activate skill ${skill_name} immediately.",
        "Use its Netrunner execution-envelope mode for this disposable provider generation.",
        "Execute only its initialization checklist first, then stop and report status.",
        "",
        f"Preselected compatibility session ID from fixer wire: `{session_id}`.",
        "This session is an execution envelope, not the permanent project `Руки` identity or mailbox.",
        f"Assigned MCP selection from fixer wire: {mcp_text}.",
        "Attached MCP how-to guidance:",
        *(how_to_lines or ["- none"]),
        "After checkout, call `fixer_mcp.log_netrunner_progress` with `log_type=\"started\"`; use only `started`, `progress`, `blocked`, `workaround`, or `completed`.",
    ]
    if standard_web_stack_text:
        prompt_lines.extend(["", *standard_web_stack_text.splitlines()])
    prompt_lines.append("Use this compatibility session ID for checkout unless Architect explicitly overrides.")
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
            f"Activate skill ${skill_name} immediately.",
            "Use Netrunner execution-envelope mode for this disposable provider generation.",
            f"Run the initialization checklist for compatibility session `{session_id}`, then report status.",
            "The session is not the permanent project `Руки` identity or mailbox.",
            f"Assigned MCPs: {mcp_text}.",
            "After checkout, call `fixer_mcp.log_netrunner_progress` with `log_type=\"started\"`; use only `started`, `progress`, `blocked`, `workaround`, or `completed`.",
        ]
    )


def _build_project_hands_prompt(
    *,
    provider_lane: str,
    worktree_path: str = "",
    branch_name: str = "",
    attached_doc_files: Sequence[str] = (),
) -> str:
    prompt = textwrap.dedent(
        f"""
        Activate skill $hands-netrunner immediately.
        Use its Project Hands Channel Mode for the current project.
        {FIXER_PROVIDER_RULE_MARKER}
        This provider process is a disposable client of the one permanent project actor `Руки`.
        Selected execution lane: `{provider_lane}`.
        Read the durable Hands state and mailbox from Fixer MCP; do not discover or resume provider transcripts as the source of history.
        First call `fixer_mcp.assume_role` with `role="netrunner"` and the current project `cwd`.
        Submit instructions to the current project without creating or asking for a Netrunner session ID.
        Provider names select execution lanes for the same actor and mailbox; they never create another actor.
        Repository-write results remain awaiting explicit Fixer review. Never simulate acceptance or weaken the review gate.
        Execute only the channel initialization checklist first. Then answer in one short line: confirm that the channel is initialized, state the current Hands task, and ask whether to bring the app up through tmux-flow. Do not print actor IDs, lanes, queue counts, journal state, history, a checklist report, or any extra explanation.
        """
    ).strip()
    extra_blocks: list[str] = []
    if worktree_path.strip():
        extra_blocks.append(
            textwrap.dedent(
                f"""
                You are running inside a dedicated clean git worktree: `{worktree_path.strip()}` (branch `{branch_name.strip()}`).
                Keep every repository read/write inside this worktree; never touch the main checkout or other worktrees.
                """
            ).strip()
        )
    if attached_doc_files:
        doc_lines = "\n".join(f"- `{path}`" for path in attached_doc_files)
        extra_blocks.append(
            textwrap.dedent(
                f"""
                The Architect attached exactly this project documentation to your session; it is the ONLY project documentation available to you:
                {doc_lines}
                Read `.hands/project_docs/_index.md` first, then the listed files when relevant. Do not ask Fixer MCP for other project docs.
                """
            ).strip()
        )
    if extra_blocks:
        prompt = prompt + "\n\n" + "\n\n".join(extra_blocks)
    return materialize_fixer_provider_prompt(prompt, provider_lane)


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
    # Deliberately empty: droid (like codex) calls mounted MCP tools directly,
    # with no extra prose. Retained as a stable symbol for callers/tests.
    del mcp_names, normalize_names
    return ""


def _append_droid_mcp_tool_guidance(
    prompt: str,
    *,
    backend: str,
    mcp_names: Sequence[str],
    backend_normalizer: Callable[[str], str] | None = None,
    droid_mcp_tool_guidance_block: Callable[[Sequence[str]], str] | None = None,
) -> str:
    # No-op now: we no longer append any MCP tool guidance for any backend.
    del backend, mcp_names, backend_normalizer, droid_mcp_tool_guidance_block
    return prompt


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


def fixer_provider_orchestration_rule(backend: str | None) -> str:
    normalized = (backend or "").strip().lower()
    if normalized == "agy":
        normalized = "antigravity"
    return _FIXER_PROVIDER_ORCHESTRATION_RULES.get(
        normalized,
        (
            "CRITICAL: Do not use the current provider's built-in subagent, "
            "delegation, scheduling, or task-management features. For all work "
            "that would use those features, use Fixer MCP Netrunner waves."
        ),
    )


def materialize_fixer_provider_prompt(prompt: str, backend: str | None) -> str:
    return prompt.replace(FIXER_PROVIDER_RULE_MARKER, fixer_provider_orchestration_rule(backend))


def _build_fixer_prompt() -> str:
    return textwrap.dedent(
        """
        Activate skill $init-fixer immediately.
        Fixer is the project-scoped orchestrator role in the current Fixer MCP system.
        [FIXER_PROVIDER_ORCHESTRATION_RULE]
        Execute only its initialization checklist first, then stop and report status.
        """
    ).strip()


def _build_unattached_fixer_prompt(scratch_cwd: Path) -> str:
    return textwrap.dedent(
        f"""
        Activate skill $init-unattached-fixer immediately.
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
        Activate skill $init-overseer immediately.
        Overseer is the global analysis and routing role in the current Fixer MCP system.
        Execute only its initialization checklist first, then stop and report status.
        """
    ).strip()
