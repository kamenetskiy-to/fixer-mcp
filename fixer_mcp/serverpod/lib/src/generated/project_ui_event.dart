/* AUTOMATICALLY GENERATED CODE DO NOT MODIFY */
/*   To generate run: "serverpod generate"    */

// ignore_for_file: implementation_imports
// ignore_for_file: library_private_types_in_public_api
// ignore_for_file: non_constant_identifier_names
// ignore_for_file: public_member_api_docs
// ignore_for_file: type_literal_in_constant_pattern
// ignore_for_file: use_super_parameters
// ignore_for_file: invalid_use_of_internal_member

// ignore_for_file: no_leading_underscores_for_library_prefixes
import 'package:serverpod/serverpod.dart' as _i1;

/// One immutable event from the authoritative per-project SQLite journal.
@_i1.immutable
abstract class ProjectUiEvent
    implements _i1.SerializableModel, _i1.ProtocolSerialization {
  const ProjectUiEvent._({
    required this.projectId,
    required this.seq,
    required this.eventId,
    required this.schemaVersion,
    required this.kind,
    required this.aggregateType,
    required this.aggregateId,
    required this.aggregateRevision,
    required this.payloadJson,
    required this.actorKind,
    required this.actorId,
    required this.causationId,
    required this.correlationId,
    required this.createdAt,
  });

  const factory ProjectUiEvent({
    required int projectId,
    required int seq,
    required String eventId,
    required int schemaVersion,
    required String kind,
    required String aggregateType,
    required String aggregateId,
    required int aggregateRevision,
    required String payloadJson,
    required String actorKind,
    required String actorId,
    required String causationId,
    required String correlationId,
    required String createdAt,
  }) = _ProjectUiEventImpl;

  factory ProjectUiEvent.fromJson(Map<String, dynamic> jsonSerialization) {
    return ProjectUiEvent(
      projectId: jsonSerialization['project_id'] as int,
      seq: jsonSerialization['seq'] as int,
      eventId: jsonSerialization['event_id'] as String,
      schemaVersion: jsonSerialization['schema_version'] as int,
      kind: jsonSerialization['kind'] as String,
      aggregateType: jsonSerialization['aggregate_type'] as String,
      aggregateId: jsonSerialization['aggregate_id'] as String,
      aggregateRevision: jsonSerialization['aggregate_revision'] as int,
      payloadJson: jsonSerialization['payload'] as String,
      actorKind: jsonSerialization['actor_kind'] as String,
      actorId: jsonSerialization['actor_id'] as String,
      causationId: jsonSerialization['causation_id'] as String,
      correlationId: jsonSerialization['correlation_id'] as String,
      createdAt: jsonSerialization['created_at'] as String,
    );
  }

  final int projectId;

  final int seq;

  final String eventId;

  final int schemaVersion;

  final String kind;

  final String aggregateType;

  final String aggregateId;

  final int aggregateRevision;

  final String payloadJson;

  final String actorKind;

  final String actorId;

  final String causationId;

  final String correlationId;

  final String createdAt;

  /// Returns a shallow copy of this [ProjectUiEvent]
  /// with some or all fields replaced by the given arguments.
  @_i1.useResult
  ProjectUiEvent copyWith({
    int? projectId,
    int? seq,
    String? eventId,
    int? schemaVersion,
    String? kind,
    String? aggregateType,
    String? aggregateId,
    int? aggregateRevision,
    String? payloadJson,
    String? actorKind,
    String? actorId,
    String? causationId,
    String? correlationId,
    String? createdAt,
  });
  @override
  bool operator ==(Object other) {
    return identical(
          other,
          this,
        ) ||
        other.runtimeType == runtimeType &&
            other is ProjectUiEvent &&
            (identical(
                  other.projectId,
                  projectId,
                ) ||
                other.projectId == projectId) &&
            (identical(
                  other.seq,
                  seq,
                ) ||
                other.seq == seq) &&
            (identical(
                  other.eventId,
                  eventId,
                ) ||
                other.eventId == eventId) &&
            (identical(
                  other.schemaVersion,
                  schemaVersion,
                ) ||
                other.schemaVersion == schemaVersion) &&
            (identical(
                  other.kind,
                  kind,
                ) ||
                other.kind == kind) &&
            (identical(
                  other.aggregateType,
                  aggregateType,
                ) ||
                other.aggregateType == aggregateType) &&
            (identical(
                  other.aggregateId,
                  aggregateId,
                ) ||
                other.aggregateId == aggregateId) &&
            (identical(
                  other.aggregateRevision,
                  aggregateRevision,
                ) ||
                other.aggregateRevision == aggregateRevision) &&
            (identical(
                  other.payloadJson,
                  payloadJson,
                ) ||
                other.payloadJson == payloadJson) &&
            (identical(
                  other.actorKind,
                  actorKind,
                ) ||
                other.actorKind == actorKind) &&
            (identical(
                  other.actorId,
                  actorId,
                ) ||
                other.actorId == actorId) &&
            (identical(
                  other.causationId,
                  causationId,
                ) ||
                other.causationId == causationId) &&
            (identical(
                  other.correlationId,
                  correlationId,
                ) ||
                other.correlationId == correlationId) &&
            (identical(
                  other.createdAt,
                  createdAt,
                ) ||
                other.createdAt == createdAt);
  }

  @override
  int get hashCode {
    return Object.hash(
      runtimeType,
      projectId,
      seq,
      eventId,
      schemaVersion,
      kind,
      aggregateType,
      aggregateId,
      aggregateRevision,
      payloadJson,
      actorKind,
      actorId,
      causationId,
      correlationId,
      createdAt,
    );
  }

  @override
  Map<String, dynamic> toJson() {
    return {
      '__className__': 'ProjectUiEvent',
      'project_id': projectId,
      'seq': seq,
      'event_id': eventId,
      'schema_version': schemaVersion,
      'kind': kind,
      'aggregate_type': aggregateType,
      'aggregate_id': aggregateId,
      'aggregate_revision': aggregateRevision,
      'payload': payloadJson,
      'actor_kind': actorKind,
      'actor_id': actorId,
      'causation_id': causationId,
      'correlation_id': correlationId,
      'created_at': createdAt,
    };
  }

  @override
  Map<String, dynamic> toJsonForProtocol() {
    return {
      '__className__': 'ProjectUiEvent',
      'project_id': projectId,
      'seq': seq,
      'event_id': eventId,
      'schema_version': schemaVersion,
      'kind': kind,
      'aggregate_type': aggregateType,
      'aggregate_id': aggregateId,
      'aggregate_revision': aggregateRevision,
      'payload': payloadJson,
      'actor_kind': actorKind,
      'actor_id': actorId,
      'causation_id': causationId,
      'correlation_id': correlationId,
      'created_at': createdAt,
    };
  }

  @override
  String toString() {
    return _i1.SerializationManager.encode(this);
  }
}

