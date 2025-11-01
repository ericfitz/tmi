# DocumentsApi

All URIs are relative to *http://localhost:8080*

Method | HTTP request | Description
------------- | ------------- | -------------
[**patchThreatModelDocument**](DocumentsApi.md#patchThreatModelDocument) | **PATCH** /threat_models/{threat_model_id}/documents/{document_id} | Partially update document

<a name="patchThreatModelDocument"></a>
# **patchThreatModelDocument**
> Document patchThreatModelDocument(body, threatModelId, documentId)

Partially update document

Apply JSON Patch operations to partially update a document

### Example
```java
// Import classes:
//import io.swagger.client.ApiClient;
//import io.swagger.client.ApiException;
//import io.swagger.client.Configuration;
//import io.swagger.client.auth.*;
//import io.swagger.client.api.DocumentsApi;

ApiClient defaultClient = Configuration.getDefaultApiClient();


DocumentsApi apiInstance = new DocumentsApi();
List<ThreatsThreatIdBody> body = Arrays.asList(new ThreatsThreatIdBody()); // List<ThreatsThreatIdBody> | 
UUID threatModelId = new UUID(); // UUID | Unique identifier of the threat model (UUID)
UUID documentId = new UUID(); // UUID | Document ID
try {
    Document result = apiInstance.patchThreatModelDocument(body, threatModelId, documentId);
    System.out.println(result);
} catch (ApiException e) {
    System.err.println("Exception when calling DocumentsApi#patchThreatModelDocument");
    e.printStackTrace();
}
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**List&lt;ThreatsThreatIdBody&gt;**](ThreatsThreatIdBody.md)|  |
 **threatModelId** | [**UUID**](.md)| Unique identifier of the threat model (UUID) |
 **documentId** | [**UUID**](.md)| Document ID |

### Return type

[**Document**](Document.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json-patch+json
 - **Accept**: application/json

