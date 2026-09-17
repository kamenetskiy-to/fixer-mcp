"""Unified stage/apply engine for Fixer MCP fresh install and managed update."""

import datetime
import os
import shutil
import sys
import time
import uuid
from typing import Any, Dict, NamedTuple, Optional

# Ensure packaging package is discoverable
_cur_dir = os.path.dirname(os.path.abspath(__file__))
_repo_root = os.path.abspath(os.path.join(_cur_dir, ".."))
if _repo_root not in sys.path:
    sys.path.insert(0, _repo_root)

from packaging.archive import safe_extract
from installer.bootstrap_checker import check_candidate_bootstrap
from installer.errors import (
    ActiveRuntimeError,
    CandidateBootstrapError,
    DevelopmentCheckoutError,
    InstallerError,
    VerificationError,
)
from installer.fetcher import fetch_descriptor, fetch_payload
from installer.lock import InstallLock, check_active_runtime, is_runtime_active
from installer.metadata import InstallMetadata, is_development_checkout
from installer.paths import (
    current_link_path,
    metadata_path,
    release_version_dir,
    releases_dir,
    resolve_cache_dir,
    resolve_config_dir,
    resolve_db_path,
    resolve_managed_root,
    resolve_state_dir,
    resolve_user_bin_dir,
    staging_dir,
)


class StageResult(NamedTuple):
    version: str
    descriptor: Dict[str, Any]
    descriptor_source: str
    stage_dir: str
    extracted_dir: str
    archive_path: str


class ApplyResult(NamedTuple):
    status: str  # "applied" or "deferred"
    version: str
    release_dir: Optional[str] = None
    message: Optional[str] = None


