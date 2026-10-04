package dashboardapi

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	missionControlFreshnessThreshold  = 5 * time.Minute
	missionControlMutationUnavailable = "Dashboard mutations are disabled because this API cannot safely delegate to the governed Wave Engine; use Fixer MCP."
)

const (
	missionControlPhaseInitialized = "initialized"
	missionControlPhaseAcceptance  = "acceptance"
	missionControlPhaseCompleted   = "completed"

	missionControlGateImplementationReview = "implementation_review"
	missionControlGateImplementationRepair = "implementation_repair"
	missionControlGateClosed               = "closed"

	missionControlControlActive             = "active"
	missionControlControlPausedForArchitect = "paused_for_architect"

	missionControlFailureRepairRequired   = "repair_required"
	missionControlFailureRepairAuthorized = "repair_authorized"
	missionControlFailureRepairInProgress = "repair_in_progress"
	missionControlFailurePassed           = "passed"
	missionControlFailurePaused           = "paused_for_architect"
)

type missionControlFreshnessTracker struct {
	latest      time.Time
	latestRaw   string
	hasValue    bool
	unparseable bool
}

func (r *Repository) ProjectMissionControlWaves(ctx context.Context, projectID int) (ProjectMissionControlWavesResponse, error) {
	now := time.Now
	if r.now != nil {
		now = r.now
	}
	return r.projectMissionControlWavesAt(ctx, projectID, now().UTC())
}

func (r *Repository) projectMissionControlWavesAt(ctx context.Context, projectID int, generatedAt time.Time) (ProjectMissionControlWavesResponse, error) {
	if _, err := r.requireProject(ctx, projectID); err != nil {
		return ProjectMissionControlWavesResponse{}, err
	}
	generatedAt = generatedAt.UTC()
	response := ProjectMissionControlWavesResponse{
		ProjectID:    projectID,
		GeneratedAt:  generatedAt.Format(time.RFC3339Nano),
		PlannedWaves: []MissionControlPlannedWave{},
		Waves:        []MissionControlWave{},
	}
	plannedWaves, err := r.loadMissionControlPlannedWaves(ctx, projectID)
	if err != nil {
		return ProjectMissionControlWavesResponse{}, err
	}
	response.PlannedWaves = plannedWaves
	if !r.tableExists(ctx, "parallel_wave") {
		response.Freshness = missionControlFreshness(generatedAt, missionControlFreshnessTracker{}, false)
		return response, nil
	}

	localSessionIDs, err := r.loadMissionControlLocalSessionIDs(ctx, projectID)
	if err != nil {
		return ProjectMissionControlWavesResponse{}, err
	}

	waves, tracker, err := r.loadMissionControlWaves(ctx, projectID, localSessionIDs)
	if err != nil {
		return ProjectMissionControlWavesResponse{}, err
	}
	response.Waves = waves
	if err := r.loadMissionControlWaveWorkers(ctx, projectID, localSessionIDs, response.Waves, &tracker); err != nil {
		return ProjectMissionControlWavesResponse{}, err
	}
	if err := r.loadMissionControlWaveReviews(ctx, projectID, localSessionIDs, response.Waves); err != nil {
		return ProjectMissionControlWavesResponse{}, err
	}

	response.Freshness = missionControlFreshness(generatedAt, tracker, len(response.Waves) > 0)
	for index := range response.Waves {
		projectMissionControlWave(&response.Waves[index])
		response.Waves[index].ActionCapabilities = missionControlWaveActionCapabilities(response.Waves[index], response.Freshness)
	}
	return response, nil
}

