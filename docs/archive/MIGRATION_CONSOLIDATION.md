# Migration Consolidation - January 2025

## Overview

This document records the consolidation of TMI database migrations from 8 separate files into 2 base migrations as part of the User Identification Architecture refactoring.

**Date**: 2025-01-22
**Purpose**: Simplify database schema management and implement new user identification architecture
**Related Document**: [User Identification Architecture Refactoring Plan](../developer/planning/user-identification-architecture.md)

## Consolidation Strategy

### Original Migration Files (Archived)

The following 8 migration files were archived to `docs/archive/migrations/`:

1. `001_core_infrastructure.up.sql` - Users, user_providers, refresh_tokens, collaboration sessions
2. `002_business_domain.up.sql` - Threat models, diagrams, threats, assets, documents, notes, repositories, metadata, threat_model_access
3. `003_webhook_subscriptions.up.sql` - Webhook subscriptions and deliveries
4. `004_addon_invocations.up.sql` - Addon invocations tracking
5. `005_administrators.up.sql` - System administrators table
6. `006_authorization_groups.up.sql` - Authorization groups for group-based access
7. `007_addon_invocation_quotas.up.sql` - Addon usage quotas
8. `008_user_api_quotas.up.sql` - User API request quotas

### New Consolidated Migrations

Created 2 new base migrations implementing the new user identification architecture:

#### 000001_core_infrastructure.up.sql

**Key Changes from Original**:
- **ELIMINATED `user_providers` table** - Consolidated into `users` table
- **New `users` table schema**:
  - `internal_uuid` (UUID primary key) - Internal system identifier
  - `provider` (TEXT) - OAuth provider: "test", "google", "github", "microsoft", "azure"
  - `provider_user_id` (TEXT) - Provider's user ID from JWT sub claim
  - `email`, `name` - User profile fields (updateable from JWT claims)
  - `access_token`, `refresh_token`, `token_expiry` - OAuth tokens (moved from user_providers)
  - **UNIQUE constraint**: `(provider, provider_user_id)` - Enforces one user per provider account
- **Updated foreign keys**:
  - `refresh_tokens.user_internal_uuid` → `users(internal_uuid)`
  - `session_participants.user_internal_uuid` → `users(internal_uuid)`
- **Business logic change**: Users from different providers treated as separate users even with same email

**Tables Created**:
- `users` (consolidated schema)
- `refresh_tokens`
- `collaboration_sessions`
- `session_participants`

#### 000002_business_domain.up.sql

**Key Changes from Original**:
- **UUID-based ownership** (NOT email-based):
  - `threat_models.owner_internal_uuid` (UUID) → `users(internal_uuid)`
  - `threat_models.created_by` (TEXT) - Denormalized email for audit trail
- **Updated `threat_model_access` table**:
  - `subject_internal_uuid` (UUID) → `users(internal_uuid)` for users
  - `subject_type` field distinguishes between 'user' and 'group' subjects
  - For groups: `subject_internal_uuid` is NULL, group identified by `subject` (group name) + `idp`
- **Consolidated tables from migrations 003-008**:
  - Webhook subscriptions and deliveries
  - Addon invocations
  - Administrators
  - Authorization groups
  - Addon invocation quotas
  - User API quotas
- **Consistent foreign key naming**: `{relationship}_internal_uuid` pattern
  - `owner_internal_uuid`, `user_internal_uuid`, `granted_by_internal_uuid`

**Tables Created**:
- `threat_models`, `diagrams`, `threats`, `assets`
- `documents`, `notes`, `repositories`, `metadata`
- `threat_model_access`
- `webhook_subscriptions`, `webhook_deliveries`
- `addon_invocations`
- `administrators`
- `authorization_groups`
- `addon_invocation_quotas`, `user_api_quotas`

## Architectural Changes

### User Identification

**Old Architecture**:
- `users.id` (UUID) as primary key
- `users.email` as UNIQUE identifier
- `user_providers` table linking users to multiple OAuth providers
- Email-based foreign keys in some tables

**New Architecture**:
- `users.internal_uuid` (UUID) as primary key
- `users.provider` + `users.provider_user_id` as UNIQUE identifier
- No `user_providers` table (data merged into `users`)
- **UUID-based foreign keys everywhere** (no email-based foreign keys)
- JWT `sub` claim contains `provider_user_id` (never `internal_uuid`)

