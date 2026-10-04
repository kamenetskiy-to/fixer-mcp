package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	plannedWaveStatusPlanned      = "planned"
	plannedWaveStatusInitializing = "initializing"
	plannedWaveStatusInitialized  = "initialized"
	plannedWaveStatusFailed       = "failed"
)

type PlannedWaveTaskInput struct {
	Key             string   `json:"key" jsonschema:"Stable key unique within the plan. Letters, numbers, dot, underscore, and hyphen are supported."`
	TaskDescription string   `json:"task_description" jsonschema:"Future Netrunner task description. No session is created until Initialize."`
	DependsOn       []string `json:"depends_on,omitempty" jsonschema:"Optional parent task keys that must complete before this task can launch."`
	Backend         string   `json:"backend,omitempty" jsonschema:"Optional CLI backend persisted onto the future session. Supported runtime backends only."`
	Model           string   `json:"model,omitempty" jsonschema:"Optional model persisted onto the future session. Defaults through the selected backend policy."`
	Reasoning       string   `json:"reasoning,omitempty" jsonschema:"Optional reasoning effort persisted onto the future session. Defaults through the selected backend policy."`
	McpServerNames  []string `json:"mcp_server_names,omitempty" jsonschema:"Project-allowed MCP servers assigned to the future session during Initialize."`
}

type CreatePlannedNetrunnerWaveInput struct {
	Title                   string                 `json:"title" jsonschema:"Human-readable planned-wave title."`
	IdempotencyKey          string                 `json:"idempotency_key,omitempty" jsonschema:"Optional project-scoped idempotency key. Reuse with a different definition is rejected."`
	Tasks                   []PlannedWaveTaskInput `json:"tasks" jsonschema:"Future work items. No Netrunner sessions are created by this operation."`
	WorktreeRoot            string                 `json:"worktree_root,omitempty" jsonschema:"Unresolved future worktree root passed to governed wave creation during Initialize."`
	BaseRef                 string                 `json:"base_ref,omitempty" jsonschema:"Unresolved future Git base ref passed to governed wave creation during Initialize."`
	Reason                  string                 `json:"reason,omitempty" jsonschema:"Optional audit reason."`
	EpicDocId               int                    `json:"epic_doc_id,omitempty" jsonschema:"Optional project-scoped epic document ID."`
	ParentWaveId            int                    `json:"parent_wave_id,omitempty" jsonschema:"Optional existing parent wave. Live lineage is validated only during Initialize."`
	MaxChildWaveDepth       int                    `json:"max_child_wave_depth,omitempty" jsonschema:"Root-only future recursion depth."`
	MaxTotalDescendantWaves int                    `json:"max_total_descendant_waves,omitempty" jsonschema:"Root-only future descendant-wave budget."`
	MaxTotalSessions        int                    `json:"max_total_sessions,omitempty" jsonschema:"Root-only future session budget."`
}

type GetPlannedNetrunnerWaveInput struct {
	PlanId int `json:"plan_id" jsonschema:"Planned-wave ID to read."`
}

type InitializePlannedNetrunnerWaveInput struct {
	PlanId int `json:"plan_id" jsonschema:"Planned-wave ID to materialize through normal wave creation."`
}

type PlannedWaveTaskSnapshot struct {
	Id                    int      `json:"id"`
	Key                   string   `json:"key"`
	Position              int      `json:"position"`
	TaskDescription       string   `json:"task_description"`
	DependsOn             []string `json:"depends_on"`
	Backend               string   `json:"backend"`
	Model                 string   `json:"model"`
	Reasoning             string   `json:"reasoning"`
	McpServerNames        []string `json:"mcp_server_names"`
	MaterializedSessionId int      `json:"materialized_session_id,omitempty"`
}

type PlannedWaveSnapshot struct {
	Id                      int                       `json:"id"`
	ProjectId               int                       `json:"project_id"`
	Title                   string                    `json:"title"`
	Status                  string                    `json:"status"`
	IdempotencyKey          string                    `json:"idempotency_key,omitempty"`
	Reason                  string                    `json:"reason,omitempty"`
	BaseRef                 string                    `json:"base_ref,omitempty"`
	WorktreeRoot            string                    `json:"worktree_root,omitempty"`
	EpicDocId               int                       `json:"epic_doc_id,omitempty"`
	ParentWaveId            int                       `json:"parent_wave_id,omitempty"`
	MaxChildWaveDepth       int                       `json:"max_child_wave_depth"`
	MaxTotalDescendantWaves int                       `json:"max_total_descendant_waves"`
	MaxTotalSessions        int                       `json:"max_total_sessions"`
	InitializedWaveId       int                       `json:"initialized_wave_id,omitempty"`
	FailureReason           string                    `json:"failure_reason,omitempty"`
	CreatedAt               string                    `json:"created_at"`
	UpdatedAt               string                    `json:"updated_at"`
	InitializedAt           string                    `json:"initialized_at,omitempty"`
	Tasks                   []PlannedWaveTaskSnapshot `json:"tasks"`
}

type CreatePlannedNetrunnerWaveOutput struct {
	Status     string              `json:"status"`
	Idempotent bool                `json:"idempotent"`
	PlanId     int                 `json:"plan_id"`
	Plan       PlannedWaveSnapshot `json:"plan"`
}

