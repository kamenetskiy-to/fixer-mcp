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
abstract class FixerTurnReceipt
    implements _i1.SerializableModel, _i1.ProtocolSerialization {
  const FixerTurnReceipt._({
    required this.status,
    required this.threadId,
    required this.turnId,
    required this.ordinal,
    required this.projectSeq,
  });

  const factory FixerTurnReceipt({
    required String status,
    required String threadId,
    required String turnId,
    required int ordinal,
    required int projectSeq,
  }) = _FixerTurnReceiptImpl;

  factory FixerTurnReceipt.fromJson(Map<String, dynamic> jsonSerialization) {
    return FixerTurnReceipt(
      status: jsonSerialization['status'] as String,
      threadId: jsonSerialization['thread_id'] as String,
      turnId: jsonSerialization['turn_id'] as String,
      ordinal: jsonSerialization['ordinal'] as int,
      projectSeq: jsonSerialization['project_seq'] as int,
    );
  }

  final String status;

  final String threadId;

  final String turnId;

  final int ordinal;

  final int projectSeq;

  /// Returns a shallow copy of this [FixerTurnReceipt]
  /// with some or all fields replaced by the given arguments.
  @_i1.useResult
  FixerTurnReceipt copyWith({
    String? status,
    String? threadId,
    String? turnId,
    int? ordinal,
    int? projectSeq,
  });
  @override
  bool operator ==(Object other) {
    return identical(
          other,
          this,
        ) ||
        other.runtimeType == runtimeType &&
            other is FixerTurnReceipt &&
            (identical(
                  other.status,
                  status,
                ) ||
                other.status == status) &&
            (identical(
                  other.threadId,
                  threadId,
                ) ||
                other.threadId == threadId) &&
            (identical(
                  other.turnId,
                  turnId,
                ) ||
                other.turnId == turnId) &&
            (identical(
                  other.ordinal,
                  ordinal,
                ) ||
                other.ordinal == ordinal) &&
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
      threadId,
      turnId,
      ordinal,
      projectSeq,
    );
  }

  @override
  Map<String, dynamic> toJson() {
    return {
      '__className__': 'FixerTurnReceipt',
      'status': status,
      'thread_id': threadId,
      'turn_id': turnId,
      'ordinal': ordinal,
      'project_seq': projectSeq,
    };
  }

  @override
  Map<String, dynamic> toJsonForProtocol() {
    return {
      '__className__': 'FixerTurnReceipt',
      'status': status,
      'thread_id': threadId,
      'turn_id': turnId,
      'ordinal': ordinal,
      'project_seq': projectSeq,
    };
  }

  @override
  String toString() {
    return _i1.SerializationManager.encode(this);
  }
}

class _FixerTurnReceiptImpl extends FixerTurnReceipt {
  const _FixerTurnReceiptImpl({
    required String status,
    required String threadId,
    required String turnId,
    required int ordinal,
    required int projectSeq,
  }) : super._(
         status: status,
         threadId: threadId,
         turnId: turnId,
         ordinal: ordinal,
         projectSeq: projectSeq,
       );

  /// Returns a shallow copy of this [FixerTurnReceipt]
  /// with some or all fields replaced by the given arguments.
  @_i1.useResult
  @override
  FixerTurnReceipt copyWith({
    String? status,
    String? threadId,
    String? turnId,
    int? ordinal,
    int? projectSeq,
  }) {
    return FixerTurnReceipt(
      status: status ?? this.status,
      threadId: threadId ?? this.threadId,
      turnId: turnId ?? this.turnId,
      ordinal: ordinal ?? this.ordinal,
      projectSeq: projectSeq ?? this.projectSeq,
    );
  }
}
