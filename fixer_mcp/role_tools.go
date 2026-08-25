package main

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	launchNetrunnerWaveToolName   = "launch_netrunner_wave"
	waitForNetrunnerWaveToolName  = "wait_for_netrunner_wave"
	launchNetrunnerWavesToolName  = "launch_netrunner_waves"
	waitForNetrunnerWavesToolName = "wait_for_netrunner_waves"
)

var bootstrapToolNames = []string{"assume_role"}

var netrunnerGateToolNames = []string{
	waitForNetrunnerWaveToolName,
	waitForNetrunnerWavesToolName,
}

var overseerToolNames = []string{
	"get_projects",
	"register_project",
	"get_all_sessions",
	"add_backlog_item",
	"get_backlog_items",
	"update_backlog_item",
	"set_session_status",
	"set_autonomous_run_status",
	"get_autonomous_run_status",
	"send_operator_telegram_notification",
	"set_project_activity",
	"set_project_overview",
	"get_project_overview",
	"get_active_project_overviews",
	"append_overseer_fixer_message",
	"get_overseer_fixer_messages",
	"set_overseer_fixer_run_state",
	"get_overseer_fixer_run_state",
	"wait_for_overseer_fixer_messages",
	"launch_and_wait_fixers",
	"set_project_handoff",
	"get_project_handoff",
	"export_project_context_package",
	"import_project_context_package",
	"get_project_balance",
	"credit_project_balance",
	"set_fixer_spend_authority",
	"get_balance_ledger",
	"get_session",
	"get_netrunner_transcript_path",
	"submit_fixer_mcp_feedback",
	"list_fixer_mcp_feedback",
	"get_hands_state",
	"list_hands_instructions",
	"get_hands_instruction",
}

var fixerToolNames = []string{
	"get_pending_tasks",
	"add_backlog_item",
	"get_backlog_items",
	"update_backlog_item",
	"list_mcp_servers",
	"set_project_mcp_servers",
	"get_project_mcp_servers",
	"set_session_mcp_servers",
	"get_session_mcp_servers",
	"check_current_project_docs",
	"set_session_attached_docs",
	"get_session_attached_docs",
	"get_attached_project_docs",
	"create_task",
	"review_doc_proposals",
	"set_doc_proposal_status",
	"view_netrunner_logs",
	"get_project_docs",
	"add_project_doc",
	"update_project_doc",
	"delete_project_doc",
	"update_task",
	"set_session_status",
	"fork_repair_session_from",
	"verify_session_cleanup_claims",
	"set_autonomous_run_status",
	"get_autonomous_run_status",
	"send_operator_telegram_notification",
	"set_project_activity",
	"set_project_overview",
	"get_project_overview",
	"append_overseer_fixer_message",
	"get_overseer_fixer_messages",
	"set_overseer_fixer_run_state",
	"get_overseer_fixer_run_state",
	"set_project_handoff",
	"get_project_handoff",
	"export_project_context_package",
	"import_project_context_package",
	"get_project_balance",
	"record_fixer_spend",
	"get_balance_ledger",
	"get_session",
	"list_project_sessions",
	"get_netrunner_transcript_path",
	"create_planned_netrunner_wave",
	"get_planned_netrunner_wave",
	"initialize_planned_netrunner_wave",
	"create_netrunner_wave",
	"get_netrunner_wave",
	"list_netrunner_waves",
	launchNetrunnerWaveToolName,
	waitForNetrunnerWaveToolName,
	launchNetrunnerWavesToolName,
	waitForNetrunnerWavesToolName,
	"transition_netrunner_wave_phase",
	"authorize_netrunner_wave_repair",
	"set_netrunner_wave_control_state",
	"get_mcp_binary_restart_state",
	"set_mcp_binary_restart_state",
	"cleanup_netrunner_wave",
	"list_active_worker_processes",
	"stop_active_worker_processes",
	"launch_image_generation_job",
	"wait_for_image_generation_job",
	"copy_image_generation_job_output",
	"submit_fixer_mcp_feedback",
	"request_genui_surface",
	"get_genui_surface",
	"submit_genui_feedback",
	"list_fixer_mcp_feedback",
	"submit_hands_instruction",
	"get_hands_state",
	"list_hands_instructions",
	"get_hands_instruction",
	"wait_hands_instruction",
	"cancel_hands_instruction",
	"review_hands_instruction",
}

