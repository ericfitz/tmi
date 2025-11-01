# ThreatModelBase

## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**name** | **str** | Name of the threat model | 
**description** | **str** | Description of the threat model | [optional] 
**owner** | **str** | Email address of the current owner | 
**threat_model_framework** | **str** | The framework used for this threat model | 
**authorization** | [**list[Authorization]**](Authorization.md) | List of users and their roles for this threat model | 
**metadata** | [**list[Metadata]**](Metadata.md) | Key-value pairs for additional threat model metadata | [optional] 
**issue_uri** | **str** | URL to an issue in an issue tracking system for this threat model | [optional] 
**status** | **list[str]** | Status of the threat model in the organization&#x27;s threat modeling or SDLC process. Examples: \&quot;Not started\&quot;, \&quot;In progress\&quot;, \&quot;Review\&quot;, \&quot;Approved\&quot;, \&quot;Closed\&quot; | [optional] 

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)

