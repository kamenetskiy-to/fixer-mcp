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

/// Active governed GenUI surface revision. The document stays opaque until the
/// Flutter schema validator accepts its versioned JSON payload.
@_i1.immutable
abstract class WorkroomSurface
    implements _i1.SerializableModel, _i1.ProtocolSerialization {
  const WorkroomSurface._({
    required this.surfaceId,
    required this.threadId,
    required this.causedByTurnId,
    required this.surfaceType,
    required this.surfaceVersion,
    required this.state,
    required this.currentRevision,
    required this.sourceSeq,
    required this.documentJson,
    required this.updatedAt,
  });

  const factory WorkroomSurface({
    required String surfaceId,
    required String threadId,
    required String causedByTurnId,
    required String surfaceType,
    required int surfaceVersion,
    required String state,
    required int currentRevision,
    required int sourceSeq,
    required String documentJson,
    required String updatedAt,
  }) = _WorkroomSurfaceImpl;

  factory WorkroomSurface.fromJson(Map<String, dynamic> jsonSerialization) {
    return WorkroomSurface(
      surfaceId: jsonSerialization['id'] as String,
      threadId: jsonSerialization['thread_id'] as String,
      causedByTurnId: jsonSerialization['caused_by_turn_id'] as String,
      surfaceType: jsonSerialization['surface_type'] as String,
      surfaceVersion: jsonSerialization['surface_version'] as int,
      state: jsonSerialization['state'] as String,
      currentRevision: jsonSerialization['current_revision'] as int,
      sourceSeq: jsonSerialization['source_seq'] as int,
      documentJson: jsonSerialization['document'] as String,
      updatedAt: jsonSerialization['updated_at'] as String,
    );
  }

  final String surfaceId;

  final String threadId;

  final String causedByTurnId;

  final String surfaceType;

  final int surfaceVersion;

  final String state;

  final int currentRevision;

  final int sourceSeq;

  final String documentJson;

  final String updatedAt;

  /// Returns a shallow copy of this [WorkroomSurface]
  /// with some or all fields replaced by the given arguments.
  @_i1.useResult
  WorkroomSurface copyWith({
    String? surfaceId,
    String? threadId,
    String? causedByTurnId,
    String? surfaceType,
    int? surfaceVersion,
    String? state,
    int? currentRevision,
    int? sourceSeq,
    String? documentJson,
    String? updatedAt,
  });
  @override
  bool operator ==(Object other) {
    return identical(
          other,
          this,
        ) ||
        other.runtimeType == runtimeType &&
            other is WorkroomSurface &&
            (identical(
                  other.surfaceId,
                  surfaceId,
                ) ||
                other.surfaceId == surfaceId) &&
            (identical(
                  other.threadId,
                  threadId,
                ) ||
                other.threadId == threadId) &&
            (identical(
                  other.causedByTurnId,
                  causedByTurnId,
                ) ||
                other.causedByTurnId == causedByTurnId) &&
            (identical(
                  other.surfaceType,
                  surfaceType,
                ) ||
                other.surfaceType == surfaceType) &&
            (identical(
                  other.surfaceVersion,
                  surfaceVersion,
                ) ||
                other.surfaceVersion == surfaceVersion) &&
            (identical(
                  other.state,
                  state,
                ) ||
                other.state == state) &&
            (identical(
                  other.currentRevision,
                  currentRevision,
                ) ||
                other.currentRevision == currentRevision) &&
            (identical(
                  other.sourceSeq,
                  sourceSeq,
                ) ||
                other.sourceSeq == sourceSeq) &&
            (identical(
                  other.documentJson,
                  documentJson,
                ) ||
                other.documentJson == documentJson) &&
            (identical(
                  other.updatedAt,
                  updatedAt,
                ) ||
                other.updatedAt == updatedAt);
  }

  @override
  int get hashCode {
    return Object.hash(
      runtimeType,
      surfaceId,
      threadId,
      causedByTurnId,
      surfaceType,
      surfaceVersion,
      state,
      currentRevision,
      sourceSeq,
      documentJson,
      updatedAt,
    );
  }

  @override
  Map<String, dynamic> toJson() {
    return {
      '__className__': 'WorkroomSurface',
      'id': surfaceId,
      'thread_id': threadId,
      'caused_by_turn_id': causedByTurnId,
      'surface_type': surfaceType,
      'surface_version': surfaceVersion,
      'state': state,
      'current_revision': currentRevision,
      'source_seq': sourceSeq,
      'document': documentJson,
      'updated_at': updatedAt,
    };
  }

  @override
  Map<String, dynamic> toJsonForProtocol() {
    return {
      '__className__': 'WorkroomSurface',
      'id': surfaceId,
      'thread_id': threadId,
      'caused_by_turn_id': causedByTurnId,
      'surface_type': surfaceType,
      'surface_version': surfaceVersion,
      'state': state,
      'current_revision': currentRevision,
      'source_seq': sourceSeq,
      'document': documentJson,
      'updated_at': updatedAt,
    };
  }

  @override
  String toString() {
    return _i1.SerializationManager.encode(this);
  }
}

class _WorkroomSurfaceImpl extends WorkroomSurface {
  const _WorkroomSurfaceImpl({
    required String surfaceId,
    required String threadId,
    required String causedByTurnId,
    required String surfaceType,
    required int surfaceVersion,
    required String state,
    required int currentRevision,
    required int sourceSeq,
    required String documentJson,
    required String updatedAt,
  }) : super._(
         surfaceId: surfaceId,
         threadId: threadId,
         causedByTurnId: causedByTurnId,
         surfaceType: surfaceType,
         surfaceVersion: surfaceVersion,
         state: state,
         currentRevision: currentRevision,
         sourceSeq: sourceSeq,
         documentJson: documentJson,
         updatedAt: updatedAt,
       );

  /// Returns a shallow copy of this [WorkroomSurface]
  /// with some or all fields replaced by the given arguments.
  @_i1.useResult
  @override
  WorkroomSurface copyWith({
    String? surfaceId,
    String? threadId,
    String? causedByTurnId,
    String? surfaceType,
    int? surfaceVersion,
    String? state,
    int? currentRevision,
    int? sourceSeq,
    String? documentJson,
    String? updatedAt,
  }) {
    return WorkroomSurface(
      surfaceId: surfaceId ?? this.surfaceId,
      threadId: threadId ?? this.threadId,
      causedByTurnId: causedByTurnId ?? this.causedByTurnId,
      surfaceType: surfaceType ?? this.surfaceType,
      surfaceVersion: surfaceVersion ?? this.surfaceVersion,
      state: state ?? this.state,
      currentRevision: currentRevision ?? this.currentRevision,
      sourceSeq: sourceSeq ?? this.sourceSeq,
      documentJson: documentJson ?? this.documentJson,
      updatedAt: updatedAt ?? this.updatedAt,
    );
  }
}
