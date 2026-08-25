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

@_i1.immutable
abstract class GenuiActionRequest
    implements _i1.SerializableModel, _i1.ProtocolSerialization {
  const GenuiActionRequest._({
    required this.projectId,
    required this.protocolVersion,
    required this.surfaceId,
    required this.surfaceRevision,
    required this.actionId,
    required this.actionVersion,
    required this.targetType,
    required this.targetId,
    required this.inputJson,
    required this.confirmed,
    required this.idempotencyKey,
  });

  const factory GenuiActionRequest({
    required int projectId,
    required int protocolVersion,
    required String surfaceId,
    required int surfaceRevision,
    required String actionId,
    required int actionVersion,
    required String targetType,
    required String targetId,
    required String inputJson,
    required bool confirmed,
    required String idempotencyKey,
  }) = _GenuiActionRequestImpl;

  factory GenuiActionRequest.fromJson(Map<String, dynamic> jsonSerialization) {
    return GenuiActionRequest(
      projectId: jsonSerialization['projectId'] as int,
      protocolVersion: jsonSerialization['protocolVersion'] as int,
      surfaceId: jsonSerialization['surfaceId'] as String,
      surfaceRevision: jsonSerialization['surfaceRevision'] as int,
      actionId: jsonSerialization['actionId'] as String,
      actionVersion: jsonSerialization['actionVersion'] as int,
      targetType: jsonSerialization['targetType'] as String,
      targetId: jsonSerialization['targetId'] as String,
      inputJson: jsonSerialization['inputJson'] as String,
      confirmed: _i1.BoolJsonExtension.fromJson(jsonSerialization['confirmed']),
      idempotencyKey: jsonSerialization['idempotencyKey'] as String,
    );
  }

  final int projectId;

  final int protocolVersion;

  final String surfaceId;

  final int surfaceRevision;

  final String actionId;

  final int actionVersion;

  final String targetType;

  final String targetId;

  final String inputJson;

  final bool confirmed;

  final String idempotencyKey;

  /// Returns a shallow copy of this [GenuiActionRequest]
  /// with some or all fields replaced by the given arguments.
  @_i1.useResult
  GenuiActionRequest copyWith({
    int? projectId,
    int? protocolVersion,
    String? surfaceId,
    int? surfaceRevision,
    String? actionId,
    int? actionVersion,
    String? targetType,
    String? targetId,
    String? inputJson,
    bool? confirmed,
    String? idempotencyKey,
  });
  @override
  bool operator ==(Object other) {
    return identical(
          other,
          this,
        ) ||
        other.runtimeType == runtimeType &&
            other is GenuiActionRequest &&
            (identical(
                  other.projectId,
                  projectId,
                ) ||
                other.projectId == projectId) &&
            (identical(
                  other.protocolVersion,
                  protocolVersion,
                ) ||
                other.protocolVersion == protocolVersion) &&
            (identical(
                  other.surfaceId,
                  surfaceId,
                ) ||
                other.surfaceId == surfaceId) &&
            (identical(
                  other.surfaceRevision,
                  surfaceRevision,
                ) ||
                other.surfaceRevision == surfaceRevision) &&
            (identical(
                  other.actionId,
                  actionId,
                ) ||
                other.actionId == actionId) &&
            (identical(
                  other.actionVersion,
                  actionVersion,
                ) ||
                other.actionVersion == actionVersion) &&
            (identical(
                  other.targetType,
                  targetType,
                ) ||
                other.targetType == targetType) &&
            (identical(
                  other.targetId,
                  targetId,
                ) ||
                other.targetId == targetId) &&
            (identical(
                  other.inputJson,
                  inputJson,
                ) ||
                other.inputJson == inputJson) &&
            (identical(
                  other.confirmed,
                  confirmed,
                ) ||
                other.confirmed == confirmed) &&
            (identical(
                  other.idempotencyKey,
                  idempotencyKey,
                ) ||
                other.idempotencyKey == idempotencyKey);
  }

  @override
  int get hashCode {
    return Object.hash(
      runtimeType,
      projectId,
      protocolVersion,
      surfaceId,
      surfaceRevision,
      actionId,
      actionVersion,
      targetType,
      targetId,
      inputJson,
      confirmed,
      idempotencyKey,
    );
  }

  @override
  Map<String, dynamic> toJson() {
    return {
      '__className__': 'GenuiActionRequest',
      'projectId': projectId,
      'protocolVersion': protocolVersion,
      'surfaceId': surfaceId,
      'surfaceRevision': surfaceRevision,
      'actionId': actionId,
      'actionVersion': actionVersion,
      'targetType': targetType,
      'targetId': targetId,
      'inputJson': inputJson,
      'confirmed': confirmed,
      'idempotencyKey': idempotencyKey,
    };
  }

  @override
  Map<String, dynamic> toJsonForProtocol() {
    return {
      '__className__': 'GenuiActionRequest',
      'projectId': projectId,
      'protocolVersion': protocolVersion,
      'surfaceId': surfaceId,
      'surfaceRevision': surfaceRevision,
      'actionId': actionId,
      'actionVersion': actionVersion,
      'targetType': targetType,
      'targetId': targetId,
      'inputJson': inputJson,
      'confirmed': confirmed,
      'idempotencyKey': idempotencyKey,
    };
  }

  @override
  String toString() {
    return _i1.SerializationManager.encode(this);
  }
}

class _GenuiActionRequestImpl extends GenuiActionRequest {
  const _GenuiActionRequestImpl({
    required int projectId,
    required int protocolVersion,
    required String surfaceId,
    required int surfaceRevision,
    required String actionId,
    required int actionVersion,
    required String targetType,
    required String targetId,
    required String inputJson,
    required bool confirmed,
    required String idempotencyKey,
  }) : super._(
         projectId: projectId,
         protocolVersion: protocolVersion,
         surfaceId: surfaceId,
         surfaceRevision: surfaceRevision,
         actionId: actionId,
         actionVersion: actionVersion,
         targetType: targetType,
         targetId: targetId,
         inputJson: inputJson,
         confirmed: confirmed,
         idempotencyKey: idempotencyKey,
       );

  /// Returns a shallow copy of this [GenuiActionRequest]
  /// with some or all fields replaced by the given arguments.
  @_i1.useResult
  @override
  GenuiActionRequest copyWith({
    int? projectId,
    int? protocolVersion,
    String? surfaceId,
    int? surfaceRevision,
    String? actionId,
    int? actionVersion,
    String? targetType,
    String? targetId,
    String? inputJson,
    bool? confirmed,
    String? idempotencyKey,
  }) {
    return GenuiActionRequest(
      projectId: projectId ?? this.projectId,
      protocolVersion: protocolVersion ?? this.protocolVersion,
      surfaceId: surfaceId ?? this.surfaceId,
      surfaceRevision: surfaceRevision ?? this.surfaceRevision,
      actionId: actionId ?? this.actionId,
      actionVersion: actionVersion ?? this.actionVersion,
      targetType: targetType ?? this.targetType,
      targetId: targetId ?? this.targetId,
      inputJson: inputJson ?? this.inputJson,
      confirmed: confirmed ?? this.confirmed,
      idempotencyKey: idempotencyKey ?? this.idempotencyKey,
    );
  }
}
