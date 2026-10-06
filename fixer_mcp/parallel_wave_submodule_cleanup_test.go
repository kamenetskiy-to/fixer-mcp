package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupGitRepoWithSubmodule builds a fixture superproject with one committed,
// initialized submodule at modules/sub.
func setupGitRepoWithSubmodule(t *testing.T) string {
	t.Helper()
	subDir := filepath.Join(t.TempDir(), "sub")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatalf("create sub repo dir: %v", err)
	}
	runGitTestCommand(t, subDir, "init")
	if err := os.WriteFile(filepath.Join(subDir, "a.txt"), []byte("original\n"), 0o644); err != nil {
		t.Fatalf("write sub file: %v", err)
	}
	runGitTestCommand(t, subDir, "add", "a.txt")
	runGitTestCommand(t, subDir, "-c", "user.name=Fixer Test", "-c", "user.email=fixer@example.test", "commit", "-m", "sub initial")

	superDir := t.TempDir()
	runGitTestCommand(t, superDir, "init")
	if err := os.WriteFile(filepath.Join(superDir, "README.md"), []byte("super\n"), 0o644); err != nil {
		t.Fatalf("write super README: %v", err)
	}
	runGitTestCommand(t, superDir, "add", "README.md")
	runGitTestCommand(t, superDir, "-c", "user.name=Fixer Test", "-c", "user.email=fixer@example.test", "commit", "-m", "super initial")
	runGitTestCommand(t, superDir, "-c", "protocol.file.allow=always", "submodule", "add", subDir, "modules/sub")
	runGitTestCommand(t, superDir, "-c", "user.name=Fixer Test", "-c", "user.email=fixer@example.test", "commit", "-m", "add submodule")
	return superDir
}

// addInitializedSubmoduleWorktree creates a linked worktree whose submodule is
// initialized — the exact shape `git worktree remove` refuses without force and
// older Git refuses even with force.
func addInitializedSubmoduleWorktree(t *testing.T, superDir string, branch string) string {
	t.Helper()
	wtPath := filepath.Join(t.TempDir(), "wt-"+branch)
	runGitTestCommand(t, superDir, "worktree", "add", "-b", branch, wtPath, "HEAD")
	runGitTestCommand(t, wtPath, "-c", "protocol.file.allow=always", "submodule", "update", "--init")
	return wtPath
}

// Clean case: an initialized-but-clean submodule worktree must be removed even
// without the caller's force flag (deinit + verified-clean removal).
func TestRemoveTerminalWorktreeSafelyRemovesCleanSubmoduleWorktree(t *testing.T) {
	superDir := setupGitRepoWithSubmodule(t)
	wtPath := addInitializedSubmoduleWorktree(t, superDir, "b-clean")

	removed, diagnostic, err := removeTerminalWorktreeSafely(superDir, wtPath, false)
	if err != nil {
		t.Fatalf("clean submodule worktree must be removable: %v", err)
	}
	if !removed {
		t.Fatalf("expected removal, got diagnostic %q", diagnostic)
	}
	if _, statErr := os.Stat(wtPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected worktree removed, stat err=%v", statErr)
	}
	// The project's own submodule checkout must be untouched.
	if _, err := os.Stat(filepath.Join(superDir, "modules", "sub", "a.txt")); err != nil {
		t.Fatalf("the main checkout's submodule must stay intact: %v", err)
	}
}

