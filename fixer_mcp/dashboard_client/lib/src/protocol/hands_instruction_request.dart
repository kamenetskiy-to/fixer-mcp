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

import 'package:serverpod_client/serverpod_client.dart' as _i1;

@_i1.immutable
abstract class HandsInstructionRequest implements _i1.SerializableModel {
  const HandsInstructionRequest._({
    required this.projectId,
    required this.instructionText,
    required this.requestedLane,
    required this.idempotencyKey,
  });

  const factory HandsInstructionRequest({
    required int projectId,
    required String instructionText,
    required String requestedLane,
    required String idempotencyKey,
  }) = _HandsInstructionRequestImpl;

  factory HandsInstructionRequest.fromJson(
    Map<String, dynamic> jsonSerialization,
  ) {
    return HandsInstructionRequest(
      projectId: jsonSerialization['projectId'] as int,
      instructionText: jsonSerialization['instructionText'] as String,
      requestedLane: jsonSerialization['requestedLane'] as String,
      idempotencyKey: jsonSerialization['idempotencyKey'] as String,
    );
  }

  final int projectId;

  final String instructionText;

  final String requestedLane;

  final String idempotencyKey;

  /// Returns a shallow copy of this [HandsInstructionRequest]
  /// with some or all fields replaced by the given arguments.
  @_i1.useResult
  HandsInstructionRequest copyWith({
    int? projectId,
    String? instructionText,
    String? requestedLane,
    String? idempotencyKey,
  });
  @override
  bool operator ==(Object other) {
    return identical(
          other,
          this,
        ) ||
        other.runtimeType == runtimeType &&
            other is HandsInstructionRequest &&
            (identical(
                  other.projectId,
                  projectId,
                ) ||
                other.projectId == projectId) &&
            (identical(
                  other.instructionText,
                  instructionText,
                ) ||
                other.instructionText == instructionText) &&
            (identical(
                  other.requestedLane,
                  requestedLane,
                ) ||
                other.requestedLane == requestedLane) &&
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
      instructionText,
      requestedLane,
      idempotencyKey,
    );
  }

  @override
  Map<String, dynamic> toJson() {
    return {
      '__className__': 'HandsInstructionRequest',
      'projectId': projectId,
      'instructionText': instructionText,
      'requestedLane': requestedLane,
      'idempotencyKey': idempotencyKey,
    };
  }

  @override
  String toString() {
    return _i1.SerializationManager.encode(this);
  }
}

class _HandsInstructionRequestImpl extends HandsInstructionRequest {
  const _HandsInstructionRequestImpl({
    required int projectId,
    required String instructionText,
    required String requestedLane,
    required String idempotencyKey,
  }) : super._(
         projectId: projectId,
         instructionText: instructionText,
         requestedLane: requestedLane,
         idempotencyKey: idempotencyKey,
       );

  /// Returns a shallow copy of this [HandsInstructionRequest]
  /// with some or all fields replaced by the given arguments.
  @_i1.useResult
  @override
  HandsInstructionRequest copyWith({
    int? projectId,
    String? instructionText,
    String? requestedLane,
    String? idempotencyKey,
  }) {
    return HandsInstructionRequest(
      projectId: projectId ?? this.projectId,
      instructionText: instructionText ?? this.instructionText,
      requestedLane: requestedLane ?? this.requestedLane,
      idempotencyKey: idempotencyKey ?? this.idempotencyKey,
    );
  }
}
