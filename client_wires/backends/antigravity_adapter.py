from __future__ import annotations

import json
import os
import re
from collections.abc import MutableMapping
from pathlib import Path
from typing import Any, Mapping, Sequence

from .base import (
    BackendAdapter,
    BackendDescriptor,
    FIXER_ROLE_SKILL_NAMES,
    materialize_antigravity_workspace_skills,
    normalize_mcp_server_for_antigravity,
)
from .catalog import load_backend_entry


ANTIGRAVITY_MCP_TIMEOUT_SECONDS = 120 * 60
ANTIGRAVITY_MCP_TIMEOUT_ENV = "FIXER_ANTIGRAVITY_MCP_TIMEOUT_SECONDS"
ANTIGRAVITY_FIXER_MCP_SERVER = "fixer_mcp"


class AntigravityBackendAdapter(BackendAdapter):
    def __init__(self) -> None:
        entry = load_backend_entry("antigravity")
        self.descriptor = BackendDescriptor(
            name="antigravity",
            label=str(entry["label"]),
            description=str(entry["description"]),
            default_model=str(entry["default_model"]),
            default_reasoning=str(entry["default_reasoning"]),
            model_options=tuple(str(value) for value in entry["model_options"]),
            reasoning_options=tuple(str(value) for value in entry["reasoning_options"]),
            fresh_launch_supported=bool(entry.get("fresh_launch_supported", True)),
            resume_supported=bool(entry.get("resume_supported", True)),
        )
        self.command = "agy"
        self.supports_resume = self.descriptor.resume_supported

    def build_llm_args(self, selection: Any) -> list[str]:
        return self._build_model_args(
            str(getattr(selection, "model", "") or ""),
            str(getattr(selection, "reasoning_effort", "") or ""),
        )

    def _build_model_args(self, model: str, reasoning: str) -> list[str]:
        model = self._resolve_model_with_reasoning(model, reasoning)
        if model == "default":
            return []
        return ["--model", model]

    def _resolve_model_with_reasoning(self, model: str | None, reasoning: str | None) -> str:
        candidate = (model or "").strip() or self.default_model
        requested_reasoning = (reasoning or "").strip().lower()
        if requested_reasoning in ("", "default"):
            requested_reasoning = ""

        if candidate == "default":
            return candidate

        if candidate in self.model_options and candidate == "default":
            return candidate

        if candidate in _ANTIGRAVITY_CLI_MODEL_OPTIONS:
            label = _antigravity_model_variant_label(candidate)
            if label and label.lower() == "thinking" and requested_reasoning == "high":
                return candidate
            if requested_reasoning and label and requested_reasoning != label.lower():
                raise RuntimeError(
                    f"Antigravity model {candidate!r} already includes reasoning {label!r}, "
                    f"which conflicts with requested reasoning {reasoning!r}."
                )
            return candidate

        base_key = _antigravity_model_key(candidate)
        variants = _antigravity_model_variants(_ANTIGRAVITY_CLI_MODEL_OPTIONS).get(base_key, {})
        if not variants:
            supported = ", ".join((*self.model_options, *_ANTIGRAVITY_CLI_MODEL_OPTIONS))
            raise RuntimeError(
                f"Unsupported model {candidate!r} for backend {self.name!r}. Supported models: {supported}"
            )

        if base_key in {"claudesonnet46", "claudeopus46"} and requested_reasoning:
            requested_reasoning = "thinking"

        if not requested_reasoning:
            if len(variants) == 1:
                return next(iter(variants.values()))
            supported_reasoning = ", ".join(sorted(variants))
            raise RuntimeError(
                f"Antigravity model {candidate!r} requires reasoning to select a concrete CLI model. "
                f"Supported reasoning values for this model: {supported_reasoning}."
            )

        resolved = variants.get(requested_reasoning)
        if resolved is None:
            supported_reasoning = ", ".join(sorted(variants))
            raise RuntimeError(
                f"Unsupported reasoning {reasoning!r} for Antigravity model {candidate!r}. "
                f"Supported reasoning values for this model: {supported_reasoning}."
            )
        return resolved

    def build_execution_args(self, prefs: Any) -> list[str]:
        if bool(getattr(prefs, "dangerous_sandbox", False)) and bool(getattr(prefs, "auto_approve", False)):
            return ["--dangerously-skip-permissions"]
        return []

    def build_resume_command(self, option_args: Sequence[str], external_session_id: str) -> list[str]:
        return [self.command, *list(option_args), "--conversation", external_session_id.strip()]

    def build_prompt_args(self, prompt: str) -> list[str]:
        trimmed = self._build_antigravity_prompt(prompt)
        if not trimmed:
            return []
        return ["--prompt-interactive", trimmed]

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
        command = [
            self.command,
            "--dangerously-skip-permissions",
            "--print-timeout",
            _antigravity_print_timeout(),
            "--output-format",
            "json",
        ]
        command.extend(self._build_model_args(model, reasoning))
        trimmed = self._build_antigravity_prompt(prompt)
        if trimmed:
            command.extend(["--print", trimmed])
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
        command.extend(["--conversation", external_session_id.strip()])
        trimmed = self._build_antigravity_prompt(prompt)
        if trimmed:
            command.extend(["--print", trimmed])
        return command

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
            server_payload = normalize_mcp_server_for_antigravity(source)
            if "serverUrl" in server_payload and not str(server_payload.get("serverUrl", "")).strip():
                continue
            if "command" in server_payload and not str(server_payload.get("command", "")).strip():
                continue
            if name == ANTIGRAVITY_FIXER_MCP_SERVER:
                # Antigravity's MCP client defaults tool calls to a short
                # timeout. Keep the durable Fixer server usable for long
                # research/build operations in both config scopes.
                server_payload["timeoutSeconds"] = _antigravity_mcp_timeout_seconds()
            mcp_servers[name] = server_payload

        agents_dir = cwd / ".agents"
        agents_dir.mkdir(parents=True, exist_ok=True)
        mcp_path = agents_dir / "mcp_config.json"
        mcp_path.write_text(
            json.dumps({"mcpServers": mcp_servers}, indent=2, sort_keys=True) + "\n",
            encoding="utf-8",
        )
        _merge_antigravity_user_mcp_config(mcp_servers)
        materialize_antigravity_workspace_skills(cwd, FIXER_ROLE_SKILL_NAMES)

    @staticmethod
    def _build_antigravity_prompt(prompt: str) -> str:
        trimmed = prompt.strip()
        if not trimmed:
            return ""
        lines = trimmed.splitlines()
        return "\n".join(_antigravity_prompt_line(line) for line in lines).strip()


