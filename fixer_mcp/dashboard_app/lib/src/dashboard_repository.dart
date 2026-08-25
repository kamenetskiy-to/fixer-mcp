import 'dart:io';

import 'dashboard_models.dart';
import 'dashboard_runtime_client.dart';
import 'client_order_repository.dart';
import 'hub/fixer_chat/fixer_chat_models.dart';
import 'hub/fixer_chat/fixer_chat_service.dart';

abstract class DashboardRepository {
  Future<HomeSnapshot> loadHomeSnapshot();
  Future<ProjectWorkspaceSnapshot> loadProjectWorkspace(int projectId);
  Future<FixerChatBindingRecord> loadFixerChatBinding(int projectId);
  Future<FixerChatBindingRecord> loadOverseerChatBinding(int projectId);
  Future<NetrunnerDetailSnapshot> loadNetrunnerDetail(int sessionId);
  Future<ThreadMessagesSnapshot> loadThreadMessages(String threadId);
  Future<ThreadSendResult> sendThreadMessage(String threadId, String prompt);
  Future<ThreadSendResult> sendThreadMessageWithConfig(
    String threadId,
    String prompt, {
    required String model,
    required String reasoning,
  });
  Future<ThreadTurnStatusSnapshot> loadThreadTurnStatus(String streamId);
  Future<ProjectWorkspaceSnapshot> createTask(
    int projectId, {
    required String taskDescription,
    List<String> declaredWriteScope = const <String>[],
  });
  Future<NetrunnerDetailSnapshot> setSessionAttachedDocs(
    int sessionId,
    List<int> projectDocIds,
  );
  Future<NetrunnerDetailSnapshot> setSessionMcpServers(
    int sessionId,
    List<String> mcpServerNames,
  );
  Future<NetrunnerDetailSnapshot> setSessionStatus(
    int sessionId,
    String status,
  );
  Future<NetrunnerDetailSnapshot> setProposalStatus(
    int proposalId,
    String status,
  );
}

class BridgeDashboardRepository implements DashboardRepository {
  BridgeDashboardRepository({
    this.baseUrl,
    this.serverpodBaseUrl,
    HttpClient? httpClient,
    DashboardRuntimeClient? runtimeClient,
    ClientSessionAuthProvider? authProvider,
  }) : _runtimeClient =
           runtimeClient ??
           DashboardRuntimeClient(
             dashboardBaseUrl: baseUrl,
             serverpodBaseUrl: serverpodBaseUrl,
             httpClient: httpClient,
           ),
       _authProvider =
           authProvider ??
           ClientSessionAuthProvider(
             sessionStore: SharedPreferencesClientSessionStore(),
           ) {
    _runtimeClient.authHeaderProvider ??= () => _authProvider.authHeaderValue;
  }

  final String? baseUrl;
  final String? serverpodBaseUrl;
  final DashboardRuntimeClient _runtimeClient;
  final ClientSessionAuthProvider _authProvider;

  @override
  Future<HomeSnapshot> loadHomeSnapshot() async {
    final payload = await _runtimeClient.readDashboardJson('/api/home');
    return HomeSnapshot.fromJson(payload);
  }

  @override
  Future<ProjectWorkspaceSnapshot> loadProjectWorkspace(int projectId) async {
    final responses = await Future.wait([
      _runtimeClient.readDashboardJson('/api/projects/$projectId/overview'),
      _runtimeClient.readDashboardJson('/api/projects/$projectId/docs'),
      _runtimeClient.readDashboardJson('/api/projects/$projectId/docs/tree'),
    ]);
    return ProjectWorkspaceSnapshot.fromJson(
      responses[0],
      responses[1],
      <String, dynamic>{},
      responses[2],
    );
  }

  @override
  Future<FixerChatBindingRecord> loadFixerChatBinding(int projectId) async {
    final payload = await _runtimeClient.readDashboardJson(
      '/api/projects/$projectId/fixer-chat-binding',
    );
    return FixerChatBindingRecord.fromJson(payload);
  }

  @override
  Future<FixerChatBindingRecord> loadOverseerChatBinding(int projectId) async {
    final payload = await _runtimeClient.readDashboardJson(
      '/api/projects/$projectId/overseer-chat-binding',
    );
    return FixerChatBindingRecord.fromJson(payload);
  }

  @override
  Future<NetrunnerDetailSnapshot> loadNetrunnerDetail(int sessionId) async {
    final payload = await _runtimeClient.readDashboardJson(
      '/api/sessions/$sessionId',
    );
    return NetrunnerDetailSnapshot.fromJson(payload);
  }

  @override
  Future<ThreadMessagesSnapshot> loadThreadMessages(String threadId) async {
    final payload = await _runtimeClient.callServerpodEndpoint(
      'dashboardRuntime',
      'threadMessages',
      {'threadId': threadId},
    );
    return ThreadMessagesSnapshot.fromJson(payload);
  }

