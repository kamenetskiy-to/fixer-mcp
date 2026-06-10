package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	explicitLauncherExitGracePeriod = 15 * time.Second
	netrunnerStartupFailureWindow   = 2 * time.Minute
)

type explicitLaunchWorkerMetadata struct {
	WorkerPID       int    `json:"worker_pid"`
	HeadlessLogPath string `json:"headless_log_path"`
	Backend         string `json:"backend"`
	SessionID       int    `json:"session_id"`
}

func explicitWaitTimeoutSeconds(raw int) (int, error) {
	if raw <= 0 {
		return explicitLaunchDefaultWait, nil
	}
	if raw > explicitLaunchMaxWait {
		return 0, fmt.Errorf("timeout_seconds must be <= %d", explicitLaunchMaxWait)
	}
	return raw, nil
}

func explicitWaitPollIntervalSeconds(raw int) (int, error) {
	if raw <= 0 {
		return explicitLaunchDefaultPoll, nil
	}
	if raw > explicitLaunchMaxPoll {
		return 0, fmt.Errorf("poll_interval_seconds must be <= %d", explicitLaunchMaxPoll)
	}
	return raw, nil
}

func resolveExplicitLauncherScript() (string, error) {
	executablePath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve executable path: %w", err)
	}
	launcherScript := filepath.Clean(filepath.Join(filepath.Dir(executablePath), "..", "client_wires", "fixer_autonomous.py"))
	if _, statErr := os.Stat(launcherScript); statErr == nil {
		return launcherScript, nil
	}

	workingDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve working directory: %w", err)
	}
	launcherScript = filepath.Clean(filepath.Join(workingDir, "..", "client_wires", "fixer_autonomous.py"))
	if _, statErr := os.Stat(launcherScript); statErr != nil {
		return "", fmt.Errorf("explicit launcher script unavailable: %v", statErr)
	}
	return launcherScript, nil
}

type sessionLaunchConfig struct {
	Backend           string
	Model             string
	Reasoning         string
	Started           bool
	ExternalSessionID string
}

func readSessionLaunchConfig(sessionID int, projectID int) (sessionLaunchConfig, error) {
	var (
		storedBackend   string
		storedModel     string
		storedReasoning string
	)

	err := db.QueryRow(
		`SELECT COALESCE(NULLIF(TRIM(cli_backend), ''), ?),
		        COALESCE(cli_model, ''),
		        COALESCE(cli_reasoning, '')
		 FROM session
		 WHERE id = ? AND project_id = ?`,
		defaultCliBackend,
		sessionID,
		projectID,
	).Scan(&storedBackend, &storedModel, &storedReasoning)
	if err != nil {
		return sessionLaunchConfig{}, err
	}

	storedBackend, err = normalizeCliBackend(storedBackend)
	if err != nil {
		return sessionLaunchConfig{}, err
	}
	storedModel = normalizeCliModel(storedBackend, storedModel)
	storedReasoning = normalizeCliReasoning(storedBackend, storedReasoning)
	externalSessionID, err := fetchSessionExternalID(sessionID, storedBackend)
	if err != nil {
		return sessionLaunchConfig{}, err
	}
	return sessionLaunchConfig{
		Backend:           storedBackend,
		Model:             strings.TrimSpace(storedModel),
		Reasoning:         storedReasoning,
		Started:           strings.TrimSpace(externalSessionID) != "",
		ExternalSessionID: externalSessionID,
	}, nil
}

func resolveSessionLaunchConfig(sessionID int, projectID int, requestedBackend string, requestedModel string, requestedReasoning string) (sessionLaunchConfig, error) {
	currentConfig, err := readSessionLaunchConfig(sessionID, projectID)
	if err != nil {
		return sessionLaunchConfig{}, err
	}
	resolvedBackend := currentConfig.Backend
	if strings.TrimSpace(requestedBackend) != "" {
		resolvedBackend, err = normalizeCliBackend(requestedBackend)
		if err != nil {
			return sessionLaunchConfig{}, err
		}
	}

	trimmedRequestedModel := normalizeCliModel(resolvedBackend, requestedModel)
	trimmedRequestedReasoning := normalizeCliReasoning(resolvedBackend, requestedReasoning)
	if err := validateCliReasoningForBackend(resolvedBackend, trimmedRequestedReasoning); err != nil {
		return sessionLaunchConfig{}, err
	}
	if currentConfig.Started && resolvedBackend != currentConfig.Backend {
		return sessionLaunchConfig{}, fmt.Errorf("session is bound to backend %q and cannot switch to %q after launch", currentConfig.Backend, resolvedBackend)
	}
	if currentConfig.Started && currentConfig.Model != "" && trimmedRequestedModel != "" && trimmedRequestedModel != currentConfig.Model {
		return sessionLaunchConfig{}, fmt.Errorf("session is bound to model %q and cannot switch to %q after launch", currentConfig.Model, trimmedRequestedModel)
	}
	if currentConfig.Started && currentConfig.Reasoning != "" && trimmedRequestedReasoning != "" && trimmedRequestedReasoning != currentConfig.Reasoning {
		return sessionLaunchConfig{}, fmt.Errorf("session is bound to reasoning %q and cannot switch to %q after launch", currentConfig.Reasoning, trimmedRequestedReasoning)
	}

	finalBackend := resolvedBackend
	if currentConfig.Started {
		finalBackend = currentConfig.Backend
	}

	finalModel := currentConfig.Model
	if !currentConfig.Started && trimmedRequestedModel != "" {
		finalModel = trimmedRequestedModel
	}
	if finalModel == "" {
		finalModel = defaultCliModelForBackend(finalBackend)
	}
	if err := validateCliModelForBackend(finalBackend, finalModel); err != nil {
		return sessionLaunchConfig{}, err
	}

	finalReasoning := currentConfig.Reasoning
	if !currentConfig.Started && finalBackend != currentConfig.Backend {
		finalReasoning = ""
	}
	if !currentConfig.Started && trimmedRequestedReasoning != "" {
		finalReasoning = trimmedRequestedReasoning
	}
	if finalReasoning == "" {
		finalReasoning = defaultCliReasoningForBackend(finalBackend)
	}
	if err := validateCliReasoningForBackend(finalBackend, finalReasoning); err != nil {
		return sessionLaunchConfig{}, err
	}

	if _, err := db.Exec(
		`UPDATE session
		 SET cli_backend = ?, cli_model = ?, cli_reasoning = ?
		 WHERE id = ? AND project_id = ?`,
		finalBackend,
		finalModel,
		finalReasoning,
		sessionID,
		projectID,
	); err != nil {
		return sessionLaunchConfig{}, err
	}

	return sessionLaunchConfig{
		Backend:           finalBackend,
		Model:             finalModel,
		Reasoning:         finalReasoning,
		Started:           currentConfig.Started,
		ExternalSessionID: currentConfig.ExternalSessionID,
	}, nil
}

