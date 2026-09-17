from __future__ import annotations

import json
import os
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from client_wires import fixer_wire_launch_support
from client_wires.codex_compat import runtime

LAUNCHER = "/mesh_browser/pw_mesh_mcp.py"


class _DummyOption:
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


def _available_servers(devices_path: Path) -> dict[str, dict[str, object]]:
    return {
        "playwright-mesh": {
            "command": "python3",
            "args": [LAUNCHER, "--target", "macbook-pro", "--viewport-size", "1280,800"],
            "env": {"PW_MESH_DEVICES": str(devices_path)},
            "startup_timeout_sec": 120.0,
        }
    }


DEVICES = {
    "version": 1,
    "devices": {
        "macbook-pro": {"label": "MacBook Pro", "local": True, "browsers": ["chrome"]},
        "ubuntu": {"label": "Ubuntu desktop", "browsers": ["chrome", "chromium"]},
        "asus-laptop": {"label": "ASUS laptop", "browsers": ["firefox"]},
    },
}


class PlaywrightMeshTargetTests(unittest.TestCase):
    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.devices_path = Path(self._tmp.name) / "devices.json"
        self.devices_path.write_text(json.dumps(DEVICES), encoding="utf-8")
        self.servers = _available_servers(self.devices_path)
        self._env = patch.dict(os.environ, {"PW_MESH_DEVICES": str(self.devices_path)}, clear=False)
        self._env.start()

    def tearDown(self) -> None:
        self._env.stop()
        self._tmp.cleanup()

    def _selected(self) -> dict[str, dict[str, object]]:
        return {"playwright-mesh": dict(self.servers["playwright-mesh"])}

    def test_devices_matrix_is_read_from_the_launcher_location(self) -> None:
        env = patch.dict(os.environ, {}, clear=False)
        with env:
            os.environ.pop("PW_MESH_DEVICES", None)
            local_dir = Path(self._tmp.name) / "mesh_browser"
            local_dir.mkdir()
            (local_dir / "devices.json").write_text(json.dumps(DEVICES), encoding="utf-8")
            servers = {
                "playwright-mesh": {
                    "command": "python3",
                    "args": [str(local_dir / "pw_mesh_mcp.py"), "--target", "macbook-pro"],
                }
            }
            devices = runtime.playwright_mesh_devices(servers)
        self.assertEqual(sorted(devices), ["asus-laptop", "macbook-pro", "ubuntu"])

    def test_missing_server_is_a_no_op(self) -> None:
        self.assertIsNone(
            runtime.maybe_configure_playwright_mesh({}, self.servers, interactive=True)
        )

    def test_env_override_pins_device_and_browser_without_prompting(self) -> None:
        selected = self._selected()
        with patch.dict(os.environ, {"PW_MESH_TARGET": "ubuntu", "PW_MESH_BROWSER": "chromium"}):
            with patch.object(runtime, "single_select_items") as prompt:
                result = runtime.maybe_configure_playwright_mesh(
                    selected, self.servers, interactive=False
                )
        prompt.assert_not_called()
        self.assertEqual(result, "ubuntu/chromium")
        args = selected["playwright-mesh"]["args"]
        self.assertIn("--target", args)
        self.assertEqual(args[args.index("--target") + 1], "ubuntu")
        self.assertEqual(args[args.index("--browser") + 1], "chromium")
        self.assertEqual(args[args.index("--viewport-size") + 1], "1280,800")

    def test_interactive_flow_asks_for_device_then_browser(self) -> None:
        selected = self._selected()
        answers = iter(["asus-laptop", "firefox"])
        seen: list[tuple[str, list[str]]] = []

        def fake_single(items: object, **kwargs: object) -> str:
            options = list(items)  # type: ignore[arg-type]
            seen.append(
                (str(kwargs.get("title")), [item.label for item in options if not item.is_header])
            )
            return next(answers)

        with patch.dict(os.environ, {}, clear=False):
            os.environ.pop("PW_MESH_TARGET", None)
            with patch.object(runtime, "single_select_items", fake_single):
                result = runtime.maybe_configure_playwright_mesh(
                    selected, self.servers, interactive=True
                )

        self.assertEqual(result, "asus-laptop/firefox")
        self.assertEqual(len(seen), 2)
        self.assertIn("target device", seen[0][0])
        self.assertIn("browser", seen[1][0])
        # The browser step must be scoped to the chosen device only.
        self.assertEqual(seen[1][1], ["Auto (device default)", "firefox"])

    def test_use_config_default_keeps_the_configured_args(self) -> None:
        selected = self._selected()
        with patch.dict(os.environ, {}, clear=False):
            os.environ.pop("PW_MESH_TARGET", None)
            with patch.object(runtime, "single_select_items", lambda *a, **k: runtime.PLAYWRIGHT_MESH_DEFAULT):
                result = runtime.maybe_configure_playwright_mesh(
                    selected, self.servers, interactive=True
                )
        self.assertIsNone(result)
        self.assertEqual(
            selected["playwright-mesh"]["args"],
            [LAUNCHER, "--target", "macbook-pro", "--viewport-size", "1280,800"],
        )

    def test_launch_support_skips_non_codex_adapters(self) -> None:
        adapter = type("A", (), {"command": "claude"})()
        selected = self._selected()
        result = fixer_wire_launch_support._maybe_configure_playwright_mesh_target(
            adapter, selected, self.servers, interactive=True
        )
        self.assertIsNone(result)


if __name__ == "__main__":
    unittest.main()
