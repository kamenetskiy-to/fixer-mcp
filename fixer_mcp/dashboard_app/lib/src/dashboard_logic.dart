import 'dart:math' as math;

import 'dashboard_models.dart';

bool looksLikeAutonomousSession(SessionRecord session) {
  final firstLine = session.taskDescription
      .trim()
      .split('\n')
      .first
      .toLowerCase();
  return firstLine.startsWith('autonomous') ||
      firstLine.startsWith('[autonomous') ||
      firstLine.contains('ghost run') ||
      firstLine.contains('serial autonomous') ||
      firstLine.contains('autonomous-resolution') ||
      RegExp(r'(^|\W)autonomous session(\W|$)').hasMatch(firstLine) ||
      RegExp(r'(^|\W)autonomous run(\W|$)').hasMatch(firstLine) ||
      RegExp(r'(^|\W)autonomous reconnaissance(\W|$)').hasMatch(firstLine);
}

String summarizeTask(String taskDescription) {
  final trimmed = taskDescription.trim();
  if (trimmed.isEmpty) {
    return '';
  }
  final firstLine = trimmed.split('\n').first.trim();
  if (firstLine.length <= 120) {
    return firstLine;
  }
  return '${firstLine.substring(0, 117)}...';
}

ProjectDashboardData buildProjectDashboardData(
  ProjectRecord project,
  List<SessionRecord> sessions,
  AutonomousStatusRecord? explicitStatus,
  AutonomousWorkflowRecord? workflow,
) {
  final orderedSessions = List<SessionRecord>.from(sessions)
    ..sort((left, right) => left.id.compareTo(right.id));
  final sessionsWithLocalIds = <SessionRecord>[
    for (var index = 0; index < orderedSessions.length; index++)
      SessionRecord(
        id: orderedSessions[index].id,
        localId: index + 1,
        projectId: orderedSessions[index].projectId,
        taskDescription: orderedSessions[index].taskDescription,
        status: orderedSessions[index].status,
        report: orderedSessions[index].report,
      ),
  ];
  final explicit = explicitStatus;
  final latestSession = sessionsWithLocalIds.isNotEmpty
      ? sessionsWithLocalIds.last
      : null;
  final latestActivitySessionId = math.max(
    latestSession?.id ?? 0,
    explicit?.sessionId ?? 0,
  );
  final latestActivityLocalSessionId = math.max(
    latestSession?.localId ?? 0,
    _localSessionIdForGlobal(explicit?.sessionId ?? 0, sessionsWithLocalIds),
  );

  final pendingCount = sessionsWithLocalIds
      .where((session) => session.status == 'pending')
      .length;
  final inProgressCount = sessionsWithLocalIds
      .where((session) => session.status == 'in_progress')
      .length;
  final reviewCount = sessionsWithLocalIds
      .where((session) => session.status == 'review')
      .length;
  final completedCount = sessionsWithLocalIds
      .where((session) => session.status == 'completed')
      .length;

  final workflowSessionLocalIds = _workflowSessionLocalIds(
    workflow,
    sessionsWithLocalIds,
  );
  final autonomousSessions = workflowSessionLocalIds.isNotEmpty
      ? sessionsWithLocalIds
            .where(
              (session) => workflowSessionLocalIds.contains(session.localId),
            )
            .toList()
      : sessionsWithLocalIds.where(looksLikeAutonomousSession).toList();
  final autonomousGroups = _buildAutonomousGroups(autonomousSessions);
  final activeAutonomousSessions = autonomousSessions
      .where(
        (session) =>
            session.status == 'in_progress' || session.status == 'review',
      )
      .toList();
  final completedAutonomousSessions = autonomousSessions
      .where((session) => session.status == 'completed')
      .toList();
  final pendingAutonomousSessions = autonomousSessions
      .where((session) => session.status == 'pending')
      .toList();

  final hasExplicitStatus = explicit != null;
  final hasWorkflow = workflow?.hasWorkflow ?? false;
  final source = hasExplicitStatus
      ? 'explicit control-plane status'
      : hasWorkflow
      ? 'repo workflow metadata + fixer handoff log'
      : 'derived from session history';

  final currentStep = _sessionHeadline(
    activeAutonomousSessions.isNotEmpty
        ? activeAutonomousSessions.last
        : (pendingAutonomousSessions.isNotEmpty
              ? pendingAutonomousSessions.first
              : null),
  );
  final lastCompletedStep = _sessionHeadline(
    completedAutonomousSessions.isNotEmpty
        ? completedAutonomousSessions.last
        : null,
  );
  final nextStep = _sessionHeadline(
    pendingAutonomousSessions.isNotEmpty
        ? pendingAutonomousSessions.first
        : null,
  );

  late final String stateLabel;
  late final String summary;
  late final String evidence;
  var focus = '';
  var blocker = '';

  if (explicit != null) {
    stateLabel = explicit.state;
    summary = explicit.summary;
    evidence = [
      if (explicit.sessionId > 0) 'status session #${explicit.sessionId}',
      if (explicit.evidence.trim().isNotEmpty) explicit.evidence.trim(),
    ].join(' · ');
    focus = explicit.focus;
    blocker = explicit.blocker;
  } else if (hasWorkflow) {
    stateLabel = _deriveWorkflowState(workflow, autonomousSessions);
    summary = _deriveWorkflowSummary(
      workflow!,
      autonomousSessions,
      stateLabel,
      currentStep,
      lastCompletedStep,
      nextStep,
    );
    evidence = _deriveWorkflowEvidence(workflow, autonomousSessions);
  } else {
    stateLabel = _deriveAutonomousState(
      activeAutonomousSessions: activeAutonomousSessions,
      completedAutonomousSessions: completedAutonomousSessions,
      pendingAutonomousSessions: pendingAutonomousSessions,
    );
    summary = _deriveAutonomousSummary(
      project,
      autonomousSessions,
      stateLabel,
      currentStep,
      lastCompletedStep,
      nextStep,
    );
    evidence = _deriveEvidence(
      autonomousSessions,
      activeAutonomousSessions,
      completedAutonomousSessions,
      pendingAutonomousSessions,
    );
  }

  return ProjectDashboardData(
    project: project,
    sessions: sessionsWithLocalIds,
    latestActivitySessionId: latestActivitySessionId,
    latestActivityLocalSessionId: latestActivityLocalSessionId,
    latestActivityLabel: _latestActivityLabel(
      latestActivitySessionId: latestActivitySessionId,
      latestActivityLocalSessionId: latestActivityLocalSessionId,
      latestSession: latestSession,
      explicitStatus: explicit,
    ),
    pendingCount: pendingCount,
    inProgressCount: inProgressCount,
    reviewCount: reviewCount,
    completedCount: completedCount,
    autonomousRun: AutonomousRunView(
      hasRun: hasExplicitStatus || hasWorkflow || autonomousSessions.isNotEmpty,
      source: source,
      stateLabel: stateLabel,
      summary: summary.isNotEmpty
          ? summary
          : _deriveAutonomousSummary(
              project,
              autonomousSessions,
              stateLabel,
              currentStep,
              lastCompletedStep,
              nextStep,
            ),
      evidence: evidence,
      sessions: autonomousSessions,
      groups: autonomousGroups,
      latestActivityLabel: _latestActivityLabel(
        latestActivitySessionId: latestActivitySessionId,
        latestActivityLocalSessionId: latestActivityLocalSessionId,
        latestSession: latestSession,
        explicitStatus: explicit,
      ),
      latestActivitySessionId: latestActivitySessionId,
      latestActivityLocalSessionId: latestActivityLocalSessionId,
      currentStep: currentStep,
      lastCompletedStep: lastCompletedStep,
      nextStep: nextStep,
      focus: focus,
      blocker: blocker,
    ),
  );
}

