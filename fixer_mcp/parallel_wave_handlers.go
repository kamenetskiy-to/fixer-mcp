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
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func recordWaveWorkerProcessLaunch(projectID int, sessionID int, pid int, launchEpoch int, waveID int, waveWorkerID int) (int, error) {
	result, err := db.Exec(
		`INSERT INTO worker_process (
			project_id,
			session_id,
			pid,
			launch_epoch,
			status,
			parallel_wave_id,
			parallel_wave_worker_id,
			updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
		projectID,
		sessionID,
		pid,
		launchEpoch,
		workerStatusRunning,
		waveID,
		waveWorkerID,
	)
	if err != nil {
		return 0, err
	}
	insertID, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	return int(insertID), nil
}

type CreateNetrunnerWaveInput struct {
	SessionIds   []int  `json:"session_ids" jsonschema:"Project-scoped pending session IDs to include in the wave. Must contain at least two sessions."`
	WorktreeRoot string `json:"worktree_root,omitempty" jsonschema:"Optional project-relative or absolute root for future worker worktrees. Defaults to .codex/netrunner_worktrees."`
	BaseRef      string `json:"base_ref,omitempty" jsonschema:"Optional Git base ref to resolve for the wave. Defaults to HEAD."`
	Reason       string `json:"reason,omitempty" jsonschema:"Optional audit reason for creating the wave."`
}

type GetNetrunnerWaveInput struct {
	WaveId int `json:"wave_id" jsonschema:"Parallel wave ID to read."`
}

type LaunchNetrunnerWaveInput struct {
	WaveId         int    `json:"wave_id" jsonschema:"Parallel wave ID to launch."`
	Backend        string `json:"backend,omitempty" jsonschema:"Optional CLI backend to launch for workers. Supported: codex, droid, antigravity, junie."`
	Model          string `json:"model,omitempty" jsonschema:"Optional backend-specific model selection to persist for each worker session."`
	Reasoning      string `json:"reasoning,omitempty" jsonschema:"Optional backend-specific reasoning setting to persist for each worker session."`
	FixerSessionId string `json:"fixer_session_id,omitempty" jsonschema:"Optional current Fixer Codex session ID to pass into worker prompts."`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema:"Optional startup metadata wait in seconds. Default 120; max 21600."`
}

type WaitForNetrunnerWaveInput struct {
	WaveId              int    `json:"wave_id" jsonschema:"Parallel wave ID to wait on."`
	TimeoutSeconds      int    `json:"timeout_seconds,omitempty" jsonschema:"Optional wait timeout in seconds. Default 7200; max 21600."`
	PollIntervalSeconds int    `json:"poll_interval_seconds,omitempty" jsonschema:"Optional poll interval in seconds. Default 5; max 60."`
	ReturnWhen          string `json:"return_when,omitempty" jsonschema:"When to return. Supported values: first_review_ready (default), all_terminal."`
}

type CleanupNetrunnerWaveInput struct {
	WaveId          int  `json:"wave_id" jsonschema:"Parallel wave ID to clean up."`
	RemoveWorktrees bool `json:"remove_worktrees,omitempty" jsonschema:"When true, remove recorded terminal worker worktrees with git worktree remove. Defaults false."`
	Prune           bool `json:"prune,omitempty" jsonschema:"When true, run git worktree prune after cleanup inspection/removal. Defaults false."`
	Force           bool `json:"force,omitempty" jsonschema:"When true, pass --force to git worktree remove. Defaults false."`
}

type NetrunnerWaveWorkerSnapshot struct {
	Id                 int      `json:"id"`
	WaveId             int      `json:"wave_id"`
	ProjectId          int      `json:"project_id"`
	SessionId          int      `json:"session_id"`
	Status             string   `json:"status"`
	DeclaredWriteScope []string `json:"declared_write_scope"`
	BranchName         string   `json:"branch_name"`
	WorktreePath       string   `json:"worktree_path"`
	BaseSha            string   `json:"base_sha"`
	HeadSha            string   `json:"head_sha"`
	ChangedPaths       []string `json:"changed_paths"`
	DiffPatchPath      string   `json:"diff_patch_path"`
	DiffStat           string   `json:"diff_stat"`
	LaunchEpoch        int      `json:"launch_epoch"`
	WorkerProcessId    int      `json:"worker_process_id,omitempty"`
	ExternalSessionId  string   `json:"external_session_id"`
	HeadlessLogPath    string   `json:"headless_log_path"`
	LauncherLogPath    string   `json:"launcher_log_path"`
	WorkerMetadataPath string   `json:"worker_metadata_path"`
	FailureReason      string   `json:"failure_reason"`
	CleanupStatus      string   `json:"cleanup_status"`
	CreatedAt          string   `json:"created_at"`
	UpdatedAt          string   `json:"updated_at"`
	LaunchedAt         string   `json:"launched_at,omitempty"`
	TerminalAt         string   `json:"terminal_at,omitempty"`
	CleanedAt          string   `json:"cleaned_at,omitempty"`
}

type NetrunnerWaveSnapshot struct {
	Id                 int                           `json:"id"`
	ProjectId          int                           `json:"project_id"`
	Status             string                        `json:"status"`
	BaseSha            string                        `json:"base_sha"`
	BaseBranch         string                        `json:"base_branch"`
	ProjectCwd         string                        `json:"project_cwd"`
	WorktreeRoot       string                        `json:"worktree_root"`
	OrchestrationEpoch int                           `json:"orchestration_epoch"`
	CreatedBySessionId int                           `json:"created_by_session_id,omitempty"`
	FailureReason      string                        `json:"failure_reason"`
	CreatedAt          string                        `json:"created_at"`
	UpdatedAt          string                        `json:"updated_at"`
	LaunchedAt         string                        `json:"launched_at,omitempty"`
	CompletedAt        string                        `json:"completed_at,omitempty"`
	Workers            []NetrunnerWaveWorkerSnapshot `json:"workers"`
}

type CreateNetrunnerWaveOutput struct {
	Status       string                        `json:"status"`
	WaveId       int                           `json:"wave_id"`
	BaseSha      string                        `json:"base_sha"`
	BaseBranch   string                        `json:"base_branch"`
	WorktreeRoot string                        `json:"worktree_root"`
	Workers      []NetrunnerWaveWorkerSnapshot `json:"workers"`
	Wave         NetrunnerWaveSnapshot         `json:"wave"`
}

type GetNetrunnerWaveOutput struct {
	Status string                `json:"status"`
	Wave   NetrunnerWaveSnapshot `json:"wave"`
}

type LaunchNetrunnerWaveOutput struct {
	Status              string                        `json:"status"`
	WaveId              int                           `json:"wave_id"`
	OrchestrationEpoch  int                           `json:"orchestration_epoch"`
	Workers             []NetrunnerWaveWorkerSnapshot `json:"workers"`
	Wave                NetrunnerWaveSnapshot         `json:"wave"`
	PartialFailure      bool                          `json:"partial_failure,omitempty"`
	PartialFailureError string                        `json:"partial_failure_error,omitempty"`
}

type NetrunnerWaveWaitResult struct {
	WaveId                int                           `json:"wave_id"`
	WaveStatus            string                        `json:"wave_status"`
	WinningSessionId      int                           `json:"winning_session_id,omitempty"`
	WorkerId              int                           `json:"worker_id,omitempty"`
	WorkerStatus          string                        `json:"worker_status,omitempty"`
	SessionStatus         string                        `json:"session_status,omitempty"`
	Backend               string                        `json:"backend,omitempty"`
	Model                 string                        `json:"model,omitempty"`
	Reasoning             string                        `json:"reasoning,omitempty"`
	ExternalSessionId     string                        `json:"external_session_id,omitempty"`
	CodexSessionId        string                        `json:"codex_session_id,omitempty"`
	Terminal              bool                          `json:"terminal"`
	TerminalCondition     string                        `json:"terminal_condition"`
	TimedOut              bool                          `json:"timed_out"`
	ElapsedSeconds        int                           `json:"elapsed_seconds"`
	TimeoutSeconds        int                           `json:"timeout_seconds"`
	PollIntervalSeconds   int                           `json:"poll_interval_seconds"`
	ReturnWhen            string                        `json:"return_when"`
	Report                string                        `json:"report,omitempty"`
	ProposalIds           []int                         `json:"proposal_ids"`
	BaseSha               string                        `json:"base_sha,omitempty"`
	HeadSha               string                        `json:"head_sha,omitempty"`
	ChangedPaths          []string                      `json:"changed_paths"`
	DiffPatchPath         string                        `json:"diff_patch_path,omitempty"`
	DiffStat              string                        `json:"diff_stat,omitempty"`
	WorktreePath          string                        `json:"worktree_path,omitempty"`
	FollowUpAllowed       bool                          `json:"follow_up_allowed"`
	FollowUpBlockedReason string                        `json:"follow_up_blocked_reason,omitempty"`
	LaunchEpoch           int                           `json:"launch_epoch,omitempty"`
	CurrentEpoch          int                           `json:"current_epoch"`
	OrchestrationFrozen   bool                          `json:"orchestration_frozen"`
	Workers               []NetrunnerWaveWorkerSnapshot `json:"workers"`
	Wave                  NetrunnerWaveSnapshot         `json:"wave"`
}

type WaitForNetrunnerWaveOutput struct {
	Status string                  `json:"status"`
	Result NetrunnerWaveWaitResult `json:"result"`
}

type NetrunnerWaveCleanupWorkerResult struct {
	WorkerId             int    `json:"worker_id"`
	SessionId            int    `json:"session_id"`
	WorkerStatus         string `json:"worker_status"`
	CleanupStatus        string `json:"cleanup_status"`
	RecordedWorktreePath string `json:"recorded_worktree_path"`
	ResolvedWorktreePath string `json:"resolved_worktree_path"`
	WorktreeListed       bool   `json:"worktree_listed"`
	WorktreeExists       bool   `json:"worktree_exists"`
	Removed              bool   `json:"removed"`
	Missing              bool   `json:"missing"`
	Skipped              bool   `json:"skipped"`
	Diagnostic           string `json:"diagnostic,omitempty"`
	Error                string `json:"error,omitempty"`
}

type CleanupNetrunnerWaveOutput struct {
	Status            string                             `json:"status"`
	WaveId            int                                `json:"wave_id"`
	WaveStatus        string                             `json:"wave_status"`
	RemoveWorktrees   bool                               `json:"remove_worktrees"`
	Prune             bool                               `json:"prune"`
	PruneRan          bool                               `json:"prune_ran"`
	Force             bool                               `json:"force"`
	Cleaned           bool                               `json:"cleaned"`
	Workers           []NetrunnerWaveCleanupWorkerResult `json:"workers"`
	OrphanDiagnostics []string                           `json:"orphan_diagnostics"`
	PruneOutput       string                             `json:"prune_output,omitempty"`
	Wave              NetrunnerWaveSnapshot              `json:"wave"`
}

func decodeParallelWaveStringList(raw string) []string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return []string{}
	}
	var values []string
	if err := json.Unmarshal([]byte(trimmed), &values); err != nil {
		return []string{}
	}
	return normalizeStringList(values)
}

