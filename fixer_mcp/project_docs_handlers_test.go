package main

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

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

func TestNetrunnerProgressLogsCreateReadValidateAndScope(t *testing.T) {
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

	callResult, started, err := LogNetrunnerProgress(context.Background(), nil, LogNetrunnerProgressInput{
		LogType: "started",
		LogText: "Started documentation split work",
	})
	if err != nil {
		t.Fatalf("log_netrunner_progress started failed: %v", err)
	}
	if callResult != nil || started.Status != "success" || started.SessionId != 1 {
		t.Fatalf("unexpected started log output: callResult=%+v out=%+v", callResult, started)
	}

	_, _, err = LogNetrunnerProgress(context.Background(), nil, LogNetrunnerProgressInput{
		LogType: "progress",
		LogText: "Added migration tests",
	})
	if err != nil {
		t.Fatalf("log_netrunner_progress progress failed: %v", err)
	}

	invalidResult, _, invalidErr := LogNetrunnerProgress(context.Background(), nil, LogNetrunnerProgressInput{
		LogType: "note",
		LogText: "This should fail",
	})
	if invalidErr == nil || invalidResult == nil || !invalidResult.IsError {
		t.Fatal("expected invalid log_type to fail with MCP error result")
	}
	if !strings.Contains(invalidErr.Error(), "invalid log_type") {
		t.Fatalf("expected invalid log_type guidance, got %v", invalidErr)
	}

	authorizedRole = "fixer"
	viewResult, logs, viewErr := ViewNetrunnerLogs(context.Background(), nil, ViewNetrunnerLogsInput{SessionId: 1})
	if viewErr != nil {
		t.Fatalf("view_netrunner_logs failed: %v", viewErr)
	}
	if viewResult != nil {
		t.Fatalf("expected nil call result on success, got %+v", viewResult)
	}
	if len(logs.Logs) != 2 {
		t.Fatalf("expected 2 logs, got %+v", logs.Logs)
	}
	if logs.Logs[0].LogType != "started" || logs.Logs[1].LogType != "progress" {
		t.Fatalf("expected chronological started/progress logs, got %+v", logs.Logs)
	}
	if logs.Logs[0].CreatedAt == "" {
		t.Fatalf("expected backend-created timestamp, got %+v", logs.Logs[0])
	}

	authorizedProjectId = 2
	scopedResult, scopedLogs, scopedErr := ViewNetrunnerLogs(context.Background(), nil, ViewNetrunnerLogsInput{SessionId: 1})
	if scopedErr != nil {
		t.Fatalf("project-2 view should succeed for its own local session without leaking project-1 logs: %v", scopedErr)
	}
	if scopedResult != nil {
		t.Fatalf("expected nil call result on project-2 empty success, got %+v", scopedResult)
	}
	if len(scopedLogs.Logs) != 0 {
		t.Fatalf("expected project-scoped empty logs, got %+v", scopedLogs.Logs)
	}

	authorizedRole = "netrunner"
	deniedResult, _, deniedErr := ViewNetrunnerLogs(context.Background(), nil, ViewNetrunnerLogsInput{SessionId: 1})
	if deniedErr == nil || deniedResult == nil || !deniedResult.IsError {
		t.Fatal("expected netrunner role to be denied view_netrunner_logs")
	}
}

func TestLogNetrunnerProgressRequiresNetrunnerRole(t *testing.T) {
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
	authorizedSessionId = 1

	callResult, _, err := LogNetrunnerProgress(context.Background(), nil, LogNetrunnerProgressInput{
		LogType: "started",
		LogText: "Fixer should not write logs",
	})
	if err == nil || callResult == nil || !callResult.IsError {
		t.Fatal("expected fixer role to be denied log_netrunner_progress")
	}
}

