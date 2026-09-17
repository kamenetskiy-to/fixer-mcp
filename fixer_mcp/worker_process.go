package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const parallelWaveReviewMarkerPrefix = "parallel-wave-review:"

type parallelWaveReviewSession struct {
	GlobalSessionID int
	LocalSessionID  int
	Status          string
	Report          string
}

func normalizeParallelWaveReviewPolicy(raw string) (string, error) {
	policy := strings.ToLower(strings.TrimSpace(raw))
	if policy == "" {
		return defaultParallelWaveReviewPolicy, nil
	}
	switch policy {
	case parallelWaveReviewPolicyAutomatic, parallelWaveReviewPolicyManual:
		return policy, nil
	default:
		return "", fmt.Errorf("unsupported review_policy %q; supported values are %q and %q", raw, parallelWaveReviewPolicyAutomatic, parallelWaveReviewPolicyManual)
	}
}

func resolveParallelWaveReviewConfig(policy, backend, model, reasoning string) (string, string, string, string, error) {
	normalizedPolicy, err := normalizeParallelWaveReviewPolicy(policy)
	if err != nil {
		return "", "", "", "", err
	}
	normalizedBackend := strings.TrimSpace(backend)
	if normalizedBackend == "" {
		normalizedBackend = defaultParallelWaveReviewBackend
	}
	normalizedModel := strings.TrimSpace(model)
	if normalizedModel == "" {
		normalizedModel = defaultParallelWaveReviewModel
	}
	if normalizedModel == "opencode-go/deepseek-v4-pro" {
		return "", "", "", "", fmt.Errorf("OpenCode Go DeepSeek V4 Pro is disabled for Fixer review")
	}
	normalizedReasoning := strings.TrimSpace(reasoning)
	if normalizedReasoning == "" {
		normalizedReasoning = defaultParallelWaveReviewReasoning
	}
	return normalizedPolicy, normalizedBackend, normalizedModel, normalizedReasoning, nil
}

func parallelWaveReviewMarker(waveID int) string {
	return fmt.Sprintf("%s%d", parallelWaveReviewMarkerPrefix, waveID)
}

func parallelWaveReviewNeedsVisualVerification(wave NetrunnerWaveSnapshot) bool {
	for _, worker := range wave.Workers {
		for _, rawPath := range worker.ChangedPaths {
			path := strings.ToLower(filepath.ToSlash(strings.TrimSpace(rawPath)))
			if strings.HasPrefix(path, "fixer_mcp/dashboard_app/") ||
				strings.HasPrefix(path, "fixer_mcp/dashboard_client/") {
				return true
			}
			switch filepath.Ext(path) {
			case ".dart", ".css", ".scss", ".less", ".html", ".tsx", ".jsx", ".vue", ".svelte":
				return true
			}
		}
	}
	return false
}

func parallelWaveReviewTaskDescription(wave NetrunnerWaveSnapshot) string {
	changedPaths := []string{}
	worktrees := []string{}
	seenPaths := map[string]struct{}{}
	seenWorktrees := map[string]struct{}{}
	for _, worker := range wave.Workers {
		if path := strings.TrimSpace(worker.WorktreePath); path != "" {
			if _, seen := seenWorktrees[path]; !seen {
				seenWorktrees[path] = struct{}{}
				worktrees = append(worktrees, path)
			}
		}
		for _, rawPath := range worker.ChangedPaths {
			path := strings.TrimSpace(rawPath)
			if path == "" {
				continue
			}
			if _, seen := seenPaths[path]; !seen {
				seenPaths[path] = struct{}{}
				changedPaths = append(changedPaths, path)
			}
		}
	}

	visualInstruction := "No UI paths were detected; record visual verification as not applicable."
	if parallelWaveReviewNeedsVisualVerification(wave) {
		visualInstruction = "UI paths were detected; activate `$run-visual-verifier` when the project supports it and record the result or the exact reason it was skipped."
	}
	return fmt.Sprintf(
		"Post-wave Reviewer for parallel wave %d. Review the completed worker changes and produce a concise, evidence-based structured final report.\n\n"+
			"Run the relevant Go tests and linters for the changed areas. Inspect each worker worktree and the captured diff artifacts; do not modify product files. %s\n"+
			"If checks fail or you find a regression, put the concrete failure, command, and affected path in blockers/residual_risks so the Architect can dispatch a manual repair Netrunner. If all checks pass, state that explicitly in checks_run. Submit the mandatory documentation-impact proposal and complete this review session.\n\n"+
			"Worker worktrees: %s\nChanged paths: %s",
		wave.Id,
		visualInstruction,
		strings.Join(worktrees, ", "),
		strings.Join(changedPaths, ", "),
	)
}