func loadParallelWaveSessionCandidates(localSessionIDs []int, projectID int) ([]parallelWaveSessionCandidate, error) {
	if len(localSessionIDs) < 2 {
		return nil, fmt.Errorf("session_ids must contain at least two sessions")
	}

	candidates := make([]parallelWaveSessionCandidate, 0, len(localSessionIDs))
	for _, localSessionID := range localSessionIDs {
		globalSessionID, err := globalSessionIDFromProjectScoped(localSessionID, projectID)
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("session %d not found in current project", localSessionID)
		}
		if err != nil {
			return nil, fmt.Errorf("DB query error: %v", err)
		}

		state, err := fetchSessionLifecycleState(globalSessionID, projectID)
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("session %d not found in current project", localSessionID)
		}
		if err != nil {
			return nil, fmt.Errorf("failed to load session %d: %v", localSessionID, err)
		}
		if state.Status != "pending" {
			return nil, fmt.Errorf("session %d must be pending, got %q", localSessionID, state.Status)
		}
		if state.ReworkCount != 0 || state.ForcedStopCount != 0 {
			return nil, fmt.Errorf("session %d has rework/forced-stop history and must be forked or handled serially", localSessionID)
		}
		if len(state.DeclaredWriteScope) == 0 {
			return nil, fmt.Errorf("session %d must declare a non-empty write scope", localSessionID)
		}
		candidates = append(candidates, parallelWaveSessionCandidate{
			LocalSessionID:     localSessionID,
			GlobalSessionID:    globalSessionID,
			DeclaredWriteScope: state.DeclaredWriteScope,
		})
	}

	globalSessionIDs := make([]int, 0, len(candidates))
	for _, candidate := range candidates {
		globalSessionIDs = append(globalSessionIDs, candidate.GlobalSessionID)
	}
	activeProcesses, err := listRunningWorkerProcesses(projectID, globalSessionIDs)
	if err != nil {
		return nil, fmt.Errorf("DB query error: %v", err)
	}
	if len(activeProcesses) > 0 {
		localIDs := make([]int, 0, len(activeProcesses))
		for _, process := range activeProcesses {
			localID, mapErr := projectScopedSessionIDFromGlobal(process.SessionID, projectID)
			if mapErr != nil {
				return nil, fmt.Errorf("DB mapping error: %v", mapErr)
			}
			localIDs = append(localIDs, localID)
		}
		sort.Ints(localIDs)
		return nil, fmt.Errorf("selected sessions have active worker processes: %v", localIDs)
	}

	admissionWorkers := make([]parallelWaveAdmissionWorker, 0, len(candidates))
	for _, candidate := range candidates {
		admissionWorkers = append(admissionWorkers, parallelWaveAdmissionWorker{
			SessionID:          candidate.LocalSessionID,
			DeclaredWriteScope: candidate.DeclaredWriteScope,
		})
	}
	normalizedAdmission, err := normalizeParallelWaveAdmissionWorkers(admissionWorkers)
	if err != nil {
		return nil, err
	}
	scopeByLocalID := make(map[int][]string, len(normalizedAdmission))
	for _, worker := range normalizedAdmission {
		scopeByLocalID[worker.SessionID] = worker.DeclaredWriteScope
	}
	for index := range candidates {
		candidates[index].DeclaredWriteScope = scopeByLocalID[candidates[index].LocalSessionID]
	}
	return candidates, nil
}

