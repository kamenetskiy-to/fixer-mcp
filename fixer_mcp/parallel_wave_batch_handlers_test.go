package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func setupTwoSingleWorkerWaves(t *testing.T) (*sql.DB, CreateNetrunnerWaveOutput, CreateNetrunnerWaveOutput) {
	t.Helper()
	repoDir := setupCleanGitRepo(t)
	testDB := setupParallelWaveTestDB(t, repoDir)
	db, authorizedRole, authorizedProjectId = testDB, "fixer", 1
	_, first, err := CreateNetrunnerWave(context.Background(), nil, CreateNetrunnerWaveInput{SessionIds: []int{1}})
	if err != nil {
		t.Fatalf("create first batch wave: %v", err)
	}
	_, second, err := CreateNetrunnerWave(context.Background(), nil, CreateNetrunnerWaveInput{SessionIds: []int{2}})
	if err != nil {
		t.Fatalf("create second batch wave: %v", err)
	}
	return testDB, first, second
}

func TestWaitNetrunnerWavesGateProfileEnvAuthSkipsAssumeRole(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	originalSessionID := authorizedSessionId
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
		authorizedSessionId = originalSessionID
	}()

	repoDir := setupCleanGitRepo(t)
	testDB := setupParallelWaveTestDB(t, repoDir)
	defer func() {
		_ = testDB.Close()
	}()

	t.Setenv(fixerMcpDefaultRoleEnv, "fixer")
	t.Setenv(fixerMcpDefaultCwdEnv, repoDir)
	t.Setenv(fixerMcpLockedRoleEnv, "fixer")
	t.Setenv(fixerMcpAutoAuthEnv, "1")
	t.Setenv(fixerMcpToolProfileEnv, netrunnerGateProfile)

	db = testDB
	authorizedRole = ""
	authorizedProjectId = 0
	authorizedSessionId = 0

	bootstrapDefaultRoleAuthFromEnv()
	if authorizedRole != "fixer" || authorizedProjectId != 1 {
		t.Fatalf("gate env auto-auth failed: role=%q project=%d", authorizedRole, authorizedProjectId)
	}

	// No explicit assume_role call: the batch wait handler must pass the
	// project-bound fixer auth gate and reject the missing waves instead of
	// returning an access-denied error.
	callResult, _, err := WaitForNetrunnerWaves(context.Background(), nil, WaitForNetrunnerWavesInput{WaveIds: []int{9998, 9999}})
	if err == nil {
		t.Fatal("expected missing-wave rejection")
	}
	if strings.Contains(err.Error(), "fixer role") {
		t.Fatalf("batch wait gate must be auto-authorized from env, got auth error: %v", err)
	}
	if callResult == nil || !callResult.IsError {
		t.Fatal("expected MCP error result")
	}
}

func TestLaunchNetrunnerWavesPreservesPerWaveFailureIsolation(t *testing.T) {
	originalDB, originalRole, originalProjectID, originalExecCommand := db, authorizedRole, authorizedProjectId, execCommand
	defer func() {
		db, authorizedRole, authorizedProjectId, execCommand = originalDB, originalRole, originalProjectID, originalExecCommand
	}()
	testDB, first, second := setupTwoSingleWorkerWaves(t)
	defer testDB.Close()
	installFakeWaveWorkerLauncher(t, "", nil)

	callResult, output, err := LaunchNetrunnerWaves(context.Background(), nil, LaunchNetrunnerWavesInput{Waves: []LaunchNetrunnerWaveInput{
		{WaveId: first.WaveId, TimeoutSeconds: 1},
		{WaveId: second.WaveId, Backend: "unsupported-backend", TimeoutSeconds: 1},
	}, DetailLevel: parallelWaveBatchDetailFull})
	if err != nil || callResult != nil {
		t.Fatalf("batch launch should isolate item errors: result=%+v err=%v", callResult, err)
	}
	if output.Status != "partial_failure" || len(output.Results) != 2 {
		t.Fatalf("unexpected batch output: %+v", output)
	}
	if output.DetailLevel != parallelWaveBatchDetailFull || output.Results[0].Output == nil || output.Results[0].Status != "success" || output.Results[0].Output.Wave.Status != parallelWaveStatusRunning {
		t.Fatalf("expected first wave launch success, got %+v", output.Results[0])
	}
	if output.Results[1].Status != "error" || !strings.Contains(output.Results[1].Error, "unsupported") {
		t.Fatalf("expected isolated second-wave validation failure, got %+v", output.Results[1])
	}
	wave, err := fetchNetrunnerWaveSnapshot(second.WaveId, 1)
	if err != nil {
		t.Fatalf("fetch failed batch wave: %v", err)
	}
	if wave.Status != parallelWaveStatusCreated {
		t.Fatalf("failed batch item must remain independently launchable, got %q", wave.Status)
	}
}

