import 'dart:convert';

import 'package:serverpod/serverpod.dart' as sp;

import '../codex_runtime/codex_bridge_client.dart';
import '../generated/protocol.dart';

class CodexTurnFutureCall extends sp.FutureCall<CodexTurnPersistRequest> {
  /// Starts immediate in-process persistence using a dedicated internal
  /// session. This avoids waiting for the FutureCall scanner/queue, while
  /// keeping FutureCall scheduling as a resilience fallback.
  Future<void> invokeDetachedSession(
    sp.Session session,
    CodexTurnPersistRequest request,
  ) async {
    await _invokeDetached(session.serverpod, request);
  }

  static Future<void> _invokeDetached(
    sp.Serverpod serverpod,
    CodexTurnPersistRequest request,
  ) async {
    final session = await serverpod.createSession(enableLogging: true);
    try {
      await CodexTurnFutureCall().invoke(session, request);
      await session.close();
    } catch (error, stackTrace) {
      session.log(
        '[codex] detached turnPersist failed '
        '(thread=${request.codexThreadId}, turn=${request.turnId}): $error',
      );
      await session.close(error: error, stackTrace: stackTrace);
    }
  }

  @override
  Future<void> invoke(
    sp.Session session,
    CodexTurnPersistRequest? object,
  ) async {
    if (object == null) return;

    if (!await _isLatestActiveAttempt(
      session,
      object.codexThreadId,
      object.turnId,
      object.streamId,
    )) {
      session.log(
        '[codex] turnPersist skip stale attempt '
        '(thread=${object.codexThreadId}, turn=${object.turnId})',
      );
      return;
    }

    await _persistTurn(
      session,
      object.codexThreadId,
      object.streamId,
      object.turnId,
    );
  }

  Uri _bridgeBaseUrl() {
    return resolveCodexBridgeBaseUrl();
  }

  CodexBridgeClient _bridge() => CodexBridgeClient(baseUrl: _bridgeBaseUrl());

  Future<bool> _isLatestActiveAttempt(
    sp.Session session,
    int codexThreadId,
    String turnId,
    String streamId,
  ) async {
    final active = await CodexTurnEvent.db.findFirstRow(
      session,
      where: (e) =>
          e.codexThreadId.equals(codexThreadId) &
          e.turnId.equals(turnId) &
          e.method.equals('thread/active'),
      orderByList: (e) => [
        sp.Order(column: e.createdAt, orderDescending: true),
      ],
    );
    if (active == null) return true;

    try {
      final decoded = jsonDecode(active.json);
      if (decoded is! Map) return true;
      final activeStreamId = decoded['streamId'];
      if (activeStreamId is! String || activeStreamId.isEmpty) return true;
      return activeStreamId == streamId;
    } catch (_) {
      return true;
    }
  }

