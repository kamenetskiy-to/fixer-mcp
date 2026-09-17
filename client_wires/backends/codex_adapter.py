from __future__ import annotations

import json
import subprocess
import sys
import time
import urllib.request
from datetime import datetime, timedelta, timezone
from functools import lru_cache
from pathlib import Path
from copy import copy
from typing import Any, Mapping, Sequence

from .base import BackendAdapter, BackendDescriptor, FIXER_ROLE_SKILL_NAMES, materialize_codex_project_skills
from .catalog import load_backend_entry
from client_wires.fixer_wire_mcp import _sanitize_mcp_servers_for_provider_config

_FIXER_MCP_SERVER = "fixer_mcp"
_FIXER_GATE_SERVER = "fixer_netrunner_gate"
# The direct gate namespace is a wait-only channel for background-friendly
# wave waits. Launch tools stay on the normally authenticated fixer_mcp
# session so a split stdio process can never hit "requires fixer role" on
# launch; the primary server only hides the wait duplicates owned by the gate.
_FIXER_GATE_WAIT_TOOLS_TOML = '["wait_for_netrunner_wave","wait_for_netrunner_waves"]'
_FIXER_GATE_IDENTITY_ENV_NAMES = (
    "FIXER_DB_PATH",
    "FIXER_MCP_DEFAULT_ROLE",
    "FIXER_MCP_DEFAULT_CWD",
    "FIXER_MCP_LOCKED_ROLE",
)
_FIXER_GATE_PROFILE = "netrunner_gate"
_CODEX_REMOTE_SOCKET = "ws://127.0.0.1:14243"
_DEEPSEEK_MODEL_CATALOG = "__FIXER_DEEPSEEK_MODEL_CATALOG__"
_COMMANDCODE_MODEL_CATALOG = "__FIXER_COMMANDCODE_MODEL_CATALOG__"
_DEEPSEEK_MODEL_CATALOG_PATH = Path(__file__).resolve().parent / "data" / "codex-deepseek-models.json"
_COMMANDCODE_PREFIX = "commandcode/"
_COMMANDCODE_PROXY_PORT = 18088
_COMMANDCODE_PROXY_BASE_URL = f"http://127.0.0.1:{_COMMANDCODE_PROXY_PORT}/v1"
_COMMANDCODE_PROXY_HEALTH_URL = f"http://127.0.0.1:{_COMMANDCODE_PROXY_PORT}/health"
_COMMANDCODE_CHAT_URL = "https://api.commandcode.ai/provider/v1/chat/completions"
_OPENCODE_GO_PROXY_PORT = 18090
_OPENCODE_GO_PROXY_BASE_URL = f"http://127.0.0.1:{_OPENCODE_GO_PROXY_PORT}/v1"
_OPENCODE_GO_PROXY_HEALTH_URL = f"http://127.0.0.1:{_OPENCODE_GO_PROXY_PORT}/health"
_OPENCODE_GO_CHAT_URL = "https://opencode.ai/zen/go/v1/chat/completions"
_ROUTED_MODEL_IDS = {
    "opencode-go/glm-5.3-flash": "glm-5.3-flash",
    "opencode-go/gpt-5.6-luna": "gpt-5.6-luna",
    "opencode-go/deepseek-v4.1-flash": "deepseek-v4.1-flash",
    "opencode-go/deepseek-v4-flash": "deepseek-v4.1-flash",
    "opencode-go/muse-spark-1.2-contributor": "muse-spark-1.2-contributor",
    "commandcode/zai-org/glm-5.3-flash": "zai-org/glm-5.3-flash",
    "commandcode/google/gemini-3.7-flash": "google/gemini-3.7-flash",
    "commandcode/deepseek/deepseek-v4-pro": "deepseek/deepseek-v4-pro",
    "commandcode/gpt-5.6-luna": "gpt-5.6-luna",
    "commandcode/deepseek/deepseek-v4-flash": "deepseek/deepseek-v4-flash",
    "commandcode/xiaomi/mimo-v2.5-pro": "xiaomi/mimo-v2.5-pro",
    "commandcode/meta/muse-spark-1.2-contributor": "meta/muse-spark-1.2-contributor",
    "commandcode/deepseek/deepseek-v4-flash-vision-exp": "deepseek/deepseek-v4-flash-vision-exp",
    "commandcode/gpt-5.6-sol": "gpt-5.6-sol",
    "commandcode/minimaxai/minimax-m3": "minimaxai/minimax-m3",
    "commandcode/moonshotai/kimi-k3": "moonshotai/kimi-k3",
    "commandcode/qwen/qwen3.8-27b": "qwen/qwen3.8-27b",
    "commandcode/qwen/qwen3.8-max": "qwen/qwen3.8-max",
    "commandcode/poolside/laguna-s-2.1-free": "poolside/laguna-s-2.1-free",
}
_DISABLED_FIXER_MODELS = {"opencode-go/deepseek-v4-pro", "deepseek-v4-pro"}
CODEX_MODEL_FAMILIES = ("openai", "opencode-go", "commandcode")
CODEX_MODEL_FAMILY_LABELS = {
    "openai": "OpenAI",
    "opencode-go": "OpenCode Go",
    "commandcode": "CommandCode",
}
CODEX_MODEL_FAMILY_DEFAULTS = {
    "openai": "gpt-5.6-luna",
    "opencode-go": "opencode-go/glm-5.3-flash",
    "commandcode": "commandcode/zai-org/glm-5.3-flash",
}

