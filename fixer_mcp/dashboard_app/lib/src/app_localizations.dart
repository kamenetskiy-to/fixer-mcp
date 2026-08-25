import 'package:flutter/material.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_localizations/flutter_localizations.dart';
import 'package:intl/intl.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// Supported languages for the dashboard. A null selected locale means that
/// Flutter should follow the operating system locale.
class AppLocalizations {
  AppLocalizations(this.locale);

  final Locale locale;

  bool get isRussian => locale.languageCode == 'ru';

  static const supportedLocales = [Locale('en'), Locale('ru')];

  static const localizationsDelegates = <LocalizationsDelegate<dynamic>>[
    AppLocalizationsDelegate(),
    GlobalMaterialLocalizations.delegate,
    GlobalCupertinoLocalizations.delegate,
    GlobalWidgetsLocalizations.delegate,
  ];

  static AppLocalizations of(BuildContext context) =>
      Localizations.of<AppLocalizations>(context, AppLocalizations) ??
      AppLocalizations(const Locale('en'));

  String get language => isRussian ? 'Язык' : 'Language';
  String get english => 'English';
  String get russian => 'Русский';
  String get productName => 'Fixer Studio';
  String get appTitle => productName;
  String get clientWorkspace =>
      isRussian ? 'Рабочее пространство клиента' : 'Client workspace';
  String get fixerWorkspace =>
      isRussian ? 'Рабочее пространство Fixer' : 'Fixer workspace';
  String get switchRole => isRussian ? 'Сменить роль' : 'Switch role';
  String get architect => isRussian ? 'Архитектор' : 'Architect';
  String get client => isRussian ? 'Клиент' : 'Client';
  String get currentRole => isRussian ? 'Текущая роль' : 'Current role';
  String role(String value) =>
      '${isRussian ? 'Роль' : 'Role'}: ${status(value)}';

  String get loginTitle => isRussian ? 'С возвращением' : 'Welcome back';
  String get loginSubtitle => isRussian
      ? 'Войдите, чтобы открыть рабочее пространство.'
      : 'Sign in to see your workspace.';
  String get loginIntroTitle => isRussian
      ? 'От идеи к\nследующему шагу.'
      : 'Turn a brief into\na clear next step.';
  String get loginIntroSubtitle => isRussian
      ? 'Рабочее пространство клиента для идей продукта,\nзаказов и всех итераций в одном месте.'
      : 'Your client workspace for sharing product ideas,\ntracking orders, and keeping every revision in view.';
  String get email => 'Email';
  String get emailHint => 'you@company.com';
  String get password => isRussian ? 'Пароль' : 'Password';
  String get validEmail =>
      isRussian ? 'Введите корректный email' : 'Enter a valid email';
  String get requiredPassword =>
      isRussian ? 'Введите пароль' : 'Enter your password';
  String get signIn => isRussian ? 'Войти' : 'Sign in';

  String greeting(String name) =>
      isRussian ? 'Доброе утро, $name' : 'Good morning, $name';
  String get latestWorkspace => isRussian
      ? 'Последние события в вашем рабочем пространстве.'
      : 'Here is the latest from your product workspace.';
  String get totalOrders => isRussian ? 'Всего заказов' : 'Total orders';
  String get inProgress => isRussian ? 'В работе' : 'In progress';
  String get draftBriefs => isRussian ? 'Черновики брифов' : 'Draft briefs';
  String get recentOrders => isRussian ? 'Последние заказы' : 'Recent orders';
  String get viewAll => isRussian ? 'Показать все' : 'View all';
  String get orders => isRussian ? 'Заказы' : 'Orders';
  String get productBriefs => isRussian
      ? 'Брифы продукта, которыми вы поделились с командой.'
      : 'Product briefs shared with your team.';
  String get noOrdersYet => isRussian ? 'Заказов пока нет' : 'No orders yet';
  String get noOrdersMessage => isRussian
      ? 'Начните с брифа продукта, а команда продолжит работу.'
      : 'Start with a product brief and your team will take it from there.';
  String get createFirstOrder =>
      isRussian ? 'Создать первый заказ' : 'Create your first order';
  String get account => isRussian ? 'Аккаунт' : 'Account';
  String signOut(String name) => isRussian ? 'Выйти: $name' : 'Sign out $name';
  String get tryAgain => isRussian ? 'Повторить' : 'Try again';

