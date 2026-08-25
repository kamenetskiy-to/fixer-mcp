import 'package:fixer_dashboard_server/src/generated/protocol.dart';
import 'package:serverpod/serverpod.dart';
import 'package:serverpod_auth_core_server/serverpod_auth_core_server.dart';

import 'client_auth_middleware.dart';

/// Password hashing and client-session orchestration for the client tenant.
class ClientAuthService {
  ClientAuthService({Argon2HashUtil? passwordHasher})
    : _passwordHasher = passwordHasher;

  Argon2HashUtil? _passwordHasher;

  Argon2HashUtil get passwordHasher =>
      _passwordHasher ??= _defaultPasswordHasher();

  static Argon2HashUtil _defaultPasswordHasher() {
    return Argon2HashUtil(
      hashPepper: _requiredPassword('clientPasswordHashPepper'),
      hashSaltLength: 16,
    );
  }

  static String _requiredPassword(String key) {
    final password = Serverpod.instance.getPassword(key);
    if (password == null || password.isEmpty) {
      throw StateError('Missing password configuration: $key');
    }
    return password;
  }

  /// Registers a client and immediately creates its first session.
  Future<ClientAuthResponse> register(
    Session session, {
    required String email,
    required String password,
    required String displayName,
  }) async {
    final normalizedEmail = ClientAuthValidation.normalizeEmail(email);
    ClientAuthValidation.validatePassword(password);
    final normalizedDisplayName = ClientAuthValidation.normalizeDisplayName(
      displayName,
    );

    final passwordHash = await passwordHasher.createHashFromString(
      secret: password,
    );

    return session.db.transaction((transaction) async {
      final existingClient = await ClientUser.db.findFirstRow(
        session,
        where: (table) => table.email.equals(normalizedEmail),
        transaction: transaction,
        lockMode: LockMode.forUpdate,
      );
      if (existingClient != null) {
        throw ClientAuthException('A client with this email already exists');
      }

      final authUser = await AuthServices.instance.authUsers.create(
        session,
        scopes: ClientAuthMiddleware.requiredScopes,
        transaction: transaction,
      );
      final client = await ClientUser.db.insertRow(
        session,
        ClientUser(
          id: authUser.id,
          email: normalizedEmail,
          hashedPassword: passwordHash,
          displayName: normalizedDisplayName,
          createdAt: DateTime.now(),
        ),
        transaction: transaction,
      );

      return _createSession(session, client, transaction: transaction);
    });
  }

  /// Authenticates a client and returns a new durable session token.
  Future<ClientAuthResponse> login(
    Session session, {
    required String email,
    required String password,
  }) async {
    final normalizedEmail = ClientAuthValidation.normalizeEmail(email);
    final client = await ClientUser.db.findFirstRow(
      session,
      where: (table) => table.email.equals(normalizedEmail),
    );

    // Keep the failure indistinguishable for unknown emails and bad passwords.
    if (client == null) {
      throw ClientAuthException(
        'Invalid email or password',
        allowAutoRegister: true,
      );
    }
    if (!await passwordHasher.validateHashFromString(
      secret: password,
      hashString: client.hashedPassword,
    )) {
      throw ClientAuthException('Invalid email or password');
    }

    return _createSession(session, client);
  }

  Future<ClientAuthResponse> _createSession(
    Session session,
    ClientUser client, {
    Transaction? transaction,
  }) async {
    final authSuccess = await AuthServices.instance.tokenManager.issueToken(
      session,
      authUserId: client.id!,
      method: 'client-password',
      scopes: ClientAuthMiddleware.requiredScopes,
      transaction: transaction,
    );

    return ClientAuthResponse(
      clientId: client.id!,
      email: client.email,
      displayName: client.displayName,
      sessionToken: authSuccess.token,
    );
  }
}

/// Input validation shared by registration and login.
abstract final class ClientAuthValidation {
  static String normalizeEmail(String email) {
    final normalized = email.trim().toLowerCase();
    final atSign = normalized.indexOf('@');
    if (atSign <= 0 ||
        atSign != normalized.lastIndexOf('@') ||
        atSign == normalized.length - 1 ||
        normalized.contains(RegExp(r'\s'))) {
      throw ClientAuthException('A valid email is required');
    }
    return normalized;
  }

  static String normalizeDisplayName(String displayName) {
    final normalized = displayName.trim();
    if (normalized.isEmpty || normalized.length > 120) {
      throw ClientAuthException(
        'Display name must be between 1 and 120 characters',
      );
    }
    return normalized;
  }

  static void validatePassword(String password) {
    if (password.length < 8 || password.length > 256) {
      throw ClientAuthException(
        'Password must be between 8 and 256 characters',
      );
    }
  }
}

/// A safe, non-sensitive authentication failure returned by client endpoints.
class ClientAuthException implements Exception {
  const ClientAuthException(this.message, {this.allowAutoRegister = false});

  final String message;
  final bool allowAutoRegister;

  @override
  String toString() => message;
}
