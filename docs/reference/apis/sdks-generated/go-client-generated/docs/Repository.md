# Repository

## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Id** | **string** | Unique identifier for the repository | [default to null]
**Metadata** | [**[]Metadata**](Metadata.md) | Optional metadata key-value pairs | [optional] [default to null]
**CreatedAt** | [**time.Time**](time.Time.md) | Creation timestamp (RFC3339) | [optional] [default to null]
**ModifiedAt** | [**time.Time**](time.Time.md) | Last modification timestamp (RFC3339) | [optional] [default to null]
**Name** | **string** | Name for the source code reference | [optional] [default to null]
**Description** | **string** | Description of the referenced source code | [optional] [default to null]
**Type_** | **string** | Source code repository type | [optional] [default to null]
**Parameters** | [***RepositoryBaseParameters**](RepositoryBase_parameters.md) |  | [optional] [default to null]
**Uri** | **string** | URL to retrieve the referenced source code | [default to null]

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)