func TestLaunchNetrunnerWavesPreventsLauncherGitSelfContamination(t *testing.T) {
	originalDB, originalRole, originalProjectID, originalExecCommand := db, authorizedRole, authorizedProjectId, execCommand
	defer func() {
		db, authorizedRole, authorizedProjectId, execCommand = originalDB, originalRole, originalProjectID, originalExecCommand
	}()
	testDB, first, second := setupTwoSingleWorkerWaves(t)
	defer testDB.Close()
	installFakeWaveWorkerLauncher(t, "", nil)
	t.Setenv(pythonNoBytecodeEnv, "0")
	t.Setenv("FAKE_TRACKED_BYTECODE_PATH", filepath.Join(first.Wave.ProjectCwd, "README.md"))

	callResult, output, err := LaunchNetrunnerWaves(context.Background(), nil, LaunchNetrunnerWavesInput{
		WaveIds:        []int{first.WaveId, second.WaveId},
		TimeoutSeconds: 1,
	})
	if err != nil || callResult != nil {
		t.Fatalf("batch launch: result=%+v err=%v", callResult, err)
	}
	if output.Status != "success" || output.DetailLevel != parallelWaveBatchDetailSummary || len(output.Results) != 2 {
		t.Fatalf("unexpected compact batch launch output: %+v", output)
	}
	for _, result := range output.Results {
		if result.Status != "success" || result.Summary == nil || result.Output != nil {
			t.Fatalf("expected compact successful result, got %+v", result)
		}
	}
	if dirty := runGitTestCommand(t, first.Wave.ProjectCwd, "status", "--porcelain=v1", "--untracked-files=no"); dirty != "" {
		t.Fatalf("launcher contaminated canonical tracked files: %s", dirty)
	}
}

func TestParallelWaveBatchCompactPayloadAndFullCompatibility(t *testing.T) {
	largeReport := strings.Repeat("diagnostic report payload ", 5000)
	wave := NetrunnerWaveSnapshot{
		Id:                 293,
		ProjectId:          2,
		Status:             parallelWaveStatusReviewReady,
		Phase:              parallelWavePhaseImplementation,
		GateState:          parallelWaveGateImplementationRepair,
		ControlState:       parallelWaveControlActive,
		FailurePolicyState: parallelWaveFailurePolicyRepairRequired,
		FailureReason:      largeReport,
		Workers: []NetrunnerWaveWorkerSnapshot{
			{Id: 1, SessionId: 1, Status: parallelWaveWorkerStatusReviewReady, SessionReport: largeReport},
			{Id: 2, SessionId: 2, Status: parallelWaveWorkerStatusFailed, SessionReport: largeReport},
		},
	}
	summary := buildNetrunnerWaveOperatorSummary(wave)
	compact := LaunchNetrunnerWavesOutput{
		Status:      "success",
		DetailLevel: parallelWaveBatchDetailSummary,
		Results:     []LaunchNetrunnerWaveBatchResult{{WaveId: wave.Id, Status: "success", Summary: &summary}},
	}
	detailedOutput := LaunchNetrunnerWaveOutput{Status: "success", WaveId: wave.Id, Workers: wave.Workers, Wave: wave}
	detailed := LaunchNetrunnerWavesOutput{
		Status:      "success",
		DetailLevel: parallelWaveBatchDetailFull,
		Results:     []LaunchNetrunnerWaveBatchResult{{WaveId: wave.Id, Status: "success", Summary: &summary, Output: &detailedOutput}},
	}

	compactJSON, err := json.Marshal(compact)
	if err != nil {
		t.Fatalf("marshal compact payload: %v", err)
	}
	detailedJSON, err := json.Marshal(detailed)
	if err != nil {
		t.Fatalf("marshal full payload: %v", err)
	}
	if len(compactJSON) > 4096 {
		t.Fatalf("compact per-wave payload exceeded 4 KiB: %d bytes", len(compactJSON))
	}
	if strings.Count(string(compactJSON), "diagnostic report payload") > 12 || strings.Contains(string(compactJSON), `"output"`) {
		t.Fatalf("compact payload leaked full diagnostics: %s", compactJSON)
	}
	if len(detailedJSON) <= len(compactJSON)*10 || !strings.Contains(string(detailedJSON), "diagnostic report payload") {
		t.Fatalf("full compatibility payload did not retain legacy diagnostics: compact=%d full=%d", len(compactJSON), len(detailedJSON))
	}
}

