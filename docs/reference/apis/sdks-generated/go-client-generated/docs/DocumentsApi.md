# {{classname}}

All URIs are relative to *http://localhost:8080*

Method | HTTP request | Description
------------- | ------------- | -------------
[**PatchThreatModelDocument**](DocumentsApi.md#PatchThreatModelDocument) | **Patch** /threat_models/{threat_model_id}/documents/{document_id} | Partially update document

# **PatchThreatModelDocument**
> Document PatchThreatModelDocument(ctx, body, threatModelId, documentId)
Partially update document

Apply JSON Patch operations to partially update a document

### Required Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **ctx** | **context.Context** | context for authentication, logging, cancellation, deadlines, tracing, etc.
  **body** | [**[]ThreatsThreatIdBody**](threats_threat_id_body.md)|  | 
  **threatModelId** | [**string**](.md)| Unique identifier of the threat model (UUID) | 
  **documentId** | [**string**](.md)| Document ID | 

### Return type

[**Document**](Document.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json-patch+json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

