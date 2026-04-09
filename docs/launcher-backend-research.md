# Launcher Backend Research

## Scope

This note answers the staged research brief for fresh launch, resume, MCP attachment, prompt handoff, and model-selection behavior across:

- Codex CLI
- Factory Droid CLI
- Claude Code

The goal is not to ship the final UX yet. The goal is to define what the next implementation slice can safely build.

## Current Repo State

The staged public launcher in `packages/client-wires` currently exposes:

- backend descriptors for `codex` and `droid`
- a `plan-launch` surface that previews fresh headless launch commands
- a staged Claude Code catalog entry may be surfaced, but no truthful headless command builder should ship until the contract exists
- no packaged resume planner yet

This matters because the backend/model chooser requested by the architecture brief must be split into:

1. fresh-launch selection
2. persisted session metadata
3. backend-specific resume behavior

Those are not interchangeable across the three CLIs.

## Findings

### Codex CLI

Fresh launch:

- Headless automation is a plain `codex exec ...` flow.
- MCP servers can be attached directly on the command line with repeated `--mcp=<name>` flags.
- Model selection is explicit at launch with `--model`.
- Reasoning effort is configurable at launch.

Resume:

- `codex resume` is the documented resume path for interactive sessions.
- `codex exec resume [SESSION_ID]` is the documented resume path for non-interactive exec sessions.
- Official docs state the resume command accepts the same global flags as `codex`, including model overrides and sandbox overrides.

Practical implication:

- Even though the CLI accepts model flags on resume, Fixer should still treat resume as backend/model sticky.
- The public operator UX should store and reuse the original backend/model unless an explicit future policy allows rebinds after validation.
- The current Fixer storage model already points in the right direction with `cli_backend`, `cli_model`, `cli_reasoning`, and `session_external_link`.

### Factory Droid CLI

Fresh launch:

- Headless automation is `droid exec`.
- Model selection is explicit with `-m/--model`.
- Reasoning is explicit with `-r/--reasoning-effort`.
- The public docs show `--auto`, `--output-format`, `--cwd`, and other automation flags.

Resume:

- Official Factory docs document `droid exec -s <session-id> "prompt"` as the headless resume path.
- Interactive resume also exists via `/sessions` and mission-oriented flows.

MCP attachment:

- Droid does not use inline per-launch `--mcp=` flags like Codex.
- Official docs describe layered MCP configuration files:
  - user: `~/.factory/mcp.json`
  - project: `.factory/mcp.json`
- The staged launcher note about materializing `.factory/settings.json` is not aligned with the public MCP docs and should be treated as a temporary local-runtime assumption, not the public contract.

Model constraints:

- Current official model lists are broader than the staged descriptor in this repo.
- The staged repo currently lists only:
  - `gpt-5.3-codex`
  - `claude-sonnet-4.5`
  - `glm-5`
- Factory docs currently expose a materially larger set, including `gpt-5.4`, `gpt-5.3-codex`, `gpt-5.2-codex`, Claude 4.6 variants, Gemini 3.1 Pro, `glm-5`, `kimi-k2.5`, and `minimax-m2.5`.

Practical implication:

- The packaged Droid adapter should not hardcode a narrow model enum unless the repo also owns a deliberate compatibility policy.
- The safer first implementation is a curated allowlist sourced from repo config, not embedded constants in the adapter.

### Claude Code

Fresh launch:

- Interactive fresh launch is `claude`.
- Non-interactive/headless usage is documented in print/headless mode.
- Model choice is part of session configuration and documented CLI/runtime behavior.

Resume:

- Official docs document `claude --continue` for the latest conversation in the current directory.
- Official docs document `claude --resume` to pick or resume a named session.
- Official docs state that a resumed conversation starts with the same model and configuration as the original.

MCP attachment:

- Claude Code supports configured MCP servers and project/user scopes.
- Official docs and changelog material show project-scoped MCP configuration via `.mcp.json`.
- Official docs also note that Claude.ai-provided MCP/connectors can appear in Claude Code, subject to policy.

