"""Focused conformance tests for Claude Opus 5.5 / Sonnet 5.5 Agy routing.

The agy 1.2.16 `agy models` inventory lists exactly:
claude-opus-5-5-{low,medium,high} / claude-sonnet-5-5-{low,medium,high} with
labels "Claude Opus 5.5 (Low|Medium|High)" and "Claude Sonnet 5.5 (Low|Medium|High)".
Retired Claude 4.6 entries must be advertised nowhere and must never be silently
substituted by a 5.5 route.

Verified against the live CLI (agy 1.2.16) without executing an LLM turn:
- `agy models` lists each variant as a `<slug>\\t<label>` pair; both spellings are
  recorded in `_AGY_1_2_16_MODELS_INVENTORY` below and pinned to the adapter tables.
- stream-json input with empty stdin (zero turns) accepts both the slug and the
  display-label form of `--model` for Claude 5.5 variants (init payload echoes the
  accepted selection) and rejects unknown models, so label dispatch follows the
  established Gemini convention with verified variants.
- `--effort` combined with a Claude 5.5 base model fails model selection
  ("--effort is not supported"), confirming effort must be encoded in the variant
  with no separate effort flag.
"""

from __future__ import annotations

import json
import os
import sqlite3
import tempfile
import types
import unittest
from pathlib import Path
from unittest.mock import patch

from client_wires import fixer_wire
from client_wires.backends.antigravity_adapter import (
    _ANTIGRAVITY_CLI_MODEL_OPTIONS,
    _ANTIGRAVITY_MODEL_SLUG_ALIASES,
    ANTIGRAVITY_MCP_TIMEOUT_SECONDS,
    AntigravityBackendAdapter,
    normalize_antigravity_model_alias,
    normalize_antigravity_reasoning_alias,
)
from client_wires.backends.catalog import load_backend_catalog
from client_wires.backends.manifest.schema import load_manifest

_MANIFEST_PATH = Path(__file__).resolve().parents[1] / "backends" / "manifest" / "antigravity.manifest.json"

_CLAUDE_55_BASES = ("Claude Opus 5.5", "Claude Sonnet 5.5")

# Exact `agy models` output pairs from agy 1.2.16 (slug, label). Model identifiers
# only; no credentials or local paths.
_AGY_1_2_16_MODELS_INVENTORY = (
    ("gemini-3.8-flash-high", "Gemini 3.8 Flash (High)"),
    ("gemini-3.8-flash-medium", "Gemini 3.8 Flash (Medium)"),
    ("gemini-3.8-flash-low", "Gemini 3.8 Flash (Low)"),
    ("gemini-3.7-flash-high", "Gemini 3.7 Flash (High)"),
    ("gemini-3.7-flash-medium", "Gemini 3.7 Flash (Medium)"),
    ("gemini-3.7-flash-low", "Gemini 3.7 Flash (Low)"),
    ("gemini-3.6-flash-high", "Gemini 3.6 Flash (High)"),
    ("gemini-3.6-flash-medium", "Gemini 3.6 Flash (Medium)"),
    ("gemini-3.6-flash-low", "Gemini 3.6 Flash (Low)"),
    ("gemini-3.1-pro-high", "Gemini 3.1 Pro (High)"),
    ("gemini-3.1-pro-low", "Gemini 3.1 Pro (Low)"),
    ("claude-opus-5-5-low", "Claude Opus 5.5 (Low)"),
    ("claude-opus-5-5-medium", "Claude Opus 5.5 (Medium)"),
    ("claude-opus-5-5-high", "Claude Opus 5.5 (High)"),
    ("claude-sonnet-5-5-low", "Claude Sonnet 5.5 (Low)"),
    ("claude-sonnet-5-5-medium", "Claude Sonnet 5.5 (Medium)"),
    ("claude-sonnet-5-5-high", "Claude Sonnet 5.5 (High)"),
    ("gpt-oss-120b-medium", "GPT-OSS 120B (Medium)"),
)

_AGY_INVENTORY_LABELS = {label for _, label in _AGY_1_2_16_MODELS_INVENTORY}
_AGY_INVENTORY_BY_SLUG = dict(_AGY_1_2_16_MODELS_INVENTORY)


