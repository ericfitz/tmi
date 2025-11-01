# {{classname}}

All URIs are relative to *http://localhost:8080*

Method | HTTP request | Description
------------- | ------------- | -------------
[**PatchThreatModelRepository**](RepositoriesApi.md#PatchThreatModelRepository) | **Patch** /threat_models/{threat_model_id}/repositories/{repository_id} | Partially update repository

# **PatchThreatModelRepository**
> Repository PatchThreatModelRepository(ctx, body, threatModelId, repositoryId)
Partially update repository

Apply JSON Patch operations to partially update a repository

### Required Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **ctx** | **context.Context** | context for authentication, logging, cancellation, deadlines, tracing, etc.
  **body** | [**[]ThreatsThreatIdBody**](threats_threat_id_body.md)|  | 
  **threatModelId** | [**string**](.md)| Unique identifier of the threat model (UUID) | 
  **repositoryId** | [**string**](.md)| Repository ID | 

### Return type

[**Repository**](Repository.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json-patch+json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

