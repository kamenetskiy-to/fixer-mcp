package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	parallelWaveStatusCreated             = "created"
	parallelWaveStatusLaunching           = "launching"
	parallelWaveStatusRunning             = "running"
	parallelWaveStatusReviewReady         = "review_ready"
	parallelWaveStatusPartiallyFailed     = "partially_failed"
	parallelWaveStatusStopping            = "stopping"
	parallelWaveStatusStopped             = "stopped"
	parallelWaveStatusCompleted           = "completed"
	parallelWaveStatusFailed              = "failed"
	parallelWaveStatusCleaned             = "cleaned"
	parallelWaveWorkerStatusCreated       = "created"
	parallelWaveWorkerStatusWorktreeReady = "worktree_ready"
	parallelWaveWorkerStatusLaunching     = "launching"
	parallelWaveWorkerStatusRunning       = "running"
	parallelWaveWorkerStatusReviewReady   = "review_ready"
	parallelWaveWorkerStatusCompleted     = "completed"
	parallelWaveWorkerStatusFailed        = "failed"
	parallelWaveWorkerStatusStopped       = "stopped"
	parallelWaveWorkerStatusStaleEpoch    = "stale_epoch"
	parallelWaveWorkerStatusCleaned       = "cleaned"
	defaultParallelWaveWorktreeRoot       = ".codex/netrunner_worktrees"
	defaultParallelWaveLaunchStartupWait  = 120
	maxParallelWaveLaunchStartupWait      = explicitLaunchMaxWait
	parallelWaveWaitFirstReviewReady      = "first_review_ready"
	parallelWaveWaitAllTerminal           = "all_terminal"
	parallelWaveCleanupStatusPending      = "pending"
	parallelWaveCleanupStatusCleaned      = "cleaned"
	parallelWaveCleanupStatusMissing      = "missing"
	parallelWaveCleanupStatusFailed       = "failed"
)

var parallelWaveBranchPattern = regexp.MustCompile(`^fixer/wave-[1-9][0-9]*/session-[1-9][0-9]*$`)
var parallelWaveFoundationWriteScopePaths = []string{
	"fixer_mcp/main.go",
	"client_wires/fixer_wire.py",
	"client_wires/fixer_autonomous.py",
	"AGENTS.md",
	".codex",
	".mcp.json",
	"mcp_config.json",
	"fixer_mcp/mcp_config.json",
	"fixer_mcp/fixer.db",
	"fixer_mcp/fixer.db-shm",
	"fixer_mcp/fixer.db-wal",
	"fixer_mcp/fixer_genui.db",
}

var parallelWaveFoundationWriteScopePrefixes = []string{
	".codex/",
	"skills/",
	".agents/plugins/",
}

type parallelWaveAdmissionWorker struct {
	SessionID          int
	DeclaredWriteScope []string
}

type parallelWaveSessionCandidate struct {
	LocalSessionID     int
	GlobalSessionID    int
	DeclaredWriteScope []string
}

type gitCommandSpec struct {
	Name string
	Args []string
}

func waveDeclaredWriteScopeFoundationMatch(entry string) (string, bool) {
	normalized, err := normalizeWriteScopePath(entry)
	if err != nil {
		return "", false
	}
	for _, forbidden := range parallelWaveFoundationWriteScopePaths {
		if writeScopePathsOverlap(normalized, forbidden) {
			return forbidden, true
		}
	}
	for _, forbiddenPrefix := range parallelWaveFoundationWriteScopePrefixes {
		forbiddenRoot := strings.TrimSuffix(forbiddenPrefix, "/")
		if normalized == forbiddenRoot || strings.HasPrefix(normalized, forbiddenPrefix) {
			return forbiddenRoot, true
		}
	}
	base := filepath.Base(normalized)
	if strings.HasSuffix(base, ".db") || strings.HasSuffix(base, ".db-shm") || strings.HasSuffix(base, ".db-wal") {
		return base, true
	}
	return "", false
}

func containsParallelWaveFoundationWriteScope(scope []string) (string, bool) {
	for _, entry := range scope {
		if matched, ok := waveDeclaredWriteScopeFoundationMatch(entry); ok {
			return matched, true
		}
	}
	return "", false
}

func normalizeParallelWaveDeclaredWriteScope(raw []string) ([]string, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("parallel wave declared_write_scope must contain at least one non-broad project-relative path")
	}
	normalized, err := normalizeDeclaredWriteScope(raw)
	if err != nil {
		return nil, err
	}
	for _, entry := range normalized {
		if entry == defaultWriteScopePath {
			return nil, fmt.Errorf("parallel wave declared_write_scope cannot use broad %q scope", defaultWriteScopePath)
		}
	}
	for leftIndex := 0; leftIndex < len(normalized); leftIndex++ {
		for rightIndex := leftIndex + 1; rightIndex < len(normalized); rightIndex++ {
			if writeScopePathsOverlap(normalized[leftIndex], normalized[rightIndex]) {
				return nil, fmt.Errorf("parallel wave declared_write_scope entries overlap: %q and %q", normalized[leftIndex], normalized[rightIndex])
			}
		}
	}
	if matched, ok := containsParallelWaveFoundationWriteScope(normalized); ok {
		return nil, fmt.Errorf("parallel wave declared_write_scope touches foundation/bootstrap path %q", matched)
	}
	return normalized, nil
}

