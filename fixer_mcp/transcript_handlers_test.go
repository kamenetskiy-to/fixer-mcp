package main

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupTranscriptPathTestDB(t *testing.T, projectOneCWD string, projectTwoCWD string) *sql.DB {
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
			cli_reasoning TEXT NOT NULL DEFAULT ''
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
		INSERT INTO project (id, name, cwd) VALUES
			(1, 'One', ?),
			(2, 'Two', ?);
		INSERT INTO session (id, project_id, task_description, status, cli_backend) VALUES
			(1, 1, 'p1 codex first', 'completed', 'codex'),
			(2, 2, 'p2 droid first', 'completed', 'droid'),
			(3, 1, 'p1 codex second', 'completed', 'codex'),
			(4, 1, 'p1 droid third', 'completed', 'droid'),
			(5, 1, 'p1 missing fourth', 'completed', 'codex'),
			(6, 1, 'p1 droid no external fifth', 'completed', 'droid');
		INSERT INTO session_external_link (session_id, backend, external_session_id) VALUES
			(2, 'droid', 'droid-project-two'),
			(3, 'codex', 'codex-project-one-second'),
			(4, 'droid', 'droid-project-one-third'),
			(5, 'codex', 'codex-missing-fourth');
		INSERT INTO session_codex_link (session_id, codex_session_id) VALUES
			(3, 'codex-project-one-second'),
			(5, 'codex-missing-fourth');
	`, projectOneCWD, projectTwoCWD)
	if err != nil {
		_ = testDB.Close()
		t.Fatalf("seed transcript lookup db: %v", err)
	}

	return testDB
}

func TestGetNetrunnerTranscriptPathCodexUsesProjectScopedSessionID(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	originalCodexRoot := codexSessionTranscriptRoot
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
		codexSessionTranscriptRoot = originalCodexRoot
	}()

	codexRoot := filepath.Join(t.TempDir(), ".codex", "sessions")
	transcriptPath := filepath.Join(codexRoot, "2026", "05", "23", "rollout-2026-05-23T12-00-00-codex-project-one-second.jsonl")
	if err := os.MkdirAll(filepath.Dir(transcriptPath), 0o755); err != nil {
		t.Fatalf("mkdir codex transcript dir: %v", err)
	}
	if err := os.WriteFile(transcriptPath, []byte("{\"type\":\"session_meta\"}\n"), 0o644); err != nil {
		t.Fatalf("write codex transcript: %v", err)
	}
	codexSessionTranscriptRoot = codexRoot

	testDB := setupTranscriptPathTestDB(t, "/tmp/project-one", "/tmp/project-two")
	defer func() {
		_ = testDB.Close()
	}()
	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1

	callResult, out, err := GetNetrunnerTranscriptPath(context.Background(), nil, GetNetrunnerTranscriptPathInput{SessionId: 2})
	if err != nil {
		t.Fatalf("get transcript path failed: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil call result, got %+v", callResult)
	}
	if out.SessionId != 2 || out.GlobalSessionId != 3 {
		t.Fatalf("expected project-scoped session 2 to map to global 3, got local=%d global=%d", out.SessionId, out.GlobalSessionId)
	}
	if out.Backend != "codex" || out.ExternalSessionId != "codex-project-one-second" {
		t.Fatalf("unexpected backend/external id: %+v", out)
	}
	if !out.Found || !out.Exists || !out.Readable || out.TranscriptPath != transcriptPath {
		t.Fatalf("expected readable codex transcript path %q, got %+v", transcriptPath, out)
	}
	if out.FileSizeBytes <= 0 || out.ModifiedAt == "" {
		t.Fatalf("expected file metadata, got %+v", out)
	}
	if strings.Contains(out.OperatorHint, "{\"type\"") {
		t.Fatalf("operator hint must not contain transcript content: %q", out.OperatorHint)
	}
}

func TestGetNetrunnerTranscriptPathDroidUsesFactorySessionPath(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	originalDroidRoot := droidSessionTranscriptRoot
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
		droidSessionTranscriptRoot = originalDroidRoot
	}()

	projectCWD := filepath.Join(t.TempDir(), "project-one")
	droidRoot := filepath.Join(t.TempDir(), ".factory", "sessions")
	droidSessionTranscriptRoot = droidRoot
	transcriptPath := filepath.Join(droidRoot, droidProjectTranscriptDirName(projectCWD), "droid-project-one-third.jsonl")
	if err := os.MkdirAll(filepath.Dir(transcriptPath), 0o755); err != nil {
		t.Fatalf("mkdir droid transcript dir: %v", err)
	}
	if err := os.WriteFile(transcriptPath, []byte("{\"type\":\"session_start\",\"id\":\"droid-project-one-third\",\"cwd\":\""+projectCWD+"\"}\n"), 0o644); err != nil {
		t.Fatalf("write droid transcript: %v", err)
	}

	testDB := setupTranscriptPathTestDB(t, projectCWD, filepath.Join(t.TempDir(), "project-two"))
	defer func() {
		_ = testDB.Close()
	}()
	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1

	_, out, err := GetNetrunnerTranscriptPath(context.Background(), nil, GetNetrunnerTranscriptPathInput{SessionId: 3})
	if err != nil {
		t.Fatalf("get transcript path failed: %v", err)
	}
	if out.Backend != "droid" || out.GlobalSessionId != 4 {
		t.Fatalf("expected droid global session 4, got %+v", out)
	}
	if !out.Found || !out.Exists || !out.Readable || out.TranscriptPath != transcriptPath {
		t.Fatalf("expected readable droid transcript path %q, got %+v", transcriptPath, out)
	}
}

func TestGetNetrunnerTranscriptPathDroidDiscoversMissingExternalIDFromProjectTranscript(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	originalDroidRoot := droidSessionTranscriptRoot
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
		droidSessionTranscriptRoot = originalDroidRoot
	}()

	projectCWD := filepath.Join(t.TempDir(), "project-one")
	droidRoot := filepath.Join(t.TempDir(), ".factory", "sessions")
	droidSessionTranscriptRoot = droidRoot
	transcriptPath := filepath.Join(droidRoot, droidProjectTranscriptDirName(projectCWD), "droid-before-checkout.jsonl")
	if err := os.MkdirAll(filepath.Dir(transcriptPath), 0o755); err != nil {
		t.Fatalf("mkdir droid transcript dir: %v", err)
	}
	if err := os.WriteFile(transcriptPath, []byte("{\"type\":\"session_start\",\"cwd\":\""+projectCWD+"\"}\n"), 0o644); err != nil {
		t.Fatalf("write droid transcript: %v", err)
	}

	testDB := setupTranscriptPathTestDB(t, projectCWD, filepath.Join(t.TempDir(), "project-two"))
	defer func() {
		_ = testDB.Close()
	}()
	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1

	_, out, err := GetNetrunnerTranscriptPath(context.Background(), nil, GetNetrunnerTranscriptPathInput{SessionId: 5})
	if err != nil {
		t.Fatalf("get transcript path failed: %v", err)
	}
	if out.Backend != "droid" || out.GlobalSessionId != 6 {
		t.Fatalf("expected droid global session 6, got %+v", out)
	}
	if out.ExternalSessionId != "droid-before-checkout" {
		t.Fatalf("expected discovered external id from transcript filename, got %+v", out)
	}
	if !out.Found || !out.Exists || !out.Readable || out.TranscriptPath != transcriptPath {
		t.Fatalf("expected readable droid transcript path %q, got %+v", transcriptPath, out)
	}
	if !strings.Contains(strings.Join(out.SearchDiagnostics, "\n"), "no external session id persisted yet") {
		t.Fatalf("expected missing external id diagnostic, got %+v", out.SearchDiagnostics)
	}

	persisted, err := fetchSessionExternalID(6, "droid")
	if err != nil {
		t.Fatalf("fetch persisted droid external id: %v", err)
	}
	if persisted != "droid-before-checkout" {
		t.Fatalf("expected discovered droid id to be persisted, got %q", persisted)
	}
}

func TestGetNetrunnerTranscriptPathMissingTranscriptReturnsDiagnostics(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	originalCodexRoot := codexSessionTranscriptRoot
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
		codexSessionTranscriptRoot = originalCodexRoot
	}()

	codexSessionTranscriptRoot = filepath.Join(t.TempDir(), ".codex", "sessions")
	if err := os.MkdirAll(codexSessionTranscriptRoot, 0o755); err != nil {
		t.Fatalf("mkdir codex root: %v", err)
	}
	testDB := setupTranscriptPathTestDB(t, "/tmp/project-one", "/tmp/project-two")
	defer func() {
		_ = testDB.Close()
	}()
	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1

	_, out, err := GetNetrunnerTranscriptPath(context.Background(), nil, GetNetrunnerTranscriptPathInput{SessionId: 4})
	if err != nil {
		t.Fatalf("missing transcript lookup should not fail: %v", err)
	}
	if out.Found || out.Exists || out.Readable || out.TranscriptPath != "" {
		t.Fatalf("expected missing transcript metadata only, got %+v", out)
	}
	if len(out.SearchDiagnostics) == 0 || !strings.Contains(strings.Join(out.SearchDiagnostics, "\n"), "codex-missing-fourth") {
		t.Fatalf("expected concise missing diagnostics with external id, got %+v", out.SearchDiagnostics)
	}
}

func TestGetNetrunnerTranscriptPathMissingExternalIDDiagnosticDiffersFromMissingTranscript(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	originalCodexRoot := codexSessionTranscriptRoot
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
		codexSessionTranscriptRoot = originalCodexRoot
	}()

	codexSessionTranscriptRoot = filepath.Join(t.TempDir(), ".codex", "sessions")
	if err := os.MkdirAll(codexSessionTranscriptRoot, 0o755); err != nil {
		t.Fatalf("mkdir codex root: %v", err)
	}
	testDB := setupTranscriptPathTestDB(t, "/tmp/project-one", "/tmp/project-two")
	defer func() {
		_ = testDB.Close()
	}()
	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1

	_, out, err := GetNetrunnerTranscriptPath(context.Background(), nil, GetNetrunnerTranscriptPathInput{SessionId: 1})
	if err != nil {
		t.Fatalf("missing external id lookup should not fail: %v", err)
	}
	diagnostics := strings.Join(out.SearchDiagnostics, "\n")
	if !strings.Contains(diagnostics, "no external session id persisted yet") {
		t.Fatalf("expected missing external id diagnostic, got %+v", out.SearchDiagnostics)
	}
	if strings.Contains(diagnostics, "external session id is empty; cannot resolve") {
		t.Fatalf("expected diagnostic to avoid old ambiguous empty-id wording, got %+v", out.SearchDiagnostics)
	}
}

func TestGetNetrunnerTranscriptPathOverseerRequiresProjectAndMapsLocalIDs(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	originalCodexRoot := codexSessionTranscriptRoot
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
		codexSessionTranscriptRoot = originalCodexRoot
	}()

	codexRoot := filepath.Join(t.TempDir(), ".codex", "sessions")
	transcriptPath := filepath.Join(codexRoot, "2026", "05", "23", "rollout-2026-05-23T12-00-00-codex-project-one-second.jsonl")
	if err := os.MkdirAll(filepath.Dir(transcriptPath), 0o755); err != nil {
		t.Fatalf("mkdir codex transcript dir: %v", err)
	}
	if err := os.WriteFile(transcriptPath, []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write codex transcript: %v", err)
	}
	codexSessionTranscriptRoot = codexRoot

	testDB := setupTranscriptPathTestDB(t, "/tmp/project-one", "/tmp/project-two")
	defer func() {
		_ = testDB.Close()
	}()
	db = testDB
	authorizedRole = "overseer"
	authorizedProjectId = 0

	callResult, _, err := GetNetrunnerTranscriptPath(context.Background(), nil, GetNetrunnerTranscriptPathInput{SessionId: 2})
	if err == nil || callResult == nil || !callResult.IsError {
		t.Fatalf("expected overseer lookup without project_id to fail")
	}

	callResult, out, err := GetNetrunnerTranscriptPath(context.Background(), nil, GetNetrunnerTranscriptPathInput{ProjectId: 1, SessionId: 2})
	if err != nil {
		t.Fatalf("overseer project-scoped lookup failed: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil call result, got %+v", callResult)
	}
	if out.ProjectId != 1 || out.SessionId != 2 || out.GlobalSessionId != 3 || out.TranscriptPath != transcriptPath {
		t.Fatalf("expected overseer to map project 1 local session 2 to global 3, got %+v", out)
	}
}

func TestGetNetrunnerTranscriptPathRejectsNetrunnerRole(t *testing.T) {
	originalRole := authorizedRole
	defer func() {
		authorizedRole = originalRole
	}()

	authorizedRole = "netrunner"
	callResult, _, err := GetNetrunnerTranscriptPath(context.Background(), nil, GetNetrunnerTranscriptPathInput{SessionId: 1})
	if err == nil || callResult == nil || !callResult.IsError {
		t.Fatalf("expected netrunner role to be rejected")
	}
}
