package dashboardapi

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func (r *Repository) tableExists(ctx context.Context, tableName string) bool {
	var found string
	err := r.db.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, tableName).Scan(&found)
	return err == nil && found == tableName
}

func normalizeProjectCWD(raw string) (string, error) {
	cwd := strings.TrimSpace(raw)
	if cwd == "" {
		return "", fmt.Errorf("cwd is required")
	}
	if !filepath.IsAbs(cwd) {
		return "", fmt.Errorf("cwd must be an absolute path")
	}
	normalized := filepath.Clean(cwd)
	if resolved, err := filepath.EvalSymlinks(normalized); err == nil {
		normalized = filepath.Clean(resolved)
	}
	return normalized, nil
}

func resolveDatabasePath(raw string) (string, error) {
	candidates := []string{}
	if trimmed := strings.TrimSpace(raw); trimmed != "" {
		candidates = append(candidates, trimmed)
	}
	if env := strings.TrimSpace(os.Getenv("FIXER_MCP_DB_PATH")); env != "" {
		candidates = append(candidates, env)
	}
	if env := strings.TrimSpace(os.Getenv("FIXER_DB_PATH")); env != "" {
		candidates = append(candidates, env)
	}
	cwd, _ := os.Getwd()
	candidates = append(candidates,
		filepath.Join(cwd, defaultFixerDBFilename),
		filepath.Join(cwd, "..", defaultFixerDBFilename),
		filepath.Join(cwd, "..", "..", defaultFixerDBFilename),
	)
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate) == "" {
			continue
		}
		if _, err := os.Stat(candidate); err == nil {
			return filepath.Clean(candidate), nil
		}
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("could not resolve fixer.db path")
	}
	return filepath.Clean(candidates[0]), nil
}

func normalizeStatuses(statuses []string) []string {
	seen := map[string]struct{}{}
	normalized := []string{}
	for _, status := range statuses {
		trimmed := strings.TrimSpace(status)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		normalized = append(normalized, trimmed)
	}
	sort.Strings(normalized)
	return normalized
}

func normalizeIntIDs(values []int) []int {
	seen := map[int]struct{}{}
	normalized := []int{}
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}
	sort.Ints(normalized)
	return normalized
}

func normalizeStringIDs(values []string) []string {
	seen := map[string]struct{}{}
	normalized := []string{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		normalized = append(normalized, trimmed)
	}
	sort.Strings(normalized)
	return normalized
}

func encodeStringList(values []string) (string, error) {
	normalized := normalizeStringIDs(values)
	if len(normalized) == 0 {
		normalized = []string{"."}
	}
	raw, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}
func decodeStringList(raw string) []string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return []string{"."}
	}
	var values []string
	if err := json.Unmarshal([]byte(trimmed), &values); err == nil && len(values) > 0 {
		result := []string{}
		for _, value := range values {
			value = strings.TrimSpace(value)
			if value != "" {
				result = append(result, value)
			}
		}
		if len(result) > 0 {
			return result
		}
	}
	return []string{"."}
}

func decodeStructuredFinalReport(raw string) *FinalReport {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || !strings.HasPrefix(trimmed, "{") {
		return nil
	}
	var report FinalReport
	if err := json.Unmarshal([]byte(trimmed), &report); err != nil {
		return nil
	}
	if len(report.FilesChanged) == 0 && len(report.CommandsRun) == 0 && len(report.ChecksRun) == 0 && len(report.Blockers) == 0 {
		return nil
	}
	return &report
}

func summarizeContent(raw string) string {
	return preview(strings.TrimSpace(raw), 160)
}

func preview(raw string, limit int) string {
	compacted := strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
	if compacted == "" {
		return ""
	}
	if len(compacted) <= limit {
		return compacted
	}
	if limit < 4 {
		return compacted[:limit]
	}
	return compacted[:limit-3] + "..."
}

func firstLineOrFallback(raw string, fallback string) string {
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			return preview(trimmed, 120)
		}
	}
	return fallback
}

func latestActivity(sessions []NetrunnerSummary) (string, int, int) {
	if len(sessions) == 0 {
		return "No sessions yet", 0, 0
	}
	last := sessions[len(sessions)-1]
	return fmt.Sprintf("%s (%s)", last.Headline, last.Status), last.ID, last.LocalID
}

func placeholderFixerChatBinding(projectID int, residualRisk string) FixerChatBinding {
	return FixerChatBinding{
		ProjectID:              projectID,
		Supported:              false,
		Sessions:               []FixerChatSessionSummary{},
		TranscriptAvailability: "metadata_only",
		ResidualRisk:           residualRisk,
	}
}

func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(os.Signal(nil)) == nil
}
