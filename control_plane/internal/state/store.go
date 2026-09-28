package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fixer-mcp/control-plane/internal/domain"
)

// Store persists only local console state. It is intentionally not the shared
// fixer_mcp SQLite database.
type Store struct {
	mu   sync.Mutex
	path string
	data domain.State
}

func DefaultPath(env map[string]string) string {
	root := strings.TrimSpace(env["XDG_STATE_HOME"])
	if root == "" {
		home := strings.TrimSpace(env["HOME"])
		if home == "" {
			if h, err := os.UserHomeDir(); err == nil {
				home = h
			}
		}
		if home != "" {
			root = filepath.Join(home, ".local", "state")
		}
	}
	if root == "" {
		return ""
	}
	return filepath.Join(root, "fixer", "console.json")
}

func Load(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("console state path is empty")
	}
	s := &Store{path: filepath.Clean(path)}
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			s.data.Normalize()
			return s, nil
		}
		return nil, fmt.Errorf("read console state %s: %w", s.path, err)
	}
	if len(data) > 16*1024*1024 {
		return nil, fmt.Errorf("console state %s is too large", s.path)
	}
	if err := json.Unmarshal(data, &s.data); err != nil {
		return nil, fmt.Errorf("parse console state %s: %w", s.path, err)
	}
	s.data.Normalize()
	return s, nil
}

func (s *Store) Path() string { return s.path }

func (s *Store) Snapshot() domain.State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return clone(s.data)
}

func (s *Store) Update(fn func(*domain.State) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := fn(&s.data); err != nil {
		return err
	}
	s.data.Normalize()
	return s.saveLocked()
}

func (s *Store) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked()
}

func (s *Store) saveLocked() error {
	if s.path == "" {
		return errors.New("console state path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("create console state directory: %w", err)
	}
	payload, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return fmt.Errorf("encode console state: %w", err)
	}
	payload = append(payload, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".console.json.*.tmp")
	if err != nil {
		return fmt.Errorf("create console state temp: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod console state temp: %w", err)
	}
	if _, err := tmp.Write(payload); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write console state: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync console state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close console state: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("replace console state: %w", err)
	}
	return nil
}

// RememberProject stores only a recent local path bookmark. Canonical project
// identity and activity are read through MCP, never written to console.json.
func (s *Store) RememberProject(p domain.Project) error {
	return s.Update(func(data *domain.State) error {
		p.LastOpened = time.Now()
		for i := range data.RecentProjects {
			if data.RecentProjects[i].ID == p.ID {
				data.RecentProjects[i] = p
				data.LastProjectID = p.ID
				return nil
			}
		}
		data.RecentProjects = append([]domain.Project{p}, data.RecentProjects...)
		data.LastProjectID = p.ID
		return nil
	})
}

func (s *Store) SaveLaunchDraft(draft domain.LaunchDraft) error {
	return s.Update(func(data *domain.State) error {
		for i := range data.LaunchDrafts {
			if data.LaunchDrafts[i].ID == draft.ID {
				data.LaunchDrafts[i] = draft
				return nil
			}
		}
		data.LaunchDrafts = append([]domain.LaunchDraft{draft}, data.LaunchDrafts...)
		return nil
	})
}

func (s *Store) UpsertLocalContext(context domain.LocalContext) error {
	return s.Update(func(data *domain.State) error {
		for i := range data.LocalContexts {
			if data.LocalContexts[i].ID == context.ID {
				data.LocalContexts[i] = context
				return nil
			}
		}
		data.LocalContexts = append([]domain.LocalContext{context}, data.LocalContexts...)
		return nil
	})
}

func (s *Store) RemoveLocalContext(id string) error {
	return s.Update(func(data *domain.State) error {
		out := data.LocalContexts[:0]
		for _, item := range data.LocalContexts {
			if item.ID != id {
				out = append(out, item)
			}
		}
		data.LocalContexts = out
		return nil
	})
}

func clone(src domain.State) domain.State {
	dst := src
	dst.RecentProjects = append([]domain.Project(nil), src.RecentProjects...)
	dst.LaunchDrafts = append([]domain.LaunchDraft(nil), src.LaunchDrafts...)
	dst.LocalContexts = make([]domain.LocalContext, len(src.LocalContexts))
	for i, context := range src.LocalContexts {
		dst.LocalContexts[i] = context
		dst.LocalContexts[i].Command = append([]string(nil), context.Command...)
	}
	return dst
}
