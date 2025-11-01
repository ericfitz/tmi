# OidcDiscoveryApi

All URIs are relative to *http://localhost:8080*

Method | HTTP request | Description
------------- | ------------- | -------------
[**getJWKS**](OidcDiscoveryApi.md#getJWKS) | **GET** /.well-known/jwks.json | JSON Web Key Set
[**getOAuthAuthorizationServerMetadata**](OidcDiscoveryApi.md#getOAuthAuthorizationServerMetadata) | **GET** /.well-known/oauth-authorization-server | OAuth 2.0 Authorization Server Metadata
[**getOpenIDConfiguration**](OidcDiscoveryApi.md#getOpenIDConfiguration) | **GET** /.well-known/openid-configuration | OpenID Connect Discovery Configuration

<a name="getJWKS"></a>
# **getJWKS**
> InlineResponse2002 getJWKS()

JSON Web Key Set

Returns the JSON Web Key Set (JWKS) for JWT signature verification

### Example
```java
// Import classes:
//import io.swagger.client.ApiException;
//import io.swagger.client.api.OidcDiscoveryApi;


OidcDiscoveryApi apiInstance = new OidcDiscoveryApi();
try {
    InlineResponse2002 result = apiInstance.getJWKS();
    System.out.println(result);
} catch (ApiException e) {
    System.err.println("Exception when calling OidcDiscoveryApi#getJWKS");
    e.printStackTrace();
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

[**InlineResponse2002**](InlineResponse2002.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

<a name="getOAuthAuthorizationServerMetadata"></a>
# **getOAuthAuthorizationServerMetadata**
> InlineResponse2001 getOAuthAuthorizationServerMetadata()

OAuth 2.0 Authorization Server Metadata

Returns OAuth 2.0 authorization server metadata as per RFC 8414

### Example
```java
// Import classes:
//import io.swagger.client.ApiException;
//import io.swagger.client.api.OidcDiscoveryApi;


OidcDiscoveryApi apiInstance = new OidcDiscoveryApi();
try {
    InlineResponse2001 result = apiInstance.getOAuthAuthorizationServerMetadata();
    System.out.println(result);
} catch (ApiException e) {
    System.err.println("Exception when calling OidcDiscoveryApi#getOAuthAuthorizationServerMetadata");
    e.printStackTrace();
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

[**InlineResponse2001**](InlineResponse2001.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

<a name="getOpenIDConfiguration"></a>
# **getOpenIDConfiguration**
> InlineResponse200 getOpenIDConfiguration()

OpenID Connect Discovery Configuration

Returns OpenID Connect provider configuration metadata as per RFC 8414

### Example
```java
// Import classes:
//import io.swagger.client.ApiException;
//import io.swagger.client.api.OidcDiscoveryApi;


OidcDiscoveryApi apiInstance = new OidcDiscoveryApi();
try {
    InlineResponse200 result = apiInstance.getOpenIDConfiguration();
    System.out.println(result);
} catch (ApiException e) {
    System.err.println("Exception when calling OidcDiscoveryApi#getOpenIDConfiguration");
    e.printStackTrace();
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

[**InlineResponse200**](InlineResponse200.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

