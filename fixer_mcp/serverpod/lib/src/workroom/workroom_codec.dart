import 'dart:convert';

import 'package:fixer_dashboard_server/src/generated/protocol.dart';

import 'workroom_bridge_client.dart';

class WorkroomJournalBatch {
  const WorkroomJournalBatch({
    required this.projectId,
    required this.afterSeq,
    required this.headSeq,
    required this.events,
    required this.timedOut,
  });

  final int projectId;
  final int afterSeq;
  final int headSeq;
  final List<ProjectUiEvent> events;
  final bool timedOut;
}

ProjectWorkroomSnapshot decodeProjectWorkroomSnapshot(
  Map<String, dynamic> json,
) {
  final surfaceJson = _optionalObject(json, 'active_surface');
  final activeInstructionJson = _optionalObject(json, 'active_instruction');
  return ProjectWorkroomSnapshot(
    projectId: _requiredInt(json, 'project_id'),
    projectName: _requiredString(json, 'project_name'),
    projectCwd: _requiredString(json, 'project_cwd'),
    protocolVersion: _requiredInt(json, 'protocol_version'),
    watermarkSeq: _requiredInt(json, 'watermark_seq'),
    threads: _objectList(json, 'threads').map(_decodeFixerThread).toList(),
    selectedThreadId: _optionalString(json, 'selected_thread_id'),
    turns: _objectList(json, 'turns').map(_decodeFixerTurn).toList(),
    activeSurface: surfaceJson == null ? null : _decodeSurface(surfaceJson),
    handsActorId: _requiredString(json, 'hands_actor_id'),
    handsDisplayName: _requiredString(json, 'hands_display_name'),
    handsAuthorityState: _requiredString(json, 'hands_authority_state'),
    handsDefaultLane: _requiredString(json, 'hands_default_lane'),
    handsLanes: _objectList(json, 'hands_lanes').map(_decodeHandsLane).toList(),
    handsMailbox: _objectList(
      json,
      'hands_mailbox',
    ).map(_decodeHandsInstruction).toList(),
    activeInstruction: activeInstructionJson == null
        ? null
        : _decodeHandsInstruction(activeInstructionJson),
    capabilities: _stringList(json, 'capabilities'),
  );
}

WorkroomJournalBatch decodeWorkroomJournalBatch(Map<String, dynamic> json) {
  return WorkroomJournalBatch(
    projectId: _requiredInt(json, 'project_id'),
    afterSeq: _requiredInt(json, 'after_seq'),
    headSeq: _requiredInt(json, 'head_seq'),
    events: _objectList(json, 'events').map(decodeProjectUiEvent).toList(),
    timedOut: _requiredBool(json, 'timed_out'),
  );
}

ProjectUiEvent decodeProjectUiEvent(Map<String, dynamic> json) {
  return ProjectUiEvent(
    projectId: _requiredInt(json, 'project_id'),
    seq: _requiredInt(json, 'seq'),
    eventId: _requiredString(json, 'event_id'),
    schemaVersion: _requiredInt(json, 'schema_version'),
    kind: _requiredString(json, 'kind'),
    aggregateType: _requiredString(json, 'aggregate_type'),
    aggregateId: _requiredString(json, 'aggregate_id'),
    aggregateRevision: _requiredInt(json, 'aggregate_revision'),
    payloadJson: _opaqueJson(json['payload']),
    actorKind: _requiredString(json, 'actor_kind'),
    actorId: _requiredString(json, 'actor_id'),
    causationId: _requiredString(json, 'causation_id'),
    correlationId: _requiredString(json, 'correlation_id'),
    createdAt: _requiredString(json, 'created_at'),
  );
}

FixerTurnReceipt decodeFixerTurnReceipt(Map<String, dynamic> json) {
  return FixerTurnReceipt(
    status: _requiredString(json, 'status'),
    threadId: _requiredString(json, 'thread_id'),
    turnId: _requiredString(json, 'turn_id'),
    ordinal: _requiredInt(json, 'ordinal'),
    projectSeq: _requiredInt(json, 'project_seq'),
  );
}

GenuiActionReceipt decodeGenuiActionReceipt(Map<String, dynamic> json) {
  return GenuiActionReceipt(
    status: _requiredString(json, 'status'),
    invocationId: _requiredString(json, 'invocation_id'),
    actionId: _requiredString(json, 'action_id'),
    decision: _requiredString(json, 'decision'),
    reasonCode: _optionalString(json, 'reason_code'),
    projectSeq: _requiredInt(json, 'project_seq'),
  );
}

HandsInstructionReceipt decodeHandsInstructionReceipt(
  Map<String, dynamic> json,
) {
  return HandsInstructionReceipt(
    status: _requiredString(json, 'status'),
    instructionId: _requiredString(json, 'instruction_id'),
    ordinal: _requiredInt(json, 'ordinal'),
    state: _requiredString(json, 'state'),
    lane: _requiredString(json, 'lane'),
    riskClass: _requiredString(json, 'risk_class'),
    projectSeq: _requiredInt(json, 'project_seq'),
    reasonCode: _optionalString(json, 'reason_code'),
    reasonText: _optionalString(json, 'reason_text'),
  );
}

CommandReceipt decodeCommandReceipt(Map<String, dynamic> json) {
  return CommandReceipt(
    status: _requiredString(json, 'status'),
    instructionId: _optionalString(json, 'instruction_id'),
    state: _optionalString(json, 'state'),
    revision: _optionalInt(json, 'revision'),
    projectSeq: _requiredInt(json, 'project_seq'),
  );
}

