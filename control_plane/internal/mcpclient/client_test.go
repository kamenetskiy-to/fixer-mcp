package mcpclient

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestInspectReadsProjectWorkThroughMCP(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "fake-mcp")
	script := `#!/bin/sh
while IFS= read -r line; do
  case "$line" in
    *'"id":1'*) echo '{"jsonrpc":"2.0","id":1,"result":{"serverInfo":{"name":"fixer_mcp"}}}' ;;
    *'"id":2'*) echo '{"jsonrpc":"2.0","id":2,"result":{"structuredContent":{"status":"success"}}}' ;;
    *'"id":3'*) echo '{"jsonrpc":"2.0","id":3,"result":{"structuredContent":{"project_id":42,"has_overview":true,"overview":{"content":"Canonical project context"}}}}' ;;
    *'"id":4'*) echo '{"jsonrpc":"2.0","id":4,"result":{"structuredContent":{"sessions":[{"id":7,"status":"review","task_summary":"Review the release","cli_backend":"pi","cli_model":"gpt-5.6-terra","cli_reasoning":"high","session_kind":"netrunner"}]}}}' ;;
    *'"id":5'*) echo '{"jsonrpc":"2.0","id":5,"result":{"structuredContent":{"derived_state":"running","queued_count":2,"default_lane":"codex","lanes":[{"provider":"codex","model":"gpt-5.6-luna","reasoning":"high"},{"provider":"commandcode","model":"commandcode/zai-org/glm-5.3-flash","reasoning":"medium"}],"active_instruction":{"instruction_text":"Ship the console","state":"running","requested_lane":"pi"}}}}' ;;
  esac
done
`
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(dir, "fixer.db")
	if err := os.WriteFile(db, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FIXER_MCP_BINARY", binary)
	t.Setenv("FIXER_DB_PATH", db)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result := Inspect(ctx, Options{RuntimeRoot: dir, ProjectPath: "/project", Environment: os.Environ()})
	if result.Error != "" {
		t.Fatalf("Inspect error: %s", result.Error)
	}
	if !result.Available || result.ProjectID != 42 || result.Overview != "Canonical project context" {
		t.Fatalf("unexpected project snapshot: %#v", result)
	}
	if len(result.Sessions) != 1 || result.Sessions[0].ID != 7 || result.Sessions[0].Backend != "pi" {
		t.Fatalf("sessions were not read from MCP: %#v", result.Sessions)
	}
	if !result.Hands.Available || result.Hands.State != "running" || result.Hands.QueuedCount != 2 || result.Hands.ActiveText != "Ship the console" {
		t.Fatalf("Hands state was not read from MCP: %#v", result.Hands)
	}
	if result.Hands.DefaultLane != "codex" {
		t.Fatalf("default lane = %q, want codex", result.Hands.DefaultLane)
	}
	if len(result.Hands.Lanes) != 2 || result.Hands.Lanes[0].Provider != "codex" {
		t.Fatalf("registered lanes were not read from MCP: %#v", result.Hands.Lanes)
	}
}