_COMMANDCODE_TASK_PRICES = {
    "commandcode/google/gemini-3.7-flash": 0.0021,
    "commandcode/gpt-5.6-luna": 0.0014,
    "commandcode/xiaomi/mimo-v2.5-pro": 0.0004,
}
_DEEPSEEK_OFF_PEAK_TASK_PRICES = {
    "opencode-go/deepseek-v4-flash": 0.0009,
    "commandcode/deepseek/deepseek-v4-flash": 0.0009,
    "commandcode/deepseek/deepseek-v4-pro": 0.0028,
}
_TASK_PRICE_LABELS = {
    "commandcode/deepseek/deepseek-v4-flash": "~$0.0009/task",
    "opencode-go/deepseek-v4-flash": "~$0.0009/task",
    "commandcode/deepseek/deepseek-v4-pro": "¢28/task",
    "commandcode/gpt-5.6-luna": "¢14/task",
    "opencode-go/gpt-5.6-luna": "¢14/task",
    "commandcode/google/gemini-3.7-flash": "¢21/task",
    "commandcode/xiaomi/mimo-v2.5-pro": "¢0.4/task",
    "commandcode/poolside/laguna-s-2.1-free": "$0/task",
}


def codex_model_family_for_model(model: str | None) -> str:
    normalized = str(model or "").strip().lower()
    if normalized.startswith(_COMMANDCODE_PREFIX):
        return "commandcode"
    if normalized.startswith("opencode-go/"):
        return "opencode-go"
    return "openai"


def codex_default_model_for_family(family: str) -> str:
    normalized = str(family or "").strip().lower()
    return CODEX_MODEL_FAMILY_DEFAULTS.get(normalized, CODEX_MODEL_FAMILY_DEFAULTS["openai"])


def codex_model_options_for_family(model_options: Sequence[str], family: str) -> tuple[str, ...]:
    normalized = str(family or "").strip().lower()
    if normalized == "commandcode":
        return tuple(option for option in model_options if option.lower().startswith(_COMMANDCODE_PREFIX))
    if normalized == "opencode-go":
        return tuple(option for option in model_options if option.lower().startswith("opencode-go/"))
    return tuple(option for option in model_options if option.lower().startswith("gpt-"))


def codex_model_family_label(family: str) -> str:
    normalized = str(family or "").strip().lower()
    return CODEX_MODEL_FAMILY_LABELS.get(normalized, normalized)


@lru_cache(maxsize=1)
def codex_catalog_reasoning_levels() -> dict[str, tuple[str, ...]]:
    """Reasoning levels declared by the DeepSeek/OpenCode Go model catalog.

    The launcher hands this exact file to the Codex CLI as
    ``model_catalog_json`` for every DeepSeek-family slug, so the file is the
    single source of truth for which levels those models actually accept. An
    unreadable or malformed catalog disables the guard instead of breaking
    unrelated launches: the CLI already fails loudly on a missing catalog path.
    """
    try:
        payload = json.loads(_DEEPSEEK_MODEL_CATALOG_PATH.read_text(encoding="utf-8"))
    except (OSError, ValueError):
        return {}
    models = payload.get("models") if isinstance(payload, Mapping) else None
    if not isinstance(models, Sequence) or isinstance(models, (str, bytes)):
        return {}

    levels: dict[str, tuple[str, ...]] = {}
    for entry in models:
        if not isinstance(entry, Mapping):
            continue
        slug = str(entry.get("slug", "")).strip()
        if not slug:
            continue
        raw_levels = entry.get("supported_reasoning_levels", [])
        if not isinstance(raw_levels, Sequence) or isinstance(raw_levels, (str, bytes)):
            continue
        declared: list[str] = []
        for raw_level in raw_levels:
            if isinstance(raw_level, Mapping):
                effort = str(raw_level.get("effort", "")).strip().lower()
            else:
                effort = str(raw_level).strip().lower()
            if effort and effort not in declared:
                declared.append(effort)
        if declared:
            levels[slug] = tuple(declared)
    return levels


