# Collaboration Protocol Flow

## Roles

Roles are not exclusive; a user may have more than one role in a session at the same time. Roles only apply to a specific session, for the duration of that session.

1. Host - The host is the user who initiated a session. During a session, the host (only) is allowed to eject users and to control who becomes a presenter. The host role does not confer any permission on the diagram itself; it only affects the set of websocket operations that will be honored by the server.
2. Participant - Any user in a session. Technically the host is also a participant but for brevity and clarity we usually refer to the user as either a host or a participant, but not both.
3. Presenter - the presenter is a user (initially the host) who is able to share their cursor position and their set of selected objects, for purposes of guiding a discussion about the diagram. Participants are able to request that the host allow them to present, and the host may allow or deny such requests. The host may also revoke the presenter role from a user and re-assume the role themself. Typically a client will show the role of presenter separately from the role of host or participant.

In the UI, the role "host" is usually represented by the material symbols outlined icon "Shield Person". Participants are identified by the icon "Person". Presenter is identified by the icon "Podium".

## Permissions

Users in a collaboration session may have either "reader" or "writer" permissions in the session. The permission level is determined at the start of the session and cannot change during the session. A user gets session permissions based on their permissions on the diagram object (through the threat model). A user with "reader" permissions on the threat model & diagram, will get "reader" permissions in the session. A user with "writer" or "owner" permissions on the threat model & diagram, will get "writer" permissions in the session. Readers in a session may not make changes to the diagram; their clients may not send diagram operation requests, undo, or redo requests, and the server will reject such requests if received. Writers in a session may send operation requests, undo and/or redo requests. A well-behaved client application will prevent locally-initiated changes to the diagram during a session where the user has only reader permissions, to prevent the user from becoming confused about the state of the shared diagram.

In the UI, the permission "writer" is usually represented by the material symbols outlined icon "Edit". Reader is represented by the icon "Edit Off".

## Miscellaneous UI

In the UI, typically the material symbols outlined icon "Person Raised Hand" is used to represent requests for the presenter role, e.g. a button that a participant uses to request the role might use this icon, and any state showing that there is an outstanding request for the presenter role might show this icon, perhaps next to the requesting user.

## Messages

All messages include a `message_type` field with a snake_case value identifying the message type.

### Message Types Summary

| Message Type | Direction | Description |
| --- | --- | --- |
| `authorization_denied` | server→client | Authorization denial notification |
| `change_presenter_request` | client→server | Host requests to change the presenter |
| `diagram_operation_event` | server→client | Diagram operation broadcast event |
| `diagram_operation_request` | client→server | Client request for diagram operation |
| `diagram_state` | server→client | Full diagram state synchronization |
| `error` | server→client | Error notification |
| `operation_rejected` | server→client | Operation rejection notification |
| `participants_update` | server→client | Participant list updates |
| `presenter_cursor` | bidirectional | Presenter cursor position |
| `presenter_denied_event` | server→client | Presenter request denial notification |
| `presenter_denied_request` | client→server | Host denies a presenter request |
| `presenter_request` | client→server | Request to become presenter |
| `presenter_request_event` | server→client | Presenter request event broadcast to host |
| `presenter_selection` | bidirectional | Presenter element selection |
| `redo_request` | client→server | Redo operation request |
| `remove_participant_request` | client→server | Host requests to remove a participant |
| `sync_request` | client→server | Request for state resynchronization |
| `sync_status_request` | client→server | Request current update_vector only |
| `sync_status_response` | server→client | Response with current update_vector |
| `undo_request` | client→server | Undo operation request |

### Diagram Editing Messages

#### diagram_operation_request

`diagram_operation_request` is sent from a client to the server to request a change to a diagram. The server authorizes the change from the connection context. If the authorization is denied, then the server sends an `authorization_denied` message to the requestor.

If authorized, the server updates the diagram with the change. If the change is successful, the server adds information about the initiating user to create a `diagram_operation_event` and broadcasts it to all clients. If the operation cannot be completed by the server, the server responds with `operation_rejected`.

