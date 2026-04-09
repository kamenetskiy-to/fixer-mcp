from __future__ import annotations

import json
from typing import Mapping

from ..catalog import load_backend_entry
from .base import BackendAdapter, BackendDescriptor, normalize_mcp_server_payload


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
            fresh_launch_supported=bool(entry.get("fresh_launch_supported", True)),
            resume_supported=bool(entry.get("resume_supported", False)),
        )

    def build_fresh_command(
        self,
        *,
        model: str,
        reasoning: str,
        prompt: str,
        selected_mcp_servers: Mapping[str, Mapping[str, object]],
    ) -> list[str]:
        del reasoning
        del selected_mcp_servers
        command = ["claude", "--permission-mode", "bypassPermissions"]
        if model:
            command.extend(["--model", model])
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
        del external_session_id
        del prompt
        del selected_mcp_servers
        raise RuntimeError(
            "Backend 'claude' keeps resume metadata sticky by design, but the staged headless resume command is not implemented yet."
        )

    def runtime_side_effects(
        self,
        *,
        mode: str,
        selected_mcp_servers: Mapping[str, Mapping[str, object]],
    ) -> list[str]:
        payload = {
            "mcpServers": {
                name: normalize_mcp_server_payload(config)
                for name, config in sorted(selected_mcp_servers.items())
            }
        }
        return [
            f"{mode} uses layered .mcp.json MCP configuration rather than inline flags",
            f"mcp payload preview: {json.dumps(payload, sort_keys=True)}",
        ]
