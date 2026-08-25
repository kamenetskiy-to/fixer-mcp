import 'dart:io';

import 'package:test/test.dart';

void main() {
  test('Serverpod scaffold declares the migrated runtime topology', () {
    final server = File('lib/server.dart').readAsStringSync();
    final endpoint = File(
      'lib/src/endpoints/dashboard_runtime_endpoint.dart',
    ).readAsStringSync();
    final generated = File(
      'lib/src/generated/endpoints.dart',
    ).readAsStringSync();
    final bridge = File(
      'lib/src/workroom/workroom_bridge_client.dart',
    ).readAsStringSync();
    final codexRuntime = File(
      'lib/src/codex_runtime/codex_thread_service.dart',
    ).readAsStringSync();
    final development = File('config/development.yaml').readAsStringSync();
    final compose = File('docker-compose.yaml').readAsStringSync();

    expect(server, contains("'goAuthority': 'dashboard_api'"));
    expect(server, contains("'codexRuntimeAdapter': 'node_bridge'"));
    expect(server, contains("'appFacingApi': 'serverpod'"));
    expect(endpoint, contains('Future<Map<String, dynamic>> homeSnapshot'));
    expect(endpoint, contains('extends ClientProtectedEndpoint'));
    expect(endpoint, contains('Future<ProjectWorkroomSnapshot>'));
    expect(endpoint, contains('Stream<ProjectUiFrame> watchProjectUi'));
    expect(endpoint, contains('Future<FixerTurnReceipt> sendFixerTurn'));
    expect(
      endpoint,
      contains('Future<GenuiActionReceipt> requestGenuiSurface'),
    );
    expect(endpoint, contains('Future<GenuiActionReceipt> invokeGenuiAction'));
    expect(
      endpoint,
      contains('Future<Map<String, dynamic>> waitProjectUiEventsJson'),
    );
    expect(endpoint, contains('genui/surfaces'));
    expect(endpoint, contains("'architect@example.com'"));
    expect(
      endpoint,
      contains('Future<HandsInstructionReceipt> submitHandsInstruction'),
    );
    expect(endpoint, contains('Future<CommandReceipt> cancelHandsInstruction'));
    expect(endpoint, contains('Future<Map<String, dynamic>> threadMessages'));
    expect(endpoint, contains('CodexThreadService().listMessages'));
    expect(endpoint, contains('CodexThreadService().startTurn'));
    expect(codexRuntime, contains("'/turn/start'"));
    expect(codexRuntime, contains('CodexTurnFutureCall'));
    expect(endpoint, contains('Future<Map<String, dynamic>> threadTurnStatus'));
    expect(endpoint, contains("'/turn/status/"));
    expect(endpoint, contains("findProxy = (_) => 'DIRECT'"));
    expect(endpoint, contains("HttpHeaders.connectionHeader, 'close'"));
    expect(endpoint, contains('request.persistentConnection = false'));
    expect(endpoint, contains("'http://127.0.0.1:18090'"));
    expect(endpoint, isNot(contains("'http://127.0.0.1:8090'")));
    expect(
      endpoint,
      contains('connection closed before full header was received'),
    );
    expect(generated, contains("'homeSnapshot'"));
    expect(generated, contains("'threadMessages'"));
    expect(generated, contains("'sendThreadMessage'"));
    expect(generated, contains("'threadTurnStatus'"));
    expect(generated, isNot(contains("'appServer'")));
    expect(generated, isNot(contains("'createThread'")));
    expect(generated, contains("'watchProjectUi'"));
    expect(generated, contains("'requestGenuiSurface'"));
    expect(generated, contains("'waitProjectUiEventsJson'"));
    expect(generated, contains('MethodStreamConnector'));
    expect(generated, isNot(contains("'watchThreadEvents'")));
    expect(bridge, contains("Hmac("));
    expect(bridge, contains("'X-Workroom-Signature'"));
    expect(bridge, contains('workroomPendingByteLimit = 1024 * 1024'));
    expect(bridge, contains('workroomEventBatchLimit = 100'));
    expect(Directory('lib/src/app_server').existsSync(), isFalse);

    expect(development, contains('port: 28080'));
    expect(development, contains('name: fixer_dashboard'));
    expect(compose, contains('pgvector/pgvector:pg16'));
  });
}
