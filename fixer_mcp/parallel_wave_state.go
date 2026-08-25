package main

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	parallelWavePhaseInitialized    = "initialized"
	parallelWavePhaseImplementation = "implementation"
	parallelWavePhaseAcceptance     = "acceptance"
	parallelWavePhaseCompleted      = "completed"

	parallelWaveGateNone                 = "none"
	parallelWaveGateImplementationReview = "implementation_review"
	parallelWaveGateAcceptanceReview     = "acceptance_review"
	parallelWaveGateArchitectReview      = "architect_review"
	parallelWaveGateImplementationRepair = "implementation_repair"
	parallelWaveGateClosed               = "closed"

	parallelWaveControlActive             = "active"
	parallelWaveControlPausedForArchitect = "paused_for_architect"

	parallelWaveFailurePolicyNone             = "none"
	parallelWaveFailurePolicyRepairRequired   = "repair_required"
	parallelWaveFailurePolicyRepairAuthorized = "repair_authorized"
	parallelWaveFailurePolicyRepairInProgress = "repair_in_progress"
	parallelWaveFailurePolicyManualRepair     = "manual_repair_required"
	parallelWaveFailurePolicyPassed           = "passed"
	parallelWaveFailurePolicyPaused           = "paused_for_architect"

	defaultParallelWaveMaxDescendants = 32
	maximumParallelWaveMaxDescendants = 256
	defaultParallelWaveMaxSessions    = 128
	maximumParallelWaveMaxSessions    = 2048
	maximumParallelWaveChildDepth     = 16
)

type parallelWaveLineage struct {
	ParentWaveID            int
	RootWaveID              int
	Depth                   int
	MaxChildWaveDepth       int
	MaxTotalDescendantWaves int
	MaxTotalSessions        int
}

type parallelWaveLineageRow struct {
	ID                      int
	ProjectID               int
	ParentWaveID            int
	RootWaveID              int
	Depth                   int
	MaxChildWaveDepth       int
	MaxTotalDescendantWaves int
	MaxTotalSessions        int
	ControlState            string
	Phase                   string
	HandoffSHA              string
}

func fetchParallelWaveLineageRow(waveID int) (parallelWaveLineageRow, error) {
	var row parallelWaveLineageRow
	err := db.QueryRow(
		`SELECT id,
		        project_id,
		        COALESCE(parent_wave_id, 0),
		        COALESCE(root_wave_id, id),
		        COALESCE(depth, 0),
		        COALESCE(max_child_wave_depth, 0),
		        COALESCE(max_total_descendant_waves, 0),
		        COALESCE(max_total_sessions, 0),
		        COALESCE(control_state, 'active'),
		        COALESCE(phase, 'initialized'),
		        COALESCE(handoff_sha, '')
		 FROM parallel_wave
		 WHERE id = ?`,
		waveID,
	).Scan(
		&row.ID,
		&row.ProjectID,
		&row.ParentWaveID,
		&row.RootWaveID,
		&row.Depth,
		&row.MaxChildWaveDepth,
		&row.MaxTotalDescendantWaves,
		&row.MaxTotalSessions,
		&row.ControlState,
		&row.Phase,
		&row.HandoffSHA,
	)
	return row, err
}

func validateParallelWaveLineageChain(parent parallelWaveLineageRow, projectID int) error {
	visited := map[int]struct{}{}
	current := parent
	for {
		if _, found := visited[current.ID]; found {
			return fmt.Errorf("parallel wave lineage contains a cycle at wave %d", current.ID)
		}
		visited[current.ID] = struct{}{}
		if len(visited) > maximumParallelWaveMaxDescendants+1 {
			return fmt.Errorf("parallel wave lineage exceeds the server safety bound")
		}
		if current.ProjectID != projectID {
			return fmt.Errorf("parent wave %d belongs to another project", current.ID)
		}
		if current.Depth < 0 || current.MaxChildWaveDepth < 0 || current.MaxTotalDescendantWaves < 0 || current.MaxTotalSessions < 0 {
			return fmt.Errorf("parallel wave %d has invalid negative lineage or budget state", current.ID)
		}
		if current.ParentWaveID == 0 {
			if current.Depth != 0 {
				return fmt.Errorf("root wave %d has invalid depth %d", current.ID, current.Depth)
			}
			if current.RootWaveID != current.ID {
				return fmt.Errorf("root wave %d has inconsistent root_wave_id %d", current.ID, current.RootWaveID)
			}
			return nil
		}

		ancestor, err := fetchParallelWaveLineageRow(current.ParentWaveID)
		if err != nil {
			if err == sql.ErrNoRows {
				return fmt.Errorf("parallel wave %d references missing parent wave %d", current.ID, current.ParentWaveID)
			}
			return err
		}
		if ancestor.ProjectID != projectID {
			return fmt.Errorf("parallel wave %d crosses project boundary through parent wave %d", current.ID, ancestor.ID)
		}
		if current.Depth != ancestor.Depth+1 {
			return fmt.Errorf("parallel wave %d depth %d does not follow parent wave %d depth %d", current.ID, current.Depth, ancestor.ID, ancestor.Depth)
		}
		if current.RootWaveID != ancestor.RootWaveID {
			return fmt.Errorf("parallel wave %d escalates or changes root lineage", current.ID)
		}
		if ancestor.MaxChildWaveDepth <= 0 || current.MaxChildWaveDepth > ancestor.MaxChildWaveDepth-1 {
			return fmt.Errorf("parallel wave %d escalates max_child_wave_depth beyond parent wave %d", current.ID, ancestor.ID)
		}
		if current.MaxTotalDescendantWaves > ancestor.MaxTotalDescendantWaves || current.MaxTotalSessions > ancestor.MaxTotalSessions {
			return fmt.Errorf("parallel wave %d escalates root safety budgets", current.ID)
		}
		current = ancestor
	}
}

func normalizeParallelWaveRootBudgets(input CreateNetrunnerWaveInput, sessionCount int) (parallelWaveLineage, error) {
	if input.MaxChildWaveDepth < 0 || input.MaxTotalDescendantWaves < 0 || input.MaxTotalSessions < 0 {
		return parallelWaveLineage{}, fmt.Errorf("wave lineage and budget values must not be negative")
	}
	if input.MaxChildWaveDepth > maximumParallelWaveChildDepth {
		return parallelWaveLineage{}, fmt.Errorf("max_child_wave_depth must be <= %d", maximumParallelWaveChildDepth)
	}
	maxDescendants := input.MaxTotalDescendantWaves
	if input.MaxChildWaveDepth == 0 {
		if maxDescendants != 0 {
			return parallelWaveLineage{}, fmt.Errorf("max_total_descendant_waves requires a positive max_child_wave_depth")
		}
	} else if maxDescendants == 0 {
		maxDescendants = defaultParallelWaveMaxDescendants
	}
	if maxDescendants > maximumParallelWaveMaxDescendants {
		return parallelWaveLineage{}, fmt.Errorf("max_total_descendant_waves must be <= %d", maximumParallelWaveMaxDescendants)
	}
	maxSessions := input.MaxTotalSessions
	if maxSessions == 0 {
		maxSessions = defaultParallelWaveMaxSessions
	}
	if maxSessions > maximumParallelWaveMaxSessions {
		return parallelWaveLineage{}, fmt.Errorf("max_total_sessions must be <= %d", maximumParallelWaveMaxSessions)
	}
	if sessionCount > maxSessions {
		return parallelWaveLineage{}, fmt.Errorf("root wave session count %d exceeds max_total_sessions %d", sessionCount, maxSessions)
	}
	return parallelWaveLineage{
		Depth:                   0,
		MaxChildWaveDepth:       input.MaxChildWaveDepth,
		MaxTotalDescendantWaves: maxDescendants,
		MaxTotalSessions:        maxSessions,
	}, nil
}

