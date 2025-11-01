# {{classname}}

All URIs are relative to *http://localhost:8080*

Method | HTTP request | Description
------------- | ------------- | -------------
[**GetJWKS**](OIDCDiscoveryApi.md#GetJWKS) | **Get** /.well-known/jwks.json | JSON Web Key Set
[**GetOAuthAuthorizationServerMetadata**](OIDCDiscoveryApi.md#GetOAuthAuthorizationServerMetadata) | **Get** /.well-known/oauth-authorization-server | OAuth 2.0 Authorization Server Metadata
[**GetOpenIDConfiguration**](OIDCDiscoveryApi.md#GetOpenIDConfiguration) | **Get** /.well-known/openid-configuration | OpenID Connect Discovery Configuration

# **GetJWKS**
> InlineResponse2002 GetJWKS(ctx, )
JSON Web Key Set

Returns the JSON Web Key Set (JWKS) for JWT signature verification

### Required Parameters
This endpoint does not need any parameter.

### Return type

[**InlineResponse2002**](inline_response_200_2.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **GetOAuthAuthorizationServerMetadata**
> InlineResponse2001 GetOAuthAuthorizationServerMetadata(ctx, )
OAuth 2.0 Authorization Server Metadata

Returns OAuth 2.0 authorization server metadata as per RFC 8414

### Required Parameters
This endpoint does not need any parameter.

### Return type

[**InlineResponse2001**](inline_response_200_1.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **GetOpenIDConfiguration**
> InlineResponse200 GetOpenIDConfiguration(ctx, )
OpenID Connect Discovery Configuration

Returns OpenID Connect provider configuration metadata as per RFC 8414

### Required Parameters
This endpoint does not need any parameter.

### Return type

[**InlineResponse200**](inline_response_200.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

