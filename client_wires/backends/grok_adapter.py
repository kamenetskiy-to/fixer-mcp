from __future__ import annotations

from pathlib import Path
from typing import Any, Mapping, Sequence

from .base import BackendAdapter, BackendDescriptor, materialize_codex_project_skills, FIXER_ROLE_SKILL_NAMES
from .catalog import load_backend_entry

# Public aliases surfaced in the catalog / launcher.
GROK_46_MODEL = "grok-4.6"
GROK_45_MODEL = "grok-4.5"

# Project-scoped MCP config for the native `grok` binary. `grok mcp add -s
# project` writes the same file; the adapter writes it directly so wave
# worktrees get the selected servers without shelling out to `grok mcp`.
_GROK_NATIVE_MCP_CONFIG = Path(".grok") / "config.toml"


def _toml_quote(value: str) -> str:
    return '"' + value.replace("\\", "\\\\").replace('"', '\\"') + '"'


def _render_grok_mcp_servers(selected: Mapping[str, Mapping[str, object]], available: Mapping[str, Mapping[str, object]]) -> str:
    blocks: list[str] = []
    for name in sorted(selected):
        source = dict(available.get(name, {}))
        source.update(dict(selected[name]))
        lines = [f"[mcp_servers.{name}]"]
        url = str(source.get("url", source.get("serverUrl", ""))).strip()
        if url:
            lines.append(f"url = {_toml_quote(url)}")
        else:
            command = str(source.get("command", "")).strip()
            if not command:
                continue
            lines.append(f"command = {_toml_quote(command)}")
            args = source.get("args")
            rendered_args = ", ".join(_toml_quote(str(item)) for item in args) if isinstance(args, (list, tuple)) else ""
            lines.append(f"args = [{rendered_args}]")
        lines.append(f"enabled = {'false' if source.get('disabled', False) else 'true'}")
        env = source.get("env")
        if isinstance(env, dict) and env:
            lines.append(f"[mcp_servers.{name}.env]")
            for key in sorted(env):
                lines.append(f"{key} = {_toml_quote(str(env[key]))}")
        blocks.append("\n".join(lines))
    return "\n\n".join(blocks)


def _strip_existing_mcp_servers(text: str) -> str:
    """Remove existing [mcp_servers...] sections, keep any other config keys."""
    kept: list[str] = []
    skipping = False
    for line in text.splitlines():
        stripped = line.strip()
        if stripped.startswith("["):
            skipping = stripped.startswith("[mcp_servers.")
            if skipping:
                continue
        if skipping:
            continue
        kept.append(line)
    return "\n".join(kept).strip("\n")


class GrokBackendAdapter(BackendAdapter):
    """Adapter for the native Grok Build CLI (`grok`, xAI).

    Skills: `grok` auto-discovers the canonical `.agents/skills/` root, so no
    provider-specific materialization is needed beyond the shared one.
    MCP: project-scoped `.grok/config.toml` `[mcp_servers.*]` sections.
    """

    def __init__(self) -> None:
        entry = load_backend_entry("grok")
        self.descriptor = BackendDescriptor(
            name="grok",
            label=str(entry["label"]),
            description=str(entry["description"]),
            default_model=str(entry["default_model"]),
            default_reasoning=str(entry["default_reasoning"]),
            model_options=tuple(str(value) for value in entry["model_options"]),
            reasoning_options=tuple(str(value) for value in entry["reasoning_options"]),
            fresh_launch_supported=bool(entry.get("fresh_launch_supported", True)),
            resume_supported=bool(entry.get("resume_supported", True)),
        )
        self.command = "grok"
        self.supports_resume = self.descriptor.resume_supported

    def build_llm_args(self, selection: Any) -> list[str]:
        model = str(getattr(selection, "model", "") or "").strip() or self.default_model
        return ["-m", model]

    def build_execution_args(self, prefs: Any) -> list[str]:
        del prefs
        return ["--always-approve"]

    def build_mcp_flags(
        self,
        selected: Mapping[str, Mapping[str, object]],
        available: Mapping[str, Mapping[str, object]],
    ) -> list[str]:
        # `grok` auto-discovers the project-scoped `.grok/config.toml`; there
        # is no --mcp-config-file flag to point at an arbitrary file.
        del selected, available
        return []

    def build_resume_command(self, option_args: Sequence[str], external_session_id: str) -> list[str]:
        return [self.command, "-r", external_session_id.strip(), *list(option_args)]

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
        del selected, available
        cmd = [
            self.command,
            "--always-approve",
            "-m",
            model.strip() or self.default_model,
        ]
        effort = reasoning.strip()
        if effort and effort != "default":
            cmd.extend(["--reasoning-effort", effort])
        if prompt.strip():
            cmd.extend(["-p", prompt.strip()])
        return cmd

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
        command[1:1] = ["-r", external_session_id.strip()]
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
        materialize_codex_project_skills(cwd, FIXER_ROLE_SKILL_NAMES)
        config_path = (cwd / _GROK_NATIVE_MCP_CONFIG).resolve()
        config_path.parent.mkdir(parents=True, exist_ok=True)
        existing = config_path.read_text(encoding="utf-8") if config_path.is_file() else ""
        base = _strip_existing_mcp_servers(existing)
        rendered = _render_grok_mcp_servers(selected, available)
        parts = [part for part in (base, rendered) if part]
        config_path.write_text("\n\n".join(parts) + "\n", encoding="utf-8")
