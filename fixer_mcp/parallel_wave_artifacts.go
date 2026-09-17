package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

func gitCommandInWorktree(worktreePath string, args ...string) (string, error) {
	spec, err := gitCommand(worktreePath, args...)
	if err != nil {
		return "", err
	}
	return runGitCommandSpec(spec)
}

func gitCommandInWorktreeAllowExitCodes(worktreePath string, allowedExitCodes map[int]struct{}, args ...string) (string, error) {
	spec, err := gitCommand(worktreePath, args...)
	if err != nil {
		return "", err
	}
	output, err := execCommand(spec.Name, spec.Args...).CombinedOutput()
	if err == nil {
		return strings.TrimSpace(string(output)), nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if _, allowed := allowedExitCodes[exitErr.ExitCode()]; allowed {
			return strings.TrimSpace(string(output)), nil
		}
	}
	detail := strings.TrimSpace(string(output))
	if detail == "" {
		detail = err.Error()
	}
	return "", fmt.Errorf("%s %s failed: %s", spec.Name, strings.Join(spec.Args, " "), detail)
}

func gitCommandInWorktreeBytes(worktreePath string, args ...string) ([]byte, error) {
	spec, err := gitCommand(worktreePath, args...)
	if err != nil {
		return nil, err
	}
	return runGitCommandSpecBytes(spec, nil)
}

func gitCommandInWorktreeBytesAllowExitCodes(worktreePath string, allowedExitCodes map[int]struct{}, args ...string) ([]byte, error) {
	spec, err := gitCommand(worktreePath, args...)
	if err != nil {
		return nil, err
	}
	return runGitCommandSpecBytes(spec, allowedExitCodes)
}

func splitGitPathLines(raw string) []string {
	paths := []string{}
	if strings.Contains(raw, "\x00") {
		for _, record := range strings.Split(raw, "\x00") {
			if record == "" {
				continue
			}
			paths = append(paths, filepath.ToSlash(record))
		}
		return paths
	}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "" {
			continue
		}
		paths = append(paths, filepath.ToSlash(line))
	}
	return paths
}

func mergeGitChangedPaths(groups ...[]string) []string {
	seen := map[string]struct{}{}
	paths := []string{}
	for _, group := range groups {
		for _, path := range group {
			normalized := filepath.ToSlash(path)
			if normalized == "" {
				continue
			}
			if _, exists := seen[normalized]; exists {
				continue
			}
			seen[normalized] = struct{}{}
			paths = append(paths, normalized)
		}
	}
	sort.Strings(paths)
	return paths
}

func gitTreeGitlinks(raw []byte) map[string]string {
	gitlinks := make(map[string]string)
	for _, record := range strings.Split(string(raw), "\x00") {
		if record == "" {
			continue
		}
		tab := strings.IndexByte(record, '\t')
		if tab < 0 {
			continue
		}
		fields := strings.Fields(record[:tab])
		if len(fields) < 3 || fields[0] != "160000" {
			continue
		}
		path := filepath.ToSlash(record[tab+1:])
		if path != "" {
			gitlinks[path] = fields[2]
		}
	}
	return gitlinks
}

func validateChangedParallelWaveSubmodules(worktreePath string, baseSHA string, headSHA string) error {
	headTree, err := gitCommandInWorktreeBytes(worktreePath, "ls-tree", "-r", "-z", headSHA, "--")
	if err != nil {
		return fmt.Errorf("failed to inspect worker HEAD gitlinks: %w", err)
	}
	baseTree, err := gitCommandInWorktreeBytes(worktreePath, "ls-tree", "-r", "-z", baseSHA, "--")
	if err != nil {
		return fmt.Errorf("failed to inspect worker base gitlinks: %w", err)
	}
	headGitlinks := gitTreeGitlinks(headTree)
	baseGitlinks := gitTreeGitlinks(baseTree)
	for path, objectID := range headGitlinks {
		if objectID == baseGitlinks[path] {
			continue
		}
		submodulePath := filepath.Join(worktreePath, filepath.FromSlash(path))
		if _, err := os.Stat(submodulePath); err != nil {
			return fmt.Errorf("submodule gitlink %q points to %s but its worktree is unavailable: %w", path, objectID, err)
		}
		if _, err := gitCommandInWorktree(submodulePath, "cat-file", "-e", objectID+"^{commit}"); err != nil {
			return fmt.Errorf("submodule gitlink %q points to unreachable commit %s: %w", path, objectID, err)
		}
	}
	return nil
}

