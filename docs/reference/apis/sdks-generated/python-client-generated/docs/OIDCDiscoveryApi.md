# tmi_client.OIDCDiscoveryApi

All URIs are relative to *http://localhost:8080*

Method | HTTP request | Description
------------- | ------------- | -------------
[**get_jwks**](OIDCDiscoveryApi.md#get_jwks) | **GET** /.well-known/jwks.json | JSON Web Key Set
[**get_o_auth_authorization_server_metadata**](OIDCDiscoveryApi.md#get_o_auth_authorization_server_metadata) | **GET** /.well-known/oauth-authorization-server | OAuth 2.0 Authorization Server Metadata
[**get_open_id_configuration**](OIDCDiscoveryApi.md#get_open_id_configuration) | **GET** /.well-known/openid-configuration | OpenID Connect Discovery Configuration

# **get_jwks**
> InlineResponse2002 get_jwks()

JSON Web Key Set

Returns the JSON Web Key Set (JWKS) for JWT signature verification

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint

# create an instance of the API class
api_instance = tmi_client.OIDCDiscoveryApi()

try:
    # JSON Web Key Set
    api_response = api_instance.get_jwks()
    pprint(api_response)
except ApiException as e:
    print("Exception when calling OIDCDiscoveryApi->get_jwks: %s\n" % e)
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

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **get_o_auth_authorization_server_metadata**
> InlineResponse2001 get_o_auth_authorization_server_metadata()

OAuth 2.0 Authorization Server Metadata

Returns OAuth 2.0 authorization server metadata as per RFC 8414

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint

# create an instance of the API class
api_instance = tmi_client.OIDCDiscoveryApi()

try:
    # OAuth 2.0 Authorization Server Metadata
    api_response = api_instance.get_o_auth_authorization_server_metadata()
    pprint(api_response)
except ApiException as e:
    print("Exception when calling OIDCDiscoveryApi->get_o_auth_authorization_server_metadata: %s\n" % e)
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

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **get_open_id_configuration**
> InlineResponse200 get_open_id_configuration()

OpenID Connect Discovery Configuration

Returns OpenID Connect provider configuration metadata as per RFC 8414

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint

# create an instance of the API class
api_instance = tmi_client.OIDCDiscoveryApi()

try:
    # OpenID Connect Discovery Configuration
    api_response = api_instance.get_open_id_configuration()
    pprint(api_response)
except ApiException as e:
    print("Exception when calling OIDCDiscoveryApi->get_open_id_configuration: %s\n" % e)
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

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

