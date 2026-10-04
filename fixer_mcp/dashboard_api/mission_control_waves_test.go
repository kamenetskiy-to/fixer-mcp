package dashboardapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestInitializeMissionControlPlannedWaveUsesGovernedProjectBoundary(t *testing.T) {
	repo := openFixtureRepository(t)
	defer repo.Close()
	now := time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC)
	repo.now = func() time.Time { return now }
	seedMissionControlWaves(t, repo, now)

	initializeCalls := 0
	repo.plannedWaveInitializer = func(ctx context.Context, project projectRecord, planID int) (int, error) {
		initializeCalls++
		if project.ID != 1 || project.CWD == "" || planID != 501 {
			t.Fatalf("governed initializer received unscoped identity: project=%+v plan=%d", project, planID)
		}
		if _, err := repo.dbWrite.ExecContext(ctx, `
			INSERT INTO parallel_wave (
				id, project_id, status, phase, gate_state, control_state, control_reason,
				failure_policy_state, repair_worker_id, repair_attempt_count, acceptance_session_id,
				failure_reason, created_at, updated_at, launched_at, completed_at
			) VALUES (150, 1, 'created', 'initialized', 'none', 'active', '', 'none', NULL, 0, NULL, '', ?, ?, NULL, NULL)`,
			now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
			return 0, err
		}
		if _, err := repo.dbWrite.ExecContext(ctx, `
			UPDATE planned_wave
			SET status = 'initialized', initialized_wave_id = 150, initialized_at = ?, updated_at = ?
			WHERE id = 501 AND project_id = 1`, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
			return 0, err
		}
		return 150, nil
	}

	server := httptest.NewServer(NewServer(repo))
	defer server.Close()
	request, err := http.NewRequest(http.MethodPost, server.URL+"/api/actions/projects/1/planned-waves/501/initialize", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("build Initialize request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("call Initialize route: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("Initialize route returned %d", response.StatusCode)
	}
	var payload InitializeMissionControlPlannedWaveResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode Initialize response: %v", err)
	}
	if initializeCalls != 1 || payload.ProjectID != 1 || payload.PlanID != 501 || payload.WaveID != 150 || payload.PlannedWave.InitializedWaveID != 150 || payload.Wave.WaveID != 150 {
		t.Fatalf("unexpected governed Initialize response: calls=%d payload=%+v", initializeCalls, payload)
	}

	leakProbe, err := http.NewRequest(http.MethodPost, server.URL+"/api/actions/projects/2/planned-waves/501/initialize", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("build project isolation request: %v", err)
	}
	leakProbe.Header.Set("Content-Type", "application/json")
	leakResponse, err := http.DefaultClient.Do(leakProbe)
	if err != nil {
		t.Fatalf("call project isolation route: %v", err)
	}
	defer leakResponse.Body.Close()
	if leakResponse.StatusCode != http.StatusNotFound || initializeCalls != 1 {
		t.Fatalf("cross-project plan reached initializer: status=%d calls=%d", leakResponse.StatusCode, initializeCalls)
	}
}

func TestMissionControlPlannedWaveInitializeCapabilityFailsClosedWithoutBridge(t *testing.T) {
	repo := openFixtureRepository(t)
	defer repo.Close()
	now := time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC)
	repo.now = func() time.Time { return now }
	seedMissionControlWaves(t, repo, now)
	repo.plannedWaveInitializerAvailable = func(projectRecord) bool { return false }

	snapshot, err := repo.ProjectMissionControlWaves(context.Background(), 1)
	if err != nil {
		t.Fatalf("load fail-closed planned waves: %v", err)
	}
	planned := snapshot.PlannedWaves[1]
	if planned.ActionCapabilities.Initialize.Enabled || !strings.Contains(planned.ActionCapabilities.Initialize.DisabledReason, "bridge") {
		t.Fatalf("Initialize capability did not fail closed: %+v", planned.ActionCapabilities.Initialize)
	}
}