func prepareParallelWaveLineage(input CreateNetrunnerWaveInput, projectID int, sessionCount int) (parallelWaveLineage, error) {
	if input.ParentWaveId < 0 || input.MaxChildWaveDepth < 0 || input.MaxTotalDescendantWaves < 0 || input.MaxTotalSessions < 0 {
		return parallelWaveLineage{}, fmt.Errorf("wave lineage and budget values must not be negative")
	}
	if input.ParentWaveId == 0 {
		return normalizeParallelWaveRootBudgets(input, sessionCount)
	}
	if input.MaxChildWaveDepth != 0 || input.MaxTotalDescendantWaves != 0 || input.MaxTotalSessions != 0 {
		return parallelWaveLineage{}, fmt.Errorf("child waves inherit lineage budgets; child inputs must not override them")
	}

	parent, err := fetchParallelWaveLineageRow(input.ParentWaveId)
	if err != nil {
		if err == sql.ErrNoRows {
			return parallelWaveLineage{}, fmt.Errorf("parent wave %d not found", input.ParentWaveId)
		}
		return parallelWaveLineage{}, err
	}
	if parent.ProjectID != projectID {
		return parallelWaveLineage{}, fmt.Errorf("parent wave %d belongs to another project", parent.ID)
	}
	if err := validateParallelWaveLineageChain(parent, projectID); err != nil {
		return parallelWaveLineage{}, err
	}
	if parent.ControlState == parallelWaveControlPausedForArchitect {
		return parallelWaveLineage{}, fmt.Errorf("parent wave %d is paused_for_architect", parent.ID)
	}
	if parent.MaxChildWaveDepth <= 0 {
		return parallelWaveLineage{}, fmt.Errorf("parent wave %d has exhausted max_child_wave_depth", parent.ID)
	}
	if parent.Phase != parallelWavePhaseCompleted {
		return parallelWaveLineage{}, fmt.Errorf("parent wave %d must have accepted/completed disposition before child creation, got phase %q", parent.ID, parent.Phase)
	}
	if strings.TrimSpace(parent.HandoffSHA) == "" {
		return parallelWaveLineage{}, fmt.Errorf("parent wave %d has no immutable committed handoff_sha", parent.ID)
	}
	if parent.MaxTotalDescendantWaves <= 0 || parent.MaxTotalSessions <= 0 {
		return parallelWaveLineage{}, fmt.Errorf("parent wave %d has no recursive safety budget", parent.ID)
	}

	var descendantCount int
	if err := db.QueryRow(
		"SELECT COUNT(*) FROM parallel_wave WHERE project_id = ? AND root_wave_id = ? AND id <> ?",
		projectID,
		parent.RootWaveID,
		parent.RootWaveID,
	).Scan(&descendantCount); err != nil {
		return parallelWaveLineage{}, err
	}
	if descendantCount+1 > parent.MaxTotalDescendantWaves {
		return parallelWaveLineage{}, fmt.Errorf("root wave %d exhausted max_total_descendant_waves %d", parent.RootWaveID, parent.MaxTotalDescendantWaves)
	}

	var existingSessions int
	if err := db.QueryRow(
		`SELECT COUNT(*)
		 FROM parallel_wave_worker AS worker
		 JOIN parallel_wave AS wave ON wave.id = worker.wave_id
		 WHERE wave.project_id = ? AND wave.root_wave_id = ?`,
		projectID,
		parent.RootWaveID,
	).Scan(&existingSessions); err != nil {
		return parallelWaveLineage{}, err
	}
	if existingSessions+sessionCount > parent.MaxTotalSessions {
		return parallelWaveLineage{}, fmt.Errorf("root wave %d session budget %d would be exceeded by %d total sessions", parent.RootWaveID, parent.MaxTotalSessions, existingSessions+sessionCount)
	}

	return parallelWaveLineage{
		ParentWaveID:            parent.ID,
		RootWaveID:              parent.RootWaveID,
		Depth:                   parent.Depth + 1,
		MaxChildWaveDepth:       parent.MaxChildWaveDepth - 1,
		MaxTotalDescendantWaves: parent.MaxTotalDescendantWaves,
		MaxTotalSessions:        parent.MaxTotalSessions,
	}, nil
}

func validateParallelWaveLineageBudgetTx(tx *sql.Tx, lineage parallelWaveLineage, projectID int, sessionCount int) error {
	if lineage.ParentWaveID == 0 {
		if sessionCount > lineage.MaxTotalSessions {
			return fmt.Errorf("root wave session count %d exceeds max_total_sessions %d", sessionCount, lineage.MaxTotalSessions)
		}
		return nil
	}
	var parentProjectID, parentRootID, parentDepth, parentRemainingDepth, parentMaxDescendants, parentMaxSessions int
	var parentControlState, parentPhase, parentHandoffSHA string
	if err := tx.QueryRow(
		`SELECT project_id, COALESCE(root_wave_id, id), COALESCE(depth, 0),
		        COALESCE(max_child_wave_depth, 0), COALESCE(max_total_descendant_waves, 0),
		        COALESCE(max_total_sessions, 0), COALESCE(control_state, 'active'),
		        COALESCE(phase, 'initialized'), COALESCE(handoff_sha, '')
		 FROM parallel_wave WHERE id = ?`,
		lineage.ParentWaveID,
	).Scan(
		&parentProjectID,
		&parentRootID,
		&parentDepth,
		&parentRemainingDepth,
		&parentMaxDescendants,
		&parentMaxSessions,
		&parentControlState,
		&parentPhase,
		&parentHandoffSHA,
	); err != nil {
		return err
	}
	if parentProjectID != projectID || parentRootID != lineage.RootWaveID || lineage.Depth != parentDepth+1 {
		return fmt.Errorf("parent wave lineage changed before child insertion")
	}
	if parentControlState == parallelWaveControlPausedForArchitect {
		return fmt.Errorf("parent wave %d is paused_for_architect", lineage.ParentWaveID)
	}
	if parentPhase != parallelWavePhaseCompleted || strings.TrimSpace(parentHandoffSHA) == "" {
		return fmt.Errorf("parent wave %d no longer has an accepted committed handoff", lineage.ParentWaveID)
	}
	if parentRemainingDepth <= 0 || lineage.MaxChildWaveDepth != parentRemainingDepth-1 {
		return fmt.Errorf("parent wave %d max_child_wave_depth was exhausted or changed", lineage.ParentWaveID)
	}
	if lineage.MaxTotalDescendantWaves != parentMaxDescendants || lineage.MaxTotalSessions != parentMaxSessions {
		return fmt.Errorf("parent wave %d safety budgets changed before child insertion", lineage.ParentWaveID)
	}

	var descendantCount int
	if err := tx.QueryRow(
		"SELECT COUNT(*) FROM parallel_wave WHERE project_id = ? AND root_wave_id = ? AND id <> ?",
		projectID,
		lineage.RootWaveID,
		lineage.RootWaveID,
	).Scan(&descendantCount); err != nil {
		return err
	}
	if descendantCount+1 > lineage.MaxTotalDescendantWaves {
		return fmt.Errorf("root wave %d exhausted max_total_descendant_waves %d", lineage.RootWaveID, lineage.MaxTotalDescendantWaves)
	}
	var existingSessions int
	if err := tx.QueryRow(
		`SELECT COUNT(*)
		 FROM parallel_wave_worker AS worker
		 JOIN parallel_wave AS wave ON wave.id = worker.wave_id
		 WHERE wave.project_id = ? AND wave.root_wave_id = ?`,
		projectID,
		lineage.RootWaveID,
	).Scan(&existingSessions); err != nil {
		return err
	}
	if existingSessions+sessionCount > lineage.MaxTotalSessions {
		return fmt.Errorf("root wave %d exhausted max_total_sessions %d", lineage.RootWaveID, lineage.MaxTotalSessions)
	}
	return nil
}

func parallelWaveFailureLike(status string) bool {
	switch status {
	case parallelWaveWorkerStatusFailed,
		parallelWaveWorkerStatusStopped,
		parallelWaveWorkerStatusStaleEpoch,
		parallelWaveWorkerStatusBlocked:
		return true
	default:
		return false
	}
}

