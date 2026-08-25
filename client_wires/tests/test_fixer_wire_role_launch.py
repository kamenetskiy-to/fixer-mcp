from __future__ import annotations

import os
import sqlite3
import sys
import tempfile
import types
import unittest
from dataclasses import replace
from datetime import datetime, timezone
from pathlib import Path
from unittest.mock import Mock, patch

from client_wires import fixer_wire
from client_wires import fixer_wire_role_launch
from client_wires.backends.antigravity_adapter import AntigravityBackendAdapter
from client_wires.backends.droid_adapter import DroidBackendAdapter


class _DummyOption:
    def __init__(self, label: str, value: object | None = None, *, disabled: bool = False, is_header: bool = False, **kwargs: object) -> None:
        self.label = label
        self.value = value
        self.disabled = disabled
        self.is_header = is_header


def _fake_codex_main_module() -> types.ModuleType:
    module = types.ModuleType("client_wires.codex_compat.llm")

    class ExecutionPreferences:
        def __init__(self, dangerous_sandbox: bool, auto_approve: bool) -> None:
            self.dangerous_sandbox = dangerous_sandbox
            self.auto_approve = auto_approve

    class LLMSelection:
        def __init__(
            self,
            *,
            display_model: str,
            detail: str,
            provider_slug: str,
            model: str,
            reasoning_effort: str,
            requires_provider_override: bool,
        ) -> None:
            self.display_model = display_model
            self.detail = detail
            self.provider_slug = provider_slug
            self.model = model
            self.reasoning_effort = reasoning_effort
            self.requires_provider_override = requires_provider_override

    def _load_llm_env() -> dict[str, str]:
        return {}

    def _merge_env_with_os(llm_env: dict[str, str]) -> dict[str, str]:
        merged = dict(os.environ)
        merged.update(llm_env)
        return merged

    def _ensure_sqlite_scaffold(_cwd: Path) -> None:
        return None

    def _reasoning_label(model: str, effort: str) -> str:
        return f"{model}:{effort}"

    module.ExecutionPreferences = ExecutionPreferences
    module.LLMSelection = LLMSelection
    module.CONFIG_ENV_VARS = {}
    module._load_llm_env = _load_llm_env
    module._merge_env_with_os = _merge_env_with_os
    module._ensure_sqlite_scaffold = _ensure_sqlite_scaffold
    module._reasoning_label = _reasoning_label
    return module


class _FakeAdapter:
    name = "codex"
    command = "codex"
    supports_resume = True

    @staticmethod
    def build_llm_args(llm_selection: object) -> list[str]:
        return ["--model", getattr(llm_selection, "model")]

    @staticmethod
    def build_execution_args(execution_prefs: object) -> list[str]:
        sandbox = "dangerous" if getattr(execution_prefs, "dangerous_sandbox") else "workspace-write"
        return ["--sandbox", sandbox]

    @staticmethod
    def build_interactive_execution_args(execution_prefs: object) -> list[str]:
        return ["--remote", "ws://127.0.0.1:14243", *_FakeAdapter.build_execution_args(execution_prefs)]

    @staticmethod
    def build_mcp_flags(selected_servers: dict[str, object], _available: dict[str, object]) -> list[str]:
        return [f"--mcp={','.join(sorted(selected_servers.keys()))}"]

    @staticmethod
    def build_prompt_args(prompt: str) -> list[str]:
        return ["--prompt", prompt]

    @staticmethod
    def prepare_env(_env: dict[str, str], _llm_selection: object) -> None:
        return None

    @staticmethod
    def ensure_runtime_files(_cwd: Path, _selection: object, _selected: dict[str, object], _available: dict[str, object]) -> None:
        return None


