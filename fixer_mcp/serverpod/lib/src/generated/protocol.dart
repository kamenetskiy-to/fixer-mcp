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
import 'package:serverpod/protocol.dart' as _i2;
import 'package:serverpod_auth_core_server/serverpod_auth_core_server.dart'
    as _i3;
import 'client_auth_response.dart' as _i4;
import 'client_profile.dart' as _i5;
import 'client_user.dart' as _i6;
import 'codex_message.dart' as _i7;
import 'codex_thread.dart' as _i8;
import 'codex_thread_activity.dart' as _i9;
import 'codex_turn_event.dart' as _i10;
import 'codex_turn_persist_request.dart' as _i11;
import 'command_receipt.dart' as _i12;
import 'fixer_turn_receipt.dart' as _i13;
import 'genui_action_receipt.dart' as _i14;
import 'genui_action_request.dart' as _i15;
import 'hands_instruction_receipt.dart' as _i16;
import 'hands_instruction_request.dart' as _i17;
import 'order.dart' as _i18;
import 'project_ui_event.dart' as _i19;
import 'project_ui_frame.dart' as _i20;
import 'project_workroom_snapshot.dart' as _i21;
import 'revision.dart' as _i22;
import 'workroom_bridge_exception.dart' as _i23;
import 'workroom_fixer_thread.dart' as _i24;
import 'workroom_fixer_turn.dart' as _i25;
import 'workroom_hands_instruction.dart' as _i26;
import 'workroom_hands_lane.dart' as _i27;
import 'workroom_surface.dart' as _i28;
import 'package:fixer_dashboard_server/src/generated/order.dart' as _i29;
import 'package:fixer_dashboard_server/src/generated/revision.dart' as _i30;
export 'client_auth_response.dart';
export 'client_profile.dart';
export 'client_user.dart';
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

class Protocol extends _i1.SerializationManagerServer {
  Protocol._();

  factory Protocol() => _instance;

  static final Protocol _instance = Protocol._();

