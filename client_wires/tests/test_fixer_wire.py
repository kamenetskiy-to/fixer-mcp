from __future__ import annotations

import json
import os
import re
import sqlite3
import sys
import tempfile
import types
import unittest
from datetime import datetime, timezone
from pathlib import Path
from unittest.mock import patch

from client_wires import fixer_wire
from client_wires import backends as backends_pkg
from client_wires.backends import catalog as backends_catalog
from client_wires.backends.antigravity_adapter import (
    ANTIGRAVITY_MCP_TIMEOUT_ENV,
    ANTIGRAVITY_MCP_TIMEOUT_SECONDS,
    AntigravityBackendAdapter,
)
from client_wires.backends.base import (
    FIXER_RETIRED_SKILL_NAMES,
    FIXER_ROLE_SKILL_NAMES,
    materialize_antigravity_workspace_skills,
    materialize_claude_workspace_skills,
    materialize_codex_project_skills,
    materialize_factory_skills,
    materialize_junie_workspace_skills,
)
from client_wires.backends.claude_adapter import ClaudeCodeBackendAdapter
from client_wires.backends.codex_adapter import CodexBackendAdapter
from client_wires.backends.droid_adapter import DroidBackendAdapter
from client_wires.backends.junie_adapter import JunieBackendAdapter


class RoleSelectionTests(unittest.TestCase):
    def test_selector_includes_unattached_fixer(self) -> None:
        captured: dict[str, object] = {}

        def select(options: list[_DummyOption], **kwargs: object) -> str:
            captured["labels"] = [option.label for option in options]
            captured["values"] = [option.value for option in options]
            captured["preselected_value"] = kwargs["preselected_value"]
            return fixer_wire.UNATTACHED_FIXER_ACTION

        selected = fixer_wire._select_role_interactive(_DummyOption, select)

        self.assertEqual(selected, fixer_wire.UNATTACHED_FIXER_ACTION)
        self.assertIn("Unattached Fixer", captured["labels"])
        self.assertNotIn("MVP Scaffold", captured["labels"])
        self.assertIn("Fixer (Project)", captured["labels"])
        self.assertIn("Overseer (Global)", captured["labels"])
        self.assertIn(fixer_wire.UNATTACHED_FIXER_ACTION, captured["values"])
        self.assertEqual(captured["preselected_value"], "fixer")


class SkillMaterializationPruningTests(unittest.TestCase):
    def test_materializers_prune_only_retired_project_local_skill_dirs(self) -> None:
        materializers = [
            (".factory/skills", materialize_factory_skills),
            (".agents/skills", materialize_antigravity_workspace_skills),
            (".agents/skills", materialize_codex_project_skills),
            (".claude/skills", materialize_claude_workspace_skills),
            (".junie/fixer-runtime/skills", materialize_junie_workspace_skills),
        ]

        for relative_root, materialize in materializers:
            with self.subTest(materializer=materialize.__name__), tempfile.TemporaryDirectory() as tmp:
                cwd = Path(tmp)
                skill_root = cwd / relative_root
                retired_dir = skill_root / FIXER_RETIRED_SKILL_NAMES[0]
                unknown_dir = skill_root / "operator-private-skill"
                codex_home = cwd / "home" / ".codex"
                global_retired_dir = codex_home / "skills" / FIXER_RETIRED_SKILL_NAMES[0]
                retired_dir.mkdir(parents=True)
                unknown_dir.mkdir(parents=True)
                global_retired_dir.mkdir(parents=True)
                (retired_dir / "SKILL.md").write_text("# retired\n", encoding="utf-8")
                (unknown_dir / "SKILL.md").write_text("# private\n", encoding="utf-8")
                (global_retired_dir / "SKILL.md").write_text("# global retired\n", encoding="utf-8")

                with patch.dict(os.environ, {"CODEX_HOME": str(codex_home), "HOME": str(cwd / "home")}, clear=True):
                    materialize(cwd, ["init-fixer"])

                self.assertFalse(retired_dir.exists())
                self.assertTrue((unknown_dir / "SKILL.md").is_file())
                self.assertTrue((global_retired_dir / "SKILL.md").is_file())
                self.assertTrue((skill_root / "init-fixer" / "SKILL.md").is_file())


class NetrunnerWorkerModelPolicyTests(unittest.TestCase):
    ROOT = Path(__file__).resolve().parents[2]
    POLICY_LINES = (
        "- simplest tasks: `codex` + `gpt-5.6-luna` + `high`",
        "- medium-complexity tasks: `codex` + `gpt-5.6-terra` + `high`",
        "- complex tasks: `codex` + `gpt-5.6-sol` + `medium`",
        "- hardest tasks: `codex` + `gpt-5.6-sol` + `xhigh`",
    )
    ROLES = ("init-fixer", "init-unattached-fixer", "init-overseer")
    MATERIALIZERS = (
        (".factory/skills", materialize_factory_skills),
        (".claude/skills", materialize_claude_workspace_skills),
        (".junie/fixer-runtime/skills", materialize_junie_workspace_skills),
    )
    POLICY_PATTERN = re.compile(
        r"^- (?:simplest tasks|medium-complexity tasks|complex tasks|hardest tasks): .+$",
        re.MULTILINE,
    )

    def test_exact_four_tier_policy_is_synchronized(self) -> None:
        canonical_paths = [
            self.ROOT / ".agents/skills" / role / "SKILL.md"
            for role in self.ROLES
        ]
        canonical_paths.append(self.ROOT / "client_wires/README.md")
        canonical_contents = {
            path: path.read_text(encoding="utf-8") for path in canonical_paths
        }

        for path, content in canonical_contents.items():
            with self.subTest(path=path.relative_to(self.ROOT)):
                self._assert_policy_content(content)

        canonical_role_contents = {
            role: canonical_contents[self.ROOT / ".agents/skills" / role / "SKILL.md"]
            for role in self.ROLES
        }
        with tempfile.TemporaryDirectory() as tmp:
            temporary_root = Path(tmp)
            for relative_root, materialize in self.MATERIALIZERS:
                live_paths = [
                    self.ROOT / relative_root / role / "SKILL.md" for role in self.ROLES
                ]
                existing_live_paths = [path for path in live_paths if path.is_file()]

                if existing_live_paths:
                    self.assertEqual(
                        len(existing_live_paths),
                        len(self.ROLES),
                        f"partial materialized skill root: {relative_root}",
                    )
                    materialized_paths = existing_live_paths
                else:
                    materialize(temporary_root, self.ROLES)
                    materialized_paths = [
                        temporary_root / relative_root / role / "SKILL.md"
                        for role in self.ROLES
                    ]

                for role, path in zip(self.ROLES, materialized_paths):
                    with self.subTest(root=relative_root, role=role):
                        content = path.read_text(encoding="utf-8")
                        self._assert_policy_content(content)
                        self.assertEqual(content, canonical_role_contents[role])

    def _assert_policy_content(self, content: str) -> None:
        self.assertIn("netrunner worker", content.lower())
        self.assertEqual(tuple(self.POLICY_PATTERN.findall(content)), self.POLICY_LINES)
        self.assertNotIn(
            "- hardest tasks: `codex` + `gpt-5.6-luna` + `xhigh`",
            content,
        )
        self.assertNotIn("default reasoning effort: `xhigh`", content)
        self.assertNotIn("- use `gpt-5.6-luna` + `xhigh`", content)
        self.assertNotIn("- use `gpt-5.6-sol` + `xhigh`", content)


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


