package dashboardapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/glebarez/go-sqlite"
)

func TestRolesFromMarkersRecognizesCanonicalSkillNames(t *testing.T) {
	cases := []struct {
		name string
		text string
		want []string
	}{
		{
			name: "canonical fixer",
			text: "Activate skill `$init-fixer` immediately.",
			want: []string{"fixer"},
		},
		{
			name: "canonical overseer",
			text: "Activate skill `$init-overseer` immediately.",
			want: []string{"overseer"},
		},
		{
			name: "canonical netrunner",
			text: "Activate skill `$run-manual-netrunner` immediately.",
			want: []string{"netrunner"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := rolesFromMarkers(tc.text)
			if fmt.Sprint(got) != fmt.Sprint(tc.want) {
				t.Fatalf("rolesFromMarkers(%q) = %v, want %v", tc.text, got, tc.want)
			}
		})
	}
}

func TestHomeSnapshotAndProjectRoutes(t *testing.T) {
	repo := openFixtureRepository(t)
	defer repo.Close()

	server := httptest.NewServer(NewServer(repo))
	defer server.Close()

	var home HomeSnapshotResponse
	readJSON(t, server.URL+"/api/home", &home)
	if home.CurrentProject == nil || home.CurrentProject.Name != "Fixer MCP" {
		t.Fatalf("expected current project binding, got %+v", home.CurrentProject)
	}
	if len(home.Projects) != 2 {
		t.Fatalf("expected 2 project cards, got %d", len(home.Projects))
	}
	if !home.Projects[0].HasActiveWorkers {
		t.Fatalf("expected active worker flag on first project")
	}
	if home.DefaultChatBinding.Supported {
		t.Fatalf("expected home snapshot to defer chat binding, got %+v", home.DefaultChatBinding)
	}
	if !strings.Contains(home.DefaultChatBinding.ResidualRisk, "loaded separately") {
		t.Fatalf("expected deferred chat binding note, got %+v", home.DefaultChatBinding)
	}

	var snapshot ProjectSnapshotResponse
	readJSON(t, server.URL+"/api/projects/1/snapshot", &snapshot)
	if snapshot.Project.Name != "Fixer MCP" {
		t.Fatalf("unexpected project snapshot: %+v", snapshot.Project)
	}
	if snapshot.Metrics.Counts.InProgress != 1 || snapshot.Metrics.Counts.Review != 1 {
		t.Fatalf("unexpected counts: %+v", snapshot.Metrics.Counts)
	}
	if snapshot.Autonomous == nil || !snapshot.Autonomous.OrchestrationFrozen {
		t.Fatalf("expected autonomous frozen status, got %+v", snapshot.Autonomous)
	}
	if !snapshot.FixerChat.Supported || snapshot.FixerChat.DefaultSession == nil {
		t.Fatalf("expected supported fixer chat binding, got %+v", snapshot.FixerChat)
	}
	if snapshot.FixerChat.DefaultSession.AgentRole != "fixer" || snapshot.FixerChat.DefaultSession.Status != "active" {
		t.Fatalf("expected active fixer binding, got %+v", snapshot.FixerChat.DefaultSession)
	}
	if !snapshot.FixerChat.DefaultSession.Transcript {
		t.Fatalf("expected active fixer transcript flag, got %+v", snapshot.FixerChat.DefaultSession)
	}
	if len(snapshot.FixerChat.Sessions) < 2 {
		t.Fatalf("expected active and historical fixer threads, got %+v", snapshot.FixerChat.Sessions)
	}
	if duplicate := duplicateChatSessionID(snapshot.FixerChat.Sessions); duplicate != "" {
		t.Fatalf("expected de-duplicated fixer chat sessions, found duplicate %s in %+v", duplicate, snapshot.FixerChat.Sessions)
	}

	var overview ProjectSnapshotResponse
	readJSON(t, server.URL+"/api/projects/1/overview", &overview)
	if overview.Project.Name != "Fixer MCP" {
		t.Fatalf("unexpected project overview: %+v", overview.Project)
	}
	if overview.FixerChat.Supported {
		t.Fatalf("expected overview to defer fixer chat binding, got %+v", overview.FixerChat)
	}

	var overseerBinding FixerChatBinding
	readJSON(t, server.URL+"/api/projects/1/overseer-chat-binding", &overseerBinding)
	if !overseerBinding.Supported || overseerBinding.DefaultSession == nil {
		t.Fatalf("expected overseer chat binding endpoint, got %+v", overseerBinding)
	}
	if overseerBinding.DefaultSession.AgentRole != "overseer" || overseerBinding.DefaultSession.Status != "resume_alias" {
		t.Fatalf("expected overseer alias binding, got %+v", overseerBinding.DefaultSession)
	}

	var docs ProjectDocsResponse
	readJSON(t, server.URL+"/api/projects/1/docs", &docs)
	if docs.Docs.TotalDocs != 2 {
		t.Fatalf("expected 2 docs, got %d", docs.Docs.TotalDocs)
	}
	if docs.Docs.PendingProposalCount != 2 {
		t.Fatalf("expected 2 pending proposals, got %d", docs.Docs.PendingProposalCount)
	}

	var netrunners ProjectNetrunnersResponse
	readJSON(t, server.URL+"/api/projects/1/netrunners?status=in_progress,review", &netrunners)
	if len(netrunners.Sessions) != 2 {
		t.Fatalf("expected filtered sessions, got %d", len(netrunners.Sessions))
	}

	var detail NetrunnerDetailResponse
	readJSON(t, server.URL+"/api/sessions/11", &detail)
	if detail.Session.LocalID != 2 {
		t.Fatalf("expected local session id 2, got %d", detail.Session.LocalID)
	}
	if detail.Session.StructuredFinalReport == nil || len(detail.Session.StructuredFinalReport.FilesChanged) != 1 {
		t.Fatalf("expected parsed structured final report, got %+v", detail.Session.StructuredFinalReport)
	}
	if len(detail.Session.AttachedDocs) != 2 {
		t.Fatalf("expected attached docs, got %d", len(detail.Session.AttachedDocs))
	}
	if len(detail.Session.MCPServers) != 2 {
		t.Fatalf("expected mcp assignments, got %d", len(detail.Session.MCPServers))
	}
	if len(detail.Session.AvailableDocs) != 2 {
		t.Fatalf("expected available docs, got %d", len(detail.Session.AvailableDocs))
	}
	if len(detail.Session.AvailableMCPServers) != 2 {
		t.Fatalf("expected available mcp servers, got %d", len(detail.Session.AvailableMCPServers))
	}
	if len(detail.Session.AllowedStatusTargets) != 1 || detail.Session.AllowedStatusTargets[0] != "in_progress" {
		t.Fatalf("expected frozen status target note, got %+v", detail.Session.AllowedStatusTargets)
	}
	if !strings.Contains(detail.Session.StatusActionNote, "frozen") {
		t.Fatalf("expected truthful frozen note, got %q", detail.Session.StatusActionNote)
	}
}

