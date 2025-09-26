# Client Integration Guides

This directory contains patterns and comprehensive guides for integrating client applications with the TMI server.

## Purpose

Complete client integration documentation covering OAuth authentication, real-time collaboration, REST API usage, and WebSocket integration patterns.

## Files in this Directory

### [client-integration-guide.md](client-integration-guide.md)
**Comprehensive client integration guide** for TMI collaborative editing.

**Content includes:**
- Complete WebSocket collaboration implementation
- Session management (create/join/leave sessions)
- Real-time diagram operations and conflict resolution
- Presenter mode with cursor sharing
- Authentication flow integration
- Error handling and recovery patterns
- Performance optimization techniques
- TypeScript definitions and examples
- Testing strategies for collaborative features

**Key Features:**
- Step-by-step WebSocket integration
- Echo prevention for collaborative editing
- State synchronization mechanisms
- Multi-user session management

### [client-oauth-integration.md](client-oauth-integration.md)
**OAuth client integration patterns** for TMI authentication.

**Content includes:**
- OAuth 2.0 client implementation
- Multi-provider authentication (Google, GitHub, Microsoft)
- JWT token handling and validation
- Authentication flow patterns
- Error handling and security considerations
- Mobile and web client patterns
- Token refresh strategies

### [collaborative-editing-plan.md](collaborative-editing-plan.md)
**Real-time collaborative editing architecture** and implementation planning.

**Content includes:**
- WebSocket protocol design
- Collaborative editing algorithms
- Conflict resolution strategies
- State synchronization approaches
- Multi-user session management
- Performance and scalability considerations
- Implementation roadmap and milestones

### [workflow-generation-prompt.md](workflow-generation-prompt.md)
**Automated workflow generation** for TMI integration and testing.

**Content includes:**
- Workflow automation patterns
- Integration testing automation
- Client SDK generation approaches
- API testing workflow generation
- Development workflow optimization

## Integration Patterns

### Authentication Integration
1. **OAuth Setup**: Configure OAuth providers in TMI server
2. **Client Registration**: Register client applications with providers
3. **Token Exchange**: Implement OAuth authorization code or implicit flow
4. **JWT Handling**: Store and use JWT tokens for API authentication

### REST API Integration
1. **Base Configuration**: Set up HTTP client with base URL and auth headers
2. **Resource Operations**: Implement CRUD operations for threat models and diagrams
3. **Error Handling**: Handle HTTP status codes and error responses
4. **Pagination**: Implement pagination for list endpoints

### WebSocket Collaboration
1. **Session Management**: Use REST API to join/create collaboration sessions
2. **WebSocket Connection**: Connect to collaborative editing WebSocket
3. **Message Handling**: Implement bidirectional message processing
4. **State Synchronization**: Handle real-time updates and conflicts

### Client Architecture Patterns
1. **Authentication Layer**: Centralized auth handling with token refresh
2. **API Layer**: REST client with consistent error handling
3. **Real-time Layer**: WebSocket client for collaborative features
4. **State Management**: Local state synchronized with server state

## Quick Start Integration

### Basic REST Client Setup
```javascript
const tmiClient = new TMIClient({
  baseUrl: 'https://api.tmi.example.com',
  authToken: 'your-jwt-token'
});

// List threat models
const threatModels = await tmiClient.threatModels.list();

// Create new diagram
const diagram = await tmiClient.diagrams.create(threatModelId, {
  name: 'Main Data Flow Diagram'
});
```

### Collaborative Editing Setup
```javascript
// Join collaboration session via REST API
const session = await tmiClient.collaboration.join(threatModelId, diagramId);

// Connect to WebSocket for real-time updates
const wsClient = new TMICollaborativeClient({
  websocketUrl: session.websocket_url,
  jwtToken: authToken
});

await wsClient.connect();

// Handle real-time diagram updates
wsClient.on('diagram_operation', (operation) => {
  applyOperationToDiagram(operation);
});
```

## Authentication Flows

### OAuth 2.0 Authorization Code Flow
1. Redirect user to TMI OAuth authorization endpoint
2. User authorizes application with OAuth provider
3. TMI redirects back to client with authorization code
4. Client exchanges code for JWT access token
5. Use JWT token for authenticated API requests

### OAuth 2.0 Implicit Flow (Web Clients)
1. Redirect user to TMI OAuth authorization endpoint
2. User authorizes application with OAuth provider
3. TMI redirects back to client with JWT token directly
4. Extract and store JWT token for API authentication

## Client Types and Patterns

### Web Applications (SPA)
- **Authentication**: OAuth 2.0 Implicit Flow
- **API Client**: Fetch-based HTTP client
- **Real-time**: WebSocket connection
- **State**: Local state management (Redux, Zustand, etc.)

### Mobile Applications
- **Authentication**: OAuth 2.0 Authorization Code Flow with PKCE
- **API Client**: Platform-specific HTTP client
- **Real-time**: WebSocket connection
- **State**: Platform state management patterns

### Server-side Applications
- **Authentication**: OAuth 2.0 Authorization Code Flow
- **API Client**: Server HTTP client
- **Real-time**: WebSocket connection (for collaboration features)
- **State**: Server-side session management

### Command Line Tools
- **Authentication**: OAuth 2.0 device flow or service accounts
- **API Client**: CLI-optimized HTTP client
- **Real-time**: Optional WebSocket for monitoring
- **State**: File-based configuration

## Testing Integration

### Unit Testing Client Integration
- Mock TMI server responses
- Test authentication flow handling
- Validate request/response processing
- Test error scenarios

### Integration Testing
- Use TMI test server instance
- Test complete authentication flows
- Validate real API interactions
- Test WebSocket collaboration

### End-to-End Testing
- Test complete user workflows
- Validate multi-user collaboration
- Performance testing under load
- Cross-browser/platform testing

## Performance Considerations

### API Optimization
- Implement request caching
- Use pagination for large datasets
- Batch operations where possible
- Optimize polling intervals

### WebSocket Optimization
- Throttle high-frequency events (cursor movements)
- Debounce user input operations
- Implement connection retry logic
- Handle network disconnections gracefully

### Client State Management
- Optimize local state updates
- Implement efficient diff algorithms
- Cache frequently accessed data
- Minimize unnecessary re-renders

## Security Best Practices

### Token Management
- Store JWT tokens securely
- Implement token refresh logic
- Handle token expiration gracefully
- Clear tokens on logout

### API Security
- Validate all server responses
- Sanitize user inputs
- Use HTTPS for all communications
- Implement request rate limiting

### WebSocket Security
- Authenticate WebSocket connections
- Validate all incoming messages
- Implement message size limits
- Handle malicious or malformed messages

## Related Documentation

### Setup and Configuration
- [Development Setup](../setup/development-setup.md) - TMI server setup
- [OAuth Integration](../setup/oauth-integration.md) - Server-side OAuth setup

### Testing and Quality
- [Integration Testing](../testing/integration-testing.md) - Server integration testing
- [WebSocket Testing](../testing/websocket-testing.md) - Real-time feature testing

### Operations and Deployment
- [Deployment Guide](../../operator/deployment/deployment-guide.md) - Production deployment
- [Database Operations](../../operator/database/postgresql-operations.md) - Database integration

## Contributing

When contributing client integration improvements:

1. **Update relevant integration guides** - Keep documentation current
2. **Add code examples** - Provide working examples for new patterns
3. **Test integration patterns** - Verify examples work with current TMI server
4. **Update TypeScript definitions** - Keep type definitions accurate
5. **Cross-reference related docs** - Maintain links to related documentation

For questions about client integration patterns or to suggest new integration approaches, please create an issue in the project repository.