class ManualNetrunnerKindPickerTests(unittest.TestCase):
    def test_select_manual_netrunner_kind_defaults_to_regular(self) -> None:
        captured: dict[str, object] = {}

        def choose(options: list[_DummyOption], **kwargs: object) -> str:
            captured["options"] = options
            captured["kwargs"] = kwargs
            return fixer_wire.NETRUNNER_KIND_MANUAL

        selected = fixer_wire._select_manual_netrunner_kind_interactive(_DummyOption, choose)

        self.assertEqual(selected, fixer_wire.NETRUNNER_KIND_MANUAL)
        self.assertEqual(captured["kwargs"]["preselected_value"], fixer_wire.NETRUNNER_KIND_MANUAL)
        labels = [option.label for option in captured["options"] if not option.is_header]
        self.assertIn("Regular manual Netrunner [default]", labels)
        self.assertIn("Acceptance manual Netrunner", labels)

    def test_select_manual_netrunner_kind_can_choose_acceptance(self) -> None:
        selected = fixer_wire._select_manual_netrunner_kind_interactive(
            _DummyOption,
            lambda *_a, **_k: fixer_wire.NETRUNNER_KIND_ACCEPTANCE,
        )

        self.assertEqual(selected, fixer_wire.NETRUNNER_KIND_ACCEPTANCE)


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

    def test_build_netrunner_prompt_can_activate_acceptance_skill(self) -> None:
        prompt = fixer_wire._build_netrunner_prompt(
            72,
            ["playwright"],
            {"playwright": "Use for browser checks."},
            netrunner_kind=fixer_wire.NETRUNNER_KIND_ACCEPTANCE,
        )

        self.assertIn("Activate skill `$run-manual-acceptance-netrunner` immediately.", prompt)
        self.assertNotIn("Activate skill `$run-manual-netrunner` immediately.", prompt)

    def test_build_droid_netrunner_prompt_can_activate_acceptance_skill(self) -> None:
        prompt = fixer_wire._build_droid_netrunner_prompt(
            72,
            ["fixer_mcp"],
            netrunner_kind=fixer_wire.NETRUNNER_KIND_ACCEPTANCE,
        )

        self.assertIn("Activate skill `$run-manual-acceptance-netrunner` immediately.", prompt)
        self.assertIn("Assigned MCPs: fixer_mcp.", prompt)
        self.assertNotIn("FIXER MCP TOOL ACCESS", prompt)

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

    def test_append_droid_mcp_tool_guidance_stays_short_and_actionable(self) -> None:
        prompt = fixer_wire._append_droid_mcp_tool_guidance(
            "Start",
            backend="droid",
            mcp_names=["fixer_mcp", "playwright"],
        )

        self.assertEqual(prompt, "Start")
        self.assertNotIn("FIXER MCP TOOL ACCESS", prompt)
        self.assertNotIn("fixer_mcp___", prompt)
        self.assertNotIn("mcp_fixer_mcp_assume_role", prompt)
        self.assertNotIn("mandatory Droid MCP warm-up", prompt)

    def test_append_droid_mcp_tool_guidance_skips_codex(self) -> None:
        prompt = fixer_wire._append_droid_mcp_tool_guidance(
            "Start",
            backend="codex",
            mcp_names=["fixer_mcp"],
        )

        self.assertEqual(prompt, "Start")

    def test_build_droid_netrunner_prompt_is_short(self) -> None:
        prompt = fixer_wire._build_droid_netrunner_prompt(
            119,
            ["chrome-devtools", "fixer_mcp", "playwright"],
        )

        self.assertIn("Run the initialization checklist for session `119`", prompt)
        self.assertIn("Assigned MCPs: chrome-devtools, fixer_mcp, playwright.", prompt)
        self.assertNotIn("fixer_mcp___", prompt)
        self.assertNotIn("FIXER MCP TOOL ACCESS", prompt)
        self.assertNotIn("Attached MCP how-to guidance:", prompt)
        self.assertNotIn("Standard web stack guidance:", prompt)
        self.assertNotIn("mcp_fixer_mcp_", prompt)

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


