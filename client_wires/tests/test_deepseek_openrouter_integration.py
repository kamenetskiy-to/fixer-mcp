from __future__ import annotations

import json
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch

import pytest

from client_wires.backends.catalog import load_backend_entry
from client_wires.backends.codex_adapter import CodexBackendAdapter
from client_wires.backends.junie_adapter import (
    JUNIE_CANONICAL_DEEPSEEK_V4_FLASH_MODEL,
    JunieBackendAdapter,
)


CODEX_MODEL = "deepseek/deepseek-v4-flash-0731"
JUNIE_MODEL = "deepseek-v4-flash-0731"


class _FakeCodexInner:
    command = "codex"
    supports_resume = True

    def build_llm_args(self, selection: object) -> list[str]:
        model = str(getattr(selection, "model"))
        reasoning = str(getattr(selection, "reasoning_effort"))
        return ["--model", model, "-c", f'model_reasoning_effort="{reasoning}"']

    def build_execution_args(self, prefs: object) -> list[str]:
        del prefs
        return []

    def build_mcp_flags(self, selected: object, available: object) -> list[str]:
        del selected, available
        return []

    def build_prompt_args(self, prompt: str) -> list[str]:
        return [prompt] if prompt else []

    def prepare_env(self, env: dict[str, str], selection: object) -> None:
        del env, selection


@pytest.fixture
def codex_adapter() -> CodexBackendAdapter:
    return CodexBackendAdapter(_FakeCodexInner())


def _expected_codex_overrides(home: Path) -> list[str]:
    del home
    catalog_path = Path(__file__).parents[1] / "backends" / "data" / "codex-deepseek-models.json"
    return [
        "-c",
        'model_provider="openrouter"',
        "-c",
        'model_providers.openrouter.name="OpenRouter"',
        "-c",
        'model_providers.openrouter.base_url="https://openrouter.ai/api/v1"',
        "-c",
        'model_providers.openrouter.env_key="OPENROUTER_API_KEY"',
        "-c",
        'model_providers.openrouter.wire_api="responses"',
        "-c",
        f'model_catalog_json="{catalog_path}"',
        "-c",
        "model_context_window=500000",
        "-c",
        "model_auto_compact_token_limit=425000",
    ]


def test_codex_interactive_command_uses_catalog_driven_openrouter_overrides(
    codex_adapter: CodexBackendAdapter, tmp_path: Path
) -> None:
    selection = SimpleNamespace(model=CODEX_MODEL, reasoning_effort="high")
    with patch.object(Path, "home", return_value=tmp_path):
        args = codex_adapter.build_llm_args(selection)

    assert args == [
        "--model",
        CODEX_MODEL,
        "-c",
        'model_reasoning_effort="high"',
        *_expected_codex_overrides(tmp_path),
    ]


def test_codex_headless_command_uses_exact_proven_model_configuration(
    codex_adapter: CodexBackendAdapter, tmp_path: Path
) -> None:
    with patch.object(Path, "home", return_value=tmp_path):
        command = codex_adapter.build_headless_command(
            model=CODEX_MODEL,
            reasoning="high",
            selected={},
            available={},
            prompt="Do the task",
        )

    assert command[:4] == ["codex", "--model", CODEX_MODEL, "-c"]
    assert command[4] == 'model_reasoning_effort="high"'
    expected_overrides = _expected_codex_overrides(tmp_path)
    assert command[5 : 5 + len(expected_overrides)] == expected_overrides
    assert command[-3:] == ["exec", "--skip-git-repo-check", "Do the task"]


def test_codex_secret_stays_in_inherited_environment_and_out_of_arguments(
    codex_adapter: CodexBackendAdapter, tmp_path: Path
) -> None:
    secret = "test-openrouter-secret-that-must-not-leak"
    env = {"OPENROUTER_API_KEY": secret}
    selection = SimpleNamespace(model=CODEX_MODEL, reasoning_effort="high")

    codex_adapter.prepare_env(env, selection)
    with patch.object(Path, "home", return_value=tmp_path):
        args = codex_adapter.build_llm_args(selection)

    assert env["OPENROUTER_API_KEY"] == secret
    assert secret not in " ".join(args)


