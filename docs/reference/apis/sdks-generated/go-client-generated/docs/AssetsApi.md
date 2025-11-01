# {{classname}}

All URIs are relative to *http://localhost:8080*

Method | HTTP request | Description
------------- | ------------- | -------------
[**PatchThreatModelAsset**](AssetsApi.md#PatchThreatModelAsset) | **Patch** /threat_models/{threat_model_id}/assets/{asset_id} | Partially update asset

# **PatchThreatModelAsset**
> Asset PatchThreatModelAsset(ctx, body, threatModelId, assetId)
Partially update asset

Apply JSON Patch operations to partially update a asset

### Required Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **ctx** | **context.Context** | context for authentication, logging, cancellation, deadlines, tracing, etc.
  **body** | [**[]ThreatsThreatIdBody**](threats_threat_id_body.md)|  | 
  **threatModelId** | [**string**](.md)| Unique identifier of the threat model (UUID) | 
  **assetId** | [**string**](.md)| Asset ID | 

### Return type

[**Asset**](Asset.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json-patch+json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

