# Docker Smoke And Bootstrap E2E

The repository has two Docker validation gates with different cost and stability
profiles.

## Clean Smoke

```bash
make docker-smoke
```

This is the required deterministic gate. It builds `fixer_mcp`, runs Go and focused
Python/Node checks, and verifies a clean MCP stdio flow without real LLM calls.

Optional auth-mounted smoke:

```bash
make docker-smoke-auth
```

That only verifies that `/root/.codex/auth.json` can be mounted read-only. It still
does not run a real Codex Fixer or Netrunner.

## Real Bootstrap E2E

```bash
make docker-bootstrap-e2e
```

This is an optional expensive gate for pre-handoff validation. It:

- builds a clean Ubuntu image;
- installs Go, Python, Node/npm, and Codex CLI;
- mounts `~/.codex/auth.json` read-only at runtime;
- uses the repo-vendored `client_wires.codex_compat` package;
- creates a fresh SQLite DB and toy project inside the container;
- launches a real Codex-backed Fixer with only forced `fixer_mcp` mounted;
- verifies MCP marketplace metadata visibility;
- asks the Fixer to launch one real Codex-backed Netrunner through
  `launch_and_wait_netrunner`;
- writes evidence under `.run/docker-bootstrap-e2e/<timestamp>/`.

Do not fold this into `make docker-smoke`: it depends on network, Codex auth, model
availability, and LLM behavior.

Useful overrides:

```bash
DOCKER_BOOTSTRAP_E2E_AUTH_PATH=/path/to/auth.json make docker-bootstrap-e2e
BOOTSTRAP_E2E_MODEL=gpt-5.5 BOOTSTRAP_E2E_REASONING=high make docker-bootstrap-e2e
BOOTSTRAP_E2E_TIMEOUT_SECONDS=5400 make docker-bootstrap-e2e
```

The auth file is never copied into an image layer or build context. The host wrapper
mounts it directly to `/root/.codex/auth.json` with Docker's `readonly` flag.

## Launcher Prerequisite

There is no external `codex_pro_app` prerequisite. The launcher imports the
repo-native `client_wires.codex_compat` package in both local and Docker runs.
