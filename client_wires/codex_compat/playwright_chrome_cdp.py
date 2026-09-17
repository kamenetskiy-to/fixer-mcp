#!/usr/bin/env python3
"""Attach/launch Chrome and expose one isolated agent-owned Playwright MCP context."""

from __future__ import annotations

import argparse
from contextlib import contextmanager
from dataclasses import dataclass
from datetime import datetime, timezone
import fcntl
import json
import os
from pathlib import Path
import shutil
import shlex
import signal
import socket
import subprocess
import sys
import time
import traceback
import urllib.error
import urllib.request


DEFAULT_CHROME_CANDIDATES = (
    Path("/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"),
    Path("/Applications/Chromium.app/Contents/MacOS/Chromium"),
)
PLAYWRIGHT_LOG_ENV = "CODEX_PRO_PLAYWRIGHT_LOG"
PLAYWRIGHT_HANDOFF_GRACE_ENV = "CODEX_PRO_PLAYWRIGHT_HANDOFF_GRACE_SEC"
PLAYWRIGHT_ALLOW_FOREGROUND_ENV = "CODEX_PRO_PLAYWRIGHT_ALLOW_FOREGROUND"
DEFAULT_HANDOFF_GRACE_SEC = 5.0
RUNTIME_DIR_NAME = ".codex-playwright-runtime"


@dataclass(frozen=True)
class ChromeProfileProcess:
    pid: int
    command: str
    remote_debugging_port: int | None


@dataclass(frozen=True)
class ManagedChromeState:
    pid: int
    port: int


def _playwright_log_path() -> Path:
    configured = os.environ.get(PLAYWRIGHT_LOG_ENV)
    if configured and configured.strip():
        return Path(configured).expanduser()
    mcp_logs = Path.home() / "Desktop" / "projects" / "mcp_servers" / "logs"
    if mcp_logs.is_dir():
        return mcp_logs / "playwright_mcp.log"
    return Path.home() / ".codex" / "logs" / "playwright_mcp.log"


def _log(message: str) -> None:
    timestamp = datetime.now(timezone.utc).isoformat(timespec="milliseconds")
    line = f"{timestamp} pid={os.getpid()} {message}\n"
    try:
        path = _playwright_log_path()
        path.parent.mkdir(parents=True, exist_ok=True)
        with path.open("a", encoding="utf-8") as stream:
            stream.write(line)
    except OSError:
        pass


def _runtime_dir(profile_dir: Path) -> Path:
    return profile_dir / RUNTIME_DIR_NAME


@contextmanager
def _profile_runtime_lock(profile_dir: Path):
    runtime_dir = _runtime_dir(profile_dir)
    runtime_dir.mkdir(parents=True, exist_ok=True)
    with (runtime_dir / "lifecycle.lock").open("a+", encoding="utf-8") as lock:
        fcntl.flock(lock.fileno(), fcntl.LOCK_EX)
        try:
            yield
        finally:
            fcntl.flock(lock.fileno(), fcntl.LOCK_UN)


def _managed_state_path(profile_dir: Path) -> Path:
    return _runtime_dir(profile_dir) / "managed-chrome.json"


def _lease_path(profile_dir: Path, pid: int) -> Path:
    return _runtime_dir(profile_dir) / f"lease-{pid}.json"


def _read_managed_state(profile_dir: Path) -> ManagedChromeState | None:
    try:
        payload = json.loads(_managed_state_path(profile_dir).read_text(encoding="utf-8"))
        return ManagedChromeState(pid=int(payload["pid"]), port=int(payload["port"]))
    except (OSError, ValueError, TypeError, KeyError, json.JSONDecodeError):
        return None


def _write_managed_state(profile_dir: Path, state: ManagedChromeState) -> None:
    path = _managed_state_path(profile_dir)
    temporary = path.with_suffix(f".{os.getpid()}.tmp")
    temporary.write_text(json.dumps({"pid": state.pid, "port": state.port}) + "\n", encoding="utf-8")
    temporary.replace(path)


