package ui

import (
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/fixer-mcp/control-plane/internal/registry"
	"github.com/fixer-mcp/control-plane/internal/vpnenv"
)

type fakeExec struct {
	ran      bool
	captured string
}

func (f *fakeExec) Run() error          { f.ran = true; return nil }
func (f *fakeExec) SetStdin(io.Reader)  {}
func (f *fakeExec) SetStdout(io.Writer) {}
func (f *fakeExec) SetStderr(io.Writer) {}
func (f *fakeExec) Capture() string     { return f.captured }

func entry(id, title string, enabled bool) registry.Resolved {
	r := registry.Resolved{
		Entry:  registry.Entry{ID: id, Title: title, Help: "help for " + id},
		Binary: "/usr/local/bin/" + id,
	}
	if !enabled {
		r.Disabled = "missing: " + id
	}
	return r
}

func key(s string) tea.KeyMsg {
	switch s {
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func update(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	model, ok := next.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want ui.Model", next)
	}
	return model, cmd
}

func TestNewSelectsFirstEnabledEntry(t *testing.T) {
	m := New(Options{Entries: []registry.Resolved{
		entry("ai-pro", "ai-pro", false),
		entry("cml", "check-my-limits", true),
		entry("myip", "my ip", true),
	}})
	if got := m.CursorID(); got != "cml" {
		t.Fatalf("cursor = %q, want cml", got)
	}
}

func TestNavigationSkipsDisabledEntries(t *testing.T) {
	m := New(Options{Entries: []registry.Resolved{
		entry("a", "a", true),
		entry("b", "b", false),
		entry("c", "c", true),
	}})

	m, _ = update(t, m, key("j"))
	if got := m.CursorID(); got != "c" {
		t.Fatalf("after down cursor = %q, want c (b is disabled)", got)
	}
	m, _ = update(t, m, key("j"))
	if got := m.CursorID(); got != "c" {
		t.Fatalf("down at the end moved to %q, want to stay on c", got)
	}
	m, _ = update(t, m, key("k"))
	if got := m.CursorID(); got != "a" {
		t.Fatalf("after up cursor = %q, want a (b is disabled)", got)
	}
	m, _ = update(t, m, key("k"))
	if got := m.CursorID(); got != "a" {
		t.Fatalf("up at the start moved to %q, want to stay on a", got)
	}
}

func TestArrowKeysAndEdges(t *testing.T) {
	m := New(Options{Entries: []registry.Resolved{
		entry("a", "a", true),
		entry("b", "b", true),
		entry("c", "c", true),
	}})
	m, _ = update(t, m, key("down"))
	if m.CursorID() != "b" {
		t.Fatalf("cursor = %q, want b", m.CursorID())
	}
	m, _ = update(t, m, key("G"))
	if m.CursorID() != "c" {
		t.Fatalf("cursor = %q, want c", m.CursorID())
	}
	m, _ = update(t, m, key("g"))
	if m.CursorID() != "a" {
		t.Fatalf("cursor = %q, want a", m.CursorID())
	}
}

func TestAllDisabledMenuHasNoSelectableEntry(t *testing.T) {
	m := New(Options{Entries: []registry.Resolved{
		entry("ai-pro", "ai-pro", false),
		entry("ghost", "ghost", false),
	}})
	if m.CursorID() != "" {
		t.Fatalf("cursor = %q, want empty", m.CursorID())
	}
	if _, ok := m.selected(); ok {
		t.Fatal("selected() should report no selection")
	}
	m, cmd := update(t, m, key("enter"))
	if cmd != nil {
		t.Fatal("enter on an all-disabled menu must not produce a command")
	}
	if m.Loading() {
		t.Fatal("enter on an all-disabled menu must not start a run")
	}
	// Navigation is a no-op too.
	m, _ = update(t, m, key("j"))
	if m.CursorID() != "" {
		t.Fatalf("cursor = %q, want empty", m.CursorID())
	}
}

func TestQuitKeys(t *testing.T) {
	for _, name := range []string{"q", "ctrl+c"} {
		t.Run(name, func(t *testing.T) {
			m := New(Options{Entries: []registry.Resolved{entry("a", "a", true)}})
			m, cmd := update(t, m, key(name))
			if !m.Quitting() {
				t.Fatal("model is not quitting")
			}
			if cmd == nil {
				t.Fatal("expected a quit command")
			}
			if _, ok := cmd().(tea.QuitMsg); !ok {
				t.Fatal("expected tea.QuitMsg")
			}
		})
	}
}

func TestEnterStartsSelectedCommand(t *testing.T) {
	fake := &fakeExec{captured: "limits table"}
	var gotID string
	m := New(Options{
		Entries: []registry.Resolved{
			entry("ai-pro", "ai-pro", false),
			entry("cml", "check-my-limits", true),
		},
		NewCommand: func(e registry.Resolved, rows, cols uint16) ExecCommand {
			gotID = e.ID
			return fake
		},
	})
	m, cmd := update(t, m, key("enter"))
	if cmd == nil {
		t.Fatal("expected an exec command")
	}
	if gotID != "cml" {
		t.Fatalf("factory got %q, want cml", gotID)
	}
	if !m.Loading() {
		t.Fatal("model should report loading")
	}

	m, _ = update(t, m, runFinishedMsg{entry: entry("cml", "check-my-limits", true), duration: 1200 * time.Millisecond, tail: "limits table"})
	if m.Loading() {
		t.Fatal("run should have finished")
	}
	if m.last == nil || m.last.EntryID != "cml" {
		t.Fatalf("last run = %+v", m.last)
	}
	if view := m.View(); !strings.Contains(view, "last: check-my-limits exit 0") {
		t.Fatalf("view does not show the last run:\n%s", view)
	}
}

func TestEnterWithoutFactoryIsNoOp(t *testing.T) {
	m := New(Options{Entries: []registry.Resolved{entry("a", "a", true)}})
	_, cmd := update(t, m, key("enter"))
	if cmd != nil {
		t.Fatal("expected no command without a factory")
	}
}

func TestExecSizeDerivedFromWindow(t *testing.T) {
	m := New(Options{Entries: []registry.Resolved{entry("a", "a", true)}})
	rows, cols := m.execSize()
	if rows != 0 || cols != 0 {
		t.Fatalf("execSize before a window size = (%d,%d), want (0,0)", rows, cols)
	}
	m, _ = update(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	rows, cols = m.execSize()
	if rows != 33 || cols != 118 {
		t.Fatalf("execSize = (%d,%d), want (33,118)", rows, cols)
	}
}

func TestReloadKeepsCursorByID(t *testing.T) {
	m := New(Options{
		Entries: []registry.Resolved{entry("a", "a", true), entry("b", "b", true)},
		Reload: func() ([]registry.Resolved, vpnenv.Status, error) {
			return []registry.Resolved{entry("b", "b", true), entry("a", "a", true)},
				vpnenv.Status{Path: "/tmp/vpn.env", Active: true, Values: map[string]string{"HTTPS_PROXY": "x"}},
				nil
		},
	})
	m, _ = update(t, m, key("j"))
	if m.CursorID() != "b" {
		t.Fatalf("cursor = %q, want b", m.CursorID())
	}
	m, cmd := update(t, m, key("r"))
	if cmd == nil {
		t.Fatal("expected a reload command")
	}
	m, _ = update(t, m, cmd())
	if m.CursorID() != "b" {
		t.Fatalf("cursor after reload = %q, want b", m.CursorID())
	}
	if !strings.Contains(m.View(), "active") {
		t.Fatalf("reloaded marker not shown:\n%s", m.View())
	}
}

func TestReloadErrorIsSurfaced(t *testing.T) {
	m := New(Options{
		Entries: []registry.Resolved{entry("a", "a", true)},
		Reload: func() ([]registry.Resolved, vpnenv.Status, error) {
			return nil, vpnenv.Status{}, errors.New("bad config")
		},
	})
	m, cmd := update(t, m, key("r"))
	m, _ = update(t, m, cmd())
	if m.loadErr == nil {
		t.Fatal("expected the reload error to be recorded")
	}
	if !strings.Contains(m.View(), "bad config") {
		t.Fatalf("view does not show the reload error:\n%s", m.View())
	}
}

func TestViewRendersStatusAndDisabledReasons(t *testing.T) {
	m := New(Options{
		Entries: []registry.Resolved{
			entry("cml", "check-my-limits", true),
			entry("ai-pro", "ai-pro", false),
		},
		GOOS:       "darwin",
		ConfigPath: "/tmp/control-plane.json",
		Marker:     vpnenv.Status{Path: "/tmp/vpn.env", Active: true, Values: map[string]string{"HTTPS_PROXY": "x"}},
		Version:    "v0.1.0",
	})
	m, _ = update(t, m, tea.WindowSizeMsg{Width: 140, Height: 40})
	view := m.View()

	for _, want := range []string{"fixerctl", "darwin", "check-my-limits", "missing: ai-pro", "active", "1/2 runnable"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
}

func TestHelpPanelToggles(t *testing.T) {
	m := New(Options{Entries: []registry.Resolved{entry("a", "a", true)}})
	m, _ = update(t, m, key("?"))
	if view := m.View(); !strings.Contains(view, "path_overrides") {
		t.Fatalf("help panel not rendered:\n%s", view)
	}
	m, _ = update(t, m, key("esc"))
	if view := m.View(); strings.Contains(view, "path_overrides") {
		t.Fatalf("help panel did not close:\n%s", view)
	}
}

func TestVPNLaneIsVisibleForPlatformEntries(t *testing.T) {
	laned := registry.Resolved{
		Entry:  registry.Entry{ID: "vpn-up", Title: "VPN up", LaneByPlatform: map[string]string{"darwin": "launchd"}},
		Binary: "/usr/local/bin/vpn-up",
		Lane:   "launchd",
	}
	m := New(Options{Entries: []registry.Resolved{laned}, GOOS: "darwin"})
	if view := m.View(); !strings.Contains(view, "launchd") {
		t.Fatalf("lane not rendered:\n%s", view)
	}
}

func TestSanitizedTailIsShownWithoutEscapes(t *testing.T) {
	m := New(Options{Entries: []registry.Resolved{entry("a", "a", true)}})
	m, _ = update(t, m, runFinishedMsg{
		entry: entry("a", "a", true),
		err:   errors.New("exit status 1"),
		tail:  "\x1b[31mfailed\x1b[0m\nsecond line",
	})
	view := m.View()
	if !strings.Contains(view, "failed") || !strings.Contains(view, "exit status 1") {
		t.Fatalf("tail not rendered:\n%s", view)
	}
	if strings.Contains(view, "\x1b[31m") {
		t.Fatalf("raw escape sequence leaked into the view:\n%q", view)
	}
}
