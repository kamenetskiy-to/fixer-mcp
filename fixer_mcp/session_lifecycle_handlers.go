package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type GetPendingTasksInput struct{}

type PendingTask struct {
	SessionId       int    `json:"session_id"`
	TaskDescription string `json:"task_description"`
}

type GetPendingTasksOutput struct {
	Tasks []PendingTask `json:"tasks"`
}

func GetPendingTasks(ctx context.Context, req *mcp.CallToolRequest, input GetPendingTasksInput) (*mcp.CallToolResult, GetPendingTasksOutput, error) {
	log.Println("get_pending_tasks called")

	if authorizedRole != "netrunner" {
		return &mcp.CallToolResult{IsError: true}, GetPendingTasksOutput{}, fmt.Errorf("access denied: requires netrunner role")
	}

	rows, err := db.Query(`
		SELECT
			(
				SELECT COUNT(*)
				FROM session s2
				WHERE s2.project_id = s.project_id AND s2.id <= s.id
			) AS local_session_id,
			s.task_description
		FROM session s
		WHERE s.status = 'pending' AND s.project_id = ?
		ORDER BY s.id`, authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetPendingTasksOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	defer rows.Close()

	var tasks []PendingTask
	for rows.Next() {
		var t PendingTask
		if err := rows.Scan(&t.SessionId, &t.TaskDescription); err != nil {
			return &mcp.CallToolResult{IsError: true}, GetPendingTasksOutput{}, fmt.Errorf("DB scan error: %v", err)
		}
		tasks = append(tasks, t)
	}

	if tasks == nil {
		tasks = []PendingTask{}
	}

	return nil, GetPendingTasksOutput{Tasks: tasks}, nil
}

type CheckoutTaskInput struct {
	SessionId int `json:"session_id" jsonschema:"The ID of the session/task to checkout"`
}

type CheckoutTaskOutput struct {
	Status string `json:"status"`
}

func CheckoutTask(ctx context.Context, req *mcp.CallToolRequest, input CheckoutTaskInput) (*mcp.CallToolResult, CheckoutTaskOutput, error) {
	log.Printf("checkout_task called for session %d", input.SessionId)

	if authorizedRole != "netrunner" {
		return &mcp.CallToolResult{IsError: true}, CheckoutTaskOutput{}, fmt.Errorf("access denied: requires netrunner role")
	}

	globalSessionID, err := globalSessionIDFromProjectScoped(input.SessionId, authorizedProjectId)
	if err == sql.ErrNoRows {
		return &mcp.CallToolResult{IsError: true}, CheckoutTaskOutput{}, fmt.Errorf("task not found or not pending")
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CheckoutTaskOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	res, err := db.Exec("UPDATE session SET status = 'in_progress' WHERE id = ? AND project_id = ? AND status = 'pending'", globalSessionID, authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CheckoutTaskOutput{}, fmt.Errorf("DB update error: %v", err)
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CheckoutTaskOutput{}, fmt.Errorf("RowsAffected error: %v", err)
	}

	if rowsAffected == 0 {
		var currentStatus string
		statusErr := db.QueryRow("SELECT status FROM session WHERE id = ? AND project_id = ?", globalSessionID, authorizedProjectId).Scan(&currentStatus)
		if statusErr == sql.ErrNoRows {
			return &mcp.CallToolResult{IsError: true}, CheckoutTaskOutput{}, fmt.Errorf("task not found or not pending")
		}
		if statusErr != nil {
			return &mcp.CallToolResult{IsError: true}, CheckoutTaskOutput{}, fmt.Errorf("DB query error: %v", statusErr)
		}
		if currentStatus != "in_progress" {
			return &mcp.CallToolResult{IsError: true}, CheckoutTaskOutput{}, fmt.Errorf("task not found or not pending")
		}
	}

	authorizedSessionId = globalSessionID

	return nil, CheckoutTaskOutput{Status: "success"}, nil
}

type CreateTaskInput struct {
	TaskDescription    string   `json:"task_description" jsonschema:"Description of the task to be created"`
	DeclaredWriteScope []string `json:"declared_write_scope,omitempty" jsonschema:"Optional declared project-relative write scope for the session. Defaults to the whole project to preserve serial execution."`
}

type CreateTaskOutput struct {
	SessionId int    `json:"session_id"`
	Status    string `json:"status"`
}

func CreateTask(ctx context.Context, req *mcp.CallToolRequest, input CreateTaskInput) (*mcp.CallToolResult, CreateTaskOutput, error) {
	if authorizedRole != "fixer" {
		return &mcp.CallToolResult{IsError: true}, CreateTaskOutput{}, fmt.Errorf("access denied: requires Fixer role")
	}

	declaredWriteScope, err := encodeDeclaredWriteScope(input.DeclaredWriteScope)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CreateTaskOutput{}, err
	}
	res, err := db.Exec(
		"INSERT INTO session (project_id, task_description, status, declared_write_scope) VALUES (?, ?, 'pending', ?)",
		authorizedProjectId,
		input.TaskDescription,
		declaredWriteScope,
	)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CreateTaskOutput{}, fmt.Errorf("DB insert error: %v", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CreateTaskOutput{}, fmt.Errorf("LastInsertId error: %v", err)
	}

	localSessionID, err := projectScopedSessionIDFromGlobal(int(id), authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CreateTaskOutput{}, fmt.Errorf("DB mapping error: %v", err)
	}

	return nil, CreateTaskOutput{SessionId: localSessionID, Status: "success"}, nil
}

type CompleteTaskInput struct {
	SessionId   int    `json:"session_id" jsonschema:"The ID of the session to complete"`
	FinalReport string `json:"final_report" jsonschema:"The final report for the task"`
}

type CompleteTaskOutput struct {
	Status string `json:"status"`
}

type SessionCleanupClaims struct {
	RemovedPaths         []string `json:"removed_paths,omitempty"`
	ExpectedPresentPaths []string `json:"expected_present_paths,omitempty"`
}

type SessionFinalReport struct {
	FilesChanged  []string             `json:"files_changed"`
	CommandsRun   []string             `json:"commands_run"`
	ChecksRun     []string             `json:"checks_run"`
	Blockers      []string             `json:"blockers"`
	ResidualRisks []string             `json:"residual_risks,omitempty"`
	CleanupClaims SessionCleanupClaims `json:"cleanup_claims,omitempty"`
}

const completeTaskReportTemplate = `{"files_changed":["path/to/file"],"commands_run":["cmd"],"checks_run":["check result"],"blockers":[]}`

func normalizeStringList(raw []string) []string {
	values := make([]string, 0, len(raw))
	for _, entry := range raw {
		normalized := strings.TrimSpace(entry)
		if normalized == "" {
			continue
		}
		values = append(values, normalized)
	}
	return values
}

func decodeStructuredFinalReport(raw string) (SessionFinalReport, string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return SessionFinalReport{}, "", fmt.Errorf("final_report is required and must be a non-empty JSON object")
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return SessionFinalReport{}, "", fmt.Errorf("final_report must be valid JSON matching the structured session report schema: %v", err)
	}

	requiredFields := []string{"files_changed", "commands_run", "checks_run", "blockers"}
	for _, field := range requiredFields {
		if _, exists := payload[field]; !exists {
			return SessionFinalReport{}, "", fmt.Errorf(
				"final_report is missing required field %q; required top-level keys: files_changed, commands_run, checks_run, blockers. Minimal template: %s",
				field,
				completeTaskReportTemplate,
			)
		}
	}

	var report SessionFinalReport
	if err := json.Unmarshal([]byte(trimmed), &report); err != nil {
		return SessionFinalReport{}, "", fmt.Errorf("final_report schema decode failed: %v", err)
	}

	report.FilesChanged = normalizeStringList(report.FilesChanged)
	report.CommandsRun = normalizeStringList(report.CommandsRun)
	report.ChecksRun = normalizeStringList(report.ChecksRun)
	report.Blockers = normalizeStringList(report.Blockers)
	report.ResidualRisks = normalizeStringList(report.ResidualRisks)
	report.CleanupClaims.RemovedPaths = normalizeStringList(report.CleanupClaims.RemovedPaths)
	report.CleanupClaims.ExpectedPresentPaths = normalizeStringList(report.CleanupClaims.ExpectedPresentPaths)

	if len(report.FilesChanged) == 0 {
		return SessionFinalReport{}, "", fmt.Errorf("final_report.files_changed must list at least one changed path")
	}
	if len(report.CommandsRun) == 0 {
		return SessionFinalReport{}, "", fmt.Errorf("final_report.commands_run must list at least one command")
	}
	if len(report.ChecksRun) == 0 {
		return SessionFinalReport{}, "", fmt.Errorf("final_report.checks_run must list at least one verification step")
	}

	normalizedPayload, err := json.Marshal(report)
	if err != nil {
		return SessionFinalReport{}, "", fmt.Errorf("failed to normalize final_report: %v", err)
	}
	return report, string(normalizedPayload), nil
}

