package main

import "database/sql"

func globalSessionIDFromProjectScoped(localSessionID int, projectID int) (int, error) {
	if localSessionID <= 0 {
		return 0, sql.ErrNoRows
	}

	var globalSessionID int
	err := db.QueryRow(
		`SELECT id
		 FROM session
		 WHERE project_id = ?
		 ORDER BY id
		 LIMIT 1 OFFSET ?`,
		projectID,
		localSessionID-1,
	).Scan(&globalSessionID)
	if err != nil {
		return 0, err
	}
	return globalSessionID, nil
}

func projectScopedSessionIDFromGlobal(globalSessionID int, projectID int) (int, error) {
	var localSessionID int
	err := db.QueryRow(
		`SELECT (
			SELECT COUNT(*)
			FROM session ranked
			WHERE ranked.project_id = ? AND ranked.id <= target.id
		)
		FROM session target
		WHERE target.id = ? AND target.project_id = ?`,
		projectID,
		globalSessionID,
		projectID,
	).Scan(&localSessionID)
	if err != nil {
		return 0, err
	}
	return localSessionID, nil
}

func globalProjectDocIDFromProjectScoped(localDocID int, projectID int) (int, error) {
	if localDocID <= 0 {
		return 0, sql.ErrNoRows
	}

	var globalDocID int
	err := db.QueryRow(
		`SELECT id
		 FROM project_doc
		 WHERE project_id = ?
		 ORDER BY id
		 LIMIT 1 OFFSET ?`,
		projectID,
		localDocID-1,
	).Scan(&globalDocID)
	if err != nil {
		return 0, err
	}
	return globalDocID, nil
}

func projectScopedDocIDFromGlobal(globalDocID int, projectID int) (int, error) {
	var localDocID int
	err := db.QueryRow(
		`SELECT (
			SELECT COUNT(*)
			FROM project_doc ranked
			WHERE ranked.project_id = ? AND ranked.id <= target.id
		)
		FROM project_doc target
		WHERE target.id = ? AND target.project_id = ?`,
		projectID,
		globalDocID,
		projectID,
	).Scan(&localDocID)
	if err != nil {
		return 0, err
	}
	return localDocID, nil
}

func globalDocProposalIDFromProjectScoped(localProposalID int, projectID int) (int, error) {
	if localProposalID <= 0 {
		return 0, sql.ErrNoRows
	}

	var globalProposalID int
	err := db.QueryRow(
		`SELECT id
		 FROM doc_proposal
		 WHERE project_id = ?
		 ORDER BY id
		 LIMIT 1 OFFSET ?`,
		projectID,
		localProposalID-1,
	).Scan(&globalProposalID)
	if err != nil {
		return 0, err
	}
	return globalProposalID, nil
}

func projectScopedDocProposalIDFromGlobal(globalProposalID int, projectID int) (int, error) {
	var localProposalID int
	err := db.QueryRow(
		`SELECT (
			SELECT COUNT(*)
			FROM doc_proposal ranked
			WHERE ranked.project_id = ? AND ranked.id <= target.id
		)
		FROM doc_proposal target
		WHERE target.id = ? AND target.project_id = ?`,
		projectID,
		globalProposalID,
		projectID,
	).Scan(&localProposalID)
	if err != nil {
		return 0, err
	}
	return localProposalID, nil
}
