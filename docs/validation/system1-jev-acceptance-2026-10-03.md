# System1 / Jev acceptance — 2026-10-03

## Reviewed implementation

The System1 layer uses the approved one-shot CommandCode MiMo 2.6 Flash
transcript reader, then **typesafe/jev** typed `noul` probability questions.
The engine computes weights and the 0.75 overall / 0.5 hard-criterion gates;
Fixer remains the second-stage reviewer and integration authority.

Reviewed worker commits: `9d73fe9` (parser, payload/answer validation and
infrastructure handling), `1ea2145` (exact CommandCode/Pi transcript discovery,
Flash stdin transport and reserved-question validation). Their integrated
source commits are `29e2f1f` and `ab3f2f6` respectively.

## Live evidence (not mocked)

- Archived reader output: **2,493,847 bytes** of real CommandCode JSONL was
  reduced by the new Go parser to its **6,568-byte final factual overview**.
  The typed Jev request was **12,683 bytes**. Jev returned real probabilities
  (c1=0.92, c2=0.40, c3=0.89): overall=0.758, but the hard c2 gate correctly
  rejected it. This replay did not rewrite historical wave/check records.
- Real implementation wave **843**, same worker/session **708**, manual review:
  a genuine Jev content FAIL (overall **0.604**) triggered continuation of
  the same worker. The following installed-candidate full pipeline found the
  recorded CommandCode transcript, ran Flash, extracted its **6,731-byte**
  final overview, called Jev, and persisted a genuine **PASS**:
  c1=**0.86**, c2=**0.67**, c3=**0.84**; weighted overall=**0.798**.
  `stronger_but_different`=0.27, so ordinary PASS handling was used.
- The final wave check used installed **1.0.9-rc.2 (ab3f2f6)**. It left the
  worker `review_ready` for explicit Fixer acceptance; it did not auto-accept.
- Earlier wave 842 and wave 843's first check were failures of the OLD
  installed pipeline (oversized CLI/API input). Their synthetic 0.1 records
  are historical infrastructure failures, NOT Jev assessments. No old rows
  were erased or relabelled to manufacture a PASS.

## Automated evidence

Independent `go build ./...`, `go vet ./...`, and fresh `go test -count=1 ./...`
passed in the reviewed implementation. Python client suites: **466 passed,
1 skipped, 30 subtests passed**. Control-plane, release, installer and install
integration suites also passed.

Regression coverage includes real cmd event shapes, terminal/fallback
`finalText`, no raw-stream leakage, missing/failed/truncated reader output,
UTF-8/per-section/whole-JSON budgets, stdin-only input, exact CommandCode/Pi
session identity, reserved question ids, required/finite/ranged probabilities,
content PASS/requeue/third-failure escalation, stronger-divergent escalation,
and infrastructure attempts that preserve content-check budget and never
requeue implementation (bounded at three attempts).

The latter exceptional branches are deterministic automated tests, not claims
of live provider-induced outages or live stronger-alternative calibration.
Jev probabilities remain model judgments, not a proof of software correctness;
Fixer review and executable tests are still required.

## Contract

See `docs/plans/system1-review-contract.md` and the `run-netrunner-wave` /
`review-netrunner-session` skills. One criterion per line; both CLI inputs use
stdin. Only the final factual overview reaches Jev, never the reader's event,
thinking or tool stream.
