package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// Declared write scopes and their scope leases are fully retired. Legacy
// values are preserved once as read-only migration history before the active
// columns and lease tables are removed; nothing reads the archive at runtime
// and it recreates no scope mechanism.
//
// Restart safety: every step below is one transaction that archives and
// removes together, and every step is conditional on the legacy column or
// table still existing. An interrupted run therefore leaves a prefix of steps
// completed; re-running the migration skips those steps and finishes the rest,
// and any failure is returned to the caller as a migration error (fail closed,
// never silently ignored). The retired waiting state survives only in these
// rebuild schemas as dated historical enum compatibility for legacy rows.

const retiredWriteScopeArchiveTable = "retired_write_scope_archive"

// migrateDeclaredWriteScopeRetirement removes the retired scope data from a
// database created before the retirement. It is idempotent: every step is
// conditional on the legacy column or table still existing, so running it
// twice is a no-op, and every archive step commits together with the removal
// step so an interrupted run never loses or duplicates archived values.
// Failures are returned to the caller and must surface as migration errors.
func migrateDeclaredWriteScopeRetirement() error {
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS ` + retiredWriteScopeArchiveTable + ` (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			source_table TEXT NOT NULL,
			source_id TEXT NOT NULL,
			legacy_value TEXT NOT NULL,
			archived_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
	`); err != nil {
		return fmt.Errorf("create %s: %w", retiredWriteScopeArchiveTable, err)
	}

	type scopeColumn struct {
		table    string
		column   string
		sourceID string
	}
	for _, source := range []scopeColumn{
		{table: "session", column: "declared_write_scope", sourceID: "CAST(id AS TEXT)"},
		{table: "parallel_wave_worker", column: "declared_write_scope", sourceID: "CAST(id AS TEXT)"},
		{table: "planned_wave_task", column: "declared_write_scope", sourceID: "CAST(id AS TEXT)"},
	} {
		if err := archiveAndDropScopeColumn(source.table, source.column, source.sourceID); err != nil {
			return err
		}
	}

	// Generations carry lease bookkeeping in two columns; archive both
	// together so each row is archived once.
	if err := archiveAndDropHandsGenerationLeaseColumns("hands_generation"); err != nil {
		return err
	}

	if err := archiveAndDropHandsInstructionScopeColumn(); err != nil {
		return err
	}

	for _, table := range []string{"parallel_wave_scope_lease", "project_write_lease", "project_write_fence"} {
		if err := archiveAndDropScopeLeaseTable(table); err != nil {
			return err
		}
	}
	return nil
}

func archiveAndDropScopeColumn(tableName, columnName, sourceIDSQL string) error {
	if !dbTableHasColumn(tableName, columnName) {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin %s.%s scope retirement: %w", tableName, columnName, err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(
		`INSERT INTO `+retiredWriteScopeArchiveTable+` (source_table, source_id, legacy_value)
		 SELECT ?, `+sourceIDSQL+`, COALESCE(`+columnName+`, '')
		 FROM `+tableName,
		tableName+"."+columnName,
	); err != nil {
		return fmt.Errorf("archive %s.%s: %w", tableName, columnName, err)
	}
	if _, err := tx.Exec(`ALTER TABLE ` + tableName + ` DROP COLUMN ` + columnName); err != nil {
		return fmt.Errorf("drop %s.%s: %w", tableName, columnName, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit %s.%s scope retirement: %w", tableName, columnName, err)
	}
	return nil
}

// archiveAndDropHandsGenerationLeaseColumns retires the lease bookkeeping
// columns kept on hands_generation by the old write-lease mechanism. Legacy
// schemas differ: a database may carry only lease_set_id, only fencing_token,
// both, or neither. Whichever columns exist are archived together as one JSON
// value per generation (each row archived exactly once) and dropped in the
// same transaction; missing columns are never referenced.
func archiveAndDropHandsGenerationLeaseColumns(tableName string) error {
	hasLeaseSetID := dbTableHasColumn(tableName, "lease_set_id")
	hasFencingToken := dbTableHasColumn(tableName, "fencing_token")
	if !hasLeaseSetID && !hasFencingToken {
		return nil
	}
	valueParts := make([]string, 0, 2)
	conditionParts := make([]string, 0, 2)
	if hasLeaseSetID {
		valueParts = append(valueParts, "'lease_set_id', COALESCE(lease_set_id, '')")
		conditionParts = append(conditionParts, "COALESCE(lease_set_id, '') != ''")
	}
	if hasFencingToken {
		valueParts = append(valueParts, "'fencing_token', COALESCE(fencing_token, 0)")
		conditionParts = append(conditionParts, "COALESCE(fencing_token, 0) != 0")
	}
	dropColumns := make([]string, 0, 2)
	if hasLeaseSetID {
		dropColumns = append(dropColumns, "lease_set_id")
	}
	if hasFencingToken {
		dropColumns = append(dropColumns, "fencing_token")
	}
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin %s lease retirement: %w", tableName, err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(
		`INSERT INTO `+retiredWriteScopeArchiveTable+` (source_table, source_id, legacy_value)
		 SELECT ?, instruction_id || ':' || CAST(generation AS TEXT),
		        json_object(`+strings.Join(valueParts, ", ")+`)
		 FROM `+tableName+`
		 WHERE `+strings.Join(conditionParts, " OR "),
		tableName+".lease_columns",
	); err != nil {
		return fmt.Errorf("archive %s lease columns: %w", tableName, err)
	}
	for _, column := range dropColumns {
		if _, err := tx.Exec(`ALTER TABLE ` + tableName + ` DROP COLUMN ` + column); err != nil {
			return fmt.Errorf("drop %s.%s: %w", tableName, column, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit %s lease retirement: %w", tableName, err)
	}
	return nil
}

// archiveAndDropHandsInstructionScopeColumn removes the retired
// declared_write_scope_json column from hands_instruction. SQLite cannot DROP
// a column that carries a CHECK constraint, so the table is rebuilt once with
// the current schema. Immutable instruction envelopes, events, ordinals and
// state values are copied through untouched, and every named index or trigger
// on the old table is recreated under its original name.
func archiveAndDropHandsInstructionScopeColumn() error {
	conn, err := db.Conn(context.Background())
	if err != nil {
		return fmt.Errorf("acquire hands_instruction retirement connection: %w", err)
	}
	defer conn.Close()
	return retireHandsInstructionScopeOnConn(context.Background(), conn)
}

func retireHandsInstructionScopeOnConn(ctx context.Context, conn *sql.Conn) (retErr error) {
	tableName := "hands_instruction"
	var columnCount int
	if err := conn.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, tableName, "declared_write_scope_json",
	).Scan(&columnCount); err != nil {
		return fmt.Errorf("inspect %s.declared_write_scope_json: %w", tableName, err)
	}
	if columnCount == 0 {
		return nil
	}
	var originalFK int
	if err := conn.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&originalFK); err != nil {
		return fmt.Errorf("read PRAGMA foreign_keys before hands_instruction retirement: %w", err)
	}
	namedDDL, err := collectNamedTableDDL(ctx, conn, tableName)
	if err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		return fmt.Errorf("disable FK enforcement for hands_instruction retirement: %w", err)
	}
	defer func() {
		restore := `PRAGMA foreign_keys = OFF`
		if originalFK != 0 {
			restore = `PRAGMA foreign_keys = ON`
		}
		if _, restoreErr := conn.ExecContext(ctx, restore); restoreErr != nil {
			retErr = errors.Join(retErr, fmt.Errorf("restore PRAGMA foreign_keys to %d after hands_instruction retirement: %w", originalFK, restoreErr))
		}
	}()

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin hands_instruction retirement: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	// Foreign-key baseline taken inside the transaction, before the rebuild.
	// Historical violations that predate this migration are tolerated; the
	// rebuild must not introduce any new one (checked again before commit).
	baselineViolations, err := foreignKeyViolationCounts(ctx, tx)
	if err != nil {
		return fmt.Errorf("hands_instruction FK baseline: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO `+retiredWriteScopeArchiveTable+` (source_table, source_id, legacy_value)
		 SELECT 'hands_instruction.declared_write_scope_json', id, COALESCE(declared_write_scope_json, '')
		 FROM hands_instruction`,
	); err != nil {
		return fmt.Errorf("archive hands_instruction.declared_write_scope_json: %w", err)
	}
	statements := []string{
		`CREATE TABLE hands_instruction_retire_mig (
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
		)`,
		// The state CHECK above keeps the retired waiting state only as dated
		// historical enum compatibility: legacy rows are copied verbatim and
		// may still carry that state; nothing writes it anymore.
		`INSERT INTO hands_instruction_retire_mig SELECT id, project_id, actor_id, ordinal, source_channel_kind, source_channel_id, source_message_id, issuer_principal_id, instruction_text, instruction_envelope_json, requested_lane, risk_class, review_policy, state, state_reason_code, state_reason_text, compat_session_id, idempotency_key, revision, created_at, updated_at, terminal_at FROM hands_instruction;`,
		`DROP TABLE hands_instruction;`,
		`ALTER TABLE hands_instruction_retire_mig RENAME TO hands_instruction;`,
		`CREATE INDEX IF NOT EXISTS hands_instruction_project_state_idx ON hands_instruction(project_id, state, ordinal);`,
	}
	statements = append(statements, namedDDL...)
	for _, statement := range statements {
		if _, err := tx.ExecContext(context.Background(), statement); err != nil {
			return fmt.Errorf("rebuild hands_instruction without declared_write_scope_json: %w", err)
		}
	}
	afterViolations, err := foreignKeyViolationCounts(ctx, tx)
	if err != nil {
		return fmt.Errorf("hands_instruction FK comparison: %w", err)
	}
	if err := fkViolationRegressions(baselineViolations, afterViolations); err != nil {
		return fmt.Errorf("hands_instruction rebuild: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit hands_instruction retirement: %w", err)
	}
	return nil
}

