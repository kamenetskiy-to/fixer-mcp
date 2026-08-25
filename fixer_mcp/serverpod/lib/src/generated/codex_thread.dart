/* AUTOMATICALLY GENERATED CODE DO NOT MODIFY */
/*   To generate run: "serverpod generate"    */

// ignore_for_file: implementation_imports
// ignore_for_file: library_private_types_in_public_api
// ignore_for_file: non_constant_identifier_names
// ignore_for_file: public_member_api_docs
// ignore_for_file: type_literal_in_constant_pattern
// ignore_for_file: use_super_parameters
// ignore_for_file: invalid_use_of_internal_member

// ignore_for_file: no_leading_underscores_for_library_prefixes
import 'package:serverpod/serverpod.dart' as _i1;

/// A persisted Codex thread bound to an external app-server thread id.
abstract class CodexThread
    implements _i1.TableRow<int?>, _i1.ProtocolSerialization {
  CodexThread._({
    this.id,
    required this.threadId,
    this.title,
    this.cwd,
    this.model,
    this.reasoningEffort,
    this.sandboxMode,
    bool? isActive,
    this.activeTurnId,
    this.activeSince,
    this.lastActivityAt,
    this.lastCompletedAt,
    DateTime? createdAt,
  }) : isActive = isActive ?? false,
       createdAt = createdAt ?? DateTime.now();

  factory CodexThread({
    int? id,
    required String threadId,
    String? title,
    String? cwd,
    String? model,
    String? reasoningEffort,
    String? sandboxMode,
    bool? isActive,
    String? activeTurnId,
    DateTime? activeSince,
    DateTime? lastActivityAt,
    DateTime? lastCompletedAt,
    DateTime? createdAt,
  }) = _CodexThreadImpl;

  factory CodexThread.fromJson(Map<String, dynamic> jsonSerialization) {
    return CodexThread(
      id: jsonSerialization['id'] as int?,
      threadId: jsonSerialization['threadId'] as String,
      title: jsonSerialization['title'] as String?,
      cwd: jsonSerialization['cwd'] as String?,
      model: jsonSerialization['model'] as String?,
      reasoningEffort: jsonSerialization['reasoningEffort'] as String?,
      sandboxMode: jsonSerialization['sandboxMode'] as String?,
      isActive: jsonSerialization['isActive'] == null
          ? null
          : _i1.BoolJsonExtension.fromJson(jsonSerialization['isActive']),
      activeTurnId: jsonSerialization['activeTurnId'] as String?,
      activeSince: jsonSerialization['activeSince'] == null
          ? null
          : _i1.DateTimeJsonExtension.fromJson(
              jsonSerialization['activeSince'],
            ),
      lastActivityAt: jsonSerialization['lastActivityAt'] == null
          ? null
          : _i1.DateTimeJsonExtension.fromJson(
              jsonSerialization['lastActivityAt'],
            ),
      lastCompletedAt: jsonSerialization['lastCompletedAt'] == null
          ? null
          : _i1.DateTimeJsonExtension.fromJson(
              jsonSerialization['lastCompletedAt'],
            ),
      createdAt: jsonSerialization['createdAt'] == null
          ? null
          : _i1.DateTimeJsonExtension.fromJson(jsonSerialization['createdAt']),
    );
  }

  static final t = CodexThreadTable();

  static const db = CodexThreadRepository._();

  @override
  int? id;

  String threadId;

  String? title;

  String? cwd;

  String? model;

  String? reasoningEffort;

  String? sandboxMode;

  bool isActive;

  String? activeTurnId;

  DateTime? activeSince;

  DateTime? lastActivityAt;

  DateTime? lastCompletedAt;

  DateTime createdAt;

  @override
  _i1.Table<int?> get table => t;

  /// Returns a shallow copy of this [CodexThread]
  /// with some or all fields replaced by the given arguments.
  @_i1.useResult
  CodexThread copyWith({
    int? id,
    String? threadId,
    String? title,
    String? cwd,
    String? model,
    String? reasoningEffort,
    String? sandboxMode,
    bool? isActive,
    String? activeTurnId,
    DateTime? activeSince,
    DateTime? lastActivityAt,
    DateTime? lastCompletedAt,
    DateTime? createdAt,
  });
  @override
  Map<String, dynamic> toJson() {
    return {
      '__className__': 'CodexThread',
      if (id != null) 'id': id,
      'threadId': threadId,
      if (title != null) 'title': title,
      if (cwd != null) 'cwd': cwd,
      if (model != null) 'model': model,
      if (reasoningEffort != null) 'reasoningEffort': reasoningEffort,
      if (sandboxMode != null) 'sandboxMode': sandboxMode,
      'isActive': isActive,
      if (activeTurnId != null) 'activeTurnId': activeTurnId,
      if (activeSince != null) 'activeSince': activeSince?.toJson(),
      if (lastActivityAt != null) 'lastActivityAt': lastActivityAt?.toJson(),
      if (lastCompletedAt != null) 'lastCompletedAt': lastCompletedAt?.toJson(),
      'createdAt': createdAt.toJson(),
    };
  }

  @override
  Map<String, dynamic> toJsonForProtocol() {
    return {
      '__className__': 'CodexThread',
      if (id != null) 'id': id,
      'threadId': threadId,
      if (title != null) 'title': title,
      if (cwd != null) 'cwd': cwd,
      if (model != null) 'model': model,
      if (reasoningEffort != null) 'reasoningEffort': reasoningEffort,
      if (sandboxMode != null) 'sandboxMode': sandboxMode,
      'isActive': isActive,
      if (activeTurnId != null) 'activeTurnId': activeTurnId,
      if (activeSince != null) 'activeSince': activeSince?.toJson(),
      if (lastActivityAt != null) 'lastActivityAt': lastActivityAt?.toJson(),
      if (lastCompletedAt != null) 'lastCompletedAt': lastCompletedAt?.toJson(),
      'createdAt': createdAt.toJson(),
    };
  }

  static CodexThreadInclude include() {
    return CodexThreadInclude._();
  }

  static CodexThreadIncludeList includeList({
    _i1.WhereExpressionBuilder<CodexThreadTable>? where,
    int? limit,
    int? offset,
    _i1.OrderByBuilder<CodexThreadTable>? orderBy,
    bool orderDescending = false,
    _i1.OrderByListBuilder<CodexThreadTable>? orderByList,
    CodexThreadInclude? include,
  }) {
    return CodexThreadIncludeList._(
      where: where,
      limit: limit,
      offset: offset,
      orderBy: orderBy?.call(CodexThread.t),
      orderDescending: orderDescending,
      orderByList: orderByList?.call(CodexThread.t),
      include: include,
    );
  }

  @override
  String toString() {
    return _i1.SerializationManager.encode(this);
  }
}

