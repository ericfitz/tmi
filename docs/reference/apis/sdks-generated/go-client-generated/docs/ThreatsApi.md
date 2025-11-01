# {{classname}}

All URIs are relative to *http://localhost:8080*

Method | HTTP request | Description
------------- | ------------- | -------------
[**BulkDeleteThreatModelThreats**](ThreatsApi.md#BulkDeleteThreatModelThreats) | **Delete** /threat_models/{threat_model_id}/threats/bulk | Bulk DELETE threats
[**BulkPatchThreatModelThreats**](ThreatsApi.md#BulkPatchThreatModelThreats) | **Patch** /threat_models/{threat_model_id}/threats/bulk | Bulk PATCH threats

# **BulkDeleteThreatModelThreats**
> InlineResponse2007 BulkDeleteThreatModelThreats(ctx, threatModelId, threatIds)
Bulk DELETE threats

Delete multiple threats in a single request

### Required Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **ctx** | **context.Context** | context for authentication, logging, cancellation, deadlines, tracing, etc.
  **threatModelId** | [**string**](.md)| Unique identifier of the threat model (UUID) | 
  **threatIds** | [**[]string**](string.md)| Comma-separated list of threat IDs to delete (UUID format) | 

### Return type

[**InlineResponse2007**](inline_response_200_7.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **BulkPatchThreatModelThreats**
> []Threat BulkPatchThreatModelThreats(ctx, body, threatModelId)
Bulk PATCH threats

Apply JSON Patch operations to multiple threats in a single request

### Required Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **ctx** | **context.Context** | context for authentication, logging, cancellation, deadlines, tracing, etc.
  **body** | [**ThreatsBulkBody**](ThreatsBulkBody.md)|  | 
  **threatModelId** | [**string**](.md)| Unique identifier of the threat model (UUID) | 

### Return type

[**[]Threat**](Threat.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json-patch+json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

