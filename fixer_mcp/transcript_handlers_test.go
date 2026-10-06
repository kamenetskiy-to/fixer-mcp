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
			(6, 1, 'p1 droid no external fifth', 'completed', 'droid'),
			(7, 1, 'p1 pending Hands manual sixth', 'pending', 'codex'),
			(8, 1, 'p1 antigravity seventh', 'completed', 'antigravity'),
			(9, 1, 'p1 commandcode eighth', 'completed', 'commandcode'),
			(10, 1, 'p1 pi ninth', 'completed', 'pi'),
			(11, 1, 'p1 commandcode worktree tenth', 'completed', 'commandcode'),
			(12, 1, 'p1 pi worktree eleventh', 'completed', 'pi'),
			(13, 1, 'p1 commandcode missing twelfth', 'completed', 'commandcode'),
			(14, 1, 'p1 pi missing thirteenth', 'completed', 'pi'),
			(15, 1, 'p1 pi anchor fourteenth', 'completed', 'pi'),
			(16, 1, 'p1 pi manifest-contradictory fifteenth', 'completed', 'pi'),
			(17, 1, 'p1 pi shared-cwd sixteenth', 'completed', 'pi'),
			(18, 1, 'p1 pi manifest-verified seventeenth', 'completed', 'pi'),
			(19, 1, 'p1 pi stale-link eighteenth', 'completed', 'pi');
		INSERT INTO session_external_link (session_id, backend, external_session_id) VALUES
			(2, 'droid', 'droid-project-two'),
			(3, 'codex', 'codex-project-one-second'),
			(4, 'droid', 'droid-project-one-third'),
			(5, 'codex', 'codex-missing-fourth'),
			(8, 'antigravity', 'antigravity-project-one-seventh'),
			(9, 'commandcode', '8f0d2f5a-command-code-direct'),
			(10, 'pi', '9a1e3b6c-pi-direct'),
			(11, 'commandcode', 'ab2c4d7e-command-code-worktree'),
			(12, 'pi', 'bc3d5e8f-pi-worktree'),
			(13, 'commandcode', 'cd4e6f90-command-code-missing'),
			(14, 'pi', 'de5f7012-pi-missing');
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

func TestGetNetrunnerTranscriptPathAntigravityUsesBrainSessionPath(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	originalAntigravityRoot := antigravitySessionTranscriptRoot
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
		antigravitySessionTranscriptRoot = originalAntigravityRoot
	}()

	antigravityRoot := filepath.Join(t.TempDir(), ".gemini", "antigravity-cli", "brain")
	transcriptPath := filepath.Join(antigravityRoot, "antigravity-project-one-seventh", ".system_generated", "logs", "transcript_full.jsonl")
	if err := os.MkdirAll(filepath.Dir(transcriptPath), 0o755); err != nil {
		t.Fatalf("mkdir Antigravity transcript dir: %v", err)
	}
	if err := os.WriteFile(transcriptPath, []byte("{\"type\":\"PLANNER_RESPONSE\"}\n"), 0o644); err != nil {
		t.Fatalf("write Antigravity transcript: %v", err)
	}
	antigravitySessionTranscriptRoot = antigravityRoot

	testDB := setupTranscriptPathTestDB(t, filepath.Join(t.TempDir(), "project-one"), filepath.Join(t.TempDir(), "project-two"))
	defer func() { _ = testDB.Close() }()
	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1

	_, out, err := GetNetrunnerTranscriptPath(context.Background(), nil, GetNetrunnerTranscriptPathInput{SessionId: 7})
	if err != nil {
		t.Fatalf("get Antigravity transcript path failed: %v", err)
	}
	if out.Backend != "antigravity" || out.GlobalSessionId != 8 {
		t.Fatalf("expected Antigravity global session 8, got %+v", out)
	}
	if !out.Found || !out.Exists || !out.Readable || out.TranscriptPath != transcriptPath {
		t.Fatalf("expected readable Antigravity transcript path %q, got %+v", transcriptPath, out)
	}
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

