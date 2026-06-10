#!/usr/bin/env python3
"""Real-Codex Docker bootstrap E2E for Fixer MCP.

This runner intentionally uses a fresh SQLite DB and a toy project inside the
container. Python performs only deterministic setup and evidence collection;
the actual Fixer/Netrunner behavior is driven by Codex CLI with fixer_mcp
mounted over stdio.
"""

from __future__ import annotations

import json
import os
import shutil
import shlex
import sqlite3
import subprocess
import sys
import textwrap
import time
from pathlib import Path
from typing import Any


ROOT_DIR = Path(os.environ.get("ROOT_DIR", "/workspace/self_orchestration")).resolve()
OUT_DIR = Path(os.environ.get("OUT_DIR", "/bootstrap-out")).resolve()
DB_PATH = Path(os.environ.get("FIXER_BOOTSTRAP_E2E_DB_PATH", OUT_DIR / "fixer-bootstrap-e2e.db")).resolve()
FIXER_BINARY = Path(os.environ.get("FIXER_BOOTSTRAP_E2E_BINARY", ROOT_DIR / "fixer_mcp" / "fixer_mcp")).resolve()
MODEL = os.environ.get("BOOTSTRAP_E2E_MODEL", "gpt-5.5").strip() or "gpt-5.5"
REASONING = os.environ.get("BOOTSTRAP_E2E_REASONING", "high").strip() or "high"
TIMEOUT_SECONDS = int(os.environ.get("BOOTSTRAP_E2E_TIMEOUT_SECONDS", "3600"))
TOY_DIR = Path(os.environ.get("FIXER_BOOTSTRAP_E2E_TOY_DIR", "/workspace/bootstrap-toy-todo")).resolve()


class MCPStdioClient:
    def __init__(self, command: list[str], *, cwd: Path, env: dict[str, str]) -> None:
        self._next_id = 1
        self._process = subprocess.Popen(
            command,
            cwd=str(cwd),
            env=env,
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            bufsize=1,
        )

    def close(self) -> None:
        if self._process.poll() is None:
            self._process.terminate()
            try:
                self._process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                self._process.kill()
                self._process.wait(timeout=5)

    def _write(self, payload: dict[str, Any]) -> None:
        if self._process.stdin is None:
            raise RuntimeError("MCP server stdin is closed")
        self._process.stdin.write(json.dumps(payload) + "\n")
        self._process.stdin.flush()

    def _read_response(self, expected_id: int) -> dict[str, Any]:
        if self._process.stdout is None:
            raise RuntimeError("MCP server stdout is closed")
        while True:
            line = self._process.stdout.readline()
            if line:
                message = json.loads(line)
                if message.get("id") == expected_id:
                    if "error" in message:
                        raise AssertionError(f"MCP error response for id={expected_id}: {message['error']}")
                    return message
                continue
            stderr = ""
            if self._process.stderr is not None:
                stderr = self._process.stderr.read()
            raise RuntimeError(f"MCP server exited before response id={expected_id}; stderr={stderr}")

    def request(self, method: str, params: dict[str, Any]) -> dict[str, Any]:
        request_id = self._next_id
        self._next_id += 1
        self._write({"jsonrpc": "2.0", "id": request_id, "method": method, "params": params})
        return self._read_response(request_id)

    def notify(self, method: str, params: dict[str, Any]) -> None:
        self._write({"jsonrpc": "2.0", "method": method, "params": params})

    def initialize(self) -> None:
        response = self.request(
            "initialize",
            {
                "protocolVersion": "2025-06-18",
                "capabilities": {},
                "clientInfo": {"name": "fixer-bootstrap-e2e", "version": "0.1.0"},
            },
        )
        assert response["result"]["serverInfo"]["name"] == "fixer_mcp", response
        self.notify("notifications/initialized", {})

    def call_tool(self, name: str, arguments: dict[str, Any]) -> dict[str, Any]:
        response = self.request("tools/call", {"name": name, "arguments": arguments})
        result = response["result"]
        if result.get("isError"):
            raise AssertionError(f"tool {name} returned isError: {result}")
        if "structuredContent" in result:
            return result["structuredContent"]
        content = result.get("content") or []
        if content and content[0].get("type") == "text":
            return json.loads(content[0]["text"])
        return {}


def run(cmd: list[str], *, cwd: Path, env: dict[str, str], stdout_path: Path, timeout: int) -> int:
    stdout_path.parent.mkdir(parents=True, exist_ok=True)
    with stdout_path.open("w", encoding="utf-8") as log:
        log.write("$ " + " ".join(shlex.quote(part) for part in cmd) + "\n")
        log.flush()
        process = subprocess.run(
            cmd,
            cwd=str(cwd),
            env=env,
            stdout=log,
            stderr=subprocess.STDOUT,
            text=True,
            timeout=timeout,
        )
    return int(process.returncode)


