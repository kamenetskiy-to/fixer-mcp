class StatusCounts {
  const StatusCounts({
    required this.pending,
    required this.inProgress,
    required this.review,
    required this.completed,
    required this.other,
    required this.total,
  });

  final int pending;
  final int inProgress;
  final int review;
  final int completed;
  final int other;
  final int total;

  factory StatusCounts.fromJson(Map<String, dynamic> json) {
    return StatusCounts(
      pending: _asInt(json['pending']),
      inProgress: _asInt(json['in_progress']),
      review: _asInt(json['review']),
      completed: _asInt(json['completed']),
      other: _asInt(json['other']),
      total: _asInt(json['total']),
    );
  }
}

class ProjectBinding {
  const ProjectBinding({
    required this.id,
    required this.name,
    required this.cwd,
  });

  final int id;
  final String name;
  final String cwd;

  factory ProjectBinding.fromJson(Map<String, dynamic> json) {
    return ProjectBinding(
      id: _asInt(json['id']),
      name: _asString(json['name']),
      cwd: _asString(json['cwd']),
    );
  }
}

class WorkerProcessRecord {
  const WorkerProcessRecord({
    required this.id,
    required this.sessionId,
    required this.localId,
    required this.pid,
    required this.launchEpoch,
    required this.status,
    required this.startedAt,
    required this.updatedAt,
    required this.stoppedAt,
    required this.alive,
    required this.stopReason,
  });

  final int id;
  final int sessionId;
  final int localId;
  final int pid;
  final int launchEpoch;
  final String status;
  final String startedAt;
  final String updatedAt;
  final String stoppedAt;
  final bool alive;
  final String stopReason;

  factory WorkerProcessRecord.fromJson(Map<String, dynamic> json) {
    return WorkerProcessRecord(
      id: _asInt(json['id']),
      sessionId: _asInt(json['session_id']),
      localId: _asInt(json['local_id']),
      pid: _asInt(json['pid']),
      launchEpoch: _asInt(json['launch_epoch']),
      status: _asString(json['status']),
      startedAt: _asString(json['started_at']),
      updatedAt: _asString(json['updated_at']),
      stoppedAt: _asString(json['stopped_at']),
      alive: _asBool(json['alive']),
      stopReason: _asString(json['stop_reason']),
    );
  }
}

class WorkerStateSummary {
  const WorkerStateSummary({
    required this.runningCount,
    required this.hasRunning,
    required this.processes,
  });

  final int runningCount;
  final bool hasRunning;
  final List<WorkerProcessRecord> processes;

  factory WorkerStateSummary.fromJson(Map<String, dynamic> json) {
    return WorkerStateSummary(
      runningCount: _asInt(json['running_count']),
      hasRunning: _asBool(json['has_running']),
      processes: _asList(json['processes'], WorkerProcessRecord.fromJson),
    );
  }
}

class AutonomousSummary {
  const AutonomousSummary({
    required this.projectsWithStatus,
    required this.runningProjects,
    required this.blockedProjects,
    required this.frozenProjects,
    required this.awaitingReviewProjects,
  });

  final int projectsWithStatus;
  final int runningProjects;
  final int blockedProjects;
  final int frozenProjects;
  final int awaitingReviewProjects;

  factory AutonomousSummary.fromJson(Map<String, dynamic> json) {
    return AutonomousSummary(
      projectsWithStatus: _asInt(json['projects_with_status']),
      runningProjects: _asInt(json['running_projects']),
      blockedProjects: _asInt(json['blocked_projects']),
      frozenProjects: _asInt(json['frozen_projects']),
      awaitingReviewProjects: _asInt(json['awaiting_review_projects']),
    );
  }
}

class AutonomousStatusRecord {
  const AutonomousStatusRecord({
    required this.projectId,
    required this.sessionId,
    required this.localSessionId,
    required this.state,
    required this.summary,
    required this.focus,
    required this.blocker,
    required this.evidence,
    required this.orchestrationEpoch,
    required this.orchestrationFrozen,
    required this.notificationsEnabledForActiveRun,
    required this.updatedAt,
  });

