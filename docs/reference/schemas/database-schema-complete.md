# TMI Database Schema Reference

**Version**: 0.11.0
**Last Updated**: 2025-11-30
**Database**: PostgreSQL 13+

## Overview

The TMI database schema is implemented through three sequential migrations that build a comprehensive data model for collaborative threat modeling. The schema supports OAuth authentication, role-based access control (RBAC), real-time collaboration, webhook integrations, and multi-framework threat modeling.

### Migration Structure

| Migration | Purpose | Key Tables |
|-----------|---------|------------|
| **001_core_infrastructure.up.sql** | Authentication, session management, collaboration | users, refresh_tokens, collaboration_sessions, session_participants |
| **002_business_domain.up.sql** | Business logic, RBAC, webhooks, addons | threat_models, diagrams, threats, assets, groups, threat_model_access, webhook_*, addons |
| **003_administrator_provider_fields.up.sql** | Administrator management with dual foreign keys | administrators (restructured) |

### Key Design Patterns

1. **UUID-based Identifiers**: All tables use UUIDs for primary keys (UUIDv4 by default, UUIDv7 for time-ordered entities)
2. **Provider-based Identity**: Users and groups are scoped by OAuth provider to support multi-provider authentication
3. **Dual Foreign Key Pattern**: Authorization tables (threat_model_access, administrators) support both user and group subjects with XOR constraints
4. **Timestamp Tracking**: All business entities track created_at and modified_at with automatic triggers
5. **Cascade Deletion**: Proper CASCADE and RESTRICT constraints maintain referential integrity
6. **Performance Optimization**: Comprehensive indexing strategy including partial, composite, and INCLUDE indexes

---

## Core Infrastructure Tables

### users

User accounts from OAuth providers. Each provider account is treated as a separate user identity.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| internal_uuid | UUID | PRIMARY KEY, DEFAULT uuid_generate_v4() | Internal user identifier |
| provider | TEXT | NOT NULL | OAuth provider: "test", "google", "github", "microsoft", "azure" |
| provider_user_id | TEXT | NOT NULL | Provider's user ID (from JWT sub claim) |
| email | TEXT | NOT NULL | User email address |
| name | TEXT | NOT NULL | Display name for UI presentation |
| email_verified | BOOLEAN | DEFAULT FALSE | Email verification status |
| access_token | TEXT | NULL | OAuth access token |
| refresh_token | TEXT | NULL | OAuth refresh token |
| token_expiry | TIMESTAMPTZ | NULL | Token expiration time |
| created_at | TIMESTAMPTZ | NOT NULL, DEFAULT CURRENT_TIMESTAMP | Account creation timestamp |
| modified_at | TIMESTAMPTZ | NOT NULL, DEFAULT CURRENT_TIMESTAMP | Last modification timestamp |
| last_login | TIMESTAMPTZ | NULL | Last successful login timestamp |

**Unique Constraints:**
- `UNIQUE(provider, provider_user_id)` - One user per provider account

**Indexes:**
- `idx_users_provider_lookup` - (provider, provider_user_id) for authentication lookups
- `idx_users_email` - Email-based searches
- `idx_users_last_login` - Last login sorting
- `idx_users_provider` - Provider filtering

**Usage Notes:**
- Users are provider-scoped: alice@google and alice@github are different users
- Tokens are stored for OAuth refresh flows
- last_login is updated on successful authentication

---

### refresh_tokens

Additional refresh token tracking for OAuth flows. Complements the refresh_token field in users table.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| id | UUID | PRIMARY KEY, DEFAULT uuid_generate_v4() | Token identifier |
| user_internal_uuid | UUID | NOT NULL, FOREIGN KEY → users(internal_uuid) ON DELETE CASCADE | User owning this token |
| token | TEXT | UNIQUE, NOT NULL | Refresh token value |
| expires_at | TIMESTAMPTZ | NOT NULL | Token expiration timestamp |
| created_at | TIMESTAMPTZ | NOT NULL, DEFAULT CURRENT_TIMESTAMP | Token creation timestamp |

**Indexes:**
- `idx_refresh_tokens_user_internal_uuid` - User token lookups
- `idx_refresh_tokens_token` - Token validation

**Usage Notes:**
- Supports multiple refresh tokens per user
- Tokens cascade delete when user is deleted
- Check expires_at before using token

---

### collaboration_sessions

WebSocket-based real-time collaboration sessions for diagram editing.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| id | UUID | PRIMARY KEY, DEFAULT uuid_generate_v4() | Session identifier |
| threat_model_id | UUID | NOT NULL, FOREIGN KEY → threat_models(id) ON DELETE CASCADE | Parent threat model |
| diagram_id | UUID | NOT NULL, FOREIGN KEY → diagrams(id) ON DELETE CASCADE | Diagram being edited |
| websocket_url | TEXT | NOT NULL, CHECK (LENGTH(TRIM(websocket_url)) > 0) | WebSocket connection URL |
| created_at | TIMESTAMPTZ | NOT NULL, DEFAULT CURRENT_TIMESTAMP | Session start time |
| expires_at | TIMESTAMPTZ | NULL, CHECK (expires_at IS NULL OR expires_at > created_at) | Session expiration time |

**Indexes:**
- `idx_collaboration_sessions_threat_model_id` - Threat model session lookups
- `idx_collaboration_sessions_diagram_id` - Diagram session lookups
- `idx_collaboration_sessions_expires_at` - Expiration cleanup queries

**Usage Notes:**
- One active session per diagram (enforced in application layer)
- Session expiration is configurable (default 300s, minimum 15s)
- Session lifecycle: Active → Terminating → Terminated
- Cascades delete when threat model or diagram is deleted

---

### session_participants

Tracks users participating in collaboration sessions.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| id | UUID | PRIMARY KEY, DEFAULT uuid_generate_v4() | Participant record identifier |
| session_id | UUID | NOT NULL, FOREIGN KEY → collaboration_sessions(id) ON DELETE CASCADE | Collaboration session |
| user_internal_uuid | UUID | NOT NULL, FOREIGN KEY → users(internal_uuid) ON DELETE CASCADE | Participating user |
| joined_at | TIMESTAMPTZ | NOT NULL, DEFAULT CURRENT_TIMESTAMP | Join timestamp |
| left_at | TIMESTAMPTZ | NULL | Leave timestamp (NULL = still active) |

**Indexes:**
- `idx_session_participants_session_id` - Session participant lists
- `idx_session_participants_user_internal_uuid` - User's active sessions
- `idx_session_participants_joined_at` - Chronological ordering

**Unique Constraints:**
- `idx_session_participants_active_unique` - UNIQUE(session_id, user_internal_uuid) WHERE left_at IS NULL
  - Prevents duplicate active participants

**Usage Notes:**
- left_at = NULL indicates active participant
- Cascades delete when session or user is deleted
- Host-based control: Only session host can manage participants
- Removed participants tracked in session-specific deny list (application layer)

---

## Business Domain Tables

