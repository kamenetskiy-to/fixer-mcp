package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

func dbTableHasColumn(tableName string, columnName string) bool {
	var count int
	if err := db.QueryRow(
		"SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?",
		tableName,
		columnName,
	).Scan(&count); err != nil {
		return false
	}
	return count > 0
}

func initDB() {
	var err error
	dsn := resolveFixerDBPath()
	separator := "?"
	if strings.Contains(dsn, "?") {
		separator = "&"
	}
	db, err = sql.Open("sqlite", dsn+separator+"_txlock=immediate")
	if err != nil {
		log.Fatalf("Error opening db: %v", err)
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	_, err = db.Exec(`
		PRAGMA journal_mode = WAL;
		PRAGMA synchronous = NORMAL;
		PRAGMA busy_timeout = 5000;
		PRAGMA foreign_keys = ON;
	`)
	if err != nil {
		log.Fatalf("Error initializing sqlite pragmas: %v", err)
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS project (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			cwd TEXT UNIQUE NOT NULL,
			active INTEGER NOT NULL DEFAULT 0
		);
		CREATE TABLE IF NOT EXISTS backlog_item (
			id INTEGER PRIMARY KEY,
			project_id INTEGER,
			title TEXT,
			description TEXT,
			status TEXT DEFAULT 'open',
			priority TEXT,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(project_id) REFERENCES project(id)
		);
			CREATE TABLE IF NOT EXISTS session (
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
				forced_stop_count INTEGER NOT NULL DEFAULT 0,
				session_kind TEXT NOT NULL DEFAULT 'netrunner',
				created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
				FOREIGN KEY(project_id) REFERENCES project(id),
				FOREIGN KEY(epic_doc_id) REFERENCES project_doc(id) ON DELETE SET NULL ON UPDATE NO ACTION,
				FOREIGN KEY(repair_source_session_id) REFERENCES session(id) ON DELETE SET NULL ON UPDATE NO ACTION
			);
		CREATE TABLE IF NOT EXISTS mcp_role_rules (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			role_name TEXT NOT NULL,
			rule_desc TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS project_doc (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER,
			title TEXT NOT NULL,
			content TEXT NOT NULL,
			doc_type TEXT DEFAULT 'documentation',
			parent_doc_id INTEGER,
			level INTEGER NOT NULL DEFAULT 0,
			slug TEXT NOT NULL DEFAULT '',
			path TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'current',
			FOREIGN KEY(project_id) REFERENCES project(id),
			FOREIGN KEY(parent_doc_id) REFERENCES project_doc(id) ON DELETE SET NULL ON UPDATE NO ACTION
		);
		CREATE TABLE IF NOT EXISTS doc_proposal (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER,
			session_id INTEGER,
			status TEXT NOT NULL,
			proposed_content TEXT NOT NULL,
			proposed_doc_type TEXT DEFAULT 'documentation',
			target_project_doc_id INTEGER,
			FOREIGN KEY(project_id) REFERENCES project(id),
			FOREIGN KEY(session_id) REFERENCES session(id),
			FOREIGN KEY(target_project_doc_id) REFERENCES project_doc(id)
		);
		CREATE TABLE IF NOT EXISTS mcp_server (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			short_description TEXT,
			long_description TEXT,
			auto_attach INTEGER NOT NULL DEFAULT 0,
			is_default INTEGER NOT NULL DEFAULT 0,
			category TEXT,
			how_to TEXT,
			auth_env_keys TEXT NOT NULL DEFAULT '',
			portability TEXT NOT NULL DEFAULT '',
			install_hint TEXT NOT NULL DEFAULT '',
			archived INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE UNIQUE INDEX IF NOT EXISTS mcp_server_name_unique_idx ON mcp_server(name);
		CREATE TABLE IF NOT EXISTS session_mcp_server (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id INTEGER NOT NULL,
			mcp_server_id INTEGER NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(session_id) REFERENCES session(id) ON DELETE CASCADE ON UPDATE NO ACTION,
			FOREIGN KEY(mcp_server_id) REFERENCES mcp_server(id) ON DELETE CASCADE ON UPDATE NO ACTION
		);
		CREATE UNIQUE INDEX IF NOT EXISTS session_mcp_server_unique_idx ON session_mcp_server(session_id, mcp_server_id);
		CREATE INDEX IF NOT EXISTS session_mcp_server_session_idx ON session_mcp_server(session_id);
		CREATE INDEX IF NOT EXISTS session_mcp_server_mcp_idx ON session_mcp_server(mcp_server_id);
		CREATE TABLE IF NOT EXISTS project_mcp_server (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL,
			mcp_server_id INTEGER NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(project_id) REFERENCES project(id) ON DELETE CASCADE ON UPDATE NO ACTION,
			FOREIGN KEY(mcp_server_id) REFERENCES mcp_server(id) ON DELETE CASCADE ON UPDATE NO ACTION
		);
		CREATE UNIQUE INDEX IF NOT EXISTS project_mcp_server_unique_idx ON project_mcp_server(project_id, mcp_server_id);
		CREATE INDEX IF NOT EXISTS project_mcp_server_project_idx ON project_mcp_server(project_id);
		CREATE INDEX IF NOT EXISTS project_mcp_server_mcp_idx ON project_mcp_server(mcp_server_id);
		CREATE TABLE IF NOT EXISTS netrunner_attached_doc (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id INTEGER NOT NULL,
			project_doc_id INTEGER NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(session_id) REFERENCES session(id) ON DELETE CASCADE ON UPDATE NO ACTION,
			FOREIGN KEY(project_doc_id) REFERENCES project_doc(id) ON DELETE CASCADE ON UPDATE NO ACTION
		);
		CREATE UNIQUE INDEX IF NOT EXISTS netrunner_attached_doc_unique_idx ON netrunner_attached_doc(session_id, project_doc_id);
		CREATE INDEX IF NOT EXISTS netrunner_attached_doc_session_idx ON netrunner_attached_doc(session_id);
		CREATE INDEX IF NOT EXISTS netrunner_attached_doc_doc_idx ON netrunner_attached_doc(project_doc_id);
		CREATE TABLE IF NOT EXISTS netrunner_session_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL,
			session_id INTEGER NOT NULL,
			log_type TEXT NOT NULL CHECK(log_type IN ('started', 'progress', 'blocked', 'workaround', 'completed')),
			log_text TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(project_id) REFERENCES project(id) ON DELETE CASCADE ON UPDATE NO ACTION,
			FOREIGN KEY(session_id) REFERENCES session(id) ON DELETE CASCADE ON UPDATE NO ACTION
		);
		CREATE INDEX IF NOT EXISTS netrunner_session_log_project_session_idx ON netrunner_session_log(project_id, session_id, id);
		CREATE INDEX IF NOT EXISTS netrunner_session_log_created_idx ON netrunner_session_log(project_id, created_at, id);
			CREATE TABLE IF NOT EXISTS autonomous_run_status (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				project_id INTEGER NOT NULL,
				session_id INTEGER,
				state TEXT NOT NULL,
				summary TEXT NOT NULL,
				focus TEXT,
				blocker TEXT,
				evidence TEXT,
				orchestration_epoch INTEGER NOT NULL DEFAULT 0,
				orchestration_frozen INTEGER NOT NULL DEFAULT 0,
				notifications_enabled_for_active_run INTEGER NOT NULL DEFAULT 1,
				created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
				FOREIGN KEY(project_id) REFERENCES project(id) ON DELETE CASCADE ON UPDATE NO ACTION,
				FOREIGN KEY(session_id) REFERENCES session(id) ON DELETE SET NULL ON UPDATE NO ACTION
			);
			CREATE UNIQUE INDEX IF NOT EXISTS autonomous_run_status_project_unique_idx ON autonomous_run_status(project_id);
			CREATE INDEX IF NOT EXISTS autonomous_run_status_project_idx ON autonomous_run_status(project_id);
			CREATE TABLE IF NOT EXISTS parallel_wave (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				project_id INTEGER NOT NULL,
				status TEXT NOT NULL DEFAULT 'created',
				phase TEXT NOT NULL DEFAULT 'initialized',
				gate_state TEXT NOT NULL DEFAULT 'none',
				control_state TEXT NOT NULL DEFAULT 'active',
				control_reason TEXT NOT NULL DEFAULT '',
				base_sha TEXT NOT NULL,
				base_branch TEXT NOT NULL DEFAULT '',
				project_cwd TEXT NOT NULL,
				worktree_root TEXT NOT NULL,
				orchestration_epoch INTEGER NOT NULL DEFAULT 0,
				created_by_session_id INTEGER,
				epic_doc_id INTEGER,
				parent_wave_id INTEGER,
				root_wave_id INTEGER,
				depth INTEGER NOT NULL DEFAULT 0,
				max_child_wave_depth INTEGER NOT NULL DEFAULT 0,
				max_total_descendant_waves INTEGER NOT NULL DEFAULT 0,
				max_total_sessions INTEGER NOT NULL DEFAULT 0,
				failure_policy_state TEXT NOT NULL DEFAULT 'none',
				repair_worker_id INTEGER,
				repair_attempt_count INTEGER NOT NULL DEFAULT 0,
				handoff_sha TEXT NOT NULL DEFAULT '',
				acceptance_session_id INTEGER,
				review_policy TEXT NOT NULL DEFAULT 'manual',
				review_backend TEXT NOT NULL DEFAULT 'codex',
					review_model TEXT NOT NULL DEFAULT 'opencode-go/deepseek-v4-flash',
				review_reasoning TEXT NOT NULL DEFAULT 'high',
				failure_reason TEXT NOT NULL DEFAULT '',
				created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
				launched_at TEXT,
				completed_at TEXT,
				FOREIGN KEY(project_id) REFERENCES project(id) ON DELETE CASCADE ON UPDATE NO ACTION,
				FOREIGN KEY(epic_doc_id) REFERENCES project_doc(id) ON DELETE SET NULL ON UPDATE NO ACTION,
				FOREIGN KEY(created_by_session_id) REFERENCES session(id) ON DELETE SET NULL ON UPDATE NO ACTION,
				FOREIGN KEY(parent_wave_id) REFERENCES parallel_wave(id) ON DELETE RESTRICT ON UPDATE NO ACTION,
				FOREIGN KEY(root_wave_id) REFERENCES parallel_wave(id) ON DELETE RESTRICT ON UPDATE NO ACTION,
				FOREIGN KEY(repair_worker_id) REFERENCES parallel_wave_worker(id) ON DELETE SET NULL ON UPDATE NO ACTION,
				FOREIGN KEY(acceptance_session_id) REFERENCES session(id) ON DELETE SET NULL ON UPDATE NO ACTION
			);
			CREATE INDEX IF NOT EXISTS parallel_wave_project_status_idx ON parallel_wave(project_id, status);
			CREATE TABLE IF NOT EXISTS parallel_wave_worker (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				wave_id INTEGER NOT NULL,
				project_id INTEGER NOT NULL,
				session_id INTEGER NOT NULL,
				status TEXT NOT NULL DEFAULT 'created',
				declared_write_scope TEXT NOT NULL,
				branch_name TEXT NOT NULL,
				worktree_path TEXT NOT NULL,
				base_sha TEXT NOT NULL,
				head_sha TEXT NOT NULL DEFAULT '',
				changed_paths TEXT NOT NULL DEFAULT '[]',
				diff_patch_path TEXT NOT NULL DEFAULT '',
				diff_stat TEXT NOT NULL DEFAULT '',
				launch_epoch INTEGER NOT NULL DEFAULT 0,
				worker_process_id INTEGER,
				external_session_id TEXT NOT NULL DEFAULT '',
				headless_log_path TEXT NOT NULL DEFAULT '',
				launcher_log_path TEXT NOT NULL DEFAULT '',
				worker_metadata_path TEXT NOT NULL DEFAULT '',
				failure_reason TEXT NOT NULL DEFAULT '',
				terminal_outcome TEXT NOT NULL DEFAULT '',
				retry_attempt_count INTEGER NOT NULL DEFAULT 0,
				retry_cause TEXT NOT NULL DEFAULT '',
				retry_next_eligible_at TEXT NOT NULL DEFAULT '',
				cleanup_status TEXT NOT NULL DEFAULT 'pending',
				created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
				launched_at TEXT,
				terminal_at TEXT,
				cleaned_at TEXT,
				FOREIGN KEY(wave_id) REFERENCES parallel_wave(id) ON DELETE CASCADE ON UPDATE NO ACTION,
				FOREIGN KEY(project_id) REFERENCES project(id) ON DELETE CASCADE ON UPDATE NO ACTION,
				FOREIGN KEY(session_id) REFERENCES session(id) ON DELETE CASCADE ON UPDATE NO ACTION,
				FOREIGN KEY(worker_process_id) REFERENCES worker_process(id) ON DELETE SET NULL ON UPDATE NO ACTION
			);
			CREATE UNIQUE INDEX IF NOT EXISTS parallel_wave_worker_wave_session_unique_idx ON parallel_wave_worker(wave_id, session_id);
			CREATE INDEX IF NOT EXISTS parallel_wave_worker_status_idx ON parallel_wave_worker(project_id, status);
			CREATE TABLE IF NOT EXISTS parallel_wave_scope_lease (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				project_id INTEGER NOT NULL,
				wave_id INTEGER NOT NULL,
				scope_path TEXT NOT NULL,
				active INTEGER NOT NULL DEFAULT 1,
				created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
				released_at TEXT,
				FOREIGN KEY(project_id) REFERENCES project(id) ON DELETE CASCADE ON UPDATE NO ACTION,
				FOREIGN KEY(wave_id) REFERENCES parallel_wave(id) ON DELETE CASCADE ON UPDATE NO ACTION
			);
			CREATE UNIQUE INDEX IF NOT EXISTS parallel_wave_scope_lease_wave_scope_unique_idx ON parallel_wave_scope_lease(wave_id, scope_path);
			CREATE INDEX IF NOT EXISTS parallel_wave_scope_lease_active_idx ON parallel_wave_scope_lease(project_id, active, wave_id);
			CREATE TABLE IF NOT EXISTS wave_worker_dependency (
				wave_id INTEGER NOT NULL,
				parent_session_id INTEGER NOT NULL,
				child_session_id INTEGER NOT NULL,
				FOREIGN KEY(wave_id) REFERENCES parallel_wave(id) ON DELETE CASCADE ON UPDATE NO ACTION,
				FOREIGN KEY(parent_session_id) REFERENCES session(id) ON DELETE CASCADE ON UPDATE NO ACTION,
				FOREIGN KEY(child_session_id) REFERENCES session(id) ON DELETE CASCADE ON UPDATE NO ACTION
			);
			CREATE UNIQUE INDEX IF NOT EXISTS wave_worker_dependency_unique_idx ON wave_worker_dependency(wave_id, parent_session_id, child_session_id);
			CREATE TABLE IF NOT EXISTS planned_wave (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				project_id INTEGER NOT NULL,
				title TEXT NOT NULL,
				status TEXT NOT NULL DEFAULT 'planned' CHECK(status IN ('planned', 'initializing', 'initialized', 'failed')),
				idempotency_key TEXT NOT NULL DEFAULT '',
				definition_hash TEXT NOT NULL,
				reason TEXT NOT NULL DEFAULT '',
				base_ref TEXT NOT NULL DEFAULT '',
				worktree_root TEXT NOT NULL DEFAULT '',
				epic_doc_id INTEGER,
				parent_wave_id INTEGER,
				max_child_wave_depth INTEGER NOT NULL DEFAULT 0,
				max_total_descendant_waves INTEGER NOT NULL DEFAULT 0,
				max_total_sessions INTEGER NOT NULL DEFAULT 0,
				initialized_wave_id INTEGER,
				failure_reason TEXT NOT NULL DEFAULT '',
				created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
				initialized_at TEXT,
				FOREIGN KEY(project_id) REFERENCES project(id) ON DELETE CASCADE ON UPDATE NO ACTION,
				FOREIGN KEY(epic_doc_id) REFERENCES project_doc(id) ON DELETE SET NULL ON UPDATE NO ACTION,
				FOREIGN KEY(parent_wave_id) REFERENCES parallel_wave(id) ON DELETE RESTRICT ON UPDATE NO ACTION,
				FOREIGN KEY(initialized_wave_id) REFERENCES parallel_wave(id) ON DELETE RESTRICT ON UPDATE NO ACTION
			);
			CREATE INDEX IF NOT EXISTS planned_wave_project_status_idx ON planned_wave(project_id, status, id);
			CREATE UNIQUE INDEX IF NOT EXISTS planned_wave_project_idempotency_unique_idx
				ON planned_wave(project_id, idempotency_key) WHERE idempotency_key != '';
			CREATE TABLE IF NOT EXISTS planned_wave_task (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				planned_wave_id INTEGER NOT NULL,
				project_id INTEGER NOT NULL,
				task_key TEXT NOT NULL,
				position INTEGER NOT NULL,
				task_description TEXT NOT NULL,
				declared_write_scope TEXT NOT NULL,
				dependencies TEXT NOT NULL DEFAULT '[]',
				cli_backend TEXT NOT NULL DEFAULT 'codex',
				cli_model TEXT NOT NULL DEFAULT '',
				cli_reasoning TEXT NOT NULL DEFAULT '',
				mcp_server_names TEXT NOT NULL DEFAULT '[]',
				materialized_session_id INTEGER,
				created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
				FOREIGN KEY(planned_wave_id) REFERENCES planned_wave(id) ON DELETE CASCADE ON UPDATE NO ACTION,
				FOREIGN KEY(project_id) REFERENCES project(id) ON DELETE CASCADE ON UPDATE NO ACTION,
				FOREIGN KEY(materialized_session_id) REFERENCES session(id) ON DELETE RESTRICT ON UPDATE NO ACTION
			);
			CREATE UNIQUE INDEX IF NOT EXISTS planned_wave_task_key_unique_idx ON planned_wave_task(planned_wave_id, task_key);
			CREATE UNIQUE INDEX IF NOT EXISTS planned_wave_task_position_unique_idx ON planned_wave_task(planned_wave_id, position);
			CREATE INDEX IF NOT EXISTS planned_wave_task_project_idx ON planned_wave_task(project_id, planned_wave_id, position);
			CREATE TABLE IF NOT EXISTS worker_process (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				project_id INTEGER NOT NULL,
				session_id INTEGER NOT NULL,
				pid INTEGER NOT NULL,
				launch_epoch INTEGER NOT NULL DEFAULT 0,
				status TEXT NOT NULL DEFAULT 'running',
				stop_reason TEXT,
				launch_origin TEXT NOT NULL DEFAULT '',
				parallel_wave_id INTEGER,
				parallel_wave_worker_id INTEGER,
				started_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
				stopped_at TEXT,
				FOREIGN KEY(project_id) REFERENCES project(id) ON DELETE CASCADE ON UPDATE NO ACTION,
				FOREIGN KEY(session_id) REFERENCES session(id) ON DELETE CASCADE ON UPDATE NO ACTION,
				FOREIGN KEY(parallel_wave_id) REFERENCES parallel_wave(id) ON DELETE SET NULL ON UPDATE NO ACTION,
				FOREIGN KEY(parallel_wave_worker_id) REFERENCES parallel_wave_worker(id) ON DELETE SET NULL ON UPDATE NO ACTION
			);
			CREATE INDEX IF NOT EXISTS worker_process_project_status_idx ON worker_process(project_id, status);
			CREATE INDEX IF NOT EXISTS worker_process_session_status_idx ON worker_process(session_id, status);
			CREATE INDEX IF NOT EXISTS worker_process_pid_idx ON worker_process(pid);
			CREATE TABLE IF NOT EXISTS mcp_binary_state (
				project_id INTEGER PRIMARY KEY,
				running_build_epoch INTEGER NOT NULL DEFAULT 0,
				required_build_epoch INTEGER NOT NULL DEFAULT 0,
				restart_required INTEGER NOT NULL DEFAULT 0,
				running_build_id TEXT NOT NULL DEFAULT '',
				required_build_id TEXT NOT NULL DEFAULT '',
				running_process_identity TEXT NOT NULL DEFAULT '',
				required_by_process_identity TEXT NOT NULL DEFAULT '',
				confirmed_at TEXT,
				reason TEXT NOT NULL DEFAULT '',
				updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
				FOREIGN KEY(project_id) REFERENCES project(id) ON DELETE CASCADE ON UPDATE NO ACTION
			);
			CREATE TABLE IF NOT EXISTS image_generation_job (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				project_id INTEGER NOT NULL,
				prompt TEXT NOT NULL,
				status TEXT NOT NULL DEFAULT 'launching',
				pid INTEGER NOT NULL DEFAULT 0,
				model TEXT NOT NULL DEFAULT '',
				output_path TEXT NOT NULL DEFAULT '',
				workspace_copy_path TEXT NOT NULL DEFAULT '',
				output_last_message_path TEXT NOT NULL DEFAULT '',
				json_output_path TEXT NOT NULL DEFAULT '',
				stderr_log_path TEXT NOT NULL DEFAULT '',
				thread_id TEXT NOT NULL DEFAULT '',
				failure_reason TEXT NOT NULL DEFAULT '',
				generated_images_baseline TEXT NOT NULL DEFAULT '[]',
				started_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
				completed_at TEXT,
				FOREIGN KEY(project_id) REFERENCES project(id) ON DELETE CASCADE ON UPDATE NO ACTION
			);
			CREATE INDEX IF NOT EXISTS image_generation_job_project_status_idx ON image_generation_job(project_id, status);
			CREATE INDEX IF NOT EXISTS image_generation_job_pid_idx ON image_generation_job(pid);
			CREATE TABLE IF NOT EXISTS project_handoff (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				project_id INTEGER NOT NULL,
				content TEXT NOT NULL,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(project_id) REFERENCES project(id) ON DELETE CASCADE ON UPDATE NO ACTION
		);
		CREATE UNIQUE INDEX IF NOT EXISTS project_handoff_project_unique_idx ON project_handoff(project_id);
		CREATE INDEX IF NOT EXISTS project_handoff_project_idx ON project_handoff(project_id);
		CREATE TABLE IF NOT EXISTS project_overview (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL,
			content TEXT NOT NULL,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(project_id) REFERENCES project(id) ON DELETE CASCADE ON UPDATE NO ACTION
		);
		CREATE UNIQUE INDEX IF NOT EXISTS project_overview_project_unique_idx ON project_overview(project_id);
		CREATE INDEX IF NOT EXISTS project_overview_project_idx ON project_overview(project_id);
		CREATE TABLE IF NOT EXISTS project_balance (
			project_id INTEGER NOT NULL UNIQUE,
			balance_units INTEGER NOT NULL DEFAULT 0,
			currency TEXT NOT NULL DEFAULT 'usd',
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(project_id) REFERENCES project(id) ON DELETE CASCADE ON UPDATE NO ACTION
		);
		CREATE UNIQUE INDEX IF NOT EXISTS project_balance_project_unique_idx ON project_balance(project_id);
		CREATE TABLE IF NOT EXISTS fixer_spend_authority (
			project_id INTEGER NOT NULL UNIQUE,
			enabled INTEGER NOT NULL DEFAULT 0,
			allowance_units INTEGER NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(project_id) REFERENCES project(id) ON DELETE CASCADE ON UPDATE NO ACTION
		);
		CREATE UNIQUE INDEX IF NOT EXISTS fixer_spend_authority_project_unique_idx ON fixer_spend_authority(project_id);
		CREATE TABLE IF NOT EXISTS balance_ledger (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL,
			delta_units INTEGER NOT NULL,
			kind TEXT NOT NULL CHECK(kind IN ('credit', 'spend', 'authority_grant')),
			reason TEXT NOT NULL,
			actor_role TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(project_id) REFERENCES project(id) ON DELETE CASCADE ON UPDATE NO ACTION
		);
		CREATE INDEX IF NOT EXISTS balance_ledger_project_id_idx ON balance_ledger(project_id, id);
		CREATE INDEX IF NOT EXISTS balance_ledger_kind_idx ON balance_ledger(project_id, kind, id);
		CREATE TABLE IF NOT EXISTS overseer_fixer_message (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL,
			sender_role TEXT NOT NULL CHECK(sender_role IN ('overseer', 'fixer')),
			content TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(project_id) REFERENCES project(id) ON DELETE CASCADE ON UPDATE NO ACTION
		);
		CREATE INDEX IF NOT EXISTS overseer_fixer_message_project_id_idx ON overseer_fixer_message(project_id, id);
		CREATE INDEX IF NOT EXISTS overseer_fixer_message_sender_idx ON overseer_fixer_message(sender_role, id);
		CREATE TABLE IF NOT EXISTS overseer_fixer_run_state (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL,
			active INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT '',
			reason TEXT NOT NULL DEFAULT '',
			last_message_id INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(project_id) REFERENCES project(id) ON DELETE CASCADE ON UPDATE NO ACTION
		);
		CREATE UNIQUE INDEX IF NOT EXISTS overseer_fixer_run_state_project_unique_idx ON overseer_fixer_run_state(project_id);
		CREATE INDEX IF NOT EXISTS overseer_fixer_run_state_active_idx ON overseer_fixer_run_state(active, project_id);
		CREATE TABLE IF NOT EXISTS session_external_link (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id INTEGER NOT NULL,
			backend TEXT NOT NULL,
			external_session_id TEXT NOT NULL,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(session_id) REFERENCES session(id) ON DELETE CASCADE ON UPDATE NO ACTION
		);
		CREATE UNIQUE INDEX IF NOT EXISTS session_external_link_session_backend_unique_idx ON session_external_link(session_id, backend);
		CREATE INDEX IF NOT EXISTS session_external_link_backend_idx ON session_external_link(backend);
		CREATE TABLE IF NOT EXISTS session_codex_link (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id INTEGER NOT NULL,
			codex_session_id TEXT NOT NULL,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(session_id) REFERENCES session(id) ON DELETE CASCADE ON UPDATE NO ACTION
		);
		CREATE UNIQUE INDEX IF NOT EXISTS session_codex_link_session_unique_idx ON session_codex_link(session_id);
		CREATE TABLE IF NOT EXISTS role_preprompt (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			role_name TEXT NOT NULL UNIQUE,
			prompt_text TEXT NOT NULL,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		log.Fatalf("Error creating tables: %v", err)
	}

	// Ensure report column exists if the DB was already created
	_, _ = db.Exec(`ALTER TABLE project ADD COLUMN active INTEGER NOT NULL DEFAULT 0;`)
	_, _ = db.Exec(`ALTER TABLE session ADD COLUMN report TEXT;`)
	_, _ = db.Exec(`ALTER TABLE session ADD COLUMN cli_backend TEXT NOT NULL DEFAULT 'codex';`)
	_, _ = db.Exec(`ALTER TABLE session ADD COLUMN cli_model TEXT NOT NULL DEFAULT '';`)
	_, _ = db.Exec(`ALTER TABLE session ADD COLUMN cli_reasoning TEXT NOT NULL DEFAULT '';`)
	_, _ = db.Exec(`ALTER TABLE session ADD COLUMN declared_write_scope TEXT NOT NULL DEFAULT '["."]';`)
	_, _ = db.Exec(`ALTER TABLE session ADD COLUMN parallel_wave_id TEXT NOT NULL DEFAULT '';`)
	_, _ = db.Exec(`ALTER TABLE session ADD COLUMN epic_doc_id INTEGER REFERENCES project_doc(id) ON DELETE SET NULL ON UPDATE NO ACTION;`)
	_, _ = db.Exec(`ALTER TABLE session ADD COLUMN repair_source_session_id INTEGER;`)
	_, _ = db.Exec(`ALTER TABLE session ADD COLUMN rework_count INTEGER NOT NULL DEFAULT 0;`)
	_, _ = db.Exec(`ALTER TABLE session ADD COLUMN forced_stop_count INTEGER NOT NULL DEFAULT 0;`)
	_, _ = db.Exec(`ALTER TABLE session ADD COLUMN session_kind TEXT NOT NULL DEFAULT 'netrunner';`)
	_, _ = db.Exec(`ALTER TABLE session ADD COLUMN created_at TEXT;`)
	_, _ = db.Exec(`ALTER TABLE session ADD COLUMN updated_at TEXT;`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS session_project_id_idx ON session(project_id, id);`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS session_project_status_idx ON session(project_id, status, id);`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS parallel_wave_project_id_idx ON parallel_wave(project_id, id);`)
	_, _ = db.Exec(`ALTER TABLE project_doc ADD COLUMN doc_type TEXT DEFAULT 'documentation';`)
	_, _ = db.Exec(`ALTER TABLE project_doc ADD COLUMN parent_doc_id INTEGER;`)
	_, _ = db.Exec(`ALTER TABLE project_doc ADD COLUMN level INTEGER NOT NULL DEFAULT 0;`)
	_, _ = db.Exec(`ALTER TABLE project_doc ADD COLUMN slug TEXT NOT NULL DEFAULT '';`)
	_, _ = db.Exec(`ALTER TABLE project_doc ADD COLUMN path TEXT NOT NULL DEFAULT '';`)
	_, _ = db.Exec(`ALTER TABLE project_doc ADD COLUMN status TEXT NOT NULL DEFAULT 'current';`)
	_, _ = db.Exec(`ALTER TABLE doc_proposal ADD COLUMN proposed_doc_type TEXT DEFAULT 'documentation';`)
	_, _ = db.Exec(`ALTER TABLE doc_proposal ADD COLUMN target_project_doc_id INTEGER;`)
	_, _ = db.Exec(`ALTER TABLE mcp_server ADD COLUMN short_description TEXT;`)
	_, _ = db.Exec(`ALTER TABLE mcp_server ADD COLUMN long_description TEXT;`)
	_, _ = db.Exec(`ALTER TABLE mcp_server ADD COLUMN auto_attach INTEGER NOT NULL DEFAULT 0;`)
	_, _ = db.Exec(`ALTER TABLE mcp_server ADD COLUMN is_default INTEGER NOT NULL DEFAULT 0;`)
	_, _ = db.Exec(`ALTER TABLE mcp_server ADD COLUMN category TEXT;`)
	_, _ = db.Exec(`ALTER TABLE mcp_server ADD COLUMN how_to TEXT;`)
	_, _ = db.Exec(`ALTER TABLE mcp_server ADD COLUMN auth_env_keys TEXT NOT NULL DEFAULT '';`)
	_, _ = db.Exec(`ALTER TABLE mcp_server ADD COLUMN portability TEXT NOT NULL DEFAULT '';`)
	_, _ = db.Exec(`ALTER TABLE mcp_server ADD COLUMN install_hint TEXT NOT NULL DEFAULT '';`)
	_, _ = db.Exec(`ALTER TABLE mcp_server ADD COLUMN archived INTEGER NOT NULL DEFAULT 0;`)
	_, _ = db.Exec(`ALTER TABLE mcp_server ADD COLUMN created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP;`)
	_, _ = db.Exec(`ALTER TABLE mcp_server ADD COLUMN updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP;`)
	_, _ = db.Exec(`ALTER TABLE mcp_server ADD COLUMN created_at TEXT NOT NULL DEFAULT '';`)
	_, _ = db.Exec(`ALTER TABLE mcp_server ADD COLUMN updated_at TEXT NOT NULL DEFAULT '';`)
	_, _ = db.Exec(`ALTER TABLE autonomous_run_status ADD COLUMN orchestration_epoch INTEGER NOT NULL DEFAULT 0;`)
	_, _ = db.Exec(`ALTER TABLE autonomous_run_status ADD COLUMN orchestration_frozen INTEGER NOT NULL DEFAULT 0;`)
	_, _ = db.Exec(`ALTER TABLE autonomous_run_status ADD COLUMN notifications_enabled_for_active_run INTEGER NOT NULL DEFAULT 1;`)
	_, _ = db.Exec(`ALTER TABLE parallel_wave ADD COLUMN epic_doc_id INTEGER REFERENCES project_doc(id) ON DELETE SET NULL ON UPDATE NO ACTION;`)
	_, _ = db.Exec(`ALTER TABLE parallel_wave ADD COLUMN phase TEXT NOT NULL DEFAULT 'initialized';`)
	_, _ = db.Exec(`ALTER TABLE parallel_wave ADD COLUMN gate_state TEXT NOT NULL DEFAULT 'none';`)
	_, _ = db.Exec(`ALTER TABLE parallel_wave ADD COLUMN control_state TEXT NOT NULL DEFAULT 'active';`)
	_, _ = db.Exec(`ALTER TABLE parallel_wave ADD COLUMN control_reason TEXT NOT NULL DEFAULT '';`)
	_, _ = db.Exec(`ALTER TABLE parallel_wave ADD COLUMN parent_wave_id INTEGER REFERENCES parallel_wave(id) ON DELETE RESTRICT ON UPDATE NO ACTION;`)
	_, _ = db.Exec(`ALTER TABLE parallel_wave ADD COLUMN root_wave_id INTEGER REFERENCES parallel_wave(id) ON DELETE RESTRICT ON UPDATE NO ACTION;`)
	_, _ = db.Exec(`ALTER TABLE parallel_wave ADD COLUMN depth INTEGER NOT NULL DEFAULT 0;`)
	_, _ = db.Exec(`ALTER TABLE parallel_wave ADD COLUMN max_child_wave_depth INTEGER NOT NULL DEFAULT 0;`)
	_, _ = db.Exec(`ALTER TABLE parallel_wave ADD COLUMN max_total_descendant_waves INTEGER NOT NULL DEFAULT 0;`)
	_, _ = db.Exec(`ALTER TABLE parallel_wave ADD COLUMN max_total_sessions INTEGER NOT NULL DEFAULT 0;`)
	_, _ = db.Exec(`ALTER TABLE parallel_wave ADD COLUMN failure_policy_state TEXT NOT NULL DEFAULT 'none';`)
	_, _ = db.Exec(`ALTER TABLE parallel_wave ADD COLUMN repair_worker_id INTEGER REFERENCES parallel_wave_worker(id) ON DELETE SET NULL ON UPDATE NO ACTION;`)
	_, _ = db.Exec(`ALTER TABLE parallel_wave ADD COLUMN repair_attempt_count INTEGER NOT NULL DEFAULT 0;`)
	_, _ = db.Exec(`ALTER TABLE parallel_wave ADD COLUMN handoff_sha TEXT NOT NULL DEFAULT '';`)
	_, _ = db.Exec(`ALTER TABLE parallel_wave ADD COLUMN acceptance_session_id INTEGER REFERENCES session(id) ON DELETE SET NULL ON UPDATE NO ACTION;`)
	_, _ = db.Exec(`ALTER TABLE parallel_wave ADD COLUMN review_policy TEXT NOT NULL DEFAULT 'manual';`)
	_, _ = db.Exec(`ALTER TABLE parallel_wave ADD COLUMN review_backend TEXT NOT NULL DEFAULT 'codex';`)
	_, _ = db.Exec(`ALTER TABLE parallel_wave ADD COLUMN review_model TEXT NOT NULL DEFAULT 'opencode-go/deepseek-v4-flash';`)
	_, _ = db.Exec(`ALTER TABLE parallel_wave ADD COLUMN review_reasoning TEXT NOT NULL DEFAULT 'high';`)
	_, _ = db.Exec(`ALTER TABLE parallel_wave_worker ADD COLUMN terminal_outcome TEXT NOT NULL DEFAULT '';`)
	_, _ = db.Exec(`ALTER TABLE parallel_wave_worker ADD COLUMN retry_attempt_count INTEGER NOT NULL DEFAULT 0;`)
	_, _ = db.Exec(`ALTER TABLE parallel_wave_worker ADD COLUMN retry_cause TEXT NOT NULL DEFAULT '';`)
	_, _ = db.Exec(`ALTER TABLE parallel_wave_worker ADD COLUMN retry_next_eligible_at TEXT NOT NULL DEFAULT '';`)
	_, _ = db.Exec(`ALTER TABLE mcp_binary_state ADD COLUMN running_build_id TEXT NOT NULL DEFAULT '';`)
	_, _ = db.Exec(`ALTER TABLE mcp_binary_state ADD COLUMN required_build_id TEXT NOT NULL DEFAULT '';`)
	_, _ = db.Exec(`ALTER TABLE mcp_binary_state ADD COLUMN running_process_identity TEXT NOT NULL DEFAULT '';`)
	_, _ = db.Exec(`ALTER TABLE mcp_binary_state ADD COLUMN required_by_process_identity TEXT NOT NULL DEFAULT '';`)
	_, _ = db.Exec(`ALTER TABLE mcp_binary_state ADD COLUMN confirmed_at TEXT;`)
	_, _ = db.Exec(`ALTER TABLE worker_process ADD COLUMN parallel_wave_id INTEGER;`)
	_, _ = db.Exec(`ALTER TABLE worker_process ADD COLUMN parallel_wave_worker_id INTEGER;`)
	_, _ = db.Exec(`ALTER TABLE worker_process ADD COLUMN launch_origin TEXT NOT NULL DEFAULT '';`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS project_doc_project_parent_idx ON project_doc(project_id, parent_doc_id);`)
	_, _ = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS project_doc_project_slug_unique_idx ON project_doc(project_id, slug) WHERE slug != '';`)
	_, _ = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS project_doc_project_path_unique_idx ON project_doc(project_id, path) WHERE path != '';`)
	_, _ = db.Exec(`
		CREATE TABLE IF NOT EXISTS netrunner_session_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL,
			session_id INTEGER NOT NULL,
			log_type TEXT NOT NULL CHECK(log_type IN ('started', 'progress', 'blocked', 'workaround', 'completed')),
			log_text TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(project_id) REFERENCES project(id) ON DELETE CASCADE ON UPDATE NO ACTION,
			FOREIGN KEY(session_id) REFERENCES session(id) ON DELETE CASCADE ON UPDATE NO ACTION
		);
	`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS netrunner_session_log_project_session_idx ON netrunner_session_log(project_id, session_id, id);`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS netrunner_session_log_created_idx ON netrunner_session_log(project_id, created_at, id);`)
	_, _ = db.Exec(`
		CREATE TABLE IF NOT EXISTS parallel_wave (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL,
			status TEXT NOT NULL DEFAULT 'created',
			phase TEXT NOT NULL DEFAULT 'initialized',
			gate_state TEXT NOT NULL DEFAULT 'none',
			control_state TEXT NOT NULL DEFAULT 'active',
			control_reason TEXT NOT NULL DEFAULT '',
			base_sha TEXT NOT NULL,
			base_branch TEXT NOT NULL DEFAULT '',
			project_cwd TEXT NOT NULL,
			worktree_root TEXT NOT NULL,
			orchestration_epoch INTEGER NOT NULL DEFAULT 0,
			created_by_session_id INTEGER,
			epic_doc_id INTEGER,
			parent_wave_id INTEGER,
			root_wave_id INTEGER,
			depth INTEGER NOT NULL DEFAULT 0,
			max_child_wave_depth INTEGER NOT NULL DEFAULT 0,
			max_total_descendant_waves INTEGER NOT NULL DEFAULT 0,
			max_total_sessions INTEGER NOT NULL DEFAULT 0,
			failure_policy_state TEXT NOT NULL DEFAULT 'none',
			repair_worker_id INTEGER,
			repair_attempt_count INTEGER NOT NULL DEFAULT 0,
			handoff_sha TEXT NOT NULL DEFAULT '',
			acceptance_session_id INTEGER,
			review_policy TEXT NOT NULL DEFAULT 'manual',
			review_backend TEXT NOT NULL DEFAULT 'codex',
				review_model TEXT NOT NULL DEFAULT 'opencode-go/deepseek-v4-flash',
			review_reasoning TEXT NOT NULL DEFAULT 'high',
			failure_reason TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			launched_at TEXT,
			completed_at TEXT,
			FOREIGN KEY(project_id) REFERENCES project(id) ON DELETE CASCADE ON UPDATE NO ACTION,
			FOREIGN KEY(created_by_session_id) REFERENCES session(id) ON DELETE SET NULL ON UPDATE NO ACTION,
			FOREIGN KEY(epic_doc_id) REFERENCES project_doc(id) ON DELETE SET NULL ON UPDATE NO ACTION,
			FOREIGN KEY(parent_wave_id) REFERENCES parallel_wave(id) ON DELETE RESTRICT ON UPDATE NO ACTION,
			FOREIGN KEY(root_wave_id) REFERENCES parallel_wave(id) ON DELETE RESTRICT ON UPDATE NO ACTION,
			FOREIGN KEY(repair_worker_id) REFERENCES parallel_wave_worker(id) ON DELETE SET NULL ON UPDATE NO ACTION,
			FOREIGN KEY(acceptance_session_id) REFERENCES session(id) ON DELETE SET NULL ON UPDATE NO ACTION
		);
	`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS parallel_wave_project_status_idx ON parallel_wave(project_id, status);`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS parallel_wave_project_root_idx ON parallel_wave(project_id, root_wave_id, id);`)
	_, _ = db.Exec(`
		CREATE TABLE IF NOT EXISTS mcp_binary_state (
			project_id INTEGER PRIMARY KEY,
			running_build_epoch INTEGER NOT NULL DEFAULT 0,
			required_build_epoch INTEGER NOT NULL DEFAULT 0,
			restart_required INTEGER NOT NULL DEFAULT 0,
			running_build_id TEXT NOT NULL DEFAULT '',
			required_build_id TEXT NOT NULL DEFAULT '',
			running_process_identity TEXT NOT NULL DEFAULT '',
			required_by_process_identity TEXT NOT NULL DEFAULT '',
			confirmed_at TEXT,
			reason TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(project_id) REFERENCES project(id) ON DELETE CASCADE ON UPDATE NO ACTION
		);
	`)
	_, _ = db.Exec(`
		CREATE TABLE IF NOT EXISTS parallel_wave_worker (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			wave_id INTEGER NOT NULL,
			project_id INTEGER NOT NULL,
			session_id INTEGER NOT NULL,
			status TEXT NOT NULL DEFAULT 'created',
			declared_write_scope TEXT NOT NULL,
			branch_name TEXT NOT NULL,
			worktree_path TEXT NOT NULL,
			base_sha TEXT NOT NULL,
			head_sha TEXT NOT NULL DEFAULT '',
			changed_paths TEXT NOT NULL DEFAULT '[]',
			diff_patch_path TEXT NOT NULL DEFAULT '',
			diff_stat TEXT NOT NULL DEFAULT '',
			launch_epoch INTEGER NOT NULL DEFAULT 0,
			worker_process_id INTEGER,
			external_session_id TEXT NOT NULL DEFAULT '',
			headless_log_path TEXT NOT NULL DEFAULT '',
			launcher_log_path TEXT NOT NULL DEFAULT '',
			worker_metadata_path TEXT NOT NULL DEFAULT '',
			failure_reason TEXT NOT NULL DEFAULT '',
			terminal_outcome TEXT NOT NULL DEFAULT '',
			retry_attempt_count INTEGER NOT NULL DEFAULT 0,
			retry_cause TEXT NOT NULL DEFAULT '',
			retry_next_eligible_at TEXT NOT NULL DEFAULT '',
			cleanup_status TEXT NOT NULL DEFAULT 'pending',
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			launched_at TEXT,
			terminal_at TEXT,
			cleaned_at TEXT,
			FOREIGN KEY(wave_id) REFERENCES parallel_wave(id) ON DELETE CASCADE ON UPDATE NO ACTION,
			FOREIGN KEY(project_id) REFERENCES project(id) ON DELETE CASCADE ON UPDATE NO ACTION,
			FOREIGN KEY(session_id) REFERENCES session(id) ON DELETE CASCADE ON UPDATE NO ACTION,
			FOREIGN KEY(worker_process_id) REFERENCES worker_process(id) ON DELETE SET NULL ON UPDATE NO ACTION
		);
	`)
	_, _ = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS parallel_wave_worker_wave_session_unique_idx ON parallel_wave_worker(wave_id, session_id);`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS parallel_wave_worker_status_idx ON parallel_wave_worker(project_id, status);`)
	_, _ = db.Exec(`
		CREATE TABLE IF NOT EXISTS parallel_wave_scope_lease (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL,
			wave_id INTEGER NOT NULL,
			scope_path TEXT NOT NULL,
			active INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			released_at TEXT,
			FOREIGN KEY(project_id) REFERENCES project(id) ON DELETE CASCADE ON UPDATE NO ACTION,
			FOREIGN KEY(wave_id) REFERENCES parallel_wave(id) ON DELETE CASCADE ON UPDATE NO ACTION
		);
	`)
	_, _ = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS parallel_wave_scope_lease_wave_scope_unique_idx ON parallel_wave_scope_lease(wave_id, scope_path);`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS parallel_wave_scope_lease_active_idx ON parallel_wave_scope_lease(project_id, active, wave_id);`)
	_, _ = db.Exec(`
			CREATE TABLE IF NOT EXISTS wave_worker_dependency (
				wave_id INTEGER NOT NULL,
				parent_session_id INTEGER NOT NULL,
				child_session_id INTEGER NOT NULL,
				FOREIGN KEY(wave_id) REFERENCES parallel_wave(id) ON DELETE CASCADE ON UPDATE NO ACTION,
				FOREIGN KEY(parent_session_id) REFERENCES session(id) ON DELETE CASCADE ON UPDATE NO ACTION,
				FOREIGN KEY(child_session_id) REFERENCES session(id) ON DELETE CASCADE ON UPDATE NO ACTION
			);
		`)
	_, _ = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS wave_worker_dependency_unique_idx ON wave_worker_dependency(wave_id, parent_session_id, child_session_id);`)
	_, _ = db.Exec(`
		CREATE TABLE IF NOT EXISTS planned_wave (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL,
			title TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'planned' CHECK(status IN ('planned', 'initializing', 'initialized', 'failed')),
			idempotency_key TEXT NOT NULL DEFAULT '',
			definition_hash TEXT NOT NULL,
			reason TEXT NOT NULL DEFAULT '',
			base_ref TEXT NOT NULL DEFAULT '',
			worktree_root TEXT NOT NULL DEFAULT '',
			epic_doc_id INTEGER,
			parent_wave_id INTEGER,
			max_child_wave_depth INTEGER NOT NULL DEFAULT 0,
			max_total_descendant_waves INTEGER NOT NULL DEFAULT 0,
			max_total_sessions INTEGER NOT NULL DEFAULT 0,
			initialized_wave_id INTEGER,
			failure_reason TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			initialized_at TEXT,
			FOREIGN KEY(project_id) REFERENCES project(id) ON DELETE CASCADE ON UPDATE NO ACTION,
			FOREIGN KEY(epic_doc_id) REFERENCES project_doc(id) ON DELETE SET NULL ON UPDATE NO ACTION,
			FOREIGN KEY(parent_wave_id) REFERENCES parallel_wave(id) ON DELETE RESTRICT ON UPDATE NO ACTION,
			FOREIGN KEY(initialized_wave_id) REFERENCES parallel_wave(id) ON DELETE RESTRICT ON UPDATE NO ACTION
		);
	`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS planned_wave_project_status_idx ON planned_wave(project_id, status, id);`)
	_, _ = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS planned_wave_project_idempotency_unique_idx ON planned_wave(project_id, idempotency_key) WHERE idempotency_key != '';`)
	_, _ = db.Exec(`
		CREATE TABLE IF NOT EXISTS planned_wave_task (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			planned_wave_id INTEGER NOT NULL,
			project_id INTEGER NOT NULL,
			task_key TEXT NOT NULL,
			position INTEGER NOT NULL,
			task_description TEXT NOT NULL,
			declared_write_scope TEXT NOT NULL,
			dependencies TEXT NOT NULL DEFAULT '[]',
			cli_backend TEXT NOT NULL DEFAULT 'codex',
			cli_model TEXT NOT NULL DEFAULT '',
			cli_reasoning TEXT NOT NULL DEFAULT '',
			mcp_server_names TEXT NOT NULL DEFAULT '[]',
			materialized_session_id INTEGER,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(planned_wave_id) REFERENCES planned_wave(id) ON DELETE CASCADE ON UPDATE NO ACTION,
			FOREIGN KEY(project_id) REFERENCES project(id) ON DELETE CASCADE ON UPDATE NO ACTION,
			FOREIGN KEY(materialized_session_id) REFERENCES session(id) ON DELETE RESTRICT ON UPDATE NO ACTION
		);
	`)
	_, _ = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS planned_wave_task_key_unique_idx ON planned_wave_task(planned_wave_id, task_key);`)
	_, _ = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS planned_wave_task_position_unique_idx ON planned_wave_task(planned_wave_id, position);`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS planned_wave_task_project_idx ON planned_wave_task(project_id, planned_wave_id, position);`)
	_, _ = db.Exec(`ALTER TABLE planned_wave_task ADD COLUMN cli_backend TEXT NOT NULL DEFAULT 'codex';`)
	_, _ = db.Exec(`ALTER TABLE planned_wave_task ADD COLUMN cli_model TEXT NOT NULL DEFAULT '';`)
	_, _ = db.Exec(`ALTER TABLE planned_wave_task ADD COLUMN cli_reasoning TEXT NOT NULL DEFAULT '';`)
	_, _ = db.Exec(`ALTER TABLE planned_wave_task ADD COLUMN mcp_server_names TEXT NOT NULL DEFAULT '[]';`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS worker_process_parallel_wave_idx ON worker_process(parallel_wave_id, parallel_wave_worker_id);`)
	_, _ = db.Exec(`
		CREATE TABLE IF NOT EXISTS project_overview (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL,
			content TEXT NOT NULL,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(project_id) REFERENCES project(id) ON DELETE CASCADE ON UPDATE NO ACTION
		);
	`)
	_, _ = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS project_overview_project_unique_idx ON project_overview(project_id);`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS project_overview_project_idx ON project_overview(project_id);`)
	_, _ = db.Exec(`
		CREATE TABLE IF NOT EXISTS project_balance (
			project_id INTEGER NOT NULL UNIQUE,
			balance_units INTEGER NOT NULL DEFAULT 0,
			currency TEXT NOT NULL DEFAULT 'usd',
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(project_id) REFERENCES project(id) ON DELETE CASCADE ON UPDATE NO ACTION
		);
	`)
	_, _ = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS project_balance_project_unique_idx ON project_balance(project_id);`)
	_, _ = db.Exec(`
		CREATE TABLE IF NOT EXISTS fixer_spend_authority (
			project_id INTEGER NOT NULL UNIQUE,
			enabled INTEGER NOT NULL DEFAULT 0,
			allowance_units INTEGER NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(project_id) REFERENCES project(id) ON DELETE CASCADE ON UPDATE NO ACTION
		);
	`)
	_, _ = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS fixer_spend_authority_project_unique_idx ON fixer_spend_authority(project_id);`)
	_, _ = db.Exec(`
		CREATE TABLE IF NOT EXISTS balance_ledger (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL,
			delta_units INTEGER NOT NULL,
			kind TEXT NOT NULL CHECK(kind IN ('credit', 'spend', 'authority_grant')),
			reason TEXT NOT NULL,
			actor_role TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(project_id) REFERENCES project(id) ON DELETE CASCADE ON UPDATE NO ACTION
		);
	`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS balance_ledger_project_id_idx ON balance_ledger(project_id, id);`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS balance_ledger_kind_idx ON balance_ledger(project_id, kind, id);`)
	_, _ = db.Exec(`
		CREATE TABLE IF NOT EXISTS fixer_mcp_feedback (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL,
			feedback_type TEXT NOT NULL,
			content TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(project_id) REFERENCES project(id) ON DELETE CASCADE ON UPDATE NO ACTION
		);
	`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS fixer_mcp_feedback_project_idx ON fixer_mcp_feedback(project_id);`)
	_, _ = db.Exec(`
		CREATE TABLE IF NOT EXISTS overseer_fixer_message (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL,
			sender_role TEXT NOT NULL CHECK(sender_role IN ('overseer', 'fixer')),
			content TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(project_id) REFERENCES project(id) ON DELETE CASCADE ON UPDATE NO ACTION
		);
	`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS overseer_fixer_message_project_id_idx ON overseer_fixer_message(project_id, id);`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS overseer_fixer_message_sender_idx ON overseer_fixer_message(sender_role, id);`)
	_, _ = db.Exec(`
		CREATE TABLE IF NOT EXISTS overseer_fixer_run_state (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL,
			active INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT '',
			reason TEXT NOT NULL DEFAULT '',
			last_message_id INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(project_id) REFERENCES project(id) ON DELETE CASCADE ON UPDATE NO ACTION
		);
	`)
	_, _ = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS overseer_fixer_run_state_project_unique_idx ON overseer_fixer_run_state(project_id);`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS overseer_fixer_run_state_active_idx ON overseer_fixer_run_state(active, project_id);`)
	_, _ = db.Exec(`UPDATE project SET active = 0 WHERE active IS NULL`)
	_, _ = db.Exec(`
		INSERT INTO session_external_link (session_id, backend, external_session_id, updated_at)
		SELECT legacy.session_id, 'codex', legacy.codex_session_id, COALESCE(legacy.updated_at, CURRENT_TIMESTAMP)
		FROM session_codex_link AS legacy
		LEFT JOIN session_external_link AS external_link
			ON external_link.session_id = legacy.session_id
		   AND external_link.backend = 'codex'
		WHERE external_link.id IS NULL
	`)
	_, _ = db.Exec(`UPDATE session SET cli_backend = 'codex' WHERE COALESCE(TRIM(cli_backend), '') = ''`)
	_, _ = db.Exec(`UPDATE session SET cli_model = '' WHERE cli_model IS NULL`)
	_, _ = db.Exec(`UPDATE session SET cli_reasoning = '' WHERE cli_reasoning IS NULL`)
	_, _ = db.Exec(`UPDATE session SET declared_write_scope = ? WHERE COALESCE(TRIM(declared_write_scope), '') = ''`, defaultDeclaredWriteScope)
	_, _ = db.Exec(`UPDATE session SET parallel_wave_id = '' WHERE parallel_wave_id IS NULL`)
	_, _ = db.Exec(`UPDATE session SET rework_count = 0 WHERE rework_count IS NULL`)
	_, _ = db.Exec(`UPDATE session SET forced_stop_count = 0 WHERE forced_stop_count IS NULL`)
	_, _ = db.Exec(`UPDATE autonomous_run_status SET orchestration_epoch = 0 WHERE orchestration_epoch IS NULL`)
	_, _ = db.Exec(`UPDATE autonomous_run_status SET orchestration_frozen = 0 WHERE orchestration_frozen IS NULL`)
	_, _ = db.Exec(`UPDATE autonomous_run_status SET notifications_enabled_for_active_run = 1 WHERE notifications_enabled_for_active_run IS NULL`)
	_, _ = db.Exec(`UPDATE parallel_wave SET root_wave_id = id WHERE COALESCE(root_wave_id, 0) = 0`)
	_, _ = db.Exec(`UPDATE parallel_wave SET depth = 0 WHERE depth IS NULL OR depth < 0`)
	_, _ = db.Exec(`UPDATE parallel_wave SET max_child_wave_depth = 0 WHERE max_child_wave_depth IS NULL OR max_child_wave_depth < 0`)
	_, _ = db.Exec(`UPDATE parallel_wave SET max_total_descendant_waves = 0 WHERE max_total_descendant_waves IS NULL OR max_total_descendant_waves < 0`)
	_, _ = db.Exec(`UPDATE parallel_wave SET max_total_sessions = 0 WHERE max_total_sessions IS NULL OR max_total_sessions < 0`)
	_, _ = db.Exec(`UPDATE parallel_wave SET failure_policy_state = 'none' WHERE COALESCE(TRIM(failure_policy_state), '') = ''`)
	_, _ = db.Exec(`UPDATE parallel_wave SET repair_attempt_count = 0 WHERE repair_attempt_count IS NULL OR repair_attempt_count < 0`)
	_, _ = db.Exec(`UPDATE parallel_wave SET handoff_sha = '' WHERE handoff_sha IS NULL`)
	_, _ = db.Exec(`UPDATE parallel_wave SET phase = CASE WHEN status IN ('completed', 'cleaned') THEN 'completed' WHEN status = 'created' THEN 'initialized' ELSE 'implementation' END WHERE COALESCE(TRIM(phase), '') = '' OR (phase = 'initialized' AND status <> 'created')`)
	_, _ = db.Exec(`UPDATE parallel_wave SET gate_state = 'none' WHERE COALESCE(TRIM(gate_state), '') = ''`)
	_, _ = db.Exec(`UPDATE parallel_wave SET control_state = 'active' WHERE COALESCE(TRIM(control_state), '') = ''`)
	_, _ = db.Exec(`UPDATE parallel_wave SET control_reason = '' WHERE control_reason IS NULL`)
	_, _ = db.Exec(`UPDATE parallel_wave SET review_policy = 'manual' WHERE COALESCE(TRIM(review_policy), '') NOT IN ('automatic', 'manual')`)
	_, _ = db.Exec(`UPDATE parallel_wave SET review_backend = 'codex' WHERE COALESCE(TRIM(review_backend), '') = ''`)
	_, _ = db.Exec(`UPDATE parallel_wave SET review_model = 'opencode-go/deepseek-v4-flash' WHERE COALESCE(TRIM(review_model), '') = '' OR review_model = 'opencode-go/deepseek-v4-pro'`)
	_, _ = db.Exec(`UPDATE parallel_wave SET review_reasoning = 'high' WHERE COALESCE(TRIM(review_reasoning), '') = ''`)
	_, _ = db.Exec(`UPDATE parallel_wave_worker SET head_sha = '' WHERE head_sha IS NULL`)
	_, _ = db.Exec(`UPDATE parallel_wave_worker SET changed_paths = '[]' WHERE COALESCE(TRIM(changed_paths), '') = ''`)
	_, _ = db.Exec(`UPDATE parallel_wave_worker SET diff_patch_path = '' WHERE diff_patch_path IS NULL`)
	_, _ = db.Exec(`UPDATE parallel_wave_worker SET diff_stat = '' WHERE diff_stat IS NULL`)
	_, _ = db.Exec(`UPDATE parallel_wave_worker SET external_session_id = '' WHERE external_session_id IS NULL`)
	_, _ = db.Exec(`ALTER TABLE parallel_wave_worker ADD COLUMN headless_log_path TEXT NOT NULL DEFAULT '';`)
	_, _ = db.Exec(`ALTER TABLE parallel_wave_worker ADD COLUMN launcher_log_path TEXT NOT NULL DEFAULT '';`)
	_, _ = db.Exec(`ALTER TABLE parallel_wave_worker ADD COLUMN worker_metadata_path TEXT NOT NULL DEFAULT '';`)
	_, _ = db.Exec(`UPDATE parallel_wave_worker SET headless_log_path = '' WHERE headless_log_path IS NULL`)
	_, _ = db.Exec(`UPDATE parallel_wave_worker SET launcher_log_path = '' WHERE launcher_log_path IS NULL`)
	_, _ = db.Exec(`UPDATE parallel_wave_worker SET worker_metadata_path = '' WHERE worker_metadata_path IS NULL`)
	_, _ = db.Exec(`UPDATE parallel_wave_worker SET failure_reason = '' WHERE failure_reason IS NULL`)
	_, _ = db.Exec(`UPDATE parallel_wave_worker SET terminal_outcome = CASE WHEN status = 'cleaned' THEN '' WHEN status IN ('review_ready', 'completed', 'failed', 'stopped', 'stale_epoch', 'blocked') THEN status ELSE '' END WHERE COALESCE(TRIM(terminal_outcome), '') = ''`)
	_, _ = db.Exec(`UPDATE parallel_wave_worker SET retry_attempt_count = 0 WHERE retry_attempt_count IS NULL OR retry_attempt_count < 0`)
	_, _ = db.Exec(`UPDATE parallel_wave_worker SET retry_cause = '' WHERE retry_cause IS NULL`)
	_, _ = db.Exec(`UPDATE parallel_wave_worker SET retry_next_eligible_at = '' WHERE retry_next_eligible_at IS NULL`)
	_, _ = db.Exec(`UPDATE parallel_wave_worker SET cleanup_status = 'pending' WHERE COALESCE(TRIM(cleanup_status), '') = ''`)

	if err := initProjectWorkroomSchema(); err != nil {
		log.Fatalf("Error initializing Project Workroom schema: %v", err)
	}

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM project").Scan(&count)
	if err == nil && count == 0 {
		seedCWD, cwdErr := os.Getwd()
		if cwdErr != nil {
			log.Fatalf("Error resolving seed cwd: %v", cwdErr)
		}
		seedName := filepath.Base(seedCWD)
		if seedName == "" || seedName == "." || seedName == string(filepath.Separator) {
			seedName = "Fixer MCP"
		}
		_, err = db.Exec(`INSERT INTO project (name, cwd) VALUES (?, ?)`, seedName, seedCWD)
		if err != nil {
			log.Fatalf("Error seeding projects: %v", err)
		}

		_, err = db.Exec(`INSERT INTO session (project_id, task_description, status) VALUES (1, 'Bootstrap Fixer MCP workspace', 'pending')`)
		if err != nil {
			log.Fatalf("Error seeding session: %v", err)
		}
	}

	synced, syncErr := syncMcpRegistryFromConfig(filepath.Join(".", "mcp_config.json"))
	if syncErr != nil {
		log.Printf("MCP registry sync skipped: %v", syncErr)
	} else if synced > 0 {
		log.Printf("MCP registry synced from config: %d server(s)", synced)
	}
	if err := applyCuratedDefaultMcpServers(); err != nil {
		log.Printf("curated MCP defaults seed skipped: %v", err)
	}
	if err := ensureGlobalEdgeMcpBindings(); err != nil {
		log.Printf("global Edge MCP project bindings seed skipped: %v", err)
	}
	if err := applyMcpMarketplaceCatalog(); err != nil {
		log.Printf("MCP marketplace catalog seed skipped: %v", err)
	}
	if err := seedProjectScopedMcpBindings(); err != nil {
		log.Printf("project MCP binding seed skipped: %v", err)
	}
	if err := pruneDeprecatedProjectMcpBindings(); err != nil {
		log.Printf("deprecated project MCP binding cleanup skipped: %v", err)
	}

	if err := seedRolePreprompts(); err != nil {
		log.Printf("role preprompt seed skipped: %v", err)
	}
}

