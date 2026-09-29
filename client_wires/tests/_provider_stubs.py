"""Hermetic provider-CLI stubs for launcher and resume tests.

CI runners (and any host without the provider CLIs) make launcher tests die
in _require_provider_executable before they reach the command construction
under test. These stubs place no-op executables for every registered backend
candidate on PATH so the tests validate wiring, not the host's tool chest.
"""

from __future__ import annotations

import os
import stat
import tempfile
from collections.abc import Iterator
from contextlib import contextmanager

from client_wires.fixer_wire_hands_context import HANDS_BACKEND_EXECUTABLES

STUB_NAMES = sorted({name for names in HANDS_BACKEND_EXECUTABLES.values() for name in names})


@contextmanager
def provider_stub_path() -> Iterator[str]:
    with tempfile.TemporaryDirectory() as tmp:
        for name in STUB_NAMES:
            stub = os.path.join(tmp, name)
            with open(stub, "w", encoding="utf-8") as handle:
                handle.write("#!/bin/sh\nexit 0\n")
            os.chmod(stub, stat.S_IEXEC | stat.S_IRUSR | stat.S_IWUSR | stat.S_IXGRP | stat.S_IXOTH)
        previous = os.environ.get("PATH", "")
        os.environ["PATH"] = tmp + os.pathsep + previous
        try:
            yield tmp
        finally:
            os.environ["PATH"] = previous
