package dashboardapi

type HealthResponse struct {
	Status            string `json:"status"`
	DatabasePath      string `json:"database_path"`
	CurrentProjectCWD string `json:"current_project_cwd,omitempty"`
}

type ProjectBinding struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	CWD  string `json:"cwd"`
}

type StatusCounts struct {
	Pending    int `json:"pending"`
	InProgress int `json:"in_progress"`
	Review     int `json:"review"`
	Completed  int `json:"completed"`
	Other      int `json:"other"`
	Total      int `json:"total"`
}

type WorkerProcess struct {
	ID          int    `json:"id"`
	SessionID   int    `json:"session_id"`
	LocalID     int    `json:"local_id,omitempty"`
	PID         int    `json:"pid"`
	LaunchEpoch int    `json:"launch_epoch"`
	Status      string `json:"status"`
	StartedAt   string `json:"started_at"`
	UpdatedAt   string `json:"updated_at"`
	StoppedAt   string `json:"stopped_at,omitempty"`
	Alive       bool   `json:"alive"`
	StopReason  string `json:"stop_reason,omitempty"`
}

type WorkerStateSummary struct {
	RunningCount int             `json:"running_count"`
	HasRunning   bool            `json:"has_running"`
	Processes    []WorkerProcess `json:"processes"`
}

type AutonomousSummary struct {
	ProjectsWithStatus int `json:"projects_with_status"`
	RunningProjects    int `json:"running_projects"`
	BlockedProjects    int `json:"blocked_projects"`
	FrozenProjects     int `json:"frozen_projects"`
	AwaitingReview     int `json:"awaiting_review_projects"`
}

type AutonomousStatus struct {
	ProjectID                        int    `json:"project_id"`
	SessionID                        int    `json:"session_id,omitempty"`
	LocalSessionID                   int    `json:"local_session_id,omitempty"`
	State                            string `json:"state"`
	Summary                          string `json:"summary"`
	Focus                            string `json:"focus,omitempty"`
	Blocker                          string `json:"blocker,omitempty"`
	Evidence                         string `json:"evidence,omitempty"`
	OrchestrationEpoch               int    `json:"orchestration_epoch"`
	OrchestrationFrozen              bool   `json:"orchestration_frozen"`
	NotificationsEnabledForActiveRun bool   `json:"notifications_enabled_for_active_run"`
	UpdatedAt                        string `json:"updated_at,omitempty"`
}

type ActiveWorkerSummary struct {
	ProjectID      int                `json:"project_id"`
	ProjectName    string             `json:"project_name"`
	SessionID      int                `json:"session_id"`
	LocalSessionID int                `json:"local_session_id"`
	Headline       string             `json:"headline"`
	WorkerState    WorkerStateSummary `json:"worker_state"`
}

type ProjectCard struct {
	Project              ProjectBinding    `json:"project"`
	Counts               StatusCounts      `json:"counts"`
	LatestActivityLabel  string            `json:"latest_activity_label"`
	LatestSessionID      int               `json:"latest_session_id,omitempty"`
	LatestLocalSessionID int               `json:"latest_local_session_id,omitempty"`
	Autonomous           *AutonomousStatus `json:"autonomous,omitempty"`
	HasPendingReview     bool              `json:"has_pending_review"`
	HasActiveWorkers     bool              `json:"has_active_workers"`
}

type HomeSnapshotResponse struct {
	CurrentProject     *ProjectBinding       `json:"current_project,omitempty"`
	DefaultChatBinding FixerChatBinding      `json:"default_chat_binding"`
	GlobalCounts       StatusCounts          `json:"global_counts"`
	Projects           []ProjectCard         `json:"projects"`
	ActiveWorkers      []ActiveWorkerSummary `json:"active_workers"`
	AutonomousSummary  AutonomousSummary     `json:"autonomous_summary"`
}

type ProjectHeader struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	CWD  string `json:"cwd"`
}

