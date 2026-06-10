from __future__ import annotations

import inspect
import unittest
from unittest.mock import patch

from client_wires import fixer_wire
from client_wires import fixer_wire_prompts


class FixerWirePromptExtractionTests(unittest.TestCase):
    def test_prompt_helpers_delegate_to_prompt_module_while_facade_symbols_remain(self) -> None:
        with patch.object(fixer_wire_prompts, "_build_fixer_prompt", return_value="patched fixer prompt") as delegated:
            prompt = fixer_wire._build_fixer_prompt()

        self.assertEqual(prompt, "patched fixer prompt")
        delegated.assert_called_once_with()
        self.assertIs(fixer_wire.NETRUNNER_KIND_MANUAL, fixer_wire_prompts.NETRUNNER_KIND_MANUAL)
        self.assertIs(fixer_wire.NETRUNNER_KIND_ACCEPTANCE, fixer_wire_prompts.NETRUNNER_KIND_ACCEPTANCE)
        self.assertTrue(hasattr(fixer_wire_prompts, "_build_netrunner_prompt"))
        self.assertTrue(hasattr(fixer_wire, "_build_netrunner_prompt"))
        self.assertNotIn(
            "Standard web stack guidance:",
            inspect.getsource(fixer_wire._build_standard_web_stack_guidance_block),
        )

    def test_prompt_wrappers_preserve_patchable_fixer_wire_dependencies(self) -> None:
        with patch.object(fixer_wire, "_build_default_how_to", return_value="Use patched facade guidance."):
            prompt = fixer_wire._build_netrunner_prompt(72, ["patched-mcp"], {})

        self.assertIn("- patched-mcp: Use patched facade guidance.", prompt)