def codex_catalog_supported_reasoning_levels(model: str) -> tuple[str, ...]:
    """Declared reasoning levels for a model id, empty when the catalog is silent.

    ``model`` is the Fixer-facing model id; the CLI receives the routed slug, so
    the lookup follows the same routing map the launch args use.
    """
    catalog_id = _ROUTED_MODEL_IDS.get(str(model or "").strip(), str(model or "").strip())
    return codex_catalog_reasoning_levels().get(catalog_id, ())


def codex_deepseek_price_status(now: datetime | None = None) -> tuple[bool, str]:
    current = (now or datetime.now(timezone.utc)).astimezone(timezone.utc)
    minute = current.hour * 60 + current.minute
    if 240 <= minute < 420:
        peak, transition_minute, next_day = True, 420, False
    elif 540 <= minute < 780:
        peak, transition_minute, next_day = True, 780, False
    elif minute < 240:
        peak, transition_minute, next_day = False, 240, False
    elif minute < 540:
        peak, transition_minute, next_day = False, 540, False
    else:
        peak, transition_minute, next_day = False, 240, True
    transition = current.replace(
        hour=transition_minute // 60,
        minute=transition_minute % 60,
        second=0,
        microsecond=0,
    )
    if next_day:
        transition += timedelta(days=1)
    moscow = transition + timedelta(hours=3)
    next_label = "off-peak starts" if peak else "peak starts"
    status = "PEAK 2x now" if peak else "OFF-PEAK now"
    return peak, (
        f"{status}; {next_label} {transition:%H:%M} UTC / {moscow:%H:%M} MSK"
    )


def codex_model_display_label(model: str, now: datetime | None = None) -> str:
    if model in _DEEPSEEK_OFF_PEAK_TASK_PRICES:
        return f"{_display_model_name(model)} | {_TASK_PRICE_LABELS[model]} | OFF-PEAK now; peaks 04:00-07:00 MSK / 09:00-13:00 MSK"
    if model in _TASK_PRICE_LABELS:
        return f"{_display_model_name(model)} | {_TASK_PRICE_LABELS[model]}"
    if model == "opencode-go/muse-spark-1.2-contributor":
        return f"{_display_model_name(model)} | subscription | MCP unavailable"
    if model == "commandcode/meta/muse-spark-1.2-contributor":
        return f"{_display_model_name(model)} | subscription | effort fixed"
    return f"{_display_model_name(model)} | subscription"


def _display_model_name(model: str) -> str:
    if model.startswith("commandcode/deepseek/"):
        return model.rsplit("/", 1)[-1]
    if model.startswith("commandcode/"):
        slug = model.rsplit("/", 1)[-1]
        return {
            "gpt-5.6-luna": "GPT 5.6 Luna",
            "gpt-5.6-sol": "GPT 5.6 Sol",
            "gemini-3.7-flash": "Gemini 3.7 Flash",
            "mimo-v2.5-pro": "/MiMo v2.5 Pro",
            "muse-spark-1.2-contributor": "Muse Spark 1.2 Contributor",
            "qwen3.8-27b": "Qwen 3.8 27B",
            "qwen3.8-max": "Qwen 3.8 Max",
            "minimax-m3": "MiniMax M3",
            "kimi-k3": "Kimi K3",
            "deepseek-v4-flash-vision-exp": "DeepSeek V4 Flash Vision",
            "deepseek-v4-pro": "Deepseek V4 Pro",
            "deepseek-v4-flash": "deepseek-v4-flash",
            "glm-5.3-flash": "GLM-5.3 Flash",
            "laguna-s-2.1-free": "Laguna S 2.1 (Free)",
        }.get(slug, slug)
    if model.startswith("opencode-go/"):
        return {
            "glm-5.3-flash": "GLM-5.3 Flash",
            "muse-spark-1.2-contributor": "Muse Spark 1.2 Contributor",
        }.get(model.rsplit("/", 1)[-1], model.rsplit("/", 1)[-1])
    return model


