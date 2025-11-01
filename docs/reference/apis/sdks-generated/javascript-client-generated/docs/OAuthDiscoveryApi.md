# TmiClient.OAuthDiscoveryApi

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
```javascript
import {TmiClient} from 'tmi-client';

let apiInstance = new TmiClient.OAuthDiscoveryApi();
apiInstance.getOAuthProtectedResourceMetadata((error, data, response) => {
  if (error) {
    console.error(error);
  } else {
    console.log('API called successfully. Returned data: ' + data);
  }
});
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

