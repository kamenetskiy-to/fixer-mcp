package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	projectWorkroomProtocol        = "fixer.genui"
	projectWorkroomProtocolVersion = 1
	maxGenUIRequestBytes           = 64 * 1024
	maxGenUIDocumentBytes          = 256 * 1024
	maxGenUIFeedbackCommentRunes   = 1000
	maxLegalResearchBytes          = 48 * 1024
	legalResearchRelativePath      = "research/legal/legal_memo.md"
)

var registeredGenUISurfaces = map[string]struct{}{
	"project.overview":    {},
	"wave.list":           {},
	"wave.detail":         {},
	"execution.review":    {},
	"backlog.list":        {},
	"backlog.item":        {},
	"docs.tree":           {},
	"docs.viewer":         {},
	"execution.list":      {},
	"execution.detail":    {},
	"skills.catalog":      {},
	"skills.detail":       {},
	"runtime.evidence":    {},
	"research.legal":      {},
	"unsupported.request": {},
}

var registeredGenUIFeedbackReasons = map[string]struct{}{
	"accurate": {},
	"helpful":  {},
	"unclear":  {},
	"stale":    {},
	"unsafe":   {},
	"other":    {},
}

var registeredProjectUIEventKinds = map[string]struct{}{
	"fixer.thread.created":             {},
	"fixer.thread.changed":             {},
	"fixer.turn.appended":              {},
	"fixer.turn.changed":               {},
	"genui.surface.presented":          {},
	"genui.surface.revised":            {},
	"genui.surface.revoked":            {},
	"genui.feedback.recorded":          {},
	"genui.demand.recorded":            {},
	"genui.action.changed":             {},
	"hands.identity.changed":           {},
	"hands.lane.changed":               {},
	"hands.instruction.created":        {},
	"hands.instruction.changed":        {},
	"hands.instruction.event_appended": {},
	"hands.generation.changed":         {},
	"lease.changed":                    {},
	"planned_wave.changed":             {},
	"wave.changed":                     {},
	"session.changed":                  {},
	"backlog.changed":                  {},
	"document.changed":                 {},
	"skill.changed":                    {},
	"project.changed":                  {},
}

type projectUIEventRecord struct {
	ProjectID         int             `json:"project_id"`
	Seq               int64           `json:"seq"`
	EventID           string          `json:"event_id"`
	SchemaVersion     int             `json:"schema_version"`
	Kind              string          `json:"kind"`
	AggregateType     string          `json:"aggregate_type"`
	AggregateID       string          `json:"aggregate_id"`
	AggregateRevision int             `json:"aggregate_revision"`
	Payload           json.RawMessage `json:"payload"`
	ActorKind         string          `json:"actor_kind"`
	ActorID           string          `json:"actor_id"`
	CausationID       string          `json:"causation_id"`
	CorrelationID     string          `json:"correlation_id"`
	CreatedAt         string          `json:"created_at"`
}

type workroomEventInput struct {
	Kind              string
	AggregateType     string
	AggregateID       string
	AggregateRevision int
	Payload           any
	ActorKind         string
	ActorID           string
	CausationID       string
	CorrelationID     string
}

func newWorkroomID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(raw[:])
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32], nil
}

func workroomTimestamp() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func hashCanonicalJSON(value any) (string, []byte, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", nil, err
	}
	hash := sha256.Sum256(payload)
	return hex.EncodeToString(hash[:]), payload, nil
}

func nextProjectUISeqTx(ctx context.Context, tx *sql.Tx, projectID int) (int64, error) {
	var seq int64
	err := tx.QueryRowContext(ctx, `
		INSERT INTO project_ui_cursor (project_id, next_seq, updated_at)
		VALUES (?, 2, ?)
		ON CONFLICT(project_id) DO UPDATE SET
			next_seq = project_ui_cursor.next_seq + 1,
			updated_at = excluded.updated_at
		RETURNING next_seq - 1`, projectID, workroomTimestamp()).Scan(&seq)
	return seq, err
}

