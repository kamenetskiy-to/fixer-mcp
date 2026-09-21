from __future__ import annotations

import os
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from client_wires import launch_env


def _write_marker(home: Path, body: str) -> Path:
    """Write the default marker at ``$HOME/.config/fleet/vpn.env``."""

    marker_dir = home / ".config" / "fleet"
    marker_dir.mkdir(parents=True, exist_ok=True)
    marker = marker_dir / "vpn.env"
    marker.write_text(body, encoding="utf-8")
    return marker


class ClearProxyDefaultTests(unittest.TestCase):
    """No opt-in must keep the original clear-only behaviour."""

    def test_default_clears_all_proxy_names_and_returns_same_mapping(self) -> None:
        env = {
            "ALL_PROXY": "socks5://10.0.0.1:1080",
            "all_proxy": "socks5://10.0.0.1:1080",
            "HTTP_PROXY": "http://proxy.example:8080",
            "http_proxy": "http://proxy.example:8080",
            "HTTPS_PROXY": "http://proxy.example:8080",
            "https_proxy": "http://proxy.example:8080",
            "NO_PROXY": "127.0.0.1,localhost",
            "no_proxy": "127.0.0.1,localhost",
            "PATH": "/usr/bin",
        }
        with tempfile.TemporaryDirectory() as tmp:
            with patch.dict(os.environ, {"HOME": tmp}, clear=True):
                result = launch_env.clear_proxy_env(env)

        self.assertIs(result, env)
        for name in launch_env.PROXY_ENV_NAMES:
            self.assertNotIn(name, env)
        self.assertEqual(env["PATH"], "/usr/bin")

    def test_default_clears_even_with_proxy_vars_in_os_environ(self) -> None:
        env = {"HTTPS_PROXY": "http://proxy.example:8080", "PATH": "/usr/bin"}
        with tempfile.TemporaryDirectory() as tmp:
            with patch.dict(
                os.environ,
                {"HOME": tmp, "HTTPS_PROXY": "http://proxy.example:8080"},
                clear=True,
            ):
                launch_env.clear_proxy_env(env)
        self.assertNotIn("HTTPS_PROXY", env)

    def test_falsey_keep_switch_is_a_kill_switch_even_with_marker(self) -> None:
        env = {"HTTPS_PROXY": "http://127.0.0.1:8888", "PATH": "/usr/bin"}
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            _write_marker(root, "export HTTPS_PROXY=http://127.0.0.1:8888\n")
            with patch.dict(
                os.environ,
                {"HOME": tmp, "FLEET_KEEP_PROXY": "0"},
                clear=True,
            ):
                launch_env.clear_proxy_env(env)
        self.assertNotIn("HTTPS_PROXY", env)


class KeepProxyOptInTests(unittest.TestCase):
    def test_env_opt_in_keeps_non_loopback_proxies(self) -> None:
        env = {
            "HTTPS_PROXY": "http://proxy.example:8080",
            "HTTP_PROXY": "http://proxy.example:8080",
            "ALL_PROXY": "socks5://proxy.example:1080",
            "PATH": "/usr/bin",
        }
        with tempfile.TemporaryDirectory() as tmp:
            with patch.dict(
                os.environ,
                {"HOME": tmp, "FLEET_KEEP_PROXY": "1"},
                clear=True,
            ):
                launch_env.clear_proxy_env(env)

        self.assertEqual(env["HTTPS_PROXY"], "http://proxy.example:8080")
        self.assertEqual(env["HTTP_PROXY"], "http://proxy.example:8080")
        self.assertEqual(env["ALL_PROXY"], "socks5://proxy.example:1080")

    def test_vpn_marker_presence_opts_in_without_keep_switch(self) -> None:
        env = {"HTTPS_PROXY": "http://proxy.example:8080"}
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            _write_marker(root, "export HTTPS_PROXY=http://proxy.example:8080\n")
            with patch.dict(os.environ, {"HOME": tmp}, clear=True):
                launch_env.clear_proxy_env(env)
        self.assertEqual(env["HTTPS_PROXY"], "http://proxy.example:8080")

    def test_marker_values_override_stale_child_values(self) -> None:
        # ~/.codex/llm.env can carry a dead Happ socks port while the live
        # tunnel in the VPN marker listens elsewhere: the marker wins.
        env = {"HTTPS_PROXY": "socks5://127.0.0.1:10808"}
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            _write_marker(
                root,
                "export HTTPS_PROXY=http://127.0.0.1:8888\n"
                "export ALL_PROXY=socks5h://127.0.0.1:1080\n",
            )
            with patch.dict(os.environ, {"HOME": tmp}, clear=True):
                with patch.object(launch_env, "_probe_tcp", return_value=True):
                    launch_env.clear_proxy_env(env)

        self.assertEqual(env["HTTPS_PROXY"], "http://127.0.0.1:8888")
        self.assertEqual(env["ALL_PROXY"], "socks5h://127.0.0.1:1080")

    def test_keep_switch_is_honoured_from_child_env(self) -> None:
        env = {
            "FLEET_KEEP_PROXY": "yes",
            "HTTPS_PROXY": "http://proxy.example:8080",
        }
        with tempfile.TemporaryDirectory() as tmp:
            with patch.dict(os.environ, {"HOME": tmp}, clear=True):
                launch_env.clear_proxy_env(env)
        self.assertEqual(env["HTTPS_PROXY"], "http://proxy.example:8080")