class AntigravityClaude55DispatchTests(unittest.TestCase):
    def setUp(self) -> None:
        self.adapter = AntigravityBackendAdapter()

    def test_bare_claude_55_models_map_explicit_effort_to_cli_variant(self) -> None:
        self.assertEqual(self.adapter._build_model_args("Claude Opus 5.5", "high"), ["--model", "Claude Opus 5.5 (High)"])
        self.assertEqual(self.adapter._build_model_args("Claude Opus 5.5", "medium"), ["--model", "Claude Opus 5.5 (Medium)"])
        self.assertEqual(self.adapter._build_model_args("Claude Opus 5.5", "low"), ["--model", "Claude Opus 5.5 (Low)"])
        self.assertEqual(self.adapter._build_model_args("Claude Sonnet 5.5", "high"), ["--model", "Claude Sonnet 5.5 (High)"])
        self.assertEqual(self.adapter._build_model_args("Claude Sonnet 5.5", "medium"), ["--model", "Claude Sonnet 5.5 (Medium)"])
        self.assertEqual(self.adapter._build_model_args("Claude Sonnet 5.5", "low"), ["--model", "Claude Sonnet 5.5 (Low)"])

    def test_literal_variant_labels_resolve_without_contradictory_reasoning(self) -> None:
        self.assertEqual(self.adapter._build_model_args("Claude Opus 5.5 (High)", ""), ["--model", "Claude Opus 5.5 (High)"])
        self.assertEqual(self.adapter._build_model_args("Claude Opus 5.5 (High)", "high"), ["--model", "Claude Opus 5.5 (High)"])
        self.assertEqual(self.adapter._build_model_args("Claude Sonnet 5.5 (Medium)", "medium"), ["--model", "Claude Sonnet 5.5 (Medium)"])

    def test_selection_object_dispatches_bare_claude_55_with_effort(self) -> None:
        selection = types.SimpleNamespace(model="Claude Opus 5.5", reasoning_effort="high")
        self.assertEqual(self.adapter.build_llm_args(selection), ["--model", "Claude Opus 5.5 (High)"])

    def test_conflicting_variant_and_effort_are_rejected(self) -> None:
        with self.assertRaisesRegex(RuntimeError, "conflicts"):
            self.adapter._build_model_args("Claude Opus 5.5 (High)", "medium")
        with self.assertRaisesRegex(RuntimeError, "conflicts"):
            self.adapter._build_model_args("Claude Sonnet 5.5 (Low)", "high")

    def test_bare_claude_55_without_effort_requires_explicit_choice(self) -> None:
        with self.assertRaisesRegex(RuntimeError, "requires reasoning"):
            self.adapter._build_model_args("Claude Opus 5.5", "")
        with self.assertRaisesRegex(RuntimeError, "Supported reasoning values"):
            self.adapter._build_model_args("Claude Sonnet 5.5", "thinking")

    def test_headless_and_resume_commands_carry_concrete_claude_55_variant(self) -> None:
        headless = self.adapter.build_headless_command(
            model="Claude Opus 5.5",
            reasoning="high",
            selected={},
            available={},
            prompt="hello",
        )
        self.assertEqual(
            headless,
            [
                "agy",
                "--dangerously-skip-permissions",
                "--print-timeout",
                "120m",
                "--output-format",
                "json",
                "--model",
                "Claude Opus 5.5 (High)",
                "--print",
                "hello",
            ],
        )

        resumed = self.adapter.build_headless_resume_command(
            external_session_id="conv-42",
            model="Claude Sonnet 5.5",
            reasoning="medium",
            selected={},
            available={},
            prompt="hello",
        )
        self.assertEqual(
            resumed,
            [
                "agy",
                "--dangerously-skip-permissions",
                "--print-timeout",
                "120m",
                "--output-format",
                "json",
                "--model",
                "Claude Sonnet 5.5 (Medium)",
                "--conversation",
                "conv-42",
                "--print",
                "hello",
            ],
        )

        interactive_resume = self.adapter.build_resume_command(
            ["--model", "Claude Opus 5.5 (Low)"],
            "conv-7",
        )
        self.assertEqual(
            interactive_resume,
            ["agy", "--model", "Claude Opus 5.5 (Low)", "--conversation", "conv-7"],
        )

        for command in (headless, resumed, interactive_resume):
            self.assertNotIn("Thinking", " ".join(command))

    def test_raw_upstream_slugs_normalize_and_dispatch(self) -> None:
        self.assertEqual(self.adapter._build_model_args("claude-opus-5-5-high", ""), ["--model", "Claude Opus 5.5 (High)"])
        self.assertEqual(self.adapter._build_model_args("claude-sonnet-5-5-medium", "medium"), ["--model", "Claude Sonnet 5.5 (Medium)"])
        self.assertEqual(normalize_antigravity_model_alias("claude-opus-5-5-high"), "Claude Opus 5.5")
        self.assertEqual(normalize_antigravity_reasoning_alias("claude-opus-5-5-high", ""), "high")
        self.assertEqual(normalize_antigravity_model_alias("gemini-3.8-flash-low"), "Gemini 3.8 Flash")

    def test_raw_slug_with_contradictory_effort_is_rejected(self) -> None:
        with self.assertRaisesRegex(RuntimeError, "conflicts"):
            self.adapter._build_model_args("claude-opus-5-5-high", "low")

    def test_retired_claude_46_requests_fail_explicitly_without_substitution(self) -> None:
        for candidate in ("Claude Sonnet 4.6 (Thinking)", "Claude Opus 4.6 (Thinking)", "Claude Opus 4.6"):
            with self.assertRaisesRegex(RuntimeError, "Unsupported model"):
                self.adapter._build_model_args(candidate, "")
            with self.assertRaisesRegex(RuntimeError, "Unsupported model"):
                self.adapter._build_model_args(candidate, "high")

        self.assertEqual(normalize_antigravity_model_alias("Claude Opus 4.6"), "Claude Opus 4.6")
        self.assertNotIn("4.6", " ".join(_ANTIGRAVITY_CLI_MODEL_OPTIONS))

    def test_gemini_routes_are_unchanged(self) -> None:
        self.assertEqual(self.adapter._build_model_args("Gemini 3.6 Flash", "high"), ["--model", "Gemini 3.6 Flash (High)"])
        self.assertEqual(self.adapter._build_model_args("gemini-3.6-flash", "low"), ["--model", "Gemini 3.6 Flash (Low)"])
        self.assertEqual(self.adapter._build_model_args("Gemini 3.1 Pro", "high"), ["--model", "Gemini 3.1 Pro (High)"])
        self.assertEqual(self.adapter._build_model_args("Gemini 3.8 Flash (Medium)", ""), ["--model", "Gemini 3.8 Flash (Medium)"])
        self.assertEqual(normalize_antigravity_model_alias("gemini-3.6-flash"), "Gemini 3.6 Flash")


