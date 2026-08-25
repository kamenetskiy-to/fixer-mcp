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

/// Safe error surfaced by the authenticated Workroom transport.
abstract class WorkroomBridgeException
    implements
        _i1.SerializableException,
        _i1.SerializableModel,
        _i1.ProtocolSerialization {
  WorkroomBridgeException._({
    required this.reasonCode,
    required this.message,
  });

  factory WorkroomBridgeException({
    required String reasonCode,
    required String message,
  }) = _WorkroomBridgeExceptionImpl;

  factory WorkroomBridgeException.fromJson(
    Map<String, dynamic> jsonSerialization,
  ) {
    return WorkroomBridgeException(
      reasonCode: jsonSerialization['reasonCode'] as String,
      message: jsonSerialization['message'] as String,
    );
  }

  String reasonCode;

  String message;

  /// Returns a shallow copy of this [WorkroomBridgeException]
  /// with some or all fields replaced by the given arguments.
  @_i1.useResult
  WorkroomBridgeException copyWith({
    String? reasonCode,
    String? message,
  });
  @override
  Map<String, dynamic> toJson() {
    return {
      '__className__': 'WorkroomBridgeException',
      'reasonCode': reasonCode,
      'message': message,
    };
  }

  @override
  Map<String, dynamic> toJsonForProtocol() {
    return {
      '__className__': 'WorkroomBridgeException',
      'reasonCode': reasonCode,
      'message': message,
    };
  }

  @override
  String toString() {
    return 'WorkroomBridgeException(reasonCode: $reasonCode, message: $message)';
  }
}

class _WorkroomBridgeExceptionImpl extends WorkroomBridgeException {
  _WorkroomBridgeExceptionImpl({
    required String reasonCode,
    required String message,
  }) : super._(
         reasonCode: reasonCode,
         message: message,
       );

  /// Returns a shallow copy of this [WorkroomBridgeException]
  /// with some or all fields replaced by the given arguments.
  @_i1.useResult
  @override
  WorkroomBridgeException copyWith({
    String? reasonCode,
    String? message,
  }) {
    return WorkroomBridgeException(
      reasonCode: reasonCode ?? this.reasonCode,
      message: message ?? this.message,
    );
  }
}
