export const environment = {
  production: false,
  auth: {
    provider: 'anonymous', // Options: 'anonymous', 'google'
  },
  googleAuth: {
    clientId: 'YOUR_CLIENT_ID',
    scopes: 'email profile https://www.googleapis.com/auth/drive.file'
  },
  logging: {
    level: 'debug', // Available levels: error, warn, info, debug, trace
    includeTimestamp: true
  },
  storage: {
    provider: 'google-drive',
    google: {
      apiKey: 'YOUR_API_KEY',
      appId: 'YOUR_APP_ID',
      mimeTypes: ['application/json']
    }
  }
};