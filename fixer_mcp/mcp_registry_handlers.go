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

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type curatedMcpServerSpec struct {
	Name     string
	Category string
	HowTo    string
}

var curatedDefaultMcpServers = []curatedMcpServerSpec{
	{Name: "postgres", Category: "DB", HowTo: "Use for relational queries, joins, and transactional updates in PostgreSQL-backed systems."},
	{Name: "sqlite", Category: "DB", HowTo: "Use for fast local database inspection, schema checks, and deterministic test-data edits."},
	{Name: "tavily", Category: "Web-search", HowTo: "Use for focused web research when local project context is insufficient."},
	{Name: "apify", Category: "Web-search", HowTo: "Use for Apify Actor discovery/runs, structured web extraction, Apify storage/result access, and Apify docs lookup. Requires APIFY_TOKEN."},
	{Name: "figma-console-mcp", Category: "Design", HowTo: "Use for Figma design-system extraction, creation, and debugging workflows across components, variables, and layout iteration."},
	{Name: "zai-mcp-server", Category: "Design", HowTo: "Use for Droid/Netrunner visual inspection: analyze screenshots, compare UI shots, extract text from images, and diagnose visual parity issues."},
	{Name: "dart_flutter", Category: "Coding", HowTo: "Use for Flutter/Dart code generation, diagnostics, and app implementation tasks."},
	{Name: "gopls", Category: "Coding", HowTo: "Use for Go semantic tooling such as diagnostics, symbol search, references, and safe refactors."},
	{Name: "serverpod", Category: "Coding", HowTo: "Use for Serverpod architecture and API questions through the local docs mirror-backed ask-question tool."},
	{Name: "nodejs_docs", Category: "Coding", HowTo: "Use for authoritative Node.js API lookup and runtime behavior guidance."},
	{Name: "shadcn", Category: "Coding", HowTo: "Use for shadcn/ui component discovery and integration patterns in frontend tasks."},
	{Name: "playwright", Category: "Coding", HowTo: "Use for deterministic browser automation and UI scenario checks across Next.js App Router flows."},
	{Name: "chrome-devtools", Category: "Coding", HowTo: "Use for deep Chrome runtime debugging across DOM/CSS, console, network, performance, Core Web Vitals, and Lighthouse traces."},
	{Name: "eslint", Category: "Coding", HowTo: "Use for direct lint loops, rule-level fixes, and quality gates in strict TypeScript + eslint-config-next codebases."},
	{Name: "mcp-language-server", Category: "Coding", HowTo: "Use for LSP-backed semantic code operations (definitions, references, hover, diagnostics, rename, and workspace edits)."},
}

type ListMcpServersInput struct {
	IncludeAll      bool `json:"include_all,omitempty" jsonschema:"Optional flag to return full registry instead of curated defaults."`
	IncludeArchived bool `json:"include_archived,omitempty" jsonschema:"Optional flag to include archived MCP servers. Archived servers are hidden by default and from include_all unless this is true."`
}

type McpServerRecord struct {
	Id               int    `json:"id"`
	Name             string `json:"name"`
	ShortDescription string `json:"short_description"`
	LongDescription  string `json:"long_description"`
	AutoAttach       bool   `json:"auto_attach"`
	IsDefault        bool   `json:"is_default"`
	Category         string `json:"category"`
	HowTo            string `json:"how_to"`
	AuthEnvKeys      string `json:"auth_env_keys"`
	Portability      string `json:"portability"`
	InstallHint      string `json:"install_hint"`
	Archived         bool   `json:"archived"`
}

type ListMcpServersOutput struct {
	Servers []McpServerRecord `json:"servers"`
}

