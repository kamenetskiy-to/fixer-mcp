package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type GetProjectsInput struct{}

type GetProjectsOutput struct {
	Projects []string `json:"projects"`
}

func GetProjects(ctx context.Context, req *mcp.CallToolRequest, input GetProjectsInput) (*mcp.CallToolResult, GetProjectsOutput, error) {
	if authorizedRole != "overseer" {
		return &mcp.CallToolResult{IsError: true}, GetProjectsOutput{}, fmt.Errorf("access denied: requires overseer role. current role: %s", authorizedRole)
	}

	rows, err := db.Query("SELECT name FROM project")
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetProjectsOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	var projects []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return &mcp.CallToolResult{IsError: true}, GetProjectsOutput{}, fmt.Errorf("DB scan error: %v", err)
		}
		projects = append(projects, name)
	}
	if err := rows.Err(); err != nil {
		return &mcp.CallToolResult{IsError: true}, GetProjectsOutput{}, fmt.Errorf("DB rows error: %v", err)
	}

	if projects == nil {
		projects = []string{}
	}

	return nil, GetProjectsOutput{Projects: projects}, nil
}

type RegisterProjectInput struct {
	Cwd  string `json:"cwd" jsonschema:"required absolute path"`
	Name string `json:"name,omitempty" jsonschema:"optional; default basename(cwd)"`
}

type RegisterProjectOutput struct {
	ProjectId int    `json:"project_id"`
	Status    string `json:"status"`
	Name      string `json:"name"`
	Cwd       string `json:"cwd"`
}

func RegisterProject(ctx context.Context, req *mcp.CallToolRequest, input RegisterProjectInput) (*mcp.CallToolResult, RegisterProjectOutput, error) {
	if authorizedRole != "overseer" {
		return &mcp.CallToolResult{IsError: true}, RegisterProjectOutput{}, fmt.Errorf("access denied: requires overseer role")
	}

	normalizedCWD, err := normalizeProjectCWD(input.Cwd)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, RegisterProjectOutput{}, fmt.Errorf("invalid cwd: %v", err)
	}

	info, err := os.Stat(normalizedCWD)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, RegisterProjectOutput{}, fmt.Errorf("invalid cwd: path does not exist")
	}
	if !info.IsDir() {
		return &mcp.CallToolResult{IsError: true}, RegisterProjectOutput{}, fmt.Errorf("invalid cwd: path is not a directory")
	}

	if projectID, storedName, storedCWD, findErr := findProjectByCWD(normalizedCWD); findErr == nil {
		return nil, RegisterProjectOutput{
			ProjectId: projectID,
			Status:    "exists",
			Name:      storedName,
			Cwd:       storedCWD,
		}, nil
	} else if findErr != sql.ErrNoRows {
		return &mcp.CallToolResult{IsError: true}, RegisterProjectOutput{}, fmt.Errorf("DB query error: %v", findErr)
	}

	requestedName := strings.TrimSpace(input.Name)
	if requestedName == "" {
		requestedName = defaultProjectName(normalizedCWD)
	}

	res, err := db.Exec(
		`INSERT INTO project (name, cwd)
		 SELECT ?, ?
		 WHERE NOT EXISTS (SELECT 1 FROM project WHERE cwd = ?)`,
		requestedName,
		normalizedCWD,
		normalizedCWD,
	)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, RegisterProjectOutput{}, fmt.Errorf("DB insert error: %v", err)
	}

	status := "exists"
	if rowsAffected, rowsErr := res.RowsAffected(); rowsErr == nil && rowsAffected > 0 {
		status = "created"
	}

	var projectID int
	var storedName string
	var storedCWD string
	err = db.QueryRow(
		"SELECT id, name, cwd FROM project WHERE cwd = ?",
		normalizedCWD,
	).Scan(&projectID, &storedName, &storedCWD)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, RegisterProjectOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	return nil, RegisterProjectOutput{
		ProjectId: projectID,
		Status:    status,
		Name:      storedName,
		Cwd:       storedCWD,
	}, nil
}

type AutonomousRunStatusRecord struct {
	ProjectId                        int    `json:"project_id"`
	SessionId                        int    `json:"session_id,omitempty"`
	State                            string `json:"state"`
	Summary                          string `json:"summary"`
	Focus                            string `json:"focus,omitempty"`
	Blocker                          string `json:"blocker,omitempty"`
	Evidence                         string `json:"evidence,omitempty"`
	OrchestrationEpoch               int    `json:"orchestration_epoch"`
	OrchestrationFrozen              bool   `json:"orchestration_frozen"`
	NotificationsEnabledForActiveRun bool   `json:"notifications_enabled_for_active_run"`
	UpdatedAt                        string `json:"updated_at"`
}

type SetAutonomousRunStatusInput struct {
	ProjectId           int    `json:"project_id,omitempty" jsonschema:"Optional project ID when called by overseer; fixer uses the bound project."`
	SessionId           int    `json:"session_id,omitempty" jsonschema:"Optional session ID for the current autonomous step."`
	State               string `json:"state" jsonschema:"Autonomous run state: running | blocked | awaiting_review | awaiting_next_dispatch | completed | idle"`
	Summary             string `json:"summary" jsonschema:"Short human-readable status summary"`
	Focus               string `json:"focus,omitempty" jsonschema:"Optional current focus"`
	Blocker             string `json:"blocker,omitempty" jsonschema:"Optional blocker note"`
	Evidence            string `json:"evidence,omitempty" jsonschema:"Optional evidence or context"`
	ResumeOrchestration bool   `json:"resume_orchestration,omitempty" jsonschema:"When true, explicitly clears the orchestration freeze and re-enables notifications for the active run."`
}

type SetAutonomousRunStatusOutput struct {
	Status string                    `json:"status"`
	Record AutonomousRunStatusRecord `json:"record"`
}

type GetAutonomousRunStatusInput struct {
	ProjectId int `json:"project_id,omitempty" jsonschema:"Optional project ID when called by overseer; fixer/netrunner use the bound project."`
}

type GetAutonomousRunStatusOutput struct {
	ProjectId int                       `json:"project_id"`
	HasStatus bool                      `json:"has_status"`
	Status    AutonomousRunStatusRecord `json:"status"`
}

type SendOperatorTelegramNotificationInput struct {
	ProjectId int    `json:"project_id,omitempty" jsonschema:"Optional project ID when called by overseer; fixer/netrunner use the bound project."`
	Source    string `json:"source" jsonschema:"Actor/source label shown in the Telegram message, in plain Russian."`
	Status    string `json:"status" jsonschema:"Concise Russian status line for the operator."`
	Summary   string `json:"summary,omitempty" jsonschema:"Optional one-line Russian summary."`
	SessionId int    `json:"session_id,omitempty" jsonschema:"Optional session context. Fixer/netrunner use local session IDs; overseer uses the global session ID."`
	RunState  string `json:"run_state,omitempty" jsonschema:"Optional run-state context such as blocked, running, awaiting_review, completed."`
	Details   string `json:"details,omitempty" jsonschema:"Optional compact details line in plain Russian."`
}

