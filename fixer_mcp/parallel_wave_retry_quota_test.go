package main

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeQuotaGate is a deterministic QuotaGate for retry-scheduler tests; live
// account quotas belong to the quota gate's own test surface.
type fakeQuotaGate struct {
	quota ProviderQuota
	found bool
	calls int
}

func (g *fakeQuotaGate) CheckQuota(string) (ProviderQuota, bool, error) {
	g.calls++
	return g.quota, g.found, nil
}

func mustGlobalSessionID(t *testing.T, localSessionID int) int {
	t.Helper()
	globalSessionID, err := globalSessionIDFromProjectScoped(localSessionID, 1)
	if err != nil {
		t.Fatalf("map session %d: %v", localSessionID, err)
	}
	return globalSessionID
}

func setupRetrySchedulerTest(t *testing.T) (string, *sql.DB, NetrunnerWaveSnapshot, NetrunnerWaveWorkerSnapshot) {
	t.Helper()
	originalDB, originalRole, originalProjectID, originalExecCommand := db, authorizedRole, authorizedProjectId, execCommand
	originalQuotaGate := DefaultQuotaGate
	t.Cleanup(func() {
		db, authorizedRole, authorizedProjectId, execCommand = originalDB, originalRole, originalProjectID, originalExecCommand
		DefaultQuotaGate = originalQuotaGate
	})
	DefaultQuotaGate = nil
	t.Setenv("FIXER_MCP_TELEGRAM_BOT_TOKEN", "")
	t.Setenv("FIXER_MCP_TELEGRAM_CHAT_ID", "")

	repoDir := setupCleanGitRepo(t)
	testDB := setupParallelWaveTestDB(t, repoDir)
	t.Cleanup(func() { _ = testDB.Close() })
	db, authorizedRole, authorizedProjectId = testDB, "fixer", 1

	created := createLaunchableTestWave(t, testDB)
	installFakeWaveWorkerLauncher(t, "", nil)
	if _, _, err := LaunchNetrunnerWave(context.Background(), nil, LaunchNetrunnerWaveInput{WaveId: created.WaveId, TimeoutSeconds: 1}); err != nil {
		t.Fatalf("launch retry test wave: %v", err)
	}
	wave, err := fetchNetrunnerWaveSnapshot(created.WaveId, 1)
	if err != nil {
		t.Fatalf("fetch launched wave: %v", err)
	}
	return repoDir, testDB, wave, wave.Workers[0]
}

// stageRetryWaitWorker puts one worker into retry_wait with the given
// eligibility, makes its previous attempt's process a dead historical row, and
// returns the refreshed wave snapshot the retry scheduler must consume.
func stageRetryWaitWorker(t *testing.T, testDB *sql.DB, wave NetrunnerWaveSnapshot, worker NetrunnerWaveWorkerSnapshot, eligibleAt string, attemptCount int) NetrunnerWaveSnapshot {
	t.Helper()
	if _, err := testDB.Exec(
		"UPDATE parallel_wave_worker SET status = ?, retry_cause = 'provider_rate_limit', retry_attempt_count = ?, retry_next_eligible_at = ? WHERE id = ?",
		parallelWaveWorkerStatusRetryWait,
		attemptCount,
		eligibleAt,
		worker.Id,
	); err != nil {
		t.Fatalf("seed retry_wait worker: %v", err)
	}
	markFakeWaveWorkerExited(t, testDB, wave.Id, mustGlobalSessionID(t, worker.SessionId))
	refreshed, err := fetchNetrunnerWaveSnapshot(wave.Id, 1)
	if err != nil {
		t.Fatalf("refresh staged wave: %v", err)
	}
	return refreshed
}

func readRetryWorkerRow(t *testing.T, testDB *sql.DB, workerID int) (status string, attempts int, cause string, eligibleAt string, failureReason string) {
	t.Helper()
	if err := testDB.QueryRow(
		"SELECT status, retry_attempt_count, COALESCE(retry_cause, ''), COALESCE(retry_next_eligible_at, ''), COALESCE(failure_reason, '') FROM parallel_wave_worker WHERE id = ?",
		workerID,
	).Scan(&status, &attempts, &cause, &eligibleAt, &failureReason); err != nil {
		t.Fatalf("read retry worker row: %v", err)
	}
	return status, attempts, cause, eligibleAt, failureReason
}

