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
abstract class CommandReceipt
    implements _i1.SerializableModel, _i1.ProtocolSerialization {
  const CommandReceipt._({
    required this.status,
    this.instructionId,
    this.state,
    this.revision,
    required this.projectSeq,
  });

  const factory CommandReceipt({
    required String status,
    String? instructionId,
    String? state,
    int? revision,
    required int projectSeq,
  }) = _CommandReceiptImpl;

  factory CommandReceipt.fromJson(Map<String, dynamic> jsonSerialization) {
    return CommandReceipt(
      status: jsonSerialization['status'] as String,
      instructionId: jsonSerialization['instruction_id'] as String?,
      state: jsonSerialization['state'] as String?,
      revision: jsonSerialization['revision'] as int?,
      projectSeq: jsonSerialization['project_seq'] as int,
    );
  }

  final String status;

  final String? instructionId;

  final String? state;

  final int? revision;

  final int projectSeq;

  /// Returns a shallow copy of this [CommandReceipt]
  /// with some or all fields replaced by the given arguments.
  @_i1.useResult
  CommandReceipt copyWith({
    String? status,
    String? instructionId,
    String? state,
    int? revision,
    int? projectSeq,
  });
  @override
  bool operator ==(Object other) {
    return identical(
          other,
          this,
        ) ||
        other.runtimeType == runtimeType &&
            other is CommandReceipt &&
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
                  other.state,
                  state,
                ) ||
                other.state == state) &&
            (identical(
                  other.revision,
                  revision,
                ) ||
                other.revision == revision) &&
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
      instructionId,
      state,
      revision,
      projectSeq,
    );
  }

  @override
  Map<String, dynamic> toJson() {
    return {
      '__className__': 'CommandReceipt',
      'status': status,
      if (instructionId != null) 'instruction_id': instructionId,
      if (state != null) 'state': state,
      if (revision != null) 'revision': revision,
      'project_seq': projectSeq,
    };
  }

  @override
  Map<String, dynamic> toJsonForProtocol() {
    return {
      '__className__': 'CommandReceipt',
      'status': status,
      if (instructionId != null) 'instruction_id': instructionId,
      if (state != null) 'state': state,
      if (revision != null) 'revision': revision,
      'project_seq': projectSeq,
    };
  }

  @override
  String toString() {
    return _i1.SerializationManager.encode(this);
  }
}

class _Undefined {}

class _CommandReceiptImpl extends CommandReceipt {
  const _CommandReceiptImpl({
    required String status,
    String? instructionId,
    String? state,
    int? revision,
    required int projectSeq,
  }) : super._(
         status: status,
         instructionId: instructionId,
         state: state,
         revision: revision,
         projectSeq: projectSeq,
       );

  /// Returns a shallow copy of this [CommandReceipt]
  /// with some or all fields replaced by the given arguments.
  @_i1.useResult
  @override
  CommandReceipt copyWith({
    String? status,
    Object? instructionId = _Undefined,
    Object? state = _Undefined,
    Object? revision = _Undefined,
    int? projectSeq,
  }) {
    return CommandReceipt(
      status: status ?? this.status,
      instructionId: instructionId is String?
          ? instructionId
          : this.instructionId,
      state: state is String? ? state : this.state,
      revision: revision is int? ? revision : this.revision,
      projectSeq: projectSeq ?? this.projectSeq,
    );
  }
}
