package dashboardapi

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
)

func (r *Repository) ProjectNetrunners(ctx context.Context, projectID int, statuses []string) (ProjectNetrunnersResponse, error) {
	project, err := r.requireProject(ctx, projectID)
	if err != nil {
		return ProjectNetrunnersResponse{}, err
	}
	sessions, _, _, err := r.loadSessionSummaries(ctx, projectID, statuses)
	if err != nil {
		return ProjectNetrunnersResponse{}, err
	}
	return ProjectNetrunnersResponse{
		Project:  ProjectHeader{ID: project.ID, Name: project.Name, CWD: project.CWD},
		Statuses: statuses,
		Sessions: sessions,
	}, nil
}

func (r *Repository) NetrunnerDetail(ctx context.Context, sessionID int) (NetrunnerDetailResponse, error) {
	projectID, err := r.sessionProjectID(ctx, sessionID)
	if err != nil {
		return NetrunnerDetailResponse{}, err
	}
	sessions, _, _, err := r.loadSessionSummaries(ctx, projectID, nil)
	if err != nil {
		return NetrunnerDetailResponse{}, err
	}
	var summary *NetrunnerSummary
	for i := range sessions {
		if sessions[i].ID == sessionID {
			summary = &sessions[i]
			break
		}
	}
	if summary == nil {
		return NetrunnerDetailResponse{}, sql.ErrNoRows
	}
	attachedDocs, err := r.loadAttachedDocs(ctx, sessionID)
	if err != nil {
		return NetrunnerDetailResponse{}, err
	}
	mcpServers, err := r.loadMCPAssignments(ctx, sessionID)
	if err != nil {
		return NetrunnerDetailResponse{}, err
	}
	availableMCPServers, err := r.loadProjectMCPServers(ctx, projectID)
	if err != nil {
		return NetrunnerDetailResponse{}, err
	}
	proposals, err := r.loadSessionProposals(ctx, projectID, sessionID)
	if err != nil {
		return NetrunnerDetailResponse{}, err
	}
	reportRaw, structuredReport, err := r.loadSessionReport(ctx, sessionID)
	if err != nil {
		return NetrunnerDetailResponse{}, err
	}

	session := SessionDetail{
		ID:                    summary.ID,
		LocalID:               summary.LocalID,
		ProjectID:             summary.ProjectID,
		TaskDescription:       "",
		Status:                summary.Status,
		Backend:               summary.Backend,
		Model:                 summary.Model,
		Reasoning:             summary.Reasoning,
		WriteScope:            summary.WriteScope,
		ReportRaw:             reportRaw,
		StructuredFinalReport: structuredReport,
		AttachedDocs:          attachedDocs,
		MCPServers:            mcpServers,
		Proposals:             proposals,
		WorkerState:           summary.WorkerState,
		ReworkCount:           summary.ReworkCount,
		ForcedStopCount:       summary.ForcedStopCount,
		RepairSourceSessionID: summary.RepairSourceSessionID,
		LocalRepairSourceID:   summary.LocalRepairSourceID,
		AvailableMCPServers:   availableMCPServers,
	}

	if err := r.db.QueryRowContext(ctx, `
		SELECT task_description
		FROM session
		WHERE id = ?`, sessionID).Scan(&session.TaskDescription); err != nil {
		return NetrunnerDetailResponse{}, err
	}
	session.AvailableDocs, err = r.loadProjectAttachedDocOptions(ctx, projectID)
	if err != nil {
		return NetrunnerDetailResponse{}, err
	}
	session.AllowedStatusTargets, session.StatusActionNote, err = r.allowedStatusTargets(ctx, projectID, session.Status)
	if err != nil {
		return NetrunnerDetailResponse{}, err
	}

	return NetrunnerDetailResponse{Session: session}, nil
}

