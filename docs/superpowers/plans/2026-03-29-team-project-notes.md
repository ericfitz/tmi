# Team and Project Notes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add notes as full CRUD sub-resources to teams and projects with role-based `sharable` visibility.

**Architecture:** Mirror the existing threat model notes pattern: separate database tables (`team_notes`, `project_notes`), GORM store implementations, and Server handler methods. The `sharable` flag provides access control — regular users only see/create `sharable=true` notes; admins/security reviewers can create private `sharable=false` notes. Authorization checks reuse existing `IsTeamMemberOrAdmin`/`IsProjectTeamMemberOrAdmin` helpers plus `IsGroupMemberFromContext` for security reviewer detection.

**Tech Stack:** Go, Gin, GORM, oapi-codegen, OpenAPI 3.0.3, jq (for large JSON editing)

**Spec:** `docs/superpowers/specs/2026-03-29-team-project-notes-design.md`

---

## File Structure

### New files
| File | Responsibility |
|------|---------------|
| `api/models/team_project_note_models.go` | GORM models for `TeamNoteRecord` and `ProjectNoteRecord` |
| `api/team_note_store.go` | `TeamNoteStoreInterface` + `GormTeamNoteStore` implementation |
| `api/project_note_store.go` | `ProjectNoteStoreInterface` + `GormProjectNoteStore` implementation |
| `api/team_note_handlers.go` | Team note CRUD handlers on `*Server` |
| `api/project_note_handlers.go` | Project note CRUD handlers on `*Server` |
| `api/team_note_handlers_test.go` | Unit tests for team note handlers |
| `api/project_note_handlers_test.go` | Unit tests for project note handlers |

### Modified files
| File | Change |
|------|--------|
| `api-schema/tmi-openapi.json` | Add schemas, parameters, endpoints, list responses |
| `api/api.go` | Regenerated from OpenAPI spec |
| `api/models/models.go` | Add new models to `AllModels()` |
| `api/store.go` | Add global store variables and initialization |
| `api/server.go` | No change needed — handlers are on `*Server` which already satisfies `ServerInterface` |

---

## Task 1: Add GORM Models

**Files:**
- Create: `api/models/team_project_note_models.go`
- Modify: `api/models/models.go:771-818` (AllModels function)

- [ ] **Step 1: Create the GORM model file**

Create `api/models/team_project_note_models.go`:

```go
package models

import (
	"time"

	"github.com/ericfitz/tmi/api/validation"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// TeamNoteRecord represents a note attached to a team
type TeamNoteRecord struct {
	ID           string    `gorm:"primaryKey;type:varchar(36)"`
	TeamID       string    `gorm:"type:varchar(36);not null;index:idx_tnote_team;index:idx_tnote_team_name,priority:1"`
	Name         string    `gorm:"type:varchar(256);not null;index:idx_tnote_name;index:idx_tnote_team_name,priority:2"`
	Content      DBText    `gorm:"not null"`
	Description  *string   `gorm:"type:varchar(2048)"`
	TimmyEnabled DBBool    `gorm:"default:1"`
	Sharable     DBBool    `gorm:"not null"`
	CreatedAt    time.Time `gorm:"not null;autoCreateTime;index:idx_tnote_created"`
	ModifiedAt   time.Time `gorm:"not null;autoUpdateTime"`

	// Relationships
	Team TeamRecord `gorm:"foreignKey:TeamID;constraint:OnDelete:CASCADE"`
}

// TableName specifies the table name for TeamNoteRecord
func (TeamNoteRecord) TableName() string {
	return tableName("team_notes")
}

// BeforeCreate generates a UUID if not set and validates required fields.
func (n *TeamNoteRecord) BeforeCreate(tx *gorm.DB) error {
	if n.ID == "" {
		n.ID = uuid.New().String()
	}
	if err := validation.ValidateNonEmpty("name", n.Name); err != nil {
		return err
	}
	if err := validation.ValidateNonEmpty("content", string(n.Content)); err != nil {
		return err
	}
	return nil
}

// ProjectNoteRecord represents a note attached to a project
type ProjectNoteRecord struct {
	ID           string    `gorm:"primaryKey;type:varchar(36)"`
	ProjectID    string    `gorm:"type:varchar(36);not null;index:idx_pnote_project;index:idx_pnote_project_name,priority:1"`
	Name         string    `gorm:"type:varchar(256);not null;index:idx_pnote_name;index:idx_pnote_project_name,priority:2"`
	Content      DBText    `gorm:"not null"`
	Description  *string   `gorm:"type:varchar(2048)"`
	TimmyEnabled DBBool    `gorm:"default:1"`
	Sharable     DBBool    `gorm:"not null"`
	CreatedAt    time.Time `gorm:"not null;autoCreateTime;index:idx_pnote_created"`
	ModifiedAt   time.Time `gorm:"not null;autoUpdateTime"`

	// Relationships
	Project ProjectRecord `gorm:"foreignKey:ProjectID;constraint:OnDelete:CASCADE"`
}

// TableName specifies the table name for ProjectNoteRecord
func (ProjectNoteRecord) TableName() string {
	return tableName("project_notes")
}

// BeforeCreate generates a UUID if not set and validates required fields.
func (n *ProjectNoteRecord) BeforeCreate(tx *gorm.DB) error {
	if n.ID == "" {
		n.ID = uuid.New().String()
	}
	if err := validation.ValidateNonEmpty("name", n.Name); err != nil {
		return err
	}
	if err := validation.ValidateNonEmpty("content", string(n.Content)); err != nil {
		return err
	}
	return nil
}
```

- [ ] **Step 2: Register models in AllModels()**

In `api/models/models.go`, add the new models to `AllModels()` after `ProjectRelationshipRecord` and before `ThreatModel`:

```go
		&ProjectRelationshipRecord{},
		// Team and project notes (after teams/projects, before threat models)
		&TeamNoteRecord{},
		&ProjectNoteRecord{},
		// Threat models and related entities
		&ThreatModel{},
```

- [ ] **Step 3: Verify models compile**

Run: `cd /Users/efitz/Projects/tmi && go build ./api/models/...`
Expected: clean build, no errors

- [ ] **Step 4: Commit**

```bash
git add api/models/team_project_note_models.go api/models/models.go
git commit -m "feat(models): add GORM models for team and project notes (#217)"
```

---

## Task 2: Add OpenAPI Schema Definitions