func TestGetNetrunnerTranscriptPathDroidMissingExternalIDFailsClosedWithMultipleSameCWDCandidates(t *testing.T) {
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
	transcriptDir := filepath.Join(droidRoot, droidProjectTranscriptDirName(projectCWD))
	if err := os.MkdirAll(transcriptDir, 0o755); err != nil {
		t.Fatalf("mkdir droid transcript dir: %v", err)
	}
	for _, name := range []string{"droid-before-checkout.jsonl", "droid-current.jsonl"} {
		transcriptPath := filepath.Join(transcriptDir, name)
		if err := os.WriteFile(transcriptPath, []byte("{\"type\":\"session_start\",\"id\":\""+strings.TrimSuffix(name, ".jsonl")+"\",\"cwd\":\""+projectCWD+"\"}\n"), 0o644); err != nil {
			t.Fatalf("write droid transcript: %v", err)
		}
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
	if out.ExternalSessionId != "" || out.Found || out.Exists || out.Readable || out.TranscriptPath != "" {
		t.Fatalf("expected missing external id to fail closed, got %+v", out)
	}
	if !strings.Contains(strings.Join(out.SearchDiagnostics, "\n"), "transcript identity cannot be proven") {
		t.Fatalf("expected explicit unavailable diagnostic, got %+v", out.SearchDiagnostics)
	}

	persisted, err := fetchSessionExternalID(6, "droid")
	if err != nil {
		t.Fatalf("fetch persisted droid external id: %v", err)
	}
	if persisted != "" {
		t.Fatalf("expected no inferred droid id to be persisted, got %q", persisted)
	}
}

func TestGetNetrunnerTranscriptPathPendingHandsManualSessionFailsClosed(t *testing.T) {
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

	projectCWD := filepath.Join(t.TempDir(), "project-one")
	codexRoot := filepath.Join(t.TempDir(), ".codex", "sessions")
	codexSessionTranscriptRoot = codexRoot
	for _, name := range []string{"rollout-old-pending.jsonl", "rollout-latest-pending.jsonl"} {
		transcriptPath := filepath.Join(codexRoot, "2026", "08", "31", name)
		if err := os.MkdirAll(filepath.Dir(transcriptPath), 0o755); err != nil {
			t.Fatalf("mkdir codex transcript dir: %v", err)
		}
		if err := os.WriteFile(transcriptPath, []byte("{\"type\":\"session_meta\",\"id\":\""+strings.TrimSuffix(name, ".jsonl")+"\",\"cwd\":\""+projectCWD+"\"}\n"), 0o644); err != nil {
			t.Fatalf("write codex transcript: %v", err)
		}
	}

	testDB := setupTranscriptPathTestDB(t, projectCWD, filepath.Join(t.TempDir(), "project-two"))
	defer func() { _ = testDB.Close() }()
	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1

	_, out, err := GetNetrunnerTranscriptPath(context.Background(), nil, GetNetrunnerTranscriptPathInput{SessionId: 6})
	if err != nil {
		t.Fatalf("pending Hands/manual lookup failed: %v", err)
	}
	if out.GlobalSessionId != 7 || out.ExternalSessionId != "" || out.Found || out.TranscriptPath != "" {
		t.Fatalf("expected pending Hands/manual session to remain unavailable, got %+v", out)
	}
	if !strings.Contains(strings.Join(out.SearchDiagnostics, "\n"), "transcript identity cannot be proven") {
		t.Fatalf("expected explicit unavailable diagnostic, got %+v", out.SearchDiagnostics)
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
	if !strings.Contains(diagnostics, "transcript unavailable: no persisted external session id") {
		t.Fatalf("expected explicit unavailable diagnostic, got %+v", out.SearchDiagnostics)
	}
	if strings.Contains(diagnostics, "scanning transcript store by project cwd") {
		t.Fatalf("lookup must not scan by project cwd without an external id, got %+v", out.SearchDiagnostics)
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

func TestCommandcodeAndPiTranscriptDirNamesMatchInstalledCliLayouts(t *testing.T) {
	if got := commandcodeProjectTranscriptDirName("/Users/operator/projects/demo_app/.codex/netrunner_worktrees/wave-1/session-2"); got != "users-operator-projects-demo-app-codex-netrunner-worktrees-wave-1-session-2" {
		t.Fatalf("commandcode project slug must match ~/.commandcode/projects/<slug> layout, got %q", got)
	}
	if got := commandcodeProjectTranscriptDirName("/Users/operator/projects/demo-tool"); got != "users-operator-projects-demo-tool" {
		t.Fatalf("commandcode project slug must collapse runs and lowercase, got %q", got)
	}
	if got := piProjectTranscriptDirName("/Users/operator"); got != "--Users-operator--" {
		t.Fatalf("pi session dir must match ~/.pi/agent/sessions/<dir> layout, got %q", got)
	}
	if got := piProjectTranscriptDirName("/Users/operator/projects/demo_app/.codex/netrunner_worktrees/wave-1/session-2"); got != "--Users-operator-projects-demo_app-.codex-netrunner_worktrees-wave-1-session-2--" {
		t.Fatalf("pi session dir must preserve case and separators, got %q", got)
	}
}

func writeTranscriptFixture(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir transcript dir: %v", err)
	}
	if err := os.WriteFile(path, []byte("{\"type\":\"session\"}\n"), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
}

func setupTemporaryHomeTranscriptRoots(t *testing.T) (string, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return filepath.Join(home, ".commandcode", "projects"), filepath.Join(home, ".pi", "agent", "sessions")
}

func TestGetNetrunnerTranscriptPathCommandCodeUsesProjectsStoreWithExactID(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	originalCommandcodeRoot := commandcodeSessionTranscriptRoot
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
		commandcodeSessionTranscriptRoot = originalCommandcodeRoot
	}()

	commandcodeRoot, _ := setupTemporaryHomeTranscriptRoots(t)
	commandcodeSessionTranscriptRoot = commandcodeRoot
	projectCWD := filepath.Join(t.TempDir(), "Project-One")
	worktreeCWD := filepath.Join(projectCWD, ".codex", "netrunner_worktrees", "wave-1", "session-10")

	directPath := filepath.Join(commandcodeRoot, commandcodeProjectTranscriptDirName(projectCWD), "8f0d2f5a-command-code-direct.jsonl")
	writeTranscriptFixture(t, directPath)
	worktreeDir := filepath.Join(commandcodeRoot, commandcodeProjectTranscriptDirName(worktreeCWD))
	worktreePath := filepath.Join(worktreeDir, "ab2c4d7e-command-code-worktree.jsonl")
	writeTranscriptFixture(t, worktreePath)

	// Identity decoys that sort before the real files and contain the looked-up
	// id as a substring: exact-id lookup must never return them.
	for _, decoy := range []string{
		filepath.Join(filepath.Dir(directPath), "8f0d2f5a-command-code-direct.checkpoints.jsonl"),
		filepath.Join(filepath.Dir(directPath), "prefix-8f0d2f5a-command-code-direct.jsonl"),
		filepath.Join(worktreeDir, "ab2c4d7e-command-code-worktree.checkpoints.jsonl"),
		filepath.Join(worktreeDir, "ab2c4d7e-command-code-worktree-extra.jsonl"),
	} {
		writeTranscriptFixture(t, decoy)
	}

	testDB := setupTranscriptPathTestDB(t, projectCWD, filepath.Join(t.TempDir(), "project-two"))
	defer func() { _ = testDB.Close() }()
	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1

	_, directOut, err := GetNetrunnerTranscriptPath(context.Background(), nil, GetNetrunnerTranscriptPathInput{SessionId: 8})
	if err != nil {
		t.Fatalf("commandcode direct lookup failed: %v", err)
	}
	if directOut.Backend != "commandcode" || directOut.GlobalSessionId != 9 || directOut.ExternalSessionId != "8f0d2f5a-command-code-direct" {
		t.Fatalf("unexpected commandcode direct identity: %+v", directOut)
	}
	if !directOut.Found || !directOut.Exists || !directOut.Readable || directOut.TranscriptPath != directPath {
		t.Fatalf("expected exact commandcode transcript %q, got %+v", directPath, directOut)
	}

	_, worktreeOut, err := GetNetrunnerTranscriptPath(context.Background(), nil, GetNetrunnerTranscriptPathInput{SessionId: 10})
	if err != nil {
		t.Fatalf("commandcode worktree lookup failed: %v", err)
	}
	if worktreeOut.GlobalSessionId != 11 || !worktreeOut.Found || worktreeOut.TranscriptPath != worktreePath {
		t.Fatalf("expected worktree commandcode transcript %q via exact-id discovery, got %+v", worktreePath, worktreeOut)
	}

	_, missingOut, err := GetNetrunnerTranscriptPath(context.Background(), nil, GetNetrunnerTranscriptPathInput{SessionId: 12})
	if err != nil {
		t.Fatalf("missing commandcode lookup should not fail: %v", err)
	}
	if missingOut.GlobalSessionId != 13 || missingOut.Found || missingOut.TranscriptPath != "" {
		t.Fatalf("expected missing commandcode transcript to fail closed, got %+v", missingOut)
	}
	if !strings.Contains(strings.Join(missingOut.SearchDiagnostics, "\n"), "cd4e6f90-command-code-missing.jsonl") {
		t.Fatalf("expected missing diagnostics naming the exact external id, got %+v", missingOut.SearchDiagnostics)
	}
}

func TestGetNetrunnerTranscriptPathPiUsesSessionStoreWithExactID(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	originalPiRoot := piSessionTranscriptRoot
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
		piSessionTranscriptRoot = originalPiRoot
	}()

	_, piRoot := setupTemporaryHomeTranscriptRoots(t)
	piSessionTranscriptRoot = piRoot
	projectCWD := filepath.Join(t.TempDir(), "Project-One")
	worktreeCWD := filepath.Join(projectCWD, ".codex", "netrunner_worktrees", "wave-1", "session-11")

	directPath := filepath.Join(piRoot, piProjectTranscriptDirName(projectCWD), "2026-09-13T00-20-45-085Z_9a1e3b6c-pi-direct.jsonl")
	writeTranscriptFixture(t, directPath)
	worktreeDir := filepath.Join(piRoot, piProjectTranscriptDirName(worktreeCWD))
	worktreePath := filepath.Join(worktreeDir, "2026-09-14T10-00-00-000Z_bc3d5e8f-pi-worktree.jsonl")
	writeTranscriptFixture(t, worktreePath)

	// Identity decoys that sort before the real files: a dash-separated name
	// and a different session id must never satisfy an exact-id lookup.
	for _, decoy := range []string{
		filepath.Join(filepath.Dir(directPath), "2026-09-13T00-20-45-085Z-9a1e3b6c-pi-direct.jsonl"),
		filepath.Join(filepath.Dir(directPath), "2026-09-13T00-20-45-085Z_9a1e3b6c-pi-direct-extra.jsonl"),
		filepath.Join(worktreeDir, "2026-09-14T10-00-00-000Z-bc3d5e8f-pi-worktree.jsonl"),
		filepath.Join(worktreeDir, "2026-09-14T10-00-00-000Z_bc3d5e8f-pi-worktree-extra.jsonl"),
	} {
		writeTranscriptFixture(t, decoy)
	}

	testDB := setupTranscriptPathTestDB(t, projectCWD, filepath.Join(t.TempDir(), "project-two"))
	defer func() { _ = testDB.Close() }()
	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1

	_, directOut, err := GetNetrunnerTranscriptPath(context.Background(), nil, GetNetrunnerTranscriptPathInput{SessionId: 9})
	if err != nil {
		t.Fatalf("pi direct lookup failed: %v", err)
	}
	if directOut.Backend != "pi" || directOut.GlobalSessionId != 10 || directOut.ExternalSessionId != "9a1e3b6c-pi-direct" {
		t.Fatalf("unexpected pi direct identity: %+v", directOut)
	}
	if !directOut.Found || !directOut.Exists || !directOut.Readable || directOut.TranscriptPath != directPath {
		t.Fatalf("expected exact pi transcript %q, got %+v", directPath, directOut)
	}

	_, worktreeOut, err := GetNetrunnerTranscriptPath(context.Background(), nil, GetNetrunnerTranscriptPathInput{SessionId: 11})
	if err != nil {
		t.Fatalf("pi worktree lookup failed: %v", err)
	}
	if worktreeOut.GlobalSessionId != 12 || !worktreeOut.Found || worktreeOut.TranscriptPath != worktreePath {
		t.Fatalf("expected worktree pi transcript %q via exact-id discovery, got %+v", worktreePath, worktreeOut)
	}

	_, missingOut, err := GetNetrunnerTranscriptPath(context.Background(), nil, GetNetrunnerTranscriptPathInput{SessionId: 13})
	if err != nil {
		t.Fatalf("missing pi lookup should not fail: %v", err)
	}
	if missingOut.GlobalSessionId != 14 || missingOut.Found || missingOut.TranscriptPath != "" {
		t.Fatalf("expected missing pi transcript to fail closed, got %+v", missingOut)
	}
	if !strings.Contains(strings.Join(missingOut.SearchDiagnostics, "\n"), "de5f7012-pi-missing") {
		t.Fatalf("expected missing diagnostics naming the exact external id, got %+v", missingOut.SearchDiagnostics)
	}
}

func TestSystem1AndPublicTranscriptLookupsResolveSameFile(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	originalCommandcodeRoot := commandcodeSessionTranscriptRoot
	originalPiRoot := piSessionTranscriptRoot
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
		commandcodeSessionTranscriptRoot = originalCommandcodeRoot
		piSessionTranscriptRoot = originalPiRoot
	}()

	commandcodeRoot, piRoot := setupTemporaryHomeTranscriptRoots(t)
	commandcodeSessionTranscriptRoot = commandcodeRoot
	piSessionTranscriptRoot = piRoot
	projectCWD := filepath.Join(t.TempDir(), "Project-One")
	commandcodeWorktreeCWD := filepath.Join(projectCWD, ".codex", "netrunner_worktrees", "wave-1", "session-10")
	piWorktreeCWD := filepath.Join(projectCWD, ".codex", "netrunner_worktrees", "wave-1", "session-11")

	commandcodeWorktreePath := filepath.Join(commandcodeRoot, commandcodeProjectTranscriptDirName(commandcodeWorktreeCWD), "ab2c4d7e-command-code-worktree.jsonl")
	writeTranscriptFixture(t, commandcodeWorktreePath)
	piWorktreePath := filepath.Join(piRoot, piProjectTranscriptDirName(piWorktreeCWD), "2026-09-14T10-00-00-000Z_bc3d5e8f-pi-worktree.jsonl")
	writeTranscriptFixture(t, piWorktreePath)

	testDB := setupTranscriptPathTestDB(t, projectCWD, filepath.Join(t.TempDir(), "project-two"))
	defer func() { _ = testDB.Close() }()
	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1

	cases := []struct {
		name           string
		localSessionID int
		globalID       int
		backend        string
		wantPath       string
	}{
		{"commandcode worktree", 10, 11, "commandcode", commandcodeWorktreePath},
		{"pi worktree", 11, 12, "pi", piWorktreePath},
	}
	for _, testCase := range cases {
		_, out, err := GetNetrunnerTranscriptPath(context.Background(), nil, GetNetrunnerTranscriptPathInput{SessionId: testCase.localSessionID})
		if err != nil {
			t.Fatalf("%s: public lookup failed: %v", testCase.name, err)
		}
		system1Path, diagnostics := resolveSystem1WorkerTranscript(testCase.globalID, testCase.localSessionID, 1, testCase.backend, projectCWD)
		if out.TranscriptPath != testCase.wantPath || system1Path != testCase.wantPath || out.TranscriptPath != system1Path {
			t.Fatalf(
				"%s: public and System1 must resolve the same transcript: public=%q system1=%q want=%q diagnostics=%v",
				testCase.name, out.TranscriptPath, system1Path, testCase.wantPath, diagnostics,
			)
		}
	}
}

// TestPiTranscriptHistoryLinksContinuationAttemptsChronologically proves that
// Pi continuation history is unambiguously linked to the exact external
// session id: fresh and resumed attempt files are ordered chronologically and
// a file whose session header contradicts the filename identity is refused as
// contradictory provenance, never silently included.
func TestPiTranscriptHistoryLinksContinuationAttemptsChronologically(t *testing.T) {
	originalPiRoot := piSessionTranscriptRoot
	defer func() { piSessionTranscriptRoot = originalPiRoot }()

	_, piRoot := setupTemporaryHomeTranscriptRoots(t)
	piSessionTranscriptRoot = piRoot
	projectCWD := filepath.Join(t.TempDir(), "Project-One")
	externalSessionID := "pi-cont-1"
	sessionDir := filepath.Join(piRoot, piProjectTranscriptDirName(projectCWD))
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir pi session dir: %v", err)
	}

	writeHeader := func(name string, headerID string) string {
		path := filepath.Join(sessionDir, name)
		body := strings.Join([]string{
			`{"type":"session","version":3,"id":"` + headerID + `","timestamp":"2026-10-05T10-00-00.000Z","cwd":"` + projectCWD + `"}`,
			`{"type":"message","id":"m1","message":{"role":"user","content":"hi"}}`,
			"",
		}, "\n")
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("write pi transcript fixture: %v", err)
		}
		return path
	}

	attemptOne := writeHeader("2026-10-05T10-00-00-000Z_"+externalSessionID+".jsonl", externalSessionID)
	attemptTwo := writeHeader("2026-10-05T11-00-00-000Z_"+externalSessionID+".jsonl", externalSessionID)
	writeHeader("2026-10-05T09-00-00-000Z_other-session.jsonl", "other-session")
	contradictory := writeHeader("2026-10-05T12-00-00-000Z_"+externalSessionID+".jsonl", "someone-else")

	diagnostics := []string{}
	history := resolveBackendTranscriptHistory("pi", projectCWD, externalSessionID, &diagnostics)
	if len(history) != 2 || history[0] != attemptOne || history[1] != attemptTwo {
		t.Fatalf("expected the two provable attempts in chronological order, got %v", history)
	}
	joinedDiagnostics := strings.Join(diagnostics, "\n")
	if !strings.Contains(joinedDiagnostics, "refusing contradictory provenance") || !strings.Contains(joinedDiagnostics, "someone-else") {
		t.Fatalf("expected a contradictory-provenance diagnostic for %q, got %v", contradictory, diagnostics)
	}

	// The full continuation history is read end to end as primary evidence.
	_, ledger, err := readTranscriptCoverageChunks(history, system1ReaderChunkBytes)
	if err != nil {
		t.Fatalf("coverage read over the continuation history failed: %v", err)
	}
	if ledger.TotalLines != 4 || ledger.HeadSessionID != externalSessionID {
		t.Fatalf("expected both attempts covered under one identity, got %+v", ledger)
	}
}