Prompt and skills handoff:

- Claude Code supports filesystem-based project instructions and skills through `CLAUDE.md` and `.claude/skills`.
- That makes it compatible with Fixer-style launch prompts, but the exact prompt/skill injection path should not be assumed to match Codex or Droid.

Model constraints:

- Resume is explicitly model/config sticky in the official docs.
- That is the strongest documented reason to enforce the architecture rule that only fresh launches offer backend/model choice.

Practical implication:

- Claude Code should be added as a first-class backend only after the launcher can model:
  - fresh launch command
  - persisted external session ID
  - resume command shape
  - MCP config scope
  - explicit sticky-resume policy

## Backend Comparison

| Question | Codex | Droid | Claude Code |
| --- | --- | --- | --- |
| Fresh headless launch | `codex exec` | `droid exec` | supported, but packaged path still needs adapter design |
| Headless resume | `codex exec resume <id>` | `droid exec -s <id>` | `claude --continue` or `claude --resume` |
| Interactive resume | `codex resume` | `/sessions` / mission flows | `/resume` / `--resume` |
| MCP attach model | inline `--mcp=` flags | layered config files | layered config files and shared connectors |
| Resume model stability | Fixer should enforce sticky policy even if CLI allows flags | treat as sticky | documented as sticky |
| Packaged support in repo today | partial | partial | missing |

## Recommendation

Implement the launcher selection work in three serial steps.

### Step 1: Make session metadata authoritative

Before exposing any new picker in the public launcher:

- treat `cli_backend`, `cli_model`, and `cli_reasoning` as the only source of truth for resume
- treat `session_external_link` as the required backend-specific resume key
- reject or hide backend/model selection in any resume path

This is already directionally consistent with the legacy Fixer wire and should become the public contract.

### Step 2: Split fresh-launch adapters from resume adapters

Add a backend interface that distinguishes:

- fresh launch command builder
- resume command builder
- MCP attachment strategy
- supported model source

Do not reuse the current staged `plan-launch` adapter shape for resume. The backend differences are too large.

Minimum backend contract:

- `build_fresh_command(...)`
- `build_resume_command(...)`
- `materialize_mcp_config(...)` or `mcp_attachment_mode`
- `normalize_model(...)`
- `normalize_reasoning(...)`

### Step 3: Add Claude Code only after the contract exists

Do not bolt Claude Code onto the current staged adapter surface.

Instead:

- land the split fresh/resume contract first
- move backend model lists into config or generated metadata
- then add a Claude Code adapter with sticky resume semantics from day one

## Implementation Guidance For The Next Slice

1. Keep the public UX rule from the brief:
   fresh launches may choose backend + model; resumes may not.
2. Change the staged Droid public contract:
   stop presenting `.factory/settings.json` as the public MCP attachment story unless the runtime actually guarantees that as the supported path.
3. Replace hardcoded backend model tuples with config-backed curated lists.
4. Keep Codex resume locked to stored metadata even though the CLI can accept global flags.
5. Add Claude Code as a planned backend, not an immediate toggle in the current packaged CLI.

## Repo Impact

The next implementation session should touch at least:

- `packages/client-wires/src/fixer_client_wires/backends/base.py`
- `packages/client-wires/src/fixer_client_wires/backends/`
- `packages/client-wires/src/fixer_client_wires/launcher.py`
- launcher tests for fresh-vs-resume planning

It should not change the legacy workspace outside `github_repo/`.

## Sources

- OpenAI Codex CLI reference: `codex resume`, `codex exec resume`, and MCP/config references
- Factory CLI reference and settings/MCP docs: `droid exec`, `-s/--session-id`, model list, and layered MCP configuration
- Anthropic Claude Code docs: `--continue`, `--resume`, resume configuration stickiness, and MCP/project configuration behavior
