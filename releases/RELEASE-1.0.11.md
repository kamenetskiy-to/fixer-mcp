# Fixer 1.0.11

Full declared-write-scope retirement: no active scope fields/CLI fences/path leases. Safe idempotent SQLite migration archives legacy metadata while preserving immutable audit, IDs, role/project isolation and one active Hands generation. **Back up the database and finish old-schema workers before first startup. Already-open MCP processes must restart. Do not roll back the database without its backup.**

Agy Claude Opus 5.5 and Sonnet 5.5 low/medium/high routes and current menus replace retired 4.6 Thinking entries; both routes were exercised by real governed MCP Netrunners. System1/Jev remains unchanged.

Independent integrated verification: Go build/vet/full test and all control-plane packages; Python494 passed,1 skipped,53 subtests; public491 passed,4 skipped,53 subtests; release/installer/install/generator114 passed+48 subtests; Flutter110 passed,Serverpod35 passed,terminal_provider28 passed,terminal_control_client8 passed. Flutter analyzer retains four pre-existing informational findings; terminal analyzers clean. Real-old-DB backup migrated twice: integrity ok, no new FK violations, immutable hashes preserved (37 old violations,4 on retired lease table).

Four checksummed payloads: macOS arm64/amd64 and Linux arm64/amd64. Publication is not remote fleet installation. Full technical evidence: [acceptance](../docs/validation/scopes-and-agy-5.5-acceptance-2026-10-04.md).

1.0.10 main/tag CI exposed an obsolete declared_write_scope argument in the managed-install smoke. This release fixes the test and adds positive schema/session key-absence assertions; the strict retired API was not weakened. The old tag and failed CI remain in history. Fresh packaged stdio smoke independently passed.
