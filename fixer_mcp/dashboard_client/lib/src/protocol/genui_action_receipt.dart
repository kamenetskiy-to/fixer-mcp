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
abstract class GenuiActionReceipt implements _i1.SerializableModel {
  const GenuiActionReceipt._({
    required this.status,
    required this.invocationId,
    required this.actionId,
    required this.decision,
    this.reasonCode,
    required this.projectSeq,
  });

  const factory GenuiActionReceipt({
    required String status,
    required String invocationId,
    required String actionId,
    required String decision,
    String? reasonCode,
    required int projectSeq,
  }) = _GenuiActionReceiptImpl;

  factory GenuiActionReceipt.fromJson(Map<String, dynamic> jsonSerialization) {
    return GenuiActionReceipt(
      status: jsonSerialization['status'] as String,
      invocationId: jsonSerialization['invocation_id'] as String,
      actionId: jsonSerialization['action_id'] as String,
      decision: jsonSerialization['decision'] as String,
      reasonCode: jsonSerialization['reason_code'] as String?,
      projectSeq: jsonSerialization['project_seq'] as int,
    );
  }

  final String status;

  final String invocationId;

  final String actionId;

  final String decision;

  final String? reasonCode;

  final int projectSeq;

  /// Returns a shallow copy of this [GenuiActionReceipt]
  /// with some or all fields replaced by the given arguments.
  @_i1.useResult
  GenuiActionReceipt copyWith({
    String? status,
    String? invocationId,
    String? actionId,
    String? decision,
    String? reasonCode,
    int? projectSeq,
  });
  @override
  bool operator ==(Object other) {
    return identical(
          other,
          this,
        ) ||
        other.runtimeType == runtimeType &&
            other is GenuiActionReceipt &&
            (identical(
                  other.status,
                  status,
                ) ||
                other.status == status) &&
            (identical(
                  other.invocationId,
                  invocationId,
                ) ||
                other.invocationId == invocationId) &&
            (identical(
                  other.actionId,
                  actionId,
                ) ||
                other.actionId == actionId) &&
            (identical(
                  other.decision,
                  decision,
                ) ||
                other.decision == decision) &&
            (identical(
                  other.reasonCode,
                  reasonCode,
                ) ||
                other.reasonCode == reasonCode) &&
            (identical(
                  other.projectSeq,
                  projectSeq,
                ) ||
                other.projectSeq == projectSeq);
  }

  @override
  int get hashCode {
    return Object.hash(
      runtimeType,
      status,
      invocationId,
      actionId,
      decision,
      reasonCode,
      projectSeq,
    );
  }

  @override
  Map<String, dynamic> toJson() {
    return {
      '__className__': 'GenuiActionReceipt',
      'status': status,
      'invocation_id': invocationId,
      'action_id': actionId,
      'decision': decision,
      if (reasonCode != null) 'reason_code': reasonCode,
      'project_seq': projectSeq,
    };
  }

  @override
  String toString() {
    return _i1.SerializationManager.encode(this);
  }
}

class _Undefined {}

class _GenuiActionReceiptImpl extends GenuiActionReceipt {
  const _GenuiActionReceiptImpl({
    required String status,
    required String invocationId,
    required String actionId,
    required String decision,
    String? reasonCode,
    required int projectSeq,
  }) : super._(
         status: status,
         invocationId: invocationId,
         actionId: actionId,
         decision: decision,
         reasonCode: reasonCode,
         projectSeq: projectSeq,
       );

  /// Returns a shallow copy of this [GenuiActionReceipt]
  /// with some or all fields replaced by the given arguments.
  @_i1.useResult
  @override
  GenuiActionReceipt copyWith({
    String? status,
    String? invocationId,
    String? actionId,
    String? decision,
    Object? reasonCode = _Undefined,
    int? projectSeq,
  }) {
    return GenuiActionReceipt(
      status: status ?? this.status,
      invocationId: invocationId ?? this.invocationId,
      actionId: actionId ?? this.actionId,
      decision: decision ?? this.decision,
      reasonCode: reasonCode is String? ? reasonCode : this.reasonCode,
      projectSeq: projectSeq ?? this.projectSeq,
    );
  }
}
