package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	listProjectSessionsDefaultLimit = 20
	listProjectSessionsMaxLimit     = 100
	sessionSearchSummaryMaxRunes    = 220
)

type ListProjectSessionsInput struct {
	Status   string `json:"status,omitempty" jsonschema:"Optional exact session status filter: pending, in_progress, review, or completed."`
	Search   string `json:"search,omitempty" jsonschema:"Optional substring filter matched against task_description and report (SQLite LIKE, case-insensitive for ASCII)."`
	Backend  string `json:"cli_backend,omitempty" jsonschema:"Optional exact cli_backend filter, for example codex or claude."`
	WaveOnly bool   `json:"wave_only,omitempty" jsonschema:"When true, only return sessions linked to a parallel Netrunner wave worker."`
	Limit    int    `json:"limit,omitempty" jsonschema:"Maximum rows to return. Defaults to 20; maximum 100."`
	Offset   int    `json:"offset,omitempty" jsonschema:"Rows to skip for pagination, ordered by most recent session first. Defaults to 0."`
}

type ProjectSessionSummary struct {
	Id               int    `json:"id"`
	Status           string `json:"status"`
	TaskSummary      string `json:"task_summary"`
	CliBackend       string `json:"cli_backend"`
	CliModel         string `json:"cli_model,omitempty"`
	CliReasoning     string `json:"cli_reasoning,omitempty"`
	SessionKind      string `json:"session_kind,omitempty"`
	ReworkCount      int    `json:"rework_count"`
	ForcedStopCount  int    `json:"forced_stop_count"`
	WaveId           int    `json:"wave_id,omitempty"`
	WaveWorkerStatus string `json:"wave_worker_status,omitempty"`
	CreatedAt        string `json:"created_at,omitempty"`
	UpdatedAt        string `json:"updated_at,omitempty"`
}

type ListProjectSessionsOutput struct {
	ProjectId int                     `json:"project_id"`
	Sessions  []ProjectSessionSummary `json:"sessions"`
	Limit     int                     `json:"limit"`
	Offset    int                     `json:"offset"`
	Returned  int                     `json:"returned"`
	HasMore   bool                    `json:"has_more"`
}

func normalizeSearchListLimit(limit, defaultLimit, maxLimit int) int {
	if limit <= 0 {
		return defaultLimit
	}
	if limit > maxLimit {
		return maxLimit
	}
	return limit
}

func normalizeSearchListOffset(offset int) int {
	if offset < 0 {
		return 0
	}
	return offset
}

func escapeSQLLikePattern(raw string) string {
	replacer := strings.NewReplacer(
		`\`, `\\`,
		`%`, `\%`,
		`_`, `\_`,
	)
	return replacer.Replace(raw)
}

func ListProjectSessions(ctx context.Context, req *mcp.CallToolRequest, input ListProjectSessionsInput) (*mcp.CallToolResult, ListProjectSessionsOutput, error) {
	if authorizedRole != "fixer" {
		return &mcp.CallToolResult{IsError: true}, ListProjectSessionsOutput{}, fmt.Errorf("access denied: requires fixer role")
	}
	if authorizedProjectId <= 0 {
		return &mcp.CallToolResult{IsError: true}, ListProjectSessionsOutput{}, fmt.Errorf("access denied: fixer role is not bound to a project")
	}

	status := strings.TrimSpace(input.Status)
	if status != "" && !isValidSessionStatus(status) {
		return &mcp.CallToolResult{IsError: true}, ListProjectSessionsOutput{}, fmt.Errorf("status must be one of pending, in_progress, review, completed")
	}
	backend := strings.TrimSpace(input.Backend)
	search := strings.TrimSpace(input.Search)

	limit := normalizeSearchListLimit(input.Limit, listProjectSessionsDefaultLimit, listProjectSessionsMaxLimit)
	offset := normalizeSearchListOffset(input.Offset)

	var query strings.Builder
	query.WriteString(`SELECT
		(SELECT COUNT(*) FROM session ranked WHERE ranked.project_id = s.project_id AND ranked.id <= s.id) AS local_session_id,
		s.status,
		s.task_description,
		COALESCE(NULLIF(TRIM(s.cli_backend), ''), ?),
		COALESCE(s.cli_model, ''),
		COALESCE(s.cli_reasoning, ''),
		COALESCE(NULLIF(TRIM(s.session_kind), ''), 'netrunner'),
		COALESCE(s.rework_count, 0),
		COALESCE(s.forced_stop_count, 0),
		COALESCE(w.wave_id, 0),
		COALESCE(w.status, ''),
		COALESCE(s.created_at, ''),
		COALESCE(s.updated_at, '')
	 FROM session s
	 LEFT JOIN parallel_wave_worker w ON w.id = (
		SELECT inner_w.id FROM parallel_wave_worker inner_w
		WHERE inner_w.session_id = s.id
		ORDER BY inner_w.id DESC
		LIMIT 1
	 )
	 WHERE s.project_id = ?`)

	args := []any{defaultCliBackend, authorizedProjectId}

	if status != "" {
		query.WriteString(" AND s.status = ?")
		args = append(args, status)
	}
	if backend != "" {
		query.WriteString(" AND COALESCE(NULLIF(TRIM(s.cli_backend), ''), ?) = ?")
		args = append(args, defaultCliBackend, backend)
	}
	if search != "" {
		query.WriteString(" AND (s.task_description LIKE ? ESCAPE '\\' OR s.report LIKE ? ESCAPE '\\')")
		pattern := "%" + escapeSQLLikePattern(search) + "%"
		args = append(args, pattern, pattern)
	}
	if input.WaveOnly {
		query.WriteString(" AND w.wave_id IS NOT NULL")
	}

	query.WriteString(" ORDER BY s.id DESC LIMIT ? OFFSET ?")
	args = append(args, limit+1, offset)

	rows, err := db.Query(query.String(), args...)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, ListProjectSessionsOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	defer rows.Close()

	sessions := []ProjectSessionSummary{}
	for rows.Next() {
		var (
			item        ProjectSessionSummary
			taskSummary string
		)
		if err := rows.Scan(
			&item.Id,
			&item.Status,
			&taskSummary,
			&item.CliBackend,
			&item.CliModel,
			&item.CliReasoning,
			&item.SessionKind,
			&item.ReworkCount,
			&item.ForcedStopCount,
			&item.WaveId,
			&item.WaveWorkerStatus,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return &mcp.CallToolResult{IsError: true}, ListProjectSessionsOutput{}, fmt.Errorf("DB scan error: %v", err)
		}
		item.TaskSummary = truncateRunes(normalizeCompactText(taskSummary), sessionSearchSummaryMaxRunes)
		sessions = append(sessions, item)
	}
	if err := rows.Err(); err != nil {
		return &mcp.CallToolResult{IsError: true}, ListProjectSessionsOutput{}, fmt.Errorf("DB rows error: %v", err)
	}

	hasMore := len(sessions) > limit
	if hasMore {
		sessions = sessions[:limit]
	}

	return nil, ListProjectSessionsOutput{
		ProjectId: authorizedProjectId,
		Sessions:  sessions,
		Limit:     limit,
		Offset:    offset,
		Returned:  len(sessions),
		HasMore:   hasMore,
	}, nil
}
