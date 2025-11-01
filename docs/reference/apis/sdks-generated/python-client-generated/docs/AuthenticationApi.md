# tmi_client.AuthenticationApi

All URIs are relative to *http://localhost:8080*

Method | HTTP request | Description
------------- | ------------- | -------------
[**authorize_o_auth_provider**](AuthenticationApi.md#authorize_o_auth_provider) | **GET** /oauth2/authorize | Initiate OAuth authorization flow
[**exchange_o_auth_code**](AuthenticationApi.md#exchange_o_auth_code) | **POST** /oauth2/token | Exchange OAuth authorization code for JWT tokens
[**get_auth_providers**](AuthenticationApi.md#get_auth_providers) | **GET** /oauth2/providers | List available OAuth providers
[**get_current_user**](AuthenticationApi.md#get_current_user) | **GET** /oauth2/userinfo | Get current user information
[**get_current_user_profile**](AuthenticationApi.md#get_current_user_profile) | **GET** /users/me | Get current user profile
[**get_provider_groups**](AuthenticationApi.md#get_provider_groups) | **GET** /oauth2/providers/{idp}/groups | Get groups for identity provider
[**get_saml_metadata**](AuthenticationApi.md#get_saml_metadata) | **GET** /saml/metadata | Get SAML service provider metadata
[**handle_o_auth_callback**](AuthenticationApi.md#handle_o_auth_callback) | **GET** /oauth2/callback | Handle OAuth callback
[**initiate_saml_login**](AuthenticationApi.md#initiate_saml_login) | **GET** /saml/login | Initiate SAML authentication
[**introspect_token**](AuthenticationApi.md#introspect_token) | **POST** /oauth2/introspect | Token Introspection
[**logout_user**](AuthenticationApi.md#logout_user) | **POST** /oauth2/revoke | Logout user
[**process_saml_logout**](AuthenticationApi.md#process_saml_logout) | **GET** /saml/slo | SAML Single Logout
[**process_saml_logout_post**](AuthenticationApi.md#process_saml_logout_post) | **POST** /saml/slo | SAML Single Logout (POST)
[**process_saml_response**](AuthenticationApi.md#process_saml_response) | **POST** /saml/acs | SAML Assertion Consumer Service
[**refresh_token**](AuthenticationApi.md#refresh_token) | **POST** /oauth2/refresh | Refresh JWT token

# **authorize_o_auth_provider**
> authorize_o_auth_provider(scope, idp=idp, client_callback=client_callback, state=state, login_hint=login_hint)

Initiate OAuth authorization flow

Redirects user to OAuth provider's authorization page. Supports client callback URL for seamless client integration. Generates state parameter for CSRF protection.

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint

# create an instance of the API class
api_instance = tmi_client.AuthenticationApi()
scope = 'scope_example' # str | OAuth 2.0 scope parameter. For OpenID Connect, must include \"openid\". Supports \"profile\" and \"email\" scopes. Other scopes are silently ignored. Space-separated values.
idp = 'idp_example' # str | OAuth provider identifier. Defaults to 'test' provider in non-production builds if not specified. (optional)
client_callback = 'client_callback_example' # str | Client callback URL where TMI should redirect after successful OAuth completion with tokens as query parameters. If not provided, tokens are returned as JSON response. (optional)
state = 'state_example' # str | CSRF protection state parameter. Recommended for security. Will be included in the callback response. (optional)
login_hint = 'login_hint_example' # str | User identity hint for test OAuth provider. Allows specifying a desired user identity for testing and automation. Only supported by the test provider (ignored by production providers like Google, GitHub, etc.). Must be 3-20 characters, alphanumeric and hyphens only. (optional)

try:
    # Initiate OAuth authorization flow
    api_instance.authorize_o_auth_provider(scope, idp=idp, client_callback=client_callback, state=state, login_hint=login_hint)
except ApiException as e:
    print("Exception when calling AuthenticationApi->authorize_o_auth_provider: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **scope** | **str**| OAuth 2.0 scope parameter. For OpenID Connect, must include \&quot;openid\&quot;. Supports \&quot;profile\&quot; and \&quot;email\&quot; scopes. Other scopes are silently ignored. Space-separated values. | 
 **idp** | **str**| OAuth provider identifier. Defaults to &#x27;test&#x27; provider in non-production builds if not specified. | [optional] 
 **client_callback** | **str**| Client callback URL where TMI should redirect after successful OAuth completion with tokens as query parameters. If not provided, tokens are returned as JSON response. | [optional] 
 **state** | **str**| CSRF protection state parameter. Recommended for security. Will be included in the callback response. | [optional] 
 **login_hint** | **str**| User identity hint for test OAuth provider. Allows specifying a desired user identity for testing and automation. Only supported by the test provider (ignored by production providers like Google, GitHub, etc.). Must be 3-20 characters, alphanumeric and hyphens only. | [optional] 

### Return type

void (empty response body)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **exchange_o_auth_code**
> AuthTokenResponse exchange_o_auth_code(body, idp=idp)

Exchange OAuth authorization code for JWT tokens

Provider-neutral endpoint to exchange OAuth authorization codes for TMI JWT tokens. Supports Google, GitHub, and Microsoft OAuth providers.

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint

# create an instance of the API class
api_instance = tmi_client.AuthenticationApi()
body = tmi_client.Oauth2TokenBody() # Oauth2TokenBody | 
idp = 'idp_example' # str | OAuth provider identifier. Defaults to 'test' provider in non-production builds if not specified. (optional)

try:
    # Exchange OAuth authorization code for JWT tokens
    api_response = api_instance.exchange_o_auth_code(body, idp=idp)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling AuthenticationApi->exchange_o_auth_code: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**Oauth2TokenBody**](Oauth2TokenBody.md)|  | 
 **idp** | **str**| OAuth provider identifier. Defaults to &#x27;test&#x27; provider in non-production builds if not specified. | [optional] 

### Return type

[**AuthTokenResponse**](AuthTokenResponse.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **get_auth_providers**
> InlineResponse2004 get_auth_providers()

List available OAuth providers

Returns a list of configured OAuth providers available for authentication

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint

# create an instance of the API class
api_instance = tmi_client.AuthenticationApi()

try:
    # List available OAuth providers
    api_response = api_instance.get_auth_providers()
    pprint(api_response)
except ApiException as e:
    print("Exception when calling AuthenticationApi->get_auth_providers: %s\n" % e)
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

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **get_current_user**
> InlineResponse2006 get_current_user()

Get current user information

Returns information about the currently authenticated user

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.AuthenticationApi(tmi_client.ApiClient(configuration))

try:
    # Get current user information
    api_response = api_instance.get_current_user()
    pprint(api_response)
except ApiException as e:
    print("Exception when calling AuthenticationApi->get_current_user: %s\n" % e)
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

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **get_current_user_profile**
> User get_current_user_profile()

Get current user profile

Returns detailed information about the currently authenticated user including groups and identity provider

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.AuthenticationApi(tmi_client.ApiClient(configuration))

try:
    # Get current user profile
    api_response = api_instance.get_current_user_profile()
    pprint(api_response)
except ApiException as e:
    print("Exception when calling AuthenticationApi->get_current_user_profile: %s\n" % e)
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

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **get_provider_groups**
> InlineResponse2005 get_provider_groups(idp)

Get groups for identity provider

Returns groups available from a specific identity provider for autocomplete and discovery

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.AuthenticationApi(tmi_client.ApiClient(configuration))
idp = 'idp_example' # str | Identity provider ID (e.g., saml_okta, saml_azure)

try:
    # Get groups for identity provider
    api_response = api_instance.get_provider_groups(idp)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling AuthenticationApi->get_provider_groups: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **idp** | **str**| Identity provider ID (e.g., saml_okta, saml_azure) | 

### Return type

[**InlineResponse2005**](InlineResponse2005.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **get_saml_metadata**
> str get_saml_metadata()

Get SAML service provider metadata

Returns the SP metadata XML for SAML configuration

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint

# create an instance of the API class
api_instance = tmi_client.AuthenticationApi()

try:
    # Get SAML service provider metadata
    api_response = api_instance.get_saml_metadata()
    pprint(api_response)
except ApiException as e:
    print("Exception when calling AuthenticationApi->get_saml_metadata: %s\n" % e)
```

### Parameters
This endpoint does not need any parameter.

### Return type

**str**

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/samlmetadata+xml, application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **handle_o_auth_callback**
> AuthTokenResponse handle_o_auth_callback(code, state=state)

Handle OAuth callback

Exchanges OAuth authorization code for JWT tokens. If client_callback was provided during authorization, redirects to client with tokens. Otherwise returns tokens as JSON response.

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint

# create an instance of the API class
api_instance = tmi_client.AuthenticationApi()
code = 'code_example' # str | Authorization code from the OAuth provider
state = 'state_example' # str | Optional state parameter for CSRF protection (optional)

try:
    # Handle OAuth callback
    api_response = api_instance.handle_o_auth_callback(code, state=state)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling AuthenticationApi->handle_o_auth_callback: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **code** | **str**| Authorization code from the OAuth provider | 
 **state** | **str**| Optional state parameter for CSRF protection | [optional] 

### Return type

[**AuthTokenResponse**](AuthTokenResponse.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **initiate_saml_login**
> initiate_saml_login(client_callback=client_callback)

Initiate SAML authentication

Starts SAML authentication flow by redirecting to IdP

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint

# create an instance of the API class
api_instance = tmi_client.AuthenticationApi()
client_callback = 'client_callback_example' # str | Client callback URL to redirect after authentication (optional)

try:
    # Initiate SAML authentication
    api_instance.initiate_saml_login(client_callback=client_callback)
except ApiException as e:
    print("Exception when calling AuthenticationApi->initiate_saml_login: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **client_callback** | **str**| Client callback URL to redirect after authentication | [optional] 

### Return type

void (empty response body)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **introspect_token**
> InlineResponse2003 introspect_token(token, token_type_hint)

Token Introspection

Introspects a JWT token to determine its validity and metadata as per RFC 7662

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint

# create an instance of the API class
api_instance = tmi_client.AuthenticationApi()
token = 'token_example' # str | 
token_type_hint = 'token_type_hint_example' # str | 

try:
    # Token Introspection
    api_response = api_instance.introspect_token(token, token_type_hint)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling AuthenticationApi->introspect_token: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **token** | **str**|  | 
 **token_type_hint** | **str**|  | 

### Return type

[**InlineResponse2003**](InlineResponse2003.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: application/x-www-form-urlencoded
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **logout_user**
> logout_user(body=body)

Logout user

Invalidates the user's JWT token by adding it to a blacklist, effectively ending the session. Once logged out, the token cannot be used for further authenticated requests until it naturally expires. The token blacklist is maintained in Redis with automatic cleanup based on token expiration times.

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint


# create an instance of the API class
api_instance = tmi_client.AuthenticationApi(tmi_client.ApiClient(configuration))
body = NULL # object | Empty request body - token is provided via Authorization header (optional)

try:
    # Logout user
    api_instance.logout_user(body=body)
except ApiException as e:
    print("Exception when calling AuthenticationApi->logout_user: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **body** | [**object**](object.md)| Empty request body - token is provided via Authorization header | [optional] 

### Return type

void (empty response body)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **process_saml_logout**
> InlineResponse2008 process_saml_logout(saml_request)

SAML Single Logout

Handles SAML logout requests from IdP

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint

# create an instance of the API class
api_instance = tmi_client.AuthenticationApi()
saml_request = 'saml_request_example' # str | Base64-encoded SAML logout request

try:
    # SAML Single Logout
    api_response = api_instance.process_saml_logout(saml_request)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling AuthenticationApi->process_saml_logout: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **saml_request** | **str**| Base64-encoded SAML logout request | 

### Return type

[**InlineResponse2008**](InlineResponse2008.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **process_saml_logout_post**
> InlineResponse2008 process_saml_logout_post(saml_request=saml_request)

SAML Single Logout (POST)

Handles SAML logout requests from IdP via POST

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint

# create an instance of the API class
api_instance = tmi_client.AuthenticationApi()
saml_request = 'saml_request_example' # str |  (optional)

try:
    # SAML Single Logout (POST)
    api_response = api_instance.process_saml_logout_post(saml_request=saml_request)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling AuthenticationApi->process_saml_logout_post: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **saml_request** | **str**|  | [optional] 

### Return type

[**InlineResponse2008**](InlineResponse2008.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: application/x-www-form-urlencoded
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **process_saml_response**
> AuthTokenResponse process_saml_response(saml_response=saml_response, relay_state=relay_state)

SAML Assertion Consumer Service

Processes SAML responses from IdP after authentication

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint

# create an instance of the API class
api_instance = tmi_client.AuthenticationApi()
saml_response = 'saml_response_example' # str |  (optional)
relay_state = 'relay_state_example' # str |  (optional)

try:
    # SAML Assertion Consumer Service
    api_response = api_instance.process_saml_response(saml_response=saml_response, relay_state=relay_state)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling AuthenticationApi->process_saml_response: %s\n" % e)
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **saml_response** | **str**|  | [optional] 
 **relay_state** | **str**|  | [optional] 

### Return type

[**AuthTokenResponse**](AuthTokenResponse.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: application/x-www-form-urlencoded
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **refresh_token**
> AuthTokenResponse refresh_token(body=body)

Refresh JWT token

Exchanges a refresh token for a new JWT access token

### Example
```python
from __future__ import print_function
import time
import tmi_client
from tmi_client.rest import ApiException
from pprint import pprint

# create an instance of the API class
api_instance = tmi_client.AuthenticationApi()
body = tmi_client.Oauth2RefreshBody() # Oauth2RefreshBody |  (optional)

try:
    # Refresh JWT token
    api_response = api_instance.refresh_token(body=body)
    pprint(api_response)
except ApiException as e:
    print("Exception when calling AuthenticationApi->refresh_token: %s\n" % e)
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

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

