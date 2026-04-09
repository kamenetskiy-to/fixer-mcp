from __future__ import annotations

import io
import json
import tarfile
import tempfile
import unittest
import zipfile
from contextlib import redirect_stdout
from pathlib import Path
import sys

REPO_ROOT = Path(__file__).resolve().parents[1]
SCRIPTS_ROOT = REPO_ROOT / "scripts"
if str(SCRIPTS_ROOT) not in sys.path:
    sys.path.insert(0, str(SCRIPTS_ROOT))

import release_public_repo


class ReleasePublicRepoTest(unittest.TestCase):
    def _write_repo_layout(self, repo_root: Path) -> None:
        repo_root.mkdir(parents=True, exist_ok=True)
        (repo_root / "README.md").write_text("# repo\n", encoding="utf-8")
        (repo_root / "apps").mkdir(parents=True, exist_ok=True)
        (repo_root / "apps" / "fixer-desktop").mkdir(parents=True, exist_ok=True)
        (repo_root / "apps" / "fixer-desktop" / "pubspec.yaml").write_text(
            "name: fixer_desktop\n",
            encoding="utf-8",
        )
        (repo_root / "docs").mkdir(parents=True, exist_ok=True)
        (repo_root / "docs" / "architecture.md").write_text("# architecture\n", encoding="utf-8")

        client_wires = repo_root / "packages" / "client-wires"
        compat_bridge = repo_root / "packages" / "compat-bridge"
        desktop_bridge = repo_root / "packages" / "desktop-bridge"
        fixer_server = repo_root / "packages" / "fixer-mcp-server"
        for package in (client_wires, compat_bridge, desktop_bridge, fixer_server):
            package.mkdir(parents=True, exist_ok=True)

        (client_wires / "pyproject.toml").write_text(
            "[build-system]\n"
            "requires=['setuptools>=68']\n"
            "build-backend='setuptools.build_meta'\n\n"
            "[project]\n"
            "name='fixer-client-wires'\n"
            "version='0.1.0'\n",
            encoding="utf-8",
        )
        (compat_bridge / "pyproject.toml").write_text(
            "[build-system]\n"
            "requires=['setuptools>=68']\n"
            "build-backend='setuptools.build_meta'\n\n"
            "[project]\n"
            "name='fixer-compat-bridge'\n"
            "version='0.1.0'\n",
            encoding="utf-8",
        )
        (desktop_bridge / "pyproject.toml").write_text(
            "[build-system]\n"
            "requires=['setuptools>=68']\n"
            "build-backend='setuptools.build_meta'\n\n"
            "[project]\n"
            "name='fixer-desktop-bridge'\n"
            "version='0.1.0'\n",
            encoding="utf-8",
        )
        (fixer_server / "go.mod").write_text("module fixer_mcp\n", encoding="utf-8")

    def test_build_release_plan_covers_repo_native_artifacts(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            repo_root = Path(temp_dir) / "github_repo"
            self._write_repo_layout(repo_root)

            plan = release_public_repo.build_release_plan(version="1.2.3", repo_root=repo_root)

        self.assertEqual(plan.version, "1.2.3")
        self.assertEqual(plan.release_dir, (repo_root / "dist" / "releases" / "1.2.3").resolve())
        self.assertEqual(
            plan.assembly_dir,
            (repo_root / "dist" / "releases" / "1.2.3" / "assembly" / "github_repo").resolve(),
        )
        self.assertEqual(plan.steps[0].command[:4], ("python3", "-m", "unittest", "discover"))
        self.assertEqual(plan.steps[1].command[:2], ("go", "test"))
        self.assertEqual(plan.steps[2].kind, "python-pep517-build")
        self.assertEqual(plan.steps[2].build_backend, "setuptools.build_meta")
        self.assertIn("client-wires", str(plan.steps[2].outputs[0]))
        self.assertEqual(plan.steps[3].kind, "python-pep517-build")
        self.assertEqual(plan.steps[3].build_backend, "setuptools.build_meta")
        self.assertIn("compat-bridge", str(plan.steps[3].outputs[0]))
        self.assertTrue(str(plan.steps[4].outputs[0]).endswith("fixer_mcp"))

    def test_write_release_manifest_records_step_metadata(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            repo_root = Path(temp_dir) / "github_repo"
            self._write_repo_layout(repo_root)
            plan = release_public_repo.build_release_plan(
                version="2.0.0",
                repo_root=repo_root,
                include_tests=False,
            )

            manifest_path = release_public_repo.write_release_manifest(plan)

            payload = json.loads(manifest_path.read_text(encoding="utf-8"))

        self.assertEqual(payload["version"], "2.0.0")
        self.assertEqual(payload["release_dir"], str(plan.release_dir))
        self.assertEqual(payload["assembly_dir"], str(plan.assembly_dir))
        self.assertEqual(payload["assembly_manifest"], str(plan.release_dir / "assembly-manifest.json"))
        self.assertIn("packages", payload["assembly_included_paths"])
        self.assertEqual(len(payload["steps"]), 3)
        self.assertEqual(payload["steps"][0]["name"], "build fixer-client-wires package")
        self.assertEqual(payload["steps"][0]["kind"], "python-pep517-build")
        self.assertEqual(payload["steps"][0]["build_backend"], "setuptools.build_meta")

    def test_write_assembly_manifest_copies_canonical_repo_surface(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            repo_root = Path(temp_dir) / "github_repo"
            self._write_repo_layout(repo_root)
            (repo_root / "examples").mkdir(parents=True, exist_ok=True)
            (repo_root / "examples" / "mcp-config.example.json").write_text("{}", encoding="utf-8")
            (repo_root / "scripts").mkdir(parents=True, exist_ok=True)
            (repo_root / "scripts" / "release_public_repo.py").write_text("# script\n", encoding="utf-8")
            (repo_root / "tests").mkdir(parents=True, exist_ok=True)
            (repo_root / "tests" / "test_scaffold.py").write_text("# tests\n", encoding="utf-8")
            pycache_dir = repo_root / "packages" / "client-wires" / "__pycache__"
            pycache_dir.mkdir(parents=True, exist_ok=True)
            (pycache_dir / "ignored.pyc").write_bytes(b"compiled")

            plan = release_public_repo.build_release_plan(
                version="2.1.0",
                repo_root=repo_root,
                include_tests=False,
            )
            manifest_path = release_public_repo.write_assembly_manifest(plan)

            payload = json.loads(manifest_path.read_text(encoding="utf-8"))

            self.assertEqual(payload["assembly_dir"], str(plan.assembly_dir))
            self.assertIn("README.md", payload["included_paths"])
            self.assertIn("packages", payload["included_paths"])
            self.assertTrue((plan.assembly_dir / "README.md").is_file())
            self.assertTrue((plan.assembly_dir / "packages" / "client-wires" / "pyproject.toml").is_file())
            self.assertFalse((plan.assembly_dir / "packages" / "client-wires" / "__pycache__").exists())

    def test_main_dry_run_json_prints_release_plan(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            repo_root = Path(temp_dir) / "github_repo"
            self._write_repo_layout(repo_root)
            stdout = io.StringIO()

            with redirect_stdout(stdout):
                exit_code = release_public_repo.main(
                    ["--repo-root", str(repo_root), "--version", "3.0.0", "--dry-run", "--json"]
                )

            payload = json.loads(stdout.getvalue())

        self.assertEqual(exit_code, 0)
        self.assertEqual(payload["version"], "3.0.0")
        self.assertEqual(payload["release_dir"], str((repo_root / "dist" / "releases" / "3.0.0").resolve()))
        self.assertEqual(
            payload["assembly_dir"],
            str((repo_root / "dist" / "releases" / "3.0.0" / "assembly" / "github_repo").resolve()),
        )
        self.assertIn("apps", payload["assembly_included_paths"])
        self.assertIn("docs", payload["assembly_included_paths"])
        self.assertEqual(payload["steps"][0]["name"], "repo python tests")

    def test_run_release_plan_builds_python_artifacts_without_build_frontend(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            repo_root = Path(temp_dir) / "github_repo"
            self._write_repo_layout(repo_root)
            (repo_root / "examples").mkdir(parents=True, exist_ok=True)
            (repo_root / "examples" / "mcp-config.example.json").write_text("{}", encoding="utf-8")
            (repo_root / "scripts").mkdir(parents=True, exist_ok=True)
            (repo_root / "scripts" / "release_public_repo.py").write_text("# script\n", encoding="utf-8")
            (repo_root / "tests").mkdir(parents=True, exist_ok=True)
            (repo_root / "tests" / "test_smoke.py").write_text(
                "import unittest\n\n"
                "class SmokeTest(unittest.TestCase):\n"
                "    def test_ok(self) -> None:\n"
                "        self.assertTrue(True)\n\n"
                "if __name__ == '__main__':\n"
                "    unittest.main()\n",
                encoding="utf-8",
            )
            (repo_root / "packages" / "client-wires" / "README.md").write_text(
                "# client wires\n",
                encoding="utf-8",
            )
            (repo_root / "packages" / "compat-bridge" / "README.md").write_text(
                "# compat bridge\n",
                encoding="utf-8",
            )
            (repo_root / "packages" / "fixer-mcp-server" / "main.go").write_text(
                "package main\n\nfunc main() {}\n",
                encoding="utf-8",
            )
            client_src = repo_root / "packages" / "client-wires" / "src" / "fixer_client_wires"
            compat_src = repo_root / "packages" / "compat-bridge" / "src" / "fixer_compat_bridge"
            client_src.mkdir(parents=True, exist_ok=True)
            compat_src.mkdir(parents=True, exist_ok=True)
            (client_src / "__init__.py").write_text("__version__ = '0.1.0'\n", encoding="utf-8")
            (compat_src / "__init__.py").write_text("__version__ = '0.1.0'\n", encoding="utf-8")
            (client_src / "cli.py").write_text(
                "def main() -> int:\n"
                "    return 0\n",
                encoding="utf-8",
            )
            (compat_src / "cli.py").write_text(
                "def main() -> int:\n"
                "    return 0\n",
                encoding="utf-8",
            )

            plan = release_public_repo.build_release_plan(
                version="4.0.0",
                repo_root=repo_root,
                include_tests=False,
            )
            manifest_path = release_public_repo.run_release_plan(plan)
            manifest_exists = manifest_path.is_file()

            client_dist = plan.release_dir / "python" / "client-wires"
            compat_dist = plan.release_dir / "python" / "compat-bridge"
            client_files = sorted(path.name for path in client_dist.iterdir())
            compat_files = sorted(path.name for path in compat_dist.iterdir())
            with tarfile.open(client_dist / "fixer_client_wires-0.1.0.tar.gz", "r:gz") as archive:
                client_sdist_names = archive.getnames()
            with zipfile.ZipFile(compat_dist / "fixer_compat_bridge-0.1.0-py3-none-any.whl") as archive:
                compat_wheel_names = archive.namelist()

        self.assertTrue(manifest_exists)
        self.assertEqual(
            client_files,
            ["fixer_client_wires-0.1.0-py3-none-any.whl", "fixer_client_wires-0.1.0.tar.gz"],
        )
        self.assertEqual(
            compat_files,
            ["fixer_compat_bridge-0.1.0-py3-none-any.whl", "fixer_compat_bridge-0.1.0.tar.gz"],
        )
        self.assertIn("fixer_client_wires-0.1.0/pyproject.toml", client_sdist_names)
        self.assertIn("fixer_compat_bridge/__init__.py", compat_wheel_names)


if __name__ == "__main__":
    unittest.main()
