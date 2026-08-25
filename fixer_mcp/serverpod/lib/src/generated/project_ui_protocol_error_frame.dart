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
abstract class ProjectUiProtocolErrorFrame extends _i2.ProjectUiFrame
    implements _i1.SerializableModel, _i1.ProtocolSerialization {
  const ProjectUiProtocolErrorFrame._({
    required super.projectId,
    required super.protocolVersion,
    required this.reasonCode,
    required this.minimumSupportedVersion,
    required this.maximumSupportedVersion,
  });

  const factory ProjectUiProtocolErrorFrame({
    required int projectId,
    required int protocolVersion,
    required String reasonCode,
    required int minimumSupportedVersion,
    required int maximumSupportedVersion,
  }) = _ProjectUiProtocolErrorFrameImpl;

  factory ProjectUiProtocolErrorFrame.fromJson(
    Map<String, dynamic> jsonSerialization,
  ) {
    return ProjectUiProtocolErrorFrame(
      projectId: jsonSerialization['projectId'] as int,
      protocolVersion: jsonSerialization['protocolVersion'] as int,
      reasonCode: jsonSerialization['reasonCode'] as String,
      minimumSupportedVersion:
          jsonSerialization['minimumSupportedVersion'] as int,
      maximumSupportedVersion:
          jsonSerialization['maximumSupportedVersion'] as int,
    );
  }

  final String reasonCode;

  final int minimumSupportedVersion;

  final int maximumSupportedVersion;

  /// Returns a shallow copy of this [ProjectUiProtocolErrorFrame]
  /// with some or all fields replaced by the given arguments.
  @override
  @_i1.useResult
  ProjectUiProtocolErrorFrame copyWith({
    int? projectId,
    int? protocolVersion,
    String? reasonCode,
    int? minimumSupportedVersion,
    int? maximumSupportedVersion,
  });
  @override
  bool operator ==(Object other) {
    return identical(
          other,
          this,
        ) ||
        other.runtimeType == runtimeType &&
            other is ProjectUiProtocolErrorFrame &&
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
                  other.reasonCode,
                  reasonCode,
                ) ||
                other.reasonCode == reasonCode) &&
            (identical(
                  other.minimumSupportedVersion,
                  minimumSupportedVersion,
                ) ||
                other.minimumSupportedVersion == minimumSupportedVersion) &&
            (identical(
                  other.maximumSupportedVersion,
                  maximumSupportedVersion,
                ) ||
                other.maximumSupportedVersion == maximumSupportedVersion);
  }

  @override
  int get hashCode {
    return Object.hash(
      runtimeType,
      projectId,
      protocolVersion,
      reasonCode,
      minimumSupportedVersion,
      maximumSupportedVersion,
    );
  }

  @override
  Map<String, dynamic> toJson() {
    return {
      '__className__': 'ProjectUiProtocolErrorFrame',
      'projectId': projectId,
      'protocolVersion': protocolVersion,
      'reasonCode': reasonCode,
      'minimumSupportedVersion': minimumSupportedVersion,
      'maximumSupportedVersion': maximumSupportedVersion,
    };
  }

  @override
  Map<String, dynamic> toJsonForProtocol() {
    return {
      '__className__': 'ProjectUiProtocolErrorFrame',
      'projectId': projectId,
      'protocolVersion': protocolVersion,
      'reasonCode': reasonCode,
      'minimumSupportedVersion': minimumSupportedVersion,
      'maximumSupportedVersion': maximumSupportedVersion,
    };
  }

  @override
  String toString() {
    return _i1.SerializationManager.encode(this);
  }
}

class _ProjectUiProtocolErrorFrameImpl extends ProjectUiProtocolErrorFrame {
  const _ProjectUiProtocolErrorFrameImpl({
    required int projectId,
    required int protocolVersion,
    required String reasonCode,
    required int minimumSupportedVersion,
    required int maximumSupportedVersion,
  }) : super._(
         projectId: projectId,
         protocolVersion: protocolVersion,
         reasonCode: reasonCode,
         minimumSupportedVersion: minimumSupportedVersion,
         maximumSupportedVersion: maximumSupportedVersion,
       );

  /// Returns a shallow copy of this [ProjectUiProtocolErrorFrame]
  /// with some or all fields replaced by the given arguments.
  @_i1.useResult
  @override
  ProjectUiProtocolErrorFrame copyWith({
    int? projectId,
    int? protocolVersion,
    String? reasonCode,
    int? minimumSupportedVersion,
    int? maximumSupportedVersion,
  }) {
    return ProjectUiProtocolErrorFrame(
      projectId: projectId ?? this.projectId,
      protocolVersion: protocolVersion ?? this.protocolVersion,
      reasonCode: reasonCode ?? this.reasonCode,
      minimumSupportedVersion:
          minimumSupportedVersion ?? this.minimumSupportedVersion,
      maximumSupportedVersion:
          maximumSupportedVersion ?? this.maximumSupportedVersion,
    );
  }
}
