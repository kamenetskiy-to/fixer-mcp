from __future__ import annotations

import json
import os
import random
import re
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Dict, Optional

from .depths import DepthProfile


RANDOM_SUFFIX_CHARS = "bcdfghjklmnpqrstvwxyz0123456789"


@dataclass(frozen=True)
class SessionPaths:
    root: Path
    logs_dir: Path
    notes_dir: Path
    artifacts_dir: Path
    search_dir: Path
    raw_dir: Path
    reports_dir: Path
    notifications_dir: Path
    metadata_path: Path
    research_log_path: Path
    inventory_path: Path


@dataclass(frozen=True)
class SessionContext:
    session_id: str
    slug: str
    created_at: datetime
    profile: DepthProfile
    paths: SessionPaths

    def to_metadata(self) -> Dict[str, object]:
        return {
            "session_id": self.session_id,
            "slug": self.slug,
            "depth": self.profile.to_metadata(),
            "created_at": self.created_at.isoformat(),
            "paths": {
                "root": str(self.paths.root),
                "logs": str(self.paths.logs_dir),
                "notes": str(self.paths.notes_dir),
                "artifacts": str(self.paths.artifacts_dir),
                "search": str(self.paths.search_dir),
                "raw": str(self.paths.raw_dir),
                "reports": str(self.paths.reports_dir),
                "notifications": str(self.paths.notifications_dir),
                "research_log": str(self.paths.research_log_path),
                "inventory": str(self.paths.inventory_path),
            },
        }

    def environment_overrides(self) -> Dict[str, str]:
        return {
            "CODEX_DR_SESSION_ID": self.session_id,
            "CODEX_DR_SESSION_SLUG": self.slug,
            "CODEX_DR_SESSION_DIR": str(self.paths.root),
            "CODEX_DR_DEPTH": self.profile.key,
            "CODEX_DR_ARTIFACTS_DIR": str(self.paths.artifacts_dir),
            "CODEX_DR_REPORTS_DIR": str(self.paths.reports_dir),
            "CODEX_DR_NOTES_DIR": str(self.paths.notes_dir),
            "CODEX_DR_LOGS_DIR": str(self.paths.logs_dir),
            "CODEX_DR_RESEARCH_LOG": str(self.paths.research_log_path),
            "CODEX_DR_INVENTORY": str(self.paths.inventory_path),
        }


def _slugify(text: str) -> str:
    lowered = text.lower()
    cleaned = re.sub(r"[^a-z0-9]+", "-", lowered)
    cleaned = cleaned.strip("-")
    return cleaned or "session"


def _random_suffix(length: int = 4) -> str:
    return "".join(random.choice(RANDOM_SUFFIX_CHARS) for _ in range(length))


def _build_slug(profile: DepthProfile, explicit_slug: Optional[str] = None) -> str:
    if explicit_slug:
        base = _slugify(explicit_slug)
    else:
        base = _slugify(profile.key)
    suffix = _random_suffix()
    return f"{base}-{suffix}"


def _session_root(base_dir: Path, timestamp: datetime, slug: str) -> Path:
    ts = timestamp.astimezone(timezone.utc).strftime("%Y%m%d-%H%M%S")
    return base_dir / f"{ts}-{slug}"


def _ensure_dir(path: Path) -> None:
    path.mkdir(parents=True, exist_ok=True)


def _write_metadata(context: SessionContext) -> None:
    metadata = context.to_metadata()
    context.paths.metadata_path.write_text(json.dumps(metadata, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")


def _write_research_log(context: SessionContext) -> None:
    header = [
        "# Deep Research Session Log",
        "",
        f"- Session ID: {context.session_id}",
        f"- Depth: {context.profile.key}",
        f"- Started: {context.created_at.isoformat()}",
        "",
        "## Timeline",
        "",
    ]
    context.paths.research_log_path.write_text("\n".join(header), encoding="utf-8")


def _write_inventory_stub(context: SessionContext) -> None:
    inventory = {
        "artifacts": [],
        "created_at": context.created_at.isoformat(),
        "session_id": context.session_id,
    }
    context.paths.inventory_path.write_text(json.dumps(inventory, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")


def create_session(
    profile: DepthProfile,
    *,
    base_dir: Path,
    explicit_slug: Optional[str] = None,
    timestamp: Optional[datetime] = None,
) -> SessionContext:
    ts = timestamp or datetime.now(timezone.utc)
    slug = _build_slug(profile, explicit_slug)
    root = _session_root(base_dir, ts, slug)

    logs_dir = root / "logs"
    notes_dir = root / "notes"
    artifacts_dir = root / "artifacts"
    search_dir = artifacts_dir / "search"
    raw_dir = artifacts_dir / "raw"
    reports_dir = root / "reports"
    notifications_dir = root / "notifications"
    metadata_path = root / "session.json"
    research_log_path = root / "research_log.md"
    inventory_path = root / "inventory.json"

    for path in (root, logs_dir, notes_dir, artifacts_dir, search_dir, raw_dir, reports_dir, notifications_dir):
        _ensure_dir(path)

    session_id = f"{ts.astimezone(timezone.utc).strftime('%Y%m%dT%H%M%SZ')}-{slug}"
    paths = SessionPaths(
        root=root,
        logs_dir=logs_dir,
        notes_dir=notes_dir,
        artifacts_dir=artifacts_dir,
        search_dir=search_dir,
        raw_dir=raw_dir,
        reports_dir=reports_dir,
        notifications_dir=notifications_dir,
        metadata_path=metadata_path,
        research_log_path=research_log_path,
        inventory_path=inventory_path,
    )
    context = SessionContext(
        session_id=session_id,
        slug=slug,
        created_at=ts,
        profile=profile,
        paths=paths,
    )

    _write_metadata(context)
    _write_research_log(context)
    _write_inventory_stub(context)

    return context


def append_inventory_entry(session: SessionContext, entry: Dict[str, object]) -> None:
    try:
        data = json.loads(session.paths.inventory_path.read_text(encoding="utf-8"))
    except (FileNotFoundError, json.JSONDecodeError):
        data = {"artifacts": [], "session_id": session.session_id, "created_at": session.created_at.isoformat()}
    artifacts = data.setdefault("artifacts", [])
    artifacts.append(entry)
    session.paths.inventory_path.write_text(json.dumps(data, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")


def append_research_log(context: SessionContext, entry: str) -> None:
    timestamp = datetime.now(timezone.utc).isoformat()
    line = f"- {timestamp}: {entry}\n"
    with context.paths.research_log_path.open("a", encoding="utf-8") as fh:
        fh.write(line)
