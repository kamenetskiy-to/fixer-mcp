package main

import (
	"database/sql"
	"fmt"
	"strings"
)

type sessionLifecycleState struct {
	GlobalSessionID       int
	Status                string
	Report                string
	CliBackend            string
	CliModel              string
	CliReasoning          string
	DeclaredWriteScope    []string
	RepairSourceSessionID int
	ReworkCount           int
	ForcedStopCount       int
}

type orchestrationControl struct {
	ProjectID                        int
	SessionID                        int
	State                            string
	Summary                          string
	Focus                            string
	Blocker                          string
	Evidence                         string
	OrchestrationEpoch               int
	OrchestrationFrozen              bool
	NotificationsEnabledForActiveRun bool
}

func fetchOrchestrationControl(projectID int) (orchestrationControl, bool, error) {
	record := orchestrationControl{
		ProjectID:                        projectID,
		OrchestrationEpoch:               0,
		OrchestrationFrozen:              false,
		NotificationsEnabledForActiveRun: true,
	}

	var (
		sessionID            int
		frozenInt            int
		notificationsEnabled int
	)
	err := db.QueryRow(
		`SELECT COALESCE(session_id, 0),
		        state,
		        summary,
		        COALESCE(focus, ''),
		        COALESCE(blocker, ''),
		        COALESCE(evidence, ''),
		        COALESCE(orchestration_epoch, 0),
		        COALESCE(orchestration_frozen, 0),
		        COALESCE(notifications_enabled_for_active_run, 1)
		 FROM autonomous_run_status
		 WHERE project_id = ?`,
		projectID,
	).Scan(
		&sessionID,
		&record.State,
		&record.Summary,
		&record.Focus,
		&record.Blocker,
		&record.Evidence,
		&record.OrchestrationEpoch,
		&frozenInt,
		&notificationsEnabled,
	)
	if err == sql.ErrNoRows {
		return record, false, nil
	}
	if err != nil {
		return orchestrationControl{}, false, err
	}
	record.SessionID = sessionID
	record.OrchestrationFrozen = frozenInt != 0
	record.NotificationsEnabledForActiveRun = notificationsEnabled != 0
	return record, true, nil
}

func upsertOrchestrationControl(projectID int, sessionID int, state string, summary string, focus string, blocker string, evidence string, epoch int, frozen bool, notificationsEnabled bool) error {
	_, err := db.Exec(
		`INSERT INTO autonomous_run_status (
			project_id,
			session_id,
			state,
			summary,
			focus,
			blocker,
			evidence,
			orchestration_epoch,
			orchestration_frozen,
			notifications_enabled_for_active_run,
			updated_at
		)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(project_id) DO UPDATE SET
		   session_id = excluded.session_id,
		   state = excluded.state,
		   summary = excluded.summary,
		   focus = excluded.focus,
		   blocker = excluded.blocker,
		   evidence = excluded.evidence,
		   orchestration_epoch = excluded.orchestration_epoch,
		   orchestration_frozen = excluded.orchestration_frozen,
		   notifications_enabled_for_active_run = excluded.notifications_enabled_for_active_run,
		   updated_at = CURRENT_TIMESTAMP`,
		projectID,
		sessionID,
		state,
		summary,
		focus,
		blocker,
		evidence,
		epoch,
		boolToInt(frozen),
		boolToInt(notificationsEnabled),
	)
	return err
}

func normalizeAutonomousStatusLabel(status string) (string, error) {
	status = strings.ToLower(strings.TrimSpace(status))
	switch status {
	case "running", "blocked", "awaiting_review", "awaiting_next_dispatch", "completed", "idle":
		return status, nil
	default:
		return "", fmt.Errorf("invalid status: must be one of running, blocked, awaiting_review, awaiting_next_dispatch, completed, idle")
	}
}
