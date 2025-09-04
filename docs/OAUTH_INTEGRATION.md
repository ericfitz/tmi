# OAuth Integration Guide for Web Application

## Overview

The TMI server supports OAuth authentication with Google, GitHub, and Microsoft using a provider-neutral API. Since the TMI server is not publicly accessible, your web application must handle the OAuth callback and exchange authorization codes with the TMI server.

## Quick Start

### 1. Get Available Providers

```javascript
// Get list of configured OAuth providers
const response = await fetch("http://localhost:8080/oauth2/providers");
const { providers } = await response.json();
// Returns: [{"id":"google","name":"Google","icon":"google"}, ...]
```

### 2. Configure OAuth Provider

In your OAuth provider dashboard (Google Cloud Console, GitHub Apps, etc.):

- Set redirect URI to: `https://your-web-app.com/oauth2/callback`
- Note your `client_id` and `client_secret`

### 3. Complete OAuth Integration

#### Option A: Simple Integration (Recommended)

```javascript
class TMIOAuth {
  constructor(tmiServerUrl = "http://localhost:8080") {
    this.tmiServerUrl = tmiServerUrl;
    this.providerConfig = {
      google: {
        authUrl: "https://accounts.google.com/o/oauth2/auth",
        clientId: "your-google-client-id",
        scopes: "openid profile email",
      },
      github: {
        authUrl: "https://github.com/login/oauth/authorize",
        clientId: "your-github-client-id",
        scopes: "user:email",
      },
      microsoft: {
        authUrl:
          "https://login.microsoftonline.com/common/oauth2/v2.0/authorize",
        clientId: "your-microsoft-client-id",
        scopes: "openid profile email User.Read",
      },
    };
  }

  // Start OAuth login flow
  login(provider) {
    const config = this.providerConfig[provider];
    if (!config) {
      throw new Error(`Unsupported provider: ${provider}`);
    }

    const state = this.generateState();
    localStorage.setItem("oauth_state", state);
    localStorage.setItem("oauth_provider", provider);

    const params = new URLSearchParams({
      client_id: config.clientId,
      redirect_uri: `${window.location.origin}/oauth2/callback`,
      response_type: "code",
      scope: config.scopes,
      state: state,
    });

    window.location.href = `${config.authUrl}?${params}`;
  }

  // Handle OAuth callback (call this in your /oauth2/callback page)
  async handleCallback() {
    const urlParams = new URLSearchParams(window.location.search);
    const code = urlParams.get("code");
    const state = urlParams.get("state");
    const error = urlParams.get("error");

    if (error) {
      throw new Error(`OAuth error: ${error}`);
    }

    if (!code || !state) {
      throw new Error("Missing authorization code or state");
    }

    // Verify state
    const storedState = localStorage.getItem("oauth_state");
    const provider = localStorage.getItem("oauth_provider");

    if (state !== storedState) {
      throw new Error("Invalid state parameter - possible CSRF attack");
    }

    // Clean up stored values
    localStorage.removeItem("oauth_state");
    localStorage.removeItem("oauth_provider");

    // Exchange code with TMI server
    const response = await fetch(
      `${this.tmiServerUrl}/oauth2/token?idp=${provider}`,
      {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          code,
          state,
          redirect_uri: `${window.location.origin}/oauth2/callback`,
        }),
      }
    );

    if (!response.ok) {
      const error = await response.json();
      throw new Error(`OAuth exchange failed: ${error.error}`);
    }

    const tokens = await response.json();

    // Store TMI tokens
    localStorage.setItem("tmi_access_token", tokens.access_token);
    localStorage.setItem("tmi_refresh_token", tokens.refresh_token);
    localStorage.setItem(
      "tmi_token_expires",
      Date.now() + tokens.expires_in * 1000
    );

    return tokens;
  }

  // Make authenticated API calls to TMI server
  async apiCall(endpoint, options = {}) {
    let token = localStorage.getItem("tmi_access_token");

    // Check if token needs refresh
    const expiresAt = localStorage.getItem("tmi_token_expires");
    if (expiresAt && Date.now() > parseInt(expiresAt) - 60000) {
      // Refresh 1 min before expiry
      await this.refreshToken();
      token = localStorage.getItem("tmi_access_token");
    }

    return fetch(`${this.tmiServerUrl}${endpoint}`, {
      ...options,
      headers: {
        Authorization: `Bearer ${token}`,
        "Content-Type": "application/json",
        ...options.headers,
      },
    });
  }

  // Refresh access token
  async refreshToken() {
    const refreshToken = localStorage.getItem("tmi_refresh_token");
    if (!refreshToken) {
      throw new Error("No refresh token available");
    }

    const response = await fetch(`${this.tmiServerUrl}/oauth2/refresh`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ refresh_token: refreshToken }),
    });

    if (!response.ok) {
      // Refresh failed, redirect to login
      this.logout();
      throw new Error("Token refresh failed - please login again");
    }

    const tokens = await response.json();
    localStorage.setItem("tmi_access_token", tokens.access_token);
    localStorage.setItem("tmi_refresh_token", tokens.refresh_token);
    localStorage.setItem(
      "tmi_token_expires",
      Date.now() + tokens.expires_in * 1000
    );

    return tokens;
  }

  // Logout user
  async logout() {
    try {
      await fetch(`${this.tmiServerUrl}/oauth2/logout`, {
        method: "POST",
        headers: {
          Authorization: `Bearer ${localStorage.getItem("tmi_access_token")}`,
          "Content-Type": "application/json",
        },
      });
    } catch (error) {
      console.warn("Logout request failed:", error);
    }

    // Clear local storage
    localStorage.removeItem("tmi_access_token");
    localStorage.removeItem("tmi_refresh_token");
    localStorage.removeItem("tmi_token_expires");
  }

  // Generate random state for CSRF protection
  generateState() {
    return btoa(
      String.fromCharCode(...crypto.getRandomValues(new Uint8Array(32)))
    );
  }

  // Check if user is logged in
  isLoggedIn() {
    const token = localStorage.getItem("tmi_access_token");
    const expiresAt = localStorage.getItem("tmi_token_expires");

    return token && expiresAt && Date.now() < parseInt(expiresAt);
  }
}
```

