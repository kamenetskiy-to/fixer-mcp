package main

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

func setupCancelWaveTest(t *testing.T) (string, *sql.DB, CreateNetrunnerWaveOutput) {
	t.Helper()
	repoDir := setupCleanGitRepo(t)
	testDB := setupParallelWaveTestDB(t, repoDir)
	t.Cleanup(func() { _ = testDB.Close() })
	db, authorizedRole, authorizedProjectId = testDB, "fixer", 1
	created := createLaunchableTestWave(t, testDB)
	return repoDir, testDB, created
}

// An initialized, never-launched orphan wave whose sessions are already
// completed must be retirable with an explicit audit reason — and the call must
// be idempotent.
func TestCancelNetrunnerWaveRetiresInitializedOrphanWave(t *testing.T) {
	originalDB, originalRole, originalProjectID := db, authorizedRole, authorizedProjectId
	defer func() {
		db, authorizedRole, authorizedProjectId = originalDB, originalRole, originalProjectID
	}()
	_, testDB, created := setupCancelWaveTest(t)

	// The sessions were completed through other means; only the orphan wave
	// rows remain.
	for _, worker := range created.Workers {
		globalSessionID, err := globalSessionIDFromProjectScoped(worker.SessionId, 1)
		if err != nil {
			t.Fatalf("map session: %v", err)
		}
		if _, err := testDB.Exec("UPDATE session SET status = 'completed' WHERE id = ?", globalSessionID); err != nil {
			t.Fatalf("mark session completed: %v", err)
		}
	}

	callResult, output, err := CancelNetrunnerWave(context.Background(), nil, CancelNetrunnerWaveInput{
		WaveId: created.WaveId,
		Reason: "orphan wave retired: sessions completed out-of-band",
	})
	if err != nil || callResult != nil {
		t.Fatalf("orphan wave must be retirable: result=%+v err=%v", callResult, err)
	}
	if output.Mode != cancelNetrunnerWaveModeRetire || output.CancelledWorkers != 2 {
		t.Fatalf("unexpected retire result: %+v", output)
	}
	if output.Wave.Status != parallelWaveStatusCancelled || !strings.HasPrefix(output.AuditReason, "cancelled: ") {
		t.Fatalf("expected a labelled cancelled wave, got %+v", output.Wave)
	}
	for _, worker := range output.Wave.Workers {
		if worker.Status != parallelWaveWorkerStatusCancelled {
			t.Fatalf("all workers must end cancelled, got %+v", worker)
		}
	}

	// Idempotence: a repeated call reports already-cancelled and rewrites
	// nothing.
	_, again, err := CancelNetrunnerWave(context.Background(), nil, CancelNetrunnerWaveInput{
		WaveId: created.WaveId,
		Reason: "orphan wave retired: sessions completed out-of-band",
	})
	if err != nil {
		t.Fatalf("repeated cancel must be idempotent: %v", err)
	}
	if !again.AlreadyCancelled || again.Mode != cancelNetrunnerWaveModeAlreadyCancelled {
		t.Fatalf("expected already-cancelled idempotence, got %+v", again)
	}

	// The cancelled wave released its session governance: the linked session
	// is manageable again.
	if _, _, err := SetSessionStatus(context.Background(), nil, SetSessionStatusInput{
		SessionId: created.Workers[0].SessionId,
		Status:    "in_progress",
		Reason:    "resume after governed cancel",
	}); err != nil {
		t.Fatalf("a cancelled wave must release session governance: %v", err)
	}
}

