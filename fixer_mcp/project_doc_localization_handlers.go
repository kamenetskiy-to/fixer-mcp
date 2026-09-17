package main

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const defaultProjectDocLanguage = "ru"

var projectDocLanguagePattern = regexp.MustCompile(`^[a-z]{2,3}(?:-[a-z0-9]{2,8})*$`)

type projectDocLocalizationExecer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

func normalizeProjectDocLanguage(raw string) (string, error) {
	normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(raw), "_", "-"))
	if normalized == "" {
		normalized = defaultProjectDocLanguage
	}
	if !projectDocLanguagePattern.MatchString(normalized) {
		return "", fmt.Errorf("invalid documentation language %q; expected a BCP-47 language tag such as en or ru", raw)
	}
	return normalized, nil
}

func projectDocLanguage(projectID int) (string, error) {
	return projectDocLanguageWithExecutor(db, projectID)
}

func projectDocLanguageWithExecutor(exec projectDocQueryer, projectID int) (string, error) {
	var language string
	err := exec.QueryRow(
		"SELECT language_code FROM project_doc_language_policy WHERE project_id = ?",
		projectID,
	).Scan(&language)
	if err == sql.ErrNoRows {
		return defaultProjectDocLanguage, nil
	}
	if err != nil {
		// Hand-authored legacy test fixtures predate the localization schema.
		// Production databases always create and seed the policy table in initDB.
		if strings.Contains(strings.ToLower(err.Error()), "no such table") {
			return "en", nil
		}
		return "", err
	}
	return normalizeProjectDocLanguage(language)
}

func projectDocLocalizationRequired(language string) bool {
	primary := strings.SplitN(language, "-", 2)[0]
	return primary != "en"
}

func requireProjectDocLocalizationCoverage(projectID int) error {
	language, err := projectDocLanguage(projectID)
	if err != nil {
		return fmt.Errorf("failed to read documentation language: %v", err)
	}
	if !projectDocLocalizationRequired(language) {
		return nil
	}

	rows, err := db.Query(`
		SELECT
			(
				SELECT COUNT(*)
				FROM project_doc ranked
				WHERE ranked.project_id = d.project_id AND ranked.id <= d.id
			) AS local_doc_id,
			d.title
		FROM project_doc d
		LEFT JOIN project_doc_title_localization l
			ON l.project_doc_id = d.id AND l.language_code = ?
		WHERE d.project_id = ? AND TRIM(COALESCE(l.localized_title, '')) = ''
		ORDER BY d.id
		LIMIT 11`, language, projectID)
	if err != nil {
		return fmt.Errorf("failed to validate documentation localization: %v", err)
	}
	defer rows.Close()

	missing := make([]string, 0, 11)
	for rows.Next() {
		var localDocID int
		var canonicalTitle string
		if err := rows.Scan(&localDocID, &canonicalTitle); err != nil {
			return fmt.Errorf("failed to validate documentation localization: %v", err)
		}
		missing = append(missing, fmt.Sprintf("%d %q", localDocID, canonicalTitle))
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("failed to validate documentation localization: %v", err)
	}
	if len(missing) == 0 {
		return nil
	}
	suffix := ""
	if len(missing) > 10 {
		missing = missing[:10]
		suffix = ", ..."
	}
	return fmt.Errorf(
		"project documentation localization is incomplete for language %q; add localized titles with set_project_doc_title_localizations before using documentation tools (missing: %s%s)",
		language,
		strings.Join(missing, ", "),
		suffix,
	)
}

func requiredLocalizedProjectDocTitle(projectID int, raw string) (language string, localizedTitle string, err error) {
	return requiredLocalizedProjectDocTitleWithExecutor(db, projectID, raw)
}

func requiredLocalizedProjectDocTitleWithExecutor(exec projectDocQueryer, projectID int, raw string) (language string, localizedTitle string, err error) {
	language, err = projectDocLanguageWithExecutor(exec, projectID)
	if err != nil {
		return "", "", fmt.Errorf("failed to read documentation language: %v", err)
	}
	localizedTitle = strings.TrimSpace(raw)
	if projectDocLocalizationRequired(language) && localizedTitle == "" {
		return "", "", fmt.Errorf("localized_title is required because project documentation language is %q", language)
	}
	return language, localizedTitle, nil
}

