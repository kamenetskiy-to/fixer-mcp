package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	listFeedbackDefaultLimit = 20
	listFeedbackMaxLimit     = 100
)

type SubmitFixerMcpFeedbackInput struct {
	ProjectID    *int   `json:"project_id,omitempty"`
	FeedbackType string `json:"feedback_type"`
	Content      string `json:"content"`
}

type SubmitFixerMcpFeedbackOutput struct {
	FeedbackID int    `json:"feedback_id"`
	Status     string `json:"status"`
}

func SubmitFixerMcpFeedback(ctx context.Context, req *mcp.CallToolRequest, input SubmitFixerMcpFeedbackInput) (*mcp.CallToolResult, SubmitFixerMcpFeedbackOutput, error) {
	if authorizedRole != "fixer" && authorizedRole != "overseer" {
		return &mcp.CallToolResult{IsError: true}, SubmitFixerMcpFeedbackOutput{}, fmt.Errorf("access denied: requires fixer or overseer role. current role: %s", authorizedRole)
	}

	targetProjectID := 0
	if authorizedRole == "overseer" {
		if input.ProjectID == nil {
			return &mcp.CallToolResult{IsError: true}, SubmitFixerMcpFeedbackOutput{}, fmt.Errorf("project_id is required for overseer role")
		}
		targetProjectID = *input.ProjectID
	} else {
		if authorizedProjectId <= 0 {
			return &mcp.CallToolResult{IsError: true}, SubmitFixerMcpFeedbackOutput{}, fmt.Errorf("fixer must be bound to a project to submit feedback")
		}
		targetProjectID = authorizedProjectId
	}

	if input.FeedbackType == "" {
		return &mcp.CallToolResult{IsError: true}, SubmitFixerMcpFeedbackOutput{}, fmt.Errorf("feedback_type is required")
	}
	if input.Content == "" {
		return &mcp.CallToolResult{IsError: true}, SubmitFixerMcpFeedbackOutput{}, fmt.Errorf("content is required")
	}

	var newID int
	err := db.QueryRow("INSERT INTO fixer_mcp_feedback (project_id, feedback_type, content) VALUES (?, ?, ?) RETURNING id",
		targetProjectID, input.FeedbackType, input.Content).Scan(&newID)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SubmitFixerMcpFeedbackOutput{}, fmt.Errorf("failed to insert feedback: %v", err)
	}

	return nil, SubmitFixerMcpFeedbackOutput{
		FeedbackID: newID,
		Status:     "success",
	}, nil
}

type ListFixerMcpFeedbackInput struct {
	ProjectId    *int   `json:"project_id,omitempty" jsonschema:"Optional project filter. The Fixer MCP project Fixer and overseer may omit this to search all projects; other Fixers always read their bound project."`
	FeedbackType string `json:"feedback_type,omitempty" jsonschema:"Optional exact feedback_type filter."`
	Search       string `json:"search,omitempty" jsonschema:"Optional substring filter matched against content (SQLite LIKE, case-insensitive for ASCII)."`
	FromDate     string `json:"from_date,omitempty" jsonschema:"Optional inclusive lower bound on created_at date (YYYY-MM-DD)."`
	ToDate       string `json:"to_date,omitempty" jsonschema:"Optional inclusive upper bound on created_at date (YYYY-MM-DD)."`
	Limit        int    `json:"limit,omitempty" jsonschema:"Maximum rows to return. Defaults to 20; maximum 100."`
	Offset       int    `json:"offset,omitempty" jsonschema:"Rows to skip for pagination, ordered by most recent feedback first. Defaults to 0."`
}

type FixerMcpFeedbackItem struct {
	Id           int    `json:"id"`
	ProjectId    int    `json:"project_id"`
	FeedbackType string `json:"feedback_type"`
	Content      string `json:"content"`
	CreatedAt    string `json:"created_at"`
}

type ListFixerMcpFeedbackOutput struct {
	ProjectId int                    `json:"project_id"`
	Feedback  []FixerMcpFeedbackItem `json:"feedback"`
	Limit     int                    `json:"limit"`
	Offset    int                    `json:"offset"`
	Returned  int                    `json:"returned"`
	HasMore   bool                   `json:"has_more"`
}

func isValidFeedbackDate(value string) bool {
	_, err := time.Parse("2006-01-02", value)
	return err == nil
}

