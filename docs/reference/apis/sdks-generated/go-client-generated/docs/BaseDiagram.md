# BaseDiagram

## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Id** | **string** | Unique identifier for the diagram (UUID) | [default to null]
**Name** | **string** | Name of the diagram | [default to null]
**Type_** | **string** | Type of diagram with version | [default to null]
**CreatedAt** | [**time.Time**](time.Time.md) | Creation timestamp (ISO3339) | [default to null]
**ModifiedAt** | [**time.Time**](time.Time.md) | Last modification timestamp (ISO3339) | [default to null]
**Metadata** | [**[]Metadata**](Metadata.md) | Key-value pairs for additional diagram metadata | [optional] [default to null]
**UpdateVector** | **int64** | Server-managed monotonic version counter, incremented on each diagram update | [optional] [default to null]
**Image** | [***BaseDiagramImage**](BaseDiagram_image.md) |  | [optional] [default to null]
**Description** | **string** | Optional description of the diagram | [optional] [default to null]

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)

