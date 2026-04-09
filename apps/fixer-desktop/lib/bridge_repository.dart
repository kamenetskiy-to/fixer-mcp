import 'dart:convert';
import 'dart:io';

import 'models.dart';

abstract class DesktopBridgeRepository {
  Future<List<ProjectSummary>> fetchProjects();

  Future<ProjectDashboard> fetchDashboard(int projectId);

  Future<SessionDetail> fetchSession(int sessionId);
}

class HttpDesktopBridgeRepository implements DesktopBridgeRepository {
  HttpDesktopBridgeRepository({required this.baseUri});

  final Uri baseUri;
  final HttpClient _client = HttpClient();

  @override
  Future<List<ProjectSummary>> fetchProjects() async {
    final payload = await _getJson('/api/projects');
    final items = (payload['projects'] as List<dynamic>? ?? const []);
    return items
        .map(
          (dynamic item) =>
              ProjectSummary.fromJson(item as Map<String, dynamic>),
        )
        .toList();
  }

  @override
  Future<ProjectDashboard> fetchDashboard(int projectId) async {
    final payload = await _getJson('/api/projects/$projectId/dashboard');
    return ProjectDashboard.fromJson(payload);
  }

  @override
  Future<SessionDetail> fetchSession(int sessionId) async {
    final payload = await _getJson('/api/sessions/$sessionId');
    return SessionDetail.fromJson(payload);
  }

  Future<Map<String, dynamic>> _getJson(String path) async {
    final request = await _client.getUrl(baseUri.resolve(path));
    final response = await request.close();
    final body = await utf8.decoder.bind(response).join();
    final decoded = jsonDecode(body);
    if (response.statusCode >= 400) {
      final error = decoded is Map<String, dynamic>
          ? decoded['error']?.toString() ?? body
          : body;
      throw HttpException('Bridge request failed: $error');
    }
    return decoded as Map<String, dynamic>;
  }
}
