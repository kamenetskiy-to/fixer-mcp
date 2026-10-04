package main

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"
)

// A rejected review must have a governed way out — never a fake PASS. Two
// roads exist since fixer 1.0.4 (feedback 95, backlog 206-208):
// rework (review -> pending with the worker requeued for relaunch) and a
// rejected close (the wave ends without attesting anything and releases its
// scope leases).

func TestManualWaveReworkRequeuesTheWorkerForRelaunch(t *testing.T) {
	originalDB, originalRole, originalProjectID := db, authorizedRole, authorizedProjectId
	defer func() {
		db, authorizedRole, authorizedProjectId = originalDB, originalRole, originalProjectID
	}()
	repoDir := setupCleanGitRepo(t)
	testDB := setupParallelWaveTestDB(t, repoDir)
	defer testDB.Close()
	db, authorizedRole, authorizedProjectId = testDB, "fixer", 1

	_, created, err := CreateNetrunnerWave(context.Background(), nil, CreateNetrunnerWaveInput{SessionIds: []int{1}})
	if err != nil {
		t.Fatalf("create manual wave: %v", err)
	}
	if _, err := testDB.Exec(
		"UPDATE parallel_wave SET phase = ? WHERE id = ?",
		parallelWavePhaseImplementation, created.WaveId,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := testDB.Exec(
		"UPDATE parallel_wave_worker SET status = ?, terminal_at = CURRENT_TIMESTAMP WHERE wave_id = ?",
		parallelWaveWorkerStatusReviewReady, created.WaveId,
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

	// The Fixer rejected the delivery and sends it back.
	if _, _, err := SetSessionStatus(context.Background(), nil, SetSessionStatusInput{
		SessionId: 1,
		Status:    "pending",
		Reason:    "rejected: false-green rows, see review notes",
	}); err != nil {
		t.Fatalf("rejected wave work must be returnable for rework: %v", err)
	}

	var rework int
	if err := testDB.QueryRow("SELECT COALESCE(rework_count,0) FROM session WHERE id = ?", globalWorkerID).Scan(&rework); err != nil || rework != 1 {
		t.Fatalf("rework must be counted once, got %d err=%v", rework, err)
	}
	var workerStatus string
	var attempts int
	var terminalCleared bool
	if err := testDB.QueryRow(
		"SELECT status, retry_attempt_count, terminal_at IS NULL FROM parallel_wave_worker WHERE wave_id = ?",
		created.WaveId,
	).Scan(&workerStatus, &attempts, &terminalCleared); err != nil {
		t.Fatal(err)
	}
	if workerStatus != parallelWaveWorkerStatusRetryWait || attempts != 0 || !terminalCleared {
		t.Fatalf("rework must requeue the worker for relaunch, got status=%s attempts=%d terminalCleared=%v", workerStatus, attempts, terminalCleared)
	}

	// The wave still owns every other lifecycle move.
	if _, _, err := SetSessionStatus(context.Background(), nil, SetSessionStatusInput{SessionId: 1, Status: "completed"}); err == nil ||
		!strings.Contains(err.Error(), "wave-linked session") {
		t.Fatalf("completion outside the accepted contract must stay governed, got err=%v", err)
	}
}

func TestManualWaveRejectedReviewClosesWithoutAttesting(t *testing.T) {
	originalDB, originalRole, originalProjectID := db, authorizedRole, authorizedProjectId
	defer func() {
		db, authorizedRole, authorizedProjectId = originalDB, originalRole, originalProjectID
	}()
	repoDir := setupCleanGitRepo(t)
	testDB := setupParallelWaveTestDB(t, repoDir)
	defer testDB.Close()
	db, authorizedRole, authorizedProjectId = testDB, "fixer", 1

	_, created, err := CreateNetrunnerWave(context.Background(), nil, CreateNetrunnerWaveInput{SessionIds: []int{1}})
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
		parallelWaveWorkerStatusReviewReady, created.WaveId,
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

	// An accepted completion still requires the acceptance phase.
	if callResult, _, err := TransitionNetrunnerWavePhase(context.Background(), nil, TransitionNetrunnerWavePhaseInput{
		WaveId:         created.WaveId,
		TargetPhase:    parallelWavePhaseCompleted,
		ReviewApproved: true,
	}); err == nil || callResult == nil || !strings.Contains(err.Error(), "must be in acceptance phase") {
		t.Fatalf("accepted completion must keep its phase contract: result=%+v err=%v", callResult, err)
	}

	// A rejected review closes the wave without attesting anything.
	callResult, closed, err := TransitionNetrunnerWavePhase(context.Background(), nil, TransitionNetrunnerWavePhaseInput{
		WaveId:         created.WaveId,
		TargetPhase:    parallelWavePhaseCompleted,
		ReviewApproved: true,
		ReviewOutcome:  "rejected",
	})
	if err != nil || callResult != nil {
		t.Fatalf("rejected review must close the wave: result=%+v err=%v", callResult, err)
	}
	if closed.Wave.Phase != parallelWavePhaseCompleted || closed.Wave.GateState != parallelWaveGateClosed {
		t.Fatalf("unexpected closed state: %+v", closed.Wave)
	}
	if !strings.Contains(closed.Wave.FailureReason, "rejected review") {
		t.Fatalf("a rejected close must be labelled as such, got %q", closed.Wave.FailureReason)
	}
	// Nothing may read as accepted: the rejected session stays unaccepted.
	var status string
	if err := testDB.QueryRow("SELECT status FROM session WHERE id = ?", globalWorkerID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status == "completed" {
		t.Fatal("a rejected close must not mark sessions completed/accepted")
	}
}

// markWaveWorkerProcessRowsDead turns every recorded worker process of the
// wave into a DEAD historical row (pid 0, exited) and returns the process row
// id per wave worker id, exactly what a finished or crashed attempt leaves
// behind in worker_process.
func markWaveWorkerProcessRowsDead(t *testing.T, testDB *sql.DB, waveID int) map[int]int {
	t.Helper()
	if _, err := testDB.Exec(
		`UPDATE worker_process
		 SET pid = 0, status = ?, stop_reason = 'process exited', stopped_at = COALESCE(stopped_at, CURRENT_TIMESTAMP), updated_at = CURRENT_TIMESTAMP
		 WHERE parallel_wave_id = ?`,
		workerStatusExited,
		waveID,
	); err != nil {
		t.Fatalf("mark worker process rows dead: %v", err)
	}
	rows, err := testDB.Query("SELECT parallel_wave_worker_id, id FROM worker_process WHERE parallel_wave_id = ?", waveID)
	if err != nil {
		t.Fatalf("read dead worker process rows: %v", err)
	}
	defer rows.Close()
	deadProcessByWorkerID := map[int]int{}
	for rows.Next() {
		var workerID, processID int
		if err := rows.Scan(&workerID, &processID); err != nil {
			t.Fatalf("scan dead worker process row: %v", err)
		}
		deadProcessByWorkerID[workerID] = processID
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate dead worker process rows: %v", err)
	}
	return deadProcessByWorkerID
}

// Regression for feedback 96/97 (wave 841): manual workers requeued for rework
// (review_ready -> pending => retry_wait) with dead historical
// worker_process_id rows must not be finalized as failed by the wait/reconcile
// path, must not trip failed_worker_majority, and must reach the retry
// scheduler's relaunch in their recorded worktrees.
func TestManualWaveReworkRequeuedWorkersSurviveWaitReconcileAndRelaunch(t *testing.T) {
	originalDB, originalRole, originalProjectID, originalExecCommand := db, authorizedRole, authorizedProjectId, execCommand
	originalQuotaGate := DefaultQuotaGate
	defer func() {
		db, authorizedRole, authorizedProjectId, execCommand = originalDB, originalRole, originalProjectID, originalExecCommand
		DefaultQuotaGate = originalQuotaGate
	}()
	// Retry claiming must be deterministic here; live quota is the quota
	// gate's own test surface.
	DefaultQuotaGate = nil

	repoDir := setupCleanGitRepo(t)
	testDB := setupParallelWaveTestDB(t, repoDir)
	defer testDB.Close()
	if _, err := testDB.Exec(`INSERT INTO session (project_id, task_description, status) VALUES (1, 'Task D', 'pending')`); err != nil {
		t.Fatalf("seed third session: %v", err)
	}
	db, authorizedRole, authorizedProjectId = testDB, "fixer", 1

	_, created, err := CreateNetrunnerWave(context.Background(), nil, CreateNetrunnerWaveInput{
		SessionIds: []int{1, 2, 3},
		BaseRef:    "HEAD",
		Reason:     "rework relaunch regression",
	})
	if err != nil {
		t.Fatalf("create rework regression wave: %v", err)
	}
	markTestWaveRunningWithWorktrees(t, testDB, repoDir, created)
	deadProcessByWorkerID := markWaveWorkerProcessRowsDead(t, testDB, created.WaveId)

	// The attempt finished and the Fixer rejected every delivery: terminal
	// workers with dead historical process rows, sessions in review.
	for _, worker := range created.Workers {
		globalSessionID, err := globalSessionIDFromProjectScoped(worker.SessionId, 1)
		if err != nil {
			t.Fatalf("map session %d: %v", worker.SessionId, err)
		}
		if _, err := testDB.Exec("UPDATE session SET status = 'review' WHERE id = ?", globalSessionID); err != nil {
			t.Fatalf("mark session %d review: %v", worker.SessionId, err)
		}
		if _, err := testDB.Exec(
			`UPDATE parallel_wave_worker
			 SET status = ?, terminal_at = CURRENT_TIMESTAMP, terminal_outcome = ?, head_sha = ?, updated_at = CURRENT_TIMESTAMP
			 WHERE id = ?`,
			parallelWaveWorkerStatusReviewReady,
			parallelWaveWorkerStatusReviewReady,
			created.BaseSha,
			worker.Id,
		); err != nil {
			t.Fatalf("mark worker %d review_ready: %v", worker.SessionId, err)
		}
	}

	// Rejected review goes back for rework on every worker.
	for _, worker := range created.Workers {
		if _, _, err := SetSessionStatus(context.Background(), nil, SetSessionStatusInput{
			SessionId: worker.SessionId,
			Status:    "pending",
			Reason:    "rejected: rework required",
		}); err != nil {
			t.Fatalf("rework worker %d must be sendable back: %v", worker.SessionId, err)
		}
	}

	// The requeue must clear the stale attempt bookkeeping atomically.
	for _, worker := range created.Workers {
		var (
			status          string
			attempts        int
			linkageCleared  bool
			terminalCleared bool
			terminalOutcome string
			failureReason   string
		)
		if err := testDB.QueryRow(
			`SELECT status, retry_attempt_count, worker_process_id IS NULL, terminal_at IS NULL,
			        COALESCE(terminal_outcome, ''), COALESCE(failure_reason, '')
			 FROM parallel_wave_worker WHERE id = ?`, worker.Id,
		).Scan(&status, &attempts, &linkageCleared, &terminalCleared, &terminalOutcome, &failureReason); err != nil {
			t.Fatalf("read requeued worker %d: %v", worker.SessionId, err)
		}
		if status != parallelWaveWorkerStatusRetryWait || attempts != 0 || !linkageCleared || !terminalCleared || terminalOutcome != "" || failureReason != "" {
			t.Fatalf(
				"requeue must clear stale attempt bookkeeping for worker %d: status=%s attempts=%d linkageCleared=%v terminalCleared=%v terminalOutcome=%q failureReason=%q",
				worker.SessionId, status, attempts, linkageCleared, terminalCleared, terminalOutcome, failureReason,
			)
		}
	}

	normalizedRepoDir, err := normalizeProjectCWD(repoDir)
	if err != nil {
		t.Fatalf("normalize repo dir: %v", err)
	}

	// A stale-linked retry_wait row (the provider rate-limit shape: the dead
	// historical worker_process_id is still in place) must get the same
	// protection from the wait/reconcile path.
	staleLinkedWorker := created.Workers[0]
	if _, err := testDB.Exec(
		"UPDATE parallel_wave_worker SET worker_process_id = ? WHERE id = ?",
		deadProcessByWorkerID[staleLinkedWorker.Id],
		staleLinkedWorker.Id,
	); err != nil {
		t.Fatalf("restage stale worker_process linkage: %v", err)
	}

	// Neither the wait inspection nor the stale-worker reconcile may finalize
	// a retry_wait worker as failed on its dead historical process row.
	wave, err := fetchNetrunnerWaveSnapshot(created.WaveId, 1)
	if err != nil {
		t.Fatalf("fetch requeued wave: %v", err)
	}
	for _, worker := range wave.Workers {
		if _, terminal, err := inspectParallelWaveWorkerForWait(normalizedRepoDir, wave, worker); err != nil {
			t.Fatalf("inspect requeued worker %d: %v", worker.SessionId, err)
		} else if terminal {
			t.Fatalf("requeued worker %d must stay non-terminal for the retry scheduler, got %+v", worker.SessionId, worker)
		}
	}
	if _, err := reconcileStaleParallelWaveWorkers(normalizedRepoDir, wave); err != nil {
		t.Fatalf("reconcile requeued wave: %v", err)
	}
	if err := refreshParallelWaveAggregateStatus(created.WaveId, 1); err != nil {
		t.Fatalf("refresh requeued wave aggregate: %v", err)
	}
	wave, err = fetchNetrunnerWaveSnapshot(created.WaveId, 1)
	if err != nil {
		t.Fatalf("fetch reconciled requeued wave: %v", err)
	}
	for _, worker := range wave.Workers {
		if worker.Status != parallelWaveWorkerStatusRetryWait {
			t.Fatalf("worker %d must survive wait/reconcile as retry_wait, got %s (%q)", worker.SessionId, worker.Status, worker.FailureReason)
		}
	}
	if wave.ControlState != parallelWaveControlActive || strings.Contains(wave.ControlReason, "failed_worker_majority") || wave.Status == parallelWaveStatusFailed {
		t.Fatalf("requeued workers must not trip a false failure pause: %+v", wave)
	}

	// The retry scheduler must relaunch every requeued worker in its recorded
	// worktree with a fresh attempt budget consumed exactly once.
	var capturedArgs [][]string
	installFakeWaveWorkerLauncher(t, "", &capturedArgs)
	wave, err = fetchNetrunnerWaveSnapshot(created.WaveId, 1)
	if err != nil {
		t.Fatalf("fetch wave before relaunch: %v", err)
	}
	if err := processParallelWaveWorkerRetries(context.Background(), normalizedRepoDir, wave, time.Second); err != nil {
		t.Fatalf("relaunch requeued workers: %v", err)
	}
	wave, err = fetchNetrunnerWaveSnapshot(created.WaveId, 1)
	if err != nil {
		t.Fatalf("fetch relaunched wave: %v", err)
	}
	relaunchedWorktreeByArg := map[string]bool{}
	for _, args := range capturedArgs {
		relaunchedWorktreeByArg[argumentAfter(args, "--worker-cwd")] = true
	}
	for _, worker := range wave.Workers {
		if worker.Status != parallelWaveWorkerStatusRunning || worker.RetryAttemptCount != 1 {
			t.Fatalf("worker %d must be relaunched with one attempt consumed: %+v", worker.SessionId, worker)
		}
		if worker.WorkerProcessId <= 0 || worker.WorkerProcessId == deadProcessByWorkerID[worker.Id] {
			t.Fatalf("worker %d must be relaunched onto a fresh process row: %+v", worker.SessionId, worker)
		}
		absWorktreePath, err := resolveParallelWaveWorktreePath(normalizedRepoDir, worker.WorktreePath)
		if err != nil {
			t.Fatalf("resolve worker %d worktree: %v", worker.SessionId, err)
		}
		if !relaunchedWorktreeByArg[absWorktreePath] {
			t.Fatalf("worker %d must relaunch in its recorded worktree %s, got %v", worker.SessionId, absWorktreePath, relaunchedWorktreeByArg)
		}
	}
}

// Regression for the wave-engine dead end: a worker whose attempt FAILED on
// infrastructure keeps its session already pending and had no governed way out
// (the one governed repair is single-use). set_session_status(pending,
// reason=...) with a non-empty reason requeues it to retry_wait and the wait
// loop relaunches it; without a reason nothing is requeued.
func TestFailedWaveWorkerRequeuesViaSetSessionStatusWithReason(t *testing.T) {
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
	markTestWaveRunningWithWorktrees(t, testDB, repoDir, created)
	deadProcessByWorkerID := markWaveWorkerProcessRowsDead(t, testDB, created.WaveId)

	// Infrastructure failure before checkout: the worker row is failed with
	// full terminal diagnostics while its session stays pending.
	for _, worker := range created.Workers {
		globalSessionID, err := globalSessionIDFromProjectScoped(worker.SessionId, 1)
		if err != nil {
			t.Fatalf("map session %d: %v", worker.SessionId, err)
		}
		if _, err := testDB.Exec("UPDATE session SET status = 'pending' WHERE id = ?", globalSessionID); err != nil {
			t.Fatalf("reset session %d: %v", worker.SessionId, err)
		}
		if _, err := testDB.Exec(
			`UPDATE parallel_wave_worker
			 SET status = ?, retry_attempt_count = 3, retry_cause = 'provider_rate_limit', retry_next_eligible_at = '',
			     terminal_at = CURRENT_TIMESTAMP, terminal_outcome = ?, failure_reason = 'wave netrunner launcher exited before startup completed',
			     updated_at = CURRENT_TIMESTAMP
			 WHERE id = ?`,
			parallelWaveWorkerStatusFailed,
			parallelWaveWorkerStatusFailed,
			worker.Id,
		); err != nil {
			t.Fatalf("fail worker %d: %v", worker.SessionId, err)
		}
	}
	failedWorker := created.Workers[0]

	// A bare pending -> pending without a reason is not a requeue trigger.
	if _, _, err := SetSessionStatus(context.Background(), nil, SetSessionStatusInput{
		SessionId: failedWorker.SessionId,
		Status:    "pending",
	}); err == nil {
		t.Fatal("a bare pending -> pending on a wave-linked session must keep its current governed behavior")
	}
	var stillFailed string
	if err := testDB.QueryRow("SELECT status FROM parallel_wave_worker WHERE id = ?", failedWorker.Id).Scan(&stillFailed); err != nil {
		t.Fatal(err)
	}
	if stillFailed != parallelWaveWorkerStatusFailed {
		t.Fatalf("a bare pending -> pending must not requeue anything, got %q", stillFailed)
	}

	// The governed trigger: an explicit reason requeues the failed worker.
	_, out, err := SetSessionStatus(context.Background(), nil, SetSessionStatusInput{
		SessionId: failedWorker.SessionId,
		Status:    "pending",
		Reason:    "infra failure: launcher exited before startup; relaunch",
	})
	if err != nil {
		t.Fatalf("failed wave worker must be requeueable with a reason: %v", err)
	}
	if out.PreviousStatus != "pending" || out.NewStatus != "pending" {
		t.Fatalf("unexpected requeue transition: %+v", out)
	}
	for _, worker := range created.Workers {
		if worker.Id == failedWorker.Id {
			continue
		}
		if _, _, err := SetSessionStatus(context.Background(), nil, SetSessionStatusInput{
			SessionId: worker.SessionId,
			Status:    "pending",
			Reason:    "infra failure: launcher exited before startup; relaunch",
		}); err != nil {
			t.Fatalf("failed wave worker %d must be requeueable with a reason: %v", worker.SessionId, err)
		}
	}
	for _, worker := range created.Workers {
		var (
			status          string
			attempts        int
			reworkCount     int
			linkageCleared  bool
			terminalCleared bool
			terminalOutcome string
			failureReason   string
		)
		if err := testDB.QueryRow(
			`SELECT w.status, w.retry_attempt_count, s.rework_count, w.worker_process_id IS NULL, w.terminal_at IS NULL,
			        COALESCE(w.terminal_outcome, ''), COALESCE(w.failure_reason, '')
			 FROM parallel_wave_worker w JOIN session s ON s.id = w.session_id
			 WHERE w.id = ?`, worker.Id,
		).Scan(&status, &attempts, &reworkCount, &linkageCleared, &terminalCleared, &terminalOutcome, &failureReason); err != nil {
			t.Fatalf("read requeued failed worker: %v", err)
		}
		if status != parallelWaveWorkerStatusRetryWait || attempts != 0 || !linkageCleared || !terminalCleared || terminalOutcome != "" || failureReason != "" {
			t.Fatalf(
				"failed worker %d must requeue with a reset attempt budget and clean diagnostics: status=%s attempts=%d linkageCleared=%v terminalCleared=%v terminalOutcome=%q failureReason=%q",
				worker.SessionId, status, attempts, linkageCleared, terminalCleared, terminalOutcome, failureReason,
			)
		}
		if reworkCount != 0 {
			t.Fatalf("the governed failed-worker retry is not a review rework, rework_count=%d", reworkCount)
		}
	}

	// The wait/reconcile path must leave it for the scheduler, and the
	// scheduler must relaunch it in its recorded worktree.
	normalizedRepoDir, err := normalizeProjectCWD(repoDir)
	if err != nil {
		t.Fatalf("normalize repo dir: %v", err)
	}
	wave, err := fetchNetrunnerWaveSnapshot(created.WaveId, 1)
	if err != nil {
		t.Fatalf("fetch wave: %v", err)
	}
	for _, worker := range wave.Workers {
		if _, terminal, err := inspectParallelWaveWorkerForWait(normalizedRepoDir, wave, worker); err != nil {
			t.Fatalf("inspect worker %d: %v", worker.SessionId, err)
		} else if terminal {
			t.Fatalf("worker %d must stay non-terminal for the retry scheduler, got %+v", worker.SessionId, worker)
		}
	}
	if _, err := reconcileStaleParallelWaveWorkers(normalizedRepoDir, wave); err != nil {
		t.Fatalf("reconcile wave: %v", err)
	}
	if err := refreshParallelWaveAggregateStatus(created.WaveId, 1); err != nil {
		t.Fatalf("refresh wave aggregate: %v", err)
	}
	wave, err = fetchNetrunnerWaveSnapshot(created.WaveId, 1)
	if err != nil {
		t.Fatalf("fetch reconciled wave: %v", err)
	}
	if wave.ControlState != parallelWaveControlActive || strings.Contains(wave.ControlReason, "failed_worker_majority") {
		t.Fatalf("a requeueable failed worker must not leave a false failure pause behind: %+v", wave)
	}

	var capturedArgs [][]string
	installFakeWaveWorkerLauncher(t, "", &capturedArgs)
	wave, err = fetchNetrunnerWaveSnapshot(created.WaveId, 1)
	if err != nil {
		t.Fatalf("fetch wave before relaunch: %v", err)
	}
	if err := processParallelWaveWorkerRetries(context.Background(), normalizedRepoDir, wave, time.Second); err != nil {
		t.Fatalf("relaunch requeued failed worker: %v", err)
	}
	wave, err = fetchNetrunnerWaveSnapshot(created.WaveId, 1)
	if err != nil {
		t.Fatalf("fetch relaunched wave: %v", err)
	}
	relaunchedWorktreeByArg := map[string]bool{}
	for _, args := range capturedArgs {
		relaunchedWorktreeByArg[argumentAfter(args, "--worker-cwd")] = true
	}
	for _, worker := range created.Workers {
		relaunched := testWaveWorkerBySession(t, wave, worker.SessionId)
		if relaunched.Status != parallelWaveWorkerStatusRunning || relaunched.RetryAttemptCount != 1 {
			t.Fatalf("requeued failed worker %d must reach relaunch: %+v", worker.SessionId, relaunched)
		}
		if relaunched.WorkerProcessId <= 0 || relaunched.WorkerProcessId == deadProcessByWorkerID[worker.Id] {
			t.Fatalf("relaunch of worker %d must record a fresh process row: %+v", worker.SessionId, relaunched)
		}
		absWorktreePath, err := resolveParallelWaveWorktreePath(normalizedRepoDir, relaunched.WorktreePath)
		if err != nil {
			t.Fatalf("resolve relaunched worktree: %v", err)
		}
		if !relaunchedWorktreeByArg[absWorktreePath] {
			t.Fatalf("failed worker %d must relaunch in its recorded worktree %s, got %v", worker.SessionId, absWorktreePath, relaunchedWorktreeByArg)
		}
		// The dead historical process row is untouched history, not a live row.
		historicalStatus := ""
		if err := testDB.QueryRow("SELECT status FROM worker_process WHERE id = ?", deadProcessByWorkerID[worker.Id]).Scan(&historicalStatus); err != nil {
			t.Fatal(err)
		}
		if historicalStatus != workerStatusExited {
			t.Fatalf("historical process row must stay terminal, got %q", historicalStatus)
		}
	}
}
