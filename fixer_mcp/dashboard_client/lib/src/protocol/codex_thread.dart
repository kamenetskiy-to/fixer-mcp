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

/// A persisted Codex thread bound to an external app-server thread id.
abstract class CodexThread implements _i1.SerializableModel {
  CodexThread._({
    this.id,
    required this.threadId,
    this.title,
    this.cwd,
    this.model,
    this.reasoningEffort,
    this.sandboxMode,
    bool? isActive,
    this.activeTurnId,
    this.activeSince,
    this.lastActivityAt,
    this.lastCompletedAt,
    DateTime? createdAt,
  }) : isActive = isActive ?? false,
       createdAt = createdAt ?? DateTime.now();

  factory CodexThread({
    int? id,
    required String threadId,
    String? title,
    String? cwd,
    String? model,
    String? reasoningEffort,
    String? sandboxMode,
    bool? isActive,
    String? activeTurnId,
    DateTime? activeSince,
    DateTime? lastActivityAt,
    DateTime? lastCompletedAt,
    DateTime? createdAt,
  }) = _CodexThreadImpl;

  factory CodexThread.fromJson(Map<String, dynamic> jsonSerialization) {
    return CodexThread(
      id: jsonSerialization['id'] as int?,
      threadId: jsonSerialization['threadId'] as String,
      title: jsonSerialization['title'] as String?,
      cwd: jsonSerialization['cwd'] as String?,
      model: jsonSerialization['model'] as String?,
      reasoningEffort: jsonSerialization['reasoningEffort'] as String?,
      sandboxMode: jsonSerialization['sandboxMode'] as String?,
      isActive: jsonSerialization['isActive'] == null
          ? null
          : _i1.BoolJsonExtension.fromJson(jsonSerialization['isActive']),
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
      createdAt: jsonSerialization['createdAt'] == null
          ? null
          : _i1.DateTimeJsonExtension.fromJson(jsonSerialization['createdAt']),
    );
  }

  /// The database id, set if the object has been inserted into the
  /// database or if it has been fetched from the database. Otherwise,
  /// the id will be null.
  int? id;

  String threadId;

  String? title;

  String? cwd;

  String? model;

  String? reasoningEffort;

  String? sandboxMode;

  bool isActive;

  String? activeTurnId;

  DateTime? activeSince;

  DateTime? lastActivityAt;

  DateTime? lastCompletedAt;

  DateTime createdAt;

  /// Returns a shallow copy of this [CodexThread]
  /// with some or all fields replaced by the given arguments.
  @_i1.useResult
  CodexThread copyWith({
    int? id,
    String? threadId,
    String? title,
    String? cwd,
    String? model,
    String? reasoningEffort,
    String? sandboxMode,
    bool? isActive,
    String? activeTurnId,
    DateTime? activeSince,
    DateTime? lastActivityAt,
    DateTime? lastCompletedAt,
    DateTime? createdAt,
  });
  @override
  Map<String, dynamic> toJson() {
    return {
      '__className__': 'CodexThread',
      if (id != null) 'id': id,
      'threadId': threadId,
      if (title != null) 'title': title,
      if (cwd != null) 'cwd': cwd,
      if (model != null) 'model': model,
      if (reasoningEffort != null) 'reasoningEffort': reasoningEffort,
      if (sandboxMode != null) 'sandboxMode': sandboxMode,
      'isActive': isActive,
      if (activeTurnId != null) 'activeTurnId': activeTurnId,
      if (activeSince != null) 'activeSince': activeSince?.toJson(),
      if (lastActivityAt != null) 'lastActivityAt': lastActivityAt?.toJson(),
      if (lastCompletedAt != null) 'lastCompletedAt': lastCompletedAt?.toJson(),
      'createdAt': createdAt.toJson(),
    };
  }

  @override
  String toString() {
    return _i1.SerializationManager.encode(this);
  }
}

class _Undefined {}

class _CodexThreadImpl extends CodexThread {
  _CodexThreadImpl({
    int? id,
    required String threadId,
    String? title,
    String? cwd,
    String? model,
    String? reasoningEffort,
    String? sandboxMode,
    bool? isActive,
    String? activeTurnId,
    DateTime? activeSince,
    DateTime? lastActivityAt,
    DateTime? lastCompletedAt,
    DateTime? createdAt,
  }) : super._(
         id: id,
         threadId: threadId,
         title: title,
         cwd: cwd,
         model: model,
         reasoningEffort: reasoningEffort,
         sandboxMode: sandboxMode,
         isActive: isActive,
         activeTurnId: activeTurnId,
         activeSince: activeSince,
         lastActivityAt: lastActivityAt,
         lastCompletedAt: lastCompletedAt,
         createdAt: createdAt,
       );

  /// Returns a shallow copy of this [CodexThread]
  /// with some or all fields replaced by the given arguments.
  @_i1.useResult
  @override
  CodexThread copyWith({
    Object? id = _Undefined,
    String? threadId,
    Object? title = _Undefined,
    Object? cwd = _Undefined,
    Object? model = _Undefined,
    Object? reasoningEffort = _Undefined,
    Object? sandboxMode = _Undefined,
    bool? isActive,
    Object? activeTurnId = _Undefined,
    Object? activeSince = _Undefined,
    Object? lastActivityAt = _Undefined,
    Object? lastCompletedAt = _Undefined,
    DateTime? createdAt,
  }) {
    return CodexThread(
      id: id is int? ? id : this.id,
      threadId: threadId ?? this.threadId,
      title: title is String? ? title : this.title,
      cwd: cwd is String? ? cwd : this.cwd,
      model: model is String? ? model : this.model,
      reasoningEffort: reasoningEffort is String?
          ? reasoningEffort
          : this.reasoningEffort,
      sandboxMode: sandboxMode is String? ? sandboxMode : this.sandboxMode,
      isActive: isActive ?? this.isActive,
      activeTurnId: activeTurnId is String? ? activeTurnId : this.activeTurnId,
      activeSince: activeSince is DateTime? ? activeSince : this.activeSince,
      lastActivityAt: lastActivityAt is DateTime?
          ? lastActivityAt
          : this.lastActivityAt,
      lastCompletedAt: lastCompletedAt is DateTime?
          ? lastCompletedAt
          : this.lastCompletedAt,
      createdAt: createdAt ?? this.createdAt,
    );
  }
}