class FixerWireRoleLaunchExtractionTests(unittest.TestCase):
    def test_role_launch_wrapper_delegates_to_role_launch_module_with_facade_callbacks(self) -> None:
        with patch.object(fixer_wire_role_launch, "launch_fresh_role_session", return_value=37) as delegated:
            code = fixer_wire._launch_fresh_role_session(
                "fixer",
                "prompt",
                ["--flag"],
                selected_mcp_names=[fixer_wire.FORCED_MCP_SERVER],
                dry_run=True,
                preset_backend="codex",
                preset_model="gpt-5.4",
                preset_reasoning="medium",
                dangerous_sandbox=True,
                Option=_DummyOption,
                single_select_items=lambda *_a, **_k: None,
            )

        self.assertEqual(code, 37)
        delegated.assert_called_once()
        callbacks = delegated.call_args.kwargs["callbacks"]
        self.assertIs(callbacks.load_available_servers, fixer_wire._load_available_servers)
        self.assertIs(callbacks.build_fixer_prompt, fixer_wire._build_fixer_prompt)
        self.assertIs(callbacks.launch_fresh_role_session, fixer_wire._launch_fresh_role_session)
        self.assertTrue(hasattr(fixer_wire_role_launch, "launch_fixer"))
        self.assertTrue(hasattr(fixer_wire, "_launch_fixer"))

    def test_launch_fixer_new_preserves_patchable_facade_fresh_launch_dependency(self) -> None:
        available = {fixer_wire.FORCED_MCP_SERVER: {"command": "fixer_mcp"}}
        with (
            patch.dict(sys.modules, {"client_wires.codex_compat.llm": _fake_codex_main_module()}),
            patch.object(fixer_wire, "_assert_project_is_registered", return_value=None),
            patch.object(fixer_wire, "_select_fixer_launch_action_interactive", return_value=fixer_wire.FIXER_LAUNCH_NEW),
            patch.object(
                fixer_wire,
                "_load_available_servers",
                return_value=(available, {}, _FakeAdapter(), (lambda _cwd: None)),
            ),
            patch.object(fixer_wire, "_build_fixer_prompt", return_value="patched facade fixer prompt"),
            patch.object(fixer_wire, "_launch_fresh_role_session", return_value=41) as fresh_launch,
        ):
            code = fixer_wire._launch_fixer(
                ["--extra"],
                dry_run=True,
                preset_resume_latest=False,
                preset_resume_session_id=None,
                Option=_DummyOption,
                single_select_items=lambda *_a, **_k: None,
            )

        self.assertEqual(code, 41)
        fresh_launch.assert_called_once()
        self.assertEqual(fresh_launch.call_args.args[:3], ("fixer", "patched facade fixer prompt", ["--extra"]))
        self.assertEqual(fresh_launch.call_args.kwargs["selected_mcp_names"], [fixer_wire.FORCED_MCP_SERVER])


