package main

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	parallelWaveBatchDetailSummary = "summary"
	parallelWaveBatchDetailFull    = "full"
)

type LaunchNetrunnerWavesInput struct {
	WaveIds         []int                      `json:"wave_ids,omitempty" jsonschema:"One or more existing project-scoped wave IDs. Uses the shared launch defaults below."`
	Waves           []LaunchNetrunnerWaveInput `json:"waves,omitempty" jsonschema:"Optional per-wave launch inputs. Mutually exclusive with wave_ids."`
	Backend         string                     `json:"backend,omitempty" jsonschema:"Shared backend used with wave_ids."`
	Model           string                     `json:"model,omitempty" jsonschema:"Shared model used with wave_ids."`
	Reasoning       string                     `json:"reasoning,omitempty" jsonschema:"Shared reasoning used with wave_ids."`
	ReviewPolicy    string                     `json:"review_policy,omitempty" jsonschema:"Shared review policy used with wave_ids: manual (default) or automatic."`
	ReviewBackend   string                     `json:"review_backend,omitempty" jsonschema:"Shared automatic reviewer backend used with wave_ids."`
	ReviewModel     string                     `json:"review_model,omitempty" jsonschema:"Shared automatic reviewer model used with wave_ids."`
	ReviewReasoning string                     `json:"review_reasoning,omitempty" jsonschema:"Shared automatic reviewer reasoning used with wave_ids."`
	FixerSessionId  string                     `json:"fixer_session_id,omitempty" jsonschema:"Shared Fixer session ID used with wave_ids."`
	TimeoutSeconds  int                        `json:"timeout_seconds,omitempty" jsonschema:"Shared launch startup timeout used with wave_ids."`
	DetailLevel     string                     `json:"detail_level,omitempty" jsonschema:"Response detail: summary (default) or full. Full preserves the legacy per-wave output payload."`
}

type LaunchNetrunnerWaveBatchResult struct {
	WaveId  int                           `json:"wave_id"`
	Status  string                        `json:"status"`
	Error   string                        `json:"error,omitempty"`
	Summary *NetrunnerWaveOperatorSummary `json:"summary,omitempty"`
	Output  *LaunchNetrunnerWaveOutput    `json:"output,omitempty"`
}

type LaunchNetrunnerWavesOutput struct {
	Status      string                           `json:"status"`
	DetailLevel string                           `json:"detail_level"`
	Results     []LaunchNetrunnerWaveBatchResult `json:"results"`
}

type WaitForNetrunnerWavesInput struct {
	WaveIds             []int                       `json:"wave_ids,omitempty" jsonschema:"One or more existing project-scoped wave IDs. Uses the shared wait settings below."`
	Waves               []WaitForNetrunnerWaveInput `json:"waves,omitempty" jsonschema:"Optional per-wave wait inputs. Mutually exclusive with wave_ids."`
	TimeoutSeconds      int                         `json:"timeout_seconds,omitempty" jsonschema:"Shared wait timeout used with wave_ids."`
	PollIntervalSeconds int                         `json:"poll_interval_seconds,omitempty" jsonschema:"Shared poll interval used with wave_ids."`
	ReturnWhen          string                      `json:"return_when,omitempty" jsonschema:"Shared return condition used with wave_ids."`
	DetailLevel         string                      `json:"detail_level,omitempty" jsonschema:"Response detail: summary (default) or full. Full preserves the legacy per-wave output payload."`
}

type NetrunnerWaveBatchWaitState struct {
	WaitResultTerminal    bool   `json:"wait_result_terminal"`
	TerminalCondition     string `json:"terminal_condition"`
	TimedOut              bool   `json:"timed_out"`
	WinningSessionId      int    `json:"winning_session_id,omitempty"`
	WorkerId              int    `json:"worker_id,omitempty"`
	WinningWorkerStatus   string `json:"winning_worker_status,omitempty"`
	WinningWorkerTerminal bool   `json:"winning_worker_terminal"`
	AllWorkersTerminal    bool   `json:"all_workers_terminal"`
	FollowUpAllowed       bool   `json:"follow_up_allowed"`
	FollowUpBlockedReason string `json:"follow_up_blocked_reason,omitempty"`
}

type WaitForNetrunnerWaveBatchResult struct {
	WaveId  int                           `json:"wave_id"`
	Status  string                        `json:"status"`
	Error   string                        `json:"error,omitempty"`
	Summary *NetrunnerWaveOperatorSummary `json:"summary,omitempty"`
	Wait    *NetrunnerWaveBatchWaitState  `json:"wait,omitempty"`
	Output  *WaitForNetrunnerWaveOutput   `json:"output,omitempty"`
}

type WaitForNetrunnerWavesOutput struct {
	Status      string                            `json:"status"`
	DetailLevel string                            `json:"detail_level"`
	Results     []WaitForNetrunnerWaveBatchResult `json:"results"`
}

