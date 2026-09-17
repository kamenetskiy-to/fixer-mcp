package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	handsActorDisplayName    = "Руки"
	handsInstructionMaxBytes = 64 * 1024
	handsReviewNoteMaxBytes  = 8 * 1024
	handsWaitMaxSeconds      = 25
)

var handsProviders = map[string]struct{}{
	"codex": {}, "commandcode": {}, "claude": {}, "kimi-code": {}, "antigravity": {}, "grok": {},
}

var terminalHandsInstructionStates = map[string]struct{}{
	"completed": {}, "cancelled": {}, "failed": {}, "abandoned": {}, "unsupported": {},
}

var handsWaiters = struct {
	sync.Mutex
	nextID    uint64
	byProject map[int]map[uint64]chan struct{}
}{byProject: map[int]map[uint64]chan struct{}{}}

func registerHandsWaiter(projectID int) (<-chan struct{}, func()) {
	handsWaiters.Lock()
	handsWaiters.nextID++
	id := handsWaiters.nextID
	if handsWaiters.byProject[projectID] == nil {
		handsWaiters.byProject[projectID] = map[uint64]chan struct{}{}
	}
	ch := make(chan struct{}, 1)
	handsWaiters.byProject[projectID][id] = ch
	handsWaiters.Unlock()
	return ch, func() {
		handsWaiters.Lock()
		delete(handsWaiters.byProject[projectID], id)
		if len(handsWaiters.byProject[projectID]) == 0 {
			delete(handsWaiters.byProject, projectID)
		}
		handsWaiters.Unlock()
	}
}

func notifyHandsWaiters(projectID int) {
	handsWaiters.Lock()
	defer handsWaiters.Unlock()
	for _, ch := range handsWaiters.byProject[projectID] {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func currentHandsProviderProcessIdentity() (int, string) {
	pid := os.Getppid()
	if raw, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat")); err == nil {
		fields := strings.Fields(string(raw))
		if len(fields) > 21 {
			return pid, "proc-start-ticks:" + fields[21]
		}
	}
	if raw, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "lstart=").Output(); err == nil {
		started := strings.Join(strings.Fields(string(raw)), " ")
		if started != "" {
			return pid, "ps-lstart:" + started
		}
	}
	return 0, ""
}

func decodeHandsJSONObject(raw, field string) (map[string]any, error) {
	value := map[string]any{}
	if strings.TrimSpace(raw) == "" {
		return value, nil
	}
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return nil, fmt.Errorf("decode %s object: %w", field, err)
	}
	if value == nil {
		value = map[string]any{}
	}
	return value, nil
}

type HandsProviderLane struct {
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	Reasoning string `json:"reasoning"`
}

type HandsInstructionSummary struct {
	InstructionID      string   `json:"instruction_id"`
	Ordinal            int      `json:"ordinal"`
	InstructionText    string   `json:"instruction_text"`
	DeclaredWriteScope []string `json:"declared_write_scope"`
	RequestedLane      string   `json:"requested_lane"`
	RiskClass          string   `json:"risk_class"`
	ReviewPolicy       string   `json:"review_policy"`
	State              string   `json:"state"`
	StateReasonCode    string   `json:"state_reason_code,omitempty"`
	StateReasonText    string   `json:"state_reason_text,omitempty"`
	Revision           int      `json:"revision"`
	CreatedAt          string   `json:"created_at"`
	UpdatedAt          string   `json:"updated_at"`
	TerminalAt         string   `json:"terminal_at,omitempty"`
}

type HandsInstructionEvent struct {
	Ordinal   int            `json:"ordinal"`
	EventType string         `json:"event_type"`
	FromState string         `json:"from_state,omitempty"`
	ToState   string         `json:"to_state,omitempty"`
	ActorKind string         `json:"actor_kind"`
	ActorID   string         `json:"actor_id"`
	Payload   map[string]any `json:"payload"`
	CreatedAt string         `json:"created_at"`
}

type HandsGeneration struct {
	Generation           int            `json:"generation"`
	Provider             string         `json:"provider"`
	Model                string         `json:"model"`
	Reasoning            string         `json:"reasoning"`
	Status               string         `json:"status"`
	ExternalSessionID    string         `json:"external_session_id,omitempty"`
	ProcessID            int            `json:"process_id,omitempty"`
	ProcessStartIdentity string         `json:"process_start_identity,omitempty"`
	BinaryBuildID        string         `json:"binary_build_id,omitempty"`
	BinaryEpoch          int            `json:"binary_epoch,omitempty"`
	LeaseSetID           string         `json:"lease_set_id,omitempty"`
	FencingToken         int64          `json:"fencing_token,omitempty"`
	LaunchMode           string         `json:"launch_mode"`
	ResultEnvelope       map[string]any `json:"result_envelope,omitempty"`
	StartedAt            string         `json:"started_at,omitempty"`
	HeartbeatAt          string         `json:"heartbeat_at,omitempty"`
	EndedAt              string         `json:"ended_at,omitempty"`
	ExitCode             *int           `json:"exit_code,omitempty"`
	StopReason           string         `json:"stop_reason,omitempty"`
}

type GetHandsStateInput struct {
	ProjectID int `json:"project_id,omitempty" jsonschema:"Overseer-only explicit global project ID. Fixer and netrunner calls are always bound to their authenticated project."`
}

type GetHandsStateOutput struct {
	ProjectID         int                      `json:"project_id"`
	ActorID           string                   `json:"actor_id"`
	DisplayName       string                   `json:"display_name"`
	AuthorityState    string                   `json:"authority_state"`
	DefaultLane       string                   `json:"default_lane"`
	DerivedState      string                   `json:"derived_state"`
	QueuedCount       int                      `json:"queued_count"`
	Lanes             []HandsProviderLane      `json:"lanes"`
	ActiveInstruction *HandsInstructionSummary `json:"active_instruction,omitempty"`
	ActiveGeneration  *HandsGeneration         `json:"active_generation,omitempty"`
	ActiveLeaseCount  int                      `json:"active_lease_count"`
	JournalHeadSeq    int64                    `json:"journal_head_seq"`
}

type ListHandsInstructionsInput struct {
	ProjectID    int `json:"project_id,omitempty" jsonschema:"Overseer-only explicit global project ID."`
	AfterOrdinal int `json:"after_ordinal,omitempty" jsonschema:"Return instructions with a greater project mailbox ordinal."`
	Limit        int `json:"limit,omitempty" jsonschema:"Page size from 1 through 100; defaults to 25."`
}

type ListHandsInstructionsOutput struct {
	Instructions []HandsInstructionSummary `json:"instructions"`
	NextOrdinal  int                       `json:"next_ordinal"`
	HasMore      bool                      `json:"has_more"`
}

type GetHandsInstructionInput struct {
	ProjectID     int    `json:"project_id,omitempty" jsonschema:"Overseer-only explicit global project ID."`
	InstructionID string `json:"instruction_id" jsonschema:"Permanent Hands instruction UUID."`
}

type GetHandsInstructionOutput struct {
	Instruction         HandsInstructionSummary `json:"instruction"`
	InstructionEnvelope map[string]any          `json:"instruction_envelope"`
	Events              []HandsInstructionEvent `json:"events"`
	Generations         []HandsGeneration       `json:"generations"`
	LatestEventOrdinal  int                     `json:"latest_event_ordinal"`
}

func scopedHandsProjectID(explicitProjectID int) (int, error) {
	if authorizedRole == "overseer" {
		if explicitProjectID <= 0 {
			return 0, fmt.Errorf("project_id is required for overseer")
		}
		return explicitProjectID, nil
	}
	if authorizedRole != "fixer" && authorizedRole != "netrunner" {
		return 0, fmt.Errorf("access denied: requires fixer, netrunner, or overseer role")
	}
	if explicitProjectID != 0 && explicitProjectID != authorizedProjectId {
		return 0, fmt.Errorf("project not found in current scope")
	}
	return authorizedProjectId, nil
}

func scanHandsInstruction(scanner interface{ Scan(...any) error }) (HandsInstructionSummary, int, string, error) {
	var item HandsInstructionSummary
	var declaredScopeJSON, envelopeJSON string
	var compatSessionID int
	err := scanner.Scan(
		&item.InstructionID, &item.Ordinal, &item.InstructionText, &declaredScopeJSON,
		&item.RequestedLane, &item.RiskClass, &item.ReviewPolicy, &item.State,
		&item.StateReasonCode, &item.StateReasonText, &compatSessionID, &item.Revision,
		&item.CreatedAt, &item.UpdatedAt, &item.TerminalAt, &envelopeJSON,
	)
	if err != nil {
		return HandsInstructionSummary{}, 0, "", err
	}
	if err := json.Unmarshal([]byte(declaredScopeJSON), &item.DeclaredWriteScope); err != nil {
		return HandsInstructionSummary{}, 0, "", fmt.Errorf("decode instruction write scope: %w", err)
	}
	if item.DeclaredWriteScope == nil {
		item.DeclaredWriteScope = []string{}
	}
	return item, compatSessionID, envelopeJSON, nil
}

const handsInstructionSelectColumns = `
	id, ordinal, instruction_text, declared_write_scope_json,
	requested_lane, risk_class, review_policy, state,
	COALESCE(state_reason_code, ''), COALESCE(state_reason_text, ''),
	COALESCE(compat_session_id, 0), revision, created_at, updated_at,
	COALESCE(terminal_at, ''), COALESCE(instruction_envelope_json, '{}')`

func readHandsLanes() []HandsProviderLane {
	lanes := make([]HandsProviderLane, 0, 7)
	for _, provider := range []string{"commandcode", "codex", "claude", "kimi-code", "antigravity", "grok", "pi"} {
		model, reasoning, _ := handsProviderConfig(provider)
		lanes = append(lanes, HandsProviderLane{Provider: provider, Model: model, Reasoning: reasoning})
	}
	return lanes
}

