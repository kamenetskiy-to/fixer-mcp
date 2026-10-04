package main

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"sync"
	"testing"
)

func setupPlannedWaveHandlerTest(t *testing.T, projectCWD string) *sql.DB {
	t.Helper()
	testDB := setupParallelWaveTestDB(t, projectCWD)
	if _, err := testDB.Exec(`
		CREATE TABLE planned_wave (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL,
			title TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'planned',
			idempotency_key TEXT NOT NULL DEFAULT '',
			definition_hash TEXT NOT NULL,
			reason TEXT NOT NULL DEFAULT '',
			base_ref TEXT NOT NULL DEFAULT '',
			worktree_root TEXT NOT NULL DEFAULT '',
			epic_doc_id INTEGER,
			parent_wave_id INTEGER,
			max_child_wave_depth INTEGER NOT NULL DEFAULT 0,
			max_total_descendant_waves INTEGER NOT NULL DEFAULT 0,
			max_total_sessions INTEGER NOT NULL DEFAULT 0,
			initialized_wave_id INTEGER,
			failure_reason TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			initialized_at TEXT
		);
		CREATE INDEX planned_wave_project_status_idx ON planned_wave(project_id, status, id);
		CREATE UNIQUE INDEX planned_wave_project_idempotency_unique_idx
			ON planned_wave(project_id, idempotency_key) WHERE idempotency_key != '';
		CREATE TABLE planned_wave_task (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			planned_wave_id INTEGER NOT NULL,
			project_id INTEGER NOT NULL,
			task_key TEXT NOT NULL,
			position INTEGER NOT NULL,
			task_description TEXT NOT NULL,
			dependencies TEXT NOT NULL DEFAULT '[]',
			cli_backend TEXT NOT NULL DEFAULT 'codex',
			cli_model TEXT NOT NULL DEFAULT '',
			cli_reasoning TEXT NOT NULL DEFAULT '',
			mcp_server_names TEXT NOT NULL DEFAULT '[]',
			materialized_session_id INTEGER,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE UNIQUE INDEX planned_wave_task_key_unique_idx ON planned_wave_task(planned_wave_id, task_key);
		CREATE UNIQUE INDEX planned_wave_task_position_unique_idx ON planned_wave_task(planned_wave_id, position);
		CREATE INDEX planned_wave_task_project_idx ON planned_wave_task(project_id, planned_wave_id, position);
	`); err != nil {
		_ = testDB.Close()
		t.Fatalf("create planned-wave test schema: %v", err)
	}
	return testDB
}

func withPlannedWaveFixerState(t *testing.T, testDB *sql.DB, projectID int) {
	t.Helper()
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	originalSessionID := authorizedSessionId
	t.Cleanup(func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
		authorizedSessionId = originalSessionID
	})
	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = projectID
	authorizedSessionId = 0
}

func testPlannedWaveInput() CreatePlannedNetrunnerWaveInput {
	return CreatePlannedNetrunnerWaveInput{
		Title:          "Grand checklist: Mission Control follow-up",
		IdempotencyKey: "mission-control-follow-up",
		BaseRef:        "HEAD",
		Reason:         "future checklist work",
		Tasks: []PlannedWaveTaskInput{
			{
				Key:             "backend",
				TaskDescription: "Implement the backend slice.",
				Backend:         "codex",
				Model:           "gpt-5.6-sol",
				Reasoning:       "high",
				McpServerNames:  []string{"sqlite"},
			},
			{
				Key:             "frontend",
				TaskDescription: "Implement the frontend slice.",
				DependsOn:       []string{"backend"},
				Backend:         "codex",
				Model:           "gpt-5.6-sol",
				Reasoning:       "high",
				McpServerNames:  []string{"sqlite"},
			},
		},
	}
}