var netrunnerToolNames = []string{
	"get_pending_tasks",
	"checkout_task",
	"list_mcp_servers",
	"get_project_mcp_servers",
	"get_session_mcp_servers",
	"get_session_attached_docs",
	"get_attached_project_docs",
	"log_netrunner_progress",
	"propose_doc_update",
	"complete_task",
	"get_autonomous_run_status",
	"send_operator_telegram_notification",
	"get_session",
	"wake_fixer_autonomous",
	"get_genui_surface",
	"get_hands_state",
	"get_hands_instruction",
	"list_hands_instructions",
	"submit_hands_instruction",
}

var adminBackcompatToolNames = []string{
	"sync_mcp_servers",
	"clear_project_handoff",
}

func appendUniqueToolNames(names []string, additions ...[]string) []string {
	seen := make(map[string]struct{}, len(names))
	out := make([]string, 0, len(names))
	for _, name := range names {
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	for _, group := range additions {
		for _, name := range group {
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			out = append(out, name)
		}
	}
	return out
}

func registeredToolNamesForMode(lockedRole string) []string {
	names := appendUniqueToolNames(nil, bootstrapToolNames)
	switch lockedRole {
	case "":
		return appendUniqueToolNames(names, netrunnerToolNames, fixerToolNames, overseerToolNames, adminBackcompatToolNames)
	case "overseer":
		return appendUniqueToolNames(names, overseerToolNames)
	case "fixer":
		return appendUniqueToolNames(names, fixerToolNames)
	case "netrunner":
		return appendUniqueToolNames(names, netrunnerToolNames)
	default:
		return names
	}
}

func addMcpTool[In, Out any](server *mcp.Server, name string, description string, handler mcp.ToolHandlerFor[In, Out]) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        name,
		Description: description,
	}, handler)
}

func registerBootstrapTools(server *mcp.Server) {
	addMcpTool(server, "assume_role", "Authenticate your MCP stdio session. Must be called first. Role can be 'fixer', 'netrunner', or 'overseer'. Provide cwd for fixer/netrunner and token for fixer/overseer.", AssumeRole)
}

func registerLaunchNetrunnerWaveTool(server *mcp.Server) {
	addMcpTool(server, launchNetrunnerWaveToolName, "Launch all workers for one created parallel Netrunner wave using isolated Git worktrees. Requires fixer role.", LaunchNetrunnerWave)
}

func registerWaitForNetrunnerWaveTool(server *mcp.Server) {
	addMcpTool(server, waitForNetrunnerWaveToolName, "Wait for a launched parallel Netrunner wave to yield a review-ready or terminal worker and capture worktree diff artifacts. Requires fixer role.", WaitForNetrunnerWave)
}

func registerLaunchNetrunnerWavesTool(server *mcp.Server) {
	addMcpTool(server, launchNetrunnerWavesToolName, "Launch multiple existing waves in the current Fixer project with prevalidated IDs and per-wave failure isolation. Returns compact operator summaries by default; set detail_level=full for the legacy full per-wave outputs.", LaunchNetrunnerWaves)
}

func registerWaitForNetrunnerWavesTool(server *mcp.Server) {
	addMcpTool(server, waitForNetrunnerWavesToolName, "Wait on multiple existing waves in the current Fixer project and return isolated compact lifecycle/terminality summaries. Set detail_level=full for the legacy full per-wave outputs.", WaitForNetrunnerWaves)
}

func gateWaitForNetrunnerWave(ctx context.Context, req *mcp.CallToolRequest, input WaitForNetrunnerWaveInput) (*mcp.CallToolResult, WaitForNetrunnerWaveOutput, error) {
	if err := ensureNetrunnerGateProjectBinding(); err != nil {
		return &mcp.CallToolResult{IsError: true}, WaitForNetrunnerWaveOutput{}, err
	}
	return WaitForNetrunnerWave(ctx, req, input)
}

func gateWaitForNetrunnerWaves(ctx context.Context, req *mcp.CallToolRequest, input WaitForNetrunnerWavesInput) (*mcp.CallToolResult, WaitForNetrunnerWavesOutput, error) {
	if err := ensureNetrunnerGateProjectBinding(); err != nil {
		return &mcp.CallToolResult{IsError: true}, WaitForNetrunnerWavesOutput{}, err
	}
	return WaitForNetrunnerWaves(ctx, req, input)
}