func parallelWaveBatchDetailLevel(raw string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	if normalized == "" {
		return parallelWaveBatchDetailSummary, nil
	}
	switch normalized {
	case parallelWaveBatchDetailSummary, parallelWaveBatchDetailFull:
		return normalized, nil
	default:
		return "", fmt.Errorf("unsupported detail_level %q; supported values are %q and %q", raw, parallelWaveBatchDetailSummary, parallelWaveBatchDetailFull)
	}
}

func parallelWaveBatchSummary(waveID int, wave NetrunnerWaveSnapshot) *NetrunnerWaveOperatorSummary {
	if wave.Id <= 0 {
		fetched, err := fetchNetrunnerWaveSnapshot(waveID, authorizedProjectId)
		if err != nil {
			return nil
		}
		wave = fetched
	}
	summary := buildNetrunnerWaveOperatorSummary(wave)
	return &summary
}

func parallelWaveBatchWaitState(output WaitForNetrunnerWaveOutput, summary *NetrunnerWaveOperatorSummary) *NetrunnerWaveBatchWaitState {
	if output.Status == "" {
		return nil
	}
	winningWorkerTerminal := false
	if output.Result.WorkerStatus != "" {
		_, winningWorkerTerminal = parallelWaveWorkerTerminalCondition(output.Result.WorkerStatus)
	}
	allWorkersTerminal := false
	if summary != nil {
		allWorkersTerminal = summary.AllWorkersTerminal
	}
	return &NetrunnerWaveBatchWaitState{
		WaitResultTerminal:    output.Result.Terminal,
		TerminalCondition:     output.Result.TerminalCondition,
		TimedOut:              output.Result.TimedOut,
		WinningSessionId:      output.Result.WinningSessionId,
		WorkerId:              output.Result.WorkerId,
		WinningWorkerStatus:   output.Result.WorkerStatus,
		WinningWorkerTerminal: winningWorkerTerminal,
		AllWorkersTerminal:    allWorkersTerminal,
		FollowUpAllowed:       output.Result.FollowUpAllowed,
		FollowUpBlockedReason: boundedParallelWaveSummaryText(output.Result.FollowUpBlockedReason, 240),
	}
}

func validateParallelWaveBatchIDs(waveIDs []int, projectID int) error {
	if len(waveIDs) < 1 {
		return fmt.Errorf("batch wave tools require at least one wave ID")
	}
	seen := make(map[int]struct{}, len(waveIDs))
	for _, waveID := range waveIDs {
		if waveID <= 0 {
			return fmt.Errorf("wave IDs must be positive")
		}
		if _, exists := seen[waveID]; exists {
			return fmt.Errorf("wave ID %d is duplicated in the batch", waveID)
		}
		seen[waveID] = struct{}{}
		var rowProjectID int
		err := db.QueryRow("SELECT project_id FROM parallel_wave WHERE id = ?", waveID).Scan(&rowProjectID)
		if err == sql.ErrNoRows {
			return fmt.Errorf("wave %d not found", waveID)
		}
		if err != nil {
			return err
		}
		if rowProjectID != projectID {
			return fmt.Errorf("wave %d does not belong to current project", waveID)
		}
	}
	return nil
}

func launchParallelWaveBatchInputs(input LaunchNetrunnerWavesInput) ([]LaunchNetrunnerWaveInput, error) {
	if len(input.WaveIds) > 0 && len(input.Waves) > 0 {
		return nil, fmt.Errorf("wave_ids and waves are mutually exclusive")
	}
	if len(input.Waves) > 0 {
		return append([]LaunchNetrunnerWaveInput(nil), input.Waves...), nil
	}
	waves := make([]LaunchNetrunnerWaveInput, 0, len(input.WaveIds))
	for _, waveID := range input.WaveIds {
		waves = append(waves, LaunchNetrunnerWaveInput{
			WaveId:          waveID,
			Backend:         input.Backend,
			Model:           input.Model,
			Reasoning:       input.Reasoning,
			ReviewPolicy:    input.ReviewPolicy,
			ReviewBackend:   input.ReviewBackend,
			ReviewModel:     input.ReviewModel,
			ReviewReasoning: input.ReviewReasoning,
			FixerSessionId:  input.FixerSessionId,
			TimeoutSeconds:  input.TimeoutSeconds,
		})
	}
	return waves, nil
}

func waitParallelWaveBatchInputs(input WaitForNetrunnerWavesInput) ([]WaitForNetrunnerWaveInput, error) {
	if len(input.WaveIds) > 0 && len(input.Waves) > 0 {
		return nil, fmt.Errorf("wave_ids and waves are mutually exclusive")
	}
	if len(input.Waves) > 0 {
		return append([]WaitForNetrunnerWaveInput(nil), input.Waves...), nil
	}
	waves := make([]WaitForNetrunnerWaveInput, 0, len(input.WaveIds))
	for _, waveID := range input.WaveIds {
		waves = append(waves, WaitForNetrunnerWaveInput{
			WaveId:              waveID,
			TimeoutSeconds:      input.TimeoutSeconds,
			PollIntervalSeconds: input.PollIntervalSeconds,
			ReturnWhen:          input.ReturnWhen,
		})
	}
	return waves, nil
}

