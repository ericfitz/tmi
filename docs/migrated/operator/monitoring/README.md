# Monitoring & Observability

This directory contains system monitoring, logging, alerting, and performance analysis documentation for TMI operations.

<!-- Verified: 2025-01-24 -->
<!-- This document provides general monitoring guidance. TMI-specific implementations should be verified against source code. -->

## Files in this Directory

## Monitoring Architecture

### TMI Observability Stack

```
[TMI Application] --> [Metrics Collection] --> [Time Series DB]
                 --> [Log Aggregation]   --> [Log Storage]
                 --> [Health Checks]     --> [Alerting System]
```

<!-- NEEDS-REVIEW: Distributed tracing (OpenTelemetry/Jaeger) is documented but not implemented in TMI source code. The trace collection path has been removed from the diagram. -->

### Key Components

- **Metrics Collection**: Application and system metrics
- **Log Aggregation**: Centralized logging with structured data (Promtail/Loki supported)
- **Health Monitoring**: Service availability and performance
- **Alerting System**: Proactive issue notification

<!-- NEEDS-REVIEW: Distributed Tracing component removed - not implemented in TMI codebase -->

## Monitoring Categories

### Application Monitoring

- **HTTP Metrics**: Request rates, response times, error rates
- **WebSocket Metrics**: Connection counts, message throughput
- **Business Metrics**: User activity, collaboration sessions
- **Performance Metrics**: Memory usage, CPU utilization
- **Error Tracking**: Exception rates, error patterns

### Infrastructure Monitoring

- **System Metrics**: CPU, memory, disk, network usage
- **Container Metrics**: Resource utilization, container health
- **Network Monitoring**: Connectivity, latency, throughput
- **Storage Monitoring**: Disk usage, I/O performance
- **Security Monitoring**: Access patterns, authentication events

### Database Monitoring

- **PostgreSQL Metrics**: Connection counts, query performance, replication lag
- **Redis Metrics**: Memory usage, cache hit rates, connection counts
- **Query Analysis**: Slow query detection and optimization
- **Connection Pool Monitoring**: Pool utilization and performance
- **Backup Monitoring**: Backup success rates and timing

### Integration Monitoring

- **OAuth Provider Health**: Authentication success rates, response times
- **External API Monitoring**: Third-party service availability
- **Client Integration Health**: Client connection success rates
- **WebSocket Coordination**: Real-time collaboration performance
- **Cross-Service Communication**: Inter-service connectivity

## Key Metrics and KPIs

### Service Level Indicators (SLIs)

- **Availability**: Service uptime percentage
- **Latency**: Request response time percentiles (P50, P95, P99)
- **Error Rate**: Percentage of failed requests
- **Throughput**: Requests per second capacity
- **Collaboration Health**: WebSocket connection success rate

### Business Metrics

- **User Activity**: Daily/monthly active users
- **Collaboration Usage**: Real-time collaboration session counts
- **Feature Usage**: Threat model and diagram creation rates
- **Performance**: Page load times and interaction responsiveness
- **Integration Health**: Client integration success rates

### System Health Metrics

- **Resource Utilization**: CPU, memory, disk usage
- **Database Performance**: Query performance and connection health
- **Cache Performance**: Redis hit rates and memory utilization
- **Network Health**: Connectivity and bandwidth utilization
- **Security Events**: Authentication failures and security incidents

## Alerting Strategy

### Critical Alerts (Immediate Response)

- **Service Down**: TMI server unavailable
- **Database Failure**: PostgreSQL connection failures
- **Authentication Outage**: OAuth provider failures
- **High Error Rate**: >5% error rate sustained
- **Resource Exhaustion**: >90% CPU/memory usage

### Warning Alerts (Monitored Response)

- **Performance Degradation**: Response times >2x baseline
- **Cache Issues**: Redis connection problems
- **Storage Issues**: Disk usage >80%
- **Backup Failures**: Database backup failures
- **Integration Issues**: Client integration problems

### Info Alerts (Awareness Only)

