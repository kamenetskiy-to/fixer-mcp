import 'dart:convert';

import 'package:fixer_dashboard_app/src/dashboard_models.dart';
import 'package:fixer_dashboard_app/src/hub/netrunners/netrunner_models.dart';
import 'package:fixer_dashboard_app/src/mission_control/mission_control_models.dart';
import 'package:fixer_dashboard_app/src/workroom/workroom_models.dart';
import 'package:fixer_dashboard_client/fixer_dashboard_client.dart'
    hide ProjectUiEvent;
import 'package:flutter_test/flutter_test.dart';

void main() {
  group('declared-scope retirement', () {
    test('HandsInstruction has no scope field and ignores legacy scope keys', () {
      final instruction = HandsInstruction.fromJson({
        'id': 'instruction-1',
        'ordinal': 7,
        'instruction_text': 'Audit the bridge.',
        'requested_lane': 'codex',
        'issuer': 'architect',
        'created_at': '2026-10-04T09:00:00Z',
        'updated_at': '2026-10-04T09:30:00Z',
        'declared_write_scope': ['fixer_mcp/dashboard_app'],
        'declaredWriteScope': ['fixer_mcp/dashboard_app'],
        'write_scope': ['fixer_mcp/dashboard_app'],
      });

      expect(instruction.id, 'instruction-1');
      expect(instruction.ordinal, 7);
      expect(instruction.instructionText, 'Audit the bridge.');
      expect(instruction.requestedLane, 'codex');
      expect(instruction.issuer, 'architect');

      final copy = instruction.copyWith(state: 'completed');
      expect(copy.state, 'completed');
      expect(copy.requestedLane, 'codex');
    });

    test('session read models ignore legacy write_scope keys', () {
      final summary = NetrunnerSummaryRecord.fromJson({
        'id': 10,
        'local_id': 3,
        'project_id': 1,
        'headline': 'Wave worker',
        'task_preview': 'Task',
        'status': 'in_progress',
        'backend': 'codex',
        'model': 'gpt-5.6',
        'reasoning': 'high',
        'write_scope': ['fixer_mcp/dashboard_app'],
        'worker_state': <String, dynamic>{},
      });
      expect(summary.id, 10);
      expect(summary.reasoning, 'high');

      final detail = SessionDetailRecord.fromJson({
        'id': 10,
        'local_id': 3,
        'project_id': 1,
        'task_description': 'Task',
        'status': 'review',
        'backend': 'codex',
        'model': 'gpt-5.6',
        'reasoning': 'high',
        'write_scope': ['fixer_mcp/dashboard_app'],
        'worker_state': <String, dynamic>{},
      });
      expect(detail.status, 'review');
      expect(detail.taskDescription, 'Task');

      final explorer = NetrunnerExplorerRecord.fromJson({
        'id': 10,
        'local_id': 3,
        'project_id': 1,
        'wave_id': 2,
        'role': 'worker',
        'kind': 'worker',
        'headline': 'Wave worker',
        'task_preview': 'Task',
        'status': 'running',
        'membership_status': 'running',
        'backend': 'codex',
        'model': 'gpt-5.6',
        'reasoning': 'high',
        'write_scope': ['fixer_mcp'],
      });
      expect(explorer.waveId, 2);
      expect(explorer.model, 'gpt-5.6');
    });

    test('planned-wave task ignores the legacy declared_write_scope key', () {
      final task = MissionControlPlannedWaveTask.fromJson({
        'task_id': 5001,
        'key': 'backend',
        'position': 1,
        'task_description': 'Add the governed planned-wave contract.',
        'declared_write_scope': ['fixer_mcp/dashboard_api'],
        'depends_on': <String>[],
      });
      expect(task.taskId, 5001);
      expect(task.taskDescription, 'Add the governed planned-wave contract.');
      expect(task.dependsOn, isEmpty);
    });

    test('generated HandsInstructionRequest emits no retired scope field', () {
      final request = HandsInstructionRequest(
        projectId: 1,
        instructionText: 'Audit the bridge.',
        requestedLane: 'codex',
        idempotencyKey: 'hands-key',
      );

      expect(request.toJson().keys, isNot(contains('declaredWriteScope')));
      expect(request.toJson().keys, isNot(contains('declared_write_scope')));
      expect(request.toJson().keys, isNot(contains('write_scope')));
      expect(request.toJson()['requestedLane'], 'codex');

      final roundTrip = HandsInstructionRequest.fromJson(request.toJson());
      expect(roundTrip.instructionText, 'Audit the bridge.');
    });

    test('generated WorkroomHandsInstruction emits no retired scope field', () {
      final instruction = WorkroomHandsInstruction(
        instructionId: 'instruction-1',
        ordinal: 7,
        instructionText: 'Audit the bridge.',
        requestedLane: 'codex',
        riskClass: 'standard',
        reviewPolicy: 'manual',
        state: 'queued',
        revision: 1,
        createdAt: '2026-10-04T09:00:00Z',
        updatedAt: '2026-10-04T09:30:00Z',
      );

      final json = instruction.toJson();
      expect(json.keys, isNot(contains('declared_write_scope')));
      expect(json.keys, isNot(contains('declaredWriteScope')));
      expect(json['id'], 'instruction-1');
      expect(json['requested_lane'], 'codex');
      expect(json['ordinal'], 7);
    });

    test('WorkroomHandsState drops legacy lease summary keys', () {
      final hands = WorkroomHandsState.fromJson({
        'actor_id': 'hands-actor-1',
        'display_name': 'Руки',
        'authority_state': 'enabled',
        'operational_state': 'idle',
        'selected_lane': 'codex',
        'queue_depth': 0,
        'active_lease_summary': 'fixer_mcp/dashboard_app',
        'lease_summary': 'fixer_mcp/dashboard_app',
        'activeLeaseSummary': 'fixer_mcp/dashboard_app',
        'lanes': <dynamic>[],
        'instructions': <dynamic>[],
        'selected_instruction_id': '',
      });
      expect(hands.actorId, 'hands-actor-1');
      expect(hands.queueDepth, 0);
      expect(hands.selectedLane, 'codex');

      final copy = hands.copyWith(queueDepth: 2);
      expect(copy.queueDepth, 2);
      expect(copy.actorId, 'hands-actor-1');
    });

    test('retired lease events parse, but never join the live contract', () {
      final retired = ProjectUiEvent.fromJson({
        'project_id': 1,
        'seq': 9,
        'event_id': 'event-9',
        'schema_version': 1,
        'kind': 'lease.changed',
        'aggregate_type': 'project_write_lease',
        'aggregate_id': 'lease-1',
        'aggregate_revision': 1,
        'created_at': '2026-10-04T09:00:00Z',
        'payload': {'lease_summary': 'fixer_mcp/dashboard_app'},
      });
      expect(retired.kind, 'lease.changed');
      expect(retiredProjectUiEventKinds, contains('lease.changed'));
      expect(registeredProjectUiEventKinds, isNot(contains('lease.changed')));

      expect(
        () => ProjectUiEvent.fromJson({
          'project_id': 1,
          'seq': 10,
          'event_id': 'event-10',
          'schema_version': 1,
          'kind': 'scope.fence.created',
          'aggregate_type': 'project_write_fence',
          'aggregate_id': 'fence-1',
          'aggregate_revision': 1,
          'created_at': '2026-10-04T09:00:00Z',
          'payload': <String, dynamic>{},
        }),
        throwsA(isA<WorkroomProtocolException>()),
      );
    });

    test('legacy waiting_for_lease instructions lose lease semantics', () {
      final instruction = HandsInstruction.fromJson({
        'id': 'instruction-2',
        'ordinal': 3,
        'instruction_text': 'Historic queued work.',
        'state': 'waiting_for_lease',
        'requested_lane': 'codex',
        'issuer': 'architect',
        'created_at': '2026-10-04T09:00:00Z',
        'updated_at': '2026-10-04T09:30:00Z',
      });
      expect(instruction.canCancel, isFalse);
      expect(instruction.isTerminal, isFalse);
      expect(instruction.awaitsReview, isFalse);
    });
  });

  group('wire contract key census (positive proof)', () {
    test('HandsInstructionRequest emits exactly the contract wire keys', () {
      final request = HandsInstructionRequest(
        projectId: 1,
        instructionText: 'Audit the bridge.',
        requestedLane: 'codex',
        idempotencyKey: 'hands-key',
      );

      expect(request.toJson().keys.toSet(), <String>{
        '__className__',
        'projectId',
        'instructionText',
        'requestedLane',
        'idempotencyKey',
      });

      final encoded = jsonEncode(request.toJson());
      expect(encoded.contains('scope'), isFalse);
      expect(encoded.contains('lease'), isFalse);
    });

    test('WorkroomHandsInstruction emits exactly the contract wire keys', () {
      final full = WorkroomHandsInstruction(
        instructionId: 'instruction-1',
        ordinal: 7,
        instructionText: 'Audit the bridge.',
        requestedLane: 'codex',
        riskClass: 'standard',
        reviewPolicy: 'manual',
        state: 'failed',
        stateReasonCode: 'timeout',
        stateReasonText: 'Lane timed out.',
        revision: 2,
        createdAt: '2026-10-04T09:00:00Z',
        updatedAt: '2026-10-04T09:30:00Z',
        terminalAt: '2026-10-04T09:31:00Z',
      );

      expect(full.toJson().keys.toSet(), <String>{
        '__className__',
        'id',
        'ordinal',
        'instruction_text',
        'requested_lane',
        'risk_class',
        'review_policy',
        'state',
        'state_reason_code',
        'state_reason_text',
        'revision',
        'created_at',
        'updated_at',
        'terminal_at',
      });

      final minimal = WorkroomHandsInstruction(
        instructionId: 'instruction-2',
        ordinal: 8,
        instructionText: 'Second instruction.',
        requestedLane: 'codex',
        riskClass: 'standard',
        reviewPolicy: 'manual',
        state: 'queued',
        revision: 1,
        createdAt: '2026-10-04T09:00:00Z',
        updatedAt: '2026-10-04T09:00:00Z',
      );

      expect(minimal.toJson().keys.toSet(), <String>{
        '__className__',
        'id',
        'ordinal',
        'instruction_text',
        'requested_lane',
        'risk_class',
        'review_policy',
        'state',
        'revision',
        'created_at',
        'updated_at',
      });

      final encoded = jsonEncode(full.toJson());
      expect(encoded.contains('scope'), isFalse);
      expect(encoded.contains('lease'), isFalse);
    });
  });
}
