// Package mcpclient reads project work through the Fixer MCP boundary.
//
// The console must not query SQLite directly or manufacture a second project
// ledger. It starts the managed stdio server with the configured database and
// asks it for the same project-scoped views used by Fixer and Hands.
package mcpclient

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Session struct {
	ID          int    `json:"id"`
	Status      string `json:"status"`
	TaskSummary string `json:"task_summary"`
	Backend     string `json:"cli_backend"`
	Model       string `json:"cli_model"`
	Reasoning   string `json:"cli_reasoning"`
	Kind        string `json:"session_kind"`
	WaveID      int    `json:"wave_id"`
}

// Lane is one Project Hands execution lane as advertised by Fixer MCP. The
// console never invents this list: only lanes the control plane can persist
// may be offered, otherwise a preset launch dies inside the launcher.
type Lane struct {
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	Reasoning string `json:"reasoning"`
}

type Hands struct {
	Available   bool
	State       string
	QueuedCount int
	ActiveText  string
	ActiveState string
	ActiveLane  string
	// DefaultLane is the project's registered default lane; Lanes is the full
	// registered set. Both are authoritative for the native work screen.
	DefaultLane string
	Lanes       []Lane
}

// MCPServer is one registry-backed MCP server offered to the native selection
// screens.
type MCPServer struct {
	Name             string `json:"name"`
	Category         string `json:"category"`
	ShortDescription string `json:"short_description"`
	IsDefault        bool   `json:"is_default"`
	AutoAttach       bool   `json:"auto_attach"`
	Archived         bool   `json:"archived"`
}

// ProjectDoc is one canonical project document. DocID is the project-local
// rank, which is exactly the id space the Hands doc selection expects.
type ProjectDoc struct {
	DocID   int    `json:"doc_id"`
	Title   string `json:"title"`
	Path    string `json:"path"`
	Level   int    `json:"level"`
	DocType string `json:"doc_type"`
	Status  string `json:"status"`
}

type HandsInstruction struct {
	ID        string `json:"instruction_id"`
	Ordinal   int    `json:"ordinal"`
	Text      string `json:"instruction_text"`
	Lane      string `json:"requested_lane"`
	State     string `json:"state"`
	CreatedAt string `json:"created_at"`
}

// FixerSession is a resumable Fixer provider session. Unlike Netrunner work it
// lives in provider transcript history, not in the Fixer MCP database, so the
// console lists it through a read-only wire query instead of a selector.
type FixerSession struct {
	SessionID string `json:"session_id"`
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	Preview   string `json:"preview"`
	// CWD is the directory the session was recorded in. Resuming it anywhere
	// else makes the provider stop and ask which working directory to use.
	CWD     string `json:"cwd"`
	Updated string `json:"updated"`
}

type Snapshot struct {
	Available bool
	ProjectID int
	Overview  string
	Hands     Hands
	Sessions  []Session
	Error     string

	// Native selection pools for the work screens.
	MCPPool           []MCPServer
	ProjectAllowedMCP []string
	HandsMCP          []string
	DocPool           []ProjectDoc
	HandsDocs         []int
	HandsInstructions []HandsInstruction
	FixerSessions     []FixerSession
}

type Options struct {
	RuntimeRoot string
	ProjectPath string
	// CWD is the directory the operator (or launcher) is standing in. It is
	// only used to discover stray bare databases; an empty value falls back to
	// the process working directory.
	CWD         string
	Environment []string
}

// Inspect performs a read-only project-scoped MCP session. It deliberately
// does not register a project or write state when a database is unavailable.
// debugf writes a trace line when FIXER_MCP_DEBUG is set. Tracing the console's
// MCP boundary is the only way to see which step stalls on a host where every
// call works when driven by hand.
func debugf(format string, args ...any) {
	if strings.TrimSpace(os.Getenv("FIXER_MCP_DEBUG")) == "" {
		return
	}
	fmt.Fprintf(os.Stderr, "[mcpclient] "+format+"\n", args...)
}

