# tmi_client.ThreatsApi

All URIs are relative to *http://localhost:8080*

Method | HTTP request | Description
------------- | ------------- | -------------
[**bulk_delete_threat_model_threats**](ThreatsApi.md#bulk_delete_threat_model_threats) | **DELETE** /threat_models/{threat_model_id}/threats/bulk | Bulk DELETE threats
[**bulk_patch_threat_model_threats**](ThreatsApi.md#bulk_patch_threat_model_threats) | **PATCH** /threat_models/{threat_model_id}/threats/bulk | Bulk PATCH threats

# **bulk_delete_threat_model_threats**
> InlineResponse2007 bulk_delete_threat_model_threats(threat_model_id, threat_ids)

Bulk DELETE threats

Delete multiple threats in a single request

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatsApi(tmi_client.ApiClient(configuration))
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
threat_ids = ['threat_ids_example'] # list[str] | Comma-separated list of threat IDs to delete (UUID format)

try:
    # Bulk DELETE threats
    api_response = api_instance.bulk_delete_threat_model_threats(threat_model_id, threat_ids)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatsApi->bulk_delete_threat_model_threats: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **threat_ids** | [**list[str]**](str.md)| Comma-separated list of threat IDs to delete (UUID format) | 

### Return type

[**InlineResponse2007**](InlineResponse2007.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **bulk_patch_threat_model_threats**
> list[Threat] bulk_patch_threat_model_threats(body, threat_model_id)

Bulk PATCH threats

Apply JSON Patch operations to multiple threats in a single request

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatsApi(tmi_client.ApiClient(configuration))
body = tmi_client.ThreatsBulkBody() # ThreatsBulkBody | 
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)

try:
    # Bulk PATCH threats
    api_response = api_instance.bulk_patch_threat_model_threats(body, threat_model_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatsApi->bulk_patch_threat_model_threats: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**ThreatsBulkBody**](ThreatsBulkBody.md)|  | 
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 

### Return type

[**list[Threat]**](Threat.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json-patch+json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

