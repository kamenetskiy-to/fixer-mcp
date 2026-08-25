import 'dart:async';
import 'dart:convert';

import 'package:serverpod/serverpod.dart' as sp;

import '../future_calls/codex_turn_future_call.dart';
import '../generated/protocol.dart';
import 'codex_bridge_client.dart';

/// Small persistence facade copied from the old Codex Hub flow. The external
/// app-server thread id remains the only identity used by the UI and TUI.
class CodexThreadService {
  CodexBridgeClient _bridge() =>
      CodexBridgeClient(baseUrl: resolveCodexBridgeBaseUrl());

  Future<CodexThread> ensureThread(
    sp.Session session,
    String rawThreadId,
  ) async {
    final threadId = rawThreadId.trim();
    if (threadId.isEmpty) {
      throw ArgumentError.value(rawThreadId, 'threadId', 'must not be empty');
    }
    final existing = await CodexThread.db.findFirstRow(
      session,
      where: (t) => t.threadId.equals(threadId),
    );
    if (existing != null) return existing;
    return CodexThread.db.insertRow(session, CodexThread(threadId: threadId));
  }

  Future<List<CodexMessage>> listMessages(
    sp.Session session,
    String rawThreadId,
  ) async {
    final thread = await ensureThread(session, rawThreadId);
    try {
      await _hydrateFromAppServer(session, thread);
    } catch (error, stackTrace) {
      session.log('[codex] thread/read hydration skipped: $error\n$stackTrace');
    }
    return CodexMessage.db.find(
      session,
      where: (m) => m.codexThreadId.equals(thread.id!),
      orderByList: (m) => [
        sp.Order(column: m.createdAt),
        sp.Order(column: m.id),
      ],
    );
  }

  Future<Map<String, dynamic>> startTurn(
    sp.Session session,
    String rawThreadId,
    String prompt, {
    String model = '',
    String reasoning = '',
  }) async {
    final normalizedPrompt = prompt.trim();
    if (normalizedPrompt.isEmpty) {
      throw ArgumentError.value(prompt, 'prompt', 'must not be empty');
    }
    final thread = await ensureThread(session, rawThreadId);
    final result = await _bridge().postJson('/turn/start', {
      'threadId': thread.threadId,
      'prompt': normalizedPrompt,
      if (model.trim().isNotEmpty) 'model': model.trim(),
      if (reasoning.trim().isNotEmpty) 'reasoningEffort': reasoning.trim(),
    });
    final streamId = result['streamId'];
    final turnId = result['turnId'];
    if (streamId is! String || turnId is! String) {
      throw StateError('Codex bridge returned no streamId/turnId.');
    }

    final now = DateTime.now();
    await CodexMessage.db.insertRow(
      session,
      CodexMessage(
        codexThreadId: thread.id!,
        role: 'user',
        text: normalizedPrompt,
        turnId: turnId,
        createdAt: now,
      ),
    );
    thread.isActive = true;
    thread.activeTurnId = turnId;
    thread.activeSince = now;
    thread.lastActivityAt = now;
    await CodexThread.db.updateRow(session, thread);
    await CodexTurnEvent.db.insertRow(
      session,
      CodexTurnEvent(
        codexThreadId: thread.id!,
        turnId: turnId,
        method: 'thread/active',
        json: jsonEncode({
          'type': 'thread_activity',
          'state': 'active',
          'streamId': streamId,
          'threadId': thread.threadId,
          'turnId': turnId,
          'startedAt': now.toIso8601String(),
        }),
        createdAt: now,
      ),
    );

    // Persist streamed events and assistant messages immediately. The normal
    // FutureCall scanner remains available as a resilience fallback.
    unawaited(
      CodexTurnFutureCall().invokeDetachedSession(
        session,
        CodexTurnPersistRequest(
          codexThreadId: thread.id!,
          streamId: streamId,
          turnId: turnId,
        ),
      ),
    );
    return {...result, 'codexThreadId': thread.id, 'persistence': 'serverpod'};
  }

  Future<void> _hydrateFromAppServer(
    sp.Session session,
    CodexThread localThread,
  ) async {
    final result = await _bridge().readThread(threadId: localThread.threadId);
    final threadJson = result['thread'];
    if (threadJson is Map) {
      localThread.title =
          _stringValue(threadJson['name']) ??
          _stringValue(threadJson['title']) ??
          localThread.title;
      localThread.cwd = _stringValue(threadJson['cwd']) ?? localThread.cwd;
      await CodexThread.db.updateRow(session, localThread);
    }
    final turns = threadJson is Map ? threadJson['turns'] : result['turns'];
    if (turns is! List) return;
    final current = await CodexMessage.db.find(
      session,
      where: (m) => m.codexThreadId.equals(localThread.id!),
    );
    final pending = <CodexMessage>[];
    for (final turn in turns.whereType<Map>()) {
      final turnId = _stringValue(turn['id']);
      final items = turn['items'];
      if (items is! List) continue;
      for (final item in items.whereType<Map>()) {
        final role = _roleForItem(item['type']);
        if (role == null) continue;
        final text = _extractText(item);
        if (text == null || text.trim().isEmpty) continue;
        if (role == 'user' && _isInjectedContext(text)) continue;
        final duplicate = current.any(
          (message) =>
              message.role == role &&
              message.turnId == turnId &&
              message.text == text,
        );
        if (duplicate) continue;
        final message = CodexMessage(
          codexThreadId: localThread.id!,
          role: role,
          text: text,
          turnId: turnId,
        );
        pending.add(message);
        current.add(message);
      }
    }
    if (pending.isNotEmpty) {
      await CodexMessage.db.insert(session, pending);
    }
  }

  String? _roleForItem(Object? rawType) {
    final type = rawType is String ? rawType.toLowerCase() : '';
    if (type.contains('usermessage') || type == 'user') return 'user';
    if (type.contains('agentmessage') || type == 'assistant') {
      return 'assistant';
    }
    return null;
  }

  bool _isInjectedContext(String text) {
    final normalized = text.trimLeft();
    return normalized.startsWith('<environment_context>') ||
        normalized.startsWith('<developer_instructions>');
  }

  String? _extractText(Map item) {
    final direct = item['text'];
    if (direct is String) return direct;
    final message = item['message'];
    if (message is String) return message;
    final content = item['content'];
    if (content is String) return content;
    if (content is List) {
      final parts = content
          .whereType<Map>()
          .map((part) => part['text'])
          .whereType<String>()
          .toList();
      if (parts.isNotEmpty) return parts.join();
    }
    return null;
  }

  String? _stringValue(Object? value) =>
      value is String && value.isNotEmpty ? value : null;
}