func (r *Repository) CreateTask(ctx context.Context, projectID int, input CreateTaskInput) (CreateTaskResponse, error) {
	if _, err := r.requireProject(ctx, projectID); err != nil {
		return CreateTaskResponse{}, err
	}
	taskDescription := strings.TrimSpace(input.TaskDescription)
	if taskDescription == "" {
		return CreateTaskResponse{}, fmt.Errorf("task_description is required")
	}
	declaredWriteScope, err := encodeStringList(input.DeclaredWriteScope)
	if err != nil {
		return CreateTaskResponse{}, err
	}
	res, err := r.dbWrite.ExecContext(
		ctx,
		"INSERT INTO session (project_id, task_description, status, declared_write_scope) VALUES (?, ?, 'pending', ?)",
		projectID,
		taskDescription,
		declaredWriteScope,
	)
	if err != nil {
		return CreateTaskResponse{}, err
	}
	sessionID64, err := res.LastInsertId()
	if err != nil {
		return CreateTaskResponse{}, err
	}
	projectSnapshot, err := r.ProjectSnapshot(ctx, projectID)
	if err != nil {
		return CreateTaskResponse{}, err
	}
	return CreateTaskResponse{
		Status:    "success",
		SessionID: int(sessionID64),
		Project:   projectSnapshot,
	}, nil
}

