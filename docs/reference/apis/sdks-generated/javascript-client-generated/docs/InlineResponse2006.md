# TmiClient.InlineResponse2006

## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**sub** | **String** | Subject identifier (email or provider-assigned ID) | [optional] 
**id** | **String** | Unique identifier for the user (UUID) | 
**email** | **String** | User email address | 
**name** | **String** | User display name | 
**idp** | **String** | Current identity provider | [optional] 
**groups** | **[String]** | Groups the user belongs to | [optional] 
**providers** | [**[InlineResponse2006Providers]**](InlineResponse2006Providers.md) | Linked OAuth providers | [optional] 
