from __future__ import annotations

import tempfile
import unittest
from pathlib import Path

from client_wires import (
    fixer_autonomous_prompts,
    fixer_autonomous_state,
    fixer_autonomous_transcripts,
    fixer_autonomous_wave,
)


class FixerAutonomousModuleTests(unittest.TestCase):
    def test_state_module_normalizes_legacy_active_worker_id(self) -> None:
        state = {
            "active_netrunner_session_ids": ["3", 3, "bad", 0],
            "active_netrunner_session_id": "4",
        }

        self.assertEqual(
            fixer_autonomous_state._normalize_active_netrunner_session_ids(state),
            [3, 4],
        )

    def test_prompt_module_uses_default_mcp_guidance_callback(self) -> None:
        prompt = fixer_autonomous_prompts._build_autonomous_netrunner_prompt(
            7,
            ["fixer_mcp"],
            "fixer-session",
            {},
            default_how_to_fn=lambda name: f"default guidance for {name}",
        )

        self.assertIn("- fixer_mcp: default guidance for fixer_mcp", prompt)
        self.assertIn("wake_fixer_autonomous", prompt)

    def test_prompt_module_can_suppress_autonomous_wake(self) -> None:
        prompt = fixer_autonomous_prompts._build_autonomous_netrunner_prompt(
            7,
            ["fixer_mcp"],
            "fixer-session",
            {},
            default_how_to_fn=lambda name: f"default guidance for {name}",
            suppress_autonomous_wake=True,
        )

        self.assertIn("Do not call fixer_mcp.wake_fixer_autonomous", prompt)
        self.assertNotIn("call the fixer_mcp tool `wake_fixer_autonomous`", prompt)

    def test_prompt_module_omits_blank_fixer_session_id(self) -> None:
        prompt = fixer_autonomous_prompts._build_autonomous_netrunner_prompt(
            7,
            ["fixer_mcp"],
            "",
            {},
            default_how_to_fn=lambda name: f"default guidance for {name}",
            suppress_autonomous_wake=True,
        )

        self.assertNotIn("Autonomous fixer Codex session ID", prompt)
        self.assertIn("Preselected session ID from fixer autonomous flow: `7`.", prompt)

    def test_transcript_module_extracts_droid_session_id_from_plain_log_line(self) -> None:
        session_id = fixer_autonomous_transcripts._extract_droid_session_id_from_line(
            "external_session_id='droid-session-123'",
        )

        self.assertEqual(session_id, "droid-session-123")

    def test_wave_module_keeps_deterministic_artifact_path(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            project_cwd = Path(tmp)

            self.assertEqual(
                fixer_autonomous_wave._wave_worker_metadata_path(project_cwd, 2, 9),
                project_cwd.resolve()
                / ".codex"
                / "netrunner_wave_artifacts"
                / "wave-2"
                / "session-9"
                / "worker_metadata.json",
            )


if __name__ == "__main__":
    unittest.main()
