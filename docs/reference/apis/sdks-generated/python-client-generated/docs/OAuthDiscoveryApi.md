# tmi_client.OAuthDiscoveryApi

All URIs are relative to *http://localhost:8080*

Method | HTTP request | Description
------------- | ------------- | -------------
[**get_o_auth_protected_resource_metadata**](OAuthDiscoveryApi.md#get_o_auth_protected_resource_metadata) | **GET** /.well-known/oauth-protected-resource | OAuth 2.0 Protected Resource Metadata

# **get_o_auth_protected_resource_metadata**
> OAuthProtectedResourceMetadata get_o_auth_protected_resource_metadata()

OAuth 2.0 Protected Resource Metadata

Returns OAuth 2.0 protected resource metadata as per RFC 9728

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint

# create an instance of the API class
api_instance = tmi_client.OAuthDiscoveryApi()

try:
    # OAuth 2.0 Protected Resource Metadata
    api_response = api_instance.get_o_auth_protected_resource_metadata()
    pprint(api_response)
except ApiException as e:
    print("Exception when calling OAuthDiscoveryApi->get_o_auth_protected_resource_metadata: %s\n" % e)
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

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