**Files:**
- Modify: `api-schema/tmi-openapi.json`

All modifications use `jq` due to the file being ~1.9MB. Back up the file before starting.

- [ ] **Step 1: Back up the OpenAPI spec**

```bash
cp api-schema/tmi-openapi.json api-schema/tmi-openapi.json.$(date +%Y%m%d_%H%M%S).backup
```

- [ ] **Step 2: Add TeamProjectNoteBase schema**

Use `jq` to add the base schema to `components.schemas`:

```bash
jq '.components.schemas.TeamProjectNoteBase = {
  "type": "object",
  "description": "Base fields for team and project notes (user-writable only)",
  "required": ["name", "content"],
  "properties": {
    "name": {
      "type": "string",
      "description": "Note name",
      "minLength": 1,
      "maxLength": 256,
      "pattern": "^[^<>\"'\''&]*$",
      "x-oapi-codegen-extra-tags": { "binding": "required" }
    },
    "content": {
      "type": "string",
      "description": "Note content in markdown format. Safe inline HTML (tables, SVG, formatting) is allowed and sanitized server-side; dangerous elements (script, iframe, event handlers) are stripped.",
      "minLength": 1,
      "maxLength": 262144,
      "x-oapi-codegen-extra-tags": { "binding": "required" },
      "pattern": "^[^\\x00-\\x08\\x0B\\x0C\\x0E-\\x1F]*$"
    },
    "description": {
      "type": "string",
      "description": "Description of note purpose or context",
      "maxLength": 2048,
      "nullable": true,
      "pattern": "^[^<>\\x00-\\x08\\x0B\\x0C\\x0E-\\x1F]*$"
    },
    "timmy_enabled": {
      "type": "boolean",
      "description": "Whether the Timmy AI assistant is enabled for this entity",
      "default": true
    },
    "sharable": {
      "type": "boolean",
      "description": "Controls note visibility. When true, visible to all team/project members. When false, only visible to admins and security reviewers. Only admins and security reviewers can set this field; regular users who include this field in requests will receive a 403 error. Default: true for regular users, false for admins/security reviewers."
    }
  },
  "example": {
    "name": "Security Review Notes",
    "content": "Initial security review completed. Key findings documented.",
    "timmy_enabled": true
  }
}' api-schema/tmi-openapi.json > api-schema/tmi-openapi.tmp.json && mv api-schema/tmi-openapi.tmp.json api-schema/tmi-openapi.json
```

- [ ] **Step 3: Add TeamNote schema**

```bash
jq '.components.schemas.TeamNote = {
  "description": "Complete team note with server-generated fields",
  "allOf": [
    { "$ref": "#/components/schemas/TeamProjectNoteBase" },
    {
      "type": "object",
      "required": ["id"],
      "properties": {
        "id": {
          "type": "string",
          "format": "uuid",
          "readOnly": true,
          "description": "Unique identifier"
        },
        "created_at": {
          "type": "string",
          "format": "date-time",
          "readOnly": true,
          "description": "Creation timestamp"
        },
        "modified_at": {
          "type": "string",
          "format": "date-time",
          "readOnly": true,
          "description": "Last modification timestamp"
        }
      }
    }
  ]
}' api-schema/tmi-openapi.json > api-schema/tmi-openapi.tmp.json && mv api-schema/tmi-openapi.tmp.json api-schema/tmi-openapi.json
```

- [ ] **Step 4: Add ProjectNote schema**

```bash
jq '.components.schemas.ProjectNote = {
  "description": "Complete project note with server-generated fields",
  "allOf": [
    { "$ref": "#/components/schemas/TeamProjectNoteBase" },
    {
      "type": "object",
      "required": ["id"],
      "properties": {
        "id": {
          "type": "string",
          "format": "uuid",
          "readOnly": true,
          "description": "Unique identifier"
        },
        "created_at": {
          "type": "string",
          "format": "date-time",
          "readOnly": true,
          "description": "Creation timestamp"
        },
        "modified_at": {
          "type": "string",
          "format": "date-time",
          "readOnly": true,
          "description": "Last modification timestamp"
        }
      }
    }
  ]
}' api-schema/tmi-openapi.json > api-schema/tmi-openapi.tmp.json && mv api-schema/tmi-openapi.tmp.json api-schema/tmi-openapi.json
```

- [ ] **Step 5: Add TeamNoteInput and ProjectNoteInput aliases**

```bash
jq '.components.schemas.TeamNoteInput = { "$ref": "#/components/schemas/TeamProjectNoteBase" } | .components.schemas.ProjectNoteInput = { "$ref": "#/components/schemas/TeamProjectNoteBase" }' api-schema/tmi-openapi.json > api-schema/tmi-openapi.tmp.json && mv api-schema/tmi-openapi.tmp.json api-schema/tmi-openapi.json
```

- [ ] **Step 6: Add TeamNoteListItem and ProjectNoteListItem schemas**

```bash
jq '.components.schemas.TeamNoteListItem = {
  "type": "object",
  "description": "Team note summary for list responses (content omitted)",
  "required": ["id", "name"],
  "properties": {
    "id": { "type": "string", "format": "uuid", "readOnly": true },
    "name": { "type": "string", "minLength": 1, "maxLength": 256 },
    "description": { "type": "string", "maxLength": 2048, "nullable": true },
    "timmy_enabled": { "type": "boolean" },
    "sharable": { "type": "boolean" },
    "created_at": { "type": "string", "format": "date-time", "readOnly": true },
    "modified_at": { "type": "string", "format": "date-time", "readOnly": true }
  }
} | .components.schemas.ProjectNoteListItem = {
  "type": "object",
  "description": "Project note summary for list responses (content omitted)",
  "required": ["id", "name"],
  "properties": {
    "id": { "type": "string", "format": "uuid", "readOnly": true },
    "name": { "type": "string", "minLength": 1, "maxLength": 256 },
    "description": { "type": "string", "maxLength": 2048, "nullable": true },
    "timmy_enabled": { "type": "boolean" },
    "sharable": { "type": "boolean" },
    "created_at": { "type": "string", "format": "date-time", "readOnly": true },
    "modified_at": { "type": "string", "format": "date-time", "readOnly": true }
  }
}' api-schema/tmi-openapi.json > api-schema/tmi-openapi.tmp.json && mv api-schema/tmi-openapi.tmp.json api-schema/tmi-openapi.json
```

- [ ] **Step 7: Add list response schemas**

