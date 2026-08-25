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
import 'package:serverpod_client/serverpod_client.dart' as _i1;

/// A client order submitted to the autonomous delivery pipeline.
abstract class Order implements _i1.SerializableModel {
  Order._({
    this.id,
    required this.clientId,
    required this.projectDescription,
    required this.budgetCents,
    required this.status,
    this.assignedProjectId,
    required this.createdAt,
    required this.updatedAt,
    required this.title,
    required this.description,
  });

  factory Order({
    int? id,
    required _i1.UuidValue clientId,
    required String projectDescription,
    required int budgetCents,
    required String status,
    int? assignedProjectId,
    required DateTime createdAt,
    required DateTime updatedAt,
    required String title,
    required String description,
  }) = _OrderImpl;

  factory Order.fromJson(Map<String, dynamic> jsonSerialization) {
    return Order(
      id: jsonSerialization['id'] as int?,
      clientId: _i1.UuidValueJsonExtension.fromJson(
        jsonSerialization['clientId'],
      ),
      projectDescription: jsonSerialization['projectDescription'] as String,
      budgetCents: jsonSerialization['budgetCents'] as int,
      status: jsonSerialization['status'] as String,
      assignedProjectId: jsonSerialization['assignedProjectId'] as int?,
      createdAt: _i1.DateTimeJsonExtension.fromJson(
        jsonSerialization['createdAt'],
      ),
      updatedAt: _i1.DateTimeJsonExtension.fromJson(
        jsonSerialization['updatedAt'],
      ),
      title: jsonSerialization['title'] as String,
      description: jsonSerialization['description'] as String,
    );
  }

  /// The database id, set if the object has been inserted into the
  /// database or if it has been fetched from the database. Otherwise,
  /// the id will be null.
  int? id;

  _i1.UuidValue clientId;

  String projectDescription;

  int budgetCents;

  String status;

  int? assignedProjectId;

  DateTime createdAt;

  DateTime updatedAt;

  String title;

  String description;

  /// Returns a shallow copy of this [Order]
  /// with some or all fields replaced by the given arguments.
  @_i1.useResult
  Order copyWith({
    int? id,
    _i1.UuidValue? clientId,
    String? projectDescription,
    int? budgetCents,
    String? status,
    int? assignedProjectId,
    DateTime? createdAt,
    DateTime? updatedAt,
    String? title,
    String? description,
  });
  @override
  Map<String, dynamic> toJson() {
    return {
      '__className__': 'Order',
      if (id != null) 'id': id,
      'clientId': clientId.toJson(),
      'projectDescription': projectDescription,
      'budgetCents': budgetCents,
      'status': status,
      if (assignedProjectId != null) 'assignedProjectId': assignedProjectId,
      'createdAt': createdAt.toJson(),
      'updatedAt': updatedAt.toJson(),
      'title': title,
      'description': description,
    };
  }

  @override
  String toString() {
    return _i1.SerializationManager.encode(this);
  }
}

class _Undefined {}

class _OrderImpl extends Order {
  _OrderImpl({
    int? id,
    required _i1.UuidValue clientId,
    required String projectDescription,
    required int budgetCents,
    required String status,
    int? assignedProjectId,
    required DateTime createdAt,
    required DateTime updatedAt,
    required String title,
    required String description,
  }) : super._(
         id: id,
         clientId: clientId,
         projectDescription: projectDescription,
         budgetCents: budgetCents,
         status: status,
         assignedProjectId: assignedProjectId,
         createdAt: createdAt,
         updatedAt: updatedAt,
         title: title,
         description: description,
       );

  /// Returns a shallow copy of this [Order]
  /// with some or all fields replaced by the given arguments.
  @_i1.useResult
  @override
  Order copyWith({
    Object? id = _Undefined,
    _i1.UuidValue? clientId,
    String? projectDescription,
    int? budgetCents,
    String? status,
    Object? assignedProjectId = _Undefined,
    DateTime? createdAt,
    DateTime? updatedAt,
    String? title,
    String? description,
  }) {
    return Order(
      id: id is int? ? id : this.id,
      clientId: clientId ?? this.clientId,
      projectDescription: projectDescription ?? this.projectDescription,
      budgetCents: budgetCents ?? this.budgetCents,
      status: status ?? this.status,
      assignedProjectId: assignedProjectId is int?
          ? assignedProjectId
          : this.assignedProjectId,
      createdAt: createdAt ?? this.createdAt,
      updatedAt: updatedAt ?? this.updatedAt,
      title: title ?? this.title,
      description: description ?? this.description,
    );
  }
}
