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

var docSlugInvalidChars = regexp.MustCompile(`[^a-z0-9]+`)

type projectDocQueryer interface {
	QueryRow(query string, args ...any) *sql.Row
}

func normalizeDocSlugValue(raw string) string {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	normalized = docSlugInvalidChars.ReplaceAllString(normalized, "-")
	normalized = strings.Trim(normalized, "-")
	if normalized == "" {
		return "doc"
	}
	return normalized
}

func normalizeDocPathValue(raw string) string {
	trimmed := strings.Trim(strings.TrimSpace(raw), "/")
	if trimmed == "" {
		return ""
	}
	parts := strings.Split(trimmed, "/")
	normalized := make([]string, 0, len(parts))
	for _, part := range parts {
		slug := normalizeDocSlugValue(part)
		if slug != "" {
			normalized = append(normalized, slug)
		}
	}
	return strings.Join(normalized, "/")
}

func projectDocSlugOrPathExists(projectID int, field string, value string, excludeGlobalDocID int) (bool, error) {
	return projectDocSlugOrPathExistsWithExecutor(db, projectID, field, value, excludeGlobalDocID)
}

func projectDocSlugOrPathExistsWithExecutor(exec projectDocQueryer, projectID int, field string, value string, excludeGlobalDocID int) (bool, error) {
	if value == "" {
		return false, nil
	}
	query := fmt.Sprintf("SELECT COUNT(*) FROM project_doc WHERE project_id = ? AND %s = ?", field)
	args := []any{projectID, value}
	if excludeGlobalDocID > 0 {
		query += " AND id != ?"
		args = append(args, excludeGlobalDocID)
	}
	var count int
	if err := exec.QueryRow(query, args...).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func projectDocParentWouldCreateCycle(projectID int, childGlobalDocID int, parentGlobalDocID int) (bool, error) {
	return projectDocParentWouldCreateCycleWithExecutor(db, projectID, childGlobalDocID, parentGlobalDocID)
}

func projectDocParentWouldCreateCycleWithExecutor(exec projectDocQueryer, projectID int, childGlobalDocID int, parentGlobalDocID int) (bool, error) {
	if childGlobalDocID <= 0 || parentGlobalDocID <= 0 {
		return false, nil
	}
	seen := map[int]struct{}{}
	currentID := parentGlobalDocID
	for currentID > 0 {
		if currentID == childGlobalDocID {
			return true, nil
		}
		if _, ok := seen[currentID]; ok {
			return true, nil
		}
		seen[currentID] = struct{}{}

		var nextParent sql.NullInt64
		err := exec.QueryRow(
			"SELECT parent_doc_id FROM project_doc WHERE id = ? AND project_id = ?",
			currentID,
			projectID,
		).Scan(&nextParent)
		if err != nil {
			return false, err
		}
		if !nextParent.Valid {
			return false, nil
		}
		currentID = int(nextParent.Int64)
	}
	return false, nil
}

func uniquifyProjectDocSlugAndPath(projectID int, slug string, path string, excludeGlobalDocID int) (string, string, error) {
	return uniquifyProjectDocSlugAndPathWithExecutor(db, projectID, slug, path, excludeGlobalDocID)
}

func uniquifyProjectDocSlugAndPathWithExecutor(exec projectDocQueryer, projectID int, slug string, path string, excludeGlobalDocID int) (string, string, error) {
	baseSlug := normalizeDocSlugValue(slug)
	basePath := normalizeDocPathValue(path)
	if basePath == "" {
		basePath = baseSlug
	}
	for suffix := 0; suffix < 1000; suffix++ {
		candidateSlug := baseSlug
		candidatePath := basePath
		if suffix > 0 {
			candidateSlug = fmt.Sprintf("%s-%d", baseSlug, suffix+1)
			pathParts := strings.Split(basePath, "/")
			pathParts[len(pathParts)-1] = fmt.Sprintf("%s-%d", pathParts[len(pathParts)-1], suffix+1)
			candidatePath = strings.Join(pathParts, "/")
		}
		slugExists, err := projectDocSlugOrPathExistsWithExecutor(exec, projectID, "slug", candidateSlug, excludeGlobalDocID)
		if err != nil {
			return "", "", err
		}
		pathExists, err := projectDocSlugOrPathExistsWithExecutor(exec, projectID, "path", candidatePath, excludeGlobalDocID)
		if err != nil {
			return "", "", err
		}
		if !slugExists && !pathExists {
			return candidateSlug, candidatePath, nil
		}
	}
	return "", "", fmt.Errorf("could not find unique project_doc slug/path for %q", baseSlug)
}

func globalProjectDocIDFromProjectScopedWithExecutor(exec projectDocQueryer, localDocID int, projectID int) (int, error) {
	if localDocID <= 0 {
		return 0, sql.ErrNoRows
	}

	var globalDocID int
	err := exec.QueryRow(
		`SELECT id
		 FROM project_doc
		 WHERE project_id = ?
		 ORDER BY id
		 LIMIT 1 OFFSET ?`,
		projectID,
		localDocID-1,
	).Scan(&globalDocID)
	if err != nil {
		return 0, err
	}
	return globalDocID, nil
}

type normalizedProjectDocTreeFields struct {
	ParentDocID any
	Level       int
	Slug        string
	Path        string
	Status      string
}

func normalizeProjectDocTreeFields(projectID int, title string, parentLocalDocID int, level int, slug string, path string, status string, excludeGlobalDocID int) (normalizedProjectDocTreeFields, error) {
	return normalizeProjectDocTreeFieldsWithExecutor(db, projectID, title, parentLocalDocID, level, slug, path, status, excludeGlobalDocID)
}

func normalizeProjectDocTreeFieldsWithExecutor(exec projectDocQueryer, projectID int, title string, parentLocalDocID int, level int, slug string, path string, status string, excludeGlobalDocID int) (normalizedProjectDocTreeFields, error) {
	if level < 0 || level > 3 {
		return normalizedProjectDocTreeFields{}, fmt.Errorf("project_doc level must be between 0 and 3")
	}

	status = strings.ToLower(strings.TrimSpace(status))
	if status == "" {
		status = "current"
	}
	switch status {
	case "current", "draft", "stale", "archived":
	default:
		return normalizedProjectDocTreeFields{}, fmt.Errorf("project_doc status must be one of current, draft, stale, archived")
	}

	parentPath := ""
	var parentGlobalDocID any
	if parentLocalDocID > 0 {
		globalParentID, err := globalProjectDocIDFromProjectScopedWithExecutor(exec, parentLocalDocID, projectID)
		if err == sql.ErrNoRows {
			return normalizedProjectDocTreeFields{}, fmt.Errorf("parent_doc_id not found in current project")
		}
		if err != nil {
			return normalizedProjectDocTreeFields{}, fmt.Errorf("failed to resolve parent_doc_id: %v", err)
		}
		if globalParentID == excludeGlobalDocID {
			return normalizedProjectDocTreeFields{}, fmt.Errorf("project_doc cannot be its own parent")
		}
		wouldCycle, err := projectDocParentWouldCreateCycleWithExecutor(exec, projectID, excludeGlobalDocID, globalParentID)
		if err != nil {
			return normalizedProjectDocTreeFields{}, fmt.Errorf("failed to validate project_doc ancestry: %v", err)
		}
		if wouldCycle {
			return normalizedProjectDocTreeFields{}, fmt.Errorf("project_doc cannot use its descendant as parent")
		}

		var parentLevel int
		if err := exec.QueryRow("SELECT level, COALESCE(path, '') FROM project_doc WHERE id = ? AND project_id = ?", globalParentID, projectID).Scan(&parentLevel, &parentPath); err != nil {
			return normalizedProjectDocTreeFields{}, fmt.Errorf("failed to read parent project_doc: %v", err)
		}
		expectedLevel := parentLevel + 1
		if expectedLevel > 3 {
			return normalizedProjectDocTreeFields{}, fmt.Errorf("project_doc tree supports levels 0..3; parent is already level %d", parentLevel)
		}
		if level == 0 {
			level = expectedLevel
		}
		if level != expectedLevel {
			return normalizedProjectDocTreeFields{}, fmt.Errorf("project_doc child level must be parent.level + 1; expected %d", expectedLevel)
		}
		parentGlobalDocID = globalParentID
	} else if level != 0 {
		return normalizedProjectDocTreeFields{}, fmt.Errorf("project_doc level %d requires parent_doc_id", level)
	}

	baseSlug := normalizeDocSlugValue(slug)
	if strings.TrimSpace(slug) == "" {
		baseSlug = normalizeDocSlugValue(title)
	}
	basePath := normalizeDocPathValue(path)
	if basePath == "" {
		if parentPath != "" {
			basePath = parentPath + "/" + baseSlug
		} else {
			basePath = baseSlug
		}
	}

	finalSlug, finalPath, err := uniquifyProjectDocSlugAndPathWithExecutor(exec, projectID, baseSlug, basePath, excludeGlobalDocID)
	if err != nil {
		return normalizedProjectDocTreeFields{}, err
	}

	return normalizedProjectDocTreeFields{
		ParentDocID: parentGlobalDocID,
		Level:       level,
		Slug:        finalSlug,
		Path:        finalPath,
		Status:      status,
	}, nil
}

func normalizeDocIDs(docIds []int) []int {
	seen := make(map[int]struct{}, len(docIds))
	normalized := make([]int, 0, len(docIds))
	for _, id := range docIds {
		if id <= 0 {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		normalized = append(normalized, id)
	}
	sort.Ints(normalized)
	return normalized
}

func summarizeDocContent(content string) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return ""
	}

	normalized := strings.Join(strings.Fields(trimmed), " ")
	const maxLen = 160
	if len(normalized) <= maxLen {
		return normalized
	}
	return normalized[:maxLen-3] + "..."
}

