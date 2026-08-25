package dashboardapi

import (
	"bufio"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// OverseerThreadSummary is provider-neutral history metadata for the global
// Overseer manager. External IDs are deliberately opaque: resume always sends
// the ID back to the provider that created it.
type OverseerThreadSummary struct {
	Backend           string `json:"backend"`
	Model             string `json:"model"`
	Reasoning         string `json:"reasoning"`
	SpawnCWD          string `json:"spawn_cwd"`
	Origin            string `json:"origin"`
	StartedAt         string `json:"started_at"`
	LastActivityAt    string `json:"last_activity_at"`
	ExternalSessionID string `json:"external_session_id"`
	Preview           string `json:"preview"`
}

type OverseerHistoryResponse struct {
	Threads []OverseerThreadSummary `json:"threads"`
}

type OverseerLaunchInput struct {
	CWD               string `json:"cwd"`
	Backend           string `json:"backend"`
	Model             string `json:"model"`
	Reasoning         string `json:"reasoning"`
	ExternalSessionID string `json:"external_session_id,omitempty"`
}

// OverseerLaunchPlan is a secret-free handoff to the process-launch layer. It
// intentionally exposes the locked-role contract and required MCP selection so
// tests and clients can verify a launch without leaking credentials.
type OverseerLaunchPlan struct {
	Mode              string            `json:"mode"`
	CWD               string            `json:"cwd"`
	Backend           string            `json:"backend"`
	Model             string            `json:"model"`
	Reasoning         string            `json:"reasoning"`
	ExternalSessionID string            `json:"external_session_id,omitempty"`
	Prompt            string            `json:"prompt,omitempty"`
	Command           []string          `json:"command"`
	Environment       map[string]string `json:"environment"`
	RequiredMCPs      []string          `json:"required_mcps"`
}

type overseerProviderDefaults struct {
	model     string
	reasoning string
}

var overseerBackends = map[string]overseerProviderDefaults{
	"codex":       {model: "gpt-5.6-luna", reasoning: "xhigh"},
	"droid":       {model: "kimi-k2.6", reasoning: "high"},
	"claude":      {model: "sonnet", reasoning: "medium"},
	"antigravity": {model: "default", reasoning: "default"},
	"junie":       {model: "kimi-k2.6", reasoning: "default"},
	"kimi-code":   {model: "kimi-k2.7-code", reasoning: "default"},
}

func (r *Repository) OverseerHistory(ctx context.Context) (OverseerHistoryResponse, error) {
	cwds, err := r.overseerDiscoveryCWDs(ctx)
	if err != nil {
		return OverseerHistoryResponse{}, err
	}

	threads := make([]OverseerThreadSummary, 0)
	projectCWDs := make(map[string]bool, len(cwds))
	for _, cwd := range cwds {
		projectCWDs[cwd] = true
	}
	// Codex stores all projects under one transcript tree.  Parse that tree
	// once and route sessions by the CWD recorded in session_meta; doing this
	// inside the project loop made the home page rescan thousands of files for
	// every registered project.
	codexSessions, _ := loadAllCodexChatSessions()
	for _, session := range codexSessions {
		if session.AgentRole != "overseer" || !projectCWDs[session.CWD] {
			continue
		}
		threads = append(threads, codexOverseerThread(session))
	}
	for _, cwd := range cwds {
		threads = append(threads, discoverJSONLOverseerThreads(cwd, "claude")...)
		threads = append(threads, discoverJSONLOverseerThreads(cwd, "droid")...)
		threads = append(threads, discoverJunieOverseerThreads(cwd)...)
		threads = append(threads, discoverAntigravityOverseerThreads(cwd)...)
		threads = append(threads, discoverKimiOverseerThreads(cwd)...)
	}

	seen := map[string]bool{}
	unique := make([]OverseerThreadSummary, 0, len(threads))
	for _, thread := range threads {
		key := thread.Backend + "\x00" + thread.ExternalSessionID
		if thread.ExternalSessionID == "" || seen[key] {
			continue
		}
		seen[key] = true
		unique = append(unique, thread)
	}
	sort.SliceStable(unique, func(i, j int) bool {
		return unique[i].LastActivityAt > unique[j].LastActivityAt
	})
	return OverseerHistoryResponse{Threads: unique}, nil
}

func (r *Repository) BuildOverseerLaunchPlan(input OverseerLaunchInput) (OverseerLaunchPlan, error) {
	cwd, err := normalizeProjectCWD(input.CWD)
	if err != nil {
		return OverseerLaunchPlan{}, err
	}
	info, err := os.Stat(cwd)
	if err != nil || !info.IsDir() {
		return OverseerLaunchPlan{}, fmt.Errorf("overseer cwd must be an existing directory")
	}

	backend := strings.ToLower(strings.TrimSpace(input.Backend))
	if backend == "agy" {
		backend = "antigravity"
	}
	defaults, supported := overseerBackends[backend]
	if !supported {
		return OverseerLaunchPlan{}, fmt.Errorf("unsupported Overseer backend %q", input.Backend)
	}
	model := strings.TrimSpace(input.Model)
	if model == "" {
		model = defaults.model
	}
	reasoning := strings.TrimSpace(input.Reasoning)
	if reasoning == "" {
		reasoning = defaults.reasoning
	}
	externalID := strings.TrimSpace(input.ExternalSessionID)
	mode := "create"
	prompt := overseerSkillMarker
	if externalID != "" {
		mode = "resume"
		prompt = ""
	}

	plan := OverseerLaunchPlan{
		Mode:              mode,
		CWD:               cwd,
		Backend:           backend,
		Model:             model,
		Reasoning:         reasoning,
		ExternalSessionID: externalID,
		Prompt:            prompt,
		Environment: map[string]string{
			"FIXER_MCP_LOCKED_ROLE":  "overseer",
			"FIXER_MCP_DEFAULT_ROLE": "overseer",
			"FIXER_MCP_DEFAULT_CWD":  cwd,
			"FIXER_MCP_AUTO_AUTH":    "1",
		},
		RequiredMCPs: []string{"fixer_mcp"},
	}
	plan.Command = buildOverseerCommand(plan)
	return plan, nil
}

func buildOverseerCommand(plan OverseerLaunchPlan) []string {
	var command []string
	switch plan.Backend {
	case "codex":
		command = []string{"codex"}
		if plan.Mode == "resume" {
			command = append(command, "fork")
		}
		command = append(command, "--model", plan.Model, "-c", fmt.Sprintf("model_reasoning_effort=%q", plan.Reasoning), "--dangerously-bypass-approvals-and-sandbox")
		if plan.Mode == "resume" {
			command = append(command, plan.ExternalSessionID)
		} else {
			command = append(command, plan.Prompt)
		}
	case "claude":
		command = []string{"claude"}
		if plan.Mode == "resume" {
			command = append(command, "--resume", plan.ExternalSessionID)
		}
		command = append(command, "--model", plan.Model, "--dangerously-skip-permissions")
		if plan.Mode == "create" {
			command = append(command, plan.Prompt)
		}
	case "droid":
		command = []string{"droid"}
		if plan.Mode == "resume" {
			command = append(command, "--resume", plan.ExternalSessionID)
		}
		command = append(command, "-m", plan.Model, "-r", plan.Reasoning)
		if plan.Mode == "create" {
			command = append(command, plan.Prompt)
		}
	case "antigravity":
		command = []string{"agy", "--model", plan.Model, "--dangerously-skip-permissions"}
		if plan.Mode == "resume" {
			command = append(command, "--conversation", plan.ExternalSessionID)
		} else {
			command = append(command, "--prompt-interactive", plan.Prompt)
		}
	case "junie":
		command = []string{"junie", "--model", plan.Model, "--mcp-default-locations", "false", "--skill-default-locations", "false"}
		if plan.Mode == "resume" {
			command = append(command, "--resume", "--session-id", plan.ExternalSessionID)
		} else {
			command = append(command, "--task", plan.Prompt)
		}
	case "kimi-code":
		command = []string{"kimi", "-m", plan.Model, "--yolo"}
		if plan.Mode == "resume" {
			command = append(command, "-r", plan.ExternalSessionID)
		} else {
			command = append(command, plan.Prompt)
		}
	}
	return command
}

func (r *Repository) overseerDiscoveryCWDs(ctx context.Context) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT cwd FROM project ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[string]bool{}
	cwds := []string{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		cwd, err := normalizeProjectCWD(raw)
		if err == nil && !seen[cwd] {
			seen[cwd] = true
			cwds = append(cwds, cwd)
		}
	}
	if r.currentProjectCWD != "" && !seen[r.currentProjectCWD] {
		cwds = append(cwds, r.currentProjectCWD)
	}
	return cwds, rows.Err()
}

