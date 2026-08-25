package dashboardapi

import (
	"context"
	"sort"
	"strings"
	"time"
)

type projectRecord struct {
	ID   int
	Name string
	CWD  string
}

func (r *Repository) loadProjects(ctx context.Context) (map[int]projectRecord, []int, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, name, cwd FROM project`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	projectMap := map[int]projectRecord{}
	order := []int{}
	for rows.Next() {
		var project projectRecord
		if err := rows.Scan(&project.ID, &project.Name, &project.CWD); err != nil {
			return nil, nil, err
		}
		projectMap[project.ID] = project
		order = append(order, project.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	activity, err := r.loadProjectActivity(ctx)
	if err != nil {
		return nil, nil, err
	}
	sort.SliceStable(order, func(i, j int) bool {
		leftAt := parseProjectActivityTime(activity[order[i]].LastActivityAt)
		rightAt := parseProjectActivityTime(activity[order[j]].LastActivityAt)
		if !leftAt.Equal(rightAt) {
			// Empty/invalid timestamps sort after real activity.
			if leftAt.IsZero() {
				return false
			}
			if rightAt.IsZero() {
				return true
			}
			// Newest first.
			return leftAt.After(rightAt)
		}
		return order[i] < order[j]
	})
	return projectMap, order, nil
}

func parseProjectActivityTime(raw string) time.Time {
	text := strings.TrimSpace(raw)
	if text == "" {
		return time.Time{}
	}
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
	} {
		parsed, err := time.Parse(layout, text)
		if err == nil {
			return parsed
		}
		if err != nil {
			// Fallback for local-style timestamps without timezone.
			if parsed, parseErr := time.ParseInLocation(layout, text, time.UTC); parseErr == nil {
				return parsed
			}
		}
	}
	return time.Time{}
}

func (r *Repository) requireProject(ctx context.Context, projectID int) (projectRecord, error) {
	var project projectRecord
	err := r.db.QueryRowContext(ctx, `SELECT id, name, cwd FROM project WHERE id = ?`, projectID).Scan(&project.ID, &project.Name, &project.CWD)
	if err != nil {
		return projectRecord{}, err
	}
	return project, nil
}

func (r *Repository) currentProjectBinding(projects map[int]projectRecord) *ProjectBinding {
	for _, project := range projects {
		if project.CWD == r.currentProjectCWD && project.CWD != "" {
			return &ProjectBinding{ID: project.ID, Name: project.Name, CWD: project.CWD}
		}
	}
	return nil
}
