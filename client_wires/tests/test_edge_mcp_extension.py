from __future__ import annotations

import base64
import hashlib
import json
from pathlib import Path
import unittest


EXTENSION_DIR = Path(__file__).parents[1] / "edge_mcp_extension"
LAUNCHER = Path(__file__).parents[1] / "codex_compat" / "playwright_edge_extension_mcp.cjs"
PACKAGED_LAUNCHER = (
    Path.home() / "Desktop/projects/mcp_servers/codex_pro_app/playwright_edge_extension_mcp.cjs"
)
BROKER = Path(__file__).parents[1] / "codex_compat" / "edge_mcp_broker.cjs"
PACKAGED_BROKER = Path.home() / "Desktop/projects/mcp_servers/codex_pro_app/edge_mcp_broker.cjs"
EXPECTED_EXTENSION_ID = "ffmbpfogmjdlbhnepmmhgahagoemdjkm"


def _extension_id_from_key(key: str) -> str:
    digest = hashlib.sha256(base64.b64decode(key)).digest()[:16]
    return "".join(chr(ord("a") + nibble) for byte in digest for nibble in (byte >> 4, byte & 0xF))


class EdgeMCPAllTabsExtensionTests(unittest.TestCase):
    def test_manifest_has_deterministic_id_and_minimal_required_permissions(self) -> None:
        manifest = json.loads((EXTENSION_DIR / "manifest.json").read_text(encoding="utf-8"))

        self.assertEqual(_extension_id_from_key(manifest["key"]), EXPECTED_EXTENSION_ID)
        self.assertEqual(set(manifest["permissions"]), {"alarms", "debugger", "tabs"})
        self.assertEqual(manifest["host_permissions"], ["<all_urls>"])
        self.assertEqual(manifest["background"], {"service_worker": "background.js"})

    def test_background_advertises_existing_and_future_tabs_without_grouping(self) -> None:
        source = (EXTENSION_DIR / "background.js").read_text(encoding="utf-8")

        self.assertIn("await chrome.tabs.query({})", source)
        self.assertIn("chrome.tabs.onCreated.addListener", source)
        self.assertIn("chrome.tabs.onUpdated.addListener", source)
        self.assertIn("chrome.tabs.onRemoved.addListener", source)
        self.assertIn("method: 'extension.initialized'", source)
        self.assertIn("chrome.debugger.detach", source)
        self.assertIn("this._createCommandDepth++", source)
        self.assertIn("this._pendingCreatedTabs.delete(response.result.id)", source)
        self.assertNotIn("chrome.tabGroups", source)
        self.assertNotIn("user-data-dir", source)
        self.assertIn("http://127.0.0.1:55712", source)
        self.assertIn("/poll?cursor=", source)
        self.assertIn("await replaceActiveConnection(relayUrl)", source)
        self.assertIn("`${BROKER_ORIGIN}/ack`", source)

    def test_connect_page_accepts_only_loopback_protocol_v2_relays(self) -> None:
        source = (EXTENSION_DIR / "connect.js").read_text(encoding="utf-8")

        self.assertIn("url.hostname === '127.0.0.1'", source)
        self.assertIn("url.hostname === 'localhost'", source)
        self.assertIn("protocolVersion !== 2", source)
        self.assertIn("type: 'connectAllTabs'", source)

    @unittest.skip("public export excludes private mcp_servers directory")
    def test_packaged_launcher_matches_source_and_targets_custom_extension(self) -> None:
        self.assertEqual(LAUNCHER.read_bytes(), PACKAGED_LAUNCHER.read_bytes())
        source = LAUNCHER.read_text(encoding="utf-8")

        self.assertIn(EXPECTED_EXTENSION_ID, source)
        self.assertIn("@playwright/mcp@latest", source)
        self.assertIn("CODEX_PRO_PLAYWRIGHT_MCP_CLI", source)
        self.assertIn("path.join(os.homedir(), '.npm', '_npx')", source)
        self.assertIn("source: 'npx cache'", source)
        self.assertIn("--extension", source)
        self.assertIn("Microsoft Edge.app", source)
        self.assertNotIn("--user-data-dir", source)

    def test_launcher_skips_renderer_tabs_that_would_block_all_other_tabs(self) -> None:
        source = LAUNCHER.read_text(encoding="utf-8")

        self.assertIn("CODEX_PRO_EDGE_TAB_PROBE_TIMEOUT_MS", source)
        self.assertIn("CODEX_PRO_EDGE_NEW_TAB_PROBE_TIMEOUT_MS", source)
        self.assertIn("CODEX_PRO_EDGE_TAB_HEADER_TIMEOUT_MS", source)
        self.assertIn("CODEX_PRO_EDGE_SNAPSHOT_TIMEOUT_MS", source)
        self.assertIn('"Page.getFrameTree"', source)
        self.assertIn('"chrome.debugger.detach"', source)
        self.assertIn("this._knownTabs.delete(tabId)", source)
        self.assertIn("skipping unresponsive tabId=", source)
        self.assertIn("__codexProEdgeTabHeaderSnapshot", source)
        self.assertIn("using header fallback index=", source)
        self.assertIn("'[unresponsive tab]'", source)
        self.assertIn('this._includeSnapshot !== "none"', source)
        self.assertIn('this._includeSnapshot === "explicit"', source)
        self.assertIn("omitting post-tool snapshot error=", source)
        self.assertIn("timeout: globalThis.__codexProEdgeSnapshotTimeoutMs", source)
        self.assertIn("forwardedArgs.includes('--timeout-action')", source)
        self.assertIn("forwardedArgs.push('--timeout-action', '10000')", source)
        self.assertIn("forwardedArgs.includes('--snapshot-mode')", source)
        self.assertIn("forwardedArgs.push('--snapshot-mode', 'none')", source)

    def test_launcher_keeps_edge_in_the_background_by_default(self) -> None:
        source = LAUNCHER.read_text(encoding="utf-8")

        self.assertIn("CODEX_PRO_EDGE_ALLOW_FOREGROUND", source)
        self.assertIn('launchThroughBroker ? "/usr/bin/curl"', source)
        self.assertIn('"http://127.0.0.1:55712/connect"', source)
        self.assertNotIn('["-g", "-a", "Microsoft Edge", href]', source)
        self.assertNotIn('tell front window to make new tab', source)
        self.assertIn("active: !globalThis.__codexProEdgeBackgroundMode", source)
        self.assertIn('case "Page.bringToFront": {', source)
        self.assertIn("suppressed Page.bringToFront", source)
        self.assertIn("__codexProEdgeCaptureFrontmostApp", source)
        self.assertIn("__codexProEdgeRestoreFrontmostApp", source)
        self.assertIn("com.microsoft.edgemac", source)
        self.assertIn("__codexProEdgeStartFocusGuard", source)
        self.assertIn("__codexProEdgeStopFocusGuard", source)
        self.assertNotIn("delay 0.1", source)

    @unittest.skip("public export excludes private mcp_servers directory")
    def test_loopback_broker_matches_packaged_copy_and_validates_relays(self) -> None:
        self.assertEqual(BROKER.read_bytes(), PACKAGED_BROKER.read_bytes())
        source = BROKER.read_text(encoding="utf-8")

        self.assertIn("server.listen(PORT, HOST)", source)
        self.assertIn("parsed.pathname.startsWith('/extension/')", source)
        self.assertIn("url.pathname === '/poll'", source)
        self.assertIn("url.pathname === '/connect'", source)
        self.assertIn("url.pathname === '/ack'", source)

    def test_launcher_enforces_one_shared_edge_relay_owner(self) -> None:
        source = LAUNCHER.read_text(encoding="utf-8")

        self.assertIn("codex-pro-edge-mcp.lock", source)
        self.assertIn("fs.openSync(INSTANCE_LOCK_PATH, 'wx', 0o600)", source)
        self.assertIn("singleton takeover previous_pid=", source)
        self.assertIn("process.kill(existingPid, 'SIGTERM')", source)
        self.assertIn("process.kill(existingPid, 'SIGKILL')", source)
        self.assertIn("record?.token === INSTANCE_LOCK_TOKEN", source)
        self.assertIn("process.once('SIGTERM'", source)


if __name__ == "__main__":
    unittest.main()