### threat_models

Top-level threat modeling projects with framework selection and ownership.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| id | UUID | PRIMARY KEY, DEFAULT uuid_generate_v4() | Threat model identifier |
| owner_internal_uuid | UUID | NOT NULL, FOREIGN KEY → users(internal_uuid) ON DELETE RESTRICT | Owner user |
| name | TEXT | NOT NULL | Threat model name |
| description | TEXT | NULL | Optional description |
| created_by_internal_uuid | UUID | NOT NULL, FOREIGN KEY → users(internal_uuid) ON DELETE RESTRICT | Creator user |
| threat_model_framework | TEXT | NOT NULL, DEFAULT 'STRIDE', CHECK (threat_model_framework IN ('CIA', 'STRIDE', 'LINDDUN', 'DIE', 'PLOT4ai')) | Threat modeling framework |
| issue_uri | TEXT | NULL | External issue tracker URI |
| status | TEXT | NULL, CHECK (status IS NULL OR LENGTH(status) <= 128) | Custom status value |
| status_updated | TIMESTAMPTZ | NULL | Last status change timestamp |
| created_at | TIMESTAMPTZ | NOT NULL, DEFAULT CURRENT_TIMESTAMP | Creation timestamp |
| modified_at | TIMESTAMPTZ | NOT NULL, DEFAULT CURRENT_TIMESTAMP | Last modification timestamp |

**Indexes:**
- `idx_threat_models_owner_internal_uuid` - Owner's threat models
- `idx_threat_models_framework` - Framework filtering
- `idx_threat_models_created_by` - Creator lookups
- `idx_threat_models_owner_created_at` - Owner models by creation date
- `idx_threat_models_status` - Status filtering (partial index WHERE status IS NOT NULL)
- `idx_threat_models_status_updated` - Recent status changes

**Triggers:**
- `update_threat_models_modified_at` - Auto-update modified_at on UPDATE

**Usage Notes:**
- Owner cannot be deleted while owning threat models (ON DELETE RESTRICT)
- Supports 5 threat modeling frameworks: CIA, STRIDE, LINDDUN, DIE, PLOT4ai
- status field supports custom workflow states
- issue_uri links to external issue tracking systems

---

### diagrams

Data Flow Diagrams (DFD) associated with threat models.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| id | UUID | PRIMARY KEY, DEFAULT uuid_generate_v4() | Diagram identifier |
| threat_model_id | UUID | NOT NULL, FOREIGN KEY → threat_models(id) ON DELETE CASCADE | Parent threat model |
| name | TEXT | NOT NULL | Diagram name |
| type | TEXT | NULL, CHECK (type IN ('DFD-1.0.0')) | Diagram format version |
| content | TEXT | NULL | Raw diagram content |
| cells | JSONB | NULL | JointJS/RappID cell definitions |
| svg_image | TEXT | NULL | Rendered SVG representation |
| image_update_vector | BIGINT | NULL | SVG update version counter |
| update_vector | BIGINT | NOT NULL, DEFAULT 0 | Diagram update version counter |
| created_at | TIMESTAMPTZ | NOT NULL, DEFAULT CURRENT_TIMESTAMP | Creation timestamp |
| modified_at | TIMESTAMPTZ | NOT NULL, DEFAULT CURRENT_TIMESTAMP | Last modification timestamp |

**Indexes:**
- `idx_diagrams_threat_model_id` - Threat model's diagrams
- `idx_diagrams_type` - Type filtering
- `idx_diagrams_cells` - GIN index for JSONB cell queries
- `idx_diagrams_threat_model_id_type` - Composite lookup

**Triggers:**
- `update_diagrams_modified_at` - Auto-update modified_at on UPDATE

**Usage Notes:**
- Cascades delete when threat model is deleted
- cells JSONB field enables efficient querying of diagram elements
- update_vector supports optimistic locking in WebSocket collaboration
- Only diagram type is DFD-1.0.0 (Data Flow Diagram)

---

### threats

Individual threats identified in threat models, linked to diagrams, cells, and assets.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| id | UUID | PRIMARY KEY | Threat identifier (UUIDv7 generated by application) |
| threat_model_id | UUID | NOT NULL, FOREIGN KEY → threat_models(id) ON DELETE CASCADE | Parent threat model |
| diagram_id | UUID | NULL, FOREIGN KEY → diagrams(id) ON DELETE SET NULL | Associated diagram |
| cell_id | UUID | NULL | Diagram cell reference |
| asset_id | UUID | NULL, FOREIGN KEY → assets(id) ON DELETE SET NULL | Associated asset |
| name | TEXT | NOT NULL | Threat name |
| description | TEXT | NULL | Threat description |
| severity | TEXT | NULL | Severity level (flexible: numeric, English, localized, custom) |
| likelihood | TEXT | NULL | Likelihood assessment |
| risk_level | TEXT | NULL | Overall risk level |
| score | DECIMAL(3,1) | NULL, CHECK (score >= 0.0 AND score <= 10.0) | Numeric risk score (0.0-10.0) |
| priority | TEXT | DEFAULT 'Medium' | Priority classification |
| mitigated | BOOLEAN | DEFAULT FALSE | Mitigation status |
| status | TEXT | DEFAULT 'Active' | Threat status |
| threat_type | TEXT | DEFAULT 'Unspecified' | Threat category |
| mitigation | TEXT | NULL | Mitigation strategy description |
| issue_uri | TEXT | NULL | External issue tracker URI |
| created_at | TIMESTAMPTZ | NOT NULL, DEFAULT CURRENT_TIMESTAMP | Creation timestamp |
| modified_at | TIMESTAMPTZ | NOT NULL, DEFAULT CURRENT_TIMESTAMP | Last modification timestamp |

**Indexes:**
- `idx_threats_threat_model_id` - Threat model's threats
- `idx_threats_severity` - Severity filtering (partial index WHERE severity IS NOT NULL)
- `idx_threats_risk_level` - Risk level filtering (partial index WHERE risk_level IS NOT NULL)
- `idx_threats_diagram_id` - Diagram threat lookups
- `idx_threats_cell_id` - Cell threat associations
- `idx_threats_asset_id` - Asset threat lookups
- `idx_threats_priority` - Priority sorting
- `idx_threats_mitigated` - Mitigation status filtering
- `idx_threats_status` - Status filtering
- `idx_threats_threat_type` - Type categorization
- `idx_threats_score` - Score-based sorting
- `idx_threats_name` - Name searches
- `idx_threats_modified_at` - Recent changes
- `idx_threats_threat_model_created_at` - (threat_model_id, created_at DESC)
- `idx_threats_threat_model_modified_at` - (threat_model_id, modified_at DESC)
- `idx_threats_owner_via_threat_model` - (threat_model_id) INCLUDE (id, name, created_at, modified_at)

**Triggers:**
- `update_threats_modified_at` - Auto-update modified_at on UPDATE

