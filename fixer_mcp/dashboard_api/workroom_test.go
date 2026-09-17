package dashboardapi

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	_ "github.com/glebarez/go-sqlite"
)

func openWorkroomRepository(t *testing.T) (*Repository, time.Time) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "workroom.db")
	seed, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open workroom seed: %v", err)
	}
	_, err = seed.Exec(`
		PRAGMA foreign_keys = ON;
		CREATE TABLE project (id INTEGER PRIMARY KEY, name TEXT NOT NULL, cwd TEXT NOT NULL);
		CREATE TABLE project_ui_cursor (project_id INTEGER PRIMARY KEY, next_seq INTEGER NOT NULL, updated_at TEXT NOT NULL);
		CREATE TABLE project_ui_event (
			project_id INTEGER NOT NULL, seq INTEGER NOT NULL, event_id TEXT NOT NULL UNIQUE,
			schema_version INTEGER NOT NULL, kind TEXT NOT NULL, aggregate_type TEXT NOT NULL,
			aggregate_id TEXT NOT NULL, aggregate_revision INTEGER NOT NULL, payload_json TEXT NOT NULL,
			actor_kind TEXT NOT NULL, actor_id TEXT NOT NULL, causation_id TEXT NOT NULL,
			correlation_id TEXT NOT NULL, created_at TEXT NOT NULL, PRIMARY KEY(project_id, seq)
		);
		CREATE TABLE command_dedup (
			project_id INTEGER NOT NULL, principal_id TEXT NOT NULL, command_kind TEXT NOT NULL,
			idempotency_key TEXT NOT NULL, request_hash TEXT NOT NULL, result_json TEXT NOT NULL,
			created_at TEXT NOT NULL, expires_at TEXT NOT NULL,
			PRIMARY KEY(project_id, principal_id, command_kind, idempotency_key)
		);
		CREATE TABLE fixer_thread (
			id TEXT PRIMARY KEY, project_id INTEGER NOT NULL, provider TEXT NOT NULL,
			external_session_id TEXT, headline TEXT NOT NULL, state TEXT NOT NULL,
			created_at TEXT NOT NULL, updated_at TEXT NOT NULL, UNIQUE(project_id, id)
		);
		CREATE TABLE fixer_turn (
			id TEXT PRIMARY KEY, project_id INTEGER NOT NULL, thread_id TEXT NOT NULL, ordinal INTEGER NOT NULL,
			role TEXT NOT NULL, content TEXT NOT NULL, status TEXT NOT NULL, client_message_id TEXT,
			provider_turn_id TEXT, created_at TEXT NOT NULL, completed_at TEXT,
			UNIQUE(thread_id, ordinal), UNIQUE(thread_id, client_message_id)
		);
		CREATE TABLE genui_surface_instance (
			id TEXT PRIMARY KEY, project_id INTEGER NOT NULL, thread_id TEXT NOT NULL,
			caused_by_turn_id TEXT NOT NULL, surface_type TEXT NOT NULL, surface_version INTEGER NOT NULL,
			state TEXT NOT NULL, current_revision INTEGER NOT NULL, created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL, UNIQUE(project_id, id)
		);
		CREATE TABLE genui_surface_revision (
			surface_id TEXT NOT NULL, revision INTEGER NOT NULL, source_seq INTEGER NOT NULL,
			document_json TEXT NOT NULL, document_hash TEXT NOT NULL, renderer_version TEXT NOT NULL,
			created_at TEXT NOT NULL, PRIMARY KEY(surface_id, revision)
		);
		CREATE TABLE genui_demand_example (
			id TEXT PRIMARY KEY, project_id INTEGER NOT NULL, thread_id TEXT NOT NULL,
			turn_id TEXT NOT NULL, requested_type TEXT, requested_version INTEGER,
			request_json TEXT NOT NULL, provider TEXT NOT NULL, model TEXT NOT NULL,
			rejection_code TEXT NOT NULL, privacy_class TEXT NOT NULL,
			triage_state TEXT NOT NULL, created_at TEXT NOT NULL
		);
		CREATE TABLE genui_surface_feedback (
			surface_id TEXT NOT NULL, revision INTEGER NOT NULL, principal_id TEXT NOT NULL,
			vote INTEGER NOT NULL, reason_code TEXT, comment TEXT, created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL, PRIMARY KEY(surface_id, revision, principal_id)
		);
		CREATE TABLE genui_action_invocation (
			id TEXT PRIMARY KEY, project_id INTEGER NOT NULL, surface_id TEXT NOT NULL,
			surface_revision INTEGER NOT NULL, action_id TEXT NOT NULL, action_version INTEGER NOT NULL,
			target_type TEXT NOT NULL, target_id TEXT NOT NULL, input_json TEXT NOT NULL,
			principal_id TEXT NOT NULL, decision TEXT NOT NULL, status TEXT NOT NULL,
			reason_code TEXT, idempotency_key TEXT NOT NULL, created_at TEXT NOT NULL, completed_at TEXT,
			UNIQUE(project_id, principal_id, action_id, idempotency_key)
		);
		CREATE TABLE project_hands (
			project_id INTEGER PRIMARY KEY, actor_id TEXT NOT NULL UNIQUE, display_name TEXT NOT NULL,
			authority_state TEXT NOT NULL, default_lane TEXT NOT NULL, next_instruction_ordinal INTEGER NOT NULL,
			created_at TEXT NOT NULL, updated_at TEXT NOT NULL
		);
		CREATE TABLE session (
			id INTEGER PRIMARY KEY AUTOINCREMENT, project_id INTEGER NOT NULL, task_description TEXT NOT NULL,
			status TEXT NOT NULL, report TEXT, cli_backend TEXT NOT NULL DEFAULT 'codex', cli_model TEXT NOT NULL DEFAULT '',
			cli_reasoning TEXT NOT NULL DEFAULT '', declared_write_scope TEXT NOT NULL DEFAULT '[]',
			session_kind TEXT NOT NULL DEFAULT 'netrunner', rework_count INTEGER NOT NULL DEFAULT 0,
			created_at TEXT, updated_at TEXT
		);
		CREATE TABLE hands_instruction (
			id TEXT PRIMARY KEY, project_id INTEGER NOT NULL, actor_id TEXT NOT NULL, ordinal INTEGER NOT NULL,
			source_channel_kind TEXT NOT NULL, source_channel_id TEXT NOT NULL, source_message_id TEXT NOT NULL DEFAULT '',
			issuer_principal_id TEXT NOT NULL, instruction_text TEXT NOT NULL, declared_write_scope_json TEXT NOT NULL,
			instruction_envelope_json TEXT NOT NULL, requested_lane TEXT NOT NULL, risk_class TEXT NOT NULL,
			review_policy TEXT NOT NULL, state TEXT NOT NULL, state_reason_code TEXT, state_reason_text TEXT,
			compat_session_id INTEGER, idempotency_key TEXT NOT NULL, revision INTEGER NOT NULL,
			created_at TEXT NOT NULL, updated_at TEXT NOT NULL, terminal_at TEXT,
			UNIQUE(project_id, ordinal), UNIQUE(project_id, source_channel_kind, source_channel_id, idempotency_key)
		);
		CREATE TABLE hands_instruction_event (
			instruction_id TEXT NOT NULL, ordinal INTEGER NOT NULL, event_type TEXT NOT NULL,
			from_state TEXT, to_state TEXT, actor_kind TEXT NOT NULL, actor_id TEXT NOT NULL,
			payload_json TEXT NOT NULL, created_at TEXT NOT NULL, PRIMARY KEY(instruction_id, ordinal)
		);
		CREATE TABLE hands_generation (
			instruction_id TEXT NOT NULL, generation INTEGER NOT NULL, project_id INTEGER NOT NULL,
			compat_session_id INTEGER, provider TEXT NOT NULL, model TEXT NOT NULL, reasoning TEXT NOT NULL,
			status TEXT NOT NULL, launch_mode TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
			ended_at TEXT, stop_reason TEXT, PRIMARY KEY(instruction_id, generation)
		);
		CREATE TABLE legacy_manual_session_link (
			project_id INTEGER NOT NULL, session_id INTEGER NOT NULL, disposition TEXT NOT NULL,
			evidence_json TEXT NOT NULL, hands_instruction_id TEXT, classified_at TEXT NOT NULL,
			classified_by TEXT NOT NULL, PRIMARY KEY(project_id, session_id)
		);
		CREATE TABLE project_mcp_server (project_id INTEGER NOT NULL, mcp_server_id INTEGER NOT NULL, PRIMARY KEY(project_id, mcp_server_id));
		CREATE TABLE project_hands_mcp_server (project_id INTEGER NOT NULL, mcp_server_id INTEGER NOT NULL, PRIMARY KEY(project_id, mcp_server_id));
		CREATE TABLE project_hands_doc (project_id INTEGER NOT NULL, project_doc_id INTEGER NOT NULL, PRIMARY KEY(project_id, project_doc_id));
		CREATE TABLE session_mcp_server (session_id INTEGER NOT NULL, mcp_server_id INTEGER NOT NULL, PRIMARY KEY(session_id, mcp_server_id));
		CREATE TABLE project_write_lease (
			id TEXT PRIMARY KEY, project_id INTEGER NOT NULL, owner_id TEXT NOT NULL,
			owner_kind TEXT NOT NULL, state TEXT NOT NULL, released_at TEXT, release_reason TEXT
		);
		CREATE TABLE workroom_audit_event (
			id TEXT PRIMARY KEY, project_id INTEGER NOT NULL, principal_id TEXT NOT NULL,
			action_id TEXT NOT NULL, target_type TEXT NOT NULL, target_id TEXT NOT NULL,
			decision TEXT NOT NULL, outcome TEXT NOT NULL, causation_id TEXT NOT NULL,
			correlation_id TEXT NOT NULL, detail_json TEXT NOT NULL, created_at TEXT NOT NULL
		);
		INSERT INTO project VALUES (1, 'Workroom Project', '/tmp/workroom-project');
		INSERT INTO project_hands VALUES (1, '11111111-1111-4111-8111-111111111111', 'Руки', 'enabled', 'codex', 1, '2026-07-30T09:00:00Z', '2026-07-30T09:00:00Z');
	`)
	if err != nil {
		_ = seed.Close()
		t.Fatalf("seed workroom schema: %v", err)
	}
	if err := seed.Close(); err != nil {
		t.Fatalf("close seed: %v", err)
	}
	repo, err := OpenRepository(dbPath, "")
	if err != nil {
		t.Fatalf("open workroom repository: %v", err)
	}
	now := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	repo.now = func() time.Time { return now }
	return repo, now
}