class LaunchEnvTests(unittest.TestCase):
    def test_build_backend_launch_env_clears_proxy_for_codex_backend(self) -> None:
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
            db_path = cwd / "fixer.db"
            db_path.touch()
            with patch.dict(
                os.environ,
                {
                    "HOME": "/tmp/home",
                    "ALL_PROXY": "http://example.invalid:8080",
                    "HTTP_PROXY": "http://example.invalid:8080",
                    "HTTPS_PROXY": "http://example.invalid:8080",
                },
                clear=True,
            ):
                with patch.object(fixer_wire, "_resolve_fixer_db_path", return_value=db_path):
                    env = fixer_wire._build_backend_launch_env(
                        adapter,
                        object(),
                        cwd=cwd,
                        load_llm_env=lambda: {"OPENAI_API_KEY": "codex-only"},
                        merge_env_with_os=lambda payload: {"HOME": "/tmp/home", **payload},
                    )

            self.assertFalse((cwd / ".codex").exists())

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
        self.assertEqual(env["PREPARED"], "1")

    def test_build_backend_launch_env_clears_proxy_for_droid_backend(self) -> None:
        from client_wires.backends.droid_adapter import DroidBackendAdapter

        adapter = DroidBackendAdapter()
        with tempfile.TemporaryDirectory() as tmp:
            cwd = Path(tmp)
            db_path = cwd / "fixer.db"
            db_path.touch()
            with patch.dict(
                os.environ,
                {
                    "HOME": "/tmp/home",
                    "ALL_PROXY": "http://example.invalid:8080",
                    "HTTP_PROXY": "http://example.invalid:8080",
                    "HTTPS_PROXY": "http://example.invalid:8080",
                    "NO_PROXY": "127.0.0.1,localhost,::1",
                },
                clear=True,
            ):
                with patch.object(fixer_wire, "_resolve_fixer_db_path", return_value=db_path):
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
                    "ALL_PROXY": "http://example.invalid:8080",
                    "HTTP_PROXY": "http://example.invalid:8080",
                    "HTTPS_PROXY": "http://example.invalid:8080",
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

    def test_project_web_mcp_config_exposes_react_native_guide(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            repo_root = Path(tmp)
            config_path = repo_root / "webMCP.toml"
            config_path.write_text(
                '[mcp_servers.react-native-guide]\n'
                'command = "npx"\n'
                'args = ["-y", "@mrnitro360/react-native-mcp-guide@latest"]\n'
                'transport = "stdio"\n'
                'startup_timeout_sec = 120\n'
                'tool_timeout_sec = 600\n'
                'timeout = 600\n',
                encoding="utf-8",
            )

            servers = fixer_wire._load_project_web_mcp_servers(repo_root)

            self.assertIn("react-native-guide", servers)
            self.assertEqual(servers["react-native-guide"]["command"], "npx")
            self.assertEqual(servers["react-native-guide"]["args"], ["-y", "@mrnitro360/react-native-mcp-guide@latest"])
            self.assertEqual(servers["react-native-guide"]["transport"], "stdio")
            self.assertEqual(servers["react-native-guide"]["_source"], "project_mcp")
            self.assertEqual(servers["react-native-guide"]["_config_path"], str(config_path.resolve()))

    def test_load_available_servers_includes_repo_web_mcp_servers(self) -> None:
        repo_root = Path(__file__).resolve().parents[2]
        main_module = _fake_codex_main_module()
        config_loader = _fake_codex_config_loader_module(
            global_servers={"sqlite": {"command": "sqlite3"}},
        )
        runtime_module = types.ModuleType("client_wires.codex_compat.runtime")
        runtime_module._ensure_sqlite_scaffold = lambda _cwd: None
        codex_pro_package = types.ModuleType("client_wires.codex_compat")
        codex_pro_package.main = main_module
        codex_pro_package.config_loader = config_loader

        with (
            patch.dict(
                sys.modules,
                {
                    "client_wires.codex_compat": codex_pro_package,
                    "client_wires.codex_compat.llm": main_module,
                    "client_wires.codex_compat.config": config_loader,
                    "client_wires.codex_compat.runtime": runtime_module,
                },
            ),
            patch.object(fixer_wire, "_inject_research_query_server", side_effect=lambda servers, _cwd: servers),
            patch.object(fixer_wire, "_inject_figma_console_server", side_effect=lambda servers, _cwd: servers),
            patch.object(fixer_wire, "_inject_forced_fixer_server", side_effect=lambda servers: servers),
            patch.object(
                fixer_wire,
                "_load_project_web_mcp_servers",
                return_value={"react-native-guide": {"command": "npx", "args": ["-y", "@mrnitro360/react-native-mcp-guide@latest"]}},
            ),
        ):
            available, _config_env_vars, _adapter, _ensure_sqlite_scaffold = fixer_wire._load_available_servers(repo_root)

        self.assertIn("sqlite", available)
        self.assertIn("react-native-guide", available)
        self.assertEqual(available["react-native-guide"]["command"], "npx")
        self.assertEqual(available["react-native-guide"]["args"], ["-y", "@mrnitro360/react-native-mcp-guide@latest"])


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
        self.assertEqual(
            backend_names,
            ["codex", "droid", "claude", "antigravity", "junie", "kimi-code", "kimi-code-native"],
        )

    def test_available_backend_descriptors_includes_unsubscribed_backends(self) -> None:
        # available_backend_descriptors() is the structural "every backend
        # Fixer MCP knows how to drive" list — resume flows and name lookups
        # depend on this including backends the Architect isn't currently
        # subscribed to. subscribed_backend_descriptors() is the filtered one.
        descriptor = next(
            item for item in backends_pkg.available_backend_descriptors() if item.name == "droid"
        )
        self.assertFalse(descriptor.available)

    def test_subscribed_backend_descriptors_matches_current_architect_subscriptions(self) -> None:
        subscribed = {descriptor.name for descriptor in backends_pkg.subscribed_backend_descriptors()}
        self.assertEqual(subscribed, {"antigravity", "claude", "codex", "kimi-code-native"})

    def test_is_backend_available_reflects_catalog_flag(self) -> None:
        self.assertTrue(backends_pkg.is_backend_available("claude"))
        self.assertTrue(backends_pkg.is_backend_available("codex"))
        self.assertFalse(backends_pkg.is_backend_available("droid"))
        self.assertFalse(backends_pkg.is_backend_available("kimi-code"))
        self.assertTrue(backends_pkg.is_backend_available("kimi-code-native"))

    def test_set_backend_availability_round_trips_without_touching_real_catalog(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            temp_catalog = Path(tmp) / "backend-catalog.json"
            temp_catalog.write_text(
                json.dumps({"backends": {"codex": {"available": False}}}), encoding="utf-8"
            )
            with patch.object(backends_catalog, "_catalog_path", return_value=temp_catalog):
                backends_catalog.load_backend_catalog.cache_clear()
                self.assertFalse(backends_catalog.is_backend_available("codex"))
                backends_catalog.set_backend_availability("codex", True)
                self.assertTrue(backends_catalog.is_backend_available("codex"))
                persisted = json.loads(temp_catalog.read_text(encoding="utf-8"))
                self.assertTrue(persisted["backends"]["codex"]["available"])
            backends_catalog.load_backend_catalog.cache_clear()

    def test_codex_catalog_exposes_gpt_56_family_with_luna_default(self) -> None:
        descriptor = next(item for item in fixer_wire.available_backend_descriptors() if item.name == "codex")
        self.assertEqual(descriptor.default_model, "gpt-5.6-luna")
        self.assertEqual(descriptor.default_reasoning, "xhigh")
        self.assertEqual(
            descriptor.model_options,
            (
                "gpt-5.6-sol",
                "gpt-5.6-terra",
                "gpt-5.6-luna",
                "gpt-5.5",
                "gpt-5.4",
                "gpt-5.4-mini",
                "gpt-5.3-codex",
                "gpt-5.3-codex-spark",
                "gpt-5.2",
                "deepseek/deepseek-v4-flash-0731",
                "opencode-go/deepseek-v4-flash",
            ),
        )

    def test_droid_catalog_hides_legacy_qwen_model(self) -> None:
        descriptor = next(item for item in fixer_wire.available_backend_descriptors() if item.name == "droid")
        self.assertNotIn("OpenRouter Qwen3.6 Plus Free", descriptor.model_options)

    def test_droid_catalog_hides_legacy_owl_model(self) -> None:
        descriptor = next(item for item in fixer_wire.available_backend_descriptors() if item.name == "droid")
        self.assertNotIn("OpenRouter Owl Alpha Free", descriptor.model_options)

    def test_droid_catalog_rejects_legacy_preview_qwen_model(self) -> None:
        descriptor = next(item for item in fixer_wire.available_backend_descriptors() if item.name == "droid")
        with self.assertRaisesRegex(RuntimeError, "Supported models: kimi-k2.6, kimi-k2.7-code, glm-5.1"):
            fixer_wire._normalize_backend_model(descriptor, "custom:qwen/qwen3.6-plus-preview:free")

    def test_droid_catalog_rejects_legacy_raw_qwen_model(self) -> None:
        descriptor = next(item for item in fixer_wire.available_backend_descriptors() if item.name == "droid")
        with self.assertRaisesRegex(RuntimeError, "Supported models: kimi-k2.6, kimi-k2.7-code, glm-5.1"):
            fixer_wire._normalize_backend_model(descriptor, "custom:qwen/qwen3.6-plus:free")

    def test_droid_catalog_rejects_raw_owl_model(self) -> None:
        descriptor = next(item for item in fixer_wire.available_backend_descriptors() if item.name == "droid")
        with self.assertRaisesRegex(RuntimeError, "Supported models: kimi-k2.6, kimi-k2.7-code, glm-5.1"):
            fixer_wire._normalize_backend_model(descriptor, "openrouter/owl-alpha")

    def test_droid_catalog_migrates_broken_glm_51_model(self) -> None:
        descriptor = next(item for item in fixer_wire.available_backend_descriptors() if item.name == "droid")
        self.assertEqual(descriptor.default_model, "kimi-k2.6")
        self.assertEqual(descriptor.default_reasoning, "high")
        self.assertEqual(descriptor.model_options, ("kimi-k2.6", "kimi-k2.7-code", "glm-5.1"))
        self.assertEqual(
            fixer_wire._normalize_backend_model(descriptor, "kimi"),
            "kimi-k2.6",
        )
        self.assertEqual(
            fixer_wire._normalize_backend_model(descriptor, "glm-5.1"),
            "glm-5.1",
        )
        self.assertEqual(
            fixer_wire._normalize_backend_model(descriptor, "Z.AI GLM-5.1"),
            "glm-5.1",
        )
        self.assertEqual(
            fixer_wire._normalize_backend_model(descriptor, "Custom:glm-5.1-[z.ai]-0"),
            "glm-5.1",
        )

    def test_persist_session_launch_selection_normalizes_broken_droid_model(self) -> None:
        conn = sqlite3.connect(":memory:")
        try:
            conn.executescript(
                """
                CREATE TABLE session (
                    id INTEGER PRIMARY KEY,
                    cli_backend TEXT NOT NULL,
                    cli_model TEXT NOT NULL,
                    cli_reasoning TEXT NOT NULL
                );
                INSERT INTO session (id, cli_backend, cli_model, cli_reasoning)
                VALUES (41, 'droid', '', '');
                """
            )
            session_row = fixer_wire.SessionRow(
                session_id=4,
                global_session_id=41,
                task_description="Droid task",
                status="pending",
            )
            selection = fixer_wire.SessionLaunchSelection("droid", "glm-5.1", "none")

            resolved = fixer_wire._persist_session_launch_selection(conn, session_row, selection)
            stored = conn.execute("SELECT cli_backend, cli_model, cli_reasoning FROM session WHERE id = 41").fetchone()
        finally:
            conn.close()

        self.assertEqual(resolved.model, "glm-5.1")
        self.assertEqual(resolved.reasoning, "high")
        self.assertEqual(stored, ("droid", "glm-5.1", "high"))

    def test_persist_session_launch_selection_allows_pending_model_override(self) -> None:
        conn = sqlite3.connect(":memory:")
        try:
            conn.executescript(
                """
                CREATE TABLE session (
                    id INTEGER PRIMARY KEY,
                    cli_backend TEXT NOT NULL,
                    cli_model TEXT NOT NULL,
                    cli_reasoning TEXT NOT NULL
                );
                INSERT INTO session (id, cli_backend, cli_model, cli_reasoning)
                VALUES (41, 'droid', 'glm-5.1', 'none');
                """
            )
            session_row = fixer_wire.SessionRow(
                session_id=4,
                global_session_id=41,
                task_description="Droid task",
                status="pending",
                cli_backend="droid",
                cli_model="glm-5.1",
                cli_reasoning="none",
            )
            selection = fixer_wire.SessionLaunchSelection("droid", "custom:GLM-5.1-[Z.AI]-0", "high")

            resolved = fixer_wire._persist_session_launch_selection(conn, session_row, selection)
            stored = conn.execute("SELECT cli_backend, cli_model, cli_reasoning FROM session WHERE id = 41").fetchone()
        finally:
            conn.close()

        self.assertEqual(resolved, fixer_wire.SessionLaunchSelection("droid", "glm-5.1", "high"))
        self.assertEqual(stored, ("droid", "glm-5.1", "high"))

    def test_claude_catalog_excludes_droid_only_qwen_model(self) -> None:
        descriptor = next(item for item in fixer_wire.available_backend_descriptors() if item.name == "claude")
        self.assertNotIn("custom:Qwen3.6-Plus-Free-[OpenRouter]-0", descriptor.model_options)

    def test_claude_catalog_excludes_droid_only_owl_model(self) -> None:
        descriptor = next(item for item in fixer_wire.available_backend_descriptors() if item.name == "claude")
        self.assertNotIn("custom:Owl-Alpha-Free-[OpenRouter]-0", descriptor.model_options)

    def test_antigravity_backend_descriptor_is_available_with_agy_alias(self) -> None:
        descriptors = {item.name: item for item in fixer_wire.available_backend_descriptors()}

        self.assertIn("antigravity", descriptors)
        self.assertEqual(fixer_wire.normalize_backend_name("agy"), "antigravity")
        self.assertEqual(fixer_wire._backend_descriptor("agy").name, "antigravity")
        self.assertEqual(descriptors["antigravity"].default_model, "default")
        self.assertEqual(descriptors["antigravity"].default_reasoning, "default")
        self.assertIn("Gemini 3.5 Flash", descriptors["antigravity"].model_options)
        self.assertIn("Gemini 3.6 Flash", descriptors["antigravity"].model_options)
        self.assertIn("Claude Sonnet 4.6", descriptors["antigravity"].model_options)
        self.assertIn("high", descriptors["antigravity"].reasoning_options)

    def test_junie_backend_descriptor_exposes_droid_public_aliases(self) -> None:
        descriptors = {item.name: item for item in fixer_wire.available_backend_descriptors()}

        self.assertIn("junie", descriptors)
        self.assertEqual(descriptors["junie"].default_model, "kimi-k2.6")
        self.assertEqual(descriptors["junie"].default_reasoning, "default")
        self.assertEqual(descriptors["junie"].model_options, ("kimi-k2.6", "kimi-k2.7-code", "glm-5.1"))
        self.assertEqual(descriptors["junie"].reasoning_options, ("default",))

    def test_droid_resume_command_uses_short_session_flag(self) -> None:
        adapter = DroidBackendAdapter()
        command = adapter.build_resume_command([], "resume-42")
        self.assertEqual(command, ["droid", "--resume", "resume-42"])

    def test_droid_adapter_omits_exec_permission_flag_for_root_launches(self) -> None:
        adapter = DroidBackendAdapter()
        prefs = types.SimpleNamespace(dangerous_sandbox=True, auto_approve=True)
        self.assertEqual(adapter.build_execution_args(prefs), [])

    def test_droid_adapter_uses_auto_high_for_interactive_launches(self) -> None:
        adapter = DroidBackendAdapter()
        prefs = types.SimpleNamespace(dangerous_sandbox=True, auto_approve=True)
        self.assertEqual(adapter.build_interactive_execution_args(prefs), ["--auto", "high"])

    def test_droid_adapter_interactive_approve_flags_require_auto_approve(self) -> None:
        adapter = DroidBackendAdapter()
        prefs = types.SimpleNamespace(dangerous_sandbox=True, auto_approve=False)
        self.assertEqual(adapter.build_interactive_execution_args(prefs), [])

    def test_droid_adapter_builds_headless_autonomous_command_with_custom_kimi_default_model(self) -> None:
        adapter = DroidBackendAdapter()
        command = adapter.build_headless_command(
            model="kimi-k2.6",
            reasoning="none",
            selected={},
            available={},
            prompt="hello",
        )
        self.assertEqual(
            command,
            [
                "droid",
                "exec",
                "--skip-permissions-unsafe",
                "-m",
                "custom:Kimi-K2.6-[Kimi]-0",
                "-r",
                "high",
                "--output-format",
                "json",
                "hello",
            ],
        )

    def test_droid_adapter_still_builds_headless_autonomous_command_with_glm_model(self) -> None:
        adapter = DroidBackendAdapter()
        command = adapter.build_headless_command(
            model="glm-5.1",
            reasoning="high",
            selected={},
            available={},
            prompt="hello",
        )
        self.assertEqual(
            command,
            [
                "droid",
                "exec",
                "--skip-permissions-unsafe",
                "-m",
                "custom:GLM-5.1-[Z.AI]-0",
                "-r",
                "high",
                "--output-format",
                "json",
                "hello",
            ],
        )

    def test_droid_adapter_materializes_internal_model_only_in_factory_settings(self) -> None:
        adapter = DroidBackendAdapter()
        with tempfile.TemporaryDirectory() as tmp:
            cwd = Path(tmp)
            selection = types.SimpleNamespace(model="kimi-k2.6", reasoning_effort="")

            adapter.ensure_runtime_files(cwd, selection, selected={}, available={})

            settings = json.loads((cwd / ".factory" / "settings.json").read_text(encoding="utf-8"))

        self.assertEqual(
            settings["model"],
            "custom:Kimi-K2.6-[Kimi]-0",
        )
        self.assertEqual(settings["reasoningEffort"], "high")
        self.assertNotIn("model", settings.get("sessionDefaultSettings", {}))
        self.assertNotIn("reasoningEffort", settings.get("sessionDefaultSettings", {}))

    def test_claude_adapter_builds_fresh_launch_command(self) -> None:
        adapter = ClaudeCodeBackendAdapter()
        command = adapter.build_headless_command(
            model="sonnet",
            reasoning="medium",
            selected={},
            available={},
            prompt="hello",
        )
        self.assertEqual(
            command,
            [
                "claude",
                "--model",
                "sonnet",
                "--effort",
                "medium",
                "--dangerously-skip-permissions",
                "hello",
            ],
        )

    def test_claude_adapter_builds_resume_command(self) -> None:
        adapter = ClaudeCodeBackendAdapter()
        command = adapter.build_resume_command(["--model", "sonnet"], "session-123")
        self.assertEqual(command, ["claude", "--resume", "session-123", "--model", "sonnet"])

    def test_antigravity_adapter_builds_headless_prompt_command_without_model_flags(self) -> None:
        adapter = AntigravityBackendAdapter()
        command = adapter.build_headless_command(
            model="default",
            reasoning="default",
            selected={},
            available={},
            prompt="hello",
        )
        self.assertEqual(
            command,
            ["agy", "--dangerously-skip-permissions", "--print-timeout", "120m", "--print", "hello"],
        )

    def test_antigravity_adapter_builds_interactive_prompt_args_for_tui_launches(self) -> None:
        adapter = AntigravityBackendAdapter()

        self.assertEqual(adapter.build_prompt_args("hello"), ["--prompt-interactive", "hello"])
        self.assertEqual(adapter.build_prompt_args("   "), [])

    def test_antigravity_adapter_translates_codex_skill_marker_to_skill_name_request(self) -> None:
        adapter = AntigravityBackendAdapter()

        self.assertEqual(
            adapter.build_prompt_args("Activate skill `$init-fixer` immediately.\nRun init."),
            ["--prompt-interactive", "/init-fixer\nRun init."],
        )

    def test_antigravity_adapter_builds_interactive_model_args_from_observed_agy_models(self) -> None:
        adapter = AntigravityBackendAdapter()
        selection = types.SimpleNamespace(model="Gemini 3.5 Flash (High)", reasoning_effort="default")

        self.assertEqual(adapter.build_llm_args(selection), ["--model", "Gemini 3.5 Flash (High)"])

    def test_antigravity_adapter_maps_base_gemini_model_plus_reasoning_to_cli_model(self) -> None:
        adapter = AntigravityBackendAdapter()

        self.assertEqual(adapter._build_model_args("Gemini 3.5 Flash", "high"), ["--model", "Gemini 3.5 Flash (High)"])
        self.assertEqual(adapter._build_model_args("gemini-3.5-flash", "low"), ["--model", "Gemini 3.5 Flash (Low)"])
        self.assertEqual(adapter._build_model_args("Gemini 3.1 Pro", "high"), ["--model", "Gemini 3.1 Pro (High)"])

    def test_antigravity_adapter_maps_gemini_36_flash_with_reasoning(self) -> None:
        # `agy models` (the live CLI's own authoritative listing command, run
        # 2026-07-24) confirms gemini-3.6-flash-high/-medium/-low all exist,
        # matching the same three-tier shape as Gemini 3.5 Flash. Natural-
        # language self-report probing turned out to be an unreliable way to
        # confirm which tier actually got selected (even a request for the
        # already-proven "High" tier didn't always echo the tier name back),
        # so trust the deterministic `agy models` listing over that.
        adapter = AntigravityBackendAdapter()
        self.assertEqual(adapter._build_model_args("Gemini 3.6 Flash", "high"), ["--model", "Gemini 3.6 Flash (High)"])
        self.assertEqual(adapter._build_model_args("gemini-3.6-flash", "medium"), ["--model", "Gemini 3.6 Flash (Medium)"])
        self.assertEqual(adapter._build_model_args("gemini-3.6-flash", "low"), ["--model", "Gemini 3.6 Flash (Low)"])

    def test_antigravity_persisted_selection_keeps_fixer_model_reasoning_contract(self) -> None:
        with sqlite3.connect(":memory:") as conn:
            conn.executescript(
                """
                CREATE TABLE session (
                    id INTEGER PRIMARY KEY,
                    cli_backend TEXT NOT NULL,
                    cli_model TEXT NOT NULL,
                    cli_reasoning TEXT NOT NULL
                );
                INSERT INTO session (id, cli_backend, cli_model, cli_reasoning)
                VALUES (41, '', '', '');
                """
            )
            session_row = fixer_wire.SessionRow(
                session_id=41,
                global_session_id=41,
                task_description="Agy selection",
                status="pending",
            )
            selection = fixer_wire.SessionLaunchSelection(
                backend="agy",
                model="gemini-3.5-flash",
                reasoning="High",
            )

            resolved = fixer_wire._persist_session_launch_selection(conn, session_row, selection)
            stored = conn.execute("SELECT cli_backend, cli_model, cli_reasoning FROM session WHERE id = 41").fetchone()

        self.assertEqual(resolved, fixer_wire.SessionLaunchSelection("antigravity", "Gemini 3.5 Flash", "high"))
        self.assertEqual(stored, ("antigravity", "Gemini 3.5 Flash", "high"))

    def test_antigravity_persisted_selection_canonicalizes_legacy_cli_model_label(self) -> None:
        with sqlite3.connect(":memory:") as conn:
            conn.executescript(
                """
                CREATE TABLE session (
                    id INTEGER PRIMARY KEY,
                    cli_backend TEXT NOT NULL,
                    cli_model TEXT NOT NULL,
                    cli_reasoning TEXT NOT NULL
                );
                INSERT INTO session (id, cli_backend, cli_model, cli_reasoning)
                VALUES (42, '', '', '');
                """
            )
            session_row = fixer_wire.SessionRow(
                session_id=42,
                global_session_id=42,
                task_description="Agy legacy selection",
                status="pending",
            )
            selection = fixer_wire.SessionLaunchSelection(
                backend="agy",
                model="Gemini 3.5 Flash (High)",
                reasoning="default",
            )

            resolved = fixer_wire._persist_session_launch_selection(conn, session_row, selection)
            stored = conn.execute("SELECT cli_backend, cli_model, cli_reasoning FROM session WHERE id = 42").fetchone()

        self.assertEqual(resolved, fixer_wire.SessionLaunchSelection("antigravity", "Gemini 3.5 Flash", "high"))
        self.assertEqual(stored, ("antigravity", "Gemini 3.5 Flash", "high"))

    def test_antigravity_adapter_rejects_conflicting_embedded_and_requested_reasoning(self) -> None:
        adapter = AntigravityBackendAdapter()

        with self.assertRaisesRegex(RuntimeError, "conflicts"):
            adapter._build_model_args("Gemini 3.5 Flash (High)", "low")

    def test_antigravity_adapter_builds_headless_prompt_command_with_model_flag(self) -> None:
        adapter = AntigravityBackendAdapter()
        command = adapter.build_headless_command(
            model="Gemini 3.5 Flash (High)",
            reasoning="default",
            selected={},
            available={},
            prompt="hello",
        )
        self.assertEqual(
            command,
            [
                "agy",
                "--dangerously-skip-permissions",
                "--print-timeout",
                "120m",
                "--model",
                "Gemini 3.5 Flash (High)",
                "--print",
                "hello",
            ],
        )

    def test_antigravity_adapter_builds_headless_skill_prompt_with_skill_name_request(self) -> None:
        adapter = AntigravityBackendAdapter()
        command = adapter.build_headless_command(
            model="default",
            reasoning="default",
            selected={},
            available={},
            prompt="Activate skill `$run-manual-netrunner` immediately.\nUse headless mode.",
        )
        self.assertEqual(
            command,
            [
                "agy",
                "--dangerously-skip-permissions",
                "--print-timeout",
                "120m",
                "--print",
                "/run-manual-netrunner\nUse headless mode.",
            ],
        )

    def test_antigravity_adapter_patches_fixer_mcp_timeout_in_both_config_scopes(self) -> None:
        adapter = AntigravityBackendAdapter()
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            cwd = root / "workspace"
            user_config = root / "home" / "config" / "mcp_config.json"
            user_config.parent.mkdir(parents=True)
            user_config.write_text(
                json.dumps(
                    {
                        "mcpServers": {
                            "playwright": {"command": "keep-me"},
                        },
                        "otherSetting": True,
                    }
                ),
                encoding="utf-8",
            )
            selected = {"fixer_mcp": {"command": "/tmp/fixer_mcp"}}
            available = {"fixer_mcp": {"command": "/tmp/fixer_mcp", "transport": "stdio"}}

            with patch.dict(os.environ, {"FIXER_ANTIGRAVITY_MCP_CONFIG_PATH": str(user_config)}, clear=False):
                adapter.ensure_runtime_files(cwd, object(), selected, available)

            workspace_payload = json.loads((cwd / ".agents" / "mcp_config.json").read_text(encoding="utf-8"))
            user_payload = json.loads(user_config.read_text(encoding="utf-8"))

        self.assertEqual(
            workspace_payload["mcpServers"]["fixer_mcp"]["timeoutSeconds"],
            ANTIGRAVITY_MCP_TIMEOUT_SECONDS,
        )
        self.assertEqual(
            user_payload["mcpServers"]["fixer_mcp"]["timeoutSeconds"],
            ANTIGRAVITY_MCP_TIMEOUT_SECONDS,
        )
        self.assertEqual(user_payload["mcpServers"]["playwright"], {"command": "keep-me"})
        self.assertTrue(user_payload["otherSetting"])

    def test_antigravity_timeout_override_updates_mcp_and_print_timeout(self) -> None:
        adapter = AntigravityBackendAdapter()
        with tempfile.TemporaryDirectory() as tmp:
            cwd = Path(tmp)
            config_path = cwd / "user" / "mcp_config.json"
            with patch.dict(
                os.environ,
                {
                    ANTIGRAVITY_MCP_TIMEOUT_ENV: "901",
                    "FIXER_ANTIGRAVITY_MCP_CONFIG_PATH": str(config_path),
                },
                clear=False,
            ):
                command = adapter.build_headless_command(
                    model="default",
                    reasoning="default",
                    selected={},
                    available={},
                    prompt="hello",
                )
                adapter.ensure_runtime_files(
                    cwd,
                    object(),
                    {"fixer_mcp": {"command": "/tmp/fixer_mcp"}},
                    {"fixer_mcp": {"command": "/tmp/fixer_mcp"}},
                )

            payload = json.loads((cwd / ".agents" / "mcp_config.json").read_text(encoding="utf-8"))

        self.assertIn("--print-timeout", command)
        self.assertIn("901s", command)
        self.assertEqual(payload["mcpServers"]["fixer_mcp"]["timeoutSeconds"], 901)

    def test_antigravity_adapter_builds_resume_command_with_conversation_flag(self) -> None:
        adapter = AntigravityBackendAdapter()
        command = adapter.build_resume_command(["--dangerously-skip-permissions"], "conversation-123")
        self.assertEqual(command, ["agy", "--dangerously-skip-permissions", "--conversation", "conversation-123"])

    def test_antigravity_adapter_rejects_models_absent_from_observed_agy_models(self) -> None:
        adapter = AntigravityBackendAdapter()
        with self.assertRaisesRegex(RuntimeError, "Unsupported model"):
            adapter.build_headless_command(
                model="not-a-real-antigravity-model",
                reasoning="default",
                selected={},
                available={},
                prompt="hello",
            )

    def test_junie_adapter_builds_headless_custom_model_command_without_secret_args(self) -> None:
        adapter = JunieBackendAdapter()
        command = adapter.build_headless_command(
            model="glm-5.1",
            reasoning="default",
            selected={"fixer_mcp": {"command": "/tmp/fixer_mcp"}},
            available={},
            prompt="hello",
        )

        self.assertEqual(
            command,
            [
                "junie",
                "--model",
                "custom:glm-5.1",
                "--model-default-locations",
                "true",
                "--skill-location",
                ".junie/fixer-runtime/skills",
                "--skill-default-locations",
                "false",
                "--mcp-default-locations",
                "false",
                "--mcp-location",
                ".junie/fixer-runtime/mcp",
                "--output-format",
                "json",
                "--task",
                "hello",
            ],
        )
        self.assertNotIn("--openrouter-api-key", command)
        self.assertNotIn("--provider", command)

    def test_junie_adapter_does_not_copy_openrouter_env(self) -> None:
        adapter = JunieBackendAdapter()
        env = {"OPENROUTER_API_KEY": "secret-openrouter"}

        adapter.prepare_env(env, object())

        self.assertNotIn("JUNIE_OPENROUTER_API_KEY", env)

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


class ClaudeRuntimeMaterializationTests(unittest.TestCase):
    def test_claude_adapter_writes_mcp_and_workspace_skills(self) -> None:
        adapter = ClaudeCodeBackendAdapter()
        with tempfile.TemporaryDirectory() as tmp, tempfile.TemporaryDirectory() as codex_home_tmp:
            cwd = Path(tmp)
            codex_home = Path(codex_home_tmp)
            for skill_name in FIXER_ROLE_SKILL_NAMES:
                skill_dir = codex_home / "skills" / skill_name
                skill_dir.mkdir(parents=True, exist_ok=True)
                (skill_dir / "SKILL.md").write_text(
                    f"---\nname: {skill_name}\ndescription: test\n---\n# {skill_name}\n",
                    encoding="utf-8",
                )
            selection = types.SimpleNamespace(model="opus", reasoning_effort="medium")
            selected = {
                "fixer_mcp": {
                    "command": "/tmp/fixer_mcp",
                    "args": ["--serve"],
                    "env": {"TOKEN": "secret"},
                    "transport": "stdio",
                    "startup_timeout_sec": 30,
                    "timeout": 21600,
                    "tool_timeout_sec": 21600,
                },
            }
            selected = fixer_wire._bind_fixer_db_path_to_server_env(selected, db_path=cwd / "fixer.db")
            selected = fixer_wire._bind_netrunner_stateless_auth_to_server_env(selected, project_cwd=cwd)
            selected = fixer_wire._bind_locked_role_to_server_env(selected, role="fixer")
            available = {
                "fixer_mcp": {
                    "command": "/tmp/fixer_mcp",
                    "args": ["--serve"],
                    "env": {"PGPASSWORD": "fixer_password"},
                    "transport": "stdio",
                    "startup_timeout_sec": 30,
                    "timeout": 21600,
                    "tool_timeout_sec": 21600,
                },
            }
            legacy_skill_file = cwd / ".claude" / "skills" / "init-fixer.md"
            legacy_skill_file.parent.mkdir(parents=True, exist_ok=True)
            legacy_skill_file.write_text("# legacy flat skill\n", encoding="utf-8")

            with patch.dict(os.environ, {"CODEX_HOME": str(codex_home), "HOME": str(cwd / "home")}, clear=True):
                adapter.ensure_runtime_files(cwd, selection, selected, available)

            mcp_payload = json.loads((cwd / ".mcp.json").read_text(encoding="utf-8"))
            self.assertEqual(
                mcp_payload,
                {
                    "mcpServers": {
                        "fixer_mcp": {
                            "args": ["--serve"],
                            "command": "/tmp/fixer_mcp",
                            "env": {
                                "FIXER_DB_PATH": str(cwd / "fixer.db"),
                                "FIXER_MCP_DEFAULT_CWD": str(cwd.resolve()),
                                "FIXER_MCP_DEFAULT_ROLE": "netrunner",
                                "FIXER_MCP_LOCKED_ROLE": "fixer",
                                "PGPASSWORD": "fixer_password",
                                "TOKEN": "secret",
                            },
                            "startup_timeout_sec": 30,
                            "timeout": 21600000,
                            "transport": "stdio",
                        },
                    },
                },
            )
            self.assertTrue((cwd / ".claude" / "skills" / "init-fixer" / "SKILL.md").is_file())
            self.assertTrue((cwd / ".claude" / "skills" / "run-manual-netrunner" / "SKILL.md").is_file())
            self.assertTrue((cwd / ".claude" / "skills" / "complete-netrunner-session" / "SKILL.md").is_file())
            self.assertTrue((cwd / ".claude" / "skills" / "maintain-project-docs" / "SKILL.md").is_file())
            self.assertTrue((cwd / ".claude" / "skills" / "run-netrunner-wave" / "SKILL.md").is_file())
            self.assertTrue((cwd / ".claude" / "skills" / "inspect-netrunner-transcript" / "SKILL.md").is_file())
            self.assertFalse((cwd / ".claude" / "skills" / "init-fixer.md").exists())

    def test_claude_adapter_writes_tool_timeout_in_milliseconds(self) -> None:
        adapter = ClaudeCodeBackendAdapter()
        with tempfile.TemporaryDirectory() as tmp, tempfile.TemporaryDirectory() as codex_home_tmp:
            cwd = Path(tmp)
            codex_home = Path(codex_home_tmp)
            for skill_name in FIXER_ROLE_SKILL_NAMES:
                skill_dir = codex_home / "skills" / skill_name
                skill_dir.mkdir(parents=True, exist_ok=True)
                (skill_dir / "SKILL.md").write_text(
                    f"---\nname: {skill_name}\ndescription: test\n---\n# {skill_name}\n",
                    encoding="utf-8",
                )
            selection = types.SimpleNamespace(model="sonnet", reasoning_effort="medium")
            selected = {
                "fixer_mcp": {
                    "command": "/tmp/fixer_mcp",
                    "per_tool_timeout_ms": fixer_wire.FORCED_FIXER_MCP_TIMEOUT_FLOOR_MS,
                    "timeout": fixer_wire.FORCED_FIXER_MCP_TIMEOUT_FLOOR_SEC,
                    "tool_timeout_sec": fixer_wire.FORCED_FIXER_MCP_TIMEOUT_FLOOR_SEC,
                },
                "project_tool": {
                    "command": "project-tool",
                    "tool_timeout_sec": 300,
                },
                "fallback_tool": {
                    "command": "fallback-tool",
                    "timeout": 45,
                },
            }

            with patch.dict(os.environ, {"CODEX_HOME": str(codex_home), "HOME": str(cwd / "home")}, clear=True):
                adapter.ensure_runtime_files(cwd, selection, selected, available={})

            servers = json.loads((cwd / ".mcp.json").read_text(encoding="utf-8"))["mcpServers"]
            self.assertEqual(servers["fixer_mcp"]["timeout"], fixer_wire.FORCED_FIXER_MCP_TIMEOUT_FLOOR_MS)
            self.assertEqual(servers["project_tool"]["timeout"], 300000)
            self.assertEqual(servers["fallback_tool"]["timeout"], 45000)
            self.assertNotIn("tool_timeout_sec", servers["fixer_mcp"])
            self.assertNotIn("per_tool_timeout_ms", servers["fixer_mcp"])


class AntigravityRuntimeMaterializationTests(unittest.TestCase):
    def test_antigravity_adapter_writes_agents_mcp_and_workspace_skills(self) -> None:
        adapter = AntigravityBackendAdapter()
        with tempfile.TemporaryDirectory() as tmp, tempfile.TemporaryDirectory() as codex_home_tmp:
            cwd = Path(tmp)
            codex_home = Path(codex_home_tmp)
            for skill_name in FIXER_ROLE_SKILL_NAMES:
                skill_dir = codex_home / "skills" / skill_name
                skill_dir.mkdir(parents=True, exist_ok=True)
                (skill_dir / "SKILL.md").write_text(
                    f"---\nname: {skill_name}\ndescription: test\n---\n# {skill_name}\n",
                    encoding="utf-8",
                )
            selection = types.SimpleNamespace(model="default", reasoning_effort="default")
            selected = {
                "fixer_mcp": {
                    "command": "/tmp/fixer_mcp",
                    "args": ["--serve"],
                    "env": {"TOKEN": "secret"},
                },
                "remote-search": {
                    "url": "https://example.test/mcp",
                    "headers": {"X-Test": "yes"},
                },
            }
            selected = fixer_wire._bind_fixer_db_path_to_server_env(selected, db_path=cwd / "fixer.db")
            selected = fixer_wire._bind_netrunner_stateless_auth_to_server_env(selected, project_cwd=cwd)
            selected = fixer_wire._bind_locked_role_to_server_env(selected, role="netrunner")
            legacy_skill_file = cwd / ".agents" / "skills" / "init-fixer.md"
            legacy_skill_file.parent.mkdir(parents=True, exist_ok=True)
            legacy_skill_file.write_text("# legacy flat skill\n", encoding="utf-8")
            user_mcp_path = cwd / "home" / ".gemini" / "config" / "mcp_config.json"
            user_mcp_path.parent.mkdir(parents=True, exist_ok=True)
            user_mcp_path.write_text(
                json.dumps({"mcpServers": {"personal_tool": {"command": "/tmp/personal"}}}) + "\n",
                encoding="utf-8",
            )

            with patch.dict(
                os.environ,
                {
                    "CODEX_HOME": str(codex_home),
                    "HOME": str(cwd / "home"),
                    "FIXER_ANTIGRAVITY_MCP_CONFIG_PATH": str(user_mcp_path),
                },
                clear=True,
            ):
                adapter.ensure_runtime_files(cwd, selection, selected, available={})

            mcp_payload = json.loads((cwd / ".agents" / "mcp_config.json").read_text(encoding="utf-8"))
            user_mcp_payload = json.loads(user_mcp_path.read_text(encoding="utf-8"))
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
                                "FIXER_MCP_LOCKED_ROLE": "netrunner",
                                "TOKEN": "secret",
                            },
                            "timeoutSeconds": ANTIGRAVITY_MCP_TIMEOUT_SECONDS,
                        },
                        "remote-search": {
                            "disabled": False,
                            "headers": {"X-Test": "yes"},
                            "serverUrl": "https://example.test/mcp",
                        },
                    },
                },
            )
            self.assertEqual(user_mcp_payload["mcpServers"]["personal_tool"], {"command": "/tmp/personal"})
            self.assertEqual(user_mcp_payload["mcpServers"]["fixer_mcp"], mcp_payload["mcpServers"]["fixer_mcp"])
            self.assertEqual(user_mcp_payload["mcpServers"]["remote-search"], mcp_payload["mcpServers"]["remote-search"])
            self.assertTrue((cwd / ".agents" / "skills" / "init-fixer" / "SKILL.md").is_file())
            self.assertTrue((cwd / ".agents" / "skills" / "run-manual-netrunner" / "SKILL.md").is_file())
            self.assertTrue((cwd / ".agents" / "skills" / "complete-netrunner-session" / "SKILL.md").is_file())
            self.assertTrue((cwd / ".agents" / "skills" / "maintain-project-docs" / "SKILL.md").is_file())
            self.assertTrue((cwd / ".agents" / "skills" / "run-netrunner-wave" / "SKILL.md").is_file())
            self.assertTrue((cwd / ".agents" / "skills" / "inspect-netrunner-transcript" / "SKILL.md").is_file())
            self.assertFalse((cwd / ".agents" / "skills" / "init-fixer.md").exists())
            self.assertFalse((cwd / ".agents" / "skills" / "missing-skill").exists())


