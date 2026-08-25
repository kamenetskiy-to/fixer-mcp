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

/// A durable user or assistant message projected from a Codex turn.
abstract class CodexMessage implements _i1.SerializableModel {
  CodexMessage._({
    this.id,
    required this.codexThreadId,
    required this.role,
    required this.text,
    this.turnId,
    DateTime? createdAt,
  }) : createdAt = createdAt ?? DateTime.now();

  factory CodexMessage({
    int? id,
    required int codexThreadId,
    required String role,
    required String text,
    String? turnId,
    DateTime? createdAt,
  }) = _CodexMessageImpl;

  factory CodexMessage.fromJson(Map<String, dynamic> jsonSerialization) {
    return CodexMessage(
      id: jsonSerialization['id'] as int?,
      codexThreadId: jsonSerialization['codexThreadId'] as int,
      role: jsonSerialization['role'] as String,
      text: jsonSerialization['text'] as String,
      turnId: jsonSerialization['turnId'] as String?,
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

  String role;

  String text;

  String? turnId;

  DateTime createdAt;

  /// Returns a shallow copy of this [CodexMessage]
  /// with some or all fields replaced by the given arguments.
  @_i1.useResult
  CodexMessage copyWith({
    int? id,
    int? codexThreadId,
    String? role,
    String? text,
    String? turnId,
    DateTime? createdAt,
  });
  @override
  Map<String, dynamic> toJson() {
    return {
      '__className__': 'CodexMessage',
      if (id != null) 'id': id,
      'codexThreadId': codexThreadId,
      'role': role,
      'text': text,
      if (turnId != null) 'turnId': turnId,
      'createdAt': createdAt.toJson(),
    };
  }

  @override
  String toString() {
    return _i1.SerializationManager.encode(this);
  }
}

class _Undefined {}

class _CodexMessageImpl extends CodexMessage {
  _CodexMessageImpl({
    int? id,
    required int codexThreadId,
    required String role,
    required String text,
    String? turnId,
    DateTime? createdAt,
  }) : super._(
         id: id,
         codexThreadId: codexThreadId,
         role: role,
         text: text,
         turnId: turnId,
         createdAt: createdAt,
       );

  /// Returns a shallow copy of this [CodexMessage]
  /// with some or all fields replaced by the given arguments.
  @_i1.useResult
  @override
  CodexMessage copyWith({
    Object? id = _Undefined,
    int? codexThreadId,
    String? role,
    String? text,
    Object? turnId = _Undefined,
    DateTime? createdAt,
  }) {
    return CodexMessage(
      id: id is int? ? id : this.id,
      codexThreadId: codexThreadId ?? this.codexThreadId,
      role: role ?? this.role,
      text: text ?? this.text,
      turnId: turnId is String? ? turnId : this.turnId,
      createdAt: createdAt ?? this.createdAt,
    );
  }
}
