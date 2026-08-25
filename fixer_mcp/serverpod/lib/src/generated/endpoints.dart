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
import 'package:serverpod/serverpod.dart' as _i1;
import '../auth/client_auth_endpoint.dart' as _i2;
import '../auth/client_profile_endpoint.dart' as _i3;
import '../endpoints/dashboard_runtime_endpoint.dart' as _i4;
import '../orders/client_order_endpoint.dart' as _i5;
import '../orders/order_delivery_endpoint.dart' as _i6;
import '../orders/order_endpoint.dart' as _i7;
import 'package:fixer_dashboard_server/src/generated/genui_action_request.dart'
    as _i8;
import 'package:fixer_dashboard_server/src/generated/hands_instruction_request.dart'
    as _i9;
import 'package:serverpod_auth_core_server/serverpod_auth_core_server.dart'
    as _i10;
import 'package:fixer_dashboard_server/src/generated/future_calls.dart' as _i11;
export 'future_calls.dart' show ServerpodFutureCallsGetter;

class Endpoints extends _i1.EndpointDispatch {
  @override
  void initializeEndpoints(_i1.Server server) {
    var endpoints = <String, _i1.Endpoint>{
      'clientAuth': _i2.ClientAuthEndpoint()
        ..initialize(server, 'clientAuth', null),
      'clientProfile': _i3.ClientProfileEndpoint()
        ..initialize(server, 'clientProfile', null),
      'dashboardRuntime': _i4.DashboardRuntimeEndpoint()
        ..initialize(server, 'dashboardRuntime', null),
      'clientOrder': _i5.ClientOrderEndpoint()
        ..initialize(server, 'clientOrder', null),
      'orderDelivery': _i6.OrderDeliveryEndpoint()
        ..initialize(server, 'orderDelivery', null),
      'order': _i7.OrderEndpoint()..initialize(server, 'order', null),
    };
    connectors['clientAuth'] = _i1.EndpointConnector(
      name: 'clientAuth',
      endpoint: endpoints['clientAuth']!,
      methodConnectors: {
        'register': _i1.MethodConnector(
          name: 'register',
          params: {
            'email': _i1.ParameterDescription(
              name: 'email',
              type: _i1.getType<String>(),
              nullable: false,
            ),
            'password': _i1.ParameterDescription(
              name: 'password',
              type: _i1.getType<String>(),
              nullable: false,
            ),
            'displayName': _i1.ParameterDescription(
              name: 'displayName',
              type: _i1.getType<String>(),
              nullable: false,
            ),
          },
          call: (_i1.Session session, Map<String, dynamic> params) async =>
              (endpoints['clientAuth'] as _i2.ClientAuthEndpoint).register(
                session,
                email: params['email'],
                password: params['password'],
                displayName: params['displayName'],
              ),
        ),
        'login': _i1.MethodConnector(
          name: 'login',
          params: {
            'email': _i1.ParameterDescription(
              name: 'email',
              type: _i1.getType<String>(),
              nullable: false,
            ),
            'password': _i1.ParameterDescription(
              name: 'password',
              type: _i1.getType<String>(),
              nullable: false,
            ),
          },
          call: (_i1.Session session, Map<String, dynamic> params) async =>
              (endpoints['clientAuth'] as _i2.ClientAuthEndpoint).login(
                session,
                email: params['email'],
                password: params['password'],
              ),
        ),
      },
    );
    connectors['clientProfile'] = _i1.EndpointConnector(
      name: 'clientProfile',
      endpoint: endpoints['clientProfile']!,
      methodConnectors: {
        'current': _i1.MethodConnector(
          name: 'current',
          params: {},
          call: (_i1.Session session, Map<String, dynamic> params) async =>
              (endpoints['clientProfile'] as _i3.ClientProfileEndpoint).current(
                session,
              ),
        ),
      },
    );
    connectors['dashboardRuntime'] = _i1.EndpointConnector(
      name: 'dashboardRuntime',
      endpoint: endpoints['dashboardRuntime']!,
      methodConnectors: {
        'health': _i1.MethodConnector(
          name: 'health',
          params: {},
          call: (_i1.Session session, Map<String, dynamic> params) async =>
              (endpoints['dashboardRuntime'] as _i4.DashboardRuntimeEndpoint)
                  .health(session),
        ),
        'topology': _i1.MethodConnector(
          name: 'topology',
          params: {},
          call: (_i1.Session session, Map<String, dynamic> params) async =>
              (endpoints['dashboardRuntime'] as _i4.DashboardRuntimeEndpoint)
                  .topology(session),
        ),
        'getProjectWorkroomSnapshot': _i1.MethodConnector(
          name: 'getProjectWorkroomSnapshot',
          params: {
            'projectId': _i1.ParameterDescription(
              name: 'projectId',
              type: _i1.getType<int>(),
              nullable: false,
            ),
          },
          call: (_i1.Session session, Map<String, dynamic> params) async =>
              (endpoints['dashboardRuntime'] as _i4.DashboardRuntimeEndpoint)
                  .getProjectWorkroomSnapshot(session, params['projectId']),
        ),
        'sendFixerTurn': _i1.MethodConnector(
          name: 'sendFixerTurn',
          params: {
            'projectId': _i1.ParameterDescription(
              name: 'projectId',
              type: _i1.getType<int>(),
              nullable: false,
            ),
            'threadId': _i1.ParameterDescription(
              name: 'threadId',
              type: _i1.getType<String>(),
              nullable: false,
            ),
            'content': _i1.ParameterDescription(
              name: 'content',
              type: _i1.getType<String>(),
              nullable: false,
            ),
            'idempotencyKey': _i1.ParameterDescription(
              name: 'idempotencyKey',
              type: _i1.getType<String>(),
              nullable: false,
            ),
          },
          call: (_i1.Session session, Map<String, dynamic> params) async =>
              (endpoints['dashboardRuntime'] as _i4.DashboardRuntimeEndpoint)
                  .sendFixerTurn(
                    session,
                    params['projectId'],
                    params['threadId'],
                    params['content'],
                    params['idempotencyKey'],
                  ),
        ),
        'requestGenuiSurface': _i1.MethodConnector(
          name: 'requestGenuiSurface',
          params: {
            'projectId': _i1.ParameterDescription(
              name: 'projectId',
              type: _i1.getType<int>(),
              nullable: false,
            ),
            'threadId': _i1.ParameterDescription(
              name: 'threadId',
              type: _i1.getType<String>(),
              nullable: false,
            ),
            'surfaceType': _i1.ParameterDescription(
              name: 'surfaceType',
              type: _i1.getType<String>(),
              nullable: false,
            ),
            'surfaceVersion': _i1.ParameterDescription(
              name: 'surfaceVersion',
              type: _i1.getType<int>(),
              nullable: false,
            ),
            'argumentsJson': _i1.ParameterDescription(
              name: 'argumentsJson',
              type: _i1.getType<String>(),
              nullable: false,
            ),
            'idempotencyKey': _i1.ParameterDescription(
              name: 'idempotencyKey',
              type: _i1.getType<String>(),
              nullable: false,
            ),
          },
          call: (_i1.Session session, Map<String, dynamic> params) async =>
              (endpoints['dashboardRuntime'] as _i4.DashboardRuntimeEndpoint)
                  .requestGenuiSurface(
                    session,
                    params['projectId'],
                    params['threadId'],
                    params['surfaceType'],
                    params['surfaceVersion'],
                    params['argumentsJson'],
                    params['idempotencyKey'],
                  ),
        ),
        'waitProjectUiEventsJson': _i1.MethodConnector(
          name: 'waitProjectUiEventsJson',
          params: {
            'projectId': _i1.ParameterDescription(
              name: 'projectId',
              type: _i1.getType<int>(),
              nullable: false,
            ),
            'afterSeq': _i1.ParameterDescription(
              name: 'afterSeq',
              type: _i1.getType<int>(),
              nullable: false,
            ),
            'protocolVersion': _i1.ParameterDescription(
              name: 'protocolVersion',
              type: _i1.getType<int>(),
              nullable: false,
            ),
          },
          call: (_i1.Session session, Map<String, dynamic> params) async =>
              (endpoints['dashboardRuntime'] as _i4.DashboardRuntimeEndpoint)
                  .waitProjectUiEventsJson(
                    session,
                    params['projectId'],
                    params['afterSeq'],
                    params['protocolVersion'],
                  ),
        ),
        'invokeGenuiAction': _i1.MethodConnector(
          name: 'invokeGenuiAction',
          params: {
            'request': _i1.ParameterDescription(
              name: 'request',
              type: _i1.getType<_i8.GenuiActionRequest>(),
              nullable: false,
            ),
          },
          call: (_i1.Session session, Map<String, dynamic> params) async =>
              (endpoints['dashboardRuntime'] as _i4.DashboardRuntimeEndpoint)
                  .invokeGenuiAction(session, params['request']),
        ),
        'submitHandsInstruction': _i1.MethodConnector(
          name: 'submitHandsInstruction',
          params: {
            'request': _i1.ParameterDescription(
              name: 'request',
              type: _i1.getType<_i9.HandsInstructionRequest>(),
              nullable: false,
            ),
          },
          call: (_i1.Session session, Map<String, dynamic> params) async =>
              (endpoints['dashboardRuntime'] as _i4.DashboardRuntimeEndpoint)
                  .submitHandsInstruction(session, params['request']),
        ),
        'selectHandsLane': _i1.MethodConnector(
          name: 'selectHandsLane',
          params: {
            'projectId': _i1.ParameterDescription(
              name: 'projectId',
              type: _i1.getType<int>(),
              nullable: false,
            ),
            'provider': _i1.ParameterDescription(
              name: 'provider',
              type: _i1.getType<String>(),
              nullable: false,
            ),
            'idempotencyKey': _i1.ParameterDescription(
              name: 'idempotencyKey',
              type: _i1.getType<String>(),
              nullable: false,
            ),
          },
          call: (_i1.Session session, Map<String, dynamic> params) async =>
              (endpoints['dashboardRuntime'] as _i4.DashboardRuntimeEndpoint)
                  .selectHandsLane(
                    session,
                    params['projectId'],
                    params['provider'],
                    params['idempotencyKey'],
                  ),
        ),
        'cancelHandsInstruction': _i1.MethodConnector(
          name: 'cancelHandsInstruction',
          params: {
            'projectId': _i1.ParameterDescription(
              name: 'projectId',
              type: _i1.getType<int>(),
              nullable: false,
            ),
            'instructionId': _i1.ParameterDescription(
              name: 'instructionId',
              type: _i1.getType<String>(),
              nullable: false,
            ),
            'idempotencyKey': _i1.ParameterDescription(
              name: 'idempotencyKey',
              type: _i1.getType<String>(),
              nullable: false,
            ),
          },
          call: (_i1.Session session, Map<String, dynamic> params) async =>
              (endpoints['dashboardRuntime'] as _i4.DashboardRuntimeEndpoint)
                  .cancelHandsInstruction(
                    session,
                    params['projectId'],
                    params['instructionId'],
                    params['idempotencyKey'],
                  ),
        ),
        'homeSnapshot': _i1.MethodConnector(
          name: 'homeSnapshot',
          params: {},
          call: (_i1.Session session, Map<String, dynamic> params) async =>
              (endpoints['dashboardRuntime'] as _i4.DashboardRuntimeEndpoint)
                  .homeSnapshot(session),
        ),
        'projectSnapshot': _i1.MethodConnector(
          name: 'projectSnapshot',
          params: {
            'projectId': _i1.ParameterDescription(
              name: 'projectId',
              type: _i1.getType<int>(),
              nullable: false,
            ),
          },
          call: (_i1.Session session, Map<String, dynamic> params) async =>
              (endpoints['dashboardRuntime'] as _i4.DashboardRuntimeEndpoint)
                  .projectSnapshot(session, params['projectId']),
        ),
        'projectDocs': _i1.MethodConnector(
          name: 'projectDocs',
          params: {
            'projectId': _i1.ParameterDescription(
              name: 'projectId',
              type: _i1.getType<int>(),
              nullable: false,
            ),
          },
          call: (_i1.Session session, Map<String, dynamic> params) async =>
              (endpoints['dashboardRuntime'] as _i4.DashboardRuntimeEndpoint)
                  .projectDocs(session, params['projectId']),
        ),
        'threadBinding': _i1.MethodConnector(
          name: 'threadBinding',
          params: {
            'projectId': _i1.ParameterDescription(
              name: 'projectId',
              type: _i1.getType<int>(),
              nullable: false,
            ),
          },
          call: (_i1.Session session, Map<String, dynamic> params) async =>
              (endpoints['dashboardRuntime'] as _i4.DashboardRuntimeEndpoint)
                  .threadBinding(session, params['projectId']),
        ),
        'sessionDetail': _i1.MethodConnector(
          name: 'sessionDetail',
          params: {
            'sessionId': _i1.ParameterDescription(
              name: 'sessionId',
              type: _i1.getType<int>(),
              nullable: false,
            ),
          },
          call: (_i1.Session session, Map<String, dynamic> params) async =>
              (endpoints['dashboardRuntime'] as _i4.DashboardRuntimeEndpoint)
                  .sessionDetail(session, params['sessionId']),
        ),
        'threadMessages': _i1.MethodConnector(
          name: 'threadMessages',
          params: {
            'threadId': _i1.ParameterDescription(
              name: 'threadId',
              type: _i1.getType<String>(),
              nullable: false,
            ),
          },
          call: (_i1.Session session, Map<String, dynamic> params) async =>
              (endpoints['dashboardRuntime'] as _i4.DashboardRuntimeEndpoint)
                  .threadMessages(session, params['threadId']),
        ),
        'sendThreadMessage': _i1.MethodConnector(
          name: 'sendThreadMessage',
          params: {
            'threadId': _i1.ParameterDescription(
              name: 'threadId',
              type: _i1.getType<String>(),
              nullable: false,
            ),
            'prompt': _i1.ParameterDescription(
              name: 'prompt',
              type: _i1.getType<String>(),
              nullable: false,
            ),
            'model': _i1.ParameterDescription(
              name: 'model',
              type: _i1.getType<String>(),
              nullable: false,
            ),
            'reasoning': _i1.ParameterDescription(
              name: 'reasoning',
              type: _i1.getType<String>(),
              nullable: false,
            ),
          },
          call: (_i1.Session session, Map<String, dynamic> params) async =>
              (endpoints['dashboardRuntime'] as _i4.DashboardRuntimeEndpoint)
                  .sendThreadMessage(
                    session,
                    params['threadId'],
                    params['prompt'],
                    params['model'],
                    params['reasoning'],
                  ),
        ),
        'threadTurnStatus': _i1.MethodConnector(
          name: 'threadTurnStatus',
          params: {
            'streamId': _i1.ParameterDescription(
              name: 'streamId',
              type: _i1.getType<String>(),
              nullable: false,
            ),
          },
          call: (_i1.Session session, Map<String, dynamic> params) async =>
              (endpoints['dashboardRuntime'] as _i4.DashboardRuntimeEndpoint)
                  .threadTurnStatus(session, params['streamId']),
        ),
        'watchProjectUi': _i1.MethodStreamConnector(
          name: 'watchProjectUi',
          params: {
            'projectId': _i1.ParameterDescription(
              name: 'projectId',
              type: _i1.getType<int>(),
              nullable: false,
            ),
            'afterSeq': _i1.ParameterDescription(
              name: 'afterSeq',
              type: _i1.getType<int>(),
              nullable: false,
            ),
            'protocolVersion': _i1.ParameterDescription(
              name: 'protocolVersion',
              type: _i1.getType<int>(),
              nullable: false,
            ),
          },
          streamParams: {},
          returnType: _i1.MethodStreamReturnType.streamType,
          call:
              (
                _i1.Session session,
                Map<String, dynamic> params,
                Map<String, Stream> streamParams,
              ) =>
                  (endpoints['dashboardRuntime']
                          as _i4.DashboardRuntimeEndpoint)
                      .watchProjectUi(
                        session,
                        params['projectId'],
                        params['afterSeq'],
                        params['protocolVersion'],
                      ),
        ),
      },
    );
    connectors['clientOrder'] = _i1.EndpointConnector(
      name: 'clientOrder',
      endpoint: endpoints['clientOrder']!,
      methodConnectors: {
        'createOrder': _i1.MethodConnector(
          name: 'createOrder',
          params: {
            'title': _i1.ParameterDescription(
              name: 'title',
              type: _i1.getType<String>(),
              nullable: false,
            ),
            'description': _i1.ParameterDescription(
              name: 'description',
              type: _i1.getType<String>(),
              nullable: false,
            ),
          },
          call: (_i1.Session session, Map<String, dynamic> params) async =>
              (endpoints['clientOrder'] as _i5.ClientOrderEndpoint).createOrder(
                session,
                title: params['title'],
                description: params['description'],
              ),
        ),
        'listOrders': _i1.MethodConnector(
          name: 'listOrders',
          params: {},
          call: (_i1.Session session, Map<String, dynamic> params) async =>
              (endpoints['clientOrder'] as _i5.ClientOrderEndpoint).listOrders(
                session,
              ),
        ),
        'getOrder': _i1.MethodConnector(
          name: 'getOrder',
          params: {
            'orderId': _i1.ParameterDescription(
              name: 'orderId',
              type: _i1.getType<int>(),
              nullable: false,
            ),
          },
          call: (_i1.Session session, Map<String, dynamic> params) async =>
              (endpoints['clientOrder'] as _i5.ClientOrderEndpoint).getOrder(
                session,
                params['orderId'],
              ),
        ),
        'updateOrder': _i1.MethodConnector(
          name: 'updateOrder',
          params: {
            'orderId': _i1.ParameterDescription(
              name: 'orderId',
              type: _i1.getType<int>(),
              nullable: false,
            ),
            'title': _i1.ParameterDescription(
              name: 'title',
              type: _i1.getType<String>(),
              nullable: false,
            ),
            'description': _i1.ParameterDescription(
              name: 'description',
              type: _i1.getType<String>(),
              nullable: false,
            ),
          },
          call: (_i1.Session session, Map<String, dynamic> params) async =>
              (endpoints['clientOrder'] as _i5.ClientOrderEndpoint).updateOrder(
                session,
                orderId: params['orderId'],
                title: params['title'],
                description: params['description'],
              ),
        ),
        'deleteOrder': _i1.MethodConnector(
          name: 'deleteOrder',
          params: {
            'orderId': _i1.ParameterDescription(
              name: 'orderId',
              type: _i1.getType<int>(),
              nullable: false,
            ),
          },
          call: (_i1.Session session, Map<String, dynamic> params) async =>
              (endpoints['clientOrder'] as _i5.ClientOrderEndpoint).deleteOrder(
                session,
                params['orderId'],
              ),
        ),
        'createRevision': _i1.MethodConnector(
          name: 'createRevision',
          params: {
            'orderId': _i1.ParameterDescription(
              name: 'orderId',
              type: _i1.getType<int>(),
              nullable: false,
            ),
            'description': _i1.ParameterDescription(
              name: 'description',
              type: _i1.getType<String>(),
              nullable: false,
            ),
          },
          call: (_i1.Session session, Map<String, dynamic> params) async =>
              (endpoints['clientOrder'] as _i5.ClientOrderEndpoint)
                  .createRevision(
                    session,
                    orderId: params['orderId'],
                    description: params['description'],
                  ),
        ),
        'listRevisions': _i1.MethodConnector(
          name: 'listRevisions',
          params: {
            'orderId': _i1.ParameterDescription(
              name: 'orderId',
              type: _i1.getType<int>(),
              nullable: false,
            ),
          },
          call: (_i1.Session session, Map<String, dynamic> params) async =>
              (endpoints['clientOrder'] as _i5.ClientOrderEndpoint)
                  .listRevisions(session, params['orderId']),
        ),
        'getRevision': _i1.MethodConnector(
          name: 'getRevision',
          params: {
            'revisionId': _i1.ParameterDescription(
              name: 'revisionId',
              type: _i1.getType<int>(),
              nullable: false,
            ),
          },
          call: (_i1.Session session, Map<String, dynamic> params) async =>
              (endpoints['clientOrder'] as _i5.ClientOrderEndpoint).getRevision(
                session,
                params['revisionId'],
              ),
        ),
        'updateRevision': _i1.MethodConnector(
          name: 'updateRevision',
          params: {
            'revisionId': _i1.ParameterDescription(
              name: 'revisionId',
              type: _i1.getType<int>(),
              nullable: false,
            ),
            'description': _i1.ParameterDescription(
              name: 'description',
              type: _i1.getType<String>(),
              nullable: false,
            ),
          },
          call: (_i1.Session session, Map<String, dynamic> params) async =>
              (endpoints['clientOrder'] as _i5.ClientOrderEndpoint)
                  .updateRevision(
                    session,
                    revisionId: params['revisionId'],
                    description: params['description'],
                  ),
        ),
        'deleteRevision': _i1.MethodConnector(
          name: 'deleteRevision',
          params: {
            'revisionId': _i1.ParameterDescription(
              name: 'revisionId',
              type: _i1.getType<int>(),
              nullable: false,
            ),
          },
          call: (_i1.Session session, Map<String, dynamic> params) async =>
              (endpoints['clientOrder'] as _i5.ClientOrderEndpoint)
                  .deleteRevision(session, params['revisionId']),
        ),
      },
    );
    connectors['orderDelivery'] = _i1.EndpointConnector(
      name: 'orderDelivery',
      endpoint: endpoints['orderDelivery']!,
      methodConnectors: {
        'mergeOrder': _i1.MethodConnector(
          name: 'mergeOrder',
          params: {
            'orderId': _i1.ParameterDescription(
              name: 'orderId',
              type: _i1.getType<int>(),
              nullable: false,
            ),
            'resultSummary': _i1.ParameterDescription(
              name: 'resultSummary',
              type: _i1.getType<String>(),
              nullable: false,
            ),
          },
          call: (_i1.Session session, Map<String, dynamic> params) async =>
              (endpoints['orderDelivery'] as _i6.OrderDeliveryEndpoint)
                  .mergeOrder(
                    session,
                    params['orderId'],
                    resultSummary: params['resultSummary'],
                  ),
        ),
        'approveOrder': _i1.MethodConnector(
          name: 'approveOrder',
          params: {
            'orderId': _i1.ParameterDescription(
              name: 'orderId',
              type: _i1.getType<int>(),
              nullable: false,
            ),
            'resultSummary': _i1.ParameterDescription(
              name: 'resultSummary',
              type: _i1.getType<String>(),
              nullable: false,
            ),
          },
          call: (_i1.Session session, Map<String, dynamic> params) async =>
              (endpoints['orderDelivery'] as _i6.OrderDeliveryEndpoint)
                  .approveOrder(
                    session,
                    params['orderId'],
                    resultSummary: params['resultSummary'],
                  ),
        ),
        'rejectOrder': _i1.MethodConnector(
          name: 'rejectOrder',
          params: {
            'orderId': _i1.ParameterDescription(
              name: 'orderId',
              type: _i1.getType<int>(),
              nullable: false,
            ),
          },
          call: (_i1.Session session, Map<String, dynamic> params) async =>
              (endpoints['orderDelivery'] as _i6.OrderDeliveryEndpoint)
                  .rejectOrder(session, params['orderId']),
        ),
      },
    );
    connectors['order'] = _i1.EndpointConnector(
      name: 'order',
      endpoint: endpoints['order']!,
      methodConnectors: {
        'createOrder': _i1.MethodConnector(
          name: 'createOrder',
          params: {
            'clientId': _i1.ParameterDescription(
              name: 'clientId',
              type: _i1.getType<String>(),
              nullable: false,
            ),
            'projectDescription': _i1.ParameterDescription(
              name: 'projectDescription',
              type: _i1.getType<String>(),
              nullable: false,
            ),
            'budgetCents': _i1.ParameterDescription(
              name: 'budgetCents',
              type: _i1.getType<int>(),
              nullable: false,
            ),
          },
          call: (_i1.Session session, Map<String, dynamic> params) async =>
              (endpoints['order'] as _i7.OrderEndpoint).createOrder(
                session,
                clientId: params['clientId'],
                projectDescription: params['projectDescription'],
                budgetCents: params['budgetCents'],
              ),
        ),
        'listOrders': _i1.MethodConnector(
          name: 'listOrders',
          params: {
            'clientId': _i1.ParameterDescription(
              name: 'clientId',
              type: _i1.getType<String>(),
              nullable: false,
            ),
          },
          call: (_i1.Session session, Map<String, dynamic> params) async =>
              (endpoints['order'] as _i7.OrderEndpoint).listOrders(
                session,
                params['clientId'],
              ),
        ),
        'submitRevision': _i1.MethodConnector(
          name: 'submitRevision',
          params: {
            'orderId': _i1.ParameterDescription(
              name: 'orderId',
              type: _i1.getType<int>(),
              nullable: false,
            ),
            'revisionText': _i1.ParameterDescription(
              name: 'revisionText',
              type: _i1.getType<String>(),
              nullable: false,
            ),
            'attachmentUrls': _i1.ParameterDescription(
              name: 'attachmentUrls',
              type: _i1.getType<List<String>?>(),
              nullable: true,
            ),
          },
          call: (_i1.Session session, Map<String, dynamic> params) async =>
              (endpoints['order'] as _i7.OrderEndpoint).submitRevision(
                session,
                orderId: params['orderId'],
                revisionText: params['revisionText'],
                attachmentUrls: params['attachmentUrls'],
              ),
        ),
        'orderStatus': _i1.MethodConnector(
          name: 'orderStatus',
          params: {
            'orderId': _i1.ParameterDescription(
              name: 'orderId',
              type: _i1.getType<int>(),
              nullable: false,
            ),
          },
          call: (_i1.Session session, Map<String, dynamic> params) async =>
              (endpoints['order'] as _i7.OrderEndpoint).orderStatus(
                session,
                params['orderId'],
              ),
        ),
      },
    );
    modules['serverpod_auth_core'] = _i10.Endpoints()
      ..initializeEndpoints(server);
  }

  @override
  _i1.FutureCallDispatch? get futureCalls {
    return _i11.FutureCalls();
  }
}
