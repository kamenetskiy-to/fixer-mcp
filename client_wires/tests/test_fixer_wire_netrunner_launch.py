from __future__ import annotations

import dataclasses
import sqlite3
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from client_wires import fixer_wire
from client_wires import fixer_wire_hands_context
from client_wires import fixer_wire_netrunner_launch
from client_wires import fixer_wire_selectors
from client_wires.backends.antigravity_adapter import AntigravityBackendAdapter
from client_wires.backends.droid_adapter import DroidBackendAdapter
from client_wires.backends.junie_adapter import JunieBackendAdapter
from client_wires.tests.test_fixer_wire import (
    _FakeAdapter,
    _TrackingConnection,
    _fake_codex_main_module,
    _make_history_summary,
)


class _DummyOption:
    def __init__(self, label: str, value: object | None = None, *, disabled: bool = False, is_header: bool = False, **kwargs: object) -> None:
        self.label = label
        self.value = value
        self.disabled = disabled
        self.is_header = is_header


class FixerWireNetrunnerLaunchExtractionTests(unittest.TestCase):
    def test_command_display_redacts_embedded_environment_payload(self) -> None:
        displayed = fixer_wire_netrunner_launch._redacted_command_for_display(
            ["codex", "--config", 'mcp_servers.secret.env={\"TOKEN\":\"private\"}']
        )

        self.assertEqual(
            displayed,
            ["codex", "--config", "mcp_servers.secret.env=<redacted>"],
        )

    def test_launch_netrunner_wrapper_delegates_with_facade_callbacks(self) -> None:
        with patch.object(fixer_wire_netrunner_launch, "launch_netrunner", return_value=73) as delegated:
            code = fixer_wire._launch_netrunner(
                ["--flag"],
                preset_session_id=217,
                preset_backend="codex",
                preset_model="gpt-5.5",
                preset_reasoning="high",
                preset_mcp_names=[fixer_wire.FORCED_MCP_SERVER],
                dry_run=True,
                Option=_DummyOption,
                single_select_items=lambda *_a, **_k: None,
                multi_select_items=lambda *_a, **_k: [],
            )

        self.assertEqual(code, 73)
        delegated.assert_called_once()
        callbacks = delegated.call_args.kwargs["callbacks"]
        self.assertIsInstance(callbacks, fixer_wire_netrunner_launch.NetrunnerLaunchCallbacks)
        self.assertIs(callbacks.load_available_servers, fixer_wire._load_available_servers)
        self.assertIs(callbacks.build_mcp_how_to_map, fixer_wire._build_mcp_how_to_map)
        self.assertIs(callbacks.resolve_netrunner_resume_session_id, fixer_wire._resolve_netrunner_resume_session_id)
        self.assertIs(callbacks.append_codex_apps_gate, fixer_wire._append_codex_apps_gate)
        self.assertEqual(callbacks.forced_mcp_server, fixer_wire.FORCED_MCP_SERVER)

    def test_project_hands_state_reads_one_actor_and_provider_lanes(self) -> None:
        tmp = tempfile.TemporaryDirectory()
        db_path = Path(tmp.name) / "fixer.db"
        conn = sqlite3.connect(db_path)
        try:
            conn.executescript(
                """
                CREATE TABLE project (
                    id INTEGER PRIMARY KEY,
                    name TEXT NOT NULL,
                    cwd TEXT UNIQUE NOT NULL
                );
                CREATE TABLE project_hands (
                    project_id INTEGER PRIMARY KEY,
                    actor_id TEXT NOT NULL,
                    display_name TEXT NOT NULL,
                    authority_state TEXT NOT NULL,
                    default_lane TEXT NOT NULL
                );
                INSERT INTO project VALUES (1, 'Project', '/tmp/project');
                INSERT INTO project_hands
                VALUES (1, 'actor-1', 'Руки', 'enabled', 'codex');
                """
            )
        finally:
            conn.close()
        try:
            with (
                patch.object(fixer_wire, "_resolve_fixer_db_path", return_value=db_path),
                patch.object(fixer_wire, "_bootstrap_fixer_mcp_database"),
                patch.object(
                    fixer_wire,
                    "_ensure_wire_schema",
                    side_effect=AssertionError("Python must not migrate the Project Hands schema"),
                ),
                patch.object(fixer_wire, "_ensure_project_registered", return_value=1),
            ):
                state = fixer_wire._load_project_hands_state(Path("/tmp/project"))
        finally:
            tmp.cleanup()

        self.assertEqual(state.display_name, "Руки")
        self.assertEqual(state.default_lane, "codex")
        self.assertEqual(
            [lane.provider for lane in state.lanes],
            ["commandcode", "codex", "claude", "kimi", "antigravity", "grok"],
        )
        self.assertEqual(state.lanes[0].backend, "commandcode")
        self.assertEqual(state.lanes[2].backend, "claude")
        self.assertEqual(state.lanes[5].model, "grok-4.6")
        self.assertEqual(state.lanes[5].reasoning, "default")

    def test_project_hands_pi_provider_maps_to_the_pi_backend(self) -> None:
        # A permanent `pi` Hands lane reaches the launcher as provider="pi".
        # The provider key and the backend name are equal here, unlike the kimi
        # asymmetry, so the map must resolve without an alias hop.
        self.assertEqual(fixer_wire_netrunner_launch.HANDS_PROVIDER_BACKENDS["pi"], "pi")
        self.assertEqual(fixer_wire_netrunner_launch.HANDS_PROVIDER_BACKENDS["kimi"], "kimi-code")
        self.assertEqual(fixer_wire_netrunner_launch._canonical_hands_provider("pi"), "pi")

        lane = fixer_wire_netrunner_launch.ProjectHandsLane(
            "pi",
            "openai-codex/gpt-5.6-luna",
            "high",
        )

        self.assertEqual(lane.provider, "pi")
        self.assertEqual(lane.backend, "pi")
        self.assertEqual(lane.model, "openai-codex/gpt-5.6-luna")

    def test_project_hands_facade_bootstraps_go_schema_before_reading_state(self) -> None:
        cwd = Path("/tmp/project")
        db_path = Path("/tmp/fixer.db")
        expected = fixer_wire_netrunner_launch.ProjectHandsState(
            project_id=1,
            display_name="Руки",
            authority_state="enabled",
            default_lane="codex",
            lanes=(),
        )
        calls: list[tuple[str, object]] = []

        def record_bootstrap(path: Path) -> None:
            calls.append(("bootstrap", path))

        def record_read(path: Path, *, callbacks: object) -> fixer_wire_netrunner_launch.ProjectHandsState:
            calls.append(("read", path))
            self.assertIsInstance(callbacks, fixer_wire_netrunner_launch.NetrunnerLaunchCallbacks)
            return expected

        with (
            patch.object(fixer_wire, "_resolve_fixer_db_path", return_value=db_path),
            patch.object(fixer_wire, "_bootstrap_fixer_mcp_database", side_effect=record_bootstrap),
            patch.object(fixer_wire_netrunner_launch, "load_project_hands_state", side_effect=record_read),
        ):
            state = fixer_wire._load_project_hands_state(cwd)

        self.assertIs(state, expected)
        self.assertEqual(calls, [("bootstrap", db_path), ("read", cwd)])

    def test_project_hands_lane_selector_exposes_every_registered_lane(self) -> None:
        lanes = (
            fixer_wire_netrunner_launch.ProjectHandsLane("codex", "gpt", "high"),
            fixer_wire_netrunner_launch.ProjectHandsLane("kimi", "kimi-k3-256k", "default"),
        )
        captured: dict[str, object] = {}

        def choose_backend(preferred: str, option_cls: object, chooser: object) -> str:
            captured["backend_args"] = (preferred, option_cls, chooser)
            return "codex"

        def choose_model(backend: str, preferred: str, *_args: object, **_kwargs: object) -> str:
            captured["model_selection"] = (backend, preferred)
            return "gpt"

        selected = fixer_wire_selectors._select_project_hands_lane_interactive(
            lanes,
            "codex",
            _DummyOption,
            choose_backend,
            select_backend_interactive=choose_backend,
            select_model_interactive=choose_model,
        )

        self.assertEqual(selected.provider, "codex")
        self.assertEqual(selected.model, "gpt")
        self.assertEqual(captured["backend_args"][0], "codex")
        self.assertEqual(captured["model_selection"], ("codex", "gpt"))

    def test_project_hands_backend_picker_excludes_unregistered_backends(self) -> None:
        lanes = (
            fixer_wire_netrunner_launch.ProjectHandsLane("codex", "gpt", "high"),
            fixer_wire_netrunner_launch.ProjectHandsLane("kimi", "kimi-k3-256k", "default"),
        )
        captured: dict[str, object] = {}

        def choose_backend(
            preferred: str,
            option_cls: object,
            chooser: object,
            *,
            allowed_backends: set[str],
        ) -> str:
            captured["allowed_backends"] = allowed_backends
            return "codex"

        with patch.object(fixer_wire_selectors, "_select_backend_interactive", side_effect=choose_backend):
            selected = fixer_wire_selectors._select_project_hands_lane_interactive(
                lanes,
                "codex",
                _DummyOption,
                lambda *_args, **_kwargs: "gpt",
                select_model_interactive=lambda *_args, **_kwargs: "gpt",
            )

        self.assertEqual(selected.provider, "codex")
        self.assertEqual(captured["allowed_backends"], {"codex", "kimi-code"})

    def test_launch_project_hands_uses_disposable_fixer_client_and_durable_lane(self) -> None:
        state = fixer_wire_netrunner_launch.ProjectHandsState(
            project_id=1,
            display_name="Руки",
            authority_state="enabled",
            default_lane="codex",
            lanes=(
                fixer_wire_netrunner_launch.ProjectHandsLane(
                    "codex",
                    "gpt-5.6-sol",
                    "high",
                ),
            ),
        )
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        db_path = Path(tmp.name) / "fixer.db"
        conn = sqlite3.connect(db_path)
        try:
            conn.executescript(
                """
                CREATE TABLE project (id INTEGER PRIMARY KEY, name TEXT, cwd TEXT);
                CREATE TABLE mcp_server (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    name TEXT UNIQUE,
                    auto_attach INTEGER NOT NULL DEFAULT 0,
                    is_default INTEGER NOT NULL DEFAULT 0,
                    category TEXT,
                    how_to TEXT
                );
                CREATE TABLE project_mcp_server (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    project_id INTEGER NOT NULL,
                    mcp_server_id INTEGER NOT NULL,
                    created_at TEXT NOT NULL DEFAULT '',
                    UNIQUE(project_id, mcp_server_id)
                );
                CREATE TABLE project_doc (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    project_id INTEGER NOT NULL,
                    title TEXT NOT NULL,
                    content TEXT NOT NULL,
                    doc_type TEXT,
                    parent_doc_id INTEGER,
                    level INTEGER,
                    slug TEXT,
                    path TEXT,
                    status TEXT
                );
                """
            )
            conn.execute(
                "INSERT INTO project (id, name, cwd) VALUES (1, 'proj', ?)",
                (str(Path.cwd().resolve()),),
            )
            conn.execute("INSERT INTO mcp_server (name, is_default) VALUES ('fixer_mcp', 1)")
            conn.execute("INSERT INTO mcp_server (name, is_default) VALUES ('sqlite', 0)")
            conn.execute(
                "INSERT INTO project_doc (project_id, title, content, level, slug, path, status) "
                "VALUES (1, 'Overview', 'doc body', 0, 'overview', 'overview', 'current')"
            )
            conn.commit()
        finally:
            conn.close()

        real_callbacks_factory = fixer_wire._netrunner_launch_callbacks

        def callbacks_with_fake_servers() -> fixer_wire_netrunner_launch.NetrunnerLaunchCallbacks:
            callbacks = real_callbacks_factory()
            return dataclasses.replace(
                callbacks,
                load_available_servers=lambda _cwd, *, backend: (  # noqa: ARG005
                    {"fixer_mcp": {}, "sqlite": {}},
                    {},
                    None,
                    None,
                ),
            )

        with (
            patch.object(fixer_wire, "_load_project_hands_state", return_value=state),
            patch.object(fixer_wire, "_resolve_fixer_db_path", return_value=db_path),
            patch.object(
                fixer_wire,
                "_netrunner_launch_callbacks",
                side_effect=callbacks_with_fake_servers,
            ),
            patch(
                "client_wires.fixer_wire_role_launch.launch_fresh_role_session",
                return_value=0,
            ) as launched,
        ):
            selections = iter(
                (
                    fixer_wire_selectors.HANDS_WORKSPACE_SAFE,
                    "codex",
                    "openai",
                    "gpt-5.5",
                )
            )
            code = fixer_wire._launch_project_hands(
                [],
                preset_backend=None,
                preset_model=None,
                preset_reasoning=None,
                preset_mcp_names=[],
                acceptance=False,
                dry_run=True,
                Option=_DummyOption,
                single_select_items=lambda *_a, **_k: next(selections),
                multi_select_items=lambda *_a, **_k: [],
            )

        self.assertEqual(code, 0)
        self.assertEqual(launched.call_args.args[0], "netrunner")
        prompt = launched.call_args.args[1]
        self.assertIn("one permanent project actor `Руки`", prompt)
        self.assertIn("without creating or asking for a Netrunner session ID", prompt)
        self.assertEqual(launched.call_args.kwargs["selected_mcp_names"], [fixer_wire.FORCED_MCP_SERVER])
        self.assertEqual(launched.call_args.kwargs["preset_backend"], "codex")
        self.assertEqual(launched.call_args.kwargs["preset_model"], "gpt-5.5")
        self.assertTrue(launched.call_args.kwargs["dangerous_sandbox"])
        self.assertEqual(
            launched.call_args.kwargs["launch_label"],
            "disposable Project Hands mailbox client",
        )

    def test_launch_project_hands_rejects_session_scoped_overrides(self) -> None:
        with self.assertRaisesRegex(RuntimeError, "control-plane managed"):
            fixer_wire._launch_project_hands(
                [],
                preset_backend=None,
                preset_model=None,
                preset_reasoning=None,
                preset_mcp_names=["sqlite"],
                acceptance=False,
                dry_run=True,
                Option=_DummyOption,
                single_select_items=lambda *_a, **_k: "codex",
                multi_select_items=lambda *_a, **_k: [],
            )


