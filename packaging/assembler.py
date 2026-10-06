"""macOS release assembler for Fixer MCP and client wires."""

import argparse
import json
import os
import platform
import shutil
import subprocess
import sys
import sys
import tempfile
from typing import Any, Dict, Optional, Tuple

from packaging.allowlist import (
    DangerousFileError,
    assert_not_dangerous,
    collect_allowed_files,
    is_allowed_bin_file,
    is_allowed_client_wire_file,
    is_allowed_control_plane_file,
    is_allowed_fixer_mcp_file,
    is_allowed_installer_file,
    is_allowed_install_script_file,
    is_allowed_packaging_file,
    is_allowed_script_file,
    is_allowed_skill_file,
)
from packaging.syntax import (
    PythonSyntaxFloorError,
    verify_python_syntax_floor,
)
from packaging.archive import (
    calculate_sha256,
    create_deterministic_tar_gz,
    verify_payload_archive,
)
from packaging.descriptor import (
    create_release_descriptor,
    serialize_descriptor,
    validate_release_descriptor,
)


class AssemblerError(Exception):
    """Raised when release assembly fails."""
    pass


def get_git_revision(repo_root: str) -> str:
    """Get the current git commit SHA of repo_root, or a deterministic fallback."""
    try:
        rev = subprocess.check_output(
            ["git", "-C", repo_root, "rev-parse", "HEAD"],
            stderr=subprocess.DEVNULL,
            text=True,
        ).strip()
        if rev:
            return rev
    except Exception:
        pass
    return "0000000000000000000000000000000000000000"


def detect_host_platform() -> str:
    """Detect and normalize the host target platform (darwin/linux, arm64/amd64)."""
    machine = platform.machine().lower()
    if machine in ("arm64", "aarch64"):
        arch = "arm64"
    elif machine in ("x86_64", "amd64"):
        arch = "amd64"
    else:
        arch = machine
    if sys.platform.startswith("linux"):
        return f"linux_{arch}"
    return f"darwin_{arch}"


def detect_macos_platform() -> str:
    """Backward-compatible alias; detection is cross-platform since 1.0.5."""
    return detect_host_platform()


def resolve_go_target(platform_id: str) -> Tuple[str, str]:
    """Resolve and validate GOOS and GOARCH for the given platform ID.

    Cross-compilation must be honest: fail loudly if the platform ID is unknown
    or unsupported instead of silently falling back to the host architecture.
    """
    normalized = platform_id.replace("-", "_").lower()
    platform_map = {
        "darwin_arm64": ("darwin", "arm64"),
        "darwin_amd64": ("darwin", "amd64"),
        "linux_amd64": ("linux", "amd64"),
        "linux_arm64": ("linux", "arm64"),
    }
    if normalized in platform_map:
        return platform_map[normalized]
    raise AssemblerError(f"Cannot cross-compile: unsupported or unmapped platform '{platform_id}'")


def verify_binary_architecture(binary_path: str, target_goos: str, target_goarch: str) -> None:
    """Verify that the compiled binary actually matches the target GOOS and GOARCH."""
    if not os.path.isfile(binary_path):
        raise AssemblerError(f"Binary was not produced at {binary_path}")

    with open(binary_path, "rb") as f:
        header = f.read(64)

    if len(header) < 20:
        raise AssemblerError(f"Binary at {binary_path} is too small to verify header")

    if target_goos == "darwin":
        # Mach-O 64-bit: magic is 0xfeedfacf (little endian: cf fa ed fe)
        magic = header[:4]
        if magic != b"\xcf\xfa\xed\xfe":
            raise AssemblerError(
                f"Binary at {binary_path} does not have Mach-O 64-bit magic header (got {magic.hex()})"
            )
        cputype = int.from_bytes(header[4:8], byteorder="little")
        # CPU_TYPE_ARM64 = 0x0100000C
        # CPU_TYPE_X86_64 = 0x01000007
        expected_cpu = 0x0100000C if target_goarch == "arm64" else (0x01000007 if target_goarch == "amd64" else None)
        if expected_cpu and cputype != expected_cpu:
            raise AssemblerError(
                f"Binary at {binary_path} has CPU type {hex(cputype)}, expected {hex(expected_cpu)} for {target_goarch}"
            )
    elif target_goos == "linux":
        # ELF: magic is \x7fELF
        if header[:4] != b"\x7fELF":
            raise AssemblerError(f"Binary at {binary_path} does not have ELF magic header")
        if header[4] != 2:
            raise AssemblerError(f"Binary at {binary_path} is not ELF 64-bit")
        machine = int.from_bytes(header[18:20], byteorder="little")
        # EM_X86_64 = 62 (0x3E), EM_AARCH64 = 183 (0xB7)
        expected_machine = 183 if target_goarch == "arm64" else (62 if target_goarch == "amd64" else None)
        if expected_machine and machine != expected_machine:
            raise AssemblerError(
                f"Binary at {binary_path} has ELF machine {hex(machine)}, expected {hex(expected_machine)} for {target_goarch}"
            )


