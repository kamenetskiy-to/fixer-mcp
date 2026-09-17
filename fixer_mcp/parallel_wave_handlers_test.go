package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestHelperProcessWaveWorkerLaunch(t *testing.T) {
	if os.Getenv("GO_WANT_WAVE_WORKER_LAUNCH") != "1" {
		return
	}
	args := os.Args
	separator := 0
	for index, arg := range args {
		if arg == "--" {
			separator = index + 1
			break
		}
	}
	if separator == 0 {
		os.Exit(2)
	}
	if trackedPath := os.Getenv("FAKE_TRACKED_BYTECODE_PATH"); trackedPath != "" && os.Getenv(pythonNoBytecodeEnv) != "1" {
		if err := os.WriteFile(trackedPath, []byte("runtime bytecode contamination\n"), 0o644); err != nil {
			os.Exit(9)
		}
	}
	launchArgs := args[separator:]
	valueAfter := func(flag string) string {
		for index := 0; index+1 < len(launchArgs); index++ {
			if launchArgs[index] == flag {
				return launchArgs[index+1]
			}
		}
		return ""
	}
	if valueAfter("--session-id") == os.Getenv("FAIL_WAVE_WORKER_SESSION_ID") {
		_, _ = os.Stderr.WriteString("fake wave worker launch failure\n")
		os.Exit(3)
	}
	metadataPath := valueAfter("--worker-metadata-path")
	if metadataPath == "" {
		os.Exit(4)
	}
	workerPID, err := strconv.Atoi(os.Getenv("FAKE_WAVE_WORKER_PID"))
	if err != nil || workerPID <= 0 {
		workerPID = os.Getppid()
	}
	sessionID, err := strconv.Atoi(valueAfter("--session-id"))
	if err != nil {
		os.Exit(8)
	}
	payload := map[string]any{
		"worker_pid":        workerPID,
		"headless_log_path": valueAfter("--headless-log-path"),
		"backend":           valueAfter("--backend"),
		"session_id":        sessionID,
	}
	if err := os.MkdirAll(filepath.Dir(metadataPath), 0o755); err != nil {
		os.Exit(5)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		os.Exit(6)
	}
	if err := os.WriteFile(metadataPath, append(encoded, '\n'), 0o644); err != nil {
		os.Exit(7)
	}
	os.Exit(0)
}

func setupParallelWaveTestDB(t *testing.T, projectCWD string) *sql.DB {
	t.Helper()

	testDB := setupGetProjectsTestDB(t)
	normalizedProjectCWD, err := normalizeProjectCWD(projectCWD)
	if err != nil {
		_ = testDB.Close()
		t.Fatalf("normalize wave project cwd: %v", err)
	}

	_, err = testDB.Exec(`
		CREATE TABLE parallel_wave (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL,
			status TEXT NOT NULL DEFAULT 'created',
			phase TEXT NOT NULL DEFAULT 'initialized',
			gate_state TEXT NOT NULL DEFAULT 'none',
			control_state TEXT NOT NULL DEFAULT 'active',
			control_reason TEXT NOT NULL DEFAULT '',
			base_sha TEXT NOT NULL,
			base_branch TEXT NOT NULL DEFAULT '',
			project_cwd TEXT NOT NULL,
			worktree_root TEXT NOT NULL,
			orchestration_epoch INTEGER NOT NULL DEFAULT 0,
			created_by_session_id INTEGER,
			epic_doc_id INTEGER,
			parent_wave_id INTEGER,
			root_wave_id INTEGER,
			depth INTEGER NOT NULL DEFAULT 0,
			max_child_wave_depth INTEGER NOT NULL DEFAULT 0,
			max_total_descendant_waves INTEGER NOT NULL DEFAULT 0,
			max_total_sessions INTEGER NOT NULL DEFAULT 0,
			failure_policy_state TEXT NOT NULL DEFAULT 'none',
			repair_worker_id INTEGER,
			repair_attempt_count INTEGER NOT NULL DEFAULT 0,
			handoff_sha TEXT NOT NULL DEFAULT '',
			acceptance_session_id INTEGER,
			review_policy TEXT NOT NULL DEFAULT 'manual',
			review_backend TEXT NOT NULL DEFAULT 'codex',
			review_model TEXT NOT NULL DEFAULT 'opencode-go/deepseek-v4-pro',
			review_reasoning TEXT NOT NULL DEFAULT 'high',
			failure_reason TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			launched_at TEXT,
			completed_at TEXT
		);
		CREATE TABLE parallel_wave_worker (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			wave_id INTEGER NOT NULL,
			project_id INTEGER NOT NULL,
			session_id INTEGER NOT NULL,
			status TEXT NOT NULL DEFAULT 'created',
			declared_write_scope TEXT NOT NULL,
			branch_name TEXT NOT NULL,
			worktree_path TEXT NOT NULL,
			base_sha TEXT NOT NULL,
			head_sha TEXT NOT NULL DEFAULT '',
			changed_paths TEXT NOT NULL DEFAULT '[]',
			diff_patch_path TEXT NOT NULL DEFAULT '',
			diff_stat TEXT NOT NULL DEFAULT '',
			launch_epoch INTEGER NOT NULL DEFAULT 0,
			worker_process_id INTEGER,
			external_session_id TEXT NOT NULL DEFAULT '',
			headless_log_path TEXT NOT NULL DEFAULT '',
			launcher_log_path TEXT NOT NULL DEFAULT '',
			worker_metadata_path TEXT NOT NULL DEFAULT '',
			failure_reason TEXT NOT NULL DEFAULT '',
			terminal_outcome TEXT NOT NULL DEFAULT '',
			retry_attempt_count INTEGER NOT NULL DEFAULT 0,
			retry_cause TEXT NOT NULL DEFAULT '',
			retry_next_eligible_at TEXT NOT NULL DEFAULT '',
			cleanup_status TEXT NOT NULL DEFAULT 'pending',
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			launched_at TEXT,
			terminal_at TEXT,
			cleaned_at TEXT
			);
			CREATE TABLE wave_worker_dependency (
				wave_id INTEGER NOT NULL,
				parent_session_id INTEGER NOT NULL,
				child_session_id INTEGER NOT NULL,
				FOREIGN KEY(wave_id) REFERENCES parallel_wave(id) ON DELETE CASCADE ON UPDATE NO ACTION,
				FOREIGN KEY(parent_session_id) REFERENCES session(id) ON DELETE CASCADE ON UPDATE NO ACTION,
				FOREIGN KEY(child_session_id) REFERENCES session(id) ON DELETE CASCADE ON UPDATE NO ACTION
			);
			CREATE UNIQUE INDEX wave_worker_dependency_unique_idx ON wave_worker_dependency(wave_id, parent_session_id, child_session_id);
			CREATE UNIQUE INDEX parallel_wave_worker_wave_session_unique_idx ON parallel_wave_worker(wave_id, session_id);
		CREATE INDEX parallel_wave_worker_status_idx ON parallel_wave_worker(project_id, status);
		CREATE TABLE parallel_wave_scope_lease (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL,
			wave_id INTEGER NOT NULL,
			scope_path TEXT NOT NULL,
			active INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			released_at TEXT
		);
		CREATE UNIQUE INDEX parallel_wave_scope_lease_wave_scope_unique_idx ON parallel_wave_scope_lease(wave_id, scope_path);
		CREATE INDEX parallel_wave_scope_lease_active_idx ON parallel_wave_scope_lease(project_id, active, wave_id);
		CREATE TABLE mcp_binary_state (
			project_id INTEGER PRIMARY KEY,
			running_build_epoch INTEGER NOT NULL DEFAULT 0,
			required_build_epoch INTEGER NOT NULL DEFAULT 0,
			restart_required INTEGER NOT NULL DEFAULT 0,
			running_build_id TEXT NOT NULL DEFAULT '',
			required_build_id TEXT NOT NULL DEFAULT '',
			running_process_identity TEXT NOT NULL DEFAULT '',
			required_by_process_identity TEXT NOT NULL DEFAULT '',
			confirmed_at TEXT,
			reason TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		ALTER TABLE worker_process ADD COLUMN parallel_wave_id INTEGER;
		ALTER TABLE worker_process ADD COLUMN parallel_wave_worker_id INTEGER;
		UPDATE project SET cwd = ? WHERE id = 1;
		UPDATE session SET declared_write_scope = '["docs/a"]' WHERE id = 1;
		INSERT INTO session (project_id, task_description, status, declared_write_scope) VALUES (1, 'Task C', 'pending', '["docs/b"]');
	`, normalizedProjectCWD)
	if err != nil {
		_ = testDB.Close()
		t.Fatalf("seed wave db: %v", err)
	}
	return testDB
}

func runGitTestCommand(t *testing.T, dir string, args ...string) string {
	t.Helper()
	commandArgs := append([]string{"-C", dir}, args...)
	output, err := exec.Command("git", commandArgs...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(commandArgs, " "), err, string(output))
	}
	return strings.TrimSpace(string(output))
}

func setupCleanGitRepo(t *testing.T) string {
	t.Helper()
	repoDir := t.TempDir()
	setupCleanGitRepoAt(t, repoDir)
	return repoDir
}

func setupCleanGitRepoAt(t *testing.T, repoDir string) {
	t.Helper()
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("create Git repo directory: %v", err)
	}
	runGitTestCommand(t, repoDir, "init")
	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("initial\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGitTestCommand(t, repoDir, "add", "README.md")
	runGitTestCommand(t, repoDir, "-c", "user.name=Fixer Test", "-c", "user.email=fixer@example.test", "commit", "-m", "initial")
}

func TestNormalizeParallelWaveAdmissionWorkers(t *testing.T) {
	t.Run("accepts disjoint normalized worker scopes", func(t *testing.T) {
		workers, err := normalizeParallelWaveAdmissionWorkers([]parallelWaveAdmissionWorker{
			{SessionID: 7, DeclaredWriteScope: []string{"docs/research"}},
			{SessionID: 8, DeclaredWriteScope: []string{"fixer_mcp/wave_helpers_test.go"}},
		})
		if err != nil {
			t.Fatalf("normalize wave workers failed: %v", err)
		}
		if len(workers) != 2 {
			t.Fatalf("expected two workers, got %+v", workers)
		}
		if workers[0].DeclaredWriteScope[0] != "docs/research" || workers[1].DeclaredWriteScope[0] != "fixer_mcp/wave_helpers_test.go" {
			t.Fatalf("unexpected normalized workers: %+v", workers)
		}
	})

	t.Run("rejects duplicate sessions and overlapping workers", func(t *testing.T) {
		if _, err := normalizeParallelWaveAdmissionWorkers([]parallelWaveAdmissionWorker{
			{SessionID: 7, DeclaredWriteScope: []string{"docs/a"}},
			{SessionID: 7, DeclaredWriteScope: []string{"docs/b"}},
		}); err == nil || !strings.Contains(err.Error(), "duplicated") {
			t.Fatalf("expected duplicate session rejection, got %v", err)
		}
		if _, err := normalizeParallelWaveAdmissionWorkers([]parallelWaveAdmissionWorker{
			{SessionID: 7, DeclaredWriteScope: []string{"docs"}},
			{SessionID: 8, DeclaredWriteScope: []string{"docs/research"}},
		}); err == nil || !strings.Contains(err.Error(), "overlapping declared write scopes") {
			t.Fatalf("expected cross-worker overlap rejection, got %v", err)
		}
	})
}

func TestParallelWaveWaitTimeoutSeconds(t *testing.T) {
	tests := []struct {
		name          string
		raw           int
		want          int
		expectError   bool
		errorFragment string
	}{
		{name: "omitted defaults to 300", raw: 0, want: 300},
		{name: "299 is rejected", raw: 299, expectError: true, errorFragment: "must be >= 300"},
		{name: "300 is accepted", raw: 300, want: 300},
		{name: "21600 is accepted", raw: 21600, want: 21600},
		{name: "21601 is rejected", raw: 21601, expectError: true, errorFragment: "must be <= 21600"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parallelWaveWaitTimeoutSeconds(tt.raw)
			if tt.expectError {
				if err == nil || !strings.Contains(err.Error(), tt.errorFragment) {
					t.Fatalf("expected error containing %q, got value=%d error=%v", tt.errorFragment, got, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected timeout validation error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("expected normalized timeout %d, got %d", tt.want, got)
			}
		})
	}
}

func TestParallelWaveNamingHelpers(t *testing.T) {
	branchName, err := parallelWaveBranchName(42, 9)
	if err != nil {
		t.Fatalf("parallelWaveBranchName failed: %v", err)
	}
	if branchName != "fixer/wave-42/session-9" {
		t.Fatalf("unexpected branch name: %q", branchName)
	}

	worktreePath, err := parallelWaveWorktreePath("", 42, 9)
	if err != nil {
		t.Fatalf("parallelWaveWorktreePath failed: %v", err)
	}
	if worktreePath != ".codex/netrunner_worktrees/wave-42/session-9" {
		t.Fatalf("unexpected worktree path: %q", worktreePath)
	}

	customPath, err := parallelWaveWorktreePath("/tmp/waves", 42, 9)
	if err != nil {
		t.Fatalf("parallelWaveWorktreePath with absolute root failed: %v", err)
	}
	if customPath != "/tmp/waves/wave-42/session-9" {
		t.Fatalf("unexpected absolute worktree path: %q", customPath)
	}
}

func TestNormalizeWaveDependenciesRejectsUnknownSessionsAndCycles(t *testing.T) {
	cases := []struct {
		name          string
		dependencies  []WaveDependency
		errorFragment string
	}{
		{
			name:          "unknown child",
			dependencies:  []WaveDependency{{Child: 3, Parents: []int64{1}}},
			errorFragment: "child session 3",
		},
		{
			name:          "unknown parent",
			dependencies:  []WaveDependency{{Child: 2, Parents: []int64{3}}},
			errorFragment: "parent session 3",
		},
		{
			name: "cycle",
			dependencies: []WaveDependency{
				{Child: 1, Parents: []int64{2}},
				{Child: 2, Parents: []int64{1}},
			},
			errorFragment: "cycle",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := normalizeWaveDependencies(tc.dependencies, []int{1, 2})
			if err == nil || !strings.Contains(err.Error(), tc.errorFragment) {
				t.Fatalf("expected %s rejection, got %v", tc.name, err)
			}
		})
	}
}

func TestParallelWaveGitCommandHelpers(t *testing.T) {
	projectCWD := t.TempDir()
	normalizedProjectCWD, err := normalizeProjectCWD(projectCWD)
	if err != nil {
		t.Fatalf("normalizeProjectCWD failed: %v", err)
	}

	rootCommand, err := gitRootCommand(projectCWD)
	if err != nil {
		t.Fatalf("gitRootCommand failed: %v", err)
	}
	if rootCommand.Name != "git" || strings.Join(rootCommand.Args, " ") != "-C "+normalizedProjectCWD+" rev-parse --show-toplevel" {
		t.Fatalf("unexpected root command: %+v", rootCommand)
	}

	statusCommand, err := gitTrackedCleanStatusCommand(projectCWD)
	if err != nil {
		t.Fatalf("gitTrackedCleanStatusCommand failed: %v", err)
	}
	if strings.Join(statusCommand.Args, " ") != "-C "+normalizedProjectCWD+" status --porcelain=v1 --untracked-files=no" {
		t.Fatalf("unexpected status command: %+v", statusCommand)
	}

	baseCommand, err := gitBaseSHACommand(projectCWD, "")
	if err != nil {
		t.Fatalf("gitBaseSHACommand failed: %v", err)
	}
	if strings.Join(baseCommand.Args, " ") != "-C "+normalizedProjectCWD+" rev-parse --verify HEAD^{commit}" {
		t.Fatalf("unexpected base command: %+v", baseCommand)
	}
}

