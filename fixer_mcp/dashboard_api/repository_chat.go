package dashboardapi

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func (r *Repository) FixerChatBinding(ctx context.Context, projectID int) (FixerChatBinding, error) {
	if _, err := r.requireProject(ctx, projectID); err != nil {
		return FixerChatBinding{}, err
	}
	return r.loadChatBinding(ctx, projectID, "fixer")
}

func (r *Repository) OverseerChatBinding(ctx context.Context, projectID int) (FixerChatBinding, error) {
	if _, err := r.requireProject(ctx, projectID); err != nil {
		return FixerChatBinding{}, err
	}
	return r.loadChatBinding(ctx, projectID, "overseer")
}

type codexChatSession struct {
	SessionID      string
	CWD            string
	StartedAt      string
	LastActivityAt string
	Backend        string
	Model          string
	Reasoning      string
	AgentRole      string
	BindingSource  string
	SessionLogPath string
	SessionLog     bool
	Status         string
	Headline       string
	Transcript     bool
}

func (r *Repository) loadChatBinding(ctx context.Context, projectID int, preferredRole string) (FixerChatBinding, error) {
	project, err := r.requireProject(ctx, projectID)
	if err != nil {
		return FixerChatBinding{}, err
	}

	aliasNotes := r.loadFixerResumeAliasNotes(ctx, projectID)
	activeFixerSessionID := r.loadActiveAutonomousFixerSessionID(ctx, projectID, project.CWD)
	sessions, ambiguousCount := loadCodexChatSessions(project.CWD, activeFixerSessionID, aliasNotes)

	filtered := make([]FixerChatSessionSummary, 0, len(sessions))
	for _, session := range sessions {
		if session.AgentRole != preferredRole {
			continue
		}
		filtered = append(filtered, toFixerChatSessionSummary(session))
	}

	if preferredRole == "overseer" {
		for sessionID, note := range aliasNotes {
			if sessionID == "" || containsChatSession(filtered, sessionID) {
				continue
			}
			role := roleFromAliasNote(note)
			if role != "overseer" {
				continue
			}
			filtered = append(filtered, toFixerChatSessionSummary(codexChatSession{
				SessionID:     sessionID,
				AgentRole:     role,
				BindingSource: "fixer_resume_alias",
				Status:        "resume_alias",
				Headline:      headlineForSession(role, note, sessionID),
			}))
		}
	}

	sort.SliceStable(filtered, func(i, j int) bool {
		return filtered[i].LastActivityAt > filtered[j].LastActivityAt
	})

	binding := FixerChatBinding{
		ProjectID:              projectID,
		Supported:              len(filtered) > 0,
		Sessions:               filtered,
		TranscriptAvailability: transcriptAvailability(filtered),
		ResidualRisk:           chatBindingResidualRisk(preferredRole, ambiguousCount),
	}
	if len(filtered) > 0 {
		binding.DefaultSession = &filtered[0]
	}
	if !binding.Supported {
		return placeholderFixerChatBinding(projectID, noChatBindingMessage(preferredRole)), nil
	}
	return binding, nil
}

func transcriptAvailability(sessions []FixerChatSessionSummary) string {
	for _, session := range sessions {
		if session.Transcript {
			return "codex_jsonl"
		}
	}
	return "metadata_only"
}

