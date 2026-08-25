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
        self.assertIn("Preselected compatibility session ID from fixer autonomous flow: `7`.", prompt)

    def test_transcript_module_extracts_droid_session_id_from_plain_log_line(self) -> None:
        session_id = fixer_autonomous_transcripts._extract_droid_session_id_from_line(
            "external_session_id='droid-session-123'",
        )

        self.assertEqual(session_id, "droid-session-123")

    def test_transcript_module_does_not_default_unlaunched_manual_session_to_codex(self) -> None:
        capability = fixer_autonomous_transcripts._provider_thread_capability("")

        self.assertEqual(capability.backend, "")
        self.assertEqual(capability.continuation_mode, "awaiting_backend")
        self.assertFalse(capability.continuation_supported)

    def test_transcript_module_reports_provider_capabilities_truthfully(self) -> None:
        codex = fixer_autonomous_transcripts._provider_thread_capability("codex")
        kimi = fixer_autonomous_transcripts._provider_thread_capability("kimi_cli")

        self.assertTrue(codex.continuation_supported)
        self.assertEqual(codex.transcript_availability, "jsonl")
        self.assertEqual(kimi.backend, "kimi-code")
        self.assertFalse(kimi.continuation_supported)
        self.assertIn("MCP config", kimi.unsupported_reason)

    def test_transcript_module_normalizes_codex_and_droid_messages(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            transcript_path = Path(tmp) / "thread.jsonl"
            transcript_path.write_text(
                "\n".join(
                    [
                        '{"timestamp":"2026-07-23T10:00:00Z","type":"event_msg","payload":{"type":"user_message","message":"Inspect this failure"}}',
                        '{"timestamp":"2026-07-23T10:00:01Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"I found the cause."}]}}',
                        '{"type":"tool_call","payload":{"type":"tool_call","name":"exec"}}',
                    ]
                )
                + "\n",
                encoding="utf-8",
            )

            messages = fixer_autonomous_transcripts._load_provider_thread_messages(
                "codex",
                transcript_path,
            )

        self.assertEqual([message.role for message in messages], ["user", "assistant"])
        self.assertEqual(messages[0].text, "Inspect this failure")
        self.assertEqual(messages[1].text, "I found the cause.")

    def test_transcript_module_extracts_antigravity_conversation_id_from_cli_line(self) -> None:
        conversation_id = fixer_autonomous_transcripts._extract_antigravity_conversation_id_from_line(
            "I0702 printmode.go:179] Print mode: conversation=cb18f692-f4a9-4895-9509-d093dd911437, sending message",
        )

        self.assertEqual(conversation_id, "cb18f692-f4a9-4895-9509-d093dd911437")

    def test_transcript_module_matches_antigravity_conversation_to_workspace(self) -> None:
        with tempfile.TemporaryDirectory() as tmp, tempfile.TemporaryDirectory() as logs_tmp:
            cwd = Path(tmp)
            log_root = Path(logs_tmp)
            log_path = log_root / "cli-20260702_023232.log"
            log_path.write_text(
                "\n".join(
                    [
                        "I0702 common.go:161] CLI app data directory: /tmp/app",
                        f"I0702 server.go:224] Creating CLI server backend: workspaceDirs=[{cwd.resolve()}] appDataDir=/tmp/app",
                        "I0702 server.go:807] Created conversation cb18f692-f4a9-4895-9509-d093dd911437",
                    ]
                )
                + "\n",
                encoding="utf-8",
            )

            conversation_id = fixer_autonomous_transcripts._find_new_antigravity_conversation_id_from_cli_logs(
                cwd,
                launch_started_at=0,
                log_root=log_root,
            )

        self.assertEqual(conversation_id, "cb18f692-f4a9-4895-9509-d093dd911437")

    def test_transcript_module_ignores_antigravity_conversation_from_other_workspace(self) -> None:
        with tempfile.TemporaryDirectory() as tmp, tempfile.TemporaryDirectory() as logs_tmp:
            cwd = Path(tmp)
            log_root = Path(logs_tmp)
            log_path = log_root / "cli-20260702_023232.log"
            log_path.write_text(
                "\n".join(
                    [
                        "I0702 server.go:224] Creating CLI server backend: workspaceDirs=[/tmp/other-project] appDataDir=/tmp/app",
                        "I0702 server.go:807] Created conversation cb18f692-f4a9-4895-9509-d093dd911437",
                    ]
                )
                + "\n",
                encoding="utf-8",
            )

            conversation_id = fixer_autonomous_transcripts._find_new_antigravity_conversation_id_from_cli_logs(
                cwd,
                launch_started_at=0,
                log_root=log_root,
            )

        self.assertIsNone(conversation_id)

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
