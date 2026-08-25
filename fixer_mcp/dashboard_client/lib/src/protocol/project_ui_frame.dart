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
import 'protocol.dart' as _i2;
import 'project_ui_event.dart' as _i3;
import 'package:fixer_dashboard_client/src/protocol/protocol.dart' as _i4;
part 'project_ui_event_batch_frame.dart';
part 'project_ui_event_frame.dart';
part 'project_ui_heartbeat_frame.dart';
part 'project_ui_protocol_error_frame.dart';

/// Exhaustive Serverpod stream frame hierarchy. Only event frames consume a
/// durable project sequence.
@_i1.immutable
sealed class ProjectUiFrame implements _i1.SerializableModel {
  const ProjectUiFrame({
    required this.projectId,
    required this.protocolVersion,
  });

  final int projectId;

  final int protocolVersion;

  /// Returns a shallow copy of this [ProjectUiFrame]
  /// with some or all fields replaced by the given arguments.
  @_i1.useResult
  ProjectUiFrame copyWith({
    int? projectId,
    int? protocolVersion,
  });
  @override
  bool operator ==(Object other) {
    return identical(
          other,
          this,
        ) ||
        other.runtimeType == runtimeType &&
            other is ProjectUiFrame &&
            (identical(
                  other.projectId,
                  projectId,
                ) ||
                other.projectId == projectId) &&
            (identical(
                  other.protocolVersion,
                  protocolVersion,
                ) ||
                other.protocolVersion == protocolVersion);
  }

  @override
  int get hashCode {
    return Object.hash(
      runtimeType,
      projectId,
      protocolVersion,
    );
  }
}
