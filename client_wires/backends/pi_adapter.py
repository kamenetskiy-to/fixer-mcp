from __future__ import annotations

import json
import os
import subprocess
import tempfile
from pathlib import Path
from typing import Any, Callable, Mapping, Sequence

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
    # MiMo 2.6 (Xiaomi). Cross-provider ids stay fully qualified. Generic
    # commandcode routes are the portable default; named provider accounts are
    # discovered dynamically from the Pi agent inventory/config.
    "mimo-v2.6-pro": "commandcode/xiaomi/mimo-v2.6-pro",
    "mimo-v2.6-flash": "commandcode/xiaomi/mimo-v2.6-flash",
    "commandcode/xiaomi/mimo-v2.6-flash": "commandcode/xiaomi/mimo-v2.6-flash",
    "commandcode/xiaomi/mimo-v2.6-pro": "commandcode/xiaomi/mimo-v2.6-pro",
    "opencode-personal/mimo-v2.6-flash": "opencode-personal/mimo-v2.6-flash",
    "opencode-personal/mimo-v2.6-pro": "opencode-personal/mimo-v2.6-pro",
    # Claude Opus 5.5 (OpenCode catalog id `claude-opus-5-5`, declared on the
    # Stas account; also catalogued as anthropic/claude-opus-5.5).
    "opencode-stas/claude-opus-5-5": "opencode-stas/claude-opus-5-5",
}

