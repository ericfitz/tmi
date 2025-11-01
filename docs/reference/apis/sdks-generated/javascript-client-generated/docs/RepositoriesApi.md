# TmiClient.RepositoriesApi

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
```javascript
import {TmiClient} from 'tmi-client';
let defaultClient = TmiClient.ApiClient.instance;


let apiInstance = new TmiClient.RepositoriesApi();
let body = [new TmiClient.ThreatsThreatIdBody()]; // [ThreatsThreatIdBody] | 
let threatModelId = "38400000-8cf0-11bd-b23e-10b96e4ef00d"; // String | Unique identifier of the threat model (UUID)
let repositoryId = "38400000-8cf0-11bd-b23e-10b96e4ef00d"; // String | Repository ID

apiInstance.patchThreatModelRepository(body, threatModelId, repositoryId, (error, data, response) => {
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
 **repositoryId** | [**String**](.md)| Repository ID | 

### Return type

[**Repository**](Repository.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json-patch+json
 - **Accept**: application/json

