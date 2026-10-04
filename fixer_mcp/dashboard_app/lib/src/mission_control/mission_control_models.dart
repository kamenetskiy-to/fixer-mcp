class MissionControlWavesSnapshot {
  const MissionControlWavesSnapshot({
    required this.projectId,
    required this.generatedAt,
    required this.freshness,
    required this.plannedWaves,
    required this.waves,
  });

  factory MissionControlWavesSnapshot.fromJson(Map<String, dynamic> json) {
    return MissionControlWavesSnapshot(
      projectId: _asInt(json['project_id']),
      generatedAt: _asString(json['generated_at']),
      freshness: MissionControlFreshness.fromJson(_asMap(json['freshness'])),
      plannedWaves: List.unmodifiable(
        _asList(json['planned_waves']).map(MissionControlPlannedWave.fromJson),
      ),
      waves: List.unmodifiable(
        _asList(json['waves']).map(MissionControlWave.fromJson),
      ),
    );
  }

  final int projectId;
  final String generatedAt;
  final MissionControlFreshness freshness;
  final List<MissionControlPlannedWave> plannedWaves;
  final List<MissionControlWave> waves;
}

class MissionControlFreshness {
  const MissionControlFreshness({
    required this.state,
    required this.stale,
    required this.sourceUpdatedAt,
    required this.ageSeconds,
    required this.staleAfterSeconds,
    required this.reason,
  });

  factory MissionControlFreshness.fromJson(Map<String, dynamic> json) {
    return MissionControlFreshness(
      state: _asString(json['state'], fallback: 'unknown'),
      stale: _asBool(json['stale']),
      sourceUpdatedAt: _asString(json['source_updated_at']),
      ageSeconds: _asInt(json['age_seconds']),
      staleAfterSeconds: _asInt(json['stale_after_seconds']),
      reason: _asString(json['reason']),
    );
  }

  final String state;
  final bool stale;
  final String sourceUpdatedAt;
  final int ageSeconds;
  final int staleAfterSeconds;
  final String reason;
}

class MissionControlPlannedWave {
  const MissionControlPlannedWave({
    required this.planId,
    required this.title,
    required this.status,
    required this.operatorState,
    required this.label,
    required this.nextAction,
    required this.reason,
    required this.baseRef,
    required this.worktreeRoot,
    required this.initializedWaveId,
    required this.failureReason,
    required this.createdAt,
    required this.updatedAt,
    required this.initializedAt,
    required this.backend,
    required this.model,
    required this.reasoning,
    required this.mcpServers,
    required this.validationErrors,
    required this.taskCounts,
    required this.tasks,
    required this.actionCapabilities,
  });

  factory MissionControlPlannedWave.fromJson(Map<String, dynamic> json) {
    final capabilities = _asMap(json['action_capabilities']);
    return MissionControlPlannedWave(
      planId: _asInt(json['plan_id']),
      title: _asString(json['title'], fallback: 'Untitled planned wave'),
      status: _asString(json['status'], fallback: 'planned'),
      operatorState: _asString(json['operator_state'], fallback: 'planned'),
      label: _asString(json['label'], fallback: 'Planned'),
      nextAction: _asString(json['next_action'], fallback: 'initialize'),
      reason: _asString(json['reason']),
      baseRef: _asString(json['base_ref']),
      worktreeRoot: _asString(json['worktree_root']),
      initializedWaveId: _asInt(json['initialized_wave_id']),
      failureReason: _asString(json['failure_reason']),
      createdAt: _asString(json['created_at']),
      updatedAt: _asString(json['updated_at']),
      initializedAt: _asString(json['initialized_at']),
      backend: _asString(json['backend'] ?? json['cli_backend']),
      model: _asString(json['model'] ?? json['cli_model']),
      reasoning: _asString(json['reasoning'] ?? json['cli_reasoning']),
      mcpServers: _asStringList(
        json['mcp_servers'] ?? json['mcp_server_names'],
      ),
      validationErrors: _asStringList(json['validation_errors']),
      taskCounts: MissionControlPlannedWaveCounts.fromJson(
        _asMap(json['task_counts']),
      ),
      tasks: List.unmodifiable(
        _asList(json['tasks']).map(MissionControlPlannedWaveTask.fromJson),
      ),
      actionCapabilities: MissionControlPlannedWaveActionCapabilities.fromJson(
        capabilities,
      ),
    );
  }

