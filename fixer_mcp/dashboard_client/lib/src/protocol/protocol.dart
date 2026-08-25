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
import 'client_auth_response.dart' as _i2;
import 'client_profile.dart' as _i3;
import 'codex_message.dart' as _i4;
import 'codex_thread.dart' as _i5;
import 'codex_thread_activity.dart' as _i6;
import 'codex_turn_event.dart' as _i7;
import 'codex_turn_persist_request.dart' as _i8;
import 'command_receipt.dart' as _i9;
import 'fixer_turn_receipt.dart' as _i10;
import 'genui_action_receipt.dart' as _i11;
import 'genui_action_request.dart' as _i12;
import 'hands_instruction_receipt.dart' as _i13;
import 'hands_instruction_request.dart' as _i14;
import 'order.dart' as _i15;
import 'project_ui_event.dart' as _i16;
import 'project_ui_frame.dart' as _i17;
import 'project_workroom_snapshot.dart' as _i18;
import 'revision.dart' as _i19;
import 'workroom_bridge_exception.dart' as _i20;
import 'workroom_fixer_thread.dart' as _i21;
import 'workroom_fixer_turn.dart' as _i22;
import 'workroom_hands_instruction.dart' as _i23;
import 'workroom_hands_lane.dart' as _i24;
import 'workroom_surface.dart' as _i25;
import 'package:fixer_dashboard_client/src/protocol/order.dart' as _i26;
import 'package:fixer_dashboard_client/src/protocol/revision.dart' as _i27;
import 'package:serverpod_auth_core_client/serverpod_auth_core_client.dart'
    as _i28;
export 'client_auth_response.dart';
export 'client_profile.dart';
export 'codex_message.dart';
export 'codex_thread.dart';
export 'codex_thread_activity.dart';
export 'codex_turn_event.dart';
export 'codex_turn_persist_request.dart';
export 'command_receipt.dart';
export 'fixer_turn_receipt.dart';
export 'genui_action_receipt.dart';
export 'genui_action_request.dart';
export 'hands_instruction_receipt.dart';
export 'hands_instruction_request.dart';
export 'order.dart';
export 'project_ui_event.dart';
export 'project_ui_frame.dart';
export 'project_workroom_snapshot.dart';
export 'revision.dart';
export 'workroom_bridge_exception.dart';
export 'workroom_fixer_thread.dart';
export 'workroom_fixer_turn.dart';
export 'workroom_hands_instruction.dart';
export 'workroom_hands_lane.dart';
export 'workroom_surface.dart';
export 'client.dart';

class Protocol extends _i1.SerializationManager {
  Protocol._();

  factory Protocol() => _instance;

  static final Protocol _instance = Protocol._();

  static String? getClassNameFromObjectJson(dynamic data) {
    if (data is! Map) return null;
    final className = data['__className__'] as String?;
    return className;
  }

