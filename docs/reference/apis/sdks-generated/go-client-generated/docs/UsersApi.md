# {{classname}}

All URIs are relative to *http://localhost:8080*

Method | HTTP request | Description
------------- | ------------- | -------------
[**DeleteUserAccount**](UsersApi.md#DeleteUserAccount) | **Delete** /users/me | Delete authenticated user account and all data

# **DeleteUserAccount**
> DeletionChallenge DeleteUserAccount(ctx, optional)
Delete authenticated user account and all data

Two-step deletion process: 1. First call (no challenge parameter) - Returns challenge string 2. Second call (with challenge parameter) - Confirms deletion  Ownership behavior: - Shared threat models: Transfers ownership to another owner from authorization list - Unshared threat models: Deletes completely along with all child resources

### Required Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **ctx** | **context.Context** | context for authentication, logging, cancellation, deadlines, tracing, etc.
 **optional** | ***UsersApiDeleteUserAccountOpts** | optional parameters | nil if no parameters

### Optional Parameters
Optional parameters are passed through a pointer to a UsersApiDeleteUserAccountOpts struct
Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **challenge** | **optional.String**| Challenge string from first request (step 2 only). Must match exactly. | 

### Return type

[**DeletionChallenge**](DeletionChallenge.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

