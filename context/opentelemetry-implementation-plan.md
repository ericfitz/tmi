# TMI OpenTelemetry Implementation Plan

## Project Overview

**Objective**: Implement comprehensive observability using OpenTelemetry (OTel) for the TMI application, including migrating existing logging, adding distributed tracing, and implementing performance metrics.

**Duration Estimate**: 4-5 weeks  
**Complexity**: Medium-High  
**Priority**: High  

## Current State Analysis

### Existing Logging Implementation
- **Custom logging package**: `internal/logging/logger.go` with structured logging
- **Features**: Log levels, file rotation (lumberjack), context-aware logging, Gin middleware
- **Output formats**: Text-based with timestamps, request IDs, and context information
- **Deployment**: File-based logging with rotation and console output

### Current Gaps
- ❌ No distributed tracing
- ❌ No performance metrics collection
- ❌ No standardized observability format
- ❌ Limited correlation between logs, traces, and metrics
- ❌ No APM integration capabilities
- ❌ Manual performance monitoring

## OpenTelemetry Integration Strategy

### Three Pillars of Observability

#### 1. **Distributed Tracing**
- **HTTP request tracing** across all API endpoints
- **Database operation tracing** for PostgreSQL queries
- **Redis cache operation tracing** 
- **WebSocket connection tracing** for collaboration
- **Authorization flow tracing** with sensitive data filtering
- **External API call tracing** (OAuth providers)

#### 2. **Metrics Collection**
- **HTTP metrics**: Request duration, status codes, throughput
- **Database metrics**: Query duration, connection pool stats, transaction metrics
- **Redis metrics**: Cache hit/miss rates, operation latency, memory usage
- **Business metrics**: Threat model operations, diagram collaboration events
- **System metrics**: Memory usage, CPU utilization, goroutine counts
- **Custom metrics**: Authorization checks, WebSocket connections, API usage

#### 3. **Structured Logging**
- **Migrate existing logging** to OpenTelemetry logs
- **Correlation with traces** using trace and span IDs
- **Structured log attributes** for better querying
- **Log sampling** for high-volume scenarios
- **Security filtering** for sensitive information

### Architecture Design

```
┌─────────────────┐    ┌──────────────────┐    ┌─────────────────┐
│   TMI App       │    │  OTel Collector  │    │  Observability  │
│                 │    │                  │    │    Backend      │
│ ┌─────────────┐ │    │ ┌──────────────┐ │    │ ┌─────────────┐ │
│ │  Tracing    │─┼────┼→│ Processors   │─┼────┼→│ Jaeger      │ │
│ │  SDK        │ │    │ │ Filters      │ │    │ │ (Traces)    │ │
│ └─────────────┘ │    │ │ Samplers     │ │    │ └─────────────┘ │
│                 │    │ └──────────────┘ │    │                 │
│ ┌─────────────┐ │    │ ┌──────────────┐ │    │ ┌─────────────┐ │
│ │  Metrics    │─┼────┼→│ Aggregation  │─┼────┼→│ Prometheus  │ │
│ │  SDK        │ │    │ │ Export       │ │    │ │ (Metrics)   │ │
│ └─────────────┘ │    │ └──────────────┘ │    │ └─────────────┘ │
│                 │    │                  │    │                 │
│ ┌─────────────┐ │    │ ┌──────────────┐ │    │ ┌─────────────┐ │
│ │  Logging    │─┼────┼→│ Log Pipeline │─┼────┼→│ Loki        │ │
│ │  SDK        │ │    │ │ Correlation  │ │    │ │ (Logs)      │ │
│ └─────────────┘ │    │ └──────────────┘ │    │ └─────────────┘ │
└─────────────────┘    └──────────────────┘    └─────────────────┘
```

## Implementation Phases

### Phase 1: Foundation Setup (Week 1)
**Goal**: Establish OpenTelemetry infrastructure and basic instrumentation

#### 1.1 OpenTelemetry Dependencies
- [ ] **Task**: Add OpenTelemetry Go SDK dependencies
  - **Dependencies**: 
    ```go
    go.opentelemetry.io/otel v1.32.0
    go.opentelemetry.io/otel/sdk v1.32.0
    go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.32.0
    go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp v1.32.0
    go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp v0.8.0
    go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin v0.58.0
    go.opentelemetry.io/contrib/instrumentation/database/sql/otelsql v0.58.0
    go.opentelemetry.io/contrib/instrumentation/github.com/go-redis/redis/v8/otelredis v0.58.0
    ```
  - **Estimated Time**: 2 hours
  - **Tests**: Dependency validation and compatibility tests