### Naming Conventions

All user-related identifiers now use descriptive prefixes:

- `internal_uuid` - System's internal UUID (never exposed in JWT)
- `provider` - OAuth provider name
- `provider_user_id` - Provider's user ID (from JWT sub)
- Foreign keys: `owner_internal_uuid`, `user_internal_uuid`, `granted_by_internal_uuid`

### Authorization Model

**Critical Change**: Authorization now uses UUIDs instead of emails

**Old**: `threat_model_access.subject` stored email addresses for users
**New**: `threat_model_access.subject_internal_uuid` stores UUIDs, `subject_type='user'`

For groups, the pattern is:
- `subject_internal_uuid` = NULL
- `subject` = group name
- `subject_type` = 'group'
- `idp` = identity provider

## Production Database Impact

### Existing Data

Production database inspection (2025-01-22) revealed:
- **6 users** in current `users` table
- **3 threat models** with email-based ownership (`owner_email` field)
- **No `user_providers` table** exists (table was never deployed to production)

### Migration Path

1. **Archive old migrations** to `docs/archive/migrations/`
2. **Reset Heroku database** using `make heroku-reset-db`
   - Drops all tables and recreates with new schema
   - **DATA LOSS**: All users and threat models will be deleted
   - Users must re-authenticate via OAuth
3. **Run new migrations** (000001, 000002)
4. **Verify schema** with database inspection

**Note**: Because the `user_providers` table never existed in production, the consolidation is cleaner than originally anticipated. No data migration needed for that table.

## Files Affected

### Created
- `auth/migrations/000001_core_infrastructure.up.sql`
- `auth/migrations/000001_core_infrastructure.down.sql`
- `auth/migrations/000002_business_domain.up.sql`
- `auth/migrations/000002_business_domain.down.sql`
- `docs/archive/MIGRATION_CONSOLIDATION.md` (this file)

### Archived
- `docs/archive/migrations/001_core_infrastructure.up.sql`
- `docs/archive/migrations/002_business_domain.up.sql`
- `docs/archive/migrations/003_webhook_subscriptions.up.sql`
- `docs/archive/migrations/004_addon_invocations.up.sql`
- `docs/archive/migrations/005_administrators.up.sql`
- `docs/archive/migrations/006_authorization_groups.up.sql`
- `docs/archive/migrations/007_addon_invocation_quotas.up.sql`
- `docs/archive/migrations/008_user_api_quotas.up.sql`

### To Be Deleted (Phase 1 completion)
- `auth/migrations/001_core_infrastructure.up.sql` (after verification)
- `auth/migrations/002_business_domain.up.sql` (after verification)
- `auth/migrations/003_webhook_subscriptions.up.sql`
- `auth/migrations/004_addon_invocations.up.sql`
- `auth/migrations/005_administrators.up.sql`
- `auth/migrations/006_authorization_groups.up.sql`
- `auth/migrations/007_addon_invocation_quotas.up.sql`
- `auth/migrations/008_user_api_quotas.up.sql`

## Next Steps

After consolidation completion:

1. **Phase 2**: Database Recreation
   - Reset development database
   - Reset Heroku database
   - Verify schema correctness

2. **Phase 3-11**: Code Updates
   - Update auth package to use new schema
   - Implement user cache
   - Update JWT middleware
   - Update all handlers
   - Update all tests
   - Update type converters
   - Add linter rules
   - Final verification

See [User Identification Architecture Refactoring Plan](../developer/planning/user-identification-architecture.md) for complete implementation timeline.

## Validation Checklist

After migration consolidation:

- [ ] New migrations apply cleanly to empty database
- [ ] Down migrations revert all changes
- [ ] All tables have proper foreign key constraints
- [ ] UNIQUE constraints enforce business logic
- [ ] Indexes created for all foreign keys
- [ ] No orphaned tables or columns
- [ ] Authorization queries work with UUID-based ownership
- [ ] JWT claims map correctly to database fields

## References

- Planning Document: `docs/developer/planning/user-identification-architecture.md`
- Original Migrations: `docs/archive/migrations/`
- New Migrations: `auth/migrations/000001_*.sql`, `auth/migrations/000002_*.sql`
