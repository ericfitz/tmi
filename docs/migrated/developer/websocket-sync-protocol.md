# WebSocket Diagram Synchronization Protocol

## Overview

This document describes the WebSocket message protocol for diagram state synchronization between clients and the TMI server during collaborative editing sessions.

<!-- NEEDS-REVIEW: The conflict detection mechanism described below uses base_vector comparison, but the actual server implementation uses cell-level state validation (cell exists/doesn't exist checks), not vector-based conflict detection. The base_vector field exists in the DiagramOperationRequest schema but is not used for conflict detection in the current implementation. -->

## Message Types

### Sync Messages

| Message | Direction | Purpose |
|---------|-----------|---------|
| **SyncStatusRequestMessage** | Client -> Server | Check server's current update vector |
| **SyncStatusResponseMessage** | Server -> Client | Report server's current update vector |
| **SyncRequestMessage** | Client -> Server | Request full state if client is stale |
| **DiagramStateMessage** | Server -> Client | Full diagram state (cells + update_vector) |

### Operation Messages

| Message | Direction | Purpose |
|---------|-----------|---------|
| **DiagramOperationRequest** | Client -> Server | Request to apply cell operations (includes base_vector) |
| **DiagramOperationEvent** | Server -> Clients | Broadcast of successfully applied operation |
| **OperationRejectedMessage** | Server -> Client | Operation failed with reason and current vector |

## Message Definitions

### SyncStatusRequestMessage

Client asks server for current update vector without receiving full state.

```json
{
  "message_type": "sync_status_request"
}
```

### SyncStatusResponseMessage

Server responds with current update vector.

```json
{
  "message_type": "sync_status_response",
  "update_vector": 42
}
```

### SyncRequestMessage

Client requests full diagram state if their version doesn't match server's. If `update_vector` is omitted or null, server always sends full state.

```json
{
  "message_type": "sync_request",
  "update_vector": 40
}
```

Server response:
- If client's `update_vector` matches server's -> `SyncStatusResponseMessage`
- If client's `update_vector` differs (or is null) -> `DiagramStateMessage`

### DiagramStateMessage

Full diagram state sent on initial connection or in response to `SyncRequestMessage`.

```json
{
  "message_type": "diagram_state",
  "diagram_id": "uuid",
  "update_vector": 42,
  "cells": [...]
}
```

### DiagramOperationRequest

Client requests to apply cell operations. Includes `base_vector` for potential conflict detection.

```json
{
  "message_type": "diagram_operation_request",
  "operation_id": "client-generated-uuid",
  "base_vector": 42,
  "operation": {
    "type": "patch",
    "cells": [
      {"id": "cell-uuid", "operation": "add", "data": {...}},
      {"id": "cell-uuid", "operation": "update", "data": {...}},
      {"id": "cell-uuid", "operation": "remove"}
    ]
  }
}
```

### DiagramOperationEvent

Broadcast to all clients when an operation is successfully applied.

```json
{
  "message_type": "diagram_operation_event",
  "initiating_user": {"id": "...", "display_name": "..."},
  "operation_id": "uuid",
  "sequence_number": 123,
  "update_vector": 43,
  "operation": {
    "type": "patch",
    "cells": [...]
  }
}
```

### OperationRejectedMessage

Sent to originating client when operation cannot be applied.

```json
{
  "message_type": "operation_rejected",
  "operation_id": "uuid",
  "sequence_number": 123,
  "reason": "conflict_detected",
  "message": "Operation conflicts with current state",
  "update_vector": 43,
  "affected_cells": ["cell-uuid-1", "cell-uuid-2"],
  "requires_resync": true
}
```

**Reason codes:**
- `validation_failed`: Operation structure or data failed validation
- `conflict_detected`: Operation conflicts with current diagram state
- `no_state_change`: Operation would result in no actual state change (idempotent no-op)
- `diagram_not_found`: Target diagram no longer exists
- `permission_denied`: User lacks permission for this operation
- `invalid_operation_type`: Operation type is not recognized
- `empty_operation`: Operation contains no cell operations

## Message Flows

### Initial Connection

```
Client connects via WebSocket
    -> Server sends DiagramStateMessage (full state)
    -> Server sends ParticipantsUpdateMessage
    -> Client is synchronized
```

### Normal Operation

```
Client sends DiagramOperationRequest
    -> Server validates operation against current cell state
    -> If valid: Apply, increment update_vector
        -> Broadcast DiagramOperationEvent to all clients
    -> If invalid: Send OperationRejectedMessage to sender
```

### Client Suspects Stale State

```
Option A: Check first, then decide
    Client sends SyncStatusRequestMessage
        -> Server sends SyncStatusResponseMessage(update_vector=X)
        -> Client compares: if stale, send SyncRequestMessage

Option B: Request state if stale
    Client sends SyncRequestMessage(update_vector=N)
        -> If N == server's vector: SyncStatusResponseMessage
        -> If N != server's vector: DiagramStateMessage
```

### Conflict Detection (Actual Implementation)

<!-- NEEDS-REVIEW: The following describes the ACTUAL implementation behavior, not the base_vector mechanism originally documented -->

The server validates operations using cell-level state checks:

```
Client sends DiagramOperationRequest with "add" operation for Cell 1
    -> Server checks if Cell 1 already exists in current state
    -> If exists: Treated as idempotent update (no conflict, applies update)
    -> If not exists: Normal add operation

Client sends DiagramOperationRequest with "update" operation for Cell 1
    -> Server checks if Cell 1 exists in current state
    -> If exists: Apply update, increment update_vector
    -> If not exists: Reject with "update_nonexistent_cell" (conflict_detected)

Client sends DiagramOperationRequest with "remove" operation for Cell 1
    -> Server checks if Cell 1 exists in current state
    -> If exists: Remove cell, increment update_vector
    -> If not exists: Treated as idempotent (no error, no state change)
```

### Non-Conflicting Concurrent Operations

```
Client A: Update Cell 1
Client B: Update Cell 2 (different cell = no conflict)

Server processes A first:
    -> Apply, vector=6
    -> Broadcast DiagramOperationEvent to all

Server processes B:
    -> Different cell, no conflict
    -> Apply, vector=7
    -> Broadcast DiagramOperationEvent to all
```

## Conflict Detection Rules

Operations are validated against current cell state:

**Add Operations:**
- If cell ID doesn't exist: Add cell normally
- If cell ID already exists: Treated as idempotent update

**Update Operations:**
- If cell ID exists: Update cell
- If cell ID doesn't exist: Rejected as `update_nonexistent_cell` (conflict)

**Remove Operations:**
- If cell ID exists: Remove cell
- If cell ID doesn't exist: Idempotent (no error, no state change)

Operations do NOT conflict when:
- They affect different cells
- Add operations for existing cells (treated as updates)
- Remove operations for non-existent cells (idempotent)

## Client Implementation Notes

1. Track local `update_vector` starting from `DiagramStateMessage`
2. Include current `update_vector` as `base_vector` in all `DiagramOperationRequest` messages (for potential future use)
3. Update local `update_vector` when receiving `DiagramOperationEvent`
4. On `OperationRejectedMessage`, use `SyncRequestMessage` to resync if `requires_resync` is true
5. Optimistic updates are allowed but must be rolled back on rejection

## Related Documentation

- [AsyncAPI Specification](../reference/apis/tmi-asyncapi.yml) - Complete WebSocket message schema
- [WebSocket API Reference](https://github.com/ericfitz/tmi/wiki/WebSocket-API-Reference) - Wiki documentation
- Source code: `/Users/efitz/Projects/tmi/api/asyncapi_types.go` - Message type definitions
- Source code: `/Users/efitz/Projects/tmi/api/websocket_diagram_handler.go` - Operation handler
- Source code: `/Users/efitz/Projects/tmi/api/websocket.go` - Cell operation validation

---

## Verification Summary

**Verified against source code (2025-01-24):**
- Message types confirmed in `api/asyncapi_types.go` (lines 55-70, 170-266, 446-523, 721-764)
- WebSocket endpoint path: `/threat_models/:threat_model_id/diagrams/:diagram_id/ws` confirmed in `api/server.go` (line 90)
- Initial connection sends `DiagramStateMessage` confirmed in `api/websocket.go` (lines 1372-1395)
- `OperationRejectedMessage` structure and reason codes confirmed in `api/asyncapi_types.go` (lines 721-764)
- Conflict detection uses cell-level state validation, NOT base_vector comparison (see `api/websocket.go` lines 2729-3265)
- AsyncAPI specification exists at `docs/reference/apis/tmi-asyncapi.yml`

**Items requiring review:**
- The `base_vector` field exists in `DiagramOperationRequest` but is not currently used for conflict detection. The actual implementation uses cell-level state validation.
