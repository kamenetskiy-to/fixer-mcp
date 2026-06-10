package main

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

func TestSetAndGetSessionMcpServers_FixerAndNetrunner(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
	}()

	testDB := setupGetProjectsTestDB(t)
	defer func() {
		_ = testDB.Close()
	}()

	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1

	callResult, setOut, err := SetSessionMcpServers(context.Background(), nil, SetSessionMcpServersInput{
		SessionId:      1,
		McpServerNames: []string{"sqlite", "legacy_operator_bridge"},
	})
	if err != nil {
		t.Fatalf("set_session_mcp_servers failed: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil call result on success, got: %+v", callResult)
	}
	if len(setOut.McpServerNames) != 2 {
		t.Fatalf("expected 2 assigned mcp server names, got: %d", len(setOut.McpServerNames))
	}

	authorizedRole = "netrunner"
	getCallResult, getOut, getErr := GetSessionMcpServers(context.Background(), nil, GetSessionMcpServersInput{SessionId: 1})
	if getErr != nil {
		t.Fatalf("get_session_mcp_servers failed: %v", getErr)
	}
	if getCallResult != nil {
		t.Fatalf("expected nil call result on success, got: %+v", getCallResult)
	}
	if len(getOut.McpServerNames) != 2 {
		t.Fatalf("expected 2 assigned mcp server names, got: %d", len(getOut.McpServerNames))
	}
}

func TestSetSessionMcpServers_RejectsMcpOutsideProjectAllowlist(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
	}()

	testDB := setupGetProjectsTestDB(t)
	defer func() {
		_ = testDB.Close()
	}()

	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 2

	callResult, _, err := SetSessionMcpServers(context.Background(), nil, SetSessionMcpServersInput{
		SessionId:      1,
		McpServerNames: []string{"legacy_operator_bridge"},
	})
	if err == nil {
		t.Fatal("expected project allowlist rejection")
	}
	if callResult == nil || !callResult.IsError {
		t.Fatal("expected MCP error result")
	}
	if !strings.Contains(err.Error(), "not allowed for current project") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSetAndGetProjectMcpServers_GatesSessionAssignment(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
	}()

	testDB := setupGetProjectsTestDB(t)
	defer func() {
		_ = testDB.Close()
	}()

	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1

	setCallResult, setOut, setErr := SetProjectMcpServers(context.Background(), nil, SetProjectMcpServersInput{
		McpServerNames: []string{"sqlite"},
	})
	if setErr != nil {
		t.Fatalf("set_project_mcp_servers failed: %v", setErr)
	}
	if setCallResult != nil {
		t.Fatalf("expected nil call result on success, got: %+v", setCallResult)
	}
	if len(setOut.McpServerNames) != 1 || setOut.McpServerNames[0] != "sqlite" {
		t.Fatalf("unexpected set_project_mcp_servers output: %+v", setOut)
	}

	getCallResult, getOut, getErr := GetProjectMcpServers(context.Background(), nil, GetProjectMcpServersInput{})
	if getErr != nil {
		t.Fatalf("get_project_mcp_servers failed: %v", getErr)
	}
	if getCallResult != nil {
		t.Fatalf("expected nil call result on success, got: %+v", getCallResult)
	}
	if len(getOut.McpServerNames) != 1 || getOut.McpServerNames[0] != "sqlite" {
		t.Fatalf("unexpected project MCP allowlist: %+v", getOut.McpServerNames)
	}

	assignDeniedResult, _, assignDeniedErr := SetSessionMcpServers(context.Background(), nil, SetSessionMcpServersInput{
		SessionId:      1,
		McpServerNames: []string{"legacy_operator_bridge"},
	})
	if assignDeniedErr == nil {
		t.Fatal("expected session assignment rejection outside project allowlist")
	}
	if assignDeniedResult == nil || !assignDeniedResult.IsError {
		t.Fatal("expected MCP error result for rejected assignment")
	}

	assignCallResult, _, assignErr := SetSessionMcpServers(context.Background(), nil, SetSessionMcpServersInput{
		SessionId:      1,
		McpServerNames: []string{"sqlite"},
	})
	if assignErr != nil {
		t.Fatalf("expected assignment for allowlisted server, got: %v", assignErr)
	}
	if assignCallResult != nil {
		t.Fatalf("expected nil call result on allowed assignment, got: %+v", assignCallResult)
	}
}