  final int projectId;
  final int sessionId;
  final int localSessionId;
  final String state;
  final String summary;
  final String focus;
  final String blocker;
  final String evidence;
  final int orchestrationEpoch;
  final bool orchestrationFrozen;
  final bool notificationsEnabledForActiveRun;
  final String updatedAt;

  factory AutonomousStatusRecord.fromJson(Map<String, dynamic> json) {
    return AutonomousStatusRecord(
      projectId: _asInt(json['project_id']),
      sessionId: _asInt(json['session_id']),
      localSessionId: _asInt(json['local_session_id']),
      state: _asString(json['state']),
      summary: _asString(json['summary']),
      focus: _asString(json['focus']),
      blocker: _asString(json['blocker']),
      evidence: _asString(json['evidence']),
      orchestrationEpoch: _asInt(json['orchestration_epoch']),
      orchestrationFrozen: _asBool(json['orchestration_frozen']),
      notificationsEnabledForActiveRun: _asBool(
        json['notifications_enabled_for_active_run'],
      ),
      updatedAt: _asString(json['updated_at']),
    );
  }
}

class ActiveWorkerSummary {
  const ActiveWorkerSummary({
    required this.projectId,
    required this.projectName,
    required this.sessionId,
    required this.localSessionId,
    required this.headline,
    required this.workerState,
  });

  final int projectId;
  final String projectName;
  final int sessionId;
  final int localSessionId;
  final String headline;
  final WorkerStateSummary workerState;

  factory ActiveWorkerSummary.fromJson(Map<String, dynamic> json) {
    return ActiveWorkerSummary(
      projectId: _asInt(json['project_id']),
      projectName: _asString(json['project_name']),
      sessionId: _asInt(json['session_id']),
      localSessionId: _asInt(json['local_session_id']),
      headline: _asString(json['headline']),
      workerState: WorkerStateSummary.fromJson(_asMap(json['worker_state'])),
    );
  }
}

class FixerChatSessionSummary {
  const FixerChatSessionSummary({
    required this.id,
    required this.localId,
    required this.externalId,
    required this.codexSessionId,
    required this.headline,
    required this.status,
    required this.agentRole,
    required this.backend,
    required this.model,
    required this.reasoning,
    required this.lastActivityAt,
    required this.bindingSource,
    required this.sessionLogPath,
    required this.sessionLog,
    required this.transcriptAvailable,
  });

  final int id;
  final int localId;
  final String externalId;
  final String codexSessionId;
  final String headline;
  final String status;
  final String agentRole;
  final String backend;
  final String model;
  final String reasoning;
  final String lastActivityAt;
  final String bindingSource;
  final String sessionLogPath;
  final bool sessionLog;
  final bool transcriptAvailable;

  factory FixerChatSessionSummary.fromJson(Map<String, dynamic> json) {
    return FixerChatSessionSummary(
      id: _asInt(json['id']),
      localId: _asInt(json['local_id']),
      externalId: _asString(json['external_id']),
      codexSessionId: _asString(json['codex_session_id']),
      headline: _asString(json['headline']),
      status: _asString(json['status']),
      agentRole: _asString(json['agent_role']),
      backend: _asString(json['backend']),
      model: _asString(json['model']),
      reasoning: _asString(json['reasoning']),
      lastActivityAt: _asString(json['last_activity_at']),
      bindingSource: _asString(json['binding_source']),
      sessionLogPath: _asString(json['session_log_path']),
      sessionLog: _asBool(json['session_log']),
      transcriptAvailable: _asBool(json['transcript_available']),
    );
  }
}

class FixerChatBindingRecord {
  const FixerChatBindingRecord({
    required this.projectId,
    required this.supported,
    required this.defaultSession,
    required this.sessions,
    required this.transcriptAvailability,
    required this.residualRisk,
  });

  final int projectId;
  final bool supported;
  final FixerChatSessionSummary? defaultSession;
  final List<FixerChatSessionSummary> sessions;
  final String transcriptAvailability;
  final String residualRisk;