func normalizeParallelWaveAdmissionWorkers(workers []parallelWaveAdmissionWorker) ([]parallelWaveAdmissionWorker, error) {
	if len(workers) < 2 {
		return nil, fmt.Errorf("parallel wave admission requires at least two sessions")
	}
	normalizedWorkers := make([]parallelWaveAdmissionWorker, 0, len(workers))
	seenSessions := make(map[int]struct{}, len(workers))
	for _, worker := range workers {
		if worker.SessionID <= 0 {
			return nil, fmt.Errorf("parallel wave session ids must be positive")
		}
		if _, exists := seenSessions[worker.SessionID]; exists {
			return nil, fmt.Errorf("parallel wave session id %d is duplicated", worker.SessionID)
		}
		seenSessions[worker.SessionID] = struct{}{}

		normalizedScope, err := normalizeParallelWaveDeclaredWriteScope(worker.DeclaredWriteScope)
		if err != nil {
			return nil, fmt.Errorf("session %d: %w", worker.SessionID, err)
		}
		for _, existing := range normalizedWorkers {
			if writeScopesOverlap(existing.DeclaredWriteScope, normalizedScope) {
				return nil, fmt.Errorf("parallel wave sessions %d and %d have overlapping declared write scopes", existing.SessionID, worker.SessionID)
			}
		}
		normalizedWorkers = append(normalizedWorkers, parallelWaveAdmissionWorker{
			SessionID:          worker.SessionID,
			DeclaredWriteScope: normalizedScope,
		})
	}
	return normalizedWorkers, nil
}

func parallelWaveBranchName(waveID, sessionID int) (string, error) {
	if waveID <= 0 || sessionID <= 0 {
		return "", fmt.Errorf("wave_id and session_id must be positive")
	}
	return fmt.Sprintf("fixer/wave-%d/session-%d", waveID, sessionID), nil
}

func validateParallelWaveBranchName(raw string) (string, error) {
	branchName := strings.TrimSpace(raw)
	if !parallelWaveBranchPattern.MatchString(branchName) {
		return "", fmt.Errorf("parallel wave branch_name must match fixer/wave-<wave_id>/session-<session_id>")
	}
	return branchName, nil
}

func normalizeParallelWaveWorktreeRoot(raw string) (string, error) {
	root := strings.TrimSpace(raw)
	if root == "" {
		root = defaultParallelWaveWorktreeRoot
	}
	cleaned := filepath.ToSlash(filepath.Clean(root))
	if cleaned == "." {
		return "", fmt.Errorf("parallel wave worktree_root must not be the project root")
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("parallel wave worktree_root must stay within the project root or be absolute: %q", raw)
	}
	return cleaned, nil
}

func parallelWaveWorktreePath(worktreeRoot string, waveID, sessionID int) (string, error) {
	if waveID <= 0 || sessionID <= 0 {
		return "", fmt.Errorf("wave_id and session_id must be positive")
	}
	root, err := normalizeParallelWaveWorktreeRoot(worktreeRoot)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(filepath.Join(root, fmt.Sprintf("wave-%d", waveID), fmt.Sprintf("session-%d", sessionID))), nil
}

func resolveParallelWaveWorktreePath(projectCWD string, rawPath string) (string, error) {
	trimmed := strings.TrimSpace(rawPath)
	if trimmed == "" {
		return "", fmt.Errorf("parallel wave worker worktree_path is required")
	}
	if filepath.IsAbs(trimmed) {
		return filepath.Clean(trimmed), nil
	}
	cleaned := filepath.Clean(trimmed)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("parallel wave worker worktree_path must stay within the project root")
	}
	return filepath.Join(projectCWD, cleaned), nil
}

func gitCommand(projectCWD string, args ...string) (gitCommandSpec, error) {
	normalizedCWD, err := normalizeProjectCWD(projectCWD)
	if err != nil {
		return gitCommandSpec{}, err
	}
	commandArgs := append([]string{"-C", normalizedCWD}, args...)
	return gitCommandSpec{Name: "git", Args: commandArgs}, nil
}

func gitRootCommand(projectCWD string) (gitCommandSpec, error) {
	return gitCommand(projectCWD, "rev-parse", "--show-toplevel")
}

func gitTrackedCleanStatusCommand(projectCWD string) (gitCommandSpec, error) {
	return gitCommand(projectCWD, "status", "--porcelain=v1", "--untracked-files=no")
}

func gitBaseSHACommand(projectCWD, baseRef string) (gitCommandSpec, error) {
	ref := strings.TrimSpace(baseRef)
	if ref == "" {
		ref = "HEAD"
	}
	return gitCommand(projectCWD, "rev-parse", "--verify", ref+"^{commit}")
}

func gitCurrentBranchCommand(projectCWD string) (gitCommandSpec, error) {
	return gitCommand(projectCWD, "symbolic-ref", "--quiet", "--short", "HEAD")
}

