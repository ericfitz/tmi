# Operations Documentation

This directory contains deployment, operations, and troubleshooting guidance for running TMI in production environments.

## Purpose

Comprehensive operations documentation covering production deployment, database management, monitoring, and troubleshooting for TMI server infrastructure.

## Directory Structure

### 🚀 [deployment/](deployment/) - Deployment & Infrastructure
Production deployment guides, container security, and infrastructure setup.

### 🗄️ [database/](database/) - Database Operations & Management
PostgreSQL and Redis operations, schema management, and performance tuning.

### 📊 [monitoring/](monitoring/) - Monitoring & Observability
System monitoring, logging, alerting, and performance analysis.

### 🔌 [addons/](addons/) - Addon Configuration
Configuration and management of TMI addons and extensions.

## Files in this Directory

### [heroku-database-reset.md](heroku-database-reset.md)
**Heroku database reset procedures** for TMI deployments on Heroku.

**Content includes:**
- Database schema drop and reset procedures
- Migration re-execution steps
- Post-reset verification
- Warning and safety procedures

### [oauth-environment-configuration.md](oauth-environment-configuration.md)
**OAuth environment configuration** for production deployments.

**Content includes:**
- OAuth provider environment variables
- Multi-provider configuration
- Production vs development settings
- Secret management best practices
- Troubleshooting OAuth configuration

### [webhook-configuration.md](webhook-configuration.md)
**Webhook system configuration** for TMI event delivery.

**Content includes:**
- Webhook endpoint configuration
- Event type configuration
- Security settings (signatures, verification)
- Retry policies and delivery guarantees
- Monitoring and troubleshooting

## Getting Started

1. **Start Here**: [deployment/deployment-guide.md](deployment/deployment-guide.md)
2. **Security**: [deployment/container-security.md](deployment/container-security.md)
3. **Database Setup**: [database/postgresql-operations.md](database/postgresql-operations.md)
4. **Monitoring**: Setup monitoring and observability infrastructure

## Operations Overview

### TMI Production Architecture
```
[Load Balancer] → [TMI Server Instances] → [PostgreSQL Primary]
                                        → [Redis Cluster]
                                        → [Monitoring Stack]
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
# Server health check
curl https://tmi.example.com/version

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
- [PostgreSQL Schema](database/postgresql-schema.md) - Schema documentation
- [Redis Schema](database/redis-schema.md) - Cache layer management

### Monitoring & Observability
- Setup monitoring and alerting systems for production TMI deployments

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
- **Security hardening** with distroless images

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
- **Centralized collection** with ELK stack or similar
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
- [Development Setup](../developer/setup/development-setup.md) - Local development environment
- [Integration Testing](../developer/testing/integration-testing.md) - Testing procedures

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