  factory FixerChatBindingRecord.fromJson(Map<String, dynamic> json) {
    return FixerChatBindingRecord(
      projectId: _asInt(json['project_id']),
      supported: _asBool(json['supported']),
      defaultSession: json['default_session'] is Map<String, dynamic>
          ? FixerChatSessionSummary.fromJson(
              json['default_session'] as Map<String, dynamic>,
            )
          : json['default_session'] is Map
          ? FixerChatSessionSummary.fromJson(
              Map<String, dynamic>.from(json['default_session'] as Map),
            )
          : null,
      sessions: _asList(json['sessions'], FixerChatSessionSummary.fromJson),
      transcriptAvailability: _asString(json['transcript_availability']),
      residualRisk: _asString(json['residual_risk']),
    );
  }
}

class ThreadMessageRecord {
  const ThreadMessageRecord({
    required this.id,
    required this.role,
    required this.text,
    required this.createdAt,
    required this.source,
    this.kind = 'message',
    this.summary = '',
    this.collapsed = false,
  });

  final String id;
  final String role;
  final String text;
  final String createdAt;
  final String source;
  final String kind;
  final String summary;
  final bool collapsed;

  factory ThreadMessageRecord.fromJson(Map<String, dynamic> json) {
    return ThreadMessageRecord(
      id: _asString(json['id']),
      role: _asString(json['role']),
      text: _asString(json['text']),
      createdAt: _asString(json['createdAt'] ?? json['created_at']),
      source: _asString(json['source']),
      kind: _asString(json['kind']).isEmpty
          ? 'message'
          : _asString(json['kind']),
      summary: _asString(json['summary']),
      collapsed: _asBool(json['collapsed']),
    );
  }
}

class ThreadMessagesSnapshot {
  const ThreadMessagesSnapshot({
    required this.threadId,
    required this.transcriptAvailable,
    required this.availability,
    required this.unsupportedReason,
    required this.sessionLogPath,
    required this.messages,
    required this.sendSupported,
    required this.sendEndpoint,
    this.streamEndpointTemplate = '',
    this.turnStatusEndpointTemplate = '',
  });

  final String threadId;
  final bool transcriptAvailable;
  final String availability;
  final String unsupportedReason;
  final String sessionLogPath;
  final List<ThreadMessageRecord> messages;
  final bool sendSupported;
  final String sendEndpoint;
  final String streamEndpointTemplate;
  final String turnStatusEndpointTemplate;

  factory ThreadMessagesSnapshot.fromJson(Map<String, dynamic> json) {
    return ThreadMessagesSnapshot(
      threadId: _asString(json['threadId'] ?? json['thread_id']),
      transcriptAvailable: _asBool(
        json['transcriptAvailable'] ?? json['transcript_available'],
      ),
      availability: _asString(json['availability']),
      unsupportedReason: _asString(
        json['unsupportedReason'] ?? json['unsupported_reason'],
      ),
      sessionLogPath: _asString(
        json['sessionLogPath'] ?? json['session_log_path'],
      ),
      messages: _asList(json['messages'], ThreadMessageRecord.fromJson),
      sendSupported: _asBool(json['sendSupported'] ?? json['send_supported']),
      sendEndpoint: _asString(json['sendEndpoint'] ?? json['send_endpoint']),
      streamEndpointTemplate: _asString(
        json['streamEndpointTemplate'] ?? json['stream_endpoint_template'],
      ),
      turnStatusEndpointTemplate: _asString(
        json['turnStatusEndpointTemplate'] ??
            json['turn_status_endpoint_template'],
      ),
    );
  }
}

class ThreadSendResult {
  const ThreadSendResult({
    required this.threadId,
    required this.turnId,
    required this.streamId,
    this.streamEndpoint = '',
    this.turnStatusEndpoint = '',
  });

  final String threadId;
  final String turnId;
  final String streamId;
  final String streamEndpoint;
  final String turnStatusEndpoint;

  factory ThreadSendResult.fromJson(Map<String, dynamic> json) {
    return ThreadSendResult(
      threadId: _asString(json['threadId'] ?? json['thread_id']),
      turnId: _asString(json['turnId'] ?? json['turn_id']),
      streamId: _asString(json['streamId'] ?? json['stream_id']),
      streamEndpoint: _asString(
        json['streamEndpoint'] ?? json['stream_endpoint'],
      ),
      turnStatusEndpoint: _asString(
        json['turnStatusEndpoint'] ?? json['turn_status_endpoint'],
      ),
    );
  }
}