func insertParallelWave(projectID int, projectCWD string, worktreeRoot string, baseSHA string, baseBranch string, orchestrationEpoch int, candidates []parallelWaveSessionCandidate) (int, error) {
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	result, err := tx.Exec(
		`INSERT INTO parallel_wave (project_id, status, base_sha, base_branch, project_cwd, worktree_root, orchestration_epoch, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
		projectID,
		parallelWaveStatusCreated,
		baseSHA,
		baseBranch,
		projectCWD,
		worktreeRoot,
		orchestrationEpoch,
	)
	if err != nil {
		return 0, err
	}
	insertID, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	waveID := int(insertID)
	waveIDText := strconv.Itoa(waveID)

	for _, candidate := range candidates {
		encodedScope, err := json.Marshal(candidate.DeclaredWriteScope)
		if err != nil {
			return 0, err
		}
		branchName, err := parallelWaveBranchName(waveID, candidate.LocalSessionID)
		if err != nil {
			return 0, err
		}
		worktreePath, err := parallelWaveWorktreePath(worktreeRoot, waveID, candidate.LocalSessionID)
		if err != nil {
			return 0, err
		}
		if _, err := tx.Exec(
			`INSERT INTO parallel_wave_worker (
				wave_id,
				project_id,
				session_id,
				status,
				declared_write_scope,
				branch_name,
				worktree_path,
				base_sha,
				updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
			waveID,
			projectID,
			candidate.GlobalSessionID,
			parallelWaveWorkerStatusCreated,
			string(encodedScope),
			branchName,
			worktreePath,
			baseSHA,
		); err != nil {
			return 0, err
		}
		if _, err := tx.Exec(
			`UPDATE session
			 SET parallel_wave_id = ?
			 WHERE id = ? AND project_id = ?`,
			waveIDText,
			candidate.GlobalSessionID,
			projectID,
		); err != nil {
			return 0, err
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return waveID, nil
}

func fetchNetrunnerWaveSnapshot(waveID int, projectID int) (NetrunnerWaveSnapshot, error) {
	if waveID <= 0 {
		return NetrunnerWaveSnapshot{}, sql.ErrNoRows
	}

	var (
		snapshot           NetrunnerWaveSnapshot
		createdBySessionID int
		launchedAt         string
		completedAt        string
	)
	err := db.QueryRow(
		`SELECT id,
		        project_id,
		        status,
		        base_sha,
		        COALESCE(base_branch, ''),
		        project_cwd,
		        worktree_root,
		        COALESCE(orchestration_epoch, 0),
		        COALESCE(created_by_session_id, 0),
		        COALESCE(failure_reason, ''),
		        created_at,
		        updated_at,
		        COALESCE(launched_at, ''),
		        COALESCE(completed_at, '')
		 FROM parallel_wave
		 WHERE id = ? AND project_id = ?`,
		waveID,
		projectID,
	).Scan(
		&snapshot.Id,
		&snapshot.ProjectId,
		&snapshot.Status,
		&snapshot.BaseSha,
		&snapshot.BaseBranch,
		&snapshot.ProjectCwd,
		&snapshot.WorktreeRoot,
		&snapshot.OrchestrationEpoch,
		&createdBySessionID,
		&snapshot.FailureReason,
		&snapshot.CreatedAt,
		&snapshot.UpdatedAt,
		&launchedAt,
		&completedAt,
	)
	if err != nil {
		return NetrunnerWaveSnapshot{}, err
	}
	snapshot.CreatedBySessionId = createdBySessionID
	snapshot.LaunchedAt = launchedAt
	snapshot.CompletedAt = completedAt

	rows, err := db.Query(
		`SELECT id,
		        wave_id,
		        project_id,
		        (
		          SELECT COUNT(*)
		          FROM session ranked
		          WHERE ranked.project_id = parallel_wave_worker.project_id
		            AND ranked.id <= parallel_wave_worker.session_id
		        ) AS local_session_id,
		        status,
		        declared_write_scope,
		        branch_name,
		        worktree_path,
		        base_sha,
		        COALESCE(head_sha, ''),
		        COALESCE(changed_paths, '[]'),
		        COALESCE(diff_patch_path, ''),
		        COALESCE(diff_stat, ''),
		        COALESCE(launch_epoch, 0),
		        COALESCE(worker_process_id, 0),
		        COALESCE(external_session_id, ''),
		        COALESCE(headless_log_path, ''),
		        COALESCE(launcher_log_path, ''),
		        COALESCE(worker_metadata_path, ''),
		        COALESCE(failure_reason, ''),
		        COALESCE(cleanup_status, 'pending'),
		        created_at,
		        updated_at,
		        COALESCE(launched_at, ''),
		        COALESCE(terminal_at, ''),
		        COALESCE(cleaned_at, '')
		 FROM parallel_wave_worker
		 WHERE wave_id = ? AND project_id = ?
		 ORDER BY session_id`,
		waveID,
		projectID,
	)
	if err != nil {
		return NetrunnerWaveSnapshot{}, err
	}
	defer rows.Close()

	workers := []NetrunnerWaveWorkerSnapshot{}
	for rows.Next() {
		var (
			worker         NetrunnerWaveWorkerSnapshot
			scopePayload   string
			changedPayload string
		)
		if err := rows.Scan(
			&worker.Id,
			&worker.WaveId,
			&worker.ProjectId,
			&worker.SessionId,
			&worker.Status,
			&scopePayload,
			&worker.BranchName,
			&worker.WorktreePath,
			&worker.BaseSha,
			&worker.HeadSha,
			&changedPayload,
			&worker.DiffPatchPath,
			&worker.DiffStat,
			&worker.LaunchEpoch,
			&worker.WorkerProcessId,
			&worker.ExternalSessionId,
			&worker.HeadlessLogPath,
			&worker.LauncherLogPath,
			&worker.WorkerMetadataPath,
			&worker.FailureReason,
			&worker.CleanupStatus,
			&worker.CreatedAt,
			&worker.UpdatedAt,
			&worker.LaunchedAt,
			&worker.TerminalAt,
			&worker.CleanedAt,
		); err != nil {
			return NetrunnerWaveSnapshot{}, err
		}
		worker.DeclaredWriteScope = decodeParallelWaveStringList(scopePayload)
		worker.ChangedPaths = decodeParallelWaveStringList(changedPayload)
		workers = append(workers, worker)
	}
	if err := rows.Err(); err != nil {
		return NetrunnerWaveSnapshot{}, err
	}
	snapshot.Workers = workers
	return snapshot, nil
}

func CreateNetrunnerWave(ctx context.Context, req *mcp.CallToolRequest, input CreateNetrunnerWaveInput) (*mcp.CallToolResult, CreateNetrunnerWaveOutput, error) {
	if authorizedRole != "fixer" {
		return &mcp.CallToolResult{IsError: true}, CreateNetrunnerWaveOutput{}, fmt.Errorf("access denied: requires fixer role")
	}
	if authorizedProjectId <= 0 {
		return &mcp.CallToolResult{IsError: true}, CreateNetrunnerWaveOutput{}, fmt.Errorf("access denied: fixer role is not bound to a project")
	}

	control, _, err := fetchOrchestrationControl(authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CreateNetrunnerWaveOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	if control.OrchestrationFrozen {
		return &mcp.CallToolResult{IsError: true}, CreateNetrunnerWaveOutput{}, fmt.Errorf("orchestration is frozen; resume orchestration before creating a parallel wave")
	}

	projectCWD, err := projectCWDFromID(authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CreateNetrunnerWaveOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	normalizedProjectCWD, err := normalizeProjectCWD(projectCWD)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CreateNetrunnerWaveOutput{}, err
	}
	worktreeRoot, err := normalizeParallelWaveWorktreeRoot(input.WorktreeRoot)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CreateNetrunnerWaveOutput{}, err
	}
	baseSHA, baseBranch, err := verifyParallelWaveGitBase(normalizedProjectCWD, input.BaseRef)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CreateNetrunnerWaveOutput{}, err
	}

	candidates, err := loadParallelWaveSessionCandidates(input.SessionIds, authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CreateNetrunnerWaveOutput{}, err
	}
	waveID, err := insertParallelWave(authorizedProjectId, normalizedProjectCWD, worktreeRoot, baseSHA, baseBranch, control.OrchestrationEpoch, candidates)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CreateNetrunnerWaveOutput{}, fmt.Errorf("DB insert error: %v", err)
	}

	wave, err := fetchNetrunnerWaveSnapshot(waveID, authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CreateNetrunnerWaveOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	return nil, CreateNetrunnerWaveOutput{
		Status:       "success",
		WaveId:       wave.Id,
		BaseSha:      wave.BaseSha,
		BaseBranch:   wave.BaseBranch,
		WorktreeRoot: wave.WorktreeRoot,
		Workers:      wave.Workers,
		Wave:         wave,
	}, nil
}

func GetNetrunnerWave(ctx context.Context, req *mcp.CallToolRequest, input GetNetrunnerWaveInput) (*mcp.CallToolResult, GetNetrunnerWaveOutput, error) {
	if authorizedRole != "fixer" {
		return &mcp.CallToolResult{IsError: true}, GetNetrunnerWaveOutput{}, fmt.Errorf("access denied: requires fixer role")
	}
	if authorizedProjectId <= 0 {
		return &mcp.CallToolResult{IsError: true}, GetNetrunnerWaveOutput{}, fmt.Errorf("access denied: fixer role is not bound to a project")
	}

	wave, err := fetchNetrunnerWaveSnapshot(input.WaveId, authorizedProjectId)
	if err == sql.ErrNoRows {
		return &mcp.CallToolResult{IsError: true}, GetNetrunnerWaveOutput{}, fmt.Errorf("wave %d not found in current project", input.WaveId)
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetNetrunnerWaveOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	return nil, GetNetrunnerWaveOutput{Status: "success", Wave: wave}, nil
}

func parallelWaveLaunchStartupTimeoutSeconds(raw int) (time.Duration, error) {
	if raw <= 0 {
		raw = defaultParallelWaveLaunchStartupWait
	}
	if raw > maxParallelWaveLaunchStartupWait {
		return 0, fmt.Errorf("timeout_seconds must be <= %d", maxParallelWaveLaunchStartupWait)
	}
	return time.Duration(raw) * time.Second, nil
}

func parallelWaveHasLaunchedWorkers(wave NetrunnerWaveSnapshot) bool {
	for _, worker := range wave.Workers {
		if worker.LaunchEpoch != 0 || worker.WorkerProcessId != 0 || strings.TrimSpace(worker.ExternalSessionId) != "" {
			return true
		}
		switch worker.Status {
		case parallelWaveWorkerStatusCreated, parallelWaveWorkerStatusStopped:
		default:
			return true
		}
	}
	return false
}

func validateParallelWaveLaunchState(wave NetrunnerWaveSnapshot) error {
	switch wave.Status {
	case parallelWaveStatusCreated:
		return nil
	case parallelWaveStatusStopped:
		if !parallelWaveHasLaunchedWorkers(wave) {
			return nil
		}
		return fmt.Errorf("wave %d is stopped but has launched worker state and cannot be safely relaunched", wave.Id)
	default:
		return fmt.Errorf("wave %d must be %q before launch, got %q", wave.Id, parallelWaveStatusCreated, wave.Status)
	}
}

func waveWorkerLaunchArtifacts(projectCWD string, waveID int, localSessionID int, backend string) (string, string, string, error) {
	logDir := filepath.Join(projectCWD, ".codex", "netrunner_wave_artifacts", fmt.Sprintf("wave-%d", waveID), fmt.Sprintf("session-%d", localSessionID))
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return "", "", "", fmt.Errorf("failed to prepare wave worker artifact dir: %v", err)
	}
	suffix := strconv.FormatInt(time.Now().Unix(), 10)
	headlessLogPath := filepath.Join(logDir, fmt.Sprintf("headless-%s-%s.log", backend, suffix))
	launcherLogPath := filepath.Join(logDir, fmt.Sprintf("launcher-%s.log", suffix))
	metadataPath := filepath.Join(logDir, fmt.Sprintf("worker_metadata-%s.json", suffix))
	return headlessLogPath, launcherLogPath, metadataPath, nil
}

func updateParallelWaveStatus(waveID int, projectID int, status string, failureReason string, markLaunched bool) error {
	if markLaunched {
		_, err := db.Exec(
			`UPDATE parallel_wave
			 SET status = ?,
			     failure_reason = ?,
			     launched_at = COALESCE(launched_at, CURRENT_TIMESTAMP),
			     updated_at = CURRENT_TIMESTAMP
			 WHERE id = ? AND project_id = ?`,
			status,
			failureReason,
			waveID,
			projectID,
		)
		return err
	}
	_, err := db.Exec(
		`UPDATE parallel_wave
		 SET status = ?,
		     failure_reason = ?,
		     updated_at = CURRENT_TIMESTAMP
		 WHERE id = ? AND project_id = ?`,
		status,
		failureReason,
		waveID,
		projectID,
	)
	return err
}

func updateParallelWaveWorkerStatus(waveWorkerID int, projectID int, status string, failureReason string) error {
	_, err := db.Exec(
		`UPDATE parallel_wave_worker
		 SET status = ?,
		     failure_reason = ?,
		     updated_at = CURRENT_TIMESTAMP
		 WHERE id = ? AND project_id = ?`,
		status,
		failureReason,
		waveWorkerID,
		projectID,
	)
	return err
}

func updateParallelWaveWorkerLaunch(
	waveWorkerID int,
	projectID int,
	status string,
	launchEpoch int,
	workerProcessID int,
	externalSessionID string,
	headlessLogPath string,
	launcherLogPath string,
	metadataPath string,
) error {
	_, err := db.Exec(
		`UPDATE parallel_wave_worker
		 SET status = ?,
		     launch_epoch = ?,
		     worker_process_id = ?,
		     external_session_id = ?,
		     headless_log_path = ?,
		     launcher_log_path = ?,
		     worker_metadata_path = ?,
		     failure_reason = '',
		     launched_at = COALESCE(launched_at, CURRENT_TIMESTAMP),
		     updated_at = CURRENT_TIMESTAMP
		 WHERE id = ? AND project_id = ?`,
		status,
		launchEpoch,
		workerProcessID,
		externalSessionID,
		headlessLogPath,
		launcherLogPath,
		metadataPath,
		waveWorkerID,
		projectID,
	)
	return err
}

func parallelWaveWaitReturnWhen(raw string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	if normalized == "" {
		return parallelWaveWaitFirstReviewReady, nil
	}
	switch normalized {
	case parallelWaveWaitFirstReviewReady, parallelWaveWaitAllTerminal:
		return normalized, nil
	default:
		return "", fmt.Errorf("unsupported return_when %q; supported values are %q and %q", raw, parallelWaveWaitFirstReviewReady, parallelWaveWaitAllTerminal)
	}
}

func validateParallelWaveWaitState(wave NetrunnerWaveSnapshot) error {
	if len(wave.Workers) == 0 {
		return fmt.Errorf("wave %d has no workers to wait on", wave.Id)
	}
	switch wave.Status {
	case parallelWaveStatusLaunching,
		parallelWaveStatusRunning,
		parallelWaveStatusReviewReady,
		parallelWaveStatusPartiallyFailed,
		parallelWaveStatusCompleted,
		parallelWaveStatusFailed:
		return nil
	case parallelWaveStatusCreated:
		return fmt.Errorf("wave %d has not been launched", wave.Id)
	default:
		return fmt.Errorf("wave %d is not waitable in status %q", wave.Id, wave.Status)
	}
}

func parallelWaveWorkerTerminalCondition(status string) (string, bool) {
	switch status {
	case parallelWaveWorkerStatusReviewReady:
		return "review_ready", true
	case parallelWaveWorkerStatusCompleted:
		return "completed", true
	case parallelWaveWorkerStatusFailed:
		return "failed", true
	case parallelWaveWorkerStatusStopped:
		return "stopped", true
	case parallelWaveWorkerStatusStaleEpoch:
		return "stale_epoch", true
	case parallelWaveWorkerStatusCleaned:
		return "cleaned", true
	default:
		return "", false
	}
}

func updateParallelWaveWorkerTerminal(
	waveWorkerID int,
	projectID int,
	status string,
	failureReason string,
	headSHA string,
	changedPaths []string,
	diffPatchPath string,
	diffStat string,
) error {
	if changedPaths == nil {
		changedPaths = []string{}
	}
	changedPayload, err := json.Marshal(changedPaths)
	if err != nil {
		return err
	}
	_, err = db.Exec(
		`UPDATE parallel_wave_worker
		 SET status = ?,
		     failure_reason = ?,
		     head_sha = ?,
		     changed_paths = ?,
		     diff_patch_path = ?,
		     diff_stat = ?,
		     terminal_at = COALESCE(terminal_at, CURRENT_TIMESTAMP),
		     updated_at = CURRENT_TIMESTAMP
		 WHERE id = ? AND project_id = ?`,
		status,
		failureReason,
		headSHA,
		string(changedPayload),
		diffPatchPath,
		diffStat,
		waveWorkerID,
		projectID,
	)
	return err
}

func finalizeParallelWaveWorker(projectCWD string, wave NetrunnerWaveSnapshot, worker NetrunnerWaveWorkerSnapshot, status string, failureReason string) (NetrunnerWaveWorkerSnapshot, error) {
	headSHA, changedPaths, patchPath, diffStat, captureErr := captureParallelWaveWorkerDiff(projectCWD, wave, worker)
	if captureErr != nil {
		if strings.TrimSpace(failureReason) == "" {
			failureReason = captureErr.Error()
		} else {
			failureReason = strings.TrimSpace(failureReason) + "; diff capture failed: " + captureErr.Error()
		}
		status = parallelWaveWorkerStatusFailed
		headSHA = ""
		changedPaths = []string{}
		patchPath = ""
		diffStat = ""
	}
	if err := updateParallelWaveWorkerTerminal(worker.Id, authorizedProjectId, status, failureReason, headSHA, changedPaths, patchPath, diffStat); err != nil {
		return NetrunnerWaveWorkerSnapshot{}, err
	}

	updatedWave, err := fetchNetrunnerWaveSnapshot(wave.Id, authorizedProjectId)
	if err != nil {
		return NetrunnerWaveWorkerSnapshot{}, err
	}
	for _, updatedWorker := range updatedWave.Workers {
		if updatedWorker.Id == worker.Id {
			return updatedWorker, nil
		}
	}
	return NetrunnerWaveWorkerSnapshot{}, fmt.Errorf("worker %d not found after terminal update", worker.SessionId)
}

func fetchWorkerProcessByID(processID int, projectID int) (workerProcessSnapshot, bool, error) {
	if processID <= 0 {
		return workerProcessSnapshot{}, false, nil
	}
	var row workerProcessSnapshot
	err := db.QueryRow(
		`SELECT id,
		        session_id,
		        pid,
		        launch_epoch,
		        status,
		        started_at,
		        updated_at,
		        COALESCE(stopped_at, ''),
		        COALESCE(stop_reason, '')
		 FROM worker_process
		 WHERE id = ? AND project_id = ?`,
		processID,
		projectID,
	).Scan(&row.ID, &row.SessionID, &row.PID, &row.LaunchEpoch, &row.Status, &row.StartedAt, &row.UpdatedAt, &row.StoppedAt, &row.StopReason)
	if err == sql.ErrNoRows {
		return workerProcessSnapshot{}, false, nil
	}
	if err != nil {
		return workerProcessSnapshot{}, false, err
	}
	row.Alive = isProcessAlive(row.PID)
	if row.Status == workerStatusRunning && !row.Alive {
		if _, err := db.Exec(
			`UPDATE worker_process
			 SET status = ?, stop_reason = ?, stopped_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
			 WHERE id = ? AND project_id = ?`,
			workerStatusExited,
			"process exited",
			row.ID,
			projectID,
		); err != nil {
			return workerProcessSnapshot{}, false, err
		}
		row.Status = workerStatusExited
		row.StopReason = "process exited"
	}
	return row, true, nil
}

func firstParallelWaveFailureReason(workers []NetrunnerWaveWorkerSnapshot) string {
	for _, worker := range workers {
		if strings.TrimSpace(worker.FailureReason) != "" {
			return worker.FailureReason
		}
	}
	return ""
}

func refreshParallelWaveAggregateStatus(waveID int, projectID int) error {
	wave, err := fetchNetrunnerWaveSnapshot(waveID, projectID)
	if err != nil {
		return err
	}
	if len(wave.Workers) == 0 {
		return nil
	}

	allCompleted := true
	allTerminal := true
	hasReviewReady := false
	hasFailed := false
	hasActive := false
	for _, worker := range wave.Workers {
		switch worker.Status {
		case parallelWaveWorkerStatusReviewReady:
			hasReviewReady = true
			allCompleted = false
		case parallelWaveWorkerStatusCompleted:
		case parallelWaveWorkerStatusFailed, parallelWaveWorkerStatusStaleEpoch, parallelWaveWorkerStatusStopped:
			hasFailed = true
			allCompleted = false
		default:
			allCompleted = false
			allTerminal = false
			hasActive = true
		}
	}

	status := parallelWaveStatusRunning
	failureReason := ""
	switch {
	case allCompleted:
		status = parallelWaveStatusCompleted
	case hasReviewReady:
		status = parallelWaveStatusReviewReady
	case hasFailed && allTerminal:
		status = parallelWaveStatusFailed
		failureReason = firstParallelWaveFailureReason(wave.Workers)
	case hasFailed && hasActive:
		status = parallelWaveStatusPartiallyFailed
		failureReason = firstParallelWaveFailureReason(wave.Workers)
	case hasFailed:
		status = parallelWaveStatusPartiallyFailed
		failureReason = firstParallelWaveFailureReason(wave.Workers)
	}

	if status == parallelWaveStatusCompleted {
		_, err = db.Exec(
			`UPDATE parallel_wave
			 SET status = ?,
			     failure_reason = '',
			     completed_at = COALESCE(completed_at, CURRENT_TIMESTAMP),
			     updated_at = CURRENT_TIMESTAMP
			 WHERE id = ? AND project_id = ?`,
			status,
			waveID,
			projectID,
		)
		return err
	}
	return updateParallelWaveStatus(waveID, projectID, status, failureReason, false)
}

type parallelWaveWaitCandidate struct {
	Worker            NetrunnerWaveWorkerSnapshot
	GlobalSessionID   int
	SessionStatus     string
	Report            string
	ProposalIDs       []int
	Backend           string
	Model             string
	Reasoning         string
	ExternalSessionID string
	CodexSessionID    string
	TerminalCondition string
}

func inspectParallelWaveWorkerForWait(projectCWD string, wave NetrunnerWaveSnapshot, worker NetrunnerWaveWorkerSnapshot) (parallelWaveWaitCandidate, bool, error) {
	candidate := parallelWaveWaitCandidate{Worker: worker}
	globalSessionID, err := globalSessionIDFromProjectScoped(worker.SessionId, authorizedProjectId)
	if err == sql.ErrNoRows {
		updatedWorker, updateErr := finalizeParallelWaveWorker(projectCWD, wave, worker, parallelWaveWorkerStatusFailed, fmt.Sprintf("session %d not found in current project", worker.SessionId))
		if updateErr != nil {
			return parallelWaveWaitCandidate{}, false, updateErr
		}
		candidate.Worker = updatedWorker
		candidate.TerminalCondition = "failed"
		return candidate, true, nil
	}
	if err != nil {
		return parallelWaveWaitCandidate{}, false, err
	}
	candidate.GlobalSessionID = globalSessionID

	status, report, proposalIDs, backend, model, reasoning, externalSessionID, err := fetchSessionWaitSnapshot(globalSessionID, authorizedProjectId)
	if err != nil {
		return parallelWaveWaitCandidate{}, false, err
	}
	candidate.SessionStatus = status
	candidate.Report = report
	candidate.ProposalIDs = proposalIDs
	candidate.Backend = backend
	candidate.Model = model
	candidate.Reasoning = reasoning
	candidate.ExternalSessionID = externalSessionID
	if backend == defaultCliBackend {
		candidate.CodexSessionID = externalSessionID
	}

	if reason := malformedReviewSnapshotReason(worker.SessionId, status, report, proposalIDs); reason != "" {
		updatedWorker, err := finalizeParallelWaveWorker(projectCWD, wave, worker, parallelWaveWorkerStatusFailed, reason)
		if err != nil {
			return parallelWaveWaitCandidate{}, false, err
		}
		candidate.Worker = updatedWorker
		candidate.TerminalCondition = "failed"
		return candidate, true, nil
	}

	if terminalCondition, terminal := parallelWaveWorkerTerminalCondition(worker.Status); terminal {
		candidate.TerminalCondition = terminalCondition
		return candidate, true, nil
	}

	if status == "review" || status == "completed" {
		targetStatus := parallelWaveWorkerStatusReviewReady
		terminalCondition := "review_ready"
		if status == "completed" {
			targetStatus = parallelWaveWorkerStatusCompleted
			terminalCondition = "completed"
		}
		updatedWorker, err := finalizeParallelWaveWorker(projectCWD, wave, worker, targetStatus, "")
		if err != nil {
			return parallelWaveWaitCandidate{}, false, err
		}
		candidate.Worker = updatedWorker
		if updatedWorker.Status == parallelWaveWorkerStatusFailed {
			terminalCondition = "failed"
		}
		candidate.TerminalCondition = terminalCondition
		return candidate, true, nil
	}

	worktreePath, pathErr := resolveParallelWaveWorktreePath(projectCWD, worker.WorktreePath)
	if pathErr != nil {
		updatedWorker, err := finalizeParallelWaveWorker(projectCWD, wave, worker, parallelWaveWorkerStatusFailed, pathErr.Error())
		if err != nil {
			return parallelWaveWaitCandidate{}, false, err
		}
		candidate.Worker = updatedWorker
		candidate.TerminalCondition = "failed"
		return candidate, true, nil
	}
	if info, statErr := os.Stat(worktreePath); statErr != nil {
		reason := fmt.Sprintf("worker worktree missing: %s", worktreePath)
		if !os.IsNotExist(statErr) {
			reason = fmt.Sprintf("failed to inspect worker worktree %s: %v", worktreePath, statErr)
		}
		updatedWorker, err := finalizeParallelWaveWorker(projectCWD, wave, worker, parallelWaveWorkerStatusFailed, reason)
		if err != nil {
			return parallelWaveWaitCandidate{}, false, err
		}
		candidate.Worker = updatedWorker
		candidate.TerminalCondition = "failed"
		return candidate, true, nil
	} else if !info.IsDir() {
		updatedWorker, err := finalizeParallelWaveWorker(projectCWD, wave, worker, parallelWaveWorkerStatusFailed, fmt.Sprintf("worker worktree is not a directory: %s", worktreePath))
		if err != nil {
			return parallelWaveWaitCandidate{}, false, err
		}
		candidate.Worker = updatedWorker
		candidate.TerminalCondition = "failed"
		return candidate, true, nil
	}

	if worker.WorkerProcessId <= 0 {
		updatedWorker, err := finalizeParallelWaveWorker(projectCWD, wave, worker, parallelWaveWorkerStatusFailed, "worker process linkage missing")
		if err != nil {
			return parallelWaveWaitCandidate{}, false, err
		}
		candidate.Worker = updatedWorker
		candidate.TerminalCondition = "failed"
		return candidate, true, nil
	}
	processRow, found, err := fetchWorkerProcessByID(worker.WorkerProcessId, authorizedProjectId)
	if err != nil {
		return parallelWaveWaitCandidate{}, false, err
	}
	if !found {
		updatedWorker, err := finalizeParallelWaveWorker(projectCWD, wave, worker, parallelWaveWorkerStatusFailed, fmt.Sprintf("worker process %d not found", worker.WorkerProcessId))
		if err != nil {
			return parallelWaveWaitCandidate{}, false, err
		}
		candidate.Worker = updatedWorker
		candidate.TerminalCondition = "failed"
		return candidate, true, nil
	}
	if processRow.Status != workerStatusRunning || !processRow.Alive {
		reason := strings.TrimSpace(processRow.StopReason)
		if reason == "" {
			reason = fmt.Sprintf("worker process %d is not running", processRow.PID)
		}
		updatedWorker, err := finalizeParallelWaveWorker(projectCWD, wave, worker, parallelWaveWorkerStatusFailed, reason)
		if err != nil {
			return parallelWaveWaitCandidate{}, false, err
		}
		candidate.Worker = updatedWorker
		candidate.TerminalCondition = "failed"
		return candidate, true, nil
	}

	return candidate, false, nil
}

func markActiveParallelWaveWorkersStale(projectCWD string, wave NetrunnerWaveSnapshot, reason string) error {
	for _, worker := range wave.Workers {
		if _, terminal := parallelWaveWorkerTerminalCondition(worker.Status); terminal {
			continue
		}
		if _, err := finalizeParallelWaveWorker(projectCWD, wave, worker, parallelWaveWorkerStatusStaleEpoch, reason); err != nil {
			return err
		}
	}
	return nil
}

func rollbackParallelWaveWorktrees(projectCWD string, worktreePaths []string) []string {
	failures := []string{}
	for index := len(worktreePaths) - 1; index >= 0; index-- {
		spec, err := gitWorktreeRemoveCommand(projectCWD, worktreePaths[index], true)
		if err != nil {
			failures = append(failures, err.Error())
			continue
		}
		if _, err := runGitCommandSpec(spec); err != nil {
			failures = append(failures, err.Error())
		}
	}
	return failures
}

func validateParallelWaveGitStillAtBase(projectCWD string, expectedBaseSHA string) error {
	baseSHA, _, err := verifyParallelWaveGitBase(projectCWD, expectedBaseSHA)
	if err != nil {
		return err
	}
	if strings.TrimSpace(baseSHA) != strings.TrimSpace(expectedBaseSHA) {
		return fmt.Errorf("parallel wave base SHA changed: stored %q current %q", expectedBaseSHA, baseSHA)
	}
	return nil
}

func ensureParallelWaveWorkerPathsAvailable(projectCWD string, wave NetrunnerWaveSnapshot) (map[int]string, error) {
	worktreePathByWorkerID := make(map[int]string, len(wave.Workers))
	for _, worker := range wave.Workers {
		branchName, err := validateParallelWaveBranchName(worker.BranchName)
		if err != nil {
			return nil, fmt.Errorf("worker %d: %w", worker.SessionId, err)
		}
		exists, err := gitBranchExists(projectCWD, branchName)
		if err != nil {
			return nil, fmt.Errorf("worker %d: failed to inspect branch: %w", worker.SessionId, err)
		}
		if exists {
			return nil, fmt.Errorf("worker %d branch already exists: %s", worker.SessionId, branchName)
		}

		worktreePath, err := resolveParallelWaveWorktreePath(projectCWD, worker.WorktreePath)
		if err != nil {
			return nil, fmt.Errorf("worker %d: %w", worker.SessionId, err)
		}
		if _, statErr := os.Stat(worktreePath); statErr == nil {
			return nil, fmt.Errorf("worker %d worktree path already exists: %s", worker.SessionId, worktreePath)
		} else if !os.IsNotExist(statErr) {
			return nil, fmt.Errorf("worker %d failed to inspect worktree path %s: %v", worker.SessionId, worktreePath, statErr)
		}
		worktreePathByWorkerID[worker.Id] = worktreePath
	}
	return worktreePathByWorkerID, nil
}

func createParallelWaveWorktrees(projectCWD string, wave NetrunnerWaveSnapshot, worktreePathByWorkerID map[int]string) ([]string, error) {
	createdWorktrees := []string{}
	for _, worker := range wave.Workers {
		worktreePath := worktreePathByWorkerID[worker.Id]
		if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
			return createdWorktrees, fmt.Errorf("worker %d failed to prepare worktree parent: %v", worker.SessionId, err)
		}
		spec, err := gitWorktreeAddCommand(projectCWD, worktreePath, worker.BranchName, wave.BaseSha)
		if err != nil {
			return createdWorktrees, fmt.Errorf("worker %d: %w", worker.SessionId, err)
		}
		if _, err := runGitCommandSpec(spec); err != nil {
			return createdWorktrees, fmt.Errorf("worker %d failed to create worktree: %w", worker.SessionId, err)
		}
		createdWorktrees = append(createdWorktrees, worktreePath)
		if err := updateParallelWaveWorkerStatus(worker.Id, authorizedProjectId, parallelWaveWorkerStatusWorktreeReady, ""); err != nil {
			return createdWorktrees, fmt.Errorf("DB update error: %v", err)
		}
	}
	return createdWorktrees, nil
}