class _Undefined {}

class _CodexThreadImpl extends CodexThread {
  _CodexThreadImpl({
    int? id,
    required String threadId,
    String? title,
    String? cwd,
    String? model,
    String? reasoningEffort,
    String? sandboxMode,
    bool? isActive,
    String? activeTurnId,
    DateTime? activeSince,
    DateTime? lastActivityAt,
    DateTime? lastCompletedAt,
    DateTime? createdAt,
  }) : super._(
         id: id,
         threadId: threadId,
         title: title,
         cwd: cwd,
         model: model,
         reasoningEffort: reasoningEffort,
         sandboxMode: sandboxMode,
         isActive: isActive,
         activeTurnId: activeTurnId,
         activeSince: activeSince,
         lastActivityAt: lastActivityAt,
         lastCompletedAt: lastCompletedAt,
         createdAt: createdAt,
       );

  /// Returns a shallow copy of this [CodexThread]
  /// with some or all fields replaced by the given arguments.
  @_i1.useResult
  @override
  CodexThread copyWith({
    Object? id = _Undefined,
    String? threadId,
    Object? title = _Undefined,
    Object? cwd = _Undefined,
    Object? model = _Undefined,
    Object? reasoningEffort = _Undefined,
    Object? sandboxMode = _Undefined,
    bool? isActive,
    Object? activeTurnId = _Undefined,
    Object? activeSince = _Undefined,
    Object? lastActivityAt = _Undefined,
    Object? lastCompletedAt = _Undefined,
    DateTime? createdAt,
  }) {
    return CodexThread(
      id: id is int? ? id : this.id,
      threadId: threadId ?? this.threadId,
      title: title is String? ? title : this.title,
      cwd: cwd is String? ? cwd : this.cwd,
      model: model is String? ? model : this.model,
      reasoningEffort: reasoningEffort is String?
          ? reasoningEffort
          : this.reasoningEffort,
      sandboxMode: sandboxMode is String? ? sandboxMode : this.sandboxMode,
      isActive: isActive ?? this.isActive,
      activeTurnId: activeTurnId is String? ? activeTurnId : this.activeTurnId,
      activeSince: activeSince is DateTime? ? activeSince : this.activeSince,
      lastActivityAt: lastActivityAt is DateTime?
          ? lastActivityAt
          : this.lastActivityAt,
      lastCompletedAt: lastCompletedAt is DateTime?
          ? lastCompletedAt
          : this.lastCompletedAt,
      createdAt: createdAt ?? this.createdAt,
    );
  }
}

