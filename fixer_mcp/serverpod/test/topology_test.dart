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
    final development = File('config/development.yaml').readAsStringSync();
    final compose = File('docker-compose.yaml').readAsStringSync();

    expect(server, contains("'goAuthority': 'dashboard_api'"));
    expect(server, contains("'codexRuntimeAdapter': 'node_bridge'"));
    expect(server, contains("'appFacingApi': 'serverpod'"));
    expect(endpoint, contains('Future<Map<String, dynamic>> homeSnapshot'));
    expect(endpoint, contains('Future<Map<String, dynamic>> threadMessages'));
    expect(endpoint, contains("'/thread/messages/read'"));
    expect(endpoint, contains("'/turn/start'"));
    expect(endpoint, contains('Future<Map<String, dynamic>> threadTurnStatus'));
    expect(endpoint, contains("'/turn/status/"));
    expect(endpoint, contains("findProxy = (_) => 'DIRECT'"));
    expect(endpoint, contains("HttpHeaders.connectionHeader, 'close'"));
    expect(endpoint, contains('request.persistentConnection = false'));
    expect(endpoint, contains('retryConnectionDrop: false'));
    expect(
      endpoint,
      contains('connection closed before full header was received'),
    );
    expect(generated, contains("'homeSnapshot'"));
    expect(generated, contains("'threadMessages'"));
    expect(generated, contains("'sendThreadMessage'"));
    expect(generated, contains("'threadTurnStatus'"));

    expect(development, contains('port: 28080'));
    expect(development, contains('name: fixer_dashboard'));
    expect(compose, contains('pgvector/pgvector:pg16'));
  });
}