func fetchParallelWaveReviewSession(waveID int, projectID int) (parallelWaveReviewSession, bool, error) {
	if waveID <= 0 || projectID <= 0 || !dbTableHasColumn("session", "parallel_wave_id") {
		return parallelWaveReviewSession{}, false, nil
	}
	var review parallelWaveReviewSession
	err := db.QueryRow(
		`SELECT id, status, COALESCE(report, '')
		 FROM session
		 WHERE project_id = ? AND parallel_wave_id = ?
		 ORDER BY id DESC
		 LIMIT 1`,
		projectID,
		parallelWaveReviewMarker(waveID),
	).Scan(&review.GlobalSessionID, &review.Status, &review.Report)
	if err == sql.ErrNoRows {
		return parallelWaveReviewSession{}, false, nil
	}
	if err != nil {
		return parallelWaveReviewSession{}, false, err
	}
	review.LocalSessionID, err = projectScopedSessionIDFromGlobal(review.GlobalSessionID, projectID)
	if err != nil {
		return parallelWaveReviewSession{}, false, err
	}
	return review, true, nil
}

func updateParallelWaveSnapshotReview(wave *NetrunnerWaveSnapshot, projectID int) error {
	if wave == nil {
		return nil
	}
	review, found, err := fetchParallelWaveReviewSession(wave.Id, projectID)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	wave.ReviewSessionId = review.LocalSessionID
	wave.ReviewSessionStatus = review.Status
	wave.ReviewSessionReport = review.Report
	return nil
}

func createParallelWaveReviewSession(wave NetrunnerWaveSnapshot) (parallelWaveReviewSession, error) {
	declaredWriteScope, err := encodeDeclaredWriteScope([]string{"fixer_mcp", "dashboard_api"})
	if err != nil {
		return parallelWaveReviewSession{}, err
	}
	taskDescription := parallelWaveReviewTaskDescription(wave)
	result, err := db.Exec(
		`INSERT INTO session (
			project_id, task_description, status, declared_write_scope, parallel_wave_id,
			cli_backend, cli_model, cli_reasoning
		) VALUES (?, ?, 'pending', ?, ?, ?, ?, ?)`,
		authorizedProjectId,
		taskDescription,
		declaredWriteScope,
		parallelWaveReviewMarker(wave.Id),
		defaultParallelWaveReviewBackend,
		defaultParallelWaveReviewModel,
		defaultParallelWaveReviewReasoning,
	)
	if err != nil {
		return parallelWaveReviewSession{}, err
	}
	globalSessionID, err := result.LastInsertId()
	if err != nil {
		return parallelWaveReviewSession{}, err
	}
	localSessionID, err := projectScopedSessionIDFromGlobal(int(globalSessionID), authorizedProjectId)
	if err != nil {
		return parallelWaveReviewSession{}, err
	}
	return parallelWaveReviewSession{
		GlobalSessionID: int(globalSessionID),
		LocalSessionID:  localSessionID,
		Status:          "pending",
	}, nil
}