class ThreadTurnEventRecord {
  const ThreadTurnEventRecord({
    required this.sequence,
    required this.receivedAt,
    required this.method,
    required this.phase,
    required this.textDelta,
  });

  final int sequence;
  final String receivedAt;
  final String method;
  final String phase;
  final String textDelta;

  factory ThreadTurnEventRecord.fromJson(Map<String, dynamic> json) {
    return ThreadTurnEventRecord(
      sequence: _asInt(json['sequence']),
      receivedAt: _asString(json['receivedAt'] ?? json['received_at']),
      method: _asString(json['method']),
      phase: _asString(json['phase']),
      textDelta: _asString(json['textDelta'] ?? json['text_delta']),
    );
  }
}

class ThreadTurnStatusSnapshot {
  const ThreadTurnStatusSnapshot({
    required this.streamId,
    required this.threadId,
    required this.turnId,
    required this.done,
    required this.eventCount,
    required this.startedAt,
    required this.completedAt,
    required this.assistantText,
    required this.progressText,
    required this.events,
    required this.expired,
  });

  final String streamId;
  final String threadId;
  final String turnId;
  final bool done;
  final int eventCount;
  final String startedAt;
  final String completedAt;
  final String assistantText;
  final String progressText;
  final List<ThreadTurnEventRecord> events;
  final bool expired;

  factory ThreadTurnStatusSnapshot.fromJson(Map<String, dynamic> json) {
    return ThreadTurnStatusSnapshot(
      streamId: _asString(json['streamId'] ?? json['stream_id']),
      threadId: _asString(json['threadId'] ?? json['thread_id']),
      turnId: _asString(json['turnId'] ?? json['turn_id']),
      done: _asBool(json['done']),
      eventCount: _asInt(json['eventCount'] ?? json['event_count']),
      startedAt: _asString(json['startedAt'] ?? json['started_at']),
      completedAt: _asString(json['completedAt'] ?? json['completed_at']),
      assistantText: _asString(json['assistantText'] ?? json['assistant_text']),
      progressText: _asString(json['progressText'] ?? json['progress_text']),
      events: _asList(json['events'], ThreadTurnEventRecord.fromJson),
      expired: _asBool(json['expired']),
    );
  }
}

class ProjectCardRecord {
  const ProjectCardRecord({
    required this.project,
    required this.counts,
    required this.latestActivityLabel,
    required this.latestSessionId,
    required this.latestLocalSessionId,
    required this.autonomous,
    required this.hasPendingReview,
    required this.hasActiveWorkers,
  });

  final ProjectBinding project;
  final StatusCounts counts;
  final String latestActivityLabel;
  final int latestSessionId;
  final int latestLocalSessionId;
  final AutonomousStatusRecord? autonomous;
  final bool hasPendingReview;
  final bool hasActiveWorkers;

  factory ProjectCardRecord.fromJson(Map<String, dynamic> json) {
    return ProjectCardRecord(
      project: ProjectBinding.fromJson(_asMap(json['project'])),
      counts: StatusCounts.fromJson(_asMap(json['counts'])),
      latestActivityLabel: _asString(json['latest_activity_label']),
      latestSessionId: _asInt(json['latest_session_id']),
      latestLocalSessionId: _asInt(json['latest_local_session_id']),
      autonomous: json['autonomous'] is Map<String, dynamic>
          ? AutonomousStatusRecord.fromJson(
              json['autonomous'] as Map<String, dynamic>,
            )
          : json['autonomous'] is Map
          ? AutonomousStatusRecord.fromJson(
              Map<String, dynamic>.from(json['autonomous'] as Map),
            )
          : null,
      hasPendingReview: _asBool(json['has_pending_review']),
      hasActiveWorkers: _asBool(json['has_active_workers']),
    );
  }
}

class HomeSnapshot {
  const HomeSnapshot({
    required this.currentProject,
    required this.defaultChatBinding,
    required this.globalCounts,
    required this.projects,
    required this.activeWorkers,
    required this.autonomousSummary,
  });

