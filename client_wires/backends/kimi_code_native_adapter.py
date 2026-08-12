from __future__ import annotations

import json
from pathlib import Path
from typing import Any, Mapping, Sequence

from .base import (
    FIXER_ROLE_SKILL_NAMES,
    BackendAdapter,
    BackendDescriptor,
    materialize_kimi_code_workspace_skills,
)
from .catalog import load_backend_entry
from .manifest import load_manifest

# Public alias surfaced in the catalog / launcher.
KIMI_CODE_NATIVE_PUBLIC_MODEL = "kimi-for-coding"
# Internal model id understood by the native `kimi` binary (-m).
KIMI_CODE_NATIVE_INTERNAL_MODEL = "kimi-code/kimi-for-coding"

# Documented project-scoped MCP config path for the native Kimi Code binary
# (https://moonshotai.github.io/kimi-code/en/customization/mcp.html). Unlike
# kimi-cli, the native binary has no --mcp-config-file-style flag to point at
# an arbitrary scoped file: `.kimi-code/mcp.json` under the project cwd is the
# only project-local location it auto-discovers, so the adapter must write
# there directly. This CAN collide with a config the operator maintains by
# hand for their own interactive sessions in the same repo; that is a known
# limitation of the project-config strategy, not an oversight.
_KIMI_NATIVE_MCP_CONFIG = Path(".kimi-code") / "mcp.json"

_KIMI_NATIVE_MODEL_ALIASES = {
    "kimi-for-coding": KIMI_CODE_NATIVE_PUBLIC_MODEL,
    "kimi-code-native": KIMI_CODE_NATIVE_PUBLIC_MODEL,
    "kimi-code/kimi-for-coding": KIMI_CODE_NATIVE_PUBLIC_MODEL,
    "kimi-k3": "kimi-k3",
    "k3": "kimi-k3",
    "kimi-code/k3": "kimi-k3",
}

_KIMI_NATIVE_INTERNAL_MODEL_IDS = {
    KIMI_CODE_NATIVE_PUBLIC_MODEL: KIMI_CODE_NATIVE_INTERNAL_MODEL,
    "kimi-k3": "kimi-code/k3",
}


def _manifest_path() -> Path:
    return Path(__file__).resolve().parent / "manifest" / "kimi-code-native.manifest.json"


def normalize_mcp_server_for_kimi_native(source: Mapping[str, object]) -> dict[str, object]:
    """Translate a launcher MCP server spec into the native kimi-code mcp.json schema.

    Schema per https://moonshotai.github.io/kimi-code/en/customization/mcp.html:
    entries with a "command" field are stdio servers; entries with a "url" field
    and no "transport" are HTTP servers; legacy SSE servers set "transport": "sse".
    """
    # Anything reaching this function is already in the launcher's
    # `selected_servers` set, i.e. the operator/Fixer already chose to
    # include it — that decision is authoritative. Some upstream server
    # specs (notably the registry-sourced `fixer_mcp` entry) carry an
    # incidental "enabled" field left over from unrelated dashboard/registry
    # state (Codex's launcher has to force-override it to true for the same
    # reason). Only "disabled" is treated as a real per-server opt-out
    # signal here, matching the existing kimi-cli adapter's convention.
    if "url" in source or "serverUrl" in source:
        payload: dict[str, object] = {
            "url": source.get("url", source.get("serverUrl", "")),
            "enabled": not bool(source.get("disabled", False)),
        }
        headers = source.get("headers")
        if isinstance(headers, dict) and headers:
            payload["headers"] = {str(k): str(v) for k, v in headers.items()}
        return payload

    payload = {
        "command": str(source.get("command", "")).strip(),
        "enabled": not bool(source.get("disabled", False)),
    }
    args = source.get("args")
    if isinstance(args, (list, tuple)):
        payload["args"] = [str(item) for item in args]
    else:
        payload["args"] = []
    env = source.get("env")
    if isinstance(env, dict) and env:
        payload["env"] = {str(key): str(value) for key, value in env.items()}
    cwd = source.get("cwd")
    if isinstance(cwd, str) and cwd.strip():
        payload["cwd"] = cwd.strip()
    return payload