func TestPruneDeprecatedProjectMcpBindings_RemovesTelegramNotify(t *testing.T) {
	originalDB := db
	defer func() {
		db = originalDB
	}()

	testDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() {
		_ = testDB.Close()
	}()

	if _, err := testDB.Exec(`
		CREATE TABLE mcp_server (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE
		);
		CREATE TABLE project_mcp_server (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL,
			mcp_server_id INTEGER NOT NULL
		);
		INSERT INTO mcp_server (id, name) VALUES (1, 'telegram_notify'), (2, 'sqlite');
		INSERT INTO project_mcp_server (project_id, mcp_server_id) VALUES (2, 1), (2, 2);
	`); err != nil {
		t.Fatalf("seed prune db: %v", err)
	}

	db = testDB
	if err := pruneDeprecatedProjectMcpBindings(); err != nil {
		t.Fatalf("pruneDeprecatedProjectMcpBindings failed: %v", err)
	}

	var count int
	if err := testDB.QueryRow("SELECT COUNT(*) FROM project_mcp_server WHERE mcp_server_id = 1").Scan(&count); err != nil {
		t.Fatalf("count telegram bindings: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected telegram_notify bindings to be removed, got %d", count)
	}
	if err := testDB.QueryRow("SELECT COUNT(*) FROM project_mcp_server WHERE mcp_server_id = 2").Scan(&count); err != nil {
		t.Fatalf("count sqlite bindings: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected non-deprecated bindings to remain, got %d", count)
	}
}