#### 1.2 OpenTelemetry Configuration
- [ ] **Task**: Create OpenTelemetry configuration system
  - **File**: `internal/telemetry/config.go`
  - **Estimated Time**: 6 hours
  - **Dependencies**: OTel SDK
  - **Features**:
    - Service name and version configuration
    - Resource attributes (environment, deployment info)
    - Exporter configurations (OTLP, Console, Jaeger)
    - Sampling configurations for traces and metrics
    - Environment-based configuration (dev/staging/prod)
  - **Tests**: Configuration validation and environment-specific setup tests

#### 1.3 Core Telemetry Service
- [ ] **Task**: Implement core telemetry initialization service
  - **File**: `internal/telemetry/service.go`
  - **Estimated Time**: 8 hours
  - **Dependencies**: OTel configuration
  - **Features**:
    - TracerProvider initialization
    - MeterProvider initialization  
    - LoggerProvider initialization (when available)
    - Graceful shutdown handling
    - Context propagation setup
  - **Tests**: Service lifecycle and provider initialization tests

#### 1.4 Development Infrastructure
- [ ] **Task**: Set up local observability stack for development
  - **Files**: `docker-compose.observability.yml`, development scripts
  - **Estimated Time**: 4 hours
  - **Dependencies**: Docker infrastructure
  - **Components**:
    - Jaeger for trace visualization
    - Prometheus for metrics collection
    - Grafana for metrics visualization
    - OpenTelemetry Collector for data processing
  - **Tests**: Local stack validation and connectivity tests

**Phase 1 Milestone**: OpenTelemetry foundation established, local observability stack running

### Phase 2: Distributed Tracing Implementation (Week 2)
**Goal**: Implement comprehensive distributed tracing across all application components

#### 2.1 HTTP Request Tracing
- [ ] **Task**: Replace existing Gin logging middleware with OpenTelemetry tracing
  - **File**: `internal/telemetry/middleware.go`
  - **Estimated Time**: 6 hours
  - **Dependencies**: Core telemetry service
  - **Features**:
    - Automatic span creation for HTTP requests
    - Request/response attribute extraction
    - Error status and exception recording
    - Context propagation between requests
    - Compatible with existing authentication middleware
  - **Tests**: HTTP tracing end-to-end tests, context propagation validation

#### 2.2 Database Operation Tracing
- [ ] **Task**: Instrument PostgreSQL operations with OpenTelemetry
  - **File**: `internal/telemetry/database.go`
  - **Estimated Time**: 8 hours
  - **Dependencies**: Database layer, OTel SQL instrumentation
  - **Features**:
    - SQL query tracing with sanitized statements
    - Connection pool metrics and tracing
    - Transaction boundary tracing
    - Database error attribution
    - Query performance correlation
  - **Tests**: Database operation tracing tests, query sanitization validation

#### 2.3 Redis Cache Tracing
- [ ] **Task**: Instrument Redis operations with OpenTelemetry
  - **File**: `internal/telemetry/redis.go`
  - **Estimated Time**: 6 hours
  - **Dependencies**: Redis client, OTel Redis instrumentation
  - **Features**:
    - Cache operation tracing (GET, SET, DEL, etc.)
    - Pipeline operation tracing
    - Redis connection and latency metrics
    - Cache hit/miss rate tracking
    - Memory usage correlation
  - **Tests**: Redis tracing tests, cache operation validation

#### 2.4 Authorization Flow Tracing
- [ ] **Task**: Add tracing to authentication and authorization flows
  - **File**: `internal/telemetry/auth.go`
  - **Estimated Time**: 6 hours
  - **Dependencies**: Auth utilities, tracing middleware
  - **Features**:
    - OAuth flow tracing (without sensitive data)
    - JWT validation tracing
    - Authorization check tracing
    - Role-based access decision tracing
    - User context correlation
  - **Tests**: Auth tracing tests, sensitive data filtering validation