func TestProjectMissionControlWavesExposesGovernedReadModel(t *testing.T) {
	repo := openFixtureRepository(t)
	defer repo.Close()

	now := time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC)
	repo.now = func() time.Time { return now }
	seedMissionControlWaves(t, repo, now)

	server := httptest.NewServer(NewServer(repo))
	defer server.Close()

	var response ProjectMissionControlWavesResponse
	readJSON(t, server.URL+"/api/projects/1/waves", &response)
	if response.ProjectID != 1 || response.GeneratedAt != now.Format(time.RFC3339Nano) {
		t.Fatalf("unexpected response identity: %+v", response)
	}
	if response.Freshness.State != "fresh" || response.Freshness.Stale || response.Freshness.AgeSeconds != 30 || response.Freshness.StaleAfterSeconds != 300 {
		t.Fatalf("unexpected freshness projection: %+v", response.Freshness)
	}
	if len(response.Waves) != 3 || response.Waves[0].WaveID != 103 || response.Waves[2].WaveID != 101 {
		t.Fatalf("expected all project waves newest first, got %+v", response.Waves)
	}
	if len(response.PlannedWaves) != 2 || response.PlannedWaves[0].PlanID != 502 || response.PlannedWaves[1].PlanID != 501 {
		t.Fatalf("expected project-scoped planned waves newest first, got %+v", response.PlannedWaves)
	}
	planned := response.PlannedWaves[1]
	if planned.OperatorState != "planned" || planned.NextAction != "initialize" || planned.TaskCounts.Total != 2 || planned.TaskCounts.Planned != 2 || planned.TaskCounts.Materialized != 0 {
		t.Fatalf("unexpected honest planned-wave projection: %+v", planned)
	}
	if !planned.ActionCapabilities.Initialize.Enabled || planned.ActionCapabilities.Launch.Enabled || !strings.Contains(planned.ActionCapabilities.Launch.DisabledReason, "Initialize") {
		t.Fatalf("unexpected planned-wave capabilities: %+v", planned.ActionCapabilities)
	}
	if planned.Backend != "codex" || planned.Model != "gpt-5.6-sol" || planned.Reasoning != "high" || len(planned.MCPServers) != 2 || planned.Tasks[0].MCPServers[0] != "gopls" {
		t.Fatalf("unexpected planned task assignments: %+v tasks=%+v", planned, planned.Tasks)
	}
	initializedPlan := response.PlannedWaves[0]
	if initializedPlan.OperatorState != "initialized" || initializedPlan.InitializedWaveID != 101 || initializedPlan.TaskCounts.Materialized != 1 || initializedPlan.Tasks[0].LocalSessionID == 0 {
		t.Fatalf("unexpected initialized-plan linkage: %+v", initializedPlan)
	}
	if initializedPlan.ActionCapabilities.Initialize.Enabled || !strings.Contains(initializedPlan.ActionCapabilities.Launch.DisabledReason, "cannot safely delegate") {
		t.Fatalf("initialized plan must not bypass governed Launch: %+v", initializedPlan.ActionCapabilities)
	}

	reviewWave := missionControlWaveByID(t, response.Waves, 101)
	if reviewWave.OperatorState != "wave_review_ready" || reviewWave.Label != "Ready for implementation review" || reviewWave.NextAction != "review_implementation" {
		t.Fatalf("unexpected implementation-review summary: %+v", reviewWave)
	}
	if reviewWave.Review.SessionID != 32 || reviewWave.Review.LocalSessionID == 0 || reviewWave.Review.State != "completed" {
		t.Fatalf("unexpected linked reviewer state: %+v", reviewWave.Review)
	}
	if reviewWave.WorkerCounts.Total != 2 || reviewWave.WorkerCounts.Terminal != 2 || reviewWave.WorkerCounts.ReviewReady != 1 || reviewWave.WorkerCounts.Completed != 1 {
		t.Fatalf("unexpected review-wave counts: %+v", reviewWave.WorkerCounts)
	}
	if len(reviewWave.Workers) != 2 || reviewWave.Workers[0].SessionID != 30 || reviewWave.Workers[0].Backend != "codex" || reviewWave.Workers[0].Model != "gpt-5.6-sol" || reviewWave.Workers[0].Reasoning != "high" || reviewWave.Workers[0].Outcome != "review_ready" {
		t.Fatalf("unexpected worker provider projection: %+v", reviewWave.Workers)
	}

	repairWave := missionControlWaveByID(t, response.Waves, 102)
	if repairWave.LegacyStatus != "review_ready" || repairWave.OperatorState != "repair_blocked" || repairWave.NextAction != "authorize_repair" {
		t.Fatalf("repair state must win over legacy review_ready: %+v", repairWave)
	}
	if repairWave.Repair.State != "required" || repairWave.Repair.WorkerID != 1005 || repairWave.Repair.SessionID != 35 || repairWave.Repair.AttemptCount != 0 {
		t.Fatalf("unexpected governed repair state: %+v", repairWave.Repair)
	}
	if repairWave.WorkerCounts.Failed != 1 || repairWave.WorkerCounts.ReviewReady != 2 || repairWave.FailurePolicyState != "repair_required" || repairWave.FailureReason == "" {
		t.Fatalf("unexpected repair failure projection: %+v", repairWave)
	}

	acceptanceWave := missionControlWaveByID(t, response.Waves, 103)
	if acceptanceWave.OperatorState != "acceptance" || acceptanceWave.NextAction != "review_acceptance" || acceptanceWave.GateState != "acceptance_review" {
		t.Fatalf("unexpected acceptance summary: %+v", acceptanceWave)
	}
	if acceptanceWave.Review.SessionID != 36 || acceptanceWave.Review.State != "completed" || acceptanceWave.Acceptance.SessionID != 37 || acceptanceWave.Acceptance.State != "completed" {
		t.Fatalf("unexpected review/acceptance linkage: review=%+v acceptance=%+v", acceptanceWave.Review, acceptanceWave.Acceptance)
	}

	for _, wave := range response.Waves {
		assertMissionControlMutationsDisabled(t, wave.ActionCapabilities)
	}
	if !strings.Contains(repairWave.ActionCapabilities.AuthorizeRepair.DisabledReason, "cannot safely delegate") {
		t.Fatalf("expected safe-delegation reason, got %+v", repairWave.ActionCapabilities.AuthorizeRepair)
	}

	var otherProject ProjectMissionControlWavesResponse
	readJSON(t, server.URL+"/api/projects/2/waves", &otherProject)
	if len(otherProject.Waves) != 1 || otherProject.Waves[0].WaveID != 201 || len(otherProject.Waves[0].Workers) != 1 || otherProject.Waves[0].Workers[0].SessionID != 40 {
		t.Fatalf("project boundary leaked or hid wave data: %+v", otherProject.Waves)
	}
	if len(otherProject.PlannedWaves) != 1 || otherProject.PlannedWaves[0].PlanID != 601 || otherProject.PlannedWaves[0].Tasks[0].MaterializedSessionID != 40 {
		t.Fatalf("project boundary leaked or hid planned-wave data: %+v", otherProject.PlannedWaves)
	}
	for _, plan := range response.PlannedWaves {
		if plan.PlanID == 601 {
			t.Fatalf("project-2 planned wave leaked into project-1 response: %+v", plan)
		}
	}
	for _, wave := range response.Waves {
		for _, worker := range wave.Workers {
			if worker.SessionID == 40 {
				t.Fatalf("project-2 worker leaked into project-1 response: %+v", worker)
			}
		}
	}
}

