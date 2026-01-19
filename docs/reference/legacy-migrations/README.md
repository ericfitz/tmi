# Legacy SQL Migrations

This directory contains the original PostgreSQL SQL migrations that were used before TMI migrated to GORM-only schema management.

## History

TMI originally used [golang-migrate](https://github.com/golang-migrate/migrate) with PostgreSQL-specific SQL migration files. When multi-database support was added (PostgreSQL, Oracle, MySQL, SQL Server, SQLite), the architecture was changed to use GORM AutoMigrate for all databases, with `api/models/models.go` as the single source of truth for schema definitions.

## Current Schema Management

As of version 0.263.0, TMI uses:

1. **Schema Definition**: `api/models/models.go` - All 24 GORM model structs
2. **Validation**: `api/validation/validators.go` - Business rules (replaces CHECK constraints)
3. **Seed Data**: `api/seed/seed.go` - Required initial data (everyone group, webhook deny list)
4. **Migration**: GORM AutoMigrate for all supported databases

## Why Keep These Files?

These files are preserved for:

1. **Reference**: Understanding the original PostgreSQL-specific schema design
2. **Documentation**: Viewing what triggers, partial indexes, and CHECK constraints were used
3. **Troubleshooting**: Comparing GORM-generated schema with the original design

## PostgreSQL-Specific Features (No Longer Used)

The original SQL migrations included features not portable to other databases:

- Partial indexes (e.g., `WHERE deleted_at IS NULL`)
- GIN indexes for JSONB columns
- Covering indexes (INCLUDE clause)
- PostgreSQL triggers for `modified_at` timestamps
- CHECK constraints for enum validation
- Native UUID type (GORM uses `varchar(36)` for Oracle compatibility)

## Do Not Use

These migration files are **not executed** by the current codebase. They are for reference only.

To manage the database schema, use:

```bash
# Run migrations and seed data
./bin/tmiserver --config=config-development.yml

# Or use the migrate CLI
go run cmd/migrate/main.go --config=config-development.yml
```