class LoopbackReachabilityTests(unittest.TestCase):
    def test_unreachable_loopback_proxy_is_dropped(self) -> None:
        env = {
            "HTTPS_PROXY": "http://127.0.0.1:10808",
            "HTTP_PROXY": "http://127.0.0.1:8888",
            "np_ran": "1",
        }
        with tempfile.TemporaryDirectory() as tmp:
            with patch.dict(
                os.environ,
                {"HOME": tmp, "FLEET_KEEP_PROXY": "1"},
                clear=True,
            ):
                with patch.object(
                    launch_env,
                    "_probe_tcp",
                    side_effect=lambda host, port, timeout: port == 8888,
                ):
                    launch_env.clear_proxy_env(env)

        self.assertNotIn("HTTPS_PROXY", env)
        self.assertEqual(env["HTTP_PROXY"], "http://127.0.0.1:8888")

    def test_reachable_loopback_proxy_is_kept(self) -> None:
        env = {"HTTPS_PROXY": "http://127.0.0.1:8888"}
        with tempfile.TemporaryDirectory() as tmp:
            with patch.dict(
                os.environ,
                {"HOME": tmp, "FLEET_KEEP_PROXY": "1"},
                clear=True,
            ):
                with patch.object(launch_env, "_probe_tcp", return_value=True):
                    launch_env.clear_proxy_env(env)
        self.assertEqual(env["HTTPS_PROXY"], "http://127.0.0.1:8888")

    def test_kill_when_no_marker_and_switch_unset(self) -> None:
        env = {"HTTPS_PROXY": "http://127.0.0.1:8888"}
        with tempfile.TemporaryDirectory() as tmp:
            with patch.dict(os.environ, {"HOME": tmp}, clear=True):
                launch_env.clear_proxy_env(env)
        self.assertNotIn("HTTPS_PROXY", env)

    def test_non_loopback_proxy_is_never_probed(self) -> None:
        with patch.object(
            launch_env,
            "_probe_tcp",
            side_effect=AssertionError("probe must not run for non-loopback"),
        ):
            self.assertFalse(launch_env.loopback_proxy_unreachable("http://proxy.example:8080"))

    def test_parse_proxy_endpoint_handles_schemes_and_defaults(self) -> None:
        self.assertEqual(
            launch_env.parse_proxy_endpoint("http://user:pass@127.0.0.1:8888"),
            ("127.0.0.1", 8888, "http"),
        )
        self.assertEqual(
            launch_env.parse_proxy_endpoint("socks5h://127.0.0.1:1080"),
            ("127.0.0.1", 1080, "socks5h"),
        )
        self.assertEqual(
            launch_env.parse_proxy_endpoint("127.0.0.1:3128"),
            ("127.0.0.1", 3128, "http"),
        )
        self.assertEqual(
            launch_env.parse_proxy_endpoint("http://127.0.0.1"),
            ("127.0.0.1", 80, "http"),
        )
        self.assertIsNone(launch_env.parse_proxy_endpoint(""))
        self.assertIsNone(launch_env.parse_proxy_endpoint("   "))


