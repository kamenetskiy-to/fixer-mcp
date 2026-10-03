package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeParallelWaveAdmissionWorkersAllowsOverlappingScopes(t *testing.T) {
	workers := []parallelWaveAdmissionWorker{
		{SessionID: 1, DeclaredWriteScope: []string{"docs"}},
		{SessionID: 2, DeclaredWriteScope: []string{"docs/generated"}},
	}

	normalized, err := normalizeParallelWaveAdmissionWorkersWithDependencies(workers, nil)
	if err != nil {
		t.Fatalf("expected unrelated overlapping scopes to be admitted, got %v", err)
	}
	if len(normalized) != len(workers) || normalized[0].DeclaredWriteScope[0] != "docs" || normalized[1].DeclaredWriteScope[0] != "docs/generated" {
		t.Fatalf("unexpected normalized workers: %+v", normalized)
	}

	dependencies := []WaveDependency{{Child: 2, Parents: []int64{1}}}
	if _, err := normalizeParallelWaveAdmissionWorkersWithDependencies(workers, dependencies); err != nil {
		t.Fatalf("expected parent-child overlap to be allowed: %v", err)
	}

	transitiveWorkers := []parallelWaveAdmissionWorker{
		{SessionID: 1, DeclaredWriteScope: []string{"docs"}},
		{SessionID: 3, DeclaredWriteScope: []string{"docs/generated"}},
	}
	transitiveDependencies := []WaveDependency{
		{Child: 2, Parents: []int64{1}},
		{Child: 3, Parents: []int64{2}},
	}
	if _, err := normalizeParallelWaveAdmissionWorkersWithDependencies(transitiveWorkers, transitiveDependencies); err != nil {
		t.Fatalf("expected transitive parent-child overlap to be allowed: %v", err)
	}
}

func TestNormalizeParallelWaveDeclaredWriteScopeAllowsFormerlyFencedScopes(t *testing.T) {
	for _, scope := range [][]string{
		{"fixer_mcp/main.go"},
		{"client_wires/fixer_autonomous.py"},
		{".codex/netrunner_worktrees"},
		{"artifacts/runtime.db"},
		{"."},
		{"docs/a", "docs/a/subtree"},
	} {
		if _, err := normalizeParallelWaveDeclaredWriteScope(scope); err != nil {
			t.Fatalf("expected formerly fenced scope %v to be admitted, got %v", scope, err)
		}
	}

	if _, err := normalizeParallelWaveDeclaredWriteScope([]string{"docs/runtime.dbx"}); err != nil {
		t.Fatalf("unexpected rejection for non-database suffix: %v", err)
	}

	if _, err := normalizeParallelWaveDeclaredWriteScope(nil); err != nil {
		t.Fatalf("missing scope must be admitted: %v", err)
	}

	if _, err := normalizeParallelWaveDeclaredWriteScope([]string{"/absolute/path"}); err == nil {
		t.Fatal("expected path-syntax validation to still reject absolute entries")
	}
}

func TestSplitGitPathLinesPreservesNULDelimitedSpecialNames(t *testing.T) {
	got := splitGitPathLines(" docs/leading.txt\x00docs/na\nme.txt\x00docs/trailing.txt \x00")
	want := []string{" docs/leading.txt", "docs/na\nme.txt", "docs/trailing.txt "}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("splitGitPathLines() = %#v, want %#v", got, want)
	}
}

func TestGitTreeGitlinksNormalizesGitlinkPaths(t *testing.T) {
	raw := []byte("100644 blob abc123\tregular.txt\x00160000 commit deadbeef\tvendor/lib\x00")
	got := gitTreeGitlinks(raw)
	if len(got) != 1 || got["vendor/lib"] != "deadbeef" {
		t.Fatalf("gitTreeGitlinks() = %#v, want vendor/lib -> deadbeef", got)
	}
}

func TestGitMergeBranchCommand(t *testing.T) {
	spec, err := gitMergeBranchCommand("/tmp/child-worktree", "fixer/wave-54/session-1")
	if err != nil {
		t.Fatalf("gitMergeBranchCommand failed: %v", err)
	}
	if spec.Name != "git" || strings.Join(spec.Args, " ") != "-C /tmp/child-worktree merge --no-edit fixer/wave-54/session-1" {
		t.Fatalf("unexpected merge command: %+v", spec)
	}

	if _, err := gitMergeBranchCommand("/tmp/child-worktree", "main"); err == nil {
		t.Fatal("expected invalid parent branch name to be rejected")
	}
}

func TestVerifyParallelWaveGitBaseAllowsNestedGitRoot(t *testing.T) {
	projectCWD := t.TempDir()
	repoDir := filepath.Join(projectCWD, "rita_repo")
	setupCleanGitRepoAt(t, repoDir)

	expectedBaseSHA := runGitTestCommand(t, repoDir, "rev-parse", "--verify", "HEAD^{commit}")
	baseSHA, _, err := verifyParallelWaveGitBase(projectCWD, "HEAD")
	if err != nil {
		t.Fatalf("expected project cwd containing nested Git root to be accepted: %v", err)
	}
	if baseSHA != expectedBaseSHA {
		t.Fatalf("expected base SHA %q, got %q", expectedBaseSHA, baseSHA)
	}

	if _, err := os.Stat(filepath.Join(repoDir, ".git")); err != nil {
		t.Fatalf("expected nested Git repository to remain available: %v", err)
	}
}
