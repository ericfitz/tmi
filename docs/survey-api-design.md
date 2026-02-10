# Survey API Design

## Overview

The Survey API enables TMI's security review intake workflow with three user personas:
- **Administrators**: Manage survey templates
- **Software Developers**: Fill out surveys (intake for security reviews)
- **Security Engineers**: Triage responses, create threat models

---

## URL Structure

### Admin Endpoints (`/admin/survey_templates`)

| Method | Path | Description |
|--------|------|-------------|
| GET | `/admin/survey_templates` | List all templates (paginated, all statuses) |
| POST | `/admin/survey_templates` | Create new template |
| GET | `/admin/survey_templates/{template_id}` | Get specific template |
| PUT | `/admin/survey_templates/{template_id}` | Full update |
| PATCH | `/admin/survey_templates/{template_id}` | Partial update (e.g., activate/deactivate) |
| DELETE | `/admin/survey_templates/{template_id}` | Delete template |

### Intake Endpoints (`/intake/...`)

| Method | Path | Description |
|--------|------|-------------|
| GET | `/intake/templates` | List **active** templates only |
| GET | `/intake/templates/{template_id}` | Get template for filling out |
| POST | `/intake/responses` | Create new response (draft) |
| GET | `/intake/responses` | List user's own responses |
| GET | `/intake/responses/{response_id}` | Get specific response |
| PUT | `/intake/responses/{response_id}` | Full update |
| PATCH | `/intake/responses/{response_id}` | Partial update |
| DELETE | `/intake/responses/{response_id}` | Delete draft response |
| POST | `/intake/responses/{response_id}/submit` | Submit for review |

### Triage Endpoints (`/triage/surveys/...`)

| Method | Path | Description |
|--------|------|-------------|
| GET | `/triage/surveys/responses` | All responses with filters (use `?status=submitted` for queue) |
| GET | `/triage/surveys/responses/{response_id}` | Get response details |
| POST | `/triage/surveys/responses/{response_id}/approve` | Mark ready for review |
| POST | `/triage/surveys/responses/{response_id}/return` | Return for revision (with notes) |
| POST | `/triage/surveys/responses/{response_id}/create_threat_model` | Create TM from response |

---

## Resource Schemas

### SurveyTemplate

```json
{
  "id": "uuid",
  "name": "string (required, max 256)",
  "description": "string (max 2048)",
  "version": "string (custom, e.g., '2024-Q1', required, max 64)",
  "status": "active | inactive",
  "questions": [SurveyQuestion],
  "settings": {
    "allow_threat_model_linking": true
  },
  "created_at": "datetime",
  "modified_at": "datetime",
  "created_by": "User"
}
```

### SurveyQuestion (SurveyJS-compatible)

```json
{
  "name": "string (field identifier, required)",
  "type": "text | comment | radiogroup | checkbox | dropdown | boolean | number",
  "title": "string (display label)",
  "description": "string (help text)",
  "is_required": false,
  "input_type": "text | email | date | url | number (for type=text)",
  "choices": [{"value": "string", "text": "string"}],
  "default_value": "any",
  "visible_if": "string (SurveyJS expression)"
}
```

### SurveyResponse

```json
{
  "id": "uuid",
  "template_id": "uuid",
  "template_version": "string (captured at creation)",
  "status": "draft | submitted | needs_revision | ready_for_review | review_created",
  "is_confidential": "boolean (set at creation only, read-only after)",
  "answers": {"question_name": "value"},
  "linked_threat_model_id": "uuid | null",
  "created_threat_model_id": "uuid | null",
  "revision_notes": "string | null",
  "owner": "User",
  "authorization": [Authorization],
  "created_at": "datetime",
  "modified_at": "datetime",
  "submitted_at": "datetime | null",
  "reviewed_at": "datetime | null",
  "reviewed_by": "User | null"
}
```

**`is_confidential` Field Behavior:**
- Set ONLY during initial POST to create the response
- After creation, treated as server-managed read-only (like `created_at`)
- If `false` (default): Security Reviewers group automatically added to authorization
- If `true`: Security Reviewers NOT added; owner is responsible for managing access

### SurveyResponseListItem (for list endpoints)

```json
{
  "id": "uuid",
  "template_id": "uuid",
  "template_name": "string",
  "template_version": "string",
  "status": "string",
  "owner": "User",
  "created_at": "datetime",
  "submitted_at": "datetime | null"
}
```

---

## State Machine

```
    [draft] ──submit──> [submitted] ──approve──> [ready_for_review] ──create_tm──> [review_created]
                             │                          │
                             │ return                   │ return
                             v                          v
                       [needs_revision] ──submit──> [submitted]
```

### State Transitions

| From | Action | To | Allowed By |
|------|--------|----|------------|
| draft | submit | submitted | owner, writer |
| submitted | approve | ready_for_review | Security Reviewers (owner role) |
| submitted | return | needs_revision | Security Reviewers |
| needs_revision | submit | submitted | owner, writer |
| ready_for_review | create_threat_model | review_created | Security Reviewers |
| ready_for_review | return | needs_revision | Security Reviewers |

---

## Authorization Model

### Survey Templates
- **No ACL** - admin-only resources
- All admins have full access via `/admin/` prefix
- All authenticated users can read active templates via `/intake/templates`

### Survey Responses
- **Full ACL** - same model as ThreatModel
- Uses existing `AccessCheckWithGroups()` from `api/auth_utils.go`

### Built-in Group: Security Reviewers

```json
{
  "principal_type": "group",
  "provider": "*",
  "provider_id": "security-reviewers",
  "role": "owner"
}
```