def _register_lease(profile_dir: Path) -> Path:
    path = _lease_path(profile_dir, os.getpid())
    path.write_text(json.dumps({"pid": os.getpid(), "started_at": time.time()}) + "\n", encoding="utf-8")
    return path


def _process_command(pid: int) -> str:
    result = subprocess.run(
        ["ps", "-p", str(pid), "-o", "command="],
        text=True,
        capture_output=True,
    )
    return result.stdout.strip()


def _is_live_wrapper_lease(pid: int, profile_dir: Path) -> bool:
    if pid == os.getpid():
        return True
    command = _process_command(pid)
    return bool(command and Path(__file__).name in command and str(profile_dir) in command)


def _live_lease_pids(profile_dir: Path) -> list[int]:
    live: list[int] = []
    for path in _runtime_dir(profile_dir).glob("lease-*.json"):
        try:
            pid = int(path.stem.removeprefix("lease-"))
        except ValueError:
            path.unlink(missing_ok=True)
            continue
        if _is_live_wrapper_lease(pid, profile_dir):
            live.append(pid)
        else:
            path.unlink(missing_ok=True)
    return sorted(live)


def _managed_state_is_live(profile_dir: Path, state: ManagedChromeState) -> bool:
    return any(
        process.pid == state.pid and process.remote_debugging_port == state.port
        for process in _chrome_processes_for_profile(profile_dir)
    )


def _handoff_grace_sec() -> float:
    raw = os.environ.get(PLAYWRIGHT_HANDOFF_GRACE_ENV)
    if not raw or not raw.strip():
        return DEFAULT_HANDOFF_GRACE_SEC
    try:
        return min(max(float(raw), 0.0), 30.0)
    except ValueError:
        return DEFAULT_HANDOFF_GRACE_SEC


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


def _allow_foreground() -> bool:
    return os.environ.get(PLAYWRIGHT_ALLOW_FOREGROUND_ENV, "").strip().lower() in {
        "1",
        "true",
        "yes",
    }


def _macos_app_bundle(executable: str) -> Path | None:
    path = Path(executable).expanduser()
    for parent in (path, *path.parents):
        if parent.suffix == ".app":
            return parent
    return None


def _macos_console_locked() -> bool | None:
    """Report the macOS console lock state without touching System Events.

    A locked console has a GUI session and running apps, but nothing can be seen
    on the physical screen. Detecting it lets the wrapper explain an otherwise
    baffling "the agent says it opened a browser but I see nothing".
    """
    if sys.platform != "darwin":
        return None
    for binary in ("/usr/sbin/ioreg", "/usr/bin/ioreg"):
        if not Path(binary).is_file():
            continue
        try:
            result = subprocess.run([binary, "-n", "Root", "-d1"], capture_output=True, text=True, timeout=5)
        except (subprocess.TimeoutExpired, OSError):
            return None
        for line in result.stdout.splitlines():
            if "IOConsoleLocked" in line:
                return "Yes" in line
        return None
    return None


def _activate_browser_app(executable: str) -> None:
    """Bring the browser to the front so a headed window is actually visible.

    Uses a direct application tell. System Events is deliberately avoided: it can
    hang for minutes on a busy or locked console.
    """
    bundle = _macos_app_bundle(executable)
    if bundle is None:
        return
    try:
        subprocess.run(
            ["/usr/bin/osascript", "-e", f'tell application "{bundle.stem}" to activate'],
            capture_output=True,
            text=True,
            timeout=8,
        )
    except (subprocess.TimeoutExpired, OSError):
        pass


def _chrome_launch_command(executable: str, chrome_args: list[str], *, headless: bool) -> list[str]:
    if sys.platform == "darwin" and not headless and not _allow_foreground():
        bundle = _macos_app_bundle(executable)
        if bundle is not None:
            # `open -g` starts the visible app without making it frontmost or
            # switching macOS Spaces.  The agent still gets a normal headed
            # Chrome window and attaches to it over CDP.
            return ["/usr/bin/open", "-g", "-na", str(bundle), "--args", *chrome_args]
    return [executable, *chrome_args]