  Future<void> _persistTurn(
    sp.Session session,
    int codexThreadId,
    String streamId,
    String turnId,
  ) async {
    final thread = await CodexThread.db.findById(session, codexThreadId);
    if (thread == null) {
      session.log(
        '[codex] turnPersist: unknown codexThreadId=$codexThreadId (turnId=$turnId)',
      );
      return;
    }

    // Mark active (best-effort). Do not reset activeSince for the same turn.
    final now = DateTime.now();
    final shouldResetActiveSince =
        thread.activeTurnId == null || thread.activeTurnId != turnId;
    thread.isActive = true;
    thread.activeTurnId = turnId;
    thread.activeSince = shouldResetActiveSince ? now : thread.activeSince;
    thread.lastActivityAt = now;
    await CodexThread.db.updateRow(session, thread);

    final bridge = _bridge();
    final eventBuffer = <CodexTurnEvent>[];
    DateTime lastActivityWrite = DateTime.fromMillisecondsSinceEpoch(0);
    final assistantDeltaByItemId = <String, _AssistantDeltaAccumulator>{};
    final completedAssistantByItemId = <String, _AssistantMessageCandidate>{};
    final completedAssistantOrder = <String>[];
    final anonymousCompletedAssistant = <_AssistantMessageCandidate>[];
    final anonymousAssistantDelta = StringBuffer();
    var streamEnded = false;

    Future<void> flushEvents() async {
      if (eventBuffer.isEmpty) return;
      final batch = List<CodexTurnEvent>.from(eventBuffer);
      eventBuffer.clear();
      await CodexTurnEvent.db.insert(session, batch);
    }

    Future<void> writeActivityHeartbeat() async {
      // Avoid a DB write per event. The UI only needs "still active" to be
      // reasonably fresh.
      final t = DateTime.now();
      if (t.difference(lastActivityWrite) < const Duration(seconds: 2)) return;
      lastActivityWrite = t;

      final fresh = await CodexThread.db.findById(session, codexThreadId);
      if (fresh == null) return;
      if (fresh.activeTurnId != turnId) return;

      fresh.lastActivityAt = t;
      await CodexThread.db.updateRow(session, fresh);
    }

    try {
      final lines = await bridge.getSseLines('/turn/stream/$streamId');

      await for (final line in lines) {
        if (!line.startsWith('data:')) continue;
        final jsonPart = line.substring(5).trimLeft();
        if (jsonPart.isEmpty) continue;

        final decoded = jsonDecode(jsonPart);
        if (decoded is! Map) continue;
        final msg = decoded['msg'];
        if (msg is! Map) continue;
        final method = msg['method'];
        if (method is! String) continue;
        final params = msg['params'];
        final observedAt = DateTime.now();
        if (params is Map && params['turnId'] is String) {
          final eventTurnId = params['turnId'] as String;
          if (eventTurnId.isNotEmpty && eventTurnId != turnId) continue;
        }

        if (method == 'item/agentMessage/delta') {
          if (params is Map) {
            final delta = params['delta'];
            if (delta is String && delta.isNotEmpty) {
              final itemId = params['itemId'];
              if (itemId is String && itemId.trim().isNotEmpty) {
                final normalizedItemId = itemId.trim();
                final bucket = assistantDeltaByItemId.putIfAbsent(
                  normalizedItemId,
                  () => _AssistantDeltaAccumulator(firstSeenAt: observedAt),
                );
                bucket.delta.write(delta);
                bucket.lastSeenAt = observedAt;
              } else {
                anonymousAssistantDelta.write(delta);
              }
            }
          }
        } else if (method == 'item/completed') {
          if (params is Map) {
            final item = params['item'];
            if (item is Map) {
              final type = item['type'];
              final text = item['text'];
              if (type is String &&
                  type.toLowerCase() == 'agentmessage' &&
                  text is String &&
                  text.trim().isNotEmpty) {
                final normalizedText = text;
                final itemId = item['id'];
                if (itemId is String && itemId.trim().isNotEmpty) {
                  final normalizedItemId = itemId.trim();
                  final candidate = _AssistantMessageCandidate(
                    text: normalizedText,
                    createdAt: observedAt,
                  );
                  if (!completedAssistantByItemId.containsKey(
                    normalizedItemId,
                  )) {
                    completedAssistantOrder.add(normalizedItemId);
                  }
                  completedAssistantByItemId[normalizedItemId] = candidate;
                } else {
                  anonymousCompletedAssistant.add(
                    _AssistantMessageCandidate(
                      text: normalizedText,
                      createdAt: observedAt,
                    ),
                  );
                }
              }
            }
          }
        }

        eventBuffer.add(
          CodexTurnEvent(
            codexThreadId: codexThreadId,
            turnId: turnId,
            method: method,
            json: jsonEncode(msg),
          ),
        );

        if (eventBuffer.length >= 25) await flushEvents();
        await writeActivityHeartbeat();

        if (method == 'turn/completed') break;
      }
      streamEnded = true;
    } catch (e) {
      session.log('[codex] turnPersist failed (turnId=$turnId): $e');
    } finally {
      try {
        await flushEvents();
      } catch (e) {
        session.log('[codex] turnPersist flush failed (turnId=$turnId): $e');
      }

      final canFinalize =
          streamEnded &&
          await _isLatestActiveAttempt(
            session,
            codexThreadId,
            turnId,
            streamId,
          );
      if (canFinalize) {
        // Persist assistant messages from this turn as separate rows so chat
        // history survives app reload without collapsing distinct replies.
        final assistantCandidates = <_AssistantMessageCandidate>[];
        if (completedAssistantOrder.isNotEmpty ||
            anonymousCompletedAssistant.isNotEmpty) {
          for (final itemId in completedAssistantOrder) {
            final candidate = completedAssistantByItemId[itemId];
            if (candidate == null) continue;
            if (candidate.text.trim().isEmpty) continue;
            assistantCandidates.add(candidate);
          }
          for (final candidate in anonymousCompletedAssistant) {
            if (candidate.text.trim().isEmpty) continue;
            assistantCandidates.add(candidate);
          }
        } else if (assistantDeltaByItemId.isNotEmpty) {
          for (final bucket in assistantDeltaByItemId.values) {
            final text = bucket.delta.toString();
            if (text.trim().isEmpty) continue;
            assistantCandidates.add(
              _AssistantMessageCandidate(
                text: text,
                createdAt: bucket.lastSeenAt,
              ),
            );
          }
        } else {
          final text = anonymousAssistantDelta.toString();
          if (text.trim().isNotEmpty) {
            assistantCandidates.add(
              _AssistantMessageCandidate(text: text, createdAt: DateTime.now()),
            );
          }
        }

        try {
          await CodexMessage.db.deleteWhere(
            session,
            where: (m) =>
                m.codexThreadId.equals(codexThreadId) &
                m.turnId.equals(turnId) &
                m.role.equals('assistant'),
          );
          if (assistantCandidates.isNotEmpty) {
            await CodexMessage.db.insert(
              session,
              assistantCandidates
                  .map(
                    (candidate) => CodexMessage(
                      codexThreadId: codexThreadId,
                      role: 'assistant',
                      text: candidate.text,
                      turnId: turnId,
                      createdAt: candidate.createdAt,
                    ),
                  )
                  .toList(),
            );
          }
        } catch (e) {
          session.log(
            '[codex] turnPersist assistant save failed (turnId=$turnId): $e',
          );
        }

        final doneAt = DateTime.now();
        final fresh = await CodexThread.db.findById(session, codexThreadId);
        if (fresh != null && fresh.activeTurnId == turnId) {
          fresh.isActive = false;
          fresh.activeTurnId = null;
          fresh.activeSince = null;
          fresh.lastActivityAt = doneAt;
          fresh.lastCompletedAt = doneAt;
          await CodexThread.db.updateRow(session, fresh);
        }

        // Emit explicit completion state (best-effort).
        try {
          await CodexTurnEvent.db.insertRow(
            session,
            CodexTurnEvent(
              codexThreadId: codexThreadId,
              turnId: turnId,
              method: 'thread/inactive',
              json: jsonEncode({
                'type': 'thread_activity',
                'state': 'inactive',
                'codexThreadId': codexThreadId,
                'turnId': turnId,
                'completedAt': doneAt.toIso8601String(),
              }),
              createdAt: doneAt,
            ),
          );
        } catch (e) {
          session.log(
            '[codex] turnPersist activity emit failed (turnId=$turnId): $e',
          );
        }
      }
    }
  }
}

class _AssistantDeltaAccumulator {
  _AssistantDeltaAccumulator({required this.firstSeenAt})
    : lastSeenAt = firstSeenAt;

  final DateTime firstSeenAt;
  DateTime lastSeenAt;
  final StringBuffer delta = StringBuffer();
}

class _AssistantMessageCandidate {
  const _AssistantMessageCandidate({
    required this.text,
    required this.createdAt,
  });

  final String text;
  final DateTime createdAt;
}
