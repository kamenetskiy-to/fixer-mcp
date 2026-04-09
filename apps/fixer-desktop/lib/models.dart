class ProjectSummary {
  ProjectSummary({
    required this.id,
    required this.name,
    required this.cwd,
    required this.sessionCounts,
    required this.pendingDocProposals,
    required this.activeWorkerCount,
    required this.latestRunStatus,
  });

  final int id;
  final String name;
  final String cwd;
  final Map<String, int> sessionCounts;
  final int pendingDocProposals;
  final int activeWorkerCount;
  final RunStatus? latestRunStatus;

  factory ProjectSummary.fromJson(Map<String, dynamic> json) {
    return ProjectSummary(
      id: json['id'] as int,
      name: json['name'] as String,
      cwd: json['cwd'] as String,
      sessionCounts: _intMap(json['session_counts']),
      pendingDocProposals: json['pending_doc_proposals'] as int? ?? 0,
      activeWorkerCount: json['active_worker_count'] as int? ?? 0,
      latestRunStatus: json['latest_run_status'] == null
          ? null
          : RunStatus.fromJson(
              json['latest_run_status'] as Map<String, dynamic>,
            ),
    );
  }
}

class ProjectDashboard {
  ProjectDashboard({
    required this.id,
    required this.name,
    required this.cwd,
    required this.sessionCounts,
    required this.pendingDocProposals,
    required this.activeWorkerCount,
    required this.runStatus,
    required this.sessions,
  });

  final int id;
  final String name;
  final String cwd;
  final Map<String, int> sessionCounts;
  final int pendingDocProposals;
  final int activeWorkerCount;
  final RunStatus? runStatus;
  final List<DashboardSession> sessions;

  factory ProjectDashboard.fromJson(Map<String, dynamic> json) {
    return ProjectDashboard(
      id: json['id'] as int,
      name: json['name'] as String,
      cwd: json['cwd'] as String,
      sessionCounts: _intMap(json['session_counts']),
      pendingDocProposals: json['pending_doc_proposals'] as int? ?? 0,
      activeWorkerCount: json['active_worker_count'] as int? ?? 0,
      runStatus: json['run_status'] == null
          ? null
          : RunStatus.fromJson(json['run_status'] as Map<String, dynamic>),
      sessions: (json['sessions'] as List<dynamic>? ?? const [])
          .map(
            (dynamic item) =>
                DashboardSession.fromJson(item as Map<String, dynamic>),
          )
          .toList(),
    );
  }
}

class DashboardSession {
  DashboardSession({
    required this.id,
    required this.status,
    required this.taskTitle,
    required this.taskDescription,
    required this.cliBackend,
    required this.cliModel,
    required this.cliReasoning,
    required this.attachedDocCount,
    required this.pendingProposalCount,
  });

  final int id;
  final String status;
  final String taskTitle;
  final String taskDescription;
  final String cliBackend;
  final String cliModel;
  final String cliReasoning;
  final int attachedDocCount;
  final int pendingProposalCount;

  factory DashboardSession.fromJson(Map<String, dynamic> json) {
    return DashboardSession(
      id: json['id'] as int,
      status: json['status'] as String,
      taskTitle: json['task_title'] as String,
      taskDescription: json['task_description'] as String,
      cliBackend: json['cli_backend'] as String? ?? '',
      cliModel: json['cli_model'] as String? ?? '',
      cliReasoning: json['cli_reasoning'] as String? ?? '',
      attachedDocCount: json['attached_doc_count'] as int? ?? 0,
      pendingProposalCount: json['pending_proposal_count'] as int? ?? 0,
    );
  }
}

class SessionDetail {
  SessionDetail({
    required this.id,
    required this.status,
    required this.taskTitle,
    required this.taskDescription,
    required this.cliBackend,
    required this.cliModel,
    required this.cliReasoning,
    required this.attachedDocs,
    required this.mcpServers,
    required this.docProposals,
    required this.workerProcesses,
  });

