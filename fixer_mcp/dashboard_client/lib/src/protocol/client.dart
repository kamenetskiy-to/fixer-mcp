/* AUTOMATICALLY GENERATED CODE DO NOT MODIFY */
/*   To generate run: "serverpod generate"    */

// ignore_for_file: implementation_imports
// ignore_for_file: library_private_types_in_public_api
// ignore_for_file: non_constant_identifier_names
// ignore_for_file: public_member_api_docs
// ignore_for_file: type_literal_in_constant_pattern
// ignore_for_file: use_super_parameters
// ignore_for_file: invalid_use_of_internal_member

// ignore_for_file: no_leading_underscores_for_library_prefixes
import 'package:serverpod_client/serverpod_client.dart' as _i1;
import 'dart:async' as _i2;
import 'package:fixer_dashboard_client/src/protocol/client_auth_response.dart'
    as _i3;
import 'package:fixer_dashboard_client/src/protocol/client_profile.dart' as _i4;
import 'package:fixer_dashboard_client/src/protocol/project_workroom_snapshot.dart'
    as _i5;
import 'package:fixer_dashboard_client/src/protocol/project_ui_frame.dart'
    as _i6;
import 'package:fixer_dashboard_client/src/protocol/fixer_turn_receipt.dart'
    as _i7;
import 'package:fixer_dashboard_client/src/protocol/genui_action_receipt.dart'
    as _i8;
import 'package:fixer_dashboard_client/src/protocol/genui_action_request.dart'
    as _i9;
import 'package:fixer_dashboard_client/src/protocol/hands_instruction_receipt.dart'
    as _i10;
import 'package:fixer_dashboard_client/src/protocol/hands_instruction_request.dart'
    as _i11;
import 'package:fixer_dashboard_client/src/protocol/command_receipt.dart'
    as _i12;
import 'package:fixer_dashboard_client/src/protocol/order.dart' as _i13;
import 'package:fixer_dashboard_client/src/protocol/revision.dart' as _i14;
import 'package:serverpod_auth_core_client/serverpod_auth_core_client.dart'
    as _i15;
import 'protocol.dart' as _i16;

/// Registration, login, and current-client operations for client tenants.
/// {@category Endpoint}
class EndpointClientAuth extends _i1.EndpointRef {
  EndpointClientAuth(_i1.EndpointCaller caller) : super(caller);

  @override
  String get name => 'clientAuth';

  /// Registers a client with an email and password and returns a session token.
  _i2.Future<_i3.ClientAuthResponse> register({
    required String email,
    required String password,
    required String displayName,
  }) => caller.callServerEndpoint<_i3.ClientAuthResponse>(
    'clientAuth',
    'register',
    {'email': email, 'password': password, 'displayName': displayName},
  );

  /// Logs a client in and returns a new session token. Auto-registers new accounts.
  _i2.Future<_i3.ClientAuthResponse> login({
    required String email,
    required String password,
  }) => caller.callServerEndpoint<_i3.ClientAuthResponse>(
    'clientAuth',
    'login',
    {'email': email, 'password': password},
  );
}

/// Base endpoint for methods that are only available to client tenants.
/// {@category Endpoint}
abstract class EndpointClientProtected extends _i1.EndpointRef {
  EndpointClientProtected(_i1.EndpointCaller caller) : super(caller);
}

/// Protected client-tenant profile operations.
/// {@category Endpoint}
class EndpointClientProfile extends EndpointClientProtected {
  EndpointClientProfile(_i1.EndpointCaller caller) : super(caller);

  @override
  String get name => 'clientProfile';

  /// Returns the profile represented by the validated client session token.
  _i2.Future<_i4.ClientProfile> current() => caller
      .callServerEndpoint<_i4.ClientProfile>('clientProfile', 'current', {});
}

/// {@category Endpoint}
class EndpointDashboardRuntime extends EndpointClientProtected {
  EndpointDashboardRuntime(_i1.EndpointCaller caller) : super(caller);

  @override
  String get name => 'dashboardRuntime';

  _i2.Future<String> health() =>
      caller.callServerEndpoint<String>('dashboardRuntime', 'health', {});

  _i2.Future<List<String>> topology() => caller
      .callServerEndpoint<List<String>>('dashboardRuntime', 'topology', {});

  /// Returns one SQLite-consistent read model and the exact journal watermark
  /// from which the client starts its replay stream.
  _i2.Future<_i5.ProjectWorkroomSnapshot> getProjectWorkroomSnapshot(
    int projectId,
  ) => caller.callServerEndpoint<_i5.ProjectWorkroomSnapshot>(
    'dashboardRuntime',
    'getProjectWorkroomSnapshot',
    {'projectId': projectId},
  );

