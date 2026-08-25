package main

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

func setupListProjectSessionsTestDB(t *testing.T) *sql.DB {
	t.Helper()

	testDB := setupParallelWaveTestDB(t, testProjectCWD)

	// setupParallelWaveTestDB seeds:
	//   session 1 (project 1, "Task A", pending)
	//   session 2 (project 2, "Task B", pending)
	//   session 3 (project 1, "Task C", pending)
	_, err := testDB.Exec(`
		UPDATE session SET status = 'completed', cli_backend = 'codex', report = 'Fixed the widget rendering bug' WHERE id = 1;
		UPDATE session SET cli_backend = 'codex' WHERE id = 3;
		INSERT INTO session (project_id, task_description, status, cli_backend, declared_write_scope)
			VALUES (1, 'Investigate widget flakiness', 'in_progress', 'claude', '["docs/c"]');
		INSERT INTO parallel_wave (id, project_id, base_sha, project_cwd, worktree_root)
			VALUES (1, 1, 'deadbeef', 'irrelevant', 'irrelevant');
		INSERT INTO parallel_wave_worker (wave_id, project_id, session_id, status, declared_write_scope, branch_name, worktree_path, base_sha)
			VALUES (1, 1, 1, 'completed', '["docs/a"]', 'fixer/wave-1/session-1', '/tmp/wt-1', 'deadbeef');
	`)
	if err != nil {
		_ = testDB.Close()
		t.Fatalf("seed list_project_sessions db: %v", err)
	}

	return testDB
}

func TestListProjectSessions_FiltersAndPagination(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
	}()

	testDB := setupListProjectSessionsTestDB(t)
	defer func() {
		_ = testDB.Close()
	}()

	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1

	// Default listing: 3 sessions in project 1 (local ids 1, 2, 3), newest first.
	callResult, out, err := ListProjectSessions(context.Background(), nil, ListProjectSessionsInput{})
	if err != nil {
		t.Fatalf("list_project_sessions failed: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil call result on success, got: %+v", callResult)
	}
	if out.ProjectId != 1 {
		t.Fatalf("expected project_id 1, got %d", out.ProjectId)
	}
	if len(out.Sessions) != 3 {
		t.Fatalf("expected 3 sessions for project 1, got %d: %+v", len(out.Sessions), out.Sessions)
	}
	if out.Sessions[0].Id != 3 || out.Sessions[1].Id != 2 || out.Sessions[2].Id != 1 {
		t.Fatalf("expected newest-first local ids [3,2,1], got %+v", out.Sessions)
	}
	if out.Returned != 3 || out.HasMore {
		t.Fatalf("unexpected pagination metadata: %+v", out)
	}

	// The newest session (local id 3, global id 4) is the claude in_progress one.
	newest := out.Sessions[0]
	if newest.CliBackend != "claude" || newest.Status != "in_progress" {
		t.Fatalf("unexpected newest session summary: %+v", newest)
	}

	// Wave-linked session (local id 1, global id 1) should carry wave metadata.
	waveLinked := out.Sessions[2]
	if waveLinked.WaveId != 1 || waveLinked.WaveWorkerStatus != "completed" {
		t.Fatalf("expected wave linkage on oldest session, got %+v", waveLinked)
	}
	if waveLinked.Status != "completed" {
		t.Fatalf("expected completed status on oldest session, got %+v", waveLinked)
	}

	// Status filter.
	_, statusOut, err := ListProjectSessions(context.Background(), nil, ListProjectSessionsInput{Status: "in_progress"})
	if err != nil {
		t.Fatalf("status-filtered list failed: %v", err)
	}
	if len(statusOut.Sessions) != 1 || statusOut.Sessions[0].Id != 3 {
		t.Fatalf("expected single in_progress session (local id 3), got %+v", statusOut.Sessions)
	}

	// Invalid status is rejected.
	invalidResult, _, invalidErr := ListProjectSessions(context.Background(), nil, ListProjectSessionsInput{Status: "bogus"})
	if invalidErr == nil {
		t.Fatal("expected error for invalid status filter")
	}
	if invalidResult == nil || !invalidResult.IsError {
		t.Fatal("expected MCP error result for invalid status filter")
	}

	// Backend filter.
	_, backendOut, err := ListProjectSessions(context.Background(), nil, ListProjectSessionsInput{Backend: "claude"})
	if err != nil {
		t.Fatalf("backend-filtered list failed: %v", err)
	}
	if len(backendOut.Sessions) != 1 || backendOut.Sessions[0].CliBackend != "claude" {
		t.Fatalf("expected single claude session, got %+v", backendOut.Sessions)
	}

	// Search filter matches report text on session local id 1.
	_, searchOut, err := ListProjectSessions(context.Background(), nil, ListProjectSessionsInput{Search: "widget rendering"})
	if err != nil {
		t.Fatalf("search-filtered list failed: %v", err)
	}
	if len(searchOut.Sessions) != 1 || searchOut.Sessions[0].Id != 1 {
		t.Fatalf("expected search to match local session 1 via report text, got %+v", searchOut.Sessions)
	}

	// wave_only filter returns only the wave-linked session.
	_, waveOnlyOut, err := ListProjectSessions(context.Background(), nil, ListProjectSessionsInput{WaveOnly: true})
	if err != nil {
		t.Fatalf("wave_only list failed: %v", err)
	}
	if len(waveOnlyOut.Sessions) != 1 || waveOnlyOut.Sessions[0].WaveId != 1 {
		t.Fatalf("expected only the wave-linked session, got %+v", waveOnlyOut.Sessions)
	}

	// Pagination: limit=1 should report has_more and return the newest row first.
	_, page1, err := ListProjectSessions(context.Background(), nil, ListProjectSessionsInput{Limit: 1})
	if err != nil {
		t.Fatalf("paginated list (page 1) failed: %v", err)
	}
	if len(page1.Sessions) != 1 || page1.Sessions[0].Id != 3 || !page1.HasMore {
		t.Fatalf("unexpected page 1: %+v", page1)
	}

	_, page2, err := ListProjectSessions(context.Background(), nil, ListProjectSessionsInput{Limit: 1, Offset: 2})
	if err != nil {
		t.Fatalf("paginated list (page 2) failed: %v", err)
	}
	if len(page2.Sessions) != 1 || page2.Sessions[0].Id != 1 || page2.HasMore {
		t.Fatalf("unexpected last page: %+v", page2)
	}
}

