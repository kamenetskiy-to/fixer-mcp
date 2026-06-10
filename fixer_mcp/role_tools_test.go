package main

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestBootstrapDefaultNetrunnerAuthFromEnv(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	originalSessionID := authorizedSessionId
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
		authorizedSessionId = originalSessionID
	}()

	testDB := setupGetProjectsTestDB(t)
	defer func() {
		_ = testDB.Close()
	}()

	db = testDB
	authorizedRole = ""
	authorizedProjectId = 0
	authorizedSessionId = 999

	withEnv := map[string]string{
		fixerMcpDefaultRoleEnv: "netrunner",
		fixerMcpDefaultCwdEnv:  testProjectCWD,
	}
	for key, value := range withEnv {
		if err := os.Setenv(key, value); err != nil {
			t.Fatalf("setenv %s: %v", key, err)
		}
		defer os.Unsetenv(key)
	}

	bootstrapDefaultNetrunnerAuthFromEnv()

	if authorizedRole != "netrunner" {
		t.Fatalf("expected env bootstrap role netrunner, got %q", authorizedRole)
	}
	if authorizedProjectId != 1 {
		t.Fatalf("expected env bootstrap project_id=1, got %d", authorizedProjectId)
	}
	if authorizedSessionId != 0 {
		t.Fatalf("expected env bootstrap session reset to 0, got %d", authorizedSessionId)
	}
}

func TestAssumeRoleLockedRoleRejectsMismatchWithoutMutatingAuth(t *testing.T) {
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	originalSessionID := authorizedSessionId
	defer func() {
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
		authorizedSessionId = originalSessionID
	}()

	t.Setenv(fixerMcpLockedRoleEnv, "overseer")
	authorizedRole = "netrunner"
	authorizedProjectId = 1
	authorizedSessionId = 77

	callResult, out, err := AssumeRole(context.Background(), nil, AssumeRoleInput{
		Role:  "fixer",
		Cwd:   testProjectCWD,
		Token: "supersecret",
	})
	if err != nil {
		t.Fatalf("locked role mismatch should return MCP error output, not handler error: %v", err)
	}
	if callResult == nil || !callResult.IsError {
		t.Fatalf("expected MCP error result, got: %+v", callResult)
	}
	if out.Status != "error" {
		t.Fatalf("expected error status, got %+v", out)
	}
	if !strings.Contains(out.Message, fixerMcpLockedRoleEnv+"=overseer") {
		t.Fatalf("expected locked role diagnostic, got %q", out.Message)
	}
	if !strings.Contains(out.Message, `assume_role("fixer")`) {
		t.Fatalf("expected attempted role in diagnostic, got %q", out.Message)
	}
	if authorizedRole != "netrunner" || authorizedProjectId != 1 || authorizedSessionId != 77 {
		t.Fatalf("auth state mutated on locked-role mismatch: role=%q project=%d session=%d", authorizedRole, authorizedProjectId, authorizedSessionId)
	}
}

func TestAssumeRoleLockedRoleAllowsMatchingRole(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	originalSessionID := authorizedSessionId
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
		authorizedSessionId = originalSessionID
	}()

	testDB := setupGetProjectsTestDB(t)
	defer func() {
		_ = testDB.Close()
	}()

	t.Setenv(fixerMcpLockedRoleEnv, "netrunner")
	db = testDB
	authorizedRole = ""
	authorizedProjectId = 0
	authorizedSessionId = 99

	callResult, out, err := AssumeRole(context.Background(), nil, AssumeRoleInput{
		Role: "netrunner",
		Cwd:  testProjectCWD,
	})
	if err != nil {
		t.Fatalf("expected matching locked role auth success, got: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil call result on success, got: %+v", callResult)
	}
	if out.Status != "success" {
		t.Fatalf("expected success output, got %+v", out)
	}
	if authorizedRole != "netrunner" || authorizedProjectId != 1 || authorizedSessionId != 0 {
		t.Fatalf("unexpected auth state after matching locked role: role=%q project=%d session=%d", authorizedRole, authorizedProjectId, authorizedSessionId)
	}
}

func TestAssumeRoleLockedFixerAllowsTokenlessAuth(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	originalSessionID := authorizedSessionId
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
		authorizedSessionId = originalSessionID
	}()

	testDB := setupGetProjectsTestDB(t)
	defer func() {
		_ = testDB.Close()
	}()

	t.Setenv(fixerMcpLockedRoleEnv, "fixer")
	db = testDB
	authorizedRole = ""
	authorizedProjectId = 0
	authorizedSessionId = 99

	callResult, out, err := AssumeRole(context.Background(), nil, AssumeRoleInput{
		Role: "fixer",
		Cwd:  testProjectCWD,
	})
	if err != nil {
		t.Fatalf("expected locked fixer tokenless auth success, got: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil call result on success, got: %+v", callResult)
	}
	if out.Status != "success" {
		t.Fatalf("expected success output, got %+v", out)
	}
	if authorizedRole != "fixer" || authorizedProjectId != 1 || authorizedSessionId != 0 {
		t.Fatalf("unexpected auth state after locked fixer auth: role=%q project=%d session=%d", authorizedRole, authorizedProjectId, authorizedSessionId)
	}
}

