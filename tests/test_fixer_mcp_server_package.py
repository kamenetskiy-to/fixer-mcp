from __future__ import annotations

import json
import unittest
from pathlib import Path


REPO_ROOT = Path(__file__).resolve().parents[1]
PACKAGE_ROOT = REPO_ROOT / "packages" / "fixer-mcp-server"


class FixerMcpServerPackageTest(unittest.TestCase):
    def test_package_contains_staged_go_module(self) -> None:
        expected_paths = [
            PACKAGE_ROOT / "go.mod",
            PACKAGE_ROOT / "go.sum",
            PACKAGE_ROOT / "main.go",
            PACKAGE_ROOT / "main_test.go",
            PACKAGE_ROOT / "migrate.go",
            PACKAGE_ROOT / "migrate_docs.go",
            PACKAGE_ROOT / "cmd" / "project_doc_hard_replace" / "main.go",
            PACKAGE_ROOT / "cmd" / "project_doc_hard_replace" / "main_test.go",
        ]

        for path in expected_paths:
            self.assertTrue(path.is_file(), msg=f"missing staged server file: {path}")

    def test_package_docs_and_examples_are_public_repo_safe(self) -> None:
        readme = (PACKAGE_ROOT / "README.md").read_text(encoding="utf-8")
        gitignore = (PACKAGE_ROOT / ".gitignore").read_text(encoding="utf-8")
        example = json.loads((PACKAGE_ROOT / "examples" / "mcp-config.example.json").read_text(encoding="utf-8"))

        self.assertIn("make test", readme)
        self.assertIn("make build", readme)
        self.assertIn("local state", readme)
        self.assertIn("fixer.db", gitignore)
        self.assertIn("migration_backups/", gitignore)

        server = example["mcpServers"]["fixer_mcp"]
        self.assertEqual(server["command"], "bash")
        self.assertIn("/path/to/github_repo/packages/fixer-mcp-server", server["args"][1])
        self.assertNotIn("/Users/hensybex", server["args"][1])


if __name__ == "__main__":
    unittest.main()
