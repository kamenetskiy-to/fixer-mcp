import 'package:fixer_dashboard_app/src/shared/provider_model_reasoning_selector.dart';
import 'package:fixer_dashboard_app/src/workroom/hands_tab.dart';
import 'package:fixer_dashboard_app/src/workroom/workroom_models.dart';
import 'package:fixer_dashboard_app/src/workroom/workroom_repository.dart';
import 'package:fixer_dashboard_app/src/workroom/workroom_store.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'workroom_test_fakes.dart';

void main() {
  testWidgets('dbg', (tester) async {
    final fixture = workroomFixture();
    final store = ProjectWorkroomStore(
      projectId: fixture.project.id,
      repository: _FakeSelectorRepository(fixture),
      cursorStore: MemoryProjectUiCursorStore(),
      seedSnapshot: fixture,
      reconnectDelay: (_) async {},
    );
    addTearDown(() async => store.stop());
    await tester.pumpWidget(MaterialApp(
      home: Scaffold(
        body: HandsTab(
          store: store,
          loadThreads: () async => const [
            WorkroomFixerThread(
              id: 'hands-deepseek',
              headline: 'Руки thread',
              provider: 'codex',
              state: 'history',
              createdAt: '2026-08-14T01:00:00Z',
              updatedAt: '2026-08-14T02:00:00Z',
              model: 'deepseek-v4-flash',
              reasoning: 'high',
            ),
          ],
        ),
      ),
    ));
    await tester.pumpAndSettle();
    final selector = tester.widget<ProviderModelReasoningSelector>(
      find.byKey(const ValueKey('hands-provider-lane-selector')),
    );
    // ignore: avoid_print
    print('RESULT prov=${selector.selectedProvider} model=${selector.selectedModel} reasoning=${selector.selectedReasoning}');
  });
}

class _FakeSelectorRepository implements ProjectWorkroomRepository {
  _FakeSelectorRepository(this.snapshot);
  final ProjectWorkroomSnapshot snapshot;
  @override
  Future<ProjectWorkroomSnapshot> loadSnapshot(int projectId) async => snapshot;
  @override
  Stream<ProjectUiFrame> watchProjectUi(int projectId, {required int afterSeq, int protocolVersion = projectWorkroomProtocolVersion}) async* {
    yield const ProjectUiHeartbeatFrame(serverTime: '2026-08-14T00:00:00Z', journalHead: 0);
  }
  @override
  Future<FixerTurnReceipt> sendFixerTurn({required int projectId, required String threadId, required String content, required String idempotencyKey}) async => throw UnimplementedError();
  @override
  Future<GenUiActionReceipt> requestSurface({required int projectId, required String threadId, required RegisteredSurfaceRequest request, required String idempotencyKey}) async => throw UnimplementedError();
  @override
  Future<GenUiActionReceipt> invokeAction({required int projectId, required String surfaceId, required int surfaceRevision, required GenUiActionDescriptor action, required Map<String, dynamic> input, required bool confirmed, required String idempotencyKey}) async => throw UnimplementedError();
  @override
  Future<HandsInstructionReceipt> submitHandsInstruction({required int projectId, required String instructionText, required List<String> declaredWriteScope, required String requestedLane, required String requestedModel, required String requestedReasoning, required String idempotencyKey}) async => throw UnimplementedError();
  @override
  Future<GenUiActionReceipt> cancelHandsInstruction({required int projectId, required String instructionId, required String reason, required String idempotencyKey}) async => throw UnimplementedError();
  @override
  Future<GenUiActionReceipt> selectHandsLane({required int projectId, required String provider, required String idempotencyKey}) async => throw UnimplementedError();
  @override
  Future<GenUiActionReceipt> reviewHandsInstruction({required int projectId, required String instructionId, required String decision, required String reviewNote, required String idempotencyKey}) async => throw UnimplementedError();
}
