package dashboardapi

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const maxNetrunnerThreadMessages = 400

// NetrunnerThreadMessage is the provider-neutral message shape consumed by
// the detail UI. Tool calls and raw provider events are intentionally omitted.
type NetrunnerThreadMessage struct {
	ID        string `json:"id"`
	Role      string `json:"role"`
	Text      string `json:"text"`
	CreatedAt string `json:"created_at,omitempty"`
	Source    string `json:"source"`
}

type NetrunnerContinuationCapability struct {
	Supported bool   `json:"supported"`
	Mode      string `json:"mode"`
	Reason    string `json:"reason,omitempty"`
}

// NetrunnerThreadResponse keeps provider linkage separate from the generic
// session summary so an unlaunched manual task is not mislabeled as Codex.
type NetrunnerThreadResponse struct {
	SessionID              int                             `json:"session_id"`
	LocalID                int                             `json:"local_id"`
	ProjectID              int                             `json:"project_id"`
	Status                 string                          `json:"status"`
	Backend                string                          `json:"backend,omitempty"`
	Model                  string                          `json:"model,omitempty"`
	Reasoning              string                          `json:"reasoning,omitempty"`
	ExternalSessionID      string                          `json:"external_session_id,omitempty"`
	LaunchState            string                          `json:"launch_state"`
	TranscriptAvailability string                          `json:"transcript_availability"`
	TranscriptPath         string                          `json:"transcript_path,omitempty"`
	Messages               []NetrunnerThreadMessage        `json:"messages"`
	Continuation           NetrunnerContinuationCapability `json:"continuation"`
}

type ContinueNetrunnerThreadInput struct {
	Message string `json:"message"`
}

type ContinueNetrunnerThreadResponse struct {
	Status            string `json:"status"`
	SessionID         int    `json:"session_id"`
	Backend           string `json:"backend"`
	ExternalSessionID string `json:"external_session_id"`
	ProcessID         int    `json:"process_id,omitempty"`
	Message           string `json:"message"`
}

type netrunnerThreadSession struct {
	SessionID  int
	LocalID    int
	ProjectID  int
	ProjectCWD string
	Status     string
	Backend    string
	Model      string
	Reasoning  string
}

func (r *Repository) NetrunnerThread(ctx context.Context, sessionID int) (NetrunnerThreadResponse, error) {
	session, err := r.loadNetrunnerThreadSession(ctx, sessionID)
	if err != nil {
		return NetrunnerThreadResponse{}, err
	}
	backend, externalID, err := r.loadNetrunnerExternalLink(ctx, sessionID, session.Backend)
	if err != nil {
		return NetrunnerThreadResponse{}, err
	}
	if session.Backend != "" {
		backend = normalizeNetrunnerThreadBackend(session.Backend)
	}

	response := NetrunnerThreadResponse{
		SessionID:              session.SessionID,
		LocalID:                session.LocalID,
		ProjectID:              session.ProjectID,
		Status:                 session.Status,
		Backend:                backend,
		Model:                  session.Model,
		Reasoning:              session.Reasoning,
		ExternalSessionID:      externalID,
		Messages:               []NetrunnerThreadMessage{},
		LaunchState:            netrunnerLaunchState(backend, externalID, session.Status),
		TranscriptAvailability: "unavailable",
		Continuation:           netrunnerContinuationCapability(backend, externalID),
	}

	if externalID == "" {
		return response, nil
	}
	availability, path, messages := loadNetrunnerProviderTranscript(backend, externalID, session.ProjectCWD)
	response.TranscriptAvailability = availability
	response.TranscriptPath = path
	response.Messages = messages
	return response, nil
}

func (r *Repository) loadNetrunnerThreadSession(ctx context.Context, sessionID int) (netrunnerThreadSession, error) {
	var session netrunnerThreadSession
	err := r.db.QueryRowContext(ctx, `
		SELECT
			s.id,
			(SELECT COUNT(*) FROM session scoped WHERE scoped.project_id = s.project_id AND scoped.id <= s.id),
			s.project_id,
			p.cwd,
			COALESCE(s.status, ''),
			COALESCE(s.cli_backend, ''),
			COALESCE(s.cli_model, ''),
			COALESCE(s.cli_reasoning, '')
		FROM session s
		JOIN project p ON p.id = s.project_id
		WHERE s.id = ?`, sessionID).Scan(
		&session.SessionID,
		&session.LocalID,
		&session.ProjectID,
		&session.ProjectCWD,
		&session.Status,
		&session.Backend,
		&session.Model,
		&session.Reasoning,
	)
	if err != nil {
		return netrunnerThreadSession{}, err
	}
	session.Backend = normalizeNetrunnerThreadBackend(session.Backend)
	return session, nil
}