func parallelWaveReviewFailureReport(waveID int, reason string) string {
	report, _ := json.Marshal(map[string]any{
		"files_changed": []string{},
		"commands_run":  []string{"parallel wave reviewer launch"},
		"checks_run":    []string{"reviewer launch diagnostics persisted"},
		"blockers":      []string{fmt.Sprintf("parallel wave %d reviewer could not start: %s", waveID, reason)},
	})
	return string(report)
}

func markParallelWaveReviewLaunchFailure(sessionID int, waveID int, reason string) error {
	_, err := db.Exec(
		`UPDATE session
		 SET status = 'review', report = ?
		 WHERE id = ? AND project_id = ?`,
		parallelWaveReviewFailureReport(waveID, reason),
		sessionID,
		authorizedProjectId,
	)
	return err
}

func recordParallelWaveReviewProcess(projectID int, sessionID int, pid int, launchEpoch int, waveID int) error {
	var (
		result sql.Result
		err    error
	)
	if dbTableHasColumn("worker_process", "launch_origin") {
		result, err = db.Exec(
			`INSERT INTO worker_process (
				project_id, session_id, pid, launch_epoch, status, launch_origin
			) VALUES (?, ?, ?, ?, ?, ?)`,
			projectID,
			sessionID,
			pid,
			launchEpoch,
			workerStatusRunning,
			"parallel-wave-reviewer",
		)
	} else {
		result, err = db.Exec(
			`INSERT INTO worker_process (
				project_id, session_id, pid, launch_epoch, status
			) VALUES (?, ?, ?, ?, ?)`,
			projectID,
			sessionID,
			pid,
			launchEpoch,
			workerStatusRunning,
		)
	}
	if err != nil {
		return err
	}
	processID, err := result.LastInsertId()
	if err != nil {
		return err
	}
	if dbTableHasColumn("worker_process", "parallel_wave_id") {
		if _, err := db.Exec(
			`UPDATE worker_process SET parallel_wave_id = ? WHERE id = ? AND project_id = ?`,
			waveID,
			processID,
			projectID,
		); err != nil {
			return err
		}
	}
	return nil
}

func launchParallelWaveReviewer(ctx context.Context, wave NetrunnerWaveSnapshot, review parallelWaveReviewSession) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	projectCWD, err := projectCWDFromID(authorizedProjectId)
	if err != nil {
		return err
	}
	projectCWD, err = normalizeProjectCWD(projectCWD)
	if err != nil {
		return err
	}
	launcherScript, err := resolveExplicitLauncherScript()
	if err != nil {
		return err
	}
	logPath := filepath.Join(projectCWD, ".codex", "netrunner_review_artifacts", fmt.Sprintf("wave-%d", wave.Id), "reviewer.log")
	metadataPath := filepath.Join(projectCWD, ".codex", "netrunner_review_artifacts", fmt.Sprintf("wave-%d", wave.Id), "reviewer.json")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return err
	}
	logHandle, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer logHandle.Close()
	reviewBackend := strings.TrimSpace(wave.ReviewBackend)
	if reviewBackend == "" {
		reviewBackend = defaultParallelWaveReviewBackend
	}
	reviewModel := strings.TrimSpace(wave.ReviewModel)
	if reviewModel == "" {
		reviewModel = defaultParallelWaveReviewModel
	}
	reviewReasoning := strings.TrimSpace(wave.ReviewReasoning)
	if reviewReasoning == "" {
		reviewReasoning = defaultParallelWaveReviewReasoning
	}
	command := execCommand(resolveFixerPythonExecutable(os.Environ()), launcherScript, "launch-wave-reviewer",
		"--cwd", projectCWD,
		"--session-id", strconv.Itoa(review.LocalSessionID),
		"--wave-id", strconv.Itoa(wave.Id),
		"--backend", reviewBackend,
		"--model", reviewModel,
		"--reasoning", reviewReasoning,
		"--headless-log-path", logPath,
		"--worker-metadata-path", metadataPath,
	)
	commandEnv, err := resolveRuntimeLaunchEnv(projectCWD, os.Environ())
	if err != nil {
		return fmt.Errorf("resolve reviewer launch environment: %w", err)
	}
	command.Env = replaceEnvSliceValue(commandEnv, "FIXER_REVIEW_WAVE_ID", strconv.Itoa(wave.Id))
	command.Stdout = logHandle
	command.Stderr = logHandle
	if err := command.Start(); err != nil {
		return err
	}
	waitErrCh := make(chan error, 1)
	go func() {
		waitErrCh <- command.Wait()
	}()

	launcherPID := 0
	if command.Process != nil {
		launcherPID = command.Process.Pid
	}

	select {
	case <-waitErrCh:
		// ignore launcher exit errors
	case <-time.After(explicitLauncherExitGracePeriod):
	case <-ctx.Done():
		return ctx.Err()
	}

	workerPID := launcherPID
	if metadata, metadataErr := readExplicitLaunchWorkerMetadata(metadataPath); metadataErr == nil {
		workerPID = metadata.WorkerPID
	}
	if workerPID <= 0 {
		return fmt.Errorf("reviewer launcher did not expose a process pid")
	}
	if err := recordParallelWaveReviewProcess(authorizedProjectId, review.GlobalSessionID, workerPID, wave.OrchestrationEpoch, wave.Id); err != nil {
		_ = command.Process.Kill()
		return err
	}
	log.Printf("parallel_wave_reviewer project_id=%d wave_id=%d session_id=%d pid=%d visual=%t", authorizedProjectId, wave.Id, review.LocalSessionID, workerPID, parallelWaveReviewNeedsVisualVerification(wave))
	return nil
}

