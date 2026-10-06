"""Focused tests for Pi session JSONL detection and the durable attempt manifest.

Covers the P0/P1 transcript-attribution contract: real Pi JSONL files are
detected and bound with proven id/cwd/time identity from fresh and resumed
runs, continuation history stays unambiguously linked to the worker head, and
launch attempts are registered in an append-only manifest without new DB
schema. No test spawns a provider subprocess or touches a live DB.
"""

from __future__ import annotations

import json
import time
import unittest
from pathlib import Path
from tempfile import TemporaryDirectory

from client_wires import fixer_autonomous, fixer_autonomous_transcripts


def _pi_header(session_id: str, cwd: str, timestamp: str) -> str:
    return json.dumps(
        {"type": "session", "version": 3, "id": session_id, "timestamp": timestamp, "cwd": cwd},
        ensure_ascii=False,
    )


def _dumps(payload: object) -> str:
    return json.dumps(payload, ensure_ascii=False)


def _write(path: Path, body: str) -> Path:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(body, encoding="utf-8")
    return path


class PiDetectionTests(unittest.TestCase):
    def test_pi_project_dir_name_matches_cli_layout(self) -> None:
        self.assertEqual(
            fixer_autonomous_transcripts._pi_project_dir_name(Path("/Users/operator")),
            "--Users-operator--",
        )
        self.assertEqual(
            fixer_autonomous_transcripts._pi_project_dir_name(
                Path("/Users/operator/projects/demo_app/.codex/netrunner_worktrees/wave-1/session-2")
            ),
            "--Users-operator-projects-demo_app-.codex-netrunner_worktrees-wave-1-session-2--",
        )

    def test_fresh_pi_session_detected_with_proven_identity(self) -> None:
        with TemporaryDirectory() as tmp:
            root = Path(tmp) / "sessions"
            cwd = Path(tmp) / "worktree"
            cwd.mkdir()
            session_id = "01a0ecc4-fresh-run"
            transcript = _write(
                root / fixer_autonomous_transcripts._pi_project_dir_name(cwd) / f"2026-10-05T10-00-00-000Z_{session_id}.jsonl",
                _pi_header(session_id, str(cwd), "2026-10-05T10:00:00.000Z")
                + "\n"
                + json.dumps({"type": "message", "id": "m1", "message": {"role": "user", "content": "hi"}})
                + "\n",
            )
            found = fixer_autonomous_transcripts._find_new_pi_session_id_from_transcript_store(
                cwd,
                launch_started_at=time.time() - 5,
                sessions_root=root,
            )
            self.assertEqual(found, session_id)
            self.assertTrue(transcript.exists())

    def test_detection_ignores_other_cwd_and_contradictory_identity(self) -> None:
        with TemporaryDirectory() as tmp:
            root = Path(tmp) / "sessions"
            cwd = Path(tmp) / "worktree"
            cwd.mkdir()
            other_cwd = Path(tmp) / "other"
            other_cwd.mkdir()
            # Real-looking files that must never be bound to this worker:
            # wrong cwd, and a header id that contradicts the filename id.
            _write(
                root / "anywhere" / "2026-10-05T10-00-00-000Z_wrong-cwd.jsonl",
                _pi_header("wrong-cwd", str(other_cwd), "2026-10-05T10:00:00.000Z") + "\n",
            )
            _write(
                root / "anywhere" / "2026-10-05T11-00-00-000Z_declared-id.jsonl",
                _pi_header("someone-else", str(cwd), "2026-10-05T11:00:00.000Z") + "\n",
            )
            found = fixer_autonomous_transcripts._find_new_pi_session_id_from_transcript_store(
                cwd,
                launch_started_at=time.time() - 5,
                sessions_root=root,
            )
            self.assertIsNone(found)

    def test_detection_rejects_files_older_than_launch(self) -> None:
        with TemporaryDirectory() as tmp:
            root = Path(tmp) / "sessions"
            cwd = Path(tmp) / "worktree"
            cwd.mkdir()
            session_id = "01a0ecc4-old-run"
            _write(
                root / fixer_autonomous_transcripts._pi_project_dir_name(cwd) / f"2026-10-05T10-00-00-000Z_{session_id}.jsonl",
                _pi_header(session_id, str(cwd), "2026-10-05T10:00:00.000Z") + "\n",
            )
            found = fixer_autonomous_transcripts._find_new_pi_session_id_from_transcript_store(
                cwd,
                launch_started_at=time.time() + 60,
                sessions_root=root,
            )
            self.assertIsNone(found)

    def test_resumed_run_binds_known_id_only_with_proven_files(self) -> None:
        with TemporaryDirectory() as tmp:
            root = Path(tmp) / "sessions"
            cwd = Path(tmp) / "worktree"
            cwd.mkdir()
            resumed_id = "01a0ecc4-resumed-run"

            def no_fresh(*_args, **_kwargs):
                return None

            # No transcript evidence for the known id: a missing file must not
            # silently become a bound session.
            self.assertIsNone(
                fixer_autonomous_transcripts._wait_for_new_pi_session_id(
                    cwd,
                    resumed_id,
                    launch_started_at=time.time(),
                    timeout_sec=0.2,
                    find_new_pi_session_id_from_transcript_store_fn=no_fresh,
                    pi_transcript_attempt_evidence_fn=lambda *a, **k: [],
                )
            )

            # With real proven evidence (the resumed attempt keeps appending to
            # its session files), the known id is bound.
            _write(
                root / fixer_autonomous_transcripts._pi_project_dir_name(cwd) / f"2026-10-05T10-00-00-000Z_{resumed_id}.jsonl",
                _pi_header(resumed_id, str(cwd), "2026-10-05T10:00:00.000Z") + "\n",
            )
            self.assertEqual(
                fixer_autonomous_transcripts._wait_for_new_pi_session_id(
                    cwd,
                    resumed_id,
                    launch_started_at=time.time(),
                    timeout_sec=0.2,
                    find_new_pi_session_id_from_transcript_store_fn=no_fresh,
                    pi_transcript_attempt_evidence_fn=lambda c, s, **k: fixer_autonomous_transcripts._pi_transcript_attempt_evidence(
                        c, s, sessions_root=root
                    ),
                ),
                resumed_id,
            )

    def test_external_session_waiter_dispatches_pi(self) -> None:
        seen: list[tuple[str, str | None]] = []

        def pi_wait(cwd: Path, before: str | None, **kwargs) -> str | None:
            seen.append((str(cwd), before))
            return "detected-pi-id"

        result = fixer_autonomous_transcripts._wait_for_new_external_session_id(
            "pi",
            Path("/tmp/project"),
            None,
            Path("/tmp/log"),
            timeout_sec=0.1,
            normalize_backend_name_fn=lambda name: name,
            wait_for_new_codex_session_id_fn=lambda *a, **k: None,
            wait_for_new_commandcode_session_id_fn=lambda *a, **k: None,
            wait_for_new_droid_session_id_fn=lambda *a, **k: None,
            wait_for_new_antigravity_conversation_id_fn=lambda *a, **k: None,
            wait_for_new_pi_session_id_fn=pi_wait,
        )
        self.assertEqual(result, "detected-pi-id")
        self.assertEqual(seen, [("/tmp/project", None)])