When the server applies a change to the diagram due to a `diagram_operation_request`, it creates an "inverse" operation (an operation that will revert the change) and adds the inverse operation to the undo stack for the collaboration session, and also clears the redo stack for the collaboration session.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| message_type | string | yes | `"diagram_operation_request"` |
| operation_id | UUID | yes | Client-generated unique identifier |
| base_vector | int64 | yes | Client's last known update_vector |
| operation | object | yes | Contains `type` and `cells` array |

#### diagram_operation_event

`diagram_operation_event` is broadcast from the server to all clients when the server updates the global diagram state. It contains the operation details plus context about the user who initiated the change.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| message_type | string | yes | `"diagram_operation_event"` |
| initiating_user | User | yes | User who initiated the operation |
| operation_id | UUID | yes | Original operation identifier |
| sequence_number | uint64 | yes | Server-assigned sequence number |
| update_vector | int64 | yes | New update_vector after operation |
| operation | object | yes | The applied operation |

#### undo_request

`undo_request` is sent from a client to the server and is a request to revert the diagram state to its state before the last `diagram_operation_event`. It is treated like a `diagram_operation_request`, only the cell(s) to modify are obtained by popping the top operation off of the undo stack for the collaboration session. If the undo is successful, the server creates and broadcasts a `diagram_operation_event` using the user context from the `undo_request` and the cell data from the operation retrieved from the undo stack, AND the server creates an inverse operation and adds it to the redo stack for the collaboration session.

If the undo request fails because the undo stack is empty, the server sends a `history_operation` message to the requesting client with message `no_operations_to_undo`.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| message_type | string | yes | `"undo_request"` |

#### redo_request

`redo_request` is sent from a client to the server and is a request to revert the diagram state to its state before the last `undo_request`. It is treated like a `diagram_operation_request`, only the cell(s) to modify are obtained by popping the top operation off of the redo stack for the collaboration session. If the redo is successful, the server creates and broadcasts a `diagram_operation_event` using the user context from the `redo_request` and the cell data from the operation retrieved from the redo stack, AND the server creates an inverse operation and adds it to the undo stack for the collaboration session.

If the redo request fails because the redo stack is empty, the server sends a `history_operation` message to the requesting client with message `no_operations_to_redo`.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| message_type | string | yes | `"redo_request"` |

### Error Messages

#### error

`error` is sent from the server to a client during various error conditions.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| message_type | string | yes | `"error"` |
| error | string | yes | Error code |
| message | string | yes | Human-readable description |
| code | string | no | Optional error code |
| details | string | no | Optional additional details |
| timestamp | string | no | ISO8601 timestamp |

#### authorization_denied

`authorization_denied` is sent from the server to a client in response to a `diagram_operation_request`, `undo_request`, or `redo_request`, if the client fails an authentication check, or if the client fails an authorization check for "Writer" or "Owner" permissions on the diagram.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| message_type | string | yes | `"authorization_denied"` |
| original_operation_id | UUID | yes | The rejected operation's ID |
| reason | string | yes | One of: `insufficient_permissions`, `read_only_user`, `invalid_user` |

#### operation_rejected

`operation_rejected` is sent from the server to the originating client when an operation fails validation or encounters a conflict.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| message_type | string | yes | `"operation_rejected"` |
| operation_id | UUID | yes | The rejected operation's ID |
| sequence_number | uint64 | no | If assigned before rejection |
| update_vector | int64 | yes | Current server update_vector |
| reason | string | yes | Rejection reason code |
| message | string | yes | Human-readable description |
| details | string | no | Additional details |
| affected_cells | array | no | Cell IDs involved in conflict |
| requires_resync | boolean | yes | Whether client should resync |
| timestamp | string | yes | ISO8601 timestamp |

