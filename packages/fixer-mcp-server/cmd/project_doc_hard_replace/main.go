package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "github.com/glebarez/go-sqlite"
	_ "github.com/lib/pq"
)

const (
	defaultTargetSQLite = "fixer.db"
	defaultSourceSchema = "public"
)

var schemaIdentRegex = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type config struct {
	sourceDSN        string
	sourceSchema     string
	targetSQLite     string
	backupDir        string
	reportPath       string
	apply            bool
	targetProjectID  map[int]struct{}
	repoCleanDocs    bool
	repoCleanDocsDir string
}

type sourceProject struct {
	ID        int64
	Name      string
	LocalPath string
}

type sourceDoc struct {
	ID        int64
	ProjectID int64
	DocType   string
	Content   string
}

type targetProject struct {
	ID   int
	Name string
	Cwd  string
}

type targetDoc struct {
	ID      int
	Title   string
	Content string
	DocType string
}

type incomingDoc struct {
	Title   string `json:"title"`
	DocType string `json:"doc_type"`
	Content string `json:"content,omitempty"`
}

type projectPlan struct {
	Target          targetProject
	Source          sourceProject
	Existing        []targetDoc
	Incoming        []incomingDoc
	ReplaceAll      bool
	ReplaceDocTypes []string
}

type backupReport struct {
	CreatedAt     string `json:"created_at"`
	Directory     string `json:"directory"`
	SQLiteCopy    string `json:"sqlite_copy"`
	PreflightJSON string `json:"preflight_json"`
}

type projectRunReport struct {
	TargetProjectID   int            `json:"target_project_id"`
	TargetProjectName string         `json:"target_project_name"`
	TargetCwd         string         `json:"target_cwd"`
	SourceProjectID   int64          `json:"source_project_id"`
	SourceProjectName string         `json:"source_project_name"`
	SourceLocalPath   string         `json:"source_local_path"`
	BeforeCount       int            `json:"before_count"`
	IncomingCount     int            `json:"incoming_count"`
	AfterCount        int            `json:"after_count,omitempty"`
	BeforeByType      map[string]int `json:"before_by_type"`
	IncomingByType    map[string]int `json:"incoming_by_type"`
	DeletedCount      int            `json:"deleted_count"`
	InsertedCount     int            `json:"inserted_count"`
	RemovedDocTypes   []string       `json:"removed_doc_types"`
	NewDocTypes       []string       `json:"new_doc_types"`
	RemovedTitles     []string       `json:"removed_titles_sample"`
}

type skippedProjectReport struct {
	TargetProjectID   int    `json:"target_project_id"`
	TargetProjectName string `json:"target_project_name"`
	TargetCwd         string `json:"target_cwd"`
	Reason            string `json:"reason"`
}

type unmatchedSourceProjectReport struct {
	SourceProjectID   int64  `json:"source_project_id"`
	SourceProjectName string `json:"source_project_name"`
	SourceLocalPath   string `json:"source_local_path"`
	Reason            string `json:"reason"`
}

type runReport struct {
	GeneratedAtUTC         string                         `json:"generated_at_utc"`
	Mode                   string                         `json:"mode"`
	SourceSchema           string                         `json:"source_schema"`
	TargetSQLite           string                         `json:"target_sqlite"`
	SourceProjectCount     int                            `json:"source_project_count"`
	TargetProjectCount     int                            `json:"target_project_count"`
	MatchedProjectCount    int                            `json:"matched_project_count"`
	SkippedProjects        []skippedProjectReport         `json:"skipped_projects"`
	UnmatchedSourceProject []unmatchedSourceProjectReport `json:"unmatched_source_projects"`
	Projects               []projectRunReport             `json:"projects"`
	Backup                 *backupReport                  `json:"backup,omitempty"`
}