class InstallEngine:
    """
    Manages local stage, verification, candidate check, and atomic switch
    for Fixer MCP installations and updates.
    """

    def __init__(
        self,
        managed_root: Optional[str] = None,
        state_dir: Optional[str] = None,
        config_dir: Optional[str] = None,
        cache_dir: Optional[str] = None,
        user_bin_dir: Optional[str] = None,
        db_path: Optional[str] = None,
    ):
        self.managed_root = resolve_managed_root(managed_root)
        self.state_dir = resolve_state_dir(state_dir)
        self.config_dir = resolve_config_dir(config_dir)
        self.cache_dir = resolve_cache_dir(cache_dir)
        self.user_bin_dir = resolve_user_bin_dir(user_bin_dir)
        self.db_path = resolve_db_path(self.state_dir, db_path)

    def current_version(self) -> Optional[str]:
        """Get currently installed version from install.json, or None."""
        meta = InstallMetadata.load(self.managed_root)
        if meta:
            return meta.version
        return None

    def stage(
        self,
        descriptor_source: str,
        payload_source: Optional[str] = None,
        timeout: float = 10.0,
    ) -> StageResult:
        """
        Stage a release candidate:
        1. Acquire install lock
        2. Verify not development checkout
        3. Fetch and validate descriptor
        4. Download and verify payload archive (SHA-256)
        5. Safely extract into staging
        6. Verify archive layout
        7. Candidate bootstrap schema check with temporary DB
        """
        # Guard against auto-updating development checkouts
        if is_development_checkout(self.managed_root):
            raise DevelopmentCheckoutError(
                f"Directory {self.managed_root} is a development checkout. "
                "Auto-update is disabled for development checkouts."
            )

        with InstallLock(self.managed_root):
            descriptor = fetch_descriptor(descriptor_source, timeout=min(timeout, 5.0))
            version = descriptor["version"]
            expected_sha256 = descriptor["sha256"]

            if not payload_source:
                payload_source = descriptor.get("payload_url") or descriptor.get("payload_path")

            if not payload_source:
                raise VerificationError("No payload source or payload_url specified in descriptor")

            stage_base = staging_dir(self.managed_root)
            stage_id = f"{version}_{os.getpid()}_{uuid.uuid4().hex[:8]}"
            stage_dir_path = os.path.join(stage_base, stage_id)
            os.makedirs(stage_dir_path, exist_ok=True)

            archive_name = "payload.tar.gz"
            archive_path = os.path.join(stage_dir_path, archive_name)
            extracted_dir_path = os.path.join(stage_dir_path, "extracted")

            try:
                # Fetch payload archive with checksum validation
                fetch_payload(
                    payload_source=payload_source,
                    dest_path=archive_path,
                    expected_sha256=expected_sha256,
                    base_descriptor_source=descriptor_source,
                    timeout=timeout,
                )

                # Safely extract archive
                safe_extract(archive_path, extracted_dir_path)

                # Layout verification: payload/fixer_mcp/fixer_mcp and payload/client_wires/fixer_wire.py
                req_binary = os.path.join(extracted_dir_path, "payload", "fixer_mcp", "fixer_mcp")
                req_wire = os.path.join(extracted_dir_path, "payload", "client_wires", "fixer_wire.py")
                if not os.path.isfile(req_binary):
                    # Check if extracted directly without payload/ prefix
                    direct_binary = os.path.join(extracted_dir_path, "fixer_mcp", "fixer_mcp")
                    if os.path.isfile(direct_binary):
                        req_binary = direct_binary
                    else:
                        raise VerificationError(f"Required binary not found in extracted payload: {req_binary}")

                # Candidate bootstrap check on a temporary database
                # Staged extraction root is either extracted_dir_path/payload or extracted_dir_path
                bootstrap_root = (
                    os.path.join(extracted_dir_path, "payload")
                    if os.path.isdir(os.path.join(extracted_dir_path, "payload"))
                    else extracted_dir_path
                )
                check_candidate_bootstrap(bootstrap_root)

                return StageResult(
                    version=version,
                    descriptor=descriptor,
                    descriptor_source=descriptor_source,
                    stage_dir=stage_dir_path,
                    extracted_dir=extracted_dir_path,
                    archive_path=archive_path,
                )

            except Exception:
                # Clean up staging directory on any failure
                if os.path.exists(stage_dir_path):
                    shutil.rmtree(stage_dir_path, ignore_errors=True)
                raise

    def apply(
        self,
        stage_result: StageResult,
        force_defer_override: bool = False,
    ) -> ApplyResult:
        """
        Apply a staged candidate release:
        1. Acquire install lock
        2. Check for active Fixer runtimes against shared state
        3. Move staged release to releases/<version> (preserving previous releases)
        4. Atomically switch current symlink
        5. Update install.json metadata
        6. Clean up staging directory
        """
        with InstallLock(self.managed_root):
            target_release_dir = release_version_dir(self.managed_root, stage_result.version)
            current_symlink = current_link_path(self.managed_root)
            is_current_active = False
            if os.path.islink(current_symlink):
                try:
                    is_current_active = (
                        os.path.exists(target_release_dir)
                        and os.path.realpath(current_symlink) == os.path.realpath(target_release_dir)
                    )
                except OSError:
                    pass

            payload_to_move = (
                os.path.join(stage_result.extracted_dir, "payload")
                if os.path.isdir(os.path.join(stage_result.extracted_dir, "payload"))
                else stage_result.extracted_dir
            )

            # Runtime usage guard check
            active, pids = is_runtime_active(self.state_dir)
            if active and not force_defer_override:
                # Active runtime detected: never delete the currently active release
                pids_str = ", ".join(str(p) for p in pids) if pids else "active locks"
                if is_current_active:
                    # Staging succeeded, but active release cannot be modified while running
                    shutil.rmtree(stage_result.stage_dir, ignore_errors=True)
                    msg = (
                        f"Active Fixer runtime detected ({pids_str}). "
                        f"Release {stage_result.version} is currently active and running. "
                        "Activation deferred until running sessions exit."
                    )
                    return ApplyResult(
                        status="deferred",
                        version=stage_result.version,
                        release_dir=target_release_dir,
                        message=msg,
                    )

                # For a new or non-active version, safely stage into target_release_dir without switching current
                target_tmp = f"{target_release_dir}.tmp.{os.getpid()}_{uuid.uuid4().hex[:8]}"
                if os.path.exists(target_tmp):
                    shutil.rmtree(target_tmp, ignore_errors=True)
                os.makedirs(os.path.dirname(target_tmp), exist_ok=True)
                shutil.move(payload_to_move, target_tmp)

                if os.path.exists(target_release_dir):
                    shutil.rmtree(target_release_dir, ignore_errors=True)
                os.replace(target_tmp, target_release_dir)

                shutil.rmtree(stage_result.stage_dir, ignore_errors=True)
                msg = (
                    f"Active Fixer runtime detected ({pids_str}). "
                    f"Release {stage_result.version} staged successfully at {target_release_dir}. "
                    "Activation deferred until running sessions exit."
                )
                return ApplyResult(
                    status="deferred",
                    version=stage_result.version,
                    release_dir=target_release_dir,
                    message=msg,
                )

            # Move staged payload into a temporary directory first so the active release is never destroyed
            target_tmp = f"{target_release_dir}.tmp.{os.getpid()}_{uuid.uuid4().hex[:8]}"
            if os.path.exists(target_tmp):
                shutil.rmtree(target_tmp, ignore_errors=True)
            os.makedirs(os.path.dirname(target_tmp), exist_ok=True)
            shutil.move(payload_to_move, target_tmp)

            tmp_symlink = os.path.join(self.managed_root, f"current_tmp_{os.getpid()}_{int(time.time()*1000)}")
            rel_target = os.path.join("releases", stage_result.version)

            target_backup = None
            if os.path.exists(target_release_dir):
                target_backup = f"{target_release_dir}.bak.{os.getpid()}_{uuid.uuid4().hex[:8]}"
                os.replace(target_release_dir, target_backup)

            try:
                os.replace(target_tmp, target_release_dir)

                # Atomic switch of current pointer
                if os.path.lexists(tmp_symlink):
                    try:
                        os.unlink(tmp_symlink)
                    except OSError:
                        pass
                os.symlink(rel_target, tmp_symlink)
                os.replace(tmp_symlink, current_symlink)

                # Clean up backup once current switch is atomic and successful
                if target_backup and os.path.exists(target_backup):
                    shutil.rmtree(target_backup, ignore_errors=True)
            except Exception:
                # Restore active release from backup on any failure
                if target_backup and os.path.exists(target_backup) and not os.path.exists(target_release_dir):
                    try:
                        os.replace(target_backup, target_release_dir)
                    except OSError:
                        pass
                if os.path.exists(target_tmp):
                    shutil.rmtree(target_tmp, ignore_errors=True)
                raise

            # Update or create install.json
            now_iso = datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
            existing_meta = InstallMetadata.load(self.managed_root)
            installed_at = existing_meta.installed_at if existing_meta else now_iso

            meta = InstallMetadata(
                mode="managed",
                version=stage_result.version,
                source_revision=stage_result.descriptor.get("source_revision", ""),
                platform=stage_result.descriptor.get("platform", ""),
                descriptor_source=stage_result.descriptor_source,
                state_dir=self.state_dir,
                installed_at=installed_at,
                updated_at=now_iso,
                extra={"schema_compatibility_class": stage_result.descriptor.get("schema_compatibility_class")},
            )
            meta.save(self.managed_root)

            # Clean up staging dir
            if os.path.exists(stage_result.stage_dir):
                shutil.rmtree(stage_result.stage_dir, ignore_errors=True)

            return ApplyResult(
                status="applied",
                version=stage_result.version,
                release_dir=target_release_dir,
                message=f"Successfully installed Fixer MCP {stage_result.version} into {target_release_dir}",
            )

    def install_or_update(
        self,
        descriptor_source: str,
        payload_source: Optional[str] = None,
        force_defer_override: bool = False,
        timeout: float = 10.0,
    ) -> ApplyResult:
        """Perform end-to-end stage and apply."""
        stage_res = self.stage(descriptor_source, payload_source=payload_source, timeout=timeout)
        return self.apply(stage_res, force_defer_override=force_defer_override)

    def rollback_to_previous(self) -> Optional[str]:
        """
        Switch current symlink back to the previous available release version.
        Returns the version switched to, or None if no alternate release exists.
        """
        with InstallLock(self.managed_root):
            releases_base = releases_dir(self.managed_root)
            if not os.path.isdir(releases_base):
                return None

            current_ver = self.current_version()
            available = sorted(
                [d for d in os.listdir(releases_base) if os.path.isdir(os.path.join(releases_base, d)) and d != current_ver],
                reverse=True,
            )
            if not available:
                return None

            prev_version = available[0]
            current_symlink = current_link_path(self.managed_root)
            tmp_symlink = os.path.join(self.managed_root, f"current_tmp_{os.getpid()}_{int(time.time()*1000)}")
            rel_target = os.path.join("releases", prev_version)

            os.symlink(rel_target, tmp_symlink)
            os.replace(tmp_symlink, current_symlink)

            # Update install.json
            meta = InstallMetadata.load(self.managed_root)
            if meta:
                meta.version = prev_version
                meta.updated_at = datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
                meta.save(self.managed_root)

            return prev_version
