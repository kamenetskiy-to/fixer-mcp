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

/// A durable raw notification received from codex-bridge for a turn.
abstract class CodexTurnEvent
    implements _i1.TableRow<int?>, _i1.ProtocolSerialization {
  CodexTurnEvent._({
    this.id,
    required this.codexThreadId,
    required this.turnId,
    required this.method,
    required this.json,
    DateTime? createdAt,
  }) : createdAt = createdAt ?? DateTime.now();

  factory CodexTurnEvent({
    int? id,
    required int codexThreadId,
    required String turnId,
    required String method,
    required String json,
    DateTime? createdAt,
  }) = _CodexTurnEventImpl;

  factory CodexTurnEvent.fromJson(Map<String, dynamic> jsonSerialization) {
    return CodexTurnEvent(
      id: jsonSerialization['id'] as int?,
      codexThreadId: jsonSerialization['codexThreadId'] as int,
      turnId: jsonSerialization['turnId'] as String,
      method: jsonSerialization['method'] as String,
      json: jsonSerialization['json'] as String,
      createdAt: jsonSerialization['createdAt'] == null
          ? null
          : _i1.DateTimeJsonExtension.fromJson(jsonSerialization['createdAt']),
    );
  }

  static final t = CodexTurnEventTable();

  static const db = CodexTurnEventRepository._();

  @override
  int? id;

  int codexThreadId;

  String turnId;

  String method;

  String json;

  DateTime createdAt;

  @override
  _i1.Table<int?> get table => t;

  /// Returns a shallow copy of this [CodexTurnEvent]
  /// with some or all fields replaced by the given arguments.
  @_i1.useResult
  CodexTurnEvent copyWith({
    int? id,
    int? codexThreadId,
    String? turnId,
    String? method,
    String? json,
    DateTime? createdAt,
  });
  @override
  Map<String, dynamic> toJson() {
    return {
      '__className__': 'CodexTurnEvent',
      if (id != null) 'id': id,
      'codexThreadId': codexThreadId,
      'turnId': turnId,
      'method': method,
      'json': json,
      'createdAt': createdAt.toJson(),
    };
  }

  @override
  Map<String, dynamic> toJsonForProtocol() {
    return {
      '__className__': 'CodexTurnEvent',
      if (id != null) 'id': id,
      'codexThreadId': codexThreadId,
      'turnId': turnId,
      'method': method,
      'json': json,
      'createdAt': createdAt.toJson(),
    };
  }

  static CodexTurnEventInclude include() {
    return CodexTurnEventInclude._();
  }

  static CodexTurnEventIncludeList includeList({
    _i1.WhereExpressionBuilder<CodexTurnEventTable>? where,
    int? limit,
    int? offset,
    _i1.OrderByBuilder<CodexTurnEventTable>? orderBy,
    bool orderDescending = false,
    _i1.OrderByListBuilder<CodexTurnEventTable>? orderByList,
    CodexTurnEventInclude? include,
  }) {
    return CodexTurnEventIncludeList._(
      where: where,
      limit: limit,
      offset: offset,
      orderBy: orderBy?.call(CodexTurnEvent.t),
      orderDescending: orderDescending,
      orderByList: orderByList?.call(CodexTurnEvent.t),
      include: include,
    );
  }

  @override
  String toString() {
    return _i1.SerializationManager.encode(this);
  }
}

class _Undefined {}

class _CodexTurnEventImpl extends CodexTurnEvent {
  _CodexTurnEventImpl({
    int? id,
    required int codexThreadId,
    required String turnId,
    required String method,
    required String json,
    DateTime? createdAt,
  }) : super._(
         id: id,
         codexThreadId: codexThreadId,
         turnId: turnId,
         method: method,
         json: json,
         createdAt: createdAt,
       );

  /// Returns a shallow copy of this [CodexTurnEvent]
  /// with some or all fields replaced by the given arguments.
  @_i1.useResult
  @override
  CodexTurnEvent copyWith({
    Object? id = _Undefined,
    int? codexThreadId,
    String? turnId,
    String? method,
    String? json,
    DateTime? createdAt,
  }) {
    return CodexTurnEvent(
      id: id is int? ? id : this.id,
      codexThreadId: codexThreadId ?? this.codexThreadId,
      turnId: turnId ?? this.turnId,
      method: method ?? this.method,
      json: json ?? this.json,
      createdAt: createdAt ?? this.createdAt,
    );
  }
}

