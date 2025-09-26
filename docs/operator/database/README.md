# Database Operations & Management

This directory contains PostgreSQL and Redis operations, schema management, and performance tuning documentation.

## Files in this Directory

### [postgresql-operations.md](postgresql-operations.md)
**Complete PostgreSQL operations guide** covering deployment, maintenance, and troubleshooting.

**Content includes:**
- PostgreSQL installation and configuration
- Database user and permission management
- Migration management and schema validation
- Performance optimization and query tuning
- Backup and recovery procedures
- Replication setup for high availability
- Connection pooling and resource management
- Monitoring and alerting setup
- Troubleshooting common database issues
- Operational commands and procedures

**Key Operations Covered:**
- Database creation and initialization
- User management and role-based access
- Automated backup scheduling
- Point-in-time recovery procedures
- Performance monitoring and tuning
- Index optimization and maintenance

### [postgresql-schema.md](postgresql-schema.md)
**Detailed database schema documentation** with comprehensive entity relationships and design patterns.

**Content includes:**
- Complete table definitions and relationships
- Entity-relationship diagrams
- Database migration history and evolution
- Constraint definitions and data integrity rules
- Indexing strategy and performance considerations
- Data types and column specifications
- Foreign key relationships and referential integrity
- Design patterns and architectural decisions
- Schema versioning and migration procedures

**Schema Components:**
- User management and authentication tables
- Threat model and diagram storage
- OAuth and session management
- Audit and logging tables
- Collaboration and real-time data structures

### [redis-schema.md](redis-schema.md)
**Redis key patterns, data structures, and caching strategies** for TMI.

**Content includes:**
- Redis key naming conventions and patterns
- Data structure usage (strings, hashes, sets, lists)
- Caching strategies and TTL policies
- Session management with Redis
- Real-time collaboration coordination
- Performance monitoring and optimization
- Memory management and eviction policies
- Security considerations and access controls
- Backup and persistence configuration
- High availability and clustering setup

**Use Cases Covered:**
- JWT session storage and validation
- WebSocket connection coordination
- Real-time collaboration state management
- Caching frequently accessed data
- Rate limiting and throttling

## Database Architecture

### Data Storage Architecture
```
[TMI Application]
        ↓
[Connection Pool]
        ↓
[PostgreSQL Primary] ← [Read Replicas]
        ↓
[Persistent Storage]

[Redis Cache] ← [TMI Application]
        ↓
[In-Memory Storage]
```

### Key Components
- **PostgreSQL Primary**: Main transactional database
- **Read Replicas**: Optional read-only replicas for scalability
- **Connection Pool**: Managed database connections
- **Redis Cache**: Session storage and real-time coordination
- **Backup System**: Automated backup and recovery

## PostgreSQL Management

### Database Operations
- **Installation**: Platform-specific installation procedures
- **Configuration**: Performance tuning and security hardening
- **User Management**: Role-based access control implementation
- **Schema Management**: Migration execution and validation
- **Backup Operations**: Automated backup scheduling and testing
- **Performance Tuning**: Query optimization and index management

### Schema Management
- **Migrations**: Database schema evolution and versioning
- **Constraints**: Data integrity and validation rules
- **Indexes**: Performance optimization strategies
- **Relationships**: Foreign key design and referential integrity
- **Partitioning**: Table partitioning for large datasets

### Performance Optimization
- **Query Performance**: SQL query optimization and analysis
- **Index Strategy**: Optimal indexing for common query patterns
- **Connection Management**: Connection pooling configuration
- **Resource Allocation**: Memory and CPU optimization
- **Monitoring**: Performance metrics and alerting

## Redis Management

### Cache Operations
- **Configuration**: Memory allocation and persistence settings
- **Key Management**: Naming conventions and organization
- **Data Structures**: Optimal data structure selection
- **Expiration**: TTL policies and cache invalidation
- **Monitoring**: Memory usage and performance tracking

### Session Management
- **JWT Storage**: Secure token storage and validation
- **User Sessions**: Session lifecycle management
- **Rate Limiting**: Request throttling and abuse prevention
- **Real-time State**: WebSocket connection coordination

### High Availability
- **Clustering**: Redis cluster setup and management
- **Replication**: Master-replica configuration
- **Failover**: Automated failover procedures
- **Backup**: Data persistence and recovery