// fkViolationRegressions reports only foreign key violations that appear
// after the rebuild with a higher count than in the pre-migration baseline.
// Historical violations that predate the migration are tolerated because they
// were not caused by it.
func fkViolationRegressions(baseline, after map[string]int) error {
	for key, afterCount := range after {
		if afterCount > baseline[key] {
			return fmt.Errorf("introduced a foreign key violation for %s (baseline %d, after %d)", key, baseline[key], afterCount)
		}
	}
	return nil
}

// collectNamedTableDDL returns idempotent recreation statements for every
// named index and trigger attached to the table. sqlite_autoindex entries
// (builtin indexes backing UNIQUE/PRIMARY KEY constraints) have no SQL and
// are rebuilt by the table definition itself, so they are intentionally
// skipped.
func collectNamedTableDDL(ctx context.Context, conn *sql.Conn, tableName string) ([]string, error) {
	rows, err := conn.QueryContext(ctx,
		`SELECT type, name, sql FROM sqlite_master
		 WHERE tbl_name = ? AND type IN ('index', 'trigger') AND sql IS NOT NULL
		 ORDER BY type, name`, tableName)
	if err != nil {
		return nil, fmt.Errorf("read named indexes/triggers for %s: %w", tableName, err)
	}
	defer rows.Close()
	var statements []string
	for rows.Next() {
		var kind, name, sqlText string
		if err := rows.Scan(&kind, &name, &sqlText); err != nil {
			return nil, fmt.Errorf("scan named %s on %s: %w", kind, tableName, err)
		}
		switch {
		case strings.HasPrefix(sqlText, "CREATE UNIQUE INDEX "):
			sqlText = strings.Replace(sqlText, "CREATE UNIQUE INDEX ", "CREATE UNIQUE INDEX IF NOT EXISTS ", 1)
		case strings.HasPrefix(sqlText, "CREATE INDEX "):
			sqlText = strings.Replace(sqlText, "CREATE INDEX ", "CREATE INDEX IF NOT EXISTS ", 1)
		case strings.HasPrefix(sqlText, "CREATE TRIGGER "):
			sqlText = strings.Replace(sqlText, "CREATE TRIGGER ", "CREATE TRIGGER IF NOT EXISTS ", 1)
		default:
			return nil, fmt.Errorf("unsupported named %s %q on %s: %q", kind, name, tableName, sqlText)
		}
		statements = append(statements, sqlText)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate named indexes/triggers for %s: %w", tableName, err)
	}
	return statements, nil
}