func launchParallelWaveWorkerProcess(
	ctx context.Context,
	projectCWD string,
	wave NetrunnerWaveSnapshot,
	worker NetrunnerWaveWorkerSnapshot,
	worktreePath string,
	input LaunchNetrunnerWaveInput,
	launchEpoch int,
	startupTimeout time.Duration,
) error {
	globalSessionID, err := globalSessionIDFromProjectScoped(worker.SessionId, authorizedProjectId)
	if err == sql.ErrNoRows {
		return fmt.Errorf("session %d not found in current project", worker.SessionId)
	}
	if err != nil {
		return fmt.Errorf("DB query error: %v", err)
	}

	launchConfig, err := resolveSessionLaunchConfig(globalSessionID, authorizedProjectId, input.Backend, input.Model, input.Reasoning)
	if err != nil {
		return fmt.Errorf("failed to resolve session launch backend: %v", err)
	}
	launcherScript, err := resolveExplicitLauncherScript()
	if err != nil {
		return err
	}
	headlessLogPath, launcherLogPath, metadataPath, err := waveWorkerLaunchArtifacts(projectCWD, wave.Id, worker.SessionId, launchConfig.Backend)
	if err != nil {
		return err
	}

	commandArgs := []string{
		launcherScript,
		"launch-wave-worker",
		"--project-cwd", projectCWD,
		"--worker-cwd", worktreePath,
		"--session-id", strconv.Itoa(worker.SessionId),
		"--wave-id", strconv.Itoa(wave.Id),
		"--wave-worker-id", strconv.Itoa(worker.Id),
		"--branch-name", worker.BranchName,
	}
	for _, scopeEntry := range worker.DeclaredWriteScope {
		commandArgs = append(commandArgs, "--declared-write-scope", scopeEntry)
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
	)

	if err := updateParallelWaveWorkerStatus(worker.Id, authorizedProjectId, parallelWaveWorkerStatusLaunching, ""); err != nil {
		return fmt.Errorf("DB update error: %v", err)
	}

	command := execCommand("python3", commandArgs...)
	commandEnv, envErr := resolveRuntimeLaunchEnv(projectCWD, os.Environ())
	if envErr != nil {
		log.Printf("warning: failed to resolve runtime launch env for %s: %v", projectCWD, envErr)
		commandEnv = os.Environ()
	}
	command.Env = commandEnv
	launcherLogHandle, err := os.OpenFile(launcherLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("failed to open wave launcher diagnostic log: %v", err)
	}
	defer launcherLogHandle.Close()
	command.Stdout = launcherLogHandle
	command.Stderr = launcherLogHandle
	if err := command.Start(); err != nil {
		return fmt.Errorf("failed to launch wave netrunner worker %d: %v", worker.SessionId, err)
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
			return fmt.Errorf(
				"wave netrunner launcher exited before startup completed for session %d: %v\nlauncher log: %s\nheadless log: %s",
				worker.SessionId,
				waitErr,
				launcherLogPath,
				headlessLogPath,
			)
		}
	case <-time.After(explicitLauncherExitGracePeriod):
	case <-ctx.Done():
		return ctx.Err()
	}

	workerPID := launcherPID
	if metadata, metadataErr := readExplicitLaunchWorkerMetadata(metadataPath); metadataErr == nil {
		workerPID = metadata.WorkerPID
		if strings.TrimSpace(metadata.HeadlessLogPath) != "" {
			headlessLogPath = strings.TrimSpace(metadata.HeadlessLogPath)
		}
	}
	if workerPID <= 0 {
		return fmt.Errorf("wave worker %d did not report a worker pid", worker.SessionId)
	}
	workerProcessID, err := recordWaveWorkerProcessLaunch(authorizedProjectId, globalSessionID, workerPID, launchEpoch, wave.Id, worker.Id)
	if err != nil {
		return fmt.Errorf("failed to persist wave worker process metadata: %v", err)
	}

	externalSessionID, err := waitForSessionExternalID(ctx, globalSessionID, launchConfig.Backend, startupTimeout)
	if err != nil {
		return fmt.Errorf("failed while waiting for backend session metadata: %v", err)
	}
	if err := updateParallelWaveWorkerLaunch(
		worker.Id,
		authorizedProjectId,
		parallelWaveWorkerStatusRunning,
		launchEpoch,
		workerProcessID,
		externalSessionID,
		headlessLogPath,
		launcherLogPath,
		metadataPath,
	); err != nil {
		return fmt.Errorf("DB update error: %v", err)
	}
	return nil
}

