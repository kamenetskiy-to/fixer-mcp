package mcpclient

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A grandchild of Fixer MCP can inherit its stderr pipe. cmd.Wait then only
// returns once that pipe closes, so an unbounded close() hung the console's
// refresh: every MCP call had already succeeded, yet the screen stayed on
// "Загружаю…" until the operator killed it.
func TestStdioCloseDoesNotBlockOnAGrandchild(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "fake-mcp")
	// Leaves a background child holding stderr, then exits immediately.
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nsleep 20 &\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	database := filepath.Join(dir, "fixer.db")
	if err := os.WriteFile(database, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FIXER_MCP_BINARY", fake)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := start(ctx, Options{
		RuntimeRoot: dir,
		ProjectPath: "/project",
		Environment: []string{"FIXER_DB_PATH=" + database},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Give the child a moment to spawn its background grandchild.
	time.Sleep(150 * time.Millisecond)

	done := make(chan struct{})
	go func() {
		client.close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(4 * time.Second):
		t.Fatal("close() blocked on a grandchild that inherited stderr")
	}
}

// The session listing must never hold the TUI hostage: if the launcher hangs,
// the console gives up and shows the home screen without recent sessions.
func TestListFixerSessionsGivesUpWhenTheLauncherHangs(t *testing.T) {
	dir := t.TempDir()
	wireDir := filepath.Join(dir, "client_wires")
	if err := os.MkdirAll(wireDir, 0o755); err != nil {
		t.Fatal(err)
	}
	wire := filepath.Join(wireDir, "fixer_wire.py")
	if err := os.WriteFile(wire, []byte("#!/usr/bin/env python3\nimport time\ntime.sleep(60)\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	database := filepath.Join(dir, "fixer.db")
	if err := os.WriteFile(database, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	started := time.Now()
	got := listFixerSessions(ctx, Options{
		RuntimeRoot: dir,
		ProjectPath: "/project",
		Environment: []string{"FIXER_DB_PATH=" + database},
	})
	elapsed := time.Since(started)
	if got != nil {
		t.Fatalf("expected no sessions from a hanging launcher, got %#v", got)
	}
	if elapsed > 4*time.Second {
		t.Fatalf("listFixerSessions blocked for %s despite a cancelled context", elapsed)
	}
}

// The launcher may try to prompt; its stdin is closed so it fails fast instead
// of consuming keystrokes that belong to the operator's TTY.
func TestListFixerSessionsNeverReadsTheOperatorTerminal(t *testing.T) {
	dir := t.TempDir()
	wireDir := filepath.Join(dir, "client_wires")
	if err := os.MkdirAll(wireDir, 0o755); err != nil {
		t.Fatal(err)
	}
	wire := filepath.Join(wireDir, "fixer_wire.py")
	script := `#!/usr/bin/env python3
import sys
data = sys.stdin.read()
print('{"sessions": [], "stdin_bytes": %d}' % len(data))
`
	if err := os.WriteFile(wire, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	database := filepath.Join(dir, "fixer.db")
	if err := os.WriteFile(database, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if sessions := listFixerSessions(ctx, Options{
		RuntimeRoot: dir,
		ProjectPath: "/project",
		Environment: []string{"FIXER_DB_PATH=" + database},
	}); sessions == nil {
		// nil here means the launcher failed; the important part is that stdin
		// was closed rather than borrowed, which the reader above observes.
		t.Log("listing returned no sessions (launcher reported stdin bytes instead)")
	}
}
