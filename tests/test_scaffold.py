from __future__ import annotations

import unittest
from pathlib import Path


REPO_ROOT = Path(__file__).resolve().parents[1]


class GithubRepoScaffoldTest(unittest.TestCase):
    def test_expected_scaffold_exists(self) -> None:
        expected_paths = [
            REPO_ROOT / "README.md",
            REPO_ROOT / "docs" / "architecture.md",
            REPO_ROOT / "docs" / "migration-plan.md",
            REPO_ROOT / "docs" / "implementation-slices.md",
            REPO_ROOT / "docs" / "compatibility.md",
            REPO_ROOT / "docs" / "onboarding.md",
            REPO_ROOT / "docs" / "release.md",
            REPO_ROOT / "examples" / "mcp-config.example.json",
            REPO_ROOT / "scripts" / "release_public_repo.py",
            REPO_ROOT / "packages" / "fixer-mcp-server" / "README.md",
            REPO_ROOT / "packages" / "fixer-mcp-server" / "go.mod",
            REPO_ROOT / "packages" / "fixer-mcp-server" / "main.go",
            REPO_ROOT / "packages" / "fixer-mcp-server" / "main_test.go",
            REPO_ROOT / "packages" / "fixer-mcp-server" / "Makefile",
            REPO_ROOT / "packages" / "fixer-mcp-server" / ".gitignore",
            REPO_ROOT / "packages" / "fixer-mcp-server" / "examples" / "mcp-config.example.json",
            REPO_ROOT / "packages" / "client-wires" / "README.md",
            REPO_ROOT / "packages" / "client-wires" / "config" / "mcp-config.json",
            REPO_ROOT / "packages" / "client-wires" / "examples" / "mcp-config.example.json",
            REPO_ROOT / "packages" / "client-wires" / "pyproject.toml",
            REPO_ROOT / "packages" / "client-wires" / "src" / "fixer_client_wires" / "__main__.py",
            REPO_ROOT / "packages" / "client-wires" / "src" / "fixer_client_wires" / "cli.py",
            REPO_ROOT / "packages" / "client-wires" / "src" / "fixer_client_wires" / "launcher.py",
            REPO_ROOT / "packages" / "client-wires" / "src" / "fixer_client_wires" / "bootstrap.py",
            REPO_ROOT / "packages" / "client-wires" / "src" / "fixer_client_wires" / "backends" / "__init__.py",
            REPO_ROOT / "packages" / "client-wires" / "src" / "fixer_client_wires" / "backends" / "base.py",
            REPO_ROOT / "packages" / "client-wires" / "src" / "fixer_client_wires" / "backends" / "codex.py",
            REPO_ROOT / "packages" / "client-wires" / "src" / "fixer_client_wires" / "backends" / "droid.py",
            REPO_ROOT / "packages" / "client-wires" / "runtime" / "fixer_runtime" / "__init__.py",
            REPO_ROOT / "packages" / "compat-bridge" / "README.md",
            REPO_ROOT / "packages" / "compat-bridge" / "pyproject.toml",
            REPO_ROOT / "packages" / "compat-bridge" / "src" / "fixer_compat_bridge" / "__init__.py",
            REPO_ROOT / "packages" / "compat-bridge" / "src" / "fixer_compat_bridge" / "__main__.py",
            REPO_ROOT / "packages" / "compat-bridge" / "src" / "fixer_compat_bridge" / "cli.py",
        ]

        for path in expected_paths:
            self.assertTrue(path.is_file(), msg=f"missing scaffold file: {path}")

    def test_docs_capture_key_migration_claims(self) -> None:
        readme = (REPO_ROOT / "README.md").read_text(encoding="utf-8")
        architecture = (REPO_ROOT / "docs" / "architecture.md").read_text(encoding="utf-8")
        migration = (REPO_ROOT / "docs" / "migration-plan.md").read_text(encoding="utf-8")

        self.assertIn("FIXER_CLIENT_WIRES_RUNTIME_ROOT", readme)
        self.assertIn("../mcp_servers", architecture)
        self.assertIn("copy-and-strip export", readme)
        self.assertIn("compat-bridge", architecture)
        self.assertIn("MCP_SERVERS_ROOT", architecture)
        self.assertIn("package-local runtime", architecture)
        self.assertIn("FIXER_CLIENT_WIRES_CONFIG_PATH", architecture)
        self.assertIn("plan-launch", readme)
        self.assertIn("list-roles", readme)
        self.assertIn("fixer_compat_bridge", readme)
        self.assertIn("docs/onboarding.md", readme)
        self.assertIn("scripts/release_public_repo.py", readme)
        self.assertIn("codex", architecture)
        self.assertIn("droid", architecture)
        self.assertIn("release-manifest.json", architecture)
        self.assertIn("assembly-manifest.json", architecture)
        self.assertIn("assembly/github_repo", architecture)
        self.assertIn("assembly/github_repo", readme)
        self.assertIn("Phase 5: Export Retirement", migration)
        self.assertIn("scripts/release_public_repo.py", migration)
        self.assertIn("packages/fixer-mcp-server", architecture)
        self.assertIn("package-local example", architecture)
        self.assertIn("fixer-compat-bridge", migration)


if __name__ == "__main__":
    unittest.main()
