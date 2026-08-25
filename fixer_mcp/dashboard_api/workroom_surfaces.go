package dashboardapi

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	legalResearchSurfaceType = "research.legal"
	legalResearchPath        = "research/legal/legal_memo.md"
	legalResearchMaxBytes    = 48 * 1024
)

var registeredDashboardGenUISurfaces = map[string]struct{}{
	"project.overview":       {},
	"wave.list":              {},
	"wave.detail":            {},
	"execution.review":       {},
	"backlog.list":           {},
	"backlog.item":           {},
	"docs.tree":              {},
	"docs.viewer":            {},
	"execution.list":         {},
	"execution.detail":       {},
	"skills.catalog":         {},
	"skills.detail":          {},
	"runtime.evidence":       {},
	legalResearchSurfaceType: {},
	"unsupported.request":    {},
}

type RequestGenuiSurfaceInput struct {
	ThreadID       string         `json:"thread_id,omitempty"`
	CausedByTurnID string         `json:"caused_by_turn_id,omitempty"`
	SurfaceType    string         `json:"surface_type"`
	SurfaceVersion int            `json:"surface_version"`
	Arguments      map[string]any `json:"arguments,omitempty"`
	Provider       string         `json:"provider,omitempty"`
	Model          string         `json:"model,omitempty"`
	IdempotencyKey string         `json:"idempotency_key"`
}

func canonicalDashboardSurfaceType(value string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(value)), ".v1")
}

func validateDashboardSurfaceRequest(input RequestGenuiSurfaceInput) error {
	if _, ok := registeredDashboardGenUISurfaces[input.SurfaceType]; !ok {
		return fmt.Errorf("surface_type_unsupported")
	}
	if input.SurfaceVersion != workroomProtocolVersion {
		return fmt.Errorf("surface_version_unsupported")
	}
	arguments := input.Arguments
	if arguments == nil {
		arguments = map[string]any{}
	}
	allowed := map[string][]string{
		"project.overview": {}, "skills.catalog": {}, legalResearchSurfaceType: {},
		"wave.list": {"filter"}, "wave.detail": {"wave_id"},
		"execution.review": {"session_id"}, "execution.detail": {"session_id"},
		"backlog.list": {"status"}, "backlog.item": {"item_id"},
		"docs.tree": {"level"}, "docs.viewer": {"project_doc_id"},
		"execution.list": {"status", "source"}, "skills.detail": {"skill_id"},
		"runtime.evidence":    {"ref_type", "ref_id"},
		"unsupported.request": {"demand_example_id"},
	}[input.SurfaceType]
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		allowedSet[key] = struct{}{}
	}
	for key := range arguments {
		if _, ok := allowedSet[key]; !ok {
			return fmt.Errorf("surface_arguments_invalid")
		}
	}
	switch input.SurfaceType {
	case "wave.detail":
		return requireDashboardPositiveInteger(arguments, "wave_id")
	case "execution.review", "execution.detail":
		return requireDashboardPositiveInteger(arguments, "session_id")
	case "docs.viewer":
		return requireDashboardPositiveInteger(arguments, "project_doc_id")
	case "backlog.item":
		return requireDashboardBoundedString(arguments, "item_id")
	case "skills.detail":
		return requireDashboardBoundedString(arguments, "skill_id")
	case "unsupported.request":
		return requireDashboardBoundedString(arguments, "demand_example_id")
	case "runtime.evidence":
		if err := requireDashboardBoundedString(arguments, "ref_type"); err != nil {
			return err
		}
		return requireDashboardBoundedString(arguments, "ref_id")
	}
	return nil
}

func requireDashboardPositiveInteger(arguments map[string]any, key string) error {
	value, ok := arguments[key]
	if !ok {
		return fmt.Errorf("surface_arguments_invalid")
	}
	switch typed := value.(type) {
	case int:
		if typed > 0 {
			return nil
		}
	case int64:
		if typed > 0 {
			return nil
		}
	case float64:
		if typed > 0 && math.Trunc(typed) == typed {
			return nil
		}
	}
	return fmt.Errorf("surface_arguments_invalid")
}

