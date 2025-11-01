# tmi_client.ThreatModelSubResourcesApi

All URIs are relative to *http://localhost:8080*

Method | HTTP request | Description
------------- | ------------- | -------------
[**bulk_create_diagram_metadata**](ThreatModelSubResourcesApi.md#bulk_create_diagram_metadata) | **POST** /threat_models/{threat_model_id}/diagrams/{diagram_id}/metadata/bulk | Bulk create diagram metadata
[**bulk_create_document_metadata**](ThreatModelSubResourcesApi.md#bulk_create_document_metadata) | **POST** /threat_models/{threat_model_id}/documents/{document_id}/metadata/bulk | Bulk create document metadata
[**bulk_create_note_metadata**](ThreatModelSubResourcesApi.md#bulk_create_note_metadata) | **POST** /threat_models/{threat_model_id}/notes/{note_id}/metadata/bulk | Bulk create note metadata
[**bulk_create_repository_metadata**](ThreatModelSubResourcesApi.md#bulk_create_repository_metadata) | **POST** /threat_models/{threat_model_id}/repositories/{repository_id}/metadata/bulk | Bulk create source metadata
[**bulk_create_threat_metadata**](ThreatModelSubResourcesApi.md#bulk_create_threat_metadata) | **POST** /threat_models/{threat_model_id}/threats/{threat_id}/metadata/bulk | Bulk create threat metadata
[**bulk_create_threat_model_asset_metadata**](ThreatModelSubResourcesApi.md#bulk_create_threat_model_asset_metadata) | **POST** /threat_models/{threat_model_id}/assets/{asset_id}/metadata/bulk | Bulk create asset metadata
[**bulk_create_threat_model_assets**](ThreatModelSubResourcesApi.md#bulk_create_threat_model_assets) | **POST** /threat_models/{threat_model_id}/assets/bulk | Bulk create assets
[**bulk_create_threat_model_documents**](ThreatModelSubResourcesApi.md#bulk_create_threat_model_documents) | **POST** /threat_models/{threat_model_id}/documents/bulk | Bulk create documents
[**bulk_create_threat_model_metadata**](ThreatModelSubResourcesApi.md#bulk_create_threat_model_metadata) | **POST** /threat_models/{threat_model_id}/metadata/bulk | Bulk create threat model metadata
[**bulk_create_threat_model_repositories**](ThreatModelSubResourcesApi.md#bulk_create_threat_model_repositories) | **POST** /threat_models/{threat_model_id}/repositories/bulk | Bulk create sources
[**bulk_create_threat_model_threats**](ThreatModelSubResourcesApi.md#bulk_create_threat_model_threats) | **POST** /threat_models/{threat_model_id}/threats/bulk | Bulk create threats
[**bulk_update_note_metadata**](ThreatModelSubResourcesApi.md#bulk_update_note_metadata) | **PUT** /threat_models/{threat_model_id}/notes/{note_id}/metadata/bulk | Bulk update note metadata
[**bulk_update_threat_model_threats**](ThreatModelSubResourcesApi.md#bulk_update_threat_model_threats) | **PUT** /threat_models/{threat_model_id}/threats/bulk | Bulk update threats
[**bulk_upsert_diagram_metadata**](ThreatModelSubResourcesApi.md#bulk_upsert_diagram_metadata) | **PUT** /threat_models/{threat_model_id}/diagrams/{diagram_id}/metadata/bulk | Bulk upsert diagram metadata
[**bulk_upsert_document_metadata**](ThreatModelSubResourcesApi.md#bulk_upsert_document_metadata) | **PUT** /threat_models/{threat_model_id}/documents/{document_id}/metadata/bulk | Bulk upsert document metadata
[**bulk_upsert_repository_metadata**](ThreatModelSubResourcesApi.md#bulk_upsert_repository_metadata) | **PUT** /threat_models/{threat_model_id}/repositories/{repository_id}/metadata/bulk | Bulk upsert repository metadata
[**bulk_upsert_threat_metadata**](ThreatModelSubResourcesApi.md#bulk_upsert_threat_metadata) | **PUT** /threat_models/{threat_model_id}/threats/{threat_id}/metadata/bulk | Bulk upsert threat metadata
[**bulk_upsert_threat_model_asset_metadata**](ThreatModelSubResourcesApi.md#bulk_upsert_threat_model_asset_metadata) | **PUT** /threat_models/{threat_model_id}/assets/{asset_id}/metadata/bulk | Bulk upsert asset metadata
[**bulk_upsert_threat_model_assets**](ThreatModelSubResourcesApi.md#bulk_upsert_threat_model_assets) | **PUT** /threat_models/{threat_model_id}/assets/bulk | Bulk upsert assets
[**bulk_upsert_threat_model_documents**](ThreatModelSubResourcesApi.md#bulk_upsert_threat_model_documents) | **PUT** /threat_models/{threat_model_id}/documents/bulk | Bulk upsert documents
[**bulk_upsert_threat_model_metadata**](ThreatModelSubResourcesApi.md#bulk_upsert_threat_model_metadata) | **PUT** /threat_models/{threat_model_id}/metadata/bulk | Bulk upsert threat model metadata
[**bulk_upsert_threat_model_repositories**](ThreatModelSubResourcesApi.md#bulk_upsert_threat_model_repositories) | **PUT** /threat_models/{threat_model_id}/repositories/bulk | Bulk upsert repositories
[**create_diagram_metadata**](ThreatModelSubResourcesApi.md#create_diagram_metadata) | **POST** /threat_models/{threat_model_id}/diagrams/{diagram_id}/metadata | Create diagram metadata
[**create_document_metadata**](ThreatModelSubResourcesApi.md#create_document_metadata) | **POST** /threat_models/{threat_model_id}/documents/{document_id}/metadata | Create document metadata
[**create_note_metadata**](ThreatModelSubResourcesApi.md#create_note_metadata) | **POST** /threat_models/{threat_model_id}/notes/{note_id}/metadata | Create note metadata
[**create_repository_metadata**](ThreatModelSubResourcesApi.md#create_repository_metadata) | **POST** /threat_models/{threat_model_id}/repositories/{repository_id}/metadata | Create source metadata
[**create_threat_metadata**](ThreatModelSubResourcesApi.md#create_threat_metadata) | **POST** /threat_models/{threat_model_id}/threats/{threat_id}/metadata | Create threat metadata
[**create_threat_model_asset**](ThreatModelSubResourcesApi.md#create_threat_model_asset) | **POST** /threat_models/{threat_model_id}/assets | Create a new asset
[**create_threat_model_asset_metadata**](ThreatModelSubResourcesApi.md#create_threat_model_asset_metadata) | **POST** /threat_models/{threat_model_id}/assets/{asset_id}/metadata | Add metadata to an asset
[**create_threat_model_diagram**](ThreatModelSubResourcesApi.md#create_threat_model_diagram) | **POST** /threat_models/{threat_model_id}/diagrams | Create a new diagram
[**create_threat_model_document**](ThreatModelSubResourcesApi.md#create_threat_model_document) | **POST** /threat_models/{threat_model_id}/documents | Create a new document
[**create_threat_model_metadata**](ThreatModelSubResourcesApi.md#create_threat_model_metadata) | **POST** /threat_models/{threat_model_id}/metadata | Create threat model metadata
[**create_threat_model_note**](ThreatModelSubResourcesApi.md#create_threat_model_note) | **POST** /threat_models/{threat_model_id}/notes | Create a new note
[**create_threat_model_repository**](ThreatModelSubResourcesApi.md#create_threat_model_repository) | **POST** /threat_models/{threat_model_id}/repositories | Create a new source reference
[**create_threat_model_threat**](ThreatModelSubResourcesApi.md#create_threat_model_threat) | **POST** /threat_models/{threat_model_id}/threats | Create a new threat
[**delete_diagram_metadata_by_key**](ThreatModelSubResourcesApi.md#delete_diagram_metadata_by_key) | **DELETE** /threat_models/{threat_model_id}/diagrams/{diagram_id}/metadata/{key} | Delete diagram metadata by key
[**delete_document_metadata_by_key**](ThreatModelSubResourcesApi.md#delete_document_metadata_by_key) | **DELETE** /threat_models/{threat_model_id}/documents/{document_id}/metadata/{key} | Delete document metadata by key
[**delete_note_metadata_by_key**](ThreatModelSubResourcesApi.md#delete_note_metadata_by_key) | **DELETE** /threat_models/{threat_model_id}/notes/{note_id}/metadata/{key} | Delete note metadata by key
[**delete_repository_metadata_by_key**](ThreatModelSubResourcesApi.md#delete_repository_metadata_by_key) | **DELETE** /threat_models/{threat_model_id}/repositories/{repository_id}/metadata/{key} | Delete source metadata by key
[**delete_threat_metadata_by_key**](ThreatModelSubResourcesApi.md#delete_threat_metadata_by_key) | **DELETE** /threat_models/{threat_model_id}/threats/{threat_id}/metadata/{key} | Delete threat metadata by key
[**delete_threat_model_asset**](ThreatModelSubResourcesApi.md#delete_threat_model_asset) | **DELETE** /threat_models/{threat_model_id}/assets/{asset_id} | Delete an asset
[**delete_threat_model_asset_metadata**](ThreatModelSubResourcesApi.md#delete_threat_model_asset_metadata) | **DELETE** /threat_models/{threat_model_id}/assets/{asset_id}/metadata/{key} | Delete asset metadata
[**delete_threat_model_diagram**](ThreatModelSubResourcesApi.md#delete_threat_model_diagram) | **DELETE** /threat_models/{threat_model_id}/diagrams/{diagram_id} | Delete a diagram
[**delete_threat_model_document**](ThreatModelSubResourcesApi.md#delete_threat_model_document) | **DELETE** /threat_models/{threat_model_id}/documents/{document_id} | Delete a document
[**delete_threat_model_metadata_by_key**](ThreatModelSubResourcesApi.md#delete_threat_model_metadata_by_key) | **DELETE** /threat_models/{threat_model_id}/metadata/{key} | Delete threat model metadata by key
[**delete_threat_model_note**](ThreatModelSubResourcesApi.md#delete_threat_model_note) | **DELETE** /threat_models/{threat_model_id}/notes/{note_id} | Delete a note
[**delete_threat_model_repository**](ThreatModelSubResourcesApi.md#delete_threat_model_repository) | **DELETE** /threat_models/{threat_model_id}/repositories/{repository_id} | Delete a source reference
[**delete_threat_model_threat**](ThreatModelSubResourcesApi.md#delete_threat_model_threat) | **DELETE** /threat_models/{threat_model_id}/threats/{threat_id} | Delete a threat
[**get_diagram_metadata**](ThreatModelSubResourcesApi.md#get_diagram_metadata) | **GET** /threat_models/{threat_model_id}/diagrams/{diagram_id}/metadata | Get diagram metadata
[**get_diagram_metadata_by_key**](ThreatModelSubResourcesApi.md#get_diagram_metadata_by_key) | **GET** /threat_models/{threat_model_id}/diagrams/{diagram_id}/metadata/{key} | Get diagram metadata by key
[**get_document_metadata**](ThreatModelSubResourcesApi.md#get_document_metadata) | **GET** /threat_models/{threat_model_id}/documents/{document_id}/metadata | Get document metadata
[**get_document_metadata_by_key**](ThreatModelSubResourcesApi.md#get_document_metadata_by_key) | **GET** /threat_models/{threat_model_id}/documents/{document_id}/metadata/{key} | Get document metadata by key
[**get_note_metadata**](ThreatModelSubResourcesApi.md#get_note_metadata) | **GET** /threat_models/{threat_model_id}/notes/{note_id}/metadata | Get note metadata
[**get_note_metadata_by_key**](ThreatModelSubResourcesApi.md#get_note_metadata_by_key) | **GET** /threat_models/{threat_model_id}/notes/{note_id}/metadata/{key} | Get note metadata by key
[**get_repository_metadata**](ThreatModelSubResourcesApi.md#get_repository_metadata) | **GET** /threat_models/{threat_model_id}/repositories/{repository_id}/metadata | Get source metadata
[**get_repository_metadata_by_key**](ThreatModelSubResourcesApi.md#get_repository_metadata_by_key) | **GET** /threat_models/{threat_model_id}/repositories/{repository_id}/metadata/{key} | Get source metadata by key
[**get_threat_metadata**](ThreatModelSubResourcesApi.md#get_threat_metadata) | **GET** /threat_models/{threat_model_id}/threats/{threat_id}/metadata | Get threat metadata
[**get_threat_metadata_by_key**](ThreatModelSubResourcesApi.md#get_threat_metadata_by_key) | **GET** /threat_models/{threat_model_id}/threats/{threat_id}/metadata/{key} | Get threat metadata by key
[**get_threat_model_asset**](ThreatModelSubResourcesApi.md#get_threat_model_asset) | **GET** /threat_models/{threat_model_id}/assets/{asset_id} | Get a specific asset
[**get_threat_model_asset_metadata**](ThreatModelSubResourcesApi.md#get_threat_model_asset_metadata) | **GET** /threat_models/{threat_model_id}/assets/{asset_id}/metadata | Get all metadata for an asset
[**get_threat_model_asset_metadata_by_key**](ThreatModelSubResourcesApi.md#get_threat_model_asset_metadata_by_key) | **GET** /threat_models/{threat_model_id}/assets/{asset_id}/metadata/{key} | Get specific metadata for an asset
[**get_threat_model_assets**](ThreatModelSubResourcesApi.md#get_threat_model_assets) | **GET** /threat_models/{threat_model_id}/assets | List assets in a threat model
[**get_threat_model_diagram**](ThreatModelSubResourcesApi.md#get_threat_model_diagram) | **GET** /threat_models/{threat_model_id}/diagrams/{diagram_id} | Get a specific diagram
[**get_threat_model_diagrams**](ThreatModelSubResourcesApi.md#get_threat_model_diagrams) | **GET** /threat_models/{threat_model_id}/diagrams | List threat model diagrams
[**get_threat_model_document**](ThreatModelSubResourcesApi.md#get_threat_model_document) | **GET** /threat_models/{threat_model_id}/documents/{document_id} | Get a specific document
[**get_threat_model_documents**](ThreatModelSubResourcesApi.md#get_threat_model_documents) | **GET** /threat_models/{threat_model_id}/documents | List documents in a threat model
[**get_threat_model_metadata**](ThreatModelSubResourcesApi.md#get_threat_model_metadata) | **GET** /threat_models/{threat_model_id}/metadata | Get threat model metadata
[**get_threat_model_metadata_by_key**](ThreatModelSubResourcesApi.md#get_threat_model_metadata_by_key) | **GET** /threat_models/{threat_model_id}/metadata/{key} | Get threat model metadata by key
[**get_threat_model_note**](ThreatModelSubResourcesApi.md#get_threat_model_note) | **GET** /threat_models/{threat_model_id}/notes/{note_id} | Get a specific note
[**get_threat_model_notes**](ThreatModelSubResourcesApi.md#get_threat_model_notes) | **GET** /threat_models/{threat_model_id}/notes | List notes in a threat model
[**get_threat_model_repositories**](ThreatModelSubResourcesApi.md#get_threat_model_repositories) | **GET** /threat_models/{threat_model_id}/repositories | List sources in a threat model
[**get_threat_model_repository**](ThreatModelSubResourcesApi.md#get_threat_model_repository) | **GET** /threat_models/{threat_model_id}/repositories/{repository_id} | Get a specific source reference
[**get_threat_model_threat**](ThreatModelSubResourcesApi.md#get_threat_model_threat) | **GET** /threat_models/{threat_model_id}/threats/{threat_id} | Get a specific threat
[**get_threat_model_threats**](ThreatModelSubResourcesApi.md#get_threat_model_threats) | **GET** /threat_models/{threat_model_id}/threats | List threats in a threat model
[**patch_threat_model_diagram**](ThreatModelSubResourcesApi.md#patch_threat_model_diagram) | **PATCH** /threat_models/{threat_model_id}/diagrams/{diagram_id} | Partially update a diagram
[**patch_threat_model_threat**](ThreatModelSubResourcesApi.md#patch_threat_model_threat) | **PATCH** /threat_models/{threat_model_id}/threats/{threat_id} | Partially update a threat
[**update_diagram_metadata_by_key**](ThreatModelSubResourcesApi.md#update_diagram_metadata_by_key) | **PUT** /threat_models/{threat_model_id}/diagrams/{diagram_id}/metadata/{key} | Update diagram metadata by key
[**update_document_metadata_by_key**](ThreatModelSubResourcesApi.md#update_document_metadata_by_key) | **PUT** /threat_models/{threat_model_id}/documents/{document_id}/metadata/{key} | Update document metadata by key
[**update_note_metadata_by_key**](ThreatModelSubResourcesApi.md#update_note_metadata_by_key) | **PUT** /threat_models/{threat_model_id}/notes/{note_id}/metadata/{key} | Update note metadata by key
[**update_repository_metadata_by_key**](ThreatModelSubResourcesApi.md#update_repository_metadata_by_key) | **PUT** /threat_models/{threat_model_id}/repositories/{repository_id}/metadata/{key} | Update source metadata by key
[**update_threat_metadata_by_key**](ThreatModelSubResourcesApi.md#update_threat_metadata_by_key) | **PUT** /threat_models/{threat_model_id}/threats/{threat_id}/metadata/{key} | Update threat metadata by key
[**update_threat_model_asset**](ThreatModelSubResourcesApi.md#update_threat_model_asset) | **PUT** /threat_models/{threat_model_id}/assets/{asset_id} | Update an asset
[**update_threat_model_asset_metadata**](ThreatModelSubResourcesApi.md#update_threat_model_asset_metadata) | **PUT** /threat_models/{threat_model_id}/assets/{asset_id}/metadata/{key} | Update asset metadata
[**update_threat_model_diagram**](ThreatModelSubResourcesApi.md#update_threat_model_diagram) | **PUT** /threat_models/{threat_model_id}/diagrams/{diagram_id} | Update a diagram
[**update_threat_model_document**](ThreatModelSubResourcesApi.md#update_threat_model_document) | **PUT** /threat_models/{threat_model_id}/documents/{document_id} | Update a document
[**update_threat_model_metadata_by_key**](ThreatModelSubResourcesApi.md#update_threat_model_metadata_by_key) | **PUT** /threat_models/{threat_model_id}/metadata/{key} | Update threat model metadata by key
[**update_threat_model_note**](ThreatModelSubResourcesApi.md#update_threat_model_note) | **PUT** /threat_models/{threat_model_id}/notes/{note_id} | Update a note
[**update_threat_model_repository**](ThreatModelSubResourcesApi.md#update_threat_model_repository) | **PUT** /threat_models/{threat_model_id}/repositories/{repository_id} | Update a source reference
[**update_threat_model_threat**](ThreatModelSubResourcesApi.md#update_threat_model_threat) | **PUT** /threat_models/{threat_model_id}/threats/{threat_id} | Update a threat

# **bulk_create_diagram_metadata**
> list[Metadata] bulk_create_diagram_metadata(body, threat_model_id, diagram_id)

Bulk create diagram metadata

Creates multiple metadata entries in a single operation for the specified diagram

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
body = [tmi_client.Metadata()] # list[Metadata] | 
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
diagram_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the diagram (UUID)

try:
    # Bulk create diagram metadata
    api_response = api_instance.bulk_create_diagram_metadata(body, threat_model_id, diagram_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->bulk_create_diagram_metadata: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**list[Metadata]**](Metadata.md)|  | 
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **diagram_id** | [**str**](.md)| Unique identifier of the diagram (UUID) | 

### Return type

[**list[Metadata]**](Metadata.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **bulk_create_document_metadata**
> list[Metadata] bulk_create_document_metadata(body, threat_model_id, document_id)

Bulk create document metadata

Creates multiple metadata entries in a single operation for the specified document

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
body = [tmi_client.Metadata()] # list[Metadata] | 
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
document_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the document (UUID)

try:
    # Bulk create document metadata
    api_response = api_instance.bulk_create_document_metadata(body, threat_model_id, document_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->bulk_create_document_metadata: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**list[Metadata]**](Metadata.md)|  | 
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **document_id** | [**str**](.md)| Unique identifier of the document (UUID) | 

### Return type

[**list[Metadata]**](Metadata.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **bulk_create_note_metadata**
> list[Metadata] bulk_create_note_metadata(body, threat_model_id, note_id)

Bulk create note metadata

Creates multiple metadata key-value pairs for the specified note

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
body = [tmi_client.Metadata()] # list[Metadata] | 
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
note_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the note (UUID)

try:
    # Bulk create note metadata
    api_response = api_instance.bulk_create_note_metadata(body, threat_model_id, note_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->bulk_create_note_metadata: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**list[Metadata]**](Metadata.md)|  | 
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **note_id** | [**str**](.md)| Unique identifier of the note (UUID) | 

### Return type

[**list[Metadata]**](Metadata.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **bulk_create_repository_metadata**
> list[Metadata] bulk_create_repository_metadata(body, threat_model_id, repository_id)

Bulk create source metadata

Creates multiple metadata entries in a single operation for the specified source reference

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
body = [tmi_client.Metadata()] # list[Metadata] | 
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
repository_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the source reference (UUID)

try:
    # Bulk create source metadata
    api_response = api_instance.bulk_create_repository_metadata(body, threat_model_id, repository_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->bulk_create_repository_metadata: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**list[Metadata]**](Metadata.md)|  | 
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **repository_id** | [**str**](.md)| Unique identifier of the source reference (UUID) | 

### Return type

[**list[Metadata]**](Metadata.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **bulk_create_threat_metadata**
> list[Metadata] bulk_create_threat_metadata(body, threat_model_id, threat_id)

Bulk create threat metadata

Creates multiple metadata entries in a single operation for the specified threat

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
body = [tmi_client.Metadata()] # list[Metadata] | 
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
threat_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat (UUID)

try:
    # Bulk create threat metadata
    api_response = api_instance.bulk_create_threat_metadata(body, threat_model_id, threat_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->bulk_create_threat_metadata: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**list[Metadata]**](Metadata.md)|  | 
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **threat_id** | [**str**](.md)| Unique identifier of the threat (UUID) | 

### Return type

[**list[Metadata]**](Metadata.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **bulk_create_threat_model_asset_metadata**
> list[Metadata] bulk_create_threat_model_asset_metadata(body, threat_model_id, asset_id)

Bulk create asset metadata

Creates multiple metadata key-value pairs for the specified asset

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
body = [tmi_client.Metadata()] # list[Metadata] | 
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
asset_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the asset (UUID)

try:
    # Bulk create asset metadata
    api_response = api_instance.bulk_create_threat_model_asset_metadata(body, threat_model_id, asset_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->bulk_create_threat_model_asset_metadata: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**list[Metadata]**](Metadata.md)|  | 
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **asset_id** | [**str**](.md)| Unique identifier of the asset (UUID) | 

### Return type

[**list[Metadata]**](Metadata.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **bulk_create_threat_model_assets**
> list[Asset] bulk_create_threat_model_assets(body, threat_model_id)

Bulk create assets

Creates multiple assets within the specified threat model (maximum 50 per request)

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
body = [tmi_client.Asset()] # list[Asset] | 
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)

try:
    # Bulk create assets
    api_response = api_instance.bulk_create_threat_model_assets(body, threat_model_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->bulk_create_threat_model_assets: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**list[Asset]**](Asset.md)|  | 
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 

### Return type

[**list[Asset]**](Asset.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **bulk_create_threat_model_documents**
> list[Document] bulk_create_threat_model_documents(body, threat_model_id)

Bulk create documents

Creates multiple documents in a single operation for the specified threat model

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
body = [tmi_client.Document()] # list[Document] | 
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)

try:
    # Bulk create documents
    api_response = api_instance.bulk_create_threat_model_documents(body, threat_model_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->bulk_create_threat_model_documents: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**list[Document]**](Document.md)|  | 
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 

### Return type

[**list[Document]**](Document.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **bulk_create_threat_model_metadata**
> list[Metadata] bulk_create_threat_model_metadata(body, threat_model_id)

Bulk create threat model metadata

Creates multiple metadata entries in a single operation for the specified threat model

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
body = [tmi_client.Metadata()] # list[Metadata] | 
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)

try:
    # Bulk create threat model metadata
    api_response = api_instance.bulk_create_threat_model_metadata(body, threat_model_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->bulk_create_threat_model_metadata: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**list[Metadata]**](Metadata.md)|  | 
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 

### Return type

[**list[Metadata]**](Metadata.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **bulk_create_threat_model_repositories**
> list[Repository] bulk_create_threat_model_repositories(body, threat_model_id)

Bulk create sources

Creates multiple source references in a single operation for the specified threat model

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
body = [tmi_client.Repository()] # list[Repository] | 
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)

try:
    # Bulk create sources
    api_response = api_instance.bulk_create_threat_model_repositories(body, threat_model_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->bulk_create_threat_model_repositories: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**list[Repository]**](Repository.md)|  | 
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 

### Return type

[**list[Repository]**](Repository.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **bulk_create_threat_model_threats**
> list[Threat] bulk_create_threat_model_threats(body, threat_model_id)

Bulk create threats

Creates multiple threats in a single operation for the specified threat model

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
body = [tmi_client.Threat()] # list[Threat] | 
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)

try:
    # Bulk create threats
    api_response = api_instance.bulk_create_threat_model_threats(body, threat_model_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->bulk_create_threat_model_threats: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**list[Threat]**](Threat.md)|  | 
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 

### Return type

[**list[Threat]**](Threat.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **bulk_update_note_metadata**
> list[Metadata] bulk_update_note_metadata(body, threat_model_id, note_id)

Bulk update note metadata

Updates multiple metadata entries for the specified note in a single operation

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
body = [tmi_client.Metadata()] # list[Metadata] | 
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
note_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the note (UUID)

try:
    # Bulk update note metadata
    api_response = api_instance.bulk_update_note_metadata(body, threat_model_id, note_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->bulk_update_note_metadata: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**list[Metadata]**](Metadata.md)|  | 
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **note_id** | [**str**](.md)| Unique identifier of the note (UUID) | 

### Return type

[**list[Metadata]**](Metadata.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **bulk_update_threat_model_threats**
> list[Threat] bulk_update_threat_model_threats(body, threat_model_id)

Bulk update threats

Updates multiple threats in a single operation for the specified threat model

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
body = [tmi_client.Threat()] # list[Threat] | 
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)

try:
    # Bulk update threats
    api_response = api_instance.bulk_update_threat_model_threats(body, threat_model_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->bulk_update_threat_model_threats: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**list[Threat]**](Threat.md)|  | 
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 

### Return type

[**list[Threat]**](Threat.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **bulk_upsert_diagram_metadata**
> list[Metadata] bulk_upsert_diagram_metadata(body, threat_model_id, diagram_id)

Bulk upsert diagram metadata

Creates or updates multiple metadata key-value pairs for the specified diagram

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
body = [tmi_client.Metadata()] # list[Metadata] | 
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
diagram_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the diagram (UUID)

try:
    # Bulk upsert diagram metadata
    api_response = api_instance.bulk_upsert_diagram_metadata(body, threat_model_id, diagram_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->bulk_upsert_diagram_metadata: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**list[Metadata]**](Metadata.md)|  | 
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **diagram_id** | [**str**](.md)| Unique identifier of the diagram (UUID) | 

### Return type

[**list[Metadata]**](Metadata.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **bulk_upsert_document_metadata**
> list[Metadata] bulk_upsert_document_metadata(body, threat_model_id, document_id)

Bulk upsert document metadata

Creates or updates multiple metadata key-value pairs for the specified document

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
body = [tmi_client.Metadata()] # list[Metadata] | 
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
document_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the document (UUID)

try:
    # Bulk upsert document metadata
    api_response = api_instance.bulk_upsert_document_metadata(body, threat_model_id, document_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->bulk_upsert_document_metadata: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**list[Metadata]**](Metadata.md)|  | 
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **document_id** | [**str**](.md)| Unique identifier of the document (UUID) | 

### Return type

[**list[Metadata]**](Metadata.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **bulk_upsert_repository_metadata**
> list[Metadata] bulk_upsert_repository_metadata(body, threat_model_id, repository_id)

Bulk upsert repository metadata

Creates or updates multiple metadata key-value pairs for the specified repository

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
body = [tmi_client.Metadata()] # list[Metadata] | 
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
repository_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the source reference (UUID)

try:
    # Bulk upsert repository metadata
    api_response = api_instance.bulk_upsert_repository_metadata(body, threat_model_id, repository_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->bulk_upsert_repository_metadata: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**list[Metadata]**](Metadata.md)|  | 
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **repository_id** | [**str**](.md)| Unique identifier of the source reference (UUID) | 

### Return type

[**list[Metadata]**](Metadata.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **bulk_upsert_threat_metadata**
> list[Metadata] bulk_upsert_threat_metadata(body, threat_model_id, threat_id)

Bulk upsert threat metadata

Creates or updates multiple metadata key-value pairs for the specified threat

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
body = [tmi_client.Metadata()] # list[Metadata] | 
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
threat_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat (UUID)

try:
    # Bulk upsert threat metadata
    api_response = api_instance.bulk_upsert_threat_metadata(body, threat_model_id, threat_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->bulk_upsert_threat_metadata: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**list[Metadata]**](Metadata.md)|  | 
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **threat_id** | [**str**](.md)| Unique identifier of the threat (UUID) | 

### Return type

[**list[Metadata]**](Metadata.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **bulk_upsert_threat_model_asset_metadata**
> list[Metadata] bulk_upsert_threat_model_asset_metadata(body, threat_model_id, asset_id)

Bulk upsert asset metadata

Creates or updates multiple metadata key-value pairs for the specified asset

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
body = [tmi_client.Metadata()] # list[Metadata] | 
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
asset_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the asset (UUID)

try:
    # Bulk upsert asset metadata
    api_response = api_instance.bulk_upsert_threat_model_asset_metadata(body, threat_model_id, asset_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->bulk_upsert_threat_model_asset_metadata: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**list[Metadata]**](Metadata.md)|  | 
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **asset_id** | [**str**](.md)| Unique identifier of the asset (UUID) | 

### Return type

[**list[Metadata]**](Metadata.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **bulk_upsert_threat_model_assets**
> list[Asset] bulk_upsert_threat_model_assets(body, threat_model_id)

Bulk upsert assets

Create or update multiple assets in a single request

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
body = [tmi_client.Asset()] # list[Asset] | 
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)

try:
    # Bulk upsert assets
    api_response = api_instance.bulk_upsert_threat_model_assets(body, threat_model_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->bulk_upsert_threat_model_assets: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**list[Asset]**](Asset.md)|  | 
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 

### Return type

[**list[Asset]**](Asset.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **bulk_upsert_threat_model_documents**
> list[Document] bulk_upsert_threat_model_documents(body, threat_model_id)

Bulk upsert documents

Create or update multiple documents in a single request

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
body = [tmi_client.Document()] # list[Document] | 
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)

try:
    # Bulk upsert documents
    api_response = api_instance.bulk_upsert_threat_model_documents(body, threat_model_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->bulk_upsert_threat_model_documents: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**list[Document]**](Document.md)|  | 
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 

### Return type

[**list[Document]**](Document.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **bulk_upsert_threat_model_metadata**
> list[Metadata] bulk_upsert_threat_model_metadata(body, threat_model_id)

Bulk upsert threat model metadata

Creates or updates multiple metadata key-value pairs for the specified threat model

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
body = [tmi_client.Metadata()] # list[Metadata] | 
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)

try:
    # Bulk upsert threat model metadata
    api_response = api_instance.bulk_upsert_threat_model_metadata(body, threat_model_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->bulk_upsert_threat_model_metadata: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**list[Metadata]**](Metadata.md)|  | 
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 

### Return type

[**list[Metadata]**](Metadata.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **bulk_upsert_threat_model_repositories**
> list[Repository] bulk_upsert_threat_model_repositories(body, threat_model_id)

Bulk upsert repositories

Create or update multiple repositories in a single request

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
body = [tmi_client.Repository()] # list[Repository] | 
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)

try:
    # Bulk upsert repositories
    api_response = api_instance.bulk_upsert_threat_model_repositories(body, threat_model_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->bulk_upsert_threat_model_repositories: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**list[Repository]**](Repository.md)|  | 
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 

### Return type

[**list[Repository]**](Repository.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **create_diagram_metadata**
> Metadata create_diagram_metadata(body, threat_model_id, diagram_id)

Create diagram metadata

Creates a new metadata entry for the specified diagram

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
body = tmi_client.Metadata() # Metadata | 
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
diagram_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the diagram (UUID)

try:
    # Create diagram metadata
    api_response = api_instance.create_diagram_metadata(body, threat_model_id, diagram_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->create_diagram_metadata: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**Metadata**](Metadata.md)|  | 
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **diagram_id** | [**str**](.md)| Unique identifier of the diagram (UUID) | 

### Return type

[**Metadata**](Metadata.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **create_document_metadata**
> Metadata create_document_metadata(body, threat_model_id, document_id)

Create document metadata

Creates new metadata entry for the specified document

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
body = tmi_client.Metadata() # Metadata | 
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
document_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the document (UUID)

try:
    # Create document metadata
    api_response = api_instance.create_document_metadata(body, threat_model_id, document_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->create_document_metadata: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**Metadata**](Metadata.md)|  | 
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **document_id** | [**str**](.md)| Unique identifier of the document (UUID) | 

### Return type

[**Metadata**](Metadata.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **create_note_metadata**
> Metadata create_note_metadata(body, threat_model_id, note_id)

Create note metadata

Creates new metadata entry for the specified note

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
body = tmi_client.Metadata() # Metadata | 
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
note_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the note (UUID)

try:
    # Create note metadata
    api_response = api_instance.create_note_metadata(body, threat_model_id, note_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->create_note_metadata: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**Metadata**](Metadata.md)|  | 
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **note_id** | [**str**](.md)| Unique identifier of the note (UUID) | 

### Return type

[**Metadata**](Metadata.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **create_repository_metadata**
> Metadata create_repository_metadata(body, threat_model_id, repository_id)

Create source metadata

Creates new metadata entry for the specified source reference

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
body = tmi_client.Metadata() # Metadata | 
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
repository_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the source reference (UUID)

try:
    # Create source metadata
    api_response = api_instance.create_repository_metadata(body, threat_model_id, repository_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->create_repository_metadata: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**Metadata**](Metadata.md)|  | 
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **repository_id** | [**str**](.md)| Unique identifier of the source reference (UUID) | 

### Return type

[**Metadata**](Metadata.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **create_threat_metadata**
> Metadata create_threat_metadata(body, threat_model_id, threat_id)

Create threat metadata

Creates new metadata entry for the specified threat

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
body = tmi_client.Metadata() # Metadata | 
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
threat_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat (UUID)

try:
    # Create threat metadata
    api_response = api_instance.create_threat_metadata(body, threat_model_id, threat_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->create_threat_metadata: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**Metadata**](Metadata.md)|  | 
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **threat_id** | [**str**](.md)| Unique identifier of the threat (UUID) | 

### Return type

[**Metadata**](Metadata.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **create_threat_model_asset**
> Asset create_threat_model_asset(body, threat_model_id)

Create a new asset

Creates a new asset within the specified threat model

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
body = tmi_client.AssetInput() # AssetInput | 
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)

try:
    # Create a new asset
    api_response = api_instance.create_threat_model_asset(body, threat_model_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->create_threat_model_asset: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**AssetInput**](AssetInput.md)|  | 
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 

### Return type

[**Asset**](Asset.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **create_threat_model_asset_metadata**
> Metadata create_threat_model_asset_metadata(body, threat_model_id, asset_id)

Add metadata to an asset

Adds a new metadata key-value pair to the specified asset

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
body = tmi_client.Metadata() # Metadata | 
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
asset_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the asset (UUID)

try:
    # Add metadata to an asset
    api_response = api_instance.create_threat_model_asset_metadata(body, threat_model_id, asset_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->create_threat_model_asset_metadata: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**Metadata**](Metadata.md)|  | 
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **asset_id** | [**str**](.md)| Unique identifier of the asset (UUID) | 

### Return type

[**Metadata**](Metadata.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **create_threat_model_diagram**
> Diagram create_threat_model_diagram(body, threat_model_id)

Create a new diagram

Creates a new diagram within the specified threat model

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
body = tmi_client.CreateDiagramRequest() # CreateDiagramRequest | 
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)

try:
    # Create a new diagram
    api_response = api_instance.create_threat_model_diagram(body, threat_model_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->create_threat_model_diagram: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**CreateDiagramRequest**](CreateDiagramRequest.md)|  | 
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 

### Return type

[**Diagram**](Diagram.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **create_threat_model_document**
> Document create_threat_model_document(body, threat_model_id)

Create a new document

Creates a new document within the specified threat model

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
body = tmi_client.DocumentInput() # DocumentInput | 
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)

try:
    # Create a new document
    api_response = api_instance.create_threat_model_document(body, threat_model_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->create_threat_model_document: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**DocumentInput**](DocumentInput.md)|  | 
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 

### Return type

[**Document**](Document.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **create_threat_model_metadata**
> Metadata create_threat_model_metadata(body, threat_model_id)

Create threat model metadata

Creates new metadata entry for the specified threat model

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
body = tmi_client.Metadata() # Metadata | 
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)

try:
    # Create threat model metadata
    api_response = api_instance.create_threat_model_metadata(body, threat_model_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->create_threat_model_metadata: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**Metadata**](Metadata.md)|  | 
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 

### Return type

[**Metadata**](Metadata.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **create_threat_model_note**
> Note create_threat_model_note(body, threat_model_id)

Create a new note

Creates a new note within the specified threat model

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
body = tmi_client.NoteInput() # NoteInput | 
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)

try:
    # Create a new note
    api_response = api_instance.create_threat_model_note(body, threat_model_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->create_threat_model_note: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**NoteInput**](NoteInput.md)|  | 
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 

### Return type

[**Note**](Note.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **create_threat_model_repository**
> Repository create_threat_model_repository(body, threat_model_id)

Create a new source reference

Creates a new source code reference within the specified threat model

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
body = tmi_client.RepositoryInput() # RepositoryInput | 
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)

try:
    # Create a new source reference
    api_response = api_instance.create_threat_model_repository(body, threat_model_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->create_threat_model_repository: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**RepositoryInput**](RepositoryInput.md)|  | 
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 

### Return type

[**Repository**](Repository.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **create_threat_model_threat**
> Threat create_threat_model_threat(body, threat_model_id)

Create a new threat

Creates a new threat within the specified threat model

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
body = tmi_client.ThreatInput() # ThreatInput | 
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)

try:
    # Create a new threat
    api_response = api_instance.create_threat_model_threat(body, threat_model_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->create_threat_model_threat: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**ThreatInput**](ThreatInput.md)|  | 
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 

### Return type

[**Threat**](Threat.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **delete_diagram_metadata_by_key**
> delete_diagram_metadata_by_key(threat_model_id, diagram_id, key)

Delete diagram metadata by key

Removes a specific metadata entry for the diagram by key

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
diagram_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the diagram (UUID)
key = 'key_example' # str | Metadata key to delete

try:
    # Delete diagram metadata by key
    api_instance.delete_diagram_metadata_by_key(threat_model_id, diagram_id, key)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->delete_diagram_metadata_by_key: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **diagram_id** | [**str**](.md)| Unique identifier of the diagram (UUID) | 
 **key** | **str**| Metadata key to delete | 

### Return type

void (empty response body)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **delete_document_metadata_by_key**
> delete_document_metadata_by_key(threat_model_id, document_id, key)

Delete document metadata by key

Deletes a specific metadata entry by key for the specified document

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
document_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the document (UUID)
key = 'key_example' # str | Metadata key to delete

try:
    # Delete document metadata by key
    api_instance.delete_document_metadata_by_key(threat_model_id, document_id, key)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->delete_document_metadata_by_key: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **document_id** | [**str**](.md)| Unique identifier of the document (UUID) | 
 **key** | **str**| Metadata key to delete | 

### Return type

void (empty response body)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **delete_note_metadata_by_key**
> delete_note_metadata_by_key(threat_model_id, note_id, key)

Delete note metadata by key

Deletes a specific metadata entry by key for the specified note

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
note_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the note (UUID)
key = 'key_example' # str | Metadata key to delete

try:
    # Delete note metadata by key
    api_instance.delete_note_metadata_by_key(threat_model_id, note_id, key)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->delete_note_metadata_by_key: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **note_id** | [**str**](.md)| Unique identifier of the note (UUID) | 
 **key** | **str**| Metadata key to delete | 

### Return type

void (empty response body)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **delete_repository_metadata_by_key**
> delete_repository_metadata_by_key(threat_model_id, repository_id, key)

Delete source metadata by key

Deletes a specific metadata entry by key for the specified source reference

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
repository_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the source reference (UUID)
key = 'key_example' # str | Metadata key to delete

try:
    # Delete source metadata by key
    api_instance.delete_repository_metadata_by_key(threat_model_id, repository_id, key)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->delete_repository_metadata_by_key: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **repository_id** | [**str**](.md)| Unique identifier of the source reference (UUID) | 
 **key** | **str**| Metadata key to delete | 

### Return type

void (empty response body)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **delete_threat_metadata_by_key**
> delete_threat_metadata_by_key(threat_model_id, threat_id, key)

Delete threat metadata by key

Deletes a specific metadata entry by key for the specified threat

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
threat_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat (UUID)
key = 'key_example' # str | Metadata key to delete

try:
    # Delete threat metadata by key
    api_instance.delete_threat_metadata_by_key(threat_model_id, threat_id, key)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->delete_threat_metadata_by_key: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **threat_id** | [**str**](.md)| Unique identifier of the threat (UUID) | 
 **key** | **str**| Metadata key to delete | 

### Return type

void (empty response body)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **delete_threat_model_asset**
> delete_threat_model_asset(threat_model_id, asset_id)

Delete an asset

Deletes an asset from the specified threat model

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
asset_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the asset (UUID)

try:
    # Delete an asset
    api_instance.delete_threat_model_asset(threat_model_id, asset_id)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->delete_threat_model_asset: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **asset_id** | [**str**](.md)| Unique identifier of the asset (UUID) | 

### Return type

void (empty response body)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **delete_threat_model_asset_metadata**
> delete_threat_model_asset_metadata(threat_model_id, asset_id, key)

Delete asset metadata

Deletes a metadata key-value pair from the specified asset

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
asset_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the asset (UUID)
key = 'key_example' # str | Metadata key

try:
    # Delete asset metadata
    api_instance.delete_threat_model_asset_metadata(threat_model_id, asset_id, key)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->delete_threat_model_asset_metadata: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **asset_id** | [**str**](.md)| Unique identifier of the asset (UUID) | 
 **key** | **str**| Metadata key | 

### Return type

void (empty response body)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **delete_threat_model_diagram**
> delete_threat_model_diagram(threat_model_id, diagram_id)

Delete a diagram

Permanently removes a diagram from the threat model

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
diagram_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the diagram (UUID)

try:
    # Delete a diagram
    api_instance.delete_threat_model_diagram(threat_model_id, diagram_id)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->delete_threat_model_diagram: %s\n" % e)
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

# **delete_threat_model_document**
> delete_threat_model_document(threat_model_id, document_id)

Delete a document

Deletes a specific document from the threat model

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
document_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the document (UUID)

try:
    # Delete a document
    api_instance.delete_threat_model_document(threat_model_id, document_id)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->delete_threat_model_document: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **document_id** | [**str**](.md)| Unique identifier of the document (UUID) | 

### Return type

void (empty response body)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **delete_threat_model_metadata_by_key**
> delete_threat_model_metadata_by_key(threat_model_id, key)

Delete threat model metadata by key

Deletes a specific metadata entry by key for the specified threat model

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
key = 'key_example' # str | Metadata key to delete

try:
    # Delete threat model metadata by key
    api_instance.delete_threat_model_metadata_by_key(threat_model_id, key)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->delete_threat_model_metadata_by_key: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **key** | **str**| Metadata key to delete | 

### Return type

void (empty response body)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **delete_threat_model_note**
> delete_threat_model_note(threat_model_id, note_id)

Delete a note

Deletes a specific note from the threat model

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
note_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the note (UUID)

try:
    # Delete a note
    api_instance.delete_threat_model_note(threat_model_id, note_id)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->delete_threat_model_note: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **note_id** | [**str**](.md)| Unique identifier of the note (UUID) | 

### Return type

void (empty response body)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **delete_threat_model_repository**
> delete_threat_model_repository(threat_model_id, repository_id)

Delete a source reference

Deletes a specific source code reference from the threat model

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
repository_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the source reference (UUID)

try:
    # Delete a source reference
    api_instance.delete_threat_model_repository(threat_model_id, repository_id)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->delete_threat_model_repository: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **repository_id** | [**str**](.md)| Unique identifier of the source reference (UUID) | 

### Return type

void (empty response body)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **delete_threat_model_threat**
> delete_threat_model_threat(threat_model_id, threat_id)

Delete a threat

Deletes a specific threat from the threat model

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
threat_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat (UUID)

try:
    # Delete a threat
    api_instance.delete_threat_model_threat(threat_model_id, threat_id)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->delete_threat_model_threat: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **threat_id** | [**str**](.md)| Unique identifier of the threat (UUID) | 

### Return type

void (empty response body)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **get_diagram_metadata**
> list[Metadata] get_diagram_metadata(threat_model_id, diagram_id)

Get diagram metadata

Retrieves all metadata entries for the specified diagram

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
diagram_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the diagram (UUID)

try:
    # Get diagram metadata
    api_response = api_instance.get_diagram_metadata(threat_model_id, diagram_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->get_diagram_metadata: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **diagram_id** | [**str**](.md)| Unique identifier of the diagram (UUID) | 

### Return type

[**list[Metadata]**](Metadata.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **get_diagram_metadata_by_key**
> Metadata get_diagram_metadata_by_key(threat_model_id, diagram_id, key)

Get diagram metadata by key

Retrieves a specific metadata entry for the diagram by key

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
diagram_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the diagram (UUID)
key = 'key_example' # str | Metadata key to retrieve

try:
    # Get diagram metadata by key
    api_response = api_instance.get_diagram_metadata_by_key(threat_model_id, diagram_id, key)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->get_diagram_metadata_by_key: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **diagram_id** | [**str**](.md)| Unique identifier of the diagram (UUID) | 
 **key** | **str**| Metadata key to retrieve | 

### Return type

[**Metadata**](Metadata.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **get_document_metadata**
> list[Metadata] get_document_metadata(threat_model_id, document_id)

Get document metadata

Returns all metadata key-value pairs for the specified document

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
document_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the document (UUID)

try:
    # Get document metadata
    api_response = api_instance.get_document_metadata(threat_model_id, document_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->get_document_metadata: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **document_id** | [**str**](.md)| Unique identifier of the document (UUID) | 

### Return type

[**list[Metadata]**](Metadata.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **get_document_metadata_by_key**
> Metadata get_document_metadata_by_key(threat_model_id, document_id, key)

Get document metadata by key

Returns a specific metadata entry by key for the specified document

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
document_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the document (UUID)
key = 'key_example' # str | Metadata key to retrieve

try:
    # Get document metadata by key
    api_response = api_instance.get_document_metadata_by_key(threat_model_id, document_id, key)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->get_document_metadata_by_key: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **document_id** | [**str**](.md)| Unique identifier of the document (UUID) | 
 **key** | **str**| Metadata key to retrieve | 

### Return type

[**Metadata**](Metadata.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **get_note_metadata**
> list[Metadata] get_note_metadata(threat_model_id, note_id)

Get note metadata

Returns all metadata key-value pairs for the specified note

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
note_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the note (UUID)

try:
    # Get note metadata
    api_response = api_instance.get_note_metadata(threat_model_id, note_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->get_note_metadata: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **note_id** | [**str**](.md)| Unique identifier of the note (UUID) | 

### Return type

[**list[Metadata]**](Metadata.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **get_note_metadata_by_key**
> Metadata get_note_metadata_by_key(threat_model_id, note_id, key)

Get note metadata by key

Returns a specific metadata entry by key for the specified note

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
note_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the note (UUID)
key = 'key_example' # str | Metadata key to retrieve

try:
    # Get note metadata by key
    api_response = api_instance.get_note_metadata_by_key(threat_model_id, note_id, key)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->get_note_metadata_by_key: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **note_id** | [**str**](.md)| Unique identifier of the note (UUID) | 
 **key** | **str**| Metadata key to retrieve | 

### Return type

[**Metadata**](Metadata.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **get_repository_metadata**
> list[Metadata] get_repository_metadata(threat_model_id, repository_id)

Get source metadata

Returns all metadata key-value pairs for the specified source reference

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
repository_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the source reference (UUID)

try:
    # Get source metadata
    api_response = api_instance.get_repository_metadata(threat_model_id, repository_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->get_repository_metadata: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **repository_id** | [**str**](.md)| Unique identifier of the source reference (UUID) | 

### Return type

[**list[Metadata]**](Metadata.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **get_repository_metadata_by_key**
> Metadata get_repository_metadata_by_key(threat_model_id, repository_id, key)

Get source metadata by key

Returns a specific metadata entry by key for the specified source reference

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
repository_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the source reference (UUID)
key = 'key_example' # str | Metadata key to retrieve

try:
    # Get source metadata by key
    api_response = api_instance.get_repository_metadata_by_key(threat_model_id, repository_id, key)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->get_repository_metadata_by_key: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **repository_id** | [**str**](.md)| Unique identifier of the source reference (UUID) | 
 **key** | **str**| Metadata key to retrieve | 

### Return type

[**Metadata**](Metadata.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **get_threat_metadata**
> list[Metadata] get_threat_metadata(threat_model_id, threat_id)

Get threat metadata

Returns all metadata key-value pairs for the specified threat

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
threat_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat (UUID)

try:
    # Get threat metadata
    api_response = api_instance.get_threat_metadata(threat_model_id, threat_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->get_threat_metadata: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **threat_id** | [**str**](.md)| Unique identifier of the threat (UUID) | 

### Return type

[**list[Metadata]**](Metadata.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **get_threat_metadata_by_key**
> Metadata get_threat_metadata_by_key(threat_model_id, threat_id, key)

Get threat metadata by key

Returns a specific metadata entry by key for the specified threat

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
threat_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat (UUID)
key = 'key_example' # str | Metadata key to retrieve

try:
    # Get threat metadata by key
    api_response = api_instance.get_threat_metadata_by_key(threat_model_id, threat_id, key)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->get_threat_metadata_by_key: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **threat_id** | [**str**](.md)| Unique identifier of the threat (UUID) | 
 **key** | **str**| Metadata key to retrieve | 

### Return type

[**Metadata**](Metadata.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **get_threat_model_asset**
> Asset get_threat_model_asset(threat_model_id, asset_id)

Get a specific asset

Returns a single asset by its ID

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
asset_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the asset (UUID)

try:
    # Get a specific asset
    api_response = api_instance.get_threat_model_asset(threat_model_id, asset_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->get_threat_model_asset: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **asset_id** | [**str**](.md)| Unique identifier of the asset (UUID) | 

### Return type

[**Asset**](Asset.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **get_threat_model_asset_metadata**
> list[Metadata] get_threat_model_asset_metadata(threat_model_id, asset_id)

Get all metadata for an asset

Returns all metadata key-value pairs for the specified asset

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
asset_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the asset (UUID)

try:
    # Get all metadata for an asset
    api_response = api_instance.get_threat_model_asset_metadata(threat_model_id, asset_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->get_threat_model_asset_metadata: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **asset_id** | [**str**](.md)| Unique identifier of the asset (UUID) | 

### Return type

[**list[Metadata]**](Metadata.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **get_threat_model_asset_metadata_by_key**
> Metadata get_threat_model_asset_metadata_by_key(threat_model_id, asset_id, key)

Get specific metadata for an asset

Returns a single metadata value by its key

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
asset_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the asset (UUID)
key = 'key_example' # str | Metadata key

try:
    # Get specific metadata for an asset
    api_response = api_instance.get_threat_model_asset_metadata_by_key(threat_model_id, asset_id, key)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->get_threat_model_asset_metadata_by_key: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **asset_id** | [**str**](.md)| Unique identifier of the asset (UUID) | 
 **key** | **str**| Metadata key | 

### Return type

[**Metadata**](Metadata.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **get_threat_model_assets**
> list[Asset] get_threat_model_assets(threat_model_id, limit=limit, offset=offset)

List assets in a threat model

Returns a paginated list of assets within the specified threat model

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
limit = 20 # int | Maximum number of assets to return (optional) (default to 20)
offset = 0 # int | Number of assets to skip (optional) (default to 0)

try:
    # List assets in a threat model
    api_response = api_instance.get_threat_model_assets(threat_model_id, limit=limit, offset=offset)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->get_threat_model_assets: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **limit** | **int**| Maximum number of assets to return | [optional] [default to 20]
 **offset** | **int**| Number of assets to skip | [optional] [default to 0]

### Return type

[**list[Asset]**](Asset.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **get_threat_model_diagram**
> Diagram get_threat_model_diagram(threat_model_id, diagram_id)

Get a specific diagram

Retrieves a specific diagram from the threat model

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
diagram_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the diagram (UUID)

try:
    # Get a specific diagram
    api_response = api_instance.get_threat_model_diagram(threat_model_id, diagram_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->get_threat_model_diagram: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **diagram_id** | [**str**](.md)| Unique identifier of the diagram (UUID) | 

### Return type

[**Diagram**](Diagram.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **get_threat_model_diagrams**
> list[DiagramListItem] get_threat_model_diagrams(threat_model_id, limit=limit, offset=offset)

List threat model diagrams

Returns all diagrams associated with a specific threat model

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
limit = 20 # int | Maximum number of items to return (optional) (default to 20)
offset = 0 # int | Number of items to skip (optional) (default to 0)

try:
    # List threat model diagrams
    api_response = api_instance.get_threat_model_diagrams(threat_model_id, limit=limit, offset=offset)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->get_threat_model_diagrams: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **limit** | **int**| Maximum number of items to return | [optional] [default to 20]
 **offset** | **int**| Number of items to skip | [optional] [default to 0]

### Return type

[**list[DiagramListItem]**](DiagramListItem.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **get_threat_model_document**
> Document get_threat_model_document(threat_model_id, document_id)

Get a specific document

Returns details of a specific document within the threat model

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
document_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the document (UUID)

try:
    # Get a specific document
    api_response = api_instance.get_threat_model_document(threat_model_id, document_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->get_threat_model_document: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **document_id** | [**str**](.md)| Unique identifier of the document (UUID) | 

### Return type

[**Document**](Document.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **get_threat_model_documents**
> list[Document] get_threat_model_documents(threat_model_id, limit=limit, offset=offset)

List documents in a threat model

Returns a paginated list of documents within the specified threat model

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
limit = 20 # int | Maximum number of documents to return (optional) (default to 20)
offset = 0 # int | Number of documents to skip (optional) (default to 0)

try:
    # List documents in a threat model
    api_response = api_instance.get_threat_model_documents(threat_model_id, limit=limit, offset=offset)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->get_threat_model_documents: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **limit** | **int**| Maximum number of documents to return | [optional] [default to 20]
 **offset** | **int**| Number of documents to skip | [optional] [default to 0]

### Return type

[**list[Document]**](Document.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **get_threat_model_metadata**
> list[Metadata] get_threat_model_metadata(threat_model_id)

Get threat model metadata

Returns all metadata key-value pairs for the specified threat model

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)

try:
    # Get threat model metadata
    api_response = api_instance.get_threat_model_metadata(threat_model_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->get_threat_model_metadata: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 

### Return type

[**list[Metadata]**](Metadata.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **get_threat_model_metadata_by_key**
> Metadata get_threat_model_metadata_by_key(threat_model_id, key)

Get threat model metadata by key

Returns a specific metadata entry by key for the specified threat model

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
key = 'key_example' # str | Metadata key to retrieve

try:
    # Get threat model metadata by key
    api_response = api_instance.get_threat_model_metadata_by_key(threat_model_id, key)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->get_threat_model_metadata_by_key: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **key** | **str**| Metadata key to retrieve | 

### Return type

[**Metadata**](Metadata.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **get_threat_model_note**
> Note get_threat_model_note(threat_model_id, note_id)

Get a specific note

Returns details of a specific note within the threat model

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
note_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the note (UUID)

try:
    # Get a specific note
    api_response = api_instance.get_threat_model_note(threat_model_id, note_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->get_threat_model_note: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **note_id** | [**str**](.md)| Unique identifier of the note (UUID) | 

### Return type

[**Note**](Note.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **get_threat_model_notes**
> list[NoteListItem] get_threat_model_notes(threat_model_id, limit=limit, offset=offset)

List notes in a threat model

Returns a paginated list of notes within the specified threat model

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
limit = 20 # int | Maximum number of notes to return (optional) (default to 20)
offset = 0 # int | Number of notes to skip (optional) (default to 0)

try:
    # List notes in a threat model
    api_response = api_instance.get_threat_model_notes(threat_model_id, limit=limit, offset=offset)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->get_threat_model_notes: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **limit** | **int**| Maximum number of notes to return | [optional] [default to 20]
 **offset** | **int**| Number of notes to skip | [optional] [default to 0]

### Return type

[**list[NoteListItem]**](NoteListItem.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **get_threat_model_repositories**
> list[Repository] get_threat_model_repositories(threat_model_id, limit=limit, offset=offset)

List sources in a threat model

Returns a paginated list of source code references within the specified threat model

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
limit = 20 # int | Maximum number of sources to return (optional) (default to 20)
offset = 0 # int | Number of sources to skip (optional) (default to 0)

try:
    # List sources in a threat model
    api_response = api_instance.get_threat_model_repositories(threat_model_id, limit=limit, offset=offset)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->get_threat_model_repositories: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **limit** | **int**| Maximum number of sources to return | [optional] [default to 20]
 **offset** | **int**| Number of sources to skip | [optional] [default to 0]

### Return type

[**list[Repository]**](Repository.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **get_threat_model_repository**
> Repository get_threat_model_repository(threat_model_id, repository_id)

Get a specific source reference

Returns details of a specific source code reference within the threat model

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
repository_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the source reference (UUID)

try:
    # Get a specific source reference
    api_response = api_instance.get_threat_model_repository(threat_model_id, repository_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->get_threat_model_repository: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **repository_id** | [**str**](.md)| Unique identifier of the source reference (UUID) | 

### Return type

[**Repository**](Repository.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **get_threat_model_threat**
> Threat get_threat_model_threat(threat_model_id, threat_id)

Get a specific threat

Returns details of a specific threat within the threat model

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
threat_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat (UUID)

try:
    # Get a specific threat
    api_response = api_instance.get_threat_model_threat(threat_model_id, threat_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->get_threat_model_threat: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **threat_id** | [**str**](.md)| Unique identifier of the threat (UUID) | 

### Return type

[**Threat**](Threat.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **get_threat_model_threats**
> list[Threat] get_threat_model_threats(threat_model_id, limit=limit, offset=offset, sort=sort, name=name, description=description, threat_type=threat_type, severity=severity, priority=priority, status=status, diagram_id=diagram_id, cell_id=cell_id, score_gt=score_gt, score_lt=score_lt, score_eq=score_eq, score_ge=score_ge, score_le=score_le, created_after=created_after, created_before=created_before, modified_after=modified_after, modified_before=modified_before)

List threats in a threat model

Returns a paginated list of threats within the specified threat model

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
limit = 20 # int | Maximum number of threats to return (optional) (default to 20)
offset = 0 # int | Number of threats to skip (optional) (default to 0)
sort = 'created_at:desc' # str | Sort order (e.g., created_at:desc, name:asc, severity:desc, score:desc) (optional) (default to created_at:desc)
name = 'name_example' # str | Filter by threat name (partial match) (optional)
description = 'description_example' # str | Filter by threat description (partial match) (optional)
threat_type = 'threat_type_example' # str | Filter by threat type (exact match) (optional)
severity = 'severity_example' # str | Filter by severity level (exact match) (optional)
priority = 'priority_example' # str | Filter by priority (exact match) (optional)
status = 'status_example' # str | Filter by status (exact match) (optional)
diagram_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Filter by diagram ID (exact match) (optional)
cell_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Filter by cell ID (exact match) (optional)
score_gt = 1.2 # float | Filter threats with score greater than this value (optional)
score_lt = 1.2 # float | Filter threats with score less than this value (optional)
score_eq = 1.2 # float | Filter threats with score equal to this value (optional)
score_ge = 1.2 # float | Filter threats with score greater than or equal to this value (optional)
score_le = 1.2 # float | Filter threats with score less than or equal to this value (optional)
created_after = '2013-10-20T19:20:30+01:00' # datetime | Filter threats created after this date (RFC3339 format) (optional)
created_before = '2013-10-20T19:20:30+01:00' # datetime | Filter threats created before this date (RFC3339 format) (optional)
modified_after = '2013-10-20T19:20:30+01:00' # datetime | Filter threats modified after this date (RFC3339 format) (optional)
modified_before = '2013-10-20T19:20:30+01:00' # datetime | Filter threats modified before this date (RFC3339 format) (optional)

try:
    # List threats in a threat model
    api_response = api_instance.get_threat_model_threats(threat_model_id, limit=limit, offset=offset, sort=sort, name=name, description=description, threat_type=threat_type, severity=severity, priority=priority, status=status, diagram_id=diagram_id, cell_id=cell_id, score_gt=score_gt, score_lt=score_lt, score_eq=score_eq, score_ge=score_ge, score_le=score_le, created_after=created_after, created_before=created_before, modified_after=modified_after, modified_before=modified_before)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->get_threat_model_threats: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **limit** | **int**| Maximum number of threats to return | [optional] [default to 20]
 **offset** | **int**| Number of threats to skip | [optional] [default to 0]
 **sort** | **str**| Sort order (e.g., created_at:desc, name:asc, severity:desc, score:desc) | [optional] [default to created_at:desc]
 **name** | **str**| Filter by threat name (partial match) | [optional] 
 **description** | **str**| Filter by threat description (partial match) | [optional] 
 **threat_type** | **str**| Filter by threat type (exact match) | [optional] 
 **severity** | **str**| Filter by severity level (exact match) | [optional] 
 **priority** | **str**| Filter by priority (exact match) | [optional] 
 **status** | **str**| Filter by status (exact match) | [optional] 
 **diagram_id** | [**str**](.md)| Filter by diagram ID (exact match) | [optional] 
 **cell_id** | [**str**](.md)| Filter by cell ID (exact match) | [optional] 
 **score_gt** | **float**| Filter threats with score greater than this value | [optional] 
 **score_lt** | **float**| Filter threats with score less than this value | [optional] 
 **score_eq** | **float**| Filter threats with score equal to this value | [optional] 
 **score_ge** | **float**| Filter threats with score greater than or equal to this value | [optional] 
 **score_le** | **float**| Filter threats with score less than or equal to this value | [optional] 
 **created_after** | **datetime**| Filter threats created after this date (RFC3339 format) | [optional] 
 **created_before** | **datetime**| Filter threats created before this date (RFC3339 format) | [optional] 
 **modified_after** | **datetime**| Filter threats modified after this date (RFC3339 format) | [optional] 
 **modified_before** | **datetime**| Filter threats modified before this date (RFC3339 format) | [optional] 

### Return type

[**list[Threat]**](Threat.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **patch_threat_model_diagram**
> Diagram patch_threat_model_diagram(body, threat_model_id, diagram_id)

Partially update a diagram

Apply JSON Patch operations to update specific parts of a diagram

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
body = [tmi_client.ThreatsThreatIdBody()] # list[ThreatsThreatIdBody] | 
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
diagram_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the diagram (UUID)

try:
    # Partially update a diagram
    api_response = api_instance.patch_threat_model_diagram(body, threat_model_id, diagram_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->patch_threat_model_diagram: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**list[ThreatsThreatIdBody]**](ThreatsThreatIdBody.md)|  | 
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **diagram_id** | [**str**](.md)| Unique identifier of the diagram (UUID) | 

### Return type

[**Diagram**](Diagram.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json-patch+json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **patch_threat_model_threat**
> Threat patch_threat_model_threat(body, threat_model_id, threat_id)

Partially update a threat

Applies JSON patch operations to a specific threat within the threat model

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
body = [tmi_client.ThreatsThreatIdBody()] # list[ThreatsThreatIdBody] | 
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
threat_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat (UUID)

try:
    # Partially update a threat
    api_response = api_instance.patch_threat_model_threat(body, threat_model_id, threat_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->patch_threat_model_threat: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**list[ThreatsThreatIdBody]**](ThreatsThreatIdBody.md)|  | 
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **threat_id** | [**str**](.md)| Unique identifier of the threat (UUID) | 

### Return type

[**Threat**](Threat.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json-patch+json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **update_diagram_metadata_by_key**
> Metadata update_diagram_metadata_by_key(body, threat_model_id, diagram_id, key)

Update diagram metadata by key

Updates or creates a metadata entry for the diagram with the specified key

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
body = tmi_client.MetadataKeyBody4() # MetadataKeyBody4 | 
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
diagram_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the diagram (UUID)
key = 'key_example' # str | Metadata key to update

try:
    # Update diagram metadata by key
    api_response = api_instance.update_diagram_metadata_by_key(body, threat_model_id, diagram_id, key)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->update_diagram_metadata_by_key: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**MetadataKeyBody4**](MetadataKeyBody4.md)|  | 
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **diagram_id** | [**str**](.md)| Unique identifier of the diagram (UUID) | 
 **key** | **str**| Metadata key to update | 

### Return type

[**Metadata**](Metadata.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **update_document_metadata_by_key**
> Metadata update_document_metadata_by_key(body, threat_model_id, document_id, key)

Update document metadata by key

Updates a specific metadata entry by key for the specified document

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
body = tmi_client.MetadataKeyBody1() # MetadataKeyBody1 | 
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
document_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the document (UUID)
key = 'key_example' # str | Metadata key to update

try:
    # Update document metadata by key
    api_response = api_instance.update_document_metadata_by_key(body, threat_model_id, document_id, key)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->update_document_metadata_by_key: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**MetadataKeyBody1**](MetadataKeyBody1.md)|  | 
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **document_id** | [**str**](.md)| Unique identifier of the document (UUID) | 
 **key** | **str**| Metadata key to update | 

### Return type

[**Metadata**](Metadata.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **update_note_metadata_by_key**
> Metadata update_note_metadata_by_key(body, threat_model_id, note_id, key)

Update note metadata by key

Updates a specific metadata entry by key for the specified note

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
body = tmi_client.MetadataKeyBody5() # MetadataKeyBody5 | 
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
note_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the note (UUID)
key = 'key_example' # str | Metadata key to update

try:
    # Update note metadata by key
    api_response = api_instance.update_note_metadata_by_key(body, threat_model_id, note_id, key)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->update_note_metadata_by_key: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**MetadataKeyBody5**](MetadataKeyBody5.md)|  | 
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **note_id** | [**str**](.md)| Unique identifier of the note (UUID) | 
 **key** | **str**| Metadata key to update | 

### Return type

[**Metadata**](Metadata.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **update_repository_metadata_by_key**
> Metadata update_repository_metadata_by_key(body, threat_model_id, repository_id, key)

Update source metadata by key

Updates a specific metadata entry by key for the specified source reference

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
body = tmi_client.MetadataKeyBody2() # MetadataKeyBody2 | 
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
repository_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the source reference (UUID)
key = 'key_example' # str | Metadata key to update

try:
    # Update source metadata by key
    api_response = api_instance.update_repository_metadata_by_key(body, threat_model_id, repository_id, key)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->update_repository_metadata_by_key: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**MetadataKeyBody2**](MetadataKeyBody2.md)|  | 
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **repository_id** | [**str**](.md)| Unique identifier of the source reference (UUID) | 
 **key** | **str**| Metadata key to update | 

### Return type

[**Metadata**](Metadata.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **update_threat_metadata_by_key**
> Metadata update_threat_metadata_by_key(body, threat_model_id, threat_id, key)

Update threat metadata by key

Updates a specific metadata entry by key for the specified threat

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
body = tmi_client.MetadataKeyBody() # MetadataKeyBody | 
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
threat_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat (UUID)
key = 'key_example' # str | Metadata key to update

try:
    # Update threat metadata by key
    api_response = api_instance.update_threat_metadata_by_key(body, threat_model_id, threat_id, key)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->update_threat_metadata_by_key: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**MetadataKeyBody**](MetadataKeyBody.md)|  | 
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **threat_id** | [**str**](.md)| Unique identifier of the threat (UUID) | 
 **key** | **str**| Metadata key to update | 

### Return type

[**Metadata**](Metadata.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **update_threat_model_asset**
> Asset update_threat_model_asset(body, threat_model_id, asset_id)

Update an asset

Updates an existing asset within the specified threat model

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
body = tmi_client.AssetInput() # AssetInput | 
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
asset_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the asset (UUID)

try:
    # Update an asset
    api_response = api_instance.update_threat_model_asset(body, threat_model_id, asset_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->update_threat_model_asset: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**AssetInput**](AssetInput.md)|  | 
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **asset_id** | [**str**](.md)| Unique identifier of the asset (UUID) | 

### Return type

[**Asset**](Asset.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **update_threat_model_asset_metadata**
> Metadata update_threat_model_asset_metadata(body, threat_model_id, asset_id, key)

Update asset metadata

Updates an existing metadata value by its key

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
body = tmi_client.Metadata() # Metadata | 
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
asset_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the asset (UUID)
key = 'key_example' # str | Metadata key

try:
    # Update asset metadata
    api_response = api_instance.update_threat_model_asset_metadata(body, threat_model_id, asset_id, key)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->update_threat_model_asset_metadata: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**Metadata**](Metadata.md)|  | 
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **asset_id** | [**str**](.md)| Unique identifier of the asset (UUID) | 
 **key** | **str**| Metadata key | 

### Return type

[**Metadata**](Metadata.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **update_threat_model_diagram**
> Diagram update_threat_model_diagram(body, threat_model_id, diagram_id)

Update a diagram

Completely replaces a diagram with new data

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
body = tmi_client.Diagram() # Diagram | 
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
diagram_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the diagram (UUID)

try:
    # Update a diagram
    api_response = api_instance.update_threat_model_diagram(body, threat_model_id, diagram_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->update_threat_model_diagram: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**Diagram**](Diagram.md)|  | 
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **diagram_id** | [**str**](.md)| Unique identifier of the diagram (UUID) | 

### Return type

[**Diagram**](Diagram.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **update_threat_model_document**
> Document update_threat_model_document(body, threat_model_id, document_id)

Update a document

Updates a specific document within the threat model

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
body = tmi_client.DocumentInput() # DocumentInput | 
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
document_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the document (UUID)

try:
    # Update a document
    api_response = api_instance.update_threat_model_document(body, threat_model_id, document_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->update_threat_model_document: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**DocumentInput**](DocumentInput.md)|  | 
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **document_id** | [**str**](.md)| Unique identifier of the document (UUID) | 

### Return type

[**Document**](Document.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **update_threat_model_metadata_by_key**
> Metadata update_threat_model_metadata_by_key(body, threat_model_id, key)

Update threat model metadata by key

Updates a specific metadata entry by key for the specified threat model

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
body = tmi_client.MetadataKeyBody3() # MetadataKeyBody3 | 
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
key = 'key_example' # str | Metadata key to update

try:
    # Update threat model metadata by key
    api_response = api_instance.update_threat_model_metadata_by_key(body, threat_model_id, key)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->update_threat_model_metadata_by_key: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**MetadataKeyBody3**](MetadataKeyBody3.md)|  | 
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **key** | **str**| Metadata key to update | 

### Return type

[**Metadata**](Metadata.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **update_threat_model_note**
> Note update_threat_model_note(body, threat_model_id, note_id)

Update a note

Updates a specific note within the threat model

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
body = tmi_client.NoteInput() # NoteInput | 
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
note_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the note (UUID)

try:
    # Update a note
    api_response = api_instance.update_threat_model_note(body, threat_model_id, note_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->update_threat_model_note: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**NoteInput**](NoteInput.md)|  | 
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **note_id** | [**str**](.md)| Unique identifier of the note (UUID) | 

### Return type

[**Note**](Note.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **update_threat_model_repository**
> Repository update_threat_model_repository(body, threat_model_id, repository_id)

Update a source reference

Updates a specific source code reference within the threat model

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
body = tmi_client.RepositoryInput() # RepositoryInput | 
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
repository_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the source reference (UUID)

try:
    # Update a source reference
    api_response = api_instance.update_threat_model_repository(body, threat_model_id, repository_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->update_threat_model_repository: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**RepositoryInput**](RepositoryInput.md)|  | 
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **repository_id** | [**str**](.md)| Unique identifier of the source reference (UUID) | 

### Return type

[**Repository**](Repository.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **update_threat_model_threat**
> Threat update_threat_model_threat(body, threat_model_id, threat_id)

Update a threat

Updates a specific threat within the threat model

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.ThreatModelSubResourcesApi(tmi_client.ApiClient(configuration))
body = tmi_client.ThreatInput() # ThreatInput | 
threat_model_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat model (UUID)
threat_id = '38400000-8cf0-11bd-b23e-10b96e4ef00d' # str | Unique identifier of the threat (UUID)

try:
    # Update a threat
    api_response = api_instance.update_threat_model_threat(body, threat_model_id, threat_id)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling ThreatModelSubResourcesApi->update_threat_model_threat: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**ThreatInput**](ThreatInput.md)|  | 
 **threat_model_id** | [**str**](.md)| Unique identifier of the threat model (UUID) | 
 **threat_id** | [**str**](.md)| Unique identifier of the threat (UUID) | 

### Return type

[**Threat**](Threat.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