class CodexThreadUpdateTable extends _i1.UpdateTable<CodexThreadTable> {
  CodexThreadUpdateTable(super.table);

  _i1.ColumnValue<String, String> threadId(String value) => _i1.ColumnValue(
    table.threadId,
    value,
  );

  _i1.ColumnValue<String, String> title(String? value) => _i1.ColumnValue(
    table.title,
    value,
  );

  _i1.ColumnValue<String, String> cwd(String? value) => _i1.ColumnValue(
    table.cwd,
    value,
  );

  _i1.ColumnValue<String, String> model(String? value) => _i1.ColumnValue(
    table.model,
    value,
  );

  _i1.ColumnValue<String, String> reasoningEffort(String? value) =>
      _i1.ColumnValue(
        table.reasoningEffort,
        value,
      );

  _i1.ColumnValue<String, String> sandboxMode(String? value) => _i1.ColumnValue(
    table.sandboxMode,
    value,
  );

  _i1.ColumnValue<bool, bool> isActive(bool value) => _i1.ColumnValue(
    table.isActive,
    value,
  );

  _i1.ColumnValue<String, String> activeTurnId(String? value) =>
      _i1.ColumnValue(
        table.activeTurnId,
        value,
      );

  _i1.ColumnValue<DateTime, DateTime> activeSince(DateTime? value) =>
      _i1.ColumnValue(
        table.activeSince,
        value,
      );

  _i1.ColumnValue<DateTime, DateTime> lastActivityAt(DateTime? value) =>
      _i1.ColumnValue(
        table.lastActivityAt,
        value,
      );

  _i1.ColumnValue<DateTime, DateTime> lastCompletedAt(DateTime? value) =>
      _i1.ColumnValue(
        table.lastCompletedAt,
        value,
      );

  _i1.ColumnValue<DateTime, DateTime> createdAt(DateTime value) =>
      _i1.ColumnValue(
        table.createdAt,
        value,
      );
}

class CodexThreadTable extends _i1.Table<int?> {
  CodexThreadTable({super.tableRelation}) : super(tableName: 'codex_thread') {
    updateTable = CodexThreadUpdateTable(this);
    threadId = _i1.ColumnString(
      'threadId',
      this,
    );
    title = _i1.ColumnString(
      'title',
      this,
    );
    cwd = _i1.ColumnString(
      'cwd',
      this,
    );
    model = _i1.ColumnString(
      'model',
      this,
    );
    reasoningEffort = _i1.ColumnString(
      'reasoningEffort',
      this,
    );
    sandboxMode = _i1.ColumnString(
      'sandboxMode',
      this,
    );
    isActive = _i1.ColumnBool(
      'isActive',
      this,
      hasDefault: true,
    );
    activeTurnId = _i1.ColumnString(
      'activeTurnId',
      this,
    );
    activeSince = _i1.ColumnDateTime(
      'activeSince',
      this,
    );
    lastActivityAt = _i1.ColumnDateTime(
      'lastActivityAt',
      this,
    );
    lastCompletedAt = _i1.ColumnDateTime(
      'lastCompletedAt',
      this,
    );
    createdAt = _i1.ColumnDateTime(
      'createdAt',
      this,
      hasDefault: true,
    );
  }