func Inspect(ctx context.Context, opts Options) Snapshot {
	debugf("start project=%s", opts.ProjectPath)
	client, err := start(ctx, opts)
	if err != nil {
		debugf("start failed: %v", err)
		return Snapshot{Error: err.Error()}
	}
	defer func() {
		debugf("closing MCP client")
		client.close()
		debugf("closed")
	}()

	if err := client.initialize(); err != nil {
		debugf("initialize failed: %v", err)
		return Snapshot{Error: "MCP initialize: " + err.Error()}
	}
	debugf("initialized")
	var auth struct {
		Status string `json:"status"`
	}
	if err := client.callTool("assume_role", map[string]any{"role": "fixer", "cwd": opts.ProjectPath}, &auth); err != nil {
		debugf("assume_role failed: %v", err)
		return Snapshot{Error: "MCP project binding: " + conciseToolError(err)}
	}
	debugf("assume_role status=%q", auth.Status)
	if auth.Status != "success" {
		return Snapshot{Error: "MCP project binding was not accepted"}
	}

	result := Snapshot{Available: true}
	var overview struct {
		ProjectID   int  `json:"project_id"`
		HasOverview bool `json:"has_overview"`
		Overview    struct {
			Content string `json:"content"`
		} `json:"overview"`
	}
	if err := client.callTool("get_project_overview", map[string]any{}, &overview); err != nil {
		debugf("overview failed: %v", err)
		return Snapshot{Error: "MCP overview: " + err.Error()}
	}
	result.ProjectID = overview.ProjectID
	if overview.HasOverview {
		result.Overview = strings.TrimSpace(overview.Overview.Content)
	}

	var sessions struct {
		Sessions []Session `json:"sessions"`
	}
	if err := client.callTool("list_project_sessions", map[string]any{"limit": 8}, &sessions); err != nil {
		return Snapshot{Error: "MCP sessions: " + err.Error()}
	}
	result.Sessions = sessions.Sessions

	var hands struct {
		DerivedState string `json:"derived_state"`
		QueuedCount  int    `json:"queued_count"`
		DefaultLane  string `json:"default_lane"`
		Lanes        []Lane `json:"lanes"`
		Active       *struct {
			InstructionText string `json:"instruction_text"`
			State           string `json:"state"`
			RequestedLane   string `json:"requested_lane"`
		} `json:"active_instruction"`
	}
	if err := client.callTool("get_hands_state", map[string]any{}, &hands); err == nil {
		result.Hands.Available = true
		result.Hands.State = hands.DerivedState
		result.Hands.QueuedCount = hands.QueuedCount
		result.Hands.DefaultLane = strings.TrimSpace(hands.DefaultLane)
		result.Hands.Lanes = append([]Lane(nil), hands.Lanes...)
		if hands.Active != nil {
			result.Hands.ActiveText = strings.TrimSpace(hands.Active.InstructionText)
			result.Hands.ActiveState = hands.Active.State
			if hands.Active.RequestedLane != "" {
				result.Hands.ActiveLane = hands.Active.RequestedLane
			}
		}
	}

	debugf("views loaded overview=%d sessions=%d", result.ProjectID, len(result.Sessions))
	result.loadSelectionPools(client)
	debugf("selection pools loaded mcp=%d docs=%d instructions=%d", len(result.MCPPool), len(result.DocPool), len(result.HandsInstructions))
	// A listing must never fail the whole snapshot: the work screens degrade to
	// "keep" when the provider-side list is unavailable.
	result.FixerSessions = listFixerSessions(ctx, opts)
	debugf("fixer sessions listed: %d", len(result.FixerSessions))
	return result
}