def _commandcode_proxy_is_ready() -> bool:
    try:
        with urllib.request.urlopen(_COMMANDCODE_PROXY_HEALTH_URL, timeout=0.4) as response:
            return response.status == 200
    except OSError:
        return False


def _ensure_commandcode_proxy() -> None:
    if _commandcode_proxy_is_ready():
        return
    bridge_script = Path(__file__).resolve().parents[1] / "commandcode_bridge.py"
    subprocess.Popen(
        [
            sys.executable,
            str(bridge_script),
            "--url",
            _COMMANDCODE_CHAT_URL,
            "--port",
            str(_COMMANDCODE_PROXY_PORT),
            "--timeout",
            "300",
        ],
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
        start_new_session=True,
    )
    deadline = time.monotonic() + 120
    while time.monotonic() < deadline:
        if _commandcode_proxy_is_ready():
            return
        time.sleep(0.25)
    raise RuntimeError("CommandCode Responses bridge did not become ready on port 18088.")


def _opencode_go_proxy_is_ready() -> bool:
    try:
        with urllib.request.urlopen(_OPENCODE_GO_PROXY_HEALTH_URL, timeout=0.4) as response:
            return response.status == 200
    except OSError:
        return False


def _ensure_opencode_go_proxy() -> None:
    if _opencode_go_proxy_is_ready():
        return
    bridge_script = Path(__file__).resolve().parents[1] / "commandcode_bridge.py"
    subprocess.Popen(
        [
            sys.executable,
            str(bridge_script),
            "--url",
            _OPENCODE_GO_CHAT_URL,
            "--port",
            str(_OPENCODE_GO_PROXY_PORT),
            "--timeout",
            "300",
        ],
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
        start_new_session=True,
    )
    deadline = time.monotonic() + 120
    while time.monotonic() < deadline:
        if _opencode_go_proxy_is_ready():
            return
        time.sleep(0.25)
    raise RuntimeError("OpenCode Go Responses bridge did not become ready on port 18090.")


def _opencode_go_proxy_args() -> list[str]:
    return [
        "-c",
        f'model_providers.opencode_go.base_url="{_OPENCODE_GO_PROXY_BASE_URL}"',
    ]


