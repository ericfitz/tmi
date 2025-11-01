# ThreatModelBase

## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Name** | **string** | Name of the threat model | [default to null]
**Description** | **string** | Description of the threat model | [optional] [default to null]
**Owner** | **string** | Email address of the current owner | [default to null]
**ThreatModelFramework** | **string** | The framework used for this threat model | [default to null]
**Authorization** | [**[]Authorization**](Authorization.md) | List of users and their roles for this threat model | [default to null]
**Metadata** | [**[]Metadata**](Metadata.md) | Key-value pairs for additional threat model metadata | [optional] [default to null]
**IssueUri** | **string** | URL to an issue in an issue tracking system for this threat model | [optional] [default to null]
**Status** | **[]string** | Status of the threat model in the organization&#x27;s threat modeling or SDLC process. Examples: \&quot;Not started\&quot;, \&quot;In progress\&quot;, \&quot;Review\&quot;, \&quot;Approved\&quot;, \&quot;Closed\&quot; | [optional] [default to null]

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)

