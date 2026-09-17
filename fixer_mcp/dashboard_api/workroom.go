package dashboardapi

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	workroomProtocolVersion      = 1
	workroomBridgeSecretEnv      = "FIXER_WORKROOM_BRIDGE_SECRET"
	workroomBridgeServiceEnv     = "FIXER_WORKROOM_BRIDGE_SERVICE_ID"
	workroomBridgeDefaultService = "serverpod"
	workroomMaxBridgeBodyBytes   = 128 * 1024
	workroomMaxEventPayloadBytes = 64 * 1024
	workroomMaxEventBatch        = 100
	workroomMaxWait              = 25 * time.Second
	workroomCrossProcessProbe    = 250 * time.Millisecond
)

var ErrWorkroomAuthorization = errors.New("workroom authorization denied")

const (
	bridgeServiceHeader   = "X-Workroom-Service"
	bridgePrincipalHeader = "X-Workroom-Principal"
	bridgeRolesHeader     = "X-Workroom-Roles"
	bridgeProjectHeader   = "X-Workroom-Project-ID"
	bridgeRequestIDHeader = "X-Workroom-Request-ID"
	bridgeIssuedAtHeader  = "X-Workroom-Issued-At"
	bridgeExpiresAtHeader = "X-Workroom-Expires-At"
	bridgeBodyHashHeader  = "X-Workroom-Body-SHA256"
	bridgeSignatureHeader = "X-Workroom-Signature"
)

type projectEventNotifier struct {
	mu        sync.Mutex
	nextID    uint64
	listeners map[int]map[uint64]chan struct{}
}

func newProjectEventNotifier() *projectEventNotifier {
	return &projectEventNotifier{listeners: map[int]map[uint64]chan struct{}{}}
}

func (n *projectEventNotifier) subscribe(projectID int) (<-chan struct{}, func()) {
	n.mu.Lock()
	n.nextID++
	id := n.nextID
	if n.listeners[projectID] == nil {
		n.listeners[projectID] = map[uint64]chan struct{}{}
	}
	ch := make(chan struct{}, 1)
	n.listeners[projectID][id] = ch
	n.mu.Unlock()
	return ch, func() {
		n.mu.Lock()
		delete(n.listeners[projectID], id)
		if len(n.listeners[projectID]) == 0 {
			delete(n.listeners, projectID)
		}
		n.mu.Unlock()
	}
}