func main() {
	cfg, err := parseConfig()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	ctx := context.Background()

	var sourceDB *sql.DB
	if !cfg.repoCleanDocs {
		sourceDB, err = sql.Open("postgres", cfg.sourceDSN)
		if err != nil {
			log.Fatalf("open source postgres: %v", err)
		}
		defer sourceDB.Close()

		if err := sourceDB.PingContext(ctx); err != nil {
			log.Fatalf("ping source postgres: %v", err)
		}
	}

	targetDB, err := sql.Open("sqlite", cfg.targetSQLite)
	if err != nil {
		log.Fatalf("open target sqlite: %v", err)
	}
	defer targetDB.Close()

	if err := targetDB.PingContext(ctx); err != nil {
		log.Fatalf("ping target sqlite: %v", err)
	}

	if _, err := targetDB.ExecContext(ctx, `
		PRAGMA busy_timeout = 5000;
		PRAGMA foreign_keys = ON;
	`); err != nil {
		log.Fatalf("init target sqlite pragmas: %v", err)
	}

	var report *runReport
	var plans []projectPlan
	if cfg.repoCleanDocs {
		report, plans, err = buildRepoCleanDocsPlan(ctx, targetDB, cfg)
	} else {
		report, plans, err = buildPlan(ctx, sourceDB, targetDB, cfg)
	}
	if err != nil {
		log.Fatalf("build migration plan: %v", err)
	}

	printSummary(report)

	if !cfg.apply {
		if err := writeJSONReport(cfg.reportPath, report); err != nil {
			log.Fatalf("write dry-run report: %v", err)
		}
		log.Printf("Dry run complete. Report: %s", cfg.reportPath)
		return
	}

	backup, err := createBackup(cfg.targetSQLite, cfg.backupDir, report)
	if err != nil {
		log.Fatalf("backup failed: %v", err)
	}
	report.Backup = backup

	if err := applyReplace(ctx, targetDB, plans); err != nil {
		log.Fatalf("apply replace failed: %v", err)
	}

	if err := enrichPostVerification(ctx, targetDB, report, plans); err != nil {
		log.Fatalf("post-verify failed: %v", err)
	}

	if err := writeJSONReport(cfg.reportPath, report); err != nil {
		log.Fatalf("write apply report: %v", err)
	}

	log.Printf("Hard replace complete. Report: %s", cfg.reportPath)
}

func parseConfig() (config, error) {
	var cfg config
	var projectIDsRaw string

	sourceDSNDefault := firstNonEmpty(
		strings.TrimSpace(os.Getenv("CODEX_HUB_POSTGRES_DSN")),
		strings.TrimSpace(os.Getenv("SOURCE_POSTGRES_DSN")),
		strings.TrimSpace(os.Getenv("POSTGRES_URL")),
	)

	flag.StringVar(&cfg.sourceDSN, "source-dsn", sourceDSNDefault, "Source Postgres DSN (or env CODEX_HUB_POSTGRES_DSN/SOURCE_POSTGRES_DSN/POSTGRES_URL)")
	flag.StringVar(&cfg.sourceSchema, "source-schema", defaultSourceSchema, "Source schema name containing project and project_doc")
	flag.StringVar(&cfg.targetSQLite, "target-sqlite", defaultTargetSQLite, "Target sqlite database path")
	flag.StringVar(&cfg.backupDir, "backup-dir", filepath.Join(".", "migration_backups"), "Directory for sqlite backup snapshots")
	flag.StringVar(&cfg.reportPath, "report", "", "Report output path (default: ./migration_reports/project_doc_replace_<timestamp>.json)")
	flag.BoolVar(&cfg.apply, "apply", false, "Apply hard replace. If false, only dry-run report is generated")
	flag.StringVar(&projectIDsRaw, "target-project-ids", "", "Optional comma-separated target project IDs to process (default: all mapped projects)")
	flag.BoolVar(&cfg.repoCleanDocs, "repo-clean-docs", false, "Replace documentation docs from repo-local clean docs instead of source Postgres")
	flag.StringVar(&cfg.repoCleanDocsDir, "repo-clean-docs-dir", filepath.Join("project_book", "clean_docs"), "Project-relative clean docs directory used by -repo-clean-docs")
	flag.Parse()

	cfg.sourceDSN = strings.TrimSpace(cfg.sourceDSN)
	cfg.repoCleanDocsDir = strings.TrimSpace(cfg.repoCleanDocsDir)
	if !cfg.repoCleanDocs && cfg.sourceDSN == "" {
		return cfg, errors.New("missing source DSN")
	}

	if !schemaIdentRegex.MatchString(cfg.sourceSchema) {
		return cfg, fmt.Errorf("invalid source schema %q", cfg.sourceSchema)
	}

	cfg.targetProjectID = make(map[int]struct{})
	projectIDsRaw = strings.TrimSpace(projectIDsRaw)
	if projectIDsRaw != "" {
		parts := strings.Split(projectIDsRaw, ",")
		for _, part := range parts {
			v := strings.TrimSpace(part)
			if v == "" {
				continue
			}
			id, err := strconv.Atoi(v)
			if err != nil {
				return cfg, fmt.Errorf("invalid target-project-ids value %q", v)
			}
			cfg.targetProjectID[id] = struct{}{}
		}
	}

	if cfg.reportPath == "" {
		now := time.Now().UTC().Format("20060102_150405")
		cfg.reportPath = filepath.Join(".", "migration_reports", "project_doc_replace_"+now+".json")
	}

	return cfg, nil
}

