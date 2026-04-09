# Migration Plan

## Goal

Build the first testable desktop orchestration app under `github_repo/` using `Codex Hub` as the UX reference, while keeping the current Fixer MCP stack as the durable source of truth.

This session recommends a thinner compatibility-backed first slice, not a wholesale rebuild of the old `codex_hub_server`.

## Decision Summary

The first implementation slice should be:

- a new Flutter desktop shell for operator UX
- a thin local desktop bridge inside `github_repo/`
- the existing `packages/fixer-mcp-server` as the control plane of record
- the existing `packages/client-wires` as the launch/runtime integration layer

It should not be:

- a new broad backend that reimplements Fixer MCP state management
- a revival of `fixer_genui`
- a direct copy of the old Serverpod stack from Codex Hub

## Reference Findings

### Reuse directly from Codex Hub

- desktop shell and route topology from `codex_hub_flutter`
- project dashboard pattern with chat plus structured side panels
- session workspace pattern with chat, plan, logs, and context panes
- shared chat composition and realtime refresh affordances

### Adapt for the new track

- route naming and surface hierarchy must become `overseer -> fixer -> autonomous netrunner`
- session views must read Fixer MCP concepts such as attached docs, MCP assignments, worker status, and review readiness
- realtime should come from a thin bridge over current Fixer MCP and SQLite state, not from Serverpod-specific generated endpoints

### Drop from the first slice

- deep research workflow
- voice manager and transcription flows
- legacy auth/sign-in flows
- Serverpod-specific generated protocol/client stack
- any netrunner-first UX that bypasses fixer-driven orchestration

## Target Architecture For The First Desktop Slice

```text
Flutter desktop app
  -> local desktop bridge
      -> fixer-mcp-server tools / SQLite read models
      -> client-wires launch helpers
          -> Codex/Droid worker runtimes
```

### Why this shape

- `packages/fixer-mcp-server` already owns durable project/session/doc/run state.
- `packages/client-wires` already owns backend launch semantics, skill/runtime materialization, and backend stickiness.
- A desktop bridge can expose app-friendly read/write operations without forcing Flutter to speak MCP or shell directly.
- This keeps backward compatibility explicit and limits new risk to one new UI shell plus one thin integration layer.

## Recommended Repository Layout Inside `github_repo/`

```text
github_repo/
  apps/
    fixer-desktop/              # new Flutter desktop shell
  packages/
    fixer-mcp-server/           # existing durable control plane
    client-wires/               # existing launcher/runtime integration
    compat-bridge/              # existing CLI compatibility layer
    desktop-bridge/             # new local app-facing bridge
  docs/
    migration-plan.md
```

### Package responsibilities

`apps/fixer-desktop`

- Flutter desktop application
- overseer-first navigation shell
- project dashboard, fixer workspace, session review surfaces
- local persistence only for UI preferences and cached view state

`packages/desktop-bridge`

- local HTTP/WebSocket bridge for the desktop app
- app-facing read models composed from Fixer MCP plus SQLite
- orchestration commands that translate UI intents into Fixer MCP calls
- polling/subscription fan-out for session state, worker status, and review readiness

`packages/fixer-mcp-server`

- remains the system of record for projects, sessions, docs, proposals, MCP selection, and autonomous run status

`packages/client-wires`

- remains the only place that knows how to prepare runtime assets and launch/resume Codex/Droid workers

`packages/compat-bridge`

- remains CLI/operator compatibility infrastructure
- `fixer-compat-bridge` is not the primary desktop path, but it should stay intact during migration

## Backend Integration Strategy With Current Fixer MCP

### Recommendation

Use the desktop bridge as a projection and command layer over current Fixer MCP primitives.

### Read side

Build app-facing read models from:

- `project`
- `session`
- `project_doc`
- `doc_proposal`
- `autonomous_run_status`
- `worker_process`
- `session_external_link`

The bridge should aggregate these into desktop-focused payloads such as:

- project summary with active fixer/autonomous state
- session list with launch backend/model metadata
- review queue with pending doc proposals
- session workspace snapshot with task description, docs, MCP servers, report status, and worker liveness

### Write side

The bridge should translate desktop actions into existing Fixer MCP calls such as:

- `create_task`
- `set_session_attached_docs`
- `set_session_mcp_servers`
- `launch_and_wait_netrunner` where allowed in the GitHub-ready track
- `get_attached_project_docs`
- `propose_doc_update`
- `review_doc_proposals`
- `set_doc_proposal_status`
- `set_session_status`
- `set_autonomous_run_status`

### Explicit non-goal

Do not create a second business-logic database or a second orchestration truth store for the desktop app.

## Desktop Frontend Strategy And Codex Hub Patterns To Port First

### Port first

1. App shell and routing model
2. project dashboard pattern
3. session workspace pattern
4. shared chat panel composition
5. lightweight realtime refresh/status affordances

### Reframe for the new product

Codex Hub route/reference | New desktop meaning
--- | ---
`/` neuro dashboard | overseer hub
project dashboard | fixer project workspace
session workspace | autonomous netrunner session workspace
thread chat | focused chat/debug pane

### First-screen priority

The first usable operator flow should be:

1. open desktop app
2. talk to overseer or select a project
3. enter fixer project workspace
4. inspect active/pending sessions
5. open one session workspace
6. review completion/doc proposals

That is the shortest path to validating the required vertical alignment.

## Domain Model Mapping

### Durable entities already present

`project`

- operator-owned workspace boundary
- maps directly to the `project` table

`session`

- fixer-created unit of work for a worker
- maps directly to the `session` table
- carries task description, status, write scope, backend, model, and reasoning

`project document`

- canon/design/reference material
- maps directly to `project_doc`

`doc proposal`

- worker-authored review payload
- maps directly to `doc_proposal`

