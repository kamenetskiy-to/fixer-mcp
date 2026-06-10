package main

import (
	"database/sql"
	"fmt"
	"strings"
)

func normalizeCliBackend(raw string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	if normalized == "" {
		return defaultCliBackend, nil
	}
	if alias, ok := cliBackendAliases[normalized]; ok {
		normalized = alias
	}
	if _, ok := supportedCliBackends[normalized]; !ok {
		return "", fmt.Errorf("unsupported backend %q", raw)
	}
	return normalized, nil
}

func normalizeCliModel(backend string, raw string) string {
	model := strings.TrimSpace(raw)
	if model == "" {
		return ""
	}
	if backend == "droid" || backend == "junie" {
		if alias, ok := droidLegacyModelAliases[strings.ToLower(model)]; ok {
			return alias
		}
	}
	return model
}

func defaultCliModelForBackend(backend string) string {
	if backend == "droid" || backend == "junie" {
		return defaultDroidCliModel
	}
	return defaultCliModel
}

func defaultCliReasoningForBackend(backend string) string {
	if backend == "junie" {
		return defaultJunieCliReasoning
	}
	return defaultCliReasoning
}

func normalizeCliReasoning(backend string, raw string) string {
	reasoning := strings.TrimSpace(raw)
	if backend == "droid" && reasoning == "none" {
		return defaultCliReasoning
	}
	return reasoning
}

func validateCliModelForBackend(backend string, model string) error {
	if backend != "droid" && backend != "junie" {
		return nil
	}
	trimmedModel := strings.TrimSpace(model)
	if trimmedModel == "" {
		return nil
	}
	if _, ok := supportedDroidCliModels[trimmedModel]; ok {
		return nil
	}
	return fmt.Errorf("unsupported %s model %q; supported models: kimi-k2.6, glm-5.1", backend, trimmedModel)
}

func validateCliReasoningForBackend(backend string, reasoning string) error {
	trimmedReasoning := strings.TrimSpace(reasoning)
	if backend != "junie" || trimmedReasoning == "" || trimmedReasoning == defaultJunieCliReasoning {
		return nil
	}
	return fmt.Errorf("unsupported junie reasoning %q; supported reasoning values: default", trimmedReasoning)
}

func fetchSessionExternalID(sessionID int, backend string) (string, error) {
	normalizedBackend, err := normalizeCliBackend(backend)
	if err != nil {
		return "", err
	}

	var externalSessionID string
	err = db.QueryRow(
		`SELECT external_session_id
		 FROM session_external_link
		 WHERE session_id = ? AND backend = ?
		 ORDER BY COALESCE(updated_at, '') DESC, id DESC
		 LIMIT 1`,
		sessionID,
		normalizedBackend,
	).Scan(&externalSessionID)
	if err == nil {
		return externalSessionID, nil
	}
	if err != nil && err != sql.ErrNoRows {
		return "", err
	}

	if normalizedBackend != defaultCliBackend {
		return "", nil
	}

	var legacyCodexSessionID string
	err = db.QueryRow(
		`SELECT codex_session_id
		 FROM session_codex_link
		 WHERE session_id = ?`,
		sessionID,
	).Scan(&legacyCodexSessionID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return legacyCodexSessionID, nil
}