type DocsSummary struct {
	TotalDocs                      int        `json:"total_docs"`
	Groups                         []DocGroup `json:"groups"`
	PendingProposalCount           int        `json:"pending_proposal_count"`
	TargetedPendingProposalCount   int        `json:"targeted_pending_proposal_count"`
	UntargetedPendingProposalCount int        `json:"untargeted_pending_proposal_count"`
}

type OverviewMetrics struct {
	Counts               StatusCounts       `json:"counts"`
	AttachedDocCount     int                `json:"attached_doc_count"`
	PendingProposalCount int                `json:"pending_proposal_count"`
	WorkerState          WorkerStateSummary `json:"worker_state"`
}

type ProjectSnapshotResponse struct {
	Project    ProjectHeader      `json:"project"`
	Metrics    OverviewMetrics    `json:"metrics"`
	Autonomous *AutonomousStatus  `json:"autonomous,omitempty"`
	Docs       DocsSummary        `json:"docs"`
	Netrunners []NetrunnerSummary `json:"netrunners"`
	FixerChat  FixerChatBinding   `json:"fixer_chat"`
}

type DocSummary struct {
	ID                       int    `json:"id"`
	Title                    string `json:"title"`
	DocType                  string `json:"doc_type"`
	ContentPreview           string `json:"content_preview"`
	TargetedPendingProposals int    `json:"targeted_pending_proposals"`
}

type DocGroup struct {
	DocType                string       `json:"doc_type"`
	Docs                   []DocSummary `json:"docs"`
	PendingProposalCount   int          `json:"pending_proposal_count"`
	TargetedPendingCount   int          `json:"targeted_pending_count"`
	UntargetedPendingCount int          `json:"untargeted_pending_count"`
}

type ProjectDocsResponse struct {
	Project ProjectHeader `json:"project"`
	Docs    DocsSummary   `json:"docs"`
}

type MCPServerAssignment struct {
	ID               int    `json:"id"`
	Name             string `json:"name"`
	ShortDescription string `json:"short_description"`
	Category         string `json:"category"`
	HowTo            string `json:"how_to"`
}

type AttachedDoc struct {
	ID      int    `json:"id"`
	Title   string `json:"title"`
	DocType string `json:"doc_type"`
	Summary string `json:"summary"`
}

type NetrunnerSummary struct {
	ID                    int                `json:"id"`
	LocalID               int                `json:"local_id"`
	ProjectID             int                `json:"project_id"`
	Headline              string             `json:"headline"`
	TaskPreview           string             `json:"task_preview"`
	Status                string             `json:"status"`
	Backend               string             `json:"backend"`
	Model                 string             `json:"model,omitempty"`
	Reasoning             string             `json:"reasoning,omitempty"`
	WriteScope            []string           `json:"write_scope"`
	AttachedDocCount      int                `json:"attached_doc_count"`
	MCPCount              int                `json:"mcp_count"`
	ProposalCount         int                `json:"proposal_count"`
	PendingProposalCount  int                `json:"pending_proposal_count"`
	WorkerState           WorkerStateSummary `json:"worker_state"`
	ReworkCount           int                `json:"rework_count"`
	ForcedStopCount       int                `json:"forced_stop_count"`
	RepairSourceSessionID int                `json:"repair_source_session_id,omitempty"`
	LocalRepairSourceID   int                `json:"local_repair_source_id,omitempty"`
}

type ProjectNetrunnersResponse struct {
	Project  ProjectHeader      `json:"project"`
	Statuses []string           `json:"statuses,omitempty"`
	Sessions []NetrunnerSummary `json:"sessions"`
}

type FinalReport struct {
	FilesChanged  []string            `json:"files_changed"`
	CommandsRun   []string            `json:"commands_run"`
	ChecksRun     []string            `json:"checks_run"`
	Blockers      []string            `json:"blockers"`
	ResidualRisks []string            `json:"residual_risks,omitempty"`
	CleanupClaims map[string][]string `json:"cleanup_claims,omitempty"`
}

