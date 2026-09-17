from __future__ import annotations

import tempfile
import unittest
from pathlib import Path

from client_wires.backends.base import (
    FIXER_ROLE_SKILL_NAMES,
    materialize_codex_project_skills,
)


class ProjectDocBundleSkillMaterializationTests(unittest.TestCase):
    def test_export_skill_is_allowlisted_and_materialized_from_canonical_source(self) -> None:
        skill_name = "export-project-doc-bundle"
        self.assertIn(skill_name, FIXER_ROLE_SKILL_NAMES)

        repo_root = Path(__file__).resolve().parents[2]
        canonical_skill = repo_root / ".agents" / "skills" / skill_name / "SKILL.md"
        self.assertTrue(canonical_skill.is_file())

        with tempfile.TemporaryDirectory() as tmp:
            target = Path(tmp)
            materialize_codex_project_skills(target, [skill_name])

            materialized_skill = target / ".agents" / "skills" / skill_name / "SKILL.md"
            self.assertEqual(
                materialized_skill.read_text(encoding="utf-8"),
                canonical_skill.read_text(encoding="utf-8"),
            )


if __name__ == "__main__":
    unittest.main()