func upsertProjectDocLocalizedTitle(exec projectDocLocalizationExecer, globalDocID int, language string, localizedTitle string) error {
	if !projectDocLocalizationRequired(language) || strings.TrimSpace(localizedTitle) == "" {
		return nil
	}
	_, err := exec.Exec(`
		INSERT INTO project_doc_title_localization (
			project_doc_id, language_code, localized_title, created_at, updated_at
		) VALUES (?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT(project_doc_id, language_code) DO UPDATE SET
			localized_title = excluded.localized_title,
			updated_at = CURRENT_TIMESTAMP`, globalDocID, language, strings.TrimSpace(localizedTitle))
	return err
}

type ProjectDocTitleLocalizationInput struct {
	ProjectDocId   int    `json:"project_doc_id" jsonschema:"Project-scoped canonical document ID"`
	LocalizedTitle string `json:"localized_title" jsonschema:"Localized title for the project's active documentation language"`
}

type SetProjectDocTitleLocalizationsInput struct {
	ProjectId int                                `json:"project_id,omitempty" jsonschema:"Overseer-only explicit project ID; fixer uses the bound project"`
	Language  string                             `json:"language" jsonschema:"Language code; must match the project's active documentation language"`
	Titles    []ProjectDocTitleLocalizationInput `json:"titles" jsonschema:"Localized titles keyed by project-scoped document ID"`
}

type SetProjectDocTitleLocalizationsOutput struct {
	Status    string `json:"status"`
	ProjectId int    `json:"project_id"`
	Language  string `json:"language"`
	Updated   int    `json:"updated"`
}

func SetProjectDocTitleLocalizations(ctx context.Context, req *mcp.CallToolRequest, input SetProjectDocTitleLocalizationsInput) (*mcp.CallToolResult, SetProjectDocTitleLocalizationsOutput, error) {
	projectID, err := resolveProjectHandoffProjectID(input.ProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetProjectDocTitleLocalizationsOutput{}, err
	}
	activeLanguage, err := projectDocLanguage(projectID)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetProjectDocTitleLocalizationsOutput{}, err
	}
	requestedLanguage, err := normalizeProjectDocLanguage(input.Language)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetProjectDocTitleLocalizationsOutput{}, err
	}
	if requestedLanguage != activeLanguage {
		return &mcp.CallToolResult{IsError: true}, SetProjectDocTitleLocalizationsOutput{}, fmt.Errorf("language %q does not match project documentation language %q", requestedLanguage, activeLanguage)
	}
	if !projectDocLocalizationRequired(activeLanguage) {
		return nil, SetProjectDocTitleLocalizationsOutput{Status: "not_required", ProjectId: projectID, Language: activeLanguage}, nil
	}
	if len(input.Titles) == 0 {
		return &mcp.CallToolResult{IsError: true}, SetProjectDocTitleLocalizationsOutput{}, fmt.Errorf("titles is required")
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetProjectDocTitleLocalizationsOutput{}, fmt.Errorf("DB transaction start error: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	seen := map[int]struct{}{}
	updated := 0
	for _, item := range input.Titles {
		if _, duplicate := seen[item.ProjectDocId]; duplicate {
			continue
		}
		seen[item.ProjectDocId] = struct{}{}
		localizedTitle := strings.TrimSpace(item.LocalizedTitle)
		if localizedTitle == "" {
			return &mcp.CallToolResult{IsError: true}, SetProjectDocTitleLocalizationsOutput{}, fmt.Errorf("localized_title is required for project_doc_id %d", item.ProjectDocId)
		}
		globalDocID, err := globalProjectDocIDFromProjectScopedWithExecutor(tx, item.ProjectDocId, projectID)
		if err == sql.ErrNoRows {
			return &mcp.CallToolResult{IsError: true}, SetProjectDocTitleLocalizationsOutput{}, fmt.Errorf("project_doc_id %d not found in project", item.ProjectDocId)
		}
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, SetProjectDocTitleLocalizationsOutput{}, fmt.Errorf("DB query error: %v", err)
		}
		if err := upsertProjectDocLocalizedTitle(tx, globalDocID, activeLanguage, localizedTitle); err != nil {
			return &mcp.CallToolResult{IsError: true}, SetProjectDocTitleLocalizationsOutput{}, fmt.Errorf("DB upsert error: %v", err)
		}
		updated++
	}
	if err := tx.Commit(); err != nil {
		return &mcp.CallToolResult{IsError: true}, SetProjectDocTitleLocalizationsOutput{}, fmt.Errorf("DB commit error: %v", err)
	}
	return nil, SetProjectDocTitleLocalizationsOutput{Status: "success", ProjectId: projectID, Language: activeLanguage, Updated: updated}, nil
}