func gitBranchExists(projectCWD string, branchName string) (bool, error) {
	branchName, err := validateParallelWaveBranchName(branchName)
	if err != nil {
		return false, err
	}
	spec, err := gitCommand(projectCWD, "rev-parse", "--verify", "--quiet", "refs/heads/"+branchName)
	if err != nil {
		return false, err
	}
	output, err := execCommand(spec.Name, spec.Args...).CombinedOutput()
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	detail := strings.TrimSpace(string(output))
	if detail == "" {
		detail = err.Error()
	}
	return false, fmt.Errorf("%s %s failed: %s", spec.Name, strings.Join(spec.Args, " "), detail)
}

func gitWorktreeAddCommand(projectCWD string, worktreePath string, branchName string, baseSHA string) (gitCommandSpec, error) {
	branchName, err := validateParallelWaveBranchName(branchName)
	if err != nil {
		return gitCommandSpec{}, err
	}
	trimmedBaseSHA := strings.TrimSpace(baseSHA)
	if trimmedBaseSHA == "" {
		return gitCommandSpec{}, fmt.Errorf("base_sha is required for parallel wave worktree creation")
	}
	return gitCommand(projectCWD, "worktree", "add", "-b", branchName, worktreePath, trimmedBaseSHA)
}

func gitWorktreeRemoveCommand(projectCWD string, worktreePath string, force bool) (gitCommandSpec, error) {
	if strings.TrimSpace(worktreePath) == "" {
		return gitCommandSpec{}, fmt.Errorf("worktree_path is required for parallel wave rollback")
	}
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, worktreePath)
	return gitCommand(projectCWD, args...)
}

func gitWorktreeListCommand(projectCWD string) (gitCommandSpec, error) {
	return gitCommand(projectCWD, "worktree", "list", "--porcelain")
}

func gitWorktreePruneCommand(projectCWD string) (gitCommandSpec, error) {
	return gitCommand(projectCWD, "worktree", "prune")
}

func runGitCommandSpec(spec gitCommandSpec) (string, error) {
	output, err := execCommand(spec.Name, spec.Args...).CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(output))
		if detail == "" {
			detail = err.Error()
		}
		return "", fmt.Errorf("%s %s failed: %s", spec.Name, strings.Join(spec.Args, " "), detail)
	}
	return strings.TrimSpace(string(output)), nil
}

func runGitCommandSpecBytes(spec gitCommandSpec, allowedExitCodes map[int]struct{}) ([]byte, error) {
	output, err := execCommand(spec.Name, spec.Args...).CombinedOutput()
	if err == nil {
		return output, nil
	}
	var exitErr *exec.ExitError
	if allowedExitCodes != nil && errors.As(err, &exitErr) {
		if _, allowed := allowedExitCodes[exitErr.ExitCode()]; allowed {
			return output, nil
		}
	}
	detail := strings.TrimSpace(string(output))
	if detail == "" {
		detail = err.Error()
	}
	return nil, fmt.Errorf("%s %s failed: %s", spec.Name, strings.Join(spec.Args, " "), detail)
}

func verifyParallelWaveGitBase(projectCWD string, baseRef string) (baseSHA string, baseBranch string, err error) {
	normalizedProjectCWD, err := normalizeProjectCWD(projectCWD)
	if err != nil {
		return "", "", err
	}

	rootSpec, err := gitRootCommand(normalizedProjectCWD)
	if err != nil {
		return "", "", err
	}
	root, err := runGitCommandSpec(rootSpec)
	if err != nil {
		return "", "", fmt.Errorf("project cwd is not a Git repository: %w", err)
	}
	normalizedRoot, err := normalizeProjectCWD(root)
	if err != nil {
		return "", "", fmt.Errorf("failed to normalize Git root: %w", err)
	}
	if normalizedRoot != normalizedProjectCWD {
		return "", "", fmt.Errorf("project cwd must match Git root for parallel waves: registered %q, git root %q", normalizedProjectCWD, normalizedRoot)
	}

	statusSpec, err := gitTrackedCleanStatusCommand(normalizedProjectCWD)
	if err != nil {
		return "", "", err
	}
	statusOutput, err := runGitCommandSpec(statusSpec)
	if err != nil {
		return "", "", fmt.Errorf("failed to inspect Git status: %w", err)
	}
	if strings.TrimSpace(statusOutput) != "" {
		return "", "", fmt.Errorf("tracked working tree must be clean before creating a parallel wave")
	}

	baseSpec, err := gitBaseSHACommand(normalizedProjectCWD, baseRef)
	if err != nil {
		return "", "", err
	}
	baseSHA, err = runGitCommandSpec(baseSpec)
	if err != nil {
		return "", "", fmt.Errorf("failed to resolve base_ref: %w", err)
	}

	branchSpec, err := gitCurrentBranchCommand(normalizedProjectCWD)
	if err != nil {
		return "", "", err
	}
	baseBranch, _ = runGitCommandSpec(branchSpec)
	return baseSHA, baseBranch, nil
}