func parallelWaveScheduledCohort(wave NetrunnerWaveSnapshot) []NetrunnerWaveWorkerSnapshot {
	cohort := []NetrunnerWaveWorkerSnapshot{}
	for _, worker := range wave.Workers {
		// A dependency-gated worker joins the failure denominator only after it
		// has actually been scheduled. Keeping untouched descendants out avoids
		// diluting an initial cohort, while still governing deferred failures.
		if worker.Status == parallelWaveWorkerStatusCreated {
			continue
		}
		cohort = append(cohort, worker)
	}
	return cohort
}

func parallelWaveFailedLikeCohortCount(wave NetrunnerWaveSnapshot) int {
	count := 0
	for _, worker := range parallelWaveScheduledCohort(wave) {
		if parallelWaveFailureLike(worker.Status) {
			count++
		}
	}
	return count
}

const parallelWaveArchitectApprovedReasonPrefix = "architect_approved:ack_failed="

func formatArchitectApprovedControlReason(acknowledgedFailedCount int, customReason string) string {
	reason := fmt.Sprintf("%s%d", parallelWaveArchitectApprovedReasonPrefix, acknowledgedFailedCount)
	if strings.TrimSpace(customReason) != "" {
		reason += ":" + strings.TrimSpace(customReason)
	}
	return reason
}

// parseArchitectApprovedAcknowledgedFailedCount extracts the failed-worker
// count the Architect acknowledged at resume time. ok is false when reason
// does not carry the marker, e.g. legacy rows written before this format.
func parseArchitectApprovedAcknowledgedFailedCount(controlReason string) (int, bool) {
	if !strings.HasPrefix(controlReason, parallelWaveArchitectApprovedReasonPrefix) {
		return 0, false
	}
	rest := controlReason[len(parallelWaveArchitectApprovedReasonPrefix):]
	if idx := strings.IndexByte(rest, ':'); idx >= 0 {
		rest = rest[:idx]
	}
	count, err := strconv.Atoi(rest)
	if err != nil || count < 0 {
		return 0, false
	}
	return count, true
}

type parallelWaveFailureDecision struct {
	State    string
	Reason   string
	WorkerID int
}

func decideParallelWaveFailurePolicy(wave NetrunnerWaveSnapshot) parallelWaveFailureDecision {
	cohort := parallelWaveScheduledCohort(wave)
	workerCount := len(cohort)
	if workerCount == 0 {
		return parallelWaveFailureDecision{State: parallelWaveFailurePolicyNone}
	}
	failed := []NetrunnerWaveWorkerSnapshot{}
	allTerminal := true
	for _, worker := range cohort {
		if parallelWaveFailureLike(worker.Status) {
			failed = append(failed, worker)
		}
		if _, terminal := parallelWaveWorkerTerminalCondition(worker.Status); !terminal {
			allTerminal = false
		}
	}
	failedCount := len(failed)
	if failedCount == 0 {
		if allTerminal {
			return parallelWaveFailureDecision{State: parallelWaveFailurePolicyPassed}
		}
		return parallelWaveFailureDecision{State: parallelWaveFailurePolicyNone}
	}

	if wave.RepairAttemptCount > 0 {
		switch wave.FailurePolicyState {
		case parallelWaveFailurePolicyRepairAuthorized:
			return parallelWaveFailureDecision{State: wave.FailurePolicyState, WorkerID: wave.RepairWorkerId}
		case parallelWaveFailurePolicyRepairInProgress:
			for _, worker := range wave.Workers {
				if worker.Id != wave.RepairWorkerId {
					continue
				}
				if _, terminal := parallelWaveWorkerTerminalCondition(worker.Status); !terminal {
					return parallelWaveFailureDecision{State: wave.FailurePolicyState, WorkerID: wave.RepairWorkerId}
				}
				break
			}
		case parallelWaveFailurePolicyPassed:
			// A later scheduled descendant failed after the one governed repair
			// was consumed. It cannot receive another autonomous repair.
		}
		if failedCount*2 <= workerCount {
			return parallelWaveFailureDecision{
				State:    parallelWaveFailurePolicyManualRepair,
				Reason:   fmt.Sprintf("manual_repair_required:%d/%d", failedCount, workerCount),
				WorkerID: failed[0].Id,
			}
		}
		return parallelWaveFailureDecision{
			State:  parallelWaveFailurePolicyPaused,
			Reason: fmt.Sprintf("failed_worker_majority:%d/%d", failedCount, workerCount),
		}
	}
	if workerCount == 1 && failedCount == 1 {
		return parallelWaveFailureDecision{
			State:    parallelWaveFailurePolicyRepairRequired,
			Reason:   "governed_repair_required:1/1",
			WorkerID: failed[0].Id,
		}
	}

	if failedCount*2 > workerCount {
		return parallelWaveFailureDecision{
			State:  parallelWaveFailurePolicyPaused,
			Reason: fmt.Sprintf("failed_worker_majority:%d/%d", failedCount, workerCount),
		}
	}

	launchStopped := false
	if wave.Status == parallelWaveStatusFailed || wave.Status == parallelWaveStatusPartiallyFailed {
		for _, worker := range cohort {
			if worker.Status == parallelWaveWorkerStatusCreated || worker.Status == parallelWaveWorkerStatusWorktreeReady {
				launchStopped = true
				break
			}
		}
	}
	if allTerminal || launchStopped {
		return parallelWaveFailureDecision{
			State:    parallelWaveFailurePolicyRepairRequired,
			Reason:   fmt.Sprintf("governed_repair_required:%d/%d", failedCount, workerCount),
			WorkerID: failed[0].Id,
		}
	}
	return parallelWaveFailureDecision{State: parallelWaveFailurePolicyNone}
}

type AuthorizeNetrunnerWaveRepairInput struct {
	WaveId          int `json:"wave_id" jsonschema:"Parallel wave awaiting its one governed implementation repair."`
	WorkerSessionId int `json:"worker_session_id" jsonschema:"Failed project-scoped worker session authorized for repair."`
}

type AuthorizeNetrunnerWaveRepairOutput struct {
	Status string                `json:"status"`
	Wave   NetrunnerWaveSnapshot `json:"wave"`
}

