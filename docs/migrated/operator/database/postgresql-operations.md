# PostgreSQL Database Operations Guide

<!-- VERIFICATION SUMMARY
Verified: 2025-01-24
Migrated to: wiki/Database-Operations.md

CORRECTIONS MADE:
1. Go version updated from 1.24 to 1.25.6 (verified from go.mod)
2. Migration system now uses GORM AutoMigrate, NOT golang-migrate SQL files
3. auth/migrations/ directory no longer exists - migrations are in api/models/models.go
4. Migration file list (000001-000008) is OUTDATED - TMI uses GORM AutoMigrate
5. Provider list in CHECK constraints updated (TMI uses 'tmi' provider, not just OAuth providers)
6. Diagram type constraint now uses 'DFD-1.0.0' not the old list
7. Added note that PostgreSQL container uses Chainguard base image

VERIFIED ITEMS:
- internal/dbschema/schema.go exists and defines expected schema
- internal/dbschema/validator.go exists
- cmd/check-db/main.go exists and works as documented
- cmd/migrate/main.go exists but uses GORM AutoMigrate
- Makefile targets verified: start-database, migrate-database, check-database
- PostgreSQL connection string format is correct
- SQL queries for monitoring are standard PostgreSQL

NEEDS-REVIEW:
- PostgreSQL version minimum "12 or higher" - not verified in source code
- Redis version minimum "6 or higher" - not verified in source code
- Connection pooling settings (SetMaxOpenConns, etc.) - default values not found in source
- External URLs (PostgreSQL docs, wiki) - certificate errors prevented verification
-->

This comprehensive guide covers all aspects of deploying, migrating, validating, and supporting the PostgreSQL database for the TMI (Threat Model Intelligence) application.

## Table of Contents

