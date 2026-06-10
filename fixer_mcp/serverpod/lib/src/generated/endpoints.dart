/* AUTOMATICALLY GENERATED CODE DO NOT MODIFY */
/*   To regenerate run: "serverpod generate"   */

// ignore_for_file: implementation_imports
// ignore_for_file: library_private_types_in_public_api
// ignore_for_file: non_constant_identifier_names
// ignore_for_file: public_member_api_docs
// ignore_for_file: type_literal_in_constant_pattern
// ignore_for_file: use_super_parameters
// ignore_for_file: invalid_use_of_internal_member

import 'package:serverpod/serverpod.dart' as _i1;

import '../endpoints/dashboard_runtime_endpoint.dart' as _i2;

class Endpoints extends _i1.EndpointDispatch {
  @override
  void initializeEndpoints(_i1.Server server) {
    final endpoints = <String, _i1.Endpoint>{
      'dashboardRuntime': _i2.DashboardRuntimeEndpoint()
        ..initialize(server, 'dashboardRuntime', null),
    };

    connectors['dashboardRuntime'] = _i1.EndpointConnector(
      name: 'dashboardRuntime',
      endpoint: endpoints['dashboardRuntime']!,
      methodConnectors: {
        'health': _i1.MethodConnector(
          name: 'health',
          params: {},
          call: (_i1.Session session, Map<String, dynamic> params) async =>
              (endpoints['dashboardRuntime'] as _i2.DashboardRuntimeEndpoint)
                  .health(session),
        ),
        'topology': _i1.MethodConnector(
          name: 'topology',
          params: {},
          call: (_i1.Session session, Map<String, dynamic> params) async =>
              (endpoints['dashboardRuntime'] as _i2.DashboardRuntimeEndpoint)
                  .topology(session),
        ),
        'homeSnapshot': _i1.MethodConnector(
          name: 'homeSnapshot',
          params: {},
          call: (_i1.Session session, Map<String, dynamic> params) async =>
              (endpoints['dashboardRuntime'] as _i2.DashboardRuntimeEndpoint)
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
              (endpoints['dashboardRuntime'] as _i2.DashboardRuntimeEndpoint)
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
              (endpoints['dashboardRuntime'] as _i2.DashboardRuntimeEndpoint)
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
              (endpoints['dashboardRuntime'] as _i2.DashboardRuntimeEndpoint)
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
              (endpoints['dashboardRuntime'] as _i2.DashboardRuntimeEndpoint)
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
              (endpoints['dashboardRuntime'] as _i2.DashboardRuntimeEndpoint)
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
          },
          call: (_i1.Session session, Map<String, dynamic> params) async =>
              (endpoints['dashboardRuntime'] as _i2.DashboardRuntimeEndpoint)
                  .sendThreadMessage(
                    session,
                    params['threadId'],
                    params['prompt'],
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
              (endpoints['dashboardRuntime'] as _i2.DashboardRuntimeEndpoint)
                  .threadTurnStatus(session, params['streamId']),
        ),
      },
    );
  }
}