  String get newOrder => isRussian ? 'Новый заказ' : 'New order';
  String get createProductBrief =>
      isRussian ? 'Создайте бриф продукта' : 'Create a product brief';
  String get createBriefSubtitle => isRussian
      ? 'Дайте команде достаточно контекста, чтобы понять, что нужно создать.'
      : 'Give your team enough context to understand what you want to build.';
  String get productBrief => isRussian ? 'Бриф продукта' : 'Product brief';
  String get productBriefHint => isRussian
      ? 'Что нужно создать? Для кого? Как выглядит успех?'
      : 'What should be built? Who is it for? What does success look like?';
  String get addShortBrief =>
      isRussian ? 'Добавьте короткий бриф' : 'Add a short brief';
  String get budget => isRussian ? 'Бюджет' : 'Budget';
  String get budgetHint => isRussian ? 'например, 2500' : 'e.g. 2500';
  String get validBudget =>
      isRussian ? 'Введите корректный бюджет' : 'Enter a valid budget';
  String get saving => isRussian ? 'Сохранение…' : 'Saving…';
  String get createOrder => isRussian ? 'Создать заказ' : 'Create order';

  String get orderDetails => isRussian ? 'Детали заказа' : 'Order details';
  String formattedBudget(String value) =>
      isRussian ? 'Бюджет $value' : 'Budget $value';
  String get projectBrief => isRussian ? 'Бриф проекта' : 'Project brief';
  String get latestResult => isRussian
      ? 'Последний результат от вашего Архитектора'
      : 'Latest result from your Architect';
  String get noResult => isRussian
      ? 'Ваш Архитектор ещё не поделился итогом.'
      : 'Your Architect has not shared a result summary yet.';
  String get revisionHistory =>
      isRussian ? 'История итераций' : 'Revision history';
  String get noRevisions =>
      isRussian ? 'Итераций пока нет.' : 'No revisions submitted yet.';
  String get submitRevisionTitle =>
      isRussian ? 'Отправить итерацию' : 'Submit Revision';
  String get submitRevisionSubtitle => isRussian
      ? 'Расскажите Архитектору, что изменить в следующем проходе.'
      : 'Tell your Architect what should change in the next pass.';
  String get revisionDetails =>
      isRussian ? 'Детали итерации' : 'Revision details';
  String get revisionHint => isRussian
      ? 'Что нужно обновить или проверить?'
      : 'What should be updated or reviewed?';
  String get addRevisionDetails =>
      isRussian ? 'Добавьте детали итерации' : 'Add revision details';
  String get submitting => isRussian ? 'Отправка…' : 'Submitting…';
  String get submitRevision =>
      isRussian ? 'Отправить итерацию' : 'Submit revision';
  String get revisionSubmitted =>
      isRussian ? 'Итерация отправлена' : 'Revision submitted';
  String get unavailableOrderDetails => isRussian
      ? 'Детали заказа недоступны.'
      : 'Order details are unavailable.';