## Database Security

### PostgreSQL Security
- **Authentication**: User authentication and password policies
- **Authorization**: Role-based access control (RBAC)
- **Network Security**: SSL/TLS encryption and network isolation
- **Audit Logging**: Database activity monitoring
- **Data Encryption**: Encryption at rest and in transit

### Redis Security
- **Authentication**: Password-based authentication
- **Network Security**: Bind address configuration and firewalling
- **Command Security**: Dangerous command restrictions
- **Data Security**: Sensitive data handling in cache
- **Access Control**: Client connection management

## Monitoring & Alerting

### PostgreSQL Monitoring
- **Performance Metrics**: Query performance and resource usage
- **Connection Monitoring**: Active connections and pool status
- **Replication Status**: Primary-replica synchronization
- **Storage Monitoring**: Disk usage and growth trends
- **Alert Conditions**: Performance degradation and failures

### Redis Monitoring
- **Memory Usage**: Memory consumption and eviction rates
- **Performance Metrics**: Command latency and throughput
- **Connection Monitoring**: Client connections and usage
- **Persistence Status**: Data persistence and backup status
- **Alert Conditions**: Memory pressure and connectivity issues

## Backup & Recovery

### PostgreSQL Backup
- **Full Backups**: Complete database backups
- **Incremental Backups**: Point-in-time recovery capability
- **Backup Validation**: Backup integrity verification
- **Restoration Procedures**: Recovery from backup files
- **Cross-Region Backups**: Disaster recovery preparation

### Redis Backup
- **Snapshot Backups**: RDB snapshot creation
- **AOF Persistence**: Append-only file persistence
- **Backup Scheduling**: Automated backup procedures
- **Recovery Testing**: Backup restoration validation
- **Data Migration**: Moving data between Redis instances

## Performance Tuning

### PostgreSQL Tuning
- **Configuration Parameters**: Memory, CPU, and I/O optimization
- **Query Optimization**: SQL performance improvement
- **Index Optimization**: Strategic index creation and maintenance
- **Vacuum and Analyze**: Table maintenance procedures
- **Connection Tuning**: Optimal connection pool sizing

### Redis Tuning
- **Memory Configuration**: Optimal memory allocation
- **Persistence Tuning**: RDB and AOF optimization
- **Network Tuning**: Connection and timeout optimization
- **Data Structure Optimization**: Efficient data organization
- **Eviction Policy**: Memory management strategies

## Troubleshooting

### Common PostgreSQL Issues
- **Connection Problems**: Connection pool exhaustion and network issues
- **Performance Issues**: Slow queries and resource contention
- **Replication Problems**: Primary-replica synchronization issues
- **Storage Issues**: Disk space and I/O performance problems
- **Migration Failures**: Schema migration troubleshooting

### Common Redis Issues  
- **Memory Issues**: Memory pressure and eviction problems
- **Performance Problems**: High latency and throughput issues
- **Connection Issues**: Client connection and timeout problems
- **Persistence Problems**: Data persistence and backup failures
- **Clustering Issues**: Redis cluster coordination problems

## Related Documentation

### Deployment and Operations
- [Deployment Guide](../deployment/deployment-guide.md) - Production deployment
- [Container Security](../deployment/container-security.md) - Secure containerization

### Development and Testing
- [Development Setup](../../developer/setup/development-setup.md) - Local database setup
- [Integration Testing](../../developer/testing/integration-testing.md) - Database testing

### Monitoring and Maintenance
- Database monitoring setup and procedures

## Quick Reference Commands

### PostgreSQL Commands
```bash
# Database backup
pg_dump -h host -U user -d tmi > backup.sql

# Database restore  
psql -h host -U user -d tmi < backup.sql

# Check database status
psql -h host -U user -d tmi -c "\dt"

# Performance analysis
psql -h host -U user -d tmi -c "SELECT * FROM pg_stat_activity"
```

### Redis Commands
```bash
# Redis health check
redis-cli -h host -p 6379 ping

# Memory usage
redis-cli -h host -p 6379 info memory

# Key analysis
redis-cli -h host -p 6379 --scan --pattern "session:*"

# Performance monitoring
redis-cli -h host -p 6379 --latency-history
```

For detailed database administration procedures and troubleshooting guides, see the individual documentation files in this directory.