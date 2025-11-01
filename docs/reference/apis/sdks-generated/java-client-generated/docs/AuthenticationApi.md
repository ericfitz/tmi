# AuthenticationApi

All URIs are relative to *http://localhost:8080*

Method | HTTP request | Description
------------- | ------------- | -------------
[**authorizeOAuthProvider**](AuthenticationApi.md#authorizeOAuthProvider) | **GET** /oauth2/authorize | Initiate OAuth authorization flow
[**exchangeOAuthCode**](AuthenticationApi.md#exchangeOAuthCode) | **POST** /oauth2/token | Exchange OAuth authorization code for JWT tokens
[**getAuthProviders**](AuthenticationApi.md#getAuthProviders) | **GET** /oauth2/providers | List available OAuth providers
[**getCurrentUser**](AuthenticationApi.md#getCurrentUser) | **GET** /oauth2/userinfo | Get current user information
[**getCurrentUserProfile**](AuthenticationApi.md#getCurrentUserProfile) | **GET** /users/me | Get current user profile
[**getProviderGroups**](AuthenticationApi.md#getProviderGroups) | **GET** /oauth2/providers/{idp}/groups | Get groups for identity provider
[**getSAMLMetadata**](AuthenticationApi.md#getSAMLMetadata) | **GET** /saml/metadata | Get SAML service provider metadata
[**handleOAuthCallback**](AuthenticationApi.md#handleOAuthCallback) | **GET** /oauth2/callback | Handle OAuth callback
[**initiateSAMLLogin**](AuthenticationApi.md#initiateSAMLLogin) | **GET** /saml/login | Initiate SAML authentication
[**introspectToken**](AuthenticationApi.md#introspectToken) | **POST** /oauth2/introspect | Token Introspection
[**logoutUser**](AuthenticationApi.md#logoutUser) | **POST** /oauth2/revoke | Logout user
[**processSAMLLogout**](AuthenticationApi.md#processSAMLLogout) | **GET** /saml/slo | SAML Single Logout
[**processSAMLLogoutPost**](AuthenticationApi.md#processSAMLLogoutPost) | **POST** /saml/slo | SAML Single Logout (POST)
[**processSAMLResponse**](AuthenticationApi.md#processSAMLResponse) | **POST** /saml/acs | SAML Assertion Consumer Service
[**refreshToken**](AuthenticationApi.md#refreshToken) | **POST** /oauth2/refresh | Refresh JWT token

<a name="authorizeOAuthProvider"></a>
# **authorizeOAuthProvider**
> authorizeOAuthProvider(scope, idp, clientCallback, state, loginHint)

Initiate OAuth authorization flow

Redirects user to OAuth provider&#x27;s authorization page. Supports client callback URL for seamless client integration. Generates state parameter for CSRF protection.

### Example
```java
// Import classes:
//import io.swagger.client.ApiException;
//import io.swagger.client.api.AuthenticationApi;


AuthenticationApi apiInstance = new AuthenticationApi();
String scope = "scope_example"; // String | OAuth 2.0 scope parameter. For OpenID Connect, must include \"openid\". Supports \"profile\" and \"email\" scopes. Other scopes are silently ignored. Space-separated values.
String idp = "idp_example"; // String | OAuth provider identifier. Defaults to 'test' provider in non-production builds if not specified.
String clientCallback = "clientCallback_example"; // String | Client callback URL where TMI should redirect after successful OAuth completion with tokens as query parameters. If not provided, tokens are returned as JSON response.
String state = "state_example"; // String | CSRF protection state parameter. Recommended for security. Will be included in the callback response.
String loginHint = "loginHint_example"; // String | User identity hint for test OAuth provider. Allows specifying a desired user identity for testing and automation. Only supported by the test provider (ignored by production providers like Google, GitHub, etc.). Must be 3-20 characters, alphanumeric and hyphens only.
try {
    apiInstance.authorizeOAuthProvider(scope, idp, clientCallback, state, loginHint);
} catch (ApiException e) {
    System.err.println("Exception when calling AuthenticationApi#authorizeOAuthProvider");
    e.printStackTrace();
}
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **scope** | **String**| OAuth 2.0 scope parameter. For OpenID Connect, must include \&quot;openid\&quot;. Supports \&quot;profile\&quot; and \&quot;email\&quot; scopes. Other scopes are silently ignored. Space-separated values. |
 **idp** | **String**| OAuth provider identifier. Defaults to &#x27;test&#x27; provider in non-production builds if not specified. | [optional]
 **clientCallback** | **String**| Client callback URL where TMI should redirect after successful OAuth completion with tokens as query parameters. If not provided, tokens are returned as JSON response. | [optional]
 **state** | **String**| CSRF protection state parameter. Recommended for security. Will be included in the callback response. | [optional]
 **loginHint** | **String**| User identity hint for test OAuth provider. Allows specifying a desired user identity for testing and automation. Only supported by the test provider (ignored by production providers like Google, GitHub, etc.). Must be 3-20 characters, alphanumeric and hyphens only. | [optional]

### Return type

null (empty response body)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

<a name="exchangeOAuthCode"></a>
# **exchangeOAuthCode**
> AuthTokenResponse exchangeOAuthCode(body, idp)

Exchange OAuth authorization code for JWT tokens

Provider-neutral endpoint to exchange OAuth authorization codes for TMI JWT tokens. Supports Google, GitHub, and Microsoft OAuth providers.

### Example
```java
// Import classes:
//import io.swagger.client.ApiException;
//import io.swagger.client.api.AuthenticationApi;


AuthenticationApi apiInstance = new AuthenticationApi();
Oauth2TokenBody body = new Oauth2TokenBody(); // Oauth2TokenBody | 
String idp = "idp_example"; // String | OAuth provider identifier. Defaults to 'test' provider in non-production builds if not specified.
try {
    AuthTokenResponse result = apiInstance.exchangeOAuthCode(body, idp);
    System.out.println(result);
} catch (ApiException e) {
    System.err.println("Exception when calling AuthenticationApi#exchangeOAuthCode");
    e.printStackTrace();
}
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**Oauth2TokenBody**](Oauth2TokenBody.md)|  |
 **idp** | **String**| OAuth provider identifier. Defaults to &#x27;test&#x27; provider in non-production builds if not specified. | [optional]

### Return type

[**AuthTokenResponse**](AuthTokenResponse.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

<a name="getAuthProviders"></a>
# **getAuthProviders**
> InlineResponse2004 getAuthProviders()

List available OAuth providers

Returns a list of configured OAuth providers available for authentication

### Example
```java
// Import classes:
//import io.swagger.client.ApiException;
//import io.swagger.client.api.AuthenticationApi;


AuthenticationApi apiInstance = new AuthenticationApi();
try {
    InlineResponse2004 result = apiInstance.getAuthProviders();
    System.out.println(result);
} catch (ApiException e) {
    System.err.println("Exception when calling AuthenticationApi#getAuthProviders");
    e.printStackTrace();
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

[**InlineResponse2004**](InlineResponse2004.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

<a name="getCurrentUser"></a>
# **getCurrentUser**
> InlineResponse2006 getCurrentUser()

Get current user information

Returns information about the currently authenticated user

### Example
```java
// Import classes:
//import io.swagger.client.ApiClient;
//import io.swagger.client.ApiException;
//import io.swagger.client.Configuration;
//import io.swagger.client.auth.*;
//import io.swagger.client.api.AuthenticationApi;

ApiClient defaultClient = Configuration.getDefaultApiClient();


AuthenticationApi apiInstance = new AuthenticationApi();
try {
    InlineResponse2006 result = apiInstance.getCurrentUser();
    System.out.println(result);
} catch (ApiException e) {
    System.err.println("Exception when calling AuthenticationApi#getCurrentUser");
    e.printStackTrace();
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

[**InlineResponse2006**](InlineResponse2006.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

<a name="getCurrentUserProfile"></a>
# **getCurrentUserProfile**
> User getCurrentUserProfile()

Get current user profile

Returns detailed information about the currently authenticated user including groups and identity provider

### Example
```java
// Import classes:
//import io.swagger.client.ApiClient;
//import io.swagger.client.ApiException;
//import io.swagger.client.Configuration;
//import io.swagger.client.auth.*;
//import io.swagger.client.api.AuthenticationApi;

ApiClient defaultClient = Configuration.getDefaultApiClient();


AuthenticationApi apiInstance = new AuthenticationApi();
try {
    User result = apiInstance.getCurrentUserProfile();
    System.out.println(result);
} catch (ApiException e) {
    System.err.println("Exception when calling AuthenticationApi#getCurrentUserProfile");
    e.printStackTrace();
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

[**User**](User.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

<a name="getProviderGroups"></a>
# **getProviderGroups**
> InlineResponse2005 getProviderGroups(idp)

Get groups for identity provider

Returns groups available from a specific identity provider for autocomplete and discovery

### Example
```java
// Import classes:
//import io.swagger.client.ApiClient;
//import io.swagger.client.ApiException;
//import io.swagger.client.Configuration;
//import io.swagger.client.auth.*;
//import io.swagger.client.api.AuthenticationApi;

ApiClient defaultClient = Configuration.getDefaultApiClient();


AuthenticationApi apiInstance = new AuthenticationApi();
String idp = "idp_example"; // String | Identity provider ID (e.g., saml_okta, saml_azure)
try {
    InlineResponse2005 result = apiInstance.getProviderGroups(idp);
    System.out.println(result);
} catch (ApiException e) {
    System.err.println("Exception when calling AuthenticationApi#getProviderGroups");
    e.printStackTrace();
}
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **idp** | **String**| Identity provider ID (e.g., saml_okta, saml_azure) |

### Return type

[**InlineResponse2005**](InlineResponse2005.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

<a name="getSAMLMetadata"></a>
# **getSAMLMetadata**
> String getSAMLMetadata()

Get SAML service provider metadata

Returns the SP metadata XML for SAML configuration

### Example
```java
// Import classes:
//import io.swagger.client.ApiException;
//import io.swagger.client.api.AuthenticationApi;


AuthenticationApi apiInstance = new AuthenticationApi();
try {
    String result = apiInstance.getSAMLMetadata();
    System.out.println(result);
} catch (ApiException e) {
    System.err.println("Exception when calling AuthenticationApi#getSAMLMetadata");
    e.printStackTrace();
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

**String**

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/samlmetadata+xml, application/json

<a name="handleOAuthCallback"></a>
# **handleOAuthCallback**
> AuthTokenResponse handleOAuthCallback(code, state)

Handle OAuth callback

Exchanges OAuth authorization code for JWT tokens. If client_callback was provided during authorization, redirects to client with tokens. Otherwise returns tokens as JSON response.

### Example
```java
// Import classes:
//import io.swagger.client.ApiException;
//import io.swagger.client.api.AuthenticationApi;


AuthenticationApi apiInstance = new AuthenticationApi();
String code = "code_example"; // String | Authorization code from the OAuth provider
String state = "state_example"; // String | Optional state parameter for CSRF protection
try {
    AuthTokenResponse result = apiInstance.handleOAuthCallback(code, state);
    System.out.println(result);
} catch (ApiException e) {
    System.err.println("Exception when calling AuthenticationApi#handleOAuthCallback");
    e.printStackTrace();
}
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **code** | **String**| Authorization code from the OAuth provider |
 **state** | **String**| Optional state parameter for CSRF protection | [optional]

### Return type

[**AuthTokenResponse**](AuthTokenResponse.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

<a name="initiateSAMLLogin"></a>
# **initiateSAMLLogin**
> initiateSAMLLogin(clientCallback)

Initiate SAML authentication

Starts SAML authentication flow by redirecting to IdP

### Example
```java
// Import classes:
//import io.swagger.client.ApiException;
//import io.swagger.client.api.AuthenticationApi;


AuthenticationApi apiInstance = new AuthenticationApi();
String clientCallback = "clientCallback_example"; // String | Client callback URL to redirect after authentication
try {
    apiInstance.initiateSAMLLogin(clientCallback);
} catch (ApiException e) {
    System.err.println("Exception when calling AuthenticationApi#initiateSAMLLogin");
    e.printStackTrace();
}
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **clientCallback** | **String**| Client callback URL to redirect after authentication | [optional]

### Return type

null (empty response body)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

<a name="introspectToken"></a>
# **introspectToken**
> InlineResponse2003 introspectToken(token, tokenTypeHint)

Token Introspection

Introspects a JWT token to determine its validity and metadata as per RFC 7662

### Example
```java
// Import classes:
//import io.swagger.client.ApiException;
//import io.swagger.client.api.AuthenticationApi;


AuthenticationApi apiInstance = new AuthenticationApi();
String token = "token_example"; // String | 
String tokenTypeHint = "tokenTypeHint_example"; // String | 
try {
    InlineResponse2003 result = apiInstance.introspectToken(token, tokenTypeHint);
    System.out.println(result);
} catch (ApiException e) {
    System.err.println("Exception when calling AuthenticationApi#introspectToken");
    e.printStackTrace();
}
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **token** | **String**|  |
 **tokenTypeHint** | **String**|  |

### Return type

[**InlineResponse2003**](InlineResponse2003.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: application/x-www-form-urlencoded
 - **Accept**: application/json

<a name="logoutUser"></a>
# **logoutUser**
> logoutUser(body)

Logout user

Invalidates the user&#x27;s JWT token by adding it to a blacklist, effectively ending the session. Once logged out, the token cannot be used for further authenticated requests until it naturally expires. The token blacklist is maintained in Redis with automatic cleanup based on token expiration times.

### Example
```java
// Import classes:
//import io.swagger.client.ApiClient;
//import io.swagger.client.ApiException;
//import io.swagger.client.Configuration;
//import io.swagger.client.auth.*;
//import io.swagger.client.api.AuthenticationApi;

ApiClient defaultClient = Configuration.getDefaultApiClient();


AuthenticationApi apiInstance = new AuthenticationApi();
Object body = null; // Object | Empty request body - token is provided via Authorization header
try {
    apiInstance.logoutUser(body);
} catch (ApiException e) {
    System.err.println("Exception when calling AuthenticationApi#logoutUser");
    e.printStackTrace();
}
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**Object**](Object.md)| Empty request body - token is provided via Authorization header | [optional]

### Return type

null (empty response body)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

<a name="processSAMLLogout"></a>
# **processSAMLLogout**
> InlineResponse2008 processSAMLLogout(saMLRequest)

SAML Single Logout

Handles SAML logout requests from IdP

### Example
```java
// Import classes:
//import io.swagger.client.ApiException;
//import io.swagger.client.api.AuthenticationApi;


AuthenticationApi apiInstance = new AuthenticationApi();
String saMLRequest = "saMLRequest_example"; // String | Base64-encoded SAML logout request
try {
    InlineResponse2008 result = apiInstance.processSAMLLogout(saMLRequest);
    System.out.println(result);
} catch (ApiException e) {
    System.err.println("Exception when calling AuthenticationApi#processSAMLLogout");
    e.printStackTrace();
}
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **saMLRequest** | **String**| Base64-encoded SAML logout request |

### Return type

[**InlineResponse2008**](InlineResponse2008.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

<a name="processSAMLLogoutPost"></a>
# **processSAMLLogoutPost**
> InlineResponse2008 processSAMLLogoutPost(saMLRequest)

SAML Single Logout (POST)

Handles SAML logout requests from IdP via POST

### Example
```java
// Import classes:
//import io.swagger.client.ApiException;
//import io.swagger.client.api.AuthenticationApi;


AuthenticationApi apiInstance = new AuthenticationApi();
String saMLRequest = "saMLRequest_example"; // String | 
try {
    InlineResponse2008 result = apiInstance.processSAMLLogoutPost(saMLRequest);
    System.out.println(result);
} catch (ApiException e) {
    System.err.println("Exception when calling AuthenticationApi#processSAMLLogoutPost");
    e.printStackTrace();
}
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **saMLRequest** | **String**|  | [optional]

### Return type

[**InlineResponse2008**](InlineResponse2008.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: application/x-www-form-urlencoded
 - **Accept**: application/json

<a name="processSAMLResponse"></a>
# **processSAMLResponse**
> AuthTokenResponse processSAMLResponse(saMLResponse, relayState)

SAML Assertion Consumer Service

Processes SAML responses from IdP after authentication

### Example
```java
// Import classes:
//import io.swagger.client.ApiException;
//import io.swagger.client.api.AuthenticationApi;


AuthenticationApi apiInstance = new AuthenticationApi();
String saMLResponse = "saMLResponse_example"; // String | 
String relayState = "relayState_example"; // String | 
try {
    AuthTokenResponse result = apiInstance.processSAMLResponse(saMLResponse, relayState);
    System.out.println(result);
} catch (ApiException e) {
    System.err.println("Exception when calling AuthenticationApi#processSAMLResponse");
    e.printStackTrace();
}
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **saMLResponse** | **String**|  | [optional]
 **relayState** | **String**|  | [optional]

### Return type

[**AuthTokenResponse**](AuthTokenResponse.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: application/x-www-form-urlencoded
 - **Accept**: application/json

<a name="refreshToken"></a>
# **refreshToken**
> AuthTokenResponse refreshToken(body)

Refresh JWT token

Exchanges a refresh token for a new JWT access token

### Example
```java
// Import classes:
//import io.swagger.client.ApiException;
//import io.swagger.client.api.AuthenticationApi;


AuthenticationApi apiInstance = new AuthenticationApi();
Oauth2RefreshBody body = new Oauth2RefreshBody(); // Oauth2RefreshBody | 
try {
    AuthTokenResponse result = apiInstance.refreshToken(body);
    System.out.println(result);
} catch (ApiException e) {
    System.err.println("Exception when calling AuthenticationApi#refreshToken");
    e.printStackTrace();
}
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**Oauth2RefreshBody**](Oauth2RefreshBody.md)|  | [optional]

### Return type

[**AuthTokenResponse**](AuthTokenResponse.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