**Usage Notes:**
- UUIDv7 IDs provide time-ordered identifiers
- severity field accepts Unicode, alphanumeric, hyphens, underscores, parentheses, periods (max 50 chars)
- Supports flexible scoring: numeric (0-5), standard (Unknown/None/Low/Medium/High/Critical), custom, localized
- diagram_id and asset_id set to NULL on deletion (ON DELETE SET NULL)
- Inherits authorization from parent threat_model

---

### assets

Assets being protected in threat models (data, hardware, software, etc.).

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| id | UUID | PRIMARY KEY, DEFAULT uuid_generate_v4() | Asset identifier |
| threat_model_id | UUID | NOT NULL, FOREIGN KEY → threat_models(id) ON DELETE CASCADE | Parent threat model |
| name | TEXT | NOT NULL, CHECK (LENGTH(TRIM(name)) > 0) | Asset name |
| description | TEXT | NULL | Asset description |
| type | TEXT | NOT NULL, CHECK (type IN ('data', 'hardware', 'software', 'infrastructure', 'service', 'personnel')) | Asset category |
| criticality | TEXT | NULL | Business criticality assessment |
| classification | TEXT[] | NULL | Data classification tags |
| sensitivity | TEXT | NULL | Sensitivity level |
| created_at | TIMESTAMPTZ | NOT NULL, DEFAULT CURRENT_TIMESTAMP | Creation timestamp |
| modified_at | TIMESTAMPTZ | NOT NULL, DEFAULT CURRENT_TIMESTAMP | Last modification timestamp |

**Indexes:**
- `idx_assets_threat_model_id` - Threat model's assets
- `idx_assets_name` - Name searches
- `idx_assets_type` - Type filtering
- `idx_assets_created_at` - Creation ordering
- `idx_assets_modified_at` - Modification ordering
- `idx_assets_threat_model_created_at` - (threat_model_id, created_at DESC)
- `idx_assets_threat_model_modified_at` - (threat_model_id, modified_at DESC)
- `idx_assets_owner_via_threat_model` - (threat_model_id) INCLUDE (id, name, type, created_at, modified_at)

**Triggers:**
- `update_assets_modified_at` - Auto-update modified_at on UPDATE

**Usage Notes:**
- Six asset types: data, hardware, software, infrastructure, service, personnel
- classification array supports multiple data classification tags
- Cascades delete when threat model is deleted
- Must be created before threats that reference it (FK dependency)

---

### groups

User groups for authorization, scoped by OAuth provider.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| internal_uuid | UUID | PRIMARY KEY, DEFAULT uuid_generate_v4() | Group identifier |
| provider | TEXT | NOT NULL | OAuth provider or "*" for provider-independent |
| group_name | TEXT | NOT NULL | Group identifier (provider_id in API) |
| name | TEXT | NULL | Display name for UI presentation |
| description | TEXT | NULL | Group description |
| first_used | TIMESTAMPTZ | NOT NULL, DEFAULT CURRENT_TIMESTAMP | First usage timestamp |
| last_used | TIMESTAMPTZ | NOT NULL, DEFAULT CURRENT_TIMESTAMP | Last usage timestamp |
| usage_count | INTEGER | DEFAULT 1 | Usage counter |

**Unique Constraints:**
- `UNIQUE(provider, group_name)` - One group per (provider, group_name) combination

**Indexes:**
- `idx_groups_provider` - Provider filtering
- `idx_groups_group_name` - Name searches
- `idx_groups_last_used` - Activity tracking

**Special Groups:**
- **everyone** (UUID: 00000000-0000-0000-0000-000000000000):
  - Provider: "*"
  - Grants access to all authenticated users
  - Protected by `prevent_everyone_deletion` trigger

**Triggers:**
- `prevent_everyone_deletion` - Prevents deletion of "everyone" pseudo-group

**Usage Notes:**
- Groups are provider-scoped (except "*" for cross-provider groups)
- "everyone" pseudo-group is system-reserved and cannot be deleted
- Usage tracking supports group lifecycle management

---

### threat_model_access

Role-based access control (RBAC) for threat models. Supports both user and group subjects.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| id | UUID | PRIMARY KEY, DEFAULT uuid_generate_v4() | Access record identifier |
| threat_model_id | UUID | NOT NULL, FOREIGN KEY → threat_models(id) ON DELETE CASCADE | Threat model being accessed |
| user_internal_uuid | UUID | NULL, FOREIGN KEY → users(internal_uuid) ON DELETE CASCADE | User subject (NULL if group) |
| group_internal_uuid | UUID | NULL, FOREIGN KEY → groups(internal_uuid) ON DELETE CASCADE | Group subject (NULL if user) |
| subject_type | TEXT | NOT NULL, CHECK (subject_type IN ('user', 'group')) | Subject discriminator |
| role | TEXT | NOT NULL, CHECK (role IN ('owner', 'writer', 'reader')) | Permission level |
| granted_by_internal_uuid | UUID | NULL, FOREIGN KEY → users(internal_uuid) ON DELETE SET NULL | User who granted access |
| created_at | TIMESTAMPTZ | NOT NULL, DEFAULT CURRENT_TIMESTAMP | Grant timestamp |
| modified_at | TIMESTAMPTZ | NOT NULL, DEFAULT CURRENT_TIMESTAMP | Last modification timestamp |

**Constraints:**
- `exactly_one_subject` - XOR constraint ensures exactly one of user_internal_uuid or group_internal_uuid is populated:
  - subject_type = 'user': user_internal_uuid NOT NULL, group_internal_uuid NULL
  - subject_type = 'group': group_internal_uuid NOT NULL, user_internal_uuid NULL

**Unique Constraints:**
- `UNIQUE NULLS NOT DISTINCT (threat_model_id, user_internal_uuid, subject_type)` - One role per user
- `UNIQUE NULLS NOT DISTINCT (threat_model_id, group_internal_uuid, subject_type)` - One role per group

**Indexes:**
- `idx_threat_model_access_threat_model_id` - Threat model ACL lookups
- `idx_threat_model_access_user_internal_uuid` - User's access grants (partial WHERE user_internal_uuid IS NOT NULL)
- `idx_threat_model_access_group_internal_uuid` - Group's access grants (partial WHERE group_internal_uuid IS NOT NULL)
- `idx_threat_model_access_subject_type` - Subject type filtering
- `idx_threat_model_access_role` - Role filtering
- `idx_threat_model_access_performance` - (threat_model_id, subject_type, user_internal_uuid, group_internal_uuid)

**Triggers:**
- `update_threat_model_access_modified_at` - Auto-update modified_at on UPDATE

**Roles:**
- **owner**: Full control (read, write, delete, manage access)
- **writer**: Read and write access
- **reader**: Read-only access

**Usage Notes:**
- Dual foreign key pattern enforces type safety
- Child resources (diagrams, threats, etc.) inherit authorization from threat_model
- Group access grants apply to all group members
- Cascades delete when threat model, user, or group is deleted

---

### documents

