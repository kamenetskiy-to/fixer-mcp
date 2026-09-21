// Package ui renders the fixerctl operator menu and drives entry execution.
package ui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/fixer-mcp/control-plane/internal/ansi"
	"github.com/fixer-mcp/control-plane/internal/registry"
	"github.com/fixer-mcp/control-plane/internal/vpnenv"
)

// ExecCommand is the PTY-backed command surface the menu needs. The runner
// package satisfies it; tests inject a fake so no test needs a real TTY.
type ExecCommand interface {
	tea.ExecCommand
	// Capture returns the sanitized tail of the child output.
	Capture() string
}

// CommandFactory builds the command for one entry at the given view size.
type CommandFactory func(entry registry.Resolved, rows, cols uint16) ExecCommand

// ReloadFunc re-resolves the menu and re-reads the VPN marker.
type ReloadFunc func() ([]registry.Resolved, vpnenv.Status, error)

// RunResult is the outcome of the last executed entry.
type RunResult struct {
	EntryID  string
	Display  string
	Err      error
	Duration time.Duration
	Tail     string
}

// Options carries everything the model needs. Nothing here requires a TTY.
type Options struct {
	Entries    []registry.Resolved
	Marker     vpnenv.Status
	GOOS       string
	ConfigPath string
	Version    string
	NewCommand CommandFactory
	Reload     ReloadFunc
	// LoadErr is a startup error (for example a malformed config) rendered in
	// the menu without preventing the operator from working.
	LoadErr error
}

// Model is the Bubble Tea state of the operator menu.
type Model struct {
	entries    []registry.Resolved
	cursor     int
	marker     vpnenv.Status
	goos       string
	configPath string
	version    string

	width    int
	height   int
	showHelp bool
	quitting bool
	loading  bool
	loadErr  error

	last       *RunResult
	startedAt  time.Time
	newCommand CommandFactory
	reload     ReloadFunc
}

// messages

type runFinishedMsg struct {
	entry    registry.Resolved
	err      error
	duration time.Duration
	tail     string
}

type reloadedMsg struct {
	entries []registry.Resolved
	marker  vpnenv.Status
	err     error
}

// New builds the model and places the cursor on the first enabled entry.
func New(opts Options) Model {
	m := Model{
		goos:       opts.GOOS,
		configPath: opts.ConfigPath,
		version:    opts.Version,
		marker:     opts.Marker,
		newCommand: opts.NewCommand,
		reload:     opts.Reload,
		loadErr:    opts.LoadErr,
		cursor:     -1,
	}
	m.setEntries(opts.Entries)
	return m
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd { return nil }

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case reloadedMsg:
		m.applyReload(msg)
		return m, nil

	case runFinishedMsg:
		m.loading = false
		m.last = &RunResult{
			EntryID:  msg.entry.ID,
			Display:  msg.entry.Display(),
			Err:      msg.err,
			Duration: msg.duration,
			Tail:     ansi.Strip(msg.tail),
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "up", "k":
		m.move(-1)
	case "down", "j":
		m.move(1)
	case "home", "g":
		m.moveToEdge(-1)
	case "end", "G":
		m.moveToEdge(1)
	case "?":
		m.showHelp = !m.showHelp
	case "esc":
		if m.showHelp {
			m.showHelp = false
		}
	case "r":
		if m.reload != nil {
			return m, reloadCmd(m.reload)
		}
	case "enter":
		return m.startSelected()
	}
	return m, nil
}

// selected returns the highlighted entry when it is runnable.
func (m Model) selected() (registry.Resolved, bool) {
	if m.cursor < 0 || m.cursor >= len(m.entries) {
		return registry.Resolved{}, false
	}
	entry := m.entries[m.cursor]
	if !entry.Enabled() {
		return registry.Resolved{}, false
	}
	return entry, true
}

// move moves the cursor by delta, skipping disabled entries. It stops at the
// first and last enabled entry instead of wrapping.
func (m *Model) move(delta int) {
	if len(m.entries) == 0 {
		return
	}
	i := m.cursor
	for {
		i += delta
		if i < 0 || i >= len(m.entries) {
			return
		}
		if m.entries[i].Enabled() {
			m.cursor = i
			return
		}
	}
}

// moveToEdge moves to the first (dir<0) or last (dir>0) enabled entry.
func (m *Model) moveToEdge(dir int) {
	if dir < 0 {
		for i := range m.entries {
			if m.entries[i].Enabled() {
				m.cursor = i
				return
			}
		}
		return
	}
	for i := len(m.entries) - 1; i >= 0; i-- {
		if m.entries[i].Enabled() {
			m.cursor = i
			return
		}
	}
}

// startSelected launches the highlighted entry in the terminal handed over by
// Bubble Tea. Disabled entries and empty menus return no command.
func (m Model) startSelected() (tea.Model, tea.Cmd) {
	entry, ok := m.selected()
	if !ok || m.newCommand == nil {
		return m, nil
	}
	rows, cols := m.execSize()
	command := m.newCommand(entry, rows, cols)
	started := time.Now()
	m.loading = true
	return m, tea.Exec(command, func(err error) tea.Msg {
		return runFinishedMsg{
			entry:    entry,
			err:      err,
			duration: time.Since(started),
			tail:     command.Capture(),
		}
	})
}

// execSize returns the PTY size for the child. Zero means "inherit the caller
// TTY", which the runner handles.
func (m Model) execSize() (uint16, uint16) {
	if m.width <= 0 || m.height <= 0 {
		return 0, 0
	}
	rows := m.height - 7
	if rows < 6 {
		rows = 6
	}
	cols := m.width - 2
	if cols < 20 {
		cols = 20
	}
	return uint16(rows), uint16(cols)
}

func (m *Model) applyReload(msg reloadedMsg) {
	if msg.err != nil {
		m.loadErr = msg.err
		return
	}
	m.loadErr = nil
	m.marker = msg.marker
	m.setEntries(msg.entries)
}

// setEntries replaces the list, trying to keep the cursor on the same entry.
func (m *Model) setEntries(entries []registry.Resolved) {
	previous := ""
	if m.cursor >= 0 && m.cursor < len(m.entries) {
		previous = m.entries[m.cursor].ID
	}
	m.entries = entries
	m.cursor = -1
	if previous != "" {
		for i, entry := range entries {
			if entry.ID == previous && entry.Enabled() {
				m.cursor = i
				break
			}
		}
	}
	if m.cursor < 0 {
		for i, entry := range entries {
			if entry.Enabled() {
				m.cursor = i
				break
			}
		}
	}
}

func reloadCmd(fn ReloadFunc) tea.Cmd {
	return func() tea.Msg {
		entries, marker, err := fn()
		return reloadedMsg{entries: entries, marker: marker, err: err}
	}
}

// EnabledCount returns the number of runnable entries.
func (m Model) EnabledCount() int {
	count := 0
	for _, entry := range m.entries {
		if entry.Enabled() {
			count++
		}
	}
	return count
}

// CursorID returns the ID under the cursor ("" when nothing is selectable).
func (m Model) CursorID() string {
	if m.cursor < 0 || m.cursor >= len(m.entries) {
		return ""
	}
	return m.entries[m.cursor].ID
}

// Quitting reports whether the quit key was pressed.
func (m Model) Quitting() bool { return m.quitting }

// Loading reports whether a child process is in flight.
func (m Model) Loading() bool { return m.loading }