type SendOperatorTelegramNotificationOutput struct {
	Status      string `json:"status"`
	ProjectId   int    `json:"project_id"`
	ProjectName string `json:"project_name"`
	SessionId   int    `json:"session_id,omitempty"`
	ChatId      string `json:"chat_id"`
	Message     string `json:"message"`
}

func resolveAutonomousProjectID(projectID int) (int, error) {
	if authorizedRole == "overseer" {
		if projectID > 0 {
			return projectID, nil
		}
		return 0, fmt.Errorf("project_id is required for overseer")
	}
	if authorizedProjectId <= 0 {
		return 0, fmt.Errorf("project context is unavailable")
	}
	if projectID > 0 && projectID != authorizedProjectId {
		return 0, fmt.Errorf("access denied: project_id does not match current project")
	}
	return authorizedProjectId, nil
}

func resolveProjectHandoffProjectID(projectID int) (int, error) {
	switch authorizedRole {
	case "fixer":
		if authorizedProjectId <= 0 {
			return 0, fmt.Errorf("project context is unavailable")
		}
		if projectID > 0 && projectID != authorizedProjectId {
			return 0, fmt.Errorf("access denied: project_id does not match current project")
		}
		return authorizedProjectId, nil
	case "overseer":
		if projectID <= 0 {
			return 0, fmt.Errorf("project_id is required for overseer")
		}
		exists, err := projectExists(projectID)
		if err != nil {
			return 0, err
		}
		if !exists {
			return 0, sql.ErrNoRows
		}
		return projectID, nil
	default:
		return 0, fmt.Errorf("access denied: requires fixer or overseer role")
	}
}

func resolveAutonomousSessionID(sessionID int, projectID int) (int, error) {
	if sessionID <= 0 {
		return 0, nil
	}
	if authorizedRole == "overseer" {
		var belongingProjectID int
		err := db.QueryRow("SELECT project_id FROM session WHERE id = ?", sessionID).Scan(&belongingProjectID)
		if err != nil {
			return 0, err
		}
		if projectID > 0 && belongingProjectID != projectID {
			return 0, fmt.Errorf("session does not belong to project %d", projectID)
		}
		return sessionID, nil
	}

	globalSessionID, err := globalSessionIDFromProjectScoped(sessionID, projectID)
	if err != nil {
		return 0, err
	}
	belongs, err := sessionBelongsToProject(globalSessionID, projectID)
	if err != nil {
		return 0, err
	}
	if !belongs {
		return 0, sql.ErrNoRows
	}
	return globalSessionID, nil
}

func fetchAutonomousRunStatusRecord(projectID int) (AutonomousRunStatusRecord, error) {
	var record AutonomousRunStatusRecord
	var frozenInt int
	var notificationsEnabled int
	err := db.QueryRow(
		`SELECT project_id,
		        COALESCE(session_id, 0),
		        state,
		        summary,
		        COALESCE(focus, ''),
		        COALESCE(blocker, ''),
		        COALESCE(evidence, ''),
		        COALESCE(orchestration_epoch, 0),
		        COALESCE(orchestration_frozen, 0),
		        COALESCE(notifications_enabled_for_active_run, 1),
		        updated_at
		 FROM autonomous_run_status
		 WHERE project_id = ?`,
		projectID,
	).Scan(
		&record.ProjectId,
		&record.SessionId,
		&record.State,
		&record.Summary,
		&record.Focus,
		&record.Blocker,
		&record.Evidence,
		&record.OrchestrationEpoch,
		&frozenInt,
		&notificationsEnabled,
		&record.UpdatedAt,
	)
	if err != nil {
		return AutonomousRunStatusRecord{}, err
	}
	record.OrchestrationFrozen = frozenInt != 0
	record.NotificationsEnabledForActiveRun = notificationsEnabled != 0
	return record, nil
}

func SetAutonomousRunStatus(ctx context.Context, req *mcp.CallToolRequest, input SetAutonomousRunStatusInput) (*mcp.CallToolResult, SetAutonomousRunStatusOutput, error) {
	if authorizedRole != "fixer" && authorizedRole != "overseer" {
		return &mcp.CallToolResult{IsError: true}, SetAutonomousRunStatusOutput{}, fmt.Errorf("access denied: requires fixer or overseer role")
	}

	projectID, err := resolveAutonomousProjectID(input.ProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetAutonomousRunStatusOutput{}, err
	}

	state, err := normalizeAutonomousStatusLabel(input.State)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetAutonomousRunStatusOutput{}, err
	}

	sessionID, err := resolveAutonomousSessionID(input.SessionId, projectID)
	if err != nil {
		if err == sql.ErrNoRows {
			return &mcp.CallToolResult{IsError: true}, SetAutonomousRunStatusOutput{}, fmt.Errorf("session not found in current project")
		}
		return &mcp.CallToolResult{IsError: true}, SetAutonomousRunStatusOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	summary := strings.TrimSpace(input.Summary)
	if summary == "" {
		return &mcp.CallToolResult{IsError: true}, SetAutonomousRunStatusOutput{}, fmt.Errorf("summary is required")
	}

	focus := strings.TrimSpace(input.Focus)
	blocker := strings.TrimSpace(input.Blocker)
	evidence := strings.TrimSpace(input.Evidence)

	control, exists, err := fetchOrchestrationControl(projectID)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetAutonomousRunStatusOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	if !exists {
		control.OrchestrationEpoch = 0
		control.NotificationsEnabledForActiveRun = true
		control.OrchestrationFrozen = false
	}
	if input.ResumeOrchestration {
		control.OrchestrationFrozen = false
		control.NotificationsEnabledForActiveRun = true
	}

	err = upsertOrchestrationControl(
		projectID,
		sessionID,
		state,
		summary,
		focus,
		blocker,
		evidence,
		control.OrchestrationEpoch,
		control.OrchestrationFrozen,
		control.NotificationsEnabledForActiveRun,
	)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetAutonomousRunStatusOutput{}, fmt.Errorf("DB upsert error: %v", err)
	}

	record, err := fetchAutonomousRunStatusRecord(projectID)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetAutonomousRunStatusOutput{}, err
	}

	return nil, SetAutonomousRunStatusOutput{Status: "success", Record: record}, nil
}