def _frontmost_bundle_identifier() -> str | None:
    if sys.platform != "darwin":
        return None
    try:
        result = subprocess.run(
            [
                "/usr/bin/osascript",
                "-e",
                'tell application "System Events" to get bundle identifier of first application process whose frontmost is true',
            ],
            capture_output=True,
            text=True,
            timeout=2,
        )
    except (subprocess.TimeoutExpired, OSError):
        # System Events can be slow, busy, or awaiting an Automation permission
        # decision. Losing the foreground hint must never abort the wrapper:
        # a raised TimeoutExpired here closed the MCP handshake before it started.
        return None
    if result.returncode != 0:
        return None
    return result.stdout.strip() or None


def _restore_frontmost_bundle_identifier(bundle_identifier: str | None) -> None:
    if sys.platform != "darwin" or not bundle_identifier:
        return
    script = """
on run argv
  tell application "System Events"
    set targetBundle to item 1 of argv
    set frontmost of first application process whose bundle identifier is targetBundle to true
  end tell
end run
""".strip()
    try:
        subprocess.run(
            ["/usr/bin/osascript", "-e", script, bundle_identifier],
            capture_output=True,
            text=True,
            timeout=3,
        )
    except (subprocess.TimeoutExpired, OSError):
        pass


def _main_chrome_pid_for_profile(profile_dir: Path) -> int | None:
    for process in _chrome_processes_for_profile(profile_dir):
        command = process.command.lower()
        if "helper" not in command and "--type=" not in command:
            return process.pid
    return None


def _wait_for_main_chrome_pid(profile_dir: Path, timeout_sec: float = 5.0) -> int:
    deadline = time.monotonic() + timeout_sec
    while time.monotonic() < deadline:
        pid = _main_chrome_pid_for_profile(profile_dir)
        if pid is not None:
            return pid
        time.sleep(0.1)
    raise RuntimeError(f"Timed out waiting for the main Chrome process for profile {profile_dir}")


def _free_port() -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
        sock.bind(("127.0.0.1", 0))
        return int(sock.getsockname()[1])


def _cdp_endpoint_is_reachable(endpoint: str, timeout_sec: float = 0.8) -> bool:
    deadline = time.monotonic() + timeout_sec
    url = endpoint.rstrip("/") + "/json/version"
    while time.monotonic() < deadline:
        try:
            with urllib.request.urlopen(url, timeout=0.5) as response:
                if response.status == 200:
                    return True
        except (OSError, urllib.error.URLError):
            pass
        time.sleep(0.1)
    return False


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


def _remote_debugging_port_from_command(command: str) -> int | None:
    try:
        parts = shlex.split(command)
    except ValueError:
        parts = command.split()

    for index, part in enumerate(parts):
        if part.startswith("--remote-debugging-port="):
            value = part.split("=", 1)[1]
        elif part == "--remote-debugging-port" and index + 1 < len(parts):
            value = parts[index + 1]
        else:
            continue
        try:
            return int(value)
        except ValueError:
            return None
    return None


def _looks_like_chrome_process(command: str) -> bool:
    lowered = command.lower()
    return (
        "google chrome" in lowered
        or "chromium" in lowered
        or "/chrome " in lowered
        or lowered.endswith("/chrome")
        or lowered.startswith("chrome ")
    )


def _chrome_processes_for_profile(profile_dir: Path) -> list[ChromeProfileProcess]:
    marker = str(profile_dir)
    result = subprocess.run(["ps", "axo", "pid=,command="], text=True, capture_output=True)
    processes: list[ChromeProfileProcess] = []
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
            command = parts[1] if len(parts) > 1 else ""
            if not _looks_like_chrome_process(command):
                continue
            processes.append(
                ChromeProfileProcess(
                    pid=pid,
                    command=command,
                    remote_debugging_port=_remote_debugging_port_from_command(command),
                )
            )
    return processes


def _chrome_pids_for_profile(profile_dir: Path) -> list[int]:
    return [process.pid for process in _chrome_processes_for_profile(profile_dir)]


