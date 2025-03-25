export const environment = {
  production: true,
  server: {
    port: 8080, // Production port - typically different than dev
    host: '0.0.0.0', // In production, bind to all interfaces
  },
  auth: {
    provider: 'google', // Options: 'anonymous', 'google'
  },
  googleAuth: {
    clientId: 'YOUR_PRODUCTION_CLIENT_ID',
    scopes: 'email profile https://www.googleapis.com/auth/drive.file'
  },
  logging: {
    level: 'info', // Available levels: error, warn, info, debug, trace
    includeTimestamp: true
  },
  storage: {
    provider: 'google-drive',
    google: {
      apiKey: 'YOUR_PRODUCTION_API_KEY',
      appId: 'YOUR_PRODUCTION_APP_ID',
      mimeTypes: ['application/json']
    }
  }
};