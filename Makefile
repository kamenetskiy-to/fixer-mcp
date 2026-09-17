SHELL := bash
.SHELLFLAGS := -eu -o pipefail -c

ROOT_DIR := $(abspath $(dir $(lastword $(MAKEFILE_LIST))))
PYTHON ?= python3
DOCKER_IMAGE ?= fixer-mcp-smoke:local
DOCKER_CONTAINER_PREFIX ?= fixer-mcp-smoke
# Optional overrides for the managed install locations. When unset, the
# installer keeps its own defaults ($HOME/.fixer, $HOME/.local/bin,
# $XDG_STATE_HOME/fixer-client-wires). The FIXER_* environment variables
# documented in AGENTS_INSTALLATION.md are honored directly as well.
MANAGED_ROOT ?=
MANAGED_STATE_DIR ?=
USER_BIN_DIR ?=

.PHONY: install install-verify launcher test-client-wires test-go docker-smoke docker-bootstrap-e2e

# Single supported install/update entry point: the managed portable installer
# (installer/, packaging/, bin/fixer, scripts/install/). It builds a release
# from this checkout, installs it into $(MANAGED_ROOT) behind an atomic
# `current` symlink, and keeps the release source it installed from so the
# installation can check for and apply its own updates afterwards.
install:
	@$(if $(MANAGED_ROOT),FIXER_MANAGED_ROOT="$(MANAGED_ROOT)",) \
	 $(if $(MANAGED_STATE_DIR),FIXER_STATE_DIR="$(MANAGED_STATE_DIR)",) \
	 $(if $(USER_BIN_DIR),FIXER_USER_BIN="$(USER_BIN_DIR)",) \
	 bash scripts/install.sh

# Install, then verify the managed installation (doctor, clean MCP stdio
# initialization, and the self-update check against the recorded release
# source).
install-verify:
	@$(if $(MANAGED_ROOT),FIXER_MANAGED_ROOT="$(MANAGED_ROOT)",) \
	 $(if $(MANAGED_STATE_DIR),FIXER_STATE_DIR="$(MANAGED_STATE_DIR)",) \
	 $(if $(USER_BIN_DIR),FIXER_USER_BIN="$(USER_BIN_DIR)",) \
	 bash scripts/install.sh --verify

# Run the launcher and wires straight from the checkout without installing.
launcher:
	$(PYTHON) bin/fixer

test-client-wires:
	$(PYTHON) -m unittest discover -s client_wires/tests

test-go:
	cd fixer_mcp && go build ./... && env -u FIXER_DB_PATH -u FIXER_MCP_LOCKED_ROLE -u FIXER_MCP_DEFAULT_ROLE -u FIXER_MCP_DEFAULT_CWD -u FIXER_MCP_AUTO_AUTH -u FIXER_MCP_TOOL_PROFILE go test ./...

docker-smoke:
	docker build -f "$(ROOT_DIR)/docker/fixer-smoke.Dockerfile" -t "$(DOCKER_IMAGE)" "$(ROOT_DIR)"
	docker run --rm --name "$(DOCKER_CONTAINER_PREFIX)-$$(date +%s)" "$(DOCKER_IMAGE)"

docker-bootstrap-e2e:
	bash docker/fixer-bootstrap-e2e.sh
