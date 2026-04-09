from __future__ import annotations

from typing import Mapping

from ..catalog import load_backend_entry
from .base import BackendAdapter, BackendDescriptor


class CodexBackendAdapter(BackendAdapter):
    def __init__(self) -> None:
        entry = load_backend_entry("codex")
        self.descriptor = BackendDescriptor(
            name="codex",
            label=str(entry["label"]),
            description=str(entry["description"]),
            default_model=str(entry["default_model"]),
            default_reasoning=str(entry["default_reasoning"]),
            model_options=tuple(str(value) for value in entry["model_options"]),
            reasoning_options=tuple(str(value) for value in entry["reasoning_options"]),
            fresh_launch_supported=bool(entry.get("fresh_launch_supported", True)),
            resume_supported=bool(entry.get("resume_supported", True)),
        )

    def build_fresh_command(
        self,
        *,
        model: str,
        reasoning: str,
        prompt: str,
        selected_mcp_servers: Mapping[str, Mapping[str, object]],
    ) -> list[str]:
        command = ["codex", "--model", model]
        if reasoning:
            command.extend(["-c", f'model_reasoning_effort="{reasoning}"'])
        command.append("--dangerously-bypass-approvals-and-sandbox")
        for name in sorted(selected_mcp_servers):
            command.append(f"--mcp={name}")
        command.extend(["exec", "--skip-git-repo-check"])
        if prompt.strip():
            command.append(prompt.strip())
        return command

    def build_resume_command(
        self,
        *,
        external_session_id: str,
        prompt: str,
        selected_mcp_servers: Mapping[str, Mapping[str, object]],
    ) -> list[str]:
        command = ["codex"]
        for name in sorted(selected_mcp_servers):
            command.append(f"--mcp={name}")
        command.extend(["exec", "resume", external_session_id.strip(), "--skip-git-repo-check"])
        if prompt.strip():
            command.append(prompt.strip())
        return command