func readHandsGenerationRows(ctx context.Context, projectID int, instructionID string) ([]HandsGeneration, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT generation, provider, model, reasoning, status,
		       COALESCE(external_session_id, ''), COALESCE(process_id, 0),
		       COALESCE(process_start_identity, ''), COALESCE(binary_build_id, ''),
		       COALESCE(binary_epoch, 0), COALESCE(lease_set_id, ''), COALESCE(fencing_token, 0),
		       launch_mode, COALESCE(result_envelope_json, ''),
		       COALESCE(started_at, ''), COALESCE(heartbeat_at, ''), COALESCE(ended_at, ''),
		       exit_code, COALESCE(stop_reason, '')
		FROM hands_generation
		WHERE project_id = ? AND instruction_id = ?
		ORDER BY generation`, projectID, instructionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	generations := []HandsGeneration{}
	for rows.Next() {
		var generation HandsGeneration
		var resultJSON string
		var exitCode sql.NullInt64
		if err := rows.Scan(
			&generation.Generation, &generation.Provider, &generation.Model, &generation.Reasoning,
			&generation.Status, &generation.ExternalSessionID, &generation.ProcessID,
			&generation.ProcessStartIdentity, &generation.BinaryBuildID, &generation.BinaryEpoch,
			&generation.LeaseSetID, &generation.FencingToken, &generation.LaunchMode, &resultJSON,
			&generation.StartedAt, &generation.HeartbeatAt, &generation.EndedAt, &exitCode,
			&generation.StopReason,
		); err != nil {
			return nil, err
		}
		if resultJSON != "" {
			generation.ResultEnvelope, err = decodeHandsJSONObject(resultJSON, "generation result_envelope")
			if err != nil {
				return nil, err
			}
		}
		if exitCode.Valid {
			value := int(exitCode.Int64)
			generation.ExitCode = &value
		}
		generations = append(generations, generation)
	}
	return generations, rows.Err()
}

func GetHandsState(ctx context.Context, req *mcp.CallToolRequest, input GetHandsStateInput) (*mcp.CallToolResult, GetHandsStateOutput, error) {
	projectID, err := scopedHandsProjectID(input.ProjectID)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetHandsStateOutput{}, err
	}
	var output GetHandsStateOutput
	output.ProjectID = projectID
	err = db.QueryRowContext(ctx, `
		SELECT actor_id, display_name, authority_state, default_lane
		FROM project_hands WHERE project_id = ?`, projectID).Scan(
		&output.ActorID, &output.DisplayName, &output.AuthorityState, &output.DefaultLane)
	if err == sql.ErrNoRows {
		return &mcp.CallToolResult{IsError: true}, GetHandsStateOutput{}, fmt.Errorf("Hands identity not found for project")
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetHandsStateOutput{}, err
	}
	output.Lanes = readHandsLanes()
	visibilityClause := ""
	visibilityArgs := []any{projectID}
	if authorizedRole == "netrunner" {
		visibilityClause = " AND compat_session_id = ?"
		visibilityArgs = append(visibilityArgs, authorizedSessionId)
	}
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM hands_instruction
		WHERE project_id = ? AND state IN ('queued', 'waiting_for_lease', 'starting', 'running')`+visibilityClause, visibilityArgs...).Scan(&output.QueuedCount); err != nil {
		return &mcp.CallToolResult{IsError: true}, GetHandsStateOutput{}, err
	}
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq), 0) FROM project_ui_event WHERE project_id = ?`, projectID).Scan(&output.JournalHeadSeq); err != nil {
		return &mcp.CallToolResult{IsError: true}, GetHandsStateOutput{}, err
	}
	row := db.QueryRowContext(ctx, `SELECT `+handsInstructionSelectColumns+`
		FROM hands_instruction
		WHERE project_id = ? AND state IN ('running', 'starting', 'awaiting_review', 'waiting_for_lease', 'queued')
		`+visibilityClause+`
		ORDER BY CASE state WHEN 'running' THEN 0 WHEN 'starting' THEN 1 WHEN 'awaiting_review' THEN 2 WHEN 'waiting_for_lease' THEN 3 ELSE 4 END,
		         ordinal LIMIT 1`, visibilityArgs...)
	active, _, _, activeErr := scanHandsInstruction(row)
	if activeErr != nil && activeErr != sql.ErrNoRows {
		return &mcp.CallToolResult{IsError: true}, GetHandsStateOutput{}, activeErr
	}
	if activeErr == nil {
		output.ActiveInstruction = &active
		generations, err := readHandsGenerationRows(ctx, projectID, active.InstructionID)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, GetHandsStateOutput{}, err
		}
		if len(generations) > 0 {
			output.ActiveGeneration = &generations[len(generations)-1]
		}
	}
	leaseQuery := `SELECT COUNT(*) FROM project_write_lease WHERE project_id = ? AND owner_kind = 'hands_instruction' AND state = 'active'`
	leaseArgs := []any{projectID}
	if authorizedRole == "netrunner" {
		if output.ActiveInstruction == nil {
			output.ActiveLeaseCount = 0
		} else {
			leaseQuery += " AND owner_id = ?"
			leaseArgs = append(leaseArgs, output.ActiveInstruction.InstructionID)
		}
	}
	if authorizedRole != "netrunner" || output.ActiveInstruction != nil {
		if err := db.QueryRowContext(ctx, leaseQuery, leaseArgs...).Scan(&output.ActiveLeaseCount); err != nil {
			return &mcp.CallToolResult{IsError: true}, GetHandsStateOutput{}, err
		}
	}
	if output.AuthorityState != "enabled" {
		output.DerivedState = "disabled"
	} else if output.ActiveInstruction == nil {
		output.DerivedState = "idle"
	} else {
		switch output.ActiveInstruction.State {
		case "starting", "running":
			output.DerivedState = "running"
		case "awaiting_review":
			output.DerivedState = "awaiting_review"
		case "waiting_for_lease":
			output.DerivedState = "waiting_for_lease"
		default:
			output.DerivedState = "queued"
		}
	}
	return nil, output, nil
}

func ListHandsInstructions(ctx context.Context, req *mcp.CallToolRequest, input ListHandsInstructionsInput) (*mcp.CallToolResult, ListHandsInstructionsOutput, error) {
	if authorizedRole != "fixer" && authorizedRole != "overseer" && authorizedRole != "netrunner" {
		return &mcp.CallToolResult{IsError: true}, ListHandsInstructionsOutput{}, fmt.Errorf("access denied: requires fixer, netrunner, or overseer role")
	}
	projectID, err := scopedHandsProjectID(input.ProjectID)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, ListHandsInstructionsOutput{}, err
	}
	limit := input.Limit
	if limit == 0 {
		limit = 25
	}
	if limit < 1 || limit > 100 || input.AfterOrdinal < 0 {
		return &mcp.CallToolResult{IsError: true}, ListHandsInstructionsOutput{}, fmt.Errorf("limit must be 1..100 and after_ordinal cannot be negative")
	}
	rows, err := db.QueryContext(ctx, `SELECT `+handsInstructionSelectColumns+`
		FROM hands_instruction WHERE project_id = ? AND ordinal > ?
		ORDER BY ordinal LIMIT ?`, projectID, input.AfterOrdinal, limit+1)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, ListHandsInstructionsOutput{}, err
	}
	defer rows.Close()
	output := ListHandsInstructionsOutput{Instructions: []HandsInstructionSummary{}}
	for rows.Next() {
		item, _, _, err := scanHandsInstruction(rows)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, ListHandsInstructionsOutput{}, err
		}
		if len(output.Instructions) == limit {
			output.HasMore = true
			break
		}
		output.Instructions = append(output.Instructions, item)
		output.NextOrdinal = item.Ordinal
	}
	if err := rows.Err(); err != nil {
		return &mcp.CallToolResult{IsError: true}, ListHandsInstructionsOutput{}, err
	}
	return nil, output, nil
}

func GetHandsInstruction(ctx context.Context, req *mcp.CallToolRequest, input GetHandsInstructionInput) (*mcp.CallToolResult, GetHandsInstructionOutput, error) {
	projectID, err := scopedHandsProjectID(input.ProjectID)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetHandsInstructionOutput{}, err
	}
	row := db.QueryRowContext(ctx, `SELECT `+handsInstructionSelectColumns+`
		FROM hands_instruction WHERE id = ? AND project_id = ?`, strings.TrimSpace(input.InstructionID), projectID)
	instruction, compatSessionID, envelopeJSON, err := scanHandsInstruction(row)
	if err == sql.ErrNoRows {
		return &mcp.CallToolResult{IsError: true}, GetHandsInstructionOutput{}, fmt.Errorf("instruction not found in current project")
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetHandsInstructionOutput{}, err
	}
	if authorizedRole == "netrunner" && (authorizedSessionId <= 0 || compatSessionID != authorizedSessionId) {
		return &mcp.CallToolResult{IsError: true}, GetHandsInstructionOutput{}, fmt.Errorf("instruction not found in current project")
	}
	instructionEnvelope, err := decodeHandsJSONObject(envelopeJSON, "instruction_envelope")
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetHandsInstructionOutput{}, err
	}
	output := GetHandsInstructionOutput{
		Instruction: instruction, InstructionEnvelope: instructionEnvelope,
		Events: []HandsInstructionEvent{}, Generations: []HandsGeneration{},
	}
	rows, err := db.QueryContext(ctx, `
		SELECT ordinal, event_type, COALESCE(from_state, ''), COALESCE(to_state, ''),
		       actor_kind, actor_id, payload_json, created_at
		FROM hands_instruction_event WHERE instruction_id = ? ORDER BY ordinal`, instruction.InstructionID)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetHandsInstructionOutput{}, err
	}
	for rows.Next() {
		var event HandsInstructionEvent
		var payloadJSON string
		if err := rows.Scan(&event.Ordinal, &event.EventType, &event.FromState, &event.ToState,
			&event.ActorKind, &event.ActorID, &payloadJSON, &event.CreatedAt); err != nil {
			_ = rows.Close()
			return &mcp.CallToolResult{IsError: true}, GetHandsInstructionOutput{}, err
		}
		event.Payload, err = decodeHandsJSONObject(payloadJSON, "instruction event payload")
		if err != nil {
			_ = rows.Close()
			return &mcp.CallToolResult{IsError: true}, GetHandsInstructionOutput{}, err
		}
		output.Events = append(output.Events, event)
		output.LatestEventOrdinal = event.Ordinal
	}
	if err := rows.Close(); err != nil {
		return &mcp.CallToolResult{IsError: true}, GetHandsInstructionOutput{}, err
	}
	output.Generations, err = readHandsGenerationRows(ctx, projectID, instruction.InstructionID)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetHandsInstructionOutput{}, err
	}
	return nil, output, nil
}

func appendHandsInstructionEventTx(ctx context.Context, tx *sql.Tx, instructionID, eventType, fromState, toState, actorKind, actorID string, payload any) (int, error) {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	if len(payloadJSON) > maxGenUIRequestBytes {
		return 0, fmt.Errorf("instruction event payload exceeds %d bytes", maxGenUIRequestBytes)
	}
	var ordinal int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(ordinal), 0) + 1 FROM hands_instruction_event WHERE instruction_id = ?`, instructionID).Scan(&ordinal); err != nil {
		return 0, err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO hands_instruction_event (
			instruction_id, ordinal, event_type, from_state, to_state,
			actor_kind, actor_id, payload_json, created_at
		) VALUES (?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), ?, ?, ?, ?)`,
		instructionID, ordinal, eventType, fromState, toState, actorKind, actorID,
		string(payloadJSON), workroomTimestamp())
	return ordinal, err
}

func appendWorkroomAuditTx(ctx context.Context, tx *sql.Tx, projectID int, principalID, actionID, targetType, targetID, decision, outcome, causationID, correlationID string, detail any) error {
	detailJSON, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	if len(detailJSON) > maxGenUIRequestBytes {
		return fmt.Errorf("audit detail exceeds %d bytes", maxGenUIRequestBytes)
	}
	id, err := newWorkroomID()
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO workroom_audit_event (
			id, project_id, principal_id, action_id, target_type, target_id,
			decision, outcome, causation_id, correlation_id, detail_json, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, projectID, principalID, actionID, targetType, targetID, decision, outcome,
		causationID, correlationID, string(detailJSON), workroomTimestamp())
	return err
}

type SubmitHandsInstructionInput struct {
	InstructionText    string   `json:"instruction_text" jsonschema:"Immutable instruction text, at most 64 KiB."`
	DeclaredWriteScope []string `json:"declared_write_scope,omitempty" jsonschema:"Normalized project-relative paths. An empty list declares read-only work."`
	RequestedLane      string   `json:"requested_lane,omitempty" jsonschema:"Registered provider lane: codex, commandcode, claude, kimi-code, antigravity, grok, or pi. Defaults to the project lane."`
	SourceChannelKind  string   `json:"source_channel_kind,omitempty" jsonschema:"Durable registered source channel; defaults to fixer_mcp."`
	SourceChannelID    string   `json:"source_channel_id,omitempty" jsonschema:"Durable source conversation identifier; defaults to the authenticated project."`
	SourceMessageID    string   `json:"source_message_id,omitempty" jsonschema:"Optional durable source message identifier."`
	IdempotencyKey     string   `json:"idempotency_key" jsonschema:"Stable caller-generated key within the source channel."`
}

type SubmitHandsInstructionOutput struct {
	Status        string `json:"status"`
	InstructionID string `json:"instruction_id"`
	Ordinal       int    `json:"ordinal"`
	State         string `json:"state"`
	Lane          string `json:"lane"`
	RiskClass     string `json:"risk_class"`
	ProjectSeq    int64  `json:"project_seq"`
	ReasonCode    string `json:"reason_code,omitempty"`
	ReasonText    string `json:"reason_text,omitempty"`
}

func normalizedHandsWriteScope(raw []string) ([]string, string, error) {
	if len(raw) == 0 {
		return []string{}, "[]", nil
	}
	normalized, err := normalizeDeclaredWriteScope(raw)
	if err != nil {
		return nil, "", err
	}
	payload, err := json.Marshal(normalized)
	if err != nil {
		return nil, "", err
	}
	return normalized, string(payload), nil
}

func projectUsesGitTx(ctx context.Context, tx *sql.Tx, projectID int) (bool, error) {
	var cwd string
	if err := tx.QueryRowContext(ctx, `SELECT cwd FROM project WHERE id = ?`, projectID).Scan(&cwd); err != nil {
		return false, err
	}
	_, err := os.Stat(filepath.Join(cwd, ".git"))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func createHandsCompatibilitySessionTx(ctx context.Context, tx *sql.Tx, projectID int, instructionID string, ordinal int, instructionText, declaredScopeJSON, provider, model, reasoning string) (int, error) {
	taskDescription := fmt.Sprintf(
		"Permanent Hands instruction %d (%s).\n\n%s\n\nExecution envelope: provider=%s model=%s reasoning=%s. The instruction, not this compatibility session, is the accountability unit.",
		ordinal, instructionID, instructionText, provider, model, reasoning,
	)
	result, err := tx.ExecContext(ctx, `
		INSERT INTO session (
			project_id, task_description, status, cli_backend, cli_model, cli_reasoning,
			declared_write_scope, session_kind, created_at, updated_at
		) VALUES (?, ?, 'pending', ?, ?, ?, ?, 'hands_instruction', ?, ?)`,
		projectID, taskDescription, provider, model, reasoning, declaredScopeJSON,
		workroomTimestamp(), workroomTimestamp())
	if err != nil {
		return 0, err
	}
	globalSessionID, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO legacy_manual_session_link (
			project_id, session_id, disposition, evidence_json, hands_instruction_id,
			classified_at, classified_by
		) VALUES (?, ?, 'live', ?, ?, ?, 'hands_dispatch')`,
		projectID, globalSessionID,
		`{"classification":"post_cutover_compatibility_projection"}`,
		instructionID, workroomTimestamp()); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO session_mcp_server (session_id, mcp_server_id)
		SELECT ?, mcp_server_id FROM project_hands_mcp_server WHERE project_id = ?`, globalSessionID, projectID); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO session_mcp_server (session_id, mcp_server_id)
		SELECT ?, mcp_server_id FROM project_mcp_server WHERE project_id = ?`, globalSessionID, projectID); err != nil {
		return 0, err
	}
	return int(globalSessionID), nil
}

