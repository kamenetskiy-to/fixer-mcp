package main

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParallelWaveFailurePauseReasonTable(t *testing.T) {
	worker := func(sessionID int, status string) NetrunnerWaveWorkerSnapshot {
		return NetrunnerWaveWorkerSnapshot{SessionId: sessionID, Status: status}
	}
	tests := []struct {
		name         string
		workers      []NetrunnerWaveWorkerSnapshot
		dependencies []WaveDependency
		wantReason   string
	}{
		{
			name:    "N1 first failure requires governed repair",
			workers: []NetrunnerWaveWorkerSnapshot{worker(1, parallelWaveWorkerStatusFailed)},
		},
		{
			name: "N2 one failure remains diagnosable",
			workers: []NetrunnerWaveWorkerSnapshot{
				worker(1, parallelWaveWorkerStatusFailed),
				worker(2, parallelWaveWorkerStatusRunning),
			},
		},
		{
			name: "N2 all failed pauses",
			workers: []NetrunnerWaveWorkerSnapshot{
				worker(1, parallelWaveWorkerStatusFailed),
				worker(2, parallelWaveWorkerStatusFailed),
			},
			wantReason: "failed_worker_majority:2/2",
		},
		{
			name: "N2 exact tie does not pause after cohort terminal",
			workers: []NetrunnerWaveWorkerSnapshot{
				worker(1, parallelWaveWorkerStatusFailed),
				worker(2, parallelWaveWorkerStatusCompleted),
			},
		},
		{
			name: "N3 two failures pauses",
			workers: []NetrunnerWaveWorkerSnapshot{
				worker(1, parallelWaveWorkerStatusFailed),
				worker(2, parallelWaveWorkerStatusFailed),
				worker(3, parallelWaveWorkerStatusRunning),
			},
			wantReason: "failed_worker_majority:2/3",
		},
		{
			name: "N4 half failed does not pause",
			workers: []NetrunnerWaveWorkerSnapshot{
				worker(1, parallelWaveWorkerStatusFailed),
				worker(2, parallelWaveWorkerStatusFailed),
				worker(3, parallelWaveWorkerStatusRunning),
				worker(4, parallelWaveWorkerStatusRunning),
			},
		},
		{
			name: "N4 exact tie does not pause after cohort terminal",
			workers: []NetrunnerWaveWorkerSnapshot{
				worker(1, parallelWaveWorkerStatusFailed),
				worker(2, parallelWaveWorkerStatusFailed),
				worker(3, parallelWaveWorkerStatusCompleted),
				worker(4, parallelWaveWorkerStatusReviewReady),
			},
		},
		{
			name: "single failed initial root requires repair before child launch",
			workers: []NetrunnerWaveWorkerSnapshot{
				worker(1, parallelWaveWorkerStatusFailed),
				worker(2, parallelWaveWorkerStatusCreated),
			},
			dependencies: []WaveDependency{{Child: 2, Parents: []int64{1}}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			wave := NetrunnerWaveSnapshot{Workers: test.workers, Dependencies: test.dependencies}
			if got := parallelWaveFailurePauseReason(wave); got != test.wantReason {
				t.Fatalf("parallelWaveFailurePauseReason() = %q, want %q", got, test.wantReason)
			}
		})
	}
}

func TestParallelWaveFailurePolicyUsesOnlyScheduledWorkers(t *testing.T) {
	worker := func(id int, status string) NetrunnerWaveWorkerSnapshot {
		return NetrunnerWaveWorkerSnapshot{Id: id, SessionId: id, Status: status}
	}
	tests := []struct {
		name       string
		wave       NetrunnerWaveSnapshot
		wantState  string
		wantReason string
	}{
		{
			name: "deferred child failure joins cohort",
			wave: NetrunnerWaveSnapshot{
				Workers: []NetrunnerWaveWorkerSnapshot{
					worker(1, parallelWaveWorkerStatusCompleted),
					worker(2, parallelWaveWorkerStatusFailed),
				},
				Dependencies: []WaveDependency{{Child: 2, Parents: []int64{1}}},
			},
			wantState:  parallelWaveFailurePolicyRepairRequired,
			wantReason: "governed_repair_required:1/2",
		},
		{
			name: "unlaunched descendant does not dilute initial tie",
			wave: NetrunnerWaveSnapshot{
				Workers: []NetrunnerWaveWorkerSnapshot{
					worker(1, parallelWaveWorkerStatusCompleted),
					worker(2, parallelWaveWorkerStatusFailed),
					worker(3, parallelWaveWorkerStatusCreated),
				},
				Dependencies: []WaveDependency{{Child: 3, Parents: []int64{1, 2}}},
			},
			wantState:  parallelWaveFailurePolicyRepairRequired,
			wantReason: "governed_repair_required:1/2",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision := decideParallelWaveFailurePolicy(test.wave)
			if decision.State != test.wantState || decision.Reason != test.wantReason {
				t.Fatalf("unexpected failure decision: got %+v want state=%q reason=%q", decision, test.wantState, test.wantReason)
			}
		})
	}
}

func TestScheduleCreatedParallelWaveWorkersBlocksFailedParentChildrenAndReleasesLease(t *testing.T) {
	originalDB, originalRole, originalProjectID := db, authorizedRole, authorizedProjectId
	defer func() { db, authorizedRole, authorizedProjectId = originalDB, originalRole, originalProjectID }()
	repoDir := setupCleanGitRepo(t)
	testDB := setupParallelWaveTestDB(t, repoDir)
	defer testDB.Close()
	db, authorizedRole, authorizedProjectId = testDB, "fixer", 1

	_, created, err := CreateNetrunnerWave(context.Background(), nil, CreateNetrunnerWaveInput{
		SessionIds:   []int{1, 2},
		Dependencies: []WaveDependency{{Child: 2, Parents: []int64{1}}},
	})
	if err != nil {
		t.Fatalf("create dependency wave: %v", err)
	}
	parent := testWaveWorkerBySession(t, created.Wave, 1)
	if _, err := testDB.Exec(
		`UPDATE parallel_wave_worker
		 SET status = ?, terminal_outcome = ?, failure_reason = ?, terminal_at = CURRENT_TIMESTAMP
		 WHERE id = ?`,
		parallelWaveWorkerStatusFailed,
		parallelWaveWorkerStatusFailed,
		"provider exited",
		parent.Id,
	); err != nil {
		t.Fatalf("seed failed parent: %v", err)
	}

	wave, err := fetchNetrunnerWaveSnapshot(created.WaveId, 1)
	if err != nil {
		t.Fatalf("fetch dependency wave: %v", err)
	}
	if err := scheduleCreatedParallelWaveWorkers(context.Background(), repoDir, wave, wave.OrchestrationEpoch, time.Second); err != nil {
		t.Fatalf("schedule dependency children: %v", err)
	}

	refreshed, err := fetchNetrunnerWaveSnapshot(created.WaveId, 1)
	if err != nil {
		t.Fatalf("fetch blocked child: %v", err)
	}
	child := testWaveWorkerBySession(t, refreshed, 2)
	if child.Status != parallelWaveWorkerStatusBlocked || !strings.Contains(child.FailureReason, "parent session 1") {
		t.Fatalf("failed-parent child must be blocked with reason, got %+v", child)
	}
	var activeLeases int
	if err := testDB.QueryRow(
		"SELECT COUNT(*) FROM parallel_wave_scope_lease WHERE wave_id = ? AND active = 1",
		created.WaveId,
	).Scan(&activeLeases); err != nil {
		t.Fatalf("count child leases: %v", err)
	}
	if activeLeases != 1 {
		t.Fatalf("blocking child must release only its lease while failed parent remains leased, got %d active leases", activeLeases)
	}
	decision := decideParallelWaveFailurePolicy(refreshed)
	if decision.State != parallelWaveFailurePolicyRepairRequired || decision.WorkerID != parent.Id {
		t.Fatalf("blocked dependent must not count as an additional provider failure: %+v", decision)
	}
}

