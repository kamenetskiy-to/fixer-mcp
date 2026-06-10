from __future__ import annotations

import json
import os
import tempfile
from pathlib import Path
from typing import Any, Mapping, Sequence

from .base import (
    BackendAdapter,
    BackendDescriptor,
    FIXER_ROLE_SKILL_NAMES,
    materialize_factory_skills,
    normalize_mcp_server_for_factory,
)
from .catalog import load_backend_entry


DROID_CANONICAL_KIMI_K26_MODEL = "kimi-k2.6"
DROID_CANONICAL_KIMI_K26_INTERNAL_MODEL = "custom:Kimi-K2.6-[Kimi]-0"
DROID_CANONICAL_GLM_51_MODEL = "glm-5.1"
DROID_CANONICAL_GLM_51_INTERNAL_MODEL = "custom:GLM-5.1-[Z.AI]-0"
ZAI_VISION_MCP_SERVER_NAME = "zai-mcp-server"
ZAI_VISION_MCP_PACKAGE = "@z_ai/mcp-server"
ZAI_WEB_SEARCH_MCP_SERVER_NAME = "web-search-prime"
ZAI_WEB_SEARCH_MCP_URL = "https://api.z.ai/api/mcp/web_search_prime/mcp"

DROID_LEGACY_MODEL_ALIASES = {
    "custom:qwen/qwen3.6-plus:free": "OpenRouter Qwen3.6 Plus Free",
    "custom:qwen/qwen3.6-plus-preview:free": "OpenRouter Qwen3.6 Plus Free",
    "custom:qwen3.6-plus-free-[openrouter]-0": "OpenRouter Qwen3.6 Plus Free",
    "custom:qwen3.6-plus-preview-free-[openrouter]-0": "OpenRouter Qwen3.6 Plus Free",
    "openrouter/owl-alpha": "OpenRouter Owl Alpha Free",
    "custom:openrouter/owl-alpha": "OpenRouter Owl Alpha Free",
    "custom:owl-alpha-free-[openrouter]-0": "OpenRouter Owl Alpha Free",
    "kimi": DROID_CANONICAL_KIMI_K26_MODEL,
    "kimi k2.6": DROID_CANONICAL_KIMI_K26_MODEL,
    "kimi-k2.6": DROID_CANONICAL_KIMI_K26_MODEL,
    "kimi k2.6 [kimi]": DROID_CANONICAL_KIMI_K26_MODEL,
    "custom:kimi-k2.6-[kimi]-0": DROID_CANONICAL_KIMI_K26_MODEL,
    "glm-5.1": DROID_CANONICAL_GLM_51_MODEL,
    "z.ai glm-5.1": DROID_CANONICAL_GLM_51_MODEL,
    "z.ai glm 5.1": DROID_CANONICAL_GLM_51_MODEL,
    "custom:glm-5.1-[z.ai]-0": DROID_CANONICAL_GLM_51_MODEL,
    "custom:glm-5-[z.ai]-0": "Z.AI GLM-5",
    "custom:glm-4.7-[z.ai]-0": "Z.AI GLM-4.7",
    "custom:glm-4.5-air-[z.ai]-0": "Z.AI GLM-4.5 Air",
}

DROID_INTERNAL_MODEL_IDS = {
    DROID_CANONICAL_KIMI_K26_MODEL: DROID_CANONICAL_KIMI_K26_INTERNAL_MODEL,
    DROID_CANONICAL_GLM_51_MODEL: DROID_CANONICAL_GLM_51_INTERNAL_MODEL,
    "Z.AI GLM-5": "custom:GLM-5-[Z.AI]-0",
    "Z.AI GLM-4.7": "custom:GLM-4.7-[Z.AI]-0",
    "Z.AI GLM-4.5 Air": "custom:GLM-4.5-air-[Z.AI]-0",
    "OpenRouter Qwen3.6 Plus Free": "custom:Qwen3.6-Plus-Free-[OpenRouter]-0",
    "OpenRouter Owl Alpha Free": "custom:Owl-Alpha-Free-[OpenRouter]-0",
}


def normalize_droid_model_alias(model: str | None) -> str:
    candidate = (model or "").strip()
    if not candidate:
        return candidate
    return DROID_LEGACY_MODEL_ALIASES.get(candidate.casefold(), candidate)


def droid_internal_model_id(model: str | None) -> str:
    public_model = normalize_droid_model_alias(model)
    return DROID_INTERNAL_MODEL_IDS.get(public_model, public_model)


