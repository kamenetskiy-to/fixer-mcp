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
abstract class HandsInstructionReceipt implements _i1.SerializableModel {
  const HandsInstructionReceipt._({
    required this.status,
    required this.instructionId,
    required this.ordinal,
    required this.state,
    required this.lane,
    required this.riskClass,
    required this.projectSeq,
    this.reasonCode,
    this.reasonText,
  });

  const factory HandsInstructionReceipt({
    required String status,
    required String instructionId,
    required int ordinal,
    required String state,
    required String lane,
    required String riskClass,
    required int projectSeq,
    String? reasonCode,
    String? reasonText,
  }) = _HandsInstructionReceiptImpl;

  factory HandsInstructionReceipt.fromJson(
    Map<String, dynamic> jsonSerialization,
  ) {
    return HandsInstructionReceipt(
      status: jsonSerialization['status'] as String,
      instructionId: jsonSerialization['instruction_id'] as String,
      ordinal: jsonSerialization['ordinal'] as int,
      state: jsonSerialization['state'] as String,
      lane: jsonSerialization['lane'] as String,
      riskClass: jsonSerialization['risk_class'] as String,
      projectSeq: jsonSerialization['project_seq'] as int,
      reasonCode: jsonSerialization['reason_code'] as String?,
      reasonText: jsonSerialization['reason_text'] as String?,
    );
  }

  final String status;

  final String instructionId;

  final int ordinal;

  final String state;

  final String lane;

  final String riskClass;

  final int projectSeq;

  final String? reasonCode;

  final String? reasonText;

  /// Returns a shallow copy of this [HandsInstructionReceipt]
  /// with some or all fields replaced by the given arguments.
  @_i1.useResult
  HandsInstructionReceipt copyWith({
    String? status,
    String? instructionId,
    int? ordinal,
    String? state,
    String? lane,
    String? riskClass,
    int? projectSeq,
    String? reasonCode,
    String? reasonText,
  });
  @override
  bool operator ==(Object other) {
    return identical(
          other,
          this,
        ) ||
        other.runtimeType == runtimeType &&
            other is HandsInstructionReceipt &&
            (identical(
                  other.status,
                  status,
                ) ||
                other.status == status) &&
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
                  other.state,
                  state,
                ) ||
                other.state == state) &&
            (identical(
                  other.lane,
                  lane,
                ) ||
                other.lane == lane) &&
            (identical(
                  other.riskClass,
                  riskClass,
                ) ||
                other.riskClass == riskClass) &&
            (identical(
                  other.projectSeq,
                  projectSeq,
                ) ||
                other.projectSeq == projectSeq) &&
            (identical(
                  other.reasonCode,
                  reasonCode,
                ) ||
                other.reasonCode == reasonCode) &&
            (identical(
                  other.reasonText,
                  reasonText,
                ) ||
                other.reasonText == reasonText);
  }

  @override
  int get hashCode {
    return Object.hash(
      runtimeType,
      status,
      instructionId,
      ordinal,
      state,
      lane,
      riskClass,
      projectSeq,
      reasonCode,
      reasonText,
    );
  }

  @override
  Map<String, dynamic> toJson() {
    return {
      '__className__': 'HandsInstructionReceipt',
      'status': status,
      'instruction_id': instructionId,
      'ordinal': ordinal,
      'state': state,
      'lane': lane,
      'risk_class': riskClass,
      'project_seq': projectSeq,
      if (reasonCode != null) 'reason_code': reasonCode,
      if (reasonText != null) 'reason_text': reasonText,
    };
  }

  @override
  String toString() {
    return _i1.SerializationManager.encode(this);
  }
}

class _Undefined {}

class _HandsInstructionReceiptImpl extends HandsInstructionReceipt {
  const _HandsInstructionReceiptImpl({
    required String status,
    required String instructionId,
    required int ordinal,
    required String state,
    required String lane,
    required String riskClass,
    required int projectSeq,
    String? reasonCode,
    String? reasonText,
  }) : super._(
         status: status,
         instructionId: instructionId,
         ordinal: ordinal,
         state: state,
         lane: lane,
         riskClass: riskClass,
         projectSeq: projectSeq,
         reasonCode: reasonCode,
         reasonText: reasonText,
       );

  /// Returns a shallow copy of this [HandsInstructionReceipt]
  /// with some or all fields replaced by the given arguments.
  @_i1.useResult
  @override
  HandsInstructionReceipt copyWith({
    String? status,
    String? instructionId,
    int? ordinal,
    String? state,
    String? lane,
    String? riskClass,
    int? projectSeq,
    Object? reasonCode = _Undefined,
    Object? reasonText = _Undefined,
  }) {
    return HandsInstructionReceipt(
      status: status ?? this.status,
      instructionId: instructionId ?? this.instructionId,
      ordinal: ordinal ?? this.ordinal,
      state: state ?? this.state,
      lane: lane ?? this.lane,
      riskClass: riskClass ?? this.riskClass,
      projectSeq: projectSeq ?? this.projectSeq,
      reasonCode: reasonCode is String? ? reasonCode : this.reasonCode,
      reasonText: reasonText is String? ? reasonText : this.reasonText,
    );
  }
}
