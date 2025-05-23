# Database Setup Instructions

This document explains how to set up the PostgreSQL database schemas required for the TMI application.

## Prerequisites

1. PostgreSQL 12+ installed and running
2. Redis 6+ installed and running (for caching)
3. Go 1.24+ installed
4. Database credentials configured in `.env.dev` file

## Database Schema Overview

The application uses the following tables:

1. **users** - Stores user accounts
2. **user_providers** - Links OAuth providers to user accounts
3. **threat_models** - Main threat model entities
4. **threat_model_access** - Authorization/access control for threat models
5. **threats** - Individual threats within threat models
6. **diagrams** - Diagrams associated with threat models

## Setup Methods

### Method 1: Using the Setup Script (Recommended)

We've created a Go script that will automatically create the database and all required tables:

```bash
# From the project root directory
go run cmd/setup-db/main.go
```

This script will:

- Create the database if it doesn't exist
- Create all required tables with proper foreign key relationships
- Create all necessary indexes
- Set up the schema_migrations table

### Method 2: Using SQL File Directly

If you have `psql` access, you can run the SQL file directly:

```bash
# Create the database first (if it doesn't exist)
createdb tmi

# Run the setup script
psql -U postgres -d tmi -f setup_database.sql
```

### Method 3: Using the Migration Tool

The project includes migration files in `auth/migrations/`. However, these need to be properly formatted for the golang-migrate tool:

```bash
# Run migrations
go run cmd/migrate/main.go --env=.env.dev

# To rollback migrations
go run cmd/migrate/main.go --env=.env.dev --down
```

## Verify Database Setup

To verify that the database has been set up correctly:

```bash
go run cmd/check-db/main.go
```

This will:

- Test the database connection
- Verify all tables exist
- Check that key indexes are created
- Display row counts for each table

## Environment Configuration

Ensure your `.env.dev` file contains the correct database credentials:

```env
POSTGRES_HOST=localhost
POSTGRES_PORT=5432
POSTGRES_USER=postgres
POSTGRES_PASSWORD=postgres
POSTGRES_DB=tmi
POSTGRES_SSLMODE=disable

REDIS_HOST=localhost
REDIS_PORT=6379
REDIS_PASSWORD=
REDIS_DB=0
```

## Troubleshooting

### Connection Issues

- Ensure PostgreSQL is running: `pg_ctl status` or `systemctl status postgresql`
- Check that the credentials in `.env.dev` match your PostgreSQL setup
- Verify PostgreSQL is listening on the correct port (default: 5432)

### Permission Issues

- The database user needs CREATE privileges to create the database
- For production, consider using a dedicated user with limited privileges

### Migration Issues

- The golang-migrate tool expects specific file naming: `000001_description.up.sql` and `000001_description.down.sql`
- Ensure no duplicate version numbers exist in the migrations directory

## Next Steps

Once the database is set up:

1. Start the Redis server for caching
2. Run the main application: `go run cmd/server/main.go --env=.env.dev`
3. The application will use the database for authentication and authorization

## Database Schema Diagram

```
users (1) ----< (N) user_providers
  |
  |
  v
threat_models (1) ----< (N) threat_model_access
  |                              ^
  |                              |
  |                            users
  |
  +----< (N) threats
  |
  +----< (N) diagrams
```

The schema supports:

- Multiple OAuth providers per user
- Role-based access control (owner, writer, reader)
- Hierarchical threat model structure
- Soft deletes through CASCADE relationships
