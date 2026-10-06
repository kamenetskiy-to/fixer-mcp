package main

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Runtime identity/health: every connected runtime health call reports the
// ACTUAL caller process (immutable build hash, PID, start, release, source,
// schema era, absolute DB identity) separately from the stored
// required/confirmed bookkeeping identities. A stale stored identity (for
// example a Sep-18 build PID) is evidence about the past, never caller proof,
// and epoch 0 on an empty DB must not pretend that nothing is running.

func TestRuntimeHealthReportsCallerIdentityNotStoredSep18Process(t *testing.T) {
	originalDB, originalRole, originalProjectID := db, authorizedRole, authorizedProjectId
	originalBuildID, originalProcessIdentity := mcpRunningBuildID, mcpProcessIdentity
	defer func() {
		db, authorizedRole, authorizedProjectId = originalDB, originalRole, originalProjectID
		mcpRunningBuildID, mcpProcessIdentity = originalBuildID, originalProcessIdentity
	}()
	t.Setenv(fixerDBPathEnv, filepath.Join(t.TempDir(), "sep18-restart.db"))
	initDB()
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(`INSERT INTO project (name, cwd, active) VALUES ('Sep18', '/tmp/sep18', 1)`); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	var projectID int
	if err := db.QueryRow(`SELECT id FROM project WHERE cwd = '/tmp/sep18'`).Scan(&projectID); err != nil {
		t.Fatalf("read seeded project: %v", err)
	}
	// The stored row still describes a long-dead Sep-18 build process.
	if _, err := db.Exec(`
		INSERT INTO mcp_binary_state (
			project_id, running_build_epoch, required_build_epoch, restart_required,
			running_build_id, required_build_id, running_process_identity, required_by_process_identity,
			reason, updated_at
		) VALUES (?, 7, 0, 0, 'sha256:sep18', '', 'pid:999999:start:12345', '', 'sep18 evidence', CURRENT_TIMESTAMP)`, projectID); err != nil {
		t.Fatalf("seed stored binary state: %v", err)
	}
	authorizedRole, authorizedProjectId = "fixer", projectID

	_, out, err := GetMCPBinaryRestartState(context.Background(), nil, GetMCPBinaryRestartStateInput{})
	if err != nil {
		t.Fatalf("runtime health call: %v", err)
	}
	if out.State.RunningBuildId != "sha256:sep18" || out.State.RunningProcessIdentity != "pid:999999:start:12345" {
		t.Fatalf("stored evidence must be preserved separately: %+v", out.State)
	}
	if out.RuntimeIdentity.ProcessId != os.Getpid() {
		t.Fatalf("health call must report the caller PID %d, not the stored one: %+v", os.Getpid(), out.RuntimeIdentity)
	}
	if out.RuntimeIdentity.BuildId != mcpRunningBuildID {
		t.Fatalf("health call must report the caller build identity: %+v", out.RuntimeIdentity)
	}
	if out.RuntimeIdentity.Release == "" || out.RuntimeIdentity.Source == "" {
		t.Fatalf("release and source provenance must be reported: %+v", out.RuntimeIdentity)
	}
	if !filepath.IsAbs(out.RuntimeIdentity.DBPath) {
		t.Fatalf("database identity must be absolute: %q", out.RuntimeIdentity.DBPath)
	}
	if !out.StoredStatePresent || !out.StaleStoredProcessIdentity || out.StoredProcessIsRunning {
		t.Fatalf("stale dead stored identity must be flagged: %+v", out)
	}

	// A stored identity that is still alive (old binary left connected) must
	// be visible as such, while the caller identity still wins.
	child := exec.Command("sleep", "60")
	if err := child.Start(); err != nil {
		t.Fatalf("start owned child: %v", err)
	}
	defer func() {
		_ = child.Process.Kill()
		_, _ = child.Process.Wait()
	}()
	storedIdentity := "pid:" + strconv.Itoa(child.Process.Pid) + ":start:1"
	if _, err := db.Exec(`UPDATE mcp_binary_state SET running_process_identity = ? WHERE project_id = ?`, storedIdentity, projectID); err != nil {
		t.Fatalf("update stored identity: %v", err)
	}
	_, out, err = GetMCPBinaryRestartState(context.Background(), nil, GetMCPBinaryRestartStateInput{})
	if err != nil {
		t.Fatalf("runtime health call: %v", err)
	}
	if !out.StoredProcessIsRunning || !out.StaleStoredProcessIdentity {
		t.Fatalf("alive foreign stored identity must be flagged: %+v", out)
	}
	if out.RuntimeIdentity.ProcessId != os.Getpid() {
		t.Fatalf("caller identity must still be reported: %+v", out.RuntimeIdentity)
	}
}

