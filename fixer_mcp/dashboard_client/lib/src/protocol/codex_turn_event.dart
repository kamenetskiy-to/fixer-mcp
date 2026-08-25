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

/// A durable raw notification received from codex-bridge for a turn.
abstract class CodexTurnEvent implements _i1.SerializableModel {
  CodexTurnEvent._({
    this.id,
    required this.codexThreadId,
    required this.turnId,
    required this.method,
    required this.json,
    DateTime? createdAt,
  }) : createdAt = createdAt ?? DateTime.now();

  factory CodexTurnEvent({
    int? id,
    required int codexThreadId,
    required String turnId,
    required String method,
    required String json,
    DateTime? createdAt,
  }) = _CodexTurnEventImpl;

  factory CodexTurnEvent.fromJson(Map<String, dynamic> jsonSerialization) {
    return CodexTurnEvent(
      id: jsonSerialization['id'] as int?,
      codexThreadId: jsonSerialization['codexThreadId'] as int,
      turnId: jsonSerialization['turnId'] as String,
      method: jsonSerialization['method'] as String,
      json: jsonSerialization['json'] as String,
      createdAt: jsonSerialization['createdAt'] == null
          ? null
          : _i1.DateTimeJsonExtension.fromJson(jsonSerialization['createdAt']),
    );
  }

  /// The database id, set if the object has been inserted into the
  /// database or if it has been fetched from the database. Otherwise,
  /// the id will be null.
  int? id;

  int codexThreadId;

  String turnId;

  String method;

  String json;

  DateTime createdAt;

  /// Returns a shallow copy of this [CodexTurnEvent]
  /// with some or all fields replaced by the given arguments.
  @_i1.useResult
  CodexTurnEvent copyWith({
    int? id,
    int? codexThreadId,
    String? turnId,
    String? method,
    String? json,
    DateTime? createdAt,
  });
  @override
  Map<String, dynamic> toJson() {
    return {
      '__className__': 'CodexTurnEvent',
      if (id != null) 'id': id,
      'codexThreadId': codexThreadId,
      'turnId': turnId,
      'method': method,
      'json': json,
      'createdAt': createdAt.toJson(),
    };
  }

  @override
  String toString() {
    return _i1.SerializationManager.encode(this);
  }
}

class _Undefined {}

class _CodexTurnEventImpl extends CodexTurnEvent {
  _CodexTurnEventImpl({
    int? id,
    required int codexThreadId,
    required String turnId,
    required String method,
    required String json,
    DateTime? createdAt,
  }) : super._(
         id: id,
         codexThreadId: codexThreadId,
         turnId: turnId,
         method: method,
         json: json,
         createdAt: createdAt,
       );

  /// Returns a shallow copy of this [CodexTurnEvent]
  /// with some or all fields replaced by the given arguments.
  @_i1.useResult
  @override
  CodexTurnEvent copyWith({
    Object? id = _Undefined,
    int? codexThreadId,
    String? turnId,
    String? method,
    String? json,
    DateTime? createdAt,
  }) {
    return CodexTurnEvent(
      id: id is int? ? id : this.id,
      codexThreadId: codexThreadId ?? this.codexThreadId,
      turnId: turnId ?? this.turnId,
      method: method ?? this.method,
      json: json ?? this.json,
      createdAt: createdAt ?? this.createdAt,
    );
  }
}
