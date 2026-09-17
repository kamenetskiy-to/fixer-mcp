package main

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func resolveHandsMcpIDs(names []string) ([]int, []string, error) {
	normalized := normalizeMcpServerNames(names)
	filtered := make([]string, 0, len(normalized))
	for _, name := range normalized {
		if name == forcedMcpServerName {
			continue
		}
		filtered = append(filtered, name)
	}
	ids := make([]int, 0, len(filtered))
	missing := []string{}
	for _, name := range filtered {
		var serverID int
		err := db.QueryRow("SELECT id FROM mcp_server WHERE name = ?", name).Scan(&serverID)
		if err == sql.ErrNoRows {
			missing = append(missing, name)
			continue
		}
		if err != nil {
			return nil, nil, fmt.Errorf("DB query error: %v", err)
		}
		ids = append(ids, serverID)
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return nil, nil, fmt.Errorf("unknown MCP server(s): %s", strings.Join(missing, ", "))
	}
	return ids, filtered, nil
}

func resolveHandsDocIDs(localDocIDs []int, projectID int) ([]int, []int, error) {
	normalizedLocal := normalizeDocIDs(localDocIDs)
	globalIDs := make([]int, 0, len(normalizedLocal))
	missing := []string{}
	for _, localDocID := range normalizedLocal {
		globalDocID, err := globalProjectDocIDFromProjectScoped(localDocID, projectID)
		if err == sql.ErrNoRows {
			missing = append(missing, fmt.Sprintf("%d", localDocID))
			continue
		}
		if err != nil {
			return nil, nil, fmt.Errorf("DB query error: %v", err)
		}
		belongs, err := projectDocBelongsToProject(globalDocID, projectID)
		if err != nil {
			return nil, nil, fmt.Errorf("DB query error: %v", err)
		}
		if !belongs {
			missing = append(missing, fmt.Sprintf("%d", localDocID))
			continue
		}
		globalIDs = append(globalIDs, globalDocID)
	}
	if len(missing) > 0 {
		return nil, nil, fmt.Errorf("unknown project_doc_id(s): %s", strings.Join(missing, ", "))
	}
	return globalIDs, normalizedLocal, nil
}

type SetProjectHandsMcpServersInput struct {
	McpServerNames []string `json:"mcp_server_names" jsonschema:"Array of MCP server names proposed for the permanent project actor Руки"`
}

type SetProjectHandsMcpServersOutput struct {
	Status         string   `json:"status"`
	ProjectId      int      `json:"project_id"`
	McpServerNames []string `json:"mcp_server_names"`
}

