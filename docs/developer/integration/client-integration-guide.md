# TMI Client Integration Guide

This comprehensive guide covers everything you need to integrate your client application with the TMI (Collaborative Threat Modeling Interface) server, including authentication, REST API usage, and real-time WebSocket collaboration.

## Table of Contents

- [Overview](#overview)
- [Prerequisites](#prerequisites)
- [Getting Started](#getting-started)
- [Authentication & JWT Token Handling](#authentication--jwt-token-handling)
- [REST API Integration](#rest-api-integration)
- [WebSocket Collaboration](#websocket-collaboration)
- [TypeScript/JavaScript Integration Patterns](#typescriptjavascript-integration-patterns)
- [Error Handling & Retry Logic](#error-handling--retry-logic)
- [Best Practices](#best-practices)
- [Complete Integration Examples](#complete-integration-examples)
- [Testing Your Integration](#testing-your-integration)
- [Troubleshooting](#troubleshooting)

## Overview

TMI is a server-based collaborative threat modeling platform that provides:

- **OAuth 2.0 Authentication with PKCE** - Secure authentication supporting multiple OAuth providers (Google, GitHub, Microsoft)
- **RESTful API** - Comprehensive API for threat models, diagrams, threats, and documents
- **Real-time Collaboration** - WebSocket-based multi-user diagram editing with conflict resolution
- **Role-based Access Control** - Granular permissions (reader/writer/owner) for all resources

### Architecture

```
┌─────────────────┐
│  Client App     │
│                 │
│  ┌───────────┐  │
│  │  Auth     │  │ ─────────┐
│  │  Layer    │  │          │
│  └───────────┘  │          │
│                 │          ▼
│  ┌───────────┐  │    ┌──────────────┐
│  │  REST     │  │───▶│  TMI Server  │
│  │  Client   │  │    │              │
│  └───────────┘  │    │  - REST API  │
│                 │    │  - WebSocket │
│  ┌───────────┐  │    │  - Database  │
│  │ WebSocket │  │───▶│  - Redis     │
│  │  Client   │  │    └──────────────┘
│  └───────────┘  │
└─────────────────┘
```

## Prerequisites

Before integrating with TMI, ensure you have:

1. **TMI Server Instance** - Access to a running TMI server (local or hosted)
2. **OAuth Provider Setup** - At least one OAuth provider configured on the server
3. **Client Registration** - Your client application registered with the OAuth provider
4. **Development Environment** - Node.js/TypeScript or Python environment for client development

### Required Information

- **TMI Server URL** - Base URL for the TMI API (e.g., `http://localhost:8080` or `https://api.tmi.example.com`)
- **OAuth Provider Details** - Provider ID, client ID, redirect URIs
- **JWT Token** - For authenticated API requests

## Getting Started

### Quick Start: 5-Minute Integration

This minimal example demonstrates the complete flow:

```javascript
// 1. Discover OAuth providers
const providersResponse = await fetch('http://localhost:8080/oauth2/providers');
const { providers } = await providersResponse.json();

// 2. Generate PKCE parameters
const codeVerifier = generateCodeVerifier();
const codeChallenge = await generateCodeChallenge(codeVerifier);
const state = generateRandomState();

// 3. Redirect to OAuth
const provider = providers[0];
const authUrl = `${provider.auth_url}?` +
  `state=${state}` +
  `&client_callback=${encodeURIComponent('http://localhost:4200/callback')}` +
  `&code_challenge=${codeChallenge}` +
  `&code_challenge_method=S256`;

window.location.href = authUrl;

// 4. Handle callback and exchange code for tokens
// (In your callback page)
const urlParams = new URLSearchParams(window.location.search);
const code = urlParams.get('code');

const tokenResponse = await fetch('http://localhost:8080/oauth2/token?idp=google', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({
    grant_type: 'authorization_code',
    code: code,
    code_verifier: codeVerifier,
    redirect_uri: 'http://localhost:4200/callback'
  })
});

const { access_token, refresh_token } = await tokenResponse.json();
localStorage.setItem('tmi_access_token', access_token);

// 5. Make authenticated API requests
const response = await fetch('http://localhost:8080/threat_models', {
  headers: { 'Authorization': `Bearer ${access_token}` }
});

const threatModels = await response.json();
```

## Authentication & JWT Token Handling

TMI uses **OAuth 2.0 Authorization Code Flow with PKCE** (Proof Key for Code Exchange) for enhanced security. This is the recommended flow for all client types: SPAs, mobile apps, desktop apps, and server-side applications.

### PKCE Overview

PKCE prevents authorization code interception attacks and eliminates the need for client secrets, making it safe for public clients.

**How PKCE Works:**

```
1. Client generates code_verifier (random string)
2. Client computes code_challenge = BASE64URL(SHA256(code_verifier))
3. Client sends code_challenge to authorization endpoint
4. Server stores code_challenge with authorization code
5. Server returns authorization code to client
6. Client exchanges code + code_verifier for tokens
7. Server validates: SHA256(code_verifier) == stored code_challenge
```

### PKCE Helper Functions

**JavaScript/TypeScript:**

```javascript
class PKCEHelper {
  // Generate cryptographically secure random code verifier
  static generateCodeVerifier() {
    const array = new Uint8Array(32); // 32 bytes = 256 bits
    crypto.getRandomValues(array);
    return this.base64URLEncode(array);
  }

  // Compute S256 challenge from verifier
  static async generateCodeChallenge(verifier) {
    const encoder = new TextEncoder();
    const data = encoder.encode(verifier);
    const digest = await crypto.subtle.digest('SHA-256', data);
    return this.base64URLEncode(new Uint8Array(digest));
  }

  // Base64URL encoding (without padding)
  static base64URLEncode(buffer) {
    const base64 = btoa(String.fromCharCode(...buffer));
    return base64
      .replace(/\+/g, '-')
      .replace(/\//g, '_')
      .replace(/=/g, '');
  }
}

// Usage:
const verifier = PKCEHelper.generateCodeVerifier();
const challenge = await PKCEHelper.generateCodeChallenge(verifier);
```

**Python:**

```python
import secrets
import hashlib
import base64

class PKCEHelper:
    @staticmethod
    def generate_code_verifier():
        """Generate cryptographically secure random code verifier."""
        verifier_bytes = secrets.token_bytes(32)
        verifier = base64.urlsafe_b64encode(verifier_bytes).decode('utf-8').rstrip('=')
        return verifier

    @staticmethod
    def generate_code_challenge(verifier):
        """Generate S256 code challenge from verifier."""
        digest = hashlib.sha256(verifier.encode('utf-8')).digest()
        challenge = base64.urlsafe_b64encode(digest).decode('utf-8').rstrip('=')
        return challenge

# Usage:
verifier = PKCEHelper.generate_code_verifier()
challenge = PKCEHelper.generate_code_challenge(verifier)
```

### Complete Authentication Flow

#### Step 1: Discover OAuth Providers

```javascript
async function discoverOAuthProviders() {
  const response = await fetch('http://localhost:8080/oauth2/providers');
  const { providers } = await response.json();

  // Response format:
  // {
  //   "providers": [
  //     {
  //       "id": "google",
  //       "name": "Google",
  //       "icon": "fa-brands fa-google",
  //       "auth_url": "http://localhost:8080/oauth2/authorize?idp=google",
  //       "redirect_uri": "http://localhost:8080/oauth2/callback",
  //       "client_id": "675196260523-..."
  //     }
  //   ]
  // }

  return providers;
}
```

#### Step 2: Initiate OAuth Flow with PKCE

```javascript
async function initiateOAuthFlow(provider) {
  // Generate PKCE parameters
  const codeVerifier = PKCEHelper.generateCodeVerifier();
  const codeChallenge = await PKCEHelper.generateCodeChallenge(codeVerifier);

  // Generate state for CSRF protection
  const state = generateRandomState();

  // Store verifier and state for later use during token exchange
  sessionStorage.setItem('pkce_verifier', codeVerifier);
  sessionStorage.setItem('oauth_state', state);

  // Define where TMI should redirect after OAuth completion
  const clientCallbackUrl = `${window.location.origin}/oauth2/callback`;

  // Build OAuth URL with PKCE parameters
  const separator = provider.auth_url.includes('?') ? '&' : '?';
  const authUrl = `${provider.auth_url}${separator}` +
    `state=${encodeURIComponent(state)}` +
    `&client_callback=${encodeURIComponent(clientCallbackUrl)}` +
    `&code_challenge=${encodeURIComponent(codeChallenge)}` +
    `&code_challenge_method=S256`;

  // Redirect to TMI OAuth endpoint
  window.location.href = authUrl;
}

function generateRandomState() {
  return Math.random().toString(36).substring(2, 15) +
         Math.random().toString(36).substring(2, 15);
}
```

#### Step 3: Handle OAuth Callback

```javascript
async function handleOAuthCallback() {
  const urlParams = new URLSearchParams(window.location.search);

  const code = urlParams.get('code');
  const state = urlParams.get('state');
  const error = urlParams.get('error');

  if (error) {
    handleOAuthError(error);
    return;
  }

  // Verify state parameter (CSRF protection)
  const storedState = sessionStorage.getItem('oauth_state');
  if (state !== storedState) {
    console.error('State mismatch - possible CSRF attack');
    handleOAuthError('invalid_state');
    return;
  }

  if (code) {
    await exchangeCodeForTokens(code);
  }
}
```

#### Step 4: Exchange Code for Tokens

```javascript
async function exchangeCodeForTokens(code) {
  // Retrieve stored PKCE verifier
  const codeVerifier = sessionStorage.getItem('pkce_verifier');
  if (!codeVerifier) {
    console.error('PKCE verifier not found - possible session loss');
    handleOAuthError('missing_verifier');
    return;
  }

  // Determine provider ID from original request or store it during Step 2
  const providerId = sessionStorage.getItem('oauth_provider_id') || 'google';

  try {
    // Exchange code + verifier for tokens
    const response = await fetch(`http://localhost:8080/oauth2/token?idp=${providerId}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        grant_type: 'authorization_code',
        code: code,
        code_verifier: codeVerifier,
        redirect_uri: `${window.location.origin}/oauth2/callback`
      })
    });

    if (!response.ok) {
      const error = await response.json();
      console.error('Token exchange failed:', error);
      handleOAuthError('token_exchange_failed');
      return;
    }

    const tokens = await response.json();

    // Store tokens securely
    const expirationTime = Date.now() + parseInt(tokens.expires_in) * 1000;
    localStorage.setItem('tmi_access_token', tokens.access_token);
    localStorage.setItem('tmi_refresh_token', tokens.refresh_token);
    localStorage.setItem('tmi_token_expires', expirationTime);

    // Clean up session storage
    sessionStorage.removeItem('pkce_verifier');
    sessionStorage.removeItem('oauth_state');
    sessionStorage.removeItem('oauth_provider_id');

    // Clean URL (remove code from address bar)
    window.history.replaceState({}, document.title, window.location.pathname);

    // Redirect to main application
    window.location.href = '/dashboard';

  } catch (error) {
    console.error('Token exchange error:', error);
    handleOAuthError('network_error');
  }
}
```

### Token Management

#### Secure Token Storage

```javascript
class TokenManager {
  setTokens(accessToken, refreshToken, expiresIn) {
    const expirationTime = Date.now() + expiresIn * 1000;

    localStorage.setItem('tmi_access_token', accessToken);
    localStorage.setItem('tmi_refresh_token', refreshToken);
    localStorage.setItem('tmi_token_expires', expirationTime);
  }

  getAccessToken() {
    const token = localStorage.getItem('tmi_access_token');
    const expires = localStorage.getItem('tmi_token_expires');

    if (!token || Date.now() > parseInt(expires)) {
      return null; // Token expired
    }

    return token;
  }

  isTokenExpired() {
    const expires = localStorage.getItem('tmi_token_expires');
    return Date.now() > parseInt(expires);
  }

  clearTokens() {
    localStorage.removeItem('tmi_access_token');
    localStorage.removeItem('tmi_refresh_token');
    localStorage.removeItem('tmi_token_expires');
  }
}
```

#### Automatic Token Refresh

```javascript
class APIClient {
  constructor(baseUrl) {
    this.baseUrl = baseUrl;
    this.tokenManager = new TokenManager();
  }

  async makeRequest(endpoint, options = {}) {
    let token = this.tokenManager.getAccessToken();

    // Refresh token if expired
    if (!token || this.tokenManager.isTokenExpired()) {
      token = await this.refreshToken();
    }

    const response = await fetch(`${this.baseUrl}${endpoint}`, {
      ...options,
      headers: {
        'Authorization': `Bearer ${token}`,
        'Content-Type': 'application/json',
        ...options.headers
      }
    });

    if (response.status === 401) {
      // Token invalid, try refresh
      token = await this.refreshToken();

      if (token) {
        // Retry original request
        return fetch(`${this.baseUrl}${endpoint}`, {
          ...options,
          headers: {
            'Authorization': `Bearer ${token}`,
            'Content-Type': 'application/json',
            ...options.headers
          }
        });
      } else {
        // Refresh failed, redirect to login
        this.redirectToLogin();
      }
    }

    return response;
  }

  async refreshToken() {
    const refreshToken = localStorage.getItem('tmi_refresh_token');
    if (!refreshToken) {
      this.redirectToLogin();
      return null;
    }

    try {
      const response = await fetch(`${this.baseUrl}/oauth2/refresh`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ refresh_token: refreshToken })
      });

      if (response.ok) {
        const tokens = await response.json();
        this.tokenManager.setTokens(
          tokens.access_token,
          tokens.refresh_token,
          tokens.expires_in
        );
        return tokens.access_token;
      } else {
        // Refresh failed
        this.redirectToLogin();
        return null;
      }
    } catch (error) {
      console.error('Token refresh failed:', error);
      this.redirectToLogin();
      return null;
    }
  }

  redirectToLogin() {
    this.tokenManager.clearTokens();
    window.location.href = '/login';
  }
}
```

## REST API Integration

### Base Client Setup

```javascript
class TMIClient {
  constructor(config) {
    this.baseUrl = config.baseUrl || 'http://localhost:8080';
    this.apiClient = new APIClient(this.baseUrl);
  }

  // Threat Models
  async listThreatModels() {
    const response = await this.apiClient.makeRequest('/threat_models');
    return await response.json();
  }

  async getThreatModel(id) {
    const response = await this.apiClient.makeRequest(`/threat_models/${id}`);
    return await response.json();
  }

  async createThreatModel(data) {
    const response = await this.apiClient.makeRequest('/threat_models', {
      method: 'POST',
      body: JSON.stringify(data)
    });
    return await response.json();
  }

  async updateThreatModel(id, data) {
    const response = await this.apiClient.makeRequest(`/threat_models/${id}`, {
      method: 'PATCH',
      body: JSON.stringify(data)
    });
    return await response.json();
  }

  async deleteThreatModel(id) {
    await this.apiClient.makeRequest(`/threat_models/${id}`, {
      method: 'DELETE'
    });
  }

  // Diagrams
  async listDiagrams(threatModelId) {
    const response = await this.apiClient.makeRequest(
      `/threat_models/${threatModelId}/diagrams`
    );
    return await response.json();
  }

  async getDiagram(threatModelId, diagramId) {
    const response = await this.apiClient.makeRequest(
      `/threat_models/${threatModelId}/diagrams/${diagramId}`
    );
    return await response.json();
  }

  async createDiagram(threatModelId, data) {
    const response = await this.apiClient.makeRequest(
      `/threat_models/${threatModelId}/diagrams`,
      {
        method: 'POST',
        body: JSON.stringify(data)
      }
    );
    return await response.json();
  }

  async updateDiagram(threatModelId, diagramId, data) {
    const response = await this.apiClient.makeRequest(
      `/threat_models/${threatModelId}/diagrams/${diagramId}`,
      {
        method: 'PATCH',
        body: JSON.stringify(data)
      }
    );
    return await response.json();
  }
}
```

### Common API Patterns

#### Creating Resources

```javascript
async function createThreatModelWithDiagram() {
  const tmiClient = new TMIClient({ baseUrl: 'http://localhost:8080' });

  // Create threat model
  const threatModel = await tmiClient.createThreatModel({
    name: 'Web Application Security Review',
    description: 'Security threat model for the customer portal',
    version: '1.0'
  });

  // Create diagram
  const diagram = await tmiClient.createDiagram(threatModel.id, {
    name: 'Main Data Flow Diagram',
    description: 'Shows data flows for user authentication and data access',
    cells: []
  });

  console.log(`Created threat model ${threatModel.id} with diagram ${diagram.id}`);
}
```

#### Pagination

```javascript
async function listAllThreatModels() {
  const tmiClient = new TMIClient({ baseUrl: 'http://localhost:8080' });

  let allModels = [];
  let offset = 0;
  const limit = 50;

  while (true) {
    const response = await tmiClient.apiClient.makeRequest(
      `/threat_models?limit=${limit}&offset=${offset}`
    );
    const models = await response.json();

    if (models.length === 0) break;

    allModels = allModels.concat(models);
    offset += limit;

    if (models.length < limit) break; // Last page
  }

  return allModels;
}
```

#### Partial Updates with JSON Patch

```javascript
async function updateDiagramCells(threatModelId, diagramId, cellUpdates) {
  const tmiClient = new TMIClient({ baseUrl: 'http://localhost:8080' });

  // Use PATCH for partial updates
  await tmiClient.updateDiagram(threatModelId, diagramId, {
    cells: cellUpdates
  });
}
```

## WebSocket Collaboration

Real-time collaborative editing is a core feature of TMI. Multiple users can edit the same diagram simultaneously with automatic conflict resolution.

### Collaboration Session Management

Before establishing a WebSocket connection, you must join a collaboration session via the REST API.

#### Step 1: Join or Create Session

```javascript
class CollaborationSessionManager {
  constructor(tmiClient) {
    this.tmiClient = tmiClient;
    this.currentSession = null;
  }

  async joinSession(threatModelId, diagramId) {
    try {
      // Try creating a new session (POST)
      const response = await this.tmiClient.apiClient.makeRequest(
        `/threat_models/${threatModelId}/diagrams/${diagramId}/collaborate`,
        { method: 'POST' }
      );

      if (response.status === 201) {
        // Session created successfully
        this.currentSession = await response.json();
        return this.currentSession;
      } else if (response.status === 409) {
        // Session already exists, join it instead (PUT)
        return await this.joinExistingSession(threatModelId, diagramId);
      }
    } catch (error) {
      console.error('Failed to join session:', error);
      throw error;
    }
  }

  async joinExistingSession(threatModelId, diagramId) {
    const response = await this.tmiClient.apiClient.makeRequest(
      `/threat_models/${threatModelId}/diagrams/${diagramId}/collaborate`,
      { method: 'PUT' }
    );

    if (response.status === 200) {
      this.currentSession = await response.json();
      return this.currentSession;
    } else {
      throw new Error(`Failed to join session: ${response.status}`);
    }
  }

  async leaveSession(threatModelId, diagramId) {
    await this.tmiClient.apiClient.makeRequest(
      `/threat_models/${threatModelId}/diagrams/${diagramId}/collaborate`,
      { method: 'DELETE' }
    );
    this.currentSession = null;
  }
}
```

#### Step 2: Connect to WebSocket

```javascript
class TMICollaborativeClient {
  constructor(config) {
    this.threatModelId = config.threatModelId;
    this.diagramId = config.diagramId;
    this.jwtToken = config.jwtToken;
    this.serverUrl = config.serverUrl || 'ws://localhost:8080';
    this.ws = null;
    this.isConnected = false;
    this.isApplyingRemoteChange = false;
    this.eventHandlers = {};
  }

  async connect() {
    const wsUrl = `${this.serverUrl}/threat_models/${this.threatModelId}/diagrams/${this.diagramId}/ws?token=${this.jwtToken}`;

    this.ws = new WebSocket(wsUrl);

    this.ws.onopen = () => {
      this.isConnected = true;
      console.log('Connected to collaborative session');
      this.emit('connected');
    };

    this.ws.onmessage = (event) => {
      const message = JSON.parse(event.data);
      this.handleMessage(message);
    };

    this.ws.onclose = (event) => {
      this.isConnected = false;
      console.log('Disconnected from collaborative session');
      this.emit('disconnected', event);

      if (event.code !== 1000) {
        // Abnormal closure, attempt reconnection
        this.scheduleReconnection();
      }
    };

    this.ws.onerror = (error) => {
      console.error('WebSocket error:', error);
      this.emit('error', error);
    };
  }

  disconnect() {
    if (this.ws) {
      this.ws.close(1000, 'Client disconnect');
    }
  }

  on(event, handler) {
    if (!this.eventHandlers[event]) {
      this.eventHandlers[event] = [];
    }
    this.eventHandlers[event].push(handler);
  }

  emit(event, data) {
    if (this.eventHandlers[event]) {
      this.eventHandlers[event].forEach(handler => handler(data));
    }
  }

  send(message) {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(message));
    } else {
      console.warn('WebSocket not connected, message not sent:', message);
    }
  }
}
```

### Real-time Diagram Operations

#### Sending Operations

```javascript
class DiagramOperationManager {
  constructor(wsClient) {
    this.wsClient = wsClient;
  }

  addCell(cellData) {
    const operation = {
      message_type: 'diagram_operation',
      user_id: this.getCurrentUserEmail(),
      operation_id: this.generateUUID(),
      operation: {
        type: 'patch',
        cells: [{
          id: cellData.id,
          operation: 'add',
          data: cellData
        }]
      }
    };

    this.wsClient.send(operation);
  }

  updateCell(cellId, updates) {
    const operation = {
      message_type: 'diagram_operation',
      user_id: this.getCurrentUserEmail(),
      operation_id: this.generateUUID(),
      operation: {
        type: 'patch',
        cells: [{
          id: cellId,
          operation: 'update',
          data: updates
        }]
      }
    };

    this.wsClient.send(operation);
  }

  removeCell(cellId) {
    const operation = {
      message_type: 'diagram_operation',
      user_id: this.getCurrentUserEmail(),
      operation_id: this.generateUUID(),
      operation: {
        type: 'patch',
        cells: [{
          id: cellId,
          operation: 'remove'
        }]
      }
    };

    this.wsClient.send(operation);
  }

  generateUUID() {
    return crypto.randomUUID();
  }
}
```

#### Receiving Operations (Echo Prevention)

**CRITICAL: Never send WebSocket messages when applying remote operations**

```javascript
class CollaborativeDiagramEditor {
  constructor(wsClient, diagramEditor) {
    this.wsClient = wsClient;
    this.diagramEditor = diagramEditor;
    this.isApplyingRemoteChange = false;

    // Set up handlers
    this.setupMessageHandlers();
    this.setupLocalChangeHandlers();
  }

  setupMessageHandlers() {
    this.wsClient.on('message', (message) => {
      this.handleMessage(message);
    });
  }

  handleMessage(message) {
    switch (message.message_type) {
      case 'diagram_state_sync':
        this.handleInitialStateSync(message);
        break;
      case 'diagram_operation':
        this.handleDiagramOperation(message);
        break;
      case 'current_presenter':
        this.handlePresenterChange(message);
        break;
      case 'presenter_cursor':
        this.handlePresenterCursor(message);
        break;
      case 'state_correction':
        this.handleStateCorrection(message);
        break;
      case 'authorization_denied':
        this.handleAuthorizationDenied(message);
        break;
      default:
        console.warn('Unknown message type:', message.message_type);
    }
  }

  handleDiagramOperation(message) {
    // Skip if this is our own operation (echo prevention)
    if (message.user_id === this.getCurrentUserEmail()) {
      return;
    }

    this.isApplyingRemoteChange = true;

    try {
      // Apply the remote operation to local diagram
      for (const cellOp of message.operation.cells) {
        switch (cellOp.operation) {
          case 'add':
            this.diagramEditor.addCell(cellOp.data);
            break;
          case 'update':
            this.diagramEditor.updateCell(cellOp.id, cellOp.data);
            break;
          case 'remove':
            this.diagramEditor.removeCell(cellOp.id);
            break;
        }
      }

      // Show user feedback
      this.showOperationFeedback(message.user_id, message.operation);
    } finally {
      this.isApplyingRemoteChange = false;
    }
  }

  setupLocalChangeHandlers() {
    // Listen to local diagram changes
    this.diagramEditor.on('cellChanged', (change) => {
      if (this.isApplyingRemoteChange) {
        return; // DON'T send WebSocket message for remote changes
      }

      // Only send for genuine local changes
      this.sendOperation(change);
    });
  }
}
```

### Initial State Synchronization

**CRITICAL: Always handle the `diagram_state_sync` message**

```javascript
handleInitialStateSync(message) {
  console.log(`Received initial state sync - UpdateVector: ${message.update_vector}, Cells: ${message.cells.length}`);

  // Compare with locally cached diagram (from REST API fetch)
  const localVersion = this.cachedDiagram?.update_vector || 0;
  const serverVersion = message.update_vector || 0;

  if (localVersion !== serverVersion) {
    console.warn(`State mismatch detected - Local: ${localVersion}, Server: ${serverVersion}`);

    // Update local state with server cells
    this.isApplyingRemoteChange = true;
    this.diagramEditor.replaceDiagram(message.cells);
    this.isApplyingRemoteChange = false;

    this.cachedDiagram = {
      cells: message.cells,
      update_vector: message.update_vector
    };
  }

  // Mark as synchronized
  this.isStateSynchronized = true;

  // Process any queued operations that were waiting for sync
  this.processQueuedOperations();
}
```

## TypeScript/JavaScript Integration Patterns

### Complete TypeScript Definitions

```typescript
// Collaboration Session Types
interface CollaborationSession {
  session_id: string;
  host: string;
  threat_model_id: string;
  threat_model_name: string;
  diagram_id: string;
  diagram_name: string;
  participants: SessionParticipant[];
  websocket_url: string;
}

interface SessionParticipant {
  user_id: string;
  joined_at: string; // ISO 8601 timestamp
  permissions: 'reader' | 'writer' | 'owner';
}

// WebSocket Message Types
interface DiagramOperationMessage {
  message_type: 'diagram_operation';
  user_id: string;
  operation_id: string;
  sequence_number?: number;
  operation: CellPatchOperation;
}

interface CellPatchOperation {
  type: 'patch';
  cells: CellOperation[];
}

interface CellOperation {
  id: string;
  operation: 'add' | 'update' | 'remove';
  data?: Cell;
}

interface Cell {
  id: string;
  shape: 'actor' | 'process' | 'store' | 'security-boundary' | 'text-box';
  x: number;
  y: number;
  width: number;
  height: number;
  label: string;
  [key: string]: any;
}

interface DiagramStateSyncMessage {
  message_type: 'diagram_state_sync';
  diagram_id: string;
  update_vector: number | null;
  cells: Cell[];
}

interface CurrentPresenterMessage {
  message_type: 'current_presenter';
  current_presenter: string;
}

interface PresenterCursorMessage {
  message_type: 'presenter_cursor';
  user_id: string;
  cursor_position: { x: number; y: number };
}

interface AuthorizationDeniedMessage {
  message_type: 'authorization_denied';
  original_operation_id: string;
  reason: string;
}

interface StateCorrectionMessage {
  message_type: 'state_correction';
  update_vector: number;
}

type WebSocketMessage =
  | DiagramOperationMessage
  | DiagramStateSyncMessage
  | CurrentPresenterMessage
  | PresenterCursorMessage
  | AuthorizationDeniedMessage
  | StateCorrectionMessage;

// Client Configuration
interface TMIClientConfig {
  baseUrl: string;
  jwtToken?: string;
}

interface TMICollaborativeClientConfig {
  threatModelId: string;
  diagramId: string;
  jwtToken: string;
  serverUrl?: string;
  autoReconnect?: boolean;
  maxReconnectAttempts?: number;
}
```

### TypeScript Client Implementation

```typescript
class TMIClient {
  private baseUrl: string;
  private apiClient: APIClient;

  constructor(config: TMIClientConfig) {
    this.baseUrl = config.baseUrl;
    this.apiClient = new APIClient(this.baseUrl);
  }

  async listThreatModels(): Promise<ThreatModel[]> {
    const response = await this.apiClient.makeRequest('/threat_models');
    return await response.json();
  }

  async createDiagram(threatModelId: string, data: CreateDiagramRequest): Promise<Diagram> {
    const response = await this.apiClient.makeRequest(
      `/threat_models/${threatModelId}/diagrams`,
      {
        method: 'POST',
        body: JSON.stringify(data)
      }
    );
    return await response.json();
  }
}
```

## Error Handling & Retry Logic

### Comprehensive Error Handling

```javascript
class ErrorHandler {
  handleAPIError(error, response) {
    if (response.status === 401) {
      return {
        type: 'authentication',
        message: 'Authentication failed. Please log in again.',
        action: 'redirect_to_login'
      };
    }

    if (response.status === 403) {
      return {
        type: 'authorization',
        message: 'You do not have permission to perform this action.',
        action: 'show_error'
      };
    }

    if (response.status === 404) {
      return {
        type: 'not_found',
        message: 'The requested resource was not found.',
        action: 'show_error'
      };
    }

    if (response.status === 409) {
      return {
        type: 'conflict',
        message: 'The resource already exists or conflicts with existing data.',
        action: 'show_error'
      };
    }

    if (response.status >= 500) {
      return {
        type: 'server_error',
        message: 'Server error. Please try again later.',
        action: 'retry'
      };
    }

    return {
      type: 'unknown',
      message: 'An unexpected error occurred.',
      action: 'show_error'
    };
  }

  handleWebSocketError(event) {
    console.error('WebSocket error:', event);

    return {
      type: 'websocket',
      message: 'Real-time collaboration connection lost.',
      action: 'reconnect'
    };
  }
}
```

### Retry Logic with Exponential Backoff

```javascript
class RetryManager {
  async makeRequestWithRetry(requestFn, options = {}) {
    const maxRetries = options.maxRetries || 3;
    const baseDelay = options.baseDelay || 1000;

    for (let attempt = 0; attempt < maxRetries; attempt++) {
      try {
        const response = await requestFn();

        if (response.ok || response.status < 500) {
          return response;
        }

        // Server error, retry
        if (attempt < maxRetries - 1) {
          const delay = baseDelay * Math.pow(2, attempt);
          console.log(`Request failed, retrying in ${delay}ms (attempt ${attempt + 1}/${maxRetries})`);
          await this.delay(delay);
        }
      } catch (error) {
        if (attempt < maxRetries - 1) {
          const delay = baseDelay * Math.pow(2, attempt);
          console.log(`Request error, retrying in ${delay}ms (attempt ${attempt + 1}/${maxRetries})`);
          await this.delay(delay);
        } else {
          throw error;
        }
      }
    }

    throw new Error(`Request failed after ${maxRetries} attempts`);
  }

  delay(ms) {
    return new Promise(resolve => setTimeout(resolve, ms));
  }
}
```

### WebSocket Reconnection

```javascript
class ConnectionManager {
  constructor(wsClient) {
    this.wsClient = wsClient;
    this.reconnectAttempts = 0;
    this.maxReconnectAttempts = 5;
    this.reconnectDelay = 1000;
  }

  scheduleReconnection() {
    if (this.reconnectAttempts >= this.maxReconnectAttempts) {
      console.error('Max reconnection attempts reached');
      this.wsClient.emit('reconnection_failed');
      return;
    }

    const delay = Math.min(
      this.reconnectDelay * Math.pow(2, this.reconnectAttempts),
      30000 // Max 30 seconds
    );

    console.log(`Reconnecting in ${delay}ms (attempt ${this.reconnectAttempts + 1}/${this.maxReconnectAttempts})`);

    setTimeout(() => {
      this.attemptReconnection();
    }, delay);
  }

  async attemptReconnection() {
    this.reconnectAttempts++;

    try {
      await this.wsClient.connect();
      this.reconnectAttempts = 0; // Reset on success
      console.log('Reconnection successful');
      this.wsClient.emit('reconnected');
    } catch (error) {
      console.error('Reconnection failed:', error);
      this.scheduleReconnection();
    }
  }
}
```

## Best Practices

### Security

1. **Always use HTTPS in production** - Never transmit tokens over unencrypted connections
2. **Store tokens securely** - Consider httpOnly cookies for sensitive applications
3. **Implement token refresh** - Refresh tokens before they expire
4. **Clear tokens on logout** - Remove all authentication data
5. **Validate user permissions** - Check permissions before allowing operations

```javascript
// Secure token storage for sensitive applications
class SecureTokenManager {
  async setTokens(accessToken, refreshToken) {
    // Store refresh token in httpOnly cookie (server-side)
    await fetch('/api/store-refresh-token', {
      method: 'POST',
      credentials: 'include',
      body: JSON.stringify({ refreshToken })
    });

    // Store access token in memory only
    this.accessToken = accessToken;
    this.tokenExpiry = Date.now() + 3600 * 1000; // 1 hour
  }
}
```

### Performance

1. **Throttle high-frequency events** - Cursor movements, scroll events
2. **Debounce user input** - Text input, search queries
3. **Implement request caching** - Cache frequently accessed data
4. **Use pagination** - Don't load all data at once
5. **Optimize WebSocket messages** - Send only necessary data

```javascript
class PerformanceOptimizer {
  throttle(func, limit) {
    let lastFunc;
    let lastRan;
    return function(...args) {
      if (!lastRan) {
        func.apply(this, args);
        lastRan = Date.now();
      } else {
        clearTimeout(lastFunc);
        lastFunc = setTimeout(() => {
          if (Date.now() - lastRan >= limit) {
            func.apply(this, args);
            lastRan = Date.now();
          }
        }, limit - (Date.now() - lastRan));
      }
    };
  }

  debounce(func, wait) {
    let timeout;
    return function(...args) {
      clearTimeout(timeout);
      timeout = setTimeout(() => func.apply(this, args), wait);
    };
  }
}
```

### User Experience

1. **Show loading states** - Indicate when operations are in progress
2. **Provide clear error messages** - Help users understand what went wrong
3. **Handle offline scenarios** - Queue operations when offline
4. **Show collaboration indicators** - Display who else is editing
5. **Implement optimistic updates** - Update UI immediately, rollback on error

```javascript
class UXManager {
  showLoadingState(message) {
    // Show loading indicator
    this.showNotification(message, { type: 'loading' });
  }

  showOperationFeedback(userId, operation) {
    const userName = this.getUserDisplayName(userId);
    const message = `${userName} updated the diagram`;
    this.showToast(message, { duration: 2000, type: 'info' });
  }

  handleOffline() {
    this.showNotification('You are offline. Changes will be synced when connection is restored.', {
      type: 'warning',
      persistent: true
    });
  }
}
```

## Complete Integration Examples

### Example 1: Simple Threat Model Viewer

```javascript
class ThreatModelViewer {
  constructor() {
    this.tmiClient = new TMIClient({ baseUrl: 'http://localhost:8080' });
  }

  async initialize() {
    // Authenticate
    const providers = await discoverOAuthProviders();
    await initiateOAuthFlow(providers[0]);

    // Handle callback
    await handleOAuthCallback();

    // Load threat models
    const threatModels = await this.tmiClient.listThreatModels();
    this.displayThreatModels(threatModels);
  }

  async viewThreatModel(id) {
    const threatModel = await this.tmiClient.getThreatModel(id);
    const diagrams = await this.tmiClient.listDiagrams(id);

    this.displayThreatModel(threatModel, diagrams);
  }
}
```

### Example 2: Collaborative Diagram Editor

```javascript
class CollaborativeDiagramApp {
  async initialize(threatModelId, diagramId) {
    // 1. Set up TMI client
    this.tmiClient = new TMIClient({ baseUrl: 'http://localhost:8080' });

    // 2. Fetch diagram via REST API
    this.diagram = await this.tmiClient.getDiagram(threatModelId, diagramId);

    // 3. Join collaboration session
    this.sessionManager = new CollaborationSessionManager(this.tmiClient);
    const session = await this.sessionManager.joinSession(threatModelId, diagramId);

    // 4. Set up WebSocket client
    this.wsClient = new TMICollaborativeClient({
      threatModelId,
      diagramId,
      jwtToken: this.getJWTToken(),
      serverUrl: 'ws://localhost:8080'
    });

    // 5. Connect to WebSocket
    await this.wsClient.connect();

    // 6. Set up event handlers
    this.setupEventHandlers();

    // 7. Initialize diagram editor
    this.initializeDiagramEditor();
  }

  setupEventHandlers() {
    this.wsClient.on('diagram_state_sync', (message) => {
      this.handleInitialStateSync(message);
    });

    this.wsClient.on('diagram_operation', (message) => {
      if (message.user_id !== this.getCurrentUserEmail()) {
        this.applyRemoteOperation(message);
      }
    });

    this.wsClient.on('current_presenter', (message) => {
      this.updatePresenterUI(message.current_presenter);
    });
  }

  initializeDiagramEditor() {
    this.editor = new DiagramEditor({
      container: '#diagram-canvas',
      cells: this.diagram.cells
    });

    // Send operations when user makes changes
    this.editor.on('cellChanged', (change) => {
      if (!this.isApplyingRemoteChange) {
        this.sendOperation(change);
      }
    });
  }
}
```

## Testing Your Integration

### Unit Testing

```javascript
describe('TMIClient', () => {
  let client;

  beforeEach(() => {
    client = new TMIClient({ baseUrl: 'http://localhost:8080' });
  });

  test('should list threat models', async () => {
    const models = await client.listThreatModels();
    expect(Array.isArray(models)).toBe(true);
  });

  test('should handle authentication errors', async () => {
    // Mock 401 response
    await expect(client.getThreatModel('invalid-id')).rejects.toThrow();
  });
});
```

### Integration Testing

```javascript
describe('Collaborative Editing', () => {
  test('should sync operations across multiple clients', async () => {
    const client1 = new TMICollaborativeClient(config1);
    const client2 = new TMICollaborativeClient(config2);

    await Promise.all([client1.connect(), client2.connect()]);

    // Client 1 adds a cell
    const testCell = { id: 'test-1', shape: 'process', x: 100, y: 100 };
    await client1.addCell(testCell);

    // Client 2 should receive the operation
    await new Promise((resolve) => {
      client2.on('diagram_operation', (op) => {
        expect(op.operation.cells[0].data).toEqual(testCell);
        resolve();
      });
    });
  });
});
```

## Troubleshooting

### Common Issues

**Issue: "Invalid state parameter" error**

```
Solution: Ensure your client is redirecting to TMI's auth endpoints,
not directly to OAuth providers.

✗ Wrong: https://accounts.google.com/o/oauth2/auth
✓ Correct: http://localhost:8080/oauth2/authorize?idp=google
```

**Issue: WebSocket connection fails**

```
Solution:
1. Check JWT token is valid and not expired
2. Verify you joined the collaboration session via REST API first
3. Check WebSocket URL format is correct
4. Ensure firewall allows WebSocket connections
```

**Issue: "cell_already_exists" validation errors**

```
Solution:
1. Always handle the diagram_state_sync message
2. Update local state with server state
3. Use correct operation types (add vs update)
4. Check if cell exists before using "add" operation
```

**Issue: 401 errors on API calls**

```
Solution:
1. Verify access token is being sent in Authorization header
2. Check token hasn't expired
3. Implement token refresh mechanism
4. Ensure token is in format: "Bearer <token>"
```

### Debug Tools

**Check OAuth Configuration:**

```bash
curl http://localhost:8080/oauth2/providers | jq
```

**Test Token Validation:**

```bash
curl -H "Authorization: Bearer YOUR_TOKEN" http://localhost:8080/oauth2/userinfo
```

**Monitor WebSocket Messages:**

```javascript
wsClient.on('message', (message) => {
  console.log('WebSocket message:', JSON.stringify(message, null, 2));
});
```

## Next Steps

Now that you understand TMI client integration, explore these related guides:

- **[Client OAuth Integration](client-oauth-integration.md)** - Detailed OAuth implementation patterns
- **[Client WebSocket Integration](client-websocket-integration-guide.md)** - Advanced WebSocket collaboration features
- **[OpenAPI Specification](../../reference/apis/tmi-openapi.json)** - Complete API reference
- **[Development Setup](../setup/development-setup.md)** - Set up a local TMI server for testing

## Support

For questions about client integration:

1. **Check the documentation** - Most questions are answered in the integration guides
2. **Use browser DevTools** - Inspect network requests and WebSocket messages
3. **Test with TMI test provider** - Use the test OAuth provider for debugging
4. **Check TMI server logs** - Server logs provide detailed error messages

---

This guide provides everything you need to build a production-ready TMI client application. Follow the patterns and best practices outlined here for robust, secure, and performant integration with the TMI server.