class LaunchFixerFlowTests(unittest.TestCase):
    def setUp(self) -> None:
        self._tmp_dir = tempfile.TemporaryDirectory()
        self._db_path = Path(self._tmp_dir.name) / "fixer.db"
        self._resolve_db_path = patch.object(fixer_wire, "_resolve_fixer_db_path", return_value=self._db_path)
        self._assert_project_registered = patch.object(fixer_wire, "_assert_project_is_registered", return_value=None)
        self._resolve_db_path.start()
        self._assert_project_registered.start()

    def tearDown(self) -> None:
        self._assert_project_registered.stop()
        self._resolve_db_path.stop()
        self._tmp_dir.cleanup()

    def _load_available_servers(self) -> tuple[dict[str, dict[str, object]], dict[str, str], _FakeAdapter, object]:
        available = {fixer_wire.FORCED_MCP_SERVER: {"command": "fixer_mcp"}}
        return available, {}, _FakeAdapter(), (lambda _cwd: None)

    def test_launch_new_fixer_chat_forwards_all_supported_provider_selections(self) -> None:
        provider_matrix = (
            ("codex", "gpt-5.6-sol", "high"),
            ("agy", "Gemini 3.6 Flash", "high"),
            ("claude", "sonnet", "medium"),
            ("kimi-code", "kimi-k2.7-code", "high"),
            ("droid", "kimi-k2.7-code", "high"),
            ("junie", "kimi-k2.6", "default"),
        )

        for backend, model, reasoning in provider_matrix:
            with self.subTest(backend=backend):
                with (
                    patch.object(
                        fixer_wire,
                        "_load_available_servers",
                        return_value=self._load_available_servers(),
                    ),
                    patch.object(fixer_wire, "_build_fixer_prompt", return_value=fixer_wire.FIXER_SKILL_MARKER),
                    patch.object(fixer_wire, "_launch_fresh_role_session", return_value=0) as launch,
                ):
                    result = fixer_wire_role_launch.launch_new_fixer_chat(
                        launch_cwd=Path.cwd(),
                        backend=backend,
                        model=model,
                        reasoning=reasoning,
                        dry_run=True,
                        Option=_DummyOption,
                        single_select_items=lambda *_a, **_k: None,
                        callbacks=fixer_wire._role_launch_callbacks(),
                    )

                self.assertEqual(result, 0)
                launch.assert_called_once()
                self.assertEqual(launch.call_args.args[:3], ("fixer", fixer_wire.FIXER_SKILL_MARKER, ()))
                self.assertEqual(
                    launch.call_args.kwargs["preset_backend"],
                    "antigravity" if backend == "agy" else backend,
                )
                self.assertEqual(launch.call_args.kwargs["preset_model"], model)
                self.assertEqual(launch.call_args.kwargs["preset_reasoning"], reasoning)
                self.assertEqual(launch.call_args.kwargs["selected_mcp_names"], [fixer_wire.FORCED_MCP_SERVER])
                self.assertTrue(launch.call_args.kwargs["dangerous_sandbox"])

    def test_launch_new_fixer_chat_constructs_locked_command_without_secret_arguments(self) -> None:
        captured: dict[str, object] = {}

        def capture_runtime_files(
            cwd: Path,
            selection: object,
            selected: dict[str, object],
            _available: dict[str, object],
        ) -> None:
            captured["cwd"] = cwd
            captured["selection"] = selection
            captured["selected"] = selected

        with (
            patch.dict(sys.modules, {"client_wires.codex_compat.llm": _fake_codex_main_module()}),
            patch.object(fixer_wire, "_load_available_servers", side_effect=lambda *_a, **_k: self._load_available_servers()),
            patch.object(
                fixer_wire,
                "_select_fresh_launch_selection",
                return_value=fixer_wire.SessionLaunchSelection("codex", "gpt-5.6-sol", "high"),
            ),
            patch.object(_FakeAdapter, "ensure_runtime_files", side_effect=capture_runtime_files),
            patch("client_wires.fixer_wire_role_launch.subprocess.call", return_value=0) as mock_call,
        ):
            result = fixer_wire_role_launch.launch_new_fixer_chat(
                launch_cwd=Path.cwd(),
                backend="codex",
                model="gpt-5.6-sol",
                reasoning="high",
                dry_run=False,
                Option=_DummyOption,
                single_select_items=lambda *_a, **_k: None,
                callbacks=fixer_wire._role_launch_callbacks(),
            )

        self.assertEqual(result, 0)
        command = mock_call.call_args.args[0]
        self.assertEqual(command[0], "codex")
        self.assertIn("--mcp=fixer_mcp", command)
        self.assertTrue(any(fixer_wire.FIXER_SKILL_MARKER in argument for argument in command))
        self.assertFalse(any("TOKEN" in argument or "API_KEY" in argument for argument in command))
        selected = captured["selected"]
        server_env = selected[fixer_wire.FORCED_MCP_SERVER]["env"]
        self.assertEqual(server_env[fixer_wire.FIXER_MCP_LOCKED_ROLE_ENV], "fixer")
        self.assertEqual(getattr(captured["selection"], "model"), "gpt-5.6-sol")

    def test_launch_fixer_new_builds_fresh_codex_command(self) -> None:
        captured: dict[str, object] = {}

        def capture_runtime_files(
            cwd: Path,
            selection: object,
            selected: dict[str, object],
            available: dict[str, object],
        ) -> None:
            captured["runtime_cwd"] = cwd
            captured["selection"] = selection
            captured["selected"] = selected
            captured["available"] = available

        with (
            patch.dict(sys.modules, {"client_wires.codex_compat.llm": _fake_codex_main_module()}),
            patch.object(fixer_wire, "_load_available_servers", side_effect=lambda *_a, **_k: self._load_available_servers()),
            patch.object(fixer_wire, "_assert_project_is_registered", return_value=None),
            patch.object(fixer_wire, "_select_fixer_launch_action_interactive", return_value=fixer_wire.FIXER_LAUNCH_NEW),
            patch.object(
                fixer_wire,
                "_select_fresh_launch_selection",
                return_value=fixer_wire.SessionLaunchSelection("codex", "gpt-5.4", "medium"),
            ),
            patch.object(_FakeAdapter, "ensure_runtime_files", side_effect=capture_runtime_files),
            patch("client_wires.fixer_wire.subprocess.call", return_value=0) as mock_call,
        ):
            code = fixer_wire._launch_fixer(
                [],
                dry_run=False,
                preset_resume_latest=False,
                preset_resume_session_id=None,
                Option=_DummyOption,
                single_select_items=lambda *_a, **_k: None,
            )

        self.assertEqual(code, 0)
        cmd = mock_call.call_args.args[0]
        self.assertEqual(cmd[0], "codex")
        self.assertNotIn("--remote", cmd)
        self.assertNotIn("resume", cmd)
        self.assertIn("--prompt", cmd)
        self.assertIn("--mcp=fixer_mcp", cmd)
        self.assertIn("--model", cmd)
        model_index = cmd.index("--model")
        self.assertEqual(cmd[model_index + 1], "gpt-5.4")
        self.assertIn("--sandbox", cmd)
        self.assertIn("dangerous", cmd)
        self.assertEqual(captured["runtime_cwd"], Path.cwd().resolve())
        self.assertEqual(getattr(captured["selection"], "model"), "gpt-5.4")
        selected = captured["selected"]
        self.assertIn(fixer_wire.FORCED_MCP_SERVER, selected)
        server_env = selected[fixer_wire.FORCED_MCP_SERVER]["env"]
        self.assertEqual(server_env[fixer_wire.FIXER_MCP_LOCKED_ROLE_ENV], "fixer")

    def test_launch_unattached_fixer_uses_scratch_cwd_and_locked_fixer_mcp(self) -> None:
        captured: dict[str, object] = {}

        def capture_runtime_files(
            cwd: Path,
            _selection: object,
            selected: dict[str, object],
            _available: dict[str, object],
        ) -> None:
            captured["runtime_cwd"] = cwd
            captured["selected"] = selected

        with tempfile.TemporaryDirectory() as tmp:
            tmp_path = Path(tmp)
            scratch_cwd = tmp_path / "scratch"
            db_path = tmp_path / "fixer.db"
            conn = sqlite3.connect(db_path)
            try:
                conn.execute(
                    """
                    CREATE TABLE project (
                        id INTEGER PRIMARY KEY AUTOINCREMENT,
                        name TEXT NOT NULL,
                        cwd TEXT UNIQUE NOT NULL
                    );
                    """
                )
                conn.commit()
            finally:
                conn.close()

            with (
                patch.dict(sys.modules, {"client_wires.codex_compat.llm": _fake_codex_main_module()}),
                patch.dict(os.environ, {fixer_wire.FIXER_UNATTACHED_CWD_ENV: str(scratch_cwd)}),
                patch.object(fixer_wire, "_resolve_fixer_db_path", return_value=db_path),
                patch.object(fixer_wire, "_load_available_servers", side_effect=lambda *_a, **_k: self._load_available_servers()),
                patch.object(
                    fixer_wire,
                    "_select_fresh_launch_selection",
                    return_value=fixer_wire.SessionLaunchSelection("codex", "gpt-5.4", "medium"),
                ),
                patch.object(_FakeAdapter, "ensure_runtime_files", side_effect=capture_runtime_files),
                patch("client_wires.fixer_wire.subprocess.call", return_value=0) as mock_call,
            ):
                code = fixer_wire._launch_unattached_fixer(
                    [],
                    dry_run=False,
                    Option=_DummyOption,
                    single_select_items=lambda *_a, **_k: None,
                )

        self.assertEqual(code, 0)
        cmd = mock_call.call_args.args[0]
        call_kwargs = mock_call.call_args.kwargs
        self.assertEqual(call_kwargs["cwd"], str(scratch_cwd.resolve()))
        self.assertIn("--prompt", cmd)
        prompt = cmd[cmd.index("--prompt") + 1]
        self.assertIn("Activate skill $init-unattached-fixer immediately.", prompt)
        self.assertIn("Unattached Fixer mode", prompt)
        self.assertIn(str(scratch_cwd.resolve()), prompt)
        self.assertEqual(captured["runtime_cwd"], scratch_cwd.resolve())
        server_env = captured["selected"][fixer_wire.FORCED_MCP_SERVER]["env"]
        self.assertEqual(server_env[fixer_wire.FIXER_DB_PATH_ENV], str(db_path))
        self.assertEqual(server_env[fixer_wire.FIXER_MCP_LOCKED_ROLE_ENV], "fixer")

    def test_launch_fixer_new_builds_fresh_droid_command(self) -> None:
        def load_available_servers(*_args: object, **_kwargs: object) -> tuple[dict[str, dict[str, object]], dict[str, str], DroidBackendAdapter, object]:
            return ({fixer_wire.FORCED_MCP_SERVER: {"command": "fixer_mcp"}}, {}, DroidBackendAdapter(), (lambda _cwd: None))

        with (
            patch.dict(sys.modules, {"client_wires.codex_compat.llm": _fake_codex_main_module()}),
            patch.object(fixer_wire, "_load_available_servers", side_effect=load_available_servers),
            patch.object(fixer_wire, "_assert_project_is_registered", return_value=None),
            patch.object(fixer_wire, "_select_fixer_launch_action_interactive", return_value=fixer_wire.FIXER_LAUNCH_NEW),
            patch.object(
                fixer_wire,
                "_select_fresh_launch_selection",
                return_value=fixer_wire.SessionLaunchSelection("droid", "gpt-5.4", "medium"),
            ),
            patch("client_wires.fixer_wire.subprocess.call", return_value=0) as mock_call,
        ):
            code = fixer_wire._launch_fixer(
                [],
                dry_run=False,
                preset_resume_latest=False,
                preset_resume_session_id=None,
                Option=_DummyOption,
                single_select_items=lambda *_a, **_k: None,
            )

        self.assertEqual(code, 0)
        cmd = mock_call.call_args.args[0]
        self.assertEqual(cmd[0], "droid")
        self.assertNotIn("exec", cmd)
        self.assertNotIn("--skip-permissions-unsafe", cmd)
        self.assertFalse(any("FIXER MCP TOOL ACCESS" in item for item in cmd))
        self.assertFalse(any("fixer_mcp___" in item for item in cmd))
        self.assertFalse(any("mcp_fixer_mcp_assume_role" in item for item in cmd))

    def test_launch_fixer_new_builds_fresh_antigravity_command(self) -> None:
        def load_available_servers(*_args: object, **_kwargs: object) -> tuple[dict[str, dict[str, object]], dict[str, str], AntigravityBackendAdapter, object]:
            return ({fixer_wire.FORCED_MCP_SERVER: {"command": "fixer_mcp"}}, {}, AntigravityBackendAdapter(), (lambda _cwd: None))

        with (
            patch.dict(sys.modules, {"client_wires.codex_compat.llm": _fake_codex_main_module()}),
            patch.object(fixer_wire, "_load_available_servers", side_effect=load_available_servers),
            patch.object(fixer_wire, "_assert_project_is_registered", return_value=None),
            patch.object(fixer_wire, "_resolve_fixer_db_path", return_value=Path("/tmp/fixer.db")),
            patch.object(fixer_wire, "_select_fixer_launch_action_interactive", return_value=fixer_wire.FIXER_LAUNCH_NEW),
            patch.object(
                fixer_wire,
                "_select_fresh_launch_selection",
                return_value=fixer_wire.SessionLaunchSelection(
                    "antigravity",
                    "Gemini 3.6 Flash (High)",
                    "default",
                ),
            ),
            patch.object(AntigravityBackendAdapter, "ensure_runtime_files", return_value=None),
            patch("client_wires.fixer_wire.subprocess.call", return_value=0) as mock_call,
        ):
            code = fixer_wire._launch_fixer(
                [],
                dry_run=False,
                preset_resume_latest=False,
                preset_resume_session_id=None,
                Option=_DummyOption,
                single_select_items=lambda *_a, **_k: None,
            )

        self.assertEqual(code, 0)
        cmd = mock_call.call_args.args[0]
        self.assertEqual(cmd[0], "agy")
        self.assertIn("--model", cmd)
        self.assertEqual(cmd[cmd.index("--model") + 1], "Gemini 3.6 Flash (High)")
        self.assertIn("--dangerously-skip-permissions", cmd)
        self.assertNotIn("-p", cmd)
        self.assertNotIn("--print", cmd)
        self.assertIn("--prompt-interactive", cmd)
        prompt = cmd[cmd.index("--prompt-interactive") + 1]
        self.assertTrue(prompt.startswith("/init-fixer\n"))
        self.assertNotIn("Activate skill $init-fixer immediately.", prompt)

    def test_launch_fixer_resume_builds_resume_codex_command(self) -> None:
        summary = types.SimpleNamespace(
            session_id="resume-456",
            created=datetime(2026, 2, 1, 10, 0, tzinfo=timezone.utc),
            updated=datetime(2026, 2, 1, 12, 30, tzinfo=timezone.utc),
            preview="Fixer session",
        )
        captured: dict[str, object] = {}

        def capture_runtime_files(
            cwd: Path,
            selection: object,
            selected: dict[str, object],
            available: dict[str, object],
        ) -> None:
            captured["runtime_cwd"] = cwd
            captured["selection"] = selection
            captured["selected"] = selected
            captured["available"] = available

        with (
            patch.dict(sys.modules, {"client_wires.codex_compat.llm": _fake_codex_main_module()}),
            patch.object(fixer_wire, "_load_available_servers", side_effect=lambda *_a, **_k: self._load_available_servers()),
            patch.object(fixer_wire, "_assert_project_is_registered", return_value=None),
            patch.object(fixer_wire, "_select_fixer_launch_action_interactive", return_value=fixer_wire.FIXER_LAUNCH_RESUME),
            patch.object(fixer_wire, "_load_fixer_resume_summaries", return_value=[summary]),
            patch.object(fixer_wire, "_select_fixer_resume_session_interactive", return_value="resume-456"),
            patch.object(_FakeAdapter, "ensure_runtime_files", side_effect=capture_runtime_files),
            patch("client_wires.fixer_wire.subprocess.call", return_value=0) as mock_call,
        ):
            code = fixer_wire._launch_fixer(
                ["--foo"],
                dry_run=False,
                preset_resume_latest=False,
                preset_resume_session_id=None,
                Option=_DummyOption,
                single_select_items=lambda *_a, **_k: None,
            )

        self.assertEqual(code, 0)
        cmd = mock_call.call_args.args[0]
        self.assertEqual(cmd[0], "codex")
        self.assertEqual(cmd[1], "resume")
        self.assertNotIn("--remote", cmd)
        self.assertEqual(cmd[-1], "resume-456")
        self.assertNotIn("--prompt", cmd)
        self.assertNotIn("--model", cmd)
        self.assertIn("--sandbox", cmd)
        self.assertIn("dangerous", cmd)
        self.assertEqual(captured["runtime_cwd"], Path.cwd().resolve())
        self.assertEqual(getattr(captured["selection"], "model"), fixer_wire.FIXER_WIRE_MODEL)
        selected = captured["selected"]
        self.assertIn(fixer_wire.FORCED_MCP_SERVER, selected)
        server_env = selected[fixer_wire.FORCED_MCP_SERVER]["env"]
        self.assertEqual(server_env[fixer_wire.FIXER_MCP_LOCKED_ROLE_ENV], "fixer")

    def test_launch_fixer_resume_uses_selected_provider_adapter(self) -> None:
        summary = types.SimpleNamespace(
            provider="claude",
            session_id="claude-session-456",
            created=datetime(2026, 2, 1, 10, 0, tzinfo=timezone.utc),
            updated=datetime(2026, 2, 1, 12, 30, tzinfo=timezone.utc),
            preview="Claude Fixer session",
        )
        captured: dict[str, object] = {}

        class ClaudeAdapter(_FakeAdapter):
            name = "claude"
            command = "claude"
            default_model = "sonnet"
            default_reasoning = "default"

            @staticmethod
            def build_interactive_execution_args(execution_prefs: object) -> list[str]:
                return ["--dangerously-skip-permissions"] if getattr(execution_prefs, "auto_approve") else []

            @staticmethod
            def build_mcp_flags(_selected: dict[str, object], _available: dict[str, object]) -> list[str]:
                return []

            @staticmethod
            def build_resume_command(option_args: list[str], external_session_id: str) -> list[str]:
                return ["claude", "--resume", external_session_id, *option_args]

            @staticmethod
            def ensure_runtime_files(
                cwd: Path,
                selection: object,
                selected: dict[str, object],
                available: dict[str, object],
            ) -> None:
                captured["runtime_cwd"] = cwd
                captured["selection"] = selection
                captured["selected"] = selected
                captured["available"] = available

        def load_available(_cwd: Path, *, backend: str | None = None) -> tuple[dict[str, dict[str, object]], dict[str, str], ClaudeAdapter, object]:
            captured["backend"] = backend
            return {fixer_wire.FORCED_MCP_SERVER: {"command": "fixer_mcp"}}, {}, ClaudeAdapter(), (lambda _cwd: None)

        with (
            patch.dict(sys.modules, {"client_wires.codex_compat.llm": _fake_codex_main_module()}),
            patch.object(fixer_wire, "_load_available_servers", side_effect=load_available),
            patch.object(fixer_wire, "_assert_project_is_registered", return_value=None),
            patch.object(fixer_wire, "_select_fixer_launch_action_interactive", return_value=fixer_wire.FIXER_LAUNCH_RESUME),
            patch.object(fixer_wire, "_load_fixer_resume_summaries", return_value=[summary]),
            patch.object(fixer_wire, "_select_fixer_resume_session_interactive", return_value="claude:claude-session-456"),
            patch("client_wires.fixer_wire.subprocess.call", return_value=0) as mock_call,
        ):
            code = fixer_wire._launch_fixer(
                [],
                dry_run=False,
                preset_resume_latest=False,
                preset_resume_session_id=None,
                Option=_DummyOption,
                single_select_items=lambda *_a, **_k: None,
            )

        self.assertEqual(code, 0)
        self.assertEqual(captured["backend"], "claude")
        cmd = mock_call.call_args.args[0]
        self.assertEqual(cmd[:3], ["claude", "--resume", "claude-session-456"])
        self.assertIn("--dangerously-skip-permissions", cmd)
        self.assertNotIn("--model", cmd)
        self.assertEqual(captured["runtime_cwd"], Path.cwd().resolve())
        self.assertIn(fixer_wire.FORCED_MCP_SERVER, captured["selected"])

    def test_launch_fixer_resume_latest_skips_interactive_picker(self) -> None:
        with (
            patch.dict(sys.modules, {"client_wires.codex_compat.llm": _fake_codex_main_module()}),
            patch.object(fixer_wire, "_load_available_servers", side_effect=lambda *_a, **_k: self._load_available_servers()),
            patch.object(fixer_wire, "_assert_project_is_registered", return_value=None),
            patch.object(fixer_wire, "_resolve_latest_fixer_resume_session_id", return_value="resume-latest-1"),
            patch.object(fixer_wire, "_select_fixer_launch_action_interactive") as mock_select_mode,
            patch("client_wires.fixer_wire.subprocess.call", return_value=0) as mock_call,
        ):
            code = fixer_wire._launch_fixer(
                [],
                dry_run=False,
                preset_resume_latest=True,
                preset_resume_session_id=None,
                Option=_DummyOption,
                single_select_items=lambda *_a, **_k: None,
            )

        self.assertEqual(code, 0)
        mock_select_mode.assert_not_called()
        cmd = mock_call.call_args.args[0]
        self.assertEqual(cmd[0], "codex")
        self.assertEqual(cmd[1], "resume")
        self.assertEqual(cmd[-1], "resume-latest-1")

    def test_role_preset_server_names_excludes_react_native_guide_for_fixer(self) -> None:
        available = {
            "react-native-guide": {"command": "npx", "_source": "project_mcp"},
            "project_tool": {"command": "project-tool", "_source": "project_mcp"},
            fixer_wire.FORCED_MCP_SERVER: {"command": "fixer_mcp"},
        }
        selected = fixer_wire._select_role_preset_server_names(
            available,
            cwd=Path("/tmp/project"),
            role="fixer",
        )
        self.assertEqual(selected, [fixer_wire.FORCED_MCP_SERVER, "project_tool"])

    def test_role_preset_server_names_excludes_react_native_guide_for_overseer(self) -> None:
        available = {
            "react-native-guide": {"command": "npx", "_source": "project_mcp"},
            "project_tool": {"command": "project-tool", "_source": "project_mcp"},
            fixer_wire.FORCED_MCP_SERVER: {"command": "fixer_mcp"},
        }
        selected = fixer_wire._select_role_preset_server_names(
            available,
            cwd=Path("/tmp/project"),
            role="overseer",
        )
        self.assertEqual(selected, [fixer_wire.FORCED_MCP_SERVER, "project_tool"])


