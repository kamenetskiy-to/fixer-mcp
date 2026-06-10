from __future__ import annotations

import types
import unittest
from unittest.mock import ANY, patch

from client_wires import fixer_wire
from client_wires import fixer_wire_launch_support


class _DummyOption:
    def __init__(self, label: str, value: object | None = None, *, disabled: bool = False, is_header: bool = False) -> None:
        self.label = label
        self.value = value
        self.disabled = disabled
        self.is_header = is_header


class LaunchSupportOwnershipTests(unittest.TestCase):
    def test_module_owns_passthrough_dangerous_sandbox_helper(self) -> None:
        resolved = fixer_wire_launch_support._ensure_passthrough_dangerous_sandbox(["--foo"])
        self.assertEqual(resolved, ["--foo", "--sandbox", "danger-full-access"])

    def test_facade_passthrough_dangerous_sandbox_wrapper_remains_compatible(self) -> None:
        original = ["--sandbox", "workspace-write", "--foo"]
        resolved = fixer_wire._ensure_passthrough_dangerous_sandbox(original)
        self.assertEqual(resolved, original)

    def test_module_owns_prefer_fixed_model_for_role_presets(self) -> None:
        module = types.SimpleNamespace(
            MODEL_DISPLAY_ORDER=["gpt-5.3-codex", "gpt-5.2"],
            MODEL_DEFAULT_EFFORT={"gpt-5.3-codex": "medium"},
        )
        fixer_wire_launch_support._prefer_fixed_model_for_role_presets(
            module,
            fixer_wire_model="gpt-5.5",
            fixer_wire_reasoning_effort="high",
        )
        self.assertEqual(module.MODEL_DISPLAY_ORDER[0], "gpt-5.5")
        self.assertEqual(module.MODEL_DEFAULT_EFFORT["gpt-5.5"], "high")

    def test_select_fresh_launch_selection_accepts_claude_through_facade(self) -> None:
        selection = fixer_wire._select_fresh_launch_selection(
            preset_backend="claude",
            preset_model="sonnet",
            preset_reasoning="medium",
            Option=_DummyOption,
            single_select_items=lambda *_a, **_k: None,
        )
        self.assertEqual(selection.backend, "claude")
        self.assertEqual(selection.model, "sonnet")


class LaunchSupportFacadePatchabilityTests(unittest.TestCase):
    def test_select_fresh_launch_selection_uses_patched_facade_callbacks(self) -> None:
        descriptor = types.SimpleNamespace(
            default_model="default-model",
            default_reasoning="default-reasoning",
            fresh_launch_supported=True,
        )
        with (
            patch.object(fixer_wire, "_select_backend_interactive", return_value="patched-backend") as select_backend,
            patch.object(fixer_wire, "_select_model_interactive", return_value="patched-model") as select_model,
            patch.object(fixer_wire, "_select_reasoning_interactive", return_value="patched-reasoning") as select_reasoning,
            patch.object(fixer_wire, "_backend_descriptor", return_value=descriptor) as backend_descriptor,
            patch.object(fixer_wire, "_normalize_backend_model", side_effect=lambda _desc, model: f"normalized-{model}"),
            patch.object(fixer_wire, "_normalize_backend_reasoning", side_effect=lambda _desc, reasoning: f"normalized-{reasoning}"),
        ):
            selection = fixer_wire._select_fresh_launch_selection(
                preset_backend=None,
                preset_model=None,
                preset_reasoning=None,
                Option=_DummyOption,
                single_select_items=lambda *_a, **_k: None,
            )

        self.assertEqual(selection.backend, "patched-backend")
        self.assertEqual(selection.model, "normalized-patched-model")
        self.assertEqual(selection.reasoning, "normalized-patched-reasoning")
        select_backend.assert_called_once()
        select_model.assert_called_once_with("patched-backend", "default-model", _DummyOption, ANY)
        select_reasoning.assert_called_once_with("patched-backend", "default-reasoning", _DummyOption, ANY)
        backend_descriptor.assert_called_once_with("patched-backend")

    def test_resolve_netrunner_launch_selection_uses_patched_facade_callbacks(self) -> None:
        descriptor = types.SimpleNamespace(
            default_model="default-model",
            default_reasoning="default-reasoning",
            fresh_launch_supported=True,
        )
        selected_session = fixer_wire.SessionRow(
            session_id=216,
            global_session_id=216,
            task_description="Task",
            status="pending",
            cli_backend="codex",
        )
        with (
            patch.object(fixer_wire, "_select_backend_interactive", return_value="patched-backend") as select_backend,
            patch.object(fixer_wire, "_select_model_interactive", return_value="patched-model") as select_model,
            patch.object(fixer_wire, "_select_reasoning_interactive", return_value="patched-reasoning") as select_reasoning,
            patch.object(fixer_wire, "_backend_descriptor", return_value=descriptor),
            patch.object(fixer_wire, "_normalize_backend_model", side_effect=lambda _desc, model: f"normalized-{model}"),
            patch.object(fixer_wire, "_normalize_backend_reasoning", side_effect=lambda _desc, reasoning: f"normalized-{reasoning}"),
        ):
            selection = fixer_wire._resolve_netrunner_launch_selection(
                selected_session,
                preset_backend=None,
                preset_model=None,
                preset_reasoning=None,
                dry_run=False,
                Option=_DummyOption,
                single_select_items=lambda *_a, **_k: None,
            )

        self.assertEqual(selection.backend, "patched-backend")
        self.assertEqual(selection.model, "normalized-patched-model")
        self.assertEqual(selection.reasoning, "normalized-patched-reasoning")
        select_backend.assert_called_once()
        select_model.assert_called_once()
        select_reasoning.assert_called_once()


if __name__ == "__main__":
    unittest.main()