def _profile_in_use_without_cdp_message(profile_dir: Path, processes: list[ChromeProfileProcess]) -> str:
    joined = ", ".join(str(process.pid) for process in processes)
    ports = sorted({process.remote_debugging_port for process in processes if process.remote_debugging_port})
    if ports:
        port_note = (
            " Found remote-debugging port(s) "
            f"{', '.join(str(port) for port in ports)}, but none answered on 127.0.0.1."
        )
    else:
        port_note = " No --remote-debugging-port flag was found on the matching Chrome processes."
    return (
        "Chrome profile is already in use by process(es) "
        f"{joined}: {profile_dir}, but no reachable Chrome DevTools Protocol endpoint "
        f"could be found.{port_note} Close that Chrome instance, restart it with "
        "--remote-debugging-port, choose the blank Headless isolated runtime, or set "
        "CODEX_PRO_PLAYWRIGHT_CHROME_PROFILE to a different profile directory."
    )


def _existing_cdp_endpoint_for_profile(profile_dir: Path) -> str | None:
    processes = _chrome_processes_for_profile(profile_dir)
    if not processes:
        return None

    for process in processes:
        if process.remote_debugging_port is None:
            continue
        endpoint = f"http://127.0.0.1:{process.remote_debugging_port}"
        if _cdp_endpoint_is_reachable(endpoint):
            return endpoint

    raise RuntimeError(_profile_in_use_without_cdp_message(profile_dir, processes))


def _ensure_profile_not_in_use(profile_dir: Path) -> None:
    processes = _chrome_processes_for_profile(profile_dir)
    if processes:
        joined = ", ".join(str(process.pid) for process in processes)
        raise RuntimeError(
            "Chrome profile is already in use by process(es) "
            f"{joined}: {profile_dir}. Close that Chrome instance, choose the blank "
            "Headless isolated runtime, or set CODEX_PRO_PLAYWRIGHT_CHROME_PROFILE "
            "to a different profile directory."
        )


def _terminate_chrome_for_profile(chrome: subprocess.Popen[object], profile_dir: Path) -> None:
    _terminate(chrome)
    _terminate_chrome_processes_for_profile(profile_dir)


def _terminate_chrome_processes_for_profile(profile_dir: Path) -> None:
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


def _release_lease_and_maybe_chrome(
    profile_dir: Path,
    lease_path: Path,
    chrome: subprocess.Popen[object] | None,
    *,
    keep_chrome: bool = False,
) -> None:
    keep_chrome = keep_chrome or os.environ.get("CODEX_PRO_PLAYWRIGHT_KEEP_CHROME") == "1"
    with _profile_runtime_lock(profile_dir):
        lease_path.unlink(missing_ok=True)
        live_leases = _live_lease_pids(profile_dir)
        state = _read_managed_state(profile_dir)
        if state is not None and not _managed_state_is_live(profile_dir, state):
            _managed_state_path(profile_dir).unlink(missing_ok=True)
            state = None

    if keep_chrome:
        _log(f"leaving persistent managed Chrome running profile={profile_dir}")
        return

    if state is None:
        if chrome is not None and chrome.poll() is None:
            _log(f"cleaning unregistered Chrome after startup failure profile={profile_dir}")
            _terminate_chrome_for_profile(chrome, profile_dir)
        return

    if live_leases:
        _log(f"handed managed Chrome to wrapper pid(s)={live_leases} profile={profile_dir}")
        return

    deadline = time.monotonic() + _handoff_grace_sec()
    while time.monotonic() < deadline:
        time.sleep(0.1)
        with _profile_runtime_lock(profile_dir):
            live_leases = _live_lease_pids(profile_dir)
            current_state = _read_managed_state(profile_dir)
        if live_leases:
            _log(f"handed managed Chrome to wrapper pid(s)={live_leases} profile={profile_dir}")
            return
        if current_state is None:
            return

    # Keep the lock while terminating so a late replacement cannot attach to a
    # browser that has already been selected for teardown.
    with _profile_runtime_lock(profile_dir):
        live_leases = _live_lease_pids(profile_dir)
        current_state = _read_managed_state(profile_dir)
        if live_leases:
            _log(f"handed managed Chrome to wrapper pid(s)={live_leases} profile={profile_dir}")
            return
        if current_state is None or not _managed_state_is_live(profile_dir, current_state):
            _managed_state_path(profile_dir).unlink(missing_ok=True)
            return
        _log(f"closing last managed Chrome pid={current_state.pid} profile={profile_dir}")
        _managed_state_path(profile_dir).unlink(missing_ok=True)
        if chrome is not None and chrome.pid == current_state.pid:
            _terminate_chrome_for_profile(chrome, profile_dir)
        else:
            _terminate_chrome_processes_for_profile(profile_dir)


