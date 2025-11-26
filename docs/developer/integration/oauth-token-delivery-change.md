# OAuth Token Delivery Change - Migration Guide for Client Developers

## Summary

**Effective Date:** 2025-01-26
**Impact:** OAuth and SAML authentication token delivery method standardized

## What Changed

Both OAuth and SAML authentication flows now deliver JWT tokens via **URL fragments** (the part after `#`) instead of query parameters (the part after `?`).

### Before (OAuth only)
```
https://your-app.com/callback?access_token=eyJhbGc...&refresh_token=abc123&token_type=Bearer&expires_in=3600&state=xyz
```

### After (Both OAuth and SAML)
```
https://your-app.com/callback#access_token=eyJhbGc...&refresh_token=abc123&token_type=Bearer&expires_in=3600&state=xyz
```

## Why This Change

1. **Security**: URL fragments are never sent to the server, preventing tokens from appearing in:
   - Server access logs
   - Reverse proxy logs
   - Browser history (on most browsers)
   - Referrer headers when navigating away

2. **Standards Compliance**: Follows OAuth 2.0 implicit flow specification (RFC 6749)

3. **Consistency**: Both OAuth and SAML now use the same token delivery method

## Required Client Changes

### 1. Update Token Extraction Code

**Old Code (Query Parameters):**
```typescript
// ❌ NO LONGER WORKS
ngOnInit() {
  const params = new URLSearchParams(window.location.search); // .search gets query params
  const accessToken = params.get('access_token');
  const refreshToken = params.get('refresh_token');
  // ...
}
```

**New Code (URL Fragment):**
```typescript
// ✅ CORRECT - Read from fragment
ngOnInit() {
  const hash = window.location.hash.substring(1); // Remove the leading '#'
  const params = new URLSearchParams(hash);

  const accessToken = params.get('access_token');
  const refreshToken = params.get('refresh_token');
  const tokenType = params.get('token_type');
  const expiresIn = params.get('expires_in');
  const state = params.get('state'); // For CSRF validation

  if (accessToken) {
    // Store tokens securely
    this.authService.setTokens({
      accessToken,
      refreshToken,
      tokenType,
      expiresIn: parseInt(expiresIn || '3600', 10)
    });

    // Clear fragment from URL to prevent token exposure
    window.history.replaceState({}, document.title, window.location.pathname);

    // Redirect to intended page
    this.router.navigate(['/']);
  }
}
```

### 2. Complete Example (Angular)

```typescript
import { Component, OnInit } from '@angular/core';
import { Router } from '@angular/router';
import { AuthService } from './auth.service';

@Component({
  selector: 'app-oauth-callback',
  template: '<p>Authenticating...</p>'
})
export class OAuthCallbackComponent implements OnInit {
  constructor(
    private authService: AuthService,
    private router: Router
  ) {}

  ngOnInit() {
    this.handleOAuthCallback();
  }

  private handleOAuthCallback() {
    // Extract tokens from URL fragment (after #)
    const hash = window.location.hash.substring(1);

    if (!hash) {
      console.error('No OAuth response in URL fragment');
      this.router.navigate(['/login']);
      return;
    }

    const params = new URLSearchParams(hash);
    const accessToken = params.get('access_token');
    const refreshToken = params.get('refresh_token');
    const state = params.get('state');

    // Validate state parameter (CSRF protection)
    const expectedState = sessionStorage.getItem('oauth_state');
    if (state !== expectedState) {
      console.error('OAuth state mismatch - possible CSRF attack');
      this.router.navigate(['/login']);
      return;
    }

    // Clear stored state
    sessionStorage.removeItem('oauth_state');

    if (accessToken && refreshToken) {
      // Store tokens
      this.authService.storeTokens({
        accessToken,
        refreshToken,
        tokenType: params.get('token_type') || 'Bearer',
        expiresIn: parseInt(params.get('expires_in') || '3600', 10)
      });

      // Clear fragment from URL (security best practice)
      window.history.replaceState(
        {},
        document.title,
        window.location.pathname + window.location.search
      );

      // Redirect to home or intended page
      const returnUrl = sessionStorage.getItem('return_url') || '/';
      sessionStorage.removeItem('return_url');
      this.router.navigate([returnUrl]);
    } else {
      console.error('Missing tokens in OAuth callback');
      this.router.navigate(['/login']);
    }
  }
}
```

### 3. React Example

```typescript
import { useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { useAuth } from './hooks/useAuth';

export function OAuthCallback() {
  const navigate = useNavigate();
  const { setTokens } = useAuth();

  useEffect(() => {
    // Extract tokens from URL fragment
    const hash = window.location.hash.substring(1);
    const params = new URLSearchParams(hash);

    const accessToken = params.get('access_token');
    const refreshToken = params.get('refresh_token');

    if (accessToken && refreshToken) {
      // Store tokens
      setTokens({
        accessToken,
        refreshToken,
        tokenType: params.get('token_type') || 'Bearer',
        expiresIn: parseInt(params.get('expires_in') || '3600', 10)
      });

      // Clear fragment
      window.history.replaceState({}, document.title, window.location.pathname);

      // Redirect
      navigate('/');
    } else {
      navigate('/login');
    }
  }, [navigate, setTokens]);

  return <div>Authenticating...</div>;
}
```

## Testing Your Changes

### 1. OAuth Flow Test
```bash
# Initiate OAuth login
curl "https://api.tmi.dev/oauth2/authorize?idp=test&client_callback=http://localhost:4200/callback"

# Expected redirect:
# http://localhost:4200/callback#access_token=eyJhbGc...&refresh_token=uuid&token_type=Bearer&expires_in=3600
```

### 2. SAML Flow Test
```bash
# Initiate SAML login
curl "https://api.tmi.dev/saml/entra-tmidev-saml/login?client_callback=http://localhost:4200/callback"

# Expected redirect (after IdP authentication):
# http://localhost:4200/callback#access_token=eyJhbGc...&refresh_token=uuid&token_type=Bearer&expires_in=3600
```

### 3. Verify Token Extraction
```javascript
// In browser console after redirect
const hash = window.location.hash.substring(1);
const params = new URLSearchParams(hash);
console.log('Access Token:', params.get('access_token'));
console.log('Refresh Token:', params.get('refresh_token'));
```

## Backward Compatibility

**Breaking Change:** This is a breaking change for existing OAuth clients.

- Clients reading from query parameters will stop working
- Update required before deploying server changes
- Both OAuth and SAML now behave identically

## Migration Checklist

- [ ] Update OAuth callback handler to read from `window.location.hash` instead of `window.location.search`
- [ ] Validate state parameter for CSRF protection
- [ ] Clear URL fragment after extracting tokens
- [ ] Test OAuth login flow end-to-end
- [ ] Test SAML login flow end-to-end (if applicable)
- [ ] Update any automated tests that verify OAuth callbacks
- [ ] Update client-side routing if callback route changed

## Support

If you encounter issues during migration:
- Check browser console for errors in token extraction
- Verify the URL contains a `#` (fragment) not a `?` (query)
- Ensure you're reading from `window.location.hash` not `window.location.search`
- Review the complete examples above

For questions, contact the TMI API team or file an issue at:
https://github.com/ericfitz/tmi/issues
