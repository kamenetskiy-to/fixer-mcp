package main

import (
	"database/sql"
	"fmt"
	"log"
	"path/filepath"
	"strings"
)

func projectCWDFromID(projectID int) (string, error) {
	var cwd string
	err := db.QueryRow("SELECT cwd FROM project WHERE id = ?", projectID).Scan(&cwd)
	if err != nil {
		return "", err
	}
	return cwd, nil
}

func projectNameFromID(projectID int) (string, error) {
	var name string
	err := db.QueryRow("SELECT name FROM project WHERE id = ?", projectID).Scan(&name)
	if err != nil {
		return "", err
	}
	return name, nil
}

func sessionBelongsToProject(sessionId int, projectId int) (bool, error) {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM session WHERE id = ? AND project_id = ?", sessionId, projectId).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func countSessionDocProposals(sessionId int, projectId int) (int, error) {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM doc_proposal WHERE session_id = ? AND project_id = ?", sessionId, projectId).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

func fetchProjectSessionStatus(globalSessionID int, projectID int) (string, int, bool, error) {
	var status string
	var localSessionID int
	err := db.QueryRow(
		`SELECT s.status,
		        (
			        SELECT COUNT(*)
			        FROM session ranked
			        WHERE ranked.project_id = s.project_id AND ranked.id <= s.id
		        ) AS local_session_id
		 FROM session s
		 WHERE s.id = ? AND s.project_id = ?`,
		globalSessionID,
		projectID,
	).Scan(&status, &localSessionID)
	if err == sql.ErrNoRows {
		return "", 0, false, nil
	}
	if err != nil {
		return "", 0, false, err
	}
	return status, localSessionID, true, nil
}

func statusAllowed(status string, allowedStatuses map[string]struct{}) bool {
	if len(allowedStatuses) == 0 {
		return true
	}
	_, ok := allowedStatuses[status]
	return ok
}

func resolveAuthorizedNetrunnerSessionID(action string, allowedStatuses map[string]struct{}) (int, int, error) {
	if authorizedSessionId <= 0 {
		return 0, 0, fmt.Errorf("%s requires a checked-out netrunner session; call checkout_task first", action)
	}

	status, localSessionID, found, err := fetchProjectSessionStatus(authorizedSessionId, authorizedProjectId)
	if err != nil {
		return 0, 0, fmt.Errorf("DB query error: %v", err)
	}
	if found && statusAllowed(status, allowedStatuses) {
		return authorizedSessionId, localSessionID, nil
	}

	globalFromLocal, mapErr := globalSessionIDFromProjectScoped(authorizedSessionId, authorizedProjectId)
	if mapErr == nil && globalFromLocal != authorizedSessionId {
		localStatus, mappedLocalSessionID, mappedFound, statusErr := fetchProjectSessionStatus(globalFromLocal, authorizedProjectId)
		if statusErr != nil {
			return 0, 0, fmt.Errorf("DB query error: %v", statusErr)
		}
		if mappedFound && statusAllowed(localStatus, allowedStatuses) {
			log.Printf(
				"recovered netrunner session binding for %s: project_id=%d project_scoped_session_id=%d global_session_id=%d",
				action,
				authorizedProjectId,
				authorizedSessionId,
				globalFromLocal,
			)
			authorizedSessionId = globalFromLocal
			return globalFromLocal, mappedLocalSessionID, nil
		}
	}

	if found {
		return 0, 0, fmt.Errorf("%s requires checked-out session %d to be in an allowed status; current status is %q", action, localSessionID, status)
	}
	if mapErr == sql.ErrNoRows {
		return 0, 0, fmt.Errorf("%s has an invalid checked-out session binding for current project; call checkout_task again", action)
	}
	if mapErr != nil {
		return 0, 0, fmt.Errorf("DB mapping error: %v", mapErr)
	}
	return 0, 0, fmt.Errorf("%s has an invalid checked-out session binding for current project; call checkout_task again", action)
}

func canAccessSession(projectId int) bool {
	if authorizedRole == "overseer" {
		return true
	}
	return projectId == authorizedProjectId
}

func normalizeProjectCWD(raw string) (string, error) {
	cwd := strings.TrimSpace(raw)
	if cwd == "" {
		return "", fmt.Errorf("cwd is required")
	}
	if !filepath.IsAbs(cwd) {
		return "", fmt.Errorf("cwd must be an absolute path")
	}

	normalized := filepath.Clean(cwd)
	if resolved, err := filepath.EvalSymlinks(normalized); err == nil {
		normalized = filepath.Clean(resolved)
	}
	return normalized, nil
}

func findProjectByCWD(normalizedCWD string) (int, string, string, error) {
	var projectID int
	var projectName string
	var projectCWD string
	err := db.QueryRow(
		`
		SELECT id, name, cwd
		FROM project
		WHERE cwd = ? OR ? LIKE cwd || '/%'
		ORDER BY LENGTH(cwd) DESC
		LIMIT 1
		`,
		normalizedCWD,
		normalizedCWD,
	).Scan(&projectID, &projectName, &projectCWD)
	return projectID, projectName, projectCWD, err
}

func defaultProjectName(cwd string) string {
	base := filepath.Base(cwd)
	switch base {
	case "", ".", string(filepath.Separator):
		return "project"
	default:
		return base
	}
}

func projectDocBelongsToProject(docId int, projectId int) (bool, error) {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM project_doc WHERE id = ? AND project_id = ?", docId, projectId).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