Set<int> _workflowSessionLocalIds(
  AutonomousWorkflowRecord? workflow,
  List<SessionRecord> sessions,
) {
  if (workflow == null) {
    return const <int>{};
  }
  final ids = <int>{...workflow.loggedSessionLocalIds};
  if (workflow.activeSessionLocalId > 0) {
    ids.add(workflow.activeSessionLocalId);
  }
  if (workflow.lastCompletedSessionLocalId > 0) {
    ids.add(workflow.lastCompletedSessionLocalId);
  }
  ids.removeWhere(
    (localId) => !sessions.any((session) => session.localId == localId),
  );
  return ids;
}

List<AutonomousRunGroup> _buildAutonomousGroups(
  List<SessionRecord> autonomousSessions,
) {
  if (autonomousSessions.isEmpty) {
    return const <AutonomousRunGroup>[];
  }

  final groups = <List<SessionRecord>>[];
  var currentGroup = <SessionRecord>[];
  for (final session in autonomousSessions) {
    if (currentGroup.isEmpty ||
        session.localId == currentGroup.last.localId + 1) {
      currentGroup.add(session);
      continue;
    }
    groups.add(currentGroup);
    currentGroup = <SessionRecord>[session];
  }
  if (currentGroup.isNotEmpty) {
    groups.add(currentGroup);
  }

  return <AutonomousRunGroup>[
    for (var index = 0; index < groups.length; index++)
      _buildAutonomousGroup(index + 1, groups[index]),
  ];
}

