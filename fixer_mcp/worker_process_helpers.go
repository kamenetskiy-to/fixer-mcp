package main

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type workerProcessSnapshot struct {
	ID           int    `json:"id"`
	SessionID    int    `json:"session_id"`
	PID          int    `json:"pid"`
	LaunchEpoch  int    `json:"launch_epoch"`
	LaunchOrigin string `json:"launch_origin,omitempty"`
	Status       string `json:"status"`
	StartedAt    string `json:"started_at"`
	UpdatedAt    string `json:"updated_at"`
	StoppedAt    string `json:"stopped_at,omitempty"`
	Alive        bool   `json:"alive"`
	StopReason   string `json:"stop_reason,omitempty"`
}

type workerProcessExitDiagnostic struct {
	WorkerProcessID  int    `json:"worker_process_id"`
	PID              int    `json:"pid"`
	ProcessStatus    string `json:"process_status"`
	StopReason       string `json:"stop_reason"`
	Alive            bool   `json:"alive"`
	HeadlessLogPath  string `json:"headless_log_path"`
	LauncherLogPath  string `json:"launcher_log_path"`
	HeadlessLogMtime string `json:"headless_log_mtime"`
}

func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	if runtime.GOOS == "windows" {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if err := process.Signal(syscall.Signal(0)); err != nil {
		return !errors.Is(err, os.ErrProcessDone) && !strings.Contains(strings.ToLower(err.Error()), "finished")
	}
	if runtime.GOOS != "windows" {
		statOut, err := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
		if err == nil {
			if strings.Contains(strings.TrimSpace(string(statOut)), "Z") {
				return false
			}
		}
	}
	return true
}

func latestMatchingFile(paths []string) string {
	type candidate struct {
		path    string
		modTime time.Time
	}
	candidates := []candidate{}
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		candidates = append(candidates, candidate{path: path, modTime: info.ModTime()})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].modTime.Equal(candidates[j].modTime) {
			return candidates[i].path < candidates[j].path
		}
		return candidates[i].modTime.After(candidates[j].modTime)
	})
	if len(candidates) == 0 {
		return ""
	}
	return candidates[0].path
}

func latestSessionLauncherLogPath(projectCWD string, sessionID int) string {
	pattern := filepath.Join(projectCWD, ".codex", "headless_netrunner_logs", fmt.Sprintf("session-%d-launcher-*.log", sessionID))
	paths, err := filepath.Glob(pattern)
	if err != nil {
		return ""
	}
	return latestMatchingFile(paths)
}

func latestSessionHeadlessLogPath(projectCWD string, sessionID int) string {
	pattern := filepath.Join(projectCWD, ".codex", "headless_netrunner_logs", fmt.Sprintf("session-%d-*.log", sessionID))
	paths, err := filepath.Glob(pattern)
	if err != nil {
		return ""
	}
	filtered := make([]string, 0, len(paths))
	for _, path := range paths {
		if strings.Contains(filepath.Base(path), "-launcher-") {
			continue
		}
		filtered = append(filtered, path)
	}
	return latestMatchingFile(filtered)
}

func buildNetrunnerStartupFailureMessage(projectID int, sessionID int, currentStatus string) string {
	projectCWD, err := projectCWDFromID(projectID)
	if err != nil {
		projectCWD = ""
	}
	launcherLogPath := ""
	headlessLogPath := ""
	if projectCWD != "" {
		launcherLogPath = latestSessionLauncherLogPath(projectCWD, sessionID)
		headlessLogPath = latestSessionHeadlessLogPath(projectCWD, sessionID)
	}
	lines := []string{
		fmt.Sprintf("netrunner session %d did not reach in_progress within 120s; current status is %q", sessionID, currentStatus),
		"This usually means the headless launcher or child Codex worker died before fixer_mcp.checkout_task.",
		"Fixer checks:",
		fmt.Sprintf("1. run list_active_worker_processes for session %d and confirm the recorded worker is still alive", sessionID),
		fmt.Sprintf("2. inspect the launcher log: %s", func() string {
			if launcherLogPath != "" {
				return launcherLogPath
			}
			return "(not found)"
		}()),
		fmt.Sprintf("3. inspect the headless netrunner log: %s", func() string {
			if headlessLogPath != "" {
				return headlessLogPath
			}
			return "(not found)"
		}()),
		fmt.Sprintf("4. if the worker is stale or defunct, run stop_active_worker_processes for session %d before any relaunch", sessionID),
		fmt.Sprintf("5. relaunch session %d only after the stale worker is cleared; if the same startup failure repeats, inspect the logs first before retrying again", sessionID),
	}
	return strings.Join(lines, "\n")
}

