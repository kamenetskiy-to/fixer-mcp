package domain

import "time"

// Screen is one of the three stable operator workspaces.
type Screen string

const (
	ScreenWork      Screen = "work"
	ScreenResources Screen = "resources"
	ScreenMachines  Screen = "machines"
	ScreenNetwork   Screen = "network"
	ScreenFleet     Screen = "fleet"
)

// SessionState describes the lifecycle independently from whether its terminal
// is currently attached to the console.
type SessionState string

const (
	SessionReady    SessionState = "ready"
	SessionRunning  SessionState = "running"
	SessionDetached SessionState = "detached"
	SessionFinished SessionState = "finished"
	SessionFailed   SessionState = "failed"
)

// Project is a user-facing project identity. Path is deliberately local to a
// host; a remote host may use a different path for the same logical project.
type Project struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Path       string    `json:"path"`
	Host       string    `json:"host,omitempty"`
	LastOpened time.Time `json:"last_opened,omitempty"`
	Pinned     bool      `json:"pinned,omitempty"`
}

// LaunchProfile is a saved, editable launch card. It stores choices, never
// credentials or provider secrets.
type LaunchProfile struct {
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

// Session is durable enough to rediscover a detached process after restarting
// the console. Command is a resolved executable plus arguments; Env is not
// persisted because it can contain secrets.
type Session struct {
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

// State is local console state. Project/workroom records remain owned by Fixer
// MCP; this file only remembers the operator's local view and detached shells.
type State struct {
	Version         int             `json:"version"`
	LastProjectID   string          `json:"last_project_id,omitempty"`
	Projects        []Project       `json:"projects,omitempty"`
	Profiles        []LaunchProfile `json:"profiles,omitempty"`
	Sessions        []Session       `json:"sessions,omitempty"`
	SelectedProfile string          `json:"selected_profile,omitempty"`
}

func (s *State) Normalize() {
	if s.Version == 0 {
		s.Version = 1
	}
	if s.Projects == nil {
		s.Projects = []Project{}
	}
	if s.Profiles == nil {
		s.Profiles = []LaunchProfile{}
	}
	if s.Sessions == nil {
		s.Sessions = []Session{}
	}
}

func (s *State) ProjectByID(id string) *Project {
	for i := range s.Projects {
		if s.Projects[i].ID == id {
			return &s.Projects[i]
		}
	}
	return nil
}

func (s *State) SessionByID(id string) *Session {
	for i := range s.Sessions {
		if s.Sessions[i].ID == id {
			return &s.Sessions[i]
		}
	}
	return nil
}
