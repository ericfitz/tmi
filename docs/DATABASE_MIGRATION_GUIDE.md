# Database Migration Guide for TMI Granular API

This guide provides detailed instructions for migrating the TMI database to support the granular API enhancements with Redis caching integration.

## Overview

The granular API enhancement introduces:
- Enhanced sub-resource tables (documents, sources, metadata)
- Improved indexes for performance optimization
- Support for polymorphic metadata associations
- Redis caching key structures

## Migration Timeline

**Estimated Total Time**: 30-60 minutes (depending on data volume)
**Downtime Required**: 5-10 minutes for index creation
**Rollback Time**: 10-15 minutes if needed

## Prerequisites

### System Requirements
- PostgreSQL 12+ with sufficient disk space (25% additional free space recommended)
- Redis 6+ for caching support
- Database backup tools available
- Migration tool (golang-migrate or similar)

### Pre-Migration Checklist
- [ ] Full database backup completed
- [ ] Redis instance configured and accessible
- [ ] Application maintenance mode enabled
- [ ] Database connection pool reduced to minimum
- [ ] Monitoring alerts configured
- [ ] Rollback scripts prepared

## Migration Steps

### Step 1: Pre-Migration Backup

```bash
# Create full database backup
pg_dump -h $DB_HOST -U $DB_USER -d $DB_NAME > tmi_backup_$(date +%Y%m%d_%H%M%S).sql

# Verify backup integrity
psql -h $DB_HOST -U $DB_USER -d tmi_test < tmi_backup_$(date +%Y%m%d_%H%M%S).sql

# Create Redis backup (if existing data)
redis-cli --rdb redis_backup_$(date +%Y%m%d_%H%M%S).rdb
```

### Step 2: Database Schema Validation

```bash
# Verify current schema state
psql -h $DB_HOST -U $DB_USER -d $DB_NAME -c "
SELECT schemaname, tablename, attname, typname
FROM pg_catalog.pg_attribute a
JOIN pg_catalog.pg_class c ON a.attrelid = c.oid
JOIN pg_catalog.pg_namespace n ON c.relnamespace = n.oid
JOIN pg_catalog.pg_type t ON a.atttypid = t.oid
WHERE n.nspname = 'public'
AND c.relname IN ('threats', 'documents', 'sources', 'metadata')
AND a.attnum > 0
ORDER BY c.relname, a.attnum;
"

# Check existing indexes
psql -h $DB_HOST -U $DB_USER -d $DB_NAME -c "
SELECT schemaname, tablename, indexname, indexdef
FROM pg_indexes
WHERE schemaname = 'public'
AND tablename IN ('threats', 'documents', 'sources', 'metadata', 'threat_models', 'threat_model_access')
ORDER BY tablename, indexname;
"
```

### Step 3: Apply Database Migrations

The migrations should be applied in sequence:

#### Migration 000016: Performance Indexes
```bash
# Apply performance optimization indexes
migrate -path ./auth/migrations -database $DATABASE_URL up 000016

# Verify index creation
psql -h $DB_HOST -U $DB_USER -d $DB_NAME -c "
SELECT schemaname, tablename, indexname, indexdef
FROM pg_indexes
WHERE indexname LIKE 'idx_%'
AND schemaname = 'public'
ORDER BY tablename, indexname;
"
```

#### Migration 000017: Enhanced Metadata Table
```bash
# Apply metadata table enhancements
migrate -path ./auth/migrations -database $DATABASE_URL up 000017

# Verify metadata table schema
psql -h $DB_HOST -U $DB_USER -d $DB_NAME -c "
SELECT column_name, data_type, character_maximum_length, is_nullable
FROM information_schema.columns
WHERE table_name = 'metadata'
AND table_schema = 'public'
ORDER BY ordinal_position;
"

# Verify constraints
psql -h $DB_HOST -U $DB_USER -d $DB_NAME -c "
SELECT conname, contype, pg_get_constraintdef(oid)
FROM pg_constraint
WHERE conrelid = 'metadata'::regclass
ORDER BY conname;
"
```

### Step 4: Data Validation

```bash
# Verify data integrity after migration
psql -h $DB_HOST -U $DB_USER -d $DB_NAME -c "
-- Check threat model relationships
SELECT COUNT(*) as threat_models FROM threat_models;
SELECT COUNT(*) as threats FROM threats;
SELECT COUNT(*) as documents FROM documents;
SELECT COUNT(*) as sources FROM sources;
SELECT COUNT(*) as metadata FROM metadata;

-- Verify foreign key relationships
SELECT COUNT(*) as orphaned_threats
FROM threats t
LEFT JOIN threat_models tm ON t.threat_model_id = tm.id
WHERE tm.id IS NULL;

SELECT COUNT(*) as orphaned_documents
FROM documents d
LEFT JOIN threat_models tm ON d.threat_model_id = tm.id
WHERE tm.id IS NULL;

SELECT COUNT(*) as orphaned_sources
FROM sources s
LEFT JOIN threat_models tm ON s.threat_model_id = tm.id
WHERE tm.id IS NULL;
"

# Verify metadata entity type constraints
psql -h $DB_HOST -U $DB_USER -d $DB_NAME -c "
SELECT entity_type, COUNT(*) as count
FROM metadata
GROUP BY entity_type
ORDER BY entity_type;
"
```