External document references associated with threat models.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| id | UUID | PRIMARY KEY, DEFAULT uuid_generate_v4() | Document identifier |
| threat_model_id | UUID | NOT NULL, FOREIGN KEY → threat_models(id) ON DELETE CASCADE | Parent threat model |
| name | TEXT | NOT NULL, CHECK (LENGTH(TRIM(name)) > 0) | Document name |
| uri | TEXT | NOT NULL, CHECK (LENGTH(TRIM(uri)) > 0) | Document URI/URL |
| description | TEXT | NULL | Document description |
| created_at | TIMESTAMPTZ | NOT NULL, DEFAULT CURRENT_TIMESTAMP | Creation timestamp |
| modified_at | TIMESTAMPTZ | NOT NULL, DEFAULT CURRENT_TIMESTAMP | Last modification timestamp |

**Indexes:**
- `idx_documents_threat_model_id` - Threat model's documents
- `idx_documents_name` - Name searches
- `idx_documents_created_at` - Creation ordering
- `idx_documents_modified_at` - Modification ordering
- `idx_documents_threat_model_created_at` - (threat_model_id, created_at DESC)
- `idx_documents_threat_model_modified_at` - (threat_model_id, modified_at DESC)
- `idx_documents_owner_via_threat_model` - (threat_model_id) INCLUDE (id, name, uri, created_at, modified_at)

**Triggers:**
- `update_documents_modified_at` - Auto-update modified_at on UPDATE

**Usage Notes:**
- Cascades delete when threat model is deleted
- uri can be HTTP/HTTPS URL, file path, or other URI scheme
- Inherits authorization from parent threat_model

---

### notes

Text notes associated with threat models.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| id | UUID | PRIMARY KEY, DEFAULT uuid_generate_v4() | Note identifier |
| threat_model_id | UUID | NOT NULL, FOREIGN KEY → threat_models(id) ON DELETE CASCADE | Parent threat model |
| name | TEXT | NOT NULL, CHECK (LENGTH(TRIM(name)) > 0) | Note title |
| content | TEXT | NOT NULL, CHECK (LENGTH(TRIM(content)) > 0) | Note content |
| description | TEXT | NULL | Note description/summary |
| created_at | TIMESTAMPTZ | NOT NULL, DEFAULT CURRENT_TIMESTAMP | Creation timestamp |
| modified_at | TIMESTAMPTZ | NOT NULL, DEFAULT CURRENT_TIMESTAMP | Last modification timestamp |

**Indexes:**
- `idx_notes_threat_model_id` - Threat model's notes
- `idx_notes_name` - Name searches
- `idx_notes_created_at` - Creation ordering
- `idx_notes_modified_at` - Modification ordering
- `idx_notes_threat_model_created_at` - (threat_model_id, created_at DESC)
- `idx_notes_threat_model_modified_at` - (threat_model_id, modified_at DESC)
- `idx_notes_owner_via_threat_model` - (threat_model_id) INCLUDE (id, name, created_at, modified_at)

**Triggers:**
- `update_notes_modified_at` - Auto-update modified_at on UPDATE

**Usage Notes:**
- Cascades delete when threat model is deleted
- content field must be non-empty (trimmed)
- Inherits authorization from parent threat_model

---

### repositories

Source code repository references associated with threat models.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| id | UUID | PRIMARY KEY, DEFAULT uuid_generate_v4() | Repository identifier |
| threat_model_id | UUID | NOT NULL, FOREIGN KEY → threat_models(id) ON DELETE CASCADE | Parent threat model |
| name | TEXT | NULL | Repository name |
| uri | TEXT | NOT NULL, CHECK (LENGTH(TRIM(uri)) > 0) | Repository URI/URL |
| description | TEXT | NULL | Repository description |
| type | TEXT | NULL, CHECK (type IN ('git', 'svn', 'mercurial', 'other')) | Version control system type |
| parameters | JSONB | NULL | Additional repository parameters |
| created_at | TIMESTAMPTZ | NOT NULL, DEFAULT CURRENT_TIMESTAMP | Creation timestamp |
| modified_at | TIMESTAMPTZ | NOT NULL, DEFAULT CURRENT_TIMESTAMP | Last modification timestamp |

**Indexes:**
- `idx_repositories_threat_model_id` - Threat model's repositories
- `idx_repositories_name` - Name searches
- `idx_repositories_type` - Type filtering
- `idx_repositories_created_at` - Creation ordering
- `idx_repositories_modified_at` - Modification ordering
- `idx_repositories_threat_model_created_at` - (threat_model_id, created_at DESC)
- `idx_repositories_threat_model_modified_at` - (threat_model_id, modified_at DESC)
- `idx_repositories_owner_via_threat_model` - (threat_model_id) INCLUDE (id, name, uri, type, created_at, modified_at)

**Triggers:**
- `update_repositories_modified_at` - Auto-update modified_at on UPDATE

**Usage Notes:**
- Cascades delete when threat model is deleted
- Supports git, svn, mercurial, or custom VCS types
- parameters JSONB field for VCS-specific configuration
- Inherits authorization from parent threat_model

---

### metadata

Flexible key-value metadata for all entity types.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| id | UUID | PRIMARY KEY | Metadata identifier (UUIDv7 generated by application) |
| entity_type | TEXT | NOT NULL, CHECK (entity_type IN ('threat_model', 'threat', 'diagram', 'document', 'repository', 'cell', 'note', 'asset')) | Entity type discriminator |
| entity_id | UUID | NOT NULL | Entity identifier |
| key | TEXT | NOT NULL, CHECK (LENGTH(TRIM(key)) > 0 AND LENGTH(key) <= 128 AND key ~ '^[a-zA-Z0-9_-]+$') | Metadata key (alphanumeric + underscore/hyphen) |
| value | TEXT | NOT NULL, CHECK (LENGTH(TRIM(value)) > 0 AND LENGTH(value) <= 65535) | Metadata value |
| created_at | TIMESTAMPTZ | NOT NULL, DEFAULT CURRENT_TIMESTAMP | Creation timestamp |
| modified_at | TIMESTAMPTZ | NOT NULL, DEFAULT CURRENT_TIMESTAMP | Last modification timestamp |

**Unique Constraints:**
- `idx_metadata_unique_key_per_entity` - UNIQUE(entity_type, entity_id, key) - One value per key per entity

**Indexes:**
- `idx_metadata_entity_type_id` - (entity_type, entity_id) lookups
- `idx_metadata_key` - Key searches
- `idx_metadata_entity_id` - Entity metadata lookups
- `idx_metadata_key_value` - (key, value) searches
- `idx_metadata_entity_key_exists` - (entity_type, entity_id, key) WHERE value IS NOT NULL
- `idx_metadata_created_at` - Creation ordering
- `idx_metadata_modified_at` - Modification ordering
- `idx_metadata_entity_type_created_at` - (entity_type, created_at DESC)
- `idx_metadata_entity_type_modified_at` - (entity_type, modified_at DESC)

