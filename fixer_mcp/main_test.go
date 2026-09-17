package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const testProjectCWD = "/tmp/self_orchestration_test_project"
const structuredTestFinalReport = `{"files_changed":["main.go"],"commands_run":["go test ./..."],"checks_run":["go test ./..."],"blockers":[]}`

func TestResolveFixerDBPathUsesEnvOrDefault(t *testing.T) {
	t.Setenv(fixerDBPathEnv, "")
	if got := resolveFixerDBPath(); got != defaultFixerDBFilename {
		t.Fatalf("expected default db filename, got %q", got)
	}

	explicitPath := filepath.Join(t.TempDir(), "custom-fixer.db")
	t.Setenv(fixerDBPathEnv, "  "+explicitPath+"  ")
	if got := resolveFixerDBPath(); got != explicitPath {
		t.Fatalf("expected explicit db path %q, got %q", explicitPath, got)
	}
}

func TestWaitDoesNotFinalizeAStillRunningWorkerAttempt(t *testing.T) {
	live := workerProcessSnapshot{Status: workerStatusRunning, Alive: true}
	if waitMayReportTerminalSessionStatus(true, live) {
		t.Fatal("a live worker process must suppress a stale terminal session status")
	}
	if !waitMayReportTerminalSessionStatus(false, workerProcessSnapshot{}) {
		t.Fatal("a session without a recorded worker process may report terminal status")
	}
	if !waitMayReportTerminalSessionStatus(true, workerProcessSnapshot{Status: workerStatusExited, Alive: false}) {
		t.Fatal("an exited worker process must allow terminal status")
	}
}

func TestSchemaBootstrapRequestIsExplicit(t *testing.T) {
	if !schemaBootstrapRequested([]string{schemaBootstrapArg}) {
		t.Fatal("expected exact schema bootstrap argument to be recognized")
	}
	for _, args := range [][]string{nil, {"--help"}, {schemaBootstrapArg, "extra"}} {
		if schemaBootstrapRequested(args) {
			t.Fatalf("unexpected schema bootstrap match for %#v", args)
		}
	}
}

func TestRunSchemaBootstrapMigratesPreHandsDatabaseIdempotently(t *testing.T) {
	originalDB := db
	defer func() {
		db = originalDB
	}()

	dbPath := filepath.Join(t.TempDir(), "pre-hands.db")
	legacyDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open pre-Hands database: %v", err)
	}
	if _, err := legacyDB.Exec(`
		CREATE TABLE project (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			cwd TEXT UNIQUE NOT NULL
		);
		INSERT INTO project (name, cwd) VALUES ('Legacy Project', '/tmp/legacy-project');
	`); err != nil {
		_ = legacyDB.Close()
		t.Fatalf("seed pre-Hands database: %v", err)
	}
	if err := legacyDB.Close(); err != nil {
		t.Fatalf("close pre-Hands database: %v", err)
	}

	t.Setenv(fixerDBPathEnv, dbPath)
	var firstOutput bytes.Buffer
	if err := runSchemaBootstrap(&firstOutput); err != nil {
		t.Fatalf("first schema bootstrap: %v", err)
	}
	var firstResult schemaBootstrapResult
	if err := json.Unmarshal(firstOutput.Bytes(), &firstResult); err != nil {
		t.Fatalf("decode first schema bootstrap result: %v", err)
	}
	if firstResult.Status != "ready" || firstResult.Schema != schemaBootstrapName {
		t.Fatalf("unexpected first schema bootstrap result: %+v", firstResult)
	}
	if firstResult.ProjectCount != 1 || firstResult.ProjectHandsCount != 1 {
		t.Fatalf("unexpected first schema bootstrap counts: %+v", firstResult)
	}

	verifyDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open migrated database: %v", err)
	}
	var firstActorID string
	if err := verifyDB.QueryRow(`SELECT actor_id FROM project_hands WHERE project_id = 1`).Scan(&firstActorID); err != nil {
		_ = verifyDB.Close()
		t.Fatalf("read migrated Hands identity: %v", err)
	}
	if err := verifyDB.Close(); err != nil {
		t.Fatalf("close migrated database: %v", err)
	}

	var secondOutput bytes.Buffer
	if err := runSchemaBootstrap(&secondOutput); err != nil {
		t.Fatalf("repeat schema bootstrap: %v", err)
	}
	var secondResult schemaBootstrapResult
	if err := json.Unmarshal(secondOutput.Bytes(), &secondResult); err != nil {
		t.Fatalf("decode repeated schema bootstrap result: %v", err)
	}
	if secondResult != firstResult {
		t.Fatalf("schema bootstrap counts changed on repeat: first=%+v second=%+v", firstResult, secondResult)
	}

	verifyDB, err = sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("reopen migrated database: %v", err)
	}
	defer func() { _ = verifyDB.Close() }()
	var secondActorID string
	if err := verifyDB.QueryRow(`SELECT actor_id FROM project_hands WHERE project_id = 1`).Scan(&secondActorID); err != nil {
		t.Fatalf("read repeated Hands identity: %v", err)
	}
	if firstActorID == "" || secondActorID != firstActorID {
		t.Fatalf("schema bootstrap replaced durable Hands identity: first=%q second=%q", firstActorID, secondActorID)
	}
}