func registerNetrunnerGateTools(server *mcp.Server) {
	addMcpTool(server, waitForNetrunnerWaveToolName, "Wait for a launched parallel Netrunner wave to yield a review-ready or terminal worker and capture worktree diff artifacts. Wait-only gate channel; binds to the project from env when startup auth was missed.", gateWaitForNetrunnerWave)
	addMcpTool(server, waitForNetrunnerWavesToolName, "Wait on multiple existing waves in the current Fixer project and return isolated compact lifecycle/terminality summaries. Wait-only gate channel; binds to the project from env when startup auth was missed.", gateWaitForNetrunnerWaves)
}

func registerOverseerTools(server *mcp.Server) {
	addMcpTool(server, "get_projects", "List all projects. Requires overseer role.", GetProjects)
	addMcpTool(server, "register_project", "Register a project cwd globally. Requires overseer role.", RegisterProject)
	addMcpTool(server, "get_all_sessions", "List all active tasks/sessions across all projects. Requires overseer role.", GetAllSessions)
	addMcpTool(server, "add_backlog_item", "Add a project-scoped backlog item. Fixer uses the bound project; overseer must provide project_id.", AddBacklogItem)
	addMcpTool(server, "get_backlog_items", "List project-scoped backlog items. Fixer uses the bound project; overseer must provide project_id.", GetBacklogItems)
	addMcpTool(server, "update_backlog_item", "Update a project-scoped backlog item. Fixer uses the bound project; overseer must provide project_id.", UpdateBacklogItem)
	addMcpTool(server, "set_session_status", "Set session lifecycle status. Fixer can only modify sessions in bound project; overseer can modify any session.", SetSessionStatus)
	addMcpTool(server, "set_autonomous_run_status", "Set the current autonomous-run status for a project. Fixer can update the bound project; overseer can update any project.", SetAutonomousRunStatus)
	addMcpTool(server, "get_autonomous_run_status", "Read the current autonomous-run status for a project. Fixer/netrunner read their bound project; overseer can read any project.", GetAutonomousRunStatus)
	addMcpTool(server, "send_operator_telegram_notification", "Send a compact Russian operator notification through Fixer MCP's native Telegram path. Requires configured FIXER_MCP_TELEGRAM_* env vars.", SendOperatorTelegramNotification)
	addMcpTool(server, "set_project_activity", "Set a project active/passive flag. Fixer updates the bound project; overseer may target any project via project_id.", SetProjectActivity)
	addMcpTool(server, "set_project_overview", "Write or replace a compact per-project overview. Fixer writes for the bound project; overseer may target any project via project_id.", SetProjectOverview)
	addMcpTool(server, "get_project_overview", "Read the compact per-project overview. Fixer reads the bound project; overseer may target any project via project_id.", GetProjectOverview)
	addMcpTool(server, "get_active_project_overviews", "List active projects with compact overview, latest handoff, and latest sessions. Requires overseer role.", GetActiveProjectOverviews)
	addMcpTool(server, "append_overseer_fixer_message", "Append a durable project-scoped Overseer/Fixer message. Overseer must pass project_id; fixer uses the bound project. sender_role must match the authenticated role.", AppendOverseerFixerMessage)
	addMcpTool(server, "get_overseer_fixer_messages", "Read the latest durable project-scoped Overseer/Fixer messages in chronological order. Overseer must pass project_id; fixer uses the bound project.", GetOverseerFixerMessages)
	addMcpTool(server, "set_overseer_fixer_run_state", "Set durable active/inactive state for an Overseer-directed Fixer run. Overseer must pass project_id; fixer uses the bound project.", SetOverseerFixerRunState)
	addMcpTool(server, "get_overseer_fixer_run_state", "Read durable active/inactive state for an Overseer-directed Fixer run. Overseer must pass project_id; fixer uses the bound project.", GetOverseerFixerRunState)
	addMcpTool(server, "wait_for_overseer_fixer_messages", "Overseer-only: wait briefly for new fixer messages after a cursor across explicit project_ids or all active fixer-run projects.", WaitForOverseerFixerMessages)
	addMcpTool(server, "launch_and_wait_fixers", "Overseer-only: launch/resume latest Fixer sessions for target projects and wait for the first new Fixer chat response.", LaunchAndWaitFixers)
	addMcpTool(server, "set_project_handoff", "Write or replace the current project handoff. Fixer writes for the bound project; overseer may target any project via project_id.", SetProjectHandoff)
	addMcpTool(server, "get_project_handoff", "Read the current project handoff. Fixer reads the bound project; overseer may target any project via project_id.", GetProjectHandoff)
	addMcpTool(server, "export_project_context_package", "Export the project context (overview, handoff, doc tree, backlog, lightweight session index) into a portable JSON package file. Fixer exports the bound project; overseer must pass project_id.", ExportProjectContextPackage)
	addMcpTool(server, "import_project_context_package", "Import a portable project-context JSON package into the project: overview, handoff, doc tree (parent links via slug/path), backlog (deduped by title). Refuses non-empty projects unless force=true. Fixer imports into the bound project; overseer must pass project_id.", ImportProjectContextPackage)
	addMcpTool(server, "get_project_balance", "Read a project's abstract balance and Fixer spend authority. Overseer must pass project_id.", GetProjectBalance)
	addMcpTool(server, "credit_project_balance", "Credit a project's abstract balance and write an audit ledger entry. Requires overseer role.", CreditProjectBalance)
	addMcpTool(server, "set_fixer_spend_authority", "Set a project's Fixer spend authority and write an audit ledger entry. Requires overseer role.", SetFixerSpendAuthority)
	addMcpTool(server, "get_balance_ledger", "Read recent project balance ledger rows. Overseer must pass project_id.", GetBalanceLedger)
	addMcpTool(server, "get_session", "Read one session by ID. Fixer/netrunner are project-scoped, overseer can read any session.", GetSession)
	addMcpTool(server, "get_netrunner_transcript_path", "Resolve local transcript path metadata for a project-scoped Netrunner session without reading transcript content. Fixer uses bound project; overseer must pass project_id.", GetNetrunnerTranscriptPath)
	addMcpTool(server, "submit_fixer_mcp_feedback", "Submit cross-project feedback to Fixer MCP. Fixer uses bound project; overseer must pass project_id.", SubmitFixerMcpFeedback)
	addMcpTool(server, "list_fixer_mcp_feedback", "List/search read-only cross-project Fixer MCP feedback reports with project/date/type filters, pagination, and full content. Overseer may filter by project_id or search all projects.", ListFixerMcpFeedback)
	addMcpTool(server, "get_hands_state", "Read the permanent Hands identity, provider lanes, queue, active generation, lease summary, and journal head for an explicit project. Requires overseer role.", GetHandsState)
	addMcpTool(server, "list_hands_instructions", "Read a cursor-paginated permanent Hands mailbox for an explicit project. Requires overseer role.", ListHandsInstructions)
	addMcpTool(server, "get_hands_instruction", "Read one permanent Hands instruction with its immutable envelope, audit events, and generations. Requires overseer role.", GetHandsInstruction)
}

