"""Prompt builders for autonomous Fixer/Netrunner flows."""

from __future__ import annotations

from pathlib import Path
from typing import Callable


def _implementation_test_discipline_lines() -> list[str]:
    return [
        "Implementation-session execution discipline:",
        "- write the required code changes",
        "- create, update, or remove the relevant automated tests for those changes",
        "- fix older broken tests in scope when they block the task instead of shipping code-only work",
    ]


def _build_autonomous_netrunner_prompt(
    session_id: int,
    mcp_names: list[str],
    fixer_session_id: str,
    mcp_how_to: dict[str, str],
    *,
    default_how_to_fn: Callable[[str], str],
    suppress_autonomous_wake: bool = False,
) -> str:
    mcp_text = ", ".join(mcp_names) if mcp_names else "none"
    how_to_lines: list[str] = []
    for name in mcp_names:
        guidance = mcp_how_to.get(name, default_how_to_fn(name))
        how_to_lines.append(f"- {name}: {guidance}")
    completion_lines = (
        [
            "When the work is finished, submit the mandatory doc proposal and completion report, then stop without waking the Fixer from this worker.",
            "Do not call fixer_mcp.wake_fixer_autonomous for this explicit launch/wait worker; the waiting Fixer's `wait_for_netrunner_session` polling is the completion signal.",
        ]
        if suppress_autonomous_wake
        else [
            "When the work is finished and you have submitted your mandatory doc proposal and completion report, call the fixer_mcp tool `wake_fixer_autonomous` with the completed session id and a concise handoff summary.",
        ]
    )
    fixer_session_lines = (
        [f"Autonomous fixer Codex session ID: `{fixer_session_id}`."]
        if fixer_session_id.strip()
        else []
    )
    lines = [
        "Activate skill `$run-manual-netrunner` immediately.",
        "Use its Netrunner separate-terminal mode for this headless durable worker.",
        "",
        f"Preselected session ID from fixer autonomous flow: `{session_id}`.",
        f"Assigned MCP selection from fixer autonomous flow: {mcp_text}.",
        *fixer_session_lines,
        "Attached MCP how-to guidance:",
        *(how_to_lines or ["- none"]),
        "Architect pre-approved execution for this autonomous run. Do not wait for a manual 'Go'.",
        "After checkout, call `fixer_mcp.log_netrunner_progress` with `log_type=\"started\"`; use `progress`, `blocked`, or `workaround` for meaningful milestones; use `completed` when finished.",
        *_implementation_test_discipline_lines(),
        "If the operator needs an out-of-band status update, use `fixer_mcp.send_operator_telegram_notification`; do not rely on a separate `telegram_notify` MCP for routine Fixer flows.",
        *completion_lines,
        "Use this session ID for checkout unless Architect explicitly overrides.",
    ]
    return "\n".join(lines)