func ensureParallelWaveReviewer(ctx context.Context, wave NetrunnerWaveSnapshot) error {
	policy, err := normalizeParallelWaveReviewPolicy(wave.ReviewPolicy)
	if err != nil {
		return err
	}
	if policy != parallelWaveReviewPolicyAutomatic {
		return nil
	}
	if wave.FailurePolicyState != parallelWaveFailurePolicyPassed {
		return fmt.Errorf("wave %d failure policy is %q; reviewer launch requires %q", wave.Id, wave.FailurePolicyState, parallelWaveFailurePolicyPassed)
	}
	review, found, err := fetchParallelWaveReviewSession(wave.Id, authorizedProjectId)
	if err != nil {
		return err
	}
	if !found {
		review, err = createParallelWaveReviewSession(wave)
		if err != nil {
			return err
		}
	}
	if review.Status != "pending" {
		return updateParallelWaveSnapshotReview(&wave, authorizedProjectId)
	}
	process, processFound, err := latestWorkerProcessForSession(authorizedProjectId, review.GlobalSessionID)
	if err != nil {
		return err
	}
	if processFound {
		refreshed, refreshErr := refreshWorkerProcessSnapshot(authorizedProjectId, process)
		if refreshErr != nil {
			return refreshErr
		}
		if refreshed.Status == workerStatusRunning && refreshed.Alive {
			return nil
		}
		reason := refreshed.StopReason
		if strings.TrimSpace(reason) == "" {
			reason = fmt.Sprintf("reviewer worker process %d is no longer running", refreshed.ID)
		}
		if markErr := markParallelWaveReviewLaunchFailure(review.GlobalSessionID, wave.Id, reason); markErr != nil {
			return fmt.Errorf("%s; failed to persist reviewer failure: %v", reason, markErr)
		}
		return fmt.Errorf("%s", reason)
	}
	if err := launchParallelWaveReviewer(ctx, wave, review); err != nil {
		if markErr := markParallelWaveReviewLaunchFailure(review.GlobalSessionID, wave.Id, err.Error()); markErr != nil {
			return fmt.Errorf("%v; failed to persist reviewer launch failure: %v", err, markErr)
		}
		return err
	}
	return nil
}
