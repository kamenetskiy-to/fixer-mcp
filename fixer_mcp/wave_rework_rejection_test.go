package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
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

// Regression for host feedback 117 (race 2, wave 869): on a partially_failed wave,
// a worker session that reached review while the worker row was still running
// must be atomically requeued by set_session_status(pending) for rework.
// The requeue must terminate any live running process, clear worker_process linkage
// and due counts, preserve the raw report in session.report, deliver the updated task,
// and not be falsely failed by subsequent wait loops.
func TestPartiallyFailedWaveWorkerInReviewRequeuesAtomicallyAndClearsRunningProcess(t *testing.T) {
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

	// Create wave with two sessions
	_, created, err := CreateNetrunnerWave(context.Background(), nil, CreateNetrunnerWaveInput{
		SessionIds: []int{1, 2},
		BaseRef:    "HEAD",
		Reason:     "partially failed wave rework test",
	})
	if err != nil {
		t.Fatalf("create test wave: %v", err)
	}
	markTestWaveRunningWithWorktrees(t, testDB, repoDir, created)

	worker1 := created.Workers[0]
	worker2 := created.Workers[1]
	globalSession2, _ := globalSessionIDFromProjectScoped(worker2.SessionId, 1)

	// Worker 1 failed previously; wave is in partially_failed status
	if _, err := testDB.Exec(
		`UPDATE parallel_wave_worker
		 SET status = ?, terminal_outcome = ?, failure_reason = 'compilation failed', terminal_at = CURRENT_TIMESTAMP
		 WHERE id = ?`,
		parallelWaveWorkerStatusFailed,
		parallelWaveWorkerStatusFailed,
		worker1.Id,
	); err != nil {
		t.Fatal(err)
	}
	if err := refreshParallelWaveAggregateStatus(created.WaveId, 1); err != nil {
		t.Fatal(err)
	}

	// Worker 2 session reached review and has a report, but worker row is STILL 'running'
	testReport := `{"files_changed":["a.go"],"commands_run":["go test"],"checks_run":["pass"],"blockers":[]}`
	if _, err := testDB.Exec("UPDATE session SET status = 'review', report = ? WHERE id = ?", testReport, globalSession2); err != nil {
		t.Fatal(err)
	}
	// Record an active mock process for worker 2
	procRes, err := testDB.Exec(
		`INSERT INTO worker_process (project_id, session_id, pid, launch_epoch, status, parallel_wave_id, parallel_wave_worker_id, updated_at)
		 VALUES (1, ?, 99999, 1, ?, ?, ?, CURRENT_TIMESTAMP)`,
		globalSession2, workerStatusRunning, created.WaveId, worker2.Id,
	)
	if err != nil {
		t.Fatal(err)
	}
	procID, _ := procRes.LastInsertId()
	if _, err := testDB.Exec(
		`UPDATE parallel_wave_worker
		 SET status = ?, worker_process_id = ?, retry_attempt_count = 2, updated_at = CURRENT_TIMESTAMP
		 WHERE id = ?`,
		parallelWaveWorkerStatusRunning, int(procID), worker2.Id,
	); err != nil {
		t.Fatal(err)
	}

	// Fixer appends instructions to session 2
	if _, _, err := UpdateTask(context.Background(), nil, UpdateTaskInput{
		SessionId:           worker2.SessionId,
		AppendedDescription: "Rework: fix race condition in wave worker",
	}); err != nil {
		t.Fatalf("update_task failed: %v", err)
	}

	// Fixer rejects review delivery and sends worker 2 back for rework
	callRes, out, err := SetSessionStatus(context.Background(), nil, SetSessionStatusInput{
		SessionId: worker2.SessionId,
		Status:    "pending",
		Reason:    "rework required: fix race condition",
	})
	if err != nil {
		t.Fatalf("set_session_status(pending) for review rework must succeed: %v", err)
	}
	if callRes != nil && callRes.IsError {
		t.Fatalf("set_session_status returned error: %+v", callRes)
	}
	if out.NewStatus != "pending" || out.PreviousStatus != "review" {
		t.Fatalf("unexpected output: %+v", out)
	}

	// Assertions on database state after requeue:
	// 1. parallel_wave_worker row must be in retry_wait with 0 attempts and cleared linkage
	var (
		wStatus         string
		wAttempts       int
		wLinkageCleared bool
		wTermCleared    bool
		wRetryCause     string
	)
	if err := testDB.QueryRow(
		`SELECT status, retry_attempt_count, worker_process_id IS NULL, terminal_at IS NULL, retry_cause
		 FROM parallel_wave_worker WHERE id = ?`,
		worker2.Id,
	).Scan(&wStatus, &wAttempts, &wLinkageCleared, &wTermCleared, &wRetryCause); err != nil {
		t.Fatal(err)
	}
	if wStatus != parallelWaveWorkerStatusRetryWait {
		t.Fatalf("worker 2 status must be retry_wait, got %q", wStatus)
	}
	if wAttempts != 0 {
		t.Fatalf("worker 2 retry_attempt_count must be reset to 0, got %d", wAttempts)
	}
	if !wLinkageCleared {
		t.Fatalf("worker 2 worker_process_id must be NULL")
	}
	if !wTermCleared {
		t.Fatalf("worker 2 terminal_at must be NULL")
	}
	if wRetryCause != "rework" {
		t.Fatalf("worker 2 retry_cause must be 'rework', got %q", wRetryCause)
	}

	// 2. Old worker_process row must be marked stopped with stop_reason = 'rework requested'
	var procStatus, procStopReason string
	if err := testDB.QueryRow(
		`SELECT status, stop_reason FROM worker_process WHERE id = ?`,
		procID,
	).Scan(&procStatus, &procStopReason); err != nil {
		t.Fatal(err)
	}
	if procStatus != workerStatusStopped {
		t.Fatalf("worker_process row status must be stopped, got %q", procStatus)
	}
	if procStopReason != "rework requested" {
		t.Fatalf("worker_process stop_reason must be 'rework requested', got %q", procStopReason)
	}

	// 3. Raw old report in session table must be preserved
	var savedReport string
	if err := testDB.QueryRow("SELECT report FROM session WHERE id = ?", globalSession2).Scan(&savedReport); err != nil {
		t.Fatal(err)
	}
	if savedReport != testReport {
		t.Fatalf("raw old report must be preserved, got %q", savedReport)
	}

	// 4. Session task description must contain appended instructions
	var savedTask string
	if err := testDB.QueryRow("SELECT task_description FROM session WHERE id = ?", globalSession2).Scan(&savedTask); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(savedTask, "Rework: fix race condition in wave worker") {
		t.Fatalf("task description must have updated instructions, got %q", savedTask)
	}

	// 5. Subsequent wait inspection must NOT mark the worker failed on the old process
	normalizedRepoDir, err := normalizeProjectCWD(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	wave, err := fetchNetrunnerWaveSnapshot(created.WaveId, 1)
	if err != nil {
		t.Fatal(err)
	}
	refreshedWorker2 := testWaveWorkerBySession(t, wave, worker2.SessionId)
	cand, terminal, err := inspectParallelWaveWorkerForWait(normalizedRepoDir, wave, refreshedWorker2)
	if err != nil {
		t.Fatalf("inspect worker 2: %v", err)
	}
	if terminal {
		t.Fatalf("requeued worker 2 must not be terminal before retry launch, got candidate: %+v", cand)
	}

	// 6. Retry scheduler relaunches worker 2 in its recorded worktree
	var capturedArgs [][]string
	installFakeWaveWorkerLauncher(t, "", &capturedArgs)
	if err := processParallelWaveWorkerRetries(context.Background(), normalizedRepoDir, wave, time.Second); err != nil {
		t.Fatalf("relaunch rework worker: %v", err)
	}
	wave, err = fetchNetrunnerWaveSnapshot(created.WaveId, 1)
	if err != nil {
		t.Fatal(err)
	}
	relaunchedWorker2 := testWaveWorkerBySession(t, wave, worker2.SessionId)
	if relaunchedWorker2.Status != parallelWaveWorkerStatusRunning || relaunchedWorker2.RetryAttemptCount != 1 {
		t.Fatalf("reworked worker must reach running with attempt 1, got %+v", relaunchedWorker2)
	}
}

// Regression for host feedback 116 (race 1): interrupted serial launch leaves some
// workers in 'worktree_ready' and an in-flight worker in 'launching' with missing FK.
// Wait must not falsely fail worktree_ready workers with 'worker process linkage missing'
// and must not majority-pause; missing durable worker_process rows must be recovered atomically;
// and worktree_ready workers must be claimed and launched without duplicates.
func TestInterruptedSerialLaunchWithWorktreeReadyWorkersRecoversAndRelaunchesWithoutDuplicates(t *testing.T) {
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

	// Seed 3 extra sessions to have 5 workers total
	for i := 3; i <= 5; i++ {
		if _, err := testDB.Exec(`INSERT INTO session (project_id, task_description, status) VALUES (1, 'Task', 'pending')`); err != nil {
			t.Fatalf("seed session %d: %v", i, err)
		}
	}
	db, authorizedRole, authorizedProjectId = testDB, "fixer", 1

	_, created, err := CreateNetrunnerWave(context.Background(), nil, CreateNetrunnerWaveInput{
		SessionIds: []int{1, 2, 3, 4, 5},
		BaseRef:    "HEAD",
		Reason:     "interrupted serial launch test",
	})
	if err != nil {
		t.Fatalf("create test wave: %v", err)
	}

	// Prepare worktrees for all 5 workers
	normalizedRepoDir, err := normalizeProjectCWD(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, worker := range created.Workers {
		absWorktreePath, err := resolveParallelWaveWorktreePath(repoDir, worker.WorktreePath)
		if err != nil {
			t.Fatalf("resolve worker worktree: %v", err)
		}
		if err := os.MkdirAll(filepath.Dir(absWorktreePath), 0o755); err != nil {
			t.Fatalf("prepare worktree parent: %v", err)
		}
		runGitTestCommand(t, repoDir, "worktree", "add", "-b", worker.BranchName, absWorktreePath, created.BaseSha)
		if _, err := testDB.Exec("UPDATE parallel_wave_worker SET status = ? WHERE id = ?", parallelWaveWorkerStatusWorktreeReady, worker.Id); err != nil {
			t.Fatal(err)
		}
	}

	// Simulate interrupted serial launch:
	// Worker 1: running with valid worker_process
	// Worker 2: in 'launching' with unlinked worker_process_id (0/NULL), but worker_process row exists (like session 695 / process 4200), and session in 'review'
	// Workers 3, 4, 5: untouched in 'worktree_ready'
	globalSession1, _ := globalSessionIDFromProjectScoped(created.Workers[0].SessionId, 1)
	globalSession2, _ := globalSessionIDFromProjectScoped(created.Workers[1].SessionId, 1)

	// Worker 1 process
	proc1, err := testDB.Exec(
		`INSERT INTO worker_process (project_id, session_id, pid, launch_epoch, status, parallel_wave_id, parallel_wave_worker_id, updated_at)
		 VALUES (1, ?, 11111, 1, ?, ?, ?, CURRENT_TIMESTAMP)`,
		globalSession1, workerStatusRunning, created.WaveId, created.Workers[0].Id,
	)
	if err != nil {
		t.Fatal(err)
	}
	proc1ID, _ := proc1.LastInsertId()
	if _, err := testDB.Exec(
		`UPDATE parallel_wave_worker SET status = ?, worker_process_id = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		parallelWaveWorkerStatusRunning, int(proc1ID), created.Workers[0].Id,
	); err != nil {
		t.Fatal(err)
	}

	// Worker 2: process 4200 exists in worker_process with exact matching identity,
	// but parallel_wave_worker.worker_process_id is NULL and status is 'launching',
	// and session is in 'review' with a report.
	proc2, err := testDB.Exec(
		`INSERT INTO worker_process (id, project_id, session_id, pid, launch_epoch, status, parallel_wave_id, parallel_wave_worker_id, updated_at)
		 VALUES (4200, 1, ?, 22222, 1, ?, ?, ?, CURRENT_TIMESTAMP)`,
		globalSession2, workerStatusExited, created.WaveId, created.Workers[1].Id,
	)
	if err != nil {
		t.Fatal(err)
	}
	_ = proc2
	if _, err := testDB.Exec(
		`UPDATE parallel_wave_worker SET status = ?, worker_process_id = NULL, launch_epoch = 1, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		parallelWaveWorkerStatusLaunching, created.Workers[1].Id,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := testDB.Exec(
		"UPDATE session SET status = 'review', report = '{\"files_changed\":[\"b.go\"],\"commands_run\":[\"go test\"],\"checks_run\":[\"ok\"],\"blockers\":[]}' WHERE id = ?",
		globalSession2,
	); err != nil {
		t.Fatal(err)
	}

	// Workers 3, 4, 5 stay in worktree_ready
	wave, err := fetchNetrunnerWaveSnapshot(created.WaveId, 1)
	if err != nil {
		t.Fatal(err)
	}

	// 1. inspectParallelWaveWorkerForWait for workers 3, 4, 5 must NOT fail them!
	for _, w := range wave.Workers[2:] {
		cand, term, err := inspectParallelWaveWorkerForWait(normalizedRepoDir, wave, w)
		if err != nil {
			t.Fatalf("inspect worktree_ready worker %d failed: %v", w.SessionId, err)
		}
		if term {
			t.Fatalf("worktree_ready worker %d must be non-terminal (queued never started), got: %+v", w.SessionId, cand)
		}
	}

	// 2. inspectParallelWaveWorkerForWait for worker 2 must recover process 4200 atomically
	// and transition cleanly to review_ready
	cand2, term2, err := inspectParallelWaveWorkerForWait(normalizedRepoDir, wave, wave.Workers[1])
	if err != nil {
		t.Fatalf("inspect worker 2: %v", err)
	}
	if !term2 || cand2.TerminalCondition != "review_ready" {
		t.Fatalf("worker 2 must recover process and reach review_ready, got term=%v cond=%q", term2, cand2.TerminalCondition)
	}
	var recoveredPID int
	if err := testDB.QueryRow("SELECT worker_process_id FROM parallel_wave_worker WHERE id = ?", wave.Workers[1].Id).Scan(&recoveredPID); err != nil {
		t.Fatal(err)
	}
	if recoveredPID != 4200 {
		t.Fatalf("expected recovered worker_process_id=4200, got %d", recoveredPID)
	}

	// 3. scheduleWorktreeReadyWaveWorkers claims and launches workers 3, 4, 5
	var capturedArgs [][]string
	installFakeWaveWorkerLauncher(t, "", &capturedArgs)
	if err := scheduleWorktreeReadyWaveWorkers(context.Background(), normalizedRepoDir, wave, 1, 5*time.Second); err != nil {
		t.Fatalf("scheduleWorktreeReadyWaveWorkers failed: %v", err)
	}

	wave, err = fetchNetrunnerWaveSnapshot(created.WaveId, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range wave.Workers[2:] {
		if w.Status != parallelWaveWorkerStatusRunning {
			t.Fatalf("worktree_ready worker %d must have been launched, got status %q", w.SessionId, w.Status)
		}
		if w.WorkerProcessId <= 0 {
			t.Fatalf("worker %d must have valid worker_process_id after launch", w.SessionId)
		}
	}

	// Calling scheduleWorktreeReadyWaveWorkers again must be an idempotent no-op (no duplicate launches)
	launchCountBefore := len(capturedArgs)
	if err := scheduleWorktreeReadyWaveWorkers(context.Background(), normalizedRepoDir, wave, 1, 5*time.Second); err != nil {
		t.Fatalf("second scheduleWorktreeReadyWaveWorkers failed: %v", err)
	}
	if len(capturedArgs) != launchCountBefore {
		t.Fatalf("subsequent schedule call must not double launch workers, had %d, now %d", launchCountBefore, len(capturedArgs))
	}

	// Wave must not be in majority pause
	if err := refreshParallelWaveAggregateStatus(created.WaveId, 1); err != nil {
		t.Fatal(err)
	}
	wave, err = fetchNetrunnerWaveSnapshot(created.WaveId, 1)
	if err != nil {
		t.Fatal(err)
	}
	if wave.ControlState == parallelWaveControlPausedForArchitect {
		t.Fatalf("interrupted launch must not cause wave majority pause: %s", wave.ControlReason)
	}
}

// Regression for host feedback 116 (race 1 WIP metadata recovery): when serial launch
// is interrupted after the launcher writes worker_metadata-*.json and headless/launcher logs,
// but before worker_process row is persisted or linked to parallel_wave_worker,
// wait inspection and scheduling must recover the matching identity plus metadata atomically
// from disk artifacts without binding arbitrary processes.
func TestInterruptedLaunchWIPMetadataRecovery(t *testing.T) {
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

	_, created, err := CreateNetrunnerWave(context.Background(), nil, CreateNetrunnerWaveInput{
		SessionIds: []int{1, 2},
		BaseRef:    "HEAD",
		Reason:     "WIP metadata recovery test",
	})
	if err != nil {
		t.Fatalf("create test wave: %v", err)
	}

	normalizedRepoDir, err := normalizeProjectCWD(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	worker1 := created.Workers[0]
	globalSession1, err := globalSessionIDFromProjectScoped(worker1.SessionId, 1)
	if err != nil {
		t.Fatal(err)
	}

	absWorktreePath, err := resolveParallelWaveWorktreePath(repoDir, worker1.WorktreePath)
	if err != nil {
		t.Fatalf("resolve worker worktree: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(absWorktreePath), 0o755); err != nil {
		t.Fatal(err)
	}
	runGitTestCommand(t, repoDir, "worktree", "add", "-b", worker1.BranchName, absWorktreePath, created.BaseSha)

	// Simulate interrupted launch:
	// Worker 1 is in 'launching', worker_process_id is NULL, worker_metadata_path is empty.
	// But the launcher has already created the artifact dir and written worker_metadata-*.json,
	// headless-*.log, and launcher-*.log on disk.
	if _, err := testDB.Exec(
		`UPDATE parallel_wave_worker
		 SET status = ?, worker_process_id = NULL, worker_metadata_path = '', headless_log_path = '', launcher_log_path = '', updated_at = CURRENT_TIMESTAMP
		 WHERE id = ?`,
		parallelWaveWorkerStatusLaunching, worker1.Id,
	); err != nil {
		t.Fatal(err)
	}

	artifactDir := filepath.Join(normalizedRepoDir, ".codex", "netrunner_wave_artifacts", fmt.Sprintf("wave-%d", created.WaveId), fmt.Sprintf("session-%d", worker1.SessionId))
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		t.Fatal(err)
	}
	metaFile := filepath.Join(artifactDir, "worker_metadata-12345.json")
	headlessFile := filepath.Join(artifactDir, "headless-codex-12345.log")
	launcherFile := filepath.Join(artifactDir, "launcher-12345.log")
	metaJSON := fmt.Sprintf(`{"worker_pid": %d, "session_id": %d, "backend": "codex", "headless_log_path": %q}`, os.Getpid(), globalSession1, headlessFile)
	if err := os.WriteFile(metaFile, []byte(metaJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(headlessFile, []byte("headless output"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(launcherFile, []byte("launcher output"), 0o644); err != nil {
		t.Fatal(err)
	}

	wave, err := fetchNetrunnerWaveSnapshot(created.WaveId, 1)
	if err != nil {
		t.Fatal(err)
	}

	// 1. inspectParallelWaveWorkerForWait must recover the matching process and metadata atomically from disk artifacts
	// Since process is alive (os.Getpid()), it must be running and non-terminal.
	cand, term, err := inspectParallelWaveWorkerForWait(normalizedRepoDir, wave, wave.Workers[0])
	if err != nil {
		t.Fatalf("inspect worker 1: %v", err)
	}
	if term {
		t.Fatalf("active recovered worker must be non-terminal while running, got term=%v cond=%q", term, cand.TerminalCondition)
	}

	var (
		recoveredPID      int
		recoveredStatus   string
		recoveredMetaPath string
		recoveredHeadless string
		recoveredLauncher string
	)
	if err := testDB.QueryRow(
		`SELECT COALESCE(worker_process_id, 0), status, worker_metadata_path, headless_log_path, launcher_log_path
		 FROM parallel_wave_worker WHERE id = ?`,
		worker1.Id,
	).Scan(&recoveredPID, &recoveredStatus, &recoveredMetaPath, &recoveredHeadless, &recoveredLauncher); err != nil {
		t.Fatal(err)
	}
	if recoveredPID <= 0 {
		t.Fatalf("worker 1 worker_process_id must be recovered from WIP metadata, got %d", recoveredPID)
	}
	if recoveredStatus != parallelWaveWorkerStatusRunning {
		t.Fatalf("worker 1 status must be running, got %q", recoveredStatus)
	}
	if recoveredMetaPath != metaFile {
		t.Fatalf("expected worker_metadata_path %q, got %q", metaFile, recoveredMetaPath)
	}
	if recoveredHeadless != headlessFile {
		t.Fatalf("expected headless_log_path %q, got %q", headlessFile, recoveredHeadless)
	}
	if recoveredLauncher != launcherFile {
		t.Fatalf("expected launcher_log_path %q, got %q", launcherFile, recoveredLauncher)
	}

	// Verify worker_process row exists with pid os.Getpid()
	var procPID int
	var procSessionID int
	if err := testDB.QueryRow("SELECT pid, session_id FROM worker_process WHERE id = ?", recoveredPID).Scan(&procPID, &procSessionID); err != nil {
		t.Fatal(err)
	}
	if procPID != os.Getpid() || procSessionID != globalSession1 {
		t.Fatalf("expected recovered process pid %d session %d, got pid %d session %d", os.Getpid(), globalSession1, procPID, procSessionID)
	}
	if cand.Worker.WorkerProcessId != recoveredPID {
		t.Fatalf("candidate worker must have recovered process id %d, got %d", recoveredPID, cand.Worker.WorkerProcessId)
	}

	// 2. When session reaches review, worker reaches review_ready
	if _, err := testDB.Exec(
		"UPDATE session SET status = 'review', report = '{\"files_changed\":[\"a.go\"],\"commands_run\":[\"go test\"],\"checks_run\":[\"pass\"],\"blockers\":[]}' WHERE id = ?",
		globalSession1,
	); err != nil {
		t.Fatal(err)
	}
	// Mark process stopped/exited so wait inspection can finalize review
	if _, err := testDB.Exec(
		"UPDATE worker_process SET status = ?, pid = 0, stop_reason = 'process exited', updated_at = CURRENT_TIMESTAMP WHERE id = ?",
		workerStatusExited, recoveredPID,
	); err != nil {
		t.Fatal(err)
	}
	wave, err = fetchNetrunnerWaveSnapshot(created.WaveId, 1)
	if err != nil {
		t.Fatal(err)
	}
	candReview, termReview, err := inspectParallelWaveWorkerForWait(normalizedRepoDir, wave, wave.Workers[0])
	if err != nil {
		t.Fatalf("inspect worker 1 review: %v", err)
	}
	if !termReview || candReview.TerminalCondition != "review_ready" {
		t.Fatalf("worker 1 must reach review_ready, got term=%v cond=%q", termReview, candReview.TerminalCondition)
	}
}

// Regression for host feedback 116 (race 1 queued vs dead spawned): correctly distinguish
// queued workers that never started (worktree_ready or unspawned launching) from dead spawned
// workers (process started, failed, and exited). Queued workers are safely scheduled/relaunched;
// dead spawned workers are finalized as failed and never falsely treated as unstarted.
func TestInterruptedLaunchDistinguishesQueuedNeverStartedFromDeadSpawned(t *testing.T) {
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

	// Seed 3 sessions: worker 1 (queued never started), worker 2 (dead spawned), worker 3 (stalled launching)
	for i := 3; i <= 3; i++ {
		if _, err := testDB.Exec(`INSERT INTO session (project_id, task_description, status) VALUES (1, 'Task 3', 'pending')`); err != nil {
			t.Fatal(err)
		}
	}
	_, created, err := CreateNetrunnerWave(context.Background(), nil, CreateNetrunnerWaveInput{
		SessionIds: []int{1, 2, 3},
		BaseRef:    "HEAD",
		Reason:     "queued vs dead spawned test",
	})
	if err != nil {
		t.Fatalf("create test wave: %v", err)
	}

	normalizedRepoDir, err := normalizeProjectCWD(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, worker := range created.Workers {
		absWorktreePath, err := resolveParallelWaveWorktreePath(repoDir, worker.WorktreePath)
		if err != nil {
			t.Fatalf("resolve worker worktree: %v", err)
		}
		if err := os.MkdirAll(filepath.Dir(absWorktreePath), 0o755); err != nil {
			t.Fatal(err)
		}
		runGitTestCommand(t, repoDir, "worktree", "add", "-b", worker.BranchName, absWorktreePath, created.BaseSha)
	}

	// Worker 1: worktree_ready (queued never started)
	if _, err := testDB.Exec("UPDATE parallel_wave_worker SET status = ? WHERE id = ?", parallelWaveWorkerStatusWorktreeReady, created.Workers[0].Id); err != nil {
		t.Fatal(err)
	}

	// Worker 2: dead spawned (process was spawned, exited with failure, session stayed pending)
	globalSession2, _ := globalSessionIDFromProjectScoped(created.Workers[1].SessionId, 1)
	procRes, err := testDB.Exec(
		`INSERT INTO worker_process (project_id, session_id, pid, launch_epoch, status, stop_reason, parallel_wave_id, parallel_wave_worker_id, updated_at)
		 VALUES (1, ?, 44444, 1, ?, 'process exited', ?, ?, CURRENT_TIMESTAMP)`,
		globalSession2, workerStatusExited, created.WaveId, created.Workers[1].Id,
	)
	if err != nil {
		t.Fatal(err)
	}
	proc2ID, _ := procRes.LastInsertId()
	if _, err := testDB.Exec(
		`UPDATE parallel_wave_worker SET status = ?, worker_process_id = ?, launch_epoch = 1, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		parallelWaveWorkerStatusRunning, int(proc2ID), created.Workers[1].Id,
	); err != nil {
		t.Fatal(err)
	}

	// Worker 3: stalled launching with no process and no WIP metadata, older than deferredLaunchTimeout
	oldTime := time.Now().Add(-10 * time.Minute).UTC().Format("2006-01-02 15:04:05")
	if _, err := testDB.Exec(
		`UPDATE parallel_wave_worker SET status = ?, worker_process_id = NULL, updated_at = ? WHERE id = ?`,
		parallelWaveWorkerStatusLaunching, oldTime, created.Workers[2].Id,
	); err != nil {
		t.Fatal(err)
	}

	wave, err := fetchNetrunnerWaveSnapshot(created.WaveId, 1)
	if err != nil {
		t.Fatal(err)
	}

	// 1. Worker 1 must be non-terminal (queued never started)
	cand1, term1, err := inspectParallelWaveWorkerForWait(normalizedRepoDir, wave, wave.Workers[0])
	if err != nil {
		t.Fatalf("inspect worker 1: %v", err)
	}
	if term1 {
		t.Fatalf("queued worker 1 must stay non-terminal, got %+v", cand1)
	}

	// 2. Worker 2 must be terminal failed (dead spawned), NOT treated as queued
	cand2, term2, err := inspectParallelWaveWorkerForWait(normalizedRepoDir, wave, wave.Workers[1])
	if err != nil {
		t.Fatalf("inspect worker 2: %v", err)
	}
	if !term2 || cand2.TerminalCondition != "failed" {
		t.Fatalf("dead spawned worker 2 must reach terminal condition 'failed', got term=%v cond=%q", term2, cand2.TerminalCondition)
	}

	// 3. Worker 3 (stalled launching) must be recovered back to worktree_ready by scheduleWorktreeReadyWaveWorkers
	var capturedArgs [][]string
	installFakeWaveWorkerLauncher(t, "", &capturedArgs)
	if err := scheduleWorktreeReadyWaveWorkers(context.Background(), normalizedRepoDir, wave, 1, 5*time.Second); err != nil {
		t.Fatalf("scheduleWorktreeReadyWaveWorkers failed: %v", err)
	}

	wave, err = fetchNetrunnerWaveSnapshot(created.WaveId, 1)
	if err != nil {
		t.Fatal(err)
	}
	// Worker 1 and Worker 3 should now be launched to running
	w1 := testWaveWorkerBySession(t, wave, created.Workers[0].SessionId)
	w3 := testWaveWorkerBySession(t, wave, created.Workers[2].SessionId)
	if w1.Status != parallelWaveWorkerStatusRunning || w1.WorkerProcessId <= 0 {
		t.Fatalf("worker 1 must be launched to running, got status=%q proc=%d", w1.Status, w1.WorkerProcessId)
	}
	if w3.Status != parallelWaveWorkerStatusRunning || w3.WorkerProcessId <= 0 {
		t.Fatalf("worker 3 must be recovered and launched to running, got status=%q proc=%d", w3.Status, w3.WorkerProcessId)
	}
}
