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

// antigravitySessionTranscriptRoot is the Antigravity CLI brain directory that
// holds per-session transcript logs. It is declared here instead of main.go so
// the transcript port stays inside the worker write scope; a main.go
// declaration of the same name must not be added on top of this one.
var antigravitySessionTranscriptRoot = filepath.Join(os.Getenv("HOME"), ".gemini", "antigravity-cli", "brain")

// commandcodeSessionTranscriptRoot is the CommandCode CLI projects directory:
// one subdirectory per encoded cwd holding <session-id>.jsonl session
// transcripts (plus .meta.json/.checkpoints.jsonl sidecars).
var commandcodeSessionTranscriptRoot = filepath.Join(os.Getenv("HOME"), ".commandcode", "projects")

// piSessionTranscriptRoot is the Pi agent session store: one subdirectory per
// encoded cwd holding timestamped <timestamp>_<session-id>.jsonl transcripts.
var piSessionTranscriptRoot = filepath.Join(os.Getenv("HOME"), ".pi", "agent", "sessions")

func findAntigravityTranscriptPath(externalSessionID string, diagnostics *[]string) string {
	sessionID := strings.TrimSpace(externalSessionID)
	root := strings.TrimSpace(antigravitySessionTranscriptRoot)
	if sessionID == "" {
		*diagnostics = append(*diagnostics, "external session id is empty; cannot resolve Antigravity transcript")
		return ""
	}
	if root == "" {
		*diagnostics = append(*diagnostics, "Antigravity transcript root is not configured")
		return ""
	}
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		*diagnostics = append(*diagnostics, fmt.Sprintf("Antigravity transcript root not found: %s", root))
		return ""
	}

	logsRoot := filepath.Join(root, sessionID, ".system_generated", "logs")
	for _, name := range []string{"transcript_full.jsonl", "transcript.jsonl"} {
		candidate := filepath.Join(logsRoot, name)
		if exists, _, _, _ := transcriptFileMetadata(candidate); exists {
			return candidate
		}
	}
	*diagnostics = append(*diagnostics, fmt.Sprintf("no Antigravity transcript for external session id %q under %s", sessionID, root))
	return ""
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

// commandcodeProjectTranscriptDirName mirrors the CommandCode CLI project
// slug: lowercased, every run of non-alphanumeric characters collapsed to one
// '-', trimmed to alphanumerics ("root" when nothing remains).
func commandcodeProjectTranscriptDirName(projectCWD string) string {
	cleaned := strings.ToLower(filepath.Clean(strings.TrimSpace(projectCWD)))
	var slug strings.Builder
	pendingDash := false
	for _, char := range cleaned {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') {
			if pendingDash && slug.Len() > 0 {
				slug.WriteByte('-')
			}
			pendingDash = false
			slug.WriteRune(char)
			continue
		}
		pendingDash = true
	}
	if slug.Len() == 0 {
		return "root"
	}
	return slug.String()
}