func SubmitHandsInstruction(ctx context.Context, req *mcp.CallToolRequest, input SubmitHandsInstructionInput) (*mcp.CallToolResult, SubmitHandsInstructionOutput, error) {
	if authorizedRole != "fixer" && authorizedRole != "netrunner" {
		return &mcp.CallToolResult{IsError: true}, SubmitHandsInstructionOutput{}, fmt.Errorf("access denied: requires fixer or netrunner role")
	}
	input.InstructionText = strings.TrimSpace(input.InstructionText)
	if input.InstructionText == "" || len(input.InstructionText) > handsInstructionMaxBytes || !utf8.ValidString(input.InstructionText) {
		return &mcp.CallToolResult{IsError: true}, SubmitHandsInstructionOutput{}, fmt.Errorf("instruction_text must contain 1..%d valid UTF-8 bytes", handsInstructionMaxBytes)
	}
	idempotencyKey, err := validateIdempotencyKey(input.IdempotencyKey)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SubmitHandsInstructionOutput{}, err
	}
	writeScope, writeScopeJSON, err := normalizedHandsWriteScope(input.DeclaredWriteScope)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SubmitHandsInstructionOutput{}, err
	}
	input.DeclaredWriteScope = writeScope
	input.RequestedLane = strings.ToLower(strings.TrimSpace(input.RequestedLane))
	if input.RequestedLane != "" {
		if _, ok := handsProviders[input.RequestedLane]; !ok {
			return &mcp.CallToolResult{IsError: true}, SubmitHandsInstructionOutput{}, fmt.Errorf("requested_lane is not registered")
		}
	}
	input.SourceChannelKind = strings.TrimSpace(input.SourceChannelKind)
	if input.SourceChannelKind == "" {
		input.SourceChannelKind = "fixer_mcp"
	}
	if !workroomContainsString([]string{"fixer_mcp", "serverpod", "dashboard", "fixer_thread"}, input.SourceChannelKind) {
		return &mcp.CallToolResult{IsError: true}, SubmitHandsInstructionOutput{}, fmt.Errorf("source_channel_kind is not registered")
	}
	input.SourceChannelID = strings.TrimSpace(input.SourceChannelID)
	if input.SourceChannelID == "" {
		input.SourceChannelID = fmt.Sprintf("project:%d", authorizedProjectId)
	}
	if len(input.SourceChannelID) > 512 || len(input.SourceMessageID) > 512 {
		return &mcp.CallToolResult{IsError: true}, SubmitHandsInstructionOutput{}, fmt.Errorf("source channel identifiers must not exceed 512 bytes")
	}
	requestHash, _, err := hashCanonicalJSON(input)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SubmitHandsInstructionOutput{}, err
	}
	principalID := workroomPrincipalID()
	causationID, err := newWorkroomID()
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SubmitHandsInstructionOutput{}, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SubmitHandsInstructionOutput{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var output SubmitHandsInstructionOutput
	if replayed, err := loadCommandDedupTx(ctx, tx, authorizedProjectId, principalID, "hands.instruction.submit", idempotencyKey, requestHash, &output); err != nil {
		return &mcp.CallToolResult{IsError: true}, SubmitHandsInstructionOutput{}, err
	} else if replayed {
		return nil, output, nil
	}

	var actorID, authorityState, defaultLane string
	var ordinal int
	if err := tx.QueryRowContext(ctx, `
		UPDATE project_hands SET next_instruction_ordinal = MAX(
			next_instruction_ordinal,
			COALESCE((SELECT MAX(ordinal) + 1 FROM hands_instruction WHERE project_id = project_hands.project_id), 1)
		) + 1, updated_at = ?
		WHERE project_id = ?
		RETURNING actor_id, authority_state, default_lane, next_instruction_ordinal - 1`,
		workroomTimestamp(), authorizedProjectId).Scan(&actorID, &authorityState, &defaultLane, &ordinal); err == sql.ErrNoRows {
		return &mcp.CallToolResult{IsError: true}, SubmitHandsInstructionOutput{}, fmt.Errorf("Hands identity not found for current project")
	} else if err != nil {
		return &mcp.CallToolResult{IsError: true}, SubmitHandsInstructionOutput{}, err
	}
	requestedLane := input.RequestedLane
	if requestedLane == "" {
		requestedLane = defaultLane
	}
	model, reasoning, ok := handsProviderConfig(requestedLane)
	if !ok {
		return &mcp.CallToolResult{IsError: true}, SubmitHandsInstructionOutput{}, fmt.Errorf("unsupported Hands provider %q", requestedLane)
	}
	riskClass := "read_only"
	reviewPolicy := "auto_read_only"
	if len(writeScope) > 0 {
		riskClass = "repository_write"
		reviewPolicy = "fixer_required"
	}
	state := "queued"
	reasonCode, reasonText := "", ""
	if authorityState != "enabled" {
		state, reasonCode, reasonText = "unsupported", "hands_authority_unavailable", "The permanent Hands authority is not enabled for this project."
	} else if len(writeScope) > 0 {
		usesGit, err := projectUsesGitTx(ctx, tx, authorizedProjectId)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, SubmitHandsInstructionOutput{}, err
		}
		if !usesGit {
			state, riskClass = "unsupported", "unsupported_high_risk"
			reasonCode, reasonText = "non_git_repository_write", "Repository-write Hands instructions require a Git project so execution can be isolated and reviewed."
		}
	}
	instructionID, err := newWorkroomID()
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SubmitHandsInstructionOutput{}, err
	}
	now := workroomTimestamp()
	envelope := map[string]any{
		"protocol": "fixer.hands.instruction", "protocol_version": 1,
		"instruction_id": instructionID, "project_id": authorizedProjectId, "actor_id": actorID,
		"ordinal": ordinal, "instruction_text": input.InstructionText, "declared_write_scope": writeScope,
		"requested_lane": requestedLane, "risk_class": riskClass, "review_policy": reviewPolicy,
		"source":              map[string]string{"channel_kind": input.SourceChannelKind, "channel_id": input.SourceChannelID, "message_id": input.SourceMessageID},
		"issuer_principal_id": principalID, "created_at": now,
	}
	_, envelopeJSON, err := hashCanonicalJSON(envelope)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SubmitHandsInstructionOutput{}, err
	}
	var terminalAt any
	if state == "unsupported" {
		terminalAt = now
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO hands_instruction (
			id, project_id, actor_id, ordinal, source_channel_kind, source_channel_id,
			source_message_id, issuer_principal_id, instruction_text, declared_write_scope_json,
			instruction_envelope_json, requested_lane, risk_class, review_policy, state,
			state_reason_code, state_reason_text, idempotency_key, revision,
			created_at, updated_at, terminal_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), ?, 1, ?, ?, ?)`,
		instructionID, authorizedProjectId, actorID, ordinal, input.SourceChannelKind,
		input.SourceChannelID, input.SourceMessageID, principalID, input.InstructionText,
		writeScopeJSON, string(envelopeJSON), requestedLane, riskClass, reviewPolicy, state,
		reasonCode, reasonText, idempotencyKey, now, now, terminalAt); err != nil {
		return &mcp.CallToolResult{IsError: true}, SubmitHandsInstructionOutput{}, err
	}
	compatSessionID := 0
	if state == "queued" {
		compatSessionID, err = createHandsCompatibilitySessionTx(
			ctx, tx, authorizedProjectId, instructionID, ordinal, input.InstructionText,
			writeScopeJSON, requestedLane, model, reasoning,
		)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, SubmitHandsInstructionOutput{}, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE hands_instruction SET compat_session_id = ? WHERE id = ?`, compatSessionID, instructionID); err != nil {
			return &mcp.CallToolResult{IsError: true}, SubmitHandsInstructionOutput{}, err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO hands_generation (
				instruction_id, generation, project_id, compat_session_id, provider, model,
				reasoning, status, launch_mode, created_at, updated_at
			) VALUES (?, 1, ?, ?, ?, ?, ?, 'planned', 'headless', ?, ?)`,
			instructionID, authorizedProjectId, compatSessionID, requestedLane, model, reasoning, now, now); err != nil {
			return &mcp.CallToolResult{IsError: true}, SubmitHandsInstructionOutput{}, err
		}
	}
	if _, err := appendHandsInstructionEventTx(ctx, tx, instructionID, "instruction.submitted", "", state, "fixer", principalID,
		map[string]any{"state": state, "lane": requestedLane, "risk_class": riskClass, "reason_code": reasonCode}); err != nil {
		return &mcp.CallToolResult{IsError: true}, SubmitHandsInstructionOutput{}, err
	}
	event, err := appendProjectUIEventTx(ctx, tx, authorizedProjectId, workroomEventInput{
		Kind: "hands.instruction.created", AggregateType: "hands_instruction", AggregateID: instructionID,
		AggregateRevision: 1,
		Payload:           map[string]any{"instruction_id": instructionID, "ordinal": ordinal, "state": state, "lane": requestedLane, "risk_class": riskClass, "review_policy": reviewPolicy, "reason_code": reasonCode, "updated_at": now},
		ActorKind:         "fixer", ActorID: principalID, CausationID: causationID, CorrelationID: causationID,
	})
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SubmitHandsInstructionOutput{}, err
	}
	if state == "queued" {
		event, err = appendProjectUIEventTx(ctx, tx, authorizedProjectId, workroomEventInput{
			Kind: "hands.generation.changed", AggregateType: "hands_generation", AggregateID: instructionID + ":1",
			AggregateRevision: 1,
			Payload:           map[string]any{"instruction_id": instructionID, "generation": 1, "provider": requestedLane, "model": model, "reasoning": reasoning, "status": "planned", "launch_mode": "headless"},
			ActorKind:         "system", ActorID: "hands-dispatch", CausationID: causationID, CorrelationID: causationID,
		})
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, SubmitHandsInstructionOutput{}, err
		}
	}
	if err := appendWorkroomAuditTx(ctx, tx, authorizedProjectId, principalID, "hands.instruction.submit", "hands_instruction", instructionID, "authorized", state, causationID, causationID,
		map[string]any{"ordinal": ordinal, "lane": requestedLane, "risk_class": riskClass, "reason_code": reasonCode}); err != nil {
		return &mcp.CallToolResult{IsError: true}, SubmitHandsInstructionOutput{}, err
	}
	output = SubmitHandsInstructionOutput{
		Status: "accepted", InstructionID: instructionID, Ordinal: ordinal, State: state,
		Lane: requestedLane, RiskClass: riskClass, ProjectSeq: event.Seq,
		ReasonCode: reasonCode, ReasonText: reasonText,
	}
	if err := storeCommandDedupTx(ctx, tx, authorizedProjectId, principalID, "hands.instruction.submit", idempotencyKey, requestHash, output); err != nil {
		return &mcp.CallToolResult{IsError: true}, SubmitHandsInstructionOutput{}, err
	}
	if err := tx.Commit(); err != nil {
		return &mcp.CallToolResult{IsError: true}, SubmitHandsInstructionOutput{}, err
	}
	notifyHandsWaiters(authorizedProjectId)
	return nil, output, nil
}

func handsInstructionForCompatibilitySession(ctx context.Context, globalSessionID, projectID int) (string, bool, error) {
	// Legacy unit fixtures and pre-migration databases may not yet have the
	// additive Workroom projection. In that case the ordinary Netrunner
	// lifecycle remains authoritative until initDB applies the migration.
	if !dbTableHasColumn("hands_instruction", "compat_session_id") || !dbTableHasColumn("session", "session_kind") {
		return "", false, nil
	}
	var instructionID string
	err := db.QueryRowContext(ctx, `
		SELECT instruction.id
		FROM hands_instruction instruction
		JOIN session compat ON compat.id = instruction.compat_session_id
		WHERE compat.id = ? AND compat.project_id = ? AND compat.session_kind = 'hands_instruction'`,
		globalSessionID, projectID).Scan(&instructionID)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	return instructionID, err == nil, err
}

func findHandsLeaseConflictTx(ctx context.Context, tx *sql.Tx, projectID int, writeScope []string) (string, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT owner_kind, owner_id, scope_path
		FROM project_write_lease
		WHERE project_id = ? AND state = 'active'
		ORDER BY owner_kind, owner_id, scope_path`, projectID)
	if err != nil {
		return "", err
	}
	for rows.Next() {
		var ownerKind, ownerID, scopePath string
		if err := rows.Scan(&ownerKind, &ownerID, &scopePath); err != nil {
			_ = rows.Close()
			return "", err
		}
		for _, requested := range writeScope {
			if writeScopePathsOverlap(requested, scopePath) {
				_ = rows.Close()
				return fmt.Sprintf("scope %q overlaps active %s %s scope %q", requested, ownerKind, ownerID, scopePath), nil
			}
		}
	}
	if err := rows.Close(); err != nil {
		return "", err
	}
	legacyRows, err := tx.QueryContext(ctx, `
		SELECT wave_id, scope_path FROM parallel_wave_scope_lease
		WHERE project_id = ? AND active = 1 ORDER BY wave_id, scope_path`, projectID)
	if err != nil {
		return "", err
	}
	defer legacyRows.Close()
	for legacyRows.Next() {
		var waveID int
		var scopePath string
		if err := legacyRows.Scan(&waveID, &scopePath); err != nil {
			return "", err
		}
		for _, requested := range writeScope {
			if writeScopePathsOverlap(requested, scopePath) {
				return fmt.Sprintf("scope %q overlaps active wave %d scope %q", requested, waveID, scopePath), nil
			}
		}
	}
	return "", legacyRows.Err()
}