### Step 5: Redis Cache Setup

```bash
# Verify Redis connectivity
redis-cli ping

# Configure Redis memory policy
redis-cli config set maxmemory-policy allkeys-lru

# Set up Redis key expiration monitoring
redis-cli config set notify-keyspace-events Ex

# Test cache key structure
redis-cli set "cache:test:migration" "success" EX 60
redis-cli get "cache:test:migration"
redis-cli del "cache:test:migration"
```

### Step 6: Application Configuration

Update application configuration to enable Redis caching:

```yaml
# config.yaml
database:
  host: $DB_HOST
  port: $DB_PORT
  name: $DB_NAME
  user: $DB_USER
  password: $DB_PASSWORD

redis:
  host: $REDIS_HOST
  port: $REDIS_PORT
  password: $REDIS_PASSWORD
  db: 0
  
cache:
  enabled: true
  default_ttl: 300s
  threat_model_ttl: 600s
  auth_data_ttl: 900s
```

### Step 7: Post-Migration Testing

```bash
# Run application health checks
curl -f http://localhost:8080/health

# Test database connections
curl -f http://localhost:8080/ready

# Run backward compatibility tests
make test-compatibility

# Test new granular API endpoints
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/threat_models/$THREAT_MODEL_ID/threats

curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/threat_models/$THREAT_MODEL_ID/documents

curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/threat_models/$THREAT_MODEL_ID/sources
```

## Performance Validation

### Database Performance Tests

```sql
-- Test query performance with new indexes
EXPLAIN ANALYZE SELECT * FROM threats
WHERE threat_model_id = $1 ORDER BY created_at DESC LIMIT 10;

EXPLAIN ANALYZE SELECT * FROM metadata
WHERE entity_type = 'threat' AND entity_id = $1;

EXPLAIN ANALYZE SELECT tm.*, tma.role
FROM threat_models tm
JOIN threat_model_access tma ON tm.id = tma.threat_model_id
WHERE tma.user_email = $1;
```

### Cache Performance Tests

```bash
# Test cache hit ratios
redis-cli info stats | grep keyspace_hits
redis-cli info stats | grep keyspace_misses

# Monitor memory usage
redis-cli info memory | grep used_memory_human

# Test cache warming
curl -X POST -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/admin/cache/warm
```

## Rollback Procedures

### Emergency Rollback (if critical issues occur)

```bash
# 1. Enable maintenance mode
curl -X POST http://localhost:8080/admin/maintenance/enable

# 2. Stop application
systemctl stop tmi-server

# 3. Restore database from backup
dropdb -h $DB_HOST -U $DB_USER $DB_NAME
createdb -h $DB_HOST -U $DB_USER $DB_NAME
psql -h $DB_HOST -U $DB_USER -d $DB_NAME < tmi_backup_$(date +%Y%m%d_%H%M%S).sql

# 4. Clear Redis cache
redis-cli flushdb

# 5. Deploy previous application version
git checkout previous-version
make build && make deploy

# 6. Disable maintenance mode
curl -X POST http://localhost:8080/admin/maintenance/disable
```

### Planned Rollback (during maintenance window)

```bash
# 1. Run down migrations in reverse order
migrate -path ./auth/migrations -database $DATABASE_URL down 000017
migrate -path ./auth/migrations -database $DATABASE_URL down 000016

# 2. Verify schema rollback
psql -h $DB_HOST -U $DB_USER -d $DB_NAME -c "
SELECT schemaname, tablename, indexname
FROM pg_indexes
WHERE indexname LIKE 'idx_%granular%'
AND schemaname = 'public';
"

# 3. Clear Redis cache
redis-cli flushdb

# 4. Deploy previous application version
git checkout previous-version
make build && make deploy
```

## Monitoring and Alerts

### Database Monitoring

