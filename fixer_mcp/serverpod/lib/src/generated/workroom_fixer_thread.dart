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

/// One durable Fixer conversation thread in a Project Workroom snapshot.
@_i1.immutable
abstract class WorkroomFixerThread
    implements _i1.SerializableModel, _i1.ProtocolSerialization {
  const WorkroomFixerThread._({
    required this.threadId,
    required this.provider,
    this.externalSessionId,
    required this.headline,
    required this.state,
    required this.createdAt,
    required this.updatedAt,
  });

  const factory WorkroomFixerThread({
    required String threadId,
    required String provider,
    String? externalSessionId,
    required String headline,
    required String state,
    required String createdAt,
    required String updatedAt,
  }) = _WorkroomFixerThreadImpl;

  factory WorkroomFixerThread.fromJson(Map<String, dynamic> jsonSerialization) {
    return WorkroomFixerThread(
      threadId: jsonSerialization['id'] as String,
      provider: jsonSerialization['provider'] as String,
      externalSessionId: jsonSerialization['external_session_id'] as String?,
      headline: jsonSerialization['headline'] as String,
      state: jsonSerialization['state'] as String,
      createdAt: jsonSerialization['created_at'] as String,
      updatedAt: jsonSerialization['updated_at'] as String,
    );
  }

  final String threadId;

  final String provider;

  final String? externalSessionId;

  final String headline;

  final String state;

  final String createdAt;

  final String updatedAt;

  /// Returns a shallow copy of this [WorkroomFixerThread]
  /// with some or all fields replaced by the given arguments.
  @_i1.useResult
  WorkroomFixerThread copyWith({
    String? threadId,
    String? provider,
    String? externalSessionId,
    String? headline,
    String? state,
    String? createdAt,
    String? updatedAt,
  });
  @override
  bool operator ==(Object other) {
    return identical(
          other,
          this,
        ) ||
        other.runtimeType == runtimeType &&
            other is WorkroomFixerThread &&
            (identical(
                  other.threadId,
                  threadId,
                ) ||
                other.threadId == threadId) &&
            (identical(
                  other.provider,
                  provider,
                ) ||
                other.provider == provider) &&
            (identical(
                  other.externalSessionId,
                  externalSessionId,
                ) ||
                other.externalSessionId == externalSessionId) &&
            (identical(
                  other.headline,
                  headline,
                ) ||
                other.headline == headline) &&
            (identical(
                  other.state,
                  state,
                ) ||
                other.state == state) &&
            (identical(
                  other.createdAt,
                  createdAt,
                ) ||
                other.createdAt == createdAt) &&
            (identical(
                  other.updatedAt,
                  updatedAt,
                ) ||
                other.updatedAt == updatedAt);
  }

  @override
  int get hashCode {
    return Object.hash(
      runtimeType,
      threadId,
      provider,
      externalSessionId,
      headline,
      state,
      createdAt,
      updatedAt,
    );
  }

  @override
  Map<String, dynamic> toJson() {
    return {
      '__className__': 'WorkroomFixerThread',
      'id': threadId,
      'provider': provider,
      if (externalSessionId != null) 'external_session_id': externalSessionId,
      'headline': headline,
      'state': state,
      'created_at': createdAt,
      'updated_at': updatedAt,
    };
  }

  @override
  Map<String, dynamic> toJsonForProtocol() {
    return {
      '__className__': 'WorkroomFixerThread',
      'id': threadId,
      'provider': provider,
      if (externalSessionId != null) 'external_session_id': externalSessionId,
      'headline': headline,
      'state': state,
      'created_at': createdAt,
      'updated_at': updatedAt,
    };
  }

  @override
  String toString() {
    return _i1.SerializationManager.encode(this);
  }
}

class _Undefined {}

class _WorkroomFixerThreadImpl extends WorkroomFixerThread {
  const _WorkroomFixerThreadImpl({
    required String threadId,
    required String provider,
    String? externalSessionId,
    required String headline,
    required String state,
    required String createdAt,
    required String updatedAt,
  }) : super._(
         threadId: threadId,
         provider: provider,
         externalSessionId: externalSessionId,
         headline: headline,
         state: state,
         createdAt: createdAt,
         updatedAt: updatedAt,
       );

  /// Returns a shallow copy of this [WorkroomFixerThread]
  /// with some or all fields replaced by the given arguments.
  @_i1.useResult
  @override
  WorkroomFixerThread copyWith({
    String? threadId,
    String? provider,
    Object? externalSessionId = _Undefined,
    String? headline,
    String? state,
    String? createdAt,
    String? updatedAt,
  }) {
    return WorkroomFixerThread(
      threadId: threadId ?? this.threadId,
      provider: provider ?? this.provider,
      externalSessionId: externalSessionId is String?
          ? externalSessionId
          : this.externalSessionId,
      headline: headline ?? this.headline,
      state: state ?? this.state,
      createdAt: createdAt ?? this.createdAt,
      updatedAt: updatedAt ?? this.updatedAt,
    );
  }
}
