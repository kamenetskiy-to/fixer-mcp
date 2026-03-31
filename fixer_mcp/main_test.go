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
	"strings"
	"testing"
)

const testProjectCWD = "/tmp/self_orchestration_test_project"
const structuredTestFinalReport = `{"files_changed":["main.go"],"commands_run":["go test ./..."],"checks_run":["go test ./..."],"blockers":[]}`

func setupGetProjectsTestDB(t *testing.T) *sql.DB {
	t.Helper()

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
			cwd TEXT UNIQUE NOT NULL
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
			doc_type TEXT DEFAULT 'documentation'
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
			how_to TEXT
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
				started_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
				stopped_at TEXT
			);
			CREATE TABLE project_handoff (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				project_id INTEGER NOT NULL,
				content TEXT NOT NULL,
				updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
				UNIQUE(project_id)
			);
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
		INSERT INTO project (name, cwd) VALUES ('Alpha', '` + testProjectCWD + `');
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

func setupWakeFixerAutonomousTestDB(t *testing.T, projectCWD string) *sql.DB {
	t.Helper()

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
			cwd TEXT UNIQUE NOT NULL
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
			CREATE TABLE session_external_link (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				session_id INTEGER NOT NULL,
				backend TEXT NOT NULL,
				external_session_id TEXT NOT NULL,
				updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
			);
			CREATE TABLE session_codex_link (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				session_id INTEGER NOT NULL UNIQUE,
				codex_session_id TEXT NOT NULL,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		INSERT INTO project (id, name, cwd) VALUES (1, 'Autonomous', ?);
		INSERT INTO session (id, project_id, task_description, status) VALUES
			(1, 1, 'Autonomous session', 'review');
	`, projectCWD)
	if err != nil {
		_ = testDB.Close()
		t.Fatalf("seed wake db: %v", err)
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

	_, assumeOut, assumeErr := AssumeRole(context.Background(), nil, AssumeRoleInput{
		Role:  "fixer",
		Cwd:   newProjectDir,
		Token: "supersecret",
	})
	if assumeErr != nil {
		t.Fatalf("assume_role fixer after registration failed: %v", assumeErr)
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

func TestGetPendingTasks_NetrunnerRole_NoRegression(t *testing.T) {
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

	callResult, out, err := GetPendingTasks(context.Background(), nil, GetPendingTasksInput{})
	if err != nil {
		t.Fatalf("expected success for netrunner get_pending_tasks, got: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil call result on success, got: %+v", callResult)
	}
	if len(out.Tasks) != 1 {
		t.Fatalf("expected 1 pending task for project 1, got %d", len(out.Tasks))
	}
}

func TestCreateTask_FixerRole_NoRegression(t *testing.T) {
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

	callResult, out, err := CreateTask(context.Background(), nil, CreateTaskInput{TaskDescription: "New task"})
	if err != nil {
		t.Fatalf("expected success for fixer create_task, got: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil call result on success, got: %+v", callResult)
	}
	if out.Status != "success" || out.SessionId == 0 {
		t.Fatalf("unexpected create_task output: %+v", out)
	}
}

func TestGetProjectDocs_NetrunnerRole_NoRegression(t *testing.T) {
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

	callResult, out, err := GetProjectDocs(context.Background(), nil, GetProjectDocsInput{})
	if err != nil {
		t.Fatalf("expected success for netrunner get_project_docs, got: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil call result on success, got: %+v", callResult)
	}
	if len(out.Docs) != 2 {
		t.Fatalf("expected 2 project docs for project 1, got %d", len(out.Docs))
	}
}

func TestSetAndGetSessionMcpServers_FixerAndNetrunner(t *testing.T) {
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

	callResult, setOut, err := SetSessionMcpServers(context.Background(), nil, SetSessionMcpServersInput{
		SessionId:      1,
		McpServerNames: []string{"sqlite", "legacy_operator_bridge"},
	})
	if err != nil {
		t.Fatalf("set_session_mcp_servers failed: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil call result on success, got: %+v", callResult)
	}
	if len(setOut.McpServerNames) != 2 {
		t.Fatalf("expected 2 assigned mcp server names, got: %d", len(setOut.McpServerNames))
	}

	authorizedRole = "netrunner"
	getCallResult, getOut, getErr := GetSessionMcpServers(context.Background(), nil, GetSessionMcpServersInput{SessionId: 1})
	if getErr != nil {
		t.Fatalf("get_session_mcp_servers failed: %v", getErr)
	}
	if getCallResult != nil {
		t.Fatalf("expected nil call result on success, got: %+v", getCallResult)
	}
	if len(getOut.McpServerNames) != 2 {
		t.Fatalf("expected 2 assigned mcp server names, got: %d", len(getOut.McpServerNames))
	}
}

func TestSetSessionMcpServers_RejectsMcpOutsideProjectAllowlist(t *testing.T) {
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
	authorizedProjectId = 2

	callResult, _, err := SetSessionMcpServers(context.Background(), nil, SetSessionMcpServersInput{
		SessionId:      1,
		McpServerNames: []string{"legacy_operator_bridge"},
	})
	if err == nil {
		t.Fatal("expected project allowlist rejection")
	}
	if callResult == nil || !callResult.IsError {
		t.Fatal("expected MCP error result")
	}
	if !strings.Contains(err.Error(), "not allowed for current project") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSetAndGetProjectMcpServers_GatesSessionAssignment(t *testing.T) {
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

	setCallResult, setOut, setErr := SetProjectMcpServers(context.Background(), nil, SetProjectMcpServersInput{
		McpServerNames: []string{"sqlite"},
	})
	if setErr != nil {
		t.Fatalf("set_project_mcp_servers failed: %v", setErr)
	}
	if setCallResult != nil {
		t.Fatalf("expected nil call result on success, got: %+v", setCallResult)
	}
	if len(setOut.McpServerNames) != 1 || setOut.McpServerNames[0] != "sqlite" {
		t.Fatalf("unexpected set_project_mcp_servers output: %+v", setOut)
	}

	getCallResult, getOut, getErr := GetProjectMcpServers(context.Background(), nil, GetProjectMcpServersInput{})
	if getErr != nil {
		t.Fatalf("get_project_mcp_servers failed: %v", getErr)
	}
	if getCallResult != nil {
		t.Fatalf("expected nil call result on success, got: %+v", getCallResult)
	}
	if len(getOut.McpServerNames) != 1 || getOut.McpServerNames[0] != "sqlite" {
		t.Fatalf("unexpected project MCP allowlist: %+v", getOut.McpServerNames)
	}

	assignDeniedResult, _, assignDeniedErr := SetSessionMcpServers(context.Background(), nil, SetSessionMcpServersInput{
		SessionId:      1,
		McpServerNames: []string{"legacy_operator_bridge"},
	})
	if assignDeniedErr == nil {
		t.Fatal("expected session assignment rejection outside project allowlist")
	}
	if assignDeniedResult == nil || !assignDeniedResult.IsError {
		t.Fatal("expected MCP error result for rejected assignment")
	}

	assignCallResult, _, assignErr := SetSessionMcpServers(context.Background(), nil, SetSessionMcpServersInput{
		SessionId:      1,
		McpServerNames: []string{"sqlite"},
	})
	if assignErr != nil {
		t.Fatalf("expected assignment for allowlisted server, got: %v", assignErr)
	}
	if assignCallResult != nil {
		t.Fatalf("expected nil call result on allowed assignment, got: %+v", assignCallResult)
	}
}

func TestWakeFixerAutonomous_NetrunnerRole_Succeeds(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	originalSessionID := authorizedSessionId
	originalExecCommand := execCommand
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
		authorizedSessionId = originalSessionID
		execCommand = originalExecCommand
	}()

	projectDir := t.TempDir()
	stateDir := filepath.Join(projectDir, ".codex")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "autonomous_resolution.json"), []byte(`{"fixer_codex_session_id":"fixer-123"}`), 0o644); err != nil {
		t.Fatalf("write state file: %v", err)
	}

	testDB := setupWakeFixerAutonomousTestDB(t, projectDir)
	defer func() {
		_ = testDB.Close()
	}()

	db = testDB
	authorizedRole = "netrunner"
	authorizedProjectId = 1
	authorizedSessionId = 1
	execCommand = func(name string, arg ...string) *exec.Cmd {
		return exec.Command("true")
	}

	callResult, out, err := WakeFixerAutonomous(context.Background(), nil, WakeFixerAutonomousInput{
		Summary: "worker finished cleanly",
	})
	if err != nil {
		t.Fatalf("wake_fixer_autonomous failed: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil call result on success, got %+v", callResult)
	}
	if out.Status != "success" {
		t.Fatalf("unexpected output: %+v", out)
	}
	if out.SessionId != 1 {
		t.Fatalf("expected session id 1, got %+v", out)
	}
	if !out.SpawnedBackground {
		t.Fatalf("expected background launch flag")
	}
}

func TestWakeFixerAutonomous_DeniesFixerRole(t *testing.T) {
	originalRole := authorizedRole
	defer func() {
		authorizedRole = originalRole
	}()

	authorizedRole = "fixer"
	callResult, _, err := WakeFixerAutonomous(context.Background(), nil, WakeFixerAutonomousInput{})
	if err == nil {
		t.Fatal("expected access denied error")
	}
	if callResult == nil || !callResult.IsError {
		t.Fatal("expected MCP error result")
	}
	if !strings.Contains(err.Error(), "requires netrunner role") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLaunchExplicitNetrunner_FixerRole_Succeeds(t *testing.T) {
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

	if _, err := testDB.Exec("INSERT INTO session_mcp_server (session_id, mcp_server_id) VALUES (1, 1)"); err != nil {
		t.Fatalf("seed session_mcp_server: %v", err)
	}
	if _, err := testDB.Exec("INSERT INTO session_codex_link (session_id, codex_session_id) VALUES (1, 'codex-launch-1')"); err != nil {
		t.Fatalf("seed session_codex_link: %v", err)
	}

	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1

	var capturedName string
	var capturedArgs []string
	execCommand = func(name string, arg ...string) *exec.Cmd {
		capturedName = name
		capturedArgs = append([]string{}, arg...)
		return exec.Command("true")
	}

	callResult, out, err := LaunchExplicitNetrunner(context.Background(), nil, LaunchExplicitNetrunnerInput{
		SessionId:      1,
		FixerSessionId: "fixer-live-123",
	})
	if err != nil {
		t.Fatalf("launch_explicit_netrunner failed: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil call result on success, got %+v", callResult)
	}
	if out.Status != "success" {
		t.Fatalf("unexpected output: %+v", out)
	}
	if capturedName != "python3" {
		t.Fatalf("expected python3 launcher, got %q", capturedName)
	}
	joinedArgs := strings.Join(capturedArgs, " ")
	if !strings.Contains(joinedArgs, "launch-netrunner") {
		t.Fatalf("expected launch-netrunner args, got %q", joinedArgs)
	}
	if !strings.Contains(joinedArgs, "--session-id 1") {
		t.Fatalf("expected session id in args, got %q", joinedArgs)
	}
	if !strings.Contains(joinedArgs, "--fixer-session-id fixer-live-123") {
		t.Fatalf("expected fixer session id in args, got %q", joinedArgs)
	}
	if !out.Launch.SpawnedBackground {
		t.Fatalf("expected background launch metadata, got %+v", out.Launch)
	}
	if out.Launch.SessionId != 1 {
		t.Fatalf("expected visible session id 1, got %+v", out.Launch)
	}
	if out.Launch.CodexSessionId != "codex-launch-1" {
		t.Fatalf("expected codex session id to be returned, got %+v", out.Launch)
	}
}

func TestLaunchExplicitNetrunner_FixerRole_AllowsEmptyAssignedMcpSet(t *testing.T) {
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

	if _, err := testDB.Exec("INSERT INTO session_codex_link (session_id, codex_session_id) VALUES (1, 'codex-launch-empty')"); err != nil {
		t.Fatalf("seed session_codex_link: %v", err)
	}

	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1

	var capturedName string
	var capturedArgs []string
	execCommand = func(name string, arg ...string) *exec.Cmd {
		capturedName = name
		capturedArgs = append([]string{}, arg...)
		return exec.Command("true")
	}

	callResult, out, err := LaunchExplicitNetrunner(context.Background(), nil, LaunchExplicitNetrunnerInput{
		SessionId: 1,
	})
	if err != nil {
		t.Fatalf("launch_explicit_netrunner with empty assigned MCP set failed: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil call result on success, got %+v", callResult)
	}
	if out.Status != "success" {
		t.Fatalf("unexpected output: %+v", out)
	}
	if capturedName != "python3" {
		t.Fatalf("expected python3 launcher, got %q", capturedName)
	}
	joinedArgs := strings.Join(capturedArgs, " ")
	if !strings.Contains(joinedArgs, "launch-netrunner") {
		t.Fatalf("expected launch-netrunner args, got %q", joinedArgs)
	}
	if !strings.Contains(joinedArgs, "--session-id 1") {
		t.Fatalf("expected session id in args, got %q", joinedArgs)
	}
	if out.Launch.CodexSessionId != "codex-launch-empty" {
		t.Fatalf("expected codex session id to be returned, got %+v", out.Launch)
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

	config, err := resolveSessionLaunchConfig(1, 1, "droid", "gpt-5.3-codex", "medium")
	if err != nil {
		t.Fatalf("resolveSessionLaunchConfig failed: %v", err)
	}
	if config.Backend != "droid" || config.Model != "gpt-5.3-codex" || config.Reasoning != "medium" {
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
	if storedBackend != "droid" || storedModel != "gpt-5.3-codex" || storedReasoning != "medium" {
		t.Fatalf("unexpected persisted launch config: backend=%q model=%q reasoning=%q", storedBackend, storedModel, storedReasoning)
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

	_, err := resolveSessionLaunchConfig(1, 1, "droid", "gpt-5.3-codex", "medium")
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

func TestWaitForNetrunnerSessions_ExplicitListReturnsDeterministicLowestWinner(t *testing.T) {
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

	if _, err := testDB.Exec("INSERT INTO session (project_id, task_description, status) VALUES (1, 'Task C', 'review')"); err != nil {
		t.Fatalf("seed review session: %v", err)
	}
	if _, err := testDB.Exec("UPDATE session SET status = 'review', report = 'Worker one ready' WHERE id = 1"); err != nil {
		t.Fatalf("seed winning review session: %v", err)
	}
	if _, err := testDB.Exec("UPDATE session SET report = 'Worker two ready' WHERE id = 3"); err != nil {
		t.Fatalf("seed second review report: %v", err)
	}
	if _, err := testDB.Exec("INSERT INTO doc_proposal (project_id, session_id, status, proposed_content, proposed_doc_type) VALUES (1, 1, 'pending', 'Doc delta', 'documentation')"); err != nil {
		t.Fatalf("seed doc proposal: %v", err)
	}
	if _, err := testDB.Exec("INSERT INTO session_codex_link (session_id, codex_session_id) VALUES (1, 'runner-1')"); err != nil {
		t.Fatalf("seed session_codex_link for winner: %v", err)
	}
	if _, err := testDB.Exec("INSERT INTO session_codex_link (session_id, codex_session_id) VALUES (3, 'runner-2')"); err != nil {
		t.Fatalf("seed session_codex_link for second worker: %v", err)
	}

	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1

	callResult, out, err := WaitForNetrunnerSessions(context.Background(), nil, WaitForNetrunnerSessionsInput{
		SessionIds:          []int{2, 1},
		TimeoutSeconds:      2,
		PollIntervalSeconds: 1,
	})
	if err != nil {
		t.Fatalf("wait_for_netrunner_sessions explicit list failed: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil call result on success, got %+v", callResult)
	}
	if out.Result.WinningSessionId != 1 {
		t.Fatalf("expected lowest project-scoped winner id 1, got %+v", out.Result)
	}
	if out.Result.TerminalCondition != "review_ready" {
		t.Fatalf("expected review_ready, got %+v", out.Result)
	}
	if out.Result.SelectionMode != "explicit_list" {
		t.Fatalf("expected explicit_list selection mode, got %+v", out.Result)
	}
	if strings.TrimSpace(out.Result.Report) != "Worker one ready" {
		t.Fatalf("expected winning report, got %+v", out.Result)
	}
	if len(out.Result.ProposalIds) != 1 || out.Result.ProposalIds[0] != 1 {
		t.Fatalf("expected local proposal id [1], got %+v", out.Result.ProposalIds)
	}
	if len(out.Result.ConsideredSessionIds) != 2 || out.Result.ConsideredSessionIds[0] != 1 || out.Result.ConsideredSessionIds[1] != 2 {
		t.Fatalf("expected sorted considered session ids [1 2], got %+v", out.Result.ConsideredSessionIds)
	}
}

func TestWaitForNetrunnerSessions_AutoDiscoveryFindsExplicitLaunchCandidates(t *testing.T) {
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

	if _, err := testDB.Exec("INSERT INTO session (project_id, task_description, status, report) VALUES (1, 'Task C', 'review', 'Auto worker ready')"); err != nil {
		t.Fatalf("seed auto review session: %v", err)
	}
	if _, err := testDB.Exec("INSERT INTO doc_proposal (project_id, session_id, status, proposed_content, proposed_doc_type) VALUES (1, 3, 'pending', 'Auto doc delta', 'documentation')"); err != nil {
		t.Fatalf("seed auto doc proposal: %v", err)
	}
	if _, err := testDB.Exec("INSERT INTO session_codex_link (session_id, codex_session_id) VALUES (3, 'runner-auto')"); err != nil {
		t.Fatalf("seed auto session_codex_link: %v", err)
	}

	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1

	callResult, out, err := WaitForNetrunnerSessions(context.Background(), nil, WaitForNetrunnerSessionsInput{
		TimeoutSeconds:      2,
		PollIntervalSeconds: 1,
	})
	if err != nil {
		t.Fatalf("wait_for_netrunner_sessions auto discovery failed: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil call result on success, got %+v", callResult)
	}
	if out.Result.SelectionMode != "auto_project_candidates" {
		t.Fatalf("expected auto_project_candidates selection mode, got %+v", out.Result)
	}
	if out.Result.WinningSessionId != 2 {
		t.Fatalf("expected project-scoped winner id 2, got %+v", out.Result)
	}
	if out.Result.CodexSessionId != "runner-auto" {
		t.Fatalf("expected codex session id runner-auto, got %+v", out.Result)
	}
	if len(out.Result.ConsideredSessionIds) != 1 || out.Result.ConsideredSessionIds[0] != 2 {
		t.Fatalf("expected discovered session ids [2], got %+v", out.Result.ConsideredSessionIds)
	}
}

func TestWaitForNetrunnerSessions_TimesOutWithoutWinner(t *testing.T) {
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

	callResult, out, err := WaitForNetrunnerSessions(context.Background(), nil, WaitForNetrunnerSessionsInput{
		SessionIds:          []int{1},
		TimeoutSeconds:      1,
		PollIntervalSeconds: 1,
	})
	if err != nil {
		t.Fatalf("wait_for_netrunner_sessions timeout path failed: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil call result on success, got %+v", callResult)
	}
	if !out.Result.TimedOut || out.Result.Terminal {
		t.Fatalf("expected timeout without terminal winner, got %+v", out.Result)
	}
	if out.Result.WinningSessionId != 0 {
		t.Fatalf("expected no winning session on timeout, got %+v", out.Result)
	}
	if out.Result.TerminalCondition != "timed_out" {
		t.Fatalf("expected timed_out condition, got %+v", out.Result)
	}
	if len(out.Result.ConsideredSessionIds) != 1 || out.Result.ConsideredSessionIds[0] != 1 {
		t.Fatalf("expected considered session ids [1], got %+v", out.Result.ConsideredSessionIds)
	}
}

func TestPruneDeprecatedProjectMcpBindings_RemovesTelegramNotify(t *testing.T) {
	originalDB := db
	defer func() {
		db = originalDB
	}()

	testDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() {
		_ = testDB.Close()
	}()

	if _, err := testDB.Exec(`
		CREATE TABLE mcp_server (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE
		);
		CREATE TABLE project_mcp_server (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL,
			mcp_server_id INTEGER NOT NULL
		);
		INSERT INTO mcp_server (id, name) VALUES (1, 'telegram_notify'), (2, 'sqlite');
		INSERT INTO project_mcp_server (project_id, mcp_server_id) VALUES (2, 1), (2, 2);
	`); err != nil {
		t.Fatalf("seed prune db: %v", err)
	}

	db = testDB
	if err := pruneDeprecatedProjectMcpBindings(); err != nil {
		t.Fatalf("pruneDeprecatedProjectMcpBindings failed: %v", err)
	}

	var count int
	if err := testDB.QueryRow("SELECT COUNT(*) FROM project_mcp_server WHERE mcp_server_id = 1").Scan(&count); err != nil {
		t.Fatalf("count telegram bindings: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected telegram_notify bindings to be removed, got %d", count)
	}
	if err := testDB.QueryRow("SELECT COUNT(*) FROM project_mcp_server WHERE mcp_server_id = 2").Scan(&count); err != nil {
		t.Fatalf("count sqlite bindings: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected non-deprecated bindings to remain, got %d", count)
	}
}

func TestResearchQueryMcp_ProjectScopedAssignmentMatrix(t *testing.T) {
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
	if _, err := db.Exec(
		"INSERT INTO mcp_server (name, short_description, long_description, auto_attach, is_default, category, how_to) VALUES ('research_query_mcp', 'Research query', '', 0, 0, 'Web-search', 'Project-scoped research MCP')",
	); err != nil {
		t.Fatalf("seed research_query_mcp failed: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO project_mcp_server (project_id, mcp_server_id)
		 SELECT 1, id FROM mcp_server WHERE name = 'research_query_mcp'`,
	); err != nil {
		t.Fatalf("bind research_query_mcp to project A failed: %v", err)
	}

	// Project A (id=1): research_query_mcp assignable and visible in session assignment output.
	authorizedRole = "fixer"
	authorizedProjectId = 1
	assignAResult, _, assignAErr := SetSessionMcpServers(context.Background(), nil, SetSessionMcpServersInput{
		SessionId:      1,
		McpServerNames: []string{"research_query_mcp"},
	})
	if assignAErr != nil {
		t.Fatalf("expected project A assignment success, got: %v", assignAErr)
	}
	if assignAResult != nil {
		t.Fatalf("expected nil call result on project A assignment, got: %+v", assignAResult)
	}

	getAResult, getAOut, getAErr := GetSessionMcpServers(context.Background(), nil, GetSessionMcpServersInput{SessionId: 1})
	if getAErr != nil {
		t.Fatalf("expected project A get_session_mcp_servers success, got: %v", getAErr)
	}
	if getAResult != nil {
		t.Fatalf("expected nil call result on project A readback, got: %+v", getAResult)
	}
	if len(getAOut.McpServerNames) != 1 || getAOut.McpServerNames[0] != "research_query_mcp" {
		t.Fatalf("expected project A to expose research_query_mcp assignment, got: %+v", getAOut.McpServerNames)
	}

	// Project B (id=2): research_query_mcp not assignable.
	authorizedProjectId = 2
	assignBDeniedResult, _, assignBDeniedErr := SetSessionMcpServers(context.Background(), nil, SetSessionMcpServersInput{
		SessionId:      1,
		McpServerNames: []string{"research_query_mcp"},
	})
	if assignBDeniedErr == nil {
		t.Fatal("expected project B assignment rejection for research_query_mcp")
	}
	if assignBDeniedResult == nil || !assignBDeniedResult.IsError {
		t.Fatal("expected MCP error result for project B assignment rejection")
	}
	if !strings.Contains(assignBDeniedErr.Error(), "not allowed for current project") {
		t.Fatalf("unexpected project B rejection error: %v", assignBDeniedErr)
	}
}