func waitForSessionExternalID(ctx context.Context, sessionID int, backend string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		externalSessionID, err := fetchSessionExternalID(sessionID, backend)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(externalSessionID) != "" {
			return externalSessionID, nil
		}
		if time.Now().After(deadline) {
			return "", nil
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func explicitLaunchArtifacts(projectCWD string, sessionID int, backend string) (string, string, string, error) {
	logDir := filepath.Join(projectCWD, ".codex", "headless_netrunner_logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return "", "", "", fmt.Errorf("failed to prepare explicit launch log dir: %v", err)
	}
	suffix := strconv.FormatInt(time.Now().Unix(), 10)
	headlessLogPath := filepath.Join(logDir, fmt.Sprintf("session-%d-%s-%s.log", sessionID, backend, suffix))
	launcherLogPath := filepath.Join(logDir, fmt.Sprintf("session-%d-launcher-%s.log", sessionID, suffix))
	metadataPath := filepath.Join(logDir, fmt.Sprintf("session-%d-launcher-%s.json", sessionID, suffix))
	return headlessLogPath, launcherLogPath, metadataPath, nil
}

func readExplicitLaunchWorkerMetadata(metadataPath string) (explicitLaunchWorkerMetadata, error) {
	payload, err := os.ReadFile(metadataPath)
	if err != nil {
		return explicitLaunchWorkerMetadata{}, err
	}
	metadata := explicitLaunchWorkerMetadata{}
	if err := json.Unmarshal(payload, &metadata); err != nil {
		return explicitLaunchWorkerMetadata{}, err
	}
	if metadata.WorkerPID <= 0 {
		return explicitLaunchWorkerMetadata{}, fmt.Errorf("worker_pid missing from %s", metadataPath)
	}
	return metadata, nil
}

type ListActiveWorkerProcessesInput struct {
	SessionIds []int `json:"session_ids,omitempty" jsonschema:"Optional project-scoped session IDs to filter the active worker process listing."`
}

type ListActiveWorkerProcessesOutput struct {
	Status    string                  `json:"status"`
	ProjectId int                     `json:"project_id"`
	Processes []workerProcessSnapshot `json:"processes"`
}

func ListActiveWorkerProcesses(ctx context.Context, req *mcp.CallToolRequest, input ListActiveWorkerProcessesInput) (*mcp.CallToolResult, ListActiveWorkerProcessesOutput, error) {
	if authorizedRole != "fixer" {
		return &mcp.CallToolResult{IsError: true}, ListActiveWorkerProcessesOutput{}, fmt.Errorf("access denied: requires fixer role")
	}

	globalSessionIDs := make([]int, 0, len(input.SessionIds))
	for _, localSessionID := range input.SessionIds {
		globalSessionID, err := globalSessionIDFromProjectScoped(localSessionID, authorizedProjectId)
		if err == sql.ErrNoRows {
			return &mcp.CallToolResult{IsError: true}, ListActiveWorkerProcessesOutput{}, fmt.Errorf("session %d not found in current project", localSessionID)
		}
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, ListActiveWorkerProcessesOutput{}, fmt.Errorf("DB query error: %v", err)
		}
		globalSessionIDs = append(globalSessionIDs, globalSessionID)
	}

	processes, err := listRunningWorkerProcesses(authorizedProjectId, globalSessionIDs)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, ListActiveWorkerProcessesOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	for i := range processes {
		localSessionID, err := projectScopedSessionIDFromGlobal(processes[i].SessionID, authorizedProjectId)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, ListActiveWorkerProcessesOutput{}, fmt.Errorf("DB mapping error: %v", err)
		}
		processes[i].SessionID = localSessionID
	}
	if processes == nil {
		processes = []workerProcessSnapshot{}
	}

	return nil, ListActiveWorkerProcessesOutput{
		Status:    "success",
		ProjectId: authorizedProjectId,
		Processes: processes,
	}, nil
}

type StopActiveWorkerProcessesInput struct {
	SessionIds          []int  `json:"session_ids,omitempty" jsonschema:"Optional project-scoped session IDs to stop. When omitted, all active worker processes in the current project are targeted."`
	FreezeOrchestration bool   `json:"freeze_orchestration,omitempty" jsonschema:"When true, increment the orchestration epoch and freeze follow-up automation until an explicit resume."`
	Reason              string `json:"reason,omitempty" jsonschema:"Optional operator-facing reason for the stop request."`
}

type StopActiveWorkerProcessesOutput struct {
	Status              string `json:"status"`
	ProjectId           int    `json:"project_id"`
	StoppedProcessCount int    `json:"stopped_process_count"`
	StoppedSessionIds   []int  `json:"stopped_session_ids"`
	FreezeApplied       bool   `json:"freeze_applied"`
	OrchestrationEpoch  int    `json:"orchestration_epoch"`
}

func StopActiveWorkerProcesses(ctx context.Context, req *mcp.CallToolRequest, input StopActiveWorkerProcessesInput) (*mcp.CallToolResult, StopActiveWorkerProcessesOutput, error) {
	if authorizedRole != "fixer" {
		return &mcp.CallToolResult{IsError: true}, StopActiveWorkerProcessesOutput{}, fmt.Errorf("access denied: requires fixer role")
	}

	globalSessionIDs := make([]int, 0, len(input.SessionIds))
	for _, localSessionID := range input.SessionIds {
		globalSessionID, err := globalSessionIDFromProjectScoped(localSessionID, authorizedProjectId)
		if err == sql.ErrNoRows {
			return &mcp.CallToolResult{IsError: true}, StopActiveWorkerProcessesOutput{}, fmt.Errorf("session %d not found in current project", localSessionID)
		}
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, StopActiveWorkerProcessesOutput{}, fmt.Errorf("DB query error: %v", err)
		}
		globalSessionIDs = append(globalSessionIDs, globalSessionID)
	}

	processes, err := listRunningWorkerProcesses(authorizedProjectId, globalSessionIDs)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, StopActiveWorkerProcessesOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	reason := strings.TrimSpace(input.Reason)
	if reason == "" {
		reason = "operator stop requested"
	}

	stoppedSessions := make(map[int]struct{})
	for _, processRow := range processes {
		status := workerStatusStopped
		if processRow.Alive && runtime.GOOS != "windows" {
			targetProcess, findErr := os.FindProcess(processRow.PID)
			if findErr == nil {
				signalErr := targetProcess.Signal(syscall.SIGTERM)
				if signalErr != nil && !errors.Is(signalErr, os.ErrProcessDone) {
					return &mcp.CallToolResult{IsError: true}, StopActiveWorkerProcessesOutput{}, fmt.Errorf("failed to stop worker pid %d: %v", processRow.PID, signalErr)
				}
				time.Sleep(250 * time.Millisecond)
				if isProcessAlive(processRow.PID) {
					if killErr := targetProcess.Signal(syscall.SIGKILL); killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
						return &mcp.CallToolResult{IsError: true}, StopActiveWorkerProcessesOutput{}, fmt.Errorf("failed to force-stop worker pid %d: %v", processRow.PID, killErr)
					}
					time.Sleep(250 * time.Millisecond)
				}
			}
		}
		if !isProcessAlive(processRow.PID) {
			status = workerStatusExited
		}
		if _, err := db.Exec(
			`UPDATE worker_process
			 SET status = ?, stop_reason = ?, stopped_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
			 WHERE id = ? AND project_id = ?`,
			status,
			reason,
			processRow.ID,
			authorizedProjectId,
		); err != nil {
			return &mcp.CallToolResult{IsError: true}, StopActiveWorkerProcessesOutput{}, fmt.Errorf("DB update error: %v", err)
		}
		stoppedSessions[processRow.SessionID] = struct{}{}
	}

	for sessionID := range stoppedSessions {
		if _, err := db.Exec("UPDATE session SET forced_stop_count = COALESCE(forced_stop_count, 0) + 1 WHERE id = ? AND project_id = ?", sessionID, authorizedProjectId); err != nil {
			return &mcp.CallToolResult{IsError: true}, StopActiveWorkerProcessesOutput{}, fmt.Errorf("DB update error: %v", err)
		}
	}

	control, exists, err := fetchOrchestrationControl(authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, StopActiveWorkerProcessesOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	if !exists {
		control.ProjectID = authorizedProjectId
		control.NotificationsEnabledForActiveRun = true
	}
	if input.FreezeOrchestration {
		control.OrchestrationEpoch++
		control.OrchestrationFrozen = true
		control.NotificationsEnabledForActiveRun = false
		control.State = "blocked"
		control.Summary = "Orchestration frozen by stop_active_worker_processes"
		control.Blocker = reason
		if err := upsertOrchestrationControl(
			authorizedProjectId,
			0,
			control.State,
			control.Summary,
			control.Focus,
			control.Blocker,
			control.Evidence,
			control.OrchestrationEpoch,
			control.OrchestrationFrozen,
			control.NotificationsEnabledForActiveRun,
		); err != nil {
			return &mcp.CallToolResult{IsError: true}, StopActiveWorkerProcessesOutput{}, fmt.Errorf("DB upsert error: %v", err)
		}
	}

	stoppedSessionIDs := make([]int, 0, len(stoppedSessions))
	for globalSessionID := range stoppedSessions {
		localSessionID, err := projectScopedSessionIDFromGlobal(globalSessionID, authorizedProjectId)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, StopActiveWorkerProcessesOutput{}, fmt.Errorf("DB mapping error: %v", err)
		}
		stoppedSessionIDs = append(stoppedSessionIDs, localSessionID)
	}
	sort.Ints(stoppedSessionIDs)

	return nil, StopActiveWorkerProcessesOutput{
		Status:              "success",
		ProjectId:           authorizedProjectId,
		StoppedProcessCount: len(processes),
		StoppedSessionIds:   stoppedSessionIDs,
		FreezeApplied:       input.FreezeOrchestration,
		OrchestrationEpoch:  control.OrchestrationEpoch,
	}, nil
}

