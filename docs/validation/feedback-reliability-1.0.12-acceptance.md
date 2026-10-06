# Feedback reliability remediation — 1.0.12 product acceptance

Date: 2026-10-05 UTC. Architect-authorized autonomous remediation of verified
feedback, preserving the Hands/TUI baseline `f5fd495` and unrelated live WIP.
This document records **software acceptance**, not a claim of fleet installation.
Publication, checksummed downloads and local activation have separate proofs.

## Accepted changes

- Decimal quotas are parsed as whole percentages, with account/month attribution;
  positive fractions below one percent are not mistaken for exhausted capacity.
  Explicit rework eligibility remains immediate; real provider limits retain
  durable deadlines. Operator summaries distinguish retry-pending from liveness.
- Interrupted serial launches reconcile process/launch identity without arbitrary
  PID attribution. Same-worker rework is atomic even for partially failed waves.
  Governed wave retirement and conservative submodule cleanup preserve audit and
  dirty/unowned data; no raw lifecycle SQL or fictitious acceptance is used.
- Pi launches and continuations persist exact identities and append-only attempt
  manifests. Durable launch anchors recover omitted IDs without guessing another
  session. Original cumulative histories are read sequentially with complete
  range/EOF/execution ledgers; missing or partial input is infrastructure, not a
  content verdict.
- Reader execution uses **Pi on the existing CommandCode Flash subscription**,
  isolated configuration/cwd and a pinned evidence-only tool. Project MCP,
  Fixer authority, discovery hooks/skills and builtin write/edit/bash are denied.
  Chunked reading and redacted timeout diagnostics replace head/tail clipping.
  Jev, weighted threshold 0.75, hard gate 0.5 and the three-check budget remain
  unchanged. No paid BYOK substitute or hidden-model-identity claim is made.
- Health reports the actual caller binary/process/source/schema/DB, independently
  of stored restart markers. Launch/resume stale-binary gates fail closed;
  doctor probing is side-effect-free, aliases are normalized, and old-schema
  migrations require quiescence. Divergent host/checkout histories are retained,
  not blindly merged or overwritten.
- Canon proposals distinguish creation from targeted updates; matching doc type
  alone cannot authorize overwrite. Proposals are required only for real canon
  impact. Malformed/legacy cleanup claims retain raw evidence and remain
  unverified rather than receiving fabricated success.
- Existing Pi subscription routes/reasoning are validated and `scripts/pi_probe.py`
  is shipped. Native Hands/Fixer selection, Pi-default Hands, categorized MCP and
  hierarchical documents from `f5fd495` are preserved.

## Independent evidence

Fixer validation ran from committed archived source with explicit isolated DBs:

- Go: build, vet and complete MCP module tests passed (104.904 seconds).
- Python/provider/client: **602 passed, 1 skipped, 53 subtests passed**.
- Release packaging/installer/install: **125 passed, 48 subtests passed**.
- Control-plane Go suite passed.
- Flutter **110**, terminal provider **28**, terminal control **8** tests passed.
- Actual isolated stdio smoke passed against the integrated binary.
- Actual authorized restricted Pi/CommandCode Flash Reader smoke passed in about
  23 seconds. Its three-line synthetic transcript retained the primary middle
  marker and a complete executed coverage ledger; this is a live route/sandbox
  smoke, not a claim that a large real transcript was live-reviewed. Large-file,
  history and fault-path coverage are hermetic regression tests.

Local evidence directory: `/tmp/fixer-1.0.12-orchestration/`. No secrets, auth
values or full process environments are part of this document.

## Honest governance

- Wave 869 closed **rejected**: four original infra/partial-delivery failures
  remain unchanged. Its accepted partial quota delivery is recorded separately.
- Waves 870–873 were reviewed serially and integrated by Fixer. Reader 871 has a
  genuine second-check PASS; lifecycle repair 872 has a genuine third-check PASS.
  Exhausted/escalated checks for the other workers are retained as such. Manual
  second-stage software acceptance is **not** labelled System1 PASS.
- Hermetic resume fixtures were repaired after three independent integration
  failures. Production stale-identity protection was not bypassed.
- A misleading numeric-string/schema test was explicitly reverted; upstream MCP
  integer-to-string coercion and unverified auth-loss reports are **not** claimed
  fixed. Superseded worker proposals were rejected, unrelated proposals retained.
- Retired scope columns/capabilities were not restored. Both original DBs and
  histories remain; 33 baseline FK findings are not claimed repaired.
- User WIP, paused historical recovery, unknown files and preservation stashes
  remain untouched. No permanent Hands actor or remote installation was launched.

Release acceptance additionally requires immutable MCP linker provenance in all
four verified payloads and a controlled local doctor/stdio check after activation.
