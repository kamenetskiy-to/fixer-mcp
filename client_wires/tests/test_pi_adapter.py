"""Coverage for the `openai-codex/gpt-5.6-luna` Pi model lane.

The broad Pi adapter suite lives in `tests/test_pi_adapter.py`. This module pins
the cross-provider Codex model a permanent Project Hands lane on `pi` selects,
because that model is declared in three places that must agree:

* `client_wires/backends/data/backend-catalog.json` - the launcher/TUI surface,
* `client_wires/backends/manifest/pi.manifest.json` - the provider manifest,
* `client_wires/backends/pi_adapter.py` - the argv and the per-model guard.

Reasoning expectations mirror the model's `thinkingLevelMap` in
`~/.pi/agent/models-store.json`
(``{"xhigh": "xhigh", "max": "max", "minimal": "low"}``), verified against the
installed `pi` 0.85.1 (`pi --list-models` advertises `openai-codex/gpt-5.6-luna`
with thinking enabled). `low`, `medium` and `high` are absent from that map, so
Pi passes them through unchanged - which is exactly what keeps `high` sendable
for a Hands lane on this model.
"""

from __future__ import annotations

import sys
from pathlib import Path
from types import SimpleNamespace

import pytest

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT))

from client_wires.backends.catalog import load_backend_entry
from client_wires.backends.manifest import load_manifest
from client_wires.backends.pi_adapter import (
    PI_MODEL_INTERNAL_IDS,
    PI_MODEL_THINKING_LEVELS,
    PI_REASONING_OPTIONS,
    PiBackendAdapter,
    pi_supported_thinking_levels,
)

LUNA = "openai-codex/gpt-5.6-luna"
PI_DEFAULT_MODEL = "deepseek-v4.1-flash"


@pytest.fixture
def adapter() -> PiBackendAdapter:
    return PiBackendAdapter()


def test_luna_is_declared_across_catalog_manifest_and_adapter(adapter: PiBackendAdapter) -> None:
    entry = load_backend_entry("pi")
    manifest = load_manifest(ROOT / "client_wires" / "backends" / "manifest" / "pi.manifest.json")

    assert LUNA in entry["model_options"]
    assert LUNA in manifest.models.options
    # Identity mapping: the catalog id already is the fully qualified id
    # `pi --model` takes, and the manifest is what the launcher reads.
    assert manifest.models.internal_id_map[LUNA] == LUNA
    assert LUNA in adapter.model_options

    # The interactive Pi base harness default must not move for this lane.
    assert entry["default_model"] == PI_DEFAULT_MODEL
    assert manifest.models.default == PI_DEFAULT_MODEL
    assert adapter.default_model == PI_DEFAULT_MODEL


def test_luna_normalizes_to_the_fully_qualified_pi_model_id(adapter: PiBackendAdapter) -> None:
    assert adapter.normalize_model(LUNA) == LUNA
    assert adapter._internal_model_id(LUNA) == LUNA
    assert PI_MODEL_INTERNAL_IDS[LUNA] == LUNA


def test_luna_reasoning_map_matches_the_pi_model_store() -> None:
    # Only the keys the store declares are listed: an absent key means Pi
    # forwards the level unchanged, while `xhigh`/`max` count as supported only
    # when the map declares them explicitly.
    assert PI_MODEL_THINKING_LEVELS[LUNA] == {
        "xhigh": "xhigh",
        "max": "max",
        "minimal": "low",
    }


def test_luna_supports_every_level_on_the_pi_reasoning_surface() -> None:
    # Unlike `openai-codex/gpt-5.3-codex-spark` (which declares no `max`), luna
    # declares `max`, so the whole low/high/max backend surface is sendable.
    # `high` is absent from the map and therefore passed through unchanged; the
    # guard must not report it as a silently clamped level.
    assert PI_REASONING_OPTIONS == ("low", "high", "max")
    assert pi_supported_thinking_levels(LUNA) == PI_REASONING_OPTIONS


def test_luna_high_and_max_reach_the_pi_argv(adapter: PiBackendAdapter) -> None:
    for reasoning in ("high", "max"):
        assert adapter.build_llm_args(
            SimpleNamespace(model=LUNA, reasoning_effort=reasoning)
        ) == ["--model", LUNA, "--thinking", reasoning]


def test_luna_rejects_a_level_outside_the_pi_reasoning_surface(adapter: PiBackendAdapter) -> None:
    # `medium` exists in Pi's own CLI level list but is not part of this
    # backend's declared reasoning surface; accepting it would silently widen
    # the lane.
    with pytest.raises(RuntimeError, match="Unsupported reasoning"):
        adapter.build_llm_args(SimpleNamespace(model=LUNA, reasoning_effort="medium"))


def test_luna_headless_command_uses_the_fully_qualified_model_id(adapter: PiBackendAdapter) -> None:
    command = adapter.build_headless_command(
        model=LUNA,
        reasoning="high",
        selected={},
        available={},
        prompt="probe",
    )

    assert command[command.index("--model") + 1] == LUNA
    assert command[command.index("--thinking") + 1] == "high"
    assert command[-2:] == ["-p", "probe"]