1. [Prerequisites](#prerequisites)
2. [Environment Configuration](#environment-configuration)
3. [Migration Management](#migration-management)
4. [Schema Validation](#schema-validation)
5. [Operational Commands](#operational-commands)
6. [Troubleshooting](#troubleshooting)
7. [Performance Optimization](#performance-optimization)
8. [Security Considerations](#security-considerations)
9. [Backup and Recovery](#backup-and-recovery)

## Prerequisites

### System Requirements

- **PostgreSQL**: Version 12 or higher <!-- NEEDS-REVIEW: minimum version not verified in source -->
- **Redis**: Version 6 or higher (for caching) <!-- NEEDS-REVIEW: minimum version not verified in source -->
- **Go**: Version 1.25 or higher (verified: go.mod specifies 1.25.6)
- **Operating System**: Linux, macOS, or Windows with WSL

### Required Permissions

- Database user with CREATE privileges for initial setup
- GRANT privileges for production user management
- Superuser access for extension installation (if needed)

## Environment Configuration

### Development Environment

Create a `.env.dev` file with the following configuration:

```env
# PostgreSQL Configuration
POSTGRES_HOST=localhost
POSTGRES_PORT=5432
POSTGRES_USER=postgres
POSTGRES_PASSWORD=postgres
POSTGRES_DB=tmi
POSTGRES_SSLMODE=disable

# Redis Configuration
REDIS_HOST=localhost
REDIS_PORT=6379
REDIS_PASSWORD=
REDIS_DB=0
```

### Production Environment

For production deployments, use environment-specific configuration:

```env
# PostgreSQL Configuration
POSTGRES_HOST=your-db-host.example.com
POSTGRES_PORT=5432
POSTGRES_USER=tmi_app
POSTGRES_PASSWORD=<strong-password>
POSTGRES_DB=tmi_production
POSTGRES_SSLMODE=require

# Redis Configuration
REDIS_HOST=your-redis-host.example.com
REDIS_PORT=6379
REDIS_PASSWORD=<redis-password>
REDIS_DB=0
```

### Connection String Format

```
postgresql://username:password@host:port/database?sslmode=require
```

### Automated Setup Script

TMI uses GORM AutoMigrate for schema management. The migration system automatically creates all required tables based on the model definitions in `api/models/models.go`:

```bash
# From the project root directory - use Make target
make migrate-database
```

This command performs the following operations:

1. Creates the database if it doesn't exist
2. Creates all required tables with proper data types (via GORM AutoMigrate)
3. Establishes foreign key relationships
4. Creates performance indexes
5. Validates the schema after creation

For manual migration with options:

```bash
# Run migrations with config file
go run cmd/migrate/main.go --config=config-development.yml

# Validate schema only (no migrations)
go run cmd/migrate/main.go --config=config-development.yml --validate

# Skip data seeding
go run cmd/migrate/main.go --config=config-development.yml --seed=false
```

## Migration Management

### Migration Architecture

**IMPORTANT**: TMI uses GORM AutoMigrate for database schema management, NOT SQL migration files. The single source of truth for the database schema is:

- `api/models/models.go` - GORM model definitions for all tables

The migration tool (`cmd/migrate/main.go`) uses GORM's AutoMigrate feature to:
- Create tables that don't exist
- Add missing columns to existing tables
- Create indexes and constraints

### Creating New Migrations

To add new tables or modify existing schema:

1. Update the model definitions in `api/models/models.go`
2. Run migrations:
   ```bash
   make migrate-database
   ```
3. Verify the schema:
   ```bash
   make check-database
   ```

### Migration Best Practices

1. **Test migrations on a copy of production data**
2. **GORM AutoMigrate is additive** - it won't drop columns or tables
3. **Document breaking changes** in model definitions
4. **Use `make check-database`** to validate schema after changes

## Schema Validation

### Validation System Architecture

The TMI application uses a centralized schema validation system with `internal/dbschema/schema.go` as the single source of truth for expected schema.

### Components

1. **Schema Definition** (`internal/dbschema/schema.go`)

   - Central definition of all expected tables, columns, indexes, and constraints
   - Used by validation tools for consistency checking

2. **Schema Validator** (`internal/dbschema/validator.go`)

   - Validates actual database against expected schema
   - Reports missing tables and schema issues

3. **GORM Models** (`api/models/models.go`)
   - Actual schema source used by AutoMigrate
   - Defines table structures, relationships, and constraints

### Running Schema Validation

#### During Server Startup

The server can validate the schema on startup (PostgreSQL only):

```bash
go run cmd/server/main.go --config=config-development.yml
```

Validation results are logged with appropriate levels:

- **DEBUG**: Detailed validation progress
- **INFO**: Summary of validation results
- **WARN**: Non-critical issues (extra columns/indexes)
- **ERROR**: Critical issues (missing tables)

#### Manual Validation

Use the check-db tool for detailed validation:

```bash
# Using Make target (recommended)
make check-database

# Or run directly
go run cmd/check-db/main.go
```

Output includes:

- Connection status
- Table existence verification
- Row counts for each table

#### After Migrations

Validation automatically runs after applying migrations:

```bash
make migrate-database
```

### Validation Rules

1. **Tables**: Must exist with expected names
2. **Columns**: Validated by GORM model definitions
3. **Indexes**: Created by GORM based on model tags
4. **Constraints**: Defined in GORM model definitions

## Operational Commands

### Database Health Check

```bash
# Comprehensive database check using Make target
make check-database

# Quick connection test
psql -h localhost -U postgres -d tmi -c "SELECT 1"
```

### Schema Information Queries

```sql
-- List all tables
SELECT table_name
FROM information_schema.tables
WHERE table_schema = 'public'
ORDER BY table_name;

-- Show table structure
\d+ table_name

-- List all indexes
SELECT schemaname, tablename, indexname, indexdef
FROM pg_indexes
WHERE schemaname = 'public'
ORDER BY tablename, indexname;

-- Show foreign key constraints
SELECT
    tc.table_name,
    kcu.column_name,
    ccu.table_name AS foreign_table_name,
    ccu.column_name AS foreign_column_name
FROM information_schema.table_constraints AS tc
JOIN information_schema.key_column_usage AS kcu
    ON tc.constraint_name = kcu.constraint_name
JOIN information_schema.constraint_column_usage AS ccu
    ON ccu.constraint_name = tc.constraint_name
WHERE tc.constraint_type = 'FOREIGN KEY';

-- Show CHECK constraints
SELECT
    tc.table_name,
    tc.constraint_name,
    cc.check_clause
FROM information_schema.table_constraints tc
JOIN information_schema.check_constraints cc
    ON tc.constraint_name = cc.constraint_name
WHERE tc.constraint_type = 'CHECK'
ORDER BY tc.table_name;
```

### Performance Monitoring

```sql
-- Table sizes
SELECT
    schemaname,
    tablename,
    pg_size_pretty(pg_total_relation_size(schemaname||'.'||tablename)) AS size
FROM pg_tables
WHERE schemaname = 'public'
ORDER BY pg_total_relation_size(schemaname||'.'||tablename) DESC;

-- Index usage statistics
SELECT
    schemaname,
    tablename,
    indexname,
    idx_scan,
    idx_tup_read,
    idx_tup_fetch
FROM pg_stat_user_indexes
ORDER BY idx_scan DESC;

-- Slow query identification (requires pg_stat_statements extension)
SELECT
    query,
    calls,
    total_time,
    mean_time,
    max_time
FROM pg_stat_statements
WHERE mean_time > 100
ORDER BY mean_time DESC
LIMIT 20;
```

## Troubleshooting

### Common Issues and Solutions

#### Connection Issues

**Problem**: Cannot connect to PostgreSQL

```
Error: dial tcp [::1]:5432: connect: connection refused
```

**Solutions**:

1. Verify PostgreSQL is running:

   ```bash
   # Linux/macOS
   pg_ctl status
   # or
   systemctl status postgresql

   # Docker
   docker ps | grep postgres
   ```

2. Check PostgreSQL configuration:

   ```bash
   # Verify listening address
   grep listen_addresses /etc/postgresql/*/main/postgresql.conf

   # Check authentication
   cat /etc/postgresql/*/main/pg_hba.conf
   ```

3. Test connection:
   ```bash
   psql -h localhost -U postgres -d postgres
   ```

#### Permission Issues

**Problem**: Permission denied to create database

```
Error: permission denied to create database
```

**Solutions**:

1. Grant necessary privileges:

   ```sql
   -- As superuser
   ALTER USER your_user CREATEDB;

   -- Or create database as superuser
   CREATE DATABASE tmi OWNER your_user;
   ```

2. Use a privileged user for migration:
   ```bash
   POSTGRES_USER=postgres make migrate-database
   ```

#### Schema Validation Failures

**Problem**: Schema validation reports mismatches

**Solutions**:

1. Review validation output carefully
2. Run migrations to update schema
3. Verify GORM model definitions match database
4. Check for manual database modifications

### Debug Mode

Enable detailed logging:

```bash
# Set log level
export LOG_LEVEL=debug

# Run with verbose output
go run cmd/migrate/main.go --config=config-development.yml --verbose
```

## Performance Optimization

### Index Strategy

The database includes indexes optimized for common query patterns. Key indexes include:

1. **Primary Key Indexes** (Automatic via GORM)

   - Unique B-tree indexes on all primary keys

2. **Foreign Key Indexes**

   - `idx_threat_models_owner_internal_uuid`
   - `idx_threat_model_access_threat_model_id`
   - `idx_threats_threat_model_id`
   - `idx_diagrams_threat_model_id`

3. **Query Optimization Indexes**

   - `idx_users_email` - User lookup by email
   - `idx_users_last_login` - Activity tracking
   - `idx_users_provider_lookup` - OAuth provider queries
   - `idx_threats_threat_model_id_created_at` - Sorted threat listings
   - `idx_diagrams_threat_model_id_type` - Filtered diagram queries

### Query Optimization Tips

1. **Use EXPLAIN ANALYZE**:

   ```sql
   EXPLAIN ANALYZE
   SELECT * FROM threats
   WHERE threat_model_id = 'uuid-here'
   ORDER BY created_at DESC;
   ```

2. **Monitor Index Usage**:

   ```sql
   SELECT
       schemaname,
       tablename,
       indexname,
       idx_scan
   FROM pg_stat_user_indexes
   WHERE idx_scan = 0
   AND schemaname = 'public';
   ```

3. **Vacuum and Analyze**:

   ```sql
   -- Manual vacuum
   VACUUM ANALYZE;

   -- Check autovacuum status
   SELECT
       schemaname,
       tablename,
       last_vacuum,
       last_autovacuum,
       last_analyze,
       last_autoanalyze
   FROM pg_stat_user_tables;
   ```

### Connection Pooling

Configure connection pooling for production: <!-- NEEDS-REVIEW: default values not verified in source -->

```go
// In your database configuration
db.SetMaxOpenConns(25)
db.SetMaxIdleConns(5)
db.SetConnMaxLifetime(5 * time.Minute)
```

## Security Considerations

### Access Control

1. **Development Environment**:

   - Use separate database for development
   - Limit network access to localhost

2. **Production Environment**:

   - Use dedicated application user
   - Grant minimal required privileges:

     ```sql
     -- Create application user
     CREATE USER tmi_app WITH PASSWORD 'strong-password';

     -- Grant necessary privileges
     GRANT CONNECT ON DATABASE tmi TO tmi_app;
     GRANT USAGE ON SCHEMA public TO tmi_app;
     GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO tmi_app;
     GRANT USAGE ON ALL SEQUENCES IN SCHEMA public TO tmi_app;
     ```

3. **SSL/TLS Configuration**:
   - Always use `sslmode=require` in production
   - Configure certificate validation for high-security environments

### Data Protection

1. **Sensitive Data**:

   - OAuth tokens are stored with limited lifetime
   - No passwords are stored (OAuth-only authentication)
   - Email addresses are used as identifiers

2. **Audit Trail**:
   - All tables include `created_at` and `modified_at` timestamps
   - Consider implementing row-level audit logging for compliance

### Constraint Enforcement

The database enforces data integrity through CHECK constraints (defined in GORM models):

1. **Threat Model Framework**:

   ```sql
   CHECK (threat_model_framework IN ('CIA', 'STRIDE', 'LINDDUN', 'DIE', 'PLOT4ai'))
   ```

2. **Role-Based Access Control**:

   ```sql
   CHECK (role IN ('owner', 'writer', 'reader'))
   ```

3. **Risk Assessment Values**:

   ```sql
   CHECK (severity IS NULL OR severity IN ('Low', 'Medium', 'High', 'Critical', 'Unknown', 'None'))
   CHECK (likelihood IS NULL OR likelihood IN ('low', 'medium', 'high'))
   CHECK (risk_level IS NULL OR risk_level IN ('low', 'medium', 'high', 'critical'))
   ```

4. **Diagram Types**:
   ```sql
   CHECK (type IN ('DFD-1.0.0'))
   ```

## Backup and Recovery

### Backup Strategies

1. **Logical Backups** (pg_dump):

   ```bash
   # Full database backup
   pg_dump -h localhost -U postgres -d tmi -f tmi_backup_$(date +%Y%m%d_%H%M%S).sql

   # Compressed backup
   pg_dump -h localhost -U postgres -d tmi -Fc -f tmi_backup_$(date +%Y%m%d_%H%M%S).dump

   # Schema only
   pg_dump -h localhost -U postgres -d tmi --schema-only -f tmi_schema.sql
   ```

2. **Physical Backups** (pg_basebackup):

   ```bash
   # Full cluster backup
   pg_basebackup -h localhost -U postgres -D /backup/location -Ft -z -P
   ```

3. **Continuous Archiving** (WAL):
   - Configure `archive_mode` and `archive_command`
   - Use for point-in-time recovery

### Recovery Procedures

1. **From Logical Backup**:

   ```bash
   # Create new database
   createdb -h localhost -U postgres tmi_restore

   # Restore from SQL
   psql -h localhost -U postgres -d tmi_restore -f tmi_backup.sql

   # Restore from custom format
   pg_restore -h localhost -U postgres -d tmi_restore tmi_backup.dump
   ```

2. **Point-in-Time Recovery**:
   - Requires WAL archiving configuration
   - Restore base backup
   - Apply WAL files to specific timestamp

### Backup Automation

Create a backup script (`backup_tmi.sh`):

```bash
#!/bin/bash
BACKUP_DIR="/var/backups/postgresql/tmi"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
DB_NAME="tmi"
DB_USER="postgres"

# Create backup directory
mkdir -p $BACKUP_DIR

# Perform backup
pg_dump -h localhost -U $DB_USER -d $DB_NAME -Fc -f "$BACKUP_DIR/tmi_$TIMESTAMP.dump"

# Keep only last 7 days of backups
find $BACKUP_DIR -name "tmi_*.dump" -mtime +7 -delete

# Log backup completion
echo "Backup completed: tmi_$TIMESTAMP.dump"
```

Schedule with cron:

```bash
# Daily backup at 2 AM
0 2 * * * /path/to/backup_tmi.sh >> /var/log/tmi_backup.log 2>&1
```

## Monitoring and Alerting

### Key Metrics to Monitor

1. **Database Health**:

   - Connection count
   - Transaction rate
   - Query performance
   - Replication lag (if applicable)

2. **Resource Usage**:

   - CPU utilization
   - Memory usage
   - Disk I/O
   - Storage space

3. **Application Metrics**:
   - Failed authentication attempts
   - Schema validation failures
   - Migration status

### Monitoring Queries

```sql
-- Active connections
SELECT count(*) FROM pg_stat_activity;

-- Long-running queries
SELECT
    pid,
    now() - pg_stat_activity.query_start AS duration,
    query,
    state
FROM pg_stat_activity
WHERE (now() - pg_stat_activity.query_start) > interval '5 minutes';

-- Database size
SELECT
    pg_database.datname,
    pg_size_pretty(pg_database_size(pg_database.datname)) AS size
FROM pg_database
ORDER BY pg_database_size(pg_database.datname) DESC;

-- Table bloat estimation
SELECT
    schemaname,
    tablename,
    pg_size_pretty(pg_total_relation_size(schemaname||'.'||tablename)) AS total_size,
    pg_size_pretty(pg_relation_size(schemaname||'.'||tablename)) AS table_size,
    pg_size_pretty(pg_total_relation_size(schemaname||'.'||tablename) - pg_relation_size(schemaname||'.'||tablename)) AS indexes_size
FROM pg_tables
WHERE schemaname = 'public'
ORDER BY pg_total_relation_size(schemaname||'.'||tablename) DESC;
```

## Disaster Recovery Plan

### Recovery Time Objective (RTO)

Define acceptable downtime:

- Development: 4 hours
- Staging: 2 hours
- Production: 30 minutes

### Recovery Point Objective (RPO)

Define acceptable data loss:

- Development: 24 hours
- Staging: 4 hours
- Production: 15 minutes

### Recovery Procedures

1. **Database Corruption**:

   - Stop application
   - Restore from latest backup
   - Apply WAL files if available
   - Validate schema
   - Resume application

2. **Accidental Data Deletion**:

   - Identify affected data
   - Restore to temporary database
   - Extract and migrate specific data
   - Validate integrity

3. **Complete System Failure**:
   - Provision new infrastructure
   - Restore database from backup
   - Update connection strings
   - Validate application functionality

## Appendix

### Useful PostgreSQL Commands

```sql
-- Show current connections
SELECT * FROM pg_stat_activity;

-- Kill a specific connection
SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE pid = <pid>;

-- Analyze query performance (requires pg_stat_statements extension)
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
SELECT * FROM pg_stat_statements ORDER BY total_time DESC LIMIT 10;
```

### Docker Development

TMI uses Chainguard PostgreSQL images for enhanced security:

```bash
# Start PostgreSQL container using Make
make start-database

# Stop PostgreSQL container
make stop-database

# Reset database (DESTRUCTIVE)
make reset-database
```

### References

- [PostgreSQL Documentation](https://www.postgresql.org/docs/) <!-- External link - not verified -->
- [PostgreSQL Performance Tuning](https://wiki.postgresql.org/wiki/Performance_Optimization) <!-- External link - not verified -->
- [PostgreSQL Security Best Practices](https://www.postgresql.org/docs/current/security.html) <!-- External link - not verified -->
- [GORM Documentation](https://gorm.io/docs/)
