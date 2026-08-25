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

/// A durable user or assistant message projected from a Codex turn.
abstract class CodexMessage
    implements _i1.TableRow<int?>, _i1.ProtocolSerialization {
  CodexMessage._({
    this.id,
    required this.codexThreadId,
    required this.role,
    required this.text,
    this.turnId,
    DateTime? createdAt,
  }) : createdAt = createdAt ?? DateTime.now();

  factory CodexMessage({
    int? id,
    required int codexThreadId,
    required String role,
    required String text,
    String? turnId,
    DateTime? createdAt,
  }) = _CodexMessageImpl;

  factory CodexMessage.fromJson(Map<String, dynamic> jsonSerialization) {
    return CodexMessage(
      id: jsonSerialization['id'] as int?,
      codexThreadId: jsonSerialization['codexThreadId'] as int,
      role: jsonSerialization['role'] as String,
      text: jsonSerialization['text'] as String,
      turnId: jsonSerialization['turnId'] as String?,
      createdAt: jsonSerialization['createdAt'] == null
          ? null
          : _i1.DateTimeJsonExtension.fromJson(jsonSerialization['createdAt']),
    );
  }

  static final t = CodexMessageTable();

  static const db = CodexMessageRepository._();

  @override
  int? id;

  int codexThreadId;

  String role;

  String text;

  String? turnId;

  DateTime createdAt;

  @override
  _i1.Table<int?> get table => t;

  /// Returns a shallow copy of this [CodexMessage]
  /// with some or all fields replaced by the given arguments.
  @_i1.useResult
  CodexMessage copyWith({
    int? id,
    int? codexThreadId,
    String? role,
    String? text,
    String? turnId,
    DateTime? createdAt,
  });
  @override
  Map<String, dynamic> toJson() {
    return {
      '__className__': 'CodexMessage',
      if (id != null) 'id': id,
      'codexThreadId': codexThreadId,
      'role': role,
      'text': text,
      if (turnId != null) 'turnId': turnId,
      'createdAt': createdAt.toJson(),
    };
  }

  @override
  Map<String, dynamic> toJsonForProtocol() {
    return {
      '__className__': 'CodexMessage',
      if (id != null) 'id': id,
      'codexThreadId': codexThreadId,
      'role': role,
      'text': text,
      if (turnId != null) 'turnId': turnId,
      'createdAt': createdAt.toJson(),
    };
  }

  static CodexMessageInclude include() {
    return CodexMessageInclude._();
  }

  static CodexMessageIncludeList includeList({
    _i1.WhereExpressionBuilder<CodexMessageTable>? where,
    int? limit,
    int? offset,
    _i1.OrderByBuilder<CodexMessageTable>? orderBy,
    bool orderDescending = false,
    _i1.OrderByListBuilder<CodexMessageTable>? orderByList,
    CodexMessageInclude? include,
  }) {
    return CodexMessageIncludeList._(
      where: where,
      limit: limit,
      offset: offset,
      orderBy: orderBy?.call(CodexMessage.t),
      orderDescending: orderDescending,
      orderByList: orderByList?.call(CodexMessage.t),
      include: include,
    );
  }

  @override
  String toString() {
    return _i1.SerializationManager.encode(this);
  }
}

class _Undefined {}

class _CodexMessageImpl extends CodexMessage {
  _CodexMessageImpl({
    int? id,
    required int codexThreadId,
    required String role,
    required String text,
    String? turnId,
    DateTime? createdAt,
  }) : super._(
         id: id,
         codexThreadId: codexThreadId,
         role: role,
         text: text,
         turnId: turnId,
         createdAt: createdAt,
       );