func TestSetSessionMcpServers_DeniesNetrunner(t *testing.T) {
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

	callResult, _, err := SetSessionMcpServers(context.Background(), nil, SetSessionMcpServersInput{
		SessionId:      1,
		McpServerNames: []string{"sqlite"},
	})
	if err == nil {
		t.Fatal("expected access denied error for netrunner")
	}
	if callResult == nil || !callResult.IsError {
		t.Fatal("expected MCP error result for netrunner")
	}
	if !strings.Contains(err.Error(), "access denied: requires fixer role") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSyncAndListMcpServers_FixerFlow(t *testing.T) {
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

	callResult, syncOut, err := SyncMcpServers(context.Background(), nil, SyncMcpServersInput{
		Servers: []McpServerUpsertInput{
			{Name: "new_registry_server", Category: "Coding", HowTo: "Use for coding tasks", IsDefault: boolPtr(true)},
		},
	})
	if err != nil {
		t.Fatalf("sync_mcp_servers failed: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil call result on success, got: %+v", callResult)
	}
	if syncOut.Total != 1 {
		t.Fatalf("expected total=1, got %+v", syncOut)
	}

	listCallResult, listOut, listErr := ListMcpServers(context.Background(), nil, ListMcpServersInput{IncludeAll: true})
	if listErr != nil {
		t.Fatalf("list_mcp_servers failed: %v", listErr)
	}
	if listCallResult != nil {
		t.Fatalf("expected nil call result on success, got: %+v", listCallResult)
	}

	found := false
	for _, server := range listOut.Servers {
		if server.Name == "new_registry_server" {
			found = true
			if server.Category != "Coding" {
				t.Fatalf("expected category Coding, got %+v", server)
			}
			if server.HowTo != "Use for coding tasks" {
				t.Fatalf("expected how_to persisted, got %+v", server)
			}
			if !server.IsDefault {
				t.Fatalf("expected is_default true, got %+v", server)
			}
			break
		}
	}
	if !found {
		t.Fatalf("expected new_registry_server in list output, got %+v", listOut.Servers)
	}
}

func TestSyncMcpServers_EnrichesCuratedDefaults(t *testing.T) {
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

	_, syncOut, err := SyncMcpServers(context.Background(), nil, SyncMcpServersInput{
		Servers: []McpServerUpsertInput{
			{Name: "figma-console-mcp"},
			{Name: "playwright"},
			{Name: "chrome-devtools"},
			{Name: "eslint"},
			{Name: "mcp-language-server"},
		},
	})
	if err != nil {
		t.Fatalf("sync_mcp_servers failed: %v", err)
	}
	if syncOut.Total != 5 {
		t.Fatalf("expected total=5, got %+v", syncOut)
	}

	_, listOut, listErr := ListMcpServers(context.Background(), nil, ListMcpServersInput{IncludeAll: true})
	if listErr != nil {
		t.Fatalf("list_mcp_servers failed: %v", listErr)
	}

	expectedNames := map[string]string{
		"figma-console-mcp":   "Design",
		"playwright":          "Coding",
		"chrome-devtools":     "Coding",
		"eslint":              "Coding",
		"mcp-language-server": "Coding",
	}
	for _, server := range listOut.Servers {
		expectedCategory, ok := expectedNames[server.Name]
		if !ok {
			continue
		}
		if server.Category != expectedCategory {
			t.Fatalf("expected %s category for %q, got %+v", expectedCategory, server.Name, server)
		}
		if strings.TrimSpace(server.HowTo) == "" {
			t.Fatalf("expected non-empty how_to for %q, got %+v", server.Name, server)
		}
		if !server.IsDefault {
			t.Fatalf("expected %q to be default, got %+v", server.Name, server)
		}
		delete(expectedNames, server.Name)
	}
	if len(expectedNames) > 0 {
		t.Fatalf("missing expected curated MCP servers in registry output: %+v", expectedNames)
	}
}

func TestListMcpServers_DeterministicCategoryOrdering(t *testing.T) {
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

	_, _, err := SyncMcpServers(context.Background(), nil, SyncMcpServersInput{
		Servers: []McpServerUpsertInput{
			{Name: "figma-console-mcp"},
			{Name: "playwright"},
			{Name: "mcp-language-server"},
			{Name: "chrome-devtools"},
			{Name: "eslint"},
		},
	})
	if err != nil {
		t.Fatalf("sync_mcp_servers failed: %v", err)
	}

	_, out, err := ListMcpServers(context.Background(), nil, ListMcpServersInput{IncludeAll: true})
	if err != nil {
		t.Fatalf("list_mcp_servers include_all failed: %v", err)
	}

	names := make([]string, 0, len(out.Servers))
	for _, item := range out.Servers {
		names = append(names, item.Name)
	}

	expected := []string{"sqlite", "figma-console-mcp", "legacy_operator_bridge", "chrome-devtools", "eslint", "mcp-language-server", "playwright"}
	if len(names) != len(expected) {
		t.Fatalf("expected %d servers, got %d (%v)", len(expected), len(names), names)
	}
	for idx := range expected {
		if names[idx] != expected[idx] {
			t.Fatalf("unexpected ordering: expected %v, got %v", expected, names)
		}
	}
}

func TestListMcpServers_DefaultsOnlyByDefault(t *testing.T) {
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

	callResult, out, err := ListMcpServers(context.Background(), nil, ListMcpServersInput{})
	if err != nil {
		t.Fatalf("list_mcp_servers defaults-only failed: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil call result on success, got: %+v", callResult)
	}
	if len(out.Servers) != 1 {
		t.Fatalf("expected 1 default server, got %d", len(out.Servers))
	}
	if out.Servers[0].Name != "sqlite" {
		t.Fatalf("expected sqlite default server, got %+v", out.Servers)
	}
	if !out.Servers[0].IsDefault {
		t.Fatalf("expected is_default=true, got %+v", out.Servers[0])
	}
}

func TestListMcpServers_IncludeAllOverridesDefaultFilter(t *testing.T) {
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

	callResult, out, err := ListMcpServers(context.Background(), nil, ListMcpServersInput{IncludeAll: true})
	if err != nil {
		t.Fatalf("list_mcp_servers include_all failed: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil call result on success, got: %+v", callResult)
	}
	if len(out.Servers) != 2 {
		t.Fatalf("expected 2 servers with include_all, got %d", len(out.Servers))
	}
}

func TestCheckCurrentProjectDocs_FixerOnly(t *testing.T) {
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

	callResult, out, err := CheckCurrentProjectDocs(context.Background(), nil, CheckCurrentProjectDocsInput{})
	if err != nil {
		t.Fatalf("check_current_project_docs failed: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil call result on success, got: %+v", callResult)
	}
	if len(out.Docs) != 2 {
		t.Fatalf("expected 2 docs in summaries, got %d", len(out.Docs))
	}
	if out.Docs[0].Summary == "" {
		t.Fatalf("expected non-empty summary for doc %+v", out.Docs[0])
	}

	authorizedRole = "netrunner"
	deniedResult, _, deniedErr := CheckCurrentProjectDocs(context.Background(), nil, CheckCurrentProjectDocsInput{})
	if deniedErr == nil {
		t.Fatal("expected access denied for netrunner role")
	}
	if deniedResult == nil || !deniedResult.IsError {
		t.Fatal("expected MCP error result for denied role")
	}
}

func TestSetAndGetSessionAttachedDocsAndContent(t *testing.T) {
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
	authorizedRole = "fixer"
	authorizedProjectId = 1

	setCallResult, setOut, setErr := SetSessionAttachedDocs(context.Background(), nil, SetSessionAttachedDocsInput{
		SessionId:     1,
		ProjectDocIds: []int{2, 1, 2},
	})
	if setErr != nil {
		t.Fatalf("set_session_attached_docs failed: %v", setErr)
	}
	if setCallResult != nil {
		t.Fatalf("expected nil call result on success, got: %+v", setCallResult)
	}
	if len(setOut.ProjectDocIds) != 2 || setOut.ProjectDocIds[0] != 1 || setOut.ProjectDocIds[1] != 2 {
		t.Fatalf("expected normalized doc ids [1,2], got %+v", setOut.ProjectDocIds)
	}

	getMetaCallResult, getMetaOut, getMetaErr := GetSessionAttachedDocs(context.Background(), nil, GetSessionAttachedDocsInput{SessionId: 1})
	if getMetaErr != nil {
		t.Fatalf("get_session_attached_docs failed: %v", getMetaErr)
	}
	if getMetaCallResult != nil {
		t.Fatalf("expected nil call result on success, got: %+v", getMetaCallResult)
	}
	if len(getMetaOut.Docs) != 2 {
		t.Fatalf("expected 2 attached docs metadata rows, got %d", len(getMetaOut.Docs))
	}

	authorizedRole = "netrunner"
	authorizedSessionId = 1
	getContentCallResult, getContentOut, getContentErr := GetAttachedProjectDocs(context.Background(), nil, GetAttachedProjectDocsInput{})
	if getContentErr != nil {
		t.Fatalf("get_attached_project_docs failed: %v", getContentErr)
	}
	if getContentCallResult != nil {
		t.Fatalf("expected nil call result on success, got: %+v", getContentCallResult)
	}
	if len(getContentOut.Docs) != 2 {
		t.Fatalf("expected 2 attached docs content rows, got %d", len(getContentOut.Docs))
	}
}

func TestSetSessionAttachedDocs_DeniesNetrunnerAndValidatesProjectDocs(t *testing.T) {
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

	deniedResult, _, deniedErr := SetSessionAttachedDocs(context.Background(), nil, SetSessionAttachedDocsInput{
		SessionId:     1,
		ProjectDocIds: []int{1},
	})
	if deniedErr == nil {
		t.Fatal("expected access denied error for netrunner")
	}
	if deniedResult == nil || !deniedResult.IsError {
		t.Fatal("expected MCP error result for netrunner")
	}

	authorizedRole = "fixer"
	validationResult, _, validationErr := SetSessionAttachedDocs(context.Background(), nil, SetSessionAttachedDocsInput{
		SessionId:     1,
		ProjectDocIds: []int{3},
	})
	if validationErr == nil {
		t.Fatal("expected project_doc validation failure for cross-project doc id")
	}
	if validationResult == nil || !validationResult.IsError {
		t.Fatal("expected MCP error result for validation failure")
	}
	if !strings.Contains(validationErr.Error(), "unknown project_doc_id(s)") {
		t.Fatalf("unexpected validation error: %v", validationErr)
	}
}

func TestAttachedDocsFlow_EndToEndVerification(t *testing.T) {
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
	authorizedRole = "fixer"
	authorizedProjectId = 1

	_, createdTask, createErr := CreateTask(context.Background(), nil, CreateTaskInput{
		TaskDescription: "Verification task for attached docs flow",
	})
	if createErr != nil {
		t.Fatalf("create_task failed: %v", createErr)
	}
	t.Logf("create_task input={task_description: %q} output={session_id: %d, status: %q}", "Verification task for attached docs flow", createdTask.SessionId, createdTask.Status)

	_, docsInventory, docsErr := CheckCurrentProjectDocs(context.Background(), nil, CheckCurrentProjectDocsInput{})
	if docsErr != nil {
		t.Fatalf("check_current_project_docs failed: %v", docsErr)
	}
	t.Logf("check_current_project_docs output_docs=%d first_doc=%+v", len(docsInventory.Docs), docsInventory.Docs[0])

	_, setOut, setErr := SetSessionAttachedDocs(context.Background(), nil, SetSessionAttachedDocsInput{
		SessionId:     createdTask.SessionId,
		ProjectDocIds: []int{1, 2},
	})
	if setErr != nil {
		t.Fatalf("set_session_attached_docs failed: %v", setErr)
	}
	t.Logf("set_session_attached_docs input={session_id: %d, project_doc_ids: [1,2]} output={status: %q, project_doc_ids: %+v}", createdTask.SessionId, setOut.Status, setOut.ProjectDocIds)

	authorizedRole = "netrunner"
	authorizedSessionId = createdTask.SessionId

	_, attachedMeta, metaErr := GetSessionAttachedDocs(context.Background(), nil, GetSessionAttachedDocsInput{
		SessionId: createdTask.SessionId,
	})
	if metaErr != nil {
		t.Fatalf("get_session_attached_docs failed: %v", metaErr)
	}
	t.Logf("get_session_attached_docs output_docs=%d first_doc=%+v", len(attachedMeta.Docs), attachedMeta.Docs[0])

	_, attachedContent, contentErr := GetAttachedProjectDocs(context.Background(), nil, GetAttachedProjectDocsInput{
		SessionId: createdTask.SessionId,
	})
	if contentErr != nil {
		t.Fatalf("get_attached_project_docs failed: %v", contentErr)
	}
	t.Logf("get_attached_project_docs output_docs=%d first_doc_title=%q", len(attachedContent.Docs), attachedContent.Docs[0].Title)
}

func TestCompleteTask_RequiresDocImpactProposal(t *testing.T) {
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

	if _, err := db.Exec("UPDATE session SET status = 'in_progress' WHERE id = 1"); err != nil {
		t.Fatalf("seed status update failed: %v", err)
	}

	callResult, _, err := CompleteTask(context.Background(), nil, CompleteTaskInput{
		SessionId:   1,
		FinalReport: "Attempted completion without proposal",
	})
	if err == nil {
		t.Fatal("expected missing doc-impact proposal error")
	}
	if callResult == nil || !callResult.IsError {
		t.Fatal("expected MCP error result for missing proposal")
	}
	if !strings.Contains(err.Error(), "missing mandatory documentation-impact proposal") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(err.Error(), "propose_doc_update") {
		t.Fatalf("expected actionable guidance, got: %v", err)
	}

	var status, report string
	if qErr := db.QueryRow("SELECT status, COALESCE(report, '') FROM session WHERE id = 1").Scan(&status, &report); qErr != nil {
		t.Fatalf("query session state failed: %v", qErr)
	}
	if status != "in_progress" {
		t.Fatalf("expected session to remain in_progress, got %q", status)
	}
	if report != "" {
		t.Fatalf("expected report to remain unchanged, got %q", report)
	}
}

func TestCompleteTask_AllowsCompletionWhenProposalExists(t *testing.T) {
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

	if _, err := db.Exec("UPDATE session SET status = 'in_progress' WHERE id = 1"); err != nil {
		t.Fatalf("seed status update failed: %v", err)
	}
	if _, err := db.Exec(
		"INSERT INTO doc_proposal (project_id, session_id, status, proposed_content, proposed_doc_type) VALUES (1, 1, 'pending', ?, 'documentation')",
		"Doc impact note for this session",
	); err != nil {
		t.Fatalf("seed doc_proposal failed: %v", err)
	}

	callResult, out, err := CompleteTask(context.Background(), nil, CompleteTaskInput{
		SessionId:   1,
		FinalReport: structuredTestFinalReport,
	})
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil call result on success, got: %+v", callResult)
	}
	if out.Status != "success" {
		t.Fatalf("unexpected output: %+v", out)
	}

	var status, report string
	if qErr := db.QueryRow("SELECT status, COALESCE(report, '') FROM session WHERE id = 1").Scan(&status, &report); qErr != nil {
		t.Fatalf("query session state failed: %v", qErr)
	}
	if status != "review" {
		t.Fatalf("expected status review, got %q", status)
	}
	for _, expectedPart := range []string{`"files_changed":["main.go"]`, `"commands_run":["go test ./..."]`, `"checks_run":["go test ./..."]`, `"blockers":[]`} {
		if !strings.Contains(report, expectedPart) {
			t.Fatalf("expected %q in normalized report, got %q", expectedPart, report)
		}
	}
}

