"""Installation metadata handling and development checkout detection."""

import datetime
import json
import os
import subprocess
from typing import Any, Dict, Optional

from installer.paths import metadata_path


class InstallMetadata:
    """Represents install.json configuration and status for a Fixer MCP installation."""

    def __init__(
        self,
        mode: str = "managed",
        version: str = "0.0.0",
        source_revision: str = "",
        platform: str = "",
        descriptor_source: str = "",
        state_dir: str = "",
        installed_at: Optional[str] = None,
        updated_at: Optional[str] = None,
        extra: Optional[Dict[str, Any]] = None,
    ):
        self.mode = mode
        self.version = version
        self.source_revision = source_revision
        self.platform = platform
        self.descriptor_source = descriptor_source
        self.state_dir = state_dir
        self.installed_at = installed_at or datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
        self.updated_at = updated_at
        self.extra = extra or {}

    def to_dict(self) -> Dict[str, Any]:
        data = {
            "mode": self.mode,
            "version": self.version,
            "source_revision": self.source_revision,
            "platform": self.platform,
            "descriptor_source": self.descriptor_source,
            "state_dir": self.state_dir,
            "installed_at": self.installed_at,
        }
        if self.updated_at:
            data["updated_at"] = self.updated_at
        if self.extra:
            data["extra"] = self.extra
        return data

    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> "InstallMetadata":
        return cls(
            mode=data.get("mode", "managed"),
            version=data.get("version", "0.0.0"),
            source_revision=data.get("source_revision", ""),
            platform=data.get("platform", ""),
            descriptor_source=data.get("descriptor_source", ""),
            state_dir=data.get("state_dir", ""),
            installed_at=data.get("installed_at"),
            updated_at=data.get("updated_at"),
            extra=data.get("extra", {}),
        )

    @classmethod
    def load(cls, managed_root: str) -> Optional["InstallMetadata"]:
        meta_file = metadata_path(managed_root)
        if not os.path.isfile(meta_file):
            return None
        try:
            with open(meta_file, "r", encoding="utf-8") as f:
                data = json.load(f)
            return cls.from_dict(data)
        except Exception:
            return None

    def save(self, managed_root: str) -> None:
        """Atomically write install.json to managed_root."""
        os.makedirs(managed_root, exist_ok=True)
        meta_file = metadata_path(managed_root)
        tmp_file = f"{meta_file}.tmp.{os.getpid()}"
        with open(tmp_file, "w", encoding="utf-8") as f:
            json.dump(self.to_dict(), f, indent=2, sort_keys=True)
            f.write("\n")
        os.replace(tmp_file, meta_file)


def is_development_checkout(path: str) -> bool:
    """
    Check if a given directory is a development git checkout.
    Detects standard .git directory and git worktree .git pointer file.
    """
    path = os.path.abspath(path)
    git_entry = os.path.join(path, ".git")
    if os.path.isdir(git_entry) or os.path.isfile(git_entry):
        return True

    # Check if there is an install.json marking it as development
    meta_file = os.path.join(path, "install.json")
    if os.path.isfile(meta_file):
        try:
            with open(meta_file, "r", encoding="utf-8") as f:
                data = json.load(f)
            if data.get("mode") == "development":
                return True
        except Exception:
            pass

    return False


def get_development_revision(repo_root: str) -> str:
    """Get the git revision for a development checkout."""
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
    return "development"