func signedBridgeRequest(t *testing.T, method, rawURL string, body []byte, projectID int, now time.Time) *http.Request {
	t.Helper()
	request, err := http.NewRequest(method, rawURL, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("create signed request: %v", err)
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse signed URL: %v", err)
	}
	serviceID := workroomBridgeDefaultService
	principalID := "principal:test"
	roles := []string{"fixer", "project_member"}
	requestID := fmt.Sprintf("request-%d", now.UnixNano())
	issuedAt := now.Unix()
	expiresAt := issuedAt + 60
	bodyDigest := sha256.Sum256(body)
	bodyHash := hex.EncodeToString(bodyDigest[:])
	canonical := bridgeCanonicalRequest(method, parsed.EscapedPath(), parsed.RawQuery, serviceID, principalID, roles, projectID, requestID, issuedAt, expiresAt, bodyHash)
	mac := hmac.New(sha256.New, []byte("test-workroom-secret"))
	_, _ = mac.Write([]byte(canonical))
	request.Header.Set(bridgeServiceHeader, serviceID)
	request.Header.Set(bridgePrincipalHeader, principalID)
	request.Header.Set(bridgeRolesHeader, strings.Join(roles, ","))
	request.Header.Set(bridgeProjectHeader, strconv.Itoa(projectID))
	request.Header.Set(bridgeRequestIDHeader, requestID)
	request.Header.Set(bridgeIssuedAtHeader, strconv.FormatInt(issuedAt, 10))
	request.Header.Set(bridgeExpiresAtHeader, strconv.FormatInt(expiresAt, 10))
	request.Header.Set(bridgeBodyHashHeader, bodyHash)
	request.Header.Set(bridgeSignatureHeader, hex.EncodeToString(mac.Sum(nil)))
	request.Header.Set("Content-Type", "application/json")
	return request
}