func (r *Repository) loadNetrunnerExternalLink(ctx context.Context, sessionID int, preferredBackend string) (string, string, error) {
	if !r.tableExists(ctx, "session_external_link") {
		return normalizeNetrunnerThreadBackend(preferredBackend), "", nil
	}
	preferredBackend = normalizeNetrunnerThreadBackend(preferredBackend)
	if preferredBackend != "" {
		var backend, externalID string
		err := r.db.QueryRowContext(ctx, `
			SELECT COALESCE(backend, ''), COALESCE(external_session_id, '')
			FROM session_external_link
			WHERE session_id = ? AND LOWER(REPLACE(backend, '_', '-')) = ?
			ORDER BY updated_at DESC, rowid DESC
			LIMIT 1`, sessionID, preferredBackend).Scan(&backend, &externalID)
		if err == nil {
			return normalizeNetrunnerThreadBackend(backend), strings.TrimSpace(externalID), nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return "", "", err
		}
	}
	var backend, externalID string
	err := r.db.QueryRowContext(ctx, `
		SELECT COALESCE(backend, ''), COALESCE(external_session_id, '')
		FROM session_external_link
		WHERE session_id = ? AND TRIM(COALESCE(external_session_id, '')) != ''
		ORDER BY updated_at DESC, rowid DESC
		LIMIT 1`, sessionID).Scan(&backend, &externalID)
	if errors.Is(err, sql.ErrNoRows) {
		return preferredBackend, "", nil
	}
	if err != nil {
		return "", "", err
	}
	return normalizeNetrunnerThreadBackend(backend), strings.TrimSpace(externalID), nil
}

func normalizeNetrunnerThreadBackend(backend string) string {
	normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(backend), "_", "-"))
	switch normalized {
	case "agy":
		return "antigravity"
	case "factory":
		return "droid"
	case "claude-code":
		return "claude"
	case "kimi", "kimi-cli":
		return "kimi-code"
	default:
		return normalized
	}
}

func netrunnerLaunchState(backend string, externalID string, status string) string {
	if strings.TrimSpace(backend) == "" {
		return "awaiting_backend"
	}
	if strings.TrimSpace(externalID) != "" {
		return "linked"
	}
	if strings.TrimSpace(status) == "pending" {
		return "unlaunched"
	}
	return "linkage_missing"
}

func netrunnerContinuationCapability(backend string, externalID string) NetrunnerContinuationCapability {
	if backend == "" {
		return NetrunnerContinuationCapability{Mode: "awaiting_backend", Reason: "Choose and launch a backend before opening this manual Netrunner thread."}
	}
	if externalID == "" {
		return NetrunnerContinuationCapability{Mode: "unavailable", Reason: "The provider session has no stored external-session linkage yet."}
	}
	switch backend {
	case "codex", "commandcode", "droid":
		return NetrunnerContinuationCapability{Supported: true, Mode: "headless_resume"}
	case "claude":
		return NetrunnerContinuationCapability{Mode: "unsupported", Reason: "Claude resume needs a scoped MCP runtime config that the dashboard does not yet materialize."}
	case "kimi-code":
		return NetrunnerContinuationCapability{Mode: "unsupported", Reason: "Kimi resume needs its project-scoped MCP config file; direct dashboard continuation is not wired yet."}
	case "antigravity":
		return NetrunnerContinuationCapability{Mode: "unsupported", Reason: "Antigravity conversation metadata is linked, but safe dashboard continuation is not wired yet."}
	case "junie":
		return NetrunnerContinuationCapability{Mode: "unsupported", Reason: "Junie session metadata is linked, but dashboard continuation is not implemented yet."}
	default:
		return NetrunnerContinuationCapability{Mode: "unsupported", Reason: fmt.Sprintf("Provider %q has no dashboard continuation adapter.", backend)}
	}
}