func TestExplicitWaitPendingStartupFailureAppliesOnlyWithoutLiveWorker(t *testing.T) {
	cases := []struct {
		name                  string
		processFound          bool
		workerProcessTerminal bool
		want                  bool
	}{
		{"no worker process metadata recorded yet", false, false, true},
		{"recorded worker process already terminal", true, true, true},
		{"live recorded worker process, session metadata still pending", true, false, false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := explicitWaitPendingStartupFailureApplies(testCase.processFound, testCase.workerProcessTerminal); got != testCase.want {
				t.Fatalf("explicitWaitPendingStartupFailureApplies(%v, %v) = %v, want %v", testCase.processFound, testCase.workerProcessTerminal, got, testCase.want)
			}
		})
	}
}

func TestWaitForContextOrDurationHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitForContextOrDuration(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("waitForContextOrDuration() error = %v, want context.Canceled", err)
	}
}

func TestWaitForNetrunnerSessionDetectsExitedWorkerProcessPromptly(t *testing.T) {
	originalDB, originalRole, originalProjectID := db, authorizedRole, authorizedProjectId
	defer func() {
		db, authorizedRole, authorizedProjectId = originalDB, originalRole, originalProjectID
	}()
	testDB := setupGetProjectsTestDB(t)
	defer testDB.Close()
	db, authorizedRole, authorizedProjectId = testDB, "fixer", 1

	if _, err := testDB.Exec("UPDATE session SET status = 'in_progress' WHERE id = 1"); err != nil {
		t.Fatalf("mark session in_progress: %v", err)
	}
	if _, err := testDB.Exec(
		"INSERT INTO worker_process (project_id, session_id, pid, launch_epoch, status, stopped_at) VALUES (1, 1, 999999, 1, 'exited', CURRENT_TIMESTAMP)",
	); err != nil {
		t.Fatalf("seed exited worker process: %v", err)
	}

	result, err := waitForNetrunnerSessionResult(context.Background(), 1, 5, 1)
	if err != nil {
		t.Fatalf("wait for exited worker: %v", err)
	}
	if !result.Terminal || result.TerminalCondition != "worker_process_exited" {
		t.Fatalf("expected prompt worker_process_exited terminal condition, got %+v", result)
	}
	if result.WorkerProcess == nil || result.WorkerProcess.ProcessStatus != workerStatusExited {
		t.Fatalf("expected worker process exit diagnostic, got %+v", result.WorkerProcess)
	}
}

func TestWaitForNetrunnerSessionTimeoutKeepsLiveWorkerPending(t *testing.T) {
	originalDB, originalRole, originalProjectID := db, authorizedRole, authorizedProjectId
	defer func() {
		db, authorizedRole, authorizedProjectId = originalDB, originalRole, originalProjectID
	}()
	testDB := setupGetProjectsTestDB(t)
	defer testDB.Close()
	db, authorizedRole, authorizedProjectId = testDB, "fixer", 1

	// A live worker process exists, but the session metadata is still
	// "pending". The timeout path must not declare a startup failure while the
	// worker is demonstrably alive.
	if _, err := testDB.Exec(
		"INSERT INTO worker_process (project_id, session_id, pid, launch_epoch, status, launch_origin) VALUES (1, 1, ?, 1, 'running', 'explicit')",
		os.Getpid(),
	); err != nil {
		t.Fatalf("seed live worker process: %v", err)
	}

	result, err := waitForNetrunnerSessionResult(context.Background(), 1, 1, 1)
	if err != nil {
		t.Fatalf("wait with live worker should time out, not fail: %v", err)
	}
	if result.Terminal {
		t.Fatalf("expected non-terminal timeout for live worker, got terminal condition %q", result.TerminalCondition)
	}
	if !result.TimedOut || result.TerminalCondition != "timed_out" {
		t.Fatalf("expected timed_out result, got %+v", result)
	}
}