func ListFixerMcpFeedback(ctx context.Context, req *mcp.CallToolRequest, input ListFixerMcpFeedbackInput) (*mcp.CallToolResult, ListFixerMcpFeedbackOutput, error) {
	if authorizedRole != "fixer" && authorizedRole != "overseer" {
		return &mcp.CallToolResult{IsError: true}, ListFixerMcpFeedbackOutput{}, fmt.Errorf("access denied: requires fixer or overseer role. current role: %s", authorizedRole)
	}

	targetProjectID := 0
	if authorizedRole == "fixer" {
		if authorizedProjectId <= 0 {
			return &mcp.CallToolResult{IsError: true}, ListFixerMcpFeedbackOutput{}, fmt.Errorf("fixer must be bound to a project to list feedback")
		}
		projectName, err := projectNameFromID(authorizedProjectId)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, ListFixerMcpFeedbackOutput{}, fmt.Errorf("failed to resolve fixer project: %v", err)
		}
		if strings.EqualFold(strings.TrimSpace(projectName), "Fixer MCP") {
			if input.ProjectId != nil {
				if *input.ProjectId <= 0 {
					return &mcp.CallToolResult{IsError: true}, ListFixerMcpFeedbackOutput{}, fmt.Errorf("project_id must be positive")
				}
				targetProjectID = *input.ProjectId
			}
		} else {
			targetProjectID = authorizedProjectId
		}
	} else if input.ProjectId != nil {
		if *input.ProjectId <= 0 {
			return &mcp.CallToolResult{IsError: true}, ListFixerMcpFeedbackOutput{}, fmt.Errorf("project_id must be positive")
		}
		targetProjectID = *input.ProjectId
	}

	feedbackType := strings.TrimSpace(input.FeedbackType)
	search := strings.TrimSpace(input.Search)
	fromDate := strings.TrimSpace(input.FromDate)
	toDate := strings.TrimSpace(input.ToDate)
	if fromDate != "" && !isValidFeedbackDate(fromDate) {
		return &mcp.CallToolResult{IsError: true}, ListFixerMcpFeedbackOutput{}, fmt.Errorf("from_date must be YYYY-MM-DD")
	}
	if toDate != "" && !isValidFeedbackDate(toDate) {
		return &mcp.CallToolResult{IsError: true}, ListFixerMcpFeedbackOutput{}, fmt.Errorf("to_date must be YYYY-MM-DD")
	}

	limit := normalizeSearchListLimit(input.Limit, listFeedbackDefaultLimit, listFeedbackMaxLimit)
	offset := normalizeSearchListOffset(input.Offset)

	var query strings.Builder
	query.WriteString(`SELECT id, project_id, feedback_type, content, COALESCE(NULLIF(TRIM(created_at), ''), '')
		FROM fixer_mcp_feedback`)

	var conditions []string
	var args []any
	if targetProjectID > 0 {
		conditions = append(conditions, "project_id = ?")
		args = append(args, targetProjectID)
	}
	if feedbackType != "" {
		conditions = append(conditions, "feedback_type = ?")
		args = append(args, feedbackType)
	}
	if search != "" {
		conditions = append(conditions, "content LIKE ? ESCAPE '\\'")
		args = append(args, "%"+escapeSQLLikePattern(search)+"%")
	}
	if fromDate != "" {
		conditions = append(conditions, "date(created_at) >= ?")
		args = append(args, fromDate)
	}
	if toDate != "" {
		conditions = append(conditions, "date(created_at) <= ?")
		args = append(args, toDate)
	}
	if len(conditions) > 0 {
		query.WriteString(" WHERE ")
		query.WriteString(strings.Join(conditions, " AND "))
	}
	query.WriteString(" ORDER BY id DESC LIMIT ? OFFSET ?")
	args = append(args, limit+1, offset)

	rows, err := db.Query(query.String(), args...)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, ListFixerMcpFeedbackOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	defer rows.Close()

	feedback := []FixerMcpFeedbackItem{}
	for rows.Next() {
		var item FixerMcpFeedbackItem
		if err := rows.Scan(&item.Id, &item.ProjectId, &item.FeedbackType, &item.Content, &item.CreatedAt); err != nil {
			return &mcp.CallToolResult{IsError: true}, ListFixerMcpFeedbackOutput{}, fmt.Errorf("DB scan error: %v", err)
		}
		feedback = append(feedback, item)
	}
	if err := rows.Err(); err != nil {
		return &mcp.CallToolResult{IsError: true}, ListFixerMcpFeedbackOutput{}, fmt.Errorf("DB rows error: %v", err)
	}

	hasMore := len(feedback) > limit
	if hasMore {
		feedback = feedback[:limit]
	}

	return nil, ListFixerMcpFeedbackOutput{
		ProjectId: targetProjectID,
		Feedback:  feedback,
		Limit:     limit,
		Offset:    offset,
		Returned:  len(feedback),
		HasMore:   hasMore,
	}, nil
}
