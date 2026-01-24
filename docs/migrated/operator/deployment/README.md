# Deployment & Infrastructure

<!-- MIGRATED: Content integrated into wiki Planning-Your-Deployment.md on 2025-01-24 -->
<!-- This file is maintained for reference. See wiki for current documentation. -->

This directory contains production deployment guides, container security, and infrastructure setup documentation.

## Files in this Directory

### [deployment-guide.md](deployment-guide.md)
**Comprehensive production deployment guide** for TMI server in various environments.

**Content includes:**
- Complete deployment procedures for traditional servers, Docker, and Kubernetes
- Environment setup and prerequisites
- Configuration management (YAML files and environment variables)
- Database and Redis setup procedures
- TLS/SSL certificate configuration
- OAuth provider setup for production
- Security hardening guidelines
- Health checks and monitoring setup
- Backup and recovery procedures
- Troubleshooting common deployment issues
- Performance tuning recommendations

**Deployment Options Covered:**
- Traditional server deployment with systemd
- Docker Compose deployment
- Kubernetes deployment with Helm charts
- Load balancer configuration
- Reverse proxy setup (Nginx/Apache)

### [container-security.md](container-security.md)
**Container security hardening** for TMI Docker deployments.

**Content includes:**
- Secure container image building with Chainguard base images
- Multi-stage builds with static Go binaries
- Security scanning integration with Docker Scout
- Container runtime security
- Network security for containers
- Volume and storage security
- Secrets management in containers
- Container orchestration security
- Compliance and audit considerations

**Security Features:**
- Chainguard base images (minimal CVEs, daily security updates)
- Static binary builds with `CGO_ENABLED=0`
- Vulnerability scanning with Docker Scout
- Multi-stage builds for minimal attack surface (~57MB images)
- Non-root user execution (nonroot:nonroot)
- No shell or package manager in runtime images
- Resource limits and constraints
- Network isolation policies

### [heroku-deployment.md](heroku-deployment.md)
**Heroku-specific deployment guide** for TMI.

**Content includes:**
- Heroku application configuration
- Heroku Postgres setup and management
- Heroku Redis configuration
- Environment variable management
- Dyno configuration and scaling
- Heroku CLI commands
- Add-on configuration
- Deployment pipelines
- Logging and monitoring on Heroku
- Troubleshooting Heroku deployments

## Deployment Architecture

### Production Architecture Overview
```
[Internet] → [Load Balancer/Reverse Proxy]
                      ↓
            [TMI Server Instances]
                      ↓
            [Database Cluster (PostgreSQL)]
            [Cache Cluster (Redis)]
            [Monitoring Stack]
```

### Key Components
- **Load Balancer**: Traffic distribution, SSL termination, health checks
- **TMI Servers**: Go application instances (horizontally scalable)
- **Database**: PostgreSQL primary with optional read replicas
- **Cache**: Redis for sessions and real-time coordination
- **Monitoring**: Observability stack for metrics and alerts

## Deployment Strategies

### 1. Single Server Deployment
**Best for**: Small teams, development/staging environments

- Single server running TMI, PostgreSQL, and Redis
- Nginx reverse proxy for SSL termination
- Local file-based logging
- Basic monitoring with health checks

### 2. Multi-Server Deployment  
**Best for**: Production environments with moderate load

- Load-balanced TMI server instances
- Dedicated database and cache servers
- Centralized logging and monitoring
- Automated backup and recovery

### 3. Container Orchestration
**Best for**: Large-scale, cloud-native deployments

- Kubernetes or Docker Swarm orchestration
- Auto-scaling based on metrics
- Service mesh for advanced networking
- GitOps deployment workflows

## Security Hardening

### Application Security
- JWT secret management with secure key rotation
- OAuth callback URL validation
- Request rate limiting and DDoS protection
- Input validation and sanitization

### Infrastructure Security
- TLS 1.3 for all communications
- Network segmentation with firewalls
- Regular security updates and patching
- Vulnerability scanning and monitoring

