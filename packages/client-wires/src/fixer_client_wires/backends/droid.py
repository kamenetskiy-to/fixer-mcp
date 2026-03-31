from __future__ import annotations

import json
from typing import Mapping

from .base import BackendAdapter, BackendDescriptor


class DroidBackendAdapter(BackendAdapter):
    descriptor = BackendDescriptor(
        name="droid",
        label="Factory Droid CLI",
        description="Project-local settings.json MCP materialization for Droid flows.",
        default_model="gpt-5.3-codex",
        default_reasoning="medium",
        model_options=("gpt-5.3-codex", "claude-sonnet-4.5", "glm-5"),
        reasoning_options=("low", "medium", "high"),
    )

    def build_headless_command(
        self,
        *,
        model: str,
        reasoning: str,
        prompt: str,
        selected_mcp_servers: Mapping[str, Mapping[str, object]],
    ) -> list[str]:
        del selected_mcp_servers
        command = ["droid", "exec", "--auto", "high", "--output-format", "json"]
        if model:
            command.extend(["-m", model])
        if reasoning:
            command.extend(["-r", reasoning])
        if prompt.strip():
            command.append(prompt.strip())
        return command

    def runtime_side_effects(
        self,
        *,
        selected_mcp_servers: Mapping[str, Mapping[str, object]],
    ) -> list[str]:
        payload = {
            "mcpServers": {
                name: dict(config) for name, config in sorted(selected_mcp_servers.items())
            }
        }
        return [
            "writes .factory/settings.json with selected MCP server definitions",
            f"settings payload preview: {json.dumps(payload, sort_keys=True)}",
        ]
