from __future__ import annotations

import inspect
import types
import unittest
from unittest.mock import patch

from client_wires import fixer_wire
from client_wires import fixer_wire_selectors


class _DummyOption:
    def __init__(self, label: str, value: object | None = None, *, disabled: bool = False, is_header: bool = False) -> None:
        self.label = label
        self.value = value
        self.disabled = disabled
        self.is_header = is_header


class FixerWireSelectorExtractionTests(unittest.TestCase):
    def test_selector_helpers_delegate_to_selector_module_while_facade_symbols_remain(self) -> None:
        with patch.object(fixer_wire_selectors, "_select_role_interactive", return_value="netrunner") as delegated:
            selected = fixer_wire._select_role_interactive(_DummyOption, lambda *_a, **_k: None)

        self.assertEqual(selected, "netrunner")
        delegated.assert_called_once()
        self.assertTrue(hasattr(fixer_wire, "_select_session_interactive"))
        self.assertTrue(hasattr(fixer_wire_selectors, "_select_session_interactive"))
        self.assertIn(
            "Select netrunner session",
            inspect.getsource(fixer_wire_selectors._select_session_interactive),
        )
        self.assertNotIn(
            "Select netrunner session",
            inspect.getsource(fixer_wire._select_session_interactive),
        )

    def test_selector_wrappers_preserve_patchable_fixer_wire_dependencies(self) -> None:
        rows = [
            fixer_wire.SessionRow(
                session_id=42,
                global_session_id=420,
                task_description="Original task title",
                status="in_progress",
                codex_session_id="",
            )
        ]
        captured: dict[str, object] = {}

        def choose(options: list[_DummyOption], **_kwargs: object) -> int:
            captured["options"] = options
            return 42

        with patch.object(fixer_wire, "_session_title", return_value="patched title"):
            selected = fixer_wire._select_session_interactive(rows, _DummyOption, choose)

        self.assertEqual(selected.session_id, 42)
        labels = [option.label for option in captured["options"] if not option.is_header]
        self.assertTrue(any("patched title" in label for label in labels))

        descriptor = types.SimpleNamespace(
            label="Patched CLI",
            model_options=["patched-model"],
            default_model="patched-model",
            reasoning_options=["patched-reasoning"],
            default_reasoning="patched-reasoning",
        )
        with patch.object(fixer_wire, "_backend_descriptor", return_value=descriptor) as patched_descriptor:
            selected_model = fixer_wire._select_model_interactive(
                "patched",
                "",
                _DummyOption,
                lambda *_a, **_k: "patched-model",
            )

        self.assertEqual(selected_model, "patched-model")
        patched_descriptor.assert_called_once_with("patched")