  /// Replays the authoritative journal after [afterSeq], then long-tails it.
  /// The pump holds at most one bounded Go batch when the WebSocket listener
  /// pauses and force-closes its active HTTP wait on cancellation.
  _i2.Stream<_i6.ProjectUiFrame> watchProjectUi(
    int projectId,
    int afterSeq,
    int protocolVersion,
  ) =>
      caller.callStreamingServerEndpoint<
        _i2.Stream<_i6.ProjectUiFrame>,
        _i6.ProjectUiFrame
      >('dashboardRuntime', 'watchProjectUi', {
        'projectId': projectId,
        'afterSeq': afterSeq,
        'protocolVersion': protocolVersion,
      }, {});

  _i2.Future<_i7.FixerTurnReceipt> sendFixerTurn(
    int projectId,
    String threadId,
    String content,
    String idempotencyKey,
  ) => caller.callServerEndpoint<_i7.FixerTurnReceipt>(
    'dashboardRuntime',
    'sendFixerTurn',
    {
      'projectId': projectId,
      'threadId': threadId,
      'content': content,
      'idempotencyKey': idempotencyKey,
    },
  );

  _i2.Future<_i8.GenuiActionReceipt> requestGenuiSurface(
    int projectId,
    String threadId,
    String surfaceType,
    int surfaceVersion,
    String argumentsJson,
    String idempotencyKey,
  ) => caller.callServerEndpoint<_i8.GenuiActionReceipt>(
    'dashboardRuntime',
    'requestGenuiSurface',
    {
      'projectId': projectId,
      'threadId': threadId,
      'surfaceType': surfaceType,
      'surfaceVersion': surfaceVersion,
      'argumentsJson': argumentsJson,
      'idempotencyKey': idempotencyKey,
    },
  );

  /// Bounded long-poll fallback for clients whose generated protocol artifact
  /// does not yet include the typed Workroom stream. Journal sequence remains
  /// authoritative, so reconnect resumes from the caller's committed cursor.
  _i2.Future<Map<String, dynamic>> waitProjectUiEventsJson(
    int projectId,
    int afterSeq,
    int protocolVersion,
  ) => caller.callServerEndpoint<Map<String, dynamic>>(
    'dashboardRuntime',
    'waitProjectUiEventsJson',
    {
      'projectId': projectId,
      'afterSeq': afterSeq,
      'protocolVersion': protocolVersion,
    },
  );

  _i2.Future<_i8.GenuiActionReceipt> invokeGenuiAction(
    _i9.GenuiActionRequest request,
  ) => caller.callServerEndpoint<_i8.GenuiActionReceipt>(
    'dashboardRuntime',
    'invokeGenuiAction',
    {'request': request},
  );

  _i2.Future<_i10.HandsInstructionReceipt> submitHandsInstruction(
    _i11.HandsInstructionRequest request,
  ) => caller.callServerEndpoint<_i10.HandsInstructionReceipt>(
    'dashboardRuntime',
    'submitHandsInstruction',
    {'request': request},
  );

  _i2.Future<_i8.GenuiActionReceipt> selectHandsLane(
    int projectId,
    String provider,
    String idempotencyKey,
  ) => caller.callServerEndpoint<_i8.GenuiActionReceipt>(
    'dashboardRuntime',
    'selectHandsLane',
    {
      'projectId': projectId,
      'provider': provider,
      'idempotencyKey': idempotencyKey,
    },
  );

  _i2.Future<_i12.CommandReceipt> cancelHandsInstruction(
    int projectId,
    String instructionId,
    String idempotencyKey,
  ) => caller.callServerEndpoint<_i12.CommandReceipt>(
    'dashboardRuntime',
    'cancelHandsInstruction',
    {
      'projectId': projectId,
      'instructionId': instructionId,
      'idempotencyKey': idempotencyKey,
    },
  );

  _i2.Future<Map<String, dynamic>> homeSnapshot() =>
      caller.callServerEndpoint<Map<String, dynamic>>(
        'dashboardRuntime',
        'homeSnapshot',
        {},
      );

  _i2.Future<Map<String, dynamic>> projectSnapshot(int projectId) =>
      caller.callServerEndpoint<Map<String, dynamic>>(
        'dashboardRuntime',
        'projectSnapshot',
        {'projectId': projectId},
      );

  _i2.Future<Map<String, dynamic>> projectDocs(int projectId) =>
      caller.callServerEndpoint<Map<String, dynamic>>(
        'dashboardRuntime',
        'projectDocs',
        {'projectId': projectId},
      );

  _i2.Future<Map<String, dynamic>> threadBinding(int projectId) =>
      caller.callServerEndpoint<Map<String, dynamic>>(
        'dashboardRuntime',
        'threadBinding',
        {'projectId': projectId},
      );