**Rejection Reason Codes:**
- `validation_failed`: Operation structure/data invalid
- `conflict_detected`: Concurrent modification conflict
- `no_state_change`: Idempotent no-op
- `diagram_not_found`: Target diagram deleted
- `permission_denied`: User lacks mutation permission
- `invalid_operation_type`: Unknown operation type
- `empty_operation`: No cell operations in batch
- `empty_history`: Undo/redo stack is empty

### Participant Management Messages

These messages manage and communicate the list of participants in a session.

#### participants_update

`participants_update` is broadcast by the server to all clients whenever there is a change to any participant - a new participant joins, a participant leaves or is removed, a change in presenter occurs, etc.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| message_type | string | yes | `"participants_update"` |
| participants | array | yes | Array of User objects |
| host | User | yes | The session host |
| current_presenter | User | no | Current presenter (null if none) |

#### remove_participant_request

`remove_participant_request` is sent by the host to the server, and is an instruction to disconnect the specified participant and add them to the deny list for the current session.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| message_type | string | yes | `"remove_participant_request"` |
| removed_user | User | yes | User to remove (must include ProviderId) |

The server validates that the `removed_user` information matches an actual connected client. If a mismatch is detected (potential spoofing), the requesting client is removed and blocked instead.

### Presentation Messages

These messages allow the presenter to share their activity with all other participants in the collaboration session.

#### presenter_cursor

`presenter_cursor` is sent by the active presenter to the server, which then rebroadcasts it to all other participants in the session. It contains the x,y position of the cursor within the diagram canvas. The client must use AntV/X6 transforms to transform the absolute position of the cursor on the screen to the position of the cursor within the diagram canvas, accounting for panning and zooming. Each receiver must invert that transformation to place the cursor in the right place in their viewport.

If a `presenter_cursor` is sent to the server by a participant who is not the active presenter, the message is silently ignored.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| message_type | string | yes | `"presenter_cursor"` |
| cursor_position | object | yes | Contains `x` and `y` coordinates |

#### presenter_selection

`presenter_selection` is sent by the active presenter to the server which then rebroadcasts it to all other participants in the session. It contains the list of currently selected cells in the presenter's diagram canvas.

If a `presenter_selection` is sent to the server by a participant who is not the active presenter, the message is silently ignored.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| message_type | string | yes | `"presenter_selection"` |
| selected_cells | array | yes | Array of cell UUIDs |

### Presenter Management Messages

These messages are used to manage and transition who is currently presenting.

#### change_presenter_request

`change_presenter_request` is sent from the host's client to the server, and is an instruction from the host to tell the server to shift the presenter role to the specified user. If the specified user is not a participant in the current session, the server sends an `error` message to the client.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| message_type | string | yes | `"change_presenter_request"` |
| new_presenter | User | yes | User to become presenter (must include ProviderId) |

The server validates that the `new_presenter` information matches an actual connected client.

#### presenter_request

`presenter_request` is sent by a participant to the server, and is a request to notify the host that the requester wishes to be the presenter.

- If the requester is the host, they automatically become the presenter (the host can approve their own request).
- If the requester is not the host, the server sends a `presenter_request_event` to the host.
- If the requester is already the current presenter, the message is ignored.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| message_type | string | yes | `"presenter_request"` |

#### presenter_request_event

`presenter_request_event` is sent by the server to the host's client, and is a relay of the `presenter_request` with context about the requesting user added by the server.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| message_type | string | yes | `"presenter_request_event"` |
| requesting_user | User | yes | User requesting presenter role |

#### presenter_denied_request

`presenter_denied_request` is sent by the host to the server to deny a pending presenter request.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| message_type | string | yes | `"presenter_denied_request"` |
| denied_user | User | yes | User whose request is denied |

#### presenter_denied_event

`presenter_denied_event` is sent by the server to the participant whose presenter request was denied.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| message_type | string | yes | `"presenter_denied_event"` |
| denied_user | User | yes | The denied user (from authenticated context) |

### Synchronization Messages

#### diagram_state

