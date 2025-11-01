# ThreatInput

## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Name** | **string** | Name of the threat | [default to null]
**Description** | **string** | Description of the threat and risk to the organization | [optional] [default to null]
**Mitigation** | **string** | Recommended or planned mitigation(s) for the threat | [optional] [default to null]
**DiagramId** | **string** | Unique identifier of the associated diagram (if applicable) (UUID) | [optional] [default to null]
**CellId** | **string** | Unique identifier of the associated cell (if applicable) (UUID) | [optional] [default to null]
**Severity** | **string** | Severity level of the threat | [default to null]
**Score** | **float64** | Numeric score representing the risk or impact of the threat | [optional] [default to null]
**Priority** | **string** | Priority level for addressing the threat | [default to null]
**Mitigated** | **bool** | Whether the threat has been mitigated | [default to null]
**Status** | **string** | Current status of the threat | [default to null]
**ThreatType** | **string** | Type or category of the threat | [default to null]
**Metadata** | [**[]Metadata**](Metadata.md) | Key-value pairs for additional threat metadata | [optional] [default to null]
**IssueUri** | **string** | URL to an issue in an issue tracking system for this threat | [optional] [default to null]
**AssetId** | **string** | Unique identifier of the associated asset (if applicable) (UUID) | [optional] [default to null]

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)