class JunieRuntimeMaterializationTests(unittest.TestCase):
    def test_junie_adapter_writes_mcp_and_workspace_skills_without_model_secrets(self) -> None:
        adapter = JunieBackendAdapter()
        with tempfile.TemporaryDirectory() as tmp, tempfile.TemporaryDirectory() as codex_home_tmp:
            cwd = Path(tmp)
            codex_home = Path(codex_home_tmp)
            for skill_name in FIXER_ROLE_SKILL_NAMES:
                skill_dir = codex_home / "skills" / skill_name
                skill_dir.mkdir(parents=True, exist_ok=True)
                (skill_dir / "SKILL.md").write_text(
                    f"---\nname: {skill_name}\ndescription: test\n---\n# {skill_name}\n",
                    encoding="utf-8",
                )
            selection = types.SimpleNamespace(model="kimi-k2.6", reasoning_effort="default")
            selected = {
                "fixer_mcp": {
                    "command": "/tmp/fixer_mcp",
                    "args": ["--serve"],
                    "env": {"TOKEN": "secret"},
                },
                "remote-search": {
                    "serverUrl": "https://example.test/mcp",
                    "headers": {"X-Test": "yes"},
                },
            }
            selected = fixer_wire._bind_fixer_db_path_to_server_env(selected, db_path=cwd / "fixer.db")
            selected = fixer_wire._bind_netrunner_stateless_auth_to_server_env(selected, project_cwd=cwd)
            selected = fixer_wire._bind_locked_role_to_server_env(selected, role="netrunner")

            with patch.dict(os.environ, {"CODEX_HOME": str(codex_home), "HOME": str(cwd / "home")}, clear=True):
                adapter.ensure_runtime_files(cwd, selection, selected, available={})

            mcp_payload = json.loads(
                (cwd / ".junie" / "fixer-runtime" / "mcp" / "mcp.json").read_text(encoding="utf-8")
            )
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
                                "FIXER_MCP_LOCKED_ROLE": "netrunner",
                                "TOKEN": "secret",
                            },
                        },
                        "remote-search": {
                            "disabled": False,
                            "headers": {"X-Test": "yes"},
                            "url": "https://example.test/mcp",
                        },
                    },
                },
            )
            self.assertFalse((cwd / ".junie" / "models").exists())
            self.assertFalse((cwd / ".junie" / "mcp").exists())
            self.assertFalse((cwd / ".junie" / "skills").exists())
            self.assertTrue((cwd / ".junie" / "fixer-runtime" / "skills" / "init-fixer" / "SKILL.md").is_file())
            self.assertTrue(
                (cwd / ".junie" / "fixer-runtime" / "skills" / "run-manual-netrunner" / "SKILL.md").is_file()
            )
            self.assertTrue(
                (cwd / ".junie" / "fixer-runtime" / "skills" / "complete-netrunner-session" / "SKILL.md").is_file()
            )


