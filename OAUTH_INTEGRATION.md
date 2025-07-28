# OAuth Integration Guide for Web Application

## Overview
The TMI server supports OAuth authentication with Google, GitHub, and Microsoft using a provider-neutral API. Since the TMI server is not publicly accessible, your web application must handle the OAuth callback and exchange authorization codes with the TMI server.

## Quick Start

### 1. Get Available Providers
```javascript
// Get list of configured OAuth providers
const response = await fetch('http://localhost:8080/auth/providers');
const { providers } = await response.json();
// Returns: [{"id":"google","name":"Google","icon":"google"}, ...]
```

### 2. Configure OAuth Provider
In your OAuth provider dashboard (Google Cloud Console, GitHub Apps, etc.):
- Set redirect URI to: `https://your-web-app.com/auth/callback`
- Note your `client_id` and `client_secret`

### 3. Complete OAuth Integration

#### Option A: Simple Integration (Recommended)
```javascript
class TMIOAuth {
  constructor(tmiServerUrl = 'http://localhost:8080') {
    this.tmiServerUrl = tmiServerUrl;
    this.providerConfig = {
      google: {
        authUrl: 'https://accounts.google.com/o/oauth2/auth',
        clientId: 'your-google-client-id',
        scopes: 'openid profile email'
      },
      github: {
        authUrl: 'https://github.com/login/oauth/authorize',
        clientId: 'your-github-client-id',
        scopes: 'user:email'
      },
      microsoft: {
        authUrl: 'https://login.microsoftonline.com/common/oauth2/v2.0/authorize',
        clientId: 'your-microsoft-client-id',
        scopes: 'openid profile email User.Read'
      }
    };
  }

  // Start OAuth login flow
  login(provider) {
    const config = this.providerConfig[provider];
    if (!config) {
      throw new Error(`Unsupported provider: ${provider}`);
    }

    const state = this.generateState();
    localStorage.setItem('oauth_state', state);
    localStorage.setItem('oauth_provider', provider);

    const params = new URLSearchParams({
      client_id: config.clientId,
      redirect_uri: `${window.location.origin}/auth/callback`,
      response_type: 'code',
      scope: config.scopes,
      state: state
    });

    window.location.href = `${config.authUrl}?${params}`;
  }

  // Handle OAuth callback (call this in your /auth/callback page)
  async handleCallback() {
    const urlParams = new URLSearchParams(window.location.search);
    const code = urlParams.get('code');
    const state = urlParams.get('state');
    const error = urlParams.get('error');

    if (error) {
      throw new Error(`OAuth error: ${error}`);
    }

    if (!code || !state) {
      throw new Error('Missing authorization code or state');
    }

    // Verify state
    const storedState = localStorage.getItem('oauth_state');
    const provider = localStorage.getItem('oauth_provider');
    
    if (state !== storedState) {
      throw new Error('Invalid state parameter - possible CSRF attack');
    }

    // Clean up stored values
    localStorage.removeItem('oauth_state');
    localStorage.removeItem('oauth_provider');

    // Exchange code with TMI server
    const response = await fetch(`${this.tmiServerUrl}/auth/exchange/${provider}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        code,
        state,
        redirect_uri: `${window.location.origin}/auth/callback`
      })
    });

    if (!response.ok) {
      const error = await response.json();
      throw new Error(`OAuth exchange failed: ${error.error}`);
    }

    const tokens = await response.json();

    // Store TMI tokens
    localStorage.setItem('tmi_access_token', tokens.access_token);
    localStorage.setItem('tmi_refresh_token', tokens.refresh_token);
    localStorage.setItem('tmi_token_expires', Date.now() + (tokens.expires_in * 1000));

    return tokens;
  }

  // Make authenticated API calls to TMI server
  async apiCall(endpoint, options = {}) {
    let token = localStorage.getItem('tmi_access_token');
    
    // Check if token needs refresh
    const expiresAt = localStorage.getItem('tmi_token_expires');
    if (expiresAt && Date.now() > (parseInt(expiresAt) - 60000)) { // Refresh 1 min before expiry
      await this.refreshToken();
      token = localStorage.getItem('tmi_access_token');
    }

    return fetch(`${this.tmiServerUrl}${endpoint}`, {
      ...options,
      headers: {
        'Authorization': `Bearer ${token}`,
        'Content-Type': 'application/json',
        ...options.headers
      }
    });
  }

  // Refresh access token
  async refreshToken() {
    const refreshToken = localStorage.getItem('tmi_refresh_token');
    if (!refreshToken) {
      throw new Error('No refresh token available');
    }

    const response = await fetch(`${this.tmiServerUrl}/auth/refresh`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ refresh_token: refreshToken })
    });

    if (!response.ok) {
      // Refresh failed, redirect to login
      this.logout();
      throw new Error('Token refresh failed - please login again');
    }

    const tokens = await response.json();
    localStorage.setItem('tmi_access_token', tokens.access_token);
    localStorage.setItem('tmi_refresh_token', tokens.refresh_token);
    localStorage.setItem('tmi_token_expires', Date.now() + (tokens.expires_in * 1000));

    return tokens;
  }

  // Logout user
  async logout() {
    try {
      await fetch(`${this.tmiServerUrl}/auth/logout`, {
        method: 'POST',
        headers: {
          'Authorization': `Bearer ${localStorage.getItem('tmi_access_token')}`,
          'Content-Type': 'application/json'
        }
      });
    } catch (error) {
      console.warn('Logout request failed:', error);
    }

    // Clear local storage
    localStorage.removeItem('tmi_access_token');
    localStorage.removeItem('tmi_refresh_token'); 
    localStorage.removeItem('tmi_token_expires');
  }

  // Generate random state for CSRF protection
  generateState() {
    return btoa(String.fromCharCode(...crypto.getRandomValues(new Uint8Array(32))));
  }

  // Check if user is logged in
  isLoggedIn() {
    const token = localStorage.getItem('tmi_access_token');
    const expiresAt = localStorage.getItem('tmi_token_expires');
    
    return token && expiresAt && Date.now() < parseInt(expiresAt);
  }
}
```

#### Usage Example:
```javascript
// Initialize OAuth client
const tmiAuth = new TMIOAuth('http://localhost:8080');

