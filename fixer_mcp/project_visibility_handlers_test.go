package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestHelperProcessOverseerLauncherFailure(t *testing.T) {
	if os.Getenv("GO_WANT_OVERSEER_LAUNCHER_FAILURE") != "1" {
		return
	}
	_, _ = os.Stdout.WriteString("launcher stdout detail\n")
	_, _ = os.Stderr.WriteString("launcher stderr detail\n")
	os.Exit(2)
}

func setupGetProjectsTestDB(t *testing.T) *sql.DB {
	t.Helper()

	normalizedProjectCWD, err := normalizeProjectCWD(testProjectCWD)
	if err != nil {
		t.Fatalf("normalize test project cwd: %v", err)
	}

	testDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	testDB.SetMaxOpenConns(1)
	testDB.SetMaxIdleConns(1)

	_, err = testDB.Exec(`
		CREATE TABLE project (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			cwd TEXT UNIQUE NOT NULL,
			active INTEGER NOT NULL DEFAULT 0
		);
			CREATE TABLE session (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				project_id INTEGER,
				task_description TEXT NOT NULL,
				status TEXT NOT NULL,
				report TEXT,
				cli_backend TEXT NOT NULL DEFAULT 'codex',
				cli_model TEXT NOT NULL DEFAULT '',
				cli_reasoning TEXT NOT NULL DEFAULT '',
				declared_write_scope TEXT NOT NULL DEFAULT '["."]',
				parallel_wave_id TEXT NOT NULL DEFAULT '',
				repair_source_session_id INTEGER,
				rework_count INTEGER NOT NULL DEFAULT 0,
				forced_stop_count INTEGER NOT NULL DEFAULT 0
			);
		CREATE TABLE project_doc (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER,
			title TEXT NOT NULL,
			content TEXT NOT NULL,
			doc_type TEXT DEFAULT 'documentation',
			parent_doc_id INTEGER,
			level INTEGER NOT NULL DEFAULT 0,
			slug TEXT NOT NULL DEFAULT '',
			path TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'current'
		);
		CREATE TABLE doc_proposal (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER,
			session_id INTEGER,
			status TEXT NOT NULL,
			proposed_content TEXT NOT NULL,
			proposed_doc_type TEXT DEFAULT 'documentation',
			target_project_doc_id INTEGER
		);
		CREATE TABLE mcp_server (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			short_description TEXT,
			long_description TEXT,
			auto_attach INTEGER NOT NULL DEFAULT 0,
			is_default INTEGER NOT NULL DEFAULT 0,
			category TEXT,
			how_to TEXT,
			auth_env_keys TEXT NOT NULL DEFAULT '',
			portability TEXT NOT NULL DEFAULT '',
			install_hint TEXT NOT NULL DEFAULT '',
			archived INTEGER NOT NULL DEFAULT 0
		);
		CREATE TABLE session_mcp_server (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id INTEGER NOT NULL,
			mcp_server_id INTEGER NOT NULL,
			UNIQUE(session_id, mcp_server_id)
		);
		CREATE TABLE project_mcp_server (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL,
			mcp_server_id INTEGER NOT NULL,
			UNIQUE(project_id, mcp_server_id)
		);
		CREATE TABLE netrunner_attached_doc (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id INTEGER NOT NULL,
			project_doc_id INTEGER NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(session_id, project_doc_id)
		);
		CREATE TABLE netrunner_session_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL,
			session_id INTEGER NOT NULL,
			log_type TEXT NOT NULL CHECK(log_type IN ('started', 'progress', 'blocked', 'workaround', 'completed')),
			log_text TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
			CREATE TABLE autonomous_run_status (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				project_id INTEGER NOT NULL,
				session_id INTEGER,
				state TEXT NOT NULL,
				summary TEXT NOT NULL,
				focus TEXT,
				blocker TEXT,
				evidence TEXT,
				orchestration_epoch INTEGER NOT NULL DEFAULT 0,
				orchestration_frozen INTEGER NOT NULL DEFAULT 0,
				notifications_enabled_for_active_run INTEGER NOT NULL DEFAULT 1,
				created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
				UNIQUE(project_id)
			);
			CREATE TABLE worker_process (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				project_id INTEGER NOT NULL,
				session_id INTEGER NOT NULL,
				pid INTEGER NOT NULL,
				launch_epoch INTEGER NOT NULL DEFAULT 0,
				status TEXT NOT NULL DEFAULT 'running',
				stop_reason TEXT,
				launch_origin TEXT NOT NULL DEFAULT '',
				started_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
				stopped_at TEXT
			);
			CREATE TABLE image_generation_job (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				project_id INTEGER NOT NULL,
				prompt TEXT NOT NULL,
				status TEXT NOT NULL DEFAULT 'launching',
				pid INTEGER NOT NULL DEFAULT 0,
				model TEXT NOT NULL DEFAULT '',
				output_path TEXT NOT NULL DEFAULT '',
				workspace_copy_path TEXT NOT NULL DEFAULT '',
				output_last_message_path TEXT NOT NULL DEFAULT '',
				json_output_path TEXT NOT NULL DEFAULT '',
				stderr_log_path TEXT NOT NULL DEFAULT '',
				thread_id TEXT NOT NULL DEFAULT '',
				failure_reason TEXT NOT NULL DEFAULT '',
				generated_images_baseline TEXT NOT NULL DEFAULT '[]',
				started_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
				completed_at TEXT
			);
			CREATE TABLE project_handoff (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				project_id INTEGER NOT NULL,
				content TEXT NOT NULL,
				updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
				UNIQUE(project_id)
			);
			CREATE TABLE project_overview (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				project_id INTEGER NOT NULL,
				content TEXT NOT NULL,
				updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
				UNIQUE(project_id)
			);
			CREATE TABLE overseer_fixer_message (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				project_id INTEGER NOT NULL,
				sender_role TEXT NOT NULL CHECK(sender_role IN ('overseer', 'fixer')),
				content TEXT NOT NULL,
				created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
			);
			CREATE INDEX overseer_fixer_message_project_id_idx ON overseer_fixer_message(project_id, id);
			CREATE INDEX overseer_fixer_message_sender_idx ON overseer_fixer_message(sender_role, id);
			CREATE TABLE overseer_fixer_run_state (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				project_id INTEGER NOT NULL UNIQUE,
				active INTEGER NOT NULL DEFAULT 0,
				status TEXT NOT NULL DEFAULT '',
				reason TEXT NOT NULL DEFAULT '',
				last_message_id INTEGER NOT NULL DEFAULT 0,
				created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
			);
			CREATE INDEX overseer_fixer_run_state_active_idx ON overseer_fixer_run_state(active, project_id);
			CREATE TABLE session_codex_link (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				session_id INTEGER NOT NULL UNIQUE,
				codex_session_id TEXT NOT NULL,
				updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
			);
			CREATE TABLE session_external_link (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				session_id INTEGER NOT NULL,
				backend TEXT NOT NULL,
				external_session_id TEXT NOT NULL,
				updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
			);
			CREATE TABLE role_preprompt (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				role_name TEXT NOT NULL UNIQUE,
				prompt_text TEXT NOT NULL,
				updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
			);
		INSERT INTO project (name, cwd) VALUES ('Alpha', '` + normalizedProjectCWD + `');
		INSERT INTO project (name, cwd) VALUES ('Beta', '/tmp/other-project');
		INSERT INTO session (project_id, task_description, status) VALUES (1, 'Task A', 'pending');
		INSERT INTO session (project_id, task_description, status) VALUES (2, 'Task B', 'pending');
		INSERT INTO project_doc (id, project_id, title, content, doc_type) VALUES
			(1, 1, 'Doc A', 'Content A', 'documentation'),
			(2, 1, 'Doc B', 'Content B', 'architecture'),
			(3, 2, 'Doc C', 'Content C', 'documentation');
		INSERT INTO mcp_server (name, short_description, long_description, auto_attach, is_default, category, how_to) VALUES
			('sqlite', 'SQLite DB', '', 1, 1, 'DB', 'Use for local database checks'),
			('legacy_operator_bridge', 'Legacy operator bridge', '', 0, 0, 'Productivity', 'Use only for legacy external notification paths');
		INSERT INTO project_mcp_server (project_id, mcp_server_id) VALUES
			(1, 1),
			(1, 2),
			(2, 1);
	`)
	if err != nil {
		_ = testDB.Close()
		t.Fatalf("seed db: %v", err)
	}

	return testDB
}