# Session rows and Fixer-side launch configs written before this adapter existed
# carry the fully qualified spelling for the opencode-go family; `pi --model`
# takes either, so accept both and canonicalize to the catalog id.
PI_MODEL_ALIASES: dict[str, str] = {
    "commandcode/xiaomi/mimo-v2.6-pro": "mimo-v2.6-pro",
    "mimo-v2.6-flash": "commandcode/xiaomi/mimo-v2.6-flash",
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
    # MiMo 2.6 per the pi model-store declarations (models.json override).
    # OpenCode Personal declares low/medium/high only (xhigh/max clamp); the
    # CommandCode accounts send the full low..max ladder.
    "opencode-personal/mimo-v2.6-flash": {
        "minimal": None,
        "low": "low",
        "medium": "medium",
        "high": "high",
        "xhigh": None,
        "max": None,
    },
    "opencode-personal/mimo-v2.6-pro": {
        "minimal": None,
        "low": "low",
        "medium": "medium",
        "high": "high",
        "xhigh": None,
        "max": None,
    },
    "commandcode/xiaomi/mimo-v2.6-flash": {
        "minimal": None,
        "low": "low",
        "medium": "medium",
        "high": "high",
        "xhigh": "xhigh",
        "max": "max",
    },
    "commandcode/xiaomi/mimo-v2.6-pro": {
        "minimal": None,
        "low": "low",
        "medium": "medium",
        "high": "high",
        "xhigh": "xhigh",
        "max": "max",
    },
    "mimo-v2.6-pro": {
        "minimal": None,
        "low": "low",
        "medium": "medium",
        "high": "high",
        "xhigh": "xhigh",
        "max": "max",
    },
    "mimo-v2.6-flash": {
        "minimal": None,
        "low": "low",
        "medium": "medium",
        "high": "high",
        "xhigh": "xhigh",
        "max": "max",
    },
    # Claude Opus 5.5 (opencode-stas): the store declares xhigh/max; absent
    # keys pass through unchanged.
    "opencode-stas/claude-opus-5-5": {
        "xhigh": "xhigh",
        "max": "max",
    },
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

# The `pi-mcp-adapter` extension owns MCP for Pi. It registers the `--mcp-config <path>` extension flag
# (`pi` also installs this extension itself on first launch; Fixer MCP provisions it
# beforehand so a failure cannot surface as a Node stack dump mid-session).
PI_MCP_ADAPTER_PACKAGE = "pi-mcp-adapter"
# pi only loads npm extensions listed in settings.json `packages`; the npm
# prefix alone is not enough (fresh d0lsi host, 2026-09-25: the package
# installed fine yet pi still rejected `--mcp-config` with "Unknown option"
# until this entry existed). Mac/WSL carried the entry from pi's own package
# manager, which is why the gap stayed hidden.
PI_MCP_ADAPTER_SETTINGS_SPEC = "npm:" + PI_MCP_ADAPTER_PACKAGE
PI_ADAPTER_PREPARE_ENV = "FIXER_PI_ADAPTER_AUTOPREPARE"


class PiAdapterInstallError(RuntimeError):
    """The `pi` MCP adapter extension is missing and could not be installed."""


def _pi_agent_dir() -> Path:
    override = os.environ.get("PI_AGENT_HOME", "").strip()
    if override:
        return Path(override).expanduser()
    return Path.home() / ".pi" / "agent"


def _ensure_settings_registration(agent: Path) -> None:
    """Register the installed adapter in pi's settings.json `packages` list.

    Idempotent: writes only when the entry is missing, preserves every other
    settings key, and keeps the file mode. A settings file pi cannot parse is a
    governed error (pi itself would fail on it at startup), never a silent skip.
    """

    settings_path = agent / "settings.json"
    data: dict[str, Any] = {}
    if settings_path.is_file():
        try:
            loaded = json.loads(settings_path.read_text(encoding="utf-8"))
        except (OSError, ValueError) as exc:
            raise PiAdapterInstallError(
                "pi settings.json cannot be read (%s): %s\n"
                "Fix or remove the file, then relaunch." % (exc, settings_path)
            )
        if not isinstance(loaded, dict):
            raise PiAdapterInstallError(
                "pi settings.json is not a JSON object: %s\n"
                "Fix or remove the file, then relaunch." % settings_path
            )
        data = loaded

    packages = data.get("packages") or []
    if not isinstance(packages, list):
        raise PiAdapterInstallError(
            "pi settings.json `packages` is not a list: %s\n"
            "Fix or remove the entry, then relaunch." % settings_path
        )
    if PI_MCP_ADAPTER_SETTINGS_SPEC in packages:
        return

    data["packages"] = [PI_MCP_ADAPTER_SETTINGS_SPEC] + [item for item in packages]
    agent.mkdir(parents=True, exist_ok=True)
    mode = (settings_path.stat().st_mode & 0o777) if settings_path.is_file() else 0o644
    fd, tmp_name = tempfile.mkstemp(dir=str(agent), prefix=".settings.", suffix=".json")
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as handle:
            json.dump(data, handle, indent=2, ensure_ascii=False)
            handle.write("\n")
        os.chmod(tmp_name, mode)
        os.replace(tmp_name, settings_path)
    except BaseException:
        try:
            os.unlink(tmp_name)
        except OSError:
            pass
        raise


def ensure_mcp_adapter_extension(
    *,
    agent_dir: Path | None = None,
    runner: Callable[..., "subprocess.CompletedProcess[str]"] | None = None,
) -> None:
    """Provision the `pi` MCP adapter before launch, with one clean-cache retry.

    `pi` installs its own MCP extension on first launch and treats a failure as
    fatal, which left the operator with a Node stack dump and no MCP (fresh WSL
    host, 2026-09-19: the same install succeeded into /tmp and on the second
    launch). Provisioning here makes it either a no-op (already installed, no npm
    call) or one actionable line; a launch never proceeds silently without MCP.
    Once the npm package is present the adapter is also registered in
    settings.json `packages` - that registration is what makes pi actually
    load the extension and its `--mcp-config` flag.

    Set `FIXER_PI_ADAPTER_AUTOPREPARE=0` to disable (tests, or hosts where the
    extension is managed outside Fixer MCP).
    """

    if os.environ.get(PI_ADAPTER_PREPARE_ENV, "").strip().lower() in {"0", "false", "no", "off"}:
        return

    agent = (agent_dir or _pi_agent_dir()).expanduser()
    npm_prefix = agent / "npm"
    package_json = npm_prefix / "node_modules" / PI_MCP_ADAPTER_PACKAGE / "package.json"
    if package_json.is_file():
        _ensure_settings_registration(agent)
        return

    run = runner or subprocess.run
    log_path = npm_prefix / "pi-mcp-adapter-install.log"
    log_path.parent.mkdir(parents=True, exist_ok=True)
    problems: list[str] = []
    with tempfile.TemporaryDirectory(prefix="pi-adapter-cache-") as cache:
        attempts = [
            ["npm", "install", PI_MCP_ADAPTER_PACKAGE, "--prefix", str(npm_prefix), "--legacy-peer-deps"],
            [
                "npm", "install", PI_MCP_ADAPTER_PACKAGE, "--prefix", str(npm_prefix), "--legacy-peer-deps",
                "--cache", cache, "--prefer-online", "--no-audit", "--no-fund",
            ],
        ]
        for index, command in enumerate(attempts, start=1):
            try:
                completed = run(command, capture_output=True, text=True, check=False)
            except OSError as exc:  # npm missing, permissions, ...
                problems.append("attempt %d: npm could not run (%s)" % (index, exc))
                break
            output = "%s\n%s" % (completed.stdout or "", completed.stderr or "")
            with log_path.open("a", encoding="utf-8") as handle:
                handle.write("$ %s\n%s\n" % (" ".join(command), output))
            if completed.returncode == 0 and package_json.is_file():
                _ensure_settings_registration(agent)
                return
            problems.append("attempt %d: exit %s" % (index, completed.returncode))

    tail = ""
    if log_path.is_file():
        lines = log_path.read_text(encoding="utf-8", errors="replace").splitlines()
        tail = "\n".join(lines[-8:])
    raise PiAdapterInstallError(
        "The pi MCP adapter extension is missing and could not be installed (%s).\n"
        "Run this by hand, then relaunch:\n"
        "  npm install %s --prefix %s --legacy-peer-deps\n"
        "Install log: %s%s"
        % (
            "; ".join(problems) or "unknown failure",
            PI_MCP_ADAPTER_PACKAGE,
            npm_prefix,
            log_path,
            ("\n" + tail) if tail else "",
        )
    )


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


COMMANDCODE_MIMO_THINKING_LEVELS: dict[str, str | None] = {
    "minimal": None,
    "low": "low",
    "medium": "medium",
    "high": "high",
    "xhigh": "xhigh",
    "max": "max",
}


def _configured_pi_commandcode_routes(agent_dir: Path | None = None) -> set[str]:
    """Dynamically discover configured named CommandCode provider routes from Pi models.json.

    Validates actually configured named CommandCode routes (e.g. from team subscriptions,
    H100 routes, or user accounts) from existing Pi inventory without reading or exposing
    auth secrets, credentials, argv, env, or private config values.
    """
    agent = (agent_dir or _pi_agent_dir()).expanduser()
    models_path = agent / "models.json"
    if not models_path.is_file():
        return set()
    try:
        data = json.loads(models_path.read_text(encoding="utf-8"))
    except Exception:
        return set()
    if not isinstance(data, dict):
        return set()
    providers = data.get("providers")
    if not isinstance(providers, dict):
        return set()
    routes: set[str] = set()
    for provider_name, provider_data in providers.items():
        if not isinstance(provider_name, str) or not isinstance(provider_data, dict):
            continue
        p_name = provider_name.strip()
        if not p_name or p_name == "commandcode":
            continue
        is_cc = (
            p_name.startswith("commandcode-")
            or p_name.startswith("cmd-")
            or "commandcode.ai" in str(provider_data.get("baseUrl", ""))
        )
        models = provider_data.get("models")
        has_mimo = False
        if isinstance(models, list):
            for m in models:
                if isinstance(m, dict) and "id" in m:
                    m_id = str(m["id"]).strip()
                    if m_id in ("xiaomi/mimo-v2.6-pro", "xiaomi/mimo-v2.6-flash"):
                        routes.add(f"{p_name}/{m_id}")
                        has_mimo = True
                    elif is_cc:
                        routes.add(f"{p_name}/{m_id}")
        if is_cc or has_mimo:
            routes.add(f"{p_name}/xiaomi/mimo-v2.6-pro")
            routes.add(f"{p_name}/xiaomi/mimo-v2.6-flash")
    return routes


def pi_supported_thinking_levels(model: str) -> tuple[str, ...] | None:
    """Reasoning levels this backend may pass to `pi --thinking` for `model`.

    Returns None when Pi declares no `thinkingLevelMap` for the model, in which
    case Pi forwards whatever level is requested and the adapter has nothing to
    contradict.
    """
    level_map = PI_MODEL_THINKING_LEVELS.get(model)
    if level_map is None:
        if model.endswith("/xiaomi/mimo-v2.6-pro") or model.endswith("/xiaomi/mimo-v2.6-flash") or "mimo-v2.6" in model:
            level_map = COMMANDCODE_MIMO_THINKING_LEVELS
        else:
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

    @property
    def model_options(self) -> tuple[str, ...]:
        base_options = list(super().model_options)
        for route in sorted(_configured_pi_commandcode_routes()):
            if route not in base_options:
                base_options.append(route)
        return tuple(base_options)

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
        ensure_mcp_adapter_extension()
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