**Partial Indexes by Entity Type:**
- `idx_metadata_threats` - (entity_id, key, value) WHERE entity_type = 'threat'
- `idx_metadata_documents` - (entity_id, key, value) WHERE entity_type = 'document'
- `idx_metadata_notes` - (entity_id, key, value) WHERE entity_type = 'note'
- `idx_metadata_repositories` - (entity_id, key, value) WHERE entity_type = 'repository'
- `idx_metadata_diagrams` - (entity_id, key, value) WHERE entity_type = 'diagram'
- `idx_metadata_threat_models` - (entity_id, key, value) WHERE entity_type = 'threat_model'
- `idx_metadata_assets` - (entity_id, key, value) WHERE entity_type = 'asset'

**Triggers:**
- `update_metadata_modified_at` - Auto-update modified_at on UPDATE

**Usage Notes:**
- UUIDv7 IDs provide time-ordered identifiers
- Key format: alphanumeric, underscore, hyphen only (regex: ^[a-zA-Z0-9_-]+$)
- Value max length: 65535 characters
- Supports 8 entity types: threat_model, threat, diagram, document, repository, cell, note, asset
- No foreign key constraints (entity_id is not validated)

---

## Webhook Tables

### webhook_subscriptions

Webhook endpoint subscriptions for event notifications.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| id | UUID | PRIMARY KEY, DEFAULT uuid_generate_v4() | Subscription identifier |
| owner_internal_uuid | UUID | NOT NULL, FOREIGN KEY → users(internal_uuid) ON DELETE CASCADE | Subscription owner |
| threat_model_id | UUID | NULL, FOREIGN KEY → threat_models(id) ON DELETE CASCADE | Scoped threat model (NULL = global) |
| name | TEXT | NOT NULL | Subscription name |
| url | TEXT | NOT NULL | Webhook endpoint URL |
| events | TEXT[] | NOT NULL | Subscribed event types |
| secret | TEXT | NULL | HMAC signing secret |
| status | TEXT | NOT NULL, DEFAULT 'pending_verification', CHECK (status IN ('pending_verification', 'active', 'pending_delete')) | Subscription status |
| challenge | TEXT | NULL | Verification challenge code |
| challenges_sent | INT | NOT NULL, DEFAULT 0 | Verification attempt counter |
| created_at | TIMESTAMPTZ | NOT NULL, DEFAULT CURRENT_TIMESTAMP | Creation timestamp |
| modified_at | TIMESTAMPTZ | NOT NULL, DEFAULT CURRENT_TIMESTAMP | Last modification timestamp |
| last_successful_use | TIMESTAMPTZ | NULL | Last successful delivery timestamp |
| publication_failures | INT | NOT NULL, DEFAULT 0 | Failed delivery counter |

**Indexes:**
- `idx_webhook_subscriptions_owner` - Owner's subscriptions
- `idx_webhook_subscriptions_threat_model` - Threat model subscriptions (partial WHERE threat_model_id IS NOT NULL)
- `idx_webhook_subscriptions_status` - Status filtering
- `idx_webhook_subscriptions_pending_verification` - (status, challenges_sent, created_at) WHERE status = 'pending_verification'
- `idx_webhook_subscriptions_active` - Active subscriptions (partial WHERE status = 'active')

**Triggers:**
- `update_webhook_subscriptions_modified_at` - Auto-update modified_at on UPDATE
- `webhook_subscription_change_notify` - pg_notify('webhook_subscription_change') for worker wake-up

**Status Values:**
- **pending_verification**: Awaiting challenge verification
- **active**: Verified and receiving events
- **pending_delete**: Marked for deletion

**Usage Notes:**
- Cascades delete when owner or threat_model is deleted
- threat_model_id = NULL creates global subscriptions (all events for owner)
- secret used for HMAC-SHA256 signature verification
- Notification trigger wakes worker processes on INSERT/UPDATE/DELETE

---

### webhook_deliveries

Delivery attempts and status tracking for webhook events.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| id | UUID | PRIMARY KEY | Delivery identifier (UUIDv7 generated by application) |
| subscription_id | UUID | NOT NULL, FOREIGN KEY → webhook_subscriptions(id) ON DELETE CASCADE | Parent subscription |
| event_type | TEXT | NOT NULL | Event type being delivered |
| payload | JSONB | NOT NULL | Event payload |
| status | TEXT | NOT NULL, DEFAULT 'pending', CHECK (status IN ('pending', 'delivered', 'failed')) | Delivery status |
| attempts | INT | NOT NULL, DEFAULT 0 | Delivery attempt counter |
| next_retry_at | TIMESTAMPTZ | NULL | Next retry timestamp |
| last_error | TEXT | NULL | Last delivery error message |
| created_at | TIMESTAMPTZ | NOT NULL, DEFAULT CURRENT_TIMESTAMP | Creation timestamp |
| delivered_at | TIMESTAMPTZ | NULL | Successful delivery timestamp |

**Indexes:**
- `idx_webhook_deliveries_subscription` - Subscription's deliveries
- `idx_webhook_deliveries_status_retry` - (status, next_retry_at) WHERE status = 'pending'
- `idx_webhook_deliveries_created` - Creation ordering

**Status Values:**
- **pending**: Awaiting delivery
- **delivered**: Successfully delivered
- **failed**: Delivery failed after retries

**Usage Notes:**
- UUIDv7 IDs provide time-ordered identifiers
- Cascades delete when subscription is deleted
- Supports retry logic with exponential backoff (next_retry_at)
- payload JSONB enables efficient event querying

---

### webhook_quotas

Per-owner webhook usage quotas and rate limits.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| owner_internal_uuid | UUID | PRIMARY KEY, FOREIGN KEY → users(internal_uuid) ON DELETE CASCADE | Quota owner |
| max_subscriptions | INT | NOT NULL, DEFAULT 10 | Maximum subscription count |
| max_events_per_minute | INT | NOT NULL, DEFAULT 12 | Event publication rate limit |
| max_subscription_requests_per_minute | INT | NOT NULL, DEFAULT 10 | Subscription API rate limit |
| max_subscription_requests_per_day | INT | NOT NULL, DEFAULT 20 | Daily subscription API limit |
| created_at | TIMESTAMPTZ | NOT NULL, DEFAULT CURRENT_TIMESTAMP | Creation timestamp |
| modified_at | TIMESTAMPTZ | NOT NULL, DEFAULT CURRENT_TIMESTAMP | Last modification timestamp |

**Triggers:**
- `update_webhook_quotas_modified_at` - Auto-update modified_at on UPDATE

**Default Quotas:**
- Subscriptions: 10
- Events per minute: 12
- Subscription requests per minute: 10
- Subscription requests per day: 20

**Usage Notes:**
- Cascades delete when user is deleted
- Quotas enforced at application layer
- Custom quotas per user for tiered access

---

### webhook_url_deny_list

