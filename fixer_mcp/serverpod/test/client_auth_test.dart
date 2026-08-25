import 'dart:io';

import 'package:fixer_dashboard_server/src/auth/client_auth_endpoint.dart';
import 'package:fixer_dashboard_server/src/auth/client_auth_middleware.dart';
import 'package:fixer_dashboard_server/src/auth/client_profile_endpoint.dart';
import 'package:fixer_dashboard_server/src/auth/client_auth_service.dart';
import 'package:fixer_dashboard_server/src/generated/protocol.dart';
import 'package:serverpod/serverpod.dart';
import 'package:serverpod_auth_core_server/serverpod_auth_core_server.dart';
import 'package:test/test.dart';

void main() {
  group('client auth validation', () {
    test('normalizes email addresses before persistence and lookup', () {
      expect(
        ClientAuthValidation.normalizeEmail('  User@Example.COM '),
        'user@example.com',
      );
    });

    test('rejects malformed email, display name, and password input', () {
      expect(
        () => ClientAuthValidation.normalizeEmail('not-an-email'),
        throwsA(isA<ClientAuthException>()),
      );
      expect(
        () => ClientAuthValidation.normalizeEmail('user@@example.com'),
        throwsA(isA<ClientAuthException>()),
      );
      expect(
        () => ClientAuthValidation.normalizeEmail('user @example.com'),
        throwsA(isA<ClientAuthException>()),
      );
      expect(
        () => ClientAuthValidation.normalizeDisplayName('   '),
        throwsA(isA<ClientAuthException>()),
      );
      expect(
        () => ClientAuthValidation.validatePassword('short'),
        throwsA(isA<ClientAuthException>()),
      );
    });

    test('only unknown-email failures allow convenience auto-registration', () {
      const unknownEmail = ClientAuthException(
        'Invalid email or password',
        allowAutoRegister: true,
      );
      const badPassword = ClientAuthException('Invalid email or password');

      expect(unknownEmail.allowAutoRegister, isTrue);
      expect(badPassword.allowAutoRegister, isFalse);
    });
  });

  test('uses Serverpod Auth Core Argon2id hashing for passwords', () async {
    final hasher = Argon2HashUtil(
      hashPepper: 'test-password-pepper',
      hashSaltLength: 16,
      parameters: Argon2HashParameters(memory: 1024, iterations: 1, lanes: 1),
    );

    final hash = await hasher.createHashFromString(secret: 'correct horse');

    expect(hash, startsWith(r'$argon2id$'));
    expect(hash, isNot(contains('correct horse')));
    expect(
      await hasher.validateHashFromString(
        secret: 'correct horse',
        hashString: hash,
      ),
      isTrue,
    );
    expect(
      await hasher.validateHashFromString(
        secret: 'wrong horse',
        hashString: hash,
      ),
      isFalse,
    );
  });

  test('keeps password hashes out of the public auth response', () {
    final response = ClientAuthResponse(
      clientId: UuidValue.withValidation(
        '00000000-0000-7000-8000-000000000001',
      ),
      email: 'client@example.com',
      displayName: 'Client',
      sessionToken: 'session-token',
    );

    final serialized = response.toJson();
    expect(
      serialized.keys,
      containsAll(<String>['clientId', 'email', 'displayName', 'sessionToken']),
    );
    expect(serialized.keys, isNot(contains('hashedPassword')));
  });

  group('client scope middleware', () {
    test('can construct auth endpoints before Serverpod is initialized', () {
      expect(ClientAuthEndpoint(), isNotNull);
    });

    test('protects client profile endpoints with the client scope', () {
      expect(
        ClientProfileEndpoint().requiredScopes,
        ClientAuthMiddleware.requiredScopes,
      );
    });

    test('accepts client sessions and rejects other identities', () {
      final clientSession = AuthenticationInfo(
        '00000000-0000-7000-8000-000000000001',
        {ClientAuthMiddleware.clientScope},
        authId: 'client-session',
      );
      final architectSession = AuthenticationInfo(
        '00000000-0000-7000-8000-000000000002',
        {const Scope('architect')},
        authId: 'architect-session',
      );

      expect(
        EndpointDispatch.canUserAccessEndpoint(
          clientSession,
          false,
          ClientAuthMiddleware.requiredScopes,
        ),
        isNull,
      );
      expect(
        EndpointDispatch.canUserAccessEndpoint(
          architectSession,
          false,
          ClientAuthMiddleware.requiredScopes,
        ),
        AuthenticationFailureReason.insufficientAccess,
      );
      expect(
        EndpointDispatch.canUserAccessEndpoint(
          null,
          false,
          ClientAuthMiddleware.requiredScopes,
        ),
        AuthenticationFailureReason.unauthenticated,
      );
    });
  });

  test('declares the requested client table columns', () {
    final model = File(
      'lib/src/models/client_user.spy.yaml',
    ).readAsStringSync();
    final migration = File(
      'migrations/20260721103654467-client-auth/migration.sql',
    ).readAsStringSync();

    expect(model, contains('table: client'));
    expect(migration, contains('"hashed_password" text NOT NULL'));
    expect(migration, contains('"display_name" text NOT NULL'));
    expect(migration, contains('"created_at" timestamp without time zone'));
  });
}
