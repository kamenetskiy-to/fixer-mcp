from __future__ import annotations

import json
import re
from pathlib import Path
from typing import Any, Mapping, Sequence

from .base import (
    BackendAdapter,
    BackendDescriptor,
    FIXER_ROLE_SKILL_NAMES,
    materialize_antigravity_workspace_skills,
    normalize_mcp_server_for_antigravity,
)
from .catalog import load_backend_entry


class AntigravityBackendAdapter(BackendAdapter):
    def __init__(self) -> None:
        entry = load_backend_entry("antigravity")
        self.descriptor = BackendDescriptor(
            name="antigravity",
            label=str(entry["label"]),
            description=str(entry["description"]),
            default_model=str(entry["default_model"]),
            default_reasoning=str(entry["default_reasoning"]),
            model_options=tuple(str(value) for value in entry["model_options"]),
            reasoning_options=tuple(str(value) for value in entry["reasoning_options"]),
            fresh_launch_supported=bool(entry.get("fresh_launch_supported", True)),
            resume_supported=bool(entry.get("resume_supported", True)),
        )
        self.command = "agy"
        self.supports_resume = self.descriptor.resume_supported

    def build_llm_args(self, selection: Any) -> list[str]:
        return self._build_model_args(
            str(getattr(selection, "model", "") or ""),
            str(getattr(selection, "reasoning_effort", "") or ""),
        )

    def _build_model_args(self, model: str, reasoning: str) -> list[str]:
        model = self.normalize_model(model)
        self.normalize_reasoning(reasoning)
        if model == "default":
            return []
        return ["--model", model]

    def build_execution_args(self, prefs: Any) -> list[str]:
        if bool(getattr(prefs, "dangerous_sandbox", False)) and bool(getattr(prefs, "auto_approve", False)):
            return ["--dangerously-skip-permissions"]
        return []

    def build_resume_command(self, option_args: Sequence[str], external_session_id: str) -> list[str]:
        return [self.command, *list(option_args), "--conversation", external_session_id.strip()]

    def build_prompt_args(self, prompt: str) -> list[str]:
        trimmed = self._build_antigravity_prompt(prompt)
        if not trimmed:
            return []
        return ["--prompt-interactive", trimmed]

    def build_headless_command(
        self,
        *,
        model: str,
        reasoning: str,
        selected: Mapping[str, Mapping[str, object]],
        available: Mapping[str, Mapping[str, object]],
        prompt: str,
    ) -> list[str]:
        del selected, available
        command = [self.command, "--dangerously-skip-permissions"]
        command.extend(self._build_model_args(model, reasoning))
        trimmed = self._build_antigravity_prompt(prompt)
        if trimmed:
            command.extend(["--print", trimmed])
        return command

    def ensure_runtime_files(
        self,
        cwd: Path,
        selection: Any,
        selected: Mapping[str, Mapping[str, object]],
        available: Mapping[str, Mapping[str, object]],
    ) -> None:
        del selection
        mcp_servers: dict[str, dict[str, object]] = {}
        for name, config in sorted(selected.items()):
            source = dict(available.get(name, {}))
            source.update(dict(config))
            server_payload = normalize_mcp_server_for_antigravity(source)
            if "serverUrl" in server_payload and not str(server_payload.get("serverUrl", "")).strip():
                continue
            if "command" in server_payload and not str(server_payload.get("command", "")).strip():
                continue
            mcp_servers[name] = server_payload

        agents_dir = cwd / ".agents"
        agents_dir.mkdir(parents=True, exist_ok=True)
        mcp_path = agents_dir / "mcp_config.json"
        mcp_path.write_text(
            json.dumps({"mcpServers": mcp_servers}, indent=2, sort_keys=True) + "\n",
            encoding="utf-8",
        )
        materialize_antigravity_workspace_skills(cwd, FIXER_ROLE_SKILL_NAMES)

    @staticmethod
    def _build_antigravity_prompt(prompt: str) -> str:
        trimmed = prompt.strip()
        if not trimmed:
            return ""
        lines = trimmed.splitlines()
        return "\n".join(_antigravity_prompt_line(line) for line in lines).strip()


_CODEX_SKILL_MARKER_RE = re.compile(r"^Activate skill `\$([a-z0-9][a-z0-9-]*)` immediately\.$")


def _antigravity_prompt_line(line: str) -> str:
    match = _CODEX_SKILL_MARKER_RE.match(line.strip())
    if match:
        return f"Use the `{match.group(1)}` skill immediately."
    return line