def test_codex_resume_keeps_the_same_thread_with_fresh_deepseek_overrides(
    codex_adapter: CodexBackendAdapter, tmp_path: Path
) -> None:
    selection = SimpleNamespace(model=CODEX_MODEL, reasoning_effort="high")
    with patch.object(Path, "home", return_value=tmp_path):
        option_args = codex_adapter.build_llm_args(selection)

    assert codex_adapter.build_resume_command(option_args, " session-123 ") == [
        "codex",
        *option_args,
        "resume",
        "session-123",
    ]


def test_codex_native_model_behavior_is_unchanged(codex_adapter: CodexBackendAdapter) -> None:
    selection = SimpleNamespace(model="gpt-5.6-sol", reasoning_effort="high")
    assert codex_adapter.build_llm_args(selection) == [
        "--model",
        "gpt-5.6-sol",
        "-c",
        'model_reasoning_effort="high"',
    ]


@pytest.mark.parametrize(
    "alias",
    [
        "deepseek",
        "DeepSeek V4 Flash 0731",
        "deepseek-v4-flash-0731",
        CODEX_MODEL,
        "custom:deepseek-v4-flash-0731",
    ],
)
def test_junie_normalizes_deepseek_aliases_to_user_profile(alias: str) -> None:
    adapter = JunieBackendAdapter()
    assert adapter.normalize_model(alias) == JUNIE_CANONICAL_DEEPSEEK_V4_FLASH_MODEL


def test_junie_uses_user_scope_custom_profile_without_api_key_flags() -> None:
    adapter = JunieBackendAdapter()
    command = adapter.build_headless_command(
        model=CODEX_MODEL,
        reasoning="default",
        selected={"fixer_mcp": {"command": "fixer-mcp"}},
        available={},
        prompt="Do the task",
    )

    assert command[:5] == [
        "junie",
        "--model",
        "custom:deepseek-v4-flash-0731",
        "--model-default-locations",
        "true",
    ]
    assert ["--skill-location", ".junie/fixer-runtime/skills"] == command[5:7]
    assert ["--mcp-location", ".junie/fixer-runtime/mcp"] == command[11:13]
    assert "--openrouter-api-key" not in command
    assert "OPENROUTER_API_KEY" not in " ".join(command)
    assert command[-4:] == ["--output-format", "json", "--task", "Do the task"]


def test_catalog_and_manifests_declare_deepseek_routes() -> None:
    codex_entry = load_backend_entry("codex")
    junie_entry = load_backend_entry("junie")
    assert CODEX_MODEL in codex_entry["model_options"]
    assert codex_entry["model_config_overrides"][CODEX_MODEL] == {
        "model_provider": "openrouter",
        "model_providers.openrouter.name": "OpenRouter",
        "model_providers.openrouter.base_url": "https://openrouter.ai/api/v1",
        "model_providers.openrouter.env_key": "OPENROUTER_API_KEY",
        "model_providers.openrouter.wire_api": "responses",
        "model_catalog_json": "__FIXER_DEEPSEEK_MODEL_CATALOG__",
        "model_context_window": 500000,
        "model_auto_compact_token_limit": 425000,
    }
    assert JUNIE_MODEL in junie_entry["model_options"]

    manifest_dir = Path(__file__).parents[1] / "backends" / "manifest"
    codex_manifest = json.loads((manifest_dir / "codex.manifest.json").read_text())
    junie_manifest = json.loads((manifest_dir / "junie.manifest.json").read_text())
    assert codex_manifest["models"]["config_overrides"][CODEX_MODEL] == codex_entry[
        "model_config_overrides"
    ][CODEX_MODEL]
    assert junie_manifest["models"]["internal_id_map"][JUNIE_MODEL] == (
        "custom:deepseek-v4-flash-0731"
    )