class LaunchNetrunnerResumeFlowTests(unittest.TestCase):
    def _make_db(self) -> tuple[tempfile.TemporaryDirectory[str], Path]:
        tmp = tempfile.TemporaryDirectory()
        db_path = Path(tmp.name) / "fixer.db"
        conn = sqlite3.connect(db_path)
        try:
            conn.executescript(
                """
                CREATE TABLE project (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    name TEXT NOT NULL,
                    cwd TEXT UNIQUE NOT NULL
                );
                CREATE TABLE session (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    project_id INTEGER NOT NULL,
                    task_description TEXT NOT NULL,
                    status TEXT NOT NULL,
                    cli_backend TEXT NOT NULL DEFAULT 'codex',
                    cli_model TEXT NOT NULL DEFAULT '',
                    cli_reasoning TEXT NOT NULL DEFAULT ''
                );
                CREATE TABLE mcp_server (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    name TEXT NOT NULL UNIQUE,
                    auto_attach INTEGER NOT NULL DEFAULT 0,
                    is_default INTEGER NOT NULL DEFAULT 0,
                    category TEXT NOT NULL DEFAULT '',
                    how_to TEXT NOT NULL DEFAULT ''
                );
                CREATE TABLE session_mcp_server (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    session_id INTEGER NOT NULL,
                    mcp_server_id INTEGER NOT NULL
                );
                CREATE TABLE project_mcp_server (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    project_id INTEGER NOT NULL,
                    mcp_server_id INTEGER NOT NULL,
                    UNIQUE(project_id, mcp_server_id)
                );
                INSERT INTO project (id, name, cwd) VALUES (1, 'Fixer MCP', '/tmp/project');
                INSERT INTO session (id, project_id, task_description, status)
                VALUES (139, 1, 'Resume me', 'pending');
                INSERT INTO mcp_server (id, name, is_default, category, how_to)
                VALUES
                    (1, 'sqlite', 1, 'DB', 'Use sqlite'),
                    (2, 'playwright', 1, 'Coding', 'Use Playwright'),
                    (3, 'figma-console-mcp', 1, 'Design', 'Use Figma console');
                INSERT INTO project_mcp_server (project_id, mcp_server_id)
                VALUES (1, 1), (1, 2), (1, 3);
                """
            )
        finally:
            conn.close()
        return tmp, db_path

    def _load_available_servers(self) -> tuple[dict[str, dict[str, object]], dict[str, str], _FakeAdapter, object]:
        available = {
            "sqlite": {"command": "sqlite"},
            "playwright": {"command": "npx"},
            "figma-console-mcp": {"command": "npx"},
            fixer_wire.COMPUTER_USE_MCP_NAME: {"command": "computer-use"},
            fixer_wire.FORCED_MCP_SERVER: {"command": "fixer_mcp"},
        }
        return available, {"sqlite": "SQLITE_MCP_CONFIG"}, _FakeAdapter(), (lambda cwd: cwd / "sqliteMCP.toml")

    def _load_droid_available_servers(self) -> tuple[dict[str, dict[str, object]], dict[str, str], DroidBackendAdapter, object]:
        available = {
            fixer_wire.FORCED_MCP_SERVER: {"command": "fixer_mcp", "args": []},
        }
        return available, {}, DroidBackendAdapter(), (lambda _cwd: None)

    def _load_antigravity_available_servers(self) -> tuple[dict[str, dict[str, object]], dict[str, str], AntigravityBackendAdapter, object]:
        available = {
            fixer_wire.FORCED_MCP_SERVER: {"command": "fixer_mcp", "args": []},
        }
        return available, {}, AntigravityBackendAdapter(), (lambda _cwd: None)

    def _load_junie_available_servers(self) -> tuple[dict[str, dict[str, object]], dict[str, str], JunieBackendAdapter, object]:
        available = {
            fixer_wire.FORCED_MCP_SERVER: {"command": "fixer_mcp", "args": []},
        }
        return available, {}, JunieBackendAdapter(), (lambda _cwd: None)

    def test_launch_netrunner_dry_run_keeps_apps_disabled_without_prompt(self) -> None:
        tmp, db_path = self._make_db()
        try:
            with (
                patch.dict(sys.modules, {"client_wires.codex_compat.llm": _fake_codex_main_module(), "client_wires.codex_compat.runtime": _fake_codex_main_module()}),
                patch.object(fixer_wire, "_resolve_fixer_db_path", return_value=db_path),
                patch.object(fixer_wire, "_resolve_project_id", return_value=1),
                patch.object(
                    fixer_wire,
                    "_load_session_rows",
                    return_value=[
                        fixer_wire.SessionRow(
                            session_id=36,
                            global_session_id=139,
                            task_description="Dry run task",
                            status="pending",
                        )
                    ],
                ),
                patch.object(fixer_wire, "_load_available_servers", side_effect=lambda *_a, **_k: self._load_available_servers()),
                patch("builtins.print") as mock_print,
            ):
                code = fixer_wire._launch_netrunner(
                    [],
                    preset_session_id=36,
                    preset_backend="codex",
                    preset_model="gpt-5.4",
                    preset_reasoning="medium",
                    preset_mcp_names=["computer-use", "playwright", "figma-console-mcp"],
                    dry_run=True,
                    Option=_DummyOption,
                    single_select_items=lambda *_a, **_k: (_ for _ in ()).throw(AssertionError("unexpected prompt")),
                    multi_select_items=lambda *_a, **_k: ["sqlite"],
                )
        finally:
            tmp.cleanup()

        self.assertEqual(code, 0)
        command_calls = [call for call in mock_print.call_args_list if call.args and call.args[0] == "[fixer-wire] command:"]
        self.assertEqual(len(command_calls), 1)
        cmd = command_calls[0].args[1]
        self.assertIn("--disable", cmd)
        self.assertIn("apps", cmd)

    def test_launch_netrunner_prompt_still_uses_patchable_facade_how_to_map(self) -> None:
        tmp, db_path = self._make_db()
        try:
            with (
                patch.dict(sys.modules, {"client_wires.codex_compat.llm": _fake_codex_main_module(), "client_wires.codex_compat.runtime": _fake_codex_main_module()}),
                patch.object(fixer_wire, "_resolve_fixer_db_path", return_value=db_path),
                patch.object(fixer_wire, "_resolve_project_id", return_value=1),
                patch.object(
                    fixer_wire,
                    "_load_session_rows",
                    return_value=[
                        fixer_wire.SessionRow(
                            session_id=36,
                            global_session_id=139,
                            task_description="Prompt patch task",
                            status="pending",
                        )
                    ],
                ),
                patch.object(fixer_wire, "_load_available_servers", side_effect=lambda *_a, **_k: self._load_available_servers()),
                patch.object(
                    fixer_wire,
                    "_build_mcp_how_to_map",
                    return_value={"playwright": "Use patched launcher guidance."},
                ) as how_to_map,
                patch("builtins.print") as mock_print,
            ):
                code = fixer_wire._launch_netrunner(
                    [],
                    preset_session_id=36,
                    preset_backend="codex",
                    preset_model="gpt-5.4",
                    preset_reasoning="medium",
                    preset_mcp_names=["playwright"],
                    dry_run=True,
                    Option=_DummyOption,
                    single_select_items=lambda *_a, **_k: (_ for _ in ()).throw(AssertionError("unexpected prompt")),
                    multi_select_items=lambda *_a, **_k: ["sqlite"],
                )
        finally:
            tmp.cleanup()

        self.assertEqual(code, 0)
        how_to_map.assert_called_once()
        command_calls = [call for call in mock_print.call_args_list if call.args and call.args[0] == "[fixer-wire] command:"]
        cmd = command_calls[0].args[1]
        prompt = cmd[cmd.index("--prompt") + 1]
        self.assertIn("- playwright: Use patched launcher guidance.", prompt)

    def test_launch_netrunner_configures_playwright_runtime_interactively(self) -> None:
        tmp, db_path = self._make_db()
        try:
            fake_codex_main = _fake_codex_main_module()
            runtime_calls: list[dict[str, object]] = []

            def _maybe_configure_playwright_runtime(
                selected_servers: dict[str, dict[str, object]],
                available_servers: dict[str, dict[str, object]],
                *,
                interactive: bool = True,
            ) -> str:
                selected_servers["playwright"]["args"] = [
                    "-y",
                    "@playwright/mcp@latest",
                    "--browser",
                    "chrome",
                ]
                available_servers["playwright"] = dict(selected_servers["playwright"])
                runtime_calls.append(
                    {
                        "interactive": interactive,
                        "selected": sorted(selected_servers.keys()),
                        "args": list(selected_servers["playwright"]["args"]),
                    }
                )
                return "chrome"

            fake_codex_main._maybe_configure_playwright_runtime = _maybe_configure_playwright_runtime

            with (
                patch.dict(sys.modules, {"client_wires.codex_compat.llm": fake_codex_main, "client_wires.codex_compat.runtime": fake_codex_main}),
                patch.object(fixer_wire, "_resolve_fixer_db_path", return_value=db_path),
                patch.object(fixer_wire, "_resolve_project_id", return_value=1),
                patch.object(
                    fixer_wire,
                    "_load_session_rows",
                    return_value=[
                        fixer_wire.SessionRow(
                            session_id=36,
                            global_session_id=139,
                            task_description="Playwright runtime task",
                            status="pending",
                        )
                    ],
                ),
                patch.object(fixer_wire, "_load_available_servers", side_effect=lambda *_a, **_k: self._load_available_servers()),
                patch.object(fixer_wire, "_latest_matching_netrunner_codex_session_id", return_value=None),
                patch.object(fixer_wire, "_latest_codex_session_id_for_cwd", side_effect=["before-session", "after-session"]),
                patch("client_wires.fixer_wire.subprocess.call", return_value=0),
            ):
                code = fixer_wire._launch_netrunner(
                    [],
                    preset_session_id=36,
                    preset_backend="codex",
                    preset_model="gpt-5.4",
                    preset_reasoning="medium",
                    preset_mcp_names=["playwright", "figma-console-mcp"],
                    dry_run=False,
                    Option=_DummyOption,
                    single_select_items=lambda *_a, **_k: "unused",
                    multi_select_items=lambda *_a, **_k: ["playwright", "figma-console-mcp"],
                )
        finally:
            tmp.cleanup()

        self.assertEqual(code, 0)
        self.assertEqual(len(runtime_calls), 1)
        self.assertIs(runtime_calls[0]["interactive"], True)
        self.assertIn("playwright", runtime_calls[0]["selected"])
        self.assertEqual(
            runtime_calls[0]["args"],
            ["-y", "@playwright/mcp@latest", "--browser", "chrome"],
        )

    def test_launch_netrunner_always_disables_computer_use_and_strips_overrides(self) -> None:
        tmp, db_path = self._make_db()
        try:
            selections = iter(["codex"])

            def choose(_options: list[_DummyOption], **_kwargs: object) -> str:
                return next(selections)

            with (
                patch.dict(sys.modules, {"client_wires.codex_compat.llm": _fake_codex_main_module(), "client_wires.codex_compat.runtime": _fake_codex_main_module()}),
                patch.object(fixer_wire, "_resolve_fixer_db_path", return_value=db_path),
                patch.object(fixer_wire, "_resolve_project_id", return_value=1),
                patch.object(
                    fixer_wire,
                    "_load_session_rows",
                    return_value=[
                        fixer_wire.SessionRow(
                            session_id=36,
                            global_session_id=139,
                            task_description="Force apps disabled",
                            status="pending",
                        )
                    ],
                ),
                patch.object(fixer_wire, "_load_available_servers", side_effect=lambda *_a, **_k: self._load_available_servers()),
                patch.object(fixer_wire, "_latest_matching_netrunner_codex_session_id", return_value=None),
                patch.object(fixer_wire, "_latest_codex_session_id_for_cwd", side_effect=["before-session", "after-session"]),
                patch("client_wires.fixer_wire.subprocess.call", return_value=0) as mock_call,
            ):
                code = fixer_wire._launch_netrunner(
                    [
                        "-c",
                        "mcp_servers.computer-use.enabled=false",
                        "-c",
                        "mcp_servers.computer-use.enabled=true",
                        "--config=mcp_servers.computer_use.disabled=true",
                        "--enable",
                        "computer_use",
                        "--kept-flag",
                    ],
                    preset_session_id=36,
                    preset_backend=None,
                    preset_model="gpt-5.4",
                    preset_reasoning="medium",
                    preset_mcp_names=["playwright", "figma-console-mcp"],
                    dry_run=False,
                    Option=_DummyOption,
                    single_select_items=choose,
                    multi_select_items=lambda *_a, **_k: ["playwright", "figma-console-mcp"],
                )
        finally:
            tmp.cleanup()

        self.assertEqual(code, 0)
        cmd = mock_call.call_args.args[0]
        self.assertIn("--disable", cmd)
        self.assertIn("apps", cmd)
        self.assertNotIn("--enable", cmd)
        self.assertNotIn("computer_use", cmd)
        self.assertIn("--kept-flag", cmd)
        self.assertIn("--mcp=figma-console-mcp,fixer_mcp,playwright", cmd)
        self.assertFalse(any("codex_apps" in arg for arg in cmd))
        self.assertFalse(any("computer-use.enabled=false" in arg for arg in cmd))
        self.assertFalse(any("computer-use.enabled=true" in arg for arg in cmd))
        self.assertFalse(any("computer_use.disabled=true" in arg for arg in cmd))

    def test_launch_netrunner_keeps_apps_disabled_without_computer_use_prompt(self) -> None:
        tmp, db_path = self._make_db()
        try:
            selections = iter(["codex"])

            def choose(_options: list[_DummyOption], **_kwargs: object) -> str:
                return next(selections)

            with (
                patch.dict(sys.modules, {"client_wires.codex_compat.llm": _fake_codex_main_module(), "client_wires.codex_compat.runtime": _fake_codex_main_module()}),
                patch.object(fixer_wire, "_resolve_fixer_db_path", return_value=db_path),
                patch.object(fixer_wire, "_resolve_project_id", return_value=1),
                patch.object(
                    fixer_wire,
                    "_load_session_rows",
                    return_value=[
                        fixer_wire.SessionRow(
                            session_id=36,
                            global_session_id=139,
                            task_description="Keep apps disabled",
                            status="pending",
                        )
                    ],
                ),
                patch.object(fixer_wire, "_load_available_servers", side_effect=lambda *_a, **_k: self._load_available_servers()),
                patch.object(fixer_wire, "_latest_matching_netrunner_codex_session_id", return_value=None),
                patch.object(fixer_wire, "_latest_codex_session_id_for_cwd", side_effect=["before-session", "after-session"]),
                patch("client_wires.fixer_wire.subprocess.call", return_value=0) as mock_call,
            ):
                code = fixer_wire._launch_netrunner(
                    [],
                    preset_session_id=36,
                    preset_backend=None,
                    preset_model="gpt-5.4",
                    preset_reasoning="medium",
                    preset_mcp_names=["playwright", "figma-console-mcp"],
                    dry_run=False,
                    Option=_DummyOption,
                    single_select_items=choose,
                    multi_select_items=lambda *_a, **_k: ["sqlite"],
                )
        finally:
            tmp.cleanup()

        self.assertEqual(code, 0)
        cmd = mock_call.call_args.args[0]
        self.assertIn("--disable", cmd)
        self.assertIn("apps", cmd)
        self.assertNotIn("computer-use", "".join(cmd))

    def test_launch_netrunner_explicit_session_can_choose_acceptance_mode(self) -> None:
        tmp, db_path = self._make_db()
        try:
            with (
                patch.dict(sys.modules, {"client_wires.codex_compat.llm": _fake_codex_main_module(), "client_wires.codex_compat.runtime": _fake_codex_main_module()}),
                patch.object(fixer_wire, "_resolve_fixer_db_path", return_value=db_path),
                patch.object(fixer_wire, "_resolve_project_id", return_value=1),
                patch.object(
                    fixer_wire,
                    "_load_session_rows",
                    return_value=[
                        fixer_wire.SessionRow(
                            session_id=36,
                            global_session_id=139,
                            task_description="Manual acceptance",
                            status="pending",
                        )
                    ],
                ),
                patch.object(fixer_wire, "_load_available_servers", side_effect=lambda *_a, **_k: self._load_available_servers()),
                patch.object(fixer_wire, "_latest_matching_netrunner_codex_session_id", return_value=None),
                patch.object(fixer_wire, "_latest_codex_session_id_for_cwd", side_effect=["before-session", "after-session"]),
                patch("client_wires.fixer_wire.subprocess.call", return_value=0) as mock_call,
            ):
                code = fixer_wire._launch_netrunner(
                    [],
                    preset_session_id=36,
                    preset_backend="codex",
                    preset_model="gpt-5.4",
                    preset_reasoning="medium",
                    preset_mcp_names=["playwright"],
                    dry_run=False,
                    Option=_DummyOption,
                    single_select_items=lambda *_a, **_k: (_ for _ in ()).throw(
                        AssertionError("unexpected compatibility-session picker")
                    ),
                    multi_select_items=lambda *_a, **_k: ["playwright"],
                    netrunner_kind=fixer_wire.NETRUNNER_KIND_ACCEPTANCE,
                )
        finally:
            tmp.cleanup()

        self.assertEqual(code, 0)
        cmd = mock_call.call_args.args[0]
        self.assertTrue(any("Activate skill $hands-netrunner immediately." in item for item in cmd))

    def test_launch_netrunner_resumes_non_pending_session_by_default(self) -> None:
        tmp, db_path = self._make_db()
        try:
            with (
                patch.dict(sys.modules, {"client_wires.codex_compat.llm": _fake_codex_main_module(), "client_wires.codex_compat.runtime": _fake_codex_main_module()}),
                patch.object(fixer_wire, "_resolve_fixer_db_path", return_value=db_path),
                patch.object(fixer_wire, "_resolve_project_id", return_value=1),
                patch.object(
                    fixer_wire,
                    "_load_session_rows",
                    return_value=[
                        fixer_wire.SessionRow(
                            session_id=36,
                            global_session_id=139,
                            task_description="Resume me",
                            status="in_progress",
                            codex_session_id="resume-139",
                        )
                    ],
                ),
                patch.object(fixer_wire, "_load_available_servers", side_effect=lambda *_a, **_k: self._load_available_servers()),
                patch.object(fixer_wire, "_load_netrunner_resume_summaries", return_value=[_make_history_summary("resume-139", preview="Existing netrunner")]),
                patch("client_wires.fixer_wire.subprocess.call", return_value=0) as mock_call,
            ):
                code = fixer_wire._launch_netrunner(
                    [],
                    preset_session_id=36,
                    preset_backend="codex",
                    preset_model="gpt-5.4",
                    preset_reasoning="medium",
                    preset_mcp_names=["playwright", "figma-console-mcp"],
                    dry_run=False,
                    Option=_DummyOption,
                    single_select_items=lambda *_a, **_k: "unused",
                    multi_select_items=lambda *_a, **_k: ["playwright", "figma-console-mcp"],
                )
        finally:
            tmp.cleanup()

        self.assertEqual(code, 0)
        cmd = mock_call.call_args.args[0]
        self.assertEqual(cmd[0], "codex")
        self.assertEqual(cmd[1], "resume")
        self.assertEqual(cmd[-1], "resume-139")
        self.assertNotIn("--prompt", cmd)
        self.assertIn("--disable", cmd)
        self.assertIn("apps", cmd)

    def test_launch_netrunner_resume_always_disables_computer_use_and_strips_overrides(self) -> None:
        tmp, db_path = self._make_db()
        try:
            with (
                patch.dict(sys.modules, {"client_wires.codex_compat.llm": _fake_codex_main_module(), "client_wires.codex_compat.runtime": _fake_codex_main_module()}),
                patch.object(fixer_wire, "_resolve_fixer_db_path", return_value=db_path),
                patch.object(fixer_wire, "_resolve_project_id", return_value=1),
                patch.object(
                    fixer_wire,
                    "_load_session_rows",
                    return_value=[
                        fixer_wire.SessionRow(
                            session_id=36,
                            global_session_id=139,
                            task_description="Resume with apps disabled",
                            status="in_progress",
                            codex_session_id="resume-139",
                        )
                    ],
                ),
                patch.object(fixer_wire, "_load_available_servers", side_effect=lambda *_a, **_k: self._load_available_servers()),
                patch.object(fixer_wire, "_load_netrunner_resume_summaries", return_value=[_make_history_summary("resume-139", preview="Existing netrunner")]),
                patch("client_wires.fixer_wire.subprocess.call", return_value=0) as mock_call,
            ):
                code = fixer_wire._launch_netrunner(
                    [
                        "-c",
                        "mcp_servers.computer-use.enabled=true",
                        "--config=mcp_servers.computer_use.disabled=false",
                        "--enable=computer_use",
                        "--kept-flag",
                    ],
                    preset_session_id=36,
                    preset_backend="codex",
                    preset_model="gpt-5.4",
                    preset_reasoning="medium",
                    preset_mcp_names=["playwright", "figma-console-mcp"],
                    dry_run=False,
                    Option=_DummyOption,
                    single_select_items=lambda *_a, **_k: "unused",
                    multi_select_items=lambda *_a, **_k: ["playwright", "figma-console-mcp"],
                )
        finally:
            tmp.cleanup()

        self.assertEqual(code, 0)
        cmd = mock_call.call_args.args[0]
        self.assertEqual(cmd[0], "codex")
        self.assertEqual(cmd[1], "resume")
        self.assertEqual(cmd[-1], "resume-139")
        self.assertIn("--disable", cmd)
        self.assertIn("apps", cmd)
        self.assertNotIn("--enable", cmd)
        self.assertNotIn("computer_use", cmd)
        self.assertIn("--kept-flag", cmd)
        self.assertIn("--mcp=figma-console-mcp,fixer_mcp,playwright", cmd)
        self.assertFalse(any("codex_apps" in arg for arg in cmd))
        self.assertFalse(any("computer-use.enabled=true" in arg for arg in cmd))
        self.assertFalse(any("computer_use.disabled=false" in arg for arg in cmd))

    def test_launch_netrunner_keeps_assigned_react_native_guide_available(self) -> None:
        tmp, db_path = self._make_db()
        try:
            with (
                patch.dict(sys.modules, {"client_wires.codex_compat.llm": _fake_codex_main_module(), "client_wires.codex_compat.runtime": _fake_codex_main_module()}),
                patch.object(fixer_wire, "_resolve_fixer_db_path", return_value=db_path),
                patch.object(fixer_wire, "_resolve_project_id", return_value=1),
                patch.object(
                    fixer_wire,
                    "_load_session_rows",
                    return_value=[
                        fixer_wire.SessionRow(
                            session_id=36,
                            global_session_id=139,
                            task_description="Assigned RN guide",
                            status="pending",
                            codex_session_id="",
                        )
                    ],
                ),
                patch.object(
                    fixer_wire,
                    "_load_available_servers",
                    return_value=(
                        {
                            "sqlite": {"command": "sqlite"},
                            "react-native-guide": {"command": "npx"},
                            fixer_wire.FORCED_MCP_SERVER: {"command": "fixer_mcp"},
                        },
                        {"sqlite": "SQLITE_MCP_CONFIG"},
                        _FakeAdapter(),
                        (lambda cwd: cwd / "sqliteMCP.toml"),
                    ),
                ),
                patch.object(fixer_wire, "_load_assigned_mcp_names", return_value=["react-native-guide"]),
                patch.object(fixer_wire, "_load_project_allowed_mcp_names", return_value=["sqlite", "react-native-guide"]),
                patch.object(fixer_wire, "_sync_registry_names", return_value=None),
                patch.object(fixer_wire, "_load_registry_mcp_metadata", return_value={}),
                patch.object(fixer_wire, "_persist_session_mcp_names", return_value=None),
                patch.object(fixer_wire, "_persist_session_launch_selection", side_effect=lambda _conn, selected_session, launch_selection: launch_selection),
                patch.object(fixer_wire, "_load_session_external_id", return_value=""),
                patch.object(fixer_wire, "_latest_matching_netrunner_codex_session_id", return_value=None),
                patch.object(fixer_wire, "_latest_codex_session_id_for_cwd", side_effect=["before-session", "after-session"]),
                patch("client_wires.fixer_wire.subprocess.call", return_value=0) as mock_call,
            ):
                code = fixer_wire._launch_netrunner(
                    [],
                    preset_session_id=36,
                    preset_backend="codex",
                    preset_model="gpt-5.4",
                    preset_reasoning="medium",
                    preset_mcp_names=[],
                    dry_run=False,
                    Option=_DummyOption,
                    single_select_items=lambda *_a, **_k: "unused",
                    multi_select_items=lambda *_a, **_k: ["react-native-guide"],
                )
        finally:
            tmp.cleanup()

        self.assertEqual(code, 0)
        cmd = mock_call.call_args.args[0]
        self.assertIn("--mcp=fixer_mcp,react-native-guide", cmd)

    def test_launch_netrunner_uses_durable_mcp_assignment_without_interactive_picker(self) -> None:
        tmp, db_path = self._make_db()
        try:
            tracked_connections: list[_TrackingConnection] = []
            real_connect = sqlite3.connect

            def tracked_connect(*args: object, **kwargs: object) -> _TrackingConnection:
                conn = _TrackingConnection(real_connect(*args, **kwargs))
                tracked_connections.append(conn)
                return conn

            def choose_session(options: list[_DummyOption], **_kwargs: object) -> object:
                self.assertTrue(tracked_connections)
                self.assertTrue(all(conn.closed for conn in tracked_connections))
                if any(option.value == "unused" for option in options):
                    return "unused"
                if any(option.value == fixer_wire.NETRUNNER_KIND_MANUAL for option in options):
                    return fixer_wire.NETRUNNER_KIND_MANUAL
                return 36

            def choose_mcp(_options: list[_DummyOption], **_kwargs: object) -> list[str]:
                raise AssertionError("compatibility envelopes must not open the MCP picker")

            def fake_call(_cmd: list[str], **_kwargs: object) -> int:
                self.assertTrue(tracked_connections)
                self.assertTrue(all(conn.closed for conn in tracked_connections))
                return 0

            with (
                patch.dict(sys.modules, {"client_wires.codex_compat.llm": _fake_codex_main_module(), "client_wires.codex_compat.runtime": _fake_codex_main_module()}),
                patch("client_wires.fixer_wire.sqlite3.connect", side_effect=tracked_connect),
                patch.object(fixer_wire, "_resolve_fixer_db_path", return_value=db_path),
                patch.object(fixer_wire, "_resolve_project_id", return_value=1),
                patch.object(
                    fixer_wire,
                    "_load_session_rows",
                    return_value=[
                        fixer_wire.SessionRow(
                            session_id=36,
                            global_session_id=139,
                            task_description="Fresh task",
                            status="pending",
                            codex_session_id="",
                        )
                    ],
                ),
                patch.object(fixer_wire, "_load_available_servers", side_effect=lambda *_a, **_k: self._load_available_servers()),
                patch.object(fixer_wire, "_latest_matching_netrunner_codex_session_id", return_value=None),
                patch.object(fixer_wire, "_latest_codex_session_id_for_cwd", side_effect=["before-session", "after-session"]),
                patch("client_wires.fixer_wire.subprocess.call", side_effect=fake_call),
            ):
                code = fixer_wire._launch_netrunner(
                    [],
                    preset_session_id=36,
                    preset_backend="codex",
                    preset_model="gpt-5.4",
                    preset_reasoning="medium",
                    preset_mcp_names=[],
                    dry_run=False,
                    Option=_DummyOption,
                    single_select_items=choose_session,
                    multi_select_items=choose_mcp,
                )

            conn = sqlite3.connect(db_path)
            try:
                assigned_rows = conn.execute(
                    """
                    SELECT s.name
                    FROM session_mcp_server sms
                    INNER JOIN mcp_server s ON s.id = sms.mcp_server_id
                    WHERE sms.session_id = 139
                    ORDER BY s.name
                    """
                ).fetchall()
                codex_link = conn.execute(
                    """
                    SELECT codex_session_id
                    FROM session_codex_link
                    WHERE session_id = 139
                    """
                ).fetchall()
            finally:
                conn.close()
        finally:
            tmp.cleanup()

        self.assertEqual(code, 0)
        self.assertTrue(all(conn.closed for conn in tracked_connections))
        self.assertEqual(assigned_rows, [("fixer_mcp",)])
        self.assertEqual(codex_link, [("after-session",)])

    def test_launch_netrunner_closes_db_before_resume_selection(self) -> None:
        tmp, db_path = self._make_db()
        try:
            tracked_connections: list[_TrackingConnection] = []
            real_connect = sqlite3.connect

            def tracked_connect(*args: object, **kwargs: object) -> _TrackingConnection:
                conn = _TrackingConnection(real_connect(*args, **kwargs))
                tracked_connections.append(conn)
                return conn

            def resolve_resume(
                _cwd: Path,
                _selected_session: fixer_wire.SessionRow,
                _Option: object,
                _single_select_items: object,
            ) -> str:
                self.assertTrue(tracked_connections)
                self.assertTrue(all(conn.closed for conn in tracked_connections))
                return "resume-139"

            def fake_call(_cmd: list[str], **_kwargs: object) -> int:
                self.assertTrue(tracked_connections)
                self.assertTrue(all(conn.closed for conn in tracked_connections))
                return 0

            with (
                patch.dict(sys.modules, {"client_wires.codex_compat.llm": _fake_codex_main_module(), "client_wires.codex_compat.runtime": _fake_codex_main_module()}),
                patch("client_wires.fixer_wire.sqlite3.connect", side_effect=tracked_connect),
                patch.object(fixer_wire, "_resolve_fixer_db_path", return_value=db_path),
                patch.object(fixer_wire, "_resolve_project_id", return_value=1),
                patch.object(
                    fixer_wire,
                    "_load_session_rows",
                    return_value=[
                        fixer_wire.SessionRow(
                            session_id=36,
                            global_session_id=139,
                            task_description="Resume me",
                            status="in_progress",
                            codex_session_id="resume-139",
                        )
                    ],
                ),
                patch.object(fixer_wire, "_load_available_servers", side_effect=lambda *_a, **_k: self._load_available_servers()),
                patch.object(fixer_wire, "_resolve_netrunner_resume_session_id", side_effect=resolve_resume),
                patch.object(fixer_wire, "_latest_codex_session_id_for_cwd", return_value="before-session"),
                patch("client_wires.fixer_wire.subprocess.call", side_effect=fake_call) as mock_call,
            ):
                code = fixer_wire._launch_netrunner(
                    [],
                    preset_session_id=36,
                    preset_backend="codex",
                    preset_model="gpt-5.4",
                    preset_reasoning="medium",
                    preset_mcp_names=["sqlite"],
                    dry_run=False,
                    Option=_DummyOption,
                    single_select_items=lambda *_a, **_k: "unused",
                    multi_select_items=lambda *_a, **_k: ["sqlite"],
                )

            conn = sqlite3.connect(db_path)
            try:
                codex_link = conn.execute(
                    """
                    SELECT codex_session_id
                    FROM session_codex_link
                    WHERE session_id = 139
                    """
                ).fetchall()
            finally:
                conn.close()
        finally:
            tmp.cleanup()

        self.assertEqual(code, 0)
        self.assertTrue(all(conn.closed for conn in tracked_connections))
        self.assertEqual(codex_link, [("resume-139",)])
        self.assertEqual(mock_call.call_args.args[0][1], "resume")

    def test_launch_netrunner_uses_deterministic_history_fallback_when_stored_id_is_missing(self) -> None:
        tmp, db_path = self._make_db()
        try:
            choose_calls: dict[str, object] = {}

            def choose_resume(options: list[_DummyOption], **kwargs: object) -> str:
                if any(option.value == "unused" for option in options):
                    return "unused"
                choose_calls["options"] = options
                choose_calls["kwargs"] = kwargs
                return "resume-fallback"

            with (
                patch.dict(sys.modules, {"client_wires.codex_compat.llm": _fake_codex_main_module(), "client_wires.codex_compat.runtime": _fake_codex_main_module()}),
                patch.object(fixer_wire, "_resolve_fixer_db_path", return_value=db_path),
                patch.object(fixer_wire, "_resolve_project_id", return_value=1),
                patch.object(
                    fixer_wire,
                    "_load_session_rows",
                    return_value=[
                        fixer_wire.SessionRow(
                            session_id=36,
                            global_session_id=139,
                            task_description="Resume me",
                            status="completed",
                            codex_session_id="stored-but-missing",
                        )
                    ],
                ),
                patch.object(fixer_wire, "_load_available_servers", side_effect=lambda *_a, **_k: self._load_available_servers()),
                patch.object(
                    fixer_wire,
                    "_load_netrunner_resume_summaries",
                    return_value=[
                        _make_history_summary("resume-fallback", preview="Newest matching thread"),
                        _make_history_summary("resume-older", preview="Older matching thread"),
                    ],
                ),
                patch("client_wires.fixer_wire.subprocess.call", return_value=0) as mock_call,
            ):
                code = fixer_wire._launch_netrunner(
                    [],
                    preset_session_id=36,
                    preset_backend="codex",
                    preset_model="gpt-5.4",
                    preset_reasoning="medium",
                    preset_mcp_names=["sqlite"],
                    dry_run=False,
                    Option=_DummyOption,
                    single_select_items=choose_resume,
                    multi_select_items=lambda *_a, **_k: ["sqlite"],
                )
        finally:
            tmp.cleanup()

        self.assertEqual(code, 0)
        cmd = mock_call.call_args.args[0]
        self.assertEqual(cmd[1], "resume")
        self.assertEqual(cmd[-1], "resume-fallback")
        labels = [option.label for option in choose_calls["options"] if not option.is_header]
        self.assertTrue(any("Newest matching thread" in label for label in labels))

    def test_launch_netrunner_persists_droid_backend_and_external_link(self) -> None:
        tmp, db_path = self._make_db()
        try:
            with (
                patch.dict(sys.modules, {"client_wires.codex_compat.llm": _fake_codex_main_module(), "client_wires.codex_compat.runtime": _fake_codex_main_module()}),
                patch.object(fixer_wire, "_resolve_fixer_db_path", return_value=db_path),
                patch.object(fixer_wire, "_resolve_project_id", return_value=1),
                patch.object(
                    fixer_wire,
                    "_load_session_rows",
                    return_value=[
                        fixer_wire.SessionRow(
                            session_id=36,
                            global_session_id=139,
                            task_description="Launch with droid",
                            status="pending",
                        )
                    ],
                ),
                patch.object(fixer_wire, "_load_available_servers", side_effect=lambda *_a, **_k: self._load_droid_available_servers()),
                patch.object(fixer_wire, "_prompt_resume_session_id", return_value="droid-session-139"),
                patch("client_wires.fixer_wire.subprocess.call", return_value=0) as mock_call,
            ):
                code = fixer_wire._launch_netrunner(
                    [],
                    preset_session_id=36,
                    preset_backend="droid",
                    preset_model="glm-5.1",
                    preset_reasoning="medium",
                    preset_mcp_names=[],
                    dry_run=False,
                    Option=_DummyOption,
                    single_select_items=lambda *_a, **_k: None,
                    multi_select_items=lambda *_a, **_k: [],
                )

            conn = sqlite3.connect(db_path)
            try:
                session_row = conn.execute(
                    """
                    SELECT cli_backend, cli_model, cli_reasoning
                    FROM session
                    WHERE id = 139
                    """
                ).fetchone()
                external_link = conn.execute(
                    """
                    SELECT backend, external_session_id
                    FROM session_external_link
                    WHERE session_id = 139
                    """
                ).fetchall()
            finally:
                conn.close()
        finally:
            tmp.cleanup()

        self.assertEqual(code, 0)
        cmd = mock_call.call_args.args[0]
        call_kwargs = mock_call.call_args.kwargs
        self.assertEqual(cmd[0], "droid")
        self.assertNotIn("exec", cmd)
        self.assertNotIn("--skip-permissions-unsafe", cmd)
        self.assertTrue(any("Activate skill $hands-netrunner immediately." in item for item in cmd))
        self.assertFalse(any("Droid MCP tool guidance:" in item for item in cmd))
        self.assertFalse(any("Attached MCP how-to guidance:" in item for item in cmd))
        self.assertFalse(any("Standard web stack guidance:" in item for item in cmd))
        self.assertTrue(any("Run the initialization checklist for compatibility session" in item for item in cmd))
        self.assertFalse(any("mcp_fixer_mcp_checkout_task" in item for item in cmd))
        self.assertEqual(call_kwargs["cwd"], str(Path.cwd()))
        self.assertEqual(session_row, ("droid", "glm-5.1", "medium"))
        self.assertEqual(external_link, [("droid", "droid-session-139")])

    def test_launch_netrunner_persists_antigravity_backend_and_external_link(self) -> None:
        tmp, db_path = self._make_db()
        try:
            with (
                patch.dict(sys.modules, {"client_wires.codex_compat.llm": _fake_codex_main_module(), "client_wires.codex_compat.runtime": _fake_codex_main_module()}),
                patch.object(fixer_wire, "_resolve_fixer_db_path", return_value=db_path),
                patch.object(fixer_wire, "_resolve_project_id", return_value=1),
                patch.object(
                    fixer_wire,
                    "_load_session_rows",
                    return_value=[
                        fixer_wire.SessionRow(
                            session_id=36,
                            global_session_id=139,
                            task_description="Launch with antigravity",
                            status="pending",
                        )
                    ],
                ),
                patch.object(
                    fixer_wire,
                    "_load_available_servers",
                    side_effect=lambda *_a, **_k: self._load_antigravity_available_servers(),
                ),
                patch.object(AntigravityBackendAdapter, "ensure_runtime_files", return_value=None),
                patch.object(fixer_wire, "_prompt_resume_session_id", return_value="agy-conversation-139"),
                patch("client_wires.fixer_wire.subprocess.call", return_value=0) as mock_call,
            ):
                code = fixer_wire._launch_netrunner(
                    [],
                    preset_session_id=36,
                    preset_backend="agy",
                    preset_model="Gemini 3.6 Flash (High)",
                    preset_reasoning="default",
                    preset_mcp_names=[],
                    dry_run=False,
                    Option=_DummyOption,
                    single_select_items=lambda *_a, **_k: None,
                    multi_select_items=lambda *_a, **_k: [],
                )

            conn = sqlite3.connect(db_path)
            try:
                session_row = conn.execute(
                    """
                    SELECT cli_backend, cli_model, cli_reasoning
                    FROM session
                    WHERE id = 139
                    """
                ).fetchone()
                external_link = conn.execute(
                    """
                    SELECT backend, external_session_id
                    FROM session_external_link
                    WHERE session_id = 139
                    """
                ).fetchall()
            finally:
                conn.close()
        finally:
            tmp.cleanup()

        self.assertEqual(code, 0)
        cmd = mock_call.call_args.args[0]
        self.assertEqual(session_row, ("antigravity", "Gemini 3.6 Flash", "high"))
        self.assertEqual(external_link, [("antigravity", "agy-conversation-139")])
        self.assertEqual(cmd[0], "agy")
        self.assertIn("--model", cmd)
        self.assertEqual(cmd[cmd.index("--model") + 1], "Gemini 3.6 Flash (High)")
        self.assertNotIn("-p", cmd)
        self.assertNotIn("--print", cmd)
        self.assertIn("--prompt-interactive", cmd)
        prompt = cmd[cmd.index("--prompt-interactive") + 1]
        self.assertTrue(prompt.startswith("/hands-netrunner\n"))
        self.assertNotIn("Activate skill $hands-netrunner immediately.", prompt)
        self.assertIn("fixer_mcp.log_netrunner_progress", prompt)

    def test_launch_netrunner_persists_junie_backend_and_external_link(self) -> None:
        tmp, db_path = self._make_db()
        try:
            with (
                patch.dict(sys.modules, {"client_wires.codex_compat.llm": _fake_codex_main_module(), "client_wires.codex_compat.runtime": _fake_codex_main_module()}),
                patch.object(fixer_wire, "_resolve_fixer_db_path", return_value=db_path),
                patch.object(fixer_wire, "_resolve_project_id", return_value=1),
                patch.object(
                    fixer_wire,
                    "_load_session_rows",
                    return_value=[
                        fixer_wire.SessionRow(
                            session_id=36,
                            global_session_id=139,
                            task_description="Launch with junie",
                            status="pending",
                        )
                    ],
                ),
                patch.object(
                    fixer_wire,
                    "_load_available_servers",
                    side_effect=lambda *_a, **_k: self._load_junie_available_servers(),
                ),
                patch.object(JunieBackendAdapter, "ensure_runtime_files", return_value=None),
                patch.object(fixer_wire, "_prompt_resume_session_id", return_value="junie-session-139"),
                patch("client_wires.fixer_wire.subprocess.call", return_value=0) as mock_call,
            ):
                code = fixer_wire._launch_netrunner(
                    [],
                    preset_session_id=36,
                    preset_backend="junie",
                    preset_model="glm-5.1",
                    preset_reasoning="default",
                    preset_mcp_names=[fixer_wire.FORCED_MCP_SERVER],
                    dry_run=False,
                    Option=_DummyOption,
                    single_select_items=lambda *_a, **_k: None,
                    multi_select_items=lambda *_a, **_k: [],
                )

            conn = sqlite3.connect(db_path)
            try:
                session_row = conn.execute(
                    """
                    SELECT cli_backend, cli_model, cli_reasoning
                    FROM session
                    WHERE id = 139
                    """
                ).fetchone()
                external_link = conn.execute(
                    """
                    SELECT backend, external_session_id
                    FROM session_external_link
                    WHERE session_id = 139
                    """
                ).fetchall()
            finally:
                conn.close()
        finally:
            tmp.cleanup()

        self.assertEqual(code, 0)
        cmd = mock_call.call_args.args[0]
        self.assertEqual(session_row, ("junie", "glm-5.1", "default"))
        self.assertEqual(external_link, [("junie", "junie-session-139")])
        self.assertEqual(cmd[0], "junie")
        self.assertNotIn("--provider", cmd)
        self.assertIn("--model", cmd)
        self.assertEqual(cmd[cmd.index("--model") + 1], "custom:glm-5.1")
        self.assertIn("--model-default-locations", cmd)
        self.assertEqual(cmd[cmd.index("--model-default-locations") + 1], "true")
        self.assertIn("--skill-default-locations", cmd)
        self.assertEqual(cmd[cmd.index("--skill-default-locations") + 1], "false")
        self.assertIn("--mcp-default-locations", cmd)
        self.assertEqual(cmd[cmd.index("--mcp-default-locations") + 1], "false")
        self.assertIn("--mcp-location", cmd)
        self.assertEqual(cmd[cmd.index("--mcp-location") + 1], ".junie/fixer-runtime/mcp")
        self.assertNotIn("--openrouter-api-key", cmd)


