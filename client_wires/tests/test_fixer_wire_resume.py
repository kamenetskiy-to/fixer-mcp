from __future__ import annotations

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
from client_wires import fixer_wire_resume


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
    module = types.ModuleType("client_wires.codex_compat.sessions")

    def _load_session_summaries(_history_path: Path, *, limit: int, cwd_filter: Path | None = None) -> list[types.SimpleNamespace]:
        del cwd_filter
        return summaries[:limit]

    def _find_session_log(session_id: str, *, created: datetime, updated: datetime) -> Path | None:
        del created, updated
        return log_paths.get(session_id)

    module._load_session_summaries = _load_session_summaries
    module._find_session_log = _find_session_log
    return module


class FixerWireResumeTests(unittest.TestCase):
    def test_module_marker_helpers_detect_role_markers(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            fixer_log = Path(tmp) / "fixer.jsonl"
            fixer_log.write_text(
                '\n'.join(
                    [
                        '{"type":"session_meta"}',
                        '{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"Activate skill `$init-fixer` immediately."}]}}',
                    ]
                ),
                encoding="utf-8",
            )
            netrunner_log = Path(tmp) / "netrunner.jsonl"
            netrunner_log.write_text(
                "Activate skill `$run-manual-netrunner` immediately.\nPreselected session ID from fixer wire: `34`.\n",
                encoding="utf-8",
            )
            acceptance_log = Path(tmp) / "acceptance.jsonl"
            acceptance_log.write_text(
                "Activate skill `$run-manual-acceptance-netrunner` immediately.\n"
                "Preselected session ID from fixer wire: `34`.\n",
                encoding="utf-8",
            )

            self.assertTrue(
                fixer_wire_resume.session_log_has_fixer_marker(
                    fixer_log,
                    fixer_skill_markers=fixer_wire.FIXER_SKILL_MARKERS,
                )
            )
            self.assertTrue(
                fixer_wire_resume.session_log_has_netrunner_marker(
                    netrunner_log,
                    34,
                    netrunner_skill_markers=fixer_wire.NETRUNNER_SKILL_MARKERS,
                )
            )
            self.assertTrue(
                fixer_wire_resume.session_log_has_netrunner_marker(
                    acceptance_log,
                    34,
                    netrunner_skill_markers=fixer_wire.NETRUNNER_SKILL_MARKERS,
                )
            )

    def test_facade_marker_helpers_stay_callable(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            log_path = Path(tmp) / "canonical.jsonl"
            log_path.write_text(
                "Activate skill `$run-manual-netrunner` immediately.\nPreselected session ID from fixer wire: `34`.\n",
                encoding="utf-8",
            )

            self.assertTrue(fixer_wire._session_log_has_netrunner_marker(log_path, 34))

    def test_load_resume_summaries_separates_fixer_and_netrunner_threads(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            fixer_log = Path(tmp) / "fixer.jsonl"
            fixer_log.write_text(
                'Activate skill `$init-fixer` immediately.\n',
                encoding="utf-8",
            )
            netrunner_log = Path(tmp) / "netrunner.jsonl"
            netrunner_log.write_text(
                (
                    'Activate skill `$run-manual-netrunner` immediately.\n'
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

            with patch.dict(sys.modules, {"client_wires.codex_compat.sessions": history_module}):
                fixer_summaries = fixer_wire._load_fixer_resume_summaries(Path("/tmp/project"))
                netrunner_summaries = fixer_wire._load_netrunner_resume_summaries(Path("/tmp/project"), 34)

        self.assertEqual([summary.session_id for summary in fixer_summaries], ["fixer-123"])
        self.assertEqual([summary.session_id for summary in netrunner_summaries], ["runner-456"])

    def test_load_resume_summaries_accepts_legacy_start_fixer_marker(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            fixer_log = Path(tmp) / "legacy-fixer.jsonl"
            fixer_log.write_text(
                'Activate skill `$start-fixer` immediately.\n',
                encoding="utf-8",
            )
            summary = _make_history_summary("legacy-fixer-123", preview="Legacy Fixer thread")
            history_module = _fake_codex_history_module([summary], {"legacy-fixer-123": fixer_log})

            with patch.dict(sys.modules, {"client_wires.codex_compat.sessions": history_module}):
                fixer_summaries = fixer_wire._load_fixer_resume_summaries(Path("/tmp/project"))

        self.assertEqual([summary.session_id for summary in fixer_summaries], ["legacy-fixer-123"])

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
                'Activate skill `$init-overseer` immediately.\n',
                encoding="utf-8",
            )
            summary = _make_history_summary("overseer-aliased", preview="Overseer thread")
            history_module = _fake_codex_history_module(
                [summary],
                {"overseer-aliased": overseer_log},
            )

            with (
                patch.dict(sys.modules, {"client_wires.codex_compat.sessions": history_module}),
                patch.dict(os.environ, {fixer_wire.FIXER_DB_PATH_ENV: str(db_path)}),
            ):
                fixer_summaries = fixer_wire._load_fixer_resume_summaries(project_cwd)

        self.assertEqual([item.session_id for item in fixer_summaries], ["overseer-aliased"])

    @unittest.skip("public export excludes private provider cache directories")
    def test_load_fixer_resume_summaries_discovers_claude_droid_and_junie_threads(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp) / "home"
            cwd = Path(tmp) / "workspace" / "self_orchestration"
            cwd.mkdir(parents=True)
            slug = "-".join(str(cwd.resolve()).split("/"))
            slug = f"-{slug}" if not slug.startswith("-") else slug
            slug = slug.replace("_", "-")

            codex_log = Path(tmp) / "codex.jsonl"
            codex_log.write_text('Activate skill `$init-fixer` immediately.\n', encoding="utf-8")
            codex_summary = _make_history_summary(
                "codex-fixer",
                preview="Codex Fixer",
                updated=datetime(2026, 2, 1, 12, 0, tzinfo=timezone.utc),
            )
            history_module = _fake_codex_history_module([codex_summary], {"codex-fixer": codex_log})

            claude_dir = home / ".claude" / "projects" / slug
            claude_dir.mkdir(parents=True)
            claude_log = claude_dir / "claude-fixer.jsonl"
            claude_log.write_text(
                "\n".join(
                    [
                        '{"type":"mode","sessionId":"claude-fixer"}',
                        '{"type":"user","message":{"role":"user","content":"Activate skill `$init-fixer` immediately."},"timestamp":"2026-02-01T13:00:00Z","cwd":"'
                        + str(cwd.resolve())
                        + '","sessionId":"claude-fixer"}',
                    ]
                ),
                encoding="utf-8",
            )

            droid_dir = home / ".factory" / "sessions" / slug
            droid_dir.mkdir(parents=True)
            droid_log = droid_dir / "droid-fixer.jsonl"
            droid_log.write_text(
                "\n".join(
                    [
                        '{"type":"session_start","id":"droid-fixer","title":"Droid Fixer","cwd":"'
                        + str(cwd.resolve())
                        + '"}',
                        '{"type":"message","timestamp":"2026-02-01T14:00:00Z","message":{"role":"user","content":[{"type":"text","text":"Activate skill `$init-fixer` immediately."}]}}',
                    ]
                ),
                encoding="utf-8",
            )

            junie_root = home / ".junie" / "sessions"
            junie_root.mkdir(parents=True)
            (junie_root / "index.jsonl").write_text(
                '{"sessionId":"junie-fixer","createdAt":1770000000000,"updatedAt":1770003600000,"projectDir":"'
                + str(cwd.resolve())
                + '","taskName":"Junie Fixer"}\n',
                encoding="utf-8",
            )
            junie_dir = junie_root / "junie-fixer"
            junie_dir.mkdir()
            (junie_dir / "state.json").write_text(
                '{"issue":{"description":"Activate skill `$init-fixer` immediately."}}\n',
                encoding="utf-8",
            )

            with (
                patch.dict(sys.modules, {"client_wires.codex_compat.sessions": history_module}),
                patch.object(Path, "home", return_value=home),
            ):
                fixer_summaries = fixer_wire._load_fixer_resume_summaries(cwd, limit=10)

        by_provider = {
            (fixer_wire_resume.summary_provider(summary), summary.session_id)
            for summary in fixer_summaries
        }
        self.assertIn(("codex", "codex-fixer"), by_provider)
        self.assertIn(("claude", "claude-fixer"), by_provider)
        self.assertIn(("droid", "droid-fixer"), by_provider)
        self.assertIn(("junie", "junie-fixer"), by_provider)

    def test_preview_from_records_reads_claude_message_content_not_role(self) -> None:
        preview = fixer_wire_resume._preview_from_records(
            [
                {
                    "type": "user",
                    "message": {
                        "role": "user",
                        "content": [
                            {
                                "type": "text",
                                "text": "Activate skill `$init-fixer` immediately.",
                            }
                        ],
                    },
                }
            ],
            fallback="fallback",
        )

        self.assertEqual(preview, "Activate skill `$init-fixer` immediately.")

    def test_preview_from_records_skips_non_informative_leading_user_texts(self) -> None:
        preview = fixer_wire_resume._preview_from_records(
            [
                {
                    "type": "user",
                    "message": {
                        "role": "user",
                        "content": "<system-reminder>Use the workspace.</system-reminder>",
                    },
                },
                {
                    "type": "user",
                    "message": {"role": "user", "content": [{"type": "text", "text": "<command-name>/status"}]},
                },
                {
                    "type": "message",
                    "message": {
                        "role": "user",
                        "content": [{"type": "text", "text": "<local-command-caveat>Output omitted."}],
                    },
                },
                {
                    "type": "user",
                    "message": {
                        "role": "user",
                        "content": [{"type": "text", "text": "<task-notification>Task changed."}],
                    },
                },
                {
                    "type": "user",
                    "message": {
                        "role": "user",
                        "content": [{"type": "text", "text": "Caveat: local shell output"}],
                    },
                },
                {
                    "type": "user",
                    "message": {
                        "role": "user",
                        "content": [{"type": "text", "text": "Please resume the Fixer for this project."}],
                    },
                },
            ],
            fallback="fallback",
        )

        self.assertEqual(preview, "Please resume the Fixer for this project.")

    def test_load_fixer_resume_summaries_excludes_unmapped_antigravity_history(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp) / "home"
            cwd = Path(tmp) / "workspace" / "self_orchestration"
            cwd.mkdir(parents=True)
            history_dir = home / ".gemini" / "antigravity-cli"
            history_dir.mkdir(parents=True)
            (history_dir / "history.jsonl").write_text(
                '{"display":"Activate skill `$init-fixer` immediately.","timestamp":1770000000000,"workspace":"'
                + str(cwd.resolve())
                + '","conversationId":"agy-fixer"}\n',
                encoding="utf-8",
            )
            history_module = _fake_codex_history_module([], {})

            with (
                patch.dict(sys.modules, {"client_wires.codex_compat.sessions": history_module}),
                patch.object(Path, "home", return_value=home),
            ):
                fixer_summaries = fixer_wire._load_fixer_resume_summaries(cwd, limit=10)

        self.assertNotIn(
            ("antigravity", "agy-fixer"),
            {(fixer_wire_resume.summary_provider(summary), summary.session_id) for summary in fixer_summaries},
        )

    def test_latest_matching_netrunner_uses_facade_loader_patch(self) -> None:
        summary = _make_history_summary("resume-139", preview="Existing netrunner")
        with patch.object(fixer_wire, "_load_netrunner_resume_summaries", return_value=[summary]) as patched:
            resolved = fixer_wire._latest_matching_netrunner_codex_session_id(Path("/tmp/project"), 139)

        self.assertEqual(resolved, "resume-139")
        patched.assert_called_once_with(Path("/tmp/project"), 139, limit=8)


if __name__ == "__main__":
    unittest.main()
