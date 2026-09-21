from __future__ import annotations

import os

import pytest


@pytest.fixture(autouse=True)
def _no_pi_adapter_autoprepare(monkeypatch: pytest.MonkeyPatch) -> None:
    """Keep the test suite offline: never provision the pi MCP adapter from a test.

    ``PiBackendAdapter.ensure_runtime_files`` calls
    ``ensure_mcp_adapter_extension()`` before writing the MCP config, which would
    run ``npm install`` on a host where the extension is absent. Individual tests
    override this variable when they exercise the provisioning path.
    """

    monkeypatch.setenv("FIXER_PI_ADAPTER_AUTOPREPARE", "0")
    os.environ.setdefault("FIXER_PI_ADAPTER_AUTOPREPARE", "0")