// ---------------------------------------------------------------------------
// Durable launcher evidence provenance: a missing or stale external session
// id must never turn an existing original transcript into missing evidence.

func writeProvenPiTranscript(t *testing.T, piRoot string, sessionCWD string, name string, headerID string, timestamp string) string {
	t.Helper()
	path := filepath.Join(piRoot, piProjectTranscriptDirName(sessionCWD), name)
	body := strings.Join([]string{
		`{"type":"session","version":3,"id":"` + headerID + `","timestamp":"` + timestamp + `","cwd":"` + sessionCWD + `"}`,
		`{"type":"message","id":"m1","message":{"role":"user","content":"hi"}}`,
		"",
	}, "\n")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir pi session dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write proven pi transcript: %v", err)
	}
	return path
}

func seedParallelWaveWorkerTable(t *testing.T, testDB *sql.DB, rows [][4]string) {
	t.Helper()
	if _, err := testDB.Exec(`CREATE TABLE parallel_wave_worker (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		wave_id INTEGER NOT NULL,
		session_id INTEGER NOT NULL,
		worktree_path TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL DEFAULT ''
	);`); err != nil {
		t.Fatalf("create wave worker table: %v", err)
	}
	for _, row := range rows {
		if _, err := testDB.Exec(
			`INSERT INTO parallel_wave_worker (wave_id, session_id, worktree_path, created_at) VALUES (?, ?, ?, ?)`,
			row[0], row[1], row[2], row[3],
		); err != nil {
			t.Fatalf("seed wave worker anchor: %v", err)
		}
	}
}