  /// Returns a shallow copy of this [CodexMessage]
  /// with some or all fields replaced by the given arguments.
  @_i1.useResult
  @override
  CodexMessage copyWith({
    Object? id = _Undefined,
    int? codexThreadId,
    String? role,
    String? text,
    Object? turnId = _Undefined,
    DateTime? createdAt,
  }) {
    return CodexMessage(
      id: id is int? ? id : this.id,
      codexThreadId: codexThreadId ?? this.codexThreadId,
      role: role ?? this.role,
      text: text ?? this.text,
      turnId: turnId is String? ? turnId : this.turnId,
      createdAt: createdAt ?? this.createdAt,
    );
  }
}

class CodexMessageUpdateTable extends _i1.UpdateTable<CodexMessageTable> {
  CodexMessageUpdateTable(super.table);

  _i1.ColumnValue<int, int> codexThreadId(int value) => _i1.ColumnValue(
    table.codexThreadId,
    value,
  );

  _i1.ColumnValue<String, String> role(String value) => _i1.ColumnValue(
    table.role,
    value,
  );

  _i1.ColumnValue<String, String> text(String value) => _i1.ColumnValue(
    table.text,
    value,
  );

  _i1.ColumnValue<String, String> turnId(String? value) => _i1.ColumnValue(
    table.turnId,
    value,
  );

  _i1.ColumnValue<DateTime, DateTime> createdAt(DateTime value) =>
      _i1.ColumnValue(
        table.createdAt,
        value,
      );
}

class CodexMessageTable extends _i1.Table<int?> {
  CodexMessageTable({super.tableRelation}) : super(tableName: 'codex_message') {
    updateTable = CodexMessageUpdateTable(this);
    codexThreadId = _i1.ColumnInt(
      'codexThreadId',
      this,
    );
    role = _i1.ColumnString(
      'role',
      this,
    );
    text = _i1.ColumnString(
      'text',
      this,
    );
    turnId = _i1.ColumnString(
      'turnId',
      this,
    );
    createdAt = _i1.ColumnDateTime(
      'createdAt',
      this,
      hasDefault: true,
    );
  }

  late final CodexMessageUpdateTable updateTable;

  late final _i1.ColumnInt codexThreadId;

  late final _i1.ColumnString role;

  late final _i1.ColumnString text;

  late final _i1.ColumnString turnId;

  late final _i1.ColumnDateTime createdAt;

  @override
  List<_i1.Column> get columns => [
    id,
    codexThreadId,
    role,
    text,
    turnId,
    createdAt,
  ];
}

class CodexMessageInclude extends _i1.IncludeObject {
  CodexMessageInclude._();

  @override
  Map<String, _i1.Include?> get includes => {};

  @override
  _i1.Table<int?> get table => CodexMessage.t;
}

class CodexMessageIncludeList extends _i1.IncludeList {
  CodexMessageIncludeList._({
    _i1.WhereExpressionBuilder<CodexMessageTable>? where,
    super.limit,
    super.offset,
    super.orderBy,
    super.orderDescending,
    super.orderByList,
    super.include,
  }) {
    super.where = where?.call(CodexMessage.t);
  }

  @override
  Map<String, _i1.Include?> get includes => include?.includes ?? {};

  @override
  _i1.Table<int?> get table => CodexMessage.t;
}

class CodexMessageRepository {
  const CodexMessageRepository._();

  /// Returns a list of [CodexMessage]s matching the given query parameters.
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
  Future<List<CodexMessage>> find(
    _i1.DatabaseSession session, {
    _i1.WhereExpressionBuilder<CodexMessageTable>? where,
    int? limit,
    int? offset,
    _i1.OrderByBuilder<CodexMessageTable>? orderBy,
    bool orderDescending = false,
    _i1.OrderByListBuilder<CodexMessageTable>? orderByList,
    _i1.Transaction? transaction,
    _i1.LockMode? lockMode,
    _i1.LockBehavior? lockBehavior,
  }) async {
    return session.db.find<CodexMessage>(
      where: where?.call(CodexMessage.t),
      orderBy: orderBy?.call(CodexMessage.t),
      orderByList: orderByList?.call(CodexMessage.t),
      orderDescending: orderDescending,
      limit: limit,
      offset: offset,
      transaction: transaction,
      lockMode: lockMode,
      lockBehavior: lockBehavior,
    );
  }