func requireDashboardBoundedString(arguments map[string]any, key string) error {
	value, ok := arguments[key].(string)
	if !ok || strings.TrimSpace(value) == "" || len(value) > 512 || !utf8.ValidString(value) {
		return fmt.Errorf("surface_arguments_invalid")
	}
	return nil
}

func resolveRegisteredFixerSurfaceIntent(content string) string {
	content = strings.ToLower(content)
	research := strings.Contains(content, "ресерч") || strings.Contains(content, "research") || strings.Contains(content, "исслед")
	legal := strings.Contains(content, "юрк") || strings.Contains(content, "юрид") || strings.Contains(content, "юрист") || strings.Contains(content, "legal")
	if research && legal {
		return legalResearchSurfaceType
	}
	return ""
}

func (r *Repository) RequestGenuiSurface(ctx context.Context, projectID int, principal BridgePrincipal, input RequestGenuiSurfaceInput) (GenuiActionReceipt, error) {
	if !bridgePrincipalHasCapability(principal, "genui.surface.request") {
		return GenuiActionReceipt{}, ErrWorkroomAuthorization
	}
	input.SurfaceType = canonicalDashboardSurfaceType(input.SurfaceType)
	if input.Arguments == nil {
		input.Arguments = map[string]any{}
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
	if replayed, err := loadWorkroomDedupTx(ctx, tx, projectID, principal.PrincipalID, "genui.surface.request", key, hash, &output, r.now); err != nil {
		return GenuiActionReceipt{}, err
	} else if replayed {
		return output, nil
	}
	causationID, err := newWorkroomUUID()
	if err != nil {
		return GenuiActionReceipt{}, err
	}
	threadID, causedByTurnID, err := ensureDashboardSurfaceContextTx(ctx, tx, projectID, principal, input.ThreadID, input.CausedByTurnID, causationID, r.now)
	if err != nil {
		return GenuiActionReceipt{}, err
	}
	if rejection := dashboardSurfaceRejectionCode(input); rejection != "" {
		demandID, projectSeq, err := recordDashboardGenuiDemandTx(ctx, tx, projectID, principal, threadID, causedByTurnID, input, rejection, causationID, r.now)
		if err != nil {
			return GenuiActionReceipt{}, err
		}
		output = GenuiActionReceipt{
			Status: "rejected", InvocationID: demandID, ActionID: "genui.surface.request",
			Decision: "denied", ReasonCode: rejection, ProjectSeq: projectSeq,
		}
		if err := storeWorkroomDedupTx(ctx, tx, projectID, principal.PrincipalID, "genui.surface.request", key, hash, output, r.now); err != nil {
			return GenuiActionReceipt{}, err
		}
		if err := appendWorkroomAuditMutationTx(ctx, tx, projectID, principal, "genui.surface.request", "genui_demand", demandID, "denied", "rejected", causationID,
			map[string]any{"surface_type": input.SurfaceType, "surface_version": input.SurfaceVersion, "reason_code": rejection}, r.now); err != nil {
			return GenuiActionReceipt{}, err
		}
		if err := tx.Commit(); err != nil {
			return GenuiActionReceipt{}, err
		}
		r.workroomEvents.notify(projectID)
		return output, nil
	}
	output, err = r.materializeRegisteredSurfaceTx(ctx, tx, projectID, principal, threadID, causedByTurnID, input.SurfaceType, input.Arguments, causationID)
	if err != nil {
		return GenuiActionReceipt{}, err
	}
	if err := storeWorkroomDedupTx(ctx, tx, projectID, principal.PrincipalID, "genui.surface.request", key, hash, output, r.now); err != nil {
		return GenuiActionReceipt{}, err
	}
	if err := appendWorkroomAuditMutationTx(ctx, tx, projectID, principal, "genui.surface.request", "genui_surface", output.InvocationID, "authorized", "succeeded", causationID,
		map[string]any{"surface_type": input.SurfaceType, "surface_version": input.SurfaceVersion}, r.now); err != nil {
		return GenuiActionReceipt{}, err
	}
	if err := tx.Commit(); err != nil {
		return GenuiActionReceipt{}, err
	}
	r.workroomEvents.notify(projectID)
	return output, nil
}

func dashboardSurfaceRejectionCode(input RequestGenuiSurfaceInput) string {
	if _, ok := registeredDashboardGenUISurfaces[input.SurfaceType]; !ok {
		return "surface_type_unsupported"
	}
	if input.SurfaceVersion != workroomProtocolVersion {
		return "surface_version_unsupported"
	}
	if err := validateDashboardSurfaceRequest(input); err != nil {
		return "arguments_invalid"
	}
	return ""
}

func recordDashboardGenuiDemandTx(ctx context.Context, tx *sql.Tx, projectID int, principal BridgePrincipal, threadID, turnID string, input RequestGenuiSurfaceInput, rejectionCode, causationID string, now func() time.Time) (string, int64, error) {
	demandID, err := newWorkroomUUID()
	if err != nil {
		return "", 0, err
	}
	requestJSON, err := json.Marshal(input)
	if err != nil {
		return "", 0, err
	}
	if len(requestJSON) > workroomMaxEventPayloadBytes {
		requestJSON = []byte(`{"redacted":"request_too_large"}`)
	}
	provider := strings.TrimSpace(input.Provider)
	if provider == "" {
		provider = "unknown"
	}
	model := strings.TrimSpace(input.Model)
	if model == "" {
		model = "unknown"
	}
	var requestedVersion any
	if input.SurfaceVersion > 0 {
		requestedVersion = input.SurfaceVersion
	}
	createdAt := dashboardWorkroomTimestamp(now)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO genui_demand_example (
			id, project_id, thread_id, turn_id, requested_type, requested_version,
			request_json, provider, model, rejection_code, privacy_class, triage_state, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'project_internal', 'new', ?)`,
		demandID, projectID, threadID, turnID, input.SurfaceType, requestedVersion,
		string(requestJSON), provider, model, rejectionCode, createdAt); err != nil {
		return "", 0, err
	}
	event, err := appendProjectUIEventMutationTx(ctx, tx, projectID, now, workroomEventMutation{
		Kind: "genui.demand.recorded", AggregateType: "genui_demand", AggregateID: demandID,
		AggregateRevision: 1,
		Payload:           map[string]any{"id": demandID, "requested_type": input.SurfaceType, "requested_version": requestedVersion, "rejection_code": rejectionCode, "triage_state": "new"},
		ActorKind:         "fixer", ActorID: principal.PrincipalID, CausationID: causationID, CorrelationID: principal.RequestID,
	})
	if err != nil {
		return "", 0, err
	}
	return demandID, event.Seq, nil
}

func ensureDashboardSurfaceContextTx(ctx context.Context, tx *sql.Tx, projectID int, principal BridgePrincipal, requestedThreadID, requestedTurnID, causationID string, now func() time.Time) (string, string, error) {
	threadID := strings.TrimSpace(requestedThreadID)
	turnID := strings.TrimSpace(requestedTurnID)
	if threadID == "" {
		if err := tx.QueryRowContext(ctx, `SELECT id FROM fixer_thread WHERE project_id = ? AND state = 'active' ORDER BY updated_at DESC, id DESC LIMIT 1`, projectID).Scan(&threadID); err != nil && err != sql.ErrNoRows {
			return "", "", err
		}
	}
	if threadID == "" {
		var err error
		threadID, err = newWorkroomUUID()
		if err != nil {
			return "", "", err
		}
		timestamp := dashboardWorkroomTimestamp(now)
		if _, err := tx.ExecContext(ctx, `INSERT INTO fixer_thread (id, project_id, provider, headline, state, created_at, updated_at) VALUES (?, ?, 'fixer', 'Project Workroom', 'active', ?, ?)`, threadID, projectID, timestamp, timestamp); err != nil {
			return "", "", err
		}
		if _, err := appendProjectUIEventMutationTx(ctx, tx, projectID, now, workroomEventMutation{
			Kind: "fixer.thread.created", AggregateType: "fixer_thread", AggregateID: threadID, AggregateRevision: 1,
			Payload:   map[string]any{"id": threadID, "headline": "Project Workroom", "state": "active"},
			ActorKind: "principal", ActorID: principal.PrincipalID, CausationID: causationID, CorrelationID: principal.RequestID,
		}); err != nil {
			return "", "", err
		}
	} else {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM fixer_thread WHERE id = ? AND project_id = ?`, threadID, projectID).Scan(&count); err != nil || count != 1 {
			if err == nil {
				err = sql.ErrNoRows
			}
			return "", "", err
		}
	}
	if turnID != "" {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM fixer_turn WHERE id = ? AND project_id = ? AND thread_id = ?`, turnID, projectID, threadID).Scan(&count); err != nil || count != 1 {
			if err == nil {
				err = sql.ErrNoRows
			}
			return "", "", err
		}
		return threadID, turnID, nil
	}
	if err := tx.QueryRowContext(ctx, `SELECT id FROM fixer_turn WHERE project_id = ? AND thread_id = ? ORDER BY ordinal DESC LIMIT 1`, projectID, threadID).Scan(&turnID); err != nil && err != sql.ErrNoRows {
		return "", "", err
	}
	if turnID != "" {
		return threadID, turnID, nil
	}
	turnID, err := newWorkroomUUID()
	if err != nil {
		return "", "", err
	}
	timestamp := dashboardWorkroomTimestamp(now)
	if _, err := tx.ExecContext(ctx, `INSERT INTO fixer_turn (id, project_id, thread_id, ordinal, role, content, status, created_at, completed_at) VALUES (?, ?, ?, 1, 'system', 'Project Workroom surface context', 'complete', ?, ?)`, turnID, projectID, threadID, timestamp, timestamp); err != nil {
		return "", "", err
	}
	if _, err := appendProjectUIEventMutationTx(ctx, tx, projectID, now, workroomEventMutation{
		Kind: "fixer.turn.appended", AggregateType: "fixer_turn", AggregateID: turnID, AggregateRevision: 1,
		Payload:   map[string]any{"id": turnID, "thread_id": threadID, "ordinal": 1, "role": "system", "content": "Project Workroom surface context", "status": "complete", "created_at": timestamp},
		ActorKind: "system", ActorID: "workroom", CausationID: causationID, CorrelationID: principal.RequestID,
	}); err != nil {
		return "", "", err
	}
	return threadID, turnID, nil
}

func (r *Repository) materializeRegisteredSurfaceTx(ctx context.Context, tx *sql.Tx, projectID int, principal BridgePrincipal, threadID, causedByTurnID, surfaceType string, arguments map[string]any, causationID string) (GenuiActionReceipt, error) {
	surfaceID, err := newWorkroomUUID()
	if err != nil {
		return GenuiActionReceipt{}, err
	}
	seq, err := nextProjectUISequenceTx(ctx, tx, projectID, r.now)
	if err != nil {
		return GenuiActionReceipt{}, err
	}
	timestamp := dashboardWorkroomTimestamp(r.now)
	document, err := renderDashboardSurfaceDocumentTx(ctx, tx, projectID, surfaceID, surfaceType, arguments, seq, timestamp)
	if err != nil {
		return GenuiActionReceipt{}, err
	}
	documentJSON, err := json.Marshal(document)
	if err != nil {
		return GenuiActionReceipt{}, err
	}
	if len(documentJSON) > 256*1024 {
		return GenuiActionReceipt{}, fmt.Errorf("surface_document_too_large")
	}
	digest := sha256.Sum256(documentJSON)
	if _, err := tx.ExecContext(ctx, `INSERT INTO genui_surface_instance (id, project_id, thread_id, caused_by_turn_id, surface_type, surface_version, state, current_revision, created_at, updated_at) VALUES (?, ?, ?, ?, ?, 1, 'presented', 1, ?, ?)`, surfaceID, projectID, threadID, causedByTurnID, surfaceType, timestamp, timestamp); err != nil {
		return GenuiActionReceipt{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO genui_surface_revision (surface_id, revision, source_seq, document_json, document_hash, renderer_version, created_at) VALUES (?, 1, ?, ?, ?, 'dashboard-workroom-v1', ?)`, surfaceID, seq, string(documentJSON), hex.EncodeToString(digest[:]), timestamp); err != nil {
		return GenuiActionReceipt{}, err
	}
	if _, err := insertProjectUIEventAtSequenceTx(ctx, tx, projectID, seq, r.now, workroomEventMutation{
		Kind: "genui.surface.presented", AggregateType: "genui_surface", AggregateID: surfaceID, AggregateRevision: 1,
		Payload:   map[string]any{"id": surfaceID, "thread_id": threadID, "caused_by_turn_id": causedByTurnID, "state": "presented", "current_revision": 1, "document": document},
		ActorKind: "fixer", ActorID: principal.PrincipalID, CausationID: causationID, CorrelationID: principal.RequestID,
	}); err != nil {
		return GenuiActionReceipt{}, err
	}
	return GenuiActionReceipt{Status: "succeeded", InvocationID: surfaceID, ActionID: "genui.surface.present", Decision: "authorized", ProjectSeq: seq}, nil
}

