from __future__ import annotations

import json
import os
import shutil
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch

from client_wires import fixer_wire
from client_wires import fixer_wire_mcp
from client_wires import fixer_wire_resume


class ResumeMcpIdentityTests(unittest.TestCase):
    """The resume helper must not attach a durable session to a stale transport.

    A resumed client re-reads the MCP config; if that config still points at an
    old installed payload (a 1.0.9 transport once survived every later
    release), resuming continues against a stale-schema transport. The resume
    identity probe reports the configured payload and fails closed on a
    definitive stale verdict with an actionable reconnect instruction.
    """

    def setUp(self) -> None:
        self.tmp = Path(tempfile.mkdtemp(prefix="fixer_mcp_resume_"))
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

    def _write_config(self, payload: Path) -> None:
        (self.module_dir / "mcp_config.json").write_text(
            json.dumps(
                {
                    "mcpServers": {
                        fixer_wire_mcp.FORCED_MCP_SERVER: {
                            "command": "/usr/bin/env",
                            "args": ["-u", "FIXER_MCP_TOOL_PROFILE", str(payload)],
                        }
                    }
                }
            ),
            encoding="utf-8",
        )

    def _environ(self) -> dict[str, str]:
        environ = dict(os.environ)
        environ.pop("FIXER_MCP_BINARY", None)
        return environ

    def test_probe_environments_never_inherit_the_live_database(self) -> None:
        db_path = Path(self._environ().get("FIXER_DB_PATH", ""))
        self.assertTrue(
            str(db_path).startswith(str(self.tmp)),
            f"probe environment inherited a non-temporary FIXER_DB_PATH: {db_path}",
        )

    def _write_payload(self, name: str, payload: bytes) -> Path:
        path = self.tmp / name
        path.write_bytes(payload)
        path.chmod(0o755)
        return path

    def test_resume_identity_current_for_env_wrapper_config(self) -> None:
        self._write_config(self.current)
        identity = fixer_wire_resume.resume_mcp_identity_status(
            repo_root=self._repo_root, environ=self._environ()
        )
        self.assertEqual(identity.status, fixer_wire_mcp.IDENTITY_CURRENT)
        self.assertFalse(identity.fail_closed)
        fixer_wire_resume.assert_resume_mcp_config_current(
            repo_root=self._repo_root, environ=self._environ()
        )

    def test_resume_identity_stale_fails_closed_with_reconnect_hint(self) -> None:
        stale = self._write_payload("stale-payload", b"#!/bin/sh\necho old-1.0.9\n")
        self._write_config(stale)
        identity = fixer_wire_resume.resume_mcp_identity_status(
            repo_root=self._repo_root, environ=self._environ()
        )
        self.assertEqual(identity.status, fixer_wire_mcp.IDENTITY_STALE)
        self.assertTrue(identity.fail_closed)
        with self.assertRaises(RuntimeError) as raised:
            fixer_wire_resume.assert_resume_mcp_config_current(
                repo_root=self._repo_root, environ=self._environ()
            )
        message = str(raised.exception)
        self.assertIn("Refusing to resume", message)
        self.assertIn("stale binary", message)
        self.assertIn("close and reopen", message.lower())
        self.assertIn("fixer doctor", message)

    def test_resume_identity_identical_payload_copy_passes(self) -> None:
        twin = self._write_payload("twin-payload", self.current.read_bytes())
        self._write_config(twin)
        identity = fixer_wire_resume.assert_resume_mcp_config_current(
            repo_root=self._repo_root, environ=self._environ()
        )
        self.assertEqual(identity.status, fixer_wire_mcp.IDENTITY_IDENTICAL_COPY)

    def test_resume_identity_sanctioned_override_is_exempt(self) -> None:
        stale = self._write_payload("operator-payload", b"#!/bin/sh\necho operator\n")
        self._write_config(stale)
        environ = self._environ()
        environ["FIXER_MCP_BINARY"] = str(stale)
        identity = fixer_wire_resume.assert_resume_mcp_config_current(
            repo_root=self._repo_root, environ=environ
        )
        self.assertEqual(identity.status, fixer_wire_mcp.IDENTITY_SANCTIONED_OVERRIDE)

    def test_resume_identity_unverified_states_do_not_block_resume(self) -> None:
        missing = self.tmp / "gone"
        self._write_config(missing)
        identity = fixer_wire_resume.assert_resume_mcp_config_current(
            repo_root=self._repo_root, environ=self._environ()
        )
        self.assertEqual(identity.status, fixer_wire_mcp.IDENTITY_UNVERIFIED_PAYLOAD_MISSING)
        self.assertIn("fixer doctor", identity.message)

    def test_resolve_latest_resume_runs_identity_gate_first(self) -> None:
        calls: list[str] = []

        def gate() -> None:
            calls.append("gate")

        def loader(_cwd: Path, *, limit: int = 8) -> list[object]:
            calls.append("loader")
            return [SimpleNamespace(provider="codex", session_id="abc", model="", log_path=None)]

        with self.assertRaises(RuntimeError) as raised:
            fixer_wire_resume.resolve_latest_fixer_resume_session_id(
                Path("/tmp/project-x"),
                load_fixer_resume_summaries=loader,
                verify_resume_mcp_identity=lambda: (_ for _ in ()).throw(
                    RuntimeError("stale transport")
                ),
            )
        self.assertIn("stale transport", str(raised.exception))
        self.assertNotIn("loader", calls)

        resolved = fixer_wire_resume.resolve_latest_fixer_resume_session_id(
            Path("/tmp/project-x"),
            load_fixer_resume_summaries=loader,
            verify_resume_mcp_identity=gate,
        )
        self.assertEqual(resolved, "abc")
        self.assertEqual(calls, ["gate", "loader"])

    def test_resolve_netrunner_resume_runs_identity_gate_first(self) -> None:
        def loader(_cwd: Path, _session_id: int) -> list[object]:
            raise AssertionError("loader must not run after a stale fail-closed gate")

        selected_session = SimpleNamespace(cli_backend="codex", external_session_id="s-1")
        with self.assertRaises(RuntimeError) as raised:
            fixer_wire_resume.resolve_netrunner_resume_session_id(
                Path("/tmp/project-x"),
                selected_session,
                None,
                None,
                prompt_resume_session_id=lambda _sid, _backend: None,
                load_netrunner_resume_summaries=loader,
                select_netrunner_resume_session_interactive=lambda *_a, **_k: "",
                verify_resume_mcp_identity=lambda: (_ for _ in ()).throw(
                    RuntimeError("stale transport")
                ),
            )
        self.assertIn("stale transport", str(raised.exception))

    def test_wire_wrappers_pass_the_resume_identity_gate(self) -> None:
        with (
            patch.object(fixer_wire_resume, "resolve_latest_fixer_resume_session_id", return_value="codex/session-1") as latest,
            patch.object(fixer_wire_resume, "assert_resume_mcp_config_current"),
        ):
            resolved = fixer_wire._resolve_latest_fixer_resume_session_id(Path("/tmp/project-x"))
        self.assertEqual(resolved, "codex/session-1")
        self.assertIs(
            latest.call_args.kwargs["verify_resume_mcp_identity"],
            fixer_wire._verify_resume_mcp_config_current,
        )

        with patch.object(
            fixer_wire_resume,
            "assert_resume_mcp_config_current",
            side_effect=RuntimeError("stale transport"),
        ):
            with self.assertRaises(RuntimeError):
                fixer_wire._verify_resume_mcp_config_current()

    def test_resolve_without_gate_stays_deterministic(self) -> None:
        def loader(_cwd: Path, *, limit: int = 8) -> list[object]:
            return []

        with self.assertRaises(RuntimeError):
            fixer_wire_resume.resolve_latest_fixer_resume_session_id(
                Path("/tmp/project-x"),
                load_fixer_resume_summaries=loader,
            )


if __name__ == "__main__":
    unittest.main()
