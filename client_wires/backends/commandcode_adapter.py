from __future__ import annotations

import json
import shutil
from pathlib import Path
from typing import Any, Mapping, Sequence

from .base import BackendAdapter, BackendDescriptor, FIXER_ROLE_SKILL_NAMES, materialize_codex_project_skills
from .catalog import load_backend_entry

_MODEL_PREFIX = "commandcode/"
_NO_EFFORT_MODELS = {
    "meta/muse-spark-1.2-contributor",
    "minimaxai/minimax-m3",
    "moonshotai/kimi-k3",
    "xiaomi/mimo-v2.5-pro",
    "poolside/laguna-s-2.1-free",
    "laguna-s-2.1-free",
}
_HIGH_MAX_MODELS = {
    "deepseek/deepseek-v4-flash",
    "deepseek/deepseek-v4-flash-vision-exp",
    "deepseek/deepseek-v4-pro",
}
_LOW_HIGH_MAX_MODELS = {"zai-org/glm-5.3-flash"}
_LOW_MEDIUM_XHIGH_MODELS = {"qwen/qwen3.8-27b", "qwen/qwen3.8-max"}


def _commandcode_model_id(model: str) -> str:
    return model[len(_MODEL_PREFIX):] if model.startswith(_MODEL_PREFIX) else model


def _commandcode_supports_effort(model: str) -> bool:
    return _commandcode_model_id(model) not in _NO_EFFORT_MODELS


def commandcode_reasoning_options(model: str) -> tuple[str, ...]:
    model_id = _commandcode_model_id(model)
    if model_id in _NO_EFFORT_MODELS:
        return ()
    if model_id in _HIGH_MAX_MODELS:
        return ("high", "max")
    if model_id in _LOW_HIGH_MAX_MODELS:
        return ("low", "high", "max")
    if model_id in _LOW_MEDIUM_XHIGH_MODELS:
        return ("low", "medium", "xhigh")
    return ("low", "medium", "high")


def _commandcode_effort(model: str, reasoning: str) -> str | None:
    if not _commandcode_supports_effort(model):
        return None
    supported = commandcode_reasoning_options(model)
    if reasoning in supported:
        return reasoning
    if reasoning == "xhigh" and "xhigh" in supported:
        return "xhigh"
    if reasoning == "max" and "max" in supported:
        return "max"
    if "high" in supported:
        return "high"
    if "xhigh" in supported:
        return "xhigh"
    return supported[0]


def _normalize_mcp_server(source: Mapping[str, object]) -> dict[str, object]:
    url = str(source.get("url", source.get("serverUrl", ""))).strip()
    if url:
        payload: dict[str, object] = {
            "transport": "http",
            "url": url,
            "enabled": not bool(source.get("disabled", False)),
        }
        headers = source.get("headers")
        if isinstance(headers, dict) and headers:
            payload["headers"] = {str(key): str(value) for key, value in headers.items()}
        return payload

    payload = {
        "transport": "stdio",
        "command": str(source.get("command", "")).strip(),
        "enabled": not bool(source.get("disabled", False)),
    }
    args = source.get("args")
    if isinstance(args, (list, tuple)):
        payload["args"] = [str(item) for item in args]
    env = source.get("env")
    if isinstance(env, dict) and env:
        payload["env"] = {str(key): str(value) for key, value in env.items()}
    return payload


class CommandCodeBackendAdapter(BackendAdapter):
    """Native Command Code CLI adapter; never routes through Codex."""

    def __init__(self) -> None:
        entry = load_backend_entry("commandcode")
        self.descriptor = BackendDescriptor(
            name="commandcode",
            label=str(entry["label"]),
            description=str(entry["description"]),
            default_model=str(entry["default_model"]),
            default_reasoning=str(entry["default_reasoning"]),
            model_options=tuple(str(value) for value in entry["model_options"]),
            reasoning_options=tuple(str(value) for value in entry["reasoning_options"]),
            fresh_launch_supported=bool(entry.get("fresh_launch_supported", True)),
            resume_supported=bool(entry.get("resume_supported", True)),
        )
        self.command = shutil.which("cmd") or shutil.which("cmdc") or shutil.which("command-code") or "cmd"
        self.supports_resume = self.descriptor.resume_supported
        self._runtime_cwd: Path | None = None
        self._interactive_prompt = ""

    def build_llm_args(self, selection: Any) -> list[str]:
        model = str(getattr(selection, "model", "") or "").strip() or self.default_model
        args = ["--model", _commandcode_model_id(self.normalize_model(model))]
        effort = _commandcode_effort(
            model,
            self.normalize_reasoning(getattr(selection, "reasoning_effort", "")),
        )
        if effort:
            args.extend(["--effort", effort])
        return args

    def build_execution_args(self, prefs: Any) -> list[str]:
        del prefs
        return ["--yolo", "--trust", "--skip-onboarding", "--no-auto-update"]

    def build_mcp_flags(
        self,
        selected: Mapping[str, Mapping[str, object]],
        available: Mapping[str, Mapping[str, object]],
    ) -> list[str]:
        del selected, available
        return []

    def build_prompt_args(self, prompt: str) -> list[str]:
        # Command Code's -p/--print exits after one query; interactive role
        # launches therefore use the launcher's clipboard/bootstrap fallback.
        del prompt
        return []

    def build_interactive_command(self, option_args: Sequence[str], prompt: str) -> list[str]:
        wrapper = Path(__file__).resolve().parents[1] / "commandcode_interactive.exp"
        self._interactive_prompt = prompt
        return ["/usr/bin/expect", str(wrapper), self.command, *list(option_args)]

    def prepare_env(self, env: dict[str, str], selection: Any) -> None:
        del selection
        if self._interactive_prompt:
            env["COMMANDCODE_INITIAL_PROMPT"] = self._interactive_prompt

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
        command = [self.command, "--model", _commandcode_model_id(self.normalize_model(model))]
        effort = _commandcode_effort(model, self.normalize_reasoning(reasoning))
        if effort:
            command.extend(["--effort", effort])
        command.extend([
            "--yolo",
            "--trust",
            "--skip-onboarding",
            "--no-auto-update",
            "--output-format",
            "json",
        ])
        if prompt.strip():
            command.extend(["--print", prompt.strip()])
        else:
            command.append("--print")
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
            prompt=prompt,
        )
        command[1:1] = ["--resume", external_session_id.strip()]
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
        materialize_codex_project_skills(cwd, FIXER_ROLE_SKILL_NAMES)
        config_path = cwd / ".mcp.json"
        payload: dict[str, object] = {}
        if config_path.is_file():
            try:
                loaded = json.loads(config_path.read_text(encoding="utf-8"))
                if isinstance(loaded, dict):
                    payload = loaded
            except (OSError, json.JSONDecodeError):
                payload = {}
        servers = payload.get("mcpServers")
        merged = dict(servers) if isinstance(servers, dict) else {}
        for name, config in selected.items():
            source = dict(available.get(name, {}))
            source.update(dict(config))
            server = _normalize_mcp_server(source)
            if server.get("command") or server.get("url"):
                merged[name] = server
        payload["mcpServers"] = merged
        config_path.write_text(json.dumps(payload, indent=2) + "\n", encoding="utf-8")