func AuthorizeNetrunnerWaveRepair(ctx context.Context, req *mcp.CallToolRequest, input AuthorizeNetrunnerWaveRepairInput) (*mcp.CallToolResult, AuthorizeNetrunnerWaveRepairOutput, error) {
	if authorizedRole != "fixer" || authorizedProjectId <= 0 {
		return &mcp.CallToolResult{IsError: true}, AuthorizeNetrunnerWaveRepairOutput{}, fmt.Errorf("access denied: requires project-bound fixer role")
	}
	if err := ensureMCPBinaryRestartNotRequired(authorizedProjectId); err != nil {
		return &mcp.CallToolResult{IsError: true}, AuthorizeNetrunnerWaveRepairOutput{}, err
	}
	wave, err := fetchNetrunnerWaveSnapshot(input.WaveId, authorizedProjectId)
	if err == sql.ErrNoRows {
		return &mcp.CallToolResult{IsError: true}, AuthorizeNetrunnerWaveRepairOutput{}, fmt.Errorf("wave %d not found in current project", input.WaveId)
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, AuthorizeNetrunnerWaveRepairOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	if wave.FailurePolicyState != parallelWaveFailurePolicyRepairRequired || wave.RepairAttemptCount != 0 {
		return &mcp.CallToolResult{IsError: true}, AuthorizeNetrunnerWaveRepairOutput{}, fmt.Errorf("wave %d does not have an unused governed repair authorization", wave.Id)
	}
	var repairWorker NetrunnerWaveWorkerSnapshot
	for _, worker := range wave.Workers {
		if worker.SessionId == input.WorkerSessionId {
			repairWorker = worker
			break
		}
	}
	if repairWorker.Id == 0 || repairWorker.Id != wave.RepairWorkerId || !parallelWaveFailureLike(repairWorker.Status) {
		return &mcp.CallToolResult{IsError: true}, AuthorizeNetrunnerWaveRepairOutput{}, fmt.Errorf("worker session %d is not the failed worker selected by wave failure policy", input.WorkerSessionId)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, AuthorizeNetrunnerWaveRepairOutput{}, fmt.Errorf("DB transaction error: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(
		ctx,
		`UPDATE parallel_wave
		 SET failure_policy_state = ?, repair_attempt_count = 1, gate_state = ?, control_reason = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE id = ? AND project_id = ? AND failure_policy_state = ? AND repair_attempt_count = 0`,
		parallelWaveFailurePolicyRepairAuthorized,
		parallelWaveGateImplementationRepair,
		fmt.Sprintf("governed_repair_authorized:session-%d", input.WorkerSessionId),
		wave.Id,
		authorizedProjectId,
		parallelWaveFailurePolicyRepairRequired,
	)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, AuthorizeNetrunnerWaveRepairOutput{}, fmt.Errorf("DB update error: %v", err)
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return &mcp.CallToolResult{IsError: true}, AuthorizeNetrunnerWaveRepairOutput{}, fmt.Errorf("wave %d governed repair authorization was already consumed", wave.Id)
	}
	if _, err := tx.ExecContext(
		ctx,
		`UPDATE parallel_wave_worker SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND project_id = ?`,
		parallelWaveWorkerStatusRepairWait,
		repairWorker.Id,
		authorizedProjectId,
	); err != nil {
		return &mcp.CallToolResult{IsError: true}, AuthorizeNetrunnerWaveRepairOutput{}, fmt.Errorf("DB update error: %v", err)
	}
	if err := tx.Commit(); err != nil {
		return &mcp.CallToolResult{IsError: true}, AuthorizeNetrunnerWaveRepairOutput{}, fmt.Errorf("DB commit error: %v", err)
	}
	wave, err = fetchNetrunnerWaveSnapshot(wave.Id, authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, AuthorizeNetrunnerWaveRepairOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	return nil, AuthorizeNetrunnerWaveRepairOutput{Status: "success", Wave: wave}, nil
}

func parallelWaveFailurePauseReason(wave NetrunnerWaveSnapshot) string {
	decision := decideParallelWaveFailurePolicy(wave)
	if decision.State == parallelWaveFailurePolicyPaused {
		return decision.Reason
	}
	return ""
}

func pauseParallelWaveForArchitect(waveID int, projectID int, reason string) error {
	_, err := db.Exec(
		`UPDATE parallel_wave
		 SET control_state = ?,
		     control_reason = ?,
		     gate_state = ?,
		     failure_policy_state = ?,
		     updated_at = CURRENT_TIMESTAMP
		 WHERE id = ? AND project_id = ?`,
		parallelWaveControlPausedForArchitect,
		reason,
		parallelWaveGateArchitectReview,
		parallelWaveFailurePolicyPaused,
		waveID,
		projectID,
	)
	return err
}

func reconcileParallelWaveFailureControl(wave NetrunnerWaveSnapshot) (NetrunnerWaveSnapshot, error) {
	if wave.ControlState == parallelWaveControlPausedForArchitect {
		return wave, nil
	}
	if wave.ControlState == parallelWaveControlActive {
		if acknowledgedFailedCount, ok := parseArchitectApprovedAcknowledgedFailedCount(wave.ControlReason); ok {
			// The Architect's resume only acknowledges the failures that existed at
			// that moment. A worker that fails afterwards (e.g. a dependency-gated
			// worker released post-repair) must still reach canonical recovery
			// instead of being silently absorbed by the stale approval forever.
			if parallelWaveFailedLikeCohortCount(wave) <= acknowledgedFailedCount {
				return wave, nil
			}
		}
	}
	decision := decideParallelWaveFailurePolicy(wave)
	switch decision.State {
	case parallelWaveFailurePolicyNone:
		return wave, nil
	case parallelWaveFailurePolicyPaused:
		if err := pauseParallelWaveForArchitect(wave.Id, wave.ProjectId, decision.Reason); err != nil {
			return NetrunnerWaveSnapshot{}, err
		}
	case parallelWaveFailurePolicyRepairRequired:
		_, err := db.Exec(
			`UPDATE parallel_wave
			 SET failure_policy_state = ?, repair_worker_id = ?, gate_state = ?, control_reason = ?, updated_at = CURRENT_TIMESTAMP
			 WHERE id = ? AND project_id = ? AND repair_attempt_count = 0`,
			parallelWaveFailurePolicyRepairRequired,
			decision.WorkerID,
			parallelWaveGateImplementationRepair,
			decision.Reason,
			wave.Id,
			wave.ProjectId,
		)
		if err != nil {
			return NetrunnerWaveSnapshot{}, err
		}
	case parallelWaveFailurePolicyManualRepair:
		_, err := db.Exec(
			`UPDATE parallel_wave
			 SET failure_policy_state = ?, repair_worker_id = ?, gate_state = ?, control_reason = ?, updated_at = CURRENT_TIMESTAMP
			 WHERE id = ? AND project_id = ? AND control_state = ?`,
			parallelWaveFailurePolicyManualRepair,
			decision.WorkerID,
			parallelWaveGateImplementationRepair,
			decision.Reason,
			wave.Id,
			wave.ProjectId,
			parallelWaveControlActive,
		)
		if err != nil {
			return NetrunnerWaveSnapshot{}, err
		}
	case parallelWaveFailurePolicyPassed:
		_, err := db.Exec(
			`UPDATE parallel_wave
			 SET failure_policy_state = ?, repair_worker_id = NULL, control_reason = '', updated_at = CURRENT_TIMESTAMP
			 WHERE id = ? AND project_id = ? AND control_state = ?`,
			parallelWaveFailurePolicyPassed,
			wave.Id,
			wave.ProjectId,
			parallelWaveControlActive,
		)
		if err != nil {
			return NetrunnerWaveSnapshot{}, err
		}
	case parallelWaveFailurePolicyRepairAuthorized, parallelWaveFailurePolicyRepairInProgress:
		return wave, nil
	}
	return fetchNetrunnerWaveSnapshot(wave.Id, wave.ProjectId)
}

type SetNetrunnerWaveControlStateInput struct {
	WaveId            int    `json:"wave_id" jsonschema:"Parallel wave ID to control."`
	ControlState      string `json:"control_state" jsonschema:"Target control state: active or paused_for_architect."`
	ArchitectApproved bool   `json:"architect_approved,omitempty" jsonschema:"Must be true when resuming a wave that was paused_for_architect."`
	Reason            string `json:"reason,omitempty" jsonschema:"Optional control-state reason."`
}

type SetNetrunnerWaveControlStateOutput struct {
	Status string                `json:"status"`
	Wave   NetrunnerWaveSnapshot `json:"wave"`
}

func SetNetrunnerWaveControlState(ctx context.Context, req *mcp.CallToolRequest, input SetNetrunnerWaveControlStateInput) (*mcp.CallToolResult, SetNetrunnerWaveControlStateOutput, error) {
	if authorizedRole != "fixer" || authorizedProjectId <= 0 {
		return &mcp.CallToolResult{IsError: true}, SetNetrunnerWaveControlStateOutput{}, fmt.Errorf("access denied: requires project-bound fixer role")
	}
	wave, err := fetchNetrunnerWaveSnapshot(input.WaveId, authorizedProjectId)
	if err == sql.ErrNoRows {
		return &mcp.CallToolResult{IsError: true}, SetNetrunnerWaveControlStateOutput{}, fmt.Errorf("wave %d not found in current project", input.WaveId)
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetNetrunnerWaveControlStateOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	target := strings.ToLower(strings.TrimSpace(input.ControlState))
	gateState := wave.GateState
	failurePolicyState := wave.FailurePolicyState
	controlReason := strings.TrimSpace(input.Reason)
	switch target {
	case parallelWaveControlPausedForArchitect:
		gateState = parallelWaveGateArchitectReview
		failurePolicyState = parallelWaveFailurePolicyPaused
		if controlReason == "" {
			controlReason = "paused by Fixer for Architect review"
		}
	case parallelWaveControlActive:
		if wave.ControlState == parallelWaveControlPausedForArchitect {
			if !input.ArchitectApproved {
				return &mcp.CallToolResult{IsError: true}, SetNetrunnerWaveControlStateOutput{}, fmt.Errorf("architect_approved must be true to resume a paused_for_architect wave")
			}
			controlReason = formatArchitectApprovedControlReason(parallelWaveFailedLikeCohortCount(wave), input.Reason)
			failurePolicyState = parallelWaveFailurePolicyPassed
		} else {
			controlReason = wave.ControlReason
		}
		switch wave.Phase {
		case parallelWavePhaseImplementation:
			if parallelWaveAllWorkersTerminal(wave) {
				gateState = parallelWaveGateImplementationReview
			} else {
				gateState = parallelWaveGateNone
			}
		case parallelWavePhaseAcceptance:
			gateState = parallelWaveGateAcceptanceReview
		case parallelWavePhaseCompleted:
			gateState = parallelWaveGateClosed
		default:
			gateState = parallelWaveGateNone
		}
	default:
		return &mcp.CallToolResult{IsError: true}, SetNetrunnerWaveControlStateOutput{}, fmt.Errorf("unsupported control_state %q; supported values are %q and %q", input.ControlState, parallelWaveControlActive, parallelWaveControlPausedForArchitect)
	}
	if _, err := db.Exec(
		`UPDATE parallel_wave
		 SET control_state = ?, control_reason = ?, gate_state = ?, failure_policy_state = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE id = ? AND project_id = ?`,
		target,
		controlReason,
		gateState,
		failurePolicyState,
		wave.Id,
		authorizedProjectId,
	); err != nil {
		return &mcp.CallToolResult{IsError: true}, SetNetrunnerWaveControlStateOutput{}, fmt.Errorf("DB update error: %v", err)
	}
	wave, err = fetchNetrunnerWaveSnapshot(wave.Id, authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetNetrunnerWaveControlStateOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	return nil, SetNetrunnerWaveControlStateOutput{Status: "success", Wave: wave}, nil
}

func markParallelWaveImplementationStarted(waveID int, projectID int) error {
	_, err := db.Exec(
		`UPDATE parallel_wave
		 SET phase = ?, gate_state = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE id = ? AND project_id = ? AND phase = ?`,
		parallelWavePhaseImplementation,
		parallelWaveGateNone,
		waveID,
		projectID,
		parallelWavePhaseInitialized,
	)
	return err
}

func markParallelWaveImplementationReviewGate(waveID int, projectID int) error {
	_, err := db.Exec(
		`UPDATE parallel_wave
		 SET gate_state = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE id = ? AND project_id = ? AND phase = ? AND control_state = ?`,
		parallelWaveGateImplementationReview,
		waveID,
		projectID,
		parallelWavePhaseImplementation,
		parallelWaveControlActive,
	)
	return err
}

func parallelWaveAllWorkersTerminal(wave NetrunnerWaveSnapshot) bool {
	if len(wave.Workers) == 0 {
		return false
	}
	for _, worker := range wave.Workers {
		if _, terminal := parallelWaveWorkerTerminalCondition(worker.Status); !terminal {
			return false
		}
	}
	return true
}

// parallelWaveScopeEntryContainsPath reports whether a single declared
// write-scope entry contains a project-relative changed path. A trailing
// "/**" glob denotes "this directory and everything inside it", so a file
// written directly inside the directory matches the same way a deeper file
// does.
func parallelWaveScopeEntryContainsPath(scopeEntry string, path string) bool {
	normalizedPath := filepath.ToSlash(filepath.Clean(path))
	normalizedScopeEntry := filepath.ToSlash(filepath.Clean(scopeEntry))
	if normalizedScopeEntry == defaultWriteScopePath {
		return true
	}
	if strings.HasSuffix(normalizedScopeEntry, "/**") {
		directory := strings.TrimSuffix(normalizedScopeEntry, "/**")
		return normalizedPath == directory || strings.HasPrefix(normalizedPath, directory+"/")
	}
	return normalizedPath == normalizedScopeEntry ||
		strings.HasPrefix(normalizedPath, normalizedScopeEntry+"/")
}

// parallelWaveDeclaredWriteScopeContainsPath is the parallel-wave completion
// variant of declaredWriteScopeContainsPath. It additionally honors "/**"
// glob entries, which the legacy overlap-only matcher silently rejects.
func parallelWaveDeclaredWriteScopeContainsPath(scope []string, path string) bool {
	for _, scopeEntry := range scope {
		if parallelWaveScopeEntryContainsPath(scopeEntry, path) {
			return true
		}
	}
	return false
}

// releaseParallelWaveWorkerScopeLeases releases the scope-lease rows owned by
// a terminal worker without waiting for whole-wave cleanup. A scope path is
// deliberately kept while another non-terminal worker in the same wave still
// declares an overlapping scope (for example a dependency-gated child that
// inherits its parent's scope), so the release never opens a fence that a live
// worker still relies on.
func releaseParallelWaveWorkerScopeLeases(wave NetrunnerWaveSnapshot, worker NetrunnerWaveWorkerSnapshot) error {
	if len(worker.DeclaredWriteScope) == 0 {
		return nil
	}
	blocked := make(map[string]struct{})
	for _, other := range wave.Workers {
		if other.Id == worker.Id {
			continue
		}
		if _, terminal := parallelWaveWorkerTerminalCondition(other.Status); terminal {
			continue
		}
		for _, otherPath := range other.DeclaredWriteScope {
			for _, workerPath := range worker.DeclaredWriteScope {
				if writeScopePathsOverlap(otherPath, workerPath) {
					blocked[workerPath] = struct{}{}
				}
			}
		}
	}
	for _, scopePath := range worker.DeclaredWriteScope {
		if _, keep := blocked[scopePath]; keep {
			continue
		}
		if _, err := db.Exec(
			`UPDATE parallel_wave_scope_lease
			 SET active = 0,
			     released_at = COALESCE(released_at, CURRENT_TIMESTAMP)
			 WHERE wave_id = ? AND project_id = ? AND scope_path = ? AND active = 1`,
			wave.Id,
			wave.ProjectId,
			scopePath,
		); err != nil {
			return err
		}
	}
	return nil
}

// reconcileStaleParallelWaveWorkers deterministically re-inspects every
// non-terminal, scheduled worker. A worker whose session already completed or
// whose recorded process is gone is finalized instead of remaining "running"
// forever, so phase transitions and cleanup are not stranded by metadata that
// the wait loop never observed again.
func reconcileStaleParallelWaveWorkers(projectCWD string, wave NetrunnerWaveSnapshot) (NetrunnerWaveSnapshot, error) {
	normalizedProjectCWD, err := normalizeProjectCWD(projectCWD)
	if err != nil {
		return NetrunnerWaveSnapshot{}, err
	}
	for _, worker := range wave.Workers {
		if _, terminal := parallelWaveWorkerTerminalCondition(worker.Status); terminal {
			continue
		}
		if worker.Status == parallelWaveWorkerStatusCreated {
			continue
		}
		if _, _, err := inspectParallelWaveWorkerForWait(normalizedProjectCWD, wave, worker); err != nil {
			return NetrunnerWaveSnapshot{}, err
		}
	}
	return fetchNetrunnerWaveSnapshot(wave.Id, wave.ProjectId)
}

type TransitionNetrunnerWavePhaseInput struct {
	WaveId              int    `json:"wave_id" jsonschema:"Parallel wave ID to transition."`
	TargetPhase         string `json:"target_phase" jsonschema:"Target phase. Supported reviewed transitions: acceptance, completed."`
	AcceptanceSessionId int    `json:"acceptance_session_id,omitempty" jsonschema:"Project-scoped pending acceptance session required when entering acceptance."`
	HandoffSha          string `json:"handoff_sha,omitempty" jsonschema:"Immutable committed Git handoff required when completing a wave that can create children."`
	ReviewApproved      bool   `json:"review_approved" jsonschema:"Must be true to attest that the preceding phase review passed."`
}

type TransitionNetrunnerWavePhaseOutput struct {
	Status string                `json:"status"`
	Wave   NetrunnerWaveSnapshot `json:"wave"`
}

func acceptanceSessionSnapshot(globalSessionID int, projectID int) (string, string, error) {
	var status, report string
	err := db.QueryRow(
		"SELECT status, COALESCE(report, '') FROM session WHERE id = ? AND project_id = ?",
		globalSessionID,
		projectID,
	).Scan(&status, &report)
	return status, report, err
}

func TransitionNetrunnerWavePhase(ctx context.Context, req *mcp.CallToolRequest, input TransitionNetrunnerWavePhaseInput) (*mcp.CallToolResult, TransitionNetrunnerWavePhaseOutput, error) {
	if authorizedRole != "fixer" {
		return &mcp.CallToolResult{IsError: true}, TransitionNetrunnerWavePhaseOutput{}, fmt.Errorf("access denied: requires fixer role")
	}
	if authorizedProjectId <= 0 {
		return &mcp.CallToolResult{IsError: true}, TransitionNetrunnerWavePhaseOutput{}, fmt.Errorf("access denied: fixer role is not bound to a project")
	}
	if !input.ReviewApproved {
		return &mcp.CallToolResult{IsError: true}, TransitionNetrunnerWavePhaseOutput{}, fmt.Errorf("review_approved must be true for a phase transition")
	}
	if err := ensureMCPBinaryRestartNotRequired(authorizedProjectId); err != nil {
		return &mcp.CallToolResult{IsError: true}, TransitionNetrunnerWavePhaseOutput{}, err
	}
	wave, err := fetchNetrunnerWaveSnapshot(input.WaveId, authorizedProjectId)
	if err == sql.ErrNoRows {
		return &mcp.CallToolResult{IsError: true}, TransitionNetrunnerWavePhaseOutput{}, fmt.Errorf("wave %d not found in current project", input.WaveId)
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, TransitionNetrunnerWavePhaseOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	if wave.ControlState == parallelWaveControlPausedForArchitect {
		return &mcp.CallToolResult{IsError: true}, TransitionNetrunnerWavePhaseOutput{}, fmt.Errorf("wave %d is paused_for_architect: %s", wave.Id, wave.ControlReason)
	}

	target := strings.ToLower(strings.TrimSpace(input.TargetPhase))
	switch target {
	case parallelWavePhaseAcceptance:
		if wave.Phase != parallelWavePhaseImplementation {
			return &mcp.CallToolResult{IsError: true}, TransitionNetrunnerWavePhaseOutput{}, fmt.Errorf("wave %d must be in implementation phase before acceptance, got %q", wave.Id, wave.Phase)
		}
		projectCWD, cwdErr := projectCWDFromID(authorizedProjectId)
		if cwdErr != nil {
			return &mcp.CallToolResult{IsError: true}, TransitionNetrunnerWavePhaseOutput{}, fmt.Errorf("DB query error: %v", cwdErr)
		}
		normalizedProjectCWD, normErr := normalizeProjectCWD(projectCWD)
		if normErr != nil {
			return &mcp.CallToolResult{IsError: true}, TransitionNetrunnerWavePhaseOutput{}, normErr
		}
		wave, err = reconcileStaleParallelWaveWorkers(normalizedProjectCWD, wave)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, TransitionNetrunnerWavePhaseOutput{}, fmt.Errorf("failed to reconcile stale implementation workers: %v", err)
		}
		if !parallelWaveAllWorkersTerminal(wave) {
			return &mcp.CallToolResult{IsError: true}, TransitionNetrunnerWavePhaseOutput{}, fmt.Errorf("wave %d implementation workers are not all terminal", wave.Id)
		}
		wave, err = reconcileParallelWaveFailureControl(wave)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, TransitionNetrunnerWavePhaseOutput{}, fmt.Errorf("DB update error: %v", err)
		}
		if wave.FailurePolicyState != parallelWaveFailurePolicyPassed {
			return &mcp.CallToolResult{IsError: true}, TransitionNetrunnerWavePhaseOutput{}, fmt.Errorf("wave %d failure policy must pass before acceptance, got %q", wave.Id, wave.FailurePolicyState)
		}
		if wave.ReviewSessionId <= 0 || wave.ReviewSessionStatus != "completed" {
			return &mcp.CallToolResult{IsError: true}, TransitionNetrunnerWavePhaseOutput{}, fmt.Errorf("wave %d implementation reviewer must be completed before acceptance", wave.Id)
		}
		if input.AcceptanceSessionId <= 0 {
			return &mcp.CallToolResult{IsError: true}, TransitionNetrunnerWavePhaseOutput{}, fmt.Errorf("acceptance_session_id is required when entering acceptance")
		}
		globalAcceptanceID, err := globalSessionIDFromProjectScoped(input.AcceptanceSessionId, authorizedProjectId)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, TransitionNetrunnerWavePhaseOutput{}, fmt.Errorf("acceptance session %d not found in current project", input.AcceptanceSessionId)
		}
		status, _, err := acceptanceSessionSnapshot(globalAcceptanceID, authorizedProjectId)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, TransitionNetrunnerWavePhaseOutput{}, fmt.Errorf("DB query error: %v", err)
		}
		if status != "pending" {
			return &mcp.CallToolResult{IsError: true}, TransitionNetrunnerWavePhaseOutput{}, fmt.Errorf("acceptance session %d must be pending, got %q", input.AcceptanceSessionId, status)
		}
		var conflicting int
		if err := db.QueryRow(
			"SELECT COUNT(*) FROM parallel_wave_worker WHERE project_id = ? AND session_id = ?",
			authorizedProjectId,
			globalAcceptanceID,
		).Scan(&conflicting); err != nil {
			return &mcp.CallToolResult{IsError: true}, TransitionNetrunnerWavePhaseOutput{}, fmt.Errorf("DB query error: %v", err)
		}
		if conflicting > 0 || input.AcceptanceSessionId == wave.ReviewSessionId {
			return &mcp.CallToolResult{IsError: true}, TransitionNetrunnerWavePhaseOutput{}, fmt.Errorf("acceptance session %d must be distinct from implementation workers and reviewer", input.AcceptanceSessionId)
		}
		_, err = db.Exec(
			`UPDATE parallel_wave
			 SET phase = ?, gate_state = ?, acceptance_session_id = ?, updated_at = CURRENT_TIMESTAMP
			 WHERE id = ? AND project_id = ?`,
			parallelWavePhaseAcceptance,
			parallelWaveGateAcceptanceReview,
			globalAcceptanceID,
			wave.Id,
			authorizedProjectId,
		)
	case parallelWavePhaseCompleted:
		if wave.Phase != parallelWavePhaseAcceptance {
			return &mcp.CallToolResult{IsError: true}, TransitionNetrunnerWavePhaseOutput{}, fmt.Errorf("wave %d must be in acceptance phase before completion, got %q", wave.Id, wave.Phase)
		}
		if wave.AcceptanceSessionId <= 0 {
			return &mcp.CallToolResult{IsError: true}, TransitionNetrunnerWavePhaseOutput{}, fmt.Errorf("wave %d has no acceptance session contract", wave.Id)
		}
		globalAcceptanceID, err := globalSessionIDFromProjectScoped(wave.AcceptanceSessionId, authorizedProjectId)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, TransitionNetrunnerWavePhaseOutput{}, fmt.Errorf("acceptance session %d not found in current project", wave.AcceptanceSessionId)
		}
		status, _, err := acceptanceSessionSnapshot(globalAcceptanceID, authorizedProjectId)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, TransitionNetrunnerWavePhaseOutput{}, fmt.Errorf("DB query error: %v", err)
		}
		if status != "completed" {
			return &mcp.CallToolResult{IsError: true}, TransitionNetrunnerWavePhaseOutput{}, fmt.Errorf("acceptance session %d must be completed, got %q", wave.AcceptanceSessionId, status)
		}
		handoffSHA := strings.TrimSpace(input.HandoffSha)
		if wave.MaxChildWaveDepth > 0 && handoffSHA == "" {
			return &mcp.CallToolResult{IsError: true}, TransitionNetrunnerWavePhaseOutput{}, fmt.Errorf("handoff_sha is required before completing recursive wave %d", wave.Id)
		}
		if handoffSHA != "" {
			projectCWD, cwdErr := projectCWDFromID(authorizedProjectId)
			if cwdErr != nil {
				return &mcp.CallToolResult{IsError: true}, TransitionNetrunnerWavePhaseOutput{}, fmt.Errorf("DB query error: %v", cwdErr)
			}
			spec, specErr := gitBaseSHACommand(projectCWD, handoffSHA)
			if specErr != nil {
				return &mcp.CallToolResult{IsError: true}, TransitionNetrunnerWavePhaseOutput{}, fmt.Errorf("failed to validate handoff_sha %q: %v", handoffSHA, specErr)
			}
			resolved, resolveErr := runGitCommandSpec(spec)
			if resolveErr != nil || strings.TrimSpace(resolved) != handoffSHA {
				return &mcp.CallToolResult{IsError: true}, TransitionNetrunnerWavePhaseOutput{}, fmt.Errorf("handoff_sha %q is not an exact committed Git object: %v", handoffSHA, resolveErr)
			}
		}
		_, err = db.Exec(
			`UPDATE parallel_wave
			 SET phase = ?, gate_state = ?, handoff_sha = ?, completed_at = COALESCE(completed_at, CURRENT_TIMESTAMP), updated_at = CURRENT_TIMESTAMP
			 WHERE id = ? AND project_id = ?`,
			parallelWavePhaseCompleted,
			parallelWaveGateClosed,
			handoffSHA,
			wave.Id,
			authorizedProjectId,
		)
		if err == nil {
			err = releaseParallelWaveScopeLeases(wave.Id, authorizedProjectId)
		}
	default:
		return &mcp.CallToolResult{IsError: true}, TransitionNetrunnerWavePhaseOutput{}, fmt.Errorf("unsupported target_phase %q; supported values are %q and %q", input.TargetPhase, parallelWavePhaseAcceptance, parallelWavePhaseCompleted)
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, TransitionNetrunnerWavePhaseOutput{}, fmt.Errorf("DB update error: %v", err)
	}
	wave, err = fetchNetrunnerWaveSnapshot(wave.Id, authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, TransitionNetrunnerWavePhaseOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	return nil, TransitionNetrunnerWavePhaseOutput{Status: "success", Wave: wave}, nil
}

