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
import 'workroom_fixer_thread.dart' as _i2;
import 'workroom_fixer_turn.dart' as _i3;
import 'workroom_surface.dart' as _i4;
import 'workroom_hands_lane.dart' as _i5;
import 'workroom_hands_instruction.dart' as _i6;
import 'package:fixer_dashboard_client/src/protocol/protocol.dart' as _i7;

/// Consistent initial Project Workroom read model and its journal watermark.
@_i1.immutable
abstract class ProjectWorkroomSnapshot implements _i1.SerializableModel {
  const ProjectWorkroomSnapshot._({
    required this.projectId,
    required this.projectName,
    required this.projectCwd,
    required this.protocolVersion,
    required this.watermarkSeq,
    required this.threads,
    this.selectedThreadId,
    required this.turns,
    this.activeSurface,
    required this.handsActorId,
    required this.handsDisplayName,
    required this.handsAuthorityState,
    required this.handsDefaultLane,
    required this.handsLanes,
    required this.handsMailbox,
    this.activeInstruction,
    required this.capabilities,
  });

  const factory ProjectWorkroomSnapshot({
    required int projectId,
    required String projectName,
    required String projectCwd,
    required int protocolVersion,
    required int watermarkSeq,
    required List<_i2.WorkroomFixerThread> threads,
    String? selectedThreadId,
    required List<_i3.WorkroomFixerTurn> turns,
    _i4.WorkroomSurface? activeSurface,
    required String handsActorId,
    required String handsDisplayName,
    required String handsAuthorityState,
    required String handsDefaultLane,
    required List<_i5.WorkroomHandsLane> handsLanes,
    required List<_i6.WorkroomHandsInstruction> handsMailbox,
    _i6.WorkroomHandsInstruction? activeInstruction,
    required List<String> capabilities,
  }) = _ProjectWorkroomSnapshotImpl;

  factory ProjectWorkroomSnapshot.fromJson(
    Map<String, dynamic> jsonSerialization,
  ) {
    return ProjectWorkroomSnapshot(
      projectId: jsonSerialization['project_id'] as int,
      projectName: jsonSerialization['project_name'] as String,
      projectCwd: jsonSerialization['project_cwd'] as String,
      protocolVersion: jsonSerialization['protocol_version'] as int,
      watermarkSeq: jsonSerialization['watermark_seq'] as int,
      threads: _i7.Protocol().deserialize<List<_i2.WorkroomFixerThread>>(
        jsonSerialization['threads'],
      ),
      selectedThreadId: jsonSerialization['selected_thread_id'] as String?,
      turns: _i7.Protocol().deserialize<List<_i3.WorkroomFixerTurn>>(
        jsonSerialization['turns'],
      ),
      activeSurface: jsonSerialization['active_surface'] == null
          ? null
          : _i7.Protocol().deserialize<_i4.WorkroomSurface>(
              jsonSerialization['active_surface'],
            ),
      handsActorId: jsonSerialization['hands_actor_id'] as String,
      handsDisplayName: jsonSerialization['hands_display_name'] as String,
      handsAuthorityState: jsonSerialization['hands_authority_state'] as String,
      handsDefaultLane: jsonSerialization['hands_default_lane'] as String,
      handsLanes: _i7.Protocol().deserialize<List<_i5.WorkroomHandsLane>>(
        jsonSerialization['hands_lanes'],
      ),
      handsMailbox: _i7.Protocol()
          .deserialize<List<_i6.WorkroomHandsInstruction>>(
            jsonSerialization['hands_mailbox'],
          ),
      activeInstruction: jsonSerialization['active_instruction'] == null
          ? null
          : _i7.Protocol().deserialize<_i6.WorkroomHandsInstruction>(
              jsonSerialization['active_instruction'],
            ),
      capabilities: _i7.Protocol().deserialize<List<String>>(
        jsonSerialization['capabilities'],
      ),
    );
  }

  final int projectId;

  final String projectName;

  final String projectCwd;

  final int protocolVersion;

  final int watermarkSeq;

  final List<_i2.WorkroomFixerThread> threads;

  final String? selectedThreadId;

  final List<_i3.WorkroomFixerTurn> turns;

  final _i4.WorkroomSurface? activeSurface;

  final String handsActorId;

  final String handsDisplayName;

  final String handsAuthorityState;

  final String handsDefaultLane;

  final List<_i5.WorkroomHandsLane> handsLanes;

  final List<_i6.WorkroomHandsInstruction> handsMailbox;

  final _i6.WorkroomHandsInstruction? activeInstruction;