func recordWorkerProcessLaunch(projectID int, sessionID int, pid int, launchEpoch int) error {
	_, err := db.Exec(
		`INSERT INTO worker_process (project_id, session_id, pid, launch_epoch, launch_origin, status, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
		projectID,
		sessionID,
		pid,
		launchEpoch,
		"explicit-wait",
		workerStatusRunning,
	)
	return err
}

func latestWorkerLaunchEpoch(sessionID int, projectID int) (int, error) {
	var launchEpoch int
	err := db.QueryRow(
		`SELECT COALESCE(launch_epoch, 0)
		 FROM worker_process
		 WHERE session_id = ? AND project_id = ?
		 ORDER BY id DESC
		 LIMIT 1`,
		sessionID,
		projectID,
	).Scan(&launchEpoch)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return launchEpoch, nil
}

func refreshWorkerProcessSnapshot(projectID int, row workerProcessSnapshot) (workerProcessSnapshot, error) {
	row.Alive = isProcessAlive(row.PID)
	if row.Status == workerStatusRunning && !row.Alive {
		stopReason := strings.TrimSpace(row.StopReason)
		if stopReason == "" {
			stopReason = "process exited"
		}
		if _, err := db.Exec(
			`UPDATE worker_process
			 SET status = ?, stop_reason = ?, stopped_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
			 WHERE id = ? AND project_id = ?`,
			workerStatusExited,
			stopReason,
			row.ID,
			projectID,
		); err != nil {
			return workerProcessSnapshot{}, err
		}
		timestamp := time.Now().UTC().Format(time.RFC3339)
		row.Status = workerStatusExited
		row.StopReason = stopReason
		row.StoppedAt = timestamp
		row.UpdatedAt = timestamp
	}
	return row, nil
}

func refreshWorkerProcessLiveness(projectID int, rows []workerProcessSnapshot) ([]workerProcessSnapshot, error) {
	active := make([]workerProcessSnapshot, 0, len(rows))
	for _, row := range rows {
		refreshed, err := refreshWorkerProcessSnapshot(projectID, row)
		if err != nil {
			return nil, err
		}
		if refreshed.Status != workerStatusRunning || !refreshed.Alive {
			continue
		}
		active = append(active, refreshed)
	}
	return active, nil
}

func latestWorkerProcessForSession(projectID int, sessionID int) (workerProcessSnapshot, bool, error) {
	var row workerProcessSnapshot
	err := db.QueryRow(
		`SELECT id,
		        session_id,
		        pid,
		        launch_epoch,
		        COALESCE(launch_origin, ''),
		        status,
		        started_at,
		        updated_at,
		        COALESCE(stopped_at, ''),
		        COALESCE(stop_reason, '')
		 FROM worker_process
		 WHERE project_id = ? AND session_id = ?
		 ORDER BY id DESC
		 LIMIT 1`,
		projectID,
		sessionID,
	).Scan(&row.ID, &row.SessionID, &row.PID, &row.LaunchEpoch, &row.LaunchOrigin, &row.Status, &row.StartedAt, &row.UpdatedAt, &row.StoppedAt, &row.StopReason)
	if err == sql.ErrNoRows {
		return workerProcessSnapshot{}, false, nil
	}
	if err != nil {
		return workerProcessSnapshot{}, false, err
	}
	refreshed, err := refreshWorkerProcessSnapshot(projectID, row)
	if err != nil {
		return workerProcessSnapshot{}, false, err
	}
	return refreshed, true, nil
}

func buildWorkerProcessExitDiagnostic(projectID int, sessionID int, process workerProcessSnapshot) workerProcessExitDiagnostic {
	projectCWD, err := projectCWDFromID(projectID)
	if err != nil {
		projectCWD = ""
	}
	launcherLogPath := ""
	headlessLogPath := ""
	headlessLogMtime := ""
	if projectCWD != "" {
		launcherLogPath = latestSessionLauncherLogPath(projectCWD, sessionID)
		headlessLogPath = latestSessionHeadlessLogPath(projectCWD, sessionID)
		if info, statErr := os.Stat(headlessLogPath); statErr == nil && !info.IsDir() {
			headlessLogMtime = info.ModTime().UTC().Format(time.RFC3339)
		}
	}
	return workerProcessExitDiagnostic{
		WorkerProcessID:  process.ID,
		PID:              process.PID,
		ProcessStatus:    process.Status,
		StopReason:       process.StopReason,
		Alive:            process.Alive,
		HeadlessLogPath:  headlessLogPath,
		LauncherLogPath:  launcherLogPath,
		HeadlessLogMtime: headlessLogMtime,
	}
}

func listRunningWorkerProcesses(projectID int, globalSessionIDs []int) ([]workerProcessSnapshot, error) {
	query := `SELECT id,
	                 session_id,
	                 pid,
	                 launch_epoch,
	                 COALESCE(launch_origin, ''),
	                 status,
	                 started_at,
	                 updated_at,
	                 COALESCE(stopped_at, ''),
	                 COALESCE(stop_reason, '')
	          FROM worker_process
	          WHERE project_id = ?
	            AND status = ?`
	args := []any{projectID, workerStatusRunning}
	if len(globalSessionIDs) > 0 {
		placeholders := make([]string, 0, len(globalSessionIDs))
		for _, sessionID := range globalSessionIDs {
			placeholders = append(placeholders, "?")
			args = append(args, sessionID)
		}
		query += " AND session_id IN (" + strings.Join(placeholders, ",") + ")"
	}
	query += " ORDER BY session_id, id"

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	processes := []workerProcessSnapshot{}
	for rows.Next() {
		var row workerProcessSnapshot
		if err := rows.Scan(&row.ID, &row.SessionID, &row.PID, &row.LaunchEpoch, &row.LaunchOrigin, &row.Status, &row.StartedAt, &row.UpdatedAt, &row.StoppedAt, &row.StopReason); err != nil {
			return nil, err
		}
		processes = append(processes, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return refreshWorkerProcessLiveness(projectID, processes)
}
