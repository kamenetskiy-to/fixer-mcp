import 'dart:io';

import 'package:serverpod/serverpod.dart';
import 'package:serverpod_auth_core_server/serverpod_auth_core_server.dart'
    hide Endpoints, Protocol;

import 'src/generated/endpoints.dart';
import 'src/generated/protocol.dart';

const fixerStudioBackendContractPath = '/fixer-studio/contract';
const fixerStudioBackendContractToken = 'fixer-studio-serverpod-rpc-v1';
const fixerStudioBackendContract = <String, String>{
  'contract': fixerStudioBackendContractToken,
};

void run(List<String> args) async {
  final pod = Serverpod(args, Protocol(), Endpoints());

  // Serverpod Auth Core owns token parsing, hashing, expiry, and revocation.
  // ClientAuthMiddleware applies the client-only scope at protected endpoints.
  pod.initializeAuthServices(
    tokenManagerBuilders: [
      ServerSideSessionsConfigFromPasswords(
        defaultSessionLifetime: const Duration(days: 30),
      ),
    ],
  );

  pod.webServer.addRoute(_DashboardHealthRoute(), '/health');
  pod.webServer.addRoute(
    _FixerStudioBackendContractRoute(),
    fixerStudioBackendContractPath,
  );

  final staticDir = Directory(Uri(path: 'web/static').toFilePath());
  if (staticDir.existsSync()) {
    pod.webServer.addRoute(StaticRoute.directory(staticDir), '/static/');
  }

  await pod.start();
}

/// Public, non-secret proof that this Serverpod exposes the RPC surface used
/// by the installed Fixer Studio clients. The token changes only when that
/// compatibility contract changes.
class _FixerStudioBackendContractRoute extends WidgetRoute {
  @override
  Future<WebWidget> build(Session session, Request request) async {
    return JsonWidget(object: fixerStudioBackendContract);
  }
}

class _DashboardHealthRoute extends WidgetRoute {
  @override
  Future<WebWidget> build(Session session, Request request) async {
    return JsonWidget(
      object: {
        'ok': true,
        'service': 'fixer_dashboard_server',
        'topology': {
          'goAuthority': 'dashboard_api',
          'codexRuntimeAdapter': 'node_bridge',
          'appFacingApi': 'serverpod',
        },
      },
    );
  }
}