SSRF prevention: blocked URL patterns for webhook endpoints.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| id | UUID | PRIMARY KEY, DEFAULT uuid_generate_v4() | Deny list entry identifier |
| pattern | TEXT | NOT NULL | URL pattern to block |
| pattern_type | TEXT | NOT NULL, CHECK (pattern_type IN ('glob', 'regex')) | Pattern matching type |
| description | TEXT | NULL | Pattern description |
| created_at | TIMESTAMPTZ | NOT NULL, DEFAULT CURRENT_TIMESTAMP | Creation timestamp |

**Indexes:**
- `idx_webhook_url_deny_list_pattern_type` - Pattern type filtering

**Seeded Patterns:**
- **Localhost**: localhost, 127.*, ::1
- **Private IPv4 (RFC 1918)**: 10.*, 172.16-31.*, 192.168.*
- **Link-local**: 169.254.*, fe80:*
- **Private IPv6**: fc00:*, fd00:*
- **Cloud metadata**: 169.254.169.254, fd00:ec2::254, metadata.google.internal, 169.254.169.123
- **Kubernetes**: kubernetes.default.svc, 10.96.0.*
- **Docker**: 172.17.0.1
- **Broadcast**: 255.255.255.255, 0.0.0.0

**Usage Notes:**
- Prevents webhooks targeting internal infrastructure
- Supports glob and regex pattern types
- Seeded on migration 002 with common SSRF targets
- Checked before webhook verification and delivery

---

## Addon Tables

### addons

Third-party integrations via webhooks with UI metadata.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| id | UUID | PRIMARY KEY, DEFAULT uuid_generate_v4() | Addon identifier |
| created_at | TIMESTAMPTZ | NOT NULL, DEFAULT CURRENT_TIMESTAMP | Creation timestamp |
| name | TEXT | NOT NULL | Addon name |
| webhook_id | UUID | NOT NULL, FOREIGN KEY → webhook_subscriptions(id) ON DELETE CASCADE | Associated webhook |
| description | TEXT | NULL | Addon description |
| icon | TEXT | NULL | Addon icon URL/data |
| objects | TEXT[] | NULL | Object types handled by addon |
| threat_model_id | UUID | NULL, FOREIGN KEY → threat_models(id) ON DELETE CASCADE | Scoped threat model (NULL = global) |

**Indexes:**
- `idx_addons_webhook` - Webhook addon lookups
- `idx_addons_threat_model` - Threat model addons (partial WHERE threat_model_id IS NOT NULL)
- `idx_addons_created_at` - Creation ordering (DESC)

**Usage Notes:**
- Cascades delete when webhook or threat_model is deleted
- threat_model_id = NULL creates global addons
- objects array defines supported entity types
- icon can be URL or data URI

---

### addon_invocation_quotas

Per-owner addon invocation quotas and rate limits.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| owner_internal_uuid | UUID | PRIMARY KEY, FOREIGN KEY → users(internal_uuid) ON DELETE CASCADE | Quota owner |
| max_active_invocations | INT | NOT NULL, DEFAULT 1 | Maximum concurrent addon invocations |
| max_invocations_per_hour | INT | NOT NULL, DEFAULT 10 | Hourly invocation rate limit |
| created_at | TIMESTAMPTZ | NOT NULL, DEFAULT CURRENT_TIMESTAMP | Creation timestamp |
| modified_at | TIMESTAMPTZ | NOT NULL, DEFAULT CURRENT_TIMESTAMP | Last modification timestamp |

**Indexes:**
- `idx_addon_invocation_quotas_owner` - Owner quota lookups

**Triggers:**
- `update_addon_invocation_quotas_modified_at` - Auto-update modified_at on UPDATE

**Default Quotas:**
- Active invocations: 1
- Invocations per hour: 10

**Usage Notes:**
- Cascades delete when user is deleted
- Quotas enforced at application layer
- Prevents addon abuse and runaway executions

---

## Administration Tables

### administrators

Administrator privileges for users and groups, supporting dual foreign keys.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| id | UUID | PRIMARY KEY, DEFAULT uuid_generate_v4() | Administrator record identifier |
| user_internal_uuid | UUID | NULL, FOREIGN KEY → users(internal_uuid) ON DELETE CASCADE | User subject (NULL if group) |
| group_internal_uuid | UUID | NULL, FOREIGN KEY → groups(internal_uuid) ON DELETE CASCADE | Group subject (NULL if user) |
| subject_type | TEXT | NOT NULL, CHECK (subject_type IN ('user', 'group')) | Subject discriminator |
| provider | TEXT | NOT NULL | OAuth provider for principal matching |
| granted_at | TIMESTAMPTZ | NOT NULL, DEFAULT CURRENT_TIMESTAMP | Grant timestamp |
| granted_by_internal_uuid | UUID | NULL, FOREIGN KEY → users(internal_uuid) ON DELETE SET NULL | User who granted admin privileges |
| notes | TEXT | NULL | Administrative notes |

**Constraints:**
- `exactly_one_subject` - XOR constraint ensures exactly one of user_internal_uuid or group_internal_uuid is populated:
  - subject_type = 'user': user_internal_uuid NOT NULL, group_internal_uuid NULL
  - subject_type = 'group': group_internal_uuid NOT NULL, user_internal_uuid NULL

**Unique Constraints:**
- `UNIQUE NULLS NOT DISTINCT (user_internal_uuid, subject_type)` - One admin record per user
- `UNIQUE NULLS NOT DISTINCT (group_internal_uuid, subject_type, provider)` - One admin record per group/provider

**Indexes:**
- `idx_administrators_user` - User admin lookups (partial WHERE user_internal_uuid IS NOT NULL)
- `idx_administrators_group` - (group_internal_uuid, provider) group admin lookups (partial WHERE group_internal_uuid IS NOT NULL)
- `idx_administrators_provider` - Provider filtering
- `idx_administrators_granted_at` - Grant chronology (DESC)

**Usage Notes:**
- Dual foreign key pattern enforces type safety
- provider field necessary for group principal matching
- Cascades delete when user or group is deleted
- granted_by_internal_uuid set to NULL if granting user is deleted

---

## API Quota Tables

### user_api_quotas

Per-user API rate limits for general TMI API usage.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| user_internal_uuid | UUID | PRIMARY KEY, FOREIGN KEY → users(internal_uuid) ON DELETE CASCADE | Quota owner |
| max_requests_per_minute | INT | NOT NULL, DEFAULT 100 | Per-minute API rate limit |
| max_requests_per_hour | INT | NULL | Per-hour API rate limit (NULL = no limit) |
| created_at | TIMESTAMPTZ | NOT NULL, DEFAULT CURRENT_TIMESTAMP | Creation timestamp |
| modified_at | TIMESTAMPTZ | NOT NULL, DEFAULT CURRENT_TIMESTAMP | Last modification timestamp |

**Indexes:**
- `idx_user_api_quotas_user` - User quota lookups

**Triggers:**
- `update_user_api_quotas_modified_at` - Auto-update modified_at on UPDATE

