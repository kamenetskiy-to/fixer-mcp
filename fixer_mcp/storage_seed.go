package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
)

type mcpConfigFile struct {
	McpServers map[string]json.RawMessage `json:"mcpServers"`
}

func normalizeMcpServerNames(names []string) []string {
	seen := make(map[string]struct{}, len(names))
	normalized := make([]string, 0, len(names))
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		normalized = append(normalized, name)
	}
	sort.Strings(normalized)
	return normalized
}

type mcpServerDBExecutor interface {
	QueryRow(query string, args ...any) *sql.Row
	Exec(query string, args ...any) (sql.Result, error)
}

func upsertMcpServerWithExecutor(
	exec mcpServerDBExecutor,
	name, shortDescription, longDescription, category, howTo string,
	authEnvKeys, portability, installHint string,
	autoAttach *bool,
	isDefault *bool,
	archived *bool,
) (bool, error) {
	var existingId int
	err := exec.QueryRow("SELECT id FROM mcp_server WHERE name = ?", name).Scan(&existingId)
	switch {
	case err == nil:
		updateFields := []string{"updated_at = CURRENT_TIMESTAMP"}
		args := []any{}
		if strings.TrimSpace(shortDescription) != "" {
			updateFields = append(updateFields, "short_description = ?")
			args = append(args, shortDescription)
		}
		if strings.TrimSpace(longDescription) != "" {
			updateFields = append(updateFields, "long_description = ?")
			args = append(args, longDescription)
		}
		if autoAttach != nil {
			updateFields = append(updateFields, "auto_attach = ?")
			args = append(args, boolToInt(*autoAttach))
		}
		if isDefault != nil {
			updateFields = append(updateFields, "is_default = ?")
			args = append(args, boolToInt(*isDefault))
		}
		if strings.TrimSpace(category) != "" {
			updateFields = append(updateFields, "category = ?")
			args = append(args, strings.TrimSpace(category))
		}
		if strings.TrimSpace(howTo) != "" {
			updateFields = append(updateFields, "how_to = ?")
			args = append(args, strings.TrimSpace(howTo))
		}
		if strings.TrimSpace(authEnvKeys) != "" {
			updateFields = append(updateFields, "auth_env_keys = ?")
			args = append(args, strings.TrimSpace(authEnvKeys))
		}
		if strings.TrimSpace(portability) != "" {
			updateFields = append(updateFields, "portability = ?")
			args = append(args, strings.TrimSpace(portability))
		}
		if strings.TrimSpace(installHint) != "" {
			updateFields = append(updateFields, "install_hint = ?")
			args = append(args, strings.TrimSpace(installHint))
		}
		if archived != nil {
			updateFields = append(updateFields, "archived = ?")
			args = append(args, boolToInt(*archived))
		}

		query := fmt.Sprintf(
			"UPDATE mcp_server SET %s WHERE id = ?",
			strings.Join(updateFields, ", "),
		)
		args = append(args, existingId)
		_, execErr := exec.Exec(query, args...)
		return false, execErr
	case err == sql.ErrNoRows:
		autoAttachValue := 0
		if autoAttach != nil && *autoAttach {
			autoAttachValue = 1
		}
		defaultValue := 0
		if isDefault != nil && *isDefault {
			defaultValue = 1
		}
		_, execErr := exec.Exec(
			`INSERT INTO mcp_server (name, short_description, long_description, auto_attach, is_default, category, how_to, auth_env_keys, portability, install_hint, archived)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			name,
			shortDescription,
			longDescription,
			autoAttachValue,
			defaultValue,
			strings.TrimSpace(category),
			strings.TrimSpace(howTo),
			strings.TrimSpace(authEnvKeys),
			strings.TrimSpace(portability),
			strings.TrimSpace(installHint),
			boolToInt(archived != nil && *archived),
		)
		return true, execErr
	default:
		return false, err
	}
}

func upsertMcpServer(
	name, shortDescription, longDescription, category, howTo string,
	authEnvKeys, portability, installHint string,
	autoAttach *bool,
	isDefault *bool,
	archived *bool,
) (bool, error) {
	return upsertMcpServerWithExecutor(db, name, shortDescription, longDescription, category, howTo, authEnvKeys, portability, installHint, autoAttach, isDefault, archived)
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func boolPtr(value bool) *bool {
	return &value
}

func findCuratedDefaultMcpServer(name string) (curatedMcpServerSpec, bool) {
	for _, spec := range curatedDefaultMcpServers {
		if spec.Name == name {
			return spec, true
		}
	}
	return curatedMcpServerSpec{}, false
}

func applyCuratedDefaultMcpServers() error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if _, err := tx.Exec("UPDATE mcp_server SET is_default = 0, updated_at = CURRENT_TIMESTAMP WHERE COALESCE(is_default, 0) != 0"); err != nil {
		return err
	}

	for _, spec := range curatedDefaultMcpServers {
		if _, err := upsertMcpServerWithExecutor(
			tx,
			spec.Name,
			"",
			"",
			spec.Category,
			spec.HowTo,
			"",
			"",
			"",
			nil,
			boolPtr(true),
			boolPtr(false),
		); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func ensureProjectMcpBindingsForProjectTx(tx *sql.Tx, projectID int) error {
	var existingCount int
	if err := tx.QueryRow(
		"SELECT COUNT(*) FROM project_mcp_server WHERE project_id = ?",
		projectID,
	).Scan(&existingCount); err != nil {
		return err
	}
	if existingCount > 0 {
		return nil
	}

	allowedServerIDs := map[int]struct{}{}
	defaultRows, err := tx.Query(
		"SELECT id FROM mcp_server WHERE COALESCE(is_default, 0) = 1 ORDER BY id",
	)
	if err != nil {
		return err
	}
	for defaultRows.Next() {
		var serverID int
		if scanErr := defaultRows.Scan(&serverID); scanErr != nil {
			_ = defaultRows.Close()
			return scanErr
		}
		allowedServerIDs[serverID] = struct{}{}
	}
	if closeErr := defaultRows.Close(); closeErr != nil {
		return closeErr
	}

	assignedRows, err := tx.Query(
		`SELECT DISTINCT sms.mcp_server_id
		 FROM session_mcp_server sms
		 INNER JOIN session s ON s.id = sms.session_id
		 WHERE s.project_id = ?
		 ORDER BY sms.mcp_server_id`,
		projectID,
	)
	if err != nil {
		return err
	}
	for assignedRows.Next() {
		var serverID int
		if scanErr := assignedRows.Scan(&serverID); scanErr != nil {
			_ = assignedRows.Close()
			return scanErr
		}
		allowedServerIDs[serverID] = struct{}{}
	}
	if closeErr := assignedRows.Close(); closeErr != nil {
		return closeErr
	}

	orderedIDs := make([]int, 0, len(allowedServerIDs))
	for serverID := range allowedServerIDs {
		orderedIDs = append(orderedIDs, serverID)
	}
	sort.Ints(orderedIDs)

	for _, serverID := range orderedIDs {
		if _, err := tx.Exec(
			"INSERT OR IGNORE INTO project_mcp_server (project_id, mcp_server_id) VALUES (?, ?)",
			projectID,
			serverID,
		); err != nil {
			return err
		}
	}
	return nil
}

func ensureProjectMcpBindingsForProject(projectID int) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()
	if err := ensureProjectMcpBindingsForProjectTx(tx, projectID); err != nil {
		return err
	}
	return tx.Commit()
}

func loadProjectAllowedMcpNames(projectID int) (map[string]struct{}, error) {
	rows, err := db.Query(
		`SELECT s.name
		 FROM project_mcp_server pms
		 INNER JOIN mcp_server s ON s.id = pms.mcp_server_id
		 WHERE pms.project_id = ?
		 ORDER BY s.name`,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	allowed := map[string]struct{}{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		allowed[name] = struct{}{}
	}
	allowed[forcedMcpServerName] = struct{}{}
	return allowed, nil
}

func seedProjectScopedMcpBindings() error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	type projectSeedRow struct {
		id   int
		name string
		cwd  string
	}

	projects := []projectSeedRow{}
	rows, err := tx.Query("SELECT id, COALESCE(name, ''), COALESCE(cwd, '') FROM project ORDER BY id")
	if err != nil {
		return err
	}
	for rows.Next() {
		item := projectSeedRow{}
		if scanErr := rows.Scan(&item.id, &item.name, &item.cwd); scanErr != nil {
			_ = rows.Close()
			return scanErr
		}
		projects = append(projects, item)
	}
	if closeErr := rows.Close(); closeErr != nil {
		return closeErr
	}

	philologistsProjectIDs := []int{}
	for _, project := range projects {
		if err := ensureProjectMcpBindingsForProjectTx(tx, project.id); err != nil {
			return err
		}

		projectName := strings.ToLower(strings.TrimSpace(project.name))
		projectCwd := strings.ToLower(strings.TrimSpace(project.cwd))
		if strings.Contains(projectName, philologistsProjectMarker) || strings.Contains(projectCwd, philologistsProjectMarker) {
			philologistsProjectIDs = append(philologistsProjectIDs, project.id)
		}
	}

	if len(philologistsProjectIDs) > 0 {
		_, err := upsertMcpServerWithExecutor(
			tx,
			researchQueryMcpName,
			"",
			"",
			"Web-search",
			"Use for project-specific research workflows in Philologists project analysis tasks.",
			"",
			"",
			"",
			nil,
			boolPtr(false),
			nil,
		)
		if err != nil {
			return err
		}

		var researchServerID int
		if err := tx.QueryRow("SELECT id FROM mcp_server WHERE name = ?", researchQueryMcpName).Scan(&researchServerID); err != nil {
			return err
		}
		for _, projectID := range philologistsProjectIDs {
			if _, err := tx.Exec(
				"INSERT OR IGNORE INTO project_mcp_server (project_id, mcp_server_id) VALUES (?, ?)",
				projectID,
				researchServerID,
			); err != nil {
				return err
			}
		}
	}

	return tx.Commit()
}

func pruneDeprecatedProjectMcpBindings() error {
	_, err := db.Exec(
		`DELETE FROM project_mcp_server
		 WHERE mcp_server_id IN (
			 SELECT id
			 FROM mcp_server
			 WHERE name = ?
		 )`,
		telegramNotifyMcpName,
	)
	return err
}

func syncMcpRegistryFromConfig(configPath string) (int, error) {
	content, err := os.ReadFile(configPath)
	if err != nil {
		return 0, err
	}

	var parsed mcpConfigFile
	if err := json.Unmarshal(content, &parsed); err != nil {
		return 0, err
	}

	names := make([]string, 0, len(parsed.McpServers))
	for name := range parsed.McpServers {
		names = append(names, name)
	}
	names = normalizeMcpServerNames(names)

	synced := 0
	for _, name := range names {
		category := ""
		howTo := ""
		var isDefault *bool
		if curatedSpec, ok := findCuratedDefaultMcpServer(name); ok {
			category = curatedSpec.Category
			howTo = curatedSpec.HowTo
			isDefault = boolPtr(true)
		}

		_, err := upsertMcpServer(name, "", "", category, howTo, "", "", "", nil, isDefault, nil)
		if err != nil {
			return synced, err
		}
		synced++
	}
	return synced, nil
}

type mcpMarketplaceCatalogSpec struct {
	Name             string
	ShortDescription string
	LongDescription  string
	Category         string
	HowTo            string
	AuthEnvKeys      string
	Portability      string
	InstallHint      string
	IsDefault        *bool
	Archived         bool
}

var zeroUsageArchivedMcpServers = []string{
	"MCP_DOCKER",
	"clickhouse",
	"corpus_search_mcp",
	"dagger",
	"dart_project_tools",
	"dataforseo",
	"deep_research",
	"deploy-mcp",
	"drawio",
	"figma",
	"firebase_mcp",
	"framer",
	"french_exam_metrics_mcp",
	"gcp_mcp",
	"genui_workspace_metrics_mcp",
	"github",
	"global_image_assets",
	"google_docs",
	"google_news_trends",
	"google_sheets",
	"gsc",
	"laravel_mcp_companion",
	"librex",
	"mcp_mermaid",
	"n8n",
	"ozon",
	"pandas_mcp",
	"plane",
	"react-native-guide",
	"resume-generator",
	"rust_architect",
	"schemacrawler",
	"searCrawl",
	"seo_mcp",
	"smysl_mcp",
	"steam",
	"svgmaker",
}

var liveMcpMarketplaceCatalog = []mcpMarketplaceCatalogSpec{
	{
		Name:             "fixer_mcp",
		ShortDescription: "Project-bound orchestration MCP for fixer/netrunner/overseer roles.",
		Category:         "Control-plane",
		HowTo:            "Use for durable Fixer orchestration state, task checkout/completion, project docs, MCP assignment, and autonomous-run control.",
		AuthEnvKeys:      "FIXER_DB_PATH,FIXER_MCP_LOCKED_ROLE",
		Portability:      "local-only",
		InstallHint:      "Build with `go build -o fixer_mcp .` in fixer_mcp/; set FIXER_DB_PATH and locked role env for the target role.",
		IsDefault:        boolPtr(false),
	},
	{
		Name:             "apify",
		ShortDescription: "Apify Actor discovery, runs, datasets, and structured web extraction.",
		Category:         "Web-search",
		HowTo:            "Use for Apify Actor discovery/runs, structured web extraction, Apify storage/result access, and Apify docs lookup. Requires APIFY_TOKEN.",
		AuthEnvKeys:      "APIFY_TOKEN",
		Portability:      "portable",
		InstallHint:      "Enable the Apify remote MCP with bearer_token_env_var=APIFY_TOKEN.",
	},
	{
		Name:             "chrome-devtools",
		ShortDescription: "Chrome DevTools browser debugging and inspection MCP.",
		Category:         "Coding",
		HowTo:            "Use for deep Chrome runtime debugging across DOM/CSS, console, network, performance, Core Web Vitals, and Lighthouse traces.",
		Portability:      "portable",
		InstallHint:      "npx -y chrome-devtools-mcp@latest --isolated --headless",
	},
	{
		Name:             "computer-use",
		ShortDescription: "Local Codex Computer Use plugin MCP for macOS desktop control.",
		Category:         "Desktop",
		HowTo:            "Use only when a task requires direct desktop/app control through the installed Codex Computer Use plugin.",
		Portability:      "local-only",
		InstallHint:      "Install/enable the Codex Computer Use plugin locally; server binary lives inside the plugin app bundle.",
	},
	{
		Name:             "dart_flutter",
		ShortDescription: "Dart/Flutter automation for analysis, formatting, tests, dependency work, and runtime diagnostics.",
		Category:         "Coding",
		HowTo:            "Use for Flutter/Dart code generation, diagnostics, and app implementation tasks.",
		Portability:      "local-only",
		InstallHint:      "Install the local dart_mcp wrapper checkout under MCP_SERVERS_ROOT and expose dart/flutter tooling on PATH.",
	},
	{
		Name:             "eslint",
		ShortDescription: "ESLint MCP for lint inspection and rule-guided fixes.",
		Category:         "Coding",
		HowTo:            "Use for direct lint loops, rule-level fixes, and quality gates in strict TypeScript + eslint-config-next codebases.",
		Portability:      "portable",
		InstallHint:      "npx -y @eslint/mcp@0.2.0",
	},
	{
		Name:             "exa",
		ShortDescription: "Exa remote search MCP wrapper for web/source discovery.",
		Category:         "Web-search",
		HowTo:            "Use for Exa-powered web research when exact source discovery is more useful than general search.",
		AuthEnvKeys:      "EXA_API_KEY",
		Portability:      "portable",
		InstallHint:      "npx -y mcp-remote https://mcp.exa.ai/mcp; needs EXA_API_KEY.",
	},
	{
		Name:             "figma-console-mcp",
		ShortDescription: "Figma console automation and design-system extraction MCP.",
		Category:         "Design",
		HowTo:            "Use for Figma design-system extraction, creation, and debugging workflows across components, variables, and layout iteration.",
		AuthEnvKeys:      "GEMINI_API_KEY,OPENROUTER_API_KEY",
		Portability:      "portable",
		InstallHint:      "npx -y figma-console-mcp@latest; set GEMINI_API_KEY/OPENROUTER_API_KEY when app features need them.",
	},
	{
		Name:             "google_search",
		ShortDescription: "Google Search MCP backed by the local google_search_mcp server.",
		Category:         "Web-search",
		HowTo:            "Use for Google-search-backed research with scraping and result summarization when configured for the project.",
		AuthEnvKeys:      "GOOGLE_API_KEY",
		Portability:      "local-only",
		InstallHint:      "Install the local google_search_mcp wrapper and set GOOGLE_API_KEY.",
	},
	{
		Name:             "gopls",
		ShortDescription: "Go language tooling for workspace discovery, symbol search, references, and diagnostics.",
		Category:         "Coding",
		HowTo:            "Use for Go semantic tooling such as diagnostics, symbol search, references, and safe refactors.",
		Portability:      "local-only",
		InstallHint:      "Install gopls and the local gopls_mcp wrapper; set GOPLS_MCP_CWD only when overriding the workspace.",
	},
	{
		Name:             "mcp-language-server",
		ShortDescription: "Generic LSP-backed MCP server for semantic code operations.",
		Category:         "Coding",
		HowTo:            "Use for LSP-backed semantic code operations (definitions, references, hover, diagnostics, rename, and workspace edits).",
		Portability:      "local-only",
		InstallHint:      "Install mcp-language-server and language servers locally; configure --workspace and --lsp paths.",
	},
	{
		Name:             "nodejs_docs",
		ShortDescription: "Node.js standard library API reference search and module documentation lookup.",
		Category:         "Coding",
		HowTo:            "Use for authoritative Node.js API lookup and runtime behavior guidance.",
		Portability:      "local-only",
		InstallHint:      "Install the local nodejs_docs_mcp wrapper checkout and Node.js runtime.",
	},
	{
		Name:             "openaiDeveloperDocs",
		ShortDescription: "OpenAI developer documentation search and reference MCP.",
		Category:         "Docs",
		HowTo:            "Use for up-to-date OpenAI API/product documentation and implementation details from official docs.",
		Portability:      "portable",
		InstallHint:      "Enable the OpenAI Developer Docs MCP in the client runtime; no local project checkout is required.",
	},
	{
		Name:             "playwright",
		ShortDescription: "Playwright MCP for deterministic browser automation.",
		Category:         "Coding",
		HowTo:            "Use for deterministic browser automation and UI scenario checks across Next.js App Router flows.",
		Portability:      "portable",
		InstallHint:      "npx -y @playwright/mcp@latest --isolated --headless",
	},
	{
		Name:             "postgres",
		ShortDescription: "PostgreSQL diagnostics and operations: inspect schemas, run queries, and analyze database health/performance.",
		Category:         "DB",
		HowTo:            "Use for relational queries, joins, and transactional updates in PostgreSQL-backed systems.",
		Portability:      "local-only",
		InstallHint:      "Install the local universal_db_mcps/postgres wrapper and provide project database connection env/config.",
	},
	{
		Name:             "research_query_mcp",
		ShortDescription: "Project-scoped research query MCP for Philologists workflows.",
		Category:         "Web-search",
		HowTo:            "Use for project-specific research workflows in Philologists project analysis tasks.",
		Portability:      "local-only",
		InstallHint:      "Install the project-specific research_query_mcp server for the target Philologists workspace.",
	},
	{
		Name:             "serverpod",
		ShortDescription: "Local Serverpod docs mirror-backed Q&A.",
		Category:         "Coding",
		HowTo:            "Use for Serverpod architecture and API questions through the local docs mirror-backed ask-question tool.",
		Portability:      "local-only",
		InstallHint:      "Install the local serverpod_mcp wrapper and docs mirror under ${MCP_SERVERS_ROOT}/serverpod_mcp.",
	},
	{
		Name:             "shadcn",
		ShortDescription: "shadcn/ui component discovery and integration guidance.",
		Category:         "Coding",
		HowTo:            "Use for shadcn/ui component discovery and integration patterns in frontend tasks.",
		Portability:      "local-only",
		InstallHint:      "Install the local shadcn_mcp wrapper checkout and Node.js runtime.",
	},
	{
		Name:             "sqlite",
		ShortDescription: "SQLite database diagnostics and query execution.",
		Category:         "DB",
		HowTo:            "Use for fast local database inspection, schema checks, and deterministic test-data edits.",
		Portability:      "local-only",
		InstallHint:      "Install the local universal_db_mcps/sqlite wrapper and point it at the target SQLite database.",
	},
	{
		Name:             "tavily",
		ShortDescription: "Tavily search MCP for focused web research.",
		Category:         "Web-search",
		HowTo:            "Use for focused web research when local project context is insufficient.",
		AuthEnvKeys:      "TAVILY_API_KEY",
		Portability:      "portable",
		InstallHint:      "npx -y tavily-mcp@latest; needs TAVILY_API_KEY.",
	},
	{
		Name:             "telegram_notify",
		ShortDescription: "Telegram notifications and simple messaging hooks.",
		Category:         "Productivity",
		HowTo:            "Use for out-of-band operator notifications only when the task explicitly needs Telegram messaging outside the normal Fixer flow.",
		AuthEnvKeys:      "TELEGRAM_NOTIFY_BOT_TOKEN,TELEGRAM_NOTIFY_CHAT_ID",
		Portability:      "portable",
		InstallHint:      "Install telegram_notify_mcp and set TELEGRAM_NOTIFY_BOT_TOKEN; set or refresh TELEGRAM_NOTIFY_CHAT_ID before sending.",
		IsDefault:        boolPtr(false),
	},
	{
		Name:             "zai-mcp-server",
		ShortDescription: "Z.AI Vision MCP for screenshot/image inspection.",
		Category:         "Design",
		HowTo:            "Use for Droid/Netrunner visual inspection: analyze screenshots, compare UI shots, extract text from images, and diagnose visual parity issues.",
		AuthEnvKeys:      "Z_AI_API_KEY,Z_AI_MODE",
		Portability:      "portable",
		InstallHint:      "npx -y @z_ai/mcp-server; needs Z_AI_API_KEY and Z_AI_MODE=ZAI.",
	},
}

func applyMcpMarketplaceCatalog() error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	for _, name := range zeroUsageArchivedMcpServers {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		_, err := upsertMcpServerWithExecutor(
			tx,
			name,
			"",
			"",
			"",
			"",
			"",
			"",
			"Archived after the 2026-06 MCP usage audit; re-enable only for an explicit project need.",
			nil,
			boolPtr(false),
			boolPtr(true),
		)
		if err != nil {
			return err
		}
	}

	for _, spec := range liveMcpMarketplaceCatalog {
		if strings.TrimSpace(spec.Name) == "" {
			continue
		}
		_, err := upsertMcpServerWithExecutor(
			tx,
			spec.Name,
			spec.ShortDescription,
			spec.LongDescription,
			spec.Category,
			spec.HowTo,
			spec.AuthEnvKeys,
			spec.Portability,
			spec.InstallHint,
			nil,
			spec.IsDefault,
			boolPtr(spec.Archived),
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func upsertRolePreprompt(roleName, promptText string) error {
	roleName = strings.TrimSpace(roleName)
	if roleName == "" {
		return fmt.Errorf("role_name is required")
	}
	promptText = strings.TrimSpace(promptText)
	if promptText == "" {
		return fmt.Errorf("prompt_text is required")
	}

	_, err := db.Exec(
		`INSERT INTO role_preprompt (role_name, prompt_text, updated_at)
		 VALUES (?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(role_name) DO UPDATE SET
		   prompt_text = excluded.prompt_text,
		   updated_at = CURRENT_TIMESTAMP`,
		roleName,
		promptText,
	)
	return err
}

func seedRolePreprompts() error {
	for roleName, promptText := range defaultRolePreprompts {
		if err := upsertRolePreprompt(roleName, promptText); err != nil {
			return err
		}
	}
	return nil
}

func getRolePreprompt(roleName string) string {
	roleName = strings.TrimSpace(roleName)
	if roleName == "" {
		return ""
	}

	var promptText string
	err := db.QueryRow("SELECT prompt_text FROM role_preprompt WHERE role_name = ?", roleName).Scan(&promptText)
	if err != nil {
		if err != sql.ErrNoRows {
			log.Printf("get_role_preprompt failed for role=%s: %v", roleName, err)
		}
		return ""
	}
	return promptText
}
