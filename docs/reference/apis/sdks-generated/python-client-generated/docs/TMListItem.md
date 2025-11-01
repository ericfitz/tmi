# TMListItem

## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**id** | **str** | Unique identifier of the threat model (UUID) | 
**name** | **str** | Name of the threat model | 
**description** | **str** | Description of the threat model | [optional] 
**created_at** | **datetime** | Creation timestamp (RFC3339) | 
**modified_at** | **datetime** | Last modification timestamp (RFC3339) | 
**owner** | **str** | Email address of the current owner | 
**created_by** | **str** | Email address, name or identifier of the creator | 
**threat_model_framework** | **str** | The framework used for this threat model | 
**document_count** | **int** | Number of documents associated with this threat model | 
**repo_count** | **int** | Number of source code repository entries associated with this threat model | 
**diagram_count** | **int** | Number of diagrams associated with this threat model | 
**threat_count** | **int** | Number of threats defined in this threat model | 
**issue_uri** | **str** | URL to an issue in an issue tracking system | [optional] 
**asset_count** | **int** | Number of assets associated with this threat model | 
**note_count** | **int** | Number of notes associated with this threat model | 

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)