func TestParallelWaveBatchDetailLevelValidation(t *testing.T) {
	tests := []struct {
		input string
		want  string
		ok    bool
	}{
		{input: "", want: parallelWaveBatchDetailSummary, ok: true},
		{input: " SUMMARY ", want: parallelWaveBatchDetailSummary, ok: true},
		{input: "full", want: parallelWaveBatchDetailFull, ok: true},
		{input: "verbose", ok: false},
	}
	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			got, err := parallelWaveBatchDetailLevel(test.input)
			if (err == nil) != test.ok || got != test.want {
				t.Fatalf("detail level %q = %q, %v", test.input, got, err)
			}
		})
	}
}

func TestValidateParallelWaveBatchIDsAllowsSingleWaveAndRejectsEmptyBatch(t *testing.T) {
	originalDB := db
	defer func() { db = originalDB }()
	testDB, first, _ := setupTwoSingleWorkerWaves(t)
	defer testDB.Close()
	db = testDB

	if err := validateParallelWaveBatchIDs([]int{first.WaveId}, 1); err != nil {
		t.Fatalf("single-wave batch should be allowed, got: %v", err)
	}
	if err := validateParallelWaveBatchIDs(nil, 1); err == nil || !strings.Contains(err.Error(), "at least one wave ID") {
		t.Fatalf("expected empty-batch rejection, got: %v", err)
	}
}

func TestParallelWaveBatchWaitStateSeparatesWaitAndLifecycleTerminality(t *testing.T) {
	tests := []struct {
		name          string
		wave          NetrunnerWaveSnapshot
		result        NetrunnerWaveWaitResult
		wantState     string
		wantCondition string
		wantFollowUp  bool
	}{
		{
			name:      "wave 289 all terminal and review ready",
			wave:      NetrunnerWaveSnapshot{Id: 289, Status: parallelWaveStatusReviewReady, Phase: parallelWavePhaseImplementation, GateState: parallelWaveGateImplementationReview, ControlState: parallelWaveControlActive, FailurePolicyState: parallelWaveFailurePolicyPassed, Workers: []NetrunnerWaveWorkerSnapshot{{Id: 1, Status: parallelWaveWorkerStatusReviewReady}}},
			result:    NetrunnerWaveWaitResult{Terminal: true, TerminalCondition: parallelWaveWaitAllTerminal, WorkerId: 1, WorkerStatus: parallelWaveWorkerStatusReviewReady, FollowUpAllowed: true},
			wantState: "wave_review_ready", wantCondition: parallelWaveWaitAllTerminal, wantFollowUp: true,
		},
		{
			name:      "wave 293 all terminal but repair follow up blocked",
			wave:      NetrunnerWaveSnapshot{Id: 293, Status: parallelWaveStatusReviewReady, Phase: parallelWavePhaseImplementation, GateState: parallelWaveGateImplementationRepair, ControlState: parallelWaveControlActive, FailurePolicyState: parallelWaveFailurePolicyRepairRequired, Workers: []NetrunnerWaveWorkerSnapshot{{Id: 1, Status: parallelWaveWorkerStatusReviewReady}, {Id: 2, Status: parallelWaveWorkerStatusFailed}}},
			result:    NetrunnerWaveWaitResult{Terminal: true, TerminalCondition: "follow_up_blocked", FollowUpAllowed: false, FollowUpBlockedReason: "governed_implementation_repair_required"},
			wantState: "repair_blocked", wantCondition: "follow_up_blocked", wantFollowUp: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			summary := buildNetrunnerWaveOperatorSummary(test.wave)
			waitState := parallelWaveBatchWaitState(WaitForNetrunnerWaveOutput{Status: "success", Result: test.result}, &summary)
			if summary.OperatorState != test.wantState || !summary.AllWorkersTerminal || waitState == nil || !waitState.WaitResultTerminal || !waitState.AllWorkersTerminal || waitState.TerminalCondition != test.wantCondition || waitState.FollowUpAllowed != test.wantFollowUp {
				t.Fatalf("ambiguous compact state: summary=%+v wait=%+v", summary, waitState)
			}
		})
	}
}