func TestParallelWaveFailurePolicyAfterGovernedRepair(t *testing.T) {
	worker := func(id int, status string) NetrunnerWaveWorkerSnapshot {
		return NetrunnerWaveWorkerSnapshot{Id: id, SessionId: id, Status: status}
	}
	tests := []struct {
		name       string
		workers    []NetrunnerWaveWorkerSnapshot
		wantState  string
		wantReason string
	}{
		{
			name:       "one worker failure still pauses",
			workers:    []NetrunnerWaveWorkerSnapshot{worker(1, parallelWaveWorkerStatusFailed)},
			wantState:  parallelWaveFailurePolicyPaused,
			wantReason: "failed_worker_majority:1/1",
		},
		{
			name: "one of two is manual repair",
			workers: []NetrunnerWaveWorkerSnapshot{
				worker(1, parallelWaveWorkerStatusFailed),
				worker(2, parallelWaveWorkerStatusCompleted),
			},
			wantState:  parallelWaveFailurePolicyManualRepair,
			wantReason: "manual_repair_required:1/2",
		},
		{
			name: "two of four is manual repair",
			workers: []NetrunnerWaveWorkerSnapshot{
				worker(1, parallelWaveWorkerStatusFailed),
				worker(2, parallelWaveWorkerStatusFailed),
				worker(3, parallelWaveWorkerStatusCompleted),
				worker(4, parallelWaveWorkerStatusReviewReady),
			},
			wantState:  parallelWaveFailurePolicyManualRepair,
			wantReason: "manual_repair_required:2/4",
		},
		{
			name: "two of three still pauses",
			workers: []NetrunnerWaveWorkerSnapshot{
				worker(1, parallelWaveWorkerStatusFailed),
				worker(2, parallelWaveWorkerStatusFailed),
				worker(3, parallelWaveWorkerStatusCompleted),
			},
			wantState:  parallelWaveFailurePolicyPaused,
			wantReason: "failed_worker_majority:2/3",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision := decideParallelWaveFailurePolicy(NetrunnerWaveSnapshot{
				Workers:            test.workers,
				FailurePolicyState: parallelWaveFailurePolicyRepairInProgress,
				RepairAttemptCount: 1,
				RepairWorkerId:     test.workers[0].Id,
			})
			if decision.State != test.wantState || decision.Reason != test.wantReason {
				t.Fatalf("unexpected post-repair decision: got %+v want state=%q reason=%q", decision, test.wantState, test.wantReason)
			}
		})
	}
}

func markTestWaveCompletedWithHandoff(t *testing.T, testDB interface {
	Exec(string, ...any) (sql.Result, error)
}, waveID int, handoffSHA string) {
	t.Helper()
	if _, err := testDB.Exec(
		"UPDATE parallel_wave SET phase = ?, gate_state = ?, handoff_sha = ? WHERE id = ?",
		parallelWavePhaseCompleted,
		parallelWaveGateClosed,
		handoffSHA,
		waveID,
	); err != nil {
		t.Fatalf("mark wave %d completed with handoff: %v", waveID, err)
	}
	if _, err := testDB.Exec("UPDATE parallel_wave_scope_lease SET active = 0, released_at = CURRENT_TIMESTAMP WHERE wave_id = ?", waveID); err != nil {
		t.Fatalf("release wave %d leases: %v", waveID, err)
	}
}

func TestCreateNetrunnerWaveRecursiveLineageDecrementsAndExhaustsDepth(t *testing.T) {
	originalDB, originalRole, originalProjectID := db, authorizedRole, authorizedProjectId
	defer func() {
		db, authorizedRole, authorizedProjectId = originalDB, originalRole, originalProjectID
	}()

	repoDir := setupCleanGitRepo(t)
	testDB := setupParallelWaveTestDB(t, repoDir)
	defer testDB.Close()
	db, authorizedRole, authorizedProjectId = testDB, "fixer", 1

	_, root, err := CreateNetrunnerWave(context.Background(), nil, CreateNetrunnerWaveInput{
		SessionIds:              []int{1},
		MaxChildWaveDepth:       2,
		MaxTotalDescendantWaves: 3,
		MaxTotalSessions:        4,
	})
	if err != nil {
		t.Fatalf("create recursive root: %v", err)
	}
	if root.Wave.RootWaveId != root.WaveId || root.Wave.Depth != 0 || root.Wave.MaxChildWaveDepth != 2 {
		t.Fatalf("unexpected root lineage: %+v", root.Wave)
	}
	handoffSHA := runGitTestCommand(t, repoDir, "rev-parse", "HEAD^{commit}")
	markTestWaveCompletedWithHandoff(t, testDB, root.WaveId, handoffSHA)

	_, child, err := CreateNetrunnerWave(context.Background(), nil, CreateNetrunnerWaveInput{
		SessionIds:   []int{2},
		ParentWaveId: root.WaveId,
	})
	if err != nil {
		t.Fatalf("create child wave: %v", err)
	}
	if child.Wave.ParentWaveId != root.WaveId || child.Wave.RootWaveId != root.WaveId || child.Wave.Depth != 1 || child.Wave.MaxChildWaveDepth != 1 {
		t.Fatalf("unexpected child lineage: %+v", child.Wave)
	}
	markTestWaveCompletedWithHandoff(t, testDB, child.WaveId, handoffSHA)

	if _, err := testDB.Exec("INSERT INTO session (project_id, task_description, status, declared_write_scope) VALUES (1, 'grandchild', 'pending', '[\"docs/c\"]')"); err != nil {
		t.Fatalf("seed grandchild session: %v", err)
	}
	_, grandchild, err := CreateNetrunnerWave(context.Background(), nil, CreateNetrunnerWaveInput{
		SessionIds:   []int{3},
		ParentWaveId: child.WaveId,
	})
	if err != nil {
		t.Fatalf("create grandchild wave: %v", err)
	}
	if grandchild.Wave.Depth != 2 || grandchild.Wave.MaxChildWaveDepth != 0 {
		t.Fatalf("expected exhausted grandchild depth, got %+v", grandchild.Wave)
	}

	if _, err := testDB.Exec("INSERT INTO session (project_id, task_description, status, declared_write_scope) VALUES (1, 'too deep', 'pending', '[\"docs/d\"]')"); err != nil {
		t.Fatalf("seed too-deep session: %v", err)
	}
	callResult, _, err := CreateNetrunnerWave(context.Background(), nil, CreateNetrunnerWaveInput{
		SessionIds:   []int{4},
		ParentWaveId: grandchild.WaveId,
	})
	if err == nil || !strings.Contains(err.Error(), "exhausted max_child_wave_depth") {
		t.Fatalf("expected depth exhaustion, result=%+v err=%v", callResult, err)
	}
}

func TestChildWaveRequiresCompletedParentAndExactCommittedHandoff(t *testing.T) {
	originalDB, originalRole, originalProjectID := db, authorizedRole, authorizedProjectId
	defer func() { db, authorizedRole, authorizedProjectId = originalDB, originalRole, originalProjectID }()
	repoDir := setupCleanGitRepo(t)
	testDB := setupParallelWaveTestDB(t, repoDir)
	defer testDB.Close()
	db, authorizedRole, authorizedProjectId = testDB, "fixer", 1

	_, root, err := CreateNetrunnerWave(context.Background(), nil, CreateNetrunnerWaveInput{
		SessionIds:              []int{1},
		MaxChildWaveDepth:       1,
		MaxTotalDescendantWaves: 2,
		MaxTotalSessions:        3,
	})
	if err != nil {
		t.Fatalf("create recursive parent: %v", err)
	}
	if _, _, err := CreateNetrunnerWave(context.Background(), nil, CreateNetrunnerWaveInput{SessionIds: []int{2}, ParentWaveId: root.WaveId}); err == nil || !strings.Contains(err.Error(), "accepted/completed") {
		t.Fatalf("expected running parent rejection, got %v", err)
	}
	handoffSHA := runGitTestCommand(t, repoDir, "rev-parse", "HEAD^{commit}")
	markTestWaveCompletedWithHandoff(t, testDB, root.WaveId, handoffSHA)
	if err := os.WriteFile(filepath.Join(repoDir, "NEXT.md"), []byte("next\n"), 0o644); err != nil {
		t.Fatalf("write next commit: %v", err)
	}
	runGitTestCommand(t, repoDir, "add", "NEXT.md")
	runGitTestCommand(t, repoDir, "-c", "user.name=Fixer Test", "-c", "user.email=fixer@example.test", "commit", "-m", "advance base")
	if _, _, err := CreateNetrunnerWave(context.Background(), nil, CreateNetrunnerWaveInput{SessionIds: []int{2}, ParentWaveId: root.WaveId}); err == nil || !strings.Contains(err.Error(), "must match parent") {
		t.Fatalf("expected mismatched handoff rejection, got %v", err)
	}
	if _, child, err := CreateNetrunnerWave(context.Background(), nil, CreateNetrunnerWaveInput{SessionIds: []int{2}, ParentWaveId: root.WaveId, BaseRef: handoffSHA}); err != nil || child.Wave.BaseSha != handoffSHA {
		t.Fatalf("expected valid committed handoff child: child=%+v err=%v", child, err)
	}
}