func discoverCodexOverseerThreads(cwd string) []OverseerThreadSummary {
	sessions, _ := loadCodexChatSessions(cwd, "", map[string]string{})
	threads := []OverseerThreadSummary{}
	for _, session := range sessions {
		if session.AgentRole != "overseer" {
			continue
		}
		thread := codexOverseerThread(session)
		thread.SpawnCWD = cwd
		threads = append(threads, thread)
	}
	return threads
}

func codexOverseerThread(session codexChatSession) OverseerThreadSummary {
	defaults := overseerBackends["codex"]
	return OverseerThreadSummary{
		Backend:           "codex",
		Model:             firstNonEmpty(session.Model, defaults.model),
		Reasoning:         firstNonEmpty(session.Reasoning, defaults.reasoning),
		SpawnCWD:          session.CWD,
		Origin:            "codex_session_log",
		StartedAt:         session.StartedAt,
		LastActivityAt:    session.LastActivityAt,
		ExternalSessionID: session.SessionID,
		Preview:           session.Headline,
	}
}

func discoverJSONLOverseerThreads(cwd string, backend string) []OverseerThreadSummary {
	root := ""
	slug := providerProjectSlug(cwd)
	switch backend {
	case "claude":
		root = filepath.Join(userHomeDir(), ".claude", "projects", slug)
	case "droid":
		root = filepath.Join(userHomeDir(), ".factory", "sessions", slug)
	}
	paths, _ := filepath.Glob(filepath.Join(root, "*.jsonl"))
	sort.Slice(paths, func(i, j int) bool {
		return fileTimestamp(paths[i]) > fileTimestamp(paths[j])
	})
	if len(paths) > maxCodexChatSessions {
		paths = paths[:maxCodexChatSessions]
	}
	threads := []OverseerThreadSummary{}
	for _, path := range paths {
		metadata, ok := inspectOverseerJSONL(path)
		if !ok {
			continue
		}
		defaults := overseerBackends[backend]
		sessionID := firstNonEmpty(metadata.sessionID, strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
		threads = append(threads, OverseerThreadSummary{
			Backend:           backend,
			Model:             firstNonEmpty(metadata.model, defaults.model),
			Reasoning:         firstNonEmpty(metadata.reasoning, defaults.reasoning),
			SpawnCWD:          cwd,
			Origin:            backend + "_session_log",
			StartedAt:         firstNonEmpty(metadata.startedAt, fileTimestamp(path)),
			LastActivityAt:    firstNonEmpty(metadata.updatedAt, fileTimestamp(path)),
			ExternalSessionID: sessionID,
			Preview:           firstNonEmpty(metadata.preview, "Overseer thread "+shortSessionID(sessionID)),
		})
	}
	return threads
}

func discoverJunieOverseerThreads(cwd string) []OverseerThreadSummary {
	indexPath := filepath.Join(userHomeDir(), ".junie", "sessions", "index.jsonl")
	file, err := os.Open(indexPath)
	if err != nil {
		return nil
	}
	defer file.Close()
	threads := []OverseerThreadSummary{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var record map[string]any
		if json.Unmarshal(scanner.Bytes(), &record) != nil || strings.TrimSpace(stringValue(record["projectDir"])) != cwd {
			continue
		}
		sessionID := strings.TrimSpace(stringValue(record["sessionId"]))
		markerPath := filepath.Join(filepath.Dir(indexPath), sessionID, "state.json")
		if _, err := os.Stat(markerPath); err != nil {
			markerPath = filepath.Join(filepath.Dir(indexPath), sessionID, "events.jsonl")
		}
		metadata, ok := inspectOverseerJSONL(markerPath)
		if !ok {
			continue
		}
		defaults := overseerBackends["junie"]
		threads = append(threads, OverseerThreadSummary{
			Backend:           "junie",
			Model:             firstNonEmpty(stringValue(record["model"]), metadata.model, defaults.model),
			Reasoning:         firstNonEmpty(stringValue(record["reasoning"]), metadata.reasoning, defaults.reasoning),
			SpawnCWD:          cwd,
			Origin:            "junie_session_index",
			StartedAt:         firstNonEmpty(stringValue(record["createdAt"]), fileTimestamp(markerPath)),
			LastActivityAt:    firstNonEmpty(stringValue(record["updatedAt"]), fileTimestamp(markerPath)),
			ExternalSessionID: sessionID,
			Preview:           firstNonEmpty(stringValue(record["taskName"]), metadata.preview, "Overseer thread "+shortSessionID(sessionID)),
		})
	}
	return threads
}

func discoverAntigravityOverseerThreads(cwd string) []OverseerThreadSummary {
	root := filepath.Join(userHomeDir(), ".gemini", "antigravity-cli")
	historyPath := filepath.Join(root, "history.jsonl")
	file, err := os.Open(historyPath)
	if err != nil {
		return nil
	}
	defer file.Close()
	threads := []OverseerThreadSummary{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var record map[string]any
		if json.Unmarshal(scanner.Bytes(), &record) != nil || strings.TrimSpace(stringValue(record["workspace"])) != cwd {
			continue
		}
		sessionID := strings.TrimSpace(stringValue(record["conversationId"]))
		path := filepath.Join(root, "conversations", sessionID+".db")
		if _, err := os.Stat(path); err != nil {
			path = filepath.Join(root, "conversations", sessionID+".pb")
		}
		if !fileContainsOverseerMarker(path) {
			continue
		}
		defaults := overseerBackends["antigravity"]
		threads = append(threads, OverseerThreadSummary{
			Backend:           "antigravity",
			Model:             defaults.model,
			Reasoning:         defaults.reasoning,
			SpawnCWD:          cwd,
			Origin:            "antigravity_conversation_store",
			StartedAt:         fileTimestamp(path),
			LastActivityAt:    fileTimestamp(path),
			ExternalSessionID: sessionID,
			Preview:           firstNonEmpty(stringValue(record["display"]), "Overseer thread "+shortSessionID(sessionID)),
		})
	}
	return threads
}

func discoverKimiOverseerThreads(cwd string) []OverseerThreadSummary {
	hash := md5.Sum([]byte(cwd))
	root := filepath.Join(userHomeDir(), ".kimi", "sessions", hex.EncodeToString(hash[:]))
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	threads := []OverseerThreadSummary{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(root, entry.Name(), "context.jsonl")
		metadata, ok := inspectOverseerJSONL(path)
		if !ok {
			continue
		}
		defaults := overseerBackends["kimi-code"]
		threads = append(threads, OverseerThreadSummary{
			Backend:           "kimi-code",
			Model:             firstNonEmpty(metadata.model, defaults.model),
			Reasoning:         firstNonEmpty(metadata.reasoning, defaults.reasoning),
			SpawnCWD:          cwd,
			Origin:            "kimi_session_store",
			StartedAt:         firstNonEmpty(metadata.startedAt, fileTimestamp(path)),
			LastActivityAt:    firstNonEmpty(metadata.updatedAt, fileTimestamp(path)),
			ExternalSessionID: entry.Name(),
			Preview:           firstNonEmpty(metadata.preview, "Overseer thread "+shortSessionID(entry.Name())),
		})
	}
	return threads
}

type overseerLogMetadata struct {
	sessionID string
	model     string
	reasoning string
	startedAt string
	updatedAt string
	preview   string
}

func inspectOverseerJSONL(path string) (overseerLogMetadata, bool) {
	file, err := os.Open(path)
	if err != nil {
		return overseerLogMetadata{}, false
	}
	defer file.Close()
	metadata := overseerLogMetadata{}
	hasMarker := false
	lineCount := 0
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if containsOverseerMarker(line) {
			hasMarker = true
		}
		var record any
		if json.Unmarshal([]byte(line), &record) != nil {
			continue
		}
		flat := flattenJSONStrings(record)
		metadata.sessionID = firstNonEmpty(metadata.sessionID, flat["sessionId"], flat["session_id"], flat["id"])
		metadata.model = firstNonEmpty(metadata.model, flat["model"], flat["modelId"], flat["model_id"])
		metadata.reasoning = firstNonEmpty(metadata.reasoning, flat["reasoning"], flat["reasoningEffort"], flat["reasoning_effort"], flat["effort"])
		timestamp := firstNonEmpty(flat["timestamp"], flat["createdAt"], flat["updatedAt"])
		if metadata.startedAt == "" {
			metadata.startedAt = timestamp
		}
		if timestamp != "" {
			metadata.updatedAt = timestamp
		}
		metadata.preview = firstNonEmpty(metadata.preview, flat["sessionTitle"], flat["taskName"], flat["title"])
		lineCount++
		if lineCount >= maxRoleMarkerLines {
			break
		}
	}
	if metadata.updatedAt == "" {
		metadata.updatedAt = fileTimestamp(path)
	}
	return metadata, hasMarker
}

