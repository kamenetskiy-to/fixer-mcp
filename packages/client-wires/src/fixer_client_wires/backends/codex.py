from __future__ import annotations

from typing import Mapping

from .base import BackendAdapter, BackendDescriptor


class CodexBackendAdapter(BackendAdapter):
    descriptor = BackendDescriptor(
        name="codex",
        label="Codex CLI",
        description="Explicit MCP flag injection for Codex-based launcher flows.",
        default_model="gpt-5.4",
        default_reasoning="medium",
        model_options=("gpt-5.4", "gpt-5.4-mini", "gpt-5.3-codex", "gpt-5.3-codex-spark", "gpt-5.2"),
        reasoning_options=("low", "medium", "high", "xhigh"),
    )

    def build_headless_command(
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
