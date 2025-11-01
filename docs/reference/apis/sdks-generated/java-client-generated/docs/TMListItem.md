# TMListItem

## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**id** | [**UUID**](UUID.md) | Unique identifier of the threat model (UUID) | 
**name** | **String** | Name of the threat model | 
**description** | **String** | Description of the threat model |  [optional]
**createdAt** | [**OffsetDateTime**](OffsetDateTime.md) | Creation timestamp (RFC3339) | 
**modifiedAt** | [**OffsetDateTime**](OffsetDateTime.md) | Last modification timestamp (RFC3339) | 
**owner** | **String** | Email address of the current owner | 
**createdBy** | **String** | Email address, name or identifier of the creator | 
**threatModelFramework** | **String** | The framework used for this threat model | 
**documentCount** | **Integer** | Number of documents associated with this threat model | 
**repoCount** | **Integer** | Number of source code repository entries associated with this threat model | 
**diagramCount** | **Integer** | Number of diagrams associated with this threat model | 
**threatCount** | **Integer** | Number of threats defined in this threat model | 
**issueUri** | **String** | URL to an issue in an issue tracking system |  [optional]
**assetCount** | **Integer** | Number of assets associated with this threat model | 
**noteCount** | **Integer** | Number of notes associated with this threat model | 