func writeAttemptManifest(t *testing.T, projectCWD string, localSessionID int, lines []string) string {
	t.Helper()
	path := attemptManifestPath(projectCWD, localSessionID, "pi")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir attempt manifest dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write attempt manifest: %v", err)
	}
	return path
}

// TestGetNetrunnerTranscriptPathPiRecoversIdentityFromWaveWorkerAnchor proves
// the missing-external-id cure is repaired, not replaced with a block: the
// original Pi JSONLs are bound by the DB-recorded per-worker worktree anchor
// with proven id/cwd/time identity (earlier attempts included, shared-cwd and
// contradictory files refused), the head is the proven worker head, and the
// recovered link is persisted so later lookups stay stable.
func TestGetNetrunnerTranscriptPathPiRecoversIdentityFromWaveWorkerAnchor(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	originalPiRoot := piSessionTranscriptRoot
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
		piSessionTranscriptRoot = originalPiRoot
	}()

	_, piRoot := setupTemporaryHomeTranscriptRoots(t)
	piSessionTranscriptRoot = piRoot
	projectCWD := filepath.Join(t.TempDir(), "Project-One")
	worktreeCWD := filepath.Join(projectCWD, ".codex", "netrunner_worktrees", "wave-871", "session-20")

	attemptOne := writeProvenPiTranscript(t, piRoot, worktreeCWD, "2026-10-05T10-00-00-000Z_attempt-one.jsonl", "attempt-one", "2026-10-05T10:00:00.000Z")
	attemptTwo := writeProvenPiTranscript(t, piRoot, worktreeCWD, "2026-10-05T11-00-00-000Z_attempt-two.jsonl", "attempt-two", "2026-10-05T11:00:00.000Z")
	// A later shared-cwd session and a contradictory header must never join
	// this worker's history even though both live in the same Pi store.
	writeProvenPiTranscript(t, piRoot, projectCWD, "2026-10-05T12-00-00-000Z_shared-cwd-run.jsonl", "shared-cwd-run", "2026-10-05T12:00:00.000Z")
	writeProvenPiTranscript(t, piRoot, worktreeCWD, "2026-10-05T13-00-00-000Z_declared-id.jsonl", "someone-else", "2026-10-05T13:00:00.000Z")

	testDB := setupTranscriptPathTestDB(t, projectCWD, filepath.Join(t.TempDir(), "project-two"))
	defer func() { _ = testDB.Close() }()
	seedParallelWaveWorkerTable(t, testDB, [][4]string{
		{"871", "15", ".codex/netrunner_worktrees/wave-871/session-20", "2026-10-05 09:55:00"},
	})
	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1

	_, out, err := GetNetrunnerTranscriptPath(context.Background(), nil, GetNetrunnerTranscriptPathInput{SessionId: 14})
	if err != nil {
		t.Fatalf("anchor-recovered lookup failed: %v", err)
	}
	if out.GlobalSessionId != 15 || !out.Found || out.TranscriptPath != attemptTwo || out.ExternalSessionId != "attempt-two" {
		t.Fatalf("expected proven worker head %q recovered, got %+v", attemptTwo, out)
	}
	joined := strings.Join(out.SearchDiagnostics, "\n")
	if !strings.Contains(joined, "recovered from durable launcher evidence") {
		t.Fatalf("expected recovery diagnostics, got %+v", out.SearchDiagnostics)
	}
	if !strings.Contains(joined, "refusing contradictory provenance") {
		t.Fatalf("expected the contradictory decoy to be diagnosed and refused, got %+v", out.SearchDiagnostics)
	}

	persisted, err := fetchSessionExternalID(15, "pi")
	if err != nil {
		t.Fatalf("fetch persisted pi external id: %v", err)
	}
	if persisted != "attempt-two" {
		t.Fatalf("expected the recovered identity to be persisted, got %q", persisted)
	}

	head, history, _ := resolveSystem1WorkerTranscriptHistory(15, 14, 1, "pi", projectCWD)
	if head != attemptTwo {
		t.Fatalf("expected System1 head %q, got %q", attemptTwo, head)
	}
	if len(history) != 2 || history[0] != attemptOne || history[1] != attemptTwo {
		t.Fatalf("expected both attempts in chronological order with the head last, got %v", history)
	}
}