WorkroomFixerThread _decodeFixerThread(Map<String, dynamic> json) {
  return WorkroomFixerThread(
    threadId: _requiredString(json, 'id'),
    provider: _requiredString(json, 'provider'),
    externalSessionId: _optionalString(json, 'external_session_id'),
    headline: _requiredString(json, 'headline'),
    state: _requiredString(json, 'state'),
    createdAt: _requiredString(json, 'created_at'),
    updatedAt: _requiredString(json, 'updated_at'),
  );
}

WorkroomFixerTurn _decodeFixerTurn(Map<String, dynamic> json) {
  return WorkroomFixerTurn(
    turnId: _requiredString(json, 'id'),
    threadId: _requiredString(json, 'thread_id'),
    ordinal: _requiredInt(json, 'ordinal'),
    role: _requiredString(json, 'role'),
    content: _requiredString(json, 'content'),
    source: _optionalString(json, 'source'),
    status: _requiredString(json, 'status'),
    clientMessageId: _optionalString(json, 'client_message_id'),
    providerTurnId: _optionalString(json, 'provider_turn_id'),
    createdAt: _requiredString(json, 'created_at'),
    completedAt: _optionalString(json, 'completed_at'),
  );
}

WorkroomSurface _decodeSurface(Map<String, dynamic> json) {
  return WorkroomSurface(
    surfaceId: _requiredString(json, 'id'),
    threadId: _requiredString(json, 'thread_id'),
    causedByTurnId: _requiredString(json, 'caused_by_turn_id'),
    surfaceType: _requiredString(json, 'surface_type'),
    surfaceVersion: _requiredInt(json, 'surface_version'),
    state: _requiredString(json, 'state'),
    currentRevision: _requiredInt(json, 'current_revision'),
    sourceSeq: _requiredInt(json, 'source_seq'),
    documentJson: _opaqueJson(json['document']),
    updatedAt: _requiredString(json, 'updated_at'),
  );
}

WorkroomHandsLane _decodeHandsLane(Map<String, dynamic> json) {
  return WorkroomHandsLane(
    provider: _requiredString(json, 'provider'),
    model: _requiredString(json, 'model'),
    reasoning: _requiredString(json, 'reasoning'),
  );
}

WorkroomHandsInstruction _decodeHandsInstruction(Map<String, dynamic> json) {
  return WorkroomHandsInstruction(
    instructionId: _requiredString(json, 'id'),
    ordinal: _requiredInt(json, 'ordinal'),
    instructionText: _requiredString(json, 'instruction_text'),
    declaredWriteScope: _stringList(json, 'declared_write_scope'),
    requestedLane: _requiredString(json, 'requested_lane'),
    riskClass: _requiredString(json, 'risk_class'),
    reviewPolicy: _requiredString(json, 'review_policy'),
    state: _requiredString(json, 'state'),
    stateReasonCode: _optionalString(json, 'state_reason_code'),
    stateReasonText: _optionalString(json, 'state_reason_text'),
    revision: _requiredInt(json, 'revision'),
    createdAt: _requiredString(json, 'created_at'),
    updatedAt: _requiredString(json, 'updated_at'),
    terminalAt: _optionalString(json, 'terminal_at'),
  );
}

List<Map<String, dynamic>> _objectList(Map<String, dynamic> json, String key) {
  final value = json[key];
  if (value is! List) {
    throw _invalidField(key);
  }
  return value.map((entry) {
    if (entry is! Map) {
      throw _invalidField(key);
    }
    return Map<String, dynamic>.from(entry);
  }).toList();
}

List<String> _stringList(Map<String, dynamic> json, String key) {
  final value = json[key];
  if (value is! List || value.any((entry) => entry is! String)) {
    throw _invalidField(key);
  }
  return value.cast<String>();
}

Map<String, dynamic>? _optionalObject(Map<String, dynamic> json, String key) {
  final value = json[key];
  if (value == null) return null;
  if (value is! Map) throw _invalidField(key);
  return Map<String, dynamic>.from(value);
}

String _requiredString(Map<String, dynamic> json, String key) {
  final value = json[key];
  if (value is! String) throw _invalidField(key);
  return value;
}

String? _optionalString(Map<String, dynamic> json, String key) {
  final value = json[key];
  if (value == null) return null;
  if (value is! String) throw _invalidField(key);
  return value.isEmpty ? null : value;
}

int _requiredInt(Map<String, dynamic> json, String key) {
  final value = json[key];
  if (value is! int) throw _invalidField(key);
  return value;
}

int? _optionalInt(Map<String, dynamic> json, String key) {
  final value = json[key];
  if (value == null) return null;
  if (value is! int) throw _invalidField(key);
  return value;
}

bool _requiredBool(Map<String, dynamic> json, String key) {
  final value = json[key];
  if (value is! bool) throw _invalidField(key);
  return value;
}

String _opaqueJson(dynamic value) {
  if (value == null) throw _invalidField('opaque_json');
  if (value is String) {
    try {
      jsonDecode(value);
      return value;
    } on FormatException {
      throw _invalidField('opaque_json');
    }
  }
  try {
    return jsonEncode(value);
  } on JsonUnsupportedObjectError {
    throw _invalidField('opaque_json');
  }
}

WorkroomBridgeFailure _invalidField(String field) {
  return WorkroomBridgeFailure(
    'invalid_bridge_response',
    'The private bridge returned an invalid $field field.',
  );
}