func TestGetProjects_DeniesFixerRole(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	defer func() {
		db = originalDB
		authorizedRole = originalRole
	}()

	testDB := setupGetProjectsTestDB(t)
	defer func() {
		_ = testDB.Close()
	}()

	db = testDB
	authorizedRole = "fixer"

	callResult, _, err := GetProjects(context.Background(), nil, GetProjectsInput{})
	if err == nil {
		t.Fatal("expected access denied error for fixer role")
	}
	t.Logf("fixer response error: %v", err)
	if callResult == nil || !callResult.IsError {
		t.Fatal("expected MCP error result for fixer role")
	}
	if !strings.Contains(err.Error(), "access denied: requires overseer role") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetProjects_AllowsOverseerRole(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	defer func() {
		db = originalDB
		authorizedRole = originalRole
	}()

	testDB := setupGetProjectsTestDB(t)
	defer func() {
		_ = testDB.Close()
	}()

	db = testDB
	authorizedRole = "overseer"

	callResult, out, err := GetProjects(context.Background(), nil, GetProjectsInput{})
	if err != nil {
		t.Fatalf("expected success for overseer role, got error: %v", err)
	}
	t.Logf("overseer response projects: %v", out.Projects)
	if callResult != nil {
		t.Fatalf("expected nil call result on success, got: %+v", callResult)
	}
	if len(out.Projects) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(out.Projects))
	}
}

func TestAssumeRoleFixerThenGetProjects_Denied(t *testing.T) {
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

	_, assumeOut, assumeErr := AssumeRole(context.Background(), nil, AssumeRoleInput{
		Role:  "fixer",
		Cwd:   testProjectCWD,
		Token: "supersecret",
	})
	if assumeErr != nil {
		t.Fatalf("assume_role fixer failed: %v", assumeErr)
	}
	if assumeOut.Status != "success" {
		t.Fatalf("expected fixer auth success, got: %+v", assumeOut)
	}

	callResult, _, err := GetProjects(context.Background(), nil, GetProjectsInput{})
	if err == nil {
		t.Fatal("expected access denied for fixer after successful assume_role")
	}
	t.Logf("assume_role=fixer -> get_projects error: %v", err)
	if callResult == nil || !callResult.IsError {
		t.Fatal("expected MCP error result for fixer get_projects")
	}
	if !strings.Contains(err.Error(), "access denied: requires overseer role") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAssumeRoleOverseerThenGetProjects_Allowed(t *testing.T) {
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

	testDB := setupGetProjectsTestDB(t)
	defer func() {
		_ = testDB.Close()
	}()

	db = testDB

	_, assumeOut, assumeErr := AssumeRole(context.Background(), nil, AssumeRoleInput{
		Role:  "overseer",
		Token: "supersecret",
	})
	if assumeErr != nil {
		t.Fatalf("assume_role overseer failed: %v", assumeErr)
	}
	if assumeOut.Status != "success" {
		t.Fatalf("expected overseer auth success, got: %+v", assumeOut)
	}

	callResult, out, err := GetProjects(context.Background(), nil, GetProjectsInput{})
	if err != nil {
		t.Fatalf("expected success for overseer get_projects, got error: %v", err)
	}
	t.Logf("assume_role=overseer -> get_projects projects: %v", out.Projects)
	if callResult != nil {
		t.Fatalf("expected nil call result on success, got: %+v", callResult)
	}
	if len(out.Projects) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(out.Projects))
	}
}

func TestAssumeRoleClearsStaleNetrunnerSessionBinding(t *testing.T) {
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

	testDB := setupGetProjectsTestDB(t)
	defer func() {
		_ = testDB.Close()
	}()

	db = testDB
	authorizedRole = "netrunner"
	authorizedProjectId = 2
	authorizedSessionId = 999

	_, assumeOut, assumeErr := AssumeRole(context.Background(), nil, AssumeRoleInput{
		Role: "netrunner",
		Cwd:  testProjectCWD,
	})
	if assumeErr != nil {
		t.Fatalf("assume_role netrunner failed: %v", assumeErr)
	}
	if assumeOut.Status != "success" {
		t.Fatalf("expected netrunner auth success, got: %+v", assumeOut)
	}
	if authorizedProjectId != 1 {
		t.Fatalf("expected project 1 after assume_role, got %d", authorizedProjectId)
	}
	if authorizedSessionId != 0 {
		t.Fatalf("expected stale session binding to be cleared, got %d", authorizedSessionId)
	}
}

