import 'dart:async';

import 'package:fixer_dashboard_server/src/generated/protocol.dart';

import 'workroom_bridge_client.dart';
import 'workroom_codec.dart';

typedef WorkroomStreamAuthorization = Future<void> Function();
typedef WorkroomJournalFetcher =
    Future<WorkroomJournalBatch> Function(int afterSeq);

/// Drives one replayable Serverpod method stream without buffering more than
/// one bounded Go journal batch while its listener is paused.
class WorkroomStreamPump {
  WorkroomStreamPump({DateTime Function()? now}) : _now = now ?? DateTime.now;

  final DateTime Function() _now;

  Stream<ProjectUiFrame> open({
    required int projectId,
    required int afterSeq,
    required int protocolVersion,
    required WorkroomStreamAuthorization authorize,
    required WorkroomJournalFetcher fetchBatch,
    required void Function() cancelWait,
    void Function()? onClosed,
  }) {
    late StreamController<ProjectUiFrame> controller;
    var cancelled = false;
    var paused = false;
    var closed = false;
    Completer<void>? resumed;

    void finish() {
      if (closed) return;
      closed = true;
      onClosed?.call();
    }

    Future<void> waitUntilResumed() async {
      while (paused && !cancelled) {
        resumed ??= Completer<void>();
        await resumed!.future;
      }
    }

    Future<void> closeWithFrame(ProjectUiProtocolErrorFrame frame) async {
      if (cancelled || controller.isClosed) return;
      controller.add(frame);
      await controller.close();
    }

    Future<void> pump() async {
      var cursor = afterSeq;
      try {
        await authorize();
        if (protocolVersion != workroomProtocolVersion) {
          await closeWithFrame(
            _protocolError(projectId, 'unsupported_protocol_version'),
          );
          return;
        }
        if (afterSeq < 0) {
          await closeWithFrame(
            _protocolError(projectId, 'invalid_replay_cursor'),
          );
          return;
        }
        while (!cancelled) {
          await waitUntilResumed();
          if (cancelled) return;
          // Recheck membership before every bounded Go long-poll.
          await authorize();
          final batch = await fetchBatch(cursor);
          if (cancelled) return;
          final validationError = validateWorkroomJournalBatch(
            batch,
            projectId: projectId,
            afterSeq: cursor,
          );
          if (validationError != null) {
            await closeWithFrame(_protocolError(projectId, validationError));
            return;
          }
          if (batch.events.isEmpty) {
            controller.add(
              ProjectUiHeartbeatFrame(
                projectId: projectId,
                protocolVersion: workroomProtocolVersion,
                serverTime: _now().toUtc().toIso8601String(),
                currentJournalHead: batch.headSeq,
              ),
            );
            continue;
          }
          cursor = batch.events.last.seq;
          controller.add(
            batch.events.length == 1
                ? ProjectUiEventFrame(
                    projectId: projectId,
                    protocolVersion: workroomProtocolVersion,
                    event: batch.events.single,
                  )
                : ProjectUiEventBatchFrame(
                    projectId: projectId,
                    protocolVersion: workroomProtocolVersion,
                    events: batch.events,
                  ),
          );
        }
      } on WorkroomBridgeFailure catch (failure, stackTrace) {
        if (cancelled) return;
        final reasonCode = failure.reasonCode == 'response_too_large'
            ? 'consumer_too_slow'
            : 'bridge_unavailable';
        await closeWithFrame(_protocolError(projectId, reasonCode));
        if (!controller.isClosed) {
          controller.addError(failure, stackTrace);
        }
      } catch (error, stackTrace) {
        if (!cancelled && !controller.isClosed) {
          controller.addError(error, stackTrace);
          await controller.close();
        }
      } finally {
        finish();
      }
    }

    controller = StreamController<ProjectUiFrame>(
      sync: true,
      onListen: () => unawaited(pump()),
      onPause: () {
        paused = true;
      },
      onResume: () {
        paused = false;
        final waiting = resumed;
        resumed = null;
        if (waiting != null && !waiting.isCompleted) waiting.complete();
      },
      onCancel: () {
        cancelled = true;
        paused = false;
        final waiting = resumed;
        resumed = null;
        if (waiting != null && !waiting.isCompleted) waiting.complete();
        cancelWait();
        finish();
      },
    );
    return controller.stream;
  }

  ProjectUiProtocolErrorFrame _protocolError(int projectId, String reasonCode) {
    return ProjectUiProtocolErrorFrame(
      projectId: projectId,
      protocolVersion: workroomProtocolVersion,
      reasonCode: reasonCode,
      minimumSupportedVersion: workroomProtocolVersion,
      maximumSupportedVersion: workroomProtocolVersion,
    );
  }
}

String? validateWorkroomJournalBatch(
  WorkroomJournalBatch batch, {
  required int projectId,
  required int afterSeq,
}) {
  if (batch.projectId != projectId || batch.afterSeq != afterSeq) {
    return 'journal_binding_mismatch';
  }
  if (batch.events.length > workroomEventBatchLimit ||
      batch.events.length > workroomPendingEventLimit) {
    return 'consumer_too_slow';
  }
  var expectedSeq = afterSeq + 1;
  for (final event in batch.events) {
    if (event.projectId != projectId ||
        event.schemaVersion != workroomProtocolVersion ||
        event.seq != expectedSeq) {
      return 'journal_sequence_corrupt';
    }
    expectedSeq++;
  }
  if (batch.headSeq < afterSeq ||
      (batch.events.isNotEmpty && batch.headSeq < batch.events.last.seq)) {
    return 'journal_head_corrupt';
  }
  return null;
}
