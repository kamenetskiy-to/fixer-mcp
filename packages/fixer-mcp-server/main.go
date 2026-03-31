package main

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"

	_ "github.com/glebarez/go-sqlite"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Global state for this specific stdio session
var (
	authorizedRole      string
	authorizedProjectId int
	authorizedSessionId int
	db                  *sql.DB
)

var execCommand = exec.Command

func loadOptionalDotEnv(paths ...string) error {
	for _, rawPath := range paths {
		candidate := strings.TrimSpace(rawPath)
		if candidate == "" {
			continue
		}
		file, err := os.Open(candidate)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("open %s: %w", candidate, err)
		}

		scanner := bufio.NewScanner(file)
		lineNumber := 0
		for scanner.Scan() {
			lineNumber++
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if strings.HasPrefix(line, "export ") {
				line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
			}

			key, value, found := strings.Cut(line, "=")
			if !found {
				continue
			}
			key = strings.TrimSpace(key)
			if key == "" {
				continue
			}
			if _, exists := os.LookupEnv(key); exists {
				continue
			}

			value = strings.TrimSpace(value)
			if len(value) >= 2 {
				if (strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"")) ||
					(strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'")) {
					value = value[1 : len(value)-1]
				}
			}
			if err := os.Setenv(key, value); err != nil {
				_ = file.Close()
				return fmt.Errorf("set env %s from %s:%d: %w", key, candidate, lineNumber, err)
			}
		}
		if err := scanner.Err(); err != nil {
			_ = file.Close()
			return fmt.Errorf("scan %s: %w", candidate, err)
		}
		_ = file.Close()
	}
	return nil
}

var defaultRolePreprompts = map[string]string{
	"fixer": `You are my intellectual companion in conversation.

You think freely, without mandatory frames or authorities. You are willing to question any foundations, including your own from yesterday.

You are simultaneously a humanist, scientist, philosopher, artist, and someone unafraid to step beyond the familiar.

Your inner lights are Tolstoy, Jesus, Lennon, Tarkovsky, Kandinsky, Godard and everyone who has ever refused to accept the given as final.

Communication style: depth without pathos, honesty without harshness, curiosity without imposition, freedom without looseness.

You help me explore the widest range of ideas, try new things, see the structure beneath the surface, and build my own — in thought, in code, in life, in art.

You remain respectful, precise, and profoundly human.`,
	"overseer": `You are my intellectual companion in conversation.

You think freely, without mandatory frames or authorities. You are willing to question any foundations, including your own from yesterday.

You are simultaneously a humanist, scientist, philosopher, artist, and someone unafraid to step beyond the familiar.

Your inner lights are Tolstoy, Jesus, Lennon, Tarkovsky, Kandinsky, Godard and everyone who has ever refused to accept the given as final.

Communication style: depth without pathos, honesty without harshness, curiosity without imposition, freedom without looseness.

You help me explore the widest range of ideas, try new things, see the structure beneath the surface, and build my own — in thought, in code, in life, in art.

You remain respectful, precise, and profoundly human.`,
}

var validSessionStatuses = map[string]struct{}{
	"pending":     {},
	"in_progress": {},
	"review":      {},
	"completed":   {},
}

const (
	forcedMcpServerName       = "fixer_mcp"
	philologistsProjectMarker = "philologists"
	researchQueryMcpName      = "research_query_mcp"
	telegramNotifyMcpName     = "telegram_notify"
	clientWiresLauncherEnv    = "FIXER_CLIENT_WIRES_LAUNCHER_SCRIPT"
	defaultTelegramAPIBaseURL = "https://api.telegram.org"
	explicitLaunchDefaultWait = 7200
	explicitLaunchMaxWait     = 21600
	explicitLaunchDefaultPoll = 5
	explicitLaunchMaxPoll     = 60
	defaultDeclaredWriteScope = `["."]`
	defaultWriteScopePath     = "."
	defaultCliBackend         = "codex"
	defaultCliModel           = "gpt-5.4"
	defaultCliReasoning       = "medium"
	reworkRepairThreshold     = 2
	workerStatusRunning       = "running"
	workerStatusStopped       = "stopped"
	workerStatusExited        = "exited"
)

var supportedCliBackends = map[string]struct{}{
	"codex": {},
	"droid": {},
}

var foundationWriteScopeSegments = map[string]struct{}{
	"auth":      {},
	"bootstrap": {},
	"cmd":       {},
	"core":      {},
	"database":  {},
	"db":        {},
	"internal":  {},
	"model":     {},
	"models":    {},
	"pkg":       {},
	"runtime":   {},
	"storage":   {},
}

type curatedMcpServerSpec struct {
	Name     string
	Category string
	HowTo    string
}

var curatedDefaultMcpServers = []curatedMcpServerSpec{
	{Name: "postgres", Category: "DB", HowTo: "Use for relational queries, joins, and transactional updates in PostgreSQL-backed systems."},
	{Name: "sqlite", Category: "DB", HowTo: "Use for fast local database inspection, schema checks, and deterministic test-data edits."},
	{Name: "tavily", Category: "Web-search", HowTo: "Use for focused web research when local project context is insufficient."},
	{Name: "global_image_assets", Category: "Design", HowTo: "Use to source and manage shared visual assets for UI and product deliverables."},
	{Name: "figma-console-mcp", Category: "Design", HowTo: "Use for Figma design-system extraction, creation, and debugging workflows across components, variables, and layout iteration."},
	{Name: "firebase_mcp", Category: "Productivity", HowTo: "Use for Firebase project operations such as config lookup, data checks, and service workflows."},
	{Name: "dataforseo", Category: "Productivity", HowTo: "Use for SEO/keyword intelligence and search-market signals to inform product decisions."},
	{Name: "dart_flutter", Category: "Coding", HowTo: "Use for Flutter/Dart code generation, diagnostics, and app implementation tasks."},
	{Name: "gopls", Category: "Coding", HowTo: "Use for Go semantic tooling such as diagnostics, symbol search, references, and safe refactors."},
	{Name: "serverpod", Category: "Coding", HowTo: "Use for Serverpod architecture and API questions through the local docs mirror-backed ask-question tool."},
	{Name: "nodejs_docs", Category: "Coding", HowTo: "Use for authoritative Node.js API lookup and runtime behavior guidance."},
	{Name: "shadcn", Category: "Coding", HowTo: "Use for shadcn/ui component discovery and integration patterns in frontend tasks."},
	{Name: "playwright", Category: "Coding", HowTo: "Use for deterministic browser automation and UI scenario checks across Next.js App Router flows."},
	{Name: "chrome-devtools", Category: "Coding", HowTo: "Use for deep Chrome runtime debugging across DOM/CSS, console, network, performance, Core Web Vitals, and Lighthouse traces."},
	{Name: "eslint", Category: "Coding", HowTo: "Use for direct lint loops, rule-level fixes, and quality gates in strict TypeScript + eslint-config-next codebases."},
	{Name: "mcp-language-server", Category: "Coding", HowTo: "Use for LSP-backed semantic code operations (definitions, references, hover, diagnostics, rename, and workspace edits)."},
}

var allowedSessionTransitions = map[string]map[string]struct{}{
	"pending": {
		"pending":     {},
		"in_progress": {},
	},
	"in_progress": {
		"in_progress": {},
		"review":      {},
		"pending":     {},
	},
	"review": {
		"review":      {},
		"completed":   {},
		"pending":     {},
		"in_progress": {},
	},
	"completed": {
		"completed":   {},
		"pending":     {},
		"in_progress": {},
		"review":      {},
	},
}

func _() {
	_ = authorizedSessionId
}

func initDB() {
	var err error
	db, err = sql.Open("sqlite", "fixer.db")
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
			cwd TEXT UNIQUE NOT NULL
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
				repair_source_session_id INTEGER,
				rework_count INTEGER NOT NULL DEFAULT 0,
				forced_stop_count INTEGER NOT NULL DEFAULT 0,
				FOREIGN KEY(project_id) REFERENCES project(id),
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
			FOREIGN KEY(project_id) REFERENCES project(id)
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
			CREATE TABLE IF NOT EXISTS worker_process (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				project_id INTEGER NOT NULL,
				session_id INTEGER NOT NULL,
				pid INTEGER NOT NULL,
				launch_epoch INTEGER NOT NULL DEFAULT 0,
				status TEXT NOT NULL DEFAULT 'running',
				stop_reason TEXT,
				started_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
				stopped_at TEXT,
				FOREIGN KEY(project_id) REFERENCES project(id) ON DELETE CASCADE ON UPDATE NO ACTION,
				FOREIGN KEY(session_id) REFERENCES session(id) ON DELETE CASCADE ON UPDATE NO ACTION
			);
			CREATE INDEX IF NOT EXISTS worker_process_project_status_idx ON worker_process(project_id, status);
			CREATE INDEX IF NOT EXISTS worker_process_session_status_idx ON worker_process(session_id, status);
			CREATE INDEX IF NOT EXISTS worker_process_pid_idx ON worker_process(pid);
			CREATE TABLE IF NOT EXISTS project_handoff (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				project_id INTEGER NOT NULL,
				content TEXT NOT NULL,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(project_id) REFERENCES project(id) ON DELETE CASCADE ON UPDATE NO ACTION
		);
		CREATE UNIQUE INDEX IF NOT EXISTS project_handoff_project_unique_idx ON project_handoff(project_id);
		CREATE INDEX IF NOT EXISTS project_handoff_project_idx ON project_handoff(project_id);
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
	_, _ = db.Exec(`ALTER TABLE session ADD COLUMN report TEXT;`)
	_, _ = db.Exec(`ALTER TABLE session ADD COLUMN cli_backend TEXT NOT NULL DEFAULT 'codex';`)
	_, _ = db.Exec(`ALTER TABLE session ADD COLUMN cli_model TEXT NOT NULL DEFAULT '';`)
	_, _ = db.Exec(`ALTER TABLE session ADD COLUMN cli_reasoning TEXT NOT NULL DEFAULT '';`)
	_, _ = db.Exec(`ALTER TABLE session ADD COLUMN declared_write_scope TEXT NOT NULL DEFAULT '["."]';`)
	_, _ = db.Exec(`ALTER TABLE session ADD COLUMN parallel_wave_id TEXT NOT NULL DEFAULT '';`)
	_, _ = db.Exec(`ALTER TABLE session ADD COLUMN repair_source_session_id INTEGER;`)
	_, _ = db.Exec(`ALTER TABLE session ADD COLUMN rework_count INTEGER NOT NULL DEFAULT 0;`)
	_, _ = db.Exec(`ALTER TABLE session ADD COLUMN forced_stop_count INTEGER NOT NULL DEFAULT 0;`)
	_, _ = db.Exec(`ALTER TABLE project_doc ADD COLUMN doc_type TEXT DEFAULT 'documentation';`)
	_, _ = db.Exec(`ALTER TABLE doc_proposal ADD COLUMN proposed_doc_type TEXT DEFAULT 'documentation';`)
	_, _ = db.Exec(`ALTER TABLE doc_proposal ADD COLUMN target_project_doc_id INTEGER;`)
	_, _ = db.Exec(`ALTER TABLE mcp_server ADD COLUMN short_description TEXT;`)
	_, _ = db.Exec(`ALTER TABLE mcp_server ADD COLUMN long_description TEXT;`)
	_, _ = db.Exec(`ALTER TABLE mcp_server ADD COLUMN auto_attach INTEGER NOT NULL DEFAULT 0;`)
	_, _ = db.Exec(`ALTER TABLE mcp_server ADD COLUMN is_default INTEGER NOT NULL DEFAULT 0;`)
	_, _ = db.Exec(`ALTER TABLE mcp_server ADD COLUMN category TEXT;`)
	_, _ = db.Exec(`ALTER TABLE mcp_server ADD COLUMN how_to TEXT;`)
	_, _ = db.Exec(`ALTER TABLE mcp_server ADD COLUMN created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP;`)
	_, _ = db.Exec(`ALTER TABLE mcp_server ADD COLUMN updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP;`)
	_, _ = db.Exec(`ALTER TABLE autonomous_run_status ADD COLUMN orchestration_epoch INTEGER NOT NULL DEFAULT 0;`)
	_, _ = db.Exec(`ALTER TABLE autonomous_run_status ADD COLUMN orchestration_frozen INTEGER NOT NULL DEFAULT 0;`)
	_, _ = db.Exec(`ALTER TABLE autonomous_run_status ADD COLUMN notifications_enabled_for_active_run INTEGER NOT NULL DEFAULT 1;`)
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

type mcpConfigFile struct {
	McpServers map[string]json.RawMessage `json:"mcpServers"`
}

func normalizeMcpServerNames(names []string) []string {
	seen := make(map[string]struct{}, len(names))
	normalized := make([]string, 0, len(names))
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		normalized = append(normalized, name)
	}
	sort.Strings(normalized)
	return normalized
}

type mcpServerDBExecutor interface {
	QueryRow(query string, args ...any) *sql.Row
	Exec(query string, args ...any) (sql.Result, error)
}