`run`

- autonomous fixer execution state plus worker process state
- maps to `autonomous_run_status` plus `worker_process`

`external session link`

- runtime binding to a CLI/backend session
- maps to `session_external_link`

### Product roles and UI surfaces

`overseer`

- not a standalone durable table in the current stack
- model it in the app as a role-scoped conversation/surface that creates or redirects work into project/fixer flows

`fixer`

- orchestrator surface over project plus session collections
- represented through project state, docs, MCP assignment, and review actions

`autonomous netrunner`

- worker behavior for a session
- represented through session state, worker process state, backend metadata, report, and proposals

`thread`

- UI conversation surface, not yet a first-class Fixer MCP table in this repo
- for MVP, treat it as a bridge-level view model backed by role/session context and external runtime state

`review`

- state entered when a worker has produced a report and pending doc proposals
- maps to `session.status = review` plus proposal inventory

## MVP Scope

### In scope

- desktop shell booting locally against repo-packaged services
- overseer hub or project chooser
- fixer project dashboard
- session list with backend/model/status visibility
- session workspace with task, docs, MCPs, report, and worker status
- review queue for pending doc proposals
- locally testable bridge between Flutter UI and current Fixer MCP state

### Deferred

- deep research
- voice/transcription
- multi-window or multi-project concurrent control
- rich netrunner prompt editing before launch
- full thread transcript persistence model
- collaborative auth/users
- broad plugin/connector management UI

## Phase 1: Desktop Contract And Read Models

Deliver:

- finalized desktop app architecture note in this doc
- `desktop-bridge` contract for project, session, run, and review read models
- first transport choice and serialization contract
- repo layout scaffold for `apps/fixer-desktop` and `packages/desktop-bridge`

Exit criteria:

- the next worker can scaffold app and bridge packages without re-deciding architecture
- every MVP screen has an identified backing read model

## Phase 2: Overseer Shell And Project Workspace

Deliver:

- Flutter shell with navigation and desktop-safe layout
- overseer landing surface
- project dashboard adapted from Codex Hub patterns
- bridge endpoints for project summaries and session listings

Exit criteria:

- an operator can launch the desktop shell and reach a live project workspace backed by current Fixer data

## Phase 3: Session Workspace And Review Flow

Deliver:

- session workspace adapted from Codex Hub
- attached-docs, MCP assignment, backend metadata, worker-state, and report views
- review surface for pending `doc_proposal` items

Exit criteria:

- an operator can inspect one worker session end-to-end without dropping to raw MCP tools for normal review

## Phase 4: Local Validation And Packaging Path

Deliver:

- local smoke run instructions for desktop app + bridge + Fixer MCP
- bridge integration tests against a temp SQLite database
- minimal desktop widget/integration tests for the primary operator flow
- packaging notes that align with `scripts/release_public_repo.py`

Exit criteria:

- the first desktop slice is locally testable
- release planning still flows through `scripts/release_public_repo.py` instead of reintroducing ad hoc export logic

## Phase 5: Export Retirement

Keep the previous GitHub-ready packaging goal intact while the desktop app arrives:

- do not reintroduce copy-and-strip export assumptions into the new desktop track
- keep `fixer-compat-bridge` available for legacy CLI migration
- treat the desktop app as another consumer of packaged repo-native surfaces, not a reason to revive private-workspace coupling

Exit criteria:

- desktop work strengthens the standalone `github_repo/` story instead of bypassing it
- the app can be validated without private workspace archaeology

## Recommended Implementation Sequence For The Next Worker

1. Scaffold `apps/fixer-desktop` and `packages/desktop-bridge`.
2. Define bridge DTOs for project summary, session summary, session workspace, and review queue.
3. Implement read-only bridge endpoints first.
4. Port the Codex Hub shell, project dashboard layout, and session workspace layout with placeholder actions.
5. Wire project/session screens to live bridge data.
6. Add the first review actions and status refresh loop.
7. Add launch/control actions only after read flows are stable.

## Validation And Test Plan

### Automated

- Python tests for `desktop-bridge` read model aggregation over temp SQLite fixtures
- bridge integration tests against a launched `fixer-mcp-server` fixture where practical
- Flutter widget tests for shell navigation, project dashboard, and session workspace rendering
- one desktop smoke test covering project list -> session workspace -> review queue

### Manual

- verify app launch from a clean `github_repo/` checkout
- verify one existing project is visible without editing the legacy workspace
- verify session backend/model/reasoning appear correctly from Fixer state
- verify a session entering `review` exposes pending doc proposals correctly

## Session 92 implementation note

The first delivered slice is intentionally read-oriented:

- `packages/desktop-bridge` serves local HTTP payloads directly from the Fixer SQLite control-plane state
- `apps/fixer-desktop` consumes that bridge for project, dashboard, and session workspace views
- write actions remain placeholders until the read contract is stable enough to layer Fixer MCP command calls on top

### Risk checks

- ensure the bridge never mutates Fixer DB state outside existing MCP contracts
- ensure no new app code depends on paths outside `github_repo/`
- ensure backend launch behavior continues to flow through `packages/client-wires`

## Risks

- Flutter desktop adds a second runtime/toolchain to the repo
- realtime semantics are weaker than old Codex Hub until the bridge exposes a stable event model
- thread/chat modeling is partially derived because current Fixer MCP persistence is session-centric, not thread-centric
- scope can drift if the first slice tries to recreate deep research, voice, or full legacy Serverpod behavior

## Final Recommendation

Proceed with a Flutter desktop shell plus a thin `desktop-bridge` over existing Fixer MCP primitives.

That gives the next worker a practical implementation path, preserves backward compatibility, and keeps `github_repo/` aligned with the current Fixer-first product model instead of rebuilding the old Codex Hub backend architecture.
