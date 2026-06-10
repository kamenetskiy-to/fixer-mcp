from __future__ import annotations

import json
import os
import sys
import tempfile
import types
import unittest
from pathlib import Path
from unittest.mock import patch

from client_wires import fixer_wire
from client_wires import fixer_wire_mcp
from client_wires.bootstrap import bootstrap_codex_pro_import_path


class FixerWireMcpExtractionTests(unittest.TestCase):
    def test_mcp_helpers_delegate_to_mcp_module_while_facade_symbols_remain(self) -> None:
        selected = {fixer_wire.FORCED_MCP_SERVER: {"env": {}}}
        delegated_result = {fixer_wire.FORCED_MCP_SERVER: {"env": {fixer_wire.FIXER_DB_PATH_ENV: "/tmp/fixer.db"}}}

        with patch.object(
            fixer_wire_mcp,
            "_bind_fixer_db_path_to_server_env",
            return_value=delegated_result,
        ) as delegated:
            result = fixer_wire._bind_fixer_db_path_to_server_env(selected, db_path=Path("/tmp/fixer.db"))

        self.assertIs(result, delegated_result)
        delegated.assert_called_once_with(selected, db_path=Path("/tmp/fixer.db"))
        self.assertEqual(fixer_wire.FORCED_MCP_SERVER, fixer_wire_mcp.FORCED_MCP_SERVER)
        self.assertTrue(hasattr(fixer_wire_mcp, "_inject_figma_console_server"))
        self.assertTrue(hasattr(fixer_wire, "_inject_figma_console_server"))

    def test_load_available_servers_still_uses_patchable_facade_injection_hooks(self) -> None:
        repo_root = Path(__file__).resolve().parents[2]
        main_module = _fake_codex_main_module()
        config_loader = _fake_codex_config_loader_module(
            global_servers={"sqlite": {"command": "sqlite3"}},
        )
        codex_pro_package = types.ModuleType("client_wires.codex_compat")
        codex_pro_package.main = main_module
        codex_pro_package.config_loader = config_loader

        def inject_research(servers: dict[str, dict[str, object]], _cwd: Path) -> dict[str, dict[str, object]]:
            merged = dict(servers)
            merged["patched-research"] = {"command": "research"}
            return merged

        def inject_figma(servers: dict[str, dict[str, object]], _cwd: Path) -> dict[str, dict[str, object]]:
            merged = dict(servers)
            merged["patched-figma"] = {"command": "figma"}
            return merged

        def inject_forced(servers: dict[str, dict[str, object]]) -> dict[str, dict[str, object]]:
            merged = dict(servers)
            merged["patched-forced"] = {"command": "forced"}
            return merged

        with (
            patch.dict(
                sys.modules,
                {
                    "client_wires.codex_compat": codex_pro_package,
                    "client_wires.codex_compat.llm": main_module,
                    "client_wires.codex_compat.config": config_loader,
                },
            ),
            patch.object(fixer_wire, "_inject_research_query_server", side_effect=inject_research) as research,
            patch.object(fixer_wire, "_inject_figma_console_server", side_effect=inject_figma) as figma,
            patch.object(fixer_wire, "_inject_forced_fixer_server", side_effect=inject_forced) as forced,
        ):
            available, _config_env_vars, _adapter, _ensure_sqlite_scaffold = fixer_wire._load_available_servers(repo_root)

        self.assertIn("patched-research", available)
        self.assertIn("patched-figma", available)
        self.assertIn("patched-forced", available)
        research.assert_called_once()
        figma.assert_called_once()
        forced.assert_called_once()


