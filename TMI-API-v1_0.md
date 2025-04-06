# TMI Collaborative Threat Modeling API

This document describes a RESTful API with WebSocket support for a server application enabling threat modeling with collaborative diagram editing. The API uses JSON payloads, OAuth for authentication, JWTs for session management, and UUIDs for unique identification. Diagrams support collaborative editing, while threat models provide a structured way to document threats linked to diagrams.

## Overview

- **Base URL**: `https://api.example.com`
- **Authentication**: OAuth 2.0 with JWTs.
- **Real-Time**: WebSocket for collaborative diagram editing (not applicable to threat models).
- **Format**: OpenAPI 3.1.

## Endpoints

### Authentication

- **`GET /auth/login`**: Redirects to OAuth provider for login.
- **`GET /auth/callback`**: Exchanges OAuth code for JWT.
- **`POST /auth/logout`**: Invalidates JWT and ends session.

### Diagram Management

- **`GET /diagrams`**: Lists diagrams accessible to the user (supports pagination and sorting).
- **`POST /diagrams`**: Creates a new diagram (owner set to creator).
- **`GET /diagrams/{id}`**: Retrieves a diagram’s details.
- **`PUT /diagrams/{id}`**: Fully updates a diagram.
- **`PATCH /diagrams/{id}`**: Partially updates a diagram (JSON Patch).
- **`DELETE /diagrams/{id}`**: Deletes a diagram (owner-only).

### Diagram Collaboration

- **`GET /diagrams/{id}/collaborate`**: Gets collaboration session status.
- **`POST /diagrams/{id}/collaborate`**: Joins or starts a session.
- **`DELETE /diagrams/{id}/collaborate`**: Leaves a session.
- **WebSocket**: `wss://api.example.com/diagrams/{id}/ws` for real-time updates.

### Threat Model Management

- **`GET /threat_models`**: Lists threat models accessible to the user (supports pagination and sorting).
- **`POST /threat_models`**: Creates a new threat model (owner set to creator).
- **`GET /threat_models/{id}`**: Retrieves a threat model’s details.
- **`PUT /threat_models/{id}`**: Fully updates a threat model.
- **`PATCH /threat_models/{id}`**: Partially updates a threat model (JSON Patch).
- **`DELETE /threat_models/{id}`**: Deletes a threat model (owner-only).

## Data Models

### Diagram

- **Fields**:
  - `id`: UUID.
  - `name`: String.
  - `description`: String.
  - `created_at`, `modified_at`: ISO8601 timestamps.
  - `owner`: Email address.
  - `authorization`: Array of `{subject: email, role: "reader"|"writer"|"owner"}`.
  - `metadata`: Array of `{key: string, value: string}`.
  - `components`: Array of maxGraph cells (`{id: uuid, type: string, data: object, metadata: array}`).

### Threat Model

- **Fields**:
  - `id`: UUID.
  - `name`: String.
  - `description`: String.
  - `created_at`, `modified_at`: ISO8601 timestamps.
  - `owner`: Email address.
  - `authorization`: Array of `{subject: email, role: "reader"|"writer"|"owner"}`.
  - `metadata`: Array of `{key: string, value: string}`.
  - `diagrams`: Array of diagram UUIDs (references to related diagrams).
  - `threats`: Array of threat objects.

### Threat

- **Fields**:
  - `id`: UUID.
  - `threat_model_id`: UUID (links to parent threat model).
  - `name`: String.
  - `description`: String.
  - `created_at`, `modified_at`: ISO8601 timestamps.
  - `metadata`: Array of `{key: string, value: string}`.

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
- **Conflict Resolution**: Last-writer-wins (simplest approach).

### Threat Model Management

- **No Collaboration**: Unlike diagrams, threat models do not support real-time collaboration or WebSocket integration.
- **Diagram Linking**: `diagrams` field references existing diagram UUIDs, enabling association without duplication.
- **Threats**: Embedded within `ThreatModel`, managed via `PUT` or `PATCH` operations on the parent object.

