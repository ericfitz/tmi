# TmiClient.GeneralApi

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
```javascript
import {TmiClient} from 'tmi-client';

let apiInstance = new TmiClient.GeneralApi();
apiInstance.getApiInfo((error, data, response) => {
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

[**ApiInfo**](ApiInfo.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