**Default Quotas:**
- Requests per minute: 100
- Requests per hour: NULL (no limit)

**Usage Notes:**
- Cascades delete when user is deleted
- Quotas enforced at application layer (middleware)
- Separate from webhook-specific quotas

---

## Database Functions

### update_modified_at_column()

Trigger function that automatically updates the `modified_at` column to the current timestamp.

**Language**: PL/pgSQL
**Returns**: TRIGGER

**Usage**: Attached to BEFORE UPDATE triggers on all tables with modified_at columns.

**Tables with Triggers**:
- threat_models
- diagrams
- threats
- threat_model_access
- documents
- notes
- repositories
- assets
- metadata
- webhook_subscriptions
- webhook_quotas
- addon_invocation_quotas
- user_api_quotas

---

### notify_webhook_subscription_change()

Trigger function that sends PostgreSQL NOTIFY events on webhook subscription changes.

**Language**: PL/pgSQL
**Returns**: TRIGGER
**Channel**: `webhook_subscription_change`

**Payload**:
```json
{
  "operation": "INSERT|UPDATE|DELETE",
  "subscription_id": "uuid",
  "status": "pending_verification|active|pending_delete"
}
```

**Usage**: Wakes worker processes to handle subscription verification and cleanup.

**Trigger**: `webhook_subscription_change_notify` on webhook_subscriptions table (AFTER INSERT OR UPDATE OR DELETE)

---

### prevent_everyone_group_deletion()

Trigger function that prevents deletion of the "everyone" pseudo-group (UUID: 00000000-0000-0000-0000-000000000000).

**Language**: PL/pgSQL
**Returns**: TRIGGER

**Usage**: Protects system-reserved group from accidental deletion.

**Trigger**: `prevent_everyone_deletion` on groups table (BEFORE DELETE)

---

## Relationships and Foreign Keys

### Core Infrastructure Relationships

```
users (1) ──< (N) refresh_tokens
users (1) ──< (N) session_participants
collaboration_sessions (1) ──< (N) session_participants
threat_models (1) ──< (N) collaboration_sessions
diagrams (1) ──< (N) collaboration_sessions
```

### Business Domain Relationships

```
users (1) ──< (N) threat_models (owner)
users (1) ──< (N) threat_models (creator)
threat_models (1) ──< (N) diagrams
threat_models (1) ──< (N) threats
threat_models (1) ──< (N) assets
threat_models (1) ──< (N) documents
threat_models (1) ──< (N) notes
threat_models (1) ──< (N) repositories
diagrams (1) ──< (N) threats (optional)
assets (1) ──< (N) threats (optional)
```

### Authorization Relationships

```
threat_models (1) ──< (N) threat_model_access
users (1) ──< (N) threat_model_access (subject)
groups (1) ──< (N) threat_model_access (subject)
users (1) ──< (N) threat_model_access (granted_by)
```

### Webhook Relationships

```
users (1) ──< (N) webhook_subscriptions (owner)
threat_models (1) ──< (N) webhook_subscriptions (optional scope)
webhook_subscriptions (1) ──< (N) webhook_deliveries
webhook_subscriptions (1) ──< (N) addons
threat_models (1) ──< (N) addons (optional scope)
users (1) ──< (1) webhook_quotas
users (1) ──< (1) addon_invocation_quotas
```

### Administration Relationships

```
users (1) ──< (N) administrators (subject)
groups (1) ──< (N) administrators (subject)
users (1) ──< (N) administrators (granted_by)
users (1) ──< (1) user_api_quotas
```

---

## Indexes Summary

### Performance Optimization Strategy

1. **Composite Indexes**: Optimized query patterns (e.g., threat_model_id + created_at)
2. **Partial Indexes**: Filtered indexes for specific conditions (e.g., WHERE status = 'active')
3. **INCLUDE Indexes**: Covering indexes with included columns (e.g., INCLUDE (id, name))
4. **GIN Indexes**: JSONB field indexing (e.g., diagrams.cells)
5. **Unique Indexes**: Enforce business constraints (e.g., active participant uniqueness)

### Index Categories

| Category | Count | Purpose |
|----------|-------|---------|
| Foreign Key Indexes | 40+ | Accelerate join queries |
| Timestamp Indexes | 30+ | Support chronological ordering |
| Status/Type Indexes | 20+ | Filter by enumerated values |
| Composite Indexes | 15+ | Multi-column query optimization |
| Partial Indexes | 12+ | Filtered subset indexing |
| GIN Indexes | 1 | JSONB cell queries |
| Unique Indexes | 8+ | Enforce constraints |

---

## Constraints Summary

### Data Integrity Constraints

1. **Foreign Key Constraints**: Referential integrity with CASCADE/RESTRICT/SET NULL
2. **Check Constraints**: Enumerated values, length limits, format validation
3. **Unique Constraints**: Prevent duplicates (UNIQUE NULLS NOT DISTINCT for nullable columns)
4. **Not Null Constraints**: Required fields
5. **XOR Constraints**: Dual foreign key pattern enforcement

### Example XOR Constraint (Dual Foreign Keys)

```sql
CONSTRAINT exactly_one_subject CHECK (
    (subject_type = 'user' AND user_internal_uuid IS NOT NULL AND group_internal_uuid IS NULL) OR
    (subject_type = 'group' AND group_internal_uuid IS NOT NULL AND user_internal_uuid IS NULL)
)
```

**Used in**: threat_model_access, administrators

---

## Cascade Deletion Rules

### ON DELETE CASCADE (Child deleted with parent)

| Parent Table | Child Table | Rationale |
|--------------|-------------|-----------|
| users | refresh_tokens | User's tokens invalidated |
| users | session_participants | User's session participation removed |
| users | threat_model_access | User's access grants revoked |
| users | webhook_subscriptions | User's webhooks deleted |
| users | webhook_quotas | User's quotas deleted |
| users | addon_invocation_quotas | User's addon quotas deleted |
| users | user_api_quotas | User's API quotas deleted |
| users | administrators | User's admin privileges revoked |
| groups | threat_model_access | Group's access grants revoked |
| groups | administrators | Group's admin privileges revoked |
| collaboration_sessions | session_participants | Session participants removed |
| threat_models | diagrams | Diagrams deleted with threat model |
| threat_models | threats | Threats deleted with threat model |
| threat_models | assets | Assets deleted with threat model |
| threat_models | documents | Documents deleted with threat model |
| threat_models | notes | Notes deleted with threat model |
| threat_models | repositories | Repositories deleted with threat model |
| threat_models | threat_model_access | Access grants deleted with threat model |
| threat_models | webhook_subscriptions | Scoped webhooks deleted |
| threat_models | addons | Scoped addons deleted |
| threat_models | collaboration_sessions | Sessions deleted with threat model |
| diagrams | collaboration_sessions | Sessions deleted with diagram |
| webhook_subscriptions | webhook_deliveries | Deliveries deleted with subscription |
| webhook_subscriptions | addons | Addons deleted with webhook |

