from __future__ import annotations

from types import SimpleNamespace

from client_wires.backends.claude_adapter import ClaudeCodeBackendAdapter


def test_claude_headless_command_forwards_explicit_opus_5_and_xhigh() -> None:
    command = ClaudeCodeBackendAdapter().build_headless_command(
        model="claude-opus-5",
        reasoning="xhigh",
        selected={},
        available={},
        prompt="implement the task",
    )

    assert command == [
        "claude",
        "--print",
        "--model",
        "claude-opus-5",
        "--effort",
        "xhigh",
        "--mcp-config",
        "/home/operator/Desktop/projects/self_orchestration/.mcp.json",
        "--strict-mcp-config",
        "--permission-mode",
        "bypassPermissions",
        "--output-format",
        "json",
        "implement the task",
    ]


def test_claude_keeps_existing_model_aliases_and_default_selection() -> None:
    adapter = ClaudeCodeBackendAdapter()

    assert adapter.default_model == "sonnet"
    assert adapter.default_reasoning == "high"
    assert adapter.model_options == (
        "sonnet",
        "opus",
        "opencode-go/glm-5.2",
        "opencode-go/qwen3.8-max",
        "kimi/k3",
        "kimi/k3-256k",
        "kimi/kimi-for-coding",
        "kimi/kimi-for-coding-highspeed",
    )
    assert adapter.normalize_model("opus") == "opus"


def test_claude_kimi_route_uses_anthropic_compatible_environment() -> None:
    adapter = ClaudeCodeBackendAdapter()
    selection = SimpleNamespace(model="kimi/k3", reasoning_effort="high")
    env = {"KIMI_API_KEY": "test-kimi-key"}

    adapter.prepare_env(env, selection)

    assert env["ANTHROPIC_BASE_URL"] == "https://api.kimi.com/coding/"
    assert env["ANTHROPIC_API_KEY"] == "test-kimi-key"
    assert env["ANTHROPIC_MODEL"] == "k3[1m]"
    assert env["CLAUDE_CODE_SUBAGENT_MODEL"] == "k3[1m]"
    assert adapter.build_llm_args(selection) == ["--effort", "high"]


def test_claude_kimi_headless_route_does_not_pass_a_claude_model_flag() -> None:
    command = ClaudeCodeBackendAdapter().build_headless_command(
        model="kimi/k3-256k",
        reasoning="medium",
        selected={},
        available={},
        prompt="hello",
    )

    assert "--model" not in command
    assert command[:4] == ["claude", "--print", "--effort", "medium"]
