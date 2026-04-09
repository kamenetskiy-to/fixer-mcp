from __future__ import annotations

import json
import os
import subprocess
import time
import urllib.error
import urllib.request
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Dict, Optional

from .session import SessionContext


DEFAULT_API_URL = "http://localhost:3010"
DEFAULT_STACK_ROOT = Path.home() / "Desktop" / "projects" / "mcp_servers" / "tmp" / "searCrawl_stack"


@dataclass(frozen=True)
class StackConfig:
    api_url: str
    stack_root: Optional[Path]
    autostart: bool = True


class StackError(RuntimeError):
    pass


def resolve_stack_config() -> StackConfig:
    api_url = os.environ.get("SEARCRAWL_API_URL", DEFAULT_API_URL)
    root_value = os.environ.get("CODEX_DR_STACK_ROOT")
    stack_root: Optional[Path]
    if root_value:
        stack_root = Path(root_value).expanduser()
    else:
        stack_root = DEFAULT_STACK_ROOT if DEFAULT_STACK_ROOT.exists() else None
    autostart = os.environ.get("CODEX_DR_AUTOSTART_STACK", "true").lower() != "false"
    return StackConfig(api_url=api_url, stack_root=stack_root, autostart=autostart)


def _health_url(api_url: str) -> str:
    base = api_url.rstrip("/")
    return f"{base}/health"


def check_health(api_url: str, *, timeout: float = 5.0) -> bool:
    payload: Dict[str, object] = {}
    request = urllib.request.Request(_health_url(api_url), method="GET")
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            if response.status == 200:
                try:
                    payload = json.loads(response.read().decode("utf-8"))
                except json.JSONDecodeError:
                    return False
                return bool(payload.get("status") in ("ok", "healthy", "ready"))
            # Treat other 2xx codes as healthy as long as service responds.
            if 200 < response.status < 300:
                return True
            return False
    except urllib.error.HTTPError as exc:
        # searCrawl currently returns 404 for /health; consider any response as proof of life.
        if exc.code == 404:
            return True
        return False
    except (urllib.error.URLError, OSError):
        return False


def _run_stack_script(stack_root: Path) -> None:
    script_path = stack_root / "scripts" / "run_stack.sh"
    if not script_path.is_file():
        raise StackError(f"Stack script '{script_path}' is missing.")
    cmd = ["bash", str(script_path)]
    try:
        subprocess.run(cmd, cwd=str(stack_root), check=True)
    except subprocess.CalledProcessError as exc:
        raise StackError(f"Failed to start searCrawl stack via {script_path}: {exc}") from exc


def ensure_stack_ready(config: StackConfig, *, timeout: float = 90.0) -> bool:
    if check_health(config.api_url):
        return True
    if not config.autostart:
        return False
    if config.stack_root is None:
        return False
    try:
        _run_stack_script(config.stack_root)
    except StackError as err:
        print(f"[codex-dr] {err}")
        return False

    deadline = time.time() + timeout
    while time.time() < deadline:
        time.sleep(3.0)
        if check_health(config.api_url):
            return True
    return False


def _search_url(api_url: str) -> str:
    base = api_url.rstrip("/")
    return f"{base}/search"


def perform_search(
    query: str,
    *,
    api_url: str,
    limit: Optional[int] = None,
    session: Optional[SessionContext] = None,
    metadata: Optional[Dict[str, object]] = None,
    timeout: float = 60.0,
) -> Dict[str, object]:
    payload: Dict[str, object] = {"query": query}
    if limit is not None:
        payload["limit"] = limit
    if metadata:
        payload.update(metadata)

    data = json.dumps(payload).encode("utf-8")
    request = urllib.request.Request(
        _search_url(api_url),
        data=data,
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            body = response.read().decode("utf-8")
            result = json.loads(body)
    except urllib.error.HTTPError as exc:
        raise StackError(f"searCrawl returned HTTP {exc.code}: {exc.reason}") from exc
    except (urllib.error.URLError, OSError) as exc:
        raise StackError(f"Failed to reach searCrawl API: {exc}") from exc
    except json.JSONDecodeError as exc:
        raise StackError(f"Invalid JSON payload from searCrawl: {exc}") from exc

    if session:
        _persist_search_artifact(session, query, result)
    return result


def _persist_search_artifact(session: SessionContext, query: str, payload: Dict[str, object]) -> None:
    timestamp = datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ")
    safe_query = query.replace("/", "_")[:80]
    filename = f"{timestamp}-{safe_query}.json"
    artifact_path = session.paths.search_dir / filename
    artifact = {
        "query": query,
        "result": payload,
        "created_at": datetime.now(timezone.utc).isoformat(),
    }
    artifact_path.write_text(json.dumps(artifact, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")

    from .session import append_inventory_entry  # local import to avoid cycle

    inventory_entry = {
        "id": filename,
        "type": "search",
        "query": query,
        "artifact_path": str(artifact_path),
        "created_at": artifact["created_at"],
    }
    append_inventory_entry(session, inventory_entry)