func ListMcpServers(ctx context.Context, req *mcp.CallToolRequest, input ListMcpServersInput) (*mcp.CallToolResult, ListMcpServersOutput, error) {
	if authorizedRole != "fixer" && authorizedRole != "netrunner" {
		return &mcp.CallToolResult{IsError: true}, ListMcpServersOutput{}, fmt.Errorf("access denied: requires authenticated role")
	}

	filters := []string{"COALESCE(is_default, 0) = 1"}
	if input.IncludeAll {
		filters = []string{}
	}
	if !input.IncludeArchived {
		filters = append(filters, "COALESCE(archived, 0) = 0")
	}
	whereClause := ""
	if len(filters) > 0 {
		whereClause = "WHERE " + strings.Join(filters, " AND ")
	}

	query := fmt.Sprintf(`
		SELECT id, name, COALESCE(short_description, ''), COALESCE(long_description, ''), COALESCE(auto_attach, 0), COALESCE(is_default, 0), COALESCE(category, ''), COALESCE(how_to, ''), COALESCE(auth_env_keys, ''), COALESCE(portability, ''), COALESCE(install_hint, ''), COALESCE(archived, 0)
		FROM mcp_server
		%s
		ORDER BY
			CASE COALESCE(category, '')
				WHEN 'DB' THEN 0
				WHEN 'Web-search' THEN 1
				WHEN 'Design' THEN 2
				WHEN 'Productivity' THEN 3
				WHEN 'Coding' THEN 4
				ELSE 99
			END,
			CASE WHEN COALESCE(category, '') = '' THEN 1 ELSE 0 END,
			COALESCE(category, ''),
			name`,
		whereClause,
	)

	rows, err := db.Query(query)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, ListMcpServersOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	defer rows.Close()

	servers := []McpServerRecord{}
	for rows.Next() {
		var item McpServerRecord
		var autoAttach int
		var isDefault int
		var archived int
		if err := rows.Scan(&item.Id, &item.Name, &item.ShortDescription, &item.LongDescription, &autoAttach, &isDefault, &item.Category, &item.HowTo, &item.AuthEnvKeys, &item.Portability, &item.InstallHint, &archived); err != nil {
			return &mcp.CallToolResult{IsError: true}, ListMcpServersOutput{}, fmt.Errorf("DB scan error: %v", err)
		}
		item.AutoAttach = autoAttach == 1
		item.IsDefault = isDefault == 1
		item.Archived = archived == 1
		servers = append(servers, item)
	}

	return nil, ListMcpServersOutput{Servers: servers}, nil
}

type McpServerUpsertInput struct {
	Name             string `json:"name" jsonschema:"MCP server name"`
	ShortDescription string `json:"short_description,omitempty" jsonschema:"Optional short description"`
	LongDescription  string `json:"long_description,omitempty" jsonschema:"Optional long description"`
	AutoAttach       *bool  `json:"auto_attach,omitempty" jsonschema:"Optional auto attach flag"`
	IsDefault        *bool  `json:"is_default,omitempty" jsonschema:"Optional curated-default flag"`
	Category         string `json:"category,omitempty" jsonschema:"Optional category label for MCP picker grouping"`
	HowTo            string `json:"how_to,omitempty" jsonschema:"Optional concise usage guidance for netrunner prompt injection"`
	AuthEnvKeys      string `json:"auth_env_keys,omitempty" jsonschema:"Optional comma-separated env var names required for auth; names only, never values"`
	Portability      string `json:"portability,omitempty" jsonschema:"Optional portability label: portable or local-only"`
	InstallHint      string `json:"install_hint,omitempty" jsonschema:"Optional one-line install/deploy instruction for a new machine"`
	Archived         *bool  `json:"archived,omitempty" jsonschema:"Optional archived flag"`
}

type SyncMcpServersInput struct {
	Servers          []McpServerUpsertInput `json:"servers,omitempty" jsonschema:"Optional MCP server records for explicit upsert"`
	SourceConfigPath string                 `json:"source_config_path,omitempty" jsonschema:"Optional config path (defaults to mcp_config.json when servers is empty)"`
}

type SyncMcpServersOutput struct {
	Status   string `json:"status"`
	Inserted int    `json:"inserted"`
	Updated  int    `json:"updated"`
	Total    int    `json:"total"`
}

