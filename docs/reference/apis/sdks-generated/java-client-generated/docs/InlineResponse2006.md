# InlineResponse2006

## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**sub** | **String** | Subject identifier (email or provider-assigned ID) |  [optional]
**id** | [**UUID**](UUID.md) | Unique identifier for the user (UUID) | 
**email** | **String** | User email address | 
**name** | **String** | User display name | 
**idp** | **String** | Current identity provider |  [optional]
**groups** | **List&lt;String&gt;** | Groups the user belongs to |  [optional]
**providers** | [**List&lt;InlineResponse2006Providers&gt;**](InlineResponse2006Providers.md) | Linked OAuth providers |  [optional]
