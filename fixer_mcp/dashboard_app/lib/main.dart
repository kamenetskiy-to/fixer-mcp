import 'package:flutter/material.dart';

import 'src/app_theme.dart';
import 'src/dashboard_repository.dart';
import 'src/dashboard_view.dart';

void main() {
  WidgetsFlutterBinding.ensureInitialized();
  runApp(const FixerDashboardApp());
}

class FixerDashboardApp extends StatelessWidget {
  const FixerDashboardApp({super.key, this.repository});

  final DashboardRepository? repository;

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      debugShowCheckedModeBanner: false,
      title: 'Codex Hub',
      theme: FixerAppTheme.light(),
      home: DashboardShell(
        repository: repository ?? BridgeDashboardRepository(),
      ),
    );
  }
}
