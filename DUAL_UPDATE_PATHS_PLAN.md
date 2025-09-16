# Plan: Centralized Diagram Updates with Update Vectors

## Problem Analysis
The system has two independent update paths for diagram changes:
1. **REST API Path**: Direct updates via PATCH/PUT to `/diagrams/{id}` 
2. **WebSocket Path**: Real-time collaborative updates via WebSocket operations

**Critical Issue**: REST API changes aren't propagated to WebSocket clients, causing stale state and validation conflicts.

## Solution Strategy: Centralized Updates with Version Control

### Phase 1: Database Schema Updates
**Files to modify:**
- Existing migration file in `auth/migrations/`
- `api/api.go` (OpenAPI generated types)
- `shared/api-specs/tmi-openapi.json` (OpenAPI specification)

**Changes:**
1. **Modify existing migration** to add update_vector column to diagrams table
2. **Add update_vector to DfdDiagram schema** (server-managed, readonly)
3. **Modify svg_image from string to object** with image and update_vector fields
4. **Regenerate API types** from updated OpenAPI spec

### Phase 2: Centralized Update Function
**Files to modify:**
- `api/websocket.go` (add shared update function)
- Create shared diagram update logic

**Changes:**
1. **Create UpdateDiagramCells function** that handles all diagram cell modifications
2. **Auto-increment update_vector** on any cells[] changes
3. **Notify WebSocket sessions** when updates come from REST API
4. **Single source of truth** for all diagram modifications

### Phase 3: REST Handler Refactoring
**Files to modify:**
- `api/threat_model_diagram_handlers.go` (PatchDiagram, UpdateDiagram)

**Changes:**
1. **Replace direct DiagramStore.Update calls** with centralized function
2. **Include update_vector in responses** (GET, PATCH, PUT)
3. **Validate that clients never send update_vector** (server-only)

### Phase 4: WebSocket Handler Refactoring  
**Files to modify:**
- `api/websocket.go` (diagram operation processing)
- `api/asyncapi_types.go` (StateCorrectionMessage)

**Changes:**
1. **Use centralized function** instead of direct store updates
2. **Enhance StateCorrectionMessage** with update_vector (remove cells[])
3. **Add staleness detection** - compare update vectors for conflicts
4. **Trigger client resync** when update_vector indicates stale state

### Phase 5: Version-Based Conflict Detection
**Files to modify:**
- `api/websocket.go` (operation validation)

**Changes:**
1. **Compare client's last known update_vector** with current diagram version
2. **Reject stale operations** and force client resync
3. **Simplified conflict resolution** - version mismatch = resync needed

## Expected Outcomes
1. **Single source of truth** for all diagram modifications
2. **Automatic conflict detection** via update_vector comparison  
3. **Simplified synchronization** - clients compare versions, resync when stale
4. **Clean separation** between server-managed versioning and client data
5. **Backwards compatibility** - update_vector is optional/readonly

## Implementation Order
1. Modify existing database migration
2. Update OpenAPI schema and regenerate types
3. Create centralized UpdateDiagramCells function
4. Refactor REST handlers to use centralized function
5. Refactor WebSocket handlers to use centralized function
6. Add version-based conflict detection