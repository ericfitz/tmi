# TmiClient.AuthenticationApi

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
> authorizeOAuthProvider(scope, opts)

Initiate OAuth authorization flow

Redirects user to OAuth provider&#x27;s authorization page. Supports client callback URL for seamless client integration. Generates state parameter for CSRF protection.

### Example
```javascript
import {TmiClient} from 'tmi-client';

let apiInstance = new TmiClient.AuthenticationApi();
let scope = "scope_example"; // String | OAuth 2.0 scope parameter. For OpenID Connect, must include \"openid\". Supports \"profile\" and \"email\" scopes. Other scopes are silently ignored. Space-separated values.
let opts = { 
  'idp': "idp_example", // String | OAuth provider identifier. Defaults to 'test' provider in non-production builds if not specified.
  'clientCallback': "clientCallback_example", // String | Client callback URL where TMI should redirect after successful OAuth completion with tokens as query parameters. If not provided, tokens are returned as JSON response.
  'state': "state_example", // String | CSRF protection state parameter. Recommended for security. Will be included in the callback response.
  'loginHint': "loginHint_example" // String | User identity hint for test OAuth provider. Allows specifying a desired user identity for testing and automation. Only supported by the test provider (ignored by production providers like Google, GitHub, etc.). Must be 3-20 characters, alphanumeric and hyphens only.
};
apiInstance.authorizeOAuthProvider(scope, opts, (error, data, response) => {
  if (error) {
    console.error(error);
  } else {
    console.log('API called successfully.');
  }
});
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
> AuthTokenResponse exchangeOAuthCode(body, opts)

Exchange OAuth authorization code for JWT tokens

Provider-neutral endpoint to exchange OAuth authorization codes for TMI JWT tokens. Supports Google, GitHub, and Microsoft OAuth providers.

### Example
```javascript
import {TmiClient} from 'tmi-client';

let apiInstance = new TmiClient.AuthenticationApi();
let body = new TmiClient.Oauth2TokenBody(); // Oauth2TokenBody | 
let opts = { 
  'idp': "idp_example" // String | OAuth provider identifier. Defaults to 'test' provider in non-production builds if not specified.
};
apiInstance.exchangeOAuthCode(body, opts, (error, data, response) => {
  if (error) {
    console.error(error);
  } else {
    console.log('API called successfully. Returned data: ' + data);
  }
});
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
```javascript
import {TmiClient} from 'tmi-client';

let apiInstance = new TmiClient.AuthenticationApi();
apiInstance.getAuthProviders((error, data, response) => {
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
```javascript
import {TmiClient} from 'tmi-client';
let defaultClient = TmiClient.ApiClient.instance;