class DroidRuntimeMaterializationTests(unittest.TestCase):
    def test_droid_adapter_writes_factory_mcp_and_skills(self) -> None:
        adapter = DroidBackendAdapter()
        with tempfile.TemporaryDirectory() as tmp, tempfile.TemporaryDirectory() as codex_home_tmp:
            cwd = Path(tmp)
            codex_home = Path(codex_home_tmp)
            for skill_name in FIXER_ROLE_SKILL_NAMES:
                skill_dir = codex_home / "skills" / skill_name
                skill_dir.mkdir(parents=True, exist_ok=True)
                (skill_dir / "SKILL.md").write_text(f"# {skill_name}\n", encoding="utf-8")
            selection = types.SimpleNamespace(model="kimi-k2.6", reasoning_effort="medium")
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
            available = {
                "fixer_mcp": {
                    "command": "/tmp/fixer_mcp",
                    "args": ["--serve"],
                    "env": {"FIXER_MCP_LOCKED_ROLE": "fixer"},
                    "transport": "stdio",
                },
                "apify": {
                    "url": "https://mcp.apify.com",
                    "bearer_token_env_var": "APIFY_TOKEN",
                },
            }
            selected = fixer_wire._bind_fixer_db_path_to_server_env(
                selected,
                db_path=cwd / "fixer.db",
            )
            selected = fixer_wire._bind_netrunner_stateless_auth_to_server_env(
                selected,
                project_cwd=cwd,
            )
            selected = fixer_wire._bind_locked_role_to_server_env(selected, role="netrunner")
            selected["apify"] = dict(available["apify"])

            with patch.dict(
                os.environ,
                {"CODEX_HOME": str(codex_home), "APIFY_TOKEN": "apify-token-123", "Z_AI_API_KEY": "test-zai-key"},
                clear=False,
            ):
                adapter.ensure_runtime_files(cwd, selection, selected, available)

            mcp_payload = json.loads((cwd / ".factory" / "mcp.json").read_text(encoding="utf-8"))
            self.assertEqual(
                mcp_payload,
                {
                    "mcpServers": {
                        "apify": {
                            "disabled": False,
                            "headers": {"Authorization": "Bearer" + " apify-token-123"},
                            "type": "http",
                            "url": "https://mcp.apify.com",
                        },
                        "fixer_mcp": {
                            "args": ["--serve"],
                            "command": "/tmp/fixer_mcp",
                            "disabled": False,
                            "env": {
                                "FIXER_DB_PATH": str(cwd / "fixer.db"),
                                "FIXER_MCP_DEFAULT_CWD": str(cwd.resolve()),
                                "FIXER_MCP_DEFAULT_ROLE": "netrunner",
                                "FIXER_MCP_LOCKED_ROLE": "netrunner",
                                "TOKEN": "secret",
                            },
                            "type": "stdio",
                        },
                    }
                },
            )
            settings_payload = json.loads((cwd / ".factory" / "settings.json").read_text(encoding="utf-8"))
            self.assertEqual(settings_payload["model"], "custom:Kimi-K2.6-[Kimi]-0")
            self.assertEqual(settings_payload["reasoningEffort"], "medium")
            self.assertNotIn("model", settings_payload.get("sessionDefaultSettings", {}))
            self.assertNotIn("reasoningEffort", settings_payload.get("sessionDefaultSettings", {}))
            self.assertTrue((cwd / ".factory" / "skills" / "init-fixer" / "SKILL.md").is_file())
            self.assertTrue((cwd / ".factory" / "skills" / "init-unattached-fixer" / "SKILL.md").is_file())
            self.assertTrue((cwd / ".factory" / "skills" / "run-manual-acceptance-netrunner" / "SKILL.md").is_file())
            self.assertTrue((cwd / ".factory" / "skills" / "run-manual-netrunner" / "SKILL.md").is_file())
            self.assertTrue((cwd / ".factory" / "skills" / "review-netrunner-session" / "SKILL.md").is_file())
            self.assertTrue((cwd / ".factory" / "skills" / "maintain-project-docs" / "SKILL.md").is_file())
            self.assertTrue((cwd / ".factory" / "skills" / "run-netrunner-wave" / "SKILL.md").is_file())
            self.assertTrue((cwd / ".factory" / "skills" / "inspect-netrunner-transcript" / "SKILL.md").is_file())

    def test_droid_adapter_does_not_force_add_zai_servers(self) -> None:
        adapter = DroidBackendAdapter()
        with tempfile.TemporaryDirectory() as tmp:
            cwd = Path(tmp)
            selection = types.SimpleNamespace(model="kimi-k2.6", reasoning_effort="medium")

            with patch.dict(os.environ, {"Z_AI_API_KEY": "test-zai-key"}, clear=False):
                adapter.ensure_runtime_files(cwd, selection, selected={}, available={})

            mcp_payload = json.loads((cwd / ".factory" / "mcp.json").read_text(encoding="utf-8"))

        self.assertEqual(mcp_payload["mcpServers"], {})
        self.assertNotIn("zai-mcp-server", mcp_payload["mcpServers"])
        self.assertNotIn("web-search-prime", mcp_payload["mcpServers"])


