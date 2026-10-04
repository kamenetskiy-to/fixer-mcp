package dashboardapi

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAcceptOrderCreatesProjectAndSession(t *testing.T) {
	repo := openFixtureRepository(t)
	defer repo.Close()

	server := httptest.NewServer(NewServer(repo))
	defer server.Close()

	projectCWD := filepath.Join(t.TempDir(), "accepted-order")
	var response AcceptOrderResponse
	postJSON(t, server.URL+"/api/actions/orders/accept", map[string]any{
		"order_id":    42,
		"title":       "Build the client cockpit",
		"description": "Implement the requested workflow.",
		"revisions":   "Use the client's existing branding.",
		"project_cwd": projectCWD,
	}, &response)

	if response.Status != "success" || response.OrderID != 42 {
		t.Fatalf("unexpected acceptance response: %+v", response)
	}
	if response.ProjectReused {
		t.Fatalf("expected a new project, got %+v", response)
	}
	if response.ProjectID == 0 || response.Project.ID != response.ProjectID {
		t.Fatalf("expected project id in response, got %+v", response)
	}
	if response.SessionID == 0 {
		t.Fatalf("expected session id in response, got %+v", response)
	}
	if !strings.Contains(response.TaskDescription, "Revisions:\nUse the client's existing branding.") {
		t.Fatalf("expected revisions in task description, got %q", response.TaskDescription)
	}

	var projectCount, sessionCount int
	if err := repo.db.QueryRow("SELECT COUNT(*) FROM project WHERE cwd = ?", response.Project.CWD).Scan(&projectCount); err != nil {
		t.Fatalf("count order projects: %v", err)
	}
	if projectCount != 1 {
		t.Fatalf("expected one order project, got %d", projectCount)
	}
	if err := repo.db.QueryRow("SELECT COUNT(*) FROM session WHERE id = ? AND project_id = ? AND status = 'pending'", response.SessionID, response.ProjectID).Scan(&sessionCount); err != nil {
		t.Fatalf("read order session: %v", err)
	}
	if sessionCount != 1 {
		t.Fatalf("expected one pending order session, got %d", sessionCount)
	}

}

func TestAcceptOrderReusesProjectByNormalizedCWD(t *testing.T) {
	repo := openFixtureRepository(t)
	defer repo.Close()

	server := httptest.NewServer(NewServer(repo))
	defer server.Close()

	projectCWD := filepath.Join(t.TempDir(), "accepted-order")
	var first AcceptOrderResponse
	postJSON(t, server.URL+"/api/actions/orders/accept", map[string]any{
		"order_id":    100,
		"title":       "First order",
		"project_cwd": projectCWD,
	}, &first)

	var second AcceptOrderResponse
	postJSON(t, server.URL+"/api/orders/101/accept", map[string]any{
		"orderId":    101,
		"title":      "Second order revision",
		"projectCwd": projectCWD + "/",
	}, &second)

	if !second.ProjectReused {
		t.Fatalf("expected project reuse, got %+v", second)
	}
	if second.ProjectID != first.ProjectID || second.Project.CWD != first.Project.CWD {
		t.Fatalf("expected the same normalized project, first=%+v second=%+v", first.Project, second.Project)
	}
	if second.SessionID == first.SessionID {
		t.Fatalf("expected each accepted order to get a session, first=%d second=%d", first.SessionID, second.SessionID)
	}

	var count int
	if err := repo.db.QueryRow("SELECT COUNT(*) FROM project WHERE cwd = ?", first.Project.CWD).Scan(&count); err != nil {
		t.Fatalf("count reused project: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected one reused project row, got %d", count)
	}
}

func TestAcceptOrderRejectsInvalidPayload(t *testing.T) {
	repo := openFixtureRepository(t)
	defer repo.Close()

	server := httptest.NewServer(NewServer(repo))
	defer server.Close()

	response, err := http.Post(server.URL+"/api/actions/orders/accept", "application/json", strings.NewReader(`{"title":"Missing order id"}`))
	if err != nil {
		t.Fatalf("post invalid order: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing order id, got %d", response.StatusCode)
	}

	response, err = http.Post(server.URL+"/api/actions/orders/accept", "application/json", strings.NewReader(`{"order_id":7}`))
	if err != nil {
		t.Fatalf("post missing task: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing task, got %d", response.StatusCode)
	}

	var projects int
	if err := repo.db.QueryRow("SELECT COUNT(*) FROM project").Scan(&projects); err != nil && err != sql.ErrNoRows {
		t.Fatalf("count projects after rejected orders: %v", err)
	}
	if projects != 2 {
		t.Fatalf("rejected orders must not create projects, got %d", projects)
	}
}

