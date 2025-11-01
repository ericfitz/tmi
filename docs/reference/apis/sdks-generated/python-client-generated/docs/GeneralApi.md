# tmi_client.GeneralApi

All URIs are relative to *http://localhost:8080*

Method | HTTP request | Description
------------- | ------------- | -------------
[**get_api_info**](GeneralApi.md#get_api_info) | **GET** / | Get API information

# **get_api_info**
> ApiInfo get_api_info()

Get API information

Returns service, API, and operator information without authentication

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint

# create an instance of the API class
api_instance = tmi_client.GeneralApi()

try:
    # Get API information
    api_response = api_instance.get_api_info()
    pprint(api_response)
except ApiException as e:
    print("Exception when calling GeneralApi->get_api_info: %s\n" % e)
```

### Parameters
This endpoint does not need any parameter.

### Return type

[**ApiInfo**](ApiInfo.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

