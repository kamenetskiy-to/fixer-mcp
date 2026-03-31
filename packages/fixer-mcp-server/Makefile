GO ?= go
BINARY ?= fixer_mcp

.PHONY: build test run fmt

build:
	$(GO) build -o $(BINARY) .

test:
	$(GO) test ./...

run: build
	./$(BINARY)

fmt:
	$(GO) fmt ./...