  String get architectCockpit =>
      isRussian ? 'Кабинет архитектора' : 'Architect cockpit';
  String get refreshWeeklyOrders =>
      isRussian ? 'Обновить заказы недели' : 'Refresh weekly orders';
  String weeklyConsolidation(int orders, int reviews) => isRussian
      ? '$orders веток · $reviews ожидают проверки'
      : '$orders branches · $reviews awaiting review';
  String buildStatus(String value) =>
      '${isRussian ? 'Сборка' : 'Build'} · ${status(value)}';
  String reviewStatus(String value) =>
      '${isRussian ? 'Проверка' : 'Review-netrunner'} · ${status(value)}';
  String get merge => isRussian ? 'Объединить' : 'Merge';
  String get reject => isRussian ? 'Отклонить' : 'Reject';
  String get codeMerged =>
      isRussian ? 'Код объединён с main.' : 'Code merged into main.';
  String get branchRejected => isRussian
      ? 'Ветка отклонена и отправлена на доработку.'
      : 'Branch rejected and sent back for rework.';
  String get docProposals =>
      isRussian ? 'Предложения по документации' : 'Doc proposals';
  String get docProposalsSubtitle => isRussian
      ? 'Одобряйте изменения документации в main независимо от кода.'
      : 'Approve documentation changes into main independently of code.';
  String get noDocProposals => isRussian
      ? 'Предложений по документации нет.'
      : 'No doc proposals recorded.';
  String get approve => isRussian ? 'Одобрить' : 'Approve';
  String get rejectDoc => isRussian ? 'Отклонить' : 'Reject';
  String get docApproved => isRussian
      ? 'Предложение по документации одобрено.'
      : 'Doc proposal approved into main.';
  String get docRejected => isRussian
      ? 'Предложение по документации отклонено.'
      : 'Doc proposal rejected.';
  String get selectBranch => isRussian
      ? 'Выберите ветку, чтобы посмотреть её diff.'
      : 'Select a branch to inspect its diff.';
  String get noBranchDetail =>
      isRussian ? 'Данные ветки не получены.' : 'No branch detail returned.';
  String get basicDiffViewer =>
      isRussian ? 'Просмотр diff' : 'Basic diff viewer';
  String get noDiff => isRussian
      ? 'Данные diff для этой сессии отсутствуют.'
      : 'No diff data is available for this session.';

  // Kept as a source-compatible alias for the client shell while the product
  // uses a provider-neutral name everywhere it is rendered.
  String get codexHub => productName;
  String get refresh => isRussian ? 'Обновить' : 'Refresh';
  String get projectListTitle => isRussian ? 'Проекты' : 'Projects';
  String get skillsManager => isRussian ? 'Менеджер скиллов' : 'Skills Manager';
  String get overseers => isRussian ? 'Оверсиры' : 'Overseers';
  String get waves => isRussian ? 'Волны' : 'Waves';
  String get missionControl => 'Mission Control';
  String get activeWaves => isRussian ? 'Активные волны' : 'Active waves';
  String get projects => isRussian ? 'Codex Hub' : 'Projects';
  String get project => isRussian ? 'Проект' : 'Project';
  String get refreshProject =>
      isRussian ? 'Обновить проект' : 'Refresh project';
  String get createTask => isRussian ? 'Создать задачу' : 'Create task';
  String get overview => isRussian ? 'Обзор' : 'Overview';
  String get backlog => isRussian ? 'Бэклог' : 'Backlog';
  String get docs => isRussian ? 'Документы' : 'Docs';
  String get netrunners => 'Netrunners';
  String get fixerChat => isRussian ? 'Чат Fixer' : 'Fixer Chat';
  String get clientOrdersSandbox => isRussian
      ? 'Заказы клиента и Sandbox Factory'
      : 'Client Orders & Sandbox Factory';
  String get pending => isRussian ? 'Ожидают' : 'Pending';
  String get running => isRussian ? 'В работе' : 'Running';
  String get review => isRussian ? 'Проверка' : 'Review';
  String get proposals => isRussian ? 'Предложения' : 'Proposals';
  String get sessions => isRussian ? 'Сессии' : 'Sessions';
  String get workers => isRussian ? 'Воркеры' : 'Workers';
  String get createNetrunnerTask =>
      isRussian ? 'Создать задачу Netrunner' : 'Create Netrunner task';
  String get attachDocs => isRussian ? 'Прикрепить документы' : 'Attach docs';
  String get assignMcps => isRussian ? 'Назначить MCP' : 'Assign MCPs';
  String get changeStatus => isRussian ? 'Изменить статус' : 'Change status';
  String get backToProject => isRussian ? 'Назад к проекту' : 'Back to project';
  String get summary => isRussian ? 'Сводка' : 'Summary';
  String get report => isRussian ? 'Отчёт' : 'Report';
  String get execution => isRussian ? 'Выполнение' : 'Execution';
  String get sessionSummary => isRussian ? 'Сводка сессии' : 'Session summary';
  String get structuredReport =>
      isRussian ? 'Структурированный отчёт' : 'Structured report';
  String get filesChanged => isRussian ? 'Изменённые файлы' : 'Files changed';
  String get checksRun => isRussian ? 'Проверки' : 'Checks run';
  String get commandsRun => isRussian ? 'Команды' : 'Commands run';
  String get blockers => isRussian ? 'Блокеры' : 'Blockers';
  String get residualRisks => isRussian ? 'Остаточные риски' : 'Residual risks';
  String get attachedDocs =>
      isRussian ? 'Прикреплённые документы' : 'Attached docs';
  String get availableDocs =>
      isRussian ? 'Доступные документы' : 'Available docs';
  String get assignedServers =>
      isRussian ? 'Назначенные серверы' : 'Assigned servers';
  String get availableServers =>
      isRussian ? 'Доступные серверы' : 'Available servers';
  String get reviewProposals =>
      isRussian ? 'Проверка предложений' : 'Review proposals';
  String get noAttachedDocs =>
      isRussian ? 'Прикреплённых документов нет.' : 'No attached docs.';
  String get noAvailableDocs => isRussian
      ? 'Нет доступных документов проекта.'
      : 'No project docs available for attachment.';
  String get noMcpAssignments =>
      isRussian ? 'Назначений MCP нет.' : 'No MCP assignments.';
  String get noAvailableServers => isRussian
      ? 'Каталог MCP проекта недоступен.'
      : 'No project MCP server catalog is available.';
  String get noProposals =>
      isRussian ? 'Предложений нет.' : 'No proposals recorded.';

