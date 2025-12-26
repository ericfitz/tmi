# WebSocket Diagram Synchronization Protocol

## Overview

This document describes the WebSocket message protocol for diagram state synchronization between clients and the TMI server during collaborative editing sessions.

## Message Types

### Sync Messages

| Message | Direction | Purpose |
|---------|-----------|---------|
| **SyncStatusRequestMessage** | Client → Server | Check server's current update vector |
| **SyncStatusResponseMessage** | Server → Client | Report server's current update vector |
| **SyncRequestMessage** | Client → Server | Request full state if client is stale |
| **DiagramStateMessage** | Server → Client | Full diagram state (cells + update_vector) |

### Operation Messages

| Message | Direction | Purpose |
|---------|-----------|---------|
| **DiagramOperationRequest** | Client → Server | Request to apply cell operations (includes base_vector) |
| **DiagramOperationEvent** | Server → Clients | Broadcast of successfully applied operation |
| **OperationRejectedMessage** | Server → Client | Operation failed with reason and current vector |

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
- If client's `update_vector` matches server's → `SyncStatusResponseMessage`
- If client's `update_vector` differs (or is null) → `DiagramStateMessage`

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

Client requests to apply cell operations. Includes `base_vector` for conflict detection.

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

## Message Flows

### Initial Connection

```
Client connects via WebSocket
    → Server sends DiagramStateMessage (full state)
    → Client is synchronized
```

### Normal Operation

```
Client sends DiagramOperationRequest (base_vector=N)
    → Server validates operation
    → If valid: Apply, increment vector to N+1
        → Broadcast DiagramOperationEvent to all clients
    → If conflict: Send OperationRejectedMessage to sender
```

### Client Suspects Stale State

```
Option A: Check first, then decide
    Client sends SyncStatusRequestMessage
        → Server sends SyncStatusResponseMessage(update_vector=X)
        → Client compares: if stale, send SyncRequestMessage

Option B: Request state if stale
    Client sends SyncRequestMessage(update_vector=N)
        → If N == server's vector: SyncStatusResponseMessage
        → If N != server's vector: DiagramStateMessage
```

### Conflict Resolution

```
Client A (vector=5): Update Cell 1
Client B (vector=5): Update Cell 1 (same cell = conflict)

Server processes A first:
    → Apply, vector=6
    → Broadcast DiagramOperationEvent to all

Server processes B:
    → Detect conflict (base_vector=5, but server=6, same cell)
    → Send OperationRejectedMessage to B (update_vector=6)
    → B can request full state via SyncRequestMessage if needed
```

### Non-Conflicting Concurrent Operations

```
Client A (vector=5): Update Cell 1
Client B (vector=5): Update Cell 2 (different cell = no conflict)

Server processes A first:
    → Apply, vector=6
    → Broadcast DiagramOperationEvent to all

Server processes B:
    → Different cell, no conflict
    → Apply, vector=7
    → Broadcast DiagramOperationEvent to all
```

## Conflict Detection Rules

Operations conflict when they affect the same cell:
- Two updates to the same cell from different base vectors = conflict
- Add + Add for same cell ID = conflict
- Update + Remove for same cell = conflict

Operations do NOT conflict when:
- They affect different cells
- They are from the same base vector and processed atomically

## Client Implementation Notes

1. Track local `update_vector` starting from `DiagramStateMessage`
2. Include current `update_vector` as `base_vector` in all `DiagramOperationRequest` messages
3. Update local `update_vector` when receiving `DiagramOperationEvent`
4. On `OperationRejectedMessage`, use `SyncRequestMessage` to resync if `requires_resync` is true
5. Optimistic updates are allowed but must be rolled back on rejection