- **Capacity Planning**: Resource usage trends
- **Performance Trends**: Gradual performance changes
- **Usage Patterns**: User activity changes
- **Security Events**: Unusual authentication patterns
- **Maintenance Reminders**: Scheduled maintenance tasks

## Logging Strategy

### Structured Logging

TMI uses structured JSON logging via the `internal/slogging` package.

<!-- Verified: internal/config/config.go contains LoggingConfig with these fields -->

- **JSON Format**: Machine-readable log format
- **Consistent Fields**: Standardized log field structure
- **Request Correlation**: Request ID tracking across services
- **Security Events**: Authentication and authorization logging
- **Performance Logging**: Request timing and resource usage

### Log Configuration

Configuration options in `config.yml` (verified in source code):

```yaml
logging:
  level: "info"                    # debug, info, warn, error
  log_dir: "logs"                  # Default: "logs"
  max_age_days: 7                  # Log retention (default: 7)
  max_size_mb: 100                 # Max file size (default: 100)
  max_backups: 10                  # Number of rotated files (default: 10)
  also_log_to_console: true        # Dual logging (default: true)
  log_api_requests: false          # Request logging
  log_websocket_messages: false    # WebSocket message logging
  redact_auth_tokens: true         # Security redaction
```

### Log Categories

- **Application Logs**: Business logic and application events
- **Access Logs**: HTTP request and response logging
- **Security Logs**: Authentication and authorization events
- **Performance Logs**: Request timing and resource usage
- **Error Logs**: Exception and error condition logging

### Log Management

- **Centralized Collection**: Log aggregation from all services
- **Retention Policies**: Appropriate log retention periods
- **Log Rotation**: Automatic log file rotation and compression
- **Search and Analysis**: Log querying and analysis capabilities
- **Alert Integration**: Log-based alerting and notification

## Performance Monitoring

### Application Performance

- **Response Time Monitoring**: HTTP and WebSocket response times
- **Throughput Monitoring**: Request processing capacity
- **Error Rate Tracking**: Application error rates and patterns
- **Resource Usage**: Memory and CPU utilization patterns
- **Concurrent User Monitoring**: Real-time user load tracking

### Database Performance

- **Query Performance**: SQL query execution time analysis
- **Connection Monitoring**: Database connection pool utilization
- **Replication Health**: Primary-replica synchronization status
- **Storage Performance**: Disk I/O and storage capacity
- **Cache Performance**: Redis performance and hit rates

### Network Performance

- **Connectivity Monitoring**: Service-to-service connectivity
- **Bandwidth Utilization**: Network throughput monitoring
- **Latency Tracking**: Network latency between components
- **WebSocket Performance**: Real-time connection performance
- **External Integration**: Third-party service response times

## Monitoring Tools and Integration

### Metrics Collection (External Tools)

<!-- NEEDS-REVIEW: Prometheus/Grafana integration not implemented in TMI - these are recommended external tools -->

- **Prometheus**: Time-series metrics collection (recommended)
- **Grafana**: Metrics visualization and dashboards (recommended)
- **Custom Metrics**: Application-specific metric collection via `api/performance_monitor.go`
- **System Metrics**: Infrastructure monitoring integration
- **Alert Manager**: Metric-based alerting (recommended)

### Log Management

<!-- Verified: promtail/ directory exists with configuration -->

- **Promtail/Loki**: TMI includes Promtail container for log shipping to Grafana Cloud/Loki
- **ELK Stack**: Alternative - Elasticsearch, Logstash, Kibana for log analysis
- **Structured Logging**: JSON-formatted application logs (verified in source)
- **Log Shipping**: Centralized log collection and processing
- **Log Alerting**: Log-pattern-based alerting

### Promtail Setup (TMI Native)

<!-- Verified: Makefile contains build-promtail, start-promtail, stop-promtail, clean-promtail targets -->

```bash
# Build Promtail container
make build-promtail

# Start Promtail with auto-detected config
make start-promtail

# Or with explicit credentials
LOKI_URL="https://user:pass@logs.grafana.net/api/prom/push" make start-promtail

# Check Promtail status
docker logs promtail
```

## Health Checks and SLOs

### Health Check Endpoints