#### Usage Example:

```javascript
// Initialize OAuth client
const tmiAuth = new TMIOAuth("http://localhost:8080");

// Login with Google
document.getElementById("google-login").onclick = () => {
  tmiAuth.login("google");
};

// Handle callback (in your /oauth2/callback page)
if (window.location.pathname === "/oauth2/callback") {
  tmiAuth
    .handleCallback()
    .then(() => {
      window.location.href = "/dashboard";
    })
    .catch((error) => {
      console.error("Login failed:", error);
      window.location.href =
        "/login?error=" + encodeURIComponent(error.message);
    });
}

// Make API calls
async function loadUserData() {
  try {
    const response = await tmiAuth.apiCall("/threat_models");
    const threatModels = await response.json();
    // Use threat models...
  } catch (error) {
    console.error("API call failed:", error);
  }
}
```

## OAuth Provider Configuration

### Understanding the New Configuration System

TMI now uses a completely generic OAuth configuration system that eliminates provider-specific code. All OAuth providers are configured through YAML configuration files with a flexible claim mapping system.

### Configuration Structure

```yaml
oauth:
  providers:
    provider_id:
      # Basic OAuth settings
      id: "provider_id"
      name: "Display Name"
      enabled: true
      icon: "fa-brands fa-provider"  # Font Awesome icon class
      client_id: "your-client-id"
      client_secret: "your-client-secret"
      
      # OAuth endpoints
      authorization_url: "https://provider.com/oauth/authorize"
      token_url: "https://provider.com/oauth/token"
      
      # Optional HTTP headers for API requests
      auth_header_format: "Bearer %s"  # Default: "Bearer %s"
      accept_header: "application/json" # Default: "application/json"
      
      # UserInfo endpoints and claim mapping
      userinfo:
        - url: "https://api.provider.com/user"
          claims:
            subject_claim: "id"        # Maps to user ID
            email_claim: "email"       # Maps to email
            name_claim: "name"         # Maps to display name
            # Optional claims:
            given_name_claim: "first_name"
            family_name_claim: "last_name"
            picture_claim: "avatar_url"
            email_verified_claim: "verified"
```

### Claim Mapping Syntax

The claim mapping system supports several patterns:

1. **Simple field mapping**: `"field_name"` - Maps a top-level field
2. **Nested field access**: `"parent.child.field"` - Accesses nested objects
3. **Array access**: `"[0].field"` - Accesses the first element of an array
4. **Literal values**: `"true"`, `"false"` - Sets a constant value

### Default Claim Names

If a claim is not specified in any userinfo endpoint configuration, TMI will look for these default field names in the first userinfo endpoint's response:

