# ThreatModelBase

## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**name** | **String** | Name of the threat model | 
**description** | **String** | Description of the threat model |  [optional]
**owner** | **String** | Email address of the current owner | 
**threatModelFramework** | **String** | The framework used for this threat model | 
**authorization** | [**List&lt;Authorization&gt;**](Authorization.md) | List of users and their roles for this threat model | 
**metadata** | [**List&lt;Metadata&gt;**](Metadata.md) | Key-value pairs for additional threat model metadata |  [optional]
**issueUri** | **String** | URL to an issue in an issue tracking system for this threat model |  [optional]
**status** | **List&lt;String&gt;** | Status of the threat model in the organization&#x27;s threat modeling or SDLC process. Examples: \&quot;Not started\&quot;, \&quot;In progress\&quot;, \&quot;Review\&quot;, \&quot;Approved\&quot;, \&quot;Closed\&quot; |  [optional]