func doSignedBridgeJSON(t *testing.T, request *http.Request, target any) int {
	t.Helper()
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("call signed bridge: %v", err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read signed bridge response (%d): %v", response.StatusCode, err)
	}
	if response.StatusCode >= 400 {
		t.Logf("signed bridge failure (%d): %s", response.StatusCode, payload)
	}
	if target != nil {
		if err := json.Unmarshal(payload, target); err != nil {
			t.Fatalf("decode signed bridge response (%d): %v", response.StatusCode, err)
		}
	}
	return response.StatusCode
}

func TestInternalWorkroomBridgeRequiresSignatureAndReturnsConsistentSnapshot(t *testing.T) {
	repo, now := openWorkroomRepository(t)
	defer repo.Close()
	t.Setenv(workroomBridgeSecretEnv, "test-workroom-secret")
	server := httptest.NewServer(NewServer(repo))
	defer server.Close()

	response, err := http.Get(server.URL + "/internal/v1/projects/1/workroom/snapshot")
	if err != nil {
		t.Fatalf("call unsigned snapshot: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unsigned internal route returned %d", response.StatusCode)
	}

	request := signedBridgeRequest(t, http.MethodGet, server.URL+"/internal/v1/projects/1/workroom/snapshot", nil, 1, now)
	var snapshot ProjectWorkroomSnapshot
	if status := doSignedBridgeJSON(t, request, &snapshot); status != http.StatusOK {
		t.Fatalf("signed snapshot returned %d", status)
	}
	if snapshot.ProjectID != 1 || snapshot.ProjectCwd != "/tmp/workroom-project" || snapshot.ProtocolVersion != 1 || snapshot.HandsDisplayName != "Руки" || len(snapshot.HandsLanes) != 5 {
		t.Fatalf("unexpected consistent snapshot: %+v", snapshot)
	}
	if snapshot.WatermarkSeq != 0 || len(snapshot.HandsMailbox) != 0 {
		t.Fatalf("unexpected initial snapshot watermark/mailbox: %+v", snapshot)
	}
}

