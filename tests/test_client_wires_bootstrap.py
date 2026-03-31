from __future__ import annotations

import tempfile
import unittest
from pathlib import Path
import sys

REPO_ROOT = Path(__file__).resolve().parents[1]
CLIENT_WIRES_SRC = REPO_ROOT / "packages" / "client-wires" / "src"
if str(CLIENT_WIRES_SRC) not in sys.path:
    sys.path.insert(0, str(CLIENT_WIRES_SRC))

from fixer_client_wires.bootstrap import (
    LEGACY_CONFIG_FILENAME,
    LEGACY_RUNTIME_ROOT_ENV,
    PUBLIC_CONFIG_FILENAME,
    PUBLIC_CONFIG_PATH_ENV,
    PUBLIC_CONFIG_ROOT_ENV,
    PUBLIC_RUNTIME_ROOT_ENV,
    PUBLIC_RUNTIME_PACKAGE,
    bootstrap_runtime_import_path,
    resolve_config_path,
    resolve_example_config_path,
    resolve_runtime_root,
    wire_info_lines,
)


class ClientWiresBootstrapTest(unittest.TestCase):
    def _write_json(self, path: Path) -> None:
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text('{"mcpServers":{}}\n', encoding="utf-8")

    def test_package_local_runtime_is_default(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            temp_path = Path(temp_dir)
            repo_root = temp_path / "github_repo"
            package_root = repo_root / "packages" / "client-wires"
            runtime_root = package_root / "runtime" / PUBLIC_RUNTIME_PACKAGE
            runtime_root.mkdir(parents=True)
            (runtime_root / "__init__.py").write_text("", encoding="utf-8")

            resolution = resolve_runtime_root(
                repo_root=repo_root,
                package_root=package_root,
                environ={},
            )

        self.assertEqual(resolution.source, "package-local runtime")
        self.assertEqual(resolution.package_name, PUBLIC_RUNTIME_PACKAGE)

    def test_public_override_beats_packaged_runtime(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            temp_path = Path(temp_dir)
            repo_root = temp_path / "github_repo"
            package_root = repo_root / "packages" / "client-wires"
            packaged_runtime = package_root / "runtime" / PUBLIC_RUNTIME_PACKAGE
            packaged_runtime.mkdir(parents=True)
            (packaged_runtime / "__init__.py").write_text("", encoding="utf-8")

            explicit_root = temp_path / "explicit-runtime" / PUBLIC_RUNTIME_PACKAGE
            explicit_root.mkdir(parents=True)
            (explicit_root / "__init__.py").write_text("", encoding="utf-8")

            resolution = resolve_runtime_root(
                repo_root=repo_root,
                package_root=package_root,
                environ={PUBLIC_RUNTIME_ROOT_ENV: str(explicit_root.parent)},
            )

        self.assertEqual(resolution.source, f"env:{PUBLIC_RUNTIME_ROOT_ENV}")
        self.assertEqual(resolution.root, explicit_root.parent.resolve())

    def test_legacy_env_is_used_when_packaged_runtime_is_missing(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            temp_path = Path(temp_dir)
            repo_root = temp_path / "github_repo"
            package_root = repo_root / "packages" / "client-wires"
            legacy_root = temp_path / "legacy-mcp-servers" / "codex_pro_app"
            legacy_root.mkdir(parents=True)
            (legacy_root / "__init__.py").write_text("", encoding="utf-8")

            resolution = resolve_runtime_root(
                repo_root=repo_root,
                package_root=package_root,
                environ={LEGACY_RUNTIME_ROOT_ENV: str(legacy_root.parent)},
            )

        self.assertEqual(resolution.source, f"compat env:{LEGACY_RUNTIME_ROOT_ENV}")
        self.assertEqual(resolution.package_name, "codex_pro_app")

    def test_package_local_config_is_default(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            temp_path = Path(temp_dir)
            repo_root = temp_path / "github_repo"
            package_root = repo_root / "packages" / "client-wires"
            self._write_json(package_root / "config" / PUBLIC_CONFIG_FILENAME)

            resolution = resolve_config_path(
                repo_root=repo_root,
                package_root=package_root,
                environ={},
            )

        self.assertEqual(resolution.source, "package-local config")
        self.assertEqual(resolution.kind, "active config")

    def test_explicit_config_path_beats_package_local_config(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            temp_path = Path(temp_dir)
            repo_root = temp_path / "github_repo"
            package_root = repo_root / "packages" / "client-wires"
            self._write_json(package_root / "config" / PUBLIC_CONFIG_FILENAME)

            explicit_config = temp_path / "custom" / PUBLIC_CONFIG_FILENAME
            self._write_json(explicit_config)

            resolution = resolve_config_path(
                repo_root=repo_root,
                package_root=package_root,
                environ={PUBLIC_CONFIG_PATH_ENV: str(explicit_config)},
            )

        self.assertEqual(resolution.source, f"env:{PUBLIC_CONFIG_PATH_ENV}")
        self.assertEqual(resolution.path, explicit_config.resolve())

    def test_explicit_config_root_resolves_standard_filename(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            temp_path = Path(temp_dir)
            repo_root = temp_path / "github_repo"
            package_root = repo_root / "packages" / "client-wires"
            config_root = temp_path / "custom-root"
            self._write_json(config_root / PUBLIC_CONFIG_FILENAME)

            resolution = resolve_config_path(
                repo_root=repo_root,
                package_root=package_root,
                environ={PUBLIC_CONFIG_ROOT_ENV: str(config_root)},
            )

        self.assertEqual(resolution.source, f"env:{PUBLIC_CONFIG_ROOT_ENV}")
        self.assertEqual(resolution.path, (config_root / PUBLIC_CONFIG_FILENAME).resolve())

    def test_package_local_example_is_used_when_active_config_is_missing(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            temp_path = Path(temp_dir)
            repo_root = temp_path / "github_repo"
            package_root = repo_root / "packages" / "client-wires"
            self._write_json(package_root / "examples" / "mcp-config.example.json")

            resolution = resolve_config_path(
                repo_root=repo_root,
                package_root=package_root,
                environ={},
            )

        self.assertEqual(resolution.source, "package-local example")
        self.assertEqual(resolution.kind, "example config")

    def test_repo_example_is_used_when_package_local_files_are_missing(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            temp_path = Path(temp_dir)
            repo_root = temp_path / "github_repo"
            package_root = repo_root / "packages" / "client-wires"
            repo_example = repo_root / "examples" / "mcp-config.example.json"
            self._write_json(repo_example)

            resolution = resolve_config_path(
                repo_root=repo_root,
                package_root=package_root,
                environ={},
            )

        self.assertEqual(resolution.source, "repo example fallback")
        self.assertEqual(resolution.path, repo_example.resolve())

    def test_legacy_repo_root_config_is_compatibility_fallback(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            temp_path = Path(temp_dir)
            repo_root = temp_path / "github_repo"
            package_root = repo_root / "packages" / "client-wires"
            legacy_config = repo_root / LEGACY_CONFIG_FILENAME
            self._write_json(legacy_config)

            resolution = resolve_config_path(
                repo_root=repo_root,
                package_root=package_root,
                environ={},
            )

        self.assertEqual(resolution.source, f"compat repo-root:{LEGACY_CONFIG_FILENAME}")
        self.assertEqual(resolution.kind, "legacy config")

    def test_example_config_path_prefers_package_local_example(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            temp_path = Path(temp_dir)
            repo_root = temp_path / "github_repo"
            package_root = repo_root / "packages" / "client-wires"
            package_example = package_root / "examples" / "mcp-config.example.json"
            self._write_json(package_example)

            self.assertEqual(
                resolve_example_config_path(repo_root, package_root),
                package_example.resolve(),
            )

    def test_wire_info_reports_runtime_source(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            temp_path = Path(temp_dir)
            repo_root = temp_path / "github_repo"
            package_root = repo_root / "packages" / "client-wires"
            runtime_root = package_root / "runtime" / PUBLIC_RUNTIME_PACKAGE
            runtime_root.mkdir(parents=True)
            (runtime_root / "__init__.py").write_text("", encoding="utf-8")
            self._write_json(package_root / "config" / PUBLIC_CONFIG_FILENAME)

            resolution = bootstrap_runtime_import_path(
                repo_root=repo_root,
                package_root=package_root,
                environ={},
            )
            info = "\n".join(
                wire_info_lines(
                    resolution,
                    repo_root=repo_root,
                    package_root=package_root,
                    environ={},
                )
            )

        self.assertIn("runtime source: package-local runtime", info)
        self.assertIn("config source: package-local config", info)
        self.assertIn("example config:", info)


if __name__ == "__main__":
    unittest.main()