  @override
  T deserialize<T>(
    dynamic data, [
    Type? t,
  ]) {
    t ??= T;

    final dataClassName = getClassNameFromObjectJson(data);
    if (dataClassName != null && dataClassName != getClassNameForType(t)) {
      try {
        return deserializeByClassName({
          'className': dataClassName,
          'data': data,
        });
      } on FormatException catch (_) {
        // If the className is not recognized (e.g., older client receiving
        // data with a new subtype), fall back to deserializing without the
        // className, using the expected type T.
      }
    }

    if (t == _i2.ClientAuthResponse) {
      return _i2.ClientAuthResponse.fromJson(data) as T;
    }
    if (t == _i3.ClientProfile) {
      return _i3.ClientProfile.fromJson(data) as T;
    }
    if (t == _i4.CodexMessage) {
      return _i4.CodexMessage.fromJson(data) as T;
    }
    if (t == _i5.CodexThread) {
      return _i5.CodexThread.fromJson(data) as T;
    }
    if (t == _i6.CodexThreadActivity) {
      return _i6.CodexThreadActivity.fromJson(data) as T;
    }
    if (t == _i7.CodexTurnEvent) {
      return _i7.CodexTurnEvent.fromJson(data) as T;
    }
    if (t == _i8.CodexTurnPersistRequest) {
      return _i8.CodexTurnPersistRequest.fromJson(data) as T;
    }
    if (t == _i9.CommandReceipt) {
      return _i9.CommandReceipt.fromJson(data) as T;
    }
    if (t == _i10.FixerTurnReceipt) {
      return _i10.FixerTurnReceipt.fromJson(data) as T;
    }
    if (t == _i11.GenuiActionReceipt) {
      return _i11.GenuiActionReceipt.fromJson(data) as T;
    }
    if (t == _i12.GenuiActionRequest) {
      return _i12.GenuiActionRequest.fromJson(data) as T;
    }
    if (t == _i13.HandsInstructionReceipt) {
      return _i13.HandsInstructionReceipt.fromJson(data) as T;
    }
    if (t == _i14.HandsInstructionRequest) {
      return _i14.HandsInstructionRequest.fromJson(data) as T;
    }
    if (t == _i15.Order) {
      return _i15.Order.fromJson(data) as T;
    }
    if (t == _i16.ProjectUiEvent) {
      return _i16.ProjectUiEvent.fromJson(data) as T;
    }
    if (t == _i17.ProjectUiEventBatchFrame) {
      return _i17.ProjectUiEventBatchFrame.fromJson(data) as T;
    }
    if (t == _i17.ProjectUiEventFrame) {
      return _i17.ProjectUiEventFrame.fromJson(data) as T;
    }
    if (t == _i17.ProjectUiHeartbeatFrame) {
      return _i17.ProjectUiHeartbeatFrame.fromJson(data) as T;
    }
    if (t == _i17.ProjectUiProtocolErrorFrame) {
      return _i17.ProjectUiProtocolErrorFrame.fromJson(data) as T;
    }
    if (t == _i18.ProjectWorkroomSnapshot) {
      return _i18.ProjectWorkroomSnapshot.fromJson(data) as T;
    }
    if (t == _i19.Revision) {
      return _i19.Revision.fromJson(data) as T;
    }
    if (t == _i20.WorkroomBridgeException) {
      return _i20.WorkroomBridgeException.fromJson(data) as T;
    }
    if (t == _i21.WorkroomFixerThread) {
      return _i21.WorkroomFixerThread.fromJson(data) as T;
    }
    if (t == _i22.WorkroomFixerTurn) {
      return _i22.WorkroomFixerTurn.fromJson(data) as T;
    }
    if (t == _i23.WorkroomHandsInstruction) {
      return _i23.WorkroomHandsInstruction.fromJson(data) as T;
    }
    if (t == _i24.WorkroomHandsLane) {
      return _i24.WorkroomHandsLane.fromJson(data) as T;
    }
    if (t == _i25.WorkroomSurface) {
      return _i25.WorkroomSurface.fromJson(data) as T;
    }
    if (t == _i1.getType<_i2.ClientAuthResponse?>()) {
      return (data != null ? _i2.ClientAuthResponse.fromJson(data) : null) as T;
    }
    if (t == _i1.getType<_i3.ClientProfile?>()) {
      return (data != null ? _i3.ClientProfile.fromJson(data) : null) as T;
    }
    if (t == _i1.getType<_i4.CodexMessage?>()) {
      return (data != null ? _i4.CodexMessage.fromJson(data) : null) as T;
    }
    if (t == _i1.getType<_i5.CodexThread?>()) {
      return (data != null ? _i5.CodexThread.fromJson(data) : null) as T;
    }
    if (t == _i1.getType<_i6.CodexThreadActivity?>()) {
      return (data != null ? _i6.CodexThreadActivity.fromJson(data) : null)
          as T;
    }
    if (t == _i1.getType<_i7.CodexTurnEvent?>()) {
      return (data != null ? _i7.CodexTurnEvent.fromJson(data) : null) as T;
    }
    if (t == _i1.getType<_i8.CodexTurnPersistRequest?>()) {
      return (data != null ? _i8.CodexTurnPersistRequest.fromJson(data) : null)
          as T;
    }
    if (t == _i1.getType<_i9.CommandReceipt?>()) {
      return (data != null ? _i9.CommandReceipt.fromJson(data) : null) as T;
    }
    if (t == _i1.getType<_i10.FixerTurnReceipt?>()) {
      return (data != null ? _i10.FixerTurnReceipt.fromJson(data) : null) as T;
    }
    if (t == _i1.getType<_i11.GenuiActionReceipt?>()) {
      return (data != null ? _i11.GenuiActionReceipt.fromJson(data) : null)
          as T;
    }
    if (t == _i1.getType<_i12.GenuiActionRequest?>()) {
      return (data != null ? _i12.GenuiActionRequest.fromJson(data) : null)
          as T;
    }
    if (t == _i1.getType<_i13.HandsInstructionReceipt?>()) {
      return (data != null ? _i13.HandsInstructionReceipt.fromJson(data) : null)
          as T;
    }
    if (t == _i1.getType<_i14.HandsInstructionRequest?>()) {
      return (data != null ? _i14.HandsInstructionRequest.fromJson(data) : null)
          as T;
    }
    if (t == _i1.getType<_i15.Order?>()) {
      return (data != null ? _i15.Order.fromJson(data) : null) as T;
    }
    if (t == _i1.getType<_i16.ProjectUiEvent?>()) {
      return (data != null ? _i16.ProjectUiEvent.fromJson(data) : null) as T;
    }
    if (t == _i1.getType<_i17.ProjectUiEventBatchFrame?>()) {
      return (data != null
              ? _i17.ProjectUiEventBatchFrame.fromJson(data)
              : null)
          as T;
    }
    if (t == _i1.getType<_i17.ProjectUiEventFrame?>()) {
      return (data != null ? _i17.ProjectUiEventFrame.fromJson(data) : null)
          as T;
    }
    if (t == _i1.getType<_i17.ProjectUiHeartbeatFrame?>()) {
      return (data != null ? _i17.ProjectUiHeartbeatFrame.fromJson(data) : null)
          as T;
    }
    if (t == _i1.getType<_i17.ProjectUiProtocolErrorFrame?>()) {
      return (data != null
              ? _i17.ProjectUiProtocolErrorFrame.fromJson(data)
              : null)
          as T;
    }
    if (t == _i1.getType<_i18.ProjectWorkroomSnapshot?>()) {
      return (data != null ? _i18.ProjectWorkroomSnapshot.fromJson(data) : null)
          as T;
    }
    if (t == _i1.getType<_i19.Revision?>()) {
      return (data != null ? _i19.Revision.fromJson(data) : null) as T;
    }
    if (t == _i1.getType<_i20.WorkroomBridgeException?>()) {
      return (data != null ? _i20.WorkroomBridgeException.fromJson(data) : null)
          as T;
    }
    if (t == _i1.getType<_i21.WorkroomFixerThread?>()) {
      return (data != null ? _i21.WorkroomFixerThread.fromJson(data) : null)
          as T;
    }
    if (t == _i1.getType<_i22.WorkroomFixerTurn?>()) {
      return (data != null ? _i22.WorkroomFixerTurn.fromJson(data) : null) as T;
    }
    if (t == _i1.getType<_i23.WorkroomHandsInstruction?>()) {
      return (data != null
              ? _i23.WorkroomHandsInstruction.fromJson(data)
              : null)
          as T;
    }
    if (t == _i1.getType<_i24.WorkroomHandsLane?>()) {
      return (data != null ? _i24.WorkroomHandsLane.fromJson(data) : null) as T;
    }
    if (t == _i1.getType<_i25.WorkroomSurface?>()) {
      return (data != null ? _i25.WorkroomSurface.fromJson(data) : null) as T;
    }
    if (t == List<String>) {
      return (data as List).map((e) => deserialize<String>(e)).toList() as T;
    }
    if (t == List<_i16.ProjectUiEvent>) {
      return (data as List)
              .map((e) => deserialize<_i16.ProjectUiEvent>(e))
              .toList()
          as T;
    }
    if (t == List<_i21.WorkroomFixerThread>) {
      return (data as List)
              .map((e) => deserialize<_i21.WorkroomFixerThread>(e))
              .toList()
          as T;
    }
    if (t == List<_i22.WorkroomFixerTurn>) {
      return (data as List)
              .map((e) => deserialize<_i22.WorkroomFixerTurn>(e))
              .toList()
          as T;
    }
    if (t == List<_i24.WorkroomHandsLane>) {
      return (data as List)
              .map((e) => deserialize<_i24.WorkroomHandsLane>(e))
              .toList()
          as T;
    }
    if (t == List<_i23.WorkroomHandsInstruction>) {
      return (data as List)
              .map((e) => deserialize<_i23.WorkroomHandsInstruction>(e))
              .toList()
          as T;
    }
    if (t == _i1.getType<List<String>?>()) {
      return (data != null
              ? (data as List).map((e) => deserialize<String>(e)).toList()
              : null)
          as T;
    }
    if (t == List<String>) {
      return (data as List).map((e) => deserialize<String>(e)).toList() as T;
    }
    if (t == Map<String, dynamic>) {
      return (data as Map).map(
            (k, v) => MapEntry(deserialize<String>(k), deserialize<dynamic>(v)),
          )
          as T;
    }
    if (t == List<_i26.Order>) {
      return (data as List).map((e) => deserialize<_i26.Order>(e)).toList()
          as T;
    }
    if (t == List<_i27.Revision>) {
      return (data as List).map((e) => deserialize<_i27.Revision>(e)).toList()
          as T;
    }
    if (t == List<Map<String, dynamic>>) {
      return (data as List)
              .map((e) => deserialize<Map<String, dynamic>>(e))
              .toList()
          as T;
    }
    if (t == _i1.getType<List<String>?>()) {
      return (data != null
              ? (data as List).map((e) => deserialize<String>(e)).toList()
              : null)
          as T;
    }
    try {
      return _i28.Protocol().deserialize<T>(data, t);
    } on _i1.DeserializationTypeNotFoundException catch (_) {}
    return super.deserialize<T>(data, t);
  }

