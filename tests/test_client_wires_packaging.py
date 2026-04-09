from __future__ import annotations

import importlib
import os
import tarfile
import tempfile
import unittest
import zipfile
from pathlib import Path
import configparser
import shutil


REPO_ROOT = Path(__file__).resolve().parents[1]
PACKAGE_ROOT = REPO_ROOT / "packages" / "client-wires"


class ClientWiresPackagingTest(unittest.TestCase):
    def _clean_build_artifacts(self) -> None:
        shutil.rmtree(PACKAGE_ROOT / "build", ignore_errors=True)
        shutil.rmtree(PACKAGE_ROOT / "src" / "fixer_client_wires.egg-info", ignore_errors=True)

    def test_release_artifacts_include_bundled_runtime_config_skills_and_claude_backend(self) -> None:
        backend = importlib.import_module("setuptools.build_meta")

        with tempfile.TemporaryDirectory() as temp_dir:
            output_dir = Path(temp_dir)
            previous_cwd = Path.cwd()
            try:
                os.chdir(PACKAGE_ROOT)
                self._clean_build_artifacts()
                sdist_name = backend.build_sdist(str(output_dir), {})
                wheel_name = backend.build_wheel(str(output_dir), {})
            finally:
                os.chdir(previous_cwd)

            with tarfile.open(output_dir / sdist_name, "r:gz") as archive:
                sdist_names = set(archive.getnames())
            with zipfile.ZipFile(output_dir / wheel_name) as archive:
                wheel_names = set(archive.namelist())
                entry_points = archive.read("fixer_client_wires-0.1.0.dist-info/entry_points.txt").decode("utf-8")

        self.assertIn("fixer_client_wires/backends/claude.py", wheel_names)
        self.assertIn("fixer_client_wires/data/backend-catalog.json", wheel_names)
        self.assertIn("fixer_client_wires/staged/config/mcp-config.json", wheel_names)
        self.assertIn("fixer_client_wires/staged/runtime/fixer_runtime/__init__.py", wheel_names)
        self.assertIn("fixer_client_wires/staged/skills/start-fixer/SKILL.md", wheel_names)
        self.assertIn("fixer_client_wires-0.1.0/src/fixer_client_wires/backends/claude.py", sdist_names)
        self.assertIn("fixer_client_wires-0.1.0/src/fixer_client_wires/staged/config/mcp-config.json", sdist_names)
        self.assertIn("fixer_client_wires-0.1.0/src/fixer_client_wires/staged/skills/start-fixer/SKILL.md", sdist_names)

        parser = configparser.ConfigParser()
        parser.read_string(entry_points)
        self.assertEqual(parser["console_scripts"]["fixer"], "fixer_client_wires.cli:main")


if __name__ == "__main__":
    unittest.main()