class NoProxySanitationTests(unittest.TestCase):
    def test_sanitize_keeps_operator_local_and_drops_public_entries(self) -> None:
        value = (
            "127.0.0.1,localhost,::1,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,"
            "100.64.0.0/10,.ts.net,*.ts.net,*.openai.com,api.anthropic.com,"
            "0.0.0.0/0,*,fixer-db"
        )
        sanitized = launch_env.sanitize_no_proxy(value)
        entries = sanitized.split(",")
        for expected in (
            "127.0.0.1",
            "localhost",
            "::1",
            "10.0.0.0/8",
            "172.16.0.0/12",
            "192.168.0.0/16",
            "100.64.0.0/10",
            ".ts.net",
            "*.ts.net",
            "fixer-db",
        ):
            self.assertIn(expected, entries)
        for rejected in ("*.openai.com", "api.anthropic.com", "0.0.0.0/0", "*"):
            self.assertNotIn(rejected, entries)

    def test_sanitize_drops_cidrs_that_reach_public_space(self) -> None:
        # Wider than RFC1918, so it would bypass the tunnel for public hosts.
        self.assertEqual(launch_env.sanitize_no_proxy("10.0.0.0/7"), "")
        self.assertEqual(launch_env.sanitize_no_proxy("172.0.0.0/8"), "")
        self.assertEqual(launch_env.sanitize_no_proxy("192.168.0.0/15"), "")

    def test_sanitize_drops_everything_when_only_public(self) -> None:
        self.assertEqual(launch_env.sanitize_no_proxy("*,api.openai.com"), "")

    def test_no_proxy_entry_allowed_cases(self) -> None:
        self.assertTrue(launch_env.no_proxy_entry_allowed("127.0.0.1:8888"))
        self.assertTrue(launch_env.no_proxy_entry_allowed("10.1.2.3"))
        self.assertTrue(launch_env.no_proxy_entry_allowed("100.64.5.5"))
        self.assertTrue(launch_env.no_proxy_entry_allowed("fd00::1"))
        self.assertTrue(launch_env.no_proxy_entry_allowed("build.local"))
        self.assertFalse(launch_env.no_proxy_entry_allowed("0.0.0.0/0"))
        self.assertFalse(launch_env.no_proxy_entry_allowed("example.com"))
        self.assertFalse(launch_env.no_proxy_entry_allowed(""))

    def test_kept_env_drops_bypassing_no_proxy_entries(self) -> None:
        env = {
            "HTTPS_PROXY": "http://proxy.example:8080",
            "NO_PROXY": "127.0.0.1,localhost,*.openai.com,api.anthropic.com",
            "no_proxy": "*,10.0.0.0/8",
        }
        with tempfile.TemporaryDirectory() as tmp:
            with patch.dict(
                os.environ,
                {"HOME": tmp, "FLEET_KEEP_PROXY": "1"},
                clear=True,
            ):
                launch_env.clear_proxy_env(env)

        self.assertEqual(env["NO_PROXY"], "127.0.0.1,localhost")
        self.assertEqual(env["no_proxy"], "10.0.0.0/8")


class IdempotenceTests(unittest.TestCase):
    def test_clear_proxy_env_is_idempotent_on_already_clean_env(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            with patch.dict(
                os.environ,
                {"HOME": tmp, "FLEET_KEEP_PROXY": "1"},
                clear=True,
            ):
                clean: dict[str, str] = {"PATH": "/usr/bin"}
                first = launch_env.clear_proxy_env(clean)
                snapshot = dict(first)
                second = launch_env.clear_proxy_env(clean)

        self.assertEqual(second, snapshot)
        self.assertEqual(second, {"PATH": "/usr/bin"})

    def test_keep_path_is_idempotent(self) -> None:
        env = {
            "HTTPS_PROXY": "http://proxy.example:8080",
            "NO_PROXY": "127.0.0.1,*.openai.com",
        }
        with tempfile.TemporaryDirectory() as tmp:
            with patch.dict(
                os.environ,
                {"HOME": tmp, "FLEET_KEEP_PROXY": "1"},
                clear=True,
            ):
                launch_env.clear_proxy_env(env)
                first = dict(env)
                launch_env.clear_proxy_env(env)

        self.assertEqual(env, first)
        self.assertEqual(env["NO_PROXY"], "127.0.0.1")


class VpnMarkerPathTests(unittest.TestCase):
    def test_xdg_config_home_wins_over_home(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            xdg = Path(tmp) / "xdg"
            home = Path(tmp) / "home"
            env = {"XDG_CONFIG_HOME": str(xdg), "HOME": str(home)}
            self.assertEqual(launch_env.vpn_env_path(env), xdg / "fleet" / "vpn.env")

    def test_explicit_marker_override_is_used(self) -> None:
        override = "/custom/path/vpn.env"
        env = {"FLEET_VPN_ENV_FILE": override}
        self.assertEqual(launch_env.vpn_env_path(env), Path(override))

    def test_load_vpn_env_proxies_reads_export_lines(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            _write_marker(
                root,
                "# comment\n"
                "export HTTPS_PROXY='http://127.0.0.1:8888'\n"
                "HTTP_PROXY=http://127.0.0.1:8888\n"
                "export UNRELATED=value\n",
            )
            with patch.dict(os.environ, {"HOME": tmp}, clear=True):
                values = launch_env.load_vpn_env_proxies()
        self.assertEqual(
            values,
            {
                "HTTPS_PROXY": "http://127.0.0.1:8888",
                "HTTP_PROXY": "http://127.0.0.1:8888",
            },
        )


if __name__ == "__main__":
    unittest.main()