func (r *Repository) loadMissionControlPlannedWaves(ctx context.Context, projectID int) ([]MissionControlPlannedWave, error) {
	if !r.tableExists(ctx, "planned_wave") {
		return []MissionControlPlannedWave{}, nil
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, title, status, reason, base_ref, worktree_root,
		       COALESCE(initialized_wave_id, 0), failure_reason,
		       created_at, updated_at, COALESCE(initialized_at, '')
		FROM planned_wave
		WHERE project_id = ?
		ORDER BY id DESC`, projectID)
	if err != nil {
		return nil, err
	}
	plans := []MissionControlPlannedWave{}
	for rows.Next() {
		var plan MissionControlPlannedWave
		if err := rows.Scan(
			&plan.PlanID,
			&plan.Title,
			&plan.Status,
			&plan.Reason,
			&plan.BaseRef,
			&plan.WorktreeRoot,
			&plan.InitializedWaveID,
			&plan.FailureReason,
			&plan.CreatedAt,
			&plan.UpdatedAt,
			&plan.InitializedAt,
		); err != nil {
			_ = rows.Close()
			return nil, err
		}
		plan.Reason = boundedMissionControlText(plan.Reason, 240)
		plan.FailureReason = boundedMissionControlText(plan.FailureReason, 240)
		plan.CreatedAt = normalizeMissionControlTimestamp(plan.CreatedAt)
		plan.UpdatedAt = normalizeMissionControlTimestamp(plan.UpdatedAt)
		plan.InitializedAt = normalizeMissionControlTimestamp(plan.InitializedAt)
		plan.Tasks = []MissionControlPlannedWaveTask{}
		plan.MCPServers = []string{}
		plan.ValidationErrors = []string{}
		plans = append(plans, plan)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if len(plans) == 0 || !r.tableExists(ctx, "planned_wave_task") {
		return plans, nil
	}

	planIndexes := make(map[int]int, len(plans))
	for index := range plans {
		planIndexes[plans[index].PlanID] = index
	}
	taskRows, err := r.db.QueryContext(ctx, `
		SELECT task.id, task.planned_wave_id, task.task_key, task.position,
		       task.task_description, task.dependencies,
		       COALESCE(NULLIF(TRIM(task.cli_backend), ''), 'codex'),
		       COALESCE(task.cli_model, ''), COALESCE(task.cli_reasoning, ''),
		       COALESCE(task.mcp_server_names, '[]'),
		       COALESCE(task.materialized_session_id, 0),
		       COALESCE((
		           SELECT COUNT(*)
		           FROM session ranked
		           WHERE ranked.project_id = task.project_id
		             AND ranked.id <= task.materialized_session_id
		       ), 0)
		FROM planned_wave_task task
		JOIN planned_wave plan
		  ON plan.id = task.planned_wave_id AND plan.project_id = task.project_id
		WHERE task.project_id = ? AND plan.project_id = ?
		ORDER BY task.planned_wave_id DESC, task.position, task.id`, projectID, projectID)
	if err != nil {
		return nil, err
	}
	defer taskRows.Close()
	for taskRows.Next() {
		var task MissionControlPlannedWaveTask
		var planID int
		var dependenciesPayload string
		var mcpServersPayload string
		if err := taskRows.Scan(
			&task.TaskID,
			&planID,
			&task.Key,
			&task.Position,
			&task.TaskDescription,
			&dependenciesPayload,
			&task.Backend,
			&task.Model,
			&task.Reasoning,
			&mcpServersPayload,
			&task.MaterializedSessionID,
			&task.LocalSessionID,
		); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(dependenciesPayload), &task.DependsOn); err != nil {
			return nil, fmt.Errorf("invalid planned task %d dependencies: %w", task.TaskID, err)
		}
		if err := json.Unmarshal([]byte(mcpServersPayload), &task.MCPServers); err != nil {
			return nil, fmt.Errorf("invalid planned task %d mcp_server_names: %w", task.TaskID, err)
		}
		index, found := planIndexes[planID]
		if !found {
			continue
		}
		plans[index].Tasks = append(plans[index].Tasks, task)
	}
	if err := taskRows.Err(); err != nil {
		return nil, err
	}
	project, err := r.requireProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	routeAvailable := r.plannedWaveInitializeRouteAvailable(project)
	for index := range plans {
		if err := r.validateMissionControlPlannedWaveAssignments(ctx, projectID, &plans[index]); err != nil {
			return nil, err
		}
		projectMissionControlPlannedWave(&plans[index], routeAvailable)
	}
	return plans, nil
}

func projectMissionControlPlannedWave(plan *MissionControlPlannedWave, routeAvailable bool) {
	if plan == nil {
		return
	}
	plan.TaskCounts.Total = len(plan.Tasks)
	for _, task := range plan.Tasks {
		if task.MaterializedSessionID > 0 {
			plan.TaskCounts.Materialized++
		} else {
			plan.TaskCounts.Planned++
		}
	}
	projectMissionControlPlannedWaveAssignments(plan)
	plan.OperatorState, plan.Label, plan.NextAction = "planned", "Planned", "initialize"
	switch strings.TrimSpace(plan.Status) {
	case "initializing":
		plan.OperatorState, plan.Label, plan.NextAction = "initializing", "Initializing", "wait"
	case "initialized":
		plan.OperatorState, plan.Label, plan.NextAction = "initialized", "Initialized", "open_wave"
	case "failed":
		plan.OperatorState, plan.Label, plan.NextAction = "initialization_failed", "Initialization failed", "retry_initialize"
	}
	initialize := MissionControlActionCapability{
		Enabled: (strings.TrimSpace(plan.Status) == "planned" || strings.TrimSpace(plan.Status) == "failed") &&
			len(plan.ValidationErrors) == 0 && routeAvailable,
	}
	if !initialize.Enabled {
		if len(plan.ValidationErrors) > 0 {
			initialize.DisabledReason = "Resolve planned-task validation errors before Initialize."
		} else if !routeAvailable && (plan.Status == "planned" || plan.Status == "failed") {
			initialize.DisabledReason = "Governed Initialize is unavailable because the Fixer MCP bridge cannot be started."
		} else if plan.Status == "initialized" {
			initialize.DisabledReason = fmt.Sprintf("This plan is already initialized as wave %d.", plan.InitializedWaveID)
		} else {
			initialize.DisabledReason = "Initialization is already in progress."
		}
	}
	launchReason := "Initialize this planned definition before launch."
	if plan.Status == "initialized" {
		launchReason = missionControlMutationUnavailable
	}
	plan.ActionCapabilities = MissionControlPlannedWaveActionCapabilities{
		Initialize: initialize,
		Launch: MissionControlActionCapability{
			Enabled:        false,
			DisabledReason: launchReason,
		},
	}
}

func projectMissionControlPlannedWaveAssignments(plan *MissionControlPlannedWave) {
	if plan == nil || len(plan.Tasks) == 0 {
		return
	}
	plan.Backend = plan.Tasks[0].Backend
	plan.Model = plan.Tasks[0].Model
	plan.Reasoning = plan.Tasks[0].Reasoning
	mcpNames := map[string]struct{}{}
	for _, task := range plan.Tasks {
		if task.Backend != plan.Backend {
			plan.Backend = "mixed"
		}
		if task.Model != plan.Model {
			plan.Model = "mixed"
		}
		if task.Reasoning != plan.Reasoning {
			plan.Reasoning = "mixed"
		}
		for _, name := range task.MCPServers {
			mcpNames[name] = struct{}{}
		}
	}
	plan.MCPServers = make([]string, 0, len(mcpNames))
	for name := range mcpNames {
		plan.MCPServers = append(plan.MCPServers, name)
	}
	sort.Strings(plan.MCPServers)
}

func (r *Repository) validateMissionControlPlannedWaveAssignments(ctx context.Context, projectID int, plan *MissionControlPlannedWave) error {
	if plan == nil || len(plan.Tasks) == 0 {
		return nil
	}
	if !r.tableExists(ctx, "mcp_server") || !r.tableExists(ctx, "project_mcp_server") {
		for _, task := range plan.Tasks {
			if len(task.MCPServers) > 0 {
				plan.ValidationErrors = append(plan.ValidationErrors, fmt.Sprintf("Task %s MCP registry is unavailable.", task.Key))
			}
		}
		return nil
	}
	rows, err := r.db.QueryContext(ctx, "SELECT name, COALESCE(archived, 0) FROM mcp_server")
	if err != nil {
		return err
	}
	registry := map[string]bool{}
	for rows.Next() {
		var name string
		var archived int
		if err := rows.Scan(&name, &archived); err != nil {
			_ = rows.Close()
			return err
		}
		registry[name] = archived == 0
	}
	if err := rows.Close(); err != nil {
		return err
	}
	boundRows, err := r.db.QueryContext(ctx, `
		SELECT server.name
		FROM project_mcp_server binding
		JOIN mcp_server server ON server.id = binding.mcp_server_id
		WHERE binding.project_id = ?`, projectID)
	if err != nil {
		return err
	}
	allowed := map[string]bool{}
	for boundRows.Next() {
		var name string
		if err := boundRows.Scan(&name); err != nil {
			_ = boundRows.Close()
			return err
		}
		allowed[name] = registry[name]
	}
	if err := boundRows.Close(); err != nil {
		return err
	}
	if active, found := registry["fixer_mcp"]; found {
		allowed["fixer_mcp"] = active
	}
	for _, task := range plan.Tasks {
		for _, name := range task.MCPServers {
			active, found := allowed[name]
			switch {
			case !found:
				plan.ValidationErrors = append(plan.ValidationErrors, fmt.Sprintf("Task %s MCP server %s is not allowed for this project.", task.Key, name))
			case !active:
				plan.ValidationErrors = append(plan.ValidationErrors, fmt.Sprintf("Task %s MCP server %s is archived.", task.Key, name))
			}
		}
	}
	return nil
}

func (r *Repository) loadMissionControlLocalSessionIDs(ctx context.Context, projectID int) (map[int]int, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, ROW_NUMBER() OVER (ORDER BY id)
		FROM session
		WHERE project_id = ?
		ORDER BY id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	localSessionIDs := map[int]int{}
	for rows.Next() {
		var sessionID int
		var localSessionID int
		if err := rows.Scan(&sessionID, &localSessionID); err != nil {
			return nil, err
		}
		localSessionIDs[sessionID] = localSessionID
	}
	return localSessionIDs, rows.Err()
}

func (r *Repository) loadMissionControlWaves(ctx context.Context, projectID int, localSessionIDs map[int]int) ([]MissionControlWave, missionControlFreshnessTracker, error) {
	columns, err := r.missionControlTableColumns(ctx, "parallel_wave")
	if err != nil {
		return nil, missionControlFreshnessTracker{}, err
	}
	column := func(name string, fallback string) string {
		if columns[name] {
			return fmt.Sprintf("COALESCE(w.%s, %s)", name, fallback)
		}
		return fallback
	}

	acceptanceJoin := ""
	acceptanceID := "0"
	acceptanceStatus := "''"
	if columns["acceptance_session_id"] {
		acceptanceJoin = "LEFT JOIN session acceptance ON acceptance.id = w.acceptance_session_id AND acceptance.project_id = w.project_id"
		acceptanceID = "COALESCE(acceptance.id, 0)"
		acceptanceStatus = "COALESCE(acceptance.status, '')"
	}

	query := fmt.Sprintf(`
		SELECT w.id,
		       COALESCE(w.status, 'created'),
		       %s,
		       %s,
		       %s,
		       %s,
		       %s,
		       %s,
		       %s,
		       %s,
		       %s,
		       %s,
		       %s,
		       %s,
		       %s,
		       %s
		FROM parallel_wave w
		%s
		WHERE w.project_id = ?
		ORDER BY w.id DESC`,
		column("phase", "'initialized'"),
		column("gate_state", "'none'"),
		column("control_state", "'active'"),
		column("control_reason", "''"),
		column("failure_policy_state", "'none'"),
		column("failure_reason", "''"),
		column("repair_worker_id", "0"),
		column("repair_attempt_count", "0"),
		acceptanceID,
		acceptanceStatus,
		column("created_at", "''"),
		column("updated_at", "''"),
		column("launched_at", "''"),
		column("completed_at", "''"),
		acceptanceJoin,
	)
	rows, err := r.db.QueryContext(ctx, query, projectID)
	if err != nil {
		return nil, missionControlFreshnessTracker{}, err
	}
	defer rows.Close()

	waves := []MissionControlWave{}
	tracker := missionControlFreshnessTracker{}
	for rows.Next() {
		var wave MissionControlWave
		var acceptanceID int
		var acceptanceStatus string
		if err := rows.Scan(
			&wave.WaveID,
			&wave.LegacyStatus,
			&wave.Phase,
			&wave.GateState,
			&wave.ControlState,
			&wave.ControlReason,
			&wave.FailurePolicyState,
			&wave.FailureReason,
			&wave.Repair.WorkerID,
			&wave.Repair.AttemptCount,
			&acceptanceID,
			&acceptanceStatus,
			&wave.CreatedAt,
			&wave.UpdatedAt,
			&wave.LaunchedAt,
			&wave.CompletedAt,
		); err != nil {
			return nil, missionControlFreshnessTracker{}, err
		}
		tracker.observe(wave.UpdatedAt)
		wave.CreatedAt = normalizeMissionControlTimestamp(wave.CreatedAt)
		wave.UpdatedAt = normalizeMissionControlTimestamp(wave.UpdatedAt)
		wave.LaunchedAt = normalizeMissionControlTimestamp(wave.LaunchedAt)
		wave.CompletedAt = normalizeMissionControlTimestamp(wave.CompletedAt)
		wave.ControlReason = boundedMissionControlText(wave.ControlReason, 240)
		wave.FailureReason = boundedMissionControlText(wave.FailureReason, 240)
		wave.Review = MissionControlLinkedSessionState{State: "not_started"}
		wave.Acceptance = missionControlLinkedSessionState(acceptanceID, localSessionIDs[acceptanceID], acceptanceStatus)
		wave.Repair.State = missionControlRepairState(wave.FailurePolicyState)
		wave.Workers = []MissionControlWaveWorker{}
		waves = append(waves, wave)
	}
	if err := rows.Err(); err != nil {
		return nil, missionControlFreshnessTracker{}, err
	}
	return waves, tracker, nil
}

func (r *Repository) loadMissionControlWaveWorkers(ctx context.Context, projectID int, localSessionIDs map[int]int, waves []MissionControlWave, tracker *missionControlFreshnessTracker) error {
	if len(waves) == 0 || !r.tableExists(ctx, "parallel_wave_worker") {
		return nil
	}
	columns, err := r.missionControlTableColumns(ctx, "parallel_wave_worker")
	if err != nil {
		return err
	}
	column := func(name string, fallback string) string {
		if columns[name] {
			return fmt.Sprintf("COALESCE(worker.%s, %s)", name, fallback)
		}
		return fallback
	}
	rows, err := r.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT worker.id,
		       worker.wave_id,
		       COALESCE(session.id, 0),
		       COALESCE(worker.status, 'created'),
		       COALESCE(session.status, ''),
		       COALESCE(session.cli_backend, ''),
		       COALESCE(session.cli_model, ''),
		       COALESCE(session.cli_reasoning, ''),
		       %s,
		       %s,
		       %s,
		       %s
		FROM parallel_wave_worker worker
		JOIN parallel_wave wave ON wave.id = worker.wave_id AND wave.project_id = worker.project_id
		LEFT JOIN session session ON session.id = worker.session_id AND session.project_id = worker.project_id
		WHERE wave.project_id = ? AND worker.project_id = ?
		ORDER BY worker.wave_id DESC, COALESCE(session.id, 0), worker.id`,
		column("terminal_outcome", "''"),
		column("failure_reason", "''"),
		column("retry_next_eligible_at", "''"),
		column("updated_at", "''"),
	), projectID, projectID)
	if err != nil {
		return err
	}
	defer rows.Close()

	waveIndexes := make(map[int]int, len(waves))
	for index := range waves {
		waveIndexes[waves[index].WaveID] = index
	}
	for rows.Next() {
		var worker MissionControlWaveWorker
		var waveID int
		var retryNextEligibleAt string
		if err := rows.Scan(
			&worker.WorkerID,
			&waveID,
			&worker.SessionID,
			&worker.Status,
			&worker.SessionStatus,
			&worker.Backend,
			&worker.Model,
			&worker.Reasoning,
			&worker.Outcome,
			&worker.FailureReason,
			&retryNextEligibleAt,
			&worker.UpdatedAt,
		); err != nil {
			return err
		}
		index, found := waveIndexes[waveID]
		if !found {
			continue
		}
		worker.LocalSessionID = localSessionIDs[worker.SessionID]
		worker.Status = strings.TrimSpace(worker.Status)
		worker.Outcome = strings.TrimSpace(worker.Outcome)
		if worker.Outcome == "" && missionControlWorkerTerminal(worker.Status) && worker.Status != "cleaned" {
			worker.Outcome = worker.Status
		}
		worker.FailureReason = boundedMissionControlText(worker.FailureReason, 240)
		worker.RetryPending = worker.Status == "retry_wait" || strings.TrimSpace(retryNextEligibleAt) != ""
		tracker.observe(worker.UpdatedAt)
		worker.UpdatedAt = normalizeMissionControlTimestamp(worker.UpdatedAt)
		waves[index].Workers = append(waves[index].Workers, worker)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for waveIndex := range waves {
		for workerIndex := range waves[waveIndex].Workers {
			worker := waves[waveIndex].Workers[workerIndex]
			if worker.WorkerID != waves[waveIndex].Repair.WorkerID {
				continue
			}
			waves[waveIndex].Repair.SessionID = worker.SessionID
			waves[waveIndex].Repair.LocalSessionID = worker.LocalSessionID
			break
		}
		if waves[waveIndex].Repair.SessionID == 0 {
			waves[waveIndex].Repair.WorkerID = 0
		}
	}
	return nil
}

