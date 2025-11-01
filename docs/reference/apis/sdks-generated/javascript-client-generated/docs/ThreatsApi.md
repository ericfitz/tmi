# TmiClient.ThreatsApi

All URIs are relative to *http://localhost:8080*

Method | HTTP request | Description
------------- | ------------- | -------------
[**bulkDeleteThreatModelThreats**](ThreatsApi.md#bulkDeleteThreatModelThreats) | **DELETE** /threat_models/{threat_model_id}/threats/bulk | Bulk DELETE threats
[**bulkPatchThreatModelThreats**](ThreatsApi.md#bulkPatchThreatModelThreats) | **PATCH** /threat_models/{threat_model_id}/threats/bulk | Bulk PATCH threats

<a name="bulkDeleteThreatModelThreats"></a>
# **bulkDeleteThreatModelThreats**
> InlineResponse2007 bulkDeleteThreatModelThreats(threatModelId, threatIds)

Bulk DELETE threats

Delete multiple threats in a single request

### Example
```javascript
import {TmiClient} from 'tmi-client';
let defaultClient = TmiClient.ApiClient.instance;


let apiInstance = new TmiClient.ThreatsApi();
let threatModelId = "38400000-8cf0-11bd-b23e-10b96e4ef00d"; // String | Unique identifier of the threat model (UUID)
let threatIds = ["threatIds_example"]; // [String] | Comma-separated list of threat IDs to delete (UUID format)

apiInstance.bulkDeleteThreatModelThreats(threatModelId, threatIds, (error, data, response) => {
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
 **threatModelId** | [**String**](.md)| Unique identifier of the threat model (UUID) | 
 **threatIds** | [**[String]**](String.md)| Comma-separated list of threat IDs to delete (UUID format) | 

### Return type

[**InlineResponse2007**](InlineResponse2007.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

<a name="bulkPatchThreatModelThreats"></a>
# **bulkPatchThreatModelThreats**
> [Threat] bulkPatchThreatModelThreats(body, threatModelId)

Bulk PATCH threats

Apply JSON Patch operations to multiple threats in a single request

### Example
```javascript
import {TmiClient} from 'tmi-client';
let defaultClient = TmiClient.ApiClient.instance;


let apiInstance = new TmiClient.ThreatsApi();
let body = new TmiClient.ThreatsBulkBody(); // ThreatsBulkBody | 
let threatModelId = "38400000-8cf0-11bd-b23e-10b96e4ef00d"; // String | Unique identifier of the threat model (UUID)

apiInstance.bulkPatchThreatModelThreats(body, threatModelId, (error, data, response) => {
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
 **body** | [**ThreatsBulkBody**](ThreatsBulkBody.md)|  | 
 **threatModelId** | [**String**](.md)| Unique identifier of the threat model (UUID) | 

### Return type

[**[Threat]**](Threat.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json-patch+json
 - **Accept**: application/json

