package main

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
)

func setupForeignKeySessionBindingTestDB(t *testing.T) *sql.DB {
	t.Helper()

	testDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	testDB.SetMaxOpenConns(1)
	testDB.SetMaxIdleConns(1)

	_, err = testDB.Exec(`
		PRAGMA foreign_keys = ON;
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
			FOREIGN KEY(project_id) REFERENCES project(id)
		);
		CREATE TABLE doc_proposal (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER,
			session_id INTEGER,
			status TEXT NOT NULL,
			proposed_content TEXT NOT NULL,
			proposed_doc_type TEXT DEFAULT 'documentation',
			target_project_doc_id INTEGER,
			FOREIGN KEY(project_id) REFERENCES project(id),
			FOREIGN KEY(session_id) REFERENCES session(id)
		);

		INSERT INTO project (id, name, cwd) VALUES
			(1, 'Alpha', '/tmp/fixer-fk-alpha'),
			(2, 'Beta', '/tmp/fixer-fk-beta'),
			(3, 'Deleted gap', '/tmp/fixer-fk-gap');
		INSERT INTO session (project_id, task_description, status) VALUES
			(1, 'Alpha historical task', 'completed'),
			(3, 'Deleted gap task', 'completed');
		DELETE FROM session WHERE project_id = 3;
		INSERT INTO session (project_id, task_description, status) VALUES
			(2, 'Beta previous task', 'completed'),
			(2, 'Beta checked-out task', 'in_progress');
	`)
	if err != nil {
		_ = testDB.Close()
		t.Fatalf("seed foreign-key db: %v", err)
	}

	return testDB
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

func TestCheckoutTaskRebindsInProgressSessionForDocProposal(t *testing.T) {
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

	if _, err := db.Exec("INSERT INTO session (project_id, task_description, status) VALUES (2, 'Task B2', 'pending')"); err != nil {
		t.Fatalf("seed extra project-2 session: %v", err)
	}
	if _, err := db.Exec("INSERT INTO session (project_id, task_description, status) VALUES (1, 'Task A2', 'in_progress')"); err != nil {
		t.Fatalf("seed resumed in-progress session: %v", err)
	}

	var globalA2 int
	if err := db.QueryRow("SELECT id FROM session WHERE project_id = 1 AND task_description = 'Task A2'").Scan(&globalA2); err != nil {
		t.Fatalf("resolve global id for Task A2: %v", err)
	}
	if globalA2 == 2 {
		t.Fatalf("expected interleaved global id for Task A2, got %d", globalA2)
	}

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

	if _, _, checkoutErr := CheckoutTask(context.Background(), nil, CheckoutTaskInput{SessionId: 2}); checkoutErr != nil {
		t.Fatalf("checkout_task should rebind existing in-progress session: %v", checkoutErr)
	}
	if authorizedSessionId != globalA2 {
		t.Fatalf("expected authorizedSessionId to store global id %d, got %d", globalA2, authorizedSessionId)
	}

	if _, _, proposeErr := ProposeDocUpdate(context.Background(), nil, ProposeDocUpdateInput{
		ProposedContent: "Resume-safe proposal",
		ProposedDocType: "documentation",
	}); proposeErr != nil {
		t.Fatalf("propose_doc_update failed after in-progress rebind: %v", proposeErr)
	}

	var proposalSessionID, proposalProjectID int
	if err := db.QueryRow(
		"SELECT session_id, project_id FROM doc_proposal WHERE proposed_content = 'Resume-safe proposal'",
	).Scan(&proposalSessionID, &proposalProjectID); err != nil {
		t.Fatalf("query inserted doc proposal: %v", err)
	}
	if proposalProjectID != 1 {
		t.Fatalf("expected proposal to stay in project 1, got %d", proposalProjectID)
	}
	if proposalSessionID != globalA2 {
		t.Fatalf("expected proposal to reference global session id %d, got %d", globalA2, proposalSessionID)
	}
}

func TestCheckoutProposeCompleteUsesGlobalForeignKeysForProjectScopedSession(t *testing.T) {
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

	testDB := setupForeignKeySessionBindingTestDB(t)
	defer func() {
		_ = testDB.Close()
	}()

	db = testDB
	authorizedRole = "netrunner"
	authorizedProjectId = 2
	authorizedSessionId = 0

	if _, _, checkoutErr := CheckoutTask(context.Background(), nil, CheckoutTaskInput{SessionId: 2}); checkoutErr != nil {
		t.Fatalf("checkout_task failed for project-scoped session 2: %v", checkoutErr)
	}

	var globalCheckedOutID int
	if err := db.QueryRow(
		`SELECT id
		 FROM session
		 WHERE project_id = 2
		 ORDER BY id
		 LIMIT 1 OFFSET 1`,
	).Scan(&globalCheckedOutID); err != nil {
		t.Fatalf("resolve global checked-out session id: %v", err)
	}
	if authorizedSessionId != globalCheckedOutID {
		t.Fatalf("expected checkout to store global id %d, got %d", globalCheckedOutID, authorizedSessionId)
	}

	if _, _, proposeErr := ProposeDocUpdate(context.Background(), nil, ProposeDocUpdateInput{
		ProposedContent: "Checked-out FK-safe proposal",
		ProposedDocType: "documentation",
	}); proposeErr != nil {
		t.Fatalf("propose_doc_update failed after project-scoped checkout: %v", proposeErr)
	}

	var proposalSessionID int
	if err := db.QueryRow(
		"SELECT session_id FROM doc_proposal WHERE proposed_content = 'Checked-out FK-safe proposal'",
	).Scan(&proposalSessionID); err != nil {
		t.Fatalf("query proposal row: %v", err)
	}
	if proposalSessionID != globalCheckedOutID {
		t.Fatalf("expected proposal to reference global session id %d, got %d", globalCheckedOutID, proposalSessionID)
	}

	if _, _, completeErr := CompleteTask(context.Background(), nil, CompleteTaskInput{
		SessionId:   2,
		FinalReport: structuredTestFinalReport,
	}); completeErr != nil {
		t.Fatalf("complete_task failed after project-scoped checkout/proposal: %v", completeErr)
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

func TestCompleteTask_IgnoresStaleOrchestrationEpoch(t *testing.T) {
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
	if err != nil {
		t.Fatalf("complete_task should ignore stale epoch metadata now: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil call result on success, got %+v", callResult)
	}
	var status string
	if queryErr := testDB.QueryRow("SELECT status FROM session WHERE id = 1").Scan(&status); queryErr != nil {
		t.Fatalf("read session status: %v", queryErr)
	}
	if status != "review" {
		t.Fatalf("expected session to advance to review, got %q", status)
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