func TestCreateNetrunnerWaveEnforcesTreeBudgets(t *testing.T) {
	tests := []struct {
		name           string
		maxDescendants int
		maxSessions    int
		errorFragment  string
	}{
		{name: "descendant wave budget", maxDescendants: 1, maxSessions: 3, errorFragment: "max_total_descendant_waves"},
		{name: "tree session budget", maxDescendants: 2, maxSessions: 2, errorFragment: "session budget"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			originalDB, originalRole, originalProjectID := db, authorizedRole, authorizedProjectId
			defer func() {
				db, authorizedRole, authorizedProjectId = originalDB, originalRole, originalProjectID
			}()
			repoDir := setupCleanGitRepo(t)
			testDB := setupParallelWaveTestDB(t, repoDir)
			defer testDB.Close()
			db, authorizedRole, authorizedProjectId = testDB, "fixer", 1
			_, root, err := CreateNetrunnerWave(context.Background(), nil, CreateNetrunnerWaveInput{
				SessionIds:              []int{1},
				MaxChildWaveDepth:       2,
				MaxTotalDescendantWaves: test.maxDescendants,
				MaxTotalSessions:        test.maxSessions,
			})
			if err != nil {
				t.Fatalf("create budgeted root: %v", err)
			}
			handoffSHA := runGitTestCommand(t, repoDir, "rev-parse", "HEAD^{commit}")
			markTestWaveCompletedWithHandoff(t, testDB, root.WaveId, handoffSHA)
			if _, _, err := CreateNetrunnerWave(context.Background(), nil, CreateNetrunnerWaveInput{SessionIds: []int{2}, ParentWaveId: root.WaveId}); err != nil {
				t.Fatalf("create first budgeted child: %v", err)
			}
			if _, err := testDB.Exec("INSERT INTO session (project_id, task_description, status, declared_write_scope) VALUES (1, 'budget overflow', 'pending', '[\"docs/c\"]')"); err != nil {
				t.Fatalf("seed overflow session: %v", err)
			}
			_, _, err = CreateNetrunnerWave(context.Background(), nil, CreateNetrunnerWaveInput{SessionIds: []int{3}, ParentWaveId: root.WaveId})
			if err == nil || !strings.Contains(err.Error(), test.errorFragment) {
				t.Fatalf("expected %s rejection, got %v", test.errorFragment, err)
			}
		})
	}
}

func TestPrepareParallelWaveLineageRejectsCrossProjectCycleEscalationAndNegativeValues(t *testing.T) {
	originalDB, originalRole, originalProjectID := db, authorizedRole, authorizedProjectId
	defer func() {
		db, authorizedRole, authorizedProjectId = originalDB, originalRole, originalProjectID
	}()
	repoDir := setupCleanGitRepo(t)
	testDB := setupParallelWaveTestDB(t, repoDir)
	defer testDB.Close()
	db, authorizedRole, authorizedProjectId = testDB, "fixer", 1

	_, root, err := CreateNetrunnerWave(context.Background(), nil, CreateNetrunnerWaveInput{
		SessionIds:              []int{1},
		MaxChildWaveDepth:       2,
		MaxTotalDescendantWaves: 2,
		MaxTotalSessions:        4,
	})
	if err != nil {
		t.Fatalf("create lineage root: %v", err)
	}

	if _, err := prepareParallelWaveLineage(CreateNetrunnerWaveInput{ParentWaveId: root.WaveId}, 2, 1); err == nil || !strings.Contains(err.Error(), "another project") {
		t.Fatalf("expected cross-project parent rejection, got %v", err)
	}
	if _, err := prepareParallelWaveLineage(CreateNetrunnerWaveInput{MaxChildWaveDepth: -1}, 1, 1); err == nil || !strings.Contains(err.Error(), "negative") {
		t.Fatalf("expected negative depth rejection, got %v", err)
	}
	if _, err := prepareParallelWaveLineage(CreateNetrunnerWaveInput{ParentWaveId: root.WaveId, MaxChildWaveDepth: 3}, 1, 1); err == nil || !strings.Contains(err.Error(), "must not override") {
		t.Fatalf("expected child escalation rejection, got %v", err)
	}
	if _, err := testDB.Exec("UPDATE parallel_wave SET parent_wave_id = id WHERE id = ?", root.WaveId); err != nil {
		t.Fatalf("corrupt lineage cycle: %v", err)
	}
	if _, err := prepareParallelWaveLineage(CreateNetrunnerWaveInput{ParentWaveId: root.WaveId}, 1, 1); err == nil {
		t.Fatal("expected corrupted cyclic lineage rejection")
	}
}

func TestCreateNetrunnerWaveRejectsDuplicateSessionAndCrossWaveScopeLease(t *testing.T) {
	originalDB, originalRole, originalProjectID := db, authorizedRole, authorizedProjectId
	defer func() { db, authorizedRole, authorizedProjectId = originalDB, originalRole, originalProjectID }()
	repoDir := setupCleanGitRepo(t)
	testDB := setupParallelWaveTestDB(t, repoDir)
	defer testDB.Close()
	db, authorizedRole, authorizedProjectId = testDB, "fixer", 1

	_, first, err := CreateNetrunnerWave(context.Background(), nil, CreateNetrunnerWaveInput{SessionIds: []int{1}})
	if err != nil {
		t.Fatalf("create first leased wave: %v", err)
	}
	if _, _, err := CreateNetrunnerWave(context.Background(), nil, CreateNetrunnerWaveInput{SessionIds: []int{1}}); err == nil || !strings.Contains(err.Error(), "already linked") {
		t.Fatalf("expected duplicate-session wave rejection, got %v", err)
	}
	if _, err := testDB.Exec("INSERT INTO session (project_id, task_description, status, declared_write_scope) VALUES (1, 'overlap', 'pending', '[\"docs/a/subtree\"]')"); err != nil {
		t.Fatalf("seed overlapping session: %v", err)
	}
	if _, _, err := CreateNetrunnerWave(context.Background(), nil, CreateNetrunnerWaveInput{SessionIds: []int{3}}); err == nil || !strings.Contains(err.Error(), "overlaps active wave") {
		t.Fatalf("expected prefix-overlap lease rejection, got %v", err)
	}
	if _, _, err := CreateNetrunnerWave(context.Background(), nil, CreateNetrunnerWaveInput{SessionIds: []int{2}}); err != nil {
		t.Fatalf("disjoint concurrent wave should remain admissible: %v", err)
	}
	var activeLeases int
	if err := testDB.QueryRow("SELECT COUNT(*) FROM parallel_wave_scope_lease WHERE wave_id = ? AND active = 1", first.WaveId).Scan(&activeLeases); err != nil || activeLeases != 1 {
		t.Fatalf("expected one durable active scope lease, count=%d err=%v", activeLeases, err)
	}
}

func TestAuthorizeNetrunnerWaveRepairIsDurableAndSingleUse(t *testing.T) {
	originalDB, originalRole, originalProjectID := db, authorizedRole, authorizedProjectId
	defer func() { db, authorizedRole, authorizedProjectId = originalDB, originalRole, originalProjectID }()
	repoDir := setupCleanGitRepo(t)
	testDB := setupParallelWaveTestDB(t, repoDir)
	defer testDB.Close()
	db, authorizedRole, authorizedProjectId = testDB, "fixer", 1

	_, created, err := CreateNetrunnerWave(context.Background(), nil, CreateNetrunnerWaveInput{SessionIds: []int{1}})
	if err != nil {
		t.Fatalf("create repair-policy wave: %v", err)
	}
	workerID := created.Wave.Workers[0].Id
	if _, err := testDB.Exec("UPDATE parallel_wave SET status = ? WHERE id = ?", parallelWaveStatusFailed, created.WaveId); err != nil {
		t.Fatalf("mark launch failed: %v", err)
	}
	if err := markParallelWaveWorkerFailed(workerID, 1, "initial launch failed"); err != nil {
		t.Fatalf("mark worker failed: %v", err)
	}
	wave, err := fetchNetrunnerWaveSnapshot(created.WaveId, 1)
	if err != nil {
		t.Fatalf("fetch failed wave: %v", err)
	}
	wave, err = reconcileParallelWaveFailureControl(wave)
	if err != nil || wave.FailurePolicyState != parallelWaveFailurePolicyRepairRequired {
		t.Fatalf("expected durable repair-required state: wave=%+v err=%v", wave, err)
	}
	if _, authorized, err := AuthorizeNetrunnerWaveRepair(context.Background(), nil, AuthorizeNetrunnerWaveRepairInput{WaveId: wave.Id, WorkerSessionId: 1}); err != nil || authorized.Wave.RepairAttemptCount != 1 || authorized.Wave.FailurePolicyState != parallelWaveFailurePolicyRepairAuthorized {
		t.Fatalf("authorize governed repair: output=%+v err=%v", authorized, err)
	}
	if _, _, err := AuthorizeNetrunnerWaveRepair(context.Background(), nil, AuthorizeNetrunnerWaveRepairInput{WaveId: wave.Id, WorkerSessionId: 1}); err == nil {
		t.Fatal("expected second governed repair authorization to be rejected")
	}
	refetched, err := fetchNetrunnerWaveSnapshot(wave.Id, 1)
	if err != nil || refetched.RepairAttemptCount != 1 || refetched.Workers[0].Status != parallelWaveWorkerStatusRepairWait {
		t.Fatalf("governed repair state did not persist: wave=%+v err=%v", refetched, err)
	}
	if _, err := testDB.Exec("UPDATE parallel_wave SET failure_policy_state = ? WHERE id = ?", parallelWaveFailurePolicyRepairInProgress, wave.Id); err != nil {
		t.Fatalf("mark repair in progress: %v", err)
	}
	if err := markParallelWaveWorkerFailed(workerID, 1, "governed repair failed"); err != nil {
		t.Fatalf("mark governed repair failed: %v", err)
	}
	refetched, err = fetchNetrunnerWaveSnapshot(wave.Id, 1)
	if err != nil {
		t.Fatalf("fetch failed repair: %v", err)
	}
	refetched, err = reconcileParallelWaveFailureControl(refetched)
	if err != nil || refetched.ControlState != parallelWaveControlPausedForArchitect || refetched.FailurePolicyState != parallelWaveFailurePolicyPaused {
		t.Fatalf("failed governed repair must pause for Architect: wave=%+v err=%v", refetched, err)
	}
}