  late final CodexThreadUpdateTable updateTable;

  late final _i1.ColumnString threadId;

  late final _i1.ColumnString title;

  late final _i1.ColumnString cwd;

  late final _i1.ColumnString model;

  late final _i1.ColumnString reasoningEffort;

  late final _i1.ColumnString sandboxMode;

  late final _i1.ColumnBool isActive;

  late final _i1.ColumnString activeTurnId;

  late final _i1.ColumnDateTime activeSince;

  late final _i1.ColumnDateTime lastActivityAt;

  late final _i1.ColumnDateTime lastCompletedAt;

  late final _i1.ColumnDateTime createdAt;

  @override
  List<_i1.Column> get columns => [
    id,
    threadId,
    title,
    cwd,
    model,
    reasoningEffort,
    sandboxMode,
    isActive,
    activeTurnId,
    activeSince,
    lastActivityAt,
    lastCompletedAt,
    createdAt,
  ];
}

class CodexThreadInclude extends _i1.IncludeObject {
  CodexThreadInclude._();

  @override
  Map<String, _i1.Include?> get includes => {};

  @override
  _i1.Table<int?> get table => CodexThread.t;
}

class CodexThreadIncludeList extends _i1.IncludeList {
  CodexThreadIncludeList._({
    _i1.WhereExpressionBuilder<CodexThreadTable>? where,
    super.limit,
    super.offset,
    super.orderBy,
    super.orderDescending,
    super.orderByList,
    super.include,
  }) {
    super.where = where?.call(CodexThread.t);
  }

  @override
  Map<String, _i1.Include?> get includes => include?.includes ?? {};

  @override
  _i1.Table<int?> get table => CodexThread.t;
}

class CodexThreadRepository {
  const CodexThreadRepository._();

  /// Returns a list of [CodexThread]s matching the given query parameters.
  ///
  /// Use [where] to specify which items to include in the return value.
  /// If none is specified, all items will be returned.
  ///
  /// To specify the order of the items use [orderBy] or [orderByList]
  /// when sorting by multiple columns.
  ///
  /// The maximum number of items can be set by [limit]. If no limit is set,
  /// all items matching the query will be returned.
  ///
  /// [offset] defines how many items to skip, after which [limit] (or all)
  /// items are read from the database.
  ///
  /// ```dart
  /// var persons = await Persons.db.find(
  ///   session,
  ///   where: (t) => t.lastName.equals('Jones'),
  ///   orderBy: (t) => t.firstName,
  ///   limit: 100,
  /// );
  /// ```
  Future<List<CodexThread>> find(
    _i1.DatabaseSession session, {
    _i1.WhereExpressionBuilder<CodexThreadTable>? where,
    int? limit,
    int? offset,
    _i1.OrderByBuilder<CodexThreadTable>? orderBy,
    bool orderDescending = false,
    _i1.OrderByListBuilder<CodexThreadTable>? orderByList,
    _i1.Transaction? transaction,
    _i1.LockMode? lockMode,
    _i1.LockBehavior? lockBehavior,
  }) async {
    return session.db.find<CodexThread>(
      where: where?.call(CodexThread.t),
      orderBy: orderBy?.call(CodexThread.t),
      orderByList: orderByList?.call(CodexThread.t),
      orderDescending: orderDescending,
      limit: limit,
      offset: offset,
      transaction: transaction,
      lockMode: lockMode,
      lockBehavior: lockBehavior,
    );
  }