```bash
jq '.components.schemas.ListTeamNotesResponse = {
  "type": "object",
  "description": "Paginated list of team notes",
  "required": ["notes", "total", "limit", "offset"],
  "properties": {
    "notes": { "type": "array", "items": { "$ref": "#/components/schemas/TeamNoteListItem" }, "maxItems": 1000 },
    "total": { "type": "integer", "description": "Total number of notes" },
    "limit": { "type": "integer", "description": "Pagination limit" },
    "offset": { "type": "integer", "description": "Pagination offset" }
  }
} | .components.schemas.ListProjectNotesResponse = {
  "type": "object",
  "description": "Paginated list of project notes",
  "required": ["notes", "total", "limit", "offset"],
  "properties": {
    "notes": { "type": "array", "items": { "$ref": "#/components/schemas/ProjectNoteListItem" }, "maxItems": 1000 },
    "total": { "type": "integer", "description": "Total number of notes" },
    "limit": { "type": "integer", "description": "Pagination limit" },
    "offset": { "type": "integer", "description": "Pagination offset" }
  }
}' api-schema/tmi-openapi.json > api-schema/tmi-openapi.tmp.json && mv api-schema/tmi-openapi.tmp.json api-schema/tmi-openapi.json
```

- [ ] **Step 8: Add TeamNoteId and ProjectNoteId parameters**

```bash
jq '.components.parameters.TeamNoteId = {
  "name": "team_note_id",
  "in": "path",
  "required": true,
  "schema": { "type": "string", "format": "uuid" },
  "description": "Team note identifier"
} | .components.parameters.ProjectNoteId = {
  "name": "project_note_id",
  "in": "path",
  "required": true,
  "schema": { "type": "string", "format": "uuid" },
  "description": "Project note identifier"
}' api-schema/tmi-openapi.json > api-schema/tmi-openapi.tmp.json && mv api-schema/tmi-openapi.tmp.json api-schema/tmi-openapi.json
```

- [ ] **Step 9: Validate the schema so far**

Run: `make validate-openapi`
Expected: Validation passes (may have existing warnings but no new errors)

- [ ] **Step 10: Commit schema definitions**

```bash
git add api-schema/tmi-openapi.json
git commit -m "feat(api): add OpenAPI schema definitions for team and project notes (#217)"
```

---

## Task 3: Add OpenAPI Endpoint Definitions

**Files:**
- Modify: `api-schema/tmi-openapi.json`

- [ ] **Step 1: Add team notes collection endpoint (/teams/{team_id}/notes)**

```bash
jq '.paths["/teams/{team_id}/notes"] = {
  "get": {
    "tags": ["Teams"],
    "summary": "List notes for a team",
    "description": "Returns a paginated list of notes for the specified team. Non-admin/non-security-reviewer users only see sharable notes.",
    "operationId": "listTeamNotes",
    "security": [{ "bearerAuth": [] }],
    "parameters": [
      { "$ref": "#/components/parameters/TeamId" },
      { "$ref": "#/components/parameters/PaginationLimit" },
      { "$ref": "#/components/parameters/PaginationOffset" }
    ],
    "responses": {
      "200": {
        "description": "List of team notes",
        "content": { "application/json": { "schema": { "$ref": "#/components/schemas/ListTeamNotesResponse" } } },
        "headers": {
          "X-RateLimit-Limit": { "schema": { "type": "integer" } },
          "X-RateLimit-Remaining": { "schema": { "type": "integer" } },
          "X-RateLimit-Reset": { "schema": { "type": "integer" } }
        }
      },
      "401": { "$ref": "#/components/responses/Error" },
      "403": { "$ref": "#/components/responses/Error" },
      "404": { "$ref": "#/components/responses/Error" },
      "429": { "$ref": "#/components/responses/TooManyRequests" },
      "500": { "$ref": "#/components/responses/Error" }
    },
    "x-rate-limit": {
      "scope": "user",
      "tier": "resource-operations",
      "limits": [{ "type": "requests_per_minute", "default": 100, "configurable": true, "quota_source": "user_api_quotas" }]
    }
  },
  "post": {
    "tags": ["Teams"],
    "summary": "Create a team note",
    "description": "Creates a new note for the specified team. Regular users always create sharable notes and must not include the sharable field. Admins/security reviewers default to non-sharable and may set sharable explicitly.",
    "operationId": "createTeamNote",
    "security": [{ "bearerAuth": [] }],
    "parameters": [
      { "$ref": "#/components/parameters/TeamId" }
    ],
    "requestBody": {
      "required": true,
      "content": { "application/json": { "schema": { "$ref": "#/components/schemas/TeamNoteInput" } } }
    },
    "responses": {
      "201": {
        "description": "Team note created",
        "content": { "application/json": { "schema": { "$ref": "#/components/schemas/TeamNote" } } }
      },
      "400": { "$ref": "#/components/responses/Error" },
      "401": { "$ref": "#/components/responses/Error" },
      "403": { "$ref": "#/components/responses/Error" },
      "404": { "$ref": "#/components/responses/Error" },
      "429": { "$ref": "#/components/responses/TooManyRequests" },
      "500": { "$ref": "#/components/responses/Error" }
    }
  }
}' api-schema/tmi-openapi.json > api-schema/tmi-openapi.tmp.json && mv api-schema/tmi-openapi.tmp.json api-schema/tmi-openapi.json
```

- [ ] **Step 2: Add team notes individual endpoint (/teams/{team_id}/notes/{team_note_id})**