class AntigravityClaude55PersistenceTests(unittest.TestCase):
    def _persist(self, session_id: int, model: str, reasoning: str):
        with sqlite3.connect(":memory:") as conn:
            conn.executescript(
                """
                CREATE TABLE session (
                    id INTEGER PRIMARY KEY,
                    cli_backend TEXT NOT NULL,
                    cli_model TEXT NOT NULL,
                    cli_reasoning TEXT NOT NULL
                );
                """
            )
            conn.execute(
                "INSERT INTO session (id, cli_backend, cli_model, cli_reasoning) VALUES (?, '', '', '')",
                (session_id,),
            )
            session_row = fixer_wire.SessionRow(
                session_id=session_id,
                global_session_id=session_id,
                task_description="Agy Claude 5.5 selection",
                status="pending",
            )
            selection = fixer_wire.SessionLaunchSelection(
                backend="agy",
                model=model,
                reasoning=reasoning,
            )
            resolved = fixer_wire._persist_session_launch_selection(conn, session_row, selection)
            stored = conn.execute(
                "SELECT cli_backend, cli_model, cli_reasoning FROM session WHERE id = ?",
                (session_id,),
            ).fetchone()
        return resolved, stored

    def test_literal_variant_selection_persists_base_model_plus_effort(self) -> None:
        resolved, stored = self._persist(61, "Claude Opus 5.5 (High)", "default")
        self.assertEqual(resolved, fixer_wire.SessionLaunchSelection("antigravity", "Claude Opus 5.5", "high"))
        self.assertEqual(stored, ("antigravity", "Claude Opus 5.5", "high"))

    def test_bare_model_with_explicit_effort_persists(self) -> None:
        resolved, stored = self._persist(62, "Claude Sonnet 5.5", "medium")
        self.assertEqual(resolved, fixer_wire.SessionLaunchSelection("antigravity", "Claude Sonnet 5.5", "medium"))
        self.assertEqual(stored, ("antigravity", "Claude Sonnet 5.5", "medium"))

    def test_raw_upstream_slug_selection_persists_canonical_base_model(self) -> None:
        resolved, stored = self._persist(63, "claude-sonnet-5-5-low", "")
        self.assertEqual(resolved, fixer_wire.SessionLaunchSelection("antigravity", "Claude Sonnet 5.5", "low"))
        self.assertEqual(stored, ("antigravity", "Claude Sonnet 5.5", "low"))


