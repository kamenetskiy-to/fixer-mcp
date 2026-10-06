package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
)

// Upgrade/probe safety for the scope retirement migration: an incompatible
// activation is admitted only when no OLD worker/process identity is still
// alive. A deferred run must not touch a single row (read-only diagnostics
// only); once the exact recorded PID stops, the upgrade runs and stays safe to
// repeat. Only exact positively-owned PIDs are ever stopped in these tests.

func TestScopeRetirementDefersWhileOldWorkerAliveWithoutTouchingRows(t *testing.T) {
	originalDB := db
	defer func() { db = originalDB }()
	t.Setenv(fixerDBPathEnv, filepath.Join(t.TempDir(), "deferred-retirement.db"))
	initDB()
	defer func() { _ = db.Close() }()
	seedLegacyWriteScopeSurface(t)
	sessionBaseline := countRows(t, `SELECT COUNT(*) FROM session`)

	child := exec.Command("sleep", "60")
	if err := child.Start(); err != nil {
		t.Fatalf("start owned old worker: %v", err)
	}
	defer func() {
		_ = child.Process.Kill()
		_, _ = child.Process.Wait()
	}()
	childPID := strconv.Itoa(child.Process.Pid)
	if _, err := db.Exec(`
		INSERT INTO worker_process (project_id, session_id, pid, launch_epoch, status, launch_origin)
		VALUES (1, 1, ` + childPID + `, 3, 'running', 'explicit')`); err != nil {
		t.Fatalf("seed active old worker row: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO mcp_binary_state (
			project_id, running_build_epoch, required_build_epoch, restart_required,
			running_build_id, required_build_id, running_process_identity, required_by_process_identity,
			reason, updated_at
		) VALUES (1, 5, 0, 0, 'sha256:old', '', 'pid:` + childPID + `:start:999', '', 'old runtime', CURRENT_TIMESTAMP)`); err != nil {
		t.Fatalf("seed old runtime identity: %v", err)
	}

	err := migrateDeclaredWriteScopeRetirement()
	if !errors.Is(err, errMigrationDeferredQuiescence) {
		t.Fatalf("active old worker must defer the upgrade, got %v", err)
	}

	// Read-only diagnostics while deferred: nothing written, nothing dropped.
	if !dbTableHasColumn("session", "declared_write_scope") {
		t.Fatal("deferred upgrade must not touch rows or columns")
	}
	if !dbTableExists("parallel_wave_scope_lease") {
		t.Fatal("deferred upgrade must leave legacy lease tables in place")
	}
	if archived := countRows(t, `SELECT COUNT(*) FROM `+retiredWriteScopeArchiveTable); archived != 0 {
		t.Fatalf("deferred upgrade must not archive or rewrite rows, got %d archived", archived)
	}
	if got := countRows(t, `SELECT COUNT(*) FROM session`); got != sessionBaseline {
		t.Fatalf("deferred upgrade must preserve rows, got %d sessions want %d", got, sessionBaseline)
	}
}

func TestScopeRetirementAdmitsUpgradeAfterOldProcessStopsAndRepeatsSafely(t *testing.T) {
	originalDB := db
	defer func() { db = originalDB }()
	t.Setenv(fixerDBPathEnv, filepath.Join(t.TempDir(), "admitted-retirement.db"))
	initDB()
	defer func() { _ = db.Close() }()
	seedLegacyWriteScopeSurface(t)
	sessionBaseline := countRows(t, `SELECT COUNT(*) FROM session`)

	child := exec.Command("sleep", "60")
	if err := child.Start(); err != nil {
		t.Fatalf("start owned old worker: %v", err)
	}
	childPID := strconv.Itoa(child.Process.Pid)
	if _, err := db.Exec(`
		INSERT INTO worker_process (project_id, session_id, pid, launch_epoch, status, launch_origin)
		VALUES (1, 1, ` + childPID + `, 3, 'running', 'explicit')`); err != nil {
		t.Fatalf("seed active old worker row: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO mcp_binary_state (
			project_id, running_build_epoch, required_build_epoch, restart_required,
			running_build_id, required_build_id, running_process_identity, required_by_process_identity,
			reason, updated_at
		) VALUES (1, 5, 0, 0, 'sha256:old', '', 'pid:` + childPID + `:start:999', '', 'old runtime', CURRENT_TIMESTAMP)`); err != nil {
		t.Fatalf("seed old runtime identity: %v", err)
	}

	// The exact recorded PID stops (positively owned child only).
	if err := child.Process.Kill(); err != nil {
		t.Fatalf("stop owned old worker: %v", err)
	}
	_, _ = child.Process.Wait()

	if err := migrateDeclaredWriteScopeRetirement(); err != nil {
		t.Fatalf("stopped old PID must admit the upgrade: %v", err)
	}
	if dbTableHasColumn("session", "declared_write_scope") {
		t.Fatal("admitted upgrade must retire the legacy column")
	}
	archiveRows := countRows(t, `SELECT COUNT(*) FROM `+retiredWriteScopeArchiveTable)
	if archiveRows == 0 {
		t.Fatal("admitted upgrade must archive legacy values before dropping them")
	}

	// Safe repeat: a second run is a no-op and never re-archives or rewrites.
	if err := migrateDeclaredWriteScopeRetirement(); err != nil {
		t.Fatalf("repeated upgrade must be safe: %v", err)
	}
	if again := countRows(t, `SELECT COUNT(*) FROM `+retiredWriteScopeArchiveTable); again != archiveRows {
		t.Fatalf("repeat must not duplicate archive history: %d != %d", again, archiveRows)
	}
	if got := countRows(t, `SELECT COUNT(*) FROM session`); got != sessionBaseline {
		t.Fatalf("upgrade must preserve rows, got %d sessions want %d", got, sessionBaseline)
	}
}