type MCPBinaryRestartState struct {
	ProjectId                 int    `json:"project_id"`
	RunningBuildEpoch         int    `json:"running_build_epoch"`
	RequiredBuildEpoch        int    `json:"required_build_epoch"`
	RestartRequired           bool   `json:"restart_required"`
	RunningBuildId            string `json:"running_build_id"`
	RequiredBuildId           string `json:"required_build_id"`
	RunningProcessIdentity    string `json:"running_process_identity"`
	RequiredByProcessIdentity string `json:"required_by_process_identity"`
	ConfirmedAt               string `json:"confirmed_at,omitempty"`
	Reason                    string `json:"reason"`
	UpdatedAt                 string `json:"updated_at"`
}

type GetMCPBinaryRestartStateInput struct{}

type GetMCPBinaryRestartStateOutput struct {
	Status string                `json:"status"`
	State  MCPBinaryRestartState `json:"state"`
}

type SetMCPBinaryRestartStateInput struct {
	Action     string `json:"action" jsonschema:"State transition: mark_required or confirm_restarted."`
	BuildEpoch int    `json:"build_epoch" jsonschema:"Positive build epoch being required or confirmed."`
	BuildId    string `json:"build_id,omitempty" jsonschema:"Exact immutable build identity required for mark_required. Confirmation uses the running process identity and ignores caller claims."`
	Reason     string `json:"reason,omitempty" jsonschema:"Optional audit reason for this binary state transition."`
}