func TestParallelNetrunnerWaveLifecycleSmoke(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	originalExecCommand := execCommand
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
		execCommand = originalExecCommand
	}()

	repoDir := setupCleanGitRepo(t)
	testDB := setupParallelWaveTestDB(t, repoDir)
	defer func() {
		_ = testDB.Close()
	}()

	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1

	callResult, created, err := CreateNetrunnerWave(context.Background(), nil, CreateNetrunnerWaveInput{
		SessionIds: []int{1, 2},
		BaseRef:    "HEAD",
		Reason:     "phase 7 lifecycle smoke",
	})
	if err != nil {
		t.Fatalf("create_netrunner_wave smoke failed: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil create call result, got %+v", callResult)
	}
	if created.Wave.Status != parallelWaveStatusCreated || len(created.Workers) != 2 {
		t.Fatalf("unexpected created wave: %+v", created)
	}
	if created.Wave.Phase != parallelWavePhaseInitialized || created.Wave.ControlState != parallelWaveControlActive {
		t.Fatalf("unexpected initialized v2 state: %+v", created.Wave)
	}

	var launchedArgs [][]string
	installFakeWaveWorkerLauncher(t, "", &launchedArgs)
	callResult, launched, err := LaunchNetrunnerWave(context.Background(), nil, LaunchNetrunnerWaveInput{
		WaveId:         created.WaveId,
		FixerSessionId: "fixer-session-smoke",
		TimeoutSeconds: 1,
	})
	if err != nil {
		t.Fatalf("launch_netrunner_wave smoke failed: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil launch call result, got %+v", callResult)
	}
	if launched.Status != "success" || launched.Wave.Status != parallelWaveStatusRunning || len(launched.Workers) != 2 {
		t.Fatalf("unexpected launched wave: %+v", launched)
	}
	if launched.Wave.Phase != parallelWavePhaseImplementation {
		t.Fatalf("expected implementation phase after launch, got %+v", launched.Wave)
	}
	if len(launchedArgs) != 2 {
		t.Fatalf("expected two fake launcher calls, got %d: %+v", len(launchedArgs), launchedArgs)
	}

	winnerWorker := testWaveWorkerBySession(t, launched.Wave, 1)
	winnerWorktreePath, err := resolveParallelWaveWorktreePath(repoDir, winnerWorker.WorktreePath)
	if err != nil {
		t.Fatalf("resolve winner worktree: %v", err)
	}
	changedPath := filepath.Join(winnerWorktreePath, "docs", "a", "smoke.md")
	if err := os.MkdirAll(filepath.Dir(changedPath), 0o755); err != nil {
		t.Fatalf("prepare winner change dir: %v", err)
	}
	if err := os.WriteFile(changedPath, []byte("phase 7 smoke worker change\n"), 0o644); err != nil {
		t.Fatalf("write winner worktree change: %v", err)
	}
	runGitTestCommand(t, winnerWorktreePath, "add", "docs/a/smoke.md")
	runGitTestCommand(t, winnerWorktreePath, "-c", "user.name=Fixer Test", "-c", "user.email=fixer@example.test", "commit", "-m", "smoke commit")

	winnerGlobalSessionID, err := globalSessionIDFromProjectScoped(1, 1)
	if err != nil {
		t.Fatalf("map winner session id: %v", err)
	}
	if _, err := testDB.Exec("UPDATE session SET status = 'review', report = 'phase 7 smoke ready' WHERE id = ?", winnerGlobalSessionID); err != nil {
		t.Fatalf("mark winner review-ready: %v", err)
	}
	if _, err := testDB.Exec(
		"UPDATE worker_process SET status = ?, stopped_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP WHERE parallel_wave_id = ? AND session_id = ?",
		workerStatusExited,
		created.WaveId,
		winnerGlobalSessionID,
	); err != nil {
		t.Fatalf("mark winner worker process exited: %v", err)
	}
	if _, err := testDB.Exec(
		"INSERT INTO doc_proposal (project_id, session_id, status, proposed_content, proposed_doc_type) VALUES (1, ?, 'pending', 'phase 7 smoke proposal', 'architecture')",
		winnerGlobalSessionID,
	); err != nil {
		t.Fatalf("seed winner proposal: %v", err)
	}

	callResult, waitOut, err := WaitForNetrunnerWave(context.Background(), nil, WaitForNetrunnerWaveInput{
		WaveId:              created.WaveId,
		TimeoutSeconds:      300,
		PollIntervalSeconds: 1,
	})
	if err != nil {
		t.Fatalf("wait_for_netrunner_wave first review smoke failed: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil first wait call result, got %+v", callResult)
	}
	if waitOut.Status != "success" || waitOut.Result.WinningSessionId != 1 || waitOut.Result.WorkerStatus != parallelWaveWorkerStatusReviewReady {
		t.Fatalf("expected local session 1 review-ready winner, got %+v", waitOut)
	}
	if waitOut.Result.WaveStatus != parallelWaveStatusReviewReady {
		t.Fatalf("expected review-ready aggregate wave status, got %+v", waitOut.Result)
	}
	if len(waitOut.Result.ProposalIds) != 1 || waitOut.Result.ProposalIds[0] != 1 {
		t.Fatalf("expected winner proposal id 1, got %+v", waitOut.Result.ProposalIds)
	}
	if !containsString(waitOut.Result.ChangedPaths, "docs/a/smoke.md") {
		t.Fatalf("expected smoke change in changed paths, got %+v", waitOut.Result.ChangedPaths)
	}
	if waitOut.Result.DiffPatchPath == "" ||
		!strings.Contains(waitOut.Result.DiffPatchPath, filepath.Join(".codex", "netrunner_wave_artifacts", "wave-"+strconv.Itoa(created.WaveId), "session-1.patch")) {
		t.Fatalf("expected deterministic smoke patch path, got %+v", waitOut.Result)
	}
	patchPayload, err := os.ReadFile(waitOut.Result.DiffPatchPath)
	if err != nil {
		t.Fatalf("read smoke patch artifact: %v", err)
	}
	if !strings.Contains(string(patchPayload), "phase 7 smoke worker change") {
		t.Fatalf("expected smoke patch payload, got:\n%s", string(patchPayload))
	}

	remainingGlobalSessionID, err := globalSessionIDFromProjectScoped(2, 1)
	if err != nil {
		t.Fatalf("map remaining session id: %v", err)
	}
	if _, err := testDB.Exec("UPDATE session SET status = 'completed', report = 'phase 7 smoke completed' WHERE id = ?", remainingGlobalSessionID); err != nil {
		t.Fatalf("mark remaining session completed: %v", err)
	}
	if _, err := testDB.Exec(
		"UPDATE worker_process SET status = ?, stopped_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP WHERE parallel_wave_id = ? AND session_id = ?",
		workerStatusExited,
		created.WaveId,
		remainingGlobalSessionID,
	); err != nil {
		t.Fatalf("mark remaining worker process exited: %v", err)
	}
	callResult, allTerminalOut, err := WaitForNetrunnerWave(context.Background(), nil, WaitForNetrunnerWaveInput{
		WaveId:              created.WaveId,
		TimeoutSeconds:      300,
		PollIntervalSeconds: 1,
		ReturnWhen:          parallelWaveWaitAllTerminal,
	})
	if err != nil {
		t.Fatalf("wait_for_netrunner_wave all-terminal smoke failed: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil all-terminal wait call result, got %+v", callResult)
	}
	if allTerminalOut.Status != "success" || allTerminalOut.Result.TerminalCondition != parallelWaveWaitAllTerminal {
		t.Fatalf("expected all-terminal wait success, got %+v", allTerminalOut)
	}
	if testWaveWorkerBySession(t, allTerminalOut.Result.Wave, 2).Status != parallelWaveWorkerStatusCompleted {
		t.Fatalf("expected remaining worker completed, got %+v", allTerminalOut.Result.Wave.Workers)
	}
	if allTerminalOut.Result.Wave.GateState != parallelWaveGateImplementationReview {
		t.Fatalf("expected implementation review gate, got %+v", allTerminalOut.Result.Wave)
	}

	if _, err := testDB.Exec(
		`UPDATE worker_process
		 SET status = ?,
		     stop_reason = 'phase 7 smoke terminal',
		     stopped_at = CURRENT_TIMESTAMP,
		     updated_at = CURRENT_TIMESTAMP
		 WHERE parallel_wave_id = ?`,
		workerStatusStopped,
		created.WaveId,
	); err != nil {
		t.Fatalf("mark smoke worker processes stopped: %v", err)
	}

	callResult, cleanupOut, err := CleanupNetrunnerWave(context.Background(), nil, CleanupNetrunnerWaveInput{
		WaveId:          created.WaveId,
		RemoveWorktrees: true,
		Force:           true,
	})
	if err != nil {
		t.Fatalf("cleanup_netrunner_wave smoke failed: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil cleanup call result, got %+v", callResult)
	}
	if cleanupOut.Status != "success" || !cleanupOut.Cleaned || cleanupOut.WaveStatus != parallelWaveStatusCleaned {
		t.Fatalf("expected cleaned smoke wave, got %+v", cleanupOut)
	}
	if !cleanupOut.RemoveWorktrees || !cleanupOut.Force {
		t.Fatalf("expected explicit remove and force cleanup, got %+v", cleanupOut)
	}
	if len(cleanupOut.Workers) != 2 {
		t.Fatalf("expected two cleanup worker results, got %+v", cleanupOut.Workers)
	}
	for _, result := range cleanupOut.Workers {
		if !result.Removed || result.CleanupStatus != parallelWaveCleanupStatusCleaned || result.WorkerStatus != parallelWaveWorkerStatusCleaned {
			t.Fatalf("expected removed and cleaned worker result, got %+v", result)
		}
		if _, err := os.Stat(result.ResolvedWorktreePath); !os.IsNotExist(err) {
			t.Fatalf("expected removed worktree %s, stat err=%v", result.ResolvedWorktreePath, err)
		}
	}
}
