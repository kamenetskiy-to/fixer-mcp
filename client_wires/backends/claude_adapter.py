from __future__ import annotations

import json
from pathlib import Path
from typing import Any, Mapping, Sequence

from .base import (
    FIXER_ROLE_SKILL_NAMES,
    BackendAdapter,
    BackendDescriptor,
    materialize_claude_workspace_skills,
)
from .catalog import load_backend_entry


def _positive_int(value: object) -> int | None:
    if isinstance(value, bool) or not isinstance(value, int):
        return None
    return value if value > 0 else None


def _claude_tool_timeout_ms(source: Mapping[str, object]) -> int | None:
    explicit_ms = _positive_int(source.get("per_tool_timeout_ms"))
    if explicit_ms is not None:
        return explicit_ms

    tool_timeout_sec = _positive_int(source.get("tool_timeout_sec"))
    if tool_timeout_sec is not None:
        return tool_timeout_sec * 1000

    timeout_sec = _positive_int(source.get("timeout"))
    if timeout_sec is not None:
        return timeout_sec * 1000

    return None


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
            # `selected` carries launch-time role/db bindings layered over the
            # raw registry entry. Preserve those bindings in project-local MCP.
            source = dict(available.get(name, {}))
            selected_config = dict(config)
            available_env = source.get("env")
            selected_env = selected_config.get("env")
            if isinstance(available_env, dict) or isinstance(selected_env, dict):
                merged_env: dict[object, object] = {}
                if isinstance(available_env, dict):
                    merged_env.update(available_env)
                if isinstance(selected_env, dict):
                    merged_env.update(selected_env)
                selected_config["env"] = merged_env
            source.update(selected_config)
            server_payload: dict[str, object] = {}
            for field in ("command", "args", "env", "transport", "cwd", "startup_timeout_sec"):
                if field in source:
                    server_payload[field] = source[field]
            timeout_ms = _claude_tool_timeout_ms(source)
            if timeout_ms is not None:
                server_payload["timeout"] = timeout_ms
            if server_payload:
                mcp_servers[name] = server_payload

        payload["mcpServers"] = mcp_servers
        config_path.write_text(json.dumps(payload, indent=2, sort_keys=True) + "\n", encoding="utf-8")
        materialize_claude_workspace_skills(cwd, FIXER_ROLE_SKILL_NAMES)