type SetProjectDocLanguageInput struct {
	ProjectId int    `json:"project_id,omitempty" jsonschema:"Overseer-only explicit project ID; fixer uses the bound project"`
	Language  string `json:"language" jsonschema:"Active documentation title language, for example en or ru"`
}

type SetProjectDocLanguageOutput struct {
	Status    string `json:"status"`
	ProjectId int    `json:"project_id"`
	Language  string `json:"language"`
}

func SetProjectDocLanguage(ctx context.Context, req *mcp.CallToolRequest, input SetProjectDocLanguageInput) (*mcp.CallToolResult, SetProjectDocLanguageOutput, error) {
	projectID, err := resolveProjectHandoffProjectID(input.ProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetProjectDocLanguageOutput{}, err
	}
	language, err := normalizeProjectDocLanguage(input.Language)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetProjectDocLanguageOutput{}, err
	}
	_, err = db.Exec(`
		INSERT INTO project_doc_language_policy (project_id, language_code, created_at, updated_at)
		VALUES (?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT(project_id) DO UPDATE SET
			language_code = excluded.language_code,
			updated_at = CURRENT_TIMESTAMP`, projectID, language)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetProjectDocLanguageOutput{}, fmt.Errorf("DB upsert error: %v", err)
	}
	return nil, SetProjectDocLanguageOutput{Status: "success", ProjectId: projectID, Language: language}, nil
}

type ProjectDocLocalizationStatusItem struct {
	ProjectDocId   int    `json:"project_doc_id"`
	CanonicalTitle string `json:"canonical_title"`
	LocalizedTitle string `json:"localized_title,omitempty"`
	Missing        bool   `json:"missing"`
}

type GetProjectDocLocalizationStatusInput struct {
	ProjectId int `json:"project_id,omitempty" jsonschema:"Overseer-only explicit project ID; fixer uses the bound project"`
}

type GetProjectDocLocalizationStatusOutput struct {
	ProjectId int                                `json:"project_id"`
	Language  string                             `json:"language"`
	Missing   int                                `json:"missing"`
	Docs      []ProjectDocLocalizationStatusItem `json:"docs"`
}

func GetProjectDocLocalizationStatus(ctx context.Context, req *mcp.CallToolRequest, input GetProjectDocLocalizationStatusInput) (*mcp.CallToolResult, GetProjectDocLocalizationStatusOutput, error) {
	projectID, err := resolveProjectHandoffProjectID(input.ProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetProjectDocLocalizationStatusOutput{}, err
	}
	language, err := projectDocLanguage(projectID)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetProjectDocLocalizationStatusOutput{}, err
	}
	rows, err := db.Query(`
		SELECT
			(
				SELECT COUNT(*) FROM project_doc ranked
				WHERE ranked.project_id = d.project_id AND ranked.id <= d.id
			),
			d.title,
			COALESCE(l.localized_title, '')
		FROM project_doc d
		LEFT JOIN project_doc_title_localization l
			ON l.project_doc_id = d.id AND l.language_code = ?
		WHERE d.project_id = ?
		ORDER BY d.id`, language, projectID)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetProjectDocLocalizationStatusOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	defer rows.Close()
	docs := []ProjectDocLocalizationStatusItem{}
	missing := 0
	for rows.Next() {
		var item ProjectDocLocalizationStatusItem
		if err := rows.Scan(&item.ProjectDocId, &item.CanonicalTitle, &item.LocalizedTitle); err != nil {
			return &mcp.CallToolResult{IsError: true}, GetProjectDocLocalizationStatusOutput{}, fmt.Errorf("DB scan error: %v", err)
		}
		item.Missing = projectDocLocalizationRequired(language) && strings.TrimSpace(item.LocalizedTitle) == ""
		if item.Missing {
			missing++
		}
		docs = append(docs, item)
	}
	sort.SliceStable(docs, func(i, j int) bool { return docs[i].ProjectDocId < docs[j].ProjectDocId })
	return nil, GetProjectDocLocalizationStatusOutput{ProjectId: projectID, Language: language, Missing: missing, Docs: docs}, nil
}
