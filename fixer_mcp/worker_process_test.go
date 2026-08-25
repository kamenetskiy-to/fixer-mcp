package main

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestParallelWaveReviewPolicyDefaultsAndValidation(t *testing.T) {
	policy, backend, model, reasoning, err := resolveParallelWaveReviewConfig("", "", "", "")
	if err != nil {
		t.Fatalf("resolve default review config: %v", err)
	}
	if policy != parallelWaveReviewPolicyManual || backend != defaultParallelWaveReviewBackend || model != defaultParallelWaveReviewModel || reasoning != defaultParallelWaveReviewReasoning {
		t.Fatalf("unexpected default review config: %q %q %q %q", policy, backend, model, reasoning)
	}
	if _, err := normalizeParallelWaveReviewPolicy("disabled"); err == nil {
		t.Fatal("expected disabled review policy to be rejected")
	}
	policy, backend, model, reasoning, err = resolveParallelWaveReviewConfig("automatic", "codex", "custom-review-model", "medium")
	if err != nil {
		t.Fatalf("resolve automatic review config: %v", err)
	}
	if policy != parallelWaveReviewPolicyAutomatic || backend != "codex" || model != "custom-review-model" || reasoning != "medium" {
		t.Fatalf("unexpected automatic review config: %q %q %q %q", policy, backend, model, reasoning)
	}
	if err := ensureParallelWaveReviewer(context.Background(), NetrunnerWaveSnapshot{ReviewPolicy: parallelWaveReviewPolicyManual}); err != nil {
		t.Fatalf("manual review policy should skip reviewer creation: %v", err)
	}
	if _, err := applyParallelWaveReviewConfig(
		NetrunnerWaveSnapshot{ReviewPolicy: parallelWaveReviewPolicyManual, ReviewBackend: backend, ReviewModel: model, ReviewReasoning: reasoning, Workers: []NetrunnerWaveWorkerSnapshot{{Id: 1}}},
		LaunchNetrunnerWaveInput{ReviewPolicy: parallelWaveReviewPolicyAutomatic},
	); err == nil {
		t.Fatal("expected automatic review to be rejected for a one-worker wave")
	}
}

func TestLaunchParallelWaveReviewerUsesRuntimeSafePythonEnvironment(t *testing.T) {
	originalDB, originalRole, originalProjectID, originalExecCommand := db, authorizedRole, authorizedProjectId, execCommand
	defer func() {
		db, authorizedRole, authorizedProjectId, execCommand = originalDB, originalRole, originalProjectID, originalExecCommand
	}()

	repoDir := setupCleanGitRepo(t)
	testDB := setupParallelWaveTestDB(t, repoDir)
	defer testDB.Close()
	db, authorizedRole, authorizedProjectId = testDB, "fixer", 1
	t.Setenv(pythonNoBytecodeEnv, "0")

	var captured *exec.Cmd
	var capturedArgs []string
	execCommand = func(name string, args ...string) *exec.Cmd {
		if name != "python3" {
			t.Fatalf("unexpected reviewer command %q", name)
		}
		capturedArgs = append([]string(nil), args...)
		captured = exec.Command(os.Args[0], "-test.run=^$")
		return captured
	}

	if err := launchParallelWaveReviewer(context.Background(), NetrunnerWaveSnapshot{Id: 42}, parallelWaveReviewSession{LocalSessionID: 2}); err != nil {
		t.Fatalf("launch reviewer: %v", err)
	}
	if captured == nil {
		t.Fatal("reviewer command was not captured")
	}
	env := envSliceToMap(captured.Env)
	if env[pythonNoBytecodeEnv] != "1" {
		t.Fatalf("reviewer bytecode guard = %q, want 1", env[pythonNoBytecodeEnv])
	}
	if env["FIXER_REVIEW_WAVE_ID"] != "42" {
		t.Fatalf("review wave ID = %q, want 42", env["FIXER_REVIEW_WAVE_ID"])
	}
	joinedArgs := strings.Join(capturedArgs, " ")
	for _, expected := range []string{
		"--backend " + defaultParallelWaveReviewBackend,
		"--model " + defaultParallelWaveReviewModel,
		"--reasoning " + defaultParallelWaveReviewReasoning,
	} {
		if !strings.Contains(joinedArgs, expected) {
			t.Fatalf("reviewer args %q do not contain %q", joinedArgs, expected)
		}
	}
}