func TestSetSessionStatus_FixerCanCompleteReviewSessionInProject(t *testing.T) {
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

	if _, err := db.Exec("UPDATE session SET status = 'review' WHERE id = 1"); err != nil {
		t.Fatalf("seed status update failed: %v", err)
	}

	callResult, out, err := SetSessionStatus(context.Background(), nil, SetSessionStatusInput{
		SessionId: 1,
		Status:    "completed",
		Reason:    "approved after review",
	})
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil call result on success, got: %+v", callResult)
	}
	if out.PreviousStatus != "review" || out.NewStatus != "completed" {
		t.Fatalf("unexpected output: %+v", out)
	}

	var status string
	if err := db.QueryRow("SELECT status FROM session WHERE id = 1").Scan(&status); err != nil {
		t.Fatalf("query status: %v", err)
	}
	if status != "completed" {
		t.Fatalf("expected completed, got %q", status)
	}
}

func TestSetSessionStatus_OverseerCanUpdateAnyProject(t *testing.T) {
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

	callResult, out, err := SetSessionStatus(context.Background(), nil, SetSessionStatusInput{
		SessionId: 2,
		Status:    "in_progress",
		Reason:    "manual takeover",
	})
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil call result on success, got: %+v", callResult)
	}
	if out.NewStatus != "in_progress" {
		t.Fatalf("unexpected output: %+v", out)
	}

	var status string
	if err := db.QueryRow("SELECT status FROM session WHERE id = 2").Scan(&status); err != nil {
		t.Fatalf("query status: %v", err)
	}
	if status != "in_progress" {
		t.Fatalf("expected in_progress, got %q", status)
	}
}

