---
name: db
description: Interact with the TMI PostgreSQL database for queries and administration. Use when asked to show data, check tables, or run SQL queries.
allowed-tools: Bash, Read
---

# Database Interaction Skill

You are helping the user interact with the TMI PostgreSQL database.

## Database Connection Information

**IMPORTANT**: All database connection information and credentials are stored in `config-development.yml`:

- **Host**: localhost (from host machine perspective)
- **Port**: 5432
- **User**: tmi_dev
- **Password**: dev123
- **Database**: tmi_dev
- **Container Name**: tmi-postgresql

## Tool Requirements

**PostgreSQL command-line tools (psql) are NOT installed on the host machine.**

You MUST use `docker exec` to run `psql` commands inside the `tmi-postgresql` container.

## Standard Database Operations

### Interactive psql Session

To start an interactive psql session:

```bash
docker exec -it tmi-postgresql psql -U tmi_dev -d tmi_dev
```

### Execute Single SQL Query

To run a single SQL query:

```bash
docker exec tmi-postgresql psql -U tmi_dev -d tmi_dev -c "YOUR SQL QUERY HERE"
```

### Execute SQL from File

To execute SQL from a file:

```bash
docker exec -i tmi-postgresql psql -U tmi_dev -d tmi_dev < /path/to/file.sql
```

Or using heredoc:

```bash
docker exec -i tmi-postgresql psql -U tmi_dev -d tmi_dev <<'EOF'
YOUR SQL QUERY HERE
EOF
```

## Common Database Tasks

### List All Tables

```bash
docker exec tmi-postgresql psql -U tmi_dev -d tmi_dev -c "\dt"
```

### Describe Table Schema

```bash
docker exec tmi-postgresql psql -U tmi_dev -d tmi_dev -c "\d table_name"
```

### Count Records

```bash
docker exec tmi-postgresql psql -U tmi_dev -d tmi_dev -c "SELECT COUNT(*) FROM table_name;"
```

### View Table Data

```bash
docker exec tmi-postgresql psql -U tmi_dev -d tmi_dev -c "SELECT * FROM table_name LIMIT 10;"
```

### Schema Migrations

The database schema is managed by GORM `AutoMigrate()`, driven by the struct tags
in `api/models/*.go` — there are no standalone SQL migration files. The schema is
applied automatically on server startup (`make dev-up`). Legacy SQL migrations
are archived under `docs/reference/legacy-migrations/` for historical reference only.

### Database Reset

To reset the local dev database (drop and recreate schema):

```bash
make reset-database  # Drop and recreate the local dev schema
```

For the Heroku database, use `make reset-db-heroku` (DESTRUCTIVE).

### Clear Generated Test Data (without dropping the database)

To clear automatically generated test data (test users with `@tmi.local`
emails, test groups, and CATS-seeded artifacts) out of the development database
without dropping and recreating it:

```bash
make test-db-cleanup
```

Note: this runs `scripts/delete-test-users.py`, which deletes via the admin API
(not direct SQL), so the **TMI server must be running** and the
`charlie@tmi.local` admin account must exist. It cascades related data and
preserves `charlie@tmi.local`.

## TMI Database Schema

Key tables in the TMI database:

- `users` - User accounts and authentication
- `threat_models` - Top-level threat model entities
- `diagrams` - Threat model diagrams (DFD, etc.)
- `cells` - Diagram cells (nodes and edges)
- `threats` - Identified threats
- `documents` - Document attachments
- `repositories` - Code repository links
- `notes` - Text notes
- `assets` - Asset inventory items
- `metadata` - Flexible key-value metadata for entities

## Best Practices

1. **Always use parameterized queries** when dealing with user input to prevent SQL injection
2. **Use transactions** for multi-statement operations
3. **Check container status** before executing commands:
   ```bash
   docker ps --filter "name=tmi-postgresql"
   ```
4. **Quote SQL properly** - use single quotes for SQL string literals, escape special characters
5. **Use heredoc for multi-line SQL** to avoid shell quoting issues:
   ```bash
   docker exec -i tmi-postgresql psql -U tmi_dev -d tmi_dev <<'EOF'
   SELECT * FROM table_name WHERE condition = 'value';
   EOF
   ```

## Error Handling

If you encounter errors:

1. **Container not running**: Start just the database container with `make start-database` (this is all the psql commands above need). `make dev-up` also starts it, but additionally brings up the full kind dev environment (cluster + server + Redis) — use that only if you want the whole stack.
2. **Connection refused**: Check if the container is healthy: `docker ps`
3. **Authentication failed**: Verify credentials in `config-development.yml`
4. **Database does not exist**: Run migrations with `make migrate-database`

## Security Notes

- The credentials in `config-development.yml` are for **local development only**
- Never commit real production credentials to the repository
- The dev password (`dev123`) is intentionally simple for local development

## Examples

### Query threat models with their owners

```bash
docker exec tmi-postgresql psql -U tmi_dev -d tmi_dev -c "
SELECT tm.id, tm.name, u.email AS owner, tm.created_at
FROM threat_models tm
JOIN users u ON u.internal_uuid = tm.owner_internal_uuid
ORDER BY tm.created_at DESC
LIMIT 5;
"
```

### Find all assets of type 'software'

```bash
docker exec tmi-postgresql psql -U tmi_dev -d tmi_dev -c "
SELECT a.id, a.name, a.type, tm.name as threat_model
FROM assets a
JOIN threat_models tm ON a.threat_model_id = tm.id
WHERE a.type = 'software';
"
```

### Check applied schema version

The schema is managed by GORM `AutoMigrate()` (see "Schema Migrations" above),
so there is no per-migration history table. The current schema state is recorded
as a single-row fingerprint stamp in `tmi_schema_versions` (`id`, a SHA-256
`fingerprint` of the GORM model set, and `applied_at`):

```bash
docker exec tmi-postgresql psql -U tmi_dev -d tmi_dev -c "
SELECT id, fingerprint, applied_at
FROM tmi_schema_versions;
"
```

(The `schema_migrations` table, columns `version`/`dirty`, is a vestigial
golang-migrate artifact and is not used by GORM AutoMigrate.)

## Integration with Claude Code

When the user asks to:

- **"Show me..."** - Use SELECT queries
- **"Add/Create..."** - Use INSERT queries (but ask for confirmation first)
- **"Update/Modify..."** - Use UPDATE queries (but ask for confirmation first)
- **"Delete/Remove..."** - Use DELETE queries (but ALWAYS ask for confirmation first)
- **"Reset/Clear..."** - Suggest using `make reset-database` (local), `make test-db-cleanup` (clear test data only), or specific DELETE queries

Always show the user the SQL query you're about to execute before running it, especially for INSERT, UPDATE, or DELETE operations.