func TestWaitForNetrunnerWavesReturnsPerWaveValidationErrors(t *testing.T) {
	originalDB, originalRole, originalProjectID := db, authorizedRole, authorizedProjectId
	defer func() {
		db, authorizedRole, authorizedProjectId = originalDB, originalRole, originalProjectID
	}()
	testDB, first, second := setupTwoSingleWorkerWaves(t)
	defer testDB.Close()

	callResult, output, err := WaitForNetrunnerWaves(context.Background(), nil, WaitForNetrunnerWavesInput{Waves: []WaitForNetrunnerWaveInput{
		{WaveId: first.WaveId, ReturnWhen: "after_lunch"},
		{WaveId: second.WaveId, TimeoutSeconds: 299},
	}})
	if err != nil || callResult != nil {
		t.Fatalf("batch wait should isolate item errors: result=%+v err=%v", callResult, err)
	}
	if output.Status != "partial_failure" || len(output.Results) != 2 {
		t.Fatalf("unexpected batch wait output: %+v", output)
	}
	if !strings.Contains(output.Results[0].Error, "unsupported return_when") || !strings.Contains(output.Results[1].Error, "must be >= 300") {
		t.Fatalf("unexpected isolated wait errors: %+v", output.Results)
	}
}

func TestWaitForNetrunnerWavesCompactLifecycleStates(t *testing.T) {
	originalDB, originalRole, originalProjectID, originalExecCommand := db, authorizedRole, authorizedProjectId, execCommand
	defer func() {
		db, authorizedRole, authorizedProjectId, execCommand = originalDB, originalRole, originalProjectID, originalExecCommand
	}()
	testDB, reviewReady, repairBlocked := setupTwoSingleWorkerWaves(t)
	defer testDB.Close()
	installFakeWaveWorkerLauncher(t, "", nil)

	if _, err := testDB.Exec(
		`UPDATE parallel_wave_worker SET status = ?, terminal_outcome = ?, terminal_at = CURRENT_TIMESTAMP WHERE wave_id = ?`,
		parallelWaveWorkerStatusReviewReady,
		parallelWaveWorkerStatusReviewReady,
		reviewReady.WaveId,
	); err != nil {
		t.Fatalf("seed review-ready worker: %v", err)
	}
	if _, err := testDB.Exec(
		`UPDATE parallel_wave SET status = ?, phase = ?, gate_state = ?, control_state = ?, failure_policy_state = ? WHERE id = ?`,
		parallelWaveStatusReviewReady,
		parallelWavePhaseImplementation,
		parallelWaveGateImplementationReview,
		parallelWaveControlActive,
		parallelWaveFailurePolicyPassed,
		reviewReady.WaveId,
	); err != nil {
		t.Fatalf("seed review-ready wave: %v", err)
	}
	if _, err := testDB.Exec(
		`UPDATE parallel_wave_worker SET status = ?, terminal_outcome = ?, terminal_at = CURRENT_TIMESTAMP, failure_reason = 'parent handoff uncommitted' WHERE wave_id = ?`,
		parallelWaveWorkerStatusFailed,
		parallelWaveWorkerStatusFailed,
		repairBlocked.WaveId,
	); err != nil {
		t.Fatalf("seed repair-blocked worker: %v", err)
	}
	if _, err := testDB.Exec(
		`UPDATE parallel_wave SET status = ?, phase = ?, gate_state = ?, control_state = ?, failure_policy_state = ?, failure_reason = 'parent handoff uncommitted' WHERE id = ?`,
		parallelWaveStatusReviewReady,
		parallelWavePhaseImplementation,
		parallelWaveGateImplementationRepair,
		parallelWaveControlActive,
		parallelWaveFailurePolicyRepairRequired,
		repairBlocked.WaveId,
	); err != nil {
		t.Fatalf("seed repair-blocked wave: %v", err)
	}

	callResult, output, err := WaitForNetrunnerWaves(context.Background(), nil, WaitForNetrunnerWavesInput{
		WaveIds:             []int{reviewReady.WaveId, repairBlocked.WaveId},
		TimeoutSeconds:      300,
		PollIntervalSeconds: 1,
		ReturnWhen:          parallelWaveWaitAllTerminal,
	})
	if err != nil || callResult != nil {
		t.Fatalf("compact batch wait: result=%+v err=%v", callResult, err)
	}
	if output.Status != "success" || output.DetailLevel != parallelWaveBatchDetailSummary || len(output.Results) != 2 {
		t.Fatalf("unexpected compact batch wait: %+v", output)
	}
	first, second := output.Results[0], output.Results[1]
	if first.Output != nil || first.Summary == nil || first.Wait == nil || first.Summary.OperatorState != "wave_review_ready" || !first.Summary.AllWorkersTerminal || first.Wait.TerminalCondition != parallelWaveWaitAllTerminal || !first.Wait.FollowUpAllowed {
		t.Fatalf("unexpected review-ready compact result: %+v", first)
	}
	if second.Output != nil || second.Status != "blocked" || second.Summary == nil || second.Wait == nil || second.Summary.OperatorState != "repair_blocked" || !second.Summary.AllWorkersTerminal || second.Wait.TerminalCondition != "follow_up_blocked" || second.Wait.FollowUpAllowed {
		t.Fatalf("unexpected repair-blocked compact result: %+v", second)
	}

	_, detailed, err := WaitForNetrunnerWaves(context.Background(), nil, WaitForNetrunnerWavesInput{
		WaveIds:             []int{reviewReady.WaveId, repairBlocked.WaveId},
		TimeoutSeconds:      300,
		PollIntervalSeconds: 1,
		ReturnWhen:          parallelWaveWaitAllTerminal,
		DetailLevel:         parallelWaveBatchDetailFull,
	})
	if err != nil || detailed.DetailLevel != parallelWaveBatchDetailFull || detailed.Results[0].Output == nil || detailed.Results[1].Output == nil {
		t.Fatalf("full detail wait did not preserve legacy outputs: output=%+v err=%v", detailed, err)
	}
	if len(detailed.Results[0].Output.Result.Wave.Workers) != 1 || detailed.Results[1].Output.Result.FollowUpBlockedReason == "" {
		t.Fatalf("full detail wait lost legacy diagnostics: %+v", detailed.Results)
	}
}