func nextHandsFencingTokenTx(ctx context.Context, tx *sql.Tx, projectID int) (int64, error) {
	var token int64
	err := tx.QueryRowContext(ctx, `
		INSERT INTO project_write_fence (project_id, next_token, updated_at)
		VALUES (?, 2, ?)
		ON CONFLICT(project_id) DO UPDATE SET
			next_token = project_write_fence.next_token + 1,
			updated_at = excluded.updated_at
		RETURNING next_token - 1`, projectID, workroomTimestamp()).Scan(&token)
	return token, err
}

func currentHandsBinaryEpochTx(ctx context.Context, tx *sql.Tx, projectID int) (int, error) {
	var epoch int
	err := tx.QueryRowContext(ctx, `SELECT COALESCE(running_build_epoch, 0) FROM mcp_binary_state WHERE project_id = ?`, projectID).Scan(&epoch)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return epoch, err
}

func transitionHandsToWaitingForLeaseTx(ctx context.Context, tx *sql.Tx, projectID int, instructionID, fromState, reason, actorID, causationID string, revision int) (int64, error) {
	if fromState == "waiting_for_lease" {
		var head int64
		err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq), 0) FROM project_ui_event WHERE project_id = ?`, projectID).Scan(&head)
		return head, err
	}
	now := workroomTimestamp()
	revision++
	if _, err := tx.ExecContext(ctx, `
		UPDATE hands_instruction
		SET state = 'waiting_for_lease', state_reason_code = 'write_lease_unavailable',
		    state_reason_text = ?, revision = ?, updated_at = ?
		WHERE id = ? AND project_id = ?`, reason, revision, now, instructionID, projectID); err != nil {
		return 0, err
	}
	if _, err := appendHandsInstructionEventTx(ctx, tx, instructionID, "instruction.waiting_for_lease", fromState, "waiting_for_lease", "system", actorID,
		map[string]any{"reason_code": "write_lease_unavailable", "reason_text": reason}); err != nil {
		return 0, err
	}
	event, err := appendProjectUIEventTx(ctx, tx, projectID, workroomEventInput{
		Kind: "hands.instruction.changed", AggregateType: "hands_instruction", AggregateID: instructionID,
		AggregateRevision: revision,
		Payload:           map[string]any{"instruction_id": instructionID, "state": "waiting_for_lease", "revision": revision, "reason_code": "write_lease_unavailable", "reason_text": reason, "updated_at": now},
		ActorKind:         "system", ActorID: actorID, CausationID: causationID, CorrelationID: causationID,
	})
	return event.Seq, err
}

func checkoutHandsCompatibilitySession(ctx context.Context, globalSessionID, projectID int) error {
	principalID := workroomPrincipalID()
	causationID, err := newWorkroomID()
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var instructionID, state, riskClass, writeScopeJSON, sessionStatus string
	var revision int
	err = tx.QueryRowContext(ctx, `
		SELECT instruction.id, instruction.state, instruction.risk_class,
		       instruction.declared_write_scope_json, instruction.revision, compat.status
		FROM hands_instruction instruction
		JOIN session compat ON compat.id = instruction.compat_session_id
		WHERE instruction.project_id = ? AND compat.id = ? AND compat.session_kind = 'hands_instruction'`,
		projectID, globalSessionID).Scan(&instructionID, &state, &riskClass, &writeScopeJSON, &revision, &sessionStatus)
	if err != nil {
		return err
	}
	if sessionStatus == "in_progress" && state == "running" {
		return nil
	}
	if !workroomContainsString([]string{"queued", "waiting_for_lease"}, state) || sessionStatus != "pending" {
		return fmt.Errorf("Hands instruction cannot start from instruction=%s session=%s", state, sessionStatus)
	}
	var activeOtherCount int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM hands_instruction
		WHERE project_id = ? AND id <> ? AND state IN ('starting', 'running')`, projectID, instructionID).Scan(&activeOtherCount); err != nil {
		return err
	}
	var writeScope []string
	if err := json.Unmarshal([]byte(writeScopeJSON), &writeScope); err != nil {
		return err
	}
	conflictReason := ""
	if activeOtherCount > 0 {
		conflictReason = "another Hands generation is starting or running"
	} else if riskClass == "repository_write" {
		conflictReason, err = findHandsLeaseConflictTx(ctx, tx, projectID, writeScope)
		if err != nil {
			return err
		}
	}
	if conflictReason != "" {
		if _, err := transitionHandsToWaitingForLeaseTx(ctx, tx, projectID, instructionID, state, conflictReason, "hands-dispatch", causationID, revision); err != nil {
			return err
		}
		if err := appendWorkroomAuditTx(ctx, tx, projectID, principalID, "hands.instruction.start", "hands_instruction", instructionID, "denied", "waiting_for_lease", causationID, causationID,
			map[string]any{"reason_code": "write_lease_unavailable", "reason_text": conflictReason}); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		notifyHandsWaiters(projectID)
		return fmt.Errorf("hands_instruction_waiting_for_lease: %s", conflictReason)
	}

	leaseSetID := ""
	var fencingToken int64
	binaryEpoch := 0
	providerProcessID, providerProcessStartIdentity := currentHandsProviderProcessIdentity()
	if providerProcessID <= 0 || providerProcessStartIdentity == "" {
		return fmt.Errorf("provider_process_identity_unavailable: cannot establish immutable parent process identity")
	}
	if riskClass == "repository_write" {
		leaseSetID, err = newWorkroomID()
		if err != nil {
			return err
		}
		fencingToken, err = nextHandsFencingTokenTx(ctx, tx, projectID)
		if err != nil {
			return err
		}
		binaryEpoch, err = currentHandsBinaryEpochTx(ctx, tx, projectID)
		if err != nil {
			return err
		}
		now := workroomTimestamp()
		for _, scopePath := range writeScope {
			leaseID, err := newWorkroomID()
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO project_write_lease (
					id, project_id, lease_set_id, owner_kind, owner_id, scope_path,
					fencing_token, state, process_id, process_start_identity,
					binary_build_id, binary_epoch, created_at, heartbeat_at
				) VALUES (?, ?, ?, 'hands_instruction', ?, ?, ?, 'active', ?, ?, ?, ?, ?, ?)`,
				leaseID, projectID, leaseSetID, instructionID, scopePath, fencingToken,
				providerProcessID, providerProcessStartIdentity,
				mcpRunningBuildID, binaryEpoch, now, now); err != nil {
				return err
			}
		}
		if _, err := appendProjectUIEventTx(ctx, tx, projectID, workroomEventInput{
			Kind: "lease.changed", AggregateType: "project_write_lease", AggregateID: leaseSetID,
			AggregateRevision: int(fencingToken),
			Payload:           map[string]any{"lease_set_id": leaseSetID, "owner_kind": "hands_instruction", "owner_id": instructionID, "scope": writeScope, "fencing_token": fencingToken, "state": "active"},
			ActorKind:         "system", ActorID: "hands-lease-authority", CausationID: causationID, CorrelationID: causationID,
		}); err != nil {
			return err
		}
	}
	var generation int
	if err := tx.QueryRowContext(ctx, `SELECT MAX(generation) FROM hands_generation WHERE instruction_id = ?`, instructionID).Scan(&generation); err != nil {
		return err
	}
	now := workroomTimestamp()
	revision++
	if _, err := tx.ExecContext(ctx, `
		UPDATE hands_instruction
		SET state = 'starting', state_reason_code = NULL, state_reason_text = NULL,
		    revision = ?, updated_at = ? WHERE id = ?`, revision, now, instructionID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE hands_generation
		SET status = 'starting', process_id = ?, process_start_identity = ?,
		    binary_build_id = ?, binary_epoch = ?, lease_set_id = NULLIF(?, ''),
		    fencing_token = NULLIF(?, 0), started_at = ?, heartbeat_at = ?, updated_at = ?
		WHERE instruction_id = ? AND generation = ? AND status = 'planned'`,
		providerProcessID, providerProcessStartIdentity, mcpRunningBuildID, binaryEpoch,
		leaseSetID, fencingToken, now, now, now, instructionID, generation); err != nil {
		return err
	}
	if _, err := appendHandsInstructionEventTx(ctx, tx, instructionID, "generation.starting", state, "starting", "hands", "hands:"+instructionID,
		map[string]any{"generation": generation, "lease_set_id": leaseSetID, "fencing_token": fencingToken}); err != nil {
		return err
	}
	if _, err := appendProjectUIEventTx(ctx, tx, projectID, workroomEventInput{
		Kind: "hands.instruction.changed", AggregateType: "hands_instruction", AggregateID: instructionID,
		AggregateRevision: revision,
		Payload:           map[string]any{"instruction_id": instructionID, "state": "starting", "revision": revision, "generation": generation, "updated_at": now},
		ActorKind:         "hands", ActorID: "hands:" + instructionID, CausationID: causationID, CorrelationID: causationID,
	}); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE session SET status = 'in_progress', updated_at = ? WHERE id = ? AND project_id = ? AND status = 'pending'`, now, globalSessionID, projectID); err != nil {
		return err
	}
	revision++
	if _, err := tx.ExecContext(ctx, `UPDATE hands_instruction SET state = 'running', revision = ?, updated_at = ? WHERE id = ?`, revision, now, instructionID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE hands_generation SET status = 'running', heartbeat_at = ?, updated_at = ? WHERE instruction_id = ? AND generation = ?`, now, now, instructionID, generation); err != nil {
		return err
	}
	if _, err := appendHandsInstructionEventTx(ctx, tx, instructionID, "generation.running", "starting", "running", "hands", "hands:"+instructionID,
		map[string]any{"generation": generation, "lease_set_id": leaseSetID, "fencing_token": fencingToken}); err != nil {
		return err
	}
	if _, err := appendProjectUIEventTx(ctx, tx, projectID, workroomEventInput{
		Kind: "hands.instruction.changed", AggregateType: "hands_instruction", AggregateID: instructionID,
		AggregateRevision: revision,
		Payload:           map[string]any{"instruction_id": instructionID, "state": "running", "revision": revision, "generation": generation, "updated_at": now},
		ActorKind:         "hands", ActorID: "hands:" + instructionID, CausationID: causationID, CorrelationID: causationID,
	}); err != nil {
		return err
	}
	if _, err := appendProjectUIEventTx(ctx, tx, projectID, workroomEventInput{
		Kind: "hands.generation.changed", AggregateType: "hands_generation", AggregateID: fmt.Sprintf("%s:%d", instructionID, generation),
		AggregateRevision: 2,
		Payload:           map[string]any{"instruction_id": instructionID, "generation": generation, "status": "running", "lease_set_id": leaseSetID, "fencing_token": fencingToken, "started_at": now},
		ActorKind:         "hands", ActorID: "hands:" + instructionID, CausationID: causationID, CorrelationID: causationID,
	}); err != nil {
		return err
	}
	if err := appendWorkroomAuditTx(ctx, tx, projectID, principalID, "hands.instruction.start", "hands_instruction", instructionID, "authorized", "running", causationID, causationID,
		map[string]any{"generation": generation, "lease_set_id": leaseSetID, "fencing_token": fencingToken}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	notifyHandsWaiters(projectID)
	return nil
}