// A fractional (non-zero) quota verdict must never block a relaunch: the 114
// repro where "50.0% left" parsed as 0 must be gone.
func TestProcessParallelWaveWorkerRetriesFractionalQuotaRelaunches(t *testing.T) {
	repoDir, testDB, wave, worker := setupRetrySchedulerTest(t)
	wave = stageRetryWaitWorker(t, testDB, wave, worker, "", 0)
	DefaultQuotaGate = &fakeQuotaGate{
		quota: ProviderQuota{PercentLeft: 50, ResetDelay: 2 * time.Hour, ResetKnown: true, Window: "7d"},
		found: true,
	}

	if err := processParallelWaveWorkerRetries(context.Background(), repoDir, wave, time.Second); err != nil {
		t.Fatalf("process retries: %v", err)
	}
	status, attempts, _, eligibleAt, _ := readRetryWorkerRow(t, testDB, worker.Id)
	if status != parallelWaveWorkerStatusRunning || attempts != 1 {
		t.Fatalf("fractional quota must relaunch immediately, got status=%s attempts=%d", status, attempts)
	}
	if strings.TrimSpace(eligibleAt) != "" {
		t.Fatalf("a consumed claim must clear eligibility, got %q", eligibleAt)
	}
}

// Genuine exhaustion (real 0%) must persist a STABLE quota eligibility: the
// scheduler pass writes the deadline once and never slides it forward on
// repeated passes.
func TestProcessParallelWaveWorkerRetriesQuotaEligibilityIsPersistedAndStable(t *testing.T) {
	repoDir, testDB, wave, worker := setupRetrySchedulerTest(t)
	wave = stageRetryWaitWorker(t, testDB, wave, worker, "", 0)
	DefaultQuotaGate = &fakeQuotaGate{
		quota: ProviderQuota{PercentLeft: 0, ResetDelay: 2 * time.Hour, ResetKnown: true, Window: "7d"},
		found: true,
	}

	if err := processParallelWaveWorkerRetries(context.Background(), repoDir, wave, time.Second); err != nil {
		t.Fatalf("process retries: %v", err)
	}
	status, attempts, cause, eligibleAt, failureReason := readRetryWorkerRow(t, testDB, worker.Id)
	if status != parallelWaveWorkerStatusRetryWait || attempts != 0 {
		t.Fatalf("exhausted quota must not consume an attempt, got status=%s attempts=%d", status, attempts)
	}
	if cause != parallelWaveRetryCauseQuotaExhausted {
		t.Fatalf("expected quota retry cause, got %q", cause)
	}
	if !strings.Contains(failureReason, "quota exhausted") {
		t.Fatalf("expected an explicit quota blocking reason, got %q", failureReason)
	}
	due := parseParallelWaveRetryEligibility(eligibleAt)
	if due.IsZero() {
		t.Fatalf("expected persisted quota eligibility, got %q", eligibleAt)
	}
	now := time.Now().UTC()
	if due.Before(now.Add(1*time.Hour)) || due.After(now.Add(4*time.Hour)) {
		t.Fatalf("expected ~2h quota reset deadline, got %v (%s)", due, eligibleAt)
	}

	// Repeated scheduler passes must not slide the deadline forward.
	for i := 0; i < 3; i++ {
		time.Sleep(20 * time.Millisecond)
		refreshed, err := fetchNetrunnerWaveSnapshot(wave.Id, 1)
		if err != nil {
			t.Fatalf("refresh wave: %v", err)
		}
		if err := processParallelWaveWorkerRetries(context.Background(), repoDir, refreshed, time.Second); err != nil {
			t.Fatalf("repeat retry pass: %v", err)
		}
	}
	_, _, _, eligibleAfter, _ := readRetryWorkerRow(t, testDB, worker.Id)
	if eligibleAfter != eligibleAt {
		t.Fatalf("quota eligibility must stay stable across passes: first=%q later=%q", eligibleAt, eligibleAfter)
	}
}

// A genuine exhaustion with an unknown reset must not invent a long quota
// wait: the plain backoff decides.
func TestProcessParallelWaveWorkerRetriesUnknownResetUsesPlainBackoff(t *testing.T) {
	repoDir, testDB, wave, worker := setupRetrySchedulerTest(t)
	wave = stageRetryWaitWorker(t, testDB, wave, worker, "", 0)
	DefaultQuotaGate = &fakeQuotaGate{
		quota: ProviderQuota{PercentLeft: 0, ResetKnown: false},
		found: true,
	}

	if err := processParallelWaveWorkerRetries(context.Background(), repoDir, wave, time.Second); err != nil {
		t.Fatalf("process retries: %v", err)
	}
	status, attempts, _, eligibleAt, _ := readRetryWorkerRow(t, testDB, worker.Id)
	if status != parallelWaveWorkerStatusRetryWait || attempts != 0 {
		t.Fatalf("unexpected claim: status=%s attempts=%d", status, attempts)
	}
	due := parseParallelWaveRetryEligibility(eligibleAt)
	if due.IsZero() {
		t.Fatalf("expected a persisted backoff deadline, got %q", eligibleAt)
	}
	if due.After(time.Now().UTC().Add(calculateBackoff(0) + 10*time.Minute)) {
		t.Fatalf("unknown-reset exhaustion must fall back to plain backoff, got %v", due)
	}
}