#### 2.5 WebSocket Collaboration Tracing
- [ ] **Task**: Instrument WebSocket operations for real-time collaboration
  - **File**: `internal/telemetry/websocket.go`
  - **Estimated Time**: 8 hours
  - **Dependencies**: WebSocket hub, tracing infrastructure
  - **Features**:
    - WebSocket connection lifecycle tracing
    - Message publishing and subscription tracing
    - Collaboration session correlation
    - Real-time update propagation tracing
    - Connection health and performance metrics
  - **Tests**: WebSocket tracing tests, collaboration flow validation

**Phase 2 Milestone**: Comprehensive distributed tracing implemented across all major components

### Phase 3: Metrics Implementation (Week 3)
**Goal**: Implement comprehensive metrics collection for performance monitoring

#### 3.1 HTTP Request Metrics
- [ ] **Task**: Implement HTTP request performance metrics
  - **File**: `internal/telemetry/http_metrics.go`
  - **Estimated Time**: 6 hours
  - **Dependencies**: HTTP tracing, metrics infrastructure
  - **Metrics**:
    - `http_request_duration_seconds` (histogram)
    - `http_requests_total` (counter)
    - `http_request_size_bytes` (histogram)
    - `http_response_size_bytes` (histogram)
    - `http_requests_in_flight` (gauge)
  - **Labels**: method, route, status_code, user_type
  - **Tests**: Metrics collection and export validation

#### 3.2 Database Performance Metrics
- [ ] **Task**: Implement database operation metrics
  - **File**: `internal/telemetry/database_metrics.go`
  - **Estimated Time**: 8 hours
  - **Dependencies**: Database tracing, connection pool instrumentation
  - **Metrics**:
    - `db_query_duration_seconds` (histogram)
    - `db_queries_total` (counter)
    - `db_connections_active` (gauge)
    - `db_connections_idle` (gauge)
    - `db_connection_wait_time_seconds` (histogram)
    - `db_transaction_duration_seconds` (histogram)
  - **Labels**: query_type, table, operation, status
  - **Tests**: Database metrics validation, connection pool monitoring

#### 3.3 Redis Cache Metrics
- [ ] **Task**: Implement Redis cache performance metrics
  - **File**: `internal/telemetry/redis_metrics.go`
  - **Estimated Time**: 6 hours
  - **Dependencies**: Redis tracing, cache service layer
  - **Metrics**:
    - `redis_operation_duration_seconds` (histogram)
    - `redis_operations_total` (counter)
    - `redis_cache_hits_total` (counter)
    - `redis_cache_misses_total` (counter)
    - `redis_memory_usage_bytes` (gauge)
    - `redis_connections_active` (gauge)
  - **Labels**: operation, cache_type, hit_status
  - **Tests**: Cache metrics validation, hit/miss ratio tracking

#### 3.4 Business Logic Metrics
- [ ] **Task**: Implement application-specific business metrics
  - **File**: `internal/telemetry/business_metrics.go`
  - **Estimated Time**: 10 hours
  - **Dependencies**: API handlers, business logic components
  - **Metrics**:
    - `threat_models_total` (counter)
    - `threat_model_operations_total` (counter)
    - `collaboration_sessions_active` (gauge)
    - `websocket_connections_active` (gauge)
    - `diagram_cells_modified_total` (counter)
    - `authorization_checks_total` (counter)
    - `api_usage_by_endpoint` (counter)
  - **Labels**: operation_type, user_role, resource_type, status
  - **Tests**: Business metrics validation, operational insight verification

#### 3.5 System Resource Metrics
- [ ] **Task**: Implement system-level performance metrics
  - **File**: `internal/telemetry/system_metrics.go`
  - **Estimated Time**: 4 hours
  - **Dependencies**: Runtime metrics, system monitoring
  - **Metrics**:
    - `go_goroutines` (gauge)
    - `go_memstats_*` (various memory metrics)
    - `process_cpu_seconds_total` (counter)
    - `process_resident_memory_bytes` (gauge)
    - `process_start_time_seconds` (gauge)
  - **Tests**: System metrics collection and accuracy validation

**Phase 3 Milestone**: Comprehensive metrics collection implemented with Prometheus export

### Phase 4: Logging Migration (Week 4)
**Goal**: Migrate existing logging to OpenTelemetry structured logging with trace correlation