  final ProjectBinding? currentProject;
  final FixerChatBindingRecord defaultChatBinding;
  final StatusCounts globalCounts;
  final List<ProjectCardRecord> projects;
  final List<ActiveWorkerSummary> activeWorkers;
  final AutonomousSummary autonomousSummary;

  factory HomeSnapshot.fromJson(Map<String, dynamic> json) {
    return HomeSnapshot(
      currentProject: json['current_project'] is Map<String, dynamic>
          ? ProjectBinding.fromJson(
              json['current_project'] as Map<String, dynamic>,
            )
          : json['current_project'] is Map
          ? ProjectBinding.fromJson(
              Map<String, dynamic>.from(json['current_project'] as Map),
            )
          : null,
      defaultChatBinding: FixerChatBindingRecord.fromJson(
        _asMap(json['default_chat_binding']),
      ),
      globalCounts: StatusCounts.fromJson(_asMap(json['global_counts'])),
      projects: _asList(json['projects'], ProjectCardRecord.fromJson),
      activeWorkers: _asList(
        json['active_workers'],
        ActiveWorkerSummary.fromJson,
      ),
      autonomousSummary: AutonomousSummary.fromJson(
        _asMap(json['autonomous_summary']),
      ),
    );
  }
}

class OverviewMetrics {
  const OverviewMetrics({
    required this.counts,
    required this.attachedDocCount,
    required this.pendingProposalCount,
    required this.workerState,
  });

  final StatusCounts counts;
  final int attachedDocCount;
  final int pendingProposalCount;
  final WorkerStateSummary workerState;

  factory OverviewMetrics.fromJson(Map<String, dynamic> json) {
    return OverviewMetrics(
      counts: StatusCounts.fromJson(_asMap(json['counts'])),
      attachedDocCount: _asInt(json['attached_doc_count']),
      pendingProposalCount: _asInt(json['pending_proposal_count']),
      workerState: WorkerStateSummary.fromJson(_asMap(json['worker_state'])),
    );
  }
}

class DocSummaryRecord {
  const DocSummaryRecord({
    required this.id,
    required this.title,
    required this.docType,
    required this.contentPreview,
    required this.targetedPendingProposals,
  });

  final int id;
  final String title;
  final String docType;
  final String contentPreview;
  final int targetedPendingProposals;

  factory DocSummaryRecord.fromJson(Map<String, dynamic> json) {
    return DocSummaryRecord(
      id: _asInt(json['id']),
      title: _asString(json['title']),
      docType: _asString(json['doc_type']),
      contentPreview: _asString(json['content_preview']),
      targetedPendingProposals: _asInt(json['targeted_pending_proposals']),
    );
  }
}

class DocGroupRecord {
  const DocGroupRecord({
    required this.docType,
    required this.docs,
    required this.pendingProposalCount,
    required this.targetedPendingCount,
    required this.untargetedPendingCount,
  });

  final String docType;
  final List<DocSummaryRecord> docs;
  final int pendingProposalCount;
  final int targetedPendingCount;
  final int untargetedPendingCount;

  factory DocGroupRecord.fromJson(Map<String, dynamic> json) {
    return DocGroupRecord(
      docType: _asString(json['doc_type']),
      docs: _asList(json['docs'], DocSummaryRecord.fromJson),
      pendingProposalCount: _asInt(json['pending_proposal_count']),
      targetedPendingCount: _asInt(json['targeted_pending_count']),
      untargetedPendingCount: _asInt(json['untargeted_pending_count']),
    );
  }
}

class DocsSummaryRecord {
  const DocsSummaryRecord({
    required this.totalDocs,
    required this.groups,
    required this.pendingProposalCount,
    required this.targetedPendingProposalCount,
    required this.untargetedPendingProposalCount,
  });

  final int totalDocs;
  final List<DocGroupRecord> groups;
  final int pendingProposalCount;
  final int targetedPendingProposalCount;
  final int untargetedPendingProposalCount;