class AntigravityClaude55ConformanceTests(unittest.TestCase):
    def test_manifest_and_catalog_agree_on_claude_55_variants(self) -> None:
        catalog = load_backend_catalog()["antigravity"]
        manifest = load_manifest(_MANIFEST_PATH)

        self.assertEqual(manifest.provider, "antigravity")
        self.assertEqual(set(manifest.models.options), set(_ANTIGRAVITY_CLI_MODEL_OPTIONS))
        self.assertEqual(set(manifest.models.internal_id_map), set(manifest.models.options))
        self.assertEqual(
            set(manifest.models.internal_id_map.values()),
            set(manifest.models.options),
        )
        self.assertIn(manifest.models.default, manifest.models.options)
        self.assertEqual(manifest.reasoning.options, ["low", "medium", "high"])

        catalog_options = set(catalog["model_options"])
        for base in _CLAUDE_55_BASES:
            self.assertIn(base, catalog_options)
        for option in manifest.models.options:
            base = option.rsplit(" (", 1)[0]
            self.assertIn(base, catalog_options)

    def test_retired_claude_46_is_advertised_nowhere(self) -> None:
        catalog = load_backend_catalog()["antigravity"]
        manifest = load_manifest(_MANIFEST_PATH)

        advertised = [*(str(item) for item in catalog["model_options"]), *manifest.models.options]
        for option in advertised:
            self.assertNotIn("4.6", option)


class AntigravityClaude55VerifiedInventoryTests(unittest.TestCase):
    def setUp(self) -> None:
        self.adapter = AntigravityBackendAdapter()

    def test_every_advertised_cli_option_is_a_verified_agy_label(self) -> None:
        for option in _ANTIGRAVITY_CLI_MODEL_OPTIONS:
            self.assertIn(option, _AGY_INVENTORY_LABELS)

    def test_every_verified_claude_55_variant_is_advertised(self) -> None:
        for slug, label in _AGY_1_2_16_MODELS_INVENTORY:
            if not label.startswith("Claude "):
                continue
            self.assertIn(label, _ANTIGRAVITY_CLI_MODEL_OPTIONS)
            self.assertEqual(_ANTIGRAVITY_MODEL_SLUG_ALIASES[slug], label)

    def test_slug_alias_map_agrees_with_verified_inventory(self) -> None:
        for slug, label in _ANTIGRAVITY_MODEL_SLUG_ALIASES.items():
            self.assertEqual(_AGY_INVENTORY_BY_SLUG[slug], label)

    def test_both_families_dispatch_verified_variants_fresh_and_resume(self) -> None:
        fresh = self.adapter.build_headless_command(
            model="Claude Opus 5.5",
            reasoning="high",
            selected={},
            available={},
            prompt="hello",
        )
        resumed = self.adapter.build_headless_resume_command(
            external_session_id="conv-1",
            model="Claude Sonnet 5.5",
            reasoning="low",
            selected={},
            available={},
            prompt="hello",
        )
        for command, expected in (
            (fresh, "Claude Opus 5.5 (High)"),
            (resumed, "Claude Sonnet 5.5 (Low)"),
        ):
            dispatched = command[command.index("--model") + 1]
            self.assertEqual(dispatched, expected)
            self.assertIn(dispatched, _AGY_INVENTORY_LABELS)


