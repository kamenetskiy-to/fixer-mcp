# Public Repository Installation

`make install` runs `scripts/install.sh`, the single supported install path. It builds a release from this checkout with `scripts/release/assemble_release.py`, installs it with `scripts/install/install.py`, and writes the `fixer` command shim.

Result of a successful install:

- versioned releases live in `FIXER_MANAGED_ROOT` (default `$HOME/.fixer`) under `releases/<version>`, with `current` as an atomic symlink;
- the command shim is written to `FIXER_USER_BIN` (default `$HOME/.local/bin`), or to `<prefix>/bin` when `FIXER_INSTALL_PREFIX` is set;
- user state and the SQLite database live in `FIXER_STATE_DIR` (`$XDG_STATE_HOME/fixer-client-wires` by default); an explicit `FIXER_DB_PATH` always wins;
- the descriptor and payload the installation came from are kept in `<managed root>/update-source/<version>/`.

Because that release source is kept, the installation can update itself without the checkout:

```bash
fixer update --check                      # compare installed vs recorded release source
fixer update                              # apply the recorded release source
fixer update --descriptor <url-or-path>   # switch to another release source
```

Installation is idempotent and only replaces files it manages. It preserves SQLite databases, project files, configuration, and authentication state, and never edits shell rc files. If you want a narrowly marked PATH block, pass `--add-to-path` to `scripts/install/install.py` directly.

`make install-verify` adds a non-interactive verification pass for the installation: shim version identity, `fixer doctor`, a fresh SQLite initialization through MCP stdio on the installed runtime, and `fixer update --check` against the recorded release source. The pass keeps the checkout clean, so it is safe to run in CI. `make docker-smoke` is the deterministic clean-container gate; the authenticated `make docker-bootstrap-e2e` gate remains optional and manual.
