# CollaborationApi

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
```java
// Import classes:
//import io.swagger.client.ApiClient;
//import io.swagger.client.ApiException;
//import io.swagger.client.Configuration;
//import io.swagger.client.auth.*;
//import io.swagger.client.api.CollaborationApi;

ApiClient defaultClient = Configuration.getDefaultApiClient();


CollaborationApi apiInstance = new CollaborationApi();
UUID threatModelId = new UUID(); // UUID | Unique identifier of the threat model (UUID)
UUID diagramId = new UUID(); // UUID | Unique identifier of the diagram (UUID)
try {
    CollaborationSession result = apiInstance.createDiagramCollaborationSession(threatModelId, diagramId);
    System.out.println(result);
} catch (ApiException e) {
    System.err.println("Exception when calling CollaborationApi#createDiagramCollaborationSession");
    e.printStackTrace();
}
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **threatModelId** | [**UUID**](.md)| Unique identifier of the threat model (UUID) |
 **diagramId** | [**UUID**](.md)| Unique identifier of the diagram (UUID) |

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
```java
// Import classes:
//import io.swagger.client.ApiClient;
//import io.swagger.client.ApiException;
//import io.swagger.client.Configuration;
//import io.swagger.client.auth.*;
//import io.swagger.client.api.CollaborationApi;

ApiClient defaultClient = Configuration.getDefaultApiClient();


CollaborationApi apiInstance = new CollaborationApi();
UUID threatModelId = new UUID(); // UUID | Unique identifier of the threat model (UUID)
UUID diagramId = new UUID(); // UUID | Unique identifier of the diagram (UUID)
try {
    apiInstance.endDiagramCollaborationSession(threatModelId, diagramId);
} catch (ApiException e) {
    System.err.println("Exception when calling CollaborationApi#endDiagramCollaborationSession");
    e.printStackTrace();
}
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **threatModelId** | [**UUID**](.md)| Unique identifier of the threat model (UUID) |
 **diagramId** | [**UUID**](.md)| Unique identifier of the diagram (UUID) |

### Return type

null (empty response body)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

<a name="getCollaborationSessions"></a>
# **getCollaborationSessions**
> List&lt;CollaborationSession&gt; getCollaborationSessions()

List active collaboration sessions

Returns all currently active collaboration sessions for diagrams. Clients can use this endpoint to discover existing sessions before connecting via WebSocket.

### Example
```java
// Import classes:
//import io.swagger.client.ApiClient;
//import io.swagger.client.ApiException;
//import io.swagger.client.Configuration;
//import io.swagger.client.auth.*;
//import io.swagger.client.api.CollaborationApi;

ApiClient defaultClient = Configuration.getDefaultApiClient();


CollaborationApi apiInstance = new CollaborationApi();
try {
    List<CollaborationSession> result = apiInstance.getCollaborationSessions();
    System.out.println(result);
} catch (ApiException e) {
    System.err.println("Exception when calling CollaborationApi#getCollaborationSessions");
    e.printStackTrace();
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

[**List&lt;CollaborationSession&gt;**](CollaborationSession.md)

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
```java
// Import classes:
//import io.swagger.client.ApiClient;
//import io.swagger.client.ApiException;
//import io.swagger.client.Configuration;
//import io.swagger.client.auth.*;
//import io.swagger.client.api.CollaborationApi;

ApiClient defaultClient = Configuration.getDefaultApiClient();


CollaborationApi apiInstance = new CollaborationApi();
UUID threatModelId = new UUID(); // UUID | Unique identifier of the threat model (UUID)
UUID diagramId = new UUID(); // UUID | Unique identifier of the diagram (UUID)
try {
    CollaborationSession result = apiInstance.getDiagramCollaborationSession(threatModelId, diagramId);
    System.out.println(result);
} catch (ApiException e) {
    System.err.println("Exception when calling CollaborationApi#getDiagramCollaborationSession");
    e.printStackTrace();
}
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **threatModelId** | [**UUID**](.md)| Unique identifier of the threat model (UUID) |
 **diagramId** | [**UUID**](.md)| Unique identifier of the diagram (UUID) |

### Return type

[**CollaborationSession**](CollaborationSession.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

