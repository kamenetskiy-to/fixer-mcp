package dashboardapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type SendFixerTurnInput struct {
	ThreadID       string `json:"thread_id,omitempty"`
	Content        string `json:"content"`
	IdempotencyKey string `json:"idempotency_key"`
}

type FixerTurnReceipt struct {
	Status     string `json:"status"`
	ThreadID   string `json:"thread_id"`
	TurnID     string `json:"turn_id"`
	Ordinal    int    `json:"ordinal"`
	ProjectSeq int64  `json:"project_seq"`
}

type BridgeSubmitHandsInstructionInput struct {
	InstructionText    string   `json:"instruction_text"`
	DeclaredWriteScope []string `json:"declared_write_scope,omitempty"`
	RequestedLane      string   `json:"requested_lane,omitempty"`
	RequestedModel     string   `json:"requested_model,omitempty"`
	RequestedReasoning string   `json:"requested_reasoning,omitempty"`
	IdempotencyKey     string   `json:"idempotency_key"`
}

type HandsInstructionReceipt struct {
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

type BridgeCancelHandsInstructionInput struct {
	Reason         string `json:"reason,omitempty"`
	IdempotencyKey string `json:"idempotency_key"`
}

type BridgeSelectHandsLaneInput struct {
	Provider       string `json:"provider"`
	IdempotencyKey string `json:"idempotency_key"`
}

type CommandReceipt struct {
	Status        string `json:"status"`
	InstructionID string `json:"instruction_id,omitempty"`
	State         string `json:"state,omitempty"`
	Revision      int    `json:"revision,omitempty"`
	ProjectSeq    int64  `json:"project_seq"`
}

type GenuiActionRequest struct {
	ProtocolVersion int             `json:"protocol_version"`
	SurfaceID       string          `json:"surface_id"`
	SurfaceRevision int             `json:"surface_revision"`
	ActionID        string          `json:"action_id"`
	ActionVersion   int             `json:"action_version"`
	TargetType      string          `json:"target_type"`
	TargetID        string          `json:"target_id"`
	Input           json.RawMessage `json:"input"`
	Confirmed       bool            `json:"confirmed"`
	IdempotencyKey  string          `json:"idempotency_key"`
}

type GenuiActionReceipt struct {
	Status       string `json:"status"`
	InvocationID string `json:"invocation_id"`
	ActionID     string `json:"action_id"`
	Decision     string `json:"decision"`
	ReasonCode   string `json:"reason_code,omitempty"`
	ProjectSeq   int64  `json:"project_seq"`
}

func appendWorkroomAuditMutationTx(ctx context.Context, tx *sql.Tx, projectID int, principal BridgePrincipal, actionID, targetType, targetID, decision, outcome, causationID string, detail any, now func() time.Time) error {
	id, err := newWorkroomUUID()
	if err != nil {
		return err
	}
	detailJSON, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	if len(detailJSON) > workroomMaxEventPayloadBytes {
		return fmt.Errorf("audit detail is too large")
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO workroom_audit_event (
			id, project_id, principal_id, action_id, target_type, target_id,
			decision, outcome, causation_id, correlation_id, detail_json, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, projectID, principal.PrincipalID, actionID, targetType, targetID,
		decision, outcome, causationID, principal.RequestID, string(detailJSON),
		dashboardWorkroomTimestamp(now))
	return err
}

func (r *Repository) SendFixerTurn(ctx context.Context, projectID int, principal BridgePrincipal, input SendFixerTurnInput) (FixerTurnReceipt, error) {
	if !bridgePrincipalHasCapability(principal, "fixer.turn.send") {
		return FixerTurnReceipt{}, ErrWorkroomAuthorization
	}
	input.Content = strings.TrimSpace(input.Content)
	if input.Content == "" || len(input.Content) > workroomMaxEventPayloadBytes || !utf8.ValidString(input.Content) {
		return FixerTurnReceipt{}, fmt.Errorf("content must contain 1..65536 valid UTF-8 bytes")
	}
	key, err := validateBridgeIdempotencyKey(input.IdempotencyKey)
	if err != nil {
		return FixerTurnReceipt{}, err
	}
	input.IdempotencyKey = key
	hash, err := workroomRequestHash(input)
	if err != nil {
		return FixerTurnReceipt{}, err
	}
	tx, err := r.dbWrite.BeginTx(ctx, nil)
	if err != nil {
		return FixerTurnReceipt{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var output FixerTurnReceipt
	if replayed, err := loadWorkroomDedupTx(ctx, tx, projectID, principal.PrincipalID, "fixer.turn.send", key, hash, &output, r.now); err != nil {
		return FixerTurnReceipt{}, err
	} else if replayed {
		return output, nil
	}
	var projectCount int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM project WHERE id = ?`, projectID).Scan(&projectCount); err != nil {
		return FixerTurnReceipt{}, err
	}
	if projectCount != 1 {
		return FixerTurnReceipt{}, sql.ErrNoRows
	}
	threadID := strings.TrimSpace(input.ThreadID)
	causationID, err := newWorkroomUUID()
	if err != nil {
		return FixerTurnReceipt{}, err
	}
	if threadID == "" {
		if err := tx.QueryRowContext(ctx, `
			SELECT id FROM fixer_thread WHERE project_id = ? AND state = 'active'
			ORDER BY updated_at DESC, id DESC LIMIT 1`, projectID).Scan(&threadID); err != nil && err != sql.ErrNoRows {
			return FixerTurnReceipt{}, err
		}
	}
	if threadID == "" {
		threadID, err = newWorkroomUUID()
		if err != nil {
			return FixerTurnReceipt{}, err
		}
		now := dashboardWorkroomTimestamp(r.now)
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO fixer_thread (id, project_id, provider, headline, state, created_at, updated_at)
			VALUES (?, ?, 'fixer', ?, 'active', ?, ?)`, threadID, projectID,
			truncateDashboardText(input.Content, 160), now, now); err != nil {
			return FixerTurnReceipt{}, err
		}
		if _, err := appendProjectUIEventMutationTx(ctx, tx, projectID, r.now, workroomEventMutation{
			Kind: "fixer.thread.created", AggregateType: "fixer_thread", AggregateID: threadID,
			AggregateRevision: 1, Payload: map[string]any{"id": threadID, "headline": truncateDashboardText(input.Content, 160), "state": "active"},
			ActorKind: "principal", ActorID: principal.PrincipalID, CausationID: causationID, CorrelationID: principal.RequestID,
		}); err != nil {
			return FixerTurnReceipt{}, err
		}
	} else {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM fixer_thread WHERE id = ? AND project_id = ?`, threadID, projectID).Scan(&count); err != nil {
			return FixerTurnReceipt{}, err
		}
		if count != 1 {
			return FixerTurnReceipt{}, sql.ErrNoRows
		}
	}
	var ordinal int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(ordinal), 0) + 1 FROM fixer_turn WHERE thread_id = ?`, threadID).Scan(&ordinal); err != nil {
		return FixerTurnReceipt{}, err
	}
	turnID, err := newWorkroomUUID()
	if err != nil {
		return FixerTurnReceipt{}, err
	}
	now := dashboardWorkroomTimestamp(r.now)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO fixer_turn (
			id, project_id, thread_id, ordinal, role, content, status,
			client_message_id, created_at, completed_at
		) VALUES (?, ?, ?, ?, 'user', ?, 'complete', ?, ?, ?)`,
		turnID, projectID, threadID, ordinal, input.Content, key, now, now); err != nil {
		return FixerTurnReceipt{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE fixer_thread SET updated_at = ? WHERE id = ? AND project_id = ?`, now, threadID, projectID); err != nil {
		return FixerTurnReceipt{}, err
	}
	event, err := appendProjectUIEventMutationTx(ctx, tx, projectID, r.now, workroomEventMutation{
		Kind: "fixer.turn.appended", AggregateType: "fixer_turn", AggregateID: turnID,
		AggregateRevision: 1,
		Payload:           map[string]any{"id": turnID, "thread_id": threadID, "ordinal": ordinal, "role": "user", "content": input.Content, "status": "complete", "created_at": now},
		ActorKind:         "principal", ActorID: principal.PrincipalID, CausationID: causationID, CorrelationID: principal.RequestID,
	})
	if err != nil {
		return FixerTurnReceipt{}, err
	}
	projectSeq := event.Seq
	resolvedSurfaceType := resolveRegisteredFixerSurfaceIntent(input.Content)
	if resolvedSurfaceType != "" {
		assistantTurnID, err := newWorkroomUUID()
		if err != nil {
			return FixerTurnReceipt{}, err
		}
		assistantOrdinal := ordinal + 1
		assistantContent := "Открываю юридический ресерч проекта как управляемую GenUI-поверхность."
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO fixer_turn (
				id, project_id, thread_id, ordinal, role, content, status,
				provider_turn_id, created_at, completed_at
			) VALUES (?, ?, ?, ?, 'fixer', ?, 'complete', ?, ?, ?)`,
			assistantTurnID, projectID, threadID, assistantOrdinal, assistantContent,
			"governed-intent:"+turnID, now, now); err != nil {
			return FixerTurnReceipt{}, err
		}
		assistantEvent, err := appendProjectUIEventMutationTx(ctx, tx, projectID, r.now, workroomEventMutation{
			Kind: "fixer.turn.appended", AggregateType: "fixer_turn", AggregateID: assistantTurnID,
			AggregateRevision: 1,
			Payload:           map[string]any{"id": assistantTurnID, "thread_id": threadID, "ordinal": assistantOrdinal, "role": "fixer", "content": assistantContent, "status": "complete", "created_at": now},
			ActorKind:         "fixer", ActorID: "governed-intent-router", CausationID: causationID, CorrelationID: principal.RequestID,
		})
		if err != nil {
			return FixerTurnReceipt{}, err
		}
		projectSeq = assistantEvent.Seq
		surfaceReceipt, err := r.materializeRegisteredSurfaceTx(ctx, tx, projectID, principal, threadID, turnID, resolvedSurfaceType, map[string]any{}, causationID)
		if err != nil {
			return FixerTurnReceipt{}, err
		}
		projectSeq = surfaceReceipt.ProjectSeq
	}
	output = FixerTurnReceipt{Status: "accepted", ThreadID: threadID, TurnID: turnID, Ordinal: ordinal, ProjectSeq: projectSeq}
	if err := storeWorkroomDedupTx(ctx, tx, projectID, principal.PrincipalID, "fixer.turn.send", key, hash, output, r.now); err != nil {
		return FixerTurnReceipt{}, err
	}
	if err := appendWorkroomAuditMutationTx(ctx, tx, projectID, principal, "fixer.turn.send", "fixer_thread", threadID, "authorized", "accepted", causationID,
		map[string]any{"turn_id": turnID, "ordinal": ordinal, "resolved_surface_type": resolvedSurfaceType}, r.now); err != nil {
		return FixerTurnReceipt{}, err
	}
	if err := tx.Commit(); err != nil {
		return FixerTurnReceipt{}, err
	}
	r.workroomEvents.notify(projectID)
	return output, nil
}

func truncateDashboardText(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= limit {
		return string(runes)
	}
	if limit < 2 {
		return "…"
	}
	return string(runes[:limit-1]) + "…"
}

func normalizeBridgeWriteScope(raw []string) ([]string, string, error) {
	if len(raw) == 0 {
		return []string{}, "[]", nil
	}
	seen := map[string]struct{}{}
	values := []string{}
	for _, entry := range raw {
		entry = strings.TrimSpace(entry)
		if entry == "" || filepath.IsAbs(entry) {
			return nil, "", fmt.Errorf("write scope entries must be non-empty project-relative paths")
		}
		cleaned := filepath.ToSlash(filepath.Clean(entry))
		if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
			return nil, "", fmt.Errorf("write scope entries must stay within the project")
		}
		cleaned = strings.TrimPrefix(cleaned, "./")
		if _, ok := seen[cleaned]; ok {
			continue
		}
		seen[cleaned] = struct{}{}
		values = append(values, cleaned)
	}
	sort.Strings(values)
	payload, err := json.Marshal(values)
	return values, string(payload), err
}

func (r *Repository) SubmitHandsInstruction(ctx context.Context, projectID int, principal BridgePrincipal, input BridgeSubmitHandsInstructionInput) (HandsInstructionReceipt, error) {
	if !bridgePrincipalHasCapability(principal, "hands.instruction.submit") {
		return HandsInstructionReceipt{}, ErrWorkroomAuthorization
	}
	input.InstructionText = strings.TrimSpace(input.InstructionText)
	if input.InstructionText == "" || len(input.InstructionText) > 64*1024 || !utf8.ValidString(input.InstructionText) {
		return HandsInstructionReceipt{}, fmt.Errorf("instruction_text must contain 1..65536 valid UTF-8 bytes")
	}
	key, err := validateBridgeIdempotencyKey(input.IdempotencyKey)
	if err != nil {
		return HandsInstructionReceipt{}, err
	}
	writeScope, scopeJSON, err := normalizeBridgeWriteScope(input.DeclaredWriteScope)
	if err != nil {
		return HandsInstructionReceipt{}, err
	}
	input.DeclaredWriteScope = writeScope
	input.RequestedLane = strings.ToLower(strings.TrimSpace(input.RequestedLane))
	input.RequestedModel = strings.TrimSpace(input.RequestedModel)
	input.RequestedReasoning = strings.ToLower(strings.TrimSpace(input.RequestedReasoning))
	if input.RequestedLane != "" && input.RequestedLane != "codex" && input.RequestedLane != "claude" && input.RequestedLane != "kimi-code" && input.RequestedLane != "antigravity" {
		return HandsInstructionReceipt{}, fmt.Errorf("requested_lane is not registered")
	}
	input.IdempotencyKey = key
	hash, err := workroomRequestHash(input)
	if err != nil {
		return HandsInstructionReceipt{}, err
	}
	tx, err := r.dbWrite.BeginTx(ctx, nil)
	if err != nil {
		return HandsInstructionReceipt{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var output HandsInstructionReceipt
	if replayed, err := loadWorkroomDedupTx(ctx, tx, projectID, principal.PrincipalID, "hands.instruction.submit", key, hash, &output, r.now); err != nil {
		return HandsInstructionReceipt{}, err
	} else if replayed {
		return output, nil
	}
	var actorID, authorityState, defaultLane string
	var ordinal int
	if err := tx.QueryRowContext(ctx, `
		UPDATE project_hands SET next_instruction_ordinal = next_instruction_ordinal + 1, updated_at = ?
		WHERE project_id = ?
		RETURNING actor_id, authority_state, default_lane, next_instruction_ordinal - 1`,
		dashboardWorkroomTimestamp(r.now), projectID).Scan(&actorID, &authorityState, &defaultLane, &ordinal); err != nil {
		return HandsInstructionReceipt{}, err
	}
	lane := input.RequestedLane
	if lane == "" {
		lane = defaultLane
	}
	spec, ok := dashboardHandsProviderSpecFor(lane)
	if !ok {
		return HandsInstructionReceipt{}, fmt.Errorf("unsupported Hands provider %q", lane)
	}
	model := input.RequestedModel
	if model == "" {
		model = spec.defaultModel
	}
	if !dashboardHandsProviderOptionContains(spec.modelOptions, model) {
		return HandsInstructionReceipt{}, fmt.Errorf("requested_model is not registered")
	}
	reasoning := input.RequestedReasoning
	if reasoning == "" {
		reasoning = spec.defaultReasoning
	}
	if !dashboardHandsProviderOptionContains(spec.reasoningOptions, reasoning) {
		return HandsInstructionReceipt{}, fmt.Errorf("requested_reasoning is not registered")
	}
	riskClass, reviewPolicy := "read_only", "auto_read_only"
	if len(writeScope) > 0 {
		riskClass, reviewPolicy = "repository_write", "fixer_required"
	}
	state, reasonCode, reasonText := "queued", "", ""
	if authorityState != "enabled" {
		state, reasonCode, reasonText = "unsupported", "hands_authority_unavailable", "The permanent Hands authority is not enabled."
	} else if len(writeScope) > 0 {
		var cwd string
		if err := tx.QueryRowContext(ctx, `SELECT cwd FROM project WHERE id = ?`, projectID).Scan(&cwd); err != nil {
			return HandsInstructionReceipt{}, err
		}
		if _, err := os.Stat(filepath.Join(cwd, ".git")); os.IsNotExist(err) {
			state, riskClass, reasonCode = "unsupported", "unsupported_high_risk", "non_git_repository_write"
			reasonText = "Repository-write Hands instructions require a Git project."
		} else if err != nil {
			return HandsInstructionReceipt{}, err
		}
	}
	instructionID, err := newWorkroomUUID()
	if err != nil {
		return HandsInstructionReceipt{}, err
	}
	now := dashboardWorkroomTimestamp(r.now)
	envelopeJSON, err := json.Marshal(map[string]any{
		"protocol": "fixer.hands.instruction", "protocol_version": 1,
		"instruction_id": instructionID, "project_id": projectID, "actor_id": actorID,
		"ordinal": ordinal, "instruction_text": input.InstructionText,
		"declared_write_scope": writeScope, "requested_lane": lane, "risk_class": riskClass,
		"review_policy": reviewPolicy, "issuer_principal_id": principal.PrincipalID,
		"source": map[string]string{"channel_kind": "serverpod", "channel_id": principal.RequestID}, "created_at": now,
	})
	if err != nil {
		return HandsInstructionReceipt{}, err
	}
	var terminalAt any
	if state == "unsupported" {
		terminalAt = now
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO hands_instruction (
			id, project_id, actor_id, ordinal, source_channel_kind, source_channel_id,
			issuer_principal_id, instruction_text, declared_write_scope_json, instruction_envelope_json,
			requested_lane, risk_class, review_policy, state, state_reason_code, state_reason_text,
			idempotency_key, revision, created_at, updated_at, terminal_at
		) VALUES (?, ?, ?, ?, 'serverpod', ?, ?, ?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), ?, 1, ?, ?, ?)`,
		instructionID, projectID, actorID, ordinal, principal.RequestID, principal.PrincipalID,
		input.InstructionText, scopeJSON, string(envelopeJSON), lane, riskClass, reviewPolicy,
		state, reasonCode, reasonText, key, now, now, terminalAt); err != nil {
		return HandsInstructionReceipt{}, err
	}
	compatSessionID := int64(0)
	if state == "queued" {
		result, err := tx.ExecContext(ctx, `
			INSERT INTO session (
				project_id, task_description, status, cli_backend, cli_model, cli_reasoning,
				declared_write_scope, session_kind, created_at, updated_at
			) VALUES (?, ?, 'pending', ?, ?, ?, ?, 'hands_instruction', ?, ?)`, projectID,
			fmt.Sprintf("Permanent Hands instruction %d (%s).\n\n%s", ordinal, instructionID, input.InstructionText),
			lane, model, reasoning, scopeJSON, now, now)
		if err != nil {
			return HandsInstructionReceipt{}, err
		}
		compatSessionID, err = result.LastInsertId()
		if err != nil {
			return HandsInstructionReceipt{}, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE hands_instruction SET compat_session_id = ? WHERE id = ?`, compatSessionID, instructionID); err != nil {
			return HandsInstructionReceipt{}, err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO legacy_manual_session_link (
				project_id, session_id, disposition, evidence_json, hands_instruction_id, classified_at, classified_by
			) VALUES (?, ?, 'live', '{"classification":"post_cutover_compatibility_projection"}', ?, ?, 'hands_dispatch')`,
			projectID, compatSessionID, instructionID, now); err != nil {
			return HandsInstructionReceipt{}, err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO hands_generation (
				instruction_id, generation, project_id, compat_session_id, provider, model, reasoning,
				status, launch_mode, created_at, updated_at
			) VALUES (?, 1, ?, ?, ?, ?, ?, 'planned', 'headless', ?, ?)`,
			instructionID, projectID, compatSessionID, lane, model, reasoning, now, now); err != nil {
			return HandsInstructionReceipt{}, err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO session_mcp_server (session_id, mcp_server_id)
			SELECT ?, mcp_server_id FROM project_hands_mcp_server WHERE project_id = ?`, compatSessionID, projectID); err != nil {
			return HandsInstructionReceipt{}, err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO session_mcp_server (session_id, mcp_server_id)
			SELECT ?, mcp_server_id FROM project_mcp_server WHERE project_id = ?`, compatSessionID, projectID); err != nil {
			return HandsInstructionReceipt{}, err
		}
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO hands_instruction_event (
			instruction_id, ordinal, event_type, to_state, actor_kind, actor_id, payload_json, created_at
		) VALUES (?, 1, 'instruction.submitted', ?, 'principal', ?, ?, ?)`,
		instructionID, state, principal.PrincipalID,
		fmt.Sprintf(`{"lane":%q,"risk_class":%q,"reason_code":%q}`, lane, riskClass, reasonCode), now); err != nil {
		return HandsInstructionReceipt{}, err
	}
	causationID, err := newWorkroomUUID()
	if err != nil {
		return HandsInstructionReceipt{}, err
	}
	event, err := appendProjectUIEventMutationTx(ctx, tx, projectID, r.now, workroomEventMutation{
		Kind: "hands.instruction.created", AggregateType: "hands_instruction", AggregateID: instructionID,
		AggregateRevision: 1,
		Payload:           map[string]any{"instruction_id": instructionID, "ordinal": ordinal, "state": state, "lane": lane, "risk_class": riskClass, "review_policy": reviewPolicy, "reason_code": reasonCode, "updated_at": now},
		ActorKind:         "principal", ActorID: principal.PrincipalID, CausationID: causationID, CorrelationID: principal.RequestID,
	})
	if err != nil {
		return HandsInstructionReceipt{}, err
	}
	if state == "queued" {
		event, err = appendProjectUIEventMutationTx(ctx, tx, projectID, r.now, workroomEventMutation{
			Kind: "hands.generation.changed", AggregateType: "hands_generation", AggregateID: instructionID + ":1",
			AggregateRevision: 1,
			Payload:           map[string]any{"instruction_id": instructionID, "generation": 1, "provider": lane, "model": model, "reasoning": reasoning, "status": "planned"},
			ActorKind:         "system", ActorID: "hands-dispatch", CausationID: causationID, CorrelationID: principal.RequestID,
		})
		if err != nil {
			return HandsInstructionReceipt{}, err
		}
	}
	if err := appendWorkroomAuditMutationTx(ctx, tx, projectID, principal, "hands.instruction.submit", "hands_instruction", instructionID, "authorized", state, causationID,
		map[string]any{"lane": lane, "risk_class": riskClass, "reason_code": reasonCode}, r.now); err != nil {
		return HandsInstructionReceipt{}, err
	}
	output = HandsInstructionReceipt{
		Status: "accepted", InstructionID: instructionID, Ordinal: ordinal, State: state,
		Lane: lane, RiskClass: riskClass, ProjectSeq: event.Seq, ReasonCode: reasonCode, ReasonText: reasonText,
	}
	if err := storeWorkroomDedupTx(ctx, tx, projectID, principal.PrincipalID, "hands.instruction.submit", key, hash, output, r.now); err != nil {
		return HandsInstructionReceipt{}, err
	}
	if err := tx.Commit(); err != nil {
		return HandsInstructionReceipt{}, err
	}
	r.workroomEvents.notify(projectID)
	return output, nil
}

func (r *Repository) CancelHandsInstruction(ctx context.Context, projectID int, instructionID string, principal BridgePrincipal, input BridgeCancelHandsInstructionInput) (CommandReceipt, error) {
	instructionID = strings.TrimSpace(instructionID)
	input.Reason = strings.TrimSpace(input.Reason)
	if instructionID == "" || utf8.RuneCountInString(input.Reason) > 1000 {
		return CommandReceipt{}, fmt.Errorf("instruction id is required and reason is too long")
	}
	key, err := validateBridgeIdempotencyKey(input.IdempotencyKey)
	if err != nil {
		return CommandReceipt{}, err
	}
	input.IdempotencyKey = key
	hash, err := workroomRequestHash(struct {
		InstructionID string
		Input         BridgeCancelHandsInstructionInput
	}{instructionID, input})
	if err != nil {
		return CommandReceipt{}, err
	}
	tx, err := r.dbWrite.BeginTx(ctx, nil)
	if err != nil {
		return CommandReceipt{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var output CommandReceipt
	if replayed, err := loadWorkroomDedupTx(ctx, tx, projectID, principal.PrincipalID, "hands.instruction.cancel", key, hash, &output, r.now); err != nil {
		return CommandReceipt{}, err
	} else if replayed {
		return output, nil
	}
	var state, issuerPrincipalID string
	var revision, sessionID int
	if err := tx.QueryRowContext(ctx, `
		SELECT state, revision, COALESCE(compat_session_id, 0), issuer_principal_id
		FROM hands_instruction WHERE id = ? AND project_id = ?`, instructionID, projectID).Scan(&state, &revision, &sessionID, &issuerPrincipalID); err != nil {
		return CommandReceipt{}, err
	}
	privileged := bridgePrincipalHasPrivilegedHandsRole(principal)
	issuerMayCancelQueued := issuerPrincipalID == principal.PrincipalID && (state == "queued" || state == "waiting_for_lease")
	if !privileged && !issuerMayCancelQueued {
		return CommandReceipt{}, ErrWorkroomAuthorization
	}
	if state != "queued" && state != "waiting_for_lease" && state != "starting" && state != "running" {
		return CommandReceipt{}, fmt.Errorf("instruction cannot be cancelled from state %s", state)
	}
	now := dashboardWorkroomTimestamp(r.now)
	revision++
	if _, err := tx.ExecContext(ctx, `
		UPDATE hands_instruction
		SET state = 'cancelled', state_reason_code = 'cancelled_by_principal',
		    state_reason_text = NULLIF(?, ''), revision = ?, updated_at = ?, terminal_at = ?
		WHERE id = ? AND project_id = ?`, input.Reason, revision, now, now, instructionID, projectID); err != nil {
		return CommandReceipt{}, err
	}
	if sessionID > 0 {
		if _, err := tx.ExecContext(ctx, `UPDATE session SET status = 'completed', updated_at = ? WHERE id = ? AND project_id = ?`, now, sessionID, projectID); err != nil {
			return CommandReceipt{}, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE hands_generation SET status = 'stopped', ended_at = ?, stop_reason = 'cancelled_by_principal', updated_at = ? WHERE instruction_id = ? AND status IN ('planned', 'starting', 'running')`, now, now, instructionID); err != nil {
			return CommandReceipt{}, err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE project_write_lease SET state = 'released', released_at = ?, release_reason = 'cancelled_by_principal' WHERE project_id = ? AND owner_id = ? AND owner_kind = 'hands_instruction' AND state = 'active'`, now, projectID, instructionID); err != nil {
		return CommandReceipt{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO hands_instruction_event (
			instruction_id, ordinal, event_type, from_state, to_state, actor_kind, actor_id, payload_json, created_at
		) VALUES (?, (SELECT COALESCE(MAX(ordinal), 0) + 1 FROM hands_instruction_event WHERE instruction_id = ?),
		          'instruction.cancelled', ?, 'cancelled', 'principal', ?, ?, ?)`,
		instructionID, instructionID, state, principal.PrincipalID,
		fmt.Sprintf(`{"reason":%q}`, input.Reason), now); err != nil {
		return CommandReceipt{}, err
	}
	causationID, err := newWorkroomUUID()
	if err != nil {
		return CommandReceipt{}, err
	}
	event, err := appendProjectUIEventMutationTx(ctx, tx, projectID, r.now, workroomEventMutation{
		Kind: "hands.instruction.changed", AggregateType: "hands_instruction", AggregateID: instructionID,
		AggregateRevision: revision,
		Payload:           map[string]any{"instruction_id": instructionID, "state": "cancelled", "revision": revision, "reason_code": "cancelled_by_principal", "updated_at": now},
		ActorKind:         "principal", ActorID: principal.PrincipalID, CausationID: causationID, CorrelationID: principal.RequestID,
	})
	if err != nil {
		return CommandReceipt{}, err
	}
	if err := appendWorkroomAuditMutationTx(ctx, tx, projectID, principal, "hands.instruction.cancel", "hands_instruction", instructionID, "authorized", "cancelled", causationID, map[string]any{"from_state": state}, r.now); err != nil {
		return CommandReceipt{}, err
	}
	output = CommandReceipt{Status: "success", InstructionID: instructionID, State: "cancelled", Revision: revision, ProjectSeq: event.Seq}
	if err := storeWorkroomDedupTx(ctx, tx, projectID, principal.PrincipalID, "hands.instruction.cancel", key, hash, output, r.now); err != nil {
		return CommandReceipt{}, err
	}
	if err := tx.Commit(); err != nil {
		return CommandReceipt{}, err
	}
	r.workroomEvents.notify(projectID)
	return output, nil
}

type feedbackActionInput struct {
	Vote       int    `json:"vote"`
	ReasonCode string `json:"reason_code,omitempty"`
	Comment    string `json:"comment,omitempty"`
}

type reviewActionInput struct {
	ReviewNote string `json:"review_note,omitempty"`
}

func decodeClosedJSON(raw json.RawMessage, output any) error {
	if len(raw) == 0 {
		raw = []byte(`{}`)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values are not allowed")
		}
		return err
	}
	return nil
}

func surfaceDocumentAllowsAction(documentJSON, actionID string, actionVersion int) bool {
	var document struct {
		Actions []struct {
			ActionID      string `json:"action_id"`
			ActionVersion int    `json:"action_version"`
			Enabled       bool   `json:"enabled"`
		} `json:"actions"`
	}
	if json.Unmarshal([]byte(documentJSON), &document) != nil {
		return false
	}
	for _, action := range document.Actions {
		if action.ActionID == actionID && action.ActionVersion == actionVersion && action.Enabled {
			return true
		}
	}
	return false
}

func (r *Repository) SelectHandsLane(ctx context.Context, projectID int, principal BridgePrincipal, input BridgeSelectHandsLaneInput) (GenuiActionReceipt, error) {
	if !bridgePrincipalHasCapability(principal, "hands.lane.admin") {
		return GenuiActionReceipt{}, ErrWorkroomAuthorization
	}
	input.Provider = strings.ToLower(strings.TrimSpace(input.Provider))
	if _, _, ok := dashboardHandsProviderConfig(input.Provider); !ok {
		return GenuiActionReceipt{}, fmt.Errorf("provider is not registered")
	}
	key, err := validateBridgeIdempotencyKey(input.IdempotencyKey)
	if err != nil {
		return GenuiActionReceipt{}, err
	}
	input.IdempotencyKey = key
	hash, err := workroomRequestHash(input)
	if err != nil {
		return GenuiActionReceipt{}, err
	}
	tx, err := r.dbWrite.BeginTx(ctx, nil)
	if err != nil {
		return GenuiActionReceipt{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var output GenuiActionReceipt
	if replayed, err := loadWorkroomDedupTx(ctx, tx, projectID, principal.PrincipalID, "hands.lane.select", key, hash, &output, r.now); err != nil {
		return GenuiActionReceipt{}, err
	} else if replayed {
		return output, nil
	}
	invocationID, err := newWorkroomUUID()
	if err != nil {
		return GenuiActionReceipt{}, err
	}
	now := dashboardWorkroomTimestamp(r.now)
	var actorID string
	if err := tx.QueryRowContext(ctx, `
		UPDATE project_hands SET default_lane = ?, updated_at = ?
		WHERE project_id = ? RETURNING actor_id`, input.Provider, now, projectID).Scan(&actorID); err != nil {
		return GenuiActionReceipt{}, err
	}
	event, err := appendProjectUIEventMutationTx(ctx, tx, projectID, r.now, workroomEventMutation{
		Kind: "hands.lane.changed", AggregateType: "project_hands", AggregateID: actorID,
		AggregateRevision: 1,
		Payload:           map[string]any{"provider": input.Provider, "updated_at": now},
		ActorKind:         "principal", ActorID: principal.PrincipalID,
		CausationID: invocationID, CorrelationID: principal.RequestID,
	})
	if err != nil {
		return GenuiActionReceipt{}, err
	}
	if err := appendWorkroomAuditMutationTx(ctx, tx, projectID, principal, "hands.lane.select", "project_hands", actorID, "authorized", "succeeded", invocationID, map[string]any{"provider": input.Provider}, r.now); err != nil {
		return GenuiActionReceipt{}, err
	}
	output = GenuiActionReceipt{Status: "succeeded", InvocationID: invocationID, ActionID: "hands.lane.select", Decision: "authorized", ProjectSeq: event.Seq}
	if err := storeWorkroomDedupTx(ctx, tx, projectID, principal.PrincipalID, "hands.lane.select", key, hash, output, r.now); err != nil {
		return GenuiActionReceipt{}, err
	}
	if err := tx.Commit(); err != nil {
		return GenuiActionReceipt{}, err
	}
	r.workroomEvents.notify(projectID)
	return output, nil
}

func (r *Repository) InvokeGenuiAction(ctx context.Context, projectID int, principal BridgePrincipal, input GenuiActionRequest) (GenuiActionReceipt, error) {
	if input.ProtocolVersion != workroomProtocolVersion || input.ActionVersion != 1 || strings.TrimSpace(input.SurfaceID) == "" || input.SurfaceRevision < 1 {
		return GenuiActionReceipt{}, fmt.Errorf("invalid protocol, action version, or surface revision")
	}
	key, err := validateBridgeIdempotencyKey(input.IdempotencyKey)
	if err != nil {
		return GenuiActionReceipt{}, err
	}
	input.IdempotencyKey = key
	hash, err := workroomRequestHash(input)
	if err != nil {
		return GenuiActionReceipt{}, err
	}
	tx, err := r.dbWrite.BeginTx(ctx, nil)
	if err != nil {
		return GenuiActionReceipt{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var output GenuiActionReceipt
	if replayed, err := loadWorkroomDedupTx(ctx, tx, projectID, principal.PrincipalID, "genui.action.invoke", key, hash, &output, r.now); err != nil {
		return GenuiActionReceipt{}, err
	} else if replayed {
		return output, nil
	}
	var currentRevision int
	var surfaceState, documentJSON string
	err = tx.QueryRowContext(ctx, `
		SELECT instance.current_revision, instance.state, revision.document_json
		FROM genui_surface_instance instance
		JOIN genui_surface_revision revision ON revision.surface_id = instance.id AND revision.revision = ?
		WHERE instance.id = ? AND instance.project_id = ?`, input.SurfaceRevision, input.SurfaceID, projectID).Scan(
		&currentRevision, &surfaceState, &documentJSON)
	if err != nil {
		return GenuiActionReceipt{}, sql.ErrNoRows
	}
	if currentRevision != input.SurfaceRevision || surfaceState != "presented" {
		return GenuiActionReceipt{}, fmt.Errorf("surface_revision_stale")
	}
	if !surfaceDocumentAllowsAction(documentJSON, input.ActionID, input.ActionVersion) {
		return GenuiActionReceipt{}, fmt.Errorf("action_not_available_on_surface")
	}
	invocationID, err := newWorkroomUUID()
	if err != nil {
		return GenuiActionReceipt{}, err
	}
	causationID := invocationID
	now := dashboardWorkroomTimestamp(r.now)
	decision, status, reasonCode := "authorized", "executing", ""
	if (input.ActionID == "hands.review.accept" || input.ActionID == "hands.review.request_changes") && !input.Confirmed {
		decision, status, reasonCode = "pending", "needs_confirmation", "confirmation_required"
	}
	inputJSON := input.Input
	if len(inputJSON) == 0 {
		inputJSON = []byte(`{}`)
	}
	if !json.Valid(inputJSON) || len(inputJSON) > workroomMaxEventPayloadBytes {
		return GenuiActionReceipt{}, fmt.Errorf("action input is invalid or too large")
	}
	var feedback feedbackActionInput
	var review reviewActionInput
	switch input.ActionID {
	case "genui.feedback.submit":
		if input.TargetType != "genui_surface" || input.TargetID != input.SurfaceID {
			return GenuiActionReceipt{}, fmt.Errorf("feedback target is invalid")
		}
		if err := decodeClosedJSON(inputJSON, &feedback); err != nil || (feedback.Vote != -1 && feedback.Vote != 1) || utf8.RuneCountInString(feedback.Comment) > 1000 {
			return GenuiActionReceipt{}, fmt.Errorf("feedback input is invalid")
		}
		if feedback.ReasonCode != "" && feedback.ReasonCode != "accurate" && feedback.ReasonCode != "helpful" && feedback.ReasonCode != "unclear" && feedback.ReasonCode != "stale" && feedback.ReasonCode != "unsafe" && feedback.ReasonCode != "other" {
			return GenuiActionReceipt{}, fmt.Errorf("feedback reason is not registered")
		}
		if !bridgePrincipalHasCapability(principal, "genui.feedback.write") {
			return GenuiActionReceipt{}, ErrWorkroomAuthorization
		}
	case "hands.review.accept", "hands.review.request_changes":
		if input.TargetType != "hands_instruction" || strings.TrimSpace(input.TargetID) == "" {
			return GenuiActionReceipt{}, fmt.Errorf("review target is invalid")
		}
		if err := decodeClosedJSON(inputJSON, &review); err != nil || len(review.ReviewNote) > 8*1024 {
			return GenuiActionReceipt{}, fmt.Errorf("review input is invalid")
		}
		if !bridgePrincipalHasCapability(principal, "hands.review") {
			return GenuiActionReceipt{}, ErrWorkroomAuthorization
		}
	default:
		return GenuiActionReceipt{}, fmt.Errorf("action execution owner is not the Go bridge")
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO genui_action_invocation (
			id, project_id, surface_id, surface_revision, action_id, action_version,
			target_type, target_id, input_json, principal_id, decision, status,
			reason_code, idempotency_key, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), ?, ?)`,
		invocationID, projectID, input.SurfaceID, input.SurfaceRevision, input.ActionID,
		input.ActionVersion, input.TargetType, input.TargetID, string(inputJSON), principal.PrincipalID,
		decision, status, reasonCode, key, now); err != nil {
		return GenuiActionReceipt{}, err
	}
	if status == "needs_confirmation" {
		event, err := appendProjectUIEventMutationTx(ctx, tx, projectID, r.now, workroomEventMutation{
			Kind: "genui.action.changed", AggregateType: "genui_action", AggregateID: invocationID,
			AggregateRevision: 1,
			Payload:           map[string]any{"invocation_id": invocationID, "action_id": input.ActionID, "decision": decision, "status": status, "reason_code": reasonCode},
			ActorKind:         "principal", ActorID: principal.PrincipalID, CausationID: causationID, CorrelationID: principal.RequestID,
		})
		if err != nil {
			return GenuiActionReceipt{}, err
		}
		output = GenuiActionReceipt{Status: status, InvocationID: invocationID, ActionID: input.ActionID, Decision: decision, ReasonCode: reasonCode, ProjectSeq: event.Seq}
		if err := storeWorkroomDedupTx(ctx, tx, projectID, principal.PrincipalID, "genui.action.invoke", key, hash, output, r.now); err != nil {
			return GenuiActionReceipt{}, err
		}
		if err := tx.Commit(); err != nil {
			return GenuiActionReceipt{}, err
		}
		r.workroomEvents.notify(projectID)
		return output, nil
	}
	var domainEvent ProjectUIEvent
	switch input.ActionID {
	case "genui.feedback.submit":
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO genui_surface_feedback (
				surface_id, revision, principal_id, vote, reason_code, comment, created_at, updated_at
			) VALUES (?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), ?, ?)
			ON CONFLICT(surface_id, revision, principal_id) DO UPDATE SET
				vote = excluded.vote, reason_code = excluded.reason_code,
				comment = excluded.comment, updated_at = excluded.updated_at`,
			input.SurfaceID, input.SurfaceRevision, principal.PrincipalID, feedback.Vote,
			feedback.ReasonCode, feedback.Comment, now, now); err != nil {
			return GenuiActionReceipt{}, err
		}
		domainEvent, err = appendProjectUIEventMutationTx(ctx, tx, projectID, r.now, workroomEventMutation{
			Kind: "genui.feedback.recorded", AggregateType: "genui_feedback",
			AggregateID:       input.SurfaceID + ":" + strconv.Itoa(input.SurfaceRevision) + ":" + principal.PrincipalID,
			AggregateRevision: input.SurfaceRevision,
			Payload:           map[string]any{"surface_id": input.SurfaceID, "revision": input.SurfaceRevision, "principal_id": principal.PrincipalID, "vote": feedback.Vote, "reason_code": feedback.ReasonCode, "updated_at": now},
			ActorKind:         "principal", ActorID: principal.PrincipalID, CausationID: causationID, CorrelationID: principal.RequestID,
		})
	case "hands.review.accept", "hands.review.request_changes":
		decisionName := "accept"
		if input.ActionID == "hands.review.request_changes" {
			decisionName = "request_changes"
		}
		domainEvent, err = reviewHandsInstructionTx(ctx, tx, projectID, principal, input.TargetID, decisionName, review.ReviewNote, causationID, r.now)
	}
	if err != nil {
		return GenuiActionReceipt{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE genui_action_invocation
		SET status = 'succeeded', completed_at = ? WHERE id = ?`, now, invocationID); err != nil {
		return GenuiActionReceipt{}, err
	}
	actionEvent, err := appendProjectUIEventMutationTx(ctx, tx, projectID, r.now, workroomEventMutation{
		Kind: "genui.action.changed", AggregateType: "genui_action", AggregateID: invocationID,
		AggregateRevision: 2,
		Payload:           map[string]any{"invocation_id": invocationID, "action_id": input.ActionID, "decision": "authorized", "status": "succeeded", "completed_at": now, "domain_seq": domainEvent.Seq},
		ActorKind:         "principal", ActorID: principal.PrincipalID, CausationID: causationID, CorrelationID: principal.RequestID,
	})
	if err != nil {
		return GenuiActionReceipt{}, err
	}
	if err := appendWorkroomAuditMutationTx(ctx, tx, projectID, principal, input.ActionID, input.TargetType, input.TargetID, "authorized", "succeeded", causationID,
		map[string]any{"invocation_id": invocationID, "surface_id": input.SurfaceID, "surface_revision": input.SurfaceRevision}, r.now); err != nil {
		return GenuiActionReceipt{}, err
	}
	output = GenuiActionReceipt{Status: "succeeded", InvocationID: invocationID, ActionID: input.ActionID, Decision: "authorized", ProjectSeq: actionEvent.Seq}
	if err := storeWorkroomDedupTx(ctx, tx, projectID, principal.PrincipalID, "genui.action.invoke", key, hash, output, r.now); err != nil {
		return GenuiActionReceipt{}, err
	}
	if err := tx.Commit(); err != nil {
		return GenuiActionReceipt{}, err
	}
	r.workroomEvents.notify(projectID)
	return output, nil
}

func reviewHandsInstructionTx(ctx context.Context, tx *sql.Tx, projectID int, principal BridgePrincipal, instructionID, decision, reviewNote, causationID string, nowFunc func() time.Time) (ProjectUIEvent, error) {
	var state, provider string
	var revision, sessionID, generation int
	err := tx.QueryRowContext(ctx, `
		SELECT instruction.state, instruction.revision, COALESCE(instruction.compat_session_id, 0),
		       instruction.requested_lane,
		       COALESCE(MAX(generation.generation), 0)
		FROM hands_instruction instruction
		LEFT JOIN hands_generation generation ON generation.instruction_id = instruction.id
		WHERE instruction.id = ? AND instruction.project_id = ? GROUP BY instruction.id`, instructionID, projectID).Scan(
		&state, &revision, &sessionID, &provider, &generation)
	if err != nil {
		return ProjectUIEvent{}, err
	}
	model, reasoning, _ := dashboardHandsProviderConfig(provider)
	if state != "awaiting_review" {
		return ProjectUIEvent{}, fmt.Errorf("instruction cannot be reviewed from state %s", state)
	}
	targetState := "completed"
	if decision == "request_changes" {
		targetState = "queued"
	}
	now := dashboardWorkroomTimestamp(nowFunc)
	revision++
	var terminalAt any
	if targetState == "completed" {
		terminalAt = now
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE hands_instruction SET state = ?, revision = ?, updated_at = ?, terminal_at = ?,
		       state_reason_code = NULL, state_reason_text = NULL WHERE id = ? AND project_id = ?`,
		targetState, revision, now, terminalAt, instructionID, projectID); err != nil {
		return ProjectUIEvent{}, err
	}
	if decision == "accept" {
		if _, err := tx.ExecContext(ctx, `UPDATE session SET status = 'completed', updated_at = ? WHERE id = ? AND project_id = ?`, now, sessionID, projectID); err != nil {
			return ProjectUIEvent{}, err
		}
	} else {
		if _, err := tx.ExecContext(ctx, `UPDATE session SET status = 'pending', rework_count = COALESCE(rework_count, 0) + 1, updated_at = ? WHERE id = ? AND project_id = ?`, now, sessionID, projectID); err != nil {
			return ProjectUIEvent{}, err
		}
		generation++
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO hands_generation (
				instruction_id, generation, project_id, compat_session_id, provider, model, reasoning,
				status, launch_mode, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, 'planned', 'headless', ?, ?)`,
			instructionID, generation, projectID, sessionID, provider, model, reasoning, now, now); err != nil {
			return ProjectUIEvent{}, err
		}
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO hands_instruction_event (
			instruction_id, ordinal, event_type, from_state, to_state, actor_kind, actor_id, payload_json, created_at
		) VALUES (?, (SELECT COALESCE(MAX(ordinal), 0) + 1 FROM hands_instruction_event WHERE instruction_id = ?),
		          'instruction.reviewed', 'awaiting_review', ?, 'principal', ?, ?, ?)`,
		instructionID, instructionID, targetState, principal.PrincipalID,
		fmt.Sprintf(`{"decision":%q,"review_note":%q,"generation":%d}`, decision, reviewNote, generation), now); err != nil {
		return ProjectUIEvent{}, err
	}
	return appendProjectUIEventMutationTx(ctx, tx, projectID, nowFunc, workroomEventMutation{
		Kind: "hands.instruction.changed", AggregateType: "hands_instruction", AggregateID: instructionID,
		AggregateRevision: revision,
		Payload:           map[string]any{"instruction_id": instructionID, "state": targetState, "revision": revision, "decision": decision, "updated_at": now},
		ActorKind:         "principal", ActorID: principal.PrincipalID, CausationID: causationID, CorrelationID: principal.RequestID,
	})
}
