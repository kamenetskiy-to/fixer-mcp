# Agent Installation Contract

Use this document as an execution checklist when installing Fixer MCP for another operator. Do not copy credentials into the repository or logs.

## Prerequisites

- Ubuntu: install Git, Bash, Python 3.12+, Go 1.25.4+, Node.js/npm, and Docker Engine for container smoke tests.
- macOS: install Xcode command-line tools, Python 3.12+, Go 1.25.4+, Node.js/npm, and Docker Desktop for container smoke tests.
- Go is required for installation: the managed installer builds the MCP server from this checkout.
- Install the Codex CLI and authenticate interactively with `codex login` before authenticated worker launches. Deterministic verification does not require Codex authentication.

## Clone, install, verify

```bash
git clone git@github.com:kamenetskiy-to/fixer-mcp.git fixer-mcp
# HTTPS alternative: git clone https://github.com/kamenetskiy-to/fixer-mcp.git fixer-mcp
cd fixer-mcp
make install-verify
```

`make install-verify` assembles a release from this checkout, installs it into the managed root, writes the `fixer` command shim, and then verifies the result: shim version identity, `fixer doctor`, a clean MCP stdio initialization on a fresh database, and `fixer update --check` against the recorded release source.

Defaults, all overridable:

| Variable | Default | Meaning |
| --- | --- | --- |
| `FIXER_MANAGED_ROOT` | `$HOME/.fixer` | versioned releases behind an atomic `current` symlink |
| `FIXER_STATE_DIR` | `$XDG_STATE_HOME/fixer-client-wires` | user state and database directory; an explicit `FIXER_DB_PATH` always wins |
| `FIXER_USER_BIN` | `$HOME/.local/bin` | directory for the `fixer` command shim |
| `FIXER_INSTALL_PREFIX` | unset | prefix shorthand: installs the shim into `<prefix>/bin` |
| `RELEASE_VERSION` | `0.1.0` | version stamped into the release built and installed |

Installation is safe to rerun and idempotent: it replaces only the releases and shim it manages, keeps SQLite state, project files, and credentials untouched, and never edits shell startup files. If needed, add the shim directory to `PATH` yourself. For a non-default location use `FIXER_MANAGED_ROOT=/path/to/root FIXER_USER_BIN=/path/to/bin make install-verify`.

## First project

From the new project's root, run:

```bash
fixer --role fixer
```

Normal launcher startup registers an unknown project CWD automatically. Do not perform manual Overseer registration for ordinary first use. For Codex-backed work, complete `codex login` first.

## Update or reinstall

The installation keeps the release descriptor and payload it came from, so it can update itself:

```bash
fixer update --check     # compare the installed version with the recorded release source
fixer update             # re-apply / move to the recorded release source
fixer update --descriptor <url-or-path>   # switch to another release source
```

To rebuild from a newer checkout instead:

```bash
git pull --ff-only
make install-verify
```

## Troubleshooting

- `fixer: command not found`: add the shim directory (`$HOME/.local/bin` by default) to `PATH`, or invoke the shim by absolute path.
- Go/Python version errors: install the prerequisite versions, then rerun; installation builds the Go server from source.
- `Missing fixer command shim` during verification: install first (`make install`) or point `FIXER_USER_BIN` at the directory that holds the shim.
- Docker smoke failures: ensure the daemon is running; `make install-verify` itself is non-interactive and does not require Docker.
- Authenticated launch failures: run `codex login`; never place auth data in this checkout.
- Override the state location with `FIXER_STATE_DIR` or an explicit `FIXER_DB_PATH`; existing state is never overwritten by installation.

## Success criteria

Installation is complete only when `make install-verify` passes: the shim reports a managed version for the checkout, `fixer doctor` is healthy, the installed runtime initializes a fresh SQLite database through MCP stdio, and `fixer update --check` resolves the recorded release source without error. `make docker-smoke` is the deterministic clean-container release gate; `make docker-bootstrap-e2e` is optional, authenticated, network-dependent, and must be run manually by an operator with local Codex auth.
