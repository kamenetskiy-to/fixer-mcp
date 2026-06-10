package main

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type GetNetrunnerTranscriptPathInput struct {
	ProjectId int `json:"project_id,omitempty" jsonschema:"Required for overseer; fixer uses the bound project."`
	SessionId int `json:"session_id" jsonschema:"Project-scoped Netrunner session ID."`
}

type GetNetrunnerTranscriptPathOutput struct {
	Backend           string   `json:"backend"`
	ProjectId         int      `json:"project_id"`
	SessionId         int      `json:"session_id"`
	GlobalSessionId   int      `json:"global_session_id"`
	ExternalSessionId string   `json:"external_session_id,omitempty"`
	TranscriptPath    string   `json:"transcript_path,omitempty"`
	Found             bool     `json:"found"`
	Exists            bool     `json:"exists"`
	Readable          bool     `json:"readable"`
	FileSizeBytes     int64    `json:"file_size_bytes,omitempty"`
	ModifiedAt        string   `json:"modified_at,omitempty"`
	SearchDiagnostics []string `json:"search_diagnostics"`
	OperatorHint      string   `json:"operator_hint,omitempty"`
}

func droidProjectTranscriptDirName(projectCWD string) string {
	cleaned := filepath.Clean(strings.TrimSpace(projectCWD))
	if cleaned == "." || cleaned == "" {
		return ""
	}
	return strings.ReplaceAll(cleaned, string(os.PathSeparator), "-")
}

func transcriptFileMetadata(path string) (bool, bool, int64, string) {
	if strings.TrimSpace(path) == "" {
		return false, false, 0, ""
	}
	info, statErr := os.Stat(path)
	if statErr != nil || info.IsDir() {
		return false, false, 0, ""
	}
	file, openErr := os.Open(path)
	if openErr == nil {
		_ = file.Close()
	}
	return true, openErr == nil, info.Size(), info.ModTime().Format(time.RFC3339)
}

func findCodexTranscriptPath(externalSessionID string, diagnostics *[]string) string {
	sessionID := strings.TrimSpace(externalSessionID)
	root := strings.TrimSpace(codexSessionTranscriptRoot)
	if sessionID == "" {
		*diagnostics = append(*diagnostics, "external session id is empty; cannot resolve Codex transcript filename")
		return ""
	}
	if root == "" {
		*diagnostics = append(*diagnostics, "Codex transcript root is not configured")
		return ""
	}
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		*diagnostics = append(*diagnostics, fmt.Sprintf("Codex transcript root not found: %s", root))
		return ""
	}

	var fallback string
	walkErr := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".jsonl") || !strings.Contains(name, sessionID) {
			return nil
		}
		if strings.HasSuffix(name, "-"+sessionID+".jsonl") || name == sessionID+".jsonl" {
			fallback = path
			return filepath.SkipAll
		}
		if fallback == "" {
			fallback = path
		}
		return nil
	})
	if walkErr != nil {
		*diagnostics = append(*diagnostics, fmt.Sprintf("Codex transcript search failed: %v", walkErr))
	}
	if fallback == "" {
		*diagnostics = append(*diagnostics, fmt.Sprintf("no Codex JSONL filename containing external session id %q under %s", sessionID, root))
	}
	return fallback
}

func payloadString(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := payload[key]; ok {
			if text := strings.TrimSpace(fmt.Sprint(value)); text != "" && text != "<nil>" {
				return text
			}
		}
	}
	return ""
}

func nestedPayloadMap(payload map[string]any, key string) map[string]any {
	value, ok := payload[key]
	if !ok {
		return nil
	}
	nested, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	return nested
}

func transcriptPayloadRecordType(payload map[string]any) string {
	return payloadString(payload, "type", "event", "event_type", "record_type")
}

func transcriptPayloadCWD(payload map[string]any) string {
	if cwd := payloadString(payload, "cwd", "current_working_directory", "workingDirectory", "working_directory"); cwd != "" {
		return cwd
	}
	for _, key := range []string{"payload", "session"} {
		if nested := nestedPayloadMap(payload, key); nested != nil {
			if cwd := transcriptPayloadCWD(nested); cwd != "" {
				return cwd
			}
		}
	}
	return ""
}