#### 4.1 OpenTelemetry Logging Bridge
- [ ] **Task**: Create bridge between existing logging and OpenTelemetry
  - **File**: `internal/telemetry/logging_bridge.go`
  - **Estimated Time**: 8 hours
  - **Dependencies**: Existing logging package, OTel logging (when stable)
  - **Features**:
    - Maintain existing logging interface for backward compatibility
    - Add trace and span ID correlation to all log entries
    - Structured attribute support
    - Log level mapping and filtering
    - Context-aware logging enhancement
  - **Tests**: Logging correlation tests, backward compatibility validation

#### 4.2 Structured Log Attributes
- [ ] **Task**: Enhance log entries with structured attributes and correlation
  - **File**: `internal/telemetry/log_attributes.go`
  - **Estimated Time**: 6 hours
  - **Dependencies**: Logging bridge, tracing infrastructure
  - **Features**:
    - Automatic trace/span ID injection
    - Request context attribute extraction
    - User and session correlation
    - Error stack trace enhancement
    - Performance timing correlation
  - **Tests**: Attribute injection tests, correlation validation

#### 4.3 Security and Sensitive Data Filtering
- [ ] **Task**: Implement security filtering for sensitive information
  - **File**: `internal/telemetry/security_filter.go`
  - **Estimated Time**: 6 hours
  - **Dependencies**: Logging infrastructure
  - **Features**:
    - Automatic PII detection and masking
    - OAuth token and secret filtering
    - Password and credential sanitization
    - Configurable sensitive field patterns
    - Audit trail for security events
  - **Tests**: Security filtering tests, PII detection validation

#### 4.4 Log Sampling and Performance
- [ ] **Task**: Implement intelligent log sampling for high-volume scenarios
  - **File**: `internal/telemetry/log_sampling.go`
  - **Estimated Time**: 4 hours
  - **Dependencies**: Logging infrastructure, performance metrics
  - **Features**:
    - Rate-based sampling for debug logs
    - Error and warning log preservation
    - Context-aware sampling decisions
    - Performance impact minimization
    - Configurable sampling rates
  - **Tests**: Sampling behavior tests, performance impact validation

**Phase 4 Milestone**: Logging fully migrated to OpenTelemetry with trace correlation and security filtering

### Phase 5: Integration and Optimization (Week 5)
**Goal**: Complete integration testing, performance optimization, and production readiness

#### 5.1 End-to-End Observability Testing
- [ ] **Task**: Create comprehensive observability integration tests
  - **File**: `internal/telemetry/integration_test.go`
  - **Estimated Time**: 12 hours
  - **Dependencies**: All telemetry components
  - **Tests**:
    - Full request trace validation (HTTP → DB → Redis → Response)
    - Metrics correlation with trace data
    - Log correlation with traces and metrics
    - Error propagation and attribution
    - Performance overhead measurement
    - Context propagation validation
  - **Acceptance Criteria**: <5% performance overhead, 100% trace correlation

#### 5.2 Production Configuration and Deployment
- [ ] **Task**: Create production-ready OpenTelemetry configuration
  - **Files**: Production configs, deployment documentation
  - **Estimated Time**: 8 hours
  - **Dependencies**: All telemetry infrastructure
  - **Features**:
    - Environment-specific sampling rates
    - Production exporter configurations
    - Resource limit and memory management
    - Graceful degradation handling
    - Health check integration
  - **Tests**: Production configuration validation, load testing

#### 5.3 Observability Documentation and Runbooks
- [ ] **Task**: Create comprehensive observability documentation
  - **Files**: `docs/OBSERVABILITY.md`, runbooks, dashboards
  - **Estimated Time**: 6 hours
  - **Dependencies**: Complete implementation
  - **Deliverables**:
    - OpenTelemetry configuration guide
    - Metrics and dashboards documentation
    - Troubleshooting runbooks
    - Alert configuration templates
    - Performance baseline documentation
  - **Tests**: Documentation accuracy validation

