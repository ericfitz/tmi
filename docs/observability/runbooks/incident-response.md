# Incident Response Runbook

This runbook provides step-by-step procedures for responding to incidents in the TMI application.

## Table of Contents

1. [General Incident Response](#general-incident-response)
2. [Service Availability Issues](#service-availability-issues)
3. [Performance Degradation](#performance-degradation)
4. [Database Issues](#database-issues)
5. [Cache Problems](#cache-problems)
6. [Security Incidents](#security-incidents)
7. [Observability System Issues](#observability-system-issues)
8. [Escalation Procedures](#escalation-procedures)

## General Incident Response

### Initial Response (First 5 minutes)

1. **Acknowledge the Alert**
   ```bash
   # Check alert details
   curl http://prometheus:9090/api/v1/alerts
   ```

2. **Assess Service Health**
   ```bash
   # Check application health endpoint
   curl http://tmi-api:8080/health
   
   # Verify service status
   kubectl get pods -n tmi -l app=tmi-api
   ```

3. **Check Key Metrics**
   ```bash
   # Error rate
   curl -s 'http://prometheus:9090/api/v1/query?query=rate(http_requests_total{status=~"5.."}[5m])'
   
   # Response time
   curl -s 'http://prometheus:9090/api/v1/query?query=histogram_quantile(0.95, rate(http_request_duration_seconds_bucket[5m]))'
   ```

4. **Review Recent Changes**
   - Check deployment history
   - Review recent configuration changes
   - Verify if incident correlates with deployments

### Investigation Phase (5-15 minutes)

1. **Examine Traces**
   - Open Jaeger UI: http://jaeger:16686
   - Search for error traces in the last 30 minutes
   - Look for unusual trace patterns or high latency

2. **Analyze Logs**
   ```bash
   # Check recent error logs
   kubectl logs -n tmi -l app=tmi-api --since=30m | grep ERROR
   
   # Look for patterns
   kubectl logs -n tmi -l app=tmi-api --since=30m | grep -E "(panic|fatal|error)" | head -20
   ```

3. **Resource Utilization**
   ```bash
   # CPU and memory usage
   kubectl top pods -n tmi
   
   # Database connections
   curl -s 'http://prometheus:9090/api/v1/query?query=postgres_connections_active'
   ```

## Service Availability Issues

### Symptoms
- Health check failures
- HTTP 5xx error rate > 5%
- Service unavailable responses

### Investigation Steps

1. **Check Pod Status**
   ```bash
   kubectl get pods -n tmi -l app=tmi-api
   kubectl describe pods -n tmi -l app=tmi-api
   ```

2. **Review Pod Events**
   ```bash
   kubectl get events -n tmi --sort-by='.lastTimestamp' | head -20
   ```

3. **Check Resource Limits**
   ```bash
   kubectl describe pod -n tmi <pod-name>
   # Look for resource limit exceeded events
   ```

4. **Verify Dependencies**
   ```bash
   # Database connectivity
   kubectl exec -n tmi <api-pod> -- pg_isready -h postgres -p 5432
   
   # Redis connectivity
   kubectl exec -n tmi <api-pod> -- redis-cli -h redis ping
   ```

### Resolution Steps

1. **Immediate Actions**
   ```bash
   # Restart failed pods
   kubectl delete pod -n tmi -l app=tmi-api
   
   # Scale up if needed
   kubectl scale deployment -n tmi tmi-api --replicas=5
   ```

2. **If Database Issues**
   ```bash
   # Check database status
   kubectl logs -n tmi postgres-0
   
   # Restart database if needed
   kubectl delete pod -n tmi postgres-0
   ```

3. **If Load Balancer Issues**
   ```bash
   # Check service endpoints
   kubectl get endpoints -n tmi tmi-api-service
   
   # Verify ingress configuration
   kubectl describe ingress -n tmi tmi-ingress
   ```

## Performance Degradation

### Symptoms
- Response time > 2 seconds (95th percentile)
- Increased CPU/memory usage
- User complaints about slow responses

### Investigation Steps

1. **Analyze Response Times**
   ```bash
   # Check percentiles
   curl -s 'http://prometheus:9090/api/v1/query?query=histogram_quantile(0.95, rate(http_request_duration_seconds_bucket[5m]))'
   
   # Slowest endpoints
   curl -s 'http://prometheus:9090/api/v1/query?query=topk(10, rate(http_request_duration_seconds_sum[5m]) / rate(http_request_duration_seconds_count[5m]))'
   ```

2. **Database Performance**
   ```bash
   # Slow queries
   curl -s 'http://prometheus:9090/api/v1/query?query=rate(postgres_slow_queries_total[5m])'
   
   # Connection pool usage
   curl -s 'http://prometheus:9090/api/v1/query?query=postgres_connections_active / postgres_connections_max'
   ```

3. **Cache Performance**
   ```bash
   # Cache hit rate
   curl -s 'http://prometheus:9090/api/v1/query?query=rate(redis_cache_hits_total[5m]) / rate(redis_cache_operations_total[5m])'
   ```

4. **System Resources**
   ```bash
   # Go runtime metrics
   curl -s 'http://prometheus:9090/api/v1/query?query=go_memstats_heap_inuse_bytes'
   curl -s 'http://prometheus:9090/api/v1/query?query=go_goroutines'
   ```

### Resolution Steps

1. **Immediate Scaling**
   ```bash
   # Scale up application
   kubectl scale deployment -n tmi tmi-api --replicas=6
   
   # Monitor impact
   watch kubectl top pods -n tmi
   ```

2. **Database Optimization**
   ```bash
   # Check for long-running queries
   kubectl exec -n tmi postgres-0 -- psql -U tmi -c "SELECT query, state, query_start FROM pg_stat_activity WHERE state = 'active' AND query_start < NOW() - INTERVAL '30 seconds';"
   
   # Kill long-running queries if necessary
   kubectl exec -n tmi postgres-0 -- psql -U tmi -c "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE state = 'active' AND query_start < NOW() - INTERVAL '5 minutes';"
   ```

3. **Cache Optimization**
   ```bash
   # Clear cache if needed
   kubectl exec -n tmi redis-0 -- redis-cli FLUSHALL
   
   # Check memory usage
   kubectl exec -n tmi redis-0 -- redis-cli INFO memory
   ```

## Database Issues

### Symptoms
- Database connection errors
- High query latency
- Connection pool exhaustion

### Investigation Steps

1. **Check Database Health**
   ```bash
   kubectl logs -n tmi postgres-0 | tail -50
   kubectl exec -n tmi postgres-0 -- pg_isready
   ```

2. **Connection Analysis**
   ```bash
   kubectl exec -n tmi postgres-0 -- psql -U tmi -c "SELECT count(*) FROM pg_stat_activity;"
   kubectl exec -n tmi postgres-0 -- psql -U tmi -c "SELECT state, count(*) FROM pg_stat_activity GROUP BY state;"
   ```

3. **Query Performance**
   ```bash
   kubectl exec -n tmi postgres-0 -- psql -U tmi -c "SELECT query, calls, total_time, mean_time FROM pg_stat_statements ORDER BY mean_time DESC LIMIT 10;"
   ```

### Resolution Steps

1. **Connection Issues**
   ```bash
   # Restart application pods to reset connections
   kubectl rollout restart deployment -n tmi tmi-api
   
   # Increase connection limits if needed
   kubectl exec -n tmi postgres-0 -- psql -U tmi -c "ALTER SYSTEM SET max_connections = 200;"
   kubectl exec -n tmi postgres-0 -- psql -U tmi -c "SELECT pg_reload_conf();"
   ```

2. **Performance Issues**
   ```bash
   # Analyze and optimize slow queries
   kubectl exec -n tmi postgres-0 -- psql -U tmi -c "ANALYZE;"
   
   # Update table statistics
   kubectl exec -n tmi postgres-0 -- psql -U tmi -c "VACUUM ANALYZE;"
   ```

## Cache Problems

### Symptoms
- High cache miss rates
- Redis connection errors
- Memory issues

### Investigation Steps

1. **Check Redis Health**
   ```bash
   kubectl logs -n tmi redis-0 | tail -50
   kubectl exec -n tmi redis-0 -- redis-cli ping
   ```

2. **Memory Analysis**
   ```bash
   kubectl exec -n tmi redis-0 -- redis-cli INFO memory
   kubectl exec -n tmi redis-0 -- redis-cli INFO stats
   ```

3. **Connection Analysis**
   ```bash
   kubectl exec -n tmi redis-0 -- redis-cli INFO clients
   kubectl exec -n tmi redis-0 -- redis-cli CLIENT LIST
   ```

### Resolution Steps

1. **Memory Issues**
   ```bash
   # Clear expired keys
   kubectl exec -n tmi redis-0 -- redis-cli --scan --pattern "*" | xargs kubectl exec -n tmi redis-0 -- redis-cli DEL
   
   # Adjust memory policy
   kubectl exec -n tmi redis-0 -- redis-cli CONFIG SET maxmemory-policy allkeys-lru
   ```

2. **Connection Issues**
   ```bash
   # Restart Redis
   kubectl delete pod -n tmi redis-0
   
   # Restart application to reset connections
   kubectl rollout restart deployment -n tmi tmi-api
   ```

## Security Incidents

### Symptoms
- Unusual access patterns
- Authentication failures
- Suspicious log entries

### Investigation Steps

1. **Review Security Logs**
   ```bash
   kubectl logs -n tmi -l app=tmi-api | grep -E "(unauthorized|forbidden|authentication|suspicious)"
   ```

2. **Check Access Patterns**
   ```bash
   # Failed authentication attempts
   curl -s 'http://prometheus:9090/api/v1/query?query=rate(http_requests_total{status="401"}[5m])'
   
   # Geographic analysis (if available)
   kubectl logs -n tmi -l app=tmi-api | grep -E "client_ip" | sort | uniq -c | sort -nr
   ```

3. **Verify System Integrity**
   ```bash
   # Check for unexpected processes
   kubectl exec -n tmi <pod-name> -- ps aux
   
   # Verify file integrity
   kubectl exec -n tmi <pod-name> -- find /app -type f -name "*.go" -exec sha256sum {} \;
   ```

### Response Steps

1. **Immediate Actions**
   ```bash
   # Block suspicious IPs (if using ingress with IP filtering)
   kubectl patch ingress -n tmi tmi-ingress --patch '{"metadata":{"annotations":{"nginx.ingress.kubernetes.io/server-snippet":"deny <suspicious-ip>;"}}}'
   
   # Increase authentication logging
   kubectl set env deployment -n tmi tmi-api LOG_LEVEL=debug
   ```

2. **Evidence Collection**
   ```bash
   # Export relevant logs
   kubectl logs -n tmi -l app=tmi-api --since=1h > incident-logs-$(date +%Y%m%d-%H%M%S).log
   
   # Capture network state
   kubectl exec -n tmi <pod-name> -- netstat -tulpn > network-state-$(date +%Y%m%d-%H%M%S).txt
   ```

## Observability System Issues

### Symptoms
- Missing metrics or traces
- Grafana dashboard failures
- Log collection problems

### Investigation Steps

1. **Check Collector Health**
   ```bash
   kubectl logs -n tmi jaeger-collector
   kubectl logs -n tmi prometheus
   ```

2. **Verify Data Flow**
   ```bash
   # Check OTLP endpoint connectivity
   kubectl exec -n tmi <api-pod> -- curl -v http://jaeger-collector:4318/v1/traces
   
   # Verify metrics endpoint
   curl http://tmi-api:8080/metrics
   ```

3. **Storage Issues**
   ```bash
   # Check storage usage
   kubectl exec -n tmi prometheus-0 -- df -h /prometheus
   
   # Verify data retention
   kubectl logs -n tmi prometheus-0 | grep -i retention
   ```

### Resolution Steps

1. **Restart Components**
   ```bash
   # Restart Jaeger
   kubectl delete pod -n tmi -l app=jaeger
   
   # Restart Prometheus
   kubectl delete pod -n tmi prometheus-0
   
   # Restart Grafana
   kubectl delete pod -n tmi -l app=grafana
   ```

2. **Clear Storage Issues**
   ```bash
   # Clean old data
   kubectl exec -n tmi prometheus-0 -- find /prometheus -name "*.tmp" -delete
   
   # Adjust retention if needed
   kubectl patch deployment -n tmi prometheus --patch '{"spec":{"template":{"spec":{"containers":[{"name":"prometheus","args":["--storage.tsdb.retention.time=15d"]}]}}}}'
   ```

## Escalation Procedures

### Severity Levels

**P0 - Critical**
- Service completely down
- Security breach
- Data loss
- Escalate immediately

**P1 - High**
- Significant performance degradation
- Partial service unavailability
- Escalate within 30 minutes

**P2 - Medium**
- Minor performance issues
- Non-critical feature failures
- Escalate within 2 hours

**P3 - Low**
- Cosmetic issues
- Enhancement requests
- Escalate within 24 hours

### Escalation Contacts

1. **Primary On-Call Engineer**: [Contact Information]
2. **Secondary On-Call Engineer**: [Contact Information]
3. **Engineering Manager**: [Contact Information]
4. **Infrastructure Team**: [Contact Information]
5. **Security Team**: [Contact Information]

### Communication

1. **Create Incident Channel**: #incident-YYYY-MM-DD-NNN
2. **Update Status Page**: [Status Page URL]
3. **Notify Stakeholders**: [Notification Process]
4. **Document Actions**: Record all steps taken

### Post-Incident

1. **Root Cause Analysis**: Complete within 48 hours
2. **Action Items**: Create and assign follow-up tasks
3. **Process Improvement**: Update runbooks based on learnings
4. **Communication**: Send post-mortem to stakeholders

## Tools and Resources

### Monitoring URLs
- Grafana: http://grafana:3000
- Prometheus: http://prometheus:9090
- Jaeger: http://jaeger:16686
- Application Metrics: http://tmi-api:8080/metrics

### Useful Commands
```bash
# Quick health check
kubectl get pods -n tmi && curl -s http://tmi-api:8080/health

# Resource usage overview
kubectl top pods -n tmi && kubectl top nodes

# Recent events
kubectl get events -n tmi --sort-by='.lastTimestamp' | head -10

# Log aggregation
kubectl logs -n tmi -l app=tmi-api --since=30m | grep -E "(ERROR|WARN|panic)"
```

### Configuration Files
- Prometheus config: `/config/prometheus.yml`
- Grafana dashboards: `/config/grafana/dashboards/`
- Application config: `/config/tmi/`

## Appendix

### Useful Prometheus Queries

```promql
# Error rate by endpoint
rate(http_requests_total{status=~"5.."}[5m]) by (endpoint)

# Memory usage trend
go_memstats_heap_inuse_bytes

# Database connection pool
postgres_connections_active / postgres_connections_max * 100

# Cache hit rate
rate(redis_cache_hits_total[5m]) / rate(redis_cache_operations_total[5m]) * 100

# Top slow endpoints
topk(10, histogram_quantile(0.95, rate(http_request_duration_seconds_bucket[5m])) by (endpoint))
```

### Log Analysis Commands

```bash
# Error frequency
kubectl logs -n tmi -l app=tmi-api --since=1h | grep ERROR | wc -l

# Unique error messages
kubectl logs -n tmi -l app=tmi-api --since=1h | grep ERROR | sort | uniq -c | sort -nr

# Request patterns
kubectl logs -n tmi -l app=tmi-api --since=1h | grep "HTTP" | awk '{print $X}' | sort | uniq -c | sort -nr
```

This runbook should be updated regularly based on operational experience and lessons learned from incidents.