`diagram_state` is sent from the server to a client to synchronize the client's diagram state with the server's diagram state. The message includes the update vector and the list of cells for the diagram; the complete diagram can be reconstructed from this message.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| message_type | string | yes | `"diagram_state"` |
| diagram_id | UUID | yes | Diagram identifier |
| update_vector | int64 | yes | Current update_vector |
| cells | array | yes | Complete array of cell objects |

#### sync_request

`sync_request` is sent by any participant to the server, and is a request for the server to send diagram state back to the requesting client. If the client includes an `update_vector` that matches the server's current vector, the server responds with `sync_status_response`. Otherwise, the server responds with `diagram_state`.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| message_type | string | yes | `"sync_request"` |
| update_vector | int64 | no | Client's current update_vector (optional) |

#### sync_status_request

`sync_status_request` is sent by any participant to request only the current update_vector without full state.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| message_type | string | yes | `"sync_status_request"` |

#### sync_status_response

`sync_status_response` is sent by the server in response to `sync_status_request`, providing just the current update_vector.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| message_type | string | yes | `"sync_status_response"` |
| update_vector | int64 | yes | Current server update_vector |

## Session Flow

### Session Initiation

Collaboration is initiated by a user connecting their client to the WebSocket endpoint for a diagram. A user must have at least reader permission on the diagram (via the threat model) to initiate a collaboration session. If no session exists for the diagram, one is created automatically. The user who initiates the collaboration session then has the role of "host" in the session; certain activities are only permitted to the host. The host is also the initial presenter.

The server synchronizes state with the client by sending a `diagram_state` message. Then the server sends a `participants_update` message to the client.

```
1. C-->S: Connect to WebSocket /threat_models/{id}/diagrams/{id}/ws
2. S: Creates session if none exists; connecting user becomes host and presenter
3. S-->C: diagram_state
4. S-->C: participants_update
```

### Session Join

A user joins a collaboration session by having their client connect to the WebSocket. The user usually discovers the WebSocket URL via the REST API. The user must have at least reader permission on the diagram to join the session, and must not be on the deny list for the current session.

```
1. C-->S: Connect to WebSocket /threat_models/{id}/diagrams/{id}/ws
2. S: Validates permissions and deny list
3. S-->C: diagram_state
4. S-->C: participants_update (includes new participant)
5. S-->all other clients: participants_update
```

### Diagram State Sync

The server synchronizes diagram state with clients by sending a `diagram_state` message with the current state of the diagram and the update vector. The client may choose to:
- Disregard this message if the update vector matches the client's current state
- Re-initialize their local copy of the diagram with the state provided
- Use the REST API to retrieve the diagram if needed

### Participants Update

Whenever the participant list changes, or any role of any participant changes, the server broadcasts a `participants_update` with the list of current participants, the current presenter, and the host.

### Idle Sessions

Idle sessions (with no participants) are detected by the server and closed immediately during the cleanup cycle.

## Heartbeat Mechanism

The server sends WebSocket-level ping frames to maintain connection health. Clients must respond with pong frames to indicate they are still alive.

| Parameter | Value | Purpose |
| --- | --- | --- |
| Ping interval | 30 seconds | Keep connection alive |
| Read timeout | 90 seconds | Detect dead connections |
| Write timeout | 10 seconds | Prevent hung writes |
| Max message size | 64 KB | Prevent oversized messages |

If no message or pong is received within 90 seconds, the connection is automatically closed.

## Session Inactivity Timeout

Sessions without activity are automatically cleaned up to conserve server resources.

- **Default timeout**: 5 minutes (configurable via `WEBSOCKET_INACTIVITY_TIMEOUT_SECONDS`)
- **Minimum timeout**: 15 seconds (enforced by server)
- **Cleanup check interval**: 15 seconds
- **Activity triggers**: Client register, client unregister, broadcast messages

When a session times out due to inactivity, all remaining clients receive an `error` message and are disconnected.

## Host Disconnection

When the host disconnects from a session:

1. Session state changes to "terminating"
2. All participants receive an `error` message: "Host has disconnected"
3. All participant connections are closed
4. Session is removed from the server

The session cannot continue without the host. This ensures the host maintains control over the collaboration session lifecycle.

## Presenter Disconnection

When the current presenter disconnects (but is not the host):

1. The host automatically becomes the new presenter
2. If the host is not connected, the first participant with writer permissions becomes presenter
3. If no writer-permission participants remain, the presenter role is cleared (null)
4. A `participants_update` is broadcast to all clients reflecting the change

## Deny List

When a host removes a participant via `remove_participant_request`:

1. The participant is added to the session's deny list (by provider_id)
2. The participant receives an `error` message explaining they were removed
3. The participant's WebSocket connection is closed
4. A `participants_update` is broadcast to remaining participants

If a denied user attempts to reconnect to the same session:

1. They are immediately rejected with an error message: "You have been removed from this collaboration session and cannot rejoin"
2. Their WebSocket connection is closed

The deny list is session-specific and does not affect other sessions or future sessions for the same diagram.

## Conflict Detection

The server uses an update vector system to detect concurrent modification conflicts.

### Update Vector

Each diagram has an `update_vector` (incrementing int64) that tracks changes:
- Starts at 0 or 1 when diagram is created
- Increments by 1 with each successful cell operation
- Clients track their own `base_vector` representing their last known state

### Conflict Detection Flow

1. Client sends `diagram_operation_request` with `base_vector` (their last known update_vector)
2. Server compares client's `base_vector` to the diagram's current `update_vector`
3. If `base_vector < update_vector` AND the same cells are affected by both the pending operation and intervening operations:
   - Conflict is detected
   - Server sends `operation_rejected` with `reason: "conflict_detected"` and `requires_resync: true`
4. Client should send `sync_request` to retrieve the latest state

### Resync Flow

```
Client receives operation_rejected with requires_resync: true

1. C-->S: sync_request (with current update_vector, or omitted)
2. S-->C: diagram_state (full state with current update_vector)
3. Client replaces local state with received state
4. Client may retry their operation if still applicable
```

## Undo/Redo

Each session maintains an operation history stack (last 100 operations) for undo/redo functionality.

### Undo Flow

1. Client sends `undo_request`
2. Server checks if undo stack has entries
3. If empty: Server sends `history_operation` with `message: "no_operations_to_undo"`
4. If has entries:
   - Get operation at current history position
   - Apply previous state to diagram
   - Move history position back by 1
   - Broadcast `diagram_operation_event` to all clients

### Redo Flow

1. Client sends `redo_request`
2. Server checks if redo stack has entries (operations after current position)
3. If empty: Server sends `history_operation` with `message: "no_operations_to_redo"`
4. If has entries:
   - Get operation at next history position
   - Re-apply operation to diagram
   - Move history position forward by 1
   - Broadcast `diagram_operation_event` to all clients

### Stack Behavior

- New `diagram_operation_request` clears the redo stack (you cannot redo after making a new change)
- Undo adds to redo stack
- Redo adds to undo stack
- Maximum 100 operations retained in history

## Security Considerations

### Identity Validation

When the host sends `remove_participant_request` or `presenter_denied_request`, the server validates that the provided user information matches an actual connected client:
- Checks `ProviderId` (user_id)
- Checks `Email`
- Checks `DisplayName`

If a mismatch is detected (potential spoofing attempt):
1. Security violation is logged
2. The requesting client (not the target) is removed and blocked
3. Error message sent: "Providing false information about other users"

### Server-Only Messages

Clients cannot send the following message types (server rejects with error):
- `participants_update`
- `diagram_operation_event`
- `presenter_denied_event`
- `presenter_request_event`
- `authorization_denied`
- `operation_rejected`
- `diagram_state`
- `sync_status_response`
- `error`
- `state_correction`
- `diagram_state_sync`
- `resync_response`

## User Object Structure