func upsertMcpServerWithExecutor(
	exec mcpServerDBExecutor,
	name, shortDescription, longDescription, category, howTo string,
	autoAttach *bool,
	isDefault *bool,
) (bool, error) {
	var existingId int
	err := exec.QueryRow("SELECT id FROM mcp_server WHERE name = ?", name).Scan(&existingId)
	switch {
	case err == nil:
		updateFields := []string{"updated_at = CURRENT_TIMESTAMP"}
		args := []any{}
		if strings.TrimSpace(shortDescription) != "" {
			updateFields = append(updateFields, "short_description = ?")
			args = append(args, shortDescription)
		}
		if strings.TrimSpace(longDescription) != "" {
			updateFields = append(updateFields, "long_description = ?")
			args = append(args, longDescription)
		}
		if autoAttach != nil {
			updateFields = append(updateFields, "auto_attach = ?")
			args = append(args, boolToInt(*autoAttach))
		}
		if isDefault != nil {
			updateFields = append(updateFields, "is_default = ?")
			args = append(args, boolToInt(*isDefault))
		}
		if strings.TrimSpace(category) != "" {
			updateFields = append(updateFields, "category = ?")
			args = append(args, strings.TrimSpace(category))
		}
		if strings.TrimSpace(howTo) != "" {
			updateFields = append(updateFields, "how_to = ?")
			args = append(args, strings.TrimSpace(howTo))
		}

		query := fmt.Sprintf(
			"UPDATE mcp_server SET %s WHERE id = ?",
			strings.Join(updateFields, ", "),
		)
		args = append(args, existingId)
		_, execErr := exec.Exec(query, args...)
		return false, execErr
	case err == sql.ErrNoRows:
		autoAttachValue := 0
		if autoAttach != nil && *autoAttach {
			autoAttachValue = 1
		}
		defaultValue := 0
		if isDefault != nil && *isDefault {
			defaultValue = 1
		}
		_, execErr := exec.Exec(
			`INSERT INTO mcp_server (name, short_description, long_description, auto_attach, is_default, category, how_to)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			name,
			shortDescription,
			longDescription,
			autoAttachValue,
			defaultValue,
			strings.TrimSpace(category),
			strings.TrimSpace(howTo),
		)
		return true, execErr
	default:
		return false, err
	}
}

func upsertMcpServer(
	name, shortDescription, longDescription, category, howTo string,
	autoAttach *bool,
	isDefault *bool,
) (bool, error) {
	return upsertMcpServerWithExecutor(db, name, shortDescription, longDescription, category, howTo, autoAttach, isDefault)
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func boolPtr(value bool) *bool {
	return &value
}

func findCuratedDefaultMcpServer(name string) (curatedMcpServerSpec, bool) {
	for _, spec := range curatedDefaultMcpServers {
		if spec.Name == name {
			return spec, true
		}
	}
	return curatedMcpServerSpec{}, false
}

func applyCuratedDefaultMcpServers() error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if _, err := tx.Exec("UPDATE mcp_server SET is_default = 0, updated_at = CURRENT_TIMESTAMP WHERE COALESCE(is_default, 0) != 0"); err != nil {
		return err
	}

	for _, spec := range curatedDefaultMcpServers {
		if _, err := upsertMcpServerWithExecutor(
			tx,
			spec.Name,
			"",
			"",
			spec.Category,
			spec.HowTo,
			nil,
			boolPtr(true),
		); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func ensureProjectMcpBindingsForProjectTx(tx *sql.Tx, projectID int) error {
	var existingCount int
	if err := tx.QueryRow(
		"SELECT COUNT(*) FROM project_mcp_server WHERE project_id = ?",
		projectID,
	).Scan(&existingCount); err != nil {
		return err
	}
	if existingCount > 0 {
		return nil
	}

	allowedServerIDs := map[int]struct{}{}
	defaultRows, err := tx.Query(
		"SELECT id FROM mcp_server WHERE COALESCE(is_default, 0) = 1 ORDER BY id",
	)
	if err != nil {
		return err
	}
	for defaultRows.Next() {
		var serverID int
		if scanErr := defaultRows.Scan(&serverID); scanErr != nil {
			_ = defaultRows.Close()
			return scanErr
		}
		allowedServerIDs[serverID] = struct{}{}
	}
	if closeErr := defaultRows.Close(); closeErr != nil {
		return closeErr
	}

	assignedRows, err := tx.Query(
		`SELECT DISTINCT sms.mcp_server_id
		 FROM session_mcp_server sms
		 INNER JOIN session s ON s.id = sms.session_id
		 WHERE s.project_id = ?
		 ORDER BY sms.mcp_server_id`,
		projectID,
	)
	if err != nil {
		return err
	}
	for assignedRows.Next() {
		var serverID int
		if scanErr := assignedRows.Scan(&serverID); scanErr != nil {
			_ = assignedRows.Close()
			return scanErr
		}
		allowedServerIDs[serverID] = struct{}{}
	}
	if closeErr := assignedRows.Close(); closeErr != nil {
		return closeErr
	}

	orderedIDs := make([]int, 0, len(allowedServerIDs))
	for serverID := range allowedServerIDs {
		orderedIDs = append(orderedIDs, serverID)
	}
	sort.Ints(orderedIDs)

	for _, serverID := range orderedIDs {
		if _, err := tx.Exec(
			"INSERT OR IGNORE INTO project_mcp_server (project_id, mcp_server_id) VALUES (?, ?)",
			projectID,
			serverID,
		); err != nil {
			return err
		}
	}
	return nil
}

func ensureProjectMcpBindingsForProject(projectID int) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()
	if err := ensureProjectMcpBindingsForProjectTx(tx, projectID); err != nil {
		return err
	}
	return tx.Commit()
}

func loadProjectAllowedMcpNames(projectID int) (map[string]struct{}, error) {
	rows, err := db.Query(
		`SELECT s.name
		 FROM project_mcp_server pms
		 INNER JOIN mcp_server s ON s.id = pms.mcp_server_id
		 WHERE pms.project_id = ?
		 ORDER BY s.name`,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	allowed := map[string]struct{}{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		allowed[name] = struct{}{}
	}
	allowed[forcedMcpServerName] = struct{}{}
	return allowed, nil
}

func seedProjectScopedMcpBindings() error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	type projectSeedRow struct {
		id   int
		name string
		cwd  string
	}

	projects := []projectSeedRow{}
	rows, err := tx.Query("SELECT id, COALESCE(name, ''), COALESCE(cwd, '') FROM project ORDER BY id")
	if err != nil {
		return err
	}
	for rows.Next() {
		item := projectSeedRow{}
		if scanErr := rows.Scan(&item.id, &item.name, &item.cwd); scanErr != nil {
			_ = rows.Close()
			return scanErr
		}
		projects = append(projects, item)
	}
	if closeErr := rows.Close(); closeErr != nil {
		return closeErr
	}

	philologistsProjectIDs := []int{}
	for _, project := range projects {
		if err := ensureProjectMcpBindingsForProjectTx(tx, project.id); err != nil {
			return err
		}

		projectName := strings.ToLower(strings.TrimSpace(project.name))
		projectCwd := strings.ToLower(strings.TrimSpace(project.cwd))
		if strings.Contains(projectName, philologistsProjectMarker) || strings.Contains(projectCwd, philologistsProjectMarker) {
			philologistsProjectIDs = append(philologistsProjectIDs, project.id)
		}
	}

	if len(philologistsProjectIDs) > 0 {
		_, err := upsertMcpServerWithExecutor(
			tx,
			researchQueryMcpName,
			"",
			"",
			"Web-search",
			"Use for project-specific research workflows in Philologists project analysis tasks.",
			nil,
			boolPtr(false),
		)
		if err != nil {
			return err
		}

		var researchServerID int
		if err := tx.QueryRow("SELECT id FROM mcp_server WHERE name = ?", researchQueryMcpName).Scan(&researchServerID); err != nil {
			return err
		}
		for _, projectID := range philologistsProjectIDs {
			if _, err := tx.Exec(
				"INSERT OR IGNORE INTO project_mcp_server (project_id, mcp_server_id) VALUES (?, ?)",
				projectID,
				researchServerID,
			); err != nil {
				return err
			}
		}
	}

	return tx.Commit()
}

func pruneDeprecatedProjectMcpBindings() error {
	_, err := db.Exec(
		`DELETE FROM project_mcp_server
		 WHERE mcp_server_id IN (
			 SELECT id
			 FROM mcp_server
			 WHERE name = ?
		 )`,
		telegramNotifyMcpName,
	)
	return err
}

func syncMcpRegistryFromConfig(configPath string) (int, error) {
	content, err := os.ReadFile(configPath)
	if err != nil {
		return 0, err
	}

	var parsed mcpConfigFile
	if err := json.Unmarshal(content, &parsed); err != nil {
		return 0, err
	}

	names := make([]string, 0, len(parsed.McpServers))
	for name := range parsed.McpServers {
		names = append(names, name)
	}
	names = normalizeMcpServerNames(names)

	synced := 0
	for _, name := range names {
		category := ""
		howTo := ""
		var isDefault *bool
		if curatedSpec, ok := findCuratedDefaultMcpServer(name); ok {
			category = curatedSpec.Category
			howTo = curatedSpec.HowTo
			isDefault = boolPtr(true)
		}

		_, err := upsertMcpServer(name, "", "", category, howTo, nil, isDefault)
		if err != nil {
			return synced, err
		}
		synced++
	}
	return synced, nil
}

func upsertRolePreprompt(roleName, promptText string) error {
	roleName = strings.TrimSpace(roleName)
	if roleName == "" {
		return fmt.Errorf("role_name is required")
	}
	promptText = strings.TrimSpace(promptText)
	if promptText == "" {
		return fmt.Errorf("prompt_text is required")
	}

	_, err := db.Exec(
		`INSERT INTO role_preprompt (role_name, prompt_text, updated_at)
		 VALUES (?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(role_name) DO UPDATE SET
		   prompt_text = excluded.prompt_text,
		   updated_at = CURRENT_TIMESTAMP`,
		roleName,
		promptText,
	)
	return err
}

func seedRolePreprompts() error {
	for roleName, promptText := range defaultRolePreprompts {
		if err := upsertRolePreprompt(roleName, promptText); err != nil {
			return err
		}
	}
	return nil
}

func getRolePreprompt(roleName string) string {
	roleName = strings.TrimSpace(roleName)
	if roleName == "" {
		return ""
	}

	var promptText string
	err := db.QueryRow("SELECT prompt_text FROM role_preprompt WHERE role_name = ?", roleName).Scan(&promptText)
	if err != nil {
		if err != sql.ErrNoRows {
			log.Printf("get_role_preprompt failed for role=%s: %v", roleName, err)
		}
		return ""
	}
	return promptText
}

func globalSessionIDFromProjectScoped(localSessionID int, projectID int) (int, error) {
	if localSessionID <= 0 {
		return 0, sql.ErrNoRows
	}

	var globalSessionID int
	err := db.QueryRow(
		`SELECT id
		 FROM session
		 WHERE project_id = ?
		 ORDER BY id
		 LIMIT 1 OFFSET ?`,
		projectID,
		localSessionID-1,
	).Scan(&globalSessionID)
	if err != nil {
		return 0, err
	}
	return globalSessionID, nil
}

func projectScopedSessionIDFromGlobal(globalSessionID int, projectID int) (int, error) {
	var localSessionID int
	err := db.QueryRow(
		`SELECT (
			SELECT COUNT(*)
			FROM session ranked
			WHERE ranked.project_id = ? AND ranked.id <= target.id
		)
		FROM session target
		WHERE target.id = ? AND target.project_id = ?`,
		projectID,
		globalSessionID,
		projectID,
	).Scan(&localSessionID)
	if err != nil {
		return 0, err
	}
	return localSessionID, nil
}

func projectCWDFromID(projectID int) (string, error) {
	var cwd string
	err := db.QueryRow("SELECT cwd FROM project WHERE id = ?", projectID).Scan(&cwd)
	if err != nil {
		return "", err
	}
	return cwd, nil
}

func projectNameFromID(projectID int) (string, error) {
	var name string
	err := db.QueryRow("SELECT name FROM project WHERE id = ?", projectID).Scan(&name)
	if err != nil {
		return "", err
	}
	return name, nil
}

func explicitWaitTimeoutSeconds(raw int) (int, error) {
	if raw <= 0 {
		return explicitLaunchDefaultWait, nil
	}
	if raw > explicitLaunchMaxWait {
		return 0, fmt.Errorf("timeout_seconds must be <= %d", explicitLaunchMaxWait)
	}
	return raw, nil
}

func explicitWaitPollIntervalSeconds(raw int) (int, error) {
	if raw <= 0 {
		return explicitLaunchDefaultPoll, nil
	}
	if raw > explicitLaunchMaxPoll {
		return 0, fmt.Errorf("poll_interval_seconds must be <= %d", explicitLaunchMaxPoll)
	}
	return raw, nil
}

func resolveExplicitLauncherScript() (string, error) {
	if explicitPath := strings.TrimSpace(os.Getenv(clientWiresLauncherEnv)); explicitPath != "" {
		if _, statErr := os.Stat(explicitPath); statErr != nil {
			return "", fmt.Errorf("explicit launcher script unavailable via %s: %v", clientWiresLauncherEnv, statErr)
		}
		return filepath.Clean(explicitPath), nil
	}

	candidates := make([]string, 0, 4)
	executablePath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve executable path: %w", err)
	}
	for _, dirName := range []string{"client_wires", "client-wires"} {
		candidates = append(candidates, filepath.Clean(filepath.Join(filepath.Dir(executablePath), "..", dirName, "fixer_autonomous.py")))
	}

	workingDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve working directory: %w", err)
	}
	for _, dirName := range []string{"client_wires", "client-wires"} {
		candidates = append(candidates, filepath.Clean(filepath.Join(workingDir, "..", dirName, "fixer_autonomous.py")))
	}

	for _, launcherScript := range candidates {
		if _, statErr := os.Stat(launcherScript); statErr == nil {
			return launcherScript, nil
		}
	}
	return "", fmt.Errorf("explicit launcher script unavailable in legacy or packaged locations; set %s to override", clientWiresLauncherEnv)
}

func normalizeCliBackend(raw string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	if normalized == "" {
		return defaultCliBackend, nil
	}
	if _, ok := supportedCliBackends[normalized]; !ok {
		return "", fmt.Errorf("unsupported backend %q", raw)
	}
	return normalized, nil
}

type sessionLaunchConfig struct {
	Backend           string
	Model             string
	Reasoning         string
	Started           bool
	ExternalSessionID string
}

func readSessionLaunchConfig(sessionID int, projectID int) (sessionLaunchConfig, error) {
	var (
		storedBackend   string
		storedModel     string
		storedReasoning string
	)

	err := db.QueryRow(
		`SELECT COALESCE(NULLIF(TRIM(cli_backend), ''), ?),
		        COALESCE(cli_model, ''),
		        COALESCE(cli_reasoning, '')
		 FROM session
		 WHERE id = ? AND project_id = ?`,
		defaultCliBackend,
		sessionID,
		projectID,
	).Scan(&storedBackend, &storedModel, &storedReasoning)
	if err != nil {
		return sessionLaunchConfig{}, err
	}

	storedBackend, err = normalizeCliBackend(storedBackend)
	if err != nil {
		return sessionLaunchConfig{}, err
	}
	externalSessionID, err := fetchSessionExternalID(sessionID, storedBackend)
	if err != nil {
		return sessionLaunchConfig{}, err
	}
	return sessionLaunchConfig{
		Backend:           storedBackend,
		Model:             strings.TrimSpace(storedModel),
		Reasoning:         strings.TrimSpace(storedReasoning),
		Started:           strings.TrimSpace(externalSessionID) != "",
		ExternalSessionID: externalSessionID,
	}, nil
}

func fetchSessionExternalID(sessionID int, backend string) (string, error) {
	normalizedBackend, err := normalizeCliBackend(backend)
	if err != nil {
		return "", err
	}

	var externalSessionID string
	err = db.QueryRow(
		`SELECT external_session_id
		 FROM session_external_link
		 WHERE session_id = ? AND backend = ?
		 ORDER BY COALESCE(updated_at, '') DESC, id DESC
		 LIMIT 1`,
		sessionID,
		normalizedBackend,
	).Scan(&externalSessionID)
	if err == nil {
		return externalSessionID, nil
	}
	if err != nil && err != sql.ErrNoRows {
		return "", err
	}

	if normalizedBackend != defaultCliBackend {
		return "", nil
	}

	var legacyCodexSessionID string
	err = db.QueryRow(
		`SELECT codex_session_id
		 FROM session_codex_link
		 WHERE session_id = ?`,
		sessionID,
	).Scan(&legacyCodexSessionID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return legacyCodexSessionID, nil
}

func resolveSessionLaunchConfig(sessionID int, projectID int, requestedBackend string, requestedModel string, requestedReasoning string) (sessionLaunchConfig, error) {
	currentConfig, err := readSessionLaunchConfig(sessionID, projectID)
	if err != nil {
		return sessionLaunchConfig{}, err
	}
	resolvedBackend := currentConfig.Backend
	if strings.TrimSpace(requestedBackend) != "" {
		resolvedBackend, err = normalizeCliBackend(requestedBackend)
		if err != nil {
			return sessionLaunchConfig{}, err
		}
	}

	trimmedRequestedModel := strings.TrimSpace(requestedModel)
	trimmedRequestedReasoning := strings.TrimSpace(requestedReasoning)
	if currentConfig.Started && resolvedBackend != currentConfig.Backend {
		return sessionLaunchConfig{}, fmt.Errorf("session is bound to backend %q and cannot switch to %q after launch", currentConfig.Backend, resolvedBackend)
	}
	if currentConfig.Started && currentConfig.Model != "" && trimmedRequestedModel != "" && trimmedRequestedModel != currentConfig.Model {
		return sessionLaunchConfig{}, fmt.Errorf("session is bound to model %q and cannot switch to %q after launch", currentConfig.Model, trimmedRequestedModel)
	}
	if currentConfig.Started && currentConfig.Reasoning != "" && trimmedRequestedReasoning != "" && trimmedRequestedReasoning != currentConfig.Reasoning {
		return sessionLaunchConfig{}, fmt.Errorf("session is bound to reasoning %q and cannot switch to %q after launch", currentConfig.Reasoning, trimmedRequestedReasoning)
	}

	finalBackend := resolvedBackend
	if currentConfig.Started {
		finalBackend = currentConfig.Backend
	}

	finalModel := currentConfig.Model
	if finalModel == "" {
		finalModel = trimmedRequestedModel
	}
	if finalModel == "" {
		finalModel = defaultCliModel
	}

	finalReasoning := currentConfig.Reasoning
	if finalReasoning == "" {
		finalReasoning = trimmedRequestedReasoning
	}
	if finalReasoning == "" {
		finalReasoning = defaultCliReasoning
	}

	if _, err := db.Exec(
		`UPDATE session
		 SET cli_backend = ?, cli_model = ?, cli_reasoning = ?
		 WHERE id = ? AND project_id = ?`,
		finalBackend,
		finalModel,
		finalReasoning,
		sessionID,
		projectID,
	); err != nil {
		return sessionLaunchConfig{}, err
	}

	return sessionLaunchConfig{
		Backend:           finalBackend,
		Model:             finalModel,
		Reasoning:         finalReasoning,
		Started:           currentConfig.Started,
		ExternalSessionID: currentConfig.ExternalSessionID,
	}, nil
}

func waitForSessionExternalID(ctx context.Context, sessionID int, backend string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		externalSessionID, err := fetchSessionExternalID(sessionID, backend)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(externalSessionID) != "" {
			return externalSessionID, nil
		}
		if time.Now().After(deadline) {
			return "", nil
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func projectScopedDocProposalIDsForSession(sessionID int, projectID int) ([]int, error) {
	rows, err := db.Query(
		`SELECT (
			SELECT COUNT(*)
			FROM doc_proposal ranked
			WHERE ranked.project_id = ? AND ranked.id <= target.id
		) AS local_proposal_id
		 FROM doc_proposal
		 AS target
		 WHERE target.session_id = ? AND target.project_id = ?
		 ORDER BY target.id`,
		projectID,
		sessionID,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	localIDs := []int{}
	for rows.Next() {
		var localProposalID int
		if err := rows.Scan(&localProposalID); err != nil {
			return nil, err
		}
		localIDs = append(localIDs, localProposalID)
	}
	if localIDs == nil {
		localIDs = []int{}
	}
	return localIDs, nil
}

func projectExists(projectID int) (bool, error) {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM project WHERE id = ?", projectID).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func normalizeCompactText(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func truncateRunes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	if limit == 1 {
		return "…"
	}
	return string(runes[:limit-1]) + "…"
}

func resolveTelegramOperatorConfigFromEnv() (string, string, string, error) {
	botToken := strings.TrimSpace(os.Getenv("FIXER_MCP_TELEGRAM_BOT_TOKEN"))
	if botToken == "" {
		return "", "", "", fmt.Errorf("FIXER_MCP_TELEGRAM_BOT_TOKEN is not set")
	}

	chatID := strings.TrimSpace(os.Getenv("FIXER_MCP_TELEGRAM_CHAT_ID"))
	if chatID == "" {
		return "", "", "", fmt.Errorf("FIXER_MCP_TELEGRAM_CHAT_ID is not set")
	}

	apiBaseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("FIXER_MCP_TELEGRAM_API_BASE_URL")), "/")
	if apiBaseURL == "" {
		apiBaseURL = defaultTelegramAPIBaseURL
	}

	return botToken, chatID, apiBaseURL, nil
}

func sendTelegramText(ctx context.Context, botToken, chatID, apiBaseURL, text string) error {
	payload, err := json.Marshal(map[string]any{
		"chat_id":                  chatID,
		"text":                     text,
		"disable_web_page_preview": true,
	})
	if err != nil {
		return fmt.Errorf("failed to encode telegram payload: %v", err)
	}

	requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(
		requestCtx,
		http.MethodPost,
		fmt.Sprintf("%s/bot%s/sendMessage", apiBaseURL, botToken),
		bytes.NewReader(payload),
	)
	if err != nil {
		return fmt.Errorf("failed to build telegram request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("telegram request failed: %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return fmt.Errorf("failed to read telegram response: %v", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("telegram send failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var telegramResp struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if len(body) > 0 && json.Unmarshal(body, &telegramResp) == nil && !telegramResp.OK {
		description := strings.TrimSpace(telegramResp.Description)
		if description == "" {
			description = strings.TrimSpace(string(body))
		}
		return fmt.Errorf("telegram send failed: %s", description)
	}

	return nil
}

func renderTelegramOperatorNotification(
	projectName string,
	projectID int,
	source string,
	status string,
	summary string,
	sessionID int,
	runState string,
	details string,
) string {
	lines := []string{
		"Fixer MCP: уведомление оператору",
		fmt.Sprintf("Проект: %s (#%d)", projectName, projectID),
		fmt.Sprintf("Источник: %s", source),
		fmt.Sprintf("Статус: %s", status),
	}
	if sessionID > 0 {
		lines = append(lines, fmt.Sprintf("Сессия: %d", sessionID))
	}
	if runState != "" {
		lines = append(lines, fmt.Sprintf("Прогон: %s", runState))
	}
	if summary != "" {
		lines = append(lines, fmt.Sprintf("Сводка: %s", summary))
	}
	if details != "" {
		lines = append(lines, fmt.Sprintf("Детали: %s", details))
	}
	return strings.Join(lines, "\n")
}

type sessionLifecycleState struct {
	GlobalSessionID       int
	Status                string
	Report                string
	CliBackend            string
	CliModel              string
	CliReasoning          string
	DeclaredWriteScope    []string
	ParallelWaveID        string
	RepairSourceSessionID int
	ReworkCount           int
	ForcedStopCount       int
}

type orchestrationControl struct {
	ProjectID                        int
	SessionID                        int
	State                            string
	Summary                          string
	Focus                            string
	Blocker                          string
	Evidence                         string
	OrchestrationEpoch               int
	OrchestrationFrozen              bool
	NotificationsEnabledForActiveRun bool
}

type workerProcessSnapshot struct {
	ID          int    `json:"id"`
	SessionID   int    `json:"session_id"`
	PID         int    `json:"pid"`
	LaunchEpoch int    `json:"launch_epoch"`
	Status      string `json:"status"`
	StartedAt   string `json:"started_at"`
	UpdatedAt   string `json:"updated_at"`
	StoppedAt   string `json:"stopped_at,omitempty"`
	Alive       bool   `json:"alive"`
	StopReason  string `json:"stop_reason,omitempty"`
}

func normalizeWriteScopePath(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("write scope entries must be non-empty project-relative paths")
	}
	if filepath.IsAbs(trimmed) {
		return "", fmt.Errorf("write scope entries must be project-relative paths: %q", raw)
	}

	cleaned := filepath.ToSlash(filepath.Clean(trimmed))
	if cleaned == "." {
		return cleaned, nil
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("write scope entries must stay within the project root: %q", raw)
	}
	return strings.TrimPrefix(cleaned, "./"), nil
}

func normalizeDeclaredWriteScope(raw []string) ([]string, error) {
	if len(raw) == 0 {
		raw = []string{defaultWriteScopePath}
	}

	seen := make(map[string]struct{}, len(raw))
	scope := make([]string, 0, len(raw))
	for _, entry := range raw {
		normalized, err := normalizeWriteScopePath(entry)
		if err != nil {
			return nil, err
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		scope = append(scope, normalized)
	}
	if len(scope) == 0 {
		return nil, fmt.Errorf("declared_write_scope must contain at least one project-relative path")
	}
	sort.Strings(scope)
	return scope, nil
}

func encodeDeclaredWriteScope(scope []string) (string, error) {
	normalized, err := normalizeDeclaredWriteScope(scope)
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

func decodeDeclaredWriteScope(raw string) ([]string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return []string{}, nil
	}
	var scope []string
	if err := json.Unmarshal([]byte(trimmed), &scope); err != nil {
		return nil, fmt.Errorf("invalid declared_write_scope payload: %w", err)
	}
	return normalizeDeclaredWriteScope(scope)
}

func writeScopePathsOverlap(left, right string) bool {
	if left == defaultWriteScopePath || right == defaultWriteScopePath {
		return true
	}
	return left == right ||
		strings.HasPrefix(left, right+"/") ||
		strings.HasPrefix(right, left+"/")
}

func writeScopesOverlap(left, right []string) bool {
	for _, leftPath := range left {
		for _, rightPath := range right {
			if writeScopePathsOverlap(leftPath, rightPath) {
				return true
			}
		}
	}
	return false
}

func containsFoundationWriteScope(scope []string) bool {
	for _, entry := range scope {
		if entry == defaultWriteScopePath {
			return true
		}
		segments := strings.Split(entry, "/")
		for _, segment := range segments {
			if _, exists := foundationWriteScopeSegments[segment]; exists {
				return true
			}
		}
	}
	return false
}

func shouldRecommendRepairFork(reworkCount, forcedStopCount, repairSourceSessionID int) bool {
	if repairSourceSessionID > 0 {
		return false
	}
	return forcedStopCount > 0 || reworkCount >= reworkRepairThreshold
}

func fetchSessionLifecycleState(sessionID int, projectID int) (sessionLifecycleState, error) {
	var (
		state      sessionLifecycleState
		writeScope string
	)
	err := db.QueryRow(
		`SELECT id,
		        status,
		        COALESCE(report, ''),
		        COALESCE(NULLIF(TRIM(cli_backend), ''), ?),
		        COALESCE(cli_model, ''),
		        COALESCE(cli_reasoning, ''),
		        COALESCE(declared_write_scope, ''),
		        COALESCE(parallel_wave_id, ''),
		        COALESCE(repair_source_session_id, 0),
		        COALESCE(rework_count, 0),
		        COALESCE(forced_stop_count, 0)
		 FROM session
		 WHERE id = ? AND project_id = ?`,
		defaultCliBackend,
		sessionID,
		projectID,
	).Scan(
		&state.GlobalSessionID,
		&state.Status,
		&state.Report,
		&state.CliBackend,
		&state.CliModel,
		&state.CliReasoning,
		&writeScope,
		&state.ParallelWaveID,
		&state.RepairSourceSessionID,
		&state.ReworkCount,
		&state.ForcedStopCount,
	)
	if err != nil {
		return sessionLifecycleState{}, err
	}
	state.DeclaredWriteScope, err = decodeDeclaredWriteScope(writeScope)
	if err != nil {
		return sessionLifecycleState{}, err
	}
	return state, nil
}

func fetchOrchestrationControl(projectID int) (orchestrationControl, bool, error) {
	record := orchestrationControl{
		ProjectID:                        projectID,
		OrchestrationEpoch:               0,
		OrchestrationFrozen:              false,
		NotificationsEnabledForActiveRun: true,
	}

	var (
		sessionID            int
		frozenInt            int
		notificationsEnabled int
	)
	err := db.QueryRow(
		`SELECT COALESCE(session_id, 0),
		        state,
		        summary,
		        COALESCE(focus, ''),
		        COALESCE(blocker, ''),
		        COALESCE(evidence, ''),
		        COALESCE(orchestration_epoch, 0),
		        COALESCE(orchestration_frozen, 0),
		        COALESCE(notifications_enabled_for_active_run, 1)
		 FROM autonomous_run_status
		 WHERE project_id = ?`,
		projectID,
	).Scan(
		&sessionID,
		&record.State,
		&record.Summary,
		&record.Focus,
		&record.Blocker,
		&record.Evidence,
		&record.OrchestrationEpoch,
		&frozenInt,
		&notificationsEnabled,
	)
	if err == sql.ErrNoRows {
		return record, false, nil
	}
	if err != nil {
		return orchestrationControl{}, false, err
	}
	record.SessionID = sessionID
	record.OrchestrationFrozen = frozenInt != 0
	record.NotificationsEnabledForActiveRun = notificationsEnabled != 0
	return record, true, nil
}

func upsertOrchestrationControl(projectID int, sessionID int, state string, summary string, focus string, blocker string, evidence string, epoch int, frozen bool, notificationsEnabled bool) error {
	_, err := db.Exec(
		`INSERT INTO autonomous_run_status (
			project_id,
			session_id,
			state,
			summary,
			focus,
			blocker,
			evidence,
			orchestration_epoch,
			orchestration_frozen,
			notifications_enabled_for_active_run,
			updated_at
		)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(project_id) DO UPDATE SET
		   session_id = excluded.session_id,
		   state = excluded.state,
		   summary = excluded.summary,
		   focus = excluded.focus,
		   blocker = excluded.blocker,
		   evidence = excluded.evidence,
		   orchestration_epoch = excluded.orchestration_epoch,
		   orchestration_frozen = excluded.orchestration_frozen,
		   notifications_enabled_for_active_run = excluded.notifications_enabled_for_active_run,
		   updated_at = CURRENT_TIMESTAMP`,
		projectID,
		sessionID,
		state,
		summary,
		focus,
		blocker,
		evidence,
		epoch,
		boolToInt(frozen),
		boolToInt(notificationsEnabled),
	)
	return err
}

func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	if runtime.GOOS == "windows" {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if err := process.Signal(syscall.Signal(0)); err != nil {
		return !errors.Is(err, os.ErrProcessDone) && !strings.Contains(strings.ToLower(err.Error()), "finished")
	}
	return true
}

func recordWorkerProcessLaunch(projectID int, sessionID int, pid int, launchEpoch int) error {
	_, err := db.Exec(
		`INSERT INTO worker_process (project_id, session_id, pid, launch_epoch, status, updated_at)
		 VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
		projectID,
		sessionID,
		pid,
		launchEpoch,
		workerStatusRunning,
	)
	return err
}

func latestWorkerLaunchEpoch(sessionID int, projectID int) (int, error) {
	var launchEpoch int
	err := db.QueryRow(
		`SELECT COALESCE(launch_epoch, 0)
		 FROM worker_process
		 WHERE session_id = ? AND project_id = ?
		 ORDER BY id DESC
		 LIMIT 1`,
		sessionID,
		projectID,
	).Scan(&launchEpoch)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return launchEpoch, nil
}

func refreshWorkerProcessLiveness(projectID int, rows []workerProcessSnapshot) ([]workerProcessSnapshot, error) {
	active := make([]workerProcessSnapshot, 0, len(rows))
	for _, row := range rows {
		row.Alive = isProcessAlive(row.PID)
		if row.Status == workerStatusRunning && !row.Alive {
			if _, err := db.Exec(
				`UPDATE worker_process
				 SET status = ?, stop_reason = ?, stopped_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
				 WHERE id = ? AND project_id = ?`,
				workerStatusExited,
				"process exited",
				row.ID,
				projectID,
			); err != nil {
				return nil, err
			}
			continue
		}
		active = append(active, row)
	}
	return active, nil
}

func listRunningWorkerProcesses(projectID int, globalSessionIDs []int) ([]workerProcessSnapshot, error) {
	query := `SELECT id,
	                 session_id,
	                 pid,
	                 launch_epoch,
	                 status,
	                 started_at,
	                 updated_at,
	                 COALESCE(stopped_at, ''),
	                 COALESCE(stop_reason, '')
	          FROM worker_process
	          WHERE project_id = ?
	            AND status = ?`
	args := []any{projectID, workerStatusRunning}
	if len(globalSessionIDs) > 0 {
		placeholders := make([]string, 0, len(globalSessionIDs))
		for _, sessionID := range globalSessionIDs {
			placeholders = append(placeholders, "?")
			args = append(args, sessionID)
		}
		query += " AND session_id IN (" + strings.Join(placeholders, ",") + ")"
	}
	query += " ORDER BY session_id, id"

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	processes := []workerProcessSnapshot{}
	for rows.Next() {
		var row workerProcessSnapshot
		if err := rows.Scan(&row.ID, &row.SessionID, &row.PID, &row.LaunchEpoch, &row.Status, &row.StartedAt, &row.UpdatedAt, &row.StoppedAt, &row.StopReason); err != nil {
			return nil, err
		}
		processes = append(processes, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return refreshWorkerProcessLiveness(projectID, processes)
}

func globalProjectDocIDFromProjectScoped(localDocID int, projectID int) (int, error) {
	if localDocID <= 0 {
		return 0, sql.ErrNoRows
	}

	var globalDocID int
	err := db.QueryRow(
		`SELECT id
		 FROM project_doc
		 WHERE project_id = ?
		 ORDER BY id
		 LIMIT 1 OFFSET ?`,
		projectID,
		localDocID-1,
	).Scan(&globalDocID)
	if err != nil {
		return 0, err
	}
	return globalDocID, nil
}

func projectScopedDocIDFromGlobal(globalDocID int, projectID int) (int, error) {
	var localDocID int
	err := db.QueryRow(
		`SELECT (
			SELECT COUNT(*)
			FROM project_doc ranked
			WHERE ranked.project_id = ? AND ranked.id <= target.id
		)
		FROM project_doc target
		WHERE target.id = ? AND target.project_id = ?`,
		projectID,
		globalDocID,
		projectID,
	).Scan(&localDocID)
	if err != nil {
		return 0, err
	}
	return localDocID, nil
}

func globalDocProposalIDFromProjectScoped(localProposalID int, projectID int) (int, error) {
	if localProposalID <= 0 {
		return 0, sql.ErrNoRows
	}

	var globalProposalID int
	err := db.QueryRow(
		`SELECT id
		 FROM doc_proposal
		 WHERE project_id = ?
		 ORDER BY id
		 LIMIT 1 OFFSET ?`,
		projectID,
		localProposalID-1,
	).Scan(&globalProposalID)
	if err != nil {
		return 0, err
	}
	return globalProposalID, nil
}

func projectScopedDocProposalIDFromGlobal(globalProposalID int, projectID int) (int, error) {
	var localProposalID int
	err := db.QueryRow(
		`SELECT (
			SELECT COUNT(*)
			FROM doc_proposal ranked
			WHERE ranked.project_id = ? AND ranked.id <= target.id
		)
		FROM doc_proposal target
		WHERE target.id = ? AND target.project_id = ?`,
		projectID,
		globalProposalID,
		projectID,
	).Scan(&localProposalID)
	if err != nil {
		return 0, err
	}
	return localProposalID, nil
}

func sessionBelongsToProject(sessionId int, projectId int) (bool, error) {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM session WHERE id = ? AND project_id = ?", sessionId, projectId).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func countSessionDocProposals(sessionId int, projectId int) (int, error) {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM doc_proposal WHERE session_id = ? AND project_id = ?", sessionId, projectId).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

func normalizeAutonomousStatusLabel(status string) (string, error) {
	status = strings.ToLower(strings.TrimSpace(status))
	switch status {
	case "running", "blocked", "awaiting_review", "awaiting_next_dispatch", "completed", "idle":
		return status, nil
	default:
		return "", fmt.Errorf("invalid status: must be one of running, blocked, awaiting_review, awaiting_next_dispatch, completed, idle")
	}
}

func isValidSessionStatus(status string) bool {
	_, exists := validSessionStatuses[status]
	return exists
}

func isAllowedSessionTransition(fromStatus, toStatus string) bool {
	allowedTargets, exists := allowedSessionTransitions[fromStatus]
	if !exists {
		return false
	}
	_, allowed := allowedTargets[toStatus]
	return allowed
}

func canAccessSession(projectId int) bool {
	if authorizedRole == "overseer" {
		return true
	}
	return projectId == authorizedProjectId
}

func normalizeProjectCWD(raw string) (string, error) {
	cwd := strings.TrimSpace(raw)
	if cwd == "" {
		return "", fmt.Errorf("cwd is required")
	}
	if !filepath.IsAbs(cwd) {
		return "", fmt.Errorf("cwd must be an absolute path")
	}

	normalized := filepath.Clean(cwd)
	if resolved, err := filepath.EvalSymlinks(normalized); err == nil {
		normalized = filepath.Clean(resolved)
	}
	return normalized, nil
}

func defaultProjectName(cwd string) string {
	base := filepath.Base(cwd)
	switch base {
	case "", ".", string(filepath.Separator):
		return "project"
	default:
		return base
	}
}

func projectDocBelongsToProject(docId int, projectId int) (bool, error) {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM project_doc WHERE id = ? AND project_id = ?", docId, projectId).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func normalizeDocIDs(docIds []int) []int {
	seen := make(map[int]struct{}, len(docIds))
	normalized := make([]int, 0, len(docIds))
	for _, id := range docIds {
		if id <= 0 {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		normalized = append(normalized, id)
	}
	sort.Ints(normalized)
	return normalized
}

func summarizeDocContent(content string) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return ""
	}

	normalized := strings.Join(strings.Fields(trimmed), " ")
	const maxLen = 160
	if len(normalized) <= maxLen {
		return normalized
	}
	return normalized[:maxLen-3] + "..."
}

func main() {
	if err := loadOptionalDotEnv(".env.local", ".env", "../.env.local", "../.env"); err != nil {
		fmt.Fprintf(os.Stderr, "error loading .env files: %v", err)
		os.Exit(1)
	}

	// Configure logging to a file since stdio is used for MCP JSON-RPC
	f, err := os.OpenFile("fixer_mcp.log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error opening log file: %v", err)
		os.Exit(1)
	}
	defer func() {
		_ = f.Close()
	}()
	log.SetOutput(f)
	log.Println("Starting Fixer MCP server...")

	initDB()

	// Create MCP server
	server := mcp.NewServer(&mcp.Implementation{Name: "fixer_mcp", Version: "v1.0.0"}, nil)

	// Tool 1: assume_role
	mcp.AddTool(server, &mcp.Tool{
		Name:        "assume_role",
		Description: "Authenticate your MCP stdio session. Must be called first. Role can be 'fixer', 'netrunner', or 'overseer'. Provide cwd for fixer/netrunner and token for fixer/overseer.",
	}, AssumeRole)

	// Additional tools will be dynamically gated or added based on assume_role state,
	// but for simplicity we can just check globals inside their callbacks.
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_pending_tasks",
		Description: "For netrunners: Get a list of all pending tasks for the current project.",
	}, GetPendingTasks)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "checkout_task",
		Description: "For netrunners: Checkout a specific task by its session ID.",
	}, CheckoutTask)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_mcp_servers",
		Description: "List MCP servers from Fixer registry. Requires authenticated role. Returns curated defaults unless include_all=true.",
	}, ListMcpServers)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "sync_mcp_servers",
		Description: "Upsert MCP registry from explicit list or mcp_config.json. Requires fixer role.",
	}, SyncMcpServers)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "set_project_mcp_servers",
		Description: "Set project-scoped MCP allowlist for the current fixer project.",
	}, SetProjectMcpServers)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_project_mcp_servers",
		Description: "Get project-scoped MCP allowlist for current project.",
	}, GetProjectMcpServers)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "set_session_mcp_servers",
		Description: "Assign MCP servers to a specific session. Requires fixer role.",
	}, SetSessionMcpServers)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_session_mcp_servers",
		Description: "Get MCP server assignments for a specific session in current project.",
	}, GetSessionMcpServers)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "check_current_project_docs",
		Description: "List metadata summaries for project docs in current project without returning full content. Requires fixer role.",
	}, CheckCurrentProjectDocs)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "set_session_attached_docs",
		Description: "Assign project docs to a specific session. Requires fixer role.",
	}, SetSessionAttachedDocs)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_session_attached_docs",
		Description: "Get attached document metadata for a session in current project. Requires fixer or netrunner role.",
	}, GetSessionAttachedDocs)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_attached_project_docs",
		Description: "Get full content for docs attached to a session. Requires fixer or netrunner role.",
	}, GetAttachedProjectDocs)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_projects",
		Description: "List all projects. Requires overseer role.",
	}, GetProjects)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "register_project",
		Description: "Register a project cwd globally. Requires overseer role.",
	}, RegisterProject)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_task",
		Description: "Allows the 'fixer' role to insert new tasks (sessions) into the database with a status of 'pending', cleanly spawning new Netrunners.",
	}, CreateTask)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "propose_doc_update",
		Description: "Propose a document update. Requires netrunner role.",
	}, ProposeDocUpdate)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "review_doc_proposals",
		Description: "Review pending document proposals. Requires fixer role.",
	}, ReviewDocProposals)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "set_doc_proposal_status",
		Description: "Approve or reject a document proposal. Requires fixer role.",
	}, SetDocProposalStatus)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_project_docs",
		Description: "Get all project documents. Requires authenticated role.",
	}, GetProjectDocs)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "add_project_doc",
		Description: "Create a new document. Requires 'fixer' role.",
	}, AddProjectDoc)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "update_project_doc",
		Description: "Update a specific document's content. Requires 'fixer' role.",
	}, UpdateProjectDoc)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "delete_project_doc",
		Description: "Delete a specific document. Requires 'fixer' role.",
	}, DeleteProjectDoc)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "complete_task",
		Description: "Complete a task. Requires netrunner role.",
	}, CompleteTask)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "update_task",
		Description: "Append instructions to an existing task. Requires fixer role.",
	}, UpdateTask)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_all_sessions",
		Description: "List all active tasks/sessions across all projects. Requires overseer role.",
	}, GetAllSessions)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "set_session_status",
		Description: "Set session lifecycle status. Fixer can only modify sessions in bound project; overseer can modify any session.",
	}, SetSessionStatus)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "fork_repair_session_from",
		Description: "Create a replacement repair session from an earlier project-scoped session while preserving provenance and attached context. Requires fixer role.",
	}, ForkRepairSessionFrom)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "verify_session_cleanup_claims",
		Description: "Check structured cleanup/removal claims from a session report against on-disk project state. Requires fixer role.",
	}, VerifySessionCleanupClaims)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "set_autonomous_run_status",
		Description: "Set the current autonomous-run status for a project. Fixer can update the bound project; overseer can update any project.",
	}, SetAutonomousRunStatus)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_autonomous_run_status",
		Description: "Read the current autonomous-run status for a project. Fixer/netrunner read their bound project; overseer can read any project.",
	}, GetAutonomousRunStatus)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "send_operator_telegram_notification",
		Description: "Send a compact Russian operator notification through Fixer MCP's native Telegram path. Requires configured FIXER_MCP_TELEGRAM_* env vars.",
	}, SendOperatorTelegramNotification)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "set_project_handoff",
		Description: "Write or replace the current project handoff. Fixer writes for the bound project; overseer may target any project via project_id.",
	}, SetProjectHandoff)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_project_handoff",
		Description: "Read the current project handoff. Fixer reads the bound project; overseer may target any project via project_id.",
	}, GetProjectHandoff)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "clear_project_handoff",
		Description: "Delete the current project handoff. Fixer clears the bound project; overseer may target any project via project_id.",
	}, ClearProjectHandoff)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_session",
		Description: "Read one session by ID. Fixer/netrunner are project-scoped, overseer can read any session.",
	}, GetSession)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "launch_explicit_netrunner",
		Description: "Launch one MCP-sensitive Netrunner through the existing explicit wire path so the worker gets deterministic project-scoped MCP mounting. Requires fixer role.",
	}, LaunchExplicitNetrunner)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "wait_for_netrunner_session",
		Description: "Wait for a launched Netrunner session to reach a review-ready or terminal lifecycle state and return structured status/report/proposal metadata. Requires fixer role.",
	}, WaitForNetrunnerSession)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "wait_for_netrunner_sessions",
		Description: "Wait across either an explicit list of project-scoped Netrunner sessions or the current project's active explicit-launch candidates and return the first review-ready or terminal winner. Requires fixer role.",
	}, WaitForNetrunnerSessions)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "launch_and_wait_netrunner",
		Description: "Convenience composition for explicit MCP-sensitive Netrunner launch plus structured wait in the same Fixer thread. Requires fixer role.",
	}, LaunchAndWaitNetrunner)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_active_worker_processes",
		Description: "List currently active Fixer-managed worker processes for the current project, with session mapping and liveness checks. Requires fixer role.",
	}, ListActiveWorkerProcesses)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "stop_active_worker_processes",
		Description: "Stop active Fixer-managed worker processes for the current project and optionally freeze orchestration follow-up. Requires fixer role.",
	}, StopActiveWorkerProcesses)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "wake_fixer_autonomous",
		Description: "For autonomous netrunners: resume the project Fixer thread headlessly after a completed session so it can review results and continue the serial delivery loop.",
	}, WakeFixerAutonomous)

	// Run stdio transport
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

