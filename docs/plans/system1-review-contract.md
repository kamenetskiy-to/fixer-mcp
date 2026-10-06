# System1 First-Stage Review Contract (`system1-trial-0.1`)

Status: trial. Owner: Fixer MCP wave engine (`fixer_mcp/system1_review.go`).

System1 is the automatic first-stage review that runs between a wave worker
reaching `review_ready` and the Fixer being asked to accept it. It never
accepts work: it either passes the worker through to normal Fixer review,
continues the same worker with an appended rework instruction, or escalates the
worker to Fixer second-stage review.

## Decision rule

Threshold `T` (packet `threshold`, default `0.75`).

A check passes **iff** `overall_probability >= T` **and** every hard criterion
(packet `hard_ids`) has `probability >= 0.5`.

- A hard criterion missing from the judge output is unproven and forces fail.
  A judge response with missing or invalid answers is never scored at all — see
  "Judge answer validation" below.
- Jev returns only typed probabilities, not a chat verdict. The decision rule
  above is computed server-side. Weights are applied locally and deterministically.
- If strict criteria fail and `stronger_but_different` is at least the
  threshold, the worker is not requeued. The Fixer reviews that delivery.

At most `max_checks` checks run per worker (default `3`, clamped `1..3`).

- Fail with budget remaining (< `max_checks`): continue the SAME worker — no
  new session is forked. The check, its criteria and returned probabilities are
  appended to the session task (`update_task` semantics), the session returns
  `review -> pending` (canonical rework transition, increments `rework_count`),
  and the worker row is requeued to `retry_wait` with a cleared stale
  `worker_process_id`; the wait loop's retry scheduler relaunches it in its
  recorded worktree.
- Third failed check (`max_checks` reached): do **not** requeue. The worker is
  marked `system1_escalated`, the operator summary reports
  `operator_state="system1_escalated"` with `next_action="fixer_second_stage_review"`,
  and the Fixer performs second-stage review using the existing manual wave
  close (accept or reject).
- Pass: record `system1_passed` on the worker; it stays `review_ready` so the
  existing manual acceptance close still works. System1 never auto-completes
  the wave.

## Input packet

Attached per wave at `create_netrunner_wave` / `launch_netrunner_wave` as
`system1_check` (required for new waves; missing packet fails closed):

```json
{
  "criteria_prompt": "c1 (weight 0.6, hard): ...\nc2 (weight 0.4, soft): ...",
  "hard_ids": ["c1"],
  "threshold": 0.75,
  "max_checks": 3,
  "contract_version": "system1-trial-0.1"
}
```

- `criteria_prompt` (required): per-task System1 review instructions — criteria
  `c1..cn` with weight and `hard|soft`, **one criterion per line** (the `\n`
  above is a real newline). A semicolon-separated single line parses as ONE
  criterion whose instructions are everything after the first colon, so prompts
  must use newline-separated criteria. Free-form lines that are not criterion
  lines are preserved verbatim as judging guidance that travels with every
  criterion question; nothing is silently dropped. Hard size and validity rules
  apply — see "Criteria input errors".
- `hard_ids` (optional): criterion ids that must reach `probability >= 0.5`;
  each must reference a criterion parsed from `criteria_prompt`.
- `threshold` (optional, default `0.75`): pass threshold for
  `overall_probability`; must be in `(0, 1]`.
- `max_checks` (optional, default `3`): clamped to `1..3`.
- `contract_version` (required): must be `system1-trial-0.1`.

## Reader output parsing

The transcript reader is Pi on the Architect's existing CommandCode Flash
subscription, never a nested implementation Netrunner or a direct `cmd`
reader. The portable route is `commandcode/xiaomi/mimo-v2.6-flash` using the
already configured generic subscription; configuration and auth are reused,
not replaced with a new paid API. Explicit named MiMo subscription routes for
implementation workers are validated against actual configured Pi inventory,
not hardcoded operator/account identifiers.

Fresh/resumed Pi runs register exact header IDs, cwd/time/attempt evidence and
an append-only attempt manifest. Recovery uses those durable launch anchors
when an old launcher omitted the ID; it never guesses an arbitrary newest file.
The public transcript tool and System1 share proven identity resolution,
including relevant continuation history. Contradictory or missing provenance
fails as infrastructure, not as a content score.

Every original JSONL range is read sequentially. Large histories are chunked,
with contiguous byte/line/EOF coverage and per-chunk execution ledgers checked
before judging. No 128-KiB head/tail or compressed index substitutes for original
evidence. Execution-capacity limits fail visibly as infrastructure, never
silently omit ranges. Only after full coverage is proven may the factual
observations be compacted into the bounded final overview.

Pi runs in an isolated cwd and agent directory with discovered extensions,
project MCP, hooks, skills, context files and builtin bash/write/edit/read tools
disabled. Its sole explicitly pinned `read_evidence` tool is restricted to the
exact proven evidence paths. It has no Fixer/DB/lifecycle capability.

Only a completed final assistant text from Pi's JSON event stream becomes the
overview. Thinking, intermediate turns and tool results never reach Jev.
Truncated, aborted, errored or mid-tool-execution runs fail closed. Durable run
metadata records coverage, timing, redacted partial output and timeout errors;
full secret-bearing argv/environment/auth files are never dumped.

## Judge payload (typed, size-bounded, stdin)

The judge request is the typed `typesafe/jev` JSON request sent on stdin. It
carries only:

1. `state`: the final factual transcript overview (evidence; wins over the
   report on conflict) and the worker final report (a claim, not evidence).
   Each section is clipped UTF-8-safely to its own budget (overview 8 KiB,
   report 4 KiB) with an explicit truncation notice, so a large report can
   never erase the evidence or the criteria.
2. `questions`: one `noul` question per criterion (criterion text verbatim,
   plus any judging guidance) and the `stronger_but_different` question.

The whole JSON payload including the questions is bounded (24 KiB); the
criterion questions have their own 8 KiB budget. If escaping inflates the
marshal beyond the bound, the two clip-safe sections are shrunk first; the
criterion text itself is never clipped.

### Criteria input errors

Oversized or invalid criteria are visible input errors (rejected at
`create_netrunner_wave` / `launch_netrunner_wave`, and again at check time as a
recorded input error) — never silently missing or altered requirements:

- `criteria_prompt` larger than 8 KiB.
- More criterion text than the judge question budget can hold.
- Duplicate criterion ids, invalid stated weights (`weight` must be a finite
  number > 0), `hard_ids` that reference no parsed criterion, or the reserved
  `stronger_but_different` id used as a criterion id (a criterion can never
  overwrite the real stronger-but-different question).

## Judge answer validation

Every asked question (each criterion plus `stronger_but_different`) must be
answered with `type:"noul"` and a finite `noul` in `[0, 1]`. A missing answer,
a missing/null `noul`, a wrong type, or an out-of-range value is an invalid
judge response — a missing `noul` is never scored as a default `0.0`, an
invalid response is never a meaningful content verdict, and it can never pass a
hard gate.

## Infrastructure failures

Technical failures of the check machinery itself — reader/CLI/API/parsing/
context failures, judge executor errors (for example provider
`max_tokens_exceeded`), invalid judge answers, oversized judge input — are
infrastructure failures, never content verdicts:

- They do not consume the `max_checks` content-check budget. The atomic claim
  on `system1_checks_used` is released with the same compare-and-set guard it
  was taken with (never a blind decrement), and historical check rows are
  never mutated.
- They never append "fix your implementation" feedback, never requeue the
  implementation worker, and never move the session: the worker stays
  `review_ready`.
- Every attempt is visible to the Fixer as an appended `wave_system1_check` row
  with `verdict="infra_failed"` and the diagnostic in `summary`, plus a
  diagnostic artifact.
- Retries are bounded: after `3` infrastructure attempts the worker is marked
  `system1_escalated` and goes to Fixer second-stage review
  (`operator_state="system1_escalated"`,
  `next_action="fixer_second_stage_review"`), so the wait loop can never retry
  in a tight infinite loop.

## Executors

The reader uses restricted one-shot Pi executions (10-minute per-execution
bound); Jev remains a bounded one-shot `cmd` call (2 minutes). They are review
components, not Netrunner implementation sessions, and no homemade model HTTP
API is called. Full inputs travel on stdin, never transcript-sized argv. The
reader uses isolated capabilities; Jev receives only the bounded typed request.

- Judge: `typesafe/jev` via `cmd -m typesafe/jev -p` with the typed request on
  stdin. Jev is not a chat model. The engine sends `state` plus one `noul`
  question per criterion, plus `stronger_but_different`. Jev returns
  probabilities. The engine applies the 0.75 / 0.5 rule itself.
- Transcript reader: Pi + existing CommandCode `xiaomi/mimo-v2.6-flash`, with
  original transcript chunks on stdin and a pinned restricted evidence tool.
  Coverage is verified before synthesis/judging. It must not judge quality.

## Default prompts

### Transcript reader (analyst) prompt

> You are an independent transcript reader. Read the worker full session transcript and produce a FACTUAL overview of what actually happened. Do not judge quality. Sections: timeline with line references; commands actually executed and real outcomes; files actually modified; tests actually run and genuine results; errors, dead ends, reverts; a claims-vs-observed table against the worker final report (supported / partially / unsupported / not observable). Evidence over narration. Read every original range and relevant continuation; if provenance or coverage is missing, report an infrastructure gap without inventing observations. Stay under 1200 words.

## Continuation text (failed check, budget remaining)

Appended to the session task verbatim in spirit of:

> You have not passed the System1 check. Here is the check that was performed, its criteria, and the returned probabilities. Address the unresolved parts.

followed by the check id, contract version, per-criterion probabilities marked
`looks satisfied` (>= 0.5) or `gap remains` (< 0.5), blocking items, and the
check summary. This text is only ever appended for a genuine content failure.

## Artifacts and recorded rows

- Content-check artifact:
  `.codex/netrunner_wave_artifacts/wave-<wave_id>/session-<local_session_id>-system1-<check_number>.json`
  containing the packet, worker status, final report, transcript path, reader
  report, raw judge output, parsed verdict, and the decision outcome.
- Infrastructure-failure artifact (never overwrites a content-check artifact):
  `session-<local_session_id>-system1-<check_number>-infra-<attempt>.json`
  with `outcome="infra_failed"`, `infra_attempt`, and `infra_diagnostic`.
- Short rows the Fixer can read: `wave_system1_check` (one row per check and
  per infrastructure attempt; `verdict` is `pass`, `fail`, or `infra_failed`)
  plus the `wave_system1_packet` row; exposed through `get_system1_reviews`.
- Worker state: `parallel_wave_worker.system1_state`
  (`'' | 'passed' | 'escalated'`) and `system1_checks_used` (content checks
  only; infrastructure attempts are counted by their appended rows).
