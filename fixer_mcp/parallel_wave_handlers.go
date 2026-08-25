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
	"unicode/utf8"

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
	SessionIds              []int            `json:"session_ids" jsonschema:"Project-scoped pending session IDs to include in the wave. Must contain at least one session."`
	Dependencies            []WaveDependency `json:"dependencies,omitempty" jsonschema:"Optional DAG dependencies. Each child is held until all listed parent sessions have completed."`
	WorktreeRoot            string           `json:"worktree_root,omitempty" jsonschema:"Optional project-relative or absolute root for future worker worktrees. Defaults to .codex/netrunner_worktrees."`
	BaseRef                 string           `json:"base_ref,omitempty" jsonschema:"Optional Git base ref to resolve for the wave. Defaults to HEAD."`
	Reason                  string           `json:"reason,omitempty" jsonschema:"Optional audit reason for creating the wave."`
	EpicDocId               int              `json:"epic_doc_id,omitempty" jsonschema:"Optional project-scoped epic documentation ID to link to the wave."`
	ParentWaveId            int              `json:"parent_wave_id,omitempty" jsonschema:"Optional parent wave ID. Child waves inherit and decrement the root recursion budgets."`
	MaxChildWaveDepth       int              `json:"max_child_wave_depth,omitempty" jsonschema:"Root-only recursion depth. Zero disables child-wave creation; maximum 16."`
	MaxTotalDescendantWaves int              `json:"max_total_descendant_waves,omitempty" jsonschema:"Root-only total descendant-wave safety budget. Defaults to 32 when recursion is enabled; maximum 256."`
	MaxTotalSessions        int              `json:"max_total_sessions,omitempty" jsonschema:"Root-only total session safety budget across the wave tree. Defaults to 128; maximum 2048."`
	ReviewPolicy            string           `json:"review_policy,omitempty" jsonschema:"Review policy for the wave: manual (default; Fixer reviews directly) or automatic (starts a reviewer Netrunner after all workers are terminal; use only when explicitly requested by the Architect or for a very large parallel wave)."`
	ReviewBackend           string           `json:"review_backend,omitempty" jsonschema:"Backend for an automatic reviewer. Defaults to codex."`
	ReviewModel             string           `json:"review_model,omitempty" jsonschema:"Model for an automatic reviewer. Defaults to opencode-go/deepseek-v4-flash."`
	ReviewReasoning         string           `json:"review_reasoning,omitempty" jsonschema:"Reasoning level for an automatic reviewer. Defaults to high."`
}

type WaveDependency struct {
	Child   int64   `json:"child"`
	Parents []int64 `json:"parents"`
}

type GetNetrunnerWaveInput struct {
	WaveId int `json:"wave_id" jsonschema:"Parallel wave ID to read."`
}

type LaunchNetrunnerWaveInput struct {
	WaveId          int                      `json:"wave_id" jsonschema:"Parallel wave ID to launch."`
	Backend         string                   `json:"backend,omitempty" jsonschema:"Optional default CLI backend for workers without a worker_configs override. Supported: codex, commandcode, droid, antigravity, junie, kimi-code, grok."`
	Model           string                   `json:"model,omitempty" jsonschema:"Optional default model for workers without a worker_configs override."`
	Reasoning       string                   `json:"reasoning,omitempty" jsonschema:"Optional default reasoning for workers without a worker_configs override."`
	WorkerConfigs   []WaveWorkerLaunchConfig `json:"worker_configs,omitempty" jsonschema:"Optional per-worker launch overrides keyed by project-scoped session_id. Unspecified fields inherit the wave defaults or the session's stored launch configuration."`
	FixerSessionId  string                   `json:"fixer_session_id,omitempty" jsonschema:"Optional current Fixer Codex session ID to pass into worker prompts."`
	TimeoutSeconds  int                      `json:"timeout_seconds,omitempty" jsonschema:"Optional startup metadata wait in seconds. Default 120; max 21600."`
	ReviewPolicy    string                   `json:"review_policy,omitempty" jsonschema:"Optional review policy override: manual (default; Fixer reviews directly) or automatic (starts a reviewer Netrunner only when explicitly requested)."`
	ReviewBackend   string                   `json:"review_backend,omitempty" jsonschema:"Optional automatic reviewer backend. Defaults to codex."`
	ReviewModel     string                   `json:"review_model,omitempty" jsonschema:"Optional automatic reviewer model. Defaults to opencode-go/deepseek-v4-flash."`
	ReviewReasoning string                   `json:"review_reasoning,omitempty" jsonschema:"Optional automatic reviewer reasoning. Defaults to high."`
}

type WaveWorkerLaunchConfig struct {
	SessionId int    `json:"session_id" jsonschema:"Project-scoped session ID of the worker to configure."`
	Backend   string `json:"backend,omitempty" jsonschema:"Optional per-worker CLI backend override."`
	Model     string `json:"model,omitempty" jsonschema:"Optional per-worker model override."`
	Reasoning string `json:"reasoning,omitempty" jsonschema:"Optional per-worker reasoning override."`
}

type WaitForNetrunnerWaveInput struct {
	WaveId              int    `json:"wave_id" jsonschema:"Parallel wave ID to wait on."`
	TimeoutSeconds      int    `json:"timeout_seconds,omitempty" jsonschema:"Optional wait timeout in seconds. Defaults to 300; minimum 300; maximum 21600."`
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
	Id                  int      `json:"id"`
	WaveId              int      `json:"wave_id"`
	ProjectId           int      `json:"project_id"`
	SessionId           int      `json:"session_id"`
	Status              string   `json:"status"`
	DeclaredWriteScope  []string `json:"declared_write_scope"`
	BranchName          string   `json:"branch_name"`
	WorktreePath        string   `json:"worktree_path"`
	BaseSha             string   `json:"base_sha"`
	HeadSha             string   `json:"head_sha"`
	ChangedPaths        []string `json:"changed_paths"`
	DiffPatchPath       string   `json:"diff_patch_path"`
	DiffStat            string   `json:"diff_stat"`
	LaunchEpoch         int      `json:"launch_epoch"`
	WorkerProcessId     int      `json:"worker_process_id,omitempty"`
	ExternalSessionId   string   `json:"external_session_id"`
	HeadlessLogPath     string   `json:"headless_log_path"`
	LauncherLogPath     string   `json:"launcher_log_path"`
	WorkerMetadataPath  string   `json:"worker_metadata_path"`
	FailureReason       string   `json:"failure_reason"`
	TerminalOutcome     string   `json:"terminal_outcome"`
	RetryAttemptCount   int      `json:"retry_attempt_count"`
	RetryCause          string   `json:"retry_cause"`
	RetryNextEligibleAt string   `json:"retry_next_eligible_at"`
	CleanupStatus       string   `json:"cleanup_status"`
	CreatedAt           string   `json:"created_at"`
	UpdatedAt           string   `json:"updated_at"`
	LaunchedAt          string   `json:"launched_at,omitempty"`
	TerminalAt          string   `json:"terminal_at,omitempty"`
	CleanedAt           string   `json:"cleaned_at,omitempty"`
	SessionReport       string   `json:"session_report,omitempty"`
}

type NetrunnerWaveSnapshot struct {
	Id                      int                           `json:"id"`
	ProjectId               int                           `json:"project_id"`
	Status                  string                        `json:"status"`
	Phase                   string                        `json:"phase"`
	GateState               string                        `json:"gate_state"`
	ControlState            string                        `json:"control_state"`
	ControlReason           string                        `json:"control_reason"`
	BaseSha                 string                        `json:"base_sha"`
	BaseBranch              string                        `json:"base_branch"`
	ProjectCwd              string                        `json:"project_cwd"`
	WorktreeRoot            string                        `json:"worktree_root"`
	OrchestrationEpoch      int                           `json:"orchestration_epoch"`
	CreatedBySessionId      int                           `json:"created_by_session_id,omitempty"`
	EpicDocId               int                           `json:"epic_doc_id,omitempty"`
	ParentWaveId            int                           `json:"parent_wave_id,omitempty"`
	RootWaveId              int                           `json:"root_wave_id"`
	Depth                   int                           `json:"depth"`
	MaxChildWaveDepth       int                           `json:"max_child_wave_depth"`
	MaxTotalDescendantWaves int                           `json:"max_total_descendant_waves"`
	MaxTotalSessions        int                           `json:"max_total_sessions"`
	FailurePolicyState      string                        `json:"failure_policy_state"`
	RepairWorkerId          int                           `json:"repair_worker_id,omitempty"`
	RepairAttemptCount      int                           `json:"repair_attempt_count"`
	HandoffSha              string                        `json:"handoff_sha"`
	FailureReason           string                        `json:"failure_reason"`
	CreatedAt               string                        `json:"created_at"`
	UpdatedAt               string                        `json:"updated_at"`
	LaunchedAt              string                        `json:"launched_at,omitempty"`
	CompletedAt             string                        `json:"completed_at,omitempty"`
	ReviewSessionId         int                           `json:"review_session_id,omitempty"`
	ReviewSessionStatus     string                        `json:"review_session_status,omitempty"`
	ReviewSessionReport     string                        `json:"review_session_report,omitempty"`
	AcceptanceSessionId     int                           `json:"acceptance_session_id,omitempty"`
	AcceptanceSessionStatus string                        `json:"acceptance_session_status,omitempty"`
	AcceptanceSessionReport string                        `json:"acceptance_session_report,omitempty"`
	ReviewPolicy            string                        `json:"review_policy"`
	ReviewBackend           string                        `json:"review_backend"`
	ReviewModel             string                        `json:"review_model"`
	ReviewReasoning         string                        `json:"review_reasoning"`
	Workers                 []NetrunnerWaveWorkerSnapshot `json:"workers"`
	Dependencies            []WaveDependency              `json:"dependencies"`
}

type NetrunnerWaveWorkerCounts struct {
	Total        int `json:"total"`
	Active       int `json:"active"`
	Terminal     int `json:"terminal"`
	ReviewReady  int `json:"review_ready"`
	Completed    int `json:"completed"`
	Failed       int `json:"failed"`
	Stopped      int `json:"stopped"`
	StaleEpoch   int `json:"stale_epoch"`
	Blocked      int `json:"blocked"`
	RetryPending int `json:"retry_pending"`
}

