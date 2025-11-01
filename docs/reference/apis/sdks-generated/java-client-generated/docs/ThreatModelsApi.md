# ThreatModelsApi

All URIs are relative to *http://localhost:8080*

Method | HTTP request | Description
------------- | ------------- | -------------
[**createThreatModel**](ThreatModelsApi.md#createThreatModel) | **POST** /threat_models | Create a threat model
[**deleteThreatModel**](ThreatModelsApi.md#deleteThreatModel) | **DELETE** /threat_models/{threat_model_id} | Delete a threat model
[**getThreatModel**](ThreatModelsApi.md#getThreatModel) | **GET** /threat_models/{threat_model_id} | Retrieve a threat model
[**listThreatModels**](ThreatModelsApi.md#listThreatModels) | **GET** /threat_models | List threat models
[**patchThreatModel**](ThreatModelsApi.md#patchThreatModel) | **PATCH** /threat_models/{threat_model_id} | Partially update a threat model
[**updateThreatModel**](ThreatModelsApi.md#updateThreatModel) | **PUT** /threat_models/{threat_model_id} | Update a threat model

<a name="createThreatModel"></a>
# **createThreatModel**
> ThreatModel createThreatModel(body)

Create a threat model

Creates a new threat model with the authenticated user as owner

### Example
```java
// Import classes:
//import io.swagger.client.ApiClient;
//import io.swagger.client.ApiException;
//import io.swagger.client.Configuration;
//import io.swagger.client.auth.*;
//import io.swagger.client.api.ThreatModelsApi;

ApiClient defaultClient = Configuration.getDefaultApiClient();


ThreatModelsApi apiInstance = new ThreatModelsApi();
ThreatModelInput body = new ThreatModelInput(); // ThreatModelInput | 
try {
    ThreatModel result = apiInstance.createThreatModel(body);
    System.out.println(result);
} catch (ApiException e) {
    System.err.println("Exception when calling ThreatModelsApi#createThreatModel");
    e.printStackTrace();
}
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

<a name="deleteThreatModel"></a>
# **deleteThreatModel**
> deleteThreatModel(threatModelId)

Delete a threat model

Deletes a threat model; restricted to owner role

### Example
```java
// Import classes:
//import io.swagger.client.ApiClient;
//import io.swagger.client.ApiException;
//import io.swagger.client.Configuration;
//import io.swagger.client.auth.*;
//import io.swagger.client.api.ThreatModelsApi;

ApiClient defaultClient = Configuration.getDefaultApiClient();


ThreatModelsApi apiInstance = new ThreatModelsApi();
UUID threatModelId = new UUID(); // UUID | Unique identifier of the threat model to delete (UUID)
try {
    apiInstance.deleteThreatModel(threatModelId);
} catch (ApiException e) {
    System.err.println("Exception when calling ThreatModelsApi#deleteThreatModel");
    e.printStackTrace();
}
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **threatModelId** | [**UUID**](.md)| Unique identifier of the threat model to delete (UUID) |

### Return type

null (empty response body)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

<a name="getThreatModel"></a>
# **getThreatModel**
> ThreatModel getThreatModel(threatModelId)

Retrieve a threat model

Retrieves the full details of a specific threat model if the user has access

### Example
```java
// Import classes:
//import io.swagger.client.ApiClient;
//import io.swagger.client.ApiException;
//import io.swagger.client.Configuration;
//import io.swagger.client.auth.*;
//import io.swagger.client.api.ThreatModelsApi;

ApiClient defaultClient = Configuration.getDefaultApiClient();


ThreatModelsApi apiInstance = new ThreatModelsApi();
UUID threatModelId = new UUID(); // UUID | Unique identifier of the threat model to retrieve (UUID)
try {
    ThreatModel result = apiInstance.getThreatModel(threatModelId);
    System.out.println(result);
} catch (ApiException e) {
    System.err.println("Exception when calling ThreatModelsApi#getThreatModel");
    e.printStackTrace();
}
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **threatModelId** | [**UUID**](.md)| Unique identifier of the threat model to retrieve (UUID) |

### Return type

[**ThreatModel**](ThreatModel.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

<a name="listThreatModels"></a>
# **listThreatModels**
> List&lt;TMListItem&gt; listThreatModels(limit, offset, owner, name, description, issueUri, createdAfter, createdBefore, modifiedAfter, modifiedBefore, status, statusUpdatedAfter, statusUpdatedBefore)

List threat models

Returns a list of threat models accessible to the authenticated user with enhanced metadata and entity counts

### Example
```java
// Import classes:
//import io.swagger.client.ApiClient;
//import io.swagger.client.ApiException;
//import io.swagger.client.Configuration;
//import io.swagger.client.auth.*;
//import io.swagger.client.api.ThreatModelsApi;

ApiClient defaultClient = Configuration.getDefaultApiClient();


ThreatModelsApi apiInstance = new ThreatModelsApi();
Integer limit = 20; // Integer | Number of threat models to return
Integer offset = 0; // Integer | Pagination offset
String owner = "owner_example"; // String | Filter by owner name or email
String name = "name_example"; // String | Filter by threat model name (partial match)
String description = "description_example"; // String | Filter by threat model description (partial match)
String issueUri = "issueUri_example"; // String | Filter by issue URI (partial match)
OffsetDateTime createdAfter = new OffsetDateTime(); // OffsetDateTime | Filter threat models created after this date (RFC3339 format)
OffsetDateTime createdBefore = new OffsetDateTime(); // OffsetDateTime | Filter threat models created before this date (RFC3339 format)
OffsetDateTime modifiedAfter = new OffsetDateTime(); // OffsetDateTime | Filter threat models modified after this date (RFC3339 format)
OffsetDateTime modifiedBefore = new OffsetDateTime(); // OffsetDateTime | Filter threat models modified before this date (RFC3339 format)
String status = "status_example"; // String | Filter by status value (exact match). To filter by multiple statuses, use multiple status parameters or comma-separated values.
OffsetDateTime statusUpdatedAfter = new OffsetDateTime(); // OffsetDateTime | Filter threat models where status was updated after this timestamp (RFC3339)
OffsetDateTime statusUpdatedBefore = new OffsetDateTime(); // OffsetDateTime | Filter threat models where status was updated before this timestamp (RFC3339)
try {
    List<TMListItem> result = apiInstance.listThreatModels(limit, offset, owner, name, description, issueUri, createdAfter, createdBefore, modifiedAfter, modifiedBefore, status, statusUpdatedAfter, statusUpdatedBefore);
    System.out.println(result);
} catch (ApiException e) {
    System.err.println("Exception when calling ThreatModelsApi#listThreatModels");
    e.printStackTrace();
}
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **limit** | **Integer**| Number of threat models to return | [optional] [default to 20]
 **offset** | **Integer**| Pagination offset | [optional] [default to 0]
 **owner** | **String**| Filter by owner name or email | [optional]
 **name** | **String**| Filter by threat model name (partial match) | [optional]
 **description** | **String**| Filter by threat model description (partial match) | [optional]
 **issueUri** | **String**| Filter by issue URI (partial match) | [optional]
 **createdAfter** | **OffsetDateTime**| Filter threat models created after this date (RFC3339 format) | [optional]
 **createdBefore** | **OffsetDateTime**| Filter threat models created before this date (RFC3339 format) | [optional]
 **modifiedAfter** | **OffsetDateTime**| Filter threat models modified after this date (RFC3339 format) | [optional]
 **modifiedBefore** | **OffsetDateTime**| Filter threat models modified before this date (RFC3339 format) | [optional]
 **status** | **String**| Filter by status value (exact match). To filter by multiple statuses, use multiple status parameters or comma-separated values. | [optional]
 **statusUpdatedAfter** | **OffsetDateTime**| Filter threat models where status was updated after this timestamp (RFC3339) | [optional]
 **statusUpdatedBefore** | **OffsetDateTime**| Filter threat models where status was updated before this timestamp (RFC3339) | [optional]

### Return type

[**List&lt;TMListItem&gt;**](TMListItem.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

<a name="patchThreatModel"></a>
# **patchThreatModel**
> ThreatModel patchThreatModel(threatModelId, body)

Partially update a threat model

Applies JSON Patch operations to a threat model; restricted to writer/owner roles

### Example
```java
// Import classes:
//import io.swagger.client.ApiClient;
//import io.swagger.client.ApiException;
//import io.swagger.client.Configuration;
//import io.swagger.client.auth.*;
//import io.swagger.client.api.ThreatModelsApi;

ApiClient defaultClient = Configuration.getDefaultApiClient();


ThreatModelsApi apiInstance = new ThreatModelsApi();
UUID threatModelId = new UUID(); // UUID | Unique identifier of the threat model to update (UUID)
List<ThreatModelsThreatModelIdBody> body = Arrays.asList(new ThreatModelsThreatModelIdBody()); // List<ThreatModelsThreatModelIdBody> | 
try {
    ThreatModel result = apiInstance.patchThreatModel(threatModelId, body);
    System.out.println(result);
} catch (ApiException e) {
    System.err.println("Exception when calling ThreatModelsApi#patchThreatModel");
    e.printStackTrace();
}
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **threatModelId** | [**UUID**](.md)| Unique identifier of the threat model to update (UUID) |
 **body** | [**List&lt;ThreatModelsThreatModelIdBody&gt;**](ThreatModelsThreatModelIdBody.md)|  | [optional]

### Return type

[**ThreatModel**](ThreatModel.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json-patch+json
 - **Accept**: application/json

<a name="updateThreatModel"></a>
# **updateThreatModel**
> ThreatModel updateThreatModel(body, threatModelId)

Update a threat model

Fully updates a threat model; restricted to writer/owner roles

### Example
```java
// Import classes:
//import io.swagger.client.ApiClient;
//import io.swagger.client.ApiException;
//import io.swagger.client.Configuration;
//import io.swagger.client.auth.*;
//import io.swagger.client.api.ThreatModelsApi;

ApiClient defaultClient = Configuration.getDefaultApiClient();


ThreatModelsApi apiInstance = new ThreatModelsApi();
ThreatModelInput body = new ThreatModelInput(); // ThreatModelInput | 
UUID threatModelId = new UUID(); // UUID | Unique identifier of the threat model to update (UUID)
try {
    ThreatModel result = apiInstance.updateThreatModel(body, threatModelId);
    System.out.println(result);
} catch (ApiException e) {
    System.err.println("Exception when calling ThreatModelsApi#updateThreatModel");
    e.printStackTrace();
}
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**ThreatModelInput**](ThreatModelInput.md)|  |
 **threatModelId** | [**UUID**](.md)| Unique identifier of the threat model to update (UUID) |

### Return type

[**ThreatModel**](ThreatModel.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