func registerFixerTools(server *mcp.Server) {
	addMcpTool(server, "get_pending_tasks", "For fixers and netrunners: Get a list of all pending tasks for the current project.", GetPendingTasks)
	addMcpTool(server, "add_backlog_item", "Add a backlog item to the current project. Requires fixer role.", AddBacklogItem)
	addMcpTool(server, "get_backlog_items", "List backlog items for the current project. Requires fixer role.", GetBacklogItems)
	addMcpTool(server, "update_backlog_item", "Update a backlog item in the current project. Requires fixer role.", UpdateBacklogItem)
	addMcpTool(server, "list_mcp_servers", "List MCP servers from Fixer registry. Requires authenticated role. Returns curated defaults unless include_all=true; archived servers require include_archived=true.", ListMcpServers)
	addMcpTool(server, "set_project_mcp_servers", "Set project-scoped MCP allowlist for the current fixer project.", SetProjectMcpServers)
	addMcpTool(server, "get_project_mcp_servers", "Get project-scoped MCP allowlist for current project.", GetProjectMcpServers)
	addMcpTool(server, "set_session_mcp_servers", "Assign MCP servers to a specific session. Requires fixer role.", SetSessionMcpServers)
	addMcpTool(server, "get_session_mcp_servers", "Get MCP server assignments for a specific session in current project.", GetSessionMcpServers)
	addMcpTool(server, "check_current_project_docs", "List metadata summaries for project docs in current project without returning full content. Requires fixer role.", CheckCurrentProjectDocs)
	addMcpTool(server, "set_session_attached_docs", "Assign project docs to a specific session. Requires fixer role.", SetSessionAttachedDocs)
	addMcpTool(server, "get_session_attached_docs", "Get attached document metadata for a session in current project. Requires fixer or netrunner role.", GetSessionAttachedDocs)
	addMcpTool(server, "get_attached_project_docs", "Get full content for docs attached to a session. Requires fixer or netrunner role.", GetAttachedProjectDocs)
	addMcpTool(server, "create_task", "Allows the 'fixer' role to insert new tasks (sessions) into the database with a status of 'pending', cleanly spawning new Netrunners.", CreateTask)
	addMcpTool(server, "review_doc_proposals", "Review pending document proposals. Requires fixer role.", ReviewDocProposals)
	addMcpTool(server, "set_doc_proposal_status", "Approve or reject a document proposal. Requires fixer role. When approval creates a new doc, optional parent_doc_id, slug, and level place it in the canonical doc tree.", SetDocProposalStatus)
	addMcpTool(server, "view_netrunner_logs", "Read append-only Netrunner progress logs for one project-scoped session in chronological order. Requires fixer role and never mutates canonical docs.", ViewNetrunnerLogs)
	addMcpTool(server, "get_project_docs", "Get all project documents. Requires authenticated role.", GetProjectDocs)
	addMcpTool(server, "add_project_doc", "Create a new canonical project document, optionally positioned in the 0..3 documentation tree. Requires 'fixer' role.", AddProjectDoc)
	addMcpTool(server, "update_project_doc", "Update a specific canonical document's content, type, or tree metadata. Requires 'fixer' role.", UpdateProjectDoc)
	addMcpTool(server, "delete_project_doc", "Delete a specific document. Requires 'fixer' role.", DeleteProjectDoc)
	addMcpTool(server, "update_task", "Append instructions to an existing task. Requires fixer role.", UpdateTask)
	addMcpTool(server, "set_session_status", "Set session lifecycle status. Fixer can only modify sessions in bound project; overseer can modify any session.", SetSessionStatus)
	addMcpTool(server, "fork_repair_session_from", "Create a replacement repair session from an earlier project-scoped session while preserving provenance and attached context. Requires fixer role.", ForkRepairSessionFrom)
	addMcpTool(server, "verify_session_cleanup_claims", "Check structured cleanup/removal claims from a session report against on-disk project state. Requires fixer role.", VerifySessionCleanupClaims)
	addMcpTool(server, "set_autonomous_run_status", "Set the current autonomous-run status for a project. Fixer can update the bound project; overseer can update any project.", SetAutonomousRunStatus)
	addMcpTool(server, "get_autonomous_run_status", "Read the current autonomous-run status for a project. Fixer/netrunner read their bound project; overseer can read any project.", GetAutonomousRunStatus)
	addMcpTool(server, "send_operator_telegram_notification", "Send a compact Russian operator notification through Fixer MCP's native Telegram path. Requires configured FIXER_MCP_TELEGRAM_* env vars.", SendOperatorTelegramNotification)
	addMcpTool(server, "set_project_activity", "Set a project active/passive flag. Fixer updates the bound project; overseer may target any project via project_id.", SetProjectActivity)
	addMcpTool(server, "set_project_overview", "Write or replace a compact per-project overview. Fixer writes for the bound project; overseer may target any project via project_id.", SetProjectOverview)
	addMcpTool(server, "get_project_overview", "Read the compact per-project overview. Fixer reads the bound project; overseer may target any project via project_id.", GetProjectOverview)
	addMcpTool(server, "append_overseer_fixer_message", "Append a durable project-scoped Overseer/Fixer message. Overseer must pass project_id; fixer uses the bound project. sender_role must match the authenticated role.", AppendOverseerFixerMessage)
	addMcpTool(server, "get_overseer_fixer_messages", "Read the latest durable project-scoped Overseer/Fixer messages in chronological order. Overseer must pass project_id; fixer uses the bound project.", GetOverseerFixerMessages)
	addMcpTool(server, "set_overseer_fixer_run_state", "Set durable active/inactive state for an Overseer-directed Fixer run. Overseer must pass project_id; fixer uses the bound project.", SetOverseerFixerRunState)
	addMcpTool(server, "get_overseer_fixer_run_state", "Read durable active/inactive state for an Overseer-directed Fixer run. Overseer must pass project_id; fixer uses the bound project.", GetOverseerFixerRunState)
	addMcpTool(server, "set_project_handoff", "Write or replace the current project handoff. Fixer writes for the bound project; overseer may target any project via project_id.", SetProjectHandoff)
	addMcpTool(server, "get_project_handoff", "Read the current project handoff. Fixer reads the bound project; overseer may target any project via project_id.", GetProjectHandoff)
	addMcpTool(server, "export_project_context_package", "Export the project context (overview, handoff, doc tree, backlog, lightweight session index) into a portable JSON package file. Fixer exports the bound project; overseer must pass project_id.", ExportProjectContextPackage)
	addMcpTool(server, "import_project_context_package", "Import a portable project-context JSON package into the project: overview, handoff, doc tree (parent links via slug/path), backlog (deduped by title). Refuses non-empty projects unless force=true. Fixer imports into the bound project; overseer must pass project_id.", ImportProjectContextPackage)
	addMcpTool(server, "get_project_balance", "Read the current project's abstract balance and Fixer spend authority. Requires fixer role.", GetProjectBalance)
	addMcpTool(server, "record_fixer_spend", "Record a Fixer spend under granted authority, decrementing balance and allowance atomically. Requires fixer role.", RecordFixerSpend)
	addMcpTool(server, "get_balance_ledger", "Read recent balance ledger rows for the current project. Requires fixer role.", GetBalanceLedger)
	addMcpTool(server, "get_session", "Read one session by ID. Fixer/netrunner are project-scoped, overseer can read any session.", GetSession)
	addMcpTool(server, "list_project_sessions", "List/search recent project-scoped Netrunner sessions with compact metadata (status, task summary, backend/model/reasoning, wave linkage, timestamps), bounded and paginated. Use this instead of direct SQLite/CLI archaeology to discover historical sessions. Requires fixer role.", ListProjectSessions)
	addMcpTool(server, "get_netrunner_transcript_path", "Resolve local transcript path metadata for a project-scoped Netrunner session without reading transcript content. Fixer uses bound project; overseer must pass project_id.", GetNetrunnerTranscriptPath)
	addMcpTool(server, "submit_fixer_mcp_feedback", "Submit cross-project feedback to Fixer MCP. Fixer uses bound project; overseer must pass project_id.", SubmitFixerMcpFeedback)
	addMcpTool(server, "list_fixer_mcp_feedback", "List/search read-only Fixer MCP feedback reports. The Fixer MCP project Fixer can read cross-project reports; other Fixers remain scoped to their current project.", ListFixerMcpFeedback)
	addMcpTool(server, "create_planned_netrunner_wave", "Persist future project-scoped wave work, including per-task backend/model/reasoning and project-allowed MCP assignments, without creating sessions, resolving Git, or reserving write scopes. Requires fixer role.", CreatePlannedNetrunnerWave)
	addMcpTool(server, "get_planned_netrunner_wave", "Read one project-scoped planned-wave definition and its future tasks. Requires fixer role.", GetPlannedNetrunnerWave)
	addMcpTool(server, "initialize_planned_netrunner_wave", "Materialize one planned wave into configured pending sessions with their MCP assignments and delegate to governed create_netrunner_wave admission. Requires fixer role.", InitializePlannedNetrunnerWave)
	addMcpTool(server, "create_netrunner_wave", "Create a durable parallel Netrunner wave from pending sessions after strict Git and write-scope admission. Does not launch workers. Requires fixer role.", CreateNetrunnerWave)
	addMcpTool(server, "get_netrunner_wave", "Read one durable parallel Netrunner wave and its worker rows for the current project. Requires fixer role.", GetNetrunnerWave)
	addMcpTool(server, "list_netrunner_waves", "List/search recent project-scoped parallel Netrunner waves with compact metadata (status, phase, worker counts, session IDs, control/failure state, timestamps), bounded and paginated. Use this instead of direct SQLite/CLI archaeology to discover historical waves. Requires fixer role.", ListNetrunnerWaves)
	registerLaunchNetrunnerWaveTool(server)
	registerWaitForNetrunnerWaveTool(server)
	registerLaunchNetrunnerWavesTool(server)
	registerWaitForNetrunnerWavesTool(server)
	addMcpTool(server, "transition_netrunner_wave_phase", "Apply an explicit reviewed implementation-to-acceptance or acceptance-to-completed wave phase transition.", TransitionNetrunnerWavePhase)
	addMcpTool(server, "authorize_netrunner_wave_repair", "Authorize exactly one durable governed implementation repair for the worker selected by wave failure policy.", AuthorizeNetrunnerWaveRepair)
	addMcpTool(server, "set_netrunner_wave_control_state", "Pause one wave for Architect review or resume it after explicit Architect approval without freezing unrelated waves.", SetNetrunnerWaveControlState)
	addMcpTool(server, "get_mcp_binary_restart_state", "Read the project-scoped MCP binary restart/build epoch marker.", GetMCPBinaryRestartState)
	addMcpTool(server, "set_mcp_binary_restart_state", "Mark a project MCP binary build epoch as restart-required or confirm that the required epoch is running.", SetMCPBinaryRestartState)
	addMcpTool(server, "cleanup_netrunner_wave", "Clean up terminal parallel Netrunner wave worktrees with explicit removal/prune flags and safety checks. Requires fixer role.", CleanupNetrunnerWave)
	addMcpTool(server, "list_active_worker_processes", "List currently active Fixer-managed worker processes for the current project, with session mapping and liveness checks. Requires fixer role.", ListActiveWorkerProcesses)
	addMcpTool(server, "stop_active_worker_processes", "Stop active Fixer-managed worker processes for the current project and optionally freeze orchestration follow-up. Requires fixer role.", StopActiveWorkerProcesses)
	addMcpTool(server, "launch_image_generation_job", "Launch a dedicated Codex image-generation or image-editing subprocess for the current project and return a durable job id. Optional local input images can be attached for edit flows. Requires fixer role.", LaunchImageGenerationJob)
	addMcpTool(server, "wait_for_image_generation_job", "Wait for an image-generation job to finish and resolve the generated image path. Requires fixer role.", WaitForImageGenerationJob)
	addMcpTool(server, "copy_image_generation_job_output", "Copy a completed image-generation job output into the current project workspace. Requires fixer role.", CopyImageGenerationJobOutput)
	addMcpTool(server, "request_genui_surface", "Request a registered Project Workroom GenUI surface by type, version, and typed arguments. Unknown or invalid requests fail closed and create durable demand evidence. Requires fixer role.", RequestGenUISurface)
	addMcpTool(server, "get_genui_surface", "Read one immutable project-owned GenUI surface revision. Requires fixer or scoped netrunner role.", GetGenUISurface)
	addMcpTool(server, "submit_genui_feedback", "Record an idempotent thumbs-up or thumbs-down for one immutable GenUI surface revision. Requires fixer role.", SubmitGenUIFeedback)
	addMcpTool(server, "submit_hands_instruction", "Submit an idempotent instruction to the current project's single permanent actor Руки. The actor and compatibility session are resolved internally. Requires fixer or authenticated Hands netrunner role.", SubmitHandsInstruction)
	addMcpTool(server, "get_hands_state", "Read the permanent Hands identity, provider lanes, queue, active generation, lease summary, and journal head for the current project.", GetHandsState)
	addMcpTool(server, "list_hands_instructions", "Read the current project's cursor-paginated permanent Hands mailbox. Requires fixer or authenticated Hands netrunner role.", ListHandsInstructions)
	addMcpTool(server, "get_hands_instruction", "Read one permanent Hands instruction with its immutable envelope, audit events, and generations.", GetHandsInstruction)
	addMcpTool(server, "wait_hands_instruction", "Wait on a durable instruction-event cursor with race-free register-and-requery semantics. Requires fixer role.", WaitHandsInstruction)
	addMcpTool(server, "cancel_hands_instruction", "Apply an idempotent governed cancellation to a queued, waiting, starting, or running Hands instruction. Requires fixer role.", CancelHandsInstruction)
	addMcpTool(server, "review_hands_instruction", "Accept or request changes for a Hands instruction awaiting review. Requires fixer role.", ReviewHandsInstruction)
}

