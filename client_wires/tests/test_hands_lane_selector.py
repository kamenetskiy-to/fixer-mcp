"""Focused tests for the Project Hands lane selector.

Regression target (wave 828 / backlog 182): on a host where the interactive
backend picker offered a backend with no durable lane (``pi``), the wire-injected
lane-selector callback raised a bare ``RuntimeError`` that escaped the curses TUI
as a raw traceback. The selector must instead:

* only offer backends that have a registered lane, and
* turn an unknown/unavailable lane into a governed ``[fixer-wire]`` error that
  lists the per-lane inventory and the reason each lane is unavailable.
"""

from __future__ import annotations

try:
    from _provider_stubs import provider_stub_path
except ImportError:  # package-style import
    from ._provider_stubs import provider_stub_path

import unittest
from unittest.mock import patch

from client_wires import fixer_wire
from client_wires import fixer_wire_hands_context
from client_wires import fixer_wire_netrunner_launch
from client_wires import fixer_wire_selectors


class _Option:
    def __init__(
        self,
        label: str,
        value: object | None = None,
        *,
        disabled: bool = False,
        is_header: bool = False,
        **kwargs: object,
    ) -> None:
        self.label = label
        self.value = value
        self.disabled = disabled
        self.is_header = is_header


def _lane(provider: str, model: str = "model-x", reasoning: str = "high") -> object:
    return fixer_wire_netrunner_launch.ProjectHandsLane(provider, model, reasoning)


def _inventory(*entries: tuple[str, str, str, bool, str]) -> tuple[object, ...]:
    return tuple(
        fixer_wire_hands_context.HandsLaneAvailability(
            provider=provider,
            backend=backend,
            command=command,
            available=available,
            reason=reason,
        )
        for provider, backend, command, available, reason in entries
    )


def _backend_picker(name: str):
    def pick(_preferred: str, _option: object, _chooser: object) -> str:
        return name

    return pick


def _model_picker(name: str = "picked-model"):
    def pick(*_args: object, **_kwargs: object) -> str:
        return name

    return pick


class HandsLaneInventoryTests(unittest.TestCase):
    def test_present_binary_marks_lane_available(self) -> None:
        availability = fixer_wire_hands_context.hands_lane_availability(
            "codex",
            "codex",
            which=lambda binary: "/usr/local/bin/codex" if binary == "codex" else None,
            platform="darwin",
        )

        self.assertTrue(availability.available)
        self.assertEqual(availability.reason, "")
        self.assertEqual(availability.command, "codex")

    def test_missing_binary_reports_the_missing_command(self) -> None:
        availability = fixer_wire_hands_context.hands_lane_availability(
            "kimi",
            "kimi-code",
            which=lambda _binary: None,
            platform="linux",
        )

        self.assertFalse(availability.available)
        self.assertIn("not installed or not on PATH", availability.reason)
        self.assertIn("'kimi'", availability.reason)

    def test_commandcode_accepts_any_known_wrapper_binary(self) -> None:
        availability = fixer_wire_hands_context.hands_lane_availability(
            "commandcode",
            "commandcode",
            which=lambda binary: "/usr/local/bin/cmdc" if binary == "cmdc" else None,
            platform="darwin",
        )

        self.assertTrue(availability.available)
        self.assertEqual(availability.command, "cmdc")

    def test_antigravity_is_unsupported_on_linux(self) -> None:
        on_linux = fixer_wire_hands_context.hands_lane_availability(
            "antigravity",
            "antigravity",
            which=lambda binary: f"/usr/bin/{binary}",
            platform="linux",
        )
        on_mac = fixer_wire_hands_context.hands_lane_availability(
            "antigravity",
            "antigravity",
            which=lambda binary: f"/usr/local/bin/{binary}",
            platform="darwin",
        )

        self.assertFalse(on_linux.available)
        self.assertIn("unsupported on this platform", on_linux.reason)
        self.assertTrue(on_mac.available)

    def test_inventory_renders_every_lane_with_its_reason(self) -> None:
        lanes = (_lane("codex"), _lane("kimi"), _lane("antigravity"))
        inventory = fixer_wire_hands_context.hands_lane_inventory(
            lanes,
            which=lambda binary: "/usr/bin/codex" if binary == "codex" else None,
            platform="linux",
        )
        rendered = fixer_wire_hands_context.render_hands_lane_inventory(inventory)

        self.assertIn("codex [codex]: available", rendered)
        self.assertIn("kimi [kimi]: unavailable", rendered)
        self.assertIn("antigravity [agy]: unavailable - unsupported on this platform (linux)", rendered)


