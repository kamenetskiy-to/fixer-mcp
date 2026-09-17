"""macOS release assembler for Fixer MCP and client wires."""

import argparse
import json
import os
import platform
import shutil
import subprocess
import sys
import tempfile
from typing import Any, Dict, Optional

from packaging.allowlist import (
    DangerousFileError,
    assert_not_dangerous,
    collect_allowed_files,
    is_allowed_bin_file,
    is_allowed_client_wire_file,
    is_allowed_fixer_mcp_file,
    is_allowed_installer_file,
    is_allowed_packaging_file,
    is_allowed_skill_file,
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


def detect_macos_platform() -> str:
    """Detect and normalize the macOS target platform (darwin_arm64 or darwin_amd64)."""
    machine = platform.machine().lower()
    if machine in ("arm64", "aarch64"):
        return "darwin_arm64"
    elif machine in ("x86_64", "amd64"):
        return "darwin_amd64"
    return f"darwin_{machine}"


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
    ):
        self.repo_root = os.path.abspath(repo_root or os.getcwd())
        self.client_wires_src = resolve_client_wires_path(self.repo_root, client_wires_src)
        self.go_module_dir = resolve_go_module_dir(self.repo_root, go_module_dir)
        self.out_dir = os.path.abspath(out_dir)
        self.version = version
        self.platform_id = platform_id or detect_macos_platform()
        self.changelog = changelog or f"Fixer MCP release {self.version} for {self.platform_id}"

    def build_go_binary(self, target_binary_path: str) -> None:
        """Compile the Fixer MCP Go server binary without host path leakage."""
        os.makedirs(os.path.dirname(target_binary_path), exist_ok=True)
        cmd = [
            "go",
            "build",
            "-trimpath",
            "-ldflags=-s -w",
            "-o",
            target_binary_path,
            ".",
        ]
        res = subprocess.run(
            cmd,
            cwd=self.go_module_dir,
            capture_output=True,
            text=True,
        )
        if res.returncode != 0:
            raise AssemblerError(f"Go build failed (code {res.returncode}):\n{res.stderr}")

        if not os.path.isfile(target_binary_path):
            raise AssemblerError(f"Go binary was not produced at {target_binary_path}")

        # Set executable permissions
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
            fixer_mcp_stage = os.path.join(staging_temp, "fixer_mcp")
            client_wires_stage = os.path.join(staging_temp, "client_wires")
            skills_stage = os.path.join(staging_temp, ".agents", "skills")

            os.makedirs(fixer_mcp_stage, exist_ok=True)
            os.makedirs(client_wires_stage, exist_ok=True)
            os.makedirs(skills_stage, exist_ok=True)

            # 1. Build and stage fixer_mcp binary
            binary_dest = os.path.join(fixer_mcp_stage, "fixer_mcp")
            self.build_go_binary(binary_dest)
            assert_not_dangerous("fixer_mcp/fixer_mcp")

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

            # 8. Create deterministic archive
            archive_filename = f"fixer-mcp-{self.version}-{self.platform_id}.tar.gz"
            archive_path = os.path.join(self.out_dir, archive_filename)
            sha256_hex = create_deterministic_tar_gz(
                source_root=staging_temp,
                output_tar_gz_path=archive_path,
                root_prefix="payload",
            )

            # 5. Create and validate format=1 descriptor
            source_revision = get_git_revision(self.repo_root)
            descriptor = create_release_descriptor(
                version=self.version,
                source_revision=source_revision,
                platform=self.platform_id,
                payload_filename=archive_filename,
                sha256_hex=sha256_hex,
                schema_compatibility_class="project-workroom-v1",
                changelog=self.changelog,
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