class AntigravityRetiredPickDiagnosticTests(unittest.TestCase):
    """Persisted or requested retired Claude 4.6 picks get an explicit diagnostic.

    The wire normalizer must never silently accept or reshape a retired pick;
    legacy `thinking` effort must never be silently remapped onto 5.5/Gemini.
    """

    def test_wire_model_normalization_rejects_retired_46_picks_explicitly(self) -> None:
        descriptor = fixer_wire._backend_descriptor("antigravity")
        for candidate in ("Claude Sonnet 4.6", "Claude Opus 4.6", "Claude Sonnet 4.6 (Thinking)"):
            with self.assertRaisesRegex(RuntimeError, "retired"):
                fixer_wire._normalize_backend_model(descriptor, candidate)
            with self.assertRaisesRegex(RuntimeError, "retired"):
                fixer_wire._normalize_backend_model(descriptor, candidate + " (Thinking)")

    def test_wire_model_normalization_still_accepts_current_families_and_slugs(self) -> None:
        descriptor = fixer_wire._backend_descriptor("antigravity")
        for candidate, expected in (
            ("Claude Opus 5.5", "Claude Opus 5.5"),
            ("Claude Sonnet 5.5", "Claude Sonnet 5.5"),
            ("Claude Opus 5.5 (High)", "Claude Opus 5.5"),
            ("claude-sonnet-5-5-low", "Claude Sonnet 5.5"),
            ("Gemini 3.7 Flash", "Gemini 3.7 Flash"),
        ):
            self.assertEqual(fixer_wire._normalize_backend_model(descriptor, candidate), expected)

    def test_wire_reasoning_normalization_rejects_legacy_thinking_effort(self) -> None:
        descriptor = fixer_wire._backend_descriptor("antigravity")
        for model in ("Claude Opus 5.5", "Claude Sonnet 5.5", "Gemini 3.7 Flash"):
            with self.assertRaisesRegex(RuntimeError, "Unsupported reasoning"):
                fixer_wire._normalize_backend_reasoning(descriptor, "thinking", model)
        for accepted in ("low", "medium", "high"):
            self.assertEqual(
                fixer_wire._normalize_backend_reasoning(descriptor, accepted, "Claude Opus 5.5"),
                accepted,
            )

    def _persist(self, session_id: int, model: str, reasoning: str, *, started: bool = False):
        with sqlite3.connect(":memory:") as conn:
            conn.executescript(
                """
                CREATE TABLE session (
                    id INTEGER PRIMARY KEY,
                    cli_backend TEXT NOT NULL,
                    cli_model TEXT NOT NULL,
                    cli_reasoning TEXT NOT NULL
                );
                """
            )
            conn.execute(
                "INSERT INTO session (id, cli_backend, cli_model, cli_reasoning) VALUES (?, ?, ?, ?)",
                (
                    session_id,
                    "antigravity" if started else "",
                    "Claude Sonnet 4.6" if started else "",
                    "thinking" if started else "",
                ),
            )
            session_row = fixer_wire.SessionRow(
                session_id=session_id,
                global_session_id=session_id,
                task_description="Agy retired pick diagnostics",
                status="pending",
                cli_backend="antigravity" if started else "",
                cli_model="Claude Sonnet 4.6" if started else "",
                cli_reasoning="thinking" if started else "",
                external_session_id="conv-legacy" if started else "",
            )
            selection = fixer_wire.SessionLaunchSelection(backend="agy", model=model, reasoning=reasoning)
            fixer_wire._persist_session_launch_selection(conn, session_row, selection)
            return conn.execute(
                "SELECT cli_backend, cli_model, cli_reasoning FROM session WHERE id = ?",
                (session_id,),
            ).fetchone()

    def test_persisting_a_retired_46_selection_raises_and_stores_nothing(self) -> None:
        for model in ("Claude Opus 4.6", "Claude Sonnet 4.6 (Thinking)"):
            with self.assertRaisesRegex(RuntimeError, "retired"):
                self._persist(71, model, "high")

    def test_persisted_old_46_pick_is_diagnosed_not_silently_changed(self) -> None:
        with self.assertRaisesRegex(RuntimeError, "retired"):
            self._persist(72, "Claude Opus 5.5", "high", started=True)


class AntigravityClaude55RuntimePreservationTests(unittest.TestCase):
    def test_mcp_config_merge_preserves_servers_and_fixer_timeout(self) -> None:
        adapter = AntigravityBackendAdapter()
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            cwd = root / "workspace"
            user_config = root / "home" / "config" / "mcp_config.json"
            user_config.parent.mkdir(parents=True)
            user_config.write_text(
                json.dumps(
                    {
                        "mcpServers": {"playwright": {"command": "keep-me"}},
                        "otherSetting": True,
                    }
                ),
                encoding="utf-8",
            )
            selected = {
                "fixer_mcp": {"command": "/tmp/fixer_mcp"},
                "sqlite": {"command": "/tmp/sqlite"},
            }
            available = {
                "fixer_mcp": {"command": "/tmp/fixer_mcp", "transport": "stdio"},
                "sqlite": {"command": "/tmp/sqlite", "transport": "stdio"},
            }
            with patch.dict(os.environ, {"FIXER_ANTIGRAVITY_MCP_CONFIG_PATH": str(user_config)}, clear=False):
                adapter.ensure_runtime_files(cwd, object(), selected, available)
            payload = json.loads(user_config.read_text(encoding="utf-8"))

        servers = payload["mcpServers"]
        self.assertEqual(servers["fixer_mcp"]["timeoutSeconds"], ANTIGRAVITY_MCP_TIMEOUT_SECONDS)
        self.assertEqual(servers["playwright"], {"command": "keep-me"})
        self.assertEqual(servers["sqlite"]["command"], "/tmp/sqlite")
        self.assertTrue(payload["otherSetting"])
        self.assertFalse((cwd / ".agents" / "mcp_config.json").exists())


if __name__ == "__main__":
    unittest.main()