type NetrunnerWaveOperatorSummary struct {
	SchemaVersion       string                    `json:"schema_version"`
	WaveId              int                       `json:"wave_id"`
	ProjectId           int                       `json:"project_id"`
	OperatorState       string                    `json:"operator_state"`
	Label               string                    `json:"label"`
	NextAction          string                    `json:"next_action"`
	LegacyStatus        string                    `json:"legacy_status"`
	Phase               string                    `json:"phase"`
	GateState           string                    `json:"gate_state"`
	ControlState        string                    `json:"control_state"`
	FailurePolicyState  string                    `json:"failure_policy_state"`
	WorkerCounts        NetrunnerWaveWorkerCounts `json:"worker_counts"`
	AllWorkersTerminal  bool                      `json:"all_workers_terminal"`
	WaveReviewReady     bool                      `json:"wave_review_ready"`
	RepairRequired      bool                      `json:"repair_required"`
	ArchitectPaused     bool                      `json:"architect_paused"`
	AcceptanceReady     bool                      `json:"acceptance_ready"`
	WaveCompleted       bool                      `json:"wave_completed"`
	ReviewSessionId     int                       `json:"review_session_id,omitempty"`
	ReviewState         string                    `json:"review_state"`
	ReviewPolicy        string                    `json:"review_policy"`
	ReviewBackend       string                    `json:"review_backend"`
	ReviewModel         string                    `json:"review_model"`
	ReviewReasoning     string                    `json:"review_reasoning"`
	AcceptanceSessionId int                       `json:"acceptance_session_id,omitempty"`
	AcceptanceState     string                    `json:"acceptance_state"`
	RepairWorkerId      int                       `json:"repair_worker_id,omitempty"`
	RepairAttemptCount  int                       `json:"repair_attempt_count"`
	PauseReason         string                    `json:"pause_reason,omitempty"`
	FailureReason       string                    `json:"failure_reason,omitempty"`
	UpdatedAt           string                    `json:"updated_at,omitempty"`
}

