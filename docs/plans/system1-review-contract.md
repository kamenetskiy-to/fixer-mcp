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

The transcript reader is still the approved one-shot `cmd` Flash call (never a
nested Netrunner). The worker transcript is resolved by the recorded external
session id only — CommandCode under `~/.commandcode/projects/<slug>/<id>.jsonl`,
Pi under `~/.pi/agent/sessions/<dir>/<timestamp>_<id>.jsonl` — through the same
lookup the public `get_netrunner_transcript_path` tool reports; an unresolved
transcript is a visible diagnostic, never another session's file. Its stdout is
parsed strictly:

- `cmd --print --output-format json` emits a JSONL event stream
  (`{"type":"event","event":{...}}` lines and a terminal
  `{"type":"result",...}` line). For a recognized event stream only the
  terminal **successful** `result.finalText` is the overview; the fallback is
  `run_end.result.finalText` when the terminal line is missing or empty.
- A `subtype:"error"` result, a `run_error`, or `max_turns` turn-limit
  truncation is recognized and fails closed — a truncated or failed run is
  never presented as a complete overview.
- A recognized event stream without a final overview fails closed. The event
  stream, intermediate text, thinking, and tool output are never sent to the
  judge.
- Legitimate existing single-document JSON results (`result`/`text`/`output`/
  `response`/`message`/`content` fields) and plain-text results stay supported.

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

Both executors are bounded one-shot headless `cmd` CLI invocations (reader:
10-minute timeout; Jev: 2-minute timeout; output captured) in the project runtime environment. They are not
Netrunner sessions, and no homemade model HTTP API is ever called. Both send
their full input on stdin — never on argv, where a transcript-sized prompt can
exceed `ARG_MAX` — and run `cmd --print`, which reads that stdin.

- Judge: `typesafe/jev` via `cmd -m typesafe/jev -p` with the typed request on
  stdin. Jev is not a chat model. The engine sends `state` plus one `noul`
  question per criterion, plus `stronger_but_different`. Jev returns
  probabilities. The engine applies the 0.75 / 0.5 rule itself.
- Transcript reader: one-shot `cmd` with `xiaomi/mimo-v2.6-flash` and the
  reader prompt plus transcript on stdin. It must not judge quality.

## Default prompts

### Transcript reader (analyst) prompt

> You are an independent transcript reader. Read the worker full session transcript and produce a FACTUAL overview of what actually happened. Do not judge quality. Sections: timeline with line references; commands actually executed and real outcomes; files actually modified; tests actually run and genuine results; errors, dead ends, reverts; a claims-vs-observed table against the worker final report (supported / partially / unsupported / not observable). Evidence over narration. If the transcript is missing or unreadable, say exactly what is missing. Stay under 1200 words.

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