// TestTranscriptProvenanceRefusesUnverifiableManifestClaims keeps the
// fail-closed contract for corrupted provenance: an attempt manifest that
// contradicts the original file, or claims an id with no proven file at all,
// must never bind a transcript (and must never persist an inferred id).
func TestTranscriptProvenanceRefusesUnverifiableManifestClaims(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	originalPiRoot := piSessionTranscriptRoot
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
		piSessionTranscriptRoot = originalPiRoot
	}()

	_, piRoot := setupTemporaryHomeTranscriptRoots(t)
	piSessionTranscriptRoot = piRoot
	projectCWD := filepath.Join(t.TempDir(), "Project-One")
	worktreeCWD := filepath.Join(projectCWD, ".codex", "netrunner_worktrees", "wave-871", "session-21")
	contradictory := writeProvenPiTranscript(t, piRoot, worktreeCWD, "2026-10-05T10-00-00-000Z_real-run.jsonl", "real-run", "2026-10-05T10:00:00.000Z")

	testDB := setupTranscriptPathTestDB(t, projectCWD, filepath.Join(t.TempDir(), "project-two"))
	defer func() { _ = testDB.Close() }()
	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1

	manifestLine := func(claim string, entries string) string {
		return `{"backend":"pi","local_session_id":15,"global_session_id":16,"external_session_id":"` + claim + `","worker_cwd":"` + worktreeCWD + `","attempt_number":1,"transcripts":[` + entries + `]}`
	}

	// A manifest claim that contradicts the original file is refused.
	writeAttemptManifest(t, projectCWD, 15, []string{
		manifestLine("claimed-run", `{"path":"`+contradictory+`","session_id":"real-run","session_cwd":"`+worktreeCWD+`"}`),
	})
	_, out, err := GetNetrunnerTranscriptPath(context.Background(), nil, GetNetrunnerTranscriptPathInput{SessionId: 15})
	if err != nil {
		t.Fatalf("contradictory manifest lookup should not hard-fail: %v", err)
	}
	if out.Found || out.TranscriptPath != "" || out.ExternalSessionId != "" {
		t.Fatalf("expected contradictory manifest provenance to fail closed, got %+v", out)
	}
	if !strings.Contains(strings.Join(out.SearchDiagnostics, "\n"), "contradictory provenance") {
		t.Fatalf("expected contradictory provenance diagnostics, got %+v", out.SearchDiagnostics)
	}

	// A manifest claim with no proven file at all is refused too.
	writeAttemptManifest(t, projectCWD, 15, []string{
		manifestLine("ghost-run", ""),
	})
	_, out, err = GetNetrunnerTranscriptPath(context.Background(), nil, GetNetrunnerTranscriptPathInput{SessionId: 15})
	if err != nil {
		t.Fatalf("ghost manifest lookup should not hard-fail: %v", err)
	}
	if out.Found || out.TranscriptPath != "" {
		t.Fatalf("expected ghost manifest provenance to fail closed, got %+v", out)
	}
	if !strings.Contains(strings.Join(out.SearchDiagnostics, "\n"), "no proven transcript file") {
		t.Fatalf("expected the missing-file claim to be diagnosed, got %+v", out.SearchDiagnostics)
	}

	persisted, err := fetchSessionExternalID(16, "pi")
	if err != nil {
		t.Fatalf("fetch persisted pi external id: %v", err)
	}
	if persisted != "" {
		t.Fatalf("expected no unproven id to be persisted, got %q", persisted)
	}
}