func LaunchNetrunnerWave(ctx context.Context, req *mcp.CallToolRequest, input LaunchNetrunnerWaveInput) (*mcp.CallToolResult, LaunchNetrunnerWaveOutput, error) {
	if authorizedRole != "fixer" {
		return &mcp.CallToolResult{IsError: true}, LaunchNetrunnerWaveOutput{}, fmt.Errorf("access denied: requires fixer role")
	}
	if authorizedProjectId <= 0 {
		return &mcp.CallToolResult{IsError: true}, LaunchNetrunnerWaveOutput{}, fmt.Errorf("access denied: fixer role is not bound to a project")
	}
	startupTimeout, err := parallelWaveLaunchStartupTimeoutSeconds(input.TimeoutSeconds)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, LaunchNetrunnerWaveOutput{}, err
	}

	wave, err := fetchNetrunnerWaveSnapshot(input.WaveId, authorizedProjectId)
	if err == sql.ErrNoRows {
		return &mcp.CallToolResult{IsError: true}, LaunchNetrunnerWaveOutput{}, fmt.Errorf("wave %d not found in current project", input.WaveId)
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, LaunchNetrunnerWaveOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	if err := validateParallelWaveLaunchState(wave); err != nil {
		return &mcp.CallToolResult{IsError: true}, LaunchNetrunnerWaveOutput{}, err
	}
	if len(wave.Workers) == 0 {
		return &mcp.CallToolResult{IsError: true}, LaunchNetrunnerWaveOutput{}, fmt.Errorf("wave %d has no workers to launch", wave.Id)
	}

	control, _, err := fetchOrchestrationControl(authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, LaunchNetrunnerWaveOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	if control.OrchestrationFrozen {
		return &mcp.CallToolResult{IsError: true}, LaunchNetrunnerWaveOutput{}, fmt.Errorf("orchestration is frozen; resume orchestration before launching a parallel wave")
	}
	if control.OrchestrationEpoch != wave.OrchestrationEpoch {
		return &mcp.CallToolResult{IsError: true}, LaunchNetrunnerWaveOutput{}, fmt.Errorf("wave %d has stale orchestration epoch %d; current epoch is %d", wave.Id, wave.OrchestrationEpoch, control.OrchestrationEpoch)
	}

	projectCWD, err := projectCWDFromID(authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, LaunchNetrunnerWaveOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	normalizedProjectCWD, err := normalizeProjectCWD(projectCWD)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, LaunchNetrunnerWaveOutput{}, err
	}
	storedProjectCWD, err := normalizeProjectCWD(wave.ProjectCwd)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, LaunchNetrunnerWaveOutput{}, fmt.Errorf("stored wave project cwd is invalid: %v", err)
	}
	if storedProjectCWD != normalizedProjectCWD {
		return &mcp.CallToolResult{IsError: true}, LaunchNetrunnerWaveOutput{}, fmt.Errorf("wave %d belongs to project cwd %q, current project cwd is %q", wave.Id, storedProjectCWD, normalizedProjectCWD)
	}
	if err := validateParallelWaveGitStillAtBase(normalizedProjectCWD, wave.BaseSha); err != nil {
		return &mcp.CallToolResult{IsError: true}, LaunchNetrunnerWaveOutput{}, err
	}
	worktreePathByWorkerID, err := ensureParallelWaveWorkerPathsAvailable(normalizedProjectCWD, wave)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, LaunchNetrunnerWaveOutput{}, err
	}

	if err := updateParallelWaveStatus(wave.Id, authorizedProjectId, parallelWaveStatusLaunching, "", false); err != nil {
		return &mcp.CallToolResult{IsError: true}, LaunchNetrunnerWaveOutput{}, fmt.Errorf("DB update error: %v", err)
	}
	createdWorktrees, err := createParallelWaveWorktrees(normalizedProjectCWD, wave, worktreePathByWorkerID)
	if err != nil {
		rollbackFailures := rollbackParallelWaveWorktrees(normalizedProjectCWD, createdWorktrees)
		reason := err.Error()
		if len(rollbackFailures) > 0 {
			reason += "; rollback failures: " + strings.Join(rollbackFailures, "; ")
		}
		_ = updateParallelWaveStatus(wave.Id, authorizedProjectId, parallelWaveStatusFailed, reason, false)
		return &mcp.CallToolResult{IsError: true}, LaunchNetrunnerWaveOutput{}, errors.New(reason)
	}

	launchedWorkers := 0
	for _, worker := range wave.Workers {
		worktreePath := worktreePathByWorkerID[worker.Id]
		if err := launchParallelWaveWorkerProcess(ctx, normalizedProjectCWD, wave, worker, worktreePath, input, control.OrchestrationEpoch, startupTimeout); err != nil {
			status := parallelWaveStatusFailed
			if launchedWorkers > 0 {
				status = parallelWaveStatusPartiallyFailed
			}
			_ = updateParallelWaveWorkerStatus(worker.Id, authorizedProjectId, parallelWaveWorkerStatusFailed, err.Error())
			_ = updateParallelWaveStatus(wave.Id, authorizedProjectId, status, err.Error(), launchedWorkers > 0)
			partialWave, fetchErr := fetchNetrunnerWaveSnapshot(wave.Id, authorizedProjectId)
			if fetchErr != nil {
				return &mcp.CallToolResult{IsError: true}, LaunchNetrunnerWaveOutput{}, err
			}
			return &mcp.CallToolResult{IsError: true}, LaunchNetrunnerWaveOutput{
				Status:              status,
				WaveId:              wave.Id,
				OrchestrationEpoch:  control.OrchestrationEpoch,
				Workers:             partialWave.Workers,
				Wave:                partialWave,
				PartialFailure:      launchedWorkers > 0,
				PartialFailureError: err.Error(),
			}, err
		}
		launchedWorkers++
	}

	if err := updateParallelWaveStatus(wave.Id, authorizedProjectId, parallelWaveStatusRunning, "", true); err != nil {
		return &mcp.CallToolResult{IsError: true}, LaunchNetrunnerWaveOutput{}, fmt.Errorf("DB update error: %v", err)
	}
	launchedWave, err := fetchNetrunnerWaveSnapshot(wave.Id, authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, LaunchNetrunnerWaveOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	return nil, LaunchNetrunnerWaveOutput{
		Status:             "success",
		WaveId:             launchedWave.Id,
		OrchestrationEpoch: control.OrchestrationEpoch,
		Workers:            launchedWave.Workers,
		Wave:               launchedWave,
	}, nil
}