func TestRegisterProject_OverseerIdempotentAndAuthRecovery(t *testing.T) {
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
	authorizedRole = "overseer"
	authorizedProjectId = 0

	newProjectDir := t.TempDir()

	createResult, createOut, createErr := RegisterProject(context.Background(), nil, RegisterProjectInput{
		Cwd: newProjectDir,
	})
	if createErr != nil {
		t.Fatalf("register_project failed: %v", createErr)
	}
	if createResult != nil {
		t.Fatalf("expected nil call result on success, got: %+v", createResult)
	}
	if createOut.Status != "created" {
		t.Fatalf("expected created status, got %+v", createOut)
	}
	if createOut.ProjectId == 0 {
		t.Fatalf("expected non-zero project id, got %+v", createOut)
	}

	existsResult, existsOut, existsErr := RegisterProject(context.Background(), nil, RegisterProjectInput{
		Cwd:  newProjectDir,
		Name: "Overridden Name Should Be Ignored On Existing",
	})
	if existsErr != nil {
		t.Fatalf("idempotent register_project failed: %v", existsErr)
	}
	if existsResult != nil {
		t.Fatalf("expected nil call result on success, got: %+v", existsResult)
	}
	if existsOut.Status != "exists" {
		t.Fatalf("expected exists status, got %+v", existsOut)
	}
	if existsOut.ProjectId != createOut.ProjectId {
		t.Fatalf("expected same project id across idempotent calls, created=%d exists=%d", createOut.ProjectId, existsOut.ProjectId)
	}

	nestedDir := filepath.Join(newProjectDir, "nested", "child")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatalf("create nested project dir: %v", err)
	}

	nestedResult, nestedOut, nestedErr := RegisterProject(context.Background(), nil, RegisterProjectInput{
		Cwd:  nestedDir,
		Name: "Nested Name Should Not Create Another Project",
	})
	if nestedErr != nil {
		t.Fatalf("nested register_project failed: %v", nestedErr)
	}
	if nestedResult != nil {
		t.Fatalf("expected nil call result on success, got: %+v", nestedResult)
	}
	if nestedOut.Status != "exists" {
		t.Fatalf("expected exists status for nested cwd, got %+v", nestedOut)
	}
	if nestedOut.ProjectId != createOut.ProjectId {
		t.Fatalf("expected nested cwd to reuse parent project id, created=%d nested=%d", createOut.ProjectId, nestedOut.ProjectId)
	}

	var projectCount int
	if err := testDB.QueryRow("SELECT COUNT(*) FROM project WHERE cwd = ?", createOut.Cwd).Scan(&projectCount); err != nil {
		t.Fatalf("count registered project rows: %v", err)
	}
	if projectCount != 1 {
		t.Fatalf("expected one row for registered project cwd, got %d", projectCount)
	}

	_, assumeOut, assumeErr := AssumeRole(context.Background(), nil, AssumeRoleInput{
		Role:  "fixer",
		Cwd:   nestedDir,
		Token: "supersecret",
	})
	if assumeErr != nil {
		t.Fatalf("assume_role fixer after nested registration failed: %v", assumeErr)
	}
	if assumeOut.Status != "success" {
		t.Fatalf("expected fixer auth success after registration, got %+v", assumeOut)
	}
}

func TestRegisterProject_DeniesNonOverseerAndValidatesCWD(t *testing.T) {
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

	deniedResult, _, deniedErr := RegisterProject(context.Background(), nil, RegisterProjectInput{
		Cwd: testProjectCWD,
	})
	if deniedErr == nil {
		t.Fatal("expected access denied for non-overseer role")
	}
	if deniedResult == nil || !deniedResult.IsError {
		t.Fatal("expected MCP error result for non-overseer registration attempt")
	}

	authorizedRole = "overseer"
	authorizedProjectId = 0

	invalidResult, _, invalidErr := RegisterProject(context.Background(), nil, RegisterProjectInput{
		Cwd: "relative/path",
	})
	if invalidErr == nil {
		t.Fatal("expected validation error for relative cwd")
	}
	if invalidResult == nil || !invalidResult.IsError {
		t.Fatal("expected MCP error result for invalid cwd")
	}
}

func TestAssumeRole_UnknownCWD_InstructsOverseerRegistrationOnly(t *testing.T) {
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

	missingCWD := t.TempDir() + "/not-registered"
	callResult, out, err := AssumeRole(context.Background(), nil, AssumeRoleInput{
		Role:  "fixer",
		Cwd:   missingCWD,
		Token: "supersecret",
	})
	if err != nil {
		t.Fatalf("expected MCP error result instead of transport error, got: %v", err)
	}
	if callResult == nil || !callResult.IsError {
		t.Fatal("expected MCP error result for unknown cwd")
	}
	if out.Status != "error" {
		t.Fatalf("expected error status, got: %+v", out)
	}
	if !strings.Contains(out.Message, "Project onboarding is Overseer-only") {
		t.Fatalf("expected overseer-only guidance, got: %q", out.Message)
	}
	if !strings.Contains(out.Message, "register_project") {
		t.Fatalf("expected register_project guidance, got: %q", out.Message)
	}
	if !strings.Contains(out.Message, "Do not retry assume_role as fixer/netrunner") {
		t.Fatalf("expected explicit no-fallback guidance, got: %q", out.Message)
	}
}

