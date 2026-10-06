package main

import (
	"strings"
	"testing"
	"time"
)

// The operator summary must be truthful about worker runtime: running vs
// queued vs retry-waiting, live vs dead processes, and why each worker waits.
// The wire-compatible "active" counter keeps counting every nonterminal
// worker, but it is explicitly labelled as such.
func TestBuildNetrunnerWaveOperatorSummaryTruthfulRuntime(t *testing.T) {
	originalDB, originalRole, originalProjectID := db, authorizedRole, authorizedProjectId
	defer func() {
		db, authorizedRole, authorizedProjectId = originalDB, originalRole, originalProjectID
	}()
	repoDir, testDB, created, wave := setupRunningWaveTest(t)
	defer testDB.Close()
	_ = repoDir

	summary := buildNetrunnerWaveOperatorSummary(wave)
	if summary.ActiveMeaning != "nonterminal" {
		t.Fatalf("the wire-compatible active counter must be labelled, got %q", summary.ActiveMeaning)
	}
	if summary.WorkerCounts.Active != 2 || summary.WorkerCounts.Total != 2 {
		t.Fatalf("expected two active (nonterminal) workers, got %+v", summary.WorkerCounts)
	}
	if summary.WorkerCounts.Running != 2 || summary.WorkerCounts.Live != 2 {
		t.Fatalf("running/live semantics must be truthful, got %+v", summary.WorkerCounts)
	}
	if summary.WorkerCounts.Queued != 0 || summary.WorkerCounts.RetryWaiting != 0 {
		t.Fatalf("queued/retry counters must be zero for running workers, got %+v", summary.WorkerCounts)
	}
	for _, state := range summary.WorkerRuntime {
		if state.SchedulerDecision != "running" || state.CurrentPid <= 0 || !state.ProcessAlive {
			t.Fatalf("expected live running runtime state, got %+v", state)
		}
	}

	// One worker exits and stays retry_wait with its dead historical pid (the
	// stuck-868 shape); the other one's process dies while "running".
	workerA := testWaveWorkerBySession(t, wave, created.Workers[0].SessionId)
	workerB := testWaveWorkerBySession(t, wave, created.Workers[1].SessionId)
	_, err := testDB.Exec(
		"UPDATE parallel_wave_worker SET status = ?, retry_cause = 'provider_rate_limit', retry_next_eligible_at = ?, failure_reason = 'retryable provider rate limit' WHERE id = ?",
		parallelWaveWorkerStatusRetryWait,
		time.Now().UTC().Add(30*time.Minute).Format(time.RFC3339Nano),
		workerA.Id,
	)
	if err != nil {
		t.Fatalf("stage retry_wait worker: %v", err)
	}
	if _, err := testDB.Exec(
		"UPDATE worker_process SET pid = 0, status = ?, stop_reason = 'process exited', stopped_at = CURRENT_TIMESTAMP WHERE parallel_wave_id = ?",
		workerStatusExited, created.WaveId,
	); err != nil {
		t.Fatalf("kill worker processes on paper: %v", err)
	}
	refreshed, err := fetchNetrunnerWaveSnapshot(created.WaveId, 1)
	if err != nil {
		t.Fatalf("refresh wave: %v", err)
	}

	summary = buildNetrunnerWaveOperatorSummary(refreshed)
	if summary.WorkerCounts.RetryWaiting != 1 || summary.WorkerCounts.RetryPending != 1 {
		t.Fatalf("retry semantics must be truthful, got %+v", summary.WorkerCounts)
	}
	if summary.WorkerCounts.Running != 1 || summary.WorkerCounts.Live != 0 {
		t.Fatalf("live/running semantics must reflect the dead pids, got %+v", summary.WorkerCounts)
	}
	if summary.WorkerCounts.DeadProcess != 1 {
		t.Fatalf("a running worker with a dead pid must be reported, got %+v", summary.WorkerCounts)
	}
	// active stays wire-compatible: both workers are still nonterminal.
	if summary.WorkerCounts.Active != 2 {
		t.Fatalf("active must keep counting every nonterminal worker, got %d", summary.WorkerCounts.Active)
	}

	byWorker := map[int]NetrunnerWaveWorkerRuntimeState{}
	for _, state := range summary.WorkerRuntime {
		byWorker[state.WorkerId] = state
	}
	retryState := byWorker[workerA.Id]
	if retryState.RetryCause != "provider_rate_limit" ||
		retryState.RetryDueAt == "" ||
		retryState.SchedulerDecision != "retry_waiting_backoff" ||
		retryState.BlockingReason == "" {
		t.Fatalf("retry cause/due/blocking reason must be exposed, got %+v", retryState)
	}
	deadState := byWorker[workerB.Id]
	if deadState.SchedulerDecision != "process_dead_pending_reconcile" || deadState.ProcessAlive {
		t.Fatalf("a dead pid must not read as live, got %+v", deadState)
	}
}