def codex_mcp_flags(role: str) -> list[str]:
    env_payload = {
        "FIXER_DB_PATH": str(DB_PATH),
        "FIXER_MCP_LOCKED_ROLE": role,
    }
    if role == "netrunner":
        env_payload["FIXER_MCP_DEFAULT_ROLE"] = "netrunner"
        env_payload["FIXER_MCP_DEFAULT_CWD"] = str(TOY_DIR)
    return [
        "-c",
        "mcp_servers.fixer_mcp.enabled=true",
        "-c",
        f"mcp_servers.fixer_mcp.command={json.dumps(str(FIXER_BINARY))}",
        "-c",
        "mcp_servers.fixer_mcp.args=[]",
        "-c",
        f"mcp_servers.fixer_mcp.cwd={json.dumps(str(FIXER_BINARY.parent))}",
        "-c",
        f"mcp_servers.fixer_mcp.env={toml_inline_table(env_payload)}",
        "-c",
        "mcp_servers.fixer_mcp.startup_timeout_sec=30",
        "-c",
        "mcp_servers.fixer_mcp.timeout=21600",
        "-c",
        "mcp_servers.fixer_mcp.tool_timeout_sec=21600",
        "-c",
        "mcp_servers.fixer_mcp.per_tool_timeout_ms=21600000",
    ]


def toml_inline_table(values: dict[str, str]) -> str:
    parts = [f"{key}={json.dumps(value)}" for key, value in sorted(values.items())]
    return "{" + ", ".join(parts) + "}"


def prepare_toy_project() -> None:
    TOY_DIR.mkdir(parents=True, exist_ok=True)
    (TOY_DIR / "README.md").write_text(
        "# Bootstrap Toy Todo\n\nA minimal project used only by docker-bootstrap-e2e.\n",
        encoding="utf-8",
    )
    (TOY_DIR / "AGENTS.md").write_text(
        textwrap.dedent(
            """\
            # Bootstrap Toy Todo

            Keep changes minimal. For the E2E worker, implement a tiny Python todo CLI and tests.
            """
        ),
        encoding="utf-8",
    )


def deterministic_onboarding() -> dict[str, Any]:
    env = dict(os.environ)
    env["FIXER_DB_PATH"] = str(DB_PATH)
    client = MCPStdioClient([str(FIXER_BINARY)], cwd=FIXER_BINARY.parent, env=env)
    try:
        client.initialize()
        overseer = client.call_tool("assume_role", {"role": "overseer", "token": "supersecret"})
        project = client.call_tool("register_project", {"cwd": str(TOY_DIR), "name": "Docker Bootstrap Toy Todo"})
        return {"overseer": overseer, "project": project}
    finally:
        client.close()


def build_fixer_prompt() -> str:
    return textwrap.dedent(
        f"""\
        Activate skill `$init-fixer` immediately if it is available.

        You are the real Codex-backed Fixer for the Docker bootstrap E2E.
        This is a clean Linux container with only the forced `fixer_mcp` server mounted.
        Do not ask the operator for confirmation.

        Required actions:
        1. Call `fixer_mcp.assume_role` with role `fixer`, cwd `{TOY_DIR}`, token `supersecret`.
        2. Confirm the role surface is locked to Fixer by using Fixer tools only.
        3. Call `fixer_mcp.list_mcp_servers` with `include_all=true` and `include_archived=true`.
           Verify that marketplace metadata is visible, especially `portability`, `install_hint`,
           `auth_env_keys`, and `archived`.
        4. Call `fixer_mcp.get_project_mcp_servers`.
        5. Create exactly one Netrunner task with declared_write_scope `["."]`:
           "Implement a minimal Python todo CLI in app.py with unittest coverage in test_app.py.
           Keep it tiny: add/list/clear helpers and tests only. Submit the mandatory doc proposal
           and structured final report."
        6. Launch that worker through `fixer_mcp.launch_and_wait_netrunner` using backend `codex`,
           model `{MODEL}`, reasoning `{REASONING}`, timeout_seconds `{TIMEOUT_SECONDS}`, and
           poll_interval_seconds `5`.
        7. In the final answer, return compact JSON with:
           `stage1_bootstrap_pass`, `marketplace_metadata_visible`, `stage2_worker_pass`,
           `worker_session_status`, `proposal_ids`, `deliverables`, and `friction`.

        Pass criteria:
        - Stage 1 passes only if assume_role succeeds and list_mcp_servers exposes marketplace metadata.
        - Stage 2 passes only if launch_and_wait_netrunner reaches review or completed with worker
          deliverables and at least one doc proposal.
        """
    )


def collect_db_evidence() -> dict[str, Any]:
    if not DB_PATH.is_file():
        return {"db_exists": False}
    conn = sqlite3.connect(DB_PATH)
    conn.row_factory = sqlite3.Row
    try:
        sessions = [dict(row) for row in conn.execute(
            "SELECT id, task_description, status, report, cli_backend, cli_model, cli_reasoning FROM session ORDER BY id"
        )]
        proposals = [dict(row) for row in conn.execute(
            "SELECT id, session_id, status, proposed_doc_type FROM doc_proposal ORDER BY id"
        )]
        mcp_rows = [dict(row) for row in conn.execute(
            "SELECT name, portability, install_hint, auth_env_keys, archived FROM mcp_server ORDER BY name LIMIT 80"
        )]
    finally:
        conn.close()
    return {
        "db_exists": True,
        "sessions": sessions,
        "doc_proposals": proposals,
        "mcp_registry_sample": mcp_rows,
    }


