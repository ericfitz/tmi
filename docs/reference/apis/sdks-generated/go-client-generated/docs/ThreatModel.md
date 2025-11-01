# ThreatModel

## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Id** | **string** | Unique identifier for the threat model (UUID) | [optional] [default to null]
**CreatedAt** | [**time.Time**](time.Time.md) | Creation timestamp (RFC3339) | [optional] [default to null]
**ModifiedAt** | [**time.Time**](time.Time.md) | Last modification timestamp (RFC3339) | [optional] [default to null]
**CreatedBy** | **string** | User name, email or identifier of the creator of the threat model | [optional] [default to null]
**Documents** | [**[]Document**](Document.md) | List of documents related to the threat model | [optional] [default to null]
**Repositories** | [**[]Repository**](Repository.md) | List of source code repositories related to the threat model | [optional] [default to null]
**Diagrams** | [**[]Diagram**](Diagram.md) | List of diagram objects associated with this threat model | [optional] [default to null]
**Threats** | [**[]Threat**](Threat.md) | List of threats within the threat model | [optional] [default to null]
**Notes** | [**[]Note**](Note.md) | List of notes associated with the threat model | [optional] [default to null]
**Assets** | [**[]ExtendedAsset**](ExtendedAsset.md) | List of assets associated with the threat model | [optional] [default to null]
**StatusUpdated** | [**time.Time**](time.Time.md) | Timestamp when the status field was last modified (RFC3339). Automatically updated by the server when status changes. | [optional] [default to null]
**Name** | **string** | Name of the threat model | [default to null]
**Description** | **string** | Description of the threat model | [optional] [default to null]
**Owner** | **string** | Email address of the current owner | [default to null]
**ThreatModelFramework** | **string** | The framework used for this threat model | [default to null]
**Authorization** | [**[]Authorization**](Authorization.md) | List of users and their roles for this threat model | [default to null]
**Metadata** | [**[]Metadata**](Metadata.md) | Key-value pairs for additional threat model metadata | [optional] [default to null]
**IssueUri** | **string** | URL to an issue in an issue tracking system for this threat model | [optional] [default to null]
**Status** | **[]string** | Status of the threat model in the organization&#x27;s threat modeling or SDLC process. Examples: \&quot;Not started\&quot;, \&quot;In progress\&quot;, \&quot;Review\&quot;, \&quot;Approved\&quot;, \&quot;Closed\&quot; | [optional] [default to null]

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)