class HandsDocsTreePickerTests(unittest.TestCase):
    def _entries(self) -> list[fixer_wire_hands_context.HandsDocEntry]:
        entry = fixer_wire_hands_context.HandsDocEntry
        return [
            entry(doc_id=1, title="Root A", content="", level=0, slug="a", path="a", status="current", parent_id=0),
            entry(doc_id=2, title="Child A1", content="", level=1, slug="a1", path="a/a1", status="current", parent_id=1),
            entry(doc_id=3, title="Grandchild", content="", level=2, slug="g", path="a/a1/g", status="current", parent_id=2),
            entry(doc_id=4, title="Root B", content="", level=0, slug="b", path="b", status="current", parent_id=0),
        ]

    def _run_picker(self, rounds: list[list[object]], preselected: list[int] | None = None):
        seen: list[list[object]] = []
        rendered: list[list[str]] = []
        calls: list[dict[str, object]] = []

        def multi_select_items(options, **kwargs):
            seen.append([getattr(option, "value", None) for option in options])
            rendered.append([str(getattr(option, "label", "")) for option in options])
            calls.append(kwargs)
            return rounds.pop(0) if rounds else []

        result = fixer_wire_selectors._select_hands_docs_interactive(
            self._entries(),
            preselected or [],
            _DummyOption,
            multi_select_items,
        )
        return result, seen, rendered, calls

    def test_initial_round_shows_only_root_docs(self) -> None:
        result, seen, _, _ = self._run_picker([[]])
        self.assertEqual(result, [])
        first_round = seen[0]
        self.assertIn(1, first_round)
        self.assertIn(4, first_round)
        self.assertNotIn(2, first_round)
        self.assertNotIn(3, first_round)
        self.assertNotIn("expand:4", first_round)  # Root B has no children

    def test_expand_branch_reveals_children_for_selection(self) -> None:
        result, seen, _, _ = self._run_picker([["expand:1"], [2]])
        self.assertEqual(result, [2])
        self.assertIn(2, seen[1])
        self.assertNotIn(3, seen[1])  # grandchild still collapsed under child

    def test_branch_toggle_selects_whole_subtree(self) -> None:
        result, _, _, _ = self._run_picker([["branch:1"], [1, 2, 3]])
        self.assertEqual(result, [1, 2, 3])

    def test_branch_toggle_twice_deselects_subtree(self) -> None:
        result, _, _, _ = self._run_picker([["branch:1"], [1, 2, 3, "branch:1"], []])
        self.assertEqual(result, [])

    def test_hidden_docs_keep_preselection(self) -> None:
        result, _, _, _ = self._run_picker([[4]], preselected=[3])
        self.assertEqual(result, [3, 4])

    def test_expand_preserves_exact_doc_checkboxes_and_cursor(self) -> None:
        result, _, rendered, calls = self._run_picker(
            [[1, 4, "expand:1"], [1, 4]],
            preselected=[1, 4],
        )

        self.assertEqual(result, [1, 4])
        self.assertEqual(calls[1]["preselected_values"], [1, 4])
        self.assertEqual(calls[1]["initial_cursor_value"], 1)
        self.assertTrue(any("  0 ▾ Root A" in label for label in rendered[1]))

    def test_parent_count_column_reports_selected_descendants_only(self) -> None:
        _, _, rendered, _ = self._run_picker([[1, 2, "expand:1"], [1, 2]], preselected=[2])

        self.assertTrue(any("  1 ▾ Root A" in label for label in rendered[1]))

    def test_refresh_callback_recomputes_counts_and_order_live(self) -> None:
        seen: list[list[object]] = []
        rendered: list[list[str]] = []

        def multi_select_items(options, **kwargs):
            refresh = kwargs.get("refresh_options")
            if refresh is not None:
                refreshed = refresh([2, 3])  # attach child A1 + grandchild inside picker
                seen.append(
                    [
                        getattr(opt, "value", None)
                        for opt in refreshed
                        if not getattr(opt, "is_header", False)
                    ]
                )
                rendered.append(
                    [
                        str(getattr(opt, "label", ""))
                        for opt in refreshed
                        if not getattr(opt, "is_header", False)
                    ]
                )
            return []

        fixer_wire_selectors._select_hands_docs_interactive(
            self._entries(), [], _DummyOption, multi_select_items
        )

        # Live refresh: counts derive from the selection passed in, and parent
        # roots stay above leaf roots.
        self.assertEqual(seen[0], [1, 4])
        self.assertTrue(any("  2 ▸ Root A" in label for label in rendered[0]))
        self.assertTrue(any("      Root B" in label for label in rendered[0]))


if __name__ == "__main__":
    unittest.main()