func TestProjectMissionControlWavesFreshnessAndReadOnlyRoute(t *testing.T) {
	repo := openFixtureRepository(t)
	defer repo.Close()

	now := time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC)
	repo.now = func() time.Time { return now }
	seedMissionControlWaves(t, repo, now)
	staleTimestamp := now.Add(-10 * time.Minute).Format(time.RFC3339Nano)
	if _, err := repo.dbWrite.Exec("UPDATE parallel_wave SET updated_at = ? WHERE project_id = 1", staleTimestamp); err != nil {
		t.Fatalf("make wave fixture stale: %v", err)
	}
	if _, err := repo.dbWrite.Exec("UPDATE parallel_wave_worker SET updated_at = ? WHERE project_id = 1", staleTimestamp); err != nil {
		t.Fatalf("make worker fixture stale: %v", err)
	}

	response, err := repo.ProjectMissionControlWaves(context.Background(), 1)
	if err != nil {
		t.Fatalf("load stale Mission Control waves: %v", err)
	}
	if response.Freshness.State != "stale" || !response.Freshness.Stale || response.Freshness.AgeSeconds != 600 {
		t.Fatalf("unexpected stale freshness: %+v", response.Freshness)
	}
	repairWave := missionControlWaveByID(t, response.Waves, 102)
	if !strings.Contains(repairWave.ActionCapabilities.AuthorizeRepair.DisabledReason, "Runtime data is stale") {
		t.Fatalf("expected stale data to suppress consequential controls: %+v", repairWave.ActionCapabilities.AuthorizeRepair)
	}

	server := httptest.NewServer(NewServer(repo))
	defer server.Close()
	request, err := http.NewRequest(http.MethodPost, server.URL+"/api/projects/1/waves", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("build mutation probe request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	httpResponse, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("probe read-only route: %v", err)
	}
	defer httpResponse.Body.Close()
	if httpResponse.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected GET-only wave route, got %d", httpResponse.StatusCode)
	}
	var failurePolicy string
	if err := repo.db.QueryRow("SELECT failure_policy_state FROM parallel_wave WHERE id = 102").Scan(&failurePolicy); err != nil {
		t.Fatalf("read wave after mutation probe: %v", err)
	}
	if failurePolicy != "repair_required" {
		t.Fatalf("read-only route changed governed state to %q", failurePolicy)
	}
}