// Dirty case: unknown dirty submodule data is preserved and reported — never
// deleted — even when the caller passes force.
func TestRemoveTerminalWorktreeSafelyPreservesDirtySubmoduleData(t *testing.T) {
	superDir := setupGitRepoWithSubmodule(t)
	wtPath := addInitializedSubmoduleWorktree(t, superDir, "b-dirty")

	dirtyFile := filepath.Join(wtPath, "modules", "sub", "untracked-unknown.txt")
	if err := os.WriteFile(dirtyFile, []byte("unknown dirty data\n"), 0o644); err != nil {
		t.Fatalf("write dirty submodule data: %v", err)
	}
	modifiedFile := filepath.Join(wtPath, "modules", "sub", "a.txt")
	if err := os.WriteFile(modifiedFile, []byte("locally modified\n"), 0o644); err != nil {
		t.Fatalf("modify submodule data: %v", err)
	}

	for _, force := range []bool{true, false} {
		removed, diagnostic, err := removeTerminalWorktreeSafely(superDir, wtPath, force)
		if err != nil {
			t.Fatalf("dirty submodule data must preserve without error (force=%v): %v", force, err)
		}
		if removed {
			t.Fatalf("dirty submodule data must not be removed (force=%v)", force)
		}
		if !strings.Contains(diagnostic, "preserved") || !strings.Contains(diagnostic, "modules/sub") {
			t.Fatalf("expected a preserved-data diagnostic naming the submodule, got %q", diagnostic)
		}
	}
	for _, path := range []string{dirtyFile, modifiedFile} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("preserved submodule data must survive at %s: %v", path, err)
		}
	}
}

// Plain worktrees keep the pre-existing force-flag semantics exactly.
func TestRemoveTerminalWorktreeSafelyPlainWorktreeHonorsForceFlag(t *testing.T) {
	repoDir := setupCleanGitRepo(t)
	wtPath := filepath.Join(t.TempDir(), "wt-plain")
	runGitTestCommand(t, repoDir, "worktree", "add", "-b", "b-plain", wtPath, "HEAD")
	if err := os.WriteFile(filepath.Join(wtPath, "tracked-dirt.txt"), []byte("dirt\n"), 0o644); err != nil {
		t.Fatalf("write dirt: %v", err)
	}

	removed, _, err := removeTerminalWorktreeSafely(repoDir, wtPath, false)
	if err == nil || removed {
		t.Fatalf("non-forced removal of a dirty plain worktree must fail as before, removed=%v err=%v", removed, err)
	}
	if _, statErr := os.Stat(filepath.Join(wtPath, "tracked-dirt.txt")); statErr != nil {
		t.Fatalf("non-forced refusal must preserve the dirt: %v", statErr)
	}

	removed, diagnostic, err := removeTerminalWorktreeSafely(repoDir, wtPath, true)
	if err != nil {
		t.Fatalf("forced removal of a dirty plain worktree must keep working: %v (diagnostic %q)", err, diagnostic)
	}
	if !removed {
		t.Fatalf("expected forced removal, got diagnostic %q", diagnostic)
	}
	if _, statErr := os.Stat(wtPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected worktree removed, stat err=%v", statErr)
	}
}