  final int planId;
  final String title;
  final String status;
  final String operatorState;
  final String label;
  final String nextAction;
  final String reason;
  final String baseRef;
  final String worktreeRoot;
  final int initializedWaveId;
  final String failureReason;
  final String createdAt;
  final String updatedAt;
  final String initializedAt;
  final String backend;
  final String model;
  final String reasoning;
  final List<String> mcpServers;
  final List<String> validationErrors;
  final MissionControlPlannedWaveCounts taskCounts;
  final List<MissionControlPlannedWaveTask> tasks;
  final MissionControlPlannedWaveActionCapabilities actionCapabilities;

  bool get isInitialized => initializedWaveId > 0 || status == 'initialized';

  bool get needsArchitect =>
      operatorState == 'initialization_failed' || readinessErrors.isNotEmpty;

  String get readinessState {
    if (isInitialized) return 'materialized';
    if (status == 'initializing') return 'initializing';
    if (readinessErrors.isNotEmpty) return 'blocked';
    if (actionCapabilities.initialize.enabled) return 'ready';
    return 'unavailable';
  }

  List<String> get readinessErrors {
    return List.unmodifiable({
      ...validationErrors.where((error) => error.isNotEmpty),
      if (failureReason.isNotEmpty) failureReason,
    });
  }
}

class MissionControlPlannedWaveCounts {
  const MissionControlPlannedWaveCounts({
    required this.total,
    required this.planned,
    required this.materialized,
  });

  factory MissionControlPlannedWaveCounts.fromJson(Map<String, dynamic> json) {
    return MissionControlPlannedWaveCounts(
      total: _asInt(json['total']),
      planned: _asInt(json['planned']),
      materialized: _asInt(json['materialized']),
    );
  }

  final int total;
  final int planned;
  final int materialized;
}

class MissionControlPlannedWaveTask {
  const MissionControlPlannedWaveTask({
    required this.taskId,
    required this.key,
    required this.position,
    required this.taskDescription,
    required this.dependsOn,
    required this.materializedSessionId,
    required this.localSessionId,
    required this.backend,
    required this.model,
    required this.reasoning,
    required this.mcpServers,
  });

  factory MissionControlPlannedWaveTask.fromJson(Map<String, dynamic> json) {
    return MissionControlPlannedWaveTask(
      taskId: _asInt(json['task_id']),
      key: _asString(json['key'], fallback: 'task'),
      position: _asInt(json['position']),
      taskDescription: _asString(json['task_description']),
      dependsOn: _asStringList(json['depends_on']),
      materializedSessionId: _asInt(json['materialized_session_id']),
      localSessionId: _asInt(json['local_session_id']),
      backend: _asString(json['backend'] ?? json['cli_backend']),
      model: _asString(json['model'] ?? json['cli_model']),
      reasoning: _asString(json['reasoning'] ?? json['cli_reasoning']),
      mcpServers: _asStringList(
        json['mcp_servers'] ?? json['mcp_server_names'],
      ),
    );
  }

  final int taskId;
  final String key;
  final int position;
  final String taskDescription;
  final List<String> dependsOn;
  final int materializedSessionId;
  final int localSessionId;
  final String backend;
  final String model;
  final String reasoning;
  final List<String> mcpServers;
}

class MissionControlPlannedWaveActionCapabilities {
  const MissionControlPlannedWaveActionCapabilities(this.values);

  static const actionOrder = <String>['initialize', 'launch'];

  factory MissionControlPlannedWaveActionCapabilities.fromJson(
    Map<String, dynamic> json,
  ) {
    return MissionControlPlannedWaveActionCapabilities(
      List.unmodifiable(
        actionOrder.map(
          (action) => MissionControlActionCapability.fromJson(
            action,
            _asMap(json[action]),
          ),
        ),
      ),
    );
  }

  final List<MissionControlActionCapability> values;

  MissionControlActionCapability get initialize => values.first;
}

class MissionControlWave {
  const MissionControlWave({
    required this.waveId,
    required this.phase,
    required this.legacyStatus,
    required this.operatorState,
    required this.label,
    required this.nextAction,
    required this.gateState,
    required this.controlState,
    required this.controlReason,
    required this.failurePolicyState,
    required this.failureReason,
    required this.createdAt,
    required this.updatedAt,
    required this.launchedAt,
    required this.completedAt,
    required this.workerCounts,
    required this.review,
    required this.acceptance,
    required this.repair,
    required this.workers,
    required this.actionCapabilities,
  });