func GetAutonomousRunStatus(ctx context.Context, req *mcp.CallToolRequest, input GetAutonomousRunStatusInput) (*mcp.CallToolResult, GetAutonomousRunStatusOutput, error) {
	if authorizedRole != "fixer" && authorizedRole != "netrunner" && authorizedRole != "overseer" {
		return &mcp.CallToolResult{IsError: true}, GetAutonomousRunStatusOutput{}, fmt.Errorf("access denied: requires authenticated role")
	}

	projectID, err := resolveAutonomousProjectID(input.ProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetAutonomousRunStatusOutput{}, err
	}

	record, err := fetchAutonomousRunStatusRecord(projectID)
	if err == sql.ErrNoRows {
		return nil, GetAutonomousRunStatusOutput{
			ProjectId: projectID,
			HasStatus: false,
			Status:    AutonomousRunStatusRecord{ProjectId: projectID, NotificationsEnabledForActiveRun: true},
		}, nil
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetAutonomousRunStatusOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	return nil, GetAutonomousRunStatusOutput{
		ProjectId: projectID,
		HasStatus: true,
		Status:    record,
	}, nil
}

func SendOperatorTelegramNotification(ctx context.Context, req *mcp.CallToolRequest, input SendOperatorTelegramNotificationInput) (*mcp.CallToolResult, SendOperatorTelegramNotificationOutput, error) {
	if authorizedRole != "fixer" && authorizedRole != "netrunner" && authorizedRole != "overseer" {
		return &mcp.CallToolResult{IsError: true}, SendOperatorTelegramNotificationOutput{}, fmt.Errorf("access denied: requires authenticated role")
	}

	projectID, err := resolveAutonomousProjectID(input.ProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SendOperatorTelegramNotificationOutput{}, err
	}

	control, _, err := fetchOrchestrationControl(projectID)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SendOperatorTelegramNotificationOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	if !control.NotificationsEnabledForActiveRun {
		return &mcp.CallToolResult{IsError: true}, SendOperatorTelegramNotificationOutput{}, fmt.Errorf("operator notifications are disabled for the active run; explicit orchestration resume is required before sending routine updates")
	}

	projectName, err := projectNameFromID(projectID)
	if err != nil {
		if err == sql.ErrNoRows {
			return &mcp.CallToolResult{IsError: true}, SendOperatorTelegramNotificationOutput{}, fmt.Errorf("project not found")
		}
		return &mcp.CallToolResult{IsError: true}, SendOperatorTelegramNotificationOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	source := truncateRunes(normalizeCompactText(input.Source), 80)
	if source == "" {
		return &mcp.CallToolResult{IsError: true}, SendOperatorTelegramNotificationOutput{}, fmt.Errorf("source is required")
	}

	statusText := truncateRunes(normalizeCompactText(input.Status), 120)
	if statusText == "" {
		return &mcp.CallToolResult{IsError: true}, SendOperatorTelegramNotificationOutput{}, fmt.Errorf("status is required")
	}

	summary := truncateRunes(normalizeCompactText(input.Summary), 220)
	runState := truncateRunes(normalizeCompactText(input.RunState), 60)
	details := truncateRunes(normalizeCompactText(input.Details), 280)

	visibleSessionID := 0
	switch {
	case input.SessionId > 0 && authorizedRole == "overseer":
		var sessionProjectID int
		err := db.QueryRow("SELECT project_id FROM session WHERE id = ?", input.SessionId).Scan(&sessionProjectID)
		if err == sql.ErrNoRows {
			return &mcp.CallToolResult{IsError: true}, SendOperatorTelegramNotificationOutput{}, fmt.Errorf("session not found")
		}
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, SendOperatorTelegramNotificationOutput{}, fmt.Errorf("DB query error: %v", err)
		}
		if sessionProjectID != projectID {
			return &mcp.CallToolResult{IsError: true}, SendOperatorTelegramNotificationOutput{}, fmt.Errorf("session does not belong to project %d", projectID)
		}
		visibleSessionID = input.SessionId
	case input.SessionId > 0:
		globalSessionID, err := globalSessionIDFromProjectScoped(input.SessionId, projectID)
		if err == sql.ErrNoRows {
			return &mcp.CallToolResult{IsError: true}, SendOperatorTelegramNotificationOutput{}, fmt.Errorf("session not found in current project")
		}
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, SendOperatorTelegramNotificationOutput{}, fmt.Errorf("DB query error: %v", err)
		}
		belongs, err := sessionBelongsToProject(globalSessionID, projectID)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, SendOperatorTelegramNotificationOutput{}, fmt.Errorf("DB query error: %v", err)
		}
		if !belongs {
			return &mcp.CallToolResult{IsError: true}, SendOperatorTelegramNotificationOutput{}, fmt.Errorf("session not found in current project")
		}
		visibleSessionID = input.SessionId
	case authorizedRole == "netrunner" && authorizedSessionId > 0:
		_, mappedSessionID, err := resolveAuthorizedNetrunnerSessionID("send_operator_telegram_notification", nil)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, SendOperatorTelegramNotificationOutput{}, fmt.Errorf("failed to map active session to project-scoped id: %v", err)
		}
		visibleSessionID = mappedSessionID
	}

	message := renderTelegramOperatorNotification(projectName, projectID, source, statusText, summary, visibleSessionID, runState, details)
	botToken, chatID, apiBaseURL, err := resolveTelegramOperatorConfigFromEnv()
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SendOperatorTelegramNotificationOutput{}, err
	}
	if err := sendTelegramText(ctx, botToken, chatID, apiBaseURL, message); err != nil {
		return &mcp.CallToolResult{IsError: true}, SendOperatorTelegramNotificationOutput{}, err
	}

	return nil, SendOperatorTelegramNotificationOutput{
		Status:      "success",
		ProjectId:   projectID,
		ProjectName: projectName,
		SessionId:   visibleSessionID,
		ChatId:      chatID,
		Message:     message,
	}, nil
}

