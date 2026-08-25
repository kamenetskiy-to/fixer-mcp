import 'package:fixer_dashboard_app/src/hub/fixer_chat/fixer_chat.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

class _FakeFixerChatService implements FixerChatService {
  _FakeFixerChatService(this.threads);

  List<FixerThreadRecord> threads;
  FixerChatLaunchRequest? launchedRequest;
  int loadCount = 0;

  @override
  Future<void> createFixerChat(
    int projectId,
    FixerChatLaunchRequest request,
  ) async {
    launchedRequest = request;
    threads = [
      FixerThreadRecord(
        externalId: 'new-${request.backend}',
        headline: 'New ${request.backend} Fixer',
        status: 'active',
        backend: request.backend,
        model: request.model,
        reasoning: request.reasoning,
        cwd: request.cwd,
        lastActivityAt: '2026-07-23T10:00:00Z',
        transcriptAvailable: true,
      ),
      ...threads,
    ];
  }

  @override
  Future<List<FixerThreadRecord>> loadFixerThreads(int projectId) async {
    loadCount += 1;
    return threads;
  }
}

void main() {
  const cwd = '/workspace/fixer-project';

  testWidgets('lists multi-provider Fixer threads with launch metadata', (
    tester,
  ) async {
    final service = _FakeFixerChatService(
      supportedFixerProviders
          .map(
            (provider) => FixerThreadRecord(
              externalId: '${provider.backend}-session',
              headline: '${provider.label} Fixer',
              status: 'history',
              backend: provider.backend,
              model: provider.defaultModel,
              reasoning: provider.defaultReasoning,
              cwd: cwd,
              lastActivityAt: '2026-07-23T09:00:00Z',
              transcriptAvailable: true,
            ),
          )
          .toList(),
    );

    await tester.pumpWidget(_testApp(service));
    await tester.pumpAndSettle();

    expect(find.text('Create new Fixer chat'), findsOneWidget);
    for (final provider in supportedFixerProviders) {
      await tester.scrollUntilVisible(
        find.byKey(Key('provider-${provider.backend}-session')),
        180,
        scrollable: find.byType(Scrollable).last,
      );
      expect(find.text('${provider.label} Fixer'), findsOneWidget);
      expect(
        find.byKey(Key('cwd-${provider.backend}-session')),
        findsOneWidget,
      );
    }
  });

  testWidgets('creates a Droid Fixer chat with selected model and reasoning', (
    tester,
  ) async {
    final service = _FakeFixerChatService([]);
    await tester.pumpWidget(_testApp(service));
    await tester.pumpAndSettle();

    await tester.tap(find.byKey(const Key('create-fixer-chat')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('fixer-provider-select')));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Factory Droid CLI').last);
    await tester.pumpAndSettle();
    await tester.tap(find.text('kimi-k2.7-code').last);
    await tester.pumpAndSettle();
    await tester.tap(find.text('high').last);
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('launch-fixer-chat')));
    await tester.pumpAndSettle();

    expect(service.launchedRequest?.backend, 'droid');
    expect(service.launchedRequest?.model, 'kimi-k2.7-code');
    expect(service.launchedRequest?.reasoning, 'high');
    expect(service.launchedRequest?.cwd, cwd);
    expect(service.loadCount, 2);
    expect(find.text('New droid Fixer'), findsOneWidget);
  });

  test('parses repository thread metadata without dropping cwd', () {
    final record = FixerThreadRecord.fromJson({
      'external_id': 'claude-1',
      'headline': 'Claude Fixer',
      'status': 'history',
      'backend': 'claude',
      'model': 'sonnet',
      'reasoning': 'medium',
      'cwd': cwd,
      'last_activity_at': '2026-07-23T08:00:00Z',
      'transcript_available': true,
    });

    expect(record.backend, 'claude');
    expect(record.model, 'sonnet');
    expect(record.cwd, cwd);
    expect(record.transcriptAvailable, isTrue);
  });
}

Widget _testApp(_FakeFixerChatService service) {
  return MaterialApp(
    home: Scaffold(
      body: SizedBox(
        width: 1000,
        height: 800,
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: FixerChatPanel(
            projectId: 7,
            projectCwd: '/workspace/fixer-project',
            service: service,
          ),
        ),
      ),
    ),
  );
}
