import 'package:fixer_dashboard_server/src/generated/protocol.dart';
import 'package:serverpod/serverpod.dart' hide Order;
import 'package:serverpod/serverpod.dart' as sp;

/// Architect actions that finalize a client order and deliver its result.
///
/// This endpoint intentionally has no client scope: the client-facing CRUD
/// endpoint is protected by [ClientProtectedEndpoint], while consolidation is
/// an Architect-side operation.
class OrderDeliveryEndpoint extends Endpoint {
  /// Merges the accepted result and makes it available to the client.
  ///
  /// The operation is idempotent. Re-merging an already completed order keeps
  /// the completed state and republishes the client update.
  Future<Order> mergeOrder(
    Session session,
    int orderId, {
    required String resultSummary,
  }) async {
    final order = await _requireOrder(session, orderId);
    final normalizedResultSummary = _requiredText(
      resultSummary,
      'resultSummary',
    );
    final now = DateTime.now();

    order.status = 'completed';
    order.updatedAt = now;
    final completedOrder = await Order.db.updateRow(session, order);

    final revision = await Revision.db.findFirstRow(
      session,
      where: (t) => t.orderId.equals(orderId),
      orderByList: (t) => [
        sp.Order(column: t.revisionNumber, orderDescending: true),
      ],
    );
    if (revision != null) {
      revision.status = 'completed';
      revision.resultSummary = normalizedResultSummary;
      revision.updatedAt = now;
      await Revision.db.updateRow(session, revision);
    }

    // The message is the simulated delivery notification consumed by the
    // client cockpit. The updated Order remains the endpoint response.
    await session.messages.postMessage(
      'order-updates-$orderId',
      completedOrder,
    );
    return completedOrder;
  }

  /// Approval is an explicit alias for the Architect UI's merge action.
  Future<Order> approveOrder(
    Session session,
    int orderId, {
    required String resultSummary,
  }) {
    return mergeOrder(session, orderId, resultSummary: resultSummary);
  }

  /// Rejects an order result and notifies the client cockpit.
  Future<Order> rejectOrder(Session session, int orderId) async {
    final order = await _requireOrder(session, orderId);
    final now = DateTime.now();
    order.status = 'rejected';
    order.updatedAt = now;
    final rejectedOrder = await Order.db.updateRow(session, order);

    final revision = await Revision.db.findFirstRow(
      session,
      where: (t) => t.orderId.equals(orderId),
      orderByList: (t) => [
        sp.Order(column: t.revisionNumber, orderDescending: true),
      ],
    );
    if (revision != null) {
      revision.status = 'rejected';
      revision.updatedAt = now;
      await Revision.db.updateRow(session, revision);
    }

    await session.messages.postMessage('order-updates-$orderId', rejectedOrder);
    return rejectedOrder;
  }

  Future<Order> _requireOrder(Session session, int orderId) async {
    final order = await Order.db.findById(session, orderId);
    if (order == null) {
      throw ArgumentError('Order not found');
    }
    return order;
  }

  String _requiredText(String value, String field) {
    final normalized = value.trim();
    if (normalized.isEmpty) {
      throw ArgumentError('$field is required');
    }
    return normalized;
  }
}
