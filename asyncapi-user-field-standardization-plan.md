# AsyncAPI User Field Standardization Plan

## Step 0: Create Backup
- Create backup: `cp shared/api-specs/tmi-asyncapi.yml shared/api-specs/tmi-asyncapi.yml.backup`

## Changes to Make

### Client-to-Server Messages (REMOVE user fields - server infers from session)
- `PresenterRequestMessage` - Remove `user` field  
- `ResyncRequestMessage` - Remove `user` field

### Client-to-Server Messages (RENAME user fields for clarity)
- `UndoRequestMessage.user` → `initiating_user` (user requesting undo)
- `RedoRequestMessage.user` → `initiating_user` (user requesting redo)

### Bidirectional Messages (RENAME user fields - needed for broadcast context)
- `DiagramOperationMessage.user` → `initiating_user` (user who made the operation)

### Server-to-Client Messages - Field Removals (optimization/redundancy)
- `ResyncResponseMessage` - Remove both `user` and `target_user` fields
- `PresenterDeniedMessage` - Remove `target_user` field (keep `user` as current presenter)
- `RemoveParticipantMessage` - Remove `user` field
- `PresenterCursorMessage` - Remove `user` field (frequent messages, presenter known from other messages)
- `PresenterSelectionMessage` - Remove `user` field (presenter known from other messages)

### Server-to-Client Messages - Rename for Clarity
- `PresenterDeniedMessage.user` → `current_presenter` (presenter who denied request)
- `ChangePresenterMessage.user` → `initiating_user` (owner who changed presenter)
- `CurrentPresenterMessage.user` → `current_presenter` (who is currently presenter)
- `HistoryOperationMessage.user` → `initiating_user` (user who originally made the operation)
- `ParticipantJoinedMessage.user` → `joined_user` (user who joined)
- `ParticipantLeftMessage.user` → `departed_user` (user who left)

### Convert String Fields to User Objects
- `ChangePresenterMessage.new_presenter` → `new_presenter: User` (user becoming new presenter)
- `RemoveParticipantMessage.target_user` → `removed_user: User` (user being removed)

## Implementation Steps

1. **Create backup of tmi-asyncapi.yml**
2. **Remove user fields from 2 client-to-server message schemas and payloads**
3. **Rename user fields in 3 client-to-server/bidirectional message schemas**
4. **Remove user fields from 2 high-frequency server-to-client messages** (PresenterCursor, PresenterSelection)
5. **Remove unnecessary fields from 3 other server-to-client messages**
6. **Rename user fields in 6 server-to-client message schemas and payloads for clarity**
7. **Convert 2 string fields to User objects** (new_presenter, target_user→removed_user)
8. **Update all corresponding payload examples**
9. **Validate AsyncAPI specification**

## Result
Optimized schema with clear semantics, smaller frequent messages, and consistent User object types throughout.