type ProjectHandoffRecord struct {
	ProjectId int    `json:"project_id"`
	Content   string `json:"content"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

type SetProjectHandoffInput struct {
	ProjectId int    `json:"project_id,omitempty" jsonschema:"Optional project ID when called by overseer; fixer uses the bound project."`
	Content   string `json:"content" jsonschema:"Concise project handoff content to persist for startup reuse."`
}

type SetProjectHandoffOutput struct {
	Status string               `json:"status"`
	Record ProjectHandoffRecord `json:"record"`
}

type GetProjectHandoffInput struct {
	ProjectId int `json:"project_id,omitempty" jsonschema:"Optional project ID when called by overseer; fixer uses the bound project."`
}

type GetProjectHandoffOutput struct {
	ProjectId  int                  `json:"project_id"`
	HasHandoff bool                 `json:"has_handoff"`
	Handoff    ProjectHandoffRecord `json:"handoff"`
}

type ClearProjectHandoffInput struct {
	ProjectId int `json:"project_id,omitempty" jsonschema:"Optional project ID when called by overseer; fixer uses the bound project."`
}

type ClearProjectHandoffOutput struct {
	Status    string `json:"status"`
	ProjectId int    `json:"project_id"`
}

type OverseerFixerMessageRecord struct {
	Id         int    `json:"id"`
	ProjectId  int    `json:"project_id"`
	SenderRole string `json:"sender_role"`
	Content    string `json:"content"`
	CreatedAt  string `json:"created_at"`
}

type AppendOverseerFixerMessageInput struct {
	ProjectId  int    `json:"project_id,omitempty" jsonschema:"Optional project ID when called by overseer; fixer uses the bound project."`
	SenderRole string `json:"sender_role" jsonschema:"Message sender role. Must match the authenticated role: overseer or fixer."`
	Content    string `json:"content" jsonschema:"Message content."`
}

type AppendOverseerFixerMessageOutput struct {
	Status  string                     `json:"status"`
	Message OverseerFixerMessageRecord `json:"message"`
}

type GetOverseerFixerMessagesInput struct {
	ProjectId int `json:"project_id,omitempty" jsonschema:"Optional project ID when called by overseer; fixer uses the bound project."`
	Limit     int `json:"limit,omitempty" jsonschema:"Maximum messages to return. Defaults to 10."`
}

type GetOverseerFixerMessagesOutput struct {
	ProjectId int                          `json:"project_id"`
	Messages  []OverseerFixerMessageRecord `json:"messages"`
}

type OverseerFixerRunStateRecord struct {
	ProjectId     int    `json:"project_id"`
	Active        bool   `json:"active"`
	Status        string `json:"status,omitempty"`
	Reason        string `json:"reason,omitempty"`
	LastMessageId int    `json:"last_message_id,omitempty"`
	UpdatedAt     string `json:"updated_at,omitempty"`
}

type SetOverseerFixerRunStateInput struct {
	ProjectId     int    `json:"project_id,omitempty" jsonschema:"Optional project ID when called by overseer; fixer uses the bound project."`
	Active        bool   `json:"active" jsonschema:"Whether the overseer-directed Fixer run is active."`
	Status        string `json:"status,omitempty" jsonschema:"Compact status label or summary."`
	Reason        string `json:"reason,omitempty" jsonschema:"Compact reason for the state change."`
	LastMessageId int    `json:"last_message_id,omitempty" jsonschema:"Optional message cursor to store with the run state."`
}

type SetOverseerFixerRunStateOutput struct {
	Status string                      `json:"status"`
	State  OverseerFixerRunStateRecord `json:"state"`
}

type GetOverseerFixerRunStateInput struct {
	ProjectId int `json:"project_id,omitempty" jsonschema:"Optional project ID when called by overseer; fixer uses the bound project."`
}

type GetOverseerFixerRunStateOutput struct {
	ProjectId int                         `json:"project_id"`
	HasState  bool                        `json:"has_state"`
	State     OverseerFixerRunStateRecord `json:"state"`
}

type WaitForOverseerFixerMessagesInput struct {
	ProjectIds     []int `json:"project_ids,omitempty" jsonschema:"Optional project IDs to watch. When empty, all active fixer-run projects are watched."`
	AfterMessageId int   `json:"after_message_id,omitempty" jsonschema:"Only fixer messages with id greater than this cursor are returned."`
	TimeoutMs      int   `json:"timeout_ms,omitempty" jsonschema:"Maximum wait in milliseconds. Defaults to 1000."`
	PollIntervalMs int   `json:"poll_interval_ms,omitempty" jsonschema:"Polling interval in milliseconds. Defaults to 50."`
}

type WaitForOverseerFixerMessagesOutput struct {
	Status          string                       `json:"status"`
	TimedOut        bool                         `json:"timed_out"`
	ProjectIds      []int                        `json:"project_ids"`
	AfterMessageId  int                          `json:"after_message_id"`
	CursorMessageId int                          `json:"cursor_message_id"`
	Messages        []OverseerFixerMessageRecord `json:"messages"`
}

type ProjectActivityRecord struct {
	ProjectId int    `json:"project_id"`
	Activity  string `json:"activity"`
	Active    bool   `json:"active"`
}

type SetProjectActivityInput struct {
	ProjectId int    `json:"project_id,omitempty" jsonschema:"Optional project ID when called by overseer; fixer uses the bound project."`
	Activity  string `json:"activity" jsonschema:"Project activity state: active or passive."`
}

type SetProjectActivityOutput struct {
	Status string                `json:"status"`
	Record ProjectActivityRecord `json:"record"`
}

type ProjectOverviewRecord struct {
	ProjectId int    `json:"project_id"`
	Content   string `json:"content"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

type SetProjectOverviewInput struct {
	ProjectId int    `json:"project_id,omitempty" jsonschema:"Optional project ID when called by overseer; fixer uses the bound project."`
	Content   string `json:"content" jsonschema:"Compact per-project overview content."`
}

type SetProjectOverviewOutput struct {
	Status string                `json:"status"`
	Record ProjectOverviewRecord `json:"record"`
}

type GetProjectOverviewInput struct {
	ProjectId int `json:"project_id,omitempty" jsonschema:"Optional project ID when called by overseer; fixer uses the bound project."`
}

type GetProjectOverviewOutput struct {
	ProjectId   int                   `json:"project_id"`
	HasOverview bool                  `json:"has_overview"`
	Overview    ProjectOverviewRecord `json:"overview"`
}

type ActiveProjectLatestSession struct {
	SessionId         int    `json:"session_id"`
	GlobalSessionId   int    `json:"global_session_id"`
	TaskDescription   string `json:"task_description"`
	Status            string `json:"status"`
	Report            string `json:"report,omitempty"`
	CliBackend        string `json:"cli_backend,omitempty"`
	CliModel          string `json:"cli_model,omitempty"`
	CliReasoning      string `json:"cli_reasoning,omitempty"`
	ExternalSessionId string `json:"external_session_id,omitempty"`
	CodexSessionId    string `json:"codex_session_id,omitempty"`
}

type ActiveProjectOverview struct {
	ProjectId      int                          `json:"project_id"`
	Name           string                       `json:"name"`
	Cwd            string                       `json:"cwd"`
	Activity       string                       `json:"activity"`
	Overview       ProjectOverviewRecord        `json:"overview"`
	HasOverview    bool                         `json:"has_overview"`
	Handoff        ProjectHandoffRecord         `json:"handoff"`
	HasHandoff     bool                         `json:"has_handoff"`
	LatestSessions []ActiveProjectLatestSession `json:"latest_sessions"`
}

type GetActiveProjectOverviewsInput struct{}

type GetActiveProjectOverviewsOutput struct {
	Projects []ActiveProjectOverview `json:"projects"`
}

func normalizeProjectActivity(raw string) (string, bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "active":
		return "active", true, nil
	case "passive":
		return "passive", false, nil
	default:
		return "", false, fmt.Errorf("activity must be active or passive")
	}
}

func fetchProjectActivityRecord(projectID int) (ProjectActivityRecord, error) {
	var activeInt int
	err := db.QueryRow("SELECT COALESCE(active, 0) FROM project WHERE id = ?", projectID).Scan(&activeInt)
	if err != nil {
		return ProjectActivityRecord{}, err
	}
	record := ProjectActivityRecord{ProjectId: projectID, Active: activeInt != 0}
	if record.Active {
		record.Activity = "active"
	} else {
		record.Activity = "passive"
	}
	return record, nil
}