type SetMCPBinaryRestartStateOutput struct {
	Status string                `json:"status"`
	State  MCPBinaryRestartState `json:"state"`
}

type mcpBinaryRestartStateQuerier interface {
	QueryRow(query string, args ...any) *sql.Row
}

func fetchMCPBinaryRestartStateWithQuerier(queryer mcpBinaryRestartStateQuerier, projectID int) (MCPBinaryRestartState, error) {
	state := MCPBinaryRestartState{ProjectId: projectID}
	var restartRequired int
	err := queryer.QueryRow(
		`SELECT running_build_epoch, required_build_epoch, restart_required,
		        COALESCE(running_build_id, ''), COALESCE(required_build_id, ''),
		        COALESCE(running_process_identity, ''), COALESCE(required_by_process_identity, ''),
		        COALESCE(confirmed_at, ''), reason, updated_at
		 FROM mcp_binary_state
		 WHERE project_id = ?`,
		projectID,
	).Scan(
		&state.RunningBuildEpoch,
		&state.RequiredBuildEpoch,
		&restartRequired,
		&state.RunningBuildId,
		&state.RequiredBuildId,
		&state.RunningProcessIdentity,
		&state.RequiredByProcessIdentity,
		&state.ConfirmedAt,
		&state.Reason,
		&state.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return state, nil
	}
	if err != nil {
		return MCPBinaryRestartState{}, err
	}
	state.RestartRequired = restartRequired != 0
	return state, nil
}