func TestScopeRetirementAdmissionExcludesCallerOwnProcess(t *testing.T) {
	originalDB := db
	defer func() { db = originalDB }()
	t.Setenv(fixerDBPathEnv, filepath.Join(t.TempDir(), "self-admission.db"))
	initDB()
	defer func() { _ = db.Close() }()
	seedLegacyWriteScopeSurface(t)

	// The caller's own live PID is this process, not a stale one: it must not
	// defer the upgrade it is running.
	selfPID := strconv.Itoa(os.Getpid())
	if _, err := db.Exec(`
		INSERT INTO worker_process (project_id, session_id, pid, launch_epoch, status, launch_origin)
		VALUES (1, 1, ` + selfPID + `, 4, 'running', 'explicit')`); err != nil {
		t.Fatalf("seed self worker row: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO mcp_binary_state (
			project_id, running_build_epoch, required_build_epoch, restart_required,
			running_build_id, required_build_id, running_process_identity, required_by_process_identity,
			reason, updated_at
		) VALUES (1, 6, 0, 0, 'sha256:self', '', '` + mcpProcessIdentity + `', '', 'self runtime', CURRENT_TIMESTAMP)`); err != nil {
		t.Fatalf("seed self runtime identity: %v", err)
	}

	if err := migrateDeclaredWriteScopeRetirement(); err != nil {
		t.Fatalf("caller's own identity must not defer the upgrade: %v", err)
	}
}

func TestScopeRetirementFreshSchemaFastPathWritesNothing(t *testing.T) {
	originalDB := db
	defer func() { db = originalDB }()
	t.Setenv(fixerDBPathEnv, filepath.Join(t.TempDir(), "fresh-fast-path.db"))
	initDB()
	defer func() { _ = db.Close() }()

	pending, err := scopeRetirementWorkPending()
	if err != nil {
		t.Fatalf("pending check: %v", err)
	}
	if pending {
		t.Fatal("fresh schema has no retirement work")
	}
	if err := migrateDeclaredWriteScopeRetirement(); err != nil {
		t.Fatalf("fast path: %v", err)
	}
	if archived := countRows(t, `SELECT COUNT(*) FROM `+retiredWriteScopeArchiveTable); archived != 0 {
		t.Fatalf("fast path must archive nothing, got %d archived", archived)
	}
}
