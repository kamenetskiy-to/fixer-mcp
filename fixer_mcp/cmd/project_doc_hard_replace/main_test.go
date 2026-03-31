package main

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func setupProjectDocHardReplaceTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if _, err := db.Exec(`
		PRAGMA foreign_keys = ON;
		CREATE TABLE project (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			cwd TEXT UNIQUE NOT NULL
		);
		CREATE TABLE project_doc (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER,
			title TEXT NOT NULL,
			content TEXT NOT NULL,
			doc_type TEXT DEFAULT 'documentation'
		);
		CREATE TABLE session (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL
		);
		CREATE TABLE netrunner_attached_doc (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id INTEGER NOT NULL,
			project_doc_id INTEGER NOT NULL,
			UNIQUE(session_id, project_doc_id),
			FOREIGN KEY(session_id) REFERENCES session(id) ON DELETE CASCADE,
			FOREIGN KEY(project_doc_id) REFERENCES project_doc(id) ON DELETE CASCADE
		);
	`); err != nil {
		_ = db.Close()
		t.Fatalf("seed db: %v", err)
	}
	return db
}

func TestBuildRepoCleanDocsPlanAndApplyReplace_PreservesNonDocumentationDocs(t *testing.T) {
	ctx := context.Background()
	projectRoot := t.TempDir()

	cleanDocsDir := filepath.Join(projectRoot, "project_book", "clean_docs")
	if err := os.MkdirAll(filepath.Join(cleanDocsDir, "30_ops"), 0o755); err != nil {
		t.Fatalf("mkdir clean docs dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(cleanDocsDir, "20_architecture"), 0o755); err != nil {
		t.Fatalf("mkdir clean docs dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cleanDocsDir, "30_ops", "native_telegram_operator_notifications.md"), []byte("# Telegram\n\nDistinct operator note.\n"), 0o644); err != nil {
		t.Fatalf("write clean doc: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cleanDocsDir, "20_architecture", "project_handoff_storage.md"), []byte("# Handoff\n\nDistinct handoff note.\n"), 0o644); err != nil {
		t.Fatalf("write clean doc: %v", err)
	}

	db := setupProjectDocHardReplaceTestDB(t)
	defer func() {
		_ = db.Close()
	}()

	if _, err := db.Exec(
		`INSERT INTO project (id, name, cwd) VALUES (1, 'Alpha', ?);
		 INSERT INTO session (id, project_id) VALUES (1, 1);
		 INSERT INTO project_doc (project_id, title, content, doc_type) VALUES
		 	(1, 'Corrupted A', 'duplicated content', 'documentation'),
		 	(1, 'Corrupted B', 'duplicated content', 'documentation'),
		 	(1, 'Healthy Architecture', 'keep me', 'architecture');
		 INSERT INTO netrunner_attached_doc (session_id, project_doc_id) VALUES
		 	(1, 1),
		 	(1, 3);`,
		projectRoot,
	); err != nil {
		t.Fatalf("seed project docs: %v", err)
	}

	cfg := config{
		targetSQLite:     ":memory:",
		targetProjectID:  map[int]struct{}{1: {}},
		repoCleanDocs:    true,
		repoCleanDocsDir: filepath.Join("project_book", "clean_docs"),
	}

	report, plans, err := buildRepoCleanDocsPlan(ctx, db, cfg)
	if err != nil {
		t.Fatalf("buildRepoCleanDocsPlan failed: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("expected one plan, got %d", len(plans))
	}
	if len(report.Projects) != 1 || report.Projects[0].IncomingCount != 2 {
		t.Fatalf("unexpected report: %+v", report.Projects)
	}

	if err := applyReplace(ctx, db, plans); err != nil {
		t.Fatalf("applyReplace failed: %v", err)
	}
	if err := enrichPostVerification(ctx, db, report, plans); err != nil {
		t.Fatalf("enrichPostVerification failed: %v", err)
	}

	docs, err := loadTargetDocs(ctx, db, 1)
	if err != nil {
		t.Fatalf("loadTargetDocs failed: %v", err)
	}
	if len(docs) != 3 {
		t.Fatalf("expected three docs after repair, got %+v", docs)
	}

	got := map[string]targetDoc{}
	for _, doc := range docs {
		got[doc.Title] = doc
	}

	if got["Healthy Architecture"].Content != "keep me" {
		t.Fatalf("expected architecture doc preserved, got %+v", got["Healthy Architecture"])
	}
	if got["project_book/clean_docs/20_architecture/project_handoff_storage.md"].Content != "# Handoff\n\nDistinct handoff note.\n" {
		t.Fatalf("expected handoff clean doc imported, got %+v", got["project_book/clean_docs/20_architecture/project_handoff_storage.md"])
	}
	if got["project_book/clean_docs/30_ops/native_telegram_operator_notifications.md"].Content != "# Telegram\n\nDistinct operator note.\n" {
		t.Fatalf("expected operator clean doc imported, got %+v", got["project_book/clean_docs/30_ops/native_telegram_operator_notifications.md"])
	}

	rows, err := db.Query(`
		SELECT d.title
		FROM netrunner_attached_doc nad
		INNER JOIN project_doc d ON d.id = nad.project_doc_id
		WHERE nad.session_id = 1
		ORDER BY d.title
	`)
	if err != nil {
		t.Fatalf("query repaired attachments: %v", err)
	}
	defer rows.Close()

	attachedTitles := make([]string, 0)
	for rows.Next() {
		var title string
		if err := rows.Scan(&title); err != nil {
			t.Fatalf("scan repaired attachment: %v", err)
		}
		attachedTitles = append(attachedTitles, title)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate repaired attachments: %v", err)
	}

	expectedTitles := []string{
		"Healthy Architecture",
		"project_book/clean_docs/20_architecture/project_handoff_storage.md",
		"project_book/clean_docs/30_ops/native_telegram_operator_notifications.md",
	}
	if len(attachedTitles) != len(expectedTitles) {
		t.Fatalf("unexpected repaired attachments: %+v", attachedTitles)
	}
	for i := range expectedTitles {
		if attachedTitles[i] != expectedTitles[i] {
			t.Fatalf("unexpected repaired attachments: got %+v want %+v", attachedTitles, expectedTitles)
		}
	}
}