  static String? getClassNameForType(Type type) {
    return switch (type) {
      _i2.ClientAuthResponse => 'ClientAuthResponse',
      _i3.ClientProfile => 'ClientProfile',
      _i4.CodexMessage => 'CodexMessage',
      _i5.CodexThread => 'CodexThread',
      _i6.CodexThreadActivity => 'CodexThreadActivity',
      _i7.CodexTurnEvent => 'CodexTurnEvent',
      _i8.CodexTurnPersistRequest => 'CodexTurnPersistRequest',
      _i9.CommandReceipt => 'CommandReceipt',
      _i10.FixerTurnReceipt => 'FixerTurnReceipt',
      _i11.GenuiActionReceipt => 'GenuiActionReceipt',
      _i12.GenuiActionRequest => 'GenuiActionRequest',
      _i13.HandsInstructionReceipt => 'HandsInstructionReceipt',
      _i14.HandsInstructionRequest => 'HandsInstructionRequest',
      _i15.Order => 'Order',
      _i16.ProjectUiEvent => 'ProjectUiEvent',
      _i17.ProjectUiEventBatchFrame => 'ProjectUiEventBatchFrame',
      _i17.ProjectUiEventFrame => 'ProjectUiEventFrame',
      _i17.ProjectUiHeartbeatFrame => 'ProjectUiHeartbeatFrame',
      _i17.ProjectUiProtocolErrorFrame => 'ProjectUiProtocolErrorFrame',
      _i18.ProjectWorkroomSnapshot => 'ProjectWorkroomSnapshot',
      _i19.Revision => 'Revision',
      _i20.WorkroomBridgeException => 'WorkroomBridgeException',
      _i21.WorkroomFixerThread => 'WorkroomFixerThread',
      _i22.WorkroomFixerTurn => 'WorkroomFixerTurn',
      _i23.WorkroomHandsInstruction => 'WorkroomHandsInstruction',
      _i24.WorkroomHandsLane => 'WorkroomHandsLane',
      _i25.WorkroomSurface => 'WorkroomSurface',
      _ => null,
    };
  }