  final List<String> capabilities;

  /// Returns a shallow copy of this [ProjectWorkroomSnapshot]
  /// with some or all fields replaced by the given arguments.
  @_i1.useResult
  ProjectWorkroomSnapshot copyWith({
    int? projectId,
    String? projectName,
    String? projectCwd,
    int? protocolVersion,
    int? watermarkSeq,
    List<_i2.WorkroomFixerThread>? threads,
    String? selectedThreadId,
    List<_i3.WorkroomFixerTurn>? turns,
    _i4.WorkroomSurface? activeSurface,
    String? handsActorId,
    String? handsDisplayName,
    String? handsAuthorityState,
    String? handsDefaultLane,
    List<_i5.WorkroomHandsLane>? handsLanes,
    List<_i6.WorkroomHandsInstruction>? handsMailbox,
    _i6.WorkroomHandsInstruction? activeInstruction,
    List<String>? capabilities,
  });
  @override
  bool operator ==(Object other) {
    return identical(
          other,
          this,
        ) ||
        other.runtimeType == runtimeType &&
            other is ProjectWorkroomSnapshot &&
            (identical(
                  other.projectId,
                  projectId,
                ) ||
                other.projectId == projectId) &&
            (identical(
                  other.projectName,
                  projectName,
                ) ||
                other.projectName == projectName) &&
            (identical(
                  other.projectCwd,
                  projectCwd,
                ) ||
                other.projectCwd == projectCwd) &&
            (identical(
                  other.protocolVersion,
                  protocolVersion,
                ) ||
                other.protocolVersion == protocolVersion) &&
            (identical(
                  other.watermarkSeq,
                  watermarkSeq,
                ) ||
                other.watermarkSeq == watermarkSeq) &&
            const _i1.DeepCollectionEquality().equals(
              other.threads,
              threads,
            ) &&
            (identical(
                  other.selectedThreadId,
                  selectedThreadId,
                ) ||
                other.selectedThreadId == selectedThreadId) &&
            const _i1.DeepCollectionEquality().equals(
              other.turns,
              turns,
            ) &&
            (identical(
                  other.activeSurface,
                  activeSurface,
                ) ||
                other.activeSurface == activeSurface) &&
            (identical(
                  other.handsActorId,
                  handsActorId,
                ) ||
                other.handsActorId == handsActorId) &&
            (identical(
                  other.handsDisplayName,
                  handsDisplayName,
                ) ||
                other.handsDisplayName == handsDisplayName) &&
            (identical(
                  other.handsAuthorityState,
                  handsAuthorityState,
                ) ||
                other.handsAuthorityState == handsAuthorityState) &&
            (identical(
                  other.handsDefaultLane,
                  handsDefaultLane,
                ) ||
                other.handsDefaultLane == handsDefaultLane) &&
            const _i1.DeepCollectionEquality().equals(
              other.handsLanes,
              handsLanes,
            ) &&
            const _i1.DeepCollectionEquality().equals(
              other.handsMailbox,
              handsMailbox,
            ) &&
            (identical(
                  other.activeInstruction,
                  activeInstruction,
                ) ||
                other.activeInstruction == activeInstruction) &&
            const _i1.DeepCollectionEquality().equals(
              other.capabilities,
              capabilities,
            );
  }

  @override
  int get hashCode {
    return Object.hash(
      runtimeType,
      projectId,
      projectName,
      projectCwd,
      protocolVersion,
      watermarkSeq,
      const _i1.DeepCollectionEquality().hash(threads),
      selectedThreadId,
      const _i1.DeepCollectionEquality().hash(turns),
      activeSurface,
      handsActorId,
      handsDisplayName,
      handsAuthorityState,
      handsDefaultLane,
      const _i1.DeepCollectionEquality().hash(handsLanes),
      const _i1.DeepCollectionEquality().hash(handsMailbox),
      activeInstruction,
      const _i1.DeepCollectionEquality().hash(capabilities),
    );
  }

  @override
  Map<String, dynamic> toJson() {
    return {
      '__className__': 'ProjectWorkroomSnapshot',
      'project_id': projectId,
      'project_name': projectName,
      'project_cwd': projectCwd,
      'protocol_version': protocolVersion,
      'watermark_seq': watermarkSeq,
      'threads': threads.toJson(valueToJson: (v) => v.toJson()),
      if (selectedThreadId != null) 'selected_thread_id': selectedThreadId,
      'turns': turns.toJson(valueToJson: (v) => v.toJson()),
      if (activeSurface != null) 'active_surface': activeSurface?.toJson(),
      'hands_actor_id': handsActorId,
      'hands_display_name': handsDisplayName,
      'hands_authority_state': handsAuthorityState,
      'hands_default_lane': handsDefaultLane,
      'hands_lanes': handsLanes.toJson(valueToJson: (v) => v.toJson()),
      'hands_mailbox': handsMailbox.toJson(valueToJson: (v) => v.toJson()),
      if (activeInstruction != null)
        'active_instruction': activeInstruction?.toJson(),
      'capabilities': capabilities.toJson(),
    };
  }