func initProjectWorkroomSchema() error {
	_, err := db.Exec(`
		CREATE TRIGGER IF NOT EXISTS session_timestamps_after_insert
		AFTER INSERT ON session
		WHEN NEW.created_at IS NULL OR TRIM(NEW.created_at) = ''
		  OR NEW.updated_at IS NULL OR TRIM(NEW.updated_at) = ''
		BEGIN
			UPDATE session
			SET created_at = COALESCE(NULLIF(TRIM(NEW.created_at), ''), CURRENT_TIMESTAMP),
			    updated_at = COALESCE(NULLIF(TRIM(NEW.updated_at), ''), CURRENT_TIMESTAMP)
			WHERE id = NEW.id;
		END;
		CREATE TRIGGER IF NOT EXISTS session_updated_at_after_update
		AFTER UPDATE ON session
		WHEN NEW.updated_at IS OLD.updated_at
		  OR NEW.updated_at IS NULL OR TRIM(NEW.updated_at) = ''
		BEGIN
			UPDATE session SET updated_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
		END;

		CREATE TABLE IF NOT EXISTS project_ui_cursor (
			project_id INTEGER PRIMARY KEY,
			next_seq INTEGER NOT NULL DEFAULT 1 CHECK(next_seq >= 1),
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(project_id) REFERENCES project(id) ON DELETE CASCADE ON UPDATE NO ACTION
		);
		CREATE TABLE IF NOT EXISTS project_ui_event (
			project_id INTEGER NOT NULL,
			seq INTEGER NOT NULL CHECK(seq > 0),
			event_id TEXT NOT NULL UNIQUE,
			schema_version INTEGER NOT NULL DEFAULT 1 CHECK(schema_version = 1),
			kind TEXT NOT NULL,
			aggregate_type TEXT NOT NULL,
			aggregate_id TEXT NOT NULL,
			aggregate_revision INTEGER NOT NULL CHECK(aggregate_revision > 0),
			payload_json TEXT NOT NULL CHECK(length(payload_json) <= 65536 AND json_valid(payload_json)),
			actor_kind TEXT NOT NULL CHECK(actor_kind IN ('principal', 'fixer', 'hands', 'system')),
			actor_id TEXT NOT NULL,
			causation_id TEXT NOT NULL,
			correlation_id TEXT NOT NULL,
			created_at TEXT NOT NULL,
			PRIMARY KEY(project_id, seq),
			FOREIGN KEY(project_id) REFERENCES project(id) ON DELETE CASCADE ON UPDATE NO ACTION
		);
		CREATE INDEX IF NOT EXISTS project_ui_event_aggregate_idx
			ON project_ui_event(project_id, aggregate_type, aggregate_id, seq);
		CREATE TABLE IF NOT EXISTS command_dedup (
			project_id INTEGER NOT NULL,
			principal_id TEXT NOT NULL,
			command_kind TEXT NOT NULL,
			idempotency_key TEXT NOT NULL,
			request_hash TEXT NOT NULL,
			result_json TEXT NOT NULL CHECK(json_valid(result_json)),
			created_at TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			PRIMARY KEY(project_id, principal_id, command_kind, idempotency_key),
			FOREIGN KEY(project_id) REFERENCES project(id) ON DELETE CASCADE ON UPDATE NO ACTION
		);
		CREATE INDEX IF NOT EXISTS command_dedup_expiry_idx ON command_dedup(expires_at);

		CREATE TABLE IF NOT EXISTS fixer_thread (
			id TEXT PRIMARY KEY,
			project_id INTEGER NOT NULL,
			provider TEXT NOT NULL,
			external_session_id TEXT,
			headline TEXT NOT NULL CHECK(length(headline) <= 160),
			state TEXT NOT NULL CHECK(state IN ('active', 'archived', 'unavailable')),
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			FOREIGN KEY(project_id) REFERENCES project(id) ON DELETE CASCADE ON UPDATE NO ACTION
		);
		CREATE UNIQUE INDEX IF NOT EXISTS fixer_thread_project_id_unique_idx ON fixer_thread(project_id, id);
		CREATE INDEX IF NOT EXISTS fixer_thread_project_updated_idx ON fixer_thread(project_id, updated_at, id);
		CREATE TABLE IF NOT EXISTS fixer_turn (
			id TEXT PRIMARY KEY,
			project_id INTEGER NOT NULL,
			thread_id TEXT NOT NULL,
			ordinal INTEGER NOT NULL CHECK(ordinal > 0),
			role TEXT NOT NULL CHECK(role IN ('user', 'fixer', 'tool', 'system')),
			content TEXT NOT NULL CHECK(length(content) <= 65536),
			status TEXT NOT NULL CHECK(status IN ('accepted', 'streaming', 'complete', 'failed')),
			client_message_id TEXT,
			provider_turn_id TEXT,
			created_at TEXT NOT NULL,
			completed_at TEXT,
			UNIQUE(thread_id, ordinal),
			UNIQUE(thread_id, client_message_id),
			FOREIGN KEY(project_id, thread_id) REFERENCES fixer_thread(project_id, id) ON DELETE CASCADE ON UPDATE NO ACTION
		);

		CREATE TABLE IF NOT EXISTS genui_surface_instance (
			id TEXT PRIMARY KEY,
			project_id INTEGER NOT NULL,
			thread_id TEXT NOT NULL,
			caused_by_turn_id TEXT NOT NULL,
			surface_type TEXT NOT NULL,
			surface_version INTEGER NOT NULL CHECK(surface_version > 0),
			state TEXT NOT NULL CHECK(state IN ('presented', 'superseded', 'dismissed', 'revoked')),
			current_revision INTEGER NOT NULL CHECK(current_revision > 0),
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE(project_id, id),
			FOREIGN KEY(project_id, thread_id) REFERENCES fixer_thread(project_id, id) ON DELETE CASCADE ON UPDATE NO ACTION,
			FOREIGN KEY(caused_by_turn_id) REFERENCES fixer_turn(id) ON DELETE RESTRICT ON UPDATE NO ACTION
		);
		CREATE INDEX IF NOT EXISTS genui_surface_project_thread_idx ON genui_surface_instance(project_id, thread_id, updated_at);
		CREATE TABLE IF NOT EXISTS genui_surface_revision (
			surface_id TEXT NOT NULL,
			revision INTEGER NOT NULL CHECK(revision > 0),
			source_seq INTEGER NOT NULL CHECK(source_seq > 0),
			document_json TEXT NOT NULL CHECK(length(document_json) <= 262144 AND json_valid(document_json)),
			document_hash TEXT NOT NULL,
			renderer_version TEXT NOT NULL,
			created_at TEXT NOT NULL,
			PRIMARY KEY(surface_id, revision),
			FOREIGN KEY(surface_id) REFERENCES genui_surface_instance(id) ON DELETE CASCADE ON UPDATE NO ACTION
		);
		CREATE TABLE IF NOT EXISTS genui_demand_example (
			id TEXT PRIMARY KEY,
			project_id INTEGER NOT NULL,
			thread_id TEXT NOT NULL DEFAULT '',
			turn_id TEXT NOT NULL DEFAULT '',
			requested_type TEXT,
			requested_version INTEGER,
			request_json TEXT NOT NULL CHECK(length(request_json) <= 65536 AND json_valid(request_json)),
			provider TEXT NOT NULL,
			model TEXT NOT NULL,
			rejection_code TEXT NOT NULL,
			privacy_class TEXT NOT NULL CHECK(privacy_class IN ('project_internal', 'sensitive', 'restricted')),
			triage_state TEXT NOT NULL DEFAULT 'new' CHECK(triage_state IN ('new', 'accepted', 'duplicate', 'rejected', 'shipped')),
			created_at TEXT NOT NULL,
			FOREIGN KEY(project_id) REFERENCES project(id) ON DELETE CASCADE ON UPDATE NO ACTION
		);
		CREATE INDEX IF NOT EXISTS genui_demand_project_created_idx ON genui_demand_example(project_id, created_at, id);
		CREATE TABLE IF NOT EXISTS genui_surface_feedback (
			surface_id TEXT NOT NULL,
			revision INTEGER NOT NULL,
			principal_id TEXT NOT NULL,
			vote INTEGER NOT NULL CHECK(vote IN (-1, 1)),
			reason_code TEXT,
			comment TEXT CHECK(comment IS NULL OR length(comment) <= 1000),
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY(surface_id, revision, principal_id),
			FOREIGN KEY(surface_id, revision) REFERENCES genui_surface_revision(surface_id, revision) ON DELETE CASCADE ON UPDATE NO ACTION
		);
		CREATE TABLE IF NOT EXISTS genui_action_invocation (
			id TEXT PRIMARY KEY,
			project_id INTEGER NOT NULL,
			surface_id TEXT NOT NULL,
			surface_revision INTEGER NOT NULL,
			action_id TEXT NOT NULL,
			action_version INTEGER NOT NULL CHECK(action_version > 0),
			target_type TEXT NOT NULL,
			target_id TEXT NOT NULL,
			input_json TEXT NOT NULL CHECK(length(input_json) <= 65536 AND json_valid(input_json)),
			principal_id TEXT NOT NULL,
			decision TEXT NOT NULL CHECK(decision IN ('pending', 'denied', 'authorized')),
			status TEXT NOT NULL CHECK(status IN ('received', 'needs_confirmation', 'executing', 'succeeded', 'failed')),
			reason_code TEXT,
			idempotency_key TEXT NOT NULL,
			created_at TEXT NOT NULL,
			completed_at TEXT,
			UNIQUE(project_id, principal_id, action_id, idempotency_key),
			FOREIGN KEY(project_id, surface_id) REFERENCES genui_surface_instance(project_id, id) ON DELETE CASCADE ON UPDATE NO ACTION,
			FOREIGN KEY(surface_id, surface_revision) REFERENCES genui_surface_revision(surface_id, revision) ON DELETE RESTRICT ON UPDATE NO ACTION
		);

		CREATE TABLE IF NOT EXISTS project_hands (
			project_id INTEGER PRIMARY KEY,
			actor_id TEXT NOT NULL UNIQUE,
			display_name TEXT NOT NULL DEFAULT 'Руки' CHECK(display_name = 'Руки'),
			authority_state TEXT NOT NULL DEFAULT 'enabled' CHECK(authority_state IN ('enabled', 'disabled', 'revoked')),
			default_lane TEXT NOT NULL DEFAULT 'codex' CHECK(default_lane IN ('codex', 'claude', 'kimi-code', 'antigravity', 'grok')),
			next_instruction_ordinal INTEGER NOT NULL DEFAULT 1 CHECK(next_instruction_ordinal > 0),
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(project_id) REFERENCES project(id) ON DELETE CASCADE ON UPDATE NO ACTION
		);
		CREATE TABLE IF NOT EXISTS hands_instruction (
			id TEXT PRIMARY KEY,
			project_id INTEGER NOT NULL,
			actor_id TEXT NOT NULL,
			ordinal INTEGER NOT NULL CHECK(ordinal > 0),
			source_channel_kind TEXT NOT NULL,
			source_channel_id TEXT NOT NULL,
			source_message_id TEXT NOT NULL DEFAULT '',
			issuer_principal_id TEXT NOT NULL,
			instruction_text TEXT NOT NULL CHECK(length(instruction_text) <= 65536),
			declared_write_scope_json TEXT NOT NULL CHECK(json_valid(declared_write_scope_json)),
			instruction_envelope_json TEXT NOT NULL CHECK(length(instruction_envelope_json) <= 131072 AND json_valid(instruction_envelope_json)),
			requested_lane TEXT NOT NULL CHECK(requested_lane IN ('codex', 'claude', 'kimi-code', 'antigravity', 'grok')),
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
		);
		CREATE INDEX IF NOT EXISTS hands_instruction_project_state_idx ON hands_instruction(project_id, state, ordinal);
		CREATE TABLE IF NOT EXISTS hands_instruction_event (
			instruction_id TEXT NOT NULL,
			ordinal INTEGER NOT NULL CHECK(ordinal > 0),
			event_type TEXT NOT NULL,
			from_state TEXT,
			to_state TEXT,
			actor_kind TEXT NOT NULL,
			actor_id TEXT NOT NULL,
			payload_json TEXT NOT NULL CHECK(length(payload_json) <= 65536 AND json_valid(payload_json)),
			created_at TEXT NOT NULL,
			PRIMARY KEY(instruction_id, ordinal),
			FOREIGN KEY(instruction_id) REFERENCES hands_instruction(id) ON DELETE CASCADE ON UPDATE NO ACTION
		);
		CREATE TABLE IF NOT EXISTS legacy_manual_session_link (
			project_id INTEGER NOT NULL,
			session_id INTEGER NOT NULL,
			disposition TEXT NOT NULL CHECK(disposition IN ('live', 'terminal', 'abandoned', 'unknown')),
			evidence_json TEXT NOT NULL CHECK(json_valid(evidence_json)),
			hands_instruction_id TEXT,
			classified_at TEXT NOT NULL,
			classified_by TEXT NOT NULL,
			PRIMARY KEY(project_id, session_id),
			FOREIGN KEY(project_id) REFERENCES project(id) ON DELETE CASCADE ON UPDATE NO ACTION,
			FOREIGN KEY(session_id) REFERENCES session(id) ON DELETE CASCADE ON UPDATE NO ACTION,
			FOREIGN KEY(hands_instruction_id) REFERENCES hands_instruction(id) ON DELETE SET NULL ON UPDATE NO ACTION
		);
		CREATE TABLE IF NOT EXISTS hands_generation (
			instruction_id TEXT NOT NULL,
			generation INTEGER NOT NULL CHECK(generation > 0),
			project_id INTEGER NOT NULL,
			compat_session_id INTEGER,
			provider TEXT NOT NULL CHECK(provider IN ('codex', 'claude', 'kimi-code', 'antigravity', 'grok')),
			model TEXT NOT NULL,
			reasoning TEXT NOT NULL,
			status TEXT NOT NULL CHECK(status IN ('planned', 'starting', 'running', 'stopped', 'failed', 'lost')),
			external_session_id TEXT,
			process_id INTEGER,
			process_start_identity TEXT,
			binary_build_id TEXT,
			binary_epoch INTEGER,
			lease_set_id TEXT,
			fencing_token INTEGER,
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
		);
		CREATE UNIQUE INDEX IF NOT EXISTS hands_generation_one_active_project_idx
			ON hands_generation(project_id) WHERE status IN ('starting', 'running');

		CREATE TABLE IF NOT EXISTS project_write_fence (
			project_id INTEGER PRIMARY KEY,
			next_token INTEGER NOT NULL DEFAULT 1 CHECK(next_token > 0),
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(project_id) REFERENCES project(id) ON DELETE CASCADE ON UPDATE NO ACTION
		);
		CREATE TABLE IF NOT EXISTS project_write_lease (
			id TEXT PRIMARY KEY,
			project_id INTEGER NOT NULL,
			lease_set_id TEXT NOT NULL,
			owner_kind TEXT NOT NULL CHECK(owner_kind IN ('wave_worker', 'wave_reviewer', 'hands_instruction')),
			owner_id TEXT NOT NULL,
			scope_path TEXT NOT NULL,
			fencing_token INTEGER NOT NULL CHECK(fencing_token > 0),
			state TEXT NOT NULL CHECK(state IN ('active', 'released', 'revoked')),
			process_id INTEGER,
			process_start_identity TEXT,
			binary_build_id TEXT NOT NULL,
			binary_epoch INTEGER NOT NULL,
			created_at TEXT NOT NULL,
			heartbeat_at TEXT NOT NULL,
			released_at TEXT,
			release_reason TEXT,
			FOREIGN KEY(project_id) REFERENCES project(id) ON DELETE CASCADE ON UPDATE NO ACTION
		);
		CREATE INDEX IF NOT EXISTS project_write_lease_active_scope_idx ON project_write_lease(project_id, state, scope_path);
		CREATE INDEX IF NOT EXISTS project_write_lease_set_idx ON project_write_lease(lease_set_id);
		CREATE UNIQUE INDEX IF NOT EXISTS project_write_lease_active_owner_scope_idx
			ON project_write_lease(project_id, owner_kind, owner_id, scope_path) WHERE state = 'active';
		CREATE TABLE IF NOT EXISTS workroom_audit_event (
			id TEXT PRIMARY KEY,
			project_id INTEGER NOT NULL,
			principal_id TEXT NOT NULL,
			action_id TEXT NOT NULL,
			target_type TEXT NOT NULL,
			target_id TEXT NOT NULL,
			decision TEXT NOT NULL,
			outcome TEXT NOT NULL,
			causation_id TEXT NOT NULL,
			correlation_id TEXT NOT NULL,
			detail_json TEXT NOT NULL CHECK(length(detail_json) <= 65536 AND json_valid(detail_json)),
			created_at TEXT NOT NULL,
			FOREIGN KEY(project_id) REFERENCES project(id) ON DELETE CASCADE ON UPDATE NO ACTION
		);
		CREATE INDEX IF NOT EXISTS workroom_audit_project_created_idx ON workroom_audit_event(project_id, created_at, id);
	`)
	if err != nil {
		return err
	}
	_, _ = db.Exec(`ALTER TABLE hands_instruction ADD COLUMN instruction_envelope_json TEXT;`)
	_, _ = db.Exec(`UPDATE hands_instruction SET instruction_envelope_json = '{}' WHERE instruction_envelope_json IS NULL OR NOT json_valid(instruction_envelope_json);`)
	_, _ = db.Exec(`DROP TRIGGER IF EXISTS project_hands_after_project_insert`)
	_, _ = db.Exec(`DROP TABLE IF EXISTS hands_provider_lane`)

	_, err = db.Exec(`
		INSERT OR IGNORE INTO project_hands (
			project_id, actor_id, display_name, authority_state, default_lane,
			next_instruction_ordinal, created_at, updated_at
		)
		SELECT id,
			lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' ||
			substr(lower(hex(randomblob(2))), 2) || '-' ||
			substr('89ab', abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))), 2) || '-' ||
			lower(hex(randomblob(6))),
			'Руки', 'enabled', 'codex', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
		FROM project;
	`)
	if err != nil {
		return err
	}

	_, err = db.Exec(`
		CREATE TRIGGER IF NOT EXISTS project_hands_after_project_insert
		AFTER INSERT ON project
		BEGIN
			INSERT OR IGNORE INTO project_hands (
				project_id, actor_id, display_name, authority_state, default_lane,
				next_instruction_ordinal, created_at, updated_at
			) VALUES (
				NEW.id,
				lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' ||
				substr(lower(hex(randomblob(2))), 2) || '-' ||
				substr('89ab', abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))), 2) || '-' ||
				lower(hex(randomblob(6))),
				'Руки', 'enabled', 'codex', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
			);
		END;
	`)
	if err != nil {
		return err
	}
	return migrateHandsGrokProviderLane()
}

