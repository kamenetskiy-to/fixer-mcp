package domain

import (
	"encoding/json"
	"time"
)

// Screen is one of the three stable operator spaces.
type Screen string

const (
	ScreenWork      Screen = "work"
	ScreenResources Screen = "resources"
	ScreenMachines  Screen = "machines"
)

// SessionState describes a local terminal carrier independently from the
// canonical MCP session lifecycle.
type SessionState string

const (
	SessionReady    SessionState = "ready"
	SessionRunning  SessionState = "running"
	SessionDetached SessionState = "detached"
	SessionFinished SessionState = "finished"
	SessionFailed   SessionState = "failed"
)

// Project is a local UI bookmark, not a canonical project record. The MCP
// resolves project identity and work state from the selected path.
type Project struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Path       string    `json:"path"`
	Host       string    `json:"host,omitempty"`
	LastOpened time.Time `json:"last_opened,omitempty"`
	Pinned     bool      `json:"pinned,omitempty"`
}

// LaunchDraft is a local, editable launch-card draft. It stores choices, never
// credentials, project records, or provider secrets.
type LaunchDraft struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Kind        string `json:"kind"`
	Provider    string `json:"provider,omitempty"`
	Account     string `json:"account,omitempty"`
	Model       string `json:"model,omitempty"`
	Thinking    string `json:"thinking,omitempty"`
	ProjectID   string `json:"project_id,omitempty"`
	Host        string `json:"host,omitempty"`
	Permissions string `json:"permissions,omitempty"`
	MCPMode     string `json:"mcp_mode,omitempty"`
}

// LocalContext contains only enough terminal-carrier metadata to reconnect to
// a locally started tmux process. It is never presented as canonical project
// work; that comes from Fixer MCP.
type LocalContext struct {
	ID          string       `json:"id"`
	Title       string       `json:"title"`
	Kind        string       `json:"kind"`
	ProjectID   string       `json:"project_id,omitempty"`
	ProjectPath string       `json:"project_path,omitempty"`
	Host        string       `json:"host,omitempty"`
	Provider    string       `json:"provider,omitempty"`
	Account     string       `json:"account,omitempty"`
	Model       string       `json:"model,omitempty"`
	Thinking    string       `json:"thinking,omitempty"`
	Command     []string     `json:"command,omitempty"`
	TmuxTarget  string       `json:"tmux_target,omitempty"`
	State       SessionState `json:"state"`
	ExitCode    int          `json:"exit_code,omitempty"`
	StartedAt   time.Time    `json:"started_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
}

// State is only a private UI cache: recent paths, launch-card drafts, and
// local terminal-carrier metadata. It is explicitly not a project/session DB.
type State struct {
	Version        int            `json:"version"`
	LastProjectID  string         `json:"last_project_id,omitempty"`
	RecentProjects []Project      `json:"recent_projects,omitempty"`
	LaunchDrafts   []LaunchDraft  `json:"launch_drafts,omitempty"`
	LocalContexts  []LocalContext `json:"local_contexts,omitempty"`
	SelectedDraft  string         `json:"selected_draft,omitempty"`
}

func (s *State) Normalize() {
	if s.Version < 2 {
		s.Version = 2
	}
	if s.RecentProjects == nil {
		s.RecentProjects = []Project{}
	}
	if s.LaunchDrafts == nil {
		s.LaunchDrafts = []LaunchDraft{}
	}
	if s.LocalContexts == nil {
		s.LocalContexts = []LocalContext{}
	}
}

// UnmarshalJSON migrates the old console.json shape in place. The old names
// implied project/session ownership; retaining only this read migration keeps
// existing tmux contexts reconnectable while all new writes use cache-only
// names.
func (s *State) UnmarshalJSON(data []byte) error {
	type stateWire struct {
		Version        int            `json:"version"`
		LastProjectID  string         `json:"last_project_id"`
		RecentProjects []Project      `json:"recent_projects"`
		LaunchDrafts   []LaunchDraft  `json:"launch_drafts"`
		LocalContexts  []LocalContext `json:"local_contexts"`
		SelectedDraft  string         `json:"selected_draft"`
		// Legacy keys written by console versions before the MCP work view.
		Projects        []Project      `json:"projects"`
		Profiles        []LaunchDraft  `json:"profiles"`
		Sessions        []LocalContext `json:"sessions"`
		SelectedProfile string         `json:"selected_profile"`
	}
	var raw stateWire
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	s.Version = raw.Version
	s.LastProjectID = raw.LastProjectID
	s.RecentProjects = raw.RecentProjects
	if s.RecentProjects == nil {
		s.RecentProjects = raw.Projects
	}
	s.LaunchDrafts = raw.LaunchDrafts
	if s.LaunchDrafts == nil {
		s.LaunchDrafts = raw.Profiles
	}
	s.LocalContexts = raw.LocalContexts
	if s.LocalContexts == nil {
		s.LocalContexts = raw.Sessions
	}
	s.SelectedDraft = raw.SelectedDraft
	if s.SelectedDraft == "" {
		s.SelectedDraft = raw.SelectedProfile
	}
	s.Normalize()
	return nil
}

func (s *State) RecentProjectByID(id string) *Project {
	for i := range s.RecentProjects {
		if s.RecentProjects[i].ID == id {
			return &s.RecentProjects[i]
		}
	}
	return nil
}

func (s *State) LocalContextByID(id string) *LocalContext {
	for i := range s.LocalContexts {
		if s.LocalContexts[i].ID == id {
			return &s.LocalContexts[i]
		}
	}
	return nil
}