func releaseHandsLeasesTx(ctx context.Context, tx *sql.Tx, projectID int, instructionID, releaseReason, actorID, causationID string) (int64, error) {
	var leaseSetID string
	var token int
	err := tx.QueryRowContext(ctx, `
		SELECT lease_set_id, MAX(fencing_token)
		FROM project_write_lease
		WHERE project_id = ? AND owner_kind = 'hands_instruction' AND owner_id = ? AND state = 'active'
		GROUP BY lease_set_id LIMIT 1`, projectID, instructionID).Scan(&leaseSetID, &token)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	now := workroomTimestamp()
	if _, err := tx.ExecContext(ctx, `
		UPDATE project_write_lease
		SET state = 'released', released_at = ?, release_reason = ?, heartbeat_at = ?
		WHERE project_id = ? AND owner_kind = 'hands_instruction' AND owner_id = ? AND state = 'active'`,
		now, releaseReason, now, projectID, instructionID); err != nil {
		return 0, err
	}
	event, err := appendProjectUIEventTx(ctx, tx, projectID, workroomEventInput{
		Kind: "lease.changed", AggregateType: "project_write_lease", AggregateID: leaseSetID,
		AggregateRevision: token + 1,
		Payload:           map[string]any{"lease_set_id": leaseSetID, "owner_kind": "hands_instruction", "owner_id": instructionID, "fencing_token": token, "state": "released", "release_reason": releaseReason},
		ActorKind:         "system", ActorID: actorID, CausationID: causationID, CorrelationID: causationID,
	})
	return event.Seq, err
}

