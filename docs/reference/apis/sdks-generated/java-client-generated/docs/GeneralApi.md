# GeneralApi

All URIs are relative to *http://localhost:8080*

Method | HTTP request | Description
------------- | ------------- | -------------
[**getApiInfo**](GeneralApi.md#getApiInfo) | **GET** / | Get API information

<a name="getApiInfo"></a>
# **getApiInfo**
> ApiInfo getApiInfo()

Get API information

Returns service, API, and operator information without authentication

### Example
```java
// Import classes:
//import io.swagger.client.ApiException;
//import io.swagger.client.api.GeneralApi;


GeneralApi apiInstance = new GeneralApi();
try {
    ApiInfo result = apiInstance.getApiInfo();
    System.out.println(result);
} catch (ApiException e) {
    System.err.println("Exception when calling GeneralApi#getApiInfo");
    e.printStackTrace();
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

[**ApiInfo**](ApiInfo.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