AutonomousRunGroup _buildAutonomousGroup(
  int index,
  List<SessionRecord> sessions,
) {
  final stateLabel = _deriveGroupState(sessions);
  final currentStepSession =
      _lastMatching(sessions, (session) => session.status == 'in_progress') ??
      _lastMatching(sessions, (session) => session.status == 'review') ??
      sessions.last;
  final lastCompletedStepSession = _lastMatching(
    sessions,
    (session) => session.status == 'completed',
  );
  final nextStepSession = _firstMatching(
    sessions,
    (session) => session.status == 'pending',
  );
  return AutonomousRunGroup(
    index: index,
    label: 'Run #$index',
    sessions: sessions,
    globalSessionSpan: _sessionSpanForGlobalIds(sessions),
    stateLabel: stateLabel,
    summary:
        '${sessions.length} autonomous session${sessions.length == 1 ? '' : 's'} · $stateLabel',
    evidence: 'grouped session ids: ${_sessionSpanForDisplayIds(sessions)}',
    currentStep: _sessionHeadline(currentStepSession),
    lastCompletedStep: _sessionHeadline(lastCompletedStepSession),
    nextStep: _sessionHeadline(nextStepSession),
    isActive: stateLabel == 'running' || stateLabel == 'awaiting_review',
  );
}

String _deriveGroupState(List<SessionRecord> sessions) {
  if (sessions.any((session) => session.status == 'in_progress')) {
    return 'running';
  }
  if (sessions.any((session) => session.status == 'review')) {
    return 'awaiting_review';
  }
  if (sessions.every((session) => session.status == 'completed')) {
    return 'completed';
  }
  if (sessions.any((session) => session.status == 'pending')) {
    return 'awaiting_next_dispatch';
  }
  return 'idle';
}

String _deriveAutonomousState({
  required List<SessionRecord> activeAutonomousSessions,
  required List<SessionRecord> completedAutonomousSessions,
  required List<SessionRecord> pendingAutonomousSessions,
}) {
  if (activeAutonomousSessions.any(
    (session) => session.status == 'in_progress',
  )) {
    return 'running';
  }
  if (activeAutonomousSessions.any((session) => session.status == 'review')) {
    return 'awaiting_review';
  }
  if (pendingAutonomousSessions.isNotEmpty &&
      completedAutonomousSessions.isNotEmpty &&
      activeAutonomousSessions.isEmpty) {
    return 'awaiting_next_dispatch';
  }
  if (completedAutonomousSessions.isNotEmpty &&
      activeAutonomousSessions.isEmpty &&
      pendingAutonomousSessions.isEmpty) {
    return 'completed';
  }
  if (pendingAutonomousSessions.isNotEmpty) {
    return 'awaiting_next_dispatch';
  }
  return 'idle';
}