```bash
jq '.paths["/teams/{team_id}/notes/{team_note_id}"] = {
  "get": {
    "tags": ["Teams"],
    "summary": "Get a team note",
    "description": "Returns a specific team note. Non-admin/non-security-reviewer users receive 404 for non-sharable notes.",
    "operationId": "getTeamNote",
    "security": [{ "bearerAuth": [] }],
    "parameters": [
      { "$ref": "#/components/parameters/TeamId" },
      { "$ref": "#/components/parameters/TeamNoteId" }
    ],
    "responses": {
      "200": { "description": "Team note", "content": { "application/json": { "schema": { "$ref": "#/components/schemas/TeamNote" } } } },
      "401": { "$ref": "#/components/responses/Error" },
      "403": { "$ref": "#/components/responses/Error" },
      "404": { "$ref": "#/components/responses/Error" },
      "429": { "$ref": "#/components/responses/TooManyRequests" },
      "500": { "$ref": "#/components/responses/Error" }
    }
  },
  "put": {
    "tags": ["Teams"],
    "summary": "Update a team note",
    "description": "Replaces a team note. Regular users can only update sharable notes and must not include the sharable field.",
    "operationId": "updateTeamNote",
    "security": [{ "bearerAuth": [] }],
    "parameters": [
      { "$ref": "#/components/parameters/TeamId" },
      { "$ref": "#/components/parameters/TeamNoteId" }
    ],
    "requestBody": {
      "required": true,
      "content": { "application/json": { "schema": { "$ref": "#/components/schemas/TeamNoteInput" } } }
    },
    "responses": {
      "200": { "description": "Team note updated", "content": { "application/json": { "schema": { "$ref": "#/components/schemas/TeamNote" } } } },
      "400": { "$ref": "#/components/responses/Error" },
      "401": { "$ref": "#/components/responses/Error" },
      "403": { "$ref": "#/components/responses/Error" },
      "404": { "$ref": "#/components/responses/Error" },
      "429": { "$ref": "#/components/responses/TooManyRequests" },
      "500": { "$ref": "#/components/responses/Error" }
    }
  },
  "patch": {
    "tags": ["Teams"],
    "summary": "Patch a team note",
    "description": "Applies JSON Patch operations to a team note. Regular users cannot patch non-sharable notes or modify the sharable field.",
    "operationId": "patchTeamNote",
    "security": [{ "bearerAuth": [] }],
    "parameters": [
      { "$ref": "#/components/parameters/TeamId" },
      { "$ref": "#/components/parameters/TeamNoteId" }
    ],
    "requestBody": {
      "required": true,
      "content": { "application/json": { "schema": { "type": "array", "items": { "$ref": "#/components/schemas/PatchOperation" } } } }
    },
    "responses": {
      "200": { "description": "Team note patched", "content": { "application/json": { "schema": { "$ref": "#/components/schemas/TeamNote" } } } },
      "400": { "$ref": "#/components/responses/Error" },
      "401": { "$ref": "#/components/responses/Error" },
      "403": { "$ref": "#/components/responses/Error" },
      "404": { "$ref": "#/components/responses/Error" },
      "429": { "$ref": "#/components/responses/TooManyRequests" },
      "500": { "$ref": "#/components/responses/Error" }
    }
  },
  "delete": {
    "tags": ["Teams"],
    "summary": "Delete a team note",
    "description": "Permanently deletes a team note. Regular users can only delete sharable notes; non-sharable notes return 404.",
    "operationId": "deleteTeamNote",
    "security": [{ "bearerAuth": [] }],
    "parameters": [
      { "$ref": "#/components/parameters/TeamId" },
      { "$ref": "#/components/parameters/TeamNoteId" }
    ],
    "responses": {
      "204": { "description": "Team note deleted" },
      "401": { "$ref": "#/components/responses/Error" },
      "403": { "$ref": "#/components/responses/Error" },
      "404": { "$ref": "#/components/responses/Error" },
      "429": { "$ref": "#/components/responses/TooManyRequests" },
      "500": { "$ref": "#/components/responses/Error" }
    }
  }
}' api-schema/tmi-openapi.json > api-schema/tmi-openapi.tmp.json && mv api-schema/tmi-openapi.tmp.json api-schema/tmi-openapi.json
```

- [ ] **Step 3: Add project notes collection endpoint (/projects/{project_id}/notes)**

Same structure as team notes but with `project_id` parameter, `ProjectNote`/`ProjectNoteInput` schemas, `ListProjectNotesResponse`, and operation IDs prefixed with `project` (e.g., `listProjectNotes`, `createProjectNote`). Tag: `"Projects"`.

```bash
jq '.paths["/projects/{project_id}/notes"] = {
  "get": {
    "tags": ["Projects"],
    "summary": "List notes for a project",
    "description": "Returns a paginated list of notes for the specified project. Non-admin/non-security-reviewer users only see sharable notes.",
    "operationId": "listProjectNotes",
    "security": [{ "bearerAuth": [] }],
    "parameters": [
      { "$ref": "#/components/parameters/ProjectId" },
      { "$ref": "#/components/parameters/PaginationLimit" },
      { "$ref": "#/components/parameters/PaginationOffset" }
    ],
    "responses": {
      "200": {
        "description": "List of project notes",
        "content": { "application/json": { "schema": { "$ref": "#/components/schemas/ListProjectNotesResponse" } } },
        "headers": {
          "X-RateLimit-Limit": { "schema": { "type": "integer" } },
          "X-RateLimit-Remaining": { "schema": { "type": "integer" } },
          "X-RateLimit-Reset": { "schema": { "type": "integer" } }
        }
      },
      "401": { "$ref": "#/components/responses/Error" },
      "403": { "$ref": "#/components/responses/Error" },
      "404": { "$ref": "#/components/responses/Error" },
      "429": { "$ref": "#/components/responses/TooManyRequests" },
      "500": { "$ref": "#/components/responses/Error" }
    },
    "x-rate-limit": {
      "scope": "user",
      "tier": "resource-operations",
      "limits": [{ "type": "requests_per_minute", "default": 100, "configurable": true, "quota_source": "user_api_quotas" }]
    }
  },
  "post": {
    "tags": ["Projects"],
    "summary": "Create a project note",
    "description": "Creates a new note for the specified project. Regular users always create sharable notes and must not include the sharable field. Admins/security reviewers default to non-sharable and may set sharable explicitly.",
    "operationId": "createProjectNote",
    "security": [{ "bearerAuth": [] }],
    "parameters": [
      { "$ref": "#/components/parameters/ProjectId" }
    ],
    "requestBody": {
      "required": true,
      "content": { "application/json": { "schema": { "$ref": "#/components/schemas/ProjectNoteInput" } } }
    },
    "responses": {
      "201": {
        "description": "Project note created",
        "content": { "application/json": { "schema": { "$ref": "#/components/schemas/ProjectNote" } } }
      },
      "400": { "$ref": "#/components/responses/Error" },
      "401": { "$ref": "#/components/responses/Error" },
      "403": { "$ref": "#/components/responses/Error" },
      "404": { "$ref": "#/components/responses/Error" },
      "429": { "$ref": "#/components/responses/TooManyRequests" },
      "500": { "$ref": "#/components/responses/Error" }
    }
  }
}' api-schema/tmi-openapi.json > api-schema/tmi-openapi.tmp.json && mv api-schema/tmi-openapi.tmp.json api-schema/tmi-openapi.json
```