func TestReconcilePostRepairTieExposesManualRepairWithoutArchitectPause(t *testing.T) {
	originalDB, originalRole, originalProjectID := db, authorizedRole, authorizedProjectId
	defer func() { db, authorizedRole, authorizedProjectId = originalDB, originalRole, originalProjectID }()
	repoDir := setupCleanGitRepo(t)
	testDB := setupParallelWaveTestDB(t, repoDir)
	defer testDB.Close()
	db, authorizedRole, authorizedProjectId = testDB, "fixer", 1

	_, created, err := CreateNetrunnerWave(context.Background(), nil, CreateNetrunnerWaveInput{SessionIds: []int{1, 2}})
	if err != nil {
		t.Fatalf("create post-repair wave: %v", err)
	}
	failedWorker := created.Wave.Workers[0]
	completedWorker := created.Wave.Workers[1]
	if _, err := testDB.Exec(
		"UPDATE parallel_wave_worker SET status = ?, terminal_at = CURRENT_TIMESTAMP WHERE id = ?",
		parallelWaveWorkerStatusFailed,
		failedWorker.Id,
	); err != nil {
		t.Fatalf("mark repaired worker failed: %v", err)
	}
	if _, err := testDB.Exec(
		"UPDATE parallel_wave_worker SET status = ?, terminal_at = CURRENT_TIMESTAMP WHERE id = ?",
		parallelWaveWorkerStatusCompleted,
		completedWorker.Id,
	); err != nil {
		t.Fatalf("mark peer worker completed: %v", err)
	}
	if _, err := testDB.Exec(
		`UPDATE parallel_wave
		 SET failure_policy_state = ?, repair_worker_id = ?, repair_attempt_count = 1
		 WHERE id = ?`,
		parallelWaveFailurePolicyRepairInProgress,
		failedWorker.Id,
		created.WaveId,
	); err != nil {
		t.Fatalf("mark governed repair consumed: %v", err)
	}

	wave, err := fetchNetrunnerWaveSnapshot(created.WaveId, 1)
	if err != nil {
		t.Fatalf("fetch post-repair tie: %v", err)
	}
	wave, err = reconcileParallelWaveFailureControl(wave)
	if err != nil {
		t.Fatalf("reconcile post-repair tie: %v", err)
	}
	if wave.ControlState != parallelWaveControlActive ||
		wave.FailurePolicyState != parallelWaveFailurePolicyManualRepair ||
		wave.GateState != parallelWaveGateImplementationRepair ||
		wave.ControlReason != "manual_repair_required:1/2" ||
		wave.RepairWorkerId != failedWorker.Id {
		t.Fatalf("post-repair tie must remain active but require manual repair: %+v", wave)
	}
}

func TestSuccessfulGovernedRepairPassesAndReleasesDeferredWorker(t *testing.T) {
	originalDB, originalRole, originalProjectID, originalExecCommand := db, authorizedRole, authorizedProjectId, execCommand
	defer func() {
		db, authorizedRole, authorizedProjectId, execCommand = originalDB, originalRole, originalProjectID, originalExecCommand
	}()
	repoDir := setupCleanGitRepo(t)
	testDB := setupParallelWaveTestDB(t, repoDir)
	defer testDB.Close()
	db, authorizedRole, authorizedProjectId = testDB, "fixer", 1

	_, created, err := CreateNetrunnerWave(context.Background(), nil, CreateNetrunnerWaveInput{
		SessionIds:   []int{1, 2},
		Dependencies: []WaveDependency{{Child: 2, Parents: []int64{1}}},
	})
	if err != nil {
		t.Fatalf("create governed-repair wave: %v", err)
	}
	seedWaveSessionExternalLink(t, testDB, 1)
	seedWaveSessionExternalLink(t, testDB, 2)
	installFakeWaveWorkerLauncher(t, "", nil)
	if _, _, err := LaunchNetrunnerWave(context.Background(), nil, LaunchNetrunnerWaveInput{WaveId: created.WaveId, TimeoutSeconds: 1}); err != nil {
		t.Fatalf("launch repair candidate: %v", err)
	}

	wave, err := fetchNetrunnerWaveSnapshot(created.WaveId, 1)
	if err != nil {
		t.Fatalf("fetch launched repair wave: %v", err)
	}
	parent := testWaveWorkerBySession(t, wave, 1)
	if err := markParallelWaveWorkerFailed(parent.Id, 1, "initial implementation failed"); err != nil {
		t.Fatalf("mark initial failure: %v", err)
	}
	wave, err = fetchNetrunnerWaveSnapshot(created.WaveId, 1)
	if err != nil {
		t.Fatalf("fetch failed repair wave: %v", err)
	}
	wave, err = reconcileParallelWaveFailureControl(wave)
	if err != nil || wave.FailurePolicyState != parallelWaveFailurePolicyRepairRequired {
		t.Fatalf("expected governed repair gate: wave=%+v err=%v", wave, err)
	}
	if _, _, err := AuthorizeNetrunnerWaveRepair(context.Background(), nil, AuthorizeNetrunnerWaveRepairInput{WaveId: wave.Id, WorkerSessionId: 1}); err != nil {
		t.Fatalf("authorize governed repair: %v", err)
	}
	if _, err := testDB.Exec(
		`UPDATE parallel_wave SET failure_policy_state = ? WHERE id = ?`,
		parallelWaveFailurePolicyRepairInProgress,
		wave.Id,
	); err != nil {
		t.Fatalf("mark repair in progress: %v", err)
	}
	if _, err := testDB.Exec(
		`UPDATE parallel_wave_worker
		 SET status = ?, head_sha = base_sha, failure_reason = '', terminal_at = CURRENT_TIMESTAMP
		 WHERE id = ?`,
		parallelWaveWorkerStatusReviewReady,
		parent.Id,
	); err != nil {
		t.Fatalf("mark governed repair successful: %v", err)
	}

	wave, err = fetchNetrunnerWaveSnapshot(wave.Id, 1)
	if err != nil {
		t.Fatalf("fetch successful repair: %v", err)
	}
	wave, err = reconcileParallelWaveFailureControl(wave)
	if err != nil || wave.FailurePolicyState != parallelWaveFailurePolicyPassed || wave.ControlState != parallelWaveControlActive {
		t.Fatalf("successful repair must pass failure policy: wave=%+v err=%v", wave, err)
	}
	if err := scheduleCreatedParallelWaveWorkers(context.Background(), repoDir, wave, wave.OrchestrationEpoch, time.Second); err != nil {
		t.Fatalf("release deferred worker after repair: %v", err)
	}
	wave, err = fetchNetrunnerWaveSnapshot(wave.Id, 1)
	if err != nil {
		t.Fatalf("fetch released deferred worker: %v", err)
	}
	child := testWaveWorkerBySession(t, wave, 2)
	if child.Status != parallelWaveWorkerStatusRunning {
		t.Fatalf("expected deferred child to launch after successful repair, got %+v", child)
	}
}

