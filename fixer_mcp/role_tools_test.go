package main

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestBootstrapDefaultRoleAuthFromEnvForNetrunner(t *testing.T) {
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

	bootstrapDefaultRoleAuthFromEnv()

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

func TestBootstrapDefaultRoleAuthFromEnvForFixerGate(t *testing.T) {
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

	t.Setenv(fixerMcpDefaultRoleEnv, "fixer")
	t.Setenv(fixerMcpDefaultCwdEnv, testProjectCWD)
	t.Setenv(fixerMcpLockedRoleEnv, "fixer")
	t.Setenv(fixerMcpAutoAuthEnv, "1")
	t.Setenv(fixerMcpToolProfileEnv, netrunnerGateProfile)
	db = testDB
	authorizedRole = ""
	authorizedProjectId = 0
	authorizedSessionId = 999

	bootstrapDefaultRoleAuthFromEnv()

	if authorizedRole != "fixer" || authorizedProjectId != 1 || authorizedSessionId != 0 {
		t.Fatalf("unexpected fixer gate auth state: role=%q project=%d session=%d", authorizedRole, authorizedProjectId, authorizedSessionId)
	}
}

func TestBootstrapDefaultRoleAuthRejectsOrdinaryFixerServer(t *testing.T) {
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	originalSessionID := authorizedSessionId
	defer func() {
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
		authorizedSessionId = originalSessionID
	}()

	t.Setenv(fixerMcpDefaultRoleEnv, "fixer")
	t.Setenv(fixerMcpDefaultCwdEnv, testProjectCWD)
	t.Setenv(fixerMcpLockedRoleEnv, "fixer")
	t.Setenv(fixerMcpAutoAuthEnv, "1")
	t.Setenv(fixerMcpToolProfileEnv, "")
	authorizedRole = ""
	authorizedProjectId = 0
	authorizedSessionId = 999

	bootstrapDefaultRoleAuthFromEnv()

	if authorizedRole != "" || authorizedProjectId != 0 || authorizedSessionId != 999 {
		t.Fatalf("ordinary fixer server must not auto-auth: role=%q project=%d session=%d", authorizedRole, authorizedProjectId, authorizedSessionId)
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
		Role: "fixer",
		Cwd:  testProjectCWD,
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

func TestAssumeRoleUnlockedOverseerAndFixerTokenlessAuth(t *testing.T) {
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

	t.Setenv(fixerMcpLockedRoleEnv, "")
	db = testDB
	authorizedRole = ""
	authorizedProjectId = 0
	authorizedSessionId = 99

	// Unlocked overseer
	callResult, out, err := AssumeRole(context.Background(), nil, AssumeRoleInput{
		Role: "overseer",
	})
	if err != nil {
		t.Fatalf("expected unlocked overseer auth success, got: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil call result on success, got: %+v", callResult)
	}
	if out.Status != "success" || authorizedRole != "overseer" || authorizedProjectId != 0 || authorizedSessionId != 0 {
		t.Fatalf("unexpected state after unlocked overseer auth: out=%+v role=%q project=%d session=%d", out, authorizedRole, authorizedProjectId, authorizedSessionId)
	}

	// Unlocked fixer
	callResult, out, err = AssumeRole(context.Background(), nil, AssumeRoleInput{
		Role: "fixer",
		Cwd:  testProjectCWD,
	})
	if err != nil {
		t.Fatalf("expected unlocked fixer auth success, got: %v", err)
	}
	if callResult != nil {
		t.Fatalf("expected nil call result on success, got: %+v", callResult)
	}
	if out.Status != "success" || authorizedRole != "fixer" || authorizedProjectId != 1 || authorizedSessionId != 0 {
		t.Fatalf("unexpected state after unlocked fixer auth: out=%+v role=%q project=%d session=%d", out, authorizedRole, authorizedProjectId, authorizedSessionId)
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
			present:    []string{"assume_role", "get_projects", "launch_and_wait_fixers", "append_overseer_fixer_message", "get_project_balance", "credit_project_balance", "set_fixer_spend_authority", "get_balance_ledger"},
			absent:     []string{"create_task", "checkout_task", "log_netrunner_progress", "view_netrunner_logs", "create_netrunner_wave", "get_netrunner_wave", "launch_netrunner_wave", "wait_for_netrunner_wave", "launch_netrunner_waves", "wait_for_netrunner_waves", "transition_netrunner_wave_phase", "set_netrunner_wave_control_state", "get_mcp_binary_restart_state", "set_mcp_binary_restart_state", "cleanup_netrunner_wave", "sync_mcp_servers", "clear_project_handoff", "wake_fixer_autonomous", "record_fixer_spend", "export_project_doc_bundle"},
		},
		{
			name:       "fixer",
			lockedRole: "fixer",
			present:    []string{"assume_role", "create_task", "sync_mcp_servers", "view_netrunner_logs", "create_netrunner_wave", "get_netrunner_wave", "launch_netrunner_wave", "wait_for_netrunner_wave", "launch_netrunner_waves", "wait_for_netrunner_waves", "transition_netrunner_wave_phase", "set_netrunner_wave_control_state", "get_mcp_binary_restart_state", "set_mcp_binary_restart_state", "cleanup_netrunner_wave", "review_doc_proposals", "get_project_balance", "record_fixer_spend", "get_balance_ledger", "export_project_doc_bundle"},
			absent:     []string{"get_projects", "checkout_task", "log_netrunner_progress", "complete_task", "clear_project_handoff", "wake_fixer_autonomous", "credit_project_balance", "set_fixer_spend_authority"},
		},
		{
			name:       "netrunner",
			lockedRole: "netrunner",
			present:    []string{"assume_role", "checkout_task", "log_netrunner_progress", "complete_task", "wake_fixer_autonomous"},
			absent:     []string{"get_projects", "create_task", "view_netrunner_logs", "review_doc_proposals", "create_netrunner_wave", "get_netrunner_wave", "launch_netrunner_wave", "wait_for_netrunner_wave", "launch_netrunner_waves", "wait_for_netrunner_waves", "transition_netrunner_wave_phase", "set_netrunner_wave_control_state", "get_mcp_binary_restart_state", "set_mcp_binary_restart_state", "cleanup_netrunner_wave", "sync_mcp_servers", "clear_project_handoff", "get_project_balance", "credit_project_balance", "set_fixer_spend_authority", "record_fixer_spend", "get_balance_ledger", "export_project_doc_bundle"},
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

func TestNetrunnerGateToolSurfaceIsWaitOnly(t *testing.T) {
	expected := []string{
		waitForNetrunnerWaveToolName,
		waitForNetrunnerWavesToolName,
	}
	if len(netrunnerGateToolNames) != len(expected) {
		t.Fatalf("unexpected direct Netrunner gate surface: %#v", netrunnerGateToolNames)
	}
	for index, name := range expected {
		if netrunnerGateToolNames[index] != name {
			t.Fatalf("unexpected direct Netrunner gate surface: %#v", netrunnerGateToolNames)
		}
	}
	for _, name := range []string{launchNetrunnerWaveToolName, launchNetrunnerWavesToolName} {
		for _, gateName := range netrunnerGateToolNames {
			if gateName == name {
				t.Fatalf("launch tool %s must not be on the direct Netrunner gate surface: %#v", name, netrunnerGateToolNames)
			}
		}
	}
}

func TestLockedRoleFeedbackSurface(t *testing.T) {
	cases := []struct {
		lockedRole string
		present    bool
	}{
		{lockedRole: "overseer", present: true},
		{lockedRole: "fixer", present: true},
		{lockedRole: "netrunner", present: false},
	}

	for _, tc := range cases {
		t.Run(tc.lockedRole, func(t *testing.T) {
			toolSet := make(map[string]struct{})
			for _, name := range registeredToolNamesForMode(tc.lockedRole) {
				toolSet[name] = struct{}{}
			}
			for _, name := range []string{"submit_fixer_mcp_feedback", "list_fixer_mcp_feedback"} {
				_, ok := toolSet[name]
				if ok != tc.present {
					if tc.present {
						t.Fatalf("expected %s in %s surface", name, tc.lockedRole)
					} else {
						t.Fatalf("did not expect %s in %s surface", name, tc.lockedRole)
					}
				}
			}
		})
	}
}

func TestLockedFixerSurfaceKeepsLaunchToolsOnMainServer(t *testing.T) {
	toolSet := make(map[string]struct{})
	for _, name := range registeredToolNamesForMode("fixer") {
		toolSet[name] = struct{}{}
	}
	for _, name := range []string{
		launchNetrunnerWaveToolName,
		launchNetrunnerWavesToolName,
		waitForNetrunnerWaveToolName,
		waitForNetrunnerWavesToolName,
	} {
		if _, ok := toolSet[name]; !ok {
			t.Fatalf("expected %s on the locked fixer fixer_mcp surface", name)
		}
	}
}

func TestLockedFixerSurfaceExposesProjectMcpConfigSync(t *testing.T) {
	toolSet := make(map[string]struct{})
	for _, name := range registeredToolNamesForMode("fixer") {
		toolSet[name] = struct{}{}
	}
	if _, ok := toolSet["sync_mcp_servers"]; !ok {
		t.Fatal("expected sync_mcp_servers on the locked fixer surface")
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
		"create_netrunner_wave",
		"get_netrunner_wave",
		"launch_netrunner_wave",
		"wait_for_netrunner_wave",
		"launch_netrunner_waves",
		"wait_for_netrunner_waves",
		"transition_netrunner_wave_phase",
		"set_netrunner_wave_control_state",
		"get_mcp_binary_restart_state",
		"set_mcp_binary_restart_state",
		"cleanup_netrunner_wave",
		"wake_fixer_autonomous",
		"get_project_balance",
		"credit_project_balance",
		"set_fixer_spend_authority",
		"record_fixer_spend",
		"get_balance_ledger",
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
		"func bootstrapDefaultRoleAuthFromEnv(",
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

func setNetrunnerGateAuthEnv(t *testing.T) {
	t.Helper()
	t.Setenv(fixerMcpDefaultRoleEnv, "fixer")
	t.Setenv(fixerMcpDefaultCwdEnv, testProjectCWD)
	t.Setenv(fixerMcpLockedRoleEnv, "fixer")
	t.Setenv(fixerMcpAutoAuthEnv, "1")
	t.Setenv(fixerMcpToolProfileEnv, netrunnerGateProfile)
}

func TestEnsureNetrunnerGateProjectBindingLazyRebinds(t *testing.T) {
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

	setNetrunnerGateAuthEnv(t)
	db = testDB
	authorizedRole = ""
	authorizedProjectId = 0
	authorizedSessionId = 999

	if err := ensureNetrunnerGateProjectBinding(); err != nil {
		t.Fatalf("expected lazy gate binding to succeed, got: %v", err)
	}
	if authorizedRole != "fixer" || authorizedProjectId != 1 || authorizedSessionId != 0 {
		t.Fatalf("unexpected gate auth state after lazy bind: role=%q project=%d session=%d", authorizedRole, authorizedProjectId, authorizedSessionId)
	}
}

func TestEnsureNetrunnerGateProjectBindingRejectsUnregisteredCwd(t *testing.T) {
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

	setNetrunnerGateAuthEnv(t)
	t.Setenv(fixerMcpDefaultCwdEnv, "/tmp/not-registered-project")
	db = testDB
	authorizedRole = ""
	authorizedProjectId = 0
	authorizedSessionId = 999

	err := ensureNetrunnerGateProjectBinding()
	if err == nil || !strings.Contains(err.Error(), "requires project-bound fixer role") {
		t.Fatalf("expected project-bound denial for unregistered cwd, got: %v", err)
	}
	if authorizedRole != "" || authorizedProjectId != 0 || authorizedSessionId != 999 {
		t.Fatalf("auth state mutated on failed lazy bind: role=%q project=%d session=%d", authorizedRole, authorizedProjectId, authorizedSessionId)
	}
}

func TestEnsureNetrunnerGateProjectBindingDeniesOutsideGateProfile(t *testing.T) {
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	defer func() {
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
	}()

	t.Setenv(fixerMcpToolProfileEnv, "")
	authorizedRole = ""
	authorizedProjectId = 0

	err := ensureNetrunnerGateProjectBinding()
	if err == nil || !strings.Contains(err.Error(), "requires project-bound fixer role") {
		t.Fatalf("expected denial outside gate profile, got: %v", err)
	}
	if authorizedRole != "" || authorizedProjectId != 0 {
		t.Fatalf("auth state mutated outside gate profile: role=%q project=%d", authorizedRole, authorizedProjectId)
	}
}

func TestEnsureNetrunnerGateProjectBindingNoopWhenAlreadyBound(t *testing.T) {
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	defer func() {
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
	}()

	t.Setenv(fixerMcpToolProfileEnv, "")
	authorizedRole = "fixer"
	authorizedProjectId = 5

	if err := ensureNetrunnerGateProjectBinding(); err != nil {
		t.Fatalf("expected already-bound auth to be accepted, got: %v", err)
	}
	if authorizedRole != "fixer" || authorizedProjectId != 5 {
		t.Fatalf("auth state mutated when already bound: role=%q project=%d", authorizedRole, authorizedProjectId)
	}
}

func TestGateWaitForNetrunnerWavesRebindsBeforeDelegating(t *testing.T) {
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

	setNetrunnerGateAuthEnv(t)
	db = testDB
	authorizedRole = ""
	authorizedProjectId = 0
	authorizedSessionId = 999

	callResult, _, err := gateWaitForNetrunnerWaves(context.Background(), nil, WaitForNetrunnerWavesInput{})
	if err == nil {
		t.Fatal("expected delegated wait to fail validation after lazy bind, got nil error")
	}
	if !strings.Contains(err.Error(), "at least one wave ID") {
		t.Fatalf("expected delegated batch validation error, got: %v", err)
	}
	if callResult == nil || !callResult.IsError {
		t.Fatalf("expected MCP error result from delegated wait, got: %+v", callResult)
	}
	if authorizedRole != "fixer" || authorizedProjectId != 1 || authorizedSessionId != 0 {
		t.Fatalf("gate did not lazily bind before delegating: role=%q project=%d session=%d", authorizedRole, authorizedProjectId, authorizedSessionId)
	}
}

func TestGateWaitForNetrunnerWavesDeniesOutsideGateProfile(t *testing.T) {
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	defer func() {
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
	}()

	t.Setenv(fixerMcpToolProfileEnv, "")
	authorizedRole = ""
	authorizedProjectId = 0

	callResult, _, err := gateWaitForNetrunnerWaves(context.Background(), nil, WaitForNetrunnerWavesInput{})
	if err == nil || !strings.Contains(err.Error(), "requires project-bound fixer role") {
		t.Fatalf("expected gate denial outside gate profile, got: %v", err)
	}
	if callResult == nil || !callResult.IsError {
		t.Fatalf("expected MCP error result outside gate profile, got: %+v", callResult)
	}
	if authorizedRole != "" || authorizedProjectId != 0 {
		t.Fatalf("auth state mutated outside gate profile: role=%q project=%d", authorizedRole, authorizedProjectId)
	}
}
