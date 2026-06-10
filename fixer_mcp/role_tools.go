package main

import "github.com/modelcontextprotocol/go-sdk/mcp"

var bootstrapToolNames = []string{"assume_role"}

var overseerToolNames = []string{
	"get_projects",
	"register_project",
	"get_all_sessions",
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
	"get_session",
	"get_netrunner_transcript_path",
}

var fixerToolNames = []string{
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
	"get_session",
	"get_netrunner_transcript_path",
	"wait_for_netrunner_session",
	"launch_and_wait_netrunner",
	"create_netrunner_wave",
	"get_netrunner_wave",
	"launch_netrunner_wave",
	"wait_for_netrunner_wave",
	"cleanup_netrunner_wave",
	"list_active_worker_processes",
	"stop_active_worker_processes",
	"launch_image_generation_job",
	"wait_for_image_generation_job",
	"copy_image_generation_job_output",
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

func registerOverseerTools(server *mcp.Server) {
	addMcpTool(server, "get_projects", "List all projects. Requires overseer role.", GetProjects)
	addMcpTool(server, "register_project", "Register a project cwd globally. Requires overseer role.", RegisterProject)
	addMcpTool(server, "get_all_sessions", "List all active tasks/sessions across all projects. Requires overseer role.", GetAllSessions)
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
	addMcpTool(server, "get_session", "Read one session by ID. Fixer/netrunner are project-scoped, overseer can read any session.", GetSession)
	addMcpTool(server, "get_netrunner_transcript_path", "Resolve local transcript path metadata for a project-scoped Netrunner session without reading transcript content. Fixer uses bound project; overseer must pass project_id.", GetNetrunnerTranscriptPath)
}

func registerFixerTools(server *mcp.Server) {
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
	addMcpTool(server, "get_session", "Read one session by ID. Fixer/netrunner are project-scoped, overseer can read any session.", GetSession)
	addMcpTool(server, "get_netrunner_transcript_path", "Resolve local transcript path metadata for a project-scoped Netrunner session without reading transcript content. Fixer uses bound project; overseer must pass project_id.", GetNetrunnerTranscriptPath)
	addMcpTool(server, "wait_for_netrunner_session", "Wait for one launched Netrunner session to reach a review-ready or terminal lifecycle state and return structured status/report/proposal metadata. Requires fixer role.", WaitForNetrunnerSession)
	addMcpTool(server, "launch_and_wait_netrunner", "Launch exactly one MCP-sensitive Netrunner through the serial explicit wire path and wait for its review-ready or terminal result in the same Fixer thread. Requires fixer role.", LaunchAndWaitNetrunner)
	addMcpTool(server, "create_netrunner_wave", "Create a durable parallel Netrunner wave from pending sessions after strict Git and write-scope admission. Does not launch workers. Requires fixer role.", CreateNetrunnerWave)
	addMcpTool(server, "get_netrunner_wave", "Read one durable parallel Netrunner wave and its worker rows for the current project. Requires fixer role.", GetNetrunnerWave)
	addMcpTool(server, "launch_netrunner_wave", "Launch all workers for one created parallel Netrunner wave using isolated Git worktrees. Requires fixer role.", LaunchNetrunnerWave)
	addMcpTool(server, "wait_for_netrunner_wave", "Wait for a launched parallel Netrunner wave to yield a review-ready or terminal worker and capture worktree diff artifacts. Requires fixer role.", WaitForNetrunnerWave)
	addMcpTool(server, "cleanup_netrunner_wave", "Clean up terminal parallel Netrunner wave worktrees with explicit removal/prune flags and safety checks. Requires fixer role.", CleanupNetrunnerWave)
	addMcpTool(server, "list_active_worker_processes", "List currently active Fixer-managed worker processes for the current project, with session mapping and liveness checks. Requires fixer role.", ListActiveWorkerProcesses)
	addMcpTool(server, "stop_active_worker_processes", "Stop active Fixer-managed worker processes for the current project and optionally freeze orchestration follow-up. Requires fixer role.", StopActiveWorkerProcesses)
	addMcpTool(server, "launch_image_generation_job", "Launch a dedicated Codex image-generation or image-editing subprocess for the current project and return a durable job id. Optional local input images can be attached for edit flows. Requires fixer role.", LaunchImageGenerationJob)
	addMcpTool(server, "wait_for_image_generation_job", "Wait for an image-generation job to finish and resolve the generated image path. Requires fixer role.", WaitForImageGenerationJob)
	addMcpTool(server, "copy_image_generation_job_output", "Copy a completed image-generation job output into the current project workspace. Requires fixer role.", CopyImageGenerationJobOutput)
}

func registerNetrunnerTools(server *mcp.Server) {
	addMcpTool(server, "get_pending_tasks", "For netrunners: Get a list of all pending tasks for the current project.", GetPendingTasks)
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