func transcriptPayloadSessionID(payload map[string]any) string {
	if sessionID := payloadString(payload, "external_session_id", "externalSessionId", "session_id", "sessionId", "id"); sessionID != "" {
		return sessionID
	}
	for _, key := range []string{"payload", "session"} {
		if nested := nestedPayloadMap(payload, key); nested != nil {
			if sessionID := transcriptPayloadSessionID(nested); sessionID != "" {
				return sessionID
			}
		}
	}
	return ""
}

func sameTranscriptCWD(actual string, expected string) bool {
	actual = strings.TrimSpace(actual)
	expected = strings.TrimSpace(expected)
	if actual == "" || expected == "" {
		return false
	}
	actualClean, actualErr := filepath.Abs(filepath.Clean(actual))
	expectedClean, expectedErr := filepath.Abs(filepath.Clean(expected))
	if actualErr != nil || expectedErr != nil {
		return actual == expected
	}
	return actualClean == expectedClean
}

func candidateTranscriptFiles(root string, preferredDir string) []string {
	type candidate struct {
		path    string
		modTime time.Time
	}
	seen := map[string]struct{}{}
	candidates := []candidate{}
	addRoot := func(searchRoot string) {
		if strings.TrimSpace(searchRoot) == "" {
			return
		}
		_ = filepath.WalkDir(searchRoot, func(path string, entry os.DirEntry, err error) error {
			if err != nil || entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
				return nil
			}
			if _, ok := seen[path]; ok {
				return nil
			}
			info, statErr := entry.Info()
			if statErr != nil {
				return nil
			}
			seen[path] = struct{}{}
			candidates = append(candidates, candidate{path: path, modTime: info.ModTime()})
			return nil
		})
	}
	addRoot(preferredDir)
	addRoot(root)
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].modTime.After(candidates[j].modTime)
	})
	paths := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		paths = append(paths, candidate.path)
	}
	return paths
}

func transcriptMatchByProjectCWD(path string, projectCWD string, acceptedTypes map[string]struct{}) (string, bool) {
	file, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	lineCount := 0
	for scanner.Scan() {
		lineCount++
		if lineCount > 80 {
			break
		}
		var payload map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &payload); err != nil {
			continue
		}
		recordType := transcriptPayloadRecordType(payload)
		if len(acceptedTypes) > 0 {
			if _, ok := acceptedTypes[recordType]; !ok {
				continue
			}
		}
		if !sameTranscriptCWD(transcriptPayloadCWD(payload), projectCWD) {
			continue
		}
		if sessionID := transcriptPayloadSessionID(payload); sessionID != "" {
			return sessionID, true
		}
		return strings.TrimSuffix(filepath.Base(path), ".jsonl"), true
	}
	return "", false
}

func findCodexTranscriptPathByProjectCWD(projectCWD string, diagnostics *[]string) (string, string) {
	root := strings.TrimSpace(codexSessionTranscriptRoot)
	if root == "" {
		*diagnostics = append(*diagnostics, "Codex transcript root is not configured")
		return "", ""
	}
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		*diagnostics = append(*diagnostics, fmt.Sprintf("Codex transcript root not found: %s", root))
		return "", ""
	}
	for _, path := range candidateTranscriptFiles(root, "") {
		sessionID, ok := transcriptMatchByProjectCWD(path, projectCWD, map[string]struct{}{"session_meta": {}})
		if ok {
			return path, sessionID
		}
	}
	*diagnostics = append(*diagnostics, fmt.Sprintf("no Codex JSONL session_meta matching project cwd %q under %s", projectCWD, root))
	return "", ""
}