  @override
  String? getClassNameForObject(Object? data) {
    String? className = super.getClassNameForObject(data);
    if (className != null) return className;

    if (data is Map<String, dynamic> && data['__className__'] is String) {
      return (data['__className__'] as String).replaceFirst(
        'fixer_dashboard.',
        '',
      );
    }

    switch (data) {
      case _i2.ClientAuthResponse():
        return 'ClientAuthResponse';
      case _i3.ClientProfile():
        return 'ClientProfile';
      case _i4.CodexMessage():
        return 'CodexMessage';
      case _i5.CodexThread():
        return 'CodexThread';
      case _i6.CodexThreadActivity():
        return 'CodexThreadActivity';
      case _i7.CodexTurnEvent():
        return 'CodexTurnEvent';
      case _i8.CodexTurnPersistRequest():
        return 'CodexTurnPersistRequest';
      case _i9.CommandReceipt():
        return 'CommandReceipt';
      case _i10.FixerTurnReceipt():
        return 'FixerTurnReceipt';
      case _i11.GenuiActionReceipt():
        return 'GenuiActionReceipt';
      case _i12.GenuiActionRequest():
        return 'GenuiActionRequest';
      case _i13.HandsInstructionReceipt():
        return 'HandsInstructionReceipt';
      case _i14.HandsInstructionRequest():
        return 'HandsInstructionRequest';
      case _i15.Order():
        return 'Order';
      case _i16.ProjectUiEvent():
        return 'ProjectUiEvent';
      case _i17.ProjectUiEventBatchFrame():
        return 'ProjectUiEventBatchFrame';
      case _i17.ProjectUiEventFrame():
        return 'ProjectUiEventFrame';
      case _i17.ProjectUiHeartbeatFrame():
        return 'ProjectUiHeartbeatFrame';
      case _i17.ProjectUiProtocolErrorFrame():
        return 'ProjectUiProtocolErrorFrame';
      case _i18.ProjectWorkroomSnapshot():
        return 'ProjectWorkroomSnapshot';
      case _i19.Revision():
        return 'Revision';
      case _i20.WorkroomBridgeException():
        return 'WorkroomBridgeException';
      case _i21.WorkroomFixerThread():
        return 'WorkroomFixerThread';
      case _i22.WorkroomFixerTurn():
        return 'WorkroomFixerTurn';
      case _i23.WorkroomHandsInstruction():
        return 'WorkroomHandsInstruction';
      case _i24.WorkroomHandsLane():
        return 'WorkroomHandsLane';
      case _i25.WorkroomSurface():
        return 'WorkroomSurface';
    }
    className = _i28.Protocol().getClassNameForObject(data);
    if (className != null) {
      return 'serverpod_auth_core.$className';
    }
    return null;
  }