  /// Returns the first matching [CodexMessage] matching the given query parameters.
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
  Future<CodexMessage?> findFirstRow(
    _i1.DatabaseSession session, {
    _i1.WhereExpressionBuilder<CodexMessageTable>? where,
    int? offset,
    _i1.OrderByBuilder<CodexMessageTable>? orderBy,
    bool orderDescending = false,
    _i1.OrderByListBuilder<CodexMessageTable>? orderByList,
    _i1.Transaction? transaction,
    _i1.LockMode? lockMode,
    _i1.LockBehavior? lockBehavior,
  }) async {
    return session.db.findFirstRow<CodexMessage>(
      where: where?.call(CodexMessage.t),
      orderBy: orderBy?.call(CodexMessage.t),
      orderByList: orderByList?.call(CodexMessage.t),
      orderDescending: orderDescending,
      offset: offset,
      transaction: transaction,
      lockMode: lockMode,
      lockBehavior: lockBehavior,
    );
  }

  /// Finds a single [CodexMessage] by its [id] or null if no such row exists.
  Future<CodexMessage?> findById(
    _i1.DatabaseSession session,
    int id, {
    _i1.Transaction? transaction,
    _i1.LockMode? lockMode,
    _i1.LockBehavior? lockBehavior,
  }) async {
    return session.db.findById<CodexMessage>(
      id,
      transaction: transaction,
      lockMode: lockMode,
      lockBehavior: lockBehavior,
    );
  }

  /// Inserts all [CodexMessage]s in the list and returns the inserted rows.
  ///
  /// The returned [CodexMessage]s will have their `id` fields set.
  ///
  /// This is an atomic operation, meaning that if one of the rows fails to
  /// insert, none of the rows will be inserted.
  ///
  /// If [ignoreConflicts] is set to `true`, rows that conflict with existing
  /// rows are silently skipped, and only the successfully inserted rows are
  /// returned.
  Future<List<CodexMessage>> insert(
    _i1.DatabaseSession session,
    List<CodexMessage> rows, {
    _i1.Transaction? transaction,
    bool ignoreConflicts = false,
  }) async {
    return session.db.insert<CodexMessage>(
      rows,
      transaction: transaction,
      ignoreConflicts: ignoreConflicts,
    );
  }

  /// Inserts a single [CodexMessage] and returns the inserted row.
  ///
  /// The returned [CodexMessage] will have its `id` field set.
  Future<CodexMessage> insertRow(
    _i1.DatabaseSession session,
    CodexMessage row, {
    _i1.Transaction? transaction,
  }) async {
    return session.db.insertRow<CodexMessage>(
      row,
      transaction: transaction,
    );
  }

  /// Updates all [CodexMessage]s in the list and returns the updated rows. If
  /// [columns] is provided, only those columns will be updated. Defaults to
  /// all columns.
  /// This is an atomic operation, meaning that if one of the rows fails to
  /// update, none of the rows will be updated.
  Future<List<CodexMessage>> update(
    _i1.DatabaseSession session,
    List<CodexMessage> rows, {
    _i1.ColumnSelections<CodexMessageTable>? columns,
    _i1.Transaction? transaction,
  }) async {
    return session.db.update<CodexMessage>(
      rows,
      columns: columns?.call(CodexMessage.t),
      transaction: transaction,
    );
  }

  /// Updates a single [CodexMessage]. The row needs to have its id set.
  /// Optionally, a list of [columns] can be provided to only update those
  /// columns. Defaults to all columns.
  Future<CodexMessage> updateRow(
    _i1.DatabaseSession session,
    CodexMessage row, {
    _i1.ColumnSelections<CodexMessageTable>? columns,
    _i1.Transaction? transaction,
  }) async {
    return session.db.updateRow<CodexMessage>(
      row,
      columns: columns?.call(CodexMessage.t),
      transaction: transaction,
    );
  }

