package main

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Governed cancel/retire for parallel Netrunner waves.
//
// Two shapes are authorized, both explicit and audited — never dummy
// acceptance, never raw SQL, never a manual worker kill:
//
//  1. retire: an initialized, never-launched orphan wave whose sessions are
//     already completed elsewhere. It only holds durable rows hostage.
//  2. cancel: a paused wave (paused_for_architect) or a wave with workers in
//     retry_wait/repair_wait, cancelled only when it is provably safe — no
//     live worker process anywhere.
//
// The outcome is terminal and idempotent: workers become cancelled with the
// audit reason, the wave becomes cancelled (its lifecycle ends exactly like a
// rejected close ends a wave: no attestation, nothing reads as accepted), and
// repeated calls report already-cancelled without rewriting anything.

type CancelNetrunnerWaveInput struct {
	WaveId int    `json:"wave_id" jsonschema:"Parallel wave ID to cancel or retire."`
	Reason string `json:"reason" jsonschema:"Required explicit audit reason (at least 8 characters) recorded with the cancellation."`
}

type CancelNetrunnerWaveWorkerResult struct {
	WorkerId       int    `json:"worker_id"`
	SessionId      int    `json:"session_id"`
	PreviousStatus string `json:"previous_status"`
	NewStatus      string `json:"new_status"`
	SessionStatus  string `json:"session_status"`
}

type CancelNetrunnerWaveOutput struct {
	Status           string                            `json:"status"`
	WaveId           int                               `json:"wave_id"`
	Mode             string                            `json:"mode"`
	AlreadyCancelled bool                              `json:"already_cancelled"`
	AuditReason      string                            `json:"audit_reason"`
	CancelledWorkers int                               `json:"cancelled_workers"`
	Workers          []CancelNetrunnerWaveWorkerResult `json:"workers"`
	Wave             NetrunnerWaveSnapshot             `json:"wave"`
}

const (
	cancelNetrunnerWaveModeRetire           = "retire_orphan"
	cancelNetrunnerWaveModeCancel           = "cancel"
	cancelNetrunnerWaveModeAlreadyCancelled = "already_cancelled"
	minCancelNetrunnerWaveReasonLength      = 8
)

// parallelWaveSessionStatuses maps project-scoped worker session ids to the
// linked session status for the given project.
func parallelWaveSessionStatuses(projectID int, workerSessionIDs []int) (map[int]string, error) {
	statuses := make(map[int]string, len(workerSessionIDs))
	for _, localSessionID := range workerSessionIDs {
		globalSessionID, err := globalSessionIDFromProjectScoped(localSessionID, projectID)
		if err != nil {
			if err == sql.ErrNoRows {
				statuses[localSessionID] = ""
				continue
			}
			return nil, err
		}
		var status string
		if err := db.QueryRow("SELECT status FROM session WHERE id = ? AND project_id = ?", globalSessionID, projectID).Scan(&status); err != nil {
			if err == sql.ErrNoRows {
				statuses[localSessionID] = ""
				continue
			}
			return nil, err
		}
		statuses[localSessionID] = status
	}
	return statuses, nil
}

// classifyParallelWaveCancellation reports the authorized cancellation shape
// for the wave or refuses with the concrete reason.
func classifyParallelWaveCancellation(wave NetrunnerWaveSnapshot, sessionStatuses map[int]string) (string, error) {
	neverLaunched := strings.TrimSpace(wave.LaunchedAt) == "" && wave.Status == parallelWaveStatusCreated
	sessionsCompleted := len(wave.Workers) > 0
	for _, worker := range wave.Workers {
		if sessionStatuses[worker.SessionId] != "completed" {
			sessionsCompleted = false
			break
		}
	}
	if neverLaunched && sessionsCompleted {
		return cancelNetrunnerWaveModeRetire, nil
	}

	quiescent := wave.ControlState == parallelWaveControlPausedForArchitect
	if !quiescent {
		for _, worker := range wave.Workers {
			if worker.Status == parallelWaveWorkerStatusRetryWait || worker.Status == parallelWaveWorkerStatusRepairWait {
				quiescent = true
				break
			}
		}
	}
	if quiescent {
		return cancelNetrunnerWaveModeCancel, nil
	}

	if neverLaunched {
		return "", fmt.Errorf(
			"wave %d is an initialized never-launched wave but its sessions are not all completed (statuses: %s); "+
				"cancel/retire is reserved for orphan waves whose sessions are already completed",
			wave.Id, formatParallelWaveSessionStatuses(sessionStatuses),
		)
	}
	return "", fmt.Errorf(
		"wave %d is not in a cancellable state (status=%q control_state=%q); "+
			"cancel/retire requires an initialized never-launched orphan wave with completed sessions, "+
			"or a paused/retry_wait wave — pause it first with set_netrunner_wave_control_state if needed",
		wave.Id, wave.Status, wave.ControlState,
	)
}