func fileContainsOverseerMarker(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	buffer := make([]byte, 256*1024)
	n, _ := file.Read(buffer)
	return n > 0 && containsOverseerMarker(string(buffer[:n]))
}

func containsOverseerMarker(text string) bool {
	return strings.Contains(text, overseerSkillMarker) ||
		strings.Contains(text, "Use the `init-overseer` skill immediately.") ||
		strings.Contains(text, "`$init-overseer`") ||
		strings.Contains(text, "$init-overseer") ||
		strings.Contains(text, "/init-overseer") ||
		strings.Contains(text, "`init-overseer`")
}

func flattenJSONStrings(value any) map[string]string {
	result := map[string]string{}
	var walk func(any)
	walk = func(current any) {
		switch typed := current.(type) {
		case map[string]any:
			for key, nested := range typed {
				if text, ok := nested.(string); ok && strings.TrimSpace(text) != "" {
					if _, exists := result[key]; !exists {
						result[key] = strings.TrimSpace(text)
					}
				}
				walk(nested)
			}
		case []any:
			for _, nested := range typed {
				walk(nested)
			}
		}
	}
	walk(value)
	return result
}

func providerProjectSlug(cwd string) string {
	var builder strings.Builder
	lastDash := false
	for _, char := range cwd {
		isAlphaNumeric := (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9')
		if isAlphaNumeric {
			builder.WriteRune(char)
			lastDash = false
		} else if !lastDash {
			builder.WriteByte('-')
			lastDash = true
		}
	}
	return builder.String()
}

func fileTimestamp(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	return info.ModTime().UTC().Format(time.RFC3339Nano)
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		return fmt.Sprintf("%.0f", typed)
	default:
		return ""
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