func (r *Repository) SetSessionAttachedDocs(ctx context.Context, sessionID int, input SetSessionAttachedDocsInput) (SessionActionResponse, error) {
	projectID, err := r.sessionProjectID(ctx, sessionID)
	if err != nil {
		return SessionActionResponse{}, err
	}
	docIDs, err := r.resolveProjectDocIDs(ctx, projectID, input.ProjectDocIDs)
	if err != nil {
		return SessionActionResponse{}, err
	}
	tx, err := r.dbWrite.BeginTx(ctx, nil)
	if err != nil {
		return SessionActionResponse{}, err
	}
	defer func() {
		_ = tx.Rollback()
	}()
	if _, err := tx.ExecContext(ctx, "DELETE FROM netrunner_attached_doc WHERE session_id = ?", sessionID); err != nil {
		return SessionActionResponse{}, err
	}
	for _, docID := range docIDs {
		if _, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO netrunner_attached_doc (session_id, project_doc_id) VALUES (?, ?)", sessionID, docID); err != nil {
			return SessionActionResponse{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return SessionActionResponse{}, err
	}
	return r.loadSessionActionResponse(ctx, sessionID, "success", "Attached docs updated.")
}

func (r *Repository) SetSessionMCPServers(ctx context.Context, sessionID int, input SetSessionMCPServersInput) (SessionActionResponse, error) {
	projectID, err := r.sessionProjectID(ctx, sessionID)
	if err != nil {
		return SessionActionResponse{}, err
	}
	serverIDs, normalizedNames, err := r.resolveMCPServerIDs(ctx, projectID, input.MCPServerNames)
	if err != nil {
		return SessionActionResponse{}, err
	}
	tx, err := r.dbWrite.BeginTx(ctx, nil)
	if err != nil {
		return SessionActionResponse{}, err
	}
	defer func() {
		_ = tx.Rollback()
	}()
	if _, err := tx.ExecContext(ctx, "DELETE FROM session_mcp_server WHERE session_id = ?", sessionID); err != nil {
		return SessionActionResponse{}, err
	}
	for _, serverID := range serverIDs {
		if _, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO session_mcp_server (session_id, mcp_server_id) VALUES (?, ?)", sessionID, serverID); err != nil {
			return SessionActionResponse{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return SessionActionResponse{}, err
	}
	return r.loadSessionActionResponse(ctx, sessionID, "success", fmt.Sprintf("Assigned %d MCP server(s): %s", len(normalizedNames), strings.Join(normalizedNames, ", ")))
}

func (r *Repository) SetSessionStatus(ctx context.Context, sessionID int, input SetSessionStatusInput) (SessionActionResponse, error) {
	projectID, err := r.sessionProjectID(ctx, sessionID)
	if err != nil {
		return SessionActionResponse{}, err
	}
	targetStatus := strings.TrimSpace(input.Status)
	if !isValidDashboardSessionStatus(targetStatus) {
		return SessionActionResponse{}, fmt.Errorf("invalid status: %s", targetStatus)
	}
	var currentStatus string
	if err := r.db.QueryRowContext(ctx, "SELECT status FROM session WHERE id = ?", sessionID).Scan(&currentStatus); err != nil {
		return SessionActionResponse{}, err
	}
	if !isAllowedDashboardSessionTransition(currentStatus, targetStatus) {
		return SessionActionResponse{}, fmt.Errorf("invalid status transition: %s -> %s", currentStatus, targetStatus)
	}
	frozen, err := r.projectOrchestrationFrozen(ctx, projectID)
	if err != nil {
		return SessionActionResponse{}, err
	}
	if frozen && targetStatus != currentStatus {
		return SessionActionResponse{}, fmt.Errorf("orchestration is frozen for project %d; explicit resume is required before changing session status", projectID)
	}
	tx, err := r.dbWrite.BeginTx(ctx, nil)
	if err != nil {
		return SessionActionResponse{}, err
	}
	defer func() {
		_ = tx.Rollback()
	}()
	if _, err := tx.ExecContext(ctx, "UPDATE session SET status = ? WHERE id = ?", targetStatus, sessionID); err != nil {
		return SessionActionResponse{}, err
	}
	if targetStatus == "pending" && (currentStatus == "review" || currentStatus == "completed") && currentStatus != targetStatus {
		if _, err := tx.ExecContext(ctx, "UPDATE session SET rework_count = COALESCE(rework_count, 0) + 1 WHERE id = ?", sessionID); err != nil {
			return SessionActionResponse{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return SessionActionResponse{}, err
	}
	return r.loadSessionActionResponse(ctx, sessionID, "success", fmt.Sprintf("Session status changed from %s to %s.", currentStatus, targetStatus))
}
func (r *Repository) loadSessionSummaries(ctx context.Context, projectID int, statuses []string) ([]NetrunnerSummary, map[int]StatusCounts, StatusCounts, error) {
	statusFilter := normalizeStatuses(statuses)
	query := `
		SELECT
			s.id,
			s.project_id,
			(
				SELECT COUNT(*)
				FROM session s2
				WHERE s2.project_id = s.project_id AND s2.id <= s.id
			) AS local_session_id,
			s.task_description,
			s.status,
			COALESCE(NULLIF(TRIM(s.cli_backend), ''), 'codex') AS cli_backend,
			COALESCE(s.cli_model, ''),
			COALESCE(s.cli_reasoning, ''),
			COALESCE(s.declared_write_scope, '["."]'),
			COALESCE(s.repair_source_session_id, 0),
			COALESCE(s.rework_count, 0),
			COALESCE(s.forced_stop_count, 0),
			COALESCE(docs.doc_count, 0),
			COALESCE(mcps.mcp_count, 0),
			COALESCE(proposals.proposal_count, 0),
			COALESCE(proposals.pending_proposal_count, 0),
			COALESCE(worker.running_count, 0)
		FROM session s
		LEFT JOIN (
			SELECT session_id, COUNT(*) AS doc_count
			FROM netrunner_attached_doc
			GROUP BY session_id
		) docs ON docs.session_id = s.id
		LEFT JOIN (
			SELECT session_id, COUNT(*) AS mcp_count
			FROM session_mcp_server
			GROUP BY session_id
		) mcps ON mcps.session_id = s.id
		LEFT JOIN (
			SELECT
				session_id,
				COUNT(*) AS proposal_count,
				SUM(CASE WHEN status = 'pending' THEN 1 ELSE 0 END) AS pending_proposal_count
			FROM doc_proposal
			GROUP BY session_id
		) proposals ON proposals.session_id = s.id
		LEFT JOIN (
			SELECT session_id, COUNT(*) AS running_count
			FROM worker_process
			WHERE status = 'running'
			GROUP BY session_id
		) worker ON worker.session_id = s.id
	`
	args := []any{}
	clauses := []string{}
	if projectID > 0 {
		clauses = append(clauses, "s.project_id = ?")
		args = append(args, projectID)
	}
	if len(statusFilter) > 0 {
		placeholders := make([]string, 0, len(statusFilter))
		for _, status := range statusFilter {
			placeholders = append(placeholders, "?")
			args = append(args, status)
		}
		clauses = append(clauses, "s.status IN ("+strings.Join(placeholders, ",")+")")
	}
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}
	query += " ORDER BY s.project_id, s.id"

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, nil, StatusCounts{}, err
	}
	defer rows.Close()

	summaries := []NetrunnerSummary{}
	countsByProject := map[int]StatusCounts{}
	globalCounts := StatusCounts{}
	runningBySession, err := r.loadRunningWorkersBySession(ctx)
	if err != nil {
		return nil, nil, StatusCounts{}, err
	}
	localIDsBySession, err := r.loadLocalSessionIDs(ctx)
	if err != nil {
		return nil, nil, StatusCounts{}, err
	}

	for rows.Next() {
		var summary NetrunnerSummary
		var taskDescription string
		var declaredWriteScope string
		var runningCount int
		if err := rows.Scan(
			&summary.ID,
			&summary.ProjectID,
			&summary.LocalID,
			&taskDescription,
			&summary.Status,
			&summary.Backend,
			&summary.Model,
			&summary.Reasoning,
			&declaredWriteScope,
			&summary.RepairSourceSessionID,
			&summary.ReworkCount,
			&summary.ForcedStopCount,
			&summary.AttachedDocCount,
			&summary.MCPCount,
			&summary.ProposalCount,
			&summary.PendingProposalCount,
			&runningCount,
		); err != nil {
			return nil, nil, StatusCounts{}, err
		}
		summary.WriteScope = decodeStringList(declaredWriteScope)
		summary.Headline = firstLineOrFallback(taskDescription, fmt.Sprintf("Session #%d", summary.LocalID))
		summary.TaskPreview = preview(taskDescription, 220)
		if summary.RepairSourceSessionID > 0 {
			summary.LocalRepairSourceID = localIDsBySession[summary.RepairSourceSessionID]
		}
		summary.WorkerState = WorkerStateSummary{
			RunningCount: runningCount,
			HasRunning:   runningCount > 0,
			Processes:    runningBySession[summary.ID],
		}
		summaries = append(summaries, summary)
		counts := countsByProject[summary.ProjectID]
		counts.bump(summary.Status)
		countsByProject[summary.ProjectID] = counts
		globalCounts.bump(summary.Status)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, StatusCounts{}, err
	}
	return summaries, countsByProject, globalCounts, nil
}
func (r *Repository) loadMCPAssignments(ctx context.Context, sessionID int) ([]MCPServerAssignment, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT s.id, s.name, COALESCE(s.short_description, ''), COALESCE(s.category, ''), COALESCE(s.how_to, '')
		FROM session_mcp_server sms
		INNER JOIN mcp_server s ON s.id = sms.mcp_server_id
		WHERE sms.session_id = ?
		ORDER BY s.name`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	assignments := []MCPServerAssignment{}
	for rows.Next() {
		var assignment MCPServerAssignment
		if err := rows.Scan(&assignment.ID, &assignment.Name, &assignment.ShortDescription, &assignment.Category, &assignment.HowTo); err != nil {
			return nil, err
		}
		assignments = append(assignments, assignment)
	}
	return assignments, rows.Err()
}

func (r *Repository) loadProjectMCPServers(ctx context.Context, projectID int) ([]MCPServerAssignment, error) {
	if r.tableExists(ctx, "project_mcp_server") {
		rows, err := r.db.QueryContext(ctx, `
			SELECT s.id, s.name, COALESCE(s.short_description, ''), COALESCE(s.category, ''), COALESCE(s.how_to, '')
			FROM project_mcp_server pms
			INNER JOIN mcp_server s ON s.id = pms.mcp_server_id
			WHERE pms.project_id = ?
			ORDER BY s.name`, projectID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		servers := []MCPServerAssignment{}
		for rows.Next() {
			var server MCPServerAssignment
			if err := rows.Scan(&server.ID, &server.Name, &server.ShortDescription, &server.Category, &server.HowTo); err != nil {
				return nil, err
			}
			servers = append(servers, server)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
		if len(servers) > 0 {
			return servers, nil
		}
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, name, COALESCE(short_description, ''), COALESCE(category, ''), COALESCE(how_to, '')
		FROM mcp_server
		ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	servers := []MCPServerAssignment{}
	for rows.Next() {
		var server MCPServerAssignment
		if err := rows.Scan(&server.ID, &server.Name, &server.ShortDescription, &server.Category, &server.HowTo); err != nil {
			return nil, err
		}
		servers = append(servers, server)
	}
	return servers, rows.Err()
}
func (r *Repository) loadSessionReport(ctx context.Context, sessionID int) (string, *FinalReport, error) {
	var reportRaw string
	if err := r.db.QueryRowContext(ctx, `SELECT COALESCE(report, '') FROM session WHERE id = ?`, sessionID).Scan(&reportRaw); err != nil {
		return "", nil, err
	}
	return reportRaw, decodeStructuredFinalReport(reportRaw), nil
}

func (r *Repository) sessionProjectID(ctx context.Context, sessionID int) (int, error) {
	var projectID int
	err := r.db.QueryRowContext(ctx, `SELECT project_id FROM session WHERE id = ?`, sessionID).Scan(&projectID)
	return projectID, err
}

func (r *Repository) loadSessionActionResponse(ctx context.Context, sessionID int, status string, message string) (SessionActionResponse, error) {
	session, err := r.NetrunnerDetail(ctx, sessionID)
	if err != nil {
		return SessionActionResponse{}, err
	}
	return SessionActionResponse{
		Status:  status,
		Message: message,
		Session: session,
	}, nil
}
func (r *Repository) resolveMCPServerIDs(ctx context.Context, projectID int, serverNames []string) ([]int, []string, error) {
	normalizedNames := normalizeStringIDs(serverNames)
	allowedServers, err := r.loadProjectMCPServers(ctx, projectID)
	if err != nil {
		return nil, nil, err
	}
	allowed := map[string]int{}
	for _, server := range allowedServers {
		allowed[server.Name] = server.ID
	}
	missing := []string{}
	serverIDs := make([]int, 0, len(normalizedNames))
	for _, name := range normalizedNames {
		serverID, ok := allowed[name]
		if !ok {
			missing = append(missing, name)
			continue
		}
		serverIDs = append(serverIDs, serverID)
	}
	if len(missing) > 0 {
		return nil, nil, fmt.Errorf("MCP server(s) not allowed for current project: %s", strings.Join(missing, ", "))
	}
	return serverIDs, normalizedNames, nil
}

func (r *Repository) projectOrchestrationFrozen(ctx context.Context, projectID int) (bool, error) {
	if !r.tableExists(ctx, "autonomous_run_status") {
		return false, nil
	}
	var frozen int
	err := r.db.QueryRowContext(ctx, `
		SELECT COALESCE(orchestration_frozen, 0)
		FROM autonomous_run_status
		WHERE project_id = ?`, projectID).Scan(&frozen)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return frozen != 0, nil
}

func (r *Repository) allowedStatusTargets(ctx context.Context, projectID int, currentStatus string) ([]string, string, error) {
	if !isValidDashboardSessionStatus(currentStatus) {
		return []string{}, "Current status is not recognized by the dashboard action policy.", nil
	}
	allowed := allowedDashboardSessionTransitions[currentStatus]
	targets := make([]string, 0, len(allowed))
	for target := range allowed {
		targets = append(targets, target)
	}
	sort.Strings(targets)
	frozen, err := r.projectOrchestrationFrozen(ctx, projectID)
	if err != nil {
		return nil, "", err
	}
	if frozen {
		return []string{currentStatus}, fmt.Sprintf("Orchestration is frozen for project %d, so status changes are disabled until an explicit resume.", projectID), nil
	}
	return targets, "", nil
}
func (r *Repository) loadLocalSessionIDs(ctx context.Context) (map[int]int, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT
			s.id,
			(
				SELECT COUNT(*)
				FROM session s2
				WHERE s2.project_id = s.project_id AND s2.id <= s.id
			) AS local_session_id
		FROM session s`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := map[int]int{}
	for rows.Next() {
		var globalID, localID int
		if err := rows.Scan(&globalID, &localID); err != nil {
			return nil, err
		}
		ids[globalID] = localID
	}
	return ids, rows.Err()
}

var validDashboardSessionStatuses = map[string]struct{}{
	"pending":     {},
	"in_progress": {},
	"review":      {},
	"completed":   {},
}

var allowedDashboardSessionTransitions = map[string]map[string]struct{}{
	"pending": {
		"pending":     {},
		"in_progress": {},
	},
	"in_progress": {
		"in_progress": {},
		"pending":     {},
		"review":      {},
	},
	"review": {
		"review":      {},
		"pending":     {},
		"in_progress": {},
		"completed":   {},
	},
	"completed": {
		"completed":   {},
		"pending":     {},
		"in_progress": {},
		"review":      {},
	},
}

func isValidDashboardSessionStatus(status string) bool {
	_, exists := validDashboardSessionStatuses[status]
	return exists
}

func isAllowedDashboardSessionTransition(fromStatus string, toStatus string) bool {
	targets, exists := allowedDashboardSessionTransitions[fromStatus]
	if !exists {
		return false
	}
	_, allowed := targets[toStatus]
	return allowed
}
func (c *StatusCounts) bump(status string) {
	switch status {
	case "pending":
		c.Pending++
	case "in_progress":
		c.InProgress++
	case "review":
		c.Review++
	case "completed":
		c.Completed++
	default:
		c.Other++
	}
	c.Total++
}
