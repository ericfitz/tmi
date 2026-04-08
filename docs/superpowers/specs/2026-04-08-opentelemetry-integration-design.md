# OpenTelemetry Integration Design

**Issue:** [#150](https://github.com/ericfitz/tmi/issues/150)
**Date:** 2026-04-08
**Branch:** dev/1.4.0

## Summary

Add OpenTelemetry traces and metrics to TMI using the OTel Go SDK with OTLP export. Replace the in-memory PerformanceMonitor with OTel metrics. Instrument HTTP requests (inbound and outbound), GORM database queries, Redis operations, SSE streams, and Timmy chat sessions. Leave existing slog-based logging untouched.

## Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Export protocol | Vendor-agnostic OTLP | Deploy-time backend choice via `OTEL_*` env vars |
| Signals | Traces + metrics (no log bridging) | slog logging is already solid; bridging adds complexity with marginal benefit |
| Instrumentation depth | HTTP + DB + Redis + SSE/Timmy | Covers the full request critical path plus Timmy business metrics |
| Configuration | Hybrid: TMI toggle + standard `OTEL_*` | Clean enable/disable without duplicating OTel's config surface |
| Metrics scope | RED + infrastructure + Timmy | Replaces PerformanceMonitor data plus Timmy-specific operational metrics |
| PerformanceMonitor | Replace | OTel metrics subsume all its data points; keeping both creates dual-system debt |
| Approach | OTel SDK direct | Full control, explicit wiring, matches TMI's hand-wired middleware style |

## Design

### 1. SDK Setup & Configuration

**New package:** `internal/otel/`

A single entry point:

```go
func Setup(ctx context.Context, cfg ObservabilityConfig) (shutdown func(context.Context) error, err error)
```

Called from `cmd/server/main.go` during startup. Creates:
- `TracerProvider` with OTLP gRPC exporter (falls back to stdout exporter in dev if no endpoint configured)
- `MeterProvider` with OTLP gRPC exporter + optional Prometheus HTTP endpoint for local scraping
- Registers both as global providers
- Returns a `shutdown` function called on graceful server stop

**TMI configuration:**

```yaml
observability:
  enabled: true          # master kill switch, default false
  sampling_rate: 1.0     # 0.0-1.0, default 1.0
  prometheus_port: 9090  # optional, 0 = disabled
```

Environment variable overrides: `TMI_OTEL_ENABLED`, `TMI_OTEL_SAMPLING_RATE`, `TMI_OTEL_PROMETHEUS_PORT`.

All other OTel configuration (endpoint, service name, resource attributes, headers, compression, etc.) uses standard `OTEL_*` environment variables per the OpenTelemetry specification.

**When disabled:** No-op tracer and meter providers are registered. All span creation and metric recording calls succeed silently with zero overhead. No code paths need conditional guards.

### 2. HTTP Trace Instrumentation

#### Inbound Requests

Add `otelgin.Middleware("tmi")` to the Gin router, placed early in the middleware stack (after recovery, before auth). This auto-generates a span per request with HTTP method, route, status code, and duration following OTel HTTP semantic conventions.

#### Span Enrichment

A custom middleware placed after JWT auth adds TMI-specific attributes to the active span:
- `tmi.user.id` — from auth context
- `tmi.threat_model.id` — from resource middleware (when applicable)
- `tmi.diagram.id` — from resource middleware (when applicable)
- `tmi.request.id` — existing request ID from tracing middleware

#### Outbound HTTP Calls

Wrap outbound `http.Client` instances with `otelhttp.NewTransport()` to auto-generate client spans and propagate W3C `traceparent` headers. Applies to:

| Client | File | Timeout |
|--------|------|---------|
| LLM API calls | `api/timmy_llm_service.go` | Configurable (default 120s) |
| Webhook delivery | `api/webhook_base_worker.go` | 30s |
| Webhook challenges | `api/webhook_challenge_worker.go` | 10s |
| HTTP content provider | `api/timmy_content_provider_http.go` | 30s |
| OAuth token exchange | `auth/provider.go` | 10s |

### 3. Database & Redis Trace Instrumentation

#### GORM Tracing

Register the `otelgorm` plugin via `db.Use(otelgorm.NewPlugin())` in `auth/db/gorm.go` after connection creation. This auto-generates spans for every query with `db.system`, `db.statement` (sanitized), and `db.operation` attributes.

#### Redis Tracing (Requires go-redis v8 to v9 Upgrade)

TMI currently uses `github.com/go-redis/redis/v8`. The OTel contrib instrumentation (`redisotel`) targets `github.com/redis/go-redis/v9`.

**Prerequisite:** Upgrade go-redis from v8 to v9. The migration is primarily import path changes (`github.com/go-redis/redis/v8` to `github.com/redis/go-redis/v9`). The API is largely compatible for TMI's usage patterns (Get, Set, Del, Pipeline).

After upgrade, add `redisotel.InstrumentClient(client)` in `auth/db/redis.go` after client creation.

### 4. SSE & Timmy Instrumentation

SSE and Timmy chat are long-lived operations that don't fit the standard request-span model.

#### SSE Stream Spans

A single parent span covers the SSE connection lifetime, from handler entry to stream close. Attributes:
- `tmi.sse.stream_type` — `"chat_session_create"` or `"chat_message"`
- `tmi.sse.session_id` — Timmy session ID
- `tmi.sse.event_count` — incremented as events are sent, recorded at span end

#### Timmy Chat Message Child Spans

Under the SSE parent span for message handling:

1. **`timmy.context.build`** — Tier 1 (snapshot summarization) + Tier 2 (vector search) context assembly
   - Attributes: `tmi.timmy.tier1_entities`, `tmi.timmy.tier2_results`
2. **`timmy.llm.generate`** — LLM API call, from request to final token
   - Attributes: `tmi.timmy.model`, `tmi.timmy.token_count`, `tmi.timmy.prompt_tokens`, `tmi.timmy.completion_tokens`
3. **`timmy.embedding.generate`** — embedding calls for vector search
   - Attributes: `tmi.timmy.embedding_model`, `tmi.timmy.text_count`

#### Timmy Session Creation Child Spans

Under the SSE parent span for session creation:

1. **`timmy.session.snapshot`** — entity snapshotting
2. **`timmy.session.index_prepare`** — vector index preparation (when enabled)

#### Timmy Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `tmi.timmy.session.active` | UpDownCounter | Currently active chat sessions |
| `tmi.timmy.llm.duration` | Histogram | LLM call latency (seconds) |
| `tmi.timmy.llm.tokens` | Counter | Total tokens consumed, with `direction` attribute (prompt/completion) |
| `tmi.timmy.embedding.duration` | Histogram | Embedding call latency (seconds) |
| `tmi.timmy.sse.duration` | Histogram | SSE stream total duration (seconds) |
| `tmi.timmy.sse.events` | Counter | SSE events sent, with `event_type` attribute |

### 5. Infrastructure Metrics & PerformanceMonitor Replacement

#### RED Metrics (Automatic)

Provided by otelgin middleware using OTel semantic conventions:
- `http.server.request.duration` — histogram by route, method, status
- `http.server.active_requests` — gauge of in-flight requests

#### Custom Infrastructure Metrics

| Metric | Type | Source |
|--------|------|--------|
| `tmi.db.pool.open` | Observable Gauge | `sql.DBStats.OpenConnections` |
| `tmi.db.pool.idle` | Observable Gauge | `sql.DBStats.Idle` |
| `tmi.db.pool.in_use` | Observable Gauge | `sql.DBStats.InUse` |
| `tmi.db.pool.wait_count` | Observable Counter | `sql.DBStats.WaitCount` |
| `tmi.db.pool.wait_duration` | Observable Counter | `sql.DBStats.WaitDuration` |
| `tmi.redis.pool.active` | Observable Gauge | go-redis `PoolStats().ActiveCount` |
| `tmi.redis.pool.idle` | Observable Gauge | go-redis `PoolStats().IdleCount` |
| `tmi.cache.hit` | Counter | `api/cache_service.go` |
| `tmi.cache.miss` | Counter | `api/cache_service.go` |
| `tmi.websocket.sessions.active` | UpDownCounter | WebSocketHub |
| `tmi.websocket.participants.active` | UpDownCounter | WebSocketHub |
| `tmi.websocket.messages` | Counter | WebSocketHub, with `direction` attribute (inbound/outbound) |
| `tmi.webhook.deliveries` | Counter | Delivery worker, with `status` attribute (success/failure) |

#### Pool Metrics Collection

DB and Redis pool metrics use OTel async instruments (observable gauges with callbacks registered during setup). The OTel SDK calls these callbacks at its configured collection interval. This replaces the health monitor's periodic stats logging with exportable metrics.

#### PerformanceMonitor Removal

Delete `api/performance_monitor.go`. Data point mapping:

| PerformanceMonitor | OTel Replacement |
|--------------------|------------------|
| Operation count/latency | `http.server.request.duration` + trace spans |
| Message count/bytes | `tmi.websocket.messages` |
| Connection tracking | `tmi.websocket.sessions.active` |
| Peak concurrency | `tmi.websocket.participants.active` (queryable in metrics backend) |

Remove all references to PerformanceMonitor in `cmd/server/main.go` and WebSocket code.

### 6. Testing & Graceful Degradation

#### Graceful Degradation

- **OTel disabled** (`observability.enabled: false`, the default): No-op providers registered. Zero overhead.
- **OTLP endpoint unreachable:** OTel SDK buffers and retries per built-in retry policy. Failed exports log a warning via slog. Never panics or blocks request handling.
- **Unit tests:** Run with OTel disabled by default. No test needs a collector.

#### Testing Strategy

- **Unit tests for `internal/otel.Setup()`:** Verify shutdown function, provider registration, disabled config handling.
- **Unit tests for span enrichment middleware:** Verify TMI attributes added to active span using `go.opentelemetry.io/otel/sdk/trace/tracetest` in-memory exporter.
- **Integration tests:** Existing tests run unchanged (OTel disabled). A small number of OTel-specific integration tests verify traces and metrics are emitted when enabled, using in-memory exporters.
- **No external collector required for CI.** Tests use `tracetest` and `metricdata` packages for in-process assertions.

#### Trace-Log Correlation

When OTel is enabled, the context logger gains a `trace_id` attribute from the active span context. This correlates slog entries with distributed traces without bridging logs into OTel.

Existing request tracing middleware (`api/request_tracing.go`) remains for its debug logging role. Its request IDs coexist with trace IDs.

## Files Changed

| File | Change |
|------|--------|
| `internal/otel/otel.go` | NEW: Setup, shutdown, provider registration |
| `internal/otel/metrics.go` | NEW: Pool metrics callbacks, metric instrument registration |
| `internal/config/config.go` | Add `ObservabilityConfig` struct and env var overrides |
| `cmd/server/main.go` | Call `otel.Setup()`, add otelgin middleware, add span enrichment middleware, remove PerformanceMonitor |
| `auth/db/gorm.go` | Register otelgorm plugin |
| `auth/db/redis.go` | Upgrade to go-redis v9, add redisotel instrumentation |
| `api/timmy_handlers.go` | Add SSE parent spans and Timmy child spans |
| `api/timmy_session_manager.go` | Add spans for context build, snapshot, index prep |
| `api/timmy_llm_service.go` | Add spans for LLM generate and embedding, wrap HTTP client, record token metrics |
| `api/timmy_sse.go` | Add event counter metric to SendEvent |
| `api/timmy_context_builder.go` | Add span for context assembly |
| `api/webhook_base_worker.go` | Wrap HTTP client with otelhttp transport |
| `api/webhook_delivery_worker.go` | Add delivery status metric |
| `api/timmy_content_provider_http.go` | Wrap HTTP client with otelhttp transport |
| `auth/provider.go` | Wrap HTTP client with otelhttp transport |
| `api/cache_service.go` | Replace internal metrics with OTel counters |
| `api/websocket.go` / `api/websocket_hub.go` | Replace PerformanceMonitor calls with OTel metrics |
| `api/performance_monitor.go` | DELETE |
| `go.mod` | Add OTel deps, upgrade go-redis v8→v9 |
| All files importing go-redis v8 | Update import paths to v9 |

## Dependencies Added

- `go.opentelemetry.io/otel`
- `go.opentelemetry.io/otel/sdk`
- `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc`
- `go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc`
- `go.opentelemetry.io/otel/exporters/prometheus` (optional Prometheus endpoint)
- `go.opentelemetry.io/otel/exporters/stdout/stdouttrace` (dev fallback)
- `go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin`
- `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp`
- `github.com/uptrace/opentelemetry-go-extra/otelgorm`
- `github.com/redis/go-redis/v9` (upgrade from v8)
- `github.com/redis/go-redis/extra/redisotel/v9`

## Out of Scope

- OTel log bridging (slog continues as-is)
- WebSocket message-level tracing (different model, separate design)
- Business metrics beyond Timmy (threat model counts, auth flow rates — add incrementally later)
- Alerting rules or dashboard definitions (backend-specific, configured at deploy time)
