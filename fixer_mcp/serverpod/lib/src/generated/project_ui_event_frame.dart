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
abstract class ProjectUiEventFrame extends _i2.ProjectUiFrame
    implements _i1.SerializableModel, _i1.ProtocolSerialization {
  const ProjectUiEventFrame._({
    required super.projectId,
    required super.protocolVersion,
    required this.event,
  });

  const factory ProjectUiEventFrame({
    required int projectId,
    required int protocolVersion,
    required _i3.ProjectUiEvent event,
  }) = _ProjectUiEventFrameImpl;

  factory ProjectUiEventFrame.fromJson(Map<String, dynamic> jsonSerialization) {
    return ProjectUiEventFrame(
      projectId: jsonSerialization['projectId'] as int,
      protocolVersion: jsonSerialization['protocolVersion'] as int,
      event: _i4.Protocol().deserialize<_i3.ProjectUiEvent>(
        jsonSerialization['event'],
      ),
    );
  }

  final _i3.ProjectUiEvent event;

  /// Returns a shallow copy of this [ProjectUiEventFrame]
  /// with some or all fields replaced by the given arguments.
  @override
  @_i1.useResult
  ProjectUiEventFrame copyWith({
    int? projectId,
    int? protocolVersion,
    _i3.ProjectUiEvent? event,
  });
  @override
  bool operator ==(Object other) {
    return identical(
          other,
          this,
        ) ||
        other.runtimeType == runtimeType &&
            other is ProjectUiEventFrame &&
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
                  other.event,
                  event,
                ) ||
                other.event == event);
  }

  @override
  int get hashCode {
    return Object.hash(
      runtimeType,
      projectId,
      protocolVersion,
      event,
    );
  }

  @override
  Map<String, dynamic> toJson() {
    return {
      '__className__': 'ProjectUiEventFrame',
      'projectId': projectId,
      'protocolVersion': protocolVersion,
      'event': event.toJson(),
    };
  }

  @override
  Map<String, dynamic> toJsonForProtocol() {
    return {
      '__className__': 'ProjectUiEventFrame',
      'projectId': projectId,
      'protocolVersion': protocolVersion,
      'event': event.toJsonForProtocol(),
    };
  }

  @override
  String toString() {
    return _i1.SerializationManager.encode(this);
  }
}

class _ProjectUiEventFrameImpl extends ProjectUiEventFrame {
  const _ProjectUiEventFrameImpl({
    required int projectId,
    required int protocolVersion,
    required _i3.ProjectUiEvent event,
  }) : super._(
         projectId: projectId,
         protocolVersion: protocolVersion,
         event: event,
       );

  /// Returns a shallow copy of this [ProjectUiEventFrame]
  /// with some or all fields replaced by the given arguments.
  @_i1.useResult
  @override
  ProjectUiEventFrame copyWith({
    int? projectId,
    int? protocolVersion,
    _i3.ProjectUiEvent? event,
  }) {
    return ProjectUiEventFrame(
      projectId: projectId ?? this.projectId,
      protocolVersion: protocolVersion ?? this.protocolVersion,
      event: event ?? this.event.copyWith(),
    );
  }
}
