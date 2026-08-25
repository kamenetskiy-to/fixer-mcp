import 'package:fixer_dashboard_app/src/app_localizations.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  test('supports English and Russian and falls back to English', () async {
    const delegate = AppLocalizationsDelegate();

    expect(delegate.isSupported(const Locale('en')), isTrue);
    expect(delegate.isSupported(const Locale('ru')), isTrue);
    expect(delegate.isSupported(const Locale('de')), isFalse);

    expect(AppLocalizations(const Locale('en')).signIn, 'Sign in');
    expect(AppLocalizations(const Locale('ru')).signIn, 'Войти');
    expect(
      AppLocalizations(const Locale('ru')).architectCockpit,
      'Кабинет архитектора',
    );
    expect(AppLocalizations(const Locale('ru')).projects, 'Codex Hub');
    expect(AppLocalizations(const Locale('ru')).workroomFixerTab, 'Фиксер');
    expect(AppLocalizations(const Locale('ru')).workroomHandsTab, 'Руки');
    expect(
      AppLocalizations(const Locale('ru')).surfaceLabel('execution.list.v1'),
      'Netrunner-сессии',
    );
    expect(AppLocalizations(const Locale('en')).workroomHandsTab, 'Hands');
    expect((await delegate.load(const Locale('de'))).signIn, 'Sign in');
  });

  test('restores and persists the selected locale', () async {
    SharedPreferences.setMockInitialValues({
      AppLocaleController.preferenceKey: 'ru',
    });
    final preferences = await SharedPreferences.getInstance();
    final restored = AppLocaleController(preferences: preferences);
    await restored.ready;

    expect(restored.locale, const Locale('ru'));

    await restored.setLocale(const Locale('en'));
    expect(preferences.getString(AppLocaleController.preferenceKey), 'en');

    await restored.useSystemLocale();
    expect(preferences.containsKey(AppLocaleController.preferenceKey), isFalse);
    restored.dispose();
  });
}
