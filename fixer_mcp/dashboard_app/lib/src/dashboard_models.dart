class ProjectRecord {
  const ProjectRecord({
    required this.id,
    required this.name,
    required this.cwd,
  });

  final int id;
  final String name;
  final String cwd;
}

class SessionRecord {
  const SessionRecord({
    required this.id,
    required this.localId,
    required this.projectId,
    required this.taskDescription,
    required this.status,
    required this.report,
  });

  final int id;
  final int localId;
  final int projectId;
  final String taskDescription;
  final String status;
  final String report;

  String get headline {
    final firstLine = taskDescription.trim().split('\n').firstOrNull;
    final base = (firstLine == null || firstLine.trim().isEmpty)
        ? taskDescription.trim()
        : firstLine.trim();
    if (base.isEmpty) {
      return 'Session #$localId';
    }
    return base.length <= 96 ? base : '${base.substring(0, 93)}...';
  }
}

class AutonomousRunGroup {
  const AutonomousRunGroup({
    required this.index,
    required this.label,
    required this.sessions,
    required this.globalSessionSpan,
    required this.stateLabel,
    required this.summary,
    required this.evidence,
    required this.currentStep,
    required this.lastCompletedStep,
    required this.nextStep,
    required this.isActive,
  });

  final int index;
  final String label;
  final List<SessionRecord> sessions;
  final String globalSessionSpan;
  final String stateLabel;
  final String summary;
  final String evidence;
  final String currentStep;
  final String lastCompletedStep;
  final String nextStep;
  final bool isActive;

  String get sessionSpan {
    if (sessions.isEmpty) {
      return '';
    }
    final first = sessions.first.localId;
    final last = sessions.last.localId;
    return first == last ? '#$first' : '#$first-#$last';
  }
}

class AutonomousStatusRecord {
  const AutonomousStatusRecord({
    required this.projectId,
    required this.state,
    required this.summary,
    required this.updatedAt,
    this.sessionId = 0,
    this.focus = '',
    this.blocker = '',
    this.evidence = '',
  });

  final int projectId;
  final int sessionId;
  final String state;
  final String summary;
  final String focus;
  final String blocker;
  final String evidence;
  final String updatedAt;
}

class AutonomousWorkflowRecord {
  const AutonomousWorkflowRecord({
    required this.mode,
    required this.workflowType,
    required this.workflowLabel,
    required this.loggedSessionLocalIds,
    this.activeSessionLocalId = 0,
    this.lastCompletedSessionLocalId = 0,
    this.lastHandoffSummary = '',
    this.updatedAtEpoch = 0,
  });

  final String mode;
  final String workflowType;
  final String workflowLabel;
  final int activeSessionLocalId;
  final int lastCompletedSessionLocalId;
  final List<int> loggedSessionLocalIds;
  final String lastHandoffSummary;
  final int updatedAtEpoch;

  bool get hasWorkflow =>
      mode.isNotEmpty ||
      workflowType.isNotEmpty ||
      workflowLabel.isNotEmpty ||
      activeSessionLocalId > 0 ||
      lastCompletedSessionLocalId > 0 ||
      loggedSessionLocalIds.isNotEmpty;
}

class AutonomousRunView {
  const AutonomousRunView({
    required this.hasRun,
    required this.source,
    required this.stateLabel,
    required this.summary,
    required this.evidence,
    required this.sessions,
    required this.groups,
    required this.latestActivityLabel,
    required this.latestActivitySessionId,
    required this.latestActivityLocalSessionId,
    this.currentStep = '',
    this.lastCompletedStep = '',
    this.nextStep = '',
    this.focus = '',
    this.blocker = '',
  });

  final bool hasRun;
  final String source;
  final String stateLabel;
  final String summary;
  final String evidence;
  final List<SessionRecord> sessions;
  final List<AutonomousRunGroup> groups;
  final String latestActivityLabel;
  final int latestActivitySessionId;
  final int latestActivityLocalSessionId;
  final String currentStep;
  final String lastCompletedStep;
  final String nextStep;
  final String focus;
  final String blocker;
}

class ProjectDashboardData {
  const ProjectDashboardData({
    required this.project,
    required this.sessions,
    required this.latestActivitySessionId,
    required this.latestActivityLocalSessionId,
    required this.latestActivityLabel,
    required this.pendingCount,
    required this.inProgressCount,
    required this.reviewCount,
    required this.completedCount,
    required this.autonomousRun,
  });

  final ProjectRecord project;
  final List<SessionRecord> sessions;
  final int latestActivitySessionId;
  final int latestActivityLocalSessionId;
  final String latestActivityLabel;
  final int pendingCount;
  final int inProgressCount;
  final int reviewCount;
  final int completedCount;
  final AutonomousRunView autonomousRun;

  int get activitySortKey => latestActivitySessionId;

  bool get hasActiveWork =>
      pendingCount > 0 || inProgressCount > 0 || reviewCount > 0;

  List<SessionRecord> get activeSessions => sessions
      .where(
        (session) =>
            session.status == 'in_progress' || session.status == 'review',
      )
      .toList();
}

class DashboardSnapshot {
  const DashboardSnapshot({required this.databasePath, required this.projects});

  final String databasePath;
  final List<ProjectDashboardData> projects;

  int get autonomousProjectCount =>
      projects.where((project) => project.autonomousRun.hasRun).length;
  int get activeSessionCount =>
      projects.fold<int>(0, (sum, project) => sum + project.inProgressCount);
}

extension _FirstOrNullExtension<T> on List<T> {
  T? get firstOrNull => isEmpty ? null : first;
}
