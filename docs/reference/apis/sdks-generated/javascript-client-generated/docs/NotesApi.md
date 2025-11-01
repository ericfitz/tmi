# TmiClient.NotesApi

All URIs are relative to *http://localhost:8080*

Method | HTTP request | Description
------------- | ------------- | -------------
[**patchThreatModelNote**](NotesApi.md#patchThreatModelNote) | **PATCH** /threat_models/{threat_model_id}/notes/{note_id} | Partially update note

<a name="patchThreatModelNote"></a>
# **patchThreatModelNote**
> Note patchThreatModelNote(body, threatModelId, noteId)

Partially update note

Apply JSON Patch operations to partially update a note

### Example
```javascript
import {TmiClient} from 'tmi-client';
let defaultClient = TmiClient.ApiClient.instance;


let apiInstance = new TmiClient.NotesApi();
let body = [new TmiClient.ThreatsThreatIdBody()]; // [ThreatsThreatIdBody] | 
let threatModelId = "38400000-8cf0-11bd-b23e-10b96e4ef00d"; // String | Unique identifier of the threat model (UUID)
let noteId = "38400000-8cf0-11bd-b23e-10b96e4ef00d"; // String | Note ID

apiInstance.patchThreatModelNote(body, threatModelId, noteId, (error, data, response) => {
  if (error) {
    console.error(error);
  } else {
    console.log('API called successfully. Returned data: ' + data);
  }
});
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**[ThreatsThreatIdBody]**](ThreatsThreatIdBody.md)|  | 
 **threatModelId** | [**String**](.md)| Unique identifier of the threat model (UUID) | 
 **noteId** | [**String**](.md)| Note ID | 

### Return type

[**Note**](Note.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json-patch+json
 - **Accept**: application/json

