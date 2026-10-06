from __future__ import annotations

import json
from pathlib import Path
import pytest

from client_wires.backends.base import (
    OVERLAY_ENV_VAR,
    BackendDescriptor,
    ModelCatalogOverlayError,
    apply_model_catalog_overlay,
    load_model_catalog_overlay,
    overlay_manifest,
)
from client_wires.backends.catalog import load_backend_entry
from client_wires.backends.commandcode_adapter import CommandCodeBackendAdapter
from client_wires.backends.manifest import load_manifest
from client_wires.backends.pi_adapter import PiBackendAdapter

ROOT = Path(__file__).resolve().parents[2]
BACKEND_CATALOG_PATH = ROOT / "client_wires" / "backends" / "data" / "backend-catalog.json"
PI_MANIFEST_PATH = ROOT / "client_wires" / "backends" / "manifest" / "pi.manifest.json"
COMMANDCODE_MANIFEST_PATH = ROOT / "client_wires" / "backends" / "manifest" / "commandcode.manifest.json"


# =============================================================================
# 1. Overlay Merge Precedence
# =============================================================================

def test_overlay_merge_precedence_adds_models_and_wins_on_conflict(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    overlay_file = tmp_path / "model-catalog-overlay.json"
    overlay_data = {
        "backends": {
            "pi": {
                "models": {
                    "custom-provider/gpt-6-ultra": {
                        "retired": False,
                    },
                    "openai-codex/gpt-6-sol": {
                        "retired": False,
                    },
                },
                "model_options": [
                    "operator-personal/local-fine-tune-v1",
                ],
            },
        },
    }
    overlay_file.write_text(json.dumps(overlay_data), encoding="utf-8")
    monkeypatch.setenv(OVERLAY_ENV_VAR, str(overlay_file))

    adapter = PiBackendAdapter()

    # Base packaged model is preserved
    assert "deepseek-v4.1-flash" in adapter.model_options
    # Overlay model additions win and are included
    assert "custom-provider/gpt-6-ultra" in adapter.model_options
    assert "operator-personal/local-fine-tune-v1" in adapter.model_options
    assert "openai-codex/gpt-6-sol" in adapter.model_options

    # normalize_model accepts the new overlay model without error
    assert adapter.normalize_model("custom-provider/gpt-6-ultra") == "custom-provider/gpt-6-ultra"

    # Direct descriptor creation also inherits merged overlay
    desc = BackendDescriptor(
        name="pi",
        label="Pi",
        description="Pi test",
        default_model="deepseek-v4.1-flash",
        default_reasoning="high",
        model_options=("deepseek-v4.1-flash",),
        reasoning_options=("low", "high"),
    )
    assert "custom-provider/gpt-6-ultra" in desc.model_options
    assert "operator-personal/local-fine-tune-v1" in desc.model_options


def test_overlay_direct_backend_key_format(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    """The overlay supports direct top-level backend keys without 'backends' wrapper."""
    overlay_file = tmp_path / "model-catalog-overlay.json"
    overlay_data = {
        "commandcode": {
            "models": {
                "commandcode/custom/qwen-local": {"retired": False},
            },
        },
    }
    overlay_file.write_text(json.dumps(overlay_data), encoding="utf-8")
    monkeypatch.setenv(OVERLAY_ENV_VAR, str(overlay_file))

    adapter = CommandCodeBackendAdapter()
    assert "commandcode/custom/qwen-local" in adapter.model_options
    assert adapter.normalize_model("commandcode/custom/qwen-local") == "commandcode/custom/qwen-local"


# =============================================================================
# 2. Retired-Marker Suppression
# =============================================================================

def test_retired_marker_suppression_prevents_packaged_resurrection(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    """An explicit 'retired': true marker suppresses packaged entries and updates cannot resurrect them."""
    overlay_file = tmp_path / "model-catalog-overlay.json"
    packaged_luna = "openai-codex/gpt-5.6-luna"
    packaged_flash = "deepseek-v4-flash"

    overlay_data = {
        "pi": {
            "models": {
                packaged_luna: {"retired": True},
            },
            "retired": [packaged_flash],
        },
    }
    overlay_file.write_text(json.dumps(overlay_data), encoding="utf-8")
    monkeypatch.setenv(OVERLAY_ENV_VAR, str(overlay_file))

    adapter = PiBackendAdapter()

    # Retired models must NOT appear in model_options
    assert packaged_luna not in adapter.model_options
    assert packaged_flash not in adapter.model_options

    # Normalization must reject the retired model with actionable error
    with pytest.raises(RuntimeError, match="Unsupported model"):
        adapter.normalize_model(packaged_luna)

    with pytest.raises(RuntimeError, match="Unsupported model"):
        adapter.normalize_model(packaged_flash)

    # An updated packaged release list being passed in will NOT resurrect the retired models
    updated_packaged = (
        "deepseek-v4.1-flash",
        packaged_luna,
        packaged_flash,
        "openai-codex/gpt-6-sol",
    )
    merged = apply_model_catalog_overlay("pi", updated_packaged, overlay_path=overlay_file)
    assert packaged_luna not in merged
    assert packaged_flash not in merged
    assert "deepseek-v4.1-flash" in merged
    assert "openai-codex/gpt-6-sol" in merged


def test_retired_marker_via_dict_list(tmp_path: Path) -> None:
    overlay_file = tmp_path / "model-catalog-overlay.json"
    overlay_data = {
        "pi": {
            "model_options": [
                {"id": "deepseek-v4.1-flash", "retired": True},
                "brand-new-model",
            ],
        },
    }
    overlay_file.write_text(json.dumps(overlay_data), encoding="utf-8")

    merged = apply_model_catalog_overlay("pi", ("deepseek-v4.1-flash", "deepseek-v4-flash"), overlay_path=overlay_file)
    assert "deepseek-v4.1-flash" not in merged
    assert "deepseek-v4-flash" in merged
    assert "brand-new-model" in merged


# =============================================================================
# 3. Malformed Overlay Error
# =============================================================================

def test_malformed_overlay_invalid_json(tmp_path: Path) -> None:
    bad_file = tmp_path / "bad.json"
    bad_file.write_text("{ not valid json !!! }", encoding="utf-8")

    with pytest.raises(ModelCatalogOverlayError, match="invalid JSON"):
        load_model_catalog_overlay(bad_file)


def test_malformed_overlay_non_object_root(tmp_path: Path) -> None:
    bad_file = tmp_path / "bad.json"
    bad_file.write_text("[\"pi\", \"codex\"]", encoding="utf-8")

    with pytest.raises(ModelCatalogOverlayError, match="root must be a JSON object"):
        load_model_catalog_overlay(bad_file)


def test_malformed_overlay_non_object_backend(tmp_path: Path) -> None:
    bad_file = tmp_path / "bad.json"
    bad_file.write_text(json.dumps({"pi": "invalid-string-instead-of-object"}), encoding="utf-8")

    with pytest.raises(ModelCatalogOverlayError, match="entry for backend 'pi' must be an object"):
        load_model_catalog_overlay(bad_file)


def test_malformed_overlay_non_boolean_retired(tmp_path: Path) -> None:
    bad_file = tmp_path / "bad.json"
    bad_file.write_text(json.dumps({"pi": {"models": {"test-model": {"retired": "not-a-bool"}}}}), encoding="utf-8")

    with pytest.raises(ModelCatalogOverlayError, match="'retired' marker for model 'test-model' in backend 'pi' must be a boolean"):
        load_model_catalog_overlay(bad_file)


def test_malformed_overlay_invalid_models_type(tmp_path: Path) -> None:
    bad_file = tmp_path / "bad.json"
    bad_file.write_text(json.dumps({"pi": {"models": 12345}}), encoding="utf-8")

    with pytest.raises(ModelCatalogOverlayError, match="'models' in backend 'pi' must be an object or list"):
        load_model_catalog_overlay(bad_file)


def test_malformed_overlay_invalid_retired_type(tmp_path: Path) -> None:
    bad_file = tmp_path / "bad.json"
    bad_file.write_text(json.dumps({"pi": {"retired": "not-a-list"}}), encoding="utf-8")

    with pytest.raises(ModelCatalogOverlayError, match="'retired' in backend 'pi' must be a list"):
        load_model_catalog_overlay(bad_file)


def test_missing_overlay_returns_clean_base_options(tmp_path: Path) -> None:
    non_existent = tmp_path / "does-not-exist.json"
    base = ("model-a", "model-b")
    result = apply_model_catalog_overlay("pi", base, overlay_path=non_existent)
    assert result == base


# =============================================================================
# 4. Catalog Membership for gpt-6 Routes and MiMo 2.6 Routes
# =============================================================================

def test_backend_catalog_json_contains_gpt6_and_mimo26_routes() -> None:
    payload = json.loads(BACKEND_CATALOG_PATH.read_text(encoding="utf-8"))
    pi_options = payload["backends"]["pi"]["model_options"]

    # Defect (1) Upstream routes in Pi catalog
    assert "openai-codex/gpt-6-sol" in pi_options
    assert "openai-codex/gpt-6-luna" in pi_options
    assert "openai-codex/gpt-6-astra" in pi_options

    # Preserved MiMo 2.6 routes in Pi catalog
    assert "opencode-personal/mimo-v2.6-flash" in pi_options
    assert "opencode-personal/mimo-v2.6-pro" in pi_options
    assert "commandcode/xiaomi/mimo-v2.6-flash" in pi_options
    assert "commandcode/xiaomi/mimo-v2.6-pro" in pi_options

    # CommandCode catalog preserves MiMo 2.6 routes
    commandcode_options = payload["backends"]["commandcode"]["model_options"]
    assert "commandcode/xiaomi/mimo-v2.6-flash" in commandcode_options
    assert "commandcode/xiaomi/mimo-v2.6-pro" in commandcode_options


def test_pi_manifest_contains_gpt6_and_mimo26_routes() -> None:
    manifest = load_manifest(PI_MANIFEST_PATH)

    # Defect (1) Upstream routes in Pi manifest
    for gpt6_route in ("openai-codex/gpt-6-sol", "openai-codex/gpt-6-luna", "openai-codex/gpt-6-astra"):
        assert gpt6_route in manifest.models.options
        assert manifest.models.internal_id_map.get(gpt6_route) == gpt6_route

    # Preserved MiMo 2.6 routes in Pi manifest
    for mimo_route in (
        "opencode-personal/mimo-v2.6-flash",
        "opencode-personal/mimo-v2.6-pro",
        "commandcode/xiaomi/mimo-v2.6-flash",
        "commandcode/xiaomi/mimo-v2.6-pro",
    ):
        assert mimo_route in manifest.models.options
        assert manifest.models.internal_id_map.get(mimo_route) == mimo_route

    # Reasoning options in manifest cover full low..max ladder
    for expected_effort in ("low", "medium", "high", "xhigh", "max"):
        assert expected_effort in manifest.reasoning.options


def test_commandcode_manifest_validates_and_contains_mimo26_routes() -> None:
    manifest = load_manifest(COMMANDCODE_MANIFEST_PATH)
    assert manifest.provider == "commandcode"

    assert "commandcode/xiaomi/mimo-v2.6-flash" in manifest.models.options
    assert "commandcode/xiaomi/mimo-v2.6-pro" in manifest.models.options
    assert manifest.models.internal_id_map.get("commandcode/xiaomi/mimo-v2.6-flash") == "xiaomi/mimo-v2.6-flash"
    assert manifest.models.internal_id_map.get("commandcode/xiaomi/mimo-v2.6-pro") == "xiaomi/mimo-v2.6-pro"

    assert manifest.reasoning.options == ["low", "medium", "high", "xhigh", "max"]


def test_pi_adapter_resolves_gpt6_routes_and_preserves_mimo26(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    models_config = {
        "providers": {
            "commandcode-team": {
                "baseUrl": "https://api.commandcode.ai/provider/v1",
                "api": "openai-completions",
                "models": [
                    {"id": "xiaomi/mimo-v2.6-pro"},
                    {"id": "xiaomi/mimo-v2.6-flash"},
                ],
            },
            "cmd-test": {
                "baseUrl": "https://api.commandcode.ai/provider/v1",
                "api": "openai-completions",
                "models": [
                    {"id": "xiaomi/mimo-v2.6-pro"},
                    {"id": "xiaomi/mimo-v2.6-flash"},
                ],
            },
        }
    }
    (tmp_path / "models.json").write_text(json.dumps(models_config), encoding="utf-8")
    monkeypatch.setenv("PI_AGENT_HOME", str(tmp_path))

    adapter = PiBackendAdapter()

    expected_normalized = {
        "commandcode/xiaomi/mimo-v2.6-pro": "mimo-v2.6-pro",
        "mimo-v2.6-flash": "commandcode/xiaomi/mimo-v2.6-flash",
    }

    for route in (
        "openai-codex/gpt-6-sol",
        "openai-codex/gpt-6-luna",
        "openai-codex/gpt-6-astra",
        "opencode-personal/mimo-v2.6-flash",
        "opencode-personal/mimo-v2.6-pro",
        "commandcode/xiaomi/mimo-v2.6-flash",
        "commandcode/xiaomi/mimo-v2.6-pro",
        "commandcode-team/xiaomi/mimo-v2.6-pro",
        "cmd-test/xiaomi/mimo-v2.6-flash",
    ):
        assert route in adapter.model_options
        expected = expected_normalized.get(route, route)
        assert adapter.normalize_model(route) == expected

    # Verify generic default internal ID resolution (not mapped to private operator ID)
    assert adapter._internal_model_id("commandcode/xiaomi/mimo-v2.6-pro") == "commandcode/xiaomi/mimo-v2.6-pro"
    assert adapter._internal_model_id("commandcode/xiaomi/mimo-v2.6-flash") == "commandcode/xiaomi/mimo-v2.6-flash"
    assert adapter._internal_model_id("mimo-v2.6-pro") == "commandcode/xiaomi/mimo-v2.6-pro"

    # Verify explicitly registered named routes are preserved
    assert adapter._internal_model_id("commandcode-team/xiaomi/mimo-v2.6-pro") == "commandcode-team/xiaomi/mimo-v2.6-pro"
    assert adapter._internal_model_id("cmd-test/xiaomi/mimo-v2.6-flash") == "cmd-test/xiaomi/mimo-v2.6-flash"

    # Unconfigured named route must fail closed
    assert "commandcode-unknown/xiaomi/mimo-v2.6-pro" not in adapter.model_options
    with pytest.raises(RuntimeError, match="Unsupported model"):
        adapter.normalize_model("commandcode-unknown/xiaomi/mimo-v2.6-pro")