func TestLockedRoleToolSurfacesHideForeignAndAdminTools(t *testing.T) {
	cases := []struct {
		name       string
		lockedRole string
		present    []string
		absent     []string
	}{
		{
			name:       "overseer",
			lockedRole: "overseer",
			present:    []string{"assume_role", "get_projects", "launch_and_wait_fixers", "append_overseer_fixer_message"},
			absent:     []string{"create_task", "checkout_task", "log_netrunner_progress", "view_netrunner_logs", "launch_and_wait_netrunner", "create_netrunner_wave", "get_netrunner_wave", "launch_netrunner_wave", "wait_for_netrunner_wave", "cleanup_netrunner_wave", "sync_mcp_servers", "clear_project_handoff", "wake_fixer_autonomous"},
		},
		{
			name:       "fixer",
			lockedRole: "fixer",
			present:    []string{"assume_role", "create_task", "view_netrunner_logs", "launch_and_wait_netrunner", "create_netrunner_wave", "get_netrunner_wave", "launch_netrunner_wave", "wait_for_netrunner_wave", "cleanup_netrunner_wave", "review_doc_proposals"},
			absent:     []string{"get_projects", "checkout_task", "log_netrunner_progress", "complete_task", "sync_mcp_servers", "clear_project_handoff", "wake_fixer_autonomous"},
		},
		{
			name:       "netrunner",
			lockedRole: "netrunner",
			present:    []string{"assume_role", "checkout_task", "log_netrunner_progress", "complete_task", "wake_fixer_autonomous"},
			absent:     []string{"get_projects", "create_task", "view_netrunner_logs", "review_doc_proposals", "launch_and_wait_netrunner", "create_netrunner_wave", "get_netrunner_wave", "launch_netrunner_wave", "wait_for_netrunner_wave", "cleanup_netrunner_wave", "sync_mcp_servers", "clear_project_handoff"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			toolSet := make(map[string]struct{})
			for _, name := range registeredToolNamesForMode(tc.lockedRole) {
				toolSet[name] = struct{}{}
			}
			for _, name := range tc.present {
				if _, ok := toolSet[name]; !ok {
					t.Fatalf("expected %s in %s surface", name, tc.lockedRole)
				}
			}
			for _, name := range tc.absent {
				if _, ok := toolSet[name]; ok {
					t.Fatalf("did not expect %s in %s surface", name, tc.lockedRole)
				}
			}
		})
	}
}

func TestUnlockedToolSurfaceKeepsLegacyBroadTools(t *testing.T) {
	toolSet := make(map[string]struct{})
	for _, name := range registeredToolNamesForMode("") {
		toolSet[name] = struct{}{}
	}

	for _, name := range []string{
		"assume_role",
		"get_projects",
		"create_task",
		"checkout_task",
		"complete_task",
		"launch_and_wait_netrunner",
		"create_netrunner_wave",
		"get_netrunner_wave",
		"launch_netrunner_wave",
		"wait_for_netrunner_wave",
		"cleanup_netrunner_wave",
		"wake_fixer_autonomous",
		"sync_mcp_servers",
		"clear_project_handoff",
	} {
		if _, ok := toolSet[name]; !ok {
			t.Fatalf("expected %s in unlocked legacy surface", name)
		}
	}
}

func TestRoleToolRegistryListsHaveNoDuplicates(t *testing.T) {
	cases := map[string][]string{
		"bootstrap":       bootstrapToolNames,
		"overseer":        overseerToolNames,
		"fixer":           fixerToolNames,
		"netrunner":       netrunnerToolNames,
		"adminBackcompat": adminBackcompatToolNames,
		"unlocked":        registeredToolNamesForMode(""),
	}

	for surface, names := range cases {
		t.Run(surface, func(t *testing.T) {
			seen := make(map[string]struct{}, len(names))
			for _, name := range names {
				if _, ok := seen[name]; ok {
					t.Fatalf("duplicate tool name %q in %s surface", name, surface)
				}
				seen[name] = struct{}{}
			}
		})
	}
}

func TestMcpToolRegistrationUsesSharedAddHelperOnly(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read fixer_mcp dir: %v", err)
	}

	var got int
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || strings.HasSuffix(name, "_test.go") || !strings.HasSuffix(name, ".go") {
			continue
		}
		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		got += strings.Count(string(source), "mcp.AddTool(")
	}
	if got != 1 {
		t.Fatalf("expected only addMcpTool to call mcp.AddTool, found %d direct calls", got)
	}
}

func TestAuthRoleHandlersLiveOutsideMain(t *testing.T) {
	mainSource, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	authRoleSource, err := os.ReadFile("auth_role_handlers.go")
	if err != nil {
		t.Fatalf("read auth_role_handlers.go: %v", err)
	}

	symbols := []string{
		"var validAssumableRoles",
		"func isValidAssumableRole(",
		"func lockedRoleFromEnv(",
		"var defaultRolePreprompts",
		"func bootstrapDefaultNetrunnerAuthFromEnv(",
		"type AssumeRoleInput",
		"type AssumeRoleOutput",
		"func AssumeRole(",
	}

	for _, symbol := range symbols {
		if strings.Contains(string(mainSource), symbol) {
			t.Fatalf("expected %q to be extracted out of main.go", symbol)
		}
		if !strings.Contains(string(authRoleSource), symbol) {
			t.Fatalf("expected %q in auth_role_handlers.go", symbol)
		}
	}
}
