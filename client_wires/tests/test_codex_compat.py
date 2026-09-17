from __future__ import annotations

import io
import os
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import Mock, patch

from client_wires.codex_compat import config, llm, playwright_chrome_cdp, runtime, sessions, ui
from client_wires.backends.catalog import load_backend_entry
from client_wires.backends.manifest import load_manifest


class CodexCompatImportSurfaceTests(unittest.TestCase):
    def test_tree_actions_use_horizontal_arrows_and_not_e(self) -> None:
        collapsed = ui.Option("  0 ▸ Root", 7)
        expanded = ui.Option("  1 ▾ Root", 7)

        self.assertEqual(ui._tree_action_for_key(ui.curses.KEY_RIGHT, collapsed), "expand:7")
        self.assertEqual(ui._tree_action_for_key(ui.curses.KEY_LEFT, expanded), "collapse:7")
        self.assertIsNone(ui._tree_action_for_key(ord("e"), collapsed))

    def test_public_and_legacy_aliases_are_available(self) -> None:
        self.assertIs(ui.Option, ui.Option)
        self.assertIs(llm._reasoning_label, llm.reasoning_label)
        self.assertIs(llm._load_llm_env, llm.load_llm_env)
        self.assertIs(llm._merge_env_with_os, llm.merge_env_with_os)
        self.assertIs(runtime._ensure_sqlite_scaffold, runtime.ensure_sqlite_scaffold)
        self.assertIs(runtime._maybe_configure_playwright_runtime, runtime.maybe_configure_playwright_runtime)
        self.assertIs(sessions._load_session_summaries, sessions.load_session_summaries)
        self.assertIs(sessions._find_session_log, sessions.find_session_log)

    def test_codex_adapter_renders_dynamic_mcp_overrides(self) -> None:
        selected = {"local": {"command": "run-local", "args": ["--stdio"], "_source": "project_mcp"}}
        flags = llm.CODEX_CLI_ADAPTER.build_mcp_flags(selected, selected)

        self.assertIn("mcp_servers.local.enabled=true", flags)
        self.assertIn("mcp_servers.local.command=\"run-local\"", flags)
        self.assertIn('mcp_servers.local.args=["--stdio"]', flags)

    def test_codex_model_defaults_include_gpt_56_family(self) -> None:
        self.assertEqual(llm.DEFAULT_MODEL, "gpt-5.6-luna")
        self.assertEqual(llm.DEFAULT_REASONING, "high")
        self.assertEqual(llm.MODEL_DEFAULT_EFFORT["gpt-5.6-sol"], "high")
        self.assertEqual(llm.MODEL_DEFAULT_EFFORT["gpt-5.6-terra"], "high")
        self.assertEqual(llm.MODEL_DEFAULT_EFFORT["gpt-5.6-luna"], "high")
        self.assertEqual(
            [key for _label, key, _description in llm.MODEL_REASONING_OPTIONS["opencode-go/glm-5.3-flash"]],
            ["minimal", "low", "medium", "high", "xhigh"],
        )

    def test_codex_sol_ultra_reasoning_is_preserved_in_launch_args(self) -> None:
        catalog = load_backend_entry("codex")
        manifest = load_manifest(
            Path(__file__).resolve().parents[1] / "backends" / "manifest" / "codex.manifest.json"
        )
        selection = llm.LLMSelection(
            display_model="GPT-5.6 Sol",
            detail="",
            provider_slug="",
            model="gpt-5.6-sol",
            reasoning_effort="ultra",
            requires_provider_override=False,
        )

        self.assertIn("ultra", catalog["reasoning_options"])
        self.assertIn("ultra", manifest.reasoning.options)
        self.assertEqual(
            [key for _label, key, _description in llm.MODEL_REASONING_OPTIONS["gpt-5.6-sol"]],
            ["low", "medium", "high", "xhigh", "max", "ultra"],
        )
        self.assertEqual(llm.reasoning_label("gpt-5.6-sol", "ultra"), "Ultra")
        self.assertIn(
            'model_reasoning_effort="ultra"',
            llm.CODEX_CLI_ADAPTER.build_llm_args(selection),
        )

    def test_project_mcp_discovery_reads_local_configs(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            cwd = Path(tmp)
            (cwd / "mcp_config.json").write_text(
                '{"mcpServers":{"demo":{"command":"npx","args":["-y","demo"],"cwd":"."}}}',
                encoding="utf-8",
            )

            servers = config.discover_project_mcp_servers(cwd)

        self.assertEqual(servers["demo"]["command"], "npx")
        self.assertEqual(servers["demo"]["args"], ["-y", "demo"])
        self.assertEqual(servers["demo"]["_source"], "project_mcp")

    def test_playwright_headless_runtime_override(self) -> None:
        available = {"playwright": {"command": "old", "args": [], "enabled": False}}
        selected = {"playwright": available["playwright"]}

        mode = runtime.apply_playwright_runtime_mode(available, selected, mode="headless")

        self.assertEqual(mode, "headless")
        self.assertEqual(available["playwright"]["command"], "npx")
        self.assertIn("--headless", available["playwright"]["args"])
        self.assertEqual(selected["playwright"]["_source"], "preset_mcp")

    def test_playwright_chrome_runtime_uses_persistent_default_profile(self) -> None:
        available = {"playwright": {"command": "old", "args": [], "enabled": False}}
        selected = {"playwright": available["playwright"]}

        mode = runtime.apply_playwright_runtime_mode(available, selected, mode="chrome")

        self.assertEqual(mode, "chrome")
        self.assertEqual(available["playwright"]["command"], sys.executable)
        self.assertIn("--user-data-dir", available["playwright"]["args"])
        self.assertIn(str(runtime.PLAYWRIGHT_CHROME_PROFILE_DEFAULT), available["playwright"]["args"])
        self.assertIn("--shared-context", available["playwright"]["args"])
        self.assertNotIn("--isolated", available["playwright"]["args"])
        self.assertEqual(selected["playwright"]["_source"], "preset_mcp")

    def test_playwright_chrome_runtime_respects_profile_env_override(self) -> None:
        available = {"playwright": {"command": "old", "args": [], "enabled": False}}
        selected = {"playwright": available["playwright"]}

        with patch.dict(os.environ, {"CODEX_PRO_PLAYWRIGHT_CHROME_PROFILE": "~/tmp/profile"}, clear=False):
            runtime.apply_playwright_runtime_mode(available, selected, mode="headed")

        self.assertIn(str(Path("~/tmp/profile").expanduser()), available["playwright"]["args"])

    def test_playwright_persistent_headless_runtime_uses_profile_and_headless_flag(self) -> None:
        available = {"playwright": {"command": "old", "args": [], "enabled": False}}
        selected = {"playwright": available["playwright"]}

        mode = runtime.apply_playwright_runtime_mode(available, selected, mode="headless-profile")

        self.assertEqual(mode, "headless-profile")
        self.assertEqual(available["playwright"]["command"], sys.executable)
        self.assertIn("--user-data-dir", available["playwright"]["args"])
        self.assertIn("--headless", available["playwright"]["args"])
        self.assertNotIn("--shared-context", available["playwright"]["args"])
        self.assertIn(str(runtime.PLAYWRIGHT_CHROME_PROFILE_DEFAULT), available["playwright"]["args"])
        self.assertNotIn("--isolated", available["playwright"]["args"])
        self.assertEqual(selected["playwright"]["_source"], "preset_mcp")

    def test_playwright_persistent_headless_runtime_aliases(self) -> None:
        self.assertEqual(
            runtime.normalize_playwright_runtime_mode("persistent-headless"),
            "headless-profile",
        )
        self.assertEqual(
            runtime.normalize_playwright_runtime_mode("chrome-headless"),
            "headless-profile",
        )

    def test_playwright_chrome_wrapper_parses_remote_debugging_port(self) -> None:
        self.assertEqual(
            playwright_chrome_cdp._remote_debugging_port_from_command(
                "/Applications/Google Chrome --remote-debugging-port=64530 --user-data-dir=/tmp/profile"
            ),
            64530,
        )
        self.assertEqual(
            playwright_chrome_cdp._remote_debugging_port_from_command(
                "/Applications/Google Chrome --remote-debugging-port 64531 --user-data-dir /tmp/profile"
            ),
            64531,
        )

    def test_playwright_chrome_wrapper_attaches_to_existing_cdp_endpoint(self) -> None:
        profile_dir = Path("~/tmp/profile").expanduser()
        processes = [
            playwright_chrome_cdp.ChromeProfileProcess(
                pid=123,
                command=f"/Applications/Google Chrome --remote-debugging-port=64530 --user-data-dir={profile_dir}",
                remote_debugging_port=64530,
            )
        ]

        with (
            patch.object(playwright_chrome_cdp, "_chrome_processes_for_profile", return_value=processes),
            patch.object(playwright_chrome_cdp, "_cdp_endpoint_is_reachable", return_value=True) as reachable,
        ):
            endpoint = playwright_chrome_cdp._existing_cdp_endpoint_for_profile(profile_dir)

        self.assertEqual(endpoint, "http://127.0.0.1:64530")
        reachable.assert_called_once_with("http://127.0.0.1:64530")

    def test_playwright_chrome_wrapper_rejects_in_use_profile_without_reachable_cdp(self) -> None:
        profile_dir = Path("~/tmp/profile").expanduser()
        processes = [
            playwright_chrome_cdp.ChromeProfileProcess(
                pid=123,
                command=f"/Applications/Google Chrome --user-data-dir={profile_dir}",
                remote_debugging_port=None,
            )
        ]

        with patch.object(playwright_chrome_cdp, "_chrome_processes_for_profile", return_value=processes):
            with self.assertRaisesRegex(RuntimeError, "no reachable Chrome DevTools Protocol endpoint"):
                playwright_chrome_cdp._existing_cdp_endpoint_for_profile(profile_dir)

    def test_playwright_chrome_wrapper_kills_stubborn_owned_profile_child(self) -> None:
        chrome = Mock()
        profile_dir = Path("/tmp/owned-profile")

        with (
            patch.object(playwright_chrome_cdp, "_terminate") as terminate,
            patch.object(
                playwright_chrome_cdp,
                "_chrome_pids_for_profile",
                side_effect=[[111, 222], [222]],
            ),
            patch.object(playwright_chrome_cdp.os, "kill") as kill,
            patch.object(playwright_chrome_cdp.time, "sleep") as sleep,
        ):
            playwright_chrome_cdp._terminate_chrome_for_profile(chrome, profile_dir)

        terminate.assert_called_once_with(chrome)
        self.assertEqual(
            kill.call_args_list,
            [
                unittest.mock.call(111, playwright_chrome_cdp.signal.SIGTERM),
                unittest.mock.call(222, playwright_chrome_cdp.signal.SIGTERM),
                unittest.mock.call(222, playwright_chrome_cdp.signal.SIGKILL),
            ],
        )
        sleep.assert_called_once_with(1)

    def test_playwright_chrome_wrapper_main_uses_existing_cdp_without_launching_chrome(self) -> None:
        mcp_process = Mock()
        mcp_process.wait.return_value = 0

        with tempfile.TemporaryDirectory() as tmp:
            with (
                patch.object(
                    playwright_chrome_cdp,
                    "_existing_cdp_endpoint_for_profile",
                    return_value="http://127.0.0.1:64530",
                ),
                patch.object(playwright_chrome_cdp, "_find_chrome") as find_chrome,
                patch.object(playwright_chrome_cdp, "_wait_for_cdp") as wait_for_cdp,
                patch.object(playwright_chrome_cdp, "_terminate_chrome_for_profile") as terminate_chrome,
                patch.object(
                    playwright_chrome_cdp,
                    "_playwright_mcp_node_modules",
                    return_value=Path(tmp) / "node_modules",
                ),
                patch.object(playwright_chrome_cdp.subprocess, "Popen", return_value=mcp_process) as popen,
            ):
                result = playwright_chrome_cdp.main(["--user-data-dir", tmp, "--viewport-size", "1280x720"])

        self.assertEqual(result, 0)
        find_chrome.assert_not_called()
        wait_for_cdp.assert_not_called()
        terminate_chrome.assert_not_called()
        popen.assert_called_once()
        command = popen.call_args.args[0]
        self.assertEqual(command[0], playwright_chrome_cdp.shutil.which("node"))
        self.assertEqual(Path(command[1]).name, "playwright_owned_context_mcp.cjs")
        self.assertEqual(command[2:], ["--cdp-endpoint", "http://127.0.0.1:64530", "--viewport-size", "1280x720"])
        self.assertIn(str(Path(tmp) / "node_modules"), popen.call_args.kwargs["env"]["NODE_PATH"])

    def test_playwright_wrapper_and_owned_context_helper_are_packaged_locally(self) -> None:
        package_dir = Path(playwright_chrome_cdp.__file__).parent

        self.assertEqual(runtime.playwright_chrome_cdp_wrapper_path(), package_dir / "playwright_chrome_cdp.py")
        for name in (
            "playwright_chrome_cdp.py",
            "playwright_owned_context_mcp.cjs",
            "playwright_shared_context_mcp.cjs",
        ):
            asset = package_dir / name
            self.assertTrue(asset.is_file(), f"missing packaged Playwright ownership asset: {name}")

    def test_load_llm_env_alias_uses_codex_env_file(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            env_path = Path(tmp) / "llm.env"
            env_path.write_text('OPENAI_API_KEY="abc"\n# ignored\nBAD\n', encoding="utf-8")
            with patch.object(llm, "LLM_ENV_PATH", env_path):
                self.assertEqual(llm._load_llm_env(), {"OPENAI_API_KEY": "abc"})

    def test_merge_env_with_os_prefers_loaded_values(self) -> None:
        with patch.dict(os.environ, {"EXISTING": "old"}, clear=True):
            self.assertEqual(llm._merge_env_with_os({"EXISTING": "new"})["EXISTING"], "new")


class EnsureSqliteScaffoldNoninteractiveTests(unittest.TestCase):
    def test_noninteractive_returns_deterministic_config_without_curses_or_input(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            cwd = Path(tmp)
            with (
                patch.object(sys.stdin, "isatty", return_value=False),
                patch.object(sys.stdout, "isatty", return_value=False),
                patch.object(
                    runtime,
                    "single_select_items",
                    side_effect=AssertionError("curses selector must not be called"),
                ),
                patch("builtins.input", side_effect=AssertionError("input() must not be called")),
            ):
                result = runtime.ensure_sqlite_scaffold(cwd)

            self.assertEqual(result, cwd / "sqliteMCP.toml")
            self.assertTrue(result.is_file())
            content = result.read_text(encoding="utf-8")
            self.assertIn("db/dev.sqlite3", content)
            self.assertTrue((cwd / "db" / "dev.sqlite3").is_file())

    def test_noninteractive_uses_sqlite_db_path_env_override(self) -> None:
        with tempfile.TemporaryDirectory() as tmp, tempfile.TemporaryDirectory() as db_tmp:
            cwd = Path(tmp)
            override = Path(db_tmp) / "custom" / "app.db"
            with (
                patch.dict(os.environ, {runtime.SQLITE_DB_PATH_ENV: str(override)}, clear=False),
                patch.object(sys.stdin, "isatty", return_value=False),
                patch.object(sys.stdout, "isatty", return_value=False),
                patch.object(
                    runtime,
                    "single_select_items",
                    side_effect=AssertionError("curses selector must not be called"),
                ),
            ):
                result = runtime.ensure_sqlite_scaffold(cwd)

            self.assertEqual(result, cwd / "sqliteMCP.toml")
            self.assertTrue(override.is_file())
            content = result.read_text(encoding="utf-8")
            self.assertIn(f'db_path = "{override.as_posix()}"', content)

    def test_noninteractive_returns_none_with_actionable_error_when_db_uncreatable(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            cwd = Path(tmp)
            stderr = io.StringIO()
            with (
                patch.object(sys.stdin, "isatty", return_value=False),
                patch.object(sys.stdout, "isatty", return_value=False),
                patch.object(Path, "touch", side_effect=OSError("disk full")),
                patch("sys.stderr", new=stderr),
            ):
                result = runtime.ensure_sqlite_scaffold(cwd)

            self.assertIsNone(result)
            self.assertIn("CODEX_PRO_SQLITE_DB_PATH", stderr.getvalue())
            self.assertFalse((cwd / "sqliteMCP.toml").exists())

    def test_interactive_tty_still_uses_curses_selector(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            cwd = Path(tmp)
            db_path = cwd / "existing.sqlite3"
            db_path.write_text("", encoding="utf-8")
            with (
                patch.object(sys.stdin, "isatty", return_value=True),
                patch.object(sys.stdout, "isatty", return_value=True),
                patch.object(runtime, "single_select_items", return_value=db_path) as selector,
            ):
                result = runtime.ensure_sqlite_scaffold(cwd)

            self.assertEqual(result, cwd / "sqliteMCP.toml")
            selector.assert_called_once()
            self.assertIn("existing.sqlite3", result.read_text(encoding="utf-8"))


if __name__ == "__main__":
    unittest.main()