func insertProjectUIEventAtSequenceTx(ctx context.Context, tx *sql.Tx, projectID int, seq int64, now func() time.Time, mutation workroomEventMutation) (ProjectUIEvent, error) {
	payloadJSON, err := json.Marshal(mutation.Payload)
	if err != nil {
		return ProjectUIEvent{}, err
	}
	if len(payloadJSON) > workroomMaxEventPayloadBytes {
		return ProjectUIEvent{}, fmt.Errorf("event payload exceeds %d bytes", workroomMaxEventPayloadBytes)
	}
	eventID, err := newWorkroomUUID()
	if err != nil {
		return ProjectUIEvent{}, err
	}
	createdAt := dashboardWorkroomTimestamp(now)
	if mutation.CausationID == "" {
		mutation.CausationID = eventID
	}
	if mutation.CorrelationID == "" {
		mutation.CorrelationID = mutation.CausationID
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO project_ui_event (project_id, seq, event_id, schema_version, kind, aggregate_type, aggregate_id, aggregate_revision, payload_json, actor_kind, actor_id, causation_id, correlation_id, created_at) VALUES (?, ?, ?, 1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, projectID, seq, eventID, mutation.Kind, mutation.AggregateType, mutation.AggregateID, mutation.AggregateRevision, string(payloadJSON), mutation.ActorKind, mutation.ActorID, mutation.CausationID, mutation.CorrelationID, createdAt)
	if err != nil {
		return ProjectUIEvent{}, err
	}
	return ProjectUIEvent{ProjectID: projectID, Seq: seq, EventID: eventID, SchemaVersion: 1, Kind: mutation.Kind, AggregateType: mutation.AggregateType, AggregateID: mutation.AggregateID, AggregateRevision: mutation.AggregateRevision, Payload: payloadJSON, ActorKind: mutation.ActorKind, ActorID: mutation.ActorID, CausationID: mutation.CausationID, CorrelationID: mutation.CorrelationID, CreatedAt: createdAt}, nil
}

func renderDashboardSurfaceDocumentTx(ctx context.Context, tx *sql.Tx, projectID int, surfaceID, surfaceType string, _ map[string]any, sourceSeq int64, generatedAt string) (map[string]any, error) {
	title := strings.ReplaceAll(surfaceType, ".", " ")
	components := []map[string]any{}
	switch surfaceType {
	case legalResearchSurfaceType:
		title = "Юридический ресерч"
		content, source, found, err := loadProjectLegalResearchTx(ctx, tx, projectID)
		if err != nil {
			return nil, err
		}
		if found {
			components = append(components,
				map[string]any{"kind": "key_value", "id": "research-source", "rows": []map[string]string{{"label": "Источник", "value": source}, {"label": "Контур", "value": "project research / governed read-only"}}},
				map[string]any{"kind": "markdown", "id": "legal-research", "source": content},
			)
		} else {
			components = append(components, map[string]any{"kind": "callout", "id": "research-unavailable", "tone": "warning", "title": "Юридический ресерч не найден", "body": "В проекте нет канонического файла research/legal/legal_memo.md. Произвольный путь не открывался."})
		}
	case "project.overview":
		var name string
		if err := tx.QueryRowContext(ctx, `SELECT name FROM project WHERE id = ?`, projectID).Scan(&name); err != nil {
			return nil, err
		}
		title = name
		components = append(components, map[string]any{"kind": "key_value", "id": "project-identity", "rows": []map[string]string{{"label": "Project", "value": name}, {"label": "Project ID", "value": fmt.Sprint(projectID)}}})
	default:
		components = append(components, map[string]any{"kind": "callout", "id": "summary", "tone": "info", "title": title, "body": "This registered view is materialized through the governed Project Workroom read path."})
	}
	actions := []map[string]any{{
		"ref": "feedback", "action_id": "genui.feedback.submit", "action_version": 1,
		"label": "Surface feedback", "target": map[string]string{"type": "genui_surface", "id": surfaceID},
		"enabled": true, "confirmation": "none", "input_schema": "feedback.v1",
	}}
	components = append(components, map[string]any{"kind": "action_group", "id": "feedback-actions", "action_refs": []string{"feedback"}})
	return map[string]any{
		"protocol": "fixer.genui", "protocol_version": workroomProtocolVersion,
		"instance_id": surfaceID, "surface_type": surfaceType, "surface_version": 1,
		"project_id": projectID, "revision": 1, "source_seq": sourceSeq, "title": title,
		"generated_at": generatedAt, "components": components, "actions": actions,
	}, nil
}

func loadProjectLegalResearchTx(ctx context.Context, tx *sql.Tx, projectID int) (string, string, bool, error) {
	var cwd string
	if err := tx.QueryRowContext(ctx, `SELECT cwd FROM project WHERE id = ?`, projectID).Scan(&cwd); err != nil {
		return "", "", false, err
	}
	root, err := filepath.Abs(cwd)
	if err != nil {
		return "", "", false, err
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		if os.IsNotExist(err) {
			return "", legalResearchPath, false, nil
		}
		return "", "", false, err
	}
	candidate := filepath.Join(realRoot, filepath.FromSlash(legalResearchPath))
	realCandidate, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		if os.IsNotExist(err) {
			return "", legalResearchPath, false, nil
		}
		return "", "", false, err
	}
	relative, err := filepath.Rel(realRoot, realCandidate)
	if err != nil || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", "", false, fmt.Errorf("legal research path escapes the project root")
	}
	file, err := os.Open(realCandidate)
	if err != nil {
		if os.IsNotExist(err) {
			return "", legalResearchPath, false, nil
		}
		return "", "", false, err
	}
	defer file.Close()
	payload, err := io.ReadAll(io.LimitReader(file, legalResearchMaxBytes+1))
	if err != nil {
		return "", "", false, err
	}
	if len(payload) > legalResearchMaxBytes || !utf8.Valid(payload) {
		return "", "", false, fmt.Errorf("legal research document is invalid or exceeds %d bytes", legalResearchMaxBytes)
	}
	content := strings.TrimSpace(string(payload))
	if content == "" {
		return "", filepath.ToSlash(relative), false, nil
	}
	return content, filepath.ToSlash(relative), true, nil
}
