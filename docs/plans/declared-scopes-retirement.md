# Declared Write Scopes Retirement (2026-10-04)

Status: retired. `declared_write_scope` no longer exists in the system's
current instructions or runtime contracts. This document is the dated record
of that retirement.

## Contract

- No `declared_write_scope` field travels anywhere: not in `create_task`
  envelopes, not in worker prompts, not in execution-envelope serializations,
  not in subprocess handoff, not in `complete_task` reports.
- The `--declared-write-scope` CLI option is removed from
  `client_wires/fixer_autonomous.py` (`launch-wave-worker`) entirely,
  including its environment/default/JSON plumbing and every call site.
- Path-fence prompts are removed: workers are never told to "operate only
  inside" a scope, and reviewers never reject a delivery for changed paths
  outside a scope. Scopes are not replaced with a renamed path fence.
- Human task ownership remains review guidance written as normal task text
  (per-worker responsibilities, forbidden areas), never a runtime path
  allowlist.

## Preserved canon

- System1 first-stage review (typed `noul`, threshold/hard gates,
  `stronger_but_different`, distinct infrastructure handling) is untouched and
  deliberately independent of this retirement.
- Project/role isolation, durable history, clean Git base, process/epoch
  governance, and normal manual acceptance are unchanged.
- Legacy historical docs and research may retain scope wording as dated
  evidence; current instructions must not say scopes exist.

## Ownership split during retirement

- Python/skills layer (`client_wires/**/*.py` + tests, `.agents/skills/*.md`):
  this document's scope; delivered first.
- Go backend, Dart/yaml clients, serverpod, generated dist mirrors: retired by
  their owners. The installed 1.0.9 binary still accepts old fields on
  execution-envelope/complete_task paths until Fixer integrates all results
  and updates the binary; no compatibility shim is added on the Python side.

## Verification (python/skills layer)

- `python3 -m compileall -q client_wires` clean.
- `python3 -m pytest client_wires/tests`: 469 passed, 1 skipped (pre-existing
  backlog 189 cookie-replay skip, unrelated), 53 subtests passed.
- `python3 -m unittest discover -s client_wires/tests -t client_wires/tests`:
  424 tests, OK (skipped=1).
- Tests assert the absence of the scope field in CLI help/options, worker
  prompts, execution-envelope serializations, and command args, and that
  no-scope create/launch paths work.
- `DeclaredScopeSkillRetirementTests` scans every `.agents/skills/**/SKILL.md`
  and fails on any declared-scope or path-fence wording, so current
  instruction skills cannot silently re-advertise scopes.

## Rework note (2026-10-04, session 710 requeue)

- Old release skill mirrors re-materialize on relaunch and during test runs
  and can reintroduce pre-retirement scope wording into tracked skill files.
  Such dirt was positively identified as a byte-exact pre-retirement revert
  and restored to the committed retirement; it is never committed.
- `.factory/skills` mirrors remain generated release artifacts owned by the
  release-mirror layer, not current instructions; they are excluded from this
  layer's patch and stay a known residual until the binary/skill mirror owners
  refresh them.

## Rework note 2 (2026-10-04, session 710 requeue 2)

Final verification evidence for the retirement, recorded so it is directly
auditable:

- Zero `declared_write_scope` / `write scope` / path-fence / scope-fence /
  scope-check tokens remain in owned source (`client_wires/**/*.py` outside
  tests) or in current skill instructions (`.agents/skills/**/SKILL.md`).
- The only remaining bare `scope` / `lease` / `fence` tokens in owned source
  are pre-existing and unrelated to write scopes: two `scope-isolation
  strategy` docstrings in `backends/manifest/schema.py` (provider sandbox
  semantics), the prose "tests in scope" in `fixer_autonomous_prompts.py`, and
  Chrome CDP browser-profile `lease-<pid>.json` filenames in
  `codex_compat/playwright_chrome_cdp.py`.
- The retired flag is not an ignored compatibility placeholder: the committed
  CLI hard-rejects `--declared-write-scope` with argparse exit code 2.
- Named absence/guard tests pass individually: worker prompt omits scope
  wording and path fences, CLI help carries no retired option, launch-plan
  envelope/metadata/command carry no scope fields, module-level prompt kwargs
  carry no scope field, and `DeclaredScopeSkillRetirementTests` scans all 23
  skill instructions clean.
- Full suites: `python3 -m pytest client_wires/tests`: 469 passed, 1 skipped
  (pre-existing backlog 189 cookie-replay skip, unrelated), 53 subtests
  passed; `unittest discover`: 424 tests, OK (skipped=1).
- Launcher mirror re-materialization recurred at requeue relaunch: a tracked
  skill file was overwritten with the main repo's committed fleet-doc version
  and a tracked `.DS_Store` was deleted. Both were positively identified as
  launcher/test-generated tracked dirt and restored to committed versions;
  neither is part of this patch.