type DocProposalSummary struct {
	ID                 int    `json:"id"`
	LocalID            int    `json:"local_id"`
	Status             string `json:"status"`
	ProposedDocType    string `json:"proposed_doc_type"`
	ProposedContent    string `json:"proposed_content"`
	TargetProjectDocID int    `json:"target_project_doc_id,omitempty"`
}

type SessionDetail struct {
	ID                    int                   `json:"id"`
	LocalID               int                   `json:"local_id"`
	ProjectID             int                   `json:"project_id"`
	TaskDescription       string                `json:"task_description"`
	Status                string                `json:"status"`
	Backend               string                `json:"backend"`
	Model                 string                `json:"model,omitempty"`
	Reasoning             string                `json:"reasoning,omitempty"`
	WriteScope            []string              `json:"write_scope"`
	ReportRaw             string                `json:"report_raw"`
	StructuredFinalReport *FinalReport          `json:"structured_final_report,omitempty"`
	AttachedDocs          []AttachedDoc         `json:"attached_docs"`
	MCPServers            []MCPServerAssignment `json:"mcp_servers"`
	Proposals             []DocProposalSummary  `json:"proposals"`
	WorkerState           WorkerStateSummary    `json:"worker_state"`
	ReworkCount           int                   `json:"rework_count"`
	ForcedStopCount       int                   `json:"forced_stop_count"`
	RepairSourceSessionID int                   `json:"repair_source_session_id,omitempty"`
	LocalRepairSourceID   int                   `json:"local_repair_source_id,omitempty"`
	AvailableDocs         []AttachedDoc         `json:"available_docs"`
	AvailableMCPServers   []MCPServerAssignment `json:"available_mcp_servers"`
	AllowedStatusTargets  []string              `json:"allowed_status_targets"`
	StatusActionNote      string                `json:"status_action_note,omitempty"`
}

type NetrunnerDetailResponse struct {
	Session SessionDetail `json:"session"`
}

type CreateTaskInput struct {
	TaskDescription    string   `json:"task_description"`
	DeclaredWriteScope []string `json:"declared_write_scope,omitempty"`
}

type CreateTaskResponse struct {
	Status    string                  `json:"status"`
	SessionID int                     `json:"session_id"`
	Project   ProjectSnapshotResponse `json:"project"`
}

type SetSessionAttachedDocsInput struct {
	ProjectDocIDs []int `json:"project_doc_ids"`
}

type SetSessionMCPServersInput struct {
	MCPServerNames []string `json:"mcp_server_names"`
}

type SetSessionStatusInput struct {
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

type SetProposalStatusInput struct {
	Status string `json:"status"`
}

type SessionActionResponse struct {
	Status  string                  `json:"status"`
	Message string                  `json:"message,omitempty"`
	Session NetrunnerDetailResponse `json:"session"`
}

type FixerChatSessionSummary struct {
	ID             int    `json:"id"`
	LocalID        int    `json:"local_id"`
	ExternalID     string `json:"external_id,omitempty"`
	CodexSessionID string `json:"codex_session_id,omitempty"`
	Headline       string `json:"headline"`
	Status         string `json:"status"`
	AgentRole      string `json:"agent_role,omitempty"`
	Backend        string `json:"backend,omitempty"`
	Model          string `json:"model,omitempty"`
	Reasoning      string `json:"reasoning,omitempty"`
	LastActivityAt string `json:"last_activity_at,omitempty"`
	BindingSource  string `json:"binding_source,omitempty"`
	SessionLogPath string `json:"session_log_path,omitempty"`
	SessionLog     bool   `json:"session_log,omitempty"`
	Transcript     bool   `json:"transcript_available"`
}

type FixerChatBinding struct {
	ProjectID              int                       `json:"project_id,omitempty"`
	Supported              bool                      `json:"supported"`
	DefaultSession         *FixerChatSessionSummary  `json:"default_session,omitempty"`
	Sessions               []FixerChatSessionSummary `json:"sessions"`
	TranscriptAvailability string                    `json:"transcript_availability,omitempty"`
	ResidualRisk           string                    `json:"residual_risk,omitempty"`
}
