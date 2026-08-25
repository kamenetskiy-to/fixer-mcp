package dashboardapi

import (
	"context"
	"testing"
)

func TestLoadProjectsOrdersByLatestDurableActivity(t *testing.T) {
	repo := openFixtureRepository(t)
	defer repo.Close()

	// Normalize fixture rows that default to CURRENT_TIMESTAMP so seeded
	// activity below stays deterministic regardless of when the test runs.
	if _, err := repo.dbWrite.Exec(`
		UPDATE autonomous_run_status SET updated_at = '2026-07-23T07:00:00Z';
		UPDATE worker_process SET started_at = '2026-07-23T07:00:00Z', updated_at = '2026-07-23T07:00:00Z';
		UPDATE fixer_resume_session_alias SET created_at = '2026-07-23T07:00:00Z'
	`); err != nil {
		t.Fatalf("normalize fixture activity timestamps: %v", err)
	}

	if _, err := repo.dbWrite.Exec(`
		CREATE TABLE netrunner_session_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL,
			session_id INTEGER NOT NULL,
			log_type TEXT NOT NULL,
			log_text TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`); err != nil {
		t.Fatalf("create activity log table: %v", err)
	}
	if _, err := repo.dbWrite.Exec(`
		INSERT INTO netrunner_session_log
			(project_id, session_id, log_type, log_text, created_at)
		VALUES (1, 11, 'progress', 'older project activity', '2026-07-23T08:00:00Z'),
		       (2, 20, 'progress', 'newer project activity', '2026-07-23T09:00:00Z')
	`); err != nil {
		t.Fatalf("seed activity log: %v", err)
	}

	_, order, err := repo.loadProjects(context.Background())
	if err != nil {
		t.Fatalf("load projects: %v", err)
	}
	if len(order) != 2 || order[0] != 2 || order[1] != 1 {
		t.Fatalf("expected newest project first, got order %v", order)
	}

	if _, err := repo.dbWrite.Exec(`
		UPDATE autonomous_run_status
		SET updated_at = '2026-07-23T10:00:00Z'
		WHERE project_id = 1
	`); err != nil {
		t.Fatalf("seed newer fixer activity: %v", err)
	}
	_, order, err = repo.loadProjects(context.Background())
	if err != nil {
		t.Fatalf("reload projects: %v", err)
	}
	if len(order) != 2 || order[0] != 1 || order[1] != 2 {
		t.Fatalf("expected newest fixer activity first, got order %v", order)
	}
}

func TestLoadProjectActivityCountsOnlyActiveWaves(t *testing.T) {
	repo := openFixtureRepository(t)
	defer repo.Close()

	// Normalize fixture rows that default to CURRENT_TIMESTAMP so seeded
	// wave timestamps below stay deterministic regardless of when the test runs.
	if _, err := repo.dbWrite.Exec(`
		UPDATE autonomous_run_status SET updated_at = '2026-07-23T07:00:00Z';
		UPDATE worker_process SET started_at = '2026-07-23T07:00:00Z', updated_at = '2026-07-23T07:00:00Z';
		UPDATE fixer_resume_session_alias SET created_at = '2026-07-23T07:00:00Z'
	`); err != nil {
		t.Fatalf("normalize fixture activity timestamps: %v", err)
	}

	if _, err := repo.dbWrite.Exec(`
		CREATE TABLE parallel_wave (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL,
			status TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`); err != nil {
		t.Fatalf("create parallel wave table: %v", err)
	}
	if _, err := repo.dbWrite.Exec(`
		INSERT INTO parallel_wave (project_id, status, updated_at)
		VALUES (1, 'running', '2026-07-23T11:00:00Z'),
		       (1, 'review_ready', '2026-07-23T12:00:00Z'),
		       (1, 'completed', '2026-07-23T13:00:00Z'),
		       (2, 'failed', '2026-07-23T14:00:00Z')
	`); err != nil {
		t.Fatalf("seed parallel waves: %v", err)
	}

	activity, err := repo.loadProjectActivity(context.Background())
	if err != nil {
		t.Fatalf("load project activity: %v", err)
	}
	if activity[1].ActiveWaveCount != 2 {
		t.Fatalf("expected two active waves, got %+v", activity[1])
	}
	if activity[2].ActiveWaveCount != 0 {
		t.Fatalf("expected terminal waves to be excluded, got %+v", activity[2])
	}
	if activity[1].LastActivityAt != "2026-07-23T13:00:00Z" {
		t.Fatalf("expected latest wave activity timestamp, got %q", activity[1].LastActivityAt)
	}
}

func TestLoadProjectActivityIncludesDurableFixerSources(t *testing.T) {
	repo := openFixtureRepository(t)
	defer repo.Close()

	if _, err := repo.dbWrite.Exec(`
		CREATE TABLE project_handoff (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL,
			content TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);
		CREATE TABLE overseer_fixer_message (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL,
			sender_role TEXT NOT NULL,
			content TEXT NOT NULL,
			created_at TEXT NOT NULL
		);
		INSERT INTO project_handoff (project_id, content, updated_at)
		VALUES (2, 'handoff', '2026-07-23T14:00:00Z');
		INSERT INTO overseer_fixer_message (project_id, sender_role, content, created_at)
		VALUES (2, 'fixer', 'message', '2026-07-23T15:00:00Z');
	`); err != nil {
		t.Fatalf("seed durable fixer activity: %v", err)
	}

	activity, err := repo.loadProjectActivity(context.Background())
	if err != nil {
		t.Fatalf("load project activity: %v", err)
	}
	if !activity[2].HasFixer {
		t.Fatalf("expected durable handoff/message to mark fixer activity, got %+v", activity[2])
	}
	if activity[2].LastActivityAt != "2026-07-23T15:00:00Z" {
		t.Fatalf("expected latest durable fixer timestamp, got %q", activity[2].LastActivityAt)
	}
}
