package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func parseGitWorktreeListPorcelain(raw string) map[string]struct{} {
	paths := make(map[string]struct{})
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "worktree ") {
			continue
		}
		path := strings.TrimSpace(strings.TrimPrefix(trimmed, "worktree "))
		if path == "" {
			continue
		}
		paths[filepath.Clean(path)] = struct{}{}
	}
	return paths
}

func isFilesystemRootPath(path string) bool {
	cleaned := filepath.Clean(path)
	return filepath.Dir(cleaned) == cleaned
}

func pathIsWithinDirectory(parent string, child string) bool {
	normalizedParent := filepath.Clean(parent)
	normalizedChild := filepath.Clean(child)
	relative, err := filepath.Rel(normalizedParent, normalizedChild)
	if err != nil {
		return false
	}
	return relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator))
}

func resolveParallelWaveCleanupWorktreePath(projectCWD string, rawPath string) (string, error) {
	normalizedProjectCWD, err := normalizeProjectCWD(projectCWD)
	if err != nil {
		return "", err
	}
	trimmed := strings.TrimSpace(rawPath)
	if trimmed == "" {
		return "", fmt.Errorf("parallel wave cleanup refuses empty worktree_path")
	}

	isAbsolute := filepath.IsAbs(trimmed)
	resolvedPath := ""
	if isAbsolute {
		resolvedPath = filepath.Clean(trimmed)
	} else {
		cleaned := filepath.Clean(trimmed)
		if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(os.PathSeparator)) {
			return "", fmt.Errorf("parallel wave cleanup refuses project-relative worktree_path escaping project root: %q", rawPath)
		}
		resolvedPath = filepath.Clean(filepath.Join(normalizedProjectCWD, cleaned))
		if !pathIsWithinDirectory(normalizedProjectCWD, resolvedPath) {
			return "", fmt.Errorf("parallel wave cleanup refuses project-relative worktree_path outside project root: %q", rawPath)
		}
	}

	if isFilesystemRootPath(resolvedPath) {
		return "", fmt.Errorf("parallel wave cleanup refuses filesystem root worktree_path: %s", resolvedPath)
	}
	if filepath.Clean(resolvedPath) == filepath.Clean(normalizedProjectCWD) {
		return "", fmt.Errorf("parallel wave cleanup refuses project root worktree_path: %s", resolvedPath)
	}

	if evaluated, evalErr := filepath.EvalSymlinks(resolvedPath); evalErr == nil {
		evaluated = filepath.Clean(evaluated)
		if isFilesystemRootPath(evaluated) {
			return "", fmt.Errorf("parallel wave cleanup refuses worktree_path resolving to filesystem root: %s", resolvedPath)
		}
		if evaluated == filepath.Clean(normalizedProjectCWD) {
			return "", fmt.Errorf("parallel wave cleanup refuses worktree_path resolving to project root: %s", resolvedPath)
		}
		if !isAbsolute && !pathIsWithinDirectory(normalizedProjectCWD, evaluated) {
			return "", fmt.Errorf("parallel wave cleanup refuses project-relative worktree_path resolving outside project root: %s", resolvedPath)
		}
	}

	return resolvedPath, nil
}

func appendCleanupDiagnostic(existing string, diagnostic string) string {
	trimmedDiagnostic := strings.TrimSpace(diagnostic)
	if trimmedDiagnostic == "" {
		return strings.TrimSpace(existing)
	}
	trimmedExisting := strings.TrimSpace(existing)
	if trimmedExisting == "" {
		return trimmedDiagnostic
	}
	if strings.Contains(trimmedExisting, trimmedDiagnostic) {
		return trimmedExisting
	}
	return trimmedExisting + "; " + trimmedDiagnostic
}

func validateParallelWaveCleanupPreconditions(wave NetrunnerWaveSnapshot, projectID int) error {
	if len(wave.Workers) == 0 {
		return fmt.Errorf("wave %d has no workers to clean up", wave.Id)
	}
	for _, worker := range wave.Workers {
		if _, terminal := parallelWaveWorkerTerminalCondition(worker.Status); !terminal {
			return fmt.Errorf("worker %d is not terminal; cleanup requires terminal worker status, got %q", worker.SessionId, worker.Status)
		}
		if worker.WorkerProcessId <= 0 {
			continue
		}
		processRow, found, err := fetchWorkerProcessByID(worker.WorkerProcessId, projectID)
		if err != nil {
			return fmt.Errorf("failed to inspect worker %d process %d: %v", worker.SessionId, worker.WorkerProcessId, err)
		}
		if found && processRow.Status == workerStatusRunning && processRow.Alive {
			return fmt.Errorf("worker %d has alive running process %d; stop it before cleanup", worker.SessionId, processRow.PID)
		}
	}
	return nil
}

func updateParallelWaveWorkerCleanup(worker NetrunnerWaveWorkerSnapshot, projectID int, cleanupStatus string, diagnostic string, markCleaned bool) error {
	workerStatus := worker.Status
	if markCleaned {
		workerStatus = parallelWaveWorkerStatusCleaned
	}
	_, err := db.Exec(
		`UPDATE parallel_wave_worker
		 SET status = ?,
		     cleanup_status = ?,
		     failure_reason = ?,
		     cleaned_at = CASE WHEN ? = 1 THEN COALESCE(cleaned_at, CURRENT_TIMESTAMP) ELSE cleaned_at END,
		     updated_at = CURRENT_TIMESTAMP
		 WHERE id = ? AND project_id = ?`,
		workerStatus,
		cleanupStatus,
		appendCleanupDiagnostic(worker.FailureReason, diagnostic),
		boolToInt(markCleaned),
		worker.Id,
		projectID,
	)
	return err
}

func markParallelWaveCleanedIfReady(waveID int, projectID int) (bool, error) {
	wave, err := fetchNetrunnerWaveSnapshot(waveID, projectID)
	if err != nil {
		return false, err
	}
	if len(wave.Workers) == 0 {
		return false, nil
	}
	for _, worker := range wave.Workers {
		switch worker.CleanupStatus {
		case parallelWaveCleanupStatusCleaned, parallelWaveCleanupStatusMissing:
		default:
			return false, nil
		}
	}
	_, err = db.Exec(
		`UPDATE parallel_wave
		 SET status = ?,
		     completed_at = COALESCE(completed_at, CURRENT_TIMESTAMP),
		     updated_at = CURRENT_TIMESTAMP
		 WHERE id = ? AND project_id = ?`,
		parallelWaveStatusCleaned,
		waveID,
		projectID,
	)
	if err != nil {
		return false, err
	}
	return true, nil
}
