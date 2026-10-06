package main

import (
	"fmt"
	"strconv"
	"strings"
)

// Safe submodule-aware terminal worktree cleanup.
//
// `git worktree remove` refuses worktrees whose tree contains gitlinks
// ("working trees containing submodules cannot be moved or removed"), and on
// older Git builds even --force fails on initialized submodules. A raw
// `rm -rf` fallback is forbidden: submodule checkouts can carry unknown dirty
// data that must be preserved. This helper handles both cases safely:
//
//   - clean case: every submodule checkout is verified clean, then
//     deinitialized (deinit only touches submodule working trees inside the
//     doomed worktree) and the worktree is removed; a forced removal is used
//     only when the whole worktree was verified clean, so nothing unknown can
//     be lost;
//   - dirty case: unknown dirty submodule data is preserved and reported,
//     never deleted, regardless of the caller's force flag.

// parallelWaveSubmoduleEntry is one line of `git submodule status`.
type parallelWaveSubmoduleEntry struct {
	// State is the leading status rune: ' ' clean, '+' commit drift,
	// '-' uninitialized, 'U' conflict.
	State rune
	// Path is the submodule path relative to the inspected worktree.
	Path string
}

// inspectWorktreeSubmoduleEntries lists the (recursive) submodules of a
// terminal worker worktree. It reports found=false when the worktree has no
// submodules or the state cannot be inspected.
func inspectWorktreeSubmoduleEntries(worktreePath string) ([]parallelWaveSubmoduleEntry, bool) {
	outputBytes, err := gitCommandInWorktreeBytesAllowExitCodes(worktreePath, map[int]struct{}{0: {}}, "submodule", "status", "--recursive")
	if err != nil {
		return nil, false
	}
	entries := []parallelWaveSubmoduleEntry{}
	for _, line := range strings.Split(string(outputBytes), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		state := rune(' ')
		body := line
		if body[0] == '+' || body[0] == '-' || body[0] == 'U' {
			state = rune(body[0])
			body = body[1:]
		}
		fields := strings.Fields(body)
		if len(fields) < 2 {
			continue
		}
		entries = append(entries, parallelWaveSubmoduleEntry{State: state, Path: fields[1]})
	}
	if len(entries) == 0 {
		return nil, false
	}
	return entries, true
}

// worktreeStatusPorcelain returns the raw `git status --porcelain` output of
// the worktree and whether that inspection succeeded. The output is kept raw:
// the leading status column of every line must survive for classification.
func worktreeStatusPorcelain(worktreePath string) (string, bool) {
	output, err := gitCommandInWorktreeBytesAllowExitCodes(worktreePath, map[int]struct{}{0: {}, 1: {}}, "status", "--porcelain")
	if err != nil {
		return "", false
	}
	return string(output), true
}

// porcelainTargetPath extracts the affected path from one --porcelain line.
func porcelainTargetPath(line string) string {
	if len(line) < 4 {
		return ""
	}
	target := strings.TrimSpace(line[3:])
	if strings.HasPrefix(target, "\"") {
		if unquoted, err := strconv.Unquote(target); err == nil {
			target = unquoted
		}
	}
	if spaceIndex := strings.Index(target, " -> "); spaceIndex >= 0 {
		target = target[spaceIndex+4:]
	}
	return strings.TrimSpace(target)
}