func TestChatBindingIgnoresStaleActiveMarkerAfterTerminalAutonomousState(t *testing.T) {
	repo := openFixtureRepository(t)
	defer repo.Close()

	if _, err := repo.dbWrite.Exec("UPDATE autonomous_run_status SET state = 'completed' WHERE project_id = 1"); err != nil {
		t.Fatalf("mark autonomous state completed: %v", err)
	}

	binding, err := repo.loadChatBinding(context.Background(), 1, "fixer")
	if err != nil {
		t.Fatalf("load chat binding: %v", err)
	}
	if !binding.Supported || binding.DefaultSession == nil {
		t.Fatalf("expected supported fixer binding, got %+v", binding)
	}
	if binding.DefaultSession.Status == "active" {
		t.Fatalf("expected completed autonomous state to ignore stale active marker, got %+v", binding.DefaultSession)
	}
	for _, session := range binding.Sessions {
		if session.Status == "active" {
			t.Fatalf("expected no active chat sessions after completed autonomous state, got %+v", binding.Sessions)
		}
	}
}

func TestActionRoutesMutateSessionState(t *testing.T) {
	repo := openFixtureRepository(t)
	defer repo.Close()

	if _, err := repo.dbWrite.Exec("UPDATE autonomous_run_status SET orchestration_frozen = 0 WHERE project_id = 1"); err != nil {
		t.Fatalf("unfreeze fixture project: %v", err)
	}

	server := httptest.NewServer(NewServer(repo))
	defer server.Close()

	var createResp CreateTaskResponse
	postJSON(t, server.URL+"/api/actions/projects/1/tasks", map[string]any{
		"task_description": "Operator-created task",
		"declared_write_scope": []string{
			"fixer_mcp/dashboard_api",
		},
	}, &createResp)
	if createResp.Status != "success" || createResp.SessionID == 0 {
		t.Fatalf("unexpected create task response: %+v", createResp)
	}

	var attachResp SessionActionResponse
	postJSON(t, server.URL+"/api/actions/sessions/11/attached-docs", map[string]any{
		"project_doc_ids": []int{1},
	}, &attachResp)
	if len(attachResp.Session.Session.AttachedDocs) != 1 {
		t.Fatalf("expected 1 attached doc after update, got %+v", attachResp.Session.Session.AttachedDocs)
	}

	var mcpResp SessionActionResponse
	postJSON(t, server.URL+"/api/actions/sessions/11/mcp-servers", map[string]any{
		"mcp_server_names": []string{"sqlite"},
	}, &mcpResp)
	if len(mcpResp.Session.Session.MCPServers) != 1 || mcpResp.Session.Session.MCPServers[0].Name != "sqlite" {
		t.Fatalf("unexpected mcp response: %+v", mcpResp)
	}

	var statusResp SessionActionResponse
	postJSON(t, server.URL+"/api/actions/sessions/11/status", map[string]any{
		"status": "review",
	}, &statusResp)
	if statusResp.Session.Session.Status != "review" {
		t.Fatalf("expected review status, got %+v", statusResp.Session.Session.Status)
	}

	var proposalResp SessionActionResponse
	postJSON(t, server.URL+"/api/actions/proposals/1/status", map[string]any{
		"status": "approved",
	}, &proposalResp)
	if proposalResp.Session.Session.Proposals[0].Status != "approved" {
		t.Fatalf("expected approved proposal, got %+v", proposalResp.Session.Session.Proposals)
	}
	var updatedContent string
	if err := repo.db.QueryRow("SELECT content FROM project_doc WHERE id = 1").Scan(&updatedContent); err != nil {
		t.Fatalf("read updated doc: %v", err)
	}
	if updatedContent != "Update bridge brief" {
		t.Fatalf("unexpected updated doc content: %q", updatedContent)
	}
}