class KimiCodeNativeBackendAdapter(BackendAdapter):
    """Adapter for the native Kimi Code binary (~/.kimi-code/bin/kimi)."""

    def __init__(self) -> None:
        entry = load_backend_entry("kimi-code-native")
        self.descriptor = BackendDescriptor(
            name="kimi-code-native",
            label=str(entry["label"]),
            description=str(entry["description"]),
            default_model=str(entry["default_model"]),
            default_reasoning=str(entry["default_reasoning"]),
            model_options=tuple(str(value) for value in entry["model_options"]),
            reasoning_options=tuple(str(value) for value in entry["reasoning_options"]),
            fresh_launch_supported=bool(entry.get("fresh_launch_supported", True)),
            resume_supported=bool(entry.get("resume_supported", True)),
        )
        self.command = "kimi"
        self.supports_resume = self.descriptor.resume_supported
        self._manifest = self._load_manifest()
        self._runtime_cwd: Path | None = None

    def _load_manifest(self) -> Any:
        try:
            return load_manifest(_manifest_path())
        except Exception:
            return None

    def _mcp_config_path(self, cwd: Path | None = None) -> Path:
        base = cwd or self._runtime_cwd or Path.cwd()
        return (base / _KIMI_NATIVE_MCP_CONFIG).resolve()

    def _interactive_approve_flags(self) -> list[str]:
        if self._manifest is not None:
            flags = list(self._manifest.permissions.interactive_auto_approve_flags)
            if flags:
                return flags
        return ["--yolo"]

    def _headless_approve_flags(self) -> list[str]:
        # Empirically verified (real `kimi` binary v0.35.0): `--auto` and
        # `--yolo` both hard-error when combined with `-p`/`--prompt`
        # ("Cannot combine --prompt with --auto."/"...--yolo."). A bare `-p`
        # invocation with no approval flag at all already auto-approves
        # ordinary tool calls, so no approval flag is needed or valid here.
        if self._manifest is not None:
            return list(self._manifest.permissions.headless_auto_approve_flags)
        return []

    def normalize_model(self, model: str | None) -> str:
        candidate = (model or "").strip().lower()
        if not candidate:
            return KIMI_CODE_NATIVE_PUBLIC_MODEL
        if candidate in _KIMI_NATIVE_MODEL_ALIASES:
            canonical = _KIMI_NATIVE_MODEL_ALIASES[candidate]
            if self._manifest is not None and canonical not in self._manifest.models.options:
                raise RuntimeError(
                    f"Kimi Code native alias {candidate!r} resolved to {canonical!r} which is "
                    f"not declared in the manifest options: {self._manifest.models.options}"
                )
            return canonical
        return super().normalize_model(model)

    def normalize_reasoning(self, reasoning: str | None) -> str:
        # No CLI flag surfaces reasoning effort for the native binary today
        # (confirmed absent from `kimi --help`); thinking is controlled only
        # via config.toml. Accept whatever is passed and ignore it downstream.
        return "default"

    def _internal_model(self, model: str | None) -> str:
        public = self.normalize_model(model)
        if self._manifest is not None:
            internal = self._manifest.models.internal_id_map.get(public)
            if internal:
                return internal
        return _KIMI_NATIVE_INTERNAL_MODEL_IDS.get(public, KIMI_CODE_NATIVE_INTERNAL_MODEL)

    def build_llm_args(self, selection: Any) -> list[str]:
        model = str(getattr(selection, "model", "") or "").strip() or self.default_model
        return ["-m", self._internal_model(model)]

    def build_execution_args(self, prefs: Any) -> list[str]:
        del prefs
        return list(self._interactive_approve_flags())

    def build_mcp_flags(
        self,
        selected: Mapping[str, Mapping[str, object]],
        available: Mapping[str, Mapping[str, object]],
    ) -> list[str]:
        del selected, available
        # No CLI flag exists to point at a scoped MCP config file; the native
        # binary only auto-discovers `.kimi-code/mcp.json` under the project
        # cwd (or the git-repo-root `.mcp.json` compat fallback, undocumented
        # and out of scope for this adapter). ensure_runtime_files() writes
        # directly to that fixed path, so there is nothing to pass on the
        # command line.
        return []

    def build_prompt_args(self, prompt: str) -> list[str]:
        # Empirically verified this session: -p/--prompt is one-shot only and
        # there is no flag to preload a message into a session that then stays
        # interactive (same conclusion as the existing kimi-code/kimi-cli
        # backend). The launcher must fall back to the clipboard/bootstrap
        # print pattern for interactive launches.
        return []

    def build_resume_command(self, option_args: Sequence[str], external_session_id: str) -> list[str]:
        return [self.command, "-S", external_session_id.strip(), *list(option_args)]

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
        del reasoning, selected, available
        cmd = [
            self.command,
            *self._headless_approve_flags(),
            "-m",
            self._internal_model(model),
            "--output-format",
            "text",
        ]
        if prompt.strip():
            cmd.extend(["-p", prompt.strip()])
        return cmd

    def ensure_runtime_files(
        self,
        cwd: Path,
        selection: Any,
        selected: Mapping[str, Mapping[str, object]],
        available: Mapping[str, Mapping[str, object]],
    ) -> None:
        del selection
        self._runtime_cwd = cwd.resolve()
        mcp_servers: dict[str, dict[str, object]] = {}
        for name, config in sorted(selected.items()):
            source = dict(available.get(name, {}))
            source.update(dict(config))
            server_payload = normalize_mcp_server_for_kimi_native(source)
            if "url" in server_payload and not str(server_payload.get("url", "")).strip():
                continue
            if "url" not in server_payload and not str(server_payload.get("command", "")).strip():
                continue
            mcp_servers[name] = server_payload

        mcp_path = self._mcp_config_path(cwd)
        mcp_path.parent.mkdir(parents=True, exist_ok=True)
        mcp_path.write_text(
            json.dumps({"mcpServers": mcp_servers}, indent=2, sort_keys=True) + "\n",
            encoding="utf-8",
        )
        materialize_kimi_code_workspace_skills(cwd, FIXER_ROLE_SKILL_NAMES)
