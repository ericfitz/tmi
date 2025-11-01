# OAuthProtectedResourceMetadata

## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**resource** | **str** | The protected resource&#x27;s resource identifier URL | 
**scopes_supported** | **list[str]** | JSON array of OAuth scope values supported by this protected resource | [optional] 
**authorization_servers** | **list[str]** | List of authorization server issuer identifiers that can issue tokens for this resource | [optional] 
**jwks_url** | **str** | URL of the protected resource&#x27;s JSON Web Key Set | [optional] 
**bearer_methods_supported** | **list[str]** | Supported token presentation methods for bearer tokens | [optional] 
**resource_name** | **str** | Human-readable name of the protected resource | [optional] 
**resource_documentation** | **str** | URL with information for developers on how to use this protected resource | [optional] 
**tls_client_certificate_bound_access_tokens** | **bool** | Whether the protected resource supports TLS client certificate bound access tokens | [optional] 

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)

