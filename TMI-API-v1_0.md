# TMI Threat Modeling Improved API

This document describes a RESTful API with WebSocket support for threat modeling with collaborative diagram editing. The API uses JSON payloads, OAuth for authentication, JWTs for session management, and UUIDs for unique identification. Diagrams support collaborative editing, while threat models provide a structured way to document threats linked to diagrams.

## Overview

- **Base URL**: `https://api.example.com`
- **Authentication**: OAuth 2.0 with JWTs.
- **Real-Time**: WebSocket for collaborative diagram editing (not applicable to threat models).
- **Format**: OpenAPI 3.0.3.

## Endpoints

### Authentication

- `**GET /auth/login**`: Redirects to OAuth provider for login.
- `**GET /auth/callback**`: Exchanges OAuth code for JWT.
- `**POST /auth/logout**`: Invalidates JWT and ends session.

### Diagram Management

- `**GET /diagrams**`: Lists diagrams accessible to the user as name-ID pairs (supports pagination and sorting).
- `**POST /diagrams**`: Creates a new diagram (owner set to creator).
- `**GET /diagrams/{id}**`: Retrieves a diagram’s full details.
- `**PUT /diagrams/{id}**`: Fully updates a diagram.
- `**PATCH /diagrams/{id}**`: Partially updates a diagram (JSON Patch).
- `**DELETE /diagrams/{id}**`: Deletes a diagram (owner-only).

### Diagram Collaboration

- `**GET /diagrams/{id}/collaborate**`: Gets collaboration session status.
- `**POST /diagrams/{id}/collaborate**`: Joins or starts a session.
- `**DELETE /diagrams/{id}/collaborate**`: Leaves a session.
- **WebSocket**: `wss://api.example.com/diagrams/{id}/ws` for real-time updates.

### Threat Model Management

- `**GET /threat_models**`: Lists threat models accessible to the user as name-ID pairs (supports pagination and sorting).
- `**POST /threat_models**`: Creates a new threat model (owner set to creator).
- `**GET /threat_models/{id}**`: Retrieves a threat model’s full details.
- `**PUT /threat_models/{id}**`: Fully updates a threat model.
- `**PATCH /threat_models/{id}**`: Partially updates a threat model (JSON Patch).
- `**DELETE /threat_models/{id}**`: Deletes a threat model (owner-only).

## Data Models

### Diagram

- **Fields**:
  - `id`: UUID - Unique identifier.
  - `name`: String - Name of the diagram.
  - `description`: String - Description of the diagram.
  - `created_at`, `modified_at`: ISO8601 timestamps - Creation and modification times.
  - `owner`: String - Username or identifier of the current owner (may be email address or other format).
  - `authorization`: Array of `{subject: string, role: "reader"|"writer"|"owner"}` - User roles.
  - `metadata`: Array of `{key: string, value: string}` - Extensible metadata.
  - `components`: Array of maxGraph cells (`{id: uuid, type: string, data: object, metadata: array}`) - Diagram elements.

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

- **Security**: All endpoints except `/auth/login` and `/auth/callback` require JWT.
- **Validation**: Server enforces role-based access, UUID uniqueness, email format, and referential integrity.
- **Scalability**: Stateless JWTs and WebSocket sessions support horizontal scaling.
- **Future Enhancements**:
  - Diagrams: Versioning, audit logs, advanced conflict resolution.
  - Threat Models: Separate threat endpoints, export functionality.

## Usage Examples

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

### Diagram Management

#### List Diagrams

```http
GET /diagrams?limit=2&offset=0
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
POST /diagrams
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

**Headers**: `Location: /diagrams/123e4567-e89b-12d3-a456-426614174000`

#### Retrieve a Diagram

```http
GET /diagrams/123e4567-e89b-12d3-a456-426614174000
Authorization: Bearer <JWT>
```

**Response** (200):

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

#### Update a Diagram (Full)

```http
PUT /diagrams/123e4567-e89b-12d3-a456-426614174000
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
PATCH /diagrams/123e4567-e89b-12d3-a456-426614174000
Authorization: Bearer <JWT>
Content-Type: application/json
[
  {"op": "add", "path": "/components/-", "value": {"id": "987fcdeb-12d3-4567-a890-426614174000", "type": "vertex", "data": {"label": "Start", "x": 100, "y": 100}, "metadata": []}}
]
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
  "components": [
    {
      "id": "987fcdeb-12d3-4567-a890-426614174000",
      "type": "vertex",
      "data": { "label": "Start", "x": 100, "y": 100 },
      "metadata": []
    }
  ]
}
```

#### Delete a Diagram

```http
DELETE /diagrams/123e4567-e89b-12d3-a456-426614174000
Authorization: Bearer <JWT>
```

**Response**: 204 No Content

### Diagram Collaboration

#### Get Collaboration Session Status

```http
GET /diagrams/123e4567-e89b-12d3-a456-426614174000/collaborate
Authorization: Bearer <JWT>
```

**Response** (200):

```json
{
  "session_id": "abc123-session-uuid",
  "diagram_id": "123e4567-e89b-12d3-a456-426614174000",
  "participants": [
    { "user_id": "user@example.com", "joined_at": "2025-04-06T12:02:00Z" }
  ],
  "websocket_url": "wss://api.example.com/diagrams/123e4567-e89b-12d3-a456-426614174000/ws"
}
```

#### Start/Join a Collaboration Session

```http
POST /diagrams/123e4567-e89b-12d3-a456-426614174000/collaborate
Authorization: Bearer <JWT>
```

**Response** (200):

```json
{
  "session_id": "abc123-session-uuid",
  "diagram_id": "123e4567-e89b-12d3-a456-426614174000",
  "participants": [
    { "user_id": "user@example.com", "joined_at": "2025-04-06T12:02:00Z" }
  ],
  "websocket_url": "wss://api.example.com/diagrams/123e4567-e89b-12d3-a456-426614174000/ws"
}
```

#### Leave a Collaboration Session

```http
DELETE /diagrams/123e4567-e89b-12d3-a456-426614174000/collaborate
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

This API provides a robust foundation for an Angular-based tool supporting threat modeling with collaborative diagramming.
