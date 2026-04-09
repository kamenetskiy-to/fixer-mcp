from __future__ import annotations

import os
import subprocess
import sys
from pathlib import Path
from typing import Dict, List, Optional

from ..config_loader import ConfigError, fetch_mcp_servers, get_config_path, load_config
from ..prompts import YOLO_PROMPT, compose_prompt
from .depths import DepthProfile, select_depth
from .session import append_research_log, create_session


DEFAULT_SESSION_ROOT = Path.home() / "Desktop" / "deep_researches"
DEFAULT_MODEL = "gpt-5.4"
DEFAULT_EFFORT = "high"


def _session_root() -> Path:
    root_env = os.environ.get("CODEX_DR_SESSION_ROOT")
    if root_env:
        root = Path(root_env).expanduser()
    else:
        root = DEFAULT_SESSION_ROOT
    root.mkdir(parents=True, exist_ok=True)
    return root


def _resolve_model_settings(config: Dict[str, object]) -> tuple[str, str]:
    model = os.environ.get("CODEX_DR_MODEL")
    effort = os.environ.get("CODEX_DR_REASONING")

    if not model:
        configured_model = config.get("model")
        if isinstance(configured_model, str):
            model = configured_model
        else:
            model = DEFAULT_MODEL

    if not effort:
        configured_effort = config.get("model_reasoning_effort")
        if isinstance(configured_effort, str):
            effort = configured_effort
        else:
            effort = DEFAULT_EFFORT

    return model, effort


def _read_prompt(path: Path) -> str:
    if not path.is_file():
        raise FileNotFoundError(f"Prompt file '{path}' is missing.")
    return path.read_text(encoding="utf-8").strip()


def _prompt_session_slug(profile: DepthProfile) -> Optional[str]:
    env_slug = os.environ.get("CODEX_DR_SESSION_NAME")
    if env_slug:
        return env_slug.strip() or None
    if not sys.stdin.isatty():
        return None
    prompt_text = (
        f"Назови исследование (slug для папки, например 'cursor-pricing').\n"
        f"Оставь пустым, чтобы использовать профиль '{profile.key}'.\n"
        "Имя:"
    )
    try:
        value = input(f"{prompt_text} ").strip()
    except KeyboardInterrupt:
        raise
    except EOFError:
        return None
    return value or None


def _build_prompt(profile: DepthProfile, prompts_dir: Path, session_dir: Path, *, include_yolo: bool) -> str:
    system_prompt = _read_prompt(prompts_dir / "system.md")
    logging_prompt = _read_prompt(prompts_dir / "logging.md")
    plan_prompt = _read_prompt(prompts_dir / profile.plan_template)

    session_prompt = "\n".join(
        [
            "Session workspace:",
            f"- Root: {session_dir}",
            f"- Logs: {session_dir / 'logs'}",
            f"- Notes: {session_dir / 'notes'}",
            f"- Artifacts: {session_dir / 'artifacts'}",
            f"- Reports: {session_dir / 'reports'}",
        ]
    )

    parts: List[str] = [system_prompt, plan_prompt, logging_prompt, session_prompt]
    if include_yolo:
        parts.insert(0, YOLO_PROMPT)
    return compose_prompt(parts)


def _select_servers(available: Dict[str, Dict[str, object]]) -> Dict[str, Dict[str, object]]:
    selected: Dict[str, Dict[str, object]] = {}
    defaults = ["telegram_notify", "tavily"]

    extra = os.environ.get("CODEX_DR_ENABLE_MCP")
    if extra:
        defaults.extend(part.strip() for part in extra.split(",") if part.strip())

    disabled = os.environ.get("CODEX_DR_DISABLE_MCP")
    disabled_set = {part.strip() for part in disabled.split(",") if part.strip()} if disabled else set()

    for name in defaults:
        if name in available and name not in disabled_set:
            selected[name] = available[name]
    return selected


def _enable_servers_flags(selected: Dict[str, Dict[str, object]], available: Dict[str, Dict[str, object]]) -> List[str]:
    overrides: List[str] = []
    for name in available:
        enabled = "true" if name in selected else "false"
        overrides.extend(["-c", f"mcp_servers.{name}.enabled={enabled}"])
    return overrides


def _print_launch_summary(model: str, effort: str, profile: DepthProfile, session_root: Path, mcp_names: List[str]) -> None:
    print("=== codex-dr ===")
    print(f"Depth profile: {profile.label} ({profile.key})")
    print(f"Model: {model} (reasoning effort: {effort})")
    print(f"Session directory: {session_root}")
    print(f"MCP servers enabled: {', '.join(mcp_names) if mcp_names else 'none'}")
    print("================")


def run(argv: List[str]) -> int:
    cwd = Path.cwd()
    repo_root = Path(__file__).resolve().parents[2]
    prompts_dir = repo_root / "codex_prompts" / "codex_dr"

    config_path = get_config_path()
    try:
        config = load_config(config_path)
        available_servers = fetch_mcp_servers(config)
    except ConfigError as err:
        print(err, file=sys.stderr)
        return 1

    model, effort = _resolve_model_settings(config)
    try:
        profile = select_depth()
    except KeyboardInterrupt:
        print("codex-dr depth selection cancelled.")
        return 130
    session_root = _session_root()
    requested_slug = _prompt_session_slug(profile)
    session = create_session(profile, base_dir=session_root, explicit_slug=requested_slug)
    append_research_log(session, f"Depth selected: {profile.key}")
    if requested_slug and requested_slug != session.slug:
        append_research_log(session, f"Session slug normalized to '{session.slug}' from '{requested_slug}'")
    else:
        append_research_log(session, f"Session slug: {session.slug}")
    append_research_log(session, f"Initialized session at {session.paths.root}")

    prompt = _build_prompt(profile, prompts_dir, session.paths.root, include_yolo=profile.yolo_enabled)

    base_cmd: List[str] = [
        "codex",
        "--model",
        model,
        "-c",
        f'model_reasoning_effort="{effort}"',
        "--sandbox",
        "danger-full-access",
        "--ask-for-approval",
        "never",
    ]
    base_cmd.extend(argv)

    selected_servers = _select_servers(available_servers)
    base_cmd.extend(_enable_servers_flags(selected_servers, available_servers))

    _print_launch_summary(model, effort, profile, session.paths.root, list(selected_servers.keys()))

    base_cmd.append(prompt)

    env = os.environ.copy()
    env.update(session.environment_overrides())
    env["CODEX_DR_TIME_LIMIT_MINUTES"] = str(profile.time_limit_minutes)
    env["CODEX_DR_MAX_SEARCH_REQUESTS"] = str(profile.max_search_requests)
    env["CODEX_DR_MAX_QUERIES"] = str(profile.max_queries)
    if profile.checkin_interval_minutes:
        env["CODEX_DR_CHECKIN_MINUTES"] = str(profile.checkin_interval_minutes)
    env["CODEX_DR_SYSTEM_PROMPT_PATH"] = str(prompts_dir / "system.md")
    env["CODEX_DR_LOGGING_PROMPT_PATH"] = str(prompts_dir / "logging.md")
    env["CODEX_DR_PLAN_TEMPLATE_PATH"] = str(prompts_dir / profile.plan_template)

    return subprocess.call(base_cmd, env=env, cwd=str(cwd))