func TestProjectDocTreeFieldsAddReadAndValidate(t *testing.T) {
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

	_, root, err := AddProjectDoc(context.Background(), nil, AddProjectDocInput{
		Title:   "Project Guide",
		Content: "Root canon",
		DocType: "documentation",
	})
	if err != nil {
		t.Fatalf("add root project doc failed: %v", err)
	}
	if root.Id != 3 {
		t.Fatalf("expected root local doc id 3 after fixtures, got %+v", root)
	}

	_, child, err := AddProjectDoc(context.Background(), nil, AddProjectDocInput{
		Title:       "Runtime Setup",
		Content:     "Setup canon",
		DocType:     "documentation",
		ParentDocId: root.Id,
	})
	if err != nil {
		t.Fatalf("add child project doc failed: %v", err)
	}

	_, docs, err := GetProjectDocs(context.Background(), nil, GetProjectDocsInput{})
	if err != nil {
		t.Fatalf("get_project_docs failed: %v", err)
	}
	if len(docs.Docs) != 4 {
		t.Fatalf("expected 4 docs, got %+v", docs.Docs)
	}
	rootDoc := docs.Docs[2]
	childDoc := docs.Docs[3]
	if rootDoc.Level != 0 || rootDoc.ParentDocId != 0 || rootDoc.Slug != "project-guide" || rootDoc.Path != "project-guide" || rootDoc.Status != "current" {
		t.Fatalf("unexpected root tree fields: %+v", rootDoc)
	}
	if childDoc.Id != child.Id || childDoc.ParentDocId != root.Id || childDoc.Level != 1 || childDoc.Path != "project-guide/runtime-setup" {
		t.Fatalf("unexpected child tree fields: %+v", childDoc)
	}

	invalidResult, _, invalidErr := AddProjectDoc(context.Background(), nil, AddProjectDocInput{
		Title:   "Orphan Deep Doc",
		Content: "Invalid",
		Level:   2,
	})
	if invalidErr == nil || invalidResult == nil || !invalidResult.IsError {
		t.Fatal("expected level > 0 without parent to fail")
	}

	mismatchResult, _, mismatchErr := AddProjectDoc(context.Background(), nil, AddProjectDocInput{
		Title:       "Wrong Level",
		Content:     "Invalid",
		ParentDocId: root.Id,
		Level:       3,
	})
	if mismatchErr == nil || mismatchResult == nil || !mismatchResult.IsError {
		t.Fatal("expected child level mismatch to fail")
	}

	cycleResult, _, cycleErr := UpdateProjectDoc(context.Background(), nil, UpdateProjectDocInput{
		DocId:       root.Id,
		Content:     "Root canon",
		ParentDocId: child.Id,
	})
	if cycleErr == nil || cycleResult == nil || !cycleResult.IsError {
		t.Fatal("expected descendant parent cycle to fail")
	}
	if !strings.Contains(cycleErr.Error(), "descendant") {
		t.Fatalf("expected descendant cycle error, got %v", cycleErr)
	}

	_, _, err = UpdateProjectDoc(context.Background(), nil, UpdateProjectDocInput{
		DocId:   child.Id,
		Content: "Updated setup canon",
		Status:  "stale",
	})
	if err != nil {
		t.Fatalf("update_project_doc tree status failed: %v", err)
	}
	_, updatedDocs, err := GetProjectDocs(context.Background(), nil, GetProjectDocsInput{})
	if err != nil {
		t.Fatalf("get_project_docs after update failed: %v", err)
	}
	if updatedDocs.Docs[3].Status != "stale" || updatedDocs.Docs[3].Content != "Updated setup canon" {
		t.Fatalf("expected child status/content update, got %+v", updatedDocs.Docs[3])
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

func TestProposeDocUpdateRecoversProjectScopedSessionBindingWithForeignKeys(t *testing.T) {
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
	authorizedSessionId = 2

	_, proposeOut, proposeErr := ProposeDocUpdate(context.Background(), nil, ProposeDocUpdateInput{
		ProposedContent: "Recovered FK-safe proposal",
		ProposedDocType: "documentation",
	})
	if proposeErr != nil {
		t.Fatalf("propose_doc_update should recover project-scoped binding: %v", proposeErr)
	}
	if proposeOut.ProposalId != 1 {
		t.Fatalf("expected first project-local proposal id, got %+v", proposeOut)
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
		t.Fatalf("expected recovered authorizedSessionId=%d, got %d", globalCheckedOutID, authorizedSessionId)
	}

	var proposalProjectID, proposalSessionID int
	if err := db.QueryRow(
		"SELECT project_id, session_id FROM doc_proposal WHERE proposed_content = 'Recovered FK-safe proposal'",
	).Scan(&proposalProjectID, &proposalSessionID); err != nil {
		t.Fatalf("query proposal row: %v", err)
	}
	if proposalProjectID != 2 || proposalSessionID != globalCheckedOutID {
		t.Fatalf("expected proposal project/session [2,%d], got [%d,%d]", globalCheckedOutID, proposalProjectID, proposalSessionID)
	}

	_, completeOut, completeErr := CompleteTask(context.Background(), nil, CompleteTaskInput{
		SessionId:   2,
		FinalReport: structuredTestFinalReport,
	})
	if completeErr != nil {
		t.Fatalf("complete_task should use the recovered proposal for local session 2: %v", completeErr)
	}
	if completeOut.Status != "success" {
		t.Fatalf("expected complete_task success, got %+v", completeOut)
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

func TestSetDocProposalStatus_ApprovalCanPlaceCreatedDoc(t *testing.T) {
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

	_, proposalOut, err := ProposeDocUpdate(context.Background(), nil, ProposeDocUpdateInput{
		ProposedContent: "MCP catalog contract",
		ProposedDocType: "catalog_contract",
	})
	if err != nil {
		t.Fatalf("propose_doc_update failed: %v", err)
	}

	authorizedRole = "fixer"
	_, _, err = SetDocProposalStatus(context.Background(), nil, SetDocProposalStatusInput{
		ProposalId:  proposalOut.ProposalId,
		Status:      "approved",
		ParentDocId: 1,
		Level:       1,
		Slug:        "mcp-catalog-contract",
	})
	if err != nil {
		t.Fatalf("set_doc_proposal_status with placement failed: %v", err)
	}

	var parentLocalID, level int
	var slug, path, status, content string
	err = db.QueryRow(`
		SELECT
			(
				SELECT COUNT(*)
				FROM project_doc parent_ranked
				WHERE parent_ranked.project_id = d.project_id AND parent_ranked.id <= d.parent_doc_id
			),
			d.level,
			d.slug,
			d.path,
			d.status,
			d.content
		FROM project_doc d
		WHERE d.project_id = 1 AND d.doc_type = 'catalog_contract'
	`).Scan(&parentLocalID, &level, &slug, &path, &status, &content)
	if err != nil {
		t.Fatalf("query placed project_doc: %v", err)
	}
	if parentLocalID != 1 || level != 1 || slug != "mcp-catalog-contract" || path != "mcp-catalog-contract" || status != "current" {
		t.Fatalf("unexpected placed doc tree fields parent=%d level=%d slug=%q path=%q status=%q", parentLocalID, level, slug, path, status)
	}
	if content != "MCP catalog contract" {
		t.Fatalf("expected proposed content, got %q", content)
	}
}
