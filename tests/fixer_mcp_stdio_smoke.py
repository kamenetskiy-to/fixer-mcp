#!/usr/bin/env python3
"""Non-interactive clean-DB smoke test for the Fixer MCP stdio server."""

from __future__ import annotations

import json
import os
import sqlite3
import subprocess
import tempfile
from pathlib import Path
from typing import Any


class MCPStdioClient:
    def __init__(self, command: list[str], *, cwd: Path, env: dict[str, str]) -> None:
        self._next_id = 1
        self._process = subprocess.Popen(
            command,
            cwd=cwd,
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
                "clientInfo": {"name": "fixer-mcp-stdio-smoke", "version": "0.1.0"},
            },
        )
        server_info = response["result"]["serverInfo"]
        assert server_info["name"] == "fixer_mcp", response
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


def assert_no_host_paths(db_path: Path) -> None:
    conn = sqlite3.connect(db_path)
    try:
        rows = conn.execute("SELECT cwd FROM project ORDER BY id").fetchall()
        cwd_values = [row[0] for row in rows]
    finally:
        conn.close()
    assert cwd_values, "expected at least one project row"
    for cwd in cwd_values:
        assert "/home/operator/" not in cwd, f"host-specific path leaked into clean DB: {cwd}"


def main() -> int:
    binary = Path(os.environ.get("FIXER_SMOKE_BINARY", "/tmp/fixer_mcp_smoke/fixer_mcp")).resolve()
    if not binary.is_file():
        raise SystemExit(f"missing smoke binary: {binary}")

    with tempfile.TemporaryDirectory(prefix="fixer-mcp-smoke-") as tmp:
        tmp_path = Path(tmp)
        runtime_dir = tmp_path / "runtime"
        alpha_dir = tmp_path / "alpha"
        beta_dir = tmp_path / "beta"
        nested_dir = alpha_dir / "nested" / "child"
        for path in (runtime_dir, alpha_dir, beta_dir, nested_dir):
            path.mkdir(parents=True, exist_ok=True)

        db_path = runtime_dir / "fixer.db"
        env = dict(os.environ)
        env["FIXER_DB_PATH"] = str(db_path)

        client = MCPStdioClient([str(binary)], cwd=runtime_dir, env=env)
        try:
            client.initialize()
            assert db_path.is_file(), "server did not initialize a fresh SQLite DB"
            assert_no_host_paths(db_path)

            overseer = client.call_tool("assume_role", {"role": "overseer", "token": "supersecret"})
            assert overseer["status"] == "success", overseer

            alpha = client.call_tool(
                "register_project",
                {"cwd": str(alpha_dir), "name": "Alpha Smoke"},
            )
            assert alpha["status"] == "created", alpha

            alpha_again = client.call_tool(
                "register_project",
                {"cwd": str(alpha_dir), "name": "Ignored Duplicate Name"},
            )
            assert alpha_again["status"] == "exists", alpha_again
            assert alpha_again["project_id"] == alpha["project_id"], (alpha, alpha_again)

            nested = client.call_tool(
                "register_project",
                {"cwd": str(nested_dir), "name": "Ignored Nested Name"},
            )
            assert nested["status"] == "exists", nested
            assert nested["project_id"] == alpha["project_id"], (alpha, nested)

            beta = client.call_tool("register_project", {"cwd": str(beta_dir), "name": "Beta Smoke"})
            assert beta["status"] == "created", beta
            assert beta["project_id"] != alpha["project_id"], (alpha, beta)

            conn = sqlite3.connect(db_path)
            try:
                project_count = conn.execute("SELECT COUNT(*) FROM project").fetchone()[0]
            finally:
                conn.close()
            assert project_count == 3, f"expected seed + alpha + beta projects, got {project_count}"
            assert_no_host_paths(db_path)

            fixer = client.call_tool(
                "assume_role",
                {"role": "fixer", "cwd": str(alpha_dir), "token": "supersecret"},
            )
            assert fixer["status"] == "success", fixer

            created = client.call_tool(
                "create_task",
                {
                    "task_description": "Docker clean smoke task",
                    "declared_write_scope": ["."],
                },
            )
            assert created["status"] == "success", created
            assert created["session_id"] == 1, created

            netrunner = client.call_tool("assume_role", {"role": "netrunner", "cwd": str(nested_dir)})
            assert netrunner["status"] == "success", netrunner

            pending = client.call_tool("get_pending_tasks", {})
            assert pending["tasks"] == [
                {"session_id": created["session_id"], "task_description": "Docker clean smoke task"}
            ], pending

            checkout = client.call_tool("checkout_task", {"session_id": created["session_id"]})
            assert checkout["status"] == "success", checkout

            pending_after_checkout = client.call_tool("get_pending_tasks", {})
            assert pending_after_checkout["tasks"] == [], pending_after_checkout
        finally:
            client.close()

    print("fixer_mcp stdio smoke passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
