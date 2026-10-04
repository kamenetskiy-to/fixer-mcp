# Declared-scope retirement and Agy Claude 5.5 acceptance

## Implemented contract

Declared write scopes are removed, not optional or ignored compatibility parameters. Current task/session/fork/planned-wave/worker/Hands DTOs, MCP schemas, CLI options, prompts, Flutter/Serverpod wire models, terminal control client and UI no longer contain the field. Scope normalization, changed-path fences, foundation-path allowlists and overlap/write-lease admission are deleted. Ordinary task ownership and DAG dependencies are review/ordering context, not a renamed path fence.

Project/role isolation, clean Git admission, one active Hands generation per project, process-start identity, binary epoch, durable audit and explicit review remain. A busy Hands project keeps work queued; it does not emit new lease-wait states. New runnable Hands instructions use repository-write risk and required Fixer review; an absent file list cannot auto-accept a report.

## Storage upgrade

Fresh databases omit retired columns and the three path-lease tables. Existing values are archived in `retired_write_scope_archive`; archive-and-remove steps commit transactionally and are restart-idempotent. Immutable instruction envelopes, instruction events, reports, System1 rows and historical ordinals are not rewritten to hide old fields.

The Hands rebuild handles partial legacy lease-column combinations, preserves named indexes/triggers, restores the original FK enforcement setting even on error, and rejects new FK violations relative to the pre-rebuild baseline. The earlier Grok-lane table rebuild is also atomic and preserves the provisioning trigger.

Independent validation against a SQLite backup of the real pre-upgrade database: two candidate initializations succeeded; active scope columns/lease tables absent; row counts and immutable history hashes unchanged; `integrity_check=ok`; no new FK violations. There were **37 pre-existing FK violations**, four attached to the retired lease table; removal leaves 33 historical violations. This is not claimed as a clean-FK database repair. Production activation requires an independent SQLite backup and no active old-schema workers in that database.

## System1 preserved

Flash final factual overview -> `typesafe/jev` typed `noul` answers; weighted threshold 0.75, every hard criterion >=0.5, at most three same-worker content checks. Infrastructure failures remain separate. Stronger divergent alternatives and exhausted checks escalate to Fixer. Manual acceptance is explicit and never inferred from provider exit or fabricated model probabilities.

Wave 844's implementation workers reached the escalation boundary. Fixer independently reviewed code, fresh tests and migration evidence and accepted the deliveries at the second stage; this is **not** labelled a System1 PASS. Earlier failure rows remain immutable. One rejected partial Go delivery was repaired before acceptance. A stale rework-transcript identity mismatch and an unowned process-cleanup incident were recorded separately as feedback; neither is hidden by this acceptance.

## Claude 5.5 routing

Agy 1.2.16 inventory lists Opus 5.5 and Sonnet 5.5 Low/Medium/High variants. Adapter, catalogue, manifest and current Go/Dart menus replace retired 4.6 Thinking routes. Bare family + effort, concrete display labels and raw upstream variant IDs normalize consistently. Unsupported/retired selections fail explicitly, not by silently substituting another model.

Two real governed Netrunners executed through `antigravity`:

- Opus 5.5 / high: wave 846, session 713, actual CLI `SUCCESS`, native MCP checkout/log/report, System1 PASS **0.8925**, manual acceptance and cleanup.
- Sonnet 5.5 / high: wave 847, session 714, actual CLI `SUCCESS`, native MCP checkout/log/report, System1 PASS **0.805**, manual acceptance and cleanup.

Evidence: `agy-opus-5.5-live-smoke-2026-10-04.md` and `agy-sonnet-5.5-live-smoke-2026-10-04.md`. These prove requested route, CLI selection and real execution; they do not claim an independently observable hidden upstream inference identity. Provider-adapter wave 845 passed its third genuine System1 check at **0.7735**, then received manual code/test review.

A later self-selected Opus menu worker exhausted provider quota before completion. Its partial patch was preserved and repaired through a new governed CommandCode wave; the provider failure is not presented as a PASS.

## Independently run checks

- Go backend: fresh `go build ./...`, `go vet ./...`, `go test -count=1 ./...`, including dashboard API and migration fixtures.
- Python combined implementation: **494 passed, 1 skipped, 53 subtests passed**. The skip predates this task.
- Dart/Flutter retirement: independent archived app runs **97**, then **101** passing tests; Serverpod **35** passing tests; explicit wire key-census tests added.
- Terminal client tail: constructor and wire projection no longer expose the field; package and consuming-workspace regression tests added.

Release and final integrated-suite proofs are recorded separately. Publication of platform payloads does not imply deployment to remote fleet machines. Already-open old MCP processes require restart to load the new schema contract.