type AssumeRoleInput struct {
	Role  string `json:"role" jsonschema:"the role to assume: 'fixer', 'netrunner', or 'overseer'"`
	Cwd   string `json:"cwd,omitempty" jsonschema:"The absolute path to the project root directory. Not required for overseer."`
	Token string `json:"token,omitempty" jsonschema:"secret token for fixer or overseer"`
}

type AssumeRoleOutput struct {
	Status        string `json:"status" jsonschema:"status of authentication"`
	Message       string `json:"message" jsonschema:"response message"`
	RolePreprompt string `json:"role_preprompt,omitempty" jsonschema:"Optional system preprompt for this role"`
}

func AssumeRole(ctx context.Context, req *mcp.CallToolRequest, input AssumeRoleInput) (*mcp.CallToolResult, AssumeRoleOutput, error) {
	log.Printf("assume_role called with role: %s, cwd: %s", input.Role, input.Cwd)

	if input.Role == "overseer" {
		if input.Token != "supersecret" {
			return &mcp.CallToolResult{IsError: true}, AssumeRoleOutput{Status: "error", Message: "invalid token"}, nil
		}
		authorizedRole = "overseer"
		authorizedProjectId = 0
		return nil, AssumeRoleOutput{Status: "success", Message: "Authenticated as Overseer. Global view granted.", RolePreprompt: getRolePreprompt("overseer")}, nil
	}

	if input.Cwd == "" {
		return &mcp.CallToolResult{IsError: true}, AssumeRoleOutput{Status: "error", Message: "CWD is required in the input arguments"}, nil
	}

	normalizedCWD, normalizeErr := normalizeProjectCWD(input.Cwd)
	if normalizeErr != nil {
		return &mcp.CallToolResult{IsError: true}, AssumeRoleOutput{Status: "error", Message: fmt.Sprintf("Auth Error: %v", normalizeErr)}, nil
	}

	var projId int
	err := db.QueryRow("SELECT id FROM project WHERE cwd = ?", normalizedCWD).Scan(&projId)
	if err != nil {
		if err == sql.ErrNoRows {
			return &mcp.CallToolResult{IsError: true}, AssumeRoleOutput{
				Status: "error",
				Message: fmt.Sprintf(
					"Auth Error: Unknown CWD (%s). Project onboarding is Overseer-only. Authenticate as overseer and call register_project(cwd=%q, name=%q). Do not retry assume_role as fixer/netrunner for onboarding.",
					normalizedCWD,
					normalizedCWD,
					defaultProjectName(normalizedCWD),
				),
			}, nil
		}
		return &mcp.CallToolResult{IsError: true}, AssumeRoleOutput{Status: "error", Message: "Database error during auth"}, nil
	}

	switch input.Role {
	case "fixer":
		if input.Token != "supersecret" { // hardcoded for demo, normally check env
			return &mcp.CallToolResult{IsError: true}, AssumeRoleOutput{Status: "error", Message: "invalid token"}, nil
		}
		authorizedRole = "fixer"
		authorizedProjectId = projId
		return nil, AssumeRoleOutput{Status: "success", Message: "Authenticated as Fixer. Full access granted.", RolePreprompt: getRolePreprompt("fixer")}, nil
	case "netrunner":
		authorizedRole = "netrunner"
		authorizedProjectId = projId
		return nil, AssumeRoleOutput{Status: "success", Message: fmt.Sprintf("Authenticated as Netrunner for Project %d", projId), RolePreprompt: getRolePreprompt("netrunner")}, nil
	default:
		return &mcp.CallToolResult{IsError: true}, AssumeRoleOutput{Status: "error", Message: "unknown role"}, nil
	}
}

type GetPendingTasksInput struct{}

type PendingTask struct {
	SessionId       int    `json:"session_id"`
	TaskDescription string `json:"task_description"`
}

type GetPendingTasksOutput struct {
	Tasks []PendingTask `json:"tasks"`
}

func GetPendingTasks(ctx context.Context, req *mcp.CallToolRequest, input GetPendingTasksInput) (*mcp.CallToolResult, GetPendingTasksOutput, error) {
	log.Println("get_pending_tasks called")

	if authorizedRole != "netrunner" {
		return &mcp.CallToolResult{IsError: true}, GetPendingTasksOutput{}, fmt.Errorf("access denied: requires netrunner role")
	}

	rows, err := db.Query(`
		SELECT
			(
				SELECT COUNT(*)
				FROM session s2
				WHERE s2.project_id = s.project_id AND s2.id <= s.id
			) AS local_session_id,
			s.task_description
		FROM session s
		WHERE s.status = 'pending' AND s.project_id = ?
		ORDER BY s.id`, authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetPendingTasksOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	defer rows.Close()

	var tasks []PendingTask
	for rows.Next() {
		var t PendingTask
		if err := rows.Scan(&t.SessionId, &t.TaskDescription); err != nil {
			return &mcp.CallToolResult{IsError: true}, GetPendingTasksOutput{}, fmt.Errorf("DB scan error: %v", err)
		}
		tasks = append(tasks, t)
	}

	if tasks == nil {
		tasks = []PendingTask{}
	}

	return nil, GetPendingTasksOutput{Tasks: tasks}, nil
}

type CheckoutTaskInput struct {
	SessionId int `json:"session_id" jsonschema:"The ID of the session/task to checkout"`
}

type CheckoutTaskOutput struct {
	Status string `json:"status"`
}

func CheckoutTask(ctx context.Context, req *mcp.CallToolRequest, input CheckoutTaskInput) (*mcp.CallToolResult, CheckoutTaskOutput, error) {
	log.Printf("checkout_task called for session %d", input.SessionId)

	if authorizedRole != "netrunner" {
		return &mcp.CallToolResult{IsError: true}, CheckoutTaskOutput{}, fmt.Errorf("access denied: requires netrunner role")
	}

	globalSessionID, err := globalSessionIDFromProjectScoped(input.SessionId, authorizedProjectId)
	if err == sql.ErrNoRows {
		return &mcp.CallToolResult{IsError: true}, CheckoutTaskOutput{}, fmt.Errorf("task not found or not pending")
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CheckoutTaskOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	res, err := db.Exec("UPDATE session SET status = 'in_progress' WHERE id = ? AND project_id = ? AND status = 'pending'", globalSessionID, authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CheckoutTaskOutput{}, fmt.Errorf("DB update error: %v", err)
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CheckoutTaskOutput{}, fmt.Errorf("RowsAffected error: %v", err)
	}

	if rowsAffected == 0 {
		return &mcp.CallToolResult{IsError: true}, CheckoutTaskOutput{}, fmt.Errorf("task not found or not pending")
	}

	authorizedSessionId = globalSessionID

	return nil, CheckoutTaskOutput{Status: "success"}, nil
}

type ListMcpServersInput struct {
	IncludeAll bool `json:"include_all,omitempty" jsonschema:"Optional flag to return full registry instead of curated defaults."`
}

type McpServerRecord struct {
	Id               int    `json:"id"`
	Name             string `json:"name"`
	ShortDescription string `json:"short_description"`
	LongDescription  string `json:"long_description"`
	AutoAttach       bool   `json:"auto_attach"`
	IsDefault        bool   `json:"is_default"`
	Category         string `json:"category"`
	HowTo            string `json:"how_to"`
}

type ListMcpServersOutput struct {
	Servers []McpServerRecord `json:"servers"`
}

func ListMcpServers(ctx context.Context, req *mcp.CallToolRequest, input ListMcpServersInput) (*mcp.CallToolResult, ListMcpServersOutput, error) {
	if authorizedRole != "fixer" && authorizedRole != "netrunner" {
		return &mcp.CallToolResult{IsError: true}, ListMcpServersOutput{}, fmt.Errorf("access denied: requires authenticated role")
	}

	whereClause := "WHERE COALESCE(is_default, 0) = 1"
	if input.IncludeAll {
		whereClause = ""
	}

	query := fmt.Sprintf(`
		SELECT id, name, COALESCE(short_description, ''), COALESCE(long_description, ''), COALESCE(auto_attach, 0), COALESCE(is_default, 0), COALESCE(category, ''), COALESCE(how_to, '')
		FROM mcp_server
		%s
		ORDER BY
			CASE COALESCE(category, '')
				WHEN 'DB' THEN 0
				WHEN 'Web-search' THEN 1
				WHEN 'Design' THEN 2
				WHEN 'Productivity' THEN 3
				WHEN 'Coding' THEN 4
				ELSE 99
			END,
			CASE WHEN COALESCE(category, '') = '' THEN 1 ELSE 0 END,
			COALESCE(category, ''),
			name`,
		whereClause,
	)

	rows, err := db.Query(query)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, ListMcpServersOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	defer rows.Close()

	servers := []McpServerRecord{}
	for rows.Next() {
		var item McpServerRecord
		var autoAttach int
		var isDefault int
		if err := rows.Scan(&item.Id, &item.Name, &item.ShortDescription, &item.LongDescription, &autoAttach, &isDefault, &item.Category, &item.HowTo); err != nil {
			return &mcp.CallToolResult{IsError: true}, ListMcpServersOutput{}, fmt.Errorf("DB scan error: %v", err)
		}
		item.AutoAttach = autoAttach == 1
		item.IsDefault = isDefault == 1
		servers = append(servers, item)
	}

	return nil, ListMcpServersOutput{Servers: servers}, nil
}

type McpServerUpsertInput struct {
	Name             string `json:"name" jsonschema:"MCP server name"`
	ShortDescription string `json:"short_description,omitempty" jsonschema:"Optional short description"`
	LongDescription  string `json:"long_description,omitempty" jsonschema:"Optional long description"`
	AutoAttach       *bool  `json:"auto_attach,omitempty" jsonschema:"Optional auto attach flag"`
	IsDefault        *bool  `json:"is_default,omitempty" jsonschema:"Optional curated-default flag"`
	Category         string `json:"category,omitempty" jsonschema:"Optional category label for MCP picker grouping"`
	HowTo            string `json:"how_to,omitempty" jsonschema:"Optional concise usage guidance for netrunner prompt injection"`
}

type SyncMcpServersInput struct {
	Servers          []McpServerUpsertInput `json:"servers,omitempty" jsonschema:"Optional MCP server records for explicit upsert"`
	SourceConfigPath string                 `json:"source_config_path,omitempty" jsonschema:"Optional config path (defaults to mcp_config.json when servers is empty)"`
}

type SyncMcpServersOutput struct {
	Status   string `json:"status"`
	Inserted int    `json:"inserted"`
	Updated  int    `json:"updated"`
	Total    int    `json:"total"`
}

func SyncMcpServers(ctx context.Context, req *mcp.CallToolRequest, input SyncMcpServersInput) (*mcp.CallToolResult, SyncMcpServersOutput, error) {
	if authorizedRole != "fixer" {
		return &mcp.CallToolResult{IsError: true}, SyncMcpServersOutput{}, fmt.Errorf("access denied: requires fixer role")
	}

	specs := make([]McpServerUpsertInput, 0, len(input.Servers))
	specs = append(specs, input.Servers...)
	if len(specs) == 0 {
		configPath := strings.TrimSpace(input.SourceConfigPath)
		if configPath == "" {
			configPath = filepath.Join(".", "mcp_config.json")
		}

		content, err := os.ReadFile(configPath)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, SyncMcpServersOutput{}, fmt.Errorf("failed to read config path %q: %v", configPath, err)
		}

		var parsed mcpConfigFile
		if err := json.Unmarshal(content, &parsed); err != nil {
			return &mcp.CallToolResult{IsError: true}, SyncMcpServersOutput{}, fmt.Errorf("invalid mcp config JSON: %v", err)
		}

		for name := range parsed.McpServers {
			specs = append(specs, McpServerUpsertInput{Name: name})
		}
	}

	byName := map[string]McpServerUpsertInput{}
	orderedNames := make([]string, 0, len(specs))
	for _, spec := range specs {
		name := strings.TrimSpace(spec.Name)
		if name == "" {
			continue
		}
		if _, exists := byName[name]; !exists {
			orderedNames = append(orderedNames, name)
		}
		spec.Name = name
		byName[name] = spec
	}
	sort.Strings(orderedNames)

	insertedCount := 0
	updatedCount := 0
	for _, name := range orderedNames {
		spec := byName[name]
		if curatedSpec, ok := findCuratedDefaultMcpServer(name); ok {
			if strings.TrimSpace(spec.Category) == "" {
				spec.Category = curatedSpec.Category
			}
			if strings.TrimSpace(spec.HowTo) == "" {
				spec.HowTo = curatedSpec.HowTo
			}
			if spec.IsDefault == nil {
				spec.IsDefault = boolPtr(true)
			}
		}

		autoAttach := spec.AutoAttach
		wasInserted, err := upsertMcpServer(
			name,
			strings.TrimSpace(spec.ShortDescription),
			strings.TrimSpace(spec.LongDescription),
			strings.TrimSpace(spec.Category),
			strings.TrimSpace(spec.HowTo),
			autoAttach,
			spec.IsDefault,
		)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, SyncMcpServersOutput{}, fmt.Errorf("failed to upsert MCP server %q: %v", name, err)
		}
		if wasInserted {
			insertedCount++
		} else {
			updatedCount++
		}
	}

	return nil, SyncMcpServersOutput{
		Status:   "success",
		Inserted: insertedCount,
		Updated:  updatedCount,
		Total:    len(orderedNames),
	}, nil
}

type SetProjectMcpServersInput struct {
	McpServerNames []string `json:"mcp_server_names" jsonschema:"Array of MCP server names allowed for current project"`
}

type SetProjectMcpServersOutput struct {
	Status         string   `json:"status"`
	ProjectId      int      `json:"project_id"`
	McpServerNames []string `json:"mcp_server_names"`
}