func formatParallelWaveSessionStatuses(sessionStatuses map[int]string) string {
	parts := []string{}
	for sessionID, status := range sessionStatuses {
		parts = append(parts, fmt.Sprintf("%d=%s", sessionID, status))
	}
	return strings.Join(parts, ", ")
}

// validateParallelWaveCancellationSafety refuses cancellation while any
// worker process could still be live. Cancellation never kills processes.
func validateParallelWaveCancellationSafety(wave NetrunnerWaveSnapshot) error {
	for _, worker := range wave.Workers {
		if worker.WorkerProcessId <= 0 {
			if worker.Status == parallelWaveWorkerStatusRunning || worker.Status == parallelWaveWorkerStatusLaunching {
				return fmt.Errorf(
					"worker %d is %s without a recorded worker process; liveness cannot be verified, "+
						"use stop_active_worker_processes / wave reconciliation before cancelling",
					worker.SessionId, worker.Status,
				)
			}
			continue
		}
		processRow, found, err := fetchWorkerProcessByID(worker.WorkerProcessId, worker.ProjectId)
		if err != nil {
			return fmt.Errorf("failed to inspect worker %d process %d: %v", worker.SessionId, worker.WorkerProcessId, err)
		}
		if found && processRow.Status == workerStatusRunning && processRow.Alive {
			return fmt.Errorf(
				"worker %d has a live process (pid %d); cancellation never kills processes — "+
					"stop it first with stop_active_worker_processes",
				worker.SessionId, processRow.PID,
			)
		}
	}
	return nil
}

