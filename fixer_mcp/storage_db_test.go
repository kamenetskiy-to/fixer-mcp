package main

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestInitDBProjectActivityOverviewSchemaIdempotent(t *testing.T) {
	originalDB := db
	defer func() {
		db = originalDB
	}()

	dbPath := filepath.Join(t.TempDir(), "fixer.db")
	t.Setenv(fixerDBPathEnv, dbPath)

	initDB()
	if db != nil {
		_ = db.Close()
	}
	initDB()
	defer func() {
		if db != nil {
			_ = db.Close()
		}
	}()

	var activeColumnCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('project') WHERE name = 'active'").Scan(&activeColumnCount); err != nil {
		t.Fatalf("inspect project schema: %v", err)
	}
	if activeColumnCount != 1 {
		t.Fatalf("expected project.active column after repeated initDB, got %d", activeColumnCount)
	}

	var overviewTableName string
	if err := db.QueryRow("SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'project_overview'").Scan(&overviewTableName); err != nil {
		t.Fatalf("expected project_overview table after repeated initDB: %v", err)
	}
	if overviewTableName != "project_overview" {
		t.Fatalf("unexpected overview table name: %q", overviewTableName)
	}

	var waveTableName string
	if err := db.QueryRow("SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'parallel_wave'").Scan(&waveTableName); err != nil {
		t.Fatalf("expected parallel_wave table after repeated initDB: %v", err)
	}
	if waveTableName != "parallel_wave" {
		t.Fatalf("unexpected parallel wave table name: %q", waveTableName)
	}

	var waveWorkerTableName string
	if err := db.QueryRow("SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'parallel_wave_worker'").Scan(&waveWorkerTableName); err != nil {
		t.Fatalf("expected parallel_wave_worker table after repeated initDB: %v", err)
	}
	if waveWorkerTableName != "parallel_wave_worker" {
		t.Fatalf("unexpected parallel wave worker table name: %q", waveWorkerTableName)
	}

	for _, columnName := range []string{"parallel_wave_id", "parallel_wave_worker_id", "launch_origin"} {
		var columnCount int
		if err := db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('worker_process') WHERE name = ?", columnName).Scan(&columnCount); err != nil {
			t.Fatalf("inspect worker_process.%s schema: %v", columnName, err)
		}
		if columnCount != 1 {
			t.Fatalf("expected worker_process.%s column after repeated initDB, got %d", columnName, columnCount)
		}
	}

	for _, columnName := range []string{"parent_doc_id", "level", "slug", "path", "status"} {
		var columnCount int
		if err := db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('project_doc') WHERE name = ?", columnName).Scan(&columnCount); err != nil {
			t.Fatalf("inspect project_doc.%s schema: %v", columnName, err)
		}
		if columnCount != 1 {
			t.Fatalf("expected project_doc.%s column after repeated initDB, got %d", columnName, columnCount)
		}
	}

	for _, columnName := range []string{"auth_env_keys", "portability", "install_hint", "archived"} {
		var columnCount int
		if err := db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('mcp_server') WHERE name = ?", columnName).Scan(&columnCount); err != nil {
			t.Fatalf("inspect mcp_server.%s schema: %v", columnName, err)
		}
		if columnCount != 1 {
			t.Fatalf("expected mcp_server.%s column after repeated initDB, got %d", columnName, columnCount)
		}
	}

	var logTableName string
	if err := db.QueryRow("SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'netrunner_session_log'").Scan(&logTableName); err != nil {
		t.Fatalf("expected netrunner_session_log table after repeated initDB: %v", err)
	}
	if logTableName != "netrunner_session_log" {
		t.Fatalf("unexpected netrunner_session_log table name: %q", logTableName)
	}
}

func TestInitDBMcpServerMarketplaceMigrationIdempotent(t *testing.T) {
	originalDB := db
	defer func() {
		db = originalDB
	}()

	dbPath := filepath.Join(t.TempDir(), "fixer.db")
	seedDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open seed db: %v", err)
	}
	_, err = seedDB.Exec(`
		CREATE TABLE project (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			cwd TEXT UNIQUE NOT NULL
		);
		CREATE TABLE session (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER,
			task_description TEXT NOT NULL,
			status TEXT NOT NULL
		);
		CREATE TABLE mcp_server (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE
		);
		INSERT INTO mcp_server (name) VALUES ('react-native-guide'), ('tavily');
	`)
	if err != nil {
		_ = seedDB.Close()
		t.Fatalf("seed old mcp_server schema: %v", err)
	}
	if err := seedDB.Close(); err != nil {
		t.Fatalf("close seed db: %v", err)
	}

	t.Setenv(fixerDBPathEnv, dbPath)
	initDB()
	if db != nil {
		_ = db.Close()
	}
	initDB()
	defer func() {
		if db != nil {
			_ = db.Close()
		}
	}()

	for _, columnName := range []string{"auth_env_keys", "portability", "install_hint", "archived"} {
		var columnCount int
		if err := db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('mcp_server') WHERE name = ?", columnName).Scan(&columnCount); err != nil {
			t.Fatalf("inspect mcp_server.%s schema: %v", columnName, err)
		}
		if columnCount != 1 {
			t.Fatalf("expected mcp_server.%s column after repeated initDB, got %d", columnName, columnCount)
		}
	}

	var archived int
	if err := db.QueryRow("SELECT archived FROM mcp_server WHERE name = 'react-native-guide'").Scan(&archived); err != nil {
		t.Fatalf("query archived migrated server: %v", err)
	}
	if archived != 1 {
		t.Fatalf("expected react-native-guide to be archived by marketplace seed, got %d", archived)
	}

	var portability, authEnvKeys, installHint string
	if err := db.QueryRow("SELECT portability, auth_env_keys, install_hint FROM mcp_server WHERE name = 'tavily'").Scan(&portability, &authEnvKeys, &installHint); err != nil {
		t.Fatalf("query tavily marketplace fields: %v", err)
	}
	if portability != "portable" || authEnvKeys != "TAVILY_API_KEY" || installHint == "" {
		t.Fatalf("expected tavily marketplace fields, got portability=%q auth=%q install=%q", portability, authEnvKeys, installHint)
	}
}