func TestCreatePlannedNetrunnerWaveHasNoRuntimeSideEffectsAndIsIdempotent(t *testing.T) {
	testDB := setupPlannedWaveHandlerTest(t, t.TempDir())
	defer testDB.Close()
	withPlannedWaveFixerState(t, testDB, 1)

	countRows := func(table string) int {
		t.Helper()
		var count int
		if err := testDB.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		return count
	}
	sessionsBefore := countRows("session")
	wavesBefore := countRows("parallel_wave")

	callResult, created, err := CreatePlannedNetrunnerWave(context.Background(), nil, testPlannedWaveInput())
	if err != nil || callResult != nil {
		t.Fatalf("create planned wave failed: result=%+v err=%v", callResult, err)
	}
	if created.Plan.Status != plannedWaveStatusPlanned || len(created.Plan.Tasks) != 2 {
		t.Fatalf("unexpected planned wave: %+v", created.Plan)
	}
	if created.Plan.Tasks[0].Backend != "codex" || created.Plan.Tasks[0].Model != "gpt-5.6-sol" || created.Plan.Tasks[0].Reasoning != "high" || len(created.Plan.Tasks[0].McpServerNames) != 1 || created.Plan.Tasks[0].McpServerNames[0] != "sqlite" {
		t.Fatalf("planned task assignments were not persisted: %+v", created.Plan.Tasks[0])
	}
	if created.Plan.Tasks[0].MaterializedSessionId != 0 || created.Plan.Tasks[1].MaterializedSessionId != 0 {
		t.Fatalf("planned tasks materialized before Initialize: %+v", created.Plan.Tasks)
	}
	if got := countRows("session"); got != sessionsBefore {
		t.Fatalf("planned definition created sessions: before=%d after=%d", sessionsBefore, got)
	}
	if got := countRows("parallel_wave"); got != wavesBefore {
		t.Fatalf("planned definition created a runtime wave: before=%d after=%d", wavesBefore, got)
	}

	_, replay, err := CreatePlannedNetrunnerWave(context.Background(), nil, testPlannedWaveInput())
	if err != nil || !replay.Idempotent || replay.PlanId != created.PlanId {
		t.Fatalf("idempotent replay mismatch: %+v err=%v", replay, err)
	}
	changed := testPlannedWaveInput()
	changed.Title = "Different definition"
	if _, _, err := CreatePlannedNetrunnerWave(context.Background(), nil, changed); err == nil || !strings.Contains(err.Error(), "different planned-wave definition") {
		t.Fatalf("expected idempotency conflict, got %v", err)
	}
}

func TestPlannedNetrunnerWaveProjectIsolation(t *testing.T) {
	testDB := setupPlannedWaveHandlerTest(t, t.TempDir())
	defer testDB.Close()
	withPlannedWaveFixerState(t, testDB, 1)

	_, created, err := CreatePlannedNetrunnerWave(context.Background(), nil, testPlannedWaveInput())
	if err != nil {
		t.Fatalf("create project-1 planned wave: %v", err)
	}
	authorizedProjectId = 2
	callResult, _, err := GetPlannedNetrunnerWave(context.Background(), nil, GetPlannedNetrunnerWaveInput{PlanId: created.PlanId})
	if err == nil || callResult == nil || !callResult.IsError || !strings.Contains(err.Error(), "not found in current project") {
		t.Fatalf("expected project isolation rejection, result=%+v err=%v", callResult, err)
	}
}

