package dashboardapi

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// ProjectDocTreeItem is the dashboard-facing representation of one canonical
// project document. IDs are project-scoped (the same convention used by the
// existing docs and attachment APIs), so the tree can be consumed without
// leaking database-global identifiers to the client.
type ProjectDocTreeItem struct {
	ID             int                  `json:"id"`
	ParentDocID    int                  `json:"parent_doc_id,omitempty"`
	Level          int                  `json:"level"`
	Slug           string               `json:"slug"`
	Path           string               `json:"path"`
	Status         string               `json:"status"`
	Title          string               `json:"title"`
	DocType        string               `json:"doc_type"`
	ContentPreview string               `json:"content_preview"`
	Content        string               `json:"content"`
	Children       []ProjectDocTreeItem `json:"children"`
}

type ProjectDocsTreeResponse struct {
	Project   ProjectHeader        `json:"project"`
	TotalDocs int                  `json:"total_docs"`
	Roots     []ProjectDocTreeItem `json:"roots"`
}

type ProjectDocDetailResponse struct {
	Project  ProjectHeader      `json:"project"`
	Document ProjectDocTreeItem `json:"document"`
}

// ProjectDocsTree returns the canonical project-doc tree in deterministic
// parent-first order. The current project-doc schema is required here; legacy
// summary consumers continue to use ProjectDocs below unchanged.
func (r *Repository) ProjectDocsTree(ctx context.Context, projectID int) (ProjectDocsTreeResponse, error) {
	project, err := r.requireProject(ctx, projectID)
	if err != nil {
		return ProjectDocsTreeResponse{}, err
	}
	if err := r.requireProjectDocLocalizationCoverage(ctx, projectID); err != nil {
		return ProjectDocsTreeResponse{}, err
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT
			(
				SELECT COUNT(*)
				FROM project_doc d2
				WHERE d2.project_id = d.project_id AND d2.id <= d.id
			) AS local_doc_id,
			COALESCE((
				SELECT COUNT(*)
				FROM project_doc parent_ranked
				WHERE parent_ranked.project_id = d.project_id AND parent_ranked.id <= d.parent_doc_id
			), 0) AS parent_local_doc_id,
			COALESCE(d.level, 0),
			COALESCE(d.slug, ''),
			COALESCE(d.path, ''),
			COALESCE(d.status, 'current'),
			d.title,
			COALESCE(d.doc_type, 'documentation'),
			d.content
		FROM project_doc d
		WHERE d.project_id = ?
		ORDER BY COALESCE(d.level, 0), COALESCE(d.path, ''), d.id`, projectID)
	if err != nil {
		return ProjectDocsTreeResponse{}, err
	}
	defer rows.Close()

	items := make([]ProjectDocTreeItem, 0)
	for rows.Next() {
		var item ProjectDocTreeItem
		if err := rows.Scan(
			&item.ID,
			&item.ParentDocID,
			&item.Level,
			&item.Slug,
			&item.Path,
			&item.Status,
			&item.Title,
			&item.DocType,
			&item.Content,
		); err != nil {
			return ProjectDocsTreeResponse{}, err
		}
		item.ContentPreview = summarizeContent(item.Content)
		item.Children = []ProjectDocTreeItem{}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return ProjectDocsTreeResponse{}, err
	}

	knownIDs := make(map[int]struct{}, len(items))
	childrenByParent := make(map[int][]ProjectDocTreeItem, len(items))
	rootItems := make([]ProjectDocTreeItem, 0, len(items))
	for _, item := range items {
		knownIDs[item.ID] = struct{}{}
	}
	for _, item := range items {
		if item.ParentDocID == 0 {
			rootItems = append(rootItems, item)
			continue
		}
		if _, ok := knownIDs[item.ParentDocID]; !ok {
			// A partially migrated legacy row should remain visible instead of
			// making the whole project tree disappear.
			rootItems = append(rootItems, item)
			continue
		}
		childrenByParent[item.ParentDocID] = append(childrenByParent[item.ParentDocID], item)
	}

	var withChildren func(ProjectDocTreeItem) ProjectDocTreeItem
	withChildren = func(item ProjectDocTreeItem) ProjectDocTreeItem {
		children := childrenByParent[item.ID]
		item.Children = make([]ProjectDocTreeItem, 0, len(children))
		for _, child := range children {
			item.Children = append(item.Children, withChildren(child))
		}
		return item
	}
	roots := make([]ProjectDocTreeItem, 0, len(rootItems))
	for _, root := range rootItems {
		roots = append(roots, withChildren(root))
	}

	return ProjectDocsTreeResponse{
		Project:   ProjectHeader{ID: project.ID, Name: project.Name, CWD: project.CWD},
		TotalDocs: len(items),
		Roots:     roots,
	}, nil
}

// ProjectDoc returns one selected document, including its complete Markdown
// content. The input ID is project-scoped, matching ProjectDocsTree.
func (r *Repository) ProjectDoc(ctx context.Context, projectID int, localDocID int) (ProjectDocDetailResponse, error) {
	project, err := r.requireProject(ctx, projectID)
	if err != nil {
		return ProjectDocDetailResponse{}, err
	}
	if err := r.requireProjectDocLocalizationCoverage(ctx, projectID); err != nil {
		return ProjectDocDetailResponse{}, err
	}
	if localDocID <= 0 {
		return ProjectDocDetailResponse{}, fmt.Errorf("invalid project document id")
	}

	var item ProjectDocTreeItem
	err = r.db.QueryRowContext(ctx, `
		SELECT
			(
				SELECT COUNT(*)
				FROM project_doc d2
				WHERE d2.project_id = d.project_id AND d2.id <= d.id
			) AS local_doc_id,
			COALESCE((
				SELECT COUNT(*)
				FROM project_doc parent_ranked
				WHERE parent_ranked.project_id = d.project_id AND parent_ranked.id <= d.parent_doc_id
			), 0) AS parent_local_doc_id,
			COALESCE(d.level, 0),
			COALESCE(d.slug, ''),
			COALESCE(d.path, ''),
			COALESCE(d.status, 'current'),
			d.title,
			COALESCE(d.doc_type, 'documentation'),
			d.content
		FROM project_doc d
		WHERE d.project_id = ?
			AND (
				SELECT COUNT(*)
				FROM project_doc d2
				WHERE d2.project_id = d.project_id AND d2.id <= d.id
			) = ?`, projectID, localDocID).Scan(
		&item.ID,
		&item.ParentDocID,
		&item.Level,
		&item.Slug,
		&item.Path,
		&item.Status,
		&item.Title,
		&item.DocType,
		&item.Content,
	)
	if err != nil {
		return ProjectDocDetailResponse{}, err
	}
	item.ContentPreview = summarizeContent(item.Content)
	item.Children = []ProjectDocTreeItem{}

	return ProjectDocDetailResponse{
		Project:  ProjectHeader{ID: project.ID, Name: project.Name, CWD: project.CWD},
		Document: item,
	}, nil
}

func (r *Repository) ProjectDocs(ctx context.Context, projectID int) (ProjectDocsResponse, error) {
	project, err := r.requireProject(ctx, projectID)
	if err != nil {
		return ProjectDocsResponse{}, err
	}
	if err := r.requireProjectDocLocalizationCoverage(ctx, projectID); err != nil {
		return ProjectDocsResponse{}, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT
			(
				SELECT COUNT(*)
				FROM project_doc d2
				WHERE d2.project_id = d.project_id AND d2.id <= d.id
			) AS local_doc_id,
			COALESCE(d.doc_type, 'documentation'),
			d.title,
			d.content,
			COALESCE(targeted.pending_count, 0) AS targeted_pending_count,
			COALESCE(untyped.pending_count, 0) AS untargeted_pending_count
		FROM project_doc d
		LEFT JOIN (
			SELECT target_project_doc_id, COUNT(*) AS pending_count
			FROM doc_proposal
			WHERE project_id = ? AND status = 'pending' AND target_project_doc_id IS NOT NULL
			GROUP BY target_project_doc_id
		) targeted ON targeted.target_project_doc_id = d.id
		LEFT JOIN (
			SELECT COALESCE(proposed_doc_type, 'documentation') AS doc_type, COUNT(*) AS pending_count
			FROM doc_proposal
			WHERE project_id = ? AND status = 'pending' AND target_project_doc_id IS NULL
			GROUP BY COALESCE(proposed_doc_type, 'documentation')
		) untyped ON untyped.doc_type = COALESCE(d.doc_type, 'documentation')
		WHERE d.project_id = ?
		ORDER BY COALESCE(d.doc_type, 'documentation'), d.id`,
		projectID,
		projectID,
		projectID,
	)
	if err != nil {
		return ProjectDocsResponse{}, err
	}
	defer rows.Close()

	groupMap := map[string]*DocGroup{}
	groupOrder := []string{}
	totalTargeted := 0
	totalUntargeted := 0
	totalDocs := 0

	for rows.Next() {
		var doc DocSummary
		var docType string
		var content string
		var untargetedCount int
		if err := rows.Scan(&doc.ID, &docType, &doc.Title, &content, &doc.TargetedPendingProposals, &untargetedCount); err != nil {
			return ProjectDocsResponse{}, err
		}
		doc.DocType = docType
		doc.ContentPreview = summarizeContent(content)
		totalDocs++
		totalTargeted += doc.TargetedPendingProposals
		group, exists := groupMap[docType]
		if !exists {
			group = &DocGroup{DocType: docType}
			groupMap[docType] = group
			groupOrder = append(groupOrder, docType)
		}
		group.Docs = append(group.Docs, doc)
		group.TargetedPendingCount += doc.TargetedPendingProposals
		group.UntargetedPendingCount = untargetedCount
	}
	if err := rows.Err(); err != nil {
		return ProjectDocsResponse{}, err
	}

	groups := make([]DocGroup, 0, len(groupOrder))
	for _, docType := range groupOrder {
		group := groupMap[docType]
		group.PendingProposalCount = group.TargetedPendingCount + group.UntargetedPendingCount
		totalUntargeted += group.UntargetedPendingCount
		groups = append(groups, *group)
	}

	return ProjectDocsResponse{
		Project: ProjectHeader{ID: project.ID, Name: project.Name, CWD: project.CWD},
		Docs: DocsSummary{
			TotalDocs:                      totalDocs,
			Groups:                         groups,
			PendingProposalCount:           totalTargeted + totalUntargeted,
			TargetedPendingProposalCount:   totalTargeted,
			UntargetedPendingProposalCount: totalUntargeted,
		},
	}, nil
}
func (r *Repository) SetProposalStatus(ctx context.Context, proposalID int, input SetProposalStatusInput) (SessionActionResponse, error) {
	targetStatus := strings.TrimSpace(input.Status)
	if targetStatus != "approved" && targetStatus != "rejected" {
		return SessionActionResponse{}, fmt.Errorf("invalid status: must be 'approved' or 'rejected'")
	}
	var sessionID, projectID int
	if err := r.db.QueryRowContext(ctx, "SELECT session_id, project_id FROM doc_proposal WHERE id = ?", proposalID).Scan(&sessionID, &projectID); err != nil {
		return SessionActionResponse{}, err
	}
	if err := r.requireProjectDocLocalizationCoverage(ctx, projectID); err != nil {
		return SessionActionResponse{}, err
	}
	if targetStatus == "rejected" {
		if _, err := r.dbWrite.ExecContext(ctx, "UPDATE doc_proposal SET status = ? WHERE id = ?", targetStatus, proposalID); err != nil {
			return SessionActionResponse{}, err
		}
		return r.loadSessionActionResponse(ctx, sessionID, "success", "Proposal rejected.")
	}
	tx, err := r.dbWrite.BeginTx(ctx, nil)
	if err != nil {
		return SessionActionResponse{}, err
	}
	defer func() {
		_ = tx.Rollback()
	}()
	var proposedContent, proposedDocType, proposedLocalizedTitle string
	var targetProjectDocID sql.NullInt64
	localizedTitleExpr := "''"
	var localizedTitleColumnCount int
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM pragma_table_info('doc_proposal') WHERE name = 'proposed_localized_title'").Scan(&localizedTitleColumnCount); err != nil {
		return SessionActionResponse{}, err
	}
	if localizedTitleColumnCount == 1 {
		localizedTitleExpr = "COALESCE(proposed_localized_title, '')"
	}
	if err := tx.QueryRowContext(
		ctx,
		fmt.Sprintf("SELECT proposed_content, COALESCE(proposed_doc_type, 'documentation'), %s, target_project_doc_id FROM doc_proposal WHERE id = ? AND project_id = ?", localizedTitleExpr),
		proposalID,
		projectID,
	).Scan(&proposedContent, &proposedDocType, &proposedLocalizedTitle, &targetProjectDocID); err != nil {
		return SessionActionResponse{}, err
	}
	if targetProjectDocID.Valid {
		res, err := tx.ExecContext(
			ctx,
			"UPDATE project_doc SET content = ?, doc_type = ? WHERE id = ? AND project_id = ?",
			proposedContent,
			proposedDocType,
			targetProjectDocID.Int64,
			projectID,
		)
		if err != nil {
			return SessionActionResponse{}, err
		}
		rowsAffected, err := res.RowsAffected()
		if err != nil {
			return SessionActionResponse{}, err
		}
		if rowsAffected == 0 {
			return SessionActionResponse{}, fmt.Errorf("target_project_doc_id no longer exists in current project")
		}
	} else {
		matchingDocIDs, err := r.matchingDocIDsByTypeTx(ctx, tx, projectID, proposedDocType)
		if err != nil {
			return SessionActionResponse{}, err
		}
		switch len(matchingDocIDs) {
		case 0:
			language, err := r.projectDocLanguage(ctx, projectID)
			if err != nil {
				return SessionActionResponse{}, err
			}
			if dashboardProjectDocLocalizationRequired(language) && strings.TrimSpace(proposedLocalizedTitle) == "" {
				return SessionActionResponse{}, fmt.Errorf("proposed_localized_title is required for language %q", language)
			}
			res, err := tx.ExecContext(
				ctx,
				"INSERT INTO project_doc (project_id, title, content, doc_type) VALUES (?, ?, ?, ?)",
				projectID,
				"Documentation ("+proposedDocType+")",
				proposedContent,
				proposedDocType,
			)
			if err != nil {
				return SessionActionResponse{}, err
			}
			newDocID, err := res.LastInsertId()
			if err != nil {
				return SessionActionResponse{}, err
			}
			if dashboardProjectDocLocalizationRequired(language) {
				if _, err := tx.ExecContext(ctx, `
					INSERT INTO project_doc_title_localization (
						project_doc_id, language_code, localized_title, created_at, updated_at
					) VALUES (?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
					ON CONFLICT(project_doc_id, language_code) DO UPDATE SET
						localized_title = excluded.localized_title,
						updated_at = CURRENT_TIMESTAMP`, newDocID, language, strings.TrimSpace(proposedLocalizedTitle)); err != nil {
					return SessionActionResponse{}, err
				}
			}
		case 1:
			if _, err := tx.ExecContext(
				ctx,
				"UPDATE project_doc SET content = ?, doc_type = ? WHERE id = ? AND project_id = ?",
				proposedContent,
				proposedDocType,
				matchingDocIDs[0],
				projectID,
			); err != nil {
				return SessionActionResponse{}, err
			}
		default:
			return SessionActionResponse{}, fmt.Errorf("proposal %d approval is ambiguous for doc_type %q; resubmit with target_project_doc_id", proposalID, proposedDocType)
		}
	}
	if _, err := tx.ExecContext(ctx, "UPDATE doc_proposal SET status = 'approved' WHERE id = ? AND project_id = ?", proposalID, projectID); err != nil {
		return SessionActionResponse{}, err
	}
	if err := tx.Commit(); err != nil {
		return SessionActionResponse{}, err
	}
	return r.loadSessionActionResponse(ctx, sessionID, "success", "Proposal approved and project docs updated.")
}
func (r *Repository) loadAttachedDocs(ctx context.Context, sessionID int) ([]AttachedDoc, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT
			(
				SELECT COUNT(*)
				FROM project_doc d2
				WHERE d2.project_id = d.project_id AND d2.id <= d.id
			) AS local_doc_id,
			d.title,
			COALESCE(d.doc_type, 'documentation'),
			d.content
		FROM netrunner_attached_doc ad
		INNER JOIN project_doc d ON d.id = ad.project_doc_id
		WHERE ad.session_id = ?
		ORDER BY d.id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	docs := []AttachedDoc{}
	for rows.Next() {
		var doc AttachedDoc
		var content string
		if err := rows.Scan(&doc.ID, &doc.Title, &doc.DocType, &content); err != nil {
			return nil, err
		}
		doc.Summary = summarizeContent(content)
		docs = append(docs, doc)
	}
	return docs, rows.Err()
}
func (r *Repository) loadSessionProposals(ctx context.Context, projectID int, sessionID int) ([]DocProposalSummary, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT
			p.id,
			(
				SELECT COUNT(*)
				FROM doc_proposal p2
				WHERE p2.project_id = p.project_id AND p2.id <= p.id
			) AS local_proposal_id,
			p.status,
			COALESCE(p.proposed_doc_type, 'documentation'),
			p.proposed_content,
			COALESCE((
				SELECT COUNT(*)
				FROM project_doc d2
				WHERE d2.project_id = p.project_id AND d2.id <= p.target_project_doc_id
			), 0)
		FROM doc_proposal p
		WHERE p.project_id = ? AND p.session_id = ?
		ORDER BY p.id`, projectID, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	proposals := []DocProposalSummary{}
	for rows.Next() {
		var proposal DocProposalSummary
		if err := rows.Scan(&proposal.ID, &proposal.LocalID, &proposal.Status, &proposal.ProposedDocType, &proposal.ProposedContent, &proposal.TargetProjectDocID); err != nil {
			return nil, err
		}
		proposals = append(proposals, proposal)
	}
	return proposals, rows.Err()
}

func (r *Repository) loadProjectAttachedDocOptions(ctx context.Context, projectID int) ([]AttachedDoc, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT
			(
				SELECT COUNT(*)
				FROM project_doc d2
				WHERE d2.project_id = d.project_id AND d2.id <= d.id
			) AS local_doc_id,
			d.title,
			COALESCE(d.doc_type, 'documentation'),
			d.content
		FROM project_doc d
		WHERE d.project_id = ?
		ORDER BY d.id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	docs := []AttachedDoc{}
	for rows.Next() {
		var doc AttachedDoc
		var content string
		if err := rows.Scan(&doc.ID, &doc.Title, &doc.DocType, &content); err != nil {
			return nil, err
		}
		doc.Summary = summarizeContent(content)
		docs = append(docs, doc)
	}
	return docs, rows.Err()
}
func (r *Repository) resolveProjectDocIDs(ctx context.Context, projectID int, localDocIDs []int) ([]int, error) {
	normalized := normalizeIntIDs(localDocIDs)
	globalDocIDs := make([]int, 0, len(normalized))
	missing := make([]string, 0)
	for _, localDocID := range normalized {
		var globalDocID int
		err := r.db.QueryRowContext(ctx, `
			SELECT d.id
			FROM project_doc d
			WHERE d.project_id = ?
			AND (
				SELECT COUNT(*)
				FROM project_doc d2
				WHERE d2.project_id = d.project_id AND d2.id <= d.id
			) = ?`, projectID, localDocID).Scan(&globalDocID)
		if err == sql.ErrNoRows {
			missing = append(missing, fmt.Sprintf("%d", localDocID))
			continue
		}
		if err != nil {
			return nil, err
		}
		globalDocIDs = append(globalDocIDs, globalDocID)
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("unknown project_doc_id(s): %s", strings.Join(missing, ", "))
	}
	return globalDocIDs, nil
}
func (r *Repository) matchingDocIDsByTypeTx(ctx context.Context, tx *sql.Tx, projectID int, proposedDocType string) ([]int, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id
		FROM project_doc
		WHERE project_id = ? AND COALESCE(doc_type, 'documentation') = ?
		ORDER BY id
		LIMIT 2`, projectID, proposedDocType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	matchingDocIDs := make([]int, 0, 2)
	for rows.Next() {
		var docID int
		if err := rows.Scan(&docID); err != nil {
			return nil, err
		}
		matchingDocIDs = append(matchingDocIDs, docID)
	}
	return matchingDocIDs, rows.Err()
}