String _deriveWorkflowState(
  AutonomousWorkflowRecord? workflow,
  List<SessionRecord> autonomousSessions,
) {
  final activeSession = autonomousSessions.firstWhereOrNull(
    (session) => session.localId == workflow?.activeSessionLocalId,
  );
  if (activeSession != null) {
    if (activeSession.status == 'review') {
      return 'awaiting_review';
    }
    if (activeSession.status == 'pending') {
      return 'awaiting_next_dispatch';
    }
    return 'running';
  }

  if (autonomousSessions.any((session) => session.status == 'in_progress')) {
    return 'running';
  }
  if (autonomousSessions.any((session) => session.status == 'review')) {
    return 'awaiting_review';
  }
  if (autonomousSessions.any((session) => session.status == 'pending')) {
    return 'awaiting_next_dispatch';
  }
  if (workflow != null &&
      workflow.lastCompletedSessionLocalId > 0 &&
      autonomousSessions.isNotEmpty) {
    return 'completed';
  }
  return 'idle';
}

String _deriveAutonomousSummary(
  ProjectRecord project,
  List<SessionRecord> autonomousSessions,
  String stateLabel,
  String currentStep,
  String lastCompletedStep,
  String nextStep,
) {
  if (autonomousSessions.isEmpty) {
    return 'No autonomous session markers found for ${project.name}.';
  }

  final count = autonomousSessions.length;
  final parts = <String>[
    '$count autonomous session${count == 1 ? '' : 's'} detected',
    'state: $stateLabel',
  ];
  if (currentStep.isNotEmpty) {
    parts.add('current: $currentStep');
  }
  if (lastCompletedStep.isNotEmpty) {
    parts.add('last done: $lastCompletedStep');
  }
  if (nextStep.isNotEmpty) {
    parts.add('next: $nextStep');
  }
  return parts.join(' · ');
}

String _deriveWorkflowSummary(
  AutonomousWorkflowRecord workflow,
  List<SessionRecord> autonomousSessions,
  String stateLabel,
  String currentStep,
  String lastCompletedStep,
  String nextStep,
) {
  final label = workflow.workflowLabel.trim().isEmpty
      ? 'Autonomous run'
      : workflow.workflowLabel.trim();
  final count = autonomousSessions.length;
  final parts = <String>[
    label,
    '$count netrunner session${count == 1 ? '' : 's'} tracked',
    'state: $stateLabel',
  ];
  if (currentStep.isNotEmpty) {
    parts.add('current: $currentStep');
  }
  if (lastCompletedStep.isNotEmpty) {
    parts.add('last done: $lastCompletedStep');
  }
  if (nextStep.isNotEmpty) {
    parts.add('next: $nextStep');
  }
  return parts.join(' · ');
}

String _deriveEvidence(
  List<SessionRecord> autonomousSessions,
  List<SessionRecord> activeAutonomousSessions,
  List<SessionRecord> completedAutonomousSessions,
  List<SessionRecord> pendingAutonomousSessions,
) {
  if (autonomousSessions.isEmpty) {
    return 'No task descriptions matched autonomous markers.';
  }

  final bits = <String>[
    'matched session ids: ${_sessionSpanForDisplayIds(autonomousSessions)}',
  ];
  if (activeAutonomousSessions.isNotEmpty) {
    bits.add(
      'active: ${activeAutonomousSessions.map((session) => '#${session.localId}:${session.status}').join(', ')}',
    );
  }
  if (completedAutonomousSessions.isNotEmpty) {
    bits.add(
      'completed: ${completedAutonomousSessions.map((session) => '#${session.localId}').join(', ')}',
    );
  }
  if (pendingAutonomousSessions.isNotEmpty) {
    bits.add(
      'pending: ${pendingAutonomousSessions.map((session) => '#${session.localId}').join(', ')}',
    );
  }
  return bits.join(' · ');
}