func TestAutonomousRunStatus_SetAndGet(t *testing.T) {
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

	firstCallResult, firstOut, err := SetAutonomousRunStatus(context.Background(), nil, SetAutonomousRunStatusInput{
		SessionId: 1,
		State:     "running",
		Summary:   "Initial dashboard scan in progress",
		Focus:     "project list and session counts",
		Evidence:  "session 1 matched autonomous workflow markers",
	})
	if err != nil {
		t.Fatalf("first set_autonomous_run_status failed: %v", err)
	}
	if firstCallResult != nil {
		t.Fatalf("expected nil call result on success, got: %+v", firstCallResult)
	}
	if firstOut.Record.State != "running" || firstOut.Record.SessionId != 1 {
		t.Fatalf("unexpected first autonomous status record: %+v", firstOut.Record)
	}

	secondCallResult, secondOut, err := SetAutonomousRunStatus(context.Background(), nil, SetAutonomousRunStatusInput{
		SessionId: 1,
		State:     "blocked",
		Summary:   "Waiting on final UI validation",
		Focus:     "detail panel",
		Blocker:   "Need one more pass on heuristics",
		Evidence:  "current netrunner session 1 remains the active marker",
	})
	if err != nil {
		t.Fatalf("second set_autonomous_run_status failed: %v", err)
	}
	if secondCallResult != nil {
		t.Fatalf("expected nil call result on success, got: %+v", secondCallResult)
	}
	if secondOut.Record.State != "blocked" {
		t.Fatalf("unexpected updated autonomous status record: %+v", secondOut.Record)
	}

	var rowCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM autonomous_run_status WHERE project_id = 1").Scan(&rowCount); err != nil {
		t.Fatalf("count autonomous_run_status rows: %v", err)
	}
	if rowCount != 1 {
		t.Fatalf("expected one current autonomous status row, got %d", rowCount)
	}

	getCallResult, getOut, err := GetAutonomousRunStatus(context.Background(), nil, GetAutonomousRunStatusInput{})
	if err != nil {
		t.Fatalf("get_autonomous_run_status failed: %v", err)
	}
	if getCallResult != nil {
		t.Fatalf("expected nil call result on success, got: %+v", getCallResult)
	}
	if !getOut.HasStatus {
		t.Fatal("expected stored autonomous status to be present")
	}
	if getOut.Status.State != "blocked" || getOut.Status.Summary != "Waiting on final UI validation" {
		t.Fatalf("unexpected stored autonomous status: %+v", getOut.Status)
	}
}