func TestProjectMissionControlWavesWithoutRuntimeTablesIsEmpty(t *testing.T) {
	repo := openFixtureRepository(t)
	defer repo.Close()

	now := time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC)
	repo.now = func() time.Time { return now }
	response, err := repo.ProjectMissionControlWaves(context.Background(), 1)
	if err != nil {
		t.Fatalf("load empty Mission Control waves: %v", err)
	}
	if response.ProjectID != 1 || response.Waves == nil || len(response.Waves) != 0 {
		t.Fatalf("unexpected empty response: %+v", response)
	}
	if response.Freshness.State != "empty" || response.Freshness.Stale || response.Freshness.Reason == "" {
		t.Fatalf("unexpected empty freshness: %+v", response.Freshness)
	}
}

func seedMissionControlWaves(t *testing.T, repo *Repository, now time.Time) {
	t.Helper()
	repo.plannedWaveInitializerAvailable = func(projectRecord) bool { return true }
	statements := []string{
		"ALTER TABLE session ADD COLUMN parallel_wave_id TEXT NOT NULL DEFAULT ''",
		`CREATE TABLE parallel_wave (
			id INTEGER PRIMARY KEY,
			project_id INTEGER NOT NULL,
			status TEXT NOT NULL,
			phase TEXT NOT NULL,
			gate_state TEXT NOT NULL,
			control_state TEXT NOT NULL,
			control_reason TEXT NOT NULL,
			failure_policy_state TEXT NOT NULL,
			repair_worker_id INTEGER,
			repair_attempt_count INTEGER NOT NULL,
			acceptance_session_id INTEGER,
			failure_reason TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			launched_at TEXT,
			completed_at TEXT
		)`,
		`CREATE TABLE parallel_wave_worker (
			id INTEGER PRIMARY KEY,
			wave_id INTEGER NOT NULL,
			project_id INTEGER NOT NULL,
			session_id INTEGER NOT NULL,
			status TEXT NOT NULL,
			terminal_outcome TEXT NOT NULL,
			failure_reason TEXT NOT NULL,
			retry_next_eligible_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE planned_wave (
			id INTEGER PRIMARY KEY,
			project_id INTEGER NOT NULL,
			title TEXT NOT NULL,
			status TEXT NOT NULL,
			reason TEXT NOT NULL,
			base_ref TEXT NOT NULL,
			worktree_root TEXT NOT NULL,
			initialized_wave_id INTEGER,
			failure_reason TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			initialized_at TEXT
		)`,
		`CREATE TABLE planned_wave_task (
			id INTEGER PRIMARY KEY,
			planned_wave_id INTEGER NOT NULL,
			project_id INTEGER NOT NULL,
			task_key TEXT NOT NULL,
			position INTEGER NOT NULL,
			task_description TEXT NOT NULL,
			dependencies TEXT NOT NULL,
			cli_backend TEXT NOT NULL DEFAULT 'codex',
			cli_model TEXT NOT NULL DEFAULT '',
			cli_reasoning TEXT NOT NULL DEFAULT '',
			mcp_server_names TEXT NOT NULL DEFAULT '[]',
			materialized_session_id INTEGER
		)`,
	}
	for _, statement := range statements {
		if _, err := repo.dbWrite.Exec(statement); err != nil {
			t.Fatalf("create Mission Control fixture with %q: %v", statement, err)
		}
	}

	sessions := []struct {
		id       int
		project  int
		task     string
		status   string
		backend  string
		model    string
		reason   string
		waveLink string
	}{
		{30, 1, "Review-ready Codex worker", "review", "codex", "gpt-5.6-sol", "high", ""},
		{31, 1, "Completed Droid worker", "completed", "droid", "gemini-3.1-pro", "high", ""},
		{32, 1, "Implementation reviewer", "completed", "codex", "gpt-5.6-sol", "high", "parallel-wave-review:101"},
		{33, 1, "Repair cohort worker one", "review", "codex", "gpt-5.6-sol", "high", ""},
		{34, 1, "Repair cohort worker two", "review", "droid", "gpt-5.4", "medium", ""},
		{35, 1, "Failed repair candidate", "review", "codex", "gpt-5.6-sol", "ultra", ""},
		{36, 1, "Acceptance implementation reviewer", "completed", "codex", "gpt-5.6-sol", "high", "parallel-wave-review:103"},
		{37, 1, "Acceptance session", "completed", "codex", "gpt-5.6-sol", "high", ""},
		{38, 1, "Accepted implementation worker", "completed", "codex", "gpt-5.6-sol", "high", ""},
		{40, 2, "Other project worker", "in_progress", "claude", "sonnet", "high", ""},
	}
	for _, session := range sessions {
		if _, err := repo.dbWrite.Exec(`
			INSERT INTO session (id, project_id, task_description, status, cli_backend, cli_model, cli_reasoning, parallel_wave_id)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			session.id, session.project, session.task, session.status, session.backend, session.model, session.reason, session.waveLink,
		); err != nil {
			t.Fatalf("seed Mission Control session %d: %v", session.id, err)
		}
	}

	createdAt := now.Add(-time.Hour).Format(time.RFC3339Nano)
	insertWave := func(id int, projectID int, legacyStatus string, phase string, gate string, control string, controlReason string, failurePolicy string, repairWorkerID any, repairAttempts int, acceptanceSessionID any, failureReason string, updatedAt time.Time) {
		t.Helper()
		if _, err := repo.dbWrite.Exec(`
			INSERT INTO parallel_wave (
				id, project_id, status, phase, gate_state, control_state, control_reason,
				failure_policy_state, repair_worker_id, repair_attempt_count, acceptance_session_id,
				failure_reason, created_at, updated_at, launched_at, completed_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL)`,
			id, projectID, legacyStatus, phase, gate, control, controlReason,
			failurePolicy, repairWorkerID, repairAttempts, acceptanceSessionID,
			failureReason, createdAt, updatedAt.Format(time.RFC3339Nano), createdAt,
		); err != nil {
			t.Fatalf("seed Mission Control wave %d: %v", id, err)
		}
	}
	insertWave(101, 1, "review_ready", "implementation", "implementation_review", "active", "", "passed", nil, 0, nil, "", now.Add(-2*time.Minute))
	insertWave(102, 1, "review_ready", "implementation", "implementation_repair", "active", "", "repair_required", 1005, 0, nil, "parent handoff uncommitted", now.Add(-time.Minute))
	insertWave(103, 1, "review_ready", "acceptance", "acceptance_review", "active", "", "passed", nil, 0, 37, "", now.Add(-30*time.Second))
	insertWave(201, 2, "running", "implementation", "none", "active", "", "none", nil, 0, nil, "", now.Add(-10*time.Second))
	if _, err := repo.dbWrite.Exec(`
		INSERT INTO planned_wave (
			id, project_id, title, status, reason, base_ref, worktree_root,
			initialized_wave_id, failure_reason, created_at, updated_at, initialized_at
		) VALUES
			(501, 1, 'Future Mission Control work', 'planned', 'grand checklist', 'HEAD', '', NULL, '', ?, ?, NULL),
			(502, 1, 'Initialized Mission Control work', 'initialized', '', 'HEAD', '', 101, '', ?, ?, ?),
			(601, 2, 'Other project plan', 'initializing', '', 'HEAD', '', NULL, '', ?, ?, NULL)`,
		createdAt, now.Add(-20*time.Second).Format(time.RFC3339Nano),
		createdAt, now.Add(-15*time.Second).Format(time.RFC3339Nano), now.Add(-15*time.Second).Format(time.RFC3339Nano),
		createdAt, now.Add(-5*time.Second).Format(time.RFC3339Nano),
	); err != nil {
		t.Fatalf("seed Mission Control planned waves: %v", err)
	}
	if _, err := repo.dbWrite.Exec(`
		INSERT INTO planned_wave_task (
			id, planned_wave_id, project_id, task_key, position, task_description,
			dependencies, cli_backend, cli_model, cli_reasoning,
			mcp_server_names, materialized_session_id
		) VALUES
			(5001, 501, 1, 'backend', 1, 'Future backend', '[]', 'codex', 'gpt-5.6-sol', 'high', '["gopls"]', NULL),
			(5002, 501, 1, 'frontend', 2, 'Future frontend', '["backend"]', 'codex', 'gpt-5.6-sol', 'high', '["sqlite"]', NULL),
			(5003, 502, 1, 'accepted', 1, 'Already initialized', '[]', 'codex', 'gpt-5.6-sol', 'high', '[]', 30),
			(6001, 601, 2, 'other', 1, 'Other project task', '[]', 'codex', 'gpt-5.6-sol', 'high', '[]', 40)`); err != nil {
		t.Fatalf("seed Mission Control planned tasks: %v", err)
	}

	workers := []struct {
		id            int
		waveID        int
		projectID     int
		sessionID     int
		status        string
		outcome       string
		failureReason string
		updatedAt     time.Time
	}{
		{1001, 101, 1, 30, "review_ready", "review_ready", "", now.Add(-2 * time.Minute)},
		{1002, 101, 1, 31, "completed", "completed", "", now.Add(-2 * time.Minute)},
		{1003, 102, 1, 33, "review_ready", "review_ready", "", now.Add(-time.Minute)},
		{1004, 102, 1, 34, "review_ready", "review_ready", "", now.Add(-time.Minute)},
		{1005, 102, 1, 35, "failed", "failed", "parent handoff uncommitted", now.Add(-time.Minute)},
		{1006, 103, 1, 38, "completed", "completed", "", now.Add(-45 * time.Second)},
		{2001, 201, 2, 40, "running", "", "", now},
	}
	for _, worker := range workers {
		if _, err := repo.dbWrite.Exec(`
			INSERT INTO parallel_wave_worker (
				id, wave_id, project_id, session_id, status, terminal_outcome,
				failure_reason, retry_next_eligible_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, '', ?)`,
			worker.id, worker.waveID, worker.projectID, worker.sessionID, worker.status,
			worker.outcome, worker.failureReason, worker.updatedAt.Format(time.RFC3339Nano),
		); err != nil {
			t.Fatalf("seed Mission Control worker %d: %v", worker.id, err)
		}
	}
}

func missionControlWaveByID(t *testing.T, waves []MissionControlWave, waveID int) MissionControlWave {
	t.Helper()
	for _, wave := range waves {
		if wave.WaveID == waveID {
			return wave
		}
	}
	t.Fatalf("wave %d not found in %+v", waveID, waves)
	return MissionControlWave{}
}

func assertMissionControlMutationsDisabled(t *testing.T, capabilities MissionControlWaveActionCapabilities) {
	t.Helper()
	all := []MissionControlActionCapability{
		capabilities.Launch,
		capabilities.Wait,
		capabilities.AuthorizeRepair,
		capabilities.Pause,
		capabilities.Resume,
		capabilities.TransitionToAcceptance,
		capabilities.Complete,
		capabilities.Cleanup,
	}
	for index, capability := range all {
		if capability.Enabled || capability.DisabledReason == "" {
			t.Fatalf("capability %d must be explicitly disabled with a reason: %+v", index, capability)
		}
	}
}
