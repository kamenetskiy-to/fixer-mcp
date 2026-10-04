package dashboardapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// AcceptOrderInput is the control-plane payload sent by the App Server when
// the Architect accepts a client order. ProjectCWD is optional when the
// caller wants dashboard_api to allocate a deterministic local order root.
type AcceptOrderInput struct {
	OrderID           int    `json:"order_id"`
	ProjectID         int    `json:"project_id,omitempty"`
	SessionID         int    `json:"session_id,omitempty"`
	ClientID          string `json:"client_id,omitempty"`
	ClientEmail       string `json:"client_email,omitempty"`
	ClientName        string `json:"client_name,omitempty"`
	ProjectName       string `json:"project_name,omitempty"`
	ProjectCWD        string `json:"project_cwd,omitempty"`
	ProjectRoot       string `json:"project_root,omitempty"`
	SourceProjectCWD  string `json:"source_project_cwd,omitempty"`
	SourceProjectRoot string `json:"source_project_root,omitempty"`
	WorktreeRoot      string `json:"worktree_root,omitempty"`
	BranchName        string `json:"branch_name,omitempty"`
	PreviewProvider   string `json:"preview_provider,omitempty"`
	PreviewTTLSeconds int    `json:"preview_ttl_seconds,omitempty"`
	CWD               string `json:"cwd,omitempty"`
	Title             string `json:"title,omitempty"`
	Description       string `json:"description,omitempty"`
	TaskDescription   string `json:"task_description,omitempty"`
	Revisions         string `json:"revisions,omitempty"`
	RevisionNotes     string `json:"revision_notes,omitempty"`
}

// UnmarshalJSON accepts both the snake_case control-plane contract and the
// camelCase names naturally emitted by Dart callers.
func (input *AcceptOrderInput) UnmarshalJSON(data []byte) error {
	type plain AcceptOrderInput
	var snake plain
	if err := json.Unmarshal(data, &snake); err != nil {
		return err
	}
	*input = AcceptOrderInput(snake)

	var camel struct {
		ID                int    `json:"id"`
		OrderID           int    `json:"orderId"`
		ProjectID         int    `json:"projectId"`
		SessionID         int    `json:"sessionId"`
		ClientID          string `json:"clientId"`
		ClientEmail       string `json:"clientEmail"`
		ClientName        string `json:"clientName"`
		ProjectName       string `json:"projectName"`
		ProjectCWD        string `json:"projectCwd"`
		ProjectRoot       string `json:"projectRoot"`
		SourceProjectCWD  string `json:"sourceProjectCwd"`
		SourceProjectRoot string `json:"sourceProjectRoot"`
		WorktreeRoot      string `json:"worktreeRoot"`
		BranchName        string `json:"branchName"`
		PreviewProvider   string `json:"previewProvider"`
		PreviewTTLSeconds int    `json:"previewTtlSeconds"`
		CWD               string `json:"cwd"`
		Title             string `json:"title"`
		Description       string `json:"description"`
		TaskDescription   string `json:"taskDescription"`
		Revisions         string `json:"revisions"`
		RevisionNotes     string `json:"revisionNotes"`
	}
	if err := json.Unmarshal(data, &camel); err != nil {
		return err
	}
	if input.OrderID == 0 {
		input.OrderID = camel.OrderID
		if input.OrderID == 0 {
			input.OrderID = camel.ID
		}
	}
	if input.ClientID == "" {
		input.ClientID = camel.ClientID
	}
	if input.ProjectID == 0 {
		input.ProjectID = camel.ProjectID
	}
	if input.SessionID == 0 {
		input.SessionID = camel.SessionID
	}
	if input.ClientEmail == "" {
		input.ClientEmail = camel.ClientEmail
	}
	if input.ClientName == "" {
		input.ClientName = camel.ClientName
	}
	if input.ProjectName == "" {
		input.ProjectName = camel.ProjectName
	}
	if input.ProjectCWD == "" {
		input.ProjectCWD = camel.ProjectCWD
	}
	if input.ProjectRoot == "" {
		input.ProjectRoot = camel.ProjectRoot
	}
	if input.SourceProjectCWD == "" {
		input.SourceProjectCWD = camel.SourceProjectCWD
	}
	if input.SourceProjectRoot == "" {
		input.SourceProjectRoot = camel.SourceProjectRoot
	}
	if input.WorktreeRoot == "" {
		input.WorktreeRoot = camel.WorktreeRoot
	}
	if input.BranchName == "" {
		input.BranchName = camel.BranchName
	}
	if input.PreviewProvider == "" {
		input.PreviewProvider = camel.PreviewProvider
	}
	if input.PreviewTTLSeconds == 0 {
		input.PreviewTTLSeconds = camel.PreviewTTLSeconds
	}
	if input.CWD == "" {
		input.CWD = camel.CWD
	}
	if input.Title == "" {
		input.Title = camel.Title
	}
	if input.Description == "" {
		input.Description = camel.Description
	}
	if input.TaskDescription == "" {
		input.TaskDescription = camel.TaskDescription
	}
	if input.Revisions == "" {
		input.Revisions = camel.Revisions
	}
	if input.RevisionNotes == "" {
		input.RevisionNotes = camel.RevisionNotes
	}
	return nil
}