func TestListProjectSessions_ProjectIsolationAndRoleAccess(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
	}()

	testDB := setupListProjectSessionsTestDB(t)
	defer func() {
		_ = testDB.Close()
	}()

	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 2

	_, out, err := ListProjectSessions(context.Background(), nil, ListProjectSessionsInput{})
	if err != nil {
		t.Fatalf("list_project_sessions for project 2 failed: %v", err)
	}
	if len(out.Sessions) != 1 {
		t.Fatalf("expected exactly the one project-2 session, got %+v", out.Sessions)
	}
	for _, session := range out.Sessions {
		if strings.Contains(session.TaskSummary, "widget") {
			t.Fatalf("project 2 listing leaked project 1 data: %+v", session)
		}
	}

	authorizedRole = "netrunner"
	authorizedProjectId = 1
	deniedResult, _, deniedErr := ListProjectSessions(context.Background(), nil, ListProjectSessionsInput{})
	if deniedErr == nil {
		t.Fatal("expected access denied for netrunner role")
	}
	if deniedResult == nil || !deniedResult.IsError {
		t.Fatal("expected MCP error result for netrunner role")
	}
	if !strings.Contains(deniedErr.Error(), "requires fixer role") {
		t.Fatalf("unexpected denial error: %v", deniedErr)
	}

	authorizedRole = "overseer"
	authorizedProjectId = 0
	overseerDeniedResult, _, overseerDeniedErr := ListProjectSessions(context.Background(), nil, ListProjectSessionsInput{})
	if overseerDeniedErr == nil {
		t.Fatal("expected access denied for overseer role")
	}
	if overseerDeniedResult == nil || !overseerDeniedResult.IsError {
		t.Fatal("expected MCP error result for overseer role")
	}
}