  _i2.Future<Map<String, dynamic>> sessionDetail(int sessionId) =>
      caller.callServerEndpoint<Map<String, dynamic>>(
        'dashboardRuntime',
        'sessionDetail',
        {'sessionId': sessionId},
      );

  _i2.Future<Map<String, dynamic>> threadMessages(String threadId) =>
      caller.callServerEndpoint<Map<String, dynamic>>(
        'dashboardRuntime',
        'threadMessages',
        {'threadId': threadId},
      );

  _i2.Future<Map<String, dynamic>> sendThreadMessage(
    String threadId,
    String prompt,
    String model,
    String reasoning,
  ) => caller.callServerEndpoint<Map<String, dynamic>>(
    'dashboardRuntime',
    'sendThreadMessage',
    {
      'threadId': threadId,
      'prompt': prompt,
      'model': model,
      'reasoning': reasoning,
    },
  );

  _i2.Future<Map<String, dynamic>> threadTurnStatus(String streamId) =>
      caller.callServerEndpoint<Map<String, dynamic>>(
        'dashboardRuntime',
        'threadTurnStatus',
        {'streamId': streamId},
      );
}

/// CRUD operations for client-owned orders and their revisions.
/// {@category Endpoint}
class EndpointClientOrder extends EndpointClientProtected {
  EndpointClientOrder(_i1.EndpointCaller caller) : super(caller);

  @override
  String get name => 'clientOrder';

  /// Creates a draft order for the authenticated client.
  _i2.Future<_i13.Order> createOrder({
    required String title,
    required String description,
  }) => caller.callServerEndpoint<_i13.Order>('clientOrder', 'createOrder', {
    'title': title,
    'description': description,
  });

  /// Lists the authenticated client's orders, newest updates first.
  _i2.Future<List<_i13.Order>> listOrders() => caller
      .callServerEndpoint<List<_i13.Order>>('clientOrder', 'listOrders', {});

  /// Fetches one order only when it belongs to the authenticated client.
  _i2.Future<_i13.Order?> getOrder(int orderId) =>
      caller.callServerEndpoint<_i13.Order?>('clientOrder', 'getOrder', {
        'orderId': orderId,
      });

  /// Updates the editable client-facing fields of an order.
  _i2.Future<_i13.Order> updateOrder({
    required int orderId,
    required String title,
    required String description,
  }) => caller.callServerEndpoint<_i13.Order>('clientOrder', 'updateOrder', {
    'orderId': orderId,
    'title': title,
    'description': description,
  });

  /// Deletes an order and all of its revisions.
  _i2.Future<bool> deleteOrder(int orderId) => caller.callServerEndpoint<bool>(
    'clientOrder',
    'deleteOrder',
    {'orderId': orderId},
  );

  /// Creates the next draft revision for an order.
  _i2.Future<_i14.Revision> createRevision({
    required int orderId,
    required String description,
  }) => caller.callServerEndpoint<_i14.Revision>(
    'clientOrder',
    'createRevision',
    {'orderId': orderId, 'description': description},
  );

  /// Lists revisions belonging to an order owned by the authenticated client.
  _i2.Future<List<_i14.Revision>> listRevisions(int orderId) =>
      caller.callServerEndpoint<List<_i14.Revision>>(
        'clientOrder',
        'listRevisions',
        {'orderId': orderId},
      );

  /// Fetches one revision only when its parent order is client-owned.
  _i2.Future<_i14.Revision?> getRevision(int revisionId) =>
      caller.callServerEndpoint<_i14.Revision?>('clientOrder', 'getRevision', {
        'revisionId': revisionId,
      });

  /// Updates the editable description of a revision.
  _i2.Future<_i14.Revision> updateRevision({
    required int revisionId,
    required String description,
  }) => caller.callServerEndpoint<_i14.Revision>(
    'clientOrder',
    'updateRevision',
    {'revisionId': revisionId, 'description': description},
  );

  /// Deletes a revision belonging to the authenticated client's order.
  _i2.Future<bool> deleteRevision(int revisionId) =>
      caller.callServerEndpoint<bool>('clientOrder', 'deleteRevision', {
        'revisionId': revisionId,
      });
}

/// Architect actions that finalize a client order and deliver its result.
///
/// This endpoint intentionally has no client scope: the client-facing CRUD
/// endpoint is protected by [ClientProtectedEndpoint], while consolidation is
/// an Architect-side operation.
/// {@category Endpoint}
class EndpointOrderDelivery extends _i1.EndpointRef {
  EndpointOrderDelivery(_i1.EndpointCaller caller) : super(caller);

  @override
  String get name => 'orderDelivery';

  /// Merges the accepted result and makes it available to the client.
  ///
  /// The operation is idempotent. Re-merging an already completed order keeps
  /// the completed state and republishes the client update.
  _i2.Future<_i13.Order> mergeOrder(
    int orderId, {
    required String resultSummary,
  }) => caller.callServerEndpoint<_i13.Order>('orderDelivery', 'mergeOrder', {
    'orderId': orderId,
    'resultSummary': resultSummary,
  });

