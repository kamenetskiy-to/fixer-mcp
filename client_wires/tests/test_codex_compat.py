from __future__ import annotations

import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from client_wires.codex_compat import config, llm, runtime, sessions, ui


class CodexCompatImportSurfaceTests(unittest.TestCase):
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

    def test_load_llm_env_alias_uses_codex_env_file(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            env_path = Path(tmp) / "llm.env"
            env_path.write_text('OPENAI_API_KEY="abc"\n# ignored\nBAD\n', encoding="utf-8")
            with patch.object(llm, "LLM_ENV_PATH", env_path):
                self.assertEqual(llm._load_llm_env(), {"OPENAI_API_KEY": "abc"})

    def test_merge_env_with_os_prefers_loaded_values(self) -> None:
        with patch.dict(os.environ, {"EXISTING": "old"}, clear=True):
            self.assertEqual(llm._merge_env_with_os({"EXISTING": "new"})["EXISTING"], "new")


if __name__ == "__main__":
    unittest.main()
