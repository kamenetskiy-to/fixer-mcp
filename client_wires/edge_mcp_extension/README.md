# Edge MCP — All Tabs

This unpacked Microsoft Edge extension implements Playwright's extension relay
protocol v2 while automatically exposing every ordinary tab in the current
Edge profile. It does not create or use a separate automation profile, group
tabs, or change normal browsing data.

Load this directory once from `edge://extensions` with Developer mode enabled.
Its deterministic extension ID is `ffmbpfogmjdlbhnepmmhgahagoemdjkm`.

The extension connects only when the matching local `edge` MCP opens its
`connect.html` page with a loopback WebSocket relay URL. While connected, the
agent has debugger-level read/write access to all ordinary tabs. When the MCP
disconnects, the extension detaches every debugger session it created.

Before exposing a tab to Playwright, the Codex launcher probes its renderer with
`Page.getFrameTree`. A renderer that does not answer within three seconds is
detached and skipped for that MCP connection so one broken or wedged tab cannot
block access to every other Edge window. Override the probe deadline with
`CODEX_PRO_EDGE_TAB_PROBE_TIMEOUT_MS` when diagnosing unusually slow pages.
New inactive tabs get a separate ten-second wake-up allowance, configurable via
`CODEX_PRO_EDGE_NEW_TAB_PROBE_TIMEOUT_MS`.

The launcher also keeps Edge in the background by default. It opens the relay
page with macOS `open -g`, creates agent-owned tabs as inactive tabs, and
acknowledges `Page.bringToFront` without forwarding it to Edge. Set
`CODEX_PRO_EDGE_ALLOW_FOREGROUND=1` only when interactive debugging should be
allowed to activate the Edge application.
Because Chromium input dispatch can still activate the macOS application, each
MCP tool call runs a 100ms focus guard. It tracks the latest user-selected app
and immediately restores that app whenever Edge takes focus, then performs a
final conditional restore after the action.
