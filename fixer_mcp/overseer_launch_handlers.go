package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type LaunchAndWaitFixersInput struct {
	ProjectIds          []int  `json:"project_ids,omitempty" jsonschema:"Optional project IDs. When empty, all projects with project.active != 0 are targeted."`
	Message             string `json:"message,omitempty" jsonschema:"Optional overseer message to append to every target project before launch."`
	TimeoutSeconds      int    `json:"timeout_seconds,omitempty" jsonschema:"Maximum wait in seconds. Defaults to 7200; max 21600."`
	PollIntervalSeconds int    `json:"poll_interval_seconds,omitempty" jsonschema:"Polling interval in seconds. Defaults to 5; max 60."`
}

type LaunchAndWaitFixerProjectResult struct {
	ProjectId          int    `json:"project_id"`
	Cwd                string `json:"cwd"`
	CursorMessageId    int    `json:"cursor_message_id"`
	AppendedMessageId  int    `json:"appended_message_id,omitempty"`
	LaunchStatus       string `json:"launch_status"`
	LauncherScript     string `json:"launcher_script,omitempty"`
	LauncherDiagnostic string `json:"launcher_diagnostic,omitempty"`
}

type LaunchAndWaitFixersOutput struct {
	Status              string                            `json:"status"`
	TimedOut            bool                              `json:"timed_out"`
	ProjectIds          []int                             `json:"project_ids"`
	TimeoutSeconds      int                               `json:"timeout_seconds"`
	PollIntervalSeconds int                               `json:"poll_interval_seconds"`
	CursorMessageId     int                               `json:"cursor_message_id"`
	Messages            []OverseerFixerMessageRecord      `json:"messages"`
	Projects            []LaunchAndWaitFixerProjectResult `json:"projects"`
}

type launchAndWaitFixerTarget struct {
	ProjectID int
	CWD       string
}

func targetProjectsForLaunchAndWait(projectIDs []int) ([]launchAndWaitFixerTarget, error) {
	targets := []launchAndWaitFixerTarget{}
	if len(projectIDs) == 0 {
		rows, err := db.Query("SELECT id, cwd FROM project WHERE COALESCE(active, 0) != 0 ORDER BY id")
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var target launchAndWaitFixerTarget
			if err := rows.Scan(&target.ProjectID, &target.CWD); err != nil {
				return nil, err
			}
			targets = append(targets, target)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return targets, nil
	}

	seen := map[int]struct{}{}
	for _, projectID := range projectIDs {
		if projectID <= 0 {
			return nil, fmt.Errorf("project_ids must contain positive project IDs")
		}
		if _, ok := seen[projectID]; ok {
			continue
		}
		seen[projectID] = struct{}{}

		var target launchAndWaitFixerTarget
		err := db.QueryRow("SELECT id, cwd FROM project WHERE id = ?", projectID).Scan(&target.ProjectID, &target.CWD)
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("project not found: %d", projectID)
		}
		if err != nil {
			return nil, err
		}
		targets = append(targets, target)
	}
	return targets, nil
}

func upsertOverseerFixerRunStateForLaunch(projectID int, cursorMessageID int) error {
	_, err := db.Exec(
		`INSERT INTO overseer_fixer_run_state (project_id, active, status, reason, last_message_id, updated_at)
		 VALUES (?, 1, ?, ?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(project_id) DO UPDATE SET
		   active = excluded.active,
		   status = excluded.status,
		   reason = excluded.reason,
		   last_message_id = excluded.last_message_id,
		   updated_at = CURRENT_TIMESTAMP`,
		projectID,
		"launched_by_overseer",
		"launch_and_wait_fixers",
		cursorMessageID,
	)
	return err
}

func appendOverseerFixerMessageForProject(projectID int, content string) (OverseerFixerMessageRecord, error) {
	result, err := db.Exec(
		`INSERT INTO overseer_fixer_message (project_id, sender_role, content)
		 VALUES (?, 'overseer', ?)`,
		projectID,
		content,
	)
	if err != nil {
		return OverseerFixerMessageRecord{}, err
	}
	messageID, err := result.LastInsertId()
	if err != nil {
		return OverseerFixerMessageRecord{}, err
	}
	return fetchOverseerFixerMessageByID(int(messageID))
}

