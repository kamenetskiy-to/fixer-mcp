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

/// Provider lane advertised by the single permanent Hands actor.
@_i1.immutable
abstract class WorkroomHandsLane
    implements _i1.SerializableModel, _i1.ProtocolSerialization {
  const WorkroomHandsLane._({
    required this.provider,
    required this.model,
    required this.reasoning,
  });

  const factory WorkroomHandsLane({
    required String provider,
    required String model,
    required String reasoning,
  }) = _WorkroomHandsLaneImpl;

  factory WorkroomHandsLane.fromJson(Map<String, dynamic> jsonSerialization) {
    return WorkroomHandsLane(
      provider: jsonSerialization['provider'] as String,
      model: jsonSerialization['model'] as String,
      reasoning: jsonSerialization['reasoning'] as String,
    );
  }

  final String provider;

  final String model;

  final String reasoning;

  /// Returns a shallow copy of this [WorkroomHandsLane]
  /// with some or all fields replaced by the given arguments.
  @_i1.useResult
  WorkroomHandsLane copyWith({
    String? provider,
    String? model,
    String? reasoning,
  });
  @override
  bool operator ==(Object other) {
    return identical(
          other,
          this,
        ) ||
        other.runtimeType == runtimeType &&
            other is WorkroomHandsLane &&
            (identical(
                  other.provider,
                  provider,
                ) ||
                other.provider == provider) &&
            (identical(
                  other.model,
                  model,
                ) ||
                other.model == model) &&
            (identical(
                  other.reasoning,
                  reasoning,
                ) ||
                other.reasoning == reasoning);
  }

  @override
  int get hashCode {
    return Object.hash(
      runtimeType,
      provider,
      model,
      reasoning,
    );
  }

  @override
  Map<String, dynamic> toJson() {
    return {
      '__className__': 'WorkroomHandsLane',
      'provider': provider,
      'model': model,
      'reasoning': reasoning,
    };
  }

  @override
  Map<String, dynamic> toJsonForProtocol() {
    return {
      '__className__': 'WorkroomHandsLane',
      'provider': provider,
      'model': model,
      'reasoning': reasoning,
    };
  }

  @override
  String toString() {
    return _i1.SerializationManager.encode(this);
  }
}

class _WorkroomHandsLaneImpl extends WorkroomHandsLane {
  const _WorkroomHandsLaneImpl({
    required String provider,
    required String model,
    required String reasoning,
  }) : super._(
         provider: provider,
         model: model,
         reasoning: reasoning,
       );

  /// Returns a shallow copy of this [WorkroomHandsLane]
  /// with some or all fields replaced by the given arguments.
  @_i1.useResult
  @override
  WorkroomHandsLane copyWith({
    String? provider,
    String? model,
    String? reasoning,
  }) {
    return WorkroomHandsLane(
      provider: provider ?? this.provider,
      model: model ?? this.model,
      reasoning: reasoning ?? this.reasoning,
    );
  }
}