- [ ] **Step 4: Add project notes individual endpoint (/projects/{project_id}/notes/{project_note_id})**

```bash
jq '.paths["/projects/{project_id}/notes/{project_note_id}"] = {
  "get": {
    "tags": ["Projects"],
    "summary": "Get a project note",
    "operationId": "getProjectNote",
    "security": [{ "bearerAuth": [] }],
    "parameters": [
      { "$ref": "#/components/parameters/ProjectId" },
      { "$ref": "#/components/parameters/ProjectNoteId" }
    ],
    "responses": {
      "200": { "description": "Project note", "content": { "application/json": { "schema": { "$ref": "#/components/schemas/ProjectNote" } } } },
      "401": { "$ref": "#/components/responses/Error" },
      "403": { "$ref": "#/components/responses/Error" },
      "404": { "$ref": "#/components/responses/Error" },
      "429": { "$ref": "#/components/responses/TooManyRequests" },
      "500": { "$ref": "#/components/responses/Error" }
    }
  },
  "put": {
    "tags": ["Projects"],
    "summary": "Update a project note",
    "operationId": "updateProjectNote",
    "security": [{ "bearerAuth": [] }],
    "parameters": [
      { "$ref": "#/components/parameters/ProjectId" },
      { "$ref": "#/components/parameters/ProjectNoteId" }
    ],
    "requestBody": {
      "required": true,
      "content": { "application/json": { "schema": { "$ref": "#/components/schemas/ProjectNoteInput" } } }
    },
    "responses": {
      "200": { "description": "Project note updated", "content": { "application/json": { "schema": { "$ref": "#/components/schemas/ProjectNote" } } } },
      "400": { "$ref": "#/components/responses/Error" },
      "401": { "$ref": "#/components/responses/Error" },
      "403": { "$ref": "#/components/responses/Error" },
      "404": { "$ref": "#/components/responses/Error" },
      "429": { "$ref": "#/components/responses/TooManyRequests" },
      "500": { "$ref": "#/components/responses/Error" }
    }
  },
  "patch": {
    "tags": ["Projects"],
    "summary": "Patch a project note",
    "operationId": "patchProjectNote",
    "security": [{ "bearerAuth": [] }],
    "parameters": [
      { "$ref": "#/components/parameters/ProjectId" },
      { "$ref": "#/components/parameters/ProjectNoteId" }
    ],
    "requestBody": {
      "required": true,
      "content": { "application/json": { "schema": { "type": "array", "items": { "$ref": "#/components/schemas/PatchOperation" } } } }
    },
    "responses": {
      "200": { "description": "Project note patched", "content": { "application/json": { "schema": { "$ref": "#/components/schemas/ProjectNote" } } } },
      "400": { "$ref": "#/components/responses/Error" },
      "401": { "$ref": "#/components/responses/Error" },
      "403": { "$ref": "#/components/responses/Error" },
      "404": { "$ref": "#/components/responses/Error" },
      "429": { "$ref": "#/components/responses/TooManyRequests" },
      "500": { "$ref": "#/components/responses/Error" }
    }
  },
  "delete": {
    "tags": ["Projects"],
    "summary": "Delete a project note",
    "operationId": "deleteProjectNote",
    "security": [{ "bearerAuth": [] }],
    "parameters": [
      { "$ref": "#/components/parameters/ProjectId" },
      { "$ref": "#/components/parameters/ProjectNoteId" }
    ],
    "responses": {
      "204": { "description": "Project note deleted" },
      "401": { "$ref": "#/components/responses/Error" },
      "403": { "$ref": "#/components/responses/Error" },
      "404": { "$ref": "#/components/responses/Error" },
      "429": { "$ref": "#/components/responses/TooManyRequests" },
      "500": { "$ref": "#/components/responses/Error" }
    }
  }
}' api-schema/tmi-openapi.json > api-schema/tmi-openapi.tmp.json && mv api-schema/tmi-openapi.tmp.json api-schema/tmi-openapi.json
```

- [ ] **Step 5: Validate the complete schema**

Run: `make validate-openapi`
Expected: Validation passes

- [ ] **Step 6: Regenerate API code**

Run: `make generate-api`
Expected: `api/api.go` regenerated with new types (`TeamNote`, `ProjectNote`, `TeamNoteInput`, `ProjectNoteInput`, `TeamNoteListItem`, `ProjectNoteListItem`, `ListTeamNotesResponse`, `ListProjectNotesResponse`) and new `ServerInterface` methods.

- [ ] **Step 7: Verify generated code compiles (expect failure — handlers not yet implemented)**

Run: `go build ./api/... 2>&1 | head -20`
Expected: Compilation errors about missing `ServerInterface` method implementations. Note which methods need implementing for Tasks 5-6.

- [ ] **Step 8: Commit schema and generated code**

```bash
git add api-schema/tmi-openapi.json api/api.go
git commit -m "feat(api): add OpenAPI endpoints for team and project notes (#217)"
```

---

## Task 4: Implement Store Layer

**Files:**
- Create: `api/team_note_store.go`
- Create: `api/project_note_store.go`
- Modify: `api/store.go:84-98` (global variables) and `api/store.go:106-157` (InitializeGormStores)

- [ ] **Step 1: Create team note store interface and implementation**

Create `api/team_note_store.go`. The store converts between API types (generated in `api/api.go`) and GORM model types (`models.TeamNoteRecord`). Key patterns to follow from existing stores:

- All methods take `context.Context` as first parameter
- Use `map[string]any{}` for GORM Where clauses (Oracle compatibility)
- Return `*RequestError` for store-level errors that map to HTTP status codes
- `List` returns `([]TeamNoteListItem, int, error)` where `int` is total count
- `includeNonSharable` parameter controls `sharable` filtering at query level

