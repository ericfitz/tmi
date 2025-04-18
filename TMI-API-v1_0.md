# TMI Threat Modeling Improved API

This document describes a RESTful API with WebSocket support for threat modeling with collaborative diagram editing. The API uses JSON payloads, OAuth for authentication, JWTs for session management, and UUIDs for unique identification. Diagrams support collaborative editing, while threat models provide a structured way to document threats linked to diagrams.

## Overview

- **Base URL**: `https://api.example.com`
- **Authentication**: OAuth 2.0 with JWTs.
- **Real-Time**: WebSocket for collaborative diagram editing (not applicable to threat models).
- **Format**: OpenAPI 3.0.3.

## Endpoints

### API Information

- `**GET /**`: Returns service, API, and operator information without authentication.

### Authentication

- `**GET /auth/login**`: Redirects to OAuth provider for login.
- `**GET /auth/callback**`: Exchanges OAuth code for JWT.
- `**POST /auth/logout**`: Invalidates JWT and ends session.

### Threat Model Management

- `**GET /threat_models**`: Lists threat models accessible to the user as name-ID pairs (supports pagination and sorting).
- `**POST /threat_models**`: Creates a new threat model (owner set to creator).
- `**GET /threat_models/{id}**`: Retrieves a threat model's full details.
- `**PUT /threat_models/{id}**`: Fully updates a threat model.
- `**PATCH /threat_models/{id}**`: Partially updates a threat model (JSON Patch).
- `**DELETE /threat_models/{id}**`: Deletes a threat model (owner-only).

### Diagram Management

- `**GET /threat_models/{threat_model_id}/diagrams**`: Lists diagrams associated with a threat model.
- `**POST /threat_models/{threat_model_id}/diagrams**`: Creates a new diagram within a threat model.
- `**GET /threat_models/{threat_model_id}/diagrams/{diagram_id}**`: Retrieves a diagram's full details.
- `**PUT /threat_models/{threat_model_id}/diagrams/{diagram_id}**`: Fully updates a diagram.
- `**PATCH /threat_models/{threat_model_id}/diagrams/{diagram_id}**`: Partially updates a diagram (JSON Patch).
- `**DELETE /threat_models/{threat_model_id}/diagrams/{diagram_id}**`: Deletes a diagram (owner-only).

### Diagram Collaboration

- `**GET /threat_models/{threat_model_id}/diagrams/{diagram_id}/collaborate**`: Gets collaboration session status.
- `**POST /threat_models/{threat_model_id}/diagrams/{diagram_id}/collaborate**`: Joins or starts a session.
- `**DELETE /threat_models/{threat_model_id}/diagrams/{diagram_id}/collaborate**`: Leaves a session.
- **WebSocket**: `wss://api.example.com/threat_models/{threat_model_id}/diagrams/{diagram_id}/ws` for real-time updates.

## Data Models

### ApiInfo

- **Fields**:
  - `status`: Object - API status information.
    - `code`: String - Status code ("OK" or "ERROR").
    - `time`: String - Current server time in UTC (RFC 3339).
  - `service`: Object - Service information.
    - `name`: String - Name of the service.
    - `build`: String - Current build number.
  - `api`: Object - API information.
    - `version`: String - API version.
    - `specification`: String - URL to the API specification.
  - `operator`: Object (optional) - Operator information.
    - `name`: String - Operator name.
    - `contact`: String - Operator contact information.

### Diagram

- **Fields**:
  - `id`: UUID - Unique identifier.
  - `name`: String - Name of the diagram.
  - `description`: String - Description of the diagram.
  - `created_at`, `modified_at`: ISO8601 timestamps - Creation and modification times.
  - `owner`: String - Username or identifier of the current owner (may be email address or other format).
  - `authorization`: Array of `{subject: string, role: "reader"|"writer"|"owner"}` - User roles.
  - `metadata`: Array of `{key: string, value: string}` - Extensible metadata.
  - `components`: Array of Cell objects - Diagram elements.

### Cell