func (r *Repository) missionControlTableColumns(ctx context.Context, tableName string) (map[string]bool, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT name FROM pragma_table_info(?)", tableName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		columns[name] = true
	}
	return columns, rows.Err()
}

func (r *Repository) loadMissionControlWaveReviews(ctx context.Context, projectID int, localSessionIDs map[int]int, waves []MissionControlWave) error {
	if len(waves) == 0 || !r.sessionHasColumn(ctx, "parallel_wave_id") {
		return nil
	}
	waveIndexes := make(map[int]int, len(waves))
	for index := range waves {
		waveIndexes[waves[index].WaveID] = index
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, status, parallel_wave_id
		FROM session
		WHERE project_id = ? AND parallel_wave_id LIKE ?
		ORDER BY id`, projectID, parallelWaveReviewMarkerPrefix+"%")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var sessionID int
		var status string
		var marker string
		if err := rows.Scan(&sessionID, &status, &marker); err != nil {
			return err
		}
		waveID, err := strconv.Atoi(strings.TrimPrefix(marker, parallelWaveReviewMarkerPrefix))
		if err != nil || waveID <= 0 {
			continue
		}
		index, found := waveIndexes[waveID]
		if !found {
			continue
		}
		waves[index].Review = missionControlLinkedSessionState(sessionID, localSessionIDs[sessionID], status)
	}
	return rows.Err()
}

func projectMissionControlWave(wave *MissionControlWave) {
	if wave == nil {
		return
	}
	wave.WorkerCounts = missionControlWorkerCounts(wave.Workers)
	allWorkersTerminal := wave.WorkerCounts.Total > 0 && wave.WorkerCounts.Terminal == wave.WorkerCounts.Total
	architectPaused := wave.ControlState == missionControlControlPausedForArchitect || wave.FailurePolicyState == missionControlFailurePaused
	waveCompleted := wave.Phase == missionControlPhaseCompleted && wave.GateState == missionControlGateClosed
	repairRequired := wave.GateState == missionControlGateImplementationRepair || missionControlRepairActive(wave.FailurePolicyState)
	waveReviewReady := wave.GateState == missionControlGateImplementationReview && wave.FailurePolicyState == missionControlFailurePassed
	acceptanceReady := wave.Phase == missionControlPhaseAcceptance && wave.Acceptance.SessionID > 0 && wave.Acceptance.State == "completed"

	wave.OperatorState, wave.Label, wave.NextAction = "implementation_active", "Implementation running", "wait"
	switch {
	case architectPaused:
		wave.OperatorState, wave.Label, wave.NextAction = "architect_paused", "Paused for Architect", "resume_by_architect"
	case waveCompleted:
		wave.OperatorState, wave.Label, wave.NextAction = "completed", "Completed", "none"
	case wave.Phase == missionControlPhaseAcceptance:
		wave.OperatorState, wave.Label, wave.NextAction = "acceptance", "Acceptance in progress", "run_acceptance"
		if acceptanceReady {
			wave.NextAction = "review_acceptance"
		}
	case repairRequired:
		wave.OperatorState, wave.Label, wave.NextAction = "repair_blocked", "Repair required", "authorize_repair"
		if wave.FailurePolicyState == missionControlFailureRepairAuthorized || wave.FailurePolicyState == missionControlFailureRepairInProgress {
			wave.NextAction = "monitor_repair"
		}
	case waveReviewReady:
		wave.OperatorState, wave.Label, wave.NextAction = "wave_review_ready", "Ready for implementation review", "review_implementation"
	case allWorkersTerminal:
		wave.OperatorState, wave.Label, wave.NextAction = "worker_terminal", "Workers terminal; review pending", "inspect_failure"
	}
}

func missionControlWorkerCounts(workers []MissionControlWaveWorker) MissionControlWorkerCounts {
	counts := MissionControlWorkerCounts{Total: len(workers)}
	for _, worker := range workers {
		if missionControlWorkerTerminal(worker.Status) {
			counts.Terminal++
		} else {
			counts.Active++
		}
		switch worker.Status {
		case "review_ready":
			counts.ReviewReady++
		case "completed", "cleaned":
			counts.Completed++
		case "failed":
			counts.Failed++
		case "stopped":
			counts.Stopped++
		case "stale_epoch":
			counts.StaleEpoch++
		case "blocked":
			counts.Blocked++
		}
		if worker.RetryPending {
			counts.RetryPending++
		}
	}
	return counts
}

func missionControlWorkerTerminal(status string) bool {
	switch strings.TrimSpace(status) {
	case "review_ready", "completed", "failed", "stopped", "stale_epoch", "cleaned", "blocked":
		return true
	default:
		return false
	}
}

func missionControlRepairActive(state string) bool {
	switch strings.TrimSpace(state) {
	case missionControlFailureRepairRequired, missionControlFailureRepairAuthorized, missionControlFailureRepairInProgress:
		return true
	default:
		return false
	}
}

func missionControlRepairState(failurePolicyState string) string {
	switch strings.TrimSpace(failurePolicyState) {
	case missionControlFailureRepairRequired:
		return "required"
	case missionControlFailureRepairAuthorized:
		return "authorized"
	case missionControlFailureRepairInProgress:
		return "in_progress"
	case missionControlFailurePassed:
		return "passed"
	case missionControlFailurePaused:
		return "paused"
	default:
		return "none"
	}
}

func missionControlLinkedSessionState(sessionID int, localSessionID int, status string) MissionControlLinkedSessionState {
	if sessionID <= 0 {
		return MissionControlLinkedSessionState{State: "not_started"}
	}
	state := strings.TrimSpace(status)
	if state == "" {
		state = "unknown"
	}
	return MissionControlLinkedSessionState{
		SessionID:      sessionID,
		LocalSessionID: localSessionID,
		State:          state,
	}
}

func missionControlWaveActionCapabilities(wave MissionControlWave, freshness MissionControlFreshness) MissionControlWaveActionCapabilities {
	disabled := func(reason string) MissionControlActionCapability {
		return MissionControlActionCapability{Enabled: false, DisabledReason: reason}
	}
	delegationReason := missionControlMutationUnavailable
	if freshness.Stale {
		delegationReason = "Runtime data is stale; refresh before evaluating governed wave actions. " + missionControlMutationUnavailable
	}

	launchReason := "Wave launch is unavailable from the current lifecycle state."
	if wave.Phase == missionControlPhaseInitialized && wave.LegacyStatus == "created" {
		launchReason = delegationReason
	}
	waitReason := delegationReason
	if wave.OperatorState == "completed" {
		waitReason = "The wave lifecycle is already completed."
	}
	repairReason := "No governed repair authorization is required for this wave."
	if wave.FailurePolicyState == missionControlFailureRepairRequired {
		repairReason = delegationReason
	}
	pauseReason := "The wave is already paused for Architect review."
	if wave.ControlState == missionControlControlActive {
		pauseReason = delegationReason
	}
	resumeReason := "The wave is not paused for Architect review."
	if wave.ControlState == missionControlControlPausedForArchitect || wave.FailurePolicyState == missionControlFailurePaused {
		resumeReason = delegationReason
	}
	acceptanceReason := "Implementation review has not reached the governed acceptance transition gate."
	if wave.OperatorState == "wave_review_ready" && wave.Review.State == "completed" {
		acceptanceReason = delegationReason
	}
	completeReason := "Acceptance has not completed its governed review contract."
	if wave.Phase == missionControlPhaseAcceptance && wave.Acceptance.State == "completed" {
		completeReason = delegationReason
	}
	cleanupReason := "Cleanup is unavailable while implementation workers remain active."
	if wave.WorkerCounts.Total > 0 && wave.WorkerCounts.Terminal == wave.WorkerCounts.Total {
		cleanupReason = delegationReason
	}

	return MissionControlWaveActionCapabilities{
		Launch:                 disabled(launchReason),
		Wait:                   disabled(waitReason),
		AuthorizeRepair:        disabled(repairReason),
		Pause:                  disabled(pauseReason),
		Resume:                 disabled(resumeReason),
		TransitionToAcceptance: disabled(acceptanceReason),
		Complete:               disabled(completeReason),
		Cleanup:                disabled(cleanupReason),
	}
}

func missionControlFreshness(generatedAt time.Time, tracker missionControlFreshnessTracker, hasWaves bool) MissionControlFreshness {
	freshness := MissionControlFreshness{
		State:             "empty",
		StaleAfterSeconds: int64(missionControlFreshnessThreshold / time.Second),
	}
	if !hasWaves {
		freshness.Reason = "No wave runtime data is recorded for this project."
		return freshness
	}
	if tracker.unparseable || !tracker.hasValue {
		freshness.State = "unknown"
		freshness.Stale = true
		freshness.SourceUpdatedAt = strings.TrimSpace(tracker.latestRaw)
		freshness.Reason = "Wave runtime timestamps are missing or invalid; refresh cannot prove that the data is current."
		return freshness
	}
	age := generatedAt.Sub(tracker.latest)
	if age < 0 {
		age = 0
	}
	freshness.SourceUpdatedAt = tracker.latest.UTC().Format(time.RFC3339Nano)
	freshness.AgeSeconds = int64(age / time.Second)
	if age >= missionControlFreshnessThreshold {
		freshness.State = "stale"
		freshness.Stale = true
		freshness.Reason = "The latest wave runtime update is at least five minutes old."
		return freshness
	}
	freshness.State = "fresh"
	return freshness
}

func (tracker *missionControlFreshnessTracker) observe(raw string) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return
	}
	parsed, ok := parseMissionControlTimestamp(trimmed)
	if !ok {
		tracker.unparseable = true
		if tracker.latestRaw == "" {
			tracker.latestRaw = trimmed
		}
		return
	}
	if !tracker.hasValue || parsed.After(tracker.latest) {
		tracker.latest = parsed
		tracker.latestRaw = trimmed
		tracker.hasValue = true
	}
}

func normalizeMissionControlTimestamp(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	parsed, ok := parseMissionControlTimestamp(trimmed)
	if !ok {
		return trimmed
	}
	return parsed.UTC().Format(time.RFC3339Nano)
}

func parseMissionControlTimestamp(raw string) (time.Time, bool) {
	for _, layout := range []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999Z07:00",
		"2006-01-02 15:04:05.999999999",
		time.DateTime,
	} {
		parsed, err := time.Parse(layout, raw)
		if err == nil {
			return parsed.UTC(), true
		}
	}
	return time.Time{}, false
}

func boundedMissionControlText(raw string, maxBytes int) string {
	trimmed := strings.TrimSpace(raw)
	if maxBytes <= 0 || len(trimmed) <= maxBytes {
		return trimmed
	}
	cut := maxBytes
	for cut > 0 && cut < len(trimmed) && trimmed[cut]&0xc0 == 0x80 {
		cut--
	}
	return strings.TrimSpace(trimmed[:cut])
}