### Design Choices

- **REST + WebSocket**: REST for structure and management; WebSocket for real-time diagram editing (diagrams only).
- **JSON Patch**: Flexible, granular updates for both diagrams and threat models.
- **UUIDs**: Ensures unique identification across distributed systems.
- **Last-Writer-Wins**: Used for diagram collaboration; not applicable to threat models (single-user updates).
- **15-Minute Timeout**: Applies to diagram collaboration sessions; threat models rely on standard HTTP request lifecycle.
- **Threat Model Similarity**: Modeled after diagrams for consistency, with `diagrams` and `threats` replacing `components`.

## Implementation Notes

- **Security**: All endpoints except `/auth/login` and `/auth/callback` require JWT.
- **Validation**: Server enforces role-based access, UUID uniqueness, email format, and referential integrity (e.g., `diagrams` UUIDs must exist).
- **Scalability**: Stateless JWTs and WebSocket sessions (for diagrams) support horizontal scaling.
- **Future Enhancements**:
  - Diagrams: Add versioning, audit logs, or advanced conflict resolution (e.g., OT/CRDT).
  - Threat Models: Add separate threat endpoints, export functionality, or threat analysis features.

## Usage Examples

### Authentication

#### Initiate Login

```http
GET /auth/login?redirect_uri=https://client.example.com/callback
```

