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
import 'package:fixer_dashboard_server/src/generated/protocol.dart' as _i2;

/// Durable mailbox item owned by the permanent Hands actor.
@_i1.immutable
abstract class WorkroomHandsInstruction
    implements _i1.SerializableModel, _i1.ProtocolSerialization {
  const WorkroomHandsInstruction._({
    required this.instructionId,
    required this.ordinal,
    required this.instructionText,
    required this.declaredWriteScope,
    required this.requestedLane,
    required this.riskClass,
    required this.reviewPolicy,
    required this.state,
    this.stateReasonCode,
    this.stateReasonText,
    required this.revision,
    required this.createdAt,
    required this.updatedAt,
    this.terminalAt,
  });

  const factory WorkroomHandsInstruction({
    required String instructionId,
    required int ordinal,
    required String instructionText,
    required List<String> declaredWriteScope,
    required String requestedLane,
    required String riskClass,
    required String reviewPolicy,
    required String state,
    String? stateReasonCode,
    String? stateReasonText,
    required int revision,
    required String createdAt,
    required String updatedAt,
    String? terminalAt,
  }) = _WorkroomHandsInstructionImpl;

  factory WorkroomHandsInstruction.fromJson(
    Map<String, dynamic> jsonSerialization,
  ) {
    return WorkroomHandsInstruction(
      instructionId: jsonSerialization['id'] as String,
      ordinal: jsonSerialization['ordinal'] as int,
      instructionText: jsonSerialization['instruction_text'] as String,
      declaredWriteScope: _i2.Protocol().deserialize<List<String>>(
        jsonSerialization['declared_write_scope'],
      ),
      requestedLane: jsonSerialization['requested_lane'] as String,
      riskClass: jsonSerialization['risk_class'] as String,
      reviewPolicy: jsonSerialization['review_policy'] as String,
      state: jsonSerialization['state'] as String,
      stateReasonCode: jsonSerialization['state_reason_code'] as String?,
      stateReasonText: jsonSerialization['state_reason_text'] as String?,
      revision: jsonSerialization['revision'] as int,
      createdAt: jsonSerialization['created_at'] as String,
      updatedAt: jsonSerialization['updated_at'] as String,
      terminalAt: jsonSerialization['terminal_at'] as String?,
    );
  }

  final String instructionId;

  final int ordinal;

  final String instructionText;

  final List<String> declaredWriteScope;

  final String requestedLane;

  final String riskClass;

  final String reviewPolicy;

  final String state;

  final String? stateReasonCode;

  final String? stateReasonText;

  final int revision;

  final String createdAt;

  final String updatedAt;

  final String? terminalAt;

  /// Returns a shallow copy of this [WorkroomHandsInstruction]
  /// with some or all fields replaced by the given arguments.
  @_i1.useResult
  WorkroomHandsInstruction copyWith({
    String? instructionId,
    int? ordinal,
    String? instructionText,
    List<String>? declaredWriteScope,
    String? requestedLane,
    String? riskClass,
    String? reviewPolicy,
    String? state,
    String? stateReasonCode,
    String? stateReasonText,
    int? revision,
    String? createdAt,
    String? updatedAt,
    String? terminalAt,
  });
  @override
  bool operator ==(Object other) {
    return identical(
          other,
          this,
        ) ||
        other.runtimeType == runtimeType &&
            other is WorkroomHandsInstruction &&
            (identical(
                  other.instructionId,
                  instructionId,
                ) ||
                other.instructionId == instructionId) &&
            (identical(
                  other.ordinal,
                  ordinal,
                ) ||
                other.ordinal == ordinal) &&
            (identical(
                  other.instructionText,
                  instructionText,
                ) ||
                other.instructionText == instructionText) &&
            const _i1.DeepCollectionEquality().equals(
              other.declaredWriteScope,
              declaredWriteScope,
            ) &&
            (identical(
                  other.requestedLane,
                  requestedLane,
                ) ||
                other.requestedLane == requestedLane) &&
            (identical(
                  other.riskClass,
                  riskClass,
                ) ||
                other.riskClass == riskClass) &&
            (identical(
                  other.reviewPolicy,
                  reviewPolicy,
                ) ||
                other.reviewPolicy == reviewPolicy) &&
            (identical(
                  other.state,
                  state,
                ) ||
                other.state == state) &&
            (identical(
                  other.stateReasonCode,
                  stateReasonCode,
                ) ||
                other.stateReasonCode == stateReasonCode) &&
            (identical(
                  other.stateReasonText,
                  stateReasonText,
                ) ||
                other.stateReasonText == stateReasonText) &&
            (identical(
                  other.revision,
                  revision,
                ) ||
                other.revision == revision) &&
            (identical(
                  other.createdAt,
                  createdAt,
                ) ||
                other.createdAt == createdAt) &&
            (identical(
                  other.updatedAt,
                  updatedAt,
                ) ||
                other.updatedAt == updatedAt) &&
            (identical(
                  other.terminalAt,
                  terminalAt,
                ) ||
                other.terminalAt == terminalAt);
  }

  @override
  int get hashCode {
    return Object.hash(
      runtimeType,
      instructionId,
      ordinal,
      instructionText,
      const _i1.DeepCollectionEquality().hash(declaredWriteScope),
      requestedLane,
      riskClass,
      reviewPolicy,
      state,
      stateReasonCode,
      stateReasonText,
      revision,
      createdAt,
      updatedAt,
      terminalAt,
    );
  }

  @override
  Map<String, dynamic> toJson() {
    return {
      '__className__': 'WorkroomHandsInstruction',
      'id': instructionId,
      'ordinal': ordinal,
      'instruction_text': instructionText,
      'declared_write_scope': declaredWriteScope.toJson(),
      'requested_lane': requestedLane,
      'risk_class': riskClass,
      'review_policy': reviewPolicy,
      'state': state,
      if (stateReasonCode != null) 'state_reason_code': stateReasonCode,
      if (stateReasonText != null) 'state_reason_text': stateReasonText,
      'revision': revision,
      'created_at': createdAt,
      'updated_at': updatedAt,
      if (terminalAt != null) 'terminal_at': terminalAt,
    };
  }

  @override
  Map<String, dynamic> toJsonForProtocol() {
    return {
      '__className__': 'WorkroomHandsInstruction',
      'id': instructionId,
      'ordinal': ordinal,
      'instruction_text': instructionText,
      'declared_write_scope': declaredWriteScope.toJson(),
      'requested_lane': requestedLane,
      'risk_class': riskClass,
      'review_policy': reviewPolicy,
      'state': state,
      if (stateReasonCode != null) 'state_reason_code': stateReasonCode,
      if (stateReasonText != null) 'state_reason_text': stateReasonText,
      'revision': revision,
      'created_at': createdAt,
      'updated_at': updatedAt,
      if (terminalAt != null) 'terminal_at': terminalAt,
    };
  }

  @override
  String toString() {
    return _i1.SerializationManager.encode(this);
  }
}