func TestResearchQueryMcp_ProjectScopedAssignmentMatrix(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
	}()

	testDB := setupGetProjectsTestDB(t)
	defer func() {
		_ = testDB.Close()
	}()

	db = testDB
	if _, err := db.Exec(
		"INSERT INTO mcp_server (name, short_description, long_description, auto_attach, is_default, category, how_to) VALUES ('research_query_mcp', 'Research query', '', 0, 0, 'Web-search', 'Project-scoped research MCP')",
	); err != nil {
		t.Fatalf("seed research_query_mcp failed: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO project_mcp_server (project_id, mcp_server_id)
		 SELECT 1, id FROM mcp_server WHERE name = 'research_query_mcp'`,
	); err != nil {
		t.Fatalf("bind research_query_mcp to project A failed: %v", err)
	}

	// Project A (id=1): research_query_mcp assignable and visible in session assignment output.
	authorizedRole = "fixer"
	authorizedProjectId = 1
	assignAResult, _, assignAErr := SetSessionMcpServers(context.Background(), nil, SetSessionMcpServersInput{
		SessionId:      1,
		McpServerNames: []string{"research_query_mcp"},
	})
	if assignAErr != nil {
		t.Fatalf("expected project A assignment success, got: %v", assignAErr)
	}
	if assignAResult != nil {
		t.Fatalf("expected nil call result on project A assignment, got: %+v", assignAResult)
	}

	getAResult, getAOut, getAErr := GetSessionMcpServers(context.Background(), nil, GetSessionMcpServersInput{SessionId: 1})
	if getAErr != nil {
		t.Fatalf("expected project A get_session_mcp_servers success, got: %v", getAErr)
	}
	if getAResult != nil {
		t.Fatalf("expected nil call result on project A readback, got: %+v", getAResult)
	}
	if len(getAOut.McpServerNames) != 1 || getAOut.McpServerNames[0] != "research_query_mcp" {
		t.Fatalf("expected project A to expose research_query_mcp assignment, got: %+v", getAOut.McpServerNames)
	}

	// Project B (id=2): research_query_mcp not assignable.
	authorizedProjectId = 2
	assignBDeniedResult, _, assignBDeniedErr := SetSessionMcpServers(context.Background(), nil, SetSessionMcpServersInput{
		SessionId:      1,
		McpServerNames: []string{"research_query_mcp"},
	})
	if assignBDeniedErr == nil {
		t.Fatal("expected project B assignment rejection for research_query_mcp")
	}
	if assignBDeniedResult == nil || !assignBDeniedResult.IsError {
		t.Fatal("expected MCP error result for project B assignment rejection")
	}
	if !strings.Contains(assignBDeniedErr.Error(), "not allowed for current project") {
		t.Fatalf("unexpected project B rejection error: %v", assignBDeniedErr)
	}
}

func TestSetSessionMcpServers_DeniesNetrunner(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
	}()

	testDB := setupGetProjectsTestDB(t)
	defer func() {
		_ = testDB.Close()
	}()

	db = testDB
	authorizedRole = "netrunner"
	authorizedProjectId = 1

	callResult, _, err := SetSessionMcpServers(context.Background(), nil, SetSessionMcpServersInput{
		SessionId:      1,
		McpServerNames: []string{"sqlite"},
	})
	if err == nil {
		t.Fatal("expected access denied error for netrunner")
	}
	if callResult == nil || !callResult.IsError {
		t.Fatal("expected MCP error result for netrunner")
	}
	if !strings.Contains(err.Error(), "access denied: requires fixer role") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSyncAndListMcpServers_FixerFlow(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
	}()

	testDB := setupGetProjectsTestDB(t)
	defer func() {
		_ = testDB.Close()
	}()

	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1

	callResult, syncOut, err := SyncMcpServers(context.Background(), nil, SyncMcpServersInput{
		Servers: []McpServerUpsertInput{
			{Name: "new_registry_server", Category: "Coding", HowTo: "Use for coding tasks", IsDefault: boolPtr(true)},
			{Name: "catalog_server", AuthEnvKeys: "CATALOG_TOKEN", Portability: "portable", InstallHint: "npx -y catalog-server"},
		},
	})
	if err != nil {
		t.Fatalf("sync_mcp_servers failed: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil call result on success, got: %+v", callResult)
	}
	if syncOut.Total != 2 {
		t.Fatalf("expected total=1, got %+v", syncOut)
	}

	listCallResult, listOut, listErr := ListMcpServers(context.Background(), nil, ListMcpServersInput{IncludeAll: true})
	if listErr != nil {
		t.Fatalf("list_mcp_servers failed: %v", listErr)
	}
	if listCallResult != nil {
		t.Fatalf("expected nil call result on success, got: %+v", listCallResult)
	}

	found := false
	foundCatalog := false
	for _, server := range listOut.Servers {
		if server.Name == "new_registry_server" {
			found = true
			if server.Category != "Coding" {
				t.Fatalf("expected category Coding, got %+v", server)
			}
			if server.HowTo != "Use for coding tasks" {
				t.Fatalf("expected how_to persisted, got %+v", server)
			}
			if !server.IsDefault {
				t.Fatalf("expected is_default true, got %+v", server)
			}
		}
		if server.Name == "catalog_server" {
			foundCatalog = true
			if server.AuthEnvKeys != "CATALOG_TOKEN" || server.Portability != "portable" || server.InstallHint != "npx -y catalog-server" {
				t.Fatalf("expected catalog fields persisted, got %+v", server)
			}
		}
	}
	if !found {
		t.Fatalf("expected new_registry_server in list output, got %+v", listOut.Servers)
	}
	if !foundCatalog {
		t.Fatalf("expected catalog_server in list output, got %+v", listOut.Servers)
	}
}

func TestSyncMcpServers_EnrichesCuratedDefaults(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
	}()

	testDB := setupGetProjectsTestDB(t)
	defer func() {
		_ = testDB.Close()
	}()

	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1

	_, syncOut, err := SyncMcpServers(context.Background(), nil, SyncMcpServersInput{
		Servers: []McpServerUpsertInput{
			{Name: "figma-console-mcp"},
			{Name: "playwright"},
			{Name: "chrome-devtools"},
			{Name: "eslint"},
			{Name: "mcp-language-server"},
			{Name: "apify"},
			{Name: "zai-mcp-server"},
		},
	})
	if err != nil {
		t.Fatalf("sync_mcp_servers failed: %v", err)
	}
	if syncOut.Total != 7 {
		t.Fatalf("expected total=7, got %+v", syncOut)
	}

	_, listOut, listErr := ListMcpServers(context.Background(), nil, ListMcpServersInput{IncludeAll: true})
	if listErr != nil {
		t.Fatalf("list_mcp_servers failed: %v", listErr)
	}

	expectedNames := map[string]string{
		"figma-console-mcp":   "Design",
		"playwright":          "Coding",
		"chrome-devtools":     "Coding",
		"eslint":              "Coding",
		"mcp-language-server": "Coding",
		"apify":               "Web-search",
		"zai-mcp-server":      "Design",
	}
	for _, server := range listOut.Servers {
		expectedCategory, ok := expectedNames[server.Name]
		if !ok {
			continue
		}
		if server.Category != expectedCategory {
			t.Fatalf("expected %s category for %q, got %+v", expectedCategory, server.Name, server)
		}
		if strings.TrimSpace(server.HowTo) == "" {
			t.Fatalf("expected non-empty how_to for %q, got %+v", server.Name, server)
		}
		if !server.IsDefault {
			t.Fatalf("expected %q to be default, got %+v", server.Name, server)
		}
		delete(expectedNames, server.Name)
	}
	if len(expectedNames) > 0 {
		t.Fatalf("missing expected curated MCP servers in registry output: %+v", expectedNames)
	}
}

func TestListMcpServers_DeterministicCategoryOrdering(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
	}()

	testDB := setupGetProjectsTestDB(t)
	defer func() {
		_ = testDB.Close()
	}()

	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1

	_, _, err := SyncMcpServers(context.Background(), nil, SyncMcpServersInput{
		Servers: []McpServerUpsertInput{
			{Name: "figma-console-mcp"},
			{Name: "playwright"},
			{Name: "mcp-language-server"},
			{Name: "chrome-devtools"},
			{Name: "eslint"},
		},
	})
	if err != nil {
		t.Fatalf("sync_mcp_servers failed: %v", err)
	}

	_, out, err := ListMcpServers(context.Background(), nil, ListMcpServersInput{IncludeAll: true})
	if err != nil {
		t.Fatalf("list_mcp_servers include_all failed: %v", err)
	}

	names := make([]string, 0, len(out.Servers))
	for _, item := range out.Servers {
		names = append(names, item.Name)
	}

	expected := []string{"sqlite", "figma-console-mcp", "legacy_operator_bridge", "chrome-devtools", "eslint", "mcp-language-server", "playwright"}
	if len(names) != len(expected) {
		t.Fatalf("expected %d servers, got %d (%v)", len(expected), len(names), names)
	}
	for idx := range expected {
		if names[idx] != expected[idx] {
			t.Fatalf("unexpected ordering: expected %v, got %v", expected, names)
		}
	}
}

func TestListMcpServers_DefaultsOnlyByDefault(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
	}()

	testDB := setupGetProjectsTestDB(t)
	defer func() {
		_ = testDB.Close()
	}()

	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1

	callResult, out, err := ListMcpServers(context.Background(), nil, ListMcpServersInput{})
	if err != nil {
		t.Fatalf("list_mcp_servers defaults-only failed: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil call result on success, got: %+v", callResult)
	}
	if len(out.Servers) != 1 {
		t.Fatalf("expected 1 default server, got %d", len(out.Servers))
	}
	if out.Servers[0].Name != "sqlite" {
		t.Fatalf("expected sqlite default server, got %+v", out.Servers)
	}
	if !out.Servers[0].IsDefault {
		t.Fatalf("expected is_default=true, got %+v", out.Servers[0])
	}
}

func TestListMcpServers_IncludeAllOverridesDefaultFilter(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
	}()

	testDB := setupGetProjectsTestDB(t)
	defer func() {
		_ = testDB.Close()
	}()

	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1

	callResult, out, err := ListMcpServers(context.Background(), nil, ListMcpServersInput{IncludeAll: true})
	if err != nil {
		t.Fatalf("list_mcp_servers include_all failed: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil call result on success, got: %+v", callResult)
	}
	if len(out.Servers) != 2 {
		t.Fatalf("expected 2 servers with include_all, got %d", len(out.Servers))
	}
}

func TestListMcpServers_ArchivedFiltering(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
	}()

	testDB := setupGetProjectsTestDB(t)
	defer func() {
		_ = testDB.Close()
	}()

	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1

	_, _, err := SyncMcpServers(context.Background(), nil, SyncMcpServersInput{
		Servers: []McpServerUpsertInput{
			{Name: "archived_default", Category: "Coding", HowTo: "Archived default", IsDefault: boolPtr(true), Archived: boolPtr(true)},
			{Name: "archived_non_default", Category: "Coding", HowTo: "Archived non-default", Archived: boolPtr(true)},
			{Name: "active_non_default", Category: "Coding", HowTo: "Active non-default"},
		},
	})
	if err != nil {
		t.Fatalf("sync_mcp_servers failed: %v", err)
	}

	_, defaultOut, err := ListMcpServers(context.Background(), nil, ListMcpServersInput{})
	if err != nil {
		t.Fatalf("list_mcp_servers default failed: %v", err)
	}
	for _, server := range defaultOut.Servers {
		if server.Archived {
			t.Fatalf("default list should exclude archived servers, got %+v", defaultOut.Servers)
		}
	}

	_, includeAllOut, err := ListMcpServers(context.Background(), nil, ListMcpServersInput{IncludeAll: true})
	if err != nil {
		t.Fatalf("list_mcp_servers include_all failed: %v", err)
	}
	for _, server := range includeAllOut.Servers {
		if server.Archived {
			t.Fatalf("include_all without include_archived should exclude archived servers, got %+v", includeAllOut.Servers)
		}
	}

	_, archivedOut, err := ListMcpServers(context.Background(), nil, ListMcpServersInput{IncludeAll: true, IncludeArchived: true})
	if err != nil {
		t.Fatalf("list_mcp_servers include_archived failed: %v", err)
	}
	foundArchived := map[string]bool{}
	for _, server := range archivedOut.Servers {
		if server.Archived {
			foundArchived[server.Name] = true
		}
	}
	if !foundArchived["archived_default"] || !foundArchived["archived_non_default"] {
		t.Fatalf("expected archived servers only with include_archived, got %+v", archivedOut.Servers)
	}
}

func TestSetSessionMcpServers_AllowsExplicitArchivedAssignment(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
	}()

	testDB := setupGetProjectsTestDB(t)
	defer func() {
		_ = testDB.Close()
	}()

	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1

	if _, err := db.Exec(
		"INSERT INTO mcp_server (name, short_description, archived) VALUES ('archived_tool', 'Archived tool', 1)",
	); err != nil {
		t.Fatalf("seed archived tool failed: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO project_mcp_server (project_id, mcp_server_id)
		 SELECT 1, id FROM mcp_server WHERE name = 'archived_tool'`,
	); err != nil {
		t.Fatalf("allow archived tool failed: %v", err)
	}

	_, _, err := SetSessionMcpServers(context.Background(), nil, SetSessionMcpServersInput{
		SessionId:      1,
		McpServerNames: []string{"archived_tool"},
	})
	if err != nil {
		t.Fatalf("expected archived server assignment by explicit name to work, got %v", err)
	}

	_, out, err := GetSessionMcpServers(context.Background(), nil, GetSessionMcpServersInput{SessionId: 1})
	if err != nil {
		t.Fatalf("get_session_mcp_servers failed: %v", err)
	}
	if len(out.Servers) != 1 || out.Servers[0].Name != "archived_tool" || !out.Servers[0].Archived {
		t.Fatalf("expected archived assignment to round-trip, got %+v", out.Servers)
	}
}
