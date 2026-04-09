from __future__ import annotations

import io
import importlib
import json
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from pathlib import Path
import sys
from unittest.mock import patch

REPO_ROOT = Path(__file__).resolve().parents[1]
CLIENT_WIRES_SRC = REPO_ROOT / "packages" / "client-wires" / "src"
if str(CLIENT_WIRES_SRC) not in sys.path:
    sys.path.insert(0, str(CLIENT_WIRES_SRC))

from fixer_client_wires import cli
from fixer_client_wires.executor import (
    FIXER_CLIENT_WIRES_SKIP_FIXER_MCP_AUTOBUILD_ENV,
    build_codex_override_args,
    execute_launch_plan,
    prepare_selected_servers,
)
from fixer_client_wires.bootstrap import PUBLIC_CONFIG_PATH_ENV, PUBLIC_RUNTIME_ROOT_ENV
from fixer_client_wires.launcher import available_roles, build_launch_plan, build_resume_plan


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

    def _reload_backend_modules(self) -> None:
        import fixer_client_wires.backends as backends
        import fixer_client_wires.backends.claude as claude_backend
        import fixer_client_wires.backends.droid as droid_backend

        importlib.reload(claude_backend)
        importlib.reload(droid_backend)
        importlib.reload(backends)

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
        self.assertEqual(plan.mode, "fresh")
        self.assertEqual(plan.backend.name, "codex")
        self.assertEqual(plan.runtime_resolution.source, "package-local runtime")
        self.assertEqual(plan.config_source, "package-local config")
        self.assertEqual(plan.selected_mcp_servers, ("fixer_mcp", "sqlite"))
        self.assertIn("--mcp=fixer_mcp", plan.command)
        self.assertIn("--mcp=sqlite", plan.command)

    def test_build_launch_plan_reports_droid_runtime_side_effects(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            repo_root, package_root = self._make_layout(Path(temp_dir))
            self._write_json(
                package_root / "config" / "mcp-config.json",
                {
                    "mcpServers": {
                        "fixer_mcp": {
                            "command": "fixer-mcp",
                            "env": {"FIXER_DB_PATH": "/tmp/project/fixer.db"},
                            "startup_timeout_sec": 30,
                            "tool_timeout_sec": 21600,
                        },
                        "sqlite": {"command": "sqlite-mcp"},
                    }
                },
            )

            plan = build_launch_plan(
                role="fixer",
                backend="droid",
                repo_root=repo_root,
                package_root=package_root,
                environ={},
            )

        self.assertEqual(plan.backend.name, "droid")
        self.assertIn("layered .factory/settings.json", "\n".join(plan.notes))
        self.assertIn("staged skills root:", "\n".join(plan.notes))
        self.assertIn('"FIXER_DB_PATH": "/tmp/project/fixer.db"', "\n".join(plan.notes))
        self.assertNotIn("startup_timeout_sec", "\n".join(plan.notes))
        self.assertEqual(plan.command[:4], ("droid", "exec", "--auto", "high"))
        self.assertIn("--output-format", plan.command)

    def test_build_resume_plan_reuses_backend_without_model_overrides(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            repo_root, package_root = self._make_layout(Path(temp_dir))

            plan = build_resume_plan(
                role="fixer",
                backend="codex",
                external_session_id="codex-session-17",
                repo_root=repo_root,
                package_root=package_root,
                environ={},
            )

        self.assertEqual(plan.mode, "resume")
        self.assertEqual(plan.external_session_id, "codex-session-17")
        self.assertIn("resume", plan.command)
        self.assertNotIn("--model", plan.command)
        self.assertIn("sticky", "\n".join(plan.notes))

    def test_droid_backend_merges_custom_models_from_factory_settings(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            repo_root, package_root = self._make_layout(Path(temp_dir))
            factory_settings_path = Path(temp_dir) / "factory-settings.json"
            self._write_json(
                factory_settings_path,
                {
                    "customModels": [
                        {
                            "id": "custom:Qwen3.6-Plus-Preview-Free-[OpenRouter]-0",
                            "displayName": "Qwen3.6 Plus Preview Free [OpenRouter]",
                        }
                    ],
                    "sessionDefaultSettings": {
                        "model": "custom:Qwen3.6-Plus-Preview-Free-[OpenRouter]-0",
                    },
                },
            )

            with patch.dict(
                "os.environ",
                {"FIXER_CLIENT_WIRES_DROID_SETTINGS_PATH": str(factory_settings_path)},
                clear=False,
            ):
                self._reload_backend_modules()
                plan = build_launch_plan(
                    role="fixer",
                    backend="droid",
                    repo_root=repo_root,
                    package_root=package_root,
                    environ={},
                )
            self._reload_backend_modules()

        self.assertEqual(plan.backend.default_model, "custom:Qwen3.6-Plus-Preview-Free-[OpenRouter]-0")
        self.assertIn("custom:Qwen3.6-Plus-Preview-Free-[OpenRouter]-0", plan.backend.model_options)
        self.assertIn("-m", plan.command)
        self.assertIn("custom:Qwen3.6-Plus-Preview-Free-[OpenRouter]-0", plan.command)

    def test_claude_backend_builds_fresh_launch_plan(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            repo_root, package_root = self._make_layout(Path(temp_dir))

            plan = build_launch_plan(
                role="fixer",
                backend="claude",
                repo_root=repo_root,
                package_root=package_root,
                environ={},
            )

        self.assertEqual(plan.backend.name, "claude")
        self.assertEqual(plan.command[:3], ("claude", "--permission-mode", "bypassPermissions"))
        self.assertIn("--model", plan.command)
        self.assertNotIn("-p", plan.command)
        self.assertNotIn("--output-format", plan.command)
        self.assertIn("layered .mcp.json", "\n".join(plan.notes))

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
        self.assertEqual(payload["mode"], "fresh")
        self.assertEqual(payload["role"]["name"], "netrunner")
        self.assertEqual(payload["backend"]["name"], "codex")
        self.assertEqual(payload["selected_mcp_servers"], ["fixer_mcp"])

    def test_cli_plan_resume_json_uses_sticky_session_metadata_contract(self) -> None:
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
                        [
                            "plan-resume",
                            "--role",
                            "netrunner",
                            "--backend",
                            "droid",
                            "--session-id",
                            "droid-session-9",
                            "--json",
                        ]
                    )

        self.assertEqual(exit_code, 0)
        payload = json.loads(stdout.getvalue())
        self.assertEqual(payload["mode"], "resume")
        self.assertEqual(payload["backend"]["name"], "droid")
        self.assertEqual(payload["external_session_id"], "droid-session-9")
        self.assertNotIn("-m", payload["command"])

    def test_direct_fixer_entrypoint_prompts_for_role_before_real_launch(self) -> None:
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
                with patch.object(sys, "argv", ["fixer"]):
                    with patch("sys.stdin.isatty", return_value=True):
                        with patch("builtins.input", return_value="2"):
                            with patch("fixer_client_wires.cli.execute_launch_plan", return_value=0) as execute_launch:
                                with redirect_stdout(stdout):
                                    exit_code = cli.main([])

        self.assertEqual(exit_code, 0)
        self.assertIn("Select role:", stdout.getvalue())
        execute_launch.assert_called_once()
        launched_plan = execute_launch.call_args.args[0]
        self.assertEqual(launched_plan.role.name, "netrunner")

    def test_direct_fixer_entrypoint_dry_run_renders_selected_role_launch_plan(self) -> None:
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
                with patch.object(sys, "argv", ["fixer"]):
                    with patch("sys.stdin.isatty", return_value=True):
                        with patch("builtins.input", return_value="overseer"):
                            with redirect_stdout(stdout):
                                exit_code = cli.main(["--dry-run", "--json"])

        self.assertEqual(exit_code, 0)
        rendered = stdout.getvalue()
        self.assertIn("Select role:", rendered)
        payload = json.loads(rendered[rendered.index("{") :])
        self.assertEqual(payload["role"]["name"], "overseer")
        self.assertEqual(payload["backend"]["name"], "codex")
        self.assertEqual(payload["mode"], "fresh")

    def test_direct_fixer_entrypoint_requires_role_when_not_interactive(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            _, package_root = self._make_layout(Path(temp_dir))
            stdout = io.StringIO()
            stderr = io.StringIO()
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
                with patch.object(sys, "argv", ["fixer"]):
                    with patch("sys.stdin.isatty", return_value=False):
                        with redirect_stdout(stdout), redirect_stderr(stderr):
                            exit_code = cli.main([])

        self.assertEqual(exit_code, 2)
        combined = stdout.getvalue() + stderr.getvalue()
        self.assertIn("Select role:", combined)
        self.assertIn("pass --role to bypass the selector", combined)

    def test_available_backends_include_staged_claude_choice(self) -> None:
        stdout = io.StringIO()
        with redirect_stdout(stdout):
            exit_code = cli.main(["list-backends"])

        self.assertEqual(exit_code, 0)
        output = stdout.getvalue()
        self.assertIn("claude", output)
        self.assertIn("resume planned", output)

    def test_available_roles_cover_public_launcher_roles(self) -> None:
        self.assertEqual(
            [role.name for role in available_roles()],
            ["fixer", "netrunner", "overseer"],
        )

    def test_prepare_selected_servers_binds_fixer_state_root(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            repo_root, package_root = self._make_layout(Path(temp_dir))
            plan = build_launch_plan(
                role="fixer",
                backend="codex",
                repo_root=repo_root,
                package_root=package_root,
                environ={FIXER_CLIENT_WIRES_SKIP_FIXER_MCP_AUTOBUILD_ENV: "1"},
            )
            state_root = Path(temp_dir) / "state"

            servers = prepare_selected_servers(
                plan,
                state_root=state_root,
                environ={FIXER_CLIENT_WIRES_SKIP_FIXER_MCP_AUTOBUILD_ENV: "1"},
            )

        fixer_spec = servers["fixer_mcp"]
        self.assertEqual(fixer_spec["cwd"], str(state_root.resolve()))
        self.assertEqual(fixer_spec["env"]["FIXER_DB_PATH"], str((state_root / "fixer.db").resolve()))

    def test_build_codex_override_args_renders_server_payload(self) -> None:
        overrides = build_codex_override_args(
            {
                "fixer_mcp": {
                    "command": "/tmp/fixer_mcp",
                    "args": ["serve"],
                    "env": {"FIXER_DB_PATH": "/tmp/fixer.db"},
                    "cwd": "/tmp/state",
                    "timeout": 21600,
                }
            }
        )

        rendered = " ".join(overrides)
        self.assertIn("mcp_servers.fixer_mcp.command=", rendered)
        self.assertIn("mcp_servers.fixer_mcp.env=", rendered)
        self.assertIn("mcp_servers.fixer_mcp.cwd=", rendered)

    def test_execute_launch_plan_injects_codex_server_overrides(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            repo_root, package_root = self._make_layout(Path(temp_dir))
            calls: list[tuple[list[str], str, dict[str, str]]] = []

            def _runner(command: list[str], *, cwd: str, env: dict[str, str]) -> int:
                calls.append((command, cwd, env))
                return 0

            config_path = package_root / "config" / "mcp-config.json"
            runtime_root = package_root / "runtime"
            state_root = Path(temp_dir) / "state"
            with patch.dict(
                "os.environ",
                {
                    PUBLIC_CONFIG_PATH_ENV: str(config_path),
                    PUBLIC_RUNTIME_ROOT_ENV: str(runtime_root),
                    FIXER_CLIENT_WIRES_SKIP_FIXER_MCP_AUTOBUILD_ENV: "1",
                    "FIXER_CLIENT_WIRES_STATE_ROOT": str(state_root),
                },
                clear=False,
            ):
                plan = build_launch_plan(
                    role="fixer",
                    backend="codex",
                    repo_root=repo_root,
                    package_root=package_root,
                    environ={FIXER_CLIENT_WIRES_SKIP_FIXER_MCP_AUTOBUILD_ENV: "1"},
                )
                exit_code = execute_launch_plan(
                    plan,
                    launch_cwd=Path(temp_dir),
                    environ={
                        FIXER_CLIENT_WIRES_SKIP_FIXER_MCP_AUTOBUILD_ENV: "1",
                        "FIXER_CLIENT_WIRES_STATE_ROOT": str(state_root),
                    },
                    runner=_runner,
                )

        self.assertEqual(exit_code, 0)
        self.assertEqual(len(calls), 1)
        command, cwd, env = calls[0]
        self.assertEqual(cwd, str(Path(temp_dir).resolve()))
        self.assertIn("mcp_servers.fixer_mcp.command=", " ".join(command))
        self.assertIn("--mcp=fixer_mcp", command)
        self.assertIn("FIXER_DB_PATH", env)


if __name__ == "__main__":
    unittest.main()
