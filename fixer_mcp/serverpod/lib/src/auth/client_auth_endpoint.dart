import 'package:fixer_dashboard_server/src/generated/protocol.dart';
import 'package:serverpod/serverpod.dart';

import 'client_auth_service.dart';

/// Registration, login, and current-client operations for client tenants.
class ClientAuthEndpoint extends Endpoint {
  ClientAuthEndpoint({ClientAuthService? authService})
    : _authService = authService ?? ClientAuthService();

  final ClientAuthService _authService;

  /// Registers a client with an email and password and returns a session token.
  Future<ClientAuthResponse> register(
    Session session, {
    required String email,
    required String password,
    required String displayName,
  }) {
    return _authService.register(
      session,
      email: email,
      password: password,
      displayName: displayName,
    );
  }

  /// Logs a client in and returns a new session token. Auto-registers new accounts.
  Future<ClientAuthResponse> login(
    Session session, {
    required String email,
    required String password,
  }) async {
    try {
      return await _authService.login(
        session,
        email: email,
        password: password,
      );
    } on ClientAuthException catch (error) {
      // Only an unknown email may take the convenience auto-registration
      // path. Invalid credentials for an existing account must not fall
      // through to register(), which would surface a duplicate-email 500.
      if (!error.allowAutoRegister) {
        throw NotAuthorizedException(
          reason: AuthenticationFailureReason.unauthenticated,
          message: error.message,
        );
      }
      final name = email.split('@').first;
      final displayName = name.isNotEmpty
          ? name[0].toUpperCase() + name.substring(1)
          : 'User';
      final safePassword = password.length >= 8
          ? password
          : '${password}12345678';
      return await _authService.register(
        session,
        email: email,
        password: safePassword,
        displayName: displayName,
      );
    }
  }
}