String _deriveWorkflowEvidence(
  AutonomousWorkflowRecord workflow,
  List<SessionRecord> autonomousSessions,
) {
  final bits = <String>[
    '.codex/autonomous_resolution.json',
    if (workflow.loggedSessionLocalIds.isNotEmpty)
      'wake log sessions: ${workflow.loggedSessionLocalIds.map((id) => '#$id').join(', ')}',
    if (workflow.activeSessionLocalId > 0)
      'active: #${workflow.activeSessionLocalId}',
    if (workflow.lastCompletedSessionLocalId > 0)
      'last completed: #${workflow.lastCompletedSessionLocalId}',
    if (workflow.lastHandoffSummary.trim().isNotEmpty)
      workflow.lastHandoffSummary.trim(),
  ];
  if (bits.length == 1 && autonomousSessions.isNotEmpty) {
    bits.add(
      'derived sessions: ${_sessionSpanForDisplayIds(autonomousSessions)}',
    );
  }
  return bits.join(' · ');
}

String _sessionHeadline(SessionRecord? session) {
  if (session == null) {
    return '';
  }
  return '#${session.localId} ${session.status} · ${summarizeTask(session.taskDescription)}';
}

String _latestActivityLabel({
  required int latestActivitySessionId,
  required int latestActivityLocalSessionId,
  required SessionRecord? latestSession,
  required AutonomousStatusRecord? explicitStatus,
}) {
  if (latestActivitySessionId == 0) {
    return '';
  }
  if (explicitStatus != null &&
      explicitStatus.sessionId >= (latestSession?.id ?? 0) &&
      explicitStatus.sessionId > 0) {
    final local = latestActivityLocalSessionId > 0
        ? latestActivityLocalSessionId
        : explicitStatus.sessionId;
    return 'status #$local · ${explicitStatus.state}';
  }
  if (latestSession == null) {
    return 'status #$latestActivityLocalSessionId';
  }
  return '#${latestSession.localId} ${latestSession.status} · ${summarizeTask(latestSession.taskDescription)}';
}

int _localSessionIdForGlobal(
  int globalSessionId,
  List<SessionRecord> sessions,
) {
  if (globalSessionId <= 0) {
    return 0;
  }
  for (final session in sessions) {
    if (session.id == globalSessionId) {
      return session.localId;
    }
  }
  return globalSessionId;
}

String _sessionSpanForDisplayIds(List<SessionRecord> sessions) {
  if (sessions.isEmpty) {
    return '';
  }
  final first = sessions.first.localId;
  final last = sessions.last.localId;
  return first == last ? '#$first' : '#$first-#$last';
}

String _sessionSpanForGlobalIds(List<SessionRecord> sessions) {
  if (sessions.isEmpty) {
    return '';
  }
  final first = sessions.first.id;
  final last = sessions.last.id;
  return first == last ? '#$first' : '#$first-#$last';
}

SessionRecord? _lastMatching(
  List<SessionRecord> sessions,
  bool Function(SessionRecord session) test,
) {
  for (var index = sessions.length - 1; index >= 0; index--) {
    final session = sessions[index];
    if (test(session)) {
      return session;
    }
  }
  return null;
}

SessionRecord? _firstMatching(
  List<SessionRecord> sessions,
  bool Function(SessionRecord session) test,
) {
  for (final session in sessions) {
    if (test(session)) {
      return session;
    }
  }
  return null;
}

extension _FirstWhereOrNullExtension<T> on Iterable<T> {
  T? firstWhereOrNull(bool Function(T value) test) {
    for (final value in this) {
      if (test(value)) {
        return value;
      }
    }
    return null;
  }
}
