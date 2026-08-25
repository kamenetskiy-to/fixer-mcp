import 'package:fixer_dashboard_server/src/auth/client_auth_middleware.dart';
import 'package:fixer_dashboard_server/src/generated/protocol.dart';
import 'package:serverpod/serverpod.dart' hide Order;
import 'package:serverpod/serverpod.dart' as sp;

/// The client order-flow API used by the external client surface.
///
/// This endpoint deliberately returns the small JSON-shaped payloads used by
/// the client API instead of exposing the persistence models directly. The
/// existing [ClientOrderEndpoint] remains available for the current cockpit
/// CRUD surface while clients migrate to this flow.
class OrderEndpoint extends ClientProtectedEndpoint {
  /// Creates an order and returns its database id.
  Future<int> createOrder(
    Session session, {
    required String clientId,
    required String projectDescription,
    required int budgetCents,
  }) async {
    final normalizedClientId = _parseClientId(clientId);
    _requireRequestedClient(session, normalizedClientId);
    final normalizedDescription = _requiredText(
      projectDescription,
      'projectDescription',
    );
    if (budgetCents < 0) {
      throw ArgumentError.value(budgetCents, 'budgetCents', 'must be >= 0');
    }

    final now = DateTime.now();
    final order = await Order.db.insertRow(
      session,
      Order(
        clientId: normalizedClientId,
        projectDescription: normalizedDescription,
        budgetCents: budgetCents,
        status: 'pending',
        assignedProjectId: null,
        createdAt: now,
        updatedAt: now,
        title: _titleFromDescription(normalizedDescription),
        description: normalizedDescription,
      ),
    );
    return order.id!;
  }

  /// Lists the orders for a client in reverse creation order.
  Future<List<Map<String, dynamic>>> listOrders(
    Session session,
    String clientId,
  ) async {
    final normalizedClientId = _parseClientId(clientId);
    _requireRequestedClient(session, normalizedClientId);
    final orders = await Order.db.find(
      session,
      where: (t) => t.clientId.equals(normalizedClientId),
      orderByList: (t) => [
        sp.Order(column: t.createdAt, orderDescending: true),
      ],
    );

    return orders
        .map(
          (order) => <String, dynamic>{
            'order_id': order.id,
            'status': order.status,
            'project_id': order.assignedProjectId,
            'created_at': order.createdAt,
          },
        )
        .toList(growable: false);
  }

  /// Adds a revision to an order and returns its database id.
  Future<int> submitRevision(
    Session session, {
    required int orderId,
    required String revisionText,
    List<String>? attachmentUrls,
  }) async {
    final order = await _requireOrder(session, orderId);
    final normalizedText = _requiredText(revisionText, 'revisionText');
    final latest = await Revision.db.findFirstRow(
      session,
      where: (t) => t.orderId.equals(orderId),
      orderByList: (t) => [
        sp.Order(column: t.revisionNumber, orderDescending: true),
      ],
    );
    final now = DateTime.now();
    final revision = await Revision.db.insertRow(
      session,
      Revision(
        orderId: orderId,
        revisionNumber: (latest?.revisionNumber ?? 0) + 1,
        revisionText: normalizedText,
        attachmentUrls: attachmentUrls == null
            ? null
            : List<String>.unmodifiable(attachmentUrls),
        resultSummary: null,
        status: 'submitted',
        description: normalizedText,
        branchName: null,
        previewUrl: null,
        createdAt: now,
        updatedAt: now,
      ),
    );

    // A new revision sends the order back through the delivery pipeline.
    order.status = 'pending';
    order.updatedAt = now;
    await Order.db.updateRow(session, order);
    return revision.id!;
  }

  /// Returns the current status and all revisions for an order.
  Future<Map<String, dynamic>> orderStatus(Session session, int orderId) async {
    final order = await _requireOrder(session, orderId);
    final revisions = await Revision.db.find(
      session,
      where: (t) => t.orderId.equals(orderId),
      orderByList: (t) => [
        sp.Order(column: t.revisionNumber, orderDescending: true),
      ],
    );
    final latestResultSummary = _latestResultSummary(revisions);

    return <String, dynamic>{
      'status': order.status,
      'latest_result_summary': latestResultSummary,
      'revisions': revisions
          .map(
            (revision) => <String, dynamic>{
              'revision_id': revision.id,
              'revision_text': revision.revisionText,
              'attachment_urls': revision.attachmentUrls ?? const <String>[],
              'status': revision.status,
              'result_summary': revision.resultSummary,
              'created_at': revision.createdAt,
            },
          )
          .toList(growable: false),
    };
  }

  Future<Order> _requireOrder(Session session, int orderId) async {
    final clientId = ClientAuthMiddleware.requireClientId(session);
    final order = await Order.db.findFirstRow(
      session,
      where: (t) => t.id.equals(orderId) & t.clientId.equals(clientId),
    );
    if (order == null) {
      throw ArgumentError('Order not found');
    }
    return order;
  }

  void _requireRequestedClient(Session session, UuidValue requestedClientId) {
    final authenticatedClientId = ClientAuthMiddleware.requireClientId(session);
    if (authenticatedClientId.toString() != requestedClientId.toString()) {
      throw NotAuthorizedException(
        reason: AuthenticationFailureReason.insufficientAccess,
      );
    }
  }

  UuidValue _parseClientId(String clientId) {
    final normalized = clientId.trim();
    if (normalized.isEmpty) {
      throw ArgumentError('clientId is required');
    }
    try {
      return UuidValue.withValidation(normalized);
    } on FormatException {
      throw ArgumentError.value(clientId, 'clientId', 'must be a UUID');
    }
  }

  String _requiredText(String value, String field) {
    final normalized = value.trim();
    if (normalized.isEmpty) {
      throw ArgumentError('$field is required');
    }
    return normalized;
  }

  String _titleFromDescription(String description) {
    final firstLine = description.split('\n').first.trim();
    if (firstLine.length <= 80) {
      return firstLine;
    }
    return '${firstLine.substring(0, 77)}...';
  }

  String? _latestResultSummary(List<Revision> revisions) {
    if (revisions.isEmpty) {
      return null;
    }
    final summary = revisions.first.resultSummary?.trim();
    return summary == null || summary.isEmpty ? null : summary;
  }
}
