from __future__ import annotations

import json
from pathlib import Path
from typing import Any, Mapping, Sequence

from .base import (
    CANONICAL_SKILLS_RELATIVE_ROOT,
    FIXER_ROLE_SKILL_NAMES,
    BackendAdapter,
    BackendDescriptor,
)
from .catalog import load_backend_entry
from client_wires.fixer_wire_mcp import _sanitize_mcp_server_for_provider_config

# Pi is the Fixer MCP base harness: one CLI in front of the Architect's
# OpenAI / OpenCode Go / CommandCode / Kimi Coding subscriptions.
#
# Everything in this module was verified against the real binary
# (`pi` 0.85.1) and its installed `pi-mcp-adapter` extension (2.33.0), not
# against documentation alone. The probes behind the non-obvious choices:
#
#   pi --list-models                                  -> provider/model ids
#   pi -p "<list servers>" (PI_MCP_CONFIG_MODE=exclusive --mcp-config <file>)
#                                                     -> only the injected server
#   pi -p "<call probe tool>"                         -> real tool round trip
#   pi --session-id <id> -p "<x>" twice               -> resumed, recalled state
#   pi --thinking low (deepseek-v4.1-flash)           -> exits 0, silently ran
#                                                        at `high` (see below)
PI_COMMAND = "pi"

# Pi's default provider for this backend. Bare catalog model ids below belong to
# it; cross-provider ids stay fully qualified because a bare id would be
# ambiguous in `pi --list-models` output.
PI_DEFAULT_PROVIDER = "opencode-go"

# Public catalog model ids -> the exact `provider/model` id `pi --model` takes.
# Only models verified present in `pi --list-models` on the real binary.
PI_MODEL_INTERNAL_IDS: dict[str, str] = {
    "deepseek-v4.1-flash": "opencode-go/deepseek-v4.1-flash",
    "deepseek-v4-flash": "opencode-go/deepseek-v4-flash",
    "deepseek-v4-flash-vision-exp": "opencode-go/deepseek-v4-flash-vision-exp",
    "deepseek-v4-pro": "opencode-go/deepseek-v4-pro",
    "commandcode/deepseek/deepseek-v4-flash": "commandcode/deepseek/deepseek-v4-flash",
    "openai-codex/gpt-5.3-codex-spark": "openai-codex/gpt-5.3-codex-spark",
    "openai-codex/gpt-5.6-luna": "openai-codex/gpt-5.6-luna",
    "kimi-coding/k3": "kimi-coding/k3",
}

# Session rows and Fixer-side launch configs written before this adapter existed
# carry the fully qualified spelling for the opencode-go family; `pi --model`
# takes either, so accept both and canonicalize to the catalog id.
PI_MODEL_ALIASES: dict[str, str] = {
    "opencode-go/deepseek-v4.1-flash": "deepseek-v4.1-flash",
    "opencode-go/deepseek-v4-flash": "deepseek-v4-flash",
    "opencode-go/deepseek-v4-flash-vision-exp": "deepseek-v4-flash-vision-exp",
    "opencode-go/deepseek-v4-pro": "deepseek-v4-pro",
}

PI_REASONING_OPTIONS = ("low", "high", "max")
PI_DEFAULT_REASONING = "high"

# `thinkingLevelMap` as Pi itself declares it per model in
# `<agent dir>/models-store.json`. Pi's own clampThinkingLevel() silently
# PROMOTES a level the model does not declare: requesting `low` on
# `opencode-go/deepseek-v4.1-flash` exits 0 and runs the whole session at
# `high`. A worker that asked for one reasoning level and silently got another
# is the exact failure wave 819 closed on the Codex backend, so Pi refuses
# loudly instead of trusting the clamp.
#
# Semantics copied from Pi's getSupportedThinkingLevels():
#   * a level mapped to `null` is unsupported;
#   * a level absent from the map is passed through unchanged, except for
#     `xhigh`/`max`, which must be declared explicitly to count as supported;
#   * a model with no map at all passes every level through.
PI_MODEL_THINKING_LEVELS: dict[str, dict[str, str | None]] = {
    "deepseek-v4.1-flash": {
        "minimal": None,
        "low": None,
        "medium": None,
        "high": "high",
        "max": "max",
    },
    "deepseek-v4-flash": {
        "minimal": None,
        "low": "low",
        "medium": None,
        "high": "high",
        "max": "max",
    },
    "deepseek-v4-flash-vision-exp": {
        "minimal": None,
        "low": "low",
        "medium": None,
        "high": "high",
        "max": "max",
    },
    "deepseek-v4-pro": {
        "minimal": None,
        "low": None,
        "medium": None,
        "high": "high",
        "max": "max",
    },
    # No `max` key at all: Pi's own model store declares only xhigh/minimal for
    # this model, so `max` is not a supported level for it.
    "openai-codex/gpt-5.3-codex-spark": {
        "xhigh": "xhigh",
        "minimal": "low",
    },
    # `thinkingLevelMap` as `openai-codex` declares it in models-store.json
    # (probed 2026-09-17): xhigh and max are real levels, `minimal` aliases to
    # `low`. `low`, `medium` and `high` are absent from the map, so Pi passes
    # them through unchanged - which is exactly what keeps `high` sendable for
    # a Pi Hands lane on this model.
    "openai-codex/gpt-5.6-luna": {
        "xhigh": "xhigh",
        "max": "max",
        "minimal": "low",
    },
    "kimi-coding/k3": {
        "off": None,
        "minimal": None,
        "low": "low",
        "medium": None,
        "high": "high",
        "xhigh": None,
        "max": "max",
    },
}