func completeHandsCompatibilitySession(ctx context.Context, globalSessionID, projectID int, report SessionFinalReport, normalizedReport string) (bool, error) {
	instructionID, handled, err := handsInstructionForCompatibilitySession(ctx, globalSessionID, projectID)
	if err != nil || !handled {
		return handled, err
	}
	causationID, err := newWorkroomID()
	if err != nil {
		return true, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return true, err
	}
	defer func() { _ = tx.Rollback() }()
	var state, riskClass string
	var revision, generation int
	if err := tx.QueryRowContext(ctx, `
		SELECT instruction.state, instruction.risk_class, instruction.revision,
		       COALESCE(MAX(generation.generation), 0)
		FROM hands_instruction instruction
		LEFT JOIN hands_generation generation ON generation.instruction_id = instruction.id
		WHERE instruction.id = ? AND instruction.project_id = ?
		GROUP BY instruction.id`, instructionID, projectID).Scan(&state, &riskClass, &revision, &generation); err != nil {
		return true, err
	}
	if state == "completed" || state == "awaiting_review" {
		return true, nil
	}
	if state != "running" {
		return true, fmt.Errorf("Hands instruction cannot complete from state %s", state)
	}
	targetState := "completed"
	targetSessionState := "completed"
	if riskClass == "repository_write" {
		targetState = "awaiting_review"
		targetSessionState = "review"
	}
	now := workroomTimestamp()
	resultEnvelope := map[string]any{
		"protocol": "fixer.hands.result", "protocol_version": 1, "instruction_id": instructionID,
		"generation": generation, "final_report": report, "completed_at": now,
	}
	resultJSON, err := json.Marshal(resultEnvelope)
	if err != nil {
		return true, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE hands_generation
		SET status = 'stopped', result_envelope_json = ?, ended_at = ?, heartbeat_at = ?,
		    exit_code = 0, stop_reason = 'report_submitted', updated_at = ?
		WHERE instruction_id = ? AND generation = ? AND status IN ('starting', 'running')`,
		string(resultJSON), now, now, now, instructionID, generation); err != nil {
		return true, err
	}
	revision++
	var terminalAt any
	if targetState == "completed" {
		terminalAt = now
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE hands_instruction
		SET state = ?, state_reason_code = NULL, state_reason_text = NULL,
		    revision = ?, updated_at = ?, terminal_at = ?
		WHERE id = ? AND project_id = ?`, targetState, revision, now, terminalAt, instructionID, projectID); err != nil {
		return true, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE session SET status = ?, report = ?, updated_at = ?
		WHERE id = ? AND project_id = ?`, targetSessionState, normalizedReport, now, globalSessionID, projectID); err != nil {
		return true, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE legacy_manual_session_link SET disposition = 'terminal', classified_at = ?, classified_by = 'hands_completion'
		WHERE project_id = ? AND session_id = ?`, now, projectID, globalSessionID); err != nil {
		return true, err
	}
	if _, err := releaseHandsLeasesTx(ctx, tx, projectID, instructionID, "generation_reported", "hands-lease-authority", causationID); err != nil {
		return true, err
	}
	if _, err := appendHandsInstructionEventTx(ctx, tx, instructionID, "generation.reported", state, targetState, "hands", "hands:"+instructionID,
		map[string]any{"generation": generation, "target_state": targetState, "result_envelope": resultEnvelope}); err != nil {
		return true, err
	}
	if _, err := appendProjectUIEventTx(ctx, tx, projectID, workroomEventInput{
		Kind: "hands.generation.changed", AggregateType: "hands_generation", AggregateID: fmt.Sprintf("%s:%d", instructionID, generation),
		AggregateRevision: 3,
		Payload:           map[string]any{"instruction_id": instructionID, "generation": generation, "status": "stopped", "result_envelope": resultEnvelope, "ended_at": now},
		ActorKind:         "hands", ActorID: "hands:" + instructionID, CausationID: causationID, CorrelationID: causationID,
	}); err != nil {
		return true, err
	}
	if _, err := appendProjectUIEventTx(ctx, tx, projectID, workroomEventInput{
		Kind: "hands.instruction.changed", AggregateType: "hands_instruction", AggregateID: instructionID,
		AggregateRevision: revision,
		Payload:           map[string]any{"instruction_id": instructionID, "state": targetState, "revision": revision, "generation": generation, "updated_at": now},
		ActorKind:         "hands", ActorID: "hands:" + instructionID, CausationID: causationID, CorrelationID: causationID,
	}); err != nil {
		return true, err
	}
	if err := appendWorkroomAuditTx(ctx, tx, projectID, "hands:"+instructionID, "hands.generation.report", "hands_instruction", instructionID, "authorized", targetState, causationID, causationID,
		map[string]any{"generation": generation, "files_changed": report.FilesChanged, "checks_run": report.ChecksRun}); err != nil {
		return true, err
	}
	if err := tx.Commit(); err != nil {
		return true, err
	}
	notifyHandsWaiters(projectID)
	return true, nil
}

type CancelHandsInstructionInput struct {
	InstructionID  string `json:"instruction_id" jsonschema:"Project-owned Hands instruction UUID."`
	Reason         string `json:"reason,omitempty" jsonschema:"Optional cancellation reason of at most 1000 characters."`
	IdempotencyKey string `json:"idempotency_key" jsonschema:"Stable caller-generated idempotency key."`
}

type HandsCommandOutput struct {
	Status        string `json:"status"`
	InstructionID string `json:"instruction_id"`
	State         string `json:"state"`
	Revision      int    `json:"revision"`
	ProjectSeq    int64  `json:"project_seq"`
}

