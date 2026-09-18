# Fixer MCP release stream

Each release is a format=1 descriptor plus its payload archive. Point an
installation at a descriptor to install or update:

```sh
fixer update --check --descriptor <descriptor-url-or-path>
fixer update         --descriptor <descriptor-url-or-path>
```

Canonical descriptors (replace the platform as needed):

- linux_amd64: `https://raw.githubusercontent.com/kamenetskiy-to/fixer-mcp/main/releases/fixer-mcp-0.3.1-linux_amd64.json`
- darwin_arm64 (Apple silicon): `.../releases/fixer-mcp-0.3.1-darwin_arm64.json`
- darwin_amd64 (Intel): `.../releases/fixer-mcp-0.3.1-darwin_amd64.json`

A relative `payload_url` inside a descriptor resolves against the descriptor's
own location, so descriptor and payload must travel together.

Release 0.3.0 (2026-09-17): pi as a first-class Hands lane, platform-aware
release pipeline (linux + darwin), fleet installs on Ubuntu and
macbook-air-lizok, single-database-host stdio-over-SSH mode.