- **Fields**:
  - `id`: String - Unique identifier of the cell.
  - `value`: String (optional) - Label or value associated with the cell.
  - `geometry`: Object (optional) - Position and size for vertices.
    - `x`, `y`: Number - Coordinates of the cell's top-left corner.
    - `width`, `height`: Number - Dimensions of the cell.
  - `style`: String (optional) - Style string defining the cell's appearance.
  - `vertex`: Boolean - Indicates if the cell is a vertex.
  - `edge`: Boolean - Indicates if the cell is an edge.
  - `parent`: String (optional) - ID of the parent cell for grouping.
  - `source`: String (optional) - ID of the source vertex for edges.
  - `target`: String (optional) - ID of the target vertex for edges.

### Threat Model

- **Fields**:
  - `id`: UUID - Unique identifier.
  - `name`: String - Name of the threat model.
  - `description`: String - Description of the threat model.
  - `created_at`, `modified_at`: ISO8601 timestamps - Creation and modification times.
  - `owner`: String - Username or identifier of the current owner (may be email address or other format).
  - `authorization`: Array of `{subject: string, role: "reader"|"writer"|"owner"}` - User roles.
  - `metadata`: Array of `{key: string, value: string}` - Extensible metadata.
  - `diagrams`: Array of diagram UUIDs - References to related diagrams.
  - `threats`: Array of threat objects - Embedded threats.

### Threat

- **Fields**:
  - `id`: UUID - Unique identifier.
  - `threat_model_id`: UUID - Parent threat model ID.
  - `name`: String - Name of the threat.
  - `description`: String - Description of the threat.
  - `created_at`, `modified_at`: ISO8601 timestamps - Creation and modification times.
  - `metadata`: Array of `{key: string, value: string}` - Extensible metadata.

### Authorization

- **Fields**:
  - `subject`: String - Username or identifier of the user.
  - `role`: String - Role: "reader" (view), "writer" (edit), "owner" (full control).

### Metadata

- **Fields**:
  - `key`: String - Metadata key.
  - `value`: String - Metadata value.

### CollaborationSession

- **Fields**:
  - `session_id`: String - Unique identifier for the session.
  - `threat_model_id`: UUID - UUID of the associated threat model.
  - `diagram_id`: UUID - UUID of the associated diagram.
  - `participants`: Array of participant objects.
    - `user_id`: String - Username or identifier of the participant.
    - `joined_at`: ISO8601 timestamp - Join timestamp.
  - `websocket_url`: String - WebSocket URL for real-time updates.

### ListItem

- **Fields**:
  - `name`: String - Name of the resource.
  - `id`: UUID - Unique identifier of the resource.

## Behavior and Implementation Choices

### Authentication

- **OAuth**: External provider handles user auth; server exchanges code for JWT.
- **JWT**: Used for stateless session management, validated on each request.

### Permissions

- **Roles** (applies to both `Diagram` and `ThreatModel`):
  - `reader`: View-only access.
  - `writer`: Edit capabilities (via `PUT`/`PATCH`).
  - `owner`: Full control, including deletion and managing `authorization`.
- **Ownership**:
  - Initial `owner` set on creation with `"owner"` role in `authorization`.
  - `owner` can transfer to another `"owner"` in `authorization`.
  - Original `owner` retained in `authorization` as `"owner"` post-transfer unless explicitly removed.

### Diagram Collaboration

- **Sessions**: Managed via REST (`/collaborate`); active for 15 minutes without activity.
- **WebSocket**: Broadcasts JSON Patch operations for real-time editing; `"reader"` cannot edit.
- **Conflict Resolution**: Last-writer-wins.

### Threat Model Management

- **No Collaboration**: Threat models do not support real-time collaboration or WebSocket integration.
- **Diagram Linking**: `diagrams` field references existing diagram UUIDs.
- **Threats**: Embedded within `ThreatModel`, managed via `PUT` or `PATCH`.

### Design Choices

- **REST + WebSocket**: REST for structure; WebSocket for real-time diagram editing.
- **JSON Patch**: Flexible updates for diagrams and threat models.
- **UUIDs**: Ensures unique identification.
- **List vs. Retrieve**: List APIs return `[name, id]` pairs; retrieve APIs return full objects.
- **Last-Writer-Wins**: Simplest conflict resolution for diagrams.
- **15-Minute Timeout**: Applies to diagram collaboration sessions.