class _Undefined {}

class _WorkroomHandsInstructionImpl extends WorkroomHandsInstruction {
  const _WorkroomHandsInstructionImpl({
    required String instructionId,
    required int ordinal,
    required String instructionText,
    required List<String> declaredWriteScope,
    required String requestedLane,
    required String riskClass,
    required String reviewPolicy,
    required String state,
    String? stateReasonCode,
    String? stateReasonText,
    required int revision,
    required String createdAt,
    required String updatedAt,
    String? terminalAt,
  }) : super._(
         instructionId: instructionId,
         ordinal: ordinal,
         instructionText: instructionText,
         declaredWriteScope: declaredWriteScope,
         requestedLane: requestedLane,
         riskClass: riskClass,
         reviewPolicy: reviewPolicy,
         state: state,
         stateReasonCode: stateReasonCode,
         stateReasonText: stateReasonText,
         revision: revision,
         createdAt: createdAt,
         updatedAt: updatedAt,
         terminalAt: terminalAt,
       );

  /// Returns a shallow copy of this [WorkroomHandsInstruction]
  /// with some or all fields replaced by the given arguments.
  @_i1.useResult
  @override
  WorkroomHandsInstruction copyWith({
    String? instructionId,
    int? ordinal,
    String? instructionText,
    List<String>? declaredWriteScope,
    String? requestedLane,
    String? riskClass,
    String? reviewPolicy,
    String? state,
    Object? stateReasonCode = _Undefined,
    Object? stateReasonText = _Undefined,
    int? revision,
    String? createdAt,
    String? updatedAt,
    Object? terminalAt = _Undefined,
  }) {
    return WorkroomHandsInstruction(
      instructionId: instructionId ?? this.instructionId,
      ordinal: ordinal ?? this.ordinal,
      instructionText: instructionText ?? this.instructionText,
      declaredWriteScope:
          declaredWriteScope ??
          this.declaredWriteScope.map((e0) => e0).toList(),
      requestedLane: requestedLane ?? this.requestedLane,
      riskClass: riskClass ?? this.riskClass,
      reviewPolicy: reviewPolicy ?? this.reviewPolicy,
      state: state ?? this.state,
      stateReasonCode: stateReasonCode is String?
          ? stateReasonCode
          : this.stateReasonCode,
      stateReasonText: stateReasonText is String?
          ? stateReasonText
          : this.stateReasonText,
      revision: revision ?? this.revision,
      createdAt: createdAt ?? this.createdAt,
      updatedAt: updatedAt ?? this.updatedAt,
      terminalAt: terminalAt is String? ? terminalAt : this.terminalAt,
    );
  }
}