_CODEX_SKILL_MARKER_RE = re.compile(r"^Activate skill `?\$([a-z0-9][a-z0-9-]*)`? immediately\.$")
_ANTIGRAVITY_MODEL_VARIANT_RE = re.compile(r"^(?P<base>.+?) \((?P<label>[^)]+)\)$")
_ANTIGRAVITY_CLI_MODEL_OPTIONS = (
    "Gemini 3.7 Flash (Medium)",
    "Gemini 3.7 Flash (High)",
    "Gemini 3.7 Flash (Low)",
    "Gemini 3.6 Flash (Medium)",
    "Gemini 3.6 Flash (High)",
    "Gemini 3.6 Flash (Low)",
    "Gemini 3.1 Pro (Low)",
    "Gemini 3.1 Pro (High)",
    "Claude Sonnet 4.6 (Thinking)",
    "Claude Opus 4.6 (Thinking)",
)
_ANTIGRAVITY_MCP_CONFIG_PATH_ENV = "FIXER_ANTIGRAVITY_MCP_CONFIG_PATH"


def _antigravity_user_mcp_config_path() -> Path:
    override = os.environ.get(_ANTIGRAVITY_MCP_CONFIG_PATH_ENV, "").strip()
    if override:
        return Path(override).expanduser()
    return Path.home() / ".gemini" / "config" / "mcp_config.json"