// TestTranscriptProvenanceUsesVerifiedManifestEvidence covers the serial
// launcher path: a verified append-only attempt manifest recovers the
// identity even without a wave worker anchor.
func TestTranscriptProvenanceUsesVerifiedManifestEvidence(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	originalPiRoot := piSessionTranscriptRoot
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
		piSessionTranscriptRoot = originalPiRoot
	}()

	_, piRoot := setupTemporaryHomeTranscriptRoots(t)
	piSessionTranscriptRoot = piRoot
	projectCWD := filepath.Join(t.TempDir(), "Project-One")
	worktreeCWD := filepath.Join(projectCWD, ".codex", "netrunner_worktrees", "wave-871", "session-22")
	manifestRun := writeProvenPiTranscript(t, piRoot, worktreeCWD, "2026-10-05T10-00-00-000Z_manifest-run.jsonl", "manifest-run", "2026-10-05T10:00:00.000Z")

	testDB := setupTranscriptPathTestDB(t, projectCWD, filepath.Join(t.TempDir(), "project-two"))
	defer func() { _ = testDB.Close() }()
	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1

	writeAttemptManifest(t, projectCWD, 17, []string{
		`{"backend":"pi","local_session_id":17,"global_session_id":18,"external_session_id":"manifest-run","worker_cwd":"` + worktreeCWD + `","attempt_number":1,"transcripts":[{"path":"` + manifestRun + `","session_id":"manifest-run","session_cwd":"` + worktreeCWD + `"}]}`,
	})

	_, out, err := GetNetrunnerTranscriptPath(context.Background(), nil, GetNetrunnerTranscriptPathInput{SessionId: 17})
	if err != nil {
		t.Fatalf("manifest-recovered lookup failed: %v", err)
	}
	if out.GlobalSessionId != 18 || !out.Found || out.TranscriptPath != manifestRun || out.ExternalSessionId != "manifest-run" {
		t.Fatalf("expected verified manifest evidence to resolve %q, got %+v", manifestRun, out)
	}
	persisted, err := fetchSessionExternalID(18, "pi")
	if err != nil {
		t.Fatalf("fetch persisted pi external id: %v", err)
	}
	if persisted != "manifest-run" {
		t.Fatalf("expected the verified manifest identity to be persisted, got %q", persisted)
	}
}

