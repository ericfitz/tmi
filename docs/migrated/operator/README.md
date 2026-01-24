# Operations Documentation

<!-- Migrated from: docs/operator/README.md on 2025-01-24 -->

This directory contains deployment, operations, and troubleshooting guidance for running TMI in production environments.

## Purpose

Comprehensive operations documentation covering production deployment, database management, monitoring, and troubleshooting for TMI server infrastructure.

## Directory Structure

### [deployment/](deployment/) - Deployment & Infrastructure
Production deployment guides, container security, and infrastructure setup.

### [database/](database/) - Database Operations & Management
PostgreSQL and Redis operations, schema management, and performance tuning.

<!-- NEEDS-REVIEW: monitoring/ directory does not exist. Content migrated to wiki at Monitoring-and-Health page. -->

### [addons/](addons/) - Addon Configuration
Configuration and management of TMI addons and extensions.

## Files in this Directory

<!-- NEEDS-REVIEW: heroku-database-reset.md does not exist at docs/operator/heroku-database-reset.md. Content migrated to wiki at Database-Operations page. -->

<!-- NEEDS-REVIEW: oauth-environment-configuration.md does not exist. Content migrated to wiki at Setting-Up-Authentication page. -->

<!-- NEEDS-REVIEW: webhook-configuration.md does not exist. Content migrated to wiki at Webhook-Integration page. -->

## Getting Started

