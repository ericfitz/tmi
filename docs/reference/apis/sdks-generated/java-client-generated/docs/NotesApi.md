# NotesApi

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
```java
// Import classes:
//import io.swagger.client.ApiClient;
//import io.swagger.client.ApiException;
//import io.swagger.client.Configuration;
//import io.swagger.client.auth.*;
//import io.swagger.client.api.NotesApi;

ApiClient defaultClient = Configuration.getDefaultApiClient();


NotesApi apiInstance = new NotesApi();
List<ThreatsThreatIdBody> body = Arrays.asList(new ThreatsThreatIdBody()); // List<ThreatsThreatIdBody> | 
UUID threatModelId = new UUID(); // UUID | Unique identifier of the threat model (UUID)
UUID noteId = new UUID(); // UUID | Note ID
try {
    Note result = apiInstance.patchThreatModelNote(body, threatModelId, noteId);
    System.out.println(result);
} catch (ApiException e) {
    System.err.println("Exception when calling NotesApi#patchThreatModelNote");
    e.printStackTrace();
}
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**List&lt;ThreatsThreatIdBody&gt;**](ThreatsThreatIdBody.md)|  |
 **threatModelId** | [**UUID**](.md)| Unique identifier of the threat model (UUID) |
 **noteId** | [**UUID**](.md)| Note ID |

### Return type

[**Note**](Note.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json-patch+json
 - **Accept**: application/json