func TestParallelWaveWorkerRetryAndTerminalOutcomePersist(t *testing.T) {
	originalDB, originalRole, originalProjectID := db, authorizedRole, authorizedProjectId
	defer func() { db, authorizedRole, authorizedProjectId = originalDB, originalRole, originalProjectID }()
	repoDir := setupCleanGitRepo(t)
	testDB := setupParallelWaveTestDB(t, repoDir)
	defer testDB.Close()
	db, authorizedRole, authorizedProjectId = testDB, "fixer", 1

	_, created, err := CreateNetrunnerWave(context.Background(), nil, CreateNetrunnerWaveInput{SessionIds: []int{1}})
	if err != nil {
		t.Fatalf("create persistence wave: %v", err)
	}
	worker := created.Wave.Workers[0]
	if err := markParallelWaveWorkerProviderRetryWait(worker); err != nil {
		t.Fatalf("persist provider retry wait: %v", err)
	}
	retried, err := fetchNetrunnerWaveSnapshot(created.WaveId, 1)
	if err != nil {
		t.Fatalf("fetch persisted retry: %v", err)
	}
	if retried.Workers[0].Status != parallelWaveWorkerStatusRetryWait || retried.Workers[0].RetryCause != "provider_rate_limit" || retried.Workers[0].RetryNextEligibleAt == "" {
		t.Fatalf("retry state is not durable: %+v", retried.Workers[0])
	}
	if err := updateParallelWaveWorkerTerminal(worker.Id, 1, parallelWaveWorkerStatusFailed, "repair failed", created.BaseSha, nil, "", ""); err != nil {
		t.Fatalf("persist terminal worker outcome: %v", err)
	}
	terminal, err := fetchNetrunnerWaveSnapshot(created.WaveId, 1)
	if err != nil {
		t.Fatalf("fetch terminal worker: %v", err)
	}
	if err := updateParallelWaveWorkerCleanup(terminal.Workers[0], 1, parallelWaveCleanupStatusCleaned, "", true); err != nil {
		t.Fatalf("clean terminal worker: %v", err)
	}
	cleaned, err := fetchNetrunnerWaveSnapshot(created.WaveId, 1)
	if err != nil {
		t.Fatalf("fetch cleaned worker: %v", err)
	}
	if cleaned.Workers[0].Status != parallelWaveWorkerStatusCleaned || cleaned.Workers[0].TerminalOutcome != parallelWaveWorkerStatusFailed {
		t.Fatalf("cleanup erased immutable terminal outcome: %+v", cleaned.Workers[0])
	}
}

func TestReconcileParallelWaveFailureControlPausesOnlyTheWave(t *testing.T) {
	originalDB, originalRole, originalProjectID := db, authorizedRole, authorizedProjectId
	defer func() {
		db, authorizedRole, authorizedProjectId = originalDB, originalRole, originalProjectID
	}()
	repoDir := setupCleanGitRepo(t)
	testDB := setupParallelWaveTestDB(t, repoDir)
	defer testDB.Close()
	if _, err := testDB.Exec("INSERT INTO session (project_id, task_description, status, declared_write_scope) VALUES (1, 'Task D', 'pending', '[\"docs/c\"]')"); err != nil {
		t.Fatalf("seed third session: %v", err)
	}
	db, authorizedRole, authorizedProjectId = testDB, "fixer", 1
	_, created, err := CreateNetrunnerWave(context.Background(), nil, CreateNetrunnerWaveInput{SessionIds: []int{1, 2, 3}})
	if err != nil {
		t.Fatalf("create failure-reconciliation wave: %v", err)
	}
	if _, err := testDB.Exec("UPDATE parallel_wave_worker SET status = ? WHERE id IN (?, ?)", parallelWaveWorkerStatusFailed, created.Workers[0].Id, created.Workers[1].Id); err != nil {
		t.Fatalf("mark majority failed: %v", err)
	}
	if _, err := testDB.Exec("UPDATE parallel_wave_worker SET status = ? WHERE id = ?", parallelWaveWorkerStatusRunning, created.Workers[2].Id); err != nil {
		t.Fatalf("mark minority worker running: %v", err)
	}
	if err := refreshParallelWaveAggregateStatus(created.WaveId, 1); err != nil {
		t.Fatalf("refresh majority failure: %v", err)
	}
	wave, err := fetchNetrunnerWaveSnapshot(created.WaveId, 1)
	if err != nil {
		t.Fatalf("fetch reconciled wave: %v", err)
	}
	if wave.ControlState != parallelWaveControlPausedForArchitect || wave.GateState != parallelWaveGateArchitectReview || !strings.Contains(wave.ControlReason, "failed_worker_majority:2/3") {
		t.Fatalf("expected local paused_for_architect state, got %+v", wave)
	}
	control, _, err := fetchOrchestrationControl(1)
	if err != nil {
		t.Fatalf("fetch project orchestration control: %v", err)
	}
	if control.OrchestrationFrozen {
		t.Fatal("wave failure reconciliation must not globally freeze unrelated waves")
	}
	if callResult, _, err := SetNetrunnerWaveControlState(context.Background(), nil, SetNetrunnerWaveControlStateInput{
		WaveId:       created.WaveId,
		ControlState: parallelWaveControlActive,
	}); err == nil || callResult == nil || !callResult.IsError {
		t.Fatalf("expected architect approval gate when resuming, result=%+v err=%v", callResult, err)
	}
	callResult, resumed, err := SetNetrunnerWaveControlState(context.Background(), nil, SetNetrunnerWaveControlStateInput{
		WaveId:            created.WaveId,
		ControlState:      parallelWaveControlActive,
		ArchitectApproved: true,
		Reason:            "Architect approved governed recovery",
	})
	if err != nil || callResult != nil {
		t.Fatalf("resume architect-paused wave: result=%+v err=%v", callResult, err)
	}
	if resumed.Wave.ControlState != parallelWaveControlActive || !strings.HasPrefix(resumed.Wave.ControlReason, "architect_approved") {
		t.Fatalf("unexpected resumed control state: %+v", resumed.Wave)
	}
	if _, err := reconcileParallelWaveFailureControl(resumed.Wave); err != nil {
		t.Fatalf("architect-approved recovery must remain acknowledged: %v", err)
	}
}

func TestReconcileCatchesNewFailureAfterArchitectApprovedResume(t *testing.T) {
	originalDB, originalRole, originalProjectID := db, authorizedRole, authorizedProjectId
	defer func() {
		db, authorizedRole, authorizedProjectId = originalDB, originalRole, originalProjectID
	}()
	repoDir := setupCleanGitRepo(t)
	testDB := setupParallelWaveTestDB(t, repoDir)
	defer testDB.Close()
	if _, err := testDB.Exec("INSERT INTO session (project_id, task_description, status, declared_write_scope) VALUES (1, 'Task D', 'pending', '[\"docs/c\"]')"); err != nil {
		t.Fatalf("seed third session: %v", err)
	}
	db, authorizedRole, authorizedProjectId = testDB, "fixer", 1

	_, created, err := CreateNetrunnerWave(context.Background(), nil, CreateNetrunnerWaveInput{SessionIds: []int{1, 2, 3}})
	if err != nil {
		t.Fatalf("create post-resume wave: %v", err)
	}
	// Two workers fail while the third stays deferred ("created"), mirroring a
	// dependency-gated worker that has not been scheduled yet.
	if _, err := testDB.Exec("UPDATE parallel_wave_worker SET status = ? WHERE id IN (?, ?)", parallelWaveWorkerStatusFailed, created.Workers[0].Id, created.Workers[1].Id); err != nil {
		t.Fatalf("mark majority failed: %v", err)
	}
	if err := refreshParallelWaveAggregateStatus(created.WaveId, 1); err != nil {
		t.Fatalf("refresh majority failure: %v", err)
	}
	wave, err := fetchNetrunnerWaveSnapshot(created.WaveId, 1)
	if err != nil {
		t.Fatalf("fetch paused wave: %v", err)
	}
	if wave.ControlState != parallelWaveControlPausedForArchitect {
		t.Fatalf("expected wave paused before resume: %+v", wave)
	}

	callResult, resumed, err := SetNetrunnerWaveControlState(context.Background(), nil, SetNetrunnerWaveControlStateInput{
		WaveId:            created.WaveId,
		ControlState:      parallelWaveControlActive,
		ArchitectApproved: true,
		Reason:            "Architect approved, proceed with deferred worker",
	})
	if err != nil || callResult != nil {
		t.Fatalf("resume architect-paused wave: result=%+v err=%v", callResult, err)
	}
	if resumed.Wave.FailurePolicyState != parallelWaveFailurePolicyPassed {
		t.Fatalf("expected resumed wave to pass: %+v", resumed.Wave)
	}

	// Reconciling immediately after resume, with nothing new having happened,
	// must not immediately re-pause on the same already-acknowledged failures.
	unchanged, err := reconcileParallelWaveFailureControl(resumed.Wave)
	if err != nil {
		t.Fatalf("reconcile immediately after resume: %v", err)
	}
	if unchanged.ControlState != parallelWaveControlActive {
		t.Fatalf("resume acknowledgment must not immediately re-pause: %+v", unchanged)
	}

	// The deferred worker is later scheduled and fails: a genuinely new
	// failure the Architect never acknowledged. Canonical recovery must still
	// trigger instead of being silently absorbed by the stale approval.
	if _, err := testDB.Exec("UPDATE parallel_wave_worker SET status = ? WHERE id = ?", parallelWaveWorkerStatusFailed, created.Workers[2].Id); err != nil {
		t.Fatalf("fail deferred worker: %v", err)
	}
	postFailure, err := fetchNetrunnerWaveSnapshot(created.WaveId, 1)
	if err != nil {
		t.Fatalf("fetch post-failure wave: %v", err)
	}
	recovered, err := reconcileParallelWaveFailureControl(postFailure)
	if err != nil {
		t.Fatalf("reconcile new post-resume failure: %v", err)
	}
	if recovered.FailurePolicyState == parallelWaveFailurePolicyPassed {
		t.Fatalf("new post-resume failure must not be silently swallowed: %+v", recovered)
	}
	if recovered.ControlState != parallelWaveControlPausedForArchitect {
		t.Fatalf("new post-resume majority failure must reach the Architect again: %+v", recovered)
	}
}