```go
package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"gorm.io/gorm"
)

// TeamNoteStoreInterface defines operations for team note storage
type TeamNoteStoreInterface interface {
	Create(ctx context.Context, note *TeamNote, teamID string) (*TeamNote, error)
	Get(ctx context.Context, id string) (*TeamNote, error)
	Update(ctx context.Context, id string, note *TeamNote, teamID string) (*TeamNote, error)
	Delete(ctx context.Context, id string) error
	Patch(ctx context.Context, id string, operations []PatchOperation) (*TeamNote, error)
	List(ctx context.Context, teamID string, offset, limit int, includeNonSharable bool) ([]TeamNoteListItem, int, error)
	Count(ctx context.Context, teamID string, includeNonSharable bool) (int, error)
}

// GormTeamNoteStore implements TeamNoteStoreInterface using GORM
type GormTeamNoteStore struct {
	db *gorm.DB
}

// NewGormTeamNoteStore creates a new GormTeamNoteStore
func NewGormTeamNoteStore(db *gorm.DB) *GormTeamNoteStore {
	return &GormTeamNoteStore{db: db}
}
```

Then implement each method. The `Create` method converts from API type to GORM record, creates it, and converts back:

```go
func (s *GormTeamNoteStore) Create(ctx context.Context, note *TeamNote, teamID string) (*TeamNote, error) {
	logger := slogging.Get()

	// Verify team exists
	var count int64
	if err := s.db.WithContext(ctx).Model(&models.TeamRecord{}).Where(map[string]any{"id": teamID}).Count(&count).Error; err != nil {
		return nil, &RequestError{Status: http.StatusInternalServerError, Code: "store_error", Message: "Failed to verify team"}
	}
	if count == 0 {
		return nil, &RequestError{Status: http.StatusNotFound, Code: "not_found", Message: "Team not found"}
	}

	record := teamNoteToRecord(note, teamID)
	if err := s.db.WithContext(ctx).Create(&record).Error; err != nil {
		logger.Error("Failed to create team note: %v", err)
		return nil, &RequestError{Status: http.StatusInternalServerError, Code: "store_error", Message: "Failed to create team note"}
	}

	return teamNoteFromRecord(&record), nil
}
```

Implement `Get`, `Update`, `Delete`, `Patch`, `List`, `Count` following the same pattern. Include conversion helpers `teamNoteToRecord`, `teamNoteFromRecord`, `teamNoteListItemFromRecord`.

For `List`, apply sharable filtering:
```go
func (s *GormTeamNoteStore) List(ctx context.Context, teamID string, offset, limit int, includeNonSharable bool) ([]TeamNoteListItem, int, error) {
	var records []models.TeamNoteRecord
	query := s.db.WithContext(ctx).Where(map[string]any{"team_id": teamID})
	if !includeNonSharable {
		query = query.Where(map[string]any{"sharable": models.DBBool(true)})
	}

	var total int64
	if err := query.Model(&models.TeamNoteRecord{}).Count(&total).Error; err != nil {
		return nil, 0, &RequestError{Status: http.StatusInternalServerError, Code: "store_error", Message: "Failed to count team notes"}
	}

	if err := query.Order("created_at DESC").Offset(offset).Limit(limit).Find(&records).Error; err != nil {
		return nil, 0, &RequestError{Status: http.StatusInternalServerError, Code: "store_error", Message: "Failed to list team notes"}
	}

	items := make([]TeamNoteListItem, len(records))
	for i, r := range records {
		items[i] = teamNoteListItemFromRecord(&r)
	}
	return items, int(total), nil
}
```

For `Patch`, follow existing pattern:
```go
func (s *GormTeamNoteStore) Patch(ctx context.Context, id string, operations []PatchOperation) (*TeamNote, error) {
	existing, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	patched, applyErr := ApplyPatchOperations(*existing, operations)
	if applyErr != nil {
		return nil, &RequestError{Status: http.StatusBadRequest, Code: "invalid_patch", Message: applyErr.Error()}
	}

	// Build update map from patched result
	timmyEnabled := true
	if patched.TimmyEnabled != nil {
		timmyEnabled = *patched.TimmyEnabled
	}
	sharable := true
	if patched.Sharable != nil {
		sharable = *patched.Sharable
	}
	updates := map[string]any{
		"name":          patched.Name,
		"content":       string(models.DBText(patched.Content)),
		"timmy_enabled": models.DBBool(timmyEnabled),
		"sharable":      models.DBBool(sharable),
	}
	if patched.Description != nil {
		updates["description"] = *patched.Description
	} else {
		updates["description"] = nil
	}

	if err := s.db.WithContext(ctx).Model(&models.TeamNoteRecord{}).Where(map[string]any{"id": id}).Updates(updates).Error; err != nil {
		return nil, &RequestError{Status: http.StatusInternalServerError, Code: "store_error", Message: "Failed to patch team note"}
	}

	return s.Get(ctx, id)
}
```

- [ ] **Step 2: Create project note store interface and implementation**

Create `api/project_note_store.go` with `ProjectNoteStoreInterface` and `GormProjectNoteStore`. Same structure as team note store but uses `ProjectNoteRecord`, `ProjectNote`, `ProjectNoteListItem`, and verifies project existence instead of team.

- [ ] **Step 3: Add global store variables and initialization**

In `api/store.go`, add global variables after `GlobalProjectStore`:

```go
var GlobalTeamNoteStore TeamNoteStoreInterface
var GlobalProjectNoteStore ProjectNoteStoreInterface
```

In `InitializeGormStores`, add after the team/project store initialization (around line 143):

```go
	// Team/Project note stores
	GlobalTeamNoteStore = NewGormTeamNoteStore(db)
	GlobalProjectNoteStore = NewGormProjectNoteStore(db)
```

- [ ] **Step 4: Verify store code compiles**

Run: `go build ./api/...`
Expected: May still fail due to missing handler implementations, but store files should have no syntax errors. If there are errors related to generated types not matching, adjust conversion code.

- [ ] **Step 5: Commit store layer**

```bash
git add api/team_note_store.go api/project_note_store.go api/store.go
git commit -m "feat(api): add store layer for team and project notes (#217)"
```

---

## Task 5: Implement Team Note Handlers

**Files:**
- Create: `api/team_note_handlers.go`

The handler methods must match the signatures generated by oapi-codegen in `api/api.go`. Check the generated `ServerInterface` for exact method signatures after Task 3 Step 6.

- [ ] **Step 1: Create team note handlers file**

Create `api/team_note_handlers.go` with all CRUD handlers. Each handler follows the pattern:

1. Extract user context via `getUserUUID(c)`
2. Check team membership via `IsTeamMemberOrAdmin`
3. Determine privilege level via `IsGroupMemberFromContext(c, GroupSecurityReviewers)` and `IsUserAdministrator(c)`
4. Apply `sharable` field enforcement
5. Call store method
6. Return response