func TestInternalWorkroomBridgeEnrichesThreadModelFromHistory(t *testing.T) {
	repo, now := openWorkroomRepository(t)
	defer repo.Close()
	home := t.TempDir()
	t.Setenv("HOME", home)
	sessionsDir := filepath.Join(home, ".codex", "sessions", "2026", "07", "30")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	threadID := "019workroom-thread"
	transcriptPath := filepath.Join(sessionsDir, "rollout-2026-07-30T12-00-00-"+threadID+".jsonl")
	transcript := strings.Join([]string{
		`{"timestamp":"2026-07-30T12:00:00Z","type":"session_meta","payload":{"id":"` + threadID + `","timestamp":"2026-07-30T12:00:00Z","cwd":"/tmp/workroom-project"}}`,
		`{"timestamp":"2026-07-30T12:00:01Z","type":"turn_context","payload":{"model":"gpt-5.5","effort":"high"}}`,
		"{\"timestamp\":\"2026-07-30T12:00:02Z\",\"type\":\"user_message\",\"payload\":{\"text\":\"Activate skill $init-fixer immediately.\"}}",
		`{"timestamp":"2026-07-30T12:00:03Z","type":"assistant_message","payload":{"text":"Fixer note"}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(transcriptPath, []byte(transcript), 0o644); err != nil {
		t.Fatalf("write session log: %v", err)
	}
	if _, err := repo.dbWrite.Exec(`
		INSERT INTO fixer_thread (id, project_id, provider, headline, state, created_at, updated_at)
		VALUES (?, 1, 'codex', 'Thread', 'active', ?, ?)`, threadID, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("insert fixer thread: %v", err)
	}
	if _, err := repo.dbWrite.Exec(`
		INSERT INTO fixer_turn (id, project_id, thread_id, ordinal, role, content, status, created_at, completed_at)
		VALUES ('turn-1', 1, ?, 1, 'user', 'Inspect', 'complete', ?, ?)`, threadID, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("insert fixer turn: %v", err)
	}

	t.Setenv(workroomBridgeSecretEnv, "test-workroom-secret")
	server := httptest.NewServer(NewServer(repo))
	defer server.Close()

	request := signedBridgeRequest(
		t,
		http.MethodGet,
		server.URL+"/internal/v1/projects/1/workroom/snapshot",
		nil,
		1,
		now,
	)
	var snapshot ProjectWorkroomSnapshot
	if status := doSignedBridgeJSON(t, request, &snapshot); status != http.StatusOK {
		t.Fatalf("signed snapshot returned %d", status)
	}
	if len(snapshot.Threads) != 1 {
		t.Fatalf("unexpected thread count: %+v", snapshot.Threads)
	}
	thread := snapshot.Threads[0]
	if thread.ID != threadID || thread.Model != "gpt-5.5" || thread.Reasoning != "high" {
		t.Fatalf("thread was not enriched from history: %+v", thread)
	}
}

func TestInternalWorkroomBridgeRejectsTamperingExpiryAndProjectSubstitution(t *testing.T) {
	repo, now := openWorkroomRepository(t)
	defer repo.Close()
	t.Setenv(workroomBridgeSecretEnv, "test-workroom-secret")
	server := httptest.NewServer(NewServer(repo))
	defer server.Close()

	assertUnauthorized := func(name string, request *http.Request) {
		t.Helper()
		t.Run(name, func(t *testing.T) {
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				t.Fatalf("call tampered bridge: %v", err)
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusUnauthorized {
				t.Fatalf("tampered bridge returned %d", response.StatusCode)
			}
		})
	}

	request := signedBridgeRequest(t, http.MethodGet, server.URL+"/internal/v1/projects/1/workroom/snapshot", nil, 1, now)
	request.Header.Set(bridgeSignatureHeader, strings.Repeat("0", sha256.Size*2))
	assertUnauthorized("signature", request)

	request = signedBridgeRequest(t, http.MethodGet, server.URL+"/internal/v1/projects/1/workroom/snapshot", nil, 1, now)
	request.Body = io.NopCloser(strings.NewReader(`{"substituted":true}`))
	request.ContentLength = int64(len(`{"substituted":true}`))
	assertUnauthorized("body", request)

	request = signedBridgeRequest(t, http.MethodGet, server.URL+"/internal/v1/projects/1/workroom/snapshot", nil, 1, now.Add(-2*time.Minute))
	assertUnauthorized("expiry", request)

	request = signedBridgeRequest(t, http.MethodGet, server.URL+"/internal/v1/projects/1/workroom/snapshot", nil, 2, now)
	assertUnauthorized("project-binding", request)

	request = signedBridgeRequest(t, http.MethodGet, server.URL+"/internal/v1/projects/1/workroom/snapshot", nil, 1, now)
	request.Header.Set(bridgeRolesHeader, "viewer")
	assertUnauthorized("roles", request)
}

func TestInternalWorkroomBridgeMutationReplayAndLongTail(t *testing.T) {
	repo, now := openWorkroomRepository(t)
	defer repo.Close()
	t.Setenv(workroomBridgeSecretEnv, "test-workroom-secret")
	server := httptest.NewServer(NewServer(repo))
	defer server.Close()

	body := []byte(`{"content":"Show the current governed state.","idempotency_key":"turn-1"}`)
	request := signedBridgeRequest(t, http.MethodPost, server.URL+"/internal/v1/projects/1/fixer/turns", body, 1, now)
	var first FixerTurnReceipt
	if status := doSignedBridgeJSON(t, request, &first); status != http.StatusAccepted {
		t.Fatalf("send turn returned %d", status)
	}
	if first.ThreadID == "" || first.TurnID == "" || first.ProjectSeq < 2 {
		t.Fatalf("unexpected first turn receipt: %+v", first)
	}
	request = signedBridgeRequest(t, http.MethodPost, server.URL+"/internal/v1/projects/1/fixer/turns", body, 1, now)
	var replay FixerTurnReceipt
	if status := doSignedBridgeJSON(t, request, &replay); status != http.StatusAccepted {
		t.Fatalf("replay turn returned %d", status)
	}
	if replay.TurnID != first.TurnID || replay.ProjectSeq != first.ProjectSeq {
		t.Fatalf("turn replay diverged: first=%+v replay=%+v", first, replay)
	}

	eventsURL := server.URL + "/internal/v1/projects/1/events?after_seq=0&limit=100&wait_ms=0"
	request = signedBridgeRequest(t, http.MethodGet, eventsURL, nil, 1, now)
	var events ProjectUIEventsResponse
	if status := doSignedBridgeJSON(t, request, &events); status != http.StatusOK {
		t.Fatalf("event replay returned %d", status)
	}
	if len(events.Events) != 2 || events.Events[0].Seq != 1 || events.Events[1].Seq != 2 || events.HeadSeq != 2 {
		t.Fatalf("unexpected ordered replay: %+v", events)
	}

	waitResult := make(chan ProjectUIEventsResponse, 1)
	waitErr := make(chan error, 1)
	go func() {
		result, err := repo.WaitProjectUIEvents(context.Background(), 1, events.HeadSeq, 100, 2*time.Second)
		if err != nil {
			waitErr <- err
			return
		}
		waitResult <- result
	}()
	time.Sleep(20 * time.Millisecond)
	principal := BridgePrincipal{PrincipalID: "principal:test", Roles: []string{"fixer"}, ProjectID: 1, RequestID: "direct-2"}
	if _, err := repo.SendFixerTurn(context.Background(), 1, principal, SendFixerTurnInput{ThreadID: first.ThreadID, Content: "Continue.", IdempotencyKey: "turn-2"}); err != nil {
		t.Fatalf("trigger long tail: %v", err)
	}
	select {
	case err := <-waitErr:
		t.Fatalf("long tail failed: %v", err)
	case result := <-waitResult:
		if result.TimedOut || len(result.Events) != 1 || result.Events[0].Seq != 3 {
			t.Fatalf("unexpected long-tail result: %+v", result)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("long-tail waiter did not wake after committed event")
	}
}

func TestWorkroomLongTailDiscoversCrossProcessJournalCommits(t *testing.T) {
	repo, now := openWorkroomRepository(t)
	defer repo.Close()
	result := make(chan ProjectUIEventsResponse, 1)
	failure := make(chan error, 1)
	startedAt := time.Now()
	go func() {
		output, err := repo.WaitProjectUIEvents(context.Background(), 1, 0, 100, 2*time.Second)
		if err != nil {
			failure <- err
			return
		}
		result <- output
	}()
	time.Sleep(20 * time.Millisecond)
	tx, err := repo.dbWrite.Begin()
	if err != nil {
		t.Fatalf("begin external writer transaction: %v", err)
	}
	if _, err := tx.Exec(`INSERT INTO project_ui_cursor (project_id, next_seq, updated_at) VALUES (1, 2, ?)`, now.Format(time.RFC3339Nano)); err != nil {
		_ = tx.Rollback()
		t.Fatalf("seed external cursor: %v", err)
	}
	if _, err := tx.Exec(`
		INSERT INTO project_ui_event (
			project_id, seq, event_id, schema_version, kind, aggregate_type, aggregate_id,
			aggregate_revision, payload_json, actor_kind, actor_id, causation_id, correlation_id, created_at
		) VALUES (1, 1, 'external-event-1', 1, 'hands.instruction.changed', 'hands_instruction',
		          'instruction-1', 1, '{}', 'hands', 'hands:1', 'cause-1', 'correlation-1', ?)`, now.Format(time.RFC3339Nano)); err != nil {
		_ = tx.Rollback()
		t.Fatalf("seed external event: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit external journal write: %v", err)
	}

	select {
	case err := <-failure:
		t.Fatalf("cross-process tail failed: %v", err)
	case output := <-result:
		if len(output.Events) != 1 || output.Events[0].Seq != 1 || output.TimedOut {
			t.Fatalf("unexpected cross-process tail: %+v", output)
		}
		if elapsed := time.Since(startedAt); elapsed >= time.Second {
			t.Fatalf("cross-process event visibility took %s", elapsed)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cross-process journal tail did not return")
	}
}

func TestInternalWorkroomHandsAndFeedbackMutationsAreDurable(t *testing.T) {
	repo, now := openWorkroomRepository(t)
	defer repo.Close()
	t.Setenv(workroomBridgeSecretEnv, "test-workroom-secret")
	server := httptest.NewServer(NewServer(repo))
	defer server.Close()

	handsBody := []byte(`{"instruction_text":"Perform a read-only replay audit.","requested_lane":"codex","idempotency_key":"hands-1"}`)
	request := signedBridgeRequest(t, http.MethodPost, server.URL+"/internal/v1/projects/1/hands/instructions", handsBody, 1, now)
	var hands HandsInstructionReceipt
	if status := doSignedBridgeJSON(t, request, &hands); status != http.StatusAccepted {
		t.Fatalf("submit Hands returned %d", status)
	}
	if hands.State != "queued" || hands.RiskClass != "read_only" || hands.InstructionID == "" {
		t.Fatalf("unexpected Hands receipt: %+v", hands)
	}

	// Materialize a server-owned feedback action fixture.
	document := fmt.Sprintf(`{"protocol":"fixer.genui","protocol_version":1,"instance_id":"surface-1","surface_type":"project.overview","surface_version":1,"project_id":1,"revision":1,"source_seq":%d,"title":"Project","components":[],"actions":[{"ref":"feedback","action_id":"genui.feedback.submit","action_version":1,"enabled":true}]}`, hands.ProjectSeq)
	if !surfaceDocumentAllowsAction(document, "genui.feedback.submit", 1) {
		t.Fatal("feedback fixture does not advertise its action")
	}
	timestamp := now.Format(time.RFC3339Nano)
	seedStatements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO fixer_thread VALUES ('thread-1', 1, 'fixer', NULL, 'Thread', 'active', ?, ?)`, []any{timestamp, timestamp}},
		{`INSERT INTO fixer_turn VALUES ('turn-1', 1, 'thread-1', 1, 'fixer', 'State', 'complete', NULL, NULL, ?, ?)`, []any{timestamp, timestamp}},
		{`INSERT INTO genui_surface_instance VALUES ('surface-1', 1, 'thread-1', 'turn-1', 'project.overview', 1, 'presented', 1, ?, ?)`, []any{timestamp, timestamp}},
		{`INSERT INTO genui_surface_revision VALUES ('surface-1', 1, ?, ?, 'hash', 'test', ?)`, []any{hands.ProjectSeq, document, timestamp}},
	}
	for _, statement := range seedStatements {
		if _, err := repo.dbWrite.Exec(statement.query, statement.args...); err != nil {
			t.Fatalf("seed feedback surface: %v", err)
		}
	}
	var storedDocument string
	if err := repo.db.QueryRow(`SELECT document_json FROM genui_surface_revision WHERE surface_id = 'surface-1' AND revision = 1`).Scan(&storedDocument); err != nil {
		t.Fatalf("read feedback surface: %v", err)
	}
	if storedDocument != document {
		t.Fatalf("unexpected stored feedback document: %q", storedDocument)
	}
	actionBody := []byte(`{"protocol_version":1,"surface_id":"surface-1","surface_revision":1,"action_id":"genui.feedback.submit","action_version":1,"target_type":"genui_surface","target_id":"surface-1","input":{"vote":1,"reason_code":"helpful","comment":"Useful."},"confirmed":true,"idempotency_key":"feedback-1"}`)
	request = signedBridgeRequest(t, http.MethodPost, server.URL+"/internal/v1/projects/1/genui/actions", actionBody, 1, now)
	var action GenuiActionReceipt
	if status := doSignedBridgeJSON(t, request, &action); status != http.StatusOK {
		t.Fatalf("feedback action returned %d", status)
	}
	if action.Status != "succeeded" || action.ProjectSeq <= hands.ProjectSeq {
		t.Fatalf("unexpected action receipt: %+v", action)
	}
	var vote int
	if err := repo.db.QueryRow(`SELECT vote FROM genui_surface_feedback WHERE surface_id = 'surface-1' AND principal_id = 'principal:test'`).Scan(&vote); err != nil {
		t.Fatalf("read feedback: %v", err)
	}
	if vote != 1 {
		t.Fatalf("unexpected persisted feedback vote %d", vote)
	}

	cancelBody := []byte(`{"reason":"No longer needed.","idempotency_key":"cancel-1"}`)
	request = signedBridgeRequest(t, http.MethodPost, server.URL+"/internal/v1/projects/1/hands/instructions/"+hands.InstructionID+"/cancel", cancelBody, 1, now)
	var cancel CommandReceipt
	if status := doSignedBridgeJSON(t, request, &cancel); status != http.StatusOK {
		t.Fatalf("cancel Hands returned %d", status)
	}
	if cancel.State != "cancelled" || cancel.ProjectSeq <= action.ProjectSeq {
		t.Fatalf("unexpected cancellation receipt: %+v", cancel)
	}
}

