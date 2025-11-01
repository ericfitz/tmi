# BaseDiagram

## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**id** | [**UUID**](UUID.md) | Unique identifier for the diagram (UUID) | 
**name** | **String** | Name of the diagram | 
**type** | [**TypeEnum**](#TypeEnum) | Type of diagram with version | 
**createdAt** | [**OffsetDateTime**](OffsetDateTime.md) | Creation timestamp (ISO3339) | 
**modifiedAt** | [**OffsetDateTime**](OffsetDateTime.md) | Last modification timestamp (ISO3339) | 
**metadata** | [**List&lt;Metadata&gt;**](Metadata.md) | Key-value pairs for additional diagram metadata |  [optional]
**updateVector** | **Long** | Server-managed monotonic version counter, incremented on each diagram update |  [optional]
**image** | [**BaseDiagramImage**](BaseDiagramImage.md) |  |  [optional]
**description** | **String** | Optional description of the diagram |  [optional]

<a name="TypeEnum"></a>
## Enum: TypeEnum
Name | Value
---- | -----
DFD_1_0_0 | &quot;DFD-1.0.0&quot;