func loadNetrunnerProviderTranscript(backend string, externalID string, projectCWD string) (string, string, []NetrunnerThreadMessage) {
	var root string
	switch backend {
	case "codex", "commandcode":
		root = strings.TrimSpace(os.Getenv("FIXER_CODEX_SESSION_ROOT"))
		if root == "" {
			if codexHome := strings.TrimSpace(os.Getenv("CODEX_HOME")); codexHome != "" {
				root = filepath.Join(codexHome, "sessions")
			} else if home, err := os.UserHomeDir(); err == nil {
				root = filepath.Join(home, ".codex", "sessions")
			}
		}
	case "droid":
		root = strings.TrimSpace(os.Getenv("FIXER_DROID_SESSION_ROOT"))
		if root == "" {
			if home, err := os.UserHomeDir(); err == nil {
				root = filepath.Join(home, ".factory", "sessions")
			}
		}
	default:
		return "metadata_only", "", []NetrunnerThreadMessage{}
	}

	path := findNetrunnerTranscript(root, externalID, projectCWD)
	if path == "" {
		return "missing", "", []NetrunnerThreadMessage{}
	}
	messages := parseNetrunnerTranscript(path, backend)
	if len(messages) == 0 {
		return "empty", path, messages
	}
	return "available", path, messages
}

func findNetrunnerTranscript(root string, externalID string, projectCWD string) string {
	if root == "" || externalID == "" {
		return ""
	}
	type candidate struct {
		path    string
		exact   bool
		modTime int64
	}
	candidates := []candidate{}
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			return nil
		}
		name := strings.TrimSuffix(entry.Name(), ".jsonl")
		if name != externalID && !strings.Contains(name, externalID) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		projectMatch := projectCWD == "" || strings.Contains(filepath.Clean(path), filepath.Base(filepath.Clean(projectCWD)))
		candidates = append(candidates, candidate{path: path, exact: name == externalID || projectMatch, modTime: info.ModTime().UnixNano()})
		return nil
	})
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].exact != candidates[j].exact {
			return candidates[i].exact
		}
		return candidates[i].modTime > candidates[j].modTime
	})
	if len(candidates) == 0 {
		return ""
	}
	return candidates[0].path
}

func parseNetrunnerTranscript(path string, backend string) []NetrunnerThreadMessage {
	file, err := os.Open(path)
	if err != nil {
		return []NetrunnerThreadMessage{}
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	messages := []NetrunnerThreadMessage{}
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		var payload map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &payload); err != nil {
			continue
		}
		message, ok := netrunnerMessageFromPayload(payload, backend, lineNumber)
		if !ok {
			continue
		}
		if len(messages) > 0 && messages[len(messages)-1].Role == message.Role && messages[len(messages)-1].Text == message.Text {
			continue
		}
		messages = append(messages, message)
	}
	if len(messages) > maxNetrunnerThreadMessages {
		messages = messages[len(messages)-maxNetrunnerThreadMessages:]
	}
	return messages
}

func netrunnerMessageFromPayload(payload map[string]any, backend string, lineNumber int) (NetrunnerThreadMessage, bool) {
	record := payload
	if nested, ok := payload["payload"].(map[string]any); ok {
		record = nested
	}
	recordType := strings.TrimSpace(fmt.Sprint(record["type"]))
	role := normalizeNetrunnerMessageRole(record["role"], recordType)
	if role == "" {
		return NetrunnerThreadMessage{}, false
	}
	text := providerMessageText(record["content"])
	if text == "" {
		text = providerMessageText(record["message"])
	}
	if text == "" {
		text = providerMessageText(record["text"])
	}
	if text == "" {
		return NetrunnerThreadMessage{}, false
	}
	id := strings.TrimSpace(fmt.Sprint(record["id"]))
	if id == "" || id == "<nil>" {
		id = fmt.Sprintf("%s-%d", backend, lineNumber)
	}
	createdAt := strings.TrimSpace(fmt.Sprint(payload["timestamp"]))
	if createdAt == "<nil>" {
		createdAt = ""
	}
	source := "provider_transcript"
	if backend == "claude" {
		message, _ := record["message"].(map[string]any)
		content, _ := message["content"].([]any)
		isToolResult := false
		for _, item := range content {
			block, _ := item.(map[string]any)
			if block["type"] == "tool_result" {
				isToolResult = true
				break
			}
		}
		toolUseResult, _ := record["toolUseResult"].(map[string]any)
		commandName, hasCommandName := toolUseResult["commandName"].(string)
		origin, _ := record["origin"].(map[string]any)
		originKind, _ := origin["kind"].(string)
		switch {
		case record["isCompactSummary"] == true:
			source = "provider_transcript:compaction"
		case hasCommandName && strings.TrimSpace(commandName) != "":
			source = "provider_transcript:skill_activation"
		case isToolResult || record["sourceToolAssistantUUID"] != nil || record["toolUseResult"] != nil:
			source = "provider_transcript:tool_result"
		case record["interruptedMessageId"] != nil:
			source = "provider_transcript:interruption"
		case record["isApiErrorMessage"] == true:
			source = "provider_transcript:api_error"
		case originKind == "task-notification":
			source = "provider_transcript:task_notification"
		case strings.TrimSpace(fmt.Sprint(message["stop_reason"])) == "tool_use":
			source = "provider_transcript:assistant_progress"
		case record["isMeta"] == true || record["isVisibleInTranscriptOnly"] == true:
			source = "provider_transcript:meta"
		case role == "user" && originKind != "human":
			source = "provider_transcript:command"
		}
	}
	return NetrunnerThreadMessage{ID: id, Role: role, Text: text, CreatedAt: createdAt, Source: source}, true
}