// TestTranscriptProvenanceRefusesSharedProjectCWDAnchor: a wave worker anchor
// pointing at the shared project cwd can never bind sessions by recency, so
// an unrelated shared-cwd transcript is never returned as this worker's.
func TestTranscriptProvenanceRefusesSharedProjectCWDAnchor(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	originalPiRoot := piSessionTranscriptRoot
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
		piSessionTranscriptRoot = originalPiRoot
	}()

	_, piRoot := setupTemporaryHomeTranscriptRoots(t)
	piSessionTranscriptRoot = piRoot
	projectCWD := filepath.Join(t.TempDir(), "Project-One")
	writeProvenPiTranscript(t, piRoot, projectCWD, "2026-10-05T10-00-00-000Z_shared-cwd-run.jsonl", "shared-cwd-run", "2026-10-05T10:00:00.000Z")

	testDB := setupTranscriptPathTestDB(t, projectCWD, filepath.Join(t.TempDir(), "project-two"))
	defer func() { _ = testDB.Close() }()
	seedParallelWaveWorkerTable(t, testDB, [][4]string{
		{"871", "17", projectCWD, "2026-10-05 09:55:00"},
	})
	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1

	_, out, err := GetNetrunnerTranscriptPath(context.Background(), nil, GetNetrunnerTranscriptPathInput{SessionId: 16})
	if err != nil {
		t.Fatalf("shared-cwd anchor lookup should not hard-fail: %v", err)
	}
	if out.Found || out.TranscriptPath != "" {
		t.Fatalf("expected the shared project cwd to refuse binding, got %+v", out)
	}
	if !strings.Contains(strings.Join(out.SearchDiagnostics, "\n"), "refusing cwd-only binding") {
		t.Fatalf("expected the shared-cwd refusal diagnostic, got %+v", out.SearchDiagnostics)
	}
}