- Auto-added to response authorization at creation if `is_confidential` is `false` (default)
- NOT added if `is_confidential` is `true` - owner must manage access manually

### Permission Matrix

| Role | View | Edit (draft/revision) | Delete | Submit | Triage Actions |
|------|------|----------------------|--------|--------|----------------|
| Owner | Yes | Yes | Yes (draft only) | Yes | No |
| Writer | Yes | Yes | No | Yes | No |
| Reader | Yes | No | No | No | No |
| Security Reviewers | Yes* | No | No | No | Yes |

*Security Reviewers have access unless `is_confidential` is `true`

---

## Key Behaviors

### Template Versioning
- `version` is a custom string set by admin (e.g., "2024-Q1", "v2-pilot")
- Response captures `template_version` at creation
- Responses complete on their original template version
- Deactivating a template does NOT block existing drafts

### Validation Strategy
- **Draft mode**: No validation, partial saves allowed
- **Submit action**: Full validation against template requirements
- Server validates state transitions (returns 409 Conflict for invalid)

### Threat Model Creation
When `POST /triage/surveys/responses/{id}/create_threat_model`:
1. Copy survey answers to TM description/metadata
2. Set TM owner = survey response owner
3. Store `created_threat_model_id` in response
4. Store `source_survey_response_id` in TM metadata
5. Transition response to `review_created`

### Threat Model Linking
- `linked_threat_model_id` for re-reviews of existing systems
- Any valid TM ID accepted (no access check at link time)
- Access verified when TM is actually accessed

---

## Database Schema

```sql
-- Survey Templates
CREATE TABLE survey_templates (
    id VARCHAR(36) PRIMARY KEY,
    name VARCHAR(256) NOT NULL,
    description VARCHAR(2048),
    version VARCHAR(64) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'inactive',
    questions JSONB NOT NULL,
    settings JSONB,
    created_by_internal_uuid VARCHAR(36) NOT NULL REFERENCES users(internal_uuid),
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    modified_at TIMESTAMP NOT NULL DEFAULT NOW(),
    UNIQUE(name, version)
);

-- Survey Responses
CREATE TABLE survey_responses (
    id VARCHAR(36) PRIMARY KEY,
    template_id VARCHAR(36) NOT NULL REFERENCES survey_templates(id),
    template_version VARCHAR(64) NOT NULL,
    status VARCHAR(30) NOT NULL DEFAULT 'draft',
    is_confidential BOOLEAN NOT NULL DEFAULT FALSE,
    answers JSONB,
    linked_threat_model_id VARCHAR(36) REFERENCES threat_models(id),
    created_threat_model_id VARCHAR(36) REFERENCES threat_models(id),
    revision_notes TEXT,
    owner_internal_uuid VARCHAR(36) NOT NULL REFERENCES users(internal_uuid),
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    modified_at TIMESTAMP NOT NULL DEFAULT NOW(),
    submitted_at TIMESTAMP,
    reviewed_at TIMESTAMP,
    reviewed_by_internal_uuid VARCHAR(36) REFERENCES users(internal_uuid)
);

-- Survey Response Access (mirrors threat_model_access)
CREATE TABLE survey_response_access (
    id VARCHAR(36) PRIMARY KEY,
    survey_response_id VARCHAR(36) NOT NULL REFERENCES survey_responses(id) ON DELETE CASCADE,
    subject_type VARCHAR(10) NOT NULL,
    user_internal_uuid VARCHAR(36) REFERENCES users(internal_uuid),
    group_internal_uuid VARCHAR(36) REFERENCES groups(internal_uuid),
    role VARCHAR(20) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_subject CHECK (
        (subject_type = 'user' AND user_internal_uuid IS NOT NULL) OR
        (subject_type = 'group' AND group_internal_uuid IS NOT NULL)
    )
);

CREATE INDEX idx_survey_responses_status ON survey_responses(status);
CREATE INDEX idx_survey_responses_owner ON survey_responses(owner_internal_uuid);
CREATE INDEX idx_survey_responses_template ON survey_responses(template_id);
CREATE INDEX idx_survey_response_access_response ON survey_response_access(survey_response_id);
```

---

## API Response Patterns

### List Response (matches existing TMI pattern)
```json
{
  "items": [],
  "total": 100,
  "limit": 20,
  "offset": 0
}
```

### Error Response
```json
{
  "error": "error_code",
  "error_description": "Human readable message"
}
```

### Common Error Codes
- `invalid_input` (400): Validation failures
- `unauthorized` (401): Authentication required
- `forbidden` (403): Insufficient permissions
- `not_found` (404): Resource not found
- `conflict` (409): Invalid state transition
- `server_error` (500): Internal error

---

## Implementation Files

### New Files to Create
- `api/survey_template_store.go` - Template storage
- `api/survey_template_handlers.go` - Admin template handlers
- `api/survey_response_store.go` - Response storage
- `api/survey_response_handlers.go` - Intake/triage handlers
- `api/survey_middleware.go` - Authorization middleware
- `auth/migrations/NNNN_create_survey_tables.sql` - Database migration

### Files to Modify
- `api-schema/tmi-openapi.json` - Add survey endpoints and schemas
- `api/server.go` - Register new handlers
- `api/pseudogroups.go` - Add Security Reviewers group constant

### Patterns to Reuse
- `api/auth_utils.go` - `AccessCheckWithGroups()`
- `api/threat_model_handlers.go` - Handler patterns
- `api/middleware.go` - `ThreatModelMiddleware` pattern
- `api/asset_store.go` - Store interface pattern