func TestDecodeClosedJSONRejectsTrailingValues(t *testing.T) {
	var input feedbackActionInput
	if err := decodeClosedJSON(json.RawMessage(`{"vote":1} {"vote":-1}`), &input); err == nil {
		t.Fatal("expected a second JSON value to fail closed")
	}
}

func TestWorkroomCapabilityPolicyEnforcesResourceSensitiveHandsCancellation(t *testing.T) {
	repo, _ := openWorkroomRepository(t)
	defer repo.Close()
	ctx := context.Background()
	member := BridgePrincipal{PrincipalID: "member:one", Roles: []string{"project_member"}, ProjectID: 1, RequestID: "policy-1"}
	viewer := BridgePrincipal{PrincipalID: "viewer:one", Roles: []string{"viewer"}, ProjectID: 1, RequestID: "policy-viewer"}
	fixer := BridgePrincipal{PrincipalID: "fixer:one", Roles: []string{"fixer"}, ProjectID: 1, RequestID: "policy-fixer"}

	if !bridgePrincipalHasCapability(member, "hands.instruction.cancel") || bridgePrincipalHasCapability(member, "hands.review") {
		t.Fatal("project-member capabilities must expose issuer cancellation but not review")
	}
	if !bridgePrincipalHasCapability(fixer, "hands.review") {
		t.Fatal("fixer must retain governed review capability")
	}
	if _, err := repo.SendFixerTurn(ctx, 1, viewer, SendFixerTurnInput{Content: "denied", IdempotencyKey: "viewer-denied"}); !errors.Is(err, ErrWorkroomAuthorization) {
		t.Fatalf("viewer turn should be denied, got %v", err)
	}

	first, err := repo.SubmitHandsInstruction(ctx, 1, member, BridgeSubmitHandsInstructionInput{
		InstructionText: "Queue then cancel my instruction.", RequestedLane: "codex", IdempotencyKey: "member-cancel-1",
	})
	if err != nil {
		t.Fatalf("submit member instruction: %v", err)
	}
	if _, err := repo.CancelHandsInstruction(ctx, 1, first.InstructionID, member, BridgeCancelHandsInstructionInput{IdempotencyKey: "member-cancel-command-1"}); err != nil {
		t.Fatalf("issuer should cancel its queued instruction: %v", err)
	}

	second, err := repo.SubmitHandsInstruction(ctx, 1, member, BridgeSubmitHandsInstructionInput{
		InstructionText: "Represent a running instruction.", RequestedLane: "codex", IdempotencyKey: "member-cancel-2",
	})
	if err != nil {
		t.Fatalf("submit second member instruction: %v", err)
	}
	if _, err := repo.dbWrite.Exec(`UPDATE hands_instruction SET state = 'running' WHERE id = ?`, second.InstructionID); err != nil {
		t.Fatalf("seed running instruction: %v", err)
	}
	if _, err := repo.CancelHandsInstruction(ctx, 1, second.InstructionID, member, BridgeCancelHandsInstructionInput{IdempotencyKey: "member-running-denied"}); !errors.Is(err, ErrWorkroomAuthorization) {
		t.Fatalf("member must not stop a running generation, got %v", err)
	}
	if _, err := repo.CancelHandsInstruction(ctx, 1, second.InstructionID, fixer, BridgeCancelHandsInstructionInput{IdempotencyKey: "fixer-running-cancel"}); err != nil {
		t.Fatalf("fixer should stop a running generation: %v", err)
	}
}