def _build_wave_netrunner_prompt(
    *,
    session_id: int,
    mcp_names: list[str],
    fixer_session_id: str,
    mcp_how_to: dict[str, str],
    wave_id: int,
    wave_worker_id: int,
    branch_name: str,
    worker_cwd: Path,
    declared_write_scope: list[str],
    positive_wave_int_fn: Callable[[str, int], int],
    validate_wave_branch_name_fn: Callable[[str], str],
    default_how_to_fn: Callable[[str], str],
) -> str:
    normalized_wave_id = positive_wave_int_fn("wave_id", wave_id)
    normalized_wave_worker_id = positive_wave_int_fn("wave_worker_id", wave_worker_id)
    mcp_text = ", ".join(mcp_names) if mcp_names else "none"
    scope_text = ", ".join(declared_write_scope) if declared_write_scope else "none"
    how_to_lines: list[str] = []
    for name in mcp_names:
        guidance = mcp_how_to.get(name, default_how_to_fn(name))
        how_to_lines.append(f"- {name}: {guidance}")
    fixer_session_lines = (
        [f"Autonomous fixer Codex session ID: `{fixer_session_id}`."]
        if fixer_session_id.strip()
        else []
    )
    lines = [
        "Activate skill `$run-manual-netrunner` immediately.",
        "Use its Netrunner separate-terminal mode for this headless durable worker.",
        "",
        f"Preselected session ID from fixer autonomous flow: `{session_id}`.",
        f"Assigned MCP selection from fixer autonomous flow: {mcp_text}.",
        *fixer_session_lines,
        "Attached MCP how-to guidance:",
        *(how_to_lines or ["- none"]),
        "Architect pre-approved execution for this autonomous run. Do not wait for a manual 'Go'.",
        "After checkout, call `fixer_mcp.log_netrunner_progress` with `log_type=\"started\"`; use `progress`, `blocked`, or `workaround` for meaningful milestones; use `completed` when finished.",
        "",
        "Parallel Netrunner wave context:",
        f"- wave_id: `{normalized_wave_id}`",
        f"- wave_worker_id: `{normalized_wave_worker_id}`",
        f"- branch_name: `{validate_wave_branch_name_fn(branch_name)}`",
        f"- worker_cwd: `{worker_cwd}`",
        f"- assigned declared_write_scope: {scope_text}",
        "Wave guardrails:",
        "- You are isolated in a Git worktree for this worker.",
        "- Operate only inside the assigned declared_write_scope.",
        "- Do not merge, rebase, remove worktrees, or alter wave state.",
        "- Report changed files in the completion report.",
        "- Do not call fixer_mcp.wake_fixer_autonomous for this wave worker; the future wave-level wait loop resumes the Fixer.",
        *_implementation_test_discipline_lines(),
        "If the operator needs an out-of-band status update, use `fixer_mcp.send_operator_telegram_notification`; do not rely on a separate `telegram_notify` MCP for routine Fixer flows.",
        "When the work is finished, submit the mandatory doc proposal and completion report, then stop without waking the Fixer from this worker.",
        "Use this session ID for checkout unless Architect explicitly overrides.",
    ]
    return "\n".join(lines)


def _build_autonomous_fixer_resume_prompt(completed_session_id: int, summary: str) -> str:
    return "\n".join(
        [
            "Activate skill `$review-netrunner-session` immediately.",
            f"Target completed session ID: `{completed_session_id}`.",
            f"Netrunner handoff summary: {summary or '(none)'}",
            "Review only the named completed session unless the registered autonomous run state explicitly says a serial run is active.",
            "Decide whether the named session is accepted or needs rework, update run state, and avoid launching unrelated pending sessions from this wake.",
            "If the active run state explicitly requires serial follow-up, follow that state; otherwise stop after review/update work.",
            "For implementation sessions, verify the worker changed the relevant automated tests and fixed older broken tests in scope when needed.",
            "Reject code-only implementation deliveries that skipped required automated test work.",
        ]
    )


def _build_overseer_directed_fixer_prompt() -> str:
    return "\n".join(
        [
            "You were invoked by the project Overseer through the durable Overseer/Fixer bridge.",
            "Read the latest project-scoped Overseer/Fixer messages with `fixer_mcp.get_overseer_fixer_messages` before deciding what to do.",
            "Do not ask the human Architect inside this Fixer thread for clarification.",
            "If you can answer, clarify status, or mark the request done immediately, append that response to the Overseer/Fixer chat with `fixer_mcp.append_overseer_fixer_message` using `sender_role='fixer'`.",
            "If implementation or review work is needed, proceed through the normal Fixer orchestration flows that are allowed for this project, and keep the Overseer/Fixer run-state current with `fixer_mcp.set_overseer_fixer_run_state`.",
            "If the request is blocked, append a compact blocker/status message to the Overseer/Fixer chat and mark or update the run-state appropriately.",
            "The Overseer is waiting for the first new Fixer chat message; make that message concise and operational.",
        ]
    )
