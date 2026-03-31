from __future__ import annotations

import json
from pathlib import Path
from typing import Any, Mapping, Sequence

from .base import BackendAdapter, BackendDescriptor


class DroidBackendAdapter(BackendAdapter):
    descriptor = BackendDescriptor(
        name="droid",
        label="droid",
        description="Factory Droid CLI flow with project-local MCP settings.",
        default_model="gpt-5.3-codex",
        default_reasoning="medium",
        model_options=("gpt-5.3-codex", "claude-sonnet-4.5", "glm-5"),
        reasoning_options=("off", "low", "medium", "high"),
    )

    def __init__(self) -> None:
        self.command = "droid"
        self.supports_resume = True

    def build_llm_args(self, selection: Any) -> list[str]:
        args = ["exec", "--auto", "high"]
        model = str(getattr(selection, "model", "") or "").strip()
        reasoning = str(getattr(selection, "reasoning_effort", "") or "").strip()
        if model:
            args.extend(["-m", model])
        if reasoning:
            args.extend(["-r", reasoning])
        return args

    def build_execution_args(self, prefs: Any) -> list[str]:
        del prefs
        return []

    def build_mcp_flags(
        self,
        selected: Mapping[str, Mapping[str, object]],
        available: Mapping[str, Mapping[str, object]],
    ) -> list[str]:
        del selected, available
        return []

    def build_prompt_args(self, prompt: str) -> list[str]:
        trimmed = prompt.strip()
        if not trimmed:
            return []
        return [trimmed]

    def build_resume_command(self, option_args: Sequence[str], external_session_id: str) -> list[str]:
        args = list(option_args)
        if args and args[0] == "exec":
            args = args[1:]
        return [self.command, "exec", "--session-id", external_session_id.strip(), *args]

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
        command = [self.command, "exec", "--auto", "high"]
        resolved_model = model.strip() or self.default_model
        resolved_reasoning = reasoning.strip() or self.default_reasoning
        if resolved_model:
            command.extend(["-m", resolved_model])
        if resolved_reasoning:
            command.extend(["-r", resolved_reasoning])
        command.extend(["--output-format", "json"])
        if prompt.strip():
            command.append(prompt)
        return command

    def ensure_runtime_files(
        self,
        cwd: Path,
        selected: Mapping[str, Mapping[str, object]],
        available: Mapping[str, Mapping[str, object]],
    ) -> None:
        payload: dict[str, object] = {}
        settings_path = cwd / ".factory" / "settings.json"
        if settings_path.is_file():
            try:
                payload = json.loads(settings_path.read_text(encoding="utf-8"))
            except json.JSONDecodeError:
                payload = {}

        mcp_servers: dict[str, dict[str, object]] = {}
        for name, config in sorted(selected.items()):
            source = available.get(name, config)
            server_payload: dict[str, object] = {}
            for field in ("command", "args", "env", "transport", "cwd", "startup_timeout_sec", "timeout", "tool_timeout_sec"):
                if field in source:
                    server_payload[field] = source[field]
            if server_payload:
                mcp_servers[name] = server_payload

        payload["mcpServers"] = mcp_servers
        settings_path.parent.mkdir(parents=True, exist_ok=True)
        settings_path.write_text(json.dumps(payload, indent=2, sort_keys=True) + "\n", encoding="utf-8")
