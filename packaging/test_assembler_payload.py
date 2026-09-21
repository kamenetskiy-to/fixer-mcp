"""Unit and integration tests for fixerctl payload packaging and cross-compilation."""

import json
import os
import shutil
import subprocess
import tempfile
import unittest

from packaging.allowlist import (
    is_allowed_bin_file,
    is_allowed_control_plane_file,
    is_dangerous_name_or_path,
)
from packaging.archive import safe_extract
from packaging.assembler import (
    AssemblerError,
    ReleaseAssembler,
    resolve_go_target,
    verify_binary_architecture,
)
from scripts.prepare_public_repo import (
    MANAGED_INSTALL_COMPONENTS,
    MANAGED_INSTALL_ENTRYPOINTS,
)

REPO_ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), ".."))


class TestFixerctlPackaging(unittest.TestCase):
    def setUp(self):
        self.temp_dist = tempfile.mkdtemp(prefix="test_dist_fixerctl_")

    def tearDown(self):
        shutil.rmtree(self.temp_dist, ignore_errors=True)

    def test_allowlist_rules_for_fixerctl(self):
        """Verify fixerctl is permitted in bin/ and control_plane/ allowlists."""
        self.assertTrue(is_allowed_bin_file("fixer"))
        self.assertTrue(is_allowed_bin_file("fixerctl"))
        self.assertTrue(is_allowed_bin_file("fixer-console"))
        self.assertFalse(is_allowed_bin_file("other_binary"))
        self.assertFalse(is_allowed_bin_file("fixerctl.db"))

        self.assertTrue(is_allowed_control_plane_file("fixerctl"))
        self.assertTrue(is_allowed_control_plane_file("README.md"))
        self.assertFalse(is_allowed_control_plane_file("main.go"))
        self.assertFalse(is_allowed_control_plane_file("go.mod"))
        self.assertFalse(is_allowed_control_plane_file("fixerctl.log"))

    def test_installer_component_list_includes_release_surface(self):
        """Verify the managed install component list includes required trees."""
        self.assertIn("packaging", MANAGED_INSTALL_COMPONENTS)
        self.assertIn("bin", MANAGED_INSTALL_COMPONENTS)
        self.assertIn("scripts/release", MANAGED_INSTALL_COMPONENTS)
        self.assertIn("bin/fixer", MANAGED_INSTALL_ENTRYPOINTS)

    def test_honest_cross_compilation_target_resolution(self):
        """Verify GOOS/GOARCH resolution fails loudly on unmapped platforms."""
        self.assertEqual(resolve_go_target("darwin_arm64"), ("darwin", "arm64"))
        self.assertEqual(resolve_go_target("darwin_amd64"), ("darwin", "amd64"))
        self.assertEqual(resolve_go_target("linux_amd64"), ("linux", "amd64"))
        self.assertEqual(resolve_go_target("linux_arm64"), ("linux", "arm64"))

        with self.assertRaises(AssemblerError) as ctx:
            resolve_go_target("windows_amd64")
        self.assertIn("Cannot cross-compile", str(ctx.exception))

        with self.assertRaises(AssemblerError) as ctx:
            resolve_go_target("plan9_386")
        self.assertIn("Cannot cross-compile", str(ctx.exception))

    def test_assembled_payload_contains_fixerctl_executable(self):
        """Verify assembling a release includes fixerctl with mode 755 in both bin and control_plane."""
        version = "0.3.4-test"
        assembler = ReleaseAssembler(
            repo_root=REPO_ROOT,
            out_dir=self.temp_dist,
            version=version,
            platform_id="darwin_arm64",
        )
        result = assembler.assemble()
        self.assertEqual(result["status"], "success")

        archive_path = result["archive_path"]
        self.assertTrue(os.path.isfile(archive_path))

        with tempfile.TemporaryDirectory(prefix="test_extract_fixerctl_") as extract_dir:
            safe_extract(archive_path, extract_dir)

            # The canonical binary and its compatibility copy are identical.
            bin_console = os.path.join(extract_dir, "payload", "bin", "fixer-console")
            self.assertTrue(os.path.isfile(bin_console), f"Expected bin/fixer-console at {bin_console}")
            self.assertTrue(os.access(bin_console, os.X_OK), "bin/fixer-console must be executable")

            # Check bin/fixerctl
            bin_fixerctl = os.path.join(extract_dir, "payload", "bin", "fixerctl")
            self.assertTrue(os.path.isfile(bin_fixerctl), f"Expected bin/fixerctl at {bin_fixerctl}")
            self.assertTrue(os.access(bin_fixerctl, os.X_OK), "bin/fixerctl must be executable")
            mode = oct(os.stat(bin_fixerctl).st_mode & 0o777)
            self.assertEqual(mode, "0o755", f"bin/fixerctl mode should be 0o755, got {mode}")

            # Check control_plane/fixerctl
            cp_fixerctl = os.path.join(extract_dir, "payload", "control_plane", "fixerctl")
            self.assertTrue(os.path.isfile(cp_fixerctl), f"Expected control_plane/fixerctl at {cp_fixerctl}")
            self.assertTrue(os.access(cp_fixerctl, os.X_OK), "control_plane/fixerctl must be executable")

            # Check fixerctl -version execution
            proc_ver = subprocess.run(
                [bin_fixerctl, "-version"],
                capture_output=True,
                text=True,
            )
            self.assertEqual(proc_ver.returncode, 0, f"fixerctl -version failed: {proc_ver.stderr}")
            self.assertIn("fixerctl v0.3.4-test", proc_ver.stdout)
            proc_console_ver = subprocess.run([bin_console, "-version"], capture_output=True, text=True)
            self.assertEqual(proc_console_ver.returncode, 0, proc_console_ver.stderr)
            self.assertEqual(proc_console_ver.stdout, "fixer v0.3.4-test (darwin/arm64)\n")

            # Check fixerctl -print execution
            proc_print = subprocess.run(
                [bin_fixerctl, "-print"],
                capture_output=True,
                text=True,
            )
            self.assertEqual(proc_print.returncode, 0, f"fixerctl -print failed: {proc_print.stderr}")
            self.assertIn("fixerctl v", proc_print.stdout)
            self.assertIn("fixer", proc_print.stdout)

    def test_cross_compile_linux_amd64_payload(self):
        """Verify cross-compilation produces honest ELF binary for linux_amd64."""
        version = "0.3.4-linux-test"
        assembler = ReleaseAssembler(
            repo_root=REPO_ROOT,
            out_dir=self.temp_dist,
            version=version,
            platform_id="linux_amd64",
        )
        result = assembler.assemble()
        self.assertEqual(result["status"], "success")

        archive_path = result["archive_path"]
        with tempfile.TemporaryDirectory(prefix="test_extract_linux_") as extract_dir:
            safe_extract(archive_path, extract_dir)
            bin_fixerctl = os.path.join(extract_dir, "payload", "bin", "fixerctl")
            self.assertTrue(os.path.isfile(bin_fixerctl))

            # Validate ELF header
            with open(bin_fixerctl, "rb") as f:
                header = f.read(20)
            self.assertEqual(header[:4], b"\x7fELF", "Binary must have ELF header")
            self.assertEqual(header[4], 2, "Binary must be 64-bit ELF")
            machine = int.from_bytes(header[18:20], byteorder="little")
            self.assertEqual(machine, 62, "Binary must have EM_X86_64 architecture")


if __name__ == "__main__":
    unittest.main()
