from __future__ import annotations

import json
from pathlib import Path
from typing import Any, Mapping, Sequence

from .base import (
    BackendAdapter,
    BackendDescriptor,
    FIXER_ROLE_SKILL_NAMES,
    materialize_junie_workspace_skills,
    normalize_mcp_server_for_junie,
)
from .catalog import load_backend_entry


JUNIE_CANONICAL_KIMI_K26_MODEL = "kimi-k2.6"
JUNIE_CANONICAL_GLM_51_MODEL = "glm-5.1"
JUNIE_CUSTOM_MODEL_IDS = {
    JUNIE_CANONICAL_KIMI_K26_MODEL: f"custom:{JUNIE_CANONICAL_KIMI_K26_MODEL}",
    JUNIE_CANONICAL_GLM_51_MODEL: f"custom:{JUNIE_CANONICAL_GLM_51_MODEL}",
}
JUNIE_FIXER_MCP_LOCATION = ".junie/fixer-runtime/mcp"
JUNIE_FIXER_SKILL_LOCATION = ".junie/fixer-runtime/skills"


class JunieBackendAdapter(BackendAdapter):
    def __init__(self) -> None:
        entry = load_backend_entry("junie")
        self.descriptor = BackendDescriptor(
            name="junie",
            label=str(entry["label"]),
            description=str(entry["description"]),
            default_model=str(entry["default_model"]),
            default_reasoning=str(entry["default_reasoning"]),
            model_options=tuple(str(value) for value in entry["model_options"]),
            reasoning_options=tuple(str(value) for value in entry["reasoning_options"]),
            fresh_launch_supported=bool(entry.get("fresh_launch_supported", True)),
            resume_supported=bool(entry.get("resume_supported", True)),
        )
        self.command = "junie"
        self.supports_resume = self.descriptor.resume_supported

    def normalize_model(self, model: str | None) -> str:
        candidate = (model or "").strip().lower()
        if not candidate:
            return JUNIE_CANONICAL_KIMI_K26_MODEL
        if candidate in ("kimi", "kimi k2.6", "kimi-k2.6", "kimi k2.6 [kimi]"):
            return JUNIE_CANONICAL_KIMI_K26_MODEL
        if candidate in ("glm", "glm-5.1", "z.ai glm-5.1", "z.ai glm 5.1"):
            return JUNIE_CANONICAL_GLM_51_MODEL
        return super().normalize_model(candidate)

    def _custom_model_id(self, model: str | None) -> str:
        normalized = self.normalize_model(model)
        return JUNIE_CUSTOM_MODEL_IDS.get(normalized, f"custom:{normalized}")

    def _build_model_args(self, model: str | None, reasoning: str | None) -> list[str]:
        args = [
            "--model",
            self._custom_model_id(model),
            "--model-default-locations",
            "true",
            "--skill-location",
            JUNIE_FIXER_SKILL_LOCATION,
            "--skill-default-locations",
            "false",
            "--mcp-default-locations",
            "false",
        ]
        effort = self.normalize_reasoning(reasoning)
        if effort != "default":
            args.extend(["--effort", effort])
        return args

    def build_llm_args(self, selection: Any) -> list[str]:
        model = str(getattr(selection, "model", "") or "").strip() or self.default_model
        effort = str(getattr(selection, "reasoning_effort", "") or "").strip() or self.default_reasoning
        return self._build_model_args(model, effort)

    def build_execution_args(self, prefs: Any) -> list[str]:
        del prefs
        return []

    def build_mcp_flags(
        self,
        selected: Mapping[str, Mapping[str, object]],
        available: Mapping[str, Mapping[str, object]],
    ) -> list[str]:
        if not selected:
            return []
        return ["--mcp-location", JUNIE_FIXER_MCP_LOCATION]

    def build_resume_command(self, option_args: Sequence[str], external_session_id: str) -> list[str]:
        return [self.command, "--resume", "--session-id", external_session_id.strip(), *list(option_args)]

    def prepare_env(self, env: dict[str, str], selection: Any) -> None:
        del env, selection
        return

    def build_headless_command(
        self,
        *,
        model: str,
        reasoning: str,
        selected: Mapping[str, Mapping[str, object]],
        available: Mapping[str, Mapping[str, object]],
        prompt: str,
    ) -> list[str]:
        cmd = [self.command, *self._build_model_args(model, reasoning)]
        if selected:
            cmd.extend(["--mcp-location", JUNIE_FIXER_MCP_LOCATION])

        cmd.extend(["--output-format", "json"])

        if prompt.strip():
            cmd.extend(["--task", prompt.strip()])

        return cmd

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
            server_payload = normalize_mcp_server_for_junie(source)
            if "url" in server_payload and not str(server_payload.get("url", "")).strip():
                continue
            if "command" in server_payload and not str(server_payload.get("command", "")).strip():
                continue
            mcp_servers[name] = server_payload

        mcp_dir = cwd / ".junie" / "fixer-runtime" / "mcp"
        mcp_dir.mkdir(parents=True, exist_ok=True)
        mcp_path = mcp_dir / "mcp.json"
        mcp_path.write_text(
            json.dumps({"mcpServers": mcp_servers}, indent=2, sort_keys=True) + "\n",
            encoding="utf-8",
        )

        materialize_junie_workspace_skills(cwd, FIXER_ROLE_SKILL_NAMES)
