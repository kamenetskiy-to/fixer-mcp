from __future__ import annotations

import json
import tempfile
from pathlib import Path
from typing import Any, Mapping, Sequence

from .base import (
    BackendAdapter,
    BackendDescriptor,
    materialize_factory_skills,
    normalize_mcp_server_for_factory,
)
from .catalog import load_backend_entry


class DroidBackendAdapter(BackendAdapter):
    LEGACY_MODEL_ALIASES = {
        "custom:qwen/qwen3.6-plus:free": "custom:Qwen3.6-Plus-Free-[OpenRouter]-0",
        "custom:qwen/qwen3.6-plus-preview:free": "custom:Qwen3.6-Plus-Free-[OpenRouter]-0",
        "custom:Qwen3.6-Plus-Preview-Free-[OpenRouter]-0": "custom:Qwen3.6-Plus-Free-[OpenRouter]-0",
    }

    def __init__(self) -> None:
        entry = load_backend_entry("droid")
        self.descriptor = BackendDescriptor(
            name="droid",
            label=str(entry["label"]),
            description=str(entry["description"]),
            default_model=str(entry["default_model"]),
            default_reasoning=str(entry["default_reasoning"]),
            model_options=tuple(str(value) for value in entry["model_options"]),
            reasoning_options=tuple(str(value) for value in entry["reasoning_options"]),
            fresh_launch_supported=bool(entry.get("fresh_launch_supported", True)),
            resume_supported=bool(entry.get("resume_supported", True)),
        )
        self.command = "droid"
        self.supports_resume = True

    def normalize_model(self, model: str | None) -> str:
        candidate = (model or "").strip()
        candidate = self.LEGACY_MODEL_ALIASES.get(candidate, candidate)
        return super().normalize_model(candidate)

    def build_llm_args(self, selection: Any) -> list[str]:
        del selection
        return []

    def build_execution_args(self, prefs: Any) -> list[str]:
        # Used by headless/exec mode
        if bool(getattr(prefs, "dangerous_sandbox", False)) and bool(getattr(prefs, "auto_approve", False)):
            return ["--skip-permissions-unsafe"]
        return []

    def build_interactive_execution_args(self, prefs: Any) -> list[str]:
        # Interactive mode does not support --auto / -m flags.
        # Instead, autonomy and model are set via --settings merge.
        return []

    def _write_launch_settings(
        self,
        selection: Any,
        prefs: Any,
    ) -> Path:
        """Write a temporary settings file for interactive launch."""
        settings: dict[str, object] = {}
        model = str(getattr(selection, "model", "") or "").strip() or self.default_model
        reasoning = str(getattr(selection, "reasoning_effort", "") or "").strip() or self.default_reasoning
        settings["model"] = model
        settings["reasoningEffort"] = reasoning
        auto = bool(getattr(prefs, "dangerous_sandbox", False)) and bool(getattr(prefs, "auto_approve", False))
        settings["autonomyMode"] = "auto-high" if auto else "normal"
        tmp = tempfile.NamedTemporaryFile(
            mode="w", suffix=".json", prefix="droid-wire-", delete=False,
        )
        json.dump(settings, tmp, indent=2)
        tmp.write("\n")
        tmp.close()
        return Path(tmp.name)

    def build_interactive_command_prefix(
        self,
        selection: Any,
        prefs: Any,
    ) -> list[str]:
        """Build the command prefix with --settings for interactive mode."""
        settings_path = self._write_launch_settings(selection, prefs)
        return [self.command, "--settings", str(settings_path)]

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

    def prepare_env(self, env: dict[str, str], selection: Any) -> None:
        del selection
        for key in (
            "ALL_PROXY",
            "all_proxy",
            "HTTP_PROXY",
            "http_proxy",
            "HTTPS_PROXY",
            "https_proxy",
            "NO_PROXY",
            "no_proxy",
        ):
            env.pop(key, None)

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
        del selected, available
        command = [self.command, "exec", "--skip-permissions-unsafe"]
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
        selection: Any,
        selected: Mapping[str, Mapping[str, object]],
        available: Mapping[str, Mapping[str, object]],
    ) -> None:
        settings_path = cwd / ".factory" / "settings.json"
        payload: dict[str, object] = {}
        if settings_path.is_file():
            try:
                payload = json.loads(settings_path.read_text(encoding="utf-8"))
            except json.JSONDecodeError:
                payload = {}

        mcp_servers: dict[str, dict[str, object]] = {}
        for name, config in sorted(selected.items()):
            source = available.get(name, config)
            server_payload = normalize_mcp_server_for_factory(source)
            if server_payload.get("type") == "stdio" and not server_payload.get("command"):
                continue
            if server_payload.get("type") == "http" and not server_payload.get("url"):
                continue
            mcp_servers[name] = server_payload

        payload["sessionDefaultSettings"] = {
            "model": str(getattr(selection, "model", "") or "").strip() or self.default_model,
            "reasoningEffort": str(getattr(selection, "reasoning_effort", "") or "").strip() or self.default_reasoning,
        }
        settings_path.parent.mkdir(parents=True, exist_ok=True)
        settings_path.write_text(json.dumps(payload, indent=2, sort_keys=True) + "\n", encoding="utf-8")
        mcp_path = cwd / ".factory" / "mcp.json"
        mcp_path.write_text(json.dumps({"mcpServers": mcp_servers}, indent=2, sort_keys=True) + "\n", encoding="utf-8")
        materialize_factory_skills(
            cwd,
            (
                "start-fixer",
                "start-overseer",
                "start-netrunner",
                "manual-resolution",
            ),
        )