```sql
-- Monitor query performance
SELECT query, mean_time, calls, total_time
FROM pg_stat_statements
WHERE query LIKE '%threats%' OR query LIKE '%documents%' OR query LIKE '%sources%'
ORDER BY mean_time DESC LIMIT 10;

-- Monitor index usage
SELECT schemaname, tablename, indexname, idx_scan, idx_tup_read, idx_tup_fetch
FROM pg_stat_user_indexes
WHERE schemaname = 'public'
ORDER BY idx_scan DESC;

-- Monitor table sizes
SELECT schemaname, tablename,
  pg_size_pretty(pg_total_relation_size(schemaname||'.'||tablename)) as size
FROM pg_tables
WHERE schemaname = 'public'
ORDER BY pg_total_relation_size(schemaname||'.'||tablename) DESC;
```

### Redis Monitoring

```bash
# Set up Redis monitoring commands
redis-cli monitor > redis_monitor.log &

# Check Redis performance
redis-cli --latency-history -i 1

# Monitor memory usage trends
watch -n 5 'redis-cli info memory | grep used_memory_human'
```

### Application Monitoring

```bash
# Monitor application logs for cache-related errors
tail -f /var/log/tmi/application.log | grep -E "(cache|redis|error)"

# Monitor API response times
curl -w "@curl-format.txt" -s -o /dev/null \
  http://localhost:8080/threat_models/$THREAT_MODEL_ID/threats

# Monitor cache hit ratios via application metrics
curl http://localhost:8080/metrics | grep cache_hit_ratio
```

## Troubleshooting

### Common Issues

#### Migration Fails on Index Creation
```bash
# Check for conflicting indexes
psql -h $DB_HOST -U $DB_USER -d $DB_NAME -c "
SELECT indexname, indexdef FROM pg_indexes
WHERE tablename = 'threats' AND indexname LIKE 'idx_%';
"

# Drop conflicting indexes manually if needed
psql -h $DB_HOST -U $DB_USER -d $DB_NAME -c "
DROP INDEX IF EXISTS conflicting_index_name;
"
```

#### Redis Connection Issues
```bash
# Test Redis connectivity
redis-cli -h $REDIS_HOST -p $REDIS_PORT ping

# Check Redis logs
tail -f /var/log/redis/redis-server.log

# Verify Redis configuration
redis-cli config get "*"
```

#### Performance Degradation
```bash
# Check for missing indexes
psql -h $DB_HOST -U $DB_USER -d $DB_NAME -c "
SELECT schemaname, tablename, attname, n_distinct, correlation
FROM pg_stats
WHERE tablename IN ('threats', 'documents', 'sources')
AND n_distinct < -0.1
ORDER BY tablename, attname;
"

# Analyze table statistics
psql -h $DB_HOST -U $DB_USER -d $DB_NAME -c "
ANALYZE threats;
ANALYZE documents;
ANALYZE sources;
ANALYZE metadata;
"
```

### Recovery Procedures

#### Corrupted Cache Recovery
```bash
# Clear all cache data
redis-cli flushdb

# Restart application to trigger cache warming
systemctl restart tmi-server

# Monitor cache population
watch -n 10 'redis-cli info keyspace'
```

#### Database Consistency Issues
```bash
# Check and repair foreign key violations
psql -h $DB_HOST -U $DB_USER -d $DB_NAME -c "
-- Find orphaned records
SELECT 'threats' as table_name, COUNT(*) as orphaned_count
FROM threats t LEFT JOIN threat_models tm ON t.threat_model_id = tm.id
WHERE tm.id IS NULL
UNION ALL
SELECT 'documents', COUNT(*)
FROM documents d LEFT JOIN threat_models tm ON d.threat_model_id = tm.id
WHERE tm.id IS NULL
UNION ALL
SELECT 'sources', COUNT(*)
FROM sources s LEFT JOIN threat_models tm ON s.threat_model_id = tm.id
WHERE tm.id IS NULL;
"

# Clean up orphaned records if found
# (Create specific cleanup scripts based on findings)
```

## Success Criteria

The migration is considered successful when:

- [ ] All database migrations applied successfully
- [ ] No data loss or corruption detected
- [ ] All existing API endpoints return expected responses
- [ ] New granular API endpoints are functional
- [ ] Cache hit ratio > 70% after 1 hour of operation
- [ ] Average API response time < 200ms
- [ ] No increase in error rates
- [ ] All backward compatibility tests pass
- [ ] Redis memory usage within expected limits

## Support Contacts

- **Database Issues**: DBA Team - dba@company.com
- **Redis Issues**: Infrastructure Team - infra@company.com
- **Application Issues**: Development Team - dev@company.com
- **Emergency Escalation**: On-call Engineer - oncall@company.com

## Additional Resources

- [TMI API Documentation](TMI-API-v1_0.md)
- [Redis Caching Strategy](redis.md)
- [Performance Benchmarking Guide](performance-benchmarks.md)
- [Monitoring Setup Guide](monitoring-setup.md)