# The `pi-mcp-adapter` extension owns MCP for Pi. It registers the
# `--mcp-config <path>` extension flag and reads the same `mcpServers` shape as
# every other MCP client, plus a few Pi-only fields (`directTools`,
# `requestTimeoutMs`, `disabled`). `process.argv` is scanned for the flag before
# flag registration, so the path works in every launch mode.
#
# PI_MCP_CONFIG_MODE=exclusive collapses Pi's whole precedence chain
# (`~/.config/mcp/mcp.json`, `~/.agents/mcp.json`, `<agent dir>/mcp.json`,
# project `.mcp.json`, project `.pi/mcp.json`, package-provided servers) down to
# the single `--mcp-config` file. Without it, a Pi worker started in this repo
# would also inherit the repo's own `.mcp.json` (a FIXER-locked `fixer_mcp`) and
# the operator's global servers, which is neither isolated nor deterministic.
PI_MCP_CONFIG_MODE_ENV = "PI_MCP_CONFIG_MODE"
PI_MCP_EXCLUSIVE_MODE = "exclusive"

# Gitignored project-local runtime state (see `.gitignore`: `.run/`). Pi resolves
# the flag value against its own process cwd, which every launcher sets to the
# launch/worktree root - the same directory ensure_runtime_files() writes into.
# A cwd-relative path is therefore correct even in flows that build argv before
# ensure_runtime_files() runs (the interactive role launch does exactly that).
PI_MCP_CONFIG_RELATIVE_PATH = ".run/pi/mcp.json"

# Per-server fields passed straight through to the extension. Pi ignores the
# launcher's own bookkeeping keys (`transport`, `startup_timeout_sec`, `_source`)
# and infers stdio vs HTTP from `command` vs `url`.
#
# `env` is deliberately NOT here: the canonical "Launcher MCP Secret and
# Artifact Hygiene" contract forbids serializing MCP server environment into
# generated provider config files. The launcher injects the selected servers'
# env into the provider process (`_bind_mcp_server_env_to_launch_env`) and a Pi
# stdio server inherits that process environment, so the config only needs
# `inheritEnv`.
_PI_MCP_SERVER_FIELDS = (
    "command",
    "args",
    "cwd",
    "url",
    "headers",
    "auth",
    "bearerToken",
    "bearerTokenEnv",
)


def _positive_int(value: object) -> int | None:
    if isinstance(value, bool) or not isinstance(value, (int, float, str)):
        return None
    try:
        parsed = int(str(value).strip())
    except (TypeError, ValueError):
        return None
    return parsed if parsed > 0 else None


def _pi_request_timeout_ms(source: Mapping[str, object]) -> int | None:
    """Launcher MCP timeout (seconds) -> Pi's per-server `requestTimeoutMs`.

    The launcher expresses MCP timeouts in seconds; the extension wants
    milliseconds. Same convention and same precedence as the Claude adapter:
    tool timeout wins over the generic timeout.
    """
    for key in ("tool_timeout_sec", "timeout"):
        seconds = _positive_int(source.get(key))
        if seconds is not None:
            return seconds * 1000
    return None