func buildRepoCleanDocsPlan(ctx context.Context, targetDB *sql.DB, cfg config) (*runReport, []projectPlan, error) {
	targetProjects, err := loadTargetProjects(ctx, targetDB, cfg.targetProjectID)
	if err != nil {
		return nil, nil, err
	}

	report := &runReport{
		GeneratedAtUTC:         time.Now().UTC().Format(time.RFC3339),
		Mode:                   "dry-run",
		SourceSchema:           "repo_clean_docs",
		TargetSQLite:           cfg.targetSQLite,
		SourceProjectCount:     0,
		TargetProjectCount:     len(targetProjects),
		SkippedProjects:        make([]skippedProjectReport, 0),
		Projects:               make([]projectRunReport, 0),
		UnmatchedSourceProject: make([]unmatchedSourceProjectReport, 0),
	}
	if cfg.apply {
		report.Mode = "apply"
	}

	plans := make([]projectPlan, 0, len(targetProjects))
	for _, tp := range targetProjects {
		existing, err := loadTargetDocs(ctx, targetDB, tp.ID)
		if err != nil {
			return nil, nil, fmt.Errorf("load target docs for project %d: %w", tp.ID, err)
		}

		incoming, cleanDocsRoot, err := loadRepoCleanDocs(tp, cfg.repoCleanDocsDir)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				report.SkippedProjects = append(report.SkippedProjects, skippedProjectReport{
					TargetProjectID:   tp.ID,
					TargetProjectName: tp.Name,
					TargetCwd:         tp.Cwd,
					Reason:            "repo clean docs directory not found",
				})
				continue
			}
			return nil, nil, fmt.Errorf("load repo clean docs for project %d: %w", tp.ID, err)
		}
		if len(incoming) == 0 {
			report.SkippedProjects = append(report.SkippedProjects, skippedProjectReport{
				TargetProjectID:   tp.ID,
				TargetProjectName: tp.Name,
				TargetCwd:         tp.Cwd,
				Reason:            "repo clean docs directory contains no markdown files",
			})
			continue
		}

		replacedExisting := filterTargetDocsByTypes(existing, []string{"documentation"})
		plans = append(plans, projectPlan{
			Target:          tp,
			Existing:        existing,
			Incoming:        incoming,
			ReplaceDocTypes: []string{"documentation"},
		})

		projectReport := projectRunReport{
			TargetProjectID:   tp.ID,
			TargetProjectName: tp.Name,
			TargetCwd:         tp.Cwd,
			SourceProjectName: "repo_clean_docs",
			SourceLocalPath:   cleanDocsRoot,
			BeforeCount:       len(replacedExisting),
			IncomingCount:     len(incoming),
			BeforeByType:      targetDocTypeCounts(replacedExisting),
			IncomingByType:    incomingDocTypeCounts(incoming),
			DeletedCount:      len(replacedExisting),
			InsertedCount:     len(incoming),
			RemovedTitles:     sampleRemovedTitles(replacedExisting, incoming, 15),
		}
		projectReport.RemovedDocTypes, projectReport.NewDocTypes = docTypeDiff(projectReport.BeforeByType, projectReport.IncomingByType)
		report.Projects = append(report.Projects, projectReport)
	}

	sort.Slice(report.Projects, func(i, j int) bool {
		return report.Projects[i].TargetProjectID < report.Projects[j].TargetProjectID
	})
	sort.Slice(report.SkippedProjects, func(i, j int) bool {
		return report.SkippedProjects[i].TargetProjectID < report.SkippedProjects[j].TargetProjectID
	})

	report.MatchedProjectCount = len(plans)
	return report, plans, nil
}