func TestHealthRoute(t *testing.T) {
	repo := openFixtureRepository(t)
	defer repo.Close()

	server := httptest.NewServer(NewServer(repo))
	defer server.Close()

	resp, err := http.Get(server.URL + "/health")
	if err != nil {
		t.Fatalf("health request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var payload HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	if payload.Status != "ok" {
		t.Fatalf("unexpected health payload: %+v", payload)
	}
}

func openFixtureRepository(t *testing.T) *Repository {
	t.Helper()

	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	dbPath := filepath.Join(tempDir, "fixer.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open fixture db: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	projectCWD := filepath.Join(tempDir, "fixer-project")
	if err := os.MkdirAll(projectCWD, 0o755); err != nil {
		t.Fatalf("mkdir project cwd: %v", err)
	}
	normalizedProjectCWD, err := normalizeProjectCWD(projectCWD)
	if err != nil {
		t.Fatalf("normalize project cwd: %v", err)
	}
	if err := seedFixtureDB(db, normalizedProjectCWD); err != nil {
		t.Fatalf("seed fixture db: %v", err)
	}
	if err := seedFixtureCodexLogs(tempDir, normalizedProjectCWD); err != nil {
		t.Fatalf("seed fixture codex logs: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close fixture db: %v", err)
	}

	repo, err := OpenRepository(dbPath, normalizedProjectCWD)
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	return repo
}

func seedFixtureDB(db *sql.DB, projectCWD string) error {
	schema := `
		CREATE TABLE project (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			cwd TEXT UNIQUE NOT NULL
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
			repair_source_session_id INTEGER,
			rework_count INTEGER NOT NULL DEFAULT 0,
			forced_stop_count INTEGER NOT NULL DEFAULT 0
		);
		CREATE TABLE project_doc (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER,
			title TEXT NOT NULL,
			content TEXT NOT NULL,
			doc_type TEXT DEFAULT 'documentation'
		);
		CREATE TABLE doc_proposal (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER,
			session_id INTEGER,
			status TEXT NOT NULL,
			proposed_content TEXT NOT NULL,
			proposed_doc_type TEXT DEFAULT 'documentation',
			target_project_doc_id INTEGER
		);
		CREATE TABLE mcp_server (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			short_description TEXT,
			long_description TEXT,
			auto_attach INTEGER NOT NULL DEFAULT 0,
			is_default INTEGER NOT NULL DEFAULT 0,
			category TEXT,
			how_to TEXT,
			auth_env_keys TEXT NOT NULL DEFAULT '',
			portability TEXT NOT NULL DEFAULT '',
			install_hint TEXT NOT NULL DEFAULT '',
			archived INTEGER NOT NULL DEFAULT 0
		);
		CREATE TABLE project_mcp_server (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL,
			mcp_server_id INTEGER NOT NULL,
			UNIQUE(project_id, mcp_server_id)
		);
		CREATE TABLE session_mcp_server (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id INTEGER NOT NULL,
			mcp_server_id INTEGER NOT NULL,
			UNIQUE(session_id, mcp_server_id)
		);
		CREATE TABLE netrunner_attached_doc (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id INTEGER NOT NULL,
			project_doc_id INTEGER NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(session_id, project_doc_id)
		);
		CREATE TABLE autonomous_run_status (
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
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(project_id)
		);
		CREATE TABLE worker_process (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL,
			session_id INTEGER NOT NULL,
			pid INTEGER NOT NULL,
				launch_epoch INTEGER NOT NULL DEFAULT 0,
				status TEXT NOT NULL DEFAULT 'running',
				stop_reason TEXT,
				launch_origin TEXT NOT NULL DEFAULT '',
				started_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
				stopped_at TEXT
		);
		CREATE TABLE fixer_resume_session_alias (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL,
			codex_session_id TEXT NOT NULL,
			note TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
	`
	if _, err := db.Exec(schema); err != nil {
		return err
	}

	report := `{"files_changed":["dashboard_api/repository.go"],"commands_run":["go test ./dashboard_api"],"checks_run":["go test ./dashboard_api"],"blockers":[]}`
	statements := []string{
		"INSERT INTO project (id, name, cwd) VALUES (1, 'Fixer MCP', '" + strings.ReplaceAll(projectCWD, "'", "''") + "')",
		"INSERT INTO project (id, name, cwd) VALUES (2, 'Another Project', '/tmp/another-project')",
		"INSERT INTO session (id, project_id, task_description, status, report, cli_backend, cli_model, cli_reasoning, declared_write_scope, rework_count, forced_stop_count) VALUES (10, 1, 'Foundation work', 'completed', '', 'codex', 'gpt-5.4', 'medium', '[\"fixer_mcp\"]', 0, 0)",
		"INSERT INTO session (id, project_id, task_description, status, report, cli_backend, cli_model, cli_reasoning, declared_write_scope, repair_source_session_id, rework_count, forced_stop_count) VALUES (11, 1, 'Implement read bridge\\nMore detail here', 'in_progress', '" + strings.ReplaceAll(report, "'", "''") + "', 'codex', 'gpt-5.4', 'medium', '[\"fixer_mcp\",\"dashboard_api\"]', 10, 1, 0)",
		"INSERT INTO session (id, project_id, task_description, status, report, cli_backend, cli_model, cli_reasoning, declared_write_scope, rework_count, forced_stop_count) VALUES (12, 1, 'Review endpoint contract', 'review', '', 'codex', 'gpt-5.4', 'medium', '[\"fixer_mcp\"]', 2, 1)",
		"INSERT INTO session (id, project_id, task_description, status, report, cli_backend, cli_model, cli_reasoning, declared_write_scope, rework_count, forced_stop_count) VALUES (20, 2, 'Other project session', 'pending', '', 'codex', 'gpt-5.4', 'medium', '[\".\"]', 0, 0)",
		"INSERT INTO project_doc (id, project_id, title, content, doc_type) VALUES (1, 1, 'Bridge Brief', 'Bridge contract details go here', 'architecture')",
		"INSERT INTO project_doc (id, project_id, title, content, doc_type) VALUES (2, 1, 'Runtime Modes', 'Runtime mode notes', 'documentation')",
		"INSERT INTO netrunner_attached_doc (session_id, project_doc_id) VALUES (11, 1)",
		"INSERT INTO netrunner_attached_doc (session_id, project_doc_id) VALUES (11, 2)",
		"INSERT INTO mcp_server (id, name, short_description, category, how_to) VALUES (1, 'sqlite', 'SQLite DB', 'DB', 'Use for local database checks')",
		"INSERT INTO mcp_server (id, name, short_description, category, how_to) VALUES (2, 'gopls', 'Go tools', 'Coding', 'Use for Go semantic tooling')",
		"INSERT INTO project_mcp_server (project_id, mcp_server_id) VALUES (1, 1)",
		"INSERT INTO project_mcp_server (project_id, mcp_server_id) VALUES (1, 2)",
		"INSERT INTO session_mcp_server (session_id, mcp_server_id) VALUES (11, 1)",
		"INSERT INTO session_mcp_server (session_id, mcp_server_id) VALUES (11, 2)",
		"INSERT INTO doc_proposal (id, project_id, session_id, status, proposed_content, proposed_doc_type, target_project_doc_id) VALUES (1, 1, 11, 'pending', 'Update bridge brief', 'architecture', 1)",
		"INSERT INTO doc_proposal (id, project_id, session_id, status, proposed_content, proposed_doc_type) VALUES (2, 1, 12, 'pending', 'General docs note', 'documentation')",
		"INSERT INTO autonomous_run_status (project_id, session_id, state, summary, focus, blocker, evidence, orchestration_epoch, orchestration_frozen, notifications_enabled_for_active_run) VALUES (1, 11, 'blocked', 'Waiting for review', 'bridge', 'frozen for review', 'seeded', 3, 1, 0)",
		"INSERT INTO worker_process (project_id, session_id, pid, launch_epoch, status) VALUES (1, 11, 999999, 3, 'running')",
		"INSERT INTO fixer_resume_session_alias (project_id, codex_session_id, note) VALUES (1, '019overseer-0000-0000-0000-000000000000', 'Archived Overseer thread')",
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func seedFixtureCodexLogs(homeDir string, projectCWD string) error {
	if err := os.MkdirAll(filepath.Join(projectCWD, ".codex"), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(
		filepath.Join(projectCWD, ".codex", "autonomous_resolution.json"),
		[]byte(`{"project_cwd":"`+projectCWD+`","fixer_codex_session_id":"019fixer-0000-0000-0000-000000000000"}`),
		0o644,
	); err != nil {
		return err
	}

	sessionsRoot := filepath.Join(homeDir, ".codex", "sessions", "2026", "04", "28")
	if err := os.MkdirAll(sessionsRoot, 0o755); err != nil {
		return err
	}

	logs := map[string]string{
		"rollout-2026-04-28T09-00-00-019overseer-0000-0000-0000-000000000000.jsonl": strings.Join([]string{
			fmt.Sprintf(`{"timestamp":"2026-04-28T09:00:00Z","type":"session_meta","payload":{"id":"019overseer-0000-0000-0000-000000000000","timestamp":"2026-04-28T09:00:00Z","cwd":"%s"}}`, projectCWD),
			`{"timestamp":"2026-04-28T09:00:05Z","type":"turn_context","payload":{"model":"gpt-5.4","effort":"medium"}}`,
			"{\"timestamp\":\"2026-04-28T09:00:10Z\",\"type\":\"user_message\",\"payload\":{\"text\":\"Activate skill `$init-overseer` immediately.\"}}",
			`{"timestamp":"2026-04-28T09:30:00Z","type":"assistant_message","payload":{"text":"Overseer note"}}`,
		}, "\n"),
		"rollout-2026-04-28T10-00-00-019fixer-0000-0000-0000-000000000000.jsonl": strings.Join([]string{
			fmt.Sprintf(`{"timestamp":"2026-04-28T10:00:00Z","type":"session_meta","payload":{"id":"019fixer-0000-0000-0000-000000000000","timestamp":"2026-04-28T10:00:00Z","cwd":"%s"}}`, projectCWD),
			`{"timestamp":"2026-04-28T10:00:05Z","type":"turn_context","payload":{"model":"gpt-5.4","effort":"medium"}}`,
			"{\"timestamp\":\"2026-04-28T10:00:10Z\",\"type\":\"user_message\",\"payload\":{\"text\":\"Activate skill `$init-fixer` immediately.\"}}",
			`{"timestamp":"2026-04-28T10:45:00Z","type":"assistant_message","payload":{"text":"Fixer note"}}`,
		}, "\n"),
		"rollout-2026-04-28T11-00-00-019resumed-fixer-filename.jsonl": strings.Join([]string{
			fmt.Sprintf(`{"timestamp":"2026-04-28T11:00:00Z","type":"session_meta","payload":{"id":"019fixer-0000-0000-0000-000000000000","timestamp":"2026-04-28T11:00:00Z","cwd":"%s"}}`, projectCWD),
			`{"timestamp":"2026-04-28T11:00:05Z","type":"turn_context","payload":{"model":"gpt-5.4","effort":"medium"}}`,
			"{\"timestamp\":\"2026-04-28T11:00:10Z\",\"type\":\"user_message\",\"payload\":{\"text\":\"Activate skill `$init-fixer` immediately.\"}}",
			`{"timestamp":"2026-04-28T11:15:00Z","type":"assistant_message","payload":{"text":"Fixer resumed note"}}`,
		}, "\n"),
		"rollout-2026-04-28T08-00-00-019ambiguous-0000-0000-0000-0000000000.jsonl": strings.Join([]string{
			fmt.Sprintf(`{"timestamp":"2026-04-28T08:00:00Z","type":"session_meta","payload":{"id":"019ambiguous-0000-0000-0000-0000000000","timestamp":"2026-04-28T08:00:00Z","cwd":"%s"}}`, projectCWD),
			`{"timestamp":"2026-04-28T08:00:05Z","type":"turn_context","payload":{"model":"gpt-5.4","effort":"medium"}}`,
			"{\"timestamp\":\"2026-04-28T08:00:10Z\",\"type\":\"user_message\",\"payload\":{\"text\":\"Activate skill `$init-fixer` immediately.\"}}",
			"{\"timestamp\":\"2026-04-28T08:00:11Z\",\"type\":\"user_message\",\"payload\":{\"text\":\"Activate skill `$run-manual-netrunner` immediately.\"}}",
			`{"timestamp":"2026-04-28T08:15:00Z","type":"assistant_message","payload":{"text":"Ambiguous note"}}`,
		}, "\n"),
	}

	for name, content := range logs {
		if err := os.WriteFile(filepath.Join(sessionsRoot, name), []byte(content+"\n"), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func duplicateChatSessionID(sessions []FixerChatSessionSummary) string {
	seen := map[string]bool{}
	for _, session := range sessions {
		if session.CodexSessionID == "" {
			continue
		}
		if seen[session.CodexSessionID] {
			return session.CodexSessionID
		}
		seen[session.CodexSessionID] = true
	}
	return ""
}

func readJSON(t *testing.T, url string, target any) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("get %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from %s, got %d", url, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		t.Fatalf("decode %s: %v", url, err)
	}
}

func postJSON(t *testing.T, url string, payload any, target any) {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal %s: %v", url, err)
	}
	resp, err := http.Post(url, "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("post %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var failure map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&failure)
		t.Fatalf("expected 200 from %s, got %d payload=%v", url, resp.StatusCode, failure)
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		t.Fatalf("decode %s: %v", url, err)
	}
}

func TestRepositoryHealth(t *testing.T) {
	repo := openFixtureRepository(t)
	defer repo.Close()

	health, err := repo.Health(context.Background())
	if err != nil {
		t.Fatalf("health failed: %v", err)
	}
	if health.Status != "ok" {
		t.Fatalf("unexpected health: %+v", health)
	}
}