def _resolve_z_ai_api_key(settings_path: Path | None = None) -> str:
    env_value = os.environ.get("Z_AI_API_KEY", "").strip()
    if env_value:
        return env_value

    path = settings_path or (Path.home() / ".factory" / "settings.json")
    try:
        payload = json.loads(path.read_text(encoding="utf-8"))
    except (FileNotFoundError, json.JSONDecodeError, OSError):
        return ""

    custom_models = payload.get("customModels")
    if not isinstance(custom_models, list):
        return ""
    for model in custom_models:
        if not isinstance(model, dict):
            continue
        model_id = str(model.get("id", "")).strip()
        base_url = str(model.get("baseUrl", "")).casefold()
        display_name = str(model.get("displayName", "")).casefold()
        if (
            model_id == DROID_CANONICAL_GLM_51_INTERNAL_MODEL
            or "z.ai" in base_url
            or "z.ai" in display_name
        ):
            api_key = str(model.get("apiKey", "")).strip()
            if api_key:
                return api_key
    return ""


def default_zai_vision_mcp_server(api_key: str | None = None) -> dict[str, object]:
    env = {"Z_AI_MODE": "ZAI"}
    resolved_key = (api_key or _resolve_z_ai_api_key()).strip()
    if resolved_key:
        env["Z_AI_API_KEY"] = resolved_key
    return {
        "type": "stdio",
        "command": "npx",
        "args": ["-y", ZAI_VISION_MCP_PACKAGE],
        "env": env,
    }


def default_zai_web_search_mcp_server(api_key: str | None = None) -> dict[str, object]:
    resolved_key = (api_key or _resolve_z_ai_api_key()).strip()
    payload: dict[str, object] = {
        "type": "http",
        "url": ZAI_WEB_SEARCH_MCP_URL,
        "disabled": False,
    }
    if resolved_key:
        payload["headers"] = {"Authorization": f"Bearer {resolved_key}"}
    return payload


class DroidBackendAdapter(BackendAdapter):
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
        candidate = normalize_droid_model_alias(model)
        return super().normalize_model(candidate)

    def build_llm_args(self, selection: Any) -> list[str]:
        del selection
        return []

    def _resolve_reasoning(self, reasoning: str | None) -> str:
        resolved_reasoning = (reasoning or "").strip() or self.default_reasoning
        if resolved_reasoning in {"", "none"}:
            return "high"
        return resolved_reasoning

    def build_execution_args(self, prefs: Any) -> list[str]:
        # Root `droid` launches do not accept exec-only permission bypass flags.
        del prefs
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
        model = droid_internal_model_id(str(getattr(selection, "model", "") or "").strip() or self.default_model)
        reasoning = self._resolve_reasoning(str(getattr(selection, "reasoning_effort", "") or ""))
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
        resolved_model = droid_internal_model_id(self.normalize_model(model))
        resolved_reasoning = self._resolve_reasoning(reasoning)
        command.extend(["-m", resolved_model, "-r", resolved_reasoning])
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
            # `selected` may carry launch-time env bindings that are not present
            # in the raw registry entry, such as Fixer MCP role/db bindings.
            source = dict(available.get(name, {}))
            source.update(dict(config))
            bearer_token_env_var = source.get("bearer_token_env_var")
            if isinstance(bearer_token_env_var, str) and bearer_token_env_var.strip():
                token = os.environ.get(bearer_token_env_var.strip(), "").strip()
                if token:
                    headers = dict(source.get("headers", {})) if isinstance(source.get("headers"), dict) else {}
                    headers.setdefault("Authorization", f"Bearer {token}")
                    source["headers"] = headers
            server_payload = normalize_mcp_server_for_factory(source)
            if server_payload.get("type") == "stdio" and not server_payload.get("command"):
                continue
            if server_payload.get("type") == "http" and not server_payload.get("url"):
                continue
            mcp_servers[name] = server_payload
        mcp_servers.setdefault(ZAI_VISION_MCP_SERVER_NAME, default_zai_vision_mcp_server())
        mcp_servers.setdefault(ZAI_WEB_SEARCH_MCP_SERVER_NAME, default_zai_web_search_mcp_server())

        payload["model"] = droid_internal_model_id(str(getattr(selection, "model", "") or "").strip() or self.default_model)
        payload["reasoningEffort"] = self._resolve_reasoning(str(getattr(selection, "reasoning_effort", "") or ""))
        session_defaults = payload.get("sessionDefaultSettings")
        if isinstance(session_defaults, dict):
            session_defaults.pop("model", None)
            session_defaults.pop("reasoningEffort", None)
            payload["sessionDefaultSettings"] = session_defaults
        settings_path.parent.mkdir(parents=True, exist_ok=True)
        settings_path.write_text(json.dumps(payload, indent=2, sort_keys=True) + "\n", encoding="utf-8")
        mcp_path = cwd / ".factory" / "mcp.json"
        mcp_path.write_text(json.dumps({"mcpServers": mcp_servers}, indent=2, sort_keys=True) + "\n", encoding="utf-8")
        materialize_factory_skills(cwd, FIXER_ROLE_SKILL_NAMES)
