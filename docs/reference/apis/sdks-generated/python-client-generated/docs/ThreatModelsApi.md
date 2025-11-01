# tmi_client.ThreatModelsApi

All URIs are relative to *http://localhost:8080*

Method | HTTP request | Description
------------- | ------------- | -------------
[**create_threat_model**](ThreatModelsApi.md#create_threat_model) | **POST** /threat_models | Create a threat model
[**delete_threat_model**](ThreatModelsApi.md#delete_threat_model) | **DELETE** /threat_models/{threat_model_id} | Delete a threat model
[**get_threat_model**](ThreatModelsApi.md#get_threat_model) | **GET** /threat_models/{threat_model_id} | Retrieve a threat model
[**list_threat_models**](ThreatModelsApi.md#list_threat_models) | **GET** /threat_models | List threat models
[**patch_threat_model**](ThreatModelsApi.md#patch_threat_model) | **PATCH** /threat_models/{threat_model_id} | Partially update a threat model
[**update_threat_model**](ThreatModelsApi.md#update_threat_model) | **PUT** /threat_models/{threat_model_id} | Update a threat model

# **create_threat_model**
> ThreatModel create_threat_model(body)

Create a threat model

Creates a new threat model with the authenticated user as owner

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelsApi(tmi_client.ApiClient(configuration))
body = tmi_client.ThreatModelInput() # ThreatModelInput | 

try:
    # Create a threat model
    api_response = api_instance.create_threat_model(body)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelsApi->create_threat_model: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**ThreatModelInput**](ThreatModelInput.md)|  | 

### Return type

[**ThreatModel**](ThreatModel.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **delete_threat_model**
> delete_threat_model(threat_model_id)

Delete a threat model

Deletes a threat model; restricted to owner role

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelsApi(tmi_client.ApiClient(configuration))
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model to delete (UUID)

try:
    # Delete a threat model
    api_instance.delete_threat_model(threat_model_id)
except ApiException as e:
    print("Exception when calling ThreatModelsApi->delete_threat_model: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model to delete (UUID) | 

### Return type

void (empty response body)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **get_threat_model**
> ThreatModel get_threat_model(threat_model_id)

Retrieve a threat model

Retrieves the full details of a specific threat model if the user has access

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelsApi(tmi_client.ApiClient(configuration))
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model to retrieve (UUID)

try:
    # Retrieve a threat model
    api_response = api_instance.get_threat_model(threat_model_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelsApi->get_threat_model: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model to retrieve (UUID) | 

### Return type

[**ThreatModel**](ThreatModel.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **list_threat_models**
> list[TMListItem] list_threat_models(limit=limit, offset=offset, owner=owner, name=name, description=description, issue_uri=issue_uri, created_after=created_after, created_before=created_before, modified_after=modified_after, modified_before=modified_before, status=status, status_updated_after=status_updated_after, status_updated_before=status_updated_before)

List threat models

Returns a list of threat models accessible to the authenticated user with enhanced metadata and entity counts

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelsApi(tmi_client.ApiClient(configuration))
limit = 20 # int | Number of threat models to return (optional) (default to 20)
offset = 0 # int | Pagination offset (optional) (default to 0)
owner = 'owner_example' # str | Filter by owner name or email (optional)
name = 'name_example' # str | Filter by threat model name (partial match) (optional)
description = 'description_example' # str | Filter by threat model description (partial match) (optional)
issue_uri = 'issue_uri_example' # str | Filter by issue URI (partial match) (optional)
created_after = '2013-10-20T19:20:30+01:00' # datetime | Filter threat models created after this date (RFC3339 format) (optional)
created_before = '2013-10-20T19:20:30+01:00' # datetime | Filter threat models created before this date (RFC3339 format) (optional)
modified_after = '2013-10-20T19:20:30+01:00' # datetime | Filter threat models modified after this date (RFC3339 format) (optional)
modified_before = '2013-10-20T19:20:30+01:00' # datetime | Filter threat models modified before this date (RFC3339 format) (optional)
status = 'status_example' # str | Filter by status value (exact match). To filter by multiple statuses, use multiple status parameters or comma-separated values. (optional)
status_updated_after = '2013-10-20T19:20:30+01:00' # datetime | Filter threat models where status was updated after this timestamp (RFC3339) (optional)
status_updated_before = '2013-10-20T19:20:30+01:00' # datetime | Filter threat models where status was updated before this timestamp (RFC3339) (optional)

try:
    # List threat models
    api_response = api_instance.list_threat_models(limit=limit, offset=offset, owner=owner, name=name, description=description, issue_uri=issue_uri, created_after=created_after, created_before=created_before, modified_after=modified_after, modified_before=modified_before, status=status, status_updated_after=status_updated_after, status_updated_before=status_updated_before)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelsApi->list_threat_models: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **limit** | **int**| Number of threat models to return | [optional] [default to 20]
 **offset** | **int**| Pagination offset | [optional] [default to 0]
 **owner** | **str**| Filter by owner name or email | [optional] 
 **name** | **str**| Filter by threat model name (partial match) | [optional] 
 **description** | **str**| Filter by threat model description (partial match) | [optional] 
 **issue_uri** | **str**| Filter by issue URI (partial match) | [optional] 
 **created_after** | **datetime**| Filter threat models created after this date (RFC3339 format) | [optional] 
 **created_before** | **datetime**| Filter threat models created before this date (RFC3339 format) | [optional] 
 **modified_after** | **datetime**| Filter threat models modified after this date (RFC3339 format) | [optional] 
 **modified_before** | **datetime**| Filter threat models modified before this date (RFC3339 format) | [optional] 
 **status** | **str**| Filter by status value (exact match). To filter by multiple statuses, use multiple status parameters or comma-separated values. | [optional] 
 **status_updated_after** | **datetime**| Filter threat models where status was updated after this timestamp (RFC3339) | [optional] 
 **status_updated_before** | **datetime**| Filter threat models where status was updated before this timestamp (RFC3339) | [optional] 

### Return type

[**list[TMListItem]**](TMListItem.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **patch_threat_model**
> ThreatModel patch_threat_model(threat_model_id, body=body)

Partially update a threat model

Applies JSON Patch operations to a threat model; restricted to writer/owner roles

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelsApi(tmi_client.ApiClient(configuration))
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model to update (UUID)
body = [tmi_client.ThreatModelsThreatModelIdBody()] # list[ThreatModelsThreatModelIdBody] |  (optional)

try:
    # Partially update a threat model
    api_response = api_instance.patch_threat_model(threat_model_id, body=body)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelsApi->patch_threat_model: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model to update (UUID) | 
 **body** | [**list[ThreatModelsThreatModelIdBody]**](ThreatModelsThreatModelIdBody.md)|  | [optional] 

### Return type

[**ThreatModel**](ThreatModel.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json-patch+json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **update_threat_model**
> ThreatModel update_threat_model(body, threat_model_id)

Update a threat model

Fully updates a threat model; restricted to writer/owner roles

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelsApi(tmi_client.ApiClient(configuration))
body = tmi_client.ThreatModelInput() # ThreatModelInput | 
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model to update (UUID)

try:
    # Update a threat model
    api_response = api_instance.update_threat_model(body, threat_model_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelsApi->update_threat_model: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**ThreatModelInput**](ThreatModelInput.md)|  | 
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model to update (UUID) | 

### Return type

[**ThreatModel**](ThreatModel.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