func buildParallelWaveWaitResult(
	startedAt time.Time,
	timeoutSeconds int,
	pollIntervalSeconds int,
	returnWhen string,
	wave NetrunnerWaveSnapshot,
	winner *parallelWaveWaitCandidate,
	terminal bool,
	terminalCondition string,
	timedOut bool,
	followUpAllowed bool,
	followUpBlockedReason string,
	control orchestrationControl,
) NetrunnerWaveWaitResult {
	proposalIDs := []int{}
	changedPaths := []string{}
	result := NetrunnerWaveWaitResult{
		WaveId:                wave.Id,
		WaveStatus:            wave.Status,
		Terminal:              terminal,
		TerminalCondition:     terminalCondition,
		TimedOut:              timedOut,
		ElapsedSeconds:        int(time.Since(startedAt).Seconds()),
		TimeoutSeconds:        timeoutSeconds,
		PollIntervalSeconds:   pollIntervalSeconds,
		ReturnWhen:            returnWhen,
		ProposalIds:           proposalIDs,
		ChangedPaths:          changedPaths,
		FollowUpAllowed:       followUpAllowed,
		FollowUpBlockedReason: followUpBlockedReason,
		LaunchEpoch:           wave.OrchestrationEpoch,
		CurrentEpoch:          control.OrchestrationEpoch,
		OrchestrationFrozen:   control.OrchestrationFrozen,
		Workers:               append([]NetrunnerWaveWorkerSnapshot{}, wave.Workers...),
		Wave:                  wave,
	}
	if winner == nil {
		return result
	}
	result.WinningSessionId = winner.Worker.SessionId
	result.WorkerId = winner.Worker.Id
	result.WorkerStatus = winner.Worker.Status
	result.SessionStatus = winner.SessionStatus
	result.Backend = winner.Backend
	result.Model = winner.Model
	result.Reasoning = winner.Reasoning
	result.ExternalSessionId = winner.ExternalSessionID
	result.CodexSessionId = winner.CodexSessionID
	result.Report = winner.Report
	if winner.ProposalIDs != nil {
		result.ProposalIds = append([]int{}, winner.ProposalIDs...)
	}
	result.BaseSha = winner.Worker.BaseSha
	result.HeadSha = winner.Worker.HeadSha
	if winner.Worker.ChangedPaths != nil {
		result.ChangedPaths = append([]string{}, winner.Worker.ChangedPaths...)
	}
	result.DiffPatchPath = winner.Worker.DiffPatchPath
	result.DiffStat = winner.Worker.DiffStat
	result.WorktreePath = winner.Worker.WorktreePath
	if winner.Worker.LaunchEpoch > 0 {
		result.LaunchEpoch = winner.Worker.LaunchEpoch
	}
	return result
}

