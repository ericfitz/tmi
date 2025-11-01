# TmiClient.CollaborationApi

All URIs are relative to *http://localhost:8080*

Method | HTTP request | Description
------------- | ------------- | -------------
[**createDiagramCollaborationSession**](CollaborationApi.md#createDiagramCollaborationSession) | **POST** /threat_models/{threat_model_id}/diagrams/{diagram_id}/collaborate | Create diagram collaboration session
[**endDiagramCollaborationSession**](CollaborationApi.md#endDiagramCollaborationSession) | **DELETE** /threat_models/{threat_model_id}/diagrams/{diagram_id}/collaborate | End diagram collaboration session
[**getCollaborationSessions**](CollaborationApi.md#getCollaborationSessions) | **GET** /collaboration/sessions | List active collaboration sessions
[**getDiagramCollaborationSession**](CollaborationApi.md#getDiagramCollaborationSession) | **GET** /threat_models/{threat_model_id}/diagrams/{diagram_id}/collaborate | Get diagram collaboration session

<a name="createDiagramCollaborationSession"></a>
# **createDiagramCollaborationSession**
> CollaborationSession createDiagramCollaborationSession(threatModelId, diagramId)

Create diagram collaboration session

Creates a new collaboration session for real-time diagram editing. Only one collaboration session can exist per diagram at a time. If a session already exists, returns 409 with a link to join the existing session.

### Example
```javascript
import {TmiClient} from 'tmi-client';
let defaultClient = TmiClient.ApiClient.instance;


let apiInstance = new TmiClient.CollaborationApi();
let threatModelId = "38400000-8cf0-11bd-b23e-10b96e4ef00d"; // String | Unique identifier of the threat model (UUID)
let diagramId = "38400000-8cf0-11bd-b23e-10b96e4ef00d"; // String | Unique identifier of the diagram (UUID)

apiInstance.createDiagramCollaborationSession(threatModelId, diagramId, (error, data, response) => {
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
 **diagramId** | [**String**](.md)| Unique identifier of the diagram (UUID) | 

### Return type

[**CollaborationSession**](CollaborationSession.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

<a name="endDiagramCollaborationSession"></a>
# **endDiagramCollaborationSession**
> endDiagramCollaborationSession(threatModelId, diagramId)

End diagram collaboration session

Terminates the active collaboration session for a diagram

### Example
```javascript
import {TmiClient} from 'tmi-client';
let defaultClient = TmiClient.ApiClient.instance;


let apiInstance = new TmiClient.CollaborationApi();
let threatModelId = "38400000-8cf0-11bd-b23e-10b96e4ef00d"; // String | Unique identifier of the threat model (UUID)
let diagramId = "38400000-8cf0-11bd-b23e-10b96e4ef00d"; // String | Unique identifier of the diagram (UUID)

apiInstance.endDiagramCollaborationSession(threatModelId, diagramId, (error, data, response) => {
  if (error) {
    console.error(error);
  } else {
    console.log('API called successfully.');
  }
});
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **threatModelId** | [**String**](.md)| Unique identifier of the threat model (UUID) | 
 **diagramId** | [**String**](.md)| Unique identifier of the diagram (UUID) | 

### Return type

null (empty response body)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

<a name="getCollaborationSessions"></a>
# **getCollaborationSessions**
> [CollaborationSession] getCollaborationSessions()

List active collaboration sessions

Returns all currently active collaboration sessions for diagrams. Clients can use this endpoint to discover existing sessions before connecting via WebSocket.

### Example
```javascript
import {TmiClient} from 'tmi-client';
let defaultClient = TmiClient.ApiClient.instance;


let apiInstance = new TmiClient.CollaborationApi();
apiInstance.getCollaborationSessions((error, data, response) => {
  if (error) {
    console.error(error);
  } else {
    console.log('API called successfully. Returned data: ' + data);
  }
});
```

### Parameters
This endpoint does not need any parameter.

### Return type

[**[CollaborationSession]**](CollaborationSession.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

<a name="getDiagramCollaborationSession"></a>
# **getDiagramCollaborationSession**
> CollaborationSession getDiagramCollaborationSession(threatModelId, diagramId)

Get diagram collaboration session

Retrieves the current collaboration session details for a diagram. The session payload indicates who has been authorized to the session, not who is currently active in the WebSocket session. The 200 status indicates successful retrieval - clients must NOT evaluate the payload to determine session status.

### Example
```javascript
import {TmiClient} from 'tmi-client';
let defaultClient = TmiClient.ApiClient.instance;


let apiInstance = new TmiClient.CollaborationApi();
let threatModelId = "38400000-8cf0-11bd-b23e-10b96e4ef00d"; // String | Unique identifier of the threat model (UUID)
let diagramId = "38400000-8cf0-11bd-b23e-10b96e4ef00d"; // String | Unique identifier of the diagram (UUID)

apiInstance.getDiagramCollaborationSession(threatModelId, diagramId, (error, data, response) => {
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
 **diagramId** | [**String**](.md)| Unique identifier of the diagram (UUID) | 

### Return type

[**CollaborationSession**](CollaborationSession.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