func findDroidTranscriptPath(projectCWD string, externalSessionID string, diagnostics *[]string) string {
	sessionID := strings.TrimSpace(externalSessionID)
	root := strings.TrimSpace(droidSessionTranscriptRoot)
	if sessionID == "" {
		*diagnostics = append(*diagnostics, "external session id is empty; cannot resolve Droid transcript filename")
		return ""
	}
	if root == "" {
		*diagnostics = append(*diagnostics, "Droid transcript root is not configured")
		return ""
	}
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		*diagnostics = append(*diagnostics, fmt.Sprintf("Droid transcript root not found: %s", root))
		return ""
	}

	if dirName := droidProjectTranscriptDirName(projectCWD); dirName != "" {
		directPath := filepath.Join(root, dirName, sessionID+".jsonl")
		if info, err := os.Stat(directPath); err == nil && !info.IsDir() {
			return directPath
		}
		*diagnostics = append(*diagnostics, fmt.Sprintf("Droid direct path not found: %s", directPath))
	}

	var fallback string
	walkErr := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() || entry.Name() != sessionID+".jsonl" {
			return nil
		}
		fallback = path
		return filepath.SkipAll
	})
	if walkErr != nil {
		*diagnostics = append(*diagnostics, fmt.Sprintf("Droid transcript search failed: %v", walkErr))
	}
	if fallback == "" {
		*diagnostics = append(*diagnostics, fmt.Sprintf("no Droid JSONL named %s.jsonl under %s", sessionID, root))
	}
	return fallback
}

func findDroidTranscriptPathByProjectCWD(projectCWD string, diagnostics *[]string) (string, string) {
	root := strings.TrimSpace(droidSessionTranscriptRoot)
	if root == "" {
		*diagnostics = append(*diagnostics, "Droid transcript root is not configured")
		return "", ""
	}
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		*diagnostics = append(*diagnostics, fmt.Sprintf("Droid transcript root not found: %s", root))
		return "", ""
	}

	preferredDir := ""
	if dirName := droidProjectTranscriptDirName(projectCWD); dirName != "" {
		preferredDir = filepath.Join(root, dirName)
	}
	for _, path := range candidateTranscriptFiles(root, preferredDir) {
		sessionID, ok := transcriptMatchByProjectCWD(path, projectCWD, map[string]struct{}{"": {}, "session_start": {}})
		if ok {
			return path, sessionID
		}
	}
	*diagnostics = append(*diagnostics, fmt.Sprintf("no Droid JSONL session_start matching project cwd %q under %s", projectCWD, root))
	return "", ""
}

func persistDiscoveredSessionExternalID(sessionID int, backend string, externalSessionID string) error {
	normalizedBackend, err := normalizeCliBackend(backend)
	if err != nil {
		return err
	}
	resolvedExternalSessionID := strings.TrimSpace(externalSessionID)
	if resolvedExternalSessionID == "" {
		return nil
	}
	result, err := db.Exec(
		`UPDATE session_external_link
		 SET external_session_id = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE session_id = ? AND backend = ?`,
		resolvedExternalSessionID,
		sessionID,
		normalizedBackend,
	)
	if err != nil {
		return err
	}
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		if _, err := db.Exec(
			`INSERT INTO session_external_link (session_id, backend, external_session_id, updated_at)
			 VALUES (?, ?, ?, CURRENT_TIMESTAMP)`,
			sessionID,
			normalizedBackend,
			resolvedExternalSessionID,
		); err != nil {
			return err
		}
	}
	if normalizedBackend != defaultCliBackend {
		return nil
	}
	result, err = db.Exec(
		`UPDATE session_codex_link
		 SET codex_session_id = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE session_id = ?`,
		resolvedExternalSessionID,
		sessionID,
	)
	if err != nil {
		return err
	}
	rowsAffected, _ = result.RowsAffected()
	if rowsAffected == 0 {
		_, err = db.Exec(
			`INSERT INTO session_codex_link (session_id, codex_session_id, updated_at)
			 VALUES (?, ?, CURRENT_TIMESTAMP)`,
			sessionID,
			resolvedExternalSessionID,
		)
	}
	return err
}

func resolveTranscriptLookupProjectAndSession(input GetNetrunnerTranscriptPathInput) (int, int, int, error) {
	if input.SessionId <= 0 {
		return 0, 0, 0, fmt.Errorf("session_id is required")
	}
	switch authorizedRole {
	case "fixer":
		projectID, err := resolveProjectHandoffProjectID(input.ProjectId)
		if err != nil {
			return 0, 0, 0, err
		}
		globalSessionID, err := globalSessionIDFromProjectScoped(input.SessionId, projectID)
		if err != nil {
			return 0, 0, 0, err
		}
		return projectID, input.SessionId, globalSessionID, nil
	case "overseer":
		projectID, err := resolveProjectHandoffProjectID(input.ProjectId)
		if err != nil {
			return 0, 0, 0, err
		}
		globalSessionID, err := globalSessionIDFromProjectScoped(input.SessionId, projectID)
		if err != nil {
			return 0, 0, 0, err
		}
		return projectID, input.SessionId, globalSessionID, nil
	default:
		return 0, 0, 0, fmt.Errorf("access denied: requires fixer or overseer role")
	}
}