func TestRuntimeHealthEmptyDBEpochZeroStillReportsRunningCaller(t *testing.T) {
	originalDB, originalRole, originalProjectID := db, authorizedRole, authorizedProjectId
	defer func() { db, authorizedRole, authorizedProjectId = originalDB, originalRole, originalProjectID }()
	t.Setenv(fixerDBPathEnv, filepath.Join(t.TempDir(), "fresh-empty.db"))
	initDB()
	defer func() { _ = db.Close() }()
	authorizedRole, authorizedProjectId = "fixer", 1

	_, out, err := GetMCPBinaryRestartState(context.Background(), nil, GetMCPBinaryRestartStateInput{})
	if err != nil {
		t.Fatalf("runtime health call: %v", err)
	}
	if out.StoredStatePresent {
		t.Fatalf("empty DB has no stored bookkeeping: %+v", out)
	}
	if out.State.RunningBuildEpoch != 0 {
		t.Fatalf("empty DB must keep epoch 0 semantics: %+v", out.State)
	}
	if out.RuntimeIdentity.ProcessId != os.Getpid() || out.RuntimeIdentity.BuildId == "" {
		t.Fatalf("epoch 0 must not pretend the caller is not running: %+v", out.RuntimeIdentity)
	}
	if !strings.Contains(out.StateSummary, "epoch 0") || !strings.Contains(out.StateSummary, "running") {
		t.Fatalf("epoch 0 summary must state the caller is running: %q", out.StateSummary)
	}
	if out.StaleStoredProcessIdentity || out.StoredProcessIsRunning {
		t.Fatalf("no stored identity means no stale/running stored process: %+v", out)
	}
}

func TestRuntimeIdentityTempDBOverrideAndPathAliasNormalization(t *testing.T) {
	originalDB := db
	defer func() { db = originalDB }()
	tmpDir := t.TempDir()
	aliasPath := filepath.Join(tmpDir, "sub", "..", "aliased.db")
	t.Setenv(fixerDBPathEnv, aliasPath)
	initDB()
	defer func() { _ = db.Close() }()

	identity := collectRuntimeProcessIdentity()
	if identity.DBAuthority != dbAuthorityExplicitOverride {
		t.Fatalf("explicit FIXER_DB_PATH must be a sanctioned override: %+v", identity)
	}
	t.Setenv(fixerDBPathEnv, filepath.Join(tmpDir, "aliased.db"))
	aliasIdentity := collectRuntimeProcessIdentity()
	if identity.DBPath != aliasIdentity.DBPath {
		t.Fatalf("path aliases must normalize to one absolute path: %q != %q", identity.DBPath, aliasIdentity.DBPath)
	}
	if !filepath.IsAbs(identity.DBPath) {
		t.Fatalf("database identity must be absolute: %q", identity.DBPath)
	}
	if identity.DBFingerprint == "" || identity.SchemaFingerprint == "" {
		t.Fatalf("fingerprints must be reported: %+v", identity)
	}
	t.Setenv(fixerDBPathEnv, "")
	if authority := fixerDBAuthority(); authority != dbAuthorityHostCanonical {
		t.Fatalf("host canonical authority expected without override, got %q", authority)
	}
}

func TestSchemaEraDistinguishesStale109FromCurrent1011(t *testing.T) {
	originalDB := db
	defer func() { db = originalDB }()
	t.Setenv(fixerDBPathEnv, filepath.Join(t.TempDir(), "schema-era.db"))
	initDB()
	defer func() { _ = db.Close() }()

	if era := classifySchemaEra(); era != schemaEraCurrent {
		t.Fatalf("fresh schema must classify as %s, got %q", schemaEraCurrent, era)
	}
	staleErr := schemaEraAwareRestartStateError(errors.New("no such column: running_process_identity"))
	if !strings.Contains(staleErr.Error(), "current 1.0.11 schema error") {
		t.Fatalf("current-era errors must say so: %v", staleErr)
	}

	// Simulate a 1.0.9-era database: the post-1.0.11 identity columns are gone.
	if _, err := db.Exec(`ALTER TABLE mcp_binary_state DROP COLUMN running_process_identity`); err != nil {
		t.Fatalf("simulate stale schema: %v", err)
	}
	if era := classifySchemaEra(); era != schemaEraStale109 {
		t.Fatalf("stale schema must classify as %s, got %q", schemaEraStale109, era)
	}
	staleErr = schemaEraAwareRestartStateError(errors.New("no such column: running_process_identity"))
	if !strings.Contains(staleErr.Error(), "stale 1.0.9-era schema") || !strings.Contains(staleErr.Error(), "reconnect") {
		t.Fatalf("stale-era errors must be distinguishable and actionable: %v", staleErr)
	}
	genericErr := schemaEraAwareRestartStateError(errors.New("database is locked"))
	if !strings.Contains(genericErr.Error(), "schema era") {
		t.Fatalf("generic errors must still name the era: %v", genericErr)
	}
}

func TestRuntimeIdentityEmptyFileClassifiesAsEmptyEra(t *testing.T) {
	originalDB := db
	defer func() { db = originalDB }()
	emptyPath := filepath.Join(t.TempDir(), "zero.db")
	opened, err := sql.Open("sqlite", emptyPath)
	if err != nil {
		t.Fatalf("open empty db: %v", err)
	}
	db = opened
	defer func() { _ = db.Close() }()
	if era := classifySchemaEra(); era != schemaEraEmpty {
		t.Fatalf("zero-table database must classify as %s, got %q", schemaEraEmpty, era)
	}
}