def copy_headless_netrunner_logs() -> Path | None:
    source = TOY_DIR / ".codex" / "headless_netrunner_logs"
    if not source.is_dir():
        return None
    destination = OUT_DIR / "headless_netrunner_logs"
    shutil.copytree(source, destination, dirs_exist_ok=True)
    return destination


def write_runtime_friction(status: str, evidence: dict[str, Any], fixer_log: Path) -> None:
    sessions = evidence.get("sessions") or []
    proposals = evidence.get("doc_proposals") or []
    final_session = sessions[-1] if sessions else {}
    green = final_session.get("status") in {"review", "completed"} and bool(proposals)
    content = textwrap.dedent(
        f"""\
        # Docker Bootstrap E2E Runtime Friction

        Status: {status}
        Generated at: {time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime())}

        ## Evidence

        - Fixer Codex log: `{fixer_log}`
        - SQLite DB: `{DB_PATH}`
        - Headless Netrunner logs: `{OUT_DIR / 'headless_netrunner_logs'}`
        - Stage 2 green: `{green}`
        - Sessions: `{len(sessions)}`
        - Doc proposals: `{len(proposals)}`

        ## Friction Observed

        1. Fixer MCP's headless launcher now uses repo-vendored `client_wires.codex_compat`;
           no external `mcp_servers/codex_pro_app` mount is required.
        2. `fixer_mcp/mcp_config.json` in the source tree contains an absolute host path; this harness
           bypasses it with runtime Codex `-c mcp_servers.fixer_mcp.*` overrides.
        3. If `launch_and_wait_netrunner` fails in this run after the launcher runtime is mounted,
           inspect the Fixer Codex log first and record any new product-level friction here.

        ## Raw DB Evidence

        ```json
        {json.dumps(evidence, indent=2)}
        ```
        """
    )
    (OUT_DIR / "FRICTION_RUNTIME.md").write_text(content, encoding="utf-8")


def main() -> int:
    OUT_DIR.mkdir(parents=True, exist_ok=True)
    prepare_toy_project()
    onboarding = deterministic_onboarding()
    (OUT_DIR / "onboarding.json").write_text(json.dumps(onboarding, indent=2), encoding="utf-8")

    env = dict(os.environ)
    env["CODEX_HOME"] = "/root/.codex"
    env["FIXER_DB_PATH"] = str(DB_PATH)

    fixer_prompt = build_fixer_prompt()
    (OUT_DIR / "fixer_prompt.md").write_text(fixer_prompt, encoding="utf-8")
    fixer_log = OUT_DIR / "stage1-stage2-fixer-codex.log"
    final_message = OUT_DIR / "stage1-stage2-fixer-final.txt"
    command = [
        "codex",
        "--model",
        MODEL,
        "-c",
        f'model_reasoning_effort="{REASONING}"',
        "--dangerously-bypass-approvals-and-sandbox",
        "--add-dir",
        str(ROOT_DIR),
        *codex_mcp_flags("fixer"),
        "exec",
        "--skip-git-repo-check",
        "--output-last-message",
        str(final_message),
        fixer_prompt,
    ]
    return_code = run(command, cwd=TOY_DIR, env=env, stdout_path=fixer_log, timeout=TIMEOUT_SECONDS + 900)
    copy_headless_netrunner_logs()
    evidence = collect_db_evidence()
    (OUT_DIR / "result.json").write_text(
        json.dumps({"return_code": return_code, "evidence": evidence}, indent=2),
        encoding="utf-8",
    )

    sessions = evidence.get("sessions") or []
    proposals = evidence.get("doc_proposals") or []
    terminal_statuses = {"review", "completed"}
    stage2_green = bool(sessions) and sessions[-1].get("status") in terminal_statuses and bool(proposals)
    status = "green" if return_code == 0 and stage2_green else "failed"
    write_runtime_friction(status, evidence, fixer_log)

    if status != "green":
        print(f"[bootstrap-e2e] failed; see {OUT_DIR / 'FRICTION_RUNTIME.md'}", file=sys.stderr)
        return 1
    print(f"[bootstrap-e2e] passed; evidence in {OUT_DIR}")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except subprocess.TimeoutExpired as exc:
        copy_headless_netrunner_logs()
        (OUT_DIR / "timeout.json").write_text(
            json.dumps({"cmd": exc.cmd, "timeout": exc.timeout}, indent=2),
            encoding="utf-8",
        )
        print(f"[bootstrap-e2e] timeout after {exc.timeout}s", file=sys.stderr)
        raise SystemExit(124)