// foreignKeyViolationCounts reports PRAGMA foreign_key_check results as
// violation counts keyed by child table, parent table and foreign key id.
// Rowids are excluded because the rebuild reassigns them.
func foreignKeyViolationCounts(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}) (map[string]int, error) {
	rows, err := q.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var table, parent string
		var rowid, fkid any
		if err := rows.Scan(&table, &rowid, &parent, &fkid); err != nil {
			return nil, err
		}
		counts[fmt.Sprintf("%s->%s#%v", table, parent, fkid)]++
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return counts, nil
}

// archiveAndDropScopeLeaseTable preserves every legacy lease row as history
// JSON and then removes the lease table itself.
func archiveAndDropScopeLeaseTable(tableName string) error {
	if !dbTableExists(tableName) {
		return nil
	}
	sourceIDSQL, valueSQL := scopeLeaseRowArchiveSQL(tableName)
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin %s retirement: %w", tableName, err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(
		`INSERT INTO `+retiredWriteScopeArchiveTable+` (source_table, source_id, legacy_value)
		 SELECT ?, `+sourceIDSQL+`, `+valueSQL+` FROM `+tableName,
		tableName,
	); err != nil {
		return fmt.Errorf("archive %s rows: %w", tableName, err)
	}
	if _, err := tx.Exec(`DROP TABLE ` + tableName); err != nil {
		return fmt.Errorf("drop %s: %w", tableName, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit %s retirement: %w", tableName, err)
	}
	return nil
}

func scopeLeaseRowArchiveSQL(tableName string) (string, string) {
	switch tableName {
	case "parallel_wave_scope_lease":
		return "CAST(id AS TEXT)",
			`json_object('project_id', project_id, 'wave_id', wave_id, 'scope_path', scope_path, 'active', active, 'created_at', created_at, 'released_at', COALESCE(released_at, ''))`
	case "project_write_lease":
		return "id",
			`json_object('project_id', project_id, 'lease_set_id', lease_set_id, 'owner_kind', owner_kind, 'owner_id', owner_id, 'scope_path', scope_path, 'fencing_token', fencing_token, 'state', state, 'process_id', COALESCE(process_id, 0), 'process_start_identity', COALESCE(process_start_identity, ''), 'binary_build_id', binary_build_id, 'binary_epoch', binary_epoch, 'created_at', created_at, 'heartbeat_at', heartbeat_at, 'released_at', COALESCE(released_at, ''), 'release_reason', COALESCE(release_reason, ''))`
	case "project_write_fence":
		return "CAST(project_id AS TEXT)",
			`json_object('project_id', project_id, 'next_token', next_token, 'updated_at', updated_at)`
	default:
		return "CAST(rowid AS TEXT)", `json_object('rowid', rowid)`
	}
}