type GetPlannedNetrunnerWaveOutput struct {
	Status string              `json:"status"`
	Plan   PlannedWaveSnapshot `json:"plan"`
}

type InitializePlannedNetrunnerWaveOutput struct {
	Status     string                `json:"status"`
	Idempotent bool                  `json:"idempotent"`
	PlanId     int                   `json:"plan_id"`
	WaveId     int                   `json:"wave_id"`
	Plan       PlannedWaveSnapshot   `json:"plan"`
	Wave       NetrunnerWaveSnapshot `json:"wave"`
}

type normalizedPlannedWaveTask struct {
	Key             string   `json:"key"`
	TaskDescription string   `json:"task_description"`
	DependsOn       []string `json:"depends_on"`
	Backend         string   `json:"backend"`
	Model           string   `json:"model"`
	Reasoning       string   `json:"reasoning"`
	McpServerNames  []string `json:"mcp_server_names"`
}

type normalizedPlannedWaveDefinition struct {
	Title                   string                      `json:"title"`
	IdempotencyKey          string                      `json:"idempotency_key"`
	Tasks                   []normalizedPlannedWaveTask `json:"tasks"`
	WorktreeRoot            string                      `json:"worktree_root"`
	BaseRef                 string                      `json:"base_ref"`
	Reason                  string                      `json:"reason"`
	EpicDocID               int                         `json:"epic_doc_id"`
	ParentWaveID            int                         `json:"parent_wave_id"`
	MaxChildWaveDepth       int                         `json:"max_child_wave_depth"`
	MaxTotalDescendantWaves int                         `json:"max_total_descendant_waves"`
	MaxTotalSessions        int                         `json:"max_total_sessions"`
}

func normalizePlannedWaveDefinition(input CreatePlannedNetrunnerWaveInput) (normalizedPlannedWaveDefinition, string, error) {
	title := strings.TrimSpace(input.Title)
	if title == "" {
		return normalizedPlannedWaveDefinition{}, "", fmt.Errorf("title is required")
	}
	if len(title) > 240 {
		return normalizedPlannedWaveDefinition{}, "", fmt.Errorf("title must be at most 240 bytes")
	}
	idempotencyKey := strings.TrimSpace(input.IdempotencyKey)
	if len(idempotencyKey) > 240 {
		return normalizedPlannedWaveDefinition{}, "", fmt.Errorf("idempotency_key must be at most 240 bytes")
	}
	if len(input.Tasks) == 0 {
		return normalizedPlannedWaveDefinition{}, "", fmt.Errorf("tasks must contain at least one future work item")
	}
	if input.ParentWaveId < 0 {
		return normalizedPlannedWaveDefinition{}, "", fmt.Errorf("parent_wave_id must not be negative")
	}

	lineageInput := CreateNetrunnerWaveInput{
		ParentWaveId:            input.ParentWaveId,
		MaxChildWaveDepth:       input.MaxChildWaveDepth,
		MaxTotalDescendantWaves: input.MaxTotalDescendantWaves,
		MaxTotalSessions:        input.MaxTotalSessions,
	}
	if input.ParentWaveId == 0 {
		lineage, err := normalizeParallelWaveRootBudgets(lineageInput, len(input.Tasks))
		if err != nil {
			return normalizedPlannedWaveDefinition{}, "", err
		}
		input.MaxChildWaveDepth = lineage.MaxChildWaveDepth
		input.MaxTotalDescendantWaves = lineage.MaxTotalDescendantWaves
		input.MaxTotalSessions = lineage.MaxTotalSessions
	} else if input.MaxChildWaveDepth != 0 || input.MaxTotalDescendantWaves != 0 || input.MaxTotalSessions != 0 {
		return normalizedPlannedWaveDefinition{}, "", fmt.Errorf("child waves inherit lineage budgets; child plan inputs must not override them")
	}

	normalized := normalizedPlannedWaveDefinition{
		Title:                   title,
		IdempotencyKey:          idempotencyKey,
		Tasks:                   make([]normalizedPlannedWaveTask, 0, len(input.Tasks)),
		WorktreeRoot:            strings.TrimSpace(input.WorktreeRoot),
		BaseRef:                 strings.TrimSpace(input.BaseRef),
		Reason:                  strings.TrimSpace(input.Reason),
		EpicDocID:               input.EpicDocId,
		ParentWaveID:            input.ParentWaveId,
		MaxChildWaveDepth:       input.MaxChildWaveDepth,
		MaxTotalDescendantWaves: input.MaxTotalDescendantWaves,
		MaxTotalSessions:        input.MaxTotalSessions,
	}

	keyPosition := make(map[string]int, len(input.Tasks))
	for index, task := range input.Tasks {
		key := strings.TrimSpace(task.Key)
		if !validPlannedWaveTaskKey(key) {
			return normalizedPlannedWaveDefinition{}, "", fmt.Errorf("task key %q must contain 1-80 letters, numbers, dot, underscore, or hyphen", task.Key)
		}
		if _, duplicate := keyPosition[key]; duplicate {
			return normalizedPlannedWaveDefinition{}, "", fmt.Errorf("task key %q is duplicated", key)
		}
		keyPosition[key] = index
		description := strings.TrimSpace(task.TaskDescription)
		if description == "" {
			return normalizedPlannedWaveDefinition{}, "", fmt.Errorf("task %q task_description is required", key)
		}
		normalized.Tasks = append(normalized.Tasks, normalizedPlannedWaveTask{
			Key:             key,
			TaskDescription: description,
		})
		launchConfig, mcpServerNames, err := normalizePlannedWaveTaskAssignments(task, authorizedProjectId)
		if err != nil {
			return normalizedPlannedWaveDefinition{}, "", fmt.Errorf("task %q assignments: %w", key, err)
		}
		normalized.Tasks[index].Backend = launchConfig.Backend
		normalized.Tasks[index].Model = launchConfig.Model
		normalized.Tasks[index].Reasoning = launchConfig.Reasoning
		normalized.Tasks[index].McpServerNames = mcpServerNames
	}

	admissionWorkers := make([]parallelWaveAdmissionWorker, 0, len(normalized.Tasks))
	dependencies := make([]WaveDependency, 0, len(normalized.Tasks))
	for index, task := range input.Tasks {
		childPosition := index + 1
		seenParents := map[string]struct{}{}
		parentPositions := make([]int64, 0, len(task.DependsOn))
		for _, rawParent := range task.DependsOn {
			parent := strings.TrimSpace(rawParent)
			parentIndex, exists := keyPosition[parent]
			if !exists {
				return normalizedPlannedWaveDefinition{}, "", fmt.Errorf("task %q depends on unknown task %q", normalized.Tasks[index].Key, parent)
			}
			if parent == normalized.Tasks[index].Key {
				return normalizedPlannedWaveDefinition{}, "", fmt.Errorf("task %q must not depend on itself", parent)
			}
			if _, duplicate := seenParents[parent]; duplicate {
				continue
			}
			seenParents[parent] = struct{}{}
			normalized.Tasks[index].DependsOn = append(normalized.Tasks[index].DependsOn, parent)
			parentPositions = append(parentPositions, int64(parentIndex+1))
		}
		sort.Strings(normalized.Tasks[index].DependsOn)
		admissionWorkers = append(admissionWorkers, parallelWaveAdmissionWorker{
			SessionID: childPosition,
		})
		if len(parentPositions) > 0 {
			dependencies = append(dependencies, WaveDependency{Child: int64(childPosition), Parents: parentPositions})
		}
	}
	normalizedDependencies, err := normalizeWaveDependencies(dependencies, plannedWavePositions(len(normalized.Tasks)))
	if err != nil {
		return normalizedPlannedWaveDefinition{}, "", err
	}
	if _, err := normalizeParallelWaveAdmissionWorkersWithDependencies(admissionWorkers, normalizedDependencies); err != nil {
		return normalizedPlannedWaveDefinition{}, "", err
	}

	payload, err := json.Marshal(normalized)
	if err != nil {
		return normalizedPlannedWaveDefinition{}, "", err
	}
	sum := sha256.Sum256(payload)
	return normalized, hex.EncodeToString(sum[:]), nil
}

