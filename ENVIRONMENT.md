# Environment Variables Documentation

This document describes all environment variables used in the TMI (Threat Modeling Improved) application. These environment settings are configured in the `src/environments/environment.ts` file for development and `src/environments/environment.prod.ts` for production builds.

## Current Environment Variables

### Authentication Provider

| Variable | Type | Description |
|----------|------|-------------|
| `auth.provider` | string | The authentication provider to use. Available options: 'anonymous' (for testing), 'google' (for production). Default is 'anonymous' in development and 'google' in production. |

### Google Authentication

| Variable | Type | Description |
|----------|------|-------------|
| `googleAuth.clientId` | string | Google OAuth client ID from the Google Cloud Console. Required for Google Sign-In. |
| `googleAuth.scopes` | string | Space-separated list of OAuth scopes requested during authentication. Currently set to "email profile https://www.googleapis.com/auth/drive.file" to access basic user information and Drive files created by the app. |

### Logging

| Variable | Type | Description |
|----------|------|-------------|
| `logging.level` | string | Log level threshold for the application. Available values: 'error', 'warn', 'info', 'debug', 'trace'. Defaults to 'info' in production and 'debug' in development. |
| `logging.includeTimestamp` | boolean | When true, all log entries include ISO timestamp. Defaults to true. |

### Storage

| Variable | Type | Description |
|----------|------|-------------|
| `storage.provider` | string | The storage provider to use. Currently supports 'google-drive'. |
| `storage.google.apiKey` | string | Google API key from the Google Cloud Console. Required for Google Drive API access. |
| `storage.google.appId` | string | Google Drive API application ID. Required for Google Picker. |
| `storage.google.mimeTypes` | string[] | Array of MIME types to filter when listing or picking files. Defaults to ['application/json']. |

## How to Configure Environment Variables

1. Development environment:
   - Edit `src/environments/environment.ts`

2. Production environment:
   - Edit `src/environments/environment.prod.ts`

### Example Configuration

```typescript
export const environment = {
  production: false, // or true for production
  googleAuth: {
    clientId: 'YOUR_CLIENT_ID_HERE',
    scopes: 'email profile'
  }
};
```

## Updating Environment Variables

When adding or modifying environment variables:

1. Update both environment files (`environment.ts` and `environment.prod.ts`)
2. Update this documentation file
3. Update any components or services that use these variables

## Security Notes

- Never commit actual client IDs, API keys or secrets to the repository
- Use placeholder values in the environment files
- For production deployments, use a secure method to inject the actual values (CI/CD secrets, environment variables, etc.)