// Paused / retry_wait waves are cancellable only when provably safe.
func TestCancelNetrunnerWaveCancelsPausedRetryWaitWave(t *testing.T) {
	originalDB, originalRole, originalProjectID, originalExecCommand := db, authorizedRole, authorizedProjectId, execCommand
	originalQuotaGate := DefaultQuotaGate
	defer func() {
		db, authorizedRole, authorizedProjectId, execCommand = originalDB, originalRole, originalProjectID, originalExecCommand
		DefaultQuotaGate = originalQuotaGate
	}()
	DefaultQuotaGate = nil

	repoDir := setupCleanGitRepo(t)
	testDB := setupParallelWaveTestDB(t, repoDir)
	defer testDB.Close()
	db, authorizedRole, authorizedProjectId = testDB, "fixer", 1
	created := createLaunchableTestWave(t, testDB)
	installFakeWaveWorkerLauncher(t, "", nil)
	if _, _, err := LaunchNetrunnerWave(context.Background(), nil, LaunchNetrunnerWaveInput{WaveId: created.WaveId, TimeoutSeconds: 1}); err != nil {
		t.Fatalf("launch wave: %v", err)
	}
	wave, err := fetchNetrunnerWaveSnapshot(created.WaveId, 1)
	if err != nil {
		t.Fatalf("fetch wave: %v", err)
	}
	for _, worker := range wave.Workers {
		if _, err := testDB.Exec(
			"UPDATE parallel_wave_worker SET status = ?, retry_cause = 'provider_rate_limit', retry_next_eligible_at = '' WHERE id = ?",
			parallelWaveWorkerStatusRetryWait, worker.Id,
		); err != nil {
			t.Fatalf("stage retry_wait worker: %v", err)
		}
		globalSessionID, err := globalSessionIDFromProjectScoped(worker.SessionId, 1)
		if err != nil {
			t.Fatalf("map session: %v", err)
		}
		markFakeWaveWorkerExited(t, testDB, created.WaveId, globalSessionID)
	}

	callResult, output, err := CancelNetrunnerWave(context.Background(), nil, CancelNetrunnerWaveInput{
		WaveId: created.WaveId,
		Reason: "operator cancelled stuck retry_wait wave 0000",
	})
	if err != nil || callResult != nil {
		t.Fatalf("paused/retry_wait wave must be cancellable when safe: result=%+v err=%v", callResult, err)
	}
	if output.Mode != cancelNetrunnerWaveModeCancel || output.CancelledWorkers != 2 {
		t.Fatalf("unexpected cancel result: %+v", output)
	}
	for _, worker := range output.Wave.Workers {
		if worker.Status != parallelWaveWorkerStatusCancelled || !strings.Contains(worker.FailureReason, "operator cancelled") {
			t.Fatalf("cancelled workers must carry the audit reason, got %+v", worker)
		}
	}
	// The wave aggregate must stay cancelled, not resurrect.
	if err := refreshParallelWaveAggregateStatus(created.WaveId, 1); err != nil {
		t.Fatalf("refresh aggregate: %v", err)
	}
	refreshed, err := fetchNetrunnerWaveSnapshot(created.WaveId, 1)
	if err != nil {
		t.Fatalf("refresh wave: %v", err)
	}
	if refreshed.Status != parallelWaveStatusCancelled {
		t.Fatalf("aggregate refresh must not resurrect a cancelled wave, got %q", refreshed.Status)
	}
}

// Cancellation never kills processes: a live worker process blocks it.
func TestCancelNetrunnerWaveRefusesLiveWorkerProcess(t *testing.T) {
	originalDB, originalRole, originalProjectID := db, authorizedRole, authorizedProjectId
	defer func() {
		db, authorizedRole, authorizedProjectId = originalDB, originalRole, originalProjectID
	}()
	repoDir, testDB, created := setupCancelWaveTest(t)
	_ = markTestWaveRunningWithWorktrees(t, testDB, repoDir, created)
	if _, err := testDB.Exec("UPDATE parallel_wave SET control_state = ? WHERE id = ?", parallelWaveControlPausedForArchitect, created.WaveId); err != nil {
		t.Fatalf("pause wave: %v", err)
	}

	callResult, _, err := CancelNetrunnerWave(context.Background(), nil, CancelNetrunnerWaveInput{
		WaveId: created.WaveId,
		Reason: "attempt to cancel a wave with live worker processes",
	})
	if err == nil || callResult == nil {
		t.Fatalf("a live worker process must block cancellation, got result=%+v", callResult)
	}
	if !strings.Contains(err.Error(), "live process") {
		t.Fatalf("expected a live-process refusal, got %v", err)
	}
}

// The audit reason is mandatory; unattributable waves are refused with the
// concrete reason instead of being silently cancelled.
func TestCancelNetrunnerWaveRequiresReasonAndCancellableShape(t *testing.T) {
	originalDB, originalRole, originalProjectID := db, authorizedRole, authorizedProjectId
	defer func() {
		db, authorizedRole, authorizedProjectId = originalDB, originalRole, originalProjectID
	}()
	repoDir, testDB, created := setupCancelWaveTest(t)

	if callResult, _, err := CancelNetrunnerWave(context.Background(), nil, CancelNetrunnerWaveInput{
		WaveId: created.WaveId,
		Reason: "short",
	}); err == nil || callResult == nil || !strings.Contains(err.Error(), "audit reason") {
		t.Fatalf("a short reason must be refused, got %+v %v", callResult, err)
	}

	// Never-launched but sessions still pending: not an orphan shape.
	if callResult, _, err := CancelNetrunnerWave(context.Background(), nil, CancelNetrunnerWaveInput{
		WaveId: created.WaveId,
		Reason: "cancel a never-launched wave with pending sessions",
	}); err == nil || callResult == nil || !strings.Contains(err.Error(), "not all completed") {
		t.Fatalf("a non-orphan never-launched wave must be refused with the concrete reason, got %+v %v", callResult, err)
	}

	// Running and unpaused without retry_wait workers: not cancellable.
	_ = markTestWaveRunningWithWorktrees(t, testDB, repoDir, created)
	if callResult, _, err := CancelNetrunnerWave(context.Background(), nil, CancelNetrunnerWaveInput{
		WaveId: created.WaveId,
		Reason: "cancel a healthy running wave out of policy",
	}); err == nil || callResult == nil || !strings.Contains(err.Error(), "not in a cancellable state") {
		t.Fatalf("a healthy running wave must be refused, got %+v %v", callResult, err)
	}
}
