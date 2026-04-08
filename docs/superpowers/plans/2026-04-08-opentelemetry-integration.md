# OpenTelemetry Integration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add OpenTelemetry traces and metrics to TMI, replacing the in-memory PerformanceMonitor with exportable OTel metrics.

**Architecture:** OTel SDK direct integration using OTLP gRPC export. `internal/otel/` package handles setup/shutdown. Gin, GORM, and Redis get auto-instrumented via community contrib packages. SSE/Timmy get manual spans. A hybrid config model uses a TMI-side toggle with standard `OTEL_*` env vars for everything else.

**Tech Stack:** go.opentelemetry.io/otel SDK, otelgin, otelhttp, otelgorm (uptrace), redisotel (go-redis v9), OTLP gRPC exporters, optional Prometheus exporter.

**Spec:** `docs/superpowers/specs/2026-04-08-opentelemetry-integration-design.md`

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/otel/otel.go` | NEW: TracerProvider + MeterProvider setup, shutdown, global registration |
| `internal/otel/otel_test.go` | NEW: Tests for setup, shutdown, disabled mode |
| `internal/otel/metrics.go` | NEW: DB/Redis pool metric callbacks, metric instrument registration |
| `internal/otel/metrics_test.go` | NEW: Tests for pool metrics callbacks |
| `internal/config/config.go` | MODIFY: Add ObservabilityConfig struct |
| `internal/config/config_test.go` | MODIFY: Add tests for ObservabilityConfig defaults and env overrides |
| `auth/db/redis.go` | MODIFY: Upgrade to go-redis v9, add redisotel |
| `auth/db/gorm.go` | MODIFY: Add otelgorm plugin registration |
| `cmd/server/main.go` | MODIFY: Call otel.Setup(), add otelgin middleware, span enrichment, remove PerformanceMonitor |
| `api/otel_middleware.go` | NEW: Span enrichment middleware (adds TMI attributes to active span) |
| `api/otel_middleware_test.go` | NEW: Tests for span enrichment |
| `api/timmy_handlers.go` | MODIFY: Add SSE parent spans |
| `api/timmy_session_manager.go` | MODIFY: Add child spans for context build, snapshot, index prep |
| `api/timmy_llm_service.go` | MODIFY: Add spans for LLM/embedding, wrap HTTP client, token metrics |
| `api/timmy_sse.go` | MODIFY: Add event counter metric |
| `api/webhook_base_worker.go` | MODIFY: Wrap HTTP client with otelhttp transport |
| `api/timmy_content_provider_http.go` | MODIFY: Wrap HTTP client with otelhttp transport |
| `auth/provider.go` | MODIFY: Wrap HTTP client with otelhttp transport |
| `api/cache_service.go` | MODIFY: Add OTel cache hit/miss counters |
| `api/websocket.go` | MODIFY: Replace PerformanceMonitor calls with OTel metrics |
| `api/performance_monitor.go` | DELETE |
| `go.mod` | MODIFY: Add OTel deps, upgrade go-redis |
| 23 files importing go-redis v8 | MODIFY: Update import paths to v9 |

---

## Task 1: Add ObservabilityConfig to Config

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

- [ ] **Step 1: Write the failing test for ObservabilityConfig defaults**

Add to `internal/config/config_test.go`:

```go
// =============================================================================
// Observability Config Tests
// =============================================================================

