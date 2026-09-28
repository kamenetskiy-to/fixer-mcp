package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

// TestInitDBMigratesLegacyProjectIdentitySchema proves that a database created
// with the previous (path-keyed) schema is migrated in place: the project gains
// an identity_key column, the path-alias table appears, existing rows and their
// path resolution survive, and opening the same database twice is a no-op.
func TestInitDBMigratesLegacyProjectIdentitySchema(t *testing.T) {
	originalDB := db
	originalRole := authorizedRole
	originalProjectID := authorizedProjectId
	defer func() {
		db = originalDB
		authorizedRole = originalRole
		authorizedProjectId = originalProjectID
	}()

	dbPath := filepath.Join(t.TempDir(), "legacy-project-identity.db")
	legacyDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open legacy database: %v", err)
	}
	if _, err := legacyDB.Exec(`
		CREATE TABLE project (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			cwd TEXT UNIQUE NOT NULL,
			active INTEGER NOT NULL DEFAULT 0
		);
		CREATE TABLE session (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER,
			task_description TEXT NOT NULL,
			status TEXT NOT NULL
		);
		INSERT INTO project (id, name, cwd, active) VALUES (1, 'Legacy Alpha', '/tmp/legacy-alpha', 1);
		INSERT INTO project (id, name, cwd, active) VALUES (2, 'Legacy Beta', '/tmp/legacy-beta', 0);
	`); err != nil {
		_ = legacyDB.Close()
		t.Fatalf("seed legacy database: %v", err)
	}
	if err := legacyDB.Close(); err != nil {
		t.Fatalf("close legacy database: %v", err)
	}

	t.Setenv(fixerDBPathEnv, dbPath)

	initDB()
	if err := db.Close(); err != nil {
		t.Fatalf("close first migrated database: %v", err)
	}
	initDB()
	defer func() {
		if db != nil {
			_ = db.Close()
		}
	}()

	var identityColumnCount int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('project') WHERE name = 'identity_key'`,
	).Scan(&identityColumnCount); err != nil {
		t.Fatalf("inspect project.identity_key column: %v", err)
	}
	if identityColumnCount != 1 {
		t.Fatalf("expected project.identity_key after migration, got %d", identityColumnCount)
	}

	for _, tableName := range []string{"project_path_alias"} {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, tableName).Scan(&count); err != nil {
			t.Fatalf("inspect %s table: %v", tableName, err)
		}
		if count != 1 {
			t.Fatalf("expected %s table after migration, got %d", tableName, count)
		}
	}
	for _, indexName := range []string{"project_path_alias_project_idx", "project_identity_key_unique_idx"} {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?`, indexName).Scan(&count); err != nil {
			t.Fatalf("inspect %s index: %v", indexName, err)
		}
		if count != 1 {
			t.Fatalf("expected %s after migration, got %d", indexName, count)
		}
	}

	var projectCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM project`).Scan(&projectCount); err != nil {
		t.Fatalf("count migrated projects: %v", err)
	}
	if projectCount != 2 {
		t.Fatalf("expected two preserved projects, got %d", projectCount)
	}

	var legacyIdentity sql.NullString
	if err := db.QueryRow(`SELECT identity_key FROM project WHERE id = 1`).Scan(&legacyIdentity); err != nil {
		t.Fatalf("read migrated identity: %v", err)
	}
	if legacyIdentity.Valid && legacyIdentity.String != "" {
		t.Fatalf("legacy project gained an identity key: %q", legacyIdentity.String)
	}

	projectID, _, knownCWD, lookupErr := findProjectByCWD("/tmp/legacy-alpha")
	if lookupErr != nil {
		t.Fatalf("legacy path must still resolve after migration: %v", lookupErr)
	}
	if projectID != 1 || knownCWD != "/tmp/legacy-alpha" {
		t.Fatalf("unexpected legacy resolution: id=%d cwd=%q", projectID, knownCWD)
	}

	var aliasCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM project_path_alias`).Scan(&aliasCount); err != nil {
		t.Fatalf("count alias rows after second open: %v", err)
	}
	if aliasCount != 0 {
		t.Fatalf("second open must be a no-op, got %d alias rows", aliasCount)
	}

	// The migrated database must serve the new identity feature end to end.
	authorizedRole = "overseer"
	authorizedProjectId = 0

	primaryPath := filepath.Join(t.TempDir(), "identity-host")
	aliasPath := filepath.Join(t.TempDir(), "identity-host-2")
	_, created, createErr := RegisterProject(context.Background(), nil, RegisterProjectInput{
		Cwd:         primaryPath,
		IdentityKey: testIdentityKeyAlpha,
	})
	if createErr != nil {
		t.Fatalf("register on migrated database failed: %v", createErr)
	}
	_, aliased, aliasErr := RegisterProject(context.Background(), nil, RegisterProjectInput{
		Cwd:         aliasPath,
		IdentityKey: testIdentityKeyAlpha,
	})
	if aliasErr != nil {
		t.Fatalf("alias registration on migrated database failed: %v", aliasErr)
	}
	if aliased.ProjectId != created.ProjectId {
		t.Fatalf("migrated database did not link the alias: created=%d aliased=%d", created.ProjectId, aliased.ProjectId)
	}
}