  factory MissionControlWave.fromJson(Map<String, dynamic> json) {
    return MissionControlWave(
      waveId: _asInt(json['wave_id']),
      phase: _asString(json['phase'], fallback: 'unknown'),
      legacyStatus: _asString(json['legacy_status']),
      operatorState: _asString(json['operator_state'], fallback: 'unknown'),
      label: _asString(json['label'], fallback: 'Wave state unavailable'),
      nextAction: _asString(json['next_action'], fallback: 'inspect'),
      gateState: _asString(json['gate_state'], fallback: 'unknown'),
      controlState: _asString(json['control_state'], fallback: 'unknown'),
      controlReason: _asString(json['control_reason']),
      failurePolicyState: _asString(
        json['failure_policy_state'],
        fallback: 'unknown',
      ),
      failureReason: _asString(json['failure_reason']),
      createdAt: _asString(json['created_at']),
      updatedAt: _asString(json['updated_at']),
      launchedAt: _asString(json['launched_at']),
      completedAt: _asString(json['completed_at']),
      workerCounts: MissionControlWorkerCounts.fromJson(
        _asMap(json['worker_counts']),
      ),
      review: MissionControlLinkedSession.fromJson(_asMap(json['review'])),
      acceptance: MissionControlLinkedSession.fromJson(
        _asMap(json['acceptance']),
      ),
      repair: MissionControlRepair.fromJson(_asMap(json['repair'])),
      workers: List.unmodifiable(
        _asList(json['workers']).map(MissionControlWorker.fromJson),
      ),
      actionCapabilities: MissionControlActionCapabilities.fromJson(
        _asMap(json['action_capabilities']),
      ),
    );
  }

  final int waveId;
  final String phase;
  final String legacyStatus;
  final String operatorState;
  final String label;
  final String nextAction;
  final String gateState;
  final String controlState;
  final String controlReason;
  final String failurePolicyState;
  final String failureReason;
  final String createdAt;
  final String updatedAt;
  final String launchedAt;
  final String completedAt;
  final MissionControlWorkerCounts workerCounts;
  final MissionControlLinkedSession review;
  final MissionControlLinkedSession acceptance;
  final MissionControlRepair repair;
  final List<MissionControlWorker> workers;
  final MissionControlActionCapabilities actionCapabilities;

  bool get needsArchitect {
    return operatorState == 'repair_blocked' ||
        controlState == 'paused_for_architect' ||
        failurePolicyState == 'paused_for_architect' ||
        nextAction == 'authorize_repair' ||
        nextAction.contains('architect');
  }

  bool get isCompleted => operatorState == 'completed' || phase == 'completed';

  bool get isReviewState {
    return operatorState.contains('review') ||
        gateState.contains('review') ||
        phase == 'acceptance';
  }

  bool get isActiveState {
    final hasRunningWorkers = workerCounts.active > 0;
    final waveCanStillBeRunning =
        legacyStatus == 'running' || legacyStatus == 'partially_failed';
    return hasRunningWorkers && waveCanStillBeRunning;
  }
}

class MissionControlWorkerCounts {
  const MissionControlWorkerCounts({
    required this.total,
    required this.active,
    required this.terminal,
    required this.reviewReady,
    required this.completed,
    required this.failed,
    required this.stopped,
    required this.staleEpoch,
    required this.blocked,
    required this.retryPending,
  });

  factory MissionControlWorkerCounts.fromJson(Map<String, dynamic> json) {
    return MissionControlWorkerCounts(
      total: _asInt(json['total']),
      active: _asInt(json['active']),
      terminal: _asInt(json['terminal']),
      reviewReady: _asInt(json['review_ready']),
      completed: _asInt(json['completed']),
      failed: _asInt(json['failed']),
      stopped: _asInt(json['stopped']),
      staleEpoch: _asInt(json['stale_epoch']),
      blocked: _asInt(json['blocked']),
      retryPending: _asInt(json['retry_pending']),
    );
  }

  final int total;
  final int active;
  final int terminal;
  final int reviewReady;
  final int completed;
  final int failed;
  final int stopped;
  final int staleEpoch;
  final int blocked;
  final int retryPending;
}

class MissionControlLinkedSession {
  const MissionControlLinkedSession({
    required this.sessionId,
    required this.localSessionId,
    required this.state,
  });

  factory MissionControlLinkedSession.fromJson(Map<String, dynamic> json) {
    return MissionControlLinkedSession(
      sessionId: _asInt(json['session_id']),
      localSessionId: _asInt(json['local_session_id']),
      state: _asString(json['state'], fallback: 'not_started'),
    );
  }

  final int sessionId;
  final int localSessionId;
  final String state;
}

