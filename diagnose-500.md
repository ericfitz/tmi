# Diagnosing 500 Error on POST /threat_models

## Issue
Client receives 500 Internal Server Error when creating a threat model after successful Google OAuth authentication.

## Error Details
```
POST https://api.tmi.dev/threat_models 500 (Internal Server Error)
User: hobobarbarian@gmail.com
```

## Root Cause Analysis

The threat model creation fails at one of these points in the database transaction:

1. **INSERT INTO threat_models** (line 421-432 in database_store.go)
   - Requires valid `owner_email` (foreign key to users.email)
   - Requires valid `created_by` (foreign key constraint on line 13 of migration)

2. **INSERT INTO threat_model_access** (line 891-897 in database_store.go via saveAuthorizationTx)
   - Requires valid `user_email` (foreign key to users.email on line 65 of migration)

## Diagnostic SQL Queries

Run these on the production database to diagnose the issue:

```sql
-- Check if user exists
SELECT id, email, name, created_at, last_login
FROM users
WHERE email = 'hobobarbarian@gmail.com';

-- Check recent threat models (to see if any were created)
SELECT id, name, owner_email, created_at
FROM threat_models
ORDER BY created_at DESC
LIMIT 10;

-- Check for constraint violations in logs
SELECT * FROM pg_stat_activity
WHERE state = 'idle in transaction'
OR wait_event IS NOT NULL;

-- Check database logs for recent errors
-- (command depends on your PostgreSQL setup)
```

## Possible Causes

1. **User doesn't exist in database** - OAuth authentication succeeded but user creation failed
2. **Email mismatch** - JWT contains different email than what's in database
3. **Database migration not applied** - Production database schema is outdated
4. **Transaction deadlock** - Multiple concurrent requests causing locks
5. **Database connection issue** - Connection pool exhausted or stale connections

## Next Steps

1. Access production database and run diagnostic queries
2. Check server logs for detailed error message (the handler logs the actual error on line 214)
3. Verify database migrations are up to date
4. Check if other users can create threat models (to isolate if it's user-specific)

## Code References

- Threat model creation: [api/threat_model_handlers.go:142-243](api/threat_model_handlers.go#L142-L243)
- Database store: [api/database_store.go:392-452](api/database_store.go#L392-L452)
- Foreign key constraints: [auth/migrations/002_business_domain.up.sql:13,65](auth/migrations/002_business_domain.up.sql#L13-L65)