- **subject_claim**: `"sub"` (user's unique identifier)
- **email_claim**: `"email"` (user's email address)
- **name_claim**: `"name"` (user's display name)

### Multiple UserInfo Endpoints

Some providers require multiple API calls to get complete user information. The `userinfo` array allows configuring multiple endpoints:

```yaml
userinfo:
  # First endpoint - defaults apply here
  - url: "https://api.provider.com/user"
    claims:
      subject_claim: "id"
      name_claim: "display_name"
  # Second endpoint - for additional data
  - url: "https://api.provider.com/user/emails"
    claims:
      email_claim: "[0].address"       # First email in array
      email_verified_claim: "[0].verified"
```

### Provider-Specific Examples

#### Google Configuration

```yaml
google:
  id: "google"
  name: "Google"
  enabled: true
  icon: "fa-brands fa-google"
  client_id: "${TMI_AUTH_OAUTH_PROVIDERS_GOOGLE_CLIENT_ID}"
  client_secret: "${TMI_AUTH_OAUTH_PROVIDERS_GOOGLE_CLIENT_SECRET}"
  authorization_url: "https://accounts.google.com/o/oauth2/auth"
  token_url: "https://oauth2.googleapis.com/token"
  userinfo:
    - url: "https://www.googleapis.com/oauth2/v3/userinfo"
      claims: {}  # Uses defaults: sub, email, name
  issuer: "https://accounts.google.com"
  jwks_url: "https://www.googleapis.com/oauth2/v3/certs"
  scopes: ["openid", "profile", "email"]
```

#### GitHub Configuration

```yaml
github:
  id: "github"
  name: "GitHub"
  enabled: true
  icon: "fa-brands fa-github"
  client_id: "${TMI_AUTH_OAUTH_PROVIDERS_GITHUB_CLIENT_ID}"
  client_secret: "${TMI_AUTH_OAUTH_PROVIDERS_GITHUB_CLIENT_SECRET}"
  authorization_url: "https://github.com/login/oauth/authorize"
  token_url: "https://github.com/login/oauth/access_token"
  auth_header_format: "token %s"  # GitHub uses "token" instead of "Bearer"
  accept_header: "application/json"
  userinfo:
    # Primary user info
    - url: "https://api.github.com/user"
      claims:
        subject_claim: "id"
        name_claim: "name"
        picture_claim: "avatar_url"
    # Email info (GitHub returns array of emails)
    - url: "https://api.github.com/user/emails"
      claims:
        email_claim: "[0].email"           # First email in array
        email_verified_claim: "[0].verified"
  scopes: ["user:email"]
```

#### Microsoft Configuration

```yaml
microsoft:
  id: "microsoft"
  name: "Microsoft"
  enabled: true
  icon: "fa-brands fa-microsoft"
  client_id: "${TMI_AUTH_OAUTH_PROVIDERS_MICROSOFT_CLIENT_ID}"
  client_secret: "${TMI_AUTH_OAUTH_PROVIDERS_MICROSOFT_CLIENT_SECRET}"
  authorization_url: "https://login.microsoftonline.com/common/oauth2/v2.0/authorize"
  token_url: "https://login.microsoftonline.com/common/oauth2/v2.0/token"
  userinfo:
    - url: "https://graph.microsoft.com/v1.0/me"
      claims:
        subject_claim: "id"
        email_claim: "mail"          # Microsoft uses "mail" not "email"
        name_claim: "displayName"
        given_name_claim: "givenName"
        family_name_claim: "surname"
        email_verified_claim: "true" # Literal - MS accounts always verified
  issuer: "https://login.microsoftonline.com/common/v2.0"
  jwks_url: "https://login.microsoftonline.com/common/discovery/v2.0/keys"
  scopes: ["openid", "profile", "email", "User.Read"]
```

### Adding a New OAuth Provider

To add a new OAuth provider:

1. **Identify the OAuth endpoints**:
   - Authorization URL (where users log in)
   - Token URL (where you exchange codes for tokens)
   - UserInfo URL(s) (where you get user data)

2. **Determine the claim mapping**:
   - Use the provider's API documentation to find field names
   - Map provider fields to TMI's standard fields
   - Test the API to see the actual response structure

3. **Configure special requirements**:
   - Custom auth headers (e.g., GitHub uses "token" instead of "Bearer")
   - Multiple endpoints for complete user data
   - Literal values for fields not provided by the API

4. **Example: Adding LinkedIn**:

```yaml
linkedin:
  id: "linkedin"
  name: "LinkedIn"
  enabled: true
  icon: "fa-brands fa-linkedin"
  client_id: "${TMI_AUTH_OAUTH_PROVIDERS_LINKEDIN_CLIENT_ID}"
  client_secret: "${TMI_AUTH_OAUTH_PROVIDERS_LINKEDIN_CLIENT_SECRET}"
  authorization_url: "https://www.linkedin.com/oauth/v2/authorization"
  token_url: "https://www.linkedin.com/oauth/v2/accessToken"
  userinfo:
    - url: "https://api.linkedin.com/v2/me"
      claims:
        subject_claim: "id"
        given_name_claim: "firstName.localized.en_US"
        family_name_claim: "lastName.localized.en_US"
    - url: "https://api.linkedin.com/v2/emailAddress?q=members&projection=(elements*(handle~))"
      claims:
        email_claim: "elements[0].handle~.emailAddress"
  scopes: ["r_liteprofile", "r_emailaddress"]
```

## TMI Server API Reference

### Provider-Neutral Endpoints

#### `GET /oauth2/providers`

Get list of configured OAuth providers.

**Response:**

```json
{
  "providers": [
    { "id": "google", "name": "Google", "icon": "google" },
    { "id": "github", "name": "GitHub", "icon": "github" },
    { "id": "microsoft", "name": "Microsoft", "icon": "microsoft" }
  ]
}
```

#### `POST /oauth2/token?idp={provider}`

Exchange OAuth authorization code for TMI JWT tokens.

**Parameters:**

- `idp` (query): `google`, `github`, or `microsoft`

**Request Body:**

```json
{
  "code": "authorization_code_from_provider",
  "state": "csrf_protection_state",
  "redirect_uri": "https://your-web-app.com/oauth2/callback"
}
```

**Response:**

```json
{
  "access_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "refresh_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "token_type": "Bearer",
  "expires_in": 3600
}
```

#### `POST /oauth2/refresh`

Refresh an expired access token.

**Request Body:**

```json
{
  "refresh_token": "your_refresh_token"
}
```

**Response:** Same as `/oauth2/token?idp={provider}`

#### `POST /oauth2/logout`

Invalidate current session (requires Bearer token).

**Headers:**

```
Authorization: Bearer your_access_token
```

## Security Best Practices

1. **CSRF Protection**: Always validate the `state` parameter
2. **HTTPS**: Use HTTPS for your web application callback URL
3. **Secure Storage**: Consider httpOnly cookies instead of localStorage for production
4. **Token Refresh**: Implement automatic token refresh before expiration
5. **Error Handling**: Handle OAuth errors gracefully and provide user feedback
6. **Logout**: Properly clear tokens and invalidate sessions

## Provider Configuration

### Google OAuth Setup

1. Go to [Google Cloud Console](https://console.cloud.google.com/)
2. Create/select project → APIs & Services → Credentials
3. Create OAuth 2.0 Client ID
4. Add `https://your-web-app.com/oauth2/callback` to authorized redirect URIs

### GitHub OAuth Setup

1. Go to GitHub Settings → Developer settings → OAuth Apps
2. Create new OAuth App
3. Set Authorization callback URL to `https://your-web-app.com/oauth2/callback`

### Microsoft OAuth Setup

1. Go to [Azure Portal](https://portal.azure.com/) → App registrations
2. Register new application
3. Add `https://your-web-app.com/oauth2/callback` to redirect URIs
4. Configure the Microsoft OAuth provider in your TMI configuration file

#### Microsoft Configuration Notes

**Important**: Microsoft uses non-standard field names in their API responses:
- Email is returned as `mail` instead of `email`
- User ID is returned as `id`
- Display name is returned as `displayName`

This is why the TMI configuration maps these fields explicitly:

```yaml
microsoft:
  userinfo:
    - url: "https://graph.microsoft.com/v1.0/me"
      claims:
        email_claim: "mail"          # Microsoft-specific field name
        name_claim: "displayName"    # Microsoft-specific field name
        subject_claim: "id"          # Microsoft-specific field name
```

#### Microsoft Endpoint Configuration Options

Microsoft Azure AD supports different endpoints depending on which types of accounts you want to support. Update your TMI configuration accordingly:

##### Option 1: All Microsoft Accounts (Work/School + Personal)
```yaml
# TMI config-development.yml
microsoft:
  authorization_url: "https://login.microsoftonline.com/common/oauth2/v2.0/authorize"
  token_url: "https://login.microsoftonline.com/common/oauth2/v2.0/token"
  issuer: "https://login.microsoftonline.com/common/v2.0"
```
**Azure AD App Configuration:**
- In App Manifest: `"signInAudience": "AzureADandPersonalMicrosoftAccount"`
- In Portal: Select "Accounts in any organizational directory and personal Microsoft accounts"

##### Option 2: Personal Microsoft Accounts Only
```yaml
# TMI config-development.yml
microsoft:
  authorization_url: "https://login.microsoftonline.com/consumers/oauth2/v2.0/authorize"
  token_url: "https://login.microsoftonline.com/consumers/oauth2/v2.0/token"
  issuer: "https://login.microsoftonline.com/consumers/v2.0"
```
**Azure AD App Configuration:**
- In App Manifest: `"signInAudience": "PersonalMicrosoftAccount"`
- In Portal: Select "Personal Microsoft accounts only"

##### Option 3: Work/School Accounts Only
```yaml
# TMI config-development.yml
microsoft:
  authorization_url: "https://login.microsoftonline.com/organizations/oauth2/v2.0/authorize"
  token_url: "https://login.microsoftonline.com/organizations/oauth2/v2.0/token"
  issuer: "https://login.microsoftonline.com/organizations/v2.0"
```
**Azure AD App Configuration:**
- In App Manifest: `"signInAudience": "AzureADMultipleOrgs"`
- In Portal: Select "Accounts in any organizational directory"

##### Option 4: Specific Azure AD Tenant
```yaml
# TMI config-development.yml
microsoft:
  authorization_url: "https://login.microsoftonline.com/{your-tenant-id}/oauth2/v2.0/authorize"
  token_url: "https://login.microsoftonline.com/{your-tenant-id}/oauth2/v2.0/token"
  issuer: "https://login.microsoftonline.com/{your-tenant-id}/v2.0"
```
**Azure AD App Configuration:**
- In App Manifest: `"signInAudience": "AzureADMyOrg"`
- In Portal: Select "Accounts in this organizational directory only"
- Replace `{your-tenant-id}` with your actual Azure AD tenant ID (GUID)

**Important Notes:**
- The endpoint type MUST match your Azure AD app's `signInAudience` configuration
- Using `/common/` with `PersonalMicrosoftAccount` will result in a "userAudience configuration" error
- The jwks_url can remain as `/common/discovery/v2.0/keys` regardless of the endpoint type

## Troubleshooting

### Common Issues

1. **"Invalid state parameter"**: Check that state is properly stored and retrieved
2. **"Provider not found"**: Ensure provider ID matches exactly (`google`, `github`, `microsoft`)
3. **"Failed to exchange code"**: Verify redirect_uri matches exactly between OAuth provider and your request
4. **"Unauthorized"**: Check that Bearer token is included in API requests
5. **"Token expired"**: Implement token refresh logic
6. **"The request is not valid for the application's 'userAudience' configuration"** (Microsoft): 
   - This error occurs when your Azure AD app's `signInAudience` doesn't match the endpoint type
   - Solution: Either update your Azure AD app's `signInAudience` setting or change the endpoint URLs in your TMI configuration
   - See the Microsoft OAuth Setup section above for the correct endpoint/audience combinations

### Configuration Troubleshooting

#### "Failed to get user email" Error

This error usually means the OAuth provider returns the email in a non-standard field. Check the provider's API response:

1. **Test the API manually**:
   ```bash
   # Get an access token first, then:
   curl -H "Authorization: Bearer YOUR_ACCESS_TOKEN" \
     https://api.provider.com/user | jq .
   ```

2. **Update your claim mapping**:
   ```yaml
   userinfo:
     - url: "https://api.provider.com/user"
       claims:
         email_claim: "mail"  # If provider uses "mail" instead of "email"
   ```

#### Debugging Claim Mapping

1. **Enable debug logging** to see the raw API responses:
   ```yaml
   logging:
     level: debug
   ```

2. **Common mapping patterns**:
   - Nested fields: `"user.profile.email"`
   - Array access: `"emails[0].address"`
   - First array item: `"[0].email"`
   - Literal values: `"true"` or `"false"`

3. **Testing array access**:
   If an API returns:
   ```json
   [
     {"email": "user@example.com", "primary": true},
     {"email": "alt@example.com", "primary": false}
   ]
   ```
   Use: `email_claim: "[0].email"`

#### Provider-Specific Header Requirements

Some providers require special headers:

```yaml
# GitHub requires "token" instead of "Bearer"
auth_header_format: "token %s"

# Some providers need specific Accept headers
accept_header: "application/vnd.github.v3+json"

### Debug Mode

Enable debug logging by checking network requests in browser dev tools:

- OAuth authorization request to provider
- Callback with authorization code
- Token exchange request to TMI server
- API requests with Bearer token