func SyncMcpServers(ctx context.Context, req *mcp.CallToolRequest, input SyncMcpServersInput) (*mcp.CallToolResult, SyncMcpServersOutput, error) {
	if authorizedRole != "fixer" {
		return &mcp.CallToolResult{IsError: true}, SyncMcpServersOutput{}, fmt.Errorf("access denied: requires fixer role")
	}

	specs := make([]McpServerUpsertInput, 0, len(input.Servers))
	specs = append(specs, input.Servers...)
	if len(specs) == 0 {
		configPath := strings.TrimSpace(input.SourceConfigPath)
		if configPath == "" {
			configPath = filepath.Join(".", "mcp_config.json")
		}

		content, err := os.ReadFile(configPath)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, SyncMcpServersOutput{}, fmt.Errorf("failed to read config path %q: %v", configPath, err)
		}

		var parsed mcpConfigFile
		if err := json.Unmarshal(content, &parsed); err != nil {
			return &mcp.CallToolResult{IsError: true}, SyncMcpServersOutput{}, fmt.Errorf("invalid mcp config JSON: %v", err)
		}

		for name := range parsed.McpServers {
			specs = append(specs, McpServerUpsertInput{Name: name})
		}
	}

	byName := map[string]McpServerUpsertInput{}
	orderedNames := make([]string, 0, len(specs))
	for _, spec := range specs {
		name := strings.TrimSpace(spec.Name)
		if name == "" {
			continue
		}
		if _, exists := byName[name]; !exists {
			orderedNames = append(orderedNames, name)
		}
		spec.Name = name
		byName[name] = spec
	}
	sort.Strings(orderedNames)

	insertedCount := 0
	updatedCount := 0
	for _, name := range orderedNames {
		spec := byName[name]
		if curatedSpec, ok := findCuratedDefaultMcpServer(name); ok {
			if strings.TrimSpace(spec.Category) == "" {
				spec.Category = curatedSpec.Category
			}
			if strings.TrimSpace(spec.HowTo) == "" {
				spec.HowTo = curatedSpec.HowTo
			}
			if spec.IsDefault == nil {
				spec.IsDefault = boolPtr(true)
			}
		}

		autoAttach := spec.AutoAttach
		wasInserted, err := upsertMcpServer(
			name,
			strings.TrimSpace(spec.ShortDescription),
			strings.TrimSpace(spec.LongDescription),
			strings.TrimSpace(spec.Category),
			strings.TrimSpace(spec.HowTo),
			strings.TrimSpace(spec.AuthEnvKeys),
			strings.TrimSpace(spec.Portability),
			strings.TrimSpace(spec.InstallHint),
			autoAttach,
			spec.IsDefault,
			spec.Archived,
		)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, SyncMcpServersOutput{}, fmt.Errorf("failed to upsert MCP server %q: %v", name, err)
		}
		if wasInserted {
			insertedCount++
		} else {
			updatedCount++
		}
	}

	return nil, SyncMcpServersOutput{
		Status:   "success",
		Inserted: insertedCount,
		Updated:  updatedCount,
		Total:    len(orderedNames),
	}, nil
}

type SetProjectMcpServersInput struct {
	McpServerNames []string `json:"mcp_server_names" jsonschema:"Array of MCP server names allowed for current project"`
}

type SetProjectMcpServersOutput struct {
	Status         string   `json:"status"`
	ProjectId      int      `json:"project_id"`
	McpServerNames []string `json:"mcp_server_names"`
}