type AcceptOrderResponse struct {
	Status          string            `json:"status"`
	OrderID         int               `json:"order_id"`
	Project         ProjectBinding    `json:"project"`
	ProjectID       int               `json:"project_id"`
	SessionID       int               `json:"session_id"`
	TaskDescription string            `json:"task_description"`
	ProjectReused   bool              `json:"project_reused"`
	Sandbox         SandboxDescriptor `json:"sandbox"`
	WorktreePath    string            `json:"worktree_path,omitempty"`
	BranchName      string            `json:"branch_name,omitempty"`
	PreviewURL      string            `json:"preview_url,omitempty"`
}

// AcceptOrder creates the durable Fixer MCP project/session projection for an
// accepted client order. Project identity is the normalized project cwd, so a
// retry of the same order reuses the existing project row without creating a
// second project.
func (r *Repository) AcceptOrder(ctx context.Context, input AcceptOrderInput) (AcceptOrderResponse, error) {
	if input.OrderID <= 0 {
		return AcceptOrderResponse{}, fmt.Errorf("order_id is required")
	}

	taskDescription := orderTaskDescription(input)
	if taskDescription == "" {
		return AcceptOrderResponse{}, fmt.Errorf("order task description is required")
	}

	if err := ensureSandboxTable(ctx, r.dbWrite); err != nil {
		return AcceptOrderResponse{}, fmt.Errorf("prepare sandbox storage: %w", err)
	}
	sourceProjectCWD, _, err := orderSourceProjectCWD(ctx, r, input)
	if err != nil {
		return AcceptOrderResponse{}, err
	}
	projectCWD, err := orderProjectCWD(input, sourceProjectCWD)
	if err != nil {
		return AcceptOrderResponse{}, err
	}
	descriptor, worktreeCreated, err := provisionOrderSandbox(ctx, input, sourceProjectCWD, projectCWD)
	if err != nil {
		return AcceptOrderResponse{}, err
	}
	cleanupWorktree := func() {
		if !worktreeCreated {
			return
		}
		_, _ = runGit(context.Background(), sourceProjectCWD, "worktree", "remove", "--force", projectCWD)
	}
	defer func() {
		if worktreeCreated {
			// A successful commit clears this flag below. Any early return cleans
			// up the filesystem half of the operation as well.
			cleanupWorktree()
		}
	}()
	projectName := orderProjectName(input)

	tx, err := r.dbWrite.BeginTx(ctx, nil)
	if err != nil {
		return AcceptOrderResponse{}, err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var project projectRecord
	projectReused := true
	err = tx.QueryRowContext(ctx, `SELECT id, name, cwd FROM project WHERE cwd = ?`, projectCWD).
		Scan(&project.ID, &project.Name, &project.CWD)
	if err == sql.ErrNoRows {
		result, insertErr := tx.ExecContext(ctx,
			`INSERT INTO project (name, cwd) VALUES (?, ?)`,
			projectName,
			projectCWD,
		)
		if insertErr != nil {
			return AcceptOrderResponse{}, insertErr
		}
		projectID, insertErr := result.LastInsertId()
		if insertErr != nil {
			return AcceptOrderResponse{}, insertErr
		}
		project = projectRecord{ID: int(projectID), Name: projectName, CWD: projectCWD}
		projectReused = false
	} else if err != nil {
		return AcceptOrderResponse{}, err
	}

	result, err := tx.ExecContext(ctx,
		`INSERT INTO session (project_id, task_description, status)
		 VALUES (?, ?, 'pending')`,
		project.ID,
		taskDescription,
	)
	if err != nil {
		return AcceptOrderResponse{}, err
	}
	sessionID, err := result.LastInsertId()
	if err != nil {
		return AcceptOrderResponse{}, err
	}
	descriptor, err = r.insertOrderSandbox(ctx, tx, input, project.ID, int(sessionID), descriptor)
	if err != nil {
		return AcceptOrderResponse{}, err
	}
	if err := tx.Commit(); err != nil {
		return AcceptOrderResponse{}, err
	}
	worktreeCreated = false

	return AcceptOrderResponse{
		Status:          "success",
		OrderID:         input.OrderID,
		Project:         ProjectBinding{ID: project.ID, Name: project.Name, CWD: project.CWD},
		ProjectID:       project.ID,
		SessionID:       int(sessionID),
		TaskDescription: taskDescription,
		ProjectReused:   projectReused,
		Sandbox:         descriptor,
		WorktreePath:    descriptor.WorktreePath,
		BranchName:      descriptor.BranchName,
		PreviewURL:      descriptor.Preview.URL,
	}, nil
}

func orderProjectName(input AcceptOrderInput) string {
	if name := strings.TrimSpace(input.ProjectName); name != "" {
		return name
	}
	if title := strings.TrimSpace(input.Title); title != "" {
		return title
	}
	if name := strings.TrimSpace(input.ClientName); name != "" {
		return name
	}
	return fmt.Sprintf("Order %d", input.OrderID)
}

func orderTaskDescription(input AcceptOrderInput) string {
	task := strings.TrimSpace(input.TaskDescription)
	if task == "" {
		parts := []string{}
		if title := strings.TrimSpace(input.Title); title != "" {
			parts = append(parts, title)
		}
		if description := strings.TrimSpace(input.Description); description != "" {
			parts = append(parts, description)
		}
		task = strings.Join(parts, "\n\n")
	}
	revisions := strings.TrimSpace(input.Revisions)
	if revisions == "" {
		revisions = strings.TrimSpace(input.RevisionNotes)
	}
	if revisions != "" {
		if task != "" {
			task += "\n\n"
		}
		task += "Revisions:\n" + revisions
	}
	return strings.TrimSpace(task)
}