  static final List<_i2.TableDefinition> targetTableDefinitions = [
    _i2.TableDefinition(
      name: 'client',
      dartName: 'ClientUser',
      schema: 'public',
      module: 'fixer_dashboard',
      columns: [
        _i2.ColumnDefinition(
          name: 'id',
          columnType: _i2.ColumnType.uuid,
          isNullable: false,
          dartType: 'UuidValue?',
          columnDefault: 'gen_random_uuid_v7()',
        ),
        _i2.ColumnDefinition(
          name: 'email',
          columnType: _i2.ColumnType.text,
          isNullable: false,
          dartType: 'String',
        ),
        _i2.ColumnDefinition(
          name: 'hashed_password',
          columnType: _i2.ColumnType.text,
          isNullable: false,
          dartType: 'String',
        ),
        _i2.ColumnDefinition(
          name: 'display_name',
          columnType: _i2.ColumnType.text,
          isNullable: false,
          dartType: 'String',
        ),
        _i2.ColumnDefinition(
          name: 'created_at',
          columnType: _i2.ColumnType.timestampWithoutTimeZone,
          isNullable: false,
          dartType: 'DateTime',
        ),
      ],
      foreignKeys: [],
      indexes: [
        _i2.IndexDefinition(
          indexName: 'client_pkey',
          tableSpace: null,
          elements: [
            _i2.IndexElementDefinition(
              type: _i2.IndexElementDefinitionType.column,
              definition: 'id',
            ),
          ],
          type: 'btree',
          isUnique: true,
          isPrimary: true,
        ),
        _i2.IndexDefinition(
          indexName: 'client_email_idx',
          tableSpace: null,
          elements: [
            _i2.IndexElementDefinition(
              type: _i2.IndexElementDefinitionType.column,
              definition: 'email',
            ),
          ],
          type: 'btree',
          isUnique: true,
          isPrimary: false,
        ),
      ],
      managed: true,
    ),
    _i2.TableDefinition(
      name: 'codex_message',
      dartName: 'CodexMessage',
      schema: 'public',
      module: 'fixer_dashboard',
      columns: [
        _i2.ColumnDefinition(
          name: 'id',
          columnType: _i2.ColumnType.bigint,
          isNullable: false,
          dartType: 'int?',
          columnDefault: 'nextval(\'codex_message_id_seq\'::regclass)',
        ),
        _i2.ColumnDefinition(
          name: 'codexThreadId',
          columnType: _i2.ColumnType.bigint,
          isNullable: false,
          dartType: 'int',
        ),
        _i2.ColumnDefinition(
          name: 'role',
          columnType: _i2.ColumnType.text,
          isNullable: false,
          dartType: 'String',
        ),
        _i2.ColumnDefinition(
          name: 'text',
          columnType: _i2.ColumnType.text,
          isNullable: false,
          dartType: 'String',
        ),
        _i2.ColumnDefinition(
          name: 'turnId',
          columnType: _i2.ColumnType.text,
          isNullable: true,
          dartType: 'String?',
        ),
        _i2.ColumnDefinition(
          name: 'createdAt',
          columnType: _i2.ColumnType.timestampWithoutTimeZone,
          isNullable: false,
          dartType: 'DateTime',
          columnDefault: 'CURRENT_TIMESTAMP',
        ),
      ],
      foreignKeys: [
        _i2.ForeignKeyDefinition(
          constraintName: 'codex_message_fk_0',
          columns: ['codexThreadId'],
          referenceTable: 'codex_thread',
          referenceTableSchema: 'public',
          referenceColumns: ['id'],
          onUpdate: _i2.ForeignKeyAction.noAction,
          onDelete: _i2.ForeignKeyAction.cascade,
          matchType: null,
        ),
      ],
      indexes: [
        _i2.IndexDefinition(
          indexName: 'codex_message_pkey',
          tableSpace: null,
          elements: [
            _i2.IndexElementDefinition(
              type: _i2.IndexElementDefinitionType.column,
              definition: 'id',
            ),
          ],
          type: 'btree',
          isUnique: true,
          isPrimary: true,
        ),
        _i2.IndexDefinition(
          indexName: 'codex_message_thread_created_idx',
          tableSpace: null,
          elements: [
            _i2.IndexElementDefinition(
              type: _i2.IndexElementDefinitionType.column,
              definition: 'codexThreadId',
            ),
            _i2.IndexElementDefinition(
              type: _i2.IndexElementDefinitionType.column,
              definition: 'createdAt',
            ),
          ],
          type: 'btree',
          isUnique: false,
          isPrimary: false,
        ),
      ],
      managed: true,
    ),
    _i2.TableDefinition(
      name: 'codex_thread',
      dartName: 'CodexThread',
      schema: 'public',
      module: 'fixer_dashboard',
      columns: [
        _i2.ColumnDefinition(
          name: 'id',
          columnType: _i2.ColumnType.bigint,
          isNullable: false,
          dartType: 'int?',
          columnDefault: 'nextval(\'codex_thread_id_seq\'::regclass)',
        ),
        _i2.ColumnDefinition(
          name: 'threadId',
          columnType: _i2.ColumnType.text,
          isNullable: false,
          dartType: 'String',
        ),
        _i2.ColumnDefinition(
          name: 'title',
          columnType: _i2.ColumnType.text,
          isNullable: true,
          dartType: 'String?',
        ),
        _i2.ColumnDefinition(
          name: 'cwd',
          columnType: _i2.ColumnType.text,
          isNullable: true,
          dartType: 'String?',
        ),
        _i2.ColumnDefinition(
          name: 'model',
          columnType: _i2.ColumnType.text,
          isNullable: true,
          dartType: 'String?',
        ),
        _i2.ColumnDefinition(
          name: 'reasoningEffort',
          columnType: _i2.ColumnType.text,
          isNullable: true,
          dartType: 'String?',
        ),
        _i2.ColumnDefinition(
          name: 'sandboxMode',
          columnType: _i2.ColumnType.text,
          isNullable: true,
          dartType: 'String?',
        ),
        _i2.ColumnDefinition(
          name: 'isActive',
          columnType: _i2.ColumnType.boolean,
          isNullable: false,
          dartType: 'bool',
          columnDefault: 'false',
        ),
        _i2.ColumnDefinition(
          name: 'activeTurnId',
          columnType: _i2.ColumnType.text,
          isNullable: true,
          dartType: 'String?',
        ),
        _i2.ColumnDefinition(
          name: 'activeSince',
          columnType: _i2.ColumnType.timestampWithoutTimeZone,
          isNullable: true,
          dartType: 'DateTime?',
        ),
        _i2.ColumnDefinition(
          name: 'lastActivityAt',
          columnType: _i2.ColumnType.timestampWithoutTimeZone,
          isNullable: true,
          dartType: 'DateTime?',
        ),
        _i2.ColumnDefinition(
          name: 'lastCompletedAt',
          columnType: _i2.ColumnType.timestampWithoutTimeZone,
          isNullable: true,
          dartType: 'DateTime?',
        ),
        _i2.ColumnDefinition(
          name: 'createdAt',
          columnType: _i2.ColumnType.timestampWithoutTimeZone,
          isNullable: false,
          dartType: 'DateTime',
          columnDefault: 'CURRENT_TIMESTAMP',
        ),
      ],
      foreignKeys: [],
      indexes: [
        _i2.IndexDefinition(
          indexName: 'codex_thread_pkey',
          tableSpace: null,
          elements: [
            _i2.IndexElementDefinition(
              type: _i2.IndexElementDefinitionType.column,
              definition: 'id',
            ),
          ],
          type: 'btree',
          isUnique: true,
          isPrimary: true,
        ),
        _i2.IndexDefinition(
          indexName: 'codex_thread_thread_id_unique_idx',
          tableSpace: null,
          elements: [
            _i2.IndexElementDefinition(
              type: _i2.IndexElementDefinitionType.column,
              definition: 'threadId',
            ),
          ],
          type: 'btree',
          isUnique: true,
          isPrimary: false,
        ),
      ],
      managed: true,
    ),
    _i2.TableDefinition(
      name: 'codex_turn_event',
      dartName: 'CodexTurnEvent',
      schema: 'public',
      module: 'fixer_dashboard',
      columns: [
        _i2.ColumnDefinition(
          name: 'id',
          columnType: _i2.ColumnType.bigint,
          isNullable: false,
          dartType: 'int?',
          columnDefault: 'nextval(\'codex_turn_event_id_seq\'::regclass)',
        ),
        _i2.ColumnDefinition(
          name: 'codexThreadId',
          columnType: _i2.ColumnType.bigint,
          isNullable: false,
          dartType: 'int',
        ),
        _i2.ColumnDefinition(
          name: 'turnId',
          columnType: _i2.ColumnType.text,
          isNullable: false,
          dartType: 'String',
        ),
        _i2.ColumnDefinition(
          name: 'method',
          columnType: _i2.ColumnType.text,
          isNullable: false,
          dartType: 'String',
        ),
        _i2.ColumnDefinition(
          name: 'json',
          columnType: _i2.ColumnType.text,
          isNullable: false,
          dartType: 'String',
        ),
        _i2.ColumnDefinition(
          name: 'createdAt',
          columnType: _i2.ColumnType.timestampWithoutTimeZone,
          isNullable: false,
          dartType: 'DateTime',
          columnDefault: 'CURRENT_TIMESTAMP',
        ),
      ],
      foreignKeys: [
        _i2.ForeignKeyDefinition(
          constraintName: 'codex_turn_event_fk_0',
          columns: ['codexThreadId'],
          referenceTable: 'codex_thread',
          referenceTableSchema: 'public',
          referenceColumns: ['id'],
          onUpdate: _i2.ForeignKeyAction.noAction,
          onDelete: _i2.ForeignKeyAction.cascade,
          matchType: null,
        ),
      ],
      indexes: [
        _i2.IndexDefinition(
          indexName: 'codex_turn_event_pkey',
          tableSpace: null,
          elements: [
            _i2.IndexElementDefinition(
              type: _i2.IndexElementDefinitionType.column,
              definition: 'id',
            ),
          ],
          type: 'btree',
          isUnique: true,
          isPrimary: true,
        ),
        _i2.IndexDefinition(
          indexName: 'codex_turn_event_turn_id_id_idx',
          tableSpace: null,
          elements: [
            _i2.IndexElementDefinition(
              type: _i2.IndexElementDefinitionType.column,
              definition: 'turnId',
            ),
            _i2.IndexElementDefinition(
              type: _i2.IndexElementDefinitionType.column,
              definition: 'id',
            ),
          ],
          type: 'btree',
          isUnique: false,
          isPrimary: false,
        ),
      ],
      managed: true,
    ),
    _i2.TableDefinition(
      name: 'order',
      dartName: 'Order',
      schema: 'public',
      module: 'fixer_dashboard',
      columns: [
        _i2.ColumnDefinition(
          name: 'id',
          columnType: _i2.ColumnType.bigint,
          isNullable: false,
          dartType: 'int?',
          columnDefault: 'nextval(\'order_id_seq\'::regclass)',
        ),
        _i2.ColumnDefinition(
          name: 'clientId',
          columnType: _i2.ColumnType.uuid,
          isNullable: false,
          dartType: 'UuidValue',
        ),
        _i2.ColumnDefinition(
          name: 'projectDescription',
          columnType: _i2.ColumnType.text,
          isNullable: false,
          dartType: 'String',
        ),
        _i2.ColumnDefinition(
          name: 'budgetCents',
          columnType: _i2.ColumnType.bigint,
          isNullable: false,
          dartType: 'int',
        ),
        _i2.ColumnDefinition(
          name: 'status',
          columnType: _i2.ColumnType.text,
          isNullable: false,
          dartType: 'String',
        ),
        _i2.ColumnDefinition(
          name: 'assignedProjectId',
          columnType: _i2.ColumnType.bigint,
          isNullable: true,
          dartType: 'int?',
        ),
        _i2.ColumnDefinition(
          name: 'createdAt',
          columnType: _i2.ColumnType.timestampWithoutTimeZone,
          isNullable: false,
          dartType: 'DateTime',
        ),
        _i2.ColumnDefinition(
          name: 'updatedAt',
          columnType: _i2.ColumnType.timestampWithoutTimeZone,
          isNullable: false,
          dartType: 'DateTime',
        ),
        _i2.ColumnDefinition(
          name: 'title',
          columnType: _i2.ColumnType.text,
          isNullable: false,
          dartType: 'String',
        ),
        _i2.ColumnDefinition(
          name: 'description',
          columnType: _i2.ColumnType.text,
          isNullable: false,
          dartType: 'String',
        ),
      ],
      foreignKeys: [],
      indexes: [
        _i2.IndexDefinition(
          indexName: 'order_pkey',
          tableSpace: null,
          elements: [
            _i2.IndexElementDefinition(
              type: _i2.IndexElementDefinitionType.column,
              definition: 'id',
            ),
          ],
          type: 'btree',
          isUnique: true,
          isPrimary: true,
        ),
        _i2.IndexDefinition(
          indexName: 'order_client_updated_idx',
          tableSpace: null,
          elements: [
            _i2.IndexElementDefinition(
              type: _i2.IndexElementDefinitionType.column,
              definition: 'clientId',
            ),
            _i2.IndexElementDefinition(
              type: _i2.IndexElementDefinitionType.column,
              definition: 'updatedAt',
            ),
          ],
          type: 'btree',
          isUnique: false,
          isPrimary: false,
        ),
        _i2.IndexDefinition(
          indexName: 'order_status_updated_idx',
          tableSpace: null,
          elements: [
            _i2.IndexElementDefinition(
              type: _i2.IndexElementDefinitionType.column,
              definition: 'status',
            ),
            _i2.IndexElementDefinition(
              type: _i2.IndexElementDefinitionType.column,
              definition: 'updatedAt',
            ),
          ],
          type: 'btree',
          isUnique: false,
          isPrimary: false,
        ),
      ],
      managed: true,
    ),
    _i2.TableDefinition(
      name: 'revision',
      dartName: 'Revision',
      schema: 'public',
      module: 'fixer_dashboard',
      columns: [
        _i2.ColumnDefinition(
          name: 'id',
          columnType: _i2.ColumnType.bigint,
          isNullable: false,
          dartType: 'int?',
          columnDefault: 'nextval(\'revision_id_seq\'::regclass)',
        ),
        _i2.ColumnDefinition(
          name: 'orderId',
          columnType: _i2.ColumnType.bigint,
          isNullable: false,
          dartType: 'int',
        ),
        _i2.ColumnDefinition(
          name: 'revisionNumber',
          columnType: _i2.ColumnType.bigint,
          isNullable: false,
          dartType: 'int',
        ),
        _i2.ColumnDefinition(
          name: 'revisionText',
          columnType: _i2.ColumnType.text,
          isNullable: false,
          dartType: 'String',
        ),
        _i2.ColumnDefinition(
          name: 'attachmentUrls',
          columnType: _i2.ColumnType.json,
          isNullable: true,
          dartType: 'List<String>?',
        ),
        _i2.ColumnDefinition(
          name: 'resultSummary',
          columnType: _i2.ColumnType.text,
          isNullable: true,
          dartType: 'String?',
        ),
        _i2.ColumnDefinition(
          name: 'status',
          columnType: _i2.ColumnType.text,
          isNullable: false,
          dartType: 'String',
        ),
        _i2.ColumnDefinition(
          name: 'description',
          columnType: _i2.ColumnType.text,
          isNullable: false,
          dartType: 'String',
        ),
        _i2.ColumnDefinition(
          name: 'branchName',
          columnType: _i2.ColumnType.text,
          isNullable: true,
          dartType: 'String?',
        ),
        _i2.ColumnDefinition(
          name: 'previewUrl',
          columnType: _i2.ColumnType.text,
          isNullable: true,
          dartType: 'String?',
        ),
        _i2.ColumnDefinition(
          name: 'createdAt',
          columnType: _i2.ColumnType.timestampWithoutTimeZone,
          isNullable: false,
          dartType: 'DateTime',
        ),
        _i2.ColumnDefinition(
          name: 'updatedAt',
          columnType: _i2.ColumnType.timestampWithoutTimeZone,
          isNullable: false,
          dartType: 'DateTime',
        ),
      ],
      foreignKeys: [],
      indexes: [
        _i2.IndexDefinition(
          indexName: 'revision_pkey',
          tableSpace: null,
          elements: [
            _i2.IndexElementDefinition(
              type: _i2.IndexElementDefinitionType.column,
              definition: 'id',
            ),
          ],
          type: 'btree',
          isUnique: true,
          isPrimary: true,
        ),
        _i2.IndexDefinition(
          indexName: 'revision_order_number_idx',
          tableSpace: null,
          elements: [
            _i2.IndexElementDefinition(
              type: _i2.IndexElementDefinitionType.column,
              definition: 'orderId',
            ),
            _i2.IndexElementDefinition(
              type: _i2.IndexElementDefinitionType.column,
              definition: 'revisionNumber',
            ),
          ],
          type: 'btree',
          isUnique: true,
          isPrimary: false,
        ),
        _i2.IndexDefinition(
          indexName: 'revision_order_updated_idx',
          tableSpace: null,
          elements: [
            _i2.IndexElementDefinition(
              type: _i2.IndexElementDefinitionType.column,
              definition: 'orderId',
            ),
            _i2.IndexElementDefinition(
              type: _i2.IndexElementDefinitionType.column,
              definition: 'updatedAt',
            ),
          ],
          type: 'btree',
          isUnique: false,
          isPrimary: false,
        ),
      ],
      managed: true,
    ),
    ..._i3.Protocol.targetTableDefinitions,
    ..._i2.Protocol.targetTableDefinitions,
  ];

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