func normalizePlannedWaveTaskAssignments(task PlannedWaveTaskInput, projectID int) (sessionLaunchConfig, []string, error) {
	launchConfig, err := resolveSessionLaunchConfigValues(sessionLaunchConfig{Backend: defaultCliBackend}, task.Backend, task.Model, task.Reasoning)
	if err != nil {
		return sessionLaunchConfig{}, nil, err
	}
	mcpServerNames := normalizeMcpServerNames(task.McpServerNames)
	if _, err := plannedWaveMcpServerIDs(projectID, mcpServerNames); err != nil {
		return sessionLaunchConfig{}, nil, err
	}
	return launchConfig, mcpServerNames, nil
}

func plannedWaveMcpServerIDs(projectID int, names []string) ([]int, error) {
	if err := ensureProjectMcpBindingsForProject(projectID); err != nil {
		return nil, fmt.Errorf("failed to bootstrap project MCP bindings: %v", err)
	}
	allowedNames, err := loadProjectAllowedMcpNames(projectID)
	if err != nil {
		return nil, fmt.Errorf("failed to load project MCP policy: %v", err)
	}
	serverIDs := make([]int, 0, len(names))
	for _, name := range names {
		if _, ok := allowedNames[name]; !ok {
			return nil, fmt.Errorf("MCP server %q is not allowed for this project", name)
		}
		var serverID int
		var archived int
		if err := db.QueryRow("SELECT id, COALESCE(archived, 0) FROM mcp_server WHERE name = ?", name).Scan(&serverID, &archived); err == sql.ErrNoRows {
			return nil, fmt.Errorf("unknown MCP server %q", name)
		} else if err != nil {
			return nil, fmt.Errorf("failed to resolve MCP server %q: %v", name, err)
		}
		if archived != 0 {
			return nil, fmt.Errorf("MCP server %q is archived", name)
		}
		serverIDs = append(serverIDs, serverID)
	}
	return serverIDs, nil
}

func validPlannedWaveTaskKey(value string) bool {
	if len(value) == 0 || len(value) > 80 {
		return false
	}
	for _, char := range value {
		switch {
		case char >= 'a' && char <= 'z':
		case char >= 'A' && char <= 'Z':
		case char >= '0' && char <= '9':
		case char == '.', char == '_', char == '-':
		default:
			return false
		}
	}
	return true
}