## Implementation Notes

- **Security**: All endpoints except `/` and `/auth/login` and `/auth/callback` require JWT.
- **Validation**: Server enforces role-based access, UUID uniqueness, email format, and referential integrity.
- **Scalability**: Stateless JWTs and WebSocket sessions support horizontal scaling.
- **Future Enhancements**:
  - Diagrams: Versioning, audit logs, advanced conflict resolution.
  - Threat Models: Separate threat endpoints, export functionality.

## Usage Examples

### API Information

#### Get API Information

```http
GET /
```

**Response** (200):

```json
{
  "status": {
    "code": "OK",
    "time": "2025-04-09T12:00:00Z"
  },
  "service": {
    "name": "TMI",
    "build": "1.0.0-386eea0"
  },
  "api": {
    "version": "1.0",
    "specification": "https://github.com/ericfitz/tmi/blob/main/tmi-openapi.json"
  },
  "operator": {
    "name": "Example Organization",
    "contact": "api-support@example.com"
  }
}
```

### Authentication

#### Initiate Login

```http
GET /auth/login?redirect_uri=https://client.example.com/callback
```

**Response**: 302 Redirect, `Location: https://oauth-provider.com/auth?...`

#### OAuth Callback

```http
GET /auth/callback?code=abc123&state=xyz789
```

**Response** (200):

```json
{
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "expires_in": 3600
}
```

#### Logout

```http
POST /auth/logout
Authorization: Bearer <JWT>
```

**Response**: 204 No Content

### Threat Model Management

#### List Threat Models

```http
GET /threat_models?limit=1
Authorization: Bearer <JWT>
```

**Response** (200):

```json
[
  {
    "name": "System Threat Model",
    "id": "550e8400-e29b-41d4-a716-446655440000"
  }
]
```

#### Create a Threat Model

```http
POST /threat_models
Authorization: Bearer <JWT>
Content-Type: application/json
{
  "name": "System Threat Model",
  "description": "Threats for system X"
}
```

**Response** (201):

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "name": "System Threat Model",
  "description": "Threats for system X",
  "created_at": "2025-04-06T12:00:00Z",
  "modified_at": "2025-04-06T12:00:00Z",
  "owner": "user@example.com",
  "authorization": [{ "subject": "user@example.com", "role": "owner" }],
  "metadata": [],
  "diagrams": [],
  "threats": []
}
```

**Headers**: `Location: /threat_models/550e8400-e29b-41d4-a716-446655440000`

#### Retrieve a Threat Model

```http
GET /threat_models/550e8400-e29b-41d4-a716-446655440000
Authorization: Bearer <JWT>
```

**Response** (200):

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "name": "System Threat Model",
  "description": "Threats for system X",
  "created_at": "2025-04-06T12:00:00Z",
  "modified_at": "2025-04-06T12:00:00Z",
  "owner": "user@example.com",
  "authorization": [{ "subject": "user@example.com", "role": "owner" }],
  "metadata": [],
  "diagrams": [],
  "threats": []
}
```

#### Update a Threat Model (Full)

```http
PUT /threat_models/550e8400-e29b-41d4-a716-446655440000
Authorization: Bearer <JWT>
Content-Type: application/json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "name": "Updated Threat Model",
  "description": "Updated threats",
  "created_at": "2025-04-06T12:00:00Z",
  "modified_at": "2025-04-06T12:00:00Z",
  "owner": "user@example.com",
  "authorization": [{"subject": "user@example.com", "role": "owner"}],
  "metadata": [],
  "diagrams": [],
  "threats": []
}
```