def resolve_client_wires_path(repo_root: str, explicit_path: Optional[str] = None) -> str:
    """
    Resolve client_wires directory path portably.
    Requires explicit path or portable sibling directory.
    Eliminates all hard-coded personal path fallbacks.
    """
    if explicit_path:
        explicit_abs = os.path.abspath(explicit_path)
        if os.path.isdir(explicit_abs):
            return explicit_abs
        raise AssemblerError(f"Specified client_wires directory does not exist: {explicit_path}")

    # Check environment variable if set
    env_path = os.environ.get("CLIENT_WIRES_DIR") or os.environ.get("CLIENT_WIRES_PATH")
    if env_path and os.path.isdir(env_path):
        return os.path.abspath(env_path)

    # 1. Inside the repository root. This is the module-source layout used by the
    #    live tree, where the Go server sits in fixer_mcp/ and the wires are a
    #    top-level sibling directory.
    in_repo = os.path.abspath(os.path.join(repo_root, "client_wires"))
    if os.path.isdir(in_repo):
        return in_repo

    # 2. Direct sibling of repo_root (flat release layout)
    direct_sibling = os.path.abspath(os.path.join(repo_root, "..", "client_wires"))
    if os.path.isdir(direct_sibling):
        return direct_sibling

    # 3. If repo_root is inside a nested worktree (e.g. .codex/netrunner_worktrees/wave-X/session-Y),
    # check git common directory's parent for sibling client_wires
    try:
        git_common = subprocess.check_output(
            ["git", "-C", repo_root, "rev-parse", "--git-common-dir"],
            stderr=subprocess.DEVNULL,
            text=True,
        ).strip()
        if git_common:
            common_root = os.path.dirname(os.path.abspath(git_common))
            common_sibling = os.path.abspath(os.path.join(common_root, "..", "client_wires"))
            if os.path.isdir(common_sibling):
                return common_sibling
    except Exception:
        pass

    # 4. Walk upward from repo_root looking for client_wires sibling
    current = os.path.abspath(repo_root)
    while True:
        parent = os.path.dirname(current)
        candidate = os.path.join(parent, "client_wires")
        if os.path.isdir(candidate):
            return candidate
        if parent == current:
            break
        current = parent

    raise AssemblerError(
        "client_wires directory not found. Please provide explicit --client-wires argument "
        "or ensure client_wires is located as a sibling directory to the repository checkout."
    )


def resolve_go_module_dir(repo_root: str, explicit_path: Optional[str] = None) -> str:
    """
    Resolve the Go module directory that owns main.go.

    The live module-source layout keeps the Go server in `fixer_mcp/`; the flat
    release layout keeps it directly at the repository root. Both are supported
    so the assembler works from this tree and from an assembled release.
    """
    if explicit_path:
        explicit_abs = os.path.abspath(explicit_path)
        if os.path.isfile(os.path.join(explicit_abs, "main.go")):
            return explicit_abs
        raise AssemblerError(f"Specified Go module directory has no main.go: {explicit_path}")

    candidates = [
        os.path.abspath(repo_root),
        os.path.abspath(os.path.join(repo_root, "fixer_mcp")),
    ]
    for candidate in candidates:
        if os.path.isfile(os.path.join(candidate, "main.go")):
            return candidate

    raise AssemblerError(
        "Go module directory with main.go not found. Looked in: "
        + ", ".join(candidates)
        + ". Pass --go-module-dir to override."
    )