class PiContinuationHistoryTests(unittest.TestCase):
    def test_real_style_218_line_history_is_linked_and_ordered(self) -> None:
        """The acceptance payload shape: 218 lines across two continuation
        attempts, every command inside lines 16..193, large UTF-8 content
        present, and the history unambiguously linked to one session id."""
        with TemporaryDirectory() as tmp:
            root = Path(tmp) / "sessions"
            cwd = Path(tmp) / "worktree"
            cwd.mkdir()
            session_id = "01a0ecc4-continuations"
            header_one = _pi_header(session_id, str(cwd), "2026-10-05T10:00:00.000Z")
            header_two = _pi_header(session_id, str(cwd), "2026-10-05T11:00:00.000Z")

            large_utf8 = "команда-проверка-✅-длинная-строка-码检验-" * 500
            attempt_one_lines = [header_one]
            while len(attempt_one_lines) < 15:
                attempt_one_lines.append(_dumps({"type": "message", "id": f"a{len(attempt_one_lines)}", "message": {"role": "assistant", "content": "step"}}))
            for index in range(100):
                if index == 50:
                    attempt_one_lines.append(_dumps({"type": "tool_call", "id": f"t{index}", "tool": "bash", "command": f"echo {large_utf8}"}))
                else:
                    attempt_one_lines.append(_dumps({"type": "tool_call", "id": f"t{index}", "tool": "bash", "command": f"go test ./... -run TestFeature{index}"}))

            attempt_two_lines = [
                header_two,
                _dumps({"type": "message", "id": "u2", "message": {"role": "user", "content": "continue after resume"}}),
            ]
            for index in range(100, 176):
                attempt_two_lines.append(_dumps({"type": "tool_call", "id": f"t{index}", "tool": "bash", "command": f"go test ./... -run TestFeature{index}"}))
            while len(attempt_two_lines) < 103:
                attempt_two_lines.append(_dumps({"type": "message", "id": f"pad{len(attempt_two_lines)}", "message": {"role": "assistant", "content": "wrap-up"}}))

            session_dir = root / fixer_autonomous_transcripts._pi_project_dir_name(cwd)
            _write(session_dir / "2026-10-05T10-00-00-000Z_01a0ecc4-continuations.jsonl", "\n".join(attempt_one_lines) + "\n")
            _write(session_dir / "2026-10-05T11-00-00-000Z_01a0ecc4-continuations.jsonl", "\n".join(attempt_two_lines) + "\n")
            # Unrelated session decoys must never join the history.
            _write(session_dir / "2026-10-05T09-00-00-000Z_other-session.jsonl", _pi_header("other-session", str(cwd), "2026-10-05T09:00:00.000Z") + "\n")
            _write(session_dir / "2026-10-05T12-00-00-000Z_01a0ecc4-continuations.jsonl", _pi_header("someone-else", str(cwd), "2026-10-05T12:00:00.000Z") + "\n")

            evidence = fixer_autonomous_transcripts._pi_transcript_attempt_evidence(
                cwd, session_id, sessions_root=root
            )
            self.assertEqual(len(evidence), 2)
            self.assertEqual(
                [Path(str(record["path"])).name for record in evidence],
                [
                    "2026-10-05T10-00-00-000Z_01a0ecc4-continuations.jsonl",
                    "2026-10-05T11-00-00-000Z_01a0ecc4-continuations.jsonl",
                ],
            )
            for record in evidence:
                self.assertEqual(record["session_id"], session_id)
                self.assertEqual(record["session_cwd"], str(cwd))
                self.assertTrue(record["session_timestamp"])

            # Joined end to end (as the coverage reader does), the history is
            # exactly 218 lines with every command in the 16..193 window and
            # the large UTF-8 content intact.
            joined_lines: list[str] = []
            for record in evidence:
                joined_lines.extend(Path(str(record["path"])).read_text(encoding="utf-8").splitlines())
            self.assertEqual(len(joined_lines), 218)
            command_lines = [i for i, line in enumerate(joined_lines, start=1) if '"command":' in line]
            self.assertEqual((min(command_lines), max(command_lines)), (16, 193))
            self.assertEqual(len(command_lines), 176)
            self.assertTrue(any("команда-проверка-✅" in line for line in joined_lines))


