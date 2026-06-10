package main

import "strings"

func _() {
	_ = authorizedSessionId
}

func projectScopedDocProposalIDsForSession(sessionID int, projectID int) ([]int, error) {
	rows, err := db.Query(
		`SELECT (
			SELECT COUNT(*)
			FROM doc_proposal ranked
			WHERE ranked.project_id = ? AND ranked.id <= target.id
		) AS local_proposal_id
		 FROM doc_proposal
		 AS target
		 WHERE target.session_id = ? AND target.project_id = ?
		 ORDER BY target.id`,
		projectID,
		sessionID,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	localIDs := []int{}
	for rows.Next() {
		var localProposalID int
		if err := rows.Scan(&localProposalID); err != nil {
			return nil, err
		}
		localIDs = append(localIDs, localProposalID)
	}
	if localIDs == nil {
		localIDs = []int{}
	}
	return localIDs, nil
}

func projectExists(projectID int) (bool, error) {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM project WHERE id = ?", projectID).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func normalizeCompactText(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func truncateRunes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	if limit == 1 {
		return "…"
	}
	return string(runes[:limit-1]) + "…"
}
func shouldRecommendRepairFork(reworkCount, forcedStopCount, repairSourceSessionID int) bool {
	if repairSourceSessionID > 0 {
		return false
	}
	return forcedStopCount > 0 || reworkCount >= reworkRepairThreshold
}

func fetchSessionLifecycleState(sessionID int, projectID int) (sessionLifecycleState, error) {
	var (
		state      sessionLifecycleState
		writeScope string
	)
	err := db.QueryRow(
		`SELECT id,
		        status,
		        COALESCE(report, ''),
		        COALESCE(NULLIF(TRIM(cli_backend), ''), ?),
		        COALESCE(cli_model, ''),
		        COALESCE(cli_reasoning, ''),
		        COALESCE(declared_write_scope, ''),
		        COALESCE(repair_source_session_id, 0),
		        COALESCE(rework_count, 0),
		        COALESCE(forced_stop_count, 0)
		 FROM session
		 WHERE id = ? AND project_id = ?`,
		defaultCliBackend,
		sessionID,
		projectID,
	).Scan(
		&state.GlobalSessionID,
		&state.Status,
		&state.Report,
		&state.CliBackend,
		&state.CliModel,
		&state.CliReasoning,
		&writeScope,
		&state.RepairSourceSessionID,
		&state.ReworkCount,
		&state.ForcedStopCount,
	)
	if err != nil {
		return sessionLifecycleState{}, err
	}
	state.DeclaredWriteScope, err = decodeDeclaredWriteScope(writeScope)
	if err != nil {
		return sessionLifecycleState{}, err
	}
	return state, nil
}