  String status(String value) {
    if (!isRussian) {
      return switch (value) {
        'completed' => 'Completed',
        'approved' => 'Approved',
        'in_progress' => 'In progress',
        'revision_submitted' => 'Revision submitted',
        'submitted' => 'Submitted',
        'review' => 'Review',
        'failed' => 'Failed',
        'rejected' => 'Rejected',
        'pending' => 'Pending',
        'architect' => 'Architect',
        'client' => 'Client',
        _ => value,
      };
    }
    return switch (value) {
      'completed' => 'Завершено',
      'approved' => 'Одобрено',
      'in_progress' => 'В работе',
      'revision_submitted' => 'Итерация отправлена',
      'submitted' => 'Отправлено',
      'review' => 'Проверка',
      'failed' => 'Ошибка',
      'rejected' => 'Отклонено',
      'pending' => 'Ожидает',
      'Building' => 'Сборка',
      'Built' => 'Собрано',
      'Queued' => 'В очереди',
      'Awaiting review' => 'Ожидает проверки',
      'Merged' => 'Объединено',
      'Not started' => 'Не начато',
      'In progress' => 'В работе',
      'architect' => 'Архитектор',
      'client' => 'Клиент',
      _ => value,
    };
  }

  String get noneDeclared => isRussian ? 'Не указано' : 'None declared';
  String get noBlockers =>
      isRussian ? 'Блокеров нет.' : 'No blockers recorded.';
  String get noResidualRisks =>
      isRussian ? 'Остаточных рисков нет.' : 'No residual risks recorded.';
  String get noStructuredReport => isRussian
      ? 'Структурированный итоговый отчёт ещё не сохранён.'
      : 'No structured final report has been stored yet.';
  String get rawReportOnly => isRussian
      ? 'Сейчас доступен только необработанный отчёт.'
      : 'Only a raw report is available right now.';
  String get reviewPosture =>
      isRussian ? 'Состояние проверки' : 'Review posture';
  String get pendingProposalNotice => isRussian
      ? 'Перед завершением нужно явно одобрить или отклонить ожидающие предложения.'
      : 'Pending proposals need an explicit approve or reject decision before closure.';
  String get noPendingProposalNotice => isRussian
      ? 'Ожидающие предложения не блокируют проверку.'
      : 'No pending doc proposals are blocking review right now.';
  String get noActiveWorkers => isRussian
      ? 'Активных процессов воркеров нет.'
      : 'No active worker processes reported.';

