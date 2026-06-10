import 'package:flutter/material.dart';

class FixerAppTheme {
  static const _seedColor = Color(0xFF2A6CF0);
  static const _background = Color(0xFFF6F7FB);
  static const _surface = Color(0xFFFFFFFF);
  static const _surfaceMuted = Color(0xFFEFF3FA);
  static const _outline = Color(0xFFD9E0EC);

  static ThemeData light() {
    final scheme = ColorScheme.fromSeed(
      seedColor: _seedColor,
      brightness: Brightness.light,
    );
    return ThemeData(
      useMaterial3: true,
      colorScheme: scheme,
      scaffoldBackgroundColor: _background,
      canvasColor: _background,
      appBarTheme: AppBarTheme(
        backgroundColor: _surface,
        surfaceTintColor: _surface,
        foregroundColor: scheme.onSurface,
        elevation: 0,
        shape: const Border(bottom: BorderSide(color: _outline)),
        titleTextStyle: const TextStyle(
          color: Color(0xFF111827),
          fontSize: 18,
          fontWeight: FontWeight.w800,
        ),
      ),
      cardTheme: CardThemeData(
        color: _surface,
        surfaceTintColor: _surface,
        elevation: 0,
        margin: EdgeInsets.zero,
        shape: RoundedRectangleBorder(
          side: const BorderSide(color: _outline),
          borderRadius: BorderRadius.circular(8),
        ),
      ),
      dividerTheme: const DividerThemeData(
        color: _outline,
        space: 1,
        thickness: 1,
      ),
      inputDecorationTheme: InputDecorationTheme(
        filled: true,
        fillColor: _surfaceMuted,
        contentPadding: const EdgeInsets.symmetric(
          horizontal: 12,
          vertical: 10,
        ),
        border: OutlineInputBorder(
          borderRadius: BorderRadius.circular(8),
          borderSide: const BorderSide(color: _outline),
        ),
        enabledBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(8),
          borderSide: const BorderSide(color: _outline),
        ),
      ),
      listTileTheme: const ListTileThemeData(
        contentPadding: EdgeInsets.symmetric(horizontal: 12, vertical: 4),
      ),
      snackBarTheme: const SnackBarThemeData(
        behavior: SnackBarBehavior.floating,
      ),
      tabBarTheme: TabBarThemeData(
        dividerColor: _outline,
        indicatorSize: TabBarIndicatorSize.label,
        labelColor: scheme.primary,
        unselectedLabelColor: scheme.onSurfaceVariant,
        labelStyle: const TextStyle(fontWeight: FontWeight.w800),
        unselectedLabelStyle: const TextStyle(fontWeight: FontWeight.w600),
      ),
    );
  }
}