func insertProjectUIEventAtSeqTx(ctx context.Context, tx *sql.Tx, projectID int, seq int64, input workroomEventInput) (projectUIEventRecord, error) {
	if _, ok := registeredProjectUIEventKinds[input.Kind]; !ok {
		return projectUIEventRecord{}, fmt.Errorf("unregistered project UI event kind %q", input.Kind)
	}
	if input.AggregateRevision < 1 {
		return projectUIEventRecord{}, fmt.Errorf("aggregate revision must be positive")
	}
	payload, err := json.Marshal(input.Payload)
	if err != nil {
		return projectUIEventRecord{}, fmt.Errorf("encode event payload: %w", err)
	}
	if len(payload) > maxGenUIRequestBytes {
		return projectUIEventRecord{}, fmt.Errorf("event payload exceeds %d bytes", maxGenUIRequestBytes)
	}
	eventID, err := newWorkroomID()
	if err != nil {
		return projectUIEventRecord{}, fmt.Errorf("create event id: %w", err)
	}
	createdAt := workroomTimestamp()
	if input.CausationID == "" {
		input.CausationID = eventID
	}
	if input.CorrelationID == "" {
		input.CorrelationID = input.CausationID
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO project_ui_event (
			project_id, seq, event_id, schema_version, kind, aggregate_type,
			aggregate_id, aggregate_revision, payload_json, actor_kind, actor_id,
			causation_id, correlation_id, created_at
		) VALUES (?, ?, ?, 1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		projectID, seq, eventID, input.Kind, input.AggregateType, input.AggregateID,
		input.AggregateRevision, string(payload), input.ActorKind, input.ActorID,
		input.CausationID, input.CorrelationID, createdAt)
	if err != nil {
		return projectUIEventRecord{}, err
	}
	return projectUIEventRecord{
		ProjectID:         projectID,
		Seq:               seq,
		EventID:           eventID,
		SchemaVersion:     projectWorkroomProtocolVersion,
		Kind:              input.Kind,
		AggregateType:     input.AggregateType,
		AggregateID:       input.AggregateID,
		AggregateRevision: input.AggregateRevision,
		Payload:           payload,
		ActorKind:         input.ActorKind,
		ActorID:           input.ActorID,
		CausationID:       input.CausationID,
		CorrelationID:     input.CorrelationID,
		CreatedAt:         createdAt,
	}, nil
}

func appendProjectUIEventTx(ctx context.Context, tx *sql.Tx, projectID int, input workroomEventInput) (projectUIEventRecord, error) {
	seq, err := nextProjectUISeqTx(ctx, tx, projectID)
	if err != nil {
		return projectUIEventRecord{}, err
	}
	return insertProjectUIEventAtSeqTx(ctx, tx, projectID, seq, input)
}

func workroomPrincipalID() string {
	return fmt.Sprintf("%s:%d", authorizedRole, authorizedProjectId)
}

func validateIdempotencyKey(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 200 || !utf8.ValidString(value) {
		return "", fmt.Errorf("idempotency_key must contain 1..200 valid UTF-8 bytes")
	}
	return value, nil
}

func loadCommandDedupTx(ctx context.Context, tx *sql.Tx, projectID int, principalID, commandKind, key, requestHash string, output any) (bool, error) {
	now := workroomTimestamp()
	if _, err := tx.ExecContext(ctx, `DELETE FROM command_dedup WHERE expires_at <= ?`, now); err != nil {
		return false, err
	}
	var storedHash, resultJSON string
	err := tx.QueryRowContext(ctx, `
		SELECT request_hash, result_json
		FROM command_dedup
		WHERE project_id = ? AND principal_id = ? AND command_kind = ? AND idempotency_key = ?`,
		projectID, principalID, commandKind, key).Scan(&storedHash, &resultJSON)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if storedHash != requestHash {
		return false, fmt.Errorf("idempotency_conflict")
	}
	if err := json.Unmarshal([]byte(resultJSON), output); err != nil {
		return false, fmt.Errorf("decode stored command result: %w", err)
	}
	return true, nil
}

func storeCommandDedupTx(ctx context.Context, tx *sql.Tx, projectID int, principalID, commandKind, key, requestHash string, output any) error {
	resultJSON, err := json.Marshal(output)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO command_dedup (
			project_id, principal_id, command_kind, idempotency_key, request_hash,
			result_json, created_at, expires_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		projectID, principalID, commandKind, key, requestHash, string(resultJSON),
		now.Format(time.RFC3339Nano), now.Add(7*24*time.Hour).Format(time.RFC3339Nano))
	return err
}

func ensureDefaultFixerContextTx(ctx context.Context, tx *sql.Tx, projectID int, requestedThreadID, requestedTurnID, actorID, causationID, correlationID string) (string, string, error) {
	threadID := strings.TrimSpace(requestedThreadID)
	turnID := strings.TrimSpace(requestedTurnID)
	if threadID != "" {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM fixer_thread WHERE id = ? AND project_id = ?`, threadID, projectID).Scan(&count); err != nil {
			return "", "", err
		}
		if count != 1 {
			return "", "", fmt.Errorf("fixer context not found")
		}
	} else {
		if err := tx.QueryRowContext(ctx, `
			SELECT id FROM fixer_thread
			WHERE project_id = ? AND state = 'active'
			ORDER BY updated_at DESC, id DESC LIMIT 1`, projectID).Scan(&threadID); err != nil && err != sql.ErrNoRows {
			return "", "", err
		}
		if threadID == "" {
			var err error
			threadID, err = newWorkroomID()
			if err != nil {
				return "", "", err
			}
			now := workroomTimestamp()
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO fixer_thread (id, project_id, provider, headline, state, created_at, updated_at)
				VALUES (?, ?, 'fixer', 'Project Workroom', 'active', ?, ?)`, threadID, projectID, now, now); err != nil {
				return "", "", err
			}
			if _, err := appendProjectUIEventTx(ctx, tx, projectID, workroomEventInput{
				Kind: "fixer.thread.created", AggregateType: "fixer_thread", AggregateID: threadID,
				AggregateRevision: 1, Payload: map[string]any{"id": threadID, "headline": "Project Workroom", "state": "active"},
				ActorKind: "system", ActorID: actorID, CausationID: causationID, CorrelationID: correlationID,
			}); err != nil {
				return "", "", err
			}
		}
	}

	if turnID != "" {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM fixer_turn WHERE id = ? AND project_id = ? AND thread_id = ?`, turnID, projectID, threadID).Scan(&count); err != nil {
			return "", "", err
		}
		if count != 1 {
			return "", "", fmt.Errorf("fixer context not found")
		}
		return threadID, turnID, nil
	}
	if err := tx.QueryRowContext(ctx, `
		SELECT id FROM fixer_turn WHERE project_id = ? AND thread_id = ?
		ORDER BY ordinal DESC LIMIT 1`, projectID, threadID).Scan(&turnID); err != nil && err != sql.ErrNoRows {
		return "", "", err
	}
	if turnID != "" {
		return threadID, turnID, nil
	}
	var err error
	turnID, err = newWorkroomID()
	if err != nil {
		return "", "", err
	}
	now := workroomTimestamp()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO fixer_turn (
			id, project_id, thread_id, ordinal, role, content, status, created_at, completed_at
		) VALUES (?, ?, ?, 1, 'system', 'Project Workroom surface context', 'complete', ?, ?)`,
		turnID, projectID, threadID, now, now); err != nil {
		return "", "", err
	}
	if _, err := appendProjectUIEventTx(ctx, tx, projectID, workroomEventInput{
		Kind: "fixer.turn.appended", AggregateType: "fixer_turn", AggregateID: turnID,
		AggregateRevision: 1, Payload: map[string]any{"id": turnID, "thread_id": threadID, "ordinal": 1, "role": "system", "status": "complete"},
		ActorKind: "system", ActorID: actorID, CausationID: causationID, CorrelationID: correlationID,
	}); err != nil {
		return "", "", err
	}
	return threadID, turnID, nil
}