func WaitForNetrunnerWave(ctx context.Context, req *mcp.CallToolRequest, input WaitForNetrunnerWaveInput) (*mcp.CallToolResult, WaitForNetrunnerWaveOutput, error) {
	if authorizedRole != "fixer" {
		return &mcp.CallToolResult{IsError: true}, WaitForNetrunnerWaveOutput{}, fmt.Errorf("access denied: requires fixer role")
	}
	if authorizedProjectId <= 0 {
		return &mcp.CallToolResult{IsError: true}, WaitForNetrunnerWaveOutput{}, fmt.Errorf("access denied: fixer role is not bound to a project")
	}

	timeoutSeconds, err := explicitWaitTimeoutSeconds(input.TimeoutSeconds)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, WaitForNetrunnerWaveOutput{}, err
	}
	pollIntervalSeconds, err := explicitWaitPollIntervalSeconds(input.PollIntervalSeconds)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, WaitForNetrunnerWaveOutput{}, err
	}
	returnWhen, err := parallelWaveWaitReturnWhen(input.ReturnWhen)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, WaitForNetrunnerWaveOutput{}, err
	}

	wave, err := fetchNetrunnerWaveSnapshot(input.WaveId, authorizedProjectId)
	if err == sql.ErrNoRows {
		return &mcp.CallToolResult{IsError: true}, WaitForNetrunnerWaveOutput{}, fmt.Errorf("wave %d not found in current project", input.WaveId)
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, WaitForNetrunnerWaveOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	if err := validateParallelWaveWaitState(wave); err != nil {
		return &mcp.CallToolResult{IsError: true}, WaitForNetrunnerWaveOutput{}, err
	}

	projectCWD, err := projectCWDFromID(authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, WaitForNetrunnerWaveOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	normalizedProjectCWD, err := normalizeProjectCWD(projectCWD)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, WaitForNetrunnerWaveOutput{}, err
	}
	storedProjectCWD, err := normalizeProjectCWD(wave.ProjectCwd)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, WaitForNetrunnerWaveOutput{}, fmt.Errorf("stored wave project cwd is invalid: %v", err)
	}
	if storedProjectCWD != normalizedProjectCWD {
		return &mcp.CallToolResult{IsError: true}, WaitForNetrunnerWaveOutput{}, fmt.Errorf("wave %d belongs to project cwd %q, current project cwd is %q", wave.Id, storedProjectCWD, normalizedProjectCWD)
	}

	startedAt := time.Now()
	deadline := startedAt.Add(time.Duration(timeoutSeconds) * time.Second)

	for {
		if err := ctx.Err(); err != nil {
			return &mcp.CallToolResult{IsError: true}, WaitForNetrunnerWaveOutput{}, err
		}

		wave, err = fetchNetrunnerWaveSnapshot(input.WaveId, authorizedProjectId)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, WaitForNetrunnerWaveOutput{}, fmt.Errorf("DB query error: %v", err)
		}
		control, _, err := fetchOrchestrationControl(authorizedProjectId)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, WaitForNetrunnerWaveOutput{}, fmt.Errorf("DB query error: %v", err)
		}
		followUpAllowed, blockedReason := parallelWaveFollowUpDecision(control, wave.OrchestrationEpoch)
		if !followUpAllowed {
			if control.OrchestrationEpoch != wave.OrchestrationEpoch {
				if err := markActiveParallelWaveWorkersStale(normalizedProjectCWD, wave, blockedReason); err != nil {
					return &mcp.CallToolResult{IsError: true}, WaitForNetrunnerWaveOutput{}, fmt.Errorf("failed to mark stale wave workers: %v", err)
				}
				if err := refreshParallelWaveAggregateStatus(wave.Id, authorizedProjectId); err != nil {
					return &mcp.CallToolResult{IsError: true}, WaitForNetrunnerWaveOutput{}, fmt.Errorf("DB update error: %v", err)
				}
				wave, err = fetchNetrunnerWaveSnapshot(wave.Id, authorizedProjectId)
				if err != nil {
					return &mcp.CallToolResult{IsError: true}, WaitForNetrunnerWaveOutput{}, fmt.Errorf("DB query error: %v", err)
				}
			}
			result := buildParallelWaveWaitResult(startedAt, timeoutSeconds, pollIntervalSeconds, returnWhen, wave, nil, true, "follow_up_blocked", false, false, blockedReason, control)
			return nil, WaitForNetrunnerWaveOutput{Status: "blocked", Result: result}, nil
		}

		qualifying := []parallelWaveWaitCandidate{}
		allTerminal := true
		for _, worker := range wave.Workers {
			candidate, terminal, err := inspectParallelWaveWorkerForWait(normalizedProjectCWD, wave, worker)
			if err != nil {
				return &mcp.CallToolResult{IsError: true}, WaitForNetrunnerWaveOutput{}, fmt.Errorf("failed to inspect wave worker %d: %v", worker.SessionId, err)
			}
			if terminal {
				qualifying = append(qualifying, candidate)
			} else {
				allTerminal = false
			}
		}
		if len(qualifying) > 0 {
			if err := refreshParallelWaveAggregateStatus(wave.Id, authorizedProjectId); err != nil {
				return &mcp.CallToolResult{IsError: true}, WaitForNetrunnerWaveOutput{}, fmt.Errorf("DB update error: %v", err)
			}
			wave, err = fetchNetrunnerWaveSnapshot(wave.Id, authorizedProjectId)
			if err != nil {
				return &mcp.CallToolResult{IsError: true}, WaitForNetrunnerWaveOutput{}, fmt.Errorf("DB query error: %v", err)
			}
			for index := range qualifying {
				for _, refreshedWorker := range wave.Workers {
					if refreshedWorker.Id == qualifying[index].Worker.Id {
						qualifying[index].Worker = refreshedWorker
						break
					}
				}
			}
			sort.Slice(qualifying, func(i, j int) bool {
				return qualifying[i].Worker.SessionId < qualifying[j].Worker.SessionId
			})
		}

		if returnWhen == parallelWaveWaitFirstReviewReady && len(qualifying) > 0 {
			winner := qualifying[0]
			result := buildParallelWaveWaitResult(startedAt, timeoutSeconds, pollIntervalSeconds, returnWhen, wave, &winner, true, winner.TerminalCondition, false, true, "", control)
			log.Printf("wait_for_netrunner_wave project_id=%d wave_id=%d winner_session_id=%d condition=%q status=%q", authorizedProjectId, wave.Id, winner.Worker.SessionId, winner.TerminalCondition, winner.Worker.Status)
			return nil, WaitForNetrunnerWaveOutput{Status: "success", Result: result}, nil
		}
		if returnWhen == parallelWaveWaitAllTerminal && allTerminal {
			var winner *parallelWaveWaitCandidate
			if len(qualifying) > 0 {
				selected := qualifying[0]
				winner = &selected
			}
			result := buildParallelWaveWaitResult(startedAt, timeoutSeconds, pollIntervalSeconds, returnWhen, wave, winner, true, parallelWaveWaitAllTerminal, false, true, "", control)
			log.Printf("wait_for_netrunner_wave project_id=%d wave_id=%d return_when=%q all_terminal=true", authorizedProjectId, wave.Id, returnWhen)
			return nil, WaitForNetrunnerWaveOutput{Status: "success", Result: result}, nil
		}

		if time.Now().After(deadline) {
			wave, err = fetchNetrunnerWaveSnapshot(input.WaveId, authorizedProjectId)
			if err != nil {
				return &mcp.CallToolResult{IsError: true}, WaitForNetrunnerWaveOutput{}, fmt.Errorf("DB query error: %v", err)
			}
			control, _, err = fetchOrchestrationControl(authorizedProjectId)
			if err != nil {
				return &mcp.CallToolResult{IsError: true}, WaitForNetrunnerWaveOutput{}, fmt.Errorf("DB query error: %v", err)
			}
			followUpAllowed, blockedReason = parallelWaveFollowUpDecision(control, wave.OrchestrationEpoch)
			result := buildParallelWaveWaitResult(startedAt, timeoutSeconds, pollIntervalSeconds, returnWhen, wave, nil, false, "timed_out", true, followUpAllowed, blockedReason, control)
			return nil, WaitForNetrunnerWaveOutput{Status: "timed_out", Result: result}, nil
		}

		time.Sleep(time.Duration(pollIntervalSeconds) * time.Second)
	}
}

