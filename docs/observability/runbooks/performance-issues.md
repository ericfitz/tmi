# Performance Issues Runbook

This runbook provides specific procedures for diagnosing and resolving performance-related issues in the TMI application.

## Table of Contents

1. [Overview](#overview)
2. [Performance Indicators](#performance-indicators)
3. [High Response Time](#high-response-time)
4. [High CPU Usage](#high-cpu-usage)
5. [High Memory Usage](#high-memory-usage)
6. [Database Performance Issues](#database-performance-issues)
7. [Cache Performance Issues](#cache-performance-issues)
8. [Network Latency Issues](#network-latency-issues)
9. [Goroutine Leaks](#goroutine-leaks)
10. [Performance Optimization](#performance-optimization)

## Overview

Performance issues can manifest in various ways:
- Slow API responses
- High resource utilization
- User experience degradation
- Timeout errors
- Reduced throughput

This runbook provides systematic approaches to diagnose and resolve these issues.

## Performance Indicators

### Key Metrics to Monitor

1. **Response Time Metrics**
   ```promql
   # 95th percentile response time
   histogram_quantile(0.95, rate(http_request_duration_seconds_bucket[5m]))
   
   # 99th percentile response time
   histogram_quantile(0.99, rate(http_request_duration_seconds_bucket[5m]))
   
   # Average response time
   rate(http_request_duration_seconds_sum[5m]) / rate(http_request_duration_seconds_count[5m])
   ```

2. **Throughput Metrics**
   ```promql
   # Requests per second
   rate(http_requests_total[5m])
   
   # Successful requests per second
   rate(http_requests_total{status!~"5.."}[5m])
   ```

3. **Resource Utilization**
   ```promql
   # CPU usage
   rate(process_cpu_seconds_total[5m]) * 100
   
   # Memory usage
   go_memstats_heap_inuse_bytes / 1024 / 1024
   
   # Goroutine count
   go_goroutines
   ```

### Performance Thresholds

| Metric | Good | Warning | Critical |
|--------|------|---------|----------|
| 95th percentile response time | < 500ms | 500ms - 2s | > 2s |
| Error rate | < 1% | 1% - 5% | > 5% |
| CPU utilization | < 70% | 70% - 85% | > 85% |
| Memory usage | < 70% | 70% - 85% | > 85% |
| Goroutines | < 1000 | 1000 - 5000 | > 5000 |

## High Response Time

### Symptoms
- P95 response time > 2 seconds
- User complaints about slow responses
- Timeout errors increasing

### Investigation Steps

1. **Identify Slow Endpoints**
   ```bash
   # Top 10 slowest endpoints
   curl -s 'http://prometheus:9090/api/v1/query?query=topk(10, histogram_quantile(0.95, rate(http_request_duration_seconds_bucket[5m])) by (endpoint))'
   ```

2. **Analyze Request Patterns**
   ```bash
   # Request rate by endpoint
   curl -s 'http://prometheus:9090/api/v1/query?query=rate(http_requests_total[5m]) by (endpoint)'
   
   # Request size distribution
   curl -s 'http://prometheus:9090/api/v1/query?query=histogram_quantile(0.95, rate(http_request_size_bytes_bucket[5m]))'
   ```

3. **Check Trace Data**
   - Open Jaeger UI: http://jaeger:16686
   - Search for traces with high duration (> 2s)
   - Analyze trace spans to identify bottlenecks

4. **Database Query Analysis**
   ```bash
   # Slow database queries
   curl -s 'http://prometheus:9090/api/v1/query?query=rate(db_slow_queries_total[5m])'
   
   # Database connection pool usage
   curl -s 'http://prometheus:9090/api/v1/query?query=db_connections_active / db_connections_max * 100'
   ```

### Resolution Steps

1. **Immediate Actions**
   ```bash
   # Scale up application instances
   kubectl scale deployment -n tmi tmi-api --replicas=5
   
   # Check resource limits
   kubectl describe pod -n tmi <pod-name> | grep -A 5 "Limits:"
   ```

2. **Database Optimization**
   ```bash
   # Check for missing indexes
   kubectl exec -n tmi postgres-0 -- psql -U tmi -c "SELECT schemaname, tablename, attname, n_distinct, correlation FROM pg_stats WHERE schemaname = 'public' ORDER BY n_distinct DESC;"
   
   # Analyze query plans
   kubectl exec -n tmi postgres-0 -- psql -U tmi -c "EXPLAIN ANALYZE SELECT * FROM threat_models WHERE created_at > NOW() - INTERVAL '1 day';"
   ```

3. **Cache Optimization**
   ```bash
   # Check cache hit rates
   curl -s 'http://prometheus:9090/api/v1/query?query=rate(redis_cache_hits_total[5m]) / rate(redis_cache_operations_total[5m]) * 100'
   
   # Warm up cache if needed
   kubectl exec -n tmi <api-pod> -- curl -X POST http://localhost:8080/admin/cache/warmup
   ```

4. **Code-Level Optimizations**
   - Review slow endpoints identified in traces
   - Optimize database queries
   - Implement caching for frequently accessed data
   - Add connection pooling optimizations

## High CPU Usage

### Symptoms
- CPU utilization > 85%
- Application becomes unresponsive
- Increased response times

### Investigation Steps

1. **CPU Usage Analysis**
   ```bash
   # Current CPU usage
   kubectl top pods -n tmi
   
   # CPU usage over time
   curl -s 'http://prometheus:9090/api/v1/query?query=rate(process_cpu_seconds_total[5m]) * 100'
   ```

2. **Profiling**
   ```bash
   # Get CPU profile
   kubectl exec -n tmi <pod-name> -- curl http://localhost:6060/debug/pprof/profile?seconds=30 > cpu-profile.prof
   
   # Analyze with go tool
   go tool pprof cpu-profile.prof
   ```

3. **Goroutine Analysis**
   ```bash
   # Check goroutine count
   curl -s 'http://prometheus:9090/api/v1/query?query=go_goroutines'
   
   # Get goroutine dump
   kubectl exec -n tmi <pod-name> -- curl http://localhost:6060/debug/pprof/goroutine?debug=1
   ```

### Resolution Steps

1. **Immediate Relief**
   ```bash
   # Scale horizontally
   kubectl scale deployment -n tmi tmi-api --replicas=6
   
   # Increase CPU limits if available
   kubectl patch deployment -n tmi tmi-api -p '{"spec":{"template":{"spec":{"containers":[{"name":"tmi-api","resources":{"limits":{"cpu":"1000m"}}}]}}}}'
   ```

2. **Long-term Solutions**
   - Optimize CPU-intensive algorithms
   - Implement request rate limiting
   - Add caching to reduce computation
   - Profile and optimize hot code paths

## High Memory Usage

### Symptoms
- Memory usage > 85% of limit
- Out of memory kills (OOMKilled)
- Garbage collection pressure

### Investigation Steps

1. **Memory Analysis**
   ```bash
   # Current memory usage
   kubectl top pods -n tmi
   
   # Memory metrics
   curl -s 'http://prometheus:9090/api/v1/query?query=go_memstats_heap_inuse_bytes / 1024 / 1024'
   curl -s 'http://prometheus:9090/api/v1/query?query=go_memstats_heap_alloc_bytes / 1024 / 1024'
   ```

2. **Garbage Collection Metrics**
   ```bash
   # GC frequency
   curl -s 'http://prometheus:9090/api/v1/query?query=rate(go_gc_duration_seconds_count[5m])'
   
   # GC pause time
   curl -s 'http://prometheus:9090/api/v1/query?query=go_gc_duration_seconds{quantile="0.95"}'
   ```

3. **Memory Profiling**
   ```bash
   # Get heap profile
   kubectl exec -n tmi <pod-name> -- curl http://localhost:6060/debug/pprof/heap > heap-profile.prof
   
   # Analyze memory usage
   go tool pprof heap-profile.prof
   ```

### Resolution Steps

1. **Immediate Actions**
   ```bash
   # Increase memory limits
   kubectl patch deployment -n tmi tmi-api -p '{"spec":{"template":{"spec":{"containers":[{"name":"tmi-api","resources":{"limits":{"memory":"1Gi"}}}]}}}}'
   
   # Restart pods to clear memory
   kubectl rollout restart deployment -n tmi tmi-api
   ```

2. **Optimization Steps**
   ```bash
   # Reduce telemetry batch sizes
   kubectl set env deployment -n tmi tmi-api OTEL_TRACING_BATCH_SIZE=256
   kubectl set env deployment -n tmi tmi-api OTEL_METRICS_BATCH_SIZE=512
   
   # Enable more aggressive GC
   kubectl set env deployment -n tmi tmi-api GOGC=50
   ```

3. **Code Optimizations**
   - Fix memory leaks identified in profiles
   - Optimize data structures
   - Implement object pooling for frequent allocations
   - Add memory-efficient caching strategies

## Database Performance Issues

### Symptoms
- High database query latency
- Connection pool exhaustion
- Lock contention

### Investigation Steps

1. **Database Metrics**
   ```bash
   # Query performance
   curl -s 'http://prometheus:9090/api/v1/query?query=histogram_quantile(0.95, rate(db_query_duration_seconds_bucket[5m]))'
   
   # Connection pool usage
   curl -s 'http://prometheus:9090/api/v1/query?query=db_connections_active'
   
   # Slow query count
   curl -s 'http://prometheus:9090/api/v1/query?query=rate(db_slow_queries_total[5m])'
   ```

2. **Database Analysis**
   ```bash
   # Active queries
   kubectl exec -n tmi postgres-0 -- psql -U tmi -c "SELECT pid, now() - pg_stat_activity.query_start AS duration, query FROM pg_stat_activity WHERE (now() - pg_stat_activity.query_start) > interval '5 minutes';"
   
   # Lock analysis
   kubectl exec -n tmi postgres-0 -- psql -U tmi -c "SELECT blocked_locks.pid AS blocked_pid, blocked_activity.usename AS blocked_user, blocking_locks.pid AS blocking_pid, blocking_activity.usename AS blocking_user, blocked_activity.query AS blocked_statement, blocking_activity.query AS current_statement_in_blocking_process FROM pg_catalog.pg_locks blocked_locks JOIN pg_catalog.pg_stat_activity blocked_activity ON blocked_activity.pid = blocked_locks.pid JOIN pg_catalog.pg_locks blocking_locks ON blocking_locks.locktype = blocked_locks.locktype AND blocking_locks.DATABASE IS NOT DISTINCT FROM blocked_locks.DATABASE AND blocking_locks.relation IS NOT DISTINCT FROM blocked_locks.relation AND blocking_locks.page IS NOT DISTINCT FROM blocked_locks.page AND blocking_locks.tuple IS NOT DISTINCT FROM blocked_locks.tuple AND blocking_locks.virtualxid IS NOT DISTINCT FROM blocked_locks.virtualxid AND blocking_locks.transactionid IS NOT DISTINCT FROM blocked_locks.transactionid AND blocking_locks.classid IS NOT DISTINCT FROM blocked_locks.classid AND blocking_locks.objid IS NOT DISTINCT FROM blocked_locks.objid AND blocking_locks.objsubid IS NOT DISTINCT FROM blocked_locks.objsubid AND blocking_locks.pid != blocked_locks.pid JOIN pg_catalog.pg_stat_activity blocking_activity ON blocking_activity.pid = blocking_locks.pid WHERE NOT blocked_locks.GRANTED;"
   ```

### Resolution Steps

1. **Connection Pool Optimization**
   ```bash
   # Increase connection pool size
   kubectl set env deployment -n tmi tmi-api DB_MAX_CONNECTIONS=50
   kubectl set env deployment -n tmi tmi-api DB_MAX_IDLE_CONNECTIONS=10
   
   # Reduce connection lifetime
   kubectl set env deployment -n tmi tmi-api DB_MAX_LIFETIME=30m
   ```

2. **Query Optimization**
   ```bash
   # Update table statistics
   kubectl exec -n tmi postgres-0 -- psql -U tmi -c "ANALYZE;"
   
   # Vacuum tables
   kubectl exec -n tmi postgres-0 -- psql -U tmi -c "VACUUM ANALYZE;"
   
   # Check for missing indexes
   kubectl exec -n tmi postgres-0 -- psql -U tmi -c "SELECT schemaname, tablename, seq_scan, seq_tup_read, idx_scan, idx_tup_fetch FROM pg_stat_user_tables WHERE seq_scan > 1000 ORDER BY seq_scan DESC;"
   ```

3. **Database Configuration**
   ```bash
   # Increase shared buffers (if needed)
   kubectl exec -n tmi postgres-0 -- psql -U tmi -c "ALTER SYSTEM SET shared_buffers = '256MB';"
   
   # Optimize work memory
   kubectl exec -n tmi postgres-0 -- psql -U tmi -c "ALTER SYSTEM SET work_mem = '8MB';"
   
   # Reload configuration
   kubectl exec -n tmi postgres-0 -- psql -U tmi -c "SELECT pg_reload_conf();"
   ```

## Cache Performance Issues

### Symptoms
- Low cache hit rates
- High cache operation latency
- Memory pressure in Redis

### Investigation Steps

1. **Cache Metrics**
   ```bash
   # Hit rate
   curl -s 'http://prometheus:9090/api/v1/query?query=rate(redis_cache_hits_total[5m]) / rate(redis_cache_operations_total[5m]) * 100'
   
   # Operation latency
   curl -s 'http://prometheus:9090/api/v1/query?query=histogram_quantile(0.95, rate(redis_operation_duration_seconds_bucket[5m]))'
   
   # Memory usage
   curl -s 'http://prometheus:9090/api/v1/query?query=redis_memory_used_bytes / redis_memory_max_bytes * 100'
   ```

2. **Redis Analysis**
   ```bash
   # Redis info
   kubectl exec -n tmi redis-0 -- redis-cli INFO stats
   kubectl exec -n tmi redis-0 -- redis-cli INFO memory
   
   # Slow operations
   kubectl exec -n tmi redis-0 -- redis-cli SLOWLOG get 10
   ```

### Resolution Steps

1. **Cache Optimization**
   ```bash
   # Adjust TTL values
   kubectl exec -n tmi redis-0 -- redis-cli CONFIG SET timeout 300
   
   # Optimize memory policy
   kubectl exec -n tmi redis-0 -- redis-cli CONFIG SET maxmemory-policy allkeys-lru
   
   # Clear expired keys
   kubectl exec -n tmi redis-0 -- redis-cli --scan --pattern "*" | xargs kubectl exec -n tmi redis-0 -- redis-cli TTL | grep -c "^-1$"
   ```

2. **Application-Level Optimizations**
   - Review caching strategies
   - Optimize cache key patterns
   - Implement cache warming
   - Add cache invalidation logic

## Network Latency Issues

### Symptoms
- High network roundtrip times
- Connection timeouts
- Intermittent connectivity issues

### Investigation Steps

1. **Network Analysis**
   ```bash
   # Test connectivity
   kubectl exec -n tmi <pod-name> -- ping -c 5 postgres
   kubectl exec -n tmi <pod-name> -- ping -c 5 redis
   
   # DNS resolution
   kubectl exec -n tmi <pod-name> -- nslookup postgres
   kubectl exec -n tmi <pod-name> -- nslookup redis
   ```

2. **Service Mesh Analysis (if applicable)**
   ```bash
   # Istio proxy stats
   kubectl exec -n tmi <pod-name> -c istio-proxy -- curl -s http://localhost:15000/stats | grep http
   ```

### Resolution Steps

1. **Network Optimization**
   ```bash
   # Check service endpoints
   kubectl get endpoints -n tmi
   
   # Verify network policies
   kubectl get networkpolicies -n tmi
   
   # Test service resolution
   kubectl run test-pod --image=busybox --rm -it -- nslookup tmi-api-service.tmi.svc.cluster.local
   ```

## Goroutine Leaks

### Symptoms
- Continuously increasing goroutine count
- Memory usage growth over time
- Application becomes unresponsive

### Investigation Steps

1. **Goroutine Analysis**
   ```bash
   # Monitor goroutine count
   curl -s 'http://prometheus:9090/api/v1/query?query=go_goroutines'
   
   # Get goroutine dump
   kubectl exec -n tmi <pod-name> -- curl http://localhost:6060/debug/pprof/goroutine?debug=2 > goroutines.txt
   
   # Analyze goroutine patterns
   grep -E "^goroutine [0-9]+" goroutines.txt | wc -l
   grep -E "goroutine.*\[.*waiting\]:" goroutines.txt | head -10
   ```

2. **Trace Analysis**
   - Look for long-running operations in Jaeger
   - Check for blocked operations
   - Identify resource leaks

### Resolution Steps

1. **Immediate Actions**
   ```bash
   # Restart affected pods
   kubectl delete pod -n tmi <pod-name>
   
   # Monitor goroutine count after restart
   watch -n 5 'kubectl exec -n tmi <new-pod> -- curl -s http://localhost:6060/debug/pprof/goroutine?debug=1 | grep "goroutine profile" | head -1'
   ```

2. **Code Fixes**
   - Fix resource leaks in goroutines
   - Add proper context cancellation
   - Implement goroutine lifecycle management
   - Add monitoring for goroutine patterns

## Performance Optimization

### General Optimization Strategies

1. **Profiling and Monitoring**
   ```bash
   # Enable profiling endpoints
   kubectl set env deployment -n tmi tmi-api ENABLE_PPROF=true
   
   # Set up continuous profiling
   kubectl apply -f monitoring/profiling-config.yaml
   ```

2. **Resource Tuning**
   ```bash
   # Optimize garbage collector
   kubectl set env deployment -n tmi tmi-api GOGC=100
   kubectl set env deployment -n tmi tmi-api GOMAXPROCS=4
   
   # Tune telemetry settings
   kubectl set env deployment -n tmi tmi-api OTEL_PERFORMANCE_PROFILE=high
   ```

3. **Caching Strategies**
   - Implement multi-level caching
   - Use appropriate cache TTLs
   - Implement cache warming
   - Add cache invalidation logic

### Performance Testing

1. **Load Testing**
   ```bash
   # Run load tests
   kubectl apply -f testing/load-test-job.yaml
   
   # Monitor during load test
   watch -n 5 'kubectl top pods -n tmi'
   ```

2. **Benchmark Comparisons**
   ```bash
   # Before optimization
   kubectl logs -n tmi load-test-before > benchmark-before.log
   
   # After optimization
   kubectl logs -n tmi load-test-after > benchmark-after.log
   
   # Compare results
   diff benchmark-before.log benchmark-after.log
   ```

### Monitoring and Alerting

1. **Performance Alerts**
   ```yaml
   # Add to Prometheus alerts
   - alert: HighResponseTime
     expr: histogram_quantile(0.95, rate(http_request_duration_seconds_bucket[5m])) > 2
     for: 5m
     annotations:
       summary: "High response time detected"
   
   - alert: HighCPUUsage
     expr: rate(process_cpu_seconds_total[5m]) * 100 > 85
     for: 5m
     annotations:
       summary: "High CPU usage detected"
   ```

2. **Performance Dashboards**
   - Create performance overview dashboard
   - Add resource utilization panels
   - Include application-specific metrics
   - Set up automated reporting

This runbook should be regularly updated based on performance analysis and optimization efforts.