// loadSelectionPools asks Fixer MCP for everything the native Руки screen
// needs: the registry-backed server pool, the project's current proposal, the
// canonical document tree and the documents currently proposed for Руки.
// Individual failures are ignored so one broken view cannot blank the screen.
func (result *Snapshot) loadSelectionPools(client *stdioClient) {
	var allMCP struct {
		Servers []MCPServer `json:"servers"`
	}
	if err := client.callTool("list_mcp_servers", map[string]any{"include_all": true}, &allMCP); err == nil && len(allMCP.Servers) > 0 {
		result.MCPPool = allMCP.Servers
	}
	var projectMCP struct {
		Servers []MCPServer `json:"servers"`
	}
	if err := client.callTool("get_project_mcp_servers", map[string]any{}, &projectMCP); err == nil {
		if len(result.MCPPool) == 0 {
			result.MCPPool = projectMCP.Servers
		}
		result.ProjectAllowedMCP = make([]string, 0, len(projectMCP.Servers))
		for _, s := range projectMCP.Servers {
			result.ProjectAllowedMCP = append(result.ProjectAllowedMCP, s.Name)
		}
	}
	var handsMCP struct {
		Names []string `json:"mcp_server_names"`
	}
	if err := client.callTool("get_project_hands_mcp_servers", map[string]any{}, &handsMCP); err == nil {
		result.HandsMCP = handsMCP.Names
	}
	var docs struct {
		Docs []ProjectDoc `json:"docs"`
	}
	if err := client.callTool("check_current_project_docs", map[string]any{}, &docs); err == nil {
		result.DocPool = docs.Docs
	}
	var handsDocs struct {
		DocIDs []int `json:"project_doc_ids"`
	}
	if err := client.callTool("get_project_hands_docs", map[string]any{}, &handsDocs); err == nil {
		result.HandsDocs = handsDocs.DocIDs
	}
	var instructions struct {
		Instructions []HandsInstruction `json:"instructions"`
	}
	if err := client.callTool("list_hands_instructions", map[string]any{"limit": 5}, &instructions); err == nil {
		result.HandsInstructions = instructions.Instructions
	}
}

func callFixerTool(ctx context.Context, opts Options, toolName string, args map[string]any, out any) error {
	client, err := start(ctx, opts)
	if err != nil {
		return err
	}
	defer client.close()
	if err := client.initialize(); err != nil {
		return err
	}
	var auth struct {
		Status string `json:"status"`
	}
	if err := client.callTool("assume_role", map[string]any{"role": "fixer", "cwd": opts.ProjectPath}, &auth); err != nil {
		return fmt.Errorf("MCP project binding: %w", err)
	}
	if auth.Status != "success" {
		return errors.New("MCP project binding was not accepted")
	}
	return client.callTool(toolName, args, out)
}

func SetProjectMCPServers(ctx context.Context, opts Options, names []string) error {
	var out struct {
		Status string `json:"status"`
	}
	return callFixerTool(ctx, opts, "set_project_mcp_servers", map[string]any{
		"mcp_server_names": names,
	}, &out)
}

func SetProjectHandsMCPServers(ctx context.Context, opts Options, names []string) error {
	var out struct {
		Status string `json:"status"`
	}
	return callFixerTool(ctx, opts, "set_project_hands_mcp_servers", map[string]any{
		"mcp_server_names": names,
	}, &out)
}

func SetProjectHandsDocs(ctx context.Context, opts Options, docIDs []int) error {
	var out struct {
		Status string `json:"status"`
	}
	return callFixerTool(ctx, opts, "set_project_hands_docs", map[string]any{
		"project_doc_ids": docIDs,
	}, &out)
}

// listFixerSessions reads recent Fixer sessions through the wire's read-only
// JSON listing. Fixer sessions are provider transcripts, so Fixer MCP has no
// row for them yet; this stays a bounded, non-interactive query until that
// bookkeeping moves behind MCP.
func listFixerSessions(ctx context.Context, opts Options) []FixerSession {
	wire := filepath.Join(opts.RuntimeRoot, "client_wires", "fixer_wire.py")
	if opts.RuntimeRoot == "" {
		wire = filepath.Join("client_wires", "fixer_wire.py")
	}
	if info, err := os.Stat(wire); err != nil || info.IsDir() {
		return nil
	}
	python := "python3"
	if resolved, err := exec.LookPath("python3"); err == nil {
		python = resolved
	}
	cmd := exec.CommandContext(ctx, python, wire, "--list-fixer-sessions", "--limit", "5")
	env := append([]string(nil), opts.Environment...)
	if len(env) == 0 {
		env = os.Environ()
	}
	if !hasEnv(env, "FIXER_DB_PATH") {
		if databasePath, err := DatabasePath(opts); err == nil {
			env = append(env, "FIXER_DB_PATH="+databasePath)
		}
	}
	cmd.Env = env
	// EOF instead of the operator's terminal: a launcher that decides to prompt
	// must fail fast, never read keystrokes meant for the TUI.
	cmd.Stdin = bytes.NewReader(nil)
	type listResult struct {
		output []byte
		err    error
	}
	done := make(chan listResult, 1)
	go func() {
		output, err := cmd.Output()
		done <- listResult{output: output, err: err}
	}()
	var output []byte
	select {
	case result := <-done:
		if result.err != nil {
			return nil
		}
		output = result.output
	case <-ctx.Done():
		// The child is killed by CommandContext; the TUI must not wait for a
		// process that outlived the deadline.
		return nil
	case <-time.After(15 * time.Second):
		return nil
	}
	var payload struct {
		Sessions []FixerSession `json:"sessions"`
		Error    string         `json:"error"`
	}
	if err := json.Unmarshal(output, &payload); err != nil || payload.Error != "" {
		return nil
	}
	return payload.Sessions
}