def _playwright_mcp_node_modules() -> Path:
    npm = shutil.which("npm")
    if not npm:
        raise RuntimeError("npm executable not found")
    result = subprocess.run(
        [
            npm,
            "exec",
            "--yes",
            "--package=@playwright/mcp@latest",
            "--package=@modelcontextprotocol/sdk@latest",
            "--",
            "sh",
            "-c",
            'realpath "$(command -v playwright-mcp)"',
        ],
        text=True,
        capture_output=True,
        timeout=90,
    )
    if result.returncode != 0 or not result.stdout.strip():
        raise RuntimeError("Unable to resolve the installed Playwright MCP package")
    cli_path = Path(result.stdout.strip()).resolve()
    node_modules = cli_path.parents[2]
    if not (node_modules / "@playwright" / "mcp").is_dir():
        raise RuntimeError("Resolved Playwright MCP package layout is invalid")
    return node_modules


def _owned_context_mcp_command(
    cdp_endpoint: str,
    viewport_size: str | None,
    *,
    shared_context: bool = False,
) -> tuple[list[str], dict[str, str]]:
    node = shutil.which("node")
    if not node:
        raise RuntimeError("node executable not found")
    helper_name = "playwright_shared_context_mcp.cjs" if shared_context else "playwright_owned_context_mcp.cjs"
    helper = Path(__file__).with_name(helper_name)
    if not helper.is_file():
        raise RuntimeError(f"Playwright context MCP helper is missing: {helper_name}")
    node_modules = _playwright_mcp_node_modules()
    command = [node, str(helper), "--cdp-endpoint", cdp_endpoint]
    if viewport_size:
        command.extend(["--viewport-size", viewport_size])
    env = os.environ.copy()
    env[PLAYWRIGHT_LOG_ENV] = str(_playwright_log_path())
    existing_node_path = env.get("NODE_PATH")
    env["NODE_PATH"] = str(node_modules) + (os.pathsep + existing_node_path if existing_node_path else "")
    return command, env


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(description="Start normal Chrome and bridge Playwright MCP over CDP.")
    parser.add_argument("--user-data-dir", required=True)
    parser.add_argument("--headless", action="store_true")
    parser.add_argument("--shared-context", action="store_true")
    parser.add_argument("--viewport-size")
    parser.add_argument("--port", type=int, default=0)
    args = parser.parse_args(argv)

    profile_dir = Path(args.user_data_dir).expanduser()
    profile_dir.mkdir(parents=True, exist_ok=True)
    _log(
        f"wrapper starting profile={profile_dir} headless={args.headless} "
        f"context={'shared' if args.shared_context else 'owned'}"
    )

    chrome: subprocess.Popen[object] | None = None
    lease_path: Path | None = None
    mcp: subprocess.Popen[object] | None = None

    def stop_children(signum: int, _frame: object) -> None:
        _log(f"wrapper received signal={signum} profile={profile_dir}")
        if mcp is not None and mcp.poll() is None:
            mcp.terminate()
        raise SystemExit(128 + signum)

    signal.signal(signal.SIGINT, stop_children)
    signal.signal(signal.SIGTERM, stop_children)

    try:
        with _profile_runtime_lock(profile_dir):
            _live_lease_pids(profile_dir)
            lease_path = _register_lease(profile_dir)
            cdp_endpoint = _existing_cdp_endpoint_for_profile(profile_dir)
            state = _read_managed_state(profile_dir)
            if state is not None and not _managed_state_is_live(profile_dir, state):
                _managed_state_path(profile_dir).unlink(missing_ok=True)
                state = None
            if cdp_endpoint is None:
                if sys.platform == "darwin" and not args.headless and _macos_console_locked():
                    _log("macOS console is locked; the headed Chrome window will not be visible on screen")
                    print(
                        "[playwright] macOS console is locked (IOConsoleLocked=Yes). The headed browser "
                        "opens on a locked screen, so it cannot be seen even though it is running. "
                        "Unlock the Mac, or target a mesh device whose screen you are actually looking at.",
                        file=sys.stderr,
                        flush=True,
                    )
                port = args.port or _free_port()
                cdp_endpoint = f"http://127.0.0.1:{port}"
                executable = _find_chrome()
                previous_frontmost = (
                    _frontmost_bundle_identifier()
                    if sys.platform == "darwin" and not args.headless and not _allow_foreground()
                    else None
                )
                chrome_args = [
                    f"--remote-debugging-port={port}",
                    f"--user-data-dir={profile_dir}",
                    "--no-first-run",
                    "--no-default-browser-check",
                    "about:blank",
                ]
                if args.headless:
                    chrome_args.insert(0, "--headless=new")
                chrome_cmd = _chrome_launch_command(executable, chrome_args, headless=args.headless)
                _log(
                    f"launching Chrome background={not _allow_foreground() and not args.headless} "
                    f"command={chrome_cmd[0]} profile={profile_dir}"
                )
                chrome = subprocess.Popen(
                    chrome_cmd,
                    stdout=subprocess.DEVNULL,
                    stderr=subprocess.DEVNULL,
                    start_new_session=True,
                )
                _log(f"launched managed Chrome pid={chrome.pid} endpoint={cdp_endpoint} profile={profile_dir}")
                _wait_for_cdp(cdp_endpoint)
                managed_pid = (
                    _wait_for_main_chrome_pid(profile_dir)
                    if sys.platform == "darwin" and not args.headless and not _allow_foreground()
                    else chrome.pid
                )
                _write_managed_state(profile_dir, ManagedChromeState(pid=managed_pid, port=port))
                _restore_frontmost_bundle_identifier(previous_frontmost)
                if sys.platform == "darwin" and not args.headless and _allow_foreground():
                    _activate_browser_app(executable)
            elif state is not None:
                _log(f"attached to managed Chrome pid={state.pid} endpoint={cdp_endpoint} profile={profile_dir}")
            else:
                _log(f"attached to external Chrome endpoint={cdp_endpoint} profile={profile_dir}")

        mcp_cmd, mcp_env = _owned_context_mcp_command(
            cdp_endpoint,
            args.viewport_size,
            shared_context=args.shared_context,
        )
        mcp = subprocess.Popen(mcp_cmd, env=mcp_env)
        context_mode = "shared" if args.shared_context else "owned"
        _log(f"started {context_mode}-context MCP pid={mcp.pid} endpoint={cdp_endpoint}")
        return_code = mcp.wait()
        _log(f"{context_mode}-context MCP exited code={return_code} endpoint={cdp_endpoint}")
        return return_code
    finally:
        if lease_path is not None:
            _release_lease_and_maybe_chrome(
                profile_dir,
                lease_path,
                chrome,
                keep_chrome=args.shared_context and not args.headless,
            )
        elif chrome is not None and chrome.poll() is None:
            _terminate_chrome_for_profile(chrome, profile_dir)


if __name__ == "__main__":
    try:
        raise SystemExit(main(sys.argv[1:]))
    except Exception as exc:
        detail = traceback.format_exc()
        print(f"playwright chrome cdp wrapper failed: {exc}\n{detail}", file=sys.stderr)
        _log(f"wrapper failed: {exc}\n{detail.rstrip()}")
        raise SystemExit(1)