The `User` object appears in many messages and contains:

| Field | Type | Description |
| --- | --- | --- |
| provider_id | string | Provider-specific unique identifier (e.g., OAuth `sub` claim) |
| email | string | User's email address |
| display_name | string | User's display name |
| provider | string | Identity provider name (e.g., "google", "test") |

## Message Sequences

This section illustrates the message flow for common collaboration operations.

### Participant Requests to Become Presenter

When a non-host participant wants to become the presenter, they must request permission from the host.

**Successful request:**
```
Participant                    Server                         Host
    |                            |                              |
    |-- presenter_request ------>|                              |
    |                            |-- presenter_request_event -->|
    |                            |                              |
    |                            |   (Host reviews request)     |
    |                            |                              |
    |                            |<-- change_presenter_request -|
    |                            |                              |
    |<-- participants_update ----|---- participants_update ---->|
    |   (shows self as presenter)|   (shows participant as presenter)
```

**Denied request:**
```
Participant                    Server                         Host
    |                            |                              |
    |-- presenter_request ------>|                              |
    |                            |-- presenter_request_event -->|
    |                            |                              |
    |                            |   (Host denies request)      |
    |                            |                              |
    |                            |<-- presenter_denied_request -|
    |                            |                              |
    |<-- presenter_denied_event -|                              |
```

**Host requests presenter role (automatic approval):**
```
Host                           Server
  |                              |
  |-- presenter_request -------->|
  |                              |  (Host auto-approved)
  |<-- participants_update ------|---> (broadcast to all)
  |   (shows self as presenter)  |
```

### Host Changes Presenter

The host can directly assign the presenter role to any participant without a request.

```
Host                           Server                      Participant
  |                              |                              |
  |-- change_presenter_request ->|                              |
  |   (new_presenter: Bob)       |                              |
  |                              |  (Validates Bob is connected)|
  |                              |                              |
  |<-- participants_update ------|---- participants_update ---->|
  |   (shows Bob as presenter)   |   (shows self as presenter)  |
```

### Participant Edits the Diagram

When a participant with writer permissions edits the diagram.

**Successful edit:**
```
Writer                         Server                      All Clients
  |                              |                              |
  |-- diagram_operation_request->|                              |
  |   (operation_id, base_vector,|                              |
  |    operation)                |                              |
  |                              |  (Validates permissions)     |
  |                              |  (Checks for conflicts)      |
  |                              |  (Applies operation)         |
  |                              |  (Updates undo stack)        |
  |                              |  (Clears redo stack)         |
  |                              |                              |
  |<-- diagram_operation_event --|---- diagram_operation_event->|
  |   (includes initiating_user, |   (all clients get same msg) |
  |    sequence_number,          |                              |
  |    update_vector)            |                              |
```

**Edit rejected - conflict detected:**
```
Writer                         Server
  |                              |
  |-- diagram_operation_request->|
  |   (base_vector: 40)          |
  |                              |  (Server update_vector: 42)
  |                              |  (Same cells affected)
  |                              |  (Conflict detected!)
  |                              |
  |<-- operation_rejected -------|
  |   (reason: conflict_detected,|
  |    requires_resync: true)    |
  |                              |
  |-- sync_request ------------->|
  |                              |
  |<-- diagram_state ------------|
  |   (full cells array,         |
  |    update_vector: 42)        |
  |                              |
  |   (Client may retry edit)    |
```

**Edit rejected - reader attempting to edit:**
```
Reader                         Server
  |                              |
  |-- diagram_operation_request->|
  |                              |  (User has reader permission)
  |                              |
  |<-- authorization_denied -----|
  |   (reason: read_only_user)   |
```

### Undo Operation

Any participant with writer permissions can undo the last operation.

**Successful undo:**
```
Writer                         Server                      All Clients
  |                              |                              |
  |-- undo_request ------------->|                              |
  |                              |  (Pops from undo stack)      |
  |                              |  (Applies inverse operation) |
  |                              |  (Pushes to redo stack)      |
  |                              |                              |
  |<-- diagram_operation_event --|---- diagram_operation_event->|
  |   (shows reverted state)     |                              |
```