// piProjectTranscriptDirName mirrors pi-coding-agent's session directory
// encoding: one leading path separator is stripped, '/', '\' and ':' become
// '-', and the result is wrapped in '--'.
func piProjectTranscriptDirName(projectCWD string) string {
	cleaned := filepath.Clean(strings.TrimSpace(projectCWD))
	if cleaned == "" || cleaned == "." {
		return ""
	}
	trimmed := cleaned
	if trimmed[0] == '/' || trimmed[0] == '\\' {
		trimmed = trimmed[1:]
	}
	safe := strings.NewReplacer("/", "-", `\`, "-", ":", "-").Replace(trimmed)
	return "--" + safe + "--"
}

// piTranscriptFileNameMatches implements exact-id filename discovery for the
// Pi store: a bare <id>.jsonl or pi's timestamped <timestamp>_<id>.jsonl.
// Substring containment is never accepted, so an unrelated session can never
// be returned for a looked-up id.
func piTranscriptFileNameMatches(name string, sessionID string) bool {
	if name == sessionID+".jsonl" {
		return true
	}
	return strings.HasSuffix(name, "_"+sessionID+".jsonl")
}

// findCommandcodeTranscriptPath resolves a CommandCode session transcript by
// exact external session id: first the canonical
// <root>/<slug(cwd)>/<id>.jsonl path, then exact-id filename discovery under
// the root (sessions run in per-wave worktree cwds). No substring matching and
// no unrelated session is ever returned.
func findCommandcodeTranscriptPath(projectCWD string, externalSessionID string, diagnostics *[]string) string {
	sessionID := strings.TrimSpace(externalSessionID)
	root := strings.TrimSpace(commandcodeSessionTranscriptRoot)
	if sessionID == "" {
		*diagnostics = append(*diagnostics, "external session id is empty; cannot resolve CommandCode transcript filename")
		return ""
	}
	if root == "" {
		*diagnostics = append(*diagnostics, "CommandCode transcript root is not configured")
		return ""
	}
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		*diagnostics = append(*diagnostics, fmt.Sprintf("CommandCode transcript root not found: %s", root))
		return ""
	}

	if strings.TrimSpace(projectCWD) != "" {
		directPath := filepath.Join(root, commandcodeProjectTranscriptDirName(projectCWD), sessionID+".jsonl")
		if exists, _, _, _ := transcriptFileMetadata(directPath); exists {
			return directPath
		}
		*diagnostics = append(*diagnostics, fmt.Sprintf("CommandCode direct path not found: %s", directPath))
	}

	var fallback string
	walkErr := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || entry.Name() != sessionID+".jsonl" {
			return nil
		}
		fallback = path
		return filepath.SkipAll
	})
	if walkErr != nil {
		*diagnostics = append(*diagnostics, fmt.Sprintf("CommandCode transcript search failed: %v", walkErr))
	}
	if fallback == "" {
		*diagnostics = append(*diagnostics, fmt.Sprintf("no CommandCode JSONL named %s.jsonl under %s", sessionID, root))
	}
	return fallback
}

// findPiTranscriptPath resolves a Pi session transcript by exact external
// session id: first the session directory encoded for the project cwd, then
// exact-id filename discovery under the root (sessions run in per-wave
// worktree cwds). Timestamped <timestamp>_<id>.jsonl names are matched by
// exact id only.
func findPiTranscriptPath(projectCWD string, externalSessionID string, diagnostics *[]string) string {
	sessionID := strings.TrimSpace(externalSessionID)
	root := strings.TrimSpace(piSessionTranscriptRoot)
	if sessionID == "" {
		*diagnostics = append(*diagnostics, "external session id is empty; cannot resolve Pi transcript filename")
		return ""
	}
	if root == "" {
		*diagnostics = append(*diagnostics, "Pi transcript root is not configured")
		return ""
	}
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		*diagnostics = append(*diagnostics, fmt.Sprintf("Pi transcript root not found: %s", root))
		return ""
	}

	if dirName := piProjectTranscriptDirName(projectCWD); dirName != "" {
		preferredDir := filepath.Join(root, dirName)
		for _, path := range candidateTranscriptFiles(preferredDir, "") {
			if piTranscriptFileNameMatches(filepath.Base(path), sessionID) {
				return path
			}
		}
		*diagnostics = append(*diagnostics, fmt.Sprintf("Pi transcript for external session id %q not found in %s", sessionID, preferredDir))
	}

	var fallback string
	walkErr := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !piTranscriptFileNameMatches(entry.Name(), sessionID) {
			return nil
		}
		fallback = path
		return filepath.SkipAll
	})
	if walkErr != nil {
		*diagnostics = append(*diagnostics, fmt.Sprintf("Pi transcript search failed: %v", walkErr))
	}
	if fallback == "" {
		*diagnostics = append(*diagnostics, fmt.Sprintf("no Pi JSONL for external session id %q under %s", sessionID, root))
	}
	return fallback
}

// resolveBackendTranscriptPath is the single transcript lookup shared by the
// public get_netrunner_transcript_path tool and the System1 reader, so public
// metadata and System1 can never disagree. Every backend resolves by recorded
// external session id only: no path is invented and no unrelated session is
// ever returned.
func resolveBackendTranscriptPath(backend string, projectCWD string, externalSessionID string, diagnostics *[]string) string {
	switch backend {
	case "codex":
		return findCodexTranscriptPath(externalSessionID, diagnostics)
	case "commandcode":
		return findCommandcodeTranscriptPath(projectCWD, externalSessionID, diagnostics)
	case "pi":
		return findPiTranscriptPath(projectCWD, externalSessionID, diagnostics)
	case "droid":
		return findDroidTranscriptPath(projectCWD, externalSessionID, diagnostics)
	case "antigravity":
		return findAntigravityTranscriptPath(externalSessionID, diagnostics)
	default:
		*diagnostics = append(*diagnostics, fmt.Sprintf("backend %q is unsupported for transcript lookup", backend))
		return ""
	}
}

// resolveBackendTranscriptHistory returns every transcript file provably
// belonging to one external session — earlier continuation attempts plus the
// head file — in chronological order. It never returns an arbitrary newest
// file: every returned file is linked to the exact external session id, and
// for Pi the linkage is proven from the filename identity and the session
// header (id/cwd). Backends without resumable continuation files resolve to a
// single-element history.
func resolveBackendTranscriptHistory(backend string, projectCWD string, externalSessionID string, diagnostics *[]string) []string {
	sessionID := strings.TrimSpace(externalSessionID)
	if sessionID == "" {
		*diagnostics = append(*diagnostics, "external session id is empty; cannot resolve transcript history")
		return nil
	}
	switch backend {
	case "pi":
		return piTranscriptHistory(projectCWD, sessionID, diagnostics)
	default:
		path := resolveBackendTranscriptPath(backend, projectCWD, externalSessionID, diagnostics)
		if path == "" {
			return nil
		}
		return []string{path}
	}
}

// piTranscriptHistory collects every Pi transcript file whose filename encodes
// the exact external session id (fresh and resumed attempts write
// <timestamp>_<id>.jsonl files for the same session), ordered chronologically
// by the filename timestamp and then mtime. A file whose session header
// contradicts the filename identity is refused as contradictory provenance.
func piTranscriptHistory(projectCWD string, externalSessionID string, diagnostics *[]string) []string {
	root := strings.TrimSpace(piSessionTranscriptRoot)
	if root == "" {
		*diagnostics = append(*diagnostics, "Pi transcript root is not configured")
		return nil
	}
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		*diagnostics = append(*diagnostics, fmt.Sprintf("Pi transcript root not found: %s", root))
		return nil
	}
	type historyEntry struct {
		path      string
		timestamp string
		modTime   time.Time
	}
	seen := map[string]struct{}{}
	entries := []historyEntry{}
	for _, path := range candidateTranscriptFiles(root, "") {
		if !piTranscriptFileNameMatches(filepath.Base(path), externalSessionID) {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		timestamp := ""
		if headerID, _, headerTimestamp := piTranscriptHeaderIdentity(path); headerID != "" {
			if headerID != externalSessionID {
				*diagnostics = append(*diagnostics, fmt.Sprintf(
					"Pi transcript %q declares session id %q but %q was requested; refusing contradictory provenance",
					path, headerID, externalSessionID,
				))
				continue
			}
			timestamp = headerTimestamp
		}
		info, statErr := os.Stat(path)
		if statErr != nil {
			continue
		}
		entries = append(entries, historyEntry{path: path, timestamp: timestamp, modTime: info.ModTime()})
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].timestamp != entries[j].timestamp {
			return entries[i].timestamp < entries[j].timestamp
		}
		return entries[i].modTime.Before(entries[j].modTime)
	})
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		paths = append(paths, entry.path)
	}
	if len(paths) == 0 {
		*diagnostics = append(*diagnostics, fmt.Sprintf("no Pi JSONL history for external session id %q under %s", externalSessionID, root))
	}
	return paths
}

// piTranscriptHeaderIdentity reads the leading session header record of a Pi
// transcript ({"type":"session","id":...,"cwd":...,"timestamp":...}).
func piTranscriptHeaderIdentity(path string) (string, string, string) {
	file, err := os.Open(path)
	if err != nil {
		return "", "", ""
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	lines := 0
	for scanner.Scan() {
		lines++
		if lines > 8 {
			break
		}
		if id, cwd, timestamp := transcriptHeaderIdentity(scanner.Text()); id != "" || cwd != "" {
			return id, cwd, timestamp
		}
	}
	return "", "", ""
}

// ---------------------------------------------------------------------------
// Durable launcher evidence provenance
//
// The persisted external session id is written by the launcher at spawn time.
// When that registration is missing or stale (for example a launcher build
// that predates Pi session detection), the original transcript must not
// become unresolvable while it exists: Architect feedback #113 forbids curing
// a detection failure with a missing-evidence block. The fallback below
// derives the identity only from durable launcher evidence — the wave worker
// anchor (DB-recorded per-worker worktree binding) and the append-only
// attempt manifest (launcher-registered attempts) — verified against the Pi
// store with proven id/cwd identity. It never binds an arbitrary newest file,
// never binds by a shared project cwd, and refuses contradictory provenance.

// piProvenTranscript is one Pi session transcript whose identity is proven
// from the filename and the session header (matching ids, declared cwd).
type piProvenTranscript struct {
	Path      string
	Identity  string
	HeaderCWD string
	Timestamp string
	ModTime   time.Time
}

// piTranscriptProvenRecord proves one transcript file's identity the same way
// the Python launcher does: the filename identity (“transcriptFileNameSessionID“)
// and the session header identity must agree (a contradiction is refused), and
// the session header must declare the cwd so the worker binding is provable.
func piTranscriptProvenRecord(path string, diagnostics *[]string) (piProvenTranscript, bool) {
	headerID, headerCWD, headerTimestamp := piTranscriptHeaderIdentity(path)
	filenameID := transcriptFileNameSessionID(filepath.Base(path))
	if headerID != "" && filenameID != "" && headerID != filenameID &&
		!strings.Contains(filenameID, headerID) && !strings.Contains(headerID, filenameID) {
		*diagnostics = append(*diagnostics, fmt.Sprintf(
			"Pi transcript %q declares session id %q but the filename encodes %q; refusing contradictory provenance",
			path, headerID, filenameID,
		))
		return piProvenTranscript{}, false
	}
	identity := headerID
	if identity == "" {
		identity = filenameID
	}
	if identity == "" || strings.TrimSpace(headerCWD) == "" {
		// Without an id or without a declared cwd the file cannot prove whose
		// session it is; it is never bound by guesswork.
		return piProvenTranscript{}, false
	}
	info, statErr := os.Stat(path)
	if statErr != nil {
		return piProvenTranscript{}, false
	}
	return piProvenTranscript{
		Path:      path,
		Identity:  identity,
		HeaderCWD: headerCWD,
		Timestamp: headerTimestamp,
		ModTime:   info.ModTime(),
	}, true
}

// transcriptTime orders proven transcripts by their declared session
// timestamp, falling back to file mtime when the header carries none.
func (record piProvenTranscript) transcriptTime() time.Time {
	if parsed, err := time.Parse(time.RFC3339Nano, record.Timestamp); err == nil {
		return parsed
	}
	return record.ModTime
}

func sortProvenTranscripts(records []piProvenTranscript) {
	sort.SliceStable(records, func(i, j int) bool {
		left, right := records[i].transcriptTime(), records[j].transcriptTime()
		if !left.Equal(right) {
			return left.Before(right)
		}
		return records[i].ModTime.Before(records[j].ModTime)
	})
}

// provenTranscriptsBoundToCWD returns every Pi transcript provably belonging
// to one execution cwd at or after a time bound, ordered chronologically.
// The cwd binding is only used for per-worker worktree cwds (see the anchor
// caller), so a shared project cwd can never mix other sessions in.
func provenTranscriptsBoundToCWD(cwd string, notBefore time.Time, diagnostics *[]string) []piProvenTranscript {
	root := strings.TrimSpace(piSessionTranscriptRoot)
	if root == "" || strings.TrimSpace(cwd) == "" {
		return nil
	}
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		return nil
	}
	records := []piProvenTranscript{}
	for _, path := range candidateTranscriptFiles(root, "") {
		record, proven := piTranscriptProvenRecord(path, diagnostics)
		if !proven {
			continue
		}
		if !sameTranscriptCWD(record.HeaderCWD, cwd) {
			continue
		}
		if !notBefore.IsZero() && record.transcriptTime().Before(notBefore) {
			continue
		}
		records = append(records, record)
	}
	sortProvenTranscripts(records)
	return records
}

// provenTranscriptsForIdentity returns every proven Pi transcript carrying one
// exact external session id, ordered chronologically.
func provenTranscriptsForIdentity(identity string, diagnostics *[]string) []piProvenTranscript {
	root := strings.TrimSpace(piSessionTranscriptRoot)
	resolvedIdentity := strings.TrimSpace(identity)
	if root == "" || resolvedIdentity == "" {
		return nil
	}
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		return nil
	}
	records := []piProvenTranscript{}
	for _, path := range candidateTranscriptFiles(root, "") {
		record, proven := piTranscriptProvenRecord(path, diagnostics)
		if !proven {
			continue
		}
		if record.Identity != resolvedIdentity {
			continue
		}
		records = append(records, record)
	}
	sortProvenTranscripts(records)
	return records
}

// attemptManifestTranscript mirrors one continuation-evidence entry written by
// the launcher's append-only attempt manifest.
type attemptManifestTranscript struct {
	Path             string `json:"path"`
	SessionID        string `json:"session_id"`
	SessionCWD       string `json:"session_cwd"`
	SessionTimestamp string `json:"session_timestamp"`
	HeaderIdentity   string `json:"header_identity"`
	FilenameIdentity string `json:"filename_identity"`
}

// attemptManifestRecord mirrors one launcher-registered attempt.
type attemptManifestRecord struct {
	Backend              string                      `json:"backend"`
	LocalSessionID       int                         `json:"local_session_id"`
	GlobalSessionID      int                         `json:"global_session_id"`
	ExternalSessionID    string                      `json:"external_session_id"`
	LaunchStartedAtEpoch float64                     `json:"launch_started_at_epoch"`
	DetectedAtEpoch      float64                     `json:"detected_at_epoch"`
	WorkerPID            int                         `json:"worker_pid"`
	HeadlessLogPath      string                      `json:"headless_log_path"`
	WorkerCWD            string                      `json:"worker_cwd"`
	AttemptNumber        int                         `json:"attempt_number"`
	Transcripts          []attemptManifestTranscript `json:"transcripts"`
}

// attemptManifestPath is the durable append-only attempt manifest the launcher
// registers for one worker session and backend (no DB schema involved).
func attemptManifestPath(projectCWD string, localSessionID int, backend string) string {
	return filepath.Join(
		projectCWD,
		".codex",
		"netrunner_attempt_manifests",
		fmt.Sprintf("session-%d-%s.jsonl", localSessionID, backend),
	)
}

// readAttemptManifestRecords reads the append-only manifest. A malformed line
// is corrupted provenance and surfaces as an error so callers can fail closed.
func readAttemptManifestRecords(path string) ([]attemptManifestRecord, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	records := []attemptManifestRecord{}
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		raw := strings.TrimSpace(scanner.Text())
		if raw == "" {
			continue
		}
		var record attemptManifestRecord
		if err := json.Unmarshal([]byte(raw), &record); err != nil {
			return nil, fmt.Errorf("attempt manifest %s line %d is malformed: %v", path, lineNumber, err)
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return records, nil
}

// waveWorkerTranscriptAnchor is the DB-recorded launcher evidence binding one
// global session to its execution worktree.
type waveWorkerTranscriptAnchor struct {
	WorktreePath string
	CreatedAt    time.Time
}

func parseTranscriptAnchorTime(raw string) time.Time {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		if parsed, err := time.Parse(layout, trimmed); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

// fetchWaveWorkerTranscriptAnchor reads the wave worker rows recorded at
// launch. Multiple rows (relaunches) must agree on one worktree; a conflict
// is contradictory provenance and is surfaced to the caller.
func fetchWaveWorkerTranscriptAnchor(globalSessionID int) ([]waveWorkerTranscriptAnchor, bool, error) {
	if !dbTableExists("parallel_wave_worker") || !dbTableHasColumn("parallel_wave_worker", "worktree_path") {
		return nil, false, nil
	}
	rows, err := db.Query(
		`SELECT COALESCE(TRIM(worktree_path), ''), COALESCE(created_at, '')
		 FROM parallel_wave_worker
		 WHERE session_id = ?
		 ORDER BY id`,
		globalSessionID,
	)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	anchors := []waveWorkerTranscriptAnchor{}
	for rows.Next() {
		var worktreePath string
		var createdAt string
		if err := rows.Scan(&worktreePath, &createdAt); err != nil {
			return nil, false, err
		}
		anchors = append(anchors, waveWorkerTranscriptAnchor{
			WorktreePath: worktreePath,
			CreatedAt:    parseTranscriptAnchorTime(createdAt),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	return anchors, len(anchors) > 0, nil
}

// resolveTranscriptProvenanceFromDurableEvidence derives the proven worker
// head, its full attempt history, and the external session id from durable
// launcher evidence alone. It is the fail-open complement to the persisted
// external session id for Pi sessions: the original JSONL files exist and
// must be read (host feedback #113), while every binding stays proven
// (id/cwd/time/attempt) and contradictory provenance fails closed.
func resolveTranscriptProvenanceFromDurableEvidence(
	globalSessionID int,
	localSessionID int,
	projectCWD string,
	backend string,
	diagnostics *[]string,
) (piProvenTranscript, []piProvenTranscript, string) {
	if backend != "pi" {
		// Only the Pi store carries proven id/cwd session headers; other
		// backends resolve through their persisted external session id.
		return piProvenTranscript{}, nil, ""
	}

	provenance := map[string]piProvenTranscript{}

	// 1) Wave worker anchor: the DB-recorded per-worker worktree binding.
	// The cwd scan is only safe for that per-worker worktree; a shared
	// project cwd must never bind sessions by recency.
	anchors, foundAnchor, err := fetchWaveWorkerTranscriptAnchor(globalSessionID)
	if err != nil {
		*diagnostics = append(*diagnostics, fmt.Sprintf("failed to read wave worker provenance anchor: %v", err))
		return piProvenTranscript{}, nil, ""
	}
	if foundAnchor {
		worktrees := map[string]struct{}{}
		createdAt := time.Time{}
		for _, anchor := range anchors {
			resolved := anchor.WorktreePath
			if resolved != "" && !filepath.IsAbs(resolved) {
				resolved = filepath.Join(projectCWD, resolved)
			}
			if resolved != "" {
				worktrees[resolved] = struct{}{}
			}
			if !anchor.CreatedAt.IsZero() && (createdAt.IsZero() || anchor.CreatedAt.Before(createdAt)) {
				createdAt = anchor.CreatedAt
			}
		}
		if len(worktrees) > 1 {
			*diagnostics = append(*diagnostics, "contradictory provenance: wave worker rows bind one session to multiple worktrees")
			return piProvenTranscript{}, nil, ""
		}
		for worktree := range worktrees {
			if sameTranscriptCWD(worktree, projectCWD) {
				*diagnostics = append(*diagnostics, "wave worker anchor points at the shared project cwd; refusing cwd-only binding across sessions")
				continue
			}
			notBefore := time.Time{}
			if !createdAt.IsZero() {
				notBefore = createdAt.Add(-15 * time.Minute)
			}
			for _, record := range provenTranscriptsBoundToCWD(worktree, notBefore, diagnostics) {
				provenance[record.Path] = record
			}
		}
	}

	// 2) Attempt manifest: launcher-registered attempts with their recorded
	// continuation evidence. Every recorded claim is re-verified against the
	// original files before it can bind anything.
	manifestRecords, err := readAttemptManifestRecords(attemptManifestPath(projectCWD, localSessionID, backend))
	if err != nil {
		*diagnostics = append(*diagnostics, fmt.Sprintf("contradictory provenance: %v", err))
		return piProvenTranscript{}, nil, ""
	}
	manifestHeadID := ""
	for _, record := range manifestRecords {
		claimedID := strings.TrimSpace(record.ExternalSessionID)
		for _, entry := range record.Transcripts {
			probe := strings.TrimSpace(entry.Path)
			if probe == "" {
				continue
			}
			proven, ok := piTranscriptProvenRecord(probe, diagnostics)
			if !ok {
				*diagnostics = append(*diagnostics, fmt.Sprintf(
					"contradictory provenance: attempt manifest references %q without a proven transcript identity", probe,
				))
				return piProvenTranscript{}, nil, ""
			}
			if entry.SessionID != "" && proven.Identity != entry.SessionID {
				*diagnostics = append(*diagnostics, fmt.Sprintf(
					"contradictory provenance: attempt manifest claims session id %q for %q but the file proves %q",
					entry.SessionID, probe, proven.Identity,
				))
				return piProvenTranscript{}, nil, ""
			}
			if claimedID != "" && proven.Identity != claimedID {
				*diagnostics = append(*diagnostics, fmt.Sprintf(
					"contradictory provenance: attempt manifest claims external session id %q but %q proves %q",
					claimedID, probe, proven.Identity,
				))
				return piProvenTranscript{}, nil, ""
			}
			expectedCWD := strings.TrimSpace(entry.SessionCWD)
			if expectedCWD == "" {
				expectedCWD = strings.TrimSpace(record.WorkerCWD)
			}
			if expectedCWD == "" || !sameTranscriptCWD(proven.HeaderCWD, expectedCWD) {
				*diagnostics = append(*diagnostics, fmt.Sprintf(
					"contradictory provenance: attempt manifest cwd %q does not match the session header cwd %q of %q",
					expectedCWD, proven.HeaderCWD, probe,
				))
				return piProvenTranscript{}, nil, ""
			}
			provenance[proven.Path] = proven
		}
		if claimedID != "" {
			matched := false
			for _, proven := range provenance {
				if proven.Identity == claimedID {
					matched = true
					break
				}
			}
			if !matched {
				for _, proven := range provenTranscriptsForIdentity(claimedID, diagnostics) {
					provenance[proven.Path] = proven
					matched = true
				}
			}
			if !matched {
				*diagnostics = append(*diagnostics, fmt.Sprintf(
					"contradictory provenance: attempt manifest claims external session id %q with no proven transcript file",
					claimedID,
				))
				return piProvenTranscript{}, nil, ""
			}
			manifestHeadID = claimedID
		}
	}

	if len(provenance) == 0 {
		*diagnostics = append(*diagnostics, "no durable launcher evidence binds a Pi transcript to this worker")
		return piProvenTranscript{}, nil, ""
	}
	history := make([]piProvenTranscript, 0, len(provenance))
	for _, record := range provenance {
		history = append(history, record)
	}
	sortProvenTranscripts(history)
	head := history[len(history)-1]
	if manifestHeadID != "" && manifestHeadID != head.Identity {
		*diagnostics = append(*diagnostics, fmt.Sprintf(
			"contradictory provenance: the latest attempt manifest record claims %q but the proven worker head is %q",
			manifestHeadID, head.Identity,
		))
		return piProvenTranscript{}, nil, ""
	}
	return head, history, head.Identity
}

// resolveWorkerTranscriptProvenance resolves the head transcript, the full
// attempt history, and the external session id for one netrunner session. The
// persisted external session id stays authoritative when it is present and
// current; when it is missing — or stale after a relaunch whose detection
// never landed — durable launcher evidence recovers the proven identity and
// the link is refreshed so later lookups stay stable.
func resolveWorkerTranscriptProvenance(
	globalSessionID int,
	localSessionID int,
	backend string,
	projectCWD string,
	diagnostics *[]string,
) (string, []string, string) {
	externalSessionID, err := fetchSessionExternalID(globalSessionID, backend)
	if err != nil {
		*diagnostics = append(*diagnostics, fmt.Sprintf("failed to resolve external session id: %v", err))
		return "", nil, ""
	}

	provenHead := piProvenTranscript{}
	provenHistory := []piProvenTranscript{}
	provenID := ""
	if backend == "pi" {
		provenHead, provenHistory, provenID = resolveTranscriptProvenanceFromDurableEvidence(
			globalSessionID, localSessionID, projectCWD, backend, diagnostics,
		)
	}

	resolvedID := strings.TrimSpace(externalSessionID)
	if resolvedID == "" {
		if provenID == "" {
			*diagnostics = append(*diagnostics, "transcript unavailable: no persisted external session id; transcript identity cannot be proven")
			return "", nil, ""
		}
		if err := persistDiscoveredSessionExternalID(globalSessionID, backend, provenID); err != nil {
			*diagnostics = append(*diagnostics, fmt.Sprintf("failed to persist discovered external session id %q: %v", provenID, err))
		}
		*diagnostics = append(*diagnostics, fmt.Sprintf(
			"external session id %q recovered from durable launcher evidence (attempt manifest / wave worker anchor + Pi store proven identity)",
			provenID,
		))
		resolvedID = provenID
	}

	if len(provenHistory) > 0 {
		if provenID != resolvedID {
			// The durable evidence proves a later attempt than the persisted
			// link (a relaunch whose detection never landed). The time-proven
			// worker head and its full history win; the link is refreshed.
			if err := persistDiscoveredSessionExternalID(globalSessionID, backend, provenID); err != nil {
				*diagnostics = append(*diagnostics, fmt.Sprintf("failed to refresh persisted external session id %q: %v", provenID, err))
			}
			*diagnostics = append(*diagnostics, fmt.Sprintf(
				"persisted external session id %q is stale for this worker; proven head %q and its full attempt history are used",
				resolvedID, provenID,
			))
			resolvedID = provenID
		}
		history := make([]string, 0, len(provenHistory))
		for _, record := range provenHistory {
			history = append(history, record.Path)
		}
		return provenHead.Path, history, resolvedID
	}

	history := resolveBackendTranscriptHistory(backend, projectCWD, resolvedID, diagnostics)
	if len(history) == 0 {
		return "", nil, resolvedID
	}
	head := resolveBackendTranscriptPath(backend, projectCWD, resolvedID, diagnostics)
	if strings.TrimSpace(head) == "" {
		head = history[len(history)-1]
	}
	return head, history, resolvedID
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
	transcriptPath, _, resolvedExternalSessionID := resolveWorkerTranscriptProvenance(
		globalSessionID, input.SessionId, backend, projectCWD, &diagnostics,
	)
	if strings.TrimSpace(resolvedExternalSessionID) != "" {
		externalSessionID = resolvedExternalSessionID
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