func fetchProjectOverviewRecord(projectID int) (ProjectOverviewRecord, error) {
	var record ProjectOverviewRecord
	err := db.QueryRow(
		`SELECT project_id, content, updated_at
		 FROM project_overview
		 WHERE project_id = ?`,
		projectID,
	).Scan(&record.ProjectId, &record.Content, &record.UpdatedAt)
	if err != nil {
		return ProjectOverviewRecord{}, err
	}
	return record, nil
}

func SetProjectActivity(ctx context.Context, req *mcp.CallToolRequest, input SetProjectActivityInput) (*mcp.CallToolResult, SetProjectActivityOutput, error) {
	projectID, err := resolveProjectHandoffProjectID(input.ProjectId)
	if err != nil {
		if err == sql.ErrNoRows {
			return &mcp.CallToolResult{IsError: true}, SetProjectActivityOutput{}, fmt.Errorf("project not found")
		}
		return &mcp.CallToolResult{IsError: true}, SetProjectActivityOutput{}, err
	}

	_, active, err := normalizeProjectActivity(input.Activity)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetProjectActivityOutput{}, err
	}

	activeInt := 0
	if active {
		activeInt = 1
	}
	if _, err := db.Exec("UPDATE project SET active = ? WHERE id = ?", activeInt, projectID); err != nil {
		return &mcp.CallToolResult{IsError: true}, SetProjectActivityOutput{}, fmt.Errorf("DB update error: %v", err)
	}

	record, err := fetchProjectActivityRecord(projectID)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetProjectActivityOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	return nil, SetProjectActivityOutput{Status: "success", Record: record}, nil
}

func SetProjectOverview(ctx context.Context, req *mcp.CallToolRequest, input SetProjectOverviewInput) (*mcp.CallToolResult, SetProjectOverviewOutput, error) {
	projectID, err := resolveProjectHandoffProjectID(input.ProjectId)
	if err != nil {
		if err == sql.ErrNoRows {
			return &mcp.CallToolResult{IsError: true}, SetProjectOverviewOutput{}, fmt.Errorf("project not found")
		}
		return &mcp.CallToolResult{IsError: true}, SetProjectOverviewOutput{}, err
	}

	content := strings.TrimSpace(input.Content)
	if content == "" {
		return &mcp.CallToolResult{IsError: true}, SetProjectOverviewOutput{}, fmt.Errorf("content is required")
	}

	_, err = db.Exec(
		`INSERT INTO project_overview (project_id, content, updated_at)
		 VALUES (?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(project_id) DO UPDATE SET
		   content = excluded.content,
		   updated_at = CURRENT_TIMESTAMP`,
		projectID,
		content,
	)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetProjectOverviewOutput{}, fmt.Errorf("DB upsert error: %v", err)
	}

	record, err := fetchProjectOverviewRecord(projectID)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetProjectOverviewOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	return nil, SetProjectOverviewOutput{Status: "success", Record: record}, nil
}