class CodexBackendAdapterTests(unittest.TestCase):
    def test_deepseek_opencode_model_maps_to_raw_id_and_portable_provider_config(self) -> None:
        captured: dict[str, object] = {}

        class Inner:
            command = "codex"
            supports_resume = True

            @staticmethod
            def build_llm_args(selection: object) -> list[str]:
                captured["model"] = getattr(selection, "model")
                return ["--model", str(getattr(selection, "model"))]

            @staticmethod
            def build_execution_args(_prefs: object) -> list[str]:
                return []

            @staticmethod
            def build_mcp_flags(_selected: dict[str, object], _available: dict[str, object]) -> list[str]:
                return []

            @staticmethod
            def build_prompt_args(_prompt: str) -> list[str]:
                return []

            @staticmethod
            def prepare_env(_env: dict[str, str], _selection: object) -> None:
                return None

        selection = types.SimpleNamespace(
            model="opencode-go/deepseek-v4-flash",
            reasoning_effort="high",
        )
        args = CodexBackendAdapter(Inner()).build_llm_args(selection)

        self.assertEqual(captured["model"], "deepseek-v4-flash")
        self.assertIn('model_provider="opencode_go"', args)
        self.assertIn('model_providers.opencode_go.env_key="OPENCODE_GO_API_KEY"', args)
        catalog_arg = next(value for value in args if value.startswith("model_catalog_json="))
        catalog_path = Path(json.loads(catalog_arg.split("=", 1)[1]))
        self.assertTrue(catalog_path.is_file())

    def test_deepseek_opencode_headless_wave_command_uses_same_provider_config(self) -> None:
        class Inner:
            command = "codex"
            supports_resume = True

            @staticmethod
            def build_mcp_flags(_selected: dict[str, object], _available: dict[str, object]) -> list[str]:
                return []

            @staticmethod
            def build_llm_args(_selection: object) -> list[str]:
                return []

            @staticmethod
            def build_execution_args(_prefs: object) -> list[str]:
                return []

            @staticmethod
            def build_prompt_args(_prompt: str) -> list[str]:
                return []

            @staticmethod
            def prepare_env(_env: dict[str, str], _selection: object) -> None:
                return None

        command = CodexBackendAdapter(Inner()).build_headless_command(
            model="opencode-go/deepseek-v4-flash",
            reasoning="high",
            selected={},
            available={},
            prompt="create a file",
        )

        self.assertEqual(command[:3], ["codex", "--model", "deepseek-v4-flash"])
        self.assertIn('model_provider="opencode_go"', command)
        self.assertIn("exec", command)
        self.assertEqual(command[-1], "create a file")

    def test_codex_adapter_materializes_project_local_fixer_skills(self) -> None:
        class Inner:
            command = "codex"
            supports_resume = True

            @staticmethod
            def build_mcp_flags(_selected: dict[str, object], _available: dict[str, object]) -> list[str]:
                return []

            @staticmethod
            def build_llm_args(_selection: object) -> list[str]:
                return []

            @staticmethod
            def build_execution_args(_prefs: object) -> list[str]:
                return []

            @staticmethod
            def build_prompt_args(_prompt: str) -> list[str]:
                return []

            @staticmethod
            def prepare_env(_env: dict[str, str], _selection: object) -> None:
                return None

        adapter = CodexBackendAdapter(Inner())
        with tempfile.TemporaryDirectory() as tmp:
            cwd = Path(tmp)

            adapter.ensure_runtime_files(cwd, object(), selected={}, available={})

            self.assertTrue((cwd / ".agents" / "skills" / "init-fixer" / "SKILL.md").is_file())
            self.assertTrue((cwd / ".agents" / "skills" / "run-manual-netrunner" / "SKILL.md").is_file())
            self.assertTrue((cwd / ".agents" / "skills" / "complete-netrunner-session" / "SKILL.md").is_file())

    def test_build_mcp_flags_uses_selected_specs_as_available_overrides(self) -> None:
        class Inner:
            command = "codex"
            supports_resume = True

            @staticmethod
            def build_mcp_flags(selected: dict[str, object], available: dict[str, object]) -> list[str]:
                env = available["fixer_mcp"]["env"]  # type: ignore[index]
                return [f"LOCKED={env['FIXER_MCP_LOCKED_ROLE']}", f"DB={env['FIXER_DB_PATH']}"]

            @staticmethod
            def build_llm_args(_selection: object) -> list[str]:
                return []

            @staticmethod
            def build_execution_args(_prefs: object) -> list[str]:
                return []

            @staticmethod
            def build_prompt_args(_prompt: str) -> list[str]:
                return []

            @staticmethod
            def prepare_env(_env: dict[str, str], _selection: object) -> None:
                return None

        adapter = CodexBackendAdapter(Inner())
        flags = adapter.build_mcp_flags(
            selected={
                "fixer_mcp": {
                    "command": "/tmp/fixer_mcp",
                    "env": {
                        "FIXER_DB_PATH": "/tmp/fixer.db",
                        "FIXER_MCP_LOCKED_ROLE": "fixer",
                    },
                },
            },
            available={
                "fixer_mcp": {
                    "command": "/tmp/fixer_mcp",
                    "env": {"PGPASSWORD": "fixer_password"},
                },
            },
        )

        self.assertEqual(flags, ["LOCKED=fixer", "DB=/tmp/fixer.db"])

    def test_build_mcp_flags_adds_narrow_direct_fixer_gate(self) -> None:
        captured: dict[str, object] = {}

        class Inner:
            command = "codex"
            supports_resume = True

            @staticmethod
            def build_mcp_flags(selected: dict[str, object], available: dict[str, object]) -> list[str]:
                captured["selected"] = selected
                captured["available"] = available
                return ["INNER_FLAGS"]

            @staticmethod
            def build_llm_args(_selection: object) -> list[str]:
                return []

            @staticmethod
            def build_execution_args(_prefs: object) -> list[str]:
                return []

            @staticmethod
            def build_prompt_args(_prompt: str) -> list[str]:
                return []

            @staticmethod
            def prepare_env(_env: dict[str, str], _selection: object) -> None:
                return None

        adapter = CodexBackendAdapter(Inner())
        flags = adapter.build_mcp_flags(
            selected={
                "fixer_mcp": {
                    "command": "/tmp/fixer_mcp",
                    "env": {
                        "FIXER_DB_PATH": "/tmp/fixer.db",
                        "FIXER_MCP_DEFAULT_CWD": "/tmp/project",
                        "FIXER_MCP_DEFAULT_ROLE": "fixer",
                        "FIXER_MCP_LOCKED_ROLE": "fixer",
                    },
                    "timeout": 21600,
                    "tool_timeout_sec": 21600,
                },
            },
            available={"fixer_mcp": {"command": "/stale/fixer_mcp"}},
        )

        selected = captured["selected"]
        self.assertEqual(set(selected), {"fixer_mcp", "fixer_netrunner_gate"})  # type: ignore[arg-type]
        gate = selected["fixer_netrunner_gate"]  # type: ignore[index]
        self.assertEqual(gate["command"], "/tmp/fixer_mcp")
        self.assertEqual(gate["timeout"], 21600)
        self.assertEqual(gate["tool_timeout_sec"], 21600)
        self.assertEqual(gate["env"]["FIXER_MCP_AUTO_AUTH"], "1")
        self.assertEqual(gate["env"]["FIXER_MCP_TOOL_PROFILE"], "netrunner_gate")
        self.assertEqual(
            flags,
            [
                "INNER_FLAGS",
                "-c",
                'mcp_servers.fixer_netrunner_gate.enabled_tools=["launch_netrunner_wave","wait_for_netrunner_wave"]',
                "-c",
                'mcp_servers.fixer_mcp.disabled_tools=["launch_netrunner_wave","wait_for_netrunner_wave"]',
                "-c",
                'features.code_mode.direct_only_tool_namespaces=["mcp__fixer_netrunner_gate"]',
            ],
        )


