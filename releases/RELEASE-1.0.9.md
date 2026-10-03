# Fixer MCP 1.0.9 — verified System1 / Jev

Source revision: `899cf001187c4e31c879aebaf7e5fed02b9a7f23`.

- Real `typesafe/jev` judge, one-shot MiMo 2.6 Flash factual reader.
- Final `finalText` only: no reader thinking/tool/event stream reaches Jev.
- Both prompts on stdin; independently bounded UTF-8-safe sections and request.
- Exact CommandCode/Pi transcript identity shared by metadata and System1.
- Strict `noul` answer validation; local weighted/hard-gate computation.
- Infra errors are distinct from content failures and never requeue implementation.
- Same-worker content continuation, three-check escalation, stronger-alternative
  escalation, explicit Fixer acceptance.

Live implementation wave 843: genuine FAIL 0.604 -> same-worker continuation ->
genuine PASS 0.798 (hard criteria 0.86 / 0.67 / 0.84), followed by Fixer review.
Full automated suites and four payload verifications passed.

Select the descriptor matching your machine; all payloads have SHA256 checksums
in `SHA256SUMS-1.0.9.txt`:

| Platform | Descriptor | Payload |
| --- | --- | --- |
| Apple Silicon macOS | fixer-mcp-1.0.9-darwin_arm64.json | fixer-mcp-1.0.9-darwin_arm64.tar.gz |
| Intel macOS | fixer-mcp-1.0.9-darwin_amd64.json | fixer-mcp-1.0.9-darwin_amd64.tar.gz |
| x86-64 Linux | fixer-mcp-1.0.9-linux_amd64.json | fixer-mcp-1.0.9-linux_amd64.tar.gz |
| ARM64 Linux | fixer-mcp-1.0.9-linux_arm64.json | fixer-mcp-1.0.9-linux_arm64.tar.gz |

From this repository checkout:

```sh
python3 scripts/install/install.py --descriptor releases/fixer-mcp-1.0.9-<platform>.json
fixer --version
fixer doctor
```

Restart already-open MCP processes to pick up the new binary. New Fixer launches
use the managed `current` release. Old synthetic 0.1 checks remain historical
infrastructure errors, not real Jev assessments; no audit rows were rewritten.

See `docs/plans/system1-review-contract.md` and
`docs/validation/system1-jev-acceptance-2026-10-03.md` for contract and proof.
