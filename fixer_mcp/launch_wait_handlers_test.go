package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLaunchExplicitNetrunner_Disabled(t *testing.T) {
	callResult, _, err := LaunchExplicitNetrunner(context.Background(), nil, LaunchExplicitNetrunnerInput{
		SessionId:      1,
		FixerSessionId: "fixer-live-123",
	})
	if err == nil {
		t.Fatal("expected disabled error")
	}
	if callResult == nil || !callResult.IsError {
		t.Fatal("expected MCP error result")
	}
	if !strings.Contains(err.Error(), "temporarily disabled") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLaunchExplicitNetrunner_DisabledWithoutAssignedMcpSet(t *testing.T) {
	callResult, _, err := LaunchExplicitNetrunner(context.Background(), nil, LaunchExplicitNetrunnerInput{
		SessionId: 1,
	})
	if err == nil {
		t.Fatal("expected disabled error")
	}
	if callResult == nil || !callResult.IsError {
		t.Fatal("expected MCP error result")
	}
	if !strings.Contains(err.Error(), "temporarily disabled") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNormalizeCliBackend_AcceptsAntigravityAliases(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "canonical", raw: "antigravity", want: "antigravity"},
		{name: "alias", raw: "agy", want: "antigravity"},
		{name: "case and whitespace", raw: " AGY ", want: "antigravity"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeCliBackend(tt.raw)
			if err != nil {
				t.Fatalf("normalizeCliBackend(%q) failed: %v", tt.raw, err)
			}
			if got != tt.want {
				t.Fatalf("normalizeCliBackend(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestResolveSessionLaunchConfig_PersistsRequestedAntigravityLaunchConfig(t *testing.T) {
	tests := []struct {
		name             string
		requestedBackend string
	}{
		{name: "canonical", requestedBackend: "antigravity"},
		{name: "alias", requestedBackend: "agy"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalDB := db
			defer func() {
				db = originalDB
			}()

			testDB := setupGetProjectsTestDB(t)
			defer func() {
				_ = testDB.Close()
			}()

			db = testDB

			config, err := resolveSessionLaunchConfig(1, 1, tt.requestedBackend, "gemini-3.5-flash", "medium")
			if err != nil {
				t.Fatalf("resolveSessionLaunchConfig failed: %v", err)
			}
			if config.Backend != "antigravity" || config.Model != "gemini-3.5-flash" || config.Reasoning != "medium" {
				t.Fatalf("unexpected launch config: %+v", config)
			}

			var storedBackend string
			var storedModel string
			var storedReasoning string
			if err := testDB.QueryRow(
				"SELECT cli_backend, cli_model, cli_reasoning FROM session WHERE id = 1",
			).Scan(&storedBackend, &storedModel, &storedReasoning); err != nil {
				t.Fatalf("read persisted launch config: %v", err)
			}
			if storedBackend != "antigravity" || storedModel != "gemini-3.5-flash" || storedReasoning != "medium" {
				t.Fatalf("unexpected persisted launch config: backend=%q model=%q reasoning=%q", storedBackend, storedModel, storedReasoning)
			}
		})
	}
}

func TestResolveSessionLaunchConfig_PersistsRequestedDroidLaunchConfig(t *testing.T) {
	originalDB := db
	defer func() {
		db = originalDB
	}()

	testDB := setupGetProjectsTestDB(t)
	defer func() {
		_ = testDB.Close()
	}()

	db = testDB

	config, err := resolveSessionLaunchConfig(1, 1, "droid", "glm-5.1", "medium")
	if err != nil {
		t.Fatalf("resolveSessionLaunchConfig failed: %v", err)
	}
	if config.Backend != "droid" || config.Model != "glm-5.1" || config.Reasoning != "medium" {
		t.Fatalf("unexpected launch config: %+v", config)
	}

	var storedBackend string
	var storedModel string
	var storedReasoning string
	if err := testDB.QueryRow(
		"SELECT cli_backend, cli_model, cli_reasoning FROM session WHERE id = 1",
	).Scan(&storedBackend, &storedModel, &storedReasoning); err != nil {
		t.Fatalf("read persisted launch config: %v", err)
	}
	if storedBackend != "droid" || storedModel != "glm-5.1" || storedReasoning != "medium" {
		t.Fatalf("unexpected persisted launch config: backend=%q model=%q reasoning=%q", storedBackend, storedModel, storedReasoning)
	}
}

func TestResolveSessionLaunchConfig_PersistsRequestedJunieLaunchConfig(t *testing.T) {
	originalDB := db
	defer func() {
		db = originalDB
	}()

	testDB := setupGetProjectsTestDB(t)
	defer func() {
		_ = testDB.Close()
	}()

	db = testDB

	config, err := resolveSessionLaunchConfig(1, 1, "junie", "glm-5.1", "default")
	if err != nil {
		t.Fatalf("resolveSessionLaunchConfig failed: %v", err)
	}
	if config.Backend != "junie" || config.Model != "glm-5.1" || config.Reasoning != "default" {
		t.Fatalf("unexpected launch config: %+v", config)
	}

	var storedBackend string
	var storedModel string
	var storedReasoning string
	if err := testDB.QueryRow(
		"SELECT cli_backend, cli_model, cli_reasoning FROM session WHERE id = 1",
	).Scan(&storedBackend, &storedModel, &storedReasoning); err != nil {
		t.Fatalf("read persisted launch config: %v", err)
	}
	if storedBackend != "junie" || storedModel != "glm-5.1" || storedReasoning != "default" {
		t.Fatalf("unexpected persisted launch config: backend=%q model=%q reasoning=%q", storedBackend, storedModel, storedReasoning)
	}
}

func TestResolveSessionLaunchConfig_DefaultsJunieReasoningToDefault(t *testing.T) {
	originalDB := db
	defer func() {
		db = originalDB
	}()

	testDB := setupGetProjectsTestDB(t)
	defer func() {
		_ = testDB.Close()
	}()

	db = testDB

	config, err := resolveSessionLaunchConfig(1, 1, "junie", "kimi-k2.6", "")
	if err != nil {
		t.Fatalf("resolveSessionLaunchConfig failed: %v", err)
	}
	if config.Backend != "junie" || config.Model != "kimi-k2.6" || config.Reasoning != "default" {
		t.Fatalf("unexpected launch config: %+v", config)
	}

	var storedBackend string
	var storedModel string
	var storedReasoning string
	if err := testDB.QueryRow(
		"SELECT cli_backend, cli_model, cli_reasoning FROM session WHERE id = 1",
	).Scan(&storedBackend, &storedModel, &storedReasoning); err != nil {
		t.Fatalf("read persisted launch config: %v", err)
	}
	if storedBackend != "junie" || storedModel != "kimi-k2.6" || storedReasoning != "default" {
		t.Fatalf("unexpected persisted launch config: backend=%q model=%q reasoning=%q", storedBackend, storedModel, storedReasoning)
	}
}

func TestResolveSessionLaunchConfig_DefaultsJunieReasoningAfterPendingBackendSwitch(t *testing.T) {
	originalDB := db
	defer func() {
		db = originalDB
	}()

	testDB := setupGetProjectsTestDB(t)
	defer func() {
		_ = testDB.Close()
	}()

	if _, err := testDB.Exec("UPDATE session SET cli_backend = 'codex', cli_model = 'gpt-5.5', cli_reasoning = 'high' WHERE id = 1"); err != nil {
		t.Fatalf("seed pending codex launch config: %v", err)
	}

	db = testDB

	config, err := resolveSessionLaunchConfig(1, 1, "junie", "glm-5.1", "")
	if err != nil {
		t.Fatalf("resolveSessionLaunchConfig failed: %v", err)
	}
	if config.Backend != "junie" || config.Model != "glm-5.1" || config.Reasoning != "default" {
		t.Fatalf("unexpected launch config: %+v", config)
	}

	var storedBackend string
	var storedModel string
	var storedReasoning string
	if err := testDB.QueryRow(
		"SELECT cli_backend, cli_model, cli_reasoning FROM session WHERE id = 1",
	).Scan(&storedBackend, &storedModel, &storedReasoning); err != nil {
		t.Fatalf("read persisted launch config: %v", err)
	}
	if storedBackend != "junie" || storedModel != "glm-5.1" || storedReasoning != "default" {
		t.Fatalf("unexpected persisted launch config: backend=%q model=%q reasoning=%q", storedBackend, storedModel, storedReasoning)
	}
}

func TestResolveSessionLaunchConfig_RejectsUnsupportedBackend(t *testing.T) {
	originalDB := db
	defer func() {
		db = originalDB
	}()

	testDB := setupGetProjectsTestDB(t)
	defer func() {
		_ = testDB.Close()
	}()

	db = testDB

	_, err := resolveSessionLaunchConfig(1, 1, "spaceconsole", "gemini-3.5-flash", "medium")
	if err == nil {
		t.Fatal("expected unsupported backend error")
	}
	if !strings.Contains(err.Error(), `unsupported backend "spaceconsole"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveSessionLaunchConfig_RejectsUnsupportedDroidModel(t *testing.T) {
	originalDB := db
	defer func() {
		db = originalDB
	}()

	testDB := setupGetProjectsTestDB(t)
	defer func() {
		_ = testDB.Close()
	}()

	db = testDB

	_, err := resolveSessionLaunchConfig(1, 1, "droid", "gpt-5.3-codex", "medium")
	if err == nil {
		t.Fatal("expected unsupported droid model error")
	}
	if !strings.Contains(err.Error(), "supported models: kimi-k2.6, glm-5.1") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveSessionLaunchConfig_RejectsUnsupportedJunieModel(t *testing.T) {
	originalDB := db
	defer func() {
		db = originalDB
	}()

	testDB := setupGetProjectsTestDB(t)
	defer func() {
		_ = testDB.Close()
	}()

	db = testDB

	_, err := resolveSessionLaunchConfig(1, 1, "junie", "gpt-5.3-codex", "default")
	if err == nil {
		t.Fatal("expected unsupported junie model error")
	}
	if !strings.Contains(err.Error(), `unsupported junie model "gpt-5.3-codex"`) {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(err.Error(), "supported models: kimi-k2.6, glm-5.1") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveSessionLaunchConfig_RejectsUnsupportedJunieReasoning(t *testing.T) {
	originalDB := db
	defer func() {
		db = originalDB
	}()

	testDB := setupGetProjectsTestDB(t)
	defer func() {
		_ = testDB.Close()
	}()

	db = testDB

	_, err := resolveSessionLaunchConfig(1, 1, "junie", "glm-5.1", "medium")
	if err == nil {
		t.Fatal("expected unsupported junie reasoning error")
	}
	if !strings.Contains(err.Error(), `unsupported junie reasoning "medium"`) {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(err.Error(), "supported reasoning values: default") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveSessionLaunchConfig_NormalizesDroidModelAliases(t *testing.T) {
	originalDB := db
	defer func() {
		db = originalDB
	}()

	testDB := setupGetProjectsTestDB(t)
	defer func() {
		_ = testDB.Close()
	}()

	db = testDB

	config, err := resolveSessionLaunchConfig(1, 1, "droid", "Z.AI GLM-5.1", "high")
	if err != nil {
		t.Fatalf("resolveSessionLaunchConfig failed: %v", err)
	}
	if config.Backend != "droid" || config.Model != "glm-5.1" || config.Reasoning != "high" {
		t.Fatalf("unexpected launch config: %+v", config)
	}

	var storedModel string
	if err := testDB.QueryRow("SELECT cli_model FROM session WHERE id = 1").Scan(&storedModel); err != nil {
		t.Fatalf("read persisted model: %v", err)
	}
	if storedModel != "glm-5.1" {
		t.Fatalf("expected public persisted model alias, got %q", storedModel)
	}
}

func TestResolveSessionLaunchConfig_NormalizesDroidKimiAliases(t *testing.T) {
	originalDB := db
	defer func() {
		db = originalDB
	}()

	testDB := setupGetProjectsTestDB(t)
	defer func() {
		_ = testDB.Close()
	}()

	db = testDB

	config, err := resolveSessionLaunchConfig(1, 1, "droid", "kimi", "high")
	if err != nil {
		t.Fatalf("resolveSessionLaunchConfig failed: %v", err)
	}
	if config.Backend != "droid" || config.Model != "kimi-k2.6" || config.Reasoning != "high" {
		t.Fatalf("unexpected launch config: %+v", config)
	}
}

func TestResolveSessionLaunchConfig_AllowsPendingExplicitModelOverride(t *testing.T) {
	originalDB := db
	defer func() {
		db = originalDB
	}()

	testDB := setupGetProjectsTestDB(t)
	defer func() {
		_ = testDB.Close()
	}()

	if _, err := testDB.Exec("UPDATE session SET cli_backend = 'droid', cli_model = 'glm-5.1', cli_reasoning = 'none' WHERE id = 1"); err != nil {
		t.Fatalf("seed stale pending launch config: %v", err)
	}

	db = testDB

	config, err := resolveSessionLaunchConfig(1, 1, "droid", "custom:GLM-5.1-[Z.AI]-0", "high")
	if err != nil {
		t.Fatalf("resolveSessionLaunchConfig failed: %v", err)
	}
	if config.Backend != "droid" || config.Model != "glm-5.1" || config.Reasoning != "high" {
		t.Fatalf("unexpected launch config: %+v", config)
	}

	var storedBackend string
	var storedModel string
	var storedReasoning string
	if err := testDB.QueryRow(
		"SELECT cli_backend, cli_model, cli_reasoning FROM session WHERE id = 1",
	).Scan(&storedBackend, &storedModel, &storedReasoning); err != nil {
		t.Fatalf("read persisted launch config: %v", err)
	}
	if storedBackend != "droid" || storedModel != "glm-5.1" || storedReasoning != "high" {
		t.Fatalf("unexpected persisted launch config: backend=%q model=%q reasoning=%q", storedBackend, storedModel, storedReasoning)
	}
}

func TestResolveSessionLaunchConfig_DefaultsDroidToHumanModelAlias(t *testing.T) {
	originalDB := db
	defer func() {
		db = originalDB
	}()

	testDB := setupGetProjectsTestDB(t)
	defer func() {
		_ = testDB.Close()
	}()

	db = testDB

	config, err := resolveSessionLaunchConfig(1, 1, "droid", "", "")
	if err != nil {
		t.Fatalf("resolveSessionLaunchConfig failed: %v", err)
	}
	if config.Backend != "droid" || config.Model != "kimi-k2.6" || config.Reasoning != "high" {
		t.Fatalf("unexpected launch config: %+v", config)
	}

	var storedBackend string
	var storedModel string
	var storedReasoning string
	if err := testDB.QueryRow(
		"SELECT cli_backend, cli_model, cli_reasoning FROM session WHERE id = 1",
	).Scan(&storedBackend, &storedModel, &storedReasoning); err != nil {
		t.Fatalf("read persisted launch config: %v", err)
	}
	if storedBackend != "droid" || storedModel != "kimi-k2.6" || storedReasoning != "high" {
		t.Fatalf("unexpected persisted launch config: backend=%q model=%q reasoning=%q", storedBackend, storedModel, storedReasoning)
	}
}

func TestResolveSessionLaunchConfig_DefaultsCodexToGpt54Xhigh(t *testing.T) {
	originalDB := db
	defer func() {
		db = originalDB
	}()

	testDB := setupGetProjectsTestDB(t)
	defer func() {
		_ = testDB.Close()
	}()

	db = testDB

	config, err := resolveSessionLaunchConfig(1, 1, "", "", "")
	if err != nil {
		t.Fatalf("resolveSessionLaunchConfig failed: %v", err)
	}
	if config.Backend != "codex" || config.Model != "gpt-5.5" || config.Reasoning != "high" {
		t.Fatalf("unexpected default launch config: %+v", config)
	}

	var storedBackend string
	var storedModel string
	var storedReasoning string
	if err := testDB.QueryRow(
		"SELECT cli_backend, cli_model, cli_reasoning FROM session WHERE id = 1",
	).Scan(&storedBackend, &storedModel, &storedReasoning); err != nil {
		t.Fatalf("read persisted launch config: %v", err)
	}
	if storedBackend != "codex" || storedModel != "gpt-5.5" || storedReasoning != "high" {
		t.Fatalf("unexpected persisted default launch config: backend=%q model=%q reasoning=%q", storedBackend, storedModel, storedReasoning)
	}
}

func TestResolveSessionLaunchConfig_RejectsBackendSwitchAfterLaunch(t *testing.T) {
	originalDB := db
	defer func() {
		db = originalDB
	}()

	testDB := setupGetProjectsTestDB(t)
	defer func() {
		_ = testDB.Close()
	}()

	if _, err := testDB.Exec("INSERT INTO session_external_link (session_id, backend, external_session_id) VALUES (1, 'codex', 'legacy-codex-1')"); err != nil {
		t.Fatalf("seed session_external_link: %v", err)
	}

	db = testDB

	_, err := resolveSessionLaunchConfig(1, 1, "droid", "glm-5.1", "medium")
	if err == nil {
		t.Fatal("expected backend switch rejection")
	}
	if !strings.Contains(err.Error(), "bound to backend") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFetchSessionExternalID_FallsBackToLegacyCodexLink(t *testing.T) {
	originalDB := db
	defer func() {
		db = originalDB
	}()

	testDB := setupGetProjectsTestDB(t)
	defer func() {
		_ = testDB.Close()
	}()

	if _, err := testDB.Exec("INSERT INTO session_codex_link (session_id, codex_session_id) VALUES (1, 'legacy-codex-fallback')"); err != nil {
		t.Fatalf("seed session_codex_link: %v", err)
	}

	db = testDB

	externalSessionID, err := fetchSessionExternalID(1, "codex")
	if err != nil {
		t.Fatalf("fetchSessionExternalID failed: %v", err)
	}
	if externalSessionID != "legacy-codex-fallback" {
		t.Fatalf("expected legacy fallback id, got %q", externalSessionID)
	}
}

func TestWaitForNetrunnerSession_ReturnsReviewReadyMetadata(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
	}()

	testDB := setupGetProjectsTestDB(t)
	defer func() {
		_ = testDB.Close()
	}()

	if _, err := testDB.Exec("UPDATE session SET status = 'review', report = 'Worker finished cleanly' WHERE id = 1"); err != nil {
		t.Fatalf("seed review session: %v", err)
	}
	if _, err := testDB.Exec("INSERT INTO doc_proposal (project_id, session_id, status, proposed_content, proposed_doc_type) VALUES (1, 1, 'pending', 'Doc delta', 'documentation')"); err != nil {
		t.Fatalf("seed doc proposal: %v", err)
	}
	if _, err := testDB.Exec("INSERT INTO session_codex_link (session_id, codex_session_id) VALUES (1, 'runner-123')"); err != nil {
		t.Fatalf("seed session_codex_link: %v", err)
	}
	if _, err := testDB.Exec("INSERT INTO worker_process (project_id, session_id, pid, launch_epoch, status) VALUES (1, 1, ?, 0, 'running')", os.Getpid()); err != nil {
		t.Fatalf("seed live worker_process: %v", err)
	}

	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1

	callResult, out, err := WaitForNetrunnerSession(context.Background(), nil, WaitForNetrunnerSessionInput{
		SessionId:           1,
		TimeoutSeconds:      2,
		PollIntervalSeconds: 1,
	})
	if err != nil {
		t.Fatalf("wait_for_netrunner_session failed: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil call result on success, got %+v", callResult)
	}
	if out.Result.TerminalCondition != "review_ready" {
		t.Fatalf("expected review_ready, got %+v", out.Result)
	}
	if !out.Result.Terminal || out.Result.TimedOut {
		t.Fatalf("expected terminal non-timeout result, got %+v", out.Result)
	}
	if out.Result.Report != "Worker finished cleanly" {
		t.Fatalf("expected report in result, got %+v", out.Result)
	}
	if len(out.Result.ProposalIds) != 1 || out.Result.ProposalIds[0] != 1 {
		t.Fatalf("expected local proposal id [1], got %+v", out.Result.ProposalIds)
	}
	if out.Result.CodexSessionId != "runner-123" {
		t.Fatalf("expected codex session id runner-123, got %+v", out.Result)
	}
}

func TestWaitForNetrunnerSession_ReturnsWorkerExitedDiagnosticAndBlocksAutonomousRun(t *testing.T) {
	tests := []struct {
		name      string
		backend   string
		writeLogs bool
	}{
		{name: "droid with logs", backend: "droid", writeLogs: true},
		{name: "antigravity without logs", backend: "antigravity", writeLogs: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalDB := db
			originalRole := authorizedRole
			originalProjectID := authorizedProjectId
			defer func() {
				db = originalDB
				authorizedRole = originalRole
				authorizedProjectId = originalProjectID
			}()

			testDB := setupGetProjectsTestDB(t)
			defer func() {
				_ = testDB.Close()
			}()

			normalizedProjectCWD, err := normalizeProjectCWD(testProjectCWD)
			if err != nil {
				t.Fatalf("normalize project cwd: %v", err)
			}
			logDir := filepath.Join(normalizedProjectCWD, ".codex", "headless_netrunner_logs")
			if err := os.RemoveAll(logDir); err != nil {
				t.Fatalf("clear log dir: %v", err)
			}
			if err := os.MkdirAll(logDir, 0o755); err != nil {
				t.Fatalf("create log dir: %v", err)
			}
			headlessLogPath := filepath.Join(logDir, "session-1-"+tt.backend+"-999.log")
			launcherLogPath := filepath.Join(logDir, "session-1-launcher-999.log")
			if tt.writeLogs {
				if err := os.WriteFile(headlessLogPath, []byte("headless died\n"), 0o644); err != nil {
					t.Fatalf("write headless log: %v", err)
				}
				if err := os.WriteFile(launcherLogPath, []byte("launcher saw exit\n"), 0o644); err != nil {
					t.Fatalf("write launcher log: %v", err)
				}
			}

			if _, err := testDB.Exec(
				"UPDATE session SET status = 'in_progress', cli_backend = ?, cli_model = 'test-model', cli_reasoning = 'medium' WHERE id = 1",
				tt.backend,
			); err != nil {
				t.Fatalf("seed in-progress session: %v", err)
			}
			if _, err := testDB.Exec("INSERT INTO worker_process (project_id, session_id, pid, launch_epoch, status) VALUES (1, 1, 0, 7, 'running')"); err != nil {
				t.Fatalf("seed dead worker_process: %v", err)
			}

			db = testDB
			authorizedRole = "fixer"
			authorizedProjectId = 1

			callResult, out, err := WaitForNetrunnerSession(context.Background(), nil, WaitForNetrunnerSessionInput{
				SessionId:           1,
				TimeoutSeconds:      30,
				PollIntervalSeconds: 1,
			})
			if err != nil {
				t.Fatalf("wait_for_netrunner_session failed: %v", err)
			}
			if callResult != nil {
				t.Fatalf("expected nil call result on terminal worker diagnostic, got %+v", callResult)
			}
			if !out.Result.Terminal || out.Result.TimedOut || out.Result.TerminalCondition != "worker_process_exited" {
				t.Fatalf("expected worker_process_exited terminal result, got %+v", out.Result)
			}
			if out.Result.Backend != tt.backend {
				t.Fatalf("expected backend %q in result, got %+v", tt.backend, out.Result)
			}
			if out.Result.WorkerProcess == nil {
				t.Fatalf("expected worker diagnostic, got %+v", out.Result)
			}
			if out.Result.WorkerProcess.PID != 0 || out.Result.WorkerProcess.ProcessStatus != workerStatusExited || out.Result.WorkerProcess.StopReason != "process exited" || out.Result.WorkerProcess.Alive {
				t.Fatalf("unexpected worker diagnostic: %+v", out.Result.WorkerProcess)
			}
			if tt.writeLogs {
				if out.Result.WorkerProcess.HeadlessLogPath != headlessLogPath || out.Result.WorkerProcess.LauncherLogPath != launcherLogPath {
					t.Fatalf("expected log paths in diagnostic, got %+v", out.Result.WorkerProcess)
				}
				if out.Result.WorkerProcess.HeadlessLogMtime == "" {
					t.Fatalf("expected headless log mtime, got %+v", out.Result.WorkerProcess)
				}
			} else if out.Result.WorkerProcess.HeadlessLogPath != "" || out.Result.WorkerProcess.LauncherLogPath != "" || out.Result.WorkerProcess.HeadlessLogMtime != "" {
				t.Fatalf("expected explicit blank log diagnostics, got %+v", out.Result.WorkerProcess)
			}

			var workerStatus string
			var workerStopReason string
			if err := testDB.QueryRow("SELECT status, COALESCE(stop_reason, '') FROM worker_process WHERE session_id = 1").Scan(&workerStatus, &workerStopReason); err != nil {
				t.Fatalf("read worker process row: %v", err)
			}
			if workerStatus != workerStatusExited || workerStopReason != "process exited" {
				t.Fatalf("expected refreshed exited worker row, got status=%q stop_reason=%q", workerStatus, workerStopReason)
			}

			var runState string
			var runSessionID int
			var blocker string
			var evidence string
			if err := testDB.QueryRow(
				"SELECT state, COALESCE(session_id, 0), COALESCE(blocker, ''), COALESCE(evidence, '') FROM autonomous_run_status WHERE project_id = 1",
			).Scan(&runState, &runSessionID, &blocker, &evidence); err != nil {
				t.Fatalf("read autonomous status: %v", err)
			}
			if runState != "blocked" || runSessionID != 1 || blocker != "worker process exited" {
				t.Fatalf("expected blocked autonomous status, got state=%q session=%d blocker=%q evidence=%q", runState, runSessionID, blocker, evidence)
			}
			if !strings.Contains(evidence, "worker_process_exited") || !strings.Contains(evidence, `"pid":0`) {
				t.Fatalf("expected worker-exit evidence, got %q", evidence)
			}
		})
	}
}

func TestWaitForNetrunnerSession_RejectsMalformedReviewWithoutReportOrProposal(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
	}()

	testDB := setupGetProjectsTestDB(t)
	defer func() {
		_ = testDB.Close()
	}()

	if _, err := testDB.Exec("UPDATE session SET status = 'review', report = '' WHERE id = 1"); err != nil {
		t.Fatalf("seed malformed review session: %v", err)
	}

	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1

	callResult, _, err := WaitForNetrunnerSession(context.Background(), nil, WaitForNetrunnerSessionInput{
		SessionId:           1,
		TimeoutSeconds:      2,
		PollIntervalSeconds: 1,
	})
	if err == nil {
		t.Fatal("expected malformed review error")
	}
	if callResult == nil || !callResult.IsError {
		t.Fatal("expected MCP error result")
	}
	if !strings.Contains(err.Error(), "reached review without final report and doc-impact proposal") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(err.Error(), "cannot be produced by complete_task") {
		t.Fatalf("expected completion-path diagnosis, got: %v", err)
	}
}

func TestWaitForNetrunnerSession_TimesOutBeforeWorkerProgress(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
	}()

	testDB := setupGetProjectsTestDB(t)
	defer func() {
		_ = testDB.Close()
	}()

	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1

	callResult, out, err := WaitForNetrunnerSession(context.Background(), nil, WaitForNetrunnerSessionInput{
		SessionId:           1,
		TimeoutSeconds:      1,
		PollIntervalSeconds: 1,
	})
	if err != nil {
		t.Fatalf("wait_for_netrunner_session timeout path failed: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil call result on success, got %+v", callResult)
	}
	if !out.Result.TimedOut || out.Result.Terminal {
		t.Fatalf("expected timeout without terminal state, got %+v", out.Result)
	}
	if out.Result.TerminalCondition != "timed_out" {
		t.Fatalf("expected timed_out condition, got %+v", out.Result)
	}
	if out.Result.SessionStatus != "pending" {
		t.Fatalf("expected pending timeout status, got %+v", out.Result)
	}
}

func TestWaitForNetrunnerSessions_Disabled(t *testing.T) {
	callResult, _, err := WaitForNetrunnerSessions(context.Background(), nil, WaitForNetrunnerSessionsInput{
		SessionIds:          []int{2, 1},
		TimeoutSeconds:      2,
		PollIntervalSeconds: 1,
	})
	if err == nil {
		t.Fatal("expected disabled error")
	}
	if callResult == nil || !callResult.IsError {
		t.Fatal("expected MCP error result")
	}
	if !strings.Contains(err.Error(), "temporarily disabled") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWaitForNetrunnerSessions_DisabledWithoutExplicitList(t *testing.T) {
	callResult, _, err := WaitForNetrunnerSessions(context.Background(), nil, WaitForNetrunnerSessionsInput{
		TimeoutSeconds:      2,
		PollIntervalSeconds: 1,
	})
	if err == nil {
		t.Fatal("expected disabled error")
	}
	if callResult == nil || !callResult.IsError {
		t.Fatal("expected MCP error result")
	}
	if !strings.Contains(err.Error(), "temporarily disabled") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWaitForNetrunnerSessions_DisabledOnTimeoutPathToo(t *testing.T) {
	callResult, _, err := WaitForNetrunnerSessions(context.Background(), nil, WaitForNetrunnerSessionsInput{
		SessionIds:          []int{1},
		TimeoutSeconds:      1,
		PollIntervalSeconds: 1,
	})
	if err == nil {
		t.Fatal("expected disabled error")
	}
	if callResult == nil || !callResult.IsError {
		t.Fatal("expected MCP error result")
	}
	if !strings.Contains(err.Error(), "temporarily disabled") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLaunchExplicitNetrunnerWithMetadata_AllowsConcurrentWorkerForDifferentSession(t *testing.T) {
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

	testDB := setupGetProjectsTestDB(t)
	defer func() {
		_ = testDB.Close()
	}()

	if _, err := testDB.Exec("UPDATE session SET declared_write_scope = '[\"fixer_mcp\"]' WHERE id = 1"); err != nil {
		t.Fatalf("seed source write scope: %v", err)
	}
	if _, err := testDB.Exec("INSERT INTO session (project_id, task_description, status, declared_write_scope) VALUES (1, 'Task C', 'pending', '[\"fixer_mcp/main.go\"]')"); err != nil {
		t.Fatalf("seed concurrent session: %v", err)
	}
	if _, err := testDB.Exec("INSERT INTO worker_process (project_id, session_id, pid, launch_epoch, status) VALUES (1, 1, ?, 0, 'running')", os.Getpid()); err != nil {
		t.Fatalf("seed active worker: %v", err)
	}

	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1

	executed := false
	var gotArgs []string
	execCommand = func(name string, arg ...string) *exec.Cmd {
		executed = true
		gotArgs = append([]string{}, arg...)
		return exec.Command("true")
	}

	metadata, err := launchExplicitNetrunnerWithMetadata(context.Background(), LaunchExplicitNetrunnerInput{
		SessionId: 2,
	})
	if err != nil {
		t.Fatalf("expected concurrent launch to succeed for a different session, got: %v", err)
	}
	if !executed {
		t.Fatal("launcher should run when only a different session is active")
	}
	if metadata.SessionId != 2 {
		t.Fatalf("expected metadata for session 2, got %+v", metadata)
	}
	if !containsString(gotArgs, "--suppress-autonomous-wake") {
		t.Fatalf("expected explicit launch command to suppress autonomous wake, got %+v", gotArgs)
	}
	globalSessionID, err := globalSessionIDFromProjectScoped(2, 1)
	if err != nil {
		t.Fatalf("map launched session: %v", err)
	}
	var launchOrigin string
	if err := testDB.QueryRow("SELECT COALESCE(launch_origin, '') FROM worker_process WHERE session_id = ? ORDER BY id DESC LIMIT 1", globalSessionID).Scan(&launchOrigin); err != nil {
		t.Fatalf("query worker launch origin: %v", err)
	}
	if launchOrigin != "explicit-wait" {
		t.Fatalf("expected explicit-wait worker launch origin, got %q", launchOrigin)
	}
}

func TestLaunchExplicitNetrunner_RequiresRepairForkAfterForcedStopWithoutOverride(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
	}()

	testDB := setupGetProjectsTestDB(t)
	defer func() {
		_ = testDB.Close()
	}()

	if _, err := testDB.Exec("UPDATE session SET forced_stop_count = 1 WHERE id = 1"); err != nil {
		t.Fatalf("seed forced stop count: %v", err)
	}

	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1

	_, err := launchExplicitNetrunnerWithMetadata(context.Background(), LaunchExplicitNetrunnerInput{
		SessionId: 1,
	})
	if err == nil {
		t.Fatal("expected repair-fork guidance rejection")
	}
	if !strings.Contains(err.Error(), "fork_repair_session_from") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWaitForNetrunnerSession_IgnoresFrozenOrEpochMetadata(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
	}()

	testDB := setupGetProjectsTestDB(t)
	defer func() {
		_ = testDB.Close()
	}()

	if _, err := testDB.Exec("UPDATE session SET status = 'review', report = ? WHERE id = 1", structuredTestFinalReport); err != nil {
		t.Fatalf("seed review session: %v", err)
	}
	if _, err := testDB.Exec("INSERT INTO doc_proposal (project_id, session_id, status, proposed_content, proposed_doc_type) VALUES (1, 1, 'pending', 'Doc delta', 'documentation')"); err != nil {
		t.Fatalf("seed proposal: %v", err)
	}
	if _, err := testDB.Exec("INSERT INTO worker_process (project_id, session_id, pid, launch_epoch, status) VALUES (1, 1, ?, 1, 'running')", os.Getpid()); err != nil {
		t.Fatalf("seed worker_process: %v", err)
	}
	if _, err := testDB.Exec("INSERT INTO autonomous_run_status (project_id, session_id, state, summary, orchestration_epoch, orchestration_frozen, notifications_enabled_for_active_run) VALUES (1, 1, 'blocked', 'Frozen by operator', 2, 1, 0)"); err != nil {
		t.Fatalf("seed autonomous status: %v", err)
	}

	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1

	_, out, err := WaitForNetrunnerSession(context.Background(), nil, WaitForNetrunnerSessionInput{
		SessionId:           1,
		TimeoutSeconds:      1,
		PollIntervalSeconds: 1,
	})
	if err != nil {
		t.Fatalf("wait_for_netrunner_session failed: %v", err)
	}
	if !out.Result.Terminal || out.Result.TerminalCondition != "review_ready" {
		t.Fatalf("expected terminal review_ready result, got %+v", out.Result)
	}
	if out.Result.RepairForkRecommended {
		t.Fatalf("did not expect repair fork recommendation, got %+v", out.Result)
	}
}
