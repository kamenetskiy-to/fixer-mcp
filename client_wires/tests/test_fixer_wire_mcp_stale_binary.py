from __future__ import annotations

import json
import os
import shutil
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from client_wires import fixer_wire_mcp


class StaleFixerMcpBinaryHandshakeTests(unittest.TestCase):
    """The configured fixer_mcp command must point at the current payload.

    A stale binary behind a fresh MCP config is a stale-schema transport: the
    handshake fails closed with an actionable reconnect instruction instead of
    silently launching the old payload.
    """

    def setUp(self) -> None:
        self.tmp = Path(tempfile.mkdtemp(prefix="fixer_mcp_stale_"))
        self.repo_root = self.tmp / "release"
        self.module_dir = self.repo_root / "fixer_mcp"
        self.module_dir.mkdir(parents=True)
        self.current = self.module_dir / "fixer_mcp"
        self.current.write_bytes(b"#!/bin/sh\necho current-payload\n")
        self.current.chmod(0o755)
        # Probe/test environments must never inherit the live production DB.
        self._live_env_patch = patch.dict(
            os.environ, {"FIXER_DB_PATH": str(self.tmp / "probe.db")}
        )
        self._live_env_patch.start()

    def tearDown(self) -> None:
        self._live_env_patch.stop()
        shutil.rmtree(self.tmp, ignore_errors=True)

    def _repo_root(self) -> Path:
        return self.repo_root

    def test_probe_environments_never_inherit_the_live_database(self) -> None:
        environ = dict(os.environ)
        db_path = Path(environ.get("FIXER_DB_PATH", ""))
        self.assertTrue(
            str(db_path).startswith(str(self.tmp)),
            f"probe environment inherited a non-temporary FIXER_DB_PATH: {db_path}",
        )

    def _write_binary(self, name: str, payload: bytes) -> Path:
        path = self.tmp / name
        path.write_bytes(payload)
        path.chmod(0o755)
        return path

    def test_current_binary_passes(self) -> None:
        fixer_wire_mcp._verify_forced_fixer_binary_current(
            self.current, repo_root=self._repo_root
        )

    def test_identical_payload_copy_passes(self) -> None:
        twin = self._write_binary("twin", self.current.read_bytes())
        fixer_wire_mcp._verify_forced_fixer_binary_current(twin, repo_root=self._repo_root)

    def test_stale_binary_fails_closed_with_reconnect_guidance(self) -> None:
        stale = self._write_binary("stale-release", b"#!/bin/sh\necho old-1.0.9-payload\n")
        with self.assertRaises(RuntimeError) as raised:
            fixer_wire_mcp._verify_forced_fixer_binary_current(stale, repo_root=self._repo_root)
        message = str(raised.exception)
        self.assertIn("stale binary", message)
        self.assertIn("fail closed", message)
        self.assertIn("close and reopen", message.lower())
        self.assertIn("fixer doctor", message)

    def test_explicit_binary_override_is_sanctioned(self) -> None:
        stale = self._write_binary("operator-choice", b"#!/bin/sh\necho operator-payload\n")
        with patch.dict(os.environ, {"FIXER_MCP_BINARY": str(stale)}, clear=False):
            fixer_wire_mcp._verify_forced_fixer_binary_current(
                stale, repo_root=self._repo_root, environ=dict(os.environ)
            )

    def test_server_resolution_fails_closed_on_stale_config_command(self) -> None:
        stale = self._write_binary("configured-old", b"#!/bin/sh\necho configured-old-payload\n")
        servers = {
            fixer_wire_mcp.FORCED_MCP_SERVER: {
                "command": str(stale),
                "args": [],
                "transport": "stdio",
            }
        }
        environ = dict(os.environ)
        environ.pop("FIXER_MCP_BINARY", None)
        with self.assertRaises(RuntimeError) as raised:
            fixer_wire_mcp._ensure_forced_fixer_server_resolved(
                servers, repo_root=self._repo_root, environ=environ
            )
        self.assertIn("stale binary", str(raised.exception))

    def test_server_resolution_accepts_current_config_command(self) -> None:
        servers = {
            fixer_wire_mcp.FORCED_MCP_SERVER: {
                "command": str(self.current),
                "args": [],
                "transport": "stdio",
            }
        }
        environ = dict(os.environ)
        environ.pop("FIXER_MCP_BINARY", None)
        fixer_wire_mcp._ensure_forced_fixer_server_resolved(
            servers, repo_root=self._repo_root, environ=environ
        )

    def test_env_wrapper_config_checks_the_payload_not_the_wrapper(self) -> None:
        """The real config shape launches the payload through /usr/bin/env.

        The handshake must resolve the payload from wrapper args: comparing the
        wrapper itself against the payload would falsely fail closed on every
        installed release.
        """
        stale = self._write_binary("wrapped-stale", b"#!/bin/sh\necho old-payload\n")
        environ = dict(os.environ)
        environ.pop("FIXER_MCP_BINARY", None)
        with self.assertRaises(RuntimeError) as raised:
            fixer_wire_mcp._ensure_forced_fixer_server_resolved(
                {
                    fixer_wire_mcp.FORCED_MCP_SERVER: {
                        "command": "/usr/bin/env",
                        "args": ["-u", "FIXER_MCP_TOOL_PROFILE", str(stale)],
                        "transport": "stdio",
                    }
                },
                repo_root=self._repo_root,
                environ=environ,
            )
        message = str(raised.exception)
        self.assertIn("stale binary", message)
        self.assertIn(str(stale), message)
        self.assertNotIn("/usr/bin/env", message)

    def test_env_wrapper_config_with_current_payload_passes(self) -> None:
        environ = dict(os.environ)
        environ.pop("FIXER_MCP_BINARY", None)
        fixer_wire_mcp._ensure_forced_fixer_server_resolved(
            {
                fixer_wire_mcp.FORCED_MCP_SERVER: {
                    "command": "/usr/bin/env",
                    "args": ["-u", "FIXER_MCP_TOOL_PROFILE", str(self.current)],
                    "transport": "stdio",
                }
            },
            repo_root=self._repo_root,
            environ=environ,
        )

    def test_env_wrapper_with_identical_payload_copy_passes(self) -> None:
        twin = self._write_binary("wrapped-twin", self.current.read_bytes())
        environ = dict(os.environ)
        environ.pop("FIXER_MCP_BINARY", None)
        fixer_wire_mcp._ensure_forced_fixer_server_resolved(
            {
                fixer_wire_mcp.FORCED_MCP_SERVER: {
                    "command": "/usr/bin/env",
                    "args": ["-u", "SOME_VAR", str(twin)],
                    "transport": "stdio",
                }
            },
            repo_root=self._repo_root,
            environ=environ,
        )

    def test_non_wrapper_command_never_mis_resolves_path_like_args(self) -> None:
        """A plain command with path-like args (config files) checks the command."""
        stale = self._write_binary("some-config", b"#!/bin/sh\necho unrelated-file\n")
        fixer_wire_mcp._verify_forced_fixer_binary_current(
            self.current,
            args=["--config", str(stale)],
            repo_root=self._repo_root,
        )

    def test_identity_reports_build_hashes_and_statuses(self) -> None:
        environ = dict(os.environ)
        environ.pop("FIXER_MCP_BINARY", None)
        current_identity = fixer_wire_mcp._forced_fixer_binary_identity(
            self.current, repo_root=self._repo_root, environ=environ
        )
        self.assertEqual(current_identity.status, fixer_wire_mcp.IDENTITY_CURRENT)
        self.assertFalse(current_identity.fail_closed)

        twin = self._write_binary("twin-identity", self.current.read_bytes())
        twin_identity = fixer_wire_mcp._forced_fixer_binary_identity(
            twin, repo_root=self._repo_root, environ=environ
        )
        self.assertEqual(twin_identity.status, fixer_wire_mcp.IDENTITY_IDENTICAL_COPY)
        self.assertEqual(twin_identity.payload_sha256, twin_identity.current_sha256)
        self.assertEqual(len(twin_identity.payload_sha256), 64)

        stale = self._write_binary("stale-identity", b"#!/bin/sh\necho old\n")
        stale_identity = fixer_wire_mcp._forced_fixer_binary_identity(
            stale, repo_root=self._repo_root, environ=environ
        )
        self.assertEqual(stale_identity.status, fixer_wire_mcp.IDENTITY_STALE)
        self.assertTrue(stale_identity.fail_closed)
        self.assertNotEqual(stale_identity.payload_sha256, stale_identity.current_sha256)

    def test_identity_flags_unverified_states_without_failing_closed(self) -> None:
        environ = dict(os.environ)
        environ.pop("FIXER_MCP_BINARY", None)
        missing = self.tmp / "gone"
        missing_identity = fixer_wire_mcp._forced_fixer_binary_identity(
            missing, repo_root=self._repo_root, environ=environ
        )
        self.assertEqual(missing_identity.status, fixer_wire_mcp.IDENTITY_UNVERIFIED_PAYLOAD_MISSING)
        self.assertFalse(missing_identity.fail_closed)

        empty_root = self.tmp / "empty-root"
        (empty_root / "fixer_mcp").mkdir(parents=True)
        no_current_identity = fixer_wire_mcp._forced_fixer_binary_identity(
            self.current, repo_root=lambda: empty_root, environ=environ
        )
        self.assertEqual(no_current_identity.status, fixer_wire_mcp.IDENTITY_UNVERIFIED_CURRENT_MISSING)
        self.assertFalse(no_current_identity.fail_closed)

    def test_configured_payload_path_resolves_env_wrapper_args(self) -> None:
        stale = self._write_binary("configured-payload", b"#!/bin/sh\necho configured\n")
        (self.module_dir / "mcp_config.json").write_text(
            json.dumps(
                {
                    "mcpServers": {
                        fixer_wire_mcp.FORCED_MCP_SERVER: {
                            "command": "/usr/bin/env",
                            "args": ["-u", "FIXER_MCP_TOOL_PROFILE", str(stale)],
                        }
                    }
                }
            ),
            encoding="utf-8",
        )
        environ = dict(os.environ)
        environ.pop("FIXER_MCP_BINARY", None)
        resolved = fixer_wire_mcp._configured_forced_fixer_payload_path(
            repo_root=self._repo_root, environ=environ
        )
        self.assertEqual(resolved, stale.resolve())

    def test_configured_payload_path_honors_explicit_override(self) -> None:
        override = self._write_binary("override-payload", b"#!/bin/sh\necho override\n")
        environ = dict(os.environ)
        environ["FIXER_MCP_BINARY"] = str(override)
        resolved = fixer_wire_mcp._configured_forced_fixer_payload_path(
            repo_root=self._repo_root, environ=environ
        )
        self.assertEqual(resolved, override.resolve())


if __name__ == "__main__":
    unittest.main()