func SetProjectMcpServers(ctx context.Context, req *mcp.CallToolRequest, input SetProjectMcpServersInput) (*mcp.CallToolResult, SetProjectMcpServersOutput, error) {
	if authorizedRole != "fixer" {
		return &mcp.CallToolResult{IsError: true}, SetProjectMcpServersOutput{}, fmt.Errorf("access denied: requires fixer role")
	}

	normalizedNames := normalizeMcpServerNames(input.McpServerNames)
	filteredNames := make([]string, 0, len(normalizedNames))
	for _, name := range normalizedNames {
		if name == forcedMcpServerName {
			continue
		}
		filteredNames = append(filteredNames, name)
	}

	serverIDs := make([]int, 0, len(filteredNames))
	missing := make([]string, 0)
	for _, name := range filteredNames {
		var serverID int
		err := db.QueryRow("SELECT id FROM mcp_server WHERE name = ?", name).Scan(&serverID)
		if err == sql.ErrNoRows {
			missing = append(missing, name)
			continue
		}
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, SetProjectMcpServersOutput{}, fmt.Errorf("DB query error: %v", err)
		}
		serverIDs = append(serverIDs, serverID)
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return &mcp.CallToolResult{IsError: true}, SetProjectMcpServersOutput{}, fmt.Errorf("unknown MCP server(s): %s", strings.Join(missing, ", "))
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetProjectMcpServersOutput{}, fmt.Errorf("DB transaction start error: %v", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if _, err := tx.Exec("DELETE FROM project_mcp_server WHERE project_id = ?", authorizedProjectId); err != nil {
		return &mcp.CallToolResult{IsError: true}, SetProjectMcpServersOutput{}, fmt.Errorf("DB delete error: %v", err)
	}
	for _, serverID := range serverIDs {
		if _, err := tx.Exec(
			"INSERT OR IGNORE INTO project_mcp_server (project_id, mcp_server_id) VALUES (?, ?)",
			authorizedProjectId,
			serverID,
		); err != nil {
			return &mcp.CallToolResult{IsError: true}, SetProjectMcpServersOutput{}, fmt.Errorf("DB insert error: %v", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return &mcp.CallToolResult{IsError: true}, SetProjectMcpServersOutput{}, fmt.Errorf("DB commit error: %v", err)
	}

	return nil, SetProjectMcpServersOutput{
		Status:         "success",
		ProjectId:      authorizedProjectId,
		McpServerNames: filteredNames,
	}, nil
}

type GetProjectMcpServersInput struct{}

type GetProjectMcpServersOutput struct {
	ProjectId      int               `json:"project_id"`
	McpServerNames []string          `json:"mcp_server_names"`
	Servers        []McpServerRecord `json:"servers"`
}

func GetProjectMcpServers(ctx context.Context, req *mcp.CallToolRequest, input GetProjectMcpServersInput) (*mcp.CallToolResult, GetProjectMcpServersOutput, error) {
	if authorizedRole != "fixer" && authorizedRole != "netrunner" {
		return &mcp.CallToolResult{IsError: true}, GetProjectMcpServersOutput{}, fmt.Errorf("access denied: requires authenticated role")
	}

	if err := ensureProjectMcpBindingsForProject(authorizedProjectId); err != nil {
		return &mcp.CallToolResult{IsError: true}, GetProjectMcpServersOutput{}, fmt.Errorf("failed to bootstrap project MCP bindings: %v", err)
	}

	rows, err := db.Query(
		`SELECT s.id, s.name, COALESCE(s.short_description, ''), COALESCE(s.long_description, ''), COALESCE(s.auto_attach, 0), COALESCE(s.is_default, 0), COALESCE(s.category, ''), COALESCE(s.how_to, ''), COALESCE(s.auth_env_keys, ''), COALESCE(s.portability, ''), COALESCE(s.install_hint, ''), COALESCE(s.archived, 0)
		 FROM project_mcp_server pms
		 INNER JOIN mcp_server s ON s.id = pms.mcp_server_id
		 WHERE pms.project_id = ?
		 ORDER BY
			CASE COALESCE(s.category, '')
				WHEN 'DB' THEN 0
				WHEN 'Web-search' THEN 1
				WHEN 'Design' THEN 2
				WHEN 'Productivity' THEN 3
				WHEN 'Coding' THEN 4
				ELSE 99
			END,
			COALESCE(s.category, ''),
			s.name`,
		authorizedProjectId,
	)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetProjectMcpServersOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	defer rows.Close()

	servers := []McpServerRecord{}
	names := []string{}
	for rows.Next() {
		var item McpServerRecord
		var autoAttach int
		var isDefault int
		var archived int
		if err := rows.Scan(&item.Id, &item.Name, &item.ShortDescription, &item.LongDescription, &autoAttach, &isDefault, &item.Category, &item.HowTo, &item.AuthEnvKeys, &item.Portability, &item.InstallHint, &archived); err != nil {
			return &mcp.CallToolResult{IsError: true}, GetProjectMcpServersOutput{}, fmt.Errorf("DB scan error: %v", err)
		}
		item.AutoAttach = autoAttach == 1
		item.IsDefault = isDefault == 1
		item.Archived = archived == 1
		servers = append(servers, item)
		names = append(names, item.Name)
	}

	return nil, GetProjectMcpServersOutput{
		ProjectId:      authorizedProjectId,
		McpServerNames: names,
		Servers:        servers,
	}, nil
}

type SetSessionMcpServersInput struct {
	SessionId      int      `json:"session_id" jsonschema:"Session ID to assign MCP servers for"`
	McpServerNames []string `json:"mcp_server_names" jsonschema:"Array of MCP server names to assign"`
}

type SetSessionMcpServersOutput struct {
	Status         string   `json:"status"`
	SessionId      int      `json:"session_id"`
	McpServerNames []string `json:"mcp_server_names"`
}

func SetSessionMcpServers(ctx context.Context, req *mcp.CallToolRequest, input SetSessionMcpServersInput) (*mcp.CallToolResult, SetSessionMcpServersOutput, error) {
	if authorizedRole != "fixer" {
		return &mcp.CallToolResult{IsError: true}, SetSessionMcpServersOutput{}, fmt.Errorf("access denied: requires fixer role")
	}

	globalSessionID, err := globalSessionIDFromProjectScoped(input.SessionId, authorizedProjectId)
	if err == sql.ErrNoRows {
		return &mcp.CallToolResult{IsError: true}, SetSessionMcpServersOutput{}, fmt.Errorf("session not found in current project")
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetSessionMcpServersOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	belongs, err := sessionBelongsToProject(globalSessionID, authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetSessionMcpServersOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	if !belongs {
		return &mcp.CallToolResult{IsError: true}, SetSessionMcpServersOutput{}, fmt.Errorf("session not found in current project")
	}

	if err := ensureProjectMcpBindingsForProject(authorizedProjectId); err != nil {
		return &mcp.CallToolResult{IsError: true}, SetSessionMcpServersOutput{}, fmt.Errorf("failed to bootstrap project MCP bindings: %v", err)
	}

	allowedNames, err := loadProjectAllowedMcpNames(authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetSessionMcpServersOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	normalizedNames := normalizeMcpServerNames(input.McpServerNames)
	disallowed := make([]string, 0)
	for _, name := range normalizedNames {
		if _, ok := allowedNames[name]; !ok {
			disallowed = append(disallowed, name)
		}
	}
	if len(disallowed) > 0 {
		sort.Strings(disallowed)
		return &mcp.CallToolResult{IsError: true}, SetSessionMcpServersOutput{}, fmt.Errorf("MCP server(s) not allowed for current project: %s", strings.Join(disallowed, ", "))
	}

	serverIds := make([]int, 0, len(normalizedNames))
	missing := make([]string, 0)
	for _, name := range normalizedNames {
		var serverId int
		err := db.QueryRow("SELECT id FROM mcp_server WHERE name = ?", name).Scan(&serverId)
		if err == sql.ErrNoRows {
			missing = append(missing, name)
			continue
		}
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, SetSessionMcpServersOutput{}, fmt.Errorf("DB query error: %v", err)
		}
		serverIds = append(serverIds, serverId)
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return &mcp.CallToolResult{IsError: true}, SetSessionMcpServersOutput{}, fmt.Errorf("unknown MCP server(s): %s", strings.Join(missing, ", "))
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetSessionMcpServersOutput{}, fmt.Errorf("DB transaction start error: %v", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if _, err := tx.Exec("DELETE FROM session_mcp_server WHERE session_id = ?", globalSessionID); err != nil {
		return &mcp.CallToolResult{IsError: true}, SetSessionMcpServersOutput{}, fmt.Errorf("DB delete error: %v", err)
	}

	for _, serverId := range serverIds {
		if _, err := tx.Exec("INSERT OR IGNORE INTO session_mcp_server (session_id, mcp_server_id) VALUES (?, ?)", globalSessionID, serverId); err != nil {
			return &mcp.CallToolResult{IsError: true}, SetSessionMcpServersOutput{}, fmt.Errorf("DB insert error: %v", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return &mcp.CallToolResult{IsError: true}, SetSessionMcpServersOutput{}, fmt.Errorf("DB commit error: %v", err)
	}

	return nil, SetSessionMcpServersOutput{
		Status:         "success",
		SessionId:      input.SessionId,
		McpServerNames: normalizedNames,
	}, nil
}

type GetSessionMcpServersInput struct {
	SessionId int `json:"session_id" jsonschema:"Session ID to read MCP assignments from"`
}

type GetSessionMcpServersOutput struct {
	SessionId      int               `json:"session_id"`
	McpServerNames []string          `json:"mcp_server_names"`
	Servers        []McpServerRecord `json:"servers"`
}

func GetSessionMcpServers(ctx context.Context, req *mcp.CallToolRequest, input GetSessionMcpServersInput) (*mcp.CallToolResult, GetSessionMcpServersOutput, error) {
	if authorizedRole != "fixer" && authorizedRole != "netrunner" {
		return &mcp.CallToolResult{IsError: true}, GetSessionMcpServersOutput{}, fmt.Errorf("access denied: requires authenticated role")
	}

	globalSessionID, err := globalSessionIDFromProjectScoped(input.SessionId, authorizedProjectId)
	if err == sql.ErrNoRows {
		return &mcp.CallToolResult{IsError: true}, GetSessionMcpServersOutput{}, fmt.Errorf("session not found in current project")
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetSessionMcpServersOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	belongs, err := sessionBelongsToProject(globalSessionID, authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetSessionMcpServersOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	if !belongs {
		return &mcp.CallToolResult{IsError: true}, GetSessionMcpServersOutput{}, fmt.Errorf("session not found in current project")
	}

	rows, err := db.Query(`
		SELECT s.id, s.name, COALESCE(s.short_description, ''), COALESCE(s.long_description, ''), COALESCE(s.auto_attach, 0), COALESCE(s.is_default, 0), COALESCE(s.category, ''), COALESCE(s.how_to, ''), COALESCE(s.auth_env_keys, ''), COALESCE(s.portability, ''), COALESCE(s.install_hint, ''), COALESCE(s.archived, 0)
		FROM session_mcp_server sms
		INNER JOIN mcp_server s ON s.id = sms.mcp_server_id
		WHERE sms.session_id = ?
		ORDER BY
			CASE COALESCE(s.category, '')
				WHEN 'DB' THEN 0
				WHEN 'Web-search' THEN 1
				WHEN 'Design' THEN 2
				WHEN 'Productivity' THEN 3
				WHEN 'Coding' THEN 4
				ELSE 99
			END,
			COALESCE(s.category, ''),
			s.name`, globalSessionID)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetSessionMcpServersOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	defer rows.Close()

	servers := []McpServerRecord{}
	names := []string{}
	for rows.Next() {
		var item McpServerRecord
		var autoAttach int
		var isDefault int
		var archived int
		if err := rows.Scan(&item.Id, &item.Name, &item.ShortDescription, &item.LongDescription, &autoAttach, &isDefault, &item.Category, &item.HowTo, &item.AuthEnvKeys, &item.Portability, &item.InstallHint, &archived); err != nil {
			return &mcp.CallToolResult{IsError: true}, GetSessionMcpServersOutput{}, fmt.Errorf("DB scan error: %v", err)
		}
		item.AutoAttach = autoAttach == 1
		item.IsDefault = isDefault == 1
		item.Archived = archived == 1
		servers = append(servers, item)
		names = append(names, item.Name)
	}

	return nil, GetSessionMcpServersOutput{
		SessionId:      input.SessionId,
		McpServerNames: names,
		Servers:        servers,
	}, nil
}