func buildPlan(ctx context.Context, sourceDB, targetDB *sql.DB, cfg config) (*runReport, []projectPlan, error) {
	sourceProjects, err := loadSourceProjects(ctx, sourceDB, cfg.sourceSchema)
	if err != nil {
		return nil, nil, err
	}

	targetProjects, err := loadTargetProjects(ctx, targetDB, cfg.targetProjectID)
	if err != nil {
		return nil, nil, err
	}

	sourceByPath := make(map[string]sourceProject, len(sourceProjects))
	for _, sp := range sourceProjects {
		normalized := normalizePath(sp.LocalPath)
		if normalized == "" {
			continue
		}
		if existing, exists := sourceByPath[normalized]; exists {
			return nil, nil, fmt.Errorf(
				"duplicate source project localPath mapping: %q between source project %d and %d",
				normalized,
				existing.ID,
				sp.ID,
			)
		}
		sourceByPath[normalized] = sp
	}

	report := &runReport{
		GeneratedAtUTC:         time.Now().UTC().Format(time.RFC3339),
		Mode:                   "dry-run",
		SourceSchema:           cfg.sourceSchema,
		TargetSQLite:           cfg.targetSQLite,
		SourceProjectCount:     len(sourceProjects),
		TargetProjectCount:     len(targetProjects),
		SkippedProjects:        make([]skippedProjectReport, 0),
		UnmatchedSourceProject: make([]unmatchedSourceProjectReport, 0),
		Projects:               make([]projectRunReport, 0),
	}
	if cfg.apply {
		report.Mode = "apply"
	}

	plans := make([]projectPlan, 0)
	usedSource := make(map[int64]struct{})

	for _, tp := range targetProjects {
		normalizedTargetPath := normalizePath(tp.Cwd)
		source, ok := sourceByPath[normalizedTargetPath]
		if !ok {
			report.SkippedProjects = append(report.SkippedProjects, skippedProjectReport{
				TargetProjectID:   tp.ID,
				TargetProjectName: tp.Name,
				TargetCwd:         tp.Cwd,
				Reason:            "no source project with matching localPath",
			})
			continue
		}

		usedSource[source.ID] = struct{}{}

		existing, err := loadTargetDocs(ctx, targetDB, tp.ID)
		if err != nil {
			return nil, nil, fmt.Errorf("load target docs for project %d: %w", tp.ID, err)
		}

		sourceDocs, err := loadSourceDocs(ctx, sourceDB, cfg.sourceSchema, source.ID)
		if err != nil {
			return nil, nil, fmt.Errorf("load source docs for source project %d: %w", source.ID, err)
		}

		incoming := buildIncomingDocs(sourceDocs)
		plans = append(plans, projectPlan{
			Target:     tp,
			Source:     source,
			Existing:   existing,
			Incoming:   incoming,
			ReplaceAll: true,
		})

		projectReport := projectRunReport{
			TargetProjectID:   tp.ID,
			TargetProjectName: tp.Name,
			TargetCwd:         tp.Cwd,
			SourceProjectID:   source.ID,
			SourceProjectName: source.Name,
			SourceLocalPath:   source.LocalPath,
			BeforeCount:       len(existing),
			IncomingCount:     len(incoming),
			BeforeByType:      targetDocTypeCounts(existing),
			IncomingByType:    incomingDocTypeCounts(incoming),
			DeletedCount:      len(existing),
			InsertedCount:     len(incoming),
			RemovedTitles:     sampleRemovedTitles(existing, incoming, 15),
		}
		projectReport.RemovedDocTypes, projectReport.NewDocTypes = docTypeDiff(projectReport.BeforeByType, projectReport.IncomingByType)
		report.Projects = append(report.Projects, projectReport)
	}

	for _, sp := range sourceProjects {
		if _, matched := usedSource[sp.ID]; matched {
			continue
		}
		report.UnmatchedSourceProject = append(report.UnmatchedSourceProject, unmatchedSourceProjectReport{
			SourceProjectID:   sp.ID,
			SourceProjectName: sp.Name,
			SourceLocalPath:   sp.LocalPath,
			Reason:            "no target project with matching cwd",
		})
	}

	sort.Slice(report.Projects, func(i, j int) bool {
		return report.Projects[i].TargetProjectID < report.Projects[j].TargetProjectID
	})
	sort.Slice(report.SkippedProjects, func(i, j int) bool {
		return report.SkippedProjects[i].TargetProjectID < report.SkippedProjects[j].TargetProjectID
	})
	sort.Slice(report.UnmatchedSourceProject, func(i, j int) bool {
		return report.UnmatchedSourceProject[i].SourceProjectID < report.UnmatchedSourceProject[j].SourceProjectID
	})

	report.MatchedProjectCount = len(plans)
	return report, plans, nil
}