class AttemptManifestTests(unittest.TestCase):
    def test_manifest_is_append_only_with_incrementing_attempts(self) -> None:
        with TemporaryDirectory() as tmp:
            cwd = Path(tmp)
            manifest = fixer_autonomous_transcripts.attempt_manifest_path(cwd, 7, "pi")
            first = fixer_autonomous_transcripts.append_attempt_manifest_record(manifest, {"external_session_id": "one"})
            second = fixer_autonomous_transcripts.append_attempt_manifest_record(manifest, {"external_session_id": "two"})
            self.assertEqual(first["attempt_number"], 1)
            self.assertEqual(second["attempt_number"], 2)
            lines = manifest.read_text(encoding="utf-8").splitlines()
            self.assertEqual(len(lines), 2)
            self.assertEqual(json.loads(lines[0])["external_session_id"], "one")

    def test_register_attempt_records_pi_continuation_evidence(self) -> None:
        import os
        from unittest.mock import patch

        with TemporaryDirectory() as tmp:
            fake_home = Path(tmp) / "home"
            cwd = Path(tmp) / "project"
            cwd.mkdir()
            # The Pi store is derived from HOME; pointing HOME at the fixture
            # exercises the real path derivation without any monkeypatched
            # lookup function.
            session_id = "01a0ecc4-attempt-run"
            home_root = fake_home / ".pi" / "agent" / "sessions"
            _write(
                home_root / fixer_autonomous_transcripts._pi_project_dir_name(cwd) / f"2026-10-05T10-00-00-000Z_{session_id}.jsonl",
                _pi_header(session_id, str(cwd), "2026-10-05T10:00:00.000Z") + "\n",
            )

            with patch.dict(os.environ, {"HOME": str(fake_home)}):
                manifest_path = fixer_autonomous._register_transcript_attempt_manifest(
                    cwd,
                    local_session_id=7,
                    global_session_id=70,
                    backend="pi",
                    external_session_id=session_id,
                    launch_started_at=time.time() - 3,
                    headless_log_path=cwd / ".codex" / "headless_netrunner_logs" / "session-7.log",
                    worker_pid=4242,
                )

            self.assertIsNotNone(manifest_path)
            record = json.loads(Path(str(manifest_path)).read_text(encoding="utf-8").splitlines()[0])
            self.assertEqual(record["attempt_number"], 1)
            self.assertEqual(record["local_session_id"], 7)
            self.assertEqual(record["global_session_id"], 70)
            self.assertEqual(record["external_session_id"], session_id)
            self.assertEqual(record["worker_pid"], 4242)
            self.assertEqual(len(record["transcripts"]), 1)
            self.assertEqual(record["transcripts"][0]["session_id"], session_id)
            self.assertEqual(record["transcripts"][0]["session_cwd"], str(cwd))

    def test_attempt_manifest_path_is_stable_and_worker_scoped(self) -> None:
        path = fixer_autonomous_transcripts.attempt_manifest_path(Path("/proj"), 12, "pi")
        self.assertEqual(path, Path("/proj/.codex/netrunner_attempt_manifests/session-12-pi.jsonl"))

    def test_proven_files_for_cwd_without_detected_id(self) -> None:
        """When detection fails, only files with proven id/cwd identity at or
        after the launch bound are recorded — never an arbitrary newest file,
        never another cwd, never a contradictory header."""
        with TemporaryDirectory() as tmp:
            root = Path(tmp) / "sessions"
            cwd = Path(tmp) / "worktree"
            cwd.mkdir()
            other_cwd = Path(tmp) / "other"
            other_cwd.mkdir()
            session_id = "01a0ecc4-undetected-run"
            proven = _write(
                root / fixer_autonomous_transcripts._pi_project_dir_name(cwd) / f"2026-10-05T10-00-00-000Z_{session_id}.jsonl",
                _pi_header(session_id, str(cwd), "2026-10-05T10:00:00.000Z") + "\n",
            )
            _write(
                root / "anywhere" / "2026-10-05T11-00-00-000Z_wrong-cwd.jsonl",
                _pi_header("wrong-cwd", str(other_cwd), "2026-10-05T11:00:00.000Z") + "\n",
            )
            _write(
                root / fixer_autonomous_transcripts._pi_project_dir_name(cwd) / "2026-10-05T12-00-00-000Z_declared-id.jsonl",
                _pi_header("someone-else", str(cwd), "2026-10-05T12:00:00.000Z") + "\n",
            )
            _write(
                root / fixer_autonomous_transcripts._pi_project_dir_name(cwd) / "2026-10-05T09-00-00-000Z_old-run.jsonl",
                _pi_header("old-run", str(cwd), "2026-10-05T09:00:00.000Z") + "\n",
            )

            records = fixer_autonomous_transcripts._pi_transcript_files_proven_for_cwd(
                cwd,
                since_epoch=fixer_autonomous_transcripts._pi_timestamp_epoch("2026-10-05T09:30:00.000Z"),
                sessions_root=root,
            )
            self.assertEqual([Path(str(record["path"])).name for record in records], [proven.name])
            self.assertEqual(records[0]["session_id"], session_id)
            self.assertEqual(records[0]["session_cwd"], str(cwd))

    def test_register_attempt_records_evidence_without_detected_id(self) -> None:
        import os
        from unittest.mock import patch

        with TemporaryDirectory() as tmp:
            fake_home = Path(tmp) / "home"
            project_cwd = Path(tmp) / "project"
            project_cwd.mkdir()
            worker_cwd = project_cwd / ".codex" / "netrunner_worktrees" / "wave-871" / "session-702"
            worker_cwd.mkdir(parents=True)
            session_id = "01a0ecc4-undetected-attempt"
            home_root = fake_home / ".pi" / "agent" / "sessions"
            _write(
                home_root / fixer_autonomous_transcripts._pi_project_dir_name(worker_cwd) / f"2026-10-05T10-00-00-000Z_{session_id}.jsonl",
                _pi_header(session_id, str(worker_cwd), "2026-10-05T10:00:00.000Z") + "\n",
            )

            with patch.dict(os.environ, {"HOME": str(fake_home)}):
                manifest_path = fixer_autonomous._register_transcript_attempt_manifest(
                    project_cwd,
                    local_session_id=702,
                    global_session_id=4485,
                    backend="pi",
                    external_session_id=None,
                    launch_started_at=fixer_autonomous_transcripts._pi_timestamp_epoch("2026-10-05T09:59:00.000Z") or 0.0,
                    headless_log_path=project_cwd / ".codex" / "netrunner_wave_artifacts" / "wave-871" / "session-702" / "headless-pi.log",
                    worker_pid=4242,
                    worker_cwd=worker_cwd,
                )

            self.assertIsNotNone(manifest_path)
            record = json.loads(Path(str(manifest_path)).read_text(encoding="utf-8").splitlines()[0])
            self.assertEqual(record["external_session_id"], "")
            self.assertEqual(record["worker_cwd"], str(worker_cwd))
            self.assertEqual(record["attempt_number"], 1)
            self.assertEqual(len(record["transcripts"]), 1)
            self.assertEqual(record["transcripts"][0]["session_id"], session_id)
            self.assertEqual(record["transcripts"][0]["session_cwd"], str(worker_cwd))

    def test_manifest_keeps_stored_path_project_scoped_for_wave_workers(self) -> None:
        import os
        from unittest.mock import patch

        with TemporaryDirectory() as tmp:
            fake_home = Path(tmp) / "home"
            project_cwd = Path(tmp) / "project"
            project_cwd.mkdir()
            worker_cwd = project_cwd / ".codex" / "netrunner_worktrees" / "wave-871" / "session-703"
            worker_cwd.mkdir(parents=True)

            with patch.dict(os.environ, {"HOME": str(fake_home)}):
                manifest_path = fixer_autonomous._register_transcript_attempt_manifest(
                    project_cwd,
                    local_session_id=703,
                    global_session_id=4486,
                    backend="pi",
                    external_session_id=None,
                    launch_started_at=time.time() - 3,
                    headless_log_path=None,
                    worker_pid=7,
                    worker_cwd=worker_cwd,
                )

            self.assertEqual(
                manifest_path,
                project_cwd / ".codex" / "netrunner_attempt_manifests" / "session-703-pi.jsonl",
            )


if __name__ == "__main__":
    unittest.main()
