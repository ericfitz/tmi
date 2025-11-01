# tmi_client.UsersApi

All URIs are relative to *http://localhost:8080*

Method | HTTP request | Description
------------- | ------------- | -------------
[**delete_user_account**](UsersApi.md#delete_user_account) | **DELETE** /users/me | Delete authenticated user account and all data

# **delete_user_account**
> DeletionChallenge delete_user_account(challenge=challenge)

Delete authenticated user account and all data

Two-step deletion process: 1. First call (no challenge parameter) - Returns challenge string 2. Second call (with challenge parameter) - Confirms deletion  Ownership behavior: - Shared threat models: Transfers ownership to another owner from authorization list - Unshared threat models: Deletes completely along with all child resources

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.UsersApi(tmi_client.ApiClient(configuration))
challenge = 'challenge_example' # str | Challenge string from first request (step 2 only). Must match exactly. (optional)

try:
    # Delete authenticated user account and all data
    api_response = api_instance.delete_user_account(challenge=challenge)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling UsersApi->delete_user_account: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **challenge** | **str**| Challenge string from first request (step 2 only). Must match exactly. | [optional] 

### Return type

[**DeletionChallenge**](DeletionChallenge.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