func TestTransitionNetrunnerWavePhaseRequiresReviewedAcceptanceSession(t *testing.T) {
	originalDB, originalRole, originalProjectID := db, authorizedRole, authorizedProjectId
	defer func() {
		db, authorizedRole, authorizedProjectId = originalDB, originalRole, originalProjectID
	}()
	repoDir := setupCleanGitRepo(t)
	testDB := setupParallelWaveTestDB(t, repoDir)
	defer testDB.Close()
	db, authorizedRole, authorizedProjectId = testDB, "fixer", 1
	_, created, err := CreateNetrunnerWave(context.Background(), nil, CreateNetrunnerWaveInput{
		SessionIds:              []int{1},
		MaxChildWaveDepth:       1,
		MaxTotalDescendantWaves: 2,
		MaxTotalSessions:        3,
	})
	if err != nil {
		t.Fatalf("create acceptance wave: %v", err)
	}
	if _, err := testDB.Exec("UPDATE parallel_wave SET phase = ?, status = ? WHERE id = ?", parallelWavePhaseImplementation, parallelWaveStatusCompleted, created.WaveId); err != nil {
		t.Fatalf("mark implementation complete: %v", err)
	}
	if _, err := testDB.Exec("UPDATE parallel_wave_worker SET status = ?, terminal_at = CURRENT_TIMESTAMP WHERE wave_id = ?", parallelWaveWorkerStatusCompleted, created.WaveId); err != nil {
		t.Fatalf("mark implementation worker complete: %v", err)
	}
	if _, err := testDB.Exec(
		"INSERT INTO session (project_id, task_description, status, report, declared_write_scope, parallel_wave_id) VALUES (1, 'reviewer', 'completed', 'approved', '[\"fixer_mcp\"]', ?)",
		parallelWaveReviewMarker(created.WaveId),
	); err != nil {
		t.Fatalf("seed completed reviewer: %v", err)
	}

	callResult, accepted, err := TransitionNetrunnerWavePhase(context.Background(), nil, TransitionNetrunnerWavePhaseInput{
		WaveId:              created.WaveId,
		TargetPhase:         parallelWavePhaseAcceptance,
		AcceptanceSessionId: 2,
		ReviewApproved:      true,
	})
	if err != nil || callResult != nil {
		t.Fatalf("transition to acceptance failed: result=%+v err=%v", callResult, err)
	}
	if accepted.Wave.Phase != parallelWavePhaseAcceptance || accepted.Wave.GateState != parallelWaveGateAcceptanceReview || accepted.Wave.AcceptanceSessionId != 2 {
		t.Fatalf("unexpected acceptance contract: %+v", accepted.Wave)
	}
	if accepted.Wave.Status != parallelWaveStatusCompleted {
		t.Fatalf("legacy status must remain compatible, got %q", accepted.Wave.Status)
	}

	globalAcceptanceID, err := globalSessionIDFromProjectScoped(2, 1)
	if err != nil {
		t.Fatalf("map acceptance session: %v", err)
	}
	if _, err := testDB.Exec("UPDATE session SET status = 'completed', report = 'acceptance passed' WHERE id = ?", globalAcceptanceID); err != nil {
		t.Fatalf("complete acceptance session: %v", err)
	}
	if callResult, _, err := TransitionNetrunnerWavePhase(context.Background(), nil, TransitionNetrunnerWavePhaseInput{
		WaveId:         created.WaveId,
		TargetPhase:    parallelWavePhaseCompleted,
		ReviewApproved: true,
	}); err == nil || callResult == nil || !strings.Contains(err.Error(), "handoff_sha is required") {
		t.Fatalf("recursive wave completion must require committed handoff: result=%+v err=%v", callResult, err)
	}
	handoffSHA := runGitTestCommand(t, repoDir, "rev-parse", "HEAD^{commit}")
	callResult, completed, err := TransitionNetrunnerWavePhase(context.Background(), nil, TransitionNetrunnerWavePhaseInput{
		WaveId:         created.WaveId,
		TargetPhase:    parallelWavePhaseCompleted,
		ReviewApproved: true,
		HandoffSha:     handoffSHA,
	})
	if err != nil || callResult != nil {
		t.Fatalf("transition to completed failed: result=%+v err=%v", callResult, err)
	}
	if completed.Wave.Phase != parallelWavePhaseCompleted || completed.Wave.GateState != parallelWaveGateClosed || completed.Wave.AcceptanceSessionStatus != "completed" {
		t.Fatalf("unexpected completed phase: %+v", completed.Wave)
	}
	if completed.Wave.HandoffSha != handoffSHA {
		t.Fatalf("committed handoff was not persisted: %+v", completed.Wave)
	}
	var activeLeases int
	if err := testDB.QueryRow("SELECT COUNT(*) FROM parallel_wave_scope_lease WHERE wave_id = ? AND active = 1", created.WaveId).Scan(&activeLeases); err != nil || activeLeases != 0 {
		t.Fatalf("accepted completion must release scope leases: count=%d err=%v", activeLeases, err)
	}
}

func TestMCPBinaryRestartStateBlocksUntilFreshExactBuildIsConfirmed(t *testing.T) {
	originalDB, originalRole, originalProjectID := db, authorizedRole, authorizedProjectId
	originalBuildID, originalProcessIdentity := mcpRunningBuildID, mcpProcessIdentity
	defer func() {
		db, authorizedRole, authorizedProjectId = originalDB, originalRole, originalProjectID
		mcpRunningBuildID, mcpProcessIdentity = originalBuildID, originalProcessIdentity
	}()
	repoDir := setupCleanGitRepo(t)
	testDB := setupParallelWaveTestDB(t, repoDir)
	defer testDB.Close()
	db, authorizedRole, authorizedProjectId = testDB, "fixer", 1

	mcpRunningBuildID = "sha256:old"
	mcpProcessIdentity = "pid:10:start:100"
	_, marked, err := SetMCPBinaryRestartState(context.Background(), nil, SetMCPBinaryRestartStateInput{Action: "mark_required", BuildEpoch: 1, BuildId: "sha256:new", Reason: "wave engine binary changed"})
	if err != nil {
		t.Fatalf("mark restart required: %v", err)
	}
	if !marked.State.RestartRequired || marked.State.RequiredBuildEpoch != 1 {
		t.Fatalf("unexpected required state: %+v", marked.State)
	}
	control, found, err := fetchOrchestrationControl(1)
	if err != nil || !found || !control.OrchestrationFrozen || !control.NotificationsEnabledForActiveRun {
		t.Fatalf("mark_required must atomically freeze dispatch while preserving native notifications: control=%+v found=%t err=%v", control, found, err)
	}
	if err := ensureMCPBinaryRestartNotRequired(1); err == nil || !strings.Contains(err.Error(), "mcp_binary_restart_required") {
		t.Fatalf("expected launch/create guard, got %v", err)
	}

	if _, _, err := SetMCPBinaryRestartState(context.Background(), nil, SetMCPBinaryRestartStateInput{Action: "confirm_restarted", BuildEpoch: 1}); err == nil || !strings.Contains(err.Error(), "running build identity") {
		t.Fatalf("old requesting process must not confirm restart: %v", err)
	}
	mcpRunningBuildID = "sha256:new"
	if _, _, err := SetMCPBinaryRestartState(context.Background(), nil, SetMCPBinaryRestartStateInput{Action: "confirm_restarted", BuildEpoch: 1}); err == nil || !strings.Contains(err.Error(), "fresh MCP process") {
		t.Fatalf("same process must not confirm replacement build: %v", err)
	}
	mcpProcessIdentity = "pid:11:start:200"
	if _, _, err := SetMCPBinaryRestartState(context.Background(), nil, SetMCPBinaryRestartStateInput{Action: "confirm_restarted", BuildEpoch: 2}); err == nil || !strings.Contains(err.Error(), "required epoch") {
		t.Fatalf("stale/wrong build epoch must not confirm restart: %v", err)
	}
	_, confirmed, err := SetMCPBinaryRestartState(context.Background(), nil, SetMCPBinaryRestartStateInput{Action: "confirm_restarted", BuildEpoch: 1, Reason: "new binary observed"})
	if err != nil {
		t.Fatalf("confirm restart: %v", err)
	}
	if confirmed.State.RestartRequired || confirmed.State.RunningBuildEpoch != 1 {
		t.Fatalf("unexpected confirmed state: %+v", confirmed.State)
	}
	if err := ensureMCPBinaryRestartNotRequired(1); err != nil {
		t.Fatalf("confirmed epoch should unblock waves: %v", err)
	}
	control, found, err = fetchOrchestrationControl(1)
	if err != nil || !found || !control.OrchestrationFrozen {
		t.Fatalf("restart confirmation must not silently resume orchestration: control=%+v found=%t err=%v", control, found, err)
	}
}

