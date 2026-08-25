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

/// Current runtime activity for a persisted Codex thread.
abstract class CodexThreadActivity implements _i1.SerializableModel {
  CodexThreadActivity._({
    required this.codexThreadId,
    required this.isActive,
    this.activeTurnId,
    this.activeSince,
    this.lastActivityAt,
    this.lastCompletedAt,
  });

  factory CodexThreadActivity({
    required int codexThreadId,
    required bool isActive,
    String? activeTurnId,
    DateTime? activeSince,
    DateTime? lastActivityAt,
    DateTime? lastCompletedAt,
  }) = _CodexThreadActivityImpl;

  factory CodexThreadActivity.fromJson(Map<String, dynamic> jsonSerialization) {
    return CodexThreadActivity(
      codexThreadId: jsonSerialization['codexThreadId'] as int,
      isActive: _i1.BoolJsonExtension.fromJson(jsonSerialization['isActive']),
      activeTurnId: jsonSerialization['activeTurnId'] as String?,
      activeSince: jsonSerialization['activeSince'] == null
          ? null
          : _i1.DateTimeJsonExtension.fromJson(
              jsonSerialization['activeSince'],
            ),
      lastActivityAt: jsonSerialization['lastActivityAt'] == null
          ? null
          : _i1.DateTimeJsonExtension.fromJson(
              jsonSerialization['lastActivityAt'],
            ),
      lastCompletedAt: jsonSerialization['lastCompletedAt'] == null
          ? null
          : _i1.DateTimeJsonExtension.fromJson(
              jsonSerialization['lastCompletedAt'],
            ),
    );
  }

  int codexThreadId;

  bool isActive;

  String? activeTurnId;

  DateTime? activeSince;

  DateTime? lastActivityAt;

  DateTime? lastCompletedAt;

  /// Returns a shallow copy of this [CodexThreadActivity]
  /// with some or all fields replaced by the given arguments.
  @_i1.useResult
  CodexThreadActivity copyWith({
    int? codexThreadId,
    bool? isActive,
    String? activeTurnId,
    DateTime? activeSince,
    DateTime? lastActivityAt,
    DateTime? lastCompletedAt,
  });
  @override
  Map<String, dynamic> toJson() {
    return {
      '__className__': 'CodexThreadActivity',
      'codexThreadId': codexThreadId,
      'isActive': isActive,
      if (activeTurnId != null) 'activeTurnId': activeTurnId,
      if (activeSince != null) 'activeSince': activeSince?.toJson(),
      if (lastActivityAt != null) 'lastActivityAt': lastActivityAt?.toJson(),
      if (lastCompletedAt != null) 'lastCompletedAt': lastCompletedAt?.toJson(),
    };
  }

  @override
  String toString() {
    return _i1.SerializationManager.encode(this);
  }
}

class _Undefined {}

class _CodexThreadActivityImpl extends CodexThreadActivity {
  _CodexThreadActivityImpl({
    required int codexThreadId,
    required bool isActive,
    String? activeTurnId,
    DateTime? activeSince,
    DateTime? lastActivityAt,
    DateTime? lastCompletedAt,
  }) : super._(
         codexThreadId: codexThreadId,
         isActive: isActive,
         activeTurnId: activeTurnId,
         activeSince: activeSince,
         lastActivityAt: lastActivityAt,
         lastCompletedAt: lastCompletedAt,
       );

  /// Returns a shallow copy of this [CodexThreadActivity]
  /// with some or all fields replaced by the given arguments.
  @_i1.useResult
  @override
  CodexThreadActivity copyWith({
    int? codexThreadId,
    bool? isActive,
    Object? activeTurnId = _Undefined,
    Object? activeSince = _Undefined,
    Object? lastActivityAt = _Undefined,
    Object? lastCompletedAt = _Undefined,
  }) {
    return CodexThreadActivity(
      codexThreadId: codexThreadId ?? this.codexThreadId,
      isActive: isActive ?? this.isActive,
      activeTurnId: activeTurnId is String? ? activeTurnId : this.activeTurnId,
      activeSince: activeSince is DateTime? ? activeSince : this.activeSince,
      lastActivityAt: lastActivityAt is DateTime?
          ? lastActivityAt
          : this.lastActivityAt,
      lastCompletedAt: lastCompletedAt is DateTime?
          ? lastCompletedAt
          : this.lastCompletedAt,
    );
  }
}
