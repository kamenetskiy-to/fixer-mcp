"""Command Code effort-flag mapping (MiMo 2.6 family is fixed-reasoning)."""

from client_wires.backends.commandcode_adapter import (
    _commandcode_effort,
    commandcode_reasoning_options,
)


def test_mimo_v26_family_never_sends_effort_flag() -> None:
    # command-code CLI 1.69.0 rejects any --effort for these models
    # ("MiMo V2.6 Pro has no adjustable reasoning effort").
    for model in (
        "xiaomi/mimo-v2.6-pro",
        "xiaomi/mimo-v2.6-flash",
        "xiaomi/mimo-v2.6-pro-ultraspeed",
        "commandcode/xiaomi/mimo-v2.6-pro",
    ):
        assert _commandcode_effort(model, "max") is None
        assert _commandcode_effort(model, "low") is None
        assert commandcode_reasoning_options(model) == ()


def test_adjustable_models_keep_effort_mapping() -> None:
    assert _commandcode_effort("deepseek/deepseek-v4-flash", "max") == "max"
    assert _commandcode_effort("xiaomi/mimo-v2.5-pro", "max") is None