func GetNetrunnerTranscriptPath(ctx context.Context, req *mcp.CallToolRequest, input GetNetrunnerTranscriptPathInput) (*mcp.CallToolResult, GetNetrunnerTranscriptPathOutput, error) {
	projectID, localSessionID, globalSessionID, err := resolveTranscriptLookupProjectAndSession(input)
	if err == sql.ErrNoRows {
		return &mcp.CallToolResult{IsError: true}, GetNetrunnerTranscriptPathOutput{}, fmt.Errorf("session not found in project")
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetNetrunnerTranscriptPathOutput{}, err
	}

	var backend string
	var projectCWD string
	err = db.QueryRow(
		`SELECT COALESCE(NULLIF(TRIM(s.cli_backend), ''), ?),
		        p.cwd
		 FROM session s
		 INNER JOIN project p ON p.id = s.project_id
		 WHERE s.id = ? AND s.project_id = ?`,
		defaultCliBackend,
		globalSessionID,
		projectID,
	).Scan(&backend, &projectCWD)
	if err == sql.ErrNoRows {
		return &mcp.CallToolResult{IsError: true}, GetNetrunnerTranscriptPathOutput{}, fmt.Errorf("session not found in project")
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetNetrunnerTranscriptPathOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	backend, err = normalizeCliBackend(backend)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetNetrunnerTranscriptPathOutput{}, err
	}

	externalSessionID, err := fetchSessionExternalID(globalSessionID, backend)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetNetrunnerTranscriptPathOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	diagnostics := []string{}
	var transcriptPath string
	if strings.TrimSpace(externalSessionID) == "" {
		diagnostics = append(diagnostics, "no external session id persisted yet; scanning transcript store by project cwd")
		var discoveredSessionID string
		switch backend {
		case "codex":
			transcriptPath, discoveredSessionID = findCodexTranscriptPathByProjectCWD(projectCWD, &diagnostics)
		case "droid":
			transcriptPath, discoveredSessionID = findDroidTranscriptPathByProjectCWD(projectCWD, &diagnostics)
		default:
			diagnostics = append(diagnostics, fmt.Sprintf("backend %q is unsupported for transcript lookup", backend))
		}
		if strings.TrimSpace(discoveredSessionID) != "" {
			externalSessionID = strings.TrimSpace(discoveredSessionID)
			if err := persistDiscoveredSessionExternalID(globalSessionID, backend, externalSessionID); err != nil {
				diagnostics = append(diagnostics, fmt.Sprintf("failed to persist discovered external session id: %v", err))
			} else {
				diagnostics = append(diagnostics, "persisted discovered external session id from transcript filename/metadata")
			}
		}
	} else {
		switch backend {
		case "codex":
			transcriptPath = findCodexTranscriptPath(externalSessionID, &diagnostics)
		case "droid":
			transcriptPath = findDroidTranscriptPath(projectCWD, externalSessionID, &diagnostics)
		default:
			diagnostics = append(diagnostics, fmt.Sprintf("backend %q is unsupported for transcript lookup", backend))
		}
	}

	exists, readable, fileSize, modifiedAt := transcriptFileMetadata(transcriptPath)
	operatorHint := ""
	if transcriptPath != "" {
		operatorHint = fmt.Sprintf("Inspect locally without dumping content: tail -n 80 %q; rg '<needle>' %q; jq -c 'select(.type)' %q | tail -n 40", transcriptPath, transcriptPath, transcriptPath)
	}

	return nil, GetNetrunnerTranscriptPathOutput{
		Backend:           backend,
		ProjectId:         projectID,
		SessionId:         localSessionID,
		GlobalSessionId:   globalSessionID,
		ExternalSessionId: externalSessionID,
		TranscriptPath:    transcriptPath,
		Found:             transcriptPath != "",
		Exists:            exists,
		Readable:          readable,
		FileSizeBytes:     fileSize,
		ModifiedAt:        modifiedAt,
		SearchDiagnostics: diagnostics,
		OperatorHint:      operatorHint,
	}, nil
}