class FigmaConsoleFallbackTests(unittest.TestCase):
    def test_inject_figma_console_server_adds_fallback_when_missing(self) -> None:
        cwd = Path("/tmp/project-a")
        merged = fixer_wire._inject_figma_console_server({"sqlite": {"command": "sqlite3"}}, cwd)

        self.assertIn("figma-console-mcp", merged)
        spec = merged["figma-console-mcp"]
        self.assertEqual(spec["command"], "npx")
        self.assertEqual(spec["args"], ["-y", "figma-console-mcp@latest"])
        self.assertEqual(spec["transport"], "stdio")
        self.assertEqual(spec["cwd"], str(cwd.resolve()))
        self.assertEqual(spec["startup_timeout_sec"], 120)
        self.assertEqual(spec["tool_timeout_sec"], 600)
        self.assertEqual(spec["timeout"], 600)
        self.assertEqual(spec["env"]["ENABLE_MCP_APPS"], "true")

    def test_inject_figma_console_server_preserves_existing_command(self) -> None:
        cwd = Path("/tmp/project-b")
        existing = {
            "figma-console-mcp": {
                "command": "custom-wrapper",
                "args": ["--foo"],
                "env": {"EXISTING": "1"},
            }
        }
        merged = fixer_wire._inject_figma_console_server(existing, cwd)
        spec = merged["figma-console-mcp"]

        self.assertEqual(spec["command"], "custom-wrapper")
        self.assertEqual(spec["args"], ["--foo"])
        self.assertEqual(spec["env"]["EXISTING"], "1")
        self.assertEqual(spec["env"]["ENABLE_MCP_APPS"], "true")

    def test_figma_console_credentials_alias_figma_token_to_access_token(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            cwd = Path(tmp) / "project"
            cwd.mkdir()
            env_file = Path(tmp) / "figma.env"
            env_file.write_text('FIGMA_TOKEN="file-token"\n', encoding="utf-8")

            with patch.dict(
                os.environ,
                {
                    "FIGMA_MCP_GLOBAL_ENV": str(env_file),
                    "FIGMA_ACCESS_TOKEN": "",
                    "FIGMA_TOKEN": "",
                },
                clear=False,
            ):
                env = fixer_wire._load_figma_console_credentials(cwd)

        self.assertEqual(env["FIGMA_TOKEN"], "file-token")
        self.assertEqual(env["FIGMA_ACCESS_TOKEN"], "file-token")

    def test_figma_console_credentials_alias_figma_api_key_to_access_token(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            cwd = Path(tmp) / "project"
            cwd.mkdir()
            env_file = Path(tmp) / "figma.env"
            env_file.write_text("FIGMA_API_KEY=file-api-key\n", encoding="utf-8")

            with patch.dict(
                os.environ,
                {
                    "FIGMA_MCP_GLOBAL_ENV": str(env_file),
                    "FIGMA_ACCESS_TOKEN": "",
                    "FIGMA_TOKEN": "",
                    "FIGMA_API_KEY": "",
                },
                clear=False,
            ):
                env = fixer_wire._load_figma_console_credentials(cwd)

        self.assertEqual(env["FIGMA_API_KEY"], "file-api-key")
        self.assertEqual(env["FIGMA_ACCESS_TOKEN"], "file-api-key")

    def test_inject_figma_console_server_preserves_env_and_adds_access_token(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            cwd = Path(tmp) / "project"
            cwd.mkdir()
            env_file = Path(tmp) / "figma.env"
            env_file.write_text("FIGMA_ACCESS_TOKEN=file-access-token\n", encoding="utf-8")

            with patch.dict(
                os.environ,
                {
                    "FIGMA_MCP_GLOBAL_ENV": str(env_file),
                    "FIGMA_ACCESS_TOKEN": "",
                    "FIGMA_TOKEN": "",
                },
                clear=False,
            ):
                merged = fixer_wire._inject_figma_console_server(
                    {
                        fixer_wire.FIGMA_CONSOLE_MCP_NAME: {
                            "command": "npx",
                            "env": {"ENABLE_MCP_APPS": "true", "EXISTING": "1"},
                        }
                    },
                    cwd,
                )

        env = merged[fixer_wire.FIGMA_CONSOLE_MCP_NAME]["env"]
        self.assertEqual(env["ENABLE_MCP_APPS"], "true")
        self.assertEqual(env["EXISTING"], "1")
        self.assertEqual(env["FIGMA_ACCESS_TOKEN"], "file-access-token")


class ForcedFixerSpecTests(unittest.TestCase):
    def test_load_forced_fixer_spec_falls_back_to_repo_binary_when_configured_path_is_missing(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            repo_root = Path(tmp)
            config_path = repo_root / "fixer_mcp" / "mcp_config.json"
            configured_path = Path(tmp) / "missing-host" / "fixer_mcp"
            fallback_path = repo_root / "fixer_mcp" / "fixer_mcp"
            config_path.parent.mkdir(parents=True, exist_ok=True)
            fallback_path.write_text("binary", encoding="utf-8")
            config_path.write_text(
                json.dumps({"mcpServers": {"fixer_mcp": {"command": str(configured_path)}}}) + "\n",
                encoding="utf-8",
            )
            rebuild_calls: list[Path] = []

            def record_rebuild(path: Path) -> None:
                rebuild_calls.append(path)

            with (
                patch.dict(os.environ, {fixer_wire.FIXER_MCP_BINARY_ENV: ""}, clear=False),
                patch.object(fixer_wire, "_repo_root", return_value=repo_root),
                patch.object(fixer_wire, "_maybe_rebuild_fixer_mcp_binary", side_effect=record_rebuild),
            ):
                spec = fixer_wire._load_forced_fixer_spec()

        self.assertEqual(spec["command"], str(fallback_path.resolve()))
        self.assertEqual(spec["cwd"], str((repo_root / "fixer_mcp").resolve()))
        self.assertEqual(rebuild_calls, [configured_path.resolve(), fallback_path.resolve()])

    def test_load_forced_fixer_spec_env_override_wins(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            repo_root = Path(tmp)
            config_path = repo_root / "fixer_mcp" / "mcp_config.json"
            configured_path = repo_root / "fixer_mcp" / "configured"
            override_path = repo_root / "override" / "fixer_mcp"
            config_path.parent.mkdir(parents=True, exist_ok=True)
            configured_path.write_text("configured", encoding="utf-8")
            override_path.parent.mkdir(parents=True, exist_ok=True)
            override_path.write_text("override", encoding="utf-8")
            config_path.write_text(
                json.dumps({"mcpServers": {"fixer_mcp": {"command": str(configured_path)}}}) + "\n",
                encoding="utf-8",
            )
            rebuild_calls: list[Path] = []

            with (
                patch.dict(os.environ, {fixer_wire.FIXER_MCP_BINARY_ENV: str(override_path)}, clear=False),
                patch.object(fixer_wire, "_repo_root", return_value=repo_root),
                patch.object(
                    fixer_wire,
                    "_maybe_rebuild_fixer_mcp_binary",
                    side_effect=lambda path: rebuild_calls.append(path),
                ),
            ):
                spec = fixer_wire._load_forced_fixer_spec()

        self.assertEqual(spec["command"], str(override_path.resolve()))
        self.assertEqual(spec["cwd"], str(override_path.parent.resolve()))
        self.assertEqual(rebuild_calls, [override_path.resolve()])

    def test_load_forced_fixer_spec_keeps_existing_configured_path(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            repo_root = Path(tmp)
            config_path = repo_root / "fixer_mcp" / "mcp_config.json"
            configured_path = Path(tmp) / "host" / "fixer_mcp"
            fallback_path = repo_root / "fixer_mcp" / "fixer_mcp"
            config_path.parent.mkdir(parents=True, exist_ok=True)
            configured_path.parent.mkdir(parents=True, exist_ok=True)
            configured_path.write_text("configured", encoding="utf-8")
            fallback_path.write_text("fallback", encoding="utf-8")
            config_path.write_text(
                json.dumps({"mcpServers": {"fixer_mcp": {"command": str(configured_path)}}}) + "\n",
                encoding="utf-8",
            )

            with (
                patch.dict(os.environ, {fixer_wire.FIXER_MCP_BINARY_ENV: ""}, clear=False),
                patch.object(fixer_wire, "_repo_root", return_value=repo_root),
            ):
                spec = fixer_wire._load_forced_fixer_spec()

        self.assertEqual(spec["command"], str(configured_path.resolve()))

    def test_load_forced_fixer_spec_applies_long_timeout_floor_when_missing(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            repo_root = Path(tmp)
            config_path = repo_root / "fixer_mcp" / "mcp_config.json"
            command_path = repo_root / "fixer_mcp" / "fixer_mcp"
            config_path.parent.mkdir(parents=True, exist_ok=True)
            command_path.write_text("binary", encoding="utf-8")
            config_path.write_text(
                json.dumps(
                    {
                        "mcpServers": {
                            "fixer_mcp": {
                                "command": str(command_path.resolve()),
                            }
                        }
                    }
                )
                + "\n",
                encoding="utf-8",
            )

            with (
                patch.dict(os.environ, {fixer_wire.FIXER_MCP_BINARY_ENV: ""}, clear=False),
                patch.object(fixer_wire, "_repo_root", return_value=repo_root),
            ):
                spec = fixer_wire._load_forced_fixer_spec()

        self.assertEqual(spec["timeout"], fixer_wire.FORCED_FIXER_MCP_TIMEOUT_FLOOR_SEC)
        self.assertEqual(spec["tool_timeout_sec"], fixer_wire.FORCED_FIXER_MCP_TIMEOUT_FLOOR_SEC)
        self.assertEqual(spec["per_tool_timeout_ms"], fixer_wire.FORCED_FIXER_MCP_TIMEOUT_FLOOR_MS)

    def test_load_forced_fixer_spec_raises_too_low_timeout_values(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            repo_root = Path(tmp)
            config_path = repo_root / "fixer_mcp" / "mcp_config.json"
            command_path = repo_root / "fixer_mcp" / "fixer_mcp"
            config_path.parent.mkdir(parents=True, exist_ok=True)
            command_path.write_text("binary", encoding="utf-8")
            config_path.write_text(
                json.dumps(
                    {
                        "mcpServers": {
                            "fixer_mcp": {
                                "command": str(command_path.resolve()),
                                "timeout": 600,
                                "tool_timeout_sec": 600,
                                "per_tool_timeout_ms": 600_000,
                            }
                        }
                    }
                )
                + "\n",
                encoding="utf-8",
            )

            with (
                patch.dict(os.environ, {fixer_wire.FIXER_MCP_BINARY_ENV: ""}, clear=False),
                patch.object(fixer_wire, "_repo_root", return_value=repo_root),
            ):
                spec = fixer_wire._load_forced_fixer_spec()

        self.assertEqual(spec["timeout"], fixer_wire.FORCED_FIXER_MCP_TIMEOUT_FLOOR_SEC)
        self.assertEqual(spec["tool_timeout_sec"], fixer_wire.FORCED_FIXER_MCP_TIMEOUT_FLOOR_SEC)
        self.assertEqual(spec["per_tool_timeout_ms"], fixer_wire.FORCED_FIXER_MCP_TIMEOUT_FLOOR_MS)

    def test_build_forced_fixer_override_args_includes_long_per_tool_timeout(self) -> None:
        spec = {
            "command": "/tmp/fixer_mcp",
            "args": [],
            "env": {},
            "transport": "stdio",
            "cwd": "/tmp",
            "startup_timeout_sec": 30,
            "timeout": fixer_wire.FORCED_FIXER_MCP_TIMEOUT_FLOOR_SEC,
            "tool_timeout_sec": fixer_wire.FORCED_FIXER_MCP_TIMEOUT_FLOOR_SEC,
            "per_tool_timeout_ms": fixer_wire.FORCED_FIXER_MCP_TIMEOUT_FLOOR_MS,
        }

        with patch.object(fixer_wire, "_load_forced_fixer_spec", return_value=spec):
            overrides = fixer_wire._build_forced_fixer_override_args()

        self.assertIn(
            "-c",
            overrides,
        )
        self.assertIn(
            f"mcp_servers.fixer_mcp.per_tool_timeout_ms={fixer_wire.FORCED_FIXER_MCP_TIMEOUT_FLOOR_MS}",
            overrides,
        )


class ResearchQueryFallbackTests(unittest.TestCase):
    def test_inject_research_query_server_adds_fallback_for_philologists(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            cwd = Path(tmp) / "philologists_paradise_ubuntu"
            llm_pipeline = cwd / "philologists_paradise" / "llm_pipeline"
            entrypoint = llm_pipeline / "cmd" / "research_query_mcp" / "main.go"
            entrypoint.parent.mkdir(parents=True, exist_ok=True)
            entrypoint.write_text("package main\nfunc main() {}\n", encoding="utf-8")

            with patch("client_wires.fixer_wire.shutil.which", return_value="/usr/bin/go"):
                merged = fixer_wire._inject_research_query_server({"sqlite": {"command": "sqlite3"}}, cwd)

        self.assertIn("research_query_mcp", merged)
        spec = merged["research_query_mcp"]
        self.assertEqual(spec["command"], "go")
        self.assertEqual(spec["args"], ["run", "./cmd/research_query_mcp", "--transport", "stdio"])
        self.assertEqual(spec["transport"], "stdio")
        self.assertEqual(spec["cwd"], str(llm_pipeline.resolve()))

    def test_inject_research_query_server_skips_when_go_missing(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            cwd = Path(tmp) / "philologists_paradise_ubuntu"
            llm_pipeline = cwd / "philologists_paradise" / "llm_pipeline"
            entrypoint = llm_pipeline / "cmd" / "research_query_mcp" / "main.go"
            entrypoint.parent.mkdir(parents=True, exist_ok=True)
            entrypoint.write_text("package main\nfunc main() {}\n", encoding="utf-8")

            with patch("client_wires.fixer_wire.shutil.which", return_value=None):
                merged = fixer_wire._inject_research_query_server({"sqlite": {"command": "sqlite3"}}, cwd)

        self.assertNotIn("research_query_mcp", merged)

    def test_inject_research_query_server_skips_for_non_philologists_project(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            cwd = Path(tmp) / "other_project"
            llm_pipeline = cwd / "philologists_paradise" / "llm_pipeline"
            entrypoint = llm_pipeline / "cmd" / "research_query_mcp" / "main.go"
            entrypoint.parent.mkdir(parents=True, exist_ok=True)
            entrypoint.write_text("package main\nfunc main() {}\n", encoding="utf-8")

            with patch("client_wires.fixer_wire.shutil.which", return_value="/usr/bin/go"):
                merged = fixer_wire._inject_research_query_server({"sqlite": {"command": "sqlite3"}}, cwd)

        self.assertNotIn("research_query_mcp", merged)


class WebMcpConfigTests(unittest.TestCase):
    @unittest.skip("public export replaces private MCP configs with examples")
    def test_repo_root_mcp_config_exposes_react_native_guide_for_codex_pro_discovery(self) -> None:
        repo_root = Path(__file__).resolve().parents[2]
        bootstrap_codex_pro_import_path()

        from client_wires.codex_compat.config import discover_project_mcp_servers

        servers = discover_project_mcp_servers(repo_root)

        self.assertIn("react-native-guide", servers)
        self.assertEqual(servers["react-native-guide"]["command"], "npx")
        self.assertEqual(servers["react-native-guide"]["args"], ["-y", "@mrnitro360/react-native-mcp-guide@latest"])
        self.assertEqual(servers["react-native-guide"]["transport"], "stdio")
        self.assertEqual(servers["react-native-guide"]["cwd"], str(repo_root.resolve()))
        self.assertEqual(servers["react-native-guide"]["startup_timeout_sec"], 120)
        self.assertEqual(servers["react-native-guide"]["tool_timeout_sec"], 600)
        self.assertEqual(servers["react-native-guide"]["timeout"], 600)
        self.assertEqual(servers["react-native-guide"]["_source"], "project_mcp")
        self.assertEqual(
            servers["react-native-guide"]["_config_path"],
            str((repo_root / "mcp_config.json").resolve()),
        )


class FixerMcpEnvBindingTests(unittest.TestCase):
    def test_bind_fixer_db_path_to_server_env_overrides_stale_value(self) -> None:
        selected = {
            "fixer_mcp": {
                "command": "/tmp/fixer_mcp",
                "env": {
                    "FIXER_DB_PATH": "/tmp/stale.db",
                    "TOKEN": "secret",
                },
            },
            "sqlite": {"command": "/tmp/sqlite"},
        }

        bound = fixer_wire._bind_fixer_db_path_to_server_env(
            selected,
            db_path=Path("/tmp/project/fixer.db"),
        )

        self.assertEqual(
            bound["fixer_mcp"]["env"],
            {
                "FIXER_DB_PATH": "/tmp/project/fixer.db",
                "TOKEN": "secret",
            },
        )
        self.assertEqual(bound["sqlite"], {"command": "/tmp/sqlite"})

    def test_bind_netrunner_stateless_auth_to_server_env_sets_role_and_cwd(self) -> None:
        selected = {
            "fixer_mcp": {
                "command": "/tmp/fixer_mcp",
                "env": {"TOKEN": "secret"},
            },
        }

        bound = fixer_wire._bind_netrunner_stateless_auth_to_server_env(
            selected,
            project_cwd=Path("/tmp/project"),
        )

        self.assertEqual(
            bound["fixer_mcp"]["env"],
            {
                "FIXER_MCP_DEFAULT_CWD": str(Path("/tmp/project").resolve()),
                "FIXER_MCP_DEFAULT_ROLE": "netrunner",
                "TOKEN": "secret",
            },
        )

    def test_bind_locked_role_to_server_env_sets_role(self) -> None:
        selected = {
            "fixer_mcp": {
                "command": "/tmp/fixer_mcp",
                "env": {"TOKEN": "secret"},
            },
            "sqlite": {"command": "/tmp/sqlite"},
        }

        bound = fixer_wire._bind_locked_role_to_server_env(selected, role="Fixer")

        self.assertEqual(
            bound["fixer_mcp"]["env"],
            {
                "FIXER_MCP_LOCKED_ROLE": "fixer",
                "TOKEN": "secret",
            },
        )
        self.assertEqual(bound["sqlite"], {"command": "/tmp/sqlite"})

    def test_launch_fresh_role_session_locks_forced_fixer_mcp_role(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            cwd = Path(tmp)
            db_path = cwd / "fixer.db"
            captured: dict[str, object] = {}

            def capture_runtime_files(
                _cwd: Path,
                _selection: object,
                selected: dict[str, object],
                _available: dict[str, object],
            ) -> None:
                captured["selected"] = selected

            available = {fixer_wire.FORCED_MCP_SERVER: {"command": "fixer_mcp"}}
            with (
                patch.dict(sys.modules, {"client_wires.codex_compat.llm": _fake_codex_main_module()}),
                patch("client_wires.fixer_wire.Path.cwd", return_value=cwd),
                patch.object(
                    fixer_wire,
                    "_select_fresh_launch_selection",
                    return_value=fixer_wire.SessionLaunchSelection("codex", "gpt-5.4", "medium"),
                ),
                patch.object(
                    fixer_wire,
                    "_load_available_servers",
                    return_value=(available, {}, _FakeAdapter(), (lambda _cwd: None)),
                ),
                patch.object(fixer_wire, "_resolve_fixer_db_path", return_value=db_path),
                patch.object(fixer_wire, "_build_backend_launch_env", return_value={}),
                patch.object(_FakeAdapter, "ensure_runtime_files", side_effect=capture_runtime_files),
                patch("client_wires.fixer_wire.subprocess.call", return_value=0),
            ):
                code = fixer_wire._launch_fresh_role_session(
                    "overseer",
                    "",
                    [],
                    selected_mcp_names=[fixer_wire.FORCED_MCP_SERVER],
                    dry_run=False,
                    preset_backend="codex",
                    preset_model="gpt-5.4",
                    preset_reasoning="medium",
                    dangerous_sandbox=True,
                    Option=_DummyOption,
                    single_select_items=lambda *_a, **_k: None,
                )

        self.assertEqual(code, 0)
        selected = captured["selected"]
        server_env = selected[fixer_wire.FORCED_MCP_SERVER]["env"]
        self.assertEqual(server_env[fixer_wire.FIXER_DB_PATH_ENV], str(db_path))
        self.assertEqual(server_env[fixer_wire.FIXER_MCP_LOCKED_ROLE_ENV], "overseer")

    def test_bind_launcher_telegram_env_to_server_env_copies_present_values(self) -> None:
        selected = {
            "fixer_mcp": {
                "command": "/tmp/fixer_mcp",
                "env": {"TOKEN": "secret"},
            },
        }

        with patch.dict(
            os.environ,
            {
                "FIXER_MCP_TELEGRAM_BOT_TOKEN": "bot-token",
                "FIXER_MCP_TELEGRAM_CHAT_ID": "12345",
                "FIXER_MCP_TELEGRAM_API_BASE_URL": "https://api.telegram.test",
            },
            clear=False,
        ):
            bound = fixer_wire._bind_launcher_telegram_env_to_server_env(selected)

        self.assertEqual(
            bound["fixer_mcp"]["env"],
            {
                "FIXER_MCP_TELEGRAM_API_BASE_URL": "https://api.telegram.test",
                "FIXER_MCP_TELEGRAM_BOT_TOKEN": "bot-token",
                "FIXER_MCP_TELEGRAM_CHAT_ID": "12345",
                "TOKEN": "secret",
            },
        )

    def test_bind_launcher_telegram_env_to_server_env_reads_missing_values_from_dotenv(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            fixer_root = Path(tmp) / "fixer_mcp"
            fixer_root.mkdir()
            (fixer_root / ".env").write_text(
                "\n".join(
                    [
                        "FIXER_MCP_TELEGRAM_BOT_TOKEN=dotenv-token",
                        'FIXER_MCP_TELEGRAM_CHAT_ID="67890"',
                        "OTHER_SECRET=ignored",
                    ]
                ),
                encoding="utf-8",
            )
            selected = {
                "fixer_mcp": {
                    "command": str(fixer_root / "fixer_mcp"),
                    "env": {"TOKEN": "secret"},
                },
            }

            with patch.dict(os.environ, {"HOME": str(Path(tmp) / "home")}, clear=True):
                bound = fixer_wire._bind_launcher_telegram_env_to_server_env(selected)

        self.assertEqual(bound["fixer_mcp"]["env"]["FIXER_MCP_TELEGRAM_BOT_TOKEN"], "dotenv-token")
        self.assertEqual(bound["fixer_mcp"]["env"]["FIXER_MCP_TELEGRAM_CHAT_ID"], "67890")
        self.assertEqual(bound["fixer_mcp"]["env"]["TOKEN"], "secret")
        self.assertNotIn("OTHER_SECRET", bound["fixer_mcp"]["env"])


class _DummyOption:
    def __init__(self, label: str, value: object | None = None, *, disabled: bool = False, is_header: bool = False) -> None:
        self.label = label
        self.value = value
        self.disabled = disabled
        self.is_header = is_header


class _FakeAdapter:
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
        return _FakeAdapter.build_execution_args(execution_prefs)

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

    @staticmethod
    def build_resume_command(option_args: list[str], external_session_id: str) -> list[str]:
        return ["codex", "resume", *option_args, external_session_id]


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

    def _reasoning_label(model: str, effort: str) -> str:
        return f"{model}:{effort}"

    class _FakeCodexCliAdapter:
        command = "codex"
        supports_resume = True

        @staticmethod
        def build_llm_args(llm_selection: object) -> list[str]:
            return ["--model", getattr(llm_selection, "model", "gpt-5.4")]

        @staticmethod
        def build_execution_args(_prefs: object) -> list[str]:
            return ["--sandbox", "danger-full-access"]

        @staticmethod
        def build_mcp_flags(selected_servers: dict[str, object], _available: dict[str, object]) -> list[str]:
            return [f"--mcp={','.join(sorted(selected_servers.keys()))}"] if selected_servers else []

        @staticmethod
        def build_prompt_args(prompt: str) -> list[str]:
            return ["--prompt", prompt] if prompt else []

        @staticmethod
        def prepare_env(_env: dict[str, str], _llm_selection: object) -> None:
            return None

    def _ensure_sqlite_scaffold(_cwd: Path) -> None:
        return None

    module.ExecutionPreferences = ExecutionPreferences
    module.LLMSelection = LLMSelection
    module.CONFIG_ENV_VARS = {}
    module.CODEX_CLI_ADAPTER = _FakeCodexCliAdapter()
    module._load_llm_env = _load_llm_env
    module._merge_env_with_os = _merge_env_with_os
    module._ensure_sqlite_scaffold = _ensure_sqlite_scaffold
    module._reasoning_label = _reasoning_label
    return module


def _fake_codex_config_loader_module(
    *,
    global_servers: dict[str, dict[str, object]] | None = None,
    self_servers: dict[str, dict[str, object]] | None = None,
    project_servers: dict[str, dict[str, object]] | None = None,
    missing_local: list[Path] | None = None,
) -> types.ModuleType:
    module = types.ModuleType("client_wires.codex_compat.config")
    global_specs = dict(global_servers or {})
    self_specs = dict(self_servers or {})
    project_specs = dict(project_servers or {})
    missing_specs = list(missing_local or [])

    class ConfigError(Exception):
        pass

    def attach_preprompts_from_command_paths(_servers: dict[str, dict[str, object]]) -> None:
        return None

    def discover_project_mcp_servers(_cwd: Path) -> dict[str, dict[str, object]]:
        return dict(project_specs)

    def discover_self_mcp_servers(_cwd: Path) -> tuple[dict[str, dict[str, object]], list[Path]]:
        return dict(self_specs), list(missing_specs)

    def fetch_mcp_servers(_config: object) -> dict[str, dict[str, object]]:
        return dict(global_specs)

    def get_config_path() -> Path:
        return Path("/tmp/codex-pro-config.toml")

    def load_config(_path: Path) -> dict[str, object]:
        return {"mcpServers": dict(global_specs)}

    def merge_mcp_servers(
        base: dict[str, dict[str, object]],
        extra: dict[str, dict[str, object]],
    ) -> dict[str, dict[str, object]]:
        merged = dict(base)
        merged.update(extra)
        return merged

    module.ConfigError = ConfigError
    module.attach_preprompts_from_command_paths = attach_preprompts_from_command_paths
    module.discover_project_mcp_servers = discover_project_mcp_servers
    module.discover_self_mcp_servers = discover_self_mcp_servers
    module.fetch_mcp_servers = fetch_mcp_servers
    module.get_config_path = get_config_path
    module.load_config = load_config
    module.merge_mcp_servers = merge_mcp_servers
    return module
