# OAuthProtectedResourceMetadata

## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Resource** | **string** | The protected resource&#x27;s resource identifier URL | [default to null]
**ScopesSupported** | **[]string** | JSON array of OAuth scope values supported by this protected resource | [optional] [default to null]
**AuthorizationServers** | **[]string** | List of authorization server issuer identifiers that can issue tokens for this resource | [optional] [default to null]
**JwksUrl** | **string** | URL of the protected resource&#x27;s JSON Web Key Set | [optional] [default to null]
**BearerMethodsSupported** | **[]string** | Supported token presentation methods for bearer tokens | [optional] [default to null]
**ResourceName** | **string** | Human-readable name of the protected resource | [optional] [default to null]
**ResourceDocumentation** | **string** | URL with information for developers on how to use this protected resource | [optional] [default to null]
**TlsClientCertificateBoundAccessTokens** | **bool** | Whether the protected resource supports TLS client certificate bound access tokens | [optional] [default to null]

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)