**Undo rejected - empty stack:**
```
Writer                         Server
  |                              |
  |-- undo_request ------------->|
  |                              |  (History position is 0)
  |                              |
  |<-- history_operation --------|
  |   (message: no_operations_to_undo)
```

### Redo Operation

After an undo, writers can redo the undone operation.

**Successful redo:**
```
Writer                         Server                      All Clients
  |                              |                              |
  |-- redo_request ------------->|                              |
  |                              |  (Pops from redo stack)      |
  |                              |  (Re-applies operation)      |
  |                              |  (Pushes to undo stack)      |
  |                              |                              |
  |<-- diagram_operation_event --|---- diagram_operation_event->|
  |   (shows restored state)     |                              |
```

**Redo rejected - empty stack (e.g., after new edit):**
```
Writer                         Server
  |                              |
  |-- redo_request ------------->|
  |                              |  (No operations after current position -
  |                              |   cleared by new operation)
  |                              |
  |<-- history_operation --------|
  |   (message: no_operations_to_redo)
```

### Presenter Shares Cursor and Selection

The active presenter can share their cursor position and selected elements.

```
Presenter                      Server                      Other Clients
  |                              |                              |
  |-- presenter_cursor --------->|                              |
  |   (cursor_position: {x, y})  |                              |
  |                              |---- presenter_cursor ------->|
  |                              |   (rebroadcast to others)    |
  |                              |                              |
  |-- presenter_selection ------>|                              |
  |   (selected_cells: [...])    |                              |
  |                              |---- presenter_selection ---->|
  |                              |   (rebroadcast to others)    |
```

**Non-presenter attempts to share (silently ignored):**
```
Participant                    Server
  |                              |
  |-- presenter_cursor --------->|
  |                              |  (User is not presenter)
  |                              |  (Message silently ignored)
  |                              |
```

### Host Removes a Participant

The host can remove any participant from the session.

```
Host                           Server                      Removed User    Others
  |                              |                              |            |
  |-- remove_participant_request>|                              |            |
  |   (removed_user: Bob)        |                              |            |
  |                              |  (Validates Bob exists)      |            |
  |                              |  (Adds Bob to deny list)     |            |
  |                              |                              |            |
  |                              |-------- error -------------->|            |
  |                              |  ("You have been removed")   |            |
  |                              |                              |            |
  |                              |  (Closes Bob's connection)   X            |
  |                              |                              |            |
  |<-- participants_update ------|---------------------- participants_update>|
  |   (Bob no longer listed)     |                              |            |
```

**Removed user attempts to rejoin:**
```
Denied User                    Server
  |                              |
  |== WebSocket connect ========>|
  |                              |  (Checks deny list)
  |                              |  (User is on deny list!)
  |                              |
  |<-- error --------------------|
  |   ("You have been removed    |
  |    from this collaboration   |
  |    session and cannot rejoin")|
  |                              |
  |  (Connection closed)         X
```

### New Participant Joins Session

When a new participant connects to an existing session.

```
New Participant                Server                      Existing Clients
  |                              |                              |
  |== WebSocket connect ========>|                              |
  |                              |  (Validates permissions)     |
  |                              |  (Checks deny list)          |
  |                              |  (Registers client)          |
  |                              |                              |
  |<-- diagram_state ------------|                              |
  |   (full cells, update_vector)|                              |
  |                              |                              |
  |<-- participants_update ------|---- participants_update ---->|
  |   (includes self in list)    |   (shows new participant)    |
```

### Client Requests Resync

When a client suspects their state is stale, they can request resynchronization.

**Client provides matching update_vector:**
```
Client                         Server
  |                              |
  |-- sync_request ------------->|
  |   (update_vector: 42)        |
  |                              |  (Server update_vector: 42)
  |                              |  (Vectors match!)
  |                              |
  |<-- sync_status_response -----|
  |   (update_vector: 42)        |
  |                              |
  |   (No action needed)         |
```