def normalize_mcp_server_for_pi(source: Mapping[str, object]) -> dict[str, object] | None:
    """Translate a launcher MCP server spec into a Pi `mcpServers` entry."""
    # Same structural sanitizer Codex and Antigravity run their specs through:
    # runtime env bindings must never reach a provider-owned config artifact.
    sanitized = _sanitize_mcp_server_for_provider_config(source)
    payload: dict[str, object] = {}
    for field in _PI_MCP_SERVER_FIELDS:
        value = sanitized.get(field)
        if value is None:
            continue
        if field in ("args",):
            if isinstance(value, (list, tuple)):
                payload[field] = [str(item) for item in value]
            continue
        if field in ("env", "headers"):
            if isinstance(value, dict) and value:
                payload[field] = {str(key): str(item) for key, item in value.items()}
            continue
        text = str(value).strip()
        if text:
            payload[field] = text

    has_url = "url" in payload
    has_command = "command" in payload
    if not has_url and not has_command:
        return None

    # `disabled` is the extension's real per-server opt-out flag; anything
    # reaching this function was already selected by the operator/Fixer, so the
    # launcher's incidental registry fields (e.g. `enabled`) are not consulted.
    payload["disabled"] = bool(sanitized.get("disabled", False))

    timeout_ms = _pi_request_timeout_ms(sanitized)
    if timeout_ms is not None:
        # Fixer MCP's long calls (wave waits, autopilot steps) must not die on
        # the MCP SDK default request timeout.
        payload["requestTimeoutMs"] = timeout_ms

    if has_command:
        # Explicit rather than relying on the extension's default, so the launch
        # env contract above cannot silently change underneath us.
        payload["inheritEnv"] = True

    raw_direct_tools = sanitized.get("directTools", sanitized.get("direct_tools"))
    payload["directTools"] = raw_direct_tools if isinstance(raw_direct_tools, bool) else True
    return payload


def pi_supported_thinking_levels(model: str) -> tuple[str, ...] | None:
    """Reasoning levels this backend may pass to `pi --thinking` for `model`.

    Returns None when Pi declares no `thinkingLevelMap` for the model, in which
    case Pi forwards whatever level is requested and the adapter has nothing to
    contradict.
    """
    level_map = PI_MODEL_THINKING_LEVELS.get(model)
    if level_map is None:
        return None
    supported: list[str] = []
    for level in PI_REASONING_OPTIONS:
        if level not in level_map:
            # Absent key: Pi passes the level through, except for xhigh/max
            # which only count as supported when the model declares them.
            if level not in ("xhigh", "max"):
                supported.append(level)
            continue
        if level_map[level] is not None:
            supported.append(level)
    return tuple(supported)