  final int id;
  final String status;
  final String taskTitle;
  final String taskDescription;
  final String cliBackend;
  final String cliModel;
  final String cliReasoning;
  final List<AttachedDoc> attachedDocs;
  final List<McpServerEntry> mcpServers;
  final List<DocProposalEntry> docProposals;
  final List<WorkerProcessEntry> workerProcesses;

  factory SessionDetail.fromJson(Map<String, dynamic> json) {
    return SessionDetail(
      id: json['id'] as int,
      status: json['status'] as String,
      taskTitle: json['task_title'] as String,
      taskDescription: json['task_description'] as String,
      cliBackend: json['cli_backend'] as String? ?? '',
      cliModel: json['cli_model'] as String? ?? '',
      cliReasoning: json['cli_reasoning'] as String? ?? '',
      attachedDocs: (json['attached_docs'] as List<dynamic>? ?? const [])
          .map(
            (dynamic item) =>
                AttachedDoc.fromJson(item as Map<String, dynamic>),
          )
          .toList(),
      mcpServers: (json['mcp_servers'] as List<dynamic>? ?? const [])
          .map(
            (dynamic item) =>
                McpServerEntry.fromJson(item as Map<String, dynamic>),
          )
          .toList(),
      docProposals: (json['doc_proposals'] as List<dynamic>? ?? const [])
          .map(
            (dynamic item) =>
                DocProposalEntry.fromJson(item as Map<String, dynamic>),
          )
          .toList(),
      workerProcesses: (json['worker_processes'] as List<dynamic>? ?? const [])
          .map(
            (dynamic item) =>
                WorkerProcessEntry.fromJson(item as Map<String, dynamic>),
          )
          .toList(),
    );
  }
}

class RunStatus {
  RunStatus({required this.state, required this.summary, required this.focus});

  final String state;
  final String summary;
  final String focus;

  factory RunStatus.fromJson(Map<String, dynamic> json) {
    return RunStatus(
      state: json['state'] as String? ?? '',
      summary: json['summary'] as String? ?? '',
      focus: json['focus'] as String? ?? '',
    );
  }
}

class AttachedDoc {
  AttachedDoc({required this.id, required this.title, required this.docType});

  final int id;
  final String title;
  final String docType;

  factory AttachedDoc.fromJson(Map<String, dynamic> json) {
    return AttachedDoc(
      id: json['id'] as int,
      title: json['title'] as String,
      docType: json['doc_type'] as String? ?? '',
    );
  }
}

class McpServerEntry {
  McpServerEntry({
    required this.name,
    required this.category,
    required this.shortDescription,
    required this.howTo,
  });

  final String name;
  final String category;
  final String shortDescription;
  final String howTo;

  factory McpServerEntry.fromJson(Map<String, dynamic> json) {
    return McpServerEntry(
      name: json['name'] as String,
      category: json['category'] as String? ?? '',
      shortDescription: json['short_description'] as String? ?? '',
      howTo: json['how_to'] as String? ?? '',
    );
  }
}

class DocProposalEntry {
  DocProposalEntry({
    required this.id,
    required this.status,
    required this.proposedDocType,
  });

  final int id;
  final String status;
  final String proposedDocType;

  factory DocProposalEntry.fromJson(Map<String, dynamic> json) {
    return DocProposalEntry(
      id: json['id'] as int,
      status: json['status'] as String,
      proposedDocType: json['proposed_doc_type'] as String? ?? '',
    );
  }
}

class WorkerProcessEntry {
  WorkerProcessEntry({
    required this.pid,
    required this.status,
    required this.updatedAt,
  });

  final int pid;
  final String status;
  final String updatedAt;

  factory WorkerProcessEntry.fromJson(Map<String, dynamic> json) {
    return WorkerProcessEntry(
      pid: json['pid'] as int,
      status: json['status'] as String,
      updatedAt: json['updated_at'] as String? ?? '',
    );
  }
}

Map<String, int> _intMap(Object? value) {
  final raw = value as Map<String, dynamic>? ?? const {};
  return raw.map(
    (String key, dynamic item) => MapEntry(key, item as int? ?? 0),
  );
}