Response: Redirects (302) to OAuth provider (e.g., Location: https://oauth-provider.com/auth?...).

#### OAuth Callback

```http
GET /auth/callback?code=abc123&state=xyz789
```

Response (200):

```json
{
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "expires_in": 3600
}
```

#### Logout

```http
POST /auth/logout
Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...
```

Response: 204 No Content.

### Diagram Management

#### List Diagrams

```http
GET /diagrams?limit=2&offset=0
Authorization: Bearer <JWT>
```

Response (200):

```json
{
  "diagrams": [
    {
      "id": "123e4567-e89b-12d3-a456-426614174000",
      "name": "Workflow Diagram",
      "created_at": "2025-04-06T12:00:00Z",
      "modified_at": "2025-04-06T12:30:00Z",
      "owner": "user@example.com",
      "authorization": [{ "subject": "user@example.com", "role": "owner" }],
      "metadata": [],
      "components": []
    }
  ],
  "total": 1,
  "limit": 2,
  "offset": 0
}
```

#### Create a Diagram

```http
POST /diagrams
Authorization: Bearer <JWT>
Content-Type: application/json

{
  "name": "New Diagram",
  "description": "A test diagram."
}
```

Response (201):

```json
{
  "id": "123e4567-e89b-12d3-a456-426614174000",
  "name": "New Diagram",
  "description": "A test diagram.",
  "created_at": "2025-04-06T12:00:00Z",
  "modified_at": "2025-04-06T12:00:00Z",
  "owner": "user@example.com",
  "authorization": [{ "subject": "user@example.com", "role": "owner" }],
  "metadata": [],
  "components": []
}
```

Headers: Location: /diagrams/123e4567-e89b-12d3-a456-426614174000

#### Update a Diagram (Partial)

```http
PATCH /diagrams/123e4567-e89b-12d3-a456-426614174000
Authorization: Bearer <JWT>
Content-Type: application/json

[
  {
    "op": "add",
    "path": "/components/-",
    "value": {
      "id": "987fcdeb-12d3-4567-a890-426614174000",
      "type": "vertex",
      "data": {"label": "Start", "x": 100, "y": 100},
      "metadata": []
    }
  }
]
```

Response (200):

```json
{
  "id": "123e4567-e89b-12d3-a456-426614174000",
  "name": "New Diagram",
  "description": "A test diagram.",
  "created_at": "2025-04-06T12:00:00Z",
  "modified_at": "2025-04-06T12:01:00Z",
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

### Diagram Collaboration

#### Start a Collaboration Session

```http
POST /diagrams/123e4567-e89b-12d3-a456-426614174000/collaborate
Authorization: Bearer <JWT>
```

Response (200):

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

#### WebSocket Update

Client Message (via wss://...):

```json
{
  "operation": {
    "op": "replace",
    "path": "/components/0/data/label",
    "value": "Start Updated"
  }
}
```

Server Broadcast:

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

###Threat Model Management
####List Threat Models

```http
GET /threat_models?limit=1
Authorization: Bearer <JWT>
```

Response (200):

```json
{
  "threat_models": [
    {
      "id": "550e8400-e29b-41d4-a716-446655440000",
      "name": "System Threat Model",
      "description": "Threats for system X.",
      "created_at": "2025-04-06T12:00:00Z",
      "modified_at": "2025-04-06T12:00:00Z",
      "owner": "user@example.com",
      "authorization": [{ "subject": "user@example.com", "role": "owner" }],
      "metadata": [],
      "diagrams": [],
      "threats": []
    }
  ],
  "total": 1,
  "limit": 1,
  "offset": 0
}
```

#### Create a Threat Model

```http
POST /threat_models
Authorization: Bearer <JWT>
Content-Type: application/json
{
  "name": "System Threat Model",
  "description": "Threats for system X."
}
```

Response (201):

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "name": "System Threat Model",
  "description": "Threats for system X.",
  "created_at": "2025-04-06T12:00:00Z",
  "modified_at": "2025-04-06T12:00:00Z",
  "owner": "user@example.com",
  "authorization": [{ "subject": "user@example.com", "role": "owner" }],
  "metadata": [],
  "diagrams": [],
  "threats": []
}
```

Headers: Location: /threat_models/550e8400-e29b-41d4-a716-446655440000

#### Add a Threat via Patch

```http
PATCH /threat_models/550e8400-e29b-41d4-a716-446655440000
Authorization: Bearer <JWT>
Content-Type: application/json
[
  {
    "op": "add",
    "path": "/threats/-",
    "value": {
      "id": "6ba7b810-9dad-11d1-80b4-00c04fd430c8",
      "threat_model_id": "550e8400-e29b-41d4-a716-446655440000",
      "name": "Data Breach",
      "description": "Unauthorized data access.",
      "created_at": "2025-04-06T12:01:00Z",
      "modified_at": "2025-04-06T12:01:00Z",
      "metadata": []
    }
  }
]
```

Response (200):

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "name": "System Threat Model",
  "description": "Threats for system X.",
  "created_at": "2025-04-06T12:00:00Z",
  "modified_at": "2025-04-06T12:01:00Z",
  "owner": "user@example.com",
  "authorization": [{ "subject": "user@example.com", "role": "owner" }],
  "metadata": [],
  "diagrams": [],
  "threats": [
    {
      "id": "6ba7b810-9dad-11d1-80b4-00c04fd430c8",
      "threat_model_id": "550e8400-e29b-41d4-a716-446655440000",
      "name": "Data Breach",
      "description": "Unauthorized data access.",
      "created_at": "2025-04-06T12:01:00Z",
      "modified_at": "2025-04-06T12:01:00Z",
      "metadata": []
    }
  ]
}
```

#### Link a Diagram to a Threat Model

```http
PATCH /threat_models/550e8400-e29b-41d4-a716-446655440000
Authorization: Bearer <JWT>
Content-Type: application/json
[
  {
    "op": "add",
    "path": "/diagrams/-",
    "value": "123e4567-e89b-12d3-a456-426614174000"
  }
]
```

Response (200):

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "name": "System Threat Model",
  "description": "Threats for system X.",
  "created_at": "2025-04-06T12:00:00Z",
  "modified_at": "2025-04-06T12:02:00Z",
  "owner": "user@example.com",
  "authorization": [{"subject": "user@example.com", "role": "owner"}],
  "metadata": [],
  "diagrams": ["123e4567-e89b-12d3-a456-426614174000"],
  "threats": [...]
}
```
