"""Read live subscription limits from the local AI Limits snapshot DB.

The DB is owned by the sibling project /home/operator/Desktop/projects/ai-limits
(`db/ai_limits.db`, table `limit_snapshots`). This module only reads the latest
snapshot per provider; it never writes and never falls back to network calls.
"""

from __future__ import annotations

import sqlite3
from pathlib import Path
from typing import Any


_AI_LIMITS_DB = Path.home() / "Desktop" / "projects" / "ai-limits" / "db" / "ai_limits.db"

# provider name prefixes (as stored by check-my-limits) -> backend/subscription label
_BACKEND_SUBSCRIPTIONS: dict[str, tuple[str, ...]] = {
    "codex": ("Codex / Plus", "OpenCode Go", "CommandCode"),
    "antigravity": ("Agy/Claude+GPT", "Agy/Gemini"),
    "grok": ("SuperGrok",),
    "claude": ("Claude Code",),
    "kimi-code": ("Kimi Code",),
}


def _latest_snapshots() -> dict[str, dict[str, Any]]:
    if not _AI_LIMITS_DB.is_file():
        return {}
    query = """
        SELECT s.provider, s.status, s.limit_5h_pct, s.limit_7d_pct, s.limit_1m_pct,
               s.limit_1m_reset_raw, s.limit_7d_reset_raw
        FROM limit_snapshots s
        INNER JOIN (
            SELECT provider, MAX(timestamp) ts
            FROM limit_snapshots
            GROUP BY provider
        ) l ON s.provider = l.provider AND s.timestamp = l.ts
        WHERE s.status NOT IN ('error', 'no-auth')
    """
    rows: dict[str, dict[str, Any]] = {}
    try:
        with sqlite3.connect(f"file:{_AI_LIMITS_DB}?mode=ro", uri=True) as conn:
            for provider, status, limit_5h, limit_7d, limit_1m, reset_1m, reset_7d in conn.execute(query):
                rows[str(provider)] = {
                    "status": status,
                    "limit_5h_pct": limit_5h,
                    "limit_7d_pct": limit_7d,
                    "limit_1m_pct": limit_1m,
                    "limit_1m_reset_raw": reset_1m,
                    "limit_7d_reset_raw": reset_7d,
                }
    except sqlite3.Error:
        return {}
    return rows


def _left_percent(row: dict[str, Any]) -> str | None:
    for key in ("limit_1m_pct", "limit_7d_pct", "limit_5h_pct"):
        value = row.get(key)
        if value is None:
            continue
        try:
            pct = float(value)
        except (TypeError, ValueError):
            continue
        if pct != pct:  # NaN
            continue
        return f"{pct:.2f}".rstrip("0").rstrip(".")
    return None


def backend_subscription_limits(backend: str) -> str:
    """Per-subscription limits for one CLI backend, e.g. 'Plus 31% · OpenCode Go 37% · CommandCode 99.85%'."""
    snapshots = _latest_snapshots()
    parts: list[str] = []
    for prefix in _BACKEND_SUBSCRIPTIONS.get(backend, ()):
        row = next(
            (row for provider, row in snapshots.items() if provider.startswith(prefix)),
            None,
        )
        left = _left_percent(row) if row else None
        if left is not None:
            label = prefix.split("/", 1)[-1].strip() or prefix
            parts.append(f"{label} {left}%")
    return " · ".join(parts)


def backend_subscriptions(backend: str) -> str:
    """Comma-separated subscription names for one CLI backend."""
    names: list[str] = []
    snapshots = _latest_snapshots()
    for prefix in _BACKEND_SUBSCRIPTIONS.get(backend, ()):
        row = next(
            (row for provider, row in snapshots.items() if provider.startswith(prefix)),
            None,
        )
        if row is None:
            continue
        label = prefix.split("/", 1)[-1].strip() or prefix
        names.append(label)
    if not names:
        return ""
    return ", ".join(names)
