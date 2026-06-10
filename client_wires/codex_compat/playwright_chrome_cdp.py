#!/usr/bin/env python3
"""Start normal Chrome and bridge Playwright MCP over CDP."""

from __future__ import annotations

import argparse
import os
from pathlib import Path
import shutil
import signal
import socket
import subprocess
import sys
import time
import urllib.error
import urllib.request


DEFAULT_CHROME_CANDIDATES = (
    Path("/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"),
    Path("/Applications/Chromium.app/Contents/MacOS/Chromium"),
)


def _find_chrome() -> str:
    env_path = os.environ.get("CODEX_PRO_PLAYWRIGHT_CHROME_EXECUTABLE")
    if env_path and env_path.strip():
        return str(Path(env_path).expanduser())

    for candidate in DEFAULT_CHROME_CANDIDATES:
        if candidate.is_file():
            return str(candidate)

    for name in ("google-chrome", "chromium", "chrome"):
        found = shutil.which(name)
        if found:
            return found

    raise RuntimeError("Chrome executable not found")


def _free_port() -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
        sock.bind(("127.0.0.1", 0))
        return int(sock.getsockname()[1])


def _wait_for_cdp(endpoint: str, timeout_sec: float = 20.0) -> None:
    deadline = time.monotonic() + timeout_sec
    url = endpoint.rstrip("/") + "/json/version"
    last_error: Exception | None = None
    while time.monotonic() < deadline:
        try:
            with urllib.request.urlopen(url, timeout=0.5) as response:
                if response.status == 200:
                    return
        except (OSError, urllib.error.URLError) as exc:
            last_error = exc
        time.sleep(0.1)
    raise RuntimeError(f"Timed out waiting for Chrome CDP endpoint {endpoint}: {last_error}")


def _terminate(process: subprocess.Popen[object]) -> None:
    if process.poll() is not None:
        return
    process.terminate()
    try:
        process.wait(timeout=5)
    except subprocess.TimeoutExpired:
        process.kill()
        process.wait(timeout=5)


def _chrome_pids_for_profile(profile_dir: Path) -> list[int]:
    marker = str(profile_dir)
    result = subprocess.run(["ps", "axo", "pid=,command="], text=True, capture_output=True)
    pids: list[int] = []
    for line in result.stdout.splitlines():
        if marker not in line:
            continue
        parts = line.strip().split(maxsplit=1)
        if not parts:
            continue
        try:
            pid = int(parts[0])
        except ValueError:
            continue
        if pid != os.getpid():
            pids.append(pid)
    return pids


def _terminate_chrome_for_profile(chrome: subprocess.Popen[object], profile_dir: Path) -> None:
    _terminate(chrome)
    pids = _chrome_pids_for_profile(profile_dir)
    for pid in pids:
        try:
            os.kill(pid, signal.SIGTERM)
        except ProcessLookupError:
            pass
    if pids:
        time.sleep(1)
    for pid in _chrome_pids_for_profile(profile_dir):
        try:
            os.kill(pid, signal.SIGKILL)
        except ProcessLookupError:
            pass


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(description="Start normal Chrome and bridge Playwright MCP over CDP.")
    parser.add_argument("--user-data-dir", required=True)
    parser.add_argument("--viewport-size")
    parser.add_argument("--port", type=int, default=0)
    args = parser.parse_args(argv)

    profile_dir = Path(args.user_data_dir).expanduser()
    profile_dir.mkdir(parents=True, exist_ok=True)

    port = args.port or _free_port()
    cdp_endpoint = f"http://127.0.0.1:{port}"
    chrome_cmd = [
        _find_chrome(),
        f"--remote-debugging-port={port}",
        f"--user-data-dir={profile_dir}",
        "--no-first-run",
        "--no-default-browser-check",
        "about:blank",
    ]
    chrome = subprocess.Popen(chrome_cmd, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)

    mcp: subprocess.Popen[object] | None = None

    def stop_children(_signum: int, _frame: object) -> None:
        if mcp is not None:
            _terminate(mcp)
        _terminate_chrome_for_profile(chrome, profile_dir)

    signal.signal(signal.SIGINT, stop_children)
    signal.signal(signal.SIGTERM, stop_children)

    try:
        _wait_for_cdp(cdp_endpoint)
        mcp_cmd = [
            "npx",
            "-y",
            "@playwright/mcp@latest",
            "--cdp-endpoint",
            cdp_endpoint,
        ]
        if args.viewport_size:
            mcp_cmd.extend(["--viewport-size", args.viewport_size])
        mcp = subprocess.Popen(mcp_cmd)
        return mcp.wait()
    finally:
        if os.environ.get("CODEX_PRO_PLAYWRIGHT_KEEP_CHROME") != "1":
            _terminate_chrome_for_profile(chrome, profile_dir)


if __name__ == "__main__":
    try:
        raise SystemExit(main(sys.argv[1:]))
    except Exception as exc:
        print(f"playwright chrome cdp wrapper failed: {exc}", file=sys.stderr)
        raise SystemExit(1)
