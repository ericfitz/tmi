# tmi_client.DocumentsApi

All URIs are relative to *http://localhost:8080*

Method | HTTP request | Description
------------- | ------------- | -------------
[**patch_threat_model_document**](DocumentsApi.md#patch_threat_model_document) | **PATCH** /threat_models/{threat_model_id}/documents/{document_id} | Partially update document

# **patch_threat_model_document**
> Document patch_threat_model_document(body, threat_model_id, document_id)

Partially update document

Apply JSON Patch operations to partially update a document

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.DocumentsApi(tmi_client.ApiClient(configuration))
body = [tmi_client.ThreatsThreatIdBody()] # list[ThreatsThreatIdBody] | 
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
document_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Document ID

try:
    # Partially update document
    api_response = api_instance.patch_threat_model_document(body, threat_model_id, document_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling DocumentsApi->patch_threat_model_document: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**list[ThreatsThreatIdBody]**](ThreatsThreatIdBody.md)|  | 
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **document_id** | [**str**](.md)| Document ID | 

### Return type

[**Document**](Document.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json-patch+json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