func CleanupNetrunnerWave(ctx context.Context, req *mcp.CallToolRequest, input CleanupNetrunnerWaveInput) (*mcp.CallToolResult, CleanupNetrunnerWaveOutput, error) {
	if authorizedRole != "fixer" {
		return &mcp.CallToolResult{IsError: true}, CleanupNetrunnerWaveOutput{}, fmt.Errorf("access denied: requires fixer role")
	}
	if authorizedProjectId <= 0 {
		return &mcp.CallToolResult{IsError: true}, CleanupNetrunnerWaveOutput{}, fmt.Errorf("access denied: fixer role is not bound to a project")
	}

	wave, err := fetchNetrunnerWaveSnapshot(input.WaveId, authorizedProjectId)
	if err == sql.ErrNoRows {
		return &mcp.CallToolResult{IsError: true}, CleanupNetrunnerWaveOutput{}, fmt.Errorf("wave %d not found in current project", input.WaveId)
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CleanupNetrunnerWaveOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	if err := validateParallelWaveCleanupPreconditions(wave, authorizedProjectId); err != nil {
		return &mcp.CallToolResult{IsError: true}, CleanupNetrunnerWaveOutput{}, err
	}

	projectCWD, err := projectCWDFromID(authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CleanupNetrunnerWaveOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	normalizedProjectCWD, err := normalizeProjectCWD(projectCWD)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CleanupNetrunnerWaveOutput{}, err
	}
	storedProjectCWD, err := normalizeProjectCWD(wave.ProjectCwd)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CleanupNetrunnerWaveOutput{}, fmt.Errorf("stored wave project cwd is invalid: %v", err)
	}
	if storedProjectCWD != normalizedProjectCWD {
		return &mcp.CallToolResult{IsError: true}, CleanupNetrunnerWaveOutput{}, fmt.Errorf("wave %d belongs to project cwd %q, current project cwd is %q", wave.Id, storedProjectCWD, normalizedProjectCWD)
	}

	resolvedPathsByWorkerID := make(map[int]string, len(wave.Workers))
	for _, worker := range wave.Workers {
		resolvedPath, err := resolveParallelWaveCleanupWorktreePath(normalizedProjectCWD, worker.WorktreePath)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, CleanupNetrunnerWaveOutput{}, fmt.Errorf("worker %d: %w", worker.SessionId, err)
		}
		resolvedPathsByWorkerID[worker.Id] = resolvedPath
	}

	listSpec, err := gitWorktreeListCommand(normalizedProjectCWD)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CleanupNetrunnerWaveOutput{}, err
	}
	listOutput, err := runGitCommandSpec(listSpec)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CleanupNetrunnerWaveOutput{}, fmt.Errorf("failed to list git worktrees: %v", err)
	}
	listedWorktrees := parseGitWorktreeListPorcelain(listOutput)

	results := make([]NetrunnerWaveCleanupWorkerResult, 0, len(wave.Workers))
	orphanDiagnostics := []string{}
	hadFailure := false
	for _, worker := range wave.Workers {
		resolvedPath := resolvedPathsByWorkerID[worker.Id]
		_, listed := listedWorktrees[filepath.Clean(resolvedPath)]
		result := NetrunnerWaveCleanupWorkerResult{
			WorkerId:             worker.Id,
			SessionId:            worker.SessionId,
			WorkerStatus:         worker.Status,
			CleanupStatus:        worker.CleanupStatus,
			RecordedWorktreePath: worker.WorktreePath,
			ResolvedWorktreePath: resolvedPath,
			WorktreeListed:       listed,
		}

		info, statErr := os.Stat(resolvedPath)
		if statErr != nil {
			if os.IsNotExist(statErr) {
				diagnostic := fmt.Sprintf("recorded worktree missing: %s", resolvedPath)
				if listed {
					diagnostic = fmt.Sprintf("recorded worktree missing but still listed by git worktree list: %s", resolvedPath)
				}
				if err := updateParallelWaveWorkerCleanup(worker, authorizedProjectId, parallelWaveCleanupStatusMissing, diagnostic, true); err != nil {
					return &mcp.CallToolResult{IsError: true}, CleanupNetrunnerWaveOutput{}, fmt.Errorf("DB update error: %v", err)
				}
				result.WorkerStatus = parallelWaveWorkerStatusCleaned
				result.CleanupStatus = parallelWaveCleanupStatusMissing
				result.Missing = true
				result.Diagnostic = diagnostic
				orphanDiagnostics = append(orphanDiagnostics, diagnostic)
				results = append(results, result)
				continue
			}
			diagnostic := fmt.Sprintf("failed to inspect recorded worktree %s: %v", resolvedPath, statErr)
			if err := updateParallelWaveWorkerCleanup(worker, authorizedProjectId, parallelWaveCleanupStatusFailed, diagnostic, false); err != nil {
				return &mcp.CallToolResult{IsError: true}, CleanupNetrunnerWaveOutput{}, fmt.Errorf("DB update error: %v", err)
			}
			result.CleanupStatus = parallelWaveCleanupStatusFailed
			result.Error = diagnostic
			hadFailure = true
			results = append(results, result)
			continue
		}
		result.WorktreeExists = true
		if !info.IsDir() {
			diagnostic := fmt.Sprintf("recorded worktree path is not a directory: %s", resolvedPath)
			if err := updateParallelWaveWorkerCleanup(worker, authorizedProjectId, parallelWaveCleanupStatusFailed, diagnostic, false); err != nil {
				return &mcp.CallToolResult{IsError: true}, CleanupNetrunnerWaveOutput{}, fmt.Errorf("DB update error: %v", err)
			}
			result.CleanupStatus = parallelWaveCleanupStatusFailed
			result.Error = diagnostic
			hadFailure = true
			results = append(results, result)
			continue
		}
		if !listed {
			diagnostic := fmt.Sprintf("recorded worktree exists but is not listed by git worktree list: %s", resolvedPath)
			if err := updateParallelWaveWorkerCleanup(worker, authorizedProjectId, parallelWaveCleanupStatusFailed, diagnostic, false); err != nil {
				return &mcp.CallToolResult{IsError: true}, CleanupNetrunnerWaveOutput{}, fmt.Errorf("DB update error: %v", err)
			}
			result.CleanupStatus = parallelWaveCleanupStatusFailed
			result.Error = diagnostic
			orphanDiagnostics = append(orphanDiagnostics, diagnostic)
			hadFailure = true
			results = append(results, result)
			continue
		}
		if !input.RemoveWorktrees {
			result.Skipped = true
			result.Diagnostic = "remove_worktrees=false; recorded terminal worktree left in place"
			results = append(results, result)
			continue
		}

		removeSpec, err := gitWorktreeRemoveCommand(normalizedProjectCWD, resolvedPath, input.Force)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, CleanupNetrunnerWaveOutput{}, fmt.Errorf("worker %d: %w", worker.SessionId, err)
		}
		if _, err := runGitCommandSpec(removeSpec); err != nil {
			diagnostic := err.Error()
			if err := updateParallelWaveWorkerCleanup(worker, authorizedProjectId, parallelWaveCleanupStatusFailed, diagnostic, false); err != nil {
				return &mcp.CallToolResult{IsError: true}, CleanupNetrunnerWaveOutput{}, fmt.Errorf("DB update error: %v", err)
			}
			result.CleanupStatus = parallelWaveCleanupStatusFailed
			result.Error = diagnostic
			hadFailure = true
			results = append(results, result)
			continue
		}
		if err := updateParallelWaveWorkerCleanup(worker, authorizedProjectId, parallelWaveCleanupStatusCleaned, "", true); err != nil {
			return &mcp.CallToolResult{IsError: true}, CleanupNetrunnerWaveOutput{}, fmt.Errorf("DB update error: %v", err)
		}
		result.WorkerStatus = parallelWaveWorkerStatusCleaned
		result.CleanupStatus = parallelWaveCleanupStatusCleaned
		result.Removed = true
		results = append(results, result)
	}

	pruneOutput := ""
	pruneRan := false
	if input.Prune {
		pruneRan = true
		pruneSpec, err := gitWorktreePruneCommand(normalizedProjectCWD)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, CleanupNetrunnerWaveOutput{}, err
		}
		pruneOutput, err = runGitCommandSpec(pruneSpec)
		if err != nil {
			pruneOutput = err.Error()
			hadFailure = true
		}
	}

	cleaned, err := markParallelWaveCleanedIfReady(wave.Id, authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CleanupNetrunnerWaveOutput{}, fmt.Errorf("DB update error: %v", err)
	}
	refreshedWave, err := fetchNetrunnerWaveSnapshot(wave.Id, authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CleanupNetrunnerWaveOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	status := "success"
	if hadFailure {
		status = "partial_failure"
	} else if !cleaned {
		status = "inspected"
	}
	return nil, CleanupNetrunnerWaveOutput{
		Status:            status,
		WaveId:            refreshedWave.Id,
		WaveStatus:        refreshedWave.Status,
		RemoveWorktrees:   input.RemoveWorktrees,
		Prune:             input.Prune,
		PruneRan:          pruneRan,
		Force:             input.Force,
		Cleaned:           cleaned,
		Workers:           results,
		OrphanDiagnostics: orphanDiagnostics,
		PruneOutput:       pruneOutput,
		Wave:              refreshedWave,
	}, nil
}
func parallelWaveFollowUpDecision(control orchestrationControl, waveEpoch int) (bool, string) {
	reasons := []string{}
	if control.OrchestrationFrozen {
		reasons = append(reasons, "project_orchestration_frozen")
	}
	if control.OrchestrationEpoch != waveEpoch {
		reasons = append(reasons, fmt.Sprintf("stale_orchestration_epoch:%d->%d", waveEpoch, control.OrchestrationEpoch))
	}
	if len(reasons) > 0 {
		return false, strings.Join(reasons, ",")
	}
	return true, ""
}
