package dashboardapi

import (
	"context"
	"fmt"
	"strings"
)

func (r *Repository) projectDocLanguage(ctx context.Context, projectID int) (string, error) {
	var language string
	err := r.db.QueryRowContext(ctx, `
		SELECT language_code FROM project_doc_language_policy WHERE project_id = ?`, projectID).Scan(&language)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "no such table") {
			return "en", nil
		}
		return "", err
	}
	language = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(language), "_", "-"))
	if language == "" {
		language = "ru"
	}
	return language, nil
}

func dashboardProjectDocLocalizationRequired(language string) bool {
	return strings.SplitN(language, "-", 2)[0] != "en"
}

func (r *Repository) requireProjectDocLocalizationCoverage(ctx context.Context, projectID int) error {
	language, err := r.projectDocLanguage(ctx, projectID)
	if err != nil {
		return err
	}
	if !dashboardProjectDocLocalizationRequired(language) {
		return nil
	}
	var missing int
	err = r.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM project_doc d
		LEFT JOIN project_doc_title_localization l
			ON l.project_doc_id = d.id AND l.language_code = ?
		WHERE d.project_id = ? AND TRIM(COALESCE(l.localized_title, '')) = ''`, language, projectID).Scan(&missing)
	if err != nil {
		return err
	}
	if missing > 0 {
		return fmt.Errorf("project documentation localization is incomplete for language %q: %d title(s) missing", language, missing)
	}
	return nil
}