func TestSendOperatorTelegramNotification_NetrunnerSuccess(t *testing.T) {
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

	testDB := setupGetProjectsTestDB(t)
	defer func() {
		_ = testDB.Close()
	}()

	db = testDB
	authorizedRole = "netrunner"
	authorizedProjectId = 1
	authorizedSessionId = 1

	var received struct {
		ChatID string `json:"chat_id"`
		Text   string `json:"text"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/bottest-token/sendMessage") {
			t.Fatalf("unexpected telegram path: %s", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		if err := json.Unmarshal(body, &received); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	t.Setenv("FIXER_MCP_TELEGRAM_BOT_TOKEN", "test-token")
	t.Setenv("FIXER_MCP_TELEGRAM_CHAT_ID", "12345")
	t.Setenv("FIXER_MCP_TELEGRAM_API_BASE_URL", server.URL)

	callResult, out, err := SendOperatorTelegramNotification(context.Background(), nil, SendOperatorTelegramNotificationInput{
		Source:   "Нетрaннер / headless",
		Status:   "Блокер",
		Summary:  "Не удалось завершить preflight",
		RunState: "blocked",
		Details:  "Figma Bridge не отвечает после reconnect.",
	})
	if err != nil {
		t.Fatalf("send_operator_telegram_notification failed: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil call result on success, got %+v", callResult)
	}
	if out.Status != "success" || out.ProjectId != 1 || out.ProjectName != "Alpha" || out.SessionId != 1 {
		t.Fatalf("unexpected output: %+v", out)
	}
	if received.ChatID != "12345" {
		t.Fatalf("unexpected chat id: %+v", received)
	}
	for _, expectedPart := range []string{
		"Fixer MCP: уведомление оператору",
		"Проект: Alpha (#1)",
		"Источник: Нетрaннер / headless",
		"Статус: Блокер",
		"Сессия: 1",
		"Прогон: blocked",
		"Сводка: Не удалось завершить preflight",
		"Детали: Figma Bridge не отвечает после reconnect.",
	} {
		if !strings.Contains(received.Text, expectedPart) {
			t.Fatalf("expected %q in message, got %q", expectedPart, received.Text)
		}
	}
}

func TestSendOperatorTelegramNotification_RequiresConfiguredEnv(t *testing.T) {
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
	t.Setenv("FIXER_MCP_TELEGRAM_BOT_TOKEN", "")
	t.Setenv("FIXER_MCP_TELEGRAM_CHAT_ID", "")
	t.Setenv("FIXER_MCP_TELEGRAM_API_BASE_URL", "")

	callResult, _, err := SendOperatorTelegramNotification(context.Background(), nil, SendOperatorTelegramNotificationInput{
		Source: "Фиксер",
		Status: "Готово",
	})
	if err == nil {
		t.Fatal("expected missing env error")
	}
	if callResult == nil || !callResult.IsError {
		t.Fatal("expected MCP error result")
	}
	if !strings.Contains(err.Error(), "FIXER_MCP_TELEGRAM_BOT_TOKEN") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProjectHandoffFixerRoundTripAndClear(t *testing.T) {
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

	_, setOut, err := SetProjectHandoff(context.Background(), nil, SetProjectHandoffInput{
		Content: "Current focus: migrate flow map.\nNext: validate wires and tests.",
	})
	if err != nil {
		t.Fatalf("set_project_handoff failed: %v", err)
	}
	if setOut.Record.ProjectId != 1 {
		t.Fatalf("expected project 1, got %d", setOut.Record.ProjectId)
	}
	if !strings.Contains(setOut.Record.Content, "migrate flow map") {
		t.Fatalf("unexpected content: %q", setOut.Record.Content)
	}

	_, getOut, err := GetProjectHandoff(context.Background(), nil, GetProjectHandoffInput{})
	if err != nil {
		t.Fatalf("get_project_handoff failed: %v", err)
	}
	if !getOut.HasHandoff {
		t.Fatal("expected handoff to exist")
	}
	if getOut.Handoff.ProjectId != 1 {
		t.Fatalf("expected project 1, got %d", getOut.Handoff.ProjectId)
	}

	_, clearOut, err := ClearProjectHandoff(context.Background(), nil, ClearProjectHandoffInput{})
	if err != nil {
		t.Fatalf("clear_project_handoff failed: %v", err)
	}
	if clearOut.ProjectId != 1 {
		t.Fatalf("expected cleared project 1, got %d", clearOut.ProjectId)
	}

	_, emptyOut, err := GetProjectHandoff(context.Background(), nil, GetProjectHandoffInput{})
	if err != nil {
		t.Fatalf("get_project_handoff after clear failed: %v", err)
	}
	if emptyOut.HasHandoff {
		t.Fatal("expected no handoff after clear")
	}
}

func TestProjectHandoffOverseerRequiresExplicitProjectID(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	defer func() {
		db = originalDB
		authorizedRole = originalRole
	}()

	testDB := setupGetProjectsTestDB(t)
	defer func() {
		_ = testDB.Close()
	}()

	db = testDB
	authorizedRole = "overseer"

	callResult, _, err := GetProjectHandoff(context.Background(), nil, GetProjectHandoffInput{})
	if err == nil {
		t.Fatal("expected overseer project_id requirement error")
	}
	if callResult == nil || !callResult.IsError {
		t.Fatal("expected MCP error result")
	}
	if !strings.Contains(err.Error(), "project_id is required for overseer") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProjectHandoffDeniesNetrunner(t *testing.T) {
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
	authorizedRole = "netrunner"
	authorizedProjectId = 1

	callResult, _, err := SetProjectHandoff(context.Background(), nil, SetProjectHandoffInput{
		Content: "Should be denied",
	})
	if err == nil {
		t.Fatal("expected access denied for netrunner")
	}
	if callResult == nil || !callResult.IsError {
		t.Fatal("expected MCP error result")
	}
	if !strings.Contains(err.Error(), "requires fixer or overseer role") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProjectActivityAndOverviewRoleAccess(t *testing.T) {
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

	activityResult, activityOut, activityErr := SetProjectActivity(context.Background(), nil, SetProjectActivityInput{
		Activity: "active",
	})
	if activityErr != nil {
		t.Fatalf("set_project_activity failed: %v", activityErr)
	}
	if activityResult != nil {
		t.Fatalf("expected nil call result on success, got: %+v", activityResult)
	}
	if activityOut.Record.ProjectId != 1 || activityOut.Record.Activity != "active" || !activityOut.Record.Active {
		t.Fatalf("unexpected activity output: %+v", activityOut)
	}

	_, emptyOverview, overviewErr := GetProjectOverview(context.Background(), nil, GetProjectOverviewInput{})
	if overviewErr != nil {
		t.Fatalf("get_project_overview empty failed: %v", overviewErr)
	}
	if emptyOverview.HasOverview {
		t.Fatalf("expected no overview before set, got %+v", emptyOverview)
	}

	setOverviewResult, setOverviewOut, setOverviewErr := SetProjectOverview(context.Background(), nil, SetProjectOverviewInput{
		Content: "Alpha is focused on launcher hardening and MCP registry cleanup.",
	})
	if setOverviewErr != nil {
		t.Fatalf("set_project_overview failed: %v", setOverviewErr)
	}
	if setOverviewResult != nil {
		t.Fatalf("expected nil call result on success, got: %+v", setOverviewResult)
	}
	if setOverviewOut.Record.ProjectId != 1 || !strings.Contains(setOverviewOut.Record.Content, "launcher hardening") {
		t.Fatalf("unexpected overview output: %+v", setOverviewOut)
	}

	_, getOverviewOut, getOverviewErr := GetProjectOverview(context.Background(), nil, GetProjectOverviewInput{})
	if getOverviewErr != nil {
		t.Fatalf("get_project_overview failed: %v", getOverviewErr)
	}
	if !getOverviewOut.HasOverview || getOverviewOut.Overview.Content != setOverviewOut.Record.Content {
		t.Fatalf("unexpected overview readback: %+v", getOverviewOut)
	}

	authorizedRole = "netrunner"
	deniedResult, _, deniedErr := SetProjectActivity(context.Background(), nil, SetProjectActivityInput{
		Activity: "passive",
	})
	if deniedErr == nil {
		t.Fatal("expected netrunner access denial")
	}
	if deniedResult == nil || !deniedResult.IsError {
		t.Fatal("expected MCP error result")
	}
	if !strings.Contains(deniedErr.Error(), "requires fixer or overseer role") {
		t.Fatalf("unexpected denial error: %v", deniedErr)
	}
}

func TestOverseerFixerMessagesRoleAccessAndProjectScoping(t *testing.T) {
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
	authorizedRole = "netrunner"
	authorizedProjectId = 1

	deniedResult, _, deniedErr := AppendOverseerFixerMessage(context.Background(), nil, AppendOverseerFixerMessageInput{
		SenderRole: "fixer",
		Content:    "not allowed",
	})
	if deniedErr == nil {
		t.Fatal("expected netrunner access denial")
	}
	if deniedResult == nil || !deniedResult.IsError {
		t.Fatal("expected MCP error result")
	}
	if !strings.Contains(deniedErr.Error(), "requires fixer or overseer role") {
		t.Fatalf("unexpected denial: %v", deniedErr)
	}

	authorizedRole = "fixer"
	authorizedProjectId = 1

	mismatchResult, _, mismatchErr := AppendOverseerFixerMessage(context.Background(), nil, AppendOverseerFixerMessageInput{
		SenderRole: "overseer",
		Content:    "spoofed role",
	})
	if mismatchErr == nil {
		t.Fatal("expected sender role mismatch denial")
	}
	if mismatchResult == nil || !mismatchResult.IsError {
		t.Fatal("expected MCP error result")
	}
	if !strings.Contains(mismatchErr.Error(), "sender_role must match authenticated role") {
		t.Fatalf("unexpected mismatch error: %v", mismatchErr)
	}

	scopeResult, _, scopeErr := AppendOverseerFixerMessage(context.Background(), nil, AppendOverseerFixerMessageInput{
		ProjectId:  2,
		SenderRole: "fixer",
		Content:    "wrong project",
	})
	if scopeErr == nil {
		t.Fatal("expected cross-project denial")
	}
	if scopeResult == nil || !scopeResult.IsError {
		t.Fatal("expected MCP error result")
	}
	if !strings.Contains(scopeErr.Error(), "project_id does not match current project") {
		t.Fatalf("unexpected scope error: %v", scopeErr)
	}

	authorizedRole = "overseer"
	authorizedProjectId = 0

	missingProjectResult, _, missingProjectErr := GetOverseerFixerMessages(context.Background(), nil, GetOverseerFixerMessagesInput{})
	if missingProjectErr == nil {
		t.Fatal("expected overseer project_id requirement")
	}
	if missingProjectResult == nil || !missingProjectResult.IsError {
		t.Fatal("expected MCP error result")
	}
	if !strings.Contains(missingProjectErr.Error(), "project_id is required for overseer") {
		t.Fatalf("unexpected missing project error: %v", missingProjectErr)
	}

	_, out, err := AppendOverseerFixerMessage(context.Background(), nil, AppendOverseerFixerMessageInput{
		ProjectId:  1,
		SenderRole: "overseer",
		Content:    "Please report status.",
	})
	if err != nil {
		t.Fatalf("append overseer message failed: %v", err)
	}
	if out.Message.ProjectId != 1 || out.Message.SenderRole != "overseer" || out.Message.Content != "Please report status." {
		t.Fatalf("unexpected message output: %+v", out.Message)
	}
}

func TestOverseerFixerMessagesLatestChronologicalOrdering(t *testing.T) {
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

	for i := 1; i <= 12; i++ {
		_, _, err := AppendOverseerFixerMessage(context.Background(), nil, AppendOverseerFixerMessageInput{
			SenderRole: "fixer",
			Content:    "message " + strconv.Itoa(i),
		})
		if err != nil {
			t.Fatalf("append message %d: %v", i, err)
		}
	}

	_, out, err := GetOverseerFixerMessages(context.Background(), nil, GetOverseerFixerMessagesInput{})
	if err != nil {
		t.Fatalf("get default latest messages failed: %v", err)
	}
	if len(out.Messages) != 10 {
		t.Fatalf("expected default 10 messages, got %d", len(out.Messages))
	}
	if out.Messages[0].Content != "message 3" || out.Messages[9].Content != "message 12" {
		t.Fatalf("expected latest 10 in chronological order, got %+v", out.Messages)
	}
	for i := 1; i < len(out.Messages); i++ {
		if out.Messages[i].Id <= out.Messages[i-1].Id {
			t.Fatalf("messages are not chronological by id: %+v", out.Messages)
		}
	}

	_, limitedOut, err := GetOverseerFixerMessages(context.Background(), nil, GetOverseerFixerMessagesInput{Limit: 3})
	if err != nil {
		t.Fatalf("get limited messages failed: %v", err)
	}
	if len(limitedOut.Messages) != 3 || limitedOut.Messages[0].Content != "message 10" || limitedOut.Messages[2].Content != "message 12" {
		t.Fatalf("unexpected limited messages: %+v", limitedOut.Messages)
	}
}

func TestOverseerFixerRunStateRoundTripAndNoActiveWait(t *testing.T) {
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

	_, emptyOut, err := GetOverseerFixerRunState(context.Background(), nil, GetOverseerFixerRunStateInput{})
	if err != nil {
		t.Fatalf("get empty run state failed: %v", err)
	}
	if emptyOut.HasState {
		t.Fatalf("expected empty run state, got %+v", emptyOut)
	}

	_, messageOut, err := AppendOverseerFixerMessage(context.Background(), nil, AppendOverseerFixerMessageInput{
		SenderRole: "fixer",
		Content:    "ready",
	})
	if err != nil {
		t.Fatalf("append fixer message failed: %v", err)
	}

	_, setOut, err := SetOverseerFixerRunState(context.Background(), nil, SetOverseerFixerRunStateInput{
		Active: true,
		Status: "Running",
		Reason: "Started by overseer request",
	})
	if err != nil {
		t.Fatalf("set active run state failed: %v", err)
	}
	if !setOut.State.Active || setOut.State.Status != "Running" || setOut.State.LastMessageId != messageOut.Message.Id {
		t.Fatalf("unexpected active run state: %+v", setOut.State)
	}

	_, getOut, err := GetOverseerFixerRunState(context.Background(), nil, GetOverseerFixerRunStateInput{})
	if err != nil {
		t.Fatalf("get run state failed: %v", err)
	}
	if !getOut.HasState || !getOut.State.Active {
		t.Fatalf("expected active state, got %+v", getOut)
	}

	_, inactiveOut, err := SetOverseerFixerRunState(context.Background(), nil, SetOverseerFixerRunStateInput{
		Active: false,
		Status: "Idle",
		Reason: "Done",
	})
	if err != nil {
		t.Fatalf("set inactive run state failed: %v", err)
	}
	if inactiveOut.State.Active {
		t.Fatalf("expected inactive run state, got %+v", inactiveOut.State)
	}

	authorizedRole = "overseer"
	authorizedProjectId = 0

	_, waitOut, err := WaitForOverseerFixerMessages(context.Background(), nil, WaitForOverseerFixerMessagesInput{
		TimeoutMs: 50,
	})
	if err != nil {
		t.Fatalf("wait no-active failed: %v", err)
	}
	if waitOut.Status != "no_active_fixers" || len(waitOut.Messages) != 0 {
		t.Fatalf("unexpected no-active wait output: %+v", waitOut)
	}
}

func TestWaitForOverseerFixerMessagesImmediateAndTimeout(t *testing.T) {
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

	if _, _, err := SetOverseerFixerRunState(context.Background(), nil, SetOverseerFixerRunStateInput{
		Active: true,
		Status: "running",
	}); err != nil {
		t.Fatalf("set active project 1: %v", err)
	}

	_, fixerMessage, err := AppendOverseerFixerMessage(context.Background(), nil, AppendOverseerFixerMessageInput{
		SenderRole: "fixer",
		Content:    "project 1 update",
	})
	if err != nil {
		t.Fatalf("append project 1 fixer message: %v", err)
	}

	authorizedProjectId = 2
	if _, _, err := SetOverseerFixerRunState(context.Background(), nil, SetOverseerFixerRunStateInput{
		Active: true,
		Status: "running",
	}); err != nil {
		t.Fatalf("set active project 2: %v", err)
	}
	_, _, err = AppendOverseerFixerMessage(context.Background(), nil, AppendOverseerFixerMessageInput{
		SenderRole: "fixer",
		Content:    "project 2 update",
	})
	if err != nil {
		t.Fatalf("append project 2 fixer message: %v", err)
	}

	authorizedRole = "overseer"
	authorizedProjectId = 0

	_, immediateOut, err := WaitForOverseerFixerMessages(context.Background(), nil, WaitForOverseerFixerMessagesInput{
		ProjectIds:     []int{1},
		AfterMessageId: fixerMessage.Message.Id - 1,
		TimeoutMs:      200,
		PollIntervalMs: 10,
	})
	if err != nil {
		t.Fatalf("immediate wait failed: %v", err)
	}
	if immediateOut.Status != "messages" || immediateOut.TimedOut || len(immediateOut.Messages) != 1 {
		t.Fatalf("unexpected immediate wait output: %+v", immediateOut)
	}
	if immediateOut.Messages[0].ProjectId != 1 || immediateOut.Messages[0].Content != "project 1 update" {
		t.Fatalf("unexpected immediate message: %+v", immediateOut.Messages)
	}
	if immediateOut.CursorMessageId != fixerMessage.Message.Id {
		t.Fatalf("expected cursor %d, got %d", fixerMessage.Message.Id, immediateOut.CursorMessageId)
	}

	started := time.Now()
	_, timeoutOut, err := WaitForOverseerFixerMessages(context.Background(), nil, WaitForOverseerFixerMessagesInput{
		ProjectIds:     []int{1},
		AfterMessageId: immediateOut.CursorMessageId,
		TimeoutMs:      25,
		PollIntervalMs: 5,
	})
	if err != nil {
		t.Fatalf("timeout wait failed: %v", err)
	}
	if timeoutOut.Status != "timeout" || !timeoutOut.TimedOut || len(timeoutOut.Messages) != 0 {
		t.Fatalf("unexpected timeout output: %+v", timeoutOut)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("short timeout took too long: %v", elapsed)
	}
}

func TestLaunchAndWaitFixers_NoActiveProjects(t *testing.T) {
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
	authorizedRole = "overseer"
	authorizedProjectId = 0

	_, out, err := LaunchAndWaitFixers(context.Background(), nil, LaunchAndWaitFixersInput{
		TimeoutSeconds:      1,
		PollIntervalSeconds: 1,
	})
	if err != nil {
		t.Fatalf("launch_and_wait_fixers no-active failed: %v", err)
	}
	if out.Status != "no_active_fixers" || len(out.ProjectIds) != 0 || len(out.Messages) != 0 {
		t.Fatalf("unexpected no-active output: %+v", out)
	}
}

func TestLaunchAndWaitFixers_AppendsMessageLaunchesAndReturnsImmediateFixerResponse(t *testing.T) {
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

	projectCWD := t.TempDir()
	testDB := setupGetProjectsTestDB(t)
	defer func() {
		_ = testDB.Close()
	}()
	if _, err := testDB.Exec("UPDATE project SET cwd = ?, active = 1 WHERE id = 1", projectCWD); err != nil {
		t.Fatalf("activate project 1: %v", err)
	}

	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1
	_, priorMessage, err := AppendOverseerFixerMessage(context.Background(), nil, AppendOverseerFixerMessageInput{
		SenderRole: "fixer",
		Content:    "old fixer status",
	})
	if err != nil {
		t.Fatalf("seed prior fixer message: %v", err)
	}

	var gotName string
	var gotArgs []string
	execCommand = func(name string, arg ...string) *exec.Cmd {
		gotName = name
		gotArgs = append([]string{}, arg...)
		if _, err := testDB.Exec(
			"INSERT INTO overseer_fixer_message (project_id, sender_role, content) VALUES (1, 'fixer', 'new fixer response')",
		); err != nil {
			t.Fatalf("append fake fixer response: %v", err)
		}
		return exec.Command("true")
	}

	authorizedRole = "overseer"
	authorizedProjectId = 0
	_, out, err := LaunchAndWaitFixers(context.Background(), nil, LaunchAndWaitFixersInput{
		ProjectIds:          []int{1},
		Message:             "Please report status",
		TimeoutSeconds:      1,
		PollIntervalSeconds: 1,
	})
	if err != nil {
		t.Fatalf("launch_and_wait_fixers immediate failed: %v", err)
	}
	if out.Status != "messages" || out.TimedOut || len(out.Messages) != 1 {
		t.Fatalf("unexpected immediate output: %+v", out)
	}
	if out.Messages[0].Content != "new fixer response" || out.Messages[0].Id <= priorMessage.Message.Id {
		t.Fatalf("unexpected response message: %+v", out.Messages)
	}
	if gotName != "python3" {
		t.Fatalf("expected python3 launcher, got %q", gotName)
	}
	if len(gotArgs) < 4 || gotArgs[1] != "launch-overseer-fixer" || gotArgs[2] != "--cwd" || gotArgs[3] != projectCWD {
		t.Fatalf("unexpected launcher args: %+v", gotArgs)
	}
	if len(out.Projects) != 1 || out.Projects[0].CursorMessageId != priorMessage.Message.Id || out.Projects[0].AppendedMessageId == 0 {
		t.Fatalf("unexpected project result: %+v", out.Projects)
	}

	var stateCursor int
	if err := testDB.QueryRow("SELECT last_message_id FROM overseer_fixer_run_state WHERE project_id = 1").Scan(&stateCursor); err != nil {
		t.Fatalf("query run state cursor: %v", err)
	}
	if stateCursor != priorMessage.Message.Id {
		t.Fatalf("expected run-state cursor %d, got %d", priorMessage.Message.Id, stateCursor)
	}
}

func TestLaunchAndWaitFixers_LaunchFailureIncludesLauncherOutput(t *testing.T) {
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

	projectCWD := t.TempDir()
	testDB := setupGetProjectsTestDB(t)
	defer func() {
		_ = testDB.Close()
	}()
	if _, err := testDB.Exec("UPDATE project SET cwd = ?, active = 1 WHERE id = 1", projectCWD); err != nil {
		t.Fatalf("activate project 1: %v", err)
	}

	db = testDB
	authorizedRole = "overseer"
	authorizedProjectId = 0
	t.Setenv("GO_WANT_OVERSEER_LAUNCHER_FAILURE", "1")
	execCommand = func(name string, arg ...string) *exec.Cmd {
		return exec.Command(os.Args[0], "-test.run=TestHelperProcessOverseerLauncherFailure", "--")
	}

	_, out, err := LaunchAndWaitFixers(context.Background(), nil, LaunchAndWaitFixersInput{
		ProjectIds:          []int{1},
		TimeoutSeconds:      1,
		PollIntervalSeconds: 1,
	})
	if err != nil {
		t.Fatalf("launch_and_wait_fixers launch failure failed unexpectedly: %v", err)
	}
	if out.Status != "launch_failed" || len(out.Projects) != 1 {
		t.Fatalf("unexpected launch failure output: %+v", out)
	}
	diagnostic := out.Projects[0].LauncherDiagnostic
	if !strings.Contains(diagnostic, "exit status 2") {
		t.Fatalf("expected exit status in diagnostic, got %q", diagnostic)
	}
	if !strings.Contains(diagnostic, "launcher stdout detail") || !strings.Contains(diagnostic, "launcher stderr detail") {
		t.Fatalf("expected captured output in diagnostic, got %q", diagnostic)
	}
}

func TestLaunchAndWaitFixers_Timeout(t *testing.T) {
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

	projectCWD := t.TempDir()
	testDB := setupGetProjectsTestDB(t)
	defer func() {
		_ = testDB.Close()
	}()
	if _, err := testDB.Exec("UPDATE project SET cwd = ?, active = 1 WHERE id = 1", projectCWD); err != nil {
		t.Fatalf("activate project 1: %v", err)
	}

	db = testDB
	authorizedRole = "overseer"
	authorizedProjectId = 0
	execCommand = func(name string, arg ...string) *exec.Cmd {
		return exec.Command("true")
	}

	started := time.Now()
	_, out, err := LaunchAndWaitFixers(context.Background(), nil, LaunchAndWaitFixersInput{
		ProjectIds:          []int{1},
		TimeoutSeconds:      1,
		PollIntervalSeconds: 1,
	})
	if err != nil {
		t.Fatalf("launch_and_wait_fixers timeout failed: %v", err)
	}
	if out.Status != "timeout" || !out.TimedOut || len(out.Messages) != 0 {
		t.Fatalf("unexpected timeout output: %+v", out)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("timeout took too long: %v", elapsed)
	}
}

func TestGetActiveProjectOverviews_OverseerPayload(t *testing.T) {
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
	authorizedRole = "overseer"
	authorizedProjectId = 0

	if _, err := testDB.Exec("UPDATE project SET active = 1 WHERE id = 1"); err != nil {
		t.Fatalf("mark project active: %v", err)
	}
	if _, err := testDB.Exec("UPDATE project SET active = 0 WHERE id = 2"); err != nil {
		t.Fatalf("mark project passive: %v", err)
	}
	if _, err := testDB.Exec("INSERT INTO project_overview (project_id, content) VALUES (1, 'Alpha overview')"); err != nil {
		t.Fatalf("seed overview: %v", err)
	}
	if _, err := testDB.Exec("INSERT INTO project_overview (project_id, content) VALUES (2, 'Beta overview')"); err != nil {
		t.Fatalf("seed passive overview: %v", err)
	}
	if _, err := testDB.Exec("INSERT INTO project_handoff (project_id, content) VALUES (1, 'Alpha handoff')"); err != nil {
		t.Fatalf("seed handoff: %v", err)
	}
	for i := 2; i <= 6; i++ {
		_, err := testDB.Exec(
			"INSERT INTO session (project_id, task_description, status, report, cli_backend, cli_model, cli_reasoning) VALUES (1, ?, 'review', ?, 'codex', 'gpt-5.5', 'high')",
			"Task A"+strconv.Itoa(i),
			"Report A"+strconv.Itoa(i),
		)
		if err != nil {
			t.Fatalf("seed session A%d: %v", i, err)
		}
	}

	var latestGlobalID int
	if err := testDB.QueryRow("SELECT id FROM session WHERE project_id = 1 AND task_description = 'Task A6'").Scan(&latestGlobalID); err != nil {
		t.Fatalf("query latest global session id: %v", err)
	}
	if _, err := testDB.Exec("INSERT INTO session_external_link (session_id, backend, external_session_id) VALUES (?, 'codex', 'external-a6')", latestGlobalID); err != nil {
		t.Fatalf("seed external link: %v", err)
	}
	if _, err := testDB.Exec("INSERT INTO session_codex_link (session_id, codex_session_id) VALUES (?, 'codex-a6')", latestGlobalID); err != nil {
		t.Fatalf("seed codex link: %v", err)
	}

	callResult, out, err := GetActiveProjectOverviews(context.Background(), nil, GetActiveProjectOverviewsInput{})
	if err != nil {
		t.Fatalf("get_active_project_overviews failed: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil call result on success, got: %+v", callResult)
	}
	if len(out.Projects) != 1 {
		t.Fatalf("expected only one active project, got %+v", out.Projects)
	}

	project := out.Projects[0]
	if project.ProjectId != 1 || project.Name != "Alpha" || project.Activity != "active" {
		t.Fatalf("unexpected active project header: %+v", project)
	}
	if !project.HasOverview || project.Overview.Content != "Alpha overview" {
		t.Fatalf("unexpected overview payload: %+v", project)
	}
	if !project.HasHandoff || project.Handoff.Content != "Alpha handoff" {
		t.Fatalf("unexpected handoff payload: %+v", project)
	}
	if len(project.LatestSessions) != 5 {
		t.Fatalf("expected latest five sessions, got %+v", project.LatestSessions)
	}
	expectedLocalIDs := []int{6, 5, 4, 3, 2}
	for index, expectedID := range expectedLocalIDs {
		if project.LatestSessions[index].SessionId != expectedID {
			t.Fatalf("expected local session order %v, got %+v", expectedLocalIDs, project.LatestSessions)
		}
	}
	latest := project.LatestSessions[0]
	if latest.GlobalSessionId != latestGlobalID || latest.TaskDescription != "Task A6" || latest.Report != "Report A6" {
		t.Fatalf("unexpected latest session: %+v", latest)
	}
	if latest.CliBackend != "codex" || latest.CliModel != "gpt-5.5" || latest.CliReasoning != "high" {
		t.Fatalf("missing launch metadata: %+v", latest)
	}
	if latest.ExternalSessionId != "external-a6" || latest.CodexSessionId != "codex-a6" {
		t.Fatalf("missing external launch metadata: %+v", latest)
	}
}

func TestGetActiveProjectOverviews_DeniesFixer(t *testing.T) {
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

	callResult, _, err := GetActiveProjectOverviews(context.Background(), nil, GetActiveProjectOverviewsInput{})
	if err == nil {
		t.Fatal("expected fixer access denial")
	}
	if callResult == nil || !callResult.IsError {
		t.Fatal("expected MCP error result")
	}
	if !strings.Contains(err.Error(), "requires overseer role") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSendOperatorTelegramNotification_BlockedWhenNotificationsDisabled(t *testing.T) {
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

	testDB := setupGetProjectsTestDB(t)
	defer func() {
		_ = testDB.Close()
	}()

	if _, err := testDB.Exec("INSERT INTO autonomous_run_status (project_id, session_id, state, summary, orchestration_epoch, orchestration_frozen, notifications_enabled_for_active_run) VALUES (1, 1, 'blocked', 'Frozen', 1, 1, 0)"); err != nil {
		t.Fatalf("seed autonomous status: %v", err)
	}

	db = testDB
	authorizedRole = "netrunner"
	authorizedProjectId = 1
	authorizedSessionId = 1

	callResult, _, err := SendOperatorTelegramNotification(context.Background(), nil, SendOperatorTelegramNotificationInput{
		Source: "Нетрaннер",
		Status: "Стоп",
	})
	if err == nil {
		t.Fatal("expected notification gate rejection")
	}
	if callResult == nil || !callResult.IsError {
		t.Fatal("expected MCP error result")
	}
	if !strings.Contains(err.Error(), "notifications are disabled") {
		t.Fatalf("unexpected error: %v", err)
	}
}