let apiInstance = new TmiClient.AuthenticationApi();
apiInstance.getCurrentUser((error, data, response) => {
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
```javascript
import {TmiClient} from 'tmi-client';
let defaultClient = TmiClient.ApiClient.instance;


let apiInstance = new TmiClient.AuthenticationApi();
apiInstance.getCurrentUserProfile((error, data, response) => {
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
```javascript
import {TmiClient} from 'tmi-client';
let defaultClient = TmiClient.ApiClient.instance;


let apiInstance = new TmiClient.AuthenticationApi();
let idp = "idp_example"; // String | Identity provider ID (e.g., saml_okta, saml_azure)

apiInstance.getProviderGroups(idp, (error, data, response) => {
  if (error) {
    console.error(error);
  } else {
    console.log('API called successfully. Returned data: ' + data);
  }
});
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
> &#x27;String&#x27; getSAMLMetadata()

Get SAML service provider metadata

Returns the SP metadata XML for SAML configuration

### Example
```javascript
import {TmiClient} from 'tmi-client';

let apiInstance = new TmiClient.AuthenticationApi();
apiInstance.getSAMLMetadata((error, data, response) => {
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

**&#x27;String&#x27;**

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/samlmetadata+xml, application/json

<a name="handleOAuthCallback"></a>
# **handleOAuthCallback**
> AuthTokenResponse handleOAuthCallback(code, opts)

Handle OAuth callback

Exchanges OAuth authorization code for JWT tokens. If client_callback was provided during authorization, redirects to client with tokens. Otherwise returns tokens as JSON response.

### Example
```javascript
import {TmiClient} from 'tmi-client';

let apiInstance = new TmiClient.AuthenticationApi();
let code = "code_example"; // String | Authorization code from the OAuth provider
let opts = { 
  'state': "state_example" // String | Optional state parameter for CSRF protection
};
apiInstance.handleOAuthCallback(code, opts, (error, data, response) => {
  if (error) {
    console.error(error);
  } else {
    console.log('API called successfully. Returned data: ' + data);
  }
});
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
> initiateSAMLLogin(opts)

Initiate SAML authentication

Starts SAML authentication flow by redirecting to IdP

### Example
```javascript
import {TmiClient} from 'tmi-client';

let apiInstance = new TmiClient.AuthenticationApi();
let opts = { 
  'clientCallback': "clientCallback_example" // String | Client callback URL to redirect after authentication
};
apiInstance.initiateSAMLLogin(opts, (error, data, response) => {
  if (error) {
    console.error(error);
  } else {
    console.log('API called successfully.');
  }
});
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
```javascript
import {TmiClient} from 'tmi-client';

let apiInstance = new TmiClient.AuthenticationApi();
let token = "token_example"; // String | 
let tokenTypeHint = "tokenTypeHint_example"; // String | 

apiInstance.introspectToken(token, tokenTypeHint, (error, data, response) => {
  if (error) {
    console.error(error);
  } else {
    console.log('API called successfully. Returned data: ' + data);
  }
});
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
> logoutUser(opts)

Logout user

Invalidates the user&#x27;s JWT token by adding it to a blacklist, effectively ending the session. Once logged out, the token cannot be used for further authenticated requests until it naturally expires. The token blacklist is maintained in Redis with automatic cleanup based on token expiration times.

### Example
```javascript
import {TmiClient} from 'tmi-client';
let defaultClient = TmiClient.ApiClient.instance;


let apiInstance = new TmiClient.AuthenticationApi();
let opts = { 
  'body': null // Object | Empty request body - token is provided via Authorization header
};
apiInstance.logoutUser(opts, (error, data, response) => {
  if (error) {
    console.error(error);
  } else {
    console.log('API called successfully.');
  }
});
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
> InlineResponse2008 processSAMLLogout(sAMLRequest)

SAML Single Logout

Handles SAML logout requests from IdP

### Example
```javascript
import {TmiClient} from 'tmi-client';

let apiInstance = new TmiClient.AuthenticationApi();
let sAMLRequest = "sAMLRequest_example"; // String | Base64-encoded SAML logout request

apiInstance.processSAMLLogout(sAMLRequest, (error, data, response) => {
  if (error) {
    console.error(error);
  } else {
    console.log('API called successfully. Returned data: ' + data);
  }
});
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **sAMLRequest** | **String**| Base64-encoded SAML logout request | 

### Return type

[**InlineResponse2008**](InlineResponse2008.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

<a name="processSAMLLogoutPost"></a>
# **processSAMLLogoutPost**
> InlineResponse2008 processSAMLLogoutPost(opts)

SAML Single Logout (POST)

Handles SAML logout requests from IdP via POST

### Example
```javascript
import {TmiClient} from 'tmi-client';

let apiInstance = new TmiClient.AuthenticationApi();
let opts = { 
  'sAMLRequest': "sAMLRequest_example" // String | 
};
apiInstance.processSAMLLogoutPost(opts, (error, data, response) => {
  if (error) {
    console.error(error);
  } else {
    console.log('API called successfully. Returned data: ' + data);
  }
});
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **sAMLRequest** | **String**|  | [optional] 

### Return type

[**InlineResponse2008**](InlineResponse2008.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: application/x-www-form-urlencoded
 - **Accept**: application/json

<a name="processSAMLResponse"></a>
# **processSAMLResponse**
> AuthTokenResponse processSAMLResponse(opts)

SAML Assertion Consumer Service

Processes SAML responses from IdP after authentication

### Example
```javascript
import {TmiClient} from 'tmi-client';

let apiInstance = new TmiClient.AuthenticationApi();
let opts = { 
  'sAMLResponse': "sAMLResponse_example", // String | 
  'relayState': "relayState_example" // String | 
};
apiInstance.processSAMLResponse(opts, (error, data, response) => {
  if (error) {
    console.error(error);
  } else {
    console.log('API called successfully. Returned data: ' + data);
  }
});
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **sAMLResponse** | **String**|  | [optional] 
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
> AuthTokenResponse refreshToken(opts)

Refresh JWT token

Exchanges a refresh token for a new JWT access token

### Example
```javascript
import {TmiClient} from 'tmi-client';

let apiInstance = new TmiClient.AuthenticationApi();
let opts = { 
  'body': new TmiClient.Oauth2RefreshBody() // Oauth2RefreshBody | 
};
apiInstance.refreshToken(opts, (error, data, response) => {
  if (error) {
    console.error(error);
  } else {
    console.log('API called successfully. Returned data: ' + data);
  }
});
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