// dirtyWorktreeSubmodulePaths classifies worktree dirt: paths inside submodule
// checkouts are unknown submodule data that must be preserved; everything else
// is ordinary superproject dirt governed by the caller's force flag.
func dirtyWorktreeSubmodulePaths(worktreePath string, entries []parallelWaveSubmoduleEntry) (dirtySubmodules []string, superDirty bool, inspected bool) {
	seen := map[string]struct{}{}
	markDirty := func(path string) {
		if _, exists := seen[path]; !exists {
			seen[path] = struct{}{}
			dirtySubmodules = append(dirtySubmodules, path)
		}
	}
	for _, entry := range entries {
		if entry.State == '+' || entry.State == 'U' {
			// The checkout does not match the recorded gitlink or is in
			// conflict: that is unknown data, preserve it.
			markDirty(entry.Path)
		}
	}

	output, ok := worktreeStatusPorcelain(worktreePath)
	if !ok {
		// Without a successful dirt inspection nothing may be deleted
		// blindly: report the submodules as unverifiable (preserved) and the
		// worktree as uninspected.
		for _, entry := range entries {
			markDirty(entry.Path)
		}
		return dirtySubmodules, false, false
	}

	submodulePaths := make([]string, 0, len(entries))
	for _, entry := range entries {
		submodulePaths = append(submodulePaths, entry.Path)
	}
	for _, line := range strings.Split(output, "\n") {
		target := porcelainTargetPath(line)
		if target == "" {
			continue
		}
		withinSubmodule := false
		for _, submodulePath := range submodulePaths {
			if target == submodulePath || strings.HasPrefix(target, submodulePath+"/") {
				markDirty(submodulePath)
				withinSubmodule = true
				break
			}
		}
		if !withinSubmodule {
			superDirty = true
		}
	}
	return dirtySubmodules, superDirty, true
}

// removeTerminalWorktreeSafely removes one terminal worker worktree with
// submodule awareness. It reports removed=false with a diagnostic (and no
// error) when dirty submodule data was preserved instead of deleted.
func removeTerminalWorktreeSafely(projectCWD string, worktreePath string, force bool) (removed bool, preserveDiagnostic string, err error) {
	removeOnce := func(useForce bool) error {
		spec, removeErr := gitWorktreeRemoveCommand(projectCWD, worktreePath, useForce)
		if removeErr != nil {
			return removeErr
		}
		_, removeErr = runGitCommandSpec(spec)
		return removeErr
	}

	entries, hasSubmodules := inspectWorktreeSubmoduleEntries(worktreePath)
	if !hasSubmodules {
		// Plain worktrees keep the exact pre-existing behavior: the caller's
		// force flag governs ordinary dirt.
		if err := removeOnce(force); err != nil {
			return false, "", err
		}
		return true, "", nil
	}

	dirtySubmodules, superDirty, inspected := dirtyWorktreeSubmodulePaths(worktreePath, entries)
	if len(dirtySubmodules) > 0 {
		preview := dirtySubmodules
		if len(preview) > 3 {
			preview = preview[:3]
		}
		verdict := "dirty submodule data preserved (not deleted)"
		if !inspected {
			verdict = "submodule dirt could not be verified clean; preserved (not deleted)"
		}
		return false, fmt.Sprintf("%s in %s: %s", verdict, worktreePath, strings.Join(preview, ", ")), nil
	}

	// Ordinary superproject dirt without a forced-removal mandate keeps the
	// exact pre-existing behavior (and must not be mutated by a deinit).
	if superDirty && !force {
		if err := removeOnce(false); err != nil {
			return false, "", err
		}
		return true, "", nil
	}

	// Submodule checkouts are verified clean: deinitializing them only clears
	// submodule working trees inside the doomed worktree and never touches the
	// project's main checkout.
	if _, deinitErr := gitCommandInWorktree(worktreePath, "submodule", "deinit", "--all", "--force"); deinitErr != nil {
		return false, "", fmt.Errorf("submodule deinit failed before worktree removal: %v", deinitErr)
	}

	if err := removeOnce(force); err == nil {
		return true, "", nil
	} else if superDirty {
		// The caller explicitly authorized forced removal of ordinary dirt; the
		// remaining failure is not a data-preservation case.
		return false, "", err
	}
	// Everything (submodules and the worktree itself) is verified clean while
	// Git still refuses a non-forced removal of a tree that contains gitlinks:
	// a forced removal here cannot lose any unknown data.
	if err := removeOnce(true); err != nil {
		return false, "", err
	}
	return true, "", nil
}
