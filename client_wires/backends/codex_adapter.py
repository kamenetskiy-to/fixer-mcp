from __future__ import annotations

from typing import Any, Mapping, Sequence

from .base import BackendAdapter, BackendDescriptor


class CodexBackendAdapter(BackendAdapter):
    descriptor = BackendDescriptor(
        name="codex",
        label="codex",
        description="Current Codex CLI flow with explicit MCP flag injection.",
        default_model="gpt-5.4",
        default_reasoning="medium",
        model_options=("gpt-5.4", "gpt-5.3-codex", "gpt-5.3-codex-spark", "gpt-5.2"),
        reasoning_options=("minimal", "low", "medium", "high"),
    )

    def __init__(self, inner: Any) -> None:
        self._inner = inner
        self.command = str(getattr(inner, "command", "codex"))
        self.supports_resume = bool(getattr(inner, "supports_resume", True))

    def build_llm_args(self, selection: Any) -> list[str]:
        return list(self._inner.build_llm_args(selection))

    def build_execution_args(self, prefs: Any) -> list[str]:
        return list(self._inner.build_execution_args(prefs))

    def build_mcp_flags(
        self,
        selected: Mapping[str, Mapping[str, object]],
        available: Mapping[str, Mapping[str, object]],
    ) -> list[str]:
        return list(self._inner.build_mcp_flags(dict(selected), dict(available)))

    def build_prompt_args(self, prompt: str) -> list[str]:
        return list(self._inner.build_prompt_args(prompt))

    def prepare_env(self, env: dict[str, str], selection: Any) -> None:
        self._inner.prepare_env(env, selection)

    def build_resume_command(self, option_args: Sequence[str], external_session_id: str) -> list[str]:
        return [self.command, "resume", *list(option_args), external_session_id.strip()]

    def build_headless_command(
        self,
        *,
        model: str,
        reasoning: str,
        selected: Mapping[str, Mapping[str, object]],
        available: Mapping[str, Mapping[str, object]],
        prompt: str,
    ) -> list[str]:
        resolved_model = model.strip() or self.default_model
        resolved_reasoning = reasoning.strip() or self.default_reasoning
        command = [self.command, "--model", resolved_model]
        if resolved_reasoning:
            command.extend(["-c", f'model_reasoning_effort="{resolved_reasoning}"'])
        command.append("--dangerously-bypass-approvals-and-sandbox")
        command.extend(self.build_mcp_flags(selected, available))
        command.extend(["exec", "--skip-git-repo-check"])
        if prompt.strip():
            command.append(prompt)
        return command