func TestWaveBatchPrevalidationRejectsCrossProjectIDsBeforeMutation(t *testing.T) {
	originalDB, originalRole, originalProjectID := db, authorizedRole, authorizedProjectId
	defer func() {
		db, authorizedRole, authorizedProjectId = originalDB, originalRole, originalProjectID
	}()
	testDB, first, _ := setupTwoSingleWorkerWaves(t)
	defer testDB.Close()
	result, err := testDB.Exec(
		`INSERT INTO parallel_wave (
			project_id, status, phase, gate_state, control_state, control_reason,
			base_sha, base_branch, project_cwd, worktree_root, orchestration_epoch,
			root_wave_id, depth, max_child_wave_depth, max_total_descendant_waves, max_total_sessions
		) VALUES (2, 'created', 'initialized', 'none', 'active', '', 'abc', 'main', '/tmp/other-wave-project', '.codex/netrunner_worktrees', 0, 0, 0, 0, 0, 128)`,
	)
	if err != nil {
		t.Fatalf("seed cross-project wave: %v", err)
	}
	otherWaveID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("cross-project wave ID: %v", err)
	}

	callResult, _, err := LaunchNetrunnerWaves(context.Background(), nil, LaunchNetrunnerWavesInput{WaveIds: []int{first.WaveId, int(otherWaveID)}})
	if err == nil || callResult == nil || !callResult.IsError || !strings.Contains(err.Error(), "does not belong to current project") {
		t.Fatalf("expected cross-project batch rejection, result=%+v err=%v", callResult, err)
	}
	wave, err := fetchNetrunnerWaveSnapshot(first.WaveId, 1)
	if err != nil {
		t.Fatalf("fetch prevalidated wave: %v", err)
	}
	if wave.Status != parallelWaveStatusCreated {
		t.Fatalf("cross-project prevalidation must run before mutation, got %q", wave.Status)
	}
}