class MissionControlRepair {
  const MissionControlRepair({
    required this.state,
    required this.workerId,
    required this.sessionId,
    required this.localSessionId,
    required this.attemptCount,
  });

  factory MissionControlRepair.fromJson(Map<String, dynamic> json) {
    return MissionControlRepair(
      state: _asString(json['state'], fallback: 'none'),
      workerId: _asInt(json['worker_id']),
      sessionId: _asInt(json['session_id']),
      localSessionId: _asInt(json['local_session_id']),
      attemptCount: _asInt(json['attempt_count']),
    );
  }

  final String state;
  final int workerId;
  final int sessionId;
  final int localSessionId;
  final int attemptCount;
}

class MissionControlWorker {
  const MissionControlWorker({
    required this.workerId,
    required this.sessionId,
    required this.localSessionId,
    required this.status,
    required this.sessionStatus,
    required this.backend,
    required this.model,
    required this.reasoning,
    required this.outcome,
    required this.failureReason,
    required this.retryPending,
    required this.updatedAt,
  });

  factory MissionControlWorker.fromJson(Map<String, dynamic> json) {
    return MissionControlWorker(
      workerId: _asInt(json['worker_id']),
      sessionId: _asInt(json['session_id']),
      localSessionId: _asInt(json['local_session_id']),
      status: _asString(json['status'], fallback: 'unknown'),
      sessionStatus: _asString(json['session_status']),
      backend: _asString(json['backend']),
      model: _asString(json['model']),
      reasoning: _asString(json['reasoning']),
      outcome: _asString(json['outcome']),
      failureReason: _asString(json['failure_reason']),
      retryPending: _asBool(json['retry_pending']),
      updatedAt: _asString(json['updated_at']),
    );
  }

  final int workerId;
  final int sessionId;
  final int localSessionId;
  final String status;
  final String sessionStatus;
  final String backend;
  final String model;
  final String reasoning;
  final String outcome;
  final String failureReason;
  final bool retryPending;
  final String updatedAt;
}

class MissionControlActionCapability {
  const MissionControlActionCapability({
    required this.action,
    required this.enabled,
    required this.disabledReason,
  });

  factory MissionControlActionCapability.fromJson(
    String action,
    Map<String, dynamic> json,
  ) {
    final enabled = _asBool(json['enabled']);
    return MissionControlActionCapability(
      action: action,
      enabled: enabled,
      disabledReason: _asString(
        json['disabled_reason'],
        fallback: enabled ? '' : 'Capability is not available.',
      ),
    );
  }

  final String action;
  final bool enabled;
  final String disabledReason;
}

class MissionControlActionCapabilities {
  const MissionControlActionCapabilities(this.values);

  static const actionOrder = <String>[
    'launch',
    'wait',
    'authorize_repair',
    'pause',
    'resume',
    'transition_to_acceptance',
    'complete',
    'cleanup',
  ];

  factory MissionControlActionCapabilities.fromJson(Map<String, dynamic> json) {
    return MissionControlActionCapabilities(
      List.unmodifiable(
        actionOrder.map(
          (action) => MissionControlActionCapability.fromJson(
            action,
            _asMap(json[action]),
          ),
        ),
      ),
    );
  }

  final List<MissionControlActionCapability> values;
}

Map<String, dynamic> _asMap(dynamic value) {
  if (value is Map<String, dynamic>) return value;
  if (value is Map) return Map<String, dynamic>.from(value);
  return const <String, dynamic>{};
}

List<Map<String, dynamic>> _asList(dynamic value) {
  if (value is! List) return const <Map<String, dynamic>>[];
  return value
      .whereType<Map>()
      .map((item) => Map<String, dynamic>.from(item))
      .toList(growable: false);
}

String _asString(dynamic value, {String fallback = ''}) {
  final text = value?.toString().trim() ?? '';
  return text.isEmpty ? fallback : text;
}

int _asInt(dynamic value) {
  if (value is int) return value;
  if (value is num) return value.toInt();
  return int.tryParse(value?.toString() ?? '') ?? 0;
}

bool _asBool(dynamic value) {
  if (value is bool) return value;
  if (value is num) return value != 0;
  return value?.toString().toLowerCase() == 'true';
}

List<String> _asStringList(dynamic value) {
  if (value is! List) return const <String>[];
  return value
      .map((item) {
        if (item is Map) {
          final map = Map<String, dynamic>.from(item);
          return _asString(map['message'] ?? map['error'] ?? map['reason']);
        }
        return _asString(item);
      })
      .where((item) => item.isNotEmpty)
      .toList(growable: false);
}