class FixerResumeUiTests(unittest.TestCase):
    def test_select_fixer_resume_requires_sessions(self) -> None:
        with self.assertRaises(RuntimeError):
            fixer_wire._select_fixer_resume_session_interactive([], _DummyOption, lambda *_a, **_k: None)

    def test_select_fixer_resume_label_uses_provider_preview_timestamp_id_columns(self) -> None:
        summary = types.SimpleNamespace(
            session_id="session-123",
            provider="codex",
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
        self.assertTrue(row_label.startswith("Codex CLI"))
        self.assertIn("Implement fixer resume flow", row_label)
        self.assertIn(summary.created.astimezone().strftime("%Y-%m-%d %H:%M"), row_label)
        self.assertIn(summary.updated.astimezone().strftime("%Y-%m-%d %H:%M"), row_label)
        self.assertTrue(row_label.endswith("session-123"))

    def test_select_fixer_resume_returns_backend_qualified_non_codex_value(self) -> None:
        summary = types.SimpleNamespace(
            session_id="claude-session-123",
            provider="claude",
            created=datetime(2026, 2, 1, 10, 0, tzinfo=timezone.utc),
            updated=datetime(2026, 2, 1, 12, 30, tzinfo=timezone.utc),
            preview="Claude fixer flow",
        )
        captured: dict[str, object] = {}

        def choose(options: list[_DummyOption], **kwargs: object) -> str:
            captured["options"] = options
            captured["kwargs"] = kwargs
            return "claude:claude-session-123"

        selected = fixer_wire._select_fixer_resume_session_interactive([summary], _DummyOption, choose)

        self.assertEqual(selected, "claude:claude-session-123")
        self.assertEqual(captured["kwargs"]["preselected_value"], "claude:claude-session-123")
        self.assertTrue(captured["options"][1].label.startswith("Claude Code"))


class MainDispatchTests(unittest.TestCase):
    def test_main_dispatches_unattached_fixer_action(self) -> None:
        fake_package = types.ModuleType("client_wires.codex_compat")
        fake_ui = types.ModuleType("client_wires.codex_compat.ui")
        fake_ui.Option = _DummyOption
        fake_ui.multi_select_items = lambda *_a, **_k: []
        fake_ui.single_select_items = lambda *_a, **_k: fixer_wire.UNATTACHED_FIXER_ACTION
        fake_main = _fake_codex_main_module()
        fake_package.main = fake_main
        fake_package.ui = fake_ui
        captured: dict[str, object] = {}

        def capture_unattached_launch(
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
            patch.object(fixer_wire, "_launch_unattached_fixer", side_effect=capture_unattached_launch),
        ):
            code = fixer_wire.main(["--dry-run", "--extra-codex-flag"])

        self.assertEqual(code, 0)
        self.assertEqual(captured["passthrough_args"], ["--extra-codex-flag"])
        self.assertTrue(captured["dry_run"])
        self.assertIs(captured["Option"], _DummyOption)


if __name__ == "__main__":
    unittest.main()
