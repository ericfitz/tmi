# Heroku Database Reset Guide

## Overview

This guide covers how to completely drop and recreate the PostgreSQL database schema on Heroku. This operation is useful when:

- Database migrations are out of sync
- Schema needs to be rebuilt from scratch
- Testing a clean deployment
- Recovering from migration errors

**⚠️ WARNING**: This operation is **DESTRUCTIVE** and will **DELETE ALL DATA** in the database. This action cannot be undone.

## Prerequisites

- Heroku CLI installed and authenticated
- Access to the `tmi-server` Heroku app
- Confirmation that data loss is acceptable

## Quick Start

### Using Make (Recommended)

```bash
make heroku-reset-db
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

### Step 2: Run Migrations

Runs `./bin/server migrate` on Heroku to:
- Apply all migration files in order
- Create tables with correct schema
- Set up indexes and constraints
- Initialize extensions (uuid-ossp)

### Step 3: Verify Schema

Verifies the database by checking:
- Total table count
- Presence of expected tables
- Specific columns (e.g., `issue_uri` in `threat_models`)
- Migration status (version and dirty flag)

## Expected Tables

After reset, the following tables should exist:

1. `collaboration_sessions` - WebSocket collaboration tracking
2. `diagrams` - Threat model diagrams
3. `documents` - Document references
4. `metadata` - Key-value metadata for entities
5. `notes` - Threat model notes
6. `refresh_tokens` - OAuth refresh tokens
7. `repositories` - Source code repository references
8. `schema_migrations` - Migration tracking
9. `session_participants` - Collaboration session users
10. `threat_model_access` - RBAC for threat models
11. `threat_models` - Main threat model entities
12. `threats` - Individual threats
13. `user_providers` - OAuth provider mappings
14. `users` - User accounts

## Critical Schema Verification

The script specifically verifies:

- **threat_models.issue_uri** - Column exists (was causing 500 errors)
- **notes table** - Exists for note-taking feature
- **Migration version** - All migrations applied without dirty flag

## Post-Reset Actions

After resetting the database:

1. **Users must re-authenticate** - All user sessions are invalidated
2. **OAuth providers** - Users need to log in again via OAuth
3. **Data recreation** - Any test data must be recreated
4. **Verify functionality** - Test creating a threat model

## Script Output

The script provides colored output:

- 🔵 **Blue** - Information messages
- 🟡 **Yellow** - Warnings and progress
- 🟢 **Green** - Success messages
- 🔴 **Red** - Errors

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
✓ Schema dropped successfully

Step 2/3: Running migrations...
✓ Migrations completed

Step 3/3: Verifying schema...
Tables created: 14
✓ issue_uri column exists
✓ notes table exists

========================================
✓ Database reset complete!
========================================

Next steps:
  1. Users will need to re-authenticate via OAuth
  2. All previous data has been deleted
  3. Test creating a threat model to verify functionality
```

## Troubleshooting

### Migration Fails

If migrations fail during Step 2:

1. Check Heroku logs: `heroku logs --tail --app tmi-server`
2. Verify the binary exists: `heroku run -a tmi-server 'ls -la bin/'`
3. Check migration files are in the slug: `heroku run -a tmi-server 'ls -la auth/migrations/'`

### Schema Verification Fails

If verification fails:

1. Manually check tables: `heroku run -a tmi-server "echo \"\\dt\" | psql \$DATABASE_URL"`
2. Check specific table: `heroku run -a tmi-server "echo \"\\d threat_models\" | psql \$DATABASE_URL"`
3. Review migration status: `heroku run -a tmi-server "echo \"SELECT * FROM schema_migrations;\" | psql \$DATABASE_URL"`

### Permission Errors

If you see permission errors:

- The `ERROR: role "postgres" does not exist` is **expected** and can be ignored
- Heroku uses a different role name, but the schema is still created correctly

## Manual Database Reset

If the script doesn't work, you can perform the steps manually:

### 1. Drop Schema Manually

```bash
heroku run -a tmi-server 'echo "DROP SCHEMA public CASCADE; CREATE SCHEMA public; GRANT ALL ON SCHEMA public TO public;" | psql $DATABASE_URL'
```

### 2. Run Migrations Manually

```bash
heroku run -a tmi-server './bin/server migrate'
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

# Then run migrations
make heroku-reset-db
```

## Safety Checklist

Before running the reset:

- [ ] Confirmed that data loss is acceptable
- [ ] Notified users of downtime (if applicable)
- [ ] Backed up any important data
- [ ] Verified you're targeting the correct app
- [ ] Ready to have users re-authenticate

## Related Documentation

- [Database Migrations](../developer/setup/database-migrations.md)
- [Heroku Deployment](heroku-deployment.md)
- [Development Setup](../developer/setup/development-setup.md)

## Script Location

- Script: [`scripts/heroku-reset-database.sh`](../../scripts/heroku-reset-database.sh)
- Make target: `make heroku-reset-db`

## Notes

- The script uses `set -e` to exit immediately on any error
- All commands are confirmed before execution
- The script requires manual "yes" confirmation to prevent accidents
- Background migration processes are handled automatically
