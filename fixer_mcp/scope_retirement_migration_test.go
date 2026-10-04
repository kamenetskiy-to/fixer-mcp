package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// Declared write scope retirement: databases created before the retirement
// must lose every active scope column and lease table exactly once, keep their
// legacy values as read-only migration history, and keep rows, ordinals,
// immutable envelopes, foreign keys and CHECK constraints intact so resumed
// tasks, plans and Hands work continue without any scope input.

const legacyScopeFixtureValue = `["fixer_mcp", "docs"]`
const legacyEnvelopeSentinel = `{"protocol":"fixer.hands.instruction","sentinel":"IMMUTABLE-ENVELOPE-SENTINEL"}`

func seedLegacyWriteScopeSurface(t *testing.T) {
	t.Helper()
	statements := []string{
		`ALTER TABLE session ADD COLUMN declared_write_scope TEXT NOT NULL DEFAULT '["."]'`,
		`ALTER TABLE parallel_wave_worker ADD COLUMN declared_write_scope TEXT NOT NULL DEFAULT '["."]'`,
		`ALTER TABLE planned_wave_task ADD COLUMN declared_write_scope TEXT NOT NULL DEFAULT '["."]'`,
		`ALTER TABLE hands_instruction ADD COLUMN declared_write_scope_json TEXT NOT NULL DEFAULT '[]' CHECK(json_valid(declared_write_scope_json))`,
		`ALTER TABLE hands_generation ADD COLUMN lease_set_id TEXT`,
		`ALTER TABLE hands_generation ADD COLUMN fencing_token INTEGER`,
		`CREATE TABLE parallel_wave_scope_lease (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL,
			wave_id INTEGER NOT NULL,
			scope_path TEXT NOT NULL,
			active INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			released_at TEXT,
			FOREIGN KEY(project_id) REFERENCES project(id) ON DELETE CASCADE ON UPDATE NO ACTION,
			FOREIGN KEY(wave_id) REFERENCES parallel_wave(id) ON DELETE CASCADE ON UPDATE NO ACTION
		)`,
		`CREATE UNIQUE INDEX parallel_wave_scope_lease_wave_scope_unique_idx ON parallel_wave_scope_lease(wave_id, scope_path)`,
		`CREATE TABLE project_write_fence (
			project_id INTEGER PRIMARY KEY,
			next_token INTEGER NOT NULL DEFAULT 1 CHECK(next_token > 0),
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(project_id) REFERENCES project(id) ON DELETE CASCADE ON UPDATE NO ACTION
		)`,
		`CREATE TABLE project_write_lease (
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
		)`,
		`CREATE UNIQUE INDEX project_write_lease_active_owner_scope_idx
			ON project_write_lease(project_id, owner_kind, owner_id, scope_path) WHERE state = 'active'`,
		`INSERT INTO project (name, cwd, active) VALUES ('Legacy Scope', '/tmp/legacy-scope-project', 1)`,
		`INSERT INTO session (project_id, task_description, status, report, declared_write_scope)
			VALUES (1, 'Historical scoped task', 'completed', 'immutable report words', '` + legacyScopeFixtureValue + `')`,
		`INSERT INTO session (project_id, task_description, status, declared_write_scope)
			VALUES (1, 'Resumed pending task', 'pending', '["."]')`,
		`INSERT INTO planned_wave (project_id, title, definition_hash) VALUES (1, 'Legacy plan', 'definition-hash-1')`,
		`INSERT INTO planned_wave_task (planned_wave_id, project_id, task_key, position, task_description, declared_write_scope)
			VALUES (1, 1, 'task-1', 1, 'Legacy planned task', '` + legacyScopeFixtureValue + `')`,
		`INSERT INTO parallel_wave (project_id, base_sha, project_cwd, worktree_root)
			VALUES (1, 'base-sha-1', '/tmp/legacy-scope-project', '/tmp/legacy-scope-worktrees')`,
		`INSERT INTO parallel_wave_worker (wave_id, project_id, session_id, branch_name, worktree_path, base_sha, declared_write_scope)
			VALUES (1, 1, 3, 'fixer/wave-1/session-3', '/tmp/legacy-scope-worktrees/session-3', 'base-sha-1', '` + legacyScopeFixtureValue + `')`,
		`INSERT INTO parallel_wave_scope_lease (project_id, wave_id, scope_path, active) VALUES (1, 1, 'fixer_mcp', 1)`,
		`INSERT INTO project_write_fence (project_id, next_token) VALUES (1, 5)`,
		`INSERT INTO project_write_lease (
			id, project_id, lease_set_id, owner_kind, owner_id, scope_path, fencing_token, state,
			process_id, process_start_identity, binary_build_id, binary_epoch, created_at, heartbeat_at
		) VALUES (
			'lease-1', 1, 'set-1', 'hands_instruction', 'instr-1', 'fixer_mcp', 3, 'active',
			111, 'start-identity-1', 'build-1', 42, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
		)`,
		`INSERT INTO hands_instruction (
			id, project_id, actor_id, ordinal, source_channel_kind, source_channel_id, issuer_principal_id,
			instruction_text, declared_write_scope_json, instruction_envelope_json, requested_lane,
			risk_class, review_policy, state, compat_session_id, idempotency_key, revision,
			created_at, updated_at, terminal_at
		) VALUES (
			'instr-1', 1, 'actor-1', 1, 'telegram', 'channel-1', 'principal-1',
			'Historical scoped instruction', '["fixer_mcp"]', '` + legacyEnvelopeSentinel + `', 'codex',
			'repository_write', 'fixer_required', 'completed', 1, 'legacy-1', 3,
			CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
		)`,
		`INSERT INTO hands_instruction (
			id, project_id, actor_id, ordinal, source_channel_kind, source_channel_id, issuer_principal_id,
			instruction_text, declared_write_scope_json, instruction_envelope_json, requested_lane,
			risk_class, review_policy, state, compat_session_id, idempotency_key, revision,
			created_at, updated_at
		) VALUES (
			'instr-2', 1, 'actor-1', 2, 'telegram', 'channel-1', 'principal-1',
			'Resumed queued instruction', '["."]', '` + legacyEnvelopeSentinel + `', 'codex',
			'repository_write', 'fixer_required', 'queued', 3, 'legacy-2', 1,
			CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
		)`,
		`INSERT INTO hands_instruction_event (instruction_id, ordinal, event_type, from_state, to_state, actor_kind, actor_id, payload_json, created_at)
			VALUES ('instr-1', 1, 'instruction.queued', NULL, 'queued', 'principal', 'principal-1', '{"sentinel":"IMMUTABLE-EVENT-SENTINEL"}', CURRENT_TIMESTAMP)`,
		`INSERT INTO hands_instruction_event (instruction_id, ordinal, event_type, from_state, to_state, actor_kind, actor_id, payload_json, created_at)
			VALUES ('instr-1', 2, 'generation.reported', 'running', 'awaiting_review', 'hands', 'hands:instr-1', '{}', CURRENT_TIMESTAMP)`,
		`INSERT INTO hands_instruction_event (instruction_id, ordinal, event_type, from_state, to_state, actor_kind, actor_id, payload_json, created_at)
			VALUES ('instr-1', 3, 'instruction.reviewed', 'awaiting_review', 'completed', 'fixer', 'fixer-1', '{}', CURRENT_TIMESTAMP)`,
		`INSERT INTO hands_generation (
			instruction_id, generation, project_id, compat_session_id, provider, model, reasoning, status,
			lease_set_id, fencing_token, binary_build_id, binary_epoch
		) VALUES ('instr-1', 1, 1, 1, 'codex', 'gpt-5.6-luna', 'high', 'stopped', 'set-1', 7, 'build-1', 42)`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("seed legacy scope surface: %v: %v", statement, err)
		}
	}
}