// Missing/malformed quota verdicts are never invented exhaustion: the
// immediately eligible worker relaunches.
func TestProcessParallelWaveWorkerRetriesMissingQuotaRelaunchesImmediately(t *testing.T) {
	repoDir, testDB, wave, worker := setupRetrySchedulerTest(t)
	wave = stageRetryWaitWorker(t, testDB, wave, worker, "", 0)
	DefaultQuotaGate = &fakeQuotaGate{found: false}

	if err := processParallelWaveWorkerRetries(context.Background(), repoDir, wave, time.Second); err != nil {
		t.Fatalf("process retries: %v", err)
	}
	status, attempts, _, _, _ := readRetryWorkerRow(t, testDB, worker.Id)
	if status != parallelWaveWorkerStatusRunning || attempts != 1 {
		t.Fatalf("missing quota must not block the immediate requeue, got status=%s attempts=%d", status, attempts)
	}
}

// Expired future eligibility is immediate; future eligibility is respected.
func TestProcessParallelWaveWorkerRetriesRespectsFutureAndExpiredEligibility(t *testing.T) {
	repoDir, testDB, wave, worker := setupRetrySchedulerTest(t)

	t.Run("future eligibility waits", func(t *testing.T) {
		staged := stageRetryWaitWorker(t, testDB, wave, worker, time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano), 0)
		if err := processParallelWaveWorkerRetries(context.Background(), repoDir, staged, time.Second); err != nil {
			t.Fatalf("process retries: %v", err)
		}
		status, attempts, _, _, _ := readRetryWorkerRow(t, testDB, worker.Id)
		if status != parallelWaveWorkerStatusRetryWait || attempts != 0 {
			t.Fatalf("future eligibility must not be consumed, got status=%s attempts=%d", status, attempts)
		}
	})

	t.Run("expired eligibility relaunches", func(t *testing.T) {
		staged := stageRetryWaitWorker(t, testDB, wave, worker, time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano), 0)
		if err := processParallelWaveWorkerRetries(context.Background(), repoDir, staged, time.Second); err != nil {
			t.Fatalf("process retries: %v", err)
		}
		status, attempts, _, _, _ := readRetryWorkerRow(t, testDB, worker.Id)
		if status != parallelWaveWorkerStatusRunning || attempts != 1 {
			t.Fatalf("expired eligibility must relaunch immediately, got status=%s attempts=%d", status, attempts)
		}
	})
}

// Concurrent retry passes must consume exactly one attempt via the CAS claim.
func TestProcessParallelWaveWorkerRetriesConcurrentClaimExactlyOneAttempt(t *testing.T) {
	repoDir, testDB, wave, worker := setupRetrySchedulerTest(t)
	stageRetryWaitWorker(t, testDB, wave, worker, time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano), 0)

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			snapshot, err := fetchNetrunnerWaveSnapshot(wave.Id, 1)
			if err != nil {
				return
			}
			_ = processParallelWaveWorkerRetries(context.Background(), repoDir, snapshot, time.Second)
		}()
	}
	wg.Wait()

	status, attempts, _, _, _ := readRetryWorkerRow(t, testDB, worker.Id)
	if attempts != 1 {
		t.Fatalf("the CAS claim must consume exactly one attempt, got %d (status=%s)", attempts, status)
	}
	var processCount int
	if err := testDB.QueryRow("SELECT COUNT(*) FROM worker_process WHERE parallel_wave_worker_id = ?", worker.Id).Scan(&processCount); err != nil {
		t.Fatalf("count worker processes: %v", err)
	}
	if processCount != 2 {
		t.Fatalf("expected initial launch plus exactly one retry launch, got %d rows", processCount)
	}
}

