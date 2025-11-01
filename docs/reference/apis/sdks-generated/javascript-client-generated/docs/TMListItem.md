# TmiClient.TMListItem

## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**id** | **String** | Unique identifier of the threat model (UUID) | 
**name** | **String** | Name of the threat model | 
**description** | **String** | Description of the threat model | [optional] 
**createdAt** | **Date** | Creation timestamp (RFC3339) | 
**modifiedAt** | **Date** | Last modification timestamp (RFC3339) | 
**owner** | **String** | Email address of the current owner | 
**createdBy** | **String** | Email address, name or identifier of the creator | 
**threatModelFramework** | **String** | The framework used for this threat model | 
**documentCount** | **Number** | Number of documents associated with this threat model | 
**repoCount** | **Number** | Number of source code repository entries associated with this threat model | 
**diagramCount** | **Number** | Number of diagrams associated with this threat model | 
**threatCount** | **Number** | Number of threats defined in this threat model | 
**issueUri** | **String** | URL to an issue in an issue tracking system | [optional] 
**assetCount** | **Number** | Number of assets associated with this threat model | 
**noteCount** | **Number** | Number of notes associated with this threat model | 
