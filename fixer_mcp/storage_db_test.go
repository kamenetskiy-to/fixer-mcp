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
	var leaseTableName string
	if err := db.QueryRow("SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'parallel_wave_scope_lease'").Scan(&leaseTableName); err != nil {
		t.Fatalf("expected parallel_wave_scope_lease table after repeated initDB: %v", err)
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
		"project_write_fence", "project_write_lease", "workroom_audit_event",
	} {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, tableName).Scan(&count); err != nil {
			t.Fatalf("inspect %s: %v", tableName, err)
		}
		if count != 1 {
			t.Fatalf("expected one %s table, got %d", tableName, count)
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