// migrateHandsGrokProviderLane widens the Hands provider-lane CHECK constraints
// on databases created before 'grok' became a registered lane. SQLite cannot
// ALTER a CHECK constraint, so the three affected tables are rebuilt in place.
// Fresh databases already carry the widened CHECKs and skip every rebuild.
func migrateHandsGrokProviderLane() error {
	rebuilds := map[string][]string{
		"hands_generation": {
			`CREATE TABLE hands_generation_grok_mig (
				instruction_id TEXT NOT NULL,
				generation INTEGER NOT NULL CHECK(generation > 0),
				project_id INTEGER NOT NULL,
				compat_session_id INTEGER,
				provider TEXT NOT NULL CHECK(provider IN ('codex', 'claude', 'kimi-code', 'antigravity', 'grok')),
				model TEXT NOT NULL,
				reasoning TEXT NOT NULL,
				status TEXT NOT NULL CHECK(status IN ('planned', 'starting', 'running', 'stopped', 'failed', 'lost')),
				external_session_id TEXT,
				process_id INTEGER,
				process_start_identity TEXT,
				binary_build_id TEXT,
				binary_epoch INTEGER,
				lease_set_id TEXT,
				fencing_token INTEGER,
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
			);`,
			`INSERT INTO hands_generation_grok_mig SELECT instruction_id, generation, project_id, compat_session_id, provider, model, reasoning, status, external_session_id, process_id, process_start_identity, binary_build_id, binary_epoch, lease_set_id, fencing_token, launch_mode, result_envelope_json, started_at, heartbeat_at, ended_at, exit_code, stop_reason, created_at, updated_at FROM hands_generation;`,
			`DROP TABLE hands_generation;`,
			`ALTER TABLE hands_generation_grok_mig RENAME TO hands_generation;`,
			`CREATE UNIQUE INDEX IF NOT EXISTS hands_generation_one_active_project_idx ON hands_generation(project_id) WHERE status IN ('starting', 'running');`,
		},
		"hands_instruction": {
			`CREATE TABLE hands_instruction_grok_mig (
				id TEXT PRIMARY KEY,
				project_id INTEGER NOT NULL,
				actor_id TEXT NOT NULL,
				ordinal INTEGER NOT NULL CHECK(ordinal > 0),
				source_channel_kind TEXT NOT NULL,
				source_channel_id TEXT NOT NULL,
				source_message_id TEXT NOT NULL DEFAULT '',
				issuer_principal_id TEXT NOT NULL,
				instruction_text TEXT NOT NULL CHECK(length(instruction_text) <= 65536),
				declared_write_scope_json TEXT NOT NULL CHECK(json_valid(declared_write_scope_json)),
				instruction_envelope_json TEXT NOT NULL CHECK(length(instruction_envelope_json) <= 131072 AND json_valid(instruction_envelope_json)),
				requested_lane TEXT NOT NULL CHECK(requested_lane IN ('codex', 'claude', 'kimi-code', 'antigravity', 'grok')),
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
			);`,
			`INSERT INTO hands_instruction_grok_mig SELECT id, project_id, actor_id, ordinal, source_channel_kind, source_channel_id, source_message_id, issuer_principal_id, instruction_text, declared_write_scope_json, instruction_envelope_json, requested_lane, risk_class, review_policy, state, state_reason_code, state_reason_text, compat_session_id, idempotency_key, revision, created_at, updated_at, terminal_at FROM hands_instruction;`,
			`DROP TABLE hands_instruction;`,
			`ALTER TABLE hands_instruction_grok_mig RENAME TO hands_instruction;`,
			`CREATE INDEX IF NOT EXISTS hands_instruction_project_state_idx ON hands_instruction(project_id, state, ordinal);`,
		},
		"project_hands": {
			`CREATE TABLE project_hands_grok_mig (
				project_id INTEGER PRIMARY KEY,
				actor_id TEXT NOT NULL UNIQUE,
				display_name TEXT NOT NULL DEFAULT 'Руки' CHECK(display_name = 'Руки'),
				authority_state TEXT NOT NULL DEFAULT 'enabled' CHECK(authority_state IN ('enabled', 'disabled', 'revoked')),
				default_lane TEXT NOT NULL DEFAULT 'codex' CHECK(default_lane IN ('codex', 'claude', 'kimi-code', 'antigravity', 'grok')),
				next_instruction_ordinal INTEGER NOT NULL DEFAULT 1 CHECK(next_instruction_ordinal > 0),
				created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
				FOREIGN KEY(project_id) REFERENCES project(id) ON DELETE CASCADE ON UPDATE NO ACTION
			);`,
			`INSERT INTO project_hands_grok_mig SELECT project_id, actor_id, display_name, authority_state, default_lane, next_instruction_ordinal, created_at, updated_at FROM project_hands;`,
			`DROP TABLE project_hands;`,
			`ALTER TABLE project_hands_grok_mig RENAME TO project_hands;`,
		},
	}

	conn, err := db.Conn(context.Background())
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(context.Background(), `PRAGMA foreign_keys = OFF;`); err != nil {
		return err
	}
	defer conn.ExecContext(context.Background(), `PRAGMA foreign_keys = ON;`)

	for _, table := range []string{"hands_generation", "hands_instruction", "project_hands"} {
		var tableSQL string
		err := conn.QueryRowContext(context.Background(),
			`SELECT COALESCE(sql, '') FROM sqlite_master WHERE type = 'table' AND name = ?`, table,
		).Scan(&tableSQL)
		if err != nil {
			return err
		}
		if tableSQL == "" || strings.Contains(tableSQL, "'grok'") {
			continue
		}
		for _, stmt := range rebuilds[table] {
			if _, err := conn.ExecContext(context.Background(), stmt); err != nil {
				return fmt.Errorf("migrate %s for grok lane: %w", table, err)
			}
		}
	}
	return nil
}

func resolveFixerDBPath() string {
	if explicitPath := strings.TrimSpace(os.Getenv(fixerDBPathEnv)); explicitPath != "" {
		return explicitPath
	}
	return defaultFixerDBFilename
}
