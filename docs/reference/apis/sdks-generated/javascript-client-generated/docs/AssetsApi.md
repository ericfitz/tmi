# TmiClient.AssetsApi

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
```javascript
import {TmiClient} from 'tmi-client';
let defaultClient = TmiClient.ApiClient.instance;


let apiInstance = new TmiClient.AssetsApi();
let body = [new TmiClient.ThreatsThreatIdBody()]; // [ThreatsThreatIdBody] | 
let threatModelId = "38400000-8cf0-11bd-b23e-10b96e4ef00d"; // String | Unique identifier of the threat model (UUID)
let assetId = "38400000-8cf0-11bd-b23e-10b96e4ef00d"; // String | Asset ID

apiInstance.patchThreatModelAsset(body, threatModelId, assetId, (error, data, response) => {
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
 **assetId** | [**String**](.md)| Asset ID | 

### Return type

[**Asset**](Asset.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json-patch+json
 - **Accept**: application/json