  factory DocsSummaryRecord.fromJson(Map<String, dynamic> json) {
    return DocsSummaryRecord(
      totalDocs: _asInt(json['total_docs']),
      groups: _asList(json['groups'], DocGroupRecord.fromJson),
      pendingProposalCount: _asInt(json['pending_proposal_count']),
      targetedPendingProposalCount: _asInt(
        json['targeted_pending_proposal_count'],
      ),
      untargetedPendingProposalCount: _asInt(
        json['untargeted_pending_proposal_count'],
      ),
    );
  }
}

class MCPServerAssignmentRecord {
  const MCPServerAssignmentRecord({
    required this.id,
    required this.name,
    required this.shortDescription,
    required this.category,
    required this.howTo,
  });

  final int id;
  final String name;
  final String shortDescription;
  final String category;
  final String howTo;

  factory MCPServerAssignmentRecord.fromJson(Map<String, dynamic> json) {
    return MCPServerAssignmentRecord(
      id: _asInt(json['id']),
      name: _asString(json['name']),
      shortDescription: _asString(json['short_description']),
      category: _asString(json['category']),
      howTo: _asString(json['how_to']),
    );
  }
}

class AttachedDocRecord {
  const AttachedDocRecord({
    required this.id,
    required this.title,
    required this.docType,
    required this.summary,
  });

  final int id;
  final String title;
  final String docType;
  final String summary;

  factory AttachedDocRecord.fromJson(Map<String, dynamic> json) {
    return AttachedDocRecord(
      id: _asInt(json['id']),
      title: _asString(json['title']),
      docType: _asString(json['doc_type']),
      summary: _asString(json['summary']),
    );
  }
}

class DocProposalSummaryRecord {
  const DocProposalSummaryRecord({
    required this.id,
    required this.localId,
    required this.status,
    required this.proposedDocType,
    required this.proposedContent,
    required this.targetProjectDocId,
  });

  final int id;
  final int localId;
  final String status;
  final String proposedDocType;
  final String proposedContent;
  final int targetProjectDocId;

  factory DocProposalSummaryRecord.fromJson(Map<String, dynamic> json) {
    return DocProposalSummaryRecord(
      id: _asInt(json['id']),
      localId: _asInt(json['local_id']),
      status: _asString(json['status']),
      proposedDocType: _asString(json['proposed_doc_type']),
      proposedContent: _asString(json['proposed_content']),
      targetProjectDocId: _asInt(json['target_project_doc_id']),
    );
  }
}

class FinalReportRecord {
  const FinalReportRecord({
    required this.filesChanged,
    required this.commandsRun,
    required this.checksRun,
    required this.blockers,
    required this.residualRisks,
    required this.cleanupClaims,
  });

  final List<String> filesChanged;
  final List<String> commandsRun;
  final List<String> checksRun;
  final List<String> blockers;
  final List<String> residualRisks;
  final Map<String, List<String>> cleanupClaims;

  factory FinalReportRecord.fromJson(Map<String, dynamic> json) {
    final cleanupClaims = <String, List<String>>{};
    final rawClaims = json['cleanup_claims'];
    if (rawClaims is Map) {
      for (final entry in rawClaims.entries) {
        cleanupClaims[entry.key.toString()] = _asStringList(entry.value);
      }
    }
    return FinalReportRecord(
      filesChanged: _asStringList(json['files_changed']),
      commandsRun: _asStringList(json['commands_run']),
      checksRun: _asStringList(json['checks_run']),
      blockers: _asStringList(json['blockers']),
      residualRisks: _asStringList(json['residual_risks']),
      cleanupClaims: cleanupClaims,
    );
  }
}

class NetrunnerSummaryRecord {
  const NetrunnerSummaryRecord({
    required this.id,
    required this.localId,
    required this.projectId,
    required this.headline,
    required this.taskPreview,
    required this.status,
    required this.backend,
    required this.model,
    required this.reasoning,
    required this.writeScope,
    required this.attachedDocCount,
    required this.mcpCount,
    required this.proposalCount,
    required this.pendingProposalCount,
    required this.workerState,
    required this.reworkCount,
    required this.forcedStopCount,
    required this.repairSourceSessionId,
    required this.localRepairSourceId,
  });

