from __future__ import annotations

import json
import os
import sqlite3
import sys
import tempfile
import types
import unittest
from datetime import datetime, timezone
from pathlib import Path
from unittest.mock import patch

from client_wires import fixer_wire
from client_wires.backends.claude_adapter import ClaudeCodeBackendAdapter
from client_wires.backends.droid_adapter import DroidBackendAdapter
from client_wires.bootstrap import bootstrap_codex_pro_import_path


class ResolveFixerDbPathTests(unittest.TestCase):
    def test_prefers_repo_local_db_over_cwd_db(self) -> None:
        with tempfile.TemporaryDirectory() as repo_tmp, tempfile.TemporaryDirectory() as cwd_tmp:
            repo_root = Path(repo_tmp)
            cwd = Path(cwd_tmp)
            repo_db = repo_root / "fixer_mcp" / "fixer.db"
            cwd_db = cwd / "fixer_mcp" / "fixer.db"
            repo_db.parent.mkdir(parents=True, exist_ok=True)
            cwd_db.parent.mkdir(parents=True, exist_ok=True)
            repo_db.touch()
            cwd_db.touch()

            with patch.object(fixer_wire, "_repo_root", return_value=repo_root):
                resolved = fixer_wire._resolve_fixer_db_path(cwd)

        self.assertEqual(resolved, repo_db.resolve())

    def test_ignores_fixer_genui_db_when_legacy_fixer_db_exists(self) -> None:
        with tempfile.TemporaryDirectory() as repo_tmp, tempfile.TemporaryDirectory() as cwd_tmp:
            repo_root = Path(repo_tmp)
            cwd = Path(cwd_tmp)
            repo_db = repo_root / "fixer_mcp" / "fixer.db"
            rogue_genui_db = repo_root / "fixer_mcp" / "fixer_genui.db"
            repo_db.parent.mkdir(parents=True, exist_ok=True)
            repo_db.touch()
            rogue_genui_db.touch()

            with patch.object(fixer_wire, "_repo_root", return_value=repo_root):
                resolved = fixer_wire._resolve_fixer_db_path(cwd)

        self.assertEqual(resolved, repo_db.resolve())

    def test_uses_env_override_first(self) -> None:
        with tempfile.TemporaryDirectory() as repo_tmp, tempfile.TemporaryDirectory() as cwd_tmp:
            repo_root = Path(repo_tmp)
            cwd = Path(cwd_tmp)
            repo_db = repo_root / "fixer_mcp" / "fixer.db"
            env_db = repo_root / "custom" / "wire.db"
            repo_db.parent.mkdir(parents=True, exist_ok=True)
            env_db.parent.mkdir(parents=True, exist_ok=True)
            repo_db.touch()
            env_db.touch()

            with (
                patch.object(fixer_wire, "_repo_root", return_value=repo_root),
                patch.dict(os.environ, {fixer_wire.FIXER_DB_PATH_ENV: str(env_db)}, clear=False),
            ):
                resolved = fixer_wire._resolve_fixer_db_path(cwd)

        self.assertEqual(resolved, env_db.resolve())

    def test_relative_env_override_is_resolved_from_repo_root(self) -> None:
        with tempfile.TemporaryDirectory() as repo_tmp, tempfile.TemporaryDirectory() as cwd_tmp:
            repo_root = Path(repo_tmp)
            cwd = Path(cwd_tmp)
            env_db = repo_root / "relative" / "fixer.db"
            env_db.parent.mkdir(parents=True, exist_ok=True)
            env_db.touch()

            with (
                patch.object(fixer_wire, "_repo_root", return_value=repo_root),
                patch.dict(os.environ, {fixer_wire.FIXER_DB_PATH_ENV: "relative/fixer.db"}, clear=False),
            ):
                resolved = fixer_wire._resolve_fixer_db_path(cwd)

        self.assertEqual(resolved, env_db.resolve())

    def test_falls_back_to_cwd_when_repo_db_missing(self) -> None:
        with tempfile.TemporaryDirectory() as repo_tmp, tempfile.TemporaryDirectory() as cwd_tmp:
            repo_root = Path(repo_tmp)
            cwd = Path(cwd_tmp)
            cwd_db = cwd / "fixer.db"
            cwd_db.touch()

            with patch.object(fixer_wire, "_repo_root", return_value=repo_root):
                resolved = fixer_wire._resolve_fixer_db_path(cwd)

        self.assertEqual(resolved, cwd_db.resolve())

    def test_error_lists_checked_candidates(self) -> None:
        with tempfile.TemporaryDirectory() as repo_tmp, tempfile.TemporaryDirectory() as cwd_tmp:
            repo_root = Path(repo_tmp)
            cwd = Path(cwd_tmp)
            with patch.object(fixer_wire, "_repo_root", return_value=repo_root):
                with self.assertRaises(RuntimeError) as ctx:
                    fixer_wire._resolve_fixer_db_path(cwd)

        message = str(ctx.exception)
        self.assertIn(f"Could not locate {fixer_wire.PRIMARY_FIXER_DB_FILENAME}.", message)
        self.assertIn(str((repo_root / "fixer_mcp" / "fixer.db").resolve()), message)
        self.assertIn(str((cwd / "fixer.db").resolve()), message)
        self.assertIn(fixer_wire.FIXER_DB_PATH_ENV, message)


class ResolveProjectIdTests(unittest.TestCase):
    def test_unknown_project_error_includes_fixer_registration_instructions(self) -> None:
        conn = sqlite3.connect(":memory:")
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
            missing_cwd = Path("/tmp/unknown-project-root")
            with self.assertRaises(RuntimeError) as ctx:
                fixer_wire._resolve_project_id(conn, missing_cwd)
        finally:
            conn.close()

        message = str(ctx.exception)
        self.assertIn("Project onboarding is Fixer-only", message)
        self.assertIn("register_project", message)
        self.assertIn(str(missing_cwd.resolve()), message)


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


class ForcedFixerSpecTests(unittest.TestCase):
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

            with patch.object(fixer_wire, "_repo_root", return_value=repo_root):
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

            with patch.object(fixer_wire, "_repo_root", return_value=repo_root):
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