  String get workroomFixerTab => isRussian ? 'Фиксер' : 'Fixer';
  String get workroomHandsTab => isRussian ? 'Руки' : 'Hands';
  String get fixerConversation =>
      isRussian ? 'Разговор с Фиксером' : 'Fixer conversation';
  String get fixerConversationHint => isRussian
      ? 'Спросите о проекте или запросите безопасное действие…'
      : 'Ask about the project or request a governed action…';
  String get sendMessage => isRussian ? 'Отправить' : 'Send';
  String get noFixerTurns => isRussian
      ? 'В этом треде пока нет сообщений.'
      : 'This thread has no messages yet.';
  String get chooseFixerThread =>
      isRussian ? 'Выбрать тред Фиксера' : 'Choose Fixer thread';
  String get genuiSurface => isRussian ? 'Поверхность GenUI' : 'GenUI surface';
  String get surfaceCatalog =>
      isRussian ? 'Поверхности проекта' : 'Project surfaces';
  String get chooseSurface =>
      isRussian ? 'Открыть поверхность' : 'Open surface';
  String get surfaceRequestPending => isRussian
      ? 'Запрашиваем типизированную поверхность…'
      : 'Requesting the typed surface…';
  String get noActiveSurface => isRussian
      ? 'Фиксер ещё не представил поверхность.'
      : 'Fixer has not presented a surface yet.';
  String get surfaceProtocolError => isRussian
      ? 'Эта ревизия не прошла проверку протокола. Последняя корректная поверхность сохранена.'
      : 'This revision failed protocol validation. The last valid surface is retained.';
  String get helpful => isRussian ? 'Полезно' : 'Helpful';
  String get notHelpful => isRussian ? 'Не помогло' : 'Not helpful';
  String get feedbackRecorded =>
      isRussian ? 'Оценка сохранена.' : 'Feedback recorded.';
  String get feedbackFailed =>
      isRussian ? 'Не удалось сохранить оценку.' : 'Could not record feedback.';
  String get governedAction =>
      isRussian ? 'Подтвердите действие' : 'Confirm governed action';
  String confirmAction(String label) => isRussian
      ? 'Выполнить «$label»? Сервер повторно проверит права и актуальность состояния.'
      : 'Run “$label”? The server will re-check authorization and current state.';
  String get cancel => isRussian ? 'Отмена' : 'Cancel';
  String get continueAction => isRussian ? 'Продолжить' : 'Continue';
  String get connectionReplaying =>
      isRussian ? 'Восстанавливаем события…' : 'Replaying durable events…';
  String get connectionReconnecting =>
      isRussian ? 'Переподключаемся…' : 'Reconnecting…';
  String get connectionProtocolError =>
      isRussian ? 'Нужно обновить приложение' : 'Application update required';
  String get connectionAuthorizationLost =>
      isRussian ? 'Доступ отозван' : 'Access revoked';
  String get handsPermanentActor =>
      isRussian ? 'Постоянный канал проекта' : 'Permanent project channel';
  String get handsMailbox =>
      isRussian ? 'Почта и история' : 'Mailbox & history';
  String get handsInstruction => isRussian ? 'Поручение' : 'Instruction';
  String get handsNoInstructions => isRussian
      ? 'Поручений пока нет. Руки остаются тем же постоянным каналом.'
      : 'No instructions yet. Hands remains the same permanent channel.';
  String get handsQueue => isRussian ? 'Очередь' : 'Queue';
  String get handsLane => isRussian ? 'Линия провайдера' : 'Provider lane';
  String get handsLease => isRussian ? 'Запись / lease' : 'Write lease';
  String get handsIdle => isRussian ? 'спит' : 'sleeping';
  String get handsRunning => isRussian ? 'работает' : 'running';
  String get handsBusy => isRussian ? 'занят' : 'busy';
  String get handsError => isRussian ? 'ошибка' : 'error';
  String get handsAwaitingReview =>
      isRussian ? 'ждёт проверки' : 'awaiting review';
  String get handsInstructionComposer =>
      isRussian ? 'Поручить Рукам' : 'Instruct Hands';
  String get handsInstructionHint => isRussian
      ? 'Опишите один проверяемый результат…'
      : 'Describe one verifiable outcome…';
  String get handsWriteScope =>
      isRussian ? 'Область записи' : 'Declared write scope';
  String get handsWriteScopeHint => isRussian
      ? 'Пути относительно проекта, по одному в строке'
      : 'Project-relative paths, one per line';
  String get handsAdvanced =>
      isRussian ? 'Провайдер и область записи' : 'Provider & write scope';
  String get handsSubmit => isRussian ? 'Поручить' : 'Submit instruction';
  String get handsSubmitted => isRussian
      ? 'Поручение сохранено в постоянной почте.'
      : 'Instruction stored in the permanent mailbox.';
  String get handsSelectLane => isRussian ? 'Сменить линию' : 'Select lane';
  String get handsCancelInstruction =>
      isRussian ? 'Отменить поручение' : 'Cancel instruction';
  String get handsAcceptResult =>
      isRussian ? 'Принять результат' : 'Accept result';
  String get handsRequestChanges =>
      isRussian ? 'Запросить изменения' : 'Request changes';
  String get handsReviewNote =>
      isRussian ? 'Комментарий проверки' : 'Review note';
  String get handsTimeline => isRussian ? 'Хронология' : 'Timeline';
  String get handsResult => isRussian ? 'Результат' : 'Result';
  String get handsRepositoryDiff =>
      isRussian ? 'Изменения репозитория' : 'Repository diff';
  String get unknownValue => isRussian ? 'неизвестно' : 'unknown';
  String surfaceLabel(String key) {
    final labels = isRussian
        ? const {
            'project.overview.v1': 'Обзор проекта',
            'wave.list.v1': 'Волны',
            'wave.detail.v1': 'Детали волны',
            'execution.review.v1': 'Проверка выполнения',
            'backlog.list.v1': 'Бэклог',
            'backlog.item.v1': 'Элемент бэклога',
            'docs.tree.v1': 'Документы',
            'docs.viewer.v1': 'Документ',
            'execution.list.v1': 'Netrunner-сессии',
            'execution.detail.v1': 'Детали сессии',
            'skills.catalog.v1': 'Навыки',
            'skills.detail.v1': 'Навык',
            'runtime.evidence.v1': 'Состояние процесса',
            'unsupported.request.v1': 'Неподдерживаемый запрос',
          }
        : const {
            'project.overview.v1': 'Project overview',
            'wave.list.v1': 'Waves',
            'wave.detail.v1': 'Wave detail',
            'execution.review.v1': 'Execution review',
            'backlog.list.v1': 'Backlog',
            'backlog.item.v1': 'Backlog item',
            'docs.tree.v1': 'Documents',
            'docs.viewer.v1': 'Document',
            'execution.list.v1': 'Netrunner sessions',
            'execution.detail.v1': 'Session detail',
            'skills.catalog.v1': 'Skills',
            'skills.detail.v1': 'Skill',
            'runtime.evidence.v1': 'Runtime evidence',
            'unsupported.request.v1': 'Unsupported request',
          };
    return labels[key] ?? key;
  }

