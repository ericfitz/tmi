# OAuthDiscoveryApi

All URIs are relative to *http://localhost:8080*

Method | HTTP request | Description
------------- | ------------- | -------------
[**getOAuthProtectedResourceMetadata**](OAuthDiscoveryApi.md#getOAuthProtectedResourceMetadata) | **GET** /.well-known/oauth-protected-resource | OAuth 2.0 Protected Resource Metadata

<a name="getOAuthProtectedResourceMetadata"></a>
# **getOAuthProtectedResourceMetadata**
> OAuthProtectedResourceMetadata getOAuthProtectedResourceMetadata()

OAuth 2.0 Protected Resource Metadata

Returns OAuth 2.0 protected resource metadata as per RFC 9728

### Example
```java
// Import classes:
//import io.swagger.client.ApiException;
//import io.swagger.client.api.OAuthDiscoveryApi;


OAuthDiscoveryApi apiInstance = new OAuthDiscoveryApi();
try {
    OAuthProtectedResourceMetadata result = apiInstance.getOAuthProtectedResourceMetadata();
    System.out.println(result);
} catch (ApiException e) {
    System.err.println("Exception when calling OAuthDiscoveryApi#getOAuthProtectedResourceMetadata");
    e.printStackTrace();
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

[**OAuthProtectedResourceMetadata**](OAuthProtectedResourceMetadata.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