func TestInitializePlannedNetrunnerWaveDelegatesToGovernedCreationAndIsIdempotent(t *testing.T) {
	repoDir := setupCleanGitRepo(t)
	testDB := setupPlannedWaveHandlerTest(t, repoDir)
	defer testDB.Close()
	withPlannedWaveFixerState(t, testDB, 1)

	_, created, err := CreatePlannedNetrunnerWave(context.Background(), nil, testPlannedWaveInput())
	if err != nil {
		t.Fatalf("create planned wave: %v", err)
	}
	callResult, initialized, err := InitializePlannedNetrunnerWave(context.Background(), nil, InitializePlannedNetrunnerWaveInput{PlanId: created.PlanId})
	if err != nil || callResult != nil {
		t.Fatalf("initialize planned wave failed: result=%+v err=%v", callResult, err)
	}
	if initialized.Plan.Status != plannedWaveStatusInitialized || initialized.Wave.Phase != parallelWavePhaseInitialized || initialized.Wave.Status != parallelWaveStatusCreated {
		t.Fatalf("unexpected initialized result: %+v", initialized)
	}
	if len(initialized.Plan.Tasks) != 2 || initialized.Plan.Tasks[0].MaterializedSessionId == 0 || len(initialized.Wave.Workers) != 2 {
		t.Fatalf("expected two ordinary materialized sessions/workers: %+v", initialized)
	}
	for _, task := range initialized.Plan.Tasks {
		var backend, model, reasoning string
		var globalSessionID int
		if err := testDB.QueryRow(`
			SELECT session.id, session.cli_backend, session.cli_model, session.cli_reasoning
			FROM planned_wave_task planned
			JOIN session ON session.id = planned.materialized_session_id
			WHERE planned.id = ?`, task.Id).Scan(&globalSessionID, &backend, &model, &reasoning); err != nil {
			t.Fatalf("read materialized launch config for %s: %v", task.Key, err)
		}
		if backend != "codex" || model != "gpt-5.6-sol" || reasoning != "high" {
			t.Fatalf("unexpected materialized launch config for %s: %s/%s/%s", task.Key, backend, model, reasoning)
		}
		var mcpCount int
		if err := testDB.QueryRow("SELECT COUNT(*) FROM session_mcp_server WHERE session_id = ?", globalSessionID).Scan(&mcpCount); err != nil || mcpCount != 1 {
			t.Fatalf("unexpected materialized MCP assignments for %s: count=%d err=%v", task.Key, mcpCount, err)
		}
	}
	var dependencyCount int
	if err := testDB.QueryRow("SELECT COUNT(*) FROM wave_worker_dependency WHERE wave_id = ?", initialized.WaveId).Scan(&dependencyCount); err != nil {
		t.Fatalf("count initialized dependencies: %v", err)
	}
	if dependencyCount != 1 {
		t.Fatalf("expected planned dependency to materialize, got %d", dependencyCount)
	}

	var sessionCountBefore, waveCountBefore int
	if err := testDB.QueryRow("SELECT COUNT(*) FROM session").Scan(&sessionCountBefore); err != nil {
		t.Fatal(err)
	}
	if err := testDB.QueryRow("SELECT COUNT(*) FROM parallel_wave").Scan(&waveCountBefore); err != nil {
		t.Fatal(err)
	}
	_, replay, err := InitializePlannedNetrunnerWave(context.Background(), nil, InitializePlannedNetrunnerWaveInput{PlanId: created.PlanId})
	if err != nil || !replay.Idempotent || replay.WaveId != initialized.WaveId {
		t.Fatalf("idempotent Initialize replay mismatch: %+v err=%v", replay, err)
	}
	var sessionCountAfter, waveCountAfter int
	_ = testDB.QueryRow("SELECT COUNT(*) FROM session").Scan(&sessionCountAfter)
	_ = testDB.QueryRow("SELECT COUNT(*) FROM parallel_wave").Scan(&waveCountAfter)
	if sessionCountAfter != sessionCountBefore || waveCountAfter != waveCountBefore {
		t.Fatalf("Initialize replay duplicated runtime state: sessions %d->%d waves %d->%d", sessionCountBefore, sessionCountAfter, waveCountBefore, waveCountAfter)
	}
}