type RequestGenUISurfaceInput struct {
	ThreadID       string         `json:"thread_id,omitempty" jsonschema:"Optional project-owned Fixer thread. The current Workroom thread is used when omitted."`
	CausedByTurnID string         `json:"caused_by_turn_id,omitempty" jsonschema:"Optional project-owned causative Fixer turn."`
	SurfaceType    string         `json:"surface_type" jsonschema:"Registered surface type without a version suffix, for example project.overview or wave.detail."`
	SurfaceVersion int            `json:"surface_version" jsonschema:"Registered surface version. Project Workroom v1 supports version 1."`
	Arguments      map[string]any `json:"arguments,omitempty" jsonschema:"Typed closed-object arguments for the registered surface."`
	Provider       string         `json:"provider,omitempty" jsonschema:"Provider that requested the surface; used for governed demand evidence."`
	Model          string         `json:"model,omitempty" jsonschema:"Model that requested the surface; used for governed demand evidence."`
	IdempotencyKey string         `json:"idempotency_key" jsonschema:"Stable caller-generated idempotency key."`
}

type RequestGenUISurfaceOutput struct {
	Status          string          `json:"status"`
	SurfaceID       string          `json:"surface_id,omitempty"`
	SurfaceRevision int             `json:"surface_revision,omitempty"`
	ProjectSeq      int64           `json:"project_seq"`
	Document        json.RawMessage `json:"document,omitempty"`
	DemandExampleID string          `json:"demand_example_id,omitempty"`
	RejectionCode   string          `json:"rejection_code,omitempty"`
}

func canonicalSurfaceType(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	return strings.TrimSuffix(value, ".v1")
}

func integerArgument(value any) (int64, bool) {
	switch typed := value.(type) {
	case int:
		return int64(typed), true
	case int64:
		return typed, true
	case float64:
		if math.Trunc(typed) == typed && typed <= math.MaxInt64 && typed >= math.MinInt64 {
			return int64(typed), true
		}
	case json.Number:
		parsed, err := typed.Int64()
		return parsed, err == nil
	}
	return 0, false
}

func validateClosedKeys(arguments map[string]any, allowed ...string) error {
	allowlist := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		allowlist[key] = struct{}{}
	}
	for key := range arguments {
		if _, ok := allowlist[key]; !ok {
			return fmt.Errorf("unknown argument %q", key)
		}
	}
	return nil
}

func workroomContainsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func validateGenUISurfaceArguments(surfaceType string, arguments map[string]any) error {
	if arguments == nil {
		arguments = map[string]any{}
	}
	switch surfaceType {
	case "project.overview", "skills.catalog", "research.legal":
		return validateClosedKeys(arguments)
	case "wave.list":
		if err := validateClosedKeys(arguments, "filter"); err != nil {
			return err
		}
		if raw, ok := arguments["filter"]; ok {
			value, ok := raw.(string)
			if !ok || !workroomContainsString([]string{"all", "active", "review_ready", "terminal"}, value) {
				return fmt.Errorf("filter is not registered")
			}
		}
	case "backlog.list":
		if err := validateClosedKeys(arguments, "status"); err != nil {
			return err
		}
		if raw, ok := arguments["status"]; ok {
			value, ok := raw.(string)
			if !ok || !workroomContainsString([]string{"all", "open", "in_progress", "done", "cancelled"}, value) {
				return fmt.Errorf("status is not registered")
			}
		}
	case "docs.tree":
		if err := validateClosedKeys(arguments, "level"); err != nil {
			return err
		}
		if raw, ok := arguments["level"]; ok {
			value, valid := integerArgument(raw)
			if !valid || value < 0 || value > 3 {
				return fmt.Errorf("level must be an integer from 0 through 3")
			}
		}
	case "execution.list":
		if err := validateClosedKeys(arguments, "status", "source"); err != nil {
			return err
		}
		if raw, ok := arguments["status"]; ok {
			value, ok := raw.(string)
			if !ok || !workroomContainsString([]string{"all", "pending", "in_progress", "review", "completed"}, value) {
				return fmt.Errorf("status is not registered")
			}
		}
		if raw, ok := arguments["source"]; ok {
			value, ok := raw.(string)
			if !ok || !workroomContainsString([]string{"all", "netrunner", "hands_instruction"}, value) {
				return fmt.Errorf("source is not registered")
			}
		}
	case "wave.detail":
		return requirePositiveIntegerArgument(arguments, "wave_id")
	case "execution.review", "execution.detail":
		return requirePositiveIntegerArgument(arguments, "session_id")
	case "docs.viewer":
		return requirePositiveIntegerArgument(arguments, "project_doc_id")
	case "backlog.item":
		return requireStringArgument(arguments, "item_id")
	case "skills.detail":
		return requireStringArgument(arguments, "skill_id")
	case "unsupported.request":
		return requireStringArgument(arguments, "demand_example_id")
	case "runtime.evidence":
		if err := validateClosedKeys(arguments, "ref_kind", "ref_id"); err != nil {
			return err
		}
		kind, ok := arguments["ref_kind"].(string)
		if !ok || !workroomContainsString([]string{"process", "generation"}, kind) {
			return fmt.Errorf("ref_kind is not registered")
		}
		return requireStringArgumentWithAllowedKeys(arguments, "ref_id", "ref_kind", "ref_id")
	default:
		return fmt.Errorf("surface_type_unsupported")
	}
	return nil
}