class CodexTurnEventUpdateTable extends _i1.UpdateTable<CodexTurnEventTable> {
  CodexTurnEventUpdateTable(super.table);

  _i1.ColumnValue<int, int> codexThreadId(int value) => _i1.ColumnValue(
    table.codexThreadId,
    value,
  );

  _i1.ColumnValue<String, String> turnId(String value) => _i1.ColumnValue(
    table.turnId,
    value,
  );

  _i1.ColumnValue<String, String> method(String value) => _i1.ColumnValue(
    table.method,
    value,
  );

  _i1.ColumnValue<String, String> json(String value) => _i1.ColumnValue(
    table.json,
    value,
  );

  _i1.ColumnValue<DateTime, DateTime> createdAt(DateTime value) =>
      _i1.ColumnValue(
        table.createdAt,
        value,
      );
}

class CodexTurnEventTable extends _i1.Table<int?> {
  CodexTurnEventTable({super.tableRelation})
    : super(tableName: 'codex_turn_event') {
    updateTable = CodexTurnEventUpdateTable(this);
    codexThreadId = _i1.ColumnInt(
      'codexThreadId',
      this,
    );
    turnId = _i1.ColumnString(
      'turnId',
      this,
    );
    method = _i1.ColumnString(
      'method',
      this,
    );
    json = _i1.ColumnString(
      'json',
      this,
    );
    createdAt = _i1.ColumnDateTime(
      'createdAt',
      this,
      hasDefault: true,
    );
  }

  late final CodexTurnEventUpdateTable updateTable;

  late final _i1.ColumnInt codexThreadId;

  late final _i1.ColumnString turnId;

  late final _i1.ColumnString method;

  late final _i1.ColumnString json;

  late final _i1.ColumnDateTime createdAt;

  @override
  List<_i1.Column> get columns => [
    id,
    codexThreadId,
    turnId,
    method,
    json,
    createdAt,
  ];
}

class CodexTurnEventInclude extends _i1.IncludeObject {
  CodexTurnEventInclude._();

  @override
  Map<String, _i1.Include?> get includes => {};

  @override
  _i1.Table<int?> get table => CodexTurnEvent.t;
}

class CodexTurnEventIncludeList extends _i1.IncludeList {
  CodexTurnEventIncludeList._({
    _i1.WhereExpressionBuilder<CodexTurnEventTable>? where,
    super.limit,
    super.offset,
    super.orderBy,
    super.orderDescending,
    super.orderByList,
    super.include,
  }) {
    super.where = where?.call(CodexTurnEvent.t);
  }

  @override
  Map<String, _i1.Include?> get includes => include?.includes ?? {};

  @override
  _i1.Table<int?> get table => CodexTurnEvent.t;
}

class CodexTurnEventRepository {
  const CodexTurnEventRepository._();

  /// Returns a list of [CodexTurnEvent]s matching the given query parameters.
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
  Future<List<CodexTurnEvent>> find(
    _i1.DatabaseSession session, {
    _i1.WhereExpressionBuilder<CodexTurnEventTable>? where,
    int? limit,
    int? offset,
    _i1.OrderByBuilder<CodexTurnEventTable>? orderBy,
    bool orderDescending = false,
    _i1.OrderByListBuilder<CodexTurnEventTable>? orderByList,
    _i1.Transaction? transaction,
    _i1.LockMode? lockMode,
    _i1.LockBehavior? lockBehavior,
  }) async {
    return session.db.find<CodexTurnEvent>(
      where: where?.call(CodexTurnEvent.t),
      orderBy: orderBy?.call(CodexTurnEvent.t),
      orderByList: orderByList?.call(CodexTurnEvent.t),
      orderDescending: orderDescending,
      limit: limit,
      offset: offset,
      transaction: transaction,
      lockMode: lockMode,
      lockBehavior: lockBehavior,
    );
  }