func parallelWavePatchArtifactPath(projectCWD string, waveID int, localSessionID int) (string, error) {
	if waveID <= 0 || localSessionID <= 0 {
		return "", fmt.Errorf("wave_id and session_id must be positive for patch artifact capture")
	}
	dir := filepath.Join(projectCWD, ".codex", "netrunner_wave_artifacts", fmt.Sprintf("wave-%d", waveID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("failed to prepare wave artifact dir: %v", err)
	}
	return filepath.Join(dir, fmt.Sprintf("session-%d.patch", localSessionID)), nil
}

func combineParallelWavePatchPayloads(payloads ...[]byte) []byte {
	combined := []byte{}
	for _, payload := range payloads {
		if len(payload) == 0 {
			continue
		}
		if len(combined) > 0 && !bytes.HasSuffix(combined, []byte("\n")) {
			combined = append(combined, '\n')
		}
		combined = append(combined, payload...)
	}
	if len(combined) == 0 {
		return combined
	}
	combined = bytes.TrimRight(combined, "\n")
	return append(combined, '\n')
}

func captureParallelWaveWorkerDiff(projectCWD string, wave NetrunnerWaveSnapshot, worker NetrunnerWaveWorkerSnapshot) (string, []string, string, string, error) {
	worktreePath, err := resolveParallelWaveWorktreePath(projectCWD, worker.WorktreePath)
	if err != nil {
		return "", nil, "", "", err
	}
	if err := validateWorktreeIsolation(projectCWD, worktreePath); err != nil {
		return "", nil, "", "", fmt.Errorf("worktree isolation check failed: %w", err)
	}
	info, statErr := os.Stat(worktreePath)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			return "", nil, "", "", fmt.Errorf("worker worktree missing: %s", worktreePath)
		}
		return "", nil, "", "", fmt.Errorf("failed to inspect worker worktree %s: %v", worktreePath, statErr)
	}
	if !info.IsDir() {
		return "", nil, "", "", fmt.Errorf("worker worktree is not a directory: %s", worktreePath)
	}

	baseSHA := strings.TrimSpace(worker.BaseSha)
	if baseSHA == "" {
		baseSHA = strings.TrimSpace(wave.BaseSha)
	}
	if baseSHA == "" {
		return "", nil, "", "", fmt.Errorf("worker base_sha is required for diff capture")
	}

	headSHA, err := gitCommandInWorktree(worktreePath, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return "", nil, "", "", fmt.Errorf("failed to capture head_sha: %w", err)
	}
	trackedNamesRaw, err := gitCommandInWorktreeBytes(worktreePath, "diff", "--name-only", "-z", baseSHA, "--")
	if err != nil {
		return "", nil, "", "", fmt.Errorf("failed to capture changed paths: %w", err)
	}
	untrackedNamesRaw, err := gitCommandInWorktreeBytes(worktreePath, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return "", nil, "", "", fmt.Errorf("failed to capture untracked paths: %w", err)
	}
	untrackedPaths := splitGitPathLines(string(untrackedNamesRaw))
	diffStat, err := gitCommandInWorktree(worktreePath, "diff", "--stat", baseSHA, "--")
	if err != nil {
		return "", nil, "", "", fmt.Errorf("failed to capture diff stat: %w", err)
	}
	patch, err := gitCommandInWorktreeBytes(worktreePath, "diff", "--binary", baseSHA, "--")
	if err != nil {
		return "", nil, "", "", fmt.Errorf("failed to capture patch: %w", err)
	}
	diffExitCodes := map[int]struct{}{0: {}, 1: {}}
	patchPayloads := [][]byte{patch}
	for _, untrackedPath := range untrackedPaths {
		untrackedStat, err := gitCommandInWorktreeAllowExitCodes(worktreePath, diffExitCodes, "diff", "--no-index", "--stat", "--", "/dev/null", untrackedPath)
		if err != nil {
			return "", nil, "", "", fmt.Errorf("failed to capture untracked diff stat for %s: %w", untrackedPath, err)
		}
		if strings.TrimSpace(untrackedStat) != "" {
			if strings.TrimSpace(diffStat) != "" {
				diffStat += "\n"
			}
			diffStat += strings.TrimSpace(untrackedStat)
		}
		untrackedPatch, err := gitCommandInWorktreeBytesAllowExitCodes(worktreePath, diffExitCodes, "diff", "--no-index", "--binary", "--", "/dev/null", untrackedPath)
		if err != nil {
			return "", nil, "", "", fmt.Errorf("failed to capture untracked patch for %s: %w", untrackedPath, err)
		}
		patchPayloads = append(patchPayloads, untrackedPatch)
	}
	patch = combineParallelWavePatchPayloads(patchPayloads...)

	changedPaths := mergeGitChangedPaths(splitGitPathLines(string(trackedNamesRaw)), untrackedPaths)

	patchPath, err := parallelWavePatchArtifactPath(projectCWD, wave.Id, worker.SessionId)
	if err != nil {
		return "", nil, "", "", err
	}
	if err := os.WriteFile(patchPath, patch, 0o644); err != nil {
		return "", nil, "", "", fmt.Errorf("failed to write patch artifact: %v", err)
	}

	return strings.TrimSpace(headSHA), changedPaths, patchPath, strings.TrimSpace(diffStat), nil
}
