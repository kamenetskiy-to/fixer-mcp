package main

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

func setupFeedbackTestDB(t *testing.T) *sql.DB {
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
		CREATE TABLE fixer_mcp_feedback (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL,
			feedback_type TEXT NOT NULL,
			content TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		INSERT INTO project (id, name, cwd) VALUES (1, 'Project One', '/tmp/project-one');
		INSERT INTO project (id, name, cwd) VALUES (2, 'Project Two', '/tmp/project-two');
		INSERT INTO fixer_mcp_feedback (project_id, feedback_type, content, created_at) VALUES
			(1, 'bug', 'gate wait access denied', '2026-08-10 09:00:00'),
			(1, 'feature', 'want feedback list tool', '2026-08-11 10:00:00'),
			(1, 'bug', 'assume_role after restart', '2026-08-12 11:00:00'),
			(2, 'bug', 'other project report', '2026-08-13 12:00:00');
	`)
	if err != nil {
		_ = testDB.Close()
		t.Fatalf("seed feedback db: %v", err)
	}

	return testDB
}

func TestListFixerMcpFeedback_FiltersAndPagination(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
	}()

	testDB := setupFeedbackTestDB(t)
	defer func() {
		_ = testDB.Close()
	}()

	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1

	// Default listing: the three project-1 rows, newest first.
	callResult, out, err := ListFixerMcpFeedback(context.Background(), nil, ListFixerMcpFeedbackInput{})
	if err != nil {
		t.Fatalf("list_fixer_mcp_feedback failed: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil call result on success, got: %+v", callResult)
	}
	if out.ProjectId != 1 {
		t.Fatalf("expected bound project id 1, got %d", out.ProjectId)
	}
	if len(out.Feedback) != 3 {
		t.Fatalf("expected 3 feedback rows for project 1, got %d: %+v", len(out.Feedback), out.Feedback)
	}
	if out.Feedback[0].Content != "assume_role after restart" {
		t.Fatalf("expected newest row first, got %+v", out.Feedback[0])
	}
	if out.Feedback[2].Content != "gate wait access denied" {
		t.Fatalf("expected oldest row last, got %+v", out.Feedback[2])
	}
	if out.Returned != 3 || out.HasMore {
		t.Fatalf("unexpected pagination metadata: %+v", out)
	}

	// Exact type filter.
	_, typeOut, err := ListFixerMcpFeedback(context.Background(), nil, ListFixerMcpFeedbackInput{FeedbackType: "bug"})
	if err != nil {
		t.Fatalf("type-filtered list failed: %v", err)
	}
	if len(typeOut.Feedback) != 2 {
		t.Fatalf("expected 2 bug rows, got %+v", typeOut.Feedback)
	}
	for _, item := range typeOut.Feedback {
		if item.FeedbackType != "bug" {
			t.Fatalf("type filter leaked non-bug row: %+v", item)
		}
	}

	// Search filter matches content substring.
	_, searchOut, err := ListFixerMcpFeedback(context.Background(), nil, ListFixerMcpFeedbackInput{Search: "restart"})
	if err != nil {
		t.Fatalf("search-filtered list failed: %v", err)
	}
	if len(searchOut.Feedback) != 1 || searchOut.Feedback[0].Content != "assume_role after restart" {
		t.Fatalf("expected search to match one row, got %+v", searchOut.Feedback)
	}

	// Inclusive date-range filter.
	_, fromOut, err := ListFixerMcpFeedback(context.Background(), nil, ListFixerMcpFeedbackInput{FromDate: "2026-08-11"})
	if err != nil {
		t.Fatalf("from_date list failed: %v", err)
	}
	if len(fromOut.Feedback) != 2 {
		t.Fatalf("expected 2 rows on/after 2026-08-11, got %+v", fromOut.Feedback)
	}

	_, toOut, err := ListFixerMcpFeedback(context.Background(), nil, ListFixerMcpFeedbackInput{ToDate: "2026-08-11"})
	if err != nil {
		t.Fatalf("to_date list failed: %v", err)
	}
	if len(toOut.Feedback) != 2 {
		t.Fatalf("expected 2 rows on/before 2026-08-11, got %+v", toOut.Feedback)
	}

	_, rangeOut, err := ListFixerMcpFeedback(context.Background(), nil, ListFixerMcpFeedbackInput{FromDate: "2026-08-11", ToDate: "2026-08-11"})
	if err != nil {
		t.Fatalf("date-range list failed: %v", err)
	}
	if len(rangeOut.Feedback) != 1 || rangeOut.Feedback[0].Content != "want feedback list tool" {
		t.Fatalf("expected exactly the 2026-08-11 row, got %+v", rangeOut.Feedback)
	}

	// Invalid dates are rejected.
	invalidResult, _, invalidErr := ListFixerMcpFeedback(context.Background(), nil, ListFixerMcpFeedbackInput{FromDate: "13/08/2026"})
	if invalidErr == nil {
		t.Fatal("expected error for invalid from_date")
	}
	if invalidResult == nil || !invalidResult.IsError {
		t.Fatal("expected MCP error result for invalid from_date")
	}
	if !strings.Contains(invalidErr.Error(), "from_date must be YYYY-MM-DD") {
		t.Fatalf("unexpected invalid from_date error: %v", invalidErr)
	}

	// Pagination: limit=1 reports has_more and returns newest first.
	_, page1, err := ListFixerMcpFeedback(context.Background(), nil, ListFixerMcpFeedbackInput{Limit: 1})
	if err != nil {
		t.Fatalf("paginated list (page 1) failed: %v", err)
	}
	if len(page1.Feedback) != 1 || page1.Feedback[0].Content != "assume_role after restart" || !page1.HasMore {
		t.Fatalf("unexpected page 1: %+v", page1)
	}

	_, page2, err := ListFixerMcpFeedback(context.Background(), nil, ListFixerMcpFeedbackInput{Limit: 1, Offset: 2})
	if err != nil {
		t.Fatalf("paginated list (page 2) failed: %v", err)
	}
	if len(page2.Feedback) != 1 || page2.Feedback[0].Content != "gate wait access denied" || page2.HasMore {
		t.Fatalf("unexpected last page: %+v", page2)
	}
}

func TestListFixerMcpFeedback_FixerMCPProjectReadsCrossProject(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
	}()

	testDB := setupFeedbackTestDB(t)
	defer func() { _ = testDB.Close() }()
	if _, err := testDB.Exec("UPDATE project SET name = 'Fixer MCP' WHERE id = 1"); err != nil {
		t.Fatalf("name Fixer MCP project: %v", err)
	}
	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1

	_, out, err := ListFixerMcpFeedback(context.Background(), nil, ListFixerMcpFeedbackInput{})
	if err != nil {
		t.Fatalf("global feedback list failed: %v", err)
	}
	if out.ProjectId != 0 || len(out.Feedback) != 4 {
		t.Fatalf("expected all four cross-project rows, got %+v", out)
	}

	projectTwo := 2
	_, filtered, err := ListFixerMcpFeedback(context.Background(), nil, ListFixerMcpFeedbackInput{ProjectId: &projectTwo})
	if err != nil {
		t.Fatalf("project-filtered feedback list failed: %v", err)
	}
	if filtered.ProjectId != 2 || len(filtered.Feedback) != 1 || filtered.Feedback[0].ProjectId != 2 {
		t.Fatalf("expected only project 2 feedback, got %+v", filtered)
	}
}

func TestListFixerMcpFeedback_RoleAccessAndCrossProjectSearch(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
	}()

	testDB := setupFeedbackTestDB(t)
	defer func() {
		_ = testDB.Close()
	}()

	db = testDB

	// Netrunner is denied.
	authorizedRole = "netrunner"
	authorizedProjectId = 1
	deniedResult, _, deniedErr := ListFixerMcpFeedback(context.Background(), nil, ListFixerMcpFeedbackInput{})
	if deniedErr == nil {
		t.Fatal("expected access denied for netrunner role")
	}
	if deniedResult == nil || !deniedResult.IsError {
		t.Fatal("expected MCP error result for netrunner role")
	}
	if !strings.Contains(deniedErr.Error(), "requires fixer or overseer role") {
		t.Fatalf("unexpected denial error: %v", deniedErr)
	}

	// Unbound fixer is denied.
	authorizedRole = "fixer"
	authorizedProjectId = 0
	unboundResult, _, unboundErr := ListFixerMcpFeedback(context.Background(), nil, ListFixerMcpFeedbackInput{})
	if unboundErr == nil {
		t.Fatal("expected access denied for unbound fixer")
	}
	if unboundResult == nil || !unboundResult.IsError {
		t.Fatal("expected MCP error result for unbound fixer")
	}

	// Overseer with no project filter sees all projects.
	authorizedRole = "overseer"
	authorizedProjectId = 0
	_, allOut, err := ListFixerMcpFeedback(context.Background(), nil, ListFixerMcpFeedbackInput{})
	if err != nil {
		t.Fatalf("overseer cross-project list failed: %v", err)
	}
	if len(allOut.Feedback) != 4 {
		t.Fatalf("expected 4 cross-project rows, got %+v", allOut.Feedback)
	}

	// Overseer project filter returns only that project.
	projectTwoID := 2
	_, projectTwoOut, err := ListFixerMcpFeedback(context.Background(), nil, ListFixerMcpFeedbackInput{ProjectId: &projectTwoID})
	if err != nil {
		t.Fatalf("overseer project-filtered list failed: %v", err)
	}
	if len(projectTwoOut.Feedback) != 1 || projectTwoOut.Feedback[0].ProjectId != 2 {
		t.Fatalf("expected only project-2 feedback, got %+v", projectTwoOut.Feedback)
	}

	// Overseer rejects a non-positive project filter.
	negativeProjectID := -1
	invalidProjectResult, _, invalidProjectErr := ListFixerMcpFeedback(context.Background(), nil, ListFixerMcpFeedbackInput{ProjectId: &negativeProjectID})
	if invalidProjectErr == nil {
		t.Fatal("expected error for non-positive project_id")
	}
	if invalidProjectResult == nil || !invalidProjectResult.IsError {
		t.Fatal("expected MCP error result for non-positive project_id")
	}
}

func TestSubmitFixerMcpFeedback_RoleBindingAndValidation(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
	}()

	testDB := setupFeedbackTestDB(t)
	defer func() {
		_ = testDB.Close()
	}()

	db = testDB

	// Netrunner is denied.
	authorizedRole = "netrunner"
	authorizedProjectId = 1
	deniedResult, _, deniedErr := SubmitFixerMcpFeedback(context.Background(), nil, SubmitFixerMcpFeedbackInput{
		FeedbackType: "bug",
		Content:      "nope",
	})
	if deniedErr == nil {
		t.Fatal("expected access denied for netrunner role")
	}
	if deniedResult == nil || !deniedResult.IsError {
		t.Fatal("expected MCP error result for netrunner role")
	}

	// Bound fixer submits into its own project.
	authorizedRole = "fixer"
	authorizedProjectId = 1
	callResult, fixerOut, err := SubmitFixerMcpFeedback(context.Background(), nil, SubmitFixerMcpFeedbackInput{
		FeedbackType: "bug",
		Content:      "fixer bound report",
	})
	if err != nil {
		t.Fatalf("fixer submit failed: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil call result on success, got: %+v", callResult)
	}
	if fixerOut.Status != "success" || fixerOut.FeedbackID <= 0 {
		t.Fatalf("unexpected fixer submit output: %+v", fixerOut)
	}
	var storedProjectID int
	if err := testDB.QueryRow("SELECT project_id FROM fixer_mcp_feedback WHERE id = ?", fixerOut.FeedbackID).Scan(&storedProjectID); err != nil {
		t.Fatalf("read submitted feedback: %v", err)
	}
	if storedProjectID != 1 {
		t.Fatalf("expected fixer feedback stored under project 1, got %d", storedProjectID)
	}

	// Overseer without project_id is rejected.
	authorizedRole = "overseer"
	authorizedProjectId = 0
	overseerNoProjectResult, _, overseerNoProjectErr := SubmitFixerMcpFeedback(context.Background(), nil, SubmitFixerMcpFeedbackInput{
		FeedbackType: "bug",
		Content:      "overseer no project",
	})
	if overseerNoProjectErr == nil {
		t.Fatal("expected overseer submit without project_id to fail")
	}
	if overseerNoProjectResult == nil || !overseerNoProjectResult.IsError {
		t.Fatal("expected MCP error result for overseer submit without project_id")
	}

	// Overseer with project_id submits cross-project.
	projectTwoID := 2
	_, overseerOut, err := SubmitFixerMcpFeedback(context.Background(), nil, SubmitFixerMcpFeedbackInput{
		ProjectID:    &projectTwoID,
		FeedbackType: "bug",
		Content:      "overseer cross-project report",
	})
	if err != nil {
		t.Fatalf("overseer submit failed: %v", err)
	}
	if overseerOut.Status != "success" || overseerOut.FeedbackID <= 0 {
		t.Fatalf("unexpected overseer submit output: %+v", overseerOut)
	}
	if err := testDB.QueryRow("SELECT project_id FROM fixer_mcp_feedback WHERE id = ?", overseerOut.FeedbackID).Scan(&storedProjectID); err != nil {
		t.Fatalf("read overseer submitted feedback: %v", err)
	}
	if storedProjectID != 2 {
		t.Fatalf("expected overseer feedback stored under project 2, got %d", storedProjectID)
	}

	// Missing type and content are rejected for a bound fixer.
	authorizedRole = "fixer"
	authorizedProjectId = 1
	missingTypeResult, _, missingTypeErr := SubmitFixerMcpFeedback(context.Background(), nil, SubmitFixerMcpFeedbackInput{
		FeedbackType: "",
		Content:      "content",
	})
	if missingTypeErr == nil || missingTypeResult == nil || !missingTypeResult.IsError {
		t.Fatal("expected missing feedback_type to be rejected")
	}
	missingContentResult, _, missingContentErr := SubmitFixerMcpFeedback(context.Background(), nil, SubmitFixerMcpFeedbackInput{
		FeedbackType: "bug",
		Content:      "",
	})
	if missingContentErr == nil || missingContentResult == nil || !missingContentResult.IsError {
		t.Fatal("expected missing content to be rejected")
	}
}
