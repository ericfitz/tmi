# ThreatModel

## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**id** | **str** | Unique identifier for the threat model (UUID) | [optional] 
**created_at** | **datetime** | Creation timestamp (RFC3339) | [optional] 
**modified_at** | **datetime** | Last modification timestamp (RFC3339) | [optional] 
**created_by** | **str** | User name, email or identifier of the creator of the threat model | [optional] 
**documents** | [**list[Document]**](Document.md) | List of documents related to the threat model | [optional] 
**repositories** | [**list[Repository]**](Repository.md) | List of source code repositories related to the threat model | [optional] 
**diagrams** | [**list[Diagram]**](Diagram.md) | List of diagram objects associated with this threat model | [optional] 
**threats** | [**list[Threat]**](Threat.md) | List of threats within the threat model | [optional] 
**notes** | [**list[Note]**](Note.md) | List of notes associated with the threat model | [optional] 
**assets** | [**list[ExtendedAsset]**](ExtendedAsset.md) | List of assets associated with the threat model | [optional] 
**status_updated** | **datetime** | Timestamp when the status field was last modified (RFC3339). Automatically updated by the server when status changes. | [optional] 

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)