### ON DELETE RESTRICT (Prevents deletion if children exist)

| Parent Table | Child Table | Rationale |
|--------------|-------------|-----------|
| users | threat_models (owner) | Cannot delete user who owns threat models |
| users | threat_models (creator) | Cannot delete user who created threat models |

### ON DELETE SET NULL (Child reference cleared)

| Parent Table | Child Table | Rationale |
|--------------|-------------|-----------|
| diagrams | threats.diagram_id | Threat remains, diagram reference cleared |
| assets | threats.asset_id | Threat remains, asset reference cleared |
| users | threat_model_access.granted_by | Grant remains, granter reference cleared |
| users | administrators.granted_by | Admin grant remains, granter reference cleared |

---

## Schema Version History

| Version | Migration | Date | Description |
|---------|-----------|------|-------------|
| 1 | 001_core_infrastructure.up.sql | Initial | Users, authentication, collaboration sessions |
| 2 | 002_business_domain.up.sql | Initial | Threat models, RBAC, webhooks, addons |
| 3 | 003_administrator_provider_fields.up.sql | Initial | Administrator table restructure with dual foreign keys |

---

## Best Practices

### UUID Generation

- **Default**: UUIDv4 (random) via `uuid_generate_v4()`
- **Time-ordered**: UUIDv7 for threats, metadata, webhook_deliveries (application-generated)
- **Special**: Flag UUID for "everyone" pseudo-group (00000000-0000-0000-0000-000000000000)

### Timestamp Management

- **created_at**: Set once on INSERT, never updated
- **modified_at**: Auto-updated via triggers on UPDATE
- **Special timestamps**: last_login, status_updated, granted_at, expires_at

### Text Field Constraints

- **Empty prevention**: `CHECK (LENGTH(TRIM(field)) > 0)` on required text fields
- **Length limits**: status (128), metadata key (128), metadata value (65535)
- **Format validation**: metadata key regex `^[a-zA-Z0-9_-]+$`

### JSONB Usage

- **diagrams.cells**: JointJS/RappID cell definitions with GIN index
- **repositories.parameters**: VCS-specific configuration
- **webhook_deliveries.payload**: Event payloads

### Array Fields

- **assets.classification**: TEXT[] for multiple classification tags
- **webhook_subscriptions.events**: TEXT[] for event type subscriptions
- **addons.objects**: TEXT[] for supported object types

---

## Common Queries

### Find threat models accessible to user

```sql
-- Direct user access
SELECT tm.*
FROM threat_models tm
JOIN threat_model_access tma ON tm.id = tma.threat_model_id
WHERE tma.subject_type = 'user'
  AND tma.user_internal_uuid = :user_uuid;

-- Group-based access
SELECT tm.*
FROM threat_models tm
JOIN threat_model_access tma ON tm.id = tma.threat_model_id
JOIN groups g ON tma.group_internal_uuid = g.internal_uuid
WHERE tma.subject_type = 'group'
  AND g.provider IN (:user_provider, '*')
  AND g.group_name IN (:user_groups);
```

### Check administrator privileges

```sql
-- User is admin
SELECT EXISTS (
  SELECT 1 FROM administrators
  WHERE subject_type = 'user'
    AND user_internal_uuid = :user_uuid
);

-- User's group is admin
SELECT EXISTS (
  SELECT 1 FROM administrators a
  JOIN groups g ON a.group_internal_uuid = g.internal_uuid
  WHERE a.subject_type = 'group'
    AND a.provider = :user_provider
    AND g.group_name IN (:user_groups)
);
```

### Get active collaboration sessions for diagram

```sql
SELECT cs.*, COUNT(sp.id) AS participant_count
FROM collaboration_sessions cs
LEFT JOIN session_participants sp ON cs.id = sp.session_id AND sp.left_at IS NULL
WHERE cs.diagram_id = :diagram_id
  AND (cs.expires_at IS NULL OR cs.expires_at > NOW())
GROUP BY cs.id;
```

### Find pending webhook deliveries for retry

```sql
SELECT wd.*
FROM webhook_deliveries wd
WHERE wd.status = 'pending'
  AND wd.next_retry_at <= NOW()
ORDER BY wd.created_at
LIMIT 100;
```

---

## Extensions

### uuid-ossp

**Purpose**: UUID generation functions
**Key Functions**:
- `uuid_generate_v4()`: Random UUIDs (default)

**Migration**: Enabled in 001_core_infrastructure.up.sql

---

## Migration Execution

### Apply Migrations

```bash
# Production deployment (auto-migration on startup)
./bin/tmiserver --config=config-production.yml

# Manual migration (check-db command)
./bin/check-db --config=config-development.yml
```

### Heroku Database Operations

```bash
# Reset database (destructive - drop and recreate schema)
make heroku-reset-db

# Drop database (destructive - leaves empty schema)
make heroku-drop-db
```

**Warning**: Both operations delete all data and require manual confirmation.

---

## Security Considerations

### SSRF Prevention

- webhook_url_deny_list table blocks internal/private endpoints
- Seeded patterns block RFC 1918 ranges, cloud metadata services, Kubernetes internals

### Token Security

- refresh_tokens stored encrypted at rest (application responsibility)
- access_token and refresh_token in users table for OAuth flows
- Token expiry tracked in token_expiry field

### Authorization Inheritance

- Child resources (diagrams, threats, documents, etc.) inherit threat_model authorization
- Performance indexes support efficient authorization queries
- Group-based access enables organization-wide permissions

### Audit Trail

- created_at, modified_at on all business entities
- granted_by_internal_uuid tracks who granted access
- status_updated tracks threat model status changes

---

## Performance Tuning

### Index Maintenance

```sql
-- Analyze index usage
SELECT schemaname, tablename, indexname, idx_scan, idx_tup_read, idx_tup_fetch
FROM pg_stat_user_indexes
ORDER BY idx_scan DESC;

-- Rebuild index (if needed)
REINDEX INDEX CONCURRENTLY idx_name;
```

### Vacuum Strategy

```sql
-- Analyze table statistics
ANALYZE threat_models;

-- Vacuum to reclaim space
VACUUM (ANALYZE, VERBOSE) threat_models;
```

### Query Optimization

- Use EXPLAIN ANALYZE to profile queries
- Leverage composite indexes for common query patterns
- Consider partial indexes for filtered queries
- Use INCLUDE indexes for covering index scans

---

## References

- **OpenAPI Specification**: `/Users/efitz/Projects/tmi/docs/reference/apis/tmi-openapi.json`
- **Migration Files**: `/Users/efitz/Projects/tmi/auth/migrations/`
- **Development Setup**: `/Users/efitz/Projects/tmi/docs/developer/setup/development-setup.md`
- **Integration Testing**: `/Users/efitz/Projects/tmi/docs/developer/testing/integration-testing.md`
- **Heroku Database Reset**: `/Users/efitz/Projects/tmi/docs/operator/heroku-database-reset.md`