#### 5.4 Performance Optimization and Tuning
- [ ] **Task**: Optimize OpenTelemetry performance for production
  - **File**: `internal/telemetry/optimization.go`
  - **Estimated Time**: 6 hours
  - **Dependencies**: Integration testing results
  - **Optimizations**:
    - Batch export configuration tuning
    - Memory allocation optimization
    - CPU usage minimization
    - Network overhead reduction
    - Sampling strategy refinement
  - **Tests**: Performance benchmark validation, resource usage monitoring

**Phase 5 Milestone**: Production-ready OpenTelemetry implementation with comprehensive observability

## OpenTelemetry Configuration Examples

### Service Configuration
```go
// internal/telemetry/config.go
type Config struct {
    ServiceName    string
    ServiceVersion string
    Environment    string
    
    // Tracing configuration
    TracingEnabled     bool
    TracingSampleRate  float64
    TracingEndpoint    string
    
    // Metrics configuration
    MetricsEnabled   bool
    MetricsInterval  time.Duration
    MetricsEndpoint  string
    
    // Logging configuration
    LoggingEnabled      bool
    LoggingEndpoint     string
    LogCorrelationEnabled bool
    
    // Resource attributes
    ResourceAttributes map[string]string
}
```

### Instrumentation Examples
```go
// HTTP Request Instrumentation
func (s *Server) instrumentedHandler(handler gin.HandlerFunc) gin.HandlerFunc {
    return func(c *gin.Context) {
        // Create span for request
        ctx, span := tracer.Start(c.Request.Context(), 
            fmt.Sprintf("%s %s", c.Request.Method, c.FullPath()))
        defer span.End()
        
        // Add request attributes
        span.SetAttributes(
            attribute.String("http.method", c.Request.Method),
            attribute.String("http.route", c.FullPath()),
            attribute.String("user.id", getUserID(c)),
        )
        
        // Update context
        c.Request = c.Request.WithContext(ctx)
        
        // Record metrics
        requestCounter.Add(ctx, 1, metric.WithAttributes(
            attribute.String("method", c.Request.Method),
            attribute.String("route", c.FullPath()),
        ))
        
        startTime := time.Now()
        
        // Execute handler
        handler(c)
        
        // Record duration
        duration := time.Since(startTime)
        requestDuration.Record(ctx, duration.Seconds())
        
        // Set span status based on response
        if c.Writer.Status() >= 400 {
            span.SetStatus(codes.Error, http.StatusText(c.Writer.Status()))
        }
        
        span.SetAttributes(
            attribute.Int("http.status_code", c.Writer.Status()),
            attribute.Float64("http.duration_ms", duration.Seconds()*1000),
        )
    }
}

// Database Query Instrumentation
func (s *ThreatStore) instrumentedQuery(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
    ctx, span := tracer.Start(ctx, "db.query")
    defer span.End()
    
    // Sanitize and add query info
    span.SetAttributes(
        attribute.String("db.system", "postgresql"),
        attribute.String("db.operation", extractOperation(query)),
        attribute.String("db.statement", sanitizeQuery(query)),
    )
    
    startTime := time.Now()
    
    rows, err := s.db.QueryContext(ctx, query, args...)
    
    duration := time.Since(startTime)
    
    // Record metrics
    dbQueryDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(
        attribute.String("operation", extractOperation(query)),
        attribute.String("status", getStatus(err)),
    ))
    
    if err != nil {
        span.RecordError(err)
        span.SetStatus(codes.Error, err.Error())
    }
    
    span.SetAttributes(
        attribute.Float64("db.duration_ms", duration.Seconds()*1000),
    )
    
    return rows, err
}
```

## Key Metrics and Dashboards

### HTTP Request Dashboard
```
- Request Rate (requests/second)
- Request Duration (P50, P95, P99)
- Error Rate (4xx, 5xx responses)
- Request Size Distribution
- Response Size Distribution
- Concurrent Requests
```

### Database Performance Dashboard
```
- Query Duration (P50, P95, P99)
- Query Rate (queries/second)
- Connection Pool Utilization
- Transaction Duration
- Slow Query Detection
- Connection Wait Time
```

### Redis Cache Dashboard
```
- Cache Hit Rate
- Cache Miss Rate
- Operation Duration
- Memory Usage
- Connection Count
- Key Distribution
```

### Business Metrics Dashboard
```
- Active Threat Models
- Collaboration Sessions
- API Usage by Endpoint
- User Activity Patterns
- Error Patterns
- Feature Adoption
```

