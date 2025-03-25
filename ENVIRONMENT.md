# Environment Variables Documentation

This document describes all environment variables used in the TMI (Threat Modeling Improved) application. These environment settings are configured in the `src/environments/environment.ts` file for development and `src/environments/environment.prod.ts` for production builds.

## Server Architecture

The application now uses a single Express server for both serving the Angular application and handling API requests. This is important for security features like CSRF protection to work correctly.

## Current Environment Variables

### Server Configuration

| Variable | Type | Description |
|----------|------|-------------|
| `PORT` | number | Environment variable that sets the port number the application server listens on. Defaults to 4200. |
| `HOST` | string | Environment variable that sets the hostname/interface the server binds to. Defaults to 'localhost'. |
| `NODE_ENV` | string | Environment variable that determines if the app runs in development or production mode. Use 'development' or 'production'. |
| `server.port` | number | The port number in the Angular environment files. This is now primarily used by the Angular app itself, as the Express server uses the PORT environment variable. |
| `server.host` | string | The hostname in the Angular environment files. This is now primarily used by the Angular app itself, as the Express server uses the HOST environment variable. |

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
   - Create a `.env` file based on `.env.example`
   - Run `./start.sh` to build the Angular app and start the Express server
   - Alternatively, run manually:
     ```
     npm run build
     node server.js
     ```

2. Production environment:
   - Create a `.env` file with `NODE_ENV=production`
   - Run `ENV=production ./start.sh`
   - Alternatively, run manually:
     ```
     npm run build:prod
     NODE_ENV=production node server.js
     ```

## CSRF Protection

The application implements CSRF protection using the double-submit cookie pattern:

1. The Express server sets a CSRF token as a cookie (`XSRF-TOKEN`)
2. For non-GET requests, the client must include this token in the `X-XSRF-TOKEN` header
3. The server validates that the cookie and header values match

This protection is handled automatically by Angular HTTP interceptors and the Express server middleware.

> **Important**: The CSRF protection relies on both client and server components working together. This is why we now use the Express server for both API and serving the Angular app.

### Example Configuration

```typescript
export const environment = {
  production: false, // or true for production
  server: {
    port: 4200,
    host: 'localhost'
  },
  auth: {
    provider: 'anonymous' // or 'google' for production
  },
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
3. Update the `.env.example` file
4. Update any components or services that use these variables
5. If applicable, update the `env.ts` helper file for runtime access

## Security Notes

- Never commit actual client IDs, API keys or secrets to the repository
- Do not commit the `.env` file (it's already in .gitignore)
- Use placeholder values in the environment files and `.env.example`
- For production deployments, use a secure method to inject the actual values (CI/CD secrets, environment variables, etc.)
- When exposing the application to public networks, consider using a reverse proxy like Nginx with proper SSL configuration