func normalizeNetrunnerMessageRole(raw any, recordType string) string {
	role := strings.ToLower(strings.TrimSpace(fmt.Sprint(raw)))
	switch role {
	case "assistant", "agent", "model":
		return "assistant"
	case "user", "human":
		return "user"
	}
	recordType = strings.ToLower(recordType)
	if strings.Contains(recordType, "assistant") || strings.Contains(recordType, "agent") {
		return "assistant"
	}
	if strings.Contains(recordType, "user") || strings.Contains(recordType, "human") {
		return "user"
	}
	return ""
}

func providerMessageText(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case []any:
		parts := []string{}
		for _, item := range typed {
			if text := providerMessageText(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.TrimSpace(strings.Join(parts, "\n"))
	case map[string]any:
		for _, key := range []string{"text", "message", "content", "output_text", "input_text", "value"} {
			if text := providerMessageText(typed[key]); text != "" {
				return text
			}
		}
	}
	return ""
}

var startNetrunnerContinuation = func(ctx context.Context, cwd string, command []string) (int, error) {
	if len(command) == 0 {
		return 0, fmt.Errorf("continuation command is empty")
	}
	// The HTTP request ends immediately after the provider process is started;
	// tying the child to that request context would cancel every follow-up.
	_ = ctx
	cmd := exec.Command(command[0], command[1:]...)
	cmd.Dir = cwd
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	pid := cmd.Process.Pid
	go func() { _ = cmd.Wait() }()
	return pid, nil
}

func (r *Repository) ContinueNetrunnerThread(ctx context.Context, sessionID int, input ContinueNetrunnerThreadInput) (ContinueNetrunnerThreadResponse, error) {
	message := strings.TrimSpace(input.Message)
	if message == "" {
		return ContinueNetrunnerThreadResponse{}, fmt.Errorf("message is required")
	}
	if len(message) > 32*1024 {
		return ContinueNetrunnerThreadResponse{}, fmt.Errorf("message exceeds 32768 bytes")
	}
	thread, err := r.NetrunnerThread(ctx, sessionID)
	if err != nil {
		return ContinueNetrunnerThreadResponse{}, err
	}
	if !thread.Continuation.Supported {
		return ContinueNetrunnerThreadResponse{}, fmt.Errorf("provider continuation is unavailable: %s", thread.Continuation.Reason)
	}
	session, err := r.loadNetrunnerThreadSession(ctx, sessionID)
	if err != nil {
		return ContinueNetrunnerThreadResponse{}, err
	}
	command, err := netrunnerContinuationCommand(thread, message)
	if err != nil {
		return ContinueNetrunnerThreadResponse{}, err
	}
	pid, err := startNetrunnerContinuation(ctx, session.ProjectCWD, command)
	if err != nil {
		return ContinueNetrunnerThreadResponse{}, fmt.Errorf("start %s continuation: %w", thread.Backend, err)
	}
	return ContinueNetrunnerThreadResponse{
		Status:            "started",
		SessionID:         sessionID,
		Backend:           thread.Backend,
		ExternalSessionID: thread.ExternalSessionID,
		ProcessID:         pid,
		Message:           "Follow-up submitted to the linked provider thread.",
	}, nil
}

func netrunnerContinuationCommand(thread NetrunnerThreadResponse, message string) ([]string, error) {
	switch thread.Backend {
	case "codex", "commandcode":
		return []string{"codex", "exec", "resume", thread.ExternalSessionID, message}, nil
	case "droid":
		command := []string{"droid", "exec", "-s", thread.ExternalSessionID}
		if thread.Model != "" {
			command = append(command, "-m", thread.Model)
		}
		if thread.Reasoning != "" {
			command = append(command, "-r", thread.Reasoning)
		}
		return append(command, "--output-format", "json", message), nil
	default:
		return nil, fmt.Errorf("provider %q has no continuation command", thread.Backend)
	}
}