type stdioClient struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	stderr *bytes.Buffer
	nextID int
}

func start(ctx context.Context, opts Options) (*stdioClient, error) {
	binary, err := binaryPath(opts.RuntimeRoot)
	if err != nil {
		return nil, err
	}
	env := append([]string(nil), opts.Environment...)
	if len(env) == 0 {
		env = os.Environ()
	}
	if !hasEnv(env, "FIXER_DB_PATH") {
		databasePath, findErr := DatabasePath(opts)
		if findErr != nil {
			return nil, findErr
		}
		env = append(env, "FIXER_DB_PATH="+databasePath)
	}
	stderr := &bytes.Buffer{}
	cmd := exec.CommandContext(ctx, binary)
	cmd.Dir = filepath.Dir(binary)
	cmd.Env = env
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("open MCP stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("open MCP stdout: %w", err)
	}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start Fixer MCP: %w", err)
	}
	return &stdioClient{cmd: cmd, stdin: stdin, stdout: bufio.NewReader(stdout), stderr: stderr, nextID: 1}, nil
}

func (c *stdioClient) close() {
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	// Wait is bounded on purpose. cmd.Wait also waits for the goroutine that
	// copies the child's stderr, and that copy only ends when the pipe closes:
	// a grandchild which inherited stderr keeps it open, so an unbounded Wait
	// hung Inspect forever and the console stayed on "Загружаю…" while every
	// MCP call had already succeeded (observed on d0lsi Invoker).
	waited := make(chan error, 1)
	go func() { waited <- c.cmd.Wait() }()
	select {
	case <-waited:
	case <-time.After(2 * time.Second):
	}
}

func (c *stdioClient) initialize() error {
	var ignored map[string]any
	if err := c.request("initialize", map[string]any{
		"protocolVersion": "2025-06-18",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]string{"name": "fixer-console", "version": "1"},
	}, &ignored); err != nil {
		return err
	}
	return c.notify("notifications/initialized", map[string]any{})
}

func (c *stdioClient) callTool(name string, arguments map[string]any, out any) error {
	var result toolResult
	if err := c.request("tools/call", map[string]any{"name": name, "arguments": arguments}, &result); err != nil {
		return err
	}
	if result.IsError {
		return errors.New(result.errorText())
	}
	payload := result.StructuredContent
	if len(payload) == 0 || string(payload) == "null" {
		for _, item := range result.Content {
			if item.Type == "text" && strings.TrimSpace(item.Text) != "" {
				payload = json.RawMessage(item.Text)
				break
			}
		}
	}
	if len(payload) == 0 {
		return errors.New("tool returned no structured content")
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return fmt.Errorf("decode tool response: %w", err)
	}
	return nil
}