<!-- Verified: api/version.go shows root endpoint (/) returns ApiInfo with health status -->

- **Service Health**: Root endpoint `/` returns API info and health status
- **Database Health**: Database connectivity verification (included in root endpoint health check)
- **Cache Health**: Redis connectivity verification (included in root endpoint health check)
- **OAuth Health**: Authentication provider availability
- **Integration Health**: External service connectivity

### Health Check Example

```bash
# Check service health (returns JSON with status, version, and health info)
curl https://tmi.example.com/

# Expected response includes:
# - status.code: "OK" or "DEGRADED"
# - service.build: version string
# - api.version: API version
# - health (only when DEGRADED): database and redis status details
```

### Service Level Objectives (SLOs)

- **Availability SLO**: 99.9% uptime target
- **Latency SLO**: <200ms P95 response time
- **Error Rate SLO**: <1% error rate target
- **Throughput SLO**: Minimum requests per second capacity
- **Recovery SLO**: <5 minute incident response time

## Incident Response

### Monitoring-Driven Incident Response

- **Alert Triage**: Prioritization based on severity and impact
- **Escalation Procedures**: Alert routing and escalation paths
- **Runbook Integration**: Automated runbook execution
- **Communication**: Stakeholder notification procedures
- **Post-Incident Analysis**: Monitoring data for root cause analysis

### Monitoring During Incidents

- **Real-time Dashboards**: Live system status visibility
- **Metric Correlation**: Cross-metric analysis for diagnosis
- **Log Analysis**: Real-time log analysis during incidents
- **Performance Impact**: User impact assessment
- **Recovery Validation**: Monitoring-based recovery verification

## Related Documentation

### Operations and Deployment

<!-- Verified: These files exist -->

- [Deployment Guide](../deployment/deployment-guide.md) - Production deployment with monitoring
- [Database Operations](../database/postgresql-operations.md) - Database monitoring integration

### Development and Testing

<!-- NEEDS-REVIEW: integration-testing.md does not exist at specified path -->
<!-- Verified: development-setup.md exists -->

- [Development Setup](../../developer/setup/development-setup.md) - Local monitoring setup

### Reference and Architecture

<!-- Verified: These directories exist -->

- [System Architecture](../../reference/architecture/) - Monitoring architecture design
- [API Documentation](../../reference/apis/) - API monitoring specifications

## Quick Monitoring Setup

### Basic Health Monitoring

```bash
# Check service health (root endpoint)
curl https://tmi.example.com/

# Database health
psql -h db-host -U user -d tmi -c "SELECT 1"

# Cache health
redis-cli -h redis-host ping
```

---

## Verification Summary

**Verified Items:**
- Logging configuration fields match `internal/config/config.go` LoggingConfig struct
- Root endpoint `/` returns API info with health status (verified in `api/version.go`)
- Promtail container setup exists (`promtail/` directory with README, config templates)
- Make targets exist: `build-promtail`, `start-promtail`, `stop-promtail`, `clean-promtail`
- Related documentation paths verified: `deployment-guide.md`, `postgresql-operations.md`, `development-setup.md`
- Reference directories exist: `docs/reference/architecture/`, `docs/reference/apis/`

**Items Requiring Review:**
- Distributed tracing (OpenTelemetry/Jaeger) is mentioned but not implemented in TMI source code
- Prometheus/Grafana integration is recommended but not built into TMI
- `/version` endpoint reference in original doc is incorrect - should be root `/` endpoint
- `integration-testing.md` does not exist at `docs/developer/testing/integration-testing.md`
- Observability make targets (`observability-start`, `obs-start`, etc.) mentioned in CLAUDE.md are not present in Makefile

**External Tools Verified (via web search):**
- Prometheus: Open-source monitoring system and time series database (CNCF graduated)
- Grafana: Visualization and dashboard platform for time series data
- ELK Stack: Elasticsearch, Logstash, Kibana for log aggregation and analysis
- OpenTelemetry: CNCF observability framework for distributed tracing (not implemented in TMI)
- Jaeger: CNCF distributed tracing platform (not implemented in TMI)
