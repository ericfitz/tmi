# tmi_client.RepositoriesApi

All URIs are relative to *http://localhost:8080*

Method | HTTP request | Description
------------- | ------------- | -------------
[**patch_threat_model_repository**](RepositoriesApi.md#patch_threat_model_repository) | **PATCH** /threat_models/{threat_model_id}/repositories/{repository_id} | Partially update repository

# **patch_threat_model_repository**
> Repository patch_threat_model_repository(body, threat_model_id, repository_id)

Partially update repository

Apply JSON Patch operations to partially update a repository

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.RepositoriesApi(tmi_client.ApiClient(configuration))
body = [tmi_client.ThreatsThreatIdBody()] # list[ThreatsThreatIdBody] | 
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
repository_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Repository ID

try:
    # Partially update repository
    api_response = api_instance.patch_threat_model_repository(body, threat_model_id, repository_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling RepositoriesApi->patch_threat_model_repository: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**list[ThreatsThreatIdBody]**](ThreatsThreatIdBody.md)|  | 
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **repository_id** | [**str**](.md)| Repository ID | 

### Return type

[**Repository**](Repository.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json-patch+json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

