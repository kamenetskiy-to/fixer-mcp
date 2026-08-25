import 'fixer_chat_models.dart';

abstract class FixerChatService {
  Future<List<FixerThreadRecord>> loadFixerThreads(int projectId);

  Future<void> createFixerChat(int projectId, FixerChatLaunchRequest request);
}

abstract class HandsChatService {
  Future<List<FixerThreadRecord>> loadHandsThreads(int projectId);
}
