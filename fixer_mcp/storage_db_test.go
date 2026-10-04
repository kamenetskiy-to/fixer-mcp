package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
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
	for _, columnName := range []string{
		"phase",
		"gate_state",
		"control_state",
		"control_reason",
		"parent_wave_id",
		"root_wave_id",
		"depth",
		"max_child_wave_depth",
		"max_total_descendant_waves",
		"max_total_sessions",
		"failure_policy_state",
		"repair_worker_id",
		"repair_attempt_count",
		"handoff_sha",
		"acceptance_session_id",
		"review_policy",
		"review_backend",
		"review_model",
		"review_reasoning",
	} {
		var columnCount int
		if err := db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('parallel_wave') WHERE name = ?", columnName).Scan(&columnCount); err != nil {
			t.Fatalf("inspect parallel_wave.%s schema: %v", columnName, err)
		}
		if columnCount != 1 {
			t.Fatalf("expected parallel_wave.%s after repeated initDB, got %d", columnName, columnCount)
		}
	}
	var binaryStateTable string
	if err := db.QueryRow("SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'mcp_binary_state'").Scan(&binaryStateTable); err != nil {
		t.Fatalf("expected mcp_binary_state table after repeated initDB: %v", err)
	}
	for _, columnName := range []string{"running_build_id", "required_build_id", "running_process_identity", "required_by_process_identity", "confirmed_at"} {
		var columnCount int
		if err := db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('mcp_binary_state') WHERE name = ?", columnName).Scan(&columnCount); err != nil {
			t.Fatalf("inspect mcp_binary_state.%s schema: %v", columnName, err)
		}
		if columnCount != 1 {
			t.Fatalf("expected mcp_binary_state.%s after repeated initDB, got %d", columnName, columnCount)
		}
	}

	var waveWorkerTableName string
	if err := db.QueryRow("SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'parallel_wave_worker'").Scan(&waveWorkerTableName); err != nil {
		t.Fatalf("expected parallel_wave_worker table after repeated initDB: %v", err)
	}
	if waveWorkerTableName != "parallel_wave_worker" {
		t.Fatalf("unexpected parallel wave worker table name: %q", waveWorkerTableName)
	}
	for _, columnName := range []string{"terminal_outcome", "retry_attempt_count", "retry_cause", "retry_next_eligible_at"} {
		var columnCount int
		if err := db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('parallel_wave_worker') WHERE name = ?", columnName).Scan(&columnCount); err != nil {
			t.Fatalf("inspect parallel_wave_worker.%s schema: %v", columnName, err)
		}
		if columnCount != 1 {
			t.Fatalf("expected parallel_wave_worker.%s after repeated initDB, got %d", columnName, columnCount)
		}
	}
	var leaseTableName sql.NullString
	if err := db.QueryRow("SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'parallel_wave_scope_lease'").Scan(&leaseTableName); err != nil && err != sql.ErrNoRows {
		t.Fatalf("inspect retired parallel_wave_scope_lease table: %v", err)
	}
	if leaseTableName.Valid {
		t.Fatalf("parallel_wave_scope_lease must stay retired after repeated initDB, found %q", leaseTableName.String)
	}

	var dependencyTableName string
	if err := db.QueryRow("SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'wave_worker_dependency'").Scan(&dependencyTableName); err != nil {
		t.Fatalf("expected wave_worker_dependency table after repeated initDB: %v", err)
	}
	if dependencyTableName != "wave_worker_dependency" {
		t.Fatalf("unexpected wave worker dependency table name: %q", dependencyTableName)
	}
	var dependencyIndexCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'wave_worker_dependency_unique_idx'").Scan(&dependencyIndexCount); err != nil {
		t.Fatalf("inspect wave_worker_dependency_unique_idx: %v", err)
	}
	if dependencyIndexCount != 1 {
		t.Fatalf("expected wave_worker_dependency_unique_idx after repeated initDB, got %d", dependencyIndexCount)
	}
	var plannedWaveTableName string
	if err := db.QueryRow("SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'planned_wave'").Scan(&plannedWaveTableName); err != nil {
		t.Fatalf("expected planned_wave table after repeated initDB: %v", err)
	}
	for _, columnName := range []string{
		"project_id",
		"status",
		"idempotency_key",
		"definition_hash",
		"base_ref",
		"worktree_root",
		"initialized_wave_id",
		"failure_reason",
	} {
		var columnCount int
		if err := db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('planned_wave') WHERE name = ?", columnName).Scan(&columnCount); err != nil {
			t.Fatalf("inspect planned_wave.%s schema: %v", columnName, err)
		}
		if columnCount != 1 {
			t.Fatalf("expected planned_wave.%s after repeated initDB, got %d", columnName, columnCount)
		}
	}
	var plannedWaveTaskTableName string
	if err := db.QueryRow("SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'planned_wave_task'").Scan(&plannedWaveTaskTableName); err != nil {
		t.Fatalf("expected planned_wave_task table after repeated initDB: %v", err)
	}
	for _, columnName := range []string{"cli_backend", "cli_model", "cli_reasoning", "mcp_server_names"} {
		var columnCount int
		if err := db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('planned_wave_task') WHERE name = ?", columnName).Scan(&columnCount); err != nil {
			t.Fatalf("inspect planned_wave_task.%s schema: %v", columnName, err)
		}
		if columnCount != 1 {
			t.Fatalf("expected planned_wave_task.%s after repeated initDB, got %d", columnName, columnCount)
		}
	}
	for _, indexName := range []string{
		"planned_wave_project_idempotency_unique_idx",
		"planned_wave_task_key_unique_idx",
		"planned_wave_task_position_unique_idx",
	} {
		var indexCount int
		if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?", indexName).Scan(&indexCount); err != nil {
			t.Fatalf("inspect %s: %v", indexName, err)
		}
		if indexCount != 1 {
			t.Fatalf("expected %s after repeated initDB, got %d", indexName, indexCount)
		}
	}

	var backlogTableName string
	if err := db.QueryRow("SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'backlog_item'").Scan(&backlogTableName); err != nil {
		t.Fatalf("expected backlog_item table after repeated initDB: %v", err)
	}
	if backlogTableName != "backlog_item" {
		t.Fatalf("unexpected backlog table name: %q", backlogTableName)
	}
	for _, columnName := range []string{"project_id", "title", "description", "status", "priority", "created_at", "updated_at"} {
		var columnCount int
		if err := db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('backlog_item') WHERE name = ?", columnName).Scan(&columnCount); err != nil {
			t.Fatalf("inspect backlog_item.%s: %v", columnName, err)
		}
		if columnCount != 1 {
			t.Fatalf("expected backlog_item.%s after repeated initDB, got %d", columnName, columnCount)
		}
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

	for _, tableName := range []string{"project_balance", "fixer_spend_authority", "balance_ledger"} {
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?", tableName).Scan(&count); err != nil {
			t.Fatalf("inspect %s table: %v", tableName, err)
		}
		if count != 1 {
			t.Fatalf("expected %s table after repeated initDB, got %d", tableName, count)
		}
	}

	for _, indexName := range []string{"project_balance_project_unique_idx", "fixer_spend_authority_project_unique_idx", "balance_ledger_project_id_idx", "balance_ledger_kind_idx"} {
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?", indexName).Scan(&count); err != nil {
			t.Fatalf("inspect %s index: %v", indexName, err)
		}
		if count != 1 {
			t.Fatalf("expected %s after repeated initDB, got %d", indexName, count)
		}
	}
}

