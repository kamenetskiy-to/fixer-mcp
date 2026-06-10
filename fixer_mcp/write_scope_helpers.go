package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

var foundationWriteScopeSegments = map[string]struct{}{
	"auth":      {},
	"bootstrap": {},
	"cmd":       {},
	"core":      {},
	"database":  {},
	"db":        {},
	"internal":  {},
	"model":     {},
	"models":    {},
	"pkg":       {},
	"runtime":   {},
	"storage":   {},
}

func containsFoundationWriteScope(scope []string) bool {
	for _, entry := range scope {
		if entry == defaultWriteScopePath {
			return true
		}
		segments := strings.Split(entry, "/")
		for _, segment := range segments {
			if _, exists := foundationWriteScopeSegments[segment]; exists {
				return true
			}
		}
	}
	return false
}
func normalizeWriteScopePath(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("write scope entries must be non-empty project-relative paths")
	}
	if filepath.IsAbs(trimmed) {
		return "", fmt.Errorf("write scope entries must be project-relative paths: %q", raw)
	}

	cleaned := filepath.ToSlash(filepath.Clean(trimmed))
	if cleaned == "." {
		return cleaned, nil
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("write scope entries must stay within the project root: %q", raw)
	}
	return strings.TrimPrefix(cleaned, "./"), nil
}

func normalizeDeclaredWriteScope(raw []string) ([]string, error) {
	if len(raw) == 0 {
		raw = []string{defaultWriteScopePath}
	}

	seen := make(map[string]struct{}, len(raw))
	scope := make([]string, 0, len(raw))
	for _, entry := range raw {
		normalized, err := normalizeWriteScopePath(entry)
		if err != nil {
			return nil, err
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		scope = append(scope, normalized)
	}
	if len(scope) == 0 {
		return nil, fmt.Errorf("declared_write_scope must contain at least one project-relative path")
	}
	sort.Strings(scope)
	return scope, nil
}

func encodeDeclaredWriteScope(scope []string) (string, error) {
	normalized, err := normalizeDeclaredWriteScope(scope)
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

func decodeDeclaredWriteScope(raw string) ([]string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return []string{}, nil
	}
	var scope []string
	if err := json.Unmarshal([]byte(trimmed), &scope); err != nil {
		return nil, fmt.Errorf("invalid declared_write_scope payload: %w", err)
	}
	return normalizeDeclaredWriteScope(scope)
}

func writeScopePathsOverlap(left, right string) bool {
	if left == defaultWriteScopePath || right == defaultWriteScopePath {
		return true
	}
	return left == right ||
		strings.HasPrefix(left, right+"/") ||
		strings.HasPrefix(right, left+"/")
}

func writeScopesOverlap(left, right []string) bool {
	for _, leftPath := range left {
		for _, rightPath := range right {
			if writeScopePathsOverlap(leftPath, rightPath) {
				return true
			}
		}
	}
	return false
}
