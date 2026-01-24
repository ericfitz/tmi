# Heroku Database Reset Guide
<!-- Migrated from: docs/operator/heroku-database-reset.md on 2025-01-24 -->

## Overview

This guide covers how to completely drop and recreate the PostgreSQL database schema on Heroku. This operation is useful when:

- Database migrations are out of sync
- Schema needs to be rebuilt from scratch
- Testing a clean deployment
- Recovering from migration errors

**WARNING**: This operation is **DESTRUCTIVE** and will **DELETE ALL DATA** in the database. This action cannot be undone.

## Prerequisites

- Heroku CLI installed and authenticated
- Access to the `tmi-server` Heroku app
- Confirmation that data loss is acceptable

## Quick Start

### Using Make (Recommended)

```bash
make reset-db-heroku
```

This will prompt for confirmation before proceeding.

### Using Script Directly

```bash
./scripts/heroku-reset-database.sh tmi-server
```

To use a different Heroku app:

```bash
./scripts/heroku-reset-database.sh your-app-name
```

## What the Script Does

The script performs three main steps:

### Step 1: Drop All Tables

Executes `DROP SCHEMA public CASCADE` which removes:
- All tables
- All indexes
- All constraints
- All stored procedures
- All extensions (except those in other schemas)

The public schema is then recreated with default permissions.

### Step 2: Restart Server to Run AutoMigrate

Restarts the Heroku dyno with `heroku dyno:restart` which triggers GORM AutoMigrate to:
- Apply all model definitions automatically
- Create tables with correct schema
- Set up indexes and constraints
- Initialize extensions (uuid-ossp)

Note: TMI uses GORM AutoMigrate on server startup rather than a separate migrate binary.

### Step 3: Verify Schema

Verifies the database by checking:
- Total table count
- Presence of expected tables
- Specific columns (e.g., `issue_uri` in `threat_models`)

## Expected Tables

After reset, the following tables should exist (25 total):

**Core User/Auth Tables:**
1. `users` - User accounts
2. `refresh_tokens` - OAuth refresh tokens
3. `client_credentials` - OAuth 2.0 client credentials for machine-to-machine auth
4. `groups` - Identity provider groups
5. `group_members` - User memberships in groups
6. `administrators` - Administrator designations (users and groups)

**Threat Model Tables:**
7. `threat_models` - Main threat model entities
8. `threat_model_access` - RBAC for threat models
9. `diagrams` - Threat model diagrams
10. `threats` - Individual threats
11. `assets` - Assets within threat models
12. `documents` - Document references
13. `notes` - Threat model notes
14. `repositories` - Source code repository references
15. `metadata` - Key-value metadata for entities

**Collaboration Tables:**
16. `collaboration_sessions` - WebSocket collaboration tracking
17. `session_participants` - Collaboration session users

**Webhook Tables:**
18. `webhook_subscriptions` - Webhook subscription configurations
19. `webhook_deliveries` - Webhook delivery attempts
20. `webhook_quotas` - Per-user webhook quotas
21. `webhook_url_deny_list` - Blocked webhook URL patterns

**Addon Tables:**
22. `addons` - Addon configurations
23. `addon_invocation_quotas` - Per-user addon invocation quotas

**Quota/Preference Tables:**
24. `user_api_quotas` - Per-user API rate limits
25. `user_preferences` - User preferences stored as JSON

## Critical Schema Verification

The script specifically verifies:

- **threat_models.issue_uri** - Column exists (was causing 500 errors)
- **notes table** - Exists for note-taking feature

## Post-Reset Actions

After resetting the database:

1. **Users must re-authenticate** - All user sessions are invalidated
2. **OAuth providers** - Users need to log in again via OAuth
3. **Data recreation** - Any test data must be recreated
4. **Verify functionality** - Test creating a threat model

## Script Output

The script provides colored output:

- Blue - Information messages
- Yellow - Warnings and progress
- Green - Success messages
- Red - Errors

### Example Output

```
========================================
Heroku Database Reset Script
========================================

App: tmi-server

WARNING: This will DELETE ALL DATA in the tmi-server database!
This action cannot be undone.

Are you sure you want to continue? (type 'yes' to confirm): yes

Step 1/3: Dropping all tables...
  Schema dropped successfully

Step 2/3: Running migrations via server restart...
  Server restarted (AutoMigrate runs on startup)

Step 3/3: Verifying schema...
Tables created: 25
  issue_uri column exists
  notes table exists

========================================
  Database reset complete!
========================================

Next steps:
  1. Users will need to re-authenticate via OAuth
  2. All previous data has been deleted
  3. Test creating a threat model to verify functionality
```

