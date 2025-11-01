# TmiClient.ThreatBase

## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**name** | **String** | Name of the threat | 
**description** | **String** | Description of the threat and risk to the organization | [optional] 
**mitigation** | **String** | Recommended or planned mitigation(s) for the threat | [optional] 
**diagramId** | **String** | Unique identifier of the associated diagram (if applicable) (UUID) | [optional] 
**cellId** | **String** | Unique identifier of the associated cell (if applicable) (UUID) | [optional] 
**severity** | **String** | Severity level of the threat | 
**score** | **Number** | Numeric score representing the risk or impact of the threat | [optional] 
**priority** | **String** | Priority level for addressing the threat | 
**mitigated** | **Boolean** | Whether the threat has been mitigated | 
**status** | **String** | Current status of the threat | 
**threatType** | **String** | Type or category of the threat | 
**metadata** | [**[Metadata]**](Metadata.md) | Key-value pairs for additional threat metadata | [optional] 
**issueUri** | **String** | URL to an issue in an issue tracking system for this threat | [optional] 
**assetId** | **String** | Unique identifier of the associated asset (if applicable) (UUID) | [optional] 

<a name="SeverityEnum"></a>
## Enum: SeverityEnum

* `unknown` (value: `"Unknown"`)
* `none` (value: `"None"`)
* `low` (value: `"Low"`)
* `medium` (value: `"Medium"`)
* `high` (value: `"High"`)
* `critical` (value: `"Critical"`)