  /// Returns the first matching [CodexThread] matching the given query parameters.
  ///
  /// Use [where] to specify which items to include in the return value.
  /// If none is specified, all items will be returned.
  ///
  /// To specify the order use [orderBy] or [orderByList]
  /// when sorting by multiple columns.
  ///
  /// [offset] defines how many items to skip, after which the next one will be picked.
  ///
  /// ```dart
  /// var youngestPerson = await Persons.db.findFirstRow(
  ///   session,
  ///   where: (t) => t.lastName.equals('Jones'),
  ///   orderBy: (t) => t.age,
  /// );
  /// ```
  Future<CodexThread?> findFirstRow(
    _i1.DatabaseSession session, {
    _i1.WhereExpressionBuilder<CodexThreadTable>? where,
    int? offset,
    _i1.OrderByBuilder<CodexThreadTable>? orderBy,
    bool orderDescending = false,
    _i1.OrderByListBuilder<CodexThreadTable>? orderByList,
    _i1.Transaction? transaction,
    _i1.LockMode? lockMode,
    _i1.LockBehavior? lockBehavior,
  }) async {
    return session.db.findFirstRow<CodexThread>(
      where: where?.call(CodexThread.t),
      orderBy: orderBy?.call(CodexThread.t),
      orderByList: orderByList?.call(CodexThread.t),
      orderDescending: orderDescending,
      offset: offset,
      transaction: transaction,
      lockMode: lockMode,
      lockBehavior: lockBehavior,
    );
  }

  /// Finds a single [CodexThread] by its [id] or null if no such row exists.
  Future<CodexThread?> findById(
    _i1.DatabaseSession session,
    int id, {
    _i1.Transaction? transaction,
    _i1.LockMode? lockMode,
    _i1.LockBehavior? lockBehavior,
  }) async {
    return session.db.findById<CodexThread>(
      id,
      transaction: transaction,
      lockMode: lockMode,
      lockBehavior: lockBehavior,
    );
  }

  /// Inserts all [CodexThread]s in the list and returns the inserted rows.
  ///
  /// The returned [CodexThread]s will have their `id` fields set.
  ///
  /// This is an atomic operation, meaning that if one of the rows fails to
  /// insert, none of the rows will be inserted.
  ///
  /// If [ignoreConflicts] is set to `true`, rows that conflict with existing
  /// rows are silently skipped, and only the successfully inserted rows are
  /// returned.
  Future<List<CodexThread>> insert(
    _i1.DatabaseSession session,
    List<CodexThread> rows, {
    _i1.Transaction? transaction,
    bool ignoreConflicts = false,
  }) async {
    return session.db.insert<CodexThread>(
      rows,
      transaction: transaction,
      ignoreConflicts: ignoreConflicts,
    );
  }

  /// Inserts a single [CodexThread] and returns the inserted row.
  ///
  /// The returned [CodexThread] will have its `id` field set.
  Future<CodexThread> insertRow(
    _i1.DatabaseSession session,
    CodexThread row, {
    _i1.Transaction? transaction,
  }) async {
    return session.db.insertRow<CodexThread>(
      row,
      transaction: transaction,
    );
  }

  /// Updates all [CodexThread]s in the list and returns the updated rows. If
  /// [columns] is provided, only those columns will be updated. Defaults to
  /// all columns.
  /// This is an atomic operation, meaning that if one of the rows fails to
  /// update, none of the rows will be updated.
  Future<List<CodexThread>> update(
    _i1.DatabaseSession session,
    List<CodexThread> rows, {
    _i1.ColumnSelections<CodexThreadTable>? columns,
    _i1.Transaction? transaction,
  }) async {
    return session.db.update<CodexThread>(
      rows,
      columns: columns?.call(CodexThread.t),
      transaction: transaction,
    );
  }

  /// Updates a single [CodexThread]. The row needs to have its id set.
  /// Optionally, a list of [columns] can be provided to only update those
  /// columns. Defaults to all columns.
  Future<CodexThread> updateRow(
    _i1.DatabaseSession session,
    CodexThread row, {
    _i1.ColumnSelections<CodexThreadTable>? columns,
    _i1.Transaction? transaction,
  }) async {
    return session.db.updateRow<CodexThread>(
      row,
      columns: columns?.call(CodexThread.t),
      transaction: transaction,
    );
  }

