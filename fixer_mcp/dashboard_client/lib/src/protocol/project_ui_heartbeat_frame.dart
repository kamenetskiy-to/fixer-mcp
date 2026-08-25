/* AUTOMATICALLY GENERATED CODE DO NOT MODIFY */
/*   To generate run: "serverpod generate"    */

// ignore_for_file: implementation_imports
// ignore_for_file: library_private_types_in_public_api
// ignore_for_file: non_constant_identifier_names
// ignore_for_file: public_member_api_docs
// ignore_for_file: type_literal_in_constant_pattern
// ignore_for_file: use_super_parameters
// ignore_for_file: invalid_use_of_internal_member

part of 'project_ui_frame.dart';

@_i1.immutable
abstract class ProjectUiHeartbeatFrame extends _i2.ProjectUiFrame
    implements _i1.SerializableModel {
  const ProjectUiHeartbeatFrame._({
    required super.projectId,
    required super.protocolVersion,
    required this.serverTime,
    required this.currentJournalHead,
  });

  const factory ProjectUiHeartbeatFrame({
    required int projectId,
    required int protocolVersion,
    required String serverTime,
    required int currentJournalHead,
  }) = _ProjectUiHeartbeatFrameImpl;

  factory ProjectUiHeartbeatFrame.fromJson(
    Map<String, dynamic> jsonSerialization,
  ) {
    return ProjectUiHeartbeatFrame(
      projectId: jsonSerialization['projectId'] as int,
      protocolVersion: jsonSerialization['protocolVersion'] as int,
      serverTime: jsonSerialization['serverTime'] as String,
      currentJournalHead: jsonSerialization['currentJournalHead'] as int,
    );
  }

  final String serverTime;

  final int currentJournalHead;

  /// Returns a shallow copy of this [ProjectUiHeartbeatFrame]
  /// with some or all fields replaced by the given arguments.
  @override
  @_i1.useResult
  ProjectUiHeartbeatFrame copyWith({
    int? projectId,
    int? protocolVersion,
    String? serverTime,
    int? currentJournalHead,
  });
  @override
  bool operator ==(Object other) {
    return identical(
          other,
          this,
        ) ||
        other.runtimeType == runtimeType &&
            other is ProjectUiHeartbeatFrame &&
            (identical(
                  other.projectId,
                  projectId,
                ) ||
                other.projectId == projectId) &&
            (identical(
                  other.protocolVersion,
                  protocolVersion,
                ) ||
                other.protocolVersion == protocolVersion) &&
            (identical(
                  other.serverTime,
                  serverTime,
                ) ||
                other.serverTime == serverTime) &&
            (identical(
                  other.currentJournalHead,
                  currentJournalHead,
                ) ||
                other.currentJournalHead == currentJournalHead);
  }

  @override
  int get hashCode {
    return Object.hash(
      runtimeType,
      projectId,
      protocolVersion,
      serverTime,
      currentJournalHead,
    );
  }

  @override
  Map<String, dynamic> toJson() {
    return {
      '__className__': 'ProjectUiHeartbeatFrame',
      'projectId': projectId,
      'protocolVersion': protocolVersion,
      'serverTime': serverTime,
      'currentJournalHead': currentJournalHead,
    };
  }

  @override
  String toString() {
    return _i1.SerializationManager.encode(this);
  }
}

class _ProjectUiHeartbeatFrameImpl extends ProjectUiHeartbeatFrame {
  const _ProjectUiHeartbeatFrameImpl({
    required int projectId,
    required int protocolVersion,
    required String serverTime,
    required int currentJournalHead,
  }) : super._(
         projectId: projectId,
         protocolVersion: protocolVersion,
         serverTime: serverTime,
         currentJournalHead: currentJournalHead,
       );

  /// Returns a shallow copy of this [ProjectUiHeartbeatFrame]
  /// with some or all fields replaced by the given arguments.
  @_i1.useResult
  @override
  ProjectUiHeartbeatFrame copyWith({
    int? projectId,
    int? protocolVersion,
    String? serverTime,
    int? currentJournalHead,
  }) {
    return ProjectUiHeartbeatFrame(
      projectId: projectId ?? this.projectId,
      protocolVersion: protocolVersion ?? this.protocolVersion,
      serverTime: serverTime ?? this.serverTime,
      currentJournalHead: currentJournalHead ?? this.currentJournalHead,
    );
  }
}