// CleanupNetrunnerWave integration: clean submodule worktrees are removed and
// marked cleaned; dirty submodule data is preserved with diagnostics and the
// wave is not falsely marked cleaned.
func TestCleanupNetrunnerWaveSubmoduleWorktreeRemovalAndPreservation(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
	}()

	t.Run("clean submodules are removed", func(t *testing.T) {
		repoDir, testDB, created, wave := setupRunningWaveTest(t)
		defer func() { _ = testDB.Close() }()

		// Give the first worker's worktree an initialized submodule.
		worker := wave.Workers[0]
		absWorktreePath, err := resolveParallelWaveWorktreePath(repoDir, worker.WorktreePath)
		if err != nil {
			t.Fatalf("resolve worktree: %v", err)
		}
		subDir := filepath.Join(t.TempDir(), "cleanup-sub")
		if err := os.MkdirAll(subDir, 0o755); err != nil {
			t.Fatalf("create sub repo dir: %v", err)
		}
		runGitTestCommand(t, subDir, "init")
		if err := os.WriteFile(filepath.Join(subDir, "a.txt"), []byte("original\n"), 0o644); err != nil {
			t.Fatalf("write sub file: %v", err)
		}
		runGitTestCommand(t, subDir, "add", "a.txt")
		runGitTestCommand(t, subDir, "-c", "user.name=Fixer Test", "-c", "user.email=fixer@example.test", "commit", "-m", "sub initial")
		runGitTestCommand(t, absWorktreePath, "-c", "protocol.file.allow=always", "submodule", "add", subDir, "modules/sub")
		runGitTestCommand(t, absWorktreePath, "-c", "user.name=Fixer Test", "-c", "user.email=fixer@example.test", "commit", "-m", "add submodule")
		runGitTestCommand(t, absWorktreePath, "-c", "protocol.file.allow=always", "submodule", "update", "--init")

		markTestWaveTerminalForCleanup(t, testDB, created.WaveId, parallelWaveWorkerStatusCompleted)
		callResult, out, err := CleanupNetrunnerWave(context.Background(), nil, CleanupNetrunnerWaveInput{
			WaveId:          created.WaveId,
			RemoveWorktrees: true,
			Force:           true,
		})
		if err != nil || callResult != nil {
			t.Fatalf("submodule worktree cleanup must succeed: result=%+v err=%v", callResult, err)
		}
		if out.Status != "success" || !out.Cleaned {
			t.Fatalf("expected a fully cleaned wave, got %+v", out)
		}
		if _, statErr := os.Stat(absWorktreePath); !os.IsNotExist(statErr) {
			t.Fatalf("expected the submodule worktree removed, stat err=%v", statErr)
		}
	})

	t.Run("dirty submodule data is preserved with diagnostics", func(t *testing.T) {
		repoDir, testDB, created, wave := setupRunningWaveTest(t)
		defer func() { _ = testDB.Close() }()

		worker := wave.Workers[0]
		absWorktreePath, err := resolveParallelWaveWorktreePath(repoDir, worker.WorktreePath)
		if err != nil {
			t.Fatalf("resolve worktree: %v", err)
		}
		subDir := filepath.Join(t.TempDir(), "dirty-sub")
		if err := os.MkdirAll(subDir, 0o755); err != nil {
			t.Fatalf("create sub repo dir: %v", err)
		}
		runGitTestCommand(t, subDir, "init")
		if err := os.WriteFile(filepath.Join(subDir, "a.txt"), []byte("original\n"), 0o644); err != nil {
			t.Fatalf("write sub file: %v", err)
		}
		runGitTestCommand(t, subDir, "add", "a.txt")
		runGitTestCommand(t, subDir, "-c", "user.name=Fixer Test", "-c", "user.email=fixer@example.test", "commit", "-m", "sub initial")
		runGitTestCommand(t, absWorktreePath, "-c", "protocol.file.allow=always", "submodule", "add", subDir, "modules/sub")
		runGitTestCommand(t, absWorktreePath, "-c", "user.name=Fixer Test", "-c", "user.email=fixer@example.test", "commit", "-m", "add submodule")
		runGitTestCommand(t, absWorktreePath, "-c", "protocol.file.allow=always", "submodule", "update", "--init")
		dirtyFile := filepath.Join(absWorktreePath, "modules", "sub", "unknown.txt")
		if err := os.WriteFile(dirtyFile, []byte("unknown dirty data\n"), 0o644); err != nil {
			t.Fatalf("write dirty submodule data: %v", err)
		}

		markTestWaveTerminalForCleanup(t, testDB, created.WaveId, parallelWaveWorkerStatusCompleted)
		callResult, out, err := CleanupNetrunnerWave(context.Background(), nil, CleanupNetrunnerWaveInput{
			WaveId:          created.WaveId,
			RemoveWorktrees: true,
			Force:           true,
		})
		if err != nil || callResult != nil {
			t.Fatalf("preserving cleanup must not error: result=%+v err=%v", callResult, err)
		}
		if out.Status != "partial_failure" || out.Cleaned {
			t.Fatalf("preserved dirty data must not read as fully cleaned, got %+v", out)
		}
		preservedFound := false
		for _, result := range out.Workers {
			if result.WorkerId == worker.Id {
				if !result.Preserved || result.CleanupStatus != parallelWaveCleanupStatusPreserved || !strings.Contains(result.Diagnostic, "preserved") {
					t.Fatalf("expected preserved diagnostics for the dirty submodule worker, got %+v", result)
				}
				preservedFound = true
			}
		}
		if !preservedFound {
			t.Fatalf("expected a preserved worker result, got %+v", out.Workers)
		}
		if _, statErr := os.Stat(dirtyFile); statErr != nil {
			t.Fatalf("the dirty submodule data must survive: %v", statErr)
		}
	})
}