func fetchMCPBinaryRestartState(projectID int) (MCPBinaryRestartState, error) {
	return fetchMCPBinaryRestartStateWithQuerier(db, projectID)
}

func ensureMCPBinaryRestartNotRequired(projectID int) error {
	state, err := fetchMCPBinaryRestartState(projectID)
	if err != nil {
		return err
	}
	if state.RestartRequired {
		return fmt.Errorf("mcp_binary_restart_required: build epoch %d must be observed before creating or launching more waves", state.RequiredBuildEpoch)
	}
	return nil
}

func GetMCPBinaryRestartState(ctx context.Context, req *mcp.CallToolRequest, input GetMCPBinaryRestartStateInput) (*mcp.CallToolResult, GetMCPBinaryRestartStateOutput, error) {
	if authorizedRole != "fixer" || authorizedProjectId <= 0 {
		return &mcp.CallToolResult{IsError: true}, GetMCPBinaryRestartStateOutput{}, fmt.Errorf("access denied: requires project-bound fixer role")
	}
	state, err := fetchMCPBinaryRestartState(authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetMCPBinaryRestartStateOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	return nil, GetMCPBinaryRestartStateOutput{Status: "success", State: state}, nil
}

func SetMCPBinaryRestartState(ctx context.Context, req *mcp.CallToolRequest, input SetMCPBinaryRestartStateInput) (*mcp.CallToolResult, SetMCPBinaryRestartStateOutput, error) {
	if authorizedRole != "fixer" || authorizedProjectId <= 0 {
		return &mcp.CallToolResult{IsError: true}, SetMCPBinaryRestartStateOutput{}, fmt.Errorf("access denied: requires project-bound fixer role")
	}
	if input.BuildEpoch <= 0 {
		return &mcp.CallToolResult{IsError: true}, SetMCPBinaryRestartStateOutput{}, fmt.Errorf("build_epoch must be positive")
	}
	action := strings.ToLower(strings.TrimSpace(input.Action))
	if action != "mark_required" && action != "confirm_restarted" {
		return &mcp.CallToolResult{IsError: true}, SetMCPBinaryRestartStateOutput{}, fmt.Errorf("unsupported action %q; supported values are mark_required and confirm_restarted", input.Action)
	}
	requiredBuildID := strings.TrimSpace(input.BuildId)
	if action == "mark_required" {
		if requiredBuildID == "" {
			return &mcp.CallToolResult{IsError: true}, SetMCPBinaryRestartStateOutput{}, fmt.Errorf("build_id is required to prove the exact replacement binary")
		}
		if requiredBuildID == mcpRunningBuildID {
			return &mcp.CallToolResult{IsError: true}, SetMCPBinaryRestartStateOutput{}, fmt.Errorf("required build identity must differ from the currently running build %q", mcpRunningBuildID)
		}
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetMCPBinaryRestartStateOutput{}, fmt.Errorf("DB transaction error: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	switch action {
	case "mark_required":
		result, updateErr := tx.ExecContext(
			ctx,
			`INSERT INTO mcp_binary_state (
				project_id, running_build_epoch, required_build_epoch, restart_required,
				running_build_id, required_build_id, running_process_identity, required_by_process_identity,
				reason, updated_at
			 ) VALUES (?, 0, ?, 1, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
			 ON CONFLICT(project_id) DO UPDATE SET
				required_build_epoch = excluded.required_build_epoch,
				restart_required = 1,
				running_build_id = CASE
					WHEN excluded.required_build_epoch > mcp_binary_state.required_build_epoch THEN excluded.running_build_id
					ELSE mcp_binary_state.running_build_id
				END,
				required_build_id = excluded.required_build_id,
				running_process_identity = CASE
					WHEN excluded.required_build_epoch > mcp_binary_state.required_build_epoch THEN excluded.running_process_identity
					ELSE mcp_binary_state.running_process_identity
				END,
				required_by_process_identity = CASE
					WHEN excluded.required_build_epoch > mcp_binary_state.required_build_epoch THEN excluded.required_by_process_identity
					ELSE mcp_binary_state.required_by_process_identity
				END,
				confirmed_at = NULL,
				reason = CASE
					WHEN excluded.required_build_epoch > mcp_binary_state.required_build_epoch THEN excluded.reason
					ELSE mcp_binary_state.reason
				END,
				updated_at = CURRENT_TIMESTAMP
			 WHERE excluded.required_build_epoch > mcp_binary_state.running_build_epoch
			   AND (
				excluded.required_build_epoch > mcp_binary_state.required_build_epoch
				OR (
					excluded.required_build_epoch = mcp_binary_state.required_build_epoch
					AND excluded.required_build_id = mcp_binary_state.required_build_id
					AND mcp_binary_state.restart_required = 1
				)
			   )`,
			authorizedProjectId,
			input.BuildEpoch,
			mcpRunningBuildID,
			requiredBuildID,
			mcpProcessIdentity,
			mcpProcessIdentity,
			strings.TrimSpace(input.Reason),
		)
		if updateErr != nil {
			return &mcp.CallToolResult{IsError: true}, SetMCPBinaryRestartStateOutput{}, fmt.Errorf("DB update error: %v", updateErr)
		}
		rowsAffected, rowsErr := result.RowsAffected()
		if rowsErr != nil {
			return &mcp.CallToolResult{IsError: true}, SetMCPBinaryRestartStateOutput{}, fmt.Errorf("DB update result error: %v", rowsErr)
		}
		if rowsAffected != 1 {
			current, fetchErr := fetchMCPBinaryRestartStateWithQuerier(tx, authorizedProjectId)
			if fetchErr != nil {
				return &mcp.CallToolResult{IsError: true}, SetMCPBinaryRestartStateOutput{}, fmt.Errorf("DB query error after restart-state conflict: %v", fetchErr)
			}
			if input.BuildEpoch <= current.RunningBuildEpoch || input.BuildEpoch < current.RequiredBuildEpoch {
				return &mcp.CallToolResult{IsError: true}, SetMCPBinaryRestartStateOutput{}, fmt.Errorf("build_epoch %d must be newer than running epoch %d and not lower than required epoch %d", input.BuildEpoch, current.RunningBuildEpoch, current.RequiredBuildEpoch)
			}
			if input.BuildEpoch == current.RequiredBuildEpoch && requiredBuildID != current.RequiredBuildId {
				return &mcp.CallToolResult{IsError: true}, SetMCPBinaryRestartStateOutput{}, fmt.Errorf("build_epoch %d already requires build identity %q; cannot replace it with %q", input.BuildEpoch, current.RequiredBuildId, requiredBuildID)
			}
			return &mcp.CallToolResult{IsError: true}, SetMCPBinaryRestartStateOutput{}, fmt.Errorf("restart-state compare-and-swap conflict while requiring build epoch %d", input.BuildEpoch)
		}
		_, err = tx.ExecContext(
			ctx,
			`INSERT INTO autonomous_run_status (
					project_id, state, summary, orchestration_frozen,
					notifications_enabled_for_active_run, updated_at
				 ) VALUES (?, 'blocked', ?, 1, 1, CURRENT_TIMESTAMP)
				 ON CONFLICT(project_id) DO UPDATE SET
					orchestration_frozen = 1,
					notifications_enabled_for_active_run = 1,
					updated_at = CURRENT_TIMESTAMP`,
			authorizedProjectId,
			fmt.Sprintf("MCP binary restart required for build epoch %d", input.BuildEpoch),
		)
	case "confirm_restarted":
		result, updateErr := tx.ExecContext(
			ctx,
			`UPDATE mcp_binary_state
			 SET running_build_epoch = ?, restart_required = 0,
			     running_build_id = ?, running_process_identity = ?, confirmed_at = CURRENT_TIMESTAMP,
			     reason = ?, updated_at = CURRENT_TIMESTAMP
			 WHERE project_id = ?
			   AND restart_required = 1
			   AND required_build_epoch = ?
			   AND required_build_id = ?
			   AND TRIM(required_by_process_identity) != ''
			   AND required_by_process_identity != ?`,
			input.BuildEpoch,
			mcpRunningBuildID,
			mcpProcessIdentity,
			strings.TrimSpace(input.Reason),
			authorizedProjectId,
			input.BuildEpoch,
			mcpRunningBuildID,
			mcpProcessIdentity,
		)
		if updateErr != nil {
			return &mcp.CallToolResult{IsError: true}, SetMCPBinaryRestartStateOutput{}, fmt.Errorf("DB update error: %v", updateErr)
		}
		rowsAffected, rowsErr := result.RowsAffected()
		if rowsErr != nil {
			return &mcp.CallToolResult{IsError: true}, SetMCPBinaryRestartStateOutput{}, fmt.Errorf("DB update result error: %v", rowsErr)
		}
		if rowsAffected != 1 {
			current, fetchErr := fetchMCPBinaryRestartStateWithQuerier(tx, authorizedProjectId)
			if fetchErr != nil {
				return &mcp.CallToolResult{IsError: true}, SetMCPBinaryRestartStateOutput{}, fmt.Errorf("DB query error after restart-state conflict: %v", fetchErr)
			}
			if !current.RestartRequired {
				return &mcp.CallToolResult{IsError: true}, SetMCPBinaryRestartStateOutput{}, fmt.Errorf("no MCP binary restart is currently required")
			}
			if input.BuildEpoch != current.RequiredBuildEpoch {
				return &mcp.CallToolResult{IsError: true}, SetMCPBinaryRestartStateOutput{}, fmt.Errorf("build_epoch %d does not match required epoch %d", input.BuildEpoch, current.RequiredBuildEpoch)
			}
			if strings.TrimSpace(current.RequiredBuildId) == "" || current.RequiredBuildId != mcpRunningBuildID {
				return &mcp.CallToolResult{IsError: true}, SetMCPBinaryRestartStateOutput{}, fmt.Errorf("running build identity %q does not match required build identity %q", mcpRunningBuildID, current.RequiredBuildId)
			}
			if strings.TrimSpace(current.RequiredByProcessIdentity) == "" || current.RequiredByProcessIdentity == mcpProcessIdentity {
				return &mcp.CallToolResult{IsError: true}, SetMCPBinaryRestartStateOutput{}, fmt.Errorf("restart confirmation requires a fresh MCP process identity")
			}
			return &mcp.CallToolResult{IsError: true}, SetMCPBinaryRestartStateOutput{}, fmt.Errorf("restart-state compare-and-swap conflict while confirming build epoch %d", input.BuildEpoch)
		}
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetMCPBinaryRestartStateOutput{}, fmt.Errorf("DB update error: %v", err)
	}
	state, err := fetchMCPBinaryRestartStateWithQuerier(tx, authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetMCPBinaryRestartStateOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	if err := tx.Commit(); err != nil {
		return &mcp.CallToolResult{IsError: true}, SetMCPBinaryRestartStateOutput{}, fmt.Errorf("DB commit error: %v", err)
	}
	return nil, SetMCPBinaryRestartStateOutput{Status: "success", State: state}, nil
}
