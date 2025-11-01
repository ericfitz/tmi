# {{classname}}

All URIs are relative to *http://localhost:8080*

Method | HTTP request | Description
------------- | ------------- | -------------
[**CreateDiagramCollaborationSession**](CollaborationApi.md#CreateDiagramCollaborationSession) | **Post** /threat_models/{threat_model_id}/diagrams/{diagram_id}/collaborate | Create diagram collaboration session
[**EndDiagramCollaborationSession**](CollaborationApi.md#EndDiagramCollaborationSession) | **Delete** /threat_models/{threat_model_id}/diagrams/{diagram_id}/collaborate | End diagram collaboration session
[**GetCollaborationSessions**](CollaborationApi.md#GetCollaborationSessions) | **Get** /collaboration/sessions | List active collaboration sessions
[**GetDiagramCollaborationSession**](CollaborationApi.md#GetDiagramCollaborationSession) | **Get** /threat_models/{threat_model_id}/diagrams/{diagram_id}/collaborate | Get diagram collaboration session

# **CreateDiagramCollaborationSession**
> CollaborationSession CreateDiagramCollaborationSession(ctx, threatModelId, diagramId)
Create diagram collaboration session

Creates a new collaboration session for real-time diagram editing. Only one collaboration session can exist per diagram at a time. If a session already exists, returns 409 with a link to join the existing session.

### Required Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **ctx** | **context.Context** | context for authentication, logging, cancellation, deadlines, tracing, etc.
  **threatModelId** | [**string**](.md)| Unique identifier of the threat model (UUID) | 
  **diagramId** | [**string**](.md)| Unique identifier of the diagram (UUID) | 

### Return type

[**CollaborationSession**](CollaborationSession.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **EndDiagramCollaborationSession**
> EndDiagramCollaborationSession(ctx, threatModelId, diagramId)
End diagram collaboration session

Terminates the active collaboration session for a diagram

### Required Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **ctx** | **context.Context** | context for authentication, logging, cancellation, deadlines, tracing, etc.
  **threatModelId** | [**string**](.md)| Unique identifier of the threat model (UUID) | 
  **diagramId** | [**string**](.md)| Unique identifier of the diagram (UUID) | 

### Return type

 (empty response body)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **GetCollaborationSessions**
> []CollaborationSession GetCollaborationSessions(ctx, )
List active collaboration sessions

Returns all currently active collaboration sessions for diagrams. Clients can use this endpoint to discover existing sessions before connecting via WebSocket.

### Required Parameters
This endpoint does not need any parameter.

### Return type

[**[]CollaborationSession**](CollaborationSession.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **GetDiagramCollaborationSession**
> CollaborationSession GetDiagramCollaborationSession(ctx, threatModelId, diagramId)
Get diagram collaboration session

Retrieves the current collaboration session details for a diagram. The session payload indicates who has been authorized to the session, not who is currently active in the WebSocket session. The 200 status indicates successful retrieval - clients must NOT evaluate the payload to determine session status.

### Required Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **ctx** | **context.Context** | context for authentication, logging, cancellation, deadlines, tracing, etc.
  **threatModelId** | [**string**](.md)| Unique identifier of the threat model (UUID) | 
  **diagramId** | [**string**](.md)| Unique identifier of the diagram (UUID) | 

### Return type

[**CollaborationSession**](CollaborationSession.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

