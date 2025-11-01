# ThreatsApi

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
```java
// Import classes:
//import io.swagger.client.ApiClient;
//import io.swagger.client.ApiException;
//import io.swagger.client.Configuration;
//import io.swagger.client.auth.*;
//import io.swagger.client.api.ThreatsApi;

ApiClient defaultClient = Configuration.getDefaultApiClient();


ThreatsApi apiInstance = new ThreatsApi();
UUID threatModelId = new UUID(); // UUID | Unique identifier of the threat model (UUID)
List<UUID> threatIds = Arrays.asList(new UUID()); // List<UUID> | Comma-separated list of threat IDs to delete (UUID format)
try {
    InlineResponse2007 result = apiInstance.bulkDeleteThreatModelThreats(threatModelId, threatIds);
    System.out.println(result);
} catch (ApiException e) {
    System.err.println("Exception when calling ThreatsApi#bulkDeleteThreatModelThreats");
    e.printStackTrace();
}
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **threatModelId** | [**UUID**](.md)| Unique identifier of the threat model (UUID) |
 **threatIds** | [**List&lt;UUID&gt;**](UUID.md)| Comma-separated list of threat IDs to delete (UUID format) |

### Return type

[**InlineResponse2007**](InlineResponse2007.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

<a name="bulkPatchThreatModelThreats"></a>
# **bulkPatchThreatModelThreats**
> List&lt;Threat&gt; bulkPatchThreatModelThreats(body, threatModelId)

Bulk PATCH threats

Apply JSON Patch operations to multiple threats in a single request

### Example
```java
// Import classes:
//import io.swagger.client.ApiClient;
//import io.swagger.client.ApiException;
//import io.swagger.client.Configuration;
//import io.swagger.client.auth.*;
//import io.swagger.client.api.ThreatsApi;

ApiClient defaultClient = Configuration.getDefaultApiClient();


ThreatsApi apiInstance = new ThreatsApi();
ThreatsBulkBody body = new ThreatsBulkBody(); // ThreatsBulkBody | 
UUID threatModelId = new UUID(); // UUID | Unique identifier of the threat model (UUID)
try {
    List<Threat> result = apiInstance.bulkPatchThreatModelThreats(body, threatModelId);
    System.out.println(result);
} catch (ApiException e) {
    System.err.println("Exception when calling ThreatsApi#bulkPatchThreatModelThreats");
    e.printStackTrace();
}
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**ThreatsBulkBody**](ThreatsBulkBody.md)|  |
 **threatModelId** | [**UUID**](.md)| Unique identifier of the threat model (UUID) |

### Return type

[**List&lt;Threat&gt;**](Threat.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json-patch+json
 - **Accept**: application/json

