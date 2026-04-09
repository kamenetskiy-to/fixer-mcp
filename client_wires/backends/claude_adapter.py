from __future__ import annotations

import json
from pathlib import Path
from typing import Any, Mapping, Sequence

from .base import BackendAdapter, BackendDescriptor
from .catalog import load_backend_entry


class ClaudeCodeBackendAdapter(BackendAdapter):
    def __init__(self) -> None:
        entry = load_backend_entry("claude")
        self.descriptor = BackendDescriptor(
            name="claude",
            label=str(entry["label"]),
            description=str(entry["description"]),
            default_model=str(entry["default_model"]),
            default_reasoning=str(entry["default_reasoning"]),
            model_options=tuple(str(value) for value in entry["model_options"]),
            reasoning_options=tuple(str(value) for value in entry["reasoning_options"]),
            fresh_launch_supported=bool(entry.get("fresh_launch_supported", False)),
            resume_supported=bool(entry.get("resume_supported", False)),
        )
        self.command = "claude"
        self.supports_resume = True

    def build_llm_args(self, selection: Any) -> list[str]:
        model = self.normalize_model(str(getattr(selection, "model", "") or "").strip())
        return ["--model", model]

    def build_execution_args(self, prefs: Any) -> list[str]:
        if bool(getattr(prefs, "dangerous_sandbox", False)) and bool(getattr(prefs, "auto_approve", False)):
            return ["--dangerously-skip-permissions"]
        return []

    def build_resume_command(self, option_args: Sequence[str], external_session_id: str) -> list[str]:
        return [self.command, "--resume", external_session_id.strip(), *list(option_args)]

    def build_headless_command(
        self,
        *,
        model: str,
        reasoning: str,
        selected: Mapping[str, Mapping[str, object]],
        available: Mapping[str, Mapping[str, object]],
        prompt: str,
    ) -> list[str]:
        command = [self.command, "--model", self.normalize_model(model), "--dangerously-skip-permissions"]
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
        del selection
        payload: dict[str, object] = {}
        config_path = cwd / ".mcp.json"
        if config_path.is_file():
            try:
                payload = json.loads(config_path.read_text(encoding="utf-8"))
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
        config_path.write_text(json.dumps(payload, indent=2, sort_keys=True) + "\n", encoding="utf-8")