// The retry budget is honest: a worker at the budget limit becomes blocked,
// never silently relaunched or fake-accepted.
func TestProcessParallelWaveWorkerRetriesBlocksAtMaxAttempts(t *testing.T) {
	repoDir, testDB, wave, worker := setupRetrySchedulerTest(t)
	wave = stageRetryWaitWorker(t, testDB, wave, worker, time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano), maxParallelWaveRetryAttempts)

	if err := processParallelWaveWorkerRetries(context.Background(), repoDir, wave, time.Second); err != nil {
		t.Fatalf("process retries: %v", err)
	}
	status, attempts, _, _, failureReason := readRetryWorkerRow(t, testDB, worker.Id)
	if status != parallelWaveWorkerStatusBlocked {
		t.Fatalf("expected blocked at retry budget, got status=%s attempts=%d (%q)", status, attempts, failureReason)
	}
}

// Manual rework must stay immediate and deliver the FRESHLY UPDATED task
// exactly once: the old process row is cleared by the governed requeue, the
// updated instructions are in the session row at relaunch time, and the
// scheduler consumes exactly one attempt.
func TestProcessParallelWaveWorkerRetriesRelaunchesFreshUpdatedInstructions(t *testing.T) {
	repoDir, testDB, wave, worker := setupRetrySchedulerTest(t)

	// The previous attempt finished; the Fixer rejects it and appends fresh
	// rework instructions before the relaunch.
	if _, err := testDB.Exec(
		"UPDATE parallel_wave_worker SET status = ?, terminal_at = CURRENT_TIMESTAMP WHERE id = ?",
		parallelWaveWorkerStatusReviewReady, worker.Id,
	); err != nil {
		t.Fatalf("mark worker review_ready: %v", err)
	}
	globalSessionID, err := globalSessionIDFromProjectScoped(worker.SessionId, 1)
	if err != nil {
		t.Fatalf("map session: %v", err)
	}
	if _, err := testDB.Exec("UPDATE session SET status = 'review' WHERE id = ?", globalSessionID); err != nil {
		t.Fatalf("mark session review: %v", err)
	}
	if _, _, err := UpdateTask(context.Background(), nil, UpdateTaskInput{
		SessionId:           worker.SessionId,
		AppendedDescription: "REWORK-FRESH-INSTRUCTIONS-42",
	}); err != nil {
		t.Fatalf("append fresh rework instructions: %v", err)
	}
	if _, _, err := SetSessionStatus(context.Background(), nil, SetSessionStatusInput{
		SessionId: worker.SessionId,
		Status:    "pending",
		Reason:    "rejected: rework with fresh instructions",
	}); err != nil {
		t.Fatalf("send rejected work back for rework: %v", err)
	}

	// The requeue clears the old process row and keeps the rework immediate.
	var oldProcessCleared bool
	var eligibility string
	var status string
	if err := testDB.QueryRow(
		"SELECT worker_process_id IS NULL, COALESCE(retry_next_eligible_at, ''), status FROM parallel_wave_worker WHERE id = ?",
		worker.Id,
	).Scan(&oldProcessCleared, &eligibility, &status); err != nil {
		t.Fatalf("read requeued worker: %v", err)
	}
	if !oldProcessCleared || eligibility != "" || status != parallelWaveWorkerStatusRetryWait {
		t.Fatalf("manual rework must be immediate with the old process cleared, got cleared=%v eligibility=%q status=%s", oldProcessCleared, eligibility, status)
	}

	// The updated instructions are durable before the relaunch.
	var description string
	if err := testDB.QueryRow("SELECT task_description FROM session WHERE id = ?", globalSessionID).Scan(&description); err != nil {
		t.Fatalf("read session description: %v", err)
	}
	if !strings.Contains(description, "REWORK-FRESH-INSTRUCTIONS-42") {
		t.Fatalf("expected the freshly updated instructions in the session row, got %q", description)
	}

	refreshed, err := fetchNetrunnerWaveSnapshot(wave.Id, 1)
	if err != nil {
		t.Fatalf("refresh wave: %v", err)
	}
	if err := processParallelWaveWorkerRetries(context.Background(), repoDir, refreshed, time.Second); err != nil {
		t.Fatalf("relaunch reworked worker: %v", err)
	}
	status, attempts, _, _, _ := readRetryWorkerRow(t, testDB, worker.Id)
	if status != parallelWaveWorkerStatusRunning || attempts != 1 {
		t.Fatalf("the fresh instructions must be delivered exactly once, got status=%s attempts=%d", status, attempts)
	}
	var processCount int
	if err := testDB.QueryRow("SELECT COUNT(*) FROM worker_process WHERE parallel_wave_worker_id = ?", worker.Id).Scan(&processCount); err != nil {
		t.Fatalf("count worker processes: %v", err)
	}
	if processCount != 2 {
		t.Fatalf("expected the historical attempt plus exactly one relaunch, got %d rows", processCount)
	}
}