func TestHandsCodexLaneRegistersRunningDeepseekModels(t *testing.T) {
	repo, _ := openWorkroomRepository(t)
	defer repo.Close()
	ctx := context.Background()
	member := BridgePrincipal{PrincipalID: "member:one", Roles: []string{"project_member"}, ProjectID: 1, RequestID: "deepseek-1"}

	spec, ok := dashboardHandsProviderSpecFor("codex")
	if !ok {
		t.Fatal("codex Hands lane spec is missing")
	}
	for _, model := range []string{"deepseek-v4-flash", "deepseek/deepseek-v4-pro-0813", "deepseek/deepseek-v4-flash-0731"} {
		if !dashboardHandsProviderOptionContains(spec.modelOptions, model) {
			t.Fatalf("codex lane must register actual running model %q", model)
		}
	}

	// A real thread launches on deepseek-v4-flash; submitting a subsequent
	// instruction on the same running model must not be rejected.
	receipt, err := repo.SubmitHandsInstruction(ctx, 1, member, BridgeSubmitHandsInstructionInput{
		InstructionText:    "Continue on the running deepseek model.",
		RequestedLane:      "codex",
		RequestedModel:     "deepseek-v4-flash",
		RequestedReasoning: "high",
		IdempotencyKey:     "deepseek-submit-1",
	})
	if err != nil {
		t.Fatalf("submit instruction on deepseek-v4-flash: %v", err)
	}
	if receipt.State != "queued" {
		t.Fatalf("unexpected instruction state: %+v", receipt)
	}
	if receipt.Lane != "codex" {
		t.Fatalf("receipt did not echo the codex lane: %+v", receipt)
	}
	if receipt.Ordinal <= 0 {
		t.Fatalf("unexpected receipt ordinal: %+v", receipt)
	}
}

