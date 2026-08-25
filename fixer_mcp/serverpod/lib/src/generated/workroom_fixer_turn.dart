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

/// One ordered turn in a durable Fixer thread.
@_i1.immutable
abstract class WorkroomFixerTurn
    implements _i1.SerializableModel, _i1.ProtocolSerialization {
  const WorkroomFixerTurn._({
    required this.turnId,
    required this.threadId,
    required this.ordinal,
    required this.role,
    required this.content,
    this.source,
    required this.status,
    this.clientMessageId,
    this.providerTurnId,
    required this.createdAt,
    this.completedAt,
  });

  const factory WorkroomFixerTurn({
    required String turnId,
    required String threadId,
    required int ordinal,
    required String role,
    required String content,
    String? source,
    required String status,
    String? clientMessageId,
    String? providerTurnId,
    required String createdAt,
    String? completedAt,
  }) = _WorkroomFixerTurnImpl;

  factory WorkroomFixerTurn.fromJson(Map<String, dynamic> jsonSerialization) {
    return WorkroomFixerTurn(
      turnId: jsonSerialization['id'] as String,
      threadId: jsonSerialization['thread_id'] as String,
      ordinal: jsonSerialization['ordinal'] as int,
      role: jsonSerialization['role'] as String,
      content: jsonSerialization['content'] as String,
      source: jsonSerialization['source'] as String?,
      status: jsonSerialization['status'] as String,
      clientMessageId: jsonSerialization['client_message_id'] as String?,
      providerTurnId: jsonSerialization['provider_turn_id'] as String?,
      createdAt: jsonSerialization['created_at'] as String,
      completedAt: jsonSerialization['completed_at'] as String?,
    );
  }

  final String turnId;

  final String threadId;

  final int ordinal;

  final String role;

  final String content;

  final String? source;

  final String status;

  final String? clientMessageId;

  final String? providerTurnId;

  final String createdAt;

  final String? completedAt;

  /// Returns a shallow copy of this [WorkroomFixerTurn]
  /// with some or all fields replaced by the given arguments.
  @_i1.useResult
  WorkroomFixerTurn copyWith({
    String? turnId,
    String? threadId,
    int? ordinal,
    String? role,
    String? content,
    String? source,
    String? status,
    String? clientMessageId,
    String? providerTurnId,
    String? createdAt,
    String? completedAt,
  });
  @override
  bool operator ==(Object other) {
    return identical(
          other,
          this,
        ) ||
        other.runtimeType == runtimeType &&
            other is WorkroomFixerTurn &&
            (identical(
                  other.turnId,
                  turnId,
                ) ||
                other.turnId == turnId) &&
            (identical(
                  other.threadId,
                  threadId,
                ) ||
                other.threadId == threadId) &&
            (identical(
                  other.ordinal,
                  ordinal,
                ) ||
                other.ordinal == ordinal) &&
            (identical(
                  other.role,
                  role,
                ) ||
                other.role == role) &&
            (identical(
                  other.content,
                  content,
                ) ||
                other.content == content) &&
            (identical(
                  other.source,
                  source,
                ) ||
                other.source == source) &&
            (identical(
                  other.status,
                  status,
                ) ||
                other.status == status) &&
            (identical(
                  other.clientMessageId,
                  clientMessageId,
                ) ||
                other.clientMessageId == clientMessageId) &&
            (identical(
                  other.providerTurnId,
                  providerTurnId,
                ) ||
                other.providerTurnId == providerTurnId) &&
            (identical(
                  other.createdAt,
                  createdAt,
                ) ||
                other.createdAt == createdAt) &&
            (identical(
                  other.completedAt,
                  completedAt,
                ) ||
                other.completedAt == completedAt);
  }

  @override
  int get hashCode {
    return Object.hash(
      runtimeType,
      turnId,
      threadId,
      ordinal,
      role,
      content,
      source,
      status,
      clientMessageId,
      providerTurnId,
      createdAt,
      completedAt,
    );
  }

  @override
  Map<String, dynamic> toJson() {
    return {
      '__className__': 'WorkroomFixerTurn',
      'id': turnId,
      'thread_id': threadId,
      'ordinal': ordinal,
      'role': role,
      'content': content,
      if (source != null) 'source': source,
      'status': status,
      if (clientMessageId != null) 'client_message_id': clientMessageId,
      if (providerTurnId != null) 'provider_turn_id': providerTurnId,
      'created_at': createdAt,
      if (completedAt != null) 'completed_at': completedAt,
    };
  }

  @override
  Map<String, dynamic> toJsonForProtocol() {
    return {
      '__className__': 'WorkroomFixerTurn',
      'id': turnId,
      'thread_id': threadId,
      'ordinal': ordinal,
      'role': role,
      'content': content,
      if (source != null) 'source': source,
      'status': status,
      if (clientMessageId != null) 'client_message_id': clientMessageId,
      if (providerTurnId != null) 'provider_turn_id': providerTurnId,
      'created_at': createdAt,
      if (completedAt != null) 'completed_at': completedAt,
    };
  }

  @override
  String toString() {
    return _i1.SerializationManager.encode(this);
  }
}

class _Undefined {}

class _WorkroomFixerTurnImpl extends WorkroomFixerTurn {
  const _WorkroomFixerTurnImpl({
    required String turnId,
    required String threadId,
    required int ordinal,
    required String role,
    required String content,
    String? source,
    required String status,
    String? clientMessageId,
    String? providerTurnId,
    required String createdAt,
    String? completedAt,
  }) : super._(
         turnId: turnId,
         threadId: threadId,
         ordinal: ordinal,
         role: role,
         content: content,
         source: source,
         status: status,
         clientMessageId: clientMessageId,
         providerTurnId: providerTurnId,
         createdAt: createdAt,
         completedAt: completedAt,
       );

  /// Returns a shallow copy of this [WorkroomFixerTurn]
  /// with some or all fields replaced by the given arguments.
  @_i1.useResult
  @override
  WorkroomFixerTurn copyWith({
    String? turnId,
    String? threadId,
    int? ordinal,
    String? role,
    String? content,
    Object? source = _Undefined,
    String? status,
    Object? clientMessageId = _Undefined,
    Object? providerTurnId = _Undefined,
    String? createdAt,
    Object? completedAt = _Undefined,
  }) {
    return WorkroomFixerTurn(
      turnId: turnId ?? this.turnId,
      threadId: threadId ?? this.threadId,
      ordinal: ordinal ?? this.ordinal,
      role: role ?? this.role,
      content: content ?? this.content,
      source: source is String? ? source : this.source,
      status: status ?? this.status,
      clientMessageId: clientMessageId is String?
          ? clientMessageId
          : this.clientMessageId,
      providerTurnId: providerTurnId is String?
          ? providerTurnId
          : this.providerTurnId,
      createdAt: createdAt ?? this.createdAt,
      completedAt: completedAt is String? ? completedAt : this.completedAt,
    );
  }
}