func TestCreateNetrunnerWaveRejectsNonGitProjectCWD(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
	}()

	testDB := setupParallelWaveTestDB(t, t.TempDir())
	defer func() {
		_ = testDB.Close()
	}()

	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1

	callResult, _, err := CreateNetrunnerWave(context.Background(), nil, CreateNetrunnerWaveInput{SessionIds: []int{1, 2}})
	if err == nil {
		t.Fatal("expected non-Git project cwd rejection")
	}
	if callResult == nil || !callResult.IsError {
		t.Fatal("expected MCP error result")
	}
	if !strings.Contains(err.Error(), "not a Git repository") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateNetrunnerWaveRejectsDirtyTrackedState(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
	}()

	repoDir := setupCleanGitRepo(t)
	testDB := setupParallelWaveTestDB(t, repoDir)
	defer func() {
		_ = testDB.Close()
	}()
	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatalf("dirty README: %v", err)
	}

	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1

	callResult, _, err := CreateNetrunnerWave(context.Background(), nil, CreateNetrunnerWaveInput{SessionIds: []int{1, 2}})
	if err == nil {
		t.Fatal("expected dirty tracked state rejection")
	}
	if callResult == nil || !callResult.IsError {
		t.Fatal("expected MCP error result")
	}
	if !strings.Contains(err.Error(), "tracked working tree must be clean") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateNetrunnerWaveRejectsNonPendingAndActiveWorkers(t *testing.T) {
	cases := []struct {
		name          string
		mutate        func(*testing.T, *sql.DB)
		errorFragment string
	}{
		{
			name: "non_pending",
			mutate: func(t *testing.T, testDB *sql.DB) {
				t.Helper()
				if _, err := testDB.Exec("UPDATE session SET status = 'in_progress' WHERE id = 1"); err != nil {
					t.Fatalf("seed in_progress: %v", err)
				}
			},
			errorFragment: "must be pending",
		},
		{
			name: "active_worker",
			mutate: func(t *testing.T, testDB *sql.DB) {
				t.Helper()
				if _, err := testDB.Exec("INSERT INTO worker_process (project_id, session_id, pid, launch_epoch, status) VALUES (1, 1, ?, 0, 'running')", os.Getpid()); err != nil {
					t.Fatalf("seed active worker: %v", err)
				}
			},
			errorFragment: "active worker processes",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			originalDB := db
			originalRole := authorizedRole
			originalProjectID := authorizedProjectId
			defer func() {
				db = originalDB
				authorizedRole = originalRole
				authorizedProjectId = originalProjectID
			}()

			repoDir := setupCleanGitRepo(t)
			testDB := setupParallelWaveTestDB(t, repoDir)
			defer func() {
				_ = testDB.Close()
			}()
			tc.mutate(t, testDB)

			db = testDB
			authorizedRole = "fixer"
			authorizedProjectId = 1

			callResult, _, err := CreateNetrunnerWave(context.Background(), nil, CreateNetrunnerWaveInput{SessionIds: []int{1, 2}})
			if err == nil {
				t.Fatalf("expected %s rejection", tc.name)
			}
			if callResult == nil || !callResult.IsError {
				t.Fatal("expected MCP error result")
			}
			if !strings.Contains(err.Error(), tc.errorFragment) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestCreateAndGetNetrunnerWavePersistsSnapshot(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
	}()

	repoDir := setupCleanGitRepo(t)
	baseSHA := runGitTestCommand(t, repoDir, "rev-parse", "--verify", "HEAD^{commit}")
	testDB := setupParallelWaveTestDB(t, repoDir)
	defer func() {
		_ = testDB.Close()
	}()

	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1

	callResult, created, err := CreateNetrunnerWave(context.Background(), nil, CreateNetrunnerWaveInput{
		SessionIds:   []int{1, 2},
		WorktreeRoot: ".codex/custom_wave_root",
		BaseRef:      "HEAD",
		Reason:       "test wave",
	})
	if err != nil {
		t.Fatalf("create_netrunner_wave failed: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil call result on success, got %+v", callResult)
	}
	if created.WaveId <= 0 || created.BaseSha != baseSHA || created.WorktreeRoot != ".codex/custom_wave_root" {
		t.Fatalf("unexpected create output: %+v", created)
	}
	if len(created.Workers) != 2 {
		t.Fatalf("expected two workers, got %+v", created.Workers)
	}
	if created.Wave.ReviewPolicy != parallelWaveReviewPolicyManual ||
		created.Wave.ReviewBackend != defaultParallelWaveReviewBackend ||
		created.Wave.ReviewModel != defaultParallelWaveReviewModel ||
		created.Wave.ReviewReasoning != defaultParallelWaveReviewReasoning {
		t.Fatalf("unexpected default review config: %+v", created.Wave)
	}
	for _, worker := range created.Workers {
		if worker.Status != parallelWaveWorkerStatusCreated {
			t.Fatalf("unexpected worker status: %+v", worker)
		}
		if !strings.Contains(worker.BranchName, "fixer/wave-") || !strings.Contains(worker.WorktreePath, "wave-") {
			t.Fatalf("missing deterministic worker naming: %+v", worker)
		}
	}

	var linked string
	if err := testDB.QueryRow("SELECT parallel_wave_id FROM session WHERE id = 1").Scan(&linked); err != nil {
		t.Fatalf("query session linkage: %v", err)
	}
	if linked != strconv.Itoa(created.WaveId) {
		t.Fatalf("expected session parallel_wave_id %d, got %q", created.WaveId, linked)
	}
	var workerCount int
	if err := testDB.QueryRow("SELECT COUNT(*) FROM parallel_wave_worker WHERE wave_id = ?", created.WaveId).Scan(&workerCount); err != nil {
		t.Fatalf("query worker count: %v", err)
	}
	if workerCount != 2 {
		t.Fatalf("expected two worker rows, got %d", workerCount)
	}

	callResult, got, err := GetNetrunnerWave(context.Background(), nil, GetNetrunnerWaveInput{WaveId: created.WaveId})
	if err != nil {
		t.Fatalf("get_netrunner_wave failed: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil call result on get success, got %+v", callResult)
	}
	if got.Wave.Id != created.WaveId || got.Wave.Status != parallelWaveStatusCreated || len(got.Wave.Workers) != 2 {
		t.Fatalf("unexpected get output: %+v", got)
	}
	if got.Wave.ReviewPolicy != parallelWaveReviewPolicyManual {
		t.Fatalf("expected persisted manual review policy, got %+v", got.Wave)
	}
	if got.Wave.Workers[0].SessionId != 1 || got.Wave.Workers[1].SessionId != 2 {
		t.Fatalf("expected project-scoped session ids in get output, got %+v", got.Wave.Workers)
	}
}

func TestCreateNetrunnerWavePersistsAutomaticReviewConfig(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
	}()

	repoDir := setupCleanGitRepo(t)
	testDB := setupParallelWaveTestDB(t, repoDir)
	defer testDB.Close()
	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1

	if _, _, err := CreateNetrunnerWave(context.Background(), nil, CreateNetrunnerWaveInput{
		SessionIds:   []int{1},
		ReviewPolicy: parallelWaveReviewPolicyAutomatic,
	}); err == nil || !strings.Contains(err.Error(), "one-worker wave") {
		t.Fatalf("expected automatic monowave rejection, got %v", err)
	}

	_, created, err := CreateNetrunnerWave(context.Background(), nil, CreateNetrunnerWaveInput{
		SessionIds:      []int{1, 2},
		ReviewPolicy:    parallelWaveReviewPolicyAutomatic,
		ReviewBackend:   "codex",
		ReviewModel:     "custom-review-model",
		ReviewReasoning: "medium",
	})
	if err != nil {
		t.Fatalf("create automatic-review wave: %v", err)
	}
	if created.Wave.ReviewPolicy != parallelWaveReviewPolicyAutomatic ||
		created.Wave.ReviewBackend != "codex" ||
		created.Wave.ReviewModel != "custom-review-model" ||
		created.Wave.ReviewReasoning != "medium" {
		t.Fatalf("unexpected persisted automatic review config: %+v", created.Wave)
	}
}

func TestCreateNetrunnerWavePersistsDependenciesAndRejectsInvalidDependencies(t *testing.T) {
	t.Run("persists project scoped dependencies", func(t *testing.T) {
		originalDB := db
		originalRole := authorizedRole
		originalProjectID := authorizedProjectId
		defer func() {
			db = originalDB
			authorizedRole = originalRole
			authorizedProjectId = originalProjectID
		}()

		repoDir := setupCleanGitRepo(t)
		testDB := setupParallelWaveTestDB(t, repoDir)
		defer func() { _ = testDB.Close() }()
		db = testDB
		authorizedRole = "fixer"
		authorizedProjectId = 1

		callResult, created, err := CreateNetrunnerWave(context.Background(), nil, CreateNetrunnerWaveInput{
			SessionIds:   []int{1, 2},
			Dependencies: []WaveDependency{{Child: 2, Parents: []int64{1}}},
			BaseRef:      "HEAD",
		})
		if err != nil {
			t.Fatalf("create wave with dependency failed: %v", err)
		}
		if callResult != nil {
			t.Fatalf("expected nil call result, got %+v", callResult)
		}
		if len(created.Wave.Dependencies) != 1 || created.Wave.Dependencies[0].Child != 2 || len(created.Wave.Dependencies[0].Parents) != 1 || created.Wave.Dependencies[0].Parents[0] != 1 {
			t.Fatalf("unexpected created dependencies: %+v", created.Wave.Dependencies)
		}

		var parentGlobalID, childGlobalID int
		if err := testDB.QueryRow(
			"SELECT parent_session_id, child_session_id FROM wave_worker_dependency WHERE wave_id = ?",
			created.WaveId,
		).Scan(&parentGlobalID, &childGlobalID); err != nil {
			t.Fatalf("query persisted dependency: %v", err)
		}
		if parentGlobalID != 1 || childGlobalID != 3 {
			t.Fatalf("expected global dependency 1 -> 3, got %d -> %d", parentGlobalID, childGlobalID)
		}

		_, got, err := GetNetrunnerWave(context.Background(), nil, GetNetrunnerWaveInput{WaveId: created.WaveId})
		if err != nil {
			t.Fatalf("get wave with dependency failed: %v", err)
		}
		if len(got.Wave.Dependencies) != 1 || got.Wave.Dependencies[0].Child != 2 || got.Wave.Dependencies[0].Parents[0] != 1 {
			t.Fatalf("unexpected fetched dependencies: %+v", got.Wave.Dependencies)
		}
	})

	cases := []struct {
		name         string
		dependencies []WaveDependency
	}{
		{name: "unknown child", dependencies: []WaveDependency{{Child: 3, Parents: []int64{1}}}},
		{name: "unknown parent", dependencies: []WaveDependency{{Child: 2, Parents: []int64{3}}}},
		{name: "cycle", dependencies: []WaveDependency{{Child: 1, Parents: []int64{2}}, {Child: 2, Parents: []int64{1}}}},
	}
	for _, tc := range cases {
		t.Run("rejects "+tc.name, func(t *testing.T) {
			originalDB := db
			originalRole := authorizedRole
			originalProjectID := authorizedProjectId
			defer func() {
				db = originalDB
				authorizedRole = originalRole
				authorizedProjectId = originalProjectID
			}()

			repoDir := setupCleanGitRepo(t)
			testDB := setupParallelWaveTestDB(t, repoDir)
			defer func() { _ = testDB.Close() }()
			db = testDB
			authorizedRole = "fixer"
			authorizedProjectId = 1

			callResult, _, err := CreateNetrunnerWave(context.Background(), nil, CreateNetrunnerWaveInput{
				SessionIds:   []int{1, 2},
				Dependencies: tc.dependencies,
				BaseRef:      "HEAD",
			})
			if err == nil || callResult == nil || !callResult.IsError {
				t.Fatalf("expected %s validation failure, result=%+v err=%v", tc.name, callResult, err)
			}
		})
	}
}

func TestCreateNetrunnerWaveAllowsOverlappingParentChildScopes(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
	}()

	repoDir := setupCleanGitRepo(t)
	testDB := setupParallelWaveTestDB(t, repoDir)
	defer func() { _ = testDB.Close() }()
	if _, err := testDB.Exec("UPDATE session SET declared_write_scope = ? WHERE id IN (1, 3)", `["docs/shared"]`); err != nil {
		t.Fatalf("seed overlapping parent-child scopes: %v", err)
	}
	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1

	callResult, created, err := CreateNetrunnerWave(context.Background(), nil, CreateNetrunnerWaveInput{
		SessionIds:   []int{1, 2},
		Dependencies: []WaveDependency{{Child: 2, Parents: []int64{1}}},
		BaseRef:      "HEAD",
	})
	if err != nil {
		t.Fatalf("expected overlapping parent-child scopes to be admitted: %v", err)
	}
	if callResult != nil || len(created.Workers) != 2 {
		t.Fatalf("unexpected create output: result=%+v wave=%+v", callResult, created)
	}
}

func createLaunchableTestWave(t *testing.T, testDB *sql.DB) CreateNetrunnerWaveOutput {
	t.Helper()
	callResult, created, err := CreateNetrunnerWave(context.Background(), nil, CreateNetrunnerWaveInput{
		SessionIds: []int{1, 2},
		BaseRef:    "HEAD",
		Reason:     "launch test",
	})
	if err != nil {
		t.Fatalf("create launchable wave: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil create call result, got %+v", callResult)
	}
	var count int
	if err := testDB.QueryRow("SELECT COUNT(*) FROM parallel_wave_worker WHERE wave_id = ?", created.WaveId).Scan(&count); err != nil {
		t.Fatalf("count wave workers: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected two wave workers, got %d", count)
	}
	return created
}

