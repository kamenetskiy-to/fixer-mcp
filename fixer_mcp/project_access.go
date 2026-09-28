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

// normalizeProjectIdentityKey reduces a caller-declared identity to a stable
// canonical form. Identity keys are normalized git remote URLs or operator
// slugs: case, a trailing slash and a trailing .git suffix are not identity.
func normalizeProjectIdentityKey(raw string) string {
	key := strings.ToLower(strings.TrimSpace(raw))
	key = strings.TrimSuffix(key, "/")
	key = strings.TrimSuffix(key, ".git")
	key = strings.TrimSuffix(key, "/")
	return strings.TrimSpace(key)
}

// findProjectByIdentityKey resolves a project by its caller-declared identity.
// It returns sql.ErrNoRows when no project owns the key yet.
func findProjectByIdentityKey(identityKey string) (int, string, string, error) {
	var projectID int
	var projectName string
	var projectCWD string
	err := db.QueryRow(
		`SELECT id, name, cwd
		 FROM project
		 WHERE identity_key = ?
		 ORDER BY id
		 LIMIT 1`,
		identityKey,
	).Scan(&projectID, &projectName, &projectCWD)
	return projectID, projectName, projectCWD, err
}

// findProjectByKnownPath resolves a cwd against every registered path: the
// canonical project.cwd plus any project_path_alias rows. Nested paths under a
// known root still resolve to that root, so longest known path wins.
func findProjectByKnownPath(normalizedCWD string) (int, string, string, error) {
	var projectID int
	var projectName string
	var projectCWD string
	err := db.QueryRow(
		`
		SELECT id, name, cwd
		FROM (
			SELECT p.id AS id, p.name AS name, p.cwd AS cwd, p.cwd AS known_path
			FROM project p
			UNION ALL
			SELECT p.id AS id, p.name AS name, p.cwd AS cwd, a.path AS known_path
			FROM project_path_alias a
			JOIN project p ON p.id = a.project_id
		)
		WHERE known_path = ? OR ? LIKE known_path || '/%'
		ORDER BY LENGTH(known_path) DESC, id ASC
		LIMIT 1
		`,
		normalizedCWD,
		normalizedCWD,
	).Scan(&projectID, &projectName, &projectCWD)
	return projectID, projectName, projectCWD, err
}

func findProjectByCWD(normalizedCWD string) (int, string, string, error) {
	return findProjectByKnownPath(normalizedCWD)
}

// findProjectByCWDOrIdentity prefers an existing identity binding over the
// incoming path. With an empty identity key it is exactly the legacy
// path-keyed lookup.
func findProjectByCWDOrIdentity(normalizedCWD string, identityKey string) (int, string, string, error) {
	normalizedIdentity := normalizeProjectIdentityKey(identityKey)
	if normalizedIdentity != "" {
		projectID, projectName, projectCWD, err := findProjectByIdentityKey(normalizedIdentity)
		if err == nil {
			return projectID, projectName, projectCWD, nil
		}
		if err != sql.ErrNoRows {
			return 0, "", "", err
		}
	}
	return findProjectByKnownPath(normalizedCWD)
}

// assignProjectIdentityKey binds an identity to a project only when the project
// has no identity yet. Reassigning a project to a different key is refused so
// two identities can never collapse into one project by accident.
func assignProjectIdentityKey(projectID int, normalizedIdentity string) error {
	if normalizedIdentity == "" {
		return nil
	}
	var existing sql.NullString
	if err := db.QueryRow(`SELECT identity_key FROM project WHERE id = ?`, projectID).Scan(&existing); err != nil {
		return err
	}
	if existing.Valid {
		current := normalizeProjectIdentityKey(existing.String)
		if current != "" {
			if current != normalizedIdentity {
				return fmt.Errorf(
					"identity_key conflict: project %d is already bound to identity_key %q and cannot be rebound to %q; use a distinct identity_key or resolve the projects explicitly",
					projectID,
					current,
					normalizedIdentity,
				)
			}
			return nil
		}
	}
	_, err := db.Exec(
		`UPDATE project SET identity_key = ? WHERE id = ? AND (identity_key IS NULL OR TRIM(identity_key) = '')`,
		normalizedIdentity,
		projectID,
	)
	return err
}

// registerProjectPathAlias records an incoming path as an alias of an existing
// project. A path already owned by another project is never silently stolen.
func registerProjectPathAlias(aliasPath string, projectID int) error {
	if aliasPath == "" || projectID <= 0 {
		return fmt.Errorf("path alias registration requires a path and a project id")
	}

	var ownerID int
	err := db.QueryRow(`SELECT id FROM project WHERE cwd = ?`, aliasPath).Scan(&ownerID)
	switch {
	case err == nil:
		if ownerID != projectID {
			return projectPathAliasConflictError(aliasPath, ownerID, projectID)
		}
		return nil
	case err != sql.ErrNoRows:
		return err
	}

	err = db.QueryRow(`SELECT project_id FROM project_path_alias WHERE path = ?`, aliasPath).Scan(&ownerID)
	switch {
	case err == nil:
		if ownerID != projectID {
			return projectPathAliasConflictError(aliasPath, ownerID, projectID)
		}
		return nil
	case err != sql.ErrNoRows:
		return err
	}

	_, err = db.Exec(
		`INSERT INTO project_path_alias (path, project_id, created_at) VALUES (?, ?, CURRENT_TIMESTAMP)`,
		aliasPath,
		projectID,
	)
	if err != nil {
		// A concurrent writer may have claimed the path between the checks and
		// the insert; re-read the owner and fail closed instead of trusting the
		// write error text.
		if rereadErr := db.QueryRow(`SELECT project_id FROM project_path_alias WHERE path = ?`, aliasPath).Scan(&ownerID); rereadErr == nil {
			if ownerID != projectID {
				return projectPathAliasConflictError(aliasPath, ownerID, projectID)
			}
			return nil
		}
		return err
	}
	return nil
}

func projectPathAliasConflictError(aliasPath string, ownerID int, projectID int) error {
	return fmt.Errorf(
		"path conflict: %q is already registered to project %d; refusing to reassign it to project %d. Register a distinct cwd or resolve the duplicate project explicitly before retrying",
		aliasPath,
		ownerID,
		projectID,
	)
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
