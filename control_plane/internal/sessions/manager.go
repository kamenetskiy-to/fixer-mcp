package sessions

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/fixer-mcp/control-plane/internal/domain"
	"github.com/fixer-mcp/control-plane/internal/launch"
	"github.com/fixer-mcp/control-plane/internal/registry"
	"github.com/fixer-mcp/control-plane/internal/runner"
	"github.com/fixer-mcp/control-plane/internal/state"
)

// Manager owns detached terminal sessions. tmux is a carrier, not a second
// operator UI: the console records the work and attaches/detaches it by ID.
type Manager struct {
	store    *state.Store
	tmuxPath string
}

func NewManager(store *state.Store) *Manager {
	path, _ := exec.LookPath("tmux")
	return &Manager{store: store, tmuxPath: path}
}

func (m *Manager) HasCarrier() bool { return m.tmuxPath != "" }

func (m *Manager) Start(spec launch.Spec, command launch.Command) (domain.Session, error) {
	if command.Binary == "" {
		return domain.Session{}, errors.New("launch command is empty")
	}
	now := time.Now()
	id := fmt.Sprintf("%s-%d", shortID(spec.Title), now.UnixNano())
	target := ""
	status := domain.SessionReady
	if m.tmuxPath != "" {
		target = "fixer-" + sanitize(id)
		args := []string{"new-session", "-d", "-s", target, "--", command.Binary}
		args = append(args, command.Args...)
		proc := exec.Command(m.tmuxPath, args...)
		proc.Dir = command.Dir
		proc.Env = command.Env
		if output, err := proc.CombinedOutput(); err != nil {
			return domain.Session{}, fmt.Errorf("start terminal session: %w: %s", err, strings.TrimSpace(string(output)))
		}
		status = domain.SessionRunning
	}
	session := domain.Session{
		ID:          id,
		Title:       firstNonEmpty(spec.Title, "Новая работа"),
		Kind:        spec.Kind,
		ProjectID:   launch.ProjectID(spec.ProjectPath),
		ProjectPath: spec.ProjectPath,
		Host:        spec.Host,
		Provider:    spec.Provider,
		Account:     spec.Account,
		Model:       spec.Model,
		Thinking:    spec.Thinking,
		Command:     append([]string{command.Binary}, command.Args...),
		TmuxTarget:  target,
		State:       status,
		StartedAt:   now,
		UpdatedAt:   now,
	}
	if err := m.store.UpsertSession(session); err != nil {
		if target != "" {
			_ = m.killTarget(target)
		}
		return domain.Session{}, err
	}
	return session, nil
}

func (m *Manager) Attach(session domain.Session) *runner.ExecCommand {
	if session.TmuxTarget != "" && m.tmuxPath != "" {
		entry := registry.Resolved{
			Entry:  registry.Entry{ID: session.ID, Title: session.Title, Args: []string{"attach-session", "-t", session.TmuxTarget}},
			Binary: m.tmuxPath,
		}
		return runner.NewExecCommand(entry, runner.Options{Env: os.Environ(), Dir: session.ProjectPath})
	}
	if len(session.Command) == 0 {
		return runner.NewExecCommand(registry.Resolved{}, runner.Options{})
	}
	entry := registry.Resolved{
		Entry:  registry.Entry{ID: session.ID, Title: session.Title, Args: append([]string(nil), session.Command[1:]...)},
		Binary: session.Command[0],
	}
	return runner.NewExecCommand(entry, runner.Options{Env: os.Environ(), Dir: session.ProjectPath})
}

func (m *Manager) Stop(session domain.Session) error {
	if session.TmuxTarget == "" || m.tmuxPath == "" {
		return errors.New("session has no detachable terminal carrier")
	}
	if err := m.killTarget(session.TmuxTarget); err != nil {
		return err
	}
	session.State = domain.SessionFinished
	session.UpdatedAt = time.Now()
	return m.store.UpsertSession(session)
}

func (m *Manager) Refresh() ([]domain.Session, error) {
	snapshot := m.store.Snapshot()
	changed := false
	for i := range snapshot.Sessions {
		session := &snapshot.Sessions[i]
		if session.State != domain.SessionRunning && session.State != domain.SessionDetached {
			continue
		}
		if session.TmuxTarget != "" && m.tmuxPath != "" {
			if !m.hasTarget(session.TmuxTarget) {
				session.State = domain.SessionFinished
				session.UpdatedAt = time.Now()
				changed = true
			}
		}
	}
	if changed {
		if err := m.store.Update(func(data *domain.State) error {
			data.Sessions = snapshot.Sessions
			return nil
		}); err != nil {
			return nil, err
		}
	}
	return snapshot.Sessions, nil
}

func (m *Manager) hasTarget(target string) bool {
	if m.tmuxPath == "" {
		return false
	}
	return exec.Command(m.tmuxPath, "has-session", "-t", target).Run() == nil
}

func (m *Manager) killTarget(target string) error {
	if !m.hasTarget(target) {
		return nil
	}
	if output, err := exec.Command(m.tmuxPath, "kill-session", "-t", target).CombinedOutput(); err != nil {
		return fmt.Errorf("stop session: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func shortID(title string) string {
	value := sanitize(title)
	if value == "" {
		return "work"
	}
	if len(value) > 18 {
		return value[:18]
	}
	return value
}

func sanitize(value string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(value) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else if r == ' ' {
			b.WriteRune('-')
		}
	}
	return b.String()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
