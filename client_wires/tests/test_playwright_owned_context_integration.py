from __future__ import annotations

from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import shutil
import signal
import socket
import subprocess
import sys
import tempfile
import threading
import time
import unittest
import urllib.request


WRAPPER = Path(__file__).parents[1] / "codex_compat" / "playwright_chrome_cdp.py"
CHROME = Path("/Applications/Google Chrome.app/Contents/MacOS/Google Chrome")


class _CookieHandler(BaseHTTPRequestHandler):
    seen_cookie = ""

    def do_GET(self) -> None:  # noqa: N802 - stdlib callback name
        body = b"fixture"
        self.send_response(200)
        self.send_header("content-type", "text/html; charset=utf-8")
        if self.path == "/set-cookie":
            self.send_header("set-cookie", "owned_context_session=survives; Path=/; Max-Age=3600; SameSite=Lax")
            body = b"cookie set"
        elif self.path == "/cookie-echo":
            type(self).seen_cookie = self.headers.get("cookie") or ""
            body = (type(self).seen_cookie or "cookie missing").encode()
        self.end_headers()
        self.wfile.write(b"<!doctype html><html><body>" + body + b"</body></html>")

    def log_message(self, _format: str, *_args: object) -> None:
        return


@unittest.skipUnless(CHROME.is_file() and shutil.which("npm") and shutil.which("node"), "Chrome/npm/node required")
class PlaywrightOwnedContextIntegrationTests(unittest.TestCase):
    def test_existing_tab_is_hidden_cookie_survives_and_attached_chrome_stays_running(self) -> None:
        _CookieHandler.seen_cookie = ""
        with tempfile.TemporaryDirectory() as tmp:
            profile = Path(tmp) / "profile"
            port = _free_port()
            server = ThreadingHTTPServer(("127.0.0.1", 0), _CookieHandler)
            server_thread = threading.Thread(target=server.serve_forever, daemon=True)
            server_thread.start()
            origin = f"http://127.0.0.1:{server.server_address[1]}"
            chrome = subprocess.Popen(
                [
                    str(CHROME),
                    "--headless=new",
                    "--remote-debugging-address=127.0.0.1",
                    f"--remote-debugging-port={port}",
                    f"--user-data-dir={profile}",
                    "--no-first-run",
                    "--no-default-browser-check",
                    "about:blank",
                ],
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
            )
            try:
                _wait_cdp(port)
                original_pages = _page_targets(port)
                self.assertEqual(len(original_pages), 1)
                original_target = (original_pages[0]["id"], original_pages[0]["url"])

                first = _start_wrapper(profile)
                try:
                    _initialize(first)
                    self.assertEqual(_main_chrome_pids(profile), [chrome.pid], "wrapper launched a duplicate profile process")
                    tabs = _call(first, 2, "browser_tabs", {"action": "list"})
                    text = _result_text(tabs)
                    self.assertEqual(text.count("- 0:"), 1)
                    self.assertNotIn("- 1:", text)
                    _call(first, 3, "browser_navigate", {"url": f"{origin}/set-cookie"})
                finally:
                    _stop_wrapper(first)

                self.assertIsNone(chrome.poll(), "attached wrapper terminated the existing Chrome")
                self.assertIn(original_target, [(page["id"], page["url"]) for page in _page_targets(port)])

                chrome.terminate()
                chrome.wait(timeout=8)

                second = _start_wrapper(profile)
                try:
                    _initialize(second)
                    self.assertEqual(len(_main_chrome_pids(profile)), 1, "owned headless restart launched duplicate profile processes")
                    _call(second, 4, "browser_navigate", {"url": f"{origin}/cookie-echo"})
                    self.assertIn("owned_context_session=survives", _CookieHandler.seen_cookie)
                finally:
                    _stop_wrapper(second)

                self.assertEqual(_main_chrome_pids(profile), [], "owned headless cleanup left its Chrome running")
            finally:
                server.shutdown()
                server.server_close()
                chrome.terminate()
                try:
                    chrome.wait(timeout=8)
                except subprocess.TimeoutExpired:
                    chrome.kill()
                    chrome.wait(timeout=5)

    def test_managed_shared_chrome_survives_last_wrapper_and_reconnects(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            profile = Path(tmp) / "profile"
            first = _start_wrapper(profile, shared_context=True, headless=False)
            second: subprocess.Popen[str] | None = None
            try:
                _initialize(first)
                original_pids = _main_chrome_pids(profile)
                self.assertEqual(len(original_pids), 1)
                _call(first, 2, "browser_tabs", {"action": "list"})
                _stop_wrapper(first)

                self.assertEqual(
                    _main_chrome_pids(profile),
                    original_pids,
                    "last shared wrapper closed the persistent headed Chrome",
                )

                second = _start_wrapper(profile, shared_context=True, headless=False)
                _initialize(second)
                self.assertEqual(
                    _main_chrome_pids(profile),
                    original_pids,
                    "replacement shared wrapper launched a second Chrome",
                )
                _call(second, 3, "browser_tabs", {"action": "list"})
                _stop_wrapper(second)
                self.assertEqual(_main_chrome_pids(profile), original_pids)
            finally:
                for process in (first, second):
                    if process is not None and process.poll() is None:
                        _stop_wrapper(process)
                _terminate_profile_chrome(profile)

    def test_owned_chrome_survives_wrapper_handoff_and_last_wrapper_cleans_it(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            profile = Path(tmp) / "profile"
            first = _start_wrapper(profile)
            second: subprocess.Popen[str] | None = None
            try:
                _initialize(first)
                original_pids = _main_chrome_pids(profile)
                self.assertEqual(len(original_pids), 1)

                os.killpg(first.pid, signal.SIGTERM)
                second = _start_wrapper(profile)
                _initialize(second)

                first.wait(timeout=15)
                self.assertIn(
                    first.returncode,
                    (0, 128 + signal.SIGTERM),
                    first.stderr.read() if first.stderr else "",
                )
                self.assertEqual(
                    _main_chrome_pids(profile),
                    original_pids,
                    "wrapper replacement killed and relaunched the managed Chrome",
                )
                _call(second, 2, "browser_tabs", {"action": "list"})
            finally:
                if first.poll() is None:
                    _stop_wrapper(first)
                else:
                    for stream in (first.stdin, first.stdout, first.stderr):
                        if stream:
                            stream.close()
                if second is not None and second.poll() is None:
                    _stop_wrapper(second)

            self.assertEqual(_main_chrome_pids(profile), [], "last wrapper left managed Chrome running")

    def test_managed_chrome_disconnect_terminates_wrapper_and_clears_runtime(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            profile = Path(tmp) / "profile"
            wrapper = _start_wrapper(profile)
            try:
                _initialize(wrapper)
                chrome_pids = _main_chrome_pids(profile)
                self.assertEqual(len(chrome_pids), 1)
                os.kill(chrome_pids[0], signal.SIGTERM)
                wrapper.wait(timeout=20)
                self.assertEqual(wrapper.returncode, 1)
            finally:
                if wrapper.poll() is None:
                    os.killpg(wrapper.pid, signal.SIGTERM)
                    wrapper.wait(timeout=15)
                for stream in (wrapper.stdin, wrapper.stdout, wrapper.stderr):
                    if stream:
                        stream.close()

            self.assertEqual(_main_chrome_pids(profile), [], "browser crash left profile Chrome running")
            self.assertFalse(
                (profile / ".codex-playwright-runtime" / "managed-chrome.json").exists(),
                "browser crash left managed Chrome state behind",
            )

    def test_shared_context_exposes_and_preserves_persistent_tabs(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            profile = Path(tmp) / "profile"
            port = _free_port()
            server = ThreadingHTTPServer(("127.0.0.1", 0), _CookieHandler)
            server_thread = threading.Thread(target=server.serve_forever, daemon=True)
            server_thread.start()
            origin = f"http://127.0.0.1:{server.server_address[1]}"
            chrome = subprocess.Popen(
                [
                    str(CHROME),
                    "--headless=new",
                    "--remote-debugging-address=127.0.0.1",
                    f"--remote-debugging-port={port}",
                    f"--user-data-dir={profile}",
                    "--no-first-run",
                    "--no-default-browser-check",
                    f"{origin}/existing",
                ],
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
            )
            try:
                _wait_cdp(port)
                wrapper = _start_wrapper(profile, shared_context=True)
                try:
                    _initialize(wrapper)
                    listed = _result_text(_call(wrapper, 2, "browser_tabs", {"action": "list"}))
                    self.assertIn(f"{origin}/existing", listed)
                    _call(wrapper, 3, "browser_tabs", {"action": "new", "url": f"{origin}/agent"})
                    urls = {page["url"] for page in _page_targets(port)}
                    self.assertIn(f"{origin}/existing", urls)
                    self.assertIn(f"{origin}/agent", urls)
                finally:
                    _stop_wrapper(wrapper)

                self.assertIsNone(chrome.poll(), "shared-context wrapper terminated external Chrome")
                urls = {page["url"] for page in _page_targets(port)}
                self.assertIn(f"{origin}/existing", urls)
                self.assertIn(f"{origin}/agent", urls)
            finally:
                server.shutdown()
                server.server_close()
                chrome.terminate()
                try:
                    chrome.wait(timeout=8)
                except subprocess.TimeoutExpired:
                    chrome.kill()
                    chrome.wait(timeout=5)


def _free_port() -> int:
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return int(sock.getsockname()[1])


def _wait_cdp(port: int) -> None:
    deadline = time.monotonic() + 15
    while time.monotonic() < deadline:
        try:
            with urllib.request.urlopen(f"http://127.0.0.1:{port}/json/version", timeout=0.5) as response:
                if response.status == 200:
                    return
        except OSError:
            time.sleep(0.1)
    raise AssertionError("synthetic Chrome CDP did not become ready")


def _page_targets(port: int) -> list[dict[str, str]]:
    with urllib.request.urlopen(f"http://127.0.0.1:{port}/json/list", timeout=2) as response:
        return [target for target in json.load(response) if target.get("type") == "page"]


def _main_chrome_pids(profile: Path) -> list[int]:
    result = subprocess.run(["ps", "axo", "pid=,command="], text=True, capture_output=True, check=True)
    marker = f"--user-data-dir={profile}"
    return sorted(
        int(line.strip().split(maxsplit=1)[0])
        for line in result.stdout.splitlines()
        if str(CHROME) in line and marker in line
    )


def _terminate_profile_chrome(profile: Path) -> None:
    for pid in _main_chrome_pids(profile):
        try:
            os.kill(pid, signal.SIGTERM)
        except ProcessLookupError:
            pass
    deadline = time.monotonic() + 8
    while time.monotonic() < deadline and _main_chrome_pids(profile):
        time.sleep(0.1)
    for pid in _main_chrome_pids(profile):
        try:
            os.kill(pid, signal.SIGKILL)
        except ProcessLookupError:
            pass


def _start_wrapper(
    profile: Path,
    *,
    shared_context: bool = False,
    headless: bool = True,
) -> subprocess.Popen[str]:
    env = os.environ.copy()
    env["CODEX_PRO_PLAYWRIGHT_HANDOFF_GRACE_SEC"] = "2"
    env["CODEX_PRO_PLAYWRIGHT_LOG"] = str(profile.parent / "playwright-mcp-test.log")
    command = [sys.executable, str(WRAPPER), "--user-data-dir", str(profile)]
    if headless:
        command.append("--headless")
    if shared_context:
        command.append("--shared-context")
    return subprocess.Popen(
        command,
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        bufsize=1,
        env=env,
        start_new_session=True,
    )


def _initialize(process: subprocess.Popen[str]) -> None:
    response = _request(
        process,
        {
            "jsonrpc": "2.0",
            "id": 1,
            "method": "initialize",
            "params": {
                "protocolVersion": "2025-06-18",
                "capabilities": {},
                "clientInfo": {"name": "owned-context-test", "version": "1.0.0"},
            },
        },
    )
    if "result" not in response:
        raise AssertionError(response)
    assert process.stdin is not None
    process.stdin.write('{"jsonrpc":"2.0","method":"notifications/initialized"}\n')
    process.stdin.flush()


def _call(process: subprocess.Popen[str], request_id: int, name: str, arguments: dict[str, object]) -> dict[str, object]:
    response = _request(
        process,
        {
            "jsonrpc": "2.0",
            "id": request_id,
            "method": "tools/call",
            "params": {"name": name, "arguments": arguments},
        },
    )
    if "error" in response:
        raise AssertionError(response)
    return response


def _request(process: subprocess.Popen[str], payload: dict[str, object]) -> dict[str, object]:
    assert process.stdin is not None and process.stdout is not None
    process.stdin.write(json.dumps(payload) + "\n")
    process.stdin.flush()
    deadline = time.monotonic() + 30
    while time.monotonic() < deadline:
        line = process.stdout.readline()
        if line:
            response = json.loads(line)
            if response.get("id") == payload.get("id"):
                return response
        elif process.poll() is not None:
            stderr = process.stderr.read() if process.stderr else ""
            raise AssertionError(f"wrapper exited early: {stderr}")
    raise AssertionError("timed out waiting for MCP response")


def _result_text(response: dict[str, object]) -> str:
    result = response.get("result")
    if not isinstance(result, dict):
        return ""
    content = result.get("content")
    if not isinstance(content, list):
        return ""
    return "\n".join(item.get("text", "") for item in content if isinstance(item, dict))


def _stop_wrapper(process: subprocess.Popen[str]) -> None:
    if process.stdin:
        process.stdin.close()
    try:
        process.wait(timeout=15)
    except subprocess.TimeoutExpired:
        process.terminate()
        process.wait(timeout=10)
    if process.returncode != 0:
        stderr = process.stderr.read() if process.stderr else ""
        raise AssertionError(f"wrapper failed during cleanup: {stderr}")
    for stream in (process.stdout, process.stderr):
        if stream:
            stream.close()


if __name__ == "__main__":
    unittest.main()