  @override
  Future<ThreadSendResult> sendThreadMessage(
    String threadId,
    String prompt,
  ) async {
    final payload = await _runtimeClient.callServerpodEndpoint(
      'dashboardRuntime',
      'sendThreadMessage',
      {'threadId': threadId, 'prompt': prompt},
    );
    return ThreadSendResult.fromJson(payload);
  }

  @override
  Future<ThreadSendResult> sendThreadMessageWithConfig(
    String threadId,
    String prompt, {
    required String model,
    required String reasoning,
  }) async {
    final payload = await _runtimeClient.callServerpodEndpoint(
      'dashboardRuntime',
      'sendThreadMessage',
      {
        'threadId': threadId,
        'prompt': prompt,
        'model': model,
        'reasoning': reasoning,
      },
    );
    return ThreadSendResult.fromJson(payload);
  }

  @override
  Future<ThreadTurnStatusSnapshot> loadThreadTurnStatus(String streamId) async {
    final payload = await _runtimeClient.callServerpodEndpoint(
      'dashboardRuntime',
      'threadTurnStatus',
      {'streamId': streamId},
    );
    return ThreadTurnStatusSnapshot.fromJson(payload);
  }

  @override
  Future<ProjectWorkspaceSnapshot> createTask(
    int projectId, {
    required String taskDescription,
    List<String> declaredWriteScope = const <String>[],
  }) async {
    await _runtimeClient
        .postDashboardJson('/api/actions/projects/$projectId/tasks', {
          'task_description': taskDescription,
          'declared_write_scope': declaredWriteScope,
        });
    return loadProjectWorkspace(projectId);
  }

  @override
  Future<NetrunnerDetailSnapshot> setSessionAttachedDocs(
    int sessionId,
    List<int> projectDocIds,
  ) async {
    final payload = await _runtimeClient.postDashboardJson(
      '/api/actions/sessions/$sessionId/attached-docs',
      {'project_doc_ids': projectDocIds},
    );
    return NetrunnerDetailSnapshot.fromJson(_asActionSessionPayload(payload));
  }

  @override
  Future<NetrunnerDetailSnapshot> setSessionMcpServers(
    int sessionId,
    List<String> mcpServerNames,
  ) async {
    final payload = await _runtimeClient.postDashboardJson(
      '/api/actions/sessions/$sessionId/mcp-servers',
      {'mcp_server_names': mcpServerNames},
    );
    return NetrunnerDetailSnapshot.fromJson(_asActionSessionPayload(payload));
  }

  @override
  Future<NetrunnerDetailSnapshot> setSessionStatus(
    int sessionId,
    String status,
  ) async {
    final payload = await _runtimeClient.postDashboardJson(
      '/api/actions/sessions/$sessionId/status',
      {'status': status},
    );
    return NetrunnerDetailSnapshot.fromJson(_asActionSessionPayload(payload));
  }

  @override
  Future<NetrunnerDetailSnapshot> setProposalStatus(
    int proposalId,
    String status,
  ) async {
    final payload = await _runtimeClient.postDashboardJson(
      '/api/actions/proposals/$proposalId/status',
      {'status': status},
    );
    return NetrunnerDetailSnapshot.fromJson(_asActionSessionPayload(payload));
  }

  Map<String, dynamic> _asActionSessionPayload(Map<String, dynamic> payload) {
    final sessionPayload = payload['session'];
    if (sessionPayload is Map<String, dynamic>) {
      return sessionPayload;
    }
    if (sessionPayload is Map) {
      return Map<String, dynamic>.from(sessionPayload);
    }
    throw StateError('Unexpected action session payload');
  }
}

class BridgeFixerChatService implements FixerChatService, HandsChatService {
  BridgeFixerChatService({DashboardRuntimeClient? runtimeClient})
    : _runtimeClient = runtimeClient ?? DashboardRuntimeClient();

  final DashboardRuntimeClient _runtimeClient;

  @override
  Future<List<FixerThreadRecord>> loadFixerThreads(int projectId) async {
    final payload = await _runtimeClient.readDashboardJson(
      '/api/projects/$projectId/fixer-threads',
    );
    final rawThreads = payload['threads'];
    if (rawThreads is! List) return const <FixerThreadRecord>[];
    return rawThreads
        .whereType<Map>()
        .map(
          (item) => FixerThreadRecord.fromJson(Map<String, dynamic>.from(item)),
        )
        .toList(growable: false);
  }

  @override
  Future<List<FixerThreadRecord>> loadHandsThreads(int projectId) async {
    final payload = await _runtimeClient.readDashboardJson(
      '/api/projects/$projectId/hands-threads',
    );
    final rawThreads = payload['threads'];
    if (rawThreads is! List) return const <FixerThreadRecord>[];
    return rawThreads
        .whereType<Map>()
        .map(
          (item) => FixerThreadRecord.fromJson(Map<String, dynamic>.from(item)),
        )
        .toList(growable: false);
  }

  @override
  Future<void> createFixerChat(
    int projectId,
    FixerChatLaunchRequest request,
  ) async {
    await _runtimeClient.postDashboardJson(
      '/api/actions/projects/$projectId/fixer-chats',
      request.toJson(),
    );
  }
}