  /// Returns the first matching [CodexTurnEvent] matching the given query parameters.
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
  Future<CodexTurnEvent?> findFirstRow(
    _i1.DatabaseSession session, {
    _i1.WhereExpressionBuilder<CodexTurnEventTable>? where,
    int? offset,
    _i1.OrderByBuilder<CodexTurnEventTable>? orderBy,
    bool orderDescending = false,
    _i1.OrderByListBuilder<CodexTurnEventTable>? orderByList,
    _i1.Transaction? transaction,
    _i1.LockMode? lockMode,
    _i1.LockBehavior? lockBehavior,
  }) async {
    return session.db.findFirstRow<CodexTurnEvent>(
      where: where?.call(CodexTurnEvent.t),
      orderBy: orderBy?.call(CodexTurnEvent.t),
      orderByList: orderByList?.call(CodexTurnEvent.t),
      orderDescending: orderDescending,
      offset: offset,
      transaction: transaction,
      lockMode: lockMode,
      lockBehavior: lockBehavior,
    );
  }

  /// Finds a single [CodexTurnEvent] by its [id] or null if no such row exists.
  Future<CodexTurnEvent?> findById(
    _i1.DatabaseSession session,
    int id, {
    _i1.Transaction? transaction,
    _i1.LockMode? lockMode,
    _i1.LockBehavior? lockBehavior,
  }) async {
    return session.db.findById<CodexTurnEvent>(
      id,
      transaction: transaction,
      lockMode: lockMode,
      lockBehavior: lockBehavior,
    );
  }

  /// Inserts all [CodexTurnEvent]s in the list and returns the inserted rows.
  ///
  /// The returned [CodexTurnEvent]s will have their `id` fields set.
  ///
  /// This is an atomic operation, meaning that if one of the rows fails to
  /// insert, none of the rows will be inserted.
  ///
  /// If [ignoreConflicts] is set to `true`, rows that conflict with existing
  /// rows are silently skipped, and only the successfully inserted rows are
  /// returned.
  Future<List<CodexTurnEvent>> insert(
    _i1.DatabaseSession session,
    List<CodexTurnEvent> rows, {
    _i1.Transaction? transaction,
    bool ignoreConflicts = false,
  }) async {
    return session.db.insert<CodexTurnEvent>(
      rows,
      transaction: transaction,
      ignoreConflicts: ignoreConflicts,
    );
  }

  /// Inserts a single [CodexTurnEvent] and returns the inserted row.
  ///
  /// The returned [CodexTurnEvent] will have its `id` field set.
  Future<CodexTurnEvent> insertRow(
    _i1.DatabaseSession session,
    CodexTurnEvent row, {
    _i1.Transaction? transaction,
  }) async {
    return session.db.insertRow<CodexTurnEvent>(
      row,
      transaction: transaction,
    );
  }

  /// Updates all [CodexTurnEvent]s in the list and returns the updated rows. If
  /// [columns] is provided, only those columns will be updated. Defaults to
  /// all columns.
  /// This is an atomic operation, meaning that if one of the rows fails to
  /// update, none of the rows will be updated.
  Future<List<CodexTurnEvent>> update(
    _i1.DatabaseSession session,
    List<CodexTurnEvent> rows, {
    _i1.ColumnSelections<CodexTurnEventTable>? columns,
    _i1.Transaction? transaction,
  }) async {
    return session.db.update<CodexTurnEvent>(
      rows,
      columns: columns?.call(CodexTurnEvent.t),
      transaction: transaction,
    );
  }

  /// Updates a single [CodexTurnEvent]. The row needs to have its id set.
  /// Optionally, a list of [columns] can be provided to only update those
  /// columns. Defaults to all columns.
  Future<CodexTurnEvent> updateRow(
    _i1.DatabaseSession session,
    CodexTurnEvent row, {
    _i1.ColumnSelections<CodexTurnEventTable>? columns,
    _i1.Transaction? transaction,
  }) async {
    return session.db.updateRow<CodexTurnEvent>(
      row,
      columns: columns?.call(CodexTurnEvent.t),
      transaction: transaction,
    );
  }