func CompleteTask(ctx context.Context, req *mcp.CallToolRequest, input CompleteTaskInput) (*mcp.CallToolResult, CompleteTaskOutput, error) {
	if authorizedRole != "netrunner" {
		return &mcp.CallToolResult{IsError: true}, CompleteTaskOutput{}, fmt.Errorf("access denied: requires netrunner role")
	}

	globalSessionID, err := globalSessionIDFromProjectScoped(input.SessionId, authorizedProjectId)
	if err == sql.ErrNoRows {
		return &mcp.CallToolResult{IsError: true}, CompleteTaskOutput{}, fmt.Errorf("session not found in current project")
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CompleteTaskOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	belongs, err := sessionBelongsToProject(globalSessionID, authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CompleteTaskOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	if !belongs {
		return &mcp.CallToolResult{IsError: true}, CompleteTaskOutput{}, fmt.Errorf("session not found in current project")
	}

	proposalCount, err := countSessionDocProposals(globalSessionID, authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CompleteTaskOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	if proposalCount == 0 {
		return &mcp.CallToolResult{IsError: true}, CompleteTaskOutput{}, fmt.Errorf(
			"missing mandatory documentation-impact proposal for session %d: submit at least one propose_doc_update before complete_task; session remains open for correction",
			input.SessionId,
		)
	}

	_, normalizedReport, err := decodeStructuredFinalReport(input.FinalReport)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CompleteTaskOutput{}, err
	}

	_, err = db.Exec("UPDATE session SET status = 'review', report = ? WHERE id = ? AND project_id = ?", normalizedReport, globalSessionID, authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CompleteTaskOutput{}, fmt.Errorf("DB update error: %v", err)
	}

	return nil, CompleteTaskOutput{Status: "success"}, nil
}

type UpdateTaskInput struct {
	SessionId           int    `json:"session_id" jsonschema:"The ID of the session to update"`
	AppendedDescription string `json:"appended_description" jsonschema:"Instructions to append to the task"`
}

type UpdateTaskOutput struct {
	Status string `json:"status"`
}

func UpdateTask(ctx context.Context, req *mcp.CallToolRequest, input UpdateTaskInput) (*mcp.CallToolResult, UpdateTaskOutput, error) {
	if authorizedRole != "fixer" {
		return &mcp.CallToolResult{IsError: true}, UpdateTaskOutput{}, fmt.Errorf("access denied: requires fixer role")
	}

	globalSessionID, err := globalSessionIDFromProjectScoped(input.SessionId, authorizedProjectId)
	if err == sql.ErrNoRows {
		return &mcp.CallToolResult{IsError: true}, UpdateTaskOutput{}, fmt.Errorf("session not found in current project")
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, UpdateTaskOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	_, err = db.Exec("UPDATE session SET task_description = task_description || '\n\n' || ? WHERE id = ? AND project_id = ?", input.AppendedDescription, globalSessionID, authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, UpdateTaskOutput{}, fmt.Errorf("DB update error: %v", err)
	}

	return nil, UpdateTaskOutput{Status: "success"}, nil
}

type GetAllSessionsInput struct{}

type SessionRecord struct {
	Id              int    `json:"id"`
	ProjectId       int    `json:"project_id"`
	TaskDescription string `json:"task_description"`
	Status          string `json:"status"`
}

type GetAllSessionsOutput struct {
	Sessions []SessionRecord `json:"sessions"`
}

func GetAllSessions(ctx context.Context, req *mcp.CallToolRequest, input GetAllSessionsInput) (*mcp.CallToolResult, GetAllSessionsOutput, error) {
	if authorizedRole != "overseer" {
		return &mcp.CallToolResult{IsError: true}, GetAllSessionsOutput{}, fmt.Errorf("access denied: requires overseer role")
	}

	rows, err := db.Query("SELECT id, project_id, task_description, status FROM session")
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetAllSessionsOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	defer rows.Close()

	var sessions []SessionRecord
	for rows.Next() {
		var s SessionRecord
		if err := rows.Scan(&s.Id, &s.ProjectId, &s.TaskDescription, &s.Status); err != nil {
			return &mcp.CallToolResult{IsError: true}, GetAllSessionsOutput{}, fmt.Errorf("DB scan error: %v", err)
		}
		sessions = append(sessions, s)
	}
	if sessions == nil {
		sessions = []SessionRecord{}
	}

	return nil, GetAllSessionsOutput{Sessions: sessions}, nil
}

type SetSessionStatusInput struct {
	SessionId int    `json:"session_id" jsonschema:"The ID of the session to update"`
	Status    string `json:"status" jsonschema:"New status: pending | in_progress | review | completed"`
	Reason    string `json:"reason,omitempty" jsonschema:"Optional reason for the transition"`
	Note      string `json:"note,omitempty" jsonschema:"Optional note alias for reason"`
}

type SetSessionStatusOutput struct {
	Status         string `json:"status"`
	SessionId      int    `json:"session_id"`
	PreviousStatus string `json:"previous_status"`
	NewStatus      string `json:"new_status"`
}

func SetSessionStatus(ctx context.Context, req *mcp.CallToolRequest, input SetSessionStatusInput) (*mcp.CallToolResult, SetSessionStatusOutput, error) {
	if authorizedRole != "fixer" && authorizedRole != "overseer" {
		return &mcp.CallToolResult{IsError: true}, SetSessionStatusOutput{}, fmt.Errorf("access denied: requires fixer or overseer role")
	}

	targetStatus := strings.ToLower(strings.TrimSpace(input.Status))
	if !isValidSessionStatus(targetStatus) {
		return &mcp.CallToolResult{IsError: true}, SetSessionStatusOutput{}, fmt.Errorf("invalid status: must be one of pending, in_progress, review, completed")
	}

	targetSessionID := input.SessionId
	if authorizedRole != "overseer" {
		globalSessionID, err := globalSessionIDFromProjectScoped(input.SessionId, authorizedProjectId)
		if err == sql.ErrNoRows {
			return &mcp.CallToolResult{IsError: true}, SetSessionStatusOutput{}, fmt.Errorf("session not found in current project")
		}
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, SetSessionStatusOutput{}, fmt.Errorf("DB query error: %v", err)
		}
		targetSessionID = globalSessionID
	}

	var projectId int
	var currentStatus string
	err := db.QueryRow("SELECT project_id, status FROM session WHERE id = ?", targetSessionID).Scan(&projectId, &currentStatus)
	if err == sql.ErrNoRows {
		return &mcp.CallToolResult{IsError: true}, SetSessionStatusOutput{}, fmt.Errorf("session not found")
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetSessionStatusOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	if authorizedRole == "fixer" && projectId != authorizedProjectId {
		return &mcp.CallToolResult{IsError: true}, SetSessionStatusOutput{}, fmt.Errorf("access denied: session not found in current project")
	}

	control, _, err := fetchOrchestrationControl(projectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetSessionStatusOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	if control.OrchestrationFrozen && targetStatus != currentStatus {
		return &mcp.CallToolResult{IsError: true}, SetSessionStatusOutput{}, fmt.Errorf("orchestration is frozen for project %d; explicit resume is required before changing session status", projectId)
	}

	if !isAllowedSessionTransition(currentStatus, targetStatus) {
		return &mcp.CallToolResult{IsError: true}, SetSessionStatusOutput{}, fmt.Errorf("invalid status transition: %s -> %s", currentStatus, targetStatus)
	}

	_, err = db.Exec("UPDATE session SET status = ? WHERE id = ?", targetStatus, targetSessionID)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetSessionStatusOutput{}, fmt.Errorf("DB update error: %v", err)
	}
	if targetStatus == "pending" && (currentStatus == "review" || currentStatus == "completed") && currentStatus != targetStatus {
		if _, err := db.Exec("UPDATE session SET rework_count = COALESCE(rework_count, 0) + 1 WHERE id = ?", targetSessionID); err != nil {
			return &mcp.CallToolResult{IsError: true}, SetSessionStatusOutput{}, fmt.Errorf("DB update error: %v", err)
		}
	}

	reason := strings.TrimSpace(input.Reason)
	if reason == "" {
		reason = strings.TrimSpace(input.Note)
	}
	visibleSessionID := targetSessionID
	if authorizedRole != "overseer" {
		visibleSessionID = input.SessionId
	}
	log.Printf("set_session_status role=%s session_id=%d project_id=%d from=%s to=%s reason=%q", authorizedRole, visibleSessionID, projectId, currentStatus, targetStatus, reason)

	return nil, SetSessionStatusOutput{
		Status:         "success",
		SessionId:      visibleSessionID,
		PreviousStatus: currentStatus,
		NewStatus:      targetStatus,
	}, nil
}

type ForkRepairSessionFromInput struct {
	SessionId          int      `json:"session_id" jsonschema:"The project-scoped session ID to fork into a new repair session."`
	Reason             string   `json:"reason,omitempty" jsonschema:"Optional concise provenance note explaining why the repair fork is being created."`
	DeclaredWriteScope []string `json:"declared_write_scope,omitempty" jsonschema:"Optional replacement declared write scope. Defaults to the source session scope."`
}

type ForkRepairSessionFromOutput struct {
	Status          string `json:"status"`
	SourceSessionId int    `json:"source_session_id"`
	NewSessionId    int    `json:"new_session_id"`
}

func ForkRepairSessionFrom(ctx context.Context, req *mcp.CallToolRequest, input ForkRepairSessionFromInput) (*mcp.CallToolResult, ForkRepairSessionFromOutput, error) {
	if authorizedRole != "fixer" {
		return &mcp.CallToolResult{IsError: true}, ForkRepairSessionFromOutput{}, fmt.Errorf("access denied: requires fixer role")
	}

	sourceSessionID, err := globalSessionIDFromProjectScoped(input.SessionId, authorizedProjectId)
	if err == sql.ErrNoRows {
		return &mcp.CallToolResult{IsError: true}, ForkRepairSessionFromOutput{}, fmt.Errorf("session not found in current project")
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, ForkRepairSessionFromOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	var taskDescription string
	var sourceWriteScope string
	err = db.QueryRow(
		`SELECT task_description, COALESCE(declared_write_scope, '')
		 FROM session
		 WHERE id = ? AND project_id = ?`,
		sourceSessionID,
		authorizedProjectId,
	).Scan(&taskDescription, &sourceWriteScope)
	if err == sql.ErrNoRows {
		return &mcp.CallToolResult{IsError: true}, ForkRepairSessionFromOutput{}, fmt.Errorf("session not found in current project")
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, ForkRepairSessionFromOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	writeScope := input.DeclaredWriteScope
	if len(writeScope) == 0 {
		writeScope, err = decodeDeclaredWriteScope(sourceWriteScope)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, ForkRepairSessionFromOutput{}, err
		}
	}
	encodedWriteScope, err := encodeDeclaredWriteScope(writeScope)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, ForkRepairSessionFromOutput{}, err
	}

	provenanceLines := []string{taskDescription, fmt.Sprintf("Repair fork source session: %d.", input.SessionId)}
	if reason := strings.TrimSpace(input.Reason); reason != "" {
		provenanceLines = append(provenanceLines, "Repair fork reason: "+reason)
	}
	newTaskDescription := strings.Join(provenanceLines, "\n\n")

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, ForkRepairSessionFromOutput{}, fmt.Errorf("DB transaction error: %v", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	result, err := tx.Exec(
		`INSERT INTO session (
			project_id,
			task_description,
			status,
			declared_write_scope,
			repair_source_session_id
		) VALUES (?, ?, 'pending', ?, ?)`,
		authorizedProjectId,
		newTaskDescription,
		encodedWriteScope,
		sourceSessionID,
	)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, ForkRepairSessionFromOutput{}, fmt.Errorf("DB insert error: %v", err)
	}

	newGlobalSessionID64, err := result.LastInsertId()
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, ForkRepairSessionFromOutput{}, fmt.Errorf("LastInsertId error: %v", err)
	}
	newGlobalSessionID := int(newGlobalSessionID64)

	if _, err := tx.Exec(
		`INSERT OR IGNORE INTO netrunner_attached_doc (session_id, project_doc_id)
		 SELECT ?, project_doc_id
		 FROM netrunner_attached_doc
		 WHERE session_id = ?`,
		newGlobalSessionID,
		sourceSessionID,
	); err != nil {
		return &mcp.CallToolResult{IsError: true}, ForkRepairSessionFromOutput{}, fmt.Errorf("DB copy error: %v", err)
	}

	if _, err := tx.Exec(
		`INSERT OR IGNORE INTO session_mcp_server (session_id, mcp_server_id)
		 SELECT ?, mcp_server_id
		 FROM session_mcp_server
		 WHERE session_id = ?`,
		newGlobalSessionID,
		sourceSessionID,
	); err != nil {
		return &mcp.CallToolResult{IsError: true}, ForkRepairSessionFromOutput{}, fmt.Errorf("DB copy error: %v", err)
	}

	if err := tx.Commit(); err != nil {
		return &mcp.CallToolResult{IsError: true}, ForkRepairSessionFromOutput{}, fmt.Errorf("DB commit error: %v", err)
	}

	newLocalSessionID, err := projectScopedSessionIDFromGlobal(newGlobalSessionID, authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, ForkRepairSessionFromOutput{}, fmt.Errorf("DB mapping error: %v", err)
	}

	return nil, ForkRepairSessionFromOutput{
		Status:          "success",
		SourceSessionId: input.SessionId,
		NewSessionId:    newLocalSessionID,
	}, nil
}

type CleanupClaimCheck struct {
	Path        string `json:"path"`
	Expectation string `json:"expectation"`
	Exists      bool   `json:"exists"`
	Matches     bool   `json:"matches"`
}

type VerifySessionCleanupClaimsInput struct {
	SessionId int `json:"session_id" jsonschema:"The project-scoped session ID whose cleanup claims should be checked against disk state."`
}

type VerifySessionCleanupClaimsOutput struct {
	Status        string              `json:"status"`
	SessionId     int                 `json:"session_id"`
	ReportPresent bool                `json:"report_present"`
	AllMatched    bool                `json:"all_matched"`
	Claims        []CleanupClaimCheck `json:"claims"`
}

func VerifySessionCleanupClaims(ctx context.Context, req *mcp.CallToolRequest, input VerifySessionCleanupClaimsInput) (*mcp.CallToolResult, VerifySessionCleanupClaimsOutput, error) {
	if authorizedRole != "fixer" {
		return &mcp.CallToolResult{IsError: true}, VerifySessionCleanupClaimsOutput{}, fmt.Errorf("access denied: requires fixer role")
	}

	globalSessionID, err := globalSessionIDFromProjectScoped(input.SessionId, authorizedProjectId)
	if err == sql.ErrNoRows {
		return &mcp.CallToolResult{IsError: true}, VerifySessionCleanupClaimsOutput{}, fmt.Errorf("session not found in current project")
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, VerifySessionCleanupClaimsOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	var report string
	err = db.QueryRow("SELECT COALESCE(report, '') FROM session WHERE id = ? AND project_id = ?", globalSessionID, authorizedProjectId).Scan(&report)
	if err == sql.ErrNoRows {
		return &mcp.CallToolResult{IsError: true}, VerifySessionCleanupClaimsOutput{}, fmt.Errorf("session not found in current project")
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, VerifySessionCleanupClaimsOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	reportPresent := strings.TrimSpace(report) != ""
	parsedReport, _, err := decodeStructuredFinalReport(report)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, VerifySessionCleanupClaimsOutput{}, err
	}

	projectCWD, err := projectCWDFromID(authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, VerifySessionCleanupClaimsOutput{}, fmt.Errorf("failed to resolve project cwd: %v", err)
	}

	claims := []CleanupClaimCheck{}
	allMatched := true

	checkPath := func(path string, expectation string) error {
		normalized, err := normalizeWriteScopePath(path)
		if err != nil {
			return err
		}
		targetPath := filepath.Join(projectCWD, filepath.FromSlash(normalized))
		_, statErr := os.Stat(targetPath)
		exists := statErr == nil
		if statErr != nil && !os.IsNotExist(statErr) {
			return statErr
		}
		matches := (expectation == "removed" && !exists) || (expectation == "present" && exists)
		if !matches {
			allMatched = false
		}
		claims = append(claims, CleanupClaimCheck{
			Path:        normalized,
			Expectation: expectation,
			Exists:      exists,
			Matches:     matches,
		})
		return nil
	}

	for _, path := range parsedReport.CleanupClaims.RemovedPaths {
		if err := checkPath(path, "removed"); err != nil {
			return &mcp.CallToolResult{IsError: true}, VerifySessionCleanupClaimsOutput{}, fmt.Errorf("filesystem check failed: %v", err)
		}
	}
	for _, path := range parsedReport.CleanupClaims.ExpectedPresentPaths {
		if err := checkPath(path, "present"); err != nil {
			return &mcp.CallToolResult{IsError: true}, VerifySessionCleanupClaimsOutput{}, fmt.Errorf("filesystem check failed: %v", err)
		}
	}

	return nil, VerifySessionCleanupClaimsOutput{
		Status:        "success",
		SessionId:     input.SessionId,
		ReportPresent: reportPresent,
		AllMatched:    allMatched,
		Claims:        claims,
	}, nil
}

type GetSessionInput struct {
	SessionId int `json:"session_id" jsonschema:"The ID of the session to read"`
}

type SessionDetails struct {
	Id                    int      `json:"id"`
	ProjectId             int      `json:"project_id"`
	TaskDescription       string   `json:"task_description"`
	Status                string   `json:"status"`
	Report                string   `json:"report"`
	CliBackend            string   `json:"cli_backend"`
	CliModel              string   `json:"cli_model,omitempty"`
	CliReasoning          string   `json:"cli_reasoning,omitempty"`
	DeclaredWriteScope    []string `json:"declared_write_scope"`
	RepairSourceSessionId int      `json:"repair_source_session_id,omitempty"`
	ReworkCount           int      `json:"rework_count"`
	ForcedStopCount       int      `json:"forced_stop_count"`
}

type GetSessionOutput struct {
	Session SessionDetails `json:"session"`
}

func GetSession(ctx context.Context, req *mcp.CallToolRequest, input GetSessionInput) (*mcp.CallToolResult, GetSessionOutput, error) {
	if authorizedRole != "fixer" && authorizedRole != "netrunner" && authorizedRole != "overseer" {
		return &mcp.CallToolResult{IsError: true}, GetSessionOutput{}, fmt.Errorf("access denied: requires authenticated role")
	}

	targetSessionID := input.SessionId
	if authorizedRole != "overseer" {
		globalSessionID, err := globalSessionIDFromProjectScoped(input.SessionId, authorizedProjectId)
		if err == sql.ErrNoRows {
			return &mcp.CallToolResult{IsError: true}, GetSessionOutput{}, fmt.Errorf("session not found in current project")
		}
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, GetSessionOutput{}, fmt.Errorf("DB query error: %v", err)
		}
		targetSessionID = globalSessionID
	}

	var session SessionDetails
	var declaredWriteScope string
	err := db.QueryRow(
		`SELECT id,
		        project_id,
		        task_description,
		        status,
		        COALESCE(report, ''),
		        COALESCE(NULLIF(TRIM(cli_backend), ''), ?),
		        COALESCE(cli_model, ''),
		        COALESCE(cli_reasoning, ''),
		        COALESCE(declared_write_scope, ''),
		        COALESCE(repair_source_session_id, 0),
		        COALESCE(rework_count, 0),
		        COALESCE(forced_stop_count, 0)
		 FROM session
		 WHERE id = ?`,
		defaultCliBackend,
		targetSessionID,
	).Scan(
		&session.Id,
		&session.ProjectId,
		&session.TaskDescription,
		&session.Status,
		&session.Report,
		&session.CliBackend,
		&session.CliModel,
		&session.CliReasoning,
		&declaredWriteScope,
		&session.RepairSourceSessionId,
		&session.ReworkCount,
		&session.ForcedStopCount,
	)
	if err == sql.ErrNoRows {
		return &mcp.CallToolResult{IsError: true}, GetSessionOutput{}, fmt.Errorf("session not found")
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetSessionOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	session.DeclaredWriteScope, err = decodeDeclaredWriteScope(declaredWriteScope)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetSessionOutput{}, fmt.Errorf("DB decode error: %v", err)
	}

	if !canAccessSession(session.ProjectId) {
		return &mcp.CallToolResult{IsError: true}, GetSessionOutput{}, fmt.Errorf("access denied: session not found in current project")
	}

	if authorizedRole != "overseer" {
		localSessionID, err := projectScopedSessionIDFromGlobal(session.Id, session.ProjectId)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, GetSessionOutput{}, fmt.Errorf("DB mapping error: %v", err)
		}
		session.Id = localSessionID
		if session.RepairSourceSessionId > 0 {
			localRepairSourceID, err := projectScopedSessionIDFromGlobal(session.RepairSourceSessionId, session.ProjectId)
			if err != nil {
				return &mcp.CallToolResult{IsError: true}, GetSessionOutput{}, fmt.Errorf("DB mapping error: %v", err)
			}
			session.RepairSourceSessionId = localRepairSourceID
		}
	}

	return nil, GetSessionOutput{Session: session}, nil
}