func CancelHandsInstruction(ctx context.Context, req *mcp.CallToolRequest, input CancelHandsInstructionInput) (*mcp.CallToolResult, HandsCommandOutput, error) {
	if authorizedRole != "fixer" {
		return &mcp.CallToolResult{IsError: true}, HandsCommandOutput{}, fmt.Errorf("access denied: requires fixer role")
	}
	input.InstructionID = strings.TrimSpace(input.InstructionID)
	input.Reason = strings.TrimSpace(input.Reason)
	if input.InstructionID == "" || utf8.RuneCountInString(input.Reason) > 1000 {
		return &mcp.CallToolResult{IsError: true}, HandsCommandOutput{}, fmt.Errorf("instruction_id is required and reason must not exceed 1000 characters")
	}
	idempotencyKey, err := validateIdempotencyKey(input.IdempotencyKey)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, HandsCommandOutput{}, err
	}
	requestHash, _, err := hashCanonicalJSON(input)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, HandsCommandOutput{}, err
	}
	principalID := workroomPrincipalID()
	causationID, err := newWorkroomID()
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, HandsCommandOutput{}, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, HandsCommandOutput{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var output HandsCommandOutput
	if replayed, err := loadCommandDedupTx(ctx, tx, authorizedProjectId, principalID, "hands.instruction.cancel", idempotencyKey, requestHash, &output); err != nil {
		return &mcp.CallToolResult{IsError: true}, HandsCommandOutput{}, err
	} else if replayed {
		return nil, output, nil
	}
	var state string
	var revision, compatSessionID, generation int
	err = tx.QueryRowContext(ctx, `
		SELECT instruction.state, instruction.revision, COALESCE(instruction.compat_session_id, 0),
		       COALESCE(MAX(generation.generation), 0)
		FROM hands_instruction instruction
		LEFT JOIN hands_generation generation ON generation.instruction_id = instruction.id
		WHERE instruction.id = ? AND instruction.project_id = ?
		GROUP BY instruction.id`, input.InstructionID, authorizedProjectId).Scan(&state, &revision, &compatSessionID, &generation)
	if err == sql.ErrNoRows {
		return &mcp.CallToolResult{IsError: true}, HandsCommandOutput{}, fmt.Errorf("instruction not found in current project")
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, HandsCommandOutput{}, err
	}
	if _, terminal := terminalHandsInstructionStates[state]; terminal {
		output = HandsCommandOutput{Status: "already_terminal", InstructionID: input.InstructionID, State: state, Revision: revision}
		if err := storeCommandDedupTx(ctx, tx, authorizedProjectId, principalID, "hands.instruction.cancel", idempotencyKey, requestHash, output); err != nil {
			return &mcp.CallToolResult{IsError: true}, HandsCommandOutput{}, err
		}
		if err := tx.Commit(); err != nil {
			return &mcp.CallToolResult{IsError: true}, HandsCommandOutput{}, err
		}
		return nil, output, nil
	}
	if !workroomContainsString([]string{"queued", "waiting_for_lease", "starting", "running"}, state) {
		return &mcp.CallToolResult{IsError: true}, HandsCommandOutput{}, fmt.Errorf("instruction cannot be cancelled from state %s", state)
	}
	now := workroomTimestamp()
	revision++
	if _, err := tx.ExecContext(ctx, `
		UPDATE hands_instruction
		SET state = 'cancelled', state_reason_code = 'cancelled_by_fixer',
		    state_reason_text = NULLIF(?, ''), revision = ?, updated_at = ?, terminal_at = ?
		WHERE id = ? AND project_id = ?`, input.Reason, revision, now, now, input.InstructionID, authorizedProjectId); err != nil {
		return &mcp.CallToolResult{IsError: true}, HandsCommandOutput{}, err
	}
	resultEnvelope := map[string]any{"protocol": "fixer.hands.result", "protocol_version": 1, "instruction_id": input.InstructionID, "generation": generation, "outcome": "cancelled", "reason": input.Reason, "completed_at": now}
	resultJSON, _ := json.Marshal(resultEnvelope)
	if generation > 0 {
		if _, err := tx.ExecContext(ctx, `
			UPDATE hands_generation SET status = 'stopped', result_envelope_json = ?, ended_at = ?,
			    stop_reason = 'cancelled_by_fixer', updated_at = ?
			WHERE instruction_id = ? AND generation = ? AND status IN ('planned', 'starting', 'running')`,
			string(resultJSON), now, now, input.InstructionID, generation); err != nil {
			return &mcp.CallToolResult{IsError: true}, HandsCommandOutput{}, err
		}
	}
	if compatSessionID > 0 {
		if _, err := tx.ExecContext(ctx, `UPDATE session SET status = 'completed', updated_at = ? WHERE id = ? AND project_id = ?`, now, compatSessionID, authorizedProjectId); err != nil {
			return &mcp.CallToolResult{IsError: true}, HandsCommandOutput{}, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE legacy_manual_session_link SET disposition = 'terminal', classified_at = ?, classified_by = 'hands_cancel' WHERE project_id = ? AND session_id = ?`, now, authorizedProjectId, compatSessionID); err != nil {
			return &mcp.CallToolResult{IsError: true}, HandsCommandOutput{}, err
		}
	}
	if _, err := releaseHandsLeasesTx(ctx, tx, authorizedProjectId, input.InstructionID, "cancelled_by_fixer", "hands-lease-authority", causationID); err != nil {
		return &mcp.CallToolResult{IsError: true}, HandsCommandOutput{}, err
	}
	if _, err := appendHandsInstructionEventTx(ctx, tx, input.InstructionID, "instruction.cancelled", state, "cancelled", "fixer", principalID,
		map[string]any{"reason": input.Reason, "generation": generation}); err != nil {
		return &mcp.CallToolResult{IsError: true}, HandsCommandOutput{}, err
	}
	event, err := appendProjectUIEventTx(ctx, tx, authorizedProjectId, workroomEventInput{
		Kind: "hands.instruction.changed", AggregateType: "hands_instruction", AggregateID: input.InstructionID,
		AggregateRevision: revision,
		Payload:           map[string]any{"instruction_id": input.InstructionID, "state": "cancelled", "revision": revision, "reason_code": "cancelled_by_fixer", "updated_at": now},
		ActorKind:         "fixer", ActorID: principalID, CausationID: causationID, CorrelationID: causationID,
	})
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, HandsCommandOutput{}, err
	}
	if err := appendWorkroomAuditTx(ctx, tx, authorizedProjectId, principalID, "hands.instruction.cancel", "hands_instruction", input.InstructionID, "authorized", "cancelled", causationID, causationID,
		map[string]any{"from_state": state, "reason": input.Reason}); err != nil {
		return &mcp.CallToolResult{IsError: true}, HandsCommandOutput{}, err
	}
	output = HandsCommandOutput{Status: "success", InstructionID: input.InstructionID, State: "cancelled", Revision: revision, ProjectSeq: event.Seq}
	if err := storeCommandDedupTx(ctx, tx, authorizedProjectId, principalID, "hands.instruction.cancel", idempotencyKey, requestHash, output); err != nil {
		return &mcp.CallToolResult{IsError: true}, HandsCommandOutput{}, err
	}
	if err := tx.Commit(); err != nil {
		return &mcp.CallToolResult{IsError: true}, HandsCommandOutput{}, err
	}
	notifyHandsWaiters(authorizedProjectId)
	return nil, output, nil
}

type ReviewHandsInstructionInput struct {
	InstructionID  string `json:"instruction_id" jsonschema:"Project-owned Hands instruction UUID in awaiting_review."`
	Decision       string `json:"decision" jsonschema:"accept or request_changes."`
	ReviewNote     string `json:"review_note,omitempty" jsonschema:"Governed review note of at most 8 KiB."`
	IdempotencyKey string `json:"idempotency_key" jsonschema:"Stable caller-generated idempotency key."`
}

func ReviewHandsInstruction(ctx context.Context, req *mcp.CallToolRequest, input ReviewHandsInstructionInput) (*mcp.CallToolResult, HandsCommandOutput, error) {
	if authorizedRole != "fixer" {
		return &mcp.CallToolResult{IsError: true}, HandsCommandOutput{}, fmt.Errorf("access denied: requires fixer role")
	}
	input.InstructionID = strings.TrimSpace(input.InstructionID)
	input.Decision = strings.ToLower(strings.TrimSpace(input.Decision))
	input.ReviewNote = strings.TrimSpace(input.ReviewNote)
	if input.InstructionID == "" || !workroomContainsString([]string{"accept", "request_changes"}, input.Decision) {
		return &mcp.CallToolResult{IsError: true}, HandsCommandOutput{}, fmt.Errorf("instruction_id is required and decision must be accept or request_changes")
	}
	if len(input.ReviewNote) > handsReviewNoteMaxBytes || !utf8.ValidString(input.ReviewNote) {
		return &mcp.CallToolResult{IsError: true}, HandsCommandOutput{}, fmt.Errorf("review_note must be valid UTF-8 and at most %d bytes", handsReviewNoteMaxBytes)
	}
	idempotencyKey, err := validateIdempotencyKey(input.IdempotencyKey)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, HandsCommandOutput{}, err
	}
	requestHash, _, err := hashCanonicalJSON(input)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, HandsCommandOutput{}, err
	}
	principalID := workroomPrincipalID()
	causationID, err := newWorkroomID()
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, HandsCommandOutput{}, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, HandsCommandOutput{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var output HandsCommandOutput
	if replayed, err := loadCommandDedupTx(ctx, tx, authorizedProjectId, principalID, "hands.instruction.review", idempotencyKey, requestHash, &output); err != nil {
		return &mcp.CallToolResult{IsError: true}, HandsCommandOutput{}, err
	} else if replayed {
		return nil, output, nil
	}
	var state, provider string
	var revision, compatSessionID, generation int
	err = tx.QueryRowContext(ctx, `
		SELECT instruction.state, instruction.revision, COALESCE(instruction.compat_session_id, 0),
		       instruction.requested_lane,
		       COALESCE(MAX(generation.generation), 0)
		FROM hands_instruction instruction
		LEFT JOIN hands_generation generation ON generation.instruction_id = instruction.id
		WHERE instruction.id = ? AND instruction.project_id = ?
		GROUP BY instruction.id`, input.InstructionID, authorizedProjectId).Scan(
		&state, &revision, &compatSessionID, &provider, &generation)
	if err == sql.ErrNoRows {
		return &mcp.CallToolResult{IsError: true}, HandsCommandOutput{}, fmt.Errorf("instruction not found in current project")
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, HandsCommandOutput{}, err
	}
	model, reasoning, _ := handsProviderConfig(provider)
	if state != "awaiting_review" {
		return &mcp.CallToolResult{IsError: true}, HandsCommandOutput{}, fmt.Errorf("instruction cannot be reviewed from state %s", state)
	}
	targetState := "completed"
	if input.Decision == "request_changes" {
		targetState = "queued"
	}
	now := workroomTimestamp()
	revision++
	var terminalAt any
	if targetState == "completed" {
		terminalAt = now
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE hands_instruction
		SET state = ?, state_reason_code = NULL, state_reason_text = NULL,
		    revision = ?, updated_at = ?, terminal_at = ?
		WHERE id = ? AND project_id = ?`, targetState, revision, now, terminalAt, input.InstructionID, authorizedProjectId); err != nil {
		return &mcp.CallToolResult{IsError: true}, HandsCommandOutput{}, err
	}
	if input.Decision == "accept" {
		if _, err := tx.ExecContext(ctx, `UPDATE session SET status = 'completed', updated_at = ? WHERE id = ? AND project_id = ?`, now, compatSessionID, authorizedProjectId); err != nil {
			return &mcp.CallToolResult{IsError: true}, HandsCommandOutput{}, err
		}
	} else {
		if _, err := tx.ExecContext(ctx, `
			UPDATE session SET status = 'pending', rework_count = COALESCE(rework_count, 0) + 1, updated_at = ?
			WHERE id = ? AND project_id = ?`, now, compatSessionID, authorizedProjectId); err != nil {
			return &mcp.CallToolResult{IsError: true}, HandsCommandOutput{}, err
		}
		generation++
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO hands_generation (
				instruction_id, generation, project_id, compat_session_id, provider, model,
				reasoning, status, launch_mode, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, 'planned', 'headless', ?, ?)`,
			input.InstructionID, generation, authorizedProjectId, compatSessionID,
			provider, model, reasoning, now, now); err != nil {
			return &mcp.CallToolResult{IsError: true}, HandsCommandOutput{}, err
		}
	}
	if _, err := appendHandsInstructionEventTx(ctx, tx, input.InstructionID, "instruction.reviewed", state, targetState, "fixer", principalID,
		map[string]any{"decision": input.Decision, "review_note": input.ReviewNote, "next_generation": generation}); err != nil {
		return &mcp.CallToolResult{IsError: true}, HandsCommandOutput{}, err
	}
	event, err := appendProjectUIEventTx(ctx, tx, authorizedProjectId, workroomEventInput{
		Kind: "hands.instruction.changed", AggregateType: "hands_instruction", AggregateID: input.InstructionID,
		AggregateRevision: revision,
		Payload:           map[string]any{"instruction_id": input.InstructionID, "state": targetState, "revision": revision, "decision": input.Decision, "updated_at": now},
		ActorKind:         "fixer", ActorID: principalID, CausationID: causationID, CorrelationID: causationID,
	})
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, HandsCommandOutput{}, err
	}
	if input.Decision == "request_changes" {
		event, err = appendProjectUIEventTx(ctx, tx, authorizedProjectId, workroomEventInput{
			Kind: "hands.generation.changed", AggregateType: "hands_generation", AggregateID: fmt.Sprintf("%s:%d", input.InstructionID, generation),
			AggregateRevision: 1,
			Payload:           map[string]any{"instruction_id": input.InstructionID, "generation": generation, "provider": provider, "model": model, "reasoning": reasoning, "status": "planned"},
			ActorKind:         "system", ActorID: "hands-dispatch", CausationID: causationID, CorrelationID: causationID,
		})
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, HandsCommandOutput{}, err
		}
	}
	if err := appendWorkroomAuditTx(ctx, tx, authorizedProjectId, principalID, "hands.instruction.review", "hands_instruction", input.InstructionID, "authorized", targetState, causationID, causationID,
		map[string]any{"decision": input.Decision, "review_note": input.ReviewNote, "generation": generation}); err != nil {
		return &mcp.CallToolResult{IsError: true}, HandsCommandOutput{}, err
	}
	output = HandsCommandOutput{Status: "success", InstructionID: input.InstructionID, State: targetState, Revision: revision, ProjectSeq: event.Seq}
	if err := storeCommandDedupTx(ctx, tx, authorizedProjectId, principalID, "hands.instruction.review", idempotencyKey, requestHash, output); err != nil {
		return &mcp.CallToolResult{IsError: true}, HandsCommandOutput{}, err
	}
	if err := tx.Commit(); err != nil {
		return &mcp.CallToolResult{IsError: true}, HandsCommandOutput{}, err
	}
	notifyHandsWaiters(authorizedProjectId)
	return nil, output, nil
}

type WaitHandsInstructionInput struct {
	InstructionID     string `json:"instruction_id" jsonschema:"Project-owned Hands instruction UUID."`
	AfterEventOrdinal int    `json:"after_event_ordinal,omitempty" jsonschema:"Durable instruction-event cursor already applied by the caller."`
	TimeoutSeconds    int    `json:"timeout_seconds,omitempty" jsonschema:"Bounded wait from 1 through 25 seconds; defaults to 20."`
}

type WaitHandsInstructionOutput struct {
	TimedOut           bool                       `json:"timed_out"`
	LatestEventOrdinal int                        `json:"latest_event_ordinal"`
	Instruction        *GetHandsInstructionOutput `json:"instruction,omitempty"`
}

func latestHandsInstructionEventOrdinal(ctx context.Context, projectID int, instructionID string) (int, error) {
	var ordinal int
	err := db.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(event.ordinal), 0)
		FROM hands_instruction instruction
		LEFT JOIN hands_instruction_event event ON event.instruction_id = instruction.id
		WHERE instruction.id = ? AND instruction.project_id = ?
		GROUP BY instruction.id`, instructionID, projectID).Scan(&ordinal)
	if err == sql.ErrNoRows {
		return 0, fmt.Errorf("instruction not found in current project")
	}
	return ordinal, err
}

func WaitHandsInstruction(ctx context.Context, req *mcp.CallToolRequest, input WaitHandsInstructionInput) (*mcp.CallToolResult, WaitHandsInstructionOutput, error) {
	if authorizedRole != "fixer" {
		return &mcp.CallToolResult{IsError: true}, WaitHandsInstructionOutput{}, fmt.Errorf("access denied: requires fixer role")
	}
	input.InstructionID = strings.TrimSpace(input.InstructionID)
	if input.InstructionID == "" || input.AfterEventOrdinal < 0 {
		return &mcp.CallToolResult{IsError: true}, WaitHandsInstructionOutput{}, fmt.Errorf("instruction_id is required and after_event_ordinal cannot be negative")
	}
	timeout := input.TimeoutSeconds
	if timeout == 0 {
		timeout = 20
	}
	if timeout < 1 || timeout > handsWaitMaxSeconds {
		return &mcp.CallToolResult{IsError: true}, WaitHandsInstructionOutput{}, fmt.Errorf("timeout_seconds must be 1..%d", handsWaitMaxSeconds)
	}
	read := func() (WaitHandsInstructionOutput, bool, error) {
		latest, err := latestHandsInstructionEventOrdinal(ctx, authorizedProjectId, input.InstructionID)
		if err != nil {
			return WaitHandsInstructionOutput{}, false, err
		}
		if latest <= input.AfterEventOrdinal {
			return WaitHandsInstructionOutput{LatestEventOrdinal: latest}, false, nil
		}
		_, detail, err := GetHandsInstruction(ctx, nil, GetHandsInstructionInput{InstructionID: input.InstructionID})
		if err != nil {
			return WaitHandsInstructionOutput{}, false, err
		}
		return WaitHandsInstructionOutput{LatestEventOrdinal: latest, Instruction: &detail}, true, nil
	}
	if output, changed, err := read(); err != nil {
		return &mcp.CallToolResult{IsError: true}, WaitHandsInstructionOutput{}, err
	} else if changed {
		return nil, output, nil
	}
	wake, unregister := registerHandsWaiter(authorizedProjectId)
	defer unregister()
	if output, changed, err := read(); err != nil {
		return &mcp.CallToolResult{IsError: true}, WaitHandsInstructionOutput{}, err
	} else if changed {
		return nil, output, nil
	}
	timer := time.NewTimer(time.Duration(timeout) * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return &mcp.CallToolResult{IsError: true}, WaitHandsInstructionOutput{}, ctx.Err()
		case <-timer.C:
			latest, err := latestHandsInstructionEventOrdinal(ctx, authorizedProjectId, input.InstructionID)
			if err != nil {
				return &mcp.CallToolResult{IsError: true}, WaitHandsInstructionOutput{}, err
			}
			return nil, WaitHandsInstructionOutput{TimedOut: true, LatestEventOrdinal: latest}, nil
		case <-wake:
		case <-ticker.C:
		}
		output, changed, err := read()
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, WaitHandsInstructionOutput{}, err
		}
		if changed {
			return nil, output, nil
		}
	}
}