class _ProjectUiEventImpl extends ProjectUiEvent {
  const _ProjectUiEventImpl({
    required int projectId,
    required int seq,
    required String eventId,
    required int schemaVersion,
    required String kind,
    required String aggregateType,
    required String aggregateId,
    required int aggregateRevision,
    required String payloadJson,
    required String actorKind,
    required String actorId,
    required String causationId,
    required String correlationId,
    required String createdAt,
  }) : super._(
         projectId: projectId,
         seq: seq,
         eventId: eventId,
         schemaVersion: schemaVersion,
         kind: kind,
         aggregateType: aggregateType,
         aggregateId: aggregateId,
         aggregateRevision: aggregateRevision,
         payloadJson: payloadJson,
         actorKind: actorKind,
         actorId: actorId,
         causationId: causationId,
         correlationId: correlationId,
         createdAt: createdAt,
       );

  /// Returns a shallow copy of this [ProjectUiEvent]
  /// with some or all fields replaced by the given arguments.
  @_i1.useResult
  @override
  ProjectUiEvent copyWith({
    int? projectId,
    int? seq,
    String? eventId,
    int? schemaVersion,
    String? kind,
    String? aggregateType,
    String? aggregateId,
    int? aggregateRevision,
    String? payloadJson,
    String? actorKind,
    String? actorId,
    String? causationId,
    String? correlationId,
    String? createdAt,
  }) {
    return ProjectUiEvent(
      projectId: projectId ?? this.projectId,
      seq: seq ?? this.seq,
      eventId: eventId ?? this.eventId,
      schemaVersion: schemaVersion ?? this.schemaVersion,
      kind: kind ?? this.kind,
      aggregateType: aggregateType ?? this.aggregateType,
      aggregateId: aggregateId ?? this.aggregateId,
      aggregateRevision: aggregateRevision ?? this.aggregateRevision,
      payloadJson: payloadJson ?? this.payloadJson,
      actorKind: actorKind ?? this.actorKind,
      actorId: actorId ?? this.actorId,
      causationId: causationId ?? this.causationId,
      correlationId: correlationId ?? this.correlationId,
      createdAt: createdAt ?? this.createdAt,
    );
  }
}