func (n *projectEventNotifier) notify(projectID int) {
	if n == nil {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, ch := range n.listeners[projectID] {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

type BridgePrincipal struct {
	ServiceID   string   `json:"service_id"`
	PrincipalID string   `json:"principal_id"`
	Roles       []string `json:"roles"`
	ProjectID   int      `json:"project_id"`
	RequestID   string   `json:"request_id"`
	IssuedAt    int64    `json:"issued_at"`
	ExpiresAt   int64    `json:"expires_at"`
}

type ProjectUIEvent struct {
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

type ProjectUIEventsResponse struct {
	ProjectID int              `json:"project_id"`
	AfterSeq  int64            `json:"after_seq"`
	HeadSeq   int64            `json:"head_seq"`
	Events    []ProjectUIEvent `json:"events"`
	TimedOut  bool             `json:"timed_out"`
}

type WorkroomFixerThread struct {
	ID                string `json:"id"`
	Provider          string `json:"provider"`
	ExternalSessionID string `json:"external_session_id,omitempty"`
	Headline          string `json:"headline"`
	State             string `json:"state"`
	CreatedAt         string `json:"created_at"`
	UpdatedAt         string `json:"updated_at"`
	Model             string `json:"model,omitempty"`
	Reasoning         string `json:"reasoning,omitempty"`
}

type WorkroomFixerTurn struct {
	ID              string `json:"id"`
	ThreadID        string `json:"thread_id"`
	Ordinal         int    `json:"ordinal"`
	Role            string `json:"role"`
	Content         string `json:"content"`
	Source          string `json:"source,omitempty"`
	Status          string `json:"status"`
	ClientMessageID string `json:"client_message_id,omitempty"`
	ProviderTurnID  string `json:"provider_turn_id,omitempty"`
	CreatedAt       string `json:"created_at"`
	CompletedAt     string `json:"completed_at,omitempty"`
}

type WorkroomSurface struct {
	ID              string          `json:"id"`
	ThreadID        string          `json:"thread_id"`
	CausedByTurnID  string          `json:"caused_by_turn_id"`
	SurfaceType     string          `json:"surface_type"`
	SurfaceVersion  int             `json:"surface_version"`
	State           string          `json:"state"`
	CurrentRevision int             `json:"current_revision"`
	SourceSeq       int64           `json:"source_seq"`
	Document        json.RawMessage `json:"document"`
	UpdatedAt       string          `json:"updated_at"`
}

type WorkroomHandsLane struct {
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	Reasoning string `json:"reasoning"`
}

type dashboardHandsProviderSpec struct {
	defaultModel     string
	defaultReasoning string
	modelOptions     []string
	reasoningOptions []string
}

var dashboardHandsProviderSpecs = map[string]dashboardHandsProviderSpec{
	"codex": {
		defaultModel:     "gpt-5.6-luna",
		defaultReasoning: "high",
		modelOptions: []string{
			"gpt-5.6-sol",
			"gpt-5.6-terra",
			"gpt-5.6-luna",
			"gpt-5.5",
			"gpt-5.4",
			"gpt-5.4-mini",
			"gpt-5.3-codex",
			"gpt-5.3-codex-spark",
			"gpt-5.2",
			"opencode-go/glm-5.3-flash",
			"deepseek/deepseek-v4-flash-0731",
			"deepseek-v4-flash",
			"deepseek/deepseek-v4-pro-0813",
		},
		reasoningOptions: []string{"low", "medium", "high", "xhigh", "max", "ultra"},
	},
	"commandcode": {
		defaultModel:     "commandcode/zai-org/glm-5.3-flash",
		defaultReasoning: "medium",
		modelOptions: []string{
			"commandcode/meta/muse-spark-1.2-contributor",
			"commandcode/deepseek/deepseek-v4-flash",
			"commandcode/deepseek/deepseek-v4-flash-vision-exp",
			"commandcode/deepseek/deepseek-v4-pro",
			"commandcode/zai-org/glm-5.3-flash",
			"commandcode/gpt-5.6-sol",
			"commandcode/minimaxai/minimax-m3",
			"commandcode/moonshotai/kimi-k3",
			"commandcode/qwen/qwen3.8-27b",
			"commandcode/qwen/qwen3.8-max",
		},
		reasoningOptions: []string{"low", "medium", "high"},
	},
	"claude": {
		defaultModel:     "sonnet",
		defaultReasoning: "high",
		modelOptions:     []string{"sonnet", "opus"},
		reasoningOptions: []string{"low", "medium", "high", "xhigh", "max"},
	},
	"kimi-code": {
		defaultModel:     "kimi-k3-256k",
		defaultReasoning: "default",
		modelOptions: []string{
			"kimi-k2.7-code",
			"kimi-k2.7-code-highspeed",
			"kimi-k3",
			"kimi-k3-256k",
		},
		reasoningOptions: []string{"default"},
	},
	"antigravity": {
		defaultModel:     "Gemini 3.6 Flash",
		defaultReasoning: "high",
		modelOptions: []string{
			"Gemini 3.6 Flash",
			"Gemini 3.1 Pro",
			"Claude Sonnet 4.6 (Thinking)",
			"Claude Opus 4.6 (Thinking)",
		},
		reasoningOptions: []string{"default", "low", "medium", "high"},
	},
}

func dashboardHandsProviderSpecFor(provider string) (dashboardHandsProviderSpec, bool) {
	spec, ok := dashboardHandsProviderSpecs[provider]
	return spec, ok
}

func dashboardHandsProviderConfig(provider string) (model string, reasoning string, ok bool) {
	if spec, ok := dashboardHandsProviderSpecFor(provider); ok {
		return spec.defaultModel, spec.defaultReasoning, true
	}
	return "", "", false
}

func dashboardHandsProviderOptionContains(options []string, value string) bool {
	for _, option := range options {
		if option == value {
			return true
		}
	}
	return false
}

type WorkroomHandsInstruction struct {
	ID                 string   `json:"id"`
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

type ProjectWorkroomSnapshot struct {
	ProjectID           int                        `json:"project_id"`
	ProjectName         string                     `json:"project_name"`
	ProjectCwd          string                     `json:"project_cwd"`
	ProtocolVersion     int                        `json:"protocol_version"`
	WatermarkSeq        int64                      `json:"watermark_seq"`
	Threads             []WorkroomFixerThread      `json:"threads"`
	SelectedThreadID    string                     `json:"selected_thread_id,omitempty"`
	Turns               []WorkroomFixerTurn        `json:"turns"`
	ActiveSurface       *WorkroomSurface           `json:"active_surface,omitempty"`
	HandsActorID        string                     `json:"hands_actor_id"`
	HandsDisplayName    string                     `json:"hands_display_name"`
	HandsAuthorityState string                     `json:"hands_authority_state"`
	HandsDefaultLane    string                     `json:"hands_default_lane"`
	HandsLanes          []WorkroomHandsLane        `json:"hands_lanes"`
	HandsMailbox        []WorkroomHandsInstruction `json:"hands_mailbox"`
	ActiveInstruction   *WorkroomHandsInstruction  `json:"active_instruction,omitempty"`
	Capabilities        []string                   `json:"capabilities"`
}

func bridgeRoles(raw string) []string {
	seen := map[string]struct{}{}
	roles := []string{}
	for _, value := range strings.Split(raw, ",") {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		roles = append(roles, value)
	}
	sort.Strings(roles)
	return roles
}

func bridgeCanonicalRequest(method, path, rawQuery, serviceID, principalID string, roles []string, projectID int, requestID string, issuedAt, expiresAt int64, bodyHash string) string {
	return strings.Join([]string{
		strings.ToUpper(method), path, rawQuery, serviceID, principalID, strings.Join(roles, ","),
		strconv.Itoa(projectID), requestID, strconv.FormatInt(issuedAt, 10),
		strconv.FormatInt(expiresAt, 10), bodyHash,
	}, "\n")
}

func bridgeHasProjectMembership(roles []string) bool {
	for _, role := range roles {
		switch role {
		case "project_member", "fixer", "architect", "admin", "overseer":
			return true
		}
	}
	return false
}

func (s *Server) authorizeWorkroomBridge(r *http.Request, projectID int, body []byte) (BridgePrincipal, error) {
	secret := strings.TrimSpace(os.Getenv(workroomBridgeSecretEnv))
	if secret == "" {
		return BridgePrincipal{}, fmt.Errorf("workroom bridge credential is not configured")
	}
	expectedService := strings.TrimSpace(os.Getenv(workroomBridgeServiceEnv))
	if expectedService == "" {
		expectedService = workroomBridgeDefaultService
	}
	serviceID := strings.TrimSpace(r.Header.Get(bridgeServiceHeader))
	principalID := strings.TrimSpace(r.Header.Get(bridgePrincipalHeader))
	requestID := strings.TrimSpace(r.Header.Get(bridgeRequestIDHeader))
	roles := bridgeRoles(r.Header.Get(bridgeRolesHeader))
	headerProjectID, err := strconv.Atoi(strings.TrimSpace(r.Header.Get(bridgeProjectHeader)))
	if err != nil || headerProjectID != projectID {
		return BridgePrincipal{}, fmt.Errorf("signed project binding is invalid")
	}
	issuedAt, err := strconv.ParseInt(strings.TrimSpace(r.Header.Get(bridgeIssuedAtHeader)), 10, 64)
	if err != nil {
		return BridgePrincipal{}, fmt.Errorf("signed issued-at is invalid")
	}
	expiresAt, err := strconv.ParseInt(strings.TrimSpace(r.Header.Get(bridgeExpiresAtHeader)), 10, 64)
	if err != nil {
		return BridgePrincipal{}, fmt.Errorf("signed expiry is invalid")
	}
	now := s.repo.now().UTC().Unix()
	if issuedAt > now+30 || expiresAt < now || expiresAt <= issuedAt || expiresAt-issuedAt > 120 {
		return BridgePrincipal{}, fmt.Errorf("signed request lifetime is invalid")
	}
	if serviceID != expectedService || principalID == "" || requestID == "" || len(principalID) > 512 || len(requestID) > 512 {
		return BridgePrincipal{}, fmt.Errorf("signed service or principal identity is invalid")
	}
	if !bridgeHasProjectMembership(roles) {
		return BridgePrincipal{}, fmt.Errorf("principal is not a project member")
	}
	bodyDigest := sha256.Sum256(body)
	bodyHash := hex.EncodeToString(bodyDigest[:])
	providedBodyHash := strings.ToLower(strings.TrimSpace(r.Header.Get(bridgeBodyHashHeader)))
	if subtle.ConstantTimeCompare([]byte(bodyHash), []byte(providedBodyHash)) != 1 {
		return BridgePrincipal{}, fmt.Errorf("signed body hash is invalid")
	}
	canonical := bridgeCanonicalRequest(r.Method, r.URL.EscapedPath(), r.URL.RawQuery, serviceID, principalID, roles, projectID, requestID, issuedAt, expiresAt, bodyHash)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(canonical))
	expectedSignature := mac.Sum(nil)
	providedSignature, err := hex.DecodeString(strings.TrimSpace(r.Header.Get(bridgeSignatureHeader)))
	if err != nil || !hmac.Equal(expectedSignature, providedSignature) {
		return BridgePrincipal{}, fmt.Errorf("signed request signature is invalid")
	}
	return BridgePrincipal{
		ServiceID: serviceID, PrincipalID: principalID, Roles: roles, ProjectID: projectID,
		RequestID: requestID, IssuedAt: issuedAt, ExpiresAt: expiresAt,
	}, nil
}

func workroomCapabilities(roles []string) []string {
	capabilities := []string{"project.view", "fixer.thread.view", "genui.surface.view", "hands.view"}
	for _, role := range roles {
		switch role {
		case "project_member":
			capabilities = append(capabilities, "fixer.turn.send", "genui.surface.request", "genui.feedback.write", "hands.instruction.submit", "hands.instruction.cancel", "hands.lane.admin")
		case "fixer", "architect", "admin", "overseer":
			capabilities = append(capabilities, "fixer.turn.send", "genui.surface.request", "genui.feedback.write", "hands.instruction.submit", "hands.instruction.cancel", "hands.lane.admin", "hands.review")
		}
	}
	sort.Strings(capabilities)
	unique := capabilities[:0]
	for _, capability := range capabilities {
		if len(unique) == 0 || unique[len(unique)-1] != capability {
			unique = append(unique, capability)
		}
	}
	return unique
}

func bridgePrincipalHasCapability(principal BridgePrincipal, capability string) bool {
	for _, candidate := range workroomCapabilities(principal.Roles) {
		if candidate == capability {
			return true
		}
	}
	return false
}

func bridgePrincipalHasPrivilegedHandsRole(principal BridgePrincipal) bool {
	for _, role := range principal.Roles {
		switch role {
		case "fixer", "architect", "admin", "overseer":
			return true
		}
	}
	return false
}

func scanWorkroomInstruction(scanner interface{ Scan(...any) error }) (WorkroomHandsInstruction, error) {
	var instruction WorkroomHandsInstruction
	var scopeJSON string
	err := scanner.Scan(
		&instruction.ID, &instruction.Ordinal, &instruction.InstructionText, &scopeJSON,
		&instruction.RequestedLane, &instruction.RiskClass, &instruction.ReviewPolicy,
		&instruction.State, &instruction.StateReasonCode, &instruction.StateReasonText,
		&instruction.Revision, &instruction.CreatedAt, &instruction.UpdatedAt, &instruction.TerminalAt,
	)
	if err != nil {
		return WorkroomHandsInstruction{}, err
	}
	if err := json.Unmarshal([]byte(scopeJSON), &instruction.DeclaredWriteScope); err != nil {
		return WorkroomHandsInstruction{}, err
	}
	if instruction.DeclaredWriteScope == nil {
		instruction.DeclaredWriteScope = []string{}
	}
	return instruction, nil
}

const workroomInstructionColumns = `
	id, ordinal, instruction_text, declared_write_scope_json, requested_lane,
	risk_class, review_policy, state, COALESCE(state_reason_code, ''),
	COALESCE(state_reason_text, ''), revision, created_at, updated_at, COALESCE(terminal_at, '')`

func (r *Repository) ProjectWorkroomSnapshot(ctx context.Context, projectID int, principal BridgePrincipal) (ProjectWorkroomSnapshot, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return ProjectWorkroomSnapshot{}, err
	}
	defer func() { _ = tx.Rollback() }()
	output := ProjectWorkroomSnapshot{
		ProjectID: projectID, ProtocolVersion: workroomProtocolVersion,
		Threads: []WorkroomFixerThread{}, Turns: []WorkroomFixerTurn{},
		HandsLanes: []WorkroomHandsLane{}, HandsMailbox: []WorkroomHandsInstruction{},
		Capabilities: workroomCapabilities(principal.Roles),
	}
	if err := tx.QueryRowContext(ctx, `SELECT name, cwd FROM project WHERE id = ?`, projectID).Scan(&output.ProjectName, &output.ProjectCwd); err == sql.ErrNoRows {
		return ProjectWorkroomSnapshot{}, sql.ErrNoRows
	} else if err != nil {
		return ProjectWorkroomSnapshot{}, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(next_seq - 1, 0) FROM project_ui_cursor WHERE project_id = ?`, projectID).Scan(&output.WatermarkSeq); err != nil && err != sql.ErrNoRows {
		return ProjectWorkroomSnapshot{}, err
	}
	threadRows, err := tx.QueryContext(ctx, `
		SELECT id, provider, COALESCE(external_session_id, ''), headline, state, created_at, updated_at
		FROM fixer_thread
		WHERE project_id = ?
		  AND EXISTS (
			SELECT 1 FROM fixer_turn
			WHERE fixer_turn.thread_id = fixer_thread.id
			  AND fixer_turn.project_id = fixer_thread.project_id
			  AND fixer_turn.role IN ('user', 'fixer')
		  )
		ORDER BY updated_at DESC, id DESC LIMIT 50`, projectID)
	if err != nil {
		return ProjectWorkroomSnapshot{}, err
	}
	for threadRows.Next() {
		var thread WorkroomFixerThread
		if err := threadRows.Scan(&thread.ID, &thread.Provider, &thread.ExternalSessionID, &thread.Headline, &thread.State, &thread.CreatedAt, &thread.UpdatedAt); err != nil {
			_ = threadRows.Close()
			return ProjectWorkroomSnapshot{}, err
		}
		output.Threads = append(output.Threads, thread)
	}
	if err := threadRows.Close(); err != nil {
		return ProjectWorkroomSnapshot{}, err
	}
	if len(output.Threads) > 0 {
		output.SelectedThreadID = output.Threads[0].ID
		turnRows, err := tx.QueryContext(ctx, `
			SELECT id, thread_id, ordinal, role, content, status, COALESCE(client_message_id, ''),
			       COALESCE(provider_turn_id, ''), created_at, COALESCE(completed_at, '')
			FROM fixer_turn WHERE project_id = ? AND thread_id = ?
			ORDER BY ordinal DESC LIMIT 100`, projectID, output.SelectedThreadID)
		if err != nil {
			return ProjectWorkroomSnapshot{}, err
		}
		for turnRows.Next() {
			var turn WorkroomFixerTurn
			if err := turnRows.Scan(&turn.ID, &turn.ThreadID, &turn.Ordinal, &turn.Role, &turn.Content,
				&turn.Status, &turn.ClientMessageID, &turn.ProviderTurnID, &turn.CreatedAt, &turn.CompletedAt); err != nil {
				_ = turnRows.Close()
				return ProjectWorkroomSnapshot{}, err
			}
			output.Turns = append(output.Turns, turn)
		}
		if err := turnRows.Close(); err != nil {
			return ProjectWorkroomSnapshot{}, err
		}
		for left, right := 0, len(output.Turns)-1; left < right; left, right = left+1, right-1 {
			output.Turns[left], output.Turns[right] = output.Turns[right], output.Turns[left]
		}
		var surface WorkroomSurface
		var documentJSON string
		err = tx.QueryRowContext(ctx, `
			SELECT instance.id, instance.thread_id, instance.caused_by_turn_id, instance.surface_type,
			       instance.surface_version, instance.state, instance.current_revision,
			       revision.source_seq, revision.document_json, instance.updated_at
			FROM genui_surface_instance instance
			JOIN genui_surface_revision revision
			  ON revision.surface_id = instance.id AND revision.revision = instance.current_revision
			WHERE instance.project_id = ? AND instance.thread_id = ? AND instance.state = 'presented'
			ORDER BY instance.updated_at DESC, instance.id DESC LIMIT 1`, projectID, output.SelectedThreadID).Scan(
			&surface.ID, &surface.ThreadID, &surface.CausedByTurnID, &surface.SurfaceType,
			&surface.SurfaceVersion, &surface.State, &surface.CurrentRevision, &surface.SourceSeq,
			&documentJSON, &surface.UpdatedAt,
		)
		if err != nil && err != sql.ErrNoRows {
			return ProjectWorkroomSnapshot{}, err
		}
		if err == nil {
			surface.Document = json.RawMessage(documentJSON)
			output.ActiveSurface = &surface
		}
	}
	if err := tx.QueryRowContext(ctx, `
		SELECT actor_id, display_name, authority_state, default_lane
		FROM project_hands WHERE project_id = ?`, projectID).Scan(
		&output.HandsActorID, &output.HandsDisplayName, &output.HandsAuthorityState, &output.HandsDefaultLane); err != nil {
		return ProjectWorkroomSnapshot{}, err
	}
	for _, provider := range []string{"codex", "commandcode", "claude", "kimi-code", "antigravity"} {
		model, reasoning, _ := dashboardHandsProviderConfig(provider)
		output.HandsLanes = append(output.HandsLanes, WorkroomHandsLane{
			Provider: provider, Model: model, Reasoning: reasoning,
		})
	}
	instructionRows, err := tx.QueryContext(ctx, `SELECT `+workroomInstructionColumns+`
		FROM hands_instruction WHERE project_id = ? ORDER BY ordinal DESC LIMIT 50`, projectID)
	if err != nil {
		return ProjectWorkroomSnapshot{}, err
	}
	for instructionRows.Next() {
		instruction, err := scanWorkroomInstruction(instructionRows)
		if err != nil {
			_ = instructionRows.Close()
			return ProjectWorkroomSnapshot{}, err
		}
		output.HandsMailbox = append(output.HandsMailbox, instruction)
		if output.ActiveInstruction == nil && (instruction.State == "queued" || instruction.State == "waiting_for_lease" || instruction.State == "starting" || instruction.State == "running" || instruction.State == "awaiting_review") {
			copy := instruction
			output.ActiveInstruction = &copy
		}
	}
	if err := instructionRows.Close(); err != nil {
		return ProjectWorkroomSnapshot{}, err
	}
	if err := tx.Commit(); err != nil {
		return ProjectWorkroomSnapshot{}, err
	}
	if history, historyErr := r.FixerThreads(ctx, projectID); historyErr == nil {
		known := make(map[string]struct{}, len(output.Threads))
		byID := make(map[string]FixerThreadSummary, len(history.Threads))
		for _, thread := range output.Threads {
			known[thread.ID] = struct{}{}
		}
		for _, thread := range history.Threads {
			if thread.ExternalID != "" {
				byID[thread.ExternalID] = thread
			}
		}
		for i := range output.Threads {
			if summary, ok := byID[output.Threads[i].ID]; ok {
				if output.Threads[i].Model == "" {
					output.Threads[i].Model = summary.Model
				}
				if output.Threads[i].Reasoning == "" {
					output.Threads[i].Reasoning = summary.Reasoning
				}
			}
		}
		for _, thread := range history.Threads {
			if !thread.Transcript || thread.ExternalID == "" {
				continue
			}
			if _, exists := known[thread.ExternalID]; exists {
				continue
			}
			known[thread.ExternalID] = struct{}{}
			output.Threads = append(output.Threads, WorkroomFixerThread{
				ID: thread.ExternalID, Provider: thread.Backend,
				ExternalSessionID: thread.ExternalID, Headline: thread.Headline,
				State: thread.Status, CreatedAt: thread.StartedAt,
				UpdatedAt: thread.LastActivityAt, Model: thread.Model,
				Reasoning: thread.Reasoning,
			})
			if strings.HasSuffix(thread.SessionLogPath, ".jsonl") {
				for ordinal, message := range parseNetrunnerTranscript(thread.SessionLogPath, thread.Backend) {
					role := message.Role
					if role == "assistant" {
						role = "fixer"
					}
					if role != "user" && role != "fixer" {
						continue
					}
					output.Turns = append(output.Turns, WorkroomFixerTurn{
						ID:       fmt.Sprintf("history-%s-%s", thread.ExternalID, message.ID),
						ThreadID: thread.ExternalID, Ordinal: ordinal,
						Role: role, Content: message.Text, Source: message.Source, Status: "complete",
						CreatedAt: message.CreatedAt,
					})
				}
			}
		}
		sort.SliceStable(output.Threads, func(i, j int) bool {
			if output.Threads[i].UpdatedAt == output.Threads[j].UpdatedAt {
				return output.Threads[i].ID > output.Threads[j].ID
			}
			return output.Threads[i].UpdatedAt > output.Threads[j].UpdatedAt
		})
		if output.SelectedThreadID == "" && len(output.Threads) > 0 {
			output.SelectedThreadID = output.Threads[0].ID
		}
	}
	return output, nil
}

func (r *Repository) projectUIEvents(ctx context.Context, projectID int, afterSeq int64, limit int) (ProjectUIEventsResponse, error) {
	if afterSeq < 0 || limit < 1 || limit > workroomMaxEventBatch {
		return ProjectUIEventsResponse{}, fmt.Errorf("invalid event cursor or limit")
	}
	if _, err := r.requireProject(ctx, projectID); err != nil {
		return ProjectUIEventsResponse{}, err
	}
	output := ProjectUIEventsResponse{ProjectID: projectID, AfterSeq: afterSeq, Events: []ProjectUIEvent{}}
	if err := r.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq), 0) FROM project_ui_event WHERE project_id = ?`, projectID).Scan(&output.HeadSeq); err != nil {
		return ProjectUIEventsResponse{}, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT project_id, seq, event_id, schema_version, kind, aggregate_type, aggregate_id,
		       aggregate_revision, payload_json, actor_kind, actor_id, causation_id, correlation_id, created_at
		FROM project_ui_event
		WHERE project_id = ? AND seq > ? AND seq <= ? ORDER BY seq LIMIT ?`, projectID, afterSeq, output.HeadSeq, limit)
	if err != nil {
		return ProjectUIEventsResponse{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var event ProjectUIEvent
		var payloadJSON string
		if err := rows.Scan(
			&event.ProjectID, &event.Seq, &event.EventID, &event.SchemaVersion, &event.Kind,
			&event.AggregateType, &event.AggregateID, &event.AggregateRevision, &payloadJSON,
			&event.ActorKind, &event.ActorID, &event.CausationID, &event.CorrelationID, &event.CreatedAt,
		); err != nil {
			return ProjectUIEventsResponse{}, err
		}
		event.Payload = json.RawMessage(payloadJSON)
		output.Events = append(output.Events, event)
	}
	return output, rows.Err()
}

func (r *Repository) WaitProjectUIEvents(ctx context.Context, projectID int, afterSeq int64, limit int, wait time.Duration) (ProjectUIEventsResponse, error) {
	if wait < 0 || wait > workroomMaxWait {
		return ProjectUIEventsResponse{}, fmt.Errorf("wait duration is out of range")
	}
	read := func() (ProjectUIEventsResponse, error) {
		return r.projectUIEvents(ctx, projectID, afterSeq, limit)
	}
	output, err := read()
	if err != nil || len(output.Events) > 0 || wait == 0 {
		return output, err
	}
	wake, unsubscribe := r.workroomEvents.subscribe(projectID)
	defer unsubscribe()
	output, err = read()
	if err != nil || len(output.Events) > 0 {
		return output, err
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	probe := time.NewTicker(workroomCrossProcessProbe)
	defer probe.Stop()
	for {
		select {
		case <-ctx.Done():
			return ProjectUIEventsResponse{}, ctx.Err()
		case <-wake:
			output, err = read()
		case <-probe.C:
			// MCP lifecycle handlers may commit through another process. A
			// bounded journal probe discovers those commits without making an
			// ephemeral wake-up channel part of replay authority.
			output, err = read()
		case <-timer.C:
			output, err = read()
			output.TimedOut = err == nil && len(output.Events) == 0
			return output, err
		}
		if err != nil || len(output.Events) > 0 {
			return output, err
		}
	}
}

func newWorkroomUUID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(value[:])
	return encoded[:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:], nil
}

type workroomEventMutation struct {
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

func dashboardWorkroomTimestamp(now func() time.Time) string {
	return now().UTC().Format(time.RFC3339Nano)
}

func nextProjectUISequenceTx(ctx context.Context, tx *sql.Tx, projectID int, now func() time.Time) (int64, error) {
	var seq int64
	err := tx.QueryRowContext(ctx, `
		INSERT INTO project_ui_cursor (project_id, next_seq, updated_at)
		VALUES (?, 2, ?)
		ON CONFLICT(project_id) DO UPDATE SET
			next_seq = project_ui_cursor.next_seq + 1,
			updated_at = excluded.updated_at
		RETURNING next_seq - 1`, projectID, dashboardWorkroomTimestamp(now)).Scan(&seq)
	return seq, err
}

func appendProjectUIEventMutationTx(ctx context.Context, tx *sql.Tx, projectID int, now func() time.Time, mutation workroomEventMutation) (ProjectUIEvent, error) {
	if mutation.AggregateRevision < 1 {
		return ProjectUIEvent{}, fmt.Errorf("aggregate revision must be positive")
	}
	payloadJSON, err := json.Marshal(mutation.Payload)
	if err != nil {
		return ProjectUIEvent{}, err
	}
	if len(payloadJSON) > workroomMaxEventPayloadBytes {
		return ProjectUIEvent{}, fmt.Errorf("event payload exceeds %d bytes", workroomMaxEventPayloadBytes)
	}
	seq, err := nextProjectUISequenceTx(ctx, tx, projectID, now)
	if err != nil {
		return ProjectUIEvent{}, err
	}
	eventID, err := newWorkroomUUID()
	if err != nil {
		return ProjectUIEvent{}, err
	}
	if mutation.CausationID == "" {
		mutation.CausationID = eventID
	}
	if mutation.CorrelationID == "" {
		mutation.CorrelationID = mutation.CausationID
	}
	createdAt := dashboardWorkroomTimestamp(now)
	_, err = tx.ExecContext(ctx, `
		INSERT INTO project_ui_event (
			project_id, seq, event_id, schema_version, kind, aggregate_type, aggregate_id,
			aggregate_revision, payload_json, actor_kind, actor_id, causation_id, correlation_id, created_at
		) VALUES (?, ?, ?, 1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		projectID, seq, eventID, mutation.Kind, mutation.AggregateType, mutation.AggregateID,
		mutation.AggregateRevision, string(payloadJSON), mutation.ActorKind, mutation.ActorID,
		mutation.CausationID, mutation.CorrelationID, createdAt)
	if err != nil {
		return ProjectUIEvent{}, err
	}
	return ProjectUIEvent{
		ProjectID: projectID, Seq: seq, EventID: eventID, SchemaVersion: workroomProtocolVersion,
		Kind: mutation.Kind, AggregateType: mutation.AggregateType, AggregateID: mutation.AggregateID,
		AggregateRevision: mutation.AggregateRevision, Payload: payloadJSON, ActorKind: mutation.ActorKind,
		ActorID: mutation.ActorID, CausationID: mutation.CausationID, CorrelationID: mutation.CorrelationID,
		CreatedAt: createdAt,
	}, nil
}

func workroomRequestHash(value any) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func validateBridgeIdempotencyKey(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 200 {
		return "", fmt.Errorf("idempotency_key must contain 1..200 bytes")
	}
	return value, nil
}

func loadWorkroomDedupTx(ctx context.Context, tx *sql.Tx, projectID int, principalID, commandKind, key, requestHash string, output any, now func() time.Time) (bool, error) {
	currentTime := time.Now().UTC()
	if now != nil {
		currentTime = now().UTC()
	}
	var storedHash, resultJSON string
	err := tx.QueryRowContext(ctx, `
		SELECT request_hash, result_json FROM command_dedup
		WHERE project_id = ? AND principal_id = ? AND command_kind = ? AND idempotency_key = ?
		  AND expires_at > ?`, projectID, principalID, commandKind, key, currentTime.Format(time.RFC3339Nano)).Scan(&storedHash, &resultJSON)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if storedHash != requestHash {
		return false, fmt.Errorf("idempotency_conflict")
	}
	return true, json.Unmarshal([]byte(resultJSON), output)
}

func storeWorkroomDedupTx(ctx context.Context, tx *sql.Tx, projectID int, principalID, commandKind, key, requestHash string, output any, now func() time.Time) error {
	resultJSON, err := json.Marshal(output)
	if err != nil {
		return err
	}
	timestamp := now().UTC()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO command_dedup (
			project_id, principal_id, command_kind, idempotency_key, request_hash,
			result_json, created_at, expires_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, projectID, principalID, commandKind, key,
		requestHash, string(resultJSON), timestamp.Format(time.RFC3339Nano),
		timestamp.Add(7*24*time.Hour).Format(time.RFC3339Nano))
	return err
}