type ExplicitNetrunnerLaunchMetadata struct {
	SessionId          int      `json:"session_id"`
	ProjectCwd         string   `json:"project_cwd"`
	LauncherScript     string   `json:"launcher_script"`
	Backend            string   `json:"backend"`
	Model              string   `json:"model,omitempty"`
	Reasoning          string   `json:"reasoning,omitempty"`
	ExternalSessionId  string   `json:"external_session_id,omitempty"`
	CodexSessionId     string   `json:"codex_session_id,omitempty"`
	SpawnedBackground  bool     `json:"spawned_background"`
	DeclaredWriteScope []string `json:"declared_write_scope"`
}

type LaunchExplicitNetrunnerInput struct {
	SessionId                  int    `json:"session_id" jsonschema:"Project-scoped session ID to launch over the explicit MCP-mounted wire path."`
	FixerSessionId             string `json:"fixer_session_id,omitempty" jsonschema:"Optional current Fixer Codex session ID to pass into the explicit wire runtime."`
	SessionReuseOverrideReason string `json:"session_reuse_override_reason,omitempty" jsonschema:"Optional explicit reason to relaunch a session that should normally be replaced by a repair fork after repeated rework or a forced stop."`
	Backend                    string `json:"backend,omitempty" jsonschema:"Optional CLI backend to launch for this session. Supported: codex, droid, antigravity (agy alias)."`
	Model                      string `json:"model,omitempty" jsonschema:"Optional backend-specific model selection to persist for this session."`
	Reasoning                  string `json:"reasoning,omitempty" jsonschema:"Optional backend-specific reasoning setting to persist for this session."`
}

type LaunchExplicitNetrunnerOutput struct {
	Status string                          `json:"status"`
	Launch ExplicitNetrunnerLaunchMetadata `json:"launch"`
}

type WaitForNetrunnerSessionInput struct {
	SessionId           int `json:"session_id" jsonschema:"Project-scoped session ID to wait on."`
	TimeoutSeconds      int `json:"timeout_seconds,omitempty" jsonschema:"Optional wait timeout in seconds. Default 7200; max 21600."`
	PollIntervalSeconds int `json:"poll_interval_seconds,omitempty" jsonschema:"Optional poll interval in seconds. Default 5; max 60."`
}

type ExplicitNetrunnerWaitResult struct {
	SessionId             int                          `json:"session_id"`
	SessionStatus         string                       `json:"session_status"`
	Backend               string                       `json:"backend,omitempty"`
	Model                 string                       `json:"model,omitempty"`
	Reasoning             string                       `json:"reasoning,omitempty"`
	ExternalSessionId     string                       `json:"external_session_id,omitempty"`
	Terminal              bool                         `json:"terminal"`
	TerminalCondition     string                       `json:"terminal_condition"`
	TimedOut              bool                         `json:"timed_out"`
	ElapsedSeconds        int                          `json:"elapsed_seconds"`
	TimeoutSeconds        int                          `json:"timeout_seconds"`
	PollIntervalSeconds   int                          `json:"poll_interval_seconds"`
	Report                string                       `json:"report,omitempty"`
	ProposalIds           []int                        `json:"proposal_ids"`
	CodexSessionId        string                       `json:"codex_session_id,omitempty"`
	RepairForkRecommended bool                         `json:"repair_fork_recommended"`
	WorkerProcess         *workerProcessExitDiagnostic `json:"worker_process,omitempty"`
}

type WaitForNetrunnerSessionOutput struct {
	Status string                      `json:"status"`
	Result ExplicitNetrunnerWaitResult `json:"result"`
}

type WaitForNetrunnerSessionsInput struct {
	SessionIds          []int `json:"session_ids,omitempty" jsonschema:"Optional explicit project-scoped session IDs to wait across. When omitted, the tool snapshots the current project's active explicit-launch candidates."`
	TimeoutSeconds      int   `json:"timeout_seconds,omitempty" jsonschema:"Optional wait timeout in seconds. Default 7200; max 21600."`
	PollIntervalSeconds int   `json:"poll_interval_seconds,omitempty" jsonschema:"Optional poll interval in seconds. Default 5; max 60."`
}

type ExplicitNetrunnerWaitAnyResult struct {
	WinningSessionId      int    `json:"winning_session_id,omitempty"`
	SessionStatus         string `json:"session_status,omitempty"`
	Backend               string `json:"backend,omitempty"`
	Model                 string `json:"model,omitempty"`
	Reasoning             string `json:"reasoning,omitempty"`
	ExternalSessionId     string `json:"external_session_id,omitempty"`
	Terminal              bool   `json:"terminal"`
	TerminalCondition     string `json:"terminal_condition"`
	TimedOut              bool   `json:"timed_out"`
	ElapsedSeconds        int    `json:"elapsed_seconds"`
	TimeoutSeconds        int    `json:"timeout_seconds"`
	PollIntervalSeconds   int    `json:"poll_interval_seconds"`
	Report                string `json:"report,omitempty"`
	ProposalIds           []int  `json:"proposal_ids"`
	CodexSessionId        string `json:"codex_session_id,omitempty"`
	ConsideredSessionIds  []int  `json:"considered_session_ids"`
	SelectionMode         string `json:"selection_mode"`
	FollowUpAllowed       bool   `json:"follow_up_allowed"`
	FollowUpBlockedReason string `json:"follow_up_blocked_reason,omitempty"`
	LaunchEpoch           int    `json:"launch_epoch"`
	CurrentEpoch          int    `json:"current_epoch"`
	OrchestrationFrozen   bool   `json:"orchestration_frozen"`
	RepairForkRecommended bool   `json:"repair_fork_recommended"`
}

