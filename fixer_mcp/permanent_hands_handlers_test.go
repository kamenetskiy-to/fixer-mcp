package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
)

func TestHandsInstructionOutputSchemasUseJSONObjectEnvelopes(t *testing.T) {
	schema, err := jsonschema.For[GetHandsInstructionOutput](nil)
	if err != nil {
		t.Fatalf("infer get_hands_instruction output schema: %v", err)
	}
	wanted := map[string]bool{
		"instruction_envelope": false,
		"payload":              false,
		"result_envelope":      false,
	}
	var inspect func(*jsonschema.Schema)
	inspect = func(current *jsonschema.Schema) {
		if current == nil {
			return
		}
		for name, property := range current.Properties {
			if _, ok := wanted[name]; ok {
				if property.Type != "object" {
					t.Fatalf("schema property %q must be an object, got type %q", name, property.Type)
				}
				wanted[name] = true
			}
			inspect(property)
		}
		for _, definition := range current.Defs {
			inspect(definition)
		}
		inspect(current.Items)
	}
	inspect(schema)
	for name, found := range wanted {
		if !found {
			t.Fatalf("schema property %q was not discovered", name)
		}
	}
}

func TestPermanentHandsIdentityAndProviderDefaultsAreAvailable(t *testing.T) {
	setupWorkroomHandlerTestDB(t)
	var identityCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM project_hands WHERE project_id = 1 AND display_name = 'Руки'`).Scan(&identityCount); err != nil {
		t.Fatalf("count identity: %v", err)
	}
	if identityCount != 1 {
		t.Fatalf("unexpected identity count: %d", identityCount)
	}
	if err := initProjectWorkroomSchema(); err != nil {
		t.Fatalf("repeat workroom migration: %v", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM project_hands WHERE project_id = 1`).Scan(&identityCount); err != nil {
		t.Fatalf("recount identity: %v", err)
	}
	if identityCount != 1 {
		t.Fatalf("repeat migration duplicated identity: %d", identityCount)
	}
	lanes := readHandsLanes()
	if len(lanes) != 5 || lanes[0].Model != "gpt-5.6-luna" || lanes[0].Reasoning != "high" || lanes[2].Model != "kimi-k3-256k" || lanes[4].Provider != "grok" {
		t.Fatalf("unexpected provider defaults: %+v", lanes)
	}
}

func TestSubmitHandsInstructionCreatesInternalCompatibilityProjectionAndIsIdempotent(t *testing.T) {
	setupWorkroomHandlerTestDB(t)
	ctx := context.Background()
	input := SubmitHandsInstructionInput{
		InstructionText: "Inspect the replay contract without changing files.",
		RequestedLane:   "codex",
		IdempotencyKey:  "hands-read-only-1",
	}
	_, first, err := SubmitHandsInstruction(ctx, nil, input)
	if err != nil {
		t.Fatalf("submit instruction: %v", err)
	}
	if first.State != "queued" || first.RiskClass != "read_only" || first.InstructionID == "" || first.Ordinal != 1 {
		t.Fatalf("unexpected receipt: %+v", first)
	}
	_, replay, err := SubmitHandsInstruction(ctx, nil, input)
	if err != nil {
		t.Fatalf("replay submit: %v", err)
	}
	if replay.InstructionID != first.InstructionID || replay.ProjectSeq != first.ProjectSeq {
		t.Fatalf("submit replay diverged: first=%+v replay=%+v", first, replay)
	}
	if _, err := db.Exec(`UPDATE project_hands SET next_instruction_ordinal = 1 WHERE project_id = 1`); err != nil {
		t.Fatalf("stale ordinal setup: %v", err)
	}
	_, second, err := SubmitHandsInstruction(ctx, nil, SubmitHandsInstructionInput{
		InstructionText: "Verify the next mailbox ordinal.",
		RequestedLane:   "codex",
		IdempotencyKey:  "hands-read-only-2",
	})
	if err != nil || second.Ordinal != 2 {
		t.Fatalf("stale mailbox ordinal must self-heal: receipt=%+v err=%v", second, err)
	}
	var instructionCount, compatSessionID int
	if err := db.QueryRow(`SELECT COUNT(*), compat_session_id FROM hands_instruction WHERE project_id = 1`).Scan(&instructionCount, &compatSessionID); err != nil {
		t.Fatalf("inspect instruction: %v", err)
	}
	if instructionCount != 2 || compatSessionID <= 0 {
		t.Fatalf("expected one instruction with internal projection: count=%d session=%d", instructionCount, compatSessionID)
	}
	var sessionKind, sessionStatus string
	if err := db.QueryRow(`SELECT session_kind, status FROM session WHERE id = ?`, compatSessionID).Scan(&sessionKind, &sessionStatus); err != nil {
		t.Fatalf("inspect compatibility session: %v", err)
	}
	if sessionKind != "hands_instruction" || sessionStatus != "pending" {
		t.Fatalf("unexpected compatibility projection: kind=%s status=%s", sessionKind, sessionStatus)
	}
	_, detail, err := GetHandsInstruction(ctx, nil, GetHandsInstructionInput{InstructionID: first.InstructionID})
	if err != nil {
		t.Fatalf("get instruction: %v", err)
	}
	if len(detail.Events) != 1 || len(detail.Generations) != 1 || detail.Generations[0].Status != "planned" {
		t.Fatalf("unexpected instruction detail: %+v", detail)
	}
}