// TestTranscriptProvenancePrefersProvenWorkerHeadOverStaleLink covers a
// relaunch whose detection never landed: the persisted link still points at
// the earlier attempt while the durable evidence proves a later worker head.
// The proven head and its full history win and the link is refreshed.
func TestTranscriptProvenancePrefersProvenWorkerHeadOverStaleLink(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	originalPiRoot := piSessionTranscriptRoot
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
		piSessionTranscriptRoot = originalPiRoot
	}()

	_, piRoot := setupTemporaryHomeTranscriptRoots(t)
	piSessionTranscriptRoot = piRoot
	projectCWD := filepath.Join(t.TempDir(), "Project-One")
	worktreeCWD := filepath.Join(projectCWD, ".codex", "netrunner_worktrees", "wave-871", "session-23")
	staleRun := writeProvenPiTranscript(t, piRoot, worktreeCWD, "2026-10-05T10-00-00-000Z_stale-run.jsonl", "stale-run", "2026-10-05T10:00:00.000Z")
	freshRun := writeProvenPiTranscript(t, piRoot, worktreeCWD, "2026-10-05T11-00-00-000Z_fresh-run.jsonl", "fresh-run", "2026-10-05T11:00:00.000Z")

	testDB := setupTranscriptPathTestDB(t, projectCWD, filepath.Join(t.TempDir(), "project-two"))
	defer func() { _ = testDB.Close() }()
	seedParallelWaveWorkerTable(t, testDB, [][4]string{
		{"871", "19", ".codex/netrunner_worktrees/wave-871/session-23", "2026-10-05 09:55:00"},
	})
	if _, err := testDB.Exec(
		`INSERT INTO session_external_link (session_id, backend, external_session_id) VALUES (19, 'pi', 'stale-run')`,
	); err != nil {
		t.Fatalf("seed stale external link: %v", err)
	}
	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1

	_, out, err := GetNetrunnerTranscriptPath(context.Background(), nil, GetNetrunnerTranscriptPathInput{SessionId: 18})
	if err != nil {
		t.Fatalf("stale-link lookup failed: %v", err)
	}
	if out.GlobalSessionId != 19 || !out.Found || out.TranscriptPath != freshRun || out.ExternalSessionId != "fresh-run" {
		t.Fatalf("expected the proven worker head %q to win, got %+v", freshRun, out)
	}
	if !strings.Contains(strings.Join(out.SearchDiagnostics, "\n"), "is stale for this worker") {
		t.Fatalf("expected the stale-link diagnostic, got %+v", out.SearchDiagnostics)
	}

	head, history, _ := resolveSystem1WorkerTranscriptHistory(19, 18, 1, "pi", projectCWD)
	if head != freshRun {
		t.Fatalf("expected proven head %q, got %q", freshRun, head)
	}
	if len(history) != 2 || history[0] != staleRun || history[1] != freshRun {
		t.Fatalf("expected the full attempt history with the head last, got %v", history)
	}

	persisted, err := fetchSessionExternalID(19, "pi")
	if err != nil {
		t.Fatalf("fetch persisted pi external id: %v", err)
	}
	if persisted != "fresh-run" {
		t.Fatalf("expected the stale link to be refreshed to the proven head, got %q", persisted)
	}
}
