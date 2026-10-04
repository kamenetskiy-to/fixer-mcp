package main

import (
	"context"
	"strings"
	"testing"
)

// Manual waves close on the Fixer's review attestation. The acceptance and
// completion transitions must never require a fabricated reviewer Netrunner or
// a mandatory acceptance worker — that dead-end registered six times
// (backlog 58/143/205, feedback #8/#12/#47/#93) and must not come back.

func TestManualWaveClosesOnFixerReviewAttestation(t *testing.T) {
	originalDB, originalRole, originalProjectID := db, authorizedRole, authorizedProjectId
	defer func() {
		db, authorizedRole, authorizedProjectId = originalDB, originalRole, originalProjectID
	}()
	repoDir := setupCleanGitRepo(t)
	testDB := setupParallelWaveTestDB(t, repoDir)
	defer testDB.Close()
	db, authorizedRole, authorizedProjectId = testDB, "fixer", 1

	_, created, err := CreateNetrunnerWave(context.Background(), nil, CreateNetrunnerWaveInput{
		SessionIds: []int{1},
		Reason:     "manual acceptance contract",
	})
	if err != nil {
		t.Fatalf("create manual wave: %v", err)
	}
	if _, err := testDB.Exec(
		"UPDATE parallel_wave SET phase = ?, status = ? WHERE id = ?",
		parallelWavePhaseImplementation, parallelWaveStatusCompleted, created.WaveId,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := testDB.Exec(
		"UPDATE parallel_wave_worker SET status = ?, terminal_at = CURRENT_TIMESTAMP WHERE wave_id = ?",
		parallelWaveWorkerStatusCompleted, created.WaveId,
	); err != nil {
		t.Fatal(err)
	}
	globalWorkerID, err := globalSessionIDFromProjectScoped(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := testDB.Exec("UPDATE session SET status = 'review' WHERE id = ?", globalWorkerID); err != nil {
		t.Fatal(err)
	}

	// While the wave is in implementation, the wave owns the session
	// lifecycle and the refusal stays actionable.
	if _, _, err := SetSessionStatus(context.Background(), nil, SetSessionStatusInput{SessionId: 1, Status: "completed"}); err == nil ||
		!strings.Contains(err.Error(), "wave-linked session") ||
		!strings.Contains(err.Error(), "transition_netrunner_wave_phase") {
		t.Fatalf("implementation-phase wave must govern its sessions with an actionable refusal, got err=%v", err)
	}

	// Acceptance without a reviewer Netrunner and without an acceptance
	// worker: the Fixer's attestation carries it.
	callResult, accepted, err := TransitionNetrunnerWavePhase(context.Background(), nil, TransitionNetrunnerWavePhaseInput{
		WaveId:         created.WaveId,
		TargetPhase:    parallelWavePhaseAcceptance,
		ReviewApproved: true,
	})
	if err != nil || callResult != nil {
		t.Fatalf("manual acceptance must not require fabricated sessions: result=%+v err=%v", callResult, err)
	}
	if accepted.Wave.Phase != parallelWavePhaseAcceptance || accepted.Wave.AcceptanceSessionId != 0 {
		t.Fatalf("unexpected acceptance state: %+v", accepted.Wave)
	}

	// At acceptance the Fixer may close reviewed worker sessions.
	if _, _, err := SetSessionStatus(context.Background(), nil, SetSessionStatusInput{
		SessionId: 1,
		Status:    "completed",
		Reason:    "reviewed and accepted",
	}); err != nil {
		t.Fatalf("reviewed worker session must be closable at acceptance: %v", err)
	}

	// The wave itself completes without an acceptance worker.
	callResult, completed, err := TransitionNetrunnerWavePhase(context.Background(), nil, TransitionNetrunnerWavePhaseInput{
		WaveId:         created.WaveId,
		TargetPhase:    parallelWavePhaseCompleted,
		ReviewApproved: true,
	})
	if err != nil || callResult != nil {
		t.Fatalf("manual wave completion failed: result=%+v err=%v", callResult, err)
	}
	if completed.Wave.Phase != parallelWavePhaseCompleted || completed.Wave.GateState != parallelWaveGateClosed {
		t.Fatalf("unexpected completed state: %+v", completed.Wave)
	}
}

func TestManualWaveAcceptanceValidatesAttestationAndAcceptanceSession(t *testing.T) {
	originalDB, originalRole, originalProjectID := db, authorizedRole, authorizedProjectId
	defer func() {
		db, authorizedRole, authorizedProjectId = originalDB, originalRole, originalProjectID
	}()
	repoDir := setupCleanGitRepo(t)
	testDB := setupParallelWaveTestDB(t, repoDir)
	defer testDB.Close()
	db, authorizedRole, authorizedProjectId = testDB, "fixer", 1

	_, created, err := CreateNetrunnerWave(context.Background(), nil, CreateNetrunnerWaveInput{
		SessionIds: []int{1},
	})
	if err != nil {
		t.Fatalf("create manual wave: %v", err)
	}
	if _, err := testDB.Exec(
		"UPDATE parallel_wave SET phase = ?, status = ? WHERE id = ?",
		parallelWavePhaseImplementation, parallelWaveStatusCompleted, created.WaveId,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := testDB.Exec(
		"UPDATE parallel_wave_worker SET status = ?, terminal_at = CURRENT_TIMESTAMP WHERE wave_id = ?",
		parallelWaveWorkerStatusCompleted, created.WaveId,
	); err != nil {
		t.Fatal(err)
	}

	if callResult, _, err := TransitionNetrunnerWavePhase(context.Background(), nil, TransitionNetrunnerWavePhaseInput{
		WaveId:      created.WaveId,
		TargetPhase: parallelWavePhaseAcceptance,
	}); err == nil || callResult == nil || !strings.Contains(err.Error(), "review_approved") {
		t.Fatalf("attestation is still mandatory: result=%+v err=%v", callResult, err)
	}

	// An implementation worker can never double as the acceptance session.
	if callResult, _, err := TransitionNetrunnerWavePhase(context.Background(), nil, TransitionNetrunnerWavePhaseInput{
		WaveId:              created.WaveId,
		TargetPhase:         parallelWavePhaseAcceptance,
		ReviewApproved:      true,
		AcceptanceSessionId: 1,
	}); err == nil || callResult == nil || !strings.Contains(err.Error(), "distinct") {
		t.Fatalf("worker as acceptance session must stay rejected: result=%+v err=%v", callResult, err)
	}
}

func TestAutomaticWaveAcceptanceStillRequiresReviewerAndAcceptanceSession(t *testing.T) {
	originalDB, originalRole, originalProjectID := db, authorizedRole, authorizedProjectId
	defer func() {
		db, authorizedRole, authorizedProjectId = originalDB, originalRole, originalProjectID
	}()
	repoDir := setupCleanGitRepo(t)
	testDB := setupParallelWaveTestDB(t, repoDir)
	defer testDB.Close()
	db, authorizedRole, authorizedProjectId = testDB, "fixer", 1

	_, created, err := CreateNetrunnerWave(context.Background(), nil, CreateNetrunnerWaveInput{
		SessionIds:   []int{1, 2},
		ReviewPolicy: parallelWaveReviewPolicyAutomatic,
	})
	if err != nil {
		t.Fatalf("create automatic wave: %v", err)
	}
	if _, err := testDB.Exec(
		"UPDATE parallel_wave SET phase = ?, status = ? WHERE id = ?",
		parallelWavePhaseImplementation, parallelWaveStatusCompleted, created.WaveId,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := testDB.Exec(
		"UPDATE parallel_wave_worker SET status = ?, terminal_at = CURRENT_TIMESTAMP WHERE wave_id = ?",
		parallelWaveWorkerStatusCompleted, created.WaveId,
	); err != nil {
		t.Fatal(err)
	}

	if callResult, _, err := TransitionNetrunnerWavePhase(context.Background(), nil, TransitionNetrunnerWavePhaseInput{
		WaveId:         created.WaveId,
		TargetPhase:    parallelWavePhaseAcceptance,
		ReviewApproved: true,
	}); err == nil || callResult == nil || !strings.Contains(err.Error(), "implementation reviewer must be completed") {
		t.Fatalf("automatic waves keep the completed-reviewer contract: result=%+v err=%v", callResult, err)
	}

	if _, err := testDB.Exec(
		"INSERT INTO session (project_id, task_description, status, report, parallel_wave_id) VALUES (1, 'reviewer', 'completed', 'approved', ?)",
		parallelWaveReviewMarker(created.WaveId),
	); err != nil {
		t.Fatalf("seed completed reviewer: %v", err)
	}
	if callResult, _, err := TransitionNetrunnerWavePhase(context.Background(), nil, TransitionNetrunnerWavePhaseInput{
		WaveId:         created.WaveId,
		TargetPhase:    parallelWavePhaseAcceptance,
		ReviewApproved: true,
	}); err == nil || callResult == nil || !strings.Contains(err.Error(), "acceptance_session_id is required") {
		t.Fatalf("automatic waves keep the acceptance-session contract: result=%+v err=%v", callResult, err)
	}
}