  /// Approval is an explicit alias for the Architect UI's merge action.
  _i2.Future<_i13.Order> approveOrder(
    int orderId, {
    required String resultSummary,
  }) => caller.callServerEndpoint<_i13.Order>('orderDelivery', 'approveOrder', {
    'orderId': orderId,
    'resultSummary': resultSummary,
  });

  /// Rejects an order result and notifies the client cockpit.
  _i2.Future<_i13.Order> rejectOrder(int orderId) =>
      caller.callServerEndpoint<_i13.Order>('orderDelivery', 'rejectOrder', {
        'orderId': orderId,
      });
}

/// The client order-flow API used by the external client surface.
///
/// This endpoint deliberately returns the small JSON-shaped payloads used by
/// the client API instead of exposing the persistence models directly. The
/// existing [ClientOrderEndpoint] remains available for the current cockpit
/// CRUD surface while clients migrate to this flow.
/// {@category Endpoint}
class EndpointOrder extends EndpointClientProtected {
  EndpointOrder(_i1.EndpointCaller caller) : super(caller);

  @override
  String get name => 'order';

  /// Creates an order and returns its database id.
  _i2.Future<int> createOrder({
    required String clientId,
    required String projectDescription,
    required int budgetCents,
  }) => caller.callServerEndpoint<int>('order', 'createOrder', {
    'clientId': clientId,
    'projectDescription': projectDescription,
    'budgetCents': budgetCents,
  });

  /// Lists the orders for a client in reverse creation order.
  _i2.Future<List<Map<String, dynamic>>> listOrders(String clientId) =>
      caller.callServerEndpoint<List<Map<String, dynamic>>>(
        'order',
        'listOrders',
        {'clientId': clientId},
      );

  /// Adds a revision to an order and returns its database id.
  _i2.Future<int> submitRevision({
    required int orderId,
    required String revisionText,
    List<String>? attachmentUrls,
  }) => caller.callServerEndpoint<int>('order', 'submitRevision', {
    'orderId': orderId,
    'revisionText': revisionText,
    'attachmentUrls': attachmentUrls,
  });

  /// Returns the current status and all revisions for an order.
  _i2.Future<Map<String, dynamic>> orderStatus(int orderId) =>
      caller.callServerEndpoint<Map<String, dynamic>>('order', 'orderStatus', {
        'orderId': orderId,
      });
}

class Modules {
  Modules(Client client) {
    serverpod_auth_core = _i15.Caller(client);
  }

  late final _i15.Caller serverpod_auth_core;
}

class Client extends _i1.ServerpodClientShared {
  Client(
    String host, {
    dynamic securityContext,
    @Deprecated(
      'Use authKeyProvider instead. This will be removed in future releases.',
    )
    super.authenticationKeyManager,
    Duration? streamingConnectionTimeout,
    Duration? connectionTimeout,
    Function(_i1.MethodCallContext, Object, StackTrace)? onFailedCall,
    Function(_i1.MethodCallContext)? onSucceededCall,
    bool? disconnectStreamsOnLostInternetConnection,
  }) : super(
         host,
         _i16.Protocol(),
         securityContext: securityContext,
         streamingConnectionTimeout: streamingConnectionTimeout,
         connectionTimeout: connectionTimeout,
         onFailedCall: onFailedCall,
         onSucceededCall: onSucceededCall,
         disconnectStreamsOnLostInternetConnection:
             disconnectStreamsOnLostInternetConnection,
       ) {
    clientAuth = EndpointClientAuth(this);
    clientProfile = EndpointClientProfile(this);
    dashboardRuntime = EndpointDashboardRuntime(this);
    clientOrder = EndpointClientOrder(this);
    orderDelivery = EndpointOrderDelivery(this);
    order = EndpointOrder(this);
    modules = Modules(this);
  }

  late final EndpointClientAuth clientAuth;

  late final EndpointClientProfile clientProfile;

  late final EndpointDashboardRuntime dashboardRuntime;

  late final EndpointClientOrder clientOrder;

  late final EndpointOrderDelivery orderDelivery;

  late final EndpointOrder order;

  late final Modules modules;

  @override
  Map<String, _i1.EndpointRef> get endpointRefLookup => {
    'clientAuth': clientAuth,
    'clientProfile': clientProfile,
    'dashboardRuntime': dashboardRuntime,
    'clientOrder': clientOrder,
    'orderDelivery': orderDelivery,
    'order': order,
  };

  @override
  Map<String, _i1.ModuleEndpointCaller> get moduleLookup => {
    'serverpod_auth_core': modules.serverpod_auth_core,
  };
}