func requirePositiveIntegerArgument(arguments map[string]any, key string) error {
	if err := validateClosedKeys(arguments, key); err != nil {
		return err
	}
	value, ok := integerArgument(arguments[key])
	if !ok || value <= 0 {
		return fmt.Errorf("%s must be a positive integer", key)
	}
	return nil
}

func requireStringArgument(arguments map[string]any, key string) error {
	return requireStringArgumentWithAllowedKeys(arguments, key, key)
}

func requireStringArgumentWithAllowedKeys(arguments map[string]any, key string, allowed ...string) error {
	if err := validateClosedKeys(arguments, allowed...); err != nil {
		return err
	}
	value, ok := arguments[key].(string)
	if !ok || strings.TrimSpace(value) == "" || len(value) > 512 {
		return fmt.Errorf("%s must be a non-empty bounded string", key)
	}
	return nil
}

func recordGenUIDemandTx(ctx context.Context, tx *sql.Tx, projectID int, threadID, turnID, requestedType string, requestedVersion int, provider, model, rejectionCode string, requestJSON []byte, actorID, causationID, correlationID string) (string, int64, error) {
	demandID, err := newWorkroomID()
	if err != nil {
		return "", 0, err
	}
	if len(requestJSON) > maxGenUIRequestBytes {
		requestJSON = []byte(`{"redacted":"request_too_large"}`)
	}
	if provider = strings.TrimSpace(provider); provider == "" {
		provider = "unknown"
	}
	if model = strings.TrimSpace(model); model == "" {
		model = "unknown"
	}
	var nullableVersion any
	if requestedVersion > 0 {
		nullableVersion = requestedVersion
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO genui_demand_example (
			id, project_id, thread_id, turn_id, requested_type, requested_version,
			request_json, provider, model, rejection_code, privacy_class, triage_state, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'project_internal', 'new', ?)`,
		demandID, projectID, threadID, turnID, requestedType, nullableVersion, string(requestJSON),
		provider, model, rejectionCode, workroomTimestamp())
	if err != nil {
		return "", 0, err
	}
	event, err := appendProjectUIEventTx(ctx, tx, projectID, workroomEventInput{
		Kind: "genui.demand.recorded", AggregateType: "genui_demand", AggregateID: demandID,
		AggregateRevision: 1,
		Payload:           map[string]any{"id": demandID, "requested_type": requestedType, "requested_version": nullableVersion, "rejection_code": rejectionCode, "triage_state": "new"},
		ActorKind:         "fixer", ActorID: actorID, CausationID: causationID, CorrelationID: correlationID,
	})
	return demandID, event.Seq, err
}

func renderRegisteredGenUISurfaceTx(ctx context.Context, tx *sql.Tx, projectID int, surfaceID, surfaceType string, arguments map[string]any, sourceSeq int64, generatedAt string) (map[string]any, error) {
	title := strings.ReplaceAll(surfaceType, ".", " ")
	components := []map[string]any{}
	addCallout := func(tone, heading, body string) {
		components = append(components, map[string]any{"kind": "callout", "id": "summary", "tone": tone, "title": heading, "body": body})
	}

	switch surfaceType {
	case "research.legal":
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
		var active int
		if err := tx.QueryRowContext(ctx, `SELECT name, COALESCE(active, 1) FROM project WHERE id = ?`, projectID).Scan(&name, &active); err != nil {
			return nil, err
		}
		title = name
		components = append(components,
			map[string]any{"kind": "status_badge", "id": "project-state", "label": map[bool]string{true: "active", false: "passive"}[active != 0], "tone": map[bool]string{true: "success", false: "neutral"}[active != 0]},
			map[string]any{"kind": "key_value", "id": "project-identity", "rows": []map[string]string{{"label": "Project", "value": name}, {"label": "Project ID", "value": fmt.Sprint(projectID)}}},
		)
	case "wave.detail":
		waveID, _ := integerArgument(arguments["wave_id"])
		var status, phase, gate string
		err := tx.QueryRowContext(ctx, `SELECT status, COALESCE(phase, ''), COALESCE(gate_state, '') FROM parallel_wave WHERE id = ? AND project_id = ?`, waveID, projectID).Scan(&status, &phase, &gate)
		if err == sql.ErrNoRows {
			addCallout("warning", "Wave unavailable", "The requested wave is not available in this project.")
		} else if err != nil {
			return nil, err
		} else {
			title = fmt.Sprintf("Wave %d", waveID)
			components = append(components, map[string]any{"kind": "key_value", "id": "wave-state", "rows": []map[string]string{{"label": "Status", "value": status}, {"label": "Phase", "value": phase}, {"label": "Gate", "value": gate}}})
		}
	case "execution.detail", "execution.review":
		sessionID, _ := integerArgument(arguments["session_id"])
		var task, status, kind string
		err := tx.QueryRowContext(ctx, `SELECT task_description, status, COALESCE(session_kind, 'netrunner') FROM session WHERE id = ? AND project_id = ?`, sessionID, projectID).Scan(&task, &status, &kind)
		if err == sql.ErrNoRows {
			addCallout("warning", "Execution unavailable", "The requested execution is not available in this project.")
		} else if err != nil {
			return nil, err
		} else {
			title = fmt.Sprintf("Execution %d", sessionID)
			components = append(components,
				map[string]any{"kind": "status_badge", "id": "execution-state", "label": status, "tone": "info"},
				map[string]any{"kind": "key_value", "id": "execution-identity", "rows": []map[string]string{{"label": "Source", "value": kind}, {"label": "Task", "value": truncateRunes(task, 200)}}},
			)
		}
	case "docs.viewer":
		docID, _ := integerArgument(arguments["project_doc_id"])
		var name, content string
		err := tx.QueryRowContext(ctx, `SELECT name, content FROM project_doc WHERE id = ? AND project_id = ?`, docID, projectID).Scan(&name, &content)
		if err == sql.ErrNoRows {
			addCallout("warning", "Document unavailable", "The requested document is not available in this project.")
		} else if err != nil {
			return nil, err
		} else {
			title = name
			components = append(components, map[string]any{"kind": "markdown", "id": "document", "source": truncateRunes(content, 48000)})
		}
	case "unsupported.request":
		demandID, _ := arguments["demand_example_id"].(string)
		var requestedType, rejectionCode string
		err := tx.QueryRowContext(ctx, `SELECT COALESCE(requested_type, ''), rejection_code FROM genui_demand_example WHERE id = ? AND project_id = ?`, demandID, projectID).Scan(&requestedType, &rejectionCode)
		if err == sql.ErrNoRows {
			addCallout("warning", "Request unavailable", "The demand record is not available in this project.")
		} else if err != nil {
			return nil, err
		} else {
			title = "Unsupported request"
			addCallout("warning", "Surface not registered", fmt.Sprintf("%s was rejected safely (%s).", requestedType, rejectionCode))
		}
	default:
		addCallout("info", title, "This registered project view is materialized from the authoritative control-plane read model.")
	}

	actions := []map[string]any{
		{
			"ref": "feedback", "action_id": "genui.feedback.submit", "action_version": 1,
			"label": "Surface feedback", "target": map[string]string{"type": "genui_surface", "id": surfaceID},
			"enabled": true, "confirmation": "none", "input_schema": "feedback.v1",
		},
	}
	components = append(components, map[string]any{"kind": "action_group", "id": "feedback-actions", "action_refs": []string{"feedback"}})
	document := map[string]any{
		"protocol": projectWorkroomProtocol, "protocol_version": projectWorkroomProtocolVersion,
		"instance_id": surfaceID, "surface_type": surfaceType, "surface_version": 1,
		"project_id": projectID, "revision": 1, "source_seq": sourceSeq, "title": title,
		"generated_at": generatedAt, "components": components, "actions": actions,
	}
	if err := validateMaterializedSurfaceDocument(document); err != nil {
		return nil, err
	}
	return document, nil
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
			return "", legalResearchRelativePath, false, nil
		}
		return "", "", false, err
	}
	candidate := filepath.Join(realRoot, filepath.FromSlash(legalResearchRelativePath))
	realCandidate, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		if os.IsNotExist(err) {
			return "", legalResearchRelativePath, false, nil
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
			return "", legalResearchRelativePath, false, nil
		}
		return "", "", false, err
	}
	defer file.Close()
	payload, err := io.ReadAll(io.LimitReader(file, maxLegalResearchBytes+1))
	if err != nil {
		return "", "", false, err
	}
	if len(payload) > maxLegalResearchBytes || !utf8.Valid(payload) {
		return "", "", false, fmt.Errorf("legal research document is invalid or exceeds %d bytes", maxLegalResearchBytes)
	}
	content := strings.TrimSpace(string(payload))
	if content == "" {
		return "", filepath.ToSlash(relative), false, nil
	}
	return content, filepath.ToSlash(relative), true, nil
}

func validateMaterializedSurfaceDocument(document map[string]any) error {
	encoded, err := json.Marshal(document)
	if err != nil {
		return err
	}
	if len(encoded) > maxGenUIDocumentBytes {
		return fmt.Errorf("surface document exceeds %d bytes", maxGenUIDocumentBytes)
	}
	components, ok := document["components"].([]map[string]any)
	if !ok || len(components) == 0 || len(components) > 64 {
		return fmt.Errorf("surface document component count is invalid")
	}
	allowedKinds := map[string]struct{}{"text": {}, "markdown": {}, "status_badge": {}, "metric": {}, "key_value": {}, "data_table": {}, "timeline": {}, "callout": {}, "action_group": {}, "divider": {}}
	ids := map[string]struct{}{}
	for _, component := range components {
		kind, kindOK := component["kind"].(string)
		id, idOK := component["id"].(string)
		if !kindOK || !idOK {
			return fmt.Errorf("surface component discriminator or id is invalid")
		}
		if _, ok := allowedKinds[kind]; !ok {
			return fmt.Errorf("surface component kind %q is not registered", kind)
		}
		if _, duplicate := ids[id]; duplicate || id == "" {
			return fmt.Errorf("surface component id %q is invalid or duplicated", id)
		}
		ids[id] = struct{}{}
	}
	actions, ok := document["actions"].([]map[string]any)
	if !ok || len(actions) > 32 {
		return fmt.Errorf("surface document action count is invalid")
	}
	return nil
}

func RequestGenUISurface(ctx context.Context, req *mcp.CallToolRequest, input RequestGenUISurfaceInput) (*mcp.CallToolResult, RequestGenUISurfaceOutput, error) {
	if authorizedRole != "fixer" {
		return &mcp.CallToolResult{IsError: true}, RequestGenUISurfaceOutput{}, fmt.Errorf("access denied: requires fixer role")
	}
	idempotencyKey, err := validateIdempotencyKey(input.IdempotencyKey)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, RequestGenUISurfaceOutput{}, err
	}
	input.SurfaceType = canonicalSurfaceType(input.SurfaceType)
	if input.Arguments == nil {
		input.Arguments = map[string]any{}
	}
	requestHash, requestJSON, err := hashCanonicalJSON(input)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, RequestGenUISurfaceOutput{}, fmt.Errorf("encode request: %v", err)
	}
	if len(requestJSON) > maxGenUIRequestBytes {
		return &mcp.CallToolResult{IsError: true}, RequestGenUISurfaceOutput{}, fmt.Errorf("request exceeds %d bytes", maxGenUIRequestBytes)
	}
	principalID := workroomPrincipalID()
	causationID, err := newWorkroomID()
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, RequestGenUISurfaceOutput{}, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, RequestGenUISurfaceOutput{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var output RequestGenUISurfaceOutput
	if replayed, err := loadCommandDedupTx(ctx, tx, authorizedProjectId, principalID, "genui.surface.request", idempotencyKey, requestHash, &output); err != nil {
		return &mcp.CallToolResult{IsError: true}, RequestGenUISurfaceOutput{}, err
	} else if replayed {
		return nil, output, nil
	}
	threadID, turnID, err := ensureDefaultFixerContextTx(ctx, tx, authorizedProjectId, input.ThreadID, input.CausedByTurnID, principalID, causationID, causationID)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, RequestGenUISurfaceOutput{}, err
	}

	rejectionCode := ""
	if _, ok := registeredGenUISurfaces[input.SurfaceType]; !ok {
		rejectionCode = "surface_type_unsupported"
	} else if input.SurfaceVersion != projectWorkroomProtocolVersion {
		rejectionCode = "surface_version_unsupported"
	} else if err := validateGenUISurfaceArguments(input.SurfaceType, input.Arguments); err != nil {
		rejectionCode = "arguments_invalid"
	}
	if rejectionCode != "" {
		demandID, seq, err := recordGenUIDemandTx(ctx, tx, authorizedProjectId, threadID, turnID, input.SurfaceType, input.SurfaceVersion, input.Provider, input.Model, rejectionCode, requestJSON, principalID, causationID, causationID)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, RequestGenUISurfaceOutput{}, err
		}
		output = RequestGenUISurfaceOutput{Status: "rejected", ProjectSeq: seq, DemandExampleID: demandID, RejectionCode: rejectionCode}
		if err := storeCommandDedupTx(ctx, tx, authorizedProjectId, principalID, "genui.surface.request", idempotencyKey, requestHash, output); err != nil {
			return &mcp.CallToolResult{IsError: true}, RequestGenUISurfaceOutput{}, err
		}
		if err := tx.Commit(); err != nil {
			return &mcp.CallToolResult{IsError: true}, RequestGenUISurfaceOutput{}, err
		}
		return nil, output, nil
	}

	surfaceID, err := newWorkroomID()
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, RequestGenUISurfaceOutput{}, err
	}
	seq, err := nextProjectUISeqTx(ctx, tx, authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, RequestGenUISurfaceOutput{}, err
	}
	now := workroomTimestamp()
	document, err := renderRegisteredGenUISurfaceTx(ctx, tx, authorizedProjectId, surfaceID, input.SurfaceType, input.Arguments, seq, now)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, RequestGenUISurfaceOutput{}, fmt.Errorf("materialize surface: %v", err)
	}
	documentHash, documentJSON, err := hashCanonicalJSON(document)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, RequestGenUISurfaceOutput{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO genui_surface_instance (
			id, project_id, thread_id, caused_by_turn_id, surface_type, surface_version,
			state, current_revision, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, 1, 'presented', 1, ?, ?)`,
		surfaceID, authorizedProjectId, threadID, turnID, input.SurfaceType, now, now); err != nil {
		return &mcp.CallToolResult{IsError: true}, RequestGenUISurfaceOutput{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO genui_surface_revision (
			surface_id, revision, source_seq, document_json, document_hash, renderer_version, created_at
		) VALUES (?, 1, ?, ?, ?, 'workroom-v1', ?)`, surfaceID, seq, string(documentJSON), documentHash, now); err != nil {
		return &mcp.CallToolResult{IsError: true}, RequestGenUISurfaceOutput{}, err
	}
	if _, err := insertProjectUIEventAtSeqTx(ctx, tx, authorizedProjectId, seq, workroomEventInput{
		Kind: "genui.surface.presented", AggregateType: "genui_surface", AggregateID: surfaceID, AggregateRevision: 1,
		Payload:   map[string]any{"id": surfaceID, "thread_id": threadID, "caused_by_turn_id": turnID, "state": "presented", "current_revision": 1, "document": document},
		ActorKind: "fixer", ActorID: principalID, CausationID: causationID, CorrelationID: causationID,
	}); err != nil {
		return &mcp.CallToolResult{IsError: true}, RequestGenUISurfaceOutput{}, err
	}
	output = RequestGenUISurfaceOutput{Status: "presented", SurfaceID: surfaceID, SurfaceRevision: 1, ProjectSeq: seq, Document: documentJSON}
	if err := storeCommandDedupTx(ctx, tx, authorizedProjectId, principalID, "genui.surface.request", idempotencyKey, requestHash, output); err != nil {
		return &mcp.CallToolResult{IsError: true}, RequestGenUISurfaceOutput{}, err
	}
	if err := tx.Commit(); err != nil {
		return &mcp.CallToolResult{IsError: true}, RequestGenUISurfaceOutput{}, err
	}
	return nil, output, nil
}

type GetGenUISurfaceInput struct {
	SurfaceID string `json:"surface_id" jsonschema:"Project-owned surface instance ID."`
	Revision  int    `json:"revision,omitempty" jsonschema:"Immutable revision to read. Defaults to the current revision."`
}

type GetGenUISurfaceOutput struct {
	SurfaceID       string          `json:"surface_id"`
	SurfaceType     string          `json:"surface_type"`
	SurfaceVersion  int             `json:"surface_version"`
	State           string          `json:"state"`
	CurrentRevision int             `json:"current_revision"`
	Revision        int             `json:"revision"`
	SourceSeq       int64           `json:"source_seq"`
	Document        json.RawMessage `json:"document"`
}

func GetGenUISurface(ctx context.Context, req *mcp.CallToolRequest, input GetGenUISurfaceInput) (*mcp.CallToolResult, GetGenUISurfaceOutput, error) {
	if authorizedRole != "fixer" && authorizedRole != "netrunner" && authorizedRole != "overseer" {
		return &mcp.CallToolResult{IsError: true}, GetGenUISurfaceOutput{}, fmt.Errorf("access denied: requires authenticated project role")
	}
	revision := input.Revision
	if revision < 0 {
		return &mcp.CallToolResult{IsError: true}, GetGenUISurfaceOutput{}, fmt.Errorf("revision cannot be negative")
	}
	var output GetGenUISurfaceOutput
	var documentJSON string
	err := db.QueryRowContext(ctx, `
		SELECT instance.id, instance.surface_type, instance.surface_version, instance.state,
		       instance.current_revision, revision.revision, revision.source_seq, revision.document_json
		FROM genui_surface_instance instance
		JOIN genui_surface_revision revision
		  ON revision.surface_id = instance.id
		 AND revision.revision = CASE WHEN ? = 0 THEN instance.current_revision ELSE ? END
		WHERE instance.id = ? AND instance.project_id = ?`,
		revision, revision, strings.TrimSpace(input.SurfaceID), authorizedProjectId).Scan(
		&output.SurfaceID, &output.SurfaceType, &output.SurfaceVersion, &output.State,
		&output.CurrentRevision, &output.Revision, &output.SourceSeq, &documentJSON)
	if err == sql.ErrNoRows {
		return &mcp.CallToolResult{IsError: true}, GetGenUISurfaceOutput{}, fmt.Errorf("surface not found in current project")
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetGenUISurfaceOutput{}, err
	}
	output.Document = json.RawMessage(documentJSON)
	return nil, output, nil
}

type SubmitGenUIFeedbackInput struct {
	SurfaceID      string `json:"surface_id" jsonschema:"Project-owned surface instance ID."`
	Revision       int    `json:"revision" jsonschema:"Immutable surface revision being rated."`
	Vote           int    `json:"vote" jsonschema:"Exactly 1 for thumbs-up or -1 for thumbs-down."`
	ReasonCode     string `json:"reason_code,omitempty" jsonschema:"Optional registered reason code."`
	Comment        string `json:"comment,omitempty" jsonschema:"Optional project-internal comment of at most 1000 characters."`
	IdempotencyKey string `json:"idempotency_key" jsonschema:"Stable caller-generated idempotency key."`
}

type SubmitGenUIFeedbackOutput struct {
	Status     string `json:"status"`
	SurfaceID  string `json:"surface_id"`
	Revision   int    `json:"revision"`
	Vote       int    `json:"vote"`
	ProjectSeq int64  `json:"project_seq"`
}

func SubmitGenUIFeedback(ctx context.Context, req *mcp.CallToolRequest, input SubmitGenUIFeedbackInput) (*mcp.CallToolResult, SubmitGenUIFeedbackOutput, error) {
	if authorizedRole != "fixer" {
		return &mcp.CallToolResult{IsError: true}, SubmitGenUIFeedbackOutput{}, fmt.Errorf("access denied: requires fixer role")
	}
	if input.Vote != -1 && input.Vote != 1 {
		return &mcp.CallToolResult{IsError: true}, SubmitGenUIFeedbackOutput{}, fmt.Errorf("vote must be exactly -1 or 1")
	}
	input.ReasonCode = strings.TrimSpace(strings.ToLower(input.ReasonCode))
	if input.ReasonCode != "" {
		if _, ok := registeredGenUIFeedbackReasons[input.ReasonCode]; !ok {
			return &mcp.CallToolResult{IsError: true}, SubmitGenUIFeedbackOutput{}, fmt.Errorf("reason_code is not registered")
		}
	}
	if utf8.RuneCountInString(input.Comment) > maxGenUIFeedbackCommentRunes || !utf8.ValidString(input.Comment) {
		return &mcp.CallToolResult{IsError: true}, SubmitGenUIFeedbackOutput{}, fmt.Errorf("comment must contain at most %d valid UTF-8 characters", maxGenUIFeedbackCommentRunes)
	}
	idempotencyKey, err := validateIdempotencyKey(input.IdempotencyKey)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SubmitGenUIFeedbackOutput{}, err
	}
	requestHash, _, err := hashCanonicalJSON(input)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SubmitGenUIFeedbackOutput{}, err
	}
	principalID := workroomPrincipalID()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SubmitGenUIFeedbackOutput{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var output SubmitGenUIFeedbackOutput
	if replayed, err := loadCommandDedupTx(ctx, tx, authorizedProjectId, principalID, "genui.feedback.submit", idempotencyKey, requestHash, &output); err != nil {
		return &mcp.CallToolResult{IsError: true}, SubmitGenUIFeedbackOutput{}, err
	} else if replayed {
		return nil, output, nil
	}
	var count int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM genui_surface_instance instance
		JOIN genui_surface_revision revision ON revision.surface_id = instance.id
		WHERE instance.id = ? AND instance.project_id = ? AND revision.revision = ?`,
		strings.TrimSpace(input.SurfaceID), authorizedProjectId, input.Revision).Scan(&count); err != nil {
		return &mcp.CallToolResult{IsError: true}, SubmitGenUIFeedbackOutput{}, err
	}
	if count != 1 {
		return &mcp.CallToolResult{IsError: true}, SubmitGenUIFeedbackOutput{}, fmt.Errorf("surface revision not found in current project")
	}
	now := workroomTimestamp()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO genui_surface_feedback (
			surface_id, revision, principal_id, vote, reason_code, comment, created_at, updated_at
		) VALUES (?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), ?, ?)
		ON CONFLICT(surface_id, revision, principal_id) DO UPDATE SET
			vote = excluded.vote,
			reason_code = excluded.reason_code,
			comment = excluded.comment,
			updated_at = excluded.updated_at`,
		input.SurfaceID, input.Revision, principalID, input.Vote, input.ReasonCode,
		input.Comment, now, now); err != nil {
		return &mcp.CallToolResult{IsError: true}, SubmitGenUIFeedbackOutput{}, err
	}
	causationID, err := newWorkroomID()
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SubmitGenUIFeedbackOutput{}, err
	}
	event, err := appendProjectUIEventTx(ctx, tx, authorizedProjectId, workroomEventInput{
		Kind: "genui.feedback.recorded", AggregateType: "genui_feedback",
		AggregateID:       input.SurfaceID + ":" + fmt.Sprint(input.Revision) + ":" + principalID,
		AggregateRevision: input.Revision,
		Payload:           map[string]any{"surface_id": input.SurfaceID, "revision": input.Revision, "principal_id": principalID, "vote": input.Vote, "reason_code": input.ReasonCode, "updated_at": now},
		ActorKind:         "principal", ActorID: principalID, CausationID: causationID, CorrelationID: causationID,
	})
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SubmitGenUIFeedbackOutput{}, err
	}
	output = SubmitGenUIFeedbackOutput{Status: "recorded", SurfaceID: input.SurfaceID, Revision: input.Revision, Vote: input.Vote, ProjectSeq: event.Seq}
	if err := storeCommandDedupTx(ctx, tx, authorizedProjectId, principalID, "genui.feedback.submit", idempotencyKey, requestHash, output); err != nil {
		return &mcp.CallToolResult{IsError: true}, SubmitGenUIFeedbackOutput{}, err
	}
	if err := tx.Commit(); err != nil {
		return &mcp.CallToolResult{IsError: true}, SubmitGenUIFeedbackOutput{}, err
	}
	return nil, output, nil
}

func sortedRegisteredGenUISurfaces() []string {
	values := make([]string, 0, len(registeredGenUISurfaces))
	for value := range registeredGenUISurfaces {
		values = append(values, value)
	}
	sort.Strings(values)
	return values
}