1. **Start Here**: [deployment/deployment-guide.md](deployment/deployment-guide.md)
2. **Security**: [deployment/container-security.md](deployment/container-security.md)
3. **Database Setup**: [database/postgresql-operations.md](database/postgresql-operations.md)
4. **Monitoring**: See wiki page [Monitoring-and-Health](https://github.com/ericfitz/tmi/wiki/Monitoring-and-Health)

## Operations Overview

### TMI Production Architecture
```
[Load Balancer] -> [TMI Server Instances] -> [PostgreSQL Primary]
                                          -> [Redis Cluster]
                                          -> [Monitoring Stack]
```

### Key Components
- **TMI Server**: Go application with HTTP/WebSocket endpoints
- **PostgreSQL**: Primary data storage with replication
- **Redis**: Session storage and real-time coordination
- **Load Balancer**: Traffic distribution and SSL termination
- **Monitoring**: Observability and alerting infrastructure

## Quick Reference

### Essential Operations Commands

#### Deployment Commands
```bash
# Build production binary
make build-server

# Build secure containers
make build-containers

# Deploy with systemd
systemctl start tmi
systemctl status tmi
```

#### Database Operations
```bash
# Database backup
pg_dump -h host -U user -d tmi > backup.sql

# Database restore
psql -h host -U user -d tmi < backup.sql

# Check database status
psql -h host -U user -d tmi -c "\dt"
```

#### Monitoring & Health Checks
```bash
# Server health check (root endpoint)
curl https://tmi.example.com/

# Database connectivity
psql -h host -U user -d tmi -c "SELECT 1"

# Redis connectivity
redis-cli -h host -p 6379 ping
```

## Documentation by Category

### Deployment & Infrastructure
- [Deployment Guide](deployment/deployment-guide.md) - Complete production deployment
- [Container Security](deployment/container-security.md) - Secure containerization

### Database Operations
- [PostgreSQL Operations](database/postgresql-operations.md) - Database administration

<!-- NEEDS-REVIEW: postgresql-schema.md and redis-schema.md do not exist. Content available in wiki at Database-Schema-Reference page. -->

### Monitoring & Observability
- See wiki page [Monitoring-and-Health](https://github.com/ericfitz/tmi/wiki/Monitoring-and-Health) for monitoring setup

## Production Deployment Options

### 1. Traditional Server Deployment
- **Systemd service** with dedicated user account
- **Reverse proxy** (Nginx/Apache) with SSL termination
- **Database servers** on separate infrastructure
- **Log aggregation** and monitoring integration

### 2. Container Deployment (Docker)
- **Docker Compose** for simple deployments
- **Container orchestration** with proper networking
- **Volume management** for persistent data
- **Security hardening** with Chainguard images

### 3. Kubernetes Deployment
- **Helm charts** for repeatable deployments
- **Horizontal pod autoscaling** for load management
- **Service mesh** integration for advanced networking
- **GitOps workflows** for deployment automation

## Security Considerations

### Infrastructure Security
- **TLS/SSL encryption** for all communications
- **Network isolation** with firewalls and VPNs
- **Access controls** with least-privilege principles
- **Regular security updates** and vulnerability scanning

### Application Security
- **JWT token security** with proper secret management
- **OAuth configuration** with secure redirect URIs
- **Input validation** and sanitization
- **Rate limiting** and DDoS protection

### Database Security
- **Encrypted connections** with SSL/TLS
- **Access controls** with role-based permissions
- **Regular backups** with encryption at rest
- **Network isolation** on private networks

## Monitoring & Alerting

### Key Metrics
- **Server Health**: Response times, error rates, availability
- **Database Performance**: Connection counts, query performance
- **Cache Performance**: Redis hit rates, memory usage
- **WebSocket Activity**: Connection counts, message rates

### Alerting Conditions
- **Service Unavailable**: HTTP health checks failing
- **High Error Rates**: 5xx responses above threshold
- **Database Issues**: Connection failures, slow queries
- **Resource Exhaustion**: High CPU, memory, or disk usage

### Log Aggregation
- **Structured logging** with JSON format
- **Centralized collection** with Promtail/Loki or ELK stack
- **Log retention** policies and rotation
- **Security event monitoring** for authentication failures

## Troubleshooting

### Common Issues

#### Authentication Problems
- **OAuth misconfig**: Check provider settings and callback URLs
- **JWT issues**: Verify secret keys and token expiration
- **Permission errors**: Check user roles and resource access

#### Database Issues
- **Connection failures**: Verify network connectivity and credentials
- **Performance problems**: Check query performance and indexing
- **Replication lag**: Monitor primary/replica synchronization

#### WebSocket Problems
- **Connection failures**: Check WebSocket endpoint availability
- **Message delivery**: Verify Redis connectivity for coordination
- **Performance issues**: Monitor concurrent connection limits

### Performance Tuning

#### Application Performance
- **Connection pooling** for database and Redis
- **Request timeouts** appropriate for workload
- **Memory management** and garbage collection tuning
- **Concurrent request limits** based on capacity

#### Database Performance
- **Index optimization** for query performance
- **Connection pooling** to manage concurrent access
- **Query optimization** and slow query analysis
- **Storage performance** with appropriate disk types

#### Cache Performance
- **Memory allocation** based on working set size
- **Eviction policies** appropriate for access patterns
- **Connection limits** and timeout configuration
- **Persistence settings** for durability requirements

## Backup & Recovery

### Backup Strategy
- **Database backups**: Daily full backups with point-in-time recovery
- **Configuration backups**: Version-controlled configuration files
- **Certificate backups**: SSL/TLS certificates and keys
- **Log retention**: Archived logs for compliance and analysis

### Recovery Procedures
- **Service recovery**: Automated restart and health checking
- **Database recovery**: Point-in-time restoration procedures
- **Configuration recovery**: Infrastructure as code restoration
- **Disaster recovery**: Cross-region failover procedures

## Capacity Planning

### Scaling Considerations
- **Horizontal scaling**: Load balancing across multiple instances
- **Database scaling**: Read replicas and connection pooling
- **Cache scaling**: Redis clustering for high availability
- **Storage scaling**: Automated storage expansion

### Performance Baselines
- **Concurrent users**: Supported user load per instance
- **Request throughput**: Requests per second capacity
- **WebSocket connections**: Concurrent real-time connections
- **Database performance**: Query performance under load

## Related Documentation

### Development Context
<!-- NEEDS-REVIEW: development-setup.md and integration-testing.md do not exist at documented paths. See wiki Getting-Started-with-Development and Testing pages. -->
- Wiki: [Getting-Started-with-Development](https://github.com/ericfitz/tmi/wiki/Getting-Started-with-Development)
- Wiki: [Testing](https://github.com/ericfitz/tmi/wiki/Testing)

### Reference Materials
- [Architecture Documentation](../reference/architecture/) - System architecture
- [API Specifications](../reference/apis/) - API reference

## Contributing to Operations

When updating operations documentation:

1. **Test procedures** in staging environments before documentation
2. **Include command examples** with expected outputs
3. **Document rollback procedures** for all changes
4. **Update monitoring configurations** when adding new metrics
5. **Cross-reference related documentation** for completeness

For operations questions or to report issues with deployment procedures, please create an issue in the project repository.

---

## Verification Summary

**Verified on 2025-01-24:**

### Existing Files Verified:
- `deployment/` directory and contents - EXISTS
- `deployment/deployment-guide.md` - EXISTS
- `deployment/container-security.md` - EXISTS
- `database/` directory - EXISTS
- `database/postgresql-operations.md` - EXISTS
- `addons/` directory - EXISTS
- `../reference/architecture/` - EXISTS
- `../reference/apis/` - EXISTS

### Make Targets Verified:
- `make build-server` - Verified in Makefile (line 164)
- `make build-containers` - Verified in Makefile (line 1000)

### Missing Files (marked with NEEDS-REVIEW):
- `heroku-database-reset.md` - Content in wiki Database-Operations
- `oauth-environment-configuration.md` - Content in wiki Setting-Up-Authentication
- `webhook-configuration.md` - Content in wiki Webhook-Integration
- `monitoring/` directory - Content in wiki Monitoring-and-Health
- `database/postgresql-schema.md` - Content in wiki Database-Schema-Reference
- `database/redis-schema.md` - Content in wiki Database-Schema-Reference
- `../developer/setup/development-setup.md` - Content in wiki Getting-Started-with-Development
- `../developer/testing/integration-testing.md` - Content in wiki Testing