  /// Updates a single [CodexTurnEvent] by its [id] with the specified [columnValues].
  /// Returns the updated row or null if no row with the given id exists.
  Future<CodexTurnEvent?> updateById(
    _i1.DatabaseSession session,
    int id, {
    required _i1.ColumnValueListBuilder<CodexTurnEventUpdateTable> columnValues,
    _i1.Transaction? transaction,
  }) async {
    return session.db.updateById<CodexTurnEvent>(
      id,
      columnValues: columnValues(CodexTurnEvent.t.updateTable),
      transaction: transaction,
    );
  }

  /// Updates all [CodexTurnEvent]s matching the [where] expression with the specified [columnValues].
  /// Returns the list of updated rows.
  Future<List<CodexTurnEvent>> updateWhere(
    _i1.DatabaseSession session, {
    required _i1.ColumnValueListBuilder<CodexTurnEventUpdateTable> columnValues,
    required _i1.WhereExpressionBuilder<CodexTurnEventTable> where,
    int? limit,
    int? offset,
    _i1.OrderByBuilder<CodexTurnEventTable>? orderBy,
    _i1.OrderByListBuilder<CodexTurnEventTable>? orderByList,
    bool orderDescending = false,
    _i1.Transaction? transaction,
  }) async {
    return session.db.updateWhere<CodexTurnEvent>(
      columnValues: columnValues(CodexTurnEvent.t.updateTable),
      where: where(CodexTurnEvent.t),
      limit: limit,
      offset: offset,
      orderBy: orderBy?.call(CodexTurnEvent.t),
      orderByList: orderByList?.call(CodexTurnEvent.t),
      orderDescending: orderDescending,
      transaction: transaction,
    );
  }

  /// Deletes all [CodexTurnEvent]s in the list and returns the deleted rows.
  /// This is an atomic operation, meaning that if one of the rows fail to
  /// be deleted, none of the rows will be deleted.
  Future<List<CodexTurnEvent>> delete(
    _i1.DatabaseSession session,
    List<CodexTurnEvent> rows, {
    _i1.Transaction? transaction,
  }) async {
    return session.db.delete<CodexTurnEvent>(
      rows,
      transaction: transaction,
    );
  }

  /// Deletes a single [CodexTurnEvent].
  Future<CodexTurnEvent> deleteRow(
    _i1.DatabaseSession session,
    CodexTurnEvent row, {
    _i1.Transaction? transaction,
  }) async {
    return session.db.deleteRow<CodexTurnEvent>(
      row,
      transaction: transaction,
    );
  }

  /// Deletes all rows matching the [where] expression.
  Future<List<CodexTurnEvent>> deleteWhere(
    _i1.DatabaseSession session, {
    required _i1.WhereExpressionBuilder<CodexTurnEventTable> where,
    _i1.Transaction? transaction,
  }) async {
    return session.db.deleteWhere<CodexTurnEvent>(
      where: where(CodexTurnEvent.t),
      transaction: transaction,
    );
  }

  /// Counts the number of rows matching the [where] expression. If omitted,
  /// will return the count of all rows in the table.
  Future<int> count(
    _i1.DatabaseSession session, {
    _i1.WhereExpressionBuilder<CodexTurnEventTable>? where,
    int? limit,
    _i1.Transaction? transaction,
  }) async {
    return session.db.count<CodexTurnEvent>(
      where: where?.call(CodexTurnEvent.t),
      limit: limit,
      transaction: transaction,
    );
  }

  /// Acquires row-level locks on [CodexTurnEvent] rows matching the [where] expression.
  Future<void> lockRows(
    _i1.DatabaseSession session, {
    required _i1.WhereExpressionBuilder<CodexTurnEventTable> where,
    required _i1.LockMode lockMode,
    required _i1.Transaction transaction,
    _i1.LockBehavior lockBehavior = _i1.LockBehavior.wait,
  }) async {
    return session.db.lockRows<CodexTurnEvent>(
      where: where(CodexTurnEvent.t),
      lockMode: lockMode,
      lockBehavior: lockBehavior,
      transaction: transaction,
    );
  }
}