func TestMCPBinaryRestartMarkRequiredCASIsMonotonicAndPreservesEvidence(t *testing.T) {
	originalDB, originalRole, originalProjectID := db, authorizedRole, authorizedProjectId
	originalBuildID, originalProcessIdentity := mcpRunningBuildID, mcpProcessIdentity
	defer func() {
		db, authorizedRole, authorizedProjectId = originalDB, originalRole, originalProjectID
		mcpRunningBuildID, mcpProcessIdentity = originalBuildID, originalProcessIdentity
	}()
	repoDir := setupCleanGitRepo(t)
	testDB := setupParallelWaveTestDB(t, repoDir)
	defer testDB.Close()
	db, authorizedRole, authorizedProjectId = testDB, "fixer", 1

	mcpRunningBuildID = "sha256:old"
	mcpProcessIdentity = "pid:10:start:100"
	_, first, err := SetMCPBinaryRestartState(context.Background(), nil, SetMCPBinaryRestartStateInput{
		Action:     "mark_required",
		BuildEpoch: 3,
		BuildId:    "sha256:new-a",
		Reason:     "first requester evidence",
	})
	if err != nil {
		t.Fatalf("mark restart required: %v", err)
	}
	if first.State.RequiredByProcessIdentity != "pid:10:start:100" {
		t.Fatalf("unexpected initial requester evidence: %+v", first.State)
	}

	mcpProcessIdentity = "pid:11:start:200"
	_, replayed, err := SetMCPBinaryRestartState(context.Background(), nil, SetMCPBinaryRestartStateInput{
		Action:     "mark_required",
		BuildEpoch: 3,
		BuildId:    "sha256:new-a",
		Reason:     "idempotent replay must not replace evidence",
	})
	if err != nil {
		t.Fatalf("idempotent same-build mark: %v", err)
	}
	if replayed.State.RequiredByProcessIdentity != "pid:10:start:100" || replayed.State.Reason != "first requester evidence" {
		t.Fatalf("idempotent replay weakened requester evidence: %+v", replayed.State)
	}

	if _, err := testDB.Exec(`UPDATE autonomous_run_status SET orchestration_frozen = 0, notifications_enabled_for_active_run = 0 WHERE project_id = 1`); err != nil {
		t.Fatalf("prepare partial-mutation sentinel: %v", err)
	}
	if _, _, err := SetMCPBinaryRestartState(context.Background(), nil, SetMCPBinaryRestartStateInput{
		Action:     "mark_required",
		BuildEpoch: 3,
		BuildId:    "sha256:new-b",
	}); err == nil || !strings.Contains(err.Error(), "already requires build identity") {
		t.Fatalf("same epoch with a different build must conflict, got %v", err)
	}
	if _, _, err := SetMCPBinaryRestartState(context.Background(), nil, SetMCPBinaryRestartStateInput{
		Action:     "mark_required",
		BuildEpoch: 2,
		BuildId:    "sha256:older",
	}); err == nil || !strings.Contains(err.Error(), "not lower than required epoch 3") {
		t.Fatalf("lower required epoch must conflict, got %v", err)
	}

	state, err := fetchMCPBinaryRestartState(1)
	if err != nil {
		t.Fatalf("fetch restart state after conflicts: %v", err)
	}
	if !state.RestartRequired || state.RequiredBuildEpoch != 3 || state.RequiredBuildId != "sha256:new-a" || state.RequiredByProcessIdentity != "pid:10:start:100" {
		t.Fatalf("conflicting marks mutated the restart requirement: %+v", state)
	}
	control, found, err := fetchOrchestrationControl(1)
	if err != nil || !found {
		t.Fatalf("fetch partial-mutation sentinel: control=%+v found=%t err=%v", control, found, err)
	}
	if control.OrchestrationFrozen || control.NotificationsEnabledForActiveRun {
		t.Fatalf("conflicting mark partially mutated orchestration control: %+v", control)
	}
}

func TestMCPBinaryRestartStaleConfirmationCannotClearNewerRequirement(t *testing.T) {
	originalDB, originalRole, originalProjectID := db, authorizedRole, authorizedProjectId
	originalBuildID, originalProcessIdentity := mcpRunningBuildID, mcpProcessIdentity
	defer func() {
		db, authorizedRole, authorizedProjectId = originalDB, originalRole, originalProjectID
		mcpRunningBuildID, mcpProcessIdentity = originalBuildID, originalProcessIdentity
	}()
	repoDir := setupCleanGitRepo(t)
	testDB := setupParallelWaveTestDB(t, repoDir)
	defer testDB.Close()
	db, authorizedRole, authorizedProjectId = testDB, "fixer", 1

	mcpRunningBuildID = "sha256:old"
	mcpProcessIdentity = "pid:10:start:100"
	if _, _, err := SetMCPBinaryRestartState(context.Background(), nil, SetMCPBinaryRestartStateInput{
		Action:     "mark_required",
		BuildEpoch: 1,
		BuildId:    "sha256:new-a",
	}); err != nil {
		t.Fatalf("mark first restart requirement: %v", err)
	}

	mcpProcessIdentity = "pid:20:start:200"
	if _, _, err := SetMCPBinaryRestartState(context.Background(), nil, SetMCPBinaryRestartStateInput{
		Action:     "mark_required",
		BuildEpoch: 2,
		BuildId:    "sha256:new-b",
		Reason:     "newer requirement won",
	}); err != nil {
		t.Fatalf("mark newer restart requirement: %v", err)
	}

	mcpRunningBuildID = "sha256:new-a"
	mcpProcessIdentity = "pid:30:start:300"
	if _, _, err := SetMCPBinaryRestartState(context.Background(), nil, SetMCPBinaryRestartStateInput{
		Action:     "confirm_restarted",
		BuildEpoch: 1,
	}); err == nil || !strings.Contains(err.Error(), "does not match required epoch 2") {
		t.Fatalf("stale confirmation must lose to the newer requirement, got %v", err)
	}

	state, err := fetchMCPBinaryRestartState(1)
	if err != nil {
		t.Fatalf("fetch restart state after stale confirmation: %v", err)
	}
	if !state.RestartRequired || state.RequiredBuildEpoch != 2 || state.RequiredBuildId != "sha256:new-b" || state.RequiredByProcessIdentity != "pid:20:start:200" {
		t.Fatalf("stale confirmation cleared or rewrote the newer requirement: %+v", state)
	}
	control, found, err := fetchOrchestrationControl(1)
	if err != nil || !found || !control.OrchestrationFrozen {
		t.Fatalf("stale confirmation must leave orchestration frozen: control=%+v found=%t err=%v", control, found, err)
	}
}

func TestMCPBinaryRestartMarkRequiredRollsBackWhenFreezeFails(t *testing.T) {
	originalDB, originalRole, originalProjectID := db, authorizedRole, authorizedProjectId
	originalBuildID, originalProcessIdentity := mcpRunningBuildID, mcpProcessIdentity
	defer func() {
		db, authorizedRole, authorizedProjectId = originalDB, originalRole, originalProjectID
		mcpRunningBuildID, mcpProcessIdentity = originalBuildID, originalProcessIdentity
	}()
	repoDir := setupCleanGitRepo(t)
	testDB := setupParallelWaveTestDB(t, repoDir)
	defer testDB.Close()
	db, authorizedRole, authorizedProjectId = testDB, "fixer", 1
	mcpRunningBuildID = "sha256:old"
	mcpProcessIdentity = "pid:10:start:100"

	if _, err := testDB.Exec(`
		CREATE TRIGGER reject_restart_freeze
		BEFORE INSERT ON autonomous_run_status
		BEGIN
			SELECT RAISE(ABORT, 'injected freeze failure');
		END;
	`); err != nil {
		t.Fatalf("install freeze failure injection: %v", err)
	}
	if _, _, err := SetMCPBinaryRestartState(context.Background(), nil, SetMCPBinaryRestartStateInput{
		Action:     "mark_required",
		BuildEpoch: 1,
		BuildId:    "sha256:new",
	}); err == nil || !strings.Contains(err.Error(), "injected freeze failure") {
		t.Fatalf("expected injected atomic freeze failure, got %v", err)
	}
	state, err := fetchMCPBinaryRestartState(1)
	if err != nil {
		t.Fatalf("fetch restart state after rollback: %v", err)
	}
	if state.RestartRequired || state.RequiredBuildEpoch != 0 {
		t.Fatalf("restart marker survived failed freeze transaction: %+v", state)
	}
}

