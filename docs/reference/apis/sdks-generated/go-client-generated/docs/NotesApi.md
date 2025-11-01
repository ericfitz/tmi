# {{classname}}

All URIs are relative to *http://localhost:8080*

Method | HTTP request | Description
------------- | ------------- | -------------
[**PatchThreatModelNote**](NotesApi.md#PatchThreatModelNote) | **Patch** /threat_models/{threat_model_id}/notes/{note_id} | Partially update note

# **PatchThreatModelNote**
> Note PatchThreatModelNote(ctx, body, threatModelId, noteId)
Partially update note

Apply JSON Patch operations to partially update a note

### Required Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **ctx** | **context.Context** | context for authentication, logging, cancellation, deadlines, tracing, etc.
  **body** | [**[]ThreatsThreatIdBody**](threats_threat_id_body.md)|  | 
  **threatModelId** | [**string**](.md)| Unique identifier of the threat model (UUID) | 
  **noteId** | [**string**](.md)| Note ID | 

### Return type

[**Note**](Note.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json-patch+json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