type WaitForNetrunnerSessionsOutput struct {
	Status string                         `json:"status"`
	Result ExplicitNetrunnerWaitAnyResult `json:"result"`
}

type LaunchAndWaitNetrunnerInput struct {
	SessionId                  int    `json:"session_id" jsonschema:"Project-scoped session ID to launch and then wait on over the explicit MCP-mounted wire path."`
	FixerSessionId             string `json:"fixer_session_id,omitempty" jsonschema:"Optional current Fixer Codex session ID to pass into the explicit wire runtime."`
	SessionReuseOverrideReason string `json:"session_reuse_override_reason,omitempty" jsonschema:"Optional explicit reason to relaunch a session that should normally be replaced by a repair fork after repeated rework or a forced stop."`
	Backend                    string `json:"backend,omitempty" jsonschema:"Optional CLI backend to launch for this session. Supported: codex, droid, antigravity (agy alias)."`
	Model                      string `json:"model,omitempty" jsonschema:"Optional backend-specific model selection to persist for this session."`
	Reasoning                  string `json:"reasoning,omitempty" jsonschema:"Optional backend-specific reasoning setting to persist for this session."`
	TimeoutSeconds             int    `json:"timeout_seconds,omitempty" jsonschema:"Optional wait timeout in seconds. Default 7200; max 21600."`
	PollIntervalSeconds        int    `json:"poll_interval_seconds,omitempty" jsonschema:"Optional poll interval in seconds. Default 5; max 60."`
}

type LaunchAndWaitNetrunnerOutput struct {
	Status string                          `json:"status"`
	Launch ExplicitNetrunnerLaunchMetadata `json:"launch"`
	Wait   ExplicitNetrunnerWaitResult     `json:"wait"`
}

type activeLaunchSession struct {
	LocalSessionID     int
	GlobalSessionID    int
	DeclaredWriteScope []string
}

func loadActiveLaunchSessions(projectID int) ([]activeLaunchSession, error) {
	processes, err := listRunningWorkerProcesses(projectID, nil)
	if err != nil {
		return nil, err
	}

	seen := make(map[int]struct{}, len(processes))
	activeSessions := make([]activeLaunchSession, 0, len(processes))
	for _, process := range processes {
		if _, exists := seen[process.SessionID]; exists {
			continue
		}
		seen[process.SessionID] = struct{}{}

		state, err := fetchSessionLifecycleState(process.SessionID, projectID)
		if err != nil {
			return nil, err
		}
		localSessionID, err := projectScopedSessionIDFromGlobal(process.SessionID, projectID)
		if err != nil {
			return nil, err
		}
		activeSessions = append(activeSessions, activeLaunchSession{
			LocalSessionID:     localSessionID,
			GlobalSessionID:    process.SessionID,
			DeclaredWriteScope: state.DeclaredWriteScope,
		})
	}
	return activeSessions, nil
}

func waitFollowUpDecision(control orchestrationControl, launchEpoch int) (bool, string) {
	reasons := []string{}
	if control.OrchestrationFrozen {
		reasons = append(reasons, "project_orchestration_frozen")
	}
	if launchEpoch > 0 && control.OrchestrationEpoch != launchEpoch {
		reasons = append(reasons, fmt.Sprintf("stale_orchestration_epoch:%d->%d", launchEpoch, control.OrchestrationEpoch))
	}
	if len(reasons) > 0 {
		return false, strings.Join(reasons, ",")
	}
	return true, ""
}