type toolResult struct {
	IsError           bool            `json:"isError"`
	StructuredContent json.RawMessage `json:"structuredContent"`
	Content           []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

func (r toolResult) errorText() string {
	for _, item := range r.Content {
		if item.Type == "text" && strings.TrimSpace(item.Text) != "" {
			return strings.TrimSpace(item.Text)
		}
	}
	return "MCP tool rejected the request"
}

func (c *stdioClient) notify(method string, params any) error {
	payload := map[string]any{"jsonrpc": "2.0", "method": method, "params": params}
	return c.write(payload)
}

func (c *stdioClient) request(method string, params any, out any) error {
	id := c.nextID
	c.nextID++
	if err := c.write(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		return err
	}
	for {
		line, err := c.stdout.ReadBytes('\n')
		if err != nil {
			stderr := strings.TrimSpace(c.stderr.String())
			if stderr != "" {
				return fmt.Errorf("MCP exited: %w: %s", err, stderr)
			}
			return fmt.Errorf("MCP response: %w", err)
		}
		var response struct {
			ID     *int            `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if err := json.Unmarshal(line, &response); err != nil {
			return fmt.Errorf("decode MCP response: %w", err)
		}
		if response.ID == nil || *response.ID != id {
			continue
		}
		if len(response.Error) > 0 && string(response.Error) != "null" {
			return fmt.Errorf("MCP response error: %s", strings.TrimSpace(string(response.Error)))
		}
		if err := json.Unmarshal(response.Result, out); err != nil {
			return fmt.Errorf("decode MCP result: %w", err)
		}
		return nil
	}
}

func (c *stdioClient) write(value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = c.stdin.Write(append(payload, '\n'))
	return err
}

var toolMessagePattern = regexp.MustCompile(`"message"\s*:\s*"((?:[^"\\]|\\.)*)"`)

// conciseToolError pulls the human sentence out of a tool error. MCP tools
// return `{"message": "..."}` blobs; showing the raw JSON in the console only
// hides what the operator must do next.
func conciseToolError(err error) string {
	if err == nil {
		return ""
	}
	text := strings.TrimSpace(err.Error())
	if match := toolMessagePattern.FindStringSubmatch(text); match != nil {
		if decoded, decodeErr := strconv.Unquote(`"` + match[1] + `"`); decodeErr == nil && strings.TrimSpace(decoded) != "" {
			text = strings.TrimSpace(decoded)
		}
	}
	text = strings.TrimPrefix(text, "Auth Error: ")
	text = strings.Join(strings.Fields(text), " ")
	if len(text) > 240 {
		text = text[:237] + "…"
	}
	return text
}

func binaryPath(runtimeRoot string) (string, error) {
	if configured := strings.TrimSpace(os.Getenv("FIXER_MCP_BINARY")); configured != "" {
		if info, err := os.Stat(configured); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return configured, nil
		}
		return "", fmt.Errorf("FIXER_MCP_BINARY is not executable: %s", configured)
	}
	candidate := filepath.Join(runtimeRoot, "fixer_mcp", "fixer_mcp")
	if info, err := os.Stat(candidate); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
		return candidate, nil
	}
	return "", fmt.Errorf("managed Fixer MCP binary is unavailable")
}

// DatabasePath resolves the MCP state file used for a project. It is exposed
// so a Hands/Fixer child receives the same canonical state as the console,
// rather than independently onboarding an already-known project.
func DatabasePath(opts Options) (string, error) {
	env := opts.Environment
	if len(env) == 0 {
		env = os.Environ()
	}
	if configured := strings.TrimSpace(envMap(env)["FIXER_DB_PATH"]); configured != "" {
		return configured, nil
	}
	return dbPath(opts.RuntimeRoot, opts.ProjectPath, env)
}

// IgnoredStrayDBs lists bare `fixer.db` files that the canonical rule refuses
// to select: one next to the project root and one in the current directory.
// A stray is never bound silently. It is reported so the operator can adopt it
// explicitly with FIXER_DB_PATH, which keeps the console and the launcher from
// disagreeing about which project world is real.
func IgnoredStrayDBs(opts Options) []string {
	env := opts.Environment
	if len(env) == 0 {
		env = os.Environ()
	}
	canonical := ""
	if resolved, err := DatabasePath(Options{
		RuntimeRoot: opts.RuntimeRoot,
		ProjectPath: opts.ProjectPath,
		CWD:         opts.CWD,
		Environment: env,
	}); err == nil {
		canonical = filepath.Clean(resolved)
	}
	roots := []string{strings.TrimSpace(opts.ProjectPath)}
	if cwd := strings.TrimSpace(opts.CWD); cwd != "" {
		roots = append(roots, cwd)
	} else if cwd, err := os.Getwd(); err == nil {
		roots = append(roots, cwd)
	}
	seen := map[string]bool{}
	strays := make([]string, 0, len(roots))
	for _, root := range roots {
		if root == "" {
			continue
		}
		candidate := filepath.Clean(filepath.Join(root, "fixer.db"))
		if seen[candidate] || candidate == canonical {
			continue
		}
		seen[candidate] = true
		if isRegularFile(candidate) {
			strays = append(strays, candidate)
		}
	}
	return strays
}

