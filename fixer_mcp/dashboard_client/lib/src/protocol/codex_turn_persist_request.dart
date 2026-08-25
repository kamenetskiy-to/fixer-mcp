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

/// Future-call payload for durable Codex turn projection.
abstract class CodexTurnPersistRequest implements _i1.SerializableModel {
  CodexTurnPersistRequest._({
    required this.codexThreadId,
    required this.streamId,
    required this.turnId,
  });

  factory CodexTurnPersistRequest({
    required int codexThreadId,
    required String streamId,
    required String turnId,
  }) = _CodexTurnPersistRequestImpl;

  factory CodexTurnPersistRequest.fromJson(
    Map<String, dynamic> jsonSerialization,
  ) {
    return CodexTurnPersistRequest(
      codexThreadId: jsonSerialization['codexThreadId'] as int,
      streamId: jsonSerialization['streamId'] as String,
      turnId: jsonSerialization['turnId'] as String,
    );
  }

  int codexThreadId;

  String streamId;

  String turnId;

  /// Returns a shallow copy of this [CodexTurnPersistRequest]
  /// with some or all fields replaced by the given arguments.
  @_i1.useResult
  CodexTurnPersistRequest copyWith({
    int? codexThreadId,
    String? streamId,
    String? turnId,
  });
  @override
  Map<String, dynamic> toJson() {
    return {
      '__className__': 'CodexTurnPersistRequest',
      'codexThreadId': codexThreadId,
      'streamId': streamId,
      'turnId': turnId,
    };
  }

  @override
  String toString() {
    return _i1.SerializationManager.encode(this);
  }
}

class _CodexTurnPersistRequestImpl extends CodexTurnPersistRequest {
  _CodexTurnPersistRequestImpl({
    required int codexThreadId,
    required String streamId,
    required String turnId,
  }) : super._(
         codexThreadId: codexThreadId,
         streamId: streamId,
         turnId: turnId,
       );

  /// Returns a shallow copy of this [CodexTurnPersistRequest]
  /// with some or all fields replaced by the given arguments.
  @_i1.useResult
  @override
  CodexTurnPersistRequest copyWith({
    int? codexThreadId,
    String? streamId,
    String? turnId,
  }) {
    return CodexTurnPersistRequest(
      codexThreadId: codexThreadId ?? this.codexThreadId,
      streamId: streamId ?? this.streamId,
      turnId: turnId ?? this.turnId,
    );
  }
}
