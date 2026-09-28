from __future__ import annotations

import json
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from client_wires.backends import pi_adapter


def _completed(command, returncode: int, stdout: str = "", stderr: str = ""):
    return subprocess.CompletedProcess(args=command, returncode=returncode, stdout=stdout, stderr=stderr)


class _FakeRunner:
    """Runner that writes the package marker on selected attempts."""

    def __init__(self, *, succeed_on: int | None, agent_dir: Path) -> None:
        self.succeed_on = succeed_on
        self.agent_dir = agent_dir
        self.calls: list[list[str]] = []

    def __call__(self, command, **_kwargs):
        self.calls.append(list(command))
        attempt = len(self.calls)
        if self.succeed_on == attempt:
            target = self.agent_dir / "npm" / "node_modules" / pi_adapter.PI_MCP_ADAPTER_PACKAGE
            target.mkdir(parents=True, exist_ok=True)
            (target / "package.json").write_text("{}\n", encoding="utf-8")
            return _completed(command, 0, stdout="added 1 package")
        return _completed(command, 1, stderr="npm ERR! code 1 boom")


class EnsureMcpAdapterExtensionTests(unittest.TestCase):
    def test_already_installed_makes_no_npm_call(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            agent = Path(tmp)
            target = agent / "npm" / "node_modules" / pi_adapter.PI_MCP_ADAPTER_PACKAGE
            target.mkdir(parents=True)
            (target / "package.json").write_text("{}\n", encoding="utf-8")
            runner = _FakeRunner(succeed_on=None, agent_dir=agent)

            with patch.dict("os.environ", {pi_adapter.PI_ADAPTER_PREPARE_ENV: "1"}):
                pi_adapter.ensure_mcp_adapter_extension(agent_dir=agent, runner=runner)

            self.assertEqual(runner.calls, [])

    def test_retry_with_clean_cache_recovers(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            agent = Path(tmp)
            runner = _FakeRunner(succeed_on=2, agent_dir=agent)

            with patch.dict("os.environ", {pi_adapter.PI_ADAPTER_PREPARE_ENV: "1"}):
                pi_adapter.ensure_mcp_adapter_extension(agent_dir=agent, runner=runner)

            self.assertEqual(len(runner.calls), 2)
            self.assertIn("--legacy-peer-deps", runner.calls[0])
            self.assertIn("--cache", runner.calls[1])
            log = (agent / "npm" / "pi-mcp-adapter-install.log").read_text(encoding="utf-8")
            self.assertIn("npm ERR! code 1 boom", log)

    def test_both_attempts_fail_raises_actionable_error(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            agent = Path(tmp)
            runner = _FakeRunner(succeed_on=None, agent_dir=agent)

            with patch.dict("os.environ", {pi_adapter.PI_ADAPTER_PREPARE_ENV: "1"}):
                with self.assertRaises(pi_adapter.PiAdapterInstallError) as raised:
                    pi_adapter.ensure_mcp_adapter_extension(agent_dir=agent, runner=runner)

            message = str(raised.exception)
            self.assertIn("npm install pi-mcp-adapter --prefix", message)
            self.assertIn(str(agent / "npm"), message)
            self.assertIn("pi-mcp-adapter-install.log", message)
            self.assertEqual(len(runner.calls), 2)

    def test_missing_npm_is_reported_not_raised_as_oserror(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            agent = Path(tmp)

            def runner(command, **_kwargs):
                raise FileNotFoundError("npm")

            with patch.dict("os.environ", {pi_adapter.PI_ADAPTER_PREPARE_ENV: "1"}):
                with self.assertRaises(pi_adapter.PiAdapterInstallError) as raised:
                    pi_adapter.ensure_mcp_adapter_extension(agent_dir=agent, runner=runner)

            self.assertIn("npm could not run", str(raised.exception))

    def test_autoprepare_can_be_disabled(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            agent = Path(tmp)
            runner = _FakeRunner(succeed_on=None, agent_dir=agent)

            with patch.dict("os.environ", {pi_adapter.PI_ADAPTER_PREPARE_ENV: "0"}):
                pi_adapter.ensure_mcp_adapter_extension(agent_dir=agent, runner=runner)

            self.assertEqual(runner.calls, [])
            self.assertFalse((agent / "settings.json").exists())

    def test_installed_package_is_registered_in_settings(self) -> None:
        # d0lsi, 2026-09-25: the npm package was present but pi refused
        # --mcp-config ("Unknown option") because settings.json `packages`
        # lacked the entry.
        with tempfile.TemporaryDirectory() as tmp:
            agent = Path(tmp)
            target = agent / "npm" / "node_modules" / pi_adapter.PI_MCP_ADAPTER_PACKAGE
            target.mkdir(parents=True)
            (target / "package.json").write_text("{}\n", encoding="utf-8")
            (agent / "settings.json").write_text(
                json.dumps({"theme": "dark", "packages": ["npm:pi-unified-model-picker@0.1.1"]})
                + "\n",
                encoding="utf-8",
            )
            runner = _FakeRunner(succeed_on=None, agent_dir=agent)

            with patch.dict("os.environ", {pi_adapter.PI_ADAPTER_PREPARE_ENV: "1"}):
                pi_adapter.ensure_mcp_adapter_extension(agent_dir=agent, runner=runner)

            self.assertEqual(runner.calls, [])
            settings = json.loads((agent / "settings.json").read_text(encoding="utf-8"))
            self.assertEqual(settings["theme"], "dark")
            self.assertEqual(
                settings["packages"],
                ["npm:pi-mcp-adapter", "npm:pi-unified-model-picker@0.1.1"],
            )

    def test_registration_is_idempotent(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            agent = Path(tmp)
            target = agent / "npm" / "node_modules" / pi_adapter.PI_MCP_ADAPTER_PACKAGE
            target.mkdir(parents=True)
            (target / "package.json").write_text("{}\n", encoding="utf-8")
            settings_path = agent / "settings.json"
            original = {"packages": ["npm:pi-mcp-adapter"], "theme": "dark"}
            settings_path.write_text(json.dumps(original) + "\n", encoding="utf-8")

            with patch.dict("os.environ", {pi_adapter.PI_ADAPTER_PREPARE_ENV: "1"}):
                pi_adapter.ensure_mcp_adapter_extension(
                    agent_dir=agent, runner=_FakeRunner(succeed_on=None, agent_dir=agent)
                )

            self.assertEqual(
                json.loads(settings_path.read_text(encoding="utf-8")), original
            )

    def test_fresh_host_gets_settings_registration(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            agent = Path(tmp)
            runner = _FakeRunner(succeed_on=1, agent_dir=agent)

            with patch.dict("os.environ", {pi_adapter.PI_ADAPTER_PREPARE_ENV: "1"}):
                pi_adapter.ensure_mcp_adapter_extension(agent_dir=agent, runner=runner)

            settings = json.loads((agent / "settings.json").read_text(encoding="utf-8"))
            self.assertEqual(settings["packages"], ["npm:pi-mcp-adapter"])

    def test_unreadable_settings_raises_actionable_error(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            agent = Path(tmp)
            target = agent / "npm" / "node_modules" / pi_adapter.PI_MCP_ADAPTER_PACKAGE
            target.mkdir(parents=True)
            (target / "package.json").write_text("{}\n", encoding="utf-8")
            (agent / "settings.json").write_text("{ not json", encoding="utf-8")

            with patch.dict("os.environ", {pi_adapter.PI_ADAPTER_PREPARE_ENV: "1"}):
                with self.assertRaises(pi_adapter.PiAdapterInstallError) as raised:
                    pi_adapter.ensure_mcp_adapter_extension(
                        agent_dir=agent, runner=_FakeRunner(succeed_on=None, agent_dir=agent)
                    )

            self.assertIn("settings.json", str(raised.exception))


if __name__ == "__main__":
    unittest.main()