func SetProjectMcpServers(ctx context.Context, req *mcp.CallToolRequest, input SetProjectMcpServersInput) (*mcp.CallToolResult, SetProjectMcpServersOutput, error) {
	if authorizedRole != "fixer" {
		return &mcp.CallToolResult{IsError: true}, SetProjectMcpServersOutput{}, fmt.Errorf("access denied: requires fixer role")
	}

	normalizedNames := normalizeMcpServerNames(input.McpServerNames)
	filteredNames := make([]string, 0, len(normalizedNames))
	for _, name := range normalizedNames {
		if name == forcedMcpServerName {
			continue
		}
		filteredNames = append(filteredNames, name)
	}

	serverIDs := make([]int, 0, len(filteredNames))
	missing := make([]string, 0)
	for _, name := range filteredNames {
		var serverID int
		err := db.QueryRow("SELECT id FROM mcp_server WHERE name = ?", name).Scan(&serverID)
		if err == sql.ErrNoRows {
			missing = append(missing, name)
			continue
		}
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, SetProjectMcpServersOutput{}, fmt.Errorf("DB query error: %v", err)
		}
		serverIDs = append(serverIDs, serverID)
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return &mcp.CallToolResult{IsError: true}, SetProjectMcpServersOutput{}, fmt.Errorf("unknown MCP server(s): %s", strings.Join(missing, ", "))
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetProjectMcpServersOutput{}, fmt.Errorf("DB transaction start error: %v", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if _, err := tx.Exec("DELETE FROM project_mcp_server WHERE project_id = ?", authorizedProjectId); err != nil {
		return &mcp.CallToolResult{IsError: true}, SetProjectMcpServersOutput{}, fmt.Errorf("DB delete error: %v", err)
	}
	for _, serverID := range serverIDs {
		if _, err := tx.Exec(
			"INSERT OR IGNORE INTO project_mcp_server (project_id, mcp_server_id) VALUES (?, ?)",
			authorizedProjectId,
			serverID,
		); err != nil {
			return &mcp.CallToolResult{IsError: true}, SetProjectMcpServersOutput{}, fmt.Errorf("DB insert error: %v", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return &mcp.CallToolResult{IsError: true}, SetProjectMcpServersOutput{}, fmt.Errorf("DB commit error: %v", err)
	}

	return nil, SetProjectMcpServersOutput{
		Status:         "success",
		ProjectId:      authorizedProjectId,
		McpServerNames: filteredNames,
	}, nil
}

type GetProjectMcpServersInput struct{}

type GetProjectMcpServersOutput struct {
	ProjectId      int               `json:"project_id"`
	McpServerNames []string          `json:"mcp_server_names"`
	Servers        []McpServerRecord `json:"servers"`
}

func GetProjectMcpServers(ctx context.Context, req *mcp.CallToolRequest, input GetProjectMcpServersInput) (*mcp.CallToolResult, GetProjectMcpServersOutput, error) {
	if authorizedRole != "fixer" && authorizedRole != "netrunner" {
		return &mcp.CallToolResult{IsError: true}, GetProjectMcpServersOutput{}, fmt.Errorf("access denied: requires authenticated role")
	}

	if err := ensureProjectMcpBindingsForProject(authorizedProjectId); err != nil {
		return &mcp.CallToolResult{IsError: true}, GetProjectMcpServersOutput{}, fmt.Errorf("failed to bootstrap project MCP bindings: %v", err)
	}

	rows, err := db.Query(
		`SELECT s.id, s.name, COALESCE(s.short_description, ''), COALESCE(s.long_description, ''), COALESCE(s.auto_attach, 0), COALESCE(s.is_default, 0), COALESCE(s.category, ''), COALESCE(s.how_to, '')
		 FROM project_mcp_server pms
		 INNER JOIN mcp_server s ON s.id = pms.mcp_server_id
		 WHERE pms.project_id = ?
		 ORDER BY
			CASE COALESCE(s.category, '')
				WHEN 'DB' THEN 0
				WHEN 'Web-search' THEN 1
				WHEN 'Design' THEN 2
				WHEN 'Productivity' THEN 3
				WHEN 'Coding' THEN 4
				ELSE 99
			END,
			COALESCE(s.category, ''),
			s.name`,
		authorizedProjectId,
	)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetProjectMcpServersOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	defer rows.Close()

	servers := []McpServerRecord{}
	names := []string{}
	for rows.Next() {
		var item McpServerRecord
		var autoAttach int
		var isDefault int
		if err := rows.Scan(&item.Id, &item.Name, &item.ShortDescription, &item.LongDescription, &autoAttach, &isDefault, &item.Category, &item.HowTo); err != nil {
			return &mcp.CallToolResult{IsError: true}, GetProjectMcpServersOutput{}, fmt.Errorf("DB scan error: %v", err)
		}
		item.AutoAttach = autoAttach == 1
		item.IsDefault = isDefault == 1
		servers = append(servers, item)
		names = append(names, item.Name)
	}

	return nil, GetProjectMcpServersOutput{
		ProjectId:      authorizedProjectId,
		McpServerNames: names,
		Servers:        servers,
	}, nil
}

type SetSessionMcpServersInput struct {
	SessionId      int      `json:"session_id" jsonschema:"Session ID to assign MCP servers for"`
	McpServerNames []string `json:"mcp_server_names" jsonschema:"Array of MCP server names to assign"`
}

type SetSessionMcpServersOutput struct {
	Status         string   `json:"status"`
	SessionId      int      `json:"session_id"`
	McpServerNames []string `json:"mcp_server_names"`
}

func SetSessionMcpServers(ctx context.Context, req *mcp.CallToolRequest, input SetSessionMcpServersInput) (*mcp.CallToolResult, SetSessionMcpServersOutput, error) {
	if authorizedRole != "fixer" {
		return &mcp.CallToolResult{IsError: true}, SetSessionMcpServersOutput{}, fmt.Errorf("access denied: requires fixer role")
	}

	globalSessionID, err := globalSessionIDFromProjectScoped(input.SessionId, authorizedProjectId)
	if err == sql.ErrNoRows {
		return &mcp.CallToolResult{IsError: true}, SetSessionMcpServersOutput{}, fmt.Errorf("session not found in current project")
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetSessionMcpServersOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	belongs, err := sessionBelongsToProject(globalSessionID, authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetSessionMcpServersOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	if !belongs {
		return &mcp.CallToolResult{IsError: true}, SetSessionMcpServersOutput{}, fmt.Errorf("session not found in current project")
	}

	if err := ensureProjectMcpBindingsForProject(authorizedProjectId); err != nil {
		return &mcp.CallToolResult{IsError: true}, SetSessionMcpServersOutput{}, fmt.Errorf("failed to bootstrap project MCP bindings: %v", err)
	}

	allowedNames, err := loadProjectAllowedMcpNames(authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetSessionMcpServersOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	normalizedNames := normalizeMcpServerNames(input.McpServerNames)
	disallowed := make([]string, 0)
	for _, name := range normalizedNames {
		if _, ok := allowedNames[name]; !ok {
			disallowed = append(disallowed, name)
		}
	}
	if len(disallowed) > 0 {
		sort.Strings(disallowed)
		return &mcp.CallToolResult{IsError: true}, SetSessionMcpServersOutput{}, fmt.Errorf("MCP server(s) not allowed for current project: %s", strings.Join(disallowed, ", "))
	}

	serverIds := make([]int, 0, len(normalizedNames))
	missing := make([]string, 0)
	for _, name := range normalizedNames {
		var serverId int
		err := db.QueryRow("SELECT id FROM mcp_server WHERE name = ?", name).Scan(&serverId)
		if err == sql.ErrNoRows {
			missing = append(missing, name)
			continue
		}
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, SetSessionMcpServersOutput{}, fmt.Errorf("DB query error: %v", err)
		}
		serverIds = append(serverIds, serverId)
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return &mcp.CallToolResult{IsError: true}, SetSessionMcpServersOutput{}, fmt.Errorf("unknown MCP server(s): %s", strings.Join(missing, ", "))
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetSessionMcpServersOutput{}, fmt.Errorf("DB transaction start error: %v", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if _, err := tx.Exec("DELETE FROM session_mcp_server WHERE session_id = ?", globalSessionID); err != nil {
		return &mcp.CallToolResult{IsError: true}, SetSessionMcpServersOutput{}, fmt.Errorf("DB delete error: %v", err)
	}

	for _, serverId := range serverIds {
		if _, err := tx.Exec("INSERT OR IGNORE INTO session_mcp_server (session_id, mcp_server_id) VALUES (?, ?)", globalSessionID, serverId); err != nil {
			return &mcp.CallToolResult{IsError: true}, SetSessionMcpServersOutput{}, fmt.Errorf("DB insert error: %v", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return &mcp.CallToolResult{IsError: true}, SetSessionMcpServersOutput{}, fmt.Errorf("DB commit error: %v", err)
	}

	return nil, SetSessionMcpServersOutput{
		Status:         "success",
		SessionId:      input.SessionId,
		McpServerNames: normalizedNames,
	}, nil
}

type GetSessionMcpServersInput struct {
	SessionId int `json:"session_id" jsonschema:"Session ID to read MCP assignments from"`
}

type GetSessionMcpServersOutput struct {
	SessionId      int               `json:"session_id"`
	McpServerNames []string          `json:"mcp_server_names"`
	Servers        []McpServerRecord `json:"servers"`
}

func GetSessionMcpServers(ctx context.Context, req *mcp.CallToolRequest, input GetSessionMcpServersInput) (*mcp.CallToolResult, GetSessionMcpServersOutput, error) {
	if authorizedRole != "fixer" && authorizedRole != "netrunner" {
		return &mcp.CallToolResult{IsError: true}, GetSessionMcpServersOutput{}, fmt.Errorf("access denied: requires authenticated role")
	}

	globalSessionID, err := globalSessionIDFromProjectScoped(input.SessionId, authorizedProjectId)
	if err == sql.ErrNoRows {
		return &mcp.CallToolResult{IsError: true}, GetSessionMcpServersOutput{}, fmt.Errorf("session not found in current project")
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetSessionMcpServersOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	belongs, err := sessionBelongsToProject(globalSessionID, authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetSessionMcpServersOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	if !belongs {
		return &mcp.CallToolResult{IsError: true}, GetSessionMcpServersOutput{}, fmt.Errorf("session not found in current project")
	}

	rows, err := db.Query(`
		SELECT s.id, s.name, COALESCE(s.short_description, ''), COALESCE(s.long_description, ''), COALESCE(s.auto_attach, 0), COALESCE(s.is_default, 0), COALESCE(s.category, ''), COALESCE(s.how_to, '')
		FROM session_mcp_server sms
		INNER JOIN mcp_server s ON s.id = sms.mcp_server_id
		WHERE sms.session_id = ?
		ORDER BY
			CASE COALESCE(s.category, '')
				WHEN 'DB' THEN 0
				WHEN 'Web-search' THEN 1
				WHEN 'Design' THEN 2
				WHEN 'Productivity' THEN 3
				WHEN 'Coding' THEN 4
				ELSE 99
			END,
			COALESCE(s.category, ''),
			s.name`, globalSessionID)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetSessionMcpServersOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	defer rows.Close()

	servers := []McpServerRecord{}
	names := []string{}
	for rows.Next() {
		var item McpServerRecord
		var autoAttach int
		var isDefault int
		if err := rows.Scan(&item.Id, &item.Name, &item.ShortDescription, &item.LongDescription, &autoAttach, &isDefault, &item.Category, &item.HowTo); err != nil {
			return &mcp.CallToolResult{IsError: true}, GetSessionMcpServersOutput{}, fmt.Errorf("DB scan error: %v", err)
		}
		item.AutoAttach = autoAttach == 1
		item.IsDefault = isDefault == 1
		servers = append(servers, item)
		names = append(names, item.Name)
	}

	return nil, GetSessionMcpServersOutput{
		SessionId:      input.SessionId,
		McpServerNames: names,
		Servers:        servers,
	}, nil
}

type CheckCurrentProjectDocsInput struct{}

type ProjectDocSummary struct {
	DocId   int    `json:"doc_id"`
	Title   string `json:"title"`
	DocType string `json:"doc_type"`
	Summary string `json:"summary"`
}

type CheckCurrentProjectDocsOutput struct {
	Docs []ProjectDocSummary `json:"docs"`
}

func CheckCurrentProjectDocs(ctx context.Context, req *mcp.CallToolRequest, input CheckCurrentProjectDocsInput) (*mcp.CallToolResult, CheckCurrentProjectDocsOutput, error) {
	if authorizedRole != "fixer" {
		return &mcp.CallToolResult{IsError: true}, CheckCurrentProjectDocsOutput{}, fmt.Errorf("access denied: requires fixer role")
	}

	rows, err := db.Query(`
		SELECT
			(
				SELECT COUNT(*)
				FROM project_doc d2
				WHERE d2.project_id = d.project_id AND d2.id <= d.id
			) AS local_doc_id,
			d.title,
			d.content,
			COALESCE(d.doc_type, 'documentation')
		FROM project_doc d
		WHERE d.project_id = ?
		ORDER BY d.id`, authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CheckCurrentProjectDocsOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	defer rows.Close()

	docs := []ProjectDocSummary{}
	for rows.Next() {
		var item ProjectDocSummary
		var content string
		if err := rows.Scan(&item.DocId, &item.Title, &content, &item.DocType); err != nil {
			return &mcp.CallToolResult{IsError: true}, CheckCurrentProjectDocsOutput{}, fmt.Errorf("DB scan error: %v", err)
		}
		item.Summary = summarizeDocContent(content)
		docs = append(docs, item)
	}

	return nil, CheckCurrentProjectDocsOutput{Docs: docs}, nil
}

type SetSessionAttachedDocsInput struct {
	SessionId     int   `json:"session_id" jsonschema:"Session ID to attach docs for"`
	ProjectDocIds []int `json:"project_doc_ids" jsonschema:"Array of project_doc IDs to attach"`
}

type SetSessionAttachedDocsOutput struct {
	Status        string `json:"status"`
	SessionId     int    `json:"session_id"`
	ProjectDocIds []int  `json:"project_doc_ids"`
}

func SetSessionAttachedDocs(ctx context.Context, req *mcp.CallToolRequest, input SetSessionAttachedDocsInput) (*mcp.CallToolResult, SetSessionAttachedDocsOutput, error) {
	if authorizedRole != "fixer" {
		return &mcp.CallToolResult{IsError: true}, SetSessionAttachedDocsOutput{}, fmt.Errorf("access denied: requires fixer role")
	}

	globalSessionID, err := globalSessionIDFromProjectScoped(input.SessionId, authorizedProjectId)
	if err == sql.ErrNoRows {
		return &mcp.CallToolResult{IsError: true}, SetSessionAttachedDocsOutput{}, fmt.Errorf("session not found in current project")
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetSessionAttachedDocsOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	belongs, err := sessionBelongsToProject(globalSessionID, authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetSessionAttachedDocsOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	if !belongs {
		return &mcp.CallToolResult{IsError: true}, SetSessionAttachedDocsOutput{}, fmt.Errorf("session not found in current project")
	}

	normalizedLocalDocIds := normalizeDocIDs(input.ProjectDocIds)
	normalizedGlobalDocIds := make([]int, 0, len(normalizedLocalDocIds))
	missing := make([]string, 0)
	for _, localDocID := range normalizedLocalDocIds {
		globalDocID, err := globalProjectDocIDFromProjectScoped(localDocID, authorizedProjectId)
		if err == sql.ErrNoRows {
			missing = append(missing, fmt.Sprintf("%d", localDocID))
			continue
		}
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, SetSessionAttachedDocsOutput{}, fmt.Errorf("DB query error: %v", err)
		}
		docBelongs, err := projectDocBelongsToProject(globalDocID, authorizedProjectId)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, SetSessionAttachedDocsOutput{}, fmt.Errorf("DB query error: %v", err)
		}
		if !docBelongs {
			missing = append(missing, fmt.Sprintf("%d", localDocID))
			continue
		}
		normalizedGlobalDocIds = append(normalizedGlobalDocIds, globalDocID)
	}
	if len(missing) > 0 {
		return &mcp.CallToolResult{IsError: true}, SetSessionAttachedDocsOutput{}, fmt.Errorf("unknown project_doc_id(s): %s", strings.Join(missing, ", "))
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetSessionAttachedDocsOutput{}, fmt.Errorf("DB transaction start error: %v", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if _, err := tx.Exec("DELETE FROM netrunner_attached_doc WHERE session_id = ?", globalSessionID); err != nil {
		return &mcp.CallToolResult{IsError: true}, SetSessionAttachedDocsOutput{}, fmt.Errorf("DB delete error: %v", err)
	}

	for _, docId := range normalizedGlobalDocIds {
		if _, err := tx.Exec("INSERT OR IGNORE INTO netrunner_attached_doc (session_id, project_doc_id) VALUES (?, ?)", globalSessionID, docId); err != nil {
			return &mcp.CallToolResult{IsError: true}, SetSessionAttachedDocsOutput{}, fmt.Errorf("DB insert error: %v", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return &mcp.CallToolResult{IsError: true}, SetSessionAttachedDocsOutput{}, fmt.Errorf("DB commit error: %v", err)
	}

	return nil, SetSessionAttachedDocsOutput{
		Status:        "success",
		SessionId:     input.SessionId,
		ProjectDocIds: normalizedLocalDocIds,
	}, nil
}

type GetSessionAttachedDocsInput struct {
	SessionId int `json:"session_id" jsonschema:"Session ID to read attached document metadata from"`
}

type GetSessionAttachedDocsOutput struct {
	SessionId     int                 `json:"session_id"`
	ProjectDocIds []int               `json:"project_doc_ids"`
	Docs          []ProjectDocSummary `json:"docs"`
}

func GetSessionAttachedDocs(ctx context.Context, req *mcp.CallToolRequest, input GetSessionAttachedDocsInput) (*mcp.CallToolResult, GetSessionAttachedDocsOutput, error) {
	if authorizedRole != "fixer" && authorizedRole != "netrunner" {
		return &mcp.CallToolResult{IsError: true}, GetSessionAttachedDocsOutput{}, fmt.Errorf("access denied: requires authenticated role")
	}

	globalSessionID, err := globalSessionIDFromProjectScoped(input.SessionId, authorizedProjectId)
	if err == sql.ErrNoRows {
		return &mcp.CallToolResult{IsError: true}, GetSessionAttachedDocsOutput{}, fmt.Errorf("session not found in current project")
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetSessionAttachedDocsOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	belongs, err := sessionBelongsToProject(globalSessionID, authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetSessionAttachedDocsOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	if !belongs {
		return &mcp.CallToolResult{IsError: true}, GetSessionAttachedDocsOutput{}, fmt.Errorf("session not found in current project")
	}

	rows, err := db.Query(`
		SELECT
			(
				SELECT COUNT(*)
				FROM project_doc d2
				WHERE d2.project_id = d.project_id AND d2.id <= d.id
			) AS local_doc_id,
			d.title,
			COALESCE(d.doc_type, 'documentation'),
			d.content
		FROM netrunner_attached_doc ad
		INNER JOIN project_doc d ON d.id = ad.project_doc_id
		WHERE ad.session_id = ? AND d.project_id = ?
		ORDER BY d.id`, globalSessionID, authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetSessionAttachedDocsOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	defer rows.Close()

	docs := []ProjectDocSummary{}
	docIds := []int{}
	for rows.Next() {
		var item ProjectDocSummary
		var content string
		if err := rows.Scan(&item.DocId, &item.Title, &item.DocType, &content); err != nil {
			return &mcp.CallToolResult{IsError: true}, GetSessionAttachedDocsOutput{}, fmt.Errorf("DB scan error: %v", err)
		}
		item.Summary = summarizeDocContent(content)
		docs = append(docs, item)
		docIds = append(docIds, item.DocId)
	}

	return nil, GetSessionAttachedDocsOutput{
		SessionId:     input.SessionId,
		ProjectDocIds: docIds,
		Docs:          docs,
	}, nil
}

type GetAttachedProjectDocsInput struct {
	SessionId int `json:"session_id" jsonschema:"Session ID to read attached project docs from. If omitted for netrunner, current checked-out session is used."`
}

type GetAttachedProjectDocsOutput struct {
	SessionId int          `json:"session_id"`
	Docs      []ProjectDoc `json:"docs"`
}

func GetAttachedProjectDocs(ctx context.Context, req *mcp.CallToolRequest, input GetAttachedProjectDocsInput) (*mcp.CallToolResult, GetAttachedProjectDocsOutput, error) {
	if authorizedRole != "fixer" && authorizedRole != "netrunner" {
		return &mcp.CallToolResult{IsError: true}, GetAttachedProjectDocsOutput{}, fmt.Errorf("access denied: requires authenticated role")
	}

	localSessionID := input.SessionId
	if localSessionID == 0 {
		if authorizedRole == "netrunner" && authorizedSessionId != 0 {
			mappedSessionID, mapErr := projectScopedSessionIDFromGlobal(authorizedSessionId, authorizedProjectId)
			if mapErr != nil {
				return &mcp.CallToolResult{IsError: true}, GetAttachedProjectDocsOutput{}, fmt.Errorf("DB mapping error: %v", mapErr)
			}
			localSessionID = mappedSessionID
		} else {
			return &mcp.CallToolResult{IsError: true}, GetAttachedProjectDocsOutput{}, fmt.Errorf("session_id is required")
		}
	}

	globalSessionID, err := globalSessionIDFromProjectScoped(localSessionID, authorizedProjectId)
	if err == sql.ErrNoRows {
		return &mcp.CallToolResult{IsError: true}, GetAttachedProjectDocsOutput{}, fmt.Errorf("session not found in current project")
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetAttachedProjectDocsOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	belongs, err := sessionBelongsToProject(globalSessionID, authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetAttachedProjectDocsOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	if !belongs {
		return &mcp.CallToolResult{IsError: true}, GetAttachedProjectDocsOutput{}, fmt.Errorf("session not found in current project")
	}

	rows, err := db.Query(`
		SELECT
			(
				SELECT COUNT(*)
				FROM project_doc d2
				WHERE d2.project_id = d.project_id AND d2.id <= d.id
			) AS local_doc_id,
			d.title,
			d.content,
			COALESCE(d.doc_type, 'documentation')
		FROM netrunner_attached_doc ad
		INNER JOIN project_doc d ON d.id = ad.project_doc_id
		WHERE ad.session_id = ? AND d.project_id = ?
		ORDER BY d.id`, globalSessionID, authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetAttachedProjectDocsOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	defer rows.Close()

	docs := []ProjectDoc{}
	for rows.Next() {
		var item ProjectDoc
		if err := rows.Scan(&item.Id, &item.Title, &item.Content, &item.DocType); err != nil {
			return &mcp.CallToolResult{IsError: true}, GetAttachedProjectDocsOutput{}, fmt.Errorf("DB scan error: %v", err)
		}
		docs = append(docs, item)
	}

	return nil, GetAttachedProjectDocsOutput{
		SessionId: localSessionID,
		Docs:      docs,
	}, nil
}

type GetProjectsInput struct{}

type GetProjectsOutput struct {
	Projects []string `json:"projects"`
}

func GetProjects(ctx context.Context, req *mcp.CallToolRequest, input GetProjectsInput) (*mcp.CallToolResult, GetProjectsOutput, error) {
	if authorizedRole != "overseer" {
		return &mcp.CallToolResult{IsError: true}, GetProjectsOutput{}, fmt.Errorf("access denied: requires overseer role. current role: %s", authorizedRole)
	}

	rows, err := db.Query("SELECT name FROM project")
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetProjectsOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	var projects []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return &mcp.CallToolResult{IsError: true}, GetProjectsOutput{}, fmt.Errorf("DB scan error: %v", err)
		}
		projects = append(projects, name)
	}
	if err := rows.Err(); err != nil {
		return &mcp.CallToolResult{IsError: true}, GetProjectsOutput{}, fmt.Errorf("DB rows error: %v", err)
	}

	if projects == nil {
		projects = []string{}
	}

	return nil, GetProjectsOutput{Projects: projects}, nil
}

type RegisterProjectInput struct {
	Cwd  string `json:"cwd" jsonschema:"required absolute path"`
	Name string `json:"name,omitempty" jsonschema:"optional; default basename(cwd)"`
}

type RegisterProjectOutput struct {
	ProjectId int    `json:"project_id"`
	Status    string `json:"status"`
	Name      string `json:"name"`
	Cwd       string `json:"cwd"`
}

func RegisterProject(ctx context.Context, req *mcp.CallToolRequest, input RegisterProjectInput) (*mcp.CallToolResult, RegisterProjectOutput, error) {
	if authorizedRole != "overseer" {
		return &mcp.CallToolResult{IsError: true}, RegisterProjectOutput{}, fmt.Errorf("access denied: requires overseer role")
	}

	normalizedCWD, err := normalizeProjectCWD(input.Cwd)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, RegisterProjectOutput{}, fmt.Errorf("invalid cwd: %v", err)
	}

	info, err := os.Stat(normalizedCWD)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, RegisterProjectOutput{}, fmt.Errorf("invalid cwd: path does not exist")
	}
	if !info.IsDir() {
		return &mcp.CallToolResult{IsError: true}, RegisterProjectOutput{}, fmt.Errorf("invalid cwd: path is not a directory")
	}

	requestedName := strings.TrimSpace(input.Name)
	if requestedName == "" {
		requestedName = defaultProjectName(normalizedCWD)
	}

	res, err := db.Exec(
		`INSERT INTO project (name, cwd)
		 SELECT ?, ?
		 WHERE NOT EXISTS (SELECT 1 FROM project WHERE cwd = ?)`,
		requestedName,
		normalizedCWD,
		normalizedCWD,
	)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, RegisterProjectOutput{}, fmt.Errorf("DB insert error: %v", err)
	}

	status := "exists"
	if rowsAffected, rowsErr := res.RowsAffected(); rowsErr == nil && rowsAffected > 0 {
		status = "created"
	}

	var projectID int
	var storedName string
	var storedCWD string
	err = db.QueryRow(
		"SELECT id, name, cwd FROM project WHERE cwd = ?",
		normalizedCWD,
	).Scan(&projectID, &storedName, &storedCWD)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, RegisterProjectOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	return nil, RegisterProjectOutput{
		ProjectId: projectID,
		Status:    status,
		Name:      storedName,
		Cwd:       storedCWD,
	}, nil
}

type CreateTaskInput struct {
	TaskDescription    string   `json:"task_description" jsonschema:"Description of the task to be created"`
	DeclaredWriteScope []string `json:"declared_write_scope,omitempty" jsonschema:"Optional declared project-relative write scope for the session. Defaults to the whole project to preserve serial execution."`
	ParallelWaveID     string   `json:"parallel_wave_id,omitempty" jsonschema:"Optional explicit wave identifier for sessions that are pre-approved to run in the same parallel launch wave."`
}

type CreateTaskOutput struct {
	SessionId int    `json:"session_id"`
	Status    string `json:"status"`
}

func CreateTask(ctx context.Context, req *mcp.CallToolRequest, input CreateTaskInput) (*mcp.CallToolResult, CreateTaskOutput, error) {
	if authorizedRole != "fixer" {
		return &mcp.CallToolResult{IsError: true}, CreateTaskOutput{}, fmt.Errorf("access denied: requires Fixer role")
	}

	declaredWriteScope, err := encodeDeclaredWriteScope(input.DeclaredWriteScope)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CreateTaskOutput{}, err
	}
	parallelWaveID := strings.TrimSpace(input.ParallelWaveID)

	res, err := db.Exec(
		"INSERT INTO session (project_id, task_description, status, declared_write_scope, parallel_wave_id) VALUES (?, ?, 'pending', ?, ?)",
		authorizedProjectId,
		input.TaskDescription,
		declaredWriteScope,
		parallelWaveID,
	)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CreateTaskOutput{}, fmt.Errorf("DB insert error: %v", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CreateTaskOutput{}, fmt.Errorf("LastInsertId error: %v", err)
	}

	localSessionID, err := projectScopedSessionIDFromGlobal(int(id), authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CreateTaskOutput{}, fmt.Errorf("DB mapping error: %v", err)
	}

	return nil, CreateTaskOutput{SessionId: localSessionID, Status: "success"}, nil
}

type ProposeDocUpdateInput struct {
	ProposedContent    string `json:"proposed_content" jsonschema:"The content to propose for the document"`
	ProposedDocType    string `json:"proposed_doc_type,omitempty" jsonschema:"The type to propose for the document"`
	TargetProjectDocId int    `json:"target_project_doc_id,omitempty" jsonschema:"Optional project-scoped target doc ID when the proposal should update one existing document"`
}

type ProposeDocUpdateOutput struct {
	ProposalId int    `json:"proposal_id"`
	Status     string `json:"status"`
}

func ProposeDocUpdate(ctx context.Context, req *mcp.CallToolRequest, input ProposeDocUpdateInput) (*mcp.CallToolResult, ProposeDocUpdateOutput, error) {
	if authorizedRole != "netrunner" {
		return &mcp.CallToolResult{IsError: true}, ProposeDocUpdateOutput{}, fmt.Errorf("access denied: requires netrunner role")
	}

	docType := input.ProposedDocType
	if docType == "" {
		docType = "documentation"
	}

	var targetProjectDocID any
	if input.TargetProjectDocId != 0 {
		globalDocID, err := globalProjectDocIDFromProjectScoped(input.TargetProjectDocId, authorizedProjectId)
		if err == sql.ErrNoRows {
			return &mcp.CallToolResult{IsError: true}, ProposeDocUpdateOutput{}, fmt.Errorf("target_project_doc_id not found in current project")
		}
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, ProposeDocUpdateOutput{}, fmt.Errorf("failed to resolve target_project_doc_id: %v", err)
		}
		targetProjectDocID = globalDocID
	}

	res, err := db.Exec(
		"INSERT INTO doc_proposal (project_id, session_id, status, proposed_content, proposed_doc_type, target_project_doc_id) VALUES (?, ?, 'pending', ?, ?, ?)",
		authorizedProjectId,
		authorizedSessionId,
		input.ProposedContent,
		docType,
		targetProjectDocID,
	)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, ProposeDocUpdateOutput{}, fmt.Errorf("DB insert error: %v", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, ProposeDocUpdateOutput{}, fmt.Errorf("LastInsertId error: %v", err)
	}

	localProposalID, err := projectScopedDocProposalIDFromGlobal(int(id), authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, ProposeDocUpdateOutput{}, fmt.Errorf("DB mapping error: %v", err)
	}

	return nil, ProposeDocUpdateOutput{ProposalId: localProposalID, Status: "success"}, nil
}

type ReviewDocProposalsInput struct{}

type DocProposal struct {
	Id                 int    `json:"id"`
	SessionId          int    `json:"session_id"`
	ProposedContent    string `json:"proposed_content"`
	ProposedDocType    string `json:"proposed_doc_type"`
	TargetProjectDocId int    `json:"target_project_doc_id,omitempty"`
}

type ReviewDocProposalsOutput struct {
	Proposals []DocProposal `json:"proposals"`
}

func ReviewDocProposals(ctx context.Context, req *mcp.CallToolRequest, input ReviewDocProposalsInput) (*mcp.CallToolResult, ReviewDocProposalsOutput, error) {
	if authorizedRole != "fixer" {
		return &mcp.CallToolResult{IsError: true}, ReviewDocProposalsOutput{}, fmt.Errorf("access denied: requires fixer role")
	}

	rows, err := db.Query(`
		SELECT
			(
				SELECT COUNT(*)
				FROM doc_proposal p2
				WHERE p2.project_id = p.project_id AND p2.id <= p.id
			) AS local_proposal_id,
			(
				SELECT COUNT(*)
				FROM session s2
				WHERE s2.project_id = p.project_id AND s2.id <= p.session_id
			) AS local_session_id,
			p.proposed_content,
			COALESCE(p.proposed_doc_type, 'documentation'),
			(
				SELECT (
					SELECT COUNT(*)
					FROM project_doc d2
					WHERE d2.project_id = p.project_id AND d2.id <= target.id
				)
				FROM project_doc target
				WHERE target.id = p.target_project_doc_id AND target.project_id = p.project_id
			)
		FROM doc_proposal p
		WHERE p.project_id = ? AND p.status = 'pending'
		ORDER BY p.id`, authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, ReviewDocProposalsOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	defer rows.Close()

	var proposals []DocProposal
	for rows.Next() {
		var p DocProposal
		var targetProjectDocID sql.NullInt64
		if err := rows.Scan(&p.Id, &p.SessionId, &p.ProposedContent, &p.ProposedDocType, &targetProjectDocID); err != nil {
			return &mcp.CallToolResult{IsError: true}, ReviewDocProposalsOutput{}, fmt.Errorf("DB scan error: %v", err)
		}
		if targetProjectDocID.Valid {
			p.TargetProjectDocId = int(targetProjectDocID.Int64)
		}
		proposals = append(proposals, p)
	}
	if proposals == nil {
		proposals = []DocProposal{}
	}

	return nil, ReviewDocProposalsOutput{Proposals: proposals}, nil
}

type SetDocProposalStatusInput struct {
	ProposalId int    `json:"proposal_id" jsonschema:"The ID of the proposal to update"`
	Status     string `json:"status" jsonschema:"New status: 'approved' or 'rejected'"`
}

type SetDocProposalStatusOutput struct {
	Status string `json:"status"`
}

func SetDocProposalStatus(ctx context.Context, req *mcp.CallToolRequest, input SetDocProposalStatusInput) (*mcp.CallToolResult, SetDocProposalStatusOutput, error) {
	if authorizedRole != "fixer" {
		return &mcp.CallToolResult{IsError: true}, SetDocProposalStatusOutput{}, fmt.Errorf("access denied: requires fixer role")
	}
	if input.Status != "approved" && input.Status != "rejected" {
		return &mcp.CallToolResult{IsError: true}, SetDocProposalStatusOutput{}, fmt.Errorf("invalid status: must be 'approved' or 'rejected'")
	}

	globalProposalID, err := globalDocProposalIDFromProjectScoped(input.ProposalId, authorizedProjectId)
	if err == sql.ErrNoRows {
		return &mcp.CallToolResult{IsError: true}, SetDocProposalStatusOutput{}, fmt.Errorf("failed to fetch proposal: %v", sql.ErrNoRows)
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetDocProposalStatusOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	if input.Status == "approved" {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, SetDocProposalStatusOutput{}, fmt.Errorf("failed to begin approval transaction: %v", err)
		}
		defer func() {
			_ = tx.Rollback()
		}()

		var proposedContent, proposedDocType string
		var targetProjectDocID sql.NullInt64
		err = tx.QueryRow(
			"SELECT proposed_content, COALESCE(proposed_doc_type, 'documentation'), target_project_doc_id FROM doc_proposal WHERE id = ? AND project_id = ?",
			globalProposalID,
			authorizedProjectId,
		).Scan(&proposedContent, &proposedDocType, &targetProjectDocID)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, SetDocProposalStatusOutput{}, fmt.Errorf("failed to fetch proposal: %v", err)
		}

		if targetProjectDocID.Valid {
			res, err := tx.Exec(
				"UPDATE project_doc SET content = ?, doc_type = ? WHERE id = ? AND project_id = ?",
				proposedContent,
				proposedDocType,
				targetProjectDocID.Int64,
				authorizedProjectId,
			)
			if err != nil {
				return &mcp.CallToolResult{IsError: true}, SetDocProposalStatusOutput{}, fmt.Errorf("failed to update targeted project_doc: %v", err)
			}
			rowsAffected, err := res.RowsAffected()
			if err != nil {
				return &mcp.CallToolResult{IsError: true}, SetDocProposalStatusOutput{}, fmt.Errorf("failed to confirm targeted project_doc update: %v", err)
			}
			if rowsAffected == 0 {
				return &mcp.CallToolResult{IsError: true}, SetDocProposalStatusOutput{}, fmt.Errorf("target_project_doc_id no longer exists in current project")
			}
		} else {
			rows, err := tx.Query(
				"SELECT id FROM project_doc WHERE project_id = ? AND COALESCE(doc_type, 'documentation') = ? ORDER BY id LIMIT 2",
				authorizedProjectId,
				proposedDocType,
			)
			if err != nil {
				return &mcp.CallToolResult{IsError: true}, SetDocProposalStatusOutput{}, fmt.Errorf("failed to resolve proposal target: %v", err)
			}
			defer func() {
				_ = rows.Close()
			}()

			matchingDocIDs := make([]int, 0, 2)
			for rows.Next() {
				var docID int
				if err := rows.Scan(&docID); err != nil {
					return &mcp.CallToolResult{IsError: true}, SetDocProposalStatusOutput{}, fmt.Errorf("failed to scan proposal target: %v", err)
				}
				matchingDocIDs = append(matchingDocIDs, docID)
			}
			if err := rows.Err(); err != nil {
				return &mcp.CallToolResult{IsError: true}, SetDocProposalStatusOutput{}, fmt.Errorf("failed to read proposal targets: %v", err)
			}

			switch len(matchingDocIDs) {
			case 0:
				_, err = tx.Exec(
					"INSERT INTO project_doc (project_id, title, content, doc_type) VALUES (?, ?, ?, ?)",
					authorizedProjectId,
					"Documentation ("+proposedDocType+")",
					proposedContent,
					proposedDocType,
				)
				if err != nil {
					return &mcp.CallToolResult{IsError: true}, SetDocProposalStatusOutput{}, fmt.Errorf("failed to insert project_doc: %v", err)
				}
			case 1:
				if _, err := tx.Exec(
					"UPDATE project_doc SET content = ?, doc_type = ? WHERE id = ? AND project_id = ?",
					proposedContent,
					proposedDocType,
					matchingDocIDs[0],
					authorizedProjectId,
				); err != nil {
					return &mcp.CallToolResult{IsError: true}, SetDocProposalStatusOutput{}, fmt.Errorf("failed to update project_doc: %v", err)
				}
			default:
				return &mcp.CallToolResult{IsError: true}, SetDocProposalStatusOutput{}, fmt.Errorf(
					"proposal %d approval is ambiguous for doc_type %q; resubmit with target_project_doc_id",
					input.ProposalId,
					proposedDocType,
				)
			}
		}

		_, err = tx.Exec("UPDATE doc_proposal SET status = ? WHERE id = ? AND project_id = ?", input.Status, globalProposalID, authorizedProjectId)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, SetDocProposalStatusOutput{}, fmt.Errorf("DB update error: %v", err)
		}

		if err := tx.Commit(); err != nil {
			return &mcp.CallToolResult{IsError: true}, SetDocProposalStatusOutput{}, fmt.Errorf("failed to commit approval transaction: %v", err)
		}

		return nil, SetDocProposalStatusOutput{Status: "success"}, nil
	}

	_, err = db.Exec("UPDATE doc_proposal SET status = ? WHERE id = ? AND project_id = ?", input.Status, globalProposalID, authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetDocProposalStatusOutput{}, fmt.Errorf("DB update error: %v", err)
	}

	return nil, SetDocProposalStatusOutput{Status: "success"}, nil
}

type GetProjectDocsInput struct{}

type ProjectDoc struct {
	Id      int    `json:"id"`
	Title   string `json:"title"`
	Content string `json:"content"`
	DocType string `json:"doc_type"`
}

type GetProjectDocsOutput struct {
	Docs []ProjectDoc `json:"docs"`
}

func GetProjectDocs(ctx context.Context, req *mcp.CallToolRequest, input GetProjectDocsInput) (*mcp.CallToolResult, GetProjectDocsOutput, error) {
	if authorizedRole != "fixer" && authorizedRole != "netrunner" {
		return &mcp.CallToolResult{IsError: true}, GetProjectDocsOutput{}, fmt.Errorf("access denied: requires authenticated role")
	}

	rows, err := db.Query(`
		SELECT
			(
				SELECT COUNT(*)
				FROM project_doc d2
				WHERE d2.project_id = d.project_id AND d2.id <= d.id
			) AS local_doc_id,
			d.title,
			d.content,
			COALESCE(d.doc_type, 'documentation')
		FROM project_doc d
		WHERE d.project_id = ?
		ORDER BY d.id`, authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetProjectDocsOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	defer rows.Close()

	var docs []ProjectDoc
	for rows.Next() {
		var d ProjectDoc
		if err := rows.Scan(&d.Id, &d.Title, &d.Content, &d.DocType); err != nil {
			return &mcp.CallToolResult{IsError: true}, GetProjectDocsOutput{}, fmt.Errorf("DB scan error: %v", err)
		}
		docs = append(docs, d)
	}
	if docs == nil {
		docs = []ProjectDoc{}
	}

	return nil, GetProjectDocsOutput{Docs: docs}, nil
}

type AddProjectDocInput struct {
	Title   string `json:"title" jsonschema:"The title of the new document"`
	Content string `json:"content" jsonschema:"The content of the new document"`
	DocType string `json:"doc_type,omitempty" jsonschema:"The type of document, e.g. 'documentation', 'architecture', etc."`
}

type AddProjectDocOutput struct {
	Id     int    `json:"id"`
	Status string `json:"status"`
}

func AddProjectDoc(ctx context.Context, req *mcp.CallToolRequest, input AddProjectDocInput) (*mcp.CallToolResult, AddProjectDocOutput, error) {
	if authorizedRole != "fixer" {
		return &mcp.CallToolResult{IsError: true}, AddProjectDocOutput{}, fmt.Errorf("access denied: requires fixer role")
	}

	docType := input.DocType
	if docType == "" {
		docType = "documentation"
	}

	res, err := db.Exec("INSERT INTO project_doc (project_id, title, content, doc_type) VALUES (?, ?, ?, ?)", authorizedProjectId, input.Title, input.Content, docType)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, AddProjectDocOutput{}, fmt.Errorf("DB insert error: %v", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, AddProjectDocOutput{}, fmt.Errorf("LastInsertId error: %v", err)
	}

	localDocID, err := projectScopedDocIDFromGlobal(int(id), authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, AddProjectDocOutput{}, fmt.Errorf("DB mapping error: %v", err)
	}

	return nil, AddProjectDocOutput{Id: localDocID, Status: "success"}, nil
}

type UpdateProjectDocInput struct {
	DocId   int    `json:"doc_id" jsonschema:"The ID of the document to update"`
	Content string `json:"content" jsonschema:"The new content of the document"`
	DocType string `json:"doc_type,omitempty" jsonschema:"Optionally update the doc type"`
}

type UpdateProjectDocOutput struct {
	Status string `json:"status"`
}

func UpdateProjectDoc(ctx context.Context, req *mcp.CallToolRequest, input UpdateProjectDocInput) (*mcp.CallToolResult, UpdateProjectDocOutput, error) {
	if authorizedRole != "fixer" {
		return &mcp.CallToolResult{IsError: true}, UpdateProjectDocOutput{}, fmt.Errorf("access denied: requires fixer role")
	}

	globalDocID, err := globalProjectDocIDFromProjectScoped(input.DocId, authorizedProjectId)
	if err == sql.ErrNoRows {
		return &mcp.CallToolResult{IsError: true}, UpdateProjectDocOutput{}, fmt.Errorf("document not found or not belonging to project")
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, UpdateProjectDocOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	var res sql.Result
	if input.DocType != "" {
		res, err = db.Exec("UPDATE project_doc SET content = ?, doc_type = ? WHERE id = ? AND project_id = ?", input.Content, input.DocType, globalDocID, authorizedProjectId)
	} else {
		res, err = db.Exec("UPDATE project_doc SET content = ? WHERE id = ? AND project_id = ?", input.Content, globalDocID, authorizedProjectId)
	}

	if err != nil {
		return &mcp.CallToolResult{IsError: true}, UpdateProjectDocOutput{}, fmt.Errorf("DB update error: %v", err)
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, UpdateProjectDocOutput{}, fmt.Errorf("RowsAffected error: %v", err)
	}

	if rowsAffected == 0 {
		return &mcp.CallToolResult{IsError: true}, UpdateProjectDocOutput{}, fmt.Errorf("document not found or not belonging to project")
	}

	return nil, UpdateProjectDocOutput{Status: "success"}, nil
}

type DeleteProjectDocInput struct {
	DocId int `json:"doc_id" jsonschema:"The ID of the document to delete"`
}

type DeleteProjectDocOutput struct {
	Status string `json:"status"`
}

func DeleteProjectDoc(ctx context.Context, req *mcp.CallToolRequest, input DeleteProjectDocInput) (*mcp.CallToolResult, DeleteProjectDocOutput, error) {
	if authorizedRole != "fixer" {
		return &mcp.CallToolResult{IsError: true}, DeleteProjectDocOutput{}, fmt.Errorf("access denied: requires fixer role")
	}

	globalDocID, err := globalProjectDocIDFromProjectScoped(input.DocId, authorizedProjectId)
	if err == sql.ErrNoRows {
		return &mcp.CallToolResult{IsError: true}, DeleteProjectDocOutput{}, fmt.Errorf("document not found or not belonging to project")
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, DeleteProjectDocOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	res, err := db.Exec("DELETE FROM project_doc WHERE id = ? AND project_id = ?", globalDocID, authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, DeleteProjectDocOutput{}, fmt.Errorf("DB delete error: %v", err)
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, DeleteProjectDocOutput{}, fmt.Errorf("RowsAffected error: %v", err)
	}

	if rowsAffected == 0 {
		return &mcp.CallToolResult{IsError: true}, DeleteProjectDocOutput{}, fmt.Errorf("document not found or not belonging to project")
	}

	return nil, DeleteProjectDocOutput{Status: "success"}, nil
}

type CompleteTaskInput struct {
	SessionId   int    `json:"session_id" jsonschema:"The ID of the session to complete"`
	FinalReport string `json:"final_report" jsonschema:"The final report for the task"`
}

type CompleteTaskOutput struct {
	Status string `json:"status"`
}

type SessionCleanupClaims struct {
	RemovedPaths         []string `json:"removed_paths,omitempty"`
	ExpectedPresentPaths []string `json:"expected_present_paths,omitempty"`
}

type SessionFinalReport struct {
	FilesChanged  []string             `json:"files_changed"`
	CommandsRun   []string             `json:"commands_run"`
	ChecksRun     []string             `json:"checks_run"`
	Blockers      []string             `json:"blockers"`
	ResidualRisks []string             `json:"residual_risks,omitempty"`
	CleanupClaims SessionCleanupClaims `json:"cleanup_claims,omitempty"`
}

func normalizeStringList(raw []string) []string {
	values := make([]string, 0, len(raw))
	for _, entry := range raw {
		normalized := strings.TrimSpace(entry)
		if normalized == "" {
			continue
		}
		values = append(values, normalized)
	}
	return values
}

func decodeStructuredFinalReport(raw string) (SessionFinalReport, string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return SessionFinalReport{}, "", fmt.Errorf("final_report is required and must be a non-empty JSON object")
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return SessionFinalReport{}, "", fmt.Errorf("final_report must be valid JSON matching the structured session report schema: %v", err)
	}

	requiredFields := []string{"files_changed", "commands_run", "checks_run", "blockers"}
	for _, field := range requiredFields {
		if _, exists := payload[field]; !exists {
			return SessionFinalReport{}, "", fmt.Errorf("final_report is missing required field %q", field)
		}
	}

	var report SessionFinalReport
	if err := json.Unmarshal([]byte(trimmed), &report); err != nil {
		return SessionFinalReport{}, "", fmt.Errorf("final_report schema decode failed: %v", err)
	}

	report.FilesChanged = normalizeStringList(report.FilesChanged)
	report.CommandsRun = normalizeStringList(report.CommandsRun)
	report.ChecksRun = normalizeStringList(report.ChecksRun)
	report.Blockers = normalizeStringList(report.Blockers)
	report.ResidualRisks = normalizeStringList(report.ResidualRisks)
	report.CleanupClaims.RemovedPaths = normalizeStringList(report.CleanupClaims.RemovedPaths)
	report.CleanupClaims.ExpectedPresentPaths = normalizeStringList(report.CleanupClaims.ExpectedPresentPaths)

	if len(report.FilesChanged) == 0 {
		return SessionFinalReport{}, "", fmt.Errorf("final_report.files_changed must list at least one changed path")
	}
	if len(report.CommandsRun) == 0 {
		return SessionFinalReport{}, "", fmt.Errorf("final_report.commands_run must list at least one command")
	}
	if len(report.ChecksRun) == 0 {
		return SessionFinalReport{}, "", fmt.Errorf("final_report.checks_run must list at least one verification step")
	}

	normalizedPayload, err := json.Marshal(report)
	if err != nil {
		return SessionFinalReport{}, "", fmt.Errorf("failed to normalize final_report: %v", err)
	}
	return report, string(normalizedPayload), nil
}

func CompleteTask(ctx context.Context, req *mcp.CallToolRequest, input CompleteTaskInput) (*mcp.CallToolResult, CompleteTaskOutput, error) {
	if authorizedRole != "netrunner" {
		return &mcp.CallToolResult{IsError: true}, CompleteTaskOutput{}, fmt.Errorf("access denied: requires netrunner role")
	}

	globalSessionID, err := globalSessionIDFromProjectScoped(input.SessionId, authorizedProjectId)
	if err == sql.ErrNoRows {
		return &mcp.CallToolResult{IsError: true}, CompleteTaskOutput{}, fmt.Errorf("session not found in current project")
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CompleteTaskOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	belongs, err := sessionBelongsToProject(globalSessionID, authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CompleteTaskOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	if !belongs {
		return &mcp.CallToolResult{IsError: true}, CompleteTaskOutput{}, fmt.Errorf("session not found in current project")
	}

	proposalCount, err := countSessionDocProposals(globalSessionID, authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CompleteTaskOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	if proposalCount == 0 {
		return &mcp.CallToolResult{IsError: true}, CompleteTaskOutput{}, fmt.Errorf(
			"missing mandatory documentation-impact proposal for session %d: submit at least one propose_doc_update before complete_task; session remains open for correction",
			input.SessionId,
		)
	}

	_, normalizedReport, err := decodeStructuredFinalReport(input.FinalReport)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CompleteTaskOutput{}, err
	}

	control, _, err := fetchOrchestrationControl(authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CompleteTaskOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	if control.OrchestrationFrozen {
		return &mcp.CallToolResult{IsError: true}, CompleteTaskOutput{}, fmt.Errorf("orchestration is frozen for project %d; explicit resume is required before review-ready completion", authorizedProjectId)
	}

	launchEpoch, err := latestWorkerLaunchEpoch(globalSessionID, authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CompleteTaskOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	if launchEpoch > 0 && control.OrchestrationEpoch != launchEpoch {
		return &mcp.CallToolResult{IsError: true}, CompleteTaskOutput{}, fmt.Errorf(
			"session %d was launched under orchestration epoch %d, but the active epoch is now %d; review-ready completion is blocked until Fixer explicitly resumes or forks repair work",
			input.SessionId,
			launchEpoch,
			control.OrchestrationEpoch,
		)
	}

	_, err = db.Exec("UPDATE session SET status = 'review', report = ? WHERE id = ? AND project_id = ?", normalizedReport, globalSessionID, authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CompleteTaskOutput{}, fmt.Errorf("DB update error: %v", err)
	}

	return nil, CompleteTaskOutput{Status: "success"}, nil
}

type UpdateTaskInput struct {
	SessionId           int    `json:"session_id" jsonschema:"The ID of the session to update"`
	AppendedDescription string `json:"appended_description" jsonschema:"Instructions to append to the task"`
}

type UpdateTaskOutput struct {
	Status string `json:"status"`
}

func UpdateTask(ctx context.Context, req *mcp.CallToolRequest, input UpdateTaskInput) (*mcp.CallToolResult, UpdateTaskOutput, error) {
	if authorizedRole != "fixer" {
		return &mcp.CallToolResult{IsError: true}, UpdateTaskOutput{}, fmt.Errorf("access denied: requires fixer role")
	}

	globalSessionID, err := globalSessionIDFromProjectScoped(input.SessionId, authorizedProjectId)
	if err == sql.ErrNoRows {
		return &mcp.CallToolResult{IsError: true}, UpdateTaskOutput{}, fmt.Errorf("session not found in current project")
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, UpdateTaskOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	_, err = db.Exec("UPDATE session SET task_description = task_description || '\n\n' || ? WHERE id = ? AND project_id = ?", input.AppendedDescription, globalSessionID, authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, UpdateTaskOutput{}, fmt.Errorf("DB update error: %v", err)
	}

	return nil, UpdateTaskOutput{Status: "success"}, nil
}

type GetAllSessionsInput struct{}

type SessionRecord struct {
	Id              int    `json:"id"`
	ProjectId       int    `json:"project_id"`
	TaskDescription string `json:"task_description"`
	Status          string `json:"status"`
}

type GetAllSessionsOutput struct {
	Sessions []SessionRecord `json:"sessions"`
}

func GetAllSessions(ctx context.Context, req *mcp.CallToolRequest, input GetAllSessionsInput) (*mcp.CallToolResult, GetAllSessionsOutput, error) {
	if authorizedRole != "overseer" {
		return &mcp.CallToolResult{IsError: true}, GetAllSessionsOutput{}, fmt.Errorf("access denied: requires overseer role")
	}

	rows, err := db.Query("SELECT id, project_id, task_description, status FROM session")
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetAllSessionsOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	defer rows.Close()

	var sessions []SessionRecord
	for rows.Next() {
		var s SessionRecord
		if err := rows.Scan(&s.Id, &s.ProjectId, &s.TaskDescription, &s.Status); err != nil {
			return &mcp.CallToolResult{IsError: true}, GetAllSessionsOutput{}, fmt.Errorf("DB scan error: %v", err)
		}
		sessions = append(sessions, s)
	}
	if sessions == nil {
		sessions = []SessionRecord{}
	}

	return nil, GetAllSessionsOutput{Sessions: sessions}, nil
}

type AutonomousRunStatusRecord struct {
	ProjectId                        int    `json:"project_id"`
	SessionId                        int    `json:"session_id,omitempty"`
	State                            string `json:"state"`
	Summary                          string `json:"summary"`
	Focus                            string `json:"focus,omitempty"`
	Blocker                          string `json:"blocker,omitempty"`
	Evidence                         string `json:"evidence,omitempty"`
	OrchestrationEpoch               int    `json:"orchestration_epoch"`
	OrchestrationFrozen              bool   `json:"orchestration_frozen"`
	NotificationsEnabledForActiveRun bool   `json:"notifications_enabled_for_active_run"`
	UpdatedAt                        string `json:"updated_at"`
}

type SetAutonomousRunStatusInput struct {
	ProjectId           int    `json:"project_id,omitempty" jsonschema:"Optional project ID when called by overseer; fixer uses the bound project."`
	SessionId           int    `json:"session_id,omitempty" jsonschema:"Optional session ID for the current autonomous step."`
	State               string `json:"state" jsonschema:"Autonomous run state: running | blocked | awaiting_review | awaiting_next_dispatch | completed | idle"`
	Summary             string `json:"summary" jsonschema:"Short human-readable status summary"`
	Focus               string `json:"focus,omitempty" jsonschema:"Optional current focus"`
	Blocker             string `json:"blocker,omitempty" jsonschema:"Optional blocker note"`
	Evidence            string `json:"evidence,omitempty" jsonschema:"Optional evidence or context"`
	ResumeOrchestration bool   `json:"resume_orchestration,omitempty" jsonschema:"When true, explicitly clears the orchestration freeze and re-enables notifications for the active run."`
}

type SetAutonomousRunStatusOutput struct {
	Status string                    `json:"status"`
	Record AutonomousRunStatusRecord `json:"record"`
}

type GetAutonomousRunStatusInput struct {
	ProjectId int `json:"project_id,omitempty" jsonschema:"Optional project ID when called by overseer; fixer/netrunner use the bound project."`
}

type GetAutonomousRunStatusOutput struct {
	ProjectId int                       `json:"project_id"`
	HasStatus bool                      `json:"has_status"`
	Status    AutonomousRunStatusRecord `json:"status"`
}

type SendOperatorTelegramNotificationInput struct {
	ProjectId int    `json:"project_id,omitempty" jsonschema:"Optional project ID when called by overseer; fixer/netrunner use the bound project."`
	Source    string `json:"source" jsonschema:"Actor/source label shown in the Telegram message, in plain Russian."`
	Status    string `json:"status" jsonschema:"Concise Russian status line for the operator."`
	Summary   string `json:"summary,omitempty" jsonschema:"Optional one-line Russian summary."`
	SessionId int    `json:"session_id,omitempty" jsonschema:"Optional session context. Fixer/netrunner use local session IDs; overseer uses the global session ID."`
	RunState  string `json:"run_state,omitempty" jsonschema:"Optional run-state context such as blocked, running, awaiting_review, completed."`
	Details   string `json:"details,omitempty" jsonschema:"Optional compact details line in plain Russian."`
}

type SendOperatorTelegramNotificationOutput struct {
	Status      string `json:"status"`
	ProjectId   int    `json:"project_id"`
	ProjectName string `json:"project_name"`
	SessionId   int    `json:"session_id,omitempty"`
	ChatId      string `json:"chat_id"`
	Message     string `json:"message"`
}

func resolveAutonomousProjectID(projectID int) (int, error) {
	if authorizedRole == "overseer" {
		if projectID > 0 {
			return projectID, nil
		}
		return 0, fmt.Errorf("project_id is required for overseer")
	}
	if authorizedProjectId <= 0 {
		return 0, fmt.Errorf("project context is unavailable")
	}
	if projectID > 0 && projectID != authorizedProjectId {
		return 0, fmt.Errorf("access denied: project_id does not match current project")
	}
	return authorizedProjectId, nil
}

func resolveProjectHandoffProjectID(projectID int) (int, error) {
	switch authorizedRole {
	case "fixer":
		if authorizedProjectId <= 0 {
			return 0, fmt.Errorf("project context is unavailable")
		}
		if projectID > 0 && projectID != authorizedProjectId {
			return 0, fmt.Errorf("access denied: project_id does not match current project")
		}
		return authorizedProjectId, nil
	case "overseer":
		if projectID <= 0 {
			return 0, fmt.Errorf("project_id is required for overseer")
		}
		exists, err := projectExists(projectID)
		if err != nil {
			return 0, err
		}
		if !exists {
			return 0, sql.ErrNoRows
		}
		return projectID, nil
	default:
		return 0, fmt.Errorf("access denied: requires fixer or overseer role")
	}
}

func resolveAutonomousSessionID(sessionID int, projectID int) (int, error) {
	if sessionID <= 0 {
		return 0, nil
	}
	if authorizedRole == "overseer" {
		var belongingProjectID int
		err := db.QueryRow("SELECT project_id FROM session WHERE id = ?", sessionID).Scan(&belongingProjectID)
		if err != nil {
			return 0, err
		}
		if projectID > 0 && belongingProjectID != projectID {
			return 0, fmt.Errorf("session does not belong to project %d", projectID)
		}
		return sessionID, nil
	}

	globalSessionID, err := globalSessionIDFromProjectScoped(sessionID, projectID)
	if err != nil {
		return 0, err
	}
	belongs, err := sessionBelongsToProject(globalSessionID, projectID)
	if err != nil {
		return 0, err
	}
	if !belongs {
		return 0, sql.ErrNoRows
	}
	return globalSessionID, nil
}

func fetchAutonomousRunStatusRecord(projectID int) (AutonomousRunStatusRecord, error) {
	var record AutonomousRunStatusRecord
	var frozenInt int
	var notificationsEnabled int
	err := db.QueryRow(
		`SELECT project_id,
		        COALESCE(session_id, 0),
		        state,
		        summary,
		        COALESCE(focus, ''),
		        COALESCE(blocker, ''),
		        COALESCE(evidence, ''),
		        COALESCE(orchestration_epoch, 0),
		        COALESCE(orchestration_frozen, 0),
		        COALESCE(notifications_enabled_for_active_run, 1),
		        updated_at
		 FROM autonomous_run_status
		 WHERE project_id = ?`,
		projectID,
	).Scan(
		&record.ProjectId,
		&record.SessionId,
		&record.State,
		&record.Summary,
		&record.Focus,
		&record.Blocker,
		&record.Evidence,
		&record.OrchestrationEpoch,
		&frozenInt,
		&notificationsEnabled,
		&record.UpdatedAt,
	)
	if err != nil {
		return AutonomousRunStatusRecord{}, err
	}
	record.OrchestrationFrozen = frozenInt != 0
	record.NotificationsEnabledForActiveRun = notificationsEnabled != 0
	return record, nil
}

func SetAutonomousRunStatus(ctx context.Context, req *mcp.CallToolRequest, input SetAutonomousRunStatusInput) (*mcp.CallToolResult, SetAutonomousRunStatusOutput, error) {
	if authorizedRole != "fixer" && authorizedRole != "overseer" {
		return &mcp.CallToolResult{IsError: true}, SetAutonomousRunStatusOutput{}, fmt.Errorf("access denied: requires fixer or overseer role")
	}

	projectID, err := resolveAutonomousProjectID(input.ProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetAutonomousRunStatusOutput{}, err
	}

	state, err := normalizeAutonomousStatusLabel(input.State)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetAutonomousRunStatusOutput{}, err
	}

	sessionID, err := resolveAutonomousSessionID(input.SessionId, projectID)
	if err != nil {
		if err == sql.ErrNoRows {
			return &mcp.CallToolResult{IsError: true}, SetAutonomousRunStatusOutput{}, fmt.Errorf("session not found in current project")
		}
		return &mcp.CallToolResult{IsError: true}, SetAutonomousRunStatusOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	summary := strings.TrimSpace(input.Summary)
	if summary == "" {
		return &mcp.CallToolResult{IsError: true}, SetAutonomousRunStatusOutput{}, fmt.Errorf("summary is required")
	}

	focus := strings.TrimSpace(input.Focus)
	blocker := strings.TrimSpace(input.Blocker)
	evidence := strings.TrimSpace(input.Evidence)

	control, exists, err := fetchOrchestrationControl(projectID)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetAutonomousRunStatusOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	if !exists {
		control.OrchestrationEpoch = 0
		control.NotificationsEnabledForActiveRun = true
		control.OrchestrationFrozen = false
	}
	if input.ResumeOrchestration {
		control.OrchestrationFrozen = false
		control.NotificationsEnabledForActiveRun = true
	}

	err = upsertOrchestrationControl(
		projectID,
		sessionID,
		state,
		summary,
		focus,
		blocker,
		evidence,
		control.OrchestrationEpoch,
		control.OrchestrationFrozen,
		control.NotificationsEnabledForActiveRun,
	)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetAutonomousRunStatusOutput{}, fmt.Errorf("DB upsert error: %v", err)
	}

	record, err := fetchAutonomousRunStatusRecord(projectID)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetAutonomousRunStatusOutput{}, err
	}

	return nil, SetAutonomousRunStatusOutput{Status: "success", Record: record}, nil
}

func GetAutonomousRunStatus(ctx context.Context, req *mcp.CallToolRequest, input GetAutonomousRunStatusInput) (*mcp.CallToolResult, GetAutonomousRunStatusOutput, error) {
	if authorizedRole != "fixer" && authorizedRole != "netrunner" && authorizedRole != "overseer" {
		return &mcp.CallToolResult{IsError: true}, GetAutonomousRunStatusOutput{}, fmt.Errorf("access denied: requires authenticated role")
	}

	projectID, err := resolveAutonomousProjectID(input.ProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetAutonomousRunStatusOutput{}, err
	}

	record, err := fetchAutonomousRunStatusRecord(projectID)
	if err == sql.ErrNoRows {
		return nil, GetAutonomousRunStatusOutput{
			ProjectId: projectID,
			HasStatus: false,
			Status:    AutonomousRunStatusRecord{ProjectId: projectID, NotificationsEnabledForActiveRun: true},
		}, nil
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetAutonomousRunStatusOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	return nil, GetAutonomousRunStatusOutput{
		ProjectId: projectID,
		HasStatus: true,
		Status:    record,
	}, nil
}

func SendOperatorTelegramNotification(ctx context.Context, req *mcp.CallToolRequest, input SendOperatorTelegramNotificationInput) (*mcp.CallToolResult, SendOperatorTelegramNotificationOutput, error) {
	if authorizedRole != "fixer" && authorizedRole != "netrunner" && authorizedRole != "overseer" {
		return &mcp.CallToolResult{IsError: true}, SendOperatorTelegramNotificationOutput{}, fmt.Errorf("access denied: requires authenticated role")
	}

	projectID, err := resolveAutonomousProjectID(input.ProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SendOperatorTelegramNotificationOutput{}, err
	}

	control, _, err := fetchOrchestrationControl(projectID)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SendOperatorTelegramNotificationOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	if !control.NotificationsEnabledForActiveRun {
		return &mcp.CallToolResult{IsError: true}, SendOperatorTelegramNotificationOutput{}, fmt.Errorf("operator notifications are disabled for the active run; explicit orchestration resume is required before sending routine updates")
	}

	projectName, err := projectNameFromID(projectID)
	if err != nil {
		if err == sql.ErrNoRows {
			return &mcp.CallToolResult{IsError: true}, SendOperatorTelegramNotificationOutput{}, fmt.Errorf("project not found")
		}
		return &mcp.CallToolResult{IsError: true}, SendOperatorTelegramNotificationOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	source := truncateRunes(normalizeCompactText(input.Source), 80)
	if source == "" {
		return &mcp.CallToolResult{IsError: true}, SendOperatorTelegramNotificationOutput{}, fmt.Errorf("source is required")
	}

	statusText := truncateRunes(normalizeCompactText(input.Status), 120)
	if statusText == "" {
		return &mcp.CallToolResult{IsError: true}, SendOperatorTelegramNotificationOutput{}, fmt.Errorf("status is required")
	}

	summary := truncateRunes(normalizeCompactText(input.Summary), 220)
	runState := truncateRunes(normalizeCompactText(input.RunState), 60)
	details := truncateRunes(normalizeCompactText(input.Details), 280)

	visibleSessionID := 0
	switch {
	case input.SessionId > 0 && authorizedRole == "overseer":
		var sessionProjectID int
		err := db.QueryRow("SELECT project_id FROM session WHERE id = ?", input.SessionId).Scan(&sessionProjectID)
		if err == sql.ErrNoRows {
			return &mcp.CallToolResult{IsError: true}, SendOperatorTelegramNotificationOutput{}, fmt.Errorf("session not found")
		}
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, SendOperatorTelegramNotificationOutput{}, fmt.Errorf("DB query error: %v", err)
		}
		if sessionProjectID != projectID {
			return &mcp.CallToolResult{IsError: true}, SendOperatorTelegramNotificationOutput{}, fmt.Errorf("session does not belong to project %d", projectID)
		}
		visibleSessionID = input.SessionId
	case input.SessionId > 0:
		globalSessionID, err := globalSessionIDFromProjectScoped(input.SessionId, projectID)
		if err == sql.ErrNoRows {
			return &mcp.CallToolResult{IsError: true}, SendOperatorTelegramNotificationOutput{}, fmt.Errorf("session not found in current project")
		}
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, SendOperatorTelegramNotificationOutput{}, fmt.Errorf("DB query error: %v", err)
		}
		belongs, err := sessionBelongsToProject(globalSessionID, projectID)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, SendOperatorTelegramNotificationOutput{}, fmt.Errorf("DB query error: %v", err)
		}
		if !belongs {
			return &mcp.CallToolResult{IsError: true}, SendOperatorTelegramNotificationOutput{}, fmt.Errorf("session not found in current project")
		}
		visibleSessionID = input.SessionId
	case authorizedRole == "netrunner" && authorizedSessionId > 0:
		mappedSessionID, err := projectScopedSessionIDFromGlobal(authorizedSessionId, projectID)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, SendOperatorTelegramNotificationOutput{}, fmt.Errorf("failed to map active session to project-scoped id: %v", err)
		}
		visibleSessionID = mappedSessionID
	}

	message := renderTelegramOperatorNotification(projectName, projectID, source, statusText, summary, visibleSessionID, runState, details)
	botToken, chatID, apiBaseURL, err := resolveTelegramOperatorConfigFromEnv()
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SendOperatorTelegramNotificationOutput{}, err
	}
	if err := sendTelegramText(ctx, botToken, chatID, apiBaseURL, message); err != nil {
		return &mcp.CallToolResult{IsError: true}, SendOperatorTelegramNotificationOutput{}, err
	}

	return nil, SendOperatorTelegramNotificationOutput{
		Status:      "success",
		ProjectId:   projectID,
		ProjectName: projectName,
		SessionId:   visibleSessionID,
		ChatId:      chatID,
		Message:     message,
	}, nil
}

type ProjectHandoffRecord struct {
	ProjectId int    `json:"project_id"`
	Content   string `json:"content"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

type SetProjectHandoffInput struct {
	ProjectId int    `json:"project_id,omitempty" jsonschema:"Optional project ID when called by overseer; fixer uses the bound project."`
	Content   string `json:"content" jsonschema:"Concise project handoff content to persist for startup reuse."`
}

type SetProjectHandoffOutput struct {
	Status string               `json:"status"`
	Record ProjectHandoffRecord `json:"record"`
}

type GetProjectHandoffInput struct {
	ProjectId int `json:"project_id,omitempty" jsonschema:"Optional project ID when called by overseer; fixer uses the bound project."`
}

type GetProjectHandoffOutput struct {
	ProjectId  int                  `json:"project_id"`
	HasHandoff bool                 `json:"has_handoff"`
	Handoff    ProjectHandoffRecord `json:"handoff"`
}

type ClearProjectHandoffInput struct {
	ProjectId int `json:"project_id,omitempty" jsonschema:"Optional project ID when called by overseer; fixer uses the bound project."`
}

type ClearProjectHandoffOutput struct {
	Status    string `json:"status"`
	ProjectId int    `json:"project_id"`
}

func fetchProjectHandoffRecord(projectID int) (ProjectHandoffRecord, error) {
	var record ProjectHandoffRecord
	err := db.QueryRow(
		`SELECT project_id, content, updated_at
		 FROM project_handoff
		 WHERE project_id = ?`,
		projectID,
	).Scan(&record.ProjectId, &record.Content, &record.UpdatedAt)
	if err != nil {
		return ProjectHandoffRecord{}, err
	}
	return record, nil
}

func SetProjectHandoff(ctx context.Context, req *mcp.CallToolRequest, input SetProjectHandoffInput) (*mcp.CallToolResult, SetProjectHandoffOutput, error) {
	projectID, err := resolveProjectHandoffProjectID(input.ProjectId)
	if err != nil {
		if err == sql.ErrNoRows {
			return &mcp.CallToolResult{IsError: true}, SetProjectHandoffOutput{}, fmt.Errorf("project not found")
		}
		return &mcp.CallToolResult{IsError: true}, SetProjectHandoffOutput{}, err
	}

	content := strings.TrimSpace(input.Content)
	if content == "" {
		return &mcp.CallToolResult{IsError: true}, SetProjectHandoffOutput{}, fmt.Errorf("content is required")
	}

	_, err = db.Exec(
		`INSERT INTO project_handoff (project_id, content, updated_at)
		 VALUES (?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(project_id) DO UPDATE SET
		   content = excluded.content,
		   updated_at = CURRENT_TIMESTAMP`,
		projectID,
		content,
	)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetProjectHandoffOutput{}, fmt.Errorf("DB upsert error: %v", err)
	}

	record, err := fetchProjectHandoffRecord(projectID)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetProjectHandoffOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	return nil, SetProjectHandoffOutput{Status: "success", Record: record}, nil
}

func GetProjectHandoff(ctx context.Context, req *mcp.CallToolRequest, input GetProjectHandoffInput) (*mcp.CallToolResult, GetProjectHandoffOutput, error) {
	projectID, err := resolveProjectHandoffProjectID(input.ProjectId)
	if err != nil {
		if err == sql.ErrNoRows {
			return &mcp.CallToolResult{IsError: true}, GetProjectHandoffOutput{}, fmt.Errorf("project not found")
		}
		return &mcp.CallToolResult{IsError: true}, GetProjectHandoffOutput{}, err
	}

	record, err := fetchProjectHandoffRecord(projectID)
	if err == sql.ErrNoRows {
		return nil, GetProjectHandoffOutput{
			ProjectId:  projectID,
			HasHandoff: false,
			Handoff:    ProjectHandoffRecord{ProjectId: projectID},
		}, nil
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetProjectHandoffOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	return nil, GetProjectHandoffOutput{
		ProjectId:  projectID,
		HasHandoff: true,
		Handoff:    record,
	}, nil
}

func ClearProjectHandoff(ctx context.Context, req *mcp.CallToolRequest, input ClearProjectHandoffInput) (*mcp.CallToolResult, ClearProjectHandoffOutput, error) {
	projectID, err := resolveProjectHandoffProjectID(input.ProjectId)
	if err != nil {
		if err == sql.ErrNoRows {
			return &mcp.CallToolResult{IsError: true}, ClearProjectHandoffOutput{}, fmt.Errorf("project not found")
		}
		return &mcp.CallToolResult{IsError: true}, ClearProjectHandoffOutput{}, err
	}

	if _, err := db.Exec("DELETE FROM project_handoff WHERE project_id = ?", projectID); err != nil {
		return &mcp.CallToolResult{IsError: true}, ClearProjectHandoffOutput{}, fmt.Errorf("DB delete error: %v", err)
	}

	return nil, ClearProjectHandoffOutput{Status: "success", ProjectId: projectID}, nil
}

type SetSessionStatusInput struct {
	SessionId int    `json:"session_id" jsonschema:"The ID of the session to update"`
	Status    string `json:"status" jsonschema:"New status: pending | in_progress | review | completed"`
	Reason    string `json:"reason,omitempty" jsonschema:"Optional reason for the transition"`
	Note      string `json:"note,omitempty" jsonschema:"Optional note alias for reason"`
}

type SetSessionStatusOutput struct {
	Status         string `json:"status"`
	SessionId      int    `json:"session_id"`
	PreviousStatus string `json:"previous_status"`
	NewStatus      string `json:"new_status"`
}

func SetSessionStatus(ctx context.Context, req *mcp.CallToolRequest, input SetSessionStatusInput) (*mcp.CallToolResult, SetSessionStatusOutput, error) {
	if authorizedRole != "fixer" && authorizedRole != "overseer" {
		return &mcp.CallToolResult{IsError: true}, SetSessionStatusOutput{}, fmt.Errorf("access denied: requires fixer or overseer role")
	}

	targetStatus := strings.ToLower(strings.TrimSpace(input.Status))
	if !isValidSessionStatus(targetStatus) {
		return &mcp.CallToolResult{IsError: true}, SetSessionStatusOutput{}, fmt.Errorf("invalid status: must be one of pending, in_progress, review, completed")
	}

	targetSessionID := input.SessionId
	if authorizedRole != "overseer" {
		globalSessionID, err := globalSessionIDFromProjectScoped(input.SessionId, authorizedProjectId)
		if err == sql.ErrNoRows {
			return &mcp.CallToolResult{IsError: true}, SetSessionStatusOutput{}, fmt.Errorf("session not found in current project")
		}
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, SetSessionStatusOutput{}, fmt.Errorf("DB query error: %v", err)
		}
		targetSessionID = globalSessionID
	}

	var projectId int
	var currentStatus string
	err := db.QueryRow("SELECT project_id, status FROM session WHERE id = ?", targetSessionID).Scan(&projectId, &currentStatus)
	if err == sql.ErrNoRows {
		return &mcp.CallToolResult{IsError: true}, SetSessionStatusOutput{}, fmt.Errorf("session not found")
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetSessionStatusOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	if authorizedRole == "fixer" && projectId != authorizedProjectId {
		return &mcp.CallToolResult{IsError: true}, SetSessionStatusOutput{}, fmt.Errorf("access denied: session not found in current project")
	}

	control, _, err := fetchOrchestrationControl(projectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetSessionStatusOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	if control.OrchestrationFrozen && targetStatus != currentStatus {
		return &mcp.CallToolResult{IsError: true}, SetSessionStatusOutput{}, fmt.Errorf("orchestration is frozen for project %d; explicit resume is required before changing session status", projectId)
	}

	if !isAllowedSessionTransition(currentStatus, targetStatus) {
		return &mcp.CallToolResult{IsError: true}, SetSessionStatusOutput{}, fmt.Errorf("invalid status transition: %s -> %s", currentStatus, targetStatus)
	}

	_, err = db.Exec("UPDATE session SET status = ? WHERE id = ?", targetStatus, targetSessionID)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SetSessionStatusOutput{}, fmt.Errorf("DB update error: %v", err)
	}
	if targetStatus == "pending" && (currentStatus == "review" || currentStatus == "completed") && currentStatus != targetStatus {
		if _, err := db.Exec("UPDATE session SET rework_count = COALESCE(rework_count, 0) + 1 WHERE id = ?", targetSessionID); err != nil {
			return &mcp.CallToolResult{IsError: true}, SetSessionStatusOutput{}, fmt.Errorf("DB update error: %v", err)
		}
	}

	reason := strings.TrimSpace(input.Reason)
	if reason == "" {
		reason = strings.TrimSpace(input.Note)
	}
	visibleSessionID := targetSessionID
	if authorizedRole != "overseer" {
		visibleSessionID = input.SessionId
	}
	log.Printf("set_session_status role=%s session_id=%d project_id=%d from=%s to=%s reason=%q", authorizedRole, visibleSessionID, projectId, currentStatus, targetStatus, reason)

	return nil, SetSessionStatusOutput{
		Status:         "success",
		SessionId:      visibleSessionID,
		PreviousStatus: currentStatus,
		NewStatus:      targetStatus,
	}, nil
}

type ForkRepairSessionFromInput struct {
	SessionId          int      `json:"session_id" jsonschema:"The project-scoped session ID to fork into a new repair session."`
	Reason             string   `json:"reason,omitempty" jsonschema:"Optional concise provenance note explaining why the repair fork is being created."`
	DeclaredWriteScope []string `json:"declared_write_scope,omitempty" jsonschema:"Optional replacement declared write scope. Defaults to the source session scope."`
	ParallelWaveID     string   `json:"parallel_wave_id,omitempty" jsonschema:"Optional replacement explicit parallel wave identifier. Defaults to serial-safe empty."`
}

type ForkRepairSessionFromOutput struct {
	Status          string `json:"status"`
	SourceSessionId int    `json:"source_session_id"`
	NewSessionId    int    `json:"new_session_id"`
}

func ForkRepairSessionFrom(ctx context.Context, req *mcp.CallToolRequest, input ForkRepairSessionFromInput) (*mcp.CallToolResult, ForkRepairSessionFromOutput, error) {
	if authorizedRole != "fixer" {
		return &mcp.CallToolResult{IsError: true}, ForkRepairSessionFromOutput{}, fmt.Errorf("access denied: requires fixer role")
	}

	sourceSessionID, err := globalSessionIDFromProjectScoped(input.SessionId, authorizedProjectId)
	if err == sql.ErrNoRows {
		return &mcp.CallToolResult{IsError: true}, ForkRepairSessionFromOutput{}, fmt.Errorf("session not found in current project")
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, ForkRepairSessionFromOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	var taskDescription string
	var sourceWriteScope string
	err = db.QueryRow(
		`SELECT task_description, COALESCE(declared_write_scope, '')
		 FROM session
		 WHERE id = ? AND project_id = ?`,
		sourceSessionID,
		authorizedProjectId,
	).Scan(&taskDescription, &sourceWriteScope)
	if err == sql.ErrNoRows {
		return &mcp.CallToolResult{IsError: true}, ForkRepairSessionFromOutput{}, fmt.Errorf("session not found in current project")
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, ForkRepairSessionFromOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	writeScope := input.DeclaredWriteScope
	if len(writeScope) == 0 {
		writeScope, err = decodeDeclaredWriteScope(sourceWriteScope)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, ForkRepairSessionFromOutput{}, err
		}
	}
	encodedWriteScope, err := encodeDeclaredWriteScope(writeScope)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, ForkRepairSessionFromOutput{}, err
	}

	provenanceLines := []string{taskDescription, fmt.Sprintf("Repair fork source session: %d.", input.SessionId)}
	if reason := strings.TrimSpace(input.Reason); reason != "" {
		provenanceLines = append(provenanceLines, "Repair fork reason: "+reason)
	}
	newTaskDescription := strings.Join(provenanceLines, "\n\n")
	parallelWaveID := strings.TrimSpace(input.ParallelWaveID)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, ForkRepairSessionFromOutput{}, fmt.Errorf("DB transaction error: %v", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	result, err := tx.Exec(
		`INSERT INTO session (
			project_id,
			task_description,
			status,
			declared_write_scope,
			parallel_wave_id,
			repair_source_session_id
		) VALUES (?, ?, 'pending', ?, ?, ?)`,
		authorizedProjectId,
		newTaskDescription,
		encodedWriteScope,
		parallelWaveID,
		sourceSessionID,
	)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, ForkRepairSessionFromOutput{}, fmt.Errorf("DB insert error: %v", err)
	}

	newGlobalSessionID64, err := result.LastInsertId()
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, ForkRepairSessionFromOutput{}, fmt.Errorf("LastInsertId error: %v", err)
	}
	newGlobalSessionID := int(newGlobalSessionID64)

	if _, err := tx.Exec(
		`INSERT OR IGNORE INTO netrunner_attached_doc (session_id, project_doc_id)
		 SELECT ?, project_doc_id
		 FROM netrunner_attached_doc
		 WHERE session_id = ?`,
		newGlobalSessionID,
		sourceSessionID,
	); err != nil {
		return &mcp.CallToolResult{IsError: true}, ForkRepairSessionFromOutput{}, fmt.Errorf("DB copy error: %v", err)
	}

	if _, err := tx.Exec(
		`INSERT OR IGNORE INTO session_mcp_server (session_id, mcp_server_id)
		 SELECT ?, mcp_server_id
		 FROM session_mcp_server
		 WHERE session_id = ?`,
		newGlobalSessionID,
		sourceSessionID,
	); err != nil {
		return &mcp.CallToolResult{IsError: true}, ForkRepairSessionFromOutput{}, fmt.Errorf("DB copy error: %v", err)
	}

	if err := tx.Commit(); err != nil {
		return &mcp.CallToolResult{IsError: true}, ForkRepairSessionFromOutput{}, fmt.Errorf("DB commit error: %v", err)
	}

	newLocalSessionID, err := projectScopedSessionIDFromGlobal(newGlobalSessionID, authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, ForkRepairSessionFromOutput{}, fmt.Errorf("DB mapping error: %v", err)
	}

	return nil, ForkRepairSessionFromOutput{
		Status:          "success",
		SourceSessionId: input.SessionId,
		NewSessionId:    newLocalSessionID,
	}, nil
}

type CleanupClaimCheck struct {
	Path        string `json:"path"`
	Expectation string `json:"expectation"`
	Exists      bool   `json:"exists"`
	Matches     bool   `json:"matches"`
}

type VerifySessionCleanupClaimsInput struct {
	SessionId int `json:"session_id" jsonschema:"The project-scoped session ID whose cleanup claims should be checked against disk state."`
}

type VerifySessionCleanupClaimsOutput struct {
	Status        string              `json:"status"`
	SessionId     int                 `json:"session_id"`
	ReportPresent bool                `json:"report_present"`
	AllMatched    bool                `json:"all_matched"`
	Claims        []CleanupClaimCheck `json:"claims"`
}

func VerifySessionCleanupClaims(ctx context.Context, req *mcp.CallToolRequest, input VerifySessionCleanupClaimsInput) (*mcp.CallToolResult, VerifySessionCleanupClaimsOutput, error) {
	if authorizedRole != "fixer" {
		return &mcp.CallToolResult{IsError: true}, VerifySessionCleanupClaimsOutput{}, fmt.Errorf("access denied: requires fixer role")
	}

	globalSessionID, err := globalSessionIDFromProjectScoped(input.SessionId, authorizedProjectId)
	if err == sql.ErrNoRows {
		return &mcp.CallToolResult{IsError: true}, VerifySessionCleanupClaimsOutput{}, fmt.Errorf("session not found in current project")
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, VerifySessionCleanupClaimsOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	var report string
	err = db.QueryRow("SELECT COALESCE(report, '') FROM session WHERE id = ? AND project_id = ?", globalSessionID, authorizedProjectId).Scan(&report)
	if err == sql.ErrNoRows {
		return &mcp.CallToolResult{IsError: true}, VerifySessionCleanupClaimsOutput{}, fmt.Errorf("session not found in current project")
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, VerifySessionCleanupClaimsOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	reportPresent := strings.TrimSpace(report) != ""
	parsedReport, _, err := decodeStructuredFinalReport(report)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, VerifySessionCleanupClaimsOutput{}, err
	}

	projectCWD, err := projectCWDFromID(authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, VerifySessionCleanupClaimsOutput{}, fmt.Errorf("failed to resolve project cwd: %v", err)
	}

	claims := []CleanupClaimCheck{}
	allMatched := true

	checkPath := func(path string, expectation string) error {
		normalized, err := normalizeWriteScopePath(path)
		if err != nil {
			return err
		}
		targetPath := filepath.Join(projectCWD, filepath.FromSlash(normalized))
		_, statErr := os.Stat(targetPath)
		exists := statErr == nil
		if statErr != nil && !os.IsNotExist(statErr) {
			return statErr
		}
		matches := (expectation == "removed" && !exists) || (expectation == "present" && exists)
		if !matches {
			allMatched = false
		}
		claims = append(claims, CleanupClaimCheck{
			Path:        normalized,
			Expectation: expectation,
			Exists:      exists,
			Matches:     matches,
		})
		return nil
	}

	for _, path := range parsedReport.CleanupClaims.RemovedPaths {
		if err := checkPath(path, "removed"); err != nil {
			return &mcp.CallToolResult{IsError: true}, VerifySessionCleanupClaimsOutput{}, fmt.Errorf("filesystem check failed: %v", err)
		}
	}
	for _, path := range parsedReport.CleanupClaims.ExpectedPresentPaths {
		if err := checkPath(path, "present"); err != nil {
			return &mcp.CallToolResult{IsError: true}, VerifySessionCleanupClaimsOutput{}, fmt.Errorf("filesystem check failed: %v", err)
		}
	}

	return nil, VerifySessionCleanupClaimsOutput{
		Status:        "success",
		SessionId:     input.SessionId,
		ReportPresent: reportPresent,
		AllMatched:    allMatched,
		Claims:        claims,
	}, nil
}

type ListActiveWorkerProcessesInput struct {
	SessionIds []int `json:"session_ids,omitempty" jsonschema:"Optional project-scoped session IDs to filter the active worker process listing."`
}

type ListActiveWorkerProcessesOutput struct {
	Status    string                  `json:"status"`
	ProjectId int                     `json:"project_id"`
	Processes []workerProcessSnapshot `json:"processes"`
}

func ListActiveWorkerProcesses(ctx context.Context, req *mcp.CallToolRequest, input ListActiveWorkerProcessesInput) (*mcp.CallToolResult, ListActiveWorkerProcessesOutput, error) {
	if authorizedRole != "fixer" {
		return &mcp.CallToolResult{IsError: true}, ListActiveWorkerProcessesOutput{}, fmt.Errorf("access denied: requires fixer role")
	}

	globalSessionIDs := make([]int, 0, len(input.SessionIds))
	for _, localSessionID := range input.SessionIds {
		globalSessionID, err := globalSessionIDFromProjectScoped(localSessionID, authorizedProjectId)
		if err == sql.ErrNoRows {
			return &mcp.CallToolResult{IsError: true}, ListActiveWorkerProcessesOutput{}, fmt.Errorf("session %d not found in current project", localSessionID)
		}
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, ListActiveWorkerProcessesOutput{}, fmt.Errorf("DB query error: %v", err)
		}
		globalSessionIDs = append(globalSessionIDs, globalSessionID)
	}

	processes, err := listRunningWorkerProcesses(authorizedProjectId, globalSessionIDs)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, ListActiveWorkerProcessesOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	for i := range processes {
		localSessionID, err := projectScopedSessionIDFromGlobal(processes[i].SessionID, authorizedProjectId)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, ListActiveWorkerProcessesOutput{}, fmt.Errorf("DB mapping error: %v", err)
		}
		processes[i].SessionID = localSessionID
	}
	if processes == nil {
		processes = []workerProcessSnapshot{}
	}

	return nil, ListActiveWorkerProcessesOutput{
		Status:    "success",
		ProjectId: authorizedProjectId,
		Processes: processes,
	}, nil
}

type StopActiveWorkerProcessesInput struct {
	SessionIds          []int  `json:"session_ids,omitempty" jsonschema:"Optional project-scoped session IDs to stop. When omitted, all active worker processes in the current project are targeted."`
	FreezeOrchestration bool   `json:"freeze_orchestration,omitempty" jsonschema:"When true, increment the orchestration epoch and freeze follow-up automation until an explicit resume."`
	Reason              string `json:"reason,omitempty" jsonschema:"Optional operator-facing reason for the stop request."`
}

type StopActiveWorkerProcessesOutput struct {
	Status              string `json:"status"`
	ProjectId           int    `json:"project_id"`
	StoppedProcessCount int    `json:"stopped_process_count"`
	StoppedSessionIds   []int  `json:"stopped_session_ids"`
	FreezeApplied       bool   `json:"freeze_applied"`
	OrchestrationEpoch  int    `json:"orchestration_epoch"`
}

func StopActiveWorkerProcesses(ctx context.Context, req *mcp.CallToolRequest, input StopActiveWorkerProcessesInput) (*mcp.CallToolResult, StopActiveWorkerProcessesOutput, error) {
	if authorizedRole != "fixer" {
		return &mcp.CallToolResult{IsError: true}, StopActiveWorkerProcessesOutput{}, fmt.Errorf("access denied: requires fixer role")
	}

	globalSessionIDs := make([]int, 0, len(input.SessionIds))
	for _, localSessionID := range input.SessionIds {
		globalSessionID, err := globalSessionIDFromProjectScoped(localSessionID, authorizedProjectId)
		if err == sql.ErrNoRows {
			return &mcp.CallToolResult{IsError: true}, StopActiveWorkerProcessesOutput{}, fmt.Errorf("session %d not found in current project", localSessionID)
		}
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, StopActiveWorkerProcessesOutput{}, fmt.Errorf("DB query error: %v", err)
		}
		globalSessionIDs = append(globalSessionIDs, globalSessionID)
	}

	processes, err := listRunningWorkerProcesses(authorizedProjectId, globalSessionIDs)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, StopActiveWorkerProcessesOutput{}, fmt.Errorf("DB query error: %v", err)
	}

	reason := strings.TrimSpace(input.Reason)
	if reason == "" {
		reason = "operator stop requested"
	}

	stoppedSessions := make(map[int]struct{})
	for _, processRow := range processes {
		status := workerStatusStopped
		if processRow.Alive && runtime.GOOS != "windows" {
			targetProcess, findErr := os.FindProcess(processRow.PID)
			if findErr == nil {
				signalErr := targetProcess.Signal(syscall.SIGTERM)
				if signalErr != nil && !errors.Is(signalErr, os.ErrProcessDone) {
					return &mcp.CallToolResult{IsError: true}, StopActiveWorkerProcessesOutput{}, fmt.Errorf("failed to stop worker pid %d: %v", processRow.PID, signalErr)
				}
				time.Sleep(250 * time.Millisecond)
				if isProcessAlive(processRow.PID) {
					if killErr := targetProcess.Signal(syscall.SIGKILL); killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
						return &mcp.CallToolResult{IsError: true}, StopActiveWorkerProcessesOutput{}, fmt.Errorf("failed to force-stop worker pid %d: %v", processRow.PID, killErr)
					}
					time.Sleep(250 * time.Millisecond)
				}
			}
		}
		if !isProcessAlive(processRow.PID) {
			status = workerStatusExited
		}
		if _, err := db.Exec(
			`UPDATE worker_process
			 SET status = ?, stop_reason = ?, stopped_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
			 WHERE id = ? AND project_id = ?`,
			status,
			reason,
			processRow.ID,
			authorizedProjectId,
		); err != nil {
			return &mcp.CallToolResult{IsError: true}, StopActiveWorkerProcessesOutput{}, fmt.Errorf("DB update error: %v", err)
		}
		stoppedSessions[processRow.SessionID] = struct{}{}
	}

	for sessionID := range stoppedSessions {
		if _, err := db.Exec("UPDATE session SET forced_stop_count = COALESCE(forced_stop_count, 0) + 1 WHERE id = ? AND project_id = ?", sessionID, authorizedProjectId); err != nil {
			return &mcp.CallToolResult{IsError: true}, StopActiveWorkerProcessesOutput{}, fmt.Errorf("DB update error: %v", err)
		}
	}

	control, exists, err := fetchOrchestrationControl(authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, StopActiveWorkerProcessesOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	if !exists {
		control.ProjectID = authorizedProjectId
		control.NotificationsEnabledForActiveRun = true
	}
	if input.FreezeOrchestration {
		control.OrchestrationEpoch++
		control.OrchestrationFrozen = true
		control.NotificationsEnabledForActiveRun = false
		control.State = "blocked"
		control.Summary = "Orchestration frozen by stop_active_worker_processes"
		control.Blocker = reason
		if err := upsertOrchestrationControl(
			authorizedProjectId,
			0,
			control.State,
			control.Summary,
			control.Focus,
			control.Blocker,
			control.Evidence,
			control.OrchestrationEpoch,
			control.OrchestrationFrozen,
			control.NotificationsEnabledForActiveRun,
		); err != nil {
			return &mcp.CallToolResult{IsError: true}, StopActiveWorkerProcessesOutput{}, fmt.Errorf("DB upsert error: %v", err)
		}
	}

	stoppedSessionIDs := make([]int, 0, len(stoppedSessions))
	for globalSessionID := range stoppedSessions {
		localSessionID, err := projectScopedSessionIDFromGlobal(globalSessionID, authorizedProjectId)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, StopActiveWorkerProcessesOutput{}, fmt.Errorf("DB mapping error: %v", err)
		}
		stoppedSessionIDs = append(stoppedSessionIDs, localSessionID)
	}
	sort.Ints(stoppedSessionIDs)

	return nil, StopActiveWorkerProcessesOutput{
		Status:              "success",
		ProjectId:           authorizedProjectId,
		StoppedProcessCount: len(processes),
		StoppedSessionIds:   stoppedSessionIDs,
		FreezeApplied:       input.FreezeOrchestration,
		OrchestrationEpoch:  control.OrchestrationEpoch,
	}, nil
}

type GetSessionInput struct {
	SessionId int `json:"session_id" jsonschema:"The ID of the session to read"`
}

type SessionDetails struct {
	Id                    int      `json:"id"`
	ProjectId             int      `json:"project_id"`
	TaskDescription       string   `json:"task_description"`
	Status                string   `json:"status"`
	Report                string   `json:"report"`
	CliBackend            string   `json:"cli_backend"`
	CliModel              string   `json:"cli_model,omitempty"`
	CliReasoning          string   `json:"cli_reasoning,omitempty"`
	DeclaredWriteScope    []string `json:"declared_write_scope"`
	ParallelWaveID        string   `json:"parallel_wave_id,omitempty"`
	RepairSourceSessionId int      `json:"repair_source_session_id,omitempty"`
	ReworkCount           int      `json:"rework_count"`
	ForcedStopCount       int      `json:"forced_stop_count"`
}

type GetSessionOutput struct {
	Session SessionDetails `json:"session"`
}

func GetSession(ctx context.Context, req *mcp.CallToolRequest, input GetSessionInput) (*mcp.CallToolResult, GetSessionOutput, error) {
	if authorizedRole != "fixer" && authorizedRole != "netrunner" && authorizedRole != "overseer" {
		return &mcp.CallToolResult{IsError: true}, GetSessionOutput{}, fmt.Errorf("access denied: requires authenticated role")
	}

	targetSessionID := input.SessionId
	if authorizedRole != "overseer" {
		globalSessionID, err := globalSessionIDFromProjectScoped(input.SessionId, authorizedProjectId)
		if err == sql.ErrNoRows {
			return &mcp.CallToolResult{IsError: true}, GetSessionOutput{}, fmt.Errorf("session not found in current project")
		}
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, GetSessionOutput{}, fmt.Errorf("DB query error: %v", err)
		}
		targetSessionID = globalSessionID
	}

	var session SessionDetails
	var declaredWriteScope string
	err := db.QueryRow(
		`SELECT id,
		        project_id,
		        task_description,
		        status,
		        COALESCE(report, ''),
		        COALESCE(NULLIF(TRIM(cli_backend), ''), ?),
		        COALESCE(cli_model, ''),
		        COALESCE(cli_reasoning, ''),
		        COALESCE(declared_write_scope, ''),
		        COALESCE(parallel_wave_id, ''),
		        COALESCE(repair_source_session_id, 0),
		        COALESCE(rework_count, 0),
		        COALESCE(forced_stop_count, 0)
		 FROM session
		 WHERE id = ?`,
		defaultCliBackend,
		targetSessionID,
	).Scan(
		&session.Id,
		&session.ProjectId,
		&session.TaskDescription,
		&session.Status,
		&session.Report,
		&session.CliBackend,
		&session.CliModel,
		&session.CliReasoning,
		&declaredWriteScope,
		&session.ParallelWaveID,
		&session.RepairSourceSessionId,
		&session.ReworkCount,
		&session.ForcedStopCount,
	)
	if err == sql.ErrNoRows {
		return &mcp.CallToolResult{IsError: true}, GetSessionOutput{}, fmt.Errorf("session not found")
	}
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetSessionOutput{}, fmt.Errorf("DB query error: %v", err)
	}
	session.DeclaredWriteScope, err = decodeDeclaredWriteScope(declaredWriteScope)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, GetSessionOutput{}, fmt.Errorf("DB decode error: %v", err)
	}

	if !canAccessSession(session.ProjectId) {
		return &mcp.CallToolResult{IsError: true}, GetSessionOutput{}, fmt.Errorf("access denied: session not found in current project")
	}

	if authorizedRole != "overseer" {
		localSessionID, err := projectScopedSessionIDFromGlobal(session.Id, session.ProjectId)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, GetSessionOutput{}, fmt.Errorf("DB mapping error: %v", err)
		}
		session.Id = localSessionID
		if session.RepairSourceSessionId > 0 {
			localRepairSourceID, err := projectScopedSessionIDFromGlobal(session.RepairSourceSessionId, session.ProjectId)
			if err != nil {
				return &mcp.CallToolResult{IsError: true}, GetSessionOutput{}, fmt.Errorf("DB mapping error: %v", err)
			}
			session.RepairSourceSessionId = localRepairSourceID
		}
	}

	return nil, GetSessionOutput{Session: session}, nil
}

type ExplicitNetrunnerLaunchMetadata struct {
	SessionId              int      `json:"session_id"`
	ProjectCwd             string   `json:"project_cwd"`
	LauncherScript         string   `json:"launcher_script"`
	Backend                string   `json:"backend"`
	Model                  string   `json:"model,omitempty"`
	Reasoning              string   `json:"reasoning,omitempty"`
	ExternalSessionId      string   `json:"external_session_id,omitempty"`
	CodexSessionId         string   `json:"codex_session_id,omitempty"`
	SpawnedBackground      bool     `json:"spawned_background"`
	OrchestrationEpoch     int      `json:"orchestration_epoch"`
	DeclaredWriteScope     []string `json:"declared_write_scope"`
	ParallelWaveID         string   `json:"parallel_wave_id,omitempty"`
	ConcurrentLaunch       bool     `json:"concurrent_launch"`
	WriteScopeOverrideUsed bool     `json:"write_scope_override_used"`
}

type LaunchExplicitNetrunnerInput struct {
	SessionId                  int    `json:"session_id" jsonschema:"Project-scoped session ID to launch over the explicit MCP-mounted wire path."`
	FixerSessionId             string `json:"fixer_session_id,omitempty" jsonschema:"Optional current Fixer Codex session ID to pass into the explicit wire runtime."`
	WriteScopeOverrideReason   string `json:"write_scope_override_reason,omitempty" jsonschema:"Optional explicit reason to override an overlapping declared write scope launch block."`
	SessionReuseOverrideReason string `json:"session_reuse_override_reason,omitempty" jsonschema:"Optional explicit reason to relaunch a session that should normally be replaced by a repair fork after repeated rework or a forced stop."`
	Backend                    string `json:"backend,omitempty" jsonschema:"Optional CLI backend to launch for this session. Supported: codex, droid."`
	Model                      string `json:"model,omitempty" jsonschema:"Optional backend-specific model selection to persist for this session."`
	Reasoning                  string `json:"reasoning,omitempty" jsonschema:"Optional backend-specific reasoning setting to persist for this session."`
}

type LaunchExplicitNetrunnerOutput struct {
	Status string                          `json:"status"`
	Launch ExplicitNetrunnerLaunchMetadata `json:"launch"`
}

type WaitForNetrunnerSessionInput struct {
	SessionId           int `json:"session_id" jsonschema:"Project-scoped session ID to wait on."`
	TimeoutSeconds      int `json:"timeout_seconds,omitempty" jsonschema:"Optional wait timeout in seconds. Default 7200; max 21600."`
	PollIntervalSeconds int `json:"poll_interval_seconds,omitempty" jsonschema:"Optional poll interval in seconds. Default 5; max 60."`
}

type ExplicitNetrunnerWaitResult struct {
	SessionId             int    `json:"session_id"`
	SessionStatus         string `json:"session_status"`
	Backend               string `json:"backend,omitempty"`
	Model                 string `json:"model,omitempty"`
	Reasoning             string `json:"reasoning,omitempty"`
	ExternalSessionId     string `json:"external_session_id,omitempty"`
	Terminal              bool   `json:"terminal"`
	TerminalCondition     string `json:"terminal_condition"`
	TimedOut              bool   `json:"timed_out"`
	ElapsedSeconds        int    `json:"elapsed_seconds"`
	TimeoutSeconds        int    `json:"timeout_seconds"`
	PollIntervalSeconds   int    `json:"poll_interval_seconds"`
	Report                string `json:"report,omitempty"`
	ProposalIds           []int  `json:"proposal_ids"`
	CodexSessionId        string `json:"codex_session_id,omitempty"`
	FollowUpAllowed       bool   `json:"follow_up_allowed"`
	FollowUpBlockedReason string `json:"follow_up_blocked_reason,omitempty"`
	LaunchEpoch           int    `json:"launch_epoch"`
	CurrentEpoch          int    `json:"current_epoch"`
	OrchestrationFrozen   bool   `json:"orchestration_frozen"`
	RepairForkRecommended bool   `json:"repair_fork_recommended"`
}

type WaitForNetrunnerSessionOutput struct {
	Status string                      `json:"status"`
	Result ExplicitNetrunnerWaitResult `json:"result"`
}

type WaitForNetrunnerSessionsInput struct {
	SessionIds          []int `json:"session_ids,omitempty" jsonschema:"Optional explicit project-scoped session IDs to wait across. When omitted, the tool snapshots the current project's active explicit-launch candidates."`
	TimeoutSeconds      int   `json:"timeout_seconds,omitempty" jsonschema:"Optional wait timeout in seconds. Default 7200; max 21600."`
	PollIntervalSeconds int   `json:"poll_interval_seconds,omitempty" jsonschema:"Optional poll interval in seconds. Default 5; max 60."`
}

type ExplicitNetrunnerWaitAnyResult struct {
	WinningSessionId      int    `json:"winning_session_id,omitempty"`
	SessionStatus         string `json:"session_status,omitempty"`
	Backend               string `json:"backend,omitempty"`
	Model                 string `json:"model,omitempty"`
	Reasoning             string `json:"reasoning,omitempty"`
	ExternalSessionId     string `json:"external_session_id,omitempty"`
	Terminal              bool   `json:"terminal"`
	TerminalCondition     string `json:"terminal_condition"`
	TimedOut              bool   `json:"timed_out"`
	ElapsedSeconds        int    `json:"elapsed_seconds"`
	TimeoutSeconds        int    `json:"timeout_seconds"`
	PollIntervalSeconds   int    `json:"poll_interval_seconds"`
	Report                string `json:"report,omitempty"`
	ProposalIds           []int  `json:"proposal_ids"`
	CodexSessionId        string `json:"codex_session_id,omitempty"`
	ConsideredSessionIds  []int  `json:"considered_session_ids"`
	SelectionMode         string `json:"selection_mode"`
	FollowUpAllowed       bool   `json:"follow_up_allowed"`
	FollowUpBlockedReason string `json:"follow_up_blocked_reason,omitempty"`
	LaunchEpoch           int    `json:"launch_epoch"`
	CurrentEpoch          int    `json:"current_epoch"`
	OrchestrationFrozen   bool   `json:"orchestration_frozen"`
	RepairForkRecommended bool   `json:"repair_fork_recommended"`
}

type WaitForNetrunnerSessionsOutput struct {
	Status string                         `json:"status"`
	Result ExplicitNetrunnerWaitAnyResult `json:"result"`
}

type LaunchAndWaitNetrunnerInput struct {
	SessionId                  int    `json:"session_id" jsonschema:"Project-scoped session ID to launch and then wait on over the explicit MCP-mounted wire path."`
	FixerSessionId             string `json:"fixer_session_id,omitempty" jsonschema:"Optional current Fixer Codex session ID to pass into the explicit wire runtime."`
	WriteScopeOverrideReason   string `json:"write_scope_override_reason,omitempty" jsonschema:"Optional explicit reason to override an overlapping declared write scope launch block."`
	SessionReuseOverrideReason string `json:"session_reuse_override_reason,omitempty" jsonschema:"Optional explicit reason to relaunch a session that should normally be replaced by a repair fork after repeated rework or a forced stop."`
	Backend                    string `json:"backend,omitempty" jsonschema:"Optional CLI backend to launch for this session. Supported: codex, droid."`
	Model                      string `json:"model,omitempty" jsonschema:"Optional backend-specific model selection to persist for this session."`
	Reasoning                  string `json:"reasoning,omitempty" jsonschema:"Optional backend-specific reasoning setting to persist for this session."`
	TimeoutSeconds             int    `json:"timeout_seconds,omitempty" jsonschema:"Optional wait timeout in seconds. Default 7200; max 21600."`
	PollIntervalSeconds        int    `json:"poll_interval_seconds,omitempty" jsonschema:"Optional poll interval in seconds. Default 5; max 60."`
}

type LaunchAndWaitNetrunnerOutput struct {
	Status string                          `json:"status"`
	Launch ExplicitNetrunnerLaunchMetadata `json:"launch"`
	Wait   ExplicitNetrunnerWaitResult     `json:"wait"`
}

type activeLaunchSession struct {
	LocalSessionID     int
	GlobalSessionID    int
	DeclaredWriteScope []string
	ParallelWaveID     string
}

func loadActiveLaunchSessions(projectID int) ([]activeLaunchSession, error) {
	processes, err := listRunningWorkerProcesses(projectID, nil)
	if err != nil {
		return nil, err
	}

	seen := make(map[int]struct{}, len(processes))
	activeSessions := make([]activeLaunchSession, 0, len(processes))
	for _, process := range processes {
		if _, exists := seen[process.SessionID]; exists {
			continue
		}
		seen[process.SessionID] = struct{}{}

		state, err := fetchSessionLifecycleState(process.SessionID, projectID)
		if err != nil {
			return nil, err
		}
		localSessionID, err := projectScopedSessionIDFromGlobal(process.SessionID, projectID)
		if err != nil {
			return nil, err
		}
		activeSessions = append(activeSessions, activeLaunchSession{
			LocalSessionID:     localSessionID,
			GlobalSessionID:    process.SessionID,
			DeclaredWriteScope: state.DeclaredWriteScope,
			ParallelWaveID:     state.ParallelWaveID,
		})
	}
	return activeSessions, nil
}

func waitFollowUpDecision(control orchestrationControl, launchEpoch int) (bool, string) {
	reasons := []string{}
	if control.OrchestrationFrozen {
		reasons = append(reasons, "project_orchestration_frozen")
	}
	if launchEpoch > 0 && control.OrchestrationEpoch != launchEpoch {
		reasons = append(reasons, fmt.Sprintf("stale_orchestration_epoch:%d->%d", launchEpoch, control.OrchestrationEpoch))
	}
	if len(reasons) > 0 {
		return false, strings.Join(reasons, ",")
	}
	return true, ""
}

func launchExplicitNetrunnerWithMetadata(ctx context.Context, input LaunchExplicitNetrunnerInput) (ExplicitNetrunnerLaunchMetadata, error) {
	sessionID := input.SessionId
	globalSessionID, err := globalSessionIDFromProjectScoped(sessionID, authorizedProjectId)
	if err == sql.ErrNoRows {
		return ExplicitNetrunnerLaunchMetadata{}, fmt.Errorf("session not found in current project")
	}
	if err != nil {
		return ExplicitNetrunnerLaunchMetadata{}, fmt.Errorf("DB query error: %v", err)
	}

	belongs, err := sessionBelongsToProject(globalSessionID, authorizedProjectId)
	if err != nil {
		return ExplicitNetrunnerLaunchMetadata{}, fmt.Errorf("DB query error: %v", err)
	}
	if !belongs {
		return ExplicitNetrunnerLaunchMetadata{}, fmt.Errorf("session not found in current project")
	}

	sessionState, err := fetchSessionLifecycleState(globalSessionID, authorizedProjectId)
	if err != nil {
		return ExplicitNetrunnerLaunchMetadata{}, fmt.Errorf("DB query error: %v", err)
	}
	if len(sessionState.DeclaredWriteScope) == 0 {
		return ExplicitNetrunnerLaunchMetadata{}, fmt.Errorf("session %d must declare a non-empty write scope before explicit launch", sessionID)
	}
	if shouldRecommendRepairFork(sessionState.ReworkCount, sessionState.ForcedStopCount, sessionState.RepairSourceSessionID) &&
		strings.TrimSpace(input.SessionReuseOverrideReason) == "" {
		return ExplicitNetrunnerLaunchMetadata{}, fmt.Errorf(
			"session %d should be replaced with fork_repair_session_from before relaunch (rework_count=%d forced_stop_count=%d); provide session_reuse_override_reason to reuse it intentionally",
			sessionID,
			sessionState.ReworkCount,
			sessionState.ForcedStopCount,
		)
	}

	control, _, err := fetchOrchestrationControl(authorizedProjectId)
	if err != nil {
		return ExplicitNetrunnerLaunchMetadata{}, fmt.Errorf("DB query error: %v", err)
	}
	if control.OrchestrationFrozen {
		return ExplicitNetrunnerLaunchMetadata{}, fmt.Errorf("orchestration is frozen for project %d; explicit resume is required before launch", authorizedProjectId)
	}

	activeSessions, err := loadActiveLaunchSessions(authorizedProjectId)
	if err != nil {
		return ExplicitNetrunnerLaunchMetadata{}, fmt.Errorf("failed to inspect active worker processes: %v", err)
	}
	concurrentLaunch := len(activeSessions) > 0
	writeScopeOverrideUsed := false
	if concurrentLaunch {
		if strings.TrimSpace(sessionState.ParallelWaveID) == "" {
			return ExplicitNetrunnerLaunchMetadata{}, fmt.Errorf("parallel launch rejected for session %d: active workers already exist and this session has no explicit parallel_wave_id", sessionID)
		}
		if containsFoundationWriteScope(sessionState.DeclaredWriteScope) {
			return ExplicitNetrunnerLaunchMetadata{}, fmt.Errorf("parallel launch rejected for session %d: declared write scope touches a foundation/bootstrap layer", sessionID)
		}

		overlappingSessions := make([]int, 0, len(activeSessions))
		for _, activeSession := range activeSessions {
			if activeSession.GlobalSessionID == globalSessionID {
				return ExplicitNetrunnerLaunchMetadata{}, fmt.Errorf("session %d already has an active worker process", sessionID)
			}
			if strings.TrimSpace(activeSession.ParallelWaveID) == "" || activeSession.ParallelWaveID != sessionState.ParallelWaveID {
				return ExplicitNetrunnerLaunchMetadata{}, fmt.Errorf(
					"parallel launch rejected for session %d: active session %d is not in the same explicit parallel wave %q",
					sessionID,
					activeSession.LocalSessionID,
					sessionState.ParallelWaveID,
				)
			}
			if containsFoundationWriteScope(activeSession.DeclaredWriteScope) {
				return ExplicitNetrunnerLaunchMetadata{}, fmt.Errorf(
					"parallel launch rejected for session %d: active session %d owns a foundation/bootstrap write scope",
					sessionID,
					activeSession.LocalSessionID,
				)
			}
			if writeScopesOverlap(sessionState.DeclaredWriteScope, activeSession.DeclaredWriteScope) {
				overlappingSessions = append(overlappingSessions, activeSession.LocalSessionID)
			}
		}
		if len(overlappingSessions) > 0 {
			if strings.TrimSpace(input.WriteScopeOverrideReason) == "" {
				return ExplicitNetrunnerLaunchMetadata{}, fmt.Errorf(
					"parallel launch rejected for session %d: declared write scope overlaps active sessions %v; provide write_scope_override_reason to override intentionally",
					sessionID,
					overlappingSessions,
				)
			}
			writeScopeOverrideUsed = true
		}
	}

	launchConfig, err := resolveSessionLaunchConfig(globalSessionID, authorizedProjectId, input.Backend, input.Model, input.Reasoning)
	if err != nil {
		return ExplicitNetrunnerLaunchMetadata{}, fmt.Errorf("failed to resolve session launch backend: %v", err)
	}

	projectCWD, err := projectCWDFromID(authorizedProjectId)
	if err != nil {
		return ExplicitNetrunnerLaunchMetadata{}, fmt.Errorf("failed to resolve project cwd: %v", err)
	}

	launcherScript, err := resolveExplicitLauncherScript()
	if err != nil {
		return ExplicitNetrunnerLaunchMetadata{}, err
	}

	commandArgs := []string{
		launcherScript,
		"launch-netrunner",
		"--cwd",
		projectCWD,
		"--session-id",
		fmt.Sprintf("%d", sessionID),
	}
	if trimmedFixerSessionID := strings.TrimSpace(input.FixerSessionId); trimmedFixerSessionID != "" {
		commandArgs = append(commandArgs, "--fixer-session-id", trimmedFixerSessionID)
	}
	commandArgs = append(commandArgs, "--backend", launchConfig.Backend)
	if strings.TrimSpace(launchConfig.Model) != "" {
		commandArgs = append(commandArgs, "--model", launchConfig.Model)
	}
	if strings.TrimSpace(launchConfig.Reasoning) != "" {
		commandArgs = append(commandArgs, "--reasoning", launchConfig.Reasoning)
	}

	command := execCommand("python3", commandArgs...)
	command.Env = os.Environ()
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if err := command.Start(); err != nil {
		return ExplicitNetrunnerLaunchMetadata{}, fmt.Errorf("failed to launch explicit netrunner: %v", err)
	}
	if command.Process != nil {
		if err := recordWorkerProcessLaunch(authorizedProjectId, globalSessionID, command.Process.Pid, control.OrchestrationEpoch); err != nil {
			_ = command.Process.Kill()
			_ = command.Process.Release()
			return ExplicitNetrunnerLaunchMetadata{}, fmt.Errorf("failed to persist worker process metadata: %v", err)
		}
	}
	if command.Process != nil {
		_ = command.Process.Release()
	}

	externalSessionID, err := waitForSessionExternalID(ctx, globalSessionID, launchConfig.Backend, 10*time.Second)
	if err != nil {
		return ExplicitNetrunnerLaunchMetadata{}, fmt.Errorf("failed while waiting for backend session metadata: %v", err)
	}

	log.Printf(
		"launch_explicit_netrunner project_id=%d session_id=%d backend=%q model=%q reasoning=%q fixer_session_id=%q external_session_id=%q",
		authorizedProjectId,
		sessionID,
		launchConfig.Backend,
		launchConfig.Model,
		launchConfig.Reasoning,
		strings.TrimSpace(input.FixerSessionId),
		externalSessionID,
	)

	legacyCodexSessionID := ""
	if launchConfig.Backend == defaultCliBackend {
		legacyCodexSessionID = externalSessionID
	}

	return ExplicitNetrunnerLaunchMetadata{
		SessionId:              sessionID,
		ProjectCwd:             projectCWD,
		LauncherScript:         launcherScript,
		Backend:                launchConfig.Backend,
		Model:                  launchConfig.Model,
		Reasoning:              launchConfig.Reasoning,
		ExternalSessionId:      externalSessionID,
		CodexSessionId:         legacyCodexSessionID,
		SpawnedBackground:      true,
		OrchestrationEpoch:     control.OrchestrationEpoch,
		DeclaredWriteScope:     append([]string{}, sessionState.DeclaredWriteScope...),
		ParallelWaveID:         sessionState.ParallelWaveID,
		ConcurrentLaunch:       concurrentLaunch,
		WriteScopeOverrideUsed: writeScopeOverrideUsed,
	}, nil
}

func fetchSessionWaitSnapshot(sessionID int, projectID int) (string, string, []int, string, string, string, string, error) {
	var status string
	var report string
	err := db.QueryRow(
		"SELECT status, COALESCE(report, '') FROM session WHERE id = ? AND project_id = ?",
		sessionID,
		projectID,
	).Scan(&status, &report)
	if err != nil {
		return "", "", nil, "", "", "", "", err
	}

	proposalIDs, err := projectScopedDocProposalIDsForSession(sessionID, projectID)
	if err != nil {
		return "", "", nil, "", "", "", "", err
	}

	launchConfig, err := readSessionLaunchConfig(sessionID, projectID)
	if err != nil {
		return "", "", nil, "", "", "", "", err
	}

	externalSessionID, err := fetchSessionExternalID(sessionID, launchConfig.Backend)
	if err != nil {
		return "", "", nil, "", "", "", "", err
	}

	return status, report, proposalIDs, launchConfig.Backend, launchConfig.Model, launchConfig.Reasoning, externalSessionID, nil
}

type explicitWaitCandidate struct {
	LocalSessionID  int
	GlobalSessionID int
	InitialStatus   string
}

type explicitWaitSnapshot struct {
	LocalSessionID        int
	Status                string
	Report                string
	ProposalIDs           []int
	Backend               string
	Model                 string
	Reasoning             string
	ExternalSessionID     string
	CodexSessionID        string
	LaunchEpoch           int
	CurrentEpoch          int
	OrchestrationFrozen   bool
	FollowUpAllowed       bool
	FollowUpBlockedReason string
	RepairForkRecommended bool
}

func resolveExplicitWaitCandidatesFromList(sessionIDs []int, projectID int) ([]explicitWaitCandidate, error) {
	normalizedIDs := make([]int, 0, len(sessionIDs))
	seen := make(map[int]struct{}, len(sessionIDs))
	for _, sessionID := range sessionIDs {
		if sessionID <= 0 {
			return nil, fmt.Errorf("session_ids must contain only positive project-scoped ids")
		}
		if _, exists := seen[sessionID]; exists {
			continue
		}
		seen[sessionID] = struct{}{}
		normalizedIDs = append(normalizedIDs, sessionID)
	}
	sort.Ints(normalizedIDs)
	if len(normalizedIDs) == 0 {
		return nil, fmt.Errorf("session_ids must contain at least one project-scoped id")
	}

	candidates := make([]explicitWaitCandidate, 0, len(normalizedIDs))
	for _, localSessionID := range normalizedIDs {
		globalSessionID, err := globalSessionIDFromProjectScoped(localSessionID, projectID)
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("session %d not found in current project", localSessionID)
		}
		if err != nil {
			return nil, fmt.Errorf("DB query error: %v", err)
		}

		var initialStatus string
		err = db.QueryRow(
			"SELECT status FROM session WHERE id = ? AND project_id = ?",
			globalSessionID,
			projectID,
		).Scan(&initialStatus)
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("session %d not found in current project", localSessionID)
		}
		if err != nil {
			return nil, fmt.Errorf("DB query error: %v", err)
		}

		candidates = append(candidates, explicitWaitCandidate{
			LocalSessionID:  localSessionID,
			GlobalSessionID: globalSessionID,
			InitialStatus:   initialStatus,
		})
	}

	return candidates, nil
}

func discoverProjectWaitCandidates(projectID int) ([]explicitWaitCandidate, error) {
	rows, err := db.Query(
		`SELECT target.id,
		        (
		          SELECT COUNT(*)
		          FROM session ranked
		          WHERE ranked.project_id = ? AND ranked.id <= target.id
		        ) AS local_session_id,
		        target.status
		 FROM session AS target
		 WHERE target.project_id = ?
		   AND (
		     target.status IN ('in_progress', 'review', 'completed')
		     OR (
		       target.status = 'pending'
		       AND EXISTS (
		         SELECT 1
		         FROM worker_process process
		         WHERE process.session_id = target.id
		           AND process.project_id = target.project_id
		           AND process.status = 'running'
		       )
		     )
		   )
		 ORDER BY target.id`,
		projectID,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	candidates := []explicitWaitCandidate{}
	for rows.Next() {
		var candidate explicitWaitCandidate
		if err := rows.Scan(&candidate.GlobalSessionID, &candidate.LocalSessionID, &candidate.InitialStatus); err != nil {
			return nil, err
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return candidates, nil
}

func fetchExplicitWaitSnapshot(candidate explicitWaitCandidate, projectID int, control orchestrationControl) (explicitWaitSnapshot, error) {
	status, report, proposalIDs, backend, model, reasoning, externalSessionID, err := fetchSessionWaitSnapshot(candidate.GlobalSessionID, projectID)
	if err != nil {
		return explicitWaitSnapshot{}, err
	}
	sessionState, err := fetchSessionLifecycleState(candidate.GlobalSessionID, projectID)
	if err != nil {
		return explicitWaitSnapshot{}, err
	}
	launchEpoch, err := latestWorkerLaunchEpoch(candidate.GlobalSessionID, projectID)
	if err != nil {
		return explicitWaitSnapshot{}, err
	}
	followUpAllowed, blockedReason := waitFollowUpDecision(control, launchEpoch)
	return explicitWaitSnapshot{
		LocalSessionID:        candidate.LocalSessionID,
		Status:                status,
		Report:                report,
		ProposalIDs:           proposalIDs,
		Backend:               backend,
		Model:                 model,
		Reasoning:             reasoning,
		ExternalSessionID:     externalSessionID,
		CodexSessionID:        func() string {
			if backend == defaultCliBackend {
				return externalSessionID
			}
			return ""
		}(),
		LaunchEpoch:           launchEpoch,
		CurrentEpoch:          control.OrchestrationEpoch,
		OrchestrationFrozen:   control.OrchestrationFrozen,
		FollowUpAllowed:       followUpAllowed,
		FollowUpBlockedReason: blockedReason,
		RepairForkRecommended: shouldRecommendRepairFork(sessionState.ReworkCount, sessionState.ForcedStopCount, sessionState.RepairSourceSessionID),
	}, nil
}

func classifyWaitTerminalCondition(initialStatus string, currentStatus string, seenActive bool) (bool, string) {
	switch currentStatus {
	case "review":
		return true, "review_ready"
	case "completed":
		return true, "completed"
	case "pending":
		if seenActive || initialStatus == "in_progress" || initialStatus == "review" || initialStatus == "completed" {
			return true, "requeued_for_rework"
		}
	}
	return false, ""
}

func waitForNetrunnerSessionsResult(ctx context.Context, sessionIDs []int, timeoutSeconds int, pollIntervalSeconds int) (ExplicitNetrunnerWaitAnyResult, error) {
	timeoutSeconds, err := explicitWaitTimeoutSeconds(timeoutSeconds)
	if err != nil {
		return ExplicitNetrunnerWaitAnyResult{}, err
	}
	pollIntervalSeconds, err = explicitWaitPollIntervalSeconds(pollIntervalSeconds)
	if err != nil {
		return ExplicitNetrunnerWaitAnyResult{}, err
	}

	selectionMode := "explicit_list"
	candidates := []explicitWaitCandidate{}
	if len(sessionIDs) > 0 {
		candidates, err = resolveExplicitWaitCandidatesFromList(sessionIDs, authorizedProjectId)
		if err != nil {
			return ExplicitNetrunnerWaitAnyResult{}, err
		}
	} else {
		selectionMode = "auto_project_candidates"
		candidates, err = discoverProjectWaitCandidates(authorizedProjectId)
		if err != nil {
			return ExplicitNetrunnerWaitAnyResult{}, fmt.Errorf("DB query error: %v", err)
		}
		if len(candidates) == 0 {
			return ExplicitNetrunnerWaitAnyResult{}, fmt.Errorf("no active explicit-launch wait candidates found in current project")
		}
	}

	consideredSessionIDs := make([]int, 0, len(candidates))
	seenActive := make(map[int]bool, len(candidates))
	for _, candidate := range candidates {
		consideredSessionIDs = append(consideredSessionIDs, candidate.LocalSessionID)
		seenActive[candidate.LocalSessionID] = candidate.InitialStatus == "in_progress" || candidate.InitialStatus == "review" || candidate.InitialStatus == "completed"
	}

	startedAt := time.Now()
	deadline := startedAt.Add(time.Duration(timeoutSeconds) * time.Second)

	buildTimeoutResult := func() ExplicitNetrunnerWaitAnyResult {
		control, _, err := fetchOrchestrationControl(authorizedProjectId)
		if err != nil {
			control = orchestrationControl{ProjectID: authorizedProjectId, NotificationsEnabledForActiveRun: true}
		}
		return ExplicitNetrunnerWaitAnyResult{
			Terminal:             false,
			TerminalCondition:    "timed_out",
			TimedOut:             true,
			ElapsedSeconds:       int(time.Since(startedAt).Seconds()),
			TimeoutSeconds:       timeoutSeconds,
			PollIntervalSeconds:  pollIntervalSeconds,
			ProposalIds:          []int{},
			ConsideredSessionIds: append([]int{}, consideredSessionIDs...),
			SelectionMode:        selectionMode,
			FollowUpAllowed:      !control.OrchestrationFrozen,
			CurrentEpoch:         control.OrchestrationEpoch,
			OrchestrationFrozen:  control.OrchestrationFrozen,
		}
	}

	buildWinnerResult := func(snapshot explicitWaitSnapshot, terminalCondition string) ExplicitNetrunnerWaitAnyResult {
		proposalIDs := snapshot.ProposalIDs
		if proposalIDs == nil {
			proposalIDs = []int{}
		}
		legacyCodexSessionID := ""
		if snapshot.Backend == defaultCliBackend {
			legacyCodexSessionID = snapshot.ExternalSessionID
		}
		return ExplicitNetrunnerWaitAnyResult{
			WinningSessionId:      snapshot.LocalSessionID,
			SessionStatus:         snapshot.Status,
			Backend:               snapshot.Backend,
			Model:                 snapshot.Model,
			Reasoning:             snapshot.Reasoning,
			ExternalSessionId:     snapshot.ExternalSessionID,
			Terminal:              true,
			TerminalCondition:     terminalCondition,
			TimedOut:              false,
			ElapsedSeconds:        int(time.Since(startedAt).Seconds()),
			TimeoutSeconds:        timeoutSeconds,
			PollIntervalSeconds:   pollIntervalSeconds,
			Report:                snapshot.Report,
			ProposalIds:           proposalIDs,
			CodexSessionId:        legacyCodexSessionID,
			ConsideredSessionIds:  append([]int{}, consideredSessionIDs...),
			SelectionMode:         selectionMode,
			FollowUpAllowed:       snapshot.FollowUpAllowed,
			FollowUpBlockedReason: snapshot.FollowUpBlockedReason,
			LaunchEpoch:           snapshot.LaunchEpoch,
			CurrentEpoch:          snapshot.CurrentEpoch,
			OrchestrationFrozen:   snapshot.OrchestrationFrozen,
			RepairForkRecommended: snapshot.RepairForkRecommended,
		}
	}

	for {
		if err := ctx.Err(); err != nil {
			return ExplicitNetrunnerWaitAnyResult{}, err
		}

		control, _, err := fetchOrchestrationControl(authorizedProjectId)
		if err != nil {
			return ExplicitNetrunnerWaitAnyResult{}, fmt.Errorf("DB query error: %v", err)
		}
		for _, candidate := range candidates {
			snapshot, err := fetchExplicitWaitSnapshot(candidate, authorizedProjectId, control)
			if err != nil {
				return ExplicitNetrunnerWaitAnyResult{}, fmt.Errorf("DB query error: %v", err)
			}
			if snapshot.Status == "in_progress" || snapshot.Status == "review" || snapshot.Status == "completed" {
				seenActive[candidate.LocalSessionID] = true
			}
			terminal, terminalCondition := classifyWaitTerminalCondition(candidate.InitialStatus, snapshot.Status, seenActive[candidate.LocalSessionID])
			if terminal {
				return buildWinnerResult(snapshot, terminalCondition), nil
			}
		}

		if time.Now().After(deadline) {
			return buildTimeoutResult(), nil
		}

		time.Sleep(time.Duration(pollIntervalSeconds) * time.Second)
	}
}

func waitForNetrunnerSessionResult(ctx context.Context, sessionID int, timeoutSeconds int, pollIntervalSeconds int) (ExplicitNetrunnerWaitResult, error) {
	timeoutSeconds, err := explicitWaitTimeoutSeconds(timeoutSeconds)
	if err != nil {
		return ExplicitNetrunnerWaitResult{}, err
	}
	pollIntervalSeconds, err = explicitWaitPollIntervalSeconds(pollIntervalSeconds)
	if err != nil {
		return ExplicitNetrunnerWaitResult{}, err
	}

	globalSessionID, err := globalSessionIDFromProjectScoped(sessionID, authorizedProjectId)
	if err == sql.ErrNoRows {
		return ExplicitNetrunnerWaitResult{}, fmt.Errorf("session not found in current project")
	}
	if err != nil {
		return ExplicitNetrunnerWaitResult{}, fmt.Errorf("DB query error: %v", err)
	}

	belongs, err := sessionBelongsToProject(globalSessionID, authorizedProjectId)
	if err != nil {
		return ExplicitNetrunnerWaitResult{}, fmt.Errorf("DB query error: %v", err)
	}
	if !belongs {
		return ExplicitNetrunnerWaitResult{}, fmt.Errorf("session not found in current project")
	}

	initialStatus, report, proposalIDs, backend, model, reasoning, externalSessionID, err := fetchSessionWaitSnapshot(globalSessionID, authorizedProjectId)
	if err != nil {
		return ExplicitNetrunnerWaitResult{}, fmt.Errorf("DB query error: %v", err)
	}
	currentStatus := initialStatus

	startedAt := time.Now()
	deadline := startedAt.Add(time.Duration(timeoutSeconds) * time.Second)
	seenActive := initialStatus == "in_progress" || initialStatus == "review" || initialStatus == "completed"
	initialLaunchEpoch, err := latestWorkerLaunchEpoch(globalSessionID, authorizedProjectId)
	if err != nil {
		return ExplicitNetrunnerWaitResult{}, fmt.Errorf("DB query error: %v", err)
	}

	buildResult := func(currentStatus string, terminal bool, terminalCondition string, timedOut bool, currentReport string, currentProposalIDs []int, currentBackend string, currentModel string, currentReasoning string, currentExternalSessionID string, control orchestrationControl, launchEpoch int, repairForkRecommended bool) ExplicitNetrunnerWaitResult {
		if currentProposalIDs == nil {
			currentProposalIDs = []int{}
		}
		legacyCodexSessionID := ""
		if currentBackend == defaultCliBackend {
			legacyCodexSessionID = currentExternalSessionID
		}
		followUpAllowed, blockedReason := waitFollowUpDecision(control, launchEpoch)
		return ExplicitNetrunnerWaitResult{
			SessionId:             sessionID,
			SessionStatus:         currentStatus,
			Backend:               currentBackend,
			Model:                 currentModel,
			Reasoning:             currentReasoning,
			ExternalSessionId:     currentExternalSessionID,
			Terminal:              terminal,
			TerminalCondition:     terminalCondition,
			TimedOut:              timedOut,
			ElapsedSeconds:        int(time.Since(startedAt).Seconds()),
			TimeoutSeconds:        timeoutSeconds,
			PollIntervalSeconds:   pollIntervalSeconds,
			Report:                currentReport,
			ProposalIds:           currentProposalIDs,
			CodexSessionId:        legacyCodexSessionID,
			FollowUpAllowed:       followUpAllowed,
			FollowUpBlockedReason: blockedReason,
			LaunchEpoch:           launchEpoch,
			CurrentEpoch:          control.OrchestrationEpoch,
			OrchestrationFrozen:   control.OrchestrationFrozen,
			RepairForkRecommended: repairForkRecommended,
		}
	}

	for {
		if err := ctx.Err(); err != nil {
			return ExplicitNetrunnerWaitResult{}, err
		}

		control, _, err := fetchOrchestrationControl(authorizedProjectId)
		if err != nil {
			return ExplicitNetrunnerWaitResult{}, fmt.Errorf("DB query error: %v", err)
		}
		sessionState, err := fetchSessionLifecycleState(globalSessionID, authorizedProjectId)
		if err != nil {
			return ExplicitNetrunnerWaitResult{}, fmt.Errorf("DB query error: %v", err)
		}

		if currentStatus == "in_progress" || currentStatus == "review" || currentStatus == "completed" {
			seenActive = true
		}

		terminal, terminalCondition := classifyWaitTerminalCondition(initialStatus, currentStatus, seenActive)
		if terminal {
			return buildResult(currentStatus, true, terminalCondition, false, report, proposalIDs, backend, model, reasoning, externalSessionID, control, initialLaunchEpoch, shouldRecommendRepairFork(sessionState.ReworkCount, sessionState.ForcedStopCount, sessionState.RepairSourceSessionID)), nil
		}

		if time.Now().After(deadline) {
			return buildResult(currentStatus, false, "timed_out", true, report, proposalIDs, backend, model, reasoning, externalSessionID, control, initialLaunchEpoch, shouldRecommendRepairFork(sessionState.ReworkCount, sessionState.ForcedStopCount, sessionState.RepairSourceSessionID)), nil
		}

		time.Sleep(time.Duration(pollIntervalSeconds) * time.Second)

		currentStatus, report, proposalIDs, backend, model, reasoning, externalSessionID, err = fetchSessionWaitSnapshot(globalSessionID, authorizedProjectId)
		if err != nil {
			return ExplicitNetrunnerWaitResult{}, fmt.Errorf("DB query error: %v", err)
		}
	}
}

func LaunchExplicitNetrunner(ctx context.Context, req *mcp.CallToolRequest, input LaunchExplicitNetrunnerInput) (*mcp.CallToolResult, LaunchExplicitNetrunnerOutput, error) {
	if authorizedRole != "fixer" {
		return &mcp.CallToolResult{IsError: true}, LaunchExplicitNetrunnerOutput{}, fmt.Errorf("access denied: requires fixer role")
	}

	launch, err := launchExplicitNetrunnerWithMetadata(ctx, input)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, LaunchExplicitNetrunnerOutput{}, err
	}

	return nil, LaunchExplicitNetrunnerOutput{
		Status: "success",
		Launch: launch,
	}, nil
}

func WaitForNetrunnerSession(ctx context.Context, req *mcp.CallToolRequest, input WaitForNetrunnerSessionInput) (*mcp.CallToolResult, WaitForNetrunnerSessionOutput, error) {
	if authorizedRole != "fixer" {
		return &mcp.CallToolResult{IsError: true}, WaitForNetrunnerSessionOutput{}, fmt.Errorf("access denied: requires fixer role")
	}

	result, err := waitForNetrunnerSessionResult(ctx, input.SessionId, input.TimeoutSeconds, input.PollIntervalSeconds)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, WaitForNetrunnerSessionOutput{}, err
	}

	log.Printf("wait_for_netrunner_session project_id=%d session_id=%d terminal=%t condition=%q timed_out=%t status=%q", authorizedProjectId, input.SessionId, result.Terminal, result.TerminalCondition, result.TimedOut, result.SessionStatus)

	return nil, WaitForNetrunnerSessionOutput{
		Status: "success",
		Result: result,
	}, nil
}

func WaitForNetrunnerSessions(ctx context.Context, req *mcp.CallToolRequest, input WaitForNetrunnerSessionsInput) (*mcp.CallToolResult, WaitForNetrunnerSessionsOutput, error) {
	if authorizedRole != "fixer" {
		return &mcp.CallToolResult{IsError: true}, WaitForNetrunnerSessionsOutput{}, fmt.Errorf("access denied: requires fixer role")
	}

	result, err := waitForNetrunnerSessionsResult(ctx, input.SessionIds, input.TimeoutSeconds, input.PollIntervalSeconds)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, WaitForNetrunnerSessionsOutput{}, err
	}

	log.Printf(
		"wait_for_netrunner_sessions project_id=%d winner_session_id=%d terminal=%t condition=%q timed_out=%t status=%q mode=%q considered=%v",
		authorizedProjectId,
		result.WinningSessionId,
		result.Terminal,
		result.TerminalCondition,
		result.TimedOut,
		result.SessionStatus,
		result.SelectionMode,
		result.ConsideredSessionIds,
	)

	return nil, WaitForNetrunnerSessionsOutput{
		Status: "success",
		Result: result,
	}, nil
}

func LaunchAndWaitNetrunner(ctx context.Context, req *mcp.CallToolRequest, input LaunchAndWaitNetrunnerInput) (*mcp.CallToolResult, LaunchAndWaitNetrunnerOutput, error) {
	if authorizedRole != "fixer" {
		return &mcp.CallToolResult{IsError: true}, LaunchAndWaitNetrunnerOutput{}, fmt.Errorf("access denied: requires fixer role")
	}

	launch, err := launchExplicitNetrunnerWithMetadata(ctx, LaunchExplicitNetrunnerInput{
		SessionId:                  input.SessionId,
		FixerSessionId:             input.FixerSessionId,
		WriteScopeOverrideReason:   input.WriteScopeOverrideReason,
		SessionReuseOverrideReason: input.SessionReuseOverrideReason,
		Backend:                    input.Backend,
		Model:                      input.Model,
		Reasoning:                  input.Reasoning,
	})
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, LaunchAndWaitNetrunnerOutput{}, err
	}

	waitResult, err := waitForNetrunnerSessionResult(ctx, input.SessionId, input.TimeoutSeconds, input.PollIntervalSeconds)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, LaunchAndWaitNetrunnerOutput{}, err
	}

	return nil, LaunchAndWaitNetrunnerOutput{
		Status: "success",
		Launch: launch,
		Wait:   waitResult,
	}, nil
}

type WakeFixerAutonomousInput struct {
	SessionId int    `json:"session_id,omitempty" jsonschema:"Optional local session ID to wake the Fixer for. Defaults to the currently checked-out netrunner session."`
	Summary   string `json:"summary,omitempty" jsonschema:"Concise handoff summary for the Fixer resume prompt."`
}

type WakeFixerAutonomousOutput struct {
	Status            string `json:"status"`
	SessionId         int    `json:"session_id"`
	ProjectCwd        string `json:"project_cwd"`
	LauncherScript    string `json:"launcher_script"`
	FixerStateFile    string `json:"fixer_state_file"`
	SpawnedBackground bool   `json:"spawned_background"`
}

func WakeFixerAutonomous(ctx context.Context, req *mcp.CallToolRequest, input WakeFixerAutonomousInput) (*mcp.CallToolResult, WakeFixerAutonomousOutput, error) {
	if authorizedRole != "netrunner" {
		return &mcp.CallToolResult{IsError: true}, WakeFixerAutonomousOutput{}, fmt.Errorf("access denied: requires netrunner role")
	}

	globalSessionID := authorizedSessionId
	visibleSessionID := 0
	if input.SessionId > 0 {
		mappedGlobalSessionID, err := globalSessionIDFromProjectScoped(input.SessionId, authorizedProjectId)
		if err == sql.ErrNoRows {
			return &mcp.CallToolResult{IsError: true}, WakeFixerAutonomousOutput{}, fmt.Errorf("session not found in current project")
		}
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, WakeFixerAutonomousOutput{}, fmt.Errorf("DB query error: %v", err)
		}
		globalSessionID = mappedGlobalSessionID
		visibleSessionID = input.SessionId
	}

	if globalSessionID == 0 {
		return &mcp.CallToolResult{IsError: true}, WakeFixerAutonomousOutput{}, fmt.Errorf("no active netrunner session is checked out")
	}

	if visibleSessionID == 0 {
		mappedSessionID, err := projectScopedSessionIDFromGlobal(globalSessionID, authorizedProjectId)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, WakeFixerAutonomousOutput{}, fmt.Errorf("failed to map active session to project-scoped id: %v", err)
		}
		visibleSessionID = mappedSessionID
	}

	projectCWD, err := projectCWDFromID(authorizedProjectId)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, WakeFixerAutonomousOutput{}, fmt.Errorf("failed to resolve project cwd: %v", err)
	}

	launcherScript, err := resolveExplicitLauncherScript()
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, WakeFixerAutonomousOutput{}, fmt.Errorf("autonomous launcher script unavailable: %v", err)
	}

	fixerStateFile := filepath.Join(projectCWD, ".codex", "autonomous_resolution.json")
	if _, statErr := os.Stat(fixerStateFile); statErr != nil {
		return &mcp.CallToolResult{IsError: true}, WakeFixerAutonomousOutput{}, fmt.Errorf("autonomous fixer state file unavailable: %v", statErr)
	}

	command := execCommand(
		"python3",
		launcherScript,
		"resume-fixer",
		"--cwd",
		projectCWD,
		"--completed-session-id",
		fmt.Sprintf("%d", visibleSessionID),
		"--summary",
		strings.TrimSpace(input.Summary),
	)
	command.Env = os.Environ()
	command.Stdout = nil
	command.Stderr = nil

	if err := command.Start(); err != nil {
		return &mcp.CallToolResult{IsError: true}, WakeFixerAutonomousOutput{}, fmt.Errorf("failed to launch autonomous fixer resume: %v", err)
	}

	log.Printf("wake_fixer_autonomous project_id=%d session_id=%d summary=%q", authorizedProjectId, visibleSessionID, strings.TrimSpace(input.Summary))

	return nil, WakeFixerAutonomousOutput{
		Status:            "success",
		SessionId:         visibleSessionID,
		ProjectCwd:        projectCWD,
		LauncherScript:    launcherScript,
		FixerStateFile:    fixerStateFile,
		SpawnedBackground: true,
	}, nil
}