func TestInitDBProvisionsProjectWorkroomSchemaIdempotently(t *testing.T) {
	originalDB := db
	defer func() { db = originalDB }()
	t.Setenv(fixerDBPathEnv, filepath.Join(t.TempDir(), "workroom-schema.db"))

	initDB()
	if _, err := db.Exec(`INSERT INTO project (name, cwd, active) VALUES ('Workroom', '/tmp/workroom-schema-project', 1)`); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	if err := initProjectWorkroomSchema(); err != nil {
		t.Fatalf("repeat workroom schema migration: %v", err)
	}
	defer func() { _ = db.Close() }()

	for _, tableName := range []string{
		"project_ui_cursor", "project_ui_event", "command_dedup", "fixer_thread", "fixer_turn",
		"genui_surface_instance", "genui_surface_revision", "genui_demand_example", "genui_surface_feedback", "genui_action_invocation",
		"project_hands", "hands_instruction", "hands_instruction_event", "hands_generation",
		"workroom_audit_event",
	} {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, tableName).Scan(&count); err != nil {
			t.Fatalf("inspect %s: %v", tableName, err)
		}
		if count != 1 {
			t.Fatalf("expected one %s table, got %d", tableName, count)
		}
	}
	for _, retiredTable := range []string{"project_write_fence", "project_write_lease", "parallel_wave_scope_lease"} {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, retiredTable).Scan(&count); err != nil {
			t.Fatalf("inspect retired %s: %v", retiredTable, err)
		}
		if count != 0 {
			t.Fatalf("retired lease table %s must not exist in the new schema, got %d", retiredTable, count)
		}
	}
	for _, columnName := range []string{"session_kind", "created_at", "updated_at"} {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('session') WHERE name = ?`, columnName).Scan(&count); err != nil {
			t.Fatalf("inspect session.%s: %v", columnName, err)
		}
		if count != 1 {
			t.Fatalf("expected session.%s once, got %d", columnName, count)
		}
	}
	var identityCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM project_hands`).Scan(&identityCount); err != nil {
		t.Fatalf("count Hands identities: %v", err)
	}
	if identityCount != 2 {
		t.Fatalf("expected one identity per project: identities=%d", identityCount)
	}
	var laneTableCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'hands_provider_lane'`).Scan(&laneTableCount); err != nil {
		t.Fatalf("inspect removed Hands lane table: %v", err)
	}
	if laneTableCount != 0 {
		t.Fatal("legacy Hands lane table still exists")
	}
	result, err := db.Exec(`INSERT INTO project (name, cwd, active) VALUES ('Disposable', '/tmp/workroom-schema-disposable', 0)`)
	if err != nil {
		t.Fatalf("seed disposable project: %v", err)
	}
	disposableID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("resolve disposable project: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM project WHERE id = ?`, disposableID); err != nil {
		t.Fatalf("permanent Hands projection must preserve project deletion: %v", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM project_hands WHERE project_id = ?`, disposableID).Scan(&identityCount); err != nil {
		t.Fatalf("inspect cascaded Hands identity: %v", err)
	}
	if identityCount != 0 {
		t.Fatalf("deleted project retained %d Hands identities", identityCount)
	}
}

func TestInitDBPreservesUnknownLegacySessionTimestampsAndStampsNewRows(t *testing.T) {
	originalDB := db
	defer func() { db = originalDB }()
	dbPath := filepath.Join(t.TempDir(), "legacy-session-timestamps.db")
	t.Setenv(fixerDBPathEnv, dbPath)

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
			status TEXT NOT NULL,
			report TEXT,
			cli_backend TEXT NOT NULL DEFAULT 'codex',
			cli_model TEXT NOT NULL DEFAULT '',
			cli_reasoning TEXT NOT NULL DEFAULT '',
			declared_write_scope TEXT NOT NULL DEFAULT '["."]',
			parallel_wave_id TEXT NOT NULL DEFAULT '',
			epic_doc_id INTEGER,
			repair_source_session_id INTEGER,
			rework_count INTEGER NOT NULL DEFAULT 0,
			forced_stop_count INTEGER NOT NULL DEFAULT 0
		);
		INSERT INTO project (id, name, cwd, active) VALUES (1, 'Legacy', '/tmp/legacy-session-timestamps', 1);
		INSERT INTO session (id, project_id, task_description, status) VALUES (1, 1, 'Historical session', 'completed');
	`); err != nil {
		_ = legacyDB.Close()
		t.Fatalf("seed legacy database: %v", err)
	}
	if err := legacyDB.Close(); err != nil {
		t.Fatalf("close legacy database: %v", err)
	}

	initDB()
	if err := db.Close(); err != nil {
		t.Fatalf("close first migrated database: %v", err)
	}
	initDB()
	defer func() { _ = db.Close() }()

	var legacyCreatedAt, legacyUpdatedAt sql.NullString
	if err := db.QueryRow(`SELECT created_at, updated_at FROM session WHERE id = 1`).Scan(&legacyCreatedAt, &legacyUpdatedAt); err != nil {
		t.Fatalf("read legacy timestamps: %v", err)
	}
	if legacyCreatedAt.Valid || legacyUpdatedAt.Valid {
		t.Fatalf("migration invented legacy timestamps: created=%+v updated=%+v", legacyCreatedAt, legacyUpdatedAt)
	}

	result, err := db.Exec(`INSERT INTO session (project_id, task_description, status) VALUES (1, 'New session', 'pending')`)
	if err != nil {
		t.Fatalf("insert new session: %v", err)
	}
	newSessionID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("resolve new session id: %v", err)
	}
	var newCreatedAt, newUpdatedAt sql.NullString
	if err := db.QueryRow(`SELECT created_at, updated_at FROM session WHERE id = ?`, newSessionID).Scan(&newCreatedAt, &newUpdatedAt); err != nil {
		t.Fatalf("read new timestamps: %v", err)
	}
	if !newCreatedAt.Valid || newCreatedAt.String == "" || !newUpdatedAt.Valid || newUpdatedAt.String == "" {
		t.Fatalf("new session was not timestamped: created=%+v updated=%+v", newCreatedAt, newUpdatedAt)
	}
}