class CodexBackendAdapter(BackendAdapter):
    def __init__(self, inner: Any) -> None:
        entry = load_backend_entry("codex")
        self.descriptor = BackendDescriptor(
            name="codex",
            label=str(entry["label"]),
            description=str(entry["description"]),
            default_model=str(entry["default_model"]),
            default_reasoning=str(entry["default_reasoning"]),
            model_options=tuple(str(value) for value in entry["model_options"]),
            reasoning_options=tuple(str(value) for value in entry["reasoning_options"]),
            fresh_launch_supported=bool(entry.get("fresh_launch_supported", True)),
            resume_supported=bool(entry.get("resume_supported", True)),
        )
        self._inner = inner
        self.command = str(getattr(inner, "command", "codex"))
        self.supports_resume = bool(getattr(inner, "supports_resume", True))
        raw_overrides = entry.get("model_config_overrides", {})
        self._model_config_overrides = (
            dict(raw_overrides) if isinstance(raw_overrides, Mapping) else {}
        )

    def build_llm_args(self, selection: Any) -> list[str]:
        model = str(getattr(selection, "model", "") or "").strip() or self.default_model
        if model in _DISABLED_FIXER_MODELS:
            raise RuntimeError("OpenCode Go DeepSeek V4 Pro is disabled for Fixer launches.")
        cli_selection = copy(selection)
        if model in _ROUTED_MODEL_IDS:
            cli_selection.model = _ROUTED_MODEL_IDS[model]
        args = list(self._inner.build_llm_args(cli_selection))
        reasoning = str(getattr(selection, "reasoning_effort", "") or "").strip() or self.default_reasoning
        self._validate_reasoning_for_model(model, reasoning)
        args.extend(self._build_model_config_args(model))
        if model.startswith("opencode-go/"):
            args.extend(_opencode_go_proxy_args())
        return args

    @staticmethod
    def _validate_reasoning_for_model(model: str, reasoning: str) -> None:
        if model == "opencode-go/muse-spark-1.2-contributor" and reasoning in {"max", "ultra"}:
            raise RuntimeError("Codex model 'opencode-go/muse-spark-1.2-contributor' supports reasoning only up to 'xhigh'.")
        if model == "gpt-5.6-luna" and reasoning == "ultra":
            raise RuntimeError("Codex model 'gpt-5.6-luna' does not support reasoning 'ultra'.")
        # DeepSeek-family slugs silently fall back to the model default when
        # `model_reasoning_effort` carries a level the model does not declare,
        # so an unsupported level must fail loudly instead of degrading the
        # worker's reasoning without telling anyone.
        requested = str(reasoning or "").strip().lower()
        supported = codex_catalog_supported_reasoning_levels(model)
        if supported and requested and requested not in supported:
            raise RuntimeError(
                f"Codex model {model!r} does not support reasoning {reasoning!r}; "
                f"declared levels: {', '.join(supported)}."
            )

    def _build_model_config_args(self, model: str) -> list[str]:
        raw_overrides = self._model_config_overrides.get(model, {})
        if not isinstance(raw_overrides, Mapping):
            raise RuntimeError(
                f"Codex config overrides for model {model!r} must be an object"
            )

        args: list[str] = []
        for raw_key, raw_value in raw_overrides.items():
            key = str(raw_key).strip()
            if not key:
                raise RuntimeError(
                    f"Codex config overrides for model {model!r} contain an empty key"
                )
            value = raw_value
            if value == _DEEPSEEK_MODEL_CATALOG:
                value = str(_DEEPSEEK_MODEL_CATALOG_PATH)
            if value == _COMMANDCODE_MODEL_CATALOG:
                value = str(Path(__file__).resolve().parent / "data" / "codex-commandcode-models.json")
            if isinstance(value, str) and value.startswith("~/"):
                value = str(Path.home() / value[2:])
            if isinstance(value, bool):
                encoded_value = "true" if value else "false"
            elif isinstance(value, (int, float)):
                encoded_value = str(value)
            elif isinstance(value, str):
                encoded_value = json.dumps(value)
            else:
                raise RuntimeError(
                    f"Codex config override {key!r} for model {model!r} "
                    "must be a string, number, or boolean"
                )
            args.extend(["-c", f"{key}={encoded_value}"])
        return args

    def build_execution_args(self, prefs: Any) -> list[str]:
        return list(self._inner.build_execution_args(prefs))

    def build_interactive_execution_args(self, prefs: Any) -> list[str]:
        # The dashboard and every interactive Codex client must share the
        # managed app-server. Otherwise the rollout lock permits only one
        # writer and UI sends fail while the TUI is open.
        return ["--remote", _CODEX_REMOTE_SOCKET, *self.build_execution_args(prefs)]

    def build_mcp_flags(
        self,
        selected: Mapping[str, Mapping[str, object]],
        available: Mapping[str, Mapping[str, object]],
    ) -> list[str]:
        selected_payload = {name: dict(config) for name, config in selected.items()}
        available_payload = {name: dict(config) for name, config in available.items()}
        fixer_spec = selected_payload.get(_FIXER_MCP_SERVER)
        gate_enabled = False
        if fixer_spec is not None:
            raw_env = fixer_spec.get("env")
            fixer_env = dict(raw_env) if isinstance(raw_env, dict) else {}
            gate_enabled = (
                fixer_env.get("FIXER_MCP_LOCKED_ROLE") == "fixer"
                and fixer_env.get("FIXER_MCP_DEFAULT_ROLE") == "fixer"
                and bool(str(fixer_env.get("FIXER_MCP_DEFAULT_CWD", "")).strip())
            )
            if gate_enabled:
                gate_spec = dict(fixer_spec)
                raw_args = gate_spec.get("args")
                if isinstance(raw_args, list):
                    gate_args: list[object] = []
                    index = 0
                    while index < len(raw_args):
                        if (
                            raw_args[index] == "-u"
                            and index + 1 < len(raw_args)
                            and raw_args[index + 1] == "FIXER_MCP_TOOL_PROFILE"
                        ):
                            index += 2
                            continue
                        gate_args.append(raw_args[index])
                        index += 1
                    gate_spec["args"] = gate_args
                gate_env = dict(fixer_env)
                gate_env["FIXER_MCP_AUTO_AUTH"] = "1"
                gate_env["FIXER_MCP_TOOL_PROFILE"] = _FIXER_GATE_PROFILE
                gate_spec["env"] = gate_env
                gate_spec["_source"] = "project_mcp"
                selected_payload[_FIXER_GATE_SERVER] = gate_spec
                available_payload[_FIXER_GATE_SERVER] = gate_spec
        selected_payload = _sanitize_mcp_servers_for_provider_config(selected_payload)
        available_payload = _sanitize_mcp_servers_for_provider_config(available_payload)
        if gate_enabled:
            gate_routing_env = {
                name: str(fixer_env[name])
                for name in _FIXER_GATE_IDENTITY_ENV_NAMES
                if str(fixer_env.get(name, "")).strip()
            }
            gate_routing_env.update({
                "FIXER_MCP_AUTO_AUTH": "1",
                "FIXER_MCP_TOOL_PROFILE": _FIXER_GATE_PROFILE,
            })
            selected_payload[_FIXER_GATE_SERVER]["env"] = gate_routing_env
            available_payload[_FIXER_GATE_SERVER]["env"] = gate_routing_env
        # The legacy Codex adapter renders details such as env/cwd from the
        # available-server map. Keep selected authoritative for launch-time
        # mutations like FIXER_DB_PATH and FIXER_MCP_LOCKED_ROLE.
        available_payload.update(selected_payload)
        flags = list(self._inner.build_mcp_flags(selected_payload, available_payload))
        if gate_enabled:
            flags.extend(
                [
                    "-c",
                    f"mcp_servers.{_FIXER_GATE_SERVER}.enabled_tools={_FIXER_GATE_WAIT_TOOLS_TOML}",
                    "-c",
                    f"mcp_servers.{_FIXER_MCP_SERVER}.disabled_tools={_FIXER_GATE_WAIT_TOOLS_TOML}",
                    "-c",
                    f'features.code_mode.direct_only_tool_namespaces=["mcp__{_FIXER_GATE_SERVER}"]',
                ]
            )
        return flags

    def build_prompt_args(self, prompt: str) -> list[str]:
        return list(self._inner.build_prompt_args(prompt))

    def prepare_env(self, env: dict[str, str], selection: Any) -> None:
        self._inner.prepare_env(env, selection)
        model = str(getattr(selection, "model", "") or "").strip()
        if model.startswith("opencode-go/"):
            if not env.get("OPENCODE_GO_API_KEY"):
                raise RuntimeError(
                    "OpenCode Go requires OPENCODE_GO_API_KEY in the shell environment or ~/.codex/llm.env."
                )
            _ensure_opencode_go_proxy()
        elif model.startswith(_COMMANDCODE_PREFIX) and not env.get("COMMANDCODE_API_KEY"):
            raise RuntimeError(
                "CommandCode requires COMMANDCODE_API_KEY in the shell environment or ~/.codex/llm.env."
            )

    def build_resume_command(self, option_args: Sequence[str], external_session_id: str) -> list[str]:
        # Continue the exact stored thread. `fork` creates a new Codex thread,
        # which breaks the app/TUI shared Hands identity.
        return [self.command, *list(option_args), "resume", external_session_id.strip()]

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
        self._validate_reasoning_for_model(resolved_model, resolved_reasoning)
        command = [self.command, "--model", _ROUTED_MODEL_IDS.get(resolved_model, resolved_model)]
        if resolved_reasoning:
            command.extend(["-c", f'model_reasoning_effort="{resolved_reasoning}"'])
        command.extend(self._build_model_config_args(resolved_model))
        if resolved_model.startswith("opencode-go/"):
            command.extend(_opencode_go_proxy_args())
        if resolved_model == "gpt-5.5":
            command.extend([
                "-c",
                "model_context_window=800000",
                "-c",
                "model_auto_compact_token_limit=736000",
            ])
        command.append("--dangerously-bypass-approvals-and-sandbox")
        command.extend(self.build_mcp_flags(selected, available))
        command.extend(["exec", "--skip-git-repo-check"])
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
        exec_index = command.index("exec")
        command[exec_index + 1 : exec_index + 1] = ["resume", external_session_id.strip()]
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
        del selected, available
        model = str(getattr(selection, "model", "") or "").strip()
        if model.startswith("opencode-go/"):
            _ensure_opencode_go_proxy()
        elif model.startswith(_COMMANDCODE_PREFIX):
            _ensure_commandcode_proxy()
        materialize_codex_project_skills(cwd, FIXER_ROLE_SKILL_NAMES)
