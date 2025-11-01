# {{classname}}

All URIs are relative to *http://localhost:8080*

Method | HTTP request | Description
------------- | ------------- | -------------
[**AuthorizeOAuthProvider**](AuthenticationApi.md#AuthorizeOAuthProvider) | **Get** /oauth2/authorize | Initiate OAuth authorization flow
[**ExchangeOAuthCode**](AuthenticationApi.md#ExchangeOAuthCode) | **Post** /oauth2/token | Exchange OAuth authorization code for JWT tokens
[**GetAuthProviders**](AuthenticationApi.md#GetAuthProviders) | **Get** /oauth2/providers | List available OAuth providers
[**GetCurrentUser**](AuthenticationApi.md#GetCurrentUser) | **Get** /oauth2/userinfo | Get current user information
[**GetCurrentUserProfile**](AuthenticationApi.md#GetCurrentUserProfile) | **Get** /users/me | Get current user profile
[**GetProviderGroups**](AuthenticationApi.md#GetProviderGroups) | **Get** /oauth2/providers/{idp}/groups | Get groups for identity provider
[**GetSAMLMetadata**](AuthenticationApi.md#GetSAMLMetadata) | **Get** /saml/metadata | Get SAML service provider metadata
[**HandleOAuthCallback**](AuthenticationApi.md#HandleOAuthCallback) | **Get** /oauth2/callback | Handle OAuth callback
[**InitiateSAMLLogin**](AuthenticationApi.md#InitiateSAMLLogin) | **Get** /saml/login | Initiate SAML authentication
[**IntrospectToken**](AuthenticationApi.md#IntrospectToken) | **Post** /oauth2/introspect | Token Introspection
[**LogoutUser**](AuthenticationApi.md#LogoutUser) | **Post** /oauth2/revoke | Logout user
[**ProcessSAMLLogout**](AuthenticationApi.md#ProcessSAMLLogout) | **Get** /saml/slo | SAML Single Logout
[**ProcessSAMLLogoutPost**](AuthenticationApi.md#ProcessSAMLLogoutPost) | **Post** /saml/slo | SAML Single Logout (POST)
[**ProcessSAMLResponse**](AuthenticationApi.md#ProcessSAMLResponse) | **Post** /saml/acs | SAML Assertion Consumer Service
[**RefreshToken**](AuthenticationApi.md#RefreshToken) | **Post** /oauth2/refresh | Refresh JWT token

# **AuthorizeOAuthProvider**
> AuthorizeOAuthProvider(ctx, scope, optional)
Initiate OAuth authorization flow

Redirects user to OAuth provider's authorization page. Supports client callback URL for seamless client integration. Generates state parameter for CSRF protection.

### Required Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **ctx** | **context.Context** | context for authentication, logging, cancellation, deadlines, tracing, etc.
  **scope** | **string**| OAuth 2.0 scope parameter. For OpenID Connect, must include \&quot;openid\&quot;. Supports \&quot;profile\&quot; and \&quot;email\&quot; scopes. Other scopes are silently ignored. Space-separated values. | 
 **optional** | ***AuthenticationApiAuthorizeOAuthProviderOpts** | optional parameters | nil if no parameters

### Optional Parameters
Optional parameters are passed through a pointer to a AuthenticationApiAuthorizeOAuthProviderOpts struct
Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------

 **idp** | **optional.String**| OAuth provider identifier. Defaults to &#x27;test&#x27; provider in non-production builds if not specified. | 
 **clientCallback** | **optional.String**| Client callback URL where TMI should redirect after successful OAuth completion with tokens as query parameters. If not provided, tokens are returned as JSON response. | 
 **state** | **optional.String**| CSRF protection state parameter. Recommended for security. Will be included in the callback response. | 
 **loginHint** | **optional.String**| User identity hint for test OAuth provider. Allows specifying a desired user identity for testing and automation. Only supported by the test provider (ignored by production providers like Google, GitHub, etc.). Must be 3-20 characters, alphanumeric and hyphens only. | 

### Return type

 (empty response body)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **ExchangeOAuthCode**
> AuthTokenResponse ExchangeOAuthCode(ctx, body, optional)
Exchange OAuth authorization code for JWT tokens

Provider-neutral endpoint to exchange OAuth authorization codes for TMI JWT tokens. Supports Google, GitHub, and Microsoft OAuth providers.

### Required Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **ctx** | **context.Context** | context for authentication, logging, cancellation, deadlines, tracing, etc.
  **body** | [**Oauth2TokenBody**](Oauth2TokenBody.md)|  | 
 **optional** | ***AuthenticationApiExchangeOAuthCodeOpts** | optional parameters | nil if no parameters

### Optional Parameters
Optional parameters are passed through a pointer to a AuthenticationApiExchangeOAuthCodeOpts struct
Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------

 **idp** | **optional.**| OAuth provider identifier. Defaults to &#x27;test&#x27; provider in non-production builds if not specified. | 

### Return type

[**AuthTokenResponse**](AuthTokenResponse.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **GetAuthProviders**
> InlineResponse2004 GetAuthProviders(ctx, )
List available OAuth providers

Returns a list of configured OAuth providers available for authentication

### Required Parameters
This endpoint does not need any parameter.

### Return type

[**InlineResponse2004**](inline_response_200_4.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **GetCurrentUser**
> InlineResponse2006 GetCurrentUser(ctx, )
Get current user information

Returns information about the currently authenticated user

### Required Parameters
This endpoint does not need any parameter.

### Return type

[**InlineResponse2006**](inline_response_200_6.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **GetCurrentUserProfile**
> User GetCurrentUserProfile(ctx, )
Get current user profile

Returns detailed information about the currently authenticated user including groups and identity provider

### Required Parameters
This endpoint does not need any parameter.

### Return type

[**User**](User.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **GetProviderGroups**
> InlineResponse2005 GetProviderGroups(ctx, idp)
Get groups for identity provider

Returns groups available from a specific identity provider for autocomplete and discovery

### Required Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **ctx** | **context.Context** | context for authentication, logging, cancellation, deadlines, tracing, etc.
  **idp** | **string**| Identity provider ID (e.g., saml_okta, saml_azure) | 

### Return type

[**InlineResponse2005**](inline_response_200_5.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **GetSAMLMetadata**
> string GetSAMLMetadata(ctx, )
Get SAML service provider metadata

Returns the SP metadata XML for SAML configuration

### Required Parameters
This endpoint does not need any parameter.

### Return type

**string**

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/samlmetadata+xml, application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **HandleOAuthCallback**
> AuthTokenResponse HandleOAuthCallback(ctx, code, optional)
Handle OAuth callback

Exchanges OAuth authorization code for JWT tokens. If client_callback was provided during authorization, redirects to client with tokens. Otherwise returns tokens as JSON response.

### Required Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **ctx** | **context.Context** | context for authentication, logging, cancellation, deadlines, tracing, etc.
  **code** | **string**| Authorization code from the OAuth provider | 
 **optional** | ***AuthenticationApiHandleOAuthCallbackOpts** | optional parameters | nil if no parameters

### Optional Parameters
Optional parameters are passed through a pointer to a AuthenticationApiHandleOAuthCallbackOpts struct
Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------

 **state** | **optional.String**| Optional state parameter for CSRF protection | 

### Return type

[**AuthTokenResponse**](AuthTokenResponse.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **InitiateSAMLLogin**
> InitiateSAMLLogin(ctx, optional)
Initiate SAML authentication

Starts SAML authentication flow by redirecting to IdP

### Required Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **ctx** | **context.Context** | context for authentication, logging, cancellation, deadlines, tracing, etc.
 **optional** | ***AuthenticationApiInitiateSAMLLoginOpts** | optional parameters | nil if no parameters

### Optional Parameters
Optional parameters are passed through a pointer to a AuthenticationApiInitiateSAMLLoginOpts struct
Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **clientCallback** | **optional.String**| Client callback URL to redirect after authentication | 

### Return type

 (empty response body)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **IntrospectToken**
> InlineResponse2003 IntrospectToken(ctx, token, tokenTypeHint)
Token Introspection

Introspects a JWT token to determine its validity and metadata as per RFC 7662

### Required Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **ctx** | **context.Context** | context for authentication, logging, cancellation, deadlines, tracing, etc.
  **token** | **string**|  | 
  **tokenTypeHint** | **string**|  | 

### Return type

[**InlineResponse2003**](inline_response_200_3.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: application/x-www-form-urlencoded
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **LogoutUser**
> LogoutUser(ctx, optional)
Logout user

Invalidates the user's JWT token by adding it to a blacklist, effectively ending the session. Once logged out, the token cannot be used for further authenticated requests until it naturally expires. The token blacklist is maintained in Redis with automatic cleanup based on token expiration times.

### Required Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **ctx** | **context.Context** | context for authentication, logging, cancellation, deadlines, tracing, etc.
 **optional** | ***AuthenticationApiLogoutUserOpts** | optional parameters | nil if no parameters

### Optional Parameters
Optional parameters are passed through a pointer to a AuthenticationApiLogoutUserOpts struct
Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**optional.Interface of interface{}**](interface{}.md)| Empty request body - token is provided via Authorization header | 

### Return type

 (empty response body)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **ProcessSAMLLogout**
> InlineResponse2008 ProcessSAMLLogout(ctx, sAMLRequest)
SAML Single Logout

Handles SAML logout requests from IdP

### Required Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **ctx** | **context.Context** | context for authentication, logging, cancellation, deadlines, tracing, etc.
  **sAMLRequest** | **string**| Base64-encoded SAML logout request | 

### Return type

[**InlineResponse2008**](inline_response_200_8.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **ProcessSAMLLogoutPost**
> InlineResponse2008 ProcessSAMLLogoutPost(ctx, optional)
SAML Single Logout (POST)

Handles SAML logout requests from IdP via POST

### Required Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **ctx** | **context.Context** | context for authentication, logging, cancellation, deadlines, tracing, etc.
 **optional** | ***AuthenticationApiProcessSAMLLogoutPostOpts** | optional parameters | nil if no parameters

### Optional Parameters
Optional parameters are passed through a pointer to a AuthenticationApiProcessSAMLLogoutPostOpts struct
Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **sAMLRequest** | **optional.**|  | 

### Return type

[**InlineResponse2008**](inline_response_200_8.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: application/x-www-form-urlencoded
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **ProcessSAMLResponse**
> AuthTokenResponse ProcessSAMLResponse(ctx, optional)
SAML Assertion Consumer Service

Processes SAML responses from IdP after authentication

### Required Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **ctx** | **context.Context** | context for authentication, logging, cancellation, deadlines, tracing, etc.
 **optional** | ***AuthenticationApiProcessSAMLResponseOpts** | optional parameters | nil if no parameters

### Optional Parameters
Optional parameters are passed through a pointer to a AuthenticationApiProcessSAMLResponseOpts struct
Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **sAMLResponse** | **optional.**|  | 
 **relayState** | **optional.**|  | 

### Return type

[**AuthTokenResponse**](AuthTokenResponse.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: application/x-www-form-urlencoded
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **RefreshToken**
> AuthTokenResponse RefreshToken(ctx, optional)
Refresh JWT token

Exchanges a refresh token for a new JWT access token

### Required Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **ctx** | **context.Context** | context for authentication, logging, cancellation, deadlines, tracing, etc.
 **optional** | ***AuthenticationApiRefreshTokenOpts** | optional parameters | nil if no parameters

### Optional Parameters
Optional parameters are passed through a pointer to a AuthenticationApiRefreshTokenOpts struct
Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**optional.Interface of Oauth2RefreshBody**](Oauth2RefreshBody.md)|  | 

### Return type

[**AuthTokenResponse**](AuthTokenResponse.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