func TestDatabasePathPrefersProjectMCPState(t *testing.T) {
	dir := t.TempDir()
	project := filepath.Join(dir, "project")
	projectDB := filepath.Join(project, "fixer_mcp", "fixer.db")
	if err := os.MkdirAll(filepath.Dir(projectDB), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(projectDB, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(dir, "state")
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "fixer.db"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := DatabasePath(Options{RuntimeRoot: filepath.Join(dir, "runtime"), ProjectPath: project, Environment: []string{"FIXER_STATE_DIR=" + state}})
	if err != nil {
		t.Fatal(err)
	}
	if got != projectDB {
		t.Fatalf("DatabasePath = %q, want project state %q", got, projectDB)
	}
}

// A fresh host has no database anywhere. Refusing to name one left the console
// permanently context-less, while `fixer doctor` promises initialization on the
// first launch: hand back the canonical state path so MCP can bootstrap it.
func TestDBPathBootstrapsTheCanonicalStateOnAFreshMachine(t *testing.T) {
	home := t.TempDir()
	stateHome := filepath.Join(home, "state")
	got, err := dbPath(
		filepath.Join(home, "release"),
		filepath.Join(home, "project"),
		[]string{"HOME=" + home, "XDG_STATE_HOME=" + stateHome},
	)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(stateHome, "fixer-client-wires", "fixer.db")
	if got != want {
		t.Fatalf("dbPath = %q, want %q", got, want)
	}
	info, err := os.Stat(filepath.Dir(got))
	if err != nil {
		t.Fatalf("state directory was not created: %v", err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("state directory permissions = %o, want 700", info.Mode().Perm())
	}
}

// A stray bare `<project>/fixer.db` used to be a silent candidate. On the host
// that reproduced the incident it shadowed the canonical state and the console
// showed an empty project world (project_id 1). It must never win again.
func TestDBPathIgnoresBareStraysWhenCanonicalStateExists(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	cwd := t.TempDir()
	stateHome := filepath.Join(home, "state")
	canonical := filepath.Join(stateHome, "fixer-client-wires", "fixer.db")
	if err := os.MkdirAll(filepath.Dir(canonical), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(canonical, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, stray := range []string{filepath.Join(project, "fixer.db"), filepath.Join(cwd, "fixer.db")} {
		if err := os.WriteFile(stray, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	got, err := dbPath(filepath.Join(home, "release"), project, []string{
		"HOME=" + home,
		"XDG_STATE_HOME=" + stateHome,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Clean(canonical) {
		t.Fatalf("dbPath = %q, want the canonical state %q", got, canonical)
	}

	strays := IgnoredStrayDBs(Options{
		RuntimeRoot: filepath.Join(home, "release"),
		ProjectPath: project,
		CWD:         cwd,
		Environment: []string{"HOME=" + home, "XDG_STATE_HOME=" + stateHome},
	})
	if len(strays) != 2 {
		t.Fatalf("IgnoredStrayDBs = %#v, want both bare strays", strays)
	}
	for _, want := range []string{filepath.Join(project, "fixer.db"), filepath.Join(cwd, "fixer.db")} {
		if !contains(strays, want) {
			t.Fatalf("IgnoredStrayDBs = %#v, missing %q", strays, want)
		}
	}
}

// The explicit override wins before any candidate is inspected, even when the
// others exist and even when the override file does not exist yet (the Go
// server bootstraps it).
func TestDatabasePathExplicitOverrideWins(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	projectDB := filepath.Join(project, "fixer_mcp", "fixer.db")
	if err := os.MkdirAll(filepath.Dir(projectDB), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(projectDB, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	override := filepath.Join(home, "custom", "chosen.db")
	got, err := DatabasePath(Options{
		ProjectPath: project,
		Environment: []string{"HOME=" + home, "FIXER_DB_PATH=" + override},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != override {
		t.Fatalf("DatabasePath = %q, want the explicit override %q", got, override)
	}
}

// An existing state database must beat the preferred-state bootstrap even when
// FIXER_STATE_DIR is configured: rule 3 checks existence first, then priority.
func TestDBPathPrefersExistingStateOverBootstrap(t *testing.T) {
	home := t.TempDir()
	stateDir := filepath.Join(home, "explicit-state")
	stateHome := filepath.Join(home, "xdg")
	xdgDB := filepath.Join(stateHome, "fixer-client-wires", "fixer.db")
	if err := os.MkdirAll(filepath.Dir(xdgDB), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(xdgDB, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := dbPath("/no-runtime", filepath.Join(home, "project"), []string{
		"HOME=" + home,
		"FIXER_STATE_DIR=" + stateDir,
		"XDG_STATE_HOME=" + stateHome,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Clean(xdgDB) {
		t.Fatalf("dbPath = %q, want the existing XDG state %q", got, xdgDB)
	}
}

// Regression for the managed-payload candidate: a database inside the release
// tree is deleted by every update, so it must never be selected even when it
// exists. The preferred state path wins instead.
func TestDBPathIgnoresManagedPayloadDatabase(t *testing.T) {
	home := t.TempDir()
	runtime := filepath.Join(home, "release")
	managed := filepath.Join(runtime, "fixer_mcp", "fixer.db")
	if err := os.MkdirAll(filepath.Dir(managed), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(managed, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := dbPath(runtime, filepath.Join(home, "project"), []string{"HOME=" + home})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".local", "state", "fixer-client-wires", "fixer.db")
	if got != want {
		t.Fatalf("dbPath = %q, want the state path %q (managed payload must be ignored)", got, want)
	}
}

// The explicit override is authoritative and must not be treated as a stray.
func TestIgnoredStrayDBsDoesNotReportExplicitOverride(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	stray := filepath.Join(project, "fixer.db")
	if err := os.WriteFile(stray, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	strays := IgnoredStrayDBs(Options{
		ProjectPath: project,
		Environment: []string{"HOME=" + home, "FIXER_DB_PATH=" + stray},
	})
	if len(strays) != 0 {
		t.Fatalf("explicit FIXER_DB_PATH must not be reported as a stray: %#v", strays)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if filepath.Clean(value) == filepath.Clean(want) {
			return true
		}
	}
	return false
}

func TestDBPathPrefersManagedState(t *testing.T) {
	dir := t.TempDir()
	state := filepath.Join(dir, "state")
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	managed := filepath.Join(state, "fixer.db")
	if err := os.WriteFile(managed, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := dbPath("/no-runtime", "/no-project", []string{"FIXER_STATE_DIR=" + state})
	if err != nil {
		t.Fatal(err)
	}
	if got != managed {
		t.Fatalf("dbPath = %q, want %q", got, managed)
	}
}
