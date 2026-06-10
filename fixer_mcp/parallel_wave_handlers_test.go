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
			base_sha TEXT NOT NULL,
			base_branch TEXT NOT NULL DEFAULT '',
			project_cwd TEXT NOT NULL,
			worktree_root TEXT NOT NULL,
			orchestration_epoch INTEGER NOT NULL DEFAULT 0,
			created_by_session_id INTEGER,
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
			cleanup_status TEXT NOT NULL DEFAULT 'pending',
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			launched_at TEXT,
			terminal_at TEXT,
			cleaned_at TEXT
		);
		CREATE UNIQUE INDEX parallel_wave_worker_wave_session_unique_idx ON parallel_wave_worker(wave_id, session_id);
		CREATE INDEX parallel_wave_worker_status_idx ON parallel_wave_worker(project_id, status);
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
	runGitTestCommand(t, repoDir, "init")
	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("initial\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGitTestCommand(t, repoDir, "add", "README.md")
	runGitTestCommand(t, repoDir, "-c", "user.name=Fixer Test", "-c", "user.email=fixer@example.test", "commit", "-m", "initial")
	return repoDir
}

func TestNormalizeParallelWaveDeclaredWriteScope(t *testing.T) {
	t.Run("normalizes and sorts project-relative scopes", func(t *testing.T) {
		scope, err := normalizeParallelWaveDeclaredWriteScope([]string{"docs/design", "./fixer_mcp/wave_helpers.go", "docs/design"})
		if err != nil {
			t.Fatalf("normalize wave scope failed: %v", err)
		}
		expected := []string{"docs/design", "fixer_mcp/wave_helpers.go"}
		if len(scope) != len(expected) {
			t.Fatalf("expected %v, got %v", expected, scope)
		}
		for index := range expected {
			if scope[index] != expected[index] {
				t.Fatalf("expected %v, got %v", expected, scope)
			}
		}
	})

	t.Run("rejects empty or whole project scope", func(t *testing.T) {
		if _, err := normalizeParallelWaveDeclaredWriteScope(nil); err == nil || !strings.Contains(err.Error(), "must contain at least one") {
			t.Fatalf("expected empty scope rejection, got %v", err)
		}
		if _, err := normalizeParallelWaveDeclaredWriteScope([]string{"."}); err == nil || !strings.Contains(err.Error(), "cannot use broad") {
			t.Fatalf("expected broad scope rejection, got %v", err)
		}
	})

	t.Run("rejects overlapping entries within a worker", func(t *testing.T) {
		_, err := normalizeParallelWaveDeclaredWriteScope([]string{"docs", "docs/research"})
		if err == nil || !strings.Contains(err.Error(), "entries overlap") {
			t.Fatalf("expected overlapping entry rejection, got %v", err)
		}
	})

	t.Run("rejects foundation and local database paths", func(t *testing.T) {
		cases := [][]string{
			{"fixer_mcp/main.go"},
			{"fixer_mcp"},
			{"client_wires/fixer_wire.py"},
			{"AGENTS.md"},
			{".codex/netrunner_worktrees"},
			{"feature/fixer.db-wal"},
			{"mcp_config.json"},
		}
		for _, candidate := range cases {
			if _, err := normalizeParallelWaveDeclaredWriteScope(candidate); err == nil {
				t.Fatalf("expected foundation path rejection for %v", candidate)
			}
		}
	})
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

func TestCreateNetrunnerWaveRejectsUnsafeWriteScopes(t *testing.T) {
	cases := []struct {
		name          string
		firstScope    string
		secondScope   string
		errorFragment string
	}{
		{name: "broad", firstScope: `["."]`, secondScope: `["docs/b"]`, errorFragment: "cannot use broad"},
		{name: "overlap", firstScope: `["docs"]`, secondScope: `["docs/b"]`, errorFragment: "overlapping declared write scopes"},
		{name: "foundation", firstScope: `["fixer_mcp/main.go"]`, secondScope: `["docs/b"]`, errorFragment: "foundation/bootstrap"},
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
			if _, err := testDB.Exec("UPDATE session SET declared_write_scope = ? WHERE id = 1", tc.firstScope); err != nil {
				t.Fatalf("seed first scope: %v", err)
			}
			if _, err := testDB.Exec("UPDATE session SET declared_write_scope = ? WHERE id = 3", tc.secondScope); err != nil {
				t.Fatalf("seed second scope: %v", err)
			}

			db = testDB
			authorizedRole = "fixer"
			authorizedProjectId = 1

			callResult, _, err := CreateNetrunnerWave(context.Background(), nil, CreateNetrunnerWaveInput{SessionIds: []int{1, 2}})
			if err == nil {
				t.Fatalf("expected %s scope rejection", tc.name)
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
	if got.Wave.Workers[0].SessionId != 1 || got.Wave.Workers[1].SessionId != 2 {
		t.Fatalf("expected project-scoped session ids in get output, got %+v", got.Wave.Workers)
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
		if name == "python3" {
			if capturedArgs != nil {
				*capturedArgs = append(*capturedArgs, append([]string{}, arg...))
			}
			helperArgs := append([]string{"-test.run=TestHelperProcessWaveWorkerLaunch", "--"}, arg...)
			return exec.Command(os.Args[0], helperArgs...)
		}
		return exec.Command(name, arg...)
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
		WaveId:         created.WaveId,
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

func TestWaitNetrunnerWaveMarksMalformedReviewWorkerFailed(t *testing.T) {
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

	callResult, out, err := WaitForNetrunnerWave(context.Background(), nil, WaitForNetrunnerWaveInput{WaveId: created.WaveId})
	if err != nil {
		t.Fatalf("wait_for_netrunner_wave failed: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil call result on success, got %+v", callResult)
	}
	if out.Result.TerminalCondition != "failed" || out.Result.WorkerStatus != parallelWaveWorkerStatusFailed {
		t.Fatalf("expected malformed review worker to fail, got %+v", out.Result)
	}
	var failureReason string
	if err := testDB.QueryRow(
		"SELECT failure_reason FROM parallel_wave_worker WHERE wave_id = ? AND session_id = 1",
		created.WaveId,
	).Scan(&failureReason); err != nil {
		t.Fatalf("query worker failure reason: %v", err)
	}
	if !strings.Contains(failureReason, "reached review without final report and doc-impact proposal") {
		t.Fatalf("expected malformed review failure reason, got %q", failureReason)
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
				TimeoutSeconds:      1,
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

func TestWaitNetrunnerWaveTimeoutDoesNotMarkSuccess(t *testing.T) {
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

	callResult, out, err := WaitForNetrunnerWave(context.Background(), nil, WaitForNetrunnerWaveInput{
		WaveId:              created.WaveId,
		TimeoutSeconds:      1,
		PollIntervalSeconds: 1,
	})
	if err != nil {
		t.Fatalf("wait_for_netrunner_wave timeout path returned error: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil call result on structured timeout, got %+v", callResult)
	}
	if out.Status != "timed_out" || !out.Result.TimedOut || out.Result.Terminal || out.Result.TerminalCondition != "timed_out" {
		t.Fatalf("unexpected timeout output: %+v", out)
	}
	var waveStatus string
	if err := testDB.QueryRow("SELECT status FROM parallel_wave WHERE id = ?", created.WaveId).Scan(&waveStatus); err != nil {
		t.Fatalf("query wave status: %v", err)
	}
	if waveStatus != parallelWaveStatusRunning {
		t.Fatalf("expected wave to remain running after timeout, got %q", waveStatus)
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