func TestInitDBParallelWaveV2LegacyRowCompatibility(t *testing.T) {
	originalDB := db
	defer func() { db = originalDB }()

	dbPath := filepath.Join(t.TempDir(), "legacy-fixer.db")
	seedDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open legacy seed db: %v", err)
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
		CREATE TABLE parallel_wave (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL,
			status TEXT NOT NULL DEFAULT 'created',
			base_sha TEXT NOT NULL,
			base_branch TEXT NOT NULL DEFAULT '',
			project_cwd TEXT NOT NULL,
			worktree_root TEXT NOT NULL,
			orchestration_epoch INTEGER NOT NULL DEFAULT 0,
			created_by_session_id INTEGER,
			failure_reason TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			launched_at TEXT,
			completed_at TEXT
		);
		INSERT INTO project (id, name, cwd) VALUES (1, 'legacy', '/tmp/legacy-fixer-project');
		INSERT INTO session (id, project_id, task_description, status) VALUES (1, 1, 'legacy task', 'completed');
		INSERT INTO parallel_wave (
			id, project_id, status, base_sha, base_branch, project_cwd, worktree_root, orchestration_epoch
		) VALUES (7, 1, 'running', 'legacy-sha', 'main', '/tmp/legacy-fixer-project', '.codex/netrunner_worktrees', 4);
	`)
	if err != nil {
		_ = seedDB.Close()
		t.Fatalf("seed legacy parallel_wave schema: %v", err)
	}
	if err := seedDB.Close(); err != nil {
		t.Fatalf("close legacy seed db: %v", err)
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

	var status, phase, gateState, controlState, failurePolicyState, handoffSHA string
	var rootWaveID, depth, maxDepth, maxDescendants, maxSessions int
	if err := db.QueryRow(
		`SELECT status, phase, gate_state, control_state, failure_policy_state, handoff_sha, root_wave_id, depth,
		        max_child_wave_depth, max_total_descendant_waves, max_total_sessions
		 FROM parallel_wave WHERE id = 7`,
	).Scan(&status, &phase, &gateState, &controlState, &failurePolicyState, &handoffSHA, &rootWaveID, &depth, &maxDepth, &maxDescendants, &maxSessions); err != nil {
		t.Fatalf("query migrated legacy wave: %v", err)
	}
	if status != "running" || phase != parallelWavePhaseImplementation {
		t.Fatalf("legacy status compatibility was not preserved: status=%q phase=%q", status, phase)
	}
	if gateState != parallelWaveGateNone || controlState != parallelWaveControlActive || rootWaveID != 7 || depth != 0 {
		t.Fatalf("unexpected legacy wave v2 defaults: gate=%q control=%q root=%d depth=%d", gateState, controlState, rootWaveID, depth)
	}
	if failurePolicyState != parallelWaveFailurePolicyNone || handoffSHA != "" {
		t.Fatalf("legacy row must not gain failure approval or handoff authority: policy=%q handoff=%q", failurePolicyState, handoffSHA)
	}
	if maxDepth != 0 || maxDescendants != 0 || maxSessions != 0 {
		t.Fatalf("legacy row must not gain recursive authority: depth=%d descendants=%d sessions=%d", maxDepth, maxDescendants, maxSessions)
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

// Pre-grok provider-lane schema: the three Hands tables exactly as a database
// created before 'grok' became a registered lane carries them, so initDB must
// widen the lane CHECK constraints through the grok lane rebuild. The fixture
// intentionally carries no retired scope columns so the grok rebuild path is
// exercised in isolation from the scope retirement.
const preGrokHandsGenerationDDL = `CREATE TABLE hands_generation (
	instruction_id TEXT NOT NULL,
	generation INTEGER NOT NULL CHECK(generation > 0),
	project_id INTEGER NOT NULL,
	compat_session_id INTEGER,
	provider TEXT NOT NULL CHECK(provider IN ('codex', 'commandcode', 'claude', 'kimi-code', 'antigravity')),
	model TEXT NOT NULL,
	reasoning TEXT NOT NULL,
	status TEXT NOT NULL CHECK(status IN ('planned', 'starting', 'running', 'stopped', 'failed', 'lost')),
	external_session_id TEXT,
	process_id INTEGER,
	process_start_identity TEXT,
	binary_build_id TEXT,
	binary_epoch INTEGER,
	launch_mode TEXT NOT NULL DEFAULT 'headless' CHECK(launch_mode = 'headless'),
	result_envelope_json TEXT CHECK(result_envelope_json IS NULL OR json_valid(result_envelope_json)),
	started_at TEXT,
	heartbeat_at TEXT,
	ended_at TEXT,
	exit_code INTEGER,
	stop_reason TEXT,
	created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	PRIMARY KEY(instruction_id, generation),
	FOREIGN KEY(instruction_id) REFERENCES hands_instruction(id) ON DELETE CASCADE ON UPDATE NO ACTION,
	FOREIGN KEY(project_id) REFERENCES project(id) ON DELETE CASCADE ON UPDATE NO ACTION,
	FOREIGN KEY(compat_session_id) REFERENCES session(id) ON DELETE SET NULL ON UPDATE NO ACTION
)`

const preGrokHandsInstructionDDL = `CREATE TABLE hands_instruction (
	id TEXT PRIMARY KEY,
	project_id INTEGER NOT NULL,
	actor_id TEXT NOT NULL,
	ordinal INTEGER NOT NULL CHECK(ordinal > 0),
	source_channel_kind TEXT NOT NULL,
	source_channel_id TEXT NOT NULL,
	source_message_id TEXT NOT NULL DEFAULT '',
	issuer_principal_id TEXT NOT NULL,
	instruction_text TEXT NOT NULL CHECK(length(instruction_text) <= 65536),
	instruction_envelope_json TEXT NOT NULL CHECK(length(instruction_envelope_json) <= 131072 AND json_valid(instruction_envelope_json)),
	requested_lane TEXT NOT NULL CHECK(requested_lane IN ('codex', 'commandcode', 'claude', 'kimi-code', 'antigravity')),
	risk_class TEXT NOT NULL CHECK(risk_class IN ('read_only', 'repository_write', 'unsupported_high_risk')),
	review_policy TEXT NOT NULL CHECK(review_policy IN ('auto_read_only', 'fixer_required')),
	state TEXT NOT NULL CHECK(state IN ('queued', 'waiting_for_lease', 'starting', 'running', 'awaiting_review', 'completed', 'cancelled', 'failed', 'abandoned', 'unsupported')),
	state_reason_code TEXT,
	state_reason_text TEXT,
	compat_session_id INTEGER,
	idempotency_key TEXT NOT NULL,
	revision INTEGER NOT NULL DEFAULT 1 CHECK(revision > 0),
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	terminal_at TEXT,
	UNIQUE(project_id, ordinal),
	UNIQUE(project_id, source_channel_kind, source_channel_id, idempotency_key),
	FOREIGN KEY(project_id) REFERENCES project_hands(project_id) ON DELETE CASCADE ON UPDATE NO ACTION,
	FOREIGN KEY(compat_session_id) REFERENCES session(id) ON DELETE SET NULL ON UPDATE NO ACTION
)`

const preGrokProjectHandsDDL = `CREATE TABLE project_hands (
	project_id INTEGER PRIMARY KEY,
	actor_id TEXT NOT NULL UNIQUE,
	display_name TEXT NOT NULL DEFAULT 'Руки' CHECK(display_name = 'Руки'),
	authority_state TEXT NOT NULL DEFAULT 'enabled' CHECK(authority_state IN ('enabled', 'disabled', 'revoked')),
	default_lane TEXT NOT NULL DEFAULT 'commandcode' CHECK(default_lane IN ('codex', 'commandcode', 'claude', 'kimi-code', 'antigravity')),
	next_instruction_ordinal INTEGER NOT NULL DEFAULT 1 CHECK(next_instruction_ordinal > 0),
	created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	FOREIGN KEY(project_id) REFERENCES project(id) ON DELETE CASCADE ON UPDATE NO ACTION
)`

func seedPreGrokHandsSchema(t *testing.T) {
	t.Helper()
	for _, statement := range []string{
		`DROP TABLE hands_generation`,
		`DROP TABLE hands_instruction`,
		`DROP TABLE project_hands`,
		preGrokProjectHandsDDL,
		preGrokHandsInstructionDDL,
		preGrokHandsGenerationDDL,
		`CREATE INDEX hands_generation_one_active_project_idx ON hands_generation(project_id) WHERE status IN ('starting', 'running')`,
		`INSERT OR IGNORE INTO project_hands (
			project_id, actor_id, display_name, authority_state, default_lane,
			next_instruction_ordinal, created_at, updated_at
		) SELECT id, 'pre-grok-actor-' || id, 'Руки', 'enabled', 'codex', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP FROM project`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("seed pre-grok Hands schema: %v: %v", statement, err)
		}
	}
}

func TestInitDBGrokLaneRebuildWidensLegacyHandsSchema(t *testing.T) {
	originalDB := db
	defer func() { db = originalDB }()
	t.Setenv(fixerDBPathEnv, filepath.Join(t.TempDir(), "grok-lane-rebuild.db"))
	initDB()
	defer func() { _ = db.Close() }()
	seedPreGrokHandsSchema(t)
	for _, statement := range []string{
		`INSERT INTO project (name, cwd, active) VALUES ('Grok lane', '/tmp/grok-lane', 1)`,
		`INSERT INTO hands_instruction (
			id, project_id, actor_id, ordinal, source_channel_kind, source_channel_id, issuer_principal_id,
			instruction_text, instruction_envelope_json, requested_lane, risk_class, review_policy, state,
			idempotency_key, created_at, updated_at
		) VALUES ('instr-grok', 1, 'actor-1', 1, 'telegram', 'channel-1', 'principal-1',
			'Pre-grok historical instruction', '` + legacyEnvelopeSentinel + `', 'codex', 'repository_write', 'fixer_required', 'queued', 'grok-legacy-1',
			CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		`INSERT INTO hands_generation (instruction_id, generation, project_id, provider, model, reasoning, status)
			VALUES ('instr-grok', 1, 1, 'codex', 'legacy-model', 'high', 'stopped')`,
		`CREATE INDEX hands_instruction_issuer_idx ON hands_instruction(issuer_principal_id)`,
		`CREATE TRIGGER hands_instruction_no_delete BEFORE DELETE ON hands_instruction BEGIN SELECT RAISE(ABORT, 'no-delete'); END`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("seed pre-grok fixture: %v: %v", statement, err)
		}
	}
	initDB()
	for _, name := range []string{
		"hands_instruction_issuer_idx",
		"hands_instruction_no_delete",
		"hands_instruction_project_state_idx",
		"hands_generation_one_active_project_idx",
	} {
		if count := countRows(t, `SELECT COUNT(*) FROM sqlite_master WHERE name = ?`, name); count != 1 {
			t.Fatalf("grok rebuild must preserve %s, found %d", name, count)
		}
	}
	var text, envelope string
	if err := db.QueryRow(`SELECT instruction_text, instruction_envelope_json FROM hands_instruction WHERE id = 'instr-grok'`).Scan(&text, &envelope); err != nil {
		t.Fatalf("read pre-grok instruction: %v", err)
	}
	if text != "Pre-grok historical instruction" || envelope != legacyEnvelopeSentinel {
		t.Fatalf("grok rebuild rewrote immutable history: text=%q envelope=%q", text, envelope)
	}
	var provider, model string
	if err := db.QueryRow(`SELECT provider, model FROM hands_generation WHERE instruction_id = 'instr-grok'`).Scan(&provider, &model); err != nil {
		t.Fatalf("read pre-grok generation: %v", err)
	}
	if provider != "codex" || model != "legacy-model" {
		t.Fatalf("grok rebuild rewrote generation rows: provider=%q model=%q", provider, model)
	}
	for _, statement := range []string{
		`INSERT INTO hands_instruction (
			id, project_id, actor_id, ordinal, source_channel_kind, source_channel_id, issuer_principal_id,
			instruction_text, instruction_envelope_json, requested_lane, risk_class, review_policy, state,
			idempotency_key, created_at, updated_at
		) VALUES ('instr-grok-new', 1, 'actor-1', 2, 'telegram', 'channel-1', 'principal-1',
			'Post-migration grok instruction', '{}', 'grok', 'repository_write', 'fixer_required', 'queued', 'grok-legacy-2',
			CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		`INSERT INTO hands_generation (instruction_id, generation, project_id, provider, model, reasoning, status)
			VALUES ('instr-grok-new', 1, 1, 'grok', 'grok-model', 'high', 'planned')`,
		`UPDATE project_hands SET default_lane = 'grok'`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("widened lane CHECK must accept grok (%s): %v", statement, err)
		}
	}
	// The project_hands provisioning trigger must survive the rebuild: it
	// writes into project_hands and is dropped/recreated around the rebuild.
	if _, err := db.Exec(`INSERT INTO project (name, cwd, active) VALUES ('Post migration project', '/tmp/post-migration-project', 1)`); err != nil {
		t.Fatalf("insert post-migration project: %v", err)
	}
	if provisioned := countRows(t,
		`SELECT COUNT(*) FROM project_hands WHERE project_id = (SELECT id FROM project WHERE cwd = '/tmp/post-migration-project')`,
	); provisioned != 1 {
		t.Fatalf("project_hands provisioning trigger must survive the rebuild, provisioned %d", provisioned)
	}
	initDB()
	if count := countRows(t, `SELECT COUNT(*) FROM hands_instruction`); count != 2 {
		t.Fatalf("repeat migration changed instruction rows: %d", count)
	}
	if count := countRows(t, `SELECT COUNT(*) FROM hands_generation`); count != 2 {
		t.Fatalf("repeat migration changed generation rows: %d", count)
	}
	if _, err := db.Exec(`DELETE FROM hands_instruction`); err == nil || !strings.Contains(err.Error(), "no-delete") {
		t.Fatalf("preserved trigger must still guard deletes after repeat migration, got %v", err)
	}
}

func TestInitDBGrokLaneRebuildToleratesHistoricalFKViolations(t *testing.T) {
	originalDB := db
	defer func() { db = originalDB }()
	t.Setenv(fixerDBPathEnv, filepath.Join(t.TempDir(), "grok-historical-fk.db"))
	initDB()
	defer func() { _ = db.Close() }()
	seedPreGrokHandsSchema(t)
	for _, statement := range []string{
		`INSERT INTO project (name, cwd, active) VALUES ('Grok historical FK', '/tmp/grok-historical-fk', 1)`,
		`PRAGMA foreign_keys = OFF`,
		`INSERT INTO hands_instruction (
			id, project_id, actor_id, ordinal, source_channel_kind, source_channel_id, issuer_principal_id,
			instruction_text, instruction_envelope_json, requested_lane, risk_class, review_policy, state,
			compat_session_id, idempotency_key, created_at, updated_at
		) VALUES ('instr-grok-orphan', 1, 'actor-1', 1, 'telegram', 'channel-1', 'principal-1',
			'Historical orphan instruction', '` + legacyEnvelopeSentinel + `', 'codex', 'repository_write', 'fixer_required', 'running',
			9999, 'grok-orphan-1', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		`PRAGMA foreign_keys = ON`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("seed historical FK fixture: %v: %v", statement, err)
		}
	}
	// The migration must succeed despite the historical violation and must
	// preserve the violating row verbatim: not fixed, not grown.
	initDB()
	if violations := countRows(t, `SELECT COUNT(*) FROM pragma_foreign_key_check`); violations != 1 {
		t.Fatalf("historical violation must survive untouched, got %d", violations)
	}
	var text, state string
	if err := db.QueryRow(`SELECT instruction_text, state FROM hands_instruction WHERE id = 'instr-grok-orphan'`).Scan(&text, &state); err != nil {
		t.Fatalf("read historical instruction: %v", err)
	}
	if text != "Historical orphan instruction" || state != "running" {
		t.Fatalf("grok rebuild rewrote historical row: text=%q state=%q", text, state)
	}
}