func registerNetrunnerTools(server *mcp.Server) {
	addMcpTool(server, "get_pending_tasks", "For fixers and netrunners: Get a list of all pending tasks for the current project.", GetPendingTasks)
	addMcpTool(server, "checkout_task", "For netrunners: Checkout a specific task by its session ID.", CheckoutTask)
	addMcpTool(server, "list_mcp_servers", "List MCP servers from Fixer registry. Requires authenticated role. Returns curated defaults unless include_all=true; archived servers require include_archived=true.", ListMcpServers)
	addMcpTool(server, "get_project_mcp_servers", "Get project-scoped MCP allowlist for current project.", GetProjectMcpServers)
	addMcpTool(server, "get_session_mcp_servers", "Get MCP server assignments for a specific session in current project.", GetSessionMcpServers)
	addMcpTool(server, "get_session_attached_docs", "Get attached document metadata for a session in current project. Requires fixer or netrunner role.", GetSessionAttachedDocs)
	addMcpTool(server, "get_attached_project_docs", "Get full content for docs attached to a session. Requires fixer or netrunner role.", GetAttachedProjectDocs)
	addMcpTool(server, "log_netrunner_progress", "Append a durable progress log for the checked-out Netrunner session. Requires netrunner role. Timestamp is generated by the backend.", LogNetrunnerProgress)
	addMcpTool(server, "propose_doc_update", "Propose an update to canonical project documentation, not history logs. Requires netrunner role.", ProposeDocUpdate)
	addMcpTool(server, "complete_task", "Complete a task. Requires netrunner role.", CompleteTask)
	addMcpTool(server, "get_autonomous_run_status", "Read the current autonomous-run status for a project. Fixer/netrunner read their bound project; overseer can read any project.", GetAutonomousRunStatus)
	addMcpTool(server, "send_operator_telegram_notification", "Send a compact Russian operator notification through Fixer MCP's native Telegram path. Requires configured FIXER_MCP_TELEGRAM_* env vars.", SendOperatorTelegramNotification)
	addMcpTool(server, "get_session", "Read one session by ID. Fixer/netrunner are project-scoped, overseer can read any session.", GetSession)
	addMcpTool(server, "wake_fixer_autonomous", "Resume the registered autonomous Fixer thread for the current project after a headless Netrunner finishes. Requires netrunner role.", WakeFixerAutonomous)
	addMcpTool(server, "get_genui_surface", "Read one immutable project-owned GenUI surface revision. Requires fixer or scoped netrunner role.", GetGenUISurface)
	addMcpTool(server, "get_hands_state", "Read the permanent Hands identity and current state for the authenticated project. Netrunner access is read-only.", GetHandsState)
	addMcpTool(server, "get_hands_instruction", "Read the permanent Hands instruction assigned to this compatibility Netrunner session, including its immutable envelope and events.", GetHandsInstruction)
	addMcpTool(server, "list_hands_instructions", "Read the current project's cursor-paginated permanent Hands mailbox. Requires authenticated Hands netrunner role.", ListHandsInstructions)
	addMcpTool(server, "submit_hands_instruction", "Submit an idempotent instruction to the current project's single permanent actor Руки. Requires authenticated Hands netrunner role.", SubmitHandsInstruction)
}

func registerAdminBackcompatTools(server *mcp.Server) {
	addMcpTool(server, "sync_mcp_servers", "Upsert MCP registry from explicit list or mcp_config.json. Requires fixer role.", SyncMcpServers)
	addMcpTool(server, "clear_project_handoff", "Delete the current project handoff. Fixer clears the bound project; overseer may target any project via project_id.", ClearProjectHandoff)
}

func registerLegacyTools(server *mcp.Server) {
	registerNetrunnerTools(server)
	registerFixerTools(server)
	registerOverseerTools(server)
	registerAdminBackcompatTools(server)
}

func registerMcpTools(server *mcp.Server, lockedRole string) {
	registerBootstrapTools(server)
	switch lockedRole {
	case "overseer":
		registerOverseerTools(server)
	case "fixer":
		registerFixerTools(server)
	case "netrunner":
		registerNetrunnerTools(server)
	default:
		registerLegacyTools(server)
	}
}
