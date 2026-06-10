package main

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
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

	winnerGlobalSessionID, err := globalSessionIDFromProjectScoped(1, 1)
	if err != nil {
		t.Fatalf("map winner session id: %v", err)
	}
	if _, err := testDB.Exec("UPDATE session SET status = 'review', report = 'phase 7 smoke ready' WHERE id = ?", winnerGlobalSessionID); err != nil {
		t.Fatalf("mark winner review-ready: %v", err)
	}
	if _, err := testDB.Exec(
		"INSERT INTO doc_proposal (project_id, session_id, status, proposed_content, proposed_doc_type) VALUES (1, ?, 'pending', 'phase 7 smoke proposal', 'architecture')",
		winnerGlobalSessionID,
	); err != nil {
		t.Fatalf("seed winner proposal: %v", err)
	}

	callResult, waitOut, err := WaitForNetrunnerWave(context.Background(), nil, WaitForNetrunnerWaveInput{
		WaveId:              created.WaveId,
		TimeoutSeconds:      1,
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
	callResult, allTerminalOut, err := WaitForNetrunnerWave(context.Background(), nil, WaitForNetrunnerWaveInput{
		WaveId:              created.WaveId,
		TimeoutSeconds:      1,
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