func LaunchNetrunnerWaves(ctx context.Context, req *mcp.CallToolRequest, input LaunchNetrunnerWavesInput) (*mcp.CallToolResult, LaunchNetrunnerWavesOutput, error) {
	if authorizedRole != "fixer" || authorizedProjectId <= 0 {
		return &mcp.CallToolResult{IsError: true}, LaunchNetrunnerWavesOutput{}, fmt.Errorf("access denied: requires project-bound fixer role")
	}
	detailLevel, err := parallelWaveBatchDetailLevel(input.DetailLevel)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, LaunchNetrunnerWavesOutput{}, err
	}
	waves, err := launchParallelWaveBatchInputs(input)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, LaunchNetrunnerWavesOutput{}, err
	}
	waveIDs := make([]int, 0, len(waves))
	for _, waveInput := range waves {
		waveIDs = append(waveIDs, waveInput.WaveId)
	}
	if err := validateParallelWaveBatchIDs(waveIDs, authorizedProjectId); err != nil {
		return &mcp.CallToolResult{IsError: true}, LaunchNetrunnerWavesOutput{}, err
	}

	results := make([]LaunchNetrunnerWaveBatchResult, 0, len(waves))
	hadFailure := false
	for _, waveInput := range waves {
		callResult, output, err := LaunchNetrunnerWave(ctx, req, waveInput)
		result := LaunchNetrunnerWaveBatchResult{WaveId: waveInput.WaveId, Status: output.Status}
		if err != nil || (callResult != nil && callResult.IsError) {
			hadFailure = true
			result.Status = "error"
			if err != nil {
				result.Error = err.Error()
			} else {
				result.Error = "launch_netrunner_wave returned an MCP error"
			}
		}
		result.Summary = parallelWaveBatchSummary(waveInput.WaveId, output.Wave)
		if detailLevel == parallelWaveBatchDetailFull {
			outputCopy := output
			result.Output = &outputCopy
		}
		results = append(results, result)
	}
	status := "success"
	if hadFailure {
		status = "partial_failure"
	}
	return nil, LaunchNetrunnerWavesOutput{Status: status, DetailLevel: detailLevel, Results: results}, nil
}

func WaitForNetrunnerWaves(ctx context.Context, req *mcp.CallToolRequest, input WaitForNetrunnerWavesInput) (*mcp.CallToolResult, WaitForNetrunnerWavesOutput, error) {
	if authorizedRole != "fixer" || authorizedProjectId <= 0 {
		return &mcp.CallToolResult{IsError: true}, WaitForNetrunnerWavesOutput{}, fmt.Errorf("access denied: requires project-bound fixer role")
	}
	detailLevel, err := parallelWaveBatchDetailLevel(input.DetailLevel)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, WaitForNetrunnerWavesOutput{}, err
	}
	waves, err := waitParallelWaveBatchInputs(input)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, WaitForNetrunnerWavesOutput{}, err
	}
	waveIDs := make([]int, 0, len(waves))
	for _, waveInput := range waves {
		waveIDs = append(waveIDs, waveInput.WaveId)
	}
	if err := validateParallelWaveBatchIDs(waveIDs, authorizedProjectId); err != nil {
		return &mcp.CallToolResult{IsError: true}, WaitForNetrunnerWavesOutput{}, err
	}

	results := make([]WaitForNetrunnerWaveBatchResult, 0, len(waves))
	hadFailure := false
	for _, waveInput := range waves {
		callResult, output, err := WaitForNetrunnerWave(ctx, req, waveInput)
		result := WaitForNetrunnerWaveBatchResult{WaveId: waveInput.WaveId, Status: output.Status}
		if err != nil || (callResult != nil && callResult.IsError) {
			hadFailure = true
			result.Status = "error"
			if err != nil {
				result.Error = err.Error()
			} else {
				result.Error = "wait_for_netrunner_wave returned an MCP error"
			}
		}
		result.Summary = parallelWaveBatchSummary(waveInput.WaveId, output.Result.Wave)
		result.Wait = parallelWaveBatchWaitState(output, result.Summary)
		if detailLevel == parallelWaveBatchDetailFull {
			outputCopy := output
			result.Output = &outputCopy
		}
		results = append(results, result)
	}
	status := "success"
	if hadFailure {
		status = "partial_failure"
	}
	return nil, WaitForNetrunnerWavesOutput{Status: status, DetailLevel: detailLevel, Results: results}, nil
}
