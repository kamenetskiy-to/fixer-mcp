import 'dart:io';

import 'package:fixer_dashboard_server/server.dart';
import 'package:serverpod/serverpod.dart';
import 'package:test/test.dart';

void main() {
  test('publishes the versioned Fixer Studio RPC contract proof', () {
    expect(fixerStudioBackendContractPath, '/fixer-studio/contract');
    expect(fixerStudioBackendContractToken, 'fixer-studio-serverpod-rpc-v1');
    expect(
      JsonWidget(object: fixerStudioBackendContract).toString(),
      '{"contract":"fixer-studio-serverpod-rpc-v1"}',
    );

    final server = File('lib/server.dart').readAsStringSync();
    expect(server, contains('_FixerStudioBackendContractRoute()'));
    expect(server, contains('fixerStudioBackendContractPath'));
    expect(server, contains('JsonWidget(object: fixerStudioBackendContract)'));
  });
}