  @override
  String toString() {
    return _i1.SerializationManager.encode(this);
  }
}

class _Undefined {}

class _ProjectWorkroomSnapshotImpl extends ProjectWorkroomSnapshot {
  const _ProjectWorkroomSnapshotImpl({
    required int projectId,
    required String projectName,
    required String projectCwd,
    required int protocolVersion,
    required int watermarkSeq,
    required List<_i2.WorkroomFixerThread> threads,
    String? selectedThreadId,
    required List<_i3.WorkroomFixerTurn> turns,
    _i4.WorkroomSurface? activeSurface,
    required String handsActorId,
    required String handsDisplayName,
    required String handsAuthorityState,
    required String handsDefaultLane,
    required List<_i5.WorkroomHandsLane> handsLanes,
    required List<_i6.WorkroomHandsInstruction> handsMailbox,
    _i6.WorkroomHandsInstruction? activeInstruction,
    required List<String> capabilities,
  }) : super._(
         projectId: projectId,
         projectName: projectName,
         projectCwd: projectCwd,
         protocolVersion: protocolVersion,
         watermarkSeq: watermarkSeq,
         threads: threads,
         selectedThreadId: selectedThreadId,
         turns: turns,
         activeSurface: activeSurface,
         handsActorId: handsActorId,
         handsDisplayName: handsDisplayName,
         handsAuthorityState: handsAuthorityState,
         handsDefaultLane: handsDefaultLane,
         handsLanes: handsLanes,
         handsMailbox: handsMailbox,
         activeInstruction: activeInstruction,
         capabilities: capabilities,
       );

  /// Returns a shallow copy of this [ProjectWorkroomSnapshot]
  /// with some or all fields replaced by the given arguments.
  @_i1.useResult
  @override
  ProjectWorkroomSnapshot copyWith({
    int? projectId,
    String? projectName,
    String? projectCwd,
    int? protocolVersion,
    int? watermarkSeq,
    List<_i2.WorkroomFixerThread>? threads,
    Object? selectedThreadId = _Undefined,
    List<_i3.WorkroomFixerTurn>? turns,
    Object? activeSurface = _Undefined,
    String? handsActorId,
    String? handsDisplayName,
    String? handsAuthorityState,
    String? handsDefaultLane,
    List<_i5.WorkroomHandsLane>? handsLanes,
    List<_i6.WorkroomHandsInstruction>? handsMailbox,
    Object? activeInstruction = _Undefined,
    List<String>? capabilities,
  }) {
    return ProjectWorkroomSnapshot(
      projectId: projectId ?? this.projectId,
      projectName: projectName ?? this.projectName,
      projectCwd: projectCwd ?? this.projectCwd,
      protocolVersion: protocolVersion ?? this.protocolVersion,
      watermarkSeq: watermarkSeq ?? this.watermarkSeq,
      threads: threads ?? this.threads.map((e0) => e0.copyWith()).toList(),
      selectedThreadId: selectedThreadId is String?
          ? selectedThreadId
          : this.selectedThreadId,
      turns: turns ?? this.turns.map((e0) => e0.copyWith()).toList(),
      activeSurface: activeSurface is _i4.WorkroomSurface?
          ? activeSurface
          : this.activeSurface?.copyWith(),
      handsActorId: handsActorId ?? this.handsActorId,
      handsDisplayName: handsDisplayName ?? this.handsDisplayName,
      handsAuthorityState: handsAuthorityState ?? this.handsAuthorityState,
      handsDefaultLane: handsDefaultLane ?? this.handsDefaultLane,
      handsLanes:
          handsLanes ?? this.handsLanes.map((e0) => e0.copyWith()).toList(),
      handsMailbox:
          handsMailbox ?? this.handsMailbox.map((e0) => e0.copyWith()).toList(),
      activeInstruction: activeInstruction is _i6.WorkroomHandsInstruction?
          ? activeInstruction
          : this.activeInstruction?.copyWith(),
      capabilities: capabilities ?? this.capabilities.map((e0) => e0).toList(),
    );
  }
}