func applyReplace(ctx context.Context, targetDB *sql.DB, plans []projectPlan) error {
	tx, err := targetDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	for _, plan := range plans {
		reattachSessionIDs, err := loadAttachedSessionIDsForPlan(ctx, tx, plan)
		if err != nil {
			return fmt.Errorf("load attachment sessions for project %d: %w", plan.Target.ID, err)
		}

		if plan.ReplaceAll {
			if _, err := tx.ExecContext(ctx, "DELETE FROM project_doc WHERE project_id = ?", plan.Target.ID); err != nil {
				return fmt.Errorf("delete target docs for project %d: %w", plan.Target.ID, err)
			}
		} else {
			for _, docType := range normalizeDocTypes(plan.ReplaceDocTypes) {
				if _, err := tx.ExecContext(
					ctx,
					"DELETE FROM project_doc WHERE project_id = ? AND COALESCE(doc_type, 'documentation') = ?",
					plan.Target.ID,
					docType,
				); err != nil {
					return fmt.Errorf("delete target docs for project %d doc_type=%q: %w", plan.Target.ID, docType, err)
				}
			}
		}

		for _, doc := range plan.Incoming {
			if _, err := tx.ExecContext(
				ctx,
				"INSERT INTO project_doc (project_id, title, content, doc_type) VALUES (?, ?, ?, ?)",
				plan.Target.ID,
				doc.Title,
				doc.Content,
				doc.DocType,
			); err != nil {
				return fmt.Errorf("insert doc for project %d doc_type=%q: %w", plan.Target.ID, doc.DocType, err)
			}
		}

		replacementDocIDs, err := loadReplacementDocIDsForPlan(ctx, tx, plan)
		if err != nil {
			return fmt.Errorf("load replacement docs for project %d: %w", plan.Target.ID, err)
		}
		for _, sessionID := range reattachSessionIDs {
			for _, docID := range replacementDocIDs {
				if _, err := tx.ExecContext(
					ctx,
					"INSERT OR IGNORE INTO netrunner_attached_doc (session_id, project_doc_id) VALUES (?, ?)",
					sessionID,
					docID,
				); err != nil {
					return fmt.Errorf("reattach docs for session %d in project %d: %w", sessionID, plan.Target.ID, err)
				}
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

func loadAttachedSessionIDsForPlan(ctx context.Context, tx *sql.Tx, plan projectPlan) ([]int, error) {
	query := `
		SELECT DISTINCT nad.session_id
		FROM netrunner_attached_doc nad
		INNER JOIN session s ON s.id = nad.session_id
		INNER JOIN project_doc d ON d.id = nad.project_doc_id
		WHERE s.project_id = ?
	`
	args := []any{plan.Target.ID}
	if !plan.ReplaceAll {
		docTypes := normalizeDocTypes(plan.ReplaceDocTypes)
		if len(docTypes) == 0 {
			return []int{}, nil
		}
		query += " AND COALESCE(d.doc_type, 'documentation') IN (" + sqlPlaceholders(len(docTypes)) + ")"
		for _, docType := range docTypes {
			args = append(args, docType)
		}
	}
	query += " ORDER BY nad.session_id"

	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	sessionIDs := make([]int, 0)
	for rows.Next() {
		var sessionID int
		if err := rows.Scan(&sessionID); err != nil {
			return nil, err
		}
		sessionIDs = append(sessionIDs, sessionID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return sessionIDs, nil
}

func loadReplacementDocIDsForPlan(ctx context.Context, tx *sql.Tx, plan projectPlan) ([]int, error) {
	query := "SELECT id FROM project_doc WHERE project_id = ?"
	args := []any{plan.Target.ID}
	if !plan.ReplaceAll {
		docTypes := normalizeDocTypes(plan.ReplaceDocTypes)
		if len(docTypes) == 0 {
			return []int{}, nil
		}
		query += " AND COALESCE(doc_type, 'documentation') IN (" + sqlPlaceholders(len(docTypes)) + ")"
		for _, docType := range docTypes {
			args = append(args, docType)
		}
	}
	query += " ORDER BY id"

	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	docIDs := make([]int, 0)
	for rows.Next() {
		var docID int
		if err := rows.Scan(&docID); err != nil {
			return nil, err
		}
		docIDs = append(docIDs, docID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return docIDs, nil
}

func enrichPostVerification(ctx context.Context, targetDB *sql.DB, report *runReport, plans []projectPlan) error {
	planByProjectID := make(map[int]projectPlan, len(plans))
	for _, plan := range plans {
		planByProjectID[plan.Target.ID] = plan
	}

	for i := range report.Projects {
		projectID := report.Projects[i].TargetProjectID
		plan, ok := planByProjectID[projectID]
		if !ok {
			return fmt.Errorf("missing plan metadata for project %d", projectID)
		}
		docs, err := loadTargetDocs(ctx, targetDB, projectID)
		if err != nil {
			return fmt.Errorf("load post-verify docs for project %d: %w", projectID, err)
		}
		report.Projects[i].AfterCount = len(docs)
		if len(docs) != expectedAfterCount(plan) {
			return fmt.Errorf(
				"post-verify mismatch for project %d: after=%d expected=%d",
				projectID,
				len(docs),
				expectedAfterCount(plan),
			)
		}
	}
	return nil
}

func loadSourceProjects(ctx context.Context, db *sql.DB, schema string) ([]sourceProject, error) {
	query := fmt.Sprintf(`SELECT id, name, COALESCE("localPath", '') FROM %s.project ORDER BY id`, schema)
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	projects := make([]sourceProject, 0)
	for rows.Next() {
		var p sourceProject
		if err := rows.Scan(&p.ID, &p.Name, &p.LocalPath); err != nil {
			return nil, err
		}
		projects = append(projects, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return projects, nil
}

func loadSourceDocs(ctx context.Context, db *sql.DB, schema string, projectID int64) ([]sourceDoc, error) {
	query := fmt.Sprintf(`SELECT id, "projectId", "docType", content FROM %s.project_doc WHERE "projectId" = $1 ORDER BY "docType", id`, schema)
	rows, err := db.QueryContext(ctx, query, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	docs := make([]sourceDoc, 0)
	for rows.Next() {
		var d sourceDoc
		if err := rows.Scan(&d.ID, &d.ProjectID, &d.DocType, &d.Content); err != nil {
			return nil, err
		}
		docs = append(docs, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return docs, nil
}

func loadTargetProjects(ctx context.Context, db *sql.DB, only map[int]struct{}) ([]targetProject, error) {
	query := "SELECT id, name, cwd FROM project ORDER BY id"
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	projects := make([]targetProject, 0)
	for rows.Next() {
		var p targetProject
		if err := rows.Scan(&p.ID, &p.Name, &p.Cwd); err != nil {
			return nil, err
		}
		if len(only) > 0 {
			if _, ok := only[p.ID]; !ok {
				continue
			}
		}
		projects = append(projects, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return projects, nil
}

func loadTargetDocs(ctx context.Context, db *sql.DB, projectID int) ([]targetDoc, error) {
	rows, err := db.QueryContext(
		ctx,
		"SELECT id, title, content, COALESCE(doc_type, 'documentation') FROM project_doc WHERE project_id = ? ORDER BY id",
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	docs := make([]targetDoc, 0)
	for rows.Next() {
		var d targetDoc
		if err := rows.Scan(&d.ID, &d.Title, &d.Content, &d.DocType); err != nil {
			return nil, err
		}
		docs = append(docs, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return docs, nil
}

func buildIncomingDocs(sourceDocs []sourceDoc) []incomingDoc {
	result := make([]incomingDoc, 0, len(sourceDocs))
	seenTitles := make(map[string]int, len(sourceDocs))

	for _, d := range sourceDocs {
		docType := strings.TrimSpace(d.DocType)
		if docType == "" {
			docType = "documentation"
		}
		baseTitle := "codex_hub/project_doc/" + sanitizeTitleFragment(docType)
		title := baseTitle
		if count, exists := seenTitles[baseTitle]; exists {
			count++
			seenTitles[baseTitle] = count
			title = fmt.Sprintf("%s#%d", baseTitle, count)
		} else {
			seenTitles[baseTitle] = 1
		}
		result = append(result, incomingDoc{
			Title:   title,
			DocType: docType,
			Content: d.Content,
		})
	}
	return result
}

func loadRepoCleanDocs(target targetProject, relativeDir string) ([]incomingDoc, string, error) {
	cleanDocsRoot := filepath.Join(target.Cwd, relativeDir)
	info, err := os.Stat(cleanDocsRoot)
	if err != nil {
		return nil, cleanDocsRoot, err
	}
	if !info.IsDir() {
		return nil, cleanDocsRoot, fmt.Errorf("clean docs path %q is not a directory", cleanDocsRoot)
	}

	relativePaths := make([]string, 0)
	if err := filepath.Walk(cleanDocsRoot, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(info.Name()), ".md") {
			return nil
		}
		relPath, err := filepath.Rel(target.Cwd, path)
		if err != nil {
			return err
		}
		relativePaths = append(relativePaths, filepath.ToSlash(relPath))
		return nil
	}); err != nil {
		return nil, cleanDocsRoot, err
	}

	sort.Strings(relativePaths)

	docs := make([]incomingDoc, 0, len(relativePaths))
	for _, relPath := range relativePaths {
		content, err := os.ReadFile(filepath.Join(target.Cwd, filepath.FromSlash(relPath)))
		if err != nil {
			return nil, cleanDocsRoot, err
		}
		docs = append(docs, incomingDoc{
			Title:   relPath,
			DocType: "documentation",
			Content: string(content),
		})
	}

	return docs, cleanDocsRoot, nil
}

func normalizePath(raw string) string {
	p := strings.TrimSpace(raw)
	if p == "" {
		return ""
	}
	p = filepath.Clean(p)
	if p != "/" {
		p = strings.TrimRight(p, `/\`)
	}
	p = filepath.ToSlash(p)
	if runtime.GOOS == "windows" {
		p = strings.ToLower(p)
	}
	return p
}

func targetDocTypeCounts(docs []targetDoc) map[string]int {
	counts := make(map[string]int)
	for _, d := range docs {
		counts[normalizeDocType(d.DocType)]++
	}
	return counts
}

func incomingDocTypeCounts(docs []incomingDoc) map[string]int {
	counts := make(map[string]int)
	for _, d := range docs {
		counts[normalizeDocType(d.DocType)]++
	}
	return counts
}

func filterTargetDocsByTypes(docs []targetDoc, docTypes []string) []targetDoc {
	typeSet := make(map[string]struct{}, len(docTypes))
	for _, docType := range normalizeDocTypes(docTypes) {
		typeSet[docType] = struct{}{}
	}
	if len(typeSet) == 0 {
		filtered := make([]targetDoc, len(docs))
		copy(filtered, docs)
		return filtered
	}

	filtered := make([]targetDoc, 0, len(docs))
	for _, doc := range docs {
		if _, ok := typeSet[normalizeDocType(doc.DocType)]; ok {
			filtered = append(filtered, doc)
		}
	}
	return filtered
}

func normalizeDocType(raw string) string {
	docType := strings.TrimSpace(raw)
	if docType == "" {
		return "documentation"
	}
	return docType
}

func normalizeDocTypes(docTypes []string) []string {
	seen := make(map[string]struct{}, len(docTypes))
	normalized := make([]string, 0, len(docTypes))
	for _, docType := range docTypes {
		key := normalizeDocType(docType)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, key)
	}
	sort.Strings(normalized)
	return normalized
}

func sqlPlaceholders(count int) string {
	if count <= 0 {
		return ""
	}
	return strings.TrimRight(strings.Repeat("?,", count), ",")
}

func expectedAfterCount(plan projectPlan) int {
	if plan.ReplaceAll {
		return len(plan.Incoming)
	}

	replacedDocTypes := make(map[string]struct{}, len(plan.ReplaceDocTypes))
	for _, docType := range normalizeDocTypes(plan.ReplaceDocTypes) {
		replacedDocTypes[docType] = struct{}{}
	}

	preservedCount := 0
	for _, doc := range plan.Existing {
		if _, replaced := replacedDocTypes[normalizeDocType(doc.DocType)]; replaced {
			continue
		}
		preservedCount++
	}
	return preservedCount + len(plan.Incoming)
}

func docTypeDiff(before, incoming map[string]int) ([]string, []string) {
	removed := make([]string, 0)
	added := make([]string, 0)

	for k := range before {
		if _, ok := incoming[k]; !ok {
			removed = append(removed, k)
		}
	}
	for k := range incoming {
		if _, ok := before[k]; !ok {
			added = append(added, k)
		}
	}
	sort.Strings(removed)
	sort.Strings(added)
	return removed, added
}

func sampleRemovedTitles(existing []targetDoc, incoming []incomingDoc, limit int) []string {
	incomingTitles := make(map[string]struct{}, len(incoming))
	for _, d := range incoming {
		incomingTitles[d.Title] = struct{}{}
	}

	removed := make([]string, 0)
	for _, d := range existing {
		if _, ok := incomingTitles[d.Title]; !ok {
			removed = append(removed, d.Title)
		}
	}
	sort.Strings(removed)
	if len(removed) > limit {
		return removed[:limit]
	}
	return removed
}

func sanitizeTitleFragment(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "documentation"
	}
	replacer := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		"\n", "_",
		"\r", "_",
		"\t", "_",
	)
	v = replacer.Replace(v)
	return v
}

func createBackup(targetSQLite, backupDir string, report *runReport) (*backupReport, error) {
	ts := time.Now().UTC().Format("20060102_150405")
	outDir := filepath.Join(backupDir, "project_doc_replace_"+ts)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, fmt.Errorf("create backup dir: %w", err)
	}

	sqliteBackupPath := filepath.Join(outDir, "fixer.db.backup")
	if err := copyFile(targetSQLite, sqliteBackupPath); err != nil {
		return nil, fmt.Errorf("copy sqlite db: %w", err)
	}

	preflightPath := filepath.Join(outDir, "preflight_report.json")
	if err := writeJSONReport(preflightPath, report); err != nil {
		return nil, fmt.Errorf("write preflight report: %w", err)
	}

	return &backupReport{
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		Directory:     outDir,
		SQLiteCopy:    sqliteBackupPath,
		PreflightJSON: preflightPath,
	}, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() {
		_ = out.Close()
	}()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

func writeJSONReport(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func printSummary(report *runReport) {
	var totalBefore int
	var totalIncoming int
	for _, p := range report.Projects {
		totalBefore += p.BeforeCount
		totalIncoming += p.IncomingCount
	}

	log.Printf("Mode: %s", report.Mode)
	log.Printf("Source projects: %d | Target projects: %d | Matched projects: %d", report.SourceProjectCount, report.TargetProjectCount, report.MatchedProjectCount)
	log.Printf("Docs before replace: %d | Docs incoming from source: %d", totalBefore, totalIncoming)
	log.Printf("Skipped target projects: %d | Unmatched source projects: %d", len(report.SkippedProjects), len(report.UnmatchedSourceProject))
}