func (r *Repository) loadFixerResumeAliasNotes(ctx context.Context, projectID int) map[string]string {
	if !r.tableExists(ctx, "fixer_resume_session_alias") {
		return map[string]string{}
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT codex_session_id, COALESCE(note, '')
		FROM fixer_resume_session_alias
		WHERE project_id = ?`, projectID)
	if err != nil {
		return map[string]string{}
	}
	defer rows.Close()

	notes := map[string]string{}
	for rows.Next() {
		var sessionID string
		var note string
		if err := rows.Scan(&sessionID, &note); err != nil {
			return notes
		}
		sessionID = strings.TrimSpace(sessionID)
		if sessionID != "" {
			notes[sessionID] = strings.TrimSpace(note)
		}
	}
	return notes
}

func loadCodexChatSessions(projectCWD string, activeFixerSessionID string, aliasNotes map[string]string) ([]codexChatSession, int) {
	sessionsRoot := filepath.Join(userHomeDir(), ".codex", "sessions")
	info, err := os.Stat(sessionsRoot)
	if err != nil || !info.IsDir() {
		return nil, 0
	}

	files := make([]string, 0, maxCodexSessionScan)
	_ = filepath.WalkDir(sessionsRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			return nil
		}
		files = append(files, path)
		return nil
	})

	sort.Slice(files, func(i, j int) bool {
		left, leftErr := os.Stat(files[i])
		right, rightErr := os.Stat(files[j])
		if leftErr != nil || rightErr != nil {
			return files[i] > files[j]
		}
		return left.ModTime().After(right.ModTime())
	})

	if len(files) > maxCodexSessionScan {
		files = files[:maxCodexSessionScan]
	}

	sessions := make([]codexChatSession, 0, maxCodexChatSessions)
	ambiguousCount := 0
	seenSessionIDs := map[string]bool{}
	for _, path := range files {
		session, ok := inspectCodexChatSession(path, projectCWD, activeFixerSessionID, aliasNotes)
		if !ok {
			continue
		}
		if session.AgentRole == "ambiguous" {
			ambiguousCount++
			continue
		}
		if session.AgentRole == "" {
			continue
		}
		if seenSessionIDs[session.SessionID] {
			continue
		}
		seenSessionIDs[session.SessionID] = true
		sessions = append(sessions, session)
		if len(sessions) >= maxCodexChatSessions {
			break
		}
	}

	return sessions, ambiguousCount
}

func inspectCodexChatSession(path string, projectCWD string, activeFixerSessionID string, aliasNotes map[string]string) (codexChatSession, bool) {
	file, err := os.Open(path)
	if err != nil {
		return codexChatSession{}, false
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)

	session := codexChatSession{
		Backend:        "codex",
		BindingSource:  "codex_session_log",
		SessionLogPath: path,
		SessionLog:     true,
		Status:         "history",
	}

	roleFirstLines := map[string]int{}
	lineIndex := 0
	for scanner.Scan() {
		line := scanner.Bytes()
		if lineIndex < maxRoleMarkerLines {
			text := string(line)
			for _, role := range rolesFromMarkers(text) {
				if _, exists := roleFirstLines[role]; !exists {
					roleFirstLines[role] = lineIndex
				}
			}
		}

		var envelope struct {
			Timestamp string          `json:"timestamp"`
			Type      string          `json:"type"`
			Payload   json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(line, &envelope); err == nil {
			if envelope.Timestamp != "" {
				session.LastActivityAt = envelope.Timestamp
			}
			switch envelope.Type {
			case "session_meta":
				var payload struct {
					ID        string `json:"id"`
					Timestamp string `json:"timestamp"`
					CWD       string `json:"cwd"`
				}
				if err := json.Unmarshal(envelope.Payload, &payload); err == nil {
					session.SessionID = strings.TrimSpace(payload.ID)
					session.CWD = filepath.Clean(strings.TrimSpace(payload.CWD))
					if session.CWD != filepath.Clean(projectCWD) {
						return codexChatSession{}, false
					}
					if payload.Timestamp != "" {
						session.StartedAt = payload.Timestamp
					} else if envelope.Timestamp != "" {
						session.StartedAt = envelope.Timestamp
					}
				}
			case "turn_context":
				var payload struct {
					Model  string `json:"model"`
					Effort string `json:"effort"`
				}
				if err := json.Unmarshal(envelope.Payload, &payload); err == nil {
					if session.Model == "" {
						session.Model = strings.TrimSpace(payload.Model)
					}
					if session.Reasoning == "" {
						session.Reasoning = strings.TrimSpace(payload.Effort)
					}
				}
			}
		}
		lineIndex++
	}

	if session.SessionID == "" || session.CWD == "" {
		return codexChatSession{}, false
	}

	aliasNote := aliasNotes[session.SessionID]
	session.AgentRole = classifyChatRole(session.SessionID, activeFixerSessionID, aliasNote, roleFirstLines)
	switch {
	case session.SessionID == activeFixerSessionID:
		session.Status = "active"
		session.BindingSource = appendBindingSource(session.BindingSource, "autonomous_state")
	case aliasNote != "":
		session.Status = "resume_alias"
		session.BindingSource = appendBindingSource(session.BindingSource, "fixer_resume_alias")
	}
	session.Headline = headlineForSession(session.AgentRole, aliasNote, session.SessionID)
	session.Transcript = session.SessionLog
	return session, true
}

func rolesFromMarkers(text string) []string {
	roles := []string{}
	if strings.Contains(text, fixerSkillMarker) {
		roles = append(roles, "fixer")
	}
	if strings.Contains(text, overseerSkillMarker) {
		roles = append(roles, "overseer")
	}
	if strings.Contains(text, netrunnerSkillMarker) {
		roles = append(roles, "netrunner")
	}
	return roles
}

func classifyChatRole(sessionID string, activeFixerSessionID string, aliasNote string, roleFirstLines map[string]int) string {
	if sessionID != "" && sessionID == activeFixerSessionID {
		return "fixer"
	}
	if role := roleFromAliasNote(aliasNote); role != "" {
		return role
	}
	if len(roleFirstLines) == 0 {
		return ""
	}

	selectedRole := ""
	selectedLine := maxRoleMarkerLines + 1
	for _, role := range []string{"fixer", "overseer", "netrunner"} {
		line, exists := roleFirstLines[role]
		if !exists {
			continue
		}
		if line < selectedLine {
			selectedRole = role
			selectedLine = line
		}
	}
	return selectedRole
}

func roleFromAliasNote(note string) string {
	normalized := strings.ToLower(strings.TrimSpace(note))
	switch {
	case strings.Contains(normalized, "overseer"):
		return "overseer"
	case strings.Contains(normalized, "fixer"):
		return "fixer"
	default:
		return ""
	}
}

func headlineForSession(role string, aliasNote string, sessionID string) string {
	if trimmed := strings.TrimSpace(aliasNote); trimmed != "" {
		return firstLineOrFallback(trimmed, fallbackHeadline(role, sessionID))
	}
	return fallbackHeadline(role, sessionID)
}

func fallbackHeadline(role string, sessionID string) string {
	label := "Codex"
	switch role {
	case "fixer":
		label = "Fixer"
	case "overseer":
		label = "Overseer"
	case "netrunner":
		label = "Netrunner"
	}
	return fmt.Sprintf("%s thread %s", label, shortSessionID(sessionID))
}

func shortSessionID(sessionID string) string {
	trimmed := strings.TrimSpace(sessionID)
	if len(trimmed) <= 8 {
		return trimmed
	}
	return trimmed[:8]
}

func appendBindingSource(source string, suffix string) string {
	if source == "" {
		return suffix
	}
	if suffix == "" || strings.Contains(source, suffix) {
		return source
	}
	return source + "+" + suffix
}

func toFixerChatSessionSummary(session codexChatSession) FixerChatSessionSummary {
	return FixerChatSessionSummary{
		ExternalID:     session.SessionID,
		CodexSessionID: session.SessionID,
		Headline:       session.Headline,
		Status:         session.Status,
		AgentRole:      session.AgentRole,
		Backend:        session.Backend,
		Model:          session.Model,
		Reasoning:      session.Reasoning,
		LastActivityAt: session.LastActivityAt,
		BindingSource:  session.BindingSource,
		SessionLogPath: session.SessionLogPath,
		SessionLog:     session.SessionLog,
		Transcript:     session.Transcript,
	}
}

func containsChatSession(sessions []FixerChatSessionSummary, sessionID string) bool {
	for _, session := range sessions {
		if session.CodexSessionID == sessionID || session.ExternalID == sessionID {
			return true
		}
	}
	return false
}

func chatBindingResidualRisk(preferredRole string, ambiguousCount int) string {
	parts := []string{
		fmt.Sprintf("Fixer MCP resolves %s session links from the autonomous state, resume aliases, and local Codex rollout logs; full messages are read from local JSONL logs through node_bridge when available.", preferredRole),
	}
	if ambiguousCount > 0 {
		parts = append(parts, fmt.Sprintf("%d local Codex session logs were hidden because they contained mixed role markers and could not be labeled truthfully.", ambiguousCount))
	}
	return strings.Join(parts, " ")
}

func noChatBindingMessage(preferredRole string) string {
	return fmt.Sprintf("No truthful %s chat binding metadata is available for this project yet.", preferredRole)
}

func loadAutonomousFixerSessionID(projectCWD string) string {
	type autonomousState struct {
		FixerCodexSessionID string `json:"fixer_codex_session_id"`
		ProjectCWD          string `json:"project_cwd"`
	}

	statePath := filepath.Join(projectCWD, ".codex", "autonomous_resolution.json")
	raw, err := os.ReadFile(statePath)
	if err != nil {
		return ""
	}
	var state autonomousState
	if err := json.Unmarshal(raw, &state); err != nil {
		return ""
	}
	if strings.TrimSpace(state.ProjectCWD) != "" && filepath.Clean(strings.TrimSpace(state.ProjectCWD)) != filepath.Clean(projectCWD) {
		return ""
	}
	return strings.TrimSpace(state.FixerCodexSessionID)
}

func (r *Repository) loadActiveAutonomousFixerSessionID(ctx context.Context, projectID int, projectCWD string) string {
	if !r.tableExists(ctx, "autonomous_run_status") {
		return loadAutonomousFixerSessionID(projectCWD)
	}
	var state string
	err := r.db.QueryRowContext(
		ctx,
		"SELECT COALESCE(state, '') FROM autonomous_run_status WHERE project_id = ?",
		projectID,
	).Scan(&state)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return loadAutonomousFixerSessionID(projectCWD)
	}
	switch strings.TrimSpace(state) {
	case "", "completed", "idle":
		return ""
	default:
		return loadAutonomousFixerSessionID(projectCWD)
	}
}

func userHomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}
