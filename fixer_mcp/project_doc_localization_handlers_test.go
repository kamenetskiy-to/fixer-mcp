package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/glebarez/go-sqlite"
)

func setupProjectDocLocalizationTestDB(t *testing.T) *sql.DB {
	t.Helper()
	testDB, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "localization.db"))
	if err != nil {
		t.Fatalf("open localization database: %v", err)
	}
	if _, err := testDB.Exec(`
		PRAGMA foreign_keys = ON;
		CREATE TABLE project (id INTEGER PRIMARY KEY, name TEXT NOT NULL, cwd TEXT NOT NULL UNIQUE);
		CREATE TABLE project_doc (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL,
			title TEXT NOT NULL,
			content TEXT NOT NULL,
			doc_type TEXT DEFAULT 'documentation',
			parent_doc_id INTEGER,
			level INTEGER NOT NULL DEFAULT 0,
			slug TEXT NOT NULL DEFAULT '',
			path TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'current'
		);
		CREATE TABLE project_doc_language_policy (
			project_id INTEGER PRIMARY KEY,
			language_code TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE project_doc_title_localization (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_doc_id INTEGER NOT NULL,
			language_code TEXT NOT NULL,
			localized_title TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(project_doc_id, language_code)
		);
		INSERT INTO project VALUES (1, 'Localized Project', '/tmp/localized-project');
		INSERT INTO project_doc_language_policy (project_id, language_code) VALUES (1, 'ru');
		INSERT INTO project_doc (project_id, title, content, level, slug, path, status) VALUES
			(1, 'Architecture', 'English architecture content.', 0, 'architecture', 'architecture', 'current'),
			(1, 'Runtime', 'English runtime content.', 0, 'runtime', 'runtime', 'current');
		INSERT INTO project_doc_title_localization (project_doc_id, language_code, localized_title)
		VALUES (1, 'ru', 'Архитектура');
	`); err != nil {
		_ = testDB.Close()
		t.Fatalf("seed localization database: %v", err)
	}
	return testDB
}

func TestProjectDocToolsFailClosedUntilRussianTitlesAreComplete(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
	}()

	testDB := setupProjectDocLocalizationTestDB(t)
	defer testDB.Close()
	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1

	result, _, err := GetProjectDocs(context.Background(), nil, GetProjectDocsInput{})
	if err == nil || result == nil || !result.IsError || !strings.Contains(err.Error(), "localization is incomplete") {
		t.Fatalf("expected fail-closed localization error, result=%+v err=%v", result, err)
	}

	_, status, err := GetProjectDocLocalizationStatus(context.Background(), nil, GetProjectDocLocalizationStatusInput{})
	if err != nil || status.Language != "ru" || status.Missing != 1 || len(status.Docs) != 2 {
		t.Fatalf("unexpected localization status: status=%+v err=%v", status, err)
	}

	_, updated, err := SetProjectDocTitleLocalizations(context.Background(), nil, SetProjectDocTitleLocalizationsInput{
		Language: "ru",
		Titles:   []ProjectDocTitleLocalizationInput{{ProjectDocId: 2, LocalizedTitle: "Среда выполнения"}},
	})
	if err != nil || updated.Updated != 1 {
		t.Fatalf("set localized title failed: out=%+v err=%v", updated, err)
	}

	_, docs, err := GetProjectDocs(context.Background(), nil, GetProjectDocsInput{})
	if err != nil || len(docs.Docs) != 2 || docs.Docs[0].Title != "Architecture" {
		t.Fatalf("canonical docs must remain English after localization: docs=%+v err=%v", docs, err)
	}
}

func TestAddProjectDocRequiresAndStoresLocalizedTitleForRussianProject(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
	}()

	testDB := setupProjectDocLocalizationTestDB(t)
	defer testDB.Close()
	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1
	if _, err := testDB.Exec(`INSERT INTO project_doc_title_localization (project_doc_id, language_code, localized_title) VALUES (2, 'ru', 'Среда выполнения')`); err != nil {
		t.Fatalf("complete initial localization: %v", err)
	}

	_, _, err := AddProjectDoc(context.Background(), nil, AddProjectDocInput{Title: "Operations", Content: "English content."})
	if err == nil || !strings.Contains(err.Error(), "localized_title is required") {
		t.Fatalf("expected localized_title requirement, got %v", err)
	}

	_, added, err := AddProjectDoc(context.Background(), nil, AddProjectDocInput{
		Title: "Operations", LocalizedTitle: "Эксплуатация", Content: "English content.",
	})
	if err != nil || added.Id != 3 {
		t.Fatalf("add localized doc failed: out=%+v err=%v", added, err)
	}
	var localizedTitle string
	if err := testDB.QueryRow(`SELECT localized_title FROM project_doc_title_localization WHERE project_doc_id = 3 AND language_code = 'ru'`).Scan(&localizedTitle); err != nil {
		t.Fatalf("read stored localized title: %v", err)
	}
	if localizedTitle != "Эксплуатация" {
		t.Fatalf("unexpected localized title %q", localizedTitle)
	}
}

func TestEnglishProjectDoesNotRequireLocalizedTitles(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
	}()

	testDB := setupProjectDocLocalizationTestDB(t)
	defer testDB.Close()
	db = testDB
	authorizedRole = "fixer"
	authorizedProjectId = 1
	if _, err := testDB.Exec(`UPDATE project_doc_language_policy SET language_code = 'en' WHERE project_id = 1`); err != nil {
		t.Fatalf("set English policy: %v", err)
	}

	if _, _, err := GetProjectDocs(context.Background(), nil, GetProjectDocsInput{}); err != nil {
		t.Fatalf("English project should not require localizations: %v", err)
	}
	if _, _, err := AddProjectDoc(context.Background(), nil, AddProjectDocInput{Title: "English Only", Content: "English content."}); err != nil {
		t.Fatalf("English project add should not require localized_title: %v", err)
	}
}