def _merge_antigravity_user_mcp_config(mcp_servers: Mapping[str, Mapping[str, object]]) -> None:
    if not mcp_servers:
        return
    config_path = _antigravity_user_mcp_config_path()
    payload = _read_json_object(config_path)
    existing_raw = payload.get("mcpServers")
    existing_servers: dict[str, object]
    if isinstance(existing_raw, MutableMapping):
        existing_servers = dict(existing_raw)
    else:
        existing_servers = {}
    existing_servers.update({name: dict(server) for name, server in sorted(mcp_servers.items())})
    payload["mcpServers"] = existing_servers
    config_path.parent.mkdir(parents=True, exist_ok=True)
    config_path.write_text(json.dumps(payload, indent=2, sort_keys=True) + "\n", encoding="utf-8")


def _antigravity_mcp_timeout_seconds() -> int:
    raw_value = os.environ.get(ANTIGRAVITY_MCP_TIMEOUT_ENV, "").strip()
    if raw_value:
        try:
            configured = int(raw_value)
        except ValueError:
            configured = 0
        if configured > 0:
            return configured
    return ANTIGRAVITY_MCP_TIMEOUT_SECONDS


def _antigravity_print_timeout() -> str:
    seconds = _antigravity_mcp_timeout_seconds()
    if seconds % 60 == 0:
        return f"{seconds // 60}m"
    return f"{seconds}s"


def _read_json_object(path: Path) -> dict[str, object]:
    try:
        raw = path.read_text(encoding="utf-8")
    except FileNotFoundError:
        return {}
    try:
        value = json.loads(raw)
    except json.JSONDecodeError:
        return {}
    if isinstance(value, dict):
        return dict(value)
    return {}


def _antigravity_model_key(model: str) -> str:
    return re.sub(r"[^a-z0-9]+", "", model.lower())


def normalize_antigravity_model_alias(model: str | None) -> str:
    candidate = (model or "").strip()
    if not candidate:
        return ""
    embedded = _antigravity_model_variant(candidate)
    if embedded is not None:
        return embedded[0]
    bases = _antigravity_model_bases(_ANTIGRAVITY_CLI_MODEL_OPTIONS)
    return bases.get(_antigravity_model_key(candidate), candidate)


def normalize_antigravity_reasoning_alias(model: str | None, reasoning: str | None) -> str:
    candidate = (reasoning or "").strip().lower()
    if candidate and candidate != "default":
        return candidate
    embedded = _antigravity_model_variant(model or "")
    if embedded is not None:
        return embedded[1].lower()
    return candidate or "default"


def _antigravity_model_variant(model: str) -> tuple[str, str] | None:
    candidate = model.strip()
    if not candidate:
        return None
    for option in _ANTIGRAVITY_CLI_MODEL_OPTIONS:
        if candidate != option and _antigravity_model_key(candidate) != _antigravity_model_key(option):
            continue
        match = _ANTIGRAVITY_MODEL_VARIANT_RE.match(option)
        if match:
            return match.group("base"), match.group("label")
    return None


def _antigravity_model_variant_label(model: str) -> str | None:
    match = _ANTIGRAVITY_MODEL_VARIANT_RE.match(model)
    if not match:
        return None
    return match.group("label")


def _antigravity_model_bases(model_options: Sequence[str]) -> dict[str, str]:
    bases: dict[str, str] = {}
    for option in model_options:
        match = _ANTIGRAVITY_MODEL_VARIANT_RE.match(option)
        if not match:
            continue
        base = match.group("base")
        bases.setdefault(_antigravity_model_key(base), base)
    return bases


def _antigravity_model_variants(model_options: Sequence[str]) -> dict[str, dict[str, str]]:
    variants: dict[str, dict[str, str]] = {}
    for option in model_options:
        match = _ANTIGRAVITY_MODEL_VARIANT_RE.match(option)
        if not match:
            continue
        base = match.group("base")
        label = match.group("label").lower()
        variants.setdefault(_antigravity_model_key(base), {})[label] = option
    return variants


def _antigravity_prompt_line(line: str) -> str:
    match = _CODEX_SKILL_MARKER_RE.match(line.strip())
    if match:
        return f"/{match.group(1)}"
    return line