  final int id;
  final int localId;
  final int projectId;
  final String headline;
  final String taskPreview;
  final String status;
  final String backend;
  final String model;
  final String reasoning;
  final List<String> writeScope;
  final int attachedDocCount;
  final int mcpCount;
  final int proposalCount;
  final int pendingProposalCount;
  final WorkerStateSummary workerState;
  final int reworkCount;
  final int forcedStopCount;
  final int repairSourceSessionId;
  final int localRepairSourceId;

  factory NetrunnerSummaryRecord.fromJson(Map<String, dynamic> json) {
    return NetrunnerSummaryRecord(
      id: _asInt(json['id']),
      localId: _asInt(json['local_id']),
      projectId: _asInt(json['project_id']),
      headline: _asString(json['headline']),
      taskPreview: _asString(json['task_preview']),
      status: _asString(json['status']),
      backend: _asString(json['backend']),
      model: _asString(json['model']),
      reasoning: _asString(json['reasoning']),
      writeScope: _asStringList(json['write_scope']),
      attachedDocCount: _asInt(json['attached_doc_count']),
      mcpCount: _asInt(json['mcp_count']),
      proposalCount: _asInt(json['proposal_count']),
      pendingProposalCount: _asInt(json['pending_proposal_count']),
      workerState: WorkerStateSummary.fromJson(_asMap(json['worker_state'])),
      reworkCount: _asInt(json['rework_count']),
      forcedStopCount: _asInt(json['forced_stop_count']),
      repairSourceSessionId: _asInt(json['repair_source_session_id']),
      localRepairSourceId: _asInt(json['local_repair_source_id']),
    );
  }
}

class SessionDetailRecord {
  const SessionDetailRecord({
    required this.id,
    required this.localId,
    required this.projectId,
    required this.taskDescription,
    required this.status,
    required this.backend,
    required this.model,
    required this.reasoning,
    required this.writeScope,
    required this.reportRaw,
    required this.structuredFinalReport,
    required this.attachedDocs,
    required this.mcpServers,
    required this.proposals,
    required this.workerState,
    required this.reworkCount,
    required this.forcedStopCount,
    required this.repairSourceSessionId,
    required this.localRepairSourceId,
    required this.availableDocs,
    required this.availableMcpServers,
    required this.allowedStatusTargets,
    required this.statusActionNote,
  });

  final int id;
  final int localId;
  final int projectId;
  final String taskDescription;
  final String status;
  final String backend;
  final String model;
  final String reasoning;
  final List<String> writeScope;
  final String reportRaw;
  final FinalReportRecord? structuredFinalReport;
  final List<AttachedDocRecord> attachedDocs;
  final List<MCPServerAssignmentRecord> mcpServers;
  final List<DocProposalSummaryRecord> proposals;
  final WorkerStateSummary workerState;
  final int reworkCount;
  final int forcedStopCount;
  final int repairSourceSessionId;
  final int localRepairSourceId;
  final List<AttachedDocRecord> availableDocs;
  final List<MCPServerAssignmentRecord> availableMcpServers;
  final List<String> allowedStatusTargets;
  final String statusActionNote;

  factory SessionDetailRecord.fromJson(Map<String, dynamic> json) {
    return SessionDetailRecord(
      id: _asInt(json['id']),
      localId: _asInt(json['local_id']),
      projectId: _asInt(json['project_id']),
      taskDescription: _asString(json['task_description']),
      status: _asString(json['status']),
      backend: _asString(json['backend']),
      model: _asString(json['model']),
      reasoning: _asString(json['reasoning']),
      writeScope: _asStringList(json['write_scope']),
      reportRaw: _asString(json['report_raw']),
      structuredFinalReport:
          json['structured_final_report'] is Map<String, dynamic>
          ? FinalReportRecord.fromJson(
              json['structured_final_report'] as Map<String, dynamic>,
            )
          : json['structured_final_report'] is Map
          ? FinalReportRecord.fromJson(
              Map<String, dynamic>.from(json['structured_final_report'] as Map),
            )
          : null,
      attachedDocs: _asList(json['attached_docs'], AttachedDocRecord.fromJson),
      mcpServers: _asList(
        json['mcp_servers'],
        MCPServerAssignmentRecord.fromJson,
      ),
      proposals: _asList(json['proposals'], DocProposalSummaryRecord.fromJson),
      workerState: WorkerStateSummary.fromJson(_asMap(json['worker_state'])),
      reworkCount: _asInt(json['rework_count']),
      forcedStopCount: _asInt(json['forced_stop_count']),
      repairSourceSessionId: _asInt(json['repair_source_session_id']),
      localRepairSourceId: _asInt(json['local_repair_source_id']),
      availableDocs: _asList(
        json['available_docs'],
        AttachedDocRecord.fromJson,
      ),
      availableMcpServers: _asList(
        json['available_mcp_servers'],
        MCPServerAssignmentRecord.fromJson,
      ),
      allowedStatusTargets: _asStringList(json['allowed_status_targets']),
      statusActionNote: _asString(json['status_action_note']),
    );
  }
}