**Response** (200):

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "name": "Updated Threat Model",
  "description": "Updated threats",
  "created_at": "2025-04-06T12:00:00Z",
  "modified_at": "2025-04-06T12:45:00Z",
  "owner": "user@example.com",
  "authorization": [{ "subject": "user@example.com", "role": "owner" }],
  "metadata": [],
  "diagrams": [],
  "threats": []
}
```

#### Update a Threat Model (Partial)

```http
PATCH /threat_models/550e8400-e29b-41d4-a716-446655440000
Authorization: Bearer <JWT>
Content-Type: application/json
[
  {"op": "add", "path": "/threats/-", "value": {"id": "6ba7b810-9dad-11d1-80b4-00c04fd430c8", "threat_model_id": "550e8400-e29b-41d4-a716-446655440000", "name": "Data Breach", "description": "Unauthorized access", "created_at": "2025-04-06T12:01:00Z", "modified_at": "2025-04-06T12:01:00Z", "metadata": []}}
]
```

**Response** (200):

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "name": "Updated Threat Model",
  "description": "Updated threats",
  "created_at": "2025-04-06T12:00:00Z",
  "modified_at": "2025-04-06T12:45:00Z",
  "owner": "user@example.com",
  "authorization": [{ "subject": "user@example.com", "role": "owner" }],
  "metadata": [],
  "diagrams": [],
  "threats": [
    {
      "id": "6ba7b810-9dad-11d1-80b4-00c04fd430c8",
      "threat_model_id": "550e8400-e29b-41d4-a716-446655440000",
      "name": "Data Breach",
      "description": "Unauthorized access",
      "created_at": "2025-04-06T12:01:00Z",
      "modified_at": "2025-04-06T12:01:00Z",
      "metadata": []
    }
  ]
}
```

#### Delete a Threat Model

```http
DELETE /threat_models/550e8400-e29b-41d4-a716-446655440000
Authorization: Bearer <JWT>
```

**Response**: 204 No Content

### Diagram Management

#### List Diagrams for a Threat Model

```http
GET /threat_models/550e8400-e29b-41d4-a716-446655440000/diagrams?limit=2&offset=0
Authorization: Bearer <JWT>
```

**Response** (200):

```json
[
  { "name": "Workflow Diagram", "id": "123e4567-e89b-12d3-a456-426614174000" },
  { "name": "System Overview", "id": "456e7890-e12f-34d5-a678-426614174001" }
]
```

#### Create a Diagram

```http
POST /threat_models/550e8400-e29b-41d4-a716-446655440000/diagrams
Authorization: Bearer <JWT>
Content-Type: application/json
{
  "name": "New Diagram",
  "description": "A test diagram"
}
```

**Response** (201):

```json
{
  "id": "123e4567-e89b-12d3-a456-426614174000",
  "name": "New Diagram",
  "description": "A test diagram",
  "created_at": "2025-04-06T12:00:00Z",
  "modified_at": "2025-04-06T12:00:00Z",
  "owner": "user@example.com",
  "authorization": [{ "subject": "user@example.com", "role": "owner" }],
  "metadata": [],
  "components": []
}
```

**Headers**: `Location: /threat_models/550e8400-e29b-41d4-a716-446655440000/diagrams/123e4567-e89b-12d3-a456-426614174000`

#### Retrieve a Diagram

```http
GET /threat_models/550e8400-e29b-41d4-a716-446655440000/diagrams/123e4567-e89b-12d3-a456-426614174000
Authorization: Bearer <JWT>
```

**Response** (200):

```json
{
  "id": "123e4567-e89b-12d3-a456-426614174000",
  "name": "Workflow Diagram",
  "description": "A process workflow",
  "created_at": "2025-04-06T12:00:00Z",
  "modified_at": "2025-04-06T12:30:00Z",
  "owner": "user@example.com",
  "authorization": [{ "subject": "user@example.com", "role": "owner" }],
  "metadata": [],
  "components": []
}
```

#### Update a Diagram (Full)

```http
PUT /threat_models/550e8400-e29b-41d4-a716-446655440000/diagrams/123e4567-e89b-12d3-a456-426614174000
Authorization: Bearer <JWT>
Content-Type: application/json
{
  "id": "123e4567-e89b-12d3-a456-426614174000",
  "name": "Updated Diagram",
  "description": "Updated description",
  "created_at": "2025-04-06T12:00:00Z",
  "modified_at": "2025-04-06T12:00:00Z",
  "owner": "user@example.com",
  "authorization": [{"subject": "user@example.com", "role": "owner"}],
  "metadata": [],
  "components": []
}
```

