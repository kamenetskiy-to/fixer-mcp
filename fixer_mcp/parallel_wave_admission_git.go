package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
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
	parallelWaveWorkerStatusRetryWait     = "retry_wait"
	parallelWaveWorkerStatusRepairWait    = "repair_wait"
	parallelWaveWorkerStatusBlocked       = "blocked"
	defaultParallelWaveWorktreeRoot       = ".codex/netrunner_worktrees"
	defaultParallelWaveLaunchStartupWait  = 120
	maxParallelWaveLaunchStartupWait      = explicitLaunchMaxWait
	parallelWaveWaitFirstReviewReady      = "first_review_ready"
	parallelWaveWaitAllTerminal           = "all_terminal"
	parallelWaveCleanupStatusPending      = "pending"
	parallelWaveCleanupStatusCleaned      = "cleaned"
	parallelWaveCleanupStatusMissing      = "missing"
	parallelWaveCleanupStatusFailed       = "failed"
	parallelWaveReviewPolicyAutomatic     = "automatic"
	parallelWaveReviewPolicyManual        = "manual"
	defaultParallelWaveReviewPolicy       = parallelWaveReviewPolicyManual
	defaultParallelWaveReviewBackend      = "codex"
	defaultParallelWaveReviewModel        = "opencode-go/deepseek-v4-flash"
	defaultParallelWaveReviewReasoning    = "high"
)

var parallelWaveBranchPattern = regexp.MustCompile(`^fixer/wave-[1-9][0-9]*/session-[1-9][0-9]*$`)
var parallelWaveFoundationWriteScopePaths = []string{
	"fixer_mcp/main.go",
	"client_wires/fixer_wire.py",
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
	return normalizeParallelWaveAdmissionWorkersWithDependencies(workers, nil)
}