func CancelNetrunnerWave(ctx context.Context, req *mcp.CallToolRequest, input CancelNetrunnerWaveInput) (*mcp.CallToolResult, CancelNetrunnerWaveOutput, error) {
	if authorizedRole != "fixer" {
		return &mcp.CallToolResult{IsError: true}, CancelNetrunnerWaveOutput{}, fmt.Errorf("access denied: requires fixer role")
	}
	if authorizedProjectId <= 0 {
		return &mcp.CallToolResult{IsError: true}, CancelNetrunnerWaveOutput{}, fmt.Errorf("access denied: fixer role is not bound to a project")
	}

	reason := strings.TrimSpace(input.Reason)
	if len(reason) < minCancelNetrunnerWaveReasonLength {
		return &mcp.CallToolResult{IsError: true}, CancelNetrunnerWaveOutput{}, fmt.Errorf(
			"reason is required: provide an explicit audit reason of at least %d characters",
			minCancelNetrunnerWaveReasonLength,
		)
	}

	wave, err := fetchNetrunnerWaveSnapshot(input.WaveId, authorizedProjectId)
	if err == sql.ErrNoRows {
		return &mcp.CallToolResult{IsError: true}, CancelNetrunnerWaveOutput{}, fmt.Errorf("wave %d not found in current project", input.WaveId)
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CancelNetrunnerWaveOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	projectCWD, err := projectCWDFromID(authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CancelNetrunnerWaveOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	normalizedProjectCWD, err := normalizeProjectCWD(projectCWD)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CancelNetrunnerWaveOutput{}, err
	}
	storedProjectCWD, err := normalizeProjectCWD(wave.ProjectCwd)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CancelNetrunnerWaveOutput{}, fmt.Errorf("stored wave project cwd is invalid: %v", err)
	}
	if storedProjectCWD != normalizedProjectCWD {
		return &mcp.CallToolResult{IsError: true}, CancelNetrunnerWaveOutput{}, fmt.Errorf(
			"wave %d belongs to project cwd %q, current project cwd is %q",
			wave.Id, storedProjectCWD, normalizedProjectCWD,
		)
	}

	// Idempotence: a cancelled wave stays cancelled and is never rewritten.
	if wave.Status == parallelWaveStatusCancelled {
		return nil, CancelNetrunnerWaveOutput{
			Status:           "success",
			WaveId:           wave.Id,
			Mode:             cancelNetrunnerWaveModeAlreadyCancelled,
			AlreadyCancelled: true,
			AuditReason:      reason,
			Workers:          []CancelNetrunnerWaveWorkerResult{},
			Wave:             wave,
		}, nil
	}
	switch wave.Status {
	case parallelWaveStatusCompleted, parallelWaveStatusCleaned, parallelWaveStatusStopped:
		return &mcp.CallToolResult{IsError: true}, CancelNetrunnerWaveOutput{}, fmt.Errorf(
			"wave %d is already terminal (%q); nothing to cancel",
			wave.Id, wave.Status,
		)
	}

	sessionIDs := make([]int, 0, len(wave.Workers))
	for _, worker := range wave.Workers {
		sessionIDs = append(sessionIDs, worker.SessionId)
	}
	sessionStatuses, err := parallelWaveSessionStatuses(authorizedProjectId, sessionIDs)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CancelNetrunnerWaveOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	mode, err := classifyParallelWaveCancellation(wave, sessionStatuses)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CancelNetrunnerWaveOutput{}, err
	}

	if err := validateParallelWaveCancellationSafety(wave); err != nil {
		return &mcp.CallToolResult{IsError: true}, CancelNetrunnerWaveOutput{}, err
	}

	audit := fmt.Sprintf("cancelled: %s", reason)
	tx, err := db.Begin()
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CancelNetrunnerWaveOutput{}, fmt.Errorf("DB transaction error: %v", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	workerResults := make([]CancelNetrunnerWaveWorkerResult, 0, len(wave.Workers))
	cancelledWorkers := 0
	for _, worker := range wave.Workers {
		result := CancelNetrunnerWaveWorkerResult{
			WorkerId:       worker.Id,
			SessionId:      worker.SessionId,
			PreviousStatus: worker.Status,
			NewStatus:      worker.Status,
			SessionStatus:  sessionStatuses[worker.SessionId],
		}
		if _, terminal := parallelWaveWorkerTerminalCondition(worker.Status); !terminal {
			if _, err := tx.Exec(
				`UPDATE parallel_wave_worker
				 SET status = ?,
				     terminal_outcome = ?,
				     failure_reason = ?,
				     retry_next_eligible_at = '',
				     terminal_at = COALESCE(terminal_at, CURRENT_TIMESTAMP),
				     updated_at = CURRENT_TIMESTAMP
				 WHERE id = ? AND project_id = ?`,
				parallelWaveWorkerStatusCancelled,
				audit,
				audit,
				worker.Id,
				authorizedProjectId,
			); err != nil {
				return &mcp.CallToolResult{IsError: true}, CancelNetrunnerWaveOutput{}, fmt.Errorf("DB update error: %v", err)
			}
			result.NewStatus = parallelWaveWorkerStatusCancelled
			cancelledWorkers++
		}
		workerResults = append(workerResults, result)
	}

	// The cancelled wave ends its lifecycle exactly like a rejected close:
	// phase completed + gate closed releases the wave governance over its
	// sessions, while status/reason stay explicitly "cancelled" so nothing is
	// ever read as accepted.
	if _, err := tx.Exec(
		`UPDATE parallel_wave
		 SET status = ?,
		     phase = ?,
		     gate_state = ?,
		     failure_reason = ?,
		     completed_at = COALESCE(completed_at, CURRENT_TIMESTAMP),
		     updated_at = CURRENT_TIMESTAMP
		 WHERE id = ? AND project_id = ?`,
		parallelWaveStatusCancelled,
		parallelWavePhaseCompleted,
		parallelWaveGateClosed,
		audit,
		wave.Id,
		authorizedProjectId,
	); err != nil {
		return &mcp.CallToolResult{IsError: true}, CancelNetrunnerWaveOutput{}, fmt.Errorf("DB update error: %v", err)
	}
	if err := tx.Commit(); err != nil {
		return &mcp.CallToolResult{IsError: true}, CancelNetrunnerWaveOutput{}, fmt.Errorf("DB commit error: %v", err)
	}
	committed = true

	refreshed, err := fetchNetrunnerWaveSnapshot(wave.Id, authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CancelNetrunnerWaveOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	return nil, CancelNetrunnerWaveOutput{
		Status:           "success",
		WaveId:           refreshed.Id,
		Mode:             mode,
		AlreadyCancelled: false,
		AuditReason:      audit,
		CancelledWorkers: cancelledWorkers,
		Workers:          workerResults,
		Wave:             refreshed,
	}, nil
}

func registerCancelNetrunnerWaveTool(server *mcp.Server) {
	addMcpTool(server, "cancel_netrunner_wave", "Governed cancel/retire for parallel Netrunner waves with a required explicit audit reason and idempotent results. Retires initialized never-launched orphan waves whose sessions are already completed, and cancels paused or retry_wait waves when no worker process is live. Never kills processes and never attests anything. Requires fixer role.", CancelNetrunnerWave)
}
