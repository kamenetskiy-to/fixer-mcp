package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeParallelWaveAdmissionWorkersRequiresDistinctSessionIDs(t *testing.T) {
	workers := []parallelWaveAdmissionWorker{
		{SessionID: 1},
		{SessionID: 2},
	}

	normalized, err := normalizeParallelWaveAdmissionWorkersWithDependencies(workers, nil)
	if err != nil {
		t.Fatalf("expected workers to be admitted without any scope declaration, got %v", err)
	}
	if len(normalized) != len(workers) || normalized[0].SessionID != 1 || normalized[1].SessionID != 2 {
		t.Fatalf("unexpected normalized workers: %+v", normalized)
	}

	dependencies := []WaveDependency{{Child: 2, Parents: []int64{1}}}
	if _, err := normalizeParallelWaveAdmissionWorkersWithDependencies(workers, dependencies); err != nil {
		t.Fatalf("expected parent-child workers to be admitted: %v", err)
	}

	duplicateWorkers := []parallelWaveAdmissionWorker{
		{SessionID: 1},
		{SessionID: 1},
	}
	if _, err := normalizeParallelWaveAdmissionWorkers(duplicateWorkers); err == nil {
		t.Fatal("expected duplicate session ids to be rejected")
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
