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
import 'package:fixer_dashboard_client/src/protocol/protocol.dart' as _i2;

/// A client-submitted revision of an order.
abstract class Revision implements _i1.SerializableModel {
  Revision._({
    this.id,
    required this.orderId,
    required this.revisionNumber,
    required this.revisionText,
    this.attachmentUrls,
    this.resultSummary,
    required this.status,
    required this.description,
    this.branchName,
    this.previewUrl,
    required this.createdAt,
    required this.updatedAt,
  });

  factory Revision({
    int? id,
    required int orderId,
    required int revisionNumber,
    required String revisionText,
    List<String>? attachmentUrls,
    String? resultSummary,
    required String status,
    required String description,
    String? branchName,
    String? previewUrl,
    required DateTime createdAt,
    required DateTime updatedAt,
  }) = _RevisionImpl;

  factory Revision.fromJson(Map<String, dynamic> jsonSerialization) {
    return Revision(
      id: jsonSerialization['id'] as int?,
      orderId: jsonSerialization['orderId'] as int,
      revisionNumber: jsonSerialization['revisionNumber'] as int,
      revisionText: jsonSerialization['revisionText'] as String,
      attachmentUrls: jsonSerialization['attachmentUrls'] == null
          ? null
          : _i2.Protocol().deserialize<List<String>>(
              jsonSerialization['attachmentUrls'],
            ),
      resultSummary: jsonSerialization['resultSummary'] as String?,
      status: jsonSerialization['status'] as String,
      description: jsonSerialization['description'] as String,
      branchName: jsonSerialization['branchName'] as String?,
      previewUrl: jsonSerialization['previewUrl'] as String?,
      createdAt: _i1.DateTimeJsonExtension.fromJson(
        jsonSerialization['createdAt'],
      ),
      updatedAt: _i1.DateTimeJsonExtension.fromJson(
        jsonSerialization['updatedAt'],
      ),
    );
  }

  /// The database id, set if the object has been inserted into the
  /// database or if it has been fetched from the database. Otherwise,
  /// the id will be null.
  int? id;

  int orderId;

  int revisionNumber;

  String revisionText;

  List<String>? attachmentUrls;

  String? resultSummary;

  String status;

  String description;

  String? branchName;

  String? previewUrl;

  DateTime createdAt;

  DateTime updatedAt;

  /// Returns a shallow copy of this [Revision]
  /// with some or all fields replaced by the given arguments.
  @_i1.useResult
  Revision copyWith({
    int? id,
    int? orderId,
    int? revisionNumber,
    String? revisionText,
    List<String>? attachmentUrls,
    String? resultSummary,
    String? status,
    String? description,
    String? branchName,
    String? previewUrl,
    DateTime? createdAt,
    DateTime? updatedAt,
  });
  @override
  Map<String, dynamic> toJson() {
    return {
      '__className__': 'Revision',
      if (id != null) 'id': id,
      'orderId': orderId,
      'revisionNumber': revisionNumber,
      'revisionText': revisionText,
      if (attachmentUrls != null) 'attachmentUrls': attachmentUrls?.toJson(),
      if (resultSummary != null) 'resultSummary': resultSummary,
      'status': status,
      'description': description,
      if (branchName != null) 'branchName': branchName,
      if (previewUrl != null) 'previewUrl': previewUrl,
      'createdAt': createdAt.toJson(),
      'updatedAt': updatedAt.toJson(),
    };
  }

  @override
  String toString() {
    return _i1.SerializationManager.encode(this);
  }
}

class _Undefined {}

class _RevisionImpl extends Revision {
  _RevisionImpl({
    int? id,
    required int orderId,
    required int revisionNumber,
    required String revisionText,
    List<String>? attachmentUrls,
    String? resultSummary,
    required String status,
    required String description,
    String? branchName,
    String? previewUrl,
    required DateTime createdAt,
    required DateTime updatedAt,
  }) : super._(
         id: id,
         orderId: orderId,
         revisionNumber: revisionNumber,
         revisionText: revisionText,
         attachmentUrls: attachmentUrls,
         resultSummary: resultSummary,
         status: status,
         description: description,
         branchName: branchName,
         previewUrl: previewUrl,
         createdAt: createdAt,
         updatedAt: updatedAt,
       );

  /// Returns a shallow copy of this [Revision]
  /// with some or all fields replaced by the given arguments.
  @_i1.useResult
  @override
  Revision copyWith({
    Object? id = _Undefined,
    int? orderId,
    int? revisionNumber,
    String? revisionText,
    Object? attachmentUrls = _Undefined,
    Object? resultSummary = _Undefined,
    String? status,
    String? description,
    Object? branchName = _Undefined,
    Object? previewUrl = _Undefined,
    DateTime? createdAt,
    DateTime? updatedAt,
  }) {
    return Revision(
      id: id is int? ? id : this.id,
      orderId: orderId ?? this.orderId,
      revisionNumber: revisionNumber ?? this.revisionNumber,
      revisionText: revisionText ?? this.revisionText,
      attachmentUrls: attachmentUrls is List<String>?
          ? attachmentUrls
          : this.attachmentUrls?.map((e0) => e0).toList(),
      resultSummary: resultSummary is String?
          ? resultSummary
          : this.resultSummary,
      status: status ?? this.status,
      description: description ?? this.description,
      branchName: branchName is String? ? branchName : this.branchName,
      previewUrl: previewUrl is String? ? previewUrl : this.previewUrl,
      createdAt: createdAt ?? this.createdAt,
      updatedAt: updatedAt ?? this.updatedAt,
    );
  }
}