func TestRebuildGrokLaneTableRejectsNewFKViolationsAndRollsBack(t *testing.T) {
	originalDB := db
	defer func() { db = originalDB }()
	t.Setenv(fixerDBPathEnv, filepath.Join(t.TempDir(), "grok-fk-guard.db"))
	initDB()
	defer func() { _ = db.Close() }()
	for _, statement := range []string{
		`INSERT INTO project (name, cwd, active) VALUES ('Grok FK guard', '/tmp/grok-fk-guard', 1)`,
		`INSERT INTO hands_instruction (
			id, project_id, actor_id, ordinal, source_channel_kind, source_channel_id, issuer_principal_id,
			instruction_text, instruction_envelope_json, requested_lane, risk_class, review_policy, state,
			idempotency_key, created_at, updated_at
		) VALUES ('instr-guard', 1, 'actor-1', 1, 'telegram', 'channel-1', 'principal-1',
			'FK guard subject', '{}', 'codex', 'repository_write', 'fixer_required', 'queued', 'guard-1',
			CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		`INSERT INTO hands_generation (instruction_id, generation, project_id, provider, model, reasoning, status)
			VALUES ('instr-guard', 1, 1, 'codex', 'model', 'high', 'stopped')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("seed FK guard fixture: %v: %v", statement, err)
		}
	}
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("acquire connection: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		_ = conn.Close()
		t.Fatalf("disable FK enforcement: %v", err)
	}
	// A rebuild that introduces a foreign key violation the database did not
	// already carry must fail and roll back, leaving the legacy rows in place.
	err = rebuildGrokLaneTable(ctx, conn, "hands_generation", []string{
		`UPDATE hands_generation SET compat_session_id = 999999`,
	})
	if _, restoreErr := conn.ExecContext(ctx, `PRAGMA foreign_keys = ON`); restoreErr != nil {
		_ = conn.Close()
		t.Fatalf("restore FK enforcement: %v", restoreErr)
	}
	closeErr := conn.Close()
	if closeErr != nil {
		t.Fatalf("release connection: %v", closeErr)
	}
	if err == nil || !strings.Contains(err.Error(), "foreign key violation") {
		t.Fatalf("expected a rejected foreign key violation, got %v", err)
	}
	var compatSessionID sql.NullInt64
	if err := db.QueryRow(`SELECT compat_session_id FROM hands_generation WHERE instruction_id = 'instr-guard'`).Scan(&compatSessionID); err != nil {
		t.Fatalf("read rolled back generation: %v", err)
	}
	if compatSessionID.Valid {
		t.Fatalf("failed rebuild must roll back, got compat_session_id=%d", compatSessionID.Int64)
	}
	if violations := countRows(t, `SELECT COUNT(*) FROM pragma_foreign_key_check`); violations != 0 {
		t.Fatalf("rejected rebuild left violations behind: %d", violations)
	}
	// A rebuild that keeps the baseline intact must commit normally.
	conn, err = db.Conn(ctx)
	if err != nil {
		t.Fatalf("reacquire connection: %v", err)
	}
	if err := rebuildGrokLaneTable(ctx, conn, "hands_generation", []string{
		`UPDATE hands_generation SET reasoning = 'high'`,
	}); err != nil {
		_ = conn.Close()
		t.Fatalf("benign rebuild must commit: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("release connection: %v", err)
	}
}
