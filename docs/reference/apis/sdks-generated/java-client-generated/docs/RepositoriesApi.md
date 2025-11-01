# RepositoriesApi

All URIs are relative to *http://localhost:8080*

Method | HTTP request | Description
------------- | ------------- | -------------
[**patchThreatModelRepository**](RepositoriesApi.md#patchThreatModelRepository) | **PATCH** /threat_models/{threat_model_id}/repositories/{repository_id} | Partially update repository

<a name="patchThreatModelRepository"></a>
# **patchThreatModelRepository**
> Repository patchThreatModelRepository(body, threatModelId, repositoryId)

Partially update repository

Apply JSON Patch operations to partially update a repository

### Example
```java
// Import classes:
//import io.swagger.client.ApiClient;
//import io.swagger.client.ApiException;
//import io.swagger.client.Configuration;
//import io.swagger.client.auth.*;
//import io.swagger.client.api.RepositoriesApi;

ApiClient defaultClient = Configuration.getDefaultApiClient();


RepositoriesApi apiInstance = new RepositoriesApi();
List<ThreatsThreatIdBody> body = Arrays.asList(new ThreatsThreatIdBody()); // List<ThreatsThreatIdBody> | 
UUID threatModelId = new UUID(); // UUID | Unique identifier of the threat model (UUID)
UUID repositoryId = new UUID(); // UUID | Repository ID
try {
    Repository result = apiInstance.patchThreatModelRepository(body, threatModelId, repositoryId);
    System.out.println(result);
} catch (ApiException e) {
    System.err.println("Exception when calling RepositoriesApi#patchThreatModelRepository");
    e.printStackTrace();
}
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**List&lt;ThreatsThreatIdBody&gt;**](ThreatsThreatIdBody.md)|  |
 **threatModelId** | [**UUID**](.md)| Unique identifier of the threat model (UUID) |
 **repositoryId** | [**UUID**](.md)| Repository ID |

### Return type

[**Repository**](Repository.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json-patch+json
 - **Accept**: application/json