### Container Security
- Chainguard base images for minimal attack surface
- Static Go binaries with `CGO_ENABLED=0`
- Non-root user execution (nonroot:nonroot)
- No shell or package manager in runtime images
- Resource limits to prevent resource exhaustion
- Docker Scout vulnerability scanning

## Configuration Management

### Environment-Specific Configuration
- **Development**: Local development with test providers
- **Staging**: Production-like environment for testing
- **Production**: Secure, scalable production deployment

### Configuration Sources
1. **YAML files**: Base configuration with environment-specific overrides
2. **Environment variables**: Secrets and environment-specific values
3. **Secret management**: External secret stores (HashiCorp Vault, etc.)

### Configuration Validation
- Required configuration validation on startup
- Configuration file syntax validation
- Environment variable presence checking
- OAuth provider connectivity verification

## Monitoring Integration

### Health Checks
- HTTP health check endpoint (`/`) returns ApiInfo with status, service, and API information
- Database connectivity verification
- Redis cache connectivity verification
- OAuth provider availability checks

### Metrics Collection
- Application metrics (request rates, response times)
- System metrics (CPU, memory, disk, network)
- Database metrics (connection counts, query performance)
- Business metrics (user activity, collaboration sessions)

### Log Management
- Structured JSON logging
- Centralized log aggregation
- Log retention and rotation policies
- Security event monitoring

## Related Documentation

### Prerequisites and Setup
- [PostgreSQL Operations](../database/postgresql-operations.md) - Database setup
- [Redis Schema](../database/redis-schema.md) - Cache configuration
- [Development Setup](../../developer/setup/development-setup.md) - Local development

### Integration and Testing
- [Integration Testing](../../developer/testing/integration-testing.md) - Testing procedures
- [Client Integration](../../developer/integration/client-integration-guide.md) - Client setup

### Operations and Maintenance
- [Database Operations](../database/postgresql-operations.md) - Database maintenance
- Monitoring setup and observability configuration

## Quick Deployment Checklist

### Pre-Deployment
- [ ] Server resources allocated and accessible
- [ ] Database and Redis instances configured
- [ ] OAuth applications configured with production URLs
- [ ] SSL/TLS certificates obtained and validated
- [ ] DNS records configured for production domain
- [ ] Backup and recovery procedures tested

### Deployment
- [ ] TMI server binary built and tested
- [ ] Configuration files prepared and validated
- [ ] Database migrations completed
- [ ] Services deployed and started
- [ ] Health checks passing
- [ ] Load balancer configured and tested

### Post-Deployment
- [ ] Application functionality verified
- [ ] Authentication flows tested
- [ ] WebSocket connections verified
- [ ] Monitoring and alerts configured
- [ ] Backup procedures scheduled
- [ ] Performance baselines established

For detailed step-by-step deployment procedures, see the individual guide files in this directory.

---

## Verification Summary (2025-01-24)

### Verified Items
- [x] File references verified: deployment-guide.md, container-security.md exist in this directory
- [x] PostgreSQL operations link verified: ../database/postgresql-operations.md exists
- [x] Make targets verified in Makefile: build-container-db, build-container-redis, build-container-tmi, build-containers, scan-containers, report-containers, observability-start, observability-stop
- [x] Health check endpoint corrected: Root endpoint `/` returns ApiInfo (was incorrectly `/version`)
- [x] Chainguard base images verified in container-security.md and Makefile
- [x] CGO_ENABLED=0 for static builds verified in container-security.md
- [x] Image size ~57MB verified in container-security.md
- [x] nonroot:nonroot user verified in container-security.md

### Items with Notes
- Related documentation paths use relative paths that work within the docs directory structure
- TLS 1.3 mentioned in security hardening but not specifically enforced by TMI code (depends on deployment configuration)

### Migration Status
- Content integrated into wiki page: Planning-Your-Deployment.md
- Added reference links to container-security.md, deployment-guide.md, and postgresql-operations.md in wiki