func TestSetSessionStatus_DeniesNetrunner(t *testing.T) {
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

	callResult, _, err := SetSessionStatus(context.Background(), nil, SetSessionStatusInput{
		SessionId: 1,
		Status:    "in_progress",
	})
	if err == nil {
		t.Fatal("expected access denied error")
	}
	if callResult == nil || !callResult.IsError {
		t.Fatal("expected MCP error result for netrunner")
	}
	if !strings.Contains(err.Error(), "access denied: requires fixer or overseer role") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSetSessionStatus_InvalidStatus(t *testing.T) {
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

	callResult, _, err := SetSessionStatus(context.Background(), nil, SetSessionStatusInput{
		SessionId: 1,
		Status:    "done",
	})
	if err == nil {
		t.Fatal("expected invalid status error")
	}
	if callResult == nil || !callResult.IsError {
		t.Fatal("expected MCP error result")
	}
	if !strings.Contains(err.Error(), "invalid status") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSetSessionStatus_FixerScopeAndTransitionValidation(t *testing.T) {
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

	callResult, _, err := SetSessionStatus(context.Background(), nil, SetSessionStatusInput{
		SessionId: 2,
		Status:    "in_progress",
	})
	if err == nil {
		t.Fatal("expected project-scope access denied error")
	}
	if callResult == nil || !callResult.IsError {
		t.Fatal("expected MCP error result for out-of-project session")
	}
	if !strings.Contains(err.Error(), "session not found in current project") {
		t.Fatalf("unexpected cross-project error: %v", err)
	}

	callResult, _, err = SetSessionStatus(context.Background(), nil, SetSessionStatusInput{
		SessionId: 1,
		Status:    "completed",
	})
	if err == nil {
		t.Fatal("expected invalid transition error")
	}
	if callResult == nil || !callResult.IsError {
		t.Fatal("expected MCP error result for invalid transition")
	}
	if !strings.Contains(err.Error(), "invalid status transition: pending -> completed") {
		t.Fatalf("unexpected transition error: %v", err)
	}
}

func TestSetSessionStatus_VisibleInGetAllSessions(t *testing.T) {
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

	callResult, _, err := SetSessionStatus(context.Background(), nil, SetSessionStatusInput{
		SessionId: 1,
		Status:    "in_progress",
		Reason:    "verification",
	})
	if err != nil {
		t.Fatalf("set_session_status failed: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil call result on success, got: %+v", callResult)
	}

	getCallResult, out, err := GetAllSessions(context.Background(), nil, GetAllSessionsInput{})
	if err != nil {
		t.Fatalf("get_all_sessions failed: %v", err)
	}
	if getCallResult != nil {
		t.Fatalf("expected nil call result on success, got: %+v", getCallResult)
	}

	found := false
	for _, session := range out.Sessions {
		if session.Id == 1 {
			found = true
			if session.Status != "in_progress" {
				t.Fatalf("expected updated status in_progress, got %q", session.Status)
			}
		}
	}
	if !found {
		t.Fatal("expected session 1 in get_all_sessions output")
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

func TestGetSession_AccessControlAndSuccess(t *testing.T) {
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

	callResult, out, err := GetSession(context.Background(), nil, GetSessionInput{SessionId: 1})
	if err != nil {
		t.Fatalf("expected fixer success for in-project session, got: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil call result on success, got: %+v", callResult)
	}
	if out.Session.Id != 1 || out.Session.ProjectId != 1 {
		t.Fatalf("unexpected session output: %+v", out.Session)
	}

	deniedResult, _, deniedErr := GetSession(context.Background(), nil, GetSessionInput{SessionId: 2})
	if deniedErr == nil {
		t.Fatal("expected fixer cross-project access denial")
	}
	if deniedResult == nil || !deniedResult.IsError {
		t.Fatal("expected MCP error result for fixer cross-project access denial")
	}
	if !strings.Contains(deniedErr.Error(), "session not found in current project") {
		t.Fatalf("unexpected cross-project denial: %v", deniedErr)
	}

	authorizedRole = "overseer"
	authorizedProjectId = 0

	overseerResult, overseerOut, overseerErr := GetSession(context.Background(), nil, GetSessionInput{SessionId: 2})
	if overseerErr != nil {
		t.Fatalf("expected overseer success, got: %v", overseerErr)
	}
	if overseerResult != nil {
		t.Fatalf("expected nil call result on overseer success, got: %+v", overseerResult)
	}
	if overseerOut.Session.Id != 2 || overseerOut.Session.ProjectId != 2 {
		t.Fatalf("unexpected overseer session output: %+v", overseerOut.Session)
	}
}

func TestProjectScopedSessionIDs_DenseAndIsolated(t *testing.T) {
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
	authorizedRole = "fixer"
	authorizedProjectId = 1

	if _, err := db.Exec("INSERT INTO session (project_id, task_description, status) VALUES (2, 'Task B2', 'pending')"); err != nil {
		t.Fatalf("seed extra project-2 session: %v", err)
	}
	if _, err := db.Exec("INSERT INTO session (project_id, task_description, status) VALUES (1, 'Task A2', 'pending')"); err != nil {
		t.Fatalf("seed extra project-1 session: %v", err)
	}

	var globalA2 int
	if err := db.QueryRow("SELECT id FROM session WHERE project_id = 1 AND task_description = 'Task A2'").Scan(&globalA2); err != nil {
		t.Fatalf("resolve global id for Task A2: %v", err)
	}

	_, createdTask, createErr := CreateTask(context.Background(), nil, CreateTaskInput{TaskDescription: "Task A3"})
	if createErr != nil {
		t.Fatalf("create_task failed: %v", createErr)
	}
	if createdTask.SessionId != 3 {
		t.Fatalf("expected project-local session_id=3 after interleaved globals, got %+v", createdTask)
	}

	authorizedRole = "netrunner"
	_, pending, pendingErr := GetPendingTasks(context.Background(), nil, GetPendingTasksInput{})
	if pendingErr != nil {
		t.Fatalf("get_pending_tasks failed: %v", pendingErr)
	}
	if len(pending.Tasks) != 3 {
		t.Fatalf("expected 3 project-1 pending tasks, got %+v", pending.Tasks)
	}
	if pending.Tasks[0].SessionId != 1 || pending.Tasks[1].SessionId != 2 || pending.Tasks[2].SessionId != 3 {
		t.Fatalf("expected dense local session ids [1,2,3], got %+v", pending.Tasks)
	}

	if _, _, checkoutErr := CheckoutTask(context.Background(), nil, CheckoutTaskInput{SessionId: 2}); checkoutErr != nil {
		t.Fatalf("checkout_task with local session_id=2 failed: %v", checkoutErr)
	}
	if authorizedSessionId != globalA2 {
		t.Fatalf("expected authorizedSessionId to store global id %d, got %d", globalA2, authorizedSessionId)
	}

	var statusA2 string
	if err := db.QueryRow("SELECT status FROM session WHERE id = ?", globalA2).Scan(&statusA2); err != nil {
		t.Fatalf("query status for Task A2: %v", err)
	}
	if statusA2 != "in_progress" {
		t.Fatalf("expected Task A2 status=in_progress, got %q", statusA2)
	}
}

func TestProjectScopedDocIDs_DenseAndRenumberedAfterDelete(t *testing.T) {
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

	if _, err := db.Exec("INSERT INTO project_doc (project_id, title, content, doc_type) VALUES (2, 'Doc D', 'Content D', 'documentation')"); err != nil {
		t.Fatalf("seed extra project-2 doc: %v", err)
	}
	if _, err := db.Exec("INSERT INTO project_doc (project_id, title, content, doc_type) VALUES (1, 'Doc E', 'Content E', 'documentation')"); err != nil {
		t.Fatalf("seed extra project-1 doc: %v", err)
	}

	var globalDocE int
	if err := db.QueryRow("SELECT id FROM project_doc WHERE project_id = 1 AND title = 'Doc E'").Scan(&globalDocE); err != nil {
		t.Fatalf("resolve global id for Doc E: %v", err)
	}

	_, beforeDelete, beforeErr := GetProjectDocs(context.Background(), nil, GetProjectDocsInput{})
	if beforeErr != nil {
		t.Fatalf("get_project_docs before delete failed: %v", beforeErr)
	}
	if len(beforeDelete.Docs) != 3 {
		t.Fatalf("expected 3 project docs before delete, got %+v", beforeDelete.Docs)
	}
	if beforeDelete.Docs[0].Id != 1 || beforeDelete.Docs[1].Id != 2 || beforeDelete.Docs[2].Id != 3 {
		t.Fatalf("expected dense local doc ids [1,2,3], got %+v", beforeDelete.Docs)
	}

	if _, _, deleteErr := DeleteProjectDoc(context.Background(), nil, DeleteProjectDocInput{DocId: 2}); deleteErr != nil {
		t.Fatalf("delete_project_doc with local doc_id=2 failed: %v", deleteErr)
	}

	_, afterDelete, afterErr := GetProjectDocs(context.Background(), nil, GetProjectDocsInput{})
	if afterErr != nil {
		t.Fatalf("get_project_docs after delete failed: %v", afterErr)
	}
	if len(afterDelete.Docs) != 2 {
		t.Fatalf("expected 2 docs after delete, got %+v", afterDelete.Docs)
	}
	if afterDelete.Docs[0].Id != 1 || afterDelete.Docs[1].Id != 2 {
		t.Fatalf("expected dense local doc ids [1,2] after delete, got %+v", afterDelete.Docs)
	}
	if afterDelete.Docs[1].Title != "Doc E" {
		t.Fatalf("expected Doc E to be renumbered to local id 2, got %+v", afterDelete.Docs[1])
	}

	if _, _, updateErr := UpdateProjectDoc(context.Background(), nil, UpdateProjectDocInput{
		DocId:   2,
		Content: "Doc E Updated",
	}); updateErr != nil {
		t.Fatalf("update_project_doc with renumbered local doc_id=2 failed: %v", updateErr)
	}

	var updatedContent string
	if err := db.QueryRow("SELECT content FROM project_doc WHERE id = ?", globalDocE).Scan(&updatedContent); err != nil {
		t.Fatalf("query updated Doc E content: %v", err)
	}
	if updatedContent != "Doc E Updated" {
		t.Fatalf("expected Doc E content updated via local doc_id mapping, got %q", updatedContent)
	}
}

func TestProjectScopedDocProposalIDs_AuditAndMapping(t *testing.T) {
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

	if _, err := db.Exec("INSERT INTO doc_proposal (project_id, session_id, status, proposed_content, proposed_doc_type) VALUES (2, 2, 'pending', 'Beta proposal', 'documentation')"); err != nil {
		t.Fatalf("seed project-2 proposal: %v", err)
	}

	authorizedRole = "netrunner"
	authorizedProjectId = 1
	authorizedSessionId = 1

	_, proposeOut, proposeErr := ProposeDocUpdate(context.Background(), nil, ProposeDocUpdateInput{
		ProposedContent:    "Alpha proposal",
		TargetProjectDocId: 1,
	})
	if proposeErr != nil {
		t.Fatalf("propose_doc_update failed: %v", proposeErr)
	}
	if proposeOut.ProposalId != 1 {
		t.Fatalf("expected project-local proposal_id=1, got %+v", proposeOut)
	}

	var alphaGlobalProposalID int
	var storedTargetProjectDocID sql.NullInt64
	if err := db.QueryRow("SELECT id, target_project_doc_id FROM doc_proposal WHERE project_id = 1 AND proposed_content = 'Alpha proposal'").Scan(&alphaGlobalProposalID, &storedTargetProjectDocID); err != nil {
		t.Fatalf("resolve global proposal id for Alpha proposal: %v", err)
	}
	if alphaGlobalProposalID == 1 {
		t.Fatalf("expected global proposal id to be interleaved after project-2 seed, got %d", alphaGlobalProposalID)
	}
	if !storedTargetProjectDocID.Valid || storedTargetProjectDocID.Int64 != 1 {
		t.Fatalf("expected target_project_doc_id=1, got %+v", storedTargetProjectDocID)
	}

	authorizedRole = "fixer"
	_, reviewOut, reviewErr := ReviewDocProposals(context.Background(), nil, ReviewDocProposalsInput{})
	if reviewErr != nil {
		t.Fatalf("review_doc_proposals failed: %v", reviewErr)
	}
	if len(reviewOut.Proposals) != 1 {
		t.Fatalf("expected only project-1 pending proposal, got %+v", reviewOut.Proposals)
	}
	if reviewOut.Proposals[0].Id != 1 || reviewOut.Proposals[0].SessionId != 1 {
		t.Fatalf("expected local proposal/session ids [1,1], got %+v", reviewOut.Proposals[0])
	}
	if reviewOut.Proposals[0].TargetProjectDocId != 1 {
		t.Fatalf("expected local target_project_doc_id=1, got %+v", reviewOut.Proposals[0])
	}

	if _, _, statusErr := SetDocProposalStatus(context.Background(), nil, SetDocProposalStatusInput{
		ProposalId: 1,
		Status:     "rejected",
	}); statusErr != nil {
		t.Fatalf("set_doc_proposal_status by local proposal_id failed: %v", statusErr)
	}

	var alphaStatus string
	if err := db.QueryRow("SELECT status FROM doc_proposal WHERE id = ?", alphaGlobalProposalID).Scan(&alphaStatus); err != nil {
		t.Fatalf("query Alpha proposal status: %v", err)
	}
	if alphaStatus != "rejected" {
		t.Fatalf("expected Alpha proposal status rejected, got %q", alphaStatus)
	}
}

func TestSetDocProposalStatus_ApprovedTargetedProposalUpdatesOnlyTargetDoc(t *testing.T) {
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

	if _, err := testDB.Exec("INSERT INTO project_doc (project_id, title, content, doc_type) VALUES (1, 'Doc D', 'Content D', 'documentation')"); err != nil {
		t.Fatalf("seed second documentation doc: %v", err)
	}

	db = testDB
	authorizedRole = "netrunner"
	authorizedProjectId = 1
	authorizedSessionId = 1

	if _, _, err := ProposeDocUpdate(context.Background(), nil, ProposeDocUpdateInput{
		ProposedContent:    "Targeted content",
		ProposedDocType:    "documentation",
		TargetProjectDocId: 3,
	}); err != nil {
		t.Fatalf("propose_doc_update failed: %v", err)
	}

	authorizedRole = "fixer"

	if _, _, err := SetDocProposalStatus(context.Background(), nil, SetDocProposalStatusInput{
		ProposalId: 1,
		Status:     "approved",
	}); err != nil {
		t.Fatalf("set_doc_proposal_status failed: %v", err)
	}

	var docAContent, docDContent, proposalStatus string
	if err := db.QueryRow("SELECT content FROM project_doc WHERE project_id = 1 AND title = 'Doc A'").Scan(&docAContent); err != nil {
		t.Fatalf("query Doc A content: %v", err)
	}
	if err := db.QueryRow("SELECT content FROM project_doc WHERE project_id = 1 AND title = 'Doc D'").Scan(&docDContent); err != nil {
		t.Fatalf("query Doc D content: %v", err)
	}
	if err := db.QueryRow("SELECT status FROM doc_proposal WHERE project_id = 1 AND session_id = 1").Scan(&proposalStatus); err != nil {
		t.Fatalf("query proposal status: %v", err)
	}

	if docAContent != "Content A" {
		t.Fatalf("expected Doc A content unchanged, got %q", docAContent)
	}
	if docDContent != "Targeted content" {
		t.Fatalf("expected Doc D content updated, got %q", docDContent)
	}
	if proposalStatus != "approved" {
		t.Fatalf("expected proposal status approved, got %q", proposalStatus)
	}
}

func TestSetDocProposalStatus_ApprovedWithoutTargetRejectsAmbiguousDocType(t *testing.T) {
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

	if _, err := testDB.Exec("INSERT INTO project_doc (project_id, title, content, doc_type) VALUES (1, 'Doc D', 'Content D', 'documentation')"); err != nil {
		t.Fatalf("seed second documentation doc: %v", err)
	}

	db = testDB
	authorizedRole = "netrunner"
	authorizedProjectId = 1
	authorizedSessionId = 1

	if _, _, err := ProposeDocUpdate(context.Background(), nil, ProposeDocUpdateInput{
		ProposedContent: "Ambiguous content",
		ProposedDocType: "documentation",
	}); err != nil {
		t.Fatalf("propose_doc_update failed: %v", err)
	}

	authorizedRole = "fixer"

	_, _, err := SetDocProposalStatus(context.Background(), nil, SetDocProposalStatusInput{
		ProposalId: 1,
		Status:     "approved",
	})
	if err == nil {
		t.Fatal("expected ambiguous proposal approval to fail")
	}
	if !strings.Contains(err.Error(), "target_project_doc_id") {
		t.Fatalf("expected target_project_doc_id guidance, got %v", err)
	}

	var docAContent, docDContent, proposalStatus string
	if err := db.QueryRow("SELECT content FROM project_doc WHERE project_id = 1 AND title = 'Doc A'").Scan(&docAContent); err != nil {
		t.Fatalf("query Doc A content: %v", err)
	}
	if err := db.QueryRow("SELECT content FROM project_doc WHERE project_id = 1 AND title = 'Doc D'").Scan(&docDContent); err != nil {
		t.Fatalf("query Doc D content: %v", err)
	}
	if err := db.QueryRow("SELECT status FROM doc_proposal WHERE project_id = 1 AND session_id = 1").Scan(&proposalStatus); err != nil {
		t.Fatalf("query proposal status: %v", err)
	}

	if docAContent != "Content A" {
		t.Fatalf("expected Doc A content unchanged, got %q", docAContent)
	}
	if docDContent != "Content D" {
		t.Fatalf("expected Doc D content unchanged, got %q", docDContent)
	}
	if proposalStatus != "pending" {
		t.Fatalf("expected proposal to remain pending, got %q", proposalStatus)
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

func TestLaunchExplicitNetrunner_RejectsOverlappingWriteScopeWithoutOverride(t *testing.T) {
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

	if _, err := testDB.Exec("UPDATE session SET declared_write_scope = '[\"fixer_mcp\"]', parallel_wave_id = 'wave-1' WHERE id = 1"); err != nil {
		t.Fatalf("seed source write scope: %v", err)
	}
	if _, err := testDB.Exec("INSERT INTO session (project_id, task_description, status, declared_write_scope, parallel_wave_id) VALUES (1, 'Task C', 'pending', '[\"fixer_mcp/main.go\"]', 'wave-1')"); err != nil {
		t.Fatalf("seed concurrent session: %v", err)
	}
	if _, err := testDB.Exec("INSERT INTO worker_process (project_id, session_id, pid, launch_epoch, status) VALUES (1, 1, ?, 0, 'running')", os.Getpid()); err != nil {
		t.Fatalf("seed active worker: %v", err)
	}

	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1

	executed := false
	execCommand = func(name string, arg ...string) *exec.Cmd {
		executed = true
		return exec.Command("true")
	}

	callResult, _, err := LaunchExplicitNetrunner(context.Background(), nil, LaunchExplicitNetrunnerInput{
		SessionId: 2,
	})
	if err == nil {
		t.Fatal("expected overlapping write scope rejection")
	}
	if callResult == nil || !callResult.IsError {
		t.Fatal("expected MCP error result")
	}
	if executed {
		t.Fatal("launcher should not run when overlap validation fails")
	}
	if !strings.Contains(err.Error(), "declared write scope overlaps active sessions") {
		t.Fatalf("unexpected error: %v", err)
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

	callResult, _, err := LaunchExplicitNetrunner(context.Background(), nil, LaunchExplicitNetrunnerInput{
		SessionId: 1,
	})
	if err == nil {
		t.Fatal("expected repair-fork guidance rejection")
	}
	if callResult == nil || !callResult.IsError {
		t.Fatal("expected MCP error result")
	}
	if !strings.Contains(err.Error(), "fork_repair_session_from") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCompleteTask_RejectsUnstructuredFinalReport(t *testing.T) {
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

	if _, err := testDB.Exec("UPDATE session SET status = 'in_progress' WHERE id = 1"); err != nil {
		t.Fatalf("seed in_progress: %v", err)
	}
	if _, err := testDB.Exec("INSERT INTO doc_proposal (project_id, session_id, status, proposed_content, proposed_doc_type) VALUES (1, 1, 'pending', 'Doc delta', 'documentation')"); err != nil {
		t.Fatalf("seed proposal: %v", err)
	}

	db = testDB
	authorizedRole = "netrunner"
	authorizedProjectId = 1
	authorizedSessionId = 1

	callResult, _, err := CompleteTask(context.Background(), nil, CompleteTaskInput{
		SessionId:   1,
		FinalReport: "plain text is no longer acceptable",
	})
	if err == nil {
		t.Fatal("expected final report schema rejection")
	}
	if callResult == nil || !callResult.IsError {
		t.Fatal("expected MCP error result")
	}
	if !strings.Contains(err.Error(), "final_report must be valid JSON") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCompleteTask_RejectsStaleOrchestrationEpoch(t *testing.T) {
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

	if _, err := testDB.Exec("UPDATE session SET status = 'in_progress' WHERE id = 1"); err != nil {
		t.Fatalf("seed in_progress: %v", err)
	}
	if _, err := testDB.Exec("INSERT INTO doc_proposal (project_id, session_id, status, proposed_content, proposed_doc_type) VALUES (1, 1, 'pending', 'Doc delta', 'documentation')"); err != nil {
		t.Fatalf("seed proposal: %v", err)
	}
	if _, err := testDB.Exec("INSERT INTO worker_process (project_id, session_id, pid, launch_epoch, status) VALUES (1, 1, ?, 1, 'running')", os.Getpid()); err != nil {
		t.Fatalf("seed worker_process: %v", err)
	}
	if _, err := testDB.Exec("INSERT INTO autonomous_run_status (project_id, session_id, state, summary, orchestration_epoch, orchestration_frozen, notifications_enabled_for_active_run) VALUES (1, 1, 'running', 'Epoch advanced', 2, 0, 1)"); err != nil {
		t.Fatalf("seed autonomous status: %v", err)
	}

	db = testDB
	authorizedRole = "netrunner"
	authorizedProjectId = 1
	authorizedSessionId = 1

	callResult, _, err := CompleteTask(context.Background(), nil, CompleteTaskInput{
		SessionId:   1,
		FinalReport: structuredTestFinalReport,
	})
	if err == nil {
		t.Fatal("expected stale epoch rejection")
	}
	if callResult == nil || !callResult.IsError {
		t.Fatal("expected MCP error result")
	}
	if !strings.Contains(err.Error(), "launched under orchestration epoch 1") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWaitForNetrunnerSession_FollowUpBlockedWhenFrozenOrEpochStale(t *testing.T) {
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
	if out.Result.FollowUpAllowed {
		t.Fatalf("expected follow-up to be blocked, got %+v", out.Result)
	}
	if out.Result.LaunchEpoch != 1 || out.Result.CurrentEpoch != 2 || !out.Result.OrchestrationFrozen {
		t.Fatalf("unexpected wait epoch/freeze metadata: %+v", out.Result)
	}
	if !strings.Contains(out.Result.FollowUpBlockedReason, "project_orchestration_frozen") || !strings.Contains(out.Result.FollowUpBlockedReason, "stale_orchestration_epoch:1->2") {
		t.Fatalf("unexpected follow-up block reason: %+v", out.Result)
	}
}

func TestSetSessionStatus_BlocksWhenOrchestrationFrozen(t *testing.T) {
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

	if _, err := testDB.Exec("UPDATE session SET status = 'review' WHERE id = 1"); err != nil {
		t.Fatalf("seed review status: %v", err)
	}
	if _, err := testDB.Exec("INSERT INTO autonomous_run_status (project_id, session_id, state, summary, orchestration_epoch, orchestration_frozen, notifications_enabled_for_active_run) VALUES (1, 1, 'blocked', 'Frozen', 1, 1, 0)"); err != nil {
		t.Fatalf("seed autonomous status: %v", err)
	}

	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1

	callResult, _, err := SetSessionStatus(context.Background(), nil, SetSessionStatusInput{
		SessionId: 1,
		Status:    "completed",
	})
	if err == nil {
		t.Fatal("expected frozen orchestration rejection")
	}
	if callResult == nil || !callResult.IsError {
		t.Fatal("expected MCP error result")
	}
	if !strings.Contains(err.Error(), "orchestration is frozen") {
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

func TestForkRepairSessionFrom_CopiesContextAndProvenance(t *testing.T) {
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

	if _, err := testDB.Exec("UPDATE session SET declared_write_scope = '[\"fixer_mcp/main.go\"]' WHERE id = 1"); err != nil {
		t.Fatalf("seed write scope: %v", err)
	}
	if _, err := testDB.Exec("INSERT INTO netrunner_attached_doc (session_id, project_doc_id) VALUES (1, 1), (1, 2)"); err != nil {
		t.Fatalf("seed attached docs: %v", err)
	}
	if _, err := testDB.Exec("INSERT INTO session_mcp_server (session_id, mcp_server_id) VALUES (1, 1)"); err != nil {
		t.Fatalf("seed session mcp server: %v", err)
	}

	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1

	_, out, err := ForkRepairSessionFrom(context.Background(), nil, ForkRepairSessionFromInput{
		SessionId: 1,
		Reason:    "forced stop",
	})
	if err != nil {
		t.Fatalf("fork_repair_session_from failed: %v", err)
	}
	if out.NewSessionId != 2 {
		t.Fatalf("expected new local session id 2, got %+v", out)
	}

	var description, declaredWriteScope string
	var repairSourceID, attachedDocCount, mcpCount int
	if err := db.QueryRow("SELECT task_description, declared_write_scope, repair_source_session_id FROM session WHERE id = 3").Scan(&description, &declaredWriteScope, &repairSourceID); err != nil {
		t.Fatalf("query forked session: %v", err)
	}
	if repairSourceID != 1 {
		t.Fatalf("expected repair provenance to point at session 1, got %d", repairSourceID)
	}
	if !strings.Contains(description, "Repair fork source session: 1.") || !strings.Contains(description, "Repair fork reason: forced stop") {
		t.Fatalf("unexpected repair provenance in task description: %q", description)
	}
	if declaredWriteScope != "[\"fixer_mcp/main.go\"]" {
		t.Fatalf("expected copied write scope, got %q", declaredWriteScope)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM netrunner_attached_doc WHERE session_id = 3").Scan(&attachedDocCount); err != nil {
		t.Fatalf("count copied docs: %v", err)
	}
	if attachedDocCount != 2 {
		t.Fatalf("expected 2 copied docs, got %d", attachedDocCount)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM session_mcp_server WHERE session_id = 3").Scan(&mcpCount); err != nil {
		t.Fatalf("count copied MCP servers: %v", err)
	}
	if mcpCount != 1 {
		t.Fatalf("expected 1 copied MCP server, got %d", mcpCount)
	}
}

func TestListAndStopActiveWorkerProcesses_FreezesOrchestration(t *testing.T) {
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

	worker := exec.Command("sleep", "30")
	if err := worker.Start(); err != nil {
		t.Fatalf("start worker process: %v", err)
	}
	defer func() {
		_ = worker.Process.Kill()
		_, _ = worker.Process.Wait()
	}()

	if _, err := testDB.Exec("INSERT INTO worker_process (project_id, session_id, pid, launch_epoch, status) VALUES (1, 1, ?, 0, 'running')", worker.Process.Pid); err != nil {
		t.Fatalf("seed worker_process: %v", err)
	}

	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1

	_, listed, err := ListActiveWorkerProcesses(context.Background(), nil, ListActiveWorkerProcessesInput{})
	if err != nil {
		t.Fatalf("list_active_worker_processes failed: %v", err)
	}
	if len(listed.Processes) != 1 || listed.Processes[0].SessionID != 1 || !listed.Processes[0].Alive {
		t.Fatalf("unexpected listed processes: %+v", listed.Processes)
	}

	_, stopped, err := StopActiveWorkerProcesses(context.Background(), nil, StopActiveWorkerProcessesInput{
		SessionIds:          []int{1},
		FreezeOrchestration: true,
		Reason:              "operator stop",
	})
	if err != nil {
		t.Fatalf("stop_active_worker_processes failed: %v", err)
	}
	if stopped.StoppedProcessCount != 1 || !stopped.FreezeApplied || stopped.OrchestrationEpoch != 1 {
		t.Fatalf("unexpected stop output: %+v", stopped)
	}

	var epoch, frozen, notificationsEnabled, forcedStopCount int
	var workerStatus string
	if err := db.QueryRow("SELECT orchestration_epoch, orchestration_frozen, notifications_enabled_for_active_run FROM autonomous_run_status WHERE project_id = 1").Scan(&epoch, &frozen, &notificationsEnabled); err != nil {
		t.Fatalf("query autonomous status: %v", err)
	}
	if epoch != 1 || frozen != 1 || notificationsEnabled != 0 {
		t.Fatalf("unexpected autonomous freeze state: epoch=%d frozen=%d notifications=%d", epoch, frozen, notificationsEnabled)
	}
	if err := db.QueryRow("SELECT forced_stop_count FROM session WHERE id = 1").Scan(&forcedStopCount); err != nil {
		t.Fatalf("query forced_stop_count: %v", err)
	}
	if forcedStopCount != 1 {
		t.Fatalf("expected forced_stop_count 1, got %d", forcedStopCount)
	}
	if err := db.QueryRow("SELECT status FROM worker_process WHERE session_id = 1").Scan(&workerStatus); err != nil {
		t.Fatalf("query worker_process status: %v", err)
	}
	if workerStatus == workerStatusRunning {
		t.Fatalf("expected worker process to be stopped or exited, got %q", workerStatus)
	}
}