func countRows(t *testing.T, query string, args ...any) int {
	t.Helper()
	var count int
	if err := db.QueryRow(query, args...).Scan(&count); err != nil {
		t.Fatalf("count rows for %q: %v", query, err)
	}
	return count
}

func TestInitDBFreshSchemaDeclaresNoWriteScopeSurface(t *testing.T) {
	originalDB := db
	defer func() { db = originalDB }()
	t.Setenv(fixerDBPathEnv, filepath.Join(t.TempDir(), "fresh-scope-free.db"))
	initDB()
	defer func() { _ = db.Close() }()

	for _, tableName := range []string{"parallel_wave_scope_lease", "project_write_lease", "project_write_fence"} {
		if dbTableExists(tableName) {
			t.Fatalf("fresh schema must not create retired table %s", tableName)
		}
	}
	for _, column := range []struct{ table, column string }{
		{"session", "declared_write_scope"},
		{"parallel_wave_worker", "declared_write_scope"},
		{"planned_wave_task", "declared_write_scope"},
		{"hands_instruction", "declared_write_scope_json"},
		{"hands_generation", "lease_set_id"},
		{"hands_generation", "fencing_token"},
	} {
		if dbTableHasColumn(column.table, column.column) {
			t.Fatalf("fresh schema must not create %s.%s", column.table, column.column)
		}
	}
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type = 'table'`)
	if err != nil {
		t.Fatalf("list fresh schema tables: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var bannedTerms = []string{"scope_lease", "write_lease", "write_fence"}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan fresh schema table: %v", err)
		}
		for _, banned := range bannedTerms {
			if strings.Contains(name, banned) {
				t.Fatalf("fresh schema must not create table %q (matched %q)", name, banned)
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate fresh schema tables: %v", err)
	}
}

func TestInitDBRetiresLegacyScopeDataIdempotentlyAndPreservesHistory(t *testing.T) {
	originalDB := db
	defer func() { db = originalDB }()
	t.Setenv(fixerDBPathEnv, filepath.Join(t.TempDir(), "legacy-scope-retirement.db"))
	initDB()
	seedLegacyWriteScopeSurface(t)

	// First retirement run.
	initDB()

	for _, column := range []struct{ table, column string }{
		{"session", "declared_write_scope"},
		{"parallel_wave_worker", "declared_write_scope"},
		{"planned_wave_task", "declared_write_scope"},
		{"hands_instruction", "declared_write_scope_json"},
		{"hands_generation", "lease_set_id"},
		{"hands_generation", "fencing_token"},
	} {
		if dbTableHasColumn(column.table, column.column) {
			t.Fatalf("retirement must drop %s.%s", column.table, column.column)
		}
	}
	for _, tableName := range []string{"parallel_wave_scope_lease", "project_write_lease", "project_write_fence"} {
		if dbTableExists(tableName) {
			t.Fatalf("retirement must drop %s", tableName)
		}
	}

	// Legacy values survive once as read-only migration history.
	archiveChecks := []struct {
		label string
		query string
	}{
		{"session scope", `SELECT COUNT(*) FROM retired_write_scope_archive WHERE source_table = 'session.declared_write_scope' AND legacy_value = '` + legacyScopeFixtureValue + `'`},
		{"planned task scope", `SELECT COUNT(*) FROM retired_write_scope_archive WHERE source_table = 'planned_wave_task.declared_write_scope' AND source_id = '1' AND legacy_value = '` + legacyScopeFixtureValue + `'`},
		{"wave worker scope", `SELECT COUNT(*) FROM retired_write_scope_archive WHERE source_table = 'parallel_wave_worker.declared_write_scope' AND source_id = '1' AND legacy_value = '` + legacyScopeFixtureValue + `'`},
		{"hands instruction scope", `SELECT COUNT(*) FROM retired_write_scope_archive WHERE source_table = 'hands_instruction.declared_write_scope_json' AND source_id = 'instr-1' AND legacy_value = '["fixer_mcp"]'`},
		{"generation lease columns", `SELECT COUNT(*) FROM retired_write_scope_archive WHERE source_table = 'hands_generation.lease_columns' AND source_id = 'instr-1:1' AND legacy_value LIKE '%set-1%' AND legacy_value LIKE '%7%'`},
		{"scope lease rows", `SELECT COUNT(*) FROM retired_write_scope_archive WHERE source_table = 'parallel_wave_scope_lease' AND legacy_value LIKE '%fixer_mcp%'`},
		{"write lease rows", `SELECT COUNT(*) FROM retired_write_scope_archive WHERE source_table = 'project_write_lease' AND source_id = 'lease-1' AND legacy_value LIKE '%fixer_mcp%' AND legacy_value LIKE '%set-1%'`},
		{"write fence rows", `SELECT COUNT(*) FROM retired_write_scope_archive WHERE source_table = 'project_write_fence' AND source_id = '1' AND legacy_value LIKE '%5%'`},
	}
	for _, check := range archiveChecks {
		if count := countRows(t, check.query); count != 1 {
			rows, err := db.Query(`SELECT source_table, source_id, legacy_value FROM retired_write_scope_archive`)
			dump := ""
			if err == nil {
				for rows.Next() {
					var sourceTable, sourceID, value string
					_ = rows.Scan(&sourceTable, &sourceID, &value)
					dump += fmt.Sprintf("[%s id=%s value=%s] ", sourceTable, sourceID, value)
				}
				_ = rows.Close()
			}
			t.Fatalf("expected exactly one archived %s row, got %d; archive=%q", check.label, count, dump)
		}
	}
	archiveTotal := countRows(t, `SELECT COUNT(*) FROM retired_write_scope_archive`)

	// Immutable history and ordinals survive untouched.
	var instructionText, envelope, state string
	var ordinal, revision int
	if err := db.QueryRow(`SELECT instruction_text, instruction_envelope_json, state, ordinal, revision FROM hands_instruction WHERE id = 'instr-1'`).
		Scan(&instructionText, &envelope, &state, &ordinal, &revision); err != nil {
		t.Fatalf("read preserved instruction: %v", err)
	}
	if instructionText != "Historical scoped instruction" || envelope != legacyEnvelopeSentinel ||
		state != "completed" || ordinal != 1 || revision != 3 {
		t.Fatalf("immutable instruction history was rewritten: text=%q envelope=%q state=%s ordinal=%d revision=%d",
			instructionText, envelope, state, ordinal, revision)
	}
	if eventOrdinals := countRows(t, `SELECT COUNT(*) FROM hands_instruction_event WHERE instruction_id = 'instr-1'`); eventOrdinals != 3 {
		t.Fatalf("instruction event history changed: events=%d", eventOrdinals)
	}
	if count := countRows(t, `SELECT COUNT(*) FROM hands_instruction_event WHERE instruction_id = 'instr-1' AND ordinal = 3 AND event_type = 'instruction.reviewed'`); count != 1 {
		t.Fatal("instruction event ordinals were not preserved")
	}
	if count := countRows(t, `SELECT COUNT(*) FROM session WHERE task_description = 'Historical scoped task' AND report = 'immutable report words'`); count != 1 {
		t.Fatal("historical session rows were rewritten")
	}
	if count := countRows(t, `SELECT COUNT(*) FROM session WHERE task_description = 'Resumed pending task' AND status = 'pending'`); count != 1 {
		t.Fatal("resumed pending session row was rewritten")
	}
	if count := countRows(t, `SELECT COUNT(*) FROM planned_wave_task WHERE id = 1 AND task_key = 'task-1' AND position = 1`); count != 1 {
		t.Fatal("planned task ordinals were rewritten")
	}
	if count := countRows(t, `SELECT COUNT(*) FROM parallel_wave_worker WHERE id = 1 AND branch_name = 'fixer/wave-1/session-3'`); count != 1 {
		t.Fatal("wave worker rows were rewritten")
	}

	// Foreign keys and CHECK constraints survive the hands_instruction rebuild.
	rows, err := db.Query(`PRAGMA foreign_key_list(hands_instruction)`)
	if err != nil {
		t.Fatalf("read hands_instruction foreign keys: %v", err)
	}
	foreignTables := map[string]bool{}
	for rows.Next() {
		var (
			id, seq     int
			table, from string
			to          *string
			onUpdate    string
			onDelete    string
			match       string
		)
		if err := rows.Scan(&id, &seq, &table, &from, &to, &onUpdate, &onDelete, &match); err != nil {
			_ = rows.Close()
			t.Fatalf("scan foreign key row: %v", err)
		}
		foreignTables[table] = true
	}
	_ = rows.Close()
	if !foreignTables["project_hands"] || !foreignTables["session"] {
		t.Fatalf("hands_instruction foreign keys were not preserved: %v", foreignTables)
	}
	if count := countRows(t, `SELECT COUNT(*) FROM pragma_foreign_key_check`); count != 0 {
		t.Fatalf("migration introduced %d foreign key violations", count)
	}
	if _, err := db.Exec(`INSERT INTO hands_instruction (
		id, project_id, actor_id, ordinal, source_channel_kind, source_channel_id, issuer_principal_id,
		instruction_text, instruction_envelope_json, requested_lane, risk_class, review_policy, state,
		idempotency_key, created_at, updated_at
	) VALUES ('bad-ordinal', 1, 'actor-1', 0, 'telegram', 'channel-1', 'principal-1',
		'bad', '{}', 'codex', 'repository_write', 'fixer_required', 'queued', 'bad-1',
		CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`); err == nil {
		t.Fatal("ordinal CHECK constraint was lost in the rebuild")
	}
	if _, err := db.Exec(`INSERT INTO hands_instruction (
		id, project_id, actor_id, ordinal, source_channel_kind, source_channel_id, issuer_principal_id,
		instruction_text, instruction_envelope_json, requested_lane, risk_class, review_policy, state,
		idempotency_key, created_at, updated_at
	) VALUES ('bad-envelope', 1, 'actor-1', 9, 'telegram', 'channel-1', 'principal-1',
		'bad', 'not-json', 'codex', 'repository_write', 'fixer_required', 'queued', 'bad-2',
		CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`); err == nil {
		t.Fatal("envelope json_valid CHECK constraint was lost in the rebuild")
	}
	if _, err := db.Exec(`INSERT INTO hands_instruction (
		id, project_id, actor_id, ordinal, source_channel_kind, source_channel_id, issuer_principal_id,
		instruction_text, instruction_envelope_json, requested_lane, risk_class, review_policy, state,
		idempotency_key, created_at, updated_at
	) VALUES ('bad-state', 1, 'actor-1', 10, 'telegram', 'channel-1', 'principal-1',
		'bad', '{}', 'codex', 'repository_write', 'fixer_required', 'quantum', 'bad-3',
		CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`); err == nil {
		t.Fatal("state CHECK constraint was lost in the rebuild")
	}

	// Resumed tasks, plans and Hands work read and launch without any scope input.
	originalRole, originalProject, originalSession := authorizedRole, authorizedProjectId, authorizedSessionId
	defer func() {
		authorizedRole, authorizedProjectId, authorizedSessionId = originalRole, originalProject, originalSession
	}()
	authorizedRole, authorizedProjectId, authorizedSessionId = "fixer", 1, 0
	ctx := context.Background()

	if _, pending, err := GetPendingTasks(ctx, nil, GetPendingTasksInput{}); err != nil {
		t.Fatalf("read resumed tasks without scope: %v", err)
	} else {
		found := false
		for _, task := range pending.Tasks {
			if task.TaskDescription == "Resumed pending task" {
				found = true
			}
		}
		if !found {
			t.Fatalf("resumed pending task missing from %d results", len(pending.Tasks))
		}
	}
	if _, plan, err := GetPlannedNetrunnerWave(ctx, nil, GetPlannedNetrunnerWaveInput{PlanId: 1}); err != nil {
		t.Fatalf("read resumed plan without scope: %v", err)
	} else if len(plan.Plan.Tasks) != 1 || plan.Plan.Tasks[0].Key != "task-1" {
		t.Fatalf("resumed plan tasks were not preserved: %+v", plan.Plan)
	}
	if _, listed, err := ListHandsInstructions(ctx, nil, ListHandsInstructionsInput{}); err != nil {
		t.Fatalf("read Hands mailbox without scope: %v", err)
	} else if len(listed.Instructions) != 2 {
		t.Fatalf("expected 2 preserved Hands instructions, got %d", len(listed.Instructions))
	}
	if _, handsState, err := GetHandsState(ctx, nil, GetHandsStateInput{}); err != nil {
		t.Fatalf("read Hands state without scope: %v", err)
	} else if handsState.QueuedCount != 1 {
		t.Fatalf("expected one queued legacy instruction, got %+v", handsState)
	}
	if _, receipt, err := SubmitHandsInstruction(ctx, nil, SubmitHandsInstructionInput{
		InstructionText: "Post-retirement launch.", RequestedLane: "codex", IdempotencyKey: "post-retirement-1",
	}); err != nil {
		t.Fatalf("launch Hands instruction without scope: %v", err)
	} else if receipt.State != "queued" || receipt.RiskClass != "repository_write" || receipt.Ordinal != 3 {
		t.Fatalf("unexpected post-retirement launch receipt: %+v", receipt)
	}

	// Second retirement run is a no-op: no duplicate archives, no schema drift.
	initDB()
	if secondTotal := countRows(t, `SELECT COUNT(*) FROM retired_write_scope_archive`); secondTotal != archiveTotal {
		t.Fatalf("repeat migration duplicated archived history: first=%d second=%d", archiveTotal, secondTotal)
	}
	if dbTableHasColumn("hands_instruction", "declared_write_scope_json") || dbTableExists("project_write_lease") {
		t.Fatal("repeat migration resurrected the retired scope surface")
	}
	if count := countRows(t, `SELECT COUNT(*) FROM hands_instruction`); count != 3 {
		t.Fatalf("repeat migration changed instruction rows: %d", count)
	}
	var repeatedText, repeatedEnvelope string
	if err := db.QueryRow(`SELECT instruction_text, instruction_envelope_json FROM hands_instruction WHERE id = 'instr-1'`).
		Scan(&repeatedText, &repeatedEnvelope); err != nil {
		t.Fatalf("read instruction after repeat migration: %v", err)
	}
	if repeatedText != "Historical scoped instruction" || repeatedEnvelope != legacyEnvelopeSentinel {
		t.Fatalf("repeat migration rewrote immutable history: text=%q envelope=%q", repeatedText, repeatedEnvelope)
	}
}

// legacyHandsInstructionDDL models hands_instruction exactly as production had
// it before the scope retirement: the retired scope column plus the historical
// state enum that still accepts the retired waiting state.
const legacyHandsInstructionDDL = `CREATE TABLE hands_instruction (
	id TEXT PRIMARY KEY,
	project_id INTEGER NOT NULL,
	actor_id TEXT NOT NULL,
	ordinal INTEGER NOT NULL CHECK(ordinal > 0),
	source_channel_kind TEXT NOT NULL,
	source_channel_id TEXT NOT NULL,
	source_message_id TEXT NOT NULL DEFAULT '',
	issuer_principal_id TEXT NOT NULL,
	instruction_text TEXT NOT NULL CHECK(length(instruction_text) <= 65536),
	declared_write_scope_json TEXT NOT NULL DEFAULT '[]' CHECK(json_valid(declared_write_scope_json)),
	instruction_envelope_json TEXT NOT NULL CHECK(length(instruction_envelope_json) <= 131072 AND json_valid(instruction_envelope_json)),
	requested_lane TEXT NOT NULL CHECK(requested_lane IN ('codex', 'commandcode', 'claude', 'kimi-code', 'antigravity', 'grok')),
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

func seedLegacyHandsInstructionTable(t *testing.T) {
	t.Helper()
	for _, statement := range []string{
		`DROP TABLE hands_instruction`,
		legacyHandsInstructionDDL,
		`CREATE INDEX hands_instruction_project_state_idx ON hands_instruction(project_id, state, ordinal)`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("seed legacy hands_instruction schema: %v: %v", statement, err)
		}
	}
}

func TestInitDBRetiresPartialGenerationLeaseColumnSchemas(t *testing.T) {
	cases := []struct {
		name        string
		columns     map[string]string
		insertSQL   string
		wantRows    int
		wantContain []string
		wantAbsent  []string
	}{
		{
			name:    "both lease columns",
			columns: map[string]string{"lease_set_id": "TEXT", "fencing_token": "INTEGER"},
			insertSQL: `INSERT INTO hands_generation (instruction_id, generation, project_id, provider, model, reasoning, status, lease_set_id, fencing_token)
				VALUES ('instr-1', 1, 1, 'codex', 'model', 'high', 'stopped', 'set-1', 7)`,
			wantRows:    1,
			wantContain: []string{"lease_set_id", "fencing_token"},
		},
		{
			name:    "only lease_set_id",
			columns: map[string]string{"lease_set_id": "TEXT"},
			insertSQL: `INSERT INTO hands_generation (instruction_id, generation, project_id, provider, model, reasoning, status, lease_set_id)
				VALUES ('instr-1', 1, 1, 'codex', 'model', 'high', 'stopped', 'set-only')`,
			wantRows:    1,
			wantContain: []string{"lease_set_id"},
			wantAbsent:  []string{"fencing_token"},
		},
		{
			name:    "only fencing_token",
			columns: map[string]string{"fencing_token": "INTEGER"},
			insertSQL: `INSERT INTO hands_generation (instruction_id, generation, project_id, provider, model, reasoning, status, fencing_token)
				VALUES ('instr-1', 1, 1, 'codex', 'model', 'high', 'stopped', 9)`,
			wantRows:    1,
			wantContain: []string{"fencing_token"},
			wantAbsent:  []string{"lease_set_id"},
		},
		{
			name:      "neither lease column",
			columns:   map[string]string{},
			insertSQL: `INSERT INTO hands_generation (instruction_id, generation, project_id, provider, model, reasoning, status) VALUES ('instr-1', 1, 1, 'codex', 'model', 'high', 'stopped')`,
			wantRows:  0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			originalDB := db
			defer func() { db = originalDB }()
			t.Setenv(fixerDBPathEnv, filepath.Join(t.TempDir(), "partial-generation-lease.db"))
			initDB()
			defer func() { _ = db.Close() }()
			for column, decl := range tc.columns {
				if _, err := db.Exec(`ALTER TABLE hands_generation ADD COLUMN ` + column + ` ` + decl); err != nil {
					t.Fatalf("add legacy column %s: %v", column, err)
				}
			}
			for _, statement := range []string{
				`INSERT INTO project (name, cwd, active) VALUES ('Partial lease', '/tmp/partial-lease', 1)`,
				`INSERT INTO hands_instruction (
					id, project_id, actor_id, ordinal, source_channel_kind, source_channel_id, issuer_principal_id,
					instruction_text, instruction_envelope_json, requested_lane, risk_class, review_policy, state,
					idempotency_key, created_at, updated_at
				) VALUES ('instr-1', 1, 'actor-1', 1, 'telegram', 'channel-1', 'principal-1',
					'Generation parent', '{}', 'codex', 'repository_write', 'fixer_required', 'queued', 'partial-1',
					CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
				tc.insertSQL,
			} {
				if _, err := db.Exec(statement); err != nil {
					t.Fatalf("seed generation fixture: %v: %v", statement, err)
				}
			}
			initDB()
			for column := range tc.columns {
				if dbTableHasColumn("hands_generation", column) {
					t.Fatalf("retirement must drop hands_generation.%s", column)
				}
			}
			archived := countRows(t, `SELECT COUNT(*) FROM retired_write_scope_archive WHERE source_table = 'hands_generation.lease_columns'`)
			if archived != tc.wantRows {
				t.Fatalf("archived generation rows = %d, want %d", archived, tc.wantRows)
			}
			if tc.wantRows == 1 {
				var value string
				if err := db.QueryRow(`SELECT legacy_value FROM retired_write_scope_archive WHERE source_table = 'hands_generation.lease_columns'`).Scan(&value); err != nil {
					t.Fatalf("read archived lease columns: %v", err)
				}
				for _, want := range tc.wantContain {
					if !strings.Contains(value, want) {
						t.Fatalf("archived value %q must contain %q", value, want)
					}
				}
				for _, absent := range tc.wantAbsent {
					if strings.Contains(value, absent) {
						t.Fatalf("archived value %q must not reference missing column %q", value, absent)
					}
				}
			}
			initDB()
			if again := countRows(t, `SELECT COUNT(*) FROM retired_write_scope_archive WHERE source_table = 'hands_generation.lease_columns'`); again != tc.wantRows {
				t.Fatalf("repeat migration changed archived rows: first=%d second=%d", tc.wantRows, again)
			}
		})
	}
}

func TestHandsInstructionRetirementPreservesNamedIndexesAndTriggers(t *testing.T) {
	originalDB := db
	defer func() { db = originalDB }()
	t.Setenv(fixerDBPathEnv, filepath.Join(t.TempDir(), "hands-named-ddl.db"))
	initDB()
	defer func() { _ = db.Close() }()
	for _, statement := range []string{
		`ALTER TABLE hands_instruction ADD COLUMN declared_write_scope_json TEXT NOT NULL DEFAULT '[]'`,
		`CREATE INDEX hands_instruction_issuer_idx ON hands_instruction(issuer_principal_id)`,
		`CREATE TRIGGER hands_instruction_no_delete BEFORE DELETE ON hands_instruction BEGIN SELECT RAISE(ABORT, 'no-delete'); END`,
		`INSERT INTO hands_instruction (
			id, project_id, actor_id, ordinal, source_channel_kind, source_channel_id, issuer_principal_id,
			instruction_text, instruction_envelope_json, requested_lane, risk_class, review_policy, state,
			idempotency_key, created_at, updated_at
		) VALUES ('instr-named', 1, 'actor-1', 1, 'telegram', 'channel-1', 'principal-1',
			'Named schema subject', '{}', 'codex', 'repository_write', 'fixer_required', 'queued', 'named-1',
			CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("seed named schema: %v: %v", statement, err)
		}
	}
	initDB()
	for _, name := range []string{"hands_instruction_issuer_idx", "hands_instruction_no_delete", "hands_instruction_project_state_idx"} {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name = ?`, name).Scan(&count); err != nil {
			t.Fatalf("inspect %s: %v", name, err)
		}
		if count != 1 {
			t.Fatalf("hands rebuild must preserve %s, found %d", name, count)
		}
	}
	if _, err := db.Exec(`DELETE FROM hands_instruction`); err == nil || !strings.Contains(err.Error(), "no-delete") {
		t.Fatalf("preserved trigger must still guard deletes, got %v", err)
	}
}

func TestHandsInstructionRetirementToleratesHistoricalFKViolations(t *testing.T) {
	originalDB := db
	defer func() { db = originalDB }()
	t.Setenv(fixerDBPathEnv, filepath.Join(t.TempDir(), "hands-historical-fk.db"))
	initDB()
	defer func() { _ = db.Close() }()
	seedLegacyHandsInstructionTable(t)
	for _, statement := range []string{
		`INSERT INTO project (name, cwd, active) VALUES ('Historical FK', '/tmp/historical-fk', 1)`,
		`PRAGMA foreign_keys = OFF`,
		`INSERT INTO hands_instruction (
			id, project_id, actor_id, ordinal, source_channel_kind, source_channel_id, issuer_principal_id,
			instruction_text, declared_write_scope_json, instruction_envelope_json, requested_lane,
			risk_class, review_policy, state, compat_session_id, idempotency_key, revision,
			created_at, updated_at
		) VALUES (
			'instr-orphan', 1, 'actor-1', 1, 'telegram', 'channel-1', 'principal-1',
			'Historical orphan instruction', '["fixer_mcp"]', '` + legacyEnvelopeSentinel + `', 'codex',
			'repository_write', 'fixer_required', 'waiting_for_lease', 9999, 'legacy-orphan', 1,
			CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
		)`,
		`PRAGMA foreign_keys = ON`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("seed historical FK fixture: %v: %v", statement, err)
		}
	}
	// The migration must succeed despite the historical violation and must
	// preserve the row verbatim, including its retired legacy state.
	initDB()
	if dbTableHasColumn("hands_instruction", "declared_write_scope_json") {
		t.Fatal("retirement must drop hands_instruction.declared_write_scope_json")
	}
	if violations := countRows(t, `SELECT COUNT(*) FROM pragma_foreign_key_check`); violations != 1 {
		t.Fatalf("historical violation must survive untouched (not fixed, not grown), got %d", violations)
	}
	var state string
	if err := db.QueryRow(`SELECT state FROM hands_instruction WHERE id = 'instr-orphan'`).Scan(&state); err != nil {
		t.Fatalf("read historical instruction: %v", err)
	}
	if state != "waiting_for_lease" {
		t.Fatalf("historical waiting state must be preserved verbatim, got %q", state)
	}
}

func TestHandsInstructionRetirementRestoresForeignKeysPragma(t *testing.T) {
	originalDB := db
	defer func() { db = originalDB }()
	t.Setenv(fixerDBPathEnv, filepath.Join(t.TempDir(), "hands-pragma-restore.db"))
	initDB()
	defer func() { _ = db.Close() }()
	seedLegacyHandsInstructionTable(t)
	if _, err := db.Exec(`INSERT INTO hands_instruction (
		id, project_id, actor_id, ordinal, source_channel_kind, source_channel_id, issuer_principal_id,
		instruction_text, declared_write_scope_json, instruction_envelope_json, requested_lane,
		risk_class, review_policy, state, idempotency_key, revision, created_at, updated_at
	) VALUES ('instr-pragma', 1, 'actor-1', 1, 'telegram', 'channel-1', 'principal-1',
		'Pragma subject', '["fixer_mcp"]', '{}', 'codex',
		'repository_write', 'fixer_required', 'queued', 'pragma-1', 1,
		CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`); err != nil {
		t.Fatalf("seed pragma fixture: %v", err)
	}
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("acquire connection: %v", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys = ON`); err != nil {
		t.Fatalf("enable FK enforcement on test connection: %v", err)
	}
	if err := retireHandsInstructionScopeOnConn(ctx, conn); err != nil {
		t.Fatalf("retire hands scope: %v", err)
	}
	var fk int
	if err := conn.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&fk); err != nil {
		t.Fatalf("read PRAGMA foreign_keys after success: %v", err)
	}
	if fk != 1 {
		t.Fatalf("PRAGMA foreign_keys must be restored to 1 on success, got %d", fk)
	}
}

func TestHandsInstructionRetirementRestoresForeignKeysPragmaOnError(t *testing.T) {
	originalDB := db
	defer func() { db = originalDB }()
	t.Setenv(fixerDBPathEnv, filepath.Join(t.TempDir(), "hands-pragma-restore-error.db"))
	initDB()
	defer func() { _ = db.Close() }()
	seedLegacyHandsInstructionTable(t)
	// A constrained archive table forces the archive step to fail, exercising
	// the error path of the rebuild.
	if _, err := db.Exec(`DROP TABLE retired_write_scope_archive`); err != nil {
		t.Fatalf("drop default archive table: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE retired_write_scope_archive (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		source_table TEXT NOT NULL,
		source_id TEXT NOT NULL,
		legacy_value TEXT NOT NULL CHECK(legacy_value <> '["boom"]'),
		archived_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		t.Fatalf("create constrained archive table: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO hands_instruction (
		id, project_id, actor_id, ordinal, source_channel_kind, source_channel_id, issuer_principal_id,
		instruction_text, declared_write_scope_json, instruction_envelope_json, requested_lane,
		risk_class, review_policy, state, idempotency_key, revision, created_at, updated_at
	) VALUES ('instr-boom', 1, 'actor-1', 1, 'telegram', 'channel-1', 'principal-1',
		'Failing archive subject', '["boom"]', '{}', 'codex',
		'repository_write', 'fixer_required', 'queued', 'boom-1', 1,
		CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`); err != nil {
		t.Fatalf("seed failing archive fixture: %v", err)
	}
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("acquire connection: %v", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys = ON`); err != nil {
		t.Fatalf("enable FK enforcement on test connection: %v", err)
	}
	err = retireHandsInstructionScopeOnConn(ctx, conn)
	if err == nil {
		t.Fatal("archive failure must surface as a migration error")
	}
	if !strings.Contains(err.Error(), "archive hands_instruction.declared_write_scope_json") {
		t.Fatalf("expected the archive failure to surface, got %v", err)
	}
	var fk int
	if err := conn.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&fk); err != nil {
		t.Fatalf("read PRAGMA foreign_keys after error: %v", err)
	}
	if fk != 1 {
		t.Fatalf("PRAGMA foreign_keys must be restored to 1 even on error, got %d", fk)
	}
	var columnCount int
	if err := conn.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pragma_table_info('hands_instruction') WHERE name = 'declared_write_scope_json'`,
	).Scan(&columnCount); err != nil {
		t.Fatalf("inspect hands_instruction after failed retirement: %v", err)
	}
	if columnCount != 1 {
		t.Fatal("failed retirement must roll back and leave the legacy column in place")
	}
}

func TestRetiredWaitingStateIsHistoricalEnumCompatOnly(t *testing.T) {
	originalDB := db
	defer func() { db = originalDB }()
	t.Setenv(fixerDBPathEnv, filepath.Join(t.TempDir(), "waiting-enum-compat.db"))
	initDB()
	defer func() { _ = db.Close() }()
	freshInsert := `INSERT INTO hands_instruction (
		id, project_id, actor_id, ordinal, source_channel_kind, source_channel_id, issuer_principal_id,
		instruction_text, instruction_envelope_json, requested_lane, risk_class, review_policy, state,
		idempotency_key, created_at, updated_at
	) VALUES (?, 1, 'actor-1', ?, 'telegram', 'channel-1', 'principal-1',
		'Waiting state subject', '{}', 'codex', 'repository_write', 'fixer_required', 'waiting_for_lease', ?,
		CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`
	if _, err := db.Exec(freshInsert, "fresh-waiting", 1, "fresh-waiting-1"); err == nil {
		t.Fatal("fresh schema must reject the retired waiting state entirely")
	}
	seedLegacyHandsInstructionTable(t)
	for _, statement := range []string{
		`INSERT INTO project (name, cwd, active) VALUES ('Waiting enum', '/tmp/waiting-enum', 1)`,
		`INSERT INTO hands_instruction (
			id, project_id, actor_id, ordinal, source_channel_kind, source_channel_id, issuer_principal_id,
			instruction_text, declared_write_scope_json, instruction_envelope_json, requested_lane,
			risk_class, review_policy, state, state_reason_code, idempotency_key, revision, created_at, updated_at
		) VALUES (
			'instr-waiting', 1, 'actor-1', 1, 'telegram', 'channel-1', 'principal-1',
			'Historical waiting instruction', '["."]', '` + legacyEnvelopeSentinel + `', 'codex',
			'repository_write', 'fixer_required', 'waiting_for_lease', 'write_lease_unavailable', 'waiting-1', 1,
			CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
		)`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("seed waiting enum fixture: %v: %v", statement, err)
		}
	}
	initDB()
	var state, reasonCode string
	if err := db.QueryRow(`SELECT state, COALESCE(state_reason_code, '') FROM hands_instruction WHERE id = 'instr-waiting'`).Scan(&state, &reasonCode); err != nil {
		t.Fatalf("read historical waiting instruction: %v", err)
	}
	if state != "waiting_for_lease" || reasonCode != "write_lease_unavailable" {
		t.Fatalf("historical waiting payload must be preserved verbatim: state=%q reason=%q", state, reasonCode)
	}
	// The migration schema keeps the historical enum so legacy rows stay
	// readable and restartable; nothing in the runtime writes it anymore.
	if _, err := db.Exec(freshInsert, "migrated-waiting", 2, "migrated-waiting-1"); err != nil {
		t.Fatalf("migration schema must keep the historical enum: %v", err)
	}
	// The active derived display never presents a lease wait: the legacy row
	// reads as plain queued work.
	originalRole, originalProject, originalSession := authorizedRole, authorizedProjectId, authorizedSessionId
	defer func() {
		authorizedRole, authorizedProjectId, authorizedSessionId = originalRole, originalProject, originalSession
	}()
	authorizedRole, authorizedProjectId, authorizedSessionId = "fixer", 1, 0
	if _, handsState, err := GetHandsState(context.Background(), nil, GetHandsStateInput{}); err != nil {
		t.Fatalf("read hands state: %v", err)
	} else if handsState.DerivedState != "queued" {
		t.Fatalf("legacy waiting rows must read as queued work, got %q", handsState.DerivedState)
	}
}

func TestFKViolationRegressionsRejectOnlyNewViolations(t *testing.T) {
	baseline := map[string]int{"hands_instruction->session#1": 1}
	if err := fkViolationRegressions(baseline, map[string]int{"hands_instruction->session#1": 1}); err != nil {
		t.Fatalf("historical violations must be tolerated: %v", err)
	}
	if err := fkViolationRegressions(baseline, map[string]int{}); err != nil {
		t.Fatalf("resolved violations must be tolerated: %v", err)
	}
	if err := fkViolationRegressions(baseline, map[string]int{"hands_instruction->session#1": 2}); err == nil {
		t.Fatal("a grown violation count must be rejected")
	}
	if err := fkViolationRegressions(baseline, map[string]int{"hands_instruction->session#2": 1}); err == nil {
		t.Fatal("a new violation key must be rejected")
	}
}