## Troubleshooting

### Server Restart Fails

If the server restart fails during Step 2:

1. Check Heroku logs: `heroku logs --tail --app tmi-server`
2. Verify the dyno status: `heroku ps --app tmi-server`
3. Check for application errors in the logs

### Schema Verification Fails

If verification fails:

1. Manually check tables: `heroku run -a tmi-server "echo \"\\dt\" | psql \$DATABASE_URL"`
2. Check specific table: `heroku run -a tmi-server "echo \"\\d threat_models\" | psql \$DATABASE_URL"`
3. Review server logs for AutoMigrate errors: `heroku logs --tail --app tmi-server`

### Permission Errors

If you see permission errors:

- The `ERROR: role "postgres" does not exist` is **expected** and can be ignored
- Heroku uses a different role name, but the schema is still created correctly

## Manual Database Reset

If the script doesn't work, you can perform the steps manually:

### 1. Drop Schema Manually

```bash
heroku run -a tmi-server 'echo "DROP SCHEMA public CASCADE; CREATE SCHEMA public;" | psql $DATABASE_URL'
```

### 2. Restart Server to Run AutoMigrate

```bash
heroku dyno:restart -a tmi-server
```

### 3. Verify Schema Manually

```bash
# List tables
heroku run -a tmi-server "echo \"SELECT table_name FROM information_schema.tables WHERE table_schema = 'public' ORDER BY table_name;\" | psql \$DATABASE_URL"

# Check specific column
heroku run -a tmi-server "echo \"SELECT column_name FROM information_schema.columns WHERE table_name = 'threat_models' AND column_name = 'issue_uri';\" | psql \$DATABASE_URL"
```

## Alternative: Heroku Database Reset

For a complete database reset (including all data structures):

```bash
# Create a new database and swap (preserves data temporarily)
heroku addons:create heroku-postgresql:essential-0 --app tmi-server
heroku pg:wait --app tmi-server
heroku maintenance:on --app tmi-server
heroku pg:promote DATABASE_URL --app tmi-server
heroku maintenance:off --app tmi-server

# Then restart to run AutoMigrate
heroku dyno:restart -a tmi-server
```

## Safety Checklist

Before running the reset:

- [ ] Confirmed that data loss is acceptable
- [ ] Notified users of downtime (if applicable)
- [ ] Backed up any important data
- [ ] Verified you're targeting the correct app
- [ ] Ready to have users re-authenticate

## Related Documentation

- [Development Setup](../developer/setup/development-setup.md)

<!-- NEEDS-REVIEW: The following referenced docs do not exist and may need to be created:
- Database Migrations guide (../developer/setup/database-migrations.md)
- Heroku Deployment guide (heroku-deployment.md)
-->

## Script Location

- Script: [`scripts/heroku-reset-database.sh`](../../scripts/heroku-reset-database.sh)
- Make target: `make reset-db-heroku`

## Notes

- The script uses `set -e` to exit immediately on any error
- All commands are confirmed before execution
- The script requires manual "yes" confirmation to prevent accidents
- TMI uses GORM AutoMigrate on server startup for schema management

---

## Verification Summary

**Document verified on 2025-01-24**

| Item | Status | Notes |
|------|--------|-------|
| Script path `scripts/heroku-reset-database.sh` | Verified | File exists and matches documentation |
| Make target `reset-db-heroku` | Verified | Target exists in Makefile (line 1163) |
| Heroku CLI commands | Verified | Commands verified against [Heroku Dev Center](https://devcenter.heroku.com/articles/heroku-cli-commands) |
| Expected tables list | Corrected | Updated from 14 to 25 tables per `api/models/models.go:AllModels()` |
| Step 2 migration method | Corrected | Changed from `./bin/server migrate` to `heroku dyno:restart` (GORM AutoMigrate) |
| Related doc: development-setup.md | Verified | File exists at `docs/developer/setup/development-setup.md` |
| Related doc: database-migrations.md | Not Found | File does not exist |
| Related doc: heroku-deployment.md | Not Found | File does not exist |