class LaunchOverseerFlowTests(unittest.TestCase):
    def setUp(self) -> None:
        self._tmp_dir = tempfile.TemporaryDirectory()
        self._db_path = Path(self._tmp_dir.name) / "fixer.db"
        self._resolve_db_path = patch.object(fixer_wire, "_resolve_fixer_db_path", return_value=self._db_path)
        self._assert_project_registered = patch.object(fixer_wire, "_assert_project_is_registered", return_value=None)
        self._resolve_db_path.start()
        self._assert_project_registered.start()

    def tearDown(self) -> None:
        self._assert_project_registered.stop()
        self._resolve_db_path.stop()
        self._tmp_dir.cleanup()

    def test_main_overseer_role_does_not_inject_unlocked_forced_override(self) -> None:
        fake_package = types.ModuleType("client_wires.codex_compat")
        fake_ui = types.ModuleType("client_wires.codex_compat.ui")
        fake_ui.Option = _DummyOption
        fake_ui.multi_select_items = lambda *_a, **_k: []
        fake_ui.single_select_items = lambda *_a, **_k: None
        fake_main = _fake_codex_main_module()
        fake_package.main = fake_main
        fake_package.ui = fake_ui
        captured: dict[str, object] = {}

        def capture_overseer_launch(
            passthrough_args: object,
            *,
            dry_run: bool,
            Option: object,
            single_select_items: object,
        ) -> int:
            captured["passthrough_args"] = list(passthrough_args)
            captured["dry_run"] = dry_run
            captured["Option"] = Option
            captured["single_select_items"] = single_select_items
            return 0

        with (
            patch.dict(
                sys.modules,
                {
                    "client_wires.codex_compat": fake_package,
                    "client_wires.codex_compat.llm": fake_main,
                    "client_wires.codex_compat.ui": fake_ui,
                },
            ),
            patch.object(fixer_wire, "bootstrap_codex_pro_import_path", return_value=Path("/tmp/codex-pro")),
            patch.object(
                fixer_wire,
                "_build_forced_fixer_override_args",
                side_effect=AssertionError("overseer role should not build unlocked forced fixer_mcp overrides"),
            ),
            patch.object(fixer_wire, "_launch_overseer", side_effect=capture_overseer_launch),
        ):
            code = fixer_wire.main(["--role", "overseer", "--dry-run", "--extra-codex-flag"])

        self.assertEqual(code, 0)
        self.assertEqual(captured["passthrough_args"], ["--extra-codex-flag"])
        self.assertTrue(captured["dry_run"])
        self.assertIs(captured["Option"], _DummyOption)

    def test_launch_overseer_uses_selected_backend_and_forced_mcp(self) -> None:
        def load_available_servers(*_args: object, **_kwargs: object) -> tuple[dict[str, dict[str, object]], dict[str, str], DroidBackendAdapter, object]:
            available = {
                "project_tool": {"command": "project-tool", "_source": "project_mcp"},
                fixer_wire.FORCED_MCP_SERVER: {"command": "fixer_mcp"},
            }
            return available, {}, DroidBackendAdapter(), (lambda _cwd: None)

        with (
            patch.dict(sys.modules, {"client_wires.codex_compat.llm": _fake_codex_main_module()}),
            patch.object(fixer_wire, "_load_available_servers", side_effect=load_available_servers),
            patch.object(
                fixer_wire,
                "_select_overseer_launch_action_interactive",
                return_value=fixer_wire.OVERSEER_LAUNCH_NEW,
            ),
            patch.object(
                fixer_wire,
                "_select_fresh_launch_selection",
                return_value=fixer_wire.SessionLaunchSelection("droid", "gpt-5.4", "medium"),
            ),
            patch("client_wires.fixer_wire.subprocess.call", return_value=0) as mock_call,
        ):
            code = fixer_wire._launch_overseer(
                [],
                dry_run=False,
                Option=_DummyOption,
                single_select_items=lambda *_a, **_k: None,
            )

        self.assertEqual(code, 0)
        cmd = mock_call.call_args.args[0]
        self.assertEqual(cmd[0], "droid")
        self.assertNotIn("exec", cmd)
        self.assertNotIn("--sandbox", cmd)

    def test_launch_overseer_resume_builds_resume_codex_command(self) -> None:
        summary = types.SimpleNamespace(
            session_id="overseer-789",
            created=datetime(2026, 2, 1, 10, 0, tzinfo=timezone.utc),
            updated=datetime(2026, 2, 1, 12, 30, tzinfo=timezone.utc),
            preview="Overseer session",
        )
        available = {
            "project_tool": {"command": "project-tool", "_source": "project_mcp"},
            fixer_wire.FORCED_MCP_SERVER: {"command": "fixer_mcp"},
        }
        with (
            patch.dict(sys.modules, {"client_wires.codex_compat.llm": _fake_codex_main_module()}),
            patch.object(
                fixer_wire,
                "_load_available_servers",
                return_value=(available, {}, _FakeAdapter(), (lambda _cwd: None)),
            ),
            patch.object(fixer_wire, "_assert_project_is_registered", return_value=None),
            patch.object(
                fixer_wire,
                "_select_overseer_launch_action_interactive",
                return_value=fixer_wire.OVERSEER_LAUNCH_RESUME,
            ),
            patch.object(fixer_wire, "_load_overseer_resume_summaries", return_value=[summary]),
            patch.object(fixer_wire, "_select_overseer_resume_session_interactive", return_value="overseer-789"),
            patch("client_wires.fixer_wire.subprocess.call", return_value=0) as mock_call,
        ):
            code = fixer_wire._launch_overseer(
                ["--foo"],
                dry_run=False,
                Option=_DummyOption,
                single_select_items=lambda *_a, **_k: None,
            )

        self.assertEqual(code, 0)
        cmd = mock_call.call_args.args[0]
        self.assertEqual(cmd[0], "codex")
        self.assertEqual(cmd[1], "resume")
        self.assertEqual(cmd[-1], "overseer-789")
        self.assertIn("--mcp=fixer_mcp,project_tool", cmd)

    def test_launch_overseer_accepts_explicit_cwd_and_backend_preferences(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            launch_cwd = Path(tmp).resolve()
            launch_fresh = Mock(return_value=0)
            callbacks = replace(
                fixer_wire._role_launch_callbacks(),
                select_overseer_launch_action_interactive=lambda *_a, **_k: fixer_wire.OVERSEER_LAUNCH_NEW,
                load_available_servers=lambda *_a, **_k: (
                    {fixer_wire.FORCED_MCP_SERVER: {"command": "fixer_mcp"}},
                    {},
                    _FakeAdapter(),
                    lambda _cwd: None,
                ),
                select_role_preset_server_names=lambda *_a, **_k: [fixer_wire.FORCED_MCP_SERVER],
                launch_fresh_role_session=launch_fresh,
            )
            with (
                patch.dict(sys.modules, {"client_wires.codex_compat.llm": _fake_codex_main_module()}),
            ):
                code = fixer_wire_role_launch.launch_overseer(
                    [],
                    launch_cwd=launch_cwd,
                    preset_backend="droid",
                    preset_model="glm-5.1",
                    preset_reasoning="high",
                    dry_run=True,
                    Option=_DummyOption,
                    single_select_items=lambda *_a, **_k: None,
                    callbacks=callbacks,
                )

        self.assertEqual(code, 0)
        self.assertEqual(launch_fresh.call_args.kwargs["launch_cwd"], launch_cwd)
        self.assertEqual(launch_fresh.call_args.kwargs["preset_backend"], "droid")
        self.assertEqual(launch_fresh.call_args.kwargs["preset_model"], "glm-5.1")
        self.assertEqual(launch_fresh.call_args.kwargs["preset_reasoning"], "high")

    def test_launch_overseer_resumes_discovered_droid_thread_on_its_provider(self) -> None:
        summary = types.SimpleNamespace(
            provider="droid",
            session_id="droid-overseer-7",
            created=datetime(2026, 2, 1, 10, 0, tzinfo=timezone.utc),
            updated=datetime(2026, 2, 1, 12, 30, tzinfo=timezone.utc),
            preview="Droid Overseer",
        )
        available = {
            fixer_wire.FORCED_MCP_SERVER: {"command": "fixer_mcp"},
        }
        load_servers = Mock(
            return_value=(available, {}, DroidBackendAdapter(), lambda _cwd: None)
        )
        callbacks = replace(
            fixer_wire._role_launch_callbacks(),
            select_overseer_launch_action_interactive=lambda *_a, **_k: fixer_wire.OVERSEER_LAUNCH_RESUME,
            load_overseer_resume_summaries=lambda _cwd: [summary],
            select_overseer_resume_session_interactive=lambda *_a, **_k: "droid-overseer-7",
            load_available_servers=load_servers,
            select_role_preset_server_names=lambda *_a, **_k: [fixer_wire.FORCED_MCP_SERVER],
        )
        with tempfile.TemporaryDirectory() as tmp:
            cwd = Path(tmp).resolve()
            with (
                patch.dict(sys.modules, {"client_wires.codex_compat.llm": _fake_codex_main_module()}),
                patch.object(DroidBackendAdapter, "ensure_runtime_files", return_value=None),
                patch("client_wires.fixer_wire_role_launch.subprocess.call", return_value=0) as mock_call,
            ):
                code = fixer_wire_role_launch.launch_overseer(
                    [],
                    launch_cwd=cwd,
                    dry_run=False,
                    Option=_DummyOption,
                    single_select_items=lambda *_a, **_k: None,
                    callbacks=callbacks,
                )

        self.assertEqual(code, 0)
        self.assertEqual(load_servers.call_args.kwargs["backend"], "droid")
        command = mock_call.call_args.args[0]
        self.assertEqual(command[:3], ["droid", "--resume", "droid-overseer-7"])
        self.assertEqual(mock_call.call_args.kwargs["cwd"], str(cwd))