type CheckCurrentProjectDocsInput struct{}

type ProjectDocSummary struct {
	DocId       int    `json:"doc_id"`
	Title       string `json:"title"`
	DocType     string `json:"doc_type"`
	ParentDocId int    `json:"parent_doc_id,omitempty"`
	Level       int    `json:"level"`
	Slug        string `json:"slug"`
	Path        string `json:"path"`
	Status      string `json:"status"`
	Summary     string `json:"summary"`
}

type CheckCurrentProjectDocsOutput struct {
	Docs []ProjectDocSummary `json:"docs"`
}

func CheckCurrentProjectDocs(ctx context.Context, req *mcp.CallToolRequest, input CheckCurrentProjectDocsInput) (*mcp.CallToolResult, CheckCurrentProjectDocsOutput, error) {
	if authorizedRole != "fixer" {
		return &mcp.CallToolResult{IsError: true}, CheckCurrentProjectDocsOutput{}, fmt.Errorf("access denied: requires fixer role")
	}

	rows, err := db.Query(`
		SELECT
			(
				SELECT COUNT(*)
				FROM project_doc d2
				WHERE d2.project_id = d.project_id AND d2.id <= d.id
			) AS local_doc_id,
			d.title,
			d.content,
			COALESCE(d.doc_type, 'documentation'),
			COALESCE((
				SELECT COUNT(*)
				FROM project_doc parent_ranked
				WHERE parent_ranked.project_id = d.project_id AND parent_ranked.id <= d.parent_doc_id
			), 0),
			COALESCE(d.level, 0),
			COALESCE(d.slug, ''),
			COALESCE(d.path, ''),
			COALESCE(d.status, 'current')
		FROM project_doc d
		WHERE d.project_id = ?
		ORDER BY d.id`, authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CheckCurrentProjectDocsOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	defer rows.Close()

	docs := []ProjectDocSummary{}
	for rows.Next() {
		var item ProjectDocSummary
		var content string
		if err := rows.Scan(&item.DocId, &item.Title, &content, &item.DocType, &item.ParentDocId, &item.Level, &item.Slug, &item.Path, &item.Status); err != nil {
			return &mcp.CallToolResult{IsError: true}, CheckCurrentProjectDocsOutput{}, fmt.Errorf("DB scan error: %v", err)
		}
		item.Summary = summarizeDocContent(content)
		docs = append(docs, item)
	}

	return nil, CheckCurrentProjectDocsOutput{Docs: docs}, nil
}

type SetSessionAttachedDocsInput struct {
	SessionId     int   `json:"session_id" jsonschema:"Session ID to attach docs for"`
	ProjectDocIds []int `json:"project_doc_ids" jsonschema:"Array of project_doc IDs to attach"`
}

type SetSessionAttachedDocsOutput struct {
	Status        string `json:"status"`
	SessionId     int    `json:"session_id"`
	ProjectDocIds []int  `json:"project_doc_ids"`
}

func SetSessionAttachedDocs(ctx context.Context, req *mcp.CallToolRequest, input SetSessionAttachedDocsInput) (*mcp.CallToolResult, SetSessionAttachedDocsOutput, error) {
	if authorizedRole != "fixer" {
		return &mcp.CallToolResult{IsError: true}, SetSessionAttachedDocsOutput{}, fmt.Errorf("access denied: requires fixer role")
	}

	globalSessionID, err := globalSessionIDFromProjectScoped(input.SessionId, authorizedProjectId)
	if err == sql.ErrNoRows {
		return &mcp.CallToolResult{IsError: true}, SetSessionAttachedDocsOutput{}, fmt.Errorf("session not found in current project")
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetSessionAttachedDocsOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	belongs, err := sessionBelongsToProject(globalSessionID, authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetSessionAttachedDocsOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	if !belongs {
		return &mcp.CallToolResult{IsError: true}, SetSessionAttachedDocsOutput{}, fmt.Errorf("session not found in current project")
	}

	normalizedLocalDocIds := normalizeDocIDs(input.ProjectDocIds)
	normalizedGlobalDocIds := make([]int, 0, len(normalizedLocalDocIds))
	missing := make([]string, 0)
	for _, localDocID := range normalizedLocalDocIds {
		globalDocID, err := globalProjectDocIDFromProjectScoped(localDocID, authorizedProjectId)
		if err == sql.ErrNoRows {
			missing = append(missing, fmt.Sprintf("%d", localDocID))
			continue
		}
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, SetSessionAttachedDocsOutput{}, fmt.Errorf("DB query error: %v", err)
		}
		docBelongs, err := projectDocBelongsToProject(globalDocID, authorizedProjectId)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, SetSessionAttachedDocsOutput{}, fmt.Errorf("DB query error: %v", err)
		}
		if !docBelongs {
			missing = append(missing, fmt.Sprintf("%d", localDocID))
			continue
		}
		normalizedGlobalDocIds = append(normalizedGlobalDocIds, globalDocID)
	}
	if len(missing) > 0 {
		return &mcp.CallToolResult{IsError: true}, SetSessionAttachedDocsOutput{}, fmt.Errorf("unknown project_doc_id(s): %s", strings.Join(missing, ", "))
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetSessionAttachedDocsOutput{}, fmt.Errorf("DB transaction start error: %v", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if _, err := tx.Exec("DELETE FROM netrunner_attached_doc WHERE session_id = ?", globalSessionID); err != nil {
		return &mcp.CallToolResult{IsError: true}, SetSessionAttachedDocsOutput{}, fmt.Errorf("DB delete error: %v", err)
	}

	for _, docId := range normalizedGlobalDocIds {
		if _, err := tx.Exec("INSERT OR IGNORE INTO netrunner_attached_doc (session_id, project_doc_id) VALUES (?, ?)", globalSessionID, docId); err != nil {
			return &mcp.CallToolResult{IsError: true}, SetSessionAttachedDocsOutput{}, fmt.Errorf("DB insert error: %v", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return &mcp.CallToolResult{IsError: true}, SetSessionAttachedDocsOutput{}, fmt.Errorf("DB commit error: %v", err)
	}

	return nil, SetSessionAttachedDocsOutput{
		Status:        "success",
		SessionId:     input.SessionId,
		ProjectDocIds: normalizedLocalDocIds,
	}, nil
}

type GetSessionAttachedDocsInput struct {
	SessionId int `json:"session_id" jsonschema:"Session ID to read attached document metadata from"`
}

type GetSessionAttachedDocsOutput struct {
	SessionId     int                 `json:"session_id"`
	ProjectDocIds []int               `json:"project_doc_ids"`
	Docs          []ProjectDocSummary `json:"docs"`
}

func GetSessionAttachedDocs(ctx context.Context, req *mcp.CallToolRequest, input GetSessionAttachedDocsInput) (*mcp.CallToolResult, GetSessionAttachedDocsOutput, error) {
	if authorizedRole != "fixer" && authorizedRole != "netrunner" {
		return &mcp.CallToolResult{IsError: true}, GetSessionAttachedDocsOutput{}, fmt.Errorf("access denied: requires authenticated role")
	}

	globalSessionID, err := globalSessionIDFromProjectScoped(input.SessionId, authorizedProjectId)
	if err == sql.ErrNoRows {
		return &mcp.CallToolResult{IsError: true}, GetSessionAttachedDocsOutput{}, fmt.Errorf("session not found in current project")
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetSessionAttachedDocsOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	belongs, err := sessionBelongsToProject(globalSessionID, authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetSessionAttachedDocsOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	if !belongs {
		return &mcp.CallToolResult{IsError: true}, GetSessionAttachedDocsOutput{}, fmt.Errorf("session not found in current project")
	}

	rows, err := db.Query(`
		SELECT
			(
				SELECT COUNT(*)
				FROM project_doc d2
				WHERE d2.project_id = d.project_id AND d2.id <= d.id
			) AS local_doc_id,
			d.title,
			COALESCE(d.doc_type, 'documentation'),
			d.content,
			COALESCE((
				SELECT COUNT(*)
				FROM project_doc parent_ranked
				WHERE parent_ranked.project_id = d.project_id AND parent_ranked.id <= d.parent_doc_id
			), 0),
			COALESCE(d.level, 0),
			COALESCE(d.slug, ''),
			COALESCE(d.path, ''),
			COALESCE(d.status, 'current')
		FROM netrunner_attached_doc ad
		INNER JOIN project_doc d ON d.id = ad.project_doc_id
		WHERE ad.session_id = ? AND d.project_id = ?
		ORDER BY d.id`, globalSessionID, authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetSessionAttachedDocsOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	defer rows.Close()

	docs := []ProjectDocSummary{}
	docIds := []int{}
	for rows.Next() {
		var item ProjectDocSummary
		var content string
		if err := rows.Scan(&item.DocId, &item.Title, &item.DocType, &content, &item.ParentDocId, &item.Level, &item.Slug, &item.Path, &item.Status); err != nil {
			return &mcp.CallToolResult{IsError: true}, GetSessionAttachedDocsOutput{}, fmt.Errorf("DB scan error: %v", err)
		}
		item.Summary = summarizeDocContent(content)
		docs = append(docs, item)
		docIds = append(docIds, item.DocId)
	}

	return nil, GetSessionAttachedDocsOutput{
		SessionId:     input.SessionId,
		ProjectDocIds: docIds,
		Docs:          docs,
	}, nil
}

type GetAttachedProjectDocsInput struct {
	SessionId int `json:"session_id" jsonschema:"Session ID to read attached project docs from. If omitted for netrunner, current checked-out session is used."`
}

type GetAttachedProjectDocsOutput struct {
	SessionId int          `json:"session_id"`
	Docs      []ProjectDoc `json:"docs"`
}

func GetAttachedProjectDocs(ctx context.Context, req *mcp.CallToolRequest, input GetAttachedProjectDocsInput) (*mcp.CallToolResult, GetAttachedProjectDocsOutput, error) {
	if authorizedRole != "fixer" && authorizedRole != "netrunner" {
		return &mcp.CallToolResult{IsError: true}, GetAttachedProjectDocsOutput{}, fmt.Errorf("access denied: requires authenticated role")
	}

	localSessionID := input.SessionId
	if localSessionID == 0 {
		if authorizedRole == "netrunner" && authorizedSessionId != 0 {
			_, mappedSessionID, resolveErr := resolveAuthorizedNetrunnerSessionID("get_attached_project_docs", nil)
			if resolveErr != nil {
				return &mcp.CallToolResult{IsError: true}, GetAttachedProjectDocsOutput{}, resolveErr
			}
			localSessionID = mappedSessionID
		} else {
			return &mcp.CallToolResult{IsError: true}, GetAttachedProjectDocsOutput{}, fmt.Errorf("session_id is required")
		}
	}

	globalSessionID, err := globalSessionIDFromProjectScoped(localSessionID, authorizedProjectId)
	if err == sql.ErrNoRows {
		return &mcp.CallToolResult{IsError: true}, GetAttachedProjectDocsOutput{}, fmt.Errorf("session not found in current project")
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetAttachedProjectDocsOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	belongs, err := sessionBelongsToProject(globalSessionID, authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetAttachedProjectDocsOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	if !belongs {
		return &mcp.CallToolResult{IsError: true}, GetAttachedProjectDocsOutput{}, fmt.Errorf("session not found in current project")
	}

	rows, err := db.Query(`
		SELECT
			(
				SELECT COUNT(*)
				FROM project_doc d2
				WHERE d2.project_id = d.project_id AND d2.id <= d.id
			) AS local_doc_id,
			d.title,
			d.content,
			COALESCE(d.doc_type, 'documentation'),
			COALESCE((
				SELECT COUNT(*)
				FROM project_doc parent_ranked
				WHERE parent_ranked.project_id = d.project_id AND parent_ranked.id <= d.parent_doc_id
			), 0),
			COALESCE(d.level, 0),
			COALESCE(d.slug, ''),
			COALESCE(d.path, ''),
			COALESCE(d.status, 'current')
		FROM netrunner_attached_doc ad
		INNER JOIN project_doc d ON d.id = ad.project_doc_id
		WHERE ad.session_id = ? AND d.project_id = ?
		ORDER BY d.id`, globalSessionID, authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetAttachedProjectDocsOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	defer rows.Close()

	docs := []ProjectDoc{}
	for rows.Next() {
		var item ProjectDoc
		if err := rows.Scan(&item.Id, &item.Title, &item.Content, &item.DocType, &item.ParentDocId, &item.Level, &item.Slug, &item.Path, &item.Status); err != nil {
			return &mcp.CallToolResult{IsError: true}, GetAttachedProjectDocsOutput{}, fmt.Errorf("DB scan error: %v", err)
		}
		docs = append(docs, item)
	}

	return nil, GetAttachedProjectDocsOutput{
		SessionId: localSessionID,
		Docs:      docs,
	}, nil
}

var validNetrunnerLogTypes = map[string]struct{}{
	"started":    {},
	"progress":   {},
	"blocked":    {},
	"workaround": {},
	"completed":  {},
}

func normalizeNetrunnerLogType(logType string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(logType))
	if _, ok := validNetrunnerLogTypes[normalized]; !ok {
		return "", fmt.Errorf("invalid log_type: must be one of started, progress, blocked, workaround, completed")
	}
	return normalized, nil
}

type LogNetrunnerProgressInput struct {
	LogType string `json:"log_type" jsonschema:"Progress log type: started, progress, blocked, workaround, or completed"`
	LogText string `json:"log_text" jsonschema:"Short Netrunner progress note. Timestamp is generated by backend."`
}

type LogNetrunnerProgressOutput struct {
	LogId     int    `json:"log_id"`
	SessionId int    `json:"session_id"`
	LogType   string `json:"log_type"`
	Status    string `json:"status"`
}

func LogNetrunnerProgress(ctx context.Context, req *mcp.CallToolRequest, input LogNetrunnerProgressInput) (*mcp.CallToolResult, LogNetrunnerProgressOutput, error) {
	if authorizedRole != "netrunner" {
		return &mcp.CallToolResult{IsError: true}, LogNetrunnerProgressOutput{}, fmt.Errorf("access denied: requires netrunner role")
	}
	globalSessionID, localSessionID, err := resolveAuthorizedNetrunnerSessionID("log_netrunner_progress", nil)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, LogNetrunnerProgressOutput{}, err
	}
	logType, err := normalizeNetrunnerLogType(input.LogType)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, LogNetrunnerProgressOutput{}, err
	}
	logText := strings.TrimSpace(input.LogText)
	if logText == "" {
		return &mcp.CallToolResult{IsError: true}, LogNetrunnerProgressOutput{}, fmt.Errorf("log_text is required")
	}

	res, err := db.Exec(
		"INSERT INTO netrunner_session_log (project_id, session_id, log_type, log_text) VALUES (?, ?, ?, ?)",
		authorizedProjectId,
		globalSessionID,
		logType,
		logText,
	)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, LogNetrunnerProgressOutput{}, fmt.Errorf("DB insert error: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, LogNetrunnerProgressOutput{}, fmt.Errorf("LastInsertId error: %v", err)
	}

	return nil, LogNetrunnerProgressOutput{
		LogId:     int(id),
		SessionId: localSessionID,
		LogType:   logType,
		Status:    "success",
	}, nil
}

type ViewNetrunnerLogsInput struct {
	SessionId   int `json:"session_id,omitempty" jsonschema:"Project-scoped Netrunner session ID to inspect"`
	NetrunnerId int `json:"netrunner_id,omitempty" jsonschema:"Alias for session_id"`
}

type NetrunnerSessionLog struct {
	Id        int    `json:"id"`
	SessionId int    `json:"session_id"`
	LogType   string `json:"log_type"`
	LogText   string `json:"log_text"`
	CreatedAt string `json:"created_at"`
}

type ViewNetrunnerLogsOutput struct {
	SessionId int                   `json:"session_id"`
	Logs      []NetrunnerSessionLog `json:"logs"`
}

func ViewNetrunnerLogs(ctx context.Context, req *mcp.CallToolRequest, input ViewNetrunnerLogsInput) (*mcp.CallToolResult, ViewNetrunnerLogsOutput, error) {
	if authorizedRole != "fixer" {
		return &mcp.CallToolResult{IsError: true}, ViewNetrunnerLogsOutput{}, fmt.Errorf("access denied: requires fixer role")
	}
	localSessionID := input.SessionId
	if localSessionID == 0 {
		localSessionID = input.NetrunnerId
	}
	if localSessionID <= 0 {
		return &mcp.CallToolResult{IsError: true}, ViewNetrunnerLogsOutput{}, fmt.Errorf("session_id is required")
	}
	globalSessionID, err := globalSessionIDFromProjectScoped(localSessionID, authorizedProjectId)
	if err == sql.ErrNoRows {
		return &mcp.CallToolResult{IsError: true}, ViewNetrunnerLogsOutput{}, fmt.Errorf("session not found in current project")
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, ViewNetrunnerLogsOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	rows, err := db.Query(`
		SELECT
			(
				SELECT COUNT(*)
				FROM netrunner_session_log ranked
				WHERE ranked.project_id = l.project_id AND ranked.id <= l.id
			) AS local_log_id,
			(
				SELECT COUNT(*)
				FROM session ranked_session
				WHERE ranked_session.project_id = l.project_id AND ranked_session.id <= l.session_id
			) AS local_session_id,
			l.log_type,
			l.log_text,
			l.created_at
		FROM netrunner_session_log l
		WHERE l.project_id = ? AND l.session_id = ?
		ORDER BY l.created_at, l.id`,
		authorizedProjectId,
		globalSessionID,
	)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, ViewNetrunnerLogsOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	defer rows.Close()

	logs := []NetrunnerSessionLog{}
	for rows.Next() {
		var item NetrunnerSessionLog
		if err := rows.Scan(&item.Id, &item.SessionId, &item.LogType, &item.LogText, &item.CreatedAt); err != nil {
			return &mcp.CallToolResult{IsError: true}, ViewNetrunnerLogsOutput{}, fmt.Errorf("DB scan error: %v", err)
		}
		logs = append(logs, item)
	}
	if err := rows.Err(); err != nil {
		return &mcp.CallToolResult{IsError: true}, ViewNetrunnerLogsOutput{}, fmt.Errorf("DB rows error: %v", err)
	}

	return nil, ViewNetrunnerLogsOutput{SessionId: localSessionID, Logs: logs}, nil
}

type ProposeDocUpdateInput struct {
	ProposedContent    string `json:"proposed_content" jsonschema:"The canonical documentation content to propose. Use netrunner logs for history, not doc_proposal."`
	ProposedDocType    string `json:"proposed_doc_type,omitempty" jsonschema:"The canonical document type to propose"`
	TargetProjectDocId int    `json:"target_project_doc_id,omitempty" jsonschema:"Optional project-scoped target doc ID when the proposal should update one existing document"`
}

type ProposeDocUpdateOutput struct {
	ProposalId int    `json:"proposal_id"`
	Status     string `json:"status"`
}

func ProposeDocUpdate(ctx context.Context, req *mcp.CallToolRequest, input ProposeDocUpdateInput) (*mcp.CallToolResult, ProposeDocUpdateOutput, error) {
	if authorizedRole != "netrunner" {
		return &mcp.CallToolResult{IsError: true}, ProposeDocUpdateOutput{}, fmt.Errorf("access denied: requires netrunner role")
	}

	globalSessionID, _, err := resolveAuthorizedNetrunnerSessionID(
		"propose_doc_update",
		nil,
	)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, ProposeDocUpdateOutput{}, err
	}

	docType := input.ProposedDocType
	if docType == "" {
		docType = "documentation"
	}

	var targetProjectDocID any
	if input.TargetProjectDocId != 0 {
		globalDocID, err := globalProjectDocIDFromProjectScoped(input.TargetProjectDocId, authorizedProjectId)
		if err == sql.ErrNoRows {
			return &mcp.CallToolResult{IsError: true}, ProposeDocUpdateOutput{}, fmt.Errorf("target_project_doc_id not found in current project")
		}
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, ProposeDocUpdateOutput{}, fmt.Errorf("failed to resolve target_project_doc_id: %v", err)
		}
		targetProjectDocID = globalDocID
	}

	res, err := db.Exec(
		"INSERT INTO doc_proposal (project_id, session_id, status, proposed_content, proposed_doc_type, target_project_doc_id) VALUES (?, ?, 'pending', ?, ?, ?)",
		authorizedProjectId,
		globalSessionID,
		input.ProposedContent,
		docType,
		targetProjectDocID,
	)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, ProposeDocUpdateOutput{}, fmt.Errorf("DB insert error: %v", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, ProposeDocUpdateOutput{}, fmt.Errorf("LastInsertId error: %v", err)
	}

	localProposalID, err := projectScopedDocProposalIDFromGlobal(int(id), authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, ProposeDocUpdateOutput{}, fmt.Errorf("DB mapping error: %v", err)
	}

	return nil, ProposeDocUpdateOutput{ProposalId: localProposalID, Status: "success"}, nil
}

type ReviewDocProposalsInput struct{}

type DocProposal struct {
	Id                 int    `json:"id"`
	SessionId          int    `json:"session_id"`
	ProposedContent    string `json:"proposed_content"`
	ProposedDocType    string `json:"proposed_doc_type"`
	TargetProjectDocId int    `json:"target_project_doc_id,omitempty"`
}

type ReviewDocProposalsOutput struct {
	Proposals []DocProposal `json:"proposals"`
}

func ReviewDocProposals(ctx context.Context, req *mcp.CallToolRequest, input ReviewDocProposalsInput) (*mcp.CallToolResult, ReviewDocProposalsOutput, error) {
	if authorizedRole != "fixer" {
		return &mcp.CallToolResult{IsError: true}, ReviewDocProposalsOutput{}, fmt.Errorf("access denied: requires fixer role")
	}

	rows, err := db.Query(`
		SELECT
			(
				SELECT COUNT(*)
				FROM doc_proposal p2
				WHERE p2.project_id = p.project_id AND p2.id <= p.id
			) AS local_proposal_id,
			(
				SELECT COUNT(*)
				FROM session s2
				WHERE s2.project_id = p.project_id AND s2.id <= p.session_id
			) AS local_session_id,
			p.proposed_content,
			COALESCE(p.proposed_doc_type, 'documentation'),
			(
				SELECT (
					SELECT COUNT(*)
					FROM project_doc d2
					WHERE d2.project_id = p.project_id AND d2.id <= target.id
				)
				FROM project_doc target
				WHERE target.id = p.target_project_doc_id AND target.project_id = p.project_id
			)
		FROM doc_proposal p
		WHERE p.project_id = ? AND p.status = 'pending'
		ORDER BY p.id`, authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, ReviewDocProposalsOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	defer rows.Close()

	var proposals []DocProposal
	for rows.Next() {
		var p DocProposal
		var targetProjectDocID sql.NullInt64
		if err := rows.Scan(&p.Id, &p.SessionId, &p.ProposedContent, &p.ProposedDocType, &targetProjectDocID); err != nil {
			return &mcp.CallToolResult{IsError: true}, ReviewDocProposalsOutput{}, fmt.Errorf("DB scan error: %v", err)
		}
		if targetProjectDocID.Valid {
			p.TargetProjectDocId = int(targetProjectDocID.Int64)
		}
		proposals = append(proposals, p)
	}
	if proposals == nil {
		proposals = []DocProposal{}
	}

	return nil, ReviewDocProposalsOutput{Proposals: proposals}, nil
}

type SetDocProposalStatusInput struct {
	ProposalId  int    `json:"proposal_id" jsonschema:"The ID of the proposal to update"`
	Status      string `json:"status" jsonschema:"New status: 'approved' or 'rejected'"`
	ParentDocId int    `json:"parent_doc_id,omitempty" jsonschema:"Optional project-scoped parent document ID when approving a proposal that creates a new canonical doc"`
	Slug        string `json:"slug,omitempty" jsonschema:"Optional stable slug when approving a proposal that creates a new canonical doc"`
	Level       int    `json:"level,omitempty" jsonschema:"Optional tree level 0..3 when approving a proposal that creates a new canonical doc"`
}

type SetDocProposalStatusOutput struct {
	Status string `json:"status"`
}

func SetDocProposalStatus(ctx context.Context, req *mcp.CallToolRequest, input SetDocProposalStatusInput) (*mcp.CallToolResult, SetDocProposalStatusOutput, error) {
	if authorizedRole != "fixer" {
		return &mcp.CallToolResult{IsError: true}, SetDocProposalStatusOutput{}, fmt.Errorf("access denied: requires fixer role")
	}
	if input.Status != "approved" && input.Status != "rejected" {
		return &mcp.CallToolResult{IsError: true}, SetDocProposalStatusOutput{}, fmt.Errorf("invalid status: must be 'approved' or 'rejected'")
	}

	globalProposalID, err := globalDocProposalIDFromProjectScoped(input.ProposalId, authorizedProjectId)
	if err == sql.ErrNoRows {
		return &mcp.CallToolResult{IsError: true}, SetDocProposalStatusOutput{}, fmt.Errorf("failed to fetch proposal: %v", sql.ErrNoRows)
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetDocProposalStatusOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	if input.Status == "approved" {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, SetDocProposalStatusOutput{}, fmt.Errorf("failed to begin approval transaction: %v", err)
		}
		defer func() {
			_ = tx.Rollback()
		}()

		var proposedContent, proposedDocType string
		var targetProjectDocID sql.NullInt64
		err = tx.QueryRow(
			"SELECT proposed_content, COALESCE(proposed_doc_type, 'documentation'), target_project_doc_id FROM doc_proposal WHERE id = ? AND project_id = ?",
			globalProposalID,
			authorizedProjectId,
		).Scan(&proposedContent, &proposedDocType, &targetProjectDocID)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, SetDocProposalStatusOutput{}, fmt.Errorf("failed to fetch proposal: %v", err)
		}

		if targetProjectDocID.Valid {
			res, err := tx.Exec(
				"UPDATE project_doc SET content = ?, doc_type = ? WHERE id = ? AND project_id = ?",
				proposedContent,
				proposedDocType,
				targetProjectDocID.Int64,
				authorizedProjectId,
			)
			if err != nil {
				return &mcp.CallToolResult{IsError: true}, SetDocProposalStatusOutput{}, fmt.Errorf("failed to update targeted project_doc: %v", err)
			}
			rowsAffected, err := res.RowsAffected()
			if err != nil {
				return &mcp.CallToolResult{IsError: true}, SetDocProposalStatusOutput{}, fmt.Errorf("failed to confirm targeted project_doc update: %v", err)
			}
			if rowsAffected == 0 {
				return &mcp.CallToolResult{IsError: true}, SetDocProposalStatusOutput{}, fmt.Errorf("target_project_doc_id no longer exists in current project")
			}
		} else {
			rows, err := tx.Query(
				"SELECT id FROM project_doc WHERE project_id = ? AND COALESCE(doc_type, 'documentation') = ? ORDER BY id LIMIT 2",
				authorizedProjectId,
				proposedDocType,
			)
			if err != nil {
				return &mcp.CallToolResult{IsError: true}, SetDocProposalStatusOutput{}, fmt.Errorf("failed to resolve proposal target: %v", err)
			}
			defer func() {
				_ = rows.Close()
			}()

			matchingDocIDs := make([]int, 0, 2)
			for rows.Next() {
				var docID int
				if err := rows.Scan(&docID); err != nil {
					return &mcp.CallToolResult{IsError: true}, SetDocProposalStatusOutput{}, fmt.Errorf("failed to scan proposal target: %v", err)
				}
				matchingDocIDs = append(matchingDocIDs, docID)
			}
			if err := rows.Err(); err != nil {
				return &mcp.CallToolResult{IsError: true}, SetDocProposalStatusOutput{}, fmt.Errorf("failed to read proposal targets: %v", err)
			}

			switch len(matchingDocIDs) {
			case 0:
				tree, err := normalizeProjectDocTreeFieldsWithExecutor(tx, authorizedProjectId, "Documentation ("+proposedDocType+")", input.ParentDocId, input.Level, input.Slug, "", "current", 0)
				if err != nil {
					return &mcp.CallToolResult{IsError: true}, SetDocProposalStatusOutput{}, err
				}
				_, err = tx.Exec(
					"INSERT INTO project_doc (project_id, title, content, doc_type, parent_doc_id, level, slug, path, status) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
					authorizedProjectId,
					"Documentation ("+proposedDocType+")",
					proposedContent,
					proposedDocType,
					tree.ParentDocID,
					tree.Level,
					tree.Slug,
					tree.Path,
					tree.Status,
				)
				if err != nil {
					return &mcp.CallToolResult{IsError: true}, SetDocProposalStatusOutput{}, fmt.Errorf("failed to insert project_doc: %v", err)
				}
			case 1:
				if _, err := tx.Exec(
					"UPDATE project_doc SET content = ?, doc_type = ? WHERE id = ? AND project_id = ?",
					proposedContent,
					proposedDocType,
					matchingDocIDs[0],
					authorizedProjectId,
				); err != nil {
					return &mcp.CallToolResult{IsError: true}, SetDocProposalStatusOutput{}, fmt.Errorf("failed to update project_doc: %v", err)
				}
			default:
				return &mcp.CallToolResult{IsError: true}, SetDocProposalStatusOutput{}, fmt.Errorf(
					"proposal %d approval is ambiguous for doc_type %q; resubmit with target_project_doc_id",
					input.ProposalId,
					proposedDocType,
				)
			}
		}

		_, err = tx.Exec("UPDATE doc_proposal SET status = ? WHERE id = ? AND project_id = ?", input.Status, globalProposalID, authorizedProjectId)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, SetDocProposalStatusOutput{}, fmt.Errorf("DB update error: %v", err)
		}

		if err := tx.Commit(); err != nil {
			return &mcp.CallToolResult{IsError: true}, SetDocProposalStatusOutput{}, fmt.Errorf("failed to commit approval transaction: %v", err)
		}

		return nil, SetDocProposalStatusOutput{Status: "success"}, nil
	}

	_, err = db.Exec("UPDATE doc_proposal SET status = ? WHERE id = ? AND project_id = ?", input.Status, globalProposalID, authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetDocProposalStatusOutput{}, fmt.Errorf("DB update error: %v", err)
	}

	return nil, SetDocProposalStatusOutput{Status: "success"}, nil
}

type GetProjectDocsInput struct{}

type ProjectDoc struct {
	Id          int    `json:"id"`
	Title       string `json:"title"`
	Content     string `json:"content"`
	DocType     string `json:"doc_type"`
	ParentDocId int    `json:"parent_doc_id,omitempty"`
	Level       int    `json:"level"`
	Slug        string `json:"slug"`
	Path        string `json:"path"`
	Status      string `json:"status"`
}

type GetProjectDocsOutput struct {
	Docs []ProjectDoc `json:"docs"`
}

func GetProjectDocs(ctx context.Context, req *mcp.CallToolRequest, input GetProjectDocsInput) (*mcp.CallToolResult, GetProjectDocsOutput, error) {
	if authorizedRole != "fixer" && authorizedRole != "netrunner" {
		return &mcp.CallToolResult{IsError: true}, GetProjectDocsOutput{}, fmt.Errorf("access denied: requires authenticated role")
	}

	rows, err := db.Query(`
		SELECT
			(
				SELECT COUNT(*)
				FROM project_doc d2
				WHERE d2.project_id = d.project_id AND d2.id <= d.id
			) AS local_doc_id,
			d.title,
			d.content,
			COALESCE(d.doc_type, 'documentation'),
			COALESCE((
				SELECT COUNT(*)
				FROM project_doc parent_ranked
				WHERE parent_ranked.project_id = d.project_id AND parent_ranked.id <= d.parent_doc_id
			), 0),
			COALESCE(d.level, 0),
			COALESCE(d.slug, ''),
			COALESCE(d.path, ''),
			COALESCE(d.status, 'current')
		FROM project_doc d
		WHERE d.project_id = ?
		ORDER BY d.id`, authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetProjectDocsOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	defer rows.Close()

	var docs []ProjectDoc
	for rows.Next() {
		var d ProjectDoc
		if err := rows.Scan(&d.Id, &d.Title, &d.Content, &d.DocType, &d.ParentDocId, &d.Level, &d.Slug, &d.Path, &d.Status); err != nil {
			return &mcp.CallToolResult{IsError: true}, GetProjectDocsOutput{}, fmt.Errorf("DB scan error: %v", err)
		}
		docs = append(docs, d)
	}
	if docs == nil {
		docs = []ProjectDoc{}
	}

	return nil, GetProjectDocsOutput{Docs: docs}, nil
}

type AddProjectDocInput struct {
	Title       string `json:"title" jsonschema:"The title of the new canonical document"`
	Content     string `json:"content" jsonschema:"The content of the new canonical document"`
	DocType     string `json:"doc_type,omitempty" jsonschema:"The canonical document type, e.g. 'documentation', 'architecture', etc."`
	ParentDocId int    `json:"parent_doc_id,omitempty" jsonschema:"Optional project-scoped parent document ID for the canonical doc tree"`
	Level       int    `json:"level,omitempty" jsonschema:"Optional tree level 0..3. Level 0 has no parent; child level must be parent.level + 1."`
	Slug        string `json:"slug,omitempty" jsonschema:"Optional stable slug unique within the project"`
	Path        string `json:"path,omitempty" jsonschema:"Optional stable materialized path unique within the project"`
	Status      string `json:"status,omitempty" jsonschema:"Optional currentness status: current, draft, stale, or archived"`
}

type AddProjectDocOutput struct {
	Id     int    `json:"id"`
	Status string `json:"status"`
}

func AddProjectDoc(ctx context.Context, req *mcp.CallToolRequest, input AddProjectDocInput) (*mcp.CallToolResult, AddProjectDocOutput, error) {
	if authorizedRole != "fixer" {
		return &mcp.CallToolResult{IsError: true}, AddProjectDocOutput{}, fmt.Errorf("access denied: requires fixer role")
	}

	docType := input.DocType
	if docType == "" {
		docType = "documentation"
	}

	tree, err := normalizeProjectDocTreeFields(authorizedProjectId, input.Title, input.ParentDocId, input.Level, input.Slug, input.Path, input.Status, 0)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, AddProjectDocOutput{}, err
	}

	res, err := db.Exec(
		"INSERT INTO project_doc (project_id, title, content, doc_type, parent_doc_id, level, slug, path, status) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		authorizedProjectId,
		input.Title,
		input.Content,
		docType,
		tree.ParentDocID,
		tree.Level,
		tree.Slug,
		tree.Path,
		tree.Status,
	)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, AddProjectDocOutput{}, fmt.Errorf("DB insert error: %v", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, AddProjectDocOutput{}, fmt.Errorf("LastInsertId error: %v", err)
	}

	localDocID, err := projectScopedDocIDFromGlobal(int(id), authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, AddProjectDocOutput{}, fmt.Errorf("DB mapping error: %v", err)
	}

	return nil, AddProjectDocOutput{Id: localDocID, Status: "success"}, nil
}

type UpdateProjectDocInput struct {
	DocId       int    `json:"doc_id" jsonschema:"The ID of the canonical document to update"`
	Content     string `json:"content" jsonschema:"The new content of the document"`
	DocType     string `json:"doc_type,omitempty" jsonschema:"Optionally update the doc type"`
	ParentDocId int    `json:"parent_doc_id,omitempty" jsonschema:"Optionally update the project-scoped parent document ID"`
	Level       int    `json:"level,omitempty" jsonschema:"Optionally update tree level 0..3. Child level must be parent.level + 1."`
	Slug        string `json:"slug,omitempty" jsonschema:"Optionally update the stable slug"`
	Path        string `json:"path,omitempty" jsonschema:"Optionally update the stable materialized path"`
	Status      string `json:"status,omitempty" jsonschema:"Optionally update currentness status: current, draft, stale, or archived"`
}

type UpdateProjectDocOutput struct {
	Status string `json:"status"`
}

func UpdateProjectDoc(ctx context.Context, req *mcp.CallToolRequest, input UpdateProjectDocInput) (*mcp.CallToolResult, UpdateProjectDocOutput, error) {
	if authorizedRole != "fixer" {
		return &mcp.CallToolResult{IsError: true}, UpdateProjectDocOutput{}, fmt.Errorf("access denied: requires fixer role")
	}

	globalDocID, err := globalProjectDocIDFromProjectScoped(input.DocId, authorizedProjectId)
	if err == sql.ErrNoRows {
		return &mcp.CallToolResult{IsError: true}, UpdateProjectDocOutput{}, fmt.Errorf("document not found or not belonging to project")
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, UpdateProjectDocOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	treeTouched := input.ParentDocId != 0 || input.Level != 0 || strings.TrimSpace(input.Slug) != "" || strings.TrimSpace(input.Path) != "" || strings.TrimSpace(input.Status) != ""
	var treeUpdate normalizedProjectDocTreeFields
	if treeTouched {
		var currentTitle string
		var currentParentGlobal sql.NullInt64
		var currentLevel int
		var currentSlug, currentPath, currentStatus string
		if err := db.QueryRow(
			"SELECT title, parent_doc_id, level, COALESCE(slug, ''), COALESCE(path, ''), COALESCE(status, 'current') FROM project_doc WHERE id = ? AND project_id = ?",
			globalDocID,
			authorizedProjectId,
		).Scan(&currentTitle, &currentParentGlobal, &currentLevel, &currentSlug, &currentPath, &currentStatus); err != nil {
			return &mcp.CallToolResult{IsError: true}, UpdateProjectDocOutput{}, fmt.Errorf("DB query error: %v", err)
		}

		parentLocalDocID := input.ParentDocId
		if parentLocalDocID == 0 && currentParentGlobal.Valid {
			parentLocalDocID, err = projectScopedDocIDFromGlobal(int(currentParentGlobal.Int64), authorizedProjectId)
			if err != nil {
				return &mcp.CallToolResult{IsError: true}, UpdateProjectDocOutput{}, fmt.Errorf("DB mapping error: %v", err)
			}
		}
		level := input.Level
		if level == 0 {
			level = currentLevel
		}
		slug := input.Slug
		if strings.TrimSpace(slug) == "" {
			slug = currentSlug
		}
		path := input.Path
		if strings.TrimSpace(path) == "" {
			path = currentPath
		}
		status := input.Status
		if strings.TrimSpace(status) == "" {
			status = currentStatus
		}

		treeUpdate, err = normalizeProjectDocTreeFields(authorizedProjectId, currentTitle, parentLocalDocID, level, slug, path, status, globalDocID)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, UpdateProjectDocOutput{}, err
		}
	}

	var res sql.Result
	if input.DocType != "" {
		res, err = db.Exec("UPDATE project_doc SET content = ?, doc_type = ? WHERE id = ? AND project_id = ?", input.Content, input.DocType, globalDocID, authorizedProjectId)
	} else {
		res, err = db.Exec("UPDATE project_doc SET content = ? WHERE id = ? AND project_id = ?", input.Content, globalDocID, authorizedProjectId)
	}

	if err != nil {
		return &mcp.CallToolResult{IsError: true}, UpdateProjectDocOutput{}, fmt.Errorf("DB update error: %v", err)
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, UpdateProjectDocOutput{}, fmt.Errorf("RowsAffected error: %v", err)
	}

	if rowsAffected == 0 {
		return &mcp.CallToolResult{IsError: true}, UpdateProjectDocOutput{}, fmt.Errorf("document not found or not belonging to project")
	}

	if treeTouched {
		if _, err := db.Exec(
			"UPDATE project_doc SET parent_doc_id = ?, level = ?, slug = ?, path = ?, status = ? WHERE id = ? AND project_id = ?",
			treeUpdate.ParentDocID,
			treeUpdate.Level,
			treeUpdate.Slug,
			treeUpdate.Path,
			treeUpdate.Status,
			globalDocID,
			authorizedProjectId,
		); err != nil {
			return &mcp.CallToolResult{IsError: true}, UpdateProjectDocOutput{}, fmt.Errorf("DB update error: %v", err)
		}
	}

	return nil, UpdateProjectDocOutput{Status: "success"}, nil
}

type DeleteProjectDocInput struct {
	DocId int `json:"doc_id" jsonschema:"The ID of the document to delete"`
}

type DeleteProjectDocOutput struct {
	Status string `json:"status"`
}

func DeleteProjectDoc(ctx context.Context, req *mcp.CallToolRequest, input DeleteProjectDocInput) (*mcp.CallToolResult, DeleteProjectDocOutput, error) {
	if authorizedRole != "fixer" {
		return &mcp.CallToolResult{IsError: true}, DeleteProjectDocOutput{}, fmt.Errorf("access denied: requires fixer role")
	}

	globalDocID, err := globalProjectDocIDFromProjectScoped(input.DocId, authorizedProjectId)
	if err == sql.ErrNoRows {
		return &mcp.CallToolResult{IsError: true}, DeleteProjectDocOutput{}, fmt.Errorf("document not found or not belonging to project")
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, DeleteProjectDocOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	res, err := db.Exec("DELETE FROM project_doc WHERE id = ? AND project_id = ?", globalDocID, authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, DeleteProjectDocOutput{}, fmt.Errorf("DB delete error: %v", err)
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, DeleteProjectDocOutput{}, fmt.Errorf("RowsAffected error: %v", err)
	}

	if rowsAffected == 0 {
		return &mcp.CallToolResult{IsError: true}, DeleteProjectDocOutput{}, fmt.Errorf("document not found or not belonging to project")
	}

	return nil, DeleteProjectDocOutput{Status: "success"}, nil
}
