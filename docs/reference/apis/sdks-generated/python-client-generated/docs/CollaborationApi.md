# tmi_client.CollaborationApi

All URIs are relative to *http://localhost:8080*

Method | HTTP request | Description
------------- | ------------- | -------------
[**create_diagram_collaboration_session**](CollaborationApi.md#create_diagram_collaboration_session) | **POST** /threat_models/{threat_model_id}/diagrams/{diagram_id}/collaborate | Create diagram collaboration session
[**end_diagram_collaboration_session**](CollaborationApi.md#end_diagram_collaboration_session) | **DELETE** /threat_models/{threat_model_id}/diagrams/{diagram_id}/collaborate | End diagram collaboration session
[**get_collaboration_sessions**](CollaborationApi.md#get_collaboration_sessions) | **GET** /collaboration/sessions | List active collaboration sessions
[**get_diagram_collaboration_session**](CollaborationApi.md#get_diagram_collaboration_session) | **GET** /threat_models/{threat_model_id}/diagrams/{diagram_id}/collaborate | Get diagram collaboration session

# **create_diagram_collaboration_session**
> CollaborationSession create_diagram_collaboration_session(threat_model_id, diagram_id)

Create diagram collaboration session

Creates a new collaboration session for real-time diagram editing. Only one collaboration session can exist per diagram at a time. If a session already exists, returns 409 with a link to join the existing session.

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.CollaborationApi(tmi_client.ApiClient(configuration))
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
diagram_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the diagram (UUID)

try:
    # Create diagram collaboration session
    api_response = api_instance.create_diagram_collaboration_session(threat_model_id, diagram_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling CollaborationApi->create_diagram_collaboration_session: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **diagram_id** | [**str**](.md)| Unique identifier of the diagram (UUID) | 

### Return type

[**CollaborationSession**](CollaborationSession.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **end_diagram_collaboration_session**
> end_diagram_collaboration_session(threat_model_id, diagram_id)

End diagram collaboration session

Terminates the active collaboration session for a diagram

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.CollaborationApi(tmi_client.ApiClient(configuration))
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
diagram_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the diagram (UUID)

try:
    # End diagram collaboration session
    api_instance.end_diagram_collaboration_session(threat_model_id, diagram_id)
except ApiException as e:
    print("Exception when calling CollaborationApi->end_diagram_collaboration_session: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **diagram_id** | [**str**](.md)| Unique identifier of the diagram (UUID) | 

### Return type

void (empty response body)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **get_collaboration_sessions**
> list[CollaborationSession] get_collaboration_sessions()

List active collaboration sessions

Returns all currently active collaboration sessions for diagrams. Clients can use this endpoint to discover existing sessions before connecting via WebSocket.

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.CollaborationApi(tmi_client.ApiClient(configuration))

try:
    # List active collaboration sessions
    api_response = api_instance.get_collaboration_sessions()
    pprint(api_response)
except ApiException as e:
    print("Exception when calling CollaborationApi->get_collaboration_sessions: %s\n" % e)
```

### Parameters
This endpoint does not need any parameter.

### Return type

[**list[CollaborationSession]**](CollaborationSession.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **get_diagram_collaboration_session**
> CollaborationSession get_diagram_collaboration_session(threat_model_id, diagram_id)

Get diagram collaboration session

Retrieves the current collaboration session details for a diagram. The session payload indicates who has been authorized to the session, not who is currently active in the WebSocket session. The 200 status indicates successful retrieval - clients must NOT evaluate the payload to determine session status.

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.CollaborationApi(tmi_client.ApiClient(configuration))
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
diagram_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the diagram (UUID)

try:
    # Get diagram collaboration session
    api_response = api_instance.get_diagram_collaboration_session(threat_model_id, diagram_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling CollaborationApi->get_diagram_collaboration_session: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **diagram_id** | [**str**](.md)| Unique identifier of the diagram (UUID) | 

### Return type

[**CollaborationSession**](CollaborationSession.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