func TestAcceptOrderRejectsRouteMismatchAndSupportsCamelCase(t *testing.T) {
	repo := openFixtureRepository(t)
	defer repo.Close()

	server := httptest.NewServer(NewServer(repo))
	defer server.Close()

	response, err := http.Post(server.URL+"/api/orders/9/accept", "application/json", strings.NewReader(`{"orderId":8,"taskDescription":"Mismatch"}`))
	if err != nil {
		t.Fatalf("post mismatched order: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for route mismatch, got %d", response.StatusCode)
	}

	var accepted AcceptOrderResponse
	postJSON(t, server.URL+"/api/orders/9/accept", map[string]any{
		"taskDescription": "Camel case order",
		"projectCwd":      filepath.Join(t.TempDir(), "camel-order"),
	}, &accepted)
	if accepted.OrderID != 9 || accepted.TaskDescription != "Camel case order" {
		t.Fatalf("unexpected camel case acceptance: %+v", accepted)
	}
}

func TestAcceptOrderSupportsClientOrderJSONID(t *testing.T) {
	repo := openFixtureRepository(t)
	defer repo.Close()

	server := httptest.NewServer(NewServer(repo))
	defer server.Close()

	var accepted AcceptOrderResponse
	postJSON(t, server.URL+"/api/actions/orders/accept", map[string]any{
		"id":          314,
		"clientId":    "client-314",
		"title":       "Client order payload",
		"description": "Use the Serverpod Order JSON shape.",
		"status":      "draft",
		"projectCwd":  filepath.Join(t.TempDir(), "client-order"),
	}, &accepted)

	if accepted.Status != "success" || accepted.OrderID != 314 || accepted.SessionID == 0 {
		t.Fatalf("unexpected client order acceptance: %+v", accepted)
	}
	if accepted.TaskDescription != "Client order payload\n\nUse the Serverpod Order JSON shape." {
		t.Fatalf("unexpected mapped task description: %q", accepted.TaskDescription)
	}
}

func TestAcceptOrderCreatesGitSandboxAndPreview(t *testing.T) {
	sourceRoot := filepath.Join(t.TempDir(), "source")
	initGitOrderSource(t, sourceRoot)
	repo := openFixtureRepository(t)
	defer repo.Close()

	server := httptest.NewServer(NewServer(repo))
	defer server.Close()

	worktreePath := filepath.Join(t.TempDir(), "order-worktree")
	var response AcceptOrderResponse
	postJSON(t, server.URL+"/api/actions/orders/accept", map[string]any{
		"order_id":            77,
		"task_description":    "Implement the client preview",
		"source_project_root": sourceRoot,
		"project_cwd":         worktreePath,
		"branch_name":         "orders/client-preview",
		"preview_provider":    "appetize_stub",
		"preview_ttl_seconds": 3600,
	}, &response)

	if response.Sandbox.Status != "ready" {
		t.Fatalf("expected ready sandbox, got %+v", response.Sandbox)
	}
	normalizedSourceRoot, _ := normalizeProjectCWD(sourceRoot)
	normalizedWorktreePath, _ := normalizeOrderProjectCWD(worktreePath)
	if response.Sandbox.SourceProjectCWD != normalizedSourceRoot || response.Sandbox.WorktreePath != normalizedWorktreePath {
		t.Fatalf("unexpected sandbox paths: got source=%q worktree=%q want source=%q worktree=%q", response.Sandbox.SourceProjectCWD, response.Sandbox.WorktreePath, normalizedSourceRoot, normalizedWorktreePath)
	}
	if response.Sandbox.ID == 0 || response.Sandbox.BranchName != "orders/client-preview" || response.PreviewURL == "" {
		t.Fatalf("expected branch and preview URL, got %+v", response)
	}
	if _, err := os.Stat(filepath.Join(worktreePath, "README.md")); err != nil {
		t.Fatalf("expected checked out worktree: %v", err)
	}
	if output := runOrderGit(t, sourceRoot, "branch", "--list", "orders/client-preview"); !strings.Contains(output, "orders/client-preview") {
		t.Fatalf("expected order branch, got %q", output)
	}

	var preview PreviewResponse
	readJSON(t, server.URL+response.PreviewURL, &preview)
	if preview.Status != "ready" || preview.OrderID != 77 || preview.Sandbox.Preview.Provider != "appetize_stub" {
		t.Fatalf("unexpected preview response: %+v", preview)
	}

	var retry AcceptOrderResponse
	postJSON(t, server.URL+"/api/actions/orders/accept", map[string]any{
		"order_id":            77,
		"task_description":    "Retry the client preview",
		"source_project_root": sourceRoot,
		"project_cwd":         worktreePath,
		"branch_name":         "orders/client-preview",
	}, &retry)
	if !retry.ProjectReused || retry.Sandbox.Status != "ready" {
		t.Fatalf("expected retry to reuse sandbox project, got %+v", retry)
	}
}