func TestObservabilityConfigDefaults(t *testing.T) {
	cfg := DefaultConfig()
	assert.False(t, cfg.Observability.Enabled, "observability should be disabled by default")
	assert.Equal(t, 1.0, cfg.Observability.SamplingRate, "sampling rate should default to 1.0")
	assert.Equal(t, 0, cfg.Observability.PrometheusPort, "prometheus port should default to 0 (disabled)")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit name=TestObservabilityConfigDefaults`
Expected: FAIL — `cfg.Observability` does not exist yet.

- [ ] **Step 3: Add ObservabilityConfig struct and wire it into Config**

In `internal/config/config.go`, add the struct after the existing SSRF config struct:

```go
// ObservabilityConfig holds OpenTelemetry configuration
type ObservabilityConfig struct {
	Enabled        bool    `yaml:"enabled" env:"TMI_OTEL_ENABLED"`
	SamplingRate   float64 `yaml:"sampling_rate" env:"TMI_OTEL_SAMPLING_RATE"`
	PrometheusPort int     `yaml:"prometheus_port" env:"TMI_OTEL_PROMETHEUS_PORT"`
}
```

Add the field to the Config struct:

```go
type Config struct {
	Server         ServerConfig          `yaml:"server"`
	Database       DatabaseConfig        `yaml:"database"`
	Auth           AuthConfig            `yaml:"auth"`
	WebSocket      WebSocketConfig       `yaml:"websocket"`
	Webhooks       WebhookConfig         `yaml:"webhooks"`
	Logging        LoggingConfig         `yaml:"logging"`
	Operator       OperatorConfig        `yaml:"operator"`
	Secrets        SecretsConfig         `yaml:"secrets"`
	Administrators []AdministratorConfig `yaml:"administrators"`
	Timmy          TimmyConfig           `yaml:"timmy"`
	SSRF           SSRFConfig            `yaml:"ssrf"`
	Observability  ObservabilityConfig   `yaml:"observability"`
}
```

Set defaults in `DefaultConfig()`:

```go
Observability: ObservabilityConfig{
	Enabled:        false,
	SamplingRate:   1.0,
	PrometheusPort: 0,
},
```

- [ ] **Step 4: Run test to verify it passes**

Run: `make test-unit name=TestObservabilityConfigDefaults`
Expected: PASS

- [ ] **Step 5: Write test for env var overrides**

Add to `internal/config/config_test.go`:

```go
func TestObservabilityConfigEnvOverrides(t *testing.T) {
	t.Setenv("TMI_OTEL_ENABLED", "true")
	t.Setenv("TMI_OTEL_SAMPLING_RATE", "0.5")
	t.Setenv("TMI_OTEL_PROMETHEUS_PORT", "9090")

	cfg := DefaultConfig()
	overrideWithEnv(cfg)

	assert.True(t, cfg.Observability.Enabled)
	assert.Equal(t, 0.5, cfg.Observability.SamplingRate)
	assert.Equal(t, 9090, cfg.Observability.PrometheusPort)
}
```

- [ ] **Step 6: Run test to verify it passes**

Run: `make test-unit name=TestObservabilityConfigEnvOverrides`
Expected: PASS — the existing `overrideWithEnv` uses struct tags reflectively, so it should handle the new fields automatically.

- [ ] **Step 7: Run full unit test suite**

Run: `make test-unit`
Expected: All tests pass.

- [ ] **Step 8: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): add ObservabilityConfig for OpenTelemetry settings"
```

---

## Task 2: Create internal/otel Package — Setup & Shutdown

**Files:**
- Create: `internal/otel/otel.go`
- Create: `internal/otel/otel_test.go`

- [ ] **Step 1: Write the failing test for Setup with OTel disabled**

Create `internal/otel/otel_test.go`:

```go
package otel

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

func TestSetup_Disabled(t *testing.T) {
	cfg := Config{
		Enabled:      false,
		SamplingRate: 1.0,
	}

	shutdown, err := Setup(context.Background(), cfg)
	require.NoError(t, err)
	require.NotNil(t, shutdown)

	// Tracer should be a no-op
	tracer := otel.Tracer("test")
	_, span := tracer.Start(context.Background(), "test-span")
	assert.False(t, span.SpanContext().IsValid(), "disabled OTel should produce invalid span contexts")
	span.End()

	// Shutdown should succeed
	err = shutdown(context.Background())
	assert.NoError(t, err)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit name=TestSetup_Disabled`
Expected: FAIL — package `internal/otel` does not exist.

- [ ] **Step 3: Implement Setup with disabled path**

Create `internal/otel/otel.go`:

```go
package otel

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// Config holds TMI-side OpenTelemetry configuration.
// All other OTel settings (endpoint, headers, etc.) use standard OTEL_* env vars.
type Config struct {
	Enabled        bool
	SamplingRate   float64
	PrometheusPort int
}

// Setup initializes OpenTelemetry trace and metric providers.
// Returns a shutdown function that must be called on server stop.
// When cfg.Enabled is false, registers no-op providers (zero overhead).
func Setup(ctx context.Context, cfg Config) (shutdown func(context.Context) error, err error) {
	logger := slogging.Get()

	if !cfg.Enabled {
		logger.Info("OpenTelemetry disabled")
		return func(context.Context) error { return nil }, nil
	}

	logger.Info("Initializing OpenTelemetry")

	// Build resource with service metadata
	res, err := resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
		resource.WithHost(),
		resource.WithProcess(),
		resource.WithAttributes(
			semconv.ServiceName("tmi"),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTel resource: %w", err)
	}

	// Set up W3C trace context propagation
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// Create trace exporter (OTLP gRPC, falls back to stdout if no endpoint)
	traceExporter, err := otlptracegrpc.New(ctx)
	if err != nil {
		logger.Warn("OTLP trace exporter failed, falling back to stdout: %v", err)
		traceExporter, err = stdouttrace.New(stdouttrace.WithPrettyPrint())
		if err != nil {
			return nil, fmt.Errorf("failed to create stdout trace exporter: %w", err)
		}
	}

	// Create tracer provider with sampling
	sampler := sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SamplingRate))
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	)
	otel.SetTracerProvider(tp)

	// Create metric exporter (OTLP gRPC)
	metricExporter, err := otlpmetricgrpc.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP metric exporter: %w", err)
	}

	// Create meter provider with OTLP reader
	readers := []sdkmetric.Option{
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter)),
	}

	// Optionally add Prometheus exporter for local scraping
	if cfg.PrometheusPort > 0 {
		promExporter, err := prometheus.New()
		if err != nil {
			return nil, fmt.Errorf("failed to create Prometheus exporter: %w", err)
		}
		readers = append(readers, sdkmetric.WithReader(promExporter))

		// Start Prometheus HTTP server in background
		promMux := http.NewServeMux()
		promMux.Handle("/metrics", promhttp.Handler())
		promServer := &http.Server{
			Addr:    fmt.Sprintf(":%d", cfg.PrometheusPort),
			Handler: promMux,
		}
		go func() {
			logger.Info("Prometheus metrics endpoint listening on :%d/metrics", cfg.PrometheusPort)
			if err := promServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				logger.Error("Prometheus server error: %v", err)
			}
		}()
	}

	mp := sdkmetric.NewMeterProvider(readers...)
	otel.SetMeterProvider(mp)

	logger.Info("OpenTelemetry initialized (sampling_rate=%.2f)", cfg.SamplingRate)

	// Return composite shutdown
	shutdownFn := func(ctx context.Context) error {
		logger.Info("Shutting down OpenTelemetry")
		var errs []error
		if err := tp.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("trace provider shutdown: %w", err))
		}
		if err := mp.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("meter provider shutdown: %w", err))
		}
		if len(errs) > 0 {
			return fmt.Errorf("OTel shutdown errors: %v", errs)
		}
		return nil
	}

	return shutdownFn, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `make test-unit name=TestSetup_Disabled`
Expected: PASS

- [ ] **Step 5: Write test for Setup with OTel enabled (in-memory exporter)**

Add to `internal/otel/otel_test.go`:

```go
func TestSetup_Enabled_ProducesSpans(t *testing.T) {
	cfg := Config{
		Enabled:      true,
		SamplingRate: 1.0,
	}

	// Set OTEL env to use a non-existent endpoint so OTLP exporter creation
	// still succeeds (it connects lazily). We just need providers registered.
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:4317")

	shutdown, err := Setup(context.Background(), cfg)
	require.NoError(t, err)
	require.NotNil(t, shutdown)
	defer func() {
		_ = shutdown(context.Background())
	}()

	// Tracer should produce valid spans
	tracer := otel.Tracer("test")
	_, span := tracer.Start(context.Background(), "test-span")
	assert.True(t, span.SpanContext().IsValid(), "enabled OTel should produce valid span contexts")
	span.End()
}
```

- [ ] **Step 6: Run test to verify it passes**

Run: `make test-unit name=TestSetup_Enabled_ProducesSpans`
Expected: PASS

- [ ] **Step 7: Run full unit test suite**

Run: `make test-unit`
Expected: All tests pass.

- [ ] **Step 8: Commit**

```bash
git add internal/otel/otel.go internal/otel/otel_test.go
git commit -m "feat(otel): add internal/otel package with Setup and shutdown"
```

---

## Task 3: Upgrade go-redis v8 to v9

This is a prerequisite for Redis OTel instrumentation. The migration is primarily import path changes.

**Files:**
- Modify: `go.mod`
- Modify: 23 files importing `github.com/go-redis/redis/v8`

- [ ] **Step 1: Update go.mod — replace go-redis v8 with v9 and add redisotel**

Run:

```bash
cd /Users/efitz/Projects/tmi
go get github.com/redis/go-redis/v9
go get github.com/redis/go-redis/extra/redisotel/v9
```

- [ ] **Step 2: Update import paths in all 23 files**

Replace `"github.com/go-redis/redis/v8"` with `"github.com/redis/go-redis/v9"` in all files:

- `auth/db/redis.go`
- `auth/db/redis_validator.go`
- `auth/db/redis_validator_test.go`
- `auth/db/redis_health.go`
- `auth/db/redis_encryption_test.go`
- `auth/main.go`
- `auth/token_blacklist.go`
- `auth/token_blacklist_test.go`
- `auth/service_test_helpers.go`
- `api/ip_rate_limiter.go`
- `api/auth_flow_rate_limiter.go`
- `api/api_rate_limiter.go`
- `api/addon_rate_limiter.go`
- `api/sliding_window_rate_limiter.go`
- `api/webhook_delivery_redis_store.go`
- `api/webhook_event_consumer.go`
- `api/webhook_rate_limiter.go`
- `api/webhook_test_helpers.go`
- `api/cache_service.go`
- `api/cache_invalidation_test.go`
- `api/settings_service.go`
- `api/events.go`
- `test/integration/framework/redis.go`

- [ ] **Step 3: Fix go-redis v9 API changes in redis.go**

In `auth/db/redis.go`, rename `MaxConnAge` to `ConnMaxLifetime` in the `redis.Options` struct:

Change:
```go
MaxConnAge:   time.Hour,
```
To:
```go
ConnMaxLifetime: time.Hour,
```

- [ ] **Step 4: Run go mod tidy to clean up dependencies**

```bash
go mod tidy
```

- [ ] **Step 5: Build to check for compilation errors**

Run: `make build-server`
Expected: Build succeeds. If there are v8→v9 API incompatibilities, fix them (most common: `MinIdleConns` stays the same, `MaxConnAge` becomes `ConnMaxLifetime`, `redis.Nil` stays the same, context-first method signatures stay the same).

- [ ] **Step 6: Run full unit test suite**

Run: `make test-unit`
Expected: All tests pass. The miniredis test library (`github.com/alicebob/miniredis/v2`) supports both v8 and v9 patterns.

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "deps: upgrade go-redis from v8 to v9 for OTel instrumentation support"
```

---

## Task 4: Add GORM and Redis OTel Instrumentation

**Files:**
- Modify: `auth/db/gorm.go`
- Modify: `auth/db/redis.go`
- Modify: `go.mod`

- [ ] **Step 1: Add OTel dependencies**

```bash
cd /Users/efitz/Projects/tmi
go get github.com/uptrace/opentelemetry-go-extra/otelgorm
go get go.opentelemetry.io/otel
go mod tidy
```

- [ ] **Step 2: Add otelgorm plugin to GORM connection**

In `auth/db/gorm.go`, after the `db, err := gorm.Open(dialector, gormConfig)` block and its error check, add:

```go
	// Register OpenTelemetry GORM plugin for query tracing
	if err := db.Use(otelgorm.NewPlugin(
		otelgorm.WithDBName(cfg.Database),
		otelgorm.WithoutQueryVariables(),
	)); err != nil {
		log.Warn("Failed to register OTel GORM plugin (tracing disabled for DB): %v", err)
	}
```

Add the import:
```go
"github.com/uptrace/opentelemetry-go-extra/otelgorm"
```

- [ ] **Step 3: Add redisotel instrumentation to Redis connection**

In `auth/db/redis.go`, after the `client := redis.NewClient(...)` call and before the ping test, add:

```go
	// Register OpenTelemetry Redis instrumentation for tracing and metrics
	if err := redisotel.InstrumentTracing(client); err != nil {
		logger.Warn("Failed to instrument Redis tracing: %v", err)
	}
	if err := redisotel.InstrumentMetrics(client); err != nil {
		logger.Warn("Failed to instrument Redis metrics: %v", err)
	}
```

Add the import:
```go
"github.com/redis/go-redis/extra/redisotel/v9"
```

- [ ] **Step 4: Build to verify compilation**

Run: `make build-server`
Expected: Build succeeds.

- [ ] **Step 5: Run full unit test suite**

Run: `make test-unit`
Expected: All tests pass. When OTel is not initialized (no global provider), the plugins produce no-op spans/metrics with zero overhead.

- [ ] **Step 6: Commit**

```bash
git add auth/db/gorm.go auth/db/redis.go go.mod go.sum
git commit -m "feat(otel): add GORM and Redis OTel instrumentation"
```

---

## Task 5: Add otelgin Middleware and Span Enrichment

**Files:**
- Create: `api/otel_middleware.go`
- Create: `api/otel_middleware_test.go`
- Modify: `cmd/server/main.go`

- [ ] **Step 1: Write the failing test for span enrichment middleware**

Create `api/otel_middleware_test.go`:

```go
package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/attribute"
)

func TestOTelSpanEnrichment_AddsUserID(t *testing.T) {
	// Set up in-memory span exporter
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)
	defer func() { _ = tp.Shutdown(nil) }()

	gin.SetMode(gin.TestMode)
	r := gin.New()

	// Simulate: start a span (normally otelgin does this), then enrich
	r.Use(func(c *gin.Context) {
		tracer := otel.Tracer("test")
		ctx, span := tracer.Start(c.Request.Context(), "test-request")
		defer span.End()
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	r.Use(OTelSpanEnrichmentMiddleware())
	r.GET("/test", func(c *gin.Context) {
		// Simulate auth middleware setting user ID
		c.Set("userID", "alice")
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)

	// Check that tmi.user.id attribute was set
	found := false
	for _, attr := range spans[0].Attributes {
		if attr.Key == "tmi.user.id" && attr.Value.AsString() == "alice" {
			found = true
		}
	}
	assert.True(t, found, "span should have tmi.user.id=alice attribute")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit name=TestOTelSpanEnrichment_AddsUserID`
Expected: FAIL — `OTelSpanEnrichmentMiddleware` does not exist.

- [ ] **Step 3: Implement span enrichment middleware**

Create `api/otel_middleware.go`:

```go
package api

import (
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// OTelSpanEnrichmentMiddleware adds TMI-specific attributes to the active OTel span.
// Must be placed after auth middleware so user context is available.
func OTelSpanEnrichmentMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		span := trace.SpanFromContext(c.Request.Context())
		if !span.IsRecording() {
			return
		}

		var attrs []attribute.KeyValue

		if userID, exists := c.Get("userID"); exists {
			if id, ok := userID.(string); ok && id != "" {
				attrs = append(attrs, attribute.String("tmi.user.id", id))
			}
		}

		if tmID, exists := c.Get("threatModelID"); exists {
			if id, ok := tmID.(string); ok && id != "" {
				attrs = append(attrs, attribute.String("tmi.threat_model.id", id))
			}
		}

		if diagID, exists := c.Get("diagramID"); exists {
			if id, ok := diagID.(string); ok && id != "" {
				attrs = append(attrs, attribute.String("tmi.diagram.id", id))
			}
		}

		if reqID, exists := c.Get("requestID"); exists {
			if id, ok := reqID.(string); ok && id != "" {
				attrs = append(attrs, attribute.String("tmi.request.id", id))
			}
		}

		if len(attrs) > 0 {
			span.SetAttributes(attrs...)
		}
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `make test-unit name=TestOTelSpanEnrichment_AddsUserID`
Expected: PASS

Note: The test sets `userID` on the gin context inside the handler. The enrichment middleware runs `c.Next()` first, then reads attributes — so the handler must set context values before the middleware reads them. Check the test carefully: the middleware calls `c.Next()` which runs the handler, then reads the gin context keys. This works because the handler sets `c.Set("userID", "alice")` during `c.Next()`.

- [ ] **Step 5: Wire otelgin and enrichment middleware into server startup**

In `cmd/server/main.go`, add the otelgin middleware after recovery and before auth. Find the line:

```go
r.Use(slogging.LoggerMiddleware())
```

Add immediately after it:

```go
// OpenTelemetry HTTP tracing middleware
r.Use(otelgin.Middleware("tmi"))
```

Add the span enrichment middleware after the JWT middleware. Find where `JWTMiddleware` is added and add after it:

```go
// Enrich OTel spans with TMI-specific attributes (user ID, resource IDs)
r.Use(api.OTelSpanEnrichmentMiddleware())
```

Add the imports:

```go
"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
```

- [ ] **Step 6: Add otelgin dependency**

```bash
go get go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin
go mod tidy
```

- [ ] **Step 7: Build to verify compilation**

Run: `make build-server`
Expected: Build succeeds.

- [ ] **Step 8: Run full unit test suite**

Run: `make test-unit`
Expected: All tests pass.

- [ ] **Step 9: Commit**

```bash
git add api/otel_middleware.go api/otel_middleware_test.go cmd/server/main.go go.mod go.sum
git commit -m "feat(otel): add otelgin middleware and span enrichment"
```

---

## Task 6: Wire OTel Setup into Server Startup and Shutdown

**Files:**
- Modify: `cmd/server/main.go`

- [ ] **Step 1: Add OTel setup call in server startup**

In `cmd/server/main.go`, find the section where the server is initialized (before `setupRouter` is called). Add OTel setup as a new phase. After PHASE 1 (database connections) and before the router setup, add:

```go
	// Initialize OpenTelemetry
	otelCfg := tmiotel.Config{
		Enabled:        cfg.Observability.Enabled,
		SamplingRate:   cfg.Observability.SamplingRate,
		PrometheusPort: cfg.Observability.PrometheusPort,
	}
	otelShutdown, err := tmiotel.Setup(ctx, otelCfg)
	if err != nil {
		logger.Error("Failed to initialize OpenTelemetry: %v", err)
		return 1
	}
```

Add the import (aliased to avoid collision with the standard `otel` package):

```go
tmiotel "github.com/ericfitz/tmi/internal/otel"
```

- [ ] **Step 2: Add OTel shutdown to graceful shutdown section**

In the graceful shutdown section of `cmd/server/main.go`, add before the final `return 0`:

```go
	// Shutdown OpenTelemetry (flush pending spans/metrics)
	logger.Info("Shutting down OpenTelemetry...")
	if err := otelShutdown(shutdownCtx); err != nil {
		logger.Error("Error shutting down OpenTelemetry: %v", err)
	}
```

- [ ] **Step 3: Build to verify compilation**

Run: `make build-server`
Expected: Build succeeds.

- [ ] **Step 4: Run full unit test suite**

Run: `make test-unit`
Expected: All tests pass.

- [ ] **Step 5: Commit**

```bash
git add cmd/server/main.go
git commit -m "feat(otel): wire OTel setup and shutdown into server lifecycle"
```

---

## Task 7: Wrap Outbound HTTP Clients with otelhttp

**Files:**
- Modify: `api/webhook_base_worker.go`
- Modify: `api/timmy_llm_service.go`
- Modify: `api/timmy_content_provider_http.go`
- Modify: `auth/provider.go`

- [ ] **Step 1: Add otelhttp dependency**

```bash
go get go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp
go mod tidy
```

- [ ] **Step 2: Wrap webhook HTTP client**

In `api/webhook_base_worker.go`, find the function that creates the HTTP client (look for `&http.Client{`). Wrap its transport:

```go
import "go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
```

Change the client creation from:

```go
&http.Client{
	Timeout: timeout,
	// ... existing config
}
```

To:

```go
&http.Client{
	Timeout:   timeout,
	Transport: otelhttp.NewTransport(http.DefaultTransport),
	// ... existing config (keep CheckRedirect if present)
}
```

If the client already has a custom Transport, wrap that instead of `http.DefaultTransport`.

- [ ] **Step 3: Wrap Timmy LLM HTTP client**

In `api/timmy_llm_service.go`, find where the HTTP client is created (look for `&http.Client{Timeout:`). Wrap its transport:

```go
import "go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
```

Change:
```go
httpClient := &http.Client{Timeout: httpTimeout}
```

To:
```go
httpClient := &http.Client{
	Timeout:   httpTimeout,
	Transport: otelhttp.NewTransport(http.DefaultTransport),
}
```

- [ ] **Step 4: Wrap HTTP content provider client**

In `api/timmy_content_provider_http.go`, find the HTTP client creation and wrap its transport the same way.

- [ ] **Step 5: Wrap OAuth provider HTTP client**

In `auth/provider.go`, find the HTTP client creation and wrap its transport the same way.

- [ ] **Step 6: Build to verify compilation**

Run: `make build-server`
Expected: Build succeeds.

- [ ] **Step 7: Run full unit test suite**

Run: `make test-unit`
Expected: All tests pass. The otelhttp transport is transparent when no tracer provider is configured.

- [ ] **Step 8: Commit**

```bash
git add api/webhook_base_worker.go api/timmy_llm_service.go api/timmy_content_provider_http.go auth/provider.go go.mod go.sum
git commit -m "feat(otel): wrap outbound HTTP clients with otelhttp transport"
```

---

## Task 8: Add Timmy SSE and Chat Span Instrumentation

**Files:**
- Modify: `api/timmy_handlers.go`
- Modify: `api/timmy_session_manager.go`
- Modify: `api/timmy_llm_service.go`

- [ ] **Step 1: Add SSE parent span to CreateTimmyChatSession handler**

In `api/timmy_handlers.go`, in the `CreateTimmyChatSession` handler function, wrap the main body with a span. After setting up the SSE writer, start a span:

```go
import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)
```

Near the top of the handler, after initial validation:

```go
	tracer := otel.Tracer("tmi.timmy")
	ctx, span := tracer.Start(c.Request.Context(), "timmy.session.create",
		trace.WithAttributes(
			attribute.String("tmi.sse.stream_type", "chat_session_create"),
			attribute.String("tmi.threat_model.id", threatModelID),
		),
	)
	defer span.End()
```

Pass `ctx` (instead of `c.Request.Context()`) to downstream calls like `timmySessionManager.CreateSession()`.

- [ ] **Step 2: Add SSE parent span to CreateTimmyChatMessage handler**

In `api/timmy_handlers.go`, in the `CreateTimmyChatMessage` handler, add a similar parent span:

```go
	tracer := otel.Tracer("tmi.timmy")
	ctx, span := tracer.Start(c.Request.Context(), "timmy.message.handle",
		trace.WithAttributes(
			attribute.String("tmi.sse.stream_type", "chat_message"),
			attribute.String("tmi.sse.session_id", sessionID),
		),
	)
	defer func() {
		span.SetAttributes(attribute.Int("tmi.sse.event_count", eventCount))
		span.End()
	}()
```

Track `eventCount` by incrementing a local counter each time an SSE event is sent.

- [ ] **Step 3: Add child spans to session manager**

In `api/timmy_session_manager.go`, add child spans in `CreateSession`:

```go
import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)
```

In `CreateSession`, wrap the entity snapshotting:

```go
	tracer := otel.Tracer("tmi.timmy")
	ctx, snapshotSpan := tracer.Start(ctx, "timmy.session.snapshot")
	// ... existing snapshot code ...
	snapshotSpan.End()
```

Wrap the vector index preparation:

```go
	ctx, indexSpan := tracer.Start(ctx, "timmy.session.index_prepare")
	// ... existing index preparation code ...
	indexSpan.End()
```

In `HandleMessage`, wrap context building:

```go
	ctx, buildSpan := tracer.Start(ctx, "timmy.context.build")
	// ... existing Tier 1 + Tier 2 context building ...
	buildSpan.SetAttributes(
		attribute.Int("tmi.timmy.tier1_entities", tier1Count),
		attribute.Int("tmi.timmy.tier2_results", tier2Count),
	)
	buildSpan.End()
```

- [ ] **Step 4: Add child spans to LLM service**

In `api/timmy_llm_service.go`, wrap the LLM generation call:

```go
import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)
```

In the method that calls `chatModel.GenerateContent()`:

```go
	tracer := otel.Tracer("tmi.timmy")
	ctx, llmSpan := tracer.Start(ctx, "timmy.llm.generate",
		trace.WithAttributes(
			attribute.String("tmi.timmy.model", s.chatModelName),
		),
	)
	defer func() {
		llmSpan.SetAttributes(
			attribute.Int("tmi.timmy.token_count", totalTokens),
		)
		llmSpan.End()
	}()