  /// Updates a single [CodexMessage] by its [id] with the specified [columnValues].
  /// Returns the updated row or null if no row with the given id exists.
  Future<CodexMessage?> updateById(
    _i1.DatabaseSession session,
    int id, {
    required _i1.ColumnValueListBuilder<CodexMessageUpdateTable> columnValues,
    _i1.Transaction? transaction,
  }) async {
    return session.db.updateById<CodexMessage>(
      id,
      columnValues: columnValues(CodexMessage.t.updateTable),
      transaction: transaction,
    );
  }

  /// Updates all [CodexMessage]s matching the [where] expression with the specified [columnValues].
  /// Returns the list of updated rows.
  Future<List<CodexMessage>> updateWhere(
    _i1.DatabaseSession session, {
    required _i1.ColumnValueListBuilder<CodexMessageUpdateTable> columnValues,
    required _i1.WhereExpressionBuilder<CodexMessageTable> where,
    int? limit,
    int? offset,
    _i1.OrderByBuilder<CodexMessageTable>? orderBy,
    _i1.OrderByListBuilder<CodexMessageTable>? orderByList,
    bool orderDescending = false,
    _i1.Transaction? transaction,
  }) async {
    return session.db.updateWhere<CodexMessage>(
      columnValues: columnValues(CodexMessage.t.updateTable),
      where: where(CodexMessage.t),
      limit: limit,
      offset: offset,
      orderBy: orderBy?.call(CodexMessage.t),
      orderByList: orderByList?.call(CodexMessage.t),
      orderDescending: orderDescending,
      transaction: transaction,
    );
  }

  /// Deletes all [CodexMessage]s in the list and returns the deleted rows.
  /// This is an atomic operation, meaning that if one of the rows fail to
  /// be deleted, none of the rows will be deleted.
  Future<List<CodexMessage>> delete(
    _i1.DatabaseSession session,
    List<CodexMessage> rows, {
    _i1.Transaction? transaction,
  }) async {
    return session.db.delete<CodexMessage>(
      rows,
      transaction: transaction,
    );
  }

  /// Deletes a single [CodexMessage].
  Future<CodexMessage> deleteRow(
    _i1.DatabaseSession session,
    CodexMessage row, {
    _i1.Transaction? transaction,
  }) async {
    return session.db.deleteRow<CodexMessage>(
      row,
      transaction: transaction,
    );
  }

  /// Deletes all rows matching the [where] expression.
  Future<List<CodexMessage>> deleteWhere(
    _i1.DatabaseSession session, {
    required _i1.WhereExpressionBuilder<CodexMessageTable> where,
    _i1.Transaction? transaction,
  }) async {
    return session.db.deleteWhere<CodexMessage>(
      where: where(CodexMessage.t),
      transaction: transaction,
    );
  }

  /// Counts the number of rows matching the [where] expression. If omitted,
  /// will return the count of all rows in the table.
  Future<int> count(
    _i1.DatabaseSession session, {
    _i1.WhereExpressionBuilder<CodexMessageTable>? where,
    int? limit,
    _i1.Transaction? transaction,
  }) async {
    return session.db.count<CodexMessage>(
      where: where?.call(CodexMessage.t),
      limit: limit,
      transaction: transaction,
    );
  }

  /// Acquires row-level locks on [CodexMessage] rows matching the [where] expression.
  Future<void> lockRows(
    _i1.DatabaseSession session, {
    required _i1.WhereExpressionBuilder<CodexMessageTable> where,
    required _i1.LockMode lockMode,
    required _i1.Transaction transaction,
    _i1.LockBehavior lockBehavior = _i1.LockBehavior.wait,
  }) async {
    return session.db.lockRows<CodexMessage>(
      where: where(CodexMessage.t),
      lockMode: lockMode,
      lockBehavior: lockBehavior,
      transaction: transaction,
    );
  }
}