// dbPath implements the pinned canonical rule shared with the Python launcher:
//
//  1. FIXER_DB_PATH — handled by DatabasePath, used as-is.
//  2. <project_root>/fixer_mcp/fixer.db — a source checkout, when it exists.
//  3. Host canonical state path, first that exists: FIXER_STATE_DIR,
//     $XDG_STATE_HOME, ~/.local/state. When none exists yet, name the preferred
//     state path and create its parent with mode 0700 so Fixer MCP can
//     bootstrap its schema on the first start.
//  4. Bare <project_root>/fixer.db and <cwd>/fixer.db are strays and are never
//     silently selected — they only ever win through FIXER_DB_PATH.
//  5. <managed runtime root>/fixer_mcp/fixer.db is not a candidate at all:
//     state inside a release tree is deleted by every update.
//
// runtimeRoot is intentionally unused; it is retained in the signature so the
// pinned rule stays one function and callers cannot reintroduce a managed
// payload candidate.
func dbPath(runtimeRoot, projectPath string, env []string) (string, error) {
	_ = runtimeRoot
	values := envMap(env)
	// A project-local MCP DB is authoritative for a source checkout. Prefer it
	// over a stale user snapshot so the known project never falls back into
	// legacy onboarding when opening Hands or Fixer from that checkout.
	projectDB := filepath.Clean(filepath.Join(projectPath, "fixer_mcp", "fixer.db"))
	if isRegularFile(projectDB) {
		return projectDB, nil
	}
	// The host canonical state path comes next, in one fixed priority order.
	for _, candidate := range stateCandidates(values) {
		candidate = filepath.Clean(candidate)
		if isRegularFile(candidate) {
			return candidate, nil
		}
	}
	// Nothing exists yet. A fresh machine must still get the canonical state
	// path so Fixer MCP can bootstrap its schema on the first start — the very
	// promise `fixer doctor` makes ("uninitialized, will initialize on first
	// launch"). Refusing here left every new host permanently context-less.
	if preferred := preferredStatePath(values); preferred != "" {
		if err := os.MkdirAll(filepath.Dir(preferred), 0o700); err == nil {
			return filepath.Clean(preferred), nil
		}
	}
	return "", errors.New("канонический Fixer MCP state пока недоступен на этой машине")
}

// stateCandidates is the host canonical state path in pinned priority order.
// It never contains a bare project/cwd database or a managed payload database.
func stateCandidates(values map[string]string) []string {
	var candidates []string
	if stateDir := strings.TrimSpace(values["FIXER_STATE_DIR"]); stateDir != "" {
		candidates = append(candidates, filepath.Join(stateDir, "fixer.db"))
	}
	if stateHome := strings.TrimSpace(values["XDG_STATE_HOME"]); stateHome != "" {
		candidates = append(candidates, filepath.Join(stateHome, "fixer-client-wires", "fixer.db"))
	}
	if home := strings.TrimSpace(values["HOME"]); home != "" {
		candidates = append(candidates, filepath.Join(home, ".local", "state", "fixer-client-wires", "fixer.db"))
	}
	return candidates
}

func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// preferredStatePath mirrors the installer's database resolution: explicit
// state dir, then XDG, then the private directory under the user's home.
func preferredStatePath(values map[string]string) string {
	if stateDir := strings.TrimSpace(values["FIXER_STATE_DIR"]); stateDir != "" {
		return filepath.Join(stateDir, "fixer.db")
	}
	if stateHome := strings.TrimSpace(values["XDG_STATE_HOME"]); stateHome != "" {
		return filepath.Join(stateHome, "fixer-client-wires", "fixer.db")
	}
	if home := strings.TrimSpace(values["HOME"]); home != "" {
		return filepath.Join(home, ".local", "state", "fixer-client-wires", "fixer.db")
	}
	return ""
}

func hasEnv(env []string, key string) bool {
	value, ok := envMap(env)[key]
	return ok && strings.TrimSpace(value) != ""
}

func envMap(env []string) map[string]string {
	result := make(map[string]string, len(env))
	for _, item := range env {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			result[key] = value
		}
	}
	return result
}
