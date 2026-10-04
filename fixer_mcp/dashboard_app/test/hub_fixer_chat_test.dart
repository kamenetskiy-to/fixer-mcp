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

  test('Antigravity menu advertises Claude 5.5 with explicit efforts only', () {
    final antigravity = supportedFixerProvidersByBackend['antigravity']!;

    expect(antigravity.models, [
      'Gemini 3.6 Flash',
      'Gemini 3.1 Pro',
      'Claude Opus 5.5',
      'Claude Sonnet 5.5',
    ]);
    expect(antigravity.defaultModel, 'Gemini 3.6 Flash');
    expect(
      antigravity.reasoningOptions,
      containsAll(['low', 'medium', 'high']),
    );
    expect(antigravity.reasoningOptions, isNot(contains('thinking')));
    expect(antigravity.models.contains(antigravity.defaultModel), isTrue);
    expect(
      antigravity.reasoningOptions.contains(antigravity.defaultReasoning),
      isTrue,
    );
    for (final provider in supportedFixerProviders) {
      expect(
        provider.models.where(
          (model) => model.contains('4.6') || model.contains('Thinking'),
        ),
        isEmpty,
        reason: '${provider.backend} still advertises a retired Claude 4.6 id',
      );
    }
  });

  for (final (model, effort) in const [
    ('Claude Opus 5.5', 'high'),
    ('Claude Sonnet 5.5', 'low'),
  ]) {
    testWidgets('creates an Antigravity Fixer chat on $model $effort', (
      tester,
    ) async {
      final service = _FakeFixerChatService([]);
      await tester.pumpWidget(_testApp(service));
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('create-fixer-chat')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('fixer-provider-select')));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Google Antigravity CLI').last);
      await tester.pumpAndSettle();
      await tester.tap(find.text(model).last);
      await tester.pumpAndSettle();
      await tester.tap(find.text(effort).last);
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('launch-fixer-chat')));
      await tester.pumpAndSettle();

      final request = service.launchedRequest!;
      expect(request.toJson(), {
        'backend': 'antigravity',
        'model': model,
        'reasoning': effort,
        'cwd': cwd,
      });
      expect(find.text('New antigravity Fixer'), findsOneWidget);
    });
  }

  test('persisted Antigravity Claude 5.5 thread metadata round-trips', () {
    final record = FixerThreadRecord.fromJson({
      'external_id': 'agy-55',
      'headline': 'Claude 5.5 Fixer',
      'status': 'history',
      'backend': 'antigravity',
      'model': 'Claude Opus 5.5',
      'reasoning': 'medium',
      'cwd': cwd,
      'last_activity_at': '2026-10-04T08:00:00Z',
      'transcript_available': true,
    });
    final provider = supportedFixerProvidersByBackend[record.backend]!;

    expect(record.model, 'Claude Opus 5.5');
    expect(record.reasoning, 'medium');
    expect(provider.models, contains(record.model));
    expect(provider.reasoningOptions, contains(record.reasoning));
    expect(
      FixerChatLaunchRequest(
        backend: record.backend,
        model: record.model,
        reasoning: record.reasoning,
        cwd: record.cwd,
      ).toJson(),
      {
        'backend': 'antigravity',
        'model': 'Claude Opus 5.5',
        'reasoning': 'medium',
        'cwd': cwd,
      },
    );
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