```

Wrap the `EmbedTexts` method:

```go
	ctx, embedSpan := tracer.Start(ctx, "timmy.embedding.generate",
		trace.WithAttributes(
			attribute.String("tmi.timmy.embedding_model", s.embeddingModelName),
			attribute.Int("tmi.timmy.text_count", len(texts)),
		),
	)
	defer embedSpan.End()
```

- [ ] **Step 5: Build to verify compilation**

Run: `make build-server`
Expected: Build succeeds.

- [ ] **Step 6: Run full unit test suite**

Run: `make test-unit`
Expected: All tests pass.

- [ ] **Step 7: Commit**

```bash
git add api/timmy_handlers.go api/timmy_session_manager.go api/timmy_llm_service.go
git commit -m "feat(otel): add trace spans for Timmy SSE streams and LLM operations"
```

---

## Task 9: Add Timmy and Infrastructure Metrics

**Files:**
- Create: `internal/otel/metrics.go`
- Create: `internal/otel/metrics_test.go`
- Modify: `api/timmy_llm_service.go`
- Modify: `api/timmy_sse.go`
- Modify: `api/timmy_handlers.go`

- [ ] **Step 1: Write test for metrics instrument creation**

Create `internal/otel/metrics_test.go`:

```go
package otel

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestNewTMIMetrics(t *testing.T) {
	// Set up in-memory metric reader
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(mp)
	defer func() { _ = mp.Shutdown(nil) }()

	metrics, err := NewTMIMetrics()
	require.NoError(t, err)
	require.NotNil(t, metrics)
	require.NotNil(t, metrics.CacheHits)
	require.NotNil(t, metrics.CacheMisses)
	require.NotNil(t, metrics.TimmyActiveSessions)
	require.NotNil(t, metrics.TimmyLLMDuration)
	require.NotNil(t, metrics.TimmyLLMTokens)

	// Record a metric and verify it's collected
	metrics.CacheHits.Add(nil, 1)

	var rm metricdata.ResourceMetrics
	err = reader.Collect(nil, &rm)
	require.NoError(t, err)
	assert.Greater(t, len(rm.ScopeMetrics), 0, "should have collected metrics")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit name=TestNewTMIMetrics`
Expected: FAIL — `NewTMIMetrics` does not exist.

- [ ] **Step 3: Implement TMIMetrics**

Create `internal/otel/metrics.go`:

```go
package otel

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

const meterName = "tmi"

// TMIMetrics holds all custom OTel metric instruments for TMI.
type TMIMetrics struct {
	// Cache metrics
	CacheHits   metric.Int64Counter
	CacheMisses metric.Int64Counter

	// WebSocket metrics
	WebSocketActiveSessions      metric.Int64UpDownCounter
	WebSocketActiveParticipants  metric.Int64UpDownCounter
	WebSocketMessages            metric.Int64Counter

	// Webhook metrics
	WebhookDeliveries metric.Int64Counter

	// Timmy metrics
	TimmyActiveSessions  metric.Int64UpDownCounter
	TimmyLLMDuration     metric.Float64Histogram
	TimmyLLMTokens       metric.Int64Counter
	TimmyEmbedDuration   metric.Float64Histogram
	TimmySSEDuration     metric.Float64Histogram
	TimmySSEEvents       metric.Int64Counter
}

// NewTMIMetrics creates and registers all TMI metric instruments.
func NewTMIMetrics() (*TMIMetrics, error) {
	meter := otel.Meter(meterName)
	m := &TMIMetrics{}
	var err error

	// Cache
	if m.CacheHits, err = meter.Int64Counter("tmi.cache.hit",
		metric.WithDescription("Cache hits")); err != nil {
		return nil, err
	}
	if m.CacheMisses, err = meter.Int64Counter("tmi.cache.miss",
		metric.WithDescription("Cache misses")); err != nil {
		return nil, err
	}

	// WebSocket
	if m.WebSocketActiveSessions, err = meter.Int64UpDownCounter("tmi.websocket.sessions.active",
		metric.WithDescription("Active WebSocket sessions")); err != nil {
		return nil, err
	}
	if m.WebSocketActiveParticipants, err = meter.Int64UpDownCounter("tmi.websocket.participants.active",
		metric.WithDescription("Active WebSocket participants")); err != nil {
		return nil, err
	}
	if m.WebSocketMessages, err = meter.Int64Counter("tmi.websocket.messages",
		metric.WithDescription("WebSocket messages")); err != nil {
		return nil, err
	}

	// Webhooks
	if m.WebhookDeliveries, err = meter.Int64Counter("tmi.webhook.deliveries",
		metric.WithDescription("Webhook delivery attempts")); err != nil {
		return nil, err
	}

	// Timmy
	if m.TimmyActiveSessions, err = meter.Int64UpDownCounter("tmi.timmy.session.active",
		metric.WithDescription("Active Timmy chat sessions")); err != nil {
		return nil, err
	}
	if m.TimmyLLMDuration, err = meter.Float64Histogram("tmi.timmy.llm.duration",
		metric.WithDescription("LLM call latency in seconds"),
		metric.WithUnit("s")); err != nil {
		return nil, err
	}
	if m.TimmyLLMTokens, err = meter.Int64Counter("tmi.timmy.llm.tokens",
		metric.WithDescription("LLM tokens consumed")); err != nil {
		return nil, err
	}
	if m.TimmyEmbedDuration, err = meter.Float64Histogram("tmi.timmy.embedding.duration",
		metric.WithDescription("Embedding call latency in seconds"),
		metric.WithUnit("s")); err != nil {
		return nil, err
	}
	if m.TimmySSEDuration, err = meter.Float64Histogram("tmi.timmy.sse.duration",
		metric.WithDescription("SSE stream total duration in seconds"),
		metric.WithUnit("s")); err != nil {
		return nil, err
	}
	if m.TimmySSEEvents, err = meter.Int64Counter("tmi.timmy.sse.events",
		metric.WithDescription("SSE events sent")); err != nil {
		return nil, err
	}

	return m, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `make test-unit name=TestNewTMIMetrics`
Expected: PASS

- [ ] **Step 5: Wire TMIMetrics into server startup**

In `cmd/server/main.go`, after the OTel setup call, create the metrics:

```go
	// Create TMI metric instruments
	tmiMetrics, err := tmiotel.NewTMIMetrics()
	if err != nil {
		logger.Error("Failed to create OTel metrics: %v", err)
		return 1
	}
```

Store `tmiMetrics` in a way that api package code can access it. Add it to the `Server` struct or use a package-level variable in `api/`. The simplest approach matching the existing `GlobalPerformanceMonitor` pattern:

In `internal/otel/metrics.go`, add:

```go
// GlobalMetrics holds the TMI metrics instance for package-level access.
var GlobalMetrics *TMIMetrics
```

In `cmd/server/main.go`, set it:

```go
tmiotel.GlobalMetrics = tmiMetrics
```

- [ ] **Step 6: Add Timmy LLM duration and token metrics**

In `api/timmy_llm_service.go`, in the LLM generate span (from Task 8), record metrics:

```go
import tmiotel "github.com/ericfitz/tmi/internal/otel"
```

After the LLM call completes:

```go
	if m := tmiotel.GlobalMetrics; m != nil {
		m.TimmyLLMDuration.Record(ctx, duration.Seconds())
		m.TimmyLLMTokens.Add(ctx, int64(promptTokens), metric.WithAttributes(attribute.String("direction", "prompt")))
		m.TimmyLLMTokens.Add(ctx, int64(completionTokens), metric.WithAttributes(attribute.String("direction", "completion")))
	}
```

After the embedding call completes:

```go
	if m := tmiotel.GlobalMetrics; m != nil {
		m.TimmyEmbedDuration.Record(ctx, duration.Seconds())
	}
```

- [ ] **Step 7: Add SSE event counter metric**

In `api/timmy_sse.go`, in `SendEvent`, after sending the event:

```go
import tmiotel "github.com/ericfitz/tmi/internal/otel"
```

```go
	if m := tmiotel.GlobalMetrics; m != nil {
		m.TimmySSEEvents.Add(context.Background(), 1, metric.WithAttributes(attribute.String("event_type", event)))
	}
```

- [ ] **Step 8: Add Timmy active session counter**

In `api/timmy_handlers.go`, at session creation:

```go
	if m := tmiotel.GlobalMetrics; m != nil {
		m.TimmyActiveSessions.Add(ctx, 1)
	}
```

At session close/end (find where sessions are cleaned up):

```go
	if m := tmiotel.GlobalMetrics; m != nil {
		m.TimmyActiveSessions.Add(ctx, -1)
	}
```

- [ ] **Step 9: Build to verify compilation**

Run: `make build-server`
Expected: Build succeeds.

- [ ] **Step 10: Run full unit test suite**

Run: `make test-unit`
Expected: All tests pass.

- [ ] **Step 11: Commit**

```bash
git add internal/otel/metrics.go internal/otel/metrics_test.go api/timmy_llm_service.go api/timmy_sse.go api/timmy_handlers.go cmd/server/main.go
git commit -m "feat(otel): add Timmy and infrastructure OTel metrics"
```

---

## Task 10: Add DB and Redis Pool Metrics (Observable Gauges)

**Files:**
- Modify: `internal/otel/metrics.go`
- Modify: `internal/otel/metrics_test.go`
- Modify: `internal/otel/otel.go`

- [ ] **Step 1: Write test for pool metrics registration**

Add to `internal/otel/metrics_test.go`:

```go
func TestRegisterPoolMetrics(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(mp)
	defer func() { _ = mp.Shutdown(nil) }()

	// Mock DB stats provider
	dbStats := func() DBPoolStats {
		return DBPoolStats{
			OpenConnections: 5,
			Idle:            3,
			InUse:           2,
			WaitCount:       10,
			WaitDuration:    100 * time.Millisecond,
		}
	}

	err := RegisterPoolMetrics(dbStats, nil)
	require.NoError(t, err)

	var rm metricdata.ResourceMetrics
	err = reader.Collect(nil, &rm)
	require.NoError(t, err)
	assert.Greater(t, len(rm.ScopeMetrics), 0)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit name=TestRegisterPoolMetrics`
Expected: FAIL — `RegisterPoolMetrics` does not exist.

- [ ] **Step 3: Implement pool metrics with observable gauges**

Add to `internal/otel/metrics.go`:

```go
import (
	"database/sql"
	"time"

	"go.opentelemetry.io/otel/metric"
)

// DBPoolStats provides database connection pool statistics.
type DBPoolStats struct {
	OpenConnections int
	Idle            int
	InUse           int
	WaitCount       int64
	WaitDuration    time.Duration
}

// RedisPoolStats provides Redis connection pool statistics.
type RedisPoolStats struct {
	ActiveCount int
	IdleCount   int
}

// RegisterPoolMetrics registers observable gauges for DB and Redis connection pools.
// Pass nil for either stats function to skip that pool's metrics.
func RegisterPoolMetrics(dbStatsFn func() DBPoolStats, redisStatsFn func() RedisPoolStats) error {
	meter := otel.Meter(meterName)

	if dbStatsFn != nil {
		if _, err := meter.Int64ObservableGauge("tmi.db.pool.open",
			metric.WithDescription("Open database connections"),
			metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
				o.Observe(int64(dbStatsFn().OpenConnections))
				return nil
			}),
		); err != nil {
			return err
		}

		if _, err := meter.Int64ObservableGauge("tmi.db.pool.idle",
			metric.WithDescription("Idle database connections"),
			metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
				o.Observe(int64(dbStatsFn().Idle))
				return nil
			}),
		); err != nil {
			return err
		}

		if _, err := meter.Int64ObservableGauge("tmi.db.pool.in_use",
			metric.WithDescription("In-use database connections"),
			metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
				o.Observe(int64(dbStatsFn().InUse))
				return nil
			}),
		); err != nil {
			return err
		}
	}

	if redisStatsFn != nil {
		if _, err := meter.Int64ObservableGauge("tmi.redis.pool.active",
			metric.WithDescription("Active Redis connections"),
			metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
				o.Observe(int64(redisStatsFn().ActiveCount))
				return nil
			}),
		); err != nil {
			return err
		}

		if _, err := meter.Int64ObservableGauge("tmi.redis.pool.idle",
			metric.WithDescription("Idle Redis connections"),
			metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
				o.Observe(int64(redisStatsFn().IdleCount))
				return nil
			}),
		); err != nil {
			return err
		}
	}

	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `make test-unit name=TestRegisterPoolMetrics`
Expected: PASS

- [ ] **Step 5: Wire pool metrics into server startup**

In `cmd/server/main.go`, after database and Redis are initialized and tmiMetrics is created, register pool metrics:

```go
	// Register DB and Redis pool metrics
	if sqlDB, err := gormDB.DB(); err == nil {
		dbStatsFn := func() tmiotel.DBPoolStats {
			stats := sqlDB.Stats()
			return tmiotel.DBPoolStats{
				OpenConnections: stats.OpenConnections,
				Idle:            stats.Idle,
				InUse:           stats.InUse,
				WaitCount:       stats.WaitCount,
				WaitDuration:    stats.WaitDuration,
			}
		}
		var redisStatsFn func() tmiotel.RedisPoolStats
		if redisDB != nil {
			redisStatsFn = func() tmiotel.RedisPoolStats {
				stats := redisDB.PoolStats()
				return tmiotel.RedisPoolStats{
					ActiveCount: int(stats.TotalConns - stats.IdleConns),
					IdleCount:   int(stats.IdleConns),
				}
			}
		}
		if err := tmiotel.RegisterPoolMetrics(dbStatsFn, redisStatsFn); err != nil {
			logger.Warn("Failed to register pool metrics: %v", err)
		}
	}
```

Note: You'll need to find the variable names for the GORM DB and Redis DB instances in `cmd/server/main.go` and adjust accordingly. The GORM instance exposes `.DB()` which returns `*sql.DB`. The Redis instance needs a `PoolStats()` method — check the go-redis v9 API for the exact method name and return type.

- [ ] **Step 6: Build to verify compilation**

Run: `make build-server`
Expected: Build succeeds.

- [ ] **Step 7: Run full unit test suite**

Run: `make test-unit`
Expected: All tests pass.

- [ ] **Step 8: Commit**

```bash
git add internal/otel/metrics.go internal/otel/metrics_test.go cmd/server/main.go
git commit -m "feat(otel): add DB and Redis pool observable gauge metrics"
```

---

## Task 11: Replace PerformanceMonitor with OTel Metrics

**Files:**
- Modify: `api/websocket.go`
- Modify: `cmd/server/main.go`
- Delete: `api/performance_monitor.go`

- [ ] **Step 1: Replace PerformanceMonitor calls in websocket.go with OTel metrics**

In `api/websocket.go`, replace each `GlobalPerformanceMonitor` call:

Replace `GlobalPerformanceMonitor.RecordSessionStart(...)`:
```go
	if m := tmiotel.GlobalMetrics; m != nil {
		m.WebSocketActiveSessions.Add(context.Background(), 1)
	}
```

Replace `GlobalPerformanceMonitor.RecordSessionEnd(...)`:
```go
	if m := tmiotel.GlobalMetrics; m != nil {
		m.WebSocketActiveSessions.Add(context.Background(), -1)
	}
```

Replace `GlobalPerformanceMonitor.RecordResyncRequest(...)`:
```go
	// Resync is now visible via traces; no separate metric needed
```

Replace `GlobalPerformanceMonitor.RecordAuthorizationDenied(...)`:
```go
	// Authorization denial is now visible via traces; no separate metric needed
```

Add the import:
```go
tmiotel "github.com/ericfitz/tmi/internal/otel"
```

- [ ] **Step 2: Remove PerformanceMonitor initialization from main.go**

In `cmd/server/main.go`, find and remove:
- The call to `api.InitializePerformanceMonitoring()`
- Any references to `api.GlobalPerformanceMonitor`

- [ ] **Step 3: Delete performance_monitor.go**

```bash
rm api/performance_monitor.go
```

- [ ] **Step 4: Build to verify compilation**

Run: `make build-server`
Expected: Build succeeds. If there are remaining references to PerformanceMonitor, remove them.

- [ ] **Step 5: Run full unit test suite**

Run: `make test-unit`
Expected: All tests pass.

- [ ] **Step 6: Commit**

```bash
git add api/websocket.go cmd/server/main.go
git rm api/performance_monitor.go
git commit -m "refactor(otel): replace PerformanceMonitor with OTel metrics"
```

---

## Task 12: Add Cache Hit/Miss and Webhook Delivery Metrics

**Files:**
- Modify: `api/cache_service.go`
- Modify: `api/webhook_delivery_worker.go`

- [ ] **Step 1: Add cache hit/miss counters to cache_service.go**

In `api/cache_service.go`, add the import:

```go
import (
	tmiotel "github.com/ericfitz/tmi/internal/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)
```

In each `GetCached*` method, where the code logs "Cache hit" or "Cache miss", add the metric:

After each `logger.Debug("Cache miss for ...")`:
```go
	if m := tmiotel.GlobalMetrics; m != nil {
		m.CacheMisses.Add(ctx, 1, metric.WithAttributes(attribute.String("entity_type", "threat")))
	}
```

After each `logger.Debug("Cache hit for ...")`:
```go
	if m := tmiotel.GlobalMetrics; m != nil {
		m.CacheHits.Add(ctx, 1, metric.WithAttributes(attribute.String("entity_type", "threat")))
	}
```

Adjust the `entity_type` attribute value for each method: `"threat"`, `"document"`, `"note"`, `"repository"`, `"asset"`, `"metadata"`, `"cells"`, `"auth_data"`, `"list"`, `"threat_model_response"`, `"middleware_auth"`.

- [ ] **Step 2: Add webhook delivery metric**

In `api/webhook_delivery_worker.go`, add the import:

```go
import (
	tmiotel "github.com/ericfitz/tmi/internal/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)
```

After a successful delivery:
```go
	if m := tmiotel.GlobalMetrics; m != nil {
		m.WebhookDeliveries.Add(ctx, 1, metric.WithAttributes(attribute.String("status", "success")))
	}
```

After a failed delivery:
```go
	if m := tmiotel.GlobalMetrics; m != nil {
		m.WebhookDeliveries.Add(ctx, 1, metric.WithAttributes(attribute.String("status", "failure")))
	}
```

- [ ] **Step 3: Build to verify compilation**

Run: `make build-server`
Expected: Build succeeds.

- [ ] **Step 4: Run full unit test suite**

Run: `make test-unit`
Expected: All tests pass.

- [ ] **Step 5: Commit**

```bash
git add api/cache_service.go api/webhook_delivery_worker.go
git commit -m "feat(otel): add cache hit/miss and webhook delivery metrics"
```

---

## Task 13: Add Trace-Log Correlation

**Files:**
- Modify: `internal/slogging/context.go`

- [ ] **Step 1: Add trace_id to context logger**

In `internal/slogging/context.go`, in the function that creates or returns the context logger (look for where request-scoped attributes like request ID are set), add trace ID extraction:

```go
import (
	"go.opentelemetry.io/otel/trace"
)
```

Where the context logger is created, add:

```go
	// Add OTel trace ID to log entries for trace-log correlation
	spanCtx := trace.SpanContextFromContext(ctx)
	if spanCtx.IsValid() {
		// Add trace_id and span_id for correlation with distributed traces
		attrs = append(attrs, slog.String("trace_id", spanCtx.TraceID().String()))
		attrs = append(attrs, slog.String("span_id", spanCtx.SpanID().String()))
	}
```

The exact insertion point depends on how `ContextLogger` or `GetContextLogger` is structured. Find where `slog.String("request_id", ...)` is set and add the trace attributes nearby.

- [ ] **Step 2: Build to verify compilation**

Run: `make build-server`
Expected: Build succeeds.

- [ ] **Step 3: Run full unit test suite**

Run: `make test-unit`
Expected: All tests pass.

- [ ] **Step 4: Commit**

```bash
git add internal/slogging/context.go
git commit -m "feat(otel): add trace_id and span_id to structured log entries"
```

---

## Task 14: Lint, Build, and Full Test Pass

**Files:** None (verification only)

- [ ] **Step 1: Run linter**

Run: `make lint`
Expected: No new lint errors. Fix any that appear.

- [ ] **Step 2: Run full build**

Run: `make build-server`
Expected: Build succeeds.

- [ ] **Step 3: Run full unit test suite**

Run: `make test-unit`
Expected: All tests pass.

- [ ] **Step 4: Run integration tests**

Run: `make test-integration`
Expected: All tests pass. OTel is disabled by default, so integration tests should be unaffected.

- [ ] **Step 5: Commit any lint or test fixes**

If any fixes were needed:
```bash
git add -A
git commit -m "fix(otel): address lint and test issues from OTel integration"
```

---

## Task 15: Final Commit and Push

- [ ] **Step 1: Verify all changes are committed**

```bash
git status
```
Expected: Clean working tree.

- [ ] **Step 2: Push to remote**

```bash
git pull --rebase
git push
git status
```
Expected: Branch is up to date with `origin/dev/1.4.0`.

- [ ] **Step 3: Close issue #150 if all work is complete**

```bash
gh issue close 150 --repo ericfitz/tmi --reason completed --comment "OpenTelemetry traces and metrics integrated. See commits on dev/1.4.0."
```
