# tmi_client.NotesApi

All URIs are relative to *http://localhost:8080*

Method | HTTP request | Description
------------- | ------------- | -------------
[**patch_threat_model_note**](NotesApi.md#patch_threat_model_note) | **PATCH** /threat_models/{threat_model_id}/notes/{note_id} | Partially update note

# **patch_threat_model_note**
> Note patch_threat_model_note(body, threat_model_id, note_id)

Partially update note

Apply JSON Patch operations to partially update a note

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.NotesApi(tmi_client.ApiClient(configuration))
body = [tmi_client.ThreatsThreatIdBody()] # list[ThreatsThreatIdBody] | 
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
note_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Note ID

try:
    # Partially update note
    api_response = api_instance.patch_threat_model_note(body, threat_model_id, note_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling NotesApi->patch_threat_model_note: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**list[ThreatsThreatIdBody]**](ThreatsThreatIdBody.md)|  | 
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **note_id** | [**str**](.md)| Note ID | 

### Return type

[**Note**](Note.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json-patch+json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

