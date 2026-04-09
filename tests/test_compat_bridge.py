from __future__ import annotations

import io
import json
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from pathlib import Path
import sys
from unittest.mock import patch

REPO_ROOT = Path(__file__).resolve().parents[1]
CLIENT_WIRES_SRC = REPO_ROOT / "packages" / "client-wires" / "src"
COMPAT_BRIDGE_SRC = REPO_ROOT / "packages" / "compat-bridge" / "src"
for candidate in (CLIENT_WIRES_SRC, COMPAT_BRIDGE_SRC):
    candidate_str = str(candidate)
    if candidate_str not in sys.path:
        sys.path.insert(0, candidate_str)

from fixer_client_wires.bootstrap import PUBLIC_CONFIG_PATH_ENV, PUBLIC_RUNTIME_ROOT_ENV
from fixer_compat_bridge import cli


class CompatBridgeTest(unittest.TestCase):
    def _write_json(self, path: Path, payload: dict[str, object]) -> None:
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(json.dumps(payload, indent=2, sort_keys=True) + "\n", encoding="utf-8")

    def _make_layout(self, temp_path: Path) -> tuple[Path, Path]:
        repo_root = temp_path / "github_repo"
        package_root = repo_root / "packages" / "client-wires"
        runtime_root = package_root / "runtime" / "fixer_runtime"
        runtime_root.mkdir(parents=True)
        (runtime_root / "__init__.py").write_text("", encoding="utf-8")
        self._write_json(
            package_root / "config" / "mcp-config.json",
            {"mcpServers": {"fixer_mcp": {"command": "fixer-mcp"}}},
        )
        return repo_root, package_root

    def test_wire_info_delegates_to_client_wires(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            _, package_root = self._make_layout(Path(temp_dir))
            stdout = io.StringIO()
            config_path = package_root / "config" / "mcp-config.json"
            runtime_root = package_root / "runtime"

            with patch.dict(
                "os.environ",
                {
                    PUBLIC_CONFIG_PATH_ENV: str(config_path),
                    PUBLIC_RUNTIME_ROOT_ENV: str(runtime_root),
                },
                clear=False,
            ):
                with redirect_stdout(stdout):
                    exit_code = cli.main(["--wire-info"])

        self.assertEqual(exit_code, 0)
        self.assertIn("runtime source:", stdout.getvalue())
        self.assertIn("config source:", stdout.getvalue())

    def test_role_delegation_renders_plan_launch_json(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            _, package_root = self._make_layout(Path(temp_dir))
            stdout = io.StringIO()
            config_path = package_root / "config" / "mcp-config.json"
            runtime_root = package_root / "runtime"

            with patch.dict(
                "os.environ",
                {
                    PUBLIC_CONFIG_PATH_ENV: str(config_path),
                    PUBLIC_RUNTIME_ROOT_ENV: str(runtime_root),
                },
                clear=False,
            ):
                with redirect_stdout(stdout):
                    exit_code = cli.main(["--role", "fixer", "--json"])

        self.assertEqual(exit_code, 0)
        payload = json.loads(stdout.getvalue())
        self.assertEqual(payload["role"]["name"], "fixer")
        self.assertEqual(payload["backend"]["name"], "codex")

    def test_resume_role_delegation_renders_plan_resume_json(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            _, package_root = self._make_layout(Path(temp_dir))
            stdout = io.StringIO()
            config_path = package_root / "config" / "mcp-config.json"
            runtime_root = package_root / "runtime"

            with patch.dict(
                "os.environ",
                {
                    PUBLIC_CONFIG_PATH_ENV: str(config_path),
                    PUBLIC_RUNTIME_ROOT_ENV: str(runtime_root),
                },
                clear=False,
            ):
                with redirect_stdout(stdout):
                    exit_code = cli.main(
                        ["--role", "netrunner", "--backend", "codex", "--resume-session-id", "codex-42", "--json"]
                    )

        self.assertEqual(exit_code, 0)
        payload = json.loads(stdout.getvalue())
        self.assertEqual(payload["mode"], "resume")
        self.assertEqual(payload["external_session_id"], "codex-42")

    def test_missing_role_returns_migration_guidance(self) -> None:
        stdout = io.StringIO()
        stderr = io.StringIO()
        with redirect_stdout(stdout), redirect_stderr(stderr):
            exit_code = cli.main([])

        self.assertEqual(exit_code, 2)
        self.assertIn("Compatibility note:", stderr.getvalue())

    def test_fixer_entrypoint_defaults_to_real_launch(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            _, package_root = self._make_layout(Path(temp_dir))
            config_path = package_root / "config" / "mcp-config.json"
            runtime_root = package_root / "runtime"
            delegated: list[list[str]] = []

            def _fake_main(argv: list[str] | None = None) -> int:
                delegated.append(list(argv or []))
                return 0

            with patch.dict(
                "os.environ",
                {
                    PUBLIC_CONFIG_PATH_ENV: str(config_path),
                    PUBLIC_RUNTIME_ROOT_ENV: str(runtime_root),
                },
                clear=False,
            ):
                with patch.object(sys, "argv", ["fixer"]):
                    with patch("fixer_client_wires.cli.main", side_effect=_fake_main):
                        exit_code = cli.main([])

        self.assertEqual(exit_code, 0)
        self.assertEqual(delegated[0][:4], ["launch", "--role", "fixer", "--backend"])


if __name__ == "__main__":
    unittest.main()
