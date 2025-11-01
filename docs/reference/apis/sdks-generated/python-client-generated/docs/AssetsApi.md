# tmi_client.AssetsApi

All URIs are relative to *http://localhost:8080*

Method | HTTP request | Description
------------- | ------------- | -------------
[**patch_threat_model_asset**](AssetsApi.md#patch_threat_model_asset) | **PATCH** /threat_models/{threat_model_id}/assets/{asset_id} | Partially update asset

# **patch_threat_model_asset**
> Asset patch_threat_model_asset(body, threat_model_id, asset_id)

Partially update asset

Apply JSON Patch operations to partially update a asset

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.AssetsApi(tmi_client.ApiClient(configuration))
body = [tmi_client.ThreatsThreatIdBody()] # list[ThreatsThreatIdBody] | 
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
asset_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Asset ID

try:
    # Partially update asset
    api_response = api_instance.patch_threat_model_asset(body, threat_model_id, asset_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling AssetsApi->patch_threat_model_asset: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**list[ThreatsThreatIdBody]**](ThreatsThreatIdBody.md)|  | 
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **asset_id** | [**str**](.md)| Asset ID | 

### Return type

[**Asset**](Asset.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json-patch+json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