// normalizeParallelWaveAdmissionWorkersWithDependencies validates the scopes
// that may run concurrently in a wave. A parent and its descendant are
// intentionally allowed to overlap because the scheduler does not run the
// descendant until the parent branch has completed and been merged.
func normalizeParallelWaveAdmissionWorkersWithDependencies(workers []parallelWaveAdmissionWorker, dependencies []WaveDependency) ([]parallelWaveAdmissionWorker, error) {
	if len(workers) < 1 {
		return nil, fmt.Errorf("parallel wave admission requires at least one session")
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
			if writeScopesOverlap(existing.DeclaredWriteScope, normalizedScope) &&
				!parallelWaveSessionsHaveDependencyRelation(existing.SessionID, worker.SessionID, dependencies) {
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

func parallelWaveSessionsHaveDependencyRelation(leftSessionID, rightSessionID int, dependencies []WaveDependency) bool {
	if leftSessionID <= 0 || rightSessionID <= 0 || leftSessionID == rightSessionID {
		return false
	}

	parentsByChild := make(map[int][]int)
	for _, dependency := range dependencies {
		child := int(dependency.Child)
		for _, parent := range dependency.Parents {
			parentsByChild[child] = append(parentsByChild[child], int(parent))
		}
	}

	// Walk from each candidate child through all ancestors, so transitive DAG
	// relationships are treated as sequential as well.
	isAncestor := func(descendant, ancestor int) bool {
		visited := map[int]struct{}{descendant: {}}
		queue := []int{descendant}
		for len(queue) > 0 {
			current := queue[0]
			queue = queue[1:]
			for _, parent := range parentsByChild[current] {
				if parent == ancestor {
					return true
				}
				if _, seen := visited[parent]; seen {
					continue
				}
				visited[parent] = struct{}{}
				queue = append(queue, parent)
			}
		}
		return false
	}
	return isAncestor(leftSessionID, rightSessionID) || isAncestor(rightSessionID, leftSessionID)
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
	if nestedGitRoot, found := findParallelWaveNestedGitRoot(normalizedCWD); found {
		normalizedCWD = nestedGitRoot
	}
	commandArgs := append([]string{"-C", normalizedCWD}, args...)
	return gitCommandSpec{Name: "git", Args: commandArgs}, nil
}

func findParallelWaveNestedGitRoot(projectCWD string) (string, bool) {
	for current := projectCWD; ; current = filepath.Dir(current) {
		if _, err := os.Lstat(filepath.Join(current, ".git")); err == nil {
			return projectCWD, false
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}

	var candidates []string
	_ = filepath.Walk(projectCWD, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info == nil {
			return nil
		}
		if path == projectCWD {
			return nil
		}
		if info.Name() != ".git" {
			return nil
		}
		candidates = append(candidates, filepath.Dir(path))
		if info.IsDir() {
			return filepath.SkipDir
		}
		return nil
	})
	if len(candidates) == 0 {
		return "", false
	}

	sort.Slice(candidates, func(left, right int) bool {
		leftRelative, _ := filepath.Rel(projectCWD, candidates[left])
		rightRelative, _ := filepath.Rel(projectCWD, candidates[right])
		leftDepth := strings.Count(leftRelative, string(os.PathSeparator))
		rightDepth := strings.Count(rightRelative, string(os.PathSeparator))
		if leftDepth != rightDepth {
			return leftDepth < rightDepth
		}
		return candidates[left] < candidates[right]
	})
	normalizedRoot, err := normalizeProjectCWD(candidates[0])
	if err != nil {
		return "", false
	}
	return normalizedRoot, true
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

func gitMergeBranchCommand(worktreePath string, branchName string) (gitCommandSpec, error) {
	validatedBranchName, err := validateParallelWaveBranchName(branchName)
	if err != nil {
		return gitCommandSpec{}, err
	}
	return gitCommand(worktreePath, "merge", "--no-edit", validatedBranchName)
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
	if normalizedRoot != normalizedProjectCWD && !pathIsWithinDirectory(normalizedProjectCWD, normalizedRoot) {
		return "", "", fmt.Errorf("Git root must match or be nested within project cwd for parallel waves: registered %q, git root %q", normalizedProjectCWD, normalizedRoot)
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

func declaredWriteScopeContainsPath(scope []string, path string) bool {
	normalizedPath := filepath.ToSlash(filepath.Clean(path))
	for _, scopeEntry := range scope {
		normalizedScopeEntry := filepath.ToSlash(filepath.Clean(scopeEntry))
		if writeScopePathsOverlap(normalizedPath, normalizedScopeEntry) {
			return true
		}
	}
	return false
}

func canonicalizePath(p string) string {
	cleaned := filepath.Clean(p)
	if eval, err := filepath.EvalSymlinks(cleaned); err == nil {
		return filepath.Clean(eval)
	}
	return cleaned
}

func validateWorktreeIsolation(projectCWD string, rawWorktreePath string) error {
	normalizedProjectCWD, err := normalizeProjectCWD(projectCWD)
	if err != nil {
		return fmt.Errorf("failed to normalize project cwd: %w", err)
	}
	resolvedPath, err := resolveParallelWaveWorktreePath(normalizedProjectCWD, rawWorktreePath)
	if err != nil {
		return fmt.Errorf("failed to resolve worktree path: %w", err)
	}
	canonicalWorktreePath := canonicalizePath(resolvedPath)
	canonicalProjectCWD := canonicalizePath(normalizedProjectCWD)

	if canonicalWorktreePath == canonicalProjectCWD {
		return fmt.Errorf("worktree path cannot be project root: %s", canonicalWorktreePath)
	}

	info, err := os.Stat(canonicalWorktreePath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("worktree directory missing: %s", canonicalWorktreePath)
		}
		return fmt.Errorf("failed to stat worktree directory %s: %w", canonicalWorktreePath, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("worktree path is not a directory: %s", canonicalWorktreePath)
	}

	gitMetaPath := filepath.Join(canonicalWorktreePath, ".git")
	if _, err := os.Lstat(gitMetaPath); err != nil {
		return fmt.Errorf("worktree directory missing .git metadata: %s", canonicalWorktreePath)
	}

	listSpec, err := gitWorktreeListCommand(canonicalProjectCWD)
	if err != nil {
		return fmt.Errorf("failed to build git worktree list command: %w", err)
	}
	listOutput, err := runGitCommandSpec(listSpec)
	if err != nil {
		return fmt.Errorf("failed to list git worktrees: %w", err)
	}
	rawListed := parseGitWorktreeListPorcelain(listOutput)
	canonicalListed := make(map[string]struct{}, len(rawListed))
	for p := range rawListed {
		canonicalListed[canonicalizePath(p)] = struct{}{}
	}

	if _, listed := canonicalListed[canonicalWorktreePath]; !listed {
		return fmt.Errorf("directory %s is not a registered git worktree", canonicalWorktreePath)
	}

	cmd := execCommand("git", "-C", canonicalWorktreePath, "rev-parse", "--show-toplevel")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to run git rev-parse in worktree %s: %v", canonicalWorktreePath, err)
	}
	topLevel := canonicalizePath(strings.TrimSpace(string(output)))
	if topLevel == canonicalProjectCWD {
		return fmt.Errorf("worktree path %s resolved to main project root %s", canonicalWorktreePath, canonicalProjectCWD)
	}
	if topLevel != canonicalWorktreePath {
		return fmt.Errorf("worktree top level %s does not match expected worktree path %s", topLevel, canonicalWorktreePath)
	}

	return nil
}

func isDirectoryEmpty(dirPath string) (bool, error) {
	f, err := os.Open(dirPath)
	if err != nil {
		return false, err
	}
	names, readErr := f.Readdirnames(1)
	closeErr := f.Close()
	if readErr != nil && readErr != io.EOF {
		return false, readErr
	}
	if closeErr != nil {
		return false, closeErr
	}
	return len(names) == 0, nil
}

func safelyQuarantineUnregisteredPath(projectCWD string, worktreePath string) (string, error) {
	resolvedPath, err := resolveParallelWaveWorktreePath(projectCWD, worktreePath)
	if err != nil {
		return "", err
	}
	cleanedPath := filepath.Clean(resolvedPath)
	info, err := os.Stat(cleanedPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("failed to stat worktree path %s: %w", cleanedPath, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("worktree path is not a directory: %s", cleanedPath)
	}

	empty, err := isDirectoryEmpty(cleanedPath)
	if err != nil {
		return "", fmt.Errorf("failed to check if directory %s is empty: %w", cleanedPath, err)
	}
	if empty {
		if err := os.Remove(cleanedPath); err != nil {
			return "", fmt.Errorf("failed to remove empty unregistered directory %s: %w", cleanedPath, err)
		}
		return "", nil
	}

	quarantinePath := fmt.Sprintf("%s.quarantine.%d", cleanedPath, time.Now().UnixNano())
	if err := os.Rename(cleanedPath, quarantinePath); err != nil {
		return "", fmt.Errorf("failed to quarantine unregistered non-empty directory %s -> %s: %w", cleanedPath, quarantinePath, err)
	}
	return quarantinePath, nil
}

func verifyAndIntegrateParentHandoffs(projectCWD string, wave NetrunnerWaveSnapshot, worker NetrunnerWaveWorkerSnapshot, worktreePath string) error {
	parentSessionIDs := make(map[int]struct{})
	for _, dependency := range wave.Dependencies {
		if dependency.Child == int64(worker.SessionId) {
			for _, parentID := range dependency.Parents {
				parentSessionIDs[int(parentID)] = struct{}{}
			}
		}
	}
	if len(parentSessionIDs) == 0 {
		return nil
	}

	parentWorkers := make([]NetrunnerWaveWorkerSnapshot, 0, len(parentSessionIDs))
	for _, candidate := range wave.Workers {
		if _, isParent := parentSessionIDs[candidate.SessionId]; isParent {
			parentWorkers = append(parentWorkers, candidate)
		}
	}
	if len(parentWorkers) == 0 {
		return nil
	}

	return mergeParallelWaveParentBranches(projectCWD, worktreePath, parentWorkers)
}

func ensureGovernedRepairWorktreeReady(projectCWD string, wave NetrunnerWaveSnapshot, worker NetrunnerWaveWorkerSnapshot) (string, error) {
	normalizedProjectCWD, err := normalizeProjectCWD(projectCWD)
	if err != nil {
		return "", err
	}
	worktreePath, err := resolveParallelWaveWorktreePath(normalizedProjectCWD, worker.WorktreePath)
	if err != nil {
		return "", err
	}
	branchName, err := validateParallelWaveBranchName(worker.BranchName)
	if err != nil {
		return "", err
	}

	validationErr := validateWorktreeIsolation(normalizedProjectCWD, worktreePath)
	if validationErr == nil {
		spec, err := gitCurrentBranchCommand(worktreePath)
		if err == nil {
			curBranch, _ := runGitCommandSpec(spec)
			if strings.TrimSpace(curBranch) == branchName {
				if err := verifyAndIntegrateParentHandoffs(normalizedProjectCWD, wave, worker, worktreePath); err != nil {
					return "", fmt.Errorf("parent handoff integration failed for valid worktree: %w", err)
				}
				return worktreePath, nil
			}
		}
	}

	quarantinedPath, err := safelyQuarantineUnregisteredPath(normalizedProjectCWD, worktreePath)
	if err != nil {
		return "", fmt.Errorf("failed to quarantine existing worktree directory: %w", err)
	}
	if quarantinedPath != "" {
		log.Printf("info: quarantined broken worktree path %s to %s", worktreePath, quarantinedPath)
	}

	pruneSpec, _ := gitWorktreePruneCommand(normalizedProjectCWD)
	_, _ = runGitCommandSpec(pruneSpec)

	if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
		return "", fmt.Errorf("failed to prepare worktree parent dir: %w", err)
	}

	branchExists, err := gitBranchExists(normalizedProjectCWD, branchName)
	if err != nil {
		return "", fmt.Errorf("failed to check if branch %s exists: %w", branchName, err)
	}

	baseSHA := strings.TrimSpace(worker.BaseSha)
	if baseSHA == "" {
		baseSHA = strings.TrimSpace(wave.BaseSha)
	}

	if branchExists {
		addSpec, err := gitCommand(normalizedProjectCWD, "worktree", "add", worktreePath, branchName)
		if err != nil {
			return "", fmt.Errorf("failed to build worktree add spec for existing branch: %w", err)
		}
		if _, err := runGitCommandSpec(addSpec); err != nil {
			return "", fmt.Errorf("failed to add worktree on existing branch %s: %w", branchName, err)
		}
	} else {
		addSpec, err := gitWorktreeAddCommand(normalizedProjectCWD, worktreePath, branchName, baseSHA)
		if err != nil {
			return "", fmt.Errorf("failed to build worktree add spec: %w", err)
		}
		if _, err := runGitCommandSpec(addSpec); err != nil {
			return "", fmt.Errorf("failed to create worktree %s on branch %s: %w", worktreePath, branchName, err)
		}
	}

	if err := verifyAndIntegrateParentHandoffs(normalizedProjectCWD, wave, worker, worktreePath); err != nil {
		return "", fmt.Errorf("failed to integrate parent handoffs during worktree repair: %w", err)
	}

	if err := validateWorktreeIsolation(normalizedProjectCWD, worktreePath); err != nil {
		return "", fmt.Errorf("isolation proof failed after worktree recreation: %w", err)
	}

	return worktreePath, nil
}