func TestInitDBProjectDocTreeMigrationWaitsForBackcompatColumns(t *testing.T) {
	originalDB := db
	defer func() {
		db = originalDB
	}()

	dbPath := filepath.Join(t.TempDir(), "fixer.db")
	seedDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open seed db: %v", err)
	}
	_, err = seedDB.Exec(`
		CREATE TABLE project (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			cwd TEXT UNIQUE NOT NULL
		);
		CREATE TABLE project_doc (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER,
			title TEXT NOT NULL,
			content TEXT NOT NULL
		);
	`)
	if err != nil {
		_ = seedDB.Close()
		t.Fatalf("seed old project_doc schema: %v", err)
	}
	if err := seedDB.Close(); err != nil {
		t.Fatalf("close seed db: %v", err)
	}

	t.Setenv(fixerDBPathEnv, dbPath)
	initDB()
	defer func() {
		if db != nil {
			_ = db.Close()
		}
	}()

	for _, columnName := range []string{"doc_type", "parent_doc_id", "level", "slug", "path", "status"} {
		var columnCount int
		if err := db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('project_doc') WHERE name = ?", columnName).Scan(&columnCount); err != nil {
			t.Fatalf("inspect project_doc.%s schema: %v", columnName, err)
		}
		if columnCount != 1 {
			t.Fatalf("expected project_doc.%s column after legacy migration, got %d", columnName, columnCount)
		}
	}

	for _, indexName := range []string{"project_doc_project_parent_idx", "project_doc_project_slug_unique_idx", "project_doc_project_path_unique_idx"} {
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?", indexName).Scan(&count); err != nil {
			t.Fatalf("inspect %s: %v", indexName, err)
		}
		if count != 1 {
			t.Fatalf("expected index %s after project_doc tree migration, got %d", indexName, count)
		}
	}
}

func TestInitDBParallelWaveIndexWaitsForBackcompatWorkerColumns(t *testing.T) {
	originalDB := db
	defer func() {
		db = originalDB
	}()

	dbPath := filepath.Join(t.TempDir(), "fixer.db")
	seedDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open seed db: %v", err)
	}
	_, err = seedDB.Exec(`
		CREATE TABLE project (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			cwd TEXT UNIQUE NOT NULL
		);
		CREATE TABLE session (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER,
			task_description TEXT NOT NULL,
			status TEXT NOT NULL
		);
		CREATE TABLE worker_process (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL,
			session_id INTEGER NOT NULL,
			pid INTEGER NOT NULL,
			launch_epoch INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT 'running',
			stop_reason TEXT,
			started_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			stopped_at TEXT
		);
	`)
	if err != nil {
		_ = seedDB.Close()
		t.Fatalf("seed old worker_process schema: %v", err)
	}
	if err := seedDB.Close(); err != nil {
		t.Fatalf("close seed db: %v", err)
	}

	t.Setenv(fixerDBPathEnv, dbPath)
	initDB()
	defer func() {
		if db != nil {
			_ = db.Close()
		}
	}()

	for _, columnName := range []string{"parallel_wave_id", "parallel_wave_worker_id", "launch_origin"} {
		var columnCount int
		if err := db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('worker_process') WHERE name = ?", columnName).Scan(&columnCount); err != nil {
			t.Fatalf("inspect worker_process.%s schema: %v", columnName, err)
		}
		if columnCount != 1 {
			t.Fatalf("expected worker_process.%s column after initDB, got %d", columnName, columnCount)
		}
	}

	var indexCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'worker_process_parallel_wave_idx'").Scan(&indexCount); err != nil {
		t.Fatalf("inspect worker_process_parallel_wave_idx: %v", err)
	}
	if indexCount != 1 {
		t.Fatalf("expected worker_process_parallel_wave_idx after initDB, got %d", indexCount)
	}
}