func TestParallelWaveFollowUpDecisionHonorsBinaryRestartMarker(t *testing.T) {
	allowed, reason := parallelWaveFollowUpDecision(
		orchestrationControl{OrchestrationEpoch: 4},
		NetrunnerWaveSnapshot{OrchestrationEpoch: 4, ControlState: parallelWaveControlActive},
		MCPBinaryRestartState{RestartRequired: true, RequiredBuildEpoch: 5},
	)
	if allowed || reason != "mcp_binary_restart_required:5" {
		t.Fatalf("expected binary restart follow-up block, allowed=%t reason=%q", allowed, reason)
	}
}

func TestParallelWaveDeclaredWriteScopeContainsPath(t *testing.T) {
	tests := []struct {
		name  string
		scope []string
		path  string
		want  bool
	}{
		{name: "exact file", scope: []string{"docs/a.md"}, path: "docs/a.md", want: true},
		{name: "directory prefix", scope: []string{"docs"}, path: "docs/a/nested.md", want: true},
		{name: "outside prefix", scope: []string{"docs"}, path: "docs2/a.md", want: false},
		{name: "double star direct child", scope: []string{"deliverables/deploy/**"}, path: "deliverables/deploy/new.txt", want: true},
		{name: "double star nested child", scope: []string{"deliverables/deploy/**"}, path: "deliverables/deploy/sub/deep/new.txt", want: true},
		{name: "double star directory itself", scope: []string{"deliverables/deploy/**"}, path: "deliverables/deploy", want: true},
		{name: "double star outside sibling", scope: []string{"deliverables/deploy/**"}, path: "deliverables/deploy_extra/new.txt", want: false},
		{name: "broad scope", scope: []string{"."}, path: "anything/at/all.txt", want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := parallelWaveDeclaredWriteScopeContainsPath(test.scope, test.path); got != test.want {
				t.Fatalf("parallelWaveDeclaredWriteScopeContainsPath(%v, %q) = %v, want %v", test.scope, test.path, got, test.want)
			}
		})
	}
}

func TestReleaseParallelWaveWorkerScopeLeases(t *testing.T) {
	t.Run("disjoint scopes release independently", func(t *testing.T) {
		originalDB, originalRole, originalProjectID := db, authorizedRole, authorizedProjectId
		defer func() {
			db, authorizedRole, authorizedProjectId = originalDB, originalRole, originalProjectID
		}()
		repoDir := setupCleanGitRepo(t)
		testDB := setupParallelWaveTestDB(t, repoDir)
		defer testDB.Close()
		db, authorizedRole, authorizedProjectId = testDB, "fixer", 1

		_, created, err := CreateNetrunnerWave(context.Background(), nil, CreateNetrunnerWaveInput{SessionIds: []int{1, 2}})
		if err != nil {
			t.Fatalf("create lease wave: %v", err)
		}
		wave, err := fetchNetrunnerWaveSnapshot(created.WaveId, 1)
		if err != nil {
			t.Fatalf("fetch lease wave: %v", err)
		}
		wave.Workers[0].Status = parallelWaveWorkerStatusCompleted
		wave.Workers[1].Status = parallelWaveWorkerStatusRunning
		if err := releaseParallelWaveWorkerScopeLeases(wave, wave.Workers[0]); err != nil {
			t.Fatalf("release terminal worker leases: %v", err)
		}

		var released, held int
		if err := testDB.QueryRow(
			"SELECT COUNT(*) FROM parallel_wave_scope_lease WHERE wave_id = ? AND scope_path = 'docs/a' AND active = 1",
			created.WaveId,
		).Scan(&released); err != nil {
			t.Fatalf("query released lease: %v", err)
		}
		if err := testDB.QueryRow(
			"SELECT COUNT(*) FROM parallel_wave_scope_lease WHERE wave_id = ? AND scope_path = 'docs/b' AND active = 1",
			created.WaveId,
		).Scan(&held); err != nil {
			t.Fatalf("query held lease: %v", err)
		}
		if released != 0 || held != 1 {
			t.Fatalf("expected terminal worker lease released and sibling lease held: released=%d held=%d", released, held)
		}
	})

	t.Run("shared scope stays held while sibling runs", func(t *testing.T) {
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
			t.Fatalf("create shared lease wave: %v", err)
		}
		wave, err := fetchNetrunnerWaveSnapshot(created.WaveId, 1)
		if err != nil {
			t.Fatalf("fetch shared lease wave: %v", err)
		}
		sharedScope := []string{"docs/a"}
		wave.Workers = append(wave.Workers, NetrunnerWaveWorkerSnapshot{
			Id:                 999,
			WaveId:             wave.Id,
			ProjectId:          wave.ProjectId,
			SessionId:          2,
			Status:             parallelWaveWorkerStatusRunning,
			DeclaredWriteScope: sharedScope,
		})
		wave.Workers[0].Status = parallelWaveWorkerStatusCompleted
		wave.Workers[0].DeclaredWriteScope = sharedScope
		if err := releaseParallelWaveWorkerScopeLeases(wave, wave.Workers[0]); err != nil {
			t.Fatalf("release shared terminal worker leases: %v", err)
		}
		var held int
		if err := testDB.QueryRow(
			"SELECT COUNT(*) FROM parallel_wave_scope_lease WHERE wave_id = ? AND scope_path = 'docs/a' AND active = 1",
			created.WaveId,
		).Scan(&held); err != nil {
			t.Fatalf("query shared lease: %v", err)
		}
		if held != 1 {
			t.Fatalf("expected shared scope lease to remain held while sibling is running, got %d", held)
		}
	})
}

func TestTransitionNetrunnerWavePhaseReconcilesStaleCompletedWorker(t *testing.T) {
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
		t.Fatalf("create stale-reconcile wave: %v", err)
	}
	markTestWaveRunningWithWorktrees(t, testDB, repoDir, created)
	if _, err := testDB.Exec(
		"UPDATE parallel_wave SET phase = ?, status = ? WHERE id = ?",
		parallelWavePhaseImplementation,
		parallelWaveStatusRunning,
		created.WaveId,
	); err != nil {
		t.Fatalf("mark implementation phase: %v", err)
	}
	globalSessionID, err := globalSessionIDFromProjectScoped(1, 1)
	if err != nil {
		t.Fatalf("map worker session id: %v", err)
	}
	// The session is completed but the wave worker is still recorded as
	// running and its process row is gone, exactly the stranded shape.
	if _, err := testDB.Exec("UPDATE session SET status = 'completed', report = 'done' WHERE id = ?", globalSessionID); err != nil {
		t.Fatalf("complete worker session: %v", err)
	}
	if _, err := testDB.Exec("DELETE FROM worker_process WHERE parallel_wave_id = ?", created.WaveId); err != nil {
		t.Fatalf("remove worker process rows: %v", err)
	}
	if _, err := testDB.Exec(
		"INSERT INTO session (project_id, task_description, status, report, declared_write_scope, parallel_wave_id) VALUES (1, 'reviewer', 'completed', 'approved', '[\"fixer_mcp\"]', ?)",
		parallelWaveReviewMarker(created.WaveId),
	); err != nil {
		t.Fatalf("seed completed reviewer: %v", err)
	}

	callResult, accepted, err := TransitionNetrunnerWavePhase(context.Background(), nil, TransitionNetrunnerWavePhaseInput{
		WaveId:              created.WaveId,
		TargetPhase:         parallelWavePhaseAcceptance,
		AcceptanceSessionId: 2,
		ReviewApproved:      true,
	})
	if err != nil || callResult != nil {
		t.Fatalf("transition to acceptance should reconcile stale worker: result=%+v err=%v", callResult, err)
	}
	if accepted.Wave.Phase != parallelWavePhaseAcceptance || accepted.Wave.AcceptanceSessionId != 2 {
		t.Fatalf("unexpected reconciled acceptance contract: %+v", accepted.Wave)
	}
	worker := testWaveWorkerBySession(t, accepted.Wave, 1)
	if worker.Status != parallelWaveWorkerStatusCompleted {
		t.Fatalf("expected stale running worker reconciled to completed, got %q", worker.Status)
	}
}
