class NetrunnerExplorerSnapshot {
  const NetrunnerExplorerSnapshot({
    required this.waveGroups,
    required this.ungroupedSessions,
  });

  final List<NetrunnerWaveGroupRecord> waveGroups;
  final List<NetrunnerExplorerRecord> ungroupedSessions;

  factory NetrunnerExplorerSnapshot.fromJson(Map<String, dynamic> json) {
    final groups = _asList(
      json['wave_groups'],
      NetrunnerWaveGroupRecord.fromJson,
    )..sort(NetrunnerWaveGroupRecord.compareNewestFirst);
    final ungrouped = _asList(
      json['ungrouped_sessions'],
      NetrunnerExplorerRecord.fromJson,
    )..sort(NetrunnerExplorerRecord.compareNewestFirst);
    return NetrunnerExplorerSnapshot(
      waveGroups: List.unmodifiable(groups),
      ungroupedSessions: List.unmodifiable(ungrouped),
    );
  }
}

class NetrunnerWaveGroupRecord {
  const NetrunnerWaveGroupRecord({
    required this.waveId,
    required this.waveIdentity,
    required this.status,
    required this.createdAt,
    required this.updatedAt,
    required this.launchedAt,
    required this.completedAt,
    required this.workerCount,
    required this.reviewerCount,
    required this.manualCount,
    required this.sessions,
  });

  final int waveId;
  final String waveIdentity;
  final String status;
  final String createdAt;
  final String updatedAt;
  final String launchedAt;
  final String completedAt;
  final int workerCount;
  final int reviewerCount;
  final int manualCount;
  final List<NetrunnerExplorerRecord> sessions;

  factory NetrunnerWaveGroupRecord.fromJson(Map<String, dynamic> json) {
    final sessions = _asList(json['sessions'], NetrunnerExplorerRecord.fromJson)
      ..sort(NetrunnerExplorerRecord.compareNewestFirst);
    return NetrunnerWaveGroupRecord(
      waveId: _asInt(json['wave_id']),
      waveIdentity: _asString(json['wave_identity']),
      status: _asString(json['status']),
      createdAt: _asString(json['created_at']),
      updatedAt: _asString(json['updated_at']),
      launchedAt: _asString(json['launched_at']),
      completedAt: _asString(json['completed_at']),
      workerCount: _asInt(json['worker_count']),
      reviewerCount: _asInt(json['reviewer_count']),
      manualCount: _asInt(json['manual_count']),
      sessions: List.unmodifiable(sessions),
    );
  }

  static int compareNewestFirst(
    NetrunnerWaveGroupRecord a,
    NetrunnerWaveGroupRecord b,
  ) {
    final createdOrder = b.createdAt.compareTo(a.createdAt);
    return createdOrder == 0 ? b.waveId.compareTo(a.waveId) : createdOrder;
  }
}

class NetrunnerExplorerRecord {
  const NetrunnerExplorerRecord({
    required this.id,
    required this.localId,
    required this.projectId,
    required this.waveId,
    required this.role,
    required this.kind,
    required this.headline,
    required this.taskPreview,
    required this.status,
    required this.membershipStatus,
    required this.backend,
    required this.model,
    required this.reasoning,
    required this.createdAt,
    required this.updatedAt,
    required this.launchedAt,
    required this.completedAt,
  });

  final int id;
  final int localId;
  final int projectId;
  final int waveId;
  final String role;
  final String kind;
  final String headline;
  final String taskPreview;
  final String status;
  final String membershipStatus;
  final String backend;
  final String model;
  final String reasoning;
  final String createdAt;
  final String updatedAt;
  final String launchedAt;
  final String completedAt;

  bool get hasLaunchDetails => launchedAt.isNotEmpty && backend.isNotEmpty;

  factory NetrunnerExplorerRecord.fromJson(Map<String, dynamic> json) {
    return NetrunnerExplorerRecord(
      id: _asInt(json['id']),
      localId: _asInt(json['local_id']),
      projectId: _asInt(json['project_id']),
      waveId: _asInt(json['wave_id']),
      role: _asString(json['role']),
      kind: _asString(json['kind']),
      headline: _asString(json['headline']),
      taskPreview: _asString(json['task_preview']),
      status: _asString(json['status']),
      membershipStatus: _asString(json['membership_status']),
      backend: _asString(json['backend']),
      model: _asString(json['model']),
      reasoning: _asString(json['reasoning']),
      createdAt: _asString(json['created_at']),
      updatedAt: _asString(json['updated_at']),
      launchedAt: _asString(json['launched_at']),
      completedAt: _asString(json['completed_at']),
    );
  }

  static int compareNewestFirst(
    NetrunnerExplorerRecord a,
    NetrunnerExplorerRecord b,
  ) => b.id.compareTo(a.id);
}

List<T> _asList<T>(Object? value, T Function(Map<String, dynamic>) decode) {
  if (value is! List) return <T>[];
  return value
      .whereType<Map>()
      .map((item) => decode(Map<String, dynamic>.from(item)))
      .toList();
}

String _asString(Object? value) => value?.toString() ?? '';

int _asInt(Object? value) => switch (value) {
  int number => number,
  num number => number.toInt(),
  String text => int.tryParse(text) ?? 0,
  _ => 0,
};