func TestHandsInstructionEnvelopesMarshalAsJSONObjects(t *testing.T) {
	setupWorkroomHandlerTestDB(t)
	ctx := context.Background()
	_, receipt, err := SubmitHandsInstruction(ctx, nil, SubmitHandsInstructionInput{
		InstructionText: "Inspect object envelope schemas.", RequestedLane: "codex", IdempotencyKey: "object-envelope-1",
	})
	if err != nil {
		t.Fatalf("submit instruction: %v", err)
	}
	_, detail, err := GetHandsInstruction(ctx, nil, GetHandsInstructionInput{InstructionID: receipt.InstructionID})
	if err != nil {
		t.Fatalf("get instruction: %v", err)
	}
	payload, err := json.Marshal(detail)
	if err != nil {
		t.Fatalf("marshal detail: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if _, ok := decoded["instruction_envelope"].(map[string]any); !ok {
		t.Fatalf("instruction_envelope must be a JSON object, got %T", decoded["instruction_envelope"])
	}
	events, ok := decoded["events"].([]any)
	if !ok || len(events) == 0 {
		t.Fatalf("expected decoded events array, got %#v", decoded["events"])
	}
	firstEvent, ok := events[0].(map[string]any)
	if !ok {
		t.Fatalf("event must be an object, got %T", events[0])
	}
	if _, ok := firstEvent["payload"].(map[string]any); !ok {
		t.Fatalf("event payload must be a JSON object, got %T", firstEvent["payload"])
	}
}

func TestWaitHandsInstructionIgnoresUnrelatedWakeAndFindsExternalCommit(t *testing.T) {
	setupWorkroomHandlerTestDB(t)
	ctx := context.Background()
	_, receipt, err := SubmitHandsInstruction(ctx, nil, SubmitHandsInstructionInput{
		InstructionText: "Watch this instruction.", RequestedLane: "codex", IdempotencyKey: "wait-live-tail-1",
	})
	if err != nil {
		t.Fatalf("submit instruction: %v", err)
	}

	type waitResult struct {
		output WaitHandsInstructionOutput
		err    error
	}
	result := make(chan waitResult, 1)
	go func() {
		_, output, waitErr := WaitHandsInstruction(ctx, nil, WaitHandsInstructionInput{
			InstructionID: receipt.InstructionID, AfterEventOrdinal: 1, TimeoutSeconds: 2,
		})
		result <- waitResult{output: output, err: waitErr}
	}()

	deadline := time.Now().Add(time.Second)
	for {
		handsWaiters.Lock()
		registered := len(handsWaiters.byProject[1]) > 0
		handsWaiters.Unlock()
		if registered {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("waiter was not registered")
		}
		time.Sleep(5 * time.Millisecond)
	}

	// A project-wide wake for another instruction must not return an empty result.
	notifyHandsWaiters(1)
	select {
	case got := <-result:
		t.Fatalf("unrelated wake returned prematurely: output=%+v err=%v", got.output, got.err)
	case <-time.After(100 * time.Millisecond):
	}

	// Commit without the in-process notifier, as a different MCP process would.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin external event transaction: %v", err)
	}
	if _, err := appendHandsInstructionEventTx(ctx, tx, receipt.InstructionID, "instruction.external_test", "queued", "queued", "system", "external-process", map[string]any{"source": "test"}); err != nil {
		_ = tx.Rollback()
		t.Fatalf("append external event: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit external event: %v", err)
	}

	select {
	case got := <-result:
		if got.err != nil {
			t.Fatalf("wait returned error: %v", got.err)
		}
		if got.output.TimedOut || got.output.LatestEventOrdinal != 2 || got.output.Instruction == nil {
			t.Fatalf("unexpected live-tail result: %+v", got.output)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("wait did not discover external commit")
	}
}

func TestSubmitHandsInstructionAcceptsEveryRegisteredLane(t *testing.T) {
	setupWorkroomHandlerTestDB(t)
	_, receipt, err := SubmitHandsInstruction(context.Background(), nil, SubmitHandsInstructionInput{
		InstructionText: "Use the registered provider lane.", RequestedLane: "kimi-code", IdempotencyKey: "registered-lane-1",
	})
	if err != nil {
		t.Fatalf("registered lane must be accepted: %v", err)
	}
	if receipt.State != "queued" {
		t.Fatalf("unexpected registered-lane receipt: %+v", receipt)
	}
	var compatCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM session WHERE session_kind = 'hands_instruction'`).Scan(&compatCount); err != nil {
		t.Fatalf("count compatibility sessions: %v", err)
	}
	if compatCount != 1 {
		t.Fatalf("registered lanes must create runnable compatibility sessions, got %d", compatCount)
	}
}

func TestNetrunnerHandsStateIsRestrictedToItsCompatibilityInstruction(t *testing.T) {
	setupWorkroomHandlerTestDB(t)
	ctx := context.Background()
	_, first, err := SubmitHandsInstruction(ctx, nil, SubmitHandsInstructionInput{
		InstructionText: "First isolated instruction.", RequestedLane: "codex", IdempotencyKey: "isolation-1",
	})
	if err != nil {
		t.Fatalf("submit first instruction: %v", err)
	}
	_, second, err := SubmitHandsInstruction(ctx, nil, SubmitHandsInstructionInput{
		InstructionText: "Second isolated instruction.", RequestedLane: "codex", IdempotencyKey: "isolation-2",
	})
	if err != nil {
		t.Fatalf("submit second instruction: %v", err)
	}
	var firstSessionID, secondSessionID int
	if err := db.QueryRow(`SELECT compat_session_id FROM hands_instruction WHERE id = ?`, first.InstructionID).Scan(&firstSessionID); err != nil {
		t.Fatalf("read first compatibility session: %v", err)
	}
	if err := db.QueryRow(`SELECT compat_session_id FROM hands_instruction WHERE id = ?`, second.InstructionID).Scan(&secondSessionID); err != nil {
		t.Fatalf("read second compatibility session: %v", err)
	}
	authorizedRole = "netrunner"
	authorizedSessionId = firstSessionID
	_, state, err := GetHandsState(ctx, nil, GetHandsStateInput{})
	if err != nil {
		t.Fatalf("read first isolated state: %v", err)
	}
	if state.QueuedCount != 1 || state.ActiveInstruction == nil || state.ActiveInstruction.InstructionID != first.InstructionID {
		t.Fatalf("first Netrunner observed another instruction: %+v", state)
	}
	authorizedSessionId = secondSessionID
	_, state, err = GetHandsState(ctx, nil, GetHandsStateInput{})
	if err != nil {
		t.Fatalf("read second isolated state: %v", err)
	}
	if state.QueuedCount != 1 || state.ActiveInstruction == nil || state.ActiveInstruction.InstructionID != second.InstructionID {
		t.Fatalf("second Netrunner observed another instruction: %+v", state)
	}
}

func TestHandsReadOnlyCompatibilityLifecycleCompletesAtomically(t *testing.T) {
	setupWorkroomHandlerTestDB(t)
	ctx := context.Background()
	_, receipt, err := SubmitHandsInstruction(ctx, nil, SubmitHandsInstructionInput{
		InstructionText: "Perform a read-only audit.", RequestedLane: "codex", IdempotencyKey: "read-lifecycle-1",
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	var globalSessionID int
	if err := db.QueryRow(`SELECT compat_session_id FROM hands_instruction WHERE id = ?`, receipt.InstructionID).Scan(&globalSessionID); err != nil {
		t.Fatalf("resolve compatibility session: %v", err)
	}
	localSessionID, err := projectScopedSessionIDFromGlobal(globalSessionID, 1)
	if err != nil {
		t.Fatalf("map session: %v", err)
	}
	authorizedRole = "netrunner"
	if _, _, err := CheckoutTask(ctx, nil, CheckoutTaskInput{SessionId: localSessionID}); err != nil {
		t.Fatalf("checkout compatibility session: %v", err)
	}
	var instructionState, generationState, sessionState string
	if err := db.QueryRow(`SELECT state FROM hands_instruction WHERE id = ?`, receipt.InstructionID).Scan(&instructionState); err != nil {
		t.Fatalf("read running instruction: %v", err)
	}
	if err := db.QueryRow(`SELECT status FROM hands_generation WHERE instruction_id = ? AND generation = 1`, receipt.InstructionID).Scan(&generationState); err != nil {
		t.Fatalf("read running generation: %v", err)
	}
	if instructionState != "running" || generationState != "running" {
		t.Fatalf("checkout did not atomically start instruction: instruction=%s generation=%s", instructionState, generationState)
	}
	var providerProcessID int
	var providerProcessIdentity string
	if err := db.QueryRow(`SELECT process_id, process_start_identity FROM hands_generation WHERE instruction_id = ? AND generation = 1`, receipt.InstructionID).Scan(&providerProcessID, &providerProcessIdentity); err != nil {
		t.Fatalf("read provider process identity: %v", err)
	}
	if providerProcessID <= 0 || providerProcessIdentity == "" {
		t.Fatalf("checkout must bind an immutable provider process identity: pid=%d identity=%q", providerProcessID, providerProcessIdentity)
	}
	if _, err := db.Exec(`INSERT INTO doc_proposal (project_id, session_id, status, proposed_content) VALUES (1, ?, 'pending', 'No canonical doc impact.')`, globalSessionID); err != nil {
		t.Fatalf("seed mandatory doc proposal: %v", err)
	}
	if _, _, err := CompleteTask(ctx, nil, CompleteTaskInput{SessionId: localSessionID, FinalReport: structuredTestFinalReport}); err != nil {
		t.Fatalf("complete compatibility session: %v", err)
	}
	if err := db.QueryRow(`SELECT state FROM hands_instruction WHERE id = ?`, receipt.InstructionID).Scan(&instructionState); err != nil {
		t.Fatalf("read completed instruction: %v", err)
	}
	if err := db.QueryRow(`SELECT status FROM session WHERE id = ?`, globalSessionID).Scan(&sessionState); err != nil {
		t.Fatalf("read completed session: %v", err)
	}
	if instructionState != "completed" || sessionState != "completed" {
		t.Fatalf("read-only completion mismatch: instruction=%s session=%s", instructionState, sessionState)
	}
}

func TestHandsRepositoryWriteLeaseAndReviewLifecycle(t *testing.T) {
	setupWorkroomHandlerTestDB(t)
	ctx := context.Background()
	gitProject := t.TempDir()
	if err := os.Mkdir(filepath.Join(gitProject, ".git"), 0o755); err != nil {
		t.Fatalf("create git marker: %v", err)
	}
	if _, err := db.Exec(`UPDATE project SET cwd = ? WHERE id = 1`, gitProject); err != nil {
		t.Fatalf("set git project cwd: %v", err)
	}
	_, receipt, err := SubmitHandsInstruction(ctx, nil, SubmitHandsInstructionInput{
		InstructionText:    "Change the governed bridge and verify it.",
		DeclaredWriteScope: []string{"fixer_mcp/dashboard_api"}, RequestedLane: "codex", IdempotencyKey: "write-lifecycle-1",
	})
	if err != nil {
		t.Fatalf("submit repository instruction: %v", err)
	}
	if receipt.State != "queued" || receipt.RiskClass != "repository_write" {
		t.Fatalf("unexpected repository receipt: %+v", receipt)
	}
	var globalSessionID int
	if err := db.QueryRow(`SELECT compat_session_id FROM hands_instruction WHERE id = ?`, receipt.InstructionID).Scan(&globalSessionID); err != nil {
		t.Fatalf("resolve compatibility session: %v", err)
	}
	localSessionID, err := projectScopedSessionIDFromGlobal(globalSessionID, 1)
	if err != nil {
		t.Fatalf("map compatibility session: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO project_write_lease (
			id, project_id, lease_set_id, owner_kind, owner_id, scope_path, fencing_token,
			state, binary_build_id, binary_epoch, created_at, heartbeat_at
		) VALUES ('conflict', 1, 'wave-set', 'wave_worker', 'worker-1', 'fixer_mcp', 1,
		          'active', 'test-build', 0, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`); err != nil {
		t.Fatalf("seed conflicting shared lease: %v", err)
	}
	authorizedRole = "netrunner"
	if _, _, err := CheckoutTask(ctx, nil, CheckoutTaskInput{SessionId: localSessionID}); err == nil {
		t.Fatal("expected checkout to wait for overlapping shared lease")
	}
	var state, sessionStatus string
	if err := db.QueryRow(`SELECT state FROM hands_instruction WHERE id = ?`, receipt.InstructionID).Scan(&state); err != nil {
		t.Fatalf("read waiting state: %v", err)
	}
	if err := db.QueryRow(`SELECT status FROM session WHERE id = ?`, globalSessionID).Scan(&sessionStatus); err != nil {
		t.Fatalf("read waiting session: %v", err)
	}
	if state != "waiting_for_lease" || sessionStatus != "pending" {
		t.Fatalf("lease denial must preserve pending execution: instruction=%s session=%s", state, sessionStatus)
	}
	if _, err := db.Exec(`UPDATE project_write_lease SET state = 'released', released_at = CURRENT_TIMESTAMP WHERE id = 'conflict'`); err != nil {
		t.Fatalf("release conflicting lease: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO mcp_binary_state (project_id, running_build_epoch) VALUES (1, 42)`); err != nil {
		t.Fatalf("seed current binary epoch: %v", err)
	}
	if _, _, err := CheckoutTask(ctx, nil, CheckoutTaskInput{SessionId: localSessionID}); err != nil {
		t.Fatalf("retry checkout after release: %v", err)
	}
	var activeLeaseCount, generationBinaryEpoch, leaseBinaryEpoch int
	var leaseSetID string
	if err := db.QueryRow(`SELECT COUNT(*) FROM project_write_lease WHERE owner_id = ? AND state = 'active'`, receipt.InstructionID).Scan(&activeLeaseCount); err != nil {
		t.Fatalf("count active Hands leases: %v", err)
	}
	if activeLeaseCount != 1 {
		t.Fatalf("expected one active scoped Hands lease, got %d", activeLeaseCount)
	}
	if err := db.QueryRow(`SELECT binary_epoch, lease_set_id FROM hands_generation WHERE instruction_id = ? AND generation = 1`, receipt.InstructionID).Scan(&generationBinaryEpoch, &leaseSetID); err != nil {
		t.Fatalf("read generation fencing metadata: %v", err)
	}
	if err := db.QueryRow(`SELECT binary_epoch FROM project_write_lease WHERE lease_set_id = ? AND state = 'active'`, leaseSetID).Scan(&leaseBinaryEpoch); err != nil {
		t.Fatalf("read lease binary epoch: %v", err)
	}
	if generationBinaryEpoch != 42 || leaseBinaryEpoch != 42 {
		t.Fatalf("generation and lease must preserve the current binary epoch: generation=%d lease=%d", generationBinaryEpoch, leaseBinaryEpoch)
	}
	if _, err := db.Exec(`INSERT INTO doc_proposal (project_id, session_id, status, proposed_content) VALUES (1, ?, 'pending', 'Bridge contract remains current.')`, globalSessionID); err != nil {
		t.Fatalf("seed doc proposal: %v", err)
	}
	if _, _, err := CompleteTask(ctx, nil, CompleteTaskInput{SessionId: localSessionID, FinalReport: structuredTestFinalReport}); err != nil {
		t.Fatalf("complete repository generation: %v", err)
	}
	if err := db.QueryRow(`SELECT state FROM hands_instruction WHERE id = ?`, receipt.InstructionID).Scan(&state); err != nil {
		t.Fatalf("read review state: %v", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM project_write_lease WHERE owner_id = ? AND state = 'active'`, receipt.InstructionID).Scan(&activeLeaseCount); err != nil {
		t.Fatalf("recount active leases: %v", err)
	}
	if state != "awaiting_review" || activeLeaseCount != 0 {
		t.Fatalf("report must enter review and release execution leases: state=%s leases=%d", state, activeLeaseCount)
	}
	var leaseEventCount, leaseRevisionCount int
	if err := db.QueryRow(`SELECT COUNT(*), COUNT(DISTINCT aggregate_revision) FROM project_ui_event WHERE project_id = 1 AND aggregate_type = 'project_write_lease' AND aggregate_id = ?`, leaseSetID).Scan(&leaseEventCount, &leaseRevisionCount); err != nil {
		t.Fatalf("inspect lease journal revisions: %v", err)
	}
	if leaseEventCount != 2 || leaseRevisionCount != 2 {
		t.Fatalf("lease acquire/release must use monotonic aggregate revisions: events=%d revisions=%d", leaseEventCount, leaseRevisionCount)
	}
	authorizedRole = "fixer"
	_, rework, err := ReviewHandsInstruction(ctx, nil, ReviewHandsInstructionInput{
		InstructionID: receipt.InstructionID, Decision: "request_changes", ReviewNote: "Add the reconnect case.", IdempotencyKey: "review-rework-1",
	})
	if err != nil {
		t.Fatalf("request changes: %v", err)
	}
	if rework.State != "queued" {
		t.Fatalf("expected queued rework, got %+v", rework)
	}
	var generationCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM hands_generation WHERE instruction_id = ?`, receipt.InstructionID).Scan(&generationCount); err != nil {
		t.Fatalf("count generations: %v", err)
	}
	if generationCount != 2 {
		t.Fatalf("request changes must create generation two, got %d", generationCount)
	}
	// Drive generation two through the same compatibility projection and accept it.
	authorizedRole = "netrunner"
	if _, _, err := CheckoutTask(ctx, nil, CheckoutTaskInput{SessionId: localSessionID}); err != nil {
		t.Fatalf("checkout rework generation: %v", err)
	}
	if _, _, err := CompleteTask(ctx, nil, CompleteTaskInput{SessionId: localSessionID, FinalReport: structuredTestFinalReport}); err != nil {
		t.Fatalf("complete rework generation: %v", err)
	}
	authorizedRole = "fixer"
	_, accepted, err := ReviewHandsInstruction(ctx, nil, ReviewHandsInstructionInput{
		InstructionID: receipt.InstructionID, Decision: "accept", ReviewNote: "Verified.", IdempotencyKey: "review-accept-2",
	})
	if err != nil {
		t.Fatalf("accept instruction: %v", err)
	}
	if accepted.State != "completed" {
		t.Fatalf("expected completed instruction, got %+v", accepted)
	}
}
