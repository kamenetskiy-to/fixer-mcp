import 'dart:io';

import 'dashboard_models.dart';
import 'dashboard_runtime_client.dart';

abstract class DashboardRepository {
  Future<HomeSnapshot> loadHomeSnapshot();
  Future<ProjectWorkspaceSnapshot> loadProjectWorkspace(int projectId);
  Future<FixerChatBindingRecord> loadFixerChatBinding(int projectId);
  Future<FixerChatBindingRecord> loadOverseerChatBinding(int projectId);
  Future<NetrunnerDetailSnapshot> loadNetrunnerDetail(int sessionId);
  Future<ThreadMessagesSnapshot> loadThreadMessages(String threadId);
  Future<ThreadSendResult> sendThreadMessage(String threadId, String prompt);
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
  }) : _runtimeClient =
           runtimeClient ??
           DashboardRuntimeClient(
             dashboardBaseUrl: baseUrl,
             serverpodBaseUrl: serverpodBaseUrl,
             httpClient: httpClient,
           );

  final String? baseUrl;
  final String? serverpodBaseUrl;
  final DashboardRuntimeClient _runtimeClient;

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
    ]);
    return ProjectWorkspaceSnapshot.fromJson(
      responses[0],
      responses[1],
      <String, dynamic>{},
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
