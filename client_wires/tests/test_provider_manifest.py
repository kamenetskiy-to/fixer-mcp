from __future__ import annotations

from pathlib import Path

from client_wires.backends.manifest import load_manifest


ROOT = Path(__file__).resolve().parents[2]
CLAUDE_MANIFEST = ROOT / "client_wires" / "backends" / "manifest" / "claude.manifest.json"


def test_claude_manifest_exposes_opus_5_and_supported_effort_levels() -> None:
    manifest = load_manifest(CLAUDE_MANIFEST)

    assert manifest.models.default == "sonnet"
    assert manifest.models.options == [
        "sonnet",
        "opus",
        "kimi/k3",
        "kimi/k3-256k",
        "kimi/kimi-for-coding",
        "kimi/kimi-for-coding-highspeed",
    ]
    assert manifest.models.internal_id_map["kimi/k3"] == "kimi/k3"
    assert manifest.reasoning.options == ["low", "medium", "high", "xhigh", "max"]
    assert manifest.reasoning.flag_or_key == "--effort"