def resolve_control_plane_dir(repo_root: str, explicit_path: Optional[str] = None) -> str:
    """
    Resolve the control_plane directory containing cmd/fixerctl.
    """
    if explicit_path:
        explicit_abs = os.path.abspath(explicit_path)
        if os.path.isdir(explicit_abs) and os.path.isfile(os.path.join(explicit_abs, "cmd", "fixerctl", "main.go")):
            return explicit_abs
        raise AssemblerError(f"Specified control_plane directory does not have cmd/fixerctl/main.go: {explicit_path}")

    candidates = [
        os.path.abspath(os.path.join(repo_root, "control_plane")),
        os.path.abspath(repo_root),
    ]
    for candidate in candidates:
        if os.path.isfile(os.path.join(candidate, "cmd", "fixerctl", "main.go")):
            return candidate

    raise AssemblerError(
        "control_plane directory with cmd/fixerctl/main.go not found. Looked in: "
        + ", ".join(candidates)
        + ". Pass --control-plane-dir to override."
    )


class ReleaseAssembler:
    """Builds and packages a release payload for macOS."""

    def __init__(
        self,
        repo_root: Optional[str] = None,
        client_wires_src: Optional[str] = None,
        out_dir: str = "dist",
        version: str = "0.1.0",
        platform_id: Optional[str] = None,
        changelog: Optional[str] = None,
        go_module_dir: Optional[str] = None,
        control_plane_dir: Optional[str] = None,
    ):
        self.repo_root = os.path.abspath(repo_root or os.getcwd())
        self.client_wires_src = resolve_client_wires_path(self.repo_root, client_wires_src)
        self.go_module_dir = resolve_go_module_dir(self.repo_root, go_module_dir)
        self.control_plane_dir = resolve_control_plane_dir(self.repo_root, control_plane_dir)
        self.out_dir = os.path.abspath(out_dir)
        self.version = version
        self.platform_id = platform_id or detect_host_platform()
        self.changelog = changelog or f"Fixer MCP release {self.version} for {self.platform_id}"

    def build_go_binary(self, target_binary_path: str, source_revision: Optional[str] = None) -> None:
        """Compile the Fixer MCP Go server binary without host path leakage.

        The target platform decides GOOS/GOARCH, so a Linux release can be
        assembled on a macOS machine (and vice versa) without a second toolchain.
        """
        os.makedirs(os.path.dirname(target_binary_path), exist_ok=True)
        target_goos, target_goarch = resolve_go_target(self.platform_id)
        build_env = os.environ.copy()
        build_env["GOOS"] = target_goos
        build_env["GOARCH"] = target_goarch
        build_env["CGO_ENABLED"] = "0"
        rev = source_revision if source_revision is not None else get_git_revision(self.repo_root)
        ldflags = (
            f"-s -w "
            f"-X main.fixerMCPReleaseVersion={self.version} "
            f"-X main.fixerMCPSourceRevision={rev}"
        )
        cmd = [
            "go",
            "build",
            "-trimpath",
            f"-ldflags={ldflags}",
            "-o",
            target_binary_path,
            ".",
        ]
        res = subprocess.run(
            cmd,
            cwd=self.go_module_dir,
            capture_output=True,
            text=True,
            env=build_env,
        )
        if res.returncode != 0:
            raise AssemblerError(
                f"Go build for fixer_mcp failed ({target_goos}/{target_goarch}, code {res.returncode}):\n{res.stderr}"
            )

        if not os.path.isfile(target_binary_path):
            raise AssemblerError(f"Go binary was not produced at {target_binary_path}")

        verify_binary_architecture(target_binary_path, target_goos, target_goarch)
        # Set executable permissions
        os.chmod(target_binary_path, 0o755)

    def build_fixerctl_binary(self, target_binary_path: str) -> None:
        """Compile the fixerctl binary from control_plane/cmd/fixerctl."""
        os.makedirs(os.path.dirname(target_binary_path), exist_ok=True)
        target_goos, target_goarch = resolve_go_target(self.platform_id)
        build_env = os.environ.copy()
        build_env["GOOS"] = target_goos
        build_env["GOARCH"] = target_goarch
        build_env["CGO_ENABLED"] = "0"
        cmd = [
            "go",
            "build",
            "-trimpath",
            f"-ldflags=-s -w -X main.version={self.version}",
            "-o",
            target_binary_path,
            "./cmd/fixerctl",
        ]
        res = subprocess.run(
            cmd,
            cwd=self.control_plane_dir,
            capture_output=True,
            text=True,
            env=build_env,
        )
        if res.returncode != 0:
            raise AssemblerError(
                f"Go build for fixerctl failed ({target_goos}/{target_goarch}, code {res.returncode}):\n{res.stderr}"
            )

        if not os.path.isfile(target_binary_path):
            raise AssemblerError(f"Go binary was not produced at {target_binary_path}")

        verify_binary_architecture(target_binary_path, target_goos, target_goarch)
        os.chmod(target_binary_path, 0o755)

    def assemble(self) -> Dict[str, Any]:
        """Run the complete release assembly pipeline."""
        if not os.path.isfile(os.path.join(self.go_module_dir, "main.go")):
            raise AssemblerError(f"Go module directory does not contain main.go: {self.go_module_dir}")

        if not os.path.isdir(self.client_wires_src):
            raise AssemblerError(f"client_wires source directory not found: {self.client_wires_src}")

        os.makedirs(self.out_dir, exist_ok=True)

        staging_temp = tempfile.mkdtemp(prefix="fixer_release_stage_")
        try:
            # Layout under staging:
            # staging/fixer_mcp/
            # staging/client_wires/
            # staging/.agents/skills/
            # staging/bin/
            # staging/control_plane/
            fixer_mcp_stage = os.path.join(staging_temp, "fixer_mcp")
            client_wires_stage = os.path.join(staging_temp, "client_wires")
            skills_stage = os.path.join(staging_temp, ".agents", "skills")
            bin_stage = os.path.join(staging_temp, "bin")
            control_plane_stage = os.path.join(staging_temp, "control_plane")

            os.makedirs(fixer_mcp_stage, exist_ok=True)
            os.makedirs(client_wires_stage, exist_ok=True)
            os.makedirs(skills_stage, exist_ok=True)
            os.makedirs(bin_stage, exist_ok=True)
            os.makedirs(control_plane_stage, exist_ok=True)

            # Compute canonical source revision once so binary provenance and descriptor match exactly
            source_revision = get_git_revision(self.repo_root)

            # 1. Build and stage fixer_mcp binary
            binary_dest = os.path.join(fixer_mcp_stage, "fixer_mcp")
            self.build_go_binary(binary_dest, source_revision=source_revision)
            assert_not_dangerous("fixer_mcp/fixer_mcp")

            # 1b. Build and stage the unified console. Keep fixerctl as a
            # compatibility filename, but both names execute the same binary.
            console_bin_dest = os.path.join(bin_stage, "fixer-console")
            self.build_fixerctl_binary(console_bin_dest)
            assert_not_dangerous("bin/fixer-console")

            fixerctl_bin_dest = os.path.join(bin_stage, "fixerctl")
            shutil.copy2(console_bin_dest, fixerctl_bin_dest)
            os.chmod(fixerctl_bin_dest, 0o755)
            assert_not_dangerous("bin/fixerctl")

            fixerctl_cp_dest = os.path.join(control_plane_stage, "fixerctl")
            shutil.copy2(console_bin_dest, fixerctl_cp_dest)
            os.chmod(fixerctl_cp_dest, 0o755)
            assert_not_dangerous("control_plane/fixerctl")

            cp_readme = os.path.join(self.control_plane_dir, "README.md")
            if os.path.isfile(cp_readme):
                shutil.copy2(cp_readme, os.path.join(control_plane_stage, "README.md"))
                assert_not_dangerous("control_plane/README.md")

            # Stage clean portable MCP configuration (no personal paths)
            clean_mcp_cfg = {
                "mcpServers": {
                    "fixer_mcp": {
                        "command": "./fixer_mcp",
                        "args": [],
                        "startup_timeout_sec": 30,
                    }
                }
            }
            mcp_cfg_path = os.path.join(fixer_mcp_stage, "mcp_config.json")
            with open(mcp_cfg_path, "w", encoding="utf-8") as f:
                json.dump(clean_mcp_cfg, f, indent=2)
            assert_not_dangerous("fixer_mcp/mcp_config.json")

            readme = os.path.join(self.repo_root, "README.md")
            if os.path.isfile(readme):
                shutil.copy2(readme, os.path.join(fixer_mcp_stage, "README.md"))
                assert_not_dangerous("fixer_mcp/README.md")

            # 2. Collect and stage client_wires
            client_wire_files = collect_allowed_files(self.client_wires_src, is_allowed_client_wire_file)
            if not client_wire_files:
                raise AssemblerError(f"No allowed client_wire files collected from: {self.client_wires_src}")

            has_fixer_wire = False
            for src_abs, rel_path in client_wire_files:
                if rel_path == "fixer_wire.py":
                    has_fixer_wire = True
                assert_not_dangerous(f"client_wires/{rel_path}")
                dest_file = os.path.join(client_wires_stage, rel_path)
                os.makedirs(os.path.dirname(dest_file), exist_ok=True)
                shutil.copy2(src_abs, dest_file)

            if not has_fixer_wire:
                raise AssemblerError("fixer_wire.py missing from collected client_wires")

            # 3. Collect and stage .agents/skills
            skills_src = os.path.join(self.repo_root, ".agents", "skills")
            skill_files = collect_allowed_files(skills_src, is_allowed_skill_file)
            if not skill_files:
                raise AssemblerError(f"No allowed skill files collected from: {skills_src}")

            for src_abs, rel_path in skill_files:
                assert_not_dangerous(f".agents/skills/{rel_path}")
                dest_file = os.path.join(skills_stage, rel_path)
                os.makedirs(os.path.dirname(dest_file), exist_ok=True)
                shutil.copy2(src_abs, dest_file)

            # 4. Collect and stage installer
            installer_stage = os.path.join(staging_temp, "installer")
            installer_src = os.path.join(self.repo_root, "installer")
            if os.path.isdir(installer_src):
                installer_files = collect_allowed_files(installer_src, is_allowed_installer_file)
                for src_abs, rel_path in installer_files:
                    assert_not_dangerous(f"installer/{rel_path}")
                    dest_file = os.path.join(installer_stage, rel_path)
                    os.makedirs(os.path.dirname(dest_file), exist_ok=True)
                    shutil.copy2(src_abs, dest_file)

            # 5. Collect and stage packaging
            packaging_stage = os.path.join(staging_temp, "packaging")
            packaging_src = os.path.join(self.repo_root, "packaging")
            if os.path.isdir(packaging_src):
                packaging_files = collect_allowed_files(packaging_src, is_allowed_packaging_file)
                for src_abs, rel_path in packaging_files:
                    assert_not_dangerous(f"packaging/{rel_path}")
                    dest_file = os.path.join(packaging_stage, rel_path)
                    os.makedirs(os.path.dirname(dest_file), exist_ok=True)
                    shutil.copy2(src_abs, dest_file)

            # 6. Collect and stage bin
            bin_stage = os.path.join(staging_temp, "bin")
            bin_src = os.path.join(self.repo_root, "bin")
            if os.path.isdir(bin_src):
                bin_files = collect_allowed_files(bin_src, is_allowed_bin_file)
                for src_abs, rel_path in bin_files:
                    if rel_path == "fixerctl":
                        continue
                    assert_not_dangerous(f"bin/{rel_path}")
                    dest_file = os.path.join(bin_stage, rel_path)
                    os.makedirs(os.path.dirname(dest_file), exist_ok=True)
                    shutil.copy2(src_abs, dest_file)
                    os.chmod(dest_file, 0o755)

            # 7. Collect and stage bundled vendor dependencies (e.g. tomli for Python < 3.11 compatibility)
            vendor_tomli_src = os.path.join(self.repo_root, "packaging", "vendor", "tomli")
            if os.path.isdir(vendor_tomli_src):
                tomli_stage = os.path.join(staging_temp, "tomli")
                tomli_files = collect_allowed_files(vendor_tomli_src, is_allowed_packaging_file)
                for src_abs, rel_path in tomli_files:
                    assert_not_dangerous(f"tomli/{rel_path}")
                    dest_file = os.path.join(tomli_stage, rel_path)
                    os.makedirs(os.path.dirname(dest_file), exist_ok=True)
                    shutil.copy2(src_abs, dest_file)

            # 8. Collect and stage allowed scripts (e.g. scripts/pi_probe.py, scripts/install) for standalone payload
            scripts_stage = os.path.join(staging_temp, "scripts")
            scripts_src = os.path.join(self.repo_root, "scripts")
            if os.path.isdir(scripts_src):
                script_files = collect_allowed_files(scripts_src, is_allowed_script_file)
                for src_abs, rel_path in script_files:
                    assert_not_dangerous(f"scripts/{rel_path}")
                    dest_file = os.path.join(scripts_stage, rel_path)
                    os.makedirs(os.path.dirname(dest_file), exist_ok=True)
                    shutil.copy2(src_abs, dest_file)
                    os.chmod(dest_file, 0o755)

            # 9. Verify Python syntax compatibility floor (Python 3.9) across all staged Python files
            try:
                verify_python_syntax_floor(staging_temp, min_version=(3, 9))
            except PythonSyntaxFloorError as e:
                raise AssemblerError(f"Release assembly failed minimum Python syntax check: {e}") from e

            # 10. Create deterministic archive
            archive_filename = f"fixer-mcp-{self.version}-{self.platform_id}.tar.gz"
            archive_path = os.path.join(self.out_dir, archive_filename)
            sha256_hex = create_deterministic_tar_gz(
                source_root=staging_temp,
                output_tar_gz_path=archive_path,
                root_prefix="payload",
            )

            # 11. Create and validate format=1 descriptor
            min_os = "macOS 12.0" if self.platform_id.startswith("darwin_") else "Linux (glibc 2.31+)"
            descriptor = create_release_descriptor(
                version=self.version,
                source_revision=source_revision,
                platform=self.platform_id,
                payload_filename=archive_filename,
                sha256_hex=sha256_hex,
                schema_compatibility_class="project-workroom-v1",
                changelog=self.changelog,
                min_os=min_os,
                min_python="3.9",
            )

            # Write descriptor files (release.json and version-specific json)
            descriptor_path = os.path.join(self.out_dir, "release.json")
            with open(descriptor_path, "w", encoding="utf-8") as f:
                f.write(serialize_descriptor(descriptor))

            versioned_desc_path = os.path.join(self.out_dir, f"fixer-mcp-{self.version}-{self.platform_id}.json")
            with open(versioned_desc_path, "w", encoding="utf-8") as f:
                f.write(serialize_descriptor(descriptor))

            # 6. Verify assembled archive
            verify_payload_archive(archive_path, descriptor, verify_extraction=True)

            return {
                "status": "success",
                "version": self.version,
                "platform": self.platform_id,
                "source_revision": source_revision,
                "archive_path": archive_path,
                "archive_filename": archive_filename,
                "descriptor_path": descriptor_path,
                "sha256": sha256_hex,
                "descriptor": descriptor,
            }

        finally:
            shutil.rmtree(staging_temp, ignore_errors=True)
