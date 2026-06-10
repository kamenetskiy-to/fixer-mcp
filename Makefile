SHELL := bash
.SHELLFLAGS := -eu -o pipefail -c

PYTHON ?= python3

.PHONY: test-client-wires test-go docker-smoke docker-bootstrap-e2e

test-client-wires:
	$(PYTHON) -m unittest discover -s client_wires/tests

test-go:
	cd fixer_mcp && go build ./... && env -u FIXER_MCP_LOCKED_ROLE go test ./...

docker-smoke:
	bash docker/fixer-smoke.sh

docker-bootstrap-e2e:
	bash docker/fixer-bootstrap-e2e.sh