  /// Updates a single [CodexThread] by its [id] with the specified [columnValues].
  /// Returns the updated row or null if no row with the given id exists.
  Future<CodexThread?> updateById(
    _i1.DatabaseSession session,
    int id, {
    required _i1.ColumnValueListBuilder<CodexThreadUpdateTable> columnValues,
    _i1.Transaction? transaction,
  }) async {
    return session.db.updateById<CodexThread>(
      id,
      columnValues: columnValues(CodexThread.t.updateTable),
      transaction: transaction,
    );
  }

  /// Updates all [CodexThread]s matching the [where] expression with the specified [columnValues].
  /// Returns the list of updated rows.
  Future<List<CodexThread>> updateWhere(
    _i1.DatabaseSession session, {
    required _i1.ColumnValueListBuilder<CodexThreadUpdateTable> columnValues,
    required _i1.WhereExpressionBuilder<CodexThreadTable> where,
    int? limit,
    int? offset,
    _i1.OrderByBuilder<CodexThreadTable>? orderBy,
    _i1.OrderByListBuilder<CodexThreadTable>? orderByList,
    bool orderDescending = false,
    _i1.Transaction? transaction,
  }) async {
    return session.db.updateWhere<CodexThread>(
      columnValues: columnValues(CodexThread.t.updateTable),
      where: where(CodexThread.t),
      limit: limit,
      offset: offset,
      orderBy: orderBy?.call(CodexThread.t),
      orderByList: orderByList?.call(CodexThread.t),
      orderDescending: orderDescending,
      transaction: transaction,
    );
  }

  /// Deletes all [CodexThread]s in the list and returns the deleted rows.
  /// This is an atomic operation, meaning that if one of the rows fail to
  /// be deleted, none of the rows will be deleted.
  Future<List<CodexThread>> delete(
    _i1.DatabaseSession session,
    List<CodexThread> rows, {
    _i1.Transaction? transaction,
  }) async {
    return session.db.delete<CodexThread>(
      rows,
      transaction: transaction,
    );
  }

  /// Deletes a single [CodexThread].
  Future<CodexThread> deleteRow(
    _i1.DatabaseSession session,
    CodexThread row, {
    _i1.Transaction? transaction,
  }) async {
    return session.db.deleteRow<CodexThread>(
      row,
      transaction: transaction,
    );
  }

  /// Deletes all rows matching the [where] expression.
  Future<List<CodexThread>> deleteWhere(
    _i1.DatabaseSession session, {
    required _i1.WhereExpressionBuilder<CodexThreadTable> where,
    _i1.Transaction? transaction,
  }) async {
    return session.db.deleteWhere<CodexThread>(
      where: where(CodexThread.t),
      transaction: transaction,
    );
  }

  /// Counts the number of rows matching the [where] expression. If omitted,
  /// will return the count of all rows in the table.
  Future<int> count(
    _i1.DatabaseSession session, {
    _i1.WhereExpressionBuilder<CodexThreadTable>? where,
    int? limit,
    _i1.Transaction? transaction,
  }) async {
    return session.db.count<CodexThread>(
      where: where?.call(CodexThread.t),
      limit: limit,
      transaction: transaction,
    );
  }

  /// Acquires row-level locks on [CodexThread] rows matching the [where] expression.
  Future<void> lockRows(
    _i1.DatabaseSession session, {
    required _i1.WhereExpressionBuilder<CodexThreadTable> where,
    required _i1.LockMode lockMode,
    required _i1.Transaction transaction,
    _i1.LockBehavior lockBehavior = _i1.LockBehavior.wait,
  }) async {
    return session.db.lockRows<CodexThread>(
      where: where(CodexThread.t),
      lockMode: lockMode,
      lockBehavior: lockBehavior,
      transaction: transaction,
    );
  }
}
