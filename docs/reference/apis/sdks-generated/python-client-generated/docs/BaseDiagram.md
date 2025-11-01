# BaseDiagram

## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**id** | **str** | Unique identifier for the diagram (UUID) | 
**name** | **str** | Name of the diagram | 
**type** | **str** | Type of diagram with version | 
**created_at** | **datetime** | Creation timestamp (ISO3339) | 
**modified_at** | **datetime** | Last modification timestamp (ISO3339) | 
**metadata** | [**list[Metadata]**](Metadata.md) | Key-value pairs for additional diagram metadata | [optional] 
**update_vector** | **int** | Server-managed monotonic version counter, incremented on each diagram update | [optional] 
**image** | [**BaseDiagramImage**](BaseDiagramImage.md) |  | [optional] 
**description** | **str** | Optional description of the diagram | [optional] 

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)