class ProjectWorkspaceSnapshot {
  const ProjectWorkspaceSnapshot({
    required this.project,
    required this.metrics,
    required this.autonomous,
    required this.docs,
    required this.netrunners,
    required this.fixerChat,
  });

  final ProjectBinding project;
  final OverviewMetrics metrics;
  final AutonomousStatusRecord? autonomous;
  final DocsSummaryRecord docs;
  final List<NetrunnerSummaryRecord> netrunners;
  final FixerChatBindingRecord fixerChat;

  factory ProjectWorkspaceSnapshot.fromJson(
    Map<String, dynamic> snapshotJson,
    Map<String, dynamic> docsJson,
    Map<String, dynamic> chatJson,
  ) {
    return ProjectWorkspaceSnapshot(
      project: ProjectBinding.fromJson(_asMap(snapshotJson['project'])),
      metrics: OverviewMetrics.fromJson(_asMap(snapshotJson['metrics'])),
      autonomous: snapshotJson['autonomous'] is Map<String, dynamic>
          ? AutonomousStatusRecord.fromJson(
              snapshotJson['autonomous'] as Map<String, dynamic>,
            )
          : snapshotJson['autonomous'] is Map
          ? AutonomousStatusRecord.fromJson(
              Map<String, dynamic>.from(snapshotJson['autonomous'] as Map),
            )
          : null,
      docs: DocsSummaryRecord.fromJson(_asMap(docsJson['docs'])),
      netrunners: _asList(
        snapshotJson['netrunners'],
        NetrunnerSummaryRecord.fromJson,
      ),
      fixerChat: FixerChatBindingRecord.fromJson(chatJson),
    );
  }
}

class NetrunnerDetailSnapshot {
  const NetrunnerDetailSnapshot({required this.session});

  final SessionDetailRecord session;

  factory NetrunnerDetailSnapshot.fromJson(Map<String, dynamic> json) {
    return NetrunnerDetailSnapshot(
      session: SessionDetailRecord.fromJson(_asMap(json['session'])),
    );
  }
}

int _asInt(Object? value) {
  if (value is int) {
    return value;
  }
  if (value is num) {
    return value.toInt();
  }
  if (value is String) {
    return int.tryParse(value.trim()) ?? 0;
  }
  return 0;
}

String _asString(Object? value) {
  if (value is String) {
    return value;
  }
  return '';
}

bool _asBool(Object? value) {
  if (value is bool) {
    return value;
  }
  if (value is num) {
    return value != 0;
  }
  if (value is String) {
    final normalized = value.trim().toLowerCase();
    return normalized == 'true' || normalized == '1';
  }
  return false;
}

Map<String, dynamic> _asMap(Object? value) {
  if (value is Map<String, dynamic>) {
    return value;
  }
  if (value is Map) {
    return Map<String, dynamic>.from(value);
  }
  return const <String, dynamic>{};
}

List<String> _asStringList(Object? value) {
  if (value is List) {
    return value.map((item) => item?.toString() ?? '').toList();
  }
  return const <String>[];
}

List<T> _asList<T>(Object? value, T Function(Map<String, dynamic>) fromJson) {
  if (value is! List) {
    return <T>[];
  }
  final items = <T>[];
  for (final item in value) {
    if (item is Map<String, dynamic>) {
      items.add(fromJson(item));
    } else if (item is Map) {
      items.add(fromJson(Map<String, dynamic>.from(item)));
    }
  }
  return items;
}
