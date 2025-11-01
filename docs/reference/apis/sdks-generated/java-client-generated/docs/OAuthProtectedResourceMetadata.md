# OAuthProtectedResourceMetadata

## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**resource** | **String** | The protected resource&#x27;s resource identifier URL | 
**scopesSupported** | **List&lt;String&gt;** | JSON array of OAuth scope values supported by this protected resource |  [optional]
**authorizationServers** | **List&lt;String&gt;** | List of authorization server issuer identifiers that can issue tokens for this resource |  [optional]
**jwksUrl** | **String** | URL of the protected resource&#x27;s JSON Web Key Set |  [optional]
**bearerMethodsSupported** | **List&lt;String&gt;** | Supported token presentation methods for bearer tokens |  [optional]
**resourceName** | **String** | Human-readable name of the protected resource |  [optional]
**resourceDocumentation** | **String** | URL with information for developers on how to use this protected resource |  [optional]
**tlsClientCertificateBoundAccessTokens** | **Boolean** | Whether the protected resource supports TLS client certificate bound access tokens |  [optional]
