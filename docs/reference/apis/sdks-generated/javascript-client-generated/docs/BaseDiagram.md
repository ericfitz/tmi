# TmiClient.BaseDiagram

## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**id** | **String** | Unique identifier for the diagram (UUID) | 
**name** | **String** | Name of the diagram | 
**type** | **String** | Type of diagram with version | 
**createdAt** | **Date** | Creation timestamp (ISO3339) | 
**modifiedAt** | **Date** | Last modification timestamp (ISO3339) | 
**metadata** | [**[Metadata]**](Metadata.md) | Key-value pairs for additional diagram metadata | [optional] 
**updateVector** | **Number** | Server-managed monotonic version counter, incremented on each diagram update | [optional] 
**image** | [**BaseDiagramImage**](BaseDiagramImage.md) |  | [optional] 
**description** | **String** | Optional description of the diagram | [optional] 

<a name="TypeEnum"></a>
## Enum: TypeEnum

* `dFD100` (value: `"DFD-1.0.0"`)

