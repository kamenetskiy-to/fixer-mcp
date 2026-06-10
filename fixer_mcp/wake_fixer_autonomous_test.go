package main

import (
	"context"
	"database/sql"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

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

func TestWakeFixerAutonomous_ResumesFixerFromCheckedOutSession(t *testing.T) {
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

	projectCWD := t.TempDir()

	testDB := setupWakeFixerAutonomousTestDB(t, projectCWD)
	defer func() {
		_ = testDB.Close()
	}()

	db = testDB
	authorizedRole = "netrunner"
	authorizedProjectId = 1
	authorizedSessionId = 1

	var gotName string
	var gotArgs []string
	execCommand = func(name string, arg ...string) *exec.Cmd {
		gotName = name
		gotArgs = append([]string{}, arg...)
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
	if gotName != "python3" {
		t.Fatalf("expected python3 launcher, got %q", gotName)
	}
	if len(gotArgs) < 6 || gotArgs[1] != "resume-fixer" {
		t.Fatalf("unexpected launcher args: %+v", gotArgs)
	}
	if out.SessionId != 1 {
		t.Fatalf("expected resolved session id 1, got %+v", out)
	}
	expectedLauncher, resolveErr := resolveExplicitLauncherScript()
	if resolveErr != nil {
		t.Fatalf("resolve expected launcher: %v", resolveErr)
	}
	if out.LauncherScript != expectedLauncher {
		t.Fatalf("unexpected launcher script path: %+v", out)
	}
	if out.LauncherScript == filepath.Join(projectCWD, "client_wires", "fixer_autonomous.py") {
		t.Fatalf("wake fixer should not resolve launcher under project cwd for scratch projects: %+v", out)
	}
}

func TestWakeFixerAutonomous_SuppressesExplicitLaunchWorkerSession(t *testing.T) {
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

	projectCWD := t.TempDir()

	testDB := setupWakeFixerAutonomousTestDB(t, projectCWD)
	defer func() {
		_ = testDB.Close()
	}()
	if _, err := testDB.Exec("INSERT INTO worker_process (project_id, session_id, pid, launch_epoch, launch_origin, status) VALUES (1, 1, 0, 0, 'explicit-wait', 'exited')"); err != nil {
		t.Fatalf("seed worker process: %v", err)
	}

	db = testDB
	authorizedRole = "netrunner"
	authorizedProjectId = 1
	authorizedSessionId = 1

	execCommand = func(name string, arg ...string) *exec.Cmd {
		t.Fatalf("wake should not spawn resume-fixer for explicit launch/wait session")
		return exec.Command("true")
	}

	callResult, out, err := WakeFixerAutonomous(context.Background(), nil, WakeFixerAutonomousInput{
		Summary: "worker finished cleanly",
	})
	if err != nil {
		t.Fatalf("wake_fixer_autonomous suppression should not error: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil call result on suppression, got %+v", callResult)
	}
	if out.Status != "suppressed" || out.SpawnedBackground {
		t.Fatalf("expected suppressed non-spawn result, got %+v", out)
	}
	if !strings.Contains(out.SuppressedReason, "explicit launch/wait") {
		t.Fatalf("expected explicit launch/wait reason, got %+v", out)
	}
}

func TestWakeFixerAutonomous_DoesNotSuppressUnmarkedWorkerProcess(t *testing.T) {
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

	projectCWD := t.TempDir()

	testDB := setupWakeFixerAutonomousTestDB(t, projectCWD)
	defer func() {
		_ = testDB.Close()
	}()
	if _, err := testDB.Exec("INSERT INTO worker_process (project_id, session_id, pid, launch_epoch, launch_origin, status) VALUES (1, 1, 0, 0, '', 'exited')"); err != nil {
		t.Fatalf("seed worker process: %v", err)
	}

	db = testDB
	authorizedRole = "netrunner"
	authorizedProjectId = 1
	authorizedSessionId = 1

	executed := false
	execCommand = func(name string, arg ...string) *exec.Cmd {
		executed = true
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
	if !executed || out.Status != "success" || !out.SpawnedBackground {
		t.Fatalf("expected non-explicit worker process to allow wake, got executed=%v out=%+v", executed, out)
	}
}

func TestWakeFixerAutonomous_RequiresNetrunnerRole(t *testing.T) {
	originalRole := authorizedRole
	defer func() {
		authorizedRole = originalRole
	}()

	authorizedRole = "fixer"
	callResult, _, err := WakeFixerAutonomous(context.Background(), nil, WakeFixerAutonomousInput{})
	if err == nil {
		t.Fatal("expected role error")
	}
	if callResult == nil || !callResult.IsError {
		t.Fatal("expected MCP error result")
	}
	if !strings.Contains(err.Error(), "requires netrunner role") {
		t.Fatalf("unexpected error: %v", err)
	}
}
