from __future__ import annotations

import json
import time
from pathlib import Path
from typing import Mapping

PROXY_ENV_ALIAS_GROUPS: tuple[tuple[str, ...], ...] = (
    ("ALL_PROXY", "all_proxy"),
    ("HTTP_PROXY", "http_proxy"),
    ("HTTPS_PROXY", "https_proxy"),
    ("NO_PROXY", "no_proxy"),
)
PROXY_ENV_STATE_RELATIVE_PATH = Path(".codex") / "runtime_proxy_env.json"


def proxy_env_state_path(cwd: Path) -> Path:
    return cwd / PROXY_ENV_STATE_RELATIVE_PATH


def capture_proxy_env(environ: Mapping[str, str] | None = None) -> dict[str, str]:
    source = environ or {}
    payload: dict[str, str] = {}
    for aliases in PROXY_ENV_ALIAS_GROUPS:
        value = ""
        for name in aliases:
            candidate = str(source.get(name, "")).strip()
            if candidate:
                value = candidate
                break
        if not value:
            continue
        for name in aliases:
            payload[name] = value
    return payload


def load_proxy_env_state(cwd: Path) -> dict[str, str]:
    path = proxy_env_state_path(cwd)
    if not path.is_file():
        return {}
    payload = json.loads(path.read_text(encoding="utf-8"))
    if isinstance(payload, dict):
        stored = payload.get("proxy_env", payload)
        if isinstance(stored, dict):
            return capture_proxy_env({str(key): str(value) for key, value in stored.items()})
    return {}


def save_proxy_env_state(cwd: Path, proxy_env: Mapping[str, str]) -> Path:
    path = proxy_env_state_path(cwd)
    path.parent.mkdir(parents=True, exist_ok=True)
    normalized = capture_proxy_env({str(key): str(value) for key, value in proxy_env.items()})
    payload = {
        "proxy_env": normalized,
        "updated_at_epoch": int(time.time()),
    }
    path.write_text(json.dumps(payload, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    return path


def resolve_proxy_env(cwd: Path, environ: Mapping[str, str] | None = None) -> dict[str, str]:
    current = capture_proxy_env(environ)
    if current:
        save_proxy_env_state(cwd, current)
        return current
    return load_proxy_env_state(cwd)


def apply_proxy_env(target_env: dict[str, str], proxy_env: Mapping[str, str]) -> dict[str, str]:
    for key, value in capture_proxy_env({str(name): str(raw) for name, raw in proxy_env.items()}).items():
        target_env[key] = value
    return target_env


def clear_proxy_env(target_env: dict[str, str]) -> dict[str, str]:
    for aliases in PROXY_ENV_ALIAS_GROUPS:
        for name in aliases:
            target_env.pop(name, None)
    return target_env
