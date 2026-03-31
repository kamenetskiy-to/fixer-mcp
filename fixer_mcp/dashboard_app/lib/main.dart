import 'package:flutter/material.dart';

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
      title: 'Fixer MCP Dashboard',
      theme: ThemeData(
        useMaterial3: true,
        colorScheme: ColorScheme.fromSeed(
          seedColor: const Color(0xFF2B6E68),
          brightness: Brightness.light,
        ),
        scaffoldBackgroundColor: const Color(0xFFF5F2EB),
        cardTheme: CardThemeData(
          color: Colors.white,
          surfaceTintColor: Colors.white,
          elevation: 0,
          shape: RoundedRectangleBorder(
            borderRadius: BorderRadius.circular(20),
          ),
        ),
      ),
      home: DashboardShell(
        repository: repository ?? SqliteFixerDashboardRepository(),
      ),
    );
  }
}