func TestInitializePlannedNetrunnerWaveFailureRetryReusesSessions(t *testing.T) {
	repoDir := setupCleanGitRepo(t)
	testDB := setupPlannedWaveHandlerTest(t, repoDir)
	defer testDB.Close()
	withPlannedWaveFixerState(t, testDB, 1)

	_, created, err := CreatePlannedNetrunnerWave(context.Background(), nil, testPlannedWaveInput())
	if err != nil {
		t.Fatalf("create planned wave: %v", err)
	}
	trackedPath := repoDir + "/README.md"
	if _, err := testDB.Exec("UPDATE planned_wave SET base_ref = 'HEAD' WHERE id = ?", created.PlanId); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(trackedPath, []byte("dirty\n"), 0o644); err != nil {
		t.Fatalf("dirty tracked file: %v", err)
	}
	if _, _, err := InitializePlannedNetrunnerWave(context.Background(), nil, InitializePlannedNetrunnerWaveInput{PlanId: created.PlanId}); err == nil {
		t.Fatal("expected dirty Git admission failure")
	}
	failed, err := fetchPlannedWaveSnapshot(context.Background(), created.PlanId, 1)
	if err != nil || failed.Status != plannedWaveStatusFailed || failed.FailureReason == "" {
		t.Fatalf("expected durable failed plan: %+v err=%v", failed, err)
	}
	var materializedBefore int
	if err := testDB.QueryRow("SELECT COUNT(*) FROM planned_wave_task WHERE planned_wave_id = ? AND materialized_session_id IS NOT NULL", created.PlanId).Scan(&materializedBefore); err != nil {
		t.Fatal(err)
	}
	runGitTestCommand(t, repoDir, "restore", "README.md")
	_, retried, err := InitializePlannedNetrunnerWave(context.Background(), nil, InitializePlannedNetrunnerWaveInput{PlanId: created.PlanId})
	if err != nil {
		t.Fatalf("retry Initialize after clearing admission failure: %v", err)
	}
	var materializedAfter int
	_ = testDB.QueryRow("SELECT COUNT(*) FROM planned_wave_task WHERE planned_wave_id = ? AND materialized_session_id IS NOT NULL", created.PlanId).Scan(&materializedAfter)
	if retried.Plan.Status != plannedWaveStatusInitialized || materializedAfter != materializedBefore {
		t.Fatalf("retry did not reuse materialized sessions: before=%d after=%d result=%+v", materializedBefore, materializedAfter, retried)
	}
}

func TestInitializePlannedNetrunnerWaveConcurrentCallsDoNotDuplicateWave(t *testing.T) {
	repoDir := setupCleanGitRepo(t)
	testDB := setupPlannedWaveHandlerTest(t, repoDir)
	defer testDB.Close()
	withPlannedWaveFixerState(t, testDB, 1)

	_, created, err := CreatePlannedNetrunnerWave(context.Background(), nil, testPlannedWaveInput())
	if err != nil {
		t.Fatalf("create planned wave: %v", err)
	}
	type outcome struct {
		output InitializePlannedNetrunnerWaveOutput
		err    error
	}
	outcomes := make(chan outcome, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, output, err := InitializePlannedNetrunnerWave(context.Background(), nil, InitializePlannedNetrunnerWaveInput{PlanId: created.PlanId})
			outcomes <- outcome{output: output, err: err}
		}()
	}
	wg.Wait()
	close(outcomes)
	successes := 0
	for result := range outcomes {
		if result.err == nil {
			successes++
			continue
		}
		if !strings.Contains(result.err.Error(), "already in progress") {
			t.Fatalf("unexpected concurrent Initialize error: %v", result.err)
		}
	}
	if successes == 0 {
		t.Fatal("expected one concurrent Initialize caller to succeed")
	}
	var waveCount int
	if err := testDB.QueryRow("SELECT COUNT(*) FROM parallel_wave").Scan(&waveCount); err != nil {
		t.Fatal(err)
	}
	if waveCount != 1 {
		t.Fatalf("concurrent Initialize duplicated governed waves: %d", waveCount)
	}
}