```go
package api

import (
	"net/http"

	"github.com/ericfitz/tmi/internal/slogging"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// isPrivilegedUser returns true if the user is an admin or security reviewer
func isPrivilegedUser(c *gin.Context) bool {
	isAdmin, _ := IsUserAdministrator(c)
	if isAdmin {
		return true
	}
	isReviewer, _ := IsGroupMemberFromContext(c, GroupSecurityReviewers)
	return isReviewer
}

// ListTeamNotes returns a paginated list of notes for a team
func (s *Server) ListTeamNotes(c *gin.Context, teamId openapi_types.UUID, params ListTeamNotesParams) {
	logger := slogging.Get().WithContext(c)
	ctx := c.Request.Context()

	userUUID, ok := getUserUUID(c)
	if !ok {
		return
	}

	authorized, err := IsTeamMemberOrAdmin(ctx, teamId.String(), userUUID, c)
	if err != nil {
		logger.Error("ListTeamNotes: authorization check failed: %v", err)
		c.JSON(http.StatusInternalServerError, Error{Error: "server_error", ErrorDescription: "Authorization check failed"})
		return
	}
	if !authorized {
		c.JSON(http.StatusForbidden, Error{Error: "forbidden", ErrorDescription: "Not a member of this team"})
		return
	}

	limit := 20
	offset := 0
	if params.Limit != nil {
		limit = *params.Limit
		if limit < 1 { limit = 1 }
		if limit > 100 { limit = 100 }
	}
	if params.Offset != nil {
		offset = *params.Offset
		if offset < 0 { offset = 0 }
	}

	privileged := isPrivilegedUser(c)
	notes, total, err := GlobalTeamNoteStore.List(ctx, teamId.String(), offset, limit, privileged)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	c.JSON(http.StatusOK, ListTeamNotesResponse{
		Notes:  notes,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	})
}
```

Then implement `CreateTeamNote`, `GetTeamNote`, `UpdateTeamNote`, `PatchTeamNote`, `DeleteTeamNote` following the same authorization pattern. Key logic for `CreateTeamNote`:

```go
func (s *Server) CreateTeamNote(c *gin.Context, teamId openapi_types.UUID) {
	// ... auth checks ...

	var req TeamNoteInput
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, Error{Error: "invalid_input", ErrorDescription: "Invalid request body"})
		return
	}

	privileged := isPrivilegedUser(c)

	// Enforce sharable field restrictions
	if req.Sharable != nil && !privileged {
		c.JSON(http.StatusForbidden, Error{Error: "forbidden", ErrorDescription: "Only admins and security reviewers can set the sharable field"})
		return
	}

	// Apply role-based defaults
	if privileged {
		if req.Sharable == nil {
			f := false
			req.Sharable = &f
		}
	} else {
		t := true
		req.Sharable = &t
	}

	// Sanitize
	req.Name = SanitizePlainText(req.Name)
	req.Content = SanitizeMarkdownContent(req.Content)
	req.Description = SanitizeOptionalString(req.Description)

	note := TeamNote{
		Name:         req.Name,
		Content:      req.Content,
		Description:  req.Description,
		TimmyEnabled: req.TimmyEnabled,
		Sharable:     req.Sharable,
	}

	result, err := GlobalTeamNoteStore.Create(ctx, &note, teamId.String())
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	c.JSON(http.StatusCreated, result)
}
```

For `GetTeamNote` and `DeleteTeamNote`, after fetching the note, check if non-privileged user is accessing a non-sharable note and return 404:

```go
	note, err := GlobalTeamNoteStore.Get(ctx, teamNoteId.String())
	// ... error handling ...

	if !isPrivilegedUser(c) && note.Sharable != nil && !*note.Sharable {
		c.JSON(http.StatusNotFound, Error{Error: "not_found", ErrorDescription: "Team note not found"})
		return
	}
```

For `PatchTeamNote`, validate that non-privileged users cannot patch `/sharable`:

```go
	operations, err := ParsePatchRequest(c)
	// ...
	if !privileged {
		for _, op := range operations {
			if op.Path == "/sharable" {
				c.JSON(http.StatusForbidden, Error{Error: "forbidden", ErrorDescription: "Only admins and security reviewers can modify the sharable field"})
				return
			}
		}
	}
```

- [ ] **Step 2: Verify team note handlers compile**

Run: `go build ./api/...`
Expected: May still fail if project note handlers not yet implemented. Team note handler code should have no syntax errors.

- [ ] **Step 3: Commit**

```bash
git add api/team_note_handlers.go
git commit -m "feat(api): add team note CRUD handlers with sharable access control (#217)"
```

---

## Task 6: Implement Project Note Handlers

**Files:**
- Create: `api/project_note_handlers.go`

- [ ] **Step 1: Create project note handlers file**

Same structure as team note handlers but uses:
- `IsProjectTeamMemberOrAdmin` for authorization
- `GlobalProjectNoteStore` for storage
- `ProjectNote`, `ProjectNoteInput`, `ProjectNoteListItem`, `ListProjectNotesResponse` types
- `projectId` and `projectNoteId` parameters

Implement: `ListProjectNotes`, `CreateProjectNote`, `GetProjectNote`, `UpdateProjectNote`, `PatchProjectNote`, `DeleteProjectNote`.

- [ ] **Step 2: Verify full build succeeds**

Run: `make build-server`
Expected: Clean build — all `ServerInterface` methods now implemented.

- [ ] **Step 3: Run linter**

Run: `make lint`
Expected: No new lint errors (existing ones in `api/api.go` are expected).

- [ ] **Step 4: Commit**

```bash
git add api/project_note_handlers.go
git commit -m "feat(api): add project note CRUD handlers with sharable access control (#217)"
```

---

## Task 7: Unit Tests — Store Layer

**Files:**
- Create: `api/team_note_handlers_test.go` (mock stores + store helper functions will live here alongside handler tests)

- [ ] **Step 1: Create mock stores and test helpers**

In `api/team_note_handlers_test.go`, create mock implementations:

```go
package api

import (
	"context"
	"errors"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type mockTeamNoteStore struct {
	notes     map[string]*TeamNote
	listItems []TeamNoteListItem
	listTotal int
	err       error
	createErr error
	getErr    error
	updateErr error
	deleteErr error
	listErr   error
	patchErr  error
}

func newMockTeamNoteStore() *mockTeamNoteStore {
	return &mockTeamNoteStore{notes: make(map[string]*TeamNote)}
}

func (m *mockTeamNoteStore) Create(_ context.Context, note *TeamNote, _ string) (*TeamNote, error) {
	if m.createErr != nil { return nil, m.createErr }
	if m.err != nil { return nil, m.err }
	if note.Id == nil {
		id := uuid.New()
		note.Id = &id
	}
	now := time.Now().UTC()
	note.CreatedAt = &now
	note.ModifiedAt = &now
	m.notes[note.Id.String()] = note
	return note, nil
}

// ... implement Get, Update, Delete, Patch, List, Count similarly ...
```

Also create `saveTeamNoteStore` and `saveProjectNoteStore` helpers:

```go
func saveTeamNoteStore(t *testing.T, store TeamNoteStoreInterface) {
	t.Helper()
	orig := GlobalTeamNoteStore
	origEmitter := GlobalEventEmitter
	GlobalTeamNoteStore = store
	GlobalEventEmitter = nil
	t.Cleanup(func() {
		GlobalTeamNoteStore = orig
		GlobalEventEmitter = origEmitter
	})
}
```

- [ ] **Step 2: Run tests to verify mock setup compiles**

Run: `make test-unit name=TestListTeamNotes`
Expected: Either passes or fails with "no test functions" (tests not yet written) — no compilation errors.

- [ ] **Step 3: Commit test infrastructure**

```bash
git add api/team_note_handlers_test.go
git commit -m "test(api): add mock stores and test helpers for team/project notes (#217)"
```

---

## Task 8: Unit Tests — Handler Authorization

**Files:**
- Modify: `api/team_note_handlers_test.go`
- Create: `api/project_note_handlers_test.go`

- [ ] **Step 1: Write team note handler tests**

Add tests covering all authorization scenarios:

```go
func TestListTeamNotes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	t.Run("success - regular user sees only sharable notes", func(t *testing.T) {
		store := newMockTeamNoteStore()
		store.listItems = []TeamNoteListItem{{Name: "Public Note"}}
		store.listTotal = 1
		saveTeamNoteStore(t, store)
		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)

		c, w := CreateTestGinContext("GET", "/teams/"+testTeamID+"/notes")
		TestUsers.Owner.SetContext(c)
		teamUUID, _ := uuid.Parse(testTeamID)
		server.ListTeamNotes(c, teamUUID, ListTeamNotesParams{})

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("forbidden - non-member", func(t *testing.T) {
		store := newMockTeamNoteStore()
		saveTeamNoteStore(t, store)
		db := setupTestTeamAuthDB(t)
		// Create team but don't add user as member
		_ = db.Create(&models.TeamRecord{
			ID: testTeamID, Name: "Test", CreatedByInternalUUID: "other-uuid",
		})

		c, w := CreateTestGinContext("GET", "/teams/"+testTeamID+"/notes")
		TestUsers.Owner.SetContext(c)
		teamUUID, _ := uuid.Parse(testTeamID)
		server.ListTeamNotes(c, teamUUID, ListTeamNotesParams{})

		assert.Equal(t, http.StatusForbidden, w.Code)
	})
}

func TestCreateTeamNote(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	t.Run("regular user - 403 when sharable field included", func(t *testing.T) {
		store := newMockTeamNoteStore()
		saveTeamNoteStore(t, store)
		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)

		sharable := false
		body := TeamNoteInput{Name: "Test", Content: "Content", Sharable: &sharable}
		bodyBytes, _ := json.Marshal(body)
		c, w := CreateTestGinContextWithBody("POST", "/teams/"+testTeamID+"/notes", "application/json", bodyBytes)
		TestUsers.Owner.SetContext(c)
		teamUUID, _ := uuid.Parse(testTeamID)
		server.CreateTeamNote(c, teamUUID)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("regular user - success with default sharable=true", func(t *testing.T) {
		// ...
	})

	// Add tests for admin/security reviewer creating with sharable=false default
	// Add tests for admin explicitly setting sharable=true
}

func TestGetTeamNote(t *testing.T) {
	// Test: regular user gets 404 for non-sharable note
	// Test: admin can access non-sharable note
	// Test: regular user can access sharable note
}

func TestPatchTeamNote(t *testing.T) {
	// Test: regular user gets 403 when patching /sharable
	// Test: admin can patch /sharable
}

func TestDeleteTeamNote(t *testing.T) {
	// Test: regular user gets 404 for non-sharable note
	// Test: admin can delete non-sharable note
}
```

- [ ] **Step 2: Run team note tests**

Run: `make test-unit name=TestListTeamNotes && make test-unit name=TestCreateTeamNote && make test-unit name=TestGetTeamNote && make test-unit name=TestPatchTeamNote && make test-unit name=TestDeleteTeamNote`
Expected: All tests pass.

- [ ] **Step 3: Write project note handler tests**

Create `api/project_note_handlers_test.go` with equivalent tests for project notes. Include mock `ProjectNoteStore` and tests for `IsProjectTeamMemberOrAdmin` authorization.

- [ ] **Step 4: Run project note tests**

Run: `make test-unit name=TestListProjectNotes && make test-unit name=TestCreateProjectNote`
Expected: All tests pass.

- [ ] **Step 5: Run full unit test suite**

Run: `make test-unit`
Expected: All existing and new tests pass.

- [ ] **Step 6: Commit**

```bash
git add api/team_note_handlers_test.go api/project_note_handlers_test.go
git commit -m "test(api): add unit tests for team and project note handlers (#217)"
```

---

## Task 9: Final Validation

- [ ] **Step 1: Run lint**

Run: `make lint`
Expected: No new lint errors.

- [ ] **Step 2: Run full build**

Run: `make build-server`
Expected: Clean build.

- [ ] **Step 3: Run full unit test suite**

Run: `make test-unit`
Expected: All tests pass.

- [ ] **Step 4: Run integration tests**

Run: `make test-integration`
Expected: All tests pass (new endpoints may not have integration tests yet, but existing tests should not regress).

- [ ] **Step 5: Clean up backup files**

Remove any `.backup` files created during OpenAPI editing:
```bash
rm -f api-schema/tmi-openapi.json.*.backup
```

- [ ] **Step 6: Final commit if any remaining changes**

```bash
git status
# If there are uncommitted changes:
git add -A && git commit -m "chore: final cleanup for team and project notes (#217)"
```
