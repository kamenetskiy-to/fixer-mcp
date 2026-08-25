import 'package:fixer_dashboard_server/src/auth/client_auth_middleware.dart';
import 'package:fixer_dashboard_server/src/generated/protocol.dart';
import 'package:serverpod/serverpod.dart' hide Order;
import 'package:serverpod/serverpod.dart' as sp;

/// CRUD operations for client-owned orders and their revisions.
class ClientOrderEndpoint extends ClientProtectedEndpoint {
  /// Creates a draft order for the authenticated client.
  Future<Order> createOrder(
    Session session, {
    required String title,
    required String description,
  }) async {
    final clientId = ClientAuthMiddleware.requireClientId(session);
    final now = DateTime.now();
    return Order.db.insertRow(
      session,
      Order(
        clientId: clientId,
        projectDescription: _requiredText(description, 'description'),
        budgetCents: 0,
        assignedProjectId: null,
        title: _requiredText(title, 'title'),
        description: _requiredText(description, 'description'),
        status: 'draft',
        createdAt: now,
        updatedAt: now,
      ),
    );
  }

  /// Lists the authenticated client's orders, newest updates first.
  Future<List<Order>> listOrders(Session session) async {
    final clientId = ClientAuthMiddleware.requireClientId(session);
    return Order.db.find(
      session,
      where: (t) => t.clientId.equals(clientId),
      orderByList: (t) => [
        sp.Order(column: t.updatedAt, orderDescending: true),
      ],
    );
  }

  /// Fetches one order only when it belongs to the authenticated client.
  Future<Order?> getOrder(Session session, int orderId) async {
    final clientId = ClientAuthMiddleware.requireClientId(session);
    return Order.db.findFirstRow(
      session,
      where: (t) => t.id.equals(orderId) & t.clientId.equals(clientId),
    );
  }

  /// Updates the editable client-facing fields of an order.
  Future<Order> updateOrder(
    Session session, {
    required int orderId,
    required String title,
    required String description,
  }) async {
    final order = await _requireOrder(session, orderId);
    order.title = _requiredText(title, 'title');
    order.description = _requiredText(description, 'description');
    order.updatedAt = DateTime.now();
    return Order.db.updateRow(session, order);
  }

  /// Deletes an order and all of its revisions.
  Future<bool> deleteOrder(Session session, int orderId) async {
    final order = await _requireOrder(session, orderId);
    await Revision.db.deleteWhere(
      session,
      where: (t) => t.orderId.equals(orderId),
    );
    await Order.db.deleteRow(session, order);
    return true;
  }

  /// Creates the next draft revision for an order.
  Future<Revision> createRevision(
    Session session, {
    required int orderId,
    required String description,
  }) async {
    final order = await _requireOrder(session, orderId);
    final latest = await Revision.db.findFirstRow(
      session,
      where: (t) => t.orderId.equals(order.id!),
      orderByList: (t) => [
        sp.Order(column: t.revisionNumber, orderDescending: true),
      ],
    );
    final now = DateTime.now();
    final revision = await Revision.db.insertRow(
      session,
      Revision(
        orderId: order.id!,
        revisionNumber: (latest?.revisionNumber ?? 0) + 1,
        revisionText: _requiredText(description, 'description'),
        attachmentUrls: null,
        resultSummary: null,
        description: _requiredText(description, 'description'),
        status: 'draft',
        createdAt: now,
        updatedAt: now,
      ),
    );
    order.updatedAt = now;
    await Order.db.updateRow(session, order);
    return revision;
  }

  /// Lists revisions belonging to an order owned by the authenticated client.
  Future<List<Revision>> listRevisions(Session session, int orderId) async {
    await _requireOrder(session, orderId);
    return Revision.db.find(
      session,
      where: (t) => t.orderId.equals(orderId),
      orderByList: (t) => [
        sp.Order(column: t.revisionNumber, orderDescending: true),
      ],
    );
  }

  /// Fetches one revision only when its parent order is client-owned.
  Future<Revision?> getRevision(Session session, int revisionId) async {
    final revision = await Revision.db.findById(session, revisionId);
    if (revision == null) {
      return null;
    }
    await _requireOrder(session, revision.orderId);
    return revision;
  }

  /// Updates the editable description of a revision.
  Future<Revision> updateRevision(
    Session session, {
    required int revisionId,
    required String description,
  }) async {
    final revision = await _requireRevision(session, revisionId);
    revision.description = _requiredText(description, 'description');
    final now = DateTime.now();
    revision.updatedAt = now;
    final updated = await Revision.db.updateRow(session, revision);
    final order = await _requireOrder(session, revision.orderId);
    order.updatedAt = now;
    await Order.db.updateRow(session, order);
    return updated;
  }

  /// Deletes a revision belonging to the authenticated client's order.
  Future<bool> deleteRevision(Session session, int revisionId) async {
    final revision = await _requireRevision(session, revisionId);
    await Revision.db.deleteRow(session, revision);
    final order = await _requireOrder(session, revision.orderId);
    order.updatedAt = DateTime.now();
    await Order.db.updateRow(session, order);
    return true;
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

  Future<Revision> _requireRevision(Session session, int revisionId) async {
    final revision = await Revision.db.findById(session, revisionId);
    if (revision == null) {
      throw ArgumentError('Revision not found');
    }
    await _requireOrder(session, revision.orderId);
    return revision;
  }

  String _requiredText(String value, String field) {
    final normalized = value.trim();
    if (normalized.isEmpty) {
      throw ArgumentError('$field is required');
    }
    return normalized;
  }
}