func GetProjectOverview(ctx context.Context, req *mcp.CallToolRequest, input GetProjectOverviewInput) (*mcp.CallToolResult, GetProjectOverviewOutput, error) {
	projectID, err := resolveProjectHandoffProjectID(input.ProjectId)
	if err != nil {
		if err == sql.ErrNoRows {
			return &mcp.CallToolResult{IsError: true}, GetProjectOverviewOutput{}, fmt.Errorf("project not found")
		}
		return &mcp.CallToolResult{IsError: true}, GetProjectOverviewOutput{}, err
	}

	record, err := fetchProjectOverviewRecord(projectID)
	if err == sql.ErrNoRows {
		return nil, GetProjectOverviewOutput{
			ProjectId:   projectID,
			HasOverview: false,
			Overview:    ProjectOverviewRecord{ProjectId: projectID},
		}, nil
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetProjectOverviewOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	return nil, GetProjectOverviewOutput{
		ProjectId:   projectID,
		HasOverview: true,
		Overview:    record,
	}, nil
}

func fetchLatestProjectSessions(projectID int) ([]ActiveProjectLatestSession, error) {
	rows, err := db.Query(
		`SELECT
		    (
		      SELECT COUNT(*)
		      FROM session scoped
		      WHERE scoped.project_id = s.project_id AND scoped.id <= s.id
		    ) AS local_session_id,
		    s.id,
		    s.task_description,
		    s.status,
		    COALESCE(s.report, ''),
		    COALESCE(NULLIF(TRIM(s.cli_backend), ''), ?),
		    COALESCE(s.cli_model, ''),
		    COALESCE(s.cli_reasoning, ''),
		    COALESCE(external_link.external_session_id, ''),
		    COALESCE(codex_link.codex_session_id, '')
		 FROM session s
		 LEFT JOIN session_external_link external_link
		   ON external_link.session_id = s.id
		  AND external_link.backend = COALESCE(NULLIF(TRIM(s.cli_backend), ''), ?)
		 LEFT JOIN session_codex_link codex_link
		   ON codex_link.session_id = s.id
		 WHERE s.project_id = ?
		 ORDER BY s.id DESC
		 LIMIT 5`,
		defaultCliBackend,
		defaultCliBackend,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	sessions := []ActiveProjectLatestSession{}
	for rows.Next() {
		var item ActiveProjectLatestSession
		if err := rows.Scan(
			&item.SessionId,
			&item.GlobalSessionId,
			&item.TaskDescription,
			&item.Status,
			&item.Report,
			&item.CliBackend,
			&item.CliModel,
			&item.CliReasoning,
			&item.ExternalSessionId,
			&item.CodexSessionId,
		); err != nil {
			return nil, err
		}
		sessions = append(sessions, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return sessions, nil
}

func GetActiveProjectOverviews(ctx context.Context, req *mcp.CallToolRequest, input GetActiveProjectOverviewsInput) (*mcp.CallToolResult, GetActiveProjectOverviewsOutput, error) {
	if authorizedRole != "overseer" {
		return &mcp.CallToolResult{IsError: true}, GetActiveProjectOverviewsOutput{}, fmt.Errorf("access denied: requires overseer role")
	}

	rows, err := db.Query("SELECT id, name, cwd FROM project WHERE COALESCE(active, 0) != 0 ORDER BY id")
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetActiveProjectOverviewsOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	type activeProjectHeader struct {
		projectID int
		name      string
		cwd       string
	}

	headers := []activeProjectHeader{}
	for rows.Next() {
		var header activeProjectHeader
		if err := rows.Scan(&header.projectID, &header.name, &header.cwd); err != nil {
			_ = rows.Close()
			return &mcp.CallToolResult{IsError: true}, GetActiveProjectOverviewsOutput{}, fmt.Errorf("DB scan error: %v", err)
		}
		headers = append(headers, header)
	}
	if err := rows.Close(); err != nil {
		return &mcp.CallToolResult{IsError: true}, GetActiveProjectOverviewsOutput{}, fmt.Errorf("DB close error: %v", err)
	}
	if err := rows.Err(); err != nil {
		return &mcp.CallToolResult{IsError: true}, GetActiveProjectOverviewsOutput{}, fmt.Errorf("DB scan error: %v", err)
	}

	projects := []ActiveProjectOverview{}
	for _, header := range headers {
		item := ActiveProjectOverview{
			ProjectId: header.projectID,
			Name:      header.name,
			Cwd:       header.cwd,
			Activity:  "active",
		}

		overview, overviewErr := fetchProjectOverviewRecord(item.ProjectId)
		if overviewErr == nil {
			item.HasOverview = true
			item.Overview = overview
		} else if overviewErr == sql.ErrNoRows {
			item.Overview = ProjectOverviewRecord{ProjectId: item.ProjectId}
		} else {
			return &mcp.CallToolResult{IsError: true}, GetActiveProjectOverviewsOutput{}, fmt.Errorf("DB query error: %v", overviewErr)
		}

		handoff, handoffErr := fetchProjectHandoffRecord(item.ProjectId)
		if handoffErr == nil {
			item.HasHandoff = true
			item.Handoff = handoff
		} else if handoffErr == sql.ErrNoRows {
			item.Handoff = ProjectHandoffRecord{ProjectId: item.ProjectId}
		} else {
			return &mcp.CallToolResult{IsError: true}, GetActiveProjectOverviewsOutput{}, fmt.Errorf("DB query error: %v", handoffErr)
		}

		sessions, sessionErr := fetchLatestProjectSessions(item.ProjectId)
		if sessionErr != nil {
			return &mcp.CallToolResult{IsError: true}, GetActiveProjectOverviewsOutput{}, fmt.Errorf("DB query error: %v", sessionErr)
		}
		item.LatestSessions = sessions
		projects = append(projects, item)
	}

	return nil, GetActiveProjectOverviewsOutput{Projects: projects}, nil
}

func fetchProjectHandoffRecord(projectID int) (ProjectHandoffRecord, error) {
	var record ProjectHandoffRecord
	err := db.QueryRow(
		`SELECT project_id, content, updated_at
		 FROM project_handoff
		 WHERE project_id = ?`,
		projectID,
	).Scan(&record.ProjectId, &record.Content, &record.UpdatedAt)
	if err != nil {
		return ProjectHandoffRecord{}, err
	}
	return record, nil
}

func SetProjectHandoff(ctx context.Context, req *mcp.CallToolRequest, input SetProjectHandoffInput) (*mcp.CallToolResult, SetProjectHandoffOutput, error) {
	projectID, err := resolveProjectHandoffProjectID(input.ProjectId)
	if err != nil {
		if err == sql.ErrNoRows {
			return &mcp.CallToolResult{IsError: true}, SetProjectHandoffOutput{}, fmt.Errorf("project not found")
		}
		return &mcp.CallToolResult{IsError: true}, SetProjectHandoffOutput{}, err
	}

	content := strings.TrimSpace(input.Content)
	if content == "" {
		return &mcp.CallToolResult{IsError: true}, SetProjectHandoffOutput{}, fmt.Errorf("content is required")
	}

	_, err = db.Exec(
		`INSERT INTO project_handoff (project_id, content, updated_at)
		 VALUES (?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(project_id) DO UPDATE SET
		   content = excluded.content,
		   updated_at = CURRENT_TIMESTAMP`,
		projectID,
		content,
	)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetProjectHandoffOutput{}, fmt.Errorf("DB upsert error: %v", err)
	}

	record, err := fetchProjectHandoffRecord(projectID)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetProjectHandoffOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	return nil, SetProjectHandoffOutput{Status: "success", Record: record}, nil
}

func GetProjectHandoff(ctx context.Context, req *mcp.CallToolRequest, input GetProjectHandoffInput) (*mcp.CallToolResult, GetProjectHandoffOutput, error) {
	projectID, err := resolveProjectHandoffProjectID(input.ProjectId)
	if err != nil {
		if err == sql.ErrNoRows {
			return &mcp.CallToolResult{IsError: true}, GetProjectHandoffOutput{}, fmt.Errorf("project not found")
		}
		return &mcp.CallToolResult{IsError: true}, GetProjectHandoffOutput{}, err
	}

	record, err := fetchProjectHandoffRecord(projectID)
	if err == sql.ErrNoRows {
		return nil, GetProjectHandoffOutput{
			ProjectId:  projectID,
			HasHandoff: false,
			Handoff:    ProjectHandoffRecord{ProjectId: projectID},
		}, nil
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetProjectHandoffOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	return nil, GetProjectHandoffOutput{
		ProjectId:  projectID,
		HasHandoff: true,
		Handoff:    record,
	}, nil
}

func ClearProjectHandoff(ctx context.Context, req *mcp.CallToolRequest, input ClearProjectHandoffInput) (*mcp.CallToolResult, ClearProjectHandoffOutput, error) {
	projectID, err := resolveProjectHandoffProjectID(input.ProjectId)
	if err != nil {
		if err == sql.ErrNoRows {
			return &mcp.CallToolResult{IsError: true}, ClearProjectHandoffOutput{}, fmt.Errorf("project not found")
		}
		return &mcp.CallToolResult{IsError: true}, ClearProjectHandoffOutput{}, err
	}

	if _, err := db.Exec("DELETE FROM project_handoff WHERE project_id = ?", projectID); err != nil {
		return &mcp.CallToolResult{IsError: true}, ClearProjectHandoffOutput{}, fmt.Errorf("DB delete error: %v", err)
	}

	return nil, ClearProjectHandoffOutput{Status: "success", ProjectId: projectID}, nil
}

func normalizeOverseerFixerSenderRole(raw string) (string, error) {
	role := strings.ToLower(strings.TrimSpace(raw))
	switch role {
	case "overseer", "fixer":
		return role, nil
	case "":
		return "", fmt.Errorf("sender_role is required")
	default:
		return "", fmt.Errorf("sender_role must be overseer or fixer")
	}
}

func normalizeOverseerFixerLimit(limit int) int {
	if limit <= 0 {
		return 10
	}
	if limit > 100 {
		return 100
	}
	return limit
}

func fetchOverseerFixerMessageByID(messageID int) (OverseerFixerMessageRecord, error) {
	var record OverseerFixerMessageRecord
	err := db.QueryRow(
		`SELECT id, project_id, sender_role, content, created_at
		 FROM overseer_fixer_message
		 WHERE id = ?`,
		messageID,
	).Scan(&record.Id, &record.ProjectId, &record.SenderRole, &record.Content, &record.CreatedAt)
	if err != nil {
		return OverseerFixerMessageRecord{}, err
	}
	return record, nil
}

func fetchOverseerFixerMessages(projectID int, limit int) ([]OverseerFixerMessageRecord, error) {
	rows, err := db.Query(
		`SELECT id, project_id, sender_role, content, created_at
		 FROM (
		   SELECT id, project_id, sender_role, content, created_at
		   FROM overseer_fixer_message
		   WHERE project_id = ?
		   ORDER BY id DESC
		   LIMIT ?
		 )
		 ORDER BY id ASC`,
		projectID,
		normalizeOverseerFixerLimit(limit),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	messages := []OverseerFixerMessageRecord{}
	for rows.Next() {
		var message OverseerFixerMessageRecord
		if err := rows.Scan(&message.Id, &message.ProjectId, &message.SenderRole, &message.Content, &message.CreatedAt); err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return messages, nil
}

func fetchOverseerFixerRunStateRecord(projectID int) (OverseerFixerRunStateRecord, error) {
	var record OverseerFixerRunStateRecord
	var activeInt int
	err := db.QueryRow(
		`SELECT project_id,
		        COALESCE(active, 0),
		        COALESCE(status, ''),
		        COALESCE(reason, ''),
		        COALESCE(last_message_id, 0),
		        updated_at
		 FROM overseer_fixer_run_state
		 WHERE project_id = ?`,
		projectID,
	).Scan(&record.ProjectId, &activeInt, &record.Status, &record.Reason, &record.LastMessageId, &record.UpdatedAt)
	if err != nil {
		return OverseerFixerRunStateRecord{}, err
	}
	record.Active = activeInt != 0
	return record, nil
}

func latestOverseerFixerMessageID(projectID int) (int, error) {
	var messageID int
	err := db.QueryRow(
		`SELECT COALESCE(MAX(id), 0)
		 FROM overseer_fixer_message
		 WHERE project_id = ?`,
		projectID,
	).Scan(&messageID)
	return messageID, err
}

func AppendOverseerFixerMessage(ctx context.Context, req *mcp.CallToolRequest, input AppendOverseerFixerMessageInput) (*mcp.CallToolResult, AppendOverseerFixerMessageOutput, error) {
	if authorizedRole != "fixer" && authorizedRole != "overseer" {
		return &mcp.CallToolResult{IsError: true}, AppendOverseerFixerMessageOutput{}, fmt.Errorf("access denied: requires fixer or overseer role")
	}

	projectID, err := resolveProjectHandoffProjectID(input.ProjectId)
	if err != nil {
		if err == sql.ErrNoRows {
			return &mcp.CallToolResult{IsError: true}, AppendOverseerFixerMessageOutput{}, fmt.Errorf("project not found")
		}
		return &mcp.CallToolResult{IsError: true}, AppendOverseerFixerMessageOutput{}, err
	}

	senderRole, err := normalizeOverseerFixerSenderRole(input.SenderRole)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, AppendOverseerFixerMessageOutput{}, err
	}
	if senderRole != authorizedRole {
		return &mcp.CallToolResult{IsError: true}, AppendOverseerFixerMessageOutput{}, fmt.Errorf("sender_role must match authenticated role")
	}

	content := strings.TrimSpace(input.Content)
	if content == "" {
		return &mcp.CallToolResult{IsError: true}, AppendOverseerFixerMessageOutput{}, fmt.Errorf("content is required")
	}

	result, err := db.Exec(
		`INSERT INTO overseer_fixer_message (project_id, sender_role, content)
		 VALUES (?, ?, ?)`,
		projectID,
		senderRole,
		content,
	)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, AppendOverseerFixerMessageOutput{}, fmt.Errorf("DB insert error: %v", err)
	}
	messageID, err := result.LastInsertId()
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, AppendOverseerFixerMessageOutput{}, fmt.Errorf("LastInsertId error: %v", err)
	}
	message, err := fetchOverseerFixerMessageByID(int(messageID))
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, AppendOverseerFixerMessageOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	return nil, AppendOverseerFixerMessageOutput{Status: "success", Message: message}, nil
}

func GetOverseerFixerMessages(ctx context.Context, req *mcp.CallToolRequest, input GetOverseerFixerMessagesInput) (*mcp.CallToolResult, GetOverseerFixerMessagesOutput, error) {
	if authorizedRole != "fixer" && authorizedRole != "overseer" {
		return &mcp.CallToolResult{IsError: true}, GetOverseerFixerMessagesOutput{}, fmt.Errorf("access denied: requires fixer or overseer role")
	}

	projectID, err := resolveProjectHandoffProjectID(input.ProjectId)
	if err != nil {
		if err == sql.ErrNoRows {
			return &mcp.CallToolResult{IsError: true}, GetOverseerFixerMessagesOutput{}, fmt.Errorf("project not found")
		}
		return &mcp.CallToolResult{IsError: true}, GetOverseerFixerMessagesOutput{}, err
	}

	messages, err := fetchOverseerFixerMessages(projectID, input.Limit)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetOverseerFixerMessagesOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	return nil, GetOverseerFixerMessagesOutput{ProjectId: projectID, Messages: messages}, nil
}

func SetOverseerFixerRunState(ctx context.Context, req *mcp.CallToolRequest, input SetOverseerFixerRunStateInput) (*mcp.CallToolResult, SetOverseerFixerRunStateOutput, error) {
	if authorizedRole != "fixer" && authorizedRole != "overseer" {
		return &mcp.CallToolResult{IsError: true}, SetOverseerFixerRunStateOutput{}, fmt.Errorf("access denied: requires fixer or overseer role")
	}

	projectID, err := resolveProjectHandoffProjectID(input.ProjectId)
	if err != nil {
		if err == sql.ErrNoRows {
			return &mcp.CallToolResult{IsError: true}, SetOverseerFixerRunStateOutput{}, fmt.Errorf("project not found")
		}
		return &mcp.CallToolResult{IsError: true}, SetOverseerFixerRunStateOutput{}, err
	}

	lastMessageID := input.LastMessageId
	if lastMessageID < 0 {
		return &mcp.CallToolResult{IsError: true}, SetOverseerFixerRunStateOutput{}, fmt.Errorf("last_message_id must be non-negative")
	}
	if lastMessageID == 0 {
		lastMessageID, err = latestOverseerFixerMessageID(projectID)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, SetOverseerFixerRunStateOutput{}, fmt.Errorf("DB query error: %v", err)
		}
	}

	statusText := truncateRunes(normalizeCompactText(input.Status), 160)
	reason := truncateRunes(normalizeCompactText(input.Reason), 280)

	_, err = db.Exec(
		`INSERT INTO overseer_fixer_run_state (project_id, active, status, reason, last_message_id, updated_at)
		 VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(project_id) DO UPDATE SET
		   active = excluded.active,
		   status = excluded.status,
		   reason = excluded.reason,
		   last_message_id = excluded.last_message_id,
		   updated_at = CURRENT_TIMESTAMP`,
		projectID,
		boolToInt(input.Active),
		statusText,
		reason,
		lastMessageID,
	)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetOverseerFixerRunStateOutput{}, fmt.Errorf("DB upsert error: %v", err)
	}

	record, err := fetchOverseerFixerRunStateRecord(projectID)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetOverseerFixerRunStateOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	return nil, SetOverseerFixerRunStateOutput{Status: "success", State: record}, nil
}

func GetOverseerFixerRunState(ctx context.Context, req *mcp.CallToolRequest, input GetOverseerFixerRunStateInput) (*mcp.CallToolResult, GetOverseerFixerRunStateOutput, error) {
	if authorizedRole != "fixer" && authorizedRole != "overseer" {
		return &mcp.CallToolResult{IsError: true}, GetOverseerFixerRunStateOutput{}, fmt.Errorf("access denied: requires fixer or overseer role")
	}

	projectID, err := resolveProjectHandoffProjectID(input.ProjectId)
	if err != nil {
		if err == sql.ErrNoRows {
			return &mcp.CallToolResult{IsError: true}, GetOverseerFixerRunStateOutput{}, fmt.Errorf("project not found")
		}
		return &mcp.CallToolResult{IsError: true}, GetOverseerFixerRunStateOutput{}, err
	}

	record, err := fetchOverseerFixerRunStateRecord(projectID)
	if err == sql.ErrNoRows {
		return nil, GetOverseerFixerRunStateOutput{
			ProjectId: projectID,
			HasState:  false,
			State:     OverseerFixerRunStateRecord{ProjectId: projectID},
		}, nil
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetOverseerFixerRunStateOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	return nil, GetOverseerFixerRunStateOutput{ProjectId: projectID, HasState: true, State: record}, nil
}

func activeOverseerFixerRunProjectIDs(projectIDs []int) ([]int, error) {
	query := `SELECT project_id
	          FROM overseer_fixer_run_state
	          WHERE active != 0`
	args := []any{}
	if len(projectIDs) > 0 {
		placeholders := make([]string, 0, len(projectIDs))
		for _, projectID := range projectIDs {
			if projectID <= 0 {
				return nil, fmt.Errorf("project_ids must contain positive project IDs")
			}
			exists, err := projectExists(projectID)
			if err != nil {
				return nil, err
			}
			if !exists {
				return nil, fmt.Errorf("project not found: %d", projectID)
			}
			placeholders = append(placeholders, "?")
			args = append(args, projectID)
		}
		query += " AND project_id IN (" + strings.Join(placeholders, ",") + ")"
	}
	query += " ORDER BY project_id"

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	activeProjectIDs := []int{}
	for rows.Next() {
		var projectID int
		if err := rows.Scan(&projectID); err != nil {
			return nil, err
		}
		activeProjectIDs = append(activeProjectIDs, projectID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return activeProjectIDs, nil
}

func fetchNewFixerMessages(projectIDs []int, afterMessageID int) ([]OverseerFixerMessageRecord, error) {
	if len(projectIDs) == 0 {
		return []OverseerFixerMessageRecord{}, nil
	}
	if afterMessageID < 0 {
		return nil, fmt.Errorf("after_message_id must be non-negative")
	}

	placeholders := make([]string, 0, len(projectIDs))
	args := []any{afterMessageID, "fixer"}
	for _, projectID := range projectIDs {
		placeholders = append(placeholders, "?")
		args = append(args, projectID)
	}

	rows, err := db.Query(
		`SELECT id, project_id, sender_role, content, created_at
		 FROM overseer_fixer_message
		 WHERE id > ?
		   AND sender_role = ?
		   AND project_id IN (`+strings.Join(placeholders, ",")+`)
		 ORDER BY id ASC`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	messages := []OverseerFixerMessageRecord{}
	for rows.Next() {
		var message OverseerFixerMessageRecord
		if err := rows.Scan(&message.Id, &message.ProjectId, &message.SenderRole, &message.Content, &message.CreatedAt); err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return messages, nil
}

func normalizeWaitTimeout(timeoutMs int) time.Duration {
	if timeoutMs <= 0 {
		return time.Second
	}
	if timeoutMs > 30000 {
		timeoutMs = 30000
	}
	return time.Duration(timeoutMs) * time.Millisecond
}

func normalizeWaitPollInterval(pollIntervalMs int, timeout time.Duration) time.Duration {
	if pollIntervalMs <= 0 {
		pollIntervalMs = 50
	}
	if pollIntervalMs > 1000 {
		pollIntervalMs = 1000
	}
	interval := time.Duration(pollIntervalMs) * time.Millisecond
	if timeout > 0 && interval > timeout {
		return timeout
	}
	return interval
}

func cursorFromMessages(messages []OverseerFixerMessageRecord, fallback int) int {
	cursor := fallback
	for _, message := range messages {
		if message.Id > cursor {
			cursor = message.Id
		}
	}
	return cursor
}

func WaitForOverseerFixerMessages(ctx context.Context, req *mcp.CallToolRequest, input WaitForOverseerFixerMessagesInput) (*mcp.CallToolResult, WaitForOverseerFixerMessagesOutput, error) {
	if authorizedRole != "overseer" {
		return &mcp.CallToolResult{IsError: true}, WaitForOverseerFixerMessagesOutput{}, fmt.Errorf("access denied: requires overseer role")
	}
	if input.AfterMessageId < 0 {
		return &mcp.CallToolResult{IsError: true}, WaitForOverseerFixerMessagesOutput{}, fmt.Errorf("after_message_id must be non-negative")
	}

	activeProjectIDs, err := activeOverseerFixerRunProjectIDs(input.ProjectIds)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, WaitForOverseerFixerMessagesOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	if len(activeProjectIDs) == 0 {
		return nil, WaitForOverseerFixerMessagesOutput{
			Status:          "no_active_fixers",
			ProjectIds:      []int{},
			AfterMessageId:  input.AfterMessageId,
			CursorMessageId: input.AfterMessageId,
			Messages:        []OverseerFixerMessageRecord{},
		}, nil
	}

	timeout := normalizeWaitTimeout(input.TimeoutMs)
	pollInterval := normalizeWaitPollInterval(input.PollIntervalMs, timeout)
	deadline := time.Now().Add(timeout)

	for {
		messages, err := fetchNewFixerMessages(activeProjectIDs, input.AfterMessageId)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, WaitForOverseerFixerMessagesOutput{}, fmt.Errorf("DB query error: %v", err)
		}
		if len(messages) > 0 {
			return nil, WaitForOverseerFixerMessagesOutput{
				Status:          "messages",
				ProjectIds:      activeProjectIDs,
				AfterMessageId:  input.AfterMessageId,
				CursorMessageId: cursorFromMessages(messages, input.AfterMessageId),
				Messages:        messages,
			}, nil
		}

		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, WaitForOverseerFixerMessagesOutput{
				Status:          "timeout",
				TimedOut:        true,
				ProjectIds:      activeProjectIDs,
				AfterMessageId:  input.AfterMessageId,
				CursorMessageId: input.AfterMessageId,
				Messages:        []OverseerFixerMessageRecord{},
			}, nil
		}
		sleepFor := pollInterval
		if remaining < sleepFor {
			sleepFor = remaining
		}

		select {
		case <-ctx.Done():
			return &mcp.CallToolResult{IsError: true}, WaitForOverseerFixerMessagesOutput{}, ctx.Err()
		case <-time.After(sleepFor):
		}
	}
}