**Client provides stale update_vector:**
```
Client                         Server
  |                              |
  |-- sync_request ------------->|
  |   (update_vector: 40)        |
  |                              |  (Server update_vector: 42)
  |                              |  (Vectors differ!)
  |                              |
  |<-- diagram_state ------------|
  |   (full cells array,         |
  |    update_vector: 42)        |
  |                              |
  |   (Replace local state)      |
```

**Client requests just the current vector:**
```
Client                         Server
  |                              |
  |-- sync_status_request ------>|
  |                              |
  |<-- sync_status_response -----|
  |   (update_vector: 42)        |
  |                              |
  |   (Compare to local vector)  |
```

### Host Disconnects

When the host disconnects, the session terminates.

```
Host                           Server                      All Participants
  |                              |                              |
  X  (Host disconnects)          |                              |
  |                              |  (Detects host disconnect)   |
  |                              |  (Session → terminating)     |
  |                              |                              |
  |                              |----------- error ----------->|
  |                              |  ("Host has disconnected")   |
  |                              |                              |
  |                              |  (Closes all connections)    X
  |                              |                              |
  |                              |  (Session → terminated)      |
  |                              |  (Session removed from hub)  |
```

### Presenter Disconnects (Non-Host)

When the presenter disconnects but they are not the host.

```
Presenter                      Server                      Host & Others
  |                              |                              |
  X  (Presenter disconnects)     |                              |
  |                              |  (Detects presenter left)    |
  |                              |  (Host becomes presenter)    |
  |                              |                              |
  |                              |---- participants_update ---->|
  |                              |  (host now shown as presenter,|
  |                              |   old presenter removed)     |
```

### Connection Timeout (Heartbeat Failure)

When a client fails to respond to ping frames.

```
Client                         Server
  |                              |
  |<-------- ping ---------------|  (t=0s)
  |                              |
  |  (No pong response)          |
  |                              |
  |<-------- ping ---------------|  (t=30s)
  |                              |
  |  (No pong response)          |
  |                              |
  |<-------- ping ---------------|  (t=60s)
  |                              |
  |  (No pong response)          |
  |                              |  (t=90s - read deadline exceeded)
  |                              |  (Connection closed by server)
  X                              |
  |                              |  (Client unregistered)
  |                              |  (participants_update broadcast)
```

---

## Verification Summary

<!-- Verified against source code on 2025-01-24 -->

| Claim | Status | Source File(s) |
|-------|--------|----------------|
| Message types (21 types) | VERIFIED | `api/asyncapi_types.go:39-71` |
| Ping interval (30 seconds) | VERIFIED | `api/websocket.go:3533` |
| Read timeout (90 seconds) | VERIFIED | `api/websocket.go:3475` |
| Write timeout (10 seconds) | VERIFIED | `api/websocket.go:3555, 3602` |
| Max message size (64 KB) | VERIFIED | `api/websocket.go:3473` |
| Session inactivity default (5 minutes) | VERIFIED | `internal/config/config.go:339` |
| Cleanup interval (15 seconds) | VERIFIED | `api/websocket.go:1286` |
| History max entries (100) | VERIFIED | `api/websocket.go:594` |
| Host disconnect terminates session | VERIFIED | `api/websocket.go:2274-2328` |
| Presenter reassignment on disconnect | VERIFIED | `api/websocket.go:2207-2271` |
| Deny list (DeniedUsers map) | VERIFIED | `api/websocket.go:87, 1324-1336, 1834-1836` |
| Server-only messages rejection | VERIFIED | `api/websocket_handlers.go:82-88` |
| WebSocket endpoint path | VERIFIED | `api/server.go:90` |
| Undo/redo history_operation response | VERIFIED | `api/websocket.go:2062-2071, 2142-2151` |
