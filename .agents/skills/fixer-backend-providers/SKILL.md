---
name: fixer-backend-providers
description: "Use when working on Fixer MCP CLI-backend provider integration, provider-adapter migration, model/reasoning mapping, MCP injection, launch modes, permissions, or provider docs mirrors."
---

# Fixer MCP Backend Provider Work

Use this skill whenever you are asked to work on Fixer MCP CLI-backend
(provider) integration: adding a provider, migrating an existing provider onto
the provider-adapter abstraction, or changing how skills, MCP servers, models,
reasoning, tool registration, launch modes, or permissions map to a provider.

## Canonical references

- `provider_docs_mirror/<provider>/` — local mirror of official provider docs.
- `tmp/provider-research/<provider>.md` — per-provider research notes and seed
  URLs for the mirror.
- `fixer-mcp/provider-adapter-abstraction` — the 6-entity capability contract.
- `fixer-mcp/provider-adapter-abstraction/provider-abstraction-decisions` —
  decisions, the 7th entity (permissions/interactive questions), and the
  mirror-docs mandate.

## Fixer MCP skill catalog

The canonical Fixer MCP skill catalog is `.agents/skills/` in this repository.
It is the only place where managed skill content may be edited. Provider
directories such as `.claude/skills/`, `.factory/skills/`,
`.junie/fixer-runtime/skills/`, and `.kimi-code/skills/` are generated runtime
materializations; never edit them by hand and never treat them as a source.

Every provider adapter must materialize the same managed skill names from that
catalog during its runtime-file preparation:

- Codex and Antigravity: `.agents/skills/`;
- Claude: `.claude/skills/`;
- Droid: `.factory/skills/`;
- Junie: `.junie/fixer-runtime/skills/`;
- Kimi Code: `.kimi-code/skills/`.

When adding or changing a managed skill, edit only the canonical catalog, keep
the name in `FIXER_ROLE_SKILL_NAMES`, and verify a fresh launch for every
provider materializes the same content. Do not recreate either retired
manual-runner skill; use the single `hands-netrunner` skill.

## Required workflow

1. **Refresh the mirror** for the provider you are touching.

   ```bash
   python3 scripts/sync_provider_docs.py --provider <name>
   ```

   Run without `--provider` to refresh all six providers (codex, droid, claude,
   antigravity, junie, kimi-code).

2. **Verify the mirror** exists and is fresh before relying on it.

   ```bash
   python3 -c "import json, datetime, pathlib; p = pathlib.Path('provider_docs_mirror/<name>/index.json'); d = json.loads(p.read_text()); latest = max(v['synced_at'] for v in d.values()); print(latest)"
   ```

   The timestamp should be from the current work session. If it is stale or the
   file is missing, re-run the sync script.

3. **Work from the local mirror.** Read `provider_docs_mirror/<provider>/`
   first when you need authoritative CLI behavior, flags, or config keys.

   Do **not** use ad-hoc web search for provider CLI documentation. If a doc
   page is missing or the mirror is incomplete, add the source URL to
   `tmp/provider-research/<provider>.md` under the official-docs list and
   re-sync.

## Scope discipline

When implementing provider behavior, keep the 7 entity contract in mind:

1. Skills — injection and scoping.
2. MCP servers — injection and scoping.
3. Models — selection, BYOK/subscription, internal ID mapping.
4. Reasoning effort — option set and mapping.
5. Tool registration — native MCP surfacing; no prompt prose.
6. Launch modes — interactive vs headless commands.
7. Permissions & interaction policy — auto-approve flags and operator
   question support.

Make provider-specific changes in the relevant adapter file
(`client_wires/backends/<provider>_adapter.py`) and update the provider
manifest data in `client_wires/backends/data/backend-catalog.json` when
needed.

## Tests

Any change to `scripts/sync_provider_docs.py` or the mirror layout must be
accompanied by a test in `tests/test_sync_provider_docs.py`. Existing tests
cover URL extraction, HTML-to-Markdown conversion, idempotent sync, and the
project skill itself.

Run the test suite before finishing:

```bash
python3 -m pytest tests/test_sync_provider_docs.py -v
```