func TestLegalResearchFixerTurnMaterializesReplayableGovernedSurface(t *testing.T) {
	repo, now := openWorkroomRepository(t)
	defer repo.Close()
	projectRoot := t.TempDir()
	legalDirectory := filepath.Join(projectRoot, "research", "legal")
	if err := os.MkdirAll(legalDirectory, 0o755); err != nil {
		t.Fatalf("create legal research directory: %v", err)
	}
	const legalMemo = "# Юридический меморандум\n\nПроверенный канон по Яндекс Юрке."
	if err := os.WriteFile(filepath.Join(legalDirectory, "legal_memo.md"), []byte(legalMemo), 0o600); err != nil {
		t.Fatalf("write legal research fixture: %v", err)
	}
	if _, err := repo.dbWrite.Exec(`UPDATE project SET cwd = ? WHERE id = 1`, projectRoot); err != nil {
		t.Fatalf("bind project research root: %v", err)
	}
	t.Setenv(workroomBridgeSecretEnv, "test-workroom-secret")
	server := httptest.NewServer(NewServer(repo))
	defer server.Close()

	body := []byte(`{"content":"Покажи ресерч по юрке","idempotency_key":"legal-turn-1"}`)
	request := signedBridgeRequest(t, http.MethodPost, server.URL+"/internal/v1/projects/1/fixer/turns", body, 1, now)
	var receipt FixerTurnReceipt
	if status := doSignedBridgeJSON(t, request, &receipt); status != http.StatusAccepted {
		t.Fatalf("send legal research turn returned %d", status)
	}
	if receipt.ProjectSeq != 4 {
		t.Fatalf("legal route must commit thread, user, fixer, and surface events; got %+v", receipt)
	}

	eventsURL := server.URL + "/internal/v1/projects/1/events?after_seq=0&limit=100&wait_ms=0"
	request = signedBridgeRequest(t, http.MethodGet, eventsURL, nil, 1, now)
	var replay ProjectUIEventsResponse
	if status := doSignedBridgeJSON(t, request, &replay); status != http.StatusOK {
		t.Fatalf("replay legal research events returned %d", status)
	}
	if len(replay.Events) != 4 || replay.HeadSeq != receipt.ProjectSeq {
		t.Fatalf("unexpected legal research replay: %+v", replay)
	}
	for index, event := range replay.Events {
		if event.Seq != int64(index+1) {
			t.Fatalf("replay contains a sequence gap at index %d: %+v", index, replay.Events)
		}
	}
	if replay.Events[2].Kind != "fixer.turn.appended" || replay.Events[3].Kind != "genui.surface.presented" {
		t.Fatalf("governed route did not append the fixer response and surface: %+v", replay.Events)
	}

	request = signedBridgeRequest(t, http.MethodGet, server.URL+"/internal/v1/projects/1/workroom/snapshot", nil, 1, now)
	var snapshot ProjectWorkroomSnapshot
	if status := doSignedBridgeJSON(t, request, &snapshot); status != http.StatusOK {
		t.Fatalf("legal research snapshot returned %d", status)
	}
	if len(snapshot.Turns) != 2 || snapshot.Turns[0].Role != "user" || snapshot.Turns[1].Role != "fixer" {
		t.Fatalf("snapshot does not contain the governed conversation: %+v", snapshot.Turns)
	}
	if snapshot.ActiveSurface == nil || snapshot.ActiveSurface.SurfaceType != legalResearchSurfaceType {
		t.Fatalf("snapshot does not activate legal research: %+v", snapshot.ActiveSurface)
	}
	var document map[string]any
	if err := json.Unmarshal(snapshot.ActiveSurface.Document, &document); err != nil {
		t.Fatalf("decode legal surface document: %v", err)
	}
	encodedDocument, _ := json.Marshal(document)
	if !strings.Contains(string(encodedDocument), "Проверенный канон по Яндекс Юрке") ||
		!strings.Contains(string(encodedDocument), legalResearchPath) {
		t.Fatalf("surface is not backed by the project legal memo: %s", encodedDocument)
	}

	feedbackBody := []byte(fmt.Sprintf(`{"protocol_version":1,"surface_id":%q,"surface_revision":1,"action_id":"genui.feedback.submit","action_version":1,"target_type":"genui_surface","target_id":%q,"input":{"vote":1,"reason_code":"accurate","comment":"Канон найден."},"confirmed":true,"idempotency_key":"legal-feedback-1"}`, snapshot.ActiveSurface.ID, snapshot.ActiveSurface.ID))
	request = signedBridgeRequest(t, http.MethodPost, server.URL+"/internal/v1/projects/1/genui/actions", feedbackBody, 1, now)
	var feedback GenuiActionReceipt
	if status := doSignedBridgeJSON(t, request, &feedback); status != http.StatusOK {
		t.Fatalf("legal surface feedback returned %d", status)
	}
	var vote int
	if err := repo.db.QueryRow(`SELECT vote FROM genui_surface_feedback WHERE surface_id = ? AND principal_id = 'principal:test'`, snapshot.ActiveSurface.ID).Scan(&vote); err != nil || vote != 1 {
		t.Fatalf("feedback was not durable: vote=%d err=%v", vote, err)
	}

	reconnectURL := fmt.Sprintf("%s/internal/v1/projects/1/events?after_seq=%d&limit=100&wait_ms=0", server.URL, receipt.ProjectSeq)
	request = signedBridgeRequest(t, http.MethodGet, reconnectURL, nil, 1, now)
	var reconnect ProjectUIEventsResponse
	if status := doSignedBridgeJSON(t, request, &reconnect); status != http.StatusOK {
		t.Fatalf("feedback reconnect returned %d", status)
	}
	if len(reconnect.Events) != 2 || reconnect.Events[0].Kind != "genui.feedback.recorded" || reconnect.Events[1].Kind != "genui.action.changed" {
		t.Fatalf("reconnect did not replay durable feedback events: %+v", reconnect)
	}
}