class ProjectHandsLaneSelectorTests(unittest.TestCase):
    def setUp(self) -> None:
        self._provider_stubs = provider_stub_path()
        self._provider_stubs.__enter__()

    def tearDown(self) -> None:
        self._provider_stubs.__exit__(None, None, None)

    def test_unknown_injected_lane_is_a_governed_error_with_inventory(self) -> None:
        # The old crash: the wire-injected backend picker offered `pi` although
        # no `pi` lane existed, then `_pick_model` raised a bare RuntimeError.
        lanes = (_lane("codex"),)
        inventory = _inventory(("codex", "codex", "codex", True, ""))

        with self.assertRaises(SystemExit) as caught:
            fixer_wire_selectors._select_project_hands_lane_interactive(
                lanes,
                "codex",
                _Option,
                lambda *_args, **_kwargs: None,
                select_backend_interactive=_backend_picker("pi"),
                select_model_interactive=_model_picker(),
                lane_inventory=lambda _lanes: inventory,
            )

        message = str(caught.exception)
        self.assertNotIsInstance(caught.exception, RuntimeError)
        self.assertIn("[fixer-wire]", message)
        self.assertIn("'pi' is not a registered lane", message)
        self.assertIn("codex [codex]: available", message)
        self.assertIn("Register the lane through the Fixer MCP control plane", message)

    def test_known_but_unavailable_lane_reports_the_missing_command(self) -> None:
        lanes = (_lane("codex"), _lane("claude"))
        inventory = _inventory(
            ("codex", "codex", "codex", True, ""),
            ("claude", "claude", "claude", False, "command 'claude' not installed or not on PATH"),
        )

        with self.assertRaises(SystemExit) as caught:
            fixer_wire_selectors._select_project_hands_lane_interactive(
                lanes,
                "codex",
                _Option,
                lambda *_args, **_kwargs: None,
                select_backend_interactive=_backend_picker("claude"),
                select_model_interactive=_model_picker(),
                lane_inventory=lambda _lanes: inventory,
            )

        message = str(caught.exception)
        self.assertIn("lane 'claude' is unavailable", message)
        self.assertIn("command 'claude' not installed or not on PATH", message)
        self.assertIn("codex [codex]: available", message)

    def test_a_host_with_no_launchable_lane_lists_every_reason(self) -> None:
        lanes = (
            _lane("commandcode"),
            _lane("codex"),
            _lane("claude"),
            _lane("kimi"),
            _lane("antigravity"),
            _lane("grok"),
        )
        inventory = _inventory(
            ("commandcode", "commandcode", "cmd", False, "commands 'cmd', 'cmdc', 'command-code' not installed or not on PATH"),
            ("codex", "codex", "codex", False, "command 'codex' not installed or not on PATH"),
            ("claude", "claude", "claude", False, "command 'claude' not installed or not on PATH"),
            ("kimi", "kimi-code", "kimi", False, "command 'kimi' not installed or not on PATH"),
            ("antigravity", "antigravity", "agy", False, "unsupported on this platform (linux)"),
            ("grok", "grok", "grok", False, "command 'grok' not installed or not on PATH"),
        )

        with self.assertRaises(SystemExit) as caught:
            fixer_wire_selectors._select_project_hands_lane_interactive(
                lanes,
                "commandcode",
                _Option,
                lambda *_args, **_kwargs: None,
                select_backend_interactive=_backend_picker("commandcode"),
                select_model_interactive=_model_picker(),
                lane_inventory=lambda _lanes: inventory,
            )

        message = str(caught.exception)
        for provider in ("commandcode", "codex", "claude", "kimi", "antigravity", "grok"):
            self.assertIn(f"{provider} [", message)
        self.assertIn("unsupported on this platform (linux)", message)

    def test_registered_lane_filter_is_published_to_an_injected_picker(self) -> None:
        lanes = (_lane("codex"), _lane("kimi"))
        captured: dict[str, object] = {}

        def pick(_preferred: str, _option: object, _chooser: object) -> str:
            captured["active"] = fixer_wire_selectors._HANDS_ACTIVE_ALLOWED_BACKENDS
            return "codex"

        selected = fixer_wire_selectors._select_project_hands_lane_interactive(
            lanes,
            "codex",
            _Option,
            lambda *_args, **_kwargs: None,
            select_backend_interactive=pick,
            select_model_interactive=_model_picker(),
            lane_inventory=lambda _lanes: _inventory(
                ("codex", "codex", "codex", True, ""),
                ("kimi", "kimi-code", "kimi", True, ""),
            ),
        )

        self.assertEqual(captured["active"], {"codex", "kimi-code"})
        self.assertEqual(selected.provider, "codex")
        self.assertIsNone(fixer_wire_selectors._HANDS_ACTIVE_ALLOWED_BACKENDS)

    def test_wire_injected_picker_never_offers_an_unregistered_pi_lane(self) -> None:
        lanes = (_lane("codex"), _lane("kimi"))
        inventory = _inventory(
            ("codex", "codex", "codex", True, ""),
            ("kimi", "kimi-code", "kimi", True, ""),
        )
        offered: list[object] = []

        def choose(options: list[object], **_kwargs: object) -> object:
            offered.extend(option.value for option in options if option.value is not None)
            for option in options:
                if option.value is not None:
                    return option.value
            return None

        with (
            patch.object(fixer_wire_hands_context, "hands_lane_inventory", return_value=inventory),
            patch.object(
                fixer_wire_selectors,
                "_select_model_interactive",
                lambda *_args, **_kwargs: "picked-model",
            ),
        ):
            selected = fixer_wire._select_project_hands_lane_interactive(
                lanes,
                "codex",
                _Option,
                choose,
            )

        self.assertNotIn("pi", offered)
        self.assertEqual(offered, ["codex", "kimi-code"])
        self.assertEqual(selected.provider, "codex")
        self.assertEqual(selected.model, "picked-model")


if __name__ == "__main__":
    unittest.main()