    if (t == _i4.ClientAuthResponse) {
      return _i4.ClientAuthResponse.fromJson(data) as T;
    }
    if (t == _i5.ClientProfile) {
      return _i5.ClientProfile.fromJson(data) as T;
    }
    if (t == _i6.ClientUser) {
      return _i6.ClientUser.fromJson(data) as T;
    }
    if (t == _i7.CodexMessage) {
      return _i7.CodexMessage.fromJson(data) as T;
    }
    if (t == _i8.CodexThread) {
      return _i8.CodexThread.fromJson(data) as T;
    }
    if (t == _i9.CodexThreadActivity) {
      return _i9.CodexThreadActivity.fromJson(data) as T;
    }
    if (t == _i10.CodexTurnEvent) {
      return _i10.CodexTurnEvent.fromJson(data) as T;
    }
    if (t == _i11.CodexTurnPersistRequest) {
      return _i11.CodexTurnPersistRequest.fromJson(data) as T;
    }
    if (t == _i12.CommandReceipt) {
      return _i12.CommandReceipt.fromJson(data) as T;
    }
    if (t == _i13.FixerTurnReceipt) {
      return _i13.FixerTurnReceipt.fromJson(data) as T;
    }
    if (t == _i14.GenuiActionReceipt) {
      return _i14.GenuiActionReceipt.fromJson(data) as T;
    }
    if (t == _i15.GenuiActionRequest) {
      return _i15.GenuiActionRequest.fromJson(data) as T;
    }
    if (t == _i16.HandsInstructionReceipt) {
      return _i16.HandsInstructionReceipt.fromJson(data) as T;
    }
    if (t == _i17.HandsInstructionRequest) {
      return _i17.HandsInstructionRequest.fromJson(data) as T;
    }
    if (t == _i18.Order) {
      return _i18.Order.fromJson(data) as T;
    }
    if (t == _i19.ProjectUiEvent) {
      return _i19.ProjectUiEvent.fromJson(data) as T;
    }
    if (t == _i20.ProjectUiEventBatchFrame) {
      return _i20.ProjectUiEventBatchFrame.fromJson(data) as T;
    }
    if (t == _i20.ProjectUiEventFrame) {
      return _i20.ProjectUiEventFrame.fromJson(data) as T;
    }
    if (t == _i20.ProjectUiHeartbeatFrame) {
      return _i20.ProjectUiHeartbeatFrame.fromJson(data) as T;
    }
    if (t == _i20.ProjectUiProtocolErrorFrame) {
      return _i20.ProjectUiProtocolErrorFrame.fromJson(data) as T;
    }
    if (t == _i21.ProjectWorkroomSnapshot) {
      return _i21.ProjectWorkroomSnapshot.fromJson(data) as T;
    }
    if (t == _i22.Revision) {
      return _i22.Revision.fromJson(data) as T;
    }
    if (t == _i23.WorkroomBridgeException) {
      return _i23.WorkroomBridgeException.fromJson(data) as T;
    }
    if (t == _i24.WorkroomFixerThread) {
      return _i24.WorkroomFixerThread.fromJson(data) as T;
    }
    if (t == _i25.WorkroomFixerTurn) {
      return _i25.WorkroomFixerTurn.fromJson(data) as T;
    }
    if (t == _i26.WorkroomHandsInstruction) {
      return _i26.WorkroomHandsInstruction.fromJson(data) as T;
    }
    if (t == _i27.WorkroomHandsLane) {
      return _i27.WorkroomHandsLane.fromJson(data) as T;
    }
    if (t == _i28.WorkroomSurface) {
      return _i28.WorkroomSurface.fromJson(data) as T;
    }
    if (t == _i1.getType<_i4.ClientAuthResponse?>()) {
      return (data != null ? _i4.ClientAuthResponse.fromJson(data) : null) as T;
    }
    if (t == _i1.getType<_i5.ClientProfile?>()) {
      return (data != null ? _i5.ClientProfile.fromJson(data) : null) as T;
    }
    if (t == _i1.getType<_i6.ClientUser?>()) {
      return (data != null ? _i6.ClientUser.fromJson(data) : null) as T;
    }
    if (t == _i1.getType<_i7.CodexMessage?>()) {
      return (data != null ? _i7.CodexMessage.fromJson(data) : null) as T;
    }
    if (t == _i1.getType<_i8.CodexThread?>()) {
      return (data != null ? _i8.CodexThread.fromJson(data) : null) as T;
    }
    if (t == _i1.getType<_i9.CodexThreadActivity?>()) {
      return (data != null ? _i9.CodexThreadActivity.fromJson(data) : null)
          as T;
    }
    if (t == _i1.getType<_i10.CodexTurnEvent?>()) {
      return (data != null ? _i10.CodexTurnEvent.fromJson(data) : null) as T;
    }
    if (t == _i1.getType<_i11.CodexTurnPersistRequest?>()) {
      return (data != null ? _i11.CodexTurnPersistRequest.fromJson(data) : null)
          as T;
    }
    if (t == _i1.getType<_i12.CommandReceipt?>()) {
      return (data != null ? _i12.CommandReceipt.fromJson(data) : null) as T;
    }
    if (t == _i1.getType<_i13.FixerTurnReceipt?>()) {
      return (data != null ? _i13.FixerTurnReceipt.fromJson(data) : null) as T;
    }
    if (t == _i1.getType<_i14.GenuiActionReceipt?>()) {
      return (data != null ? _i14.GenuiActionReceipt.fromJson(data) : null)
          as T;
    }
    if (t == _i1.getType<_i15.GenuiActionRequest?>()) {
      return (data != null ? _i15.GenuiActionRequest.fromJson(data) : null)
          as T;
    }
    if (t == _i1.getType<_i16.HandsInstructionReceipt?>()) {
      return (data != null ? _i16.HandsInstructionReceipt.fromJson(data) : null)
          as T;
    }
    if (t == _i1.getType<_i17.HandsInstructionRequest?>()) {
      return (data != null ? _i17.HandsInstructionRequest.fromJson(data) : null)
          as T;
    }
    if (t == _i1.getType<_i18.Order?>()) {
      return (data != null ? _i18.Order.fromJson(data) : null) as T;
    }
    if (t == _i1.getType<_i19.ProjectUiEvent?>()) {
      return (data != null ? _i19.ProjectUiEvent.fromJson(data) : null) as T;
    }
    if (t == _i1.getType<_i20.ProjectUiEventBatchFrame?>()) {
      return (data != null
              ? _i20.ProjectUiEventBatchFrame.fromJson(data)
              : null)
          as T;
    }
    if (t == _i1.getType<_i20.ProjectUiEventFrame?>()) {
      return (data != null ? _i20.ProjectUiEventFrame.fromJson(data) : null)
          as T;
    }
    if (t == _i1.getType<_i20.ProjectUiHeartbeatFrame?>()) {
      return (data != null ? _i20.ProjectUiHeartbeatFrame.fromJson(data) : null)
          as T;
    }
    if (t == _i1.getType<_i20.ProjectUiProtocolErrorFrame?>()) {
      return (data != null
              ? _i20.ProjectUiProtocolErrorFrame.fromJson(data)
              : null)
          as T;
    }
    if (t == _i1.getType<_i21.ProjectWorkroomSnapshot?>()) {
      return (data != null ? _i21.ProjectWorkroomSnapshot.fromJson(data) : null)
          as T;
    }
    if (t == _i1.getType<_i22.Revision?>()) {
      return (data != null ? _i22.Revision.fromJson(data) : null) as T;
    }
    if (t == _i1.getType<_i23.WorkroomBridgeException?>()) {
      return (data != null ? _i23.WorkroomBridgeException.fromJson(data) : null)
          as T;
    }
    if (t == _i1.getType<_i24.WorkroomFixerThread?>()) {
      return (data != null ? _i24.WorkroomFixerThread.fromJson(data) : null)
          as T;
    }
    if (t == _i1.getType<_i25.WorkroomFixerTurn?>()) {
      return (data != null ? _i25.WorkroomFixerTurn.fromJson(data) : null) as T;
    }
    if (t == _i1.getType<_i26.WorkroomHandsInstruction?>()) {
      return (data != null
              ? _i26.WorkroomHandsInstruction.fromJson(data)
              : null)
          as T;
    }
    if (t == _i1.getType<_i27.WorkroomHandsLane?>()) {
      return (data != null ? _i27.WorkroomHandsLane.fromJson(data) : null) as T;
    }
    if (t == _i1.getType<_i28.WorkroomSurface?>()) {
      return (data != null ? _i28.WorkroomSurface.fromJson(data) : null) as T;
    }
    if (t == List<String>) {
      return (data as List).map((e) => deserialize<String>(e)).toList() as T;
    }
    if (t == List<_i19.ProjectUiEvent>) {
      return (data as List)
              .map((e) => deserialize<_i19.ProjectUiEvent>(e))
              .toList()
          as T;
    }
    if (t == List<_i24.WorkroomFixerThread>) {
      return (data as List)
              .map((e) => deserialize<_i24.WorkroomFixerThread>(e))
              .toList()
          as T;
    }
    if (t == List<_i25.WorkroomFixerTurn>) {
      return (data as List)
              .map((e) => deserialize<_i25.WorkroomFixerTurn>(e))
              .toList()
          as T;
    }
    if (t == List<_i27.WorkroomHandsLane>) {
      return (data as List)
              .map((e) => deserialize<_i27.WorkroomHandsLane>(e))
              .toList()
          as T;
    }
    if (t == List<_i26.WorkroomHandsInstruction>) {
      return (data as List)
              .map((e) => deserialize<_i26.WorkroomHandsInstruction>(e))
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
    if (t == List<_i29.Order>) {
      return (data as List).map((e) => deserialize<_i29.Order>(e)).toList()
          as T;
    }
    if (t == List<_i30.Revision>) {
      return (data as List).map((e) => deserialize<_i30.Revision>(e)).toList()
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
      return _i3.Protocol().deserialize<T>(data, t);
    } on _i1.DeserializationTypeNotFoundException catch (_) {}
    try {
      return _i2.Protocol().deserialize<T>(data, t);
    } on _i1.DeserializationTypeNotFoundException catch (_) {}
    return super.deserialize<T>(data, t);
  }