func markTestWaveRunningWithWorktrees(t *testing.T, testDB *sql.DB, repoDir string, created CreateNetrunnerWaveOutput) NetrunnerWaveSnapshot {
	t.Helper()
	for _, worker := range created.Workers {
		absWorktreePath, err := resolveParallelWaveWorktreePath(repoDir, worker.WorktreePath)
		if err != nil {
			t.Fatalf("resolve worker worktree: %v", err)
		}
		if err := os.MkdirAll(filepath.Dir(absWorktreePath), 0o755); err != nil {
			t.Fatalf("prepare worktree parent: %v", err)
		}
		runGitTestCommand(t, repoDir, "worktree", "add", "-b", worker.BranchName, absWorktreePath, created.BaseSha)
		globalSessionID, err := globalSessionIDFromProjectScoped(worker.SessionId, 1)
		if err != nil {
			t.Fatalf("map local session id: %v", err)
		}
		result, err := testDB.Exec(
			`INSERT INTO worker_process (
				project_id,
				session_id,
				pid,
				launch_epoch,
				status,
				parallel_wave_id,
				parallel_wave_worker_id,
				updated_at
			) VALUES (1, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
			globalSessionID,
			os.Getpid(),
			created.Wave.OrchestrationEpoch,
			workerStatusRunning,
			created.WaveId,
			worker.Id,
		)
		if err != nil {
			t.Fatalf("seed worker process: %v", err)
		}
		processID, err := result.LastInsertId()
		if err != nil {
			t.Fatalf("worker process id: %v", err)
		}
		if _, err := testDB.Exec(
			`UPDATE parallel_wave_worker
			 SET status = ?,
			     launch_epoch = ?,
			     worker_process_id = ?,
			     external_session_id = ?,
			     launched_at = CURRENT_TIMESTAMP,
			     updated_at = CURRENT_TIMESTAMP
			 WHERE id = ?`,
			parallelWaveWorkerStatusRunning,
			created.Wave.OrchestrationEpoch,
			int(processID),
			"external-session-"+strconv.Itoa(worker.SessionId),
			worker.Id,
		); err != nil {
			t.Fatalf("mark worker running: %v", err)
		}
		if _, err := testDB.Exec(
			"INSERT INTO session_external_link (session_id, backend, external_session_id) VALUES (?, 'codex', ?)",
			globalSessionID,
			"external-session-"+strconv.Itoa(worker.SessionId),
		); err != nil {
			t.Fatalf("seed session external link: %v", err)
		}
		if _, err := testDB.Exec("UPDATE session SET status = 'in_progress' WHERE id = ? AND project_id = 1", globalSessionID); err != nil {
			t.Fatalf("mark session in_progress: %v", err)
		}
	}
	if _, err := testDB.Exec(
		`UPDATE parallel_wave
		 SET status = ?,
		     launched_at = CURRENT_TIMESTAMP,
		     updated_at = CURRENT_TIMESTAMP
		 WHERE id = ?`,
		parallelWaveStatusRunning,
		created.WaveId,
	); err != nil {
		t.Fatalf("mark wave running: %v", err)
	}
	wave, err := fetchNetrunnerWaveSnapshot(created.WaveId, 1)
	if err != nil {
		t.Fatalf("fetch running wave: %v", err)
	}
	return wave
}

func setupRunningWaveTest(t *testing.T) (string, *sql.DB, CreateNetrunnerWaveOutput, NetrunnerWaveSnapshot) {
	t.Helper()
	repoDir := setupCleanGitRepo(t)
	testDB := setupParallelWaveTestDB(t, repoDir)
	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1
	created := createLaunchableTestWave(t, testDB)
	wave := markTestWaveRunningWithWorktrees(t, testDB, repoDir, created)
	return repoDir, testDB, created, wave
}

func markTestWaveTerminalForCleanup(t *testing.T, testDB *sql.DB, waveID int, workerStatus string) {
	t.Helper()
	waveStatus := parallelWaveStatusCompleted
	switch workerStatus {
	case parallelWaveWorkerStatusReviewReady:
		waveStatus = parallelWaveStatusReviewReady
	case parallelWaveWorkerStatusFailed, parallelWaveWorkerStatusStopped, parallelWaveWorkerStatusStaleEpoch:
		waveStatus = parallelWaveStatusFailed
	case parallelWaveWorkerStatusCleaned:
		waveStatus = parallelWaveStatusCleaned
	}
	if _, err := testDB.Exec(
		`UPDATE parallel_wave_worker
		 SET status = ?,
		     terminal_at = COALESCE(terminal_at, CURRENT_TIMESTAMP),
		     cleanup_status = ?,
		     updated_at = CURRENT_TIMESTAMP
		 WHERE wave_id = ?`,
		workerStatus,
		parallelWaveCleanupStatusPending,
		waveID,
	); err != nil {
		t.Fatalf("mark wave workers terminal: %v", err)
	}
	if _, err := testDB.Exec(
		`UPDATE worker_process
		 SET status = ?,
		     stop_reason = 'test terminal cleanup',
		     stopped_at = COALESCE(stopped_at, CURRENT_TIMESTAMP),
		     updated_at = CURRENT_TIMESTAMP
		 WHERE parallel_wave_id = ?`,
		workerStatusStopped,
		waveID,
	); err != nil {
		t.Fatalf("mark worker processes stopped: %v", err)
	}
	if _, err := testDB.Exec(
		`UPDATE parallel_wave
		 SET status = ?,
		     completed_at = COALESCE(completed_at, CURRENT_TIMESTAMP),
		     updated_at = CURRENT_TIMESTAMP
		 WHERE id = ?`,
		waveStatus,
		waveID,
	); err != nil {
		t.Fatalf("mark wave terminal: %v", err)
	}
}

func testWaveWorkerBySession(t *testing.T, wave NetrunnerWaveSnapshot, localSessionID int) NetrunnerWaveWorkerSnapshot {
	t.Helper()
	for _, worker := range wave.Workers {
		if worker.SessionId == localSessionID {
			return worker
		}
	}
	t.Fatalf("worker for local session %d not found in %+v", localSessionID, wave.Workers)
	return NetrunnerWaveWorkerSnapshot{}
}

func installFakeWaveWorkerLauncher(t *testing.T, failSessionID string, capturedArgs *[][]string) {
	t.Helper()
	t.Setenv("GO_WANT_WAVE_WORKER_LAUNCH", "1")
	t.Setenv("FAKE_WAVE_WORKER_PID", strconv.Itoa(os.Getpid()))
	if failSessionID != "" {
		t.Setenv("FAIL_WAVE_WORKER_SESSION_ID", failSessionID)
	}
	execCommand = func(name string, arg ...string) *exec.Cmd {
		if name == resolveFixerPythonExecutable(os.Environ()) {
			if capturedArgs != nil {
				*capturedArgs = append(*capturedArgs, append([]string{}, arg...))
			}
			helperArgs := append([]string{"-test.run=TestHelperProcessWaveWorkerLaunch", "--"}, arg...)
			return exec.Command(os.Args[0], helperArgs...)
		}
		return exec.Command(name, arg...)
	}
}

func markFakeWaveWorkerExited(t *testing.T, testDB *sql.DB, waveID int, globalSessionID int) {
	t.Helper()
	if _, err := testDB.Exec(
		"UPDATE worker_process SET status = ?, stopped_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP WHERE parallel_wave_id = ? AND session_id = ?",
		workerStatusExited,
		waveID,
		globalSessionID,
	); err != nil {
		t.Fatalf("mark fake worker process exited: %v", err)
	}
}

func seedWaveSessionExternalLink(t *testing.T, testDB *sql.DB, localSessionID int) {
	t.Helper()
	globalSessionID, err := globalSessionIDFromProjectScoped(localSessionID, 1)
	if err != nil {
		t.Fatalf("map wave session %d: %v", localSessionID, err)
	}
	if _, err := testDB.Exec("DELETE FROM session_external_link WHERE session_id = ? AND backend = 'codex'", globalSessionID); err != nil {
		t.Fatalf("clear session %d external link: %v", localSessionID, err)
	}
	if _, err := testDB.Exec(
		"INSERT INTO session_external_link (session_id, backend, external_session_id) VALUES (?, 'codex', ?)",
		globalSessionID,
		"external-wave-session-"+strconv.Itoa(localSessionID),
	); err != nil {
		t.Fatalf("seed session %d external link: %v", localSessionID, err)
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func TestBuildNetrunnerWaveOperatorSummaryStatePrecedence(t *testing.T) {
	reviewReadyWorkers := []NetrunnerWaveWorkerSnapshot{
		{Id: 1, Status: parallelWaveWorkerStatusReviewReady},
		{Id: 2, Status: parallelWaveWorkerStatusReviewReady},
		{Id: 3, Status: parallelWaveWorkerStatusReviewReady},
	}
	repairWorkers := []NetrunnerWaveWorkerSnapshot{
		{Id: 1, Status: parallelWaveWorkerStatusReviewReady},
		{Id: 2, Status: parallelWaveWorkerStatusReviewReady},
		{Id: 3, Status: parallelWaveWorkerStatusFailed},
	}
	tests := []struct {
		name            string
		wave            NetrunnerWaveSnapshot
		wantState       string
		wantAllTerminal bool
		wantReviewReady bool
		wantRepair      bool
		wantAcceptance  bool
		wantCompleted   bool
	}{
		{
			name:      "wave 289 review ready with all workers terminal",
			wave:      NetrunnerWaveSnapshot{Id: 289, Status: parallelWaveStatusReviewReady, Phase: parallelWavePhaseImplementation, GateState: parallelWaveGateImplementationReview, ControlState: parallelWaveControlActive, FailurePolicyState: parallelWaveFailurePolicyPassed, Workers: reviewReadyWorkers},
			wantState: "wave_review_ready", wantAllTerminal: true, wantReviewReady: true,
		},
		{
			name:      "wave 293 repair required overrides legacy review ready",
			wave:      NetrunnerWaveSnapshot{Id: 293, Status: parallelWaveStatusReviewReady, Phase: parallelWavePhaseImplementation, GateState: parallelWaveGateImplementationRepair, ControlState: parallelWaveControlActive, FailurePolicyState: parallelWaveFailurePolicyRepairRequired, Workers: repairWorkers},
			wantState: "repair_blocked", wantAllTerminal: true, wantRepair: true,
		},
		{
			name:      "architect pause has highest precedence",
			wave:      NetrunnerWaveSnapshot{Id: 294, Status: parallelWaveStatusReviewReady, Phase: parallelWavePhaseImplementation, GateState: parallelWaveGateImplementationRepair, ControlState: parallelWaveControlPausedForArchitect, FailurePolicyState: parallelWaveFailurePolicyPaused, Workers: repairWorkers},
			wantState: "architect_paused", wantAllTerminal: true, wantRepair: true,
		},
		{
			name:      "legacy completed is not true completion",
			wave:      NetrunnerWaveSnapshot{Id: 295, Status: parallelWaveStatusCompleted, Phase: parallelWavePhaseImplementation, GateState: parallelWaveGateNone, ControlState: parallelWaveControlActive, FailurePolicyState: parallelWaveFailurePolicyPassed, Workers: reviewReadyWorkers},
			wantState: "worker_terminal", wantAllTerminal: true,
		},
		{
			name:      "acceptance readiness remains distinct",
			wave:      NetrunnerWaveSnapshot{Id: 296, Status: parallelWaveStatusReviewReady, Phase: parallelWavePhaseAcceptance, GateState: parallelWaveGateAcceptanceReview, ControlState: parallelWaveControlActive, FailurePolicyState: parallelWaveFailurePolicyPassed, AcceptanceSessionId: 9, AcceptanceSessionStatus: "completed", Workers: reviewReadyWorkers},
			wantState: "acceptance", wantAllTerminal: true, wantAcceptance: true,
		},
		{
			name:      "true completion requires completed phase and closed gate",
			wave:      NetrunnerWaveSnapshot{Id: 297, Status: parallelWaveStatusCompleted, Phase: parallelWavePhaseCompleted, GateState: parallelWaveGateClosed, ControlState: parallelWaveControlActive, FailurePolicyState: parallelWaveFailurePolicyPassed, Workers: reviewReadyWorkers},
			wantState: "completed", wantAllTerminal: true, wantCompleted: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			summary := buildNetrunnerWaveOperatorSummary(test.wave)
			if summary.OperatorState != test.wantState || summary.AllWorkersTerminal != test.wantAllTerminal || summary.WaveReviewReady != test.wantReviewReady || summary.RepairRequired != test.wantRepair || summary.AcceptanceReady != test.wantAcceptance || summary.WaveCompleted != test.wantCompleted {
				t.Fatalf("unexpected operator summary: %+v", summary)
			}
			if summary.WorkerCounts.Total != len(test.wave.Workers) || summary.WorkerCounts.Terminal+summary.WorkerCounts.Active != summary.WorkerCounts.Total {
				t.Fatalf("incoherent worker counts: %+v", summary.WorkerCounts)
			}
		})
	}
}

func argumentAfter(values []string, target string) string {
	for index, value := range values {
		if value == target && index+1 < len(values) {
			return values[index+1]
		}
	}
	return ""
}

func TestWaitNetrunnerWaveMergesCompletedParentBranchIntoChildWorktree(t *testing.T) {
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
	defer func() { _ = testDB.Close() }()
	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1

	_, created, err := CreateNetrunnerWave(context.Background(), nil, CreateNetrunnerWaveInput{
		SessionIds:   []int{1, 2},
		Dependencies: []WaveDependency{{Child: 2, Parents: []int64{1}}},
		BaseRef:      "HEAD",
	})
	if err != nil {
		t.Fatalf("create dependency wave: %v", err)
	}
	seedWaveSessionExternalLink(t, testDB, 1)
	seedWaveSessionExternalLink(t, testDB, 2)

	var launchedArgs [][]string
	installFakeWaveWorkerLauncher(t, "", &launchedArgs)
	if callResult, _, err := LaunchNetrunnerWave(context.Background(), nil, LaunchNetrunnerWaveInput{
		WaveId:         created.WaveId,
		TimeoutSeconds: 1,
	}); err != nil || callResult != nil {
		t.Fatalf("launch dependency root: result=%+v err=%v", callResult, err)
	}
	parent := testWaveWorkerBySession(t, created.Wave, 1)
	parentWorktreePath, err := resolveParallelWaveWorktreePath(repoDir, parent.WorktreePath)
	if err != nil {
		t.Fatalf("resolve parent worktree: %v", err)
	}
	generatedPath := filepath.Join(parentWorktreePath, "generated-by-parent.go")
	if err := os.WriteFile(generatedPath, []byte("package generated\n\nconst FromParent = true\n"), 0o644); err != nil {
		t.Fatalf("write parent output: %v", err)
	}
	runGitTestCommand(t, parentWorktreePath, "add", "generated-by-parent.go")
	runGitTestCommand(t, parentWorktreePath, "-c", "user.name=Fixer Test", "-c", "user.email=fixer@example.test", "commit", "-m", "parent output")

	globalParentID, err := globalSessionIDFromProjectScoped(1, 1)
	if err != nil {
		t.Fatalf("map parent session: %v", err)
	}
	if _, err := testDB.Exec("UPDATE parallel_wave_worker SET declared_write_scope = '[\".\"]' WHERE wave_id = ? AND session_id = ?", created.WaveId, globalParentID); err != nil {
		t.Fatalf("update worker write scope: %v", err)
	}
	if _, err := testDB.Exec("UPDATE session SET declared_write_scope = '[\".\"]' WHERE id = ?", globalParentID); err != nil {
		t.Fatalf("update parent write scope: %v", err)
	}
	if _, err := testDB.Exec("UPDATE session SET status = 'review', report = 'parent ready' WHERE id = ?", globalParentID); err != nil {
		t.Fatalf("mark parent review: %v", err)
	}
	markFakeWaveWorkerExited(t, testDB, created.WaveId, globalParentID)
	if _, err := testDB.Exec(
		"INSERT INTO doc_proposal (project_id, session_id, status, proposed_content) VALUES (1, ?, 'pending', 'parent doc')",
		globalParentID,
	); err != nil {
		t.Fatalf("seed parent doc proposal: %v", err)
	}

	callResult, out, err := WaitForNetrunnerWave(context.Background(), nil, WaitForNetrunnerWaveInput{WaveId: created.WaveId})
	if err != nil {
		t.Fatalf("wait dependency wave: %v", err)
	}
	if callResult != nil || len(launchedArgs) != 2 {
		t.Fatalf("expected parent and child launches, result=%+v launches=%+v", callResult, launchedArgs)
	}
	child := testWaveWorkerBySession(t, out.Result.Wave, 2)
	if child.Status != parallelWaveWorkerStatusRunning {
		t.Fatalf("expected child to be running after parent merge, got %+v", child)
	}
	childWorktreePath, err := resolveParallelWaveWorktreePath(repoDir, child.WorktreePath)
	if err != nil {
		t.Fatalf("resolve child worktree: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(childWorktreePath, "generated-by-parent.go"))
	if err != nil {
		t.Fatalf("expected parent output in child worktree: %v", err)
	}
	if string(content) != "package generated\n\nconst FromParent = true\n" {
		t.Fatalf("unexpected merged parent output: %q", content)
	}
}

func TestWaitNetrunnerWaveRejectsUncommittedParentHandoff(t *testing.T) {
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
		t.Fatalf("create dependency wave: %v", err)
	}
	seedWaveSessionExternalLink(t, testDB, 1)
	seedWaveSessionExternalLink(t, testDB, 2)
	installFakeWaveWorkerLauncher(t, "", nil)
	if _, _, err := LaunchNetrunnerWave(context.Background(), nil, LaunchNetrunnerWaveInput{WaveId: created.WaveId, TimeoutSeconds: 1}); err != nil {
		t.Fatalf("launch dependency root: %v", err)
	}
	parent := testWaveWorkerBySession(t, created.Wave, 1)
	parentPath, err := resolveParallelWaveWorktreePath(repoDir, parent.WorktreePath)
	if err != nil {
		t.Fatalf("resolve parent worktree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(parentPath, "uncommitted.txt"), []byte("not committed\n"), 0o644); err != nil {
		t.Fatalf("write uncommitted parent output: %v", err)
	}
	globalParentID, err := globalSessionIDFromProjectScoped(1, 1)
	if err != nil {
		t.Fatalf("map parent session: %v", err)
	}
	if _, err := testDB.Exec("UPDATE parallel_wave_worker SET declared_write_scope = '[\".\"]' WHERE wave_id = ? AND session_id = ?", created.WaveId, globalParentID); err != nil {
		t.Fatalf("update worker write scope: %v", err)
	}
	if _, err := testDB.Exec("UPDATE session SET declared_write_scope = '[\".\"]' WHERE id = ?", globalParentID); err != nil {
		t.Fatalf("update parent write scope: %v", err)
	}
	if _, err := testDB.Exec("UPDATE session SET status = 'review', report = 'parent ready' WHERE id = ?", globalParentID); err != nil {
		t.Fatalf("mark parent review: %v", err)
	}
	markFakeWaveWorkerExited(t, testDB, created.WaveId, globalParentID)
	if _, err := testDB.Exec("INSERT INTO doc_proposal (project_id, session_id, status, proposed_content) VALUES (1, ?, 'pending', 'parent docs')", globalParentID); err != nil {
		t.Fatalf("seed parent proposal: %v", err)
	}
	_, output, err := WaitForNetrunnerWave(context.Background(), nil, WaitForNetrunnerWaveInput{WaveId: created.WaveId})
	if err != nil {
		t.Fatalf("wait dependency wave: %v", err)
	}
	parentWorker := testWaveWorkerBySession(t, output.Result.Wave, 1)
	if parentWorker.Status != parallelWaveWorkerStatusReviewReady || !strings.Contains(parentWorker.FailureReason, "uncommitted") {
		t.Fatalf("expected uncommitted parent handoff to remain review-ready with blocker, got %+v", parentWorker)
	}
	if output.Status != "blocked" {
		t.Fatalf("deferred child failure must block follow-up in the same wait iteration: %+v", output.Result)
	}
	var reviewerCount int
	if err := testDB.QueryRow("SELECT COUNT(*) FROM session WHERE parallel_wave_id = ?", parallelWaveReviewMarker(created.WaveId)).Scan(&reviewerCount); err != nil {
		t.Fatalf("count post-wave reviewers: %v", err)
	}
	if reviewerCount != 0 {
		t.Fatalf("deferred child failure launched %d reviewer session(s)", reviewerCount)
	}
}

func TestLaunchNetrunnerWaveHappyPathCreatesWorktreesAndWorkerLinks(t *testing.T) {
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
	created := createLaunchableTestWave(t, testDB)

	var launchedArgs [][]string
	installFakeWaveWorkerLauncher(t, "", &launchedArgs)
	callResult, launched, err := LaunchNetrunnerWave(context.Background(), nil, LaunchNetrunnerWaveInput{
		WaveId:    created.WaveId,
		Backend:   "codex",
		Model:     "gpt-5.6-luna",
		Reasoning: "high",
		WorkerConfigs: []WaveWorkerLaunchConfig{
			{SessionId: 2, Model: "gpt-5.6-sol", Reasoning: "high"},
		},
		FixerSessionId: "fixer-session-123",
		TimeoutSeconds: 1,
	})
	if err != nil {
		t.Fatalf("launch_netrunner_wave failed: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil launch call result on success, got %+v", callResult)
	}
	if launched.Status != "success" || launched.Wave.Status != parallelWaveStatusRunning || len(launched.Workers) != 2 {
		t.Fatalf("unexpected launch output: %+v", launched)
	}
	if len(launchedArgs) != 2 {
		t.Fatalf("expected two launcher calls, got %d: %+v", len(launchedArgs), launchedArgs)
	}
	for _, worker := range launched.Workers {
		if worker.Status != parallelWaveWorkerStatusRunning {
			t.Fatalf("expected running worker, got %+v", worker)
		}
		if worker.WorkerProcessId <= 0 || worker.LaunchEpoch != launched.OrchestrationEpoch {
			t.Fatalf("expected process linkage and launch epoch, got %+v", worker)
		}
		if worker.HeadlessLogPath == "" || worker.LauncherLogPath == "" || worker.WorkerMetadataPath == "" {
			t.Fatalf("expected persisted log metadata, got %+v", worker)
		}
		absWorktreePath, err := resolveParallelWaveWorktreePath(repoDir, worker.WorktreePath)
		if err != nil {
			t.Fatalf("resolve worktree path: %v", err)
		}
		if info, err := os.Stat(absWorktreePath); err != nil || !info.IsDir() {
			t.Fatalf("expected worktree directory %s, stat=%v info=%+v", absWorktreePath, err, info)
		}
	}

	var linkedCount int
	if err := testDB.QueryRow(
		`SELECT COUNT(*)
		 FROM worker_process
		 WHERE parallel_wave_id = ?
		   AND parallel_wave_worker_id IS NOT NULL
		   AND status = ?`,
		created.WaveId,
		workerStatusRunning,
	).Scan(&linkedCount); err != nil {
		t.Fatalf("query wave worker process links: %v", err)
	}
	if linkedCount != 2 {
		t.Fatalf("expected two linked worker processes, got %d", linkedCount)
	}
	if len(launchedArgs[0]) < 2 || launchedArgs[0][1] != "launch-wave-worker" {
		t.Fatalf("unexpected launcher args: %+v", launchedArgs[0])
	}
	if !containsString(launchedArgs[0], "--project-cwd") || !containsString(launchedArgs[0], "--worker-cwd") || !containsString(launchedArgs[0], "--fixer-session-id") {
		t.Fatalf("expected wave launcher args to include project/worker/fixer context: %+v", launchedArgs[0])
	}
	launchArgsBySession := map[string][]string{}
	for _, args := range launchedArgs {
		launchArgsBySession[argumentAfter(args, "--session-id")] = args
	}
	if got := argumentAfter(launchArgsBySession["1"], "--model"); got != "gpt-5.6-luna" {
		t.Fatalf("expected session 1 to inherit luna, got %q in %+v", got, launchArgsBySession["1"])
	}
	if got := argumentAfter(launchArgsBySession["2"], "--model"); got != "gpt-5.6-sol" {
		t.Fatalf("expected session 2 to use sol override, got %q in %+v", got, launchArgsBySession["2"])
	}
	for _, localSessionID := range []int{1, 2} {
		globalSessionID, err := globalSessionIDFromProjectScoped(localSessionID, 1)
		if err != nil {
			t.Fatalf("map session %d: %v", localSessionID, err)
		}
		var backend, model, reasoning string
		if err := testDB.QueryRow(
			"SELECT cli_backend, cli_model, cli_reasoning FROM session WHERE id = ?",
			globalSessionID,
		).Scan(&backend, &model, &reasoning); err != nil {
			t.Fatalf("read session %d launch config: %v", localSessionID, err)
		}
		expectedModel := "gpt-5.6-luna"
		if localSessionID == 2 {
			expectedModel = "gpt-5.6-sol"
		}
		if backend != "codex" || model != expectedModel || reasoning != "high" {
			t.Fatalf("unexpected persisted config for session %d: %s/%s/%s", localSessionID, backend, model, reasoning)
		}
	}
}

func TestGovernedRepairWorktreeValidationAndQuarantine(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
	}()

	repoDir, testDB, _, wave := setupRunningWaveTest(t)
	defer func() {
		_ = testDB.Close()
	}()

	worker := testWaveWorkerBySession(t, wave, 1)
	absWorktreePath, err := resolveParallelWaveWorktreePath(repoDir, worker.WorktreePath)
	if err != nil {
		t.Fatalf("resolve worktree: %v", err)
	}

	runGitTestCommand(t, repoDir, "worktree", "remove", "--force", absWorktreePath)
	if err := os.MkdirAll(absWorktreePath, 0o755); err != nil {
		t.Fatalf("create broken worktree dir: %v", err)
	}
	valuableContent := "valuable plan content line 1\nvaluable plan content line 2\n"
	valuableFilePath := filepath.Join(absWorktreePath, "valuable_plan.md")
	if err := os.WriteFile(valuableFilePath, []byte(valuableContent), 0o644); err != nil {
		t.Fatalf("write valuable plan: %v", err)
	}

	if _, err := testDB.Exec(
		`UPDATE parallel_wave SET failure_policy_state = ?, repair_worker_id = ?, repair_attempt_count = 0 WHERE id = ?`,
		parallelWaveFailurePolicyRepairRequired,
		worker.Id,
		wave.Id,
	); err != nil {
		t.Fatalf("setup repair state: %v", err)
	}
	if _, err := testDB.Exec(
		`UPDATE parallel_wave_worker SET status = 'failed' WHERE id = ?`,
		worker.Id,
	); err != nil {
		t.Fatalf("setup worker failed state: %v", err)
	}

	_, _, err = AuthorizeNetrunnerWaveRepair(context.Background(), nil, AuthorizeNetrunnerWaveRepairInput{
		WaveId:          wave.Id,
		WorkerSessionId: worker.SessionId,
	})
	if err != nil {
		t.Fatalf("authorize repair: %v", err)
	}

	refreshedWave, err := fetchNetrunnerWaveSnapshot(wave.Id, 1)
	if err != nil {
		t.Fatalf("fetch refreshed wave: %v", err)
	}

	repairedPath, err := ensureGovernedRepairWorktreeReady(repoDir, refreshedWave, refreshedWave.Workers[0])
	if err != nil {
		t.Fatalf("ensureGovernedRepairWorktreeReady failed: %v", err)
	}

	if err := validateWorktreeIsolation(repoDir, repairedPath); err != nil {
		t.Fatalf("worktree isolation validation failed: %v", err)
	}

	parentDir := filepath.Dir(absWorktreePath)
	entries, err := os.ReadDir(parentDir)
	if err != nil {
		t.Fatalf("read parent dir: %v", err)
	}
	foundQuarantine := false
	for _, entry := range entries {
		if strings.Contains(entry.Name(), "quarantine") {
			quarantineFile := filepath.Join(parentDir, entry.Name(), "valuable_plan.md")
			data, err := os.ReadFile(quarantineFile)
			if err == nil && string(data) == valuableContent {
				foundQuarantine = true
				break
			}
		}
	}
	if !foundQuarantine {
		t.Fatalf("expected valuable plan file to survive in quarantine directory, but was not found")
	}
}

func TestGovernedRepairRecreationParentHandoffAndBranchIntegration(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
	}()

	repoDir := setupCleanGitRepo(t)
	testDB := setupParallelWaveTestDB(t, repoDir)
	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1

	callResult, created, err := CreateNetrunnerWave(context.Background(), nil, CreateNetrunnerWaveInput{
		SessionIds:   []int{1, 2},
		Dependencies: []WaveDependency{{Child: 2, Parents: []int64{1}}},
		BaseRef:      "HEAD",
	})
	if err != nil || callResult != nil {
		t.Fatalf("create wave with dependencies: %v", err)
	}

	parentWorker := created.Workers[0]
	childWorker := created.Workers[1]

	parentWorktreePath, err := resolveParallelWaveWorktreePath(repoDir, parentWorker.WorktreePath)
	if err != nil {
		t.Fatalf("resolve parent worktree: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(parentWorktreePath), 0o755); err != nil {
		t.Fatalf("prepare parent worktree dir: %v", err)
	}
	runGitTestCommand(t, repoDir, "worktree", "add", "-b", parentWorker.BranchName, parentWorktreePath, created.BaseSha)
	if err := os.MkdirAll(filepath.Join(parentWorktreePath, "docs/a"), 0o755); err != nil {
		t.Fatalf("prepare docs/a dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(parentWorktreePath, "docs/a/parent_change.txt"), []byte("parent handoff\n"), 0o644); err != nil {
		t.Fatalf("write parent file: %v", err)
	}
	runGitTestCommand(t, parentWorktreePath, "add", ".")
	runGitTestCommand(t, parentWorktreePath, "-c", "user.name=Fixer Test", "-c", "user.email=fixer@example.test", "commit", "-m", "parent commit")
	parentHeadSHA, err := gitCommandInWorktree(parentWorktreePath, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		t.Fatalf("get parent head sha: %v", err)
	}

	if _, err := testDB.Exec(
		`UPDATE parallel_wave_worker SET status = 'completed', head_sha = ? WHERE id = ?`,
		parentHeadSHA,
		parentWorker.Id,
	); err != nil {
		t.Fatalf("update parent worker state: %v", err)
	}

	runGitTestCommand(t, repoDir, "branch", childWorker.BranchName, created.BaseSha)

	refreshedWave, err := fetchNetrunnerWaveSnapshot(created.WaveId, 1)
	if err != nil {
		t.Fatalf("fetch refreshed wave: %v", err)
	}
	refreshedChildWorker := testWaveWorkerBySession(t, refreshedWave, 2)

	childWorktreePath, err := ensureGovernedRepairWorktreeReady(repoDir, refreshedWave, refreshedChildWorker)
	if err != nil {
		t.Fatalf("ensureGovernedRepairWorktreeReady for child worker: %v", err)
	}

	if _, err := os.Stat(filepath.Join(childWorktreePath, "docs/a/parent_change.txt")); err != nil {
		t.Fatalf("expected parent committed handoff to be merged into child worktree: %v", err)
	}
}

func TestGovernedRepairUncommittedOrContaminatedPatchRemainsReviewReady(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
	}()

	repoDir, testDB, _, wave := setupRunningWaveTest(t)
	defer func() {
		_ = testDB.Close()
	}()

	worker := testWaveWorkerBySession(t, wave, 1)
	absWorktreePath, err := resolveParallelWaveWorktreePath(repoDir, worker.WorktreePath)
	if err != nil {
		t.Fatalf("resolve worktree: %v", err)
	}

	if err := os.WriteFile(filepath.Join(absWorktreePath, "OUT_OF_SCOPE.txt"), []byte("contaminated\n"), 0o644); err != nil {
		t.Fatalf("write out of scope file: %v", err)
	}

	finalized, err := finalizeParallelWaveWorker(repoDir, wave, worker, parallelWaveWorkerStatusReviewReady, "")
	if err != nil {
		t.Fatalf("finalizeParallelWaveWorker returned error: %v", err)
	}

	if finalized.Status != parallelWaveWorkerStatusReviewReady {
		t.Fatalf("expected worker status to remain review-ready with deliverable blockers, got %s", finalized.Status)
	}
	if !strings.Contains(finalized.FailureReason, "uncommitted") && !strings.Contains(finalized.FailureReason, "outside declared write scope") {
		t.Fatalf("unexpected failure reason: %s", finalized.FailureReason)
	}
}

func TestGovernedRepairAllowsReviewSubmissionWithoutNewCommit(t *testing.T) {
	originalDB, originalRole, originalProjectID := db, authorizedRole, authorizedProjectId
	defer func() { db, authorizedRole, authorizedProjectId = originalDB, originalRole, originalProjectID }()
	repoDir, testDB, _, wave := setupRunningWaveTest(t)
	defer testDB.Close()

	worker := testWaveWorkerBySession(t, wave, 1)
	if _, err := testDB.Exec(
		`UPDATE parallel_wave SET repair_attempt_count = 1 WHERE id = ?`,
		wave.Id,
	); err != nil {
		t.Fatalf("seed governed repair attempt: %v", err)
	}
	if _, err := testDB.Exec(
		`UPDATE parallel_wave_worker SET head_sha = base_sha WHERE id = ?`,
		worker.Id,
	); err != nil {
		t.Fatalf("seed previous repair head: %v", err)
	}

	finalized, err := finalizeParallelWaveWorker(repoDir, wave, worker, parallelWaveWorkerStatusReviewReady, "")
	if err != nil {
		t.Fatalf("finalize governed repair without new commit: %v", err)
	}
	if finalized.Status != parallelWaveWorkerStatusReviewReady {
		t.Fatalf("a clean repair re-submission must remain review-ready, got %+v", finalized)
	}
}

func TestSetSessionStatusRejectsWaveWorkerLifecycleBypass(t *testing.T) {
	originalDB, originalRole, originalProjectID := db, authorizedRole, authorizedProjectId
	defer func() { db, authorizedRole, authorizedProjectId = originalDB, originalRole, originalProjectID }()
	_, testDB, _, wave := setupRunningWaveTest(t)
	defer testDB.Close()

	callResult, _, err := SetSessionStatus(context.Background(), nil, SetSessionStatusInput{
		SessionId: wave.Workers[0].SessionId,
		Status:    "review",
	})
	if err == nil || callResult == nil || !callResult.IsError {
		t.Fatalf("expected wave worker lifecycle bypass to be rejected: result=%+v err=%v", callResult, err)
	}
	if !strings.Contains(err.Error(), "wave-linked session") {
		t.Fatalf("unexpected bypass rejection: %v", err)
	}
}

func TestParallelWaveProviderRetryClaimPersistsAndDoesNotDuplicate(t *testing.T) {
	originalDB, originalRole, originalProjectID, originalExecCommand := db, authorizedRole, authorizedProjectId, execCommand
	originalQuotaGate := DefaultQuotaGate
	defer func() {
		db, authorizedRole, authorizedProjectId, execCommand = originalDB, originalRole, originalProjectID, originalExecCommand
		DefaultQuotaGate = originalQuotaGate
	}()
	// This test verifies durable retry claiming. Live account quotas belong to
	// the separately tested quota gate and must not make it time-dependent.
	DefaultQuotaGate = nil
	repoDir := setupCleanGitRepo(t)
	testDB := setupParallelWaveTestDB(t, repoDir)
	defer testDB.Close()
	db, authorizedRole, authorizedProjectId = testDB, "fixer", 1
	created := createLaunchableTestWave(t, testDB)
	installFakeWaveWorkerLauncher(t, "", nil)
	if _, _, err := LaunchNetrunnerWave(context.Background(), nil, LaunchNetrunnerWaveInput{WaveId: created.WaveId, TimeoutSeconds: 1}); err != nil {
		t.Fatalf("launch retry test wave: %v", err)
	}
	wave, err := fetchNetrunnerWaveSnapshot(created.WaveId, 1)
	if err != nil {
		t.Fatalf("fetch launched retry wave: %v", err)
	}
	worker := wave.Workers[0]
	if _, err := testDB.Exec(
		"UPDATE parallel_wave_worker SET status = ?, retry_cause = 'provider_rate_limit', retry_next_eligible_at = ? WHERE id = ?",
		parallelWaveWorkerStatusRetryWait,
		time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano),
		worker.Id,
	); err != nil {
		t.Fatalf("seed eligible retry: %v", err)
	}
	markFakeWaveWorkerExited(t, testDB, created.WaveId, worker.SessionId)
	wave, err = fetchNetrunnerWaveSnapshot(created.WaveId, 1)
	if err != nil {
		t.Fatalf("fetch retry-wait wave: %v", err)
	}
	if err := processParallelWaveWorkerRetries(context.Background(), repoDir, wave, time.Second); err != nil {
		t.Fatalf("process persisted provider retry: %v", err)
	}
	retried, err := fetchNetrunnerWaveSnapshot(created.WaveId, 1)
	if err != nil {
		t.Fatalf("fetch retried wave: %v", err)
	}
	if retried.Workers[0].RetryAttemptCount != 1 || retried.Workers[0].Status != parallelWaveWorkerStatusRunning {
		t.Fatalf("retry claim was not durably consumed: %+v", retried.Workers[0])
	}
	if err := processParallelWaveWorkerRetries(context.Background(), repoDir, retried, time.Second); err != nil {
		t.Fatalf("repeat retry reconciliation: %v", err)
	}
	var processCount int
	if err := testDB.QueryRow("SELECT COUNT(*) FROM worker_process WHERE parallel_wave_worker_id = ?", worker.Id).Scan(&processCount); err != nil {
		t.Fatalf("count retry worker processes: %v", err)
	}
	if processCount != 2 {
		t.Fatalf("expected initial launch plus exactly one retry process, got %d", processCount)
	}
}

func TestParallelWaveWorkerLaunchInputsRejectsUnknownAndDuplicateSessions(t *testing.T) {
	wave := NetrunnerWaveSnapshot{
		Id: 9,
		Workers: []NetrunnerWaveWorkerSnapshot{
			{SessionId: 1},
			{SessionId: 2},
		},
	}

	if _, err := parallelWaveWorkerLaunchInputs(wave, LaunchNetrunnerWaveInput{
		WorkerConfigs: []WaveWorkerLaunchConfig{{SessionId: 3, Model: "gpt-5.6-sol"}},
	}); err == nil || !strings.Contains(err.Error(), "not part of wave 9") {
		t.Fatalf("expected unknown-session rejection, got %v", err)
	}
	if _, err := parallelWaveWorkerLaunchInputs(wave, LaunchNetrunnerWaveInput{
		WorkerConfigs: []WaveWorkerLaunchConfig{
			{SessionId: 1, Model: "gpt-5.6-luna"},
			{SessionId: 1, Model: "gpt-5.6-sol"},
		},
	}); err == nil || !strings.Contains(err.Error(), "duplicate session 1") {
		t.Fatalf("expected duplicate-session rejection, got %v", err)
	}
}

func TestLaunchNetrunnerWaveOnlyLaunchesWorkersWithoutParents(t *testing.T) {
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
	defer func() { _ = testDB.Close() }()
	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1

	_, created, err := CreateNetrunnerWave(context.Background(), nil, CreateNetrunnerWaveInput{
		SessionIds:   []int{1, 2},
		Dependencies: []WaveDependency{{Child: 2, Parents: []int64{1}}},
		BaseRef:      "HEAD",
	})
	if err != nil {
		t.Fatalf("create dependency wave failed: %v", err)
	}

	var launchedArgs [][]string
	installFakeWaveWorkerLauncher(t, "", &launchedArgs)
	callResult, launched, err := LaunchNetrunnerWave(context.Background(), nil, LaunchNetrunnerWaveInput{
		WaveId:         created.WaveId,
		TimeoutSeconds: 1,
	})
	if err != nil {
		t.Fatalf("launch dependency wave failed: %v", err)
	}
	if callResult != nil || launched.Status != "success" {
		t.Fatalf("unexpected launch result: result=%+v output=%+v", callResult, launched)
	}
	if len(launchedArgs) != 1 {
		t.Fatalf("expected only parent session to launch, got %+v", launchedArgs)
	}
	launchedSessionID := ""
	for index, arg := range launchedArgs[0] {
		if arg == "--session-id" && index+1 < len(launchedArgs[0]) {
			launchedSessionID = launchedArgs[0][index+1]
			break
		}
	}
	if launchedSessionID != "1" {
		t.Fatalf("expected only parent session to launch, got %+v", launchedArgs)
	}
	if testWaveWorkerBySession(t, launched.Wave, 1).Status != parallelWaveWorkerStatusRunning {
		t.Fatalf("expected parent worker running, got %+v", launched.Wave.Workers)
	}
	childWorker := testWaveWorkerBySession(t, launched.Wave, 2)
	if childWorker.Status != parallelWaveWorkerStatusCreated {
		t.Fatalf("expected child worker to remain created, got %+v", childWorker)
	}
	childWorktreePath, err := resolveParallelWaveWorktreePath(repoDir, childWorker.WorktreePath)
	if err != nil {
		t.Fatalf("resolve child worktree path: %v", err)
	}
	if _, err := os.Stat(childWorktreePath); !os.IsNotExist(err) {
		t.Fatalf("expected child worktree not to be created, stat error=%v", err)
	}
}

func TestWaitNetrunnerWaveLaunchesDeferredWorkerAfterResolvedParent(t *testing.T) {
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
	defer func() { _ = testDB.Close() }()
	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1

	_, created, err := CreateNetrunnerWave(context.Background(), nil, CreateNetrunnerWaveInput{
		SessionIds:   []int{1, 2},
		Dependencies: []WaveDependency{{Child: 2, Parents: []int64{1}}},
		BaseRef:      "HEAD",
	})
	if err != nil {
		t.Fatalf("create dependency wave: %v", err)
	}
	seedWaveSessionExternalLink(t, testDB, 1)
	seedWaveSessionExternalLink(t, testDB, 2)

	var launchedArgs [][]string
	installFakeWaveWorkerLauncher(t, "", &launchedArgs)
	if callResult, _, err := LaunchNetrunnerWave(context.Background(), nil, LaunchNetrunnerWaveInput{
		WaveId:         created.WaveId,
		TimeoutSeconds: 1,
	}); err != nil || callResult != nil {
		t.Fatalf("launch dependency root failed: result=%+v err=%v", callResult, err)
	}
	if len(launchedArgs) != 1 {
		t.Fatalf("expected only parent launch before wait, got %+v", launchedArgs)
	}

	globalParentID, err := globalSessionIDFromProjectScoped(1, 1)
	if err != nil {
		t.Fatalf("map parent session: %v", err)
	}
	if _, err := testDB.Exec("UPDATE session SET status = 'review', report = 'parent ready' WHERE id = ?", globalParentID); err != nil {
		t.Fatalf("mark parent review: %v", err)
	}
	markFakeWaveWorkerExited(t, testDB, created.WaveId, globalParentID)
	if _, err := testDB.Exec(
		"INSERT INTO doc_proposal (project_id, session_id, status, proposed_content) VALUES (1, ?, 'pending', 'parent doc')",
		globalParentID,
	); err != nil {
		t.Fatalf("seed parent doc proposal: %v", err)
	}

	callResult, out, err := WaitForNetrunnerWave(context.Background(), nil, WaitForNetrunnerWaveInput{
		WaveId: created.WaveId,
	})
	if err != nil {
		t.Fatalf("wait dependency wave: %v", err)
	}
	if callResult != nil || out.Status != "success" || out.Result.WinningSessionId != 1 {
		t.Fatalf("unexpected deferred launch output: result=%+v out=%+v", callResult, out)
	}
	if len(launchedArgs) != 2 {
		t.Fatalf("expected child launch before terminal return, got %+v", launchedArgs)
	}
	child := testWaveWorkerBySession(t, out.Result.Wave, 2)
	if child.Status != parallelWaveWorkerStatusRunning {
		t.Fatalf("expected deferred child to be running, got %+v", child)
	}
	childWorktreePath, err := resolveParallelWaveWorktreePath(repoDir, child.WorktreePath)
	if err != nil {
		t.Fatalf("resolve child worktree: %v", err)
	}
	if info, err := os.Stat(childWorktreePath); err != nil || !info.IsDir() {
		t.Fatalf("expected deferred child worktree %s, stat=%v info=%+v", childWorktreePath, err, info)
	}
}

func TestWaitNetrunnerWaveRequiresAllParentsBeforeDeferredLaunch(t *testing.T) {
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
	defer func() { _ = testDB.Close() }()
	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1
	if _, err := testDB.Exec("INSERT INTO session (project_id, task_description, status, declared_write_scope) VALUES (1, 'Task D', 'pending', '[\"docs/d\"]')"); err != nil {
		t.Fatalf("seed second dependency child session: %v", err)
	}

	_, created, err := CreateNetrunnerWave(context.Background(), nil, CreateNetrunnerWaveInput{
		SessionIds: []int{1, 2, 3},
		Dependencies: []WaveDependency{{
			Child:   3,
			Parents: []int64{1, 2},
		}},
		BaseRef: "HEAD",
	})
	if err != nil {
		t.Fatalf("create multi-parent wave: %v", err)
	}
	for _, localSessionID := range []int{1, 2, 3} {
		seedWaveSessionExternalLink(t, testDB, localSessionID)
	}

	var launchedArgs [][]string
	installFakeWaveWorkerLauncher(t, "", &launchedArgs)
	if callResult, _, err := LaunchNetrunnerWave(context.Background(), nil, LaunchNetrunnerWaveInput{
		WaveId:         created.WaveId,
		TimeoutSeconds: 1,
	}); err != nil || callResult != nil {
		t.Fatalf("launch multi-parent roots failed: result=%+v err=%v", callResult, err)
	}
	if len(launchedArgs) != 2 {
		t.Fatalf("expected two dependency roots, got %+v", launchedArgs)
	}
	wave, err := fetchNetrunnerWaveSnapshot(created.WaveId, 1)
	if err != nil {
		t.Fatalf("fetch multi-parent wave: %v", err)
	}
	parentOne := testWaveWorkerBySession(t, wave, 1)
	parentTwo := testWaveWorkerBySession(t, wave, 2)
	parentOnePath, err := resolveParallelWaveWorktreePath(repoDir, parentOne.WorktreePath)
	if err != nil {
		t.Fatalf("resolve first parent worktree: %v", err)
	}
	parentTwoPath, err := resolveParallelWaveWorktreePath(repoDir, parentTwo.WorktreePath)
	if err != nil {
		t.Fatalf("resolve second parent worktree: %v", err)
	}
	runGitTestCommand(t, parentOnePath, "-c", "user.name=Fixer Test", "-c", "user.email=fixer@example.test", "commit", "--allow-empty", "-m", "first parent handoff")
	runGitTestCommand(t, parentTwoPath, "-c", "user.name=Fixer Test", "-c", "user.email=fixer@example.test", "commit", "--allow-empty", "-m", "second parent handoff")
	parentOneHead := runGitTestCommand(t, parentOnePath, "rev-parse", "HEAD^{commit}")
	parentTwoHead := runGitTestCommand(t, parentTwoPath, "rev-parse", "HEAD^{commit}")
	if _, err := testDB.Exec("UPDATE parallel_wave_worker SET status = ?, head_sha = ?, terminal_at = CURRENT_TIMESTAMP WHERE id = ?", parallelWaveWorkerStatusReviewReady, parentOneHead, parentOne.Id); err != nil {
		t.Fatalf("mark first parent resolved: %v", err)
	}
	if _, err := testDB.Exec("UPDATE parallel_wave_worker SET status = ?, head_sha = ?, terminal_at = CURRENT_TIMESTAMP WHERE id = ?", parallelWaveWorkerStatusCompleted, parentTwoHead, parentTwo.Id); err != nil {
		t.Fatalf("mark second parent resolved: %v", err)
	}
	markFakeWaveWorkerExited(t, testDB, created.WaveId, parentOne.SessionId)
	markFakeWaveWorkerExited(t, testDB, created.WaveId, parentTwo.SessionId)

	callResult, out, err := WaitForNetrunnerWave(context.Background(), nil, WaitForNetrunnerWaveInput{WaveId: created.WaveId})
	if err != nil {
		t.Fatalf("wait multi-parent wave: %v", err)
	}
	if callResult != nil || out.Status != "success" {
		t.Fatalf("unexpected multi-parent output: result=%+v out=%+v", callResult, out)
	}
	var reviewerCount int
	if err := testDB.QueryRow(
		"SELECT COUNT(*) FROM session WHERE project_id = 1 AND parallel_wave_id = ?",
		parallelWaveReviewMarker(created.WaveId),
	).Scan(&reviewerCount); err != nil {
		t.Fatalf("count reviewer sessions: %v", err)
	}
	if reviewerCount != 0 {
		t.Fatalf("manual review policy created %d reviewer sessions", reviewerCount)
	}
	if len(launchedArgs) != 3 {
		t.Fatalf("expected child launch only after both parents resolved, got %+v", launchedArgs)
	}
	child := testWaveWorkerBySession(t, out.Result.Wave, 3)
	if child.Status != parallelWaveWorkerStatusRunning {
		t.Fatalf("expected multi-parent child to be running, got %+v", child)
	}
}

func TestWaitNetrunnerWaveRequiresGovernedRepairWhenSingleInitialWorkerFails(t *testing.T) {
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
	defer func() { _ = testDB.Close() }()
	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1

	_, created, err := CreateNetrunnerWave(context.Background(), nil, CreateNetrunnerWaveInput{
		SessionIds:   []int{1, 2},
		Dependencies: []WaveDependency{{Child: 2, Parents: []int64{1}}},
		BaseRef:      "HEAD",
	})
	if err != nil {
		t.Fatalf("create failed-parent wave: %v", err)
	}
	seedWaveSessionExternalLink(t, testDB, 1)
	installFakeWaveWorkerLauncher(t, "", nil)
	if callResult, _, err := LaunchNetrunnerWave(context.Background(), nil, LaunchNetrunnerWaveInput{
		WaveId:         created.WaveId,
		TimeoutSeconds: 1,
	}); err != nil || callResult != nil {
		t.Fatalf("launch failed-parent root: result=%+v err=%v", callResult, err)
	}
	wave, err := fetchNetrunnerWaveSnapshot(created.WaveId, 1)
	if err != nil {
		t.Fatalf("fetch failed-parent wave: %v", err)
	}
	parent := testWaveWorkerBySession(t, wave, 1)
	if _, err := testDB.Exec(
		"UPDATE parallel_wave_worker SET status = ?, failure_reason = ?, terminal_at = CURRENT_TIMESTAMP WHERE id = ?",
		parallelWaveWorkerStatusFailed,
		"root worker failed",
		parent.Id,
	); err != nil {
		t.Fatalf("mark parent failed: %v", err)
	}

	callResult, out, err := WaitForNetrunnerWave(context.Background(), nil, WaitForNetrunnerWaveInput{
		WaveId:     created.WaveId,
		ReturnWhen: parallelWaveWaitAllTerminal,
	})
	if err != nil {
		t.Fatalf("wait failed-parent wave: %v", err)
	}
	if callResult != nil || out.Status != "blocked" || out.Result.TerminalCondition != "follow_up_blocked" {
		t.Fatalf("unexpected failed-parent output: result=%+v out=%+v", callResult, out)
	}
	if out.Result.Wave.ControlState != parallelWaveControlActive || out.Result.Wave.FailurePolicyState != parallelWaveFailurePolicyRepairRequired || !strings.Contains(out.Result.FollowUpBlockedReason, "governed_implementation_repair_required") {
		t.Fatalf("expected one governed repair gate, got %+v", out.Result)
	}
	child := testWaveWorkerBySession(t, out.Result.Wave, 2)
	if child.Status != parallelWaveWorkerStatusCreated || child.FailureReason != "" {
		t.Fatalf("paused wave must not silently schedule or fail a second batch, got %+v", child)
	}
}

func TestLaunchNetrunnerWaveRejectsMissingNonCreatedFrozenStaleAndDirty(t *testing.T) {
	cases := []struct {
		name          string
		setup         func(t *testing.T, testDB *sql.DB, repoDir string, waveID int)
		waveID        func(created CreateNetrunnerWaveOutput) int
		errorFragment string
	}{
		{
			name:          "missing",
			waveID:        func(_ CreateNetrunnerWaveOutput) int { return 9999 },
			errorFragment: "not found",
		},
		{
			name: "non_created",
			setup: func(t *testing.T, testDB *sql.DB, _ string, waveID int) {
				t.Helper()
				if _, err := testDB.Exec("UPDATE parallel_wave SET status = ? WHERE id = ?", parallelWaveStatusRunning, waveID); err != nil {
					t.Fatalf("mark wave running: %v", err)
				}
			},
			errorFragment: "must be",
		},
		{
			name: "frozen",
			setup: func(t *testing.T, testDB *sql.DB, _ string, _ int) {
				t.Helper()
				if _, err := testDB.Exec(
					`INSERT INTO autonomous_run_status (project_id, state, summary, orchestration_epoch, orchestration_frozen)
					 VALUES (1, 'blocked', 'frozen test', 0, 1)`,
				); err != nil {
					t.Fatalf("freeze orchestration: %v", err)
				}
			},
			errorFragment: "orchestration is frozen",
		},
		{
			name: "stale_epoch",
			setup: func(t *testing.T, testDB *sql.DB, _ string, _ int) {
				t.Helper()
				if _, err := testDB.Exec(
					`INSERT INTO autonomous_run_status (project_id, state, summary, orchestration_epoch, orchestration_frozen)
					 VALUES (1, 'running', 'stale test', 1, 0)`,
				); err != nil {
					t.Fatalf("set stale epoch: %v", err)
				}
			},
			errorFragment: "stale orchestration epoch",
		},
		{
			name: "dirty",
			setup: func(t *testing.T, _ *sql.DB, repoDir string, _ int) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("dirty\n"), 0o644); err != nil {
					t.Fatalf("dirty README: %v", err)
				}
			},
			errorFragment: "tracked working tree must be clean",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			originalDB := db
			originalRole := authorizedRole
			originalProjectID := authorizedProjectId
			defer func() {
				db = originalDB
				authorizedRole = originalRole
				authorizedProjectId = originalProjectID
			}()

			repoDir := setupCleanGitRepo(t)
			testDB := setupParallelWaveTestDB(t, repoDir)
			defer func() {
				_ = testDB.Close()
			}()
			db = testDB
			authorizedRole = "fixer"
			authorizedProjectId = 1
			created := createLaunchableTestWave(t, testDB)
			if tc.setup != nil {
				tc.setup(t, testDB, repoDir, created.WaveId)
			}
			waveID := created.WaveId
			if tc.waveID != nil {
				waveID = tc.waveID(created)
			}

			callResult, _, err := LaunchNetrunnerWave(context.Background(), nil, LaunchNetrunnerWaveInput{WaveId: waveID})
			if err == nil {
				t.Fatalf("expected %s rejection", tc.name)
			}
			if callResult == nil || !callResult.IsError {
				t.Fatal("expected MCP error result")
			}
			if !strings.Contains(err.Error(), tc.errorFragment) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestLaunchNetrunnerWaveRejectsExistingBranchAndPathConflicts(t *testing.T) {
	cases := []struct {
		name          string
		setup         func(t *testing.T, repoDir string, created CreateNetrunnerWaveOutput)
		errorFragment string
	}{
		{
			name: "branch",
			setup: func(t *testing.T, repoDir string, created CreateNetrunnerWaveOutput) {
				t.Helper()
				runGitTestCommand(t, repoDir, "branch", created.Workers[0].BranchName)
			},
			errorFragment: "branch already exists",
		},
		{
			name: "path",
			setup: func(t *testing.T, repoDir string, created CreateNetrunnerWaveOutput) {
				t.Helper()
				path, err := resolveParallelWaveWorktreePath(repoDir, created.Workers[0].WorktreePath)
				if err != nil {
					t.Fatalf("resolve path: %v", err)
				}
				if err := os.MkdirAll(path, 0o755); err != nil {
					t.Fatalf("mkdir conflict path: %v", err)
				}
			},
			errorFragment: "worktree path already exists",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			originalDB := db
			originalRole := authorizedRole
			originalProjectID := authorizedProjectId
			defer func() {
				db = originalDB
				authorizedRole = originalRole
				authorizedProjectId = originalProjectID
			}()

			repoDir := setupCleanGitRepo(t)
			testDB := setupParallelWaveTestDB(t, repoDir)
			defer func() {
				_ = testDB.Close()
			}()
			db = testDB
			authorizedRole = "fixer"
			authorizedProjectId = 1
			created := createLaunchableTestWave(t, testDB)
			tc.setup(t, repoDir, created)

			callResult, _, err := LaunchNetrunnerWave(context.Background(), nil, LaunchNetrunnerWaveInput{WaveId: created.WaveId})
			if err == nil {
				t.Fatalf("expected %s conflict", tc.name)
			}
			if callResult == nil || !callResult.IsError {
				t.Fatal("expected MCP error result")
			}
			if !strings.Contains(err.Error(), tc.errorFragment) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestLaunchNetrunnerWavePartialFailurePreservesLaunchedWorkerState(t *testing.T) {
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
	created := createLaunchableTestWave(t, testDB)
	installFakeWaveWorkerLauncher(t, "2", nil)

	callResult, out, err := LaunchNetrunnerWave(context.Background(), nil, LaunchNetrunnerWaveInput{
		WaveId:         created.WaveId,
		TimeoutSeconds: 1,
	})
	if err == nil {
		t.Fatal("expected partial launch failure")
	}
	if callResult == nil || !callResult.IsError {
		t.Fatal("expected MCP error result")
	}
	if out.Status != parallelWaveStatusPartiallyFailed || !out.PartialFailure || !strings.Contains(out.PartialFailureError, "session 2") {
		t.Fatalf("unexpected partial failure output: %+v err=%v", out, err)
	}

	var waveStatus string
	if err := testDB.QueryRow("SELECT status FROM parallel_wave WHERE id = ?", created.WaveId).Scan(&waveStatus); err != nil {
		t.Fatalf("query wave status: %v", err)
	}
	if waveStatus != parallelWaveStatusPartiallyFailed {
		t.Fatalf("expected partial wave status, got %q", waveStatus)
	}
	var runningCount int
	if err := testDB.QueryRow(
		"SELECT COUNT(*) FROM parallel_wave_worker WHERE wave_id = ? AND status = ? AND worker_process_id IS NOT NULL",
		created.WaveId,
		parallelWaveWorkerStatusRunning,
	).Scan(&runningCount); err != nil {
		t.Fatalf("query running worker count: %v", err)
	}
	if runningCount != 1 {
		t.Fatalf("expected one launched worker preserved, got %d", runningCount)
	}
	var failedReason string
	if err := testDB.QueryRow(
		`SELECT failure_reason
		 FROM parallel_wave_worker
		 WHERE wave_id = ? AND status = ?`,
		created.WaveId,
		parallelWaveWorkerStatusFailed,
	).Scan(&failedReason); err != nil {
		t.Fatalf("query failed worker reason: %v", err)
	}
	if !strings.Contains(failedReason, "fake wave worker launch failure") && !strings.Contains(failedReason, "exit status 3") {
		t.Fatalf("expected exact failed worker reason, got %q", failedReason)
	}
}

func TestWaitNetrunnerWaveRejectsMissingNonLaunchedAndUnsupportedReturnWhen(t *testing.T) {
	cases := []struct {
		name          string
		input         func(CreateNetrunnerWaveOutput) WaitForNetrunnerWaveInput
		errorFragment string
	}{
		{
			name: "missing",
			input: func(_ CreateNetrunnerWaveOutput) WaitForNetrunnerWaveInput {
				return WaitForNetrunnerWaveInput{WaveId: 9999}
			},
			errorFragment: "not found",
		},
		{
			name: "non_launched",
			input: func(created CreateNetrunnerWaveOutput) WaitForNetrunnerWaveInput {
				return WaitForNetrunnerWaveInput{WaveId: created.WaveId}
			},
			errorFragment: "has not been launched",
		},
		{
			name: "unsupported_return_when",
			input: func(created CreateNetrunnerWaveOutput) WaitForNetrunnerWaveInput {
				return WaitForNetrunnerWaveInput{WaveId: created.WaveId, ReturnWhen: "after_lunch"}
			},
			errorFragment: "unsupported return_when",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			originalDB := db
			originalRole := authorizedRole
			originalProjectID := authorizedProjectId
			defer func() {
				db = originalDB
				authorizedRole = originalRole
				authorizedProjectId = originalProjectID
			}()

			repoDir := setupCleanGitRepo(t)
			testDB := setupParallelWaveTestDB(t, repoDir)
			defer func() {
				_ = testDB.Close()
			}()
			db = testDB
			authorizedRole = "fixer"
			authorizedProjectId = 1
			created := createLaunchableTestWave(t, testDB)

			callResult, _, err := WaitForNetrunnerWave(context.Background(), nil, tc.input(created))
			if err == nil {
				t.Fatalf("expected %s rejection", tc.name)
			}
			if callResult == nil || !callResult.IsError {
				t.Fatal("expected MCP error result")
			}
			if !strings.Contains(err.Error(), tc.errorFragment) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestWaitNetrunnerWaveGateProfileEnvAuthSkipsAssumeRole(t *testing.T) {
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

	// No explicit assume_role call: the wait handler must pass the fixer
	// auth gate and reject the missing wave instead of returning
	// "access denied: requires fixer role".
	callResult, _, err := WaitForNetrunnerWave(context.Background(), nil, WaitForNetrunnerWaveInput{WaveId: 9999})
	if err == nil {
		t.Fatal("expected missing-wave rejection")
	}
	if strings.Contains(err.Error(), "requires fixer role") {
		t.Fatalf("wait gate must be auto-authorized from env, got auth error: %v", err)
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unexpected error: %v", err)
	}
	if callResult == nil || !callResult.IsError {
		t.Fatal("expected MCP error result")
	}
}

func TestWaitNetrunnerWaveFrozenAndStaleEpochReturnBlocked(t *testing.T) {
	cases := []struct {
		name              string
		epoch             int
		frozen            int
		reasonFragment    string
		expectStaleWorker bool
	}{
		{name: "frozen", epoch: 0, frozen: 1, reasonFragment: "project_orchestration_frozen"},
		{name: "stale_epoch", epoch: 1, frozen: 0, reasonFragment: "stale_orchestration_epoch", expectStaleWorker: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			originalDB := db
			originalRole := authorizedRole
			originalProjectID := authorizedProjectId
			defer func() {
				db = originalDB
				authorizedRole = originalRole
				authorizedProjectId = originalProjectID
			}()

			_, testDB, created, _ := setupRunningWaveTest(t)
			defer func() {
				_ = testDB.Close()
			}()
			if _, err := testDB.Exec(
				`INSERT INTO autonomous_run_status (project_id, state, summary, orchestration_epoch, orchestration_frozen)
				 VALUES (1, 'blocked', 'wait blocked test', ?, ?)`,
				tc.epoch,
				tc.frozen,
			); err != nil {
				t.Fatalf("seed orchestration control: %v", err)
			}

			callResult, out, err := WaitForNetrunnerWave(context.Background(), nil, WaitForNetrunnerWaveInput{WaveId: created.WaveId})
			if err != nil {
				t.Fatalf("wait_for_netrunner_wave returned error: %v", err)
			}
			if callResult != nil {
				t.Fatalf("expected structured blocked output, got call result %+v", callResult)
			}
			if out.Status != "blocked" || out.Result.FollowUpAllowed || out.Result.TerminalCondition != "follow_up_blocked" {
				t.Fatalf("unexpected blocked output: %+v", out)
			}
			if !strings.Contains(out.Result.FollowUpBlockedReason, tc.reasonFragment) {
				t.Fatalf("expected blocked reason %q, got %+v", tc.reasonFragment, out.Result)
			}
			if tc.expectStaleWorker {
				var staleCount int
				if err := testDB.QueryRow(
					"SELECT COUNT(*) FROM parallel_wave_worker WHERE wave_id = ? AND status = ?",
					created.WaveId,
					parallelWaveWorkerStatusStaleEpoch,
				).Scan(&staleCount); err != nil {
					t.Fatalf("query stale workers: %v", err)
				}
				if staleCount != 2 {
					t.Fatalf("expected two stale workers, got %d", staleCount)
				}
			}
		})
	}
}

func TestWaitNetrunnerWaveReturnsLowestReadyWorker(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
	}()

	_, testDB, created, _ := setupRunningWaveTest(t)
	defer func() {
		_ = testDB.Close()
	}()
	if _, err := testDB.Exec("UPDATE session SET status = 'review', report = 'ready one' WHERE id IN (1, 3)"); err != nil {
		t.Fatalf("mark sessions review: %v", err)
	}
	markFakeWaveWorkerExited(t, testDB, created.WaveId, 1)
	if _, err := testDB.Exec("INSERT INTO doc_proposal (project_id, session_id, status, proposed_content) VALUES (1, 1, 'pending', 'doc one'), (1, 3, 'pending', 'doc two')"); err != nil {
		t.Fatalf("seed proposals: %v", err)
	}

	callResult, out, err := WaitForNetrunnerWave(context.Background(), nil, WaitForNetrunnerWaveInput{WaveId: created.WaveId})
	if err != nil {
		t.Fatalf("wait_for_netrunner_wave failed: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil call result on success, got %+v", callResult)
	}
	if out.Result.WinningSessionId != 1 || out.Result.TerminalCondition != "review_ready" || out.Result.WorkerStatus != parallelWaveWorkerStatusReviewReady {
		t.Fatalf("expected lowest ready worker to win, got %+v", out.Result)
	}
	if len(out.Result.ProposalIds) != 1 || out.Result.ProposalIds[0] != 1 {
		t.Fatalf("expected winner proposal id, got %+v", out.Result.ProposalIds)
	}
	if out.Result.WaveStatus != parallelWaveStatusReviewReady {
		t.Fatalf("expected review-ready wave status, got %+v", out.Result)
	}
}

func TestWaitNetrunnerWaveKeepsMalformedReviewWorkerReviewReady(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
	}()

	_, testDB, created, _ := setupRunningWaveTest(t)
	defer func() {
		_ = testDB.Close()
	}()
	if _, err := testDB.Exec("UPDATE session SET status = 'review', report = '' WHERE id = 1"); err != nil {
		t.Fatalf("mark session malformed review: %v", err)
	}
	markFakeWaveWorkerExited(t, testDB, created.WaveId, 1)

	callResult, out, err := WaitForNetrunnerWave(context.Background(), nil, WaitForNetrunnerWaveInput{WaveId: created.WaveId})
	if err != nil {
		t.Fatalf("wait_for_netrunner_wave failed: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil call result on success, got %+v", callResult)
	}
	if out.Result.TerminalCondition != "review_ready" || out.Result.WorkerStatus != parallelWaveWorkerStatusReviewReady {
		t.Fatalf("expected malformed review worker to remain review-ready, got %+v", out.Result)
	}
	var failureReason string
	if err := testDB.QueryRow(
		"SELECT failure_reason FROM parallel_wave_worker WHERE wave_id = ? AND session_id = 1",
		created.WaveId,
	).Scan(&failureReason); err != nil {
		t.Fatalf("query worker failure reason: %v", err)
	}
	if !strings.Contains(failureReason, "reached review without final report") {
		t.Fatalf("expected malformed review blocker, got %q", failureReason)
	}
}

func TestWaitNetrunnerWaveCapturesReviewReadyDiffArtifact(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
	}()

	repoDir, testDB, created, wave := setupRunningWaveTest(t)
	defer func() {
		_ = testDB.Close()
	}()
	worker := testWaveWorkerBySession(t, wave, 1)
	absWorktreePath, err := resolveParallelWaveWorktreePath(repoDir, worker.WorktreePath)
	if err != nil {
		t.Fatalf("resolve worktree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(absWorktreePath, "README.md"), []byte("worker change\n"), 0o644); err != nil {
		t.Fatalf("write worker change: %v", err)
	}
	if err := os.WriteFile(filepath.Join(absWorktreePath, "NEW.md"), []byte("new worker file\n"), 0o644); err != nil {
		t.Fatalf("write untracked worker change: %v", err)
	}
	if _, err := testDB.Exec("UPDATE session SET status = 'review', report = 'ready with diff' WHERE id = 1"); err != nil {
		t.Fatalf("mark session review: %v", err)
	}
	markFakeWaveWorkerExited(t, testDB, created.WaveId, 1)
	if _, err := testDB.Exec("INSERT INTO doc_proposal (project_id, session_id, status, proposed_content) VALUES (1, 1, 'pending', 'phase 5 doc')"); err != nil {
		t.Fatalf("seed proposal: %v", err)
	}

	callResult, out, err := WaitForNetrunnerWave(context.Background(), nil, WaitForNetrunnerWaveInput{WaveId: created.WaveId})
	if err != nil {
		t.Fatalf("wait_for_netrunner_wave failed: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil call result on success, got %+v", callResult)
	}
	if out.Result.HeadSha == "" || out.Result.HeadSha != created.BaseSha {
		t.Fatalf("expected head sha captured from worktree base, got %+v", out.Result)
	}
	if !containsString(out.Result.ChangedPaths, "README.md") {
		t.Fatalf("expected README.md in changed paths, got %+v", out.Result.ChangedPaths)
	}
	if !containsString(out.Result.ChangedPaths, "NEW.md") {
		t.Fatalf("expected NEW.md in changed paths, got %+v", out.Result.ChangedPaths)
	}
	if !strings.Contains(out.Result.DiffStat, "README.md") {
		t.Fatalf("expected diff stat to mention README.md, got %q", out.Result.DiffStat)
	}
	if !strings.Contains(out.Result.DiffStat, "NEW.md") {
		t.Fatalf("expected diff stat to mention NEW.md, got %q", out.Result.DiffStat)
	}
	if out.Result.DiffPatchPath == "" || !strings.Contains(out.Result.DiffPatchPath, filepath.Join(".codex", "netrunner_wave_artifacts")) {
		t.Fatalf("expected deterministic patch artifact path, got %+v", out.Result)
	}
	patchPayload, err := os.ReadFile(out.Result.DiffPatchPath)
	if err != nil {
		t.Fatalf("read patch artifact: %v", err)
	}
	if !strings.Contains(string(patchPayload), "worker change") {
		t.Fatalf("expected patch artifact to contain worker change, got:\n%s", string(patchPayload))
	}
	if !strings.Contains(string(patchPayload), "new worker file") {
		t.Fatalf("expected patch artifact to contain untracked worker change, got:\n%s", string(patchPayload))
	}
	var storedPatchPath string
	if err := testDB.QueryRow("SELECT diff_patch_path FROM parallel_wave_worker WHERE id = ?", worker.Id).Scan(&storedPatchPath); err != nil {
		t.Fatalf("query stored patch path: %v", err)
	}
	if storedPatchPath != out.Result.DiffPatchPath {
		t.Fatalf("expected DB patch path %q, got %q", out.Result.DiffPatchPath, storedPatchPath)
	}
}

func TestWaitNetrunnerWavePatchArtifactsPassGitApplyCheck(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(t *testing.T, worktreePath string)
		paths  []string
	}{
		{
			name: "tracked_only_change",
			mutate: func(t *testing.T, worktreePath string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(worktreePath, "README.md"), []byte("tracked worker change\n"), 0o644); err != nil {
					t.Fatalf("write tracked change: %v", err)
				}
			},
			paths: []string{"README.md"},
		},
		{
			name: "untracked_only_new_file",
			mutate: func(t *testing.T, worktreePath string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(worktreePath, "NEW.md"), []byte("new worker file\n"), 0o644); err != nil {
					t.Fatalf("write untracked change: %v", err)
				}
			},
			paths: []string{"NEW.md"},
		},
		{
			name: "mixed_tracked_and_untracked",
			mutate: func(t *testing.T, worktreePath string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(worktreePath, "README.md"), []byte("mixed tracked worker change\n"), 0o644); err != nil {
					t.Fatalf("write mixed tracked change: %v", err)
				}
				if err := os.WriteFile(filepath.Join(worktreePath, "NEW.md"), []byte("mixed new worker file\n"), 0o644); err != nil {
					t.Fatalf("write mixed untracked change: %v", err)
				}
			},
			paths: []string{"NEW.md", "README.md"},
		},
		{
			name: "tracked_no_newline_at_eof",
			mutate: func(t *testing.T, worktreePath string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(worktreePath, "README.md"), []byte("tracked worker change without trailing newline"), 0o644); err != nil {
					t.Fatalf("write no-newline tracked change: %v", err)
				}
			},
			paths: []string{"README.md"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			originalDB := db
			originalRole := authorizedRole
			originalProjectID := authorizedProjectId
			defer func() {
				db = originalDB
				authorizedRole = originalRole
				authorizedProjectId = originalProjectID
			}()

			repoDir, testDB, _, wave := setupRunningWaveTest(t)
			defer func() {
				_ = testDB.Close()
			}()
			worker := testWaveWorkerBySession(t, wave, 1)
			absWorktreePath, err := resolveParallelWaveWorktreePath(repoDir, worker.WorktreePath)
			if err != nil {
				t.Fatalf("resolve worktree: %v", err)
			}
			tc.mutate(t, absWorktreePath)

			_, changedPaths, patchPath, _, err := captureParallelWaveWorkerDiff(repoDir, wave, worker)
			if err != nil {
				t.Fatalf("capture diff artifact: %v", err)
			}
			for _, expectedPath := range tc.paths {
				if !containsString(changedPaths, expectedPath) {
					t.Fatalf("expected changed path %q in %+v", expectedPath, changedPaths)
				}
			}
			patchPayload, err := os.ReadFile(patchPath)
			if err != nil {
				t.Fatalf("read patch artifact: %v", err)
			}
			if len(patchPayload) == 0 {
				t.Fatalf("expected non-empty patch artifact")
			}
			if patchPayload[len(patchPayload)-1] != '\n' {
				t.Fatalf("expected patch artifact to end with newline")
			}
			if bytes.HasSuffix(patchPayload, []byte("\n\n")) {
				t.Fatalf("expected patch artifact to end with exactly one newline, got:\n%s", string(patchPayload))
			}
			runGitTestCommand(t, repoDir, "apply", "--check", patchPath)
		})
	}
}

func TestWaitNetrunnerWaveMarksMissingWorktreeAndDeadProcessFailed(t *testing.T) {
	cases := []struct {
		name           string
		mutate         func(t *testing.T, testDB *sql.DB, repoDir string, wave NetrunnerWaveSnapshot)
		reasonFragment string
	}{
		{
			name: "missing_worktree",
			mutate: func(t *testing.T, _ *sql.DB, repoDir string, wave NetrunnerWaveSnapshot) {
				t.Helper()
				worker := testWaveWorkerBySession(t, wave, 1)
				absWorktreePath, err := resolveParallelWaveWorktreePath(repoDir, worker.WorktreePath)
				if err != nil {
					t.Fatalf("resolve worktree: %v", err)
				}
				if err := os.RemoveAll(absWorktreePath); err != nil {
					t.Fatalf("remove worktree path: %v", err)
				}
			},
			reasonFragment: "worktree missing",
		},
		{
			name: "dead_process",
			mutate: func(t *testing.T, testDB *sql.DB, _ string, wave NetrunnerWaveSnapshot) {
				t.Helper()
				worker := testWaveWorkerBySession(t, wave, 1)
				if _, err := testDB.Exec("UPDATE worker_process SET pid = 0 WHERE id = ?", worker.WorkerProcessId); err != nil {
					t.Fatalf("mark process dead: %v", err)
				}
			},
			reasonFragment: "process exited",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			originalDB := db
			originalRole := authorizedRole
			originalProjectID := authorizedProjectId
			defer func() {
				db = originalDB
				authorizedRole = originalRole
				authorizedProjectId = originalProjectID
			}()

			repoDir, testDB, created, wave := setupRunningWaveTest(t)
			defer func() {
				_ = testDB.Close()
			}()
			tc.mutate(t, testDB, repoDir, wave)

			callResult, out, err := WaitForNetrunnerWave(context.Background(), nil, WaitForNetrunnerWaveInput{
				WaveId:              created.WaveId,
				TimeoutSeconds:      300,
				PollIntervalSeconds: 1,
			})
			if err != nil {
				t.Fatalf("wait_for_netrunner_wave failed: %v", err)
			}
			if callResult != nil {
				t.Fatalf("expected nil call result on terminal worker failure, got %+v", callResult)
			}
			if out.Result.WinningSessionId != 1 || out.Result.WorkerStatus != parallelWaveWorkerStatusFailed || out.Result.TerminalCondition != "failed" {
				t.Fatalf("expected failed worker 1 to win, got %+v", out.Result)
			}
			var failureReason string
			if err := testDB.QueryRow(
				"SELECT failure_reason FROM parallel_wave_worker WHERE wave_id = ? AND status = ? ORDER BY session_id LIMIT 1",
				created.WaveId,
				parallelWaveWorkerStatusFailed,
			).Scan(&failureReason); err != nil {
				t.Fatalf("query failure reason: %v", err)
			}
			if !strings.Contains(failureReason, tc.reasonFragment) {
				t.Fatalf("expected failure reason containing %q, got %q", tc.reasonFragment, failureReason)
			}
		})
	}
}

func TestInspectParallelWaveWorker_LiveProcessDoesNotMaskMissingWorktree(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
	}()

	repoDir, testDB, _, wave := setupRunningWaveTest(t)
	defer func() {
		_ = testDB.Close()
	}()

	worker := testWaveWorkerBySession(t, wave, 1)

	// Case 1: Healthy worktree with live process must remain non-terminal (running).
	candidate, terminal, err := inspectParallelWaveWorkerForWait(repoDir, wave, worker)
	if err != nil {
		t.Fatalf("inspect healthy worker: %v", err)
	}
	if terminal {
		t.Fatalf("expected healthy worker with live process to be non-terminal, got %+v", candidate)
	}

	// Case 2: Removed worktree while process is still alive must fail immediately instead of hanging.
	absWorktreePath, err := resolveParallelWaveWorktreePath(repoDir, worker.WorktreePath)
	if err != nil {
		t.Fatalf("resolve worktree: %v", err)
	}
	if err := os.RemoveAll(absWorktreePath); err != nil {
		t.Fatalf("remove worktree: %v", err)
	}

	candidate, terminal, err = inspectParallelWaveWorkerForWait(repoDir, wave, worker)
	if err != nil {
		t.Fatalf("inspect worker with missing worktree: %v", err)
	}
	if !terminal || candidate.TerminalCondition != "failed" || candidate.Worker.Status != parallelWaveWorkerStatusFailed {
		t.Fatalf("expected missing worktree to be terminal failed immediately, got terminal=%v candidate=%+v", terminal, candidate)
	}
	if !strings.Contains(candidate.Worker.FailureReason, "worker worktree missing") {
		t.Fatalf("expected failure reason to contain 'worker worktree missing', got %q", candidate.Worker.FailureReason)
	}
}

func TestCleanupNetrunnerWaveRejectsMissingAliveAndActiveWorkers(t *testing.T) {
	cases := []struct {
		name          string
		waveID        func(CreateNetrunnerWaveOutput) int
		setup         func(t *testing.T, testDB *sql.DB, waveID int)
		errorFragment string
	}{
		{
			name:          "missing",
			waveID:        func(_ CreateNetrunnerWaveOutput) int { return 9999 },
			errorFragment: "not found",
		},
		{
			name:          "alive_process",
			errorFragment: "alive running process",
			setup: func(t *testing.T, testDB *sql.DB, waveID int) {
				t.Helper()
				if _, err := testDB.Exec(
					`UPDATE parallel_wave_worker
					 SET status = ?, terminal_at = CURRENT_TIMESTAMP
					 WHERE wave_id = ?`,
					parallelWaveWorkerStatusCompleted,
					waveID,
				); err != nil {
					t.Fatalf("mark workers completed: %v", err)
				}
				if _, err := testDB.Exec("UPDATE parallel_wave SET status = ? WHERE id = ?", parallelWaveStatusCompleted, waveID); err != nil {
					t.Fatalf("mark wave completed: %v", err)
				}
			},
		},
		{
			name:          "active_worker_status",
			errorFragment: "is not terminal",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			originalDB := db
			originalRole := authorizedRole
			originalProjectID := authorizedProjectId
			defer func() {
				db = originalDB
				authorizedRole = originalRole
				authorizedProjectId = originalProjectID
			}()

			_, testDB, created, _ := setupRunningWaveTest(t)
			defer func() {
				_ = testDB.Close()
			}()
			if tc.setup != nil {
				tc.setup(t, testDB, created.WaveId)
			}
			waveID := created.WaveId
			if tc.waveID != nil {
				waveID = tc.waveID(created)
			}

			callResult, _, err := CleanupNetrunnerWave(context.Background(), nil, CleanupNetrunnerWaveInput{WaveId: waveID})
			if err == nil {
				t.Fatalf("expected cleanup rejection for %s", tc.name)
			}
			if callResult == nil || !callResult.IsError {
				t.Fatal("expected MCP error result")
			}
			if !strings.Contains(err.Error(), tc.errorFragment) {
				t.Fatalf("expected error containing %q, got %v", tc.errorFragment, err)
			}
		})
	}
}

func TestCleanupNetrunnerWaveRefusesUnsafeWorktreePaths(t *testing.T) {
	cases := []struct {
		name          string
		rawPath       string
		errorFragment string
	}{
		{name: "project_root", rawPath: ".", errorFragment: "project root"},
		{name: "filesystem_root", rawPath: string(os.PathSeparator), errorFragment: "filesystem root"},
		{name: "relative_escape", rawPath: "../outside", errorFragment: "escaping project root"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			originalDB := db
			originalRole := authorizedRole
			originalProjectID := authorizedProjectId
			defer func() {
				db = originalDB
				authorizedRole = originalRole
				authorizedProjectId = originalProjectID
			}()

			_, testDB, created, _ := setupRunningWaveTest(t)
			defer func() {
				_ = testDB.Close()
			}()
			markTestWaveTerminalForCleanup(t, testDB, created.WaveId, parallelWaveWorkerStatusCompleted)
			var workerID int
			if err := testDB.QueryRow("SELECT id FROM parallel_wave_worker WHERE wave_id = ? ORDER BY id LIMIT 1", created.WaveId).Scan(&workerID); err != nil {
				t.Fatalf("query worker id: %v", err)
			}
			if _, err := testDB.Exec(
				"UPDATE parallel_wave_worker SET worktree_path = ? WHERE id = ?",
				tc.rawPath,
				workerID,
			); err != nil {
				t.Fatalf("set unsafe worktree path: %v", err)
			}

			callResult, _, err := CleanupNetrunnerWave(context.Background(), nil, CleanupNetrunnerWaveInput{WaveId: created.WaveId})
			if err == nil {
				t.Fatalf("expected unsafe path rejection for %s", tc.name)
			}
			if callResult == nil || !callResult.IsError {
				t.Fatal("expected MCP error result")
			}
			if !strings.Contains(err.Error(), tc.errorFragment) {
				t.Fatalf("expected error containing %q, got %v", tc.errorFragment, err)
			}
		})
	}
}

func TestCleanupNetrunnerWaveDryRunReportsWithoutRemoving(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
	}()

	repoDir, testDB, created, wave := setupRunningWaveTest(t)
	defer func() {
		_ = testDB.Close()
	}()
	markTestWaveTerminalForCleanup(t, testDB, created.WaveId, parallelWaveWorkerStatusCompleted)

	callResult, out, err := CleanupNetrunnerWave(context.Background(), nil, CleanupNetrunnerWaveInput{WaveId: created.WaveId})
	if err != nil {
		t.Fatalf("cleanup dry run failed: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil call result on dry cleanup, got %+v", callResult)
	}
	if out.Status != "inspected" || out.Cleaned || out.RemoveWorktrees {
		t.Fatalf("unexpected dry cleanup output: %+v", out)
	}
	if len(out.Workers) != 2 {
		t.Fatalf("expected two worker cleanup results, got %+v", out.Workers)
	}
	for _, worker := range wave.Workers {
		absWorktreePath, err := resolveParallelWaveWorktreePath(repoDir, worker.WorktreePath)
		if err != nil {
			t.Fatalf("resolve worktree path: %v", err)
		}
		if info, err := os.Stat(absWorktreePath); err != nil || !info.IsDir() {
			t.Fatalf("dry cleanup should leave worktree %s in place, stat=%v info=%+v", absWorktreePath, err, info)
		}
	}
	var pendingCount int
	if err := testDB.QueryRow(
		"SELECT COUNT(*) FROM parallel_wave_worker WHERE wave_id = ? AND cleanup_status = ?",
		created.WaveId,
		parallelWaveCleanupStatusPending,
	).Scan(&pendingCount); err != nil {
		t.Fatalf("query cleanup pending count: %v", err)
	}
	if pendingCount != 2 {
		t.Fatalf("expected dry cleanup to leave statuses pending, got %d", pendingCount)
	}
}

func TestCleanupNetrunnerWaveRemoveModeRemovesWorktreesAndMarksCleaned(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
	}()

	repoDir, testDB, created, wave := setupRunningWaveTest(t)
	defer func() {
		_ = testDB.Close()
	}()
	markTestWaveTerminalForCleanup(t, testDB, created.WaveId, parallelWaveWorkerStatusCompleted)

	callResult, out, err := CleanupNetrunnerWave(context.Background(), nil, CleanupNetrunnerWaveInput{
		WaveId:          created.WaveId,
		RemoveWorktrees: true,
	})
	if err != nil {
		t.Fatalf("cleanup remove failed: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil call result on cleanup success, got %+v", callResult)
	}
	if out.Status != "success" || !out.Cleaned || out.WaveStatus != parallelWaveStatusCleaned {
		t.Fatalf("unexpected cleanup remove output: %+v", out)
	}
	for _, result := range out.Workers {
		if !result.Removed || result.CleanupStatus != parallelWaveCleanupStatusCleaned || result.WorkerStatus != parallelWaveWorkerStatusCleaned {
			t.Fatalf("expected removed/cleaned worker result, got %+v", result)
		}
	}
	for _, worker := range wave.Workers {
		absWorktreePath, err := resolveParallelWaveWorktreePath(repoDir, worker.WorktreePath)
		if err != nil {
			t.Fatalf("resolve worktree path: %v", err)
		}
		if _, err := os.Stat(absWorktreePath); !os.IsNotExist(err) {
			t.Fatalf("expected removed worktree %s, stat err=%v", absWorktreePath, err)
		}
	}
	var cleanedCount int
	if err := testDB.QueryRow(
		"SELECT COUNT(*) FROM parallel_wave_worker WHERE wave_id = ? AND status = ? AND cleanup_status = ? AND cleaned_at IS NOT NULL",
		created.WaveId,
		parallelWaveWorkerStatusCleaned,
		parallelWaveCleanupStatusCleaned,
	).Scan(&cleanedCount); err != nil {
		t.Fatalf("query cleaned workers: %v", err)
	}
	if cleanedCount != 2 {
		t.Fatalf("expected two cleaned workers, got %d", cleanedCount)
	}
}

func TestCleanupNetrunnerWaveMissingWorktreesMarkMissingAndCanPrune(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
	}()

	repoDir, testDB, created, wave := setupRunningWaveTest(t)
	defer func() {
		_ = testDB.Close()
	}()
	markTestWaveTerminalForCleanup(t, testDB, created.WaveId, parallelWaveWorkerStatusFailed)
	for _, worker := range wave.Workers {
		absWorktreePath, err := resolveParallelWaveWorktreePath(repoDir, worker.WorktreePath)
		if err != nil {
			t.Fatalf("resolve worktree path: %v", err)
		}
		if err := os.RemoveAll(absWorktreePath); err != nil {
			t.Fatalf("remove worktree dir manually: %v", err)
		}
	}

	callResult, out, err := CleanupNetrunnerWave(context.Background(), nil, CleanupNetrunnerWaveInput{
		WaveId: created.WaveId,
		Prune:  true,
	})
	if err != nil {
		t.Fatalf("cleanup missing/prune failed: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil call result on missing cleanup, got %+v", callResult)
	}
	if out.Status != "success" || !out.Cleaned || !out.PruneRan || out.WaveStatus != parallelWaveStatusCleaned {
		t.Fatalf("unexpected missing cleanup output: %+v", out)
	}
	if len(out.OrphanDiagnostics) != 2 {
		t.Fatalf("expected two missing diagnostics, got %+v", out.OrphanDiagnostics)
	}
	for _, result := range out.Workers {
		if !result.Missing || result.CleanupStatus != parallelWaveCleanupStatusMissing || !strings.Contains(result.Diagnostic, "missing") {
			t.Fatalf("expected missing cleanup result, got %+v", result)
		}
	}
	var missingCount int
	if err := testDB.QueryRow(
		"SELECT COUNT(*) FROM parallel_wave_worker WHERE wave_id = ? AND cleanup_status = ? AND cleaned_at IS NOT NULL",
		created.WaveId,
		parallelWaveCleanupStatusMissing,
	).Scan(&missingCount); err != nil {
		t.Fatalf("query missing cleanup workers: %v", err)
	}
	if missingCount != 2 {
		t.Fatalf("expected two missing cleanup statuses, got %d", missingCount)
	}
}

func TestParallelWaveHandlersLiveOutsideMain(t *testing.T) {
	mainSource, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	waveSource, err := os.ReadFile("parallel_wave_handlers.go")
	if err != nil {
		t.Fatalf("read parallel_wave_handlers.go: %v", err)
	}
	waveAdmissionGitSource, err := os.ReadFile("parallel_wave_admission_git.go")
	if err != nil {
		t.Fatalf("read parallel_wave_admission_git.go: %v", err)
	}
	waveArtifactsSource, err := os.ReadFile("parallel_wave_artifacts.go")
	if err != nil {
		t.Fatalf("read parallel_wave_artifacts.go: %v", err)
	}
	waveCleanupSource, err := os.ReadFile("parallel_wave_cleanup.go")
	if err != nil {
		t.Fatalf("read parallel_wave_cleanup.go: %v", err)
	}
	launchWaitSource, err := os.ReadFile("launch_wait_handlers.go")
	if err != nil {
		t.Fatalf("read launch_wait_handlers.go: %v", err)
	}

	waveSourceByName := map[string]string{
		"parallel_wave_handlers.go":      string(waveSource),
		"parallel_wave_admission_git.go": string(waveAdmissionGitSource),
		"parallel_wave_artifacts.go":     string(waveArtifactsSource),
		"parallel_wave_cleanup.go":       string(waveCleanupSource),
	}
	waveSymbolsByFile := map[string][]string{
		"parallel_wave_admission_git.go": {
			"const (\n\tparallelWaveStatusCreated",
			"var parallelWaveBranchPattern",
			"var parallelWaveFoundationWriteScopePaths",
			"type parallelWaveAdmissionWorker",
			"type parallelWaveSessionCandidate",
			"type gitCommandSpec",
			"func normalizeParallelWaveAdmissionWorkers(",
			"func gitCommand(",
			"func verifyParallelWaveGitBase(",
		},
		"parallel_wave_artifacts.go": {
			"func gitCommandInWorktree(",
			"func splitGitPathLines(",
			"func mergeGitChangedPaths(",
			"func combineParallelWavePatchPayloads(",
			"func captureParallelWaveWorkerDiff(",
		},
		"parallel_wave_cleanup.go": {
			"func parseGitWorktreeListPorcelain(",
			"func resolveParallelWaveCleanupWorktreePath(",
			"func validateParallelWaveCleanupPreconditions(",
			"func markParallelWaveCleanedIfReady(",
		},
		"parallel_wave_handlers.go": {
			"func recordWaveWorkerProcessLaunch(",
			"type CreateNetrunnerWaveInput",
			"type GetNetrunnerWaveInput",
			"type LaunchNetrunnerWaveInput",
			"type WaitForNetrunnerWaveInput",
			"type CleanupNetrunnerWaveInput",
			"type NetrunnerWaveSnapshot",
			"type NetrunnerWaveCleanupWorkerResult",
			"func CreateNetrunnerWave(",
			"func GetNetrunnerWave(",
			"func LaunchNetrunnerWave(",
			"func WaitForNetrunnerWave(",
			"func CleanupNetrunnerWave(",
			"func parallelWaveFollowUpDecision(",
		},
	}
	for fileName, symbols := range waveSymbolsByFile {
		source := waveSourceByName[fileName]
		for _, symbol := range symbols {
			if strings.Contains(string(mainSource), symbol) {
				t.Fatalf("expected wave symbol %q to be extracted out of main.go", symbol)
			}
			if strings.Contains(string(launchWaitSource), symbol) {
				t.Fatalf("expected wave symbol %q to stay out of launch_wait_handlers.go", symbol)
			}
			if !strings.Contains(source, symbol) {
				t.Fatalf("expected wave symbol %q in %s", symbol, fileName)
			}
			for otherFileName, otherSource := range waveSourceByName {
				if otherFileName == fileName {
					continue
				}
				if strings.Contains(otherSource, symbol) {
					t.Fatalf("expected wave symbol %q to stay out of %s", symbol, otherFileName)
				}
			}
		}
	}

	nonWaveSymbols := []string{
		"type LaunchAndWaitFixersInput",
		"func LaunchAndWaitFixers(",
		"type LaunchAndWaitNetrunnerInput",
		"func LaunchAndWaitNetrunner(",
		"type LaunchImageGenerationJobInput",
		"func LaunchImageGenerationJob(",
		"func WaitForImageGenerationJob(",
		"type workerProcessSnapshot",
		"func isProcessAlive(",
		"func latestWorkerLaunchEpoch(",
		"func listRunningWorkerProcesses(",
	}
	for _, symbol := range nonWaveSymbols {
		if strings.Contains(string(waveSource), symbol) {
			t.Fatalf("expected non-wave/shared symbol %q to stay out of parallel_wave_handlers.go", symbol)
		}
	}
}

func TestWaitNetrunnerWaveAggregatesReportsOnAllTerminal(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
	}()

	repoDir, testDB, created, _ := setupRunningWaveTest(t)
	defer func() {
		_ = os.RemoveAll(repoDir)
		_ = testDB.Close()
	}()

	// Give workers reports and mark them terminal
	for _, w := range created.Workers {
		reportName := "report_" + strconv.Itoa(w.SessionId)
		globalSessionID, err := globalSessionIDFromProjectScoped(w.SessionId, w.ProjectId)
		if err != nil {
			t.Fatalf("globalSessionIDFromProjectScoped error: %v", err)
		}
		_, err = testDB.Exec("UPDATE session SET report = ?, status = 'completed' WHERE id = ?", reportName, globalSessionID)
		if err != nil {
			t.Fatalf("update session error: %v", err)
		}
	}
	markTestWaveTerminalForCleanup(t, testDB, created.WaveId, "completed")

	callResult, out, err := WaitForNetrunnerWave(context.Background(), nil, WaitForNetrunnerWaveInput{
		WaveId:              created.WaveId,
		TimeoutSeconds:      300,
		PollIntervalSeconds: 1,
		ReturnWhen:          parallelWaveWaitAllTerminal,
	})
	if err != nil {
		t.Fatalf("wait error: %v", err)
	}
	if callResult != nil || out.Status != "success" {
		t.Fatalf("expected success, got %+v out: %+v", callResult, out)
	}

	if len(out.Reports) != 2 {
		t.Fatalf("expected 2 reports, got %d", len(out.Reports))
	}
	var manualReviewerCount int
	if err := testDB.QueryRow(
		"SELECT COUNT(*) FROM session WHERE project_id = 1 AND parallel_wave_id = ?",
		parallelWaveReviewMarker(created.WaveId),
	).Scan(&manualReviewerCount); err != nil {
		t.Fatalf("count manual-policy reviewer sessions: %v", err)
	}
	if manualReviewerCount != 0 {
		t.Fatalf("manual review policy created %d reviewer sessions after all workers terminated", manualReviewerCount)
	}

	for _, w := range created.Workers {
		reportName := "report_" + strconv.Itoa(w.SessionId)
		found := false
		for _, r := range out.Reports {
			if r == reportName {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing report for session %d in reports: %v", w.SessionId, out.Reports)
		}
	}
}

func TestLaunchNetrunnerWaveAbandonsFailedAndUnlaunchedWorkers(t *testing.T) {
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

	if _, err := testDB.Exec(
		"INSERT INTO session (project_id, task_description, status, declared_write_scope) VALUES (1, 'Task D', 'pending', '[\"docs/c\"]')",
	); err != nil {
		t.Fatalf("seed third worker session: %v", err)
	}
	callResult, created, err := CreateNetrunnerWave(context.Background(), nil, CreateNetrunnerWaveInput{
		SessionIds:   []int{1, 2, 3},
		Dependencies: []WaveDependency{{Child: 3, Parents: []int64{1}}},
		BaseRef:      "HEAD",
		Reason:       "launch abandonment test",
	})
	if err != nil || callResult != nil {
		t.Fatalf("create abandonment wave: result=%+v err=%v", callResult, err)
	}

	installFakeWaveWorkerLauncher(t, "2", nil)
	_, out, err := LaunchNetrunnerWave(context.Background(), nil, LaunchNetrunnerWaveInput{
		WaveId:         created.WaveId,
		TimeoutSeconds: 1,
	})
	if err == nil {
		t.Fatal("expected launch failure")
	}
	if out.Status != parallelWaveStatusPartiallyFailed || !out.PartialFailure {
		t.Fatalf("unexpected partial failure output: %+v err=%v", out, err)
	}

	wave, err := fetchNetrunnerWaveSnapshot(created.WaveId, 1)
	if err != nil {
		t.Fatalf("fetch abandonment wave: %v", err)
	}
	launched := testWaveWorkerBySession(t, wave, 1)
	failed := testWaveWorkerBySession(t, wave, 2)
	abandoned := testWaveWorkerBySession(t, wave, 3)
	if launched.Status != parallelWaveWorkerStatusRunning {
		t.Fatalf("expected launched worker to stay running, got %q", launched.Status)
	}
	if failed.Status != parallelWaveWorkerStatusFailed {
		t.Fatalf("expected failed worker status, got %q", failed.Status)
	}
	if abandoned.Status != parallelWaveWorkerStatusBlocked {
		t.Fatalf("expected never-launched worker to be blocked, got %q", abandoned.Status)
	}
	if !strings.Contains(abandoned.FailureReason, "blocked") || !strings.Contains(abandoned.FailureReason, "launch abandoned") {
		t.Fatalf("expected abandonment reason, got %q", abandoned.FailureReason)
	}

	launchedPath, err := resolveParallelWaveWorktreePath(repoDir, launched.WorktreePath)
	if err != nil {
		t.Fatalf("resolve launched worktree: %v", err)
	}
	failedPath, err := resolveParallelWaveWorktreePath(repoDir, failed.WorktreePath)
	if err != nil {
		t.Fatalf("resolve failed worktree: %v", err)
	}
	abandonedPath, err := resolveParallelWaveWorktreePath(repoDir, abandoned.WorktreePath)
	if err != nil {
		t.Fatalf("resolve abandoned worktree: %v", err)
	}
	if info, statErr := os.Stat(launchedPath); statErr != nil || !info.IsDir() {
		t.Fatalf("expected launched worktree to remain, stat=%v info=%+v", statErr, info)
	}
	for _, path := range []string{failedPath, abandonedPath} {
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Fatalf("expected abandoned worktree %s to be removed, stat err=%v", path, statErr)
		}
	}

	leaseActive := func(scopePath string) int {
		t.Helper()
		var count int
		if err := testDB.QueryRow(
			"SELECT COUNT(*) FROM parallel_wave_scope_lease WHERE wave_id = ? AND scope_path = ? AND active = 1",
			created.WaveId,
			scopePath,
		).Scan(&count); err != nil {
			t.Fatalf("query lease %q: %v", scopePath, err)
		}
		return count
	}
	if got := leaseActive("docs/a"); got != 1 {
		t.Fatalf("expected launched worker lease held, got %d", got)
	}
	if got := leaseActive("docs/b"); got != 0 {
		t.Fatalf("expected failed worker lease released, got %d", got)
	}
	if got := leaseActive("docs/c"); got != 0 {
		t.Fatalf("expected abandoned worker lease released, got %d", got)
	}
}

func TestCleanupNetrunnerWaveReconcilesStaleCompletedWorker(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
	}()

	_, testDB, created, wave := setupRunningWaveTest(t)
	defer func() {
		_ = testDB.Close()
	}()
	_ = wave
	// Sessions are completed but the workers are still recorded as running and
	// their process rows no longer exist. Cleanup must reconcile them instead
	// of refusing a non-terminal worker forever.
	for _, worker := range created.Workers {
		globalSessionID, err := globalSessionIDFromProjectScoped(worker.SessionId, 1)
		if err != nil {
			t.Fatalf("map worker session: %v", err)
		}
		if _, err := testDB.Exec("UPDATE session SET status = 'completed', report = 'done' WHERE id = ?", globalSessionID); err != nil {
			t.Fatalf("complete worker session: %v", err)
		}
	}
	if _, err := testDB.Exec("DELETE FROM worker_process WHERE parallel_wave_id = ?", created.WaveId); err != nil {
		t.Fatalf("remove worker process rows: %v", err)
	}

	callResult, out, err := CleanupNetrunnerWave(context.Background(), nil, CleanupNetrunnerWaveInput{
		WaveId:          created.WaveId,
		RemoveWorktrees: true,
	})
	if err != nil {
		t.Fatalf("cleanup should reconcile stale workers: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil call result, got %+v", callResult)
	}
	if out.Status != "success" || !out.Cleaned || out.WaveStatus != parallelWaveStatusCleaned {
		t.Fatalf("unexpected reconciled cleanup output: %+v", out)
	}
	for _, result := range out.Workers {
		if result.WorkerStatus != parallelWaveWorkerStatusCleaned || result.CleanupStatus != parallelWaveCleanupStatusCleaned {
			t.Fatalf("expected reconciled worker cleaned, got %+v", result)
		}
	}
}
