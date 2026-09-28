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

// Manager owns detached local terminal contexts. tmux is only a carrier; the
// canonical project/session lifecycle stays in Fixer MCP.
type Manager struct {
	store    *state.Store
	tmuxPath string
}

func NewManager(store *state.Store) *Manager {
	path, _ := exec.LookPath("tmux")
	return &Manager{store: store, tmuxPath: path}
}

func (m *Manager) HasCarrier() bool { return m.tmuxPath != "" }

func (m *Manager) Start(spec launch.Spec, command launch.Command) (domain.LocalContext, error) {
	if command.Binary == "" {
		return domain.LocalContext{}, errors.New("launch command is empty")
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
			return domain.LocalContext{}, fmt.Errorf("start local terminal context: %w: %s", err, strings.TrimSpace(string(output)))
		}
		status = domain.SessionRunning
	}
	context := domain.LocalContext{
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
	if err := m.store.UpsertLocalContext(context); err != nil {
		if target != "" {
			_ = m.killTarget(target)
		}
		return domain.LocalContext{}, err
	}
	return context, nil
}

func (m *Manager) Attach(context domain.LocalContext) *runner.ExecCommand {
	if context.TmuxTarget != "" && m.tmuxPath != "" {
		entry := registry.Resolved{
			Entry:  registry.Entry{ID: context.ID, Title: context.Title, Args: []string{"attach-session", "-t", context.TmuxTarget}},
			Binary: m.tmuxPath,
		}
		return runner.NewExecCommand(entry, runner.Options{Env: os.Environ(), Dir: context.ProjectPath})
	}
	if len(context.Command) == 0 {
		return runner.NewExecCommand(registry.Resolved{}, runner.Options{})
	}
	entry := registry.Resolved{
		Entry:  registry.Entry{ID: context.ID, Title: context.Title, Args: append([]string(nil), context.Command[1:]...)},
		Binary: context.Command[0],
	}
	return runner.NewExecCommand(entry, runner.Options{Env: os.Environ(), Dir: context.ProjectPath})
}

func (m *Manager) Stop(context domain.LocalContext) error {
	if context.TmuxTarget == "" || m.tmuxPath == "" {
		return errors.New("session has no detachable terminal carrier")
	}
	if err := m.killTarget(context.TmuxTarget); err != nil {
		return err
	}
	context.State = domain.SessionFinished
	context.UpdatedAt = time.Now()
	return m.store.UpsertLocalContext(context)
}

func (m *Manager) Refresh() ([]domain.LocalContext, error) {
	snapshot := m.store.Snapshot()
	changed := false
	for i := range snapshot.LocalContexts {
		context := &snapshot.LocalContexts[i]
		if context.State != domain.SessionRunning && context.State != domain.SessionDetached {
			continue
		}
		if context.TmuxTarget != "" && m.tmuxPath != "" {
			if !m.hasTarget(context.TmuxTarget) {
				context.State = domain.SessionFinished
				context.UpdatedAt = time.Now()
				changed = true
			}
		}
	}
	if changed {
		if err := m.store.Update(func(data *domain.State) error {
			data.LocalContexts = snapshot.LocalContexts
			return nil
		}); err != nil {
			return nil, err
		}
	}
	return snapshot.LocalContexts, nil
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