func plannedWavePositions(count int) []int {
	positions := make([]int, count)
	for index := range positions {
		positions[index] = index + 1
	}
	return positions
}

func CreatePlannedNetrunnerWave(ctx context.Context, req *mcp.CallToolRequest, input CreatePlannedNetrunnerWaveInput) (*mcp.CallToolResult, CreatePlannedNetrunnerWaveOutput, error) {
	if authorizedRole != "fixer" || authorizedProjectId <= 0 {
		return &mcp.CallToolResult{IsError: true}, CreatePlannedNetrunnerWaveOutput{}, fmt.Errorf("access denied: requires project-bound fixer role")
	}
	definition, definitionHash, err := normalizePlannedWaveDefinition(input)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CreatePlannedNetrunnerWaveOutput{}, err
	}
	epicDocID, err := resolveProjectScopedEpicDocID(definition.EpicDocID, authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CreatePlannedNetrunnerWaveOutput{}, err
	}
	if definition.ParentWaveID > 0 {
		var parentProjectID int
		if err := db.QueryRowContext(ctx, "SELECT project_id FROM parallel_wave WHERE id = ?", definition.ParentWaveID).Scan(&parentProjectID); err == sql.ErrNoRows {
			return &mcp.CallToolResult{IsError: true}, CreatePlannedNetrunnerWaveOutput{}, fmt.Errorf("parent wave %d not found", definition.ParentWaveID)
		} else if err != nil {
			return &mcp.CallToolResult{IsError: true}, CreatePlannedNetrunnerWaveOutput{}, fmt.Errorf("DB query error: %v", err)
		} else if parentProjectID != authorizedProjectId {
			return &mcp.CallToolResult{IsError: true}, CreatePlannedNetrunnerWaveOutput{}, fmt.Errorf("parent wave %d belongs to another project", definition.ParentWaveID)
		}
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CreatePlannedNetrunnerWaveOutput{}, fmt.Errorf("DB transaction error: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `
		INSERT INTO planned_wave (
			project_id, title, status, idempotency_key, definition_hash, reason, base_ref,
			worktree_root, epic_doc_id, parent_wave_id, max_child_wave_depth,
			max_total_descendant_waves, max_total_sessions, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
		authorizedProjectId,
		definition.Title,
		plannedWaveStatusPlanned,
		definition.IdempotencyKey,
		definitionHash,
		definition.Reason,
		definition.BaseRef,
		definition.WorktreeRoot,
		nullableEpicDocID(epicDocID),
		nullablePositiveInt(definition.ParentWaveID),
		definition.MaxChildWaveDepth,
		definition.MaxTotalDescendantWaves,
		definition.MaxTotalSessions,
	)
	if err != nil {
		_ = tx.Rollback()
		if definition.IdempotencyKey != "" {
			existing, lookupErr := fetchPlannedWaveByIdempotencyKey(ctx, authorizedProjectId, definition.IdempotencyKey)
			if lookupErr == nil {
				var existingHash string
				if hashErr := db.QueryRowContext(ctx, "SELECT definition_hash FROM planned_wave WHERE id = ? AND project_id = ?", existing.Id, authorizedProjectId).Scan(&existingHash); hashErr != nil {
					return &mcp.CallToolResult{IsError: true}, CreatePlannedNetrunnerWaveOutput{}, fmt.Errorf("DB query error: %v", hashErr)
				}
				if existingHash != definitionHash {
					return &mcp.CallToolResult{IsError: true}, CreatePlannedNetrunnerWaveOutput{}, fmt.Errorf("idempotency_key %q already identifies a different planned-wave definition", definition.IdempotencyKey)
				}
				return nil, CreatePlannedNetrunnerWaveOutput{Status: "success", Idempotent: true, PlanId: existing.Id, Plan: existing}, nil
			}
		}
		return &mcp.CallToolResult{IsError: true}, CreatePlannedNetrunnerWaveOutput{}, fmt.Errorf("DB insert error: %v", err)
	}
	planID64, err := result.LastInsertId()
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CreatePlannedNetrunnerWaveOutput{}, fmt.Errorf("LastInsertId error: %v", err)
	}
	planID := int(planID64)
	for position, task := range definition.Tasks {
		dependencies, _ := json.Marshal(task.DependsOn)
		mcpServerNames, _ := json.Marshal(task.McpServerNames)
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO planned_wave_task (
				planned_wave_id, project_id, task_key, position, task_description,
				dependencies, cli_backend, cli_model, cli_reasoning,
				mcp_server_names, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
			planID, authorizedProjectId, task.Key, position+1, task.TaskDescription, string(dependencies),
			task.Backend, task.Model, task.Reasoning, string(mcpServerNames),
		); err != nil {
			return &mcp.CallToolResult{IsError: true}, CreatePlannedNetrunnerWaveOutput{}, fmt.Errorf("DB insert error: %v", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return &mcp.CallToolResult{IsError: true}, CreatePlannedNetrunnerWaveOutput{}, fmt.Errorf("DB commit error: %v", err)
	}
	plan, err := fetchPlannedWaveSnapshot(ctx, planID, authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CreatePlannedNetrunnerWaveOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	return nil, CreatePlannedNetrunnerWaveOutput{Status: "success", PlanId: planID, Plan: plan}, nil
}

func GetPlannedNetrunnerWave(ctx context.Context, req *mcp.CallToolRequest, input GetPlannedNetrunnerWaveInput) (*mcp.CallToolResult, GetPlannedNetrunnerWaveOutput, error) {
	if authorizedRole != "fixer" || authorizedProjectId <= 0 {
		return &mcp.CallToolResult{IsError: true}, GetPlannedNetrunnerWaveOutput{}, fmt.Errorf("access denied: requires project-bound fixer role")
	}
	plan, err := fetchPlannedWaveSnapshot(ctx, input.PlanId, authorizedProjectId)
	if err == sql.ErrNoRows {
		return &mcp.CallToolResult{IsError: true}, GetPlannedNetrunnerWaveOutput{}, fmt.Errorf("planned wave %d not found in current project", input.PlanId)
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetPlannedNetrunnerWaveOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	return nil, GetPlannedNetrunnerWaveOutput{Status: "success", Plan: plan}, nil
}

func InitializePlannedNetrunnerWave(ctx context.Context, req *mcp.CallToolRequest, input InitializePlannedNetrunnerWaveInput) (*mcp.CallToolResult, InitializePlannedNetrunnerWaveOutput, error) {
	if authorizedRole != "fixer" || authorizedProjectId <= 0 {
		return &mcp.CallToolResult{IsError: true}, InitializePlannedNetrunnerWaveOutput{}, fmt.Errorf("access denied: requires project-bound fixer role")
	}
	if input.PlanId <= 0 {
		return &mcp.CallToolResult{IsError: true}, InitializePlannedNetrunnerWaveOutput{}, fmt.Errorf("plan_id must be positive")
	}
	if recovered, ok, err := recoverInitializedPlannedWave(ctx, input.PlanId, authorizedProjectId); err != nil {
		return &mcp.CallToolResult{IsError: true}, InitializePlannedNetrunnerWaveOutput{}, err
	} else if ok {
		return nil, recovered, nil
	}

	result, err := db.ExecContext(ctx, `
		UPDATE planned_wave
		SET status = ?, failure_reason = '', updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND project_id = ? AND status IN (?, ?)`,
		plannedWaveStatusInitializing, input.PlanId, authorizedProjectId, plannedWaveStatusPlanned, plannedWaveStatusFailed,
	)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, InitializePlannedNetrunnerWaveOutput{}, fmt.Errorf("DB update error: %v", err)
	}
	claimed, err := result.RowsAffected()
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, InitializePlannedNetrunnerWaveOutput{}, fmt.Errorf("DB update error: %v", err)
	}
	if claimed != 1 {
		plan, fetchErr := fetchPlannedWaveSnapshot(ctx, input.PlanId, authorizedProjectId)
		if fetchErr == sql.ErrNoRows {
			return &mcp.CallToolResult{IsError: true}, InitializePlannedNetrunnerWaveOutput{}, fmt.Errorf("planned wave %d not found in current project", input.PlanId)
		}
		if fetchErr != nil {
			return &mcp.CallToolResult{IsError: true}, InitializePlannedNetrunnerWaveOutput{}, fmt.Errorf("DB query error: %v", fetchErr)
		}
		if plan.Status == plannedWaveStatusInitialized {
			return initializedPlannedWaveOutput(ctx, plan, true)
		}
		return &mcp.CallToolResult{IsError: true}, InitializePlannedNetrunnerWaveOutput{}, fmt.Errorf("planned wave %d initialization is already in progress", input.PlanId)
	}

	plan, err := fetchPlannedWaveSnapshot(ctx, input.PlanId, authorizedProjectId)
	if err != nil {
		return failPlannedWaveInitialization(ctx, input.PlanId, fmt.Errorf("DB query error: %v", err))
	}
	sessionIDs, dependencies, err := materializePlannedWaveSessions(ctx, plan)
	if err != nil {
		return failPlannedWaveInitialization(ctx, input.PlanId, err)
	}
	callResult, created, err := CreateNetrunnerWave(ctx, req, CreateNetrunnerWaveInput{
		SessionIds:              sessionIDs,
		Dependencies:            dependencies,
		WorktreeRoot:            plan.WorktreeRoot,
		BaseRef:                 plan.BaseRef,
		Reason:                  plan.Reason,
		EpicDocId:               plan.EpicDocId,
		ParentWaveId:            plan.ParentWaveId,
		MaxChildWaveDepth:       plan.MaxChildWaveDepth,
		MaxTotalDescendantWaves: plan.MaxTotalDescendantWaves,
		MaxTotalSessions:        plan.MaxTotalSessions,
	})
	if err != nil {
		_ = callResult
		return failPlannedWaveInitialization(ctx, input.PlanId, err)
	}
	update, err := db.ExecContext(ctx, `
		UPDATE planned_wave
		SET status = ?, initialized_wave_id = ?, failure_reason = '',
		    initialized_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND project_id = ? AND status = ?`,
		plannedWaveStatusInitialized, created.WaveId, input.PlanId, authorizedProjectId, plannedWaveStatusInitializing,
	)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, InitializePlannedNetrunnerWaveOutput{}, fmt.Errorf("wave %d was created but planned-wave finalization failed: %v", created.WaveId, err)
	}
	updated, _ := update.RowsAffected()
	if updated != 1 {
		return &mcp.CallToolResult{IsError: true}, InitializePlannedNetrunnerWaveOutput{}, fmt.Errorf("wave %d was created but planned-wave finalization lost its initialization claim", created.WaveId)
	}
	plan, err = fetchPlannedWaveSnapshot(ctx, input.PlanId, authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, InitializePlannedNetrunnerWaveOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	return nil, InitializePlannedNetrunnerWaveOutput{
		Status: "success", PlanId: plan.Id, WaveId: created.WaveId, Plan: plan, Wave: created.Wave,
	}, nil
}

func nullablePositiveInt(value int) any {
	if value <= 0 {
		return nil
	}
	return value
}

func fetchPlannedWaveByIdempotencyKey(ctx context.Context, projectID int, key string) (PlannedWaveSnapshot, error) {
	var planID int
	if err := db.QueryRowContext(ctx, "SELECT id FROM planned_wave WHERE project_id = ? AND idempotency_key = ?", projectID, key).Scan(&planID); err != nil {
		return PlannedWaveSnapshot{}, err
	}
	return fetchPlannedWaveSnapshot(ctx, planID, projectID)
}

func fetchPlannedWaveSnapshot(ctx context.Context, planID int, projectID int) (PlannedWaveSnapshot, error) {
	var plan PlannedWaveSnapshot
	var epicDocID sql.NullInt64
	var parentWaveID sql.NullInt64
	var initializedWaveID sql.NullInt64
	var initializedAt sql.NullString
	err := db.QueryRowContext(ctx, `
		SELECT id, project_id, title, status, idempotency_key, reason, base_ref, worktree_root,
		       epic_doc_id, parent_wave_id, max_child_wave_depth, max_total_descendant_waves,
		       max_total_sessions, initialized_wave_id, failure_reason, created_at, updated_at, initialized_at
		FROM planned_wave
		WHERE id = ? AND project_id = ?`, planID, projectID).Scan(
		&plan.Id, &plan.ProjectId, &plan.Title, &plan.Status, &plan.IdempotencyKey, &plan.Reason,
		&plan.BaseRef, &plan.WorktreeRoot, &epicDocID, &parentWaveID, &plan.MaxChildWaveDepth,
		&plan.MaxTotalDescendantWaves, &plan.MaxTotalSessions, &initializedWaveID,
		&plan.FailureReason, &plan.CreatedAt, &plan.UpdatedAt, &initializedAt,
	)
	if err != nil {
		return PlannedWaveSnapshot{}, err
	}
	if epicDocID.Valid {
		localID, err := projectScopedDocIDFromGlobal(int(epicDocID.Int64), projectID)
		if err != nil {
			return PlannedWaveSnapshot{}, err
		}
		plan.EpicDocId = localID
	}
	if parentWaveID.Valid {
		plan.ParentWaveId = int(parentWaveID.Int64)
	}
	if initializedWaveID.Valid {
		plan.InitializedWaveId = int(initializedWaveID.Int64)
	}
	if initializedAt.Valid {
		plan.InitializedAt = initializedAt.String
	}
	rows, err := db.QueryContext(ctx, `
		SELECT id, task_key, position, task_description, dependencies,
		       COALESCE(NULLIF(TRIM(cli_backend), ''), ?), COALESCE(cli_model, ''),
		       COALESCE(cli_reasoning, ''), COALESCE(mcp_server_names, '[]'),
		       COALESCE((
		           SELECT COUNT(*)
		           FROM session ranked
		           WHERE ranked.project_id = task.project_id
		             AND ranked.id <= task.materialized_session_id
		       ), 0)
		FROM planned_wave_task task
		WHERE planned_wave_id = ? AND project_id = ?
		ORDER BY position, id`, defaultCliBackend, planID, projectID)
	if err != nil {
		return PlannedWaveSnapshot{}, err
	}
	defer rows.Close()
	plan.Tasks = []PlannedWaveTaskSnapshot{}
	for rows.Next() {
		var task PlannedWaveTaskSnapshot
		var dependenciesPayload string
		var mcpServerNamesPayload string
		if err := rows.Scan(
			&task.Id, &task.Key, &task.Position, &task.TaskDescription,
			&dependenciesPayload, &task.Backend, &task.Model, &task.Reasoning,
			&mcpServerNamesPayload, &task.MaterializedSessionId,
		); err != nil {
			return PlannedWaveSnapshot{}, err
		}
		if err := json.Unmarshal([]byte(dependenciesPayload), &task.DependsOn); err != nil {
			return PlannedWaveSnapshot{}, fmt.Errorf("invalid planned task %q dependencies: %w", task.Key, err)
		}
		if err := json.Unmarshal([]byte(mcpServerNamesPayload), &task.McpServerNames); err != nil {
			return PlannedWaveSnapshot{}, fmt.Errorf("invalid planned task %q mcp_server_names: %w", task.Key, err)
		}
		plan.Tasks = append(plan.Tasks, task)
	}
	if err := rows.Err(); err != nil {
		return PlannedWaveSnapshot{}, err
	}
	return plan, nil
}

func materializePlannedWaveSessions(ctx context.Context, plan PlannedWaveSnapshot) ([]int, []WaveDependency, error) {
	epicDocID, err := resolveProjectScopedEpicDocID(plan.EpicDocId, plan.ProjectId)
	if err != nil {
		return nil, nil, err
	}
	hasEpicDocColumn := dbTableHasColumn("session", "epic_doc_id")
	if epicDocID > 0 && !hasEpicDocColumn {
		return nil, nil, fmt.Errorf("session table is missing epic_doc_id")
	}
	mcpServerIDsByTask := make(map[int][]int, len(plan.Tasks))
	for index := range plan.Tasks {
		task := &plan.Tasks[index]
		resolved, err := resolveSessionLaunchConfigValues(sessionLaunchConfig{Backend: defaultCliBackend}, task.Backend, task.Model, task.Reasoning)
		if err != nil {
			return nil, nil, fmt.Errorf("planned task %q launch config is no longer valid: %v", task.Key, err)
		}
		task.Backend, task.Model, task.Reasoning = resolved.Backend, resolved.Model, resolved.Reasoning
		serverIDs, err := plannedWaveMcpServerIDs(plan.ProjectId, task.McpServerNames)
		if err != nil {
			return nil, nil, fmt.Errorf("planned task %q MCP assignment is no longer valid: %v", task.Key, err)
		}
		mcpServerIDsByTask[task.Id] = serverIDs
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to begin planned-wave materialization: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	globalSessionByKey := make(map[string]int, len(plan.Tasks))
	localSessionByKey := make(map[string]int, len(plan.Tasks))
	sessionIDs := make([]int, 0, len(plan.Tasks))
	for _, task := range plan.Tasks {
		var globalSessionID sql.NullInt64
		if err := tx.QueryRowContext(ctx, `
			SELECT materialized_session_id
			FROM planned_wave_task
			WHERE id = ? AND planned_wave_id = ? AND project_id = ?`,
			task.Id, plan.Id, plan.ProjectId,
		).Scan(&globalSessionID); err != nil {
			return nil, nil, fmt.Errorf("failed to read planned task %q: %v", task.Key, err)
		}
		if !globalSessionID.Valid {
			var result sql.Result
			if hasEpicDocColumn {
				result, err = tx.ExecContext(ctx, `
					INSERT INTO session (
						project_id, task_description, status, epic_doc_id,
						cli_backend, cli_model, cli_reasoning
					) VALUES (?, ?, 'pending', ?, ?, ?, ?)`,
					plan.ProjectId, task.TaskDescription, nullableEpicDocID(epicDocID),
					task.Backend, task.Model, task.Reasoning,
				)
			} else {
				result, err = tx.ExecContext(ctx, `
					INSERT INTO session (
						project_id, task_description, status,
						cli_backend, cli_model, cli_reasoning
					) VALUES (?, ?, 'pending', ?, ?, ?)`,
					plan.ProjectId, task.TaskDescription, task.Backend, task.Model, task.Reasoning,
				)
			}
			if err != nil {
				return nil, nil, fmt.Errorf("failed to materialize planned task %q: %v", task.Key, err)
			}
			insertedID, err := result.LastInsertId()
			if err != nil {
				return nil, nil, err
			}
			globalSessionID = sql.NullInt64{Int64: insertedID, Valid: true}
			update, err := tx.ExecContext(ctx, `
				UPDATE planned_wave_task
				SET materialized_session_id = ?, updated_at = CURRENT_TIMESTAMP
				WHERE id = ? AND planned_wave_id = ? AND project_id = ? AND materialized_session_id IS NULL`,
				insertedID, task.Id, plan.Id, plan.ProjectId,
			)
			if err != nil {
				return nil, nil, err
			}
			updated, _ := update.RowsAffected()
			if updated != 1 {
				return nil, nil, fmt.Errorf("planned task %q lost its materialization claim", task.Key)
			}
		}
		configUpdate, err := tx.ExecContext(ctx, `
			UPDATE session
			SET cli_backend = ?, cli_model = ?, cli_reasoning = ?
			WHERE id = ? AND project_id = ? AND status = 'pending'`,
			task.Backend, task.Model, task.Reasoning, globalSessionID.Int64, plan.ProjectId,
		)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to persist planned task %q launch config: %v", task.Key, err)
		}
		if updated, err := configUpdate.RowsAffected(); err != nil || updated != 1 {
			return nil, nil, fmt.Errorf("planned task %q materialized session is no longer pending", task.Key)
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM session_mcp_server WHERE session_id = ?", globalSessionID.Int64); err != nil {
			return nil, nil, fmt.Errorf("failed to reset planned task %q MCP assignments: %v", task.Key, err)
		}
		for _, serverID := range mcpServerIDsByTask[task.Id] {
			if _, err := tx.ExecContext(ctx, "INSERT INTO session_mcp_server (session_id, mcp_server_id) VALUES (?, ?)", globalSessionID.Int64, serverID); err != nil {
				return nil, nil, fmt.Errorf("failed to persist planned task %q MCP assignments: %v", task.Key, err)
			}
		}
		globalSessionByKey[task.Key] = int(globalSessionID.Int64)
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("failed to commit planned-wave sessions: %v", err)
	}
	for _, task := range plan.Tasks {
		localID, err := projectScopedSessionIDFromGlobal(globalSessionByKey[task.Key], plan.ProjectId)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to map materialized task %q: %v", task.Key, err)
		}
		localSessionByKey[task.Key] = localID
		sessionIDs = append(sessionIDs, localID)
	}
	dependencies := make([]WaveDependency, 0, len(plan.Tasks))
	for _, task := range plan.Tasks {
		if len(task.DependsOn) == 0 {
			continue
		}
		parents := make([]int64, 0, len(task.DependsOn))
		for _, parent := range task.DependsOn {
			parents = append(parents, int64(localSessionByKey[parent]))
		}
		dependencies = append(dependencies, WaveDependency{Child: int64(localSessionByKey[task.Key]), Parents: parents})
	}
	return sessionIDs, dependencies, nil
}

func failPlannedWaveInitialization(ctx context.Context, planID int, cause error) (*mcp.CallToolResult, InitializePlannedNetrunnerWaveOutput, error) {
	reason := strings.TrimSpace(cause.Error())
	if len(reason) > 1000 {
		reason = reason[:1000]
	}
	_, _ = db.ExecContext(ctx, `
		UPDATE planned_wave
		SET status = ?, failure_reason = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND project_id = ? AND status = ?`,
		plannedWaveStatusFailed, reason, planID, authorizedProjectId, plannedWaveStatusInitializing,
	)
	return &mcp.CallToolResult{IsError: true}, InitializePlannedNetrunnerWaveOutput{}, cause
}

func recoverInitializedPlannedWave(ctx context.Context, planID int, projectID int) (InitializePlannedNetrunnerWaveOutput, bool, error) {
	plan, err := fetchPlannedWaveSnapshot(ctx, planID, projectID)
	if err == sql.ErrNoRows {
		return InitializePlannedNetrunnerWaveOutput{}, false, nil
	}
	if err != nil {
		return InitializePlannedNetrunnerWaveOutput{}, false, fmt.Errorf("DB query error: %v", err)
	}
	if plan.Status == plannedWaveStatusInitialized {
		callResult, output, err := initializedPlannedWaveOutput(ctx, plan, true)
		_ = callResult
		return output, err == nil, err
	}
	var waveMarker string
	err = db.QueryRowContext(ctx, `
		SELECT MIN(session.parallel_wave_id)
		FROM planned_wave_task task
		JOIN session ON session.id = task.materialized_session_id AND session.project_id = task.project_id
		WHERE task.planned_wave_id = ? AND task.project_id = ?
		HAVING COUNT(*) = (SELECT COUNT(*) FROM planned_wave_task WHERE planned_wave_id = ? AND project_id = ?)
		   AND COUNT(DISTINCT session.parallel_wave_id) = 1
		   AND TRIM(MIN(session.parallel_wave_id)) != ''`,
		planID, projectID, planID, projectID,
	).Scan(&waveMarker)
	if err == sql.ErrNoRows {
		return InitializePlannedNetrunnerWaveOutput{}, false, nil
	}
	if err != nil {
		return InitializePlannedNetrunnerWaveOutput{}, false, fmt.Errorf("DB query error: %v", err)
	}
	waveID, err := strconv.Atoi(waveMarker)
	if err != nil || waveID <= 0 {
		return InitializePlannedNetrunnerWaveOutput{}, false, nil
	}
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM parallel_wave WHERE id = ? AND project_id = ?", waveID, projectID).Scan(&count); err != nil {
		return InitializePlannedNetrunnerWaveOutput{}, false, err
	}
	if count != 1 {
		return InitializePlannedNetrunnerWaveOutput{}, false, nil
	}
	if _, err := db.ExecContext(ctx, `
		UPDATE planned_wave
		SET status = ?, initialized_wave_id = ?, failure_reason = '',
		    initialized_at = COALESCE(initialized_at, CURRENT_TIMESTAMP), updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND project_id = ? AND initialized_wave_id IS NULL`,
		plannedWaveStatusInitialized, waveID, planID, projectID,
	); err != nil {
		return InitializePlannedNetrunnerWaveOutput{}, false, err
	}
	plan, err = fetchPlannedWaveSnapshot(ctx, planID, projectID)
	if err != nil {
		return InitializePlannedNetrunnerWaveOutput{}, false, err
	}
	callResult, output, err := initializedPlannedWaveOutput(ctx, plan, true)
	_ = callResult
	return output, err == nil, err
}

func initializedPlannedWaveOutput(ctx context.Context, plan PlannedWaveSnapshot, idempotent bool) (*mcp.CallToolResult, InitializePlannedNetrunnerWaveOutput, error) {
	if plan.InitializedWaveId <= 0 {
		return &mcp.CallToolResult{IsError: true}, InitializePlannedNetrunnerWaveOutput{}, fmt.Errorf("planned wave %d is initialized without an initialized_wave_id", plan.Id)
	}
	wave, err := fetchNetrunnerWaveSnapshot(plan.InitializedWaveId, plan.ProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, InitializePlannedNetrunnerWaveOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	return nil, InitializePlannedNetrunnerWaveOutput{
		Status: "success", Idempotent: idempotent, PlanId: plan.Id,
		WaveId: plan.InitializedWaveId, Plan: plan, Wave: wave,
	}, nil
}