## Testing Strategy

### Unit Testing Requirements
- [ ] **Telemetry Configuration Tests**
  - Configuration validation
  - Environment-specific setup
  - Resource attribute handling
  - Exporter initialization

- [ ] **Instrumentation Tests**
  - Span creation and attributes
  - Metrics collection accuracy
  - Context propagation
  - Error handling and recording

- [ ] **Bridge and Compatibility Tests**
  - Backward compatibility with existing logging
  - Migration path validation
  - Performance impact measurement

### Integration Testing Requirements
- [ ] **End-to-End Trace Validation**
  - Complete request traces
  - Cross-service correlation
  - Error propagation
  - Context preservation

- [ ] **Metrics Correlation Tests**
  - Metrics alignment with traces
  - Business logic correlation
  - Performance baseline validation

### Performance Testing Requirements
- [ ] **Overhead Measurement**
  - CPU usage impact
  - Memory allocation overhead
  - Network bandwidth utilization
  - Latency impact on requests

- [ ] **Load Testing with Observability**
  - High-volume tracing behavior
  - Sampling effectiveness
  - Export performance under load
  - Resource limit handling

## Production Deployment Strategy

### Rollout Phases
1. **Phase 1**: Deploy with observability disabled (infrastructure only)
2. **Phase 2**: Enable tracing with high sampling rate in staging
3. **Phase 3**: Enable metrics collection in staging
4. **Phase 4**: Enable logging correlation in staging
5. **Phase 5**: Gradual production rollout with monitoring
6. **Phase 6**: Full production deployment with optimized configuration

### Monitoring and Alerts
```yaml
# Example alert rules
- alert: HighTraceDropRate
  expr: otel_exporter_traces_dropped_total / otel_exporter_traces_total > 0.05
  
- alert: HighObservabilityOverhead
  expr: otel_cpu_usage_percent > 5.0
  
- alert: TraceExportFailure
  expr: otel_exporter_traces_failed_total > 0
```

### Configuration Management
```yaml
# Production configuration example
telemetry:
  service:
    name: "tmi-api"
    version: "1.0.0"
    environment: "production"
  
  tracing:
    enabled: true
    sample_rate: 0.1  # 10% sampling in production
    endpoint: "https://jaeger-collector:14268/api/traces"
  
  metrics:
    enabled: true
    interval: "30s"
    endpoint: "https://prometheus-gateway:9091/metrics"
  
  logging:
    enabled: true
    correlation_enabled: true
    endpoint: "https://loki:3100/api/v1/push"
```

## Success Criteria

### Functional Requirements
- [ ] Complete distributed tracing across all major request flows
- [ ] Comprehensive metrics collection for performance monitoring
- [ ] Structured logging with trace correlation
- [ ] Backward compatibility with existing logging interfaces
- [ ] Security filtering for sensitive information

### Performance Requirements
- [ ] <5% CPU overhead from observability instrumentation
- [ ] <10MB additional memory usage under normal load
- [ ] <100ms additional latency for trace export
- [ ] >99% trace delivery success rate
- [ ] <1% sampling impact on application performance

### Operational Requirements
- [ ] Production-ready configuration and deployment
- [ ] Comprehensive documentation and runbooks
- [ ] Alert rules and dashboard templates
- [ ] Graceful degradation under failure conditions
- [ ] Integration with existing monitoring infrastructure

## Development Commands

### Testing Commands
```bash
# Run all telemetry tests
make test-telemetry

# Run observability integration tests
make test-observability

# Performance benchmarks with instrumentation
make benchmark-telemetry

# Validate OpenTelemetry configuration
make validate-otel-config
```

### Development Commands
```bash
# Start local observability stack
make dev-observability

# Generate sample traces and metrics
make generate-telemetry-data

# Export telemetry data
make export-telemetry

# Clean up telemetry data
make clean-telemetry
```

### Deployment Commands
```bash
# Deploy with observability enabled
make deploy-with-telemetry

# Validate production telemetry configuration
make validate-prod-telemetry

# Monitor telemetry health
make monitor-telemetry
```

This comprehensive OpenTelemetry implementation plan provides a structured approach to implementing world-class observability for the TMI application, enabling better performance monitoring, debugging, and operational insights while maintaining security and performance standards.