func SetProjectHandsMcpServers(ctx context.Context, req *mcp.CallToolRequest, input SetProjectHandsMcpServersInput) (*mcp.CallToolResult, SetProjectHandsMcpServersOutput, error) {
	if authorizedRole != "fixer" {
		return &mcp.CallToolResult{IsError: true}, SetProjectHandsMcpServersOutput{}, fmt.Errorf("access denied: requires fixer role")
	}
	serverIDs, filteredNames, err := resolveHandsMcpIDs(input.McpServerNames)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetProjectHandsMcpServersOutput{}, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetProjectHandsMcpServersOutput{}, fmt.Errorf("DB transaction start error: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec("DELETE FROM project_hands_mcp_server WHERE project_id = ?", authorizedProjectId); err != nil {
		return &mcp.CallToolResult{IsError: true}, SetProjectHandsMcpServersOutput{}, fmt.Errorf("DB delete error: %v", err)
	}
	for _, serverID := range serverIDs {
		if _, err := tx.Exec("INSERT OR IGNORE INTO project_hands_mcp_server (project_id, mcp_server_id) VALUES (?, ?)", authorizedProjectId, serverID); err != nil {
			return &mcp.CallToolResult{IsError: true}, SetProjectHandsMcpServersOutput{}, fmt.Errorf("DB insert error: %v", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return &mcp.CallToolResult{IsError: true}, SetProjectHandsMcpServersOutput{}, fmt.Errorf("DB commit error: %v", err)
	}
	return nil, SetProjectHandsMcpServersOutput{Status: "success", ProjectId: authorizedProjectId, McpServerNames: filteredNames}, nil
}

type GetProjectHandsMcpServersInput struct{}

type GetProjectHandsMcpServersOutput struct {
	ProjectId      int               `json:"project_id"`
	McpServerNames []string          `json:"mcp_server_names"`
	Servers        []McpServerRecord `json:"servers"`
}

func GetProjectHandsMcpServers(ctx context.Context, req *mcp.CallToolRequest, input GetProjectHandsMcpServersInput) (*mcp.CallToolResult, GetProjectHandsMcpServersOutput, error) {
	if authorizedRole != "fixer" && authorizedRole != "netrunner" {
		return &mcp.CallToolResult{IsError: true}, GetProjectHandsMcpServersOutput{}, fmt.Errorf("access denied: requires authenticated role")
	}
	rows, err := db.Query(
		`SELECT s.id, s.name, COALESCE(s.short_description, ''), COALESCE(s.long_description, ''), COALESCE(s.auto_attach, 0), COALESCE(s.is_default, 0), COALESCE(s.category, ''), COALESCE(s.how_to, ''), COALESCE(s.auth_env_keys, ''), COALESCE(s.portability, ''), COALESCE(s.install_hint, ''), COALESCE(s.archived, 0)
		 FROM project_hands_mcp_server phm
		 INNER JOIN mcp_server s ON s.id = phm.mcp_server_id
		 WHERE phm.project_id = ?
		 ORDER BY COALESCE(s.category, ''), s.name`,
		authorizedProjectId,
	)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetProjectHandsMcpServersOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	defer rows.Close()
	servers := []McpServerRecord{}
	names := []string{}
	for rows.Next() {
		var item McpServerRecord
		var autoAttach, isDefault, archived int
		if err := rows.Scan(&item.Id, &item.Name, &item.ShortDescription, &item.LongDescription, &autoAttach, &isDefault, &item.Category, &item.HowTo, &item.AuthEnvKeys, &item.Portability, &item.InstallHint, &archived); err != nil {
			return &mcp.CallToolResult{IsError: true}, GetProjectHandsMcpServersOutput{}, fmt.Errorf("DB scan error: %v", err)
		}
		item.AutoAttach = autoAttach == 1
		item.IsDefault = isDefault == 1
		item.Archived = archived == 1
		servers = append(servers, item)
		names = append(names, item.Name)
	}
	return nil, GetProjectHandsMcpServersOutput{ProjectId: authorizedProjectId, McpServerNames: names, Servers: servers}, nil
}

type SetProjectHandsDocsInput struct {
	ProjectDocIds []int `json:"project_doc_ids" jsonschema:"Array of project_doc IDs proposed for the permanent project actor Руки"`
}

type SetProjectHandsDocsOutput struct {
	Status        string `json:"status"`
	ProjectId     int    `json:"project_id"`
	ProjectDocIds []int  `json:"project_doc_ids"`
}

func SetProjectHandsDocs(ctx context.Context, req *mcp.CallToolRequest, input SetProjectHandsDocsInput) (*mcp.CallToolResult, SetProjectHandsDocsOutput, error) {
	if authorizedRole != "fixer" {
		return &mcp.CallToolResult{IsError: true}, SetProjectHandsDocsOutput{}, fmt.Errorf("access denied: requires fixer role")
	}
	if err := requireProjectDocLocalizationCoverage(authorizedProjectId); err != nil {
		return &mcp.CallToolResult{IsError: true}, SetProjectHandsDocsOutput{}, err
	}
	globalIDs, normalizedLocal, err := resolveHandsDocIDs(input.ProjectDocIds, authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetProjectHandsDocsOutput{}, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetProjectHandsDocsOutput{}, fmt.Errorf("DB transaction start error: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec("DELETE FROM project_hands_doc WHERE project_id = ?", authorizedProjectId); err != nil {
		return &mcp.CallToolResult{IsError: true}, SetProjectHandsDocsOutput{}, fmt.Errorf("DB delete error: %v", err)
	}
	for _, docID := range globalIDs {
		if _, err := tx.Exec("INSERT OR IGNORE INTO project_hands_doc (project_id, project_doc_id) VALUES (?, ?)", authorizedProjectId, docID); err != nil {
			return &mcp.CallToolResult{IsError: true}, SetProjectHandsDocsOutput{}, fmt.Errorf("DB insert error: %v", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return &mcp.CallToolResult{IsError: true}, SetProjectHandsDocsOutput{}, fmt.Errorf("DB commit error: %v", err)
	}
	return nil, SetProjectHandsDocsOutput{Status: "success", ProjectId: authorizedProjectId, ProjectDocIds: normalizedLocal}, nil
}

type GetProjectHandsDocsInput struct{}

type GetProjectHandsDocsOutput struct {
	ProjectId     int                 `json:"project_id"`
	ProjectDocIds []int               `json:"project_doc_ids"`
	Docs          []ProjectDocSummary `json:"docs"`
}

func GetProjectHandsDocs(ctx context.Context, req *mcp.CallToolRequest, input GetProjectHandsDocsInput) (*mcp.CallToolResult, GetProjectHandsDocsOutput, error) {
	if authorizedRole != "fixer" && authorizedRole != "netrunner" {
		return &mcp.CallToolResult{IsError: true}, GetProjectHandsDocsOutput{}, fmt.Errorf("access denied: requires authenticated role")
	}
	if err := requireProjectDocLocalizationCoverage(authorizedProjectId); err != nil {
		return &mcp.CallToolResult{IsError: true}, GetProjectHandsDocsOutput{}, err
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
		FROM project_hands_doc phd
		INNER JOIN project_doc d ON d.id = phd.project_doc_id
		WHERE phd.project_id = ?
		ORDER BY d.id`, authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetProjectHandsDocsOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	defer rows.Close()
	docs := []ProjectDocSummary{}
	ids := []int{}
	for rows.Next() {
		var item ProjectDocSummary
		var content string
		if err := rows.Scan(&item.DocId, &item.Title, &content, &item.DocType, &item.ParentDocId, &item.Level, &item.Slug, &item.Path, &item.Status); err != nil {
			return &mcp.CallToolResult{IsError: true}, GetProjectHandsDocsOutput{}, fmt.Errorf("DB scan error: %v", err)
		}
		item.Summary = summarizeDocContent(content)
		docs = append(docs, item)
		ids = append(ids, item.DocId)
	}
	return nil, GetProjectHandsDocsOutput{ProjectId: authorizedProjectId, ProjectDocIds: ids, Docs: docs}, nil
}

type GetProjectHandsDocsContentInput struct{}

type GetProjectHandsDocsContentOutput struct {
	ProjectId int          `json:"project_id"`
	Docs      []ProjectDoc `json:"docs"`
}

func GetProjectHandsDocsContent(ctx context.Context, req *mcp.CallToolRequest, input GetProjectHandsDocsContentInput) (*mcp.CallToolResult, GetProjectHandsDocsContentOutput, error) {
	if authorizedRole != "fixer" && authorizedRole != "netrunner" {
		return &mcp.CallToolResult{IsError: true}, GetProjectHandsDocsContentOutput{}, fmt.Errorf("access denied: requires authenticated role")
	}
	if err := requireProjectDocLocalizationCoverage(authorizedProjectId); err != nil {
		return &mcp.CallToolResult{IsError: true}, GetProjectHandsDocsContentOutput{}, err
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
		FROM project_hands_doc phd
		INNER JOIN project_doc d ON d.id = phd.project_doc_id
		WHERE phd.project_id = ?
		ORDER BY d.id`, authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetProjectHandsDocsContentOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	defer rows.Close()
	docs := []ProjectDoc{}
	for rows.Next() {
		var item ProjectDoc
		if err := rows.Scan(&item.Id, &item.Title, &item.Content, &item.DocType, &item.ParentDocId, &item.Level, &item.Slug, &item.Path, &item.Status); err != nil {
			return &mcp.CallToolResult{IsError: true}, GetProjectHandsDocsContentOutput{}, fmt.Errorf("DB scan error: %v", err)
		}
		docs = append(docs, item)
	}
	if docs == nil {
		docs = []ProjectDoc{}
	}
	return nil, GetProjectHandsDocsContentOutput{ProjectId: authorizedProjectId, Docs: docs}, nil
}