func TestBuildNetrunnerWaveWorkerSchedulerDecisionTable(t *testing.T) {
	future := time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano)
	past := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)
	cases := []struct {
		name     string
		worker   NetrunnerWaveWorkerSnapshot
		terminal bool
		expect   string
	}{
		{"queued created", NetrunnerWaveWorkerSnapshot{Status: parallelWaveWorkerStatusCreated}, false, "queued_for_launch"},
		{"launching", NetrunnerWaveWorkerSnapshot{Status: parallelWaveWorkerStatusLaunching}, false, "launching"},
		{"repair wait", NetrunnerWaveWorkerSnapshot{Status: parallelWaveWorkerStatusRepairWait}, false, "waiting_governed_repair"},
		{"retry blocked at budget", NetrunnerWaveWorkerSnapshot{Status: parallelWaveWorkerStatusRetryWait, RetryAttemptCount: maxParallelWaveRetryAttempts}, false, "retry_blocked_max_attempts"},
		{"retry quota reset", NetrunnerWaveWorkerSnapshot{Status: parallelWaveWorkerStatusRetryWait, RetryCause: parallelWaveRetryCauseQuotaExhausted, RetryNextEligibleAt: future}, false, "retry_waiting_quota_reset"},
		{"retry backoff", NetrunnerWaveWorkerSnapshot{Status: parallelWaveWorkerStatusRetryWait, RetryNextEligibleAt: future}, false, "retry_waiting_backoff"},
		{"retry eligible", NetrunnerWaveWorkerSnapshot{Status: parallelWaveWorkerStatusRetryWait, RetryNextEligibleAt: past}, false, "retry_eligible_now"},
		{"retry missing eligibility", NetrunnerWaveWorkerSnapshot{Status: parallelWaveWorkerStatusRetryWait}, false, "retry_eligible_now"},
		{"terminal cancelled", NetrunnerWaveWorkerSnapshot{Status: parallelWaveWorkerStatusCancelled}, true, "terminal:cancelled"},
	}
	for _, tc := range cases {
		state := NetrunnerWaveWorkerRuntimeState{Status: tc.worker.Status}
		if got := parallelWaveWorkerSchedulerDecision(tc.worker, state, tc.terminal); got != tc.expect {
			t.Errorf("%s: expected %q, got %q", tc.name, tc.expect, got)
		}
	}
}

// A cancelled wave must surface as cancelled — never as completed — even
// though its lifecycle phase is closed for session governance.
func TestBuildNetrunnerWaveOperatorSummaryCancelledWaveIsNotCompleted(t *testing.T) {
	wave := NetrunnerWaveSnapshot{
		Id:           7,
		ProjectId:    1,
		Status:       parallelWaveStatusCancelled,
		Phase:        parallelWavePhaseCompleted,
		GateState:    parallelWaveGateClosed,
		ControlState: parallelWaveControlActive,
		Workers: []NetrunnerWaveWorkerSnapshot{
			{Id: 1, SessionId: 1, Status: parallelWaveWorkerStatusCancelled, FailureReason: "cancelled: operator decision"},
		},
	}
	summary := buildNetrunnerWaveOperatorSummary(wave)
	if summary.OperatorState != "cancelled" {
		t.Fatalf("a cancelled wave must read as cancelled, got %q (%s)", summary.OperatorState, summary.Label)
	}
	if summary.WaveCompleted {
		t.Fatal("a cancelled wave must never read as completed/accepted")
	}
	if summary.WorkerCounts.Cancelled != 1 || summary.WorkerCounts.Terminal != 1 {
		t.Fatalf("cancelled workers are terminal and labelled, got %+v", summary.WorkerCounts)
	}
	if summary.WorkerRuntime[0].BlockingReason == "" || !strings.Contains(summary.WorkerRuntime[0].BlockingReason, "cancelled") {
		t.Fatalf("the audit reason must surface as blocking reason, got %+v", summary.WorkerRuntime[0])
	}
}
