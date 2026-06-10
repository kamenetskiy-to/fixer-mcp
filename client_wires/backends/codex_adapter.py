from __future__ import annotations

from pathlib import Path
from typing import Any, Mapping, Sequence

from .base import BackendAdapter, BackendDescriptor, FIXER_ROLE_SKILL_NAMES, materialize_codex_project_skills
from .catalog import load_backend_entry


class CodexBackendAdapter(BackendAdapter):
    def __init__(self, inner: Any) -> None:
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
        selected_payload = {name: dict(config) for name, config in selected.items()}
        available_payload = {name: dict(config) for name, config in available.items()}
        # The legacy Codex adapter renders details such as env/cwd from the
        # available-server map. Keep selected authoritative for launch-time
        # mutations like FIXER_DB_PATH and FIXER_MCP_LOCKED_ROLE.
        available_payload.update(selected_payload)
        return list(self._inner.build_mcp_flags(selected_payload, available_payload))

    def build_prompt_args(self, prompt: str) -> list[str]:
        return list(self._inner.build_prompt_args(prompt))

    def prepare_env(self, env: dict[str, str], selection: Any) -> None:
        self._inner.prepare_env(env, selection)

    def build_resume_command(self, option_args: Sequence[str], external_session_id: str) -> list[str]:
        # `codex resume` appears to keep sticky session-side state, which can
        # ignore fresh CLI flags such as `--disable apps`. Use `fork` so the
        # new interactive session inherits the previous context but honors the
        # current launcher configuration.
        return [self.command, "fork", *list(option_args), external_session_id.strip()]

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
        if resolved_model == "gpt-5.5":
            command.extend([
                "-c",
                "model_context_window=800000",
                "-c",
                "model_auto_compact_token_limit=736000",
            ])
        command.append("--dangerously-bypass-approvals-and-sandbox")
        command.extend(self.build_mcp_flags(selected, available))
        command.extend(["exec", "--skip-git-repo-check"])
        if prompt.strip():
            command.append(prompt)
        return command

    def ensure_runtime_files(
        self,
        cwd: Path,
        selection: Any,
        selected: Mapping[str, Mapping[str, object]],
        available: Mapping[str, Mapping[str, object]],
    ) -> None:
        del selection, selected, available
        materialize_codex_project_skills(cwd, FIXER_ROLE_SKILL_NAMES)
