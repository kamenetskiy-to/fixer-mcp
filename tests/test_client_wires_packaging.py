from __future__ import annotations

import importlib
import os
import tarfile
import tempfile
import unittest
import zipfile
from pathlib import Path


REPO_ROOT = Path(__file__).resolve().parents[1]
PACKAGE_ROOT = REPO_ROOT / "packages" / "client-wires"


class ClientWiresPackagingTest(unittest.TestCase):
    def test_release_artifacts_include_bundled_runtime_config_skills_and_claude_backend(self) -> None:
        backend = importlib.import_module("setuptools.build_meta")

        with tempfile.TemporaryDirectory() as temp_dir:
            output_dir = Path(temp_dir)
            previous_cwd = Path.cwd()
            try:
                os.chdir(PACKAGE_ROOT)
                sdist_name = backend.build_sdist(str(output_dir), {})
                wheel_name = backend.build_wheel(str(output_dir), {})
            finally:
                os.chdir(previous_cwd)

            with tarfile.open(output_dir / sdist_name, "r:gz") as archive:
                sdist_names = set(archive.getnames())
            with zipfile.ZipFile(output_dir / wheel_name) as archive:
                wheel_names = set(archive.namelist())

        self.assertIn("fixer_client_wires/backends/claude.py", wheel_names)
        self.assertIn("fixer_client_wires/data/backend-catalog.json", wheel_names)
        self.assertIn("fixer_client_wires/staged/config/mcp-config.json", wheel_names)
        self.assertIn("fixer_client_wires/staged/runtime/fixer_runtime/__init__.py", wheel_names)
        self.assertIn("fixer_client_wires/staged/skills/start-fixer/SKILL.md", wheel_names)
        self.assertIn("fixer_client_wires-0.1.0/src/fixer_client_wires/backends/claude.py", sdist_names)
        self.assertIn("fixer_client_wires-0.1.0/src/fixer_client_wires/staged/config/mcp-config.json", sdist_names)
        self.assertIn("fixer_client_wires-0.1.0/src/fixer_client_wires/staged/skills/start-fixer/SKILL.md", sdist_names)


if __name__ == "__main__":
    unittest.main()