  static String? getClassNameForType(Type type) {
    return switch (type) {
      _i4.ClientAuthResponse => 'ClientAuthResponse',
      _i5.ClientProfile => 'ClientProfile',
      _i6.ClientUser => 'ClientUser',
      _i7.CodexMessage => 'CodexMessage',
      _i8.CodexThread => 'CodexThread',
      _i9.CodexThreadActivity => 'CodexThreadActivity',
      _i10.CodexTurnEvent => 'CodexTurnEvent',
      _i11.CodexTurnPersistRequest => 'CodexTurnPersistRequest',
      _i12.CommandReceipt => 'CommandReceipt',
      _i13.FixerTurnReceipt => 'FixerTurnReceipt',
      _i14.GenuiActionReceipt => 'GenuiActionReceipt',
      _i15.GenuiActionRequest => 'GenuiActionRequest',
      _i16.HandsInstructionReceipt => 'HandsInstructionReceipt',
      _i17.HandsInstructionRequest => 'HandsInstructionRequest',
      _i18.Order => 'Order',
      _i19.ProjectUiEvent => 'ProjectUiEvent',
      _i20.ProjectUiEventBatchFrame => 'ProjectUiEventBatchFrame',
      _i20.ProjectUiEventFrame => 'ProjectUiEventFrame',
      _i20.ProjectUiHeartbeatFrame => 'ProjectUiHeartbeatFrame',
      _i20.ProjectUiProtocolErrorFrame => 'ProjectUiProtocolErrorFrame',
      _i21.ProjectWorkroomSnapshot => 'ProjectWorkroomSnapshot',
      _i22.Revision => 'Revision',
      _i23.WorkroomBridgeException => 'WorkroomBridgeException',
      _i24.WorkroomFixerThread => 'WorkroomFixerThread',
      _i25.WorkroomFixerTurn => 'WorkroomFixerTurn',
      _i26.WorkroomHandsInstruction => 'WorkroomHandsInstruction',
      _i27.WorkroomHandsLane => 'WorkroomHandsLane',
      _i28.WorkroomSurface => 'WorkroomSurface',
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
      case _i4.ClientAuthResponse():
        return 'ClientAuthResponse';
      case _i5.ClientProfile():
        return 'ClientProfile';
      case _i6.ClientUser():
        return 'ClientUser';
      case _i7.CodexMessage():
        return 'CodexMessage';
      case _i8.CodexThread():
        return 'CodexThread';
      case _i9.CodexThreadActivity():
        return 'CodexThreadActivity';
      case _i10.CodexTurnEvent():
        return 'CodexTurnEvent';
      case _i11.CodexTurnPersistRequest():
        return 'CodexTurnPersistRequest';
      case _i12.CommandReceipt():
        return 'CommandReceipt';
      case _i13.FixerTurnReceipt():
        return 'FixerTurnReceipt';
      case _i14.GenuiActionReceipt():
        return 'GenuiActionReceipt';
      case _i15.GenuiActionRequest():
        return 'GenuiActionRequest';
      case _i16.HandsInstructionReceipt():
        return 'HandsInstructionReceipt';
      case _i17.HandsInstructionRequest():
        return 'HandsInstructionRequest';
      case _i18.Order():
        return 'Order';
      case _i19.ProjectUiEvent():
        return 'ProjectUiEvent';
      case _i20.ProjectUiEventBatchFrame():
        return 'ProjectUiEventBatchFrame';
      case _i20.ProjectUiEventFrame():
        return 'ProjectUiEventFrame';
      case _i20.ProjectUiHeartbeatFrame():
        return 'ProjectUiHeartbeatFrame';
      case _i20.ProjectUiProtocolErrorFrame():
        return 'ProjectUiProtocolErrorFrame';
      case _i21.ProjectWorkroomSnapshot():
        return 'ProjectWorkroomSnapshot';
      case _i22.Revision():
        return 'Revision';
      case _i23.WorkroomBridgeException():
        return 'WorkroomBridgeException';
      case _i24.WorkroomFixerThread():
        return 'WorkroomFixerThread';
      case _i25.WorkroomFixerTurn():
        return 'WorkroomFixerTurn';
      case _i26.WorkroomHandsInstruction():
        return 'WorkroomHandsInstruction';
      case _i27.WorkroomHandsLane():
        return 'WorkroomHandsLane';
      case _i28.WorkroomSurface():
        return 'WorkroomSurface';
    }
    className = _i2.Protocol().getClassNameForObject(data);
    if (className != null) {
      return 'serverpod.$className';
    }
    className = _i3.Protocol().getClassNameForObject(data);
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
      return deserialize<_i4.ClientAuthResponse>(data['data']);
    }
    if (dataClassName == 'ClientProfile') {
      return deserialize<_i5.ClientProfile>(data['data']);
    }
    if (dataClassName == 'ClientUser') {
      return deserialize<_i6.ClientUser>(data['data']);
    }
    if (dataClassName == 'CodexMessage') {
      return deserialize<_i7.CodexMessage>(data['data']);
    }
    if (dataClassName == 'CodexThread') {
      return deserialize<_i8.CodexThread>(data['data']);
    }
    if (dataClassName == 'CodexThreadActivity') {
      return deserialize<_i9.CodexThreadActivity>(data['data']);
    }
    if (dataClassName == 'CodexTurnEvent') {
      return deserialize<_i10.CodexTurnEvent>(data['data']);
    }
    if (dataClassName == 'CodexTurnPersistRequest') {
      return deserialize<_i11.CodexTurnPersistRequest>(data['data']);
    }
    if (dataClassName == 'CommandReceipt') {
      return deserialize<_i12.CommandReceipt>(data['data']);
    }
    if (dataClassName == 'FixerTurnReceipt') {
      return deserialize<_i13.FixerTurnReceipt>(data['data']);
    }
    if (dataClassName == 'GenuiActionReceipt') {
      return deserialize<_i14.GenuiActionReceipt>(data['data']);
    }
    if (dataClassName == 'GenuiActionRequest') {
      return deserialize<_i15.GenuiActionRequest>(data['data']);
    }
    if (dataClassName == 'HandsInstructionReceipt') {
      return deserialize<_i16.HandsInstructionReceipt>(data['data']);
    }
    if (dataClassName == 'HandsInstructionRequest') {
      return deserialize<_i17.HandsInstructionRequest>(data['data']);
    }
    if (dataClassName == 'Order') {
      return deserialize<_i18.Order>(data['data']);
    }
    if (dataClassName == 'ProjectUiEvent') {
      return deserialize<_i19.ProjectUiEvent>(data['data']);
    }
    if (dataClassName == 'ProjectUiEventBatchFrame') {
      return deserialize<_i20.ProjectUiEventBatchFrame>(data['data']);
    }
    if (dataClassName == 'ProjectUiEventFrame') {
      return deserialize<_i20.ProjectUiEventFrame>(data['data']);
    }
    if (dataClassName == 'ProjectUiHeartbeatFrame') {
      return deserialize<_i20.ProjectUiHeartbeatFrame>(data['data']);
    }
    if (dataClassName == 'ProjectUiProtocolErrorFrame') {
      return deserialize<_i20.ProjectUiProtocolErrorFrame>(data['data']);
    }
    if (dataClassName == 'ProjectWorkroomSnapshot') {
      return deserialize<_i21.ProjectWorkroomSnapshot>(data['data']);
    }
    if (dataClassName == 'Revision') {
      return deserialize<_i22.Revision>(data['data']);
    }
    if (dataClassName == 'WorkroomBridgeException') {
      return deserialize<_i23.WorkroomBridgeException>(data['data']);
    }
    if (dataClassName == 'WorkroomFixerThread') {
      return deserialize<_i24.WorkroomFixerThread>(data['data']);
    }
    if (dataClassName == 'WorkroomFixerTurn') {
      return deserialize<_i25.WorkroomFixerTurn>(data['data']);
    }
    if (dataClassName == 'WorkroomHandsInstruction') {
      return deserialize<_i26.WorkroomHandsInstruction>(data['data']);
    }
    if (dataClassName == 'WorkroomHandsLane') {
      return deserialize<_i27.WorkroomHandsLane>(data['data']);
    }
    if (dataClassName == 'WorkroomSurface') {
      return deserialize<_i28.WorkroomSurface>(data['data']);
    }
    if (dataClassName.startsWith('serverpod.')) {
      data['className'] = dataClassName.substring(10);
      return _i2.Protocol().deserializeByClassName(data);
    }
    if (dataClassName.startsWith('serverpod_auth_core.')) {
      data['className'] = dataClassName.substring(20);
      return _i3.Protocol().deserializeByClassName(data);
    }
    return super.deserializeByClassName(data);
  }

  @override
  _i1.Table? getTableForType(Type t) {
    {
      var table = _i3.Protocol().getTableForType(t);
      if (table != null) {
        return table;
      }
    }
    {
      var table = _i2.Protocol().getTableForType(t);
      if (table != null) {
        return table;
      }
    }
    switch (t) {
      case _i6.ClientUser:
        return _i6.ClientUser.t;
      case _i7.CodexMessage:
        return _i7.CodexMessage.t;
      case _i8.CodexThread:
        return _i8.CodexThread.t;
      case _i10.CodexTurnEvent:
        return _i10.CodexTurnEvent.t;
      case _i18.Order:
        return _i18.Order.t;
      case _i22.Revision:
        return _i22.Revision.t;
    }
    return null;
  }

  @override
  List<_i2.TableDefinition> getTargetTableDefinitions() =>
      targetTableDefinitions;

  @override
  String getModuleName() => 'fixer_dashboard';

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
      return _i3.Protocol().mapRecordToJson(record);
    } catch (_) {}
    throw Exception('Unsupported record type ${record.runtimeType}');
  }
}
