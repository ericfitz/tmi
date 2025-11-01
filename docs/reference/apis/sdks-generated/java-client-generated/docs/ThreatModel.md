# ThreatModel

## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**id** | [**UUID**](UUID.md) | Unique identifier for the threat model (UUID) |  [optional]
**createdAt** | [**OffsetDateTime**](OffsetDateTime.md) | Creation timestamp (RFC3339) |  [optional]
**modifiedAt** | [**OffsetDateTime**](OffsetDateTime.md) | Last modification timestamp (RFC3339) |  [optional]
**createdBy** | **String** | User name, email or identifier of the creator of the threat model |  [optional]
**documents** | [**List&lt;Document&gt;**](Document.md) | List of documents related to the threat model |  [optional]
**repositories** | [**List&lt;Repository&gt;**](Repository.md) | List of source code repositories related to the threat model |  [optional]
**diagrams** | [**List&lt;Diagram&gt;**](Diagram.md) | List of diagram objects associated with this threat model |  [optional]
**threats** | [**List&lt;Threat&gt;**](Threat.md) | List of threats within the threat model |  [optional]
**notes** | [**List&lt;Note&gt;**](Note.md) | List of notes associated with the threat model |  [optional]
**assets** | [**List&lt;ExtendedAsset&gt;**](ExtendedAsset.md) | List of assets associated with the threat model |  [optional]
**statusUpdated** | [**OffsetDateTime**](OffsetDateTime.md) | Timestamp when the status field was last modified (RFC3339). Automatically updated by the server when status changes. |  [optional]