func TestGenuiSurfaceRequestFailsClosedForUnregisteredType(t *testing.T) {
	repo, now := openWorkroomRepository(t)
	defer repo.Close()
	t.Setenv(workroomBridgeSecretEnv, "test-workroom-secret")
	server := httptest.NewServer(NewServer(repo))
	defer server.Close()

	body := []byte(`{"surface_type":"html.script","surface_version":1,"arguments":{},"idempotency_key":"unsupported-surface-1"}`)
	request := signedBridgeRequest(t, http.MethodPost, server.URL+"/internal/v1/projects/1/genui/surfaces", body, 1, now)
	var receipt GenuiActionReceipt
	if status := doSignedBridgeJSON(t, request, &receipt); status != http.StatusOK {
		t.Fatalf("unregistered surface returned %d", status)
	}
	if receipt.Status != "rejected" || receipt.Decision != "denied" || receipt.ReasonCode != "surface_type_unsupported" || receipt.InvocationID == "" {
		t.Fatalf("unregistered surface did not return governed rejection: %+v", receipt)
	}
	var demandCount, surfaceCount int
	if err := repo.db.QueryRow(`SELECT COUNT(*) FROM genui_demand_example`).Scan(&demandCount); err != nil {
		t.Fatalf("count demand examples: %v", err)
	}
	if err := repo.db.QueryRow(`SELECT COUNT(*) FROM genui_surface_instance`).Scan(&surfaceCount); err != nil {
		t.Fatalf("count surfaces: %v", err)
	}
	if demandCount != 1 || surfaceCount != 0 {
		t.Fatalf("fail-closed persistence mismatch: demand=%d surfaces=%d", demandCount, surfaceCount)
	}
}
