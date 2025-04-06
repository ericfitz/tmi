# Collaborative Diagram Editing API

This document describes a RESTful API with WebSocket support for a server application enabling collaborative diagram editing. The API uses JSON payloads, OAuth for authentication, JWTs for session management, and UUIDs for unique identification. Diagrams are modeled with metadata and components inspired by maxGraph cells.

## Overview

- **Base URL**: `https://api.example.com`
- **Authentication**: OAuth 2.0 with JWTs.
- **Real-Time**: WebSocket for collaborative editing.
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

### Collaboration

- **`GET /diagrams/{id}/collaborate`**: Gets session status.
- **`POST /diagrams/{id}/collaborate`**: Joins or starts a session.
- **`DELETE /diagrams/{id}/collaborate`**: Leaves a session.
- **WebSocket**: `wss://api.example.com/diagrams/{id}/ws` for real-time updates.

## Diagram Model

- **Fields**:
  - `id`: UUID.
  - `name`: String.
  - `description`: String.
  - `created_at`, `modified_at`: ISO8601 timestamps.
  - `owner`: Email address.
  - `authorization`: Array of `{subject: email, role: "reader"|"writer"|"owner"}`.
  - `metadata`: Array of `{key: string, value: string}`.
  - `components`: Array of maxGraph cells (`{id: uuid, type: string, data: object, metadata: array}`).

## Behavior and Implementation Choices

### Authentication

- **OAuth**: External provider handles user auth; server exchanges code for JWT.
- **JWT**: Used for stateless session management, validated on each request.

### Permissions

- **Roles**:
  - `reader`: View and join sessions (read-only).
  - `writer`: Edit and collaborate.
  - `owner`: Full control, including deleting and managing `authorization`.
- **Ownership**:
  - Initial `owner` set on creation with `"owner"` role.
  - `owner` can transfer to another `"owner"` in `authorization`.
  - Original `owner` retained in `authorization` as `"owner"` post-transfer.

### Collaboration

- **Sessions**: Managed via REST (`/collaborate`); active for 15 minutes without activity.
- **WebSocket**: Broadcasts JSON Patch operations; `"reader"` cannot edit.
- **Conflict Resolution**: Last-writer-wins (simplest approach).

### Design Choices

- **REST + WebSocket**: REST for structure, WebSocket for real-time efficiency.
- **JSON Patch**: Flexible, granular updates for collaboration.
- **UUIDs**: Ensures unique identification across distributed systems.
- **Last-Writer-Wins**: Chosen for simplicity; upgradeable to OT/CRDT if needed.
- **15-Minute Timeout**: Balances resource use and user convenience.

## Implementation Notes

- **Security**: All endpoints except `/auth/login` and `/auth/callback` require JWT.
- **Validation**: Server enforces role-based access, UUID uniqueness, and email format.
- **Scalability**: Stateless JWTs and WebSocket sessions support horizontal scaling.
- **Future Enhancements**: Could add versioning, audit logs, or advanced conflict resolution.

This API provides a robust foundation for an Angular-based collaborative diagramming tool, balancing simplicity, security, and real-time functionality.