// Login with Google
document.getElementById('google-login').onclick = () => {
  tmiAuth.login('google');
};

// Handle callback (in your /auth/callback page)
if (window.location.pathname === '/auth/callback') {
  tmiAuth.handleCallback()
    .then(() => {
      window.location.href = '/dashboard';
    })
    .catch(error => {
      console.error('Login failed:', error);
      window.location.href = '/login?error=' + encodeURIComponent(error.message);
    });
}

// Make API calls
async function loadUserData() {
  try {
    const response = await tmiAuth.apiCall('/threat_models');
    const threatModels = await response.json();
    // Use threat models...
  } catch (error) {
    console.error('API call failed:', error);
  }
}
```

## TMI Server API Reference

### Provider-Neutral Endpoints

#### `GET /auth/providers`
Get list of configured OAuth providers.

**Response:**
```json
{
  "providers": [
    {"id": "google", "name": "Google", "icon": "google"},
    {"id": "github", "name": "GitHub", "icon": "github"},
    {"id": "microsoft", "name": "Microsoft", "icon": "microsoft"}
  ]
}
```

#### `POST /auth/exchange/{provider}`
Exchange OAuth authorization code for TMI JWT tokens.

**Parameters:**
- `{provider}`: `google`, `github`, or `microsoft`

**Request Body:**
```json
{
  "code": "authorization_code_from_provider",
  "state": "csrf_protection_state", 
  "redirect_uri": "https://your-web-app.com/auth/callback"
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

#### `POST /auth/refresh`
Refresh an expired access token.

**Request Body:**
```json
{
  "refresh_token": "your_refresh_token"
}
```

**Response:** Same as `/auth/exchange/{provider}`

#### `POST /auth/logout`
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
4. Add `https://your-web-app.com/auth/callback` to authorized redirect URIs

### GitHub OAuth Setup  
1. Go to GitHub Settings → Developer settings → OAuth Apps
2. Create new OAuth App
3. Set Authorization callback URL to `https://your-web-app.com/auth/callback`

### Microsoft OAuth Setup
1. Go to [Azure Portal](https://portal.azure.com/) → App registrations
2. Register new application
3. Add `https://your-web-app.com/auth/callback` to redirect URIs

## Troubleshooting

### Common Issues

1. **"Invalid state parameter"**: Check that state is properly stored and retrieved
2. **"Provider not found"**: Ensure provider ID matches exactly (`google`, `github`, `microsoft`)
3. **"Failed to exchange code"**: Verify redirect_uri matches exactly between OAuth provider and your request
4. **"Unauthorized"**: Check that Bearer token is included in API requests
5. **"Token expired"**: Implement token refresh logic

### Debug Mode
Enable debug logging by checking network requests in browser dev tools:
- OAuth authorization request to provider
- Callback with authorization code
- Token exchange request to TMI server
- API requests with Bearer token