  @override
  dynamic deserializeByClassName(Map<String, dynamic> data) {
    var dataClassName = data['className'];
    if (dataClassName is! String) {
      return super.deserializeByClassName(data);
    }
    if (dataClassName == 'ClientAuthResponse') {
      return deserialize<_i2.ClientAuthResponse>(data['data']);
    }
    if (dataClassName == 'ClientProfile') {
      return deserialize<_i3.ClientProfile>(data['data']);
    }
    if (dataClassName == 'CodexMessage') {
      return deserialize<_i4.CodexMessage>(data['data']);
    }
    if (dataClassName == 'CodexThread') {
      return deserialize<_i5.CodexThread>(data['data']);
    }
    if (dataClassName == 'CodexThreadActivity') {
      return deserialize<_i6.CodexThreadActivity>(data['data']);
    }
    if (dataClassName == 'CodexTurnEvent') {
      return deserialize<_i7.CodexTurnEvent>(data['data']);
    }
    if (dataClassName == 'CodexTurnPersistRequest') {
      return deserialize<_i8.CodexTurnPersistRequest>(data['data']);
    }
    if (dataClassName == 'CommandReceipt') {
      return deserialize<_i9.CommandReceipt>(data['data']);
    }
    if (dataClassName == 'FixerTurnReceipt') {
      return deserialize<_i10.FixerTurnReceipt>(data['data']);
    }
    if (dataClassName == 'GenuiActionReceipt') {
      return deserialize<_i11.GenuiActionReceipt>(data['data']);
    }
    if (dataClassName == 'GenuiActionRequest') {
      return deserialize<_i12.GenuiActionRequest>(data['data']);
    }
    if (dataClassName == 'HandsInstructionReceipt') {
      return deserialize<_i13.HandsInstructionReceipt>(data['data']);
    }
    if (dataClassName == 'HandsInstructionRequest') {
      return deserialize<_i14.HandsInstructionRequest>(data['data']);
    }
    if (dataClassName == 'Order') {
      return deserialize<_i15.Order>(data['data']);
    }
    if (dataClassName == 'ProjectUiEvent') {
      return deserialize<_i16.ProjectUiEvent>(data['data']);
    }
    if (dataClassName == 'ProjectUiEventBatchFrame') {
      return deserialize<_i17.ProjectUiEventBatchFrame>(data['data']);
    }
    if (dataClassName == 'ProjectUiEventFrame') {
      return deserialize<_i17.ProjectUiEventFrame>(data['data']);
    }
    if (dataClassName == 'ProjectUiHeartbeatFrame') {
      return deserialize<_i17.ProjectUiHeartbeatFrame>(data['data']);
    }
    if (dataClassName == 'ProjectUiProtocolErrorFrame') {
      return deserialize<_i17.ProjectUiProtocolErrorFrame>(data['data']);
    }
    if (dataClassName == 'ProjectWorkroomSnapshot') {
      return deserialize<_i18.ProjectWorkroomSnapshot>(data['data']);
    }
    if (dataClassName == 'Revision') {
      return deserialize<_i19.Revision>(data['data']);
    }
    if (dataClassName == 'WorkroomBridgeException') {
      return deserialize<_i20.WorkroomBridgeException>(data['data']);
    }
    if (dataClassName == 'WorkroomFixerThread') {
      return deserialize<_i21.WorkroomFixerThread>(data['data']);
    }
    if (dataClassName == 'WorkroomFixerTurn') {
      return deserialize<_i22.WorkroomFixerTurn>(data['data']);
    }
    if (dataClassName == 'WorkroomHandsInstruction') {
      return deserialize<_i23.WorkroomHandsInstruction>(data['data']);
    }
    if (dataClassName == 'WorkroomHandsLane') {
      return deserialize<_i24.WorkroomHandsLane>(data['data']);
    }
    if (dataClassName == 'WorkroomSurface') {
      return deserialize<_i25.WorkroomSurface>(data['data']);
    }
    if (dataClassName.startsWith('serverpod_auth_core.')) {
      data['className'] = dataClassName.substring(20);
      return _i28.Protocol().deserializeByClassName(data);
    }
    return super.deserializeByClassName(data);
  }

  /// Maps any `Record`s known to this [Protocol] to their JSON representation
  ///
  /// Throws in case the record type is not known.
  ///
  /// This method will return `null` (only) for `null` inputs.
  Map<String, dynamic>? mapRecordToJson(Record? record) {
    if (record == null) {
      return null;
    }
    try {
      return _i28.Protocol().mapRecordToJson(record);
    } catch (_) {}
    throw Exception('Unsupported record type ${record.runtimeType}');
  }
}
