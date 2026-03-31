from __future__ import annotations

import io
import json
import tempfile
import unittest
from contextlib import redirect_stdout
from pathlib import Path
import sys
from unittest.mock import patch

REPO_ROOT = Path(__file__).resolve().parents[1]
CLIENT_WIRES_SRC = REPO_ROOT / "packages" / "client-wires" / "src"
if str(CLIENT_WIRES_SRC) not in sys.path:
    sys.path.insert(0, str(CLIENT_WIRES_SRC))

from fixer_client_wires import cli
from fixer_client_wires.bootstrap import PUBLIC_CONFIG_PATH_ENV, PUBLIC_RUNTIME_ROOT_ENV
from fixer_client_wires.launcher import available_roles, build_launch_plan


class ClientWiresLauncherTest(unittest.TestCase):
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
            {
                "mcpServers": {
                    "fixer_mcp": {"command": "fixer-mcp"},
                    "sqlite": {"command": "sqlite-mcp"},
                }
            },
        )
        return repo_root, package_root

    def test_build_launch_plan_uses_packaged_backend_and_role_contract(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            repo_root, package_root = self._make_layout(Path(temp_dir))

            plan = build_launch_plan(
                role="netrunner",
                backend="codex",
                mcp_servers=["sqlite", "fixer_mcp"],
                repo_root=repo_root,
                package_root=package_root,
                environ={},
            )

        self.assertEqual(plan.role.name, "netrunner")
        self.assertEqual(plan.backend.name, "codex")
        self.assertEqual(plan.runtime_resolution.source, "package-local runtime")
        self.assertEqual(plan.config_source, "package-local config")
        self.assertEqual(plan.selected_mcp_servers, ("fixer_mcp", "sqlite"))
        self.assertIn("--mcp=fixer_mcp", plan.command)
        self.assertIn("--mcp=sqlite", plan.command)

    def test_build_launch_plan_reports_droid_runtime_side_effects(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            repo_root, package_root = self._make_layout(Path(temp_dir))

            plan = build_launch_plan(
                role="fixer",
                backend="droid",
                repo_root=repo_root,
                package_root=package_root,
                environ={},
            )

        self.assertEqual(plan.backend.name, "droid")
        self.assertIn("writes .factory/settings.json", "\n".join(plan.notes))
        self.assertIn("--output-format", plan.command)

    def test_cli_plan_launch_json_uses_packaged_surface(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            repo_root, package_root = self._make_layout(Path(temp_dir))
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
                        [
                            "plan-launch",
                            "--role",
                            "netrunner",
                            "--backend",
                            "codex",
                            "--mcp-server",
                            "fixer_mcp",
                            "--json",
                        ]
                    )

        self.assertEqual(exit_code, 0)
        payload = json.loads(stdout.getvalue())
        self.assertEqual(payload["role"]["name"], "netrunner")
        self.assertEqual(payload["backend"]["name"], "codex")
        self.assertEqual(payload["selected_mcp_servers"], ["fixer_mcp"])

    def test_available_roles_cover_public_launcher_roles(self) -> None:
        self.assertEqual(
            [role.name for role in available_roles()],
            ["fixer", "netrunner", "overseer"],
        )


if __name__ == "__main__":
    unittest.main()
