from __future__ import annotations

import io
import os
import sys
import tempfile
import types
import unittest
from contextlib import redirect_stdout
from pathlib import Path
from unittest.mock import patch

from client_wires import fixer_wire, mvp_scaffold
from client_wires.tests.test_fixer_wire import _DummyOption, _fake_codex_main_module


class MVPScaffoldSpecTests(unittest.TestCase):
    def test_normalize_project_slug_rewrites_display_name(self) -> None:
        self.assertEqual(mvp_scaffold.normalize_project_slug("My MVP App"), "my_mvp_app")

    def test_normalize_project_slug_prefixes_numeric_names(self) -> None:
        self.assertEqual(mvp_scaffold.normalize_project_slug("2026 launch"), "mvp_2026_launch")

    def test_build_scaffold_spec_uses_explicit_target_dir(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            spec = mvp_scaffold.build_scaffold_spec("Neuro Canvas", target_dir=tmp, dry_run=True)

        self.assertEqual(spec.project_slug, "neuro_canvas")
        self.assertEqual(spec.target_root, Path(tmp).resolve())
        self.assertEqual(spec.destination, Path(tmp).resolve() / "neuro_canvas")
        self.assertTrue(spec.dry_run)


class MVPScaffoldExecutionTests(unittest.TestCase):
    def test_scaffold_mvp_project_dry_run_skips_serverpod(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            spec = mvp_scaffold.build_scaffold_spec("Dry Run", target_dir=tmp, dry_run=True)
            output = io.StringIO()

            with redirect_stdout(output):
                mvp_scaffold.scaffold_mvp_project(spec)

        self.assertIn("dry-run only", output.getvalue())
        self.assertFalse(spec.destination.exists())

    def test_scaffold_mvp_project_writes_overlay_files(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            spec = mvp_scaffold.build_scaffold_spec("Neuro Launch", target_dir=tmp)

            def fake_runner(command: list[str], cwd: Path) -> None:
                self.assertEqual(command, ["serverpod", "create", "neuro_launch"])
                self.assertEqual(cwd, Path(tmp).resolve())
                destination = cwd / "neuro_launch"
                destination.mkdir()
                nested_root = destination / "neuro_launch"
                nested_root.mkdir()
                (nested_root / "neuro_launch_server").mkdir()
                (nested_root / "neuro_launch_client").mkdir()
                (nested_root / "neuro_launch_flutter").mkdir()

            mvp_scaffold.scaffold_mvp_project(spec, command_runner=fake_runner)

            self.assertTrue((spec.destination / "README.md").is_file())
            self.assertTrue((spec.destination / "WORKFLOW.md").is_file())
            self.assertTrue((spec.destination / "llm_pipeline" / "README.md").is_file())
            self.assertTrue(
                (spec.destination / "llm_pipeline" / "cmd" / "neuro_launch_ai_service" / "main.go").is_file()
            )
            script_path = spec.destination / "llm_pipeline" / "scripts" / "run_codex_app_server.sh"
            self.assertTrue(script_path.is_file())
            self.assertTrue(os.access(script_path, os.X_OK))

            readme = (spec.destination / "README.md").read_text(encoding="utf-8")
            self.assertIn("fixer --scaffold-mvp neuro_launch", readme)
            workflow = (spec.destination / "WORKFLOW.md").read_text(encoding="utf-8")
            self.assertIn("codex --config shell_environment_policy.inherit=all", workflow)


class MVPScaffoldWireTests(unittest.TestCase):
    def test_select_role_interactive_exposes_scaffold_option(self) -> None:
        captured: dict[str, object] = {}

        def choose(options: list[object], **kwargs: object) -> str:
            captured["options"] = options
            captured["kwargs"] = kwargs
            return fixer_wire.SCAFFOLD_MVP_ACTION

        selected = fixer_wire._select_role_interactive(_DummyOption, choose)

        self.assertEqual(selected, fixer_wire.SCAFFOLD_MVP_ACTION)
        labels = [option.label for option in captured["options"]]
        self.assertIn("MVP Scaffold", labels)
        self.assertEqual(captured["kwargs"]["preselected_value"], fixer_wire.SCAFFOLD_MVP_ACTION)

    def test_launch_scaffold_interactive_collects_inputs(self) -> None:
        with (
            patch("builtins.input", side_effect=["neuro_canvas", "/tmp/mvps"]),
            patch.object(fixer_wire, "run_scaffold_cli", return_value=0) as mock_scaffold,
        ):
            code = fixer_wire._launch_scaffold_interactive(
                _DummyOption,
                lambda _options, **_kwargs: "create",
            )

        self.assertEqual(code, 0)
        mock_scaffold.assert_called_once_with("neuro_canvas", target_dir="/tmp/mvps", dry_run=False)

    def test_launch_scaffold_interactive_defaults_target_dir_and_dry_run(self) -> None:
        with (
            patch("builtins.input", side_effect=["neuro_canvas", ""]),
            patch.object(fixer_wire, "run_scaffold_cli", return_value=0) as mock_scaffold,
            patch("client_wires.fixer_wire.Path.cwd", return_value=Path("/tmp/current")),
        ):
            code = fixer_wire._launch_scaffold_interactive(
                _DummyOption,
                lambda _options, **_kwargs: "dry_run",
            )

        self.assertEqual(code, 0)
        mock_scaffold.assert_called_once_with("neuro_canvas", target_dir="/tmp/current", dry_run=True)

    def test_main_scaffold_mode_bypasses_codex_bootstrap(self) -> None:
        with (
            patch.object(fixer_wire, "run_scaffold_cli", return_value=0) as mock_scaffold,
            patch.object(fixer_wire, "bootstrap_codex_pro_import_path", side_effect=AssertionError("should not run")),
        ):
            code = fixer_wire.main(["--scaffold-mvp", "neuro_canvas", "--dry-run"])

        self.assertEqual(code, 0)
        mock_scaffold.assert_called_once_with("neuro_canvas", target_dir=None, dry_run=True)

    def test_main_scaffold_mode_rejects_role_combination(self) -> None:
        code = fixer_wire.main(["--role", "fixer", "--scaffold-mvp", "neuro_canvas"])
        self.assertEqual(code, 2)

    def test_main_plain_fixer_can_route_into_interactive_scaffold(self) -> None:
        fake_pkg = types.ModuleType("codex_pro_app")
        fake_ui = type(
            "_FakeUi",
            (),
            {
                "Option": _DummyOption,
                "single_select_items": staticmethod(lambda *_args, **_kwargs: fixer_wire.SCAFFOLD_MVP_ACTION),
                "multi_select_items": staticmethod(lambda *_args, **_kwargs: []),
            },
        )

        with (
            patch.object(fixer_wire, "bootstrap_codex_pro_import_path", return_value=Path("/tmp/mcp-root")),
            patch.dict(
                sys.modules,
                {
                    "codex_pro_app": fake_pkg,
                    "codex_pro_app.main": _fake_codex_main_module(),
                    "codex_pro_app.ui": fake_ui,
                },
            ),
            patch.object(fixer_wire, "_launch_scaffold_interactive", return_value=0) as mock_launch,
        ):
            code = fixer_wire.main([])

        self.assertEqual(code, 0)
        mock_launch.assert_called_once()


if __name__ == "__main__":
    unittest.main()