class FixerMcpAutobuildTests(unittest.TestCase):
    def test_rebuilds_stale_binary_when_sources_are_newer(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            repo_root = Path(tmp)
            module_dir = repo_root / "fixer_mcp"
            module_dir.mkdir(parents=True, exist_ok=True)
            binary_path = module_dir / "fixer_mcp"
            go_main = module_dir / "main.go"
            go_mod = module_dir / "go.mod"
            go_sum = module_dir / "go.sum"

            go_main.write_text("package main\nfunc main() {}\n", encoding="utf-8")
            go_mod.write_text("module fixer_mcp\n\ngo 1.23\n", encoding="utf-8")
            go_sum.write_text("", encoding="utf-8")
            binary_path.write_text("old-binary", encoding="utf-8")

            os.utime(binary_path, (1_700_000_000, 1_700_000_000))
            os.utime(go_main, (1_700_000_100, 1_700_000_100))
            os.utime(go_mod, (1_700_000_100, 1_700_000_100))
            os.utime(go_sum, (1_700_000_100, 1_700_000_100))

            with (
                patch.object(fixer_wire, "_repo_root", return_value=repo_root),
                patch.object(fixer_wire, "_FIXER_MCP_BUILD_CHECKED", set()),
                patch("client_wires.fixer_wire.subprocess.run") as mock_run,
            ):
                fixer_wire._maybe_rebuild_fixer_mcp_binary(binary_path)

            mock_run.assert_called_once_with(
                ["go", "build", "-o", str(binary_path.resolve()), "."],
                cwd=str(module_dir),
                check=True,
            )

    def test_skips_rebuild_when_binary_is_current(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            repo_root = Path(tmp)
            module_dir = repo_root / "fixer_mcp"
            module_dir.mkdir(parents=True, exist_ok=True)
            binary_path = module_dir / "fixer_mcp"
            go_main = module_dir / "main.go"
            go_mod = module_dir / "go.mod"
            go_sum = module_dir / "go.sum"

            go_main.write_text("package main\nfunc main() {}\n", encoding="utf-8")
            go_mod.write_text("module fixer_mcp\n\ngo 1.23\n", encoding="utf-8")
            go_sum.write_text("", encoding="utf-8")
            binary_path.write_text("fresh-binary", encoding="utf-8")

            os.utime(go_main, (1_700_000_000, 1_700_000_000))
            os.utime(go_mod, (1_700_000_000, 1_700_000_000))
            os.utime(go_sum, (1_700_000_000, 1_700_000_000))
            os.utime(binary_path, (1_700_000_100, 1_700_000_100))

            with (
                patch.object(fixer_wire, "_repo_root", return_value=repo_root),
                patch.object(fixer_wire, "_FIXER_MCP_BUILD_CHECKED", set()),
                patch("client_wires.fixer_wire.subprocess.run") as mock_run,
            ):
                fixer_wire._maybe_rebuild_fixer_mcp_binary(binary_path)

            mock_run.assert_not_called()

    def test_skips_rebuild_for_external_binary_path(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            repo_root = Path(tmp)
            module_dir = repo_root / "fixer_mcp"
            module_dir.mkdir(parents=True, exist_ok=True)
            (module_dir / "main.go").write_text("package main\nfunc main() {}\n", encoding="utf-8")
            (module_dir / "go.mod").write_text("module fixer_mcp\n\ngo 1.23\n", encoding="utf-8")
            (module_dir / "go.sum").write_text("", encoding="utf-8")
            external_binary = repo_root / "external" / "fixer_mcp"
            external_binary.parent.mkdir(parents=True, exist_ok=True)
            external_binary.write_text("external-binary", encoding="utf-8")

            with (
                patch.object(fixer_wire, "_repo_root", return_value=repo_root),
                patch.object(fixer_wire, "_FIXER_MCP_BUILD_CHECKED", set()),
                patch("client_wires.fixer_wire.subprocess.run") as mock_run,
            ):
                fixer_wire._maybe_rebuild_fixer_mcp_binary(external_binary)

            mock_run.assert_not_called()


class _DummyOption:
    def __init__(self, label: str, value: object | None = None, *, disabled: bool = False, is_header: bool = False) -> None:
        self.label = label
        self.value = value
        self.disabled = disabled
        self.is_header = is_header


class _TrackingConnection:
    def __init__(self, inner: sqlite3.Connection) -> None:
        self._inner = inner
        self.closed = False

    def __enter__(self) -> "_TrackingConnection":
        self._inner.__enter__()
        return self

    def __exit__(self, exc_type: object, exc: object, tb: object) -> bool | None:
        return self._inner.__exit__(exc_type, exc, tb)

    def close(self) -> None:
        self.closed = True
        self._inner.close()

    def __getattr__(self, name: str) -> object:
        return getattr(self._inner, name)


class SessionPickerTests(unittest.TestCase):
    def test_select_session_interactive_uses_archived_when_no_active_sessions(self) -> None:
        rows = [
            fixer_wire.SessionRow(
                session_id=10,
                global_session_id=97,
                task_description="Validation Session: research_query_mcp",
                status="completed",
                codex_session_id="",
            ),
            fixer_wire.SessionRow(
                session_id=11,
                global_session_id=98,
                task_description="Fix Session: archived",
                status="review",
                codex_session_id="",
            ),
        ]
        captured: dict[str, object] = {}

        def choose(options: list[_DummyOption], **kwargs: object) -> int:
            captured["options"] = options
            return 10

        selected = fixer_wire._select_session_interactive(rows, _DummyOption, choose)

        self.assertEqual(selected.session_id, 10)
        labels = [option.label for option in captured["options"] if not option.is_header]
        self.assertTrue(any("[10]" in label and "completed" in label for label in labels))
        self.assertTrue(any("[11]" in label and "review" in label for label in labels))

    def test_select_session_interactive_raises_for_empty_rows(self) -> None:
        with self.assertRaises(RuntimeError):
            fixer_wire._select_session_interactive([], _DummyOption, lambda *_a, **_k: 1)


class McpPickerAndPromptTests(unittest.TestCase):
    def test_select_mcp_interactive_groups_by_category(self) -> None:
        captured: dict[str, object] = {}

        def choose(options: list[_DummyOption], **kwargs: object) -> list[str]:
            captured["options"] = options
            captured["kwargs"] = kwargs
            return ["sqlite", "gopls"]

        selected = fixer_wire._select_mcp_interactive(
            registry_names=["sqlite", "figma-console-mcp", "gopls", "tavily"],
            assigned_names=["gopls"],
            registry_meta={
                "sqlite": fixer_wire.RegistryMcpMetadata(is_default=True, category="DB", how_to="Use sqlite"),
                "figma-console-mcp": fixer_wire.RegistryMcpMetadata(
                    is_default=True,
                    category="Design",
                    how_to="Use Figma MCP",
                ),
                "gopls": fixer_wire.RegistryMcpMetadata(is_default=False, category="Coding", how_to="Use gopls"),
                "tavily": fixer_wire.RegistryMcpMetadata(is_default=False, category="Web-search", how_to="Use tavily"),
            },
            available_servers={"sqlite": {}, "figma-console-mcp": {}, "gopls": {}, "tavily": {}},
            Option=_DummyOption,
            multi_select_items=choose,
        )

        self.assertEqual(selected, ["sqlite", "gopls"])
        options = captured["options"]
        header_labels = [option.label for option in options if option.is_header]
        self.assertEqual(header_labels[0], "Session MCP defaults")
        self.assertEqual(header_labels[1:], ["DB", "Design", "Coding"])
        labels = [option.label for option in options if not option.is_header]
        self.assertIn("sqlite [default]", labels)
        self.assertIn("figma-console-mcp [default]", labels)
        self.assertNotIn("tavily", labels)

    def test_select_mcp_interactive_includes_assigned_even_when_not_default(self) -> None:
        captured: dict[str, object] = {}

        def choose(options: list[_DummyOption], **kwargs: object) -> list[str]:
            captured["options"] = options
            return ["gopls"]

        fixer_wire._select_mcp_interactive(
            registry_names=["sqlite", "gopls", "tavily"],
            assigned_names=["gopls"],
            registry_meta={
                "sqlite": fixer_wire.RegistryMcpMetadata(is_default=True, category="DB", how_to="Use sqlite"),
                "gopls": fixer_wire.RegistryMcpMetadata(is_default=False, category="Coding", how_to="Use gopls"),
                "tavily": fixer_wire.RegistryMcpMetadata(is_default=False, category="Web-search", how_to="Use tavily"),
            },
            available_servers={"sqlite": {}, "gopls": {}, "tavily": {}},
            Option=_DummyOption,
            multi_select_items=choose,
        )
        labels = [option.label for option in captured["options"] if not option.is_header]
        self.assertIn("sqlite [default]", labels)
        self.assertIn("gopls", labels)
        self.assertNotIn("tavily", labels)

    def test_select_mcp_interactive_always_shows_figma_console_in_design(self) -> None:
        captured: dict[str, object] = {}

        def choose(options: list[_DummyOption], **kwargs: object) -> list[str]:
            captured["options"] = options
            return ["sqlite"]

        fixer_wire._select_mcp_interactive(
            registry_names=["sqlite", "figma-console-mcp", "gopls"],
            assigned_names=[],
            registry_meta={
                "sqlite": fixer_wire.RegistryMcpMetadata(is_default=True, category="DB", how_to="Use sqlite"),
                "figma-console-mcp": fixer_wire.RegistryMcpMetadata(is_default=False, category="", how_to=""),
                "gopls": fixer_wire.RegistryMcpMetadata(is_default=False, category="Coding", how_to="Use gopls"),
            },
            available_servers={"sqlite": {}, "figma-console-mcp": {}, "gopls": {}},
            Option=_DummyOption,
            multi_select_items=choose,
        )

        options = captured["options"]
        header_labels = [option.label for option in options if option.is_header]
        self.assertEqual(header_labels[0], "Session MCP defaults")
        self.assertEqual(header_labels[1:], ["DB", "Design"])
        labels = [option.label for option in options if not option.is_header]
        self.assertIn("figma-console-mcp", labels)
        self.assertNotIn("gopls", labels)

    def test_load_registry_mcp_metadata_falls_back_for_legacy_schema(self) -> None:
        conn = sqlite3.connect(":memory:")
        try:
            conn.execute(
                """
                CREATE TABLE mcp_server (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    name TEXT NOT NULL UNIQUE
                )
                """
            )
            conn.execute("INSERT INTO mcp_server (name) VALUES ('legacy_server')")
            metadata = fixer_wire._load_registry_mcp_metadata(conn)
        finally:
            conn.close()

        self.assertIn("legacy_server", metadata)
        self.assertEqual(metadata["legacy_server"].category, "")
        self.assertEqual(metadata["legacy_server"].how_to, "")
        self.assertFalse(metadata["legacy_server"].is_default)

    def test_build_netrunner_prompt_includes_how_to_block(self) -> None:
        prompt = fixer_wire._build_netrunner_prompt(
            72,
            ["gopls", "sqlite"],
            {
                "gopls": "Use for Go diagnostics and references.",
                "sqlite": "Use for quick local DB inspection.",
            },
        )
        self.assertIn("Preselected session ID from fixer wire: `72`.", prompt)
        self.assertIn("Assigned MCP selection from fixer wire: gopls, sqlite.", prompt)
        self.assertIn("- gopls: Use for Go diagnostics and references.", prompt)
        self.assertIn("- sqlite: Use for quick local DB inspection.", prompt)
        self.assertNotIn("\n ", prompt)
        self.assertNotIn("Standard web stack guidance:", prompt)

    def test_build_netrunner_prompt_includes_standard_web_stack_guidance(self) -> None:
        prompt = fixer_wire._build_netrunner_prompt(
            73,
            ["playwright", "chrome-devtools", "eslint", "mcp-language-server"],
            {
                "playwright": "Use for deterministic browser automation.",
                "chrome-devtools": "Use for runtime debugging in Chrome.",
                "eslint": "Use for lint loops.",
                "mcp-language-server": "Use for LSP semantic tooling.",
            },
        )
        self.assertIn("Preselected session ID from fixer wire: `73`.", prompt)
        self.assertIn("Standard web stack guidance:", prompt)
        self.assertIn("- Next.js (App Router)", prompt)
        self.assertIn("- React + react-dom", prompt)
        self.assertIn("- TypeScript strict", prompt)
        self.assertIn("- Tailwind CSS + daisyUI", prompt)
        self.assertIn("- Framer Motion", prompt)
        self.assertIn("- react-responsive", prompt)
        self.assertIn("- eslint + eslint-config-next", prompt)
        self.assertNotIn("\n ", prompt)

    def test_build_mcp_how_to_map_uses_figma_fallback_guidance(self) -> None:
        result = fixer_wire._build_mcp_how_to_map(
            ["figma-console-mcp"],
            {"figma-console-mcp": fixer_wire.RegistryMcpMetadata(is_default=False, category="", how_to="")},
        )
        self.assertIn("Figma design-system extraction", result["figma-console-mcp"])

    def test_select_mcp_interactive_uses_allowed_runtime_pool_with_assigned_preselect(self) -> None:
        captured: dict[str, object] = {}

        def choose(options: list[_DummyOption], **kwargs: object) -> list[str]:
            captured["options"] = options
            return ["sqlite"]

        allowed_runtime = fixer_wire._allowed_runtime_mcp_names(
            allowed_names=["sqlite", "research_query_mcp"],
            available_servers={"sqlite": {}, "gopls": {}},
        )
        preselected = fixer_wire._assigned_preselected_mcp_names(
            assigned_names=["sqlite", "research_query_mcp"],
            allowed_runtime_names=allowed_runtime,
        )

        selected = fixer_wire._select_mcp_interactive(
            registry_names=allowed_runtime,
            assigned_names=preselected,
            registry_meta={
                "sqlite": fixer_wire.RegistryMcpMetadata(is_default=True, category="DB", how_to="Use sqlite"),
            },
            available_servers={"sqlite": {}, "gopls": {}},
            Option=_DummyOption,
            multi_select_items=choose,
        )

        self.assertEqual(selected, ["sqlite"])
        self.assertEqual(allowed_runtime, ["sqlite"])
        self.assertEqual(preselected, ["sqlite"])
        labels = [option.label for option in captured["options"] if not option.is_header]
        self.assertIn("sqlite [default]", labels)
        self.assertNotIn("research_query_mcp", labels)

    def test_select_mcp_interactive_show_all_registry_names_includes_non_default(self) -> None:
        captured: dict[str, object] = {}

        def choose(options: list[_DummyOption], **kwargs: object) -> list[str]:
            captured["options"] = options
            return ["research_query_mcp", "sqlite"]

        selected = fixer_wire._select_mcp_interactive(
            registry_names=["sqlite", "research_query_mcp"],
            assigned_names=["sqlite"],
            registry_meta={
                "sqlite": fixer_wire.RegistryMcpMetadata(is_default=True, category="DB", how_to="Use sqlite"),
                "research_query_mcp": fixer_wire.RegistryMcpMetadata(
                    is_default=False,
                    category="Web-search",
                    how_to="Use project research MCP",
                ),
            },
            available_servers={"sqlite": {}, "research_query_mcp": {}},
            Option=_DummyOption,
            multi_select_items=choose,
            show_all_registry_names=True,
        )

        self.assertEqual(selected, ["research_query_mcp", "sqlite"])
        labels = [option.label for option in captured["options"] if not option.is_header]
        self.assertIn("sqlite [default]", labels)
        self.assertIn("research_query_mcp", labels)


class ProjectScopedMcpBindingTests(unittest.TestCase):
    def test_load_project_allowed_mcp_names_isolated_per_project(self) -> None:
        conn = sqlite3.connect(":memory:")
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
                    status TEXT NOT NULL
                );
                CREATE TABLE mcp_server (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    name TEXT NOT NULL UNIQUE,
                    is_default INTEGER NOT NULL DEFAULT 0
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
                INSERT INTO project (id, name, cwd) VALUES
                    (1, 'Philologists Paradise', '/tmp/philologists'),
                    (2, 'Other', '/tmp/other');
                INSERT INTO mcp_server (id, name, is_default) VALUES
                    (1, 'sqlite', 1),
                    (2, 'research_query_mcp', 0);
                INSERT INTO project_mcp_server (project_id, mcp_server_id) VALUES
                    (1, 1),
                    (1, 2),
                    (2, 1);
                """
            )

            philologists_allowed = fixer_wire._load_project_allowed_mcp_names(conn, 1)
            other_allowed = fixer_wire._load_project_allowed_mcp_names(conn, 2)
        finally:
            conn.close()

        self.assertIn("research_query_mcp", philologists_allowed)
        self.assertNotIn("research_query_mcp", other_allowed)

    def test_allowed_runtime_and_preselected_mcp_names(self) -> None:
        assigned = ["sqlite", "research_query_mcp", "fixer_mcp"]
        allowed_project_a = ["sqlite", "research_query_mcp"]
        allowed_project_b = ["sqlite"]
        available = {"sqlite": {}, "research_query_mcp": {}, "gopls": {}}

        project_a_allowed_runtime = fixer_wire._allowed_runtime_mcp_names(allowed_project_a, available)
        project_b_allowed_runtime = fixer_wire._allowed_runtime_mcp_names(allowed_project_b, available)
        project_a_preselected = fixer_wire._assigned_preselected_mcp_names(assigned, project_a_allowed_runtime)
        project_b_preselected = fixer_wire._assigned_preselected_mcp_names(assigned, project_b_allowed_runtime)

        self.assertEqual(project_a_allowed_runtime, ["research_query_mcp", "sqlite"])
        self.assertEqual(project_b_allowed_runtime, ["sqlite"])
        self.assertEqual(project_a_preselected, ["research_query_mcp", "sqlite"])
        self.assertEqual(project_b_preselected, ["sqlite"])

    def test_assigned_allowed_includes_unavailable_for_visibility(self) -> None:
        assigned = ["sqlite", "research_query_mcp"]
        allowed = ["sqlite", "research_query_mcp"]
        available = {"sqlite": {}}

        assigned_allowed = fixer_wire._assigned_allowed_mcp_names(assigned, allowed)
        allowed_runtime = fixer_wire._allowed_runtime_mcp_names(allowed, available)
        assigned_unavailable = sorted(set(assigned_allowed) - set(allowed_runtime))
        picker_pool = sorted(set(allowed_runtime).union(assigned_unavailable))

        self.assertEqual(assigned_allowed, ["research_query_mcp", "sqlite"])
        self.assertEqual(allowed_runtime, ["sqlite"])
        self.assertEqual(assigned_unavailable, ["research_query_mcp"])
        self.assertEqual(picker_pool, ["research_query_mcp", "sqlite"])


class WebMcpConfigTests(unittest.TestCase):
    def test_repo_root_mcp_config_exposes_react_native_guide_for_codex_pro_discovery(self) -> None:
        repo_root = Path(__file__).resolve().parents[2]
        bootstrap_codex_pro_import_path()

        from codex_pro_app.config_loader import discover_project_mcp_servers

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


class LaunchEnvTests(unittest.TestCase):
    def test_build_backend_launch_env_persists_proxy_snapshot_for_project(self) -> None:
        adapter = type(
            "Adapter",
            (),
            {
                "name": "codex",
                "prepare_env": staticmethod(lambda env, _selection: env.setdefault("PREPARED", "1")),
            },
        )()
        with tempfile.TemporaryDirectory() as tmp:
            cwd = Path(tmp)
            with patch.dict(
                os.environ,
                {
                    "HOME": "/tmp/home",
                    "ALL_PROXY": "socks5h://[::1]:11024",
                    "HTTP_PROXY": "socks5h://[::1]:11024",
                    "HTTPS_PROXY": "socks5h://[::1]:11024",
                },
                clear=True,
            ):
                env = fixer_wire._build_backend_launch_env(
                    adapter,
                    object(),
                    cwd=cwd,
                    load_llm_env=lambda: {"OPENAI_API_KEY": "codex-only"},
                    merge_env_with_os=lambda payload: {"HOME": "/tmp/home", **payload},
                )

            payload = json.loads((cwd / ".codex" / "runtime_proxy_env.json").read_text(encoding="utf-8"))

        self.assertEqual(env["ALL_PROXY"], "socks5h://[::1]:11024")
        self.assertEqual(env["all_proxy"], "socks5h://[::1]:11024")
        self.assertEqual(payload["proxy_env"]["HTTPS_PROXY"], "socks5h://[::1]:11024")
        self.assertEqual(payload["proxy_env"]["https_proxy"], "socks5h://[::1]:11024")

    def test_build_backend_launch_env_clears_proxy_for_droid_backend(self) -> None:
        from client_wires.backends.droid_adapter import DroidBackendAdapter

        adapter = DroidBackendAdapter()
        with tempfile.TemporaryDirectory() as tmp:
            cwd = Path(tmp)
            with patch.dict(
                os.environ,
                {
                    "HOME": "/tmp/home",
                    "ALL_PROXY": "socks5h://[::1]:11024",
                    "HTTP_PROXY": "socks5h://[::1]:11024",
                    "HTTPS_PROXY": "socks5h://[::1]:11024",
                    "NO_PROXY": "127.0.0.1,localhost,::1",
                },
                clear=True,
            ):
                env = fixer_wire._build_backend_launch_env(
                    adapter,
                    object(),
                    cwd=cwd,
                )

        for key in (
            "ALL_PROXY",
            "all_proxy",
            "HTTP_PROXY",
            "http_proxy",
            "HTTPS_PROXY",
            "https_proxy",
            "NO_PROXY",
            "no_proxy",
        ):
            self.assertNotIn(key, env)

    def test_build_backend_launch_env_clears_proxy_for_claude_backend(self) -> None:
        from client_wires.backends.claude_adapter import ClaudeCodeBackendAdapter

        adapter = ClaudeCodeBackendAdapter()
        selection = types.SimpleNamespace(model="sonnet", reasoning_effort="medium")
        with tempfile.TemporaryDirectory() as tmp:
            cwd = Path(tmp)
            db_path = cwd / "fixer.db"
            db_path.write_text("", encoding="utf-8")
            with patch.dict(
                os.environ,
                {
                    "HOME": "/tmp/home",
                    "ALL_PROXY": "socks5h://[::1]:11024",
                    "HTTP_PROXY": "socks5h://[::1]:11024",
                    "HTTPS_PROXY": "socks5h://[::1]:11024",
                    "NO_PROXY": "127.0.0.1,localhost,::1",
                },
                clear=True,
            ):
                with patch.object(fixer_wire, "_resolve_fixer_db_path", return_value=db_path):
                    env = fixer_wire._build_backend_launch_env(
                        adapter,
                        selection,
                        cwd=cwd,
                        load_llm_env=lambda: {},
                        merge_env_with_os=lambda payload: payload,
                    )

        for key in (
            "ALL_PROXY",
            "all_proxy",
            "HTTP_PROXY",
            "http_proxy",
            "HTTPS_PROXY",
            "https_proxy",
            "NO_PROXY",
            "no_proxy",
        ):
            self.assertNotIn(key, env)

    def test_load_project_web_mcp_servers_from_toml(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            cwd = Path(tmp)
            (cwd / "webMCP.toml").write_text(
                """
[mcp_servers.playwright]
command = "npx"
args = ["-y", "@playwright/mcp@latest"]
transport = "stdio"
startup_timeout_sec = 90
tool_timeout_sec = 300

[mcp_servers.chrome-devtools]
command = "npx"
args = ["-y", "chrome-devtools-mcp@latest"]
cwd = "./frontend"
                """.strip()
                + "\n",
                encoding="utf-8",
            )

            servers = fixer_wire._load_project_web_mcp_servers(cwd)

        self.assertIn("playwright", servers)
        self.assertEqual(servers["playwright"]["command"], "npx")
        self.assertEqual(servers["playwright"]["args"], ["-y", "@playwright/mcp@latest"])
        self.assertEqual(servers["playwright"]["startup_timeout_sec"], 90)
        self.assertEqual(servers["playwright"]["tool_timeout_sec"], 300)
        self.assertEqual(servers["playwright"]["_source"], "project_mcp")

        self.assertIn("chrome-devtools", servers)
        self.assertEqual(servers["chrome-devtools"]["command"], "npx")
        self.assertEqual(servers["chrome-devtools"]["args"], ["-y", "chrome-devtools-mcp@latest"])
        self.assertEqual(
            servers["chrome-devtools"]["cwd"],
            str((cwd / "frontend").resolve()),
        )

    def test_overlay_project_mcp_servers_overrides_global(self) -> None:
        base = {
            "playwright": {"command": "old-wrapper.sh", "enabled": False},
            "gopls": {"command": "gopls-wrapper.sh", "enabled": False},
        }
        overrides = {
            "playwright": {"command": "npx", "args": ["-y", "@playwright/mcp@latest"], "_source": "project_mcp"},
            "eslint": {"command": "npx", "args": ["-y", "@eslint/mcp@latest"], "_source": "project_mcp"},
        }

        merged = fixer_wire._overlay_project_mcp_servers(base, overrides)

        self.assertEqual(merged["playwright"]["command"], "npx")
        self.assertEqual(merged["playwright"]["args"], ["-y", "@playwright/mcp@latest"])
        self.assertIn("gopls", merged)
        self.assertIn("eslint", merged)

    def test_repo_web_mcp_config_exposes_react_native_guide(self) -> None:
        repo_root = Path(__file__).resolve().parents[2]

        servers = fixer_wire._load_project_web_mcp_servers(repo_root)

        self.assertIn("react-native-guide", servers)
        self.assertEqual(servers["react-native-guide"]["command"], "npx")
        self.assertEqual(servers["react-native-guide"]["args"], ["-y", "@mrnitro360/react-native-mcp-guide@latest"])
        self.assertEqual(servers["react-native-guide"]["transport"], "stdio")
        self.assertEqual(servers["react-native-guide"]["_source"], "project_mcp")
        self.assertEqual(
            servers["react-native-guide"]["_config_path"],
            str((repo_root / "webMCP.toml").resolve()),
        )

    def test_load_available_servers_includes_repo_web_mcp_servers(self) -> None:
        repo_root = Path(__file__).resolve().parents[2]
        main_module = _fake_codex_main_module()
        config_loader = _fake_codex_config_loader_module(
            global_servers={"sqlite": {"command": "sqlite3"}},
        )
        codex_pro_package = types.ModuleType("codex_pro_app")
        codex_pro_package.main = main_module
        codex_pro_package.config_loader = config_loader

        with (
            patch.dict(
                sys.modules,
                {
                    "codex_pro_app": codex_pro_package,
                    "codex_pro_app.main": main_module,
                    "codex_pro_app.config_loader": config_loader,
                },
            ),
            patch.object(fixer_wire, "_inject_research_query_server", side_effect=lambda servers, _cwd: servers),
            patch.object(fixer_wire, "_inject_figma_console_server", side_effect=lambda servers, _cwd: servers),
            patch.object(fixer_wire, "_inject_forced_fixer_server", side_effect=lambda servers: servers),
        ):
            available, _config_env_vars, _adapter, _ensure_sqlite_scaffold = fixer_wire._load_available_servers(repo_root)

        self.assertIn("sqlite", available)
        self.assertIn("react-native-guide", available)
        self.assertEqual(available["react-native-guide"]["command"], "npx")
        self.assertEqual(available["react-native-guide"]["args"], ["-y", "@mrnitro360/react-native-mcp-guide@latest"])


def _fake_codex_main_module() -> types.ModuleType:
    module = types.ModuleType("codex_pro_app.main")

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
    module = types.ModuleType("codex_pro_app.config_loader")
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


def _make_history_summary(
    session_id: str,
    *,
    preview: str,
    created: datetime | None = None,
    updated: datetime | None = None,
) -> types.SimpleNamespace:
    return types.SimpleNamespace(
        session_id=session_id,
        created=created or datetime(2026, 2, 1, 10, 0, tzinfo=timezone.utc),
        updated=updated or datetime(2026, 2, 1, 12, 30, tzinfo=timezone.utc),
        preview=preview,
    )


def _fake_codex_history_module(
    summaries: list[types.SimpleNamespace],
    log_paths: dict[str, Path],
) -> types.ModuleType:
    module = _fake_codex_main_module()

    def _load_session_summaries(_history_path: Path, *, limit: int, cwd_filter: Path | None = None) -> list[types.SimpleNamespace]:
        del cwd_filter
        return summaries[:limit]

    def _find_session_log(session_id: str, *, created: datetime, updated: datetime) -> Path | None:
        del created, updated
        return log_paths.get(session_id)

    module._load_session_summaries = _load_session_summaries
    module._find_session_log = _find_session_log
    return module


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


class BackendCatalogTests(unittest.TestCase):
    def test_available_backend_descriptors_include_claude(self) -> None:
        backend_names = [descriptor.name for descriptor in fixer_wire.available_backend_descriptors()]
        self.assertEqual(backend_names, ["codex", "droid", "claude"])

    def test_droid_catalog_includes_custom_qwen_model(self) -> None:
        descriptor = next(item for item in fixer_wire.available_backend_descriptors() if item.name == "droid")
        self.assertIn("custom:Qwen3.6-Plus-Free-[OpenRouter]-0", descriptor.model_options)

    def test_droid_catalog_migrates_legacy_preview_qwen_model(self) -> None:
        descriptor = next(item for item in fixer_wire.available_backend_descriptors() if item.name == "droid")
        self.assertEqual(
            fixer_wire._normalize_backend_model(descriptor, "custom:qwen/qwen3.6-plus-preview:free"),
            "custom:Qwen3.6-Plus-Free-[OpenRouter]-0",
        )

    def test_droid_catalog_migrates_legacy_raw_qwen_model(self) -> None:
        descriptor = next(item for item in fixer_wire.available_backend_descriptors() if item.name == "droid")
        self.assertEqual(
            fixer_wire._normalize_backend_model(descriptor, "custom:qwen/qwen3.6-plus:free"),
            "custom:Qwen3.6-Plus-Free-[OpenRouter]-0",
        )

    def test_claude_catalog_excludes_droid_only_qwen_model(self) -> None:
        descriptor = next(item for item in fixer_wire.available_backend_descriptors() if item.name == "claude")
        self.assertNotIn("custom:Qwen3.6-Plus-Free-[OpenRouter]-0", descriptor.model_options)

    def test_droid_resume_command_uses_short_session_flag(self) -> None:
        adapter = DroidBackendAdapter()
        command = adapter.build_resume_command([], "resume-42")
        self.assertEqual(command, ["droid", "--resume", "resume-42"])

    def test_droid_adapter_requests_true_no_confirmation_mode(self) -> None:
        adapter = DroidBackendAdapter()
        prefs = types.SimpleNamespace(dangerous_sandbox=True, auto_approve=True)
        self.assertEqual(adapter.build_execution_args(prefs), ["--skip-permissions-unsafe"])

    def test_droid_adapter_omits_no_confirmation_flag_for_interactive_launches(self) -> None:
        adapter = DroidBackendAdapter()
        prefs = types.SimpleNamespace(dangerous_sandbox=True, auto_approve=True)
        self.assertEqual(adapter.build_interactive_execution_args(prefs), [])

    def test_claude_adapter_builds_fresh_launch_command(self) -> None:
        adapter = ClaudeCodeBackendAdapter()
        command = adapter.build_headless_command(
            model="sonnet",
            reasoning="medium",
            selected={},
            available={},
            prompt="hello",
        )
        self.assertEqual(command, ["claude", "--model", "sonnet", "--dangerously-skip-permissions", "hello"])

    def test_claude_adapter_builds_resume_command(self) -> None:
        adapter = ClaudeCodeBackendAdapter()
        command = adapter.build_resume_command(["--model", "sonnet"], "session-123")
        self.assertEqual(command, ["claude", "--resume", "session-123", "--model", "sonnet"])

    def test_build_backend_launch_env_uses_os_env_for_non_codex_backends(self) -> None:
        adapter = ClaudeCodeBackendAdapter()
        selection = types.SimpleNamespace(model="sonnet", reasoning_effort="medium")
        with patch.dict(os.environ, {"HOME": "/tmp/home", "OPENAI_API_KEY": "host-openai"}, clear=True):
            with tempfile.TemporaryDirectory() as tmp:
                cwd = Path(tmp)
                db_path = cwd / "fixer.db"
                db_path.write_text("", encoding="utf-8")
                with patch.object(fixer_wire, "_resolve_fixer_db_path", return_value=db_path):
                    env = fixer_wire._build_backend_launch_env(
                        adapter,
                        selection,
                        cwd=cwd,
                        load_llm_env=lambda: {"OPENAI_API_KEY": "codex-only", "ANTHROPIC_API_KEY": "codex-anthropic"},
                        merge_env_with_os=lambda payload: {"merged": "unused", **payload},
                    )
        self.assertEqual(env["HOME"], "/tmp/home")
        self.assertEqual(env["OPENAI_API_KEY"], "host-openai")
        self.assertEqual(env[fixer_wire.FIXER_DB_PATH_ENV], str(db_path))
        self.assertNotIn("ANTHROPIC_API_KEY", env)


class DroidRuntimeMaterializationTests(unittest.TestCase):
    def test_droid_adapter_writes_factory_mcp_and_skills(self) -> None:
        adapter = DroidBackendAdapter()
        with tempfile.TemporaryDirectory() as tmp, tempfile.TemporaryDirectory() as codex_home_tmp:
            cwd = Path(tmp)
            codex_home = Path(codex_home_tmp)
            skill_dir = codex_home / "skills" / "start-fixer"
            skill_dir.mkdir(parents=True, exist_ok=True)
            (skill_dir / "SKILL.md").write_text("# start-fixer\n", encoding="utf-8")
            selection = types.SimpleNamespace(model="gpt-5.4", reasoning_effort="medium")
            selected = {
                "fixer_mcp": {
                    "command": "/tmp/fixer_mcp",
                    "args": ["--serve"],
                    "env": {"TOKEN": "secret"},
                    "timeout": 21600,
                    "tool_timeout_sec": 21600,
                    "startup_timeout_sec": 30,
                    "transport": "stdio",
                }
            }
            selected = fixer_wire._bind_fixer_db_path_to_server_env(
                selected,
                db_path=cwd / "fixer.db",
            )
            selected = fixer_wire._bind_netrunner_stateless_auth_to_server_env(
                selected,
                project_cwd=cwd,
            )

            with patch.dict(os.environ, {"CODEX_HOME": str(codex_home)}, clear=False):
                adapter.ensure_runtime_files(cwd, selection, selected, selected)

            mcp_payload = json.loads((cwd / ".factory" / "mcp.json").read_text(encoding="utf-8"))
            self.assertEqual(
                mcp_payload,
                {
                    "mcpServers": {
                        "fixer_mcp": {
                            "args": ["--serve"],
                            "command": "/tmp/fixer_mcp",
                            "disabled": False,
                            "env": {
                                "FIXER_DB_PATH": str(cwd / "fixer.db"),
                                "FIXER_MCP_DEFAULT_CWD": str(cwd.resolve()),
                                "FIXER_MCP_DEFAULT_ROLE": "netrunner",
                                "TOKEN": "secret",
                            },
                            "type": "stdio",
                        }
                    }
                },
            )
            settings_payload = json.loads((cwd / ".factory" / "settings.json").read_text(encoding="utf-8"))
            self.assertEqual(
                settings_payload["sessionDefaultSettings"],
                {"model": "gpt-5.4", "reasoningEffort": "medium"},
            )
            self.assertTrue((cwd / ".factory" / "skills" / "start-fixer" / "SKILL.md").is_file())

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


class FreshLaunchSelectionTests(unittest.TestCase):
    def test_select_fresh_launch_selection_accepts_claude(self) -> None:
        selection = fixer_wire._select_fresh_launch_selection(
            preset_backend="claude",
            preset_model="sonnet",
            preset_reasoning="medium",
            Option=_DummyOption,
            single_select_items=lambda *_a, **_k: None,
        )
        self.assertEqual(selection.backend, "claude")
        self.assertEqual(selection.model, "sonnet")


class FixerResumeUiTests(unittest.TestCase):
    def test_select_fixer_resume_requires_sessions(self) -> None:
        with self.assertRaises(RuntimeError):
            fixer_wire._select_fixer_resume_session_interactive([], _DummyOption, lambda *_a, **_k: None)

    def test_select_fixer_resume_label_includes_started_and_updated(self) -> None:
        summary = types.SimpleNamespace(
            session_id="session-123",
            created=datetime(2026, 2, 1, 10, 0, tzinfo=timezone.utc),
            updated=datetime(2026, 2, 1, 12, 30, tzinfo=timezone.utc),
            preview="## Goal\nImplement fixer resume flow",
        )
        captured: dict[str, object] = {}

        def choose(options: list[_DummyOption], **kwargs: object) -> str:
            captured["options"] = options
            captured["kwargs"] = kwargs
            return "session-123"

        selected = fixer_wire._select_fixer_resume_session_interactive([summary], _DummyOption, choose)
        self.assertEqual(selected, "session-123")
        options = captured["options"]
        self.assertEqual(options[0].is_header, True)
        row_label = options[1].label
        self.assertIn("started", row_label)
        self.assertIn("updated", row_label)
        self.assertIn("Implement fixer resume flow", row_label)

    def test_session_log_has_fixer_marker(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            log_path = Path(tmp) / "session.jsonl"
            log_path.write_text(
                '\n'.join(
                    [
                        '{"type":"session_meta"}',
                        '{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"Activate skill `$start-fixer` immediately."}]}}',
                    ]
                ),
                encoding="utf-8",
            )
            self.assertTrue(fixer_wire._session_log_has_fixer_marker(log_path))

    def test_load_resume_summaries_separates_fixer_and_netrunner_threads(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            fixer_log = Path(tmp) / "fixer.jsonl"
            fixer_log.write_text(
                'Activate skill `$start-fixer` immediately.\n',
                encoding="utf-8",
            )
            netrunner_log = Path(tmp) / "netrunner.jsonl"
            netrunner_log.write_text(
                (
                    'Activate skill `$manual-resolution` immediately.\n'
                    'Preselected session ID from fixer wire: `34`.\n'
                ),
                encoding="utf-8",
            )
            fixer_summary = _make_history_summary("fixer-123", preview="Fixer thread")
            netrunner_summary = _make_history_summary("runner-456", preview="Netrunner thread")
            history_module = _fake_codex_history_module(
                [netrunner_summary, fixer_summary],
                {
                    "fixer-123": fixer_log,
                    "runner-456": netrunner_log,
                },
            )

            with patch.dict(sys.modules, {"codex_pro_app.main": history_module}):
                fixer_summaries = fixer_wire._load_fixer_resume_summaries(Path("/tmp/project"))
                netrunner_summaries = fixer_wire._load_netrunner_resume_summaries(Path("/tmp/project"), 34)

        self.assertEqual([summary.session_id for summary in fixer_summaries], ["fixer-123"])
        self.assertEqual([summary.session_id for summary in netrunner_summaries], ["runner-456"])

    def test_load_resume_summaries_includes_sqlite_aliased_fixer_thread(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            db_path = Path(tmp) / "fixer.db"
            project_cwd = Path("/tmp/project").resolve()
            conn = sqlite3.connect(db_path)
            try:
                conn.executescript(
                    f"""
                    CREATE TABLE project (
                        id INTEGER PRIMARY KEY AUTOINCREMENT,
                        name TEXT NOT NULL,
                        cwd TEXT UNIQUE NOT NULL
                    );
                    INSERT INTO project (id, name, cwd)
                    VALUES (1, 'Test Project', '{project_cwd}');
                    """
                )
                fixer_wire._ensure_wire_schema(conn)
                conn.execute(
                    """
                    INSERT INTO fixer_resume_session_alias (project_id, codex_session_id, note)
                    VALUES (1, 'overseer-aliased', 'manual fixer resume alias')
                    """
                )
                conn.commit()
            finally:
                conn.close()

            overseer_log = Path(tmp) / "overseer.jsonl"
            overseer_log.write_text(
                'Activate skill `$start-overseer` immediately.\n',
                encoding="utf-8",
            )
            summary = _make_history_summary("overseer-aliased", preview="Overseer thread")
            history_module = _fake_codex_history_module(
                [summary],
                {"overseer-aliased": overseer_log},
            )

            with (
                patch.dict(sys.modules, {"codex_pro_app.main": history_module}),
                patch.dict(os.environ, {fixer_wire.FIXER_DB_PATH_ENV: str(db_path)}),
            ):
                fixer_summaries = fixer_wire._load_fixer_resume_summaries(project_cwd)

        self.assertEqual([item.session_id for item in fixer_summaries], ["overseer-aliased"])


class LaunchFixerFlowTests(unittest.TestCase):
    def _load_available_servers(self) -> tuple[dict[str, dict[str, object]], dict[str, str], _FakeAdapter, object]:
        available = {fixer_wire.FORCED_MCP_SERVER: {"command": "fixer_mcp"}}
        return available, {}, _FakeAdapter(), (lambda _cwd: None)

    def test_launch_fixer_new_builds_fresh_codex_command(self) -> None:
        with (
            patch.dict(sys.modules, {"codex_pro_app.main": _fake_codex_main_module()}),
            patch.object(fixer_wire, "_load_available_servers", side_effect=lambda *_a, **_k: self._load_available_servers()),
            patch.object(fixer_wire, "_assert_project_is_registered", return_value=None),
            patch.object(fixer_wire, "_select_fixer_launch_action_interactive", return_value=fixer_wire.FIXER_LAUNCH_NEW),
            patch.object(
                fixer_wire,
                "_select_fresh_launch_selection",
                return_value=fixer_wire.SessionLaunchSelection("codex", "gpt-5.4", "medium"),
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
        self.assertEqual(cmd[0], "codex")
        self.assertNotIn("resume", cmd)
        self.assertIn("--prompt", cmd)
        self.assertIn("--mcp=fixer_mcp", cmd)
        self.assertIn("--model", cmd)
        model_index = cmd.index("--model")
        self.assertEqual(cmd[model_index + 1], "gpt-5.4")
        self.assertIn("--sandbox", cmd)
        self.assertIn("dangerous", cmd)

    def test_launch_fixer_new_builds_fresh_droid_command(self) -> None:
        def load_available_servers(*_args: object, **_kwargs: object) -> tuple[dict[str, dict[str, object]], dict[str, str], DroidBackendAdapter, object]:
            return ({fixer_wire.FORCED_MCP_SERVER: {"command": "fixer_mcp"}}, {}, DroidBackendAdapter(), (lambda _cwd: None))

        with (
            patch.dict(sys.modules, {"codex_pro_app.main": _fake_codex_main_module()}),
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

    def test_launch_fixer_resume_builds_resume_codex_command(self) -> None:
        summary = types.SimpleNamespace(
            session_id="resume-456",
            created=datetime(2026, 2, 1, 10, 0, tzinfo=timezone.utc),
            updated=datetime(2026, 2, 1, 12, 30, tzinfo=timezone.utc),
            preview="Fixer session",
        )
        with (
            patch.dict(sys.modules, {"codex_pro_app.main": _fake_codex_main_module()}),
            patch.object(fixer_wire, "_load_available_servers", side_effect=lambda *_a, **_k: self._load_available_servers()),
            patch.object(fixer_wire, "_assert_project_is_registered", return_value=None),
            patch.object(fixer_wire, "_select_fixer_launch_action_interactive", return_value=fixer_wire.FIXER_LAUNCH_RESUME),
            patch.object(fixer_wire, "_load_fixer_resume_summaries", return_value=[summary]),
            patch.object(fixer_wire, "_select_fixer_resume_session_interactive", return_value="resume-456"),
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
        self.assertEqual(cmd[-1], "resume-456")
        self.assertNotIn("--prompt", cmd)
        self.assertIn("--model", cmd)
        model_index = cmd.index("--model")
        self.assertEqual(cmd[model_index + 1], "gpt-5.4")
        self.assertIn("--sandbox", cmd)
        self.assertIn("dangerous", cmd)

    def test_launch_fixer_resume_latest_skips_interactive_picker(self) -> None:
        with (
            patch.dict(sys.modules, {"codex_pro_app.main": _fake_codex_main_module()}),
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


class PassthroughSandboxTests(unittest.TestCase):
    def test_adds_dangerous_sandbox_when_missing(self) -> None:
        resolved = fixer_wire._ensure_passthrough_dangerous_sandbox(["--foo"])
        self.assertEqual(resolved, ["--foo", "--sandbox", "danger-full-access"])

    def test_preserves_existing_sandbox_choice(self) -> None:
        original = ["--sandbox", "workspace-write", "--foo"]
        resolved = fixer_wire._ensure_passthrough_dangerous_sandbox(original)
        self.assertEqual(resolved, original)

    def test_prefer_fixed_model_for_role_presets(self) -> None:
        module = types.SimpleNamespace(
            MODEL_DISPLAY_ORDER=["gpt-5.3-codex", "gpt-5.2"],
            MODEL_DEFAULT_EFFORT={"gpt-5.3-codex": "medium"},
        )
        fixer_wire._prefer_fixed_model_for_role_presets(module)
        self.assertEqual(module.MODEL_DISPLAY_ORDER[0], "gpt-5.4")
        self.assertEqual(module.MODEL_DEFAULT_EFFORT["gpt-5.4"], "medium")


class SessionCodexLinkPersistenceTests(unittest.TestCase):
    def test_ensure_wire_schema_migrates_legacy_codex_links_into_external_link_table(self) -> None:
        conn = sqlite3.connect(":memory:")
        try:
            conn.executescript(
                """
                CREATE TABLE session (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    project_id INTEGER NOT NULL,
                    task_description TEXT NOT NULL,
                    status TEXT NOT NULL
                );
                CREATE TABLE session_codex_link (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    session_id INTEGER NOT NULL,
                    codex_session_id TEXT NOT NULL,
                    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
                );
                INSERT INTO session (id, project_id, task_description, status)
                VALUES (4, 1, 'Legacy session', 'review');
                INSERT INTO session_codex_link (session_id, codex_session_id)
                VALUES (4, 'legacy-codex-4');
                """
            )

            fixer_wire._ensure_wire_schema(conn)
            rows = conn.execute(
                """
                SELECT backend, external_session_id
                FROM session_external_link
                WHERE session_id = 4
                """
            ).fetchall()
        finally:
            conn.close()

        self.assertEqual(rows, [("codex", "legacy-codex-4")])

    def test_load_session_rows_prefers_latest_codex_link_for_legacy_duplicates(self) -> None:
        conn = sqlite3.connect(":memory:")
        try:
            conn.executescript(
                """
                CREATE TABLE session (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    project_id INTEGER NOT NULL,
                    task_description TEXT NOT NULL,
                    status TEXT NOT NULL
                );
                CREATE TABLE session_codex_link (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    session_id INTEGER NOT NULL,
                    codex_session_id TEXT NOT NULL,
                    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
                );
                INSERT INTO session (id, project_id, task_description, status)
                VALUES (7, 2, 'Repair resume flow', 'review');
                INSERT INTO session_codex_link (session_id, codex_session_id, updated_at)
                VALUES
                    (7, 'resume-old', '2026-03-01 09:00:00'),
                    (7, 'resume-new', '2026-03-01 10:00:00');
                """
            )

            fixer_wire._ensure_wire_schema(conn)
            rows = fixer_wire._load_session_rows(conn, 2)
        finally:
            conn.close()

        self.assertEqual(len(rows), 1)
        self.assertEqual(rows[0].codex_session_id, "resume-new")

    def test_load_session_external_id_falls_back_to_legacy_codex_link(self) -> None:
        conn = sqlite3.connect(":memory:")
        try:
            conn.executescript(
                """
                CREATE TABLE session (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    project_id INTEGER NOT NULL,
                    task_description TEXT NOT NULL,
                    status TEXT NOT NULL
                );
                CREATE TABLE session_codex_link (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    session_id INTEGER NOT NULL,
                    codex_session_id TEXT NOT NULL,
                    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
                );
                INSERT INTO session (id, project_id, task_description, status)
                VALUES (11, 2, 'Fallback session', 'review');
                INSERT INTO session_codex_link (session_id, codex_session_id)
                VALUES (11, 'legacy-fallback-11');
                """
            )

            fixer_wire._ensure_wire_schema(conn)
            external_session_id = fixer_wire._load_session_external_id(conn, 11, "codex")
        finally:
            conn.close()

        self.assertEqual(external_session_id, "legacy-fallback-11")

    def test_ensure_wire_schema_deduplicates_links_and_enforces_unique_session_mapping(self) -> None:
        conn = sqlite3.connect(":memory:")
        try:
            conn.executescript(
                """
                CREATE TABLE session_codex_link (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    session_id INTEGER NOT NULL,
                    codex_session_id TEXT NOT NULL,
                    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
                );
                INSERT INTO session_codex_link (session_id, codex_session_id, updated_at)
                VALUES
                    (9, 'resume-old', '2026-03-01 09:00:00'),
                    (9, 'resume-new', '2026-03-01 10:00:00');
                """
            )

            fixer_wire._ensure_wire_schema(conn)
            fixer_wire._save_session_codex_id(conn, 9, "resume-stable")
            rows = conn.execute(
                """
                SELECT codex_session_id
                FROM session_codex_link
                WHERE session_id = 9
                ORDER BY id
                """
            ).fetchall()
            indexes = conn.execute("PRAGMA index_list('session_codex_link')").fetchall()
        finally:
            conn.close()

        self.assertEqual(rows, [("resume-stable",)])
        self.assertTrue(any(index[2] for index in indexes))


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
                VALUES (1, 'sqlite', 1, 'DB', 'Use sqlite');
                INSERT INTO project_mcp_server (project_id, mcp_server_id) VALUES (1, 1);
                """
            )
        finally:
            conn.close()
        return tmp, db_path

    def _load_available_servers(self) -> tuple[dict[str, dict[str, object]], dict[str, str], _FakeAdapter, object]:
        available = {
            "sqlite": {"command": "sqlite"},
            fixer_wire.FORCED_MCP_SERVER: {"command": "fixer_mcp"},
        }
        return available, {"sqlite": "SQLITE_MCP_CONFIG"}, _FakeAdapter(), (lambda cwd: cwd / "sqliteMCP.toml")

    def _load_droid_available_servers(self) -> tuple[dict[str, dict[str, object]], dict[str, str], DroidBackendAdapter, object]:
        available = {
            fixer_wire.FORCED_MCP_SERVER: {"command": "fixer_mcp", "args": []},
        }
        return available, {}, DroidBackendAdapter(), (lambda _cwd: None)

    def test_launch_netrunner_resumes_non_pending_session_by_default(self) -> None:
        tmp, db_path = self._make_db()
        try:
            with (
                patch.dict(sys.modules, {"codex_pro_app.main": _fake_codex_main_module()}),
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
                    preset_mcp_names=["sqlite"],
                    dry_run=False,
                    Option=_DummyOption,
                    single_select_items=lambda *_a, **_k: None,
                    multi_select_items=lambda *_a, **_k: ["sqlite"],
                )
        finally:
            tmp.cleanup()

        self.assertEqual(code, 0)
        cmd = mock_call.call_args.args[0]
        self.assertEqual(cmd[0], "codex")
        self.assertEqual(cmd[1], "resume")
        self.assertEqual(cmd[-1], "resume-139")
        self.assertNotIn("--prompt", cmd)

    def test_launch_netrunner_closes_db_before_interactive_steps_and_subprocess(self) -> None:
        tmp, db_path = self._make_db()
        try:
            tracked_connections: list[_TrackingConnection] = []
            real_connect = sqlite3.connect

            def tracked_connect(*args: object, **kwargs: object) -> _TrackingConnection:
                conn = _TrackingConnection(real_connect(*args, **kwargs))
                tracked_connections.append(conn)
                return conn

            def choose_session(_options: list[_DummyOption], **_kwargs: object) -> int:
                self.assertTrue(tracked_connections)
                self.assertTrue(all(conn.closed for conn in tracked_connections))
                return 36

            def choose_mcp(_options: list[_DummyOption], **_kwargs: object) -> list[str]:
                self.assertTrue(tracked_connections)
                self.assertTrue(all(conn.closed for conn in tracked_connections))
                return ["sqlite"]

            def fake_call(_cmd: list[str], **_kwargs: object) -> int:
                self.assertTrue(tracked_connections)
                self.assertTrue(all(conn.closed for conn in tracked_connections))
                return 0

            with (
                patch.dict(sys.modules, {"codex_pro_app.main": _fake_codex_main_module()}),
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
                    preset_session_id=None,
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
        self.assertEqual(assigned_rows, [("fixer_mcp",), ("sqlite",)])
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
                patch.dict(sys.modules, {"codex_pro_app.main": _fake_codex_main_module()}),
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
                    single_select_items=lambda *_a, **_k: None,
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
                choose_calls["options"] = options
                choose_calls["kwargs"] = kwargs
                return "resume-fallback"

            with (
                patch.dict(sys.modules, {"codex_pro_app.main": _fake_codex_main_module()}),
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
                patch.dict(sys.modules, {"codex_pro_app.main": _fake_codex_main_module()}),
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
                    preset_model="gpt-5.3-codex",
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
        self.assertEqual(cmd[0], "droid")
        self.assertNotIn("exec", cmd)
        self.assertTrue(any("Activate skill `$manual-resolution` immediately." in item for item in cmd))
        self.assertEqual(session_row, ("droid", "gpt-5.3-codex", "medium"))
        self.assertEqual(external_link, [("droid", "droid-session-139")])


class LaunchOverseerFlowTests(unittest.TestCase):
    def test_launch_overseer_uses_selected_backend_and_forced_mcp(self) -> None:
        def load_available_servers(*_args: object, **_kwargs: object) -> tuple[dict[str, dict[str, object]], dict[str, str], DroidBackendAdapter, object]:
            available = {
                "project_tool": {"command": "project-tool", "_source": "project_mcp"},
                fixer_wire.FORCED_MCP_SERVER: {"command": "fixer_mcp"},
            }
            return available, {}, DroidBackendAdapter(), (lambda _cwd: None)

        with (
            patch.dict(sys.modules, {"codex_pro_app.main": _fake_codex_main_module()}),
            patch.object(fixer_wire, "_load_available_servers", side_effect=load_available_servers),
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


if __name__ == "__main__":
    unittest.main()
