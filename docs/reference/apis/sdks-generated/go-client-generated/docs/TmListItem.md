# TmListItem

## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Id** | **string** | Unique identifier of the threat model (UUID) | [default to null]
**Name** | **string** | Name of the threat model | [default to null]
**Description** | **string** | Description of the threat model | [optional] [default to null]
**CreatedAt** | [**time.Time**](time.Time.md) | Creation timestamp (RFC3339) | [default to null]
**ModifiedAt** | [**time.Time**](time.Time.md) | Last modification timestamp (RFC3339) | [default to null]
**Owner** | **string** | Email address of the current owner | [default to null]
**CreatedBy** | **string** | Email address, name or identifier of the creator | [default to null]
**ThreatModelFramework** | **string** | The framework used for this threat model | [default to null]
**DocumentCount** | **int32** | Number of documents associated with this threat model | [default to null]
**RepoCount** | **int32** | Number of source code repository entries associated with this threat model | [default to null]
**DiagramCount** | **int32** | Number of diagrams associated with this threat model | [default to null]
**ThreatCount** | **int32** | Number of threats defined in this threat model | [default to null]
**IssueUri** | **string** | URL to an issue in an issue tracking system | [optional] [default to null]
**AssetCount** | **int32** | Number of assets associated with this threat model | [default to null]
**NoteCount** | **int32** | Number of notes associated with this threat model | [default to null]

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)