func launchExplicitNetrunnerWithMetadata(ctx context.Context, input LaunchExplicitNetrunnerInput) (ExplicitNetrunnerLaunchMetadata, error) {
	sessionID := input.SessionId
	globalSessionID, err := globalSessionIDFromProjectScoped(sessionID, authorizedProjectId)
	if err == sql.ErrNoRows {
		return ExplicitNetrunnerLaunchMetadata{}, fmt.Errorf("session not found in current project")
	}
	if err != nil {
		return ExplicitNetrunnerLaunchMetadata{}, fmt.Errorf("DB query error: %v", err)
	}

	belongs, err := sessionBelongsToProject(globalSessionID, authorizedProjectId)
	if err != nil {
		return ExplicitNetrunnerLaunchMetadata{}, fmt.Errorf("DB query error: %v", err)
	}
	if !belongs {
		return ExplicitNetrunnerLaunchMetadata{}, fmt.Errorf("session not found in current project")
	}

	sessionState, err := fetchSessionLifecycleState(globalSessionID, authorizedProjectId)
	if err != nil {
		return ExplicitNetrunnerLaunchMetadata{}, fmt.Errorf("DB query error: %v", err)
	}
	if len(sessionState.DeclaredWriteScope) == 0 {
		return ExplicitNetrunnerLaunchMetadata{}, fmt.Errorf("session %d must declare a non-empty write scope before explicit launch", sessionID)
	}
	if shouldRecommendRepairFork(sessionState.ReworkCount, sessionState.ForcedStopCount, sessionState.RepairSourceSessionID) &&
		strings.TrimSpace(input.SessionReuseOverrideReason) == "" {
		return ExplicitNetrunnerLaunchMetadata{}, fmt.Errorf(
			"session %d should be replaced with fork_repair_session_from before relaunch (rework_count=%d forced_stop_count=%d); provide session_reuse_override_reason to reuse it intentionally",
			sessionID,
			sessionState.ReworkCount,
			sessionState.ForcedStopCount,
		)
	}

	activeSessions, err := loadActiveLaunchSessions(authorizedProjectId)
	if err != nil {
		return ExplicitNetrunnerLaunchMetadata{}, fmt.Errorf("failed to inspect active worker processes: %v", err)
	}
	if len(activeSessions) > 0 {
		for _, activeSession := range activeSessions {
			if activeSession.GlobalSessionID == globalSessionID {
				return ExplicitNetrunnerLaunchMetadata{}, fmt.Errorf("session %d already has an active worker process", sessionID)
			}
		}
	}

	launchConfig, err := resolveSessionLaunchConfig(globalSessionID, authorizedProjectId, input.Backend, input.Model, input.Reasoning)
	if err != nil {
		return ExplicitNetrunnerLaunchMetadata{}, fmt.Errorf("failed to resolve session launch backend: %v", err)
	}

	projectCWD, err := projectCWDFromID(authorizedProjectId)
	if err != nil {
		return ExplicitNetrunnerLaunchMetadata{}, fmt.Errorf("failed to resolve project cwd: %v", err)
	}

	launcherScript, err := resolveExplicitLauncherScript()
	if err != nil {
		return ExplicitNetrunnerLaunchMetadata{}, err
	}
	headlessLogPath, launcherLogPath, metadataPath, err := explicitLaunchArtifacts(projectCWD, sessionID, launchConfig.Backend)
	if err != nil {
		return ExplicitNetrunnerLaunchMetadata{}, err
	}

	commandArgs := []string{
		launcherScript,
		"launch-netrunner",
		"--cwd",
		projectCWD,
		"--session-id",
		fmt.Sprintf("%d", sessionID),
	}
	if trimmedFixerSessionID := strings.TrimSpace(input.FixerSessionId); trimmedFixerSessionID != "" {
		commandArgs = append(commandArgs, "--fixer-session-id", trimmedFixerSessionID)
	}
	commandArgs = append(commandArgs, "--backend", launchConfig.Backend)
	if strings.TrimSpace(launchConfig.Model) != "" {
		commandArgs = append(commandArgs, "--model", launchConfig.Model)
	}
	if strings.TrimSpace(launchConfig.Reasoning) != "" {
		commandArgs = append(commandArgs, "--reasoning", launchConfig.Reasoning)
	}
	commandArgs = append(
		commandArgs,
		"--headless-log-path", headlessLogPath,
		"--worker-metadata-path", metadataPath,
		"--suppress-autonomous-wake",
	)

	command := execCommand("python3", commandArgs...)
	commandEnv, envErr := resolveRuntimeLaunchEnv(projectCWD, os.Environ())
	if envErr != nil {
		log.Printf("warning: failed to resolve runtime launch env for %s: %v", projectCWD, envErr)
		commandEnv = os.Environ()
	}
	command.Env = commandEnv
	launcherLogHandle, err := os.OpenFile(launcherLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return ExplicitNetrunnerLaunchMetadata{}, fmt.Errorf("failed to open launcher diagnostic log: %v", err)
	}
	defer launcherLogHandle.Close()
	command.Stdout = launcherLogHandle
	command.Stderr = launcherLogHandle
	if err := command.Start(); err != nil {
		return ExplicitNetrunnerLaunchMetadata{}, fmt.Errorf("failed to launch explicit netrunner: %v", err)
	}

	waitErrCh := make(chan error, 1)
	go func() {
		waitErrCh <- command.Wait()
	}()

	launcherPID := 0
	if command.Process != nil {
		launcherPID = command.Process.Pid
	}

	select {
	case waitErr := <-waitErrCh:
		if waitErr != nil {
			return ExplicitNetrunnerLaunchMetadata{}, fmt.Errorf(
				"explicit netrunner launcher exited before startup completed: %v\nlauncher log: %s\nheadless log: %s",
				waitErr,
				launcherLogPath,
				headlessLogPath,
			)
		}
	case <-time.After(explicitLauncherExitGracePeriod):
	}

	workerPID := launcherPID
	if metadata, metadataErr := readExplicitLaunchWorkerMetadata(metadataPath); metadataErr == nil {
		workerPID = metadata.WorkerPID
		if strings.TrimSpace(metadata.HeadlessLogPath) != "" {
			headlessLogPath = strings.TrimSpace(metadata.HeadlessLogPath)
		}
	}
	if workerPID > 0 {
		if err := recordWorkerProcessLaunch(authorizedProjectId, globalSessionID, workerPID, 0); err != nil {
			return ExplicitNetrunnerLaunchMetadata{}, fmt.Errorf("failed to persist worker process metadata: %v", err)
		}
	}

	externalSessionID, err := waitForSessionExternalID(ctx, globalSessionID, launchConfig.Backend, 10*time.Second)
	if err != nil {
		return ExplicitNetrunnerLaunchMetadata{}, fmt.Errorf("failed while waiting for backend session metadata: %v", err)
	}

	log.Printf(
		"launch_explicit_netrunner project_id=%d session_id=%d backend=%q model=%q reasoning=%q fixer_session_id=%q external_session_id=%q",
		authorizedProjectId,
		sessionID,
		launchConfig.Backend,
		launchConfig.Model,
		launchConfig.Reasoning,
		strings.TrimSpace(input.FixerSessionId),
		externalSessionID,
	)

	legacyCodexSessionID := ""
	if launchConfig.Backend == defaultCliBackend {
		legacyCodexSessionID = externalSessionID
	}

	return ExplicitNetrunnerLaunchMetadata{
		SessionId:          sessionID,
		ProjectCwd:         projectCWD,
		LauncherScript:     launcherScript,
		Backend:            launchConfig.Backend,
		Model:              launchConfig.Model,
		Reasoning:          launchConfig.Reasoning,
		ExternalSessionId:  externalSessionID,
		CodexSessionId:     legacyCodexSessionID,
		SpawnedBackground:  true,
		DeclaredWriteScope: append([]string{}, sessionState.DeclaredWriteScope...),
	}, nil
}

func fetchSessionWaitSnapshot(sessionID int, projectID int) (string, string, []int, string, string, string, string, error) {
	var status string
	var report string
	err := db.QueryRow(
		"SELECT status, COALESCE(report, '') FROM session WHERE id = ? AND project_id = ?",
		sessionID,
		projectID,
	).Scan(&status, &report)
	if err != nil {
		return "", "", nil, "", "", "", "", err
	}

	proposalIDs, err := projectScopedDocProposalIDsForSession(sessionID, projectID)
	if err != nil {
		return "", "", nil, "", "", "", "", err
	}

	launchConfig, err := readSessionLaunchConfig(sessionID, projectID)
	if err != nil {
		return "", "", nil, "", "", "", "", err
	}

	externalSessionID, err := fetchSessionExternalID(sessionID, launchConfig.Backend)
	if err != nil {
		return "", "", nil, "", "", "", "", err
	}

	return status, report, proposalIDs, launchConfig.Backend, launchConfig.Model, launchConfig.Reasoning, externalSessionID, nil
}

type explicitWaitCandidate struct {
	LocalSessionID  int
	GlobalSessionID int
	InitialStatus   string
}

type explicitWaitSnapshot struct {
	LocalSessionID        int
	Status                string
	Report                string
	ProposalIDs           []int
	Backend               string
	Model                 string
	Reasoning             string
	ExternalSessionID     string
	CodexSessionID        string
	LaunchEpoch           int
	CurrentEpoch          int
	OrchestrationFrozen   bool
	FollowUpAllowed       bool
	FollowUpBlockedReason string
	RepairForkRecommended bool
}

func resolveExplicitWaitCandidatesFromList(sessionIDs []int, projectID int) ([]explicitWaitCandidate, error) {
	normalizedIDs := make([]int, 0, len(sessionIDs))
	seen := make(map[int]struct{}, len(sessionIDs))
	for _, sessionID := range sessionIDs {
		if sessionID <= 0 {
			return nil, fmt.Errorf("session_ids must contain only positive project-scoped ids")
		}
		if _, exists := seen[sessionID]; exists {
			continue
		}
		seen[sessionID] = struct{}{}
		normalizedIDs = append(normalizedIDs, sessionID)
	}
	sort.Ints(normalizedIDs)
	if len(normalizedIDs) == 0 {
		return nil, fmt.Errorf("session_ids must contain at least one project-scoped id")
	}

	candidates := make([]explicitWaitCandidate, 0, len(normalizedIDs))
	for _, localSessionID := range normalizedIDs {
		globalSessionID, err := globalSessionIDFromProjectScoped(localSessionID, projectID)
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("session %d not found in current project", localSessionID)
		}
		if err != nil {
			return nil, fmt.Errorf("DB query error: %v", err)
		}

		var initialStatus string
		err = db.QueryRow(
			"SELECT status FROM session WHERE id = ? AND project_id = ?",
			globalSessionID,
			projectID,
		).Scan(&initialStatus)
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("session %d not found in current project", localSessionID)
		}
		if err != nil {
			return nil, fmt.Errorf("DB query error: %v", err)
		}

		candidates = append(candidates, explicitWaitCandidate{
			LocalSessionID:  localSessionID,
			GlobalSessionID: globalSessionID,
			InitialStatus:   initialStatus,
		})
	}

	return candidates, nil
}