func fetchNewFixerMessagesAfterProjectCursors(cursors map[int]int) ([]OverseerFixerMessageRecord, error) {
	if len(cursors) == 0 {
		return []OverseerFixerMessageRecord{}, nil
	}

	projectIDs := make([]int, 0, len(cursors))
	for projectID := range cursors {
		projectIDs = append(projectIDs, projectID)
	}
	sort.Ints(projectIDs)

	clauses := make([]string, 0, len(projectIDs))
	args := []any{"fixer"}
	for _, projectID := range projectIDs {
		cursor := cursors[projectID]
		if cursor < 0 {
			return nil, fmt.Errorf("message cursors must be non-negative")
		}
		clauses = append(clauses, "(project_id = ? AND id > ?)")
		args = append(args, projectID, cursor)
	}

	rows, err := db.Query(
		`SELECT id, project_id, sender_role, content, created_at
		 FROM overseer_fixer_message
		 WHERE sender_role = ?
		   AND (`+strings.Join(clauses, " OR ")+`)
		 ORDER BY id ASC`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	messages := []OverseerFixerMessageRecord{}
	for rows.Next() {
		var message OverseerFixerMessageRecord
		if err := rows.Scan(&message.Id, &message.ProjectId, &message.SenderRole, &message.Content, &message.CreatedAt); err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return messages, nil
}

func maxCursorFromProjectCursors(cursors map[int]int) int {
	maxCursor := 0
	for _, cursor := range cursors {
		if cursor > maxCursor {
			maxCursor = cursor
		}
	}
	return maxCursor
}

func truncateLauncherOutput(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "... [truncated]"
}

func launcherFailureDiagnostic(err error, stdout string, stderr string) string {
	parts := []string{err.Error()}
	if output := truncateLauncherOutput(stdout, 2000); output != "" {
		parts = append(parts, "stdout: "+output)
	}
	if output := truncateLauncherOutput(stderr, 2000); output != "" {
		parts = append(parts, "stderr: "+output)
	}
	return strings.Join(parts, "\n")
}

func launchOverseerFixerForProject(projectCWD string, launcherScript string) error {
	command := execCommand(
		"python3",
		launcherScript,
		"launch-overseer-fixer",
		"--cwd",
		projectCWD,
	)
	command.Dir = projectCWD
	command.Stdin = bytes.NewReader(nil)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	commandEnv, envErr := resolveRuntimeLaunchEnv(projectCWD, os.Environ())
	if envErr != nil {
		log.Printf("warning: failed to resolve runtime launch env for %s: %v", projectCWD, envErr)
		commandEnv = os.Environ()
	}
	command.Env = commandEnv
	if err := command.Run(); err != nil {
		return errors.New(launcherFailureDiagnostic(err, stdout.String(), stderr.String()))
	}
	return nil
}

func LaunchAndWaitFixers(ctx context.Context, req *mcp.CallToolRequest, input LaunchAndWaitFixersInput) (*mcp.CallToolResult, LaunchAndWaitFixersOutput, error) {
	if authorizedRole != "overseer" {
		return &mcp.CallToolResult{IsError: true}, LaunchAndWaitFixersOutput{}, fmt.Errorf("access denied: requires overseer role")
	}

	timeoutSeconds, err := explicitWaitTimeoutSeconds(input.TimeoutSeconds)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, LaunchAndWaitFixersOutput{}, err
	}
	pollIntervalSeconds, err := explicitWaitPollIntervalSeconds(input.PollIntervalSeconds)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, LaunchAndWaitFixersOutput{}, err
	}

	targets, err := targetProjectsForLaunchAndWait(input.ProjectIds)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, LaunchAndWaitFixersOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	if len(targets) == 0 {
		status := "no_active_fixers"
		if len(input.ProjectIds) > 0 {
			status = "no_target_projects"
		}
		return nil, LaunchAndWaitFixersOutput{
			Status:              status,
			ProjectIds:          []int{},
			TimeoutSeconds:      timeoutSeconds,
			PollIntervalSeconds: pollIntervalSeconds,
			Messages:            []OverseerFixerMessageRecord{},
			Projects:            []LaunchAndWaitFixerProjectResult{},
		}, nil
	}

	launcherScript, err := resolveExplicitLauncherScript()
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, LaunchAndWaitFixersOutput{}, err
	}

	projectIDs := make([]int, 0, len(targets))
	cursors := map[int]int{}
	projectResults := make([]LaunchAndWaitFixerProjectResult, 0, len(targets))
	messageContent := strings.TrimSpace(input.Message)

	for _, target := range targets {
		cursor, err := latestOverseerFixerMessageID(target.ProjectID)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, LaunchAndWaitFixersOutput{}, fmt.Errorf("DB query error: %v", err)
		}

		result := LaunchAndWaitFixerProjectResult{
			ProjectId:       target.ProjectID,
			Cwd:             target.CWD,
			CursorMessageId: cursor,
			LaunchStatus:    "pending",
			LauncherScript:  launcherScript,
		}

		if messageContent != "" {
			message, err := appendOverseerFixerMessageForProject(target.ProjectID, messageContent)
			if err != nil {
				return &mcp.CallToolResult{IsError: true}, LaunchAndWaitFixersOutput{}, fmt.Errorf("DB insert error: %v", err)
			}
			result.AppendedMessageId = message.Id
		}

		if err := upsertOverseerFixerRunStateForLaunch(target.ProjectID, cursor); err != nil {
			return &mcp.CallToolResult{IsError: true}, LaunchAndWaitFixersOutput{}, fmt.Errorf("DB upsert error: %v", err)
		}

		if err := launchOverseerFixerForProject(target.CWD, launcherScript); err != nil {
			result.LaunchStatus = "failed"
			result.LauncherDiagnostic = err.Error()
			projectResults = append(projectResults, result)
			return nil, LaunchAndWaitFixersOutput{
				Status:              "launch_failed",
				ProjectIds:          projectIDs,
				TimeoutSeconds:      timeoutSeconds,
				PollIntervalSeconds: pollIntervalSeconds,
				CursorMessageId:     maxCursorFromProjectCursors(cursors),
				Messages:            []OverseerFixerMessageRecord{},
				Projects:            projectResults,
			}, nil
		}

		result.LaunchStatus = "launched"
		projectIDs = append(projectIDs, target.ProjectID)
		cursors[target.ProjectID] = cursor
		projectResults = append(projectResults, result)
	}

	timeout := time.Duration(timeoutSeconds) * time.Second
	pollInterval := time.Duration(pollIntervalSeconds) * time.Second
	deadline := time.Now().Add(timeout)

	for {
		messages, err := fetchNewFixerMessagesAfterProjectCursors(cursors)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, LaunchAndWaitFixersOutput{}, fmt.Errorf("DB query error: %v", err)
		}
		if len(messages) > 0 {
			return nil, LaunchAndWaitFixersOutput{
				Status:              "messages",
				ProjectIds:          projectIDs,
				TimeoutSeconds:      timeoutSeconds,
				PollIntervalSeconds: pollIntervalSeconds,
				CursorMessageId:     cursorFromMessages(messages, maxCursorFromProjectCursors(cursors)),
				Messages:            messages,
				Projects:            projectResults,
			}, nil
		}

		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, LaunchAndWaitFixersOutput{
				Status:              "timeout",
				TimedOut:            true,
				ProjectIds:          projectIDs,
				TimeoutSeconds:      timeoutSeconds,
				PollIntervalSeconds: pollIntervalSeconds,
				CursorMessageId:     maxCursorFromProjectCursors(cursors),
				Messages:            []OverseerFixerMessageRecord{},
				Projects:            projectResults,
			}, nil
		}

		sleepFor := pollInterval
		if remaining < sleepFor {
			sleepFor = remaining
		}

		select {
		case <-ctx.Done():
			return &mcp.CallToolResult{IsError: true}, LaunchAndWaitFixersOutput{}, ctx.Err()
		case <-time.After(sleepFor):
		}
	}
}