**Response** (200):

```json
{
  "id": "123e4567-e89b-12d3-a456-426614174000",
  "name": "Updated Diagram",
  "description": "Updated description",
  "created_at": "2025-04-06T12:00:00Z",
  "modified_at": "2025-04-06T12:45:00Z",
  "owner": "user@example.com",
  "authorization": [{ "subject": "user@example.com", "role": "owner" }],
  "metadata": [],
  "components": []
}
```

#### Update a Diagram (Partial)

```http
PATCH /threat_models/550e8400-e29b-41d4-a716-446655440000/diagrams/123e4567-e89b-12d3-a456-426614174000
Authorization: Bearer <JWT>
Content-Type: application/json
[
  {"op": "replace", "path": "/name", "value": "Patched Diagram"}
]
```

**Response** (200):

```json
{
  "id": "123e4567-e89b-12d3-a456-426614174000",
  "name": "Patched Diagram",
  "description": "A process workflow",
  "created_at": "2025-04-06T12:00:00Z",
  "modified_at": "2025-04-06T12:45:00Z",
  "owner": "user@example.com",
  "authorization": [{ "subject": "user@example.com", "role": "owner" }],
  "metadata": [],
  "components": []
}
```

#### Delete a Diagram

```http
DELETE /threat_models/550e8400-e29b-41d4-a716-446655440000/diagrams/123e4567-e89b-12d3-a456-426614174000
Authorization: Bearer <JWT>
```

**Response**: 204 No Content

### Diagram Collaboration

#### Get Collaboration Session Status

```http
GET /threat_models/550e8400-e29b-41d4-a716-446655440000/diagrams/123e4567-e89b-12d3-a456-426614174000/collaborate
Authorization: Bearer <JWT>
```

**Response** (200):

```json
{
  "session_id": "abc123-session-uuid",
  "threat_model_id": "550e8400-e29b-41d4-a716-446655440000",
  "diagram_id": "123e4567-e89b-12d3-a456-426614174000",
  "participants": [
    { "user_id": "user@example.com", "joined_at": "2025-04-06T12:02:00Z" }
  ],
  "websocket_url": "wss://api.example.com/threat_models/550e8400-e29b-41d4-a716-446655440000/diagrams/123e4567-e89b-12d3-a456-426614174000/ws"
}
```

#### Start/Join a Collaboration Session

```http
POST /threat_models/550e8400-e29b-41d4-a716-446655440000/diagrams/123e4567-e89b-12d3-a456-426614174000/collaborate
Authorization: Bearer <JWT>
```

**Response** (200):

```json
{
  "session_id": "abc123-session-uuid",
  "threat_model_id": "550e8400-e29b-41d4-a716-446655440000",
  "diagram_id": "123e4567-e89b-12d3-a456-426614174000",
  "participants": [
    { "user_id": "user@example.com", "joined_at": "2025-04-06T12:02:00Z" }
  ],
  "websocket_url": "wss://api.example.com/threat_models/550e8400-e29b-41d4-a716-446655440000/diagrams/123e4567-e89b-12d3-a456-426614174000/ws"
}
```

#### Leave a Collaboration Session

```http
DELETE /threat_models/550e8400-e29b-41d4-a716-446655440000/diagrams/123e4567-e89b-12d3-a456-426614174000/collaborate
Authorization: Bearer <JWT>
```

**Response**: 204 No Content

#### WebSocket Update (Example)

**Client Message** (via `wss://...`):

```json
{
  "operation": {
    "op": "replace",
    "path": "/components/0/data/label",
    "value": "Start Updated"
  }
}
```

**Server Broadcast**:

```json
{
  "event": "update",
  "user_id": "user@example.com",
  "operation": {
    "op": "replace",
    "path": "/components/0/data/label",
    "value": "Start Updated"
  },
  "timestamp": "2025-04-06T12:03:00Z"
}
```

This API provides a robust foundation for an Angular-based tool supporting threat modeling with collaborative diagramming.