func boundedParallelWaveSummaryText(raw string, maxBytes int) string {
	trimmed := strings.TrimSpace(raw)
	if maxBytes <= 0 || len(trimmed) <= maxBytes {
		return trimmed
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(trimmed[cut]) {
		cut--
	}
	return strings.TrimSpace(trimmed[:cut])
}

func parallelWaveSessionState(sessionID int, status string) string {
	if sessionID <= 0 {
		return "not_started"
	}
	if normalized := strings.TrimSpace(status); normalized != "" {
		return normalized
	}
	return "pending"
}

func buildNetrunnerWaveOperatorSummary(wave NetrunnerWaveSnapshot) NetrunnerWaveOperatorSummary {
	counts := NetrunnerWaveWorkerCounts{Total: len(wave.Workers)}
	for _, worker := range wave.Workers {
		if _, terminal := parallelWaveWorkerTerminalCondition(worker.Status); terminal {
			counts.Terminal++
		} else {
			counts.Active++
		}
		switch worker.Status {
		case parallelWaveWorkerStatusReviewReady:
			counts.ReviewReady++
		case parallelWaveWorkerStatusCompleted, parallelWaveWorkerStatusCleaned:
			counts.Completed++
		case parallelWaveWorkerStatusFailed:
			counts.Failed++
		case parallelWaveWorkerStatusStopped:
			counts.Stopped++
		case parallelWaveWorkerStatusStaleEpoch:
			counts.StaleEpoch++
		case parallelWaveWorkerStatusBlocked:
			counts.Blocked++
		}
		if worker.Status == parallelWaveWorkerStatusRetryWait || strings.TrimSpace(worker.RetryNextEligibleAt) != "" {
			counts.RetryPending++
		}
	}

	allWorkersTerminal := counts.Total > 0 && counts.Terminal == counts.Total
	architectPaused := wave.ControlState == parallelWaveControlPausedForArchitect || wave.FailurePolicyState == parallelWaveFailurePolicyPaused
	waveCompleted := wave.Phase == parallelWavePhaseCompleted && wave.GateState == parallelWaveGateClosed
	repairRequired := wave.GateState == parallelWaveGateImplementationRepair || wave.FailurePolicyState == parallelWaveFailurePolicyRepairRequired || wave.FailurePolicyState == parallelWaveFailurePolicyRepairAuthorized || wave.FailurePolicyState == parallelWaveFailurePolicyRepairInProgress
	waveReviewReady := wave.GateState == parallelWaveGateImplementationReview && wave.FailurePolicyState == parallelWaveFailurePolicyPassed
	acceptanceReady := wave.Phase == parallelWavePhaseAcceptance && wave.AcceptanceSessionId > 0 && wave.AcceptanceSessionStatus == "completed"

	operatorState, label, nextAction := "implementation_active", "Implementation running", "wait"
	switch {
	case architectPaused:
		operatorState, label, nextAction = "architect_paused", "Paused for Architect", "resume_by_architect"
	case waveCompleted:
		operatorState, label, nextAction = "completed", "Completed", "none"
	case wave.Phase == parallelWavePhaseAcceptance:
		operatorState, label, nextAction = "acceptance", "Acceptance in progress", "run_acceptance"
		if acceptanceReady {
			nextAction = "review_acceptance"
		}
	case repairRequired:
		operatorState, label, nextAction = "repair_blocked", "Repair required", "authorize_repair"
		if wave.FailurePolicyState == parallelWaveFailurePolicyRepairAuthorized || wave.FailurePolicyState == parallelWaveFailurePolicyRepairInProgress {
			nextAction = "monitor_repair"
		}
	case waveReviewReady:
		operatorState, label, nextAction = "wave_review_ready", "Ready for implementation review", "review_implementation"
	case allWorkersTerminal:
		operatorState, label, nextAction = "worker_terminal", "Workers terminal; Fixer review pending", "review_implementation"
	}

	return NetrunnerWaveOperatorSummary{
		SchemaVersion:       "wave-operator-summary/v1",
		WaveId:              wave.Id,
		ProjectId:           wave.ProjectId,
		OperatorState:       operatorState,
		Label:               label,
		NextAction:          nextAction,
		LegacyStatus:        wave.Status,
		Phase:               wave.Phase,
		GateState:           wave.GateState,
		ControlState:        wave.ControlState,
		FailurePolicyState:  wave.FailurePolicyState,
		WorkerCounts:        counts,
		AllWorkersTerminal:  allWorkersTerminal,
		WaveReviewReady:     waveReviewReady,
		RepairRequired:      repairRequired,
		ArchitectPaused:     architectPaused,
		AcceptanceReady:     acceptanceReady,
		WaveCompleted:       waveCompleted,
		ReviewSessionId:     wave.ReviewSessionId,
		ReviewState:         parallelWaveSessionState(wave.ReviewSessionId, wave.ReviewSessionStatus),
		ReviewPolicy:        wave.ReviewPolicy,
		ReviewBackend:       wave.ReviewBackend,
		ReviewModel:         wave.ReviewModel,
		ReviewReasoning:     wave.ReviewReasoning,
		AcceptanceSessionId: wave.AcceptanceSessionId,
		AcceptanceState:     parallelWaveSessionState(wave.AcceptanceSessionId, wave.AcceptanceSessionStatus),
		RepairWorkerId:      wave.RepairWorkerId,
		RepairAttemptCount:  wave.RepairAttemptCount,
		PauseReason:         boundedParallelWaveSummaryText(wave.ControlReason, 240),
		FailureReason:       boundedParallelWaveSummaryText(wave.FailureReason, 240),
		UpdatedAt:           wave.UpdatedAt,
	}
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
	Status  string                  `json:"status"`
	Result  NetrunnerWaveWaitResult `json:"result"`
	Reports []string                `json:"reports,omitempty"`
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

func parallelWaveWorkerLaunchInputs(wave NetrunnerWaveSnapshot, input LaunchNetrunnerWaveInput) (map[int]LaunchNetrunnerWaveInput, error) {
	workerSessionIDs := make(map[int]struct{}, len(wave.Workers))
	inputs := make(map[int]LaunchNetrunnerWaveInput, len(wave.Workers))
	for _, worker := range wave.Workers {
		workerSessionIDs[worker.SessionId] = struct{}{}
		inputs[worker.SessionId] = LaunchNetrunnerWaveInput{
			WaveId:         input.WaveId,
			Backend:        input.Backend,
			Model:          input.Model,
			Reasoning:      input.Reasoning,
			FixerSessionId: input.FixerSessionId,
			TimeoutSeconds: input.TimeoutSeconds,
		}
	}

	configured := make(map[int]struct{}, len(input.WorkerConfigs))
	for _, override := range input.WorkerConfigs {
		if override.SessionId <= 0 {
			return nil, fmt.Errorf("worker_configs session_id must be positive")
		}
		if _, ok := workerSessionIDs[override.SessionId]; !ok {
			return nil, fmt.Errorf("worker_configs session %d is not part of wave %d", override.SessionId, wave.Id)
		}
		if _, duplicate := configured[override.SessionId]; duplicate {
			return nil, fmt.Errorf("worker_configs contains duplicate session %d", override.SessionId)
		}
		configured[override.SessionId] = struct{}{}

		workerInput := inputs[override.SessionId]
		if strings.TrimSpace(override.Backend) != "" {
			workerInput.Backend = override.Backend
		}
		if strings.TrimSpace(override.Model) != "" {
			workerInput.Model = override.Model
		}
		if strings.TrimSpace(override.Reasoning) != "" {
			workerInput.Reasoning = override.Reasoning
		}
		inputs[override.SessionId] = workerInput
	}
	return inputs, nil
}

func applyParallelWaveReviewConfig(wave NetrunnerWaveSnapshot, input LaunchNetrunnerWaveInput) (NetrunnerWaveSnapshot, error) {
	policy := wave.ReviewPolicy
	backend := wave.ReviewBackend
	model := wave.ReviewModel
	reasoning := wave.ReviewReasoning
	if strings.TrimSpace(input.ReviewPolicy) != "" {
		policy = input.ReviewPolicy
	}
	if strings.TrimSpace(input.ReviewBackend) != "" {
		backend = input.ReviewBackend
	}
	if strings.TrimSpace(input.ReviewModel) != "" {
		model = input.ReviewModel
	}
	if strings.TrimSpace(input.ReviewReasoning) != "" {
		reasoning = input.ReviewReasoning
	}
	policy, backend, model, reasoning, err := resolveParallelWaveReviewConfig(policy, backend, model, reasoning)
	if err != nil {
		return NetrunnerWaveSnapshot{}, err
	}
	if policy == parallelWaveReviewPolicyAutomatic && len(wave.Workers) == 1 {
		return NetrunnerWaveSnapshot{}, fmt.Errorf("automatic review_policy is not allowed for a one-worker wave; use manual Fixer review")
	}
	if policy == wave.ReviewPolicy && backend == wave.ReviewBackend && model == wave.ReviewModel && reasoning == wave.ReviewReasoning {
		return wave, nil
	}
	if _, err := db.Exec(
		`UPDATE parallel_wave
		 SET review_policy = ?, review_backend = ?, review_model = ?, review_reasoning = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE id = ? AND project_id = ?`,
		policy, backend, model, reasoning, wave.Id, authorizedProjectId,
	); err != nil {
		return NetrunnerWaveSnapshot{}, fmt.Errorf("DB update error: %v", err)
	}
	wave.ReviewPolicy = policy
	wave.ReviewBackend = backend
	wave.ReviewModel = model
	wave.ReviewReasoning = reasoning
	return wave, nil
}

func prepareParallelWaveWorkerLaunchConfigs(ctx context.Context, wave NetrunnerWaveSnapshot, input LaunchNetrunnerWaveInput) (map[int]LaunchNetrunnerWaveInput, error) {
	inputs, err := parallelWaveWorkerLaunchInputs(wave, input)
	if err != nil {
		return nil, err
	}

	type plannedLaunchConfig struct {
		globalSessionID int
		config          sessionLaunchConfig
	}
	planned := make([]plannedLaunchConfig, 0, len(wave.Workers))
	for _, worker := range wave.Workers {
		globalSessionID, err := globalSessionIDFromProjectScoped(worker.SessionId, authorizedProjectId)
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("session %d not found in current project", worker.SessionId)
		}
		if err != nil {
			return nil, fmt.Errorf("DB query error: %v", err)
		}
		currentConfig, err := readSessionLaunchConfig(globalSessionID, authorizedProjectId)
		if err != nil {
			return nil, fmt.Errorf("failed to read launch config for session %d: %v", worker.SessionId, err)
		}
		workerInput := inputs[worker.SessionId]
		resolved, err := resolveSessionLaunchConfigValues(currentConfig, workerInput.Backend, workerInput.Model, workerInput.Reasoning)
		if err != nil {
			return nil, fmt.Errorf("invalid launch config for session %d: %v", worker.SessionId, err)
		}
		planned = append(planned, plannedLaunchConfig{globalSessionID: globalSessionID, config: resolved})
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to begin worker launch config transaction: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, plan := range planned {
		if _, err := tx.ExecContext(
			ctx,
			`UPDATE session
			 SET cli_backend = ?, cli_model = ?, cli_reasoning = ?
			 WHERE id = ? AND project_id = ?`,
			plan.config.Backend,
			plan.config.Model,
			plan.config.Reasoning,
			plan.globalSessionID,
			authorizedProjectId,
		); err != nil {
			return nil, fmt.Errorf("failed to persist worker launch config: %v", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit worker launch configs: %v", err)
	}
	return inputs, nil
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
	if len(localSessionIDs) == 0 {
		return nil, fmt.Errorf("session_ids must contain at least one session")
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
		if dbTableHasColumn("session", "parallel_wave_id") {
			var parallelWaveID string
			if err := db.QueryRow(
				`SELECT COALESCE(parallel_wave_id, '') FROM session WHERE id = ? AND project_id = ?`,
				globalSessionID,
				projectID,
			).Scan(&parallelWaveID); err != nil {
				return nil, fmt.Errorf("failed to inspect session %d wave linkage: %v", localSessionID, err)
			}
			if strings.TrimSpace(parallelWaveID) != "" {
				return nil, fmt.Errorf("session %d is already linked to wave marker %q", localSessionID, parallelWaveID)
			}
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

	return candidates, nil
}

func normalizeWaveDependencies(dependencies []WaveDependency, sessionIDs []int) ([]WaveDependency, error) {
	allowed := make(map[int64]struct{}, len(sessionIDs))
	for _, sessionID := range sessionIDs {
		allowed[int64(sessionID)] = struct{}{}
	}

	parentsByChild := make(map[int64]map[int64]struct{}, len(dependencies))
	childOrder := make([]int64, 0, len(dependencies))
	for _, dependency := range dependencies {
		if _, ok := allowed[dependency.Child]; !ok {
			return nil, fmt.Errorf("dependency child session %d must be included in session_ids", dependency.Child)
		}
		parents, ok := parentsByChild[dependency.Child]
		if !ok {
			parents = make(map[int64]struct{}, len(dependency.Parents))
			parentsByChild[dependency.Child] = parents
			childOrder = append(childOrder, dependency.Child)
		}
		for _, parent := range dependency.Parents {
			if _, ok := allowed[parent]; !ok {
				return nil, fmt.Errorf("dependency parent session %d must be included in session_ids", parent)
			}
			parents[parent] = struct{}{}
		}
	}

	visitState := make(map[int64]uint8, len(allowed))
	var visit func(int64) error
	visit = func(sessionID int64) error {
		switch visitState[sessionID] {
		case 1:
			return fmt.Errorf("wave dependencies contain a cycle involving session %d", sessionID)
		case 2:
			return nil
		}
		visitState[sessionID] = 1
		parents := make([]int64, 0, len(parentsByChild[sessionID]))
		for parent := range parentsByChild[sessionID] {
			parents = append(parents, parent)
		}
		sort.Slice(parents, func(i, j int) bool { return parents[i] < parents[j] })
		for _, parent := range parents {
			if err := visit(parent); err != nil {
				return err
			}
		}
		visitState[sessionID] = 2
		return nil
	}
	for _, child := range childOrder {
		if err := visit(child); err != nil {
			return nil, err
		}
	}

	normalized := make([]WaveDependency, 0, len(childOrder))
	for _, child := range childOrder {
		parents := make([]int64, 0, len(parentsByChild[child]))
		for parent := range parentsByChild[child] {
			parents = append(parents, parent)
		}
		sort.Slice(parents, func(i, j int) bool { return parents[i] < parents[j] })
		normalized = append(normalized, WaveDependency{Child: child, Parents: parents})
	}
	return normalized, nil
}

func insertParallelWave(projectID int, projectCWD string, worktreeRoot string, baseSHA string, baseBranch string, orchestrationEpoch int, epicDocID int, lineage parallelWaveLineage, candidates []parallelWaveSessionCandidate, dependencies []WaveDependency, reviewPolicy string, reviewBackend string, reviewModel string, reviewReasoning string) (int, error) {
	hasEpicDocColumn := dbTableHasColumn("parallel_wave", "epic_doc_id")
	if epicDocID > 0 && !hasEpicDocColumn {
		return 0, fmt.Errorf("parallel_wave table is missing epic_doc_id")
	}

	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer func() {
		_ = tx.Rollback()
	}()
	if err := validateParallelWaveLineageBudgetTx(tx, lineage, projectID, len(candidates)); err != nil {
		return 0, err
	}
	if err := validateParallelWaveAdmissionLeasesTx(tx, projectID, candidates); err != nil {
		return 0, err
	}

	var parentWaveID any
	if lineage.ParentWaveID > 0 {
		parentWaveID = lineage.ParentWaveID
	}
	var rootWaveID any
	if lineage.RootWaveID > 0 {
		rootWaveID = lineage.RootWaveID
	}
	insertColumns := "project_id, status, phase, gate_state, control_state, control_reason, base_sha, base_branch, project_cwd, worktree_root, orchestration_epoch, parent_wave_id, root_wave_id, depth, max_child_wave_depth, max_total_descendant_waves, max_total_sessions, review_policy, review_backend, review_model, review_reasoning"
	insertValues := "?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?"
	args := []any{
		projectID,
		parallelWaveStatusCreated,
		parallelWavePhaseInitialized,
		parallelWaveGateNone,
		parallelWaveControlActive,
		"",
		baseSHA,
		baseBranch,
		projectCWD,
		worktreeRoot,
		orchestrationEpoch,
		parentWaveID,
		rootWaveID,
		lineage.Depth,
		lineage.MaxChildWaveDepth,
		lineage.MaxTotalDescendantWaves,
		lineage.MaxTotalSessions,
		reviewPolicy,
		reviewBackend,
		reviewModel,
		reviewReasoning,
	}
	if hasEpicDocColumn {
		insertColumns += ", epic_doc_id"
		insertValues += ", ?"
		args = append(args, nullableEpicDocID(epicDocID))
	}
	insertColumns += ", updated_at"
	insertValues += ", CURRENT_TIMESTAMP"
	result, err := tx.Exec(
		"INSERT INTO parallel_wave ("+insertColumns+") VALUES ("+insertValues+")",
		args...,
	)
	if err != nil {
		return 0, err
	}
	insertID, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	waveID := int(insertID)
	if lineage.RootWaveID == 0 {
		if _, err := tx.Exec(
			"UPDATE parallel_wave SET root_wave_id = ? WHERE id = ? AND project_id = ?",
			waveID,
			waveID,
			projectID,
		); err != nil {
			return 0, err
		}
	}
	if err := insertParallelWaveScopeLeasesTx(tx, projectID, waveID, candidates); err != nil {
		return 0, err
	}
	waveIDText := strconv.Itoa(waveID)
	globalSessionIDByLocalID := make(map[int]int, len(candidates))

	for _, candidate := range candidates {
		globalSessionIDByLocalID[candidate.LocalSessionID] = candidate.GlobalSessionID
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
	for _, dependency := range dependencies {
		childGlobalID := globalSessionIDByLocalID[int(dependency.Child)]
		for _, parent := range dependency.Parents {
			parentGlobalID := globalSessionIDByLocalID[int(parent)]
			if _, err := tx.Exec(
				`INSERT INTO wave_worker_dependency (wave_id, parent_session_id, child_session_id)
				 VALUES (?, ?, ?)`,
				waveID,
				parentGlobalID,
				childGlobalID,
			); err != nil {
				return 0, err
			}
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
		snapshot            NetrunnerWaveSnapshot
		createdBySessionID  int
		acceptanceSessionID int
		launchedAt          string
		completedAt         string
	)
	epicDocColumn := ""
	scanArgs := []any{
		&snapshot.Id,
		&snapshot.ProjectId,
		&snapshot.Status,
		&snapshot.Phase,
		&snapshot.GateState,
		&snapshot.ControlState,
		&snapshot.ControlReason,
		&snapshot.BaseSha,
		&snapshot.BaseBranch,
		&snapshot.ProjectCwd,
		&snapshot.WorktreeRoot,
		&snapshot.OrchestrationEpoch,
		&createdBySessionID,
		&snapshot.ParentWaveId,
		&snapshot.RootWaveId,
		&snapshot.Depth,
		&snapshot.MaxChildWaveDepth,
		&snapshot.MaxTotalDescendantWaves,
		&snapshot.MaxTotalSessions,
		&snapshot.FailurePolicyState,
		&snapshot.RepairWorkerId,
		&snapshot.RepairAttemptCount,
		&snapshot.HandoffSha,
		&acceptanceSessionID,
		&snapshot.ReviewPolicy,
		&snapshot.ReviewBackend,
		&snapshot.ReviewModel,
		&snapshot.ReviewReasoning,
	}
	if dbTableHasColumn("parallel_wave", "epic_doc_id") {
		epicDocColumn = "COALESCE(epic_doc_id, 0),"
		scanArgs = append(scanArgs, &snapshot.EpicDocId)
	}
	scanArgs = append(scanArgs,
		&snapshot.FailureReason,
		&snapshot.CreatedAt,
		&snapshot.UpdatedAt,
		&launchedAt,
		&completedAt,
	)
	err := db.QueryRow(
		`SELECT id,
		        project_id,
		        status,
		        COALESCE(phase, 'initialized'),
		        COALESCE(gate_state, 'none'),
		        COALESCE(control_state, 'active'),
		        COALESCE(control_reason, ''),
		        base_sha,
		        COALESCE(base_branch, ''),
		        project_cwd,
		        worktree_root,
		        COALESCE(orchestration_epoch, 0),
		        COALESCE(created_by_session_id, 0),
		        COALESCE(parent_wave_id, 0),
		        COALESCE(root_wave_id, id),
		        COALESCE(depth, 0),
		        COALESCE(max_child_wave_depth, 0),
		        COALESCE(max_total_descendant_waves, 0),
		        COALESCE(max_total_sessions, 0),
		        COALESCE(failure_policy_state, 'none'),
		        COALESCE(repair_worker_id, 0),
		        COALESCE(repair_attempt_count, 0),
		        COALESCE(handoff_sha, ''),
		        COALESCE(acceptance_session_id, 0),
		        COALESCE(review_policy, 'manual'),
		        COALESCE(review_backend, 'codex'),
		        COALESCE(NULLIF(review_model, 'opencode-go/deepseek-v4-pro'), 'opencode-go/deepseek-v4-flash'),
		        COALESCE(review_reasoning, 'high'),
		        `+epicDocColumn+`
		        COALESCE(failure_reason, ''),
		        created_at,
		        updated_at,
		        COALESCE(launched_at, ''),
		        COALESCE(completed_at, '')
		 FROM parallel_wave
		 WHERE id = ? AND project_id = ?`,
		waveID,
		projectID,
	).Scan(scanArgs...)
	if err != nil {
		return NetrunnerWaveSnapshot{}, err
	}
	snapshot.CreatedBySessionId = createdBySessionID
	if acceptanceSessionID > 0 {
		localAcceptanceID, err := projectScopedSessionIDFromGlobal(acceptanceSessionID, projectID)
		if err != nil {
			return NetrunnerWaveSnapshot{}, err
		}
		snapshot.AcceptanceSessionId = localAcceptanceID
		if err := db.QueryRow(
			"SELECT status, COALESCE(report, '') FROM session WHERE id = ? AND project_id = ?",
			acceptanceSessionID,
			projectID,
		).Scan(&snapshot.AcceptanceSessionStatus, &snapshot.AcceptanceSessionReport); err != nil {
			return NetrunnerWaveSnapshot{}, err
		}
	}
	if snapshot.EpicDocId > 0 {
		localEpicDocID, err := projectScopedDocIDFromGlobal(snapshot.EpicDocId, projectID)
		if err != nil {
			return NetrunnerWaveSnapshot{}, err
		}
		snapshot.EpicDocId = localEpicDocID
	}
	snapshot.LaunchedAt = launchedAt
	snapshot.CompletedAt = completedAt

	rows, err := db.Query(
		`SELECT p.id,
		        p.wave_id,
		        p.project_id,
		        (
		          SELECT COUNT(*)
		          FROM session ranked
		          WHERE ranked.project_id = p.project_id
		            AND ranked.id <= p.session_id
		        ) AS local_session_id,
		        p.status,
		        p.declared_write_scope,
		        p.branch_name,
		        p.worktree_path,
		        p.base_sha,
		        COALESCE(p.head_sha, ''),
		        COALESCE(p.changed_paths, '[]'),
		        COALESCE(p.diff_patch_path, ''),
		        COALESCE(p.diff_stat, ''),
		        COALESCE(p.launch_epoch, 0),
		        COALESCE(p.worker_process_id, 0),
		        COALESCE(p.external_session_id, ''),
		        COALESCE(p.headless_log_path, ''),
		        COALESCE(p.launcher_log_path, ''),
		        COALESCE(p.worker_metadata_path, ''),
		        COALESCE(p.failure_reason, ''),
		        COALESCE(p.terminal_outcome, ''),
		        COALESCE(p.retry_attempt_count, 0),
		        COALESCE(p.retry_cause, ''),
		        COALESCE(p.retry_next_eligible_at, ''),
		        COALESCE(p.cleanup_status, 'pending'),
		        p.created_at,
		        p.updated_at,
		        COALESCE(p.launched_at, ''),
		        COALESCE(p.terminal_at, ''),
		        COALESCE(p.cleaned_at, ''),
		        COALESCE(s.report, '')
		 FROM parallel_wave_worker p
		 LEFT JOIN session s ON s.id = p.session_id
		 WHERE p.wave_id = ? AND p.project_id = ?
		 ORDER BY p.session_id`,
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
			&worker.TerminalOutcome,
			&worker.RetryAttemptCount,
			&worker.RetryCause,
			&worker.RetryNextEligibleAt,
			&worker.CleanupStatus,
			&worker.CreatedAt,
			&worker.UpdatedAt,
			&worker.LaunchedAt,
			&worker.TerminalAt,
			&worker.CleanedAt,
			&worker.SessionReport,
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

	dependencyRows, err := db.Query(
		`SELECT
				(SELECT COUNT(*) FROM session child_ranked WHERE child_ranked.project_id = ? AND child_ranked.id <= dependency.child_session_id) AS child_session_id,
				(SELECT COUNT(*) FROM session parent_ranked WHERE parent_ranked.project_id = ? AND parent_ranked.id <= dependency.parent_session_id) AS parent_session_id
		 FROM wave_worker_dependency AS dependency
		 JOIN session AS child_session ON child_session.id = dependency.child_session_id AND child_session.project_id = ?
		 JOIN session AS parent_session ON parent_session.id = dependency.parent_session_id AND parent_session.project_id = ?
		 WHERE dependency.wave_id = ?
		 ORDER BY dependency.child_session_id, dependency.parent_session_id`,
		projectID,
		projectID,
		projectID,
		projectID,
		waveID,
	)
	if err != nil {
		return NetrunnerWaveSnapshot{}, err
	}
	defer dependencyRows.Close()
	dependencies := []WaveDependency{}
	for dependencyRows.Next() {
		var child, parent int64
		if err := dependencyRows.Scan(&child, &parent); err != nil {
			return NetrunnerWaveSnapshot{}, err
		}
		if len(dependencies) == 0 || dependencies[len(dependencies)-1].Child != child {
			dependencies = append(dependencies, WaveDependency{Child: child, Parents: []int64{}})
		}
		dependencies[len(dependencies)-1].Parents = append(dependencies[len(dependencies)-1].Parents, parent)
	}
	if err := dependencyRows.Err(); err != nil {
		return NetrunnerWaveSnapshot{}, err
	}
	snapshot.Dependencies = dependencies
	if err := updateParallelWaveSnapshotReview(&snapshot, projectID); err != nil {
		return NetrunnerWaveSnapshot{}, err
	}
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
	if err := ensureMCPBinaryRestartNotRequired(authorizedProjectId); err != nil {
		return &mcp.CallToolResult{IsError: true}, CreateNetrunnerWaveOutput{}, err
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
	epicDocID, err := resolveProjectScopedEpicDocID(input.EpicDocId, authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CreateNetrunnerWaveOutput{}, err
	}

	candidates, err := loadParallelWaveSessionCandidates(input.SessionIds, authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CreateNetrunnerWaveOutput{}, err
	}
	dependencySessionIDs := make([]int, 0, len(candidates))
	for _, candidate := range candidates {
		dependencySessionIDs = append(dependencySessionIDs, candidate.LocalSessionID)
	}
	dependencies, err := normalizeWaveDependencies(input.Dependencies, dependencySessionIDs)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CreateNetrunnerWaveOutput{}, err
	}
	admissionWorkers := make([]parallelWaveAdmissionWorker, 0, len(candidates))
	for _, candidate := range candidates {
		admissionWorkers = append(admissionWorkers, parallelWaveAdmissionWorker{
			SessionID:          candidate.LocalSessionID,
			DeclaredWriteScope: candidate.DeclaredWriteScope,
		})
	}
	normalizedAdmission, err := normalizeParallelWaveAdmissionWorkersWithDependencies(admissionWorkers, dependencies)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CreateNetrunnerWaveOutput{}, err
	}
	scopeByLocalID := make(map[int][]string, len(normalizedAdmission))
	for _, worker := range normalizedAdmission {
		scopeByLocalID[worker.SessionID] = worker.DeclaredWriteScope
	}
	for index := range candidates {
		candidates[index].DeclaredWriteScope = scopeByLocalID[candidates[index].LocalSessionID]
	}
	lineage, err := prepareParallelWaveLineage(input, authorizedProjectId, len(candidates))
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CreateNetrunnerWaveOutput{}, err
	}
	if lineage.ParentWaveID > 0 {
		parent, parentErr := fetchParallelWaveLineageRow(lineage.ParentWaveID)
		if parentErr != nil {
			return &mcp.CallToolResult{IsError: true}, CreateNetrunnerWaveOutput{}, fmt.Errorf("failed to re-read parent wave handoff: %v", parentErr)
		}
		if strings.TrimSpace(baseSHA) != strings.TrimSpace(parent.HandoffSHA) {
			return &mcp.CallToolResult{IsError: true}, CreateNetrunnerWaveOutput{}, fmt.Errorf(
				"child wave base_sha %q must match parent wave %d committed handoff_sha %q",
				baseSHA,
				parent.ID,
				parent.HandoffSHA,
			)
		}
	}
	reviewPolicy, reviewBackend, reviewModel, reviewReasoning, err := resolveParallelWaveReviewConfig(
		input.ReviewPolicy,
		input.ReviewBackend,
		input.ReviewModel,
		input.ReviewReasoning,
	)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CreateNetrunnerWaveOutput{}, err
	}
	if reviewPolicy == parallelWaveReviewPolicyAutomatic && len(candidates) == 1 {
		return &mcp.CallToolResult{IsError: true}, CreateNetrunnerWaveOutput{}, fmt.Errorf("automatic review_policy is not allowed for a one-worker wave; use manual Fixer review")
	}
	waveID, err := insertParallelWave(authorizedProjectId, normalizedProjectCWD, worktreeRoot, baseSHA, baseBranch, control.OrchestrationEpoch, epicDocID, lineage, candidates, dependencies, reviewPolicy, reviewBackend, reviewModel, reviewReasoning)
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

const parallelWaveWaitDefaultTimeoutSeconds = 300

func parallelWaveWaitTimeoutSeconds(raw int) (int, error) {
	if raw == 0 {
		return parallelWaveWaitDefaultTimeoutSeconds, nil
	}
	if raw < parallelWaveWaitDefaultTimeoutSeconds {
		return 0, fmt.Errorf("timeout_seconds must be >= %d", parallelWaveWaitDefaultTimeoutSeconds)
	}
	if raw > explicitLaunchMaxWait {
		return 0, fmt.Errorf("timeout_seconds must be <= %d", explicitLaunchMaxWait)
	}
	return raw, nil
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

func parallelWaveWorkersWithoutParents(wave NetrunnerWaveSnapshot) []NetrunnerWaveWorkerSnapshot {
	childrenWithParents := make(map[int64]struct{}, len(wave.Dependencies))
	for _, dependency := range wave.Dependencies {
		if len(dependency.Parents) > 0 {
			childrenWithParents[dependency.Child] = struct{}{}
		}
	}

	workers := make([]NetrunnerWaveWorkerSnapshot, 0, len(wave.Workers))
	for _, worker := range wave.Workers {
		if _, blocked := childrenWithParents[int64(worker.SessionId)]; !blocked {
			workers = append(workers, worker)
		}
	}
	return workers
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
	terminalOutcome := ""
	if _, terminal := parallelWaveWorkerTerminalCondition(status); terminal && status != parallelWaveWorkerStatusCleaned {
		terminalOutcome = status
	}
	_, err := db.Exec(
		`UPDATE parallel_wave_worker
		 SET status = ?,
		     terminal_outcome = CASE WHEN ? <> '' AND COALESCE(TRIM(terminal_outcome), '') = '' THEN ? ELSE terminal_outcome END,
		     failure_reason = ?,
		     updated_at = CURRENT_TIMESTAMP
		 WHERE id = ? AND project_id = ?`,
		status,
		terminalOutcome,
		terminalOutcome,
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
	case parallelWaveWorkerStatusBlocked:
		return "blocked", true
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
		     terminal_outcome = CASE WHEN COALESCE(TRIM(terminal_outcome), '') = '' THEN ? ELSE terminal_outcome END,
		     failure_reason = ?,
		     head_sha = ?,
		     changed_paths = ?,
		     diff_patch_path = ?,
		     diff_stat = ?,
		     terminal_at = COALESCE(terminal_at, CURRENT_TIMESTAMP),
		     updated_at = CURRENT_TIMESTAMP
		 WHERE id = ? AND project_id = ?`,
		status,
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

func validateWorkerCompletionState(projectCWD string, wave NetrunnerWaveSnapshot, worker NetrunnerWaveWorkerSnapshot) error {
	worktreePath, err := resolveParallelWaveWorktreePath(projectCWD, worker.WorktreePath)
	if err != nil {
		return err
	}

	if err := validateWorktreeIsolation(projectCWD, worktreePath); err != nil {
		return fmt.Errorf("worker worktree isolation validation failed: %w", err)
	}

	spec, err := gitCommand(worktreePath, "status", "--porcelain=v1")
	if err != nil {
		return fmt.Errorf("failed to check worktree status: %w", err)
	}
	statusOutput, err := runGitCommandSpec(spec)
	if err != nil {
		return fmt.Errorf("failed to inspect worktree status: %w", err)
	}
	if strings.TrimSpace(statusOutput) != "" {
		return fmt.Errorf("worker worktree has uncommitted or untracked changes: %s", strings.TrimSpace(statusOutput))
	}

	headSHA, err := gitCommandInWorktree(worktreePath, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return fmt.Errorf("failed to inspect worker HEAD: %w", err)
	}
	headSHA = strings.TrimSpace(headSHA)
	baseSHA := strings.TrimSpace(worker.BaseSha)
	if baseSHA == "" {
		baseSHA = strings.TrimSpace(wave.BaseSha)
	}

	if wave.RepairAttemptCount > 0 {
		if headSHA == baseSHA {
			return fmt.Errorf("governed repair completion requires a committed HEAD distinct from base SHA %s", baseSHA)
		}
		if strings.TrimSpace(worker.HeadSha) != "" && headSHA == strings.TrimSpace(worker.HeadSha) {
			return fmt.Errorf("governed repair completion requires a committed HEAD distinct from previous failed head %s", worker.HeadSha)
		}
	}

	trackedNames, err := gitCommandInWorktree(worktreePath, "diff", "--name-only", baseSHA, "--")
	if err == nil {
		for _, path := range splitGitPathLines(trackedNames) {
			if !parallelWaveDeclaredWriteScopeContainsPath(worker.DeclaredWriteScope, path) {
				return fmt.Errorf("worker completion rejected: changed path %q is outside declared write scope %v", path, worker.DeclaredWriteScope)
			}
		}
	}

	return nil
}

func finalizeParallelWaveWorker(projectCWD string, wave NetrunnerWaveSnapshot, worker NetrunnerWaveWorkerSnapshot, status string, failureReason string) (NetrunnerWaveWorkerSnapshot, error) {
	if status == parallelWaveWorkerStatusReviewReady || status == parallelWaveWorkerStatusCompleted {
		if valErr := validateWorkerCompletionState(projectCWD, wave, worker); valErr != nil {
			status = parallelWaveWorkerStatusFailed
			failureReason = valErr.Error()
		}
	}

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
			if updatedWorker.Status == parallelWaveWorkerStatusReviewReady || updatedWorker.Status == parallelWaveWorkerStatusCompleted {
				if leaseErr := releaseParallelWaveWorkerScopeLeases(updatedWave, updatedWorker); leaseErr != nil {
					log.Printf("warning: failed to release terminal worker %d scope leases: %v", updatedWorker.SessionId, leaseErr)
				}
			}
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
		case parallelWaveWorkerStatusFailed, parallelWaveWorkerStatusStaleEpoch, parallelWaveWorkerStatusStopped, parallelWaveWorkerStatusBlocked:
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
	} else {
		err = updateParallelWaveStatus(waveID, projectID, status, failureReason, false)
	}
	if err != nil {
		return err
	}
	if allTerminal {
		if err := markParallelWaveImplementationReviewGate(waveID, projectID); err != nil {
			return err
		}
	}
	refreshed, err := fetchNetrunnerWaveSnapshot(waveID, projectID)
	if err != nil {
		return err
	}
	_, err = reconcileParallelWaveFailureControl(refreshed)
	return err
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
	// Dependency-gated workers do not have a worktree or worker process yet.
	// Leave them non-terminal until the scheduler below either launches them or
	// marks them failed because a parent dependency failed.
	if worker.Status == parallelWaveWorkerStatusCreated {
		return candidate, false, nil
	}
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

		if worker.HeadlessLogPath != "" {
			if b, err := os.ReadFile(worker.HeadlessLogPath); err == nil {
				if isRetryableRateLimit(string(b)) {
					if err := markParallelWaveWorkerProviderRetryWait(worker); err != nil {
						return parallelWaveWaitCandidate{}, false, err
					}
					worker.Status = parallelWaveWorkerStatusRetryWait
					candidate.Worker = worker
					candidate.TerminalCondition = ""
					return candidate, false, nil
				}
			}
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

func markParallelWaveWorkerFailed(workerID int, projectID int, reason string) error {
	_, err := db.Exec(
		`UPDATE parallel_wave_worker
		 SET status = ?,
		     terminal_outcome = CASE WHEN COALESCE(TRIM(terminal_outcome), '') = '' THEN ? ELSE terminal_outcome END,
		     failure_reason = ?,
		     terminal_at = COALESCE(terminal_at, CURRENT_TIMESTAMP),
		     updated_at = CURRENT_TIMESTAMP
		 WHERE id = ? AND project_id = ?`,
		parallelWaveWorkerStatusFailed,
		parallelWaveWorkerStatusFailed,
		reason,
		workerID,
		projectID,
	)
	return err
}

func mergeParallelWaveParentBranches(projectCWD string, childWorktreePath string, parentWorkers []NetrunnerWaveWorkerSnapshot) error {
	if len(parentWorkers) == 0 {
		return nil
	}
	parents := append([]NetrunnerWaveWorkerSnapshot(nil), parentWorkers...)
	sort.Slice(parents, func(i, j int) bool {
		return parents[i].SessionId < parents[j].SessionId
	})
	for _, parent := range parents {
		parentWorktreePath, err := resolveParallelWaveWorktreePath(projectCWD, parent.WorktreePath)
		if err != nil {
			return fmt.Errorf("parent session %d: invalid worktree path: %w", parent.SessionId, err)
		}
		observedHead, err := gitCommandInWorktree(parentWorktreePath, "rev-parse", "--verify", "HEAD^{commit}")
		if err != nil || strings.TrimSpace(observedHead) == "" || strings.TrimSpace(observedHead) != strings.TrimSpace(parent.HeadSha) {
			return fmt.Errorf("parent session %d has no stable committed handoff (stored=%q observed=%q)", parent.SessionId, parent.HeadSha, observedHead)
		}
		dirty, err := gitCommandInWorktree(parentWorktreePath, "status", "--porcelain=v1")
		if err != nil {
			return fmt.Errorf("parent session %d: failed to verify committed handoff: %w", parent.SessionId, err)
		}
		if strings.TrimSpace(dirty) != "" {
			return fmt.Errorf("parent session %d has uncommitted changes; committed handoff is required", parent.SessionId)
		}
		spec, err := gitCommand(childWorktreePath, "merge", "--no-edit", strings.TrimSpace(parent.HeadSha))
		if err != nil {
			return fmt.Errorf("parent session %d: %w", parent.SessionId, err)
		}
		if _, err := runGitCommandSpec(spec); err != nil {
			return fmt.Errorf("parent session %d: failed to merge committed handoff %s: %w", parent.SessionId, parent.HeadSha, err)
		}
	}
	return nil
}

func refreshParallelWaveDependencyParents(projectCWD string, wave NetrunnerWaveSnapshot) error {
	parentSessionIDs := make(map[int]struct{})
	for _, dependency := range wave.Dependencies {
		for _, parentSessionID := range dependency.Parents {
			parentSessionIDs[int(parentSessionID)] = struct{}{}
		}
	}
	for _, worker := range wave.Workers {
		if _, isParent := parentSessionIDs[worker.SessionId]; !isParent || worker.Status == parallelWaveWorkerStatusCreated {
			continue
		}
		if _, _, err := inspectParallelWaveWorkerForWait(projectCWD, wave, worker); err != nil {
			return fmt.Errorf("failed to refresh dependency parent %d: %v", worker.SessionId, err)
		}
	}
	return nil
}

func scheduleCreatedParallelWaveWorkers(
	ctx context.Context,
	projectCWD string,
	wave NetrunnerWaveSnapshot,
	launchEpoch int,
	startupTimeout time.Duration,
) error {
	workerBySessionID := make(map[int]NetrunnerWaveWorkerSnapshot, len(wave.Workers))
	for _, worker := range wave.Workers {
		workerBySessionID[worker.SessionId] = worker
	}

	parentsByChild := make(map[int][]int64, len(wave.Dependencies))
	for _, dependency := range wave.Dependencies {
		parentsByChild[int(dependency.Child)] = append(parentsByChild[int(dependency.Child)], dependency.Parents...)
	}

	for _, worker := range wave.Workers {
		if worker.Status != parallelWaveWorkerStatusCreated {
			continue
		}

		readyToLaunch := true
		dependencyFailed := false
		for _, parentSessionID := range parentsByChild[worker.SessionId] {
			parent, found := workerBySessionID[int(parentSessionID)]
			if !found {
				if err := markParallelWaveWorkerFailed(worker.Id, authorizedProjectId, "parent dependency missing"); err != nil {
					return fmt.Errorf("failed to mark child worker %d failed: %v", worker.SessionId, err)
				}
				dependencyFailed = true
				break
			}

			switch parent.Status {
			case parallelWaveWorkerStatusReviewReady, parallelWaveWorkerStatusCleaned, parallelWaveWorkerStatusCompleted:
			case parallelWaveWorkerStatusFailed:
				reason := fmt.Sprintf("parent session %d: %s", parent.SessionId, parent.FailureReason)
				if strings.TrimSpace(parent.FailureReason) == "" {
					reason = fmt.Sprintf("parent session %d failed", parent.SessionId)
				}
				if err := markParallelWaveWorkerFailed(worker.Id, authorizedProjectId, reason); err != nil {
					return fmt.Errorf("failed to mark child worker %d failed: %v", worker.SessionId, err)
				}
				dependencyFailed = true
			default:
				readyToLaunch = false
			}
			if dependencyFailed {
				break
			}
		}
		if dependencyFailed || !readyToLaunch || (wave.FailurePolicyState != parallelWaveFailurePolicyNone && wave.FailurePolicyState != parallelWaveFailurePolicyPassed) {
			continue
		}
		parentWorkers := make([]NetrunnerWaveWorkerSnapshot, 0, len(parentsByChild[worker.SessionId]))
		for _, parentSessionID := range parentsByChild[worker.SessionId] {
			parent, found := workerBySessionID[int(parentSessionID)]
			if !found {
				continue
			}
			parentWorkers = append(parentWorkers, parent)
		}

		worktreePath, err := resolveParallelWaveWorktreePath(projectCWD, worker.WorktreePath)
		if err != nil {
			if markErr := markParallelWaveWorkerFailed(worker.Id, authorizedProjectId, err.Error()); markErr != nil {
				return fmt.Errorf("failed to mark child worker %d failed: %v", worker.SessionId, markErr)
			}
			continue
		}
		if _, statErr := os.Stat(worktreePath); statErr == nil {
			reason := fmt.Sprintf("worker worktree path already exists: %s", worktreePath)
			if markErr := markParallelWaveWorkerFailed(worker.Id, authorizedProjectId, reason); markErr != nil {
				return fmt.Errorf("failed to mark child worker %d failed: %v", worker.SessionId, markErr)
			}
			continue
		} else if !os.IsNotExist(statErr) {
			reason := fmt.Sprintf("failed to inspect worker worktree %s: %v", worktreePath, statErr)
			if markErr := markParallelWaveWorkerFailed(worker.Id, authorizedProjectId, reason); markErr != nil {
				return fmt.Errorf("failed to mark child worker %d failed: %v", worker.SessionId, markErr)
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
			reason := fmt.Sprintf("failed to prepare worker worktree parent: %v", err)
			if markErr := markParallelWaveWorkerFailed(worker.Id, authorizedProjectId, reason); markErr != nil {
				return fmt.Errorf("failed to mark child worker %d failed: %v", worker.SessionId, markErr)
			}
			continue
		}
		spec, err := gitWorktreeAddCommand(projectCWD, worktreePath, worker.BranchName, wave.BaseSha)
		if err == nil {
			_, err = runGitCommandSpec(spec)
		}
		if err != nil {
			reason := fmt.Sprintf("failed to create worker worktree: %v", err)
			if markErr := markParallelWaveWorkerFailed(worker.Id, authorizedProjectId, reason); markErr != nil {
				return fmt.Errorf("failed to mark child worker %d failed: %v", worker.SessionId, markErr)
			}
			continue
		}
		if err := mergeParallelWaveParentBranches(projectCWD, worktreePath, parentWorkers); err != nil {
			rollbackFailures := rollbackParallelWaveWorktrees(projectCWD, []string{worktreePath})
			reason := fmt.Sprintf("failed to integrate parent branches: %v", err)
			if len(rollbackFailures) > 0 {
				reason += "; rollback failures: " + strings.Join(rollbackFailures, "; ")
			}
			if markErr := markParallelWaveWorkerFailed(worker.Id, authorizedProjectId, reason); markErr != nil {
				return fmt.Errorf("failed to mark child worker %d failed: %v", worker.SessionId, markErr)
			}
			continue
		}
		if err := updateParallelWaveWorkerStatus(worker.Id, authorizedProjectId, parallelWaveWorkerStatusWorktreeReady, ""); err != nil {
			if markErr := markParallelWaveWorkerFailed(worker.Id, authorizedProjectId, err.Error()); markErr != nil {
				return fmt.Errorf("failed to mark child worker %d failed: %v", worker.SessionId, markErr)
			}
			continue
		}

		if err := launchParallelWaveWorkerProcess(
			ctx,
			projectCWD,
			wave,
			worker,
			worktreePath,
			LaunchNetrunnerWaveInput{},
			launchEpoch,
			startupTimeout,
		); err != nil {
			if markErr := markParallelWaveWorkerFailed(worker.Id, authorizedProjectId, err.Error()); markErr != nil {
				return fmt.Errorf("failed to mark child worker %d failed: %v", worker.SessionId, markErr)
			}
		}
	}
	return nil
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

// abandonNeverLaunchedParallelWaveWorkers deterministically closes out workers
// whose worktrees were prepared but whose process never launched: mark them
// failed, remove their worktrees, and release their scope leases so they do
// not remain stranded until whole-wave cleanup.
func abandonNeverLaunchedParallelWaveWorkers(projectCWD string, waveID int, projectID int, workerFailures map[int]string, worktreePaths []string) []string {
	for workerID, reason := range workerFailures {
		_ = markParallelWaveWorkerFailed(workerID, projectID, reason)
	}
	rollbackFailures := rollbackParallelWaveWorktrees(projectCWD, worktreePaths)
	if len(rollbackFailures) > 0 {
		log.Printf("warning: wave %d launch-failure worktree rollback: %s", waveID, strings.Join(rollbackFailures, "; "))
	}
	partialWave, err := fetchNetrunnerWaveSnapshot(waveID, projectID)
	if err != nil {
		log.Printf("warning: failed to fetch wave %d after launch abandonment: %v", waveID, err)
		return rollbackFailures
	}
	for _, worker := range partialWave.Workers {
		if worker.Status != parallelWaveWorkerStatusFailed {
			continue
		}
		if leaseErr := releaseParallelWaveWorkerScopeLeases(partialWave, worker); leaseErr != nil {
			log.Printf("warning: failed to release abandoned worker %d scope leases: %v", worker.SessionId, leaseErr)
		}
	}
	return rollbackFailures
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
	if wave.ControlState == parallelWaveControlPausedForArchitect {
		return &mcp.CallToolResult{IsError: true}, LaunchNetrunnerWaveOutput{}, fmt.Errorf("wave %d is paused_for_architect: %s", wave.Id, wave.ControlReason)
	}
	if err := validateParallelWaveLaunchState(wave); err != nil {
		return &mcp.CallToolResult{IsError: true}, LaunchNetrunnerWaveOutput{}, err
	}
	if len(wave.Workers) == 0 {
		return &mcp.CallToolResult{IsError: true}, LaunchNetrunnerWaveOutput{}, fmt.Errorf("wave %d has no workers to launch", wave.Id)
	}
	wave, err = applyParallelWaveReviewConfig(wave, input)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, LaunchNetrunnerWaveOutput{}, err
	}

	control, _, err := fetchOrchestrationControl(authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, LaunchNetrunnerWaveOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	if control.OrchestrationFrozen {
		return &mcp.CallToolResult{IsError: true}, LaunchNetrunnerWaveOutput{}, fmt.Errorf("orchestration is frozen; resume orchestration before launching a parallel wave")
	}
	if err := ensureMCPBinaryRestartNotRequired(authorizedProjectId); err != nil {
		return &mcp.CallToolResult{IsError: true}, LaunchNetrunnerWaveOutput{}, err
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
	launchWave := wave
	launchWave.Workers = parallelWaveWorkersWithoutParents(wave)
	if len(launchWave.Workers) == 0 {
		return &mcp.CallToolResult{IsError: true}, LaunchNetrunnerWaveOutput{}, fmt.Errorf("wave %d has no dependency-free workers to launch", wave.Id)
	}
	worktreePathByWorkerID, err := ensureParallelWaveWorkerPathsAvailable(normalizedProjectCWD, launchWave)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, LaunchNetrunnerWaveOutput{}, err
	}
	workerLaunchInputs, err := prepareParallelWaveWorkerLaunchConfigs(ctx, wave, input)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, LaunchNetrunnerWaveOutput{}, err
	}

	if err := updateParallelWaveStatus(wave.Id, authorizedProjectId, parallelWaveStatusLaunching, "", false); err != nil {
		return &mcp.CallToolResult{IsError: true}, LaunchNetrunnerWaveOutput{}, fmt.Errorf("DB update error: %v", err)
	}
	if err := markParallelWaveImplementationStarted(wave.Id, authorizedProjectId); err != nil {
		return &mcp.CallToolResult{IsError: true}, LaunchNetrunnerWaveOutput{}, fmt.Errorf("DB update error: %v", err)
	}
	createdWorktrees, err := createParallelWaveWorktrees(normalizedProjectCWD, launchWave, worktreePathByWorkerID)
	if err != nil {
		reason := err.Error()
		workerFailures := make(map[int]string, len(launchWave.Workers))
		for _, worker := range launchWave.Workers {
			workerFailures[worker.Id] = "launch aborted: " + reason
		}
		rollbackFailures := abandonNeverLaunchedParallelWaveWorkers(normalizedProjectCWD, wave.Id, authorizedProjectId, workerFailures, createdWorktrees)
		if len(rollbackFailures) > 0 {
			reason += "; rollback failures: " + strings.Join(rollbackFailures, "; ")
		}
		_ = updateParallelWaveStatus(wave.Id, authorizedProjectId, parallelWaveStatusFailed, reason, false)
		return &mcp.CallToolResult{IsError: true}, LaunchNetrunnerWaveOutput{}, errors.New(reason)
	}

	launchedWorkers := 0
	for workerIndex, worker := range launchWave.Workers {
		worktreePath := worktreePathByWorkerID[worker.Id]
		workerInput := workerLaunchInputs[worker.SessionId]
		if err := launchParallelWaveWorkerProcess(ctx, normalizedProjectCWD, wave, worker, worktreePath, workerInput, control.OrchestrationEpoch, startupTimeout); err != nil {
			status := parallelWaveStatusFailed
			if launchedWorkers > 0 {
				status = parallelWaveStatusPartiallyFailed
			}
			workerFailures := map[int]string{worker.Id: err.Error()}
			abandonedWorktreePaths := []string{worktreePath}
			for _, remaining := range launchWave.Workers[workerIndex+1:] {
				reason := fmt.Sprintf("launch abandoned after session %d launch failure", worker.SessionId)
				workerFailures[remaining.Id] = reason
				abandonedWorktreePaths = append(abandonedWorktreePaths, worktreePathByWorkerID[remaining.Id])
			}
			_ = updateParallelWaveStatus(wave.Id, authorizedProjectId, status, err.Error(), launchedWorkers > 0)
			abandonNeverLaunchedParallelWaveWorkers(normalizedProjectCWD, wave.Id, authorizedProjectId, workerFailures, abandonedWorktreePaths)
			partialWave, fetchErr := fetchNetrunnerWaveSnapshot(wave.Id, authorizedProjectId)
			if fetchErr != nil {
				return &mcp.CallToolResult{IsError: true}, LaunchNetrunnerWaveOutput{}, err
			}
			partialWave, fetchErr = reconcileParallelWaveFailureControl(partialWave)
			if fetchErr != nil {
				return &mcp.CallToolResult{IsError: true}, LaunchNetrunnerWaveOutput{}, fetchErr
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

	timeoutSeconds, err := parallelWaveWaitTimeoutSeconds(input.TimeoutSeconds)
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
	deferredLaunchTimeout, err := parallelWaveLaunchStartupTimeoutSeconds(0)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, WaitForNetrunnerWaveOutput{}, err
	}

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
		binaryState, err := fetchMCPBinaryRestartState(authorizedProjectId)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, WaitForNetrunnerWaveOutput{}, fmt.Errorf("DB query error: %v", err)
		}
		followUpAllowed, blockedReason := parallelWaveFollowUpDecision(control, wave, binaryState)
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

		if err := refreshParallelWaveDependencyParents(normalizedProjectCWD, wave); err != nil {
			return &mcp.CallToolResult{IsError: true}, WaitForNetrunnerWaveOutput{}, err
		}
		wave, err = fetchNetrunnerWaveSnapshot(input.WaveId, authorizedProjectId)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, WaitForNetrunnerWaveOutput{}, fmt.Errorf("DB query error: %v", err)
		}
		wave, err = reconcileParallelWaveFailureControl(wave)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, WaitForNetrunnerWaveOutput{}, fmt.Errorf("DB update error: %v", err)
		}
		followUpAllowed, blockedReason = parallelWaveFollowUpDecision(control, wave, binaryState)
		if !followUpAllowed {
			result := buildParallelWaveWaitResult(startedAt, timeoutSeconds, pollIntervalSeconds, returnWhen, wave, nil, true, "follow_up_blocked", false, false, blockedReason, control)
			return nil, WaitForNetrunnerWaveOutput{Status: "blocked", Result: result}, nil
		}
		if err := processParallelWaveGovernedRepair(ctx, normalizedProjectCWD, wave, deferredLaunchTimeout); err != nil {
			return &mcp.CallToolResult{IsError: true}, WaitForNetrunnerWaveOutput{}, fmt.Errorf("failed to process governed implementation repair: %v", err)
		}
		wave, err = fetchNetrunnerWaveSnapshot(input.WaveId, authorizedProjectId)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, WaitForNetrunnerWaveOutput{}, fmt.Errorf("DB query error: %v", err)
		}
		if err := scheduleCreatedParallelWaveWorkers(ctx, normalizedProjectCWD, wave, control.OrchestrationEpoch, deferredLaunchTimeout); err != nil {
			return &mcp.CallToolResult{IsError: true}, WaitForNetrunnerWaveOutput{}, fmt.Errorf("failed to schedule dependency-gated wave workers: %v", err)
		}
		wave, err = fetchNetrunnerWaveSnapshot(input.WaveId, authorizedProjectId)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, WaitForNetrunnerWaveOutput{}, fmt.Errorf("DB query error: %v", err)
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

		if err := processParallelWaveWorkerRetries(ctx, normalizedProjectCWD, wave, deferredLaunchTimeout); err != nil {
			return &mcp.CallToolResult{IsError: true}, WaitForNetrunnerWaveOutput{}, fmt.Errorf("failed to process worker retries: %v", err)
		}
		wave, err = fetchNetrunnerWaveSnapshot(input.WaveId, authorizedProjectId)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, WaitForNetrunnerWaveOutput{}, fmt.Errorf("DB query error: %v", err)
		}

		if err := refreshParallelWaveAggregateStatus(wave.Id, authorizedProjectId); err != nil {
			return &mcp.CallToolResult{IsError: true}, WaitForNetrunnerWaveOutput{}, fmt.Errorf("DB update error: %v", err)
		}
		wave, err = fetchNetrunnerWaveSnapshot(wave.Id, authorizedProjectId)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, WaitForNetrunnerWaveOutput{}, fmt.Errorf("DB query error: %v", err)
		}
		// Reconcile after this iteration's inspection/finalization and before
		// reviewer creation. A just-failed deferred worker must close the gate
		// immediately instead of inheriting an earlier passed decision.
		wave, err = reconcileParallelWaveFailureControl(wave)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, WaitForNetrunnerWaveOutput{}, fmt.Errorf("DB update error: %v", err)
		}
		allTerminal = parallelWaveAllWorkersTerminal(wave)

		if len(qualifying) > 0 {
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
		followUpAllowed, blockedReason = parallelWaveFollowUpDecision(control, wave, binaryState)
		if !followUpAllowed {
			result := buildParallelWaveWaitResult(startedAt, timeoutSeconds, pollIntervalSeconds, returnWhen, wave, nil, true, "follow_up_blocked", false, false, blockedReason, control)
			return nil, WaitForNetrunnerWaveOutput{Status: "blocked", Result: result}, nil
		}
		reviewPolicy := strings.TrimSpace(wave.ReviewPolicy)
		if reviewPolicy == "" {
			reviewPolicy = defaultParallelWaveReviewPolicy
		}
		if allTerminal && reviewPolicy == parallelWaveReviewPolicyAutomatic {
			if err := ensureParallelWaveReviewer(ctx, wave); err != nil {
				// Reviewer launch is deliberately non-fatal for the worker result: the
				// failure is persisted on the reviewer session for Architect follow-up.
				log.Printf("wait_for_netrunner_wave project_id=%d wave_id=%d reviewer launch failed: %v", authorizedProjectId, wave.Id, err)
			}
			wave, err = fetchNetrunnerWaveSnapshot(wave.Id, authorizedProjectId)
			if err != nil {
				return &mcp.CallToolResult{IsError: true}, WaitForNetrunnerWaveOutput{}, fmt.Errorf("DB query error: %v", err)
			}
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

			var reports []string
			for _, w := range wave.Workers {
				if w.SessionReport != "" {
					reports = append(reports, w.SessionReport)
				}
			}
			if wave.ReviewSessionReport != "" {
				reports = append(reports, wave.ReviewSessionReport)
			}

			log.Printf("wait_for_netrunner_wave project_id=%d wave_id=%d return_when=%q all_terminal=true", authorizedProjectId, wave.Id, returnWhen)
			return nil, WaitForNetrunnerWaveOutput{Status: "success", Result: result, Reports: reports}, nil
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
			binaryState, err = fetchMCPBinaryRestartState(authorizedProjectId)
			if err != nil {
				return &mcp.CallToolResult{IsError: true}, WaitForNetrunnerWaveOutput{}, fmt.Errorf("DB query error: %v", err)
			}
			followUpAllowed, blockedReason = parallelWaveFollowUpDecision(control, wave, binaryState)
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

	wave, err = reconcileStaleParallelWaveWorkers(normalizedProjectCWD, wave)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CleanupNetrunnerWaveOutput{}, fmt.Errorf("failed to reconcile stale wave workers: %v", err)
	}
	if err := validateParallelWaveCleanupPreconditions(wave, authorizedProjectId); err != nil {
		return &mcp.CallToolResult{IsError: true}, CleanupNetrunnerWaveOutput{}, err
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

func parallelWaveFollowUpDecision(control orchestrationControl, wave NetrunnerWaveSnapshot, binaryState MCPBinaryRestartState) (bool, string) {
	reasons := []string{}
	if wave.ControlState == parallelWaveControlPausedForArchitect {
		reason := "wave_paused_for_architect"
		if strings.TrimSpace(wave.ControlReason) != "" {
			reason += ":" + wave.ControlReason
		}
		reasons = append(reasons, reason)
	}
	if wave.FailurePolicyState == parallelWaveFailurePolicyRepairRequired {
		reasons = append(reasons, "governed_implementation_repair_required")
	}
	if control.OrchestrationFrozen {
		reasons = append(reasons, "project_orchestration_frozen")
	}
	if binaryState.RestartRequired {
		reasons = append(reasons, fmt.Sprintf("mcp_binary_restart_required:%d", binaryState.RequiredBuildEpoch))
	}
	if control.OrchestrationEpoch != wave.OrchestrationEpoch {
		reasons = append(reasons, fmt.Sprintf("stale_orchestration_epoch:%d->%d", wave.OrchestrationEpoch, control.OrchestrationEpoch))
	}
	if len(reasons) > 0 {
		return false, strings.Join(reasons, ",")
	}
	return true, ""
}

func processParallelWaveGovernedRepair(ctx context.Context, projectCWD string, wave NetrunnerWaveSnapshot, deferredLaunchTimeout time.Duration) error {
	if wave.FailurePolicyState != parallelWaveFailurePolicyRepairAuthorized || wave.RepairAttemptCount != 1 || wave.RepairWorkerId <= 0 {
		return nil
	}
	if err := ensureMCPBinaryRestartNotRequired(wave.ProjectId); err != nil {
		return err
	}
	var worker NetrunnerWaveWorkerSnapshot
	for _, candidate := range wave.Workers {
		if candidate.Id == wave.RepairWorkerId {
			worker = candidate
			break
		}
	}
	if worker.Id == 0 || worker.Status != parallelWaveWorkerStatusRepairWait {
		return fmt.Errorf("wave %d governed repair worker is missing or not repair_wait", wave.Id)
	}
	claim, err := db.Exec(
		`UPDATE parallel_wave_worker
		 SET status = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE id = ? AND project_id = ? AND status = ?`,
		parallelWaveWorkerStatusLaunching,
		worker.Id,
		wave.ProjectId,
		parallelWaveWorkerStatusRepairWait,
	)
	if err != nil {
		return err
	}
	claimed, _ := claim.RowsAffected()
	if claimed == 0 {
		return nil
	}
	if _, err := db.Exec(
		`UPDATE parallel_wave SET failure_policy_state = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND project_id = ?`,
		parallelWaveFailurePolicyRepairInProgress,
		wave.Id,
		wave.ProjectId,
	); err != nil {
		return err
	}
	worktreePath, err := ensureGovernedRepairWorktreeReady(projectCWD, wave, worker)
	if err == nil {
		err = launchParallelWaveWorkerProcess(ctx, projectCWD, wave, worker, worktreePath, LaunchNetrunnerWaveInput{}, wave.OrchestrationEpoch, deferredLaunchTimeout)
	}
	if err != nil {
		if markErr := markParallelWaveWorkerFailed(worker.Id, wave.ProjectId, fmt.Sprintf("governed repair launch failed: %v", err)); markErr != nil {
			return markErr
		}
		refreshed, fetchErr := fetchNetrunnerWaveSnapshot(wave.Id, wave.ProjectId)
		if fetchErr != nil {
			return fetchErr
		}
		_, reconcileErr := reconcileParallelWaveFailureControl(refreshed)
		return reconcileErr
	}
	return nil
}

func backendToProviderName(backend string) string {
	switch backend {
	case "claude":
		return "Claude Code"
	case "codex":
		return "Codex"
	case "commandcode":
		return "CommandCode"
	case "antigravity":
		return "Agy/Gemini"
	// droid (Factory CLI) and other backends have no check-my-limits bucket:
	// return "" so the quota gate falls back to plain backoff.
	default:
		return ""
	}
}

func markParallelWaveWorkerProviderRetryWait(worker NetrunnerWaveWorkerSnapshot) error {
	nextEligible := time.Now().UTC().Add(calculateBackoff(worker.RetryAttemptCount)).Format(time.RFC3339Nano)
	_, err := db.Exec(
		`UPDATE parallel_wave_worker
		 SET status = ?, retry_cause = ?, retry_next_eligible_at = ?, failure_reason = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE id = ? AND project_id = ?`,
		parallelWaveWorkerStatusRetryWait,
		"provider_rate_limit",
		nextEligible,
		"retryable provider rate limit",
		worker.Id,
		worker.ProjectId,
	)
	return err
}

func parseParallelWaveRetryEligibility(raw string) time.Time {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		if parsed, err := time.Parse(layout, trimmed); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func processParallelWaveWorkerRetries(ctx context.Context, projectCWD string, wave NetrunnerWaveSnapshot, deferredLaunchTimeout time.Duration) error {
	if err := ensureMCPBinaryRestartNotRequired(wave.ProjectId); err != nil {
		return err
	}
	for _, worker := range wave.Workers {
		if worker.Status != parallelWaveWorkerStatusRetryWait {
			continue
		}

		globalSessionID, err := globalSessionIDFromProjectScoped(worker.SessionId, wave.ProjectId)
		if err != nil {
			continue
		}
		_, _, _, backend, _, _, _, err := fetchSessionWaitSnapshot(globalSessionID, wave.ProjectId)
		if err != nil {
			continue
		}
		providerName := backendToProviderName(backend)

		attempts := worker.RetryAttemptCount
		if attempts >= 5 {
			if err := updateParallelWaveWorkerStatus(worker.Id, wave.ProjectId, parallelWaveWorkerStatusBlocked, "blocked: max retries reached"); err != nil {
				return err
			}
			botToken, chatID, apiBaseURL, envErr := resolveTelegramOperatorConfigFromEnv()
			if envErr == nil {
				_ = sendTelegramText(ctx, botToken, chatID, apiBaseURL, fmt.Sprintf("Worker %d blocked after max retries", worker.SessionId))
			}
			continue
		}

		delay := calculateBackoff(attempts)
		nextEligible := parseParallelWaveRetryEligibility(worker.RetryNextEligibleAt)
		if nextEligible.IsZero() {
			nextEligible = time.Now().UTC().Add(delay)
		}

		quotaDelay := delay
		if DefaultQuotaGate != nil && providerName != "" {
			q, found, err := DefaultQuotaGate.CheckQuota(providerName)
			if err != nil {
				log.Printf("quota gate check failed for %s: %v", providerName, err)
			} else if found && q.PercentLeft == 0 {
				quotaDelay = q.ResetDelay + 5*time.Minute
			}
		}

		if quotaDelay > delay {
			quotaEligible := time.Now().UTC().Add(quotaDelay)
			if quotaEligible.After(nextEligible) {
				nextEligible = quotaEligible
			}
		}

		if !time.Now().UTC().Before(nextEligible) {
			claim, err := db.Exec(
				`UPDATE parallel_wave_worker
				 SET retry_attempt_count = retry_attempt_count + 1,
				     retry_next_eligible_at = '',
				     status = ?,
				     updated_at = CURRENT_TIMESTAMP
				 WHERE id = ? AND project_id = ? AND status = ? AND retry_attempt_count = ?`,
				parallelWaveWorkerStatusLaunching,
				worker.Id,
				wave.ProjectId,
				parallelWaveWorkerStatusRetryWait,
				attempts,
			)
			if err != nil {
				return err
			}
			claimed, _ := claim.RowsAffected()
			if claimed == 0 {
				continue
			}
			worktreePath, err := resolveParallelWaveWorktreePath(projectCWD, worker.WorktreePath)
			if err == nil {
				err = launchParallelWaveWorkerProcess(ctx, projectCWD, wave, worker, worktreePath, LaunchNetrunnerWaveInput{}, wave.OrchestrationEpoch, deferredLaunchTimeout)
			}
			if err != nil {
				_ = markParallelWaveWorkerFailed(worker.Id, wave.ProjectId, fmt.Sprintf("retry launch failed: %v", err))
			}
		}
	}
	return nil
}

func isRetryableRateLimit(logContent string) bool {
	lower := strings.ToLower(logContent)
	return strings.Contains(lower, "429") || strings.Contains(lower, "rate limit") || strings.Contains(lower, "too many requests")
}

func calculateBackoff(attempts int) time.Duration {
	delayMinutes := 1 << attempts
	if delayMinutes > 15 {
		delayMinutes = 15
	}
	return time.Duration(delayMinutes)*time.Minute + time.Duration(attempts)*time.Second
}
