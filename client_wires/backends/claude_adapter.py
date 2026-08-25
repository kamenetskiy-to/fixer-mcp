from __future__ import annotations

import json
import os
import subprocess
from pathlib import Path
from typing import Any, Mapping, Sequence

from .base import (
    FIXER_ROLE_SKILL_NAMES,
    BackendAdapter,
    BackendDescriptor,
    materialize_claude_workspace_skills,
)
from .catalog import load_backend_entry


_OPENCODE_GO_MODELS = {
    "opencode-go/glm-5.2": "glm-5.2",
    "opencode-go/qwen3.8-max": "qwen3.8-max",
}
_KIMI_CLAUDE_MODELS = {
    "kimi/k3": ("k3[1m]", "1048576"),
    "kimi/k3-256k": ("k3-256k", "262144"),
    "kimi/kimi-for-coding": ("kimi-for-coding", "262144"),
    "kimi/kimi-for-coding-highspeed": ("kimi-for-coding-highspeed", "262144"),
}
_OPENCODE_GO_BASE_URL = "https://opencode.ai/zen/go"


def _load_opencode_go_key() -> str:
    value = os.environ.get("OPENCODE_GO_API_KEY", "").strip()
    if value:
        return value
    path = Path.home() / ".codex" / "llm.env"
    try:
        for line in path.read_text(encoding="utf-8").splitlines():
            key, sep, raw = line.partition("=")
            if sep and key.strip() == "OPENCODE_GO_API_KEY":
                return raw.strip().strip('"').strip("'")
    except OSError:
        pass
    return ""


def _load_kimi_key() -> str:
    value = os.environ.get("KIMI_API_KEY", "").strip()
    if value:
        return value
    path = Path.home() / ".codex" / "llm.env"
    try:
        for line in path.read_text(encoding="utf-8").splitlines():
            key, sep, raw = line.partition("=")
            if sep and key.strip() == "KIMI_API_KEY":
                return raw.strip().strip('"').strip("'")
    except OSError:
        pass
    return ""


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
        self._runtime_cwd: Path | None = None

    def _mcp_config_path(self) -> Path:
        return (self._runtime_cwd or Path.cwd()).resolve() / ".mcp.json"

    def build_llm_args(self, selection: Any) -> list[str]:
        model = self.normalize_model(str(getattr(selection, "model", "") or "").strip())
        if model in _KIMI_CLAUDE_MODELS:
            return ["--effort", self.normalize_reasoning(getattr(selection, "reasoning_effort", ""))]
        return ["--model", _OPENCODE_GO_MODELS.get(model, model)]

    def prepare_env(self, env: dict[str, str], selection: Any) -> None:
        model = str(getattr(selection, "model", "") or "").strip()
        if model in _KIMI_CLAUDE_MODELS:
            env_model, context_window = _KIMI_CLAUDE_MODELS[model]
            kimi_key = env.get("KIMI_API_KEY", "").strip() or _load_kimi_key()
            if kimi_key:
                env["KIMI_API_KEY"] = kimi_key
            env["ANTHROPIC_BASE_URL"] = "https://api.kimi.com/coding/"
            env["ANTHROPIC_API_KEY"] = kimi_key
            env["ANTHROPIC_MODEL"] = env_model
            env["ANTHROPIC_DEFAULT_FABLE_MODEL"] = env_model
            env["ANTHROPIC_DEFAULT_OPUS_MODEL"] = env_model
            env["ANTHROPIC_DEFAULT_SONNET_MODEL"] = env_model
            env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = env_model
            env["CLAUDE_CODE_SUBAGENT_MODEL"] = env_model
            env["CLAUDE_CODE_AUTO_COMPACT_WINDOW"] = context_window
            env["CLAUDE_CODE_MAX_CONTEXT_TOKENS"] = context_window
            return
        if model not in _OPENCODE_GO_MODELS:
            return
        key = _load_opencode_go_key()
        if key:
            status = subprocess.run(
                ["routatic-proxy", "status"], stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL, check=False,
            )
            if status.returncode != 0:
                proxy_env = dict(env)
                proxy_env["ROUTATIC_PROXY_API_KEY"] = key
                subprocess.Popen(
                    ["routatic-proxy", "serve", "-b"], env=proxy_env,
                    stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                )
            env["ANTHROPIC_BASE_URL"] = "http://127.0.0.1:3456"
            env["ANTHROPIC_AUTH_TOKEN"] = "unused"
            env["ANTHROPIC_API_KEY"] = ""
            env["CLAUDE_CODE_MAX_CONTEXT_TOKENS"] = "500000"
            env["CLAUDE_CODE_DISABLE_UNKNOWN_MODEL_WINDOW_ENFORCEMENT"] = "1"

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
        del selected, available
        normalized_model = str(model or "").strip() or self.default_model
        command = [
            self.command,
            "--print",
            "--effort",
            self.normalize_reasoning(reasoning),
            "--mcp-config",
            str(self._mcp_config_path()),
            "--strict-mcp-config",
            "--permission-mode",
            "bypassPermissions",
            "--output-format",
            "json",
        ]
        if normalized_model not in _KIMI_CLAUDE_MODELS:
            command[2:2] = ["--model", _OPENCODE_GO_MODELS.get(normalized_model, normalized_model)]
        if prompt.strip():
            command.append(prompt)
        return command

    def build_headless_resume_command(
        self,
        *,
        external_session_id: str,
        model: str,
        reasoning: str,
        selected: Mapping[str, Mapping[str, object]],
        available: Mapping[str, Mapping[str, object]],
        prompt: str,
    ) -> list[str]:
        command = self.build_headless_command(
            model=model,
            reasoning=reasoning,
            selected=selected,
            available=available,
            prompt="",
        )
        command[2:2] = ["--resume", external_session_id.strip()]
        if prompt.strip():
            command.append(prompt.strip())
        return command

    def ensure_runtime_files(
        self,
        cwd: Path,
        selection: Any,
        selected: Mapping[str, Mapping[str, object]],
        available: Mapping[str, Mapping[str, object]],
    ) -> None:
        del selection
        self._runtime_cwd = cwd.resolve()
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