func discoverProjectWaitCandidates(projectID int) ([]explicitWaitCandidate, error) {
	rows, err := db.Query(
		`SELECT target.id,
		        (
		          SELECT COUNT(*)
		          FROM session ranked
		          WHERE ranked.project_id = ? AND ranked.id <= target.id
		        ) AS local_session_id,
		        target.status
		 FROM session AS target
		 WHERE target.project_id = ?
		   AND (
		     target.status IN ('in_progress', 'review', 'completed')
		     OR (
		       target.status = 'pending'
		       AND EXISTS (
		         SELECT 1
		         FROM worker_process process
		         WHERE process.session_id = target.id
		           AND process.project_id = target.project_id
		           AND process.status = 'running'
		       )
		     )
		   )
		 ORDER BY target.id`,
		projectID,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	candidates := []explicitWaitCandidate{}
	for rows.Next() {
		var candidate explicitWaitCandidate
		if err := rows.Scan(&candidate.GlobalSessionID, &candidate.LocalSessionID, &candidate.InitialStatus); err != nil {
			return nil, err
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return candidates, nil
}

func fetchExplicitWaitSnapshot(candidate explicitWaitCandidate, projectID int, control orchestrationControl) (explicitWaitSnapshot, error) {
	status, report, proposalIDs, backend, model, reasoning, externalSessionID, err := fetchSessionWaitSnapshot(candidate.GlobalSessionID, projectID)
	if err != nil {
		return explicitWaitSnapshot{}, err
	}
	sessionState, err := fetchSessionLifecycleState(candidate.GlobalSessionID, projectID)
	if err != nil {
		return explicitWaitSnapshot{}, err
	}
	launchEpoch, err := latestWorkerLaunchEpoch(candidate.GlobalSessionID, projectID)
	if err != nil {
		return explicitWaitSnapshot{}, err
	}
	followUpAllowed, blockedReason := waitFollowUpDecision(control, launchEpoch)
	return explicitWaitSnapshot{
		LocalSessionID:    candidate.LocalSessionID,
		Status:            status,
		Report:            report,
		ProposalIDs:       proposalIDs,
		Backend:           backend,
		Model:             model,
		Reasoning:         reasoning,
		ExternalSessionID: externalSessionID,
		CodexSessionID: func() string {
			if backend == defaultCliBackend {
				return externalSessionID
			}
			return ""
		}(),
		LaunchEpoch:           launchEpoch,
		CurrentEpoch:          control.OrchestrationEpoch,
		OrchestrationFrozen:   control.OrchestrationFrozen,
		FollowUpAllowed:       followUpAllowed,
		FollowUpBlockedReason: blockedReason,
		RepairForkRecommended: shouldRecommendRepairFork(sessionState.ReworkCount, sessionState.ForcedStopCount, sessionState.RepairSourceSessionID),
	}, nil
}

func classifyWaitTerminalCondition(initialStatus string, currentStatus string, seenActive bool) (bool, string) {
	switch currentStatus {
	case "review":
		return true, "review_ready"
	case "completed":
		return true, "completed"
	case "pending":
		if seenActive || initialStatus == "in_progress" || initialStatus == "review" || initialStatus == "completed" {
			return true, "requeued_for_rework"
		}
	}
	return false, ""
}

func isExplicitWaitWorkerWatchdogStatus(status string) bool {
	return status == "pending" || status == "in_progress"
}

func isWorkerProcessTerminal(process workerProcessSnapshot) bool {
	return process.Status != workerStatusRunning || !process.Alive
}

func markAutonomousRunBlockedForWorkerExit(projectID int, globalSessionID int, localSessionID int, diagnostic workerProcessExitDiagnostic) error {
	control, exists, err := fetchOrchestrationControl(projectID)
	if err != nil {
		return err
	}
	if !exists {
		control = orchestrationControl{
			ProjectID:                        projectID,
			OrchestrationEpoch:               0,
			OrchestrationFrozen:              false,
			NotificationsEnabledForActiveRun: true,
		}
	}
	evidencePayload := struct {
		TerminalCondition string                      `json:"terminal_condition"`
		SessionID         int                         `json:"session_id"`
		WorkerProcess     workerProcessExitDiagnostic `json:"worker_process"`
	}{
		TerminalCondition: "worker_process_exited",
		SessionID:         localSessionID,
		WorkerProcess:     diagnostic,
	}
	evidenceBytes, err := json.Marshal(evidencePayload)
	if err != nil {
		return err
	}
	return upsertOrchestrationControl(
		projectID,
		globalSessionID,
		"blocked",
		fmt.Sprintf("Netrunner session %d worker process exited during explicit wait", localSessionID),
		control.Focus,
		"worker process exited",
		string(evidenceBytes),
		control.OrchestrationEpoch,
		control.OrchestrationFrozen,
		control.NotificationsEnabledForActiveRun,
	)
}

func malformedReviewSnapshotReason(localSessionID int, status string, report string, proposalIDs []int) string {
	if status != "review" {
		return ""
	}
	missing := []string{}
	if strings.TrimSpace(report) == "" {
		missing = append(missing, "final report")
	}
	if len(proposalIDs) == 0 {
		missing = append(missing, "doc-impact proposal")
	}
	if len(missing) == 0 {
		return ""
	}
	return fmt.Sprintf(
		"session %d reached review without %s; this cannot be produced by complete_task and points to a stale/manual status update or worker-completion gap",
		localSessionID,
		strings.Join(missing, " and "),
	)
}

func waitForNetrunnerSessionsResult(ctx context.Context, sessionIDs []int, timeoutSeconds int, pollIntervalSeconds int) (ExplicitNetrunnerWaitAnyResult, error) {
	timeoutSeconds, err := explicitWaitTimeoutSeconds(timeoutSeconds)
	if err != nil {
		return ExplicitNetrunnerWaitAnyResult{}, err
	}
	pollIntervalSeconds, err = explicitWaitPollIntervalSeconds(pollIntervalSeconds)
	if err != nil {
		return ExplicitNetrunnerWaitAnyResult{}, err
	}

	selectionMode := "explicit_list"
	candidates := []explicitWaitCandidate{}
	if len(sessionIDs) > 0 {
		candidates, err = resolveExplicitWaitCandidatesFromList(sessionIDs, authorizedProjectId)
		if err != nil {
			return ExplicitNetrunnerWaitAnyResult{}, err
		}
	} else {
		selectionMode = "auto_project_candidates"
		candidates, err = discoverProjectWaitCandidates(authorizedProjectId)
		if err != nil {
			return ExplicitNetrunnerWaitAnyResult{}, fmt.Errorf("DB query error: %v", err)
		}
		if len(candidates) == 0 {
			return ExplicitNetrunnerWaitAnyResult{}, fmt.Errorf("no active explicit-launch wait candidates found in current project")
		}
	}

	consideredSessionIDs := make([]int, 0, len(candidates))
	seenActive := make(map[int]bool, len(candidates))
	for _, candidate := range candidates {
		consideredSessionIDs = append(consideredSessionIDs, candidate.LocalSessionID)
		seenActive[candidate.LocalSessionID] = candidate.InitialStatus == "in_progress" || candidate.InitialStatus == "review" || candidate.InitialStatus == "completed"
	}

	startedAt := time.Now()
	deadline := startedAt.Add(time.Duration(timeoutSeconds) * time.Second)

	buildTimeoutResult := func() ExplicitNetrunnerWaitAnyResult {
		control, _, err := fetchOrchestrationControl(authorizedProjectId)
		if err != nil {
			control = orchestrationControl{ProjectID: authorizedProjectId, NotificationsEnabledForActiveRun: true}
		}
		return ExplicitNetrunnerWaitAnyResult{
			Terminal:             false,
			TerminalCondition:    "timed_out",
			TimedOut:             true,
			ElapsedSeconds:       int(time.Since(startedAt).Seconds()),
			TimeoutSeconds:       timeoutSeconds,
			PollIntervalSeconds:  pollIntervalSeconds,
			ProposalIds:          []int{},
			ConsideredSessionIds: append([]int{}, consideredSessionIDs...),
			SelectionMode:        selectionMode,
			FollowUpAllowed:      !control.OrchestrationFrozen,
			CurrentEpoch:         control.OrchestrationEpoch,
			OrchestrationFrozen:  control.OrchestrationFrozen,
		}
	}

	buildWinnerResult := func(snapshot explicitWaitSnapshot, terminalCondition string) ExplicitNetrunnerWaitAnyResult {
		proposalIDs := snapshot.ProposalIDs
		if proposalIDs == nil {
			proposalIDs = []int{}
		}
		legacyCodexSessionID := ""
		if snapshot.Backend == defaultCliBackend {
			legacyCodexSessionID = snapshot.ExternalSessionID
		}
		return ExplicitNetrunnerWaitAnyResult{
			WinningSessionId:      snapshot.LocalSessionID,
			SessionStatus:         snapshot.Status,
			Backend:               snapshot.Backend,
			Model:                 snapshot.Model,
			Reasoning:             snapshot.Reasoning,
			ExternalSessionId:     snapshot.ExternalSessionID,
			Terminal:              true,
			TerminalCondition:     terminalCondition,
			TimedOut:              false,
			ElapsedSeconds:        int(time.Since(startedAt).Seconds()),
			TimeoutSeconds:        timeoutSeconds,
			PollIntervalSeconds:   pollIntervalSeconds,
			Report:                snapshot.Report,
			ProposalIds:           proposalIDs,
			CodexSessionId:        legacyCodexSessionID,
			ConsideredSessionIds:  append([]int{}, consideredSessionIDs...),
			SelectionMode:         selectionMode,
			FollowUpAllowed:       snapshot.FollowUpAllowed,
			FollowUpBlockedReason: snapshot.FollowUpBlockedReason,
			LaunchEpoch:           snapshot.LaunchEpoch,
			CurrentEpoch:          snapshot.CurrentEpoch,
			OrchestrationFrozen:   snapshot.OrchestrationFrozen,
			RepairForkRecommended: snapshot.RepairForkRecommended,
		}
	}

	for {
		if err := ctx.Err(); err != nil {
			return ExplicitNetrunnerWaitAnyResult{}, err
		}

		control, _, err := fetchOrchestrationControl(authorizedProjectId)
		if err != nil {
			return ExplicitNetrunnerWaitAnyResult{}, fmt.Errorf("DB query error: %v", err)
		}
		for _, candidate := range candidates {
			snapshot, err := fetchExplicitWaitSnapshot(candidate, authorizedProjectId, control)
			if err != nil {
				return ExplicitNetrunnerWaitAnyResult{}, fmt.Errorf("DB query error: %v", err)
			}
			if snapshot.Status == "in_progress" || snapshot.Status == "review" || snapshot.Status == "completed" {
				seenActive[candidate.LocalSessionID] = true
			}
			if reason := malformedReviewSnapshotReason(candidate.LocalSessionID, snapshot.Status, snapshot.Report, snapshot.ProposalIDs); reason != "" {
				return ExplicitNetrunnerWaitAnyResult{}, errors.New(reason)
			}
			terminal, terminalCondition := classifyWaitTerminalCondition(candidate.InitialStatus, snapshot.Status, seenActive[candidate.LocalSessionID])
			if terminal {
				return buildWinnerResult(snapshot, terminalCondition), nil
			}
		}

		if time.Now().After(deadline) {
			return buildTimeoutResult(), nil
		}

		time.Sleep(time.Duration(pollIntervalSeconds) * time.Second)
	}
}

func waitForNetrunnerSessionResult(ctx context.Context, sessionID int, timeoutSeconds int, pollIntervalSeconds int) (ExplicitNetrunnerWaitResult, error) {
	timeoutSeconds, err := explicitWaitTimeoutSeconds(timeoutSeconds)
	if err != nil {
		return ExplicitNetrunnerWaitResult{}, err
	}
	pollIntervalSeconds, err = explicitWaitPollIntervalSeconds(pollIntervalSeconds)
	if err != nil {
		return ExplicitNetrunnerWaitResult{}, err
	}

	globalSessionID, err := globalSessionIDFromProjectScoped(sessionID, authorizedProjectId)
	if err == sql.ErrNoRows {
		return ExplicitNetrunnerWaitResult{}, fmt.Errorf("session not found in current project")
	}
	if err != nil {
		return ExplicitNetrunnerWaitResult{}, fmt.Errorf("DB query error: %v", err)
	}

	belongs, err := sessionBelongsToProject(globalSessionID, authorizedProjectId)
	if err != nil {
		return ExplicitNetrunnerWaitResult{}, fmt.Errorf("DB query error: %v", err)
	}
	if !belongs {
		return ExplicitNetrunnerWaitResult{}, fmt.Errorf("session not found in current project")
	}

	initialStatus, report, proposalIDs, backend, model, reasoning, externalSessionID, err := fetchSessionWaitSnapshot(globalSessionID, authorizedProjectId)
	if err != nil {
		return ExplicitNetrunnerWaitResult{}, fmt.Errorf("DB query error: %v", err)
	}
	currentStatus := initialStatus

	startedAt := time.Now()
	deadline := startedAt.Add(time.Duration(timeoutSeconds) * time.Second)
	startupFailureDeadline := startedAt.Add(netrunnerStartupFailureWindow)
	seenActive := initialStatus == "in_progress" || initialStatus == "review" || initialStatus == "completed"

	buildResult := func(currentStatus string, terminal bool, terminalCondition string, timedOut bool, currentReport string, currentProposalIDs []int, currentBackend string, currentModel string, currentReasoning string, currentExternalSessionID string, repairForkRecommended bool, workerProcess *workerProcessExitDiagnostic) ExplicitNetrunnerWaitResult {
		if currentProposalIDs == nil {
			currentProposalIDs = []int{}
		}
		legacyCodexSessionID := ""
		if currentBackend == defaultCliBackend {
			legacyCodexSessionID = currentExternalSessionID
		}
		return ExplicitNetrunnerWaitResult{
			SessionId:             sessionID,
			SessionStatus:         currentStatus,
			Backend:               currentBackend,
			Model:                 currentModel,
			Reasoning:             currentReasoning,
			ExternalSessionId:     currentExternalSessionID,
			Terminal:              terminal,
			TerminalCondition:     terminalCondition,
			TimedOut:              timedOut,
			ElapsedSeconds:        int(time.Since(startedAt).Seconds()),
			TimeoutSeconds:        timeoutSeconds,
			PollIntervalSeconds:   pollIntervalSeconds,
			Report:                currentReport,
			ProposalIds:           currentProposalIDs,
			CodexSessionId:        legacyCodexSessionID,
			RepairForkRecommended: repairForkRecommended,
			WorkerProcess:         workerProcess,
		}
	}

	for {
		if err := ctx.Err(); err != nil {
			return ExplicitNetrunnerWaitResult{}, err
		}

		sessionState, err := fetchSessionLifecycleState(globalSessionID, authorizedProjectId)
		if err != nil {
			return ExplicitNetrunnerWaitResult{}, fmt.Errorf("DB query error: %v", err)
		}

		if currentStatus == "in_progress" || currentStatus == "review" || currentStatus == "completed" {
			seenActive = true
		}
		if reason := malformedReviewSnapshotReason(sessionID, currentStatus, report, proposalIDs); reason != "" {
			return ExplicitNetrunnerWaitResult{}, errors.New(reason)
		}

		terminal, terminalCondition := classifyWaitTerminalCondition(initialStatus, currentStatus, seenActive)
		if terminal {
			return buildResult(currentStatus, true, terminalCondition, false, report, proposalIDs, backend, model, reasoning, externalSessionID, shouldRecommendRepairFork(sessionState.ReworkCount, sessionState.ForcedStopCount, sessionState.RepairSourceSessionID), nil), nil
		}
		if isExplicitWaitWorkerWatchdogStatus(currentStatus) {
			process, found, err := latestWorkerProcessForSession(authorizedProjectId, globalSessionID)
			if err != nil {
				return ExplicitNetrunnerWaitResult{}, fmt.Errorf("DB query error: %v", err)
			}
			if found && isWorkerProcessTerminal(process) {
				diagnostic := buildWorkerProcessExitDiagnostic(authorizedProjectId, sessionID, process)
				if err := markAutonomousRunBlockedForWorkerExit(authorizedProjectId, globalSessionID, sessionID, diagnostic); err != nil {
					return ExplicitNetrunnerWaitResult{}, fmt.Errorf("DB upsert error: %v", err)
				}
				return buildResult(currentStatus, true, "worker_process_exited", false, report, proposalIDs, backend, model, reasoning, externalSessionID, shouldRecommendRepairFork(sessionState.ReworkCount, sessionState.ForcedStopCount, sessionState.RepairSourceSessionID), &diagnostic), nil
			}
		}
		if initialStatus == "pending" && currentStatus == "pending" && time.Now().After(startupFailureDeadline) {
			return ExplicitNetrunnerWaitResult{}, errors.New(buildNetrunnerStartupFailureMessage(authorizedProjectId, sessionID, currentStatus))
		}

		if time.Now().After(deadline) {
			return buildResult(currentStatus, false, "timed_out", true, report, proposalIDs, backend, model, reasoning, externalSessionID, shouldRecommendRepairFork(sessionState.ReworkCount, sessionState.ForcedStopCount, sessionState.RepairSourceSessionID), nil), nil
		}

		time.Sleep(time.Duration(pollIntervalSeconds) * time.Second)

		currentStatus, report, proposalIDs, backend, model, reasoning, externalSessionID, err = fetchSessionWaitSnapshot(globalSessionID, authorizedProjectId)
		if err != nil {
			return ExplicitNetrunnerWaitResult{}, fmt.Errorf("DB query error: %v", err)
		}
	}
}

func LaunchExplicitNetrunner(ctx context.Context, req *mcp.CallToolRequest, input LaunchExplicitNetrunnerInput) (*mcp.CallToolResult, LaunchExplicitNetrunnerOutput, error) {
	return &mcp.CallToolResult{IsError: true}, LaunchExplicitNetrunnerOutput{}, fmt.Errorf(
		"launch_explicit_netrunner is temporarily disabled; use launch_and_wait_netrunner for the one-task autonomous flow or run-manual-netrunner for separate-terminal work",
	)
}

func WaitForNetrunnerSession(ctx context.Context, req *mcp.CallToolRequest, input WaitForNetrunnerSessionInput) (*mcp.CallToolResult, WaitForNetrunnerSessionOutput, error) {
	if authorizedRole != "fixer" {
		return &mcp.CallToolResult{IsError: true}, WaitForNetrunnerSessionOutput{}, fmt.Errorf("access denied: requires fixer role")
	}

	result, err := waitForNetrunnerSessionResult(ctx, input.SessionId, input.TimeoutSeconds, input.PollIntervalSeconds)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, WaitForNetrunnerSessionOutput{}, err
	}

	log.Printf("wait_for_netrunner_session project_id=%d session_id=%d terminal=%t condition=%q timed_out=%t status=%q", authorizedProjectId, input.SessionId, result.Terminal, result.TerminalCondition, result.TimedOut, result.SessionStatus)

	return nil, WaitForNetrunnerSessionOutput{
		Status: "success",
		Result: result,
	}, nil
}

func WaitForNetrunnerSessions(ctx context.Context, req *mcp.CallToolRequest, input WaitForNetrunnerSessionsInput) (*mcp.CallToolResult, WaitForNetrunnerSessionsOutput, error) {
	return &mcp.CallToolResult{IsError: true}, WaitForNetrunnerSessionsOutput{}, fmt.Errorf(
		"wait_for_netrunner_sessions is temporarily disabled; parallel Netrunner orchestration is not available",
	)
}

func LaunchAndWaitNetrunner(ctx context.Context, req *mcp.CallToolRequest, input LaunchAndWaitNetrunnerInput) (*mcp.CallToolResult, LaunchAndWaitNetrunnerOutput, error) {
	if authorizedRole != "fixer" {
		return &mcp.CallToolResult{IsError: true}, LaunchAndWaitNetrunnerOutput{}, fmt.Errorf("access denied: requires fixer role")
	}

	launch, err := launchExplicitNetrunnerWithMetadata(ctx, LaunchExplicitNetrunnerInput{
		SessionId:                  input.SessionId,
		FixerSessionId:             input.FixerSessionId,
		SessionReuseOverrideReason: input.SessionReuseOverrideReason,
		Backend:                    input.Backend,
		Model:                      input.Model,
		Reasoning:                  input.Reasoning,
	})
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, LaunchAndWaitNetrunnerOutput{}, err
	}

	waitResult, err := waitForNetrunnerSessionResult(ctx, input.SessionId, input.TimeoutSeconds, input.PollIntervalSeconds)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, LaunchAndWaitNetrunnerOutput{}, err
	}

	return nil, LaunchAndWaitNetrunnerOutput{
		Status: "success",
		Launch: launch,
		Wait:   waitResult,
	}, nil
}
