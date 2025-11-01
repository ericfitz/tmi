# AssetsApi

All URIs are relative to *http://localhost:8080*

Method | HTTP request | Description
------------- | ------------- | -------------
[**patchThreatModelAsset**](AssetsApi.md#patchThreatModelAsset) | **PATCH** /threat_models/{threat_model_id}/assets/{asset_id} | Partially update asset

<a name="patchThreatModelAsset"></a>
# **patchThreatModelAsset**
> Asset patchThreatModelAsset(body, threatModelId, assetId)

Partially update asset

Apply JSON Patch operations to partially update a asset

### Example
```java
// Import classes:
//import io.swagger.client.ApiClient;
//import io.swagger.client.ApiException;
//import io.swagger.client.Configuration;
//import io.swagger.client.auth.*;
//import io.swagger.client.api.AssetsApi;

ApiClient defaultClient = Configuration.getDefaultApiClient();


AssetsApi apiInstance = new AssetsApi();
List<ThreatsThreatIdBody> body = Arrays.asList(new ThreatsThreatIdBody()); // List<ThreatsThreatIdBody> | 
UUID threatModelId = new UUID(); // UUID | Unique identifier of the threat model (UUID)
UUID assetId = new UUID(); // UUID | Asset ID
try {
    Asset result = apiInstance.patchThreatModelAsset(body, threatModelId, assetId);
    System.out.println(result);
} catch (ApiException e) {
    System.err.println("Exception when calling AssetsApi#patchThreatModelAsset");
    e.printStackTrace();
}
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**List&lt;ThreatsThreatIdBody&gt;**](ThreatsThreatIdBody.md)|  |
 **threatModelId** | [**UUID**](.md)| Unique identifier of the threat model (UUID) |
 **assetId** | [**UUID**](.md)| Asset ID |

### Return type

[**Asset**](Asset.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json-patch+json
 - **Accept**: application/json

