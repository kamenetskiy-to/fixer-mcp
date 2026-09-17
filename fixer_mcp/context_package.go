package main

import (
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

// contextPackageSchemaVersion is the current portable project-context package
// schema version. Import refuses packages with a different major version.
const contextPackageSchemaVersion = 1

// contextPackageSessionExcerptLen bounds the task_description excerpt carried
// by the lightweight session index inside a context package.
const contextPackageSessionExcerptLen = 300

// ContextPackageManifest describes where and when a package was exported.
type ContextPackageManifest struct {
	SchemaVersion         int    `json:"schema_version"`
	ExportedAt            string `json:"exported_at"`
	ProjectName           string `json:"project_name"`
	ProjectRoot           string `json:"project_root"`
	DocumentationLanguage string `json:"documentation_language,omitempty"`
}

// ContextPackageDoc carries one canonical project doc. Parent linkage is
// expressed via the parent's slug/path, never via raw numeric doc ids, so the
// tree can be recreated on a machine where ids differ.
type ContextPackageDoc struct {
	Title          string `json:"title"`
	LocalizedTitle string `json:"localized_title,omitempty"`
	Content        string `json:"content"`
	DocType        string `json:"doc_type"`
	Level          int    `json:"level"`
	Slug           string `json:"slug"`
	Path           string `json:"path"`
	Status         string `json:"status"`
	ParentSlug     string `json:"parent_slug,omitempty"`
	ParentPath     string `json:"parent_path,omitempty"`
}

// ContextPackageBacklogItem carries one backlog item without machine-local ids.
type ContextPackageBacklogItem struct {
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Priority    string `json:"priority,omitempty"`
	Status      string `json:"status,omitempty"`
}

// ContextPackageSession is a lightweight history index entry only: no
// netrunner logs and no transcripts are exported (documentation/history split).
type ContextPackageSession struct {
	SessionId   int    `json:"session_id"`
	Status      string `json:"status"`
	TaskExcerpt string `json:"task_excerpt"`
}

// ProjectContextPackage is the portable project-context package file format.
type ProjectContextPackage struct {
	Manifest ContextPackageManifest      `json:"manifest"`
	Overview string                      `json:"overview,omitempty"`
	Handoff  string                      `json:"handoff,omitempty"`
	Docs     []ContextPackageDoc         `json:"docs"`
	Backlog  []ContextPackageBacklogItem `json:"backlog"`
	Sessions []ContextPackageSession     `json:"sessions"`
}

type ExportProjectContextPackageInput struct {
	ProjectId int    `json:"project_id,omitempty" jsonschema:"Optional project ID when called by overseer; fixer uses the bound project."`
	Path      string `json:"path,omitempty" jsonschema:"Optional output file path. Defaults to artifacts/context_packages/<project-slug>-<timestamp>.json under the project root; relative paths resolve against the project root."`
}

type ExportProjectContextPackageOutput struct {
	Status        string `json:"status"`
	Path          string `json:"path"`
	ProjectId     int    `json:"project_id"`
	DocsCount     int    `json:"docs_count"`
	BacklogCount  int    `json:"backlog_count"`
	SessionsCount int    `json:"sessions_count"`
	HasOverview   bool   `json:"has_overview"`
	HasHandoff    bool   `json:"has_handoff"`
}

type ImportProjectContextPackageInput struct {
	ProjectId int    `json:"project_id,omitempty" jsonschema:"Optional project ID when called by overseer; fixer uses the bound project."`
	Path      string `json:"path" jsonschema:"Path to the context package JSON file produced by export_project_context_package."`
	Force     bool   `json:"force,omitempty" jsonschema:"Required when the target project already has docs or a handoff. Without force the import is refused to avoid mixing trees."`
}

type ImportProjectContextPackageOutput struct {
	Status            string `json:"status"`
	Path              string `json:"path"`
	ProjectId         int    `json:"project_id"`
	DocsCreated       int    `json:"docs_created"`
	BacklogInserted   int    `json:"backlog_inserted"`
	BacklogSkipped    int    `json:"backlog_skipped"`
	OverviewSet       bool   `json:"overview_set"`
	HandoffSet        bool   `json:"handoff_set"`
	SessionsInPackage int    `json:"sessions_in_package"`
}

func fetchProjectContextDocs(projectID int) ([]ContextPackageDoc, error) {
	language, err := projectDocLanguage(projectID)
	if err != nil {
		return nil, err
	}
	localized := projectDocLocalizationRequired(language)
	localizedTitleExpr := "''"
	localizationJoin := ""
	args := []any{projectID}
	if localized {
		localizedTitleExpr = "COALESCE(l.localized_title, '')"
		localizationJoin = "LEFT JOIN project_doc_title_localization l ON l.project_doc_id = d.id AND l.language_code = ?"
		args = []any{language, projectID}
	}
	rows, err := db.Query(
		fmt.Sprintf(`SELECT d.id,
		        d.title,
		        %s,
		        d.content,
		        COALESCE(d.doc_type, 'documentation'),
		        d.parent_doc_id,
		        COALESCE(d.level, 0),
		        COALESCE(d.slug, ''),
		        COALESCE(d.path, ''),
		        COALESCE(d.status, 'current')
		 FROM project_doc d
		 %s
		 WHERE d.project_id = ?
		 ORDER BY d.id`, localizedTitleExpr, localizationJoin),
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type rawDoc struct {
		doc      ContextPackageDoc
		globalID int
		parentID int
	}
	raw := []rawDoc{}
	for rows.Next() {
		var entry rawDoc
		var parentID sql.NullInt64
		if err := rows.Scan(
			&entry.globalID,
			&entry.doc.Title,
			&entry.doc.LocalizedTitle,
			&entry.doc.Content,
			&entry.doc.DocType,
			&parentID,
			&entry.doc.Level,
			&entry.doc.Slug,
			&entry.doc.Path,
			&entry.doc.Status,
		); err != nil {
			return nil, err
		}
		if parentID.Valid {
			entry.parentID = int(parentID.Int64)
		}
		raw = append(raw, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	byGlobalID := make(map[int]ContextPackageDoc, len(raw))
	for _, entry := range raw {
		byGlobalID[entry.globalID] = entry.doc
	}
	docs := make([]ContextPackageDoc, 0, len(raw))
	for _, entry := range raw {
		doc := entry.doc
		if entry.parentID > 0 {
			if parent, ok := byGlobalID[entry.parentID]; ok {
				doc.ParentSlug = parent.Slug
				doc.ParentPath = parent.Path
			}
		}
		docs = append(docs, doc)
	}
	return docs, nil
}

func fetchProjectContextBacklog(projectID int) ([]ContextPackageBacklogItem, error) {
	rows, err := db.Query(
		`SELECT COALESCE(title, ''),
		        COALESCE(description, ''),
		        COALESCE(priority, ''),
		        COALESCE(status, 'open')
		 FROM backlog_item
		 WHERE project_id = ?
		 ORDER BY id`,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []ContextPackageBacklogItem{}
	for rows.Next() {
		var item ContextPackageBacklogItem
		if err := rows.Scan(&item.Title, &item.Description, &item.Priority, &item.Status); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func fetchProjectContextSessions(projectID int) ([]ContextPackageSession, error) {
	rows, err := db.Query(
		fmt.Sprintf(
			`SELECT (
			         SELECT COUNT(*)
			         FROM session ranked
			         WHERE ranked.project_id = s.project_id AND ranked.id <= s.id
			        ) AS local_session_id,
			        s.status,
			        substr(COALESCE(s.task_description, ''), 1, %d) AS task_excerpt
			 FROM session s
			 WHERE s.project_id = ?
			 ORDER BY s.id`,
			contextPackageSessionExcerptLen,
		),
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	sessions := []ContextPackageSession{}
	for rows.Next() {
		var entry ContextPackageSession
		if err := rows.Scan(&entry.SessionId, &entry.Status, &entry.TaskExcerpt); err != nil {
			return nil, err
		}
		sessions = append(sessions, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return sessions, nil
}

func fetchProjectContextOptionalContent(table string, projectID int) (string, error) {
	var content string
	err := db.QueryRow(
		fmt.Sprintf("SELECT content FROM %s WHERE project_id = ?", table),
		projectID,
	).Scan(&content)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return content, nil
}

func resolveContextPackagePath(projectCWD string, rawPath string, projectName string) (string, error) {
	trimmed := strings.TrimSpace(rawPath)
	if trimmed == "" {
		slug := normalizeDocSlugValue(projectName)
		timestamp := time.Now().UTC().Format("20060102T150405Z")
		trimmed = filepath.Join("artifacts", "context_packages", fmt.Sprintf("%s-%s.json", slug, timestamp))
	}
	if !filepath.IsAbs(trimmed) {
		trimmed = filepath.Join(projectCWD, trimmed)
	}
	return filepath.Clean(trimmed), nil
}

func ExportProjectContextPackage(ctx context.Context, req *mcp.CallToolRequest, input ExportProjectContextPackageInput) (*mcp.CallToolResult, ExportProjectContextPackageOutput, error) {
	projectID, err := resolveProjectHandoffProjectID(input.ProjectId)
	if err != nil {
		if err == sql.ErrNoRows {
			return &mcp.CallToolResult{IsError: true}, ExportProjectContextPackageOutput{}, fmt.Errorf("project not found")
		}
		return &mcp.CallToolResult{IsError: true}, ExportProjectContextPackageOutput{}, err
	}

	projectName, err := projectNameFromID(projectID)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, ExportProjectContextPackageOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	projectCWD, err := projectCWDFromID(projectID)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, ExportProjectContextPackageOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	if err := requireProjectDocLocalizationCoverage(projectID); err != nil {
		return &mcp.CallToolResult{IsError: true}, ExportProjectContextPackageOutput{}, err
	}
	documentationLanguage, err := projectDocLanguage(projectID)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, ExportProjectContextPackageOutput{}, err
	}

	overview, err := fetchProjectContextOptionalContent("project_overview", projectID)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, ExportProjectContextPackageOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	handoff, err := fetchProjectContextOptionalContent("project_handoff", projectID)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, ExportProjectContextPackageOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	docs, err := fetchProjectContextDocs(projectID)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, ExportProjectContextPackageOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	backlog, err := fetchProjectContextBacklog(projectID)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, ExportProjectContextPackageOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	sessions, err := fetchProjectContextSessions(projectID)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, ExportProjectContextPackageOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	pkg := ProjectContextPackage{
		Manifest: ContextPackageManifest{
			SchemaVersion:         contextPackageSchemaVersion,
			ExportedAt:            time.Now().UTC().Format(time.RFC3339),
			ProjectName:           projectName,
			ProjectRoot:           projectCWD,
			DocumentationLanguage: documentationLanguage,
		},
		Overview: overview,
		Handoff:  handoff,
		Docs:     docs,
		Backlog:  backlog,
		Sessions: sessions,
	}

	data, err := json.MarshalIndent(pkg, "", "  ")
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, ExportProjectContextPackageOutput{}, fmt.Errorf("failed to encode context package: %v", err)
	}

	outPath, err := resolveContextPackagePath(projectCWD, input.Path, projectName)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, ExportProjectContextPackageOutput{}, err
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return &mcp.CallToolResult{IsError: true}, ExportProjectContextPackageOutput{}, fmt.Errorf("failed to create context package directory: %v", err)
	}
	if err := os.WriteFile(outPath, data, 0o644); err != nil {
		return &mcp.CallToolResult{IsError: true}, ExportProjectContextPackageOutput{}, fmt.Errorf("failed to write context package: %v", err)
	}

	return nil, ExportProjectContextPackageOutput{
		Status:        "success",
		Path:          outPath,
		ProjectId:     projectID,
		DocsCount:     len(docs),
		BacklogCount:  len(backlog),
		SessionsCount: len(sessions),
		HasOverview:   strings.TrimSpace(overview) != "",
		HasHandoff:    strings.TrimSpace(handoff) != "",
	}, nil
}

func validateContextPackage(pkg ProjectContextPackage) error {
	if pkg.Manifest.SchemaVersion != contextPackageSchemaVersion {
		return fmt.Errorf("unsupported context package schema_version %d (expected %d)", pkg.Manifest.SchemaVersion, contextPackageSchemaVersion)
	}

	slugs := make(map[string]struct{}, len(pkg.Docs))
	paths := make(map[string]struct{}, len(pkg.Docs))
	for _, doc := range pkg.Docs {
		if strings.TrimSpace(doc.Title) == "" {
			return fmt.Errorf("context package contains a doc without a title")
		}
		if doc.Slug != "" {
			slugs[doc.Slug] = struct{}{}
		}
		if doc.Path != "" {
			paths[doc.Path] = struct{}{}
		}
	}
	for _, doc := range pkg.Docs {
		if doc.Level < 0 || doc.Level > 3 {
			return fmt.Errorf("context package doc %q has invalid level %d", doc.Title, doc.Level)
		}
		if doc.Level == 0 {
			continue
		}
		parentKey := doc.ParentPath
		if parentKey == "" {
			parentKey = doc.ParentSlug
		}
		if parentKey == "" {
			return fmt.Errorf("context package doc %q has level %d but no parent slug/path", doc.Title, doc.Level)
		}
		_, byPath := paths[parentKey]
		_, bySlug := slugs[parentKey]
		if !byPath && !bySlug {
			return fmt.Errorf("context package doc %q references unknown parent %q", doc.Title, parentKey)
		}
	}
	return nil
}

func ImportProjectContextPackage(ctx context.Context, req *mcp.CallToolRequest, input ImportProjectContextPackageInput) (*mcp.CallToolResult, ImportProjectContextPackageOutput, error) {
	projectID, err := resolveProjectHandoffProjectID(input.ProjectId)
	if err != nil {
		if err == sql.ErrNoRows {
			return &mcp.CallToolResult{IsError: true}, ImportProjectContextPackageOutput{}, fmt.Errorf("project not found")
		}
		return &mcp.CallToolResult{IsError: true}, ImportProjectContextPackageOutput{}, err
	}

	path := strings.TrimSpace(input.Path)
	if path == "" {
		return &mcp.CallToolResult{IsError: true}, ImportProjectContextPackageOutput{}, fmt.Errorf("path is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, ImportProjectContextPackageOutput{}, fmt.Errorf("failed to read context package: %v", err)
	}
	var pkg ProjectContextPackage
	if err := json.Unmarshal(data, &pkg); err != nil {
		return &mcp.CallToolResult{IsError: true}, ImportProjectContextPackageOutput{}, fmt.Errorf("failed to parse context package: %v", err)
	}
	if err := validateContextPackage(pkg); err != nil {
		return &mcp.CallToolResult{IsError: true}, ImportProjectContextPackageOutput{}, err
	}

	var existingDocs int
	if err := db.QueryRow("SELECT COUNT(*) FROM project_doc WHERE project_id = ?", projectID).Scan(&existingDocs); err != nil {
		return &mcp.CallToolResult{IsError: true}, ImportProjectContextPackageOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	if existingDocs > 0 {
		if err := requireProjectDocLocalizationCoverage(projectID); err != nil {
			return &mcp.CallToolResult{IsError: true}, ImportProjectContextPackageOutput{}, err
		}
	}
	var existingHandoff int
	if err := db.QueryRow("SELECT COUNT(*) FROM project_handoff WHERE project_id = ?", projectID).Scan(&existingHandoff); err != nil {
		return &mcp.CallToolResult{IsError: true}, ImportProjectContextPackageOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	if !input.Force && (existingDocs > 0 || existingHandoff > 0) {
		return &mcp.CallToolResult{IsError: true}, ImportProjectContextPackageOutput{}, fmt.Errorf("project already has %d docs and handoff presence %d; pass force=true to import anyway", existingDocs, existingHandoff)
	}

	output := ImportProjectContextPackageOutput{
		Status:            "success",
		Path:              path,
		ProjectId:         projectID,
		SessionsInPackage: len(pkg.Sessions),
	}

	// Overview + handoff upserts.
	if content := strings.TrimSpace(pkg.Overview); content != "" {
		if _, err := db.Exec(
			`INSERT INTO project_overview (project_id, content, updated_at)
			 VALUES (?, ?, CURRENT_TIMESTAMP)
			 ON CONFLICT(project_id) DO UPDATE SET
			   content = excluded.content,
			   updated_at = CURRENT_TIMESTAMP`,
			projectID,
			content,
		); err != nil {
			return &mcp.CallToolResult{IsError: true}, ImportProjectContextPackageOutput{}, fmt.Errorf("DB upsert error: %v", err)
		}
		output.OverviewSet = true
	}
	if content := strings.TrimSpace(pkg.Handoff); content != "" {
		if _, err := db.Exec(
			`INSERT INTO project_handoff (project_id, content, updated_at)
			 VALUES (?, ?, CURRENT_TIMESTAMP)
			 ON CONFLICT(project_id) DO UPDATE SET
			   content = excluded.content,
			   updated_at = CURRENT_TIMESTAMP`,
			projectID,
			content,
		); err != nil {
			return &mcp.CallToolResult{IsError: true}, ImportProjectContextPackageOutput{}, fmt.Errorf("DB upsert error: %v", err)
		}
		output.HandoffSet = true
	}

	// Recreate the doc tree, parents before children, mapping parent linkage
	// via exported slug/path to the new machine-local global doc ids.
	docs := make([]ContextPackageDoc, len(pkg.Docs))
	copy(docs, pkg.Docs)
	sort.SliceStable(docs, func(i, j int) bool { return docs[i].Level < docs[j].Level })

	slugToGlobalID := make(map[string]int, len(docs))
	pathToGlobalID := make(map[string]int, len(docs))
	for _, doc := range docs {
		language, localizedTitle, err := requiredLocalizedProjectDocTitle(projectID, doc.LocalizedTitle)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, ImportProjectContextPackageOutput{}, fmt.Errorf("doc %q: %v", doc.Title, err)
		}
		parentLocalDocID := 0
		if doc.Level > 0 {
			parentKey := doc.ParentPath
			if parentKey == "" {
				parentKey = doc.ParentSlug
			}
			parentGlobalID, ok := pathToGlobalID[parentKey]
			if !ok {
				parentGlobalID, ok = slugToGlobalID[parentKey]
			}
			if !ok {
				return &mcp.CallToolResult{IsError: true}, ImportProjectContextPackageOutput{}, fmt.Errorf("failed to resolve parent %q for doc %q", parentKey, doc.Title)
			}
			parentLocalDocID, err = projectScopedDocIDFromGlobal(parentGlobalID, projectID)
			if err != nil {
				return &mcp.CallToolResult{IsError: true}, ImportProjectContextPackageOutput{}, fmt.Errorf("DB mapping error: %v", err)
			}
		}

		tree, err := normalizeProjectDocTreeFields(projectID, doc.Title, parentLocalDocID, doc.Level, doc.Slug, doc.Path, doc.Status, 0)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, ImportProjectContextPackageOutput{}, fmt.Errorf("failed to normalize doc %q: %v", doc.Title, err)
		}

		docType := strings.TrimSpace(doc.DocType)
		if docType == "" {
			docType = "documentation"
		}
		res, err := db.Exec(
			"INSERT INTO project_doc (project_id, title, content, doc_type, parent_doc_id, level, slug, path, status) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
			projectID,
			doc.Title,
			doc.Content,
			docType,
			tree.ParentDocID,
			tree.Level,
			tree.Slug,
			tree.Path,
			tree.Status,
		)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, ImportProjectContextPackageOutput{}, fmt.Errorf("DB insert error: %v", err)
		}
		newGlobalID, err := res.LastInsertId()
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, ImportProjectContextPackageOutput{}, fmt.Errorf("LastInsertId error: %v", err)
		}
		if err := upsertProjectDocLocalizedTitle(db, int(newGlobalID), language, localizedTitle); err != nil {
			return &mcp.CallToolResult{IsError: true}, ImportProjectContextPackageOutput{}, fmt.Errorf("localized title insert error for doc %q: %v", doc.Title, err)
		}
		if doc.Slug != "" {
			slugToGlobalID[doc.Slug] = int(newGlobalID)
		}
		if doc.Path != "" {
			pathToGlobalID[doc.Path] = int(newGlobalID)
		}
		output.DocsCreated++
	}

	// Backlog items, deduped by title against existing items.
	existingTitles := map[string]struct{}{}
	titleRows, err := db.Query("SELECT COALESCE(title, '') FROM backlog_item WHERE project_id = ?", projectID)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, ImportProjectContextPackageOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	for titleRows.Next() {
		var title string
		if err := titleRows.Scan(&title); err != nil {
			titleRows.Close()
			return &mcp.CallToolResult{IsError: true}, ImportProjectContextPackageOutput{}, fmt.Errorf("DB scan error: %v", err)
		}
		existingTitles[title] = struct{}{}
	}
	titleRows.Close()

	for _, item := range pkg.Backlog {
		title := strings.TrimSpace(item.Title)
		if title == "" {
			output.BacklogSkipped++
			continue
		}
		if _, exists := existingTitles[title]; exists {
			output.BacklogSkipped++
			continue
		}
		if _, err := db.Exec(
			`INSERT INTO backlog_item (project_id, title, description, status, priority, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
			projectID,
			title,
			strings.TrimSpace(item.Description),
			normalizeBacklogStatus(item.Status),
			strings.TrimSpace(item.Priority),
		); err != nil {
			return &mcp.CallToolResult{IsError: true}, ImportProjectContextPackageOutput{}, fmt.Errorf("DB insert error: %v", err)
		}
		existingTitles[title] = struct{}{}
		output.BacklogInserted++
	}

	return nil, output, nil
}