  String get formatDatePattern => isRussian ? 'dd.MM.yyyy' : 'dd.MM.yyyy';
  String formatDate(DateTime value) =>
      DateFormat(formatDatePattern, locale.languageCode).format(value);
  String formatMoney(int cents) => NumberFormat.currency(
    locale: locale.toString(),
    symbol: '\$',
    decimalDigits: 2,
  ).format(cents / 100);
}

class AppLocalizationsDelegate extends LocalizationsDelegate<AppLocalizations> {
  const AppLocalizationsDelegate();

  @override
  bool isSupported(Locale locale) => AppLocalizations.supportedLocales.any(
    (item) => item.languageCode == locale.languageCode,
  );

  @override
  Future<AppLocalizations> load(Locale locale) =>
      SynchronousFuture<AppLocalizations>(
        AppLocalizations(isSupported(locale) ? locale : const Locale('en')),
      );

  @override
  bool shouldReload(covariant AppLocalizationsDelegate old) => false;
}

class AppLocaleController extends ChangeNotifier {
  AppLocaleController({SharedPreferences? preferences})
    : _preferences = preferences,
      _ready = Future<void>.value() {
    _ready = _restore(preferences);
  }

  static const preferenceKey = 'fixer_dashboard.locale';

  Locale? _locale;
  SharedPreferences? _preferences;
  late Future<void> _ready;
  Future<void> _pendingWrite = Future<void>.value();
  bool _hasExplicitSelection = false;
  bool _disposed = false;

