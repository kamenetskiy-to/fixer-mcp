from __future__ import annotations

import json
import os
from pathlib import Path
from typing import Mapping

from ..catalog import load_backend_entry
from .base import BackendAdapter, BackendDescriptor, normalize_mcp_server_payload

_DROID_SETTINGS_PATH_ENV = "FIXER_CLIENT_WIRES_DROID_SETTINGS_PATH"


def _droid_settings_path() -> Path:
    override = os.environ.get(_DROID_SETTINGS_PATH_ENV, "").strip()
    if override:
        return Path(override).expanduser()
    return Path.home() / ".factory" / "settings.json"


def _load_custom_models() -> tuple[str | None, tuple[str, ...]]:
    path = _droid_settings_path()
    if not path.is_file():
        return None, ()

    try:
        payload = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return None, ()

    default_model = None
    session_defaults = payload.get("sessionDefaultSettings")
    if isinstance(session_defaults, dict):
        raw_default_model = session_defaults.get("model")
        if isinstance(raw_default_model, str) and raw_default_model.strip():
            default_model = raw_default_model.strip()

    custom_model_ids: list[str] = []
    raw_custom_models = payload.get("customModels")
    if isinstance(raw_custom_models, list):
        for entry in raw_custom_models:
            if not isinstance(entry, dict):
                continue
            raw_model_id = entry.get("id")
            if isinstance(raw_model_id, str) and raw_model_id.strip():
                custom_model_ids.append(raw_model_id.strip())

    return default_model, tuple(custom_model_ids)


class DroidBackendAdapter(BackendAdapter):
    def __init__(self) -> None:
        entry = load_backend_entry("droid")
        configured_default_model, custom_model_ids = _load_custom_models()
        curated_model_options = tuple(str(value) for value in entry["model_options"])
        merged_model_options = tuple(dict.fromkeys((*curated_model_options, *custom_model_ids)))
        default_model = configured_default_model or str(entry["default_model"])
        if default_model not in merged_model_options:
            merged_model_options = (*merged_model_options, default_model)
        self.descriptor = BackendDescriptor(
            name="droid",
            label=str(entry["label"]),
            description=str(entry["description"]),
            default_model=default_model,
            default_reasoning=str(entry["default_reasoning"]),
            model_options=merged_model_options,
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
        del selected_mcp_servers
        command = ["droid", "exec", "--auto", "high", "--output-format", "json"]
        if model:
            command.extend(["-m", model])
        if reasoning:
            command.extend(["-r", reasoning])
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
        del selected_mcp_servers
        command = ["droid", "exec", "-s", external_session_id.strip(), "--auto", "high", "--output-format", "json"]
        if prompt.strip():
            command.append(prompt.strip())
        return command

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
            f"{mode} uses layered .factory/settings.json MCP configuration rather than inline flags",
            f"mcp payload preview: {json.dumps(payload, sort_keys=True)}",
        ]
