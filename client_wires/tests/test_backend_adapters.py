from __future__ import annotations

import tempfile
import unittest
from pathlib import Path

from client_wires.backends.claude_adapter import ClaudeCodeBackendAdapter
from client_wires.backends.kimi_code_native_adapter import KimiCodeNativeBackendAdapter


def test_claude_headless_command_forwards_explicit_opus_5_and_xhigh() -> None:
    command = ClaudeCodeBackendAdapter().build_headless_command(
        model="claude-opus-5",
        reasoning="xhigh",
        selected={},
        available={},
        prompt="implement the task",
    )

    assert command == [
        "claude",
        "--model",
        "claude-opus-5",
        "--effort",
        "xhigh",
        "--dangerously-skip-permissions",
        "implement the task",
    ]


def test_claude_keeps_existing_model_aliases_and_default_selection() -> None:
    adapter = ClaudeCodeBackendAdapter()

    assert adapter.default_model == "sonnet"
    assert adapter.default_reasoning == "medium"
    assert adapter.model_options == ("sonnet", "opus", "claude-opus-5", "haiku")
    assert adapter.normalize_model("opus") == "opus"


class KimiCodeNativeBackendAdapterTests(unittest.TestCase):
    def test_materializes_project_fixer_skills_with_mcp_config(self) -> None:
        adapter = KimiCodeNativeBackendAdapter()
        with tempfile.TemporaryDirectory() as tmp:
            cwd = Path(tmp)

            adapter.ensure_runtime_files(cwd, object(), selected={}, available={})

            self.assertTrue((cwd / ".kimi-code" / "mcp.json").is_file())
            self.assertTrue((cwd / ".kimi-code" / "skills" / "init-fixer" / "SKILL.md").is_file())
            self.assertTrue(
                (cwd / ".kimi-code" / "skills" / "run-manual-netrunner" / "SKILL.md").is_file()
            )
            self.assertTrue(
                (cwd / ".kimi-code" / "skills" / "complete-netrunner-session" / "SKILL.md").is_file()
            )
