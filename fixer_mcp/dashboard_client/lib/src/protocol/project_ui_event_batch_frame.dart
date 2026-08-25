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
abstract class ProjectUiEventBatchFrame extends _i2.ProjectUiFrame
    implements _i1.SerializableModel {
  const ProjectUiEventBatchFrame._({
    required super.projectId,
    required super.protocolVersion,
    required this.events,
  });

  const factory ProjectUiEventBatchFrame({
    required int projectId,
    required int protocolVersion,
    required List<_i3.ProjectUiEvent> events,
  }) = _ProjectUiEventBatchFrameImpl;

  factory ProjectUiEventBatchFrame.fromJson(
    Map<String, dynamic> jsonSerialization,
  ) {
    return ProjectUiEventBatchFrame(
      projectId: jsonSerialization['projectId'] as int,
      protocolVersion: jsonSerialization['protocolVersion'] as int,
      events: _i4.Protocol().deserialize<List<_i3.ProjectUiEvent>>(
        jsonSerialization['events'],
      ),
    );
  }

  final List<_i3.ProjectUiEvent> events;

  /// Returns a shallow copy of this [ProjectUiEventBatchFrame]
  /// with some or all fields replaced by the given arguments.
  @override
  @_i1.useResult
  ProjectUiEventBatchFrame copyWith({
    int? projectId,
    int? protocolVersion,
    List<_i3.ProjectUiEvent>? events,
  });
  @override
  bool operator ==(Object other) {
    return identical(
          other,
          this,
        ) ||
        other.runtimeType == runtimeType &&
            other is ProjectUiEventBatchFrame &&
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
            const _i1.DeepCollectionEquality().equals(
              other.events,
              events,
            );
  }

  @override
  int get hashCode {
    return Object.hash(
      runtimeType,
      projectId,
      protocolVersion,
      const _i1.DeepCollectionEquality().hash(events),
    );
  }

  @override
  Map<String, dynamic> toJson() {
    return {
      '__className__': 'ProjectUiEventBatchFrame',
      'projectId': projectId,
      'protocolVersion': protocolVersion,
      'events': events.toJson(valueToJson: (v) => v.toJson()),
    };
  }

  @override
  String toString() {
    return _i1.SerializationManager.encode(this);
  }
}

class _ProjectUiEventBatchFrameImpl extends ProjectUiEventBatchFrame {
  const _ProjectUiEventBatchFrameImpl({
    required int projectId,
    required int protocolVersion,
    required List<_i3.ProjectUiEvent> events,
  }) : super._(
         projectId: projectId,
         protocolVersion: protocolVersion,
         events: events,
       );

  /// Returns a shallow copy of this [ProjectUiEventBatchFrame]
  /// with some or all fields replaced by the given arguments.
  @_i1.useResult
  @override
  ProjectUiEventBatchFrame copyWith({
    int? projectId,
    int? protocolVersion,
    List<_i3.ProjectUiEvent>? events,
  }) {
    return ProjectUiEventBatchFrame(
      projectId: projectId ?? this.projectId,
      protocolVersion: protocolVersion ?? this.protocolVersion,
      events: events ?? this.events.map((e0) => e0.copyWith()).toList(),
    );
  }
}