func TestOrderSandboxLifecycleEndpoints(t *testing.T) {
	sourceRoot := filepath.Join(t.TempDir(), "source")
	initGitOrderSource(t, sourceRoot)
	repo := openFixtureRepository(t)
	defer repo.Close()

	server := httptest.NewServer(NewServer(repo))
	defer server.Close()

	worktreePath := filepath.Join(t.TempDir(), "order-worktree")
	var created SandboxActionResponse
	status := requestJSON(t, http.MethodPost, server.URL+"/api/orders/808/sandbox", map[string]any{
		"source_project_root": sourceRoot,
		"project_cwd":         worktreePath,
		"branch_name":         "orders/lifecycle",
		"preview_provider":    "lifecycle_stub",
	}, &created)
	if status != http.StatusOK || created.Status != "success" || created.Sandbox.Status != "ready" {
		t.Fatalf("unexpected sandbox create response: status=%d response=%+v", status, created)
	}
	if !created.Sandbox.WorktreeExists || !created.Sandbox.BranchExists {
		t.Fatalf("expected created worktree and branch flags, got %+v", created.Sandbox)
	}
	if _, err := os.Stat(filepath.Join(worktreePath, "README.md")); err != nil {
		t.Fatalf("expected sandbox worktree contents: %v", err)
	}

	var inspected SandboxActionResponse
	status = requestJSON(t, http.MethodGet, server.URL+"/api/orders/808/sandbox", nil, &inspected)
	if status != http.StatusOK || inspected.Sandbox.Status != "ready" || inspected.Sandbox.ID != created.Sandbox.ID {
		t.Fatalf("unexpected sandbox inspect response: status=%d response=%+v", status, inspected)
	}
	if !inspected.Sandbox.WorktreeExists || !inspected.Sandbox.BranchExists {
		t.Fatalf("expected inspect to confirm Git resources, got %+v", inspected.Sandbox)
	}

	var preview PreviewResponse
	status = requestJSON(t, http.MethodGet, server.URL+"/api/orders/808/preview-url", nil, &preview)
	if status != http.StatusOK || preview.Status != "ready" || preview.Sandbox.Preview.Provider != "lifecycle_stub" {
		t.Fatalf("unexpected order preview response: status=%d response=%+v", status, preview)
	}

	var tornDown SandboxActionResponse
	status = requestJSON(t, http.MethodDelete, server.URL+"/api/orders/808/sandbox", nil, &tornDown)
	if status != http.StatusOK || tornDown.Status != "success" || tornDown.Sandbox.Status != "removed" {
		t.Fatalf("unexpected sandbox teardown response: status=%d response=%+v", status, tornDown)
	}
	if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
		t.Fatalf("expected worktree to be removed, stat error=%v", err)
	}
	if output := runOrderGit(t, sourceRoot, "branch", "--list", "orders/lifecycle"); strings.TrimSpace(output) != "" {
		t.Fatalf("expected order branch to be removed, got %q", output)
	}

	status = requestJSON(t, http.MethodGet, server.URL+"/api/orders/808/sandbox", nil, &inspected)
	if status != http.StatusOK || inspected.Sandbox.Status != "removed" || inspected.Sandbox.WorktreeExists || inspected.Sandbox.BranchExists {
		t.Fatalf("expected removed sandbox inspection, status=%d response=%+v", status, inspected)
	}
	status = requestJSON(t, http.MethodGet, server.URL+created.Sandbox.Preview.URL, nil, &preview)
	if status != http.StatusGone || preview.Status != "removed" {
		t.Fatalf("expected revoked preview URL, status=%d response=%+v", status, preview)
	}
}

func requestJSON(t *testing.T, method string, url string, payload any, target any) int {
	t.Helper()
	var body *bytes.Reader
	if payload == nil {
		body = bytes.NewReader(nil)
	} else {
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal %s: %v", url, err)
		}
		body = bytes.NewReader(raw)
	}
	request, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatalf("create %s request: %v", url, err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("request %s: %v", url, err)
	}
	defer response.Body.Close()
	if target != nil {
		if err := json.NewDecoder(response.Body).Decode(target); err != nil {
			t.Fatalf("decode %s response: %v", url, err)
		}
	}
	return response.StatusCode
}

func initGitOrderSource(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("create git source: %v", err)
	}
	runOrderGit(t, root, "init", "-b", "main")
	runOrderGit(t, root, "config", "user.email", "test@example.com")
	runOrderGit(t, root, "config", "user.name", "Dashboard API Test")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("sandbox source\n"), 0o644); err != nil {
		t.Fatalf("write git source: %v", err)
	}
	runOrderGit(t, root, "add", "README.md")
	runOrderGit(t, root, "commit", "-m", "initial")
}

func runOrderGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, output)
	}
	return string(output)
}