class PiBackendAdapter(BackendAdapter):
    """Adapter for the Pi coding agent CLI (`pi`, v0.85.x).

    Skills: Pi only loads project `.agents/skills` for trusted projects and
    would otherwise also pull in global/package skills, so the adapter disables
    discovery with `--no-skills` and passes every Fixer role skill explicitly
    with a repeatable `--skill <dir>` (verified additive under `--no-skills`,
    with and without project trust).

    MCP: delegated to the installed `pi-mcp-adapter` extension. The adapter
    writes a project-scoped config to the gitignored `.run/pi/mcp.json`, points
    the extension at it with `--mcp-config`, and forces exclusive config mode so
    no ambient server leaks into the launch.
    """

    def __init__(self) -> None:
        entry = load_backend_entry("pi")
        self.descriptor = BackendDescriptor(
            name="pi",
            label=str(entry["label"]),
            description=str(entry["description"]),
            default_model=str(entry["default_model"]),
            default_reasoning=str(entry["default_reasoning"]),
            model_options=tuple(str(value) for value in entry["model_options"]),
            reasoning_options=tuple(str(value) for value in entry["reasoning_options"]),
            fresh_launch_supported=bool(entry.get("fresh_launch_supported", True)),
            resume_supported=bool(entry.get("resume_supported", True)),
        )
        self.command = PI_COMMAND
        self.supports_resume = self.descriptor.resume_supported

    # ------------------------------------------------------------------ models

    def normalize_model(self, model: str | None) -> str:
        candidate = (model or "").strip()
        if not candidate:
            return self.default_model
        alias = PI_MODEL_ALIASES.get(candidate)
        if alias is not None:
            return alias
        return super().normalize_model(candidate)

    def _internal_model_id(self, model: str) -> str:
        normalized = self.normalize_model(model)
        return PI_MODEL_INTERNAL_IDS.get(normalized, normalized)

    # --------------------------------------------------------------- reasoning

    def _validate_reasoning_for_model(self, model: str, reasoning: str) -> None:
        normalized_model = self.normalize_model(model)
        declared = pi_supported_thinking_levels(normalized_model)
        if declared is None:
            return
        if reasoning not in declared:
            raise RuntimeError(
                f"Pi model {normalized_model!r} does not support reasoning {reasoning!r}; "
                f"declared levels: {', '.join(declared) if declared else '(none)'}. "
                "Pi would otherwise silently clamp the level instead of failing."
            )

    def _reasoning_args(self, model: str, reasoning: str) -> list[str]:
        effort = self.normalize_reasoning(reasoning)
        self._validate_reasoning_for_model(model, effort)
        return ["--thinking", effort]

    @staticmethod
    def _resolved_model(selection: Any, default: str) -> str:
        return str(getattr(selection, "model", "") or "").strip() or default

    # ------------------------------------------------------------------ skills

    @staticmethod
    def _repo_root() -> Path:
        return Path(__file__).resolve().parents[2]

    def _skill_dirs(self) -> list[Path]:
        """Canonical Fixer role skills, in canonical order."""
        root = self._repo_root() / CANONICAL_SKILLS_RELATIVE_ROOT
        return [
            root / name
            for name in FIXER_ROLE_SKILL_NAMES
            if (root / name / "SKILL.md").is_file()
        ]

    def _skill_scope_args(self) -> list[str]:
        # `--no-skills` disables Pi's global/project/package skill discovery;
        # explicit `--skill` paths stay additive on top of it.
        args = ["--no-skills"]
        for skill_dir in self._skill_dirs():
            args.extend(["--skill", str(skill_dir)])
        return args

    # --------------------------------------------------------------------- MCP

    def _mcp_config_path(self, cwd: Path) -> Path:
        return cwd / PI_MCP_CONFIG_RELATIVE_PATH

    def build_llm_args(self, selection: Any) -> list[str]:
        model = self._resolved_model(selection, self.default_model)
        reasoning = str(getattr(selection, "reasoning_effort", "") or "").strip() or self.default_reasoning
        return [
            "--model",
            self._internal_model_id(model),
            *self._reasoning_args(model, reasoning),
        ]

    def build_execution_args(self, prefs: Any) -> list[str]:
        # `--approve` is Pi's project-trust grant for one run: it is what stops
        # the TUI from blocking on a trust prompt for a repository that carries
        # project-local resources. Pi has no separate tool-approval flag.
        trust_args = ["--approve"] if bool(getattr(prefs, "auto_approve", False)) else []
        return [*trust_args, *self._skill_scope_args()]

    def build_mcp_flags(
        self,
        selected: Mapping[str, Mapping[str, object]],
        available: Mapping[str, Mapping[str, object]],
    ) -> list[str]:
        del available
        if not selected:
            return []
        # The config body is written by ensure_runtime_files(); Pi resolves the
        # value against its own process cwd, which is the launch directory.
        return ["--mcp-config", PI_MCP_CONFIG_RELATIVE_PATH]

    def prepare_env(self, env: dict[str, str], selection: Any) -> None:
        del selection
        # Exclusive mode is what keeps `--mcp-config` authoritative. See the
        # PI_MCP_CONFIG_MODE_ENV comment above.
        env[PI_MCP_CONFIG_MODE_ENV] = PI_MCP_EXCLUSIVE_MODE

    def build_resume_command(self, option_args: Sequence[str], external_session_id: str) -> list[str]:
        # `--session-id` is Pi's exact project-session id: it resumes the session
        # when it exists and creates it when it does not, so this one flag covers
        # both resume and fresh-with-a-known-id.
        return [self.command, *list(option_args), "--session-id", external_session_id.strip()]

    def build_headless_command(
        self,
        *,
        model: str,
        reasoning: str,
        selected: Mapping[str, Mapping[str, object]],
        available: Mapping[str, Mapping[str, object]],
        prompt: str,
    ) -> list[str]:
        resolved_model = model.strip() or self.default_model
        resolved_reasoning = reasoning.strip() or self.default_reasoning
        command = [
            self.command,
            # Non-interactive runs never show a trust prompt; `--no-approve`
            # makes project-local settings/extensions a deterministic non-input
            # instead of depending on the machine's saved trust decisions.
            "--no-approve",
            *self._skill_scope_args(),
            "--model",
            self._internal_model_id(resolved_model),
            *self._reasoning_args(resolved_model, resolved_reasoning),
            *self.build_mcp_flags(selected, available),
        ]
        if prompt.strip():
            command.extend(["-p", prompt.strip()])
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
        command[1:1] = ["--session-id", external_session_id.strip()]
        if prompt.strip():
            command.extend(["-p", prompt.strip()])
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
            server_payload = normalize_mcp_server_for_pi(source)
            if server_payload is None:
                continue
            mcp_servers[name] = server_payload

        config_path = self._mcp_config_path(cwd)
        config_path.parent.mkdir(parents=True, exist_ok=True)
        config_path.write_text(
            json.dumps({"mcpServers": mcp_servers}, indent=2, sort_keys=True) + "\n",
            encoding="utf-8",
        )