  Locale? get locale => _locale;

  /// Completes after the persisted locale has been loaded.
  Future<void> get ready => _ready;

  Future<void> setLocale(Locale? locale) {
    final normalizedLocale = _normalizeLocale(locale);
    _hasExplicitSelection = true;
    if (_locale != normalizedLocale) {
      _locale = normalizedLocale;
      if (!_disposed) notifyListeners();
    }
    return _queuePersistence(normalizedLocale);
  }

  Future<void> useSystemLocale() => setLocale(null);

  Future<void> _restore(SharedPreferences? injectedPreferences) async {
    _preferences = injectedPreferences ?? await SharedPreferences.getInstance();
    if (_hasExplicitSelection) return;

    final storedLocale = _normalizeLocale(
      _localeFromPreference(_preferences!.getString(preferenceKey)),
    );
    if (_locale == storedLocale) return;
    _locale = storedLocale;
    if (!_disposed) notifyListeners();
  }

  Future<void> _queuePersistence(Locale? locale) {
    _pendingWrite = _pendingWrite.then((_) async {
      await _ready;
      final preferences = _preferences!;
      if (locale == null) {
        await preferences.remove(preferenceKey);
      } else {
        await preferences.setString(preferenceKey, locale.languageCode);
      }
    });
    return _pendingWrite;
  }

  Locale? _normalizeLocale(Locale? locale) {
    if (locale == null) return null;
    return AppLocalizations.supportedLocales.any(
          (supported) => supported.languageCode == locale.languageCode,
        )
        ? Locale(locale.languageCode)
        : null;
  }

  Locale? _localeFromPreference(String? value) {
    if (value == null || value.trim().isEmpty) return null;
    return Locale(value.trim());
  }

  @override
  void dispose() {
    _disposed = true;
    super.dispose();
  }
}

class AppLocaleScope extends InheritedNotifier<AppLocaleController> {
  const AppLocaleScope({
    super.key,
    required super.notifier,
    required super.child,
  });

  static AppLocaleController? maybeOf(BuildContext context) =>
      context.dependOnInheritedWidgetOfExactType<AppLocaleScope>()?.notifier;
}

class LanguageSwitcher extends StatelessWidget {
  const LanguageSwitcher({super.key, this.compact = false});

  final bool compact;

  @override
  Widget build(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    final controller = AppLocaleScope.maybeOf(context);
    return PopupMenuButton<Locale>(
      key: const ValueKey('language-switcher'),
      tooltip: l10n.language,
      onSelected: controller?.setLocale,
      itemBuilder: (_) => const [
        PopupMenuItem(value: Locale('en'), child: Text('English')),
        PopupMenuItem(value: Locale('ru'), child: Text('Русский')),
      ],
      child: Padding(
        padding: EdgeInsets.symmetric(horizontal: compact ? 8 : 12),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            const Icon(Icons.language),
            if (!compact) ...[const SizedBox(width: 6), Text(l10n.language)],
          ],
        ),
      